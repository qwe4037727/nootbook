package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"novelagent/internal/novel"
)

// ---- 阻塞修复 #1：孤儿 mapping 状态 ----

// TestRecoveryResetsOrphanMapping 覆盖修复 #1：初始化中途重启 → 孤儿 mapping 重置为 failed → 可续跑。
func TestRecoveryResetsOrphanMapping(t *testing.T) {
	st, dir := newTestStore(t)
	project, err := st.ImportChapters("书", []novel.Chapter{{Title: "一", Content: "一。"}})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	writeKnowledgeDirect(t, st, novel.Knowledge{
		Characters: []novel.Character{{ID: "c_a1", Name: "林越", State: "重伤"}},
		Summaries:  []novel.ChapterSummary{{ChapterID: project.Chapters[0].ID, Index: 1, Summary: "摘要"}},
	})
	// 模拟"初始化中途进程死亡"：init_state 停留 mapping
	st.memoryMu.Lock()
	_ = st.writeMemoryInitStateLocked(MemoryInitState{Status: InitMapping, StartedAt: time.Now().UTC()})
	st.memoryMu.Unlock()

	// 重启（新 Store）→ 孤儿状态被重置为 failed
	st2, err := New(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	state, err := st2.LoadMemoryInitState()
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	if state.Status != InitFailed || state.LastError != "interrupted_by_restart" {
		t.Fatalf("after restart = %+v, want failed/interrupted_by_restart", state)
	}

	// failed → 续跑（202 语义）→ ready
	if _, err := st2.InitializeMemory(); err != nil {
		t.Fatalf("resume after restart: %v", err)
	}
	final := waitForInitTerminal(t, st2)
	if final.Status != InitReady {
		t.Errorf("final = %s (%s), want ready", final.Status, final.LastError)
	}
}

// ---- 阻塞修复 #2：提交失败置 aborted，重试不分叉 ----

// TestCommitFailureAbortsPreparedNoFork 覆盖修复 #2：提交中途失败 → prepared 置 aborted → 重试成功 → 重启无安全态。
func TestCommitFailureAbortsPreparedNoFork(t *testing.T) {
	st, dir := newTestStore(t)
	info, _ := st.LoadMemoryInfo()

	// 构造会失败的提交：payload 文件名含子目录 → 落盘失败
	st.memoryMu.Lock()
	_, firstErr := st.commitMemoryLocked(CommitInitialize, info.MemoryRevision, map[string]any{
		"sub/dir.json": []map[string]any{{"id": "x"}},
	})
	st.memoryMu.Unlock()
	if firstErr == nil || errors.Is(firstErr, ErrRevisionConflict) {
		t.Fatalf("first commit should fail with io error, got %v", firstErr)
	}

	// commits.json 中该记录应为 aborted
	commits := decodeCommits(t, dir)
	if len(commits) != 1 || commits[0].Status != CommitAborted {
		t.Fatalf("commits after failure = %+v, want 1 aborted", commits)
	}

	// 重试（同 basedOn）→ 成功，且不产生分叉
	info, _ = st.LoadMemoryInfo()
	st.memoryMu.Lock()
	record, retryErr := st.commitMemoryLocked(CommitInitialize, info.MemoryRevision, map[string]any{
		"events.json": []map[string]any{{"id": "evt_ok"}},
	})
	st.memoryMu.Unlock()
	if retryErr != nil {
		t.Fatalf("retry commit: %v", retryErr)
	}
	if record.TargetMemoryRevision != info.MemoryRevision+1 {
		t.Errorf("retry target = %d, want %d", record.TargetMemoryRevision, info.MemoryRevision+1)
	}

	// 重启恢复：aborted 跳过、链干净、无安全态、revision 收敛正确
	st2, err := New(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	info2, err := st2.LoadMemoryInfo()
	if err != nil {
		t.Fatalf("info: %v", err)
	}
	if info2.SafeState {
		t.Errorf("safe state after aborted-then-retry recovery: %s", info2.SafeStateError)
	}
	if info2.MemoryRevision != record.TargetMemoryRevision {
		t.Errorf("memoryRevision = %d, want %d", info2.MemoryRevision, record.TargetMemoryRevision)
	}
	commits = decodeCommits(t, dir)
	abortedCount, committedCount := 0, 0
	for _, c := range commits {
		switch c.Status {
		case CommitAborted:
			abortedCount++
		case CommitCommitted:
			committedCount++
		}
	}
	if abortedCount != 1 || committedCount != 1 {
		t.Errorf("commits = %d aborted / %d committed, want 1/1", abortedCount, committedCount)
	}
}

// ---- 阻塞修复 #3：恢复拒绝非法提交记录 ----

// TestRecoveryRejectsInvalidCommitRecords 覆盖修复 #3：非法 status/target/type 记录 → 恢复进安全态。
func TestRecoveryRejectsInvalidCommitRecords(t *testing.T) {
	cases := []struct {
		name   string
		mutate func([]CommitRecord) []CommitRecord
	}{
		{"bogus status", func(rs []CommitRecord) []CommitRecord {
			rs[0].Status = "half_done"
			return rs
		}},
		{"target not basedOn+1", func(rs []CommitRecord) []CommitRecord {
			rs[0].TargetMemoryRevision = rs[0].BasedOnMemoryRevision + 2
			return rs
		}},
		{"bogus type", func(rs []CommitRecord) []CommitRecord {
			rs[0].Type = "mystery"
			return rs
		}},
		{"empty commitId", func(rs []CommitRecord) []CommitRecord {
			rs[0].CommitID = ""
			return rs
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st, dir := newTestStore(t)
			st.memoryMu.Lock()
			if _, err := st.commitMemoryLocked(CommitInitialize, 0, map[string]any{
				"events.json": []map[string]any{{"id": "evt_1"}},
			}); err != nil {
				st.memoryMu.Unlock()
				t.Fatalf("commit: %v", err)
			}
			st.memoryMu.Unlock()
			rewriteCommits(t, dir, tc.mutate)

			st2, err := New(dir)
			if err != nil {
				t.Fatalf("reopen: %v", err)
			}
			info, err := st2.LoadMemoryInfo()
			if err != nil {
				t.Fatalf("info: %v", err)
			}
			if !info.SafeState {
				t.Errorf("invalid record accepted, info = %+v", info)
			}
		})
	}
}

// ---- 阻塞修复 #4：导入新书重置记忆 ----

// TestImportResetsMemory 覆盖修复 #4：旧书 ready → 导入新书 → 记忆全零 + not_started → 可重新初始化。
func TestImportResetsMemory(t *testing.T) {
	st, dir := newTestStore(t)
	oldProject, err := st.ImportChapters("旧书", []novel.Chapter{{Title: "一", Content: "旧。"}})
	if err != nil {
		t.Fatalf("old import: %v", err)
	}
	writeKnowledgeDirect(t, st, novel.Knowledge{
		Characters: []novel.Character{{ID: "c_old1", Name: "旧角色", State: "旧状态"}},
		Summaries:  []novel.ChapterSummary{{ChapterID: oldProject.Chapters[0].ID, Index: 1, Summary: "旧摘要"}},
	})
	if _, err := st.InitializeMemory(); err != nil {
		t.Fatalf("old init: %v", err)
	}
	waitForInitTerminal(t, st)
	before, err := st.LoadMemoryOverview()
	if err != nil {
		t.Fatalf("overview: %v", err)
	}
	if before.MemoryRevision != 1 || before.Counts.Events != 1 {
		t.Fatalf("precondition: %+v", before)
	}

	// 导入新书 → 记忆全零
	newProject, err := st.ImportChapters("新书", []novel.Chapter{{Title: "新章", Content: "新。"}})
	if err != nil {
		t.Fatalf("new import: %v", err)
	}
	after, err := st.LoadMemoryOverview()
	if err != nil {
		t.Fatalf("info after import: %v", err)
	}
	if after.MemoryRevision != 0 || after.LastCommitID != "" {
		t.Errorf("memory after import = %+v, want revision 0 / empty lastCommitId", after)
	}
	if after.Counts.Events != 0 || after.Counts.States != 0 || after.Counts.Cognitions != 0 ||
		after.Counts.Threads != 0 || after.Counts.Plans != 0 {
		t.Errorf("counts after import = %+v, want all zero", after.Counts)
	}
	initState, err := st.LoadMemoryInitState()
	if err != nil {
		t.Fatalf("init state: %v", err)
	}
	if initState.Status != InitNotStarted {
		t.Errorf("init status = %q, want not_started", initState.Status)
	}
	// 提交日志清空
	if commits := decodeCommits(t, dir); len(commits) != 0 {
		t.Errorf("commits after import = %d, want 0", len(commits))
	}
	// 磁盘文件确实清空
	var events []novel.StoryEvent
	raw, err := os.ReadFile(filepath.Join(dir, memoryDirName, "events.json"))
	if err != nil {
		t.Fatalf("read events.json: %v", err)
	}
	if err := json.Unmarshal(raw, &events); err != nil || len(events) != 0 {
		t.Errorf("events.json after import = %s (err=%v)", raw, err)
	}

	// 新书可重新初始化到 ready（旧章节 ID 不再残留）
	writeKnowledgeDirect(t, st, novel.Knowledge{
		Characters: []novel.Character{{ID: "c_new1", Name: "新角色", State: "新状态"}},
		Summaries:  []novel.ChapterSummary{{ChapterID: newProject.Chapters[0].ID, Index: 1, Summary: "新摘要"}},
	})
	if _, err := st.InitializeMemory(); err != nil {
		t.Fatalf("new init: %v", err)
	}
	final := waitForInitTerminal(t, st)
	if final.Status != InitReady {
		t.Errorf("new-book init = %s (%s), want ready", final.Status, final.LastError)
	}
	events = loadEventsForTest(t, dir)
	if len(events) != 1 || events[0].ChapterID != newProject.Chapters[0].ID {
		t.Errorf("events after new-book init = %+v, want only new chapter", events)
	}
	states := loadStatesForTest(t, dir)
	if len(states) != 1 || states[0].SubjectID != "c_new1" {
		t.Errorf("states after new-book init = %+v, want c_new1 only", states)
	}
}

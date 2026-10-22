package store

import (
	"encoding/json"
	"errors"
	"fmt"
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

// ---- P1 修复 #15：aborted 回滚部分写入（RevertPayload）----

// TestCommitFailureRevertsPartialWrites 覆盖 P1-1：
// 多文件 payload 写入中途失败 → 已写入文件按写前快照回滚 → 数据与版本一致 → 重启健康。
func TestCommitFailureRevertsPartialWrites(t *testing.T) {
	st, dir := newTestStore(t)

	// 第一次提交成功：events.json 落地为"旧内容"
	st.memoryMu.Lock()
	_, err := st.commitMemoryLocked(CommitInitialize, 0, map[string]any{
		"events.json": []map[string]any{{"id": "evt_old"}},
	})
	st.memoryMu.Unlock()
	if err != nil {
		t.Fatalf("seed commit: %v", err)
	}

	// 第二次提交失败：events.json（旧文件，先写成功 = 部分写入）+ sub/dir.json（必失败）
	st.memoryMu.Lock()
	_, failErr := st.commitMemoryLocked(CommitFormal, 1, map[string]any{
		"events.json":  []map[string]any{{"id": "evt_new"}},
		"sub/dir.json": []map[string]any{{"id": "x"}},
	})
	st.memoryMu.Unlock()
	if failErr == nil || errors.Is(failErr, ErrRevisionConflict) {
		t.Fatalf("second commit should fail with io error, got %v", failErr)
	}

	// 记录应为 aborted，且携带写前快照
	commits := decodeCommits(t, dir)
	if len(commits) != 2 || commits[1].Status != CommitAborted {
		t.Fatalf("commits = %+v, want [committed, aborted]", commits)
	}
	revert := commits[1].RevertPayload
	if len(revert) != 2 {
		t.Fatalf("revertPayload = %v, want 2 entries", revert)
	}
	if string(revert["sub/dir.json"]) != "null" {
		t.Errorf("revert[sub/dir.json] = %s, want null (file absent before commit)", revert["sub/dir.json"])
	}
	var revertedOld []map[string]any
	if err := json.Unmarshal(revert["events.json"], &revertedOld); err != nil || len(revertedOld) != 1 || revertedOld[0]["id"] != "evt_old" {
		t.Errorf("revert[events.json] = %s, want snapshot of evt_old", revert["events.json"])
	}

	// 部分写入已被回滚：events.json 恢复为旧内容
	events := loadEventsForTest(t, dir)
	if len(events) != 1 || events[0].ID != "evt_old" {
		t.Errorf("events.json after failure = %+v, want reverted to evt_old (partial write must be undone)", events)
	}

	// meta 未推进、无安全态
	info, err := st.LoadMemoryInfo()
	if err != nil {
		t.Fatalf("info: %v", err)
	}
	if info.MemoryRevision != 1 || info.LastCommitID != commits[0].CommitID {
		t.Errorf("meta after failure = rev %d / %s, want rev 1 / %s", info.MemoryRevision, info.LastCommitID, commits[0].CommitID)
	}
	if info.SafeState {
		t.Errorf("unexpected safe state: %s", info.SafeStateError)
	}

	// 同 basedOn 重试成功 → 重启恢复健康且不分叉
	st.memoryMu.Lock()
	record, retryErr := st.commitMemoryLocked(CommitFormal, 1, map[string]any{
		"events.json": []map[string]any{{"id": "evt_old"}, {"id": "evt_retry"}},
	})
	st.memoryMu.Unlock()
	if retryErr != nil {
		t.Fatalf("retry commit: %v", retryErr)
	}
	st2, err := New(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	info2, err := st2.LoadMemoryInfo()
	if err != nil {
		t.Fatalf("info after reopen: %v", err)
	}
	if info2.SafeState {
		t.Errorf("safe state after recovery: %s", info2.SafeStateError)
	}
	if info2.MemoryRevision != record.TargetMemoryRevision {
		t.Errorf("memoryRevision = %d, want %d", info2.MemoryRevision, record.TargetMemoryRevision)
	}
	final := loadEventsForTest(t, dir)
	if len(final) != 2 || final[0].ID != "evt_old" || final[1].ID != "evt_retry" {
		t.Errorf("events after retry+recovery = %+v, want [evt_old evt_retry]", final)
	}
}

// TestRecoveryReplaysAbortedRevert 覆盖 P1-1 恢复路径：
// "标记 aborted 落盘成功、回滚未执行"的崩溃间隙 → 重启恢复补做回滚。
func TestRecoveryReplaysAbortedRevert(t *testing.T) {
	st, dir := newTestStore(t)
	st.memoryMu.Lock()
	_, err := st.commitMemoryLocked(CommitInitialize, 0, map[string]any{
		"events.json": []map[string]any{{"id": "evt_old"}},
	})
	st.memoryMu.Unlock()
	if err != nil {
		t.Fatalf("seed commit: %v", err)
	}

	// 手工构造崩溃残迹：aborted 记录 + 未回滚的"新内容"数据文件
	rewriteCommits(t, dir, func(rs []CommitRecord) []CommitRecord {
		aborted := CommitRecord{
			CommitID:              novel.NewID("commit"),
			Type:                  CommitFormal,
			Status:                CommitAborted,
			BasedOnMemoryRevision: 1,
			TargetMemoryRevision:  2,
			Payload: map[string]json.RawMessage{
				"events.json": json.RawMessage(`[{"id":"evt_new"}]`),
			},
			RevertPayload: map[string]json.RawMessage{
				"events.json": json.RawMessage(`[{"id":"evt_old"}]`),
			},
			CreatedAt: time.Now().UTC(),
		}
		return append(rs, aborted)
	})
	if err := os.WriteFile(filepath.Join(dir, memoryDirName, "events.json"),
		[]byte(`[{"id":"evt_new"}]`), 0o644); err != nil {
		t.Fatalf("simulate un-reverted partial write: %v", err)
	}

	// 重启 → 恢复补做回滚 → events.json 回到 evt_old，健康无安全态
	st2, err := New(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	info, err := st2.LoadMemoryInfo()
	if err != nil {
		t.Fatalf("info: %v", err)
	}
	if info.SafeState {
		t.Errorf("safe state after aborted-revert recovery: %s", info.SafeStateError)
	}
	if info.MemoryRevision != 1 {
		t.Errorf("memoryRevision = %d, want 1 (aborted record must not advance)", info.MemoryRevision)
	}
	events := loadEventsForTest(t, dir)
	if len(events) != 1 || events[0].ID != "evt_old" {
		t.Errorf("events after recovery = %+v, want reverted to evt_old", events)
	}
}

// ---- P1 修复 #16：运行期链尾一致性校验 ----

// TestRuntimeMetaLagRejectedNoFork 覆盖 P1-2：
// meta.json 写失败残留旧版本 → 同进程重试被拒绝进安全态、零新增记录 → 重启收敛健康。
func TestRuntimeMetaLagRejectedNoFork(t *testing.T) {
	st, dir := newTestStore(t)
	st.memoryMu.Lock()
	if _, err := st.commitMemoryLocked(CommitInitialize, 0, map[string]any{
		"events.json": []map[string]any{{"id": "evt_1"}},
	}); err != nil {
		st.memoryMu.Unlock()
		t.Fatalf("seed commit: %v", err)
	}
	st.memoryMu.Unlock()

	// 模拟"meta 对齐写失败"：磁盘 meta 停留在 revision 0 / 空 lastCommitId
	rewriteMeta(t, dir, 0, "")

	// 同进程重试：链尾（rev 1）与 meta（rev 0）不对齐 → 拒绝 + 安全态
	st.memoryMu.Lock()
	_, retryErr := st.commitMemoryLocked(CommitInitialize, 0, map[string]any{
		"events.json": []map[string]any{{"id": "evt_fork"}},
	})
	st.memoryMu.Unlock()
	if !errors.Is(retryErr, ErrMemorySafeState) {
		t.Fatalf("retry error = %v, want ErrMemorySafeState (fork must be impossible)", retryErr)
	}
	// 零新增记录：重试未追加任何分叉记录
	if commits := decodeCommits(t, dir); len(commits) != 1 {
		t.Errorf("commits after rejected retry = %d, want 1 (no fork records)", len(commits))
	}

	// 重启 → 恢复按链尾收敛 meta → 健康、revision=1
	st2, err := New(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	info, err := st2.LoadMemoryInfo()
	if err != nil {
		t.Fatalf("info: %v", err)
	}
	if info.SafeState {
		t.Errorf("safe state after recovery: %s", info.SafeStateError)
	}
	commits := decodeCommits(t, dir)
	if info.MemoryRevision != 1 || info.LastCommitID != commits[0].CommitID {
		t.Errorf("meta after recovery = rev %d / %s, want rev 1 / %s (converged to chain tail)",
			info.MemoryRevision, info.LastCommitID, commits[0].CommitID)
	}
	// 收敛后重试成功：不会被安全态卡死
	st2.memoryMu.Lock()
	_, err = st2.commitMemoryLocked(CommitFormal, 1, map[string]any{
		"events.json": []map[string]any{{"id": "evt_1"}, {"id": "evt_2"}},
	})
	st2.memoryMu.Unlock()
	if err != nil {
		t.Fatalf("commit after recovery: %v", err)
	}
}

// ---- P1 修复 #17：导入重置实际持有 memoryMu ----

// TestImportResetConcurrentWithMemoryOps 覆盖 P1-3：
// 导入重置（store.mu→memoryMu）与记忆读并发 → 无 panic、无解码错误、终态一致。
// 注：本机无 cgo，-race 不可用；本测试至少保证锁互斥路径不饿死、不崩溃。
func TestImportResetConcurrentWithMemoryOps(t *testing.T) {
	st, _ := newTestStore(t)
	oldProject, err := st.ImportChapters("旧书", []novel.Chapter{{Title: "一", Content: "旧。"}})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	writeKnowledgeDirect(t, st, novel.Knowledge{
		Characters: []novel.Character{{ID: "c_1", Name: "角色", State: "状态"}},
		Summaries:  []novel.ChapterSummary{{ChapterID: oldProject.Chapters[0].ID, Index: 1, Summary: "摘要"}},
	})
	if _, err := st.InitializeMemory(); err != nil {
		t.Fatalf("init: %v", err)
	}
	waitForInitTerminal(t, st)

	stop := make(chan struct{})
	readerErr := make(chan error, 1)
	go func() {
		defer close(readerErr)
		for {
			select {
			case <-stop:
				return
			default:
			}
			if _, err := st.LoadMemoryOverview(); err != nil {
				readerErr <- fmt.Errorf("overview: %w", err)
				return
			}
			if _, err := st.LoadMemoryInitState(); err != nil {
				readerErr <- fmt.Errorf("init state: %w", err)
				return
			}
			if _, err := st.LoadMemoryInfo(); err != nil {
				readerErr <- fmt.Errorf("info: %w", err)
				return
			}
		}
	}()

	for i := 0; i < 20; i++ {
		if _, err := st.ImportChapters(fmt.Sprintf("书%d", i), []novel.Chapter{{Title: "章", Content: "正文。"}}); err != nil {
			close(stop)
			t.Fatalf("import %d: %v", i, err)
		}
	}
	close(stop)

	select {
	case err := <-readerErr:
		if err != nil {
			t.Fatalf("concurrent memory read failed: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("memory reads blocked > 15s during imports (lock deadlock suspected)")
	}

	// 终态：最后一次导入后记忆全零、init 未开始
	overview, err := st.LoadMemoryOverview()
	if err != nil {
		t.Fatalf("final overview: %v", err)
	}
	if overview.MemoryRevision != 0 || overview.Counts.Events != 0 || overview.Counts.States != 0 {
		t.Errorf("final memory = %+v, want all zero after reset", overview)
	}
}

// ---- P1 修复 #18：导入重置使在途初始化任务失效 ----

// TestImportInvalidatesInFlightInitTask 覆盖 P1-1（第二复审 S01）：
// 旧初始化任务已读取旧书 knowledge 快照 → 导入新书（代次递增）→
// 旧任务提交前核对代次失败 → 旧角色状态不得写入新书，新书 init_state 保持 not_started。
func TestImportInvalidatesInFlightInitTask(t *testing.T) {
	st, dir := newTestStore(t)
	if _, err := st.ImportChapters("旧书", []novel.Chapter{{Title: "一", Content: "旧。"}}); err != nil {
		t.Fatalf("import old: %v", err)
	}
	writeKnowledgeDirect(t, st, novel.Knowledge{
		Characters: []novel.Character{{ID: "c_old", Name: "旧角色", State: "旧状态"}},
		Summaries:  []novel.ChapterSummary{{ChapterID: "ch_old", Index: 1, Summary: "旧摘要"}},
	})

	// 捕获任务令牌（模拟旧任务已获授权、已读取旧书快照的时刻）
	st.memoryMu.Lock()
	st.initRunCounter++
	runID := st.initRunCounter
	st.initCurrentRun = runID
	token := initTaskToken{Generation: st.memoryGeneration, RunID: runID}
	st.memoryMu.Unlock()

	// 导入新书 → 代次递增、旧运行号失效
	if _, err := st.ImportChapters("新书", []novel.Chapter{{Title: "新", Content: "新。"}}); err != nil {
		t.Fatalf("import new: %v", err)
	}

	// 旧任务继续执行：应因代次不符而放弃，不得写入任何旧书数据
	st.runMemoryMappingFor(token)

	states := loadStatesForTest(t, dir)
	for _, state := range states {
		if state.SubjectID == "c_old" {
			t.Fatalf("old task wrote stale character state into new book: %+v", state)
		}
	}
	events := loadEventsForTest(t, dir)
	for _, event := range events {
		if event.ChapterID == "ch_old" {
			t.Fatalf("old task wrote stale event into new book: %+v", event)
		}
	}
	// 新书初始化状态不得被旧任务改写（不得 ready/partial/failed）
	initState, err := st.LoadMemoryInitState()
	if err != nil {
		t.Fatalf("init state: %v", err)
	}
	if initState.Status != InitNotStarted {
		t.Errorf("new-book init status = %q (%s), want not_started (stale task must not write back)", initState.Status, initState.LastError)
	}
}

// TestInitTokenGuardsTerminalWrite 覆盖 P1-1 的第二道防线：
// 代次失效的任务即使走到终态回写前，也不得覆盖新书的 init_state。
func TestInitTokenGuardsTerminalWrite(t *testing.T) {
	st, _ := newTestStore(t)
	st.memoryMu.Lock()
	st.initRunCounter++
	runID := st.initRunCounter
	st.initCurrentRun = runID
	token := initTaskToken{Generation: st.memoryGeneration, RunID: runID}
	st.memoryMu.Unlock()

	// 令牌失效：运行号被新任务接管
	st.memoryMu.Lock()
	st.initRunCounter++
	st.initCurrentRun = st.initRunCounter
	st.memoryMu.Unlock()

	if st.initTokenValidLocked(token) {
		t.Fatal("token with superseded run id must be invalid")
	}

	// 失效任务调用 failMemoryInit → 不得写任何状态
	st.memoryMu.Lock()
	_ = st.writeMemoryInitStateLocked(MemoryInitState{Status: InitMapping})
	st.memoryMu.Unlock()
	st.failMemoryInit(token, errors.New("boom"))
	initState, err := st.LoadMemoryInitState()
	if err != nil {
		t.Fatalf("init state: %v", err)
	}
	if initState.Status != InitMapping {
		t.Errorf("init status = %q, want mapping (invalidated task must not write failed)", initState.Status)
	}
}

// TestInitGenerationInvalidatedByImport 覆盖代次机制本身：
// 导入递增代次 → 旧令牌失效；新令牌有效。
func TestInitGenerationInvalidatedByImport(t *testing.T) {
	st, _ := newTestStore(t)
	st.memoryMu.Lock()
	st.initRunCounter++
	runID := st.initRunCounter
	st.initCurrentRun = runID
	token := initTaskToken{Generation: st.memoryGeneration, RunID: runID}
	st.memoryMu.Unlock()

	if !st.initTokenValidLocked(token) {
		t.Fatal("fresh token must be valid")
	}
	if _, err := st.ImportChapters("新书", []novel.Chapter{{Title: "一", Content: "一。"}}); err != nil {
		t.Fatalf("import: %v", err)
	}
	st.memoryMu.Lock()
	valid := st.initTokenValidLocked(token)
	st.memoryMu.Unlock()
	if valid {
		t.Error("token must be invalid after import (generation incremented)")
	}
}

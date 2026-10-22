package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestMemoryMetaInitializedOnFirstAccess 覆盖 U5：无 memory/ 目录 → 首次访问创建 meta.json {schemaVersion:1, memoryRevision:0}。
func TestMemoryMetaInitializedOnFirstAccess(t *testing.T) {
	st, dir := newTestStore(t)
	info, err := st.LoadMemoryInfo()
	if err != nil {
		t.Fatalf("LoadMemoryInfo: %v", err)
	}
	if info.SchemaVersion != 1 || info.MemoryRevision != 0 || info.SafeState {
		t.Errorf("info = %+v", info)
	}
	raw, err := os.ReadFile(filepath.Join(dir, memoryDirName, "meta.json"))
	if err != nil {
		t.Fatalf("read meta.json: %v", err)
	}
	var meta map[string]any
	if err := json.Unmarshal(raw, &meta); err != nil {
		t.Fatalf("decode meta.json: %v", err)
	}
	if meta["schemaVersion"] != float64(1) || meta["memoryRevision"] != float64(0) {
		t.Errorf("meta.json = %v", meta)
	}
}

// TestMemoryCommitProtocol 覆盖提交协议正向路径：基线校验通过 → prepared → 目标文件 → committed → meta 对齐。
func TestMemoryCommitProtocol(t *testing.T) {
	st, dir := newTestStore(t)
	info, err := st.LoadMemoryInfo()
	if err != nil {
		t.Fatalf("LoadMemoryInfo: %v", err)
	}

	st.memoryMu.Lock()
	record, err := st.commitMemoryLocked(CommitInitialize, info.MemoryRevision, map[string]any{
		"events.json": []map[string]any{{"id": "evt_1"}},
	})
	st.memoryMu.Unlock()
	if err != nil {
		t.Fatalf("commitMemoryLocked: %v", err)
	}
	if record.Status != CommitCommitted || record.TargetMemoryRevision != 1 {
		t.Errorf("record = %+v", record)
	}

	info, err = st.LoadMemoryInfo()
	if err != nil {
		t.Fatalf("reload info: %v", err)
	}
	if info.MemoryRevision != 1 || info.LastCommitID != record.CommitID {
		t.Errorf("info after commit = %+v", info)
	}

	var events []map[string]any
	raw, err := os.ReadFile(filepath.Join(dir, memoryDirName, "events.json"))
	if err != nil {
		t.Fatalf("read events.json: %v", err)
	}
	if err := json.Unmarshal(raw, &events); err != nil {
		t.Fatalf("decode events.json: %v", err)
	}
	if len(events) != 1 || events[0]["id"] != "evt_1" {
		t.Errorf("events.json = %v", events)
	}

	var commits []CommitRecord
	raw, err = os.ReadFile(filepath.Join(dir, memoryDirName, "commits.json"))
	if err != nil {
		t.Fatalf("read commits.json: %v", err)
	}
	if err := json.Unmarshal(raw, &commits); err != nil {
		t.Fatalf("decode commits.json: %v", err)
	}
	if len(commits) != 1 || commits[0].Status != CommitCommitted || commits[0].BasedOnMemoryRevision != 0 || commits[0].TargetMemoryRevision != 1 {
		t.Errorf("commits.json = %+v", commits)
	}
}

// TestMemoryCommitBaselineConflict 覆盖 U31：basedOn 过期 → 拒绝 revision_conflict 且零落盘。
func TestMemoryCommitBaselineConflict(t *testing.T) {
	st, dir := newTestStore(t)
	info, _ := st.LoadMemoryInfo()

	st.memoryMu.Lock()
	_, err := st.commitMemoryLocked(CommitInitialize, info.MemoryRevision, map[string]any{"events.json": []any{}})
	st.memoryMu.Unlock()
	if err != nil {
		t.Fatalf("first commit: %v", err)
	}

	commitsBefore := readFileOrFail(t, dir, "commits.json")
	eventsBefore := readFileOrFail(t, dir, "events.json")

	// 以过期基线（0）再提交 → 拒绝
	st.memoryMu.Lock()
	_, err = st.commitMemoryLocked(CommitInitialize, 0, map[string]any{"events.json": []map[string]any{{"id": "evt_2"}}})
	st.memoryMu.Unlock()
	if !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale commit error = %v, want ErrRevisionConflict", err)
	}
	if got := readFileOrFail(t, dir, "commits.json"); string(got) != string(commitsBefore) {
		t.Errorf("commits.json changed on rejected commit")
	}
	if got := readFileOrFail(t, dir, "events.json"); string(got) != string(eventsBefore) {
		t.Errorf("events.json changed on rejected commit")
	}

	// 基于最新 revision 重建后重试成功
	info, _ = st.LoadMemoryInfo()
	st.memoryMu.Lock()
	record, err := st.commitMemoryLocked(CommitInitialize, info.MemoryRevision, map[string]any{"events.json": []map[string]any{{"id": "evt_2"}}})
	st.memoryMu.Unlock()
	if err != nil {
		t.Fatalf("rebuilt commit: %v", err)
	}
	if record.TargetMemoryRevision != 2 {
		t.Errorf("target = %d, want 2", record.TargetMemoryRevision)
	}
}

// TestMemoryRecoveryPrepared 覆盖 U13：prepared 记录 + 重启 → 重放、置 committed、meta 对齐 target。
func TestMemoryRecoveryPrepared(t *testing.T) {
	st, dir := newTestStore(t)
	st.memoryMu.Lock()
	record, err := st.commitMemoryLocked(CommitInitialize, 0, map[string]any{"events.json": []map[string]any{{"id": "evt_1"}}})
	st.memoryMu.Unlock()
	if err != nil {
		t.Fatalf("commit: %v", err)
	}

	// 模拟崩溃点 4：目标文件已写、committed 未持久化（手工把状态改回 prepared）
	rewriteCommits(t, dir, func(records []CommitRecord) []CommitRecord {
		for i := range records {
			if records[i].CommitID == record.CommitID {
				records[i].Status = CommitPrepared
				records[i].CompletedAt = time.Time{}
			}
		}
		return records
	})
	// 模拟 meta 未更新（崩溃点 2/3 附加窗口）
	rewriteMeta(t, dir, 0, "")

	// 重新打开 Store → 恢复
	st2, err := New(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	info, err := st2.LoadMemoryInfo()
	if err != nil {
		t.Fatalf("info: %v", err)
	}
	if info.MemoryRevision != 1 || info.LastCommitID != record.CommitID || info.SafeState {
		t.Errorf("info after recovery = %+v", info)
	}
	commits := decodeCommits(t, dir)
	if len(commits) != 1 || commits[0].Status != CommitCommitted {
		t.Errorf("commits after recovery = %+v", commits)
	}
}

// TestMemoryRecoveryCommittedMetaWindow 覆盖 U25：committed 已持久化但 meta 未更新 → 恢复仅对齐 meta、不重放文件。
func TestMemoryRecoveryCommittedMetaWindow(t *testing.T) {
	st, dir := newTestStore(t)
	st.memoryMu.Lock()
	record, err := st.commitMemoryLocked(CommitInitialize, 0, map[string]any{"events.json": []map[string]any{{"id": "evt_1"}}})
	st.memoryMu.Unlock()
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	rewriteMeta(t, dir, 0, "")

	eventsBefore := readFileOrFail(t, dir, "events.json")

	st2, err := New(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	info, err := st2.LoadMemoryInfo()
	if err != nil {
		t.Fatalf("info: %v", err)
	}
	if info.MemoryRevision != record.TargetMemoryRevision || info.LastCommitID != record.CommitID || info.SafeState {
		t.Errorf("info after recovery = %+v", info)
	}
	if got := readFileOrFail(t, dir, "events.json"); string(got) != string(eventsBefore) {
		t.Errorf("events.json replayed on committed recovery (must only align meta)")
	}
}

// TestMemoryRecoveryIdempotent 覆盖 U14：恢复收敛后再次重开 → 数据不变、memoryRevision 不再 +1。
func TestMemoryRecoveryIdempotent(t *testing.T) {
	st, dir := newTestStore(t)
	st.memoryMu.Lock()
	if _, err := st.commitMemoryLocked(CommitInitialize, 0, map[string]any{"events.json": []map[string]any{{"id": "evt_1"}}}); err != nil {
		st.memoryMu.Unlock()
		t.Fatalf("commit: %v", err)
	}
	st.memoryMu.Unlock()
	rewriteMeta(t, dir, 0, "")

	first, err := New(dir)
	if err != nil {
		t.Fatalf("reopen 1: %v", err)
	}
	info1, _ := first.LoadMemoryInfo()

	second, err := New(dir)
	if err != nil {
		t.Fatalf("reopen 2: %v", err)
	}
	info2, _ := second.LoadMemoryInfo()
	if info1.MemoryRevision != info2.MemoryRevision || info1.LastCommitID != info2.LastCommitID {
		t.Errorf("second recovery changed state: %+v vs %+v", info1, info2)
	}
}

// TestMemorySafeStateOnBrokenChain 覆盖 U32：断链/分叉 → 安全态、拒绝记忆写、memoryRevision 不收敛。
func TestMemorySafeStateOnBrokenChain(t *testing.T) {
	st, dir := newTestStore(t)
	st.memoryMu.Lock()
	first, err := st.commitMemoryLocked(CommitInitialize, 0, map[string]any{"events.json": []map[string]any{{"id": "evt_1"}}})
	st.memoryMu.Unlock()
	if err != nil {
		t.Fatalf("commit 1: %v", err)
	}
	// 构造断链：追加一条 basedOn=5（应为 1）的记录
	rewriteCommits(t, dir, func(records []CommitRecord) []CommitRecord {
		return append(records, CommitRecord{
			CommitID: "commit_broken", Type: CommitReconcile, Status: CommitCommitted,
			BasedOnMemoryRevision: 5, TargetMemoryRevision: 6,
			Payload: map[string]json.RawMessage{}, CreatedAt: time.Now().UTC().Add(time.Second),
		})
	})

	st2, err := New(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	info, err := st2.LoadMemoryInfo()
	if err != nil {
		t.Fatalf("info: %v", err)
	}
	if !info.SafeState || info.MemoryRevision != first.TargetMemoryRevision {
		t.Errorf("info = %+v, want safe state with revision %d", info, first.TargetMemoryRevision)
	}

	st2.memoryMu.Lock()
	_, err = st2.commitMemoryLocked(CommitReconcile, info.MemoryRevision, map[string]any{"events.json": []any{}})
	st2.memoryMu.Unlock()
	if !errors.Is(err, ErrMemorySafeState) {
		t.Fatalf("commit in safe state error = %v, want ErrMemorySafeState", err)
	}
}

// TestMemorySafeStateOnCorruptCommits 覆盖 R4：commits.json 解码失败 → 安全态。
func TestMemorySafeStateOnCorruptCommits(t *testing.T) {
	st, dir := newTestStore(t)
	if _, err := st.LoadMemoryInfo(); err != nil {
		t.Fatalf("info: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, memoryDirName, "commits.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatalf("write corrupt commits: %v", err)
	}
	st2, err := New(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	info, err := st2.LoadMemoryInfo()
	if err != nil {
		t.Fatalf("info: %v", err)
	}
	if !info.SafeState {
		t.Errorf("info = %+v, want safe state", info)
	}
}

// ---- 测试辅助：直接读写 memory 文件模拟崩溃窗口 ----

func readFileOrFail(t *testing.T, dir, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, memoryDirName, name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return raw
}

func decodeCommits(t *testing.T, dir string) []CommitRecord {
	t.Helper()
	var commits []CommitRecord
	if err := json.Unmarshal(readFileOrFail(t, dir, "commits.json"), &commits); err != nil {
		t.Fatalf("decode commits.json: %v", err)
	}
	return commits
}

func rewriteCommits(t *testing.T, dir string, mutate func([]CommitRecord) []CommitRecord) {
	t.Helper()
	commits := decodeCommits(t, dir)
	commits = mutate(commits)
	raw, err := json.MarshalIndent(commits, "", "  ")
	if err != nil {
		t.Fatalf("encode commits: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, memoryDirName, "commits.json"), raw, 0o644); err != nil {
		t.Fatalf("write commits: %v", err)
	}
}

func rewriteMeta(t *testing.T, dir string, revision int64, lastCommitID string) {
	t.Helper()
	meta := map[string]any{
		"schemaVersion":  1,
		"memoryRevision": revision,
		"lastCommitId":   lastCommitID,
		"updatedAt":      time.Now().UTC().Format(time.RFC3339),
	}
	raw, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		t.Fatalf("encode meta: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, memoryDirName, "meta.json"), raw, 0o644); err != nil {
		t.Fatalf("write meta: %v", err)
	}
}

package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"novelagent/internal/novel"
)

// resetHook 恢复包级钩子，避免测试间相互污染（chapterOrderHook 为包级全局，注册经互斥保护）。
func resetHook(t *testing.T) {
	t.Helper()
	chapterOrderHookMu.RLock()
	previous := chapterOrderHook
	chapterOrderHookMu.RUnlock()
	t.Cleanup(func() {
		RegisterChapterOrderHook(previous)
	})
}

// TestSaveChapterNoOpShortCircuit 覆盖 U2：无变化保存 → 零写入、版本全不递增、Analysis 不回退。
func TestSaveChapterNoOpShortCircuit(t *testing.T) {
	st, _ := newTestStore(t)
	project, err := st.ImportChapters("书", []novel.Chapter{
		{Title: "一", Content: "天空下着雨。"},
	})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	id := project.Chapters[0].ID
	loaded, err := st.LoadChapter(id)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.Revision != 1 {
		t.Fatalf("initial chapter revision = %d, want 1", loaded.Revision)
	}

	saved, updated, err := st.SaveChapter(novel.Chapter{ID: id, Title: "一", Content: "天空下着雨。"})
	if err != nil {
		t.Fatalf("no-op save: %v", err)
	}
	if saved.Revision != 1 {
		t.Errorf("chapter revision after no-op = %d, want 1", saved.Revision)
	}
	if !saved.UpdatedAt.Equal(loaded.UpdatedAt) {
		t.Errorf("chapter UpdatedAt changed on no-op: %v -> %v", loaded.UpdatedAt, saved.UpdatedAt)
	}
	if updated.Revision != project.Revision {
		t.Errorf("project revision changed on no-op: %d -> %d", project.Revision, updated.Revision)
	}
	if updated.Analysis.Status != project.Analysis.Status || updated.Analysis.AnalyzedCount != project.Analysis.AnalyzedCount {
		t.Errorf("analysis state changed on no-op: %+v -> %+v", project.Analysis, updated.Analysis)
	}
}

// TestSaveChapterChangeIncrements 覆盖 U1/U3：内容或标题实际变化 → Chapter.Revision+1、Analysis 回退 pending、Project.Revision+1。
func TestSaveChapterChangeIncrements(t *testing.T) {
	st, _ := newTestStore(t)
	project, err := st.ImportChapters("书", []novel.Chapter{
		{Title: "一", Content: "天空下着雨。"},
	})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	id := project.Chapters[0].ID

	// U1：内容变化
	saved, updated, err := st.SaveChapter(novel.Chapter{ID: id, Title: "一", Content: "雨停了。"})
	if err != nil {
		t.Fatalf("content save: %v", err)
	}
	if saved.Revision != 2 {
		t.Errorf("revision after content change = %d, want 2", saved.Revision)
	}
	if updated.Analysis.Status != novel.AnalysisPending {
		t.Errorf("analysis status = %v, want pending", updated.Analysis.Status)
	}
	if updated.Revision != project.Revision+1 {
		t.Errorf("project revision = %d, want %d", updated.Revision, project.Revision+1)
	}

	// U3：仅标题变化也递增（决策 8）
	after, err := st.LoadChapter(id)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	saved, _, err = st.SaveChapter(novel.Chapter{ID: id, Title: "壹", Content: after.Content})
	if err != nil {
		t.Fatalf("title save: %v", err)
	}
	if saved.Revision != 3 {
		t.Errorf("revision after title change = %d, want 3", saved.Revision)
	}
	if saved.Title != "壹" {
		t.Errorf("title = %q, want 壹", saved.Title)
	}
}

// TestSaveChapterLegacyRevisionCompat 覆盖 U4：旧文件无 revision 字段 → 加载置 1，首次变化保存 → 2（幂等）。
func TestSaveChapterLegacyRevisionCompat(t *testing.T) {
	st, dir := newTestStore(t)
	project, err := st.ImportChapters("书", []novel.Chapter{{Title: "一", Content: "天空下着雨。"}})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	id := project.Chapters[0].ID

	// 模拟旧文件：去掉 revision 字段（含 meta 缓存）
	chapterPath := filepath.Join(dir, chaptersDirName, id+".json")
	raw, err := os.ReadFile(chapterPath)
	if err != nil {
		t.Fatalf("read chapter file: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("decode chapter file: %v", err)
	}
	delete(payload, "revision")
	legacy, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("encode legacy chapter: %v", err)
	}
	if err := os.WriteFile(chapterPath, legacy, 0o644); err != nil {
		t.Fatalf("write legacy chapter: %v", err)
	}

	loaded, err := st.LoadChapter(id)
	if err != nil {
		t.Fatalf("load legacy: %v", err)
	}
	if loaded.Revision != 1 {
		t.Errorf("legacy chapter revision = %d, want 1", loaded.Revision)
	}

	saved, _, err := st.SaveChapter(novel.Chapter{ID: id, Title: "一", Content: "雨停了。"})
	if err != nil {
		t.Fatalf("save after legacy load: %v", err)
	}
	if saved.Revision != 2 {
		t.Errorf("revision after first change = %d, want 2", saved.Revision)
	}
}

// TestLoadRepairsMetaFromChapterFile 覆盖真源规则：meta 缓存损坏 → 加载以 chapter 文件修复（不递增任何 Revision）。
func TestLoadRepairsMetaFromChapterFile(t *testing.T) {
	st, dir := newTestStore(t)
	project, err := st.ImportChapters("书", []novel.Chapter{{Title: "一", Content: "天空下着雨。"}})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	id := project.Chapters[0].ID
	if _, _, err := st.SaveChapter(novel.Chapter{ID: id, Title: "一", Content: "雨停了。"}); err != nil {
		t.Fatalf("save: %v", err)
	}

	// 构造 meta 残留（模拟"章节文件写成功、project.json 写失败"的旧残留）
	projectPath := filepath.Join(dir, "project.json")
	raw, err := os.ReadFile(projectPath)
	if err != nil {
		t.Fatalf("read project: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("decode project: %v", err)
	}
	chapters := doc["chapters"].([]any)
	meta := chapters[0].(map[string]any)
	meta["title"] = "过期标题"
	meta["revision"] = float64(0)
	staleProjectRevision := int64(doc["revision"].(float64))
	legacy, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("encode project: %v", err)
	}
	if err := os.WriteFile(projectPath, legacy, 0o644); err != nil {
		t.Fatalf("write project: %v", err)
	}

	chapter, err := st.LoadChapter(id)
	if err != nil {
		t.Fatalf("load with stale meta: %v", err)
	}
	if chapter.Title != "一" || chapter.Revision != 2 {
		t.Errorf("chapter from file = %q / R%d, want 一 / R2", chapter.Title, chapter.Revision)
	}

	repaired, err := st.LoadProject()
	if err != nil {
		t.Fatalf("reload project: %v", err)
	}
	if repaired.Chapters[0].Title != "一" || repaired.Chapters[0].Revision != 2 {
		t.Errorf("meta not repaired: %+v", repaired.Chapters[0])
	}
	if repaired.Revision != staleProjectRevision {
		t.Errorf("project revision changed on meta repair: %v -> %d", staleProjectRevision, repaired.Revision)
	}
}

// TestChapterOrderHookFiresOnDelete 覆盖 U17：删除中间章 → 回调收到 old/new ID→Index 快照。
func TestChapterOrderHookFiresOnDelete(t *testing.T) {
	resetHook(t)
	st, _ := newTestStore(t)
	project, err := st.ImportChapters("书", []novel.Chapter{
		{Title: "一", Content: "一。"},
		{Title: "二", Content: "二。"},
		{Title: "三", Content: "三。"},
	})
	if err != nil {
		t.Fatalf("import: %v", err)
	}

	var gotOld, gotNew map[string]int
	called := false
	RegisterChapterOrderHook(func(oldIndexes, newIndexes map[string]int) error {
		called = true
		gotOld = oldIndexes
		gotNew = newIndexes
		return nil
	})

	target := project.Chapters[0].ID
	if _, err := st.DeleteChapter(target); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if !called {
		t.Fatalf("hook not called on delete")
	}
	if gotOld[target] != 1 {
		t.Errorf("old index of deleted = %d, want 1", gotOld[target])
	}
	remaining := project.Chapters[1].ID
	if gotNew[remaining] != 1 {
		t.Errorf("new index after delete = %d, want 1", gotNew[remaining])
	}
}

// TestChapterOrderHookNoOpWhenUnregistered 覆盖 U17 零开销路径：未注册回调 → 章节操作不受影响。
func TestChapterOrderHookNoOpWhenUnregistered(t *testing.T) {
	resetHook(t)
	RegisterChapterOrderHook(nil)
	st, _ := newTestStore(t)
	project, err := st.ImportChapters("书", []novel.Chapter{{Title: "一", Content: "一。"}, {Title: "二", Content: "二。"}})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	updated, err := st.DeleteChapter(project.Chapters[0].ID)
	if err != nil {
		t.Fatalf("delete without hook: %v", err)
	}
	if len(updated.Chapters) != 1 {
		t.Errorf("chapters after delete = %d, want 1", len(updated.Chapters))
	}
}

// TestChapterOrderHookFailureNotBlocking 覆盖 U34：回调 panic → 仅记日志，章节操作成功返回。
func TestChapterOrderHookFailureNotBlocking(t *testing.T) {
	resetHook(t)
	st, _ := newTestStore(t)
	project, err := st.ImportChapters("书", []novel.Chapter{{Title: "一", Content: "一。"}, {Title: "二", Content: "二。"}})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	RegisterChapterOrderHook(func(oldIndexes, newIndexes map[string]int) error {
		panic("memory module simulated failure")
	})
	updated, err := st.DeleteChapter(project.Chapters[0].ID)
	if err != nil {
		t.Fatalf("delete must not be blocked by hook failure: %v", err)
	}
	if len(updated.Chapters) != 1 {
		t.Errorf("chapters after delete = %d, want 1", len(updated.Chapters))
	}
}

// TestChapterOrderHookErrorNotBlocking 覆盖 U34（R10）：回调返回 error → 仅记日志，章节操作成功返回。
func TestChapterOrderHookErrorNotBlocking(t *testing.T) {
	resetHook(t)
	st, _ := newTestStore(t)
	project, err := st.ImportChapters("书", []novel.Chapter{{Title: "一", Content: "一。"}, {Title: "二", Content: "二。"}})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	RegisterChapterOrderHook(func(oldIndexes, newIndexes map[string]int) error {
		return errors.New("memory module simulated error")
	})
	updated, err := st.DeleteChapter(project.Chapters[0].ID)
	if err != nil {
		t.Fatalf("delete must not be blocked by hook error: %v", err)
	}
	if len(updated.Chapters) != 1 {
		t.Errorf("chapters after delete = %d, want 1", len(updated.Chapters))
	}
}

// TestChapterOrderHookRegisterRace 覆盖 R11 并发保护：运行期注册与章节写操作并发无数据竞争（配合 -race 验证）。
func TestChapterOrderHookRegisterRace(t *testing.T) {
	resetHook(t)
	st, _ := newTestStore(t)
	RegisterChapterOrderHook(func(oldIndexes, newIndexes map[string]int) error { return nil })

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 50; i++ {
			RegisterChapterOrderHook(func(oldIndexes, newIndexes map[string]int) error { return nil })
		}
	}()
	for i := 0; i < 50; i++ {
		project, err := st.ImportChapters("书", []novel.Chapter{
			{Title: "一", Content: "一。"},
			{Title: "二", Content: "二。"},
		})
		if err != nil {
			t.Fatalf("import: %v", err)
		}
		if _, err := st.DeleteChapter(project.Chapters[0].ID); err != nil {
			t.Fatalf("delete: %v", err)
		}
	}
	<-done
}

// TestSaveChapterNoOpRepairsStaleMeta 覆盖 U35（R11）：章节文件新、meta 旧 → 相同内容保存触发短路，
// 但短路路径仍以文件值修复 meta；所有 Revision 不递增。
func TestSaveChapterNoOpRepairsStaleMeta(t *testing.T) {
	st, dir := newTestStore(t)
	project, err := st.ImportChapters("书", []novel.Chapter{{Title: "一", Content: "天空下着雨。"}})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	id := project.Chapters[0].ID
	if _, _, err := st.SaveChapter(novel.Chapter{ID: id, Title: "一", Content: "雨停了。"}); err != nil {
		t.Fatalf("save: %v", err)
	}

	// 构造残留：章节文件已到 R2，meta 仍停在旧值（模拟"章节写成功、project.json 写失败"）
	projectPath := filepath.Join(dir, "project.json")
	raw, err := os.ReadFile(projectPath)
	if err != nil {
		t.Fatalf("read project: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("decode project: %v", err)
	}
	meta := doc["chapters"].([]any)[0].(map[string]any)
	meta["title"] = "过期标题"
	meta["revision"] = float64(1)
	staleProjectRevision := int64(doc["revision"].(float64))
	legacy, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("encode project: %v", err)
	}
	if err := os.WriteFile(projectPath, legacy, 0o644); err != nil {
		t.Fatalf("write project: %v", err)
	}

	// 客户端经 LoadChapter（返回文件真源）重新保存相同内容 → 走短路路径
	saved, updated, err := st.SaveChapter(novel.Chapter{ID: id, Title: "一", Content: "雨停了。"})
	if err != nil {
		t.Fatalf("no-op save with stale meta: %v", err)
	}
	if saved.Revision != 2 || saved.Title != "一" {
		t.Errorf("saved chapter = %q / R%d, want 一 / R2（以文件为真源）", saved.Title, saved.Revision)
	}
	if updated.Revision != staleProjectRevision {
		t.Errorf("project revision changed on no-op: %d -> %d", staleProjectRevision, updated.Revision)
	}

	repaired, err := st.LoadProject()
	if err != nil {
		t.Fatalf("reload project: %v", err)
	}
	if repaired.Chapters[0].Title != "一" || repaired.Chapters[0].Revision != 2 {
		t.Errorf("meta not repaired on no-op path: %+v", repaired.Chapters[0])
	}
}

// TestAppendChapterRevisionBaseline 覆盖新建章节 Revision=1。
func TestAppendChapterRevisionBaseline(t *testing.T) {
	st, _ := newTestStore(t)
	if _, err := st.ImportChapters("书", []novel.Chapter{{Title: "一", Content: "一。"}}); err != nil {
		t.Fatalf("import: %v", err)
	}
	appended, project, err := st.AppendChapter("二", "二。")
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	if appended.Revision != 1 {
		t.Errorf("appended revision = %d, want 1", appended.Revision)
	}
	loaded, err := st.LoadChapter(appended.ID)
	if err != nil {
		t.Fatalf("load appended: %v", err)
	}
	if loaded.Revision != 1 {
		t.Errorf("loaded appended revision = %d, want 1", loaded.Revision)
	}
	if project.Chapters[1].Revision != 1 {
		t.Errorf("meta revision = %d, want 1", project.Chapters[1].Revision)
	}
}

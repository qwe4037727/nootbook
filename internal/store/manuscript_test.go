package store

import (
	"errors"
	"strings"
	"testing"
	"time"

	"novelagent/internal/novel"
)

func TestAppendImportKeepsExistingBook(t *testing.T) {
	st, _ := newTestStore(t)
	if _, err := st.ImportChapters("原名", []novel.Chapter{
		{Title: "第一章", Content: "旧内容一。"},
		{Title: "第二章", Content: "旧内容二。"},
	}); err != nil {
		t.Fatalf("ImportChapters: %v", err)
	}
	before, err := st.LoadProject()
	if err != nil || len(before.Chapters) != 2 {
		t.Fatalf("initial project = %+v err=%v", before, err)
	}

	project, appended, err := st.AppendImport("新书名", []novel.Chapter{
		{Title: "第三章", Content: "追加内容。"},
	})
	if err != nil {
		t.Fatalf("AppendImport: %v", err)
	}
	if len(appended) != 1 || len(project.Chapters) != 3 {
		t.Fatalf("appended=%d chapters=%d", len(appended), len(project.Chapters))
	}
	if project.Title != "原名" {
		t.Fatalf("title changed to %q", project.Title)
	}
	chapter, err := st.LoadChapter(appended[0].ID)
	if err != nil || chapter.Content != "追加内容。" || chapter.Index != 3 {
		t.Fatalf("appended chapter = %+v err=%v", chapter, err)
	}
	first, err := st.LoadChapter(project.Chapters[0].ID)
	if err != nil || first.Content != "旧内容一。" {
		t.Fatalf("existing chapter changed: %+v err=%v", first, err)
	}
}

func TestReorderMergeSplitChapters(t *testing.T) {
	st, _ := newTestStore(t)
	project, err := st.ImportChapters("书", []novel.Chapter{
		{Title: "一", Content: "甲甲甲。"},
		{Title: "二", Content: "乙乙乙。"},
		{Title: "三", Content: "丙丙丙。"},
	})
	if err != nil {
		t.Fatalf("ImportChapters: %v", err)
	}
	ids := []string{project.Chapters[2].ID, project.Chapters[0].ID, project.Chapters[1].ID}
	reordered, err := st.ReorderChapters(ids)
	if err != nil {
		t.Fatalf("ReorderChapters: %v", err)
	}
	for i, id := range ids {
		if reordered.Chapters[i].ID != id || reordered.Chapters[i].Index != i+1 {
			t.Fatalf("chapter %d = %+v", i, reordered.Chapters[i])
		}
	}
	if _, err := st.ReorderChapters(ids[:2]); !errors.Is(err, ErrInvalidChapterOrder) {
		t.Fatalf("short order err = %v", err)
	}

	merged, mergedProject, err := st.MergeChapters([]string{ids[1], ids[2]}, "合章")
	if err != nil {
		t.Fatalf("MergeChapters: %v", err)
	}
	if merged.Title != "合章" || !strings.Contains(merged.Content, "甲甲甲") || !strings.Contains(merged.Content, "乙乙乙") {
		t.Fatalf("merged = %+v", merged)
	}
	if len(mergedProject.Chapters) != 2 {
		t.Fatalf("after merge chapters = %d", len(mergedProject.Chapters))
	}
	for _, sourceID := range []string{ids[1], ids[2]} {
		versions, err := st.ListChapterVersions(sourceID, 10)
		if err != nil || len(versions) == 0 || versions[0].Reason != ChapterVersionMerge {
			t.Fatalf("merge source %s versions = %+v, err = %v", sourceID, versions, err)
		}
	}

	first, second, splitProject, err := st.SplitChapter(merged.ID, 4, "前半", "后半")
	if err != nil {
		t.Fatalf("SplitChapter: %v", err)
	}
	if first.Title != "前半" || second.Title != "后半" {
		t.Fatalf("split titles = %q / %q", first.Title, second.Title)
	}
	if len(splitProject.Chapters) != 3 || second.Index != first.Index+1 {
		t.Fatalf("split project = %+v", splitProject.Chapters)
	}
	versions, err := st.ListChapterVersions(merged.ID, 10)
	if err != nil || len(versions) == 0 || versions[0].Reason != ChapterVersionSplit || versions[0].Content != merged.Content {
		t.Fatalf("split source versions = %+v, err = %v", versions, err)
	}
	if _, _, _, err := st.SplitChapter(first.ID, 0, "", ""); !errors.Is(err, ErrInvalidSplit) {
		t.Fatalf("split at 0 err = %v", err)
	}
}

func TestChapterVersionsAndRestore(t *testing.T) {
	st, _ := newTestStore(t)
	project, err := st.ImportChapters("书", []novel.Chapter{{Title: "一", Content: "第一版内容。"}})
	if err != nil {
		t.Fatalf("ImportChapters: %v", err)
	}
	chapterID := project.Chapters[0].ID
	chapter, err := st.LoadChapter(chapterID)
	if err != nil {
		t.Fatalf("LoadChapter: %v", err)
	}
	chapter.Content = "第二版内容。"
	if _, _, err := st.SaveChapter(chapter); err != nil {
		t.Fatalf("SaveChapter: %v", err)
	}
	versions, err := st.ListChapterVersions(chapterID, 10)
	if err != nil || len(versions) == 0 {
		t.Fatalf("versions = %+v err=%v", versions, err)
	}

	restored, _, autoSnapshot, err := st.RestoreChapterVersion(chapterID, versions[0].Revision)
	if err != nil {
		t.Fatalf("RestoreChapterVersion: %v", err)
	}
	if autoSnapshot == "" || restored.Content != "第一版内容。" {
		t.Fatalf("restored = %+v snapshot=%q", restored, autoSnapshot)
	}
	if _, _, _, err := st.RestoreChapterVersion(chapterID, 999); !errors.Is(err, ErrChapterVersionNotFound) {
		t.Fatalf("missing version err = %v", err)
	}
}

func TestBookSnapshotRestore(t *testing.T) {
	st, _ := newTestStore(t)
	project, err := st.ImportChapters("书", []novel.Chapter{
		{Title: "一", Content: "快照前。"},
		{Title: "二", Content: "第二段。"},
	})
	if err != nil {
		t.Fatalf("ImportChapters: %v", err)
	}
	snapshot, err := st.CreateSnapshot("起点", "manual")
	if err != nil {
		t.Fatalf("CreateSnapshot: %v", err)
	}
	if snapshot.ChapterCount != 2 || snapshot.CharCount == 0 {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	detail, err := st.GetSnapshot(snapshot.ID)
	if err != nil || len(detail.Chapters) != 2 {
		t.Fatalf("snapshot detail = %+v err=%v", detail, err)
	}

	first, err := st.LoadChapter(project.Chapters[0].ID)
	if err != nil {
		t.Fatalf("LoadChapter: %v", err)
	}
	first.Content = "快照后。"
	if _, _, err := st.SaveChapter(first); err != nil {
		t.Fatalf("SaveChapter: %v", err)
	}
	appended, _, err := st.AppendChapter("三", "新增章。")
	if err != nil {
		t.Fatalf("AppendChapter: %v", err)
	}

	restoredProject, autoSnapshot, err := st.RestoreSnapshot(snapshot.ID)
	if err != nil {
		t.Fatalf("RestoreSnapshot: %v", err)
	}
	if autoSnapshot == "" || len(restoredProject.Chapters) != 2 {
		t.Fatalf("restore auto=%q chapters=%d", autoSnapshot, len(restoredProject.Chapters))
	}
	restoredFirst, err := st.LoadChapter(project.Chapters[0].ID)
	if err != nil || restoredFirst.Content != "快照前。" {
		t.Fatalf("restored first = %+v err=%v", restoredFirst, err)
	}
	if _, err := st.LoadChapter(appended.ID); !errors.Is(err, ErrChapterNotFound) {
		t.Fatalf("appended chapter should be removed after restore, err = %v", err)
	}
}

func TestCostSummaryAndAuditFilter(t *testing.T) {
	st, _ := newTestStore(t)
	if err := st.SaveAgentReport(AgentReportRecord{
		ID: "ar_1", AgentKey: "plot", AgentName: "A2", ChapterID: "ch_1", ChapterIndex: 2,
		Severity: "warning", Summary: "剧情停滞",
		Findings: []byte(`[{"type":"plot_static","severity":"warning","entryId":"c_hero"}]`),
	}); err != nil {
		t.Fatalf("SaveAgentReport: %v", err)
	}
	if err := st.SaveAgentReport(AgentReportRecord{
		ID: "ar_2", AgentKey: "style", AgentName: "A5", ChapterID: "ch_2", ChapterIndex: 9,
		Severity: "info", Summary: "文风偏移", Findings: []byte(`[]`),
	}); err != nil {
		t.Fatalf("SaveAgentReport: %v", err)
	}
	hits, err := st.ListAgentReportsFiltered(AgentReportFilter{EntryID: "c_hero", Limit: 10})
	if err != nil || len(hits) != 1 || hits[0].ID != "ar_1" {
		t.Fatalf("entry filter = %+v err=%v", hits, err)
	}
	hits, err = st.ListAgentReportsFiltered(AgentReportFilter{ChapterIndexMin: 5, ChapterIndexMax: 10, Limit: 10})
	if err != nil || len(hits) != 1 || hits[0].ID != "ar_2" {
		t.Fatalf("index filter = %+v err=%v", hits, err)
	}

	if err := st.RecordAgentRun(AgentRunRecord{
		ID: "run_cost", ProjectID: "p", AgentKey: "plot", AgentName: "A2", EventID: "ev", EventType: "chapter_saved",
		Permission: "observe", Status: "done", StartedAt: time.Now().UTC(),
		PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15,
	}); err != nil {
		t.Fatalf("RecordAgentRun: %v", err)
	}
	if _, err := st.CreateTask(novel.Task{ID: "task_cost", Kind: novel.TaskKindAgentRun, Description: "x"}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	summary, err := st.CostSummary(time.Time{})
	if err != nil {
		t.Fatalf("CostSummary: %v", err)
	}
	if summary.Total.TotalTokens != 15 || summary.TaskCounts[string(novel.TaskPending)] != 1 {
		t.Fatalf("summary = %+v", summary)
	}
	since, err := st.CostSummary(time.Now().UTC().Add(-time.Hour))
	if err != nil {
		t.Fatalf("CostSummary since: %v", err)
	}
	if since.Total.TotalTokens != 15 || since.Total.Calls != 1 {
		t.Fatalf("summary since = %+v", since)
	}
}

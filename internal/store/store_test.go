package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"novelagent/internal/novel"
)

func newTestStore(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	st, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return st, dir
}

func chapterFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(dir, chaptersDirName))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatalf("read chapters dir: %v", err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

func TestImportAndLoadChapters(t *testing.T) {
	st, dir := newTestStore(t)

	project, err := st.ImportChapters("无边悔恨", []novel.Chapter{
		{Title: "初始", Content: "天空下着雨。"},
		{Title: "", Content: "他睁开眼。"},
		{Title: "空章", Content: "   "},
	})
	if err != nil {
		t.Fatalf("ImportChapters: %v", err)
	}
	if len(project.Chapters) != 2 {
		t.Fatalf("len(project.Chapters) = %d, want 2", len(project.Chapters))
	}
	if project.Chapters[1].Title != "第2章" {
		t.Errorf("default title = %q", project.Chapters[1].Title)
	}
	if project.Analysis.Status != novel.AnalysisPending || project.Analysis.TotalCount != 2 {
		t.Errorf("analysis state = %+v", project.Analysis)
	}
	if project.Revision != 1 {
		t.Errorf("revision = %d, want 1", project.Revision)
	}
	if files := chapterFiles(t, dir); len(files) != 2 {
		t.Errorf("chapter files = %v, want 2", files)
	}

	chapters, err := st.LoadChapters()
	if err != nil {
		t.Fatalf("LoadChapters: %v", err)
	}
	if len(chapters) != 2 || chapters[0].Content != "天空下着雨。" || chapters[1].Index != 2 {
		t.Errorf("chapters = %+v", chapters)
	}

	loaded, err := st.LoadChapter(project.Chapters[0].ID)
	if err != nil {
		t.Fatalf("LoadChapter: %v", err)
	}
	if loaded.Title != "初始" || loaded.Index != 1 {
		t.Errorf("loaded chapter = %+v", loaded)
	}
	if _, err := st.LoadChapter("missing"); !errors.Is(err, ErrChapterNotFound) {
		t.Errorf("LoadChapter(missing) error = %v", err)
	}
	if _, err := st.LoadChapter("../escape"); !errors.Is(err, ErrInvalidChapter) {
		t.Errorf("LoadChapter(invalid) error = %v", err)
	}

	knowledge, err := st.LoadKnowledge()
	if err != nil {
		t.Fatalf("LoadKnowledge: %v", err)
	}
	if len(knowledge.Characters) != 0 || len(knowledge.World) != 0 || len(knowledge.Summaries) != 0 {
		t.Errorf("import should reset knowledge: %+v", knowledge)
	}
}

func TestImportReplacesPreviousChapters(t *testing.T) {
	st, dir := newTestStore(t)

	if _, err := st.ImportChapters("旧书", []novel.Chapter{
		{Title: "一", Content: "旧一。"},
		{Title: "二", Content: "旧二。"},
		{Title: "三", Content: "旧三。"},
	}); err != nil {
		t.Fatalf("first import: %v", err)
	}
	project, err := st.ImportChapters("新书", []novel.Chapter{
		{Title: "新的", Content: "新内容。"},
	})
	if err != nil {
		t.Fatalf("second import: %v", err)
	}
	if project.Title != "新书" || len(project.Chapters) != 1 {
		t.Errorf("project after reimport = %q / %d chapters", project.Title, len(project.Chapters))
	}
	if project.Revision != 2 {
		t.Errorf("revision = %d, want 2", project.Revision)
	}
	if files := chapterFiles(t, dir); len(files) != 1 {
		t.Errorf("stale chapter files remain: %v", files)
	}
}

func TestSaveChapterInvalidatesAnalysis(t *testing.T) {
	st, _ := newTestStore(t)
	project, err := st.ImportChapters("书", []novel.Chapter{
		{Title: "一", Content: "一。"},
		{Title: "二", Content: "二。"},
		{Title: "三", Content: "三。"},
	})
	if err != nil {
		t.Fatalf("ImportChapters: %v", err)
	}
	knowledge := novel.NewKnowledge()
	knowledge.Overview = "总览"
	if _, ok, err := st.CommitAnalysis(project.Revision, 3, knowledge); err != nil || !ok {
		t.Fatalf("CommitAnalysis = ok %v, err %v", ok, err)
	}
	project, err = st.LoadProject()
	if err != nil {
		t.Fatalf("LoadProject: %v", err)
	}
	project, ok, err := st.FinishAnalysis(project.Revision, knowledge)
	if err != nil || !ok {
		t.Fatalf("FinishAnalysis = ok %v, err %v", ok, err)
	}
	if project.Analysis.Status != novel.AnalysisReady {
		t.Fatalf("status = %q, want ready", project.Analysis.Status)
	}

	edit := project.Chapters[0]
	saved, updated, err := st.SaveChapter(novel.Chapter{
		ID:      edit.ID,
		Title:   "一（改）",
		Content: "改过的第一段。",
	})
	if err != nil {
		t.Fatalf("SaveChapter: %v", err)
	}
	if saved.Title != "一（改）" || saved.CharCount != novel.CountChars("改过的第一段。") {
		t.Errorf("saved chapter = %+v", saved)
	}
	if updated.Analysis.AnalyzedCount != 0 || updated.Analysis.Status != novel.AnalysisPending {
		t.Errorf("analysis after edit = %+v", updated.Analysis)
	}
	if updated.Revision != project.Revision+1 {
		t.Errorf("revision = %d, want %d", updated.Revision, project.Revision+1)
	}

	reloaded, err := st.LoadChapter(edit.ID)
	if err != nil {
		t.Fatalf("LoadChapter: %v", err)
	}
	if reloaded.Content != "改过的第一段。" || reloaded.Title != "一（改）" {
		t.Errorf("chapter was not persisted: %+v", reloaded)
	}
}

func TestAppendAndDeleteChapter(t *testing.T) {
	st, _ := newTestStore(t)
	project, err := st.ImportChapters("书", []novel.Chapter{
		{Title: "一", Content: "一。"},
		{Title: "二", Content: "二。"},
	})
	if err != nil {
		t.Fatalf("ImportChapters: %v", err)
	}

	chapter, updated, err := st.AppendChapter("三", "三。")
	if err != nil {
		t.Fatalf("AppendChapter: %v", err)
	}
	if chapter.Index != 3 || updated.Analysis.TotalCount != 3 || updated.Analysis.Status != novel.AnalysisPending {
		t.Errorf("append result = chapter %+v, analysis %+v", chapter, updated.Analysis)
	}
	if updated.Revision != project.Revision+1 {
		t.Errorf("revision = %d", updated.Revision)
	}

	updated, err = st.DeleteChapter(project.Chapters[0].ID)
	if err != nil {
		t.Fatalf("DeleteChapter: %v", err)
	}
	if len(updated.Chapters) != 2 {
		t.Fatalf("len(chapters) = %d, want 2", len(updated.Chapters))
	}
	if updated.Chapters[0].Index != 1 || updated.Chapters[0].Title != "二" {
		t.Errorf("chapters were not reindexed: %+v", updated.Chapters)
	}
	if _, err := st.LoadChapter(project.Chapters[0].ID); !errors.Is(err, ErrChapterNotFound) {
		t.Errorf("deleted chapter load error = %v", err)
	}
	if _, err := st.DeleteChapter("missing"); !errors.Is(err, ErrChapterNotFound) {
		t.Errorf("DeleteChapter(missing) error = %v", err)
	}
}

func TestAnalysisRevisionGuard(t *testing.T) {
	st, _ := newTestStore(t)
	project, err := st.ImportChapters("书", []novel.Chapter{
		{Title: "一", Content: "一。"},
	})
	if err != nil {
		t.Fatalf("ImportChapters: %v", err)
	}
	knowledge := novel.NewKnowledge()
	knowledge.Overview = "新总览"

	if _, ok, err := st.CommitAnalysis(project.Revision-1, 1, knowledge); err != nil || ok {
		t.Fatalf("stale commit = ok %v, err %v; want discarded", ok, err)
	}
	if got, err := st.LoadKnowledge(); err != nil || got.Overview != "" {
		t.Errorf("stale commit wrote knowledge: %+v, err %v", got, err)
	}

	updated, ok, err := st.CommitAnalysis(project.Revision, 1, knowledge)
	if err != nil || !ok {
		t.Fatalf("CommitAnalysis = ok %v, err %v", ok, err)
	}
	if updated.Analysis.Status != novel.AnalysisRunning || updated.Analysis.AnalyzedCount != 1 {
		t.Errorf("analysis after commit = %+v", updated.Analysis)
	}
	if _, ok, err := st.FinishAnalysis(project.Revision-1, knowledge); err != nil || ok {
		t.Fatalf("stale finish = ok %v, err %v; want discarded", ok, err)
	}
	updated, ok, err = st.FinishAnalysis(updated.Revision, knowledge)
	if err != nil || !ok {
		t.Fatalf("FinishAnalysis = ok %v, err %v", ok, err)
	}
	if updated.Analysis.Status != novel.AnalysisReady || updated.Analysis.AnalyzedCount != 1 {
		t.Errorf("analysis after finish = %+v", updated.Analysis)
	}
	if got, err := st.LoadKnowledge(); err != nil || got.Overview != "新总览" {
		t.Errorf("knowledge = %+v, err %v", got, err)
	}

	if _, ok, err := st.FailAnalysis(project.Revision-1, "旧失败"); err != nil || ok {
		t.Fatalf("stale failure = ok %v, err %v; want discarded", ok, err)
	}
}

func TestConfigRoundTrip(t *testing.T) {
	st, _ := newTestStore(t)

	cfg, err := st.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.ContextChars != defaultContextChars {
		t.Errorf("default contextChars = %d", cfg.ContextChars)
	}

	saved, err := st.SaveConfig(Config{
		BaseURL:      " https://api.example.com/v1/ ",
		Model:        " model ",
		APIKey:       " sk-secret ",
		ContextChars: 10,
	})
	if err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	if saved.BaseURL != "https://api.example.com/v1" || saved.Model != "model" || saved.APIKey != "sk-secret" {
		t.Errorf("normalized config = %+v", saved)
	}
	if saved.ContextChars != minContextChars {
		t.Errorf("contextChars = %d, want %d", saved.ContextChars, minContextChars)
	}

	loaded, err := st.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if loaded != saved {
		t.Errorf("loaded config = %+v, want %+v", loaded, saved)
	}
}

func TestMigrateLegacyProject(t *testing.T) {
	dir := t.TempDir()
	legacy := map[string]any{
		"title":      "旧书",
		"style":      "第三人称，短句。",
		"manuscript": "第一章 起\n\n旧正文从这里开始。\n\n第二章 承\n\n后面还有一段。",
		"characters": []map[string]any{{"name": "林砚", "role": "捕快", "personality": "寡言"}},
		"world":      []map[string]any{{"title": "灵气", "content": "三年前复苏。"}},
	}
	data, err := json.MarshalIndent(legacy, "", "  ")
	if err != nil {
		t.Fatalf("encode legacy: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "project.json"), data, 0o644); err != nil {
		t.Fatalf("write legacy project: %v", err)
	}

	st, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	project, err := st.LoadProject()
	if err != nil {
		t.Fatalf("LoadProject: %v", err)
	}
	if project.Title != "旧书" || len(project.Chapters) != 2 {
		t.Fatalf("migrated project = %q / %d chapters", project.Title, len(project.Chapters))
	}
	if project.Analysis.Status != novel.AnalysisPending || project.Analysis.TotalCount != 2 {
		t.Errorf("analysis = %+v", project.Analysis)
	}
	if _, err := os.Stat(filepath.Join(dir, "project.v1.backup.json")); err != nil {
		t.Errorf("legacy backup missing: %v", err)
	}
	knowledge, err := st.LoadKnowledge()
	if err != nil {
		t.Fatalf("LoadKnowledge: %v", err)
	}
	if knowledge.Style.Notes != "第三人称，短句。" {
		t.Errorf("style notes = %q", knowledge.Style.Notes)
	}
	if len(knowledge.Characters) != 1 || knowledge.Characters[0].Name != "林砚" || knowledge.Characters[0].Confidence != 0.9 {
		t.Errorf("characters = %+v", knowledge.Characters)
	}
	if len(knowledge.World) != 1 || knowledge.World[0].Title != "灵气" || knowledge.World[0].Confidence != 0.8 {
		t.Errorf("world = %+v", knowledge.World)
	}

	if _, err := New(dir); err != nil {
		t.Fatalf("second New: %v", err)
	}
	again, err := st.LoadProject()
	if err != nil {
		t.Fatalf("LoadProject after second New: %v", err)
	}
	if again.Revision != project.Revision || len(again.Chapters) != len(project.Chapters) {
		t.Errorf("migration is not idempotent: %+v", again)
	}
	if got := strings.TrimSpace(again.Title); got != "旧书" {
		t.Errorf("title after second New = %q", got)
	}
}

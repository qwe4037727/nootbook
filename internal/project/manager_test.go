package project

import (
	"os"
	"path/filepath"
	"testing"

	"novelagent/internal/novel"
	"novelagent/internal/store"
)

func TestDefaultProjectReusesLegacyData(t *testing.T) {
	root := t.TempDir()
	legacy, err := store.New(root)
	if err != nil {
		t.Fatalf("legacy store: %v", err)
	}
	if _, err := legacy.ImportChapters("旧书", []novel.Chapter{
		{Title: "一", Content: "第一章正文。"},
		{Title: "二", Content: "第二章正文。"},
	}); err != nil {
		t.Fatalf("import legacy: %v", err)
	}
	if _, err := legacy.SaveConfig(store.Config{BaseURL: "http://localhost:1", Model: "legacy-model"}); err != nil {
		t.Fatalf("save legacy config: %v", err)
	}
	legacyProject, err := legacy.LoadProject()
	if err != nil {
		t.Fatalf("load legacy project: %v", err)
	}
	knowledge := novel.NewKnowledge()
	knowledge.Characters = append(knowledge.Characters, novel.Character{ID: "c_legacy", Name: "旧角色"})
	if _, _, err := legacy.CommitAnalysis(legacyProject.Revision, 1, knowledge); err != nil {
		t.Fatalf("commit legacy knowledge: %v", err)
	}

	m, err := NewManager(root, nil)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	defer m.Stop()

	list, err := m.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 || list[0].ID != DefaultID {
		t.Fatalf("projects = %+v, want only default", list)
	}
	if list[0].ChapterCount != 2 {
		t.Fatalf("default chapter count = %d, want 2", list[0].ChapterCount)
	}

	rt, err := m.Runtime(DefaultID)
	if err != nil {
		t.Fatalf("default runtime: %v", err)
	}
	project, err := rt.Store.LoadProject()
	if err != nil {
		t.Fatalf("load default project: %v", err)
	}
	if project.Title != "旧书" || len(project.Chapters) != 2 {
		t.Fatalf("default project = title %q chapters %d", project.Title, len(project.Chapters))
	}
	cfg, err := rt.Store.LoadConfig()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Model != "legacy-model" {
		t.Fatalf("legacy config lost: %+v", cfg)
	}
	kb, err := rt.Store.LoadKnowledge()
	if err != nil {
		t.Fatalf("load knowledge: %v", err)
	}
	if len(kb.Characters) != 1 || kb.Characters[0].Name != "旧角色" {
		t.Fatalf("legacy knowledge lost: %+v", kb.Characters)
	}
	info, err := rt.Store.LoadMemoryInfo()
	if err != nil {
		t.Fatalf("memory info: %v", err)
	}
	if info.SafeState {
		t.Fatalf("legacy memory unexpectedly in safe state: %s", info.SafeStateError)
	}
}

func TestProjectsAreIsolated(t *testing.T) {
	root := t.TempDir()
	m, err := NewManager(root, nil)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	defer m.Stop()

	first, err := m.Create("作品甲")
	if err != nil {
		t.Fatalf("create first: %v", err)
	}
	second, err := m.Create("作品乙")
	if err != nil {
		t.Fatalf("create second: %v", err)
	}
	a, err := m.Runtime(first.ID)
	if err != nil {
		t.Fatalf("runtime first: %v", err)
	}
	b, err := m.Runtime(second.ID)
	if err != nil {
		t.Fatalf("runtime second: %v", err)
	}
	if _, _, err := a.Store.AppendChapter("同名章", "甲的正文。"); err != nil {
		t.Fatalf("append first: %v", err)
	}
	if _, _, err := b.Store.AppendChapter("同名章", "乙的正文。"); err != nil {
		t.Fatalf("append second: %v", err)
	}

	aProject, err := a.Store.LoadProject()
	if err != nil {
		t.Fatalf("load first: %v", err)
	}
	bProject, err := b.Store.LoadProject()
	if err != nil {
		t.Fatalf("load second: %v", err)
	}
	if len(aProject.Chapters) != 1 || len(bProject.Chapters) != 1 {
		t.Fatalf("chapter counts = %d/%d, want 1/1", len(aProject.Chapters), len(bProject.Chapters))
	}
	aChapter, err := a.Store.LoadChapter(aProject.Chapters[0].ID)
	if err != nil {
		t.Fatalf("load first chapter: %v", err)
	}
	bChapter, err := b.Store.LoadChapter(bProject.Chapters[0].ID)
	if err != nil {
		t.Fatalf("load second chapter: %v", err)
	}
	if aChapter.Content != "甲的正文。" || bChapter.Content != "乙的正文。" {
		t.Fatalf("chapters crossed projects: %q / %q", aChapter.Content, bChapter.Content)
	}
	if aChapter.ID == bChapter.ID {
		t.Fatalf("chapter ids should differ across projects")
	}

	knowledgeA := novel.NewKnowledge()
	knowledgeA.Characters = append(knowledgeA.Characters, novel.Character{ID: "c_same", Name: "同名角色", Role: "甲角色"})
	if _, _, err := a.Store.CommitAnalysis(aProject.Revision, 1, knowledgeA); err != nil {
		t.Fatalf("commit first knowledge: %v", err)
	}
	knowledgeB := novel.NewKnowledge()
	knowledgeB.Characters = append(knowledgeB.Characters, novel.Character{ID: "c_same", Name: "同名角色", Role: "乙角色"})
	if _, _, err := b.Store.CommitAnalysis(bProject.Revision, 1, knowledgeB); err != nil {
		t.Fatalf("commit second knowledge: %v", err)
	}
	kbA, err := a.Store.LoadKnowledge()
	if err != nil {
		t.Fatalf("load first knowledge: %v", err)
	}
	kbB, err := b.Store.LoadKnowledge()
	if err != nil {
		t.Fatalf("load second knowledge: %v", err)
	}
	if len(kbA.Characters) != 1 || kbA.Characters[0].Role != "甲角色" {
		t.Fatalf("first knowledge crossed: %+v", kbA.Characters)
	}
	if len(kbB.Characters) != 1 || kbB.Characters[0].Role != "乙角色" {
		t.Fatalf("second knowledge crossed: %+v", kbB.Characters)
	}
}

func TestDeleteProjectRemovesOnlyItsData(t *testing.T) {
	root := t.TempDir()
	m, err := NewManager(root, nil)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	defer m.Stop()

	info, err := m.Create("待删除")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	rt, err := m.Runtime(info.ID)
	if err != nil {
		t.Fatalf("runtime: %v", err)
	}
	if _, _, err := rt.Store.AppendChapter("一", "正文"); err != nil {
		t.Fatalf("append: %v", err)
	}
	dir := rt.Dir
	if err := m.Delete(DefaultID); err != ErrDefaultProject {
		t.Fatalf("delete default err = %v, want ErrDefaultProject", err)
	}
	if err := m.Delete(info.ID); err != nil {
		t.Fatalf("delete project: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("project dir still exists: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "projects.db")); err != nil {
		t.Fatalf("index should remain: %v", err)
	}
}

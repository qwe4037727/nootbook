package world

import (
	"errors"
	"testing"

	"novelagent/internal/novel"
	"novelagent/internal/store"
)

func setupWorld(t *testing.T) (*Service, *store.Store, novel.Project, novel.Character, novel.WorldEntry) {
	t.Helper()
	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	project, err := st.ImportChapters("雨夜", []novel.Chapter{
		{Title: "第一章 开端", Content: "林砚走进雨巷，青石板上泛着水光。"},
		{Title: "第二章 转折", Content: "他在城隍庙前停下脚步。"},
	})
	if err != nil {
		t.Fatalf("ImportChapters: %v", err)
	}
	character := novel.Character{
		ID: novel.NewID("c"), Name: "林砚", Role: "捕快", State: "左肩受伤",
		Evidence: []string{"林砚走进雨巷"}, Confidence: 0.9, LastSeen: 1,
	}
	setting := novel.WorldEntry{
		ID: novel.NewID("w"), Title: "城隍庙", Category: "地点", Content: "县城北侧古庙。",
		Evidence: []string{"城隍庙前停下脚步"}, Confidence: 0.8, LastSeen: 2,
	}
	knowledge := novel.NewKnowledge()
	knowledge.Characters = []novel.Character{character}
	knowledge.World = []novel.WorldEntry{setting}
	knowledge.Summaries = []novel.ChapterSummary{
		{ChapterID: project.Chapters[0].ID, Index: 1, Title: "第一章 开端", Summary: "雨夜入城。"},
	}
	if _, ok, err := st.CommitAnalysis(project.Revision, 1, knowledge); err != nil || !ok {
		t.Fatalf("CommitAnalysis ok=%v err=%v", ok, err)
	}
	project, err = st.LoadProject()
	if err != nil {
		t.Fatalf("LoadProject: %v", err)
	}
	if _, ok, err := st.FinishAnalysis(project.Revision, knowledge); err != nil || !ok {
		t.Fatalf("FinishAnalysis ok=%v err=%v", ok, err)
	}
	project, err = st.LoadProject()
	if err != nil {
		t.Fatalf("LoadProject: %v", err)
	}
	return NewService(st, nil), st, project, character, setting
}

func TestWorldEntriesLocksAndEdit(t *testing.T) {
	svc, _, _, character, setting := setupWorld(t)

	entries, err := svc.ListEntries(EntryQuery{})
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(entries))
	}

	locked, err := svc.SetFieldLocks(character.ID, []string{"role", "state"}, true)
	if err != nil {
		t.Fatalf("SetFieldLocks: %v", err)
	}
	if len(locked) != 2 || locked[0] != "role" {
		t.Errorf("locked fields = %v", locked)
	}
	entry, err := svc.GetEntry(character.ID)
	if err != nil {
		t.Fatalf("GetEntry: %v", err)
	}
	if len(entry.LockedFields) != 2 {
		t.Errorf("entry locked fields = %v", entry.LockedFields)
	}

	if _, err := svc.SetFieldLocks(setting.ID, []string{"category"}, true); err != nil {
		t.Fatalf("lock setting: %v", err)
	}
	if _, err := svc.SetFieldLocks(character.ID, []string{"unknown"}, true); err == nil {
		t.Error("locking unknown field should fail")
	}

	updated, err := svc.UpdateEntry(character.ID, EntryPatch{
		Name:   "林砚",
		Fields: map[string]string{"role": "典史", "personality": "冷静"},
	})
	if err != nil {
		t.Fatalf("UpdateEntry: %v", err)
	}
	if updated.Fields["role"] != "典史" || updated.Fields["personality"] != "冷静" {
		t.Errorf("updated fields = %v", updated.Fields)
	}

	remaining, err := svc.SetFieldLocks(character.ID, []string{"role"}, false)
	if err != nil {
		t.Fatalf("unlock: %v", err)
	}
	if len(remaining) != 1 || remaining[0] != "state" {
		t.Errorf("remaining locks = %v", remaining)
	}
}

func TestWorldEvidenceTimelineAndSnapshot(t *testing.T) {
	svc, _, project, character, setting := setupWorld(t)

	evidence, err := svc.Evidence(character.ID)
	if err != nil {
		t.Fatalf("Evidence: %v", err)
	}
	if len(evidence) != 1 {
		t.Fatalf("evidence = %+v", evidence)
	}
	if evidence[0].ChapterID != project.Chapters[0].ID || evidence[0].Quote != "林砚走进雨巷" {
		t.Errorf("evidence item = %+v", evidence[0])
	}

	if _, err := svc.SaveRelation(novel.Relation{
		FromEntryID: character.ID,
		ToEntryID:   novel.NewID("w"),
		Type:        "located_in",
	}); err == nil {
		t.Fatal("relation to non-existent entry should fail")
	}
	if _, err := svc.SaveRelation(novel.Relation{
		FromEntryID: character.ID,
		ToEntryID:   character.ID,
		Type:        "self",
	}); err == nil {
		t.Fatal("self relation should fail")
	}
	rel, err := svc.SaveRelation(novel.Relation{
		FromEntryID:  character.ID,
		ToEntryID:    setting.ID,
		Type:         "visited",
		StartChapter: 2,
	})
	if err != nil {
		t.Fatalf("SaveRelation: %v", err)
	}
	if rel.ID == "" || rel.Status != novel.RelationActive {
		t.Errorf("relation = %+v", rel)
	}

	event, err := svc.SaveTimelineEvent("", TimelinePatch{
		ChapterID:  project.Chapters[0].ID,
		Title:      "林砚入城",
		Summary:    "雨夜抵达县城。",
		StoryOrder: 1,
	})
	if err != nil {
		t.Fatalf("SaveTimelineEvent: %v", err)
	}
	if event.ID == "" || event.ChapterIndex != 1 || event.Source != novel.TimelineManual {
		t.Errorf("timeline event = %+v", event)
	}
	timeline, err := svc.Timeline()
	if err != nil {
		t.Fatalf("Timeline: %v", err)
	}
	if len(timeline) != 1 {
		t.Errorf("timeline = %+v", timeline)
	}

	snap, err := svc.Snapshot(1)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if len(snap.Entries) != 1 || snap.Entries[0].ID != character.ID {
		t.Errorf("snapshot ch1 entries = %+v", snap.Entries)
	}
	if len(snap.Relations) != 0 {
		t.Errorf("snapshot ch1 relations = %+v", snap.Relations)
	}
	if len(snap.Summaries) != 1 || snap.Summaries[0].Index != 1 {
		t.Errorf("snapshot summaries = %+v", snap.Summaries)
	}
	if len(snap.Timeline) != 1 {
		t.Errorf("snapshot timeline = %+v", snap.Timeline)
	}

	snap2, err := svc.Snapshot(2)
	if err != nil {
		t.Fatalf("Snapshot 2: %v", err)
	}
	if len(snap2.Entries) != 2 {
		t.Errorf("snapshot ch2 entries = %+v", snap2.Entries)
	}
	if len(snap2.Relations) != 1 {
		t.Errorf("snapshot ch2 relations = %+v", snap2.Relations)
	}
	if _, err := svc.Snapshot(3); !errors.Is(err, ErrChapterNotFound) {
		t.Errorf("snapshot out of range err = %v", err)
	}

	if err := svc.DeleteTimelineEvent(event.ID); err != nil {
		t.Fatalf("DeleteTimelineEvent: %v", err)
	}
	if err := svc.DeleteTimelineEvent(event.ID); !errors.Is(err, ErrTimelineNotFound) {
		t.Errorf("delete twice err = %v", err)
	}
	if err := svc.DeleteRelation(rel.ID); err != nil {
		t.Fatalf("DeleteRelation: %v", err)
	}
}

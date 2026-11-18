package store

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"novelagent/internal/novel"
)

// G6 联合提交（iter2-详设 §4/§8.2）：草稿区不触碰正式区；confirm 时正文与记忆变更
// 在同一次 memoryRevision 提交中落盘，失败/崩溃均不留下半写状态。

func draftEventOp(t *testing.T, id, chapterID string, chapterRevision int64, quote string) novel.ChangeOp {
	t.Helper()
	event := novel.StoryEvent{
		ID: id,
		MemoryMeta: novel.MemoryMeta{
			WorkID:        novel.InternalWorkID(),
			RecordVersion: 1,
			Origin:        novel.Origin{Type: novel.OriginDeepExtract},
			LastSource:    novel.LastSourceAgentExtraction,
			SourceRefs: []novel.SourceRef{{
				ChapterID:       chapterID,
				ChapterRevision: chapterRevision,
				StartOffset:     0,
				EndOffset:       len([]rune(quote)),
				OffsetUnit:      "rune",
				Quote:           quote,
			}},
		},
		Status:       novel.EventConfirmed,
		EventType:    string(novel.EventAction),
		Summary:      "林越推门",
		ChapterID:    chapterID,
		Participants: []string{"c_hero"},
	}
	return novel.ChangeOp{Op: novel.OpAddEvent, Record: eventRaw(t, event)}
}

func draftChangeSet(t *testing.T, rev int64, draft novel.Draft, ops ...novel.ChangeOp) novel.ChangeSet {
	t.Helper()
	cs := newChangeSet(rev, ops...)
	cs.ChapterID = draft.TargetChapterID
	cs.ChapterRevision = draft.PendingChapterRevision()
	return cs
}

func mustCreateDraft(t *testing.T, st *Store, p DraftCreateParams) novel.Draft {
	t.Helper()
	draft, err := st.CreateDraft(p)
	if err != nil {
		t.Fatalf("CreateDraft: %v", err)
	}
	return draft
}

func mustStashDraft(t *testing.T, st *Store, draftID string, revision int64, cs novel.ChangeSet) novel.ChangeSet {
	t.Helper()
	_, stored, err := st.StashDraftChangeSet(draftID, revision, cs, nil)
	if err != nil {
		t.Fatalf("StashDraftChangeSet: %v", err)
	}
	return stored
}

func TestDraftLifecycleDoesNotTouchFormalArea(t *testing.T) {
	st, dir := newTestStore(t)

	draft := mustCreateDraft(t, st, DraftCreateParams{Title: "第一章", Content: "林越推门而入。"})
	if draft.ID == "" || !strings.HasPrefix(draft.TargetChapterID, "ch_") {
		t.Fatalf("draft = %+v", draft)
	}
	if !draft.NewChapter || draft.Status != novel.DraftPending || draft.Revision != 1 {
		t.Errorf("new draft = %+v", draft)
	}
	if draft.BaseMemoryRevision != memoryRevisionOf(t, st) {
		t.Errorf("baseMemoryRevision = %d", draft.BaseMemoryRevision)
	}
	if files := chapterFiles(t, dir); files != nil {
		t.Errorf("unexpected chapter files %v", files)
	}
	project, err := st.LoadProject()
	if err != nil {
		t.Fatalf("LoadProject: %v", err)
	}
	if len(project.Chapters) != 0 {
		t.Errorf("draft leaked into project: %+v", project.Chapters)
	}
	if rev := memoryRevisionOf(t, st); rev != 0 {
		t.Errorf("memoryRevision after create = %d, want 0", rev)
	}

	updated, err := st.UpdateDraft(draft.ID, 1, "改标题", "林越推门而入。风很冷。")
	if err != nil {
		t.Fatalf("UpdateDraft: %v", err)
	}
	if updated.Revision != 2 || updated.Title != "改标题" || updated.Content != "林越推门而入。风很冷。" {
		t.Errorf("updated draft = %+v", updated)
	}
	if _, err := st.UpdateDraft(draft.ID, 1, "", "x"); !errors.Is(err, ErrDraftConflict) {
		t.Errorf("stale update error = %v, want ErrDraftConflict", err)
	}

	summaries, err := st.LoadDrafts(string(novel.DraftPending))
	if err != nil || len(summaries) != 1 || summaries[0].ID != draft.ID {
		t.Fatalf("LoadDrafts pending = %+v, err = %v", summaries, err)
	}
	committed, err := st.LoadDrafts(string(novel.DraftCommitted))
	if err != nil || len(committed) != 0 {
		t.Errorf("LoadDrafts committed = %+v, err = %v", committed, err)
	}

	rejected, err := st.RejectDraft(draft.ID, "不需要")
	if err != nil {
		t.Fatalf("RejectDraft: %v", err)
	}
	if rejected.Status != novel.DraftRejected || rejected.RejectReason != "不需要" {
		t.Errorf("rejected draft = %+v", rejected)
	}
	if _, err := st.RejectDraft(draft.ID, ""); !errors.Is(err, ErrDraftTerminal) {
		t.Errorf("re-reject error = %v, want ErrDraftTerminal", err)
	}
	if rev := memoryRevisionOf(t, st); rev != 0 {
		t.Errorf("memoryRevision after draft ops = %d, want 0", rev)
	}
}

func TestConfirmDraftNewChapterCommitsChapterAndMemoryTogether(t *testing.T) {
	st, dir := newTestStore(t)
	draft := mustCreateDraft(t, st, DraftCreateParams{Title: "第一章", Content: "林越推门而入。"})

	cs := draftChangeSet(t, 0, draft, draftEventOp(t, "evt_door", draft.TargetChapterID, 1, "林越推门而入"))
	stored := mustStashDraft(t, st, draft.ID, draft.Revision, cs)
	if stored.Status != novel.ChangeSetProposed || stored.DraftID != draft.ID || stored.DraftRevision != 1 {
		t.Fatalf("stored changeset = %+v", stored)
	}

	result, err := st.ConfirmDraft(draft.ID, DraftConfirmParams{
		DraftRevision:          draft.Revision,
		ExpectedMemoryRevision: 0,
	})
	if err != nil {
		t.Fatalf("ConfirmDraft: %v", err)
	}
	if result.MemoryRevision != 1 || result.CommitID == "" {
		t.Errorf("result memoryRevision/commit = %d/%q", result.MemoryRevision, result.CommitID)
	}
	if result.Draft.Status != novel.DraftCommitted || result.Draft.ChapterRevision != 1 {
		t.Errorf("draft after confirm = %+v", result.Draft)
	}
	if result.Chapter.ID != draft.TargetChapterID || result.Chapter.Content != draft.Content || result.Chapter.Revision != 1 {
		t.Errorf("chapter after confirm = %+v", result.Chapter)
	}
	if len(result.Project.Chapters) != 1 || result.Project.Chapters[0].ID != draft.TargetChapterID {
		t.Errorf("project after confirm = %+v", result.Project)
	}

	loaded, err := st.LoadChapter(draft.TargetChapterID)
	if err != nil || loaded.Content != draft.Content {
		t.Fatalf("LoadChapter = %+v, err = %v", loaded, err)
	}
	events, _, _, _, _, err := st.LoadMemoryRecords()
	if err != nil || len(events) != 1 || events[0].ID != "evt_door" || events[0].Status != novel.EventConfirmed {
		t.Fatalf("events after confirm = %+v, err = %v", events, err)
	}

	commits := decodeCommits(t, dir)
	if len(commits) != 1 || commits[0].CommitID != result.CommitID || commits[0].Type != CommitFormal {
		t.Fatalf("commits = %+v", commits)
	}
	for _, key := range []string{"project.json", "chapter:" + draft.TargetChapterID, "drafts.json", "changesets.json", "events.json"} {
		if _, ok := commits[0].Payload[key]; !ok {
			t.Errorf("commit payload missing %q", key)
		}
		if _, ok := commits[0].RevertPayload[key]; !ok {
			t.Errorf("revert payload missing %q", key)
		}
	}
	if got := string(commits[0].RevertPayload["chapter:"+draft.TargetChapterID]); got != "null" {
		t.Errorf("chapter revert snapshot = %s, want null", got)
	}

	// 幂等重放：不追加提交、不递增版本。
	again, err := st.ConfirmDraft(draft.ID, DraftConfirmParams{DraftRevision: 1})
	if err != nil {
		t.Fatalf("idempotent ConfirmDraft: %v", err)
	}
	if again.CommitID != result.CommitID || again.MemoryRevision != result.MemoryRevision {
		t.Errorf("idempotent result = %+v", again)
	}
	if commits := decodeCommits(t, dir); len(commits) != 1 {
		t.Errorf("idempotent confirm wrote %d commits", len(commits))
	}
}

func TestConfirmDraftExistingChapterBindsRevisions(t *testing.T) {
	st, _ := newTestStore(t)
	project, err := st.ImportChapters("书名", []novel.Chapter{{Title: "旧章", Content: "原始内容。"}})
	if err != nil {
		t.Fatalf("ImportChapters: %v", err)
	}
	chapterID := project.Chapters[0].ID

	draft := mustCreateDraft(t, st, DraftCreateParams{ChapterID: chapterID, Content: "改写后的正文。"})
	if draft.NewChapter || draft.BaseChapterRevision != project.Chapters[0].Revision {
		t.Fatalf("draft = %+v", draft)
	}
	cs := draftChangeSet(t, 0, draft)
	mustStashDraft(t, st, draft.ID, draft.Revision, cs)

	if _, err := st.ConfirmDraft(draft.ID, DraftConfirmParams{DraftRevision: 1, ExpectedChapterRevision: 99}); !errors.Is(err, ErrChapterConflict) {
		t.Fatalf("stale expected chapter revision error = %v, want ErrChapterConflict", err)
	}
	if loaded, _ := st.LoadChapter(chapterID); loaded.Content != "原始内容。" {
		t.Errorf("failed confirm modified chapter: %+v", loaded)
	}

	result, err := st.ConfirmDraft(draft.ID, DraftConfirmParams{
		DraftRevision:           1,
		ExpectedChapterRevision: project.Chapters[0].Revision,
	})
	if err != nil {
		t.Fatalf("ConfirmDraft: %v", err)
	}
	if result.Chapter.Revision != project.Chapters[0].Revision+1 || result.Chapter.Content != "改写后的正文。" {
		t.Errorf("chapter = %+v", result.Chapter)
	}
	if len(result.Project.Chapters) != 1 || result.Project.Chapters[0].Revision != result.Chapter.Revision {
		t.Errorf("project chapters = %+v", result.Project.Chapters)
	}
}

func TestDraftEditInvalidatesExtraction(t *testing.T) {
	st, _ := newTestStore(t)
	draft := mustCreateDraft(t, st, DraftCreateParams{Content: "林越推门而入。"})
	first := mustStashDraft(t, st, draft.ID, 1, draftChangeSet(t, 0, draft))

	updated, err := st.UpdateDraft(draft.ID, 1, "", "林越推门而入。风很冷。")
	if err != nil {
		t.Fatalf("UpdateDraft: %v", err)
	}
	if updated.ChangeSetID != "" {
		t.Errorf("edit kept stale changeset %q", updated.ChangeSetID)
	}
	if _, _, err := st.StashDraftChangeSet(draft.ID, 1, draftChangeSet(t, 0, draft), nil); !errors.Is(err, ErrDraftConflict) {
		t.Errorf("stale stash error = %v, want ErrDraftConflict", err)
	}
	if _, err := st.ConfirmDraft(draft.ID, DraftConfirmParams{DraftRevision: 1}); !errors.Is(err, ErrDraftConflict) {
		t.Errorf("stale confirm error = %v, want ErrDraftConflict", err)
	}

	second := mustStashDraft(t, st, draft.ID, 2, draftChangeSet(t, 0, updated))
	if _, err := st.AcceptChangeSet(second.ID); !errors.Is(err, ErrDraftManagedChangeSet) {
		t.Errorf("accept draft changeset error = %v, want ErrDraftManagedChangeSet", err)
	}
	if _, err := st.RebaseChangeSet(second.ID); !errors.Is(err, ErrDraftManagedChangeSet) {
		t.Errorf("rebase draft changeset error = %v, want ErrDraftManagedChangeSet", err)
	}
	sets, err := st.LoadChangeSets("")
	if err != nil {
		t.Fatalf("LoadChangeSets: %v", err)
	}
	for _, summary := range sets {
		if summary.ID == first.ID && summary.Status != novel.ChangeSetRejected {
			t.Errorf("old changeset status = %s, want rejected", summary.Status)
		}
	}
	result, err := st.ConfirmDraft(draft.ID, DraftConfirmParams{DraftRevision: 2})
	if err != nil {
		t.Fatalf("ConfirmDraft after re-extract: %v", err)
	}
	if result.ChangeSetID != second.ID {
		t.Errorf("confirmed changeset = %q, want %q", result.ChangeSetID, second.ID)
	}
}

func TestConfirmDraftRejectsStaleMemoryBaseline(t *testing.T) {
	st, _ := newTestStore(t)
	draft := mustCreateDraft(t, st, DraftCreateParams{Content: "林越推门而入。"})
	mustStashDraft(t, st, draft.ID, 1, draftChangeSet(t, 0, draft))

	seedFormalCommit(t, st, map[string]any{"states.json": []novel.StoryState{}})

	if _, err := st.ConfirmDraft(draft.ID, DraftConfirmParams{DraftRevision: 1}); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale baseline error = %v, want ErrRevisionConflict", err)
	}
	if _, err := st.LoadChapter(draft.TargetChapterID); !errors.Is(err, ErrChapterNotFound) {
		t.Errorf("chapter written despite stale baseline: %v", err)
	}
}

func TestConfirmDraftReconcilesEvidenceInSameCommit(t *testing.T) {
	st, dir := newTestStore(t)
	project, err := st.ImportChapters("书名", []novel.Chapter{{Title: "旧章", Content: "林越推门而入。"}})
	if err != nil {
		t.Fatalf("ImportChapters: %v", err)
	}
	chapterID := project.Chapters[0].ID
	event := novel.StoryEvent{
		ID: "evt_old",
		MemoryMeta: novel.MemoryMeta{
			WorkID:        novel.InternalWorkID(),
			RecordVersion: 1,
			Origin:        novel.Origin{Type: novel.OriginBackfill},
			LastSource:    novel.LastSourceAgentExtraction,
			SourceRefs: []novel.SourceRef{{
				ChapterID:       chapterID,
				ChapterRevision: 1,
				StartOffset:     0,
				EndOffset:       6,
				OffsetUnit:      "rune",
				Quote:           "林越推门而入",
			}},
		},
		Status:    novel.EventConfirmed,
		EventType: string(novel.EventAction),
		Summary:   "林越推门",
		ChapterID: chapterID,
	}
	seedFormalCommit(t, st, map[string]any{"events.json": []novel.StoryEvent{event}})

	draft := mustCreateDraft(t, st, DraftCreateParams{ChapterID: chapterID, Content: "林越推门而入。风很冷。"})
	mustStashDraft(t, st, draft.ID, draft.Revision, draftChangeSet(t, 1, draft))

	result, err := st.ConfirmDraft(draft.ID, DraftConfirmParams{DraftRevision: 1, ExpectedChapterRevision: 1})
	if err != nil {
		t.Fatalf("ConfirmDraft: %v", err)
	}
	if result.MemoryRevision != 2 {
		t.Errorf("memoryRevision = %d, want 2", result.MemoryRevision)
	}
	events, _, _, _, _, err := st.LoadMemoryRecords()
	if err != nil || len(events) != 1 {
		t.Fatalf("events = %+v, err = %v", events, err)
	}
	if !events[0].NeedsReview || events[0].ReviewReason != "evidence_stale" {
		t.Errorf("event review = %v/%q, want evidence_stale", events[0].NeedsReview, events[0].ReviewReason)
	}
	commits := decodeCommits(t, dir)
	last := commits[len(commits)-1]
	if _, ok := last.Payload["events.json"]; !ok {
		t.Errorf("reconcile marks not in same commit payload: %v", last.Payload)
	}
}

func TestConfirmDraftFailureLeavesNoPartialWrite(t *testing.T) {
	st, dir := newTestStore(t)
	draft := mustCreateDraft(t, st, DraftCreateParams{Content: "林越推门而入。"})
	mustStashDraft(t, st, draft.ID, draft.Revision, draftChangeSet(t, 0, draft))

	// 目标章节路径被目录占用 → 提交快照/写入失败；整批必须回滚为零写入。
	chapterPath := filepath.Join(dir, chaptersDirName, draft.TargetChapterID+".json")
	if err := os.MkdirAll(chapterPath, 0o755); err != nil {
		t.Fatalf("occupy chapter path: %v", err)
	}
	if _, err := st.ConfirmDraft(draft.ID, DraftConfirmParams{DraftRevision: 1}); err == nil {
		t.Fatal("ConfirmDraft succeeded with an unwritable chapter path")
	}

	loaded, err := st.LoadDraft(draft.ID)
	if err != nil || loaded.Status != novel.DraftPending || loaded.CommitID != "" {
		t.Errorf("draft after failed confirm = %+v, err = %v", loaded, err)
	}
	project, err := st.LoadProject()
	if err != nil || len(project.Chapters) != 0 {
		t.Errorf("project after failed confirm = %+v, err = %v", project, err)
	}
	if rev := memoryRevisionOf(t, st); rev != 0 {
		t.Errorf("memoryRevision after failed confirm = %d, want 0", rev)
	}
	if _, err := os.Stat(st.memoryPath("commits.json")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("failed confirm wrote commit log: %v", err)
	}
}

func TestRecoveryReplaysDraftCommitTargets(t *testing.T) {
	st, dir := newTestStore(t)
	preProject, err := os.ReadFile(filepath.Join(dir, "project.json"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("read project.json: %v", err)
	}
	draft := mustCreateDraft(t, st, DraftCreateParams{Title: "第一章", Content: "林越推门而入。"})
	result, err := st.ConfirmDraft(draft.ID, DraftConfirmParams{DraftRevision: 1})
	if err != nil {
		t.Fatalf("ConfirmDraft: %v", err)
	}
	chapterPath := filepath.Join(dir, chaptersDirName, draft.TargetChapterID+".json")
	if _, err := os.Stat(chapterPath); err != nil {
		t.Fatalf("chapter not written: %v", err)
	}

	// 模拟崩溃：正文/项目已部分写入后丢失，committed/meta 未持久化。
	if err := os.Remove(chapterPath); err != nil {
		t.Fatalf("remove chapter: %v", err)
	}
	if preProject == nil {
		if err := os.Remove(filepath.Join(dir, "project.json")); err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("remove project: %v", err)
		}
	} else if err := os.WriteFile(filepath.Join(dir, "project.json"), preProject, 0o644); err != nil {
		t.Fatalf("restore project: %v", err)
	}
	rewriteCommits(t, dir, func(records []CommitRecord) []CommitRecord {
		for i := range records {
			if records[i].CommitID == result.CommitID {
				records[i].Status = CommitPrepared
			}
		}
		return records
	})
	rewriteMeta(t, dir, 0, "")

	st2, err := New(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	loaded, err := st2.LoadChapter(draft.TargetChapterID)
	if err != nil {
		t.Fatalf("chapter not replayed: %v", err)
	}
	if loaded.Content != draft.Content {
		t.Errorf("replayed chapter = %+v", loaded)
	}
	if project, err := st2.LoadProject(); err != nil || len(project.Chapters) != 1 {
		t.Errorf("replayed project = %+v, err = %v", project, err)
	}
	if info, err := st2.LoadMemoryInfo(); err != nil || info.MemoryRevision != 1 || info.SafeState {
		t.Errorf("info after replay = %+v, err = %v", info, err)
	}
	commits := decodeCommits(t, dir)
	if len(commits) != 1 || commits[0].Status != CommitCommitted {
		t.Errorf("commits after replay = %+v", commits)
	}
}

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

// 迭代 2 验收用例（iter2-详设 §10）：U9/U10/U16/U18/U29/U31/U33 写入口、
// I2-A/I2-B/I2-E/I2-F/I2-G/I2-H 与整改项 D2。

func seedFormalCommit(t *testing.T, st *Store, payload map[string]any) CommitRecord {
	t.Helper()
	st.memoryMu.Lock()
	defer st.memoryMu.Unlock()
	meta, err := st.ensureMemoryMetaLocked()
	if err != nil {
		t.Fatalf("ensureMemoryMetaLocked: %v", err)
	}
	record, err := st.commitMemoryLocked(CommitFormal, meta.MemoryRevision, payload)
	if err != nil {
		t.Fatalf("commitMemoryLocked: %v", err)
	}
	return record
}

func memoryRevisionOf(t *testing.T, st *Store) int64 {
	t.Helper()
	info, err := st.LoadMemoryInfo()
	if err != nil {
		t.Fatalf("LoadMemoryInfo: %v", err)
	}
	return info.MemoryRevision
}

func formalState(id, subject, category, attribute, value string) novel.StoryState {
	return novel.StoryState{
		ID: id,
		MemoryMeta: novel.MemoryMeta{
			WorkID:        novel.InternalWorkID(),
			RecordVersion: 1,
			Origin:        novel.Origin{Type: novel.OriginBackfill},
			LastSource:    novel.LastSourceAgentExtraction,
		},
		Status:    novel.StateActive,
		SubjectID: subject,
		Category:  category,
		Attribute: attribute,
		Value:     value,
	}
}

func rawState(t *testing.T, state novel.StoryState) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("marshal state: %v", err)
	}
	return raw
}

func addStateOp(t *testing.T, state novel.StoryState) novel.ChangeOp {
	t.Helper()
	return novel.ChangeOp{Op: novel.OpAddState, Record: rawState(t, state)}
}

func updateStateOp(t *testing.T, targetID string, expected int64, state novel.StoryState) novel.ChangeOp {
	t.Helper()
	return novel.ChangeOp{Op: novel.OpUpdateState, TargetID: targetID, ExpectedRecordVersion: expected, Record: rawState(t, state)}
}

func newChangeSet(rev int64, ops ...novel.ChangeOp) novel.ChangeSet {
	return novel.ChangeSet{
		ID:              novel.NewID("mcs"),
		Status:          novel.ChangeSetProposed,
		Proposer:        "test",
		BasedOnRevision: rev,
		Ops:             ops,
		Conflicts:       []novel.ConflictItem{},
	}
}

func saveTestChangeSet(t *testing.T, st *Store, cs novel.ChangeSet) {
	t.Helper()
	if err := st.SaveChangeSet(cs, nil); err != nil {
		t.Fatalf("SaveChangeSet: %v", err)
	}
}

func findState(states []novel.StoryState, id string) *novel.StoryState {
	for i := range states {
		if states[i].ID == id {
			return &states[i]
		}
	}
	return nil
}

func knowledgeOf(chars ...novel.Character) novel.Knowledge {
	knowledge := novel.NewKnowledge()
	knowledge.Characters = chars
	return knowledge
}

// TestReconcileEvidenceStaleOnChapterSave 覆盖 U9（完整）：章节正文变更 → evidence_stale + 版本递增，
// 再次复核为 no-op（不重复递增）。
func TestReconcileEvidenceStaleOnChapterSave(t *testing.T) {
	st, _ := newTestStore(t)
	project, err := st.ImportChapters("书", []novel.Chapter{{Title: "一", Content: "下雨了。"}})
	if err != nil {
		t.Fatalf("ImportChapters: %v", err)
	}
	chapterMeta := project.Chapters[0]
	event := novel.StoryEvent{
		ID: "evt_rain",
		MemoryMeta: novel.MemoryMeta{
			WorkID:        novel.InternalWorkID(),
			RecordVersion: 1,
			Origin:        novel.Origin{Type: novel.OriginDeepExtract, Chapter: chapterMeta.ID},
			LastSource:    novel.LastSourceAgentExtraction,
			SourceRefs: []novel.SourceRef{{ChapterID: chapterMeta.ID, ChapterRevision: 1,
				StartOffset: 0, EndOffset: 3, OffsetUnit: "rune", Quote: "下雨了"}},
		},
		Status:    novel.EventConfirmed,
		EventType: string(novel.EventOther),
		Summary:   "下雨",
		ChapterID: chapterMeta.ID,
	}
	if record := seedFormalCommit(t, st, map[string]any{"events.json": []novel.StoryEvent{event}}); record.TargetMemoryRevision != 1 {
		t.Fatalf("seed revision = %d", record.TargetMemoryRevision)
	}

	if _, _, err := st.SaveChapter(novel.Chapter{ID: chapterMeta.ID, Title: chapterMeta.Title, Content: "天晴了。"}); err != nil {
		t.Fatalf("SaveChapter: %v", err)
	}

	events, _, _, _, _, err := st.LoadMemoryRecords()
	if err != nil {
		t.Fatalf("LoadMemoryRecords: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %d", len(events))
	}
	if !events[0].NeedsReview || events[0].ReviewReason != "evidence_stale" || events[0].RecordVersion != 2 {
		t.Errorf("event after save = %+v", events[0].MemoryMeta)
	}
	if got := memoryRevisionOf(t, st); got != 2 {
		t.Errorf("revision after save = %d, want 2", got)
	}

	marked, rev, err := st.ReconcileMemory(nil)
	if err != nil {
		t.Fatalf("ReconcileMemory: %v", err)
	}
	if marked != 0 || rev != 2 {
		t.Errorf("no-op reconcile = (%d,%d), want (0,2)", marked, rev)
	}
}

// TestReconcileQuoteMismatch 覆盖 U10（完整）：章节 Revision 未变但 quote 与正文不符 → quote_mismatch。
func TestReconcileQuoteMismatch(t *testing.T) {
	st, _ := newTestStore(t)
	project, err := st.ImportChapters("书", []novel.Chapter{{Title: "一", Content: "下雨了。"}})
	if err != nil {
		t.Fatalf("ImportChapters: %v", err)
	}
	chapterMeta := project.Chapters[0]
	event := novel.StoryEvent{
		ID: "evt_bad_quote",
		MemoryMeta: novel.MemoryMeta{
			WorkID:        novel.InternalWorkID(),
			RecordVersion: 1,
			Origin:        novel.Origin{Type: novel.OriginDeepExtract, Chapter: chapterMeta.ID},
			LastSource:    novel.LastSourceAgentExtraction,
			SourceRefs: []novel.SourceRef{{ChapterID: chapterMeta.ID, ChapterRevision: 1,
				StartOffset: 0, EndOffset: 2, OffsetUnit: "rune", Quote: "错误"}},
		},
		Status:    novel.EventConfirmed,
		EventType: string(novel.EventOther),
		Summary:   "引用错误",
		ChapterID: chapterMeta.ID,
	}
	seedFormalCommit(t, st, map[string]any{"events.json": []novel.StoryEvent{event}})

	marked, rev, err := st.ReconcileMemory([]string{chapterMeta.ID})
	if err != nil {
		t.Fatalf("ReconcileMemory: %v", err)
	}
	if marked != 1 || rev != 2 {
		t.Fatalf("marked/rev = %d/%d, want 1/2", marked, rev)
	}
	events, _, _, _, _, err := st.LoadMemoryRecords()
	if err != nil {
		t.Fatalf("LoadMemoryRecords: %v", err)
	}
	if !events[0].NeedsReview || events[0].ReviewReason != "quote_mismatch" || events[0].RecordVersion != 2 {
		t.Errorf("event after reconcile = %+v", events[0].MemoryMeta)
	}
}

// TestAcceptInvalidOpRejectsWholeChangeSet 覆盖 U16：任一 op L-S 失败 → 整个 accept 拒绝、零写入。
func TestAcceptInvalidOpRejectsWholeChangeSet(t *testing.T) {
	st, _ := newTestStore(t)
	valid := formalState("state_ok", "c_hero", "physical", "伤势", "重伤")
	invalid := novel.StoryState{
		ID: "state_bad",
		MemoryMeta: novel.MemoryMeta{WorkID: novel.InternalWorkID(), RecordVersion: 1,
			Origin: novel.Origin{Type: novel.OriginBackfill}, LastSource: novel.LastSourceAgentExtraction},
		Status:    novel.StateActive,
		SubjectID: "hero",
		Category:  "physical",
		Attribute: "伤势",
		Value:     "重伤",
	}
	cs := newChangeSet(0, addStateOp(t, valid), addStateOp(t, invalid))
	saveTestChangeSet(t, st, cs)

	if _, err := st.AcceptChangeSet(cs.ID); !errors.Is(err, ErrInvalidChangeSet) {
		t.Fatalf("accept error = %v, want ErrInvalidChangeSet", err)
	}
	if got := memoryRevisionOf(t, st); got != 0 {
		t.Errorf("revision = %d, want 0", got)
	}
	_, states, _, _, _, err := st.LoadMemoryRecords()
	if err != nil {
		t.Fatalf("LoadMemoryRecords: %v", err)
	}
	if len(states) != 0 {
		t.Errorf("states persisted on rejected accept: %+v", states)
	}
	stored, err := st.GetChangeSet(cs.ID)
	if err != nil {
		t.Fatalf("GetChangeSet: %v", err)
	}
	if stored.Status != novel.ChangeSetProposed {
		t.Errorf("changeset status = %s, want proposed", stored.Status)
	}
}

// TestAcceptCognitionOpPersists 覆盖 S3 cognitions 写入口：add_cognition 经 accept 正式落盘。
func TestAcceptCognitionOpPersists(t *testing.T) {
	st, _ := newTestStore(t)
	cog := novel.Cognition{
		ID: "cog_belief",
		MemoryMeta: novel.MemoryMeta{
			WorkID:        novel.InternalWorkID(),
			RecordVersion: 1,
			Origin:        novel.Origin{Type: novel.OriginDeepExtract},
			LastSource:    novel.LastSourceAgentExtraction,
		},
		Status:    novel.CogWrong,
		SubjectID: "c_hero",
		Content:   "以为铜牌是真的",
	}
	raw, err := json.Marshal(cog)
	if err != nil {
		t.Fatalf("marshal cognition: %v", err)
	}
	cs := newChangeSet(0, novel.ChangeOp{Op: novel.OpAddCognition, Record: raw})
	saveTestChangeSet(t, st, cs)

	result, err := st.AcceptChangeSet(cs.ID)
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	if result.MemoryRevision != 1 || result.CommitID == "" {
		t.Errorf("accept result = %+v, want committed revision 1", result)
	}
	_, _, cognitions, _, _, err := st.LoadMemoryRecords()
	if err != nil {
		t.Fatalf("LoadMemoryRecords: %v", err)
	}
	if len(cognitions) != 1 || cognitions[0].SubjectID != "c_hero" || cognitions[0].Status != novel.CogWrong || cognitions[0].Content != "以为铜牌是真的" {
		t.Errorf("cognitions = %+v", cognitions)
	}
}

// TestAcceptStateCrossCategoryCoexist 覆盖 U18 写入口：update 只替换目标，其他 category 的同属性状态不受影响。
func TestAcceptStateCrossCategoryCoexist(t *testing.T) {
	st, _ := newTestStore(t)
	physical := formalState("state_phys", "c_hero", "physical", "状态", "重伤")
	relation := formalState("state_rel", "c_hero", "relation", "状态", "敌对")
	seedFormalCommit(t, st, map[string]any{"states.json": []novel.StoryState{physical, relation}})

	changed := formalState("state_new", "c_hero", "physical", "状态", "轻伤")
	cs := newChangeSet(1, updateStateOp(t, "state_phys", 1, changed))
	saveTestChangeSet(t, st, cs)
	if _, err := st.AcceptChangeSet(cs.ID); err != nil {
		t.Fatalf("accept: %v", err)
	}

	_, states, _, _, _, err := st.LoadMemoryRecords()
	if err != nil {
		t.Fatalf("LoadMemoryRecords: %v", err)
	}
	if rel := findState(states, "state_rel"); rel == nil || rel.Status != novel.StateActive || rel.Value != "敌对" || rel.RecordVersion != 1 {
		t.Errorf("relation state changed: %+v", rel)
	}
	if old := findState(states, "state_phys"); old == nil || old.Status != novel.StateSuperseded || old.RecordVersion != 2 {
		t.Errorf("physical target not superseded: %+v", old)
	}
	active := 0
	for _, state := range states {
		if state.Status == novel.StateActive && state.Category == "physical" && state.Attribute == "状态" && state.Value == "轻伤" {
			active++
		}
	}
	if active != 1 {
		t.Errorf("new physical active states = %d, want 1 (states=%+v)", active, states)
	}
}

// TestAcceptSameValueStateNoop 覆盖 U29 写入口（accept 侧）：同三元组同值 → applied 但不提交、不递增。
func TestAcceptSameValueStateNoop(t *testing.T) {
	st, _ := newTestStore(t)
	state := formalState("state_same", "c_hero", "physical", "伤势", "重伤")
	seedFormalCommit(t, st, map[string]any{"states.json": []novel.StoryState{state}})

	same := formalState("state_same", "c_hero", "physical", "伤势", "重伤")
	cs := newChangeSet(1, updateStateOp(t, "state_same", 1, same))
	saveTestChangeSet(t, st, cs)
	result, err := st.AcceptChangeSet(cs.ID)
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	if result.Status != string(novel.ChangeSetApplied) || result.CommitID != "" || result.MemoryRevision != 1 {
		t.Errorf("result = %+v, want applied without commit at rev1", result)
	}
	_, states, _, _, _, err := st.LoadMemoryRecords()
	if err != nil {
		t.Fatalf("LoadMemoryRecords: %v", err)
	}
	if len(states) != 1 || states[0].RecordVersion != 1 || states[0].Status != novel.StateActive {
		t.Errorf("same-value state changed: %+v", states)
	}
}

// TestAcceptRecordVersionSemantics 覆盖 U33：accept 后旧记录 recordVersion+1、新记录 rv1；
// 过期 expectedRecordVersion → optimistic_conflict 且零写入。
func TestAcceptRecordVersionSemantics(t *testing.T) {
	st, _ := newTestStore(t)
	seedFormalCommit(t, st, map[string]any{"states.json": []novel.StoryState{
		formalState("state_rv", "c_hero", "physical", "伤势", "重伤"),
	}})

	changed := formalState("state_new", "c_hero", "physical", "伤势", "轻伤")
	cs := newChangeSet(1, updateStateOp(t, "state_rv", 1, changed))
	saveTestChangeSet(t, st, cs)
	if _, err := st.AcceptChangeSet(cs.ID); err != nil {
		t.Fatalf("accept: %v", err)
	}
	if got := memoryRevisionOf(t, st); got != 2 {
		t.Fatalf("revision = %d, want 2", got)
	}
	_, states, _, _, _, err := st.LoadMemoryRecords()
	if err != nil {
		t.Fatalf("LoadMemoryRecords: %v", err)
	}
	if old := findState(states, "state_rv"); old == nil || old.Status != novel.StateSuperseded || old.RecordVersion != 2 {
		t.Errorf("old state = %+v", old)
	}
	var active *novel.StoryState
	for i := range states {
		if states[i].Status == novel.StateActive {
			active = &states[i]
		}
	}
	if active == nil || active.RecordVersion != 1 || active.Value != "轻伤" {
		t.Errorf("new active state = %+v", active)
	}

	statesBefore := len(states)
	stale := newChangeSet(2, updateStateOp(t, "state_rv", 1, changed))
	saveTestChangeSet(t, st, stale)
	if _, err := st.AcceptChangeSet(stale.ID); !errors.Is(err, ErrOptimisticConflict) {
		t.Fatalf("stale accept error = %v, want ErrOptimisticConflict", err)
	}
	if got := memoryRevisionOf(t, st); got != 2 {
		t.Errorf("revision changed on optimistic conflict: %d", got)
	}
	_, after, _, _, _, err := st.LoadMemoryRecords()
	if err != nil {
		t.Fatalf("LoadMemoryRecords: %v", err)
	}
	if len(after) != statesBefore {
		t.Errorf("states changed on optimistic conflict: %d -> %d", statesBefore, len(after))
	}
}

// TestAcceptExpiredBaselineThenRebase 覆盖 U31：过期基线 accept → revision_conflict 零落盘；rebase 后成功。
func TestAcceptExpiredBaselineThenRebase(t *testing.T) {
	st, _ := newTestStore(t)
	seedFormalCommit(t, st, map[string]any{"states.json": []novel.StoryState{
		formalState("state_base", "c_hero", "physical", "伤势", "重伤"),
	}})

	cs := newChangeSet(0, addStateOp(t, formalState("state_added", "c_hero", "location", "位置", "山门")))
	saveTestChangeSet(t, st, cs)
	if _, err := st.AcceptChangeSet(cs.ID); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("expired accept error = %v, want ErrRevisionConflict", err)
	}
	if got := memoryRevisionOf(t, st); got != 1 {
		t.Errorf("revision = %d, want 1", got)
	}
	_, states, _, _, _, err := st.LoadMemoryRecords()
	if err != nil {
		t.Fatalf("LoadMemoryRecords: %v", err)
	}
	if len(states) != 1 {
		t.Errorf("states after conflict = %d, want 1", len(states))
	}

	rebased, err := st.RebaseChangeSet(cs.ID)
	if err != nil {
		t.Fatalf("RebaseChangeSet: %v", err)
	}
	if rebased.BasedOnRevision != 1 {
		t.Fatalf("rebased baseline = %d, want 1", rebased.BasedOnRevision)
	}
	if _, err := st.AcceptChangeSet(cs.ID); err != nil {
		t.Fatalf("accept after rebase: %v", err)
	}
	if got := memoryRevisionOf(t, st); got != 2 {
		t.Errorf("revision after rebase accept = %d, want 2", got)
	}
}

// TestAcceptIdempotentReplay 覆盖 I2-A：二次 accept 返回原 commitId，不重复提交、不重复事件。
func TestAcceptIdempotentReplay(t *testing.T) {
	st, _ := newTestStore(t)
	cs := newChangeSet(0, addStateOp(t, formalState("state_once", "c_hero", "physical", "伤势", "重伤")))
	saveTestChangeSet(t, st, cs)

	first, err := st.AcceptChangeSet(cs.ID)
	if err != nil {
		t.Fatalf("first accept: %v", err)
	}
	if first.CommitID == "" || first.MemoryRevision != 1 {
		t.Fatalf("first result = %+v", first)
	}
	second, err := st.AcceptChangeSet(cs.ID)
	if err != nil {
		t.Fatalf("second accept: %v", err)
	}
	if second.CommitID != first.CommitID || second.MemoryRevision != 1 {
		t.Errorf("second result = %+v, want replay of %+v", second, first)
	}
	_, states, _, _, _, err := st.LoadMemoryRecords()
	if err != nil {
		t.Fatalf("LoadMemoryRecords: %v", err)
	}
	if len(states) != 1 {
		t.Errorf("states = %d, want 1", len(states))
	}
}

// TestAcceptManualEditConflict 覆盖 I2-B 提交时点：目标为 manual_edit → 冲突、不覆盖、无提交。
func TestAcceptManualEditConflict(t *testing.T) {
	st, _ := newTestStore(t)
	manual := formalState("state_manual", "c_hero", "physical", "伤势", "轻伤")
	manual.LastSource = novel.LastSourceManualEdit
	manual.Origin = novel.Origin{Type: novel.OriginManual}
	seedFormalCommit(t, st, map[string]any{"states.json": []novel.StoryState{manual}})

	proposed := formalState("state_manual", "c_hero", "physical", "伤势", "重伤")
	cs := newChangeSet(1, updateStateOp(t, "state_manual", 1, proposed))
	saveTestChangeSet(t, st, cs)
	result, err := st.AcceptChangeSet(cs.ID)
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	if result.Status != string(novel.ChangeSetApplied) || result.CommitID != "" || result.MemoryRevision != 1 {
		t.Errorf("result = %+v, want applied without commit", result)
	}
	_, states, _, _, _, err := st.LoadMemoryRecords()
	if err != nil {
		t.Fatalf("LoadMemoryRecords: %v", err)
	}
	if len(states) != 1 || states[0].Value != "轻伤" || states[0].LastSource != novel.LastSourceManualEdit {
		t.Errorf("manual state overwritten: %+v", states)
	}
	stored, err := st.GetChangeSet(cs.ID)
	if err != nil {
		t.Fatalf("GetChangeSet: %v", err)
	}
	found := false
	for _, conflict := range stored.Conflicts {
		if conflict.Kind == novel.ConflictManualEdit && conflict.TargetID == "state_manual" {
			found = true
		}
	}
	if !found {
		t.Errorf("manual_edit conflict not retained: %+v", stored.Conflicts)
	}
}

// TestAcceptEmptyOpsAppliedWithoutCommit 覆盖 I2-F：空 ops → 直接 applied、无 CommitRecord、不递增。
func TestAcceptEmptyOpsAppliedWithoutCommit(t *testing.T) {
	st, dir := newTestStore(t)
	cs := newChangeSet(0)
	saveTestChangeSet(t, st, cs)

	result, err := st.AcceptChangeSet(cs.ID)
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	if result.Status != string(novel.ChangeSetApplied) || result.CommitID != "" || result.MemoryRevision != 0 {
		t.Errorf("result = %+v", result)
	}
	if _, err := os.ReadFile(filepath.Join(dir, memoryDirName, "commits.json")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("commits.json should not exist for empty accept, err=%v", err)
	}
}

// TestAcceptRejectedWhileAnalysisRunning 覆盖 I2-G：分析运行中 accept → ErrAnalysisRunning 零写入。
func TestAcceptRejectedWhileAnalysisRunning(t *testing.T) {
	st, _ := newTestStore(t)
	project, err := st.ImportChapters("书", []novel.Chapter{{Title: "一", Content: "内容。"}})
	if err != nil {
		t.Fatalf("ImportChapters: %v", err)
	}
	cs := newChangeSet(0, addStateOp(t, formalState("state_g", "c_hero", "physical", "伤势", "重伤")))
	saveTestChangeSet(t, st, cs)

	if _, _, err := st.CommitAnalysis(project.Revision, 0, novel.NewKnowledge()); err != nil {
		t.Fatalf("CommitAnalysis: %v", err)
	}
	if _, err := st.AcceptChangeSet(cs.ID); !errors.Is(err, ErrAnalysisRunning) {
		t.Fatalf("accept error = %v, want ErrAnalysisRunning", err)
	}
	if got := memoryRevisionOf(t, st); got != 0 {
		t.Errorf("revision = %d, want 0", got)
	}
	_, states, _, _, _, err := st.LoadMemoryRecords()
	if err != nil {
		t.Fatalf("LoadMemoryRecords: %v", err)
	}
	if len(states) != 0 {
		t.Errorf("states written while analysis running: %+v", states)
	}
}

// TestCorruptChangeSetsDraftRejectedFormalReadable 覆盖 I2-H：changesets.json 损坏 → 草稿区写操作报错，正式区可读。
func TestCorruptChangeSetsDraftRejectedFormalReadable(t *testing.T) {
	st, dir := newTestStore(t)
	seedFormalCommit(t, st, map[string]any{"states.json": []novel.StoryState{
		formalState("state_ok", "c_hero", "physical", "伤势", "重伤"),
	}})
	if err := os.WriteFile(filepath.Join(dir, memoryDirName, "changesets.json"), []byte("{corrupt"), 0o644); err != nil {
		t.Fatalf("corrupt changesets: %v", err)
	}

	if _, err := st.LoadChangeSets(""); !errors.Is(err, ErrChangeSetsCorrupted) {
		t.Errorf("LoadChangeSets error = %v", err)
	}
	cs := newChangeSet(1)
	if err := st.SaveChangeSet(cs, nil); !errors.Is(err, ErrChangeSetsCorrupted) {
		t.Errorf("SaveChangeSet error = %v", err)
	}
	if _, err := st.AcceptChangeSet(cs.ID); !errors.Is(err, ErrChangeSetsCorrupted) {
		t.Errorf("AcceptChangeSet error = %v", err)
	}
	if err := st.RejectChangeSet(cs.ID, "x"); !errors.Is(err, ErrChangeSetsCorrupted) {
		t.Errorf("RejectChangeSet error = %v", err)
	}
	_, states, _, _, _, err := st.LoadMemoryRecords()
	if err != nil {
		t.Fatalf("formal read failed on corrupt changesets: %v", err)
	}
	if len(states) != 1 {
		t.Errorf("formal states = %d, want 1", len(states))
	}
}

// TestExtractTokenInvalidatedByImport 覆盖 I2-E（前半）：章节快照后导入新书 → 暂存零写入、终态零回写。
func TestExtractTokenInvalidatedByImport(t *testing.T) {
	st, _ := newTestStore(t)
	if _, err := st.ImportChapters("书", []novel.Chapter{{Title: "一", Content: "内容。"}}); err != nil {
		t.Fatalf("ImportChapters: %v", err)
	}
	writeKnowledgeDirect(t, st, novel.Knowledge{Characters: []novel.Character{{ID: "c_hero", Name: "林越"}}})
	if _, err := st.SaveConfig(Config{BaseURL: "http://localhost:11434/v1", Model: "test-model"}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	status, token, err := st.BeginExtract(nil, false)
	if err != nil {
		t.Fatalf("BeginExtract: %v", err)
	}
	if status != ExtractRunning || len(token.Scope) != 1 {
		t.Fatalf("begin = %s scope=%v", status, token.Scope)
	}

	if _, err := st.ImportChapters("新书", []novel.Chapter{{Title: "一", Content: "新内容。"}}); err != nil {
		t.Fatalf("second import: %v", err)
	}
	if err := st.StashExtractResult(token, newChangeSet(0), nil); err == nil || !strings.Contains(err.Error(), "invalidated") {
		t.Fatalf("stash error = %v, want invalidated", err)
	}
	if got := memoryRevisionOf(t, st); got != 0 {
		t.Errorf("revision = %d, want 0", got)
	}
	sets, err := st.LoadChangeSets("")
	if err != nil {
		t.Fatalf("LoadChangeSets: %v", err)
	}
	if len(sets) != 0 {
		t.Errorf("changesets after invalidation = %+v", sets)
	}
	if err := st.FinishExtract(token, ExtractDone, ExtractStats{}, ""); err != nil {
		t.Fatalf("FinishExtract: %v", err)
	}
	state, err := st.LoadExtractState()
	if err != nil {
		t.Fatalf("LoadExtractState: %v", err)
	}
	if state.Status != ExtractIdle {
		t.Errorf("extract state after import = %s, want idle", state.Status)
	}
}

// TestOrphanExtractingResetOnRestart 覆盖 I2-E（后半）：重启后孤儿 extracting → failed + interrupted_by_restart。
func TestOrphanExtractingResetOnRestart(t *testing.T) {
	dir := t.TempDir()
	if _, err := New(dir); err != nil {
		t.Fatalf("New: %v", err)
	}
	state := ExtractState{Status: ExtractRunning, RunID: 7, Scope: []string{"ch_x"}}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("marshal extract state: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, memoryDirName), 0o755); err != nil {
		t.Fatalf("mkdir memory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, memoryDirName, extractStateFile), raw, 0o644); err != nil {
		t.Fatalf("write extract state: %v", err)
	}

	restarted, err := New(dir)
	if err != nil {
		t.Fatalf("restart New: %v", err)
	}
	got, err := restarted.LoadExtractState()
	if err != nil {
		t.Fatalf("LoadExtractState: %v", err)
	}
	if got.Status != ExtractFailed || got.LastError != "interrupted_by_restart" {
		t.Errorf("restarted state = %+v", got)
	}
}

// TestBeginExtractForceAlwaysSkipsAppliedSameKey 覆盖 D2/§3.2：force 只绕过 pending，已 applied 同键始终跳过。
func TestBeginExtractForceAlwaysSkipsAppliedSameKey(t *testing.T) {
	st, _ := newTestStore(t)
	project, err := st.ImportChapters("书", []novel.Chapter{{Title: "一", Content: "内容。"}})
	if err != nil {
		t.Fatalf("ImportChapters: %v", err)
	}
	writeKnowledgeDirect(t, st, novel.Knowledge{Characters: []novel.Character{{ID: "c_hero", Name: "林越"}}})
	if _, err := st.SaveConfig(Config{BaseURL: "http://localhost:11434/v1", Model: "test-model"}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	chapterID := project.Chapters[0].ID
	applied := newChangeSet(0)
	applied.Status = novel.ChangeSetApplied
	// D3：同键判定走结构化溯源字段，不再依赖 Reason 文本。
	applied.ChapterID = chapterID
	applied.ChapterRevision = project.Chapters[0].Revision
	applied.Reason = "深度提取（已提交）"
	saveTestChangeSet(t, st, applied)

	status, token, err := st.BeginExtract(nil, true)
	if err != nil {
		t.Fatalf("BeginExtract force: %v", err)
	}
	if status != ExtractDone || len(token.Scope) != 0 {
		t.Fatalf("force over applied: status=%s scope=%v, want done/empty", status, token.Scope)
	}

	second, project, err := st.AppendChapter("二", "新内容。")
	if err != nil {
		t.Fatalf("AppendChapter: %v", err)
	}
	pending := newChangeSet(0)
	pending.ChapterID = second.ID
	pending.ChapterRevision = project.Chapters[1].Revision
	pending.Reason = "深度提取（待审）"
	saveTestChangeSet(t, st, pending)
	if status, token, err = st.BeginExtract(nil, true); err != nil {
		t.Fatalf("BeginExtract force over pending: %v", err)
	}
	if status != ExtractRunning || len(token.Scope) != 1 || token.Scope[0] != second.ID {
		t.Fatalf("force over pending: status=%s scope=%v, want running/[%s]", status, token.Scope, second.ID)
	}
}

// TestChangeSetCoversChapterStructuredAndLegacy 覆盖 D3：结构化字段优先；
// 仅旧数据缺失结构化字段时才回退解析 Reason。
func TestChangeSetCoversChapterStructuredAndLegacy(t *testing.T) {
	structured := novel.ChangeSet{ChapterID: "ch_a", ChapterRevision: 3, Reason: "任意说明"}
	if !changeSetCoversChapter(structured, "ch_a", 3) {
		t.Error("structured same key should match")
	}
	if changeSetCoversChapter(structured, "ch_a", 4) || changeSetCoversChapter(structured, "ch_b", 3) {
		t.Error("structured mismatched key must not match")
	}
	// 结构化字段存在时，即使 Reason 指向另一章也不得回退（防旧文本污染判定）。
	structured.Reason = "chapter ch_b rev 1 深度提取"
	if changeSetCoversChapter(structured, "ch_b", 1) {
		t.Error("structured provenance must not fall back to Reason")
	}

	legacy := novel.ChangeSet{Reason: "chapter ch_a rev 3 深度提取"}
	if !changeSetCoversChapter(legacy, "ch_a", 3) {
		t.Error("legacy reason should match")
	}
	if changeSetCoversChapter(legacy, "ch_a", 2) {
		t.Error("legacy mismatched revision must not match")
	}
	if changeSetCoversChapter(novel.ChangeSet{}, "ch_a", 3) {
		t.Error("empty provenance must not match")
	}
}

// TestValidateChangeSetChapterProvenance 覆盖 D3 校验：非 ch_ 前缀或非正修订号拒绝。
func TestValidateChangeSetChapterProvenance(t *testing.T) {
	valid := newChangeSet(0)
	valid.ChapterID = "ch_a"
	valid.ChapterRevision = 1
	if err := novel.ValidateChangeSet(&valid); err != nil {
		t.Fatalf("valid provenance rejected: %v", err)
	}
	badID := newChangeSet(0)
	badID.ChapterID = "chapter_a"
	badID.ChapterRevision = 1
	if err := novel.ValidateChangeSet(&badID); !errors.Is(err, novel.ErrChangeSetValidation) {
		t.Errorf("bad chapterId error = %v, want ErrChangeSetValidation", err)
	}
	badRev := newChangeSet(0)
	badRev.ChapterID = "ch_a"
	if err := novel.ValidateChangeSet(&badRev); !errors.Is(err, novel.ErrChangeSetValidation) {
		t.Errorf("zero chapterRevision error = %v, want ErrChangeSetValidation", err)
	}
}

func eventRaw(t *testing.T, event novel.StoryEvent) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	return raw
}

// TestLoadPendingEventRecordsOnlyProposed 覆盖 D7：只返回 proposed 批次的 add_event，
// 并按章节过滤；applied/rejected 与其他章节不参与去重。
func TestLoadPendingEventRecordsOnlyProposed(t *testing.T) {
	st, _ := newTestStore(t)
	addEventOp := func(event novel.StoryEvent) novel.ChangeOp {
		return novel.ChangeOp{Op: novel.OpAddEvent, Record: eventRaw(t, event)}
	}
	proposed := newChangeSet(0, addEventOp(novel.StoryEvent{ID: "evt_pending", ChapterID: "ch_a"}))
	saveTestChangeSet(t, st, proposed)
	applied := newChangeSet(0, addEventOp(novel.StoryEvent{ID: "evt_applied", ChapterID: "ch_a"}))
	applied.Status = novel.ChangeSetApplied
	saveTestChangeSet(t, st, applied)
	other := newChangeSet(0, addEventOp(novel.StoryEvent{ID: "evt_other", ChapterID: "ch_b"}))
	saveTestChangeSet(t, st, other)

	events, err := st.LoadPendingEventRecords("ch_a")
	if err != nil {
		t.Fatalf("LoadPendingEventRecords: %v", err)
	}
	if len(events) != 1 || events[0].ID != "evt_pending" {
		t.Fatalf("pending events = %+v, want only evt_pending", events)
	}

	empty, err := st.LoadPendingEventRecords("ch_missing")
	if err != nil {
		t.Fatalf("LoadPendingEventRecords missing: %v", err)
	}
	if len(empty) != 0 {
		t.Errorf("events for unknown chapter = %+v", empty)
	}
}

func saveTestCandidates(t *testing.T, st *Store, cands ...novel.EntityCandidate) {
	t.Helper()
	if err := st.SaveChangeSet(newChangeSet(0), cands); err != nil {
		t.Fatalf("SaveChangeSet candidates: %v", err)
	}
}

// TestPrewriteCandidateKnowledgeRegisterDedupIdempotent 覆盖 D5：register 预写先查重，
// 命中既有实体则复用 ID 并并入别名；重复预写不产生重复实体。
func TestPrewriteCandidateKnowledgeRegisterDedupIdempotent(t *testing.T) {
	st, _ := newTestStore(t)
	writeKnowledgeDirect(t, st, knowledgeOf(
		novel.Character{ID: "c_hero", Name: "林越", Aliases: []string{"阿青"}},
	))

	cand := novel.EntityCandidate{Name: "阿越", Aliases: []string{"林越"}}
	firstID, err := st.prewriteCandidateKnowledge(cand, "register", "")
	if err != nil {
		t.Fatalf("first prewrite: %v", err)
	}
	if firstID != "c_hero" {
		t.Errorf("dedup target = %q, want c_hero", firstID)
	}
	secondID, err := st.prewriteCandidateKnowledge(cand, "register", "")
	if err != nil {
		t.Fatalf("second prewrite: %v", err)
	}
	if secondID != firstID {
		t.Errorf("idempotent prewrite target = %q, want %q", secondID, firstID)
	}

	knowledge, err := st.LoadKnowledge()
	if err != nil {
		t.Fatalf("LoadKnowledge: %v", err)
	}
	if len(knowledge.Characters) != 1 {
		t.Fatalf("duplicate characters created: %+v", knowledge.Characters)
	}
	count := 0
	for _, alias := range knowledge.Characters[0].Aliases {
		if alias == "阿越" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("alias 阿越 appears %d times: %+v", count, knowledge.Characters[0].Aliases)
	}
	for _, alias := range knowledge.Characters[0].Aliases {
		if alias == "林越" {
			t.Errorf("existing name leaked into aliases: %+v", knowledge.Characters[0].Aliases)
		}
	}
}

// TestResolveCandidateRegisterRecordsEntityID 覆盖 D6：新建实体时 Resolution 记录最终 c_ ID。
func TestResolveCandidateRegisterRecordsEntityID(t *testing.T) {
	st, _ := newTestStore(t)
	writeKnowledgeDirect(t, st, novel.NewKnowledge())
	cand := novel.EntityCandidate{ID: "cand_shen", Name: "沈青", Status: string(novel.CandidatePending)}
	saveTestCandidates(t, st, cand)

	resolved, err := st.ResolveCandidate(cand.ID, "register", "")
	if err != nil {
		t.Fatalf("ResolveCandidate: %v", err)
	}
	if resolved.Status != string(novel.CandidateRegister) {
		t.Fatalf("status = %q, want register", resolved.Status)
	}
	if !strings.HasPrefix(resolved.Resolution, "c_") {
		t.Fatalf("resolution = %q, want c_ id", resolved.Resolution)
	}
	knowledge, err := st.LoadKnowledge()
	if err != nil {
		t.Fatalf("LoadKnowledge: %v", err)
	}
	if len(knowledge.Characters) != 1 || knowledge.Characters[0].ID != resolved.Resolution || knowledge.Characters[0].Name != "沈青" {
		t.Errorf("knowledge characters = %+v", knowledge.Characters)
	}
	if got := memoryRevisionOf(t, st); got != 1 {
		t.Errorf("memory revision = %d, want 1", got)
	}
}

// TestResolveCandidateRegisterAmbiguousRejected 覆盖 D5 歧义保护：
// 实体内部多命中或角色/世界条目交叉命中 → 拒绝，候选保持 pending、知识库零写入。
func TestResolveCandidateRegisterAmbiguousRejected(t *testing.T) {
	cases := []struct {
		name      string
		knowledge novel.Knowledge
		cand      novel.EntityCandidate
	}{
		{
			name: "multi_character",
			knowledge: knowledgeOf(
				novel.Character{ID: "c_a", Name: "林越"},
				novel.Character{ID: "c_b", Name: "苏青"},
			),
			cand: novel.EntityCandidate{ID: "cand_multi", Name: "林越", Aliases: []string{"苏青"}, Status: string(novel.CandidatePending)},
		},
		{
			name:      "character_world_cross",
			knowledge: knowledgeOf(novel.Character{ID: "c_a", Name: "林越"}),
			cand:      novel.EntityCandidate{ID: "cand_cross", Name: "林越", Aliases: []string{"山门"}, Status: string(novel.CandidatePending)},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.name == "character_world_cross" {
				tc.knowledge.World = []novel.WorldEntry{{ID: "w_gate", Title: "山门"}}
			}
			st, _ := newTestStore(t)
			writeKnowledgeDirect(t, st, tc.knowledge)
			saveTestCandidates(t, st, tc.cand)

			if _, err := st.ResolveCandidate(tc.cand.ID, "register", ""); !errors.Is(err, ErrCandidateResolutionInvalid) {
				t.Fatalf("error = %v, want ErrCandidateResolutionInvalid", err)
			}
			if got := memoryRevisionOf(t, st); got != 0 {
				t.Errorf("revision = %d, want 0", got)
			}
			pending, err := st.LoadCandidates(string(novel.CandidatePending))
			if err != nil {
				t.Fatalf("LoadCandidates: %v", err)
			}
			if len(pending) != 1 || pending[0].Status != string(novel.CandidatePending) {
				t.Errorf("candidate mutated on rejection: %+v", pending)
			}
			knowledge, err := st.LoadKnowledge()
			if err != nil {
				t.Fatalf("LoadKnowledge: %v", err)
			}
			if len(knowledge.Characters) != len(tc.knowledge.Characters) || len(knowledge.World) != len(tc.knowledge.World) {
				t.Errorf("knowledge mutated on rejection: chars=%d world=%d, want %d/%d", len(knowledge.Characters), len(knowledge.World), len(tc.knowledge.Characters), len(tc.knowledge.World))
			}
		})
	}
}

package analysis

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"novelagent/internal/llm"
	"novelagent/internal/novel"
	"novelagent/internal/store"
)

// 迭代 2 验收用例（iter2-详设 §10）：提取侧组装语义 U18/U19/U24/U29、I2-B/I2-C/I2-D
// 与整改项 B2/B3/D8。

func extractChapterFixture(content string) novel.Chapter {
	return novel.Chapter{ID: "ch_test01", Index: 1, Title: "第一章", Content: content, Revision: 1}
}

func knowledgeWith(chars ...novel.Character) novel.Knowledge {
	knowledge := novel.NewKnowledge()
	knowledge.Characters = chars
	return knowledge
}

func activeStateRecord(id, subject, category, attribute, value string) novel.StoryState {
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

func openThreadRecord(id, title string) novel.StoryThread {
	return novel.StoryThread{
		ID: id,
		MemoryMeta: novel.MemoryMeta{
			WorkID:        novel.InternalWorkID(),
			RecordVersion: 1,
			Origin:        novel.Origin{Type: novel.OriginBackfill},
			LastSource:    novel.LastSourceAgentExtraction,
		},
		Status: novel.ThreadOpen,
		Title:  title,
	}
}

func decodeStateOp(t *testing.T, raw json.RawMessage) novel.StoryState {
	t.Helper()
	var st novel.StoryState
	if err := json.Unmarshal(raw, &st); err != nil {
		t.Fatalf("decode state op: %v", err)
	}
	return st
}

func decodeEventOp(t *testing.T, raw json.RawMessage) novel.StoryEvent {
	t.Helper()
	var ev novel.StoryEvent
	if err := json.Unmarshal(raw, &ev); err != nil {
		t.Fatalf("decode event op: %v", err)
	}
	return ev
}

func decodeThreadOp(t *testing.T, raw json.RawMessage) novel.StoryThread {
	t.Helper()
	var th novel.StoryThread
	if err := json.Unmarshal(raw, &th); err != nil {
		t.Fatalf("decode thread op: %v", err)
	}
	return th
}

func decodeCognitionOp(t *testing.T, raw json.RawMessage) novel.Cognition {
	t.Helper()
	var cog novel.Cognition
	if err := json.Unmarshal(raw, &cog); err != nil {
		t.Fatalf("decode cognition op: %v", err)
	}
	return cog
}

// TestAssembleCognitionsProduced 覆盖 S3「cognitions 产出方」：
// 角色主观认知落 add_cognition，绑定同证据事件；未知主体转冲突，世界条目/非法状态跳过，重复项去重。
func TestAssembleCognitionsProduced(t *testing.T) {
	chapter := extractChapterFixture("铜牌是假的。林越认定铜牌是真的。苏青以为林越已死。")
	knowledge := novel.Knowledge{
		Characters: []novel.Character{
			{ID: "c_hero", Name: "林越"},
			{ID: "c_rival", Name: "苏青"},
		},
		World: []novel.WorldEntry{{ID: "w_bronze", Title: "铜牌"}},
	}
	parsed := extractResult{
		Events: []extractEvent{{
			Summary: "林越认定铜牌是真的", EventType: "other", Quote: "林越认定铜牌是真的", ParticipantNamesProvided: true, Participants: []string{"林越"},
		}},
		Cognitions: []extractCognition{
			{Subject: "林越", Content: "认为铜牌是真的", Status: "wrong", Quote: "林越认定铜牌是真的"},
			{Subject: "林越", Content: "认为铜牌是真的", Status: "wrong", Quote: "林越认定铜牌是真的"},
			{Subject: "苏青", Content: "以为林越已死", Status: "known", Quote: "苏青以为林越已死"},
			{Subject: "黑衣人", Content: "以为无人跟踪", Status: "known", Quote: "苏青以为林越已死"},
			{Subject: "铜牌", Content: "被人藏起", Status: "known", Quote: "铜牌是假的"},
			{Subject: "苏青", Content: "状态非法", Status: "bogus", Quote: "苏青以为林越已死"},
		},
	}

	cs, candidates := assembleChapterChangeSet(chapter, knowledge, parsed, nil, nil, nil, 0)

	var cogs []novel.Cognition
	for _, op := range cs.Ops {
		if op.Op == novel.OpAddCognition {
			cogs = append(cogs, decodeCognitionOp(t, op.Record))
		}
	}
	if len(cogs) != 2 {
		t.Fatalf("cognition ops = %d, want 2 (dup/world/invalid skipped): %+v", len(cogs), cogs)
	}
	first := cogs[0]
	if first.SubjectID != "c_hero" || first.Status != novel.CogWrong || first.Content != "认为铜牌是真的" {
		t.Errorf("first cognition = %+v", first)
	}
	if len(first.SourceRefs) != 1 || first.SourceRefs[0].ChapterID != chapter.ID || first.SourceRefs[0].Quote != "林越认定铜牌是真的" {
		t.Errorf("first cognition refs = %+v", first.SourceRefs)
	}
	if first.KnownSinceEvent == "" || !strings.HasPrefix(first.KnownSinceEvent, "evt_") {
		t.Errorf("first cognition knownSinceEvent = %q, want bound event", first.KnownSinceEvent)
	}
	second := cogs[1]
	if second.SubjectID != "c_rival" || second.Status != novel.CogKnown || second.KnownSinceEvent != "" {
		t.Errorf("second cognition = %+v", second)
	}
	if len(cs.Conflicts) != 1 || cs.Conflicts[0].Kind != novel.ConflictAmbiguous || !strings.Contains(cs.Conflicts[0].Detail, "黑衣人") {
		t.Errorf("cognition conflicts = %+v", cs.Conflicts)
	}
	if len(candidates) != 1 || candidates[0].Name != "黑衣人" || candidates[0].Reason != "unknown_entity" {
		t.Errorf("cognition candidates = %+v", candidates)
	}
}

// TestAssembleCognitionWithoutQuoteNeedsReview 覆盖证据红线：认知 quote 无法定位 → needsReview=evidence_unlocated。
func TestAssembleCognitionWithoutQuoteNeedsReview(t *testing.T) {
	chapter := extractChapterFixture("苏青以为林越已死。")
	knowledge := knowledgeWith(novel.Character{ID: "c_rival", Name: "苏青"})
	parsed := extractResult{Cognitions: []extractCognition{{
		Subject: "苏青", Content: "以为林越已死", Status: "known", Quote: "正文中不存在这句话",
	}}}

	cs, _ := assembleChapterChangeSet(chapter, knowledge, parsed, nil, nil, nil, 0)
	if len(cs.Ops) != 1 || cs.Ops[0].Op != novel.OpAddCognition {
		t.Fatalf("ops = %+v, want one add_cognition", cs.Ops)
	}
	cog := decodeCognitionOp(t, cs.Ops[0].Record)
	if !cog.NeedsReview || cog.ReviewReason != "evidence_unlocated" || len(cog.SourceRefs) != 0 {
		t.Errorf("unlocated cognition = %+v", cog)
	}
}

// TestAssembleStateSameValueSkipped 覆盖 U29 写入口：三元组与值均未变 → 不产出 op（recordVersion 不变）。
func TestAssembleStateSameValueSkipped(t *testing.T) {
	chapter := extractChapterFixture("林越仍受重伤。")
	knowledge := knowledgeWith(novel.Character{ID: "c_hero", Name: "林越"})
	existing := []novel.StoryState{activeStateRecord("state_old", "c_hero", "physical", "伤势", "重伤")}

	parsed := extractResult{States: []extractState{{
		Subject: "林越", Category: "physical", Attribute: "伤势", Value: "重伤", Quote: "林越仍受重伤",
	}}}
	cs, _ := assembleChapterChangeSet(chapter, knowledge, parsed, nil, existing, nil, 1)
	if len(cs.Ops) != 0 || len(cs.Conflicts) != 0 {
		t.Fatalf("same-value state produced ops=%d conflicts=%d, want 0/0", len(cs.Ops), len(cs.Conflicts))
	}

	// 值变化 → update_state，带乐观版本。
	parsed.States[0].Value = "轻伤"
	cs, _ = assembleChapterChangeSet(chapter, knowledge, parsed, nil, existing, nil, 1)
	if len(cs.Ops) != 1 || cs.Ops[0].Op != novel.OpUpdateState {
		t.Fatalf("changed state ops = %+v, want one update_state", cs.Ops)
	}
	if cs.Ops[0].TargetID != "state_old" || cs.Ops[0].ExpectedRecordVersion != 1 {
		t.Errorf("update target/version = %q/%d", cs.Ops[0].TargetID, cs.Ops[0].ExpectedRecordVersion)
	}
}

// TestAssembleUnknownSubjectBecomesConflict 覆盖 B3：未知/歧义主体状态转 ConflictItem，不进入 Ops。
func TestAssembleUnknownSubjectBecomesConflict(t *testing.T) {
	chapter := extractChapterFixture("黑衣人站在雨里。")
	knowledge := knowledgeWith(novel.Character{ID: "c_hero", Name: "林越"})
	parsed := extractResult{States: []extractState{
		{Subject: "黑衣人", Category: "physical", Attribute: "状态", Value: "受伤", Quote: "黑衣人站在雨里"},
		{Subject: "黑衣人", Category: "", Attribute: "状态", Value: "受伤"},
	}}

	cs, candidates := assembleChapterChangeSet(chapter, knowledge, parsed, nil, nil, nil, 0)
	if len(cs.Ops) != 0 {
		t.Fatalf("unknown subject leaked into ops: %+v", cs.Ops)
	}
	if len(cs.Conflicts) != 1 {
		t.Fatalf("conflicts = %d, want 1 (empty category entry must be dropped)", len(cs.Conflicts))
	}
	if cs.Conflicts[0].Kind != novel.ConflictAmbiguous || !strings.Contains(cs.Conflicts[0].Detail, "黑衣人") {
		t.Errorf("conflict = %+v", cs.Conflicts[0])
	}
	if len(candidates) != 1 || candidates[0].Name != "黑衣人" || candidates[0].Reason != "unknown_entity" {
		t.Errorf("candidates = %+v", candidates)
	}

	// 歧义主体（两个实体的别名相交）同样转冲突。
	ambiguousKnowledge := knowledgeWith(
		novel.Character{ID: "c_hero", Name: "林越", Aliases: []string{"阿青"}},
		novel.Character{ID: "c_rival", Name: "苏青", Aliases: []string{"阿青"}},
	)
	parsed = extractResult{States: []extractState{{Subject: "阿青", Category: "physical", Attribute: "伤势", Value: "重伤"}}}
	cs, candidates = assembleChapterChangeSet(chapter, ambiguousKnowledge, parsed, nil, nil, nil, 0)
	if len(cs.Ops) != 0 || len(cs.Conflicts) != 1 || cs.Conflicts[0].Kind != novel.ConflictAmbiguous {
		t.Fatalf("ambiguous subject ops=%d conflicts=%+v", len(cs.Ops), cs.Conflicts)
	}
	if len(candidates) != 1 || candidates[0].Reason != "ambiguous_alias" {
		t.Errorf("ambiguous candidates = %+v", candidates)
	}
}

// TestAssembleStateCrossCategoryAdds 覆盖 U18 写入口（暂存侧）：同属性不同 category → add_state 共存。
func TestAssembleStateCrossCategoryAdds(t *testing.T) {
	chapter := extractChapterFixture("两人从此敌对，林越已重伤。")
	knowledge := knowledgeWith(novel.Character{ID: "c_hero", Name: "林越"})
	existing := []novel.StoryState{activeStateRecord("state_old", "c_hero", "physical", "状态", "重伤")}

	parsed := extractResult{States: []extractState{{
		Subject: "林越", Category: "relation", Attribute: "状态", Value: "敌对", Quote: "两人从此敌对",
	}}}
	cs, _ := assembleChapterChangeSet(chapter, knowledge, parsed, nil, existing, nil, 1)
	if len(cs.Ops) != 1 || cs.Ops[0].Op != novel.OpAddState {
		t.Fatalf("ops = %+v, want add_state", cs.Ops)
	}
	if got := decodeStateOp(t, cs.Ops[0].Record); got.Category != "relation" || got.SubjectID != "c_hero" {
		t.Errorf("state record = %+v", got)
	}
}

// TestAssembleParticipantsKnown 覆盖 U19：显式无参与者 → participantsKnown=true 且空列表；
// 未识别参与者 → participantsKnown=false + 候选。
func TestAssembleParticipantsKnown(t *testing.T) {
	chapter := extractChapterFixture("林越独自离开。")
	knowledge := knowledgeWith(novel.Character{ID: "c_hero", Name: "林越"})
	parsed := extractResult{Events: []extractEvent{
		{Summary: "林越离开", EventType: "character_action", ParticipantNamesProvided: true},
		{Summary: "神秘人现身", EventType: "other", Participants: []string{"无名剑客"}, ParticipantNamesProvided: true},
		{Summary: "非法类型事件", EventType: "not_a_type", ParticipantNamesProvided: true},
	}}

	cs, candidates := assembleChapterChangeSet(chapter, knowledge, parsed, nil, nil, nil, 1)
	if len(cs.Ops) != 2 {
		t.Fatalf("ops = %d, want 2 (invalid eventType dropped)", len(cs.Ops))
	}
	first := decodeEventOp(t, cs.Ops[0].Record)
	if !first.ParticipantsKnown || len(first.Participants) != 0 {
		t.Errorf("explicit no-participants = known:%v participants:%v", first.ParticipantsKnown, first.Participants)
	}
	second := decodeEventOp(t, cs.Ops[1].Record)
	if second.ParticipantsKnown || len(second.Participants) != 0 {
		t.Errorf("unknown participant = known:%v participants:%v", second.ParticipantsKnown, second.Participants)
	}
	if len(candidates) != 1 || candidates[0].Name != "无名剑客" || candidates[0].Reason != "unknown_entity" {
		t.Errorf("candidates = %+v", candidates)
	}
}

// TestAssembleAmbiguousParticipant 覆盖 U24 提取侧：歧义参与者剔除 + needsReview + 候选。
func TestAssembleAmbiguousParticipant(t *testing.T) {
	chapter := extractChapterFixture("阿青转身离去。")
	knowledge := knowledgeWith(
		novel.Character{ID: "c_hero", Name: "林越", Aliases: []string{"阿青"}},
		novel.Character{ID: "c_rival", Name: "苏青", Aliases: []string{"阿青"}},
	)
	parsed := extractResult{Events: []extractEvent{{
		Summary: "阿青离开", EventType: "character_action",
		Participants: []string{"阿青"}, ParticipantNamesProvided: true, Quote: "阿青转身离去",
	}}}

	cs, candidates := assembleChapterChangeSet(chapter, knowledge, parsed, nil, nil, nil, 1)
	if len(cs.Ops) != 1 {
		t.Fatalf("ops = %+v", cs.Ops)
	}
	event := decodeEventOp(t, cs.Ops[0].Record)
	if len(event.Participants) != 0 {
		t.Errorf("ambiguous participant not removed: %v", event.Participants)
	}
	if !event.NeedsReview || event.ReviewReason != novel.ConflictAmbiguous {
		t.Errorf("event review = %v/%q", event.NeedsReview, event.ReviewReason)
	}
	if len(candidates) != 1 || candidates[0].Reason != "ambiguous_alias" {
		t.Errorf("candidates = %+v", candidates)
	}
}

// TestAssembleLocateQuoteRuneOffsets 覆盖 I2-C/D8：rune 偏移（含 emoji）、定位失败降级、多处命中记 evidenceNote。
func TestAssembleLocateQuoteRuneOffsets(t *testing.T) {
	chapter := extractChapterFixture("😀雨停了。他推门。")
	parsed := extractResult{Events: []extractEvent{
		{Summary: "他推门", EventType: "character_action", ParticipantNamesProvided: true, Quote: "他推门"},
		{Summary: "找不到原文", EventType: "other", ParticipantNamesProvided: true, Quote: "正文里没有这句"},
	}}
	cs, _ := assembleChapterChangeSet(chapter, knowledgeWith(), parsed, nil, nil, nil, 1)
	if len(cs.Ops) != 2 {
		t.Fatalf("ops = %d", len(cs.Ops))
	}
	located := decodeEventOp(t, cs.Ops[0].Record)
	if len(located.SourceRefs) != 1 {
		t.Fatalf("sourceRefs = %+v", located.SourceRefs)
	}
	ref := located.SourceRefs[0]
	if ref.StartOffset != 5 || ref.EndOffset != 8 || ref.OffsetUnit != "rune" || ref.ChapterRevision != 1 {
		t.Errorf("ref = %+v, want [5,8) rune rev1", ref)
	}
	unlocated := decodeEventOp(t, cs.Ops[1].Record)
	if len(unlocated.SourceRefs) != 0 || !unlocated.NeedsReview || unlocated.ReviewReason != "evidence_unlocated" {
		t.Errorf("unlocated event = refs:%v review:%v/%q", unlocated.SourceRefs, unlocated.NeedsReview, unlocated.ReviewReason)
	}

	// 同一 quote 多处命中：取第一处并记 evidenceNote。
	multi := extractChapterFixture("雨。雨。")
	parsed = extractResult{Events: []extractEvent{{
		Summary: "下雨", EventType: "world_change", ParticipantNamesProvided: true, Quote: "雨",
	}}}
	cs, _ = assembleChapterChangeSet(multi, knowledgeWith(), parsed, nil, nil, nil, 1)
	if len(cs.Ops) != 1 || cs.Ops[0].EvidenceNote != "quote_multi_hit" {
		t.Fatalf("multi-hit op = %+v", cs.Ops)
	}
	if refs := decodeEventOp(t, cs.Ops[0].Record).SourceRefs; len(refs) != 1 || refs[0].StartOffset != 0 {
		t.Errorf("multi-hit refs = %+v", refs)
	}
}

// TestAssembleResolvedThreadWithoutProof 覆盖 I2-D：无证据 resolve 降级 developing + 冲突项；
// 有证据但无关联事件同样降级（避免 accept 时 L-S 因缺 resolutionEvent 拒绝整章）。
func TestAssembleResolvedThreadWithoutProof(t *testing.T) {
	chapter := extractChapterFixture("老仆低声道：你才是少爷。")
	parsed := extractResult{Threads: []extractThread{{Title: "身世之谜", Action: "resolve"}}}
	cs, _ := assembleChapterChangeSet(chapter, knowledgeWith(), parsed, nil, nil, nil, 1)
	if len(cs.Ops) != 1 || cs.Ops[0].Op != novel.OpAddThread {
		t.Fatalf("ops = %+v", cs.Ops)
	}
	thread := decodeThreadOp(t, cs.Ops[0].Record)
	if thread.Status != novel.ThreadDeveloping || thread.ResolutionEvent != "" {
		t.Errorf("degraded thread = %+v", thread)
	}
	if len(cs.Conflicts) != 1 || cs.Conflicts[0].Kind != novel.ConflictResolvedNoProof {
		t.Fatalf("conflicts = %+v", cs.Conflicts)
	}

	// 有证据但本章没有可关联事件 → 仍降级并记冲突。
	parsed = extractResult{Threads: []extractThread{{Title: "身世之谜", Action: "resolve", Quote: "你才是少爷"}}}
	cs, _ = assembleChapterChangeSet(chapter, knowledgeWith(), parsed, nil, nil, nil, 1)
	thread = decodeThreadOp(t, cs.Ops[0].Record)
	if thread.Status != novel.ThreadDeveloping || len(cs.Conflicts) != 1 {
		t.Errorf("evidence-without-event thread = %+v conflicts=%+v", thread, cs.Conflicts)
	}

	// 同证据事件存在 → resolved 且绑定 resolutionEvent。
	parsed = extractResult{
		Events: []extractEvent{{
			Summary: "老仆揭穿身世", EventType: "info_reveal", ParticipantNamesProvided: true, Quote: "你才是少爷",
		}},
		Threads: []extractThread{{Title: "身世之谜", Action: "resolve", Quote: "你才是少爷"}},
	}
	cs, _ = assembleChapterChangeSet(chapter, knowledgeWith(), parsed, nil, nil, nil, 1)
	if len(cs.Ops) != 2 || len(cs.Conflicts) != 0 {
		t.Fatalf("resolved ops=%d conflicts=%+v", len(cs.Ops), cs.Conflicts)
	}
	event := decodeEventOp(t, cs.Ops[0].Record)
	thread = decodeThreadOp(t, cs.Ops[1].Record)
	if thread.Status != novel.ThreadResolved || thread.ResolutionEvent != event.ID {
		t.Errorf("resolved thread = %+v (event %s)", thread, event.ID)
	}
}

// TestAssembleManualEditStagingConflict 覆盖 I2-B 暂存时点：人工修订的状态/伏笔不被提取覆盖。
func TestAssembleManualEditStagingConflict(t *testing.T) {
	chapter := extractChapterFixture("林越的伤势好转，旧谜团有了新线索。")
	knowledge := knowledgeWith(novel.Character{ID: "c_hero", Name: "林越"})

	manualState := activeStateRecord("state_manual", "c_hero", "physical", "伤势", "轻伤")
	manualState.LastSource = novel.LastSourceManualEdit
	manualThread := openThreadRecord("thread_manual", "旧谜团")
	manualThread.LastSource = novel.LastSourceManualEdit

	parsed := extractResult{
		States:  []extractState{{Subject: "林越", Category: "physical", Attribute: "伤势", Value: "重伤", Quote: "林越的伤势好转"}},
		Threads: []extractThread{{Title: "旧谜团", Action: "update", Clue: "新线索", Quote: "旧谜团有了新线索"}},
	}
	cs, _ := assembleChapterChangeSet(chapter, knowledge, parsed, nil, []novel.StoryState{manualState}, []novel.StoryThread{manualThread}, 1)
	if len(cs.Ops) != 0 {
		t.Fatalf("manual-edited targets leaked into ops: %+v", cs.Ops)
	}
	manualConflicts := 0
	for _, c := range cs.Conflicts {
		if c.Kind == novel.ConflictManualEdit {
			manualConflicts++
		}
	}
	if manualConflicts != 2 {
		t.Fatalf("manual_edit conflicts = %d, conflicts=%+v", manualConflicts, cs.Conflicts)
	}
}

// TestExtractAmbiguousFullChain 覆盖 U24 全链路：暂存候选 → knowledge 不变 → 三动作 resolve。
func TestExtractAmbiguousFullChain(t *testing.T) {
	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	project, err := st.ImportChapters("书", []novel.Chapter{{Title: "一", Content: "阿青站在门前。"}})
	if err != nil {
		t.Fatalf("ImportChapters: %v", err)
	}
	knowledge := knowledgeWith(
		novel.Character{ID: "c_hero", Name: "林越", Aliases: []string{"阿青"}},
		novel.Character{ID: "c_rival", Name: "苏青", Aliases: []string{"阿青"}},
	)
	if _, _, err := st.CommitAnalysis(project.Revision, 0, knowledge); err != nil {
		t.Fatalf("CommitAnalysis: %v", err)
	}
	if _, _, err := st.FinishAnalysis(project.Revision, knowledge); err != nil {
		t.Fatalf("FinishAnalysis: %v", err)
	}
	chapter, err := st.LoadChapter(project.Chapters[0].ID)
	if err != nil {
		t.Fatalf("LoadChapter: %v", err)
	}

	parsed := extractResult{States: []extractState{{
		Subject: "阿青", Category: "physical", Attribute: "伤势", Value: "重伤", Quote: "阿青站在门前",
	}}}
	cs, candidates := assembleChapterChangeSet(chapter, knowledge, parsed, nil, nil, nil, 0)
	cs.ID = "mcs_ambiguous"
	if err := st.SaveChangeSet(cs, candidates); err != nil {
		t.Fatalf("SaveChangeSet: %v", err)
	}
	stored, err := st.GetChangeSet(cs.ID)
	if err != nil {
		t.Fatalf("GetChangeSet: %v", err)
	}
	if len(stored.Conflicts) != 1 || stored.Conflicts[0].Kind != novel.ConflictAmbiguous {
		t.Fatalf("stored conflicts = %+v", stored.Conflicts)
	}

	afterStash, err := st.LoadKnowledge()
	if err != nil {
		t.Fatalf("LoadKnowledge: %v", err)
	}
	if len(afterStash.Characters) != 2 {
		t.Fatalf("stash changed knowledge: %+v", afterStash.Characters)
	}
	pending, err := st.LoadCandidates("pending")
	if err != nil {
		t.Fatalf("LoadCandidates: %v", err)
	}
	if len(pending) != 1 || pending[0].Name != "阿青" || pending[0].Reason != "ambiguous_alias" {
		t.Fatalf("pending candidates = %+v", pending)
	}

	// 追加 register/merge 候选（同一草稿区路径）。
	extra := novel.ChangeSet{
		ID: "mcs_candidates", Status: novel.ChangeSetProposed, Proposer: "test",
		BasedOnRevision: 0, Conflicts: []novel.ConflictItem{},
	}
	extraCandidates := []novel.EntityCandidate{
		{Name: "顾长风", Reason: "unknown_entity", Status: string(novel.CandidatePending)},
		{Name: "越儿", Reason: "unknown_entity", Status: string(novel.CandidatePending)},
	}
	if err := st.SaveChangeSet(extra, extraCandidates); err != nil {
		t.Fatalf("SaveChangeSet extra: %v", err)
	}

	if _, err := st.ResolveCandidate(pending[0].ID, "dismiss", ""); err != nil {
		t.Fatalf("dismiss: %v", err)
	}
	if _, err := st.ResolveCandidate(pending[0].ID, "dismiss", ""); err == nil {
		t.Fatal("second dismiss must fail with ErrCandidateNotPending")
	}

	pending, _ = st.LoadCandidates("pending")
	byName := make(map[string]novel.EntityCandidate, len(pending))
	for _, c := range pending {
		byName[c.Name] = c
	}
	registerCand, ok := byName["顾长风"]
	if !ok {
		t.Fatalf("register candidate missing: %+v", pending)
	}
	if _, err := st.ResolveCandidate(registerCand.ID, "register", ""); err != nil {
		t.Fatalf("register: %v", err)
	}
	mergeCand, ok := byName["越儿"]
	if !ok {
		t.Fatalf("merge candidate missing: %+v", pending)
	}
	if _, err := st.ResolveCandidate(mergeCand.ID, "merge", ""); err == nil {
		t.Fatal("merge without resolution must fail")
	}
	if _, err := st.ResolveCandidate(mergeCand.ID, "merge", "c_hero"); err != nil {
		t.Fatalf("merge: %v", err)
	}

	resolved, err := st.LoadKnowledge()
	if err != nil {
		t.Fatalf("LoadKnowledge after resolve: %v", err)
	}
	if len(resolved.Characters) != 3 {
		t.Fatalf("register did not add character: %+v", resolved.Characters)
	}
	if resolved.Characters[2].Name != "顾长风" {
		t.Errorf("registered character = %+v", resolved.Characters[2])
	}
	if !containsString(resolved.Characters[0].Aliases, "越儿") {
		t.Errorf("merge did not add alias: %+v", resolved.Characters[0])
	}
}

func containsString(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}

// TestAssemblePendingEventDedup 覆盖 D7 纯函数侧：既有事件集合包含 pending 批次事件时，
// 同摘要不再产出 add_event；不同章节的事件不参与本章去重。
func TestAssemblePendingEventDedup(t *testing.T) {
	chapter := extractChapterFixture("林越推门而入。")
	parsed := extractResult{Events: []extractEvent{{
		Summary: "林越 推门", EventType: "character_action", ParticipantNamesProvided: true, Quote: "林越推门而入",
	}}}
	pending := novel.StoryEvent{ID: "evt_pending", ChapterID: chapter.ID, Summary: "林越推门"}

	cs, _ := assembleChapterChangeSet(chapter, knowledgeWith(), parsed, []novel.StoryEvent{pending}, nil, nil, 0)
	if len(cs.Ops) != 0 {
		t.Fatalf("pending duplicate leaked into ops: %+v", cs.Ops)
	}
	if cs.ChapterID != chapter.ID || cs.ChapterRevision != chapter.Revision {
		t.Errorf("provenance = %q/%d, want %q/%d", cs.ChapterID, cs.ChapterRevision, chapter.ID, chapter.Revision)
	}

	other := novel.StoryEvent{ID: "evt_other", ChapterID: "ch_other", Summary: "林越推门"}
	cs, _ = assembleChapterChangeSet(chapter, knowledgeWith(), parsed, []novel.StoryEvent{other}, nil, nil, 0)
	if len(cs.Ops) != 1 {
		t.Errorf("other chapter pending suppressed current event: ops=%d", len(cs.Ops))
	}
}

// TestAssembleEntityHintReasons 覆盖 EntityHint 原因码：未知 → unknown_entity，
// 歧义 → ambiguous_alias，已唯一归并的不产出候选。
func TestAssembleEntityHintReasons(t *testing.T) {
	chapter := extractChapterFixture("阿青与林越并肩而立。")
	knowledge := knowledgeWith(
		novel.Character{ID: "c_hero", Name: "林越"},
		novel.Character{ID: "c_a", Name: "苏青", Aliases: []string{"阿青"}},
		novel.Character{ID: "c_b", Name: "顾青", Aliases: []string{"阿青"}},
	)
	parsed := extractResult{EntityHints: []extractEntityHint{
		{Name: "林越"},
		{Name: "阿青"},
		{Name: "黑衣人"},
	}}

	_, candidates := assembleChapterChangeSet(chapter, knowledge, parsed, nil, nil, nil, 0)
	if len(candidates) != 2 {
		t.Fatalf("candidates = %+v, want 2", candidates)
	}
	byName := make(map[string]string, len(candidates))
	for _, c := range candidates {
		byName[c.Name] = c.Reason
	}
	if byName["阿青"] != "ambiguous_alias" {
		t.Errorf("ambiguous hint reason = %q", byName["阿青"])
	}
	if byName["黑衣人"] != "unknown_entity" {
		t.Errorf("unknown hint reason = %q", byName["黑衣人"])
	}
	if _, ok := byName["林越"]; ok {
		t.Errorf("known entity produced candidate: %+v", candidates)
	}
}

// TestExtractChapterDedupesPendingEvents 覆盖 D7 全链路：extractChapter 读取 pending
// ChangeSet 事件并参与去重，模型重复输出的同章事件不再进入新批次。
func TestExtractChapterDedupesPendingEvents(t *testing.T) {
	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	project, err := st.ImportChapters("书", []novel.Chapter{{Title: "一", Content: "林越推门而入。"}})
	if err != nil {
		t.Fatalf("ImportChapters: %v", err)
	}
	knowledge := knowledgeWith(novel.Character{ID: "c_hero", Name: "林越"})
	if _, _, err := st.CommitAnalysis(project.Revision, 0, knowledge); err != nil {
		t.Fatalf("CommitAnalysis: %v", err)
	}
	if _, _, err := st.FinishAnalysis(project.Revision, knowledge); err != nil {
		t.Fatalf("FinishAnalysis: %v", err)
	}
	chapterID := project.Chapters[0].ID

	raw, err := json.Marshal(novel.StoryEvent{ID: "evt_pending", ChapterID: chapterID, Summary: "林越推门"})
	if err != nil {
		t.Fatalf("marshal pending event: %v", err)
	}
	pendingCS := novel.ChangeSet{
		ID: "mcs_pending", Status: novel.ChangeSetProposed, Proposer: "test",
		Conflicts: []novel.ConflictItem{},
		Ops:       []novel.ChangeOp{{Op: novel.OpAddEvent, Record: raw}},
	}
	if err := st.SaveChangeSet(pendingCS, nil); err != nil {
		t.Fatalf("SaveChangeSet: %v", err)
	}

	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"message": map[string]string{
					"role":    "assistant",
					"content": `{"events":[{"summary":"林越推门","eventType":"character_action","participants":["林越"],"participantNamesProvided":true,"quote":"林越推门而入"}]}`,
				},
			}},
		})
	}))
	defer provider.Close()

	launcher := NewExtractLauncher(st, slog.New(slog.NewTextHandler(io.Discard, nil)))
	client := llm.NewClient(provider.URL+"/v1", "test-key", "mock-model")
	cs, _, ok := launcher.extractChapter(context.Background(), client, store.ExtractRunToken{Scope: []string{chapterID}}, chapterID)
	if !ok {
		t.Fatal("extractChapter failed")
	}
	if len(cs.Ops) != 0 {
		t.Fatalf("duplicate of pending event produced ops: %+v", cs.Ops)
	}
	if cs.ChapterID != chapterID || cs.ChapterRevision != project.Chapters[0].Revision {
		t.Errorf("provenance = %q/%d", cs.ChapterID, cs.ChapterRevision)
	}
}

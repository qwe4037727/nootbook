package novel

import (
	"errors"
	"testing"
	"time"
)

var memNow = time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)

func baseMeta() MemoryMeta {
	return MemoryMeta{
		WorkID:        internalWorkID,
		RecordVersion: 1,
		CreatedAt:     memNow,
		UpdatedAt:     memNow,
		Origin:        Origin{Type: OriginBackfill, Kind: "summary"},
		LastSource:    LastSourceBackfill,
	}
}

// TestSliceByRune 覆盖 U11：含 emoji 的正文按 rune [s,e) 切片正确；byte 下标混用/越界报错。
func TestSliceByRune(t *testing.T) {
	// "风停rain🌧后" —— 🌖（无变体选择符）是多 byte 单 rune，共 8 个 rune
	text := "风停rain🌧后"
	runes := []rune(text)
	if len(runes) != 8 {
		t.Fatalf("rune length = %d, want 8", len(runes))
	}
	got, err := SliceByRune(text, 6, 7)
	if err != nil {
		t.Fatalf("SliceByRune: %v", err)
	}
	if got != "🌧" {
		t.Errorf("slice = %q, want 🌖", got)
	}
	if got2, _ := SliceByRune(text, 0, 3); got2 != "风停r" {
		t.Errorf("ascii prefix slice = %q", got2)
	}
	if _, err := SliceByRune(text, 0, 10); err == nil {
		t.Errorf("out-of-range interval accepted")
	}
	if _, err := SliceByRune(text, 3, 3); err == nil {
		t.Errorf("empty interval accepted")
	}
	if _, err := SliceByRune(text, -1, 2); err == nil {
		t.Errorf("negative start accepted")
	}
}

// TestStatusEnumsClosed 覆盖 §3.2：专用状态类型枚举封闭、跨模型不混用。
func TestStatusEnumsClosed(t *testing.T) {
	if EventProposed.Valid() != true || EventInvalidated.Valid() != true {
		t.Errorf("StoryEventStatus closed set broken")
	}
	if StoryEventStatus("active").Valid() {
		t.Errorf("state enum value accepted by event status")
	}
	if StoryStateStatus("confirmed").Valid() {
		t.Errorf("event enum value accepted by state status")
	}
	if CognitionStatus("open").Valid() || StoryThreadStatus("known").Valid() || StoryPlanStatus("wrong").Valid() {
		t.Errorf("cross-model enum leakage")
	}
	if StoryEventType("other").Valid() != true || StoryEventType("dance").Valid() {
		t.Errorf("StoryEventType set broken")
	}
	if StoryPlanLevel("global").Valid() != true || StoryPlanLevel("chapter").Valid() {
		t.Errorf("StoryPlanLevel set broken")
	}
}

// TestNormalizeDoesNotDropRecords 覆盖 L-N：缺 ID/缺时间/超长字段 → 补默认与截断，不丢记录、不动 status。
func TestNormalizeDoesNotDropRecords(t *testing.T) {
	event := StoryEvent{
		MemoryMeta: MemoryMeta{Origin: Origin{Type: OriginBackfill}, LastSource: LastSourceBackfill},
		Status:     EventProposed,
		EventType:  "other",
		Summary:    string(make([]rune, 300)),
	}
	event.Normalize(memNow)
	if event.ID == "" || !validPrefixedID(event.ID, "evt") {
		t.Errorf("ID not filled: %q", event.ID)
	}
	if event.MemoryMeta.WorkID != internalWorkID || event.MemoryMeta.RecordVersion != 1 {
		t.Errorf("meta defaults not filled: %+v", event.MemoryMeta)
	}
	if RuneLen(event.Summary) != 200 {
		t.Errorf("summary not truncated to 200: %d", RuneLen(event.Summary))
	}
	if event.Status != EventProposed {
		t.Errorf("status changed by normalize: %v", event.Status)
	}

	plan := StoryEvent{MemoryMeta: baseMeta(), Status: "bogus", EventType: "other", Summary: "x"}
	plan.Normalize(memNow)
	if plan.Status != "bogus" {
		t.Errorf("normalize must not fix status, got %v", plan.Status)
	}
}

// TestPlanNormalizeFlexibilityDefault 覆盖 §4.5：Flexibility 缺省补 high。
func TestPlanNormalizeFlexibilityDefault(t *testing.T) {
	plan := StoryPlan{MemoryMeta: baseMeta(), Status: PlanProposed, Level: "global", Title: "主线"}
	plan.Normalize(memNow)
	if plan.Flexibility != "high" {
		t.Errorf("flexibility = %q, want high", plan.Flexibility)
	}
	if plan.ID == "" || !validPrefixedID(plan.ID, "plan") {
		t.Errorf("plan ID not filled: %q", plan.ID)
	}
}

func validEventForValidation() StoryEvent {
	return StoryEvent{
		ID:        "evt_a1",
		MemoryMeta: baseMeta(),
		Status:    EventProposed,
		EventType: "other",
		Summary:   "事件摘要",
	}
}

// TestValidateStoryEvent 覆盖 L-S：confirmed 强制证据（R1）、ChapterID 与 SourceRefs[0] 一致（§5.2）、前缀与枚举拒绝。
func TestValidateStoryEvent(t *testing.T) {
	event := validEventForValidation()
	if err := ValidateStoryEvent(&event); err != nil {
		t.Fatalf("proposed event without refs should pass: %v", err)
	}

	confirmed := validEventForValidation()
	confirmed.Status = EventConfirmed
	if err := ValidateStoryEvent(&confirmed); !errors.Is(err, ErrMemoryValidation) {
		t.Errorf("confirmed without sourceRefs error = %v", err)
	}
	confirmed.SourceRefs = []SourceRef{{ChapterID: "ch_1", ChapterRevision: 1, StartOffset: 0, EndOffset: 2, OffsetUnit: "rune"}}
	confirmed.ChapterID = "ch_1"
	if err := ValidateStoryEvent(&confirmed); err != nil {
		t.Errorf("confirmed with refs error = %v", err)
	}
	confirmed.ChapterID = "ch_2"
	if err := ValidateStoryEvent(&confirmed); !errors.Is(err, ErrMemoryValidation) {
		t.Errorf("chapterId mismatch error = %v", err)
	}

	bad := validEventForValidation()
	bad.Participants = []string{"char_x"}
	if err := ValidateStoryEvent(&bad); !errors.Is(err, ErrMemoryValidation) {
		t.Errorf("bad participant prefix error = %v", err)
	}

	badLink := validEventForValidation()
	badLink.Preconditions = []EventLink{{}}
	if err := ValidateStoryEvent(&badLink); !errors.Is(err, ErrMemoryValidation) {
		t.Errorf("empty event link error = %v", err)
	}

	badConf := validEventForValidation()
	badConf.Confidence = 1.5
	if err := ValidateStoryEvent(&badConf); !errors.Is(err, ErrMemoryValidation) {
		t.Errorf("confidence out of range error = %v", err)
	}
}

// TestValidateStoryState 覆盖 L-S：SubjectID 允许 c_/w_，拒绝其他前缀与缺失必填。
func TestValidateStoryState(t *testing.T) {
	state := StoryState{
		ID:         "state_a1",
		MemoryMeta: baseMeta(),
		Status:     StateActive,
		SubjectID:  "c_a1",
		Category:   "general",
		Attribute:  "profile",
		Value:      "状态",
	}
	if err := ValidateStoryState(&state); err != nil {
		t.Fatalf("c_ subject error = %v", err)
	}
	state.SubjectID = "w_b1"
	if err := ValidateStoryState(&state); err != nil {
		t.Errorf("w_ subject error = %v", err)
	}
	state.SubjectID = "evt_x"
	if err := ValidateStoryState(&state); !errors.Is(err, ErrMemoryValidation) {
		t.Errorf("evt_ subject error = %v", err)
	}
	state.SubjectID = "c_a1"
	state.Attribute = ""
	if err := ValidateStoryState(&state); !errors.Is(err, ErrMemoryValidation) {
		t.Errorf("missing attribute error = %v", err)
	}
}

// TestValidateStoryPlanScopes 覆盖 §4.5（R2）：父层级校验 + 同作用域单 active；跨 parent 可并行。
func TestValidateStoryPlanScopes(t *testing.T) {
	global := StoryPlan{ID: "plan_g1", MemoryMeta: baseMeta(), Status: PlanActive, Level: "global", Title: "总"}
	volA := StoryPlan{ID: "plan_v1", MemoryMeta: baseMeta(), Status: PlanActive, Level: "volume", ParentPlanID: "plan_g1", Title: "卷一", Flexibility: "high"}
	volB := StoryPlan{ID: "plan_v2", MemoryMeta: baseMeta(), Status: PlanActive, Level: "volume", ParentPlanID: "plan_g1", Title: "卷二", Flexibility: "high"}
	// 不同作用域可并行 active（U21）：volume 计划挂不同 parent，各自 active 共存
	globalB := StoryPlan{ID: "plan_g2", MemoryMeta: baseMeta(), Status: PlanProposed, Level: "global", Title: "另线", Flexibility: "high"}
	volC := StoryPlan{ID: "plan_v3", MemoryMeta: baseMeta(), Status: PlanActive, Level: "volume", ParentPlanID: "plan_g2", Title: "另一卷", Flexibility: "high"}
	if err := ValidateStoryPlanScopes([]StoryPlan{global, volA, globalB, volC}); err != nil {
		t.Errorf("parallel scopes error = %v", err)
	}
	// 同一 global 作用域双 active → 拒绝（global 与 global 同属 (global,"") 作用域）
	globalB2 := StoryPlan{ID: "plan_g9", MemoryMeta: baseMeta(), Status: PlanActive, Level: "global", Title: "再一条", Flexibility: "high"}
	if err := ValidateStoryPlanScopes([]StoryPlan{global, volA, globalB2}); !errors.Is(err, ErrMemoryValidation) {
		t.Errorf("duplicate active global error = %v", err)
	}
	// 同一 (volume, plan_g1) 作用域双 active → 拒绝
	if err := ValidateStoryPlanScopes([]StoryPlan{global, volA, volB}); !errors.Is(err, ErrMemoryValidation) {
		t.Errorf("duplicate active in scope error = %v", err)
	}
	// 父层级错误：volume 挂在 volume 下 → 拒绝
	if err := ValidateStoryPlanScopes([]StoryPlan{volA, volB}); !errors.Is(err, ErrMemoryValidation) {
		t.Errorf("bad parent level error = %v", err)
	}
	// global 带 parent → 结构校验拒绝
	badGlobal := StoryPlan{ID: "plan_g3", MemoryMeta: baseMeta(), Status: PlanProposed, Level: "global", ParentPlanID: "plan_g1", Title: "错", Flexibility: "high"}
	if err := ValidateStoryPlanStruct(&badGlobal); !errors.Is(err, ErrMemoryValidation) {
		t.Errorf("global with parent error = %v", err)
	}
	// 结构校验正向：合法 volume 计划通过
	if err := ValidateStoryPlanStruct(&volA); err != nil {
		t.Errorf("valid volume struct error = %v", err)
	}
}

// TestValidateStoryThreadTransitions 覆盖 §4.4：resolved 需 ResolutionEvent、abandoned 需理由。
func TestValidateStoryThreadTransitions(t *testing.T) {
	thread := StoryThread{
		ID:         "thread_a1",
		MemoryMeta: baseMeta(),
		Status:     ThreadResolved,
		Title:      "伏笔",
	}
	if err := ValidateStoryThread(&thread); !errors.Is(err, ErrMemoryValidation) {
		t.Errorf("resolved without event error = %v", err)
	}
	thread.ResolutionEvent = "evt_e1"
	if err := ValidateStoryThread(&thread); err != nil {
		t.Errorf("resolved with event error = %v", err)
	}
	abandoned := StoryThread{
		ID:         "thread_a2",
		MemoryMeta: baseMeta(),
		Status:     ThreadAbandoned,
		Title:      "伏笔",
	}
	if err := ValidateStoryThread(&abandoned); !errors.Is(err, ErrMemoryValidation) {
		t.Errorf("abandoned without reason error = %v", err)
	}
	abandoned.AbandonReason = "不再展开"
	if err := ValidateStoryThread(&abandoned); err != nil {
		t.Errorf("abandoned with reason error = %v", err)
	}
}

// TestValidateCognitionSubjectPrefix 覆盖 §6.1：Cognition.SubjectID 仅 c_。
func TestValidateCognitionSubjectPrefix(t *testing.T) {
	cog := Cognition{
		ID:         "cog_a1",
		MemoryMeta: baseMeta(),
		Status:     CogKnown,
		SubjectID:  "c_a1",
		Content:    "认知内容",
	}
	if err := ValidateCognition(&cog); err != nil {
		t.Fatalf("c_ subject error = %v", err)
	}
	cog.SubjectID = "w_b1"
	if err := ValidateCognition(&cog); !errors.Is(err, ErrMemoryValidation) {
		t.Errorf("w_ subject error = %v", err)
	}
}

// TestCheckSourceRefAgainstContent 覆盖 U10：quote 不一致 → quote_mismatch。
func TestCheckSourceRefAgainstContent(t *testing.T) {
	content := "风停了，雨也停了。"
	ref := SourceRef{ChapterID: "ch_1", ChapterRevision: 1, StartOffset: 0, EndOffset: 3, OffsetUnit: "rune", Quote: "风停了"}
	if err := CheckSourceRefAgainstContent(content, ref); err != nil {
		t.Fatalf("valid quote error = %v", err)
	}
	ref.Quote = "雨停了"
	if err := CheckSourceRefAgainstContent(content, ref); !errors.Is(err, ErrMemoryValidation) {
		t.Errorf("quote mismatch error = %v", err)
	}
	ref.Quote = ""
	ref.EndOffset = 999
	if err := CheckSourceRefAgainstContent(content, ref); !errors.Is(err, ErrMemoryValidation) {
		t.Errorf("interval beyond content error = %v", err)
	}
}

// TestEntityCandidateNormalize 覆盖 §6.3：候选默认 pending、ID 补 cand_ 前缀。
func TestEntityCandidateNormalize(t *testing.T) {
	candidate := EntityCandidate{Name: "可疑人物", Reason: "ambiguous_alias"}
	candidate.Normalize(memNow)
	if candidate.ID == "" || !validPrefixedID(candidate.ID, "cand") {
		t.Errorf("candidate ID not filled: %q", candidate.ID)
	}
	if candidate.Status != string(CandidatePending) {
		t.Errorf("status = %q, want pending", candidate.Status)
	}
	if RuneLen(candidate.Name) > 40 {
		t.Errorf("name not truncated")
	}
}

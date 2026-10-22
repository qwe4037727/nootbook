package novel

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// 记忆系统数据模型（docs/memory_model_spec.md V1.2 冻结版 §2–§6）。
// 校验分层：L-N Normalize（纯函数，不丢记录、不修 status/confidence/前缀）；
// L-S 结构校验（Validate*，持久化侧失败拒绝整个操作）；L-V 语义复核归 store/reconcile。

// internalWorkID 为单作品架构下的固定 work_id（冻结决策 9）。
const internalWorkID = "work_001"

// InternalWorkID 暴露给外部包（server 响应等）。
func InternalWorkID() string { return internalWorkID }

// ---- Origin 与 LastSource（§3.1）----

type OriginType string

const (
	OriginBackfill    OriginType = "backfill"
	OriginDeepExtract OriginType = "deep_extract"
	OriginManual      OriginType = "manual"
	OriginCommit      OriginType = "commit"
	OriginReconcile   OriginType = "reconcile"
)

func (t OriginType) Valid() bool {
	switch t {
	case OriginBackfill, OriginDeepExtract, OriginManual, OriginCommit, OriginReconcile:
		return true
	}
	return false
}

type Origin struct {
	Type    OriginType `json:"type"`
	Chapter string     `json:"chapterId,omitempty"`
	Kind    string     `json:"kind,omitempty"`
}

const (
	LastSourceAgentExtraction = "agent_extraction"
	LastSourceManualEdit      = "manual_edit"
	LastSourceBackfill        = "backfill"
	LastSourceReconcile       = "reconcile"
)

func ValidLastSource(value string) bool {
	switch value {
	case LastSourceAgentExtraction, LastSourceManualEdit, LastSourceBackfill, LastSourceReconcile:
		return true
	}
	return false
}

// ---- MemoryMeta（不含 Status，§3.1）----

type MemoryMeta struct {
	WorkID        string      `json:"workId"`
	RecordVersion int64       `json:"recordVersion"`
	CreatedAt     time.Time   `json:"createdAt"`
	UpdatedAt     time.Time   `json:"updatedAt"`
	SourceRefs    []SourceRef `json:"sourceRefs,omitempty"`
	Confidence    float64     `json:"confidence,omitempty"`
	NeedsReview   bool        `json:"needsReview"`
	ReviewReason  string      `json:"reviewReason,omitempty"`
	Origin        Origin      `json:"origin"`
	LastSource    string      `json:"lastSource"`
}

// Normalize 补默认值（L-N）：不修 confidence 越界、不修非法 lastSource（留给 L-S 拒绝）。
func (m *MemoryMeta) Normalize(now time.Time) {
	if m.WorkID == "" {
		m.WorkID = internalWorkID
	}
	if m.RecordVersion < 1 {
		m.RecordVersion = 1
	}
	if m.CreatedAt.IsZero() {
		m.CreatedAt = now
	}
	if m.UpdatedAt.IsZero() {
		m.UpdatedAt = now
	}
	m.CreatedAt = m.CreatedAt.UTC()
	m.UpdatedAt = m.UpdatedAt.UTC()
	m.ReviewReason = strings.TrimSpace(m.ReviewReason)
}

// ---- SourceRef（§5.1）----

type SourceRef struct {
	ChapterID       string `json:"chapterId"`
	ChapterRevision int64  `json:"chapterRevision"`
	StartOffset     int    `json:"startOffset"`
	EndOffset       int    `json:"endOffset"`
	OffsetUnit      string `json:"offsetUnit"`
	Quote           string `json:"quote,omitempty"`
}

// SliceByRune 按 rune 左闭右开区间 [start, end) 切片（§5.1，#5 交付）。
func SliceByRune(content string, start, end int) (string, error) {
	runes := []rune(content)
	if start < 0 || end <= start || end > len(runes) {
		return "", fmt.Errorf("invalid rune interval [%d,%d) for length %d", start, end, len(runes))
	}
	return string(runes[start:end]), nil
}

// ---- 专用状态类型（§3.2，各模型独立、编译期隔离）----

type StoryEventStatus string

const (
	EventProposed    StoryEventStatus = "proposed"
	EventConfirmed   StoryEventStatus = "confirmed"
	EventConflicted  StoryEventStatus = "conflicted"
	EventInvalidated StoryEventStatus = "invalidated"
)

func (s StoryEventStatus) Valid() bool {
	switch s {
	case EventProposed, EventConfirmed, EventConflicted, EventInvalidated:
		return true
	}
	return false
}

type StoryStateStatus string

const (
	StateActive     StoryStateStatus = "active"
	StateSuperseded StoryStateStatus = "superseded"
	StateInvalid    StoryStateStatus = "invalidated"
)

func (s StoryStateStatus) Valid() bool {
	switch s {
	case StateActive, StateSuperseded, StateInvalid:
		return true
	}
	return false
}

type CognitionStatus string

const (
	CogKnown     CognitionStatus = "known"
	CogWrong     CognitionStatus = "wrong"
	CogForgotten CognitionStatus = "forgotten"
	CogUncertain CognitionStatus = "uncertain"
)

func (s CognitionStatus) Valid() bool {
	switch s {
	case CogKnown, CogWrong, CogForgotten, CogUncertain:
		return true
	}
	return false
}

type StoryThreadStatus string

const (
	ThreadOpen       StoryThreadStatus = "open"
	ThreadDeveloping StoryThreadStatus = "developing"
	ThreadResolved   StoryThreadStatus = "resolved"
	ThreadDeferred   StoryThreadStatus = "deferred"
	ThreadAbandoned  StoryThreadStatus = "abandoned"
	ThreadConflicted StoryThreadStatus = "conflicted"
)

func (s StoryThreadStatus) Valid() bool {
	switch s {
	case ThreadOpen, ThreadDeveloping, ThreadResolved, ThreadDeferred, ThreadAbandoned, ThreadConflicted:
		return true
	}
	return false
}

type StoryPlanStatus string

const (
	PlanProposed   StoryPlanStatus = "proposed"
	PlanActive     StoryPlanStatus = "active"
	PlanPaused     StoryPlanStatus = "paused"
	PlanCompleted  StoryPlanStatus = "completed"
	PlanSuperseded StoryPlanStatus = "superseded"
	PlanCancelled  StoryPlanStatus = "cancelled"
)

func (s StoryPlanStatus) Valid() bool {
	switch s {
	case PlanProposed, PlanActive, PlanPaused, PlanCompleted, PlanSuperseded, PlanCancelled:
		return true
	}
	return false
}

// ---- StoryEvent（§4.1）----

type StoryEventType string

const (
	EventAction       StoryEventType = "character_action"
	EventRelation     StoryEventType = "relation_change"
	EventReveal       StoryEventType = "info_reveal"
	EventConflict     StoryEventType = "conflict"
	EventLocation     StoryEventType = "location_change"
	EventItemTransfer StoryEventType = "item_transfer"
	EventWorldChange  StoryEventType = "world_change"
	EventOther        StoryEventType = "other"
)

func (t StoryEventType) Valid() bool {
	switch t {
	case EventAction, EventRelation, EventReveal, EventConflict, EventLocation, EventItemTransfer, EventWorldChange, EventOther:
		return true
	}
	return false
}

type EventLink struct {
	EventID string `json:"eventId,omitempty"`
	Text    string `json:"text,omitempty"`
}

type StoryEvent struct {
	ID string `json:"id"`
	MemoryMeta
	Status            StoryEventStatus `json:"status"`
	EventType         string           `json:"eventType"`
	Summary           string           `json:"summary"`
	ChapterID         string           `json:"chapterId,omitempty"`
	StoryOrder        int              `json:"storyOrder"`
	ParticipantsKnown bool             `json:"participantsKnown"`
	Participants      []string         `json:"participants,omitempty"`
	RelatedEntities   []string         `json:"relatedEntities,omitempty"`
	Preconditions     []EventLink      `json:"preconditions,omitempty"`
	Consequences      []EventLink      `json:"consequences,omitempty"`
}

func (e *StoryEvent) Normalize(now time.Time) {
	if e.ID == "" {
		e.ID = NewID("evt")
	}
	e.MemoryMeta.Normalize(now)
	e.EventType = strings.TrimSpace(e.EventType)
	e.Summary = memTruncate(strings.TrimSpace(e.Summary), 200)
	e.ChapterID = strings.TrimSpace(e.ChapterID)
	e.Participants = dedupe(e.Participants)
	e.RelatedEntities = dedupe(e.RelatedEntities)
	e.Preconditions = normalizeEventLinks(e.Preconditions)
	e.Consequences = normalizeEventLinks(e.Consequences)
}

// ---- StoryState（§4.2）----

type StoryState struct {
	ID string `json:"id"`
	MemoryMeta
	Status               StoryStateStatus `json:"status"`
	SubjectID            string           `json:"subjectId"`
	Category             string           `json:"category"`
	Attribute            string           `json:"attribute"`
	Value                string           `json:"value"`
	ValidFromEvent       string           `json:"validFromEvent,omitempty"`
	LastConfirmedChapter string           `json:"lastConfirmedChapter,omitempty"`
}

func (s *StoryState) Normalize(now time.Time) {
	if s.ID == "" {
		s.ID = NewID("state")
	}
	s.MemoryMeta.Normalize(now)
	s.SubjectID = strings.TrimSpace(s.SubjectID)
	s.Category = strings.TrimSpace(s.Category)
	s.Attribute = memTruncate(strings.TrimSpace(s.Attribute), 40)
	s.Value = memTruncate(strings.TrimSpace(s.Value), 200)
	s.ValidFromEvent = strings.TrimSpace(s.ValidFromEvent)
	s.LastConfirmedChapter = strings.TrimSpace(s.LastConfirmedChapter)
}

// ---- Cognition（§4.3）----

type Cognition struct {
	ID string `json:"id"`
	MemoryMeta
	Status          CognitionStatus `json:"status"`
	SubjectID       string          `json:"subjectId"`
	Content         string          `json:"content"`
	KnowledgeItemID string          `json:"knowledgeItemId,omitempty"`
	KnownSinceEvent string          `json:"knownSinceEvent,omitempty"`
}

func (c *Cognition) Normalize(now time.Time) {
	if c.ID == "" {
		c.ID = NewID("cog")
	}
	c.MemoryMeta.Normalize(now)
	c.SubjectID = strings.TrimSpace(c.SubjectID)
	c.Content = memTruncate(strings.TrimSpace(c.Content), 200)
	c.KnowledgeItemID = strings.TrimSpace(c.KnowledgeItemID)
	c.KnownSinceEvent = strings.TrimSpace(c.KnownSinceEvent)
}

// ---- StoryThread（§4.4）----

type StoryThread struct {
	ID string `json:"id"`
	MemoryMeta
	Status             StoryThreadStatus `json:"status"`
	Title              string            `json:"title"`
	Question           string            `json:"question,omitempty"`
	IntroducedByEvent  string            `json:"introducedByEvent,omitempty"`
	RelatedEvents      []string          `json:"relatedEvents,omitempty"`
	InvolvedCharacters []string          `json:"involvedCharacters,omitempty"`
	KnownClues         []string          `json:"knownClues,omitempty"`
	ResolutionCriteria []string          `json:"resolutionCriteria,omitempty"`
	ResolutionEvent    string            `json:"resolutionEvent,omitempty"`
	AbandonReason      string            `json:"abandonReason,omitempty"`
}

func (t *StoryThread) Normalize(now time.Time) {
	if t.ID == "" {
		t.ID = NewID("thread")
	}
	t.MemoryMeta.Normalize(now)
	t.Title = memTruncate(strings.TrimSpace(t.Title), 60)
	t.Question = memTruncate(strings.TrimSpace(t.Question), 120)
	t.IntroducedByEvent = strings.TrimSpace(t.IntroducedByEvent)
	t.RelatedEvents = dedupe(t.RelatedEvents)
	t.InvolvedCharacters = dedupe(t.InvolvedCharacters)
	t.KnownClues = truncateList(t.KnownClues, 8, 100)
	t.ResolutionCriteria = truncateList(t.ResolutionCriteria, 6, 100)
	t.ResolutionEvent = strings.TrimSpace(t.ResolutionEvent)
	t.AbandonReason = memTruncate(strings.TrimSpace(t.AbandonReason), 200)
}

// ---- StoryPlan（§4.5）----

type StoryPlanLevel string

const (
	PlanLevelGlobal   StoryPlanLevel = "global"
	PlanLevelVolume   StoryPlanLevel = "volume"
	PlanLevelNearTerm StoryPlanLevel = "near_term"
)

func (l StoryPlanLevel) Valid() bool {
	switch l {
	case PlanLevelGlobal, PlanLevelVolume, PlanLevelNearTerm:
		return true
	}
	return false
}

// ParentLevel 返回 parentPlanID 允许的父层级；global 不得有 parent（返回空）。
func (l StoryPlanLevel) ParentLevel() StoryPlanLevel {
	switch l {
	case PlanLevelVolume:
		return PlanLevelGlobal
	case PlanLevelNearTerm:
		return PlanLevelVolume // 或 global，由校验侧放宽
	}
	return ""
}

type StoryPlan struct {
	ID string `json:"id"`
	MemoryMeta
	Status             StoryPlanStatus `json:"status"`
	Level              string          `json:"level"`
	ParentPlanID       string          `json:"parentPlanId,omitempty"`
	Title              string          `json:"title"`
	Objective          string          `json:"objective,omitempty"`
	Constraints        []string        `json:"constraints,omitempty"`
	CandidateBeats     []string        `json:"candidateBeats,omitempty"`
	DependsOnThreads   []string        `json:"dependsOnThreads,omitempty"`
	InvolvedCharacters []string        `json:"involvedCharacters,omitempty"`
	Flexibility        string          `json:"flexibility,omitempty"`
	CreatedFromCommit  string          `json:"createdFromCommit,omitempty"`
}

func (p *StoryPlan) Normalize(now time.Time) {
	if p.ID == "" {
		p.ID = NewID("plan")
	}
	p.MemoryMeta.Normalize(now)
	p.Level = strings.TrimSpace(p.Level)
	p.ParentPlanID = strings.TrimSpace(p.ParentPlanID)
	p.Title = memTruncate(strings.TrimSpace(p.Title), 80)
	p.Objective = memTruncate(strings.TrimSpace(p.Objective), 300)
	p.Constraints = truncateList(p.Constraints, 8, 100)
	p.CandidateBeats = truncateList(p.CandidateBeats, 10, 100)
	p.DependsOnThreads = dedupe(p.DependsOnThreads)
	p.InvolvedCharacters = dedupe(p.InvolvedCharacters)
	p.Flexibility = strings.TrimSpace(p.Flexibility)
	if p.Flexibility == "" {
		p.Flexibility = "high"
	}
	p.CreatedFromCommit = strings.TrimSpace(p.CreatedFromCommit)
}

// ---- EntityCandidate（§6.3，R3）----

type EntityCandidateStatus string

const (
	CandidatePending   EntityCandidateStatus = "pending"
	CandidateRegister  EntityCandidateStatus = "register"
	CandidateMerge     EntityCandidateStatus = "merge"
	CandidateDismissed EntityCandidateStatus = "dismissed"
)

func (s EntityCandidateStatus) Valid() bool {
	switch s {
	case CandidatePending, CandidateRegister, CandidateMerge, CandidateDismissed:
		return true
	}
	return false
}

type EntityCandidate struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Aliases     []string    `json:"aliases,omitempty"`
	PossibleIDs []string    `json:"possibleIds,omitempty"`
	SourceRefs  []SourceRef `json:"sourceRefs,omitempty"`
	Reason      string      `json:"reason"`
	Status      string      `json:"status"`
	Resolution  string      `json:"resolution,omitempty"`
	CreatedAt   time.Time   `json:"createdAt"`
	ResolvedAt  time.Time   `json:"resolvedAt,omitempty"`
}

func (c *EntityCandidate) Normalize(now time.Time) {
	if c.ID == "" {
		c.ID = NewID("cand")
	}
	c.Name = memTruncate(strings.TrimSpace(c.Name), 40)
	c.Aliases = cleanAliases(c.Aliases, c.Name)
	c.PossibleIDs = dedupe(c.PossibleIDs)
	c.Reason = strings.TrimSpace(c.Reason)
	if c.Status == "" {
		c.Status = string(CandidatePending)
	}
	if c.CreatedAt.IsZero() {
		c.CreatedAt = now
	}
	c.CreatedAt = c.CreatedAt.UTC()
	c.ResolvedAt = c.ResolvedAt.UTC()
}

// ---- 通用辅助 ----

// memTruncate 按 rune 截断至总长 ≤ limit（含省略号，区别于 project.go 的 truncateRunes）。
func memTruncate(text string, limit int) string {
	if limit <= 0 || utf8.RuneCountInString(text) <= limit {
		return text
	}
	runes := []rune(text)
	return string(runes[:limit-1]) + "…"
}

func dedupe(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	if result == nil {
		result = []string{}
	}
	return result
}

func truncateList(values []string, limit, itemLimit int) []string {
	result := make([]string, 0, min(len(values), limit))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = truncateRunes(strings.TrimSpace(value), itemLimit)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
		if len(result) >= limit {
			break
		}
	}
	return result
}

func normalizeEventLinks(links []EventLink) []EventLink {
	result := make([]EventLink, 0, len(links))
	for _, link := range links {
		link.EventID = strings.TrimSpace(link.EventID)
		link.Text = strings.TrimSpace(link.Text)
		if link.EventID == "" && link.Text == "" {
			continue // 双空链接为无效载荷，L-N 丢弃空壳（记录本身不受影响）
		}
		result = append(result, link)
	}
	if result == nil {
		result = []EventLink{}
	}
	return result
}

// ---- L-S 结构校验（Validate*：返回 error 即拒绝整个持久化操作）----

var ErrMemoryValidation = errors.New("novel: memory validation failed")

func validationErrorf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrMemoryValidation, fmt.Sprintf(format, args...))
}

func validPrefixedID(id, prefix string) bool {
	return ValidID(id) && strings.HasPrefix(id, prefix+"_")
}

func validateMemoryMeta(m *MemoryMeta, id string) error {
	if m.WorkID != internalWorkID {
		return validationErrorf("%s: workId=%q want %q", id, m.WorkID, internalWorkID)
	}
	if m.RecordVersion < 1 {
		return validationErrorf("%s: recordVersion=%d want >=1", id, m.RecordVersion)
	}
	if !ValidLastSource(m.LastSource) {
		return validationErrorf("%s: lastSource=%q is not allowed", id, m.LastSource)
	}
	if !m.Origin.Type.Valid() {
		return validationErrorf("%s: origin.type=%q is not allowed", id, m.Origin.Type)
	}
	if m.Confidence < 0 || m.Confidence > 1 {
		return validationErrorf("%s: confidence=%v out of range [0,1]", id, m.Confidence)
	}
	for i, ref := range m.SourceRefs {
		if err := ValidateSourceRefStruct(ref); err != nil {
			return validationErrorf("%s: sourceRefs[%d]: %v", id, i, err)
		}
	}
	return nil
}

// ValidateSourceRefStruct 做无正文依赖的结构校验；区间上界与 quote 一致性需正文，属 L-V（§5.1）。
func ValidateSourceRefStruct(ref SourceRef) error {
	if !validPrefixedID(ref.ChapterID, "ch") {
		return validationErrorf("chapterId %q must be ch_ id", ref.ChapterID)
	}
	if ref.ChapterRevision <= 0 {
		return validationErrorf("chapterRevision=%d want >0", ref.ChapterRevision)
	}
	if ref.OffsetUnit != "rune" {
		return validationErrorf("offsetUnit=%q want rune", ref.OffsetUnit)
	}
	if ref.StartOffset < 0 || ref.EndOffset <= ref.StartOffset {
		return validationErrorf("interval [%d,%d) invalid", ref.StartOffset, ref.EndOffset)
	}
	return nil
}

// CheckSourceRefAgainstContent 为 L-V 辅助：区间上界 + quote 一致性（有正文时调用）。
func CheckSourceRefAgainstContent(content string, ref SourceRef) error {
	runes := []rune(content)
	if ref.EndOffset > len(runes) {
		return validationErrorf("interval [%d,%d) exceeds content length %d", ref.StartOffset, ref.EndOffset, len(runes))
	}
	if ref.Quote != "" {
		quote, err := SliceByRune(content, ref.StartOffset, ref.EndOffset)
		if err != nil {
			return err
		}
		if quote != ref.Quote {
			return validationErrorf("quote mismatch")
		}
	}
	return nil
}

func validateEventLinks(links []EventLink, id, field string) error {
	for i, link := range links {
		if link.EventID == "" && link.Text == "" {
			return validationErrorf("%s: %s[%d] has empty eventId and text", id, field, i)
		}
		if link.EventID != "" && !validPrefixedID(link.EventID, "evt") {
			return validationErrorf("%s: %s[%d].eventId %q must be evt_ id", id, field, i, link.EventID)
		}
	}
	return nil
}

func validateCharacterRefs(values []string, id, field string) error {
	for _, value := range values {
		if !validPrefixedID(value, "c") {
			return validationErrorf("%s: %s entry %q must be c_ id", id, field, value)
		}
	}
	return nil
}

// ValidateStoryEvent 执行 L-S 结构校验（§4.7；confirmed 强制证据；ChapterID 与 SourceRefs[0] 一致性 §5.2）。
func ValidateStoryEvent(event *StoryEvent) error {
	if !validPrefixedID(event.ID, "evt") {
		return validationErrorf("story event id %q must be evt_ id", event.ID)
	}
	if err := validateMemoryMeta(&event.MemoryMeta, event.ID); err != nil {
		return err
	}
	if !event.Status.Valid() {
		return validationErrorf("%s: status %q is not allowed", event.ID, event.Status)
	}
	if !StoryEventType(event.EventType).Valid() {
		return validationErrorf("%s: eventType %q is not allowed", event.ID, event.EventType)
	}
	if event.Summary == "" {
		return validationErrorf("%s: summary is required", event.ID)
	}
	if event.ChapterID != "" && !validPrefixedID(event.ChapterID, "ch") {
		return validationErrorf("%s: chapterId %q must be ch_ id", event.ID, event.ChapterID)
	}
	// §5.2：SourceRefs 非空时 ChapterID 必须 == SourceRefs[0].ChapterID
	if len(event.SourceRefs) > 0 && event.ChapterID != event.SourceRefs[0].ChapterID {
		return validationErrorf("%s: chapterId %q != sourceRefs[0].chapterId %q", event.ID, event.ChapterID, event.SourceRefs[0].ChapterID)
	}
	// R1：confirmed 强制证据
	if event.Status == EventConfirmed && len(event.SourceRefs) == 0 {
		return validationErrorf("%s: confirmed event requires non-empty sourceRefs", event.ID)
	}
	if err := validateCharacterRefs(event.Participants, event.ID, "participants"); err != nil {
		return err
	}
	for _, entity := range event.RelatedEntities {
		if !validPrefixedID(entity, "w") {
			return validationErrorf("%s: relatedEntities entry %q must be w_ id", event.ID, entity)
		}
	}
	if err := validateEventLinks(event.Preconditions, event.ID, "preconditions"); err != nil {
		return err
	}
	return validateEventLinks(event.Consequences, event.ID, "consequences")
}

// ValidateStoryState 执行 L-S 结构校验（SubjectID 允许 c_/w_，§6.1）。
func ValidateStoryState(state *StoryState) error {
	if !validPrefixedID(state.ID, "state") {
		return validationErrorf("story state id %q must be state_ id", state.ID)
	}
	if err := validateMemoryMeta(&state.MemoryMeta, state.ID); err != nil {
		return err
	}
	if !state.Status.Valid() {
		return validationErrorf("%s: status %q is not allowed", state.ID, state.Status)
	}
	if !validPrefixedID(state.SubjectID, "c") && !validPrefixedID(state.SubjectID, "w") {
		return validationErrorf("%s: subjectId %q must be c_ or w_ id", state.ID, state.SubjectID)
	}
	if state.Category == "" || state.Attribute == "" || state.Value == "" {
		return validationErrorf("%s: category/attribute/value are required", state.ID)
	}
	if state.ValidFromEvent != "" && !validPrefixedID(state.ValidFromEvent, "evt") {
		return validationErrorf("%s: validFromEvent %q must be evt_ id", state.ID, state.ValidFromEvent)
	}
	if state.LastConfirmedChapter != "" && !validPrefixedID(state.LastConfirmedChapter, "ch") {
		return validationErrorf("%s: lastConfirmedChapter %q must be ch_ id", state.ID, state.LastConfirmedChapter)
	}
	return nil
}

// ValidateCognition 执行 L-S 结构校验（SubjectID 仅 c_，§6.1）。
func ValidateCognition(cog *Cognition) error {
	if !validPrefixedID(cog.ID, "cog") {
		return validationErrorf("cognition id %q must be cog_ id", cog.ID)
	}
	if err := validateMemoryMeta(&cog.MemoryMeta, cog.ID); err != nil {
		return err
	}
	if !cog.Status.Valid() {
		return validationErrorf("%s: status %q is not allowed", cog.ID, cog.Status)
	}
	if !validPrefixedID(cog.SubjectID, "c") {
		return validationErrorf("%s: subjectId %q must be c_ id", cog.ID, cog.SubjectID)
	}
	if cog.Content == "" {
		return validationErrorf("%s: content is required", cog.ID)
	}
	if cog.KnownSinceEvent != "" && !validPrefixedID(cog.KnownSinceEvent, "evt") {
		return validationErrorf("%s: knownSinceEvent %q must be evt_ id", cog.ID, cog.KnownSinceEvent)
	}
	return nil
}

// ValidateStoryThread 执行 L-S 结构校验（§4.4 转换前置：resolved 需 ResolutionEvent、abandoned 需理由）。
func ValidateStoryThread(thread *StoryThread) error {
	if !validPrefixedID(thread.ID, "thread") {
		return validationErrorf("story thread id %q must be thread_ id", thread.ID)
	}
	if err := validateMemoryMeta(&thread.MemoryMeta, thread.ID); err != nil {
		return err
	}
	if !thread.Status.Valid() {
		return validationErrorf("%s: status %q is not allowed", thread.ID, thread.Status)
	}
	if thread.Title == "" {
		return validationErrorf("%s: title is required", thread.ID)
	}
	if thread.IntroducedByEvent != "" && !validPrefixedID(thread.IntroducedByEvent, "evt") {
		return validationErrorf("%s: introducedByEvent %q must be evt_ id", thread.ID, thread.IntroducedByEvent)
	}
	for _, event := range thread.RelatedEvents {
		if !validPrefixedID(event, "evt") {
			return validationErrorf("%s: relatedEvents entry %q must be evt_ id", thread.ID, event)
		}
	}
	if err := validateCharacterRefs(thread.InvolvedCharacters, thread.ID, "involvedCharacters"); err != nil {
		return err
	}
	if thread.ResolutionEvent != "" && !validPrefixedID(thread.ResolutionEvent, "evt") {
		return validationErrorf("%s: resolutionEvent %q must be evt_ id", thread.ID, thread.ResolutionEvent)
	}
	if thread.Status == ThreadResolved && thread.ResolutionEvent == "" {
		return validationErrorf("%s: resolved thread requires resolutionEvent", thread.ID)
	}
	if thread.Status == ThreadAbandoned && thread.AbandonReason == "" {
		return validationErrorf("%s: abandoned thread requires abandonReason", thread.ID)
	}
	return nil
}

// ValidateStoryPlanStruct 执行 L-S 结构校验中不依赖跨记录的部分；
// parentPlanID 的存在性/层级校验需计划全集，由 store 侧 ValidateStoryPlanScopes 完成。
func ValidateStoryPlanStruct(plan *StoryPlan) error {
	if !validPrefixedID(plan.ID, "plan") {
		return validationErrorf("story plan id %q must be plan_ id", plan.ID)
	}
	if err := validateMemoryMeta(&plan.MemoryMeta, plan.ID); err != nil {
		return err
	}
	if !plan.Status.Valid() {
		return validationErrorf("%s: status %q is not allowed", plan.ID, plan.Status)
	}
	level := StoryPlanLevel(plan.Level)
	if !level.Valid() {
		return validationErrorf("%s: level %q is not allowed", plan.ID, plan.Level)
	}
	if plan.Title == "" {
		return validationErrorf("%s: title is required", plan.ID)
	}
	if plan.ParentPlanID != "" && !validPrefixedID(plan.ParentPlanID, "plan") {
		return validationErrorf("%s: parentPlanId %q must be plan_ id", plan.ID, plan.ParentPlanID)
	}
	if level == PlanLevelGlobal && plan.ParentPlanID != "" {
		return validationErrorf("%s: global plan must not have parent", plan.ID)
	}
	switch plan.Flexibility {
	case "high", "medium", "low":
	default:
		return validationErrorf("%s: flexibility %q is not allowed", plan.ID, plan.Flexibility)
	}
	for _, thread := range plan.DependsOnThreads {
		if !validPrefixedID(thread, "thread") {
			return validationErrorf("%s: dependsOnThreads entry %q must be thread_ id", plan.ID, thread)
		}
	}
	return validateCharacterRefs(plan.InvolvedCharacters, plan.ID, "involvedCharacters")
}

// ValidateStoryPlanScopes 校验计划全集的作用域规则（§4.5 R2）：
// parent 必须存在且层级合法；同一 (level, parentPlanID) 作用域内最多一个 active。
func ValidateStoryPlanScopes(plans []StoryPlan) error {
	byID := make(map[string]*StoryPlan, len(plans))
	for i := range plans {
		byID[plans[i].ID] = &plans[i]
	}
	activeScopes := make(map[string]string, len(plans))
	for i := range plans {
		plan := &plans[i]
		if plan.ParentPlanID == "" {
			continue
		}
		parent, ok := byID[plan.ParentPlanID]
		if !ok {
			return validationErrorf("%s: parentPlanId %q does not exist", plan.ID, plan.ParentPlanID)
		}
		level := StoryPlanLevel(plan.Level)
		parentLevel := StoryPlanLevel(parent.Level)
		okParent := (level == PlanLevelVolume && parentLevel == PlanLevelGlobal) ||
			(level == PlanLevelNearTerm && (parentLevel == PlanLevelVolume || parentLevel == PlanLevelGlobal))
		if !okParent {
			return validationErrorf("%s: parent level %q is not valid for level %q", plan.ID, parent.Level, plan.Level)
		}
	}
	for i := range plans {
		plan := &plans[i]
		if plan.Status != PlanActive {
			continue
		}
		scope := plan.Level + "|" + plan.ParentPlanID
		if other, ok := activeScopes[scope]; ok {
			return validationErrorf("%s: active plan conflicts with %s in scope %q", plan.ID, other, scope)
		}
		activeScopes[scope] = plan.ID
	}
	return nil
}

// RuneLen 暴露 rune 计数（与 SliceByRune 同一语义）。
func RuneLen(text string) int { return utf8.RuneCountInString(text) }

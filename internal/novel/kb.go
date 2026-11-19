package novel

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// M2 大世界统一词条模型。
//
// 设计口径：
//   - 角色（character）与世界设定（setting）仍以 knowledge.json 为真源，抽取/分析链路不变；
//   - 势力（faction）/地点（location）/物品（item）等扩展词条落在项目级 agent.db 的 kb_entry 表；
//   - 字段锁（FieldLockSet）与词条来源无关，按 entryID+field 持久化；
//   - 关系与时间线统一按 entryID 绑定，因此可跨真源关联角色、势力、地点与物品。

type EntryKind string

const (
	EntryCharacter EntryKind = "character"
	EntryFaction   EntryKind = "faction"
	EntryLocation  EntryKind = "location"
	EntryItem      EntryKind = "item"
	EntrySetting   EntryKind = "setting"
)

func (k EntryKind) Valid() bool {
	switch k {
	case EntryCharacter, EntryFaction, EntryLocation, EntryItem, EntrySetting:
		return true
	}
	return false
}

// Extended 报告该类型是否由 kb_entry 表承载（相对 knowledge.json 真源）。
func (k EntryKind) Extended() bool {
	switch k {
	case EntryFaction, EntryLocation, EntryItem, EntrySetting:
		return true
	}
	return false
}

// IDPrefix 返回该类型词条的 ID 前缀（与既有 c_/w_ 约定兼容）。
func (k EntryKind) IDPrefix() string {
	switch k {
	case EntryCharacter:
		return "c_"
	case EntryFaction:
		return "f_"
	case EntryLocation:
		return "loc_"
	case EntryItem:
		return "it_"
	case EntrySetting:
		return "w_"
	}
	return ""
}

// EntryKindForID 按 ID 前缀推断类型；未知前缀返回空。
func EntryKindForID(id string) EntryKind {
	switch {
	case strings.HasPrefix(id, "c_"):
		return EntryCharacter
	case strings.HasPrefix(id, "f_"):
		return EntryFaction
	case strings.HasPrefix(id, "loc_"):
		return EntryLocation
	case strings.HasPrefix(id, "it_"):
		return EntryItem
	case strings.HasPrefix(id, "w_"):
		return EntrySetting
	}
	return ""
}

// EntryKindForCategory 把分析产出的世界条目标注映射到 M2 实体大类。
// 未识别的类别保持 setting（世界设定），避免丢失数据。
func EntryKindForCategory(category string) EntryKind {
	category = strings.TrimSpace(category)
	switch {
	case containsAny(category, "势力", "门派", "组织", "家族", "王朝", "朝廷", "宗门", "帮会", "军团"):
		return EntryFaction
	case containsAny(category, "地点", "场景", "地域", "城市", "山川", "建筑", "秘境", "府邸"):
		return EntryLocation
	case containsAny(category, "物品", "道具", "法宝", "武器", "丹药", "灵器", "信物"):
		return EntryItem
	}
	return EntrySetting
}

func containsAny(value string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}

// FieldLockSet 为 entryID → 已锁定字段集合。
type FieldLockSet map[string]map[string]struct{}

func (s FieldLockSet) Locked(entryID, field string) bool {
	if s == nil {
		return false
	}
	fields, ok := s[entryID]
	if !ok {
		return false
	}
	_, ok = fields[strings.TrimSpace(field)]
	return ok
}

// Fields 返回某词条的锁定字段（排序后，便于稳定输出）。
func (s FieldLockSet) Fields(entryID string) []string {
	fields, ok := s[entryID]
	if !ok {
		return []string{}
	}
	out := make([]string, 0, len(fields))
	for field := range fields {
		out = append(out, field)
	}
	sort.Strings(out)
	return out
}

// KBEntry 是统一词条视图。Fields 为可编辑字段集合：
//   - character：role / personality / appearance / relationships / state / notes
//   - setting：category / content
//   - faction/location/item：任意业务字段（如 leader、region、effect）
type KBEntry struct {
	ID           string            `json:"id"`
	Kind         EntryKind         `json:"kind"`
	Name         string            `json:"name"`
	Aliases      []string          `json:"aliases"`
	Summary      string            `json:"summary,omitempty"`
	Fields       map[string]string `json:"fields"`
	SourceRefs   []SourceRef       `json:"sourceRefs,omitempty"`
	Evidence     []string          `json:"evidence,omitempty"`
	LockedFields []string          `json:"lockedFields"`
	Confidence   float64           `json:"confidence,omitempty"`
	LastSeen     int               `json:"lastSeen,omitempty"`
	CreatedAt    time.Time         `json:"createdAt,omitempty"`
	UpdatedAt    time.Time         `json:"updatedAt,omitempty"`
	ReadOnly     bool              `json:"readOnly,omitempty"`
}

func (e *KBEntry) Normalize(now time.Time) {
	e.ID = strings.TrimSpace(e.ID)
	e.Name = strings.TrimSpace(e.Name)
	e.Name = memTruncate(e.Name, 80)
	if e.Kind == "" {
		e.Kind = EntryKindForID(e.ID)
	}
	e.Aliases = cleanAliases(e.Aliases, e.Name)
	e.Summary = memTruncate(strings.TrimSpace(e.Summary), 300)
	if e.Fields == nil {
		e.Fields = map[string]string{}
	}
	fields := make(map[string]string, len(e.Fields))
	for key, value := range e.Fields {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		fields[memTruncate(key, 40)] = memTruncate(strings.TrimSpace(value), 500)
	}
	e.Fields = fields
	e.LockedFields = dedupe(e.LockedFields)
	e.Evidence = dedupe(e.Evidence)
	e.SourceRefs = normalizeSourceRefs(e.SourceRefs)
	e.Confidence = clamp01(e.Confidence)
	e.LastSeen = max(e.LastSeen, 0)
	if e.CreatedAt.IsZero() {
		e.CreatedAt = now
	}
	if e.UpdatedAt.IsZero() {
		e.UpdatedAt = now
	}
	e.CreatedAt = e.CreatedAt.UTC()
	e.UpdatedAt = e.UpdatedAt.UTC()
	// knowledge.json 真源承载的角色/设定允许人工修正，但不允许经由扩展表删除。
	e.ReadOnly = false
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func normalizeSourceRefs(refs []SourceRef) []SourceRef {
	out := make([]SourceRef, 0, len(refs))
	seen := make(map[string]struct{}, len(refs))
	for _, ref := range refs {
		ref.ChapterID = strings.TrimSpace(ref.ChapterID)
		ref.Quote = strings.TrimSpace(ref.Quote)
		if ref.ChapterID == "" || ref.EndOffset <= ref.StartOffset {
			continue
		}
		key := fmt.Sprintf("%s:%d:%d", ref.ChapterID, ref.StartOffset, ref.EndOffset)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, ref)
	}
	return out
}

var ErrKBValidation = errors.New("novel: kb validation failed")

func kbValidationErrorf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrKBValidation, fmt.Sprintf(format, args...))
}

func (e KBEntry) Validate() error {
	if !ValidID(e.ID) || !strings.HasPrefix(e.ID, e.Kind.IDPrefix()) {
		return kbValidationErrorf("entry id %q must be %s id", e.ID, e.Kind.IDPrefix())
	}
	if !e.Kind.Valid() {
		return kbValidationErrorf("entry kind %q is not allowed", e.Kind)
	}
	if strings.TrimSpace(e.Name) == "" {
		return kbValidationErrorf("%s: name is required", e.ID)
	}
	for i, ref := range e.SourceRefs {
		if err := ValidateSourceRefStruct(ref); err != nil {
			return kbValidationErrorf("%s: sourceRefs[%d]: %v", e.ID, i, err)
		}
	}
	return nil
}

type RelationStatus string

const (
	RelationActive  RelationStatus = "active"
	RelationInvalid RelationStatus = "invalidated"
)

func (s RelationStatus) Valid() bool {
	return s == RelationActive || s == RelationInvalid
}

// Relation 是两个词条之间可人工编辑的关系边。
type Relation struct {
	ID           string         `json:"id"`
	FromEntryID  string         `json:"fromEntryId"`
	ToEntryID    string         `json:"toEntryId"`
	Type         string         `json:"type"`
	Label        string         `json:"label,omitempty"`
	Description  string         `json:"description,omitempty"`
	Status       RelationStatus `json:"status"`
	StartChapter int            `json:"startChapterIndex,omitempty"`
	EndChapter   int            `json:"endChapterIndex,omitempty"`
	SourceRefs   []SourceRef    `json:"sourceRefs,omitempty"`
	CreatedAt    time.Time      `json:"createdAt,omitempty"`
	UpdatedAt    time.Time      `json:"updatedAt,omitempty"`
}

func (r *Relation) Normalize(now time.Time) {
	r.ID = strings.TrimSpace(r.ID)
	r.FromEntryID = strings.TrimSpace(r.FromEntryID)
	r.ToEntryID = strings.TrimSpace(r.ToEntryID)
	r.Type = memTruncate(strings.TrimSpace(r.Type), 40)
	r.Label = memTruncate(strings.TrimSpace(r.Label), 80)
	r.Description = memTruncate(strings.TrimSpace(r.Description), 300)
	if r.Status == "" {
		r.Status = RelationActive
	}
	r.StartChapter = max(r.StartChapter, 0)
	r.EndChapter = max(r.EndChapter, 0)
	if r.EndChapter > 0 && r.EndChapter < r.StartChapter {
		r.EndChapter = r.StartChapter
	}
	r.SourceRefs = normalizeSourceRefs(r.SourceRefs)
	if r.CreatedAt.IsZero() {
		r.CreatedAt = now
	}
	if r.UpdatedAt.IsZero() {
		r.UpdatedAt = now
	}
	r.CreatedAt = r.CreatedAt.UTC()
	r.UpdatedAt = r.UpdatedAt.UTC()
}

func (r Relation) Validate() error {
	if !ValidID(r.ID) || !strings.HasPrefix(r.ID, "rel_") {
		return kbValidationErrorf("relation id %q must be rel_ id", r.ID)
	}
	if !ValidID(r.FromEntryID) || !ValidID(r.ToEntryID) {
		return kbValidationErrorf("%s: from/to entry id invalid", r.ID)
	}
	if r.FromEntryID == r.ToEntryID {
		return kbValidationErrorf("%s: self relation is not allowed", r.ID)
	}
	if r.Type == "" {
		return kbValidationErrorf("%s: relation type is required", r.ID)
	}
	if !r.Status.Valid() {
		return kbValidationErrorf("%s: status %q is not allowed", r.ID, r.Status)
	}
	return nil
}

// ActiveAt 判断关系在指定章序是否有效（0 表示未标注起止章）。
func (r Relation) ActiveAt(chapterIndex int) bool {
	if r.Status != RelationActive {
		return false
	}
	if r.StartChapter > 0 && chapterIndex < r.StartChapter {
		return false
	}
	if r.EndChapter > 0 && chapterIndex > r.EndChapter {
		return false
	}
	return true
}

type TimelineSource string

const (
	TimelineMemory TimelineSource = "memory"
	TimelineManual TimelineSource = "manual"
)

func (s TimelineSource) Valid() bool {
	return s == TimelineMemory || s == TimelineManual
}

// TimelineEvent 是全书时间线条目：既有记忆事件（只读派生）或人工补录事件。
type TimelineEvent struct {
	ID            string         `json:"id"`
	ChapterID     string         `json:"chapterId,omitempty"`
	ChapterIndex  int            `json:"chapterIndex"`
	Title         string         `json:"title"`
	Summary       string         `json:"summary,omitempty"`
	EventType     string         `json:"eventType,omitempty"`
	Participants  []string       `json:"participants,omitempty"`
	Entities      []string       `json:"entities,omitempty"`
	Source        TimelineSource `json:"source"`
	SourceEventID string         `json:"sourceEventId,omitempty"`
	StoryOrder    int            `json:"storyOrder"`
	CreatedAt     time.Time      `json:"createdAt,omitempty"`
	UpdatedAt     time.Time      `json:"updatedAt,omitempty"`
}

func (t *TimelineEvent) Normalize(now time.Time) {
	t.ID = strings.TrimSpace(t.ID)
	t.ChapterID = strings.TrimSpace(t.ChapterID)
	t.ChapterIndex = max(t.ChapterIndex, 0)
	t.Title = memTruncate(strings.TrimSpace(t.Title), 120)
	t.Summary = memTruncate(strings.TrimSpace(t.Summary), 300)
	t.EventType = memTruncate(strings.TrimSpace(t.EventType), 40)
	t.Participants = dedupe(t.Participants)
	t.Entities = dedupe(t.Entities)
	if t.Source == "" {
		t.Source = TimelineManual
	}
	t.SourceEventID = strings.TrimSpace(t.SourceEventID)
	t.StoryOrder = max(t.StoryOrder, 0)
	if t.CreatedAt.IsZero() {
		t.CreatedAt = now
	}
	if t.UpdatedAt.IsZero() {
		t.UpdatedAt = now
	}
	t.CreatedAt = t.CreatedAt.UTC()
	t.UpdatedAt = t.UpdatedAt.UTC()
}

func (t TimelineEvent) Validate() error {
	if !ValidID(t.ID) || !strings.HasPrefix(t.ID, "tl_") {
		return kbValidationErrorf("timeline id %q must be tl_ id", t.ID)
	}
	if t.ChapterID != "" && (!ValidID(t.ChapterID) || !strings.HasPrefix(t.ChapterID, "ch_")) {
		return kbValidationErrorf("%s: chapterId %q must be ch_ id", t.ID, t.ChapterID)
	}
	if t.Title == "" {
		return kbValidationErrorf("%s: title is required", t.ID)
	}
	if !t.Source.Valid() {
		return kbValidationErrorf("%s: source %q is not allowed", t.ID, t.Source)
	}
	return nil
}

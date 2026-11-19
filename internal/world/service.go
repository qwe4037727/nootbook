package world

import (
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"novelagent/internal/novel"
	"novelagent/internal/store"
)

// Service 是大世界（M2）读取与人工维护的业务层：
// 统一视图 = knowledge.json（角色/设定）+ agent.db.kb_entry（势力/地点/物品），
// 关系、时间线、字段锁与证据跳转都在此聚合。
type Service struct {
	st  *store.Store
	log *slog.Logger
}

func NewService(st *store.Store, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{st: st, log: logger}
}

var (
	ErrEntryNotFound    = errors.New("world: entry not found")
	ErrRelationNotFound = errors.New("world: relation not found")
	ErrTimelineNotFound = errors.New("world: timeline event not found")
	ErrChapterNotFound  = errors.New("world: chapter not found")
)

type EntryQuery struct {
	Kind    novel.EntryKind
	Query   string
	Chapter int
}

// ListEntries 返回统一词条视图（含锁定字段）。Chapter>0 时只返回该章之前已出现的词条。
func (s *Service) ListEntries(query EntryQuery) ([]novel.KBEntry, error) {
	entries, err := s.allEntries()
	if err != nil {
		return nil, err
	}
	var chapters []novel.Chapter
	if query.Chapter > 0 || strings.TrimSpace(query.Query) != "" {
		if chapters, err = s.st.LoadChapters(); err != nil {
			return nil, err
		}
	}
	if query.Chapter > 0 && query.Chapter > len(chapters) {
		return nil, ErrChapterNotFound
	}
	needle := strings.ToLower(strings.TrimSpace(query.Query))
	out := []novel.KBEntry{}
	for _, entry := range entries {
		if query.Kind != "" && entry.Kind != query.Kind {
			continue
		}
		if query.Chapter > 0 && introducedAt(entry, chapters) > query.Chapter {
			continue
		}
		if needle != "" && !entryMatches(entry, needle) {
			continue
		}
		out = append(out, entry)
	}
	return out, nil
}

// EntryCreate 是新建世界词条的请求。
type EntryCreate struct {
	Kind       novel.EntryKind
	Name       string
	Summary    string
	Aliases    []string
	Fields     map[string]string
	SourceRefs []novel.SourceRef
}

func (s *Service) CreateEntry(in EntryCreate) (novel.KBEntry, error) {
	if !in.Kind.Valid() {
		return novel.KBEntry{}, fmt.Errorf("%w: invalid kind", novel.ErrKBValidation)
	}
	if strings.TrimSpace(in.Name) == "" {
		return novel.KBEntry{}, fmt.Errorf("%w: name is required", novel.ErrKBValidation)
	}
	entry := novel.KBEntry{
		ID:         novel.NewID(strings.TrimSuffix(in.Kind.IDPrefix(), "_")),
		Kind:       in.Kind,
		Name:       in.Name,
		Summary:    in.Summary,
		Aliases:    in.Aliases,
		Fields:     in.Fields,
		SourceRefs: in.SourceRefs,
	}
	var saved novel.KBEntry
	var err error
	if in.Kind.Extended() {
		saved, err = s.st.SaveKBEntry(entry)
	} else {
		if err := validateEntryFields(novel.KBEntry{ID: entry.ID, Kind: entry.Kind}, entry.Fields); err != nil {
			return novel.KBEntry{}, err
		}
		saved, err = s.st.CreateKnowledgeEntry(entry)
	}
	if err != nil {
		return novel.KBEntry{}, err
	}
	s.log.Info("world entry created", "entry", saved.ID, "kind", saved.Kind, "name", saved.Name)
	saved.LockedFields = []string{}
	return saved, nil
}

func (s *Service) GetEntry(id string) (novel.KBEntry, error) {
	entries, err := s.allEntries()
	if err != nil {
		return novel.KBEntry{}, err
	}
	for _, entry := range entries {
		if entry.ID == id {
			return entry, nil
		}
	}
	return novel.KBEntry{}, ErrEntryNotFound
}

func (s *Service) allEntries() ([]novel.KBEntry, error) {
	knowledge, err := s.st.LoadKnowledge()
	if err != nil {
		return nil, err
	}
	locks, err := s.st.LoadFieldLocksIfPresent()
	if err != nil {
		return nil, err
	}
	extended, err := s.st.ListKBEntries("")
	if err != nil {
		return nil, err
	}

	out := make([]novel.KBEntry, 0, len(knowledge.Characters)+len(knowledge.World)+len(extended))
	for _, character := range knowledge.Characters {
		entry := store.ViewCharacter(character)
		entry.LockedFields = locks.Fields(entry.ID)
		out = append(out, entry)
	}
	for _, item := range knowledge.World {
		entry := store.ViewSetting(item)
		entry.LockedFields = locks.Fields(entry.ID)
		out = append(out, entry)
	}
	for _, entry := range extended {
		// Older setting edits could leave shadow rows that must never override or resurrect knowledge.json entries.
		if entry.Kind == novel.EntrySetting {
			continue
		}
		entry.LockedFields = locks.Fields(entry.ID)
		out = append(out, entry)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// EntryPatch 是人工编辑请求；Fields 只可包含该词条允许的字段名。
type EntryPatch struct {
	Name   string
	Fields map[string]string
}

func (s *Service) UpdateEntry(id string, patch EntryPatch) (novel.KBEntry, error) {
	entry, err := s.GetEntry(id)
	if err != nil {
		return novel.KBEntry{}, err
	}
	if err := validateEntryFields(entry, patch.Fields); err != nil {
		return novel.KBEntry{}, err
	}
	update := novel.KBEntry{
		ID:     entry.ID,
		Kind:   entry.Kind,
		Name:   strings.TrimSpace(patch.Name),
		Fields: patch.Fields,
	}
	if entry.Kind.Extended() {
		// 扩展词条整体存于 kb_entry：保留未修改的元数据，仅合并可编辑字段。
		merged := entry
		if update.Name != "" {
			merged.Name = update.Name
		}
		merged.Fields = make(map[string]string, len(entry.Fields)+len(patch.Fields))
		for key, value := range entry.Fields {
			merged.Fields[key] = value
		}
		for key, value := range patch.Fields {
			merged.Fields[key] = value
		}
		update = merged
	}
	if _, err := s.st.UpdateKnowledgeEntry(update); err != nil {
		if errors.Is(err, store.ErrKnowledgeEntryNotFound) {
			return novel.KBEntry{}, ErrEntryNotFound
		}
		return novel.KBEntry{}, err
	}
	s.log.Info("world entry updated", "entry", id, "name", update.Name, "fields", fieldNames(patch.Fields))
	return s.GetEntry(id)
}

func (s *Service) DeleteEntry(id string) error {
	if _, err := s.GetEntry(id); err != nil {
		return err
	}
	if err := s.st.DeleteKnowledgeEntry(id); err != nil {
		if errors.Is(err, store.ErrKnowledgeEntryNotFound) {
			return ErrEntryNotFound
		}
		return err
	}
	s.log.Info("world entry deleted", "entry", id)
	return nil
}

// SetFieldLocks 以全量替换语义设置锁定字段；lock=true 写入，false 解锁。
func (s *Service) SetFieldLocks(id string, fields []string, lock bool) ([]string, error) {
	entry, err := s.GetEntry(id)
	if err != nil {
		return nil, err
	}
	current := append([]string{}, entry.LockedFields...)
	next := map[string]struct{}{}
	for _, field := range current {
		next[field] = struct{}{}
	}
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		if !allowedField(entry, field) {
			return nil, fmt.Errorf("%w: field %q is not editable on %s", novel.ErrKBValidation, field, entry.ID)
		}
		if lock {
			next[field] = struct{}{}
		} else {
			delete(next, field)
		}
	}
	result := make([]string, 0, len(next))
	for field := range next {
		result = append(result, field)
	}
	sort.Strings(result)
	if err := s.st.SetKBFieldLocks(id, result); err != nil {
		return nil, err
	}
	s.log.Info("world field locks updated", "entry", id, "locked", result)
	return result, nil
}

// EvidenceItem 是证据跳转的最小单元：章节 + rune 区间 + 引文。
type EvidenceItem struct {
	ChapterID    string `json:"chapterId"`
	ChapterIndex int    `json:"chapterIndex"`
	ChapterTitle string `json:"chapterTitle"`
	StartOffset  int    `json:"startOffset"`
	EndOffset    int    `json:"endOffset"`
	Quote        string `json:"quote"`
	Source       string `json:"source"`
}

// Evidence 把词条证据解析为可跳转的章节区间。
// knowledge 证据只有引文（无章节号），按全文定位；扩展词条优先使用 SourceRefs 的偏移。
func (s *Service) Evidence(id string) ([]EvidenceItem, error) {
	entry, err := s.GetEntry(id)
	if err != nil {
		return nil, err
	}
	chapters, err := s.st.LoadChapters()
	if err != nil {
		return nil, err
	}
	byID := make(map[string]novel.Chapter, len(chapters))
	for _, chapter := range chapters {
		byID[chapter.ID] = chapter
	}

	out := []EvidenceItem{}
	for _, ref := range entry.SourceRefs {
		chapter, ok := byID[ref.ChapterID]
		if !ok {
			continue
		}
		item := EvidenceItem{
			ChapterID:    chapter.ID,
			ChapterIndex: chapter.Index,
			ChapterTitle: chapter.Title,
			StartOffset:  ref.StartOffset,
			EndOffset:    ref.EndOffset,
			Quote:        ref.Quote,
			Source:       "sourceRef",
		}
		if item.Quote == "" {
			item.Quote = sliceRunes(chapter.Content, ref.StartOffset, ref.EndOffset)
		}
		if item.Quote == "" {
			continue
		}
		out = append(out, item)
	}
	if len(out) > 0 {
		return out, nil
	}

	for _, quote := range knowledgeEvidence(entry) {
		for _, chapter := range chapters {
			start, end, ok := locateRunes(chapter.Content, quote)
			if !ok {
				continue
			}
			out = append(out, EvidenceItem{
				ChapterID:    chapter.ID,
				ChapterIndex: chapter.Index,
				ChapterTitle: chapter.Title,
				StartOffset:  start,
				EndOffset:    end,
				Quote:        quote,
				Source:       "evidence",
			})
		}
	}
	return out, nil
}

// ---- 关系 ----

func (s *Service) ListRelations(entryID, status string) ([]novel.Relation, error) {
	return s.st.ListRelations(entryID, status)
}

func (s *Service) SaveRelation(rel novel.Relation) (novel.Relation, error) {
	if strings.TrimSpace(rel.ID) == "" {
		rel.ID = novel.NewID("rel")
	} else if existing, err := s.st.GetRelation(rel.ID); err == nil {
		rel.CreatedAt = existing.CreatedAt
	}
	rel.Normalize(time.Now().UTC())
	if err := rel.Validate(); err != nil {
		return novel.Relation{}, err
	}
	if err := s.requireEntry(rel.FromEntryID); err != nil {
		return novel.Relation{}, err
	}
	if err := s.requireEntry(rel.ToEntryID); err != nil {
		return novel.Relation{}, err
	}
	saved, err := s.st.SaveRelation(rel)
	if err != nil {
		return novel.Relation{}, err
	}
	s.log.Info("world relation saved", "relation", saved.ID, "from", saved.FromEntryID, "to", saved.ToEntryID, "type", saved.Type)
	return saved, nil
}

func (s *Service) DeleteRelation(id string) error {
	if err := s.st.DeleteRelation(id); err != nil {
		if errors.Is(err, store.ErrRelationNotFound) {
			return ErrRelationNotFound
		}
		return err
	}
	s.log.Info("world relation deleted", "relation", id)
	return nil
}

// ---- 时间线 ----

func (s *Service) Timeline() ([]novel.TimelineEvent, error) {
	memoryEvents, _, _, _, _, err := s.st.LoadMemoryRecords()
	if err != nil {
		return nil, err
	}
	manual, err := s.st.ListManualTimelineEvents()
	if err != nil {
		return nil, err
	}
	project, err := s.st.LoadProject()
	if err != nil {
		return nil, err
	}
	indexByID := make(map[string]int, len(project.Chapters))
	titleByID := make(map[string]string, len(project.Chapters))
	for _, meta := range project.Chapters {
		indexByID[meta.ID] = meta.Index
		titleByID[meta.ID] = meta.Title
	}

	out := make([]novel.TimelineEvent, 0, len(memoryEvents)+len(manual))
	for _, event := range memoryEvents {
		if event.Status == novel.EventInvalidated {
			continue
		}
		out = append(out, novel.TimelineEvent{
			ID:            event.ID,
			ChapterID:     event.ChapterID,
			ChapterIndex:  indexByID[event.ChapterID],
			Title:         event.Summary,
			Summary:       event.Summary,
			EventType:     event.EventType,
			Participants:  event.Participants,
			Entities:      event.RelatedEntities,
			Source:        novel.TimelineMemory,
			SourceEventID: event.ID,
			StoryOrder:    event.StoryOrder,
			CreatedAt:     event.CreatedAt,
			UpdatedAt:     event.UpdatedAt,
		})
	}
	for _, event := range manual {
		if event.ChapterIndex == 0 && event.ChapterID != "" {
			event.ChapterIndex = indexByID[event.ChapterID]
		}
		out = append(out, event)
	}
	sortTimeline(out)
	return out, nil
}

type TimelinePatch struct {
	ChapterID    string
	Title        string
	Summary      string
	EventType    string
	Participants []string
	Entities     []string
	StoryOrder   int
}

func (s *Service) SaveTimelineEvent(id string, patch TimelinePatch) (novel.TimelineEvent, error) {
	if strings.TrimSpace(patch.Title) == "" {
		return novel.TimelineEvent{}, fmt.Errorf("%w: title is required", novel.ErrKBValidation)
	}
	chapterIndex := 0
	if chapterID := strings.TrimSpace(patch.ChapterID); chapterID != "" {
		chapter, err := s.st.LoadChapter(chapterID)
		if err != nil {
			if errors.Is(err, store.ErrChapterNotFound) || errors.Is(err, store.ErrInvalidChapter) {
				return novel.TimelineEvent{}, ErrChapterNotFound
			}
			return novel.TimelineEvent{}, err
		}
		chapterIndex = chapter.Index
	}
	event := novel.TimelineEvent{
		ID:           strings.TrimSpace(id),
		ChapterID:    strings.TrimSpace(patch.ChapterID),
		ChapterIndex: chapterIndex,
		Title:        patch.Title,
		Summary:      patch.Summary,
		EventType:    patch.EventType,
		Participants: patch.Participants,
		Entities:     patch.Entities,
		Source:       novel.TimelineManual,
		StoryOrder:   patch.StoryOrder,
	}
	if event.ID != "" {
		existing, err := s.manualEvent(event.ID)
		if err != nil {
			return novel.TimelineEvent{}, err
		}
		event.CreatedAt = existing.CreatedAt
	}
	saved, err := s.st.SaveTimelineEvent(event)
	if err != nil {
		return novel.TimelineEvent{}, err
	}
	s.log.Info("world timeline saved", "event", saved.ID, "chapter", saved.ChapterID, "title", saved.Title)
	return saved, nil
}

func (s *Service) DeleteTimelineEvent(id string) error {
	if err := s.st.DeleteTimelineEvent(id); err != nil {
		if errors.Is(err, store.ErrTimelineNotFound) {
			return ErrTimelineNotFound
		}
		return err
	}
	s.log.Info("world timeline deleted", "event", id)
	return nil
}

func (s *Service) manualEvent(id string) (novel.TimelineEvent, error) {
	events, err := s.st.ListManualTimelineEvents()
	if err != nil {
		return novel.TimelineEvent{}, err
	}
	for _, event := range events {
		if event.ID == id {
			return event, nil
		}
	}
	return novel.TimelineEvent{}, ErrTimelineNotFound
}

// ---- 第 N 章快照 ----

type Snapshot struct {
	Chapter   int                    `json:"chapter"`
	Entries   []novel.KBEntry        `json:"entries"`
	Relations []novel.Relation       `json:"relations"`
	Timeline  []novel.TimelineEvent  `json:"timeline"`
	Summaries []novel.ChapterSummary `json:"summaries"`
}

// Snapshot 返回“回到第 N 章”时刻的大世界视图（按证据出现位置近似）。
func (s *Service) Snapshot(chapter int) (Snapshot, error) {
	if chapter <= 0 {
		return Snapshot{}, ErrChapterNotFound
	}
	chapters, err := s.st.LoadChapters()
	if err != nil {
		return Snapshot{}, err
	}
	if chapter > len(chapters) {
		return Snapshot{}, ErrChapterNotFound
	}
	entries, err := s.allEntries()
	if err != nil {
		return Snapshot{}, err
	}
	knowledge, err := s.st.LoadKnowledge()
	if err != nil {
		return Snapshot{}, err
	}

	snap := Snapshot{Chapter: chapter, Entries: []novel.KBEntry{}, Relations: []novel.Relation{}, Timeline: []novel.TimelineEvent{}, Summaries: []novel.ChapterSummary{}}
	known := map[string]struct{}{}
	for _, entry := range entries {
		if introducedAt(entry, chapters) > chapter {
			continue
		}
		snap.Entries = append(snap.Entries, entry)
		known[entry.ID] = struct{}{}
	}
	relations, err := s.st.ListRelations("", string(novel.RelationActive))
	if err != nil {
		return Snapshot{}, err
	}
	for _, rel := range relations {
		if !rel.ActiveAt(chapter) {
			continue
		}
		if _, ok := known[rel.FromEntryID]; !ok {
			continue
		}
		if _, ok := known[rel.ToEntryID]; !ok {
			continue
		}
		snap.Relations = append(snap.Relations, rel)
	}
	timeline, err := s.Timeline()
	if err != nil {
		return Snapshot{}, err
	}
	for _, event := range timeline {
		if event.ChapterIndex <= chapter {
			snap.Timeline = append(snap.Timeline, event)
		}
	}
	for _, summary := range knowledge.Summaries {
		if summary.Index <= chapter {
			snap.Summaries = append(snap.Summaries, summary)
		}
	}
	return snap, nil
}

// ---- 辅助 ----

func (s *Service) requireEntry(id string) error {
	entries, err := s.allEntries()
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.ID == id {
			return nil
		}
	}
	return fmt.Errorf("%w: %s", ErrEntryNotFound, id)
}

func entryMatches(entry novel.KBEntry, needle string) bool {
	if strings.Contains(strings.ToLower(entry.Name), needle) {
		return true
	}
	for _, alias := range entry.Aliases {
		if strings.Contains(strings.ToLower(alias), needle) {
			return true
		}
	}
	if strings.Contains(strings.ToLower(entry.Summary), needle) {
		return true
	}
	for key, value := range entry.Fields {
		if strings.Contains(strings.ToLower(key), needle) || strings.Contains(strings.ToLower(value), needle) {
			return true
		}
	}
	return false
}

func knowledgeEvidence(entry novel.KBEntry) []string {
	return entry.Evidence
}

func allowedField(entry novel.KBEntry, field string) bool {
	switch field {
	case "name", "summary":
		return true
	}
	if entry.Kind == novel.EntryCharacter {
		switch field {
		case "role", "personality", "appearance", "relationships", "state", "notes":
			return true
		}
		return false
	}
	if entry.Kind == novel.EntryFaction || entry.Kind == novel.EntryLocation || entry.Kind == novel.EntryItem {
		return true
	}
	if _, ok := entry.Fields[field]; ok {
		return true
	}
	if entry.Kind == novel.EntrySetting && (field == "category" || field == "content") {
		return true
	}
	return false
}

func validateEntryFields(entry novel.KBEntry, fields map[string]string) error {
	for field := range fields {
		if !allowedField(entry, field) {
			return fmt.Errorf("%w: field %q is not editable on %s", novel.ErrKBValidation, field, entry.ID)
		}
	}
	return nil
}

func fieldNames(fields map[string]string) []string {
	out := make([]string, 0, len(fields))
	for field := range fields {
		out = append(out, field)
	}
	sort.Strings(out)
	return out
}

// introducedAt 估算词条首次出现的章序：优先证据引文定位，否则视为开局已知。
func introducedAt(entry novel.KBEntry, chapters []novel.Chapter) int {
	best := 0
	for _, ref := range entry.SourceRefs {
		for _, chapter := range chapters {
			if chapter.ID == ref.ChapterID {
				if best == 0 || chapter.Index < best {
					best = chapter.Index
				}
			}
		}
	}
	quotes := knowledgeEvidence(entry)
	for _, alias := range entry.Aliases {
		quotes = append(quotes, alias)
	}
	for _, quote := range quotes {
		quote = strings.TrimSpace(quote)
		if quote == "" {
			continue
		}
		for _, chapter := range chapters {
			if _, _, ok := locateRunes(chapter.Content, quote); ok {
				if best == 0 || chapter.Index < best {
					best = chapter.Index
				}
			}
		}
	}
	if best == 0 {
		return 1
	}
	return best
}

func locateRunes(content, quote string) (int, int, bool) {
	byteIdx := strings.Index(content, quote)
	if byteIdx < 0 {
		return 0, 0, false
	}
	start := utf8.RuneCountInString(content[:byteIdx])
	length := utf8.RuneCountInString(quote)
	return start, start + length, true
}

func sliceRunes(content string, start, end int) string {
	runes := []rune(content)
	if start < 0 || end <= start || end > len(runes) {
		return ""
	}
	return string(runes[start:end])
}

func sortTimeline(items []novel.TimelineEvent) {
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].ChapterIndex != items[j].ChapterIndex {
			return items[i].ChapterIndex < items[j].ChapterIndex
		}
		if items[i].StoryOrder != items[j].StoryOrder {
			return items[i].StoryOrder < items[j].StoryOrder
		}
		return items[i].ID < items[j].ID
	})
}

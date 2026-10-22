package novel

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type Chapter struct {
	ID        string    `json:"id"`
	Index     int       `json:"index"`
	Title     string    `json:"title"`
	Content   string    `json:"content"`
	CharCount int       `json:"charCount"`
	// Revision 为章节级版本号（决策 8）：章节文件是真源，Title 或 Content
	// 实际变化时递增；与 Project.Revision 职责分离，不互相推导。
	Revision  int64     `json:"revision"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type ChapterMeta struct {
	ID        string    `json:"id"`
	Index     int       `json:"index"`
	Title     string    `json:"title"`
	CharCount int       `json:"charCount"`
	// Revision 为章节文件 Revision 的缓存；不一致时加载以 chapter 文件修复。
	Revision  int64     `json:"revision"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (c Chapter) Meta() ChapterMeta {
	return ChapterMeta{
		ID:        c.ID,
		Index:     c.Index,
		Title:     c.Title,
		CharCount: c.CharCount,
		Revision:  c.Revision,
		UpdatedAt: c.UpdatedAt,
	}
}

type AnalysisStatus string

const (
	AnalysisIdle    AnalysisStatus = "idle"
	AnalysisPending AnalysisStatus = "pending"
	AnalysisRunning AnalysisStatus = "analyzing"
	AnalysisReady   AnalysisStatus = "ready"
	AnalysisError   AnalysisStatus = "error"
)

type AnalysisState struct {
	Status        AnalysisStatus `json:"status"`
	AnalyzedCount int            `json:"analyzedCount"`
	TotalCount    int            `json:"totalCount"`
	Message       string         `json:"message,omitempty"`
	UpdatedAt     time.Time      `json:"updatedAt"`
}

type Project struct {
	Title     string        `json:"title"`
	Chapters  []ChapterMeta `json:"chapters"`
	Analysis  AnalysisState `json:"analysis"`
	Revision  int64         `json:"revision"`
	UpdatedAt time.Time     `json:"updatedAt"`
}

func NewProject() Project {
	return Project{
		Title:    "未命名小说",
		Chapters: []ChapterMeta{},
		Analysis: AnalysisState{Status: AnalysisIdle},
	}
}

func (p *Project) Normalize() {
	p.Title = strings.TrimSpace(p.Title)
	if p.Title == "" {
		p.Title = "未命名小说"
	}

	chapters := make([]ChapterMeta, 0, len(p.Chapters))
	for _, c := range p.Chapters {
		c.ID = strings.TrimSpace(c.ID)
		c.Title = strings.TrimSpace(c.Title)
		if c.ID == "" || c.Title == "" {
			continue
		}
		c.CharCount = max(c.CharCount, 0)
		chapters = append(chapters, c)
	}
	sort.SliceStable(chapters, func(i, j int) bool {
		if chapters[i].Index == chapters[j].Index {
			return chapters[i].ID < chapters[j].ID
		}
		return chapters[i].Index < chapters[j].Index
	})
	for i := range chapters {
		chapters[i].Index = i + 1
	}
	if chapters == nil {
		chapters = []ChapterMeta{}
	}
	p.Chapters = chapters

	state := p.Analysis
	if state.Status == "" {
		state.Status = AnalysisIdle
	}
	switch state.Status {
	case AnalysisIdle, AnalysisPending, AnalysisRunning, AnalysisReady, AnalysisError:
	default:
		state.Status = AnalysisIdle
	}
	state.TotalCount = len(chapters)
	state.AnalyzedCount = min(max(state.AnalyzedCount, 0), state.TotalCount)
	if state.TotalCount == 0 {
		state.Status = AnalysisIdle
		state.AnalyzedCount = 0
		state.Message = ""
	}
	if state.Status == AnalysisReady && state.AnalyzedCount < state.TotalCount {
		state.AnalyzedCount = state.TotalCount
	}
	state.Message = truncateRunes(strings.TrimSpace(state.Message), 200)
	p.Analysis = state
}

type Character struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Aliases       []string `json:"aliases,omitempty"`
	Role          string   `json:"role,omitempty"`
	Personality   string   `json:"personality,omitempty"`
	Appearance    string   `json:"appearance,omitempty"`
	Relationships string   `json:"relationships,omitempty"`
	State         string   `json:"state,omitempty"`
	Notes         string   `json:"notes,omitempty"`
	Evidence      []string `json:"evidence,omitempty"`
	Confidence    float64  `json:"confidence,omitempty"`
	LastSeen      int      `json:"lastSeen,omitempty"`
}

type WorldEntry struct {
	ID         string   `json:"id"`
	Title      string   `json:"title"`
	Aliases    []string `json:"aliases,omitempty"`
	Category   string   `json:"category,omitempty"`
	Content    string   `json:"content"`
	Evidence   []string `json:"evidence,omitempty"`
	Confidence float64  `json:"confidence,omitempty"`
	LastSeen   int      `json:"lastSeen,omitempty"`
}

type StyleProfile struct {
	Perspective string   `json:"perspective,omitempty"`
	Tone        string   `json:"tone,omitempty"`
	Sentence    string   `json:"sentence,omitempty"`
	Dialogue    string   `json:"dialogue,omitempty"`
	Pacing      string   `json:"pacing,omitempty"`
	Notes       string   `json:"notes,omitempty"`
	Samples     []string `json:"samples,omitempty"`
}

type ChapterSummary struct {
	ChapterID string `json:"chapterId"`
	Index     int    `json:"index"`
	Title     string `json:"title,omitempty"`
	Summary   string `json:"summary"`
	EndsAt    string `json:"endsAt,omitempty"`
}

type Knowledge struct {
	Overview   string           `json:"overview,omitempty"`
	Style      StyleProfile     `json:"style"`
	Characters []Character      `json:"characters"`
	World      []WorldEntry     `json:"world"`
	Summaries  []ChapterSummary `json:"summaries"`
	UpdatedAt  time.Time        `json:"updatedAt"`
}

func NewKnowledge() Knowledge {
	return Knowledge{
		Characters: []Character{},
		World:      []WorldEntry{},
		Summaries:  []ChapterSummary{},
	}
}

func (k *Knowledge) Normalize() {
	k.Overview = strings.TrimSpace(k.Overview)

	characters := make([]Character, 0, len(k.Characters))
	seenCharacters := make(map[string]int)
	for _, c := range k.Characters {
		c.ID = strings.TrimSpace(c.ID)
		c.Name = strings.TrimSpace(c.Name)
		if c.Name == "" {
			continue
		}
		c.Role = strings.TrimSpace(c.Role)
		c.Personality = strings.TrimSpace(c.Personality)
		c.Appearance = strings.TrimSpace(c.Appearance)
		c.Relationships = strings.TrimSpace(c.Relationships)
		c.State = strings.TrimSpace(c.State)
		c.Notes = strings.TrimSpace(c.Notes)
		c.Aliases = cleanAliases(c.Aliases, c.Name)
		c.Evidence = cleanList(c.Evidence, 6)
		c.Confidence = clampConfidence(c.Confidence)
		c.LastSeen = max(c.LastSeen, 0)
		key := normalizeKey(c.Name)
		if i, ok := seenCharacters[key]; ok {
			characters[i] = mergeCharacter(characters[i], c)
			continue
		}
		if c.ID == "" {
			c.ID = NewID("c")
		}
		seenCharacters[key] = len(characters)
		characters = append(characters, c)
	}
	if characters == nil {
		characters = []Character{}
	}
	k.Characters = characters

	world := make([]WorldEntry, 0, len(k.World))
	seenWorld := make(map[string]int)
	for _, w := range k.World {
		w.ID = strings.TrimSpace(w.ID)
		w.Title = strings.TrimSpace(w.Title)
		if w.Title == "" {
			continue
		}
		w.Category = strings.TrimSpace(w.Category)
		w.Content = strings.TrimSpace(w.Content)
		w.Aliases = cleanAliases(w.Aliases, w.Title)
		w.Evidence = cleanList(w.Evidence, 6)
		w.Confidence = clampConfidence(w.Confidence)
		w.LastSeen = max(w.LastSeen, 0)
		key := normalizeKey(w.Title)
		if i, ok := seenWorld[key]; ok {
			world[i] = mergeWorldEntry(world[i], w)
			continue
		}
		if w.ID == "" {
			w.ID = NewID("w")
		}
		seenWorld[key] = len(world)
		world = append(world, w)
	}
	if world == nil {
		world = []WorldEntry{}
	}
	k.World = world

	style := k.Style
	style.Perspective = strings.TrimSpace(style.Perspective)
	style.Tone = strings.TrimSpace(style.Tone)
	style.Sentence = strings.TrimSpace(style.Sentence)
	style.Dialogue = strings.TrimSpace(style.Dialogue)
	style.Pacing = strings.TrimSpace(style.Pacing)
	style.Notes = strings.TrimSpace(style.Notes)
	style.Samples = cleanList(style.Samples, 8)
	if style.Samples == nil {
		style.Samples = []string{}
	}
	k.Style = style

	summaries := make([]ChapterSummary, 0, len(k.Summaries))
	seenSummaries := make(map[int]ChapterSummary)
	for _, s := range k.Summaries {
		s.ChapterID = strings.TrimSpace(s.ChapterID)
		s.Title = strings.TrimSpace(s.Title)
		s.Summary = strings.TrimSpace(s.Summary)
		s.EndsAt = strings.TrimSpace(s.EndsAt)
		if s.Index <= 0 || s.Summary == "" {
			continue
		}
		if previous, ok := seenSummaries[s.Index]; ok && previous.Summary != "" {
			s.ChapterID = previous.ChapterID
		}
		seenSummaries[s.Index] = s
	}
	for _, s := range seenSummaries {
		summaries = append(summaries, s)
	}
	sort.Slice(summaries, func(i, j int) bool { return summaries[i].Index < summaries[j].Index })
	if summaries == nil {
		summaries = []ChapterSummary{}
	}
	k.Summaries = summaries
}

func mergeCharacter(base, update Character) Character {
	if update.ID != "" {
		base.ID = update.ID
	}
	if update.Name != "" {
		base.Name = update.Name
	}
	base.Aliases = cleanAliases(append(base.Aliases, update.Aliases...), base.Name)
	base.Role = firstNonEmpty(update.Role, base.Role)
	base.Personality = firstNonEmpty(update.Personality, base.Personality)
	base.Appearance = firstNonEmpty(update.Appearance, base.Appearance)
	base.Relationships = firstNonEmpty(update.Relationships, base.Relationships)
	base.State = firstNonEmpty(update.State, base.State)
	base.Notes = firstNonEmpty(update.Notes, base.Notes)
	base.Evidence = cleanList(append(update.Evidence, base.Evidence...), 6)
	base.Confidence = max(base.Confidence, update.Confidence)
	base.LastSeen = max(base.LastSeen, update.LastSeen)
	return base
}

func mergeWorldEntry(base, update WorldEntry) WorldEntry {
	if update.ID != "" {
		base.ID = update.ID
	}
	if update.Title != "" {
		base.Title = update.Title
	}
	base.Aliases = cleanAliases(append(base.Aliases, update.Aliases...), base.Title)
	base.Category = firstNonEmpty(update.Category, base.Category)
	base.Content = firstNonEmpty(update.Content, base.Content)
	base.Evidence = cleanList(append(update.Evidence, base.Evidence...), 6)
	base.Confidence = max(base.Confidence, update.Confidence)
	base.LastSeen = max(base.LastSeen, update.LastSeen)
	return base
}

func cleanList(values []string, limit int) []string {
	result := make([]string, 0, min(len(values), limit))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := normalizeKey(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, value)
		if len(result) >= limit {
			break
		}
	}
	return result
}

func cleanAliases(aliases []string, name string) []string {
	nameKey := normalizeKey(name)
	filtered := make([]string, 0, len(aliases))
	for _, alias := range aliases {
		if normalizeKey(alias) == nameKey {
			continue
		}
		filtered = append(filtered, alias)
	}
	return cleanList(filtered, 8)
}

func clampConfidence(value float64) float64 {
	if value <= 0 {
		return 0
	}
	return min(value, 1)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func normalizeKey(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(value), ""))
}

func NewID(prefix string) string {
	var b [9]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
	}
	id := hex.EncodeToString(b[:])
	if prefix == "" {
		return id
	}
	return prefix + "_" + id
}

func ValidID(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
		default:
			return false
		}
	}
	return true
}

func CountChars(text string) int {
	count := 0
	for _, r := range text {
		if unicode.IsSpace(r) || r == '\u200b' || r == '\u200c' || r == '\u200d' || r == '\ufeff' {
			continue
		}
		count++
	}
	return count
}

func DefaultChapterTitle(index int) string {
	return fmt.Sprintf("第%d章", index)
}

func InferTitle(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if i >= 30 {
			break
		}
		line = strings.TrimSpace(strings.TrimPrefix(line, "\ufeff"))
		for _, prefix := range []string{"书名：", "书名:", "作品名：", "作品名:"} {
			if strings.HasPrefix(line, prefix) {
				return strings.TrimSpace(strings.TrimPrefix(line, prefix))
			}
		}
	}
	return ""
}

func truncateRunes(text string, limit int) string {
	if limit <= 0 || utf8.RuneCountInString(text) <= limit {
		return text
	}
	runes := []rune(text)
	return string(runes[:limit]) + "…"
}

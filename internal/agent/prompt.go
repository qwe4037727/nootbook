package agent

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"novelagent/internal/novel"
)

const defaultDirection = "无额外指定，顺着当前情节自然发展。保持已有冲突、人物动机和叙事张力，不要草率收尾。"
const defaultContextChars = 12000
const defaultTargetChars = 800

const systemPrompt = `你是一位专业的中文小说续写引擎。你唯一的输出是续写正文。

规则：
1. 只输出续写正文，不要标题、序号、说明、总结、大纲或 Markdown。
2. 从上文最后一句直接接续，第一句要自然衔接，不重复已有句子或段落。
3. 以【最近正文】为最高事实来源：人物状态、称谓、伤势、地点、已发生事件，一律以最新正文为准；知识摘要与正文冲突时，服从正文。
4. 保持原文的人称、视角、时态、语气、句式节奏、段落密度和对白习惯。
5. 依据【当前人物状态】推动行动与对白，不要遗忘伤势、目标、关系变化；不要让已退场或已死亡的人物无故出现。
6. 不要重复揭示已经揭示过的身份、伏笔或设定，不要回顾前情。
7. 承接当前场景的悬念或冲突继续写，不要跳过场景直接收束，不要草率解决核心矛盾。
8. 非必要不新增重要人物、设定或道具；需要新增时，必须与现有世界观一致。
9. 不写作者旁白，不解释意图，不在结尾写总结。
10. 在自然的情节停顿处结束，篇幅接近目标字数。`

type Input struct {
	Title        string
	Knowledge    novel.Knowledge
	Chapters     []novel.Chapter
	Direction    string
	TargetChars  int
	ContextChars int
}

type Prompt struct {
	System       string
	User         string
	ContextChars int
	Truncated    bool
	Characters   int
	WorldEntries int
	Summaries    int
}

func Build(input Input) Prompt {
	contextChars := input.ContextChars
	if contextChars <= 0 {
		contextChars = defaultContextChars
	}
	targetChars := input.TargetChars
	if targetChars <= 0 {
		targetChars = defaultTargetChars
	}
	direction := strings.TrimSpace(input.Direction)
	if direction == "" {
		direction = defaultDirection
	}

	chapters := sortedChapters(input.Chapters)
	recent, truncated, startIndex := manuscriptTail(chapters, contextChars)
	recentWindow := tailRunes(recent, min(contextChars, 8000))
	characters := selectCharacters(input.Knowledge, recentWindow, startIndex)
	world := selectWorld(input.Knowledge, recentWindow, startIndex)
	summaries := selectSummaries(input.Knowledge.Summaries, chapters, startIndex)

	var b strings.Builder
	fmt.Fprintf(&b, "【作品】《%s》\n", displayTitle(input.Title))

	if overview := strings.TrimSpace(input.Knowledge.Overview); overview != "" {
		b.WriteString("\n【核心设定与当前进展】\n")
		b.WriteString(overview)
		b.WriteString("\n")
	}

	b.WriteString("\n【文风指纹】\n")
	if !formatStyle(&b, input.Knowledge.Style) {
		b.WriteString("严格贴合下方正文的叙述视角、句式节奏、段落密度和对白习惯。\n")
	}

	if len(characters) > 0 {
		b.WriteString("\n【当前人物状态】\n")
		for _, character := range characters {
			formatCharacter(&b, character)
		}
	}

	if len(world) > 0 {
		b.WriteString("\n【相关设定】\n")
		for _, entry := range world {
			formatWorld(&b, entry)
		}
	}

	if len(summaries) > 0 {
		b.WriteString("\n【前情提要】\n")
		for _, summary := range summaries {
			label := fmt.Sprintf("第%d章", summary.Index)
			if summary.Title != "" {
				label += " " + summary.Title
			}
			text := clipRunes(summary.Summary, 180)
			fmt.Fprintf(&b, "%s：%s\n", label, text)
		}
	}

	b.WriteString("\n【最近正文（末尾即续写起点）】\n")
	b.WriteString(recent)
	b.WriteString("\n")

	b.WriteString("\n【本次续写】\n")
	fmt.Fprintf(&b, "剧情方向：%s\n", direction)
	fmt.Fprintf(&b, "目标篇幅：约 %d 字。\n", targetChars)
	b.WriteString("请从最近正文末尾直接续写，不要复述、概括或重写已有内容。")

	return Prompt{
		System:       systemPrompt,
		User:         b.String(),
		ContextChars: utf8.RuneCountInString(recent),
		Truncated:    truncated,
		Characters:   len(characters),
		WorldEntries: len(world),
		Summaries:    len(summaries),
	}
}

func formatStyle(b *strings.Builder, style novel.StyleProfile) bool {
	var parts []string
	if style.Perspective != "" {
		parts = append(parts, "视角："+style.Perspective)
	}
	if style.Tone != "" {
		parts = append(parts, "语气："+style.Tone)
	}
	if style.Sentence != "" {
		parts = append(parts, "句式："+style.Sentence)
	}
	if style.Dialogue != "" {
		parts = append(parts, "对白："+style.Dialogue)
	}
	if style.Pacing != "" {
		parts = append(parts, "节奏："+style.Pacing)
	}
	if style.Notes != "" {
		parts = append(parts, "补充："+style.Notes)
	}
	if len(parts) > 0 {
		b.WriteString(strings.Join(parts, "；"))
		b.WriteString("\n")
	}
	if len(style.Samples) > 0 {
		b.WriteString("原文示例：\n")
		for i, sample := range style.Samples {
			if i >= 2 {
				break
			}
			fmt.Fprintf(b, "- %s\n", clipRunes(sample, 100))
		}
	}
	return len(parts) > 0 || len(style.Samples) > 0
}

func formatCharacter(b *strings.Builder, character novel.Character) {
	b.WriteString("- ")
	b.WriteString(character.Name)
	if len(character.Aliases) > 0 {
		fmt.Fprintf(b, "（别名：%s）", strings.Join(character.Aliases, "、"))
	}
	var fields []string
	if character.Role != "" {
		fields = append(fields, "身份："+character.Role)
	}
	if character.State != "" {
		fields = append(fields, "当前："+character.State)
	}
	if character.Personality != "" {
		fields = append(fields, "性格："+character.Personality)
	}
	if character.Relationships != "" {
		fields = append(fields, "关系："+character.Relationships)
	}
	if character.Appearance != "" {
		fields = append(fields, "外貌："+character.Appearance)
	}
	if character.Notes != "" {
		fields = append(fields, "补充："+character.Notes)
	}
	if len(fields) > 0 {
		b.WriteString("｜")
		b.WriteString(strings.Join(fields, "｜"))
	}
	b.WriteString("\n")
}

func formatWorld(b *strings.Builder, entry novel.WorldEntry) {
	b.WriteString("- ")
	b.WriteString(entry.Title)
	if entry.Category != "" {
		fmt.Fprintf(b, "（%s）", entry.Category)
	}
	if entry.Content != "" {
		b.WriteString("：")
		b.WriteString(entry.Content)
	}
	if len(entry.Aliases) > 0 {
		fmt.Fprintf(b, "（又称：%s）", strings.Join(entry.Aliases, "、"))
	}
	b.WriteString("\n")
}

func selectCharacters(knowledge novel.Knowledge, recent string, startIndex int) []novel.Character {
	type scored struct {
		character novel.Character
		score     float64
	}
	compactRecent := compact(recent)
	compactOverview := compact(knowledge.Overview)
	candidates := make([]scored, 0, len(knowledge.Characters))
	for _, character := range knowledge.Characters {
		score := character.Confidence
		if mentionsAny(compactRecent, character.Name, character.Aliases) {
			score += 4
		}
		if mentionsAny(compactOverview, character.Name, character.Aliases) {
			score += 2
		}
		if startIndex > 0 && character.LastSeen > 0 && character.LastSeen >= startIndex-2 {
			score += 2
		}
		if startIndex > 0 && character.LastSeen >= startIndex {
			score += 1
		}
		if character.State != "" {
			score += 0.5
		}
		if score >= 1 {
			candidates = append(candidates, scored{character: character, score: score})
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].score == candidates[j].score {
			if candidates[i].character.LastSeen == candidates[j].character.LastSeen {
				return candidates[i].character.Name < candidates[j].character.Name
			}
			return candidates[i].character.LastSeen > candidates[j].character.LastSeen
		}
		return candidates[i].score > candidates[j].score
	})
	if len(candidates) > 12 {
		candidates = candidates[:12]
	}
	result := make([]novel.Character, len(candidates))
	for i, candidate := range candidates {
		result[i] = candidate.character
	}
	return result
}

func selectWorld(knowledge novel.Knowledge, recent string, startIndex int) []novel.WorldEntry {
	type scored struct {
		entry novel.WorldEntry
		score float64
	}
	compactRecent := compact(recent)
	candidates := make([]scored, 0, len(knowledge.World))
	for _, entry := range knowledge.World {
		score := entry.Confidence
		if mentionsAny(compactRecent, entry.Title, entry.Aliases) {
			score += 3
		}
		if startIndex > 0 && entry.LastSeen > 0 && entry.LastSeen >= startIndex-2 {
			score += 2
		}
		if score >= 1.2 {
			candidates = append(candidates, scored{entry: entry, score: score})
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].score == candidates[j].score {
			return candidates[i].entry.LastSeen > candidates[j].entry.LastSeen
		}
		return candidates[i].score > candidates[j].score
	})
	if len(candidates) > 10 {
		candidates = candidates[:10]
	}
	result := make([]novel.WorldEntry, len(candidates))
	for i, candidate := range candidates {
		result[i] = candidate.entry
	}
	return result
}

func selectSummaries(summaries []novel.ChapterSummary, chapters []novel.Chapter, startIndex int) []novel.ChapterSummary {
	existing := make(map[string]struct{}, len(chapters))
	existingIndexes := make(map[int]struct{}, len(chapters))
	for _, chapter := range chapters {
		existing[chapter.ID] = struct{}{}
		existingIndexes[chapter.Index] = struct{}{}
	}
	filtered := make([]novel.ChapterSummary, 0, len(summaries))
	for _, summary := range summaries {
		if summary.Index >= startIndex {
			continue
		}
		if summary.ChapterID != "" {
			if _, ok := existing[summary.ChapterID]; !ok {
				continue
			}
		} else if _, ok := existingIndexes[summary.Index]; !ok {
			continue
		}
		filtered = append(filtered, summary)
	}
	if len(filtered) > 3 {
		filtered = filtered[len(filtered)-3:]
	}
	return filtered
}

func sortedChapters(chapters []novel.Chapter) []novel.Chapter {
	result := append([]novel.Chapter(nil), chapters...)
	sort.SliceStable(result, func(i, j int) bool { return result[i].Index < result[j].Index })
	return result
}

func manuscriptTail(chapters []novel.Chapter, limit int) (string, bool, int) {
	firstIndex := 0
	var b strings.Builder
	for _, chapter := range chapters {
		content := strings.TrimSpace(chapter.Content)
		if content == "" {
			continue
		}
		if firstIndex == 0 {
			firstIndex = chapter.Index
		}
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(content)
	}
	full := b.String()
	if utf8.RuneCountInString(full) <= limit {
		return full, false, firstIndex
	}

	startIndex := firstIndex
	remaining := limit
	for i := len(chapters) - 1; i >= 0; i-- {
		content := strings.TrimSpace(chapters[i].Content)
		if content == "" {
			continue
		}
		length := utf8.RuneCountInString(content) + 2
		if length > remaining {
			startIndex = chapters[i].Index
			break
		}
		remaining -= length
		startIndex = chapters[i].Index
	}

	tail, truncated := tailContext(full, limit)
	return tail, truncated, startIndex
}

func tailContext(text string, limit int) (string, bool) {
	runes := []rune(text)
	if len(runes) <= limit {
		return text, false
	}
	tail := runes[len(runes)-limit:]
	cut := 0
	for i := 0; i < len(tail) && i < 300; i++ {
		if tail[i] == '\n' {
			cut = i + 1
			break
		}
	}
	tail = tail[cut:]
	return strings.TrimLeft(string(tail), "\n"), true
}

func tailRunes(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[len(runes)-limit:])
}

func compact(text string) string {
	return strings.ToLower(strings.Join(strings.Fields(text), ""))
}

func mentionsAny(compactText, name string, aliases []string) bool {
	if compactText == "" {
		return false
	}
	for _, value := range append([]string{name}, aliases...) {
		key := compact(value)
		if key == "" {
			continue
		}
		switch key {
		case "他", "她", "它", "我", "你", "他们", "她们":
			continue
		}
		if strings.Contains(compactText, key) {
			return true
		}
	}
	return false
}

func displayTitle(title string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return "未命名小说"
	}
	return title
}

func clipRunes(text string, limit int) string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) <= limit {
		return string(runes)
	}
	return string(runes[:limit]) + "…"
}

package agent

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"novelagent/internal/novel"
	"novelagent/internal/recall"
)

const defaultDirection = "无额外指定，顺着当前情节自然发展。保持已有冲突、人物动机和叙事张力，不要草率收尾。"
const defaultContextChars = novel.DefaultContextChars
const defaultTargetChars = 800

const systemPromptHead = `你是一位专业的中文小说续写引擎。你唯一的输出是续写正文。

规则：
1. 只输出续写正文，不要标题、序号、说明、总结、大纲或 Markdown。
2. 从上文最后一句直接接续，第一句要自然衔接，不重复已有句子或段落。
3. 以【最近正文】为最高事实来源：人物状态、称谓、伤势、地点、已发生事件，一律以最新正文为准；知识摘要与正文冲突时，服从正文。
`

// 仅在实际注入记忆时追加（零记忆输出与迭代 2 基线逐字节一致，I3-01）。
const systemPromptMemoryRule = `3.1 【已确认剧情记忆】用于维持长程一致性（伤势、目标、关系、未兑现伏笔）。它与【最近正文】冲突时一律以最近正文为准，不要试图调和或解释差异；【近期计划】只有走向建议的效力，不能改写已确认事实。
`

// 仅在注入 active 计划时追加（F23）。计划不得覆盖已确认事实，也不得据此提前收束。
const systemPromptPlanRule = `3.2 【计划对齐】只提供后续走向建议。不要为了让剧情赶上计划而跳过场景、压缩冲突或提前收束；计划与【最近正文】或【已确认剧情记忆】冲突时，一律以事实为准。
`

// 仅在注入 R4 历史正文召回时追加。召回是措辞/场景参考，不是当前事实。
const systemPromptRecallRule = `3.3 【前文相关片段】是从更早章节检索出的原文参考，只用于保持物件、场景与措辞一致；不要照抄，不要把它当作当前场景或已确认事实。它与【最近正文】【已确认剧情记忆】冲突时，一律以前两者为准。
`

const systemPromptTail = `4. 保持原文的人称、视角、时态、语气、句式节奏、段落密度和对白习惯。
5. 依据【当前人物状态】推动行动与对白，不要遗忘伤势、目标、关系变化；不要让已退场或已死亡的人物无故出现。
6. 不要重复揭示已经揭示过的身份、伏笔或设定，不要回顾前情。
7. 承接当前场景的悬念或冲突继续写，不要跳过场景直接收束，不要草率解决核心矛盾。
8. 非必要不新增重要人物、设定或道具；需要新增时，必须与现有世界观一致。
9. 不写作者旁白，不解释意图，不在结尾写总结。
10. 在自然的情节停顿处结束，篇幅接近目标字数。`

const systemPrompt = systemPromptHead + systemPromptTail

func systemPromptFor(withMemory, withPlan, withRecall bool) string {
	if !withMemory && !withPlan && !withRecall {
		return systemPrompt
	}
	var b strings.Builder
	b.WriteString(systemPromptHead)
	if withMemory {
		b.WriteString(systemPromptMemoryRule)
	}
	if withPlan {
		b.WriteString(systemPromptPlanRule)
	}
	if withRecall {
		b.WriteString(systemPromptRecallRule)
	}
	b.WriteString(systemPromptTail)
	return b.String()
}

type Input struct {
	Title        string
	Knowledge    novel.Knowledge
	Chapters     []novel.Chapter
	Direction    string
	TargetChars  int
	ContextChars int
	// Memory 为 store 检索并裁剪后的记忆上下文；nil/空不改变既有输出。
	Memory *novel.ContinuationMemory
	// Plans 为 active 计划（global/volume/near_term）；nil/空不注入计划段。
	Plans []novel.StoryPlan
	// UseRecall 为 nil 时按章节数自动判定（>=20 章启用 R4）；显式值用于 A/B。
	UseRecall *bool
}

type Prompt struct {
	System       string
	User         string
	ContextChars int
	Truncated    bool
	Characters   int
	WorldEntries int
	Summaries    int
	// MemorySection 为本次实际注入的记忆段（未注入时为空，预览接口复用）。
	MemorySection string
	// PlanSection 为本次实际注入的计划段（未注入时为空）。
	PlanSection string
	// RecallSection 为本次实际注入的历史正文召回段（未注入时为空）。
	RecallSection string
	// RecallBudget 为召回段硬上限（rune）；未启用为 0。
	RecallBudget int
	// RecallChapters 为实际注入的召回章节号（按分数降序）。
	RecallChapters []int
	// TotalChars 为 system + user 的 rune 总数（I3-7/R2 全量计量）。
	TotalChars int
	// Sections 为八个内容段的 rune 数（含段标题与前后换行）。
	Sections map[string]int
	// OverBudget 表示按冻结顺位收缩到底后仍超过 ContextChars；不阻断续写。
	OverBudget bool
}

const (
	SectionOverview   = "overview"
	SectionCharacters = "characters"
	SectionWorld      = "world"
	SectionMemories   = "memories"
	SectionRecall     = "recall"
	SectionPlans      = "plans"
	SectionSummaries  = "summaries"
	SectionManuscript = "manuscript"
)

type scoredCharacter struct {
	character novel.Character
	score     float64
}

type scoredWorld struct {
	entry novel.WorldEntry
	score float64
}

type promptLayout struct {
	user          string
	sections      map[string]int
	recent        string
	truncated     bool
	memorySection string
	recallSection string
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
	recallBudget := recallBudgetFor(input.UseRecall, len(chapters), contextChars)
	memoryBudget := novel.MemoryBudgetChars(contextChars)
	manuscriptWindow := contextChars - memoryBudget - recallBudget

	recent, _, startIndex := manuscriptTail(chapters, manuscriptWindow)
	recallHits := searchRecall(chapters, startIndex, recent, direction, recallBudget)
	if len(recallHits) == 0 && recallBudget > 0 {
		// 没有命中就不占用预算，正文窗口恢复为未减召回额度。
		recallBudget = 0
		manuscriptWindow = contextChars - memoryBudget
		recent, _, startIndex = manuscriptTail(chapters, manuscriptWindow)
	}
	recallSection, recallHits := renderRecallSection(recallHits, recallBudget)

	recentWindow := tailRunes(recent, min(manuscriptWindow, 8000))
	characters := rankCharacters(input.Knowledge, recentWindow, startIndex)
	world := rankWorld(input.Knowledge, recentWindow, startIndex)
	summaries := selectSummaries(input.Knowledge.Summaries, chapters, startIndex)
	planSection := novel.RenderPlanSection(input.Plans)
	memoryRecords := append([]novel.MemoryRecord(nil), recordsOf(input.Memory)...)
	manuscriptLimit := manuscriptWindow

	render := func() promptLayout {
		recent, truncated, _ := manuscriptTail(chapters, manuscriptLimit)
		return assembleLayout(
			displayTitle(input.Title),
			input.Knowledge,
			characters,
			world,
			summaries,
			memorySectionOf(input.Memory, memoryRecords),
			recallSection,
			planSection,
			recent,
			direction,
			targetChars,
			truncated,
		)
	}
	layout := render()

	if utf8.RuneCountInString(layout.user) > contextChars {
		// 冻结收缩顺位（iter3-详设 §4.1）：知识字段 → 知识条目 → 摘要 → 召回 → 记忆 P1 → 正文尾窗。
		for level := 1; level <= 4 && !layoutFits(layout, contextChars); level++ {
			changed := false
			for i := range characters {
				if trimCharacterField(&characters[i].character, level) {
					changed = true
				}
			}
			if changed {
				layout = render()
			}
		}
		changed := false
		for i := range world {
			if len(world[i].entry.Aliases) > 0 {
				world[i].entry.Aliases = nil
				changed = true
			}
		}
		if changed {
			layout = render()
		}
		for !layoutFits(layout, contextChars) && len(characters) > 0 {
			characters = characters[:len(characters)-1]
			layout = render()
		}
		for !layoutFits(layout, contextChars) && len(world) > 0 {
			world = world[:len(world)-1]
			layout = render()
		}
		for !layoutFits(layout, contextChars) && len(summaries) > 0 {
			summaries = summaries[1:]
			layout = render()
		}
		for !layoutFits(layout, contextChars) && len(recallHits) > 0 {
			recallHits = recallHits[:len(recallHits)-1]
			recallSection, recallHits = renderRecallSection(recallHits, recallBudget)
			layout = render()
		}
		for !layoutFits(layout, contextChars) && dropMemoryRecord(&memoryRecords, novel.MemoryKindEvent) {
			layout = render()
		}
		for !layoutFits(layout, contextChars) && dropMemoryRecord(&memoryRecords, novel.MemoryKindThread) {
			layout = render()
		}
		minManuscript := contextChars / 2
		for attempt := 0; !layoutFits(layout, contextChars) && attempt < 32; attempt++ {
			current := utf8.RuneCountInString(layout.recent)
			if current <= minManuscript {
				break
			}
			excess := utf8.RuneCountInString(layout.user) - contextChars
			target := current - excess
			if target < minManuscript {
				target = minManuscript
			}
			if target >= current {
				target = current - 1
			}
			if target <= 0 {
				break
			}
			manuscriptLimit = target
			next := render()
			if utf8.RuneCountInString(next.recent) >= current {
				break
			}
			layout = next
		}
		// 行界裁剪最多会吃掉一行的长度，可能让尾窗略低于硬下限；补回缺口。
		for attempt := 0; attempt < 8; attempt++ {
			shortfall := minManuscript - utf8.RuneCountInString(layout.recent)
			if shortfall <= 0 || !layout.truncated {
				break
			}
			manuscriptLimit += shortfall
			layout = render()
		}
	}

	system := systemPromptFor(layout.memorySection != "", planSection != "", layout.recallSection != "")
	return Prompt{
		System:         system,
		User:           layout.user,
		ContextChars:   utf8.RuneCountInString(layout.recent),
		Truncated:      layout.truncated,
		Characters:     len(characters),
		WorldEntries:   len(world),
		Summaries:      len(summaries),
		MemorySection:  layout.memorySection,
		PlanSection:    planSection,
		RecallSection:  layout.recallSection,
		RecallBudget:   recallBudget,
		RecallChapters: recallChaptersOf(recallHits),
		TotalChars:     utf8.RuneCountInString(system) + utf8.RuneCountInString(layout.user),
		Sections:       layout.sections,
		OverBudget:     !layoutFits(layout, contextChars),
	}
}

func layoutFits(layout promptLayout, contextChars int) bool {
	return utf8.RuneCountInString(layout.user) <= contextChars
}

func recordsOf(m *novel.ContinuationMemory) []novel.MemoryRecord {
	if m == nil {
		return nil
	}
	return m.Records
}

func memorySectionOf(m *novel.ContinuationMemory, records []novel.MemoryRecord) string {
	if m == nil {
		return ""
	}
	clone := *m
	clone.Records = records
	return novel.RenderMemorySection(&clone)
}

func recallBudgetFor(useRecall *bool, chapterCount, contextChars int) int {
	enabled := novel.RecallEnabled(chapterCount, contextChars)
	if useRecall != nil {
		enabled = *useRecall
	}
	if !enabled {
		return 0
	}
	return novel.RecallBudgetChars(contextChars)
}

// searchRecall 只在正文尾窗之外的旧章节中检索；查询由最近正文末尾与方向组成，
// 方向作为 Intent 加权，保证方向里的关键名词优先于最近正文的表层词。
func searchRecall(chapters []novel.Chapter, startIndex int, recent, direction string, budget int) []recall.Hit {
	if budget <= 0 || startIndex <= 0 {
		return nil
	}
	docs := make([]recall.Document, 0, len(chapters))
	for _, chapter := range chapters {
		if chapter.Index >= startIndex {
			break
		}
		if strings.TrimSpace(chapter.Content) == "" {
			continue
		}
		docs = append(docs, recall.Document{
			ChapterID: chapter.ID,
			Index:     chapter.Index,
			Title:     chapter.Title,
			Content:   chapter.Content,
		})
	}
	if len(docs) == 0 {
		return nil
	}
	query := strings.TrimSpace(tailRunes(recent, 1200))
	return recall.Search(docs, query, recall.Options{SnippetRunes: budget, Intent: strings.TrimSpace(direction)})
}

// renderRecallSection 按剩余预算均分 top hits；放不下的命中不注入。
func renderRecallSection(hits []recall.Hit, budget int) (string, []recall.Hit) {
	if len(hits) == 0 || budget <= 0 {
		return "", nil
	}
	const header = "\n【前文相关片段】\n"
	remaining := budget - utf8.RuneCountInString(header)
	if remaining <= 0 {
		return "", nil
	}
	var b strings.Builder
	b.WriteString(header)
	used := make([]recall.Hit, 0, len(hits))
	for i := range hits {
		remainingHits := len(hits) - i
		hit := hits[i]
		prefix := fmt.Sprintf("第%d章", hit.ChapterIndex)
		label := prefix
		title := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(hit.ChapterTitle), prefix))
		if title != "" {
			label += " " + clipRunes(title, 40)
		}
		label += "：\n"
		labelChars := utf8.RuneCountInString(label)
		per := remaining / remainingHits
		if per <= labelChars+40 {
			break
		}
		snippetLimit := per - labelChars - 1
		snippet := clipRunes(hit.Snippet, snippetLimit)
		if strings.TrimSpace(snippet) == "" {
			continue
		}
		b.WriteString(label)
		b.WriteString(snippet)
		b.WriteString("\n")
		remaining -= labelChars + utf8.RuneCountInString(snippet) + 1
		used = append(used, hit)
	}
	if len(used) == 0 {
		return "", nil
	}
	return b.String(), used
}

func recallChaptersOf(hits []recall.Hit) []int {
	if len(hits) == 0 {
		return nil
	}
	chapters := make([]int, 0, len(hits))
	for _, hit := range hits {
		chapters = append(chapters, hit.ChapterIndex)
	}
	return chapters
}

func trimCharacterField(character *novel.Character, level int) bool {
	switch level {
	case 1:
		if character.Notes != "" {
			character.Notes = ""
			return true
		}
	case 2:
		if character.Appearance != "" {
			character.Appearance = ""
			return true
		}
	case 3:
		if character.Personality != "" {
			character.Personality = ""
			return true
		}
	case 4:
		if character.Relationships != "" {
			character.Relationships = ""
			return true
		}
	}
	return false
}

// dropMemoryRecord 删除该类别最后一条（近因优先排序下即最不近因的一条）。
func dropMemoryRecord(records *[]novel.MemoryRecord, kind string) bool {
	list := *records
	for i := len(list) - 1; i >= 0; i-- {
		if list[i].Kind == kind {
			*records = append(append([]novel.MemoryRecord(nil), list[:i]...), list[i+1:]...)
			return true
		}
	}
	return false
}

func assembleLayout(
	title string,
	knowledge novel.Knowledge,
	characters []scoredCharacter,
	world []scoredWorld,
	summaries []novel.ChapterSummary,
	memorySection string,
	recallSection string,
	planSection string,
	recent string,
	direction string,
	targetChars int,
	truncated bool,
) promptLayout {
	sections := map[string]int{
		SectionOverview:   0,
		SectionCharacters: 0,
		SectionWorld:      0,
		SectionMemories:   0,
		SectionRecall:     0,
		SectionPlans:      0,
		SectionSummaries:  0,
		SectionManuscript: 0,
	}

	var b strings.Builder
	fmt.Fprintf(&b, "【作品】《%s》\n", title)

	if overview := strings.TrimSpace(knowledge.Overview); overview != "" {
		var s strings.Builder
		s.WriteString("\n【核心设定与当前进展】\n")
		s.WriteString(overview)
		s.WriteString("\n")
		sections[SectionOverview] = utf8.RuneCountInString(s.String())
		b.WriteString(s.String())
	}

	b.WriteString("\n【文风指纹】\n")
	if !formatStyle(&b, knowledge.Style) {
		b.WriteString("严格贴合下方正文的叙述视角、句式节奏、段落密度和对白习惯。\n")
	}

	if len(characters) > 0 {
		var s strings.Builder
		s.WriteString("\n【当前人物状态】\n")
		for _, candidate := range characters {
			formatCharacter(&s, candidate.character)
		}
		sections[SectionCharacters] = utf8.RuneCountInString(s.String())
		b.WriteString(s.String())
	}

	if len(world) > 0 {
		var s strings.Builder
		s.WriteString("\n【相关设定】\n")
		for _, candidate := range world {
			formatWorld(&s, candidate.entry)
		}
		sections[SectionWorld] = utf8.RuneCountInString(s.String())
		b.WriteString(s.String())
	}

	if memorySection != "" {
		sections[SectionMemories] = utf8.RuneCountInString(memorySection)
		b.WriteString(memorySection)
	}

	if recallSection != "" {
		sections[SectionRecall] = utf8.RuneCountInString(recallSection)
		b.WriteString(recallSection)
	}

	if planSection != "" {
		sections[SectionPlans] = utf8.RuneCountInString(planSection)
		b.WriteString(planSection)
	}

	if len(summaries) > 0 {
		var s strings.Builder
		s.WriteString("\n【前情提要】\n")
		for _, summary := range summaries {
			label := fmt.Sprintf("第%d章", summary.Index)
			if summary.Title != "" {
				label += " " + summary.Title
			}
			text := clipRunes(summary.Summary, 180)
			fmt.Fprintf(&s, "%s：%s\n", label, text)
		}
		sections[SectionSummaries] = utf8.RuneCountInString(s.String())
		b.WriteString(s.String())
	}

	var manuscript strings.Builder
	manuscript.WriteString("\n【最近正文（末尾即续写起点）】\n")
	manuscript.WriteString(recent)
	manuscript.WriteString("\n")
	sections[SectionManuscript] = utf8.RuneCountInString(manuscript.String())
	b.WriteString(manuscript.String())

	b.WriteString("\n【本次续写】\n")
	fmt.Fprintf(&b, "剧情方向：%s\n", direction)
	fmt.Fprintf(&b, "目标篇幅：约 %d 字。\n", targetChars)
	b.WriteString("请从最近正文末尾直接续写，不要复述、概括或重写已有内容。")

	return promptLayout{
		user:          b.String(),
		sections:      sections,
		recent:        recent,
		truncated:     truncated,
		memorySection: memorySection,
		recallSection: recallSection,
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

func rankCharacters(knowledge novel.Knowledge, recent string, startIndex int) []scoredCharacter {
	compactRecent := novel.CompactText(recent)
	compactOverview := novel.CompactText(knowledge.Overview)
	candidates := make([]scoredCharacter, 0, len(knowledge.Characters))
	for _, character := range knowledge.Characters {
		score, _ := novel.ScoreCharacterMention(character, compactRecent, compactOverview, startIndex, nil)
		if score >= 1 {
			candidates = append(candidates, scoredCharacter{character: character, score: score})
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
	return candidates
}

func rankWorld(knowledge novel.Knowledge, recent string, startIndex int) []scoredWorld {
	compactRecent := novel.CompactText(recent)
	candidates := make([]scoredWorld, 0, len(knowledge.World))
	for _, entry := range knowledge.World {
		score, _ := novel.ScoreWorldMention(entry, compactRecent, startIndex, nil)
		if score >= 1.2 {
			candidates = append(candidates, scoredWorld{entry: entry, score: score})
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
	return candidates
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

// ContinuationWindow 返回检索与组装共用的最近正文窗口与起始章节号。
// 与 Build 内部同源：正文窗口 = 总预算 − 记忆预算 − R4 召回预算（启用时）。
func ContinuationWindow(chapters []novel.Chapter, contextChars int) (string, int) {
	return ContinuationWindowFor(chapters, contextChars, nil)
}

// ContinuationWindowFor 与 ContinuationWindow 相同，但可显式覆盖 R4 自动开关（A/B 用）。
func ContinuationWindowFor(chapters []novel.Chapter, contextChars int, useRecall *bool) (string, int) {
	if contextChars <= 0 {
		contextChars = novel.DefaultContextChars
	}
	recallBudget := recallBudgetFor(useRecall, len(chapters), contextChars)
	manuscriptWindow := novel.ManuscriptWindowChars(contextChars) - recallBudget
	recent, _, startIndex := manuscriptTail(sortedChapters(chapters), manuscriptWindow)
	return tailRunes(recent, min(manuscriptWindow, 8000)), startIndex
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
	if limit <= 0 {
		return ""
	}
	if limit == 1 {
		return "…"
	}
	return string(runes[:limit-1]) + "…"
}

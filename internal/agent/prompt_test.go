package agent

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"novelagent/internal/novel"
)

func TestBuildAssemblesHiddenKnowledgeAndRecentChapters(t *testing.T) {
	chapters := []novel.Chapter{
		{ID: "ch_1", Index: 1, Title: "初始", Content: "旧事已经过去。"},
		{ID: "ch_2", Index: 2, Title: "重瞳", Content: "林砚推门而入，雨声更密。"},
	}
	knowledge := novel.Knowledge{
		Overview: "灵气复苏后的县城，林砚在追查旧案。",
		Style: novel.StyleProfile{
			Perspective: "第三人称限知",
			Tone:        "冷峻克制",
			Sentence:    "短句为主",
			Dialogue:    "对白简短",
			Pacing:      "缓慢推进",
			Notes:       "多用雨夜意象",
			Samples:     []string{"雨声更密。", "他睁开眼。", "第三句不应出现。"},
		},
		Characters: []novel.Character{
			{
				Name:          "林砚",
				Aliases:       []string{"砚哥"},
				Role:          "县衙捕快",
				Personality:   "寡言",
				Appearance:    "眉骨有旧疤",
				Relationships: "与沈青禾是旧识",
				State:         "左肩受伤",
				Notes:         "不饮酒",
				Confidence:    0.9,
				LastSeen:      2,
			},
			{Name: "沈青禾", Role: "医女", Confidence: 0.4},
		},
		World: []novel.WorldEntry{
			{Title: "灵气复苏", Aliases: []string{"灵气潮"}, Category: "规则", Content: "三年前开始。", Confidence: 0.8, LastSeen: 2},
			{Title: "轮回镜", Category: "物品", Content: "上古神器。", Confidence: 0.4},
		},
	}

	prompt := Build(Input{
		Title:        "雨夜",
		Knowledge:    knowledge,
		Chapters:     chapters,
		Direction:    "让门外的人开口。",
		TargetChars:  500,
		ContextChars: 12000,
	})

	wants := []string{
		"《雨夜》",
		"【核心设定与当前进展】",
		"灵气复苏后的县城，林砚在追查旧案。",
		"【文风指纹】",
		"视角：第三人称限知",
		"语气：冷峻克制",
		"句式：短句为主",
		"对白：对白简短",
		"节奏：缓慢推进",
		"补充：多用雨夜意象",
		"雨声更密。",
		"他睁开眼。",
		"【当前人物状态】",
		"林砚（别名：砚哥）",
		"身份：县衙捕快",
		"当前：左肩受伤",
		"性格：寡言",
		"关系：与沈青禾是旧识",
		"外貌：眉骨有旧疤",
		"补充：不饮酒",
		"【相关设定】",
		"灵气复苏（规则）：三年前开始。",
		"（又称：灵气潮）",
		"【最近正文（末尾即续写起点）】",
		"旧事已经过去。",
		"林砚推门而入，雨声更密。",
		"剧情方向：让门外的人开口。",
		"目标篇幅：约 500 字。",
	}
	for _, want := range wants {
		if !strings.Contains(prompt.User, want) {
			t.Errorf("user prompt does not contain %q\n%s", want, prompt.User)
		}
	}
	if strings.Contains(prompt.User, "- 沈青禾") || strings.Contains(prompt.User, "- 轮回镜") {
		t.Errorf("low-confidence, unrelated entries should be skipped\n%s", prompt.User)
	}
	if strings.Contains(prompt.User, "第三句不应出现。") {
		t.Errorf("style samples were not capped\n%s", prompt.User)
	}
	if prompt.Characters != 1 || prompt.WorldEntries != 1 || prompt.Summaries != 0 {
		t.Errorf("selection counts = %d/%d/%d", prompt.Characters, prompt.WorldEntries, prompt.Summaries)
	}
	if prompt.Truncated {
		t.Error("Truncated = true, want false")
	}
	wantContext := utf8.RuneCountInString("旧事已经过去。\n\n林砚推门而入，雨声更密。")
	if prompt.ContextChars != wantContext {
		t.Errorf("ContextChars = %d, want %d", prompt.ContextChars, wantContext)
	}
	for _, rule := range []string{"只输出续写正文", "最高事实来源", "不要重复揭示"} {
		if !strings.Contains(prompt.System, rule) {
			t.Errorf("system prompt is missing %q", rule)
		}
	}
	if !strings.Contains(prompt.User, "请从最近正文末尾直接续写") {
		t.Error("user prompt is missing the continuation instruction")
	}
}

func TestBuildUsesDefaultsWithoutKnowledge(t *testing.T) {
	prompt := Build(Input{
		Chapters: []novel.Chapter{{ID: "ch_1", Index: 1, Content: "风停了。"}},
	})

	if !strings.Contains(prompt.User, defaultDirection) {
		t.Errorf("user prompt does not contain the default direction\n%s", prompt.User)
	}
	if !strings.Contains(prompt.User, "目标篇幅：约 800 字。") {
		t.Errorf("default target length is missing\n%s", prompt.User)
	}
	if !strings.Contains(prompt.User, "严格贴合下方正文的叙述视角") {
		t.Errorf("default style guidance is missing\n%s", prompt.User)
	}
	for _, section := range []string{"【核心设定与当前进展】", "【当前人物状态】", "【相关设定】", "【前情提要】"} {
		if strings.Contains(prompt.User, section) {
			t.Errorf("empty section %q should be omitted\n%s", section, prompt.User)
		}
	}
	if prompt.Characters != 0 || prompt.WorldEntries != 0 || prompt.Summaries != 0 {
		t.Errorf("selection counts = %d/%d/%d", prompt.Characters, prompt.WorldEntries, prompt.Summaries)
	}
	if prompt.ContextChars != utf8.RuneCountInString("风停了。") {
		t.Errorf("ContextChars = %d", prompt.ContextChars)
	}
}

func TestBuildUsesSummariesOnlyForChaptersOutsideRecentWindow(t *testing.T) {
	chapters := []novel.Chapter{
		{ID: "ch_1", Index: 1, Title: "一", Content: strings.Repeat("第一章旧事。", 250)},
		{ID: "ch_2", Index: 2, Title: "二", Content: strings.Repeat("第二章旧事。", 250)},
		{ID: "ch_3", Index: 3, Title: "三", Content: strings.Repeat("第三章旧事。", 250)},
	}
	knowledge := novel.Knowledge{
		Summaries: []novel.ChapterSummary{
			{ChapterID: "ch_1", Index: 1, Summary: "第一章摘要。"},
			{ChapterID: "ch_2", Index: 2, Summary: "第二章摘要。"},
			{ChapterID: "ch_3", Index: 3, Summary: "第三章摘要。"},
		},
	}

	prompt := Build(Input{Chapters: chapters, Knowledge: knowledge, ContextChars: 2000})

	if !prompt.Truncated {
		t.Fatal("Truncated = false, want true")
	}
	if prompt.ContextChars > 2000 || prompt.ContextChars < 1000 {
		t.Errorf("ContextChars = %d, want within the allocated manuscript window", prompt.ContextChars)
	}
	if strings.Contains(prompt.User, "第一章旧事。") {
		t.Error("recent context still contains the earliest chapter")
	}
	if !strings.Contains(prompt.User, "第三章旧事。") {
		t.Error("recent context does not contain the latest chapter")
	}
	if !strings.Contains(prompt.User, "第一章摘要。") || !strings.Contains(prompt.User, "第二章摘要。") {
		t.Errorf("summaries for earlier chapters are missing\n%s", prompt.User)
	}
	if strings.Contains(prompt.User, "第三章摘要。") {
		t.Errorf("summary for the chapter already in the recent window should be skipped\n%s", prompt.User)
	}
	if prompt.Summaries != 2 {
		t.Errorf("Summaries = %d, want 2", prompt.Summaries)
	}
}

func TestBuildTruncatesLongContextAtLineBoundary(t *testing.T) {
	var b strings.Builder
	b.WriteString("很久以前的旧事，不应出现在节选里。\n")
	for i := 0; i < 1000; i++ {
		b.WriteString("这是一段用来填充正文长度的内容。\n")
	}
	prompt := Build(Input{
		Chapters:     []novel.Chapter{{ID: "ch_1", Index: 1, Content: b.String()}},
		ContextChars: 400,
	})

	if !prompt.Truncated {
		t.Fatal("Truncated = false, want true")
	}
	if prompt.ContextChars > 400 || prompt.ContextChars < 200 {
		t.Errorf("ContextChars = %d, want >= ctx/2 and <= ctx", prompt.ContextChars)
	}
	if got := utf8.RuneCountInString(prompt.User); got > 400 {
		t.Errorf("user prompt = %d runes, want <= 400 (R2 full budget)", got)
	}
	if strings.Contains(prompt.User, "很久以前的旧事") {
		t.Error("truncated context still contains the beginning of the chapter")
	}
	_, after, found := strings.Cut(prompt.User, "【最近正文（末尾即续写起点）】\n")
	if !found {
		t.Fatal("recent manuscript marker is missing")
	}
	excerpt, _, found := strings.Cut(after, "\n\n【本次续写】")
	if !found {
		t.Fatal("continuation marker is missing")
	}
	if !strings.HasPrefix(excerpt, "这是一段用来填充正文长度的内容。") {
		t.Errorf("excerpt does not start at a line boundary: %q", excerpt[:min(40, len(excerpt))])
	}
	if want := utf8.RuneCountInString(excerpt); prompt.ContextChars != want {
		t.Errorf("ContextChars = %d, excerpt runes = %d", prompt.ContextChars, want)
	}
}

// TestBuildFullBudgetInvariants 覆盖 I3-7/R2 四档守卫（iter3-详设 §4.1/§13.2）。
func TestBuildFullBudgetInvariants(t *testing.T) {
	const (
		title       = "长篇预算"
		direction   = "推进主线"
		targetChars = 800
	)

	chapters := make([]novel.Chapter, 0, 31)
	for i := 1; i <= 30; i++ {
		chapters = append(chapters, novel.Chapter{
			ID:      fmt.Sprintf("ch_%02d", i),
			Index:   i,
			Title:   fmt.Sprintf("第%d章", i),
			Content: strings.Repeat(fmt.Sprintf("第%d章铺垫。", i), 120),
		})
	}
	chapters = append(chapters, novel.Chapter{
		ID:      "ch_31",
		Index:   31,
		Title:   "终章",
		Content: strings.Repeat("雨夜追查旧案的正文。\n", 1200),
	})

	characters := make([]novel.Character, 0, 50)
	names := make([]string, 0, 50)
	for i := 1; i <= 50; i++ {
		name := fmt.Sprintf("角色%02d", i)
		names = append(names, name)
		characters = append(characters, novel.Character{
			Name:          name,
			Aliases:       []string{fmt.Sprintf("别名%02d", i)},
			Role:          "身份",
			State:         "当前状态",
			Personality:   strings.Repeat("性格", 20),
			Appearance:    strings.Repeat("外貌", 20),
			Relationships: strings.Repeat("关系", 20),
			Notes:         strings.Repeat("备注", 20),
			Confidence:    0.9,
			LastSeen:      31,
		})
	}
	world := make([]novel.WorldEntry, 0, 50)
	for i := 1; i <= 50; i++ {
		world = append(world, novel.WorldEntry{
			Title:      fmt.Sprintf("设定%02d", i),
			Aliases:    []string{fmt.Sprintf("旧称%02d", i)},
			Category:   "规则",
			Content:    strings.Repeat("设定内容", 20),
			Confidence: 0.9,
			LastSeen:   31,
		})
	}
	summaries := make([]novel.ChapterSummary, 0, 30)
	for i := 1; i <= 30; i++ {
		summaries = append(summaries, novel.ChapterSummary{
			ChapterID: fmt.Sprintf("ch_%02d", i),
			Index:     i,
			Summary:   strings.Repeat("摘要", 50),
		})
	}

	kinds := []string{
		novel.MemoryKindWorldState, novel.MemoryKindWorldState,
		novel.MemoryKindState, novel.MemoryKindState, novel.MemoryKindState,
		novel.MemoryKindCognition, novel.MemoryKindCognition,
		novel.MemoryKindThread, novel.MemoryKindThread, novel.MemoryKindThread,
		novel.MemoryKindEvent, novel.MemoryKindEvent, novel.MemoryKindEvent,
	}
	records := make([]novel.MemoryRecord, 0, len(kinds)*2)
	for round := 0; round < 2; round++ {
		for i, kind := range kinds {
			priority := novel.MemoryPriorityP0
			if kind == novel.MemoryKindThread || kind == novel.MemoryKindEvent {
				priority = novel.MemoryPriorityP1
			}
			records = append(records, novel.MemoryRecord{
				ID:       fmt.Sprintf("rec_%d_%d", round, i),
				Kind:     kind,
				Subject:  "角色01",
				Text:     strings.Repeat("记忆内容", 12),
				Order:    i,
				Priority: priority,
			})
		}
	}

	for _, contextChars := range []int{1600, 4000, 12000, 30000} {
		t.Run(fmt.Sprintf("ctx_%d", contextChars), func(t *testing.T) {
			prompt := Build(Input{
				Title: title,
				Knowledge: novel.Knowledge{
					Overview:   strings.Join(names, "，"),
					Characters: characters,
					World:      world,
					Summaries:  summaries,
				},
				Chapters:     chapters,
				Direction:    direction,
				TargetChars:  targetChars,
				ContextChars: contextChars,
				Memory: &novel.ContinuationMemory{
					BudgetChars: novel.MemoryBudgetChars(contextChars),
					Records:     records,
				},
			})

			if prompt.OverBudget {
				t.Fatalf("OverBudget = true, want false")
			}
			userRunes := utf8.RuneCountInString(prompt.User)
			if userRunes > contextChars {
				t.Fatalf("user prompt = %d runes, want <= %d", userRunes, contextChars)
			}
			if prompt.TotalChars != utf8.RuneCountInString(prompt.System)+userRunes {
				t.Errorf("TotalChars = %d, want system+user", prompt.TotalChars)
			}
			sectionSum := 0
			for _, key := range []string{
				SectionOverview, SectionCharacters, SectionWorld, SectionMemories,
				SectionPlans, SectionSummaries, SectionManuscript,
			} {
				sectionSum += prompt.Sections[key]
			}
			if sectionSum > userRunes {
				t.Errorf("sum(Sections) = %d > user runes %d", sectionSum, userRunes)
			}
			if prompt.ContextChars != prompt.Sections[SectionManuscript]-utf8.RuneCountInString("\n【最近正文（末尾即续写起点）】\n")-1 {
				t.Errorf("ContextChars = %d does not match manuscript section %d",
					prompt.ContextChars, prompt.Sections[SectionManuscript])
			}
			if prompt.ContextChars < contextChars/2 {
				t.Errorf("manuscript chars = %d, want >= ctx/2", prompt.ContextChars)
			}
			if contextChars == 1600 && prompt.MemorySection != "" {
				t.Errorf("memory section should degrade to empty at ctx=1600")
			}
			if prompt.System == "" {
				t.Error("system prompt is empty")
			}
			// 固定文本（标题/文风兜底/续写指令）与 system 不参与裁剪，四档必须一致。
			fixed := userRunes - sectionSum
			wantFixed := utf8.RuneCountInString(fmt.Sprintf("【作品】《%s》\n", title)) +
				utf8.RuneCountInString("\n【文风指纹】\n严格贴合下方正文的叙述视角、句式节奏、段落密度和对白习惯。\n") +
				utf8.RuneCountInString(fmt.Sprintf("\n【本次续写】\n剧情方向：%s\n目标篇幅：约 %d 字。\n请从最近正文末尾直接续写，不要复述、概括或重写已有内容。", direction, targetChars))
			if fixed != wantFixed {
				t.Errorf("fixed prompt chars = %d, want %d", fixed, wantFixed)
			}
			// I3-17 精确口径：sum(Sections) + 标题/固定文本 + system = TotalChars。
			if sectionSum+wantFixed+utf8.RuneCountInString(prompt.System) != prompt.TotalChars {
				t.Errorf("sum(Sections)+fixed+system = %d, TotalChars = %d",
					sectionSum+wantFixed+utf8.RuneCountInString(prompt.System), prompt.TotalChars)
			}
		})
	}

	// 收缩到底仍放不下时（正文下限 ctx/2 + 固定文本 > ctx），OverBudget 才置真。
	t.Run("over_budget", func(t *testing.T) {
		const contextChars = 200
		prompt := Build(Input{
			Title:        title,
			Knowledge:    novel.Knowledge{Overview: strings.Repeat("设定", 100), Characters: characters, World: world},
			Chapters:     chapters,
			Direction:    direction,
			TargetChars:  targetChars,
			ContextChars: contextChars,
		})
		if !prompt.OverBudget {
			t.Error("OverBudget = false, want true when fixed text + ctx/2 manuscript exceeds the budget")
		}
		if utf8.RuneCountInString(prompt.User) <= contextChars {
			t.Errorf("user prompt = %d runes, want > %d for the OverBudget fixture",
				utf8.RuneCountInString(prompt.User), contextChars)
		}
		if prompt.ContextChars < contextChars/2 {
			t.Errorf("manuscript chars = %d, want >= ctx/2", prompt.ContextChars)
		}
	})
}

func TestBuildFixedOverheadStableAcrossContexts(t *testing.T) {
	chapter := novel.Chapter{ID: "ch_1", Index: 1, Content: strings.Repeat("正文。", 2000)}
	fixed := map[int]struct{}{}
	for _, contextChars := range []int{1600, 4000, 12000, 30000} {
		prompt := Build(Input{
			Title:        "固定开销",
			Chapters:     []novel.Chapter{chapter},
			Direction:    "继续",
			TargetChars:  600,
			ContextChars: contextChars,
		})
		sectionSum := 0
		for _, count := range prompt.Sections {
			sectionSum += count
		}
		fixed[utf8.RuneCountInString(prompt.User)-sectionSum] = struct{}{}
		if utf8.RuneCountInString(prompt.User) > contextChars {
			t.Errorf("ctx %d: user prompt over budget", contextChars)
		}
	}
	if len(fixed) != 1 {
		t.Errorf("fixed overhead differs across contexts: %v", fixed)
	}
}

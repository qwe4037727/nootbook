package agent

import (
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
		{ID: "ch_1", Index: 1, Title: "一", Content: strings.Repeat("第一章旧事。", 100)},
		{ID: "ch_2", Index: 2, Title: "二", Content: strings.Repeat("第二章旧事。", 100)},
		{ID: "ch_3", Index: 3, Title: "三", Content: strings.Repeat("第三章旧事。", 100)},
	}
	knowledge := novel.Knowledge{
		Summaries: []novel.ChapterSummary{
			{ChapterID: "ch_1", Index: 1, Summary: "第一章摘要。"},
			{ChapterID: "ch_2", Index: 2, Summary: "第二章摘要。"},
			{ChapterID: "ch_3", Index: 3, Summary: "第三章摘要。"},
		},
	}

	prompt := Build(Input{Chapters: chapters, Knowledge: knowledge, ContextChars: 400})

	if !prompt.Truncated {
		t.Fatal("Truncated = false, want true")
	}
	if prompt.ContextChars > 400 {
		t.Errorf("ContextChars = %d, want <= 400", prompt.ContextChars)
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
	if prompt.ContextChars > 400 || prompt.ContextChars < 350 {
		t.Errorf("ContextChars = %d, want a near-limit tail", prompt.ContextChars)
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

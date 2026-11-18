package agent

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"novelagent/internal/novel"
)

func boolRef(value bool) *bool { return &value }

func recallChapters(count int) []novel.Chapter {
	chapters := make([]novel.Chapter, 0, count)
	for i := 1; i <= count; i++ {
		content := strings.Repeat("雨夜的长街空无一人。", 150)
		if i == 3 {
			content += "\n林砚从旧宅梁下摸出半枚青玉符，符上刻着旧朝年号。"
		}
		chapters = append(chapters, novel.Chapter{
			ID:      fmt.Sprintf("ch_%d", i),
			Index:   i,
			Title:   fmt.Sprintf("第%d章 雨夜", i),
			Content: content,
		})
	}
	return chapters
}

func TestBuildRecallInjectsPlantedChapter(t *testing.T) {
	chapters := recallChapters(22)
	prompt := Build(Input{
		Title:        "召回测试",
		Chapters:     chapters,
		Direction:    "继续追查青玉符的来历。",
		TargetChars:  800,
		ContextChars: 8000,
	})
	if prompt.RecallSection == "" {
		t.Fatal("RecallSection empty, want injected recall")
	}
	if !strings.Contains(prompt.RecallSection, "青玉符") {
		t.Errorf("recall section missing planted fact: %q", prompt.RecallSection)
	}
	if !strings.Contains(prompt.RecallSection, "第3章") {
		t.Errorf("recall section missing chapter label: %q", prompt.RecallSection)
	}
	if len(prompt.RecallChapters) == 0 || prompt.RecallChapters[0] != 3 {
		t.Errorf("RecallChapters = %v, want first=3", prompt.RecallChapters)
	}
	if prompt.RecallBudget != 800 {
		t.Errorf("RecallBudget = %d, want 800", prompt.RecallBudget)
	}
	if prompt.Sections[SectionRecall] == 0 {
		t.Error("Sections[recall] = 0, want > 0")
	}
	if prompt.Sections[SectionRecall] > prompt.RecallBudget {
		t.Errorf("recall section = %d runes, budget %d", prompt.Sections[SectionRecall], prompt.RecallBudget)
	}
	if !strings.Contains(prompt.System, "【前文相关片段】") {
		t.Error("system prompt missing recall rule")
	}
	if utf8.RuneCountInString(prompt.User) > 8000 {
		t.Errorf("user prompt = %d runes, want <= 8000", utf8.RuneCountInString(prompt.User))
	}
	if prompt.ContextChars < 4000 {
		t.Errorf("manuscript chars = %d, want >= ctx/2", prompt.ContextChars)
	}
	if len(prompt.Sections) != 8 {
		t.Errorf("sections = %d, want 8", len(prompt.Sections))
	}
}

func TestBuildRecallDisabledBelowThreshold(t *testing.T) {
	chapters := recallChapters(19)
	prompt := Build(Input{
		Title:        "短篇",
		Chapters:     chapters,
		Direction:    "继续追查青玉符的来历。",
		ContextChars: 8000,
	})
	if prompt.RecallSection != "" || len(prompt.RecallChapters) != 0 || prompt.RecallBudget != 0 {
		t.Errorf("recall should be disabled below %d chapters: section=%q chapters=%v budget=%d",
			novel.RecallMinChapters, prompt.RecallSection, prompt.RecallChapters, prompt.RecallBudget)
	}
	if prompt.Sections[SectionRecall] != 0 {
		t.Errorf("Sections[recall] = %d, want 0", prompt.Sections[SectionRecall])
	}
	if prompt.System != systemPrompt {
		t.Error("system prompt changed with recall disabled")
	}
}

func TestBuildRecallExplicitOff(t *testing.T) {
	chapters := recallChapters(22)
	prompt := Build(Input{
		Title:        "显式关闭",
		Chapters:     chapters,
		Direction:    "继续追查青玉符的来历。",
		ContextChars: 8000,
		UseRecall:    boolRef(false),
	})
	if prompt.RecallSection != "" || prompt.RecallBudget != 0 {
		t.Errorf("explicit off still injected recall: %q budget=%d", prompt.RecallSection, prompt.RecallBudget)
	}
	if prompt.Sections[SectionRecall] != 0 {
		t.Errorf("Sections[recall] = %d, want 0", prompt.Sections[SectionRecall])
	}
}

// disjointRecallChapters 让历史章节与近期章节使用完全不相交的词汇，
// 用于验证“无命中时不占用召回预算”。
func disjointRecallChapters(count int) []novel.Chapter {
	chapters := make([]novel.Chapter, 0, count)
	for i := 1; i <= count; i++ {
		text := "青铜古卷埋在旧井之下。"
		if i > count-4 {
			text = "雨夜的长街空无一人。"
		}
		chapters = append(chapters, novel.Chapter{
			ID:      fmt.Sprintf("ch_%d", i),
			Index:   i,
			Title:   fmt.Sprintf("第%d章", i),
			Content: strings.Repeat(text, 150),
		})
	}
	return chapters
}

func TestBuildRecallNoHitReclaimsBudget(t *testing.T) {
	chapters := disjointRecallChapters(22)
	auto := Build(Input{
		Title:        "无命中",
		Chapters:     chapters,
		ContextChars: 8000,
	})
	off := Build(Input{
		Title:        "无命中",
		Chapters:     chapters,
		ContextChars: 8000,
		UseRecall:    boolRef(false),
	})
	if auto.RecallSection != "" || auto.RecallBudget != 0 {
		t.Errorf("no-hit recall should not consume budget: %q budget=%d", auto.RecallSection, auto.RecallBudget)
	}
	if auto.ContextChars != off.ContextChars {
		t.Errorf("no-hit manuscript = %d, want same as recall off %d", auto.ContextChars, off.ContextChars)
	}
}

func TestContinuationWindowForRecall(t *testing.T) {
	chapters := recallChapters(22)
	recentAuto, startAuto := ContinuationWindowFor(chapters, 8000, nil)
	recentOff, startOff := ContinuationWindowFor(chapters, 8000, boolRef(false))
	if startAuto < startOff {
		t.Errorf("auto startIndex = %d, want >= off %d", startAuto, startOff)
	}
	if utf8.RuneCountInString(recentAuto) >= utf8.RuneCountInString(recentOff) {
		t.Errorf("auto recent = %d runes, want < off %d",
			utf8.RuneCountInString(recentAuto), utf8.RuneCountInString(recentOff))
	}
}

func TestBuildRecallBudgetInvariant(t *testing.T) {
	chapters := recallChapters(22)
	memory := &novel.ContinuationMemory{
		BudgetChars: novel.MemoryBudgetChars(4000),
		Records: []novel.MemoryRecord{
			{ID: "rec_1", Kind: novel.MemoryKindEvent, Text: strings.Repeat("已确认事件", 40), Order: 1, Priority: novel.MemoryPriorityP1},
			{ID: "rec_2", Kind: novel.MemoryKindThread, Text: strings.Repeat("未兑现伏笔", 40), Order: 2, Priority: novel.MemoryPriorityP1},
		},
	}
	prompt := Build(Input{
		Title:        "预算不变量",
		Chapters:     chapters,
		Direction:    "继续追查青玉符的来历。",
		ContextChars: 4000,
		Memory:       memory,
	})
	if prompt.OverBudget {
		t.Error("OverBudget = true, want false")
	}
	userRunes := utf8.RuneCountInString(prompt.User)
	if userRunes > 4000 {
		t.Errorf("user prompt = %d, want <= 4000", userRunes)
	}
	sectionSum := 0
	for _, count := range prompt.Sections {
		sectionSum += count
	}
	if sectionSum > userRunes {
		t.Errorf("sum(Sections) = %d > user runes %d", sectionSum, userRunes)
	}
	if prompt.Sections[SectionRecall] > prompt.RecallBudget {
		t.Errorf("recall section = %d > budget %d", prompt.Sections[SectionRecall], prompt.RecallBudget)
	}
	if prompt.ContextChars < 2000 {
		t.Errorf("manuscript = %d, want >= ctx/2", prompt.ContextChars)
	}
}

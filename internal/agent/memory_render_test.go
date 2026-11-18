package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"novelagent/internal/novel"
)

// goldenBuildInput 返回固定语料：章节总量 < 9000 rune，保证预算改造后
// nil 记忆输出仍应与迭代 2 基线逐字节一致（I3-01）。
func goldenBuildInput() Input {
	return Input{
		Title: "长夜行",
		Knowledge: novel.Knowledge{
			Overview: "边城失守后，林渊带着铜牌南下，苏黎以为他已死。",
			Style: novel.StyleProfile{
				Perspective: "第三人称限知",
				Tone:        "冷峻",
				Sentence:    "短句为主",
				Dialogue:    "对白克制",
				Pacing:      "紧凑",
				Notes:       "多用雨夜意象",
				Samples:     []string{"雨落在铜牌上。"},
			},
			Characters: []novel.Character{
				{
					ID: "c_lin", Name: "林渊", Aliases: []string{"阿渊"},
					Role: "边军校尉", State: "左臂贯穿伤", Personality: "寡言",
					Confidence: 0.9, LastSeen: 2,
				},
				{ID: "c_su", Name: "苏黎", Role: "医女", Confidence: 0.5, LastSeen: 1},
			},
			World: []novel.WorldEntry{
				{ID: "w_bronze", Title: "铜牌", Category: "物品", Content: "刻有旧朝纹样。", Confidence: 0.8, LastSeen: 2},
			},
			Summaries: []novel.ChapterSummary{
				{ChapterID: "ch_1", Index: 1, Title: "边城", Summary: "边城失守，林渊突围。"},
			},
		},
		Chapters: []novel.Chapter{
			{ID: "ch_1", Index: 1, Title: "边城", Content: "边城的火光照了整夜。林渊把铜牌塞进怀里，翻身上马。"},
			{ID: "ch_2", Index: 2, Title: "南渡", Content: "雨声里，他按住左臂的伤。苏黎在渡口等了一夜，没有等到人。"},
		},
		Direction:    "让苏黎在渡口撞见林渊。",
		TargetChars:  600,
		ContextChars: 12000,
	}
}

func goldenPath(name string) string {
	return filepath.Join("testdata", name)
}

func readGolden(t *testing.T, name, got string) {
	t.Helper()
	path := goldenPath(name)
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("create testdata: %v", err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (run with UPDATE_GOLDEN=1 to record)", name, err)
	}
	if got != string(want) {
		t.Errorf("prompt differs from golden %s (len got=%d want=%d)", name, len(got), len(want))
	}
}

func TestBuildGoldenBaselineWithoutMemory(t *testing.T) {
	prompt := Build(goldenBuildInput())
	got := prompt.System + "\n===USER===\n" + prompt.User
	readGolden(t, "prompt_without_memory.golden", got)
	if prompt.MemorySection != "" {
		t.Error("MemorySection should be empty without memory")
	}
	if strings.Contains(prompt.System, "3.1") {
		t.Error("memory rule must not be added when no memory is injected")
	}
}

func memoryFixture() *novel.ContinuationMemory {
	return &novel.ContinuationMemory{
		BudgetChars:    3000,
		UsedChars:      0,
		MemoryRevision: 12,
		Seeds:          []novel.MemorySeed{{EntityID: "c_lin", EntityName: "林渊", Score: 4.9, Reason: novel.MentionReasonRecent}},
		Records: []novel.MemoryRecord{
			{ID: "state_1", Kind: novel.MemoryKindState, Subject: "林渊", Text: "林渊｜伤势：左臂贯穿伤，未处理", Priority: novel.MemoryPriorityP0},
			{ID: "state_2", Kind: novel.MemoryKindWorldState, Subject: "铜牌", Text: "铜牌｜纹样：旧朝双螭纹", Priority: novel.MemoryPriorityP0},
			{ID: "evt_1", Kind: novel.MemoryKindEvent, Text: "第2章：林渊在渡口躲过搜捕", Order: 2, Priority: novel.MemoryPriorityP1},
		},
	}
}

func TestBuildInjectsMemorySectionAndRule(t *testing.T) {
	input := goldenBuildInput()
	// 窗口 1200（2000 − 800）：ch_2 超出窗口 → 起点为第 2 章，ch_1 摘要进入【前情提要】。
	input.ContextChars = 2000
	input.Chapters = []novel.Chapter{
		{ID: "ch_1", Index: 1, Title: "边城", Content: "边城的火光照了整夜。"},
		{ID: "ch_2", Index: 2, Title: "南渡", Content: strings.Repeat("雨声更密，林渊按住左臂。", 150)},
	}
	input.Memory = memoryFixture()
	prompt := Build(input)

	for _, want := range []string{
		"【已确认剧情记忆（来自正文抽取，效力低于下方最近正文）】",
		"世界规则：",
		"人物当前状态：",
		"- 林渊｜伤势：左臂贯穿伤，未处理",
		"- 铜牌｜纹样：旧朝双螭纹",
		"- 第2章：林渊在渡口躲过搜捕",
	} {
		if !strings.Contains(prompt.User, want) {
			t.Errorf("user prompt missing %q\n%s", want, prompt.User)
		}
	}
	worldAt := strings.Index(prompt.User, "【相关设定】")
	memoryAt := strings.Index(prompt.User, "【已确认剧情记忆")
	summariesAt := strings.Index(prompt.User, "【前情提要】")
	if worldAt < 0 || memoryAt < 0 || summariesAt < 0 || !(worldAt < memoryAt && memoryAt < summariesAt) {
		t.Errorf("memory section order wrong: world=%d memory=%d summaries=%d", worldAt, memoryAt, summariesAt)
	}
	if !strings.Contains(prompt.System, "3.1") || !strings.Contains(prompt.System, "一律以最近正文为准") {
		t.Error("system prompt memory rule is missing")
	}
	for _, internalID := range []string{"state_1", "evt_1", "c_lin"} {
		if strings.Contains(prompt.User, internalID) {
			t.Errorf("internal id %q leaked into prompt", internalID)
		}
	}
	if prompt.MemorySection != novel.RenderMemorySection(input.Memory) {
		t.Error("MemorySection does not match renderer output")
	}
}

func TestBuildMemoryBudgetWindows(t *testing.T) {
	cases := []struct {
		context, budget, window int
	}{
		{12000, 3000, 9000},
		{30000, 3000, 27000},
		{4000, 1000, 3000},
		{1600, 0, 1600},
		{1000, 0, 1000},
	}
	for _, c := range cases {
		if got := novel.MemoryBudgetChars(c.context); got != c.budget {
			t.Errorf("MemoryBudgetChars(%d) = %d, want %d", c.context, got, c.budget)
		}
		if got := novel.ManuscriptWindowChars(c.context); got != c.window {
			t.Errorf("ManuscriptWindowChars(%d) = %d, want %d", c.context, got, c.window)
		}
	}

	var b strings.Builder
	for i := 0; i < 600; i++ {
		b.WriteString("这是一段用于填充正文窗口的长句子。\n")
	}
	input := goldenBuildInput()
	input.Chapters = []novel.Chapter{{ID: "ch_1", Index: 1, Content: b.String()}}
	input.Memory = memoryFixture()
	prompt := Build(input)
	if prompt.ContextChars > 9000 {
		t.Errorf("manuscript window = %d, want <= 9000 (ContextChars 12000 - memory 3000)", prompt.ContextChars)
	}
	if !prompt.Truncated {
		t.Error("Truncated = false, want true")
	}
	if prompt.User != "" && strings.Count(prompt.User, "【已确认剧情记忆") != 1 {
		t.Error("memory section missing or duplicated")
	}
	if novel.MemorySectionChars(input.Memory) > 3000 {
		t.Errorf("memory section chars = %d, want <= 3000", novel.MemorySectionChars(input.Memory))
	}

	// I3-08：小窗口预算为 0 时即使带记忆记录也不渲染记忆段、不加规则，
	// 输出与零记忆基线逐字节一致。
	small := goldenBuildInput()
	small.ContextChars = 1600
	small.Memory = &novel.ContinuationMemory{
		BudgetChars: 0,
		Records: []novel.MemoryRecord{
			{ID: "state_ignored", Kind: novel.MemoryKindState, Text: "不应渲染", Priority: novel.MemoryPriorityP0},
		},
	}
	smallPrompt := Build(small)
	readGolden(t, "prompt_without_memory.golden", smallPrompt.System+"\n===USER===\n"+smallPrompt.User)
	if smallPrompt.MemorySection != "" {
		t.Errorf("zero budget rendered memory section: %q", smallPrompt.MemorySection)
	}
}

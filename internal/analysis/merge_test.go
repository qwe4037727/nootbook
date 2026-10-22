package analysis

import (
	"strings"
	"testing"
	"unicode/utf8"

	"novelagent/internal/novel"
)

func TestParseJSONObjectToleratesFencesAndProse(t *testing.T) {
	var result chapterResult
	text := "分析结果如下：\n```json\n{\"summary\":\"雨夜来客。\",\"endsAt\":\"门被推开。\",\"characters\":[{\"name\":\"林砚\",\"confidence\":0.9}]}\n```\n以上。"
	if err := parseJSONObject(text, &result); err != nil {
		t.Fatalf("parseJSONObject: %v", err)
	}
	if result.Summary != "雨夜来客。" || result.EndsAt != "门被推开。" {
		t.Errorf("result = %+v", result)
	}
	if len(result.Characters) != 1 || result.Characters[0].Name != "林砚" {
		t.Errorf("characters = %+v", result.Characters)
	}

	if err := parseJSONObject("没有 JSON", &result); err == nil {
		t.Error("parseJSONObject(no JSON) returned nil error")
	}
}

func TestApplyChapterMergesAndKeepsLatestState(t *testing.T) {
	knowledge := novel.NewKnowledge()

	first := novel.Chapter{ID: "ch_1", Index: 1, Title: "初始"}
	applyChapter(&knowledge, first, chapterResult{
		Summary: "主角在雨夜醒来。",
		EndsAt:  "门外传来脚步声。",
		Characters: []novel.Character{
			{
				Name:       "林砚",
				Aliases:    []string{"砚哥", "林砚"},
				Role:       "县衙捕快",
				Personality: "寡言",
				State:      "在县衙值夜",
				Confidence: 0.9,
				Evidence:   []string{"林砚推门而入。"},
			},
			{Name: "  ", Role: "应被丢弃"},
		},
		World: []novel.WorldEntry{
			{Title: "灵气复苏", Category: "规则", Content: "三年前开始。", Confidence: 0.8},
			{Title: "   ", Content: "应被丢弃"},
		},
		Style: novel.StyleProfile{Perspective: "第三人称限知", Samples: []string{"雨声渐密。"}},
	})

	if len(knowledge.Characters) != 1 || len(knowledge.World) != 1 {
		t.Fatalf("characters/world = %d/%d", len(knowledge.Characters), len(knowledge.World))
	}
	character := knowledge.Characters[0]
	if character.Name != "林砚" || character.ID == "" {
		t.Errorf("character = %+v", character)
	}
	if len(character.Aliases) != 1 || character.Aliases[0] != "砚哥" {
		t.Errorf("aliases = %+v", character.Aliases)
	}
	if character.LastSeen != 1 || knowledge.World[0].LastSeen != 1 {
		t.Errorf("lastSeen = %d / %d", character.LastSeen, knowledge.World[0].LastSeen)
	}
	if len(knowledge.Summaries) != 1 || knowledge.Summaries[0].ChapterID != "ch_1" {
		t.Errorf("summaries = %+v", knowledge.Summaries)
	}

	second := novel.Chapter{ID: "ch_2", Index: 2, Title: "重瞳"}
	applyChapter(&knowledge, second, chapterResult{
		Summary: "林砚在旧宅遇袭。",
		EndsAt:  "黑影掠上屋脊。",
		Characters: []novel.Character{
			{Name: "林砚", Aliases: []string{"林捕快"}, State: "左肩中箭", Confidence: 0.95, Evidence: []string{"血顺着袖口滴下。"}},
			{Name: "沈青禾", Role: "医女", Confidence: 0.7},
		},
		World: []novel.WorldEntry{
			{Title: "灵气复苏", Content: "灵气在三年前复苏，近来愈发浓烈。", Confidence: 0.9},
		},
	})

	if len(knowledge.Characters) != 2 {
		t.Fatalf("characters = %+v", knowledge.Characters)
	}
	character = knowledge.Characters[0]
	if character.State != "左肩中箭" || character.LastSeen != 2 || character.Confidence != 0.95 {
		t.Errorf("latest chapter did not update character: %+v", character)
	}
	if len(character.Aliases) != 2 {
		t.Errorf("aliases were not merged: %+v", character.Aliases)
	}
	if knowledge.World[0].Content != "灵气在三年前复苏，近来愈发浓烈。" {
		t.Errorf("world content = %q", knowledge.World[0].Content)
	}
	if len(knowledge.Summaries) != 2 || knowledge.Summaries[1].Summary != "林砚在旧宅遇袭。" {
		t.Errorf("summaries = %+v", knowledge.Summaries)
	}

	applyChapter(&knowledge, first, chapterResult{
		Summary: "雨夜重写后的摘要。",
		Characters: []novel.Character{
			{Name: "林砚", State: "旧状态", Confidence: 0.9},
		},
	})
	character = knowledge.Characters[0]
	if character.State != "左肩中箭" {
		t.Errorf("older chapter overwrote the latest state: %+v", character)
	}
	if knowledge.Summaries[0].Summary != "雨夜重写后的摘要。" {
		t.Errorf("summary for the reanalyzed chapter was not replaced: %+v", knowledge.Summaries)
	}
	if len(knowledge.Characters[0].Evidence) != 2 ||
		!strings.Contains(strings.Join(knowledge.Characters[0].Evidence, "|"), "林砚推门而入。") ||
		!strings.Contains(strings.Join(knowledge.Characters[0].Evidence, "|"), "血顺着袖口滴下。") {
		t.Errorf("evidence was not preserved: %+v", knowledge.Characters[0].Evidence)
	}
}

func TestApplyChapterMatchesAliases(t *testing.T) {
	knowledge := novel.NewKnowledge()
	knowledge.Characters = []novel.Character{
		{ID: "c_1", Name: "林砚", Aliases: []string{"砚哥"}, State: "在县衙", Confidence: 0.8, LastSeen: 1},
	}
	applyChapter(&knowledge, novel.Chapter{ID: "ch_2", Index: 2}, chapterResult{
		Characters: []novel.Character{
			{Name: "砚哥", State: "在旧宅", Confidence: 0.9},
		},
	})
	if len(knowledge.Characters) != 1 {
		t.Fatalf("characters = %+v", knowledge.Characters)
	}
	character := knowledge.Characters[0]
	if character.Name != "林砚" || character.State != "在旧宅" || character.LastSeen != 2 {
		t.Errorf("character = %+v", character)
	}
	if character.ID != "c_1" {
		t.Errorf("character ID = %q", character.ID)
	}
}

func TestApplySynthesisUpdatesOverviewAndStyle(t *testing.T) {
	knowledge := novel.NewKnowledge()
	knowledge.Style.Notes = "旧笔记"
	knowledge.Style.Tone = "平缓"
	applySynthesis(&knowledge, synthesisResult{
		Overview: "  灵气复苏后的县城，林砚追查旧案。  ",
		Style:    novel.StyleProfile{Tone: "冷峻", Samples: []string{"雨声渐密。"}},
	})
	if knowledge.Overview != "灵气复苏后的县城，林砚追查旧案。" {
		t.Errorf("overview = %q", knowledge.Overview)
	}
	if knowledge.Style.Tone != "冷峻" || knowledge.Style.Notes != "旧笔记" {
		t.Errorf("style = %+v", knowledge.Style)
	}
	if len(knowledge.Style.Samples) != 1 {
		t.Errorf("samples = %+v", knowledge.Style.Samples)
	}
}

func TestBuildAnalystMessagesIncludesDigestAndClipsLongChapter(t *testing.T) {
	content := strings.Repeat("雨", 31000)
	channel := novel.Chapter{ID: "ch_1", Index: 1, Title: "落雨", Content: content}
	knowledge := novel.NewKnowledge()
	knowledge.Characters = []novel.Character{{Name: "林砚", Role: "捕快"}}

	messages := buildAnalystMessages("雨夜", channel, knowledge)
	if len(messages) != 2 || messages[0].Role != "system" || messages[1].Role != "user" {
		t.Fatalf("messages = %+v", messages)
	}
	user := messages[1].Content
	if !strings.Contains(user, "《雨夜》") || !strings.Contains(user, "第 1 章 落雨") {
		t.Errorf("user prompt is missing book/chapter header:\n%s", user[:200])
	}
	if !strings.Contains(user, "林砚") {
		t.Error("user prompt is missing the existing knowledge digest")
	}
	if !strings.Contains(user, "（本章过长，以上仅为前半部分）") {
		t.Error("user prompt is missing the truncation note")
	}
	if strings.Count(user, "雨") > 30050 {
		t.Errorf("chapter was not clipped: %d runes", strings.Count(user, "雨"))
	}
}

func TestBalancedSummariesKeepsHeadAndTail(t *testing.T) {
	summaries := make([]novel.ChapterSummary, 0, 40)
	for i := 1; i <= 40; i++ {
		summaries = append(summaries, novel.ChapterSummary{Index: i, Summary: "摘要"})
	}
	selected := balancedSummaries(summaries, 30)
	if len(selected) != 30 {
		t.Fatalf("len(selected) = %d, want 30", len(selected))
	}
	if selected[0].Index != 1 || selected[2].Index != 3 {
		t.Errorf("head = %d..%d", selected[0].Index, selected[2].Index)
	}
	if selected[len(selected)-1].Index != 40 {
		t.Errorf("tail ends at %d", selected[len(selected)-1].Index)
	}
	if selected[3].Index != 14 {
		t.Errorf("tail starts at %d, want 14", selected[3].Index)
	}

	messages := buildSynthesisMessages("雨夜", novel.Knowledge{Summaries: summaries})
	user := messages[1].Content
	if !strings.Contains(user, "……（中间章节略）") {
		t.Errorf("synthesis prompt is missing the omitted-chapters marker\n%s", user)
	}
	if !strings.Contains(user, "（尚未归纳）") {
		t.Errorf("synthesis prompt should note missing style\n%s", user)
	}
	if utf8.RuneCountInString(user) == 0 {
		t.Error("synthesis prompt is empty")
	}
}

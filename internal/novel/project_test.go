package novel

import "testing"

func TestCountCharsIgnoresWhitespace(t *testing.T) {
	if got := CountChars("雨落\n到　青石板上。 \t"); got != 8 {
		t.Errorf("CountChars = %d, want 8", got)
	}
	if got := CountChars("\n\r\t ｦ "); got != 1 {
		t.Errorf("CountChars for whitespace plus one rune = %d, want 1", got)
	}
}

func TestKnowledgeNormalizeMergesAndCleans(t *testing.T) {
	knowledge := Knowledge{
		Characters: []Character{
			{Name: " 林砚 ", Aliases: []string{"砚哥", " 林砚 ", ""}, Role: "捕快", Confidence: 1.4, LastSeen: -1},
			{Name: "林砚", State: "左肩受伤", Evidence: []string{"血顺着袖口滴下。", "血顺着袖口滴下。"}, Confidence: 0.8},
			{Name: "  ", Role: "应被丢弃"},
		},
		World: []WorldEntry{
			{Title: "灵气复苏", Content: "三年前开始。", Confidence: -1},
			{Title: "", Content: "应被丢弃"},
		},
		Style: StyleProfile{Samples: []string{"原文", "原文", " "}},
		Summaries: []ChapterSummary{
			{Index: 2, Summary: "第二章摘要。"},
			{Index: 1, Summary: "第一章摘要。"},
			{Index: 0, Summary: "应被丢弃"},
			{Index: 2, Summary: "第二章新摘要。"},
		},
	}
	knowledge.Normalize()

	if len(knowledge.Characters) != 1 {
		t.Fatalf("characters = %d, want 1: %+v", len(knowledge.Characters), knowledge.Characters)
	}
	character := knowledge.Characters[0]
	if len(character.Aliases) != 1 || character.Aliases[0] != "砚哥" {
		t.Errorf("aliases = %+v", character.Aliases)
	}
	if character.State != "左肩受伤" || character.Confidence != 1 {
		t.Errorf("merged character = %+v", character)
	}
	if character.ID == "" {
		t.Error("character ID was not assigned")
	}
	if len(character.Evidence) != 1 {
		t.Errorf("evidence = %+v", character.Evidence)
	}
	if len(knowledge.World) != 1 || knowledge.World[0].ID == "" {
		t.Errorf("world = %+v", knowledge.World)
	}
	if len(knowledge.Style.Samples) != 1 {
		t.Errorf("style samples = %+v", knowledge.Style.Samples)
	}
	if len(knowledge.Summaries) != 2 || knowledge.Summaries[0].Index != 1 || knowledge.Summaries[1].Summary != "第二章新摘要。" {
		t.Errorf("summaries = %+v", knowledge.Summaries)
	}
}

func TestProjectNormalizeSortsAndClamps(t *testing.T) {
	project := Project{
		Title: "  ",
		Chapters: []ChapterMeta{
			{ID: "b", Index: 5, Title: "第二章", CharCount: -3},
			{ID: "a", Index: 2, Title: "第一章"},
		},
		Analysis: AnalysisState{Status: AnalysisReady, AnalyzedCount: 1, TotalCount: 9},
	}
	project.Normalize()
	if project.Title != "未命名小说" {
		t.Errorf("title = %q", project.Title)
	}
	if len(project.Chapters) != 2 || project.Chapters[0].ID != "a" || project.Chapters[1].Index != 2 {
		t.Errorf("chapters = %+v", project.Chapters)
	}
	if project.Chapters[0].CharCount != 0 {
		t.Errorf("charCount = %d", project.Chapters[0].CharCount)
	}
	if project.Analysis.TotalCount != 2 || project.Analysis.AnalyzedCount != 2 || project.Analysis.Status != AnalysisReady {
		t.Errorf("analysis = %+v", project.Analysis)
	}
}

func TestNewIDAndValidID(t *testing.T) {
	id := NewID("ch")
	if !ValidID(id) {
		t.Errorf("NewID produced invalid id %q", id)
	}
	if len(id) < 8 {
		t.Errorf("id too short: %q", id)
	}
	for _, invalid := range []string{"", "../x", "a/b", "a.b", "a b", "中文"} {
		if ValidID(invalid) {
			t.Errorf("ValidID(%q) = true", invalid)
		}
	}
	if NewID("c") == NewID("c") {
		t.Error("NewID returned duplicate values")
	}
}

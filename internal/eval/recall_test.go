package eval

import (
	"path/filepath"
	"strings"
	"testing"

	"novelagent/internal/agent"
	"novelagent/internal/novel"
)

// R4 在长篇回归集里的可度量性：召回开关必须进报告、进预算、进章节前缀语义。

func recallCase(chapters int) Case {
	return Case{
		ID:    "recall-test",
		Title: "长篇召回回归",
		Source: SourceSpec{
			Synthetic: &SyntheticSpec{
				Chapters: chapters, ChapterChars: 600, Seed: 11,
				Planted: []PlantedFact{{Text: "青玉符", Chapter: 2}},
			},
		},
		StopPoints: []StopPoint{
			{Name: "auto", AfterChapter: chapters, Direction: "继续追查青玉符的来历。"},
			{Name: "on", AfterChapter: chapters, Direction: "继续追查青玉符的来历。", UseRecall: boolPtr(true)},
			{Name: "off", AfterChapter: chapters, Direction: "继续追查青玉符的来历。", UseRecall: boolPtr(false)},
		},
	}
}

func TestRunRecallStopPoints(t *testing.T) {
	caseDir := t.TempDir()
	c := recallCase(novel.RecallMinChapters + 4)
	workDir := filepath.Join(t.TempDir(), "data")

	report, err := Run(c, caseDir, workDir, Options{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(report.Stops) != 3 {
		t.Fatalf("stops = %d, want 3", len(report.Stops))
	}

	auto := findStop(t, report, "auto")
	if !auto.Recall.Enabled || auto.Recall.InjectedChars == 0 {
		t.Fatalf("auto stop at %d chapters should inject recall: %+v", auto.AfterChapter, auto.Recall)
	}
	if auto.Recall.Requested != "auto" {
		t.Errorf("auto requested = %q, want auto", auto.Recall.Requested)
	}
	if !auto.Recall.AutoEligible {
		t.Error("autoEligible = false at 24 chapters with ctx12000")
	}
	if auto.Recall.BudgetChars != novel.RecallBudgetChars(12000) {
		t.Errorf("recall budget = %d, want %d", auto.Recall.BudgetChars, novel.RecallBudgetChars(12000))
	}
	if auto.Recall.InjectedChars > auto.Recall.BudgetChars {
		t.Errorf("recall injected %d > budget %d", auto.Recall.InjectedChars, auto.Recall.BudgetChars)
	}
	if auto.Recall.InjectedChars != auto.Prompt.Sections[agent.SectionRecall] {
		t.Errorf("recall injected = %d, sections[recall] = %d", auto.Recall.InjectedChars, auto.Prompt.Sections[agent.SectionRecall])
	}
	if !containsInt(auto.Recall.Chapters, 2) {
		t.Errorf("recall chapters = %v, want to include planted chapter 2", auto.Recall.Chapters)
	}
	if auto.Recall.Hits != len(auto.Recall.Chapters) {
		t.Errorf("recall hits = %d, chapters = %v", auto.Recall.Hits, auto.Recall.Chapters)
	}
	if auto.Prompt.OverBudget {
		t.Errorf("recall stop over budget: %+v", auto.Prompt)
	}

	on := findStop(t, report, "on")
	if !on.Recall.Enabled || on.Recall.Requested != "on" {
		t.Errorf("explicit on not injected: %+v", on.Recall)
	}
	if on.Prompt.UserChars != auto.Prompt.UserChars {
		t.Errorf("auto user chars = %d, explicit on = %d, want identical (auto==on at this size)",
			on.Prompt.UserChars, auto.Prompt.UserChars)
	}

	off := findStop(t, report, "off")
	if off.Recall.Enabled || off.Recall.InjectedChars != 0 || off.Recall.BudgetChars != 0 {
		t.Errorf("useRecall=false injected recall: %+v", off.Recall)
	}
	if off.Recall.Requested != "off" {
		t.Errorf("off requested = %q, want off", off.Recall.Requested)
	}
	if off.Prompt.Sections[agent.SectionRecall] != 0 {
		t.Errorf("off sections[recall] = %d, want 0", off.Prompt.Sections[agent.SectionRecall])
	}
	if off.Prompt.ManuscriptChars <= auto.Prompt.ManuscriptChars {
		t.Errorf("recall-off manuscript = %d, want > recall-on %d",
			off.Prompt.ManuscriptChars, auto.Prompt.ManuscriptChars)
	}

	if report.Totals.RecallInjectedStops != 2 {
		t.Errorf("totals.recallInjectedStops = %d, want 2", report.Totals.RecallInjectedStops)
	}

	// ctx1600 的正文硬下限优先：召回退化为 0，不挤占正文窗口。
	narrowCase := recallCase(novel.RecallMinChapters + 4)
	narrowCase.StopPoints[0].ContextChars = 1600
	narrow, err := Run(narrowCase, caseDir, filepath.Join(t.TempDir(), "data-narrow"), Options{})
	if err != nil {
		t.Fatalf("narrow Run: %v", err)
	}
	narrowAuto := findStop(t, narrow, "auto")
	if narrowAuto.Recall.BudgetChars != 0 || narrowAuto.Recall.Enabled {
		t.Errorf("ctx1600 recall should degrade to 0: %+v", narrowAuto.Recall)
	}
	if narrowAuto.Prompt.OverBudget {
		t.Errorf("ctx1600 stop over budget: %+v", narrowAuto.Prompt)
	}
}

func TestRunRecallDisabledForShortCorpus(t *testing.T) {
	caseDir := t.TempDir()
	c := recallCase(12)
	c.StopPoints = []StopPoint{{Name: "auto", AfterChapter: 12, Direction: "继续追查青玉符的来历。"}}

	report, err := Run(c, caseDir, filepath.Join(t.TempDir(), "data"), Options{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	stop := findStop(t, report, "auto")
	if stop.Recall.Enabled || stop.Recall.BudgetChars != 0 || stop.Recall.AutoEligible {
		t.Errorf("recall should stay off below %d chapters: %+v", novel.RecallMinChapters, stop.Recall)
	}
	if report.Totals.RecallInjectedStops != 0 {
		t.Errorf("totals.recallInjectedStops = %d, want 0", report.Totals.RecallInjectedStops)
	}
	for _, warning := range report.Warnings {
		if strings.Contains(warning, "recall requested but not injected") {
			t.Errorf("unexpected recall warning without explicit useRecall: %q", warning)
		}
	}
}

func TestRunRecallExplicitOnWarnsWhenNothingInjected(t *testing.T) {
	caseDir := t.TempDir()
	c := recallCase(12)
	c.StopPoints = []StopPoint{{Name: "forced", AfterChapter: 12, Direction: "继续。",
		UseRecall: boolPtr(true), ContextChars: 1600}}

	report, err := Run(c, caseDir, filepath.Join(t.TempDir(), "data"), Options{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	stop := findStop(t, report, "forced")
	if stop.Recall.Enabled {
		t.Fatal("ctx1600 forced recall should not inject")
	}
	if !containsWarning(report.Warnings, "recall requested but not injected") {
		t.Errorf("warnings = %v, want recall-not-injected warning", report.Warnings)
	}
}

func containsInt(values []int, want int) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

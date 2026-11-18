package eval

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"novelagent/internal/agent"
	"novelagent/internal/llm"
	"novelagent/internal/novel"
	"novelagent/internal/store"
)

func writeJSONFixture(t *testing.T, path string, value any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir fixture dir: %v", err)
	}
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
}

func boolPtr(value bool) *bool { return &value }

func writeLongformCaseFixtures(t *testing.T, caseDir string) Case {
	t.Helper()
	writeJSONFixture(t, filepath.Join(caseDir, "knowledge.json"), novel.Knowledge{
		Overview: "县城连月阴雨，林砚在追查旧案。",
		Characters: []novel.Character{
			{ID: "c_lin", Name: "林砚", Aliases: []string{"砚哥"}, Confidence: 0.95, LastSeen: 12},
			{ID: "c_shen", Name: "沈青禾", Confidence: 0.9, LastSeen: 12},
		},
		Summaries: []novel.ChapterSummary{
			{Index: 1, Title: "雨夜", Summary: "林砚在长街追查旧案。"},
			{Index: 2, Title: "旧信", Summary: "一封没有署名的旧信送到县衙。"},
		},
	})
	memoryDir := filepath.Join(caseDir, "memory")
	writeJSONFixture(t, filepath.Join(memoryDir, "meta.json"), map[string]any{
		"schemaVersion": 1, "memoryRevision": 12,
	})
	writeJSONFixture(t, filepath.Join(memoryDir, "states.json"), []novel.StoryState{
		{ID: "state_wound", Status: novel.StateActive, SubjectID: "c_lin", Category: "状态",
			Attribute: "伤势", Value: "左臂旧伤未愈", MemoryMeta: novel.MemoryMeta{RecordVersion: 3}},
		{ID: "state_review", Status: novel.StateActive, SubjectID: "c_lin", Category: "计划",
			Attribute: "行程", Value: "三日后离城", MemoryMeta: novel.MemoryMeta{RecordVersion: 2, NeedsReview: true}},
		{ID: "state_stale", Status: novel.StateSuperseded, SubjectID: "c_lin", Category: "状态",
			Attribute: "伤势", Value: "已经痊愈", MemoryMeta: novel.MemoryMeta{RecordVersion: 1}},
	})
	writeJSONFixture(t, filepath.Join(memoryDir, "events.json"), []novel.StoryEvent{
		{ID: "evt_1", Status: novel.EventConfirmed, EventType: "discovery",
			Summary: "林砚在旧宅找到半枚青玉符。", StoryOrder: 2, ParticipantsKnown: true,
			Participants: []string{"c_lin"}},
	})
	writeJSONFixture(t, filepath.Join(memoryDir, "threads.json"), []novel.StoryThread{
		{ID: "thread_jade", Status: novel.ThreadOpen, Title: "青玉符的来历",
			InvolvedCharacters: []string{"c_lin"}},
	})
	writeJSONFixture(t, filepath.Join(memoryDir, "cognitions.json"), []novel.Cognition{
		{ID: "cog_1", Status: novel.CogKnown, SubjectID: "c_lin", Content: "知道青玉符与旧朝有关。"},
	})
	writeJSONFixture(t, filepath.Join(memoryDir, "plans.json"), []novel.StoryPlan{
		{ID: "plan_near", Status: novel.PlanActive, Level: "near_term", Title: "追查青玉符",
			InvolvedCharacters: []string{"c_lin"}},
	})

	return Case{
		ID:    "runner-test",
		Title: "长篇回归测试",
		Source: SourceSpec{
			Synthetic: &SyntheticSpec{
				Chapters: 12, ChapterChars: 600, Seed: 7,
				Planted: []PlantedFact{{Text: "青玉符", Chapter: 2}},
			},
			Knowledge: "knowledge.json",
			Memory:    "memory",
		},
		StopPoints: []StopPoint{
			{Name: "early", AfterChapter: 3, MustLand: []string{"左臂旧伤"}, MustNot: []string{"痊愈"}},
			{Name: "late", AfterChapter: 12, MustLand: []string{"青玉符"}},
			{Name: "nomem", AfterChapter: 12, UseMemory: boolPtr(false)},
		},
	}
}

func TestRunSyntheticDryRunWithMemory(t *testing.T) {
	caseDir := t.TempDir()
	c := writeLongformCaseFixtures(t, caseDir)
	fixedNow := time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)
	opts := Options{
		Pricing: Pricing{InputPerMillion: 1, OutputPerMillion: 2},
		Tiers:   []int{1600, 12000},
		Now:     func() time.Time { return fixedNow },
	}

	report, err := Run(c, caseDir, filepath.Join(t.TempDir(), "data1"), opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if report.Mode != "dry_run" {
		t.Errorf("Mode = %q, want dry_run", report.Mode)
	}
	if report.Source.Kind != "synthetic" || report.Source.Chapters != 12 {
		t.Errorf("source = %+v, want synthetic/12", report.Source)
	}
	if report.Source.Chars < 12*500 {
		t.Errorf("source chars = %d, want >= 6000", report.Source.Chars)
	}
	if len(report.Stops) != 6 {
		t.Fatalf("stops = %d, want 6", len(report.Stops))
	}

	late := findStop(t, report, "late@ctx12000")
	if late.Memory.InjectedChars == 0 || late.Memory.Records == 0 {
		t.Errorf("late stop injected no memory: %+v", late.Memory)
	}
	if late.Memory.InjectedChars != late.Prompt.Sections[agent.SectionMemories] {
		t.Errorf("memory injected chars = %d, sections[memories] = %d",
			late.Memory.InjectedChars, late.Prompt.Sections[agent.SectionMemories])
	}
	if late.Prompt.UserChars > late.ContextChars {
		t.Errorf("user chars = %d > context budget %d", late.Prompt.UserChars, late.ContextChars)
	}
	if late.Prompt.TotalChars < late.Prompt.UserChars {
		t.Errorf("total chars = %d < user chars %d", late.Prompt.TotalChars, late.Prompt.UserChars)
	}
	if late.Prompt.OverBudget {
		t.Errorf("late@ctx12000 unexpectedly over budget: %+v", late.Prompt)
	}
	if !late.Cost.Priced || late.Cost.TotalCost <= 0 {
		t.Errorf("cost = %+v, want priced positive cost", late.Cost)
	}
	if late.Memory.Revision != 12 {
		t.Errorf("memory revision = %d, want 12", late.Memory.Revision)
	}

	earlyNarrow := findStop(t, report, "early@ctx1600")
	if earlyNarrow.Memory.BudgetChars != 0 || earlyNarrow.Memory.Records != 0 {
		t.Errorf("ctx1600 should disable memory: %+v", earlyNarrow.Memory)
	}
	if earlyNarrow.Prompt.Sections[agent.SectionMemories] != 0 {
		t.Errorf("ctx1600 memory section = %d, want 0", earlyNarrow.Prompt.Sections[agent.SectionMemories])
	}

	noMem := findStop(t, report, "nomem@ctx12000")
	if noMem.Memory.Enabled || noMem.Memory.InjectedChars != 0 {
		t.Errorf("useMemory=false still injected memory: %+v", noMem.Memory)
	}
	if noMem.Prompt.Sections[agent.SectionMemories] != 0 {
		t.Errorf("useMemory=false memory section = %d, want 0", noMem.Prompt.Sections[agent.SectionMemories])
	}

	if len(late.Checklist) == 0 {
		t.Fatal("late checklist is empty")
	}
	for _, item := range late.Checklist {
		if item.Judgment != "" {
			t.Errorf("dry-run judgment should be empty: %+v", item)
		}
		if item.Hint != "unjudged" {
			t.Errorf("dry-run hint = %q, want unjudged", item.Hint)
		}
	}
	if report.Totals.Stops != 6 || report.Totals.InputTokens <= 0 || report.Totals.TotalCost <= 0 {
		t.Errorf("totals = %+v", report.Totals)
	}

	second, err := Run(c, caseDir, filepath.Join(t.TempDir(), "data2"), opts)
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}
	if !reflect.DeepEqual(report, second) {
		t.Errorf("dry-run report is not deterministic")
	}
}

func TestRunCorpusStopPointUsesChapterPrefix(t *testing.T) {
	caseDir := t.TempDir()
	corpus := strings.Join([]string{
		"第1章 起点\n\n" + strings.Repeat("第一章的旧事。", 60),
		"第2章 转折\n\n" + strings.Repeat("第二章的秘密。", 60),
		"第3章 收束\n\n" + strings.Repeat("第三章的余波。", 60),
	}, "\n\n")
	if err := os.WriteFile(filepath.Join(caseDir, "corpus.txt"), []byte(corpus), 0o644); err != nil {
		t.Fatalf("write corpus: %v", err)
	}
	c := Case{
		ID:    "corpus-prefix",
		Title: "语料前缀",
		Source: SourceSpec{
			Corpus: "corpus.txt",
		},
		StopPoints: []StopPoint{
			{Name: "first", AfterChapter: 1},
			{Name: "all", AfterChapter: 0},
		},
	}

	report, err := Run(c, caseDir, filepath.Join(t.TempDir(), "data"), Options{Tiers: []int{4000}})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if report.Source.Chapters != 3 {
		t.Fatalf("chapters = %d, want 3", report.Source.Chapters)
	}
	first := findStop(t, report, "first")
	all := findStop(t, report, "all")
	if first.AfterChapter != 1 {
		t.Errorf("first afterChapter = %d, want 1", first.AfterChapter)
	}
	if first.Prompt.ManuscriptChars >= all.Prompt.ManuscriptChars {
		t.Errorf("prefix manuscript = %d, want < full manuscript %d",
			first.Prompt.ManuscriptChars, all.Prompt.ManuscriptChars)
	}
}

func TestRunLiveWithMockProvider(t *testing.T) {
	const output = "林砚按住左臂旧伤，看向雨里。"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"` + output + `"},"finish_reason":"length"}]}`))
	}))
	defer server.Close()

	caseDir := t.TempDir()
	c := Case{
		ID:    "live-mock",
		Title: "实时通道",
		Source: SourceSpec{
			Synthetic: &SyntheticSpec{Chapters: 3, ChapterChars: 300, Seed: 11},
		},
		StopPoints: []StopPoint{
			{Name: "one", AfterChapter: 3, MustLand: []string{"左臂旧伤"}, MustNot: []string{"痊愈"}},
		},
	}
	report, err := Run(c, caseDir, filepath.Join(t.TempDir(), "data"), Options{
		Live:    true,
		Client:  llm.NewClient(server.URL, "", "mock"),
		Pricing: Pricing{InputPerMillion: 1, OutputPerMillion: 2},
		Tiers:   []int{4000},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if report.Mode != "live" || len(report.Stops) != 1 {
		t.Fatalf("mode=%q stops=%d", report.Mode, len(report.Stops))
	}
	live := report.Stops[0].Live
	if live == nil {
		t.Fatal("live report is nil")
	}
	if live.Error != "" {
		t.Fatalf("live error = %q", live.Error)
	}
	if live.Output != output || !live.Truncated {
		t.Errorf("live = %+v, want truncated output", live)
	}
	if live.OutputChars == 0 {
		t.Error("live output chars = 0")
	}
	if want := EstimateTokens(output).Total; live.OutputTokens != want {
		t.Errorf("output tokens = %d, want %d", live.OutputTokens, want)
	}
	if report.Stops[0].Cost.OutputTokens != live.OutputTokens {
		t.Errorf("cost output tokens = %d, want %d", report.Stops[0].Cost.OutputTokens, live.OutputTokens)
	}
	for _, item := range report.Stops[0].Checklist {
		switch item.Kind {
		case "must_land":
			if item.Hint != "observed" {
				t.Errorf("must_land hint = %q, want observed", item.Hint)
			}
		case "must_not":
			if item.Hint != "clear" {
				t.Errorf("must_not hint = %q, want clear", item.Hint)
			}
		}
	}
	if !containsWarning(report.Warnings, "truncated") {
		t.Errorf("warnings = %v, want truncation warning", report.Warnings)
	}
}

func TestRunLiveCostLimitSkipsRemainingCalls(t *testing.T) {
	const output = "林砚按住左臂旧伤，看向雨里。"
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"` + output + `"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()

	caseDir := t.TempDir()
	c := Case{
		ID:    "cost-limit",
		Title: "成本上限",
		Source: SourceSpec{
			Synthetic: &SyntheticSpec{Chapters: 3, ChapterChars: 300, Seed: 13},
		},
		StopPoints: []StopPoint{
			{Name: "one", AfterChapter: 3, TargetChars: 20},
			{Name: "two", AfterChapter: 3, TargetChars: 20},
		},
	}
	pricing := Pricing{InputPerMillion: 1, OutputPerMillion: 2}
	tiers := []int{4000}

	if _, err := Run(c, caseDir, filepath.Join(t.TempDir(), "unpriced"), Options{MaxCost: 1}); err == nil ||
		!strings.Contains(err.Error(), "pricing") {
		t.Fatalf("unpriced max cost error = %v, want pricing error", err)
	}

	dry, err := Run(c, caseDir, filepath.Join(t.TempDir(), "dry"), Options{
		Pricing:   pricing,
		Tiers:     tiers,
		StopNames: []string{"one"},
	})
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	projected := dry.Stops[0].Cost.TotalCost
	if projected <= 0 {
		t.Fatalf("projected cost = %f, want > 0", projected)
	}

	report, err := Run(c, caseDir, filepath.Join(t.TempDir(), "live"), Options{
		Live:    true,
		Client:  llm.NewClient(server.URL, "", "mock"),
		Pricing: pricing,
		Tiers:   tiers,
		MaxCost: projected + 1e-9,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if calls.Load() != 1 {
		t.Errorf("provider calls = %d, want 1 (second stop must be skipped)", calls.Load())
	}
	first := findStop(t, report, "one")
	if first.LiveSkipped != "" || first.Live == nil || first.Live.Error != "" {
		t.Errorf("first stop should run live: %+v", first)
	}
	second := findStop(t, report, "two")
	if second.Live != nil {
		t.Errorf("second stop called provider after cost limit: %+v", second.Live)
	}
	if !strings.Contains(second.LiveSkipped, "cost limit") {
		t.Errorf("second liveSkipped = %q, want cost limit", second.LiveSkipped)
	}
	if !report.CostLimitHit || report.Totals.LiveSkippedStops != 1 {
		t.Errorf("costLimitHit=%t skipped=%d, want true/1", report.CostLimitHit, report.Totals.LiveSkippedStops)
	}
	if !containsWarning(report.Warnings, "cost limit") {
		t.Errorf("warnings = %v, want cost limit warning", report.Warnings)
	}
	if first.Cost.TotalCost > report.CostLimit {
		t.Errorf("first cost %f exceeds limit %f", first.Cost.TotalCost, report.CostLimit)
	}
	for _, item := range second.Checklist {
		if item.Hint != "unjudged" {
			t.Errorf("skipped stop checklist hint = %q, want unjudged", item.Hint)
		}
	}
}

func TestRunLiveCostLimitFlagsActualOverrun(t *testing.T) {
	const output = "林砚按住左臂旧伤，看向雨里。"
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"` + output + `"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()

	caseDir := t.TempDir()
	c := Case{
		ID:    "cost-overrun",
		Title: "实际超限",
		Source: SourceSpec{
			Synthetic: &SyntheticSpec{Chapters: 3, ChapterChars: 300, Seed: 17},
		},
		StopPoints: []StopPoint{{Name: "one", AfterChapter: 3, TargetChars: 1}},
	}
	pricing := Pricing{InputPerMillion: 1, OutputPerMillion: 2}
	tiers := []int{4000}
	dry, err := Run(c, caseDir, filepath.Join(t.TempDir(), "dry"), Options{Pricing: pricing, Tiers: tiers})
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	maxCost := dry.Stops[0].Cost.TotalCost + 1e-9

	report, err := Run(c, caseDir, filepath.Join(t.TempDir(), "live"), Options{
		Live:    true,
		Client:  llm.NewClient(server.URL, "", "mock"),
		Pricing: pricing,
		Tiers:   tiers,
		MaxCost: maxCost,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if calls.Load() != 1 {
		t.Errorf("provider calls = %d, want 1", calls.Load())
	}
	stop := findStop(t, report, "one")
	if stop.LiveSkipped != "" || stop.Live == nil || stop.Live.Error != "" {
		t.Errorf("stop should have run live: %+v", stop)
	}
	if !report.CostLimitHit {
		t.Error("costLimitHit = false, want true after actual output exceeded limit")
	}
	if !containsWarning(report.Warnings, "exceeds max cost") {
		t.Errorf("warnings = %v, want actual overrun warning", report.Warnings)
	}
}

func TestCaseValidationAndLoad(t *testing.T) {
	bad := Case{
		ID: "bad",
		Source: SourceSpec{
			Corpus:    "a.txt",
			Synthetic: &SyntheticSpec{Chapters: 2, ChapterChars: 300},
		},
		StopPoints: []StopPoint{{Name: "s"}},
	}
	if err := bad.Validate(); err == nil {
		t.Error("Validate accepted two sources")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "case.json")
	if err := os.WriteFile(path, []byte(`{"id":"x","title":"t","source":{"synthetic":{"chapters":2,"chapterChars":300}},"stopPoints":[{"name":"s"}],"unknown":1}`), 0o644); err != nil {
		t.Fatalf("write case: %v", err)
	}
	if _, err := LoadCase(path); err == nil {
		t.Error("LoadCase accepted unknown fields")
	}
}

func TestGenerateSyntheticIsDeterministic(t *testing.T) {
	spec := SyntheticSpec{
		Chapters: 3, ChapterChars: 300, Seed: 42,
		Planted: []PlantedFact{{Text: "青玉符", Chapter: 2}},
	}
	first := GenerateSynthetic(spec)
	second := GenerateSynthetic(spec)
	if first != second {
		t.Error("GenerateSynthetic is not deterministic")
	}
	if !strings.Contains(first, "青玉符") {
		t.Error("planted fact missing")
	}
	chapters := novel.SplitChapters(first, "合成")
	if len(chapters) != 3 {
		t.Fatalf("SplitChapters = %d chapters, want 3", len(chapters))
	}
	other := GenerateSynthetic(SyntheticSpec{Chapters: 3, ChapterChars: 300, Seed: 43})
	if first == other {
		t.Error("different seeds produced identical corpora")
	}
}

func TestRunDataDirCopiesAndDoesNotModifySource(t *testing.T) {
	srcDir := filepath.Join(t.TempDir(), "source")
	st, err := store.New(srcDir)
	if err != nil {
		t.Fatalf("open source store: %v", err)
	}
	corpus := "第1章 起点\n\n林砚沿着长街往前走，雨声一直没有停。\n\n" +
		"第2章 转折\n\n沈青禾替林砚处理左臂的旧伤。\n\n" +
		"第3章 收束\n\n林砚在旧宅找到半枚青玉符。"
	if _, err := st.ImportChapters("源数据", novel.SplitChapters(corpus, "源数据")); err != nil {
		t.Fatalf("import source: %v", err)
	}
	project, err := st.LoadProject()
	if err != nil {
		t.Fatalf("load source project: %v", err)
	}
	if _, _, err := st.CommitAnalysis(project.Revision, len(project.Chapters), novel.Knowledge{
		Characters: []novel.Character{
			{ID: "c_lin", Name: "林砚", Confidence: 0.9, LastSeen: 3},
			{ID: "c_shen", Name: "沈青禾", Confidence: 0.9, LastSeen: 3},
		},
	}); err != nil {
		t.Fatalf("seed analysis: %v", err)
	}
	fixtureDir := filepath.Join(t.TempDir(), "memory")
	writeJSONFixture(t, filepath.Join(fixtureDir, "states.json"), []novel.StoryState{
		{ID: "state_wound", Status: novel.StateActive, SubjectID: "c_lin", Category: "状态",
			Attribute: "伤势", Value: "左臂旧伤未愈", MemoryMeta: novel.MemoryMeta{RecordVersion: 2}},
	})
	if err := seedMemory(fixtureDir, filepath.Join(srcDir, "memory")); err != nil {
		t.Fatalf("seed source memory: %v", err)
	}
	before := hashTree(t, srcDir)

	c := Case{
		ID:    "datadir",
		Title: "源数据",
		Source: SourceSpec{
			DataDir: srcDir,
		},
		StopPoints: []StopPoint{{Name: "all"}},
	}
	report, err := Run(c, t.TempDir(), filepath.Join(t.TempDir(), "copy"), Options{Tiers: []int{12000}})
	if err != nil {
		t.Fatalf("Run data dir: %v", err)
	}
	if report.Source.Kind != "data_dir" || report.Source.Chapters != 3 {
		t.Errorf("source = %+v, want data_dir/3", report.Source)
	}
	stop := findStop(t, report, "all")
	if stop.Memory.InjectedChars == 0 {
		t.Errorf("data dir memory not injected: %+v", stop.Memory)
	}
	if after := hashTree(t, srcDir); !reflect.DeepEqual(before, after) {
		t.Error("source data dir was modified by the eval run")
	}
}

func hashTree(t *testing.T, root string) map[string]string {
	t.Helper()
	hashes := make(map[string]string)
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()
		sum := sha256.New()
		if _, err := io.Copy(sum, file); err != nil {
			return err
		}
		hashes[rel] = hex.EncodeToString(sum.Sum(nil))
		return nil
	})
	if err != nil {
		t.Fatalf("hash tree: %v", err)
	}
	return hashes
}

func findStop(t *testing.T, report Report, name string) StopReport {
	t.Helper()
	for _, stop := range report.Stops {
		if stop.Name == name {
			return stop
		}
	}
	t.Fatalf("stop %q not found in %v", name, stopNames(report))
	return StopReport{}
}

func stopNames(report Report) []string {
	names := make([]string, 0, len(report.Stops))
	for _, stop := range report.Stops {
		names = append(names, stop.Name)
	}
	return names
}

func containsWarning(warnings []string, want string) bool {
	for _, warning := range warnings {
		if strings.Contains(warning, want) {
			return true
		}
	}
	return false
}

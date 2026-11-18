package eval

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"novelagent/internal/agent"
	"novelagent/internal/llm"
	"novelagent/internal/novel"
	"novelagent/internal/store"
)

const (
	reportSchemaVersion = 1
	defaultTargetChars  = 800
	defaultTemperature  = 0.8
	defaultTimeout      = 3 * time.Minute
)

type Options struct {
	Pricing     Pricing
	Tiers       []int
	StopNames   []string
	Live        bool
	Client      *llm.Client
	Temperature float64
	Timeout     time.Duration
	MaxCost     float64
	Now         func() time.Time
}

type Report struct {
	SchemaVersion int          `json:"schemaVersion"`
	CaseID        string       `json:"caseId"`
	Title         string       `json:"title"`
	Description   string       `json:"description,omitempty"`
	Mode          string       `json:"mode"`
	GeneratedAt   time.Time    `json:"generatedAt"`
	Source        CorpusInfo   `json:"source"`
	Pricing       Pricing      `json:"pricing"`
	CostLimit     float64      `json:"costLimit,omitempty"`
	CostLimitHit  bool         `json:"costLimitHit,omitempty"`
	Stops         []StopReport `json:"stops"`
	Totals        Totals       `json:"totals"`
	Warnings      []string     `json:"warnings,omitempty"`
}

type Totals struct {
	Stops               int     `json:"stops"`
	InputTokens         int     `json:"inputTokens"`
	OutputTokens        int     `json:"outputTokens"`
	TotalCost           float64 `json:"totalCost"`
	Currency            string  `json:"currency"`
	Priced              bool    `json:"priced"`
	MaxUserBudgetPct    float64 `json:"maxUserBudgetPct"`
	MemoryInjectedStops int     `json:"memoryInjectedStops"`
	RecallInjectedStops int     `json:"recallInjectedStops"`
	OverBudgetStops     int     `json:"overBudgetStops"`
	LiveSkippedStops    int     `json:"liveSkippedStops,omitempty"`
}

type StopReport struct {
	Name         string          `json:"name"`
	AfterChapter int             `json:"afterChapter"`
	ContextChars int             `json:"contextChars"`
	TargetChars  int             `json:"targetChars"`
	Direction    string          `json:"direction,omitempty"`
	UseMemory    bool            `json:"useMemory"`
	Notes        string          `json:"notes,omitempty"`
	Memory       MemoryMetrics   `json:"memory"`
	Recall       RecallMetrics   `json:"recall"`
	Prompt       PromptMetrics   `json:"prompt"`
	Cost         CostEstimate    `json:"cost"`
	Live         *LiveReport     `json:"live,omitempty"`
	LiveSkipped  string          `json:"liveSkipped,omitempty"`
	Checklist    []ChecklistItem `json:"checklist,omitempty"`
}

type MemoryMetrics struct {
	Enabled            bool   `json:"enabled"`
	BudgetChars        int    `json:"budgetChars"`
	Records            int    `json:"records"`
	Seeds              int    `json:"seeds"`
	DroppedByBudget    int    `json:"droppedByBudget"`
	SkippedReview      int    `json:"skippedReview"`
	SkippedAmbiguous   int    `json:"skippedAmbiguous"`
	RetrievalUsedChars int    `json:"retrievalUsedChars"`
	InjectedChars      int    `json:"injectedChars"`
	DisabledReason     string `json:"disabledReason,omitempty"`
	Revision           int64  `json:"revision"`
}

// RecallMetrics 记录 R4 历史召回在某个停点的实际形态：请求值（auto/on/off）、
// 是否真的注入、预算与注入字符数、命中章节序号，用于长篇回归对照。
type RecallMetrics struct {
	Requested     string `json:"requested"`
	Enabled       bool   `json:"enabled"`
	AutoEligible  bool   `json:"autoEligible"`
	BudgetChars   int    `json:"budgetChars"`
	InjectedChars int    `json:"injectedChars"`
	Hits          int    `json:"hits"`
	Chapters      []int  `json:"chapters"`
}

type PromptMetrics struct {
	SystemChars     int            `json:"systemChars"`
	UserChars       int            `json:"userChars"`
	TotalChars      int            `json:"totalChars"`
	LimitChars      int            `json:"limitChars"`
	ManuscriptChars int            `json:"manuscriptChars"`
	Truncated       bool           `json:"truncated"`
	OverBudget      bool           `json:"overBudget"`
	UserBudgetPct   float64        `json:"userBudgetPct"`
	Sections        map[string]int `json:"sections"`
	InputTokens     int            `json:"inputTokens"`
}

type LiveReport struct {
	FinishReason string `json:"finishReason,omitempty"`
	Truncated    bool   `json:"truncated"`
	LatencyMS    int64  `json:"latencyMs"`
	OutputChars  int    `json:"outputChars"`
	OutputTokens int    `json:"outputTokens"`
	Output       string `json:"output,omitempty"`
	Error        string `json:"error,omitempty"`
}

type ChecklistItem struct {
	Kind     string `json:"kind"`
	Text     string `json:"text"`
	Hint     string `json:"hint"`
	Judgment string `json:"judgment"`
}

func Run(c Case, caseDir, workDir string, opts Options) (Report, error) {
	src, err := Prepare(c, caseDir, workDir)
	if err != nil {
		return Report{}, err
	}
	return runPrepared(c, src, opts)
}

func runPrepared(c Case, src Source, opts Options) (Report, error) {
	if opts.MaxCost < 0 {
		return Report{}, fmt.Errorf("eval: max cost must be >= 0")
	}
	if opts.MaxCost > 0 && !opts.Pricing.Priced() {
		return Report{}, fmt.Errorf("eval: max cost requires input/output pricing")
	}
	now := time.Now
	if opts.Now != nil {
		now = opts.Now
	}
	mode := "dry_run"
	if opts.Live {
		mode = "live"
	}
	report := Report{
		SchemaVersion: reportSchemaVersion,
		CaseID:        c.ID,
		Title:         c.Title,
		Description:   c.Description,
		Mode:          mode,
		GeneratedAt:   now().UTC(),
		Source:        src.Info,
		Pricing:       opts.Pricing.Normalized(),
		CostLimit:     opts.MaxCost,
		Stops:         []StopReport{},
	}

	stops, err := filterStops(c.StopPoints, opts.StopNames)
	if err != nil {
		return Report{}, err
	}
	spent := 0.0
	for _, stop := range stops {
		tiers := opts.Tiers
		if len(tiers) == 0 {
			tiers = []int{stopContextChars(c, stop, src.Config)}
		}
		for _, tier := range tiers {
			if tier <= 0 {
				return Report{}, fmt.Errorf("eval: stop %q has non-positive context budget %d", stop.Name, tier)
			}
			stopReport, warnings := runStop(c, src, stop, tier, opts, spent)
			report.Stops = append(report.Stops, stopReport)
			report.Warnings = append(report.Warnings, warnings...)
			if stopReport.LiveSkipped != "" {
				report.CostLimitHit = true
			}
			if opts.Live && stopReport.Live != nil && stopReport.Live.Error == "" {
				spent = round6(spent + stopReport.Cost.TotalCost)
				if opts.MaxCost > 0 && spent > opts.MaxCost {
					if !report.CostLimitHit {
						report.Warnings = append(report.Warnings, fmt.Sprintf(
							"%s: actual/estimated spend %.6f %s exceeds max cost %.6f",
							stopReport.Name, spent, report.Pricing.Currency, opts.MaxCost))
					}
					report.CostLimitHit = true
				}
			}
		}
	}

	report.Totals = summarize(report)
	if !opts.Live && opts.MaxCost > 0 && report.Totals.TotalCost > opts.MaxCost {
		report.Warnings = append(report.Warnings, fmt.Sprintf(
			"dry-run projected cost %.6f %s exceeds max cost %.6f; no calls were made",
			report.Totals.TotalCost, report.Totals.Currency, opts.MaxCost))
	}
	return report, nil
}

func filterStops(all []StopPoint, names []string) ([]StopPoint, error) {
	if len(names) == 0 {
		return all, nil
	}
	wanted := make(map[string]struct{}, len(names))
	for _, name := range names {
		wanted[strings.TrimSpace(name)] = struct{}{}
	}
	filtered := make([]StopPoint, 0, len(names))
	for _, stop := range all {
		if _, ok := wanted[stop.Name]; ok {
			filtered = append(filtered, stop)
		}
	}
	if len(filtered) == 0 {
		return nil, fmt.Errorf("eval: no stopPoint matched %v", names)
	}
	return filtered, nil
}

func stopContextChars(c Case, stop StopPoint, config store.Config) int {
	if stop.ContextChars > 0 {
		return stop.ContextChars
	}
	if c.DefaultContextChars > 0 {
		return c.DefaultContextChars
	}
	if config.ContextChars > 0 {
		return config.ContextChars
	}
	return novel.DefaultContextChars
}

func stopTargetChars(c Case, stop StopPoint) int {
	if stop.TargetChars > 0 {
		return stop.TargetChars
	}
	if c.DefaultTargetChars > 0 {
		return c.DefaultTargetChars
	}
	return defaultTargetChars
}

func runStop(c Case, src Source, stop StopPoint, contextChars int, opts Options, spentCost float64) (StopReport, []string) {
	budget := novel.MemoryBudgetChars(contextChars)
	name := stop.Name
	if len(opts.Tiers) > 1 {
		name = fmt.Sprintf("%s@ctx%d", stop.Name, contextChars)
	}
	targetChars := stopTargetChars(c, stop)

	limit := stop.AfterChapter
	if limit <= 0 || limit > len(src.Chapters) {
		limit = len(src.Chapters)
	}
	chapters := src.Chapters[:limit]
	recent, startIndex := agent.ContinuationWindowFor(chapters, contextChars, stop.UseRecall)

	useMemory := boolValue(stop.UseMemory, true)
	memory := novel.ContinuationMemory{}
	var warnings []string
	if useMemory {
		retrieved, err := src.Store.RetrieveContinuationMemory(src.Knowledge, recent, startIndex, budget)
		memory = retrieved
		memory.BudgetChars = budget
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("%s: memory retrieval failed, continuing without memory: %v", name, err))
		}
	}
	var memoryPtr *novel.ContinuationMemory
	if useMemory {
		memoryPtr = &memory
	}

	prompt := agent.Build(agent.Input{
		Title:        src.Info.Title,
		Knowledge:    src.Knowledge,
		Chapters:     chapters,
		Direction:    stop.Direction,
		TargetChars:  targetChars,
		ContextChars: contextChars,
		Memory:       memoryPtr,
		UseRecall:    stop.UseRecall,
	})

	sections := make(map[string]int, len(prompt.Sections))
	for key, value := range prompt.Sections {
		sections[key] = value
	}
	injectedChars := utf8.RuneCountInString(prompt.MemorySection)
	inputTokens := EstimateTokens(prompt.System).Total + EstimateTokens(prompt.User).Total
	outputTokens := EstimateOutputTokens(targetChars)
	cost := EstimateCost(inputTokens, outputTokens, opts.Pricing)

	report := StopReport{
		Name:         name,
		AfterChapter: limit,
		ContextChars: contextChars,
		TargetChars:  targetChars,
		Direction:    stop.Direction,
		UseMemory:    useMemory,
		Notes:        stop.Notes,
		Memory: MemoryMetrics{
			Enabled:            useMemory,
			BudgetChars:        budget,
			Records:            len(memory.Records),
			Seeds:              len(memory.Seeds),
			DroppedByBudget:    memory.DroppedByBudget,
			SkippedReview:      memory.SkippedReview,
			SkippedAmbiguous:   memory.SkippedAmbiguous,
			RetrievalUsedChars: memory.UsedChars,
			InjectedChars:      injectedChars,
			DisabledReason:     memory.DisabledReason,
			Revision:           memory.MemoryRevision,
		},
		Recall: recallMetrics(stop.UseRecall, len(chapters), contextChars, prompt),
		Prompt: PromptMetrics{
			SystemChars:     utf8.RuneCountInString(prompt.System),
			UserChars:       utf8.RuneCountInString(prompt.User),
			TotalChars:      prompt.TotalChars,
			LimitChars:      contextChars,
			ManuscriptChars: prompt.ContextChars,
			Truncated:       prompt.Truncated,
			OverBudget:      prompt.OverBudget,
			UserBudgetPct:   round6(float64(utf8.RuneCountInString(prompt.User)) / float64(contextChars) * 100),
			Sections:        sections,
			InputTokens:     inputTokens,
		},
		Cost: cost,
	}

	if prompt.OverBudget {
		warnings = append(warnings, fmt.Sprintf("%s: prompt exceeds budget after shrink (%d > %d runes)",
			name, utf8.RuneCountInString(prompt.User), contextChars))
	}
	if useMemory && (memory.DisabledReason == novel.MemoryDisabledSafeState || memory.DisabledReason == novel.MemoryDisabledReadError) {
		warnings = append(warnings, fmt.Sprintf("%s: memory disabled (%s)", name, memory.DisabledReason))
	}
	// 显式要求召回却没注入，说明该停点的 R4 对照无效（预算被裁剪或无命中）。
	if boolValue(stop.UseRecall, false) && prompt.RecallSection == "" {
		warnings = append(warnings, fmt.Sprintf("%s: recall requested but not injected (chapters %d, recallBudget %d)",
			name, len(chapters), prompt.RecallBudget))
	}

	if opts.Live {
		if opts.MaxCost > 0 {
			remaining := opts.MaxCost - spentCost
			if remaining < 0 {
				remaining = 0
			}
			if cost.TotalCost > remaining {
				report.LiveSkipped = fmt.Sprintf("cost limit: projected %.6f %s exceeds remaining %.6f %s",
					cost.TotalCost, cost.Currency, remaining, cost.Currency)
				warnings = append(warnings, fmt.Sprintf("%s: live call skipped (%s)", name, report.LiveSkipped))
				report.Checklist = buildChecklist(stop, nil)
				return report, warnings
			}
		}
		live := callLive(prompt, targetChars, opts)
		report.Live = &live
		if live.Error != "" {
			warnings = append(warnings, fmt.Sprintf("%s: live call failed: %s", name, live.Error))
		} else {
			cost = EstimateCost(inputTokens, live.OutputTokens, opts.Pricing)
			report.Cost = cost
			if live.Truncated {
				warnings = append(warnings, fmt.Sprintf("%s: output truncated (finish_reason=%q)", name, live.FinishReason))
			}
		}
	}

	report.Checklist = buildChecklist(stop, report.Live)
	return report, warnings
}

func callLive(prompt agent.Prompt, targetChars int, opts Options) LiveReport {
	if opts.Client == nil {
		return LiveReport{Error: "live mode requires an llm client"}
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	temperature := opts.Temperature
	if temperature <= 0 {
		temperature = defaultTemperature
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var content strings.Builder
	started := time.Now()
	// 与 /api/continue 同走流式路径，输出长度与 finish_reason 更具可比性。
	result, err := opts.Client.StreamChat(ctx, []llm.Message{
		{Role: "system", Content: prompt.System},
		{Role: "user", Content: prompt.User},
	}, temperature, func(delta string) error {
		content.WriteString(delta)
		return nil
	})
	latency := time.Since(started)
	if err != nil {
		return LiveReport{LatencyMS: latency.Milliseconds(), Error: err.Error()}
	}
	output := content.String()
	live := LiveReport{
		FinishReason: result.FinishReason,
		Truncated:    llm.IsTruncated(result.FinishReason),
		LatencyMS:    latency.Milliseconds(),
		OutputChars:  novel.CountChars(output),
		OutputTokens: EstimateTokens(output).Total,
		Output:       output,
	}
	if targetChars > 0 && live.OutputChars == 0 {
		live.Error = "empty output"
	}
	return live
}

func buildChecklist(stop StopPoint, live *LiveReport) []ChecklistItem {
	items := make([]ChecklistItem, 0, len(stop.MustLand)+len(stop.MustNot))
	add := func(kind, text string) {
		text = strings.TrimSpace(text)
		if text == "" {
			return
		}
		item := ChecklistItem{Kind: kind, Text: text, Hint: "unjudged"}
		if live != nil && live.Error == "" {
			contains := strings.Contains(live.Output, text)
			switch kind {
			case "must_land":
				if contains {
					item.Hint = "observed"
				} else {
					item.Hint = "not_observed"
				}
			case "must_not":
				if contains {
					item.Hint = "flagged"
				} else {
					item.Hint = "clear"
				}
			}
		}
		items = append(items, item)
	}
	for _, text := range stop.MustLand {
		add("must_land", text)
	}
	for _, text := range stop.MustNot {
		add("must_not", text)
	}
	return items
}

// recallMetrics 把 prompt 侧的 R4 结果整理成停点指标。
func recallMetrics(useRecall *bool, chapterCount, contextChars int, prompt agent.Prompt) RecallMetrics {
	requested := "auto"
	if useRecall != nil {
		requested = "off"
		if *useRecall {
			requested = "on"
		}
	}
	chapters := make([]int, len(prompt.RecallChapters))
	copy(chapters, prompt.RecallChapters)
	return RecallMetrics{
		Requested:     requested,
		Enabled:       prompt.RecallSection != "",
		AutoEligible:  novel.RecallEnabled(chapterCount, contextChars),
		BudgetChars:   prompt.RecallBudget,
		InjectedChars: utf8.RuneCountInString(prompt.RecallSection),
		Hits:          len(prompt.RecallChapters),
		Chapters:      chapters,
	}
}

func summarize(report Report) Totals {
	totals := Totals{Stops: len(report.Stops), Currency: report.Pricing.Currency, Priced: report.Pricing.Priced()}
	for _, stop := range report.Stops {
		totals.InputTokens += stop.Prompt.InputTokens
		totals.OutputTokens += stop.Cost.OutputTokens
		if totals.Priced {
			totals.TotalCost = round6(totals.TotalCost + stop.Cost.TotalCost)
		}
		if stop.Prompt.UserBudgetPct > totals.MaxUserBudgetPct {
			totals.MaxUserBudgetPct = stop.Prompt.UserBudgetPct
		}
		if stop.Memory.InjectedChars > 0 {
			totals.MemoryInjectedStops++
		}
		if stop.Recall.InjectedChars > 0 {
			totals.RecallInjectedStops++
		}
		if stop.Prompt.OverBudget {
			totals.OverBudgetStops++
		}
		if stop.LiveSkipped != "" {
			totals.LiveSkippedStops++
		}
	}
	return totals
}

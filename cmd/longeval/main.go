// Command longeval 跑长篇续写停点的预算/成本回归。
//
// 默认 dry-run：只组装 prompt，不调用模型，可在无模型通道时复现
// 「每个停点注入多少记忆、prompt 多长、预计多少 token/费用」。
// -live 时额外调用 OpenAI 兼容接口，并把输出与三态覆盖清单写入报告。
//
// 用法：
//
//	go run ./cmd/longeval -case eval/cases/longform-synthetic.json \
//	    -pricing eval/pricing.example.json -tiers 4000,12000 -out report.json
//
// 实时调用：
//
//	OPENAI_API_KEY=... go run ./cmd/longeval -case ... -live \
//	    -base-url https://api.example.com/v1 -model <model>
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"novelagent/internal/eval"
	"novelagent/internal/llm"
)

func main() {
	casePath := flag.String("case", "", "回归用例 JSON 路径（必填）")
	pricingPath := flag.String("pricing", "", "定价 JSON（每百万 token 价格；缺省则只统计 token，不估算费用）")
	tiersText := flag.String("tiers", "", "逗号分隔的 ContextChars 覆盖档位，如 4000,12000,30000")
	stopsText := flag.String("stops", "", "逗号分隔的停点名过滤")
	outPath := flag.String("out", "", "报告输出路径（缺省打印 JSON 到 stdout）")
	live := flag.Bool("live", false, "调用真实模型（需要 -base-url/-model 与 OPENAI_API_KEY）")
	baseURL := flag.String("base-url", "", "OpenAI 兼容接口地址（-live 必填）")
	model := flag.String("model", "", "模型名称（-live 必填）")
	timeout := flag.Duration("timeout", 3*time.Minute, "单次模型调用超时")
	temperature := flag.Float64("temperature", 0.8, "采样温度")
	maxCost := flag.Float64("max-cost", 0, "live 成本上限（币种同 -pricing；0 不限。超限跳过剩余 live 调用并退出码 2）")
	strictBudget := flag.Bool("strict-budget", false, "任一停点 prompt 收缩后仍超 ContextChars 时退出码 3")
	tempParent := flag.String("temp", "", "隔离数据目录的父目录（缺省系统临时目录）")
	keepTemp := flag.Bool("keep", false, "保留隔离数据目录（打印路径）")
	flag.Parse()

	if strings.TrimSpace(*casePath) == "" {
		fail("必须指定 -case")
	}
	c, err := eval.LoadCase(*casePath)
	if err != nil {
		fail(err.Error())
	}
	pricing, err := loadPricing(*pricingPath)
	if err != nil {
		fail(err.Error())
	}
	tiers, err := parseTiers(*tiersText)
	if err != nil {
		fail(err.Error())
	}
	stopNames := splitCSV(*stopsText)

	opts := eval.Options{
		Pricing:     pricing,
		Tiers:       tiers,
		StopNames:   stopNames,
		Live:        *live,
		Temperature: *temperature,
		Timeout:     *timeout,
		MaxCost:     *maxCost,
	}
	if *live {
		if strings.TrimSpace(*baseURL) == "" || strings.TrimSpace(*model) == "" {
			fail("-live 需要同时指定 -base-url 与 -model")
		}
		opts.Client = llm.NewClient(*baseURL, os.Getenv("OPENAI_API_KEY"), *model)
	}

	workDir, err := os.MkdirTemp(*tempParent, "longeval-*")
	if err != nil {
		fail("创建临时目录失败: " + err.Error())
	}
	if *keepTemp {
		fmt.Fprintf(os.Stderr, "isolated data dir: %s\n", workDir)
	} else {
		defer os.RemoveAll(workDir)
	}

	caseDir, err := filepath.Abs(filepath.Dir(*casePath))
	if err != nil {
		fail(err.Error())
	}
	report, err := eval.Run(c, caseDir, workDir, opts)
	if err != nil {
		fail(err.Error())
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		fail("编码报告失败: " + err.Error())
	}
	if *outPath != "" {
		if err := os.WriteFile(*outPath, append(data, '\n'), 0o644); err != nil {
			fail("写报告失败: " + err.Error())
		}
		printSummary(report, *outPath)
	} else {
		fmt.Println(string(data))
	}
	if report.CostLimitHit {
		fmt.Fprintln(os.Stderr, "longeval: cost limit hit; remaining live calls skipped")
		os.Exit(2)
	}
	if *strictBudget && report.Totals.OverBudgetStops > 0 {
		fmt.Fprintf(os.Stderr, "longeval: %d stop(s) exceed ContextChars after shrink\n", report.Totals.OverBudgetStops)
		os.Exit(3)
	}
}

func fail(message string) {
	fmt.Fprintln(os.Stderr, "longeval:", message)
	os.Exit(1)
}

func loadPricing(path string) (eval.Pricing, error) {
	if strings.TrimSpace(path) == "" {
		return eval.Pricing{}, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return eval.Pricing{}, fmt.Errorf("读取定价失败: %w", err)
	}
	var pricing eval.Pricing
	if err := json.Unmarshal(data, &pricing); err != nil {
		return eval.Pricing{}, fmt.Errorf("解析定价失败: %w", err)
	}
	return pricing.Normalized(), nil
}

func parseTiers(value string) ([]int, error) {
	parts := splitCSV(value)
	tiers := make([]int, 0, len(parts))
	for _, part := range parts {
		tier, err := strconv.Atoi(part)
		if err != nil || tier <= 0 {
			return nil, fmt.Errorf("无效的 -tiers 值 %q（应为正整数）", part)
		}
		tiers = append(tiers, tier)
	}
	return tiers, nil
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func printSummary(report eval.Report, outPath string) {
	fmt.Printf("case=%s mode=%s source=%s chapters=%d chars=%d\n",
		report.CaseID, report.Mode, report.Source.Kind, report.Source.Chapters, report.Source.Chars)
	for _, stop := range report.Stops {
		line := fmt.Sprintf("%-24s chapter=%-4d input=%-6d user=%d/%d (%.1f%%) memory=%d/%d records=%d recall=%s %d/%d hits=%d %v",
			stop.Name, stop.AfterChapter, stop.Prompt.InputTokens,
			stop.Prompt.UserChars, stop.Prompt.LimitChars, stop.Prompt.UserBudgetPct,
			stop.Memory.InjectedChars, stop.Memory.BudgetChars, stop.Memory.Records,
			stop.Recall.Requested, stop.Recall.InjectedChars, stop.Recall.BudgetChars,
			stop.Recall.Hits, stop.Recall.Chapters)
		if report.Totals.Priced {
			line += fmt.Sprintf(" cost=%.6f %s", stop.Cost.TotalCost, stop.Cost.Currency)
		}
		if stop.LiveSkipped != "" {
			line += " live=SKIPPED"
		} else if stop.Live != nil {
			if stop.Live.Error != "" {
				line += " live=ERROR"
			} else {
				line += fmt.Sprintf(" live=%d chars/%dms", stop.Live.OutputChars, stop.Live.LatencyMS)
			}
		}
		fmt.Println(line)
	}
	if report.CostLimit > 0 {
		fmt.Printf("costLimit=%.6f %s hit=%t\n", report.CostLimit, report.Totals.Currency, report.CostLimitHit)
	}
	if report.Totals.OverBudgetStops > 0 {
		fmt.Printf("overBudgetStops=%d\n", report.Totals.OverBudgetStops)
	}
	if report.Totals.Priced {
		fmt.Printf("total: stops=%d inputTokens=%d outputTokens=%d cost=%.6f %s recallStops=%d/%d\n",
			report.Totals.Stops, report.Totals.InputTokens, report.Totals.OutputTokens,
			report.Totals.TotalCost, report.Totals.Currency,
			report.Totals.RecallInjectedStops, report.Totals.Stops)
	} else {
		fmt.Printf("total: stops=%d inputTokens=%d outputTokens=%d (unpriced) recallStops=%d/%d\n",
			report.Totals.Stops, report.Totals.InputTokens, report.Totals.OutputTokens,
			report.Totals.RecallInjectedStops, report.Totals.Stops)
	}
	for _, warning := range report.Warnings {
		fmt.Println("warning:", warning)
	}
	fmt.Println("report:", outPath)
}

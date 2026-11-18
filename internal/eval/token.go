package eval

import (
	"math"
	"strings"
	"unicode"
)

// TokenBreakdown 是启发式 token 估算的明细，不是上游计费真值。
// 口径（保守上界）：中日韩统一表意文字 1 rune ≈ 1 token；ASCII 字母/数字
// 每 4 rune ≈ 1 token；全角标点、符号各 1 token；连续空白合并为 1 token。
// 真实 tokenizer 对中文常见 0.6–1.2 token/字，本估算用于预算与成本对比，
// 不用于对账；实际费用以供应商账单为准。
type TokenBreakdown struct {
	Han        int `json:"han"`
	ASCIIRunes int `json:"asciiRunes"`
	Symbols    int `json:"symbols"`
	Whitespace int `json:"whitespaceRunes"`
	Total      int `json:"total"`
}

func EstimateTokens(text string) TokenBreakdown {
	var b TokenBreakdown
	asciiRun := 0
	spacePending := false
	flushASCII := func() {
		if asciiRun > 0 {
			b.Total += (asciiRun + 3) / 4
			asciiRun = 0
		}
	}
	flushSpace := func() {
		if spacePending {
			b.Total++
			spacePending = false
		}
	}
	for _, r := range text {
		if unicode.IsSpace(r) {
			flushASCII()
			b.Whitespace++
			spacePending = true
			continue
		}
		flushSpace()
		switch {
		case unicode.Is(unicode.Han, r):
			flushASCII()
			b.Han++
			b.Total++
		case r <= unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)):
			b.ASCIIRunes++
			asciiRun++
		default:
			flushASCII()
			b.Symbols++
			b.Total++
		}
	}
	flushASCII()
	flushSpace()
	return b
}

// EstimateOutputTokens 按目标中文字数给出输出 token 的保守估算。
func EstimateOutputTokens(chars int) int {
	if chars <= 0 {
		return 0
	}
	return chars
}

type Pricing struct {
	InputPerMillion  float64 `json:"inputPerMillion"`
	OutputPerMillion float64 `json:"outputPerMillion"`
	Currency         string  `json:"currency,omitempty"`
	Note             string  `json:"note,omitempty"`
}

func (p Pricing) Normalized() Pricing {
	if strings.TrimSpace(p.Currency) == "" {
		p.Currency = "USD"
	}
	return p
}

func (p Pricing) Priced() bool {
	return p.InputPerMillion > 0 || p.OutputPerMillion > 0
}

type CostEstimate struct {
	InputTokens  int     `json:"inputTokens"`
	OutputTokens int     `json:"outputTokens"`
	InputCost    float64 `json:"inputCost"`
	OutputCost   float64 `json:"outputCost"`
	TotalCost    float64 `json:"totalCost"`
	Currency     string  `json:"currency"`
	Priced       bool    `json:"priced"`
}

func EstimateCost(inputTokens, outputTokens int, pricing Pricing) CostEstimate {
	pricing = pricing.Normalized()
	cost := CostEstimate{
		InputTokens:  max(inputTokens, 0),
		OutputTokens: max(outputTokens, 0),
		Currency:     pricing.Currency,
		Priced:       pricing.Priced(),
	}
	if !cost.Priced {
		return cost
	}
	cost.InputCost = round6(float64(cost.InputTokens) * pricing.InputPerMillion / 1_000_000)
	cost.OutputCost = round6(float64(cost.OutputTokens) * pricing.OutputPerMillion / 1_000_000)
	cost.TotalCost = round6(cost.InputCost + cost.OutputCost)
	return cost
}

func round6(value float64) float64 {
	return math.Round(value*1_000_000) / 1_000_000
}

package eval

import "testing"

func TestEstimateTokens(t *testing.T) {
	cases := []struct {
		name       string
		text       string
		han        int
		ascii      int
		symbols    int
		whitespace int
		total      int
	}{
		{name: "empty", text: "", total: 0},
		{name: "han_and_punctuation", text: "林砚推门而入。", han: 6, symbols: 1, total: 7},
		{name: "ascii_words", text: "abc def", ascii: 6, whitespace: 1, total: 3},
		{name: "newline_run_collapses", text: "a\n\nb", ascii: 2, whitespace: 2, total: 3},
		{name: "mixed", text: "chapter 12：雨夜", ascii: 9, han: 2, symbols: 1, whitespace: 1, total: 7},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := EstimateTokens(tc.text)
			if got.Han != tc.han || got.ASCIIRunes != tc.ascii || got.Symbols != tc.symbols ||
				got.Whitespace != tc.whitespace || got.Total != tc.total {
				t.Fatalf("EstimateTokens(%q) = %+v, want han=%d ascii=%d symbols=%d whitespace=%d total=%d",
					tc.text, got, tc.han, tc.ascii, tc.symbols, tc.whitespace, tc.total)
			}
		})
	}
}

func TestEstimateCost(t *testing.T) {
	pricing := Pricing{InputPerMillion: 1, OutputPerMillion: 2}
	cost := EstimateCost(1_000_000, 500_000, pricing)
	if !cost.Priced {
		t.Fatal("Priced = false, want true")
	}
	if cost.InputCost != 1 || cost.OutputCost != 1 || cost.TotalCost != 2 {
		t.Fatalf("cost = %+v, want 1/1/2", cost)
	}
	if cost.Currency != "USD" {
		t.Errorf("currency = %q, want USD", cost.Currency)
	}

	unpriced := EstimateCost(1000, 500, Pricing{})
	if unpriced.Priced {
		t.Error("Priced = true for zero prices")
	}
	if unpriced.TotalCost != 0 {
		t.Errorf("unpriced total = %v, want 0", unpriced.TotalCost)
	}
}

package novel

import "testing"

func TestRecallBudgetChars(t *testing.T) {
	cases := []struct {
		contextChars int
		want         int
	}{
		{0, 800},
		{1600, 0},
		{2399, 0},
		{2400, 0},
		{3000, 0},
		{3199, 799},
		{3200, 800},
		{4000, 800},
		{8000, 800},
		{12000, 800},
		{30000, 800},
	}
	for _, tc := range cases {
		if got := RecallBudgetChars(tc.contextChars); got != tc.want {
			t.Errorf("RecallBudgetChars(%d) = %d, want %d", tc.contextChars, got, tc.want)
		}
	}
}

func TestRecallEnabled(t *testing.T) {
	cases := []struct {
		chapters     int
		contextChars int
		want         bool
	}{
		{19, 12000, false},
		{20, 12000, true},
		{200, 4000, true},
		{20, 3000, false},
		{20, 3199, true},
		{0, 12000, false},
	}
	for _, tc := range cases {
		if got := RecallEnabled(tc.chapters, tc.contextChars); got != tc.want {
			t.Errorf("RecallEnabled(%d,%d) = %t, want %t", tc.chapters, tc.contextChars, got, tc.want)
		}
	}
}

func TestRecallBudgetKeepsManuscriptFloor(t *testing.T) {
	for _, contextChars := range []int{3200, 4000, 8000, 12000, 30000} {
		recallBudget := RecallBudgetChars(contextChars)
		if recallBudget == 0 {
			t.Fatalf("ctx %d: recall budget unexpectedly 0", contextChars)
		}
		manuscript := contextChars - MemoryBudgetChars(contextChars) - recallBudget
		if manuscript < contextChars/2 {
			t.Errorf("ctx %d: manuscript %d < ctx/2 %d", contextChars, manuscript, contextChars/2)
		}
	}
}

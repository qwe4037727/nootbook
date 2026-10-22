//go:build acceptance_review

package novel

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestFunctionalMemoryScalarBoundaries(t *testing.T) {
	cases := []struct {
		name      string
		limit     int
		normalize func(string) string
	}{
		{"event.summary", 200, func(v string) string { x := StoryEvent{Summary: v}; x.Normalize(memNow); return x.Summary }},
		{"state.attribute", 40, func(v string) string { x := StoryState{Attribute: v}; x.Normalize(memNow); return x.Attribute }},
		{"state.value", 200, func(v string) string { x := StoryState{Value: v}; x.Normalize(memNow); return x.Value }},
		{"cognition.content", 200, func(v string) string { x := Cognition{Content: v}; x.Normalize(memNow); return x.Content }},
		{"thread.title", 60, func(v string) string { x := StoryThread{Title: v}; x.Normalize(memNow); return x.Title }},
		{"thread.question", 120, func(v string) string { x := StoryThread{Question: v}; x.Normalize(memNow); return x.Question }},
		{"thread.abandonReason", 200, func(v string) string { x := StoryThread{AbandonReason: v}; x.Normalize(memNow); return x.AbandonReason }},
		{"plan.title", 80, func(v string) string { x := StoryPlan{Title: v}; x.Normalize(memNow); return x.Title }},
		{"plan.objective", 300, func(v string) string { x := StoryPlan{Objective: v}; x.Normalize(memNow); return x.Objective }},
		{"candidate.name", 40, func(v string) string { x := EntityCandidate{Name: v}; x.Normalize(memNow); return x.Name }},
	}
	for _, tc := range cases {
		for _, char := range []string{"界", "🙂"} {
			for _, length := range []int{0, tc.limit - 1, tc.limit, tc.limit + 1} {
				t.Run(fmt.Sprintf("%s/%s/%d", tc.name, char, length), func(t *testing.T) {
					got := tc.normalize("  " + strings.Repeat(char, length) + "  ")
					if utf8.RuneCountInString(got) != min(length, tc.limit) || !utf8.ValidString(got) {
						t.Fatalf("rune length=%d want=%d", utf8.RuneCountInString(got), min(length, tc.limit))
					}
				})
			}
		}
	}
}

func TestFunctionalMemoryListBoundaries(t *testing.T) {
	cases := []struct {
		name      string
		limit     int
		normalize func([]string) []string
	}{
		{"thread.knownClues", 8, func(v []string) []string { x := StoryThread{KnownClues: v}; x.Normalize(memNow); return x.KnownClues }},
		{"thread.resolutionCriteria", 6, func(v []string) []string {
			x := StoryThread{ResolutionCriteria: v}
			x.Normalize(memNow)
			return x.ResolutionCriteria
		}},
		{"plan.constraints", 8, func(v []string) []string { x := StoryPlan{Constraints: v}; x.Normalize(memNow); return x.Constraints }},
		{"plan.candidateBeats", 10, func(v []string) []string {
			x := StoryPlan{CandidateBeats: v}
			x.Normalize(memNow)
			return x.CandidateBeats
		}},
	}
	for _, tc := range cases {
		for _, count := range []int{0, tc.limit - 1, tc.limit, tc.limit + 1} {
			t.Run(fmt.Sprintf("%s/count/%d", tc.name, count), func(t *testing.T) {
				values := make([]string, count)
				for i := range values {
					values[i] = fmt.Sprintf("item-%d", i)
				}
				if got := tc.normalize(values); len(got) != min(count, tc.limit) {
					t.Fatalf("count=%d", len(got))
				}
			})
		}
		for _, length := range []int{99, 100, 101} {
			t.Run(fmt.Sprintf("%s/item/%d", tc.name, length), func(t *testing.T) {
				got := tc.normalize([]string{strings.Repeat("🙂", length)})
				if len(got) != 1 || utf8.RuneCountInString(got[0]) > 100 {
					t.Fatalf("item exceeds spec 100-rune cap: %d", utf8.RuneCountInString(got[0]))
				}
			})
		}
	}
}

func TestFunctionalMemoryMetadataMatrix(t *testing.T) {
	models := []struct {
		name     string
		validate func(MemoryMeta, string) error
	}{
		{"event", func(m MemoryMeta, status string) error {
			return ValidateStoryEvent(&StoryEvent{ID: "evt_t", MemoryMeta: m, Status: StoryEventStatus(status), EventType: "other", Summary: "x"})
		}},
		{"state", func(m MemoryMeta, status string) error {
			return ValidateStoryState(&StoryState{ID: "state_t", MemoryMeta: m, Status: StoryStateStatus(status), SubjectID: "c_t", Category: "general", Attribute: "profile", Value: "x"})
		}},
		{"cognition", func(m MemoryMeta, status string) error {
			return ValidateCognition(&Cognition{ID: "cog_t", MemoryMeta: m, Status: CognitionStatus(status), SubjectID: "c_t", Content: "x"})
		}},
		{"thread", func(m MemoryMeta, status string) error {
			return ValidateStoryThread(&StoryThread{ID: "thread_t", MemoryMeta: m, Status: StoryThreadStatus(status), Title: "x"})
		}},
		{"plan", func(m MemoryMeta, status string) error {
			return ValidateStoryPlanStruct(&StoryPlan{ID: "plan_t", MemoryMeta: m, Status: StoryPlanStatus(status), Level: "global", Title: "x", Flexibility: "high"})
		}},
	}
	validStatuses := []string{"proposed", "active", "known", "open", "proposed"}
	for i, model := range models {
		for _, origin := range []OriginType{OriginBackfill, OriginDeepExtract, OriginManual, OriginCommit, OriginReconcile, "unknown"} {
			for _, source := range []string{LastSourceAgentExtraction, LastSourceManualEdit, LastSourceBackfill, LastSourceReconcile, "unknown"} {
				t.Run(model.name+"/"+string(origin)+"/"+source, func(t *testing.T) {
					m := baseMeta()
					m.Origin.Type = origin
					m.LastSource = source
					err := model.validate(m, validStatuses[i])
					wantValid := origin != "unknown" && source != "unknown"
					if (err == nil) != wantValid {
						t.Fatalf("valid=%v err=%v", wantValid, err)
					}
					if model.validate(m, "unknown") == nil {
						t.Fatal("unknown status accepted")
					}
				})
			}
		}
	}
}

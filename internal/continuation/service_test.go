package continuation

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"novelagent/internal/novel"
	"novelagent/internal/store"
)

func newContinuationFixture(t *testing.T) (*store.Store, *Service, *int64) {
	t.Helper()
	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if _, err := st.ImportChapters("测试书", []novel.Chapter{
		{Title: "第一章", Content: "林越推开山门，风雪扑面而来。"},
		{Title: "第二章", Content: "他握紧长剑，沿着石阶向上走去。"},
	}); err != nil {
		t.Fatalf("ImportChapters: %v", err)
	}
	var calls int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		number := atomic.AddInt64(&calls, 1)
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		content := fmt.Sprintf("候选版本%d：风雪更急，林越继续前行。", number)
		payload := map[string]any{
			"choices": []map[string]any{{"message": map[string]any{"role": "assistant", "content": content}}},
			"usage":   map[string]any{"prompt_tokens": 12, "completion_tokens": 8, "total_tokens": 20},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(payload)
	}))
	t.Cleanup(server.Close)
	cfg := store.DefaultConfig()
	cfg.BaseURL = server.URL + "/v1"
	cfg.Model = "deepseek-chat"
	cfg.APIKey = "sk-test"
	if _, err := st.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	return st, NewService(st, nil), &calls
}

func TestGenerateCandidatesFromChapterOffset(t *testing.T) {
	st, service, calls := newContinuationFixture(t)
	project, err := st.LoadProject()
	if err != nil {
		t.Fatalf("LoadProject: %v", err)
	}
	chapterID := project.Chapters[0].ID
	result, err := service.Generate(context.Background(), Request{
		StartMode:   novel.ContinuationStartChapterOffset,
		ChapterID:   chapterID,
		StartOffset: 6,
		Direction:   "让风雪更大",
		TargetChars: 500,
		Candidates:  2,
		Temperature: 0.8,
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if result.Group.ID == "" || result.Group.StartMode != novel.ContinuationStartChapterOffset ||
		result.Group.StartIndex != 1 || result.Group.ChapterID != chapterID {
		t.Fatalf("group = %+v", result.Group)
	}
	if len(result.Candidates) != 2 || atomic.LoadInt64(calls) != 2 {
		t.Fatalf("candidates = %d calls = %d", len(result.Candidates), atomic.LoadInt64(calls))
	}
	seen := map[string]bool{}
	for _, candidate := range result.Candidates {
		if candidate.Status != "done" || candidate.Content == "" || candidate.TotalTokens != 20 {
			t.Fatalf("candidate = %+v", candidate)
		}
		seen[candidate.Content] = true
	}
	if len(seen) != 2 {
		t.Fatalf("candidate versions are identical: %+v", result.Candidates)
	}

	groups, err := st.ListContinuationGroups(10)
	if err != nil || len(groups) != 1 || groups[0].ID != result.Group.ID {
		t.Fatalf("groups = %+v err=%v", groups, err)
	}
	group, persisted, err := st.GetContinuationGroup(result.Group.ID)
	if err != nil || group.ID != result.Group.ID || len(persisted) != 2 {
		t.Fatalf("persisted = %+v / %+v err=%v", group, persisted, err)
	}

	unchanged, err := st.LoadProject()
	if err != nil || unchanged.Revision != project.Revision || len(unchanged.Chapters) != len(project.Chapters) {
		t.Fatalf("continuation mutated the book: %+v err=%v", unchanged, err)
	}
}

func TestGenerateValidatesStartPoint(t *testing.T) {
	_, service, _ := newContinuationFixture(t)
	if _, err := service.Generate(context.Background(), Request{
		StartMode: novel.ContinuationStartChapterOffset, ChapterID: "ch_missing", StartOffset: 1,
	}); err == nil {
		t.Fatal("expected invalid start error")
	}
	if _, err := service.Generate(context.Background(), Request{
		StartMode: novel.ContinuationStartChapterOffset, ChapterID: "ch_missing", StartOffset: 99999,
	}); err == nil {
		t.Fatal("expected offset error")
	}
}

func TestGenerateRequiresContentAndConfig(t *testing.T) {
	empty, err := store.New(t.TempDir())
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() { _ = empty.Close() })
	service := NewService(empty, nil)
	if _, err := service.Generate(context.Background(), Request{Direction: "x"}); err == nil {
		t.Fatal("expected config/no-content error")
	}
}

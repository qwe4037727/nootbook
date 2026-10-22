package analysis

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"novelagent/internal/llm"
	"novelagent/internal/novel"
	"novelagent/internal/store"
)

func TestManagerAnalyzesIncrementally(t *testing.T) {
	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	if _, err := st.ImportChapters("雨夜", []novel.Chapter{
		{Title: "初始", Content: "林砚在雨夜走进县衙。"},
		{Title: "重瞳", Content: "林砚的左肩中箭。"},
	}); err != nil {
		t.Fatalf("ImportChapters: %v", err)
	}

	var mu sync.Mutex
	calls := 0
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []llm.Message `json:"messages"`
			Stream   bool          `json:"stream"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode provider request: %v", err)
		}
		if body.Stream {
			t.Errorf("analysis should use non-streaming requests")
		}
		mu.Lock()
		calls++
		mu.Unlock()

		user := body.Messages[len(body.Messages)-1].Content
		var content string
		switch {
		case strings.Contains(user, "【本章】第 1 章"):
			content = `{"summary":"第一章摘要。","endsAt":"林砚走进县衙。","characters":[{"name":"林砚","role":"捕快","state":"在县衙值夜","confidence":0.9}],"world":[{"title":"县衙","category":"地点","content":"县城的衙门。","confidence":0.8}],"style":{"perspective":"第三人称限知","samples":["林砚在雨夜走进县衙。"]}}`
		case strings.Contains(user, "【本章】第 2 章"):
			content = `{"summary":"第二章摘要。","endsAt":"林砚左肩中箭。","characters":[{"name":"林砚","state":"左肩中箭","confidence":0.95}],"world":[],"style":{}}`
		case strings.Contains(user, "【本章】第 3 章"):
			content = `{"summary":"第三章摘要。","endsAt":"伤口包扎完毕。","characters":[{"name":"沈青禾","role":"医女","state":"替林砚包扎","confidence":0.85}],"world":[],"style":{}}`
		case strings.Contains(user, "【章节摘要】"):
			content = `{"overview":"林砚在县城追查旧案，左肩受伤。","style":{"perspective":"第三人称限知","samples":["林砚在雨夜走进县衙。"]}}`
		default:
			t.Errorf("unexpected analyst prompt:\n%s", user)
			content = `{}`
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]string{"role": "assistant", "content": content}},
			},
		})
	}))
	defer provider.Close()

	if _, err := st.SaveConfig(store.Config{
		BaseURL:      provider.URL + "/v1",
		Model:        "mock-model",
		ContextChars: 4000,
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	manager := NewManager(st, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if more := manager.step(context.Background()); more {
		t.Error("step reported more work after finishing the analysis")
	}

	project, err := st.LoadProject()
	if err != nil {
		t.Fatalf("LoadProject: %v", err)
	}
	if project.Analysis.Status != novel.AnalysisReady || project.Analysis.AnalyzedCount != 2 {
		t.Fatalf("analysis = %+v", project.Analysis)
	}
	knowledge, err := st.LoadKnowledge()
	if err != nil {
		t.Fatalf("LoadKnowledge: %v", err)
	}
	if knowledge.Overview != "林砚在县城追查旧案，左肩受伤。" {
		t.Errorf("overview = %q", knowledge.Overview)
	}
	if len(knowledge.Characters) != 1 || knowledge.Characters[0].State != "左肩中箭" {
		t.Errorf("characters = %+v", knowledge.Characters)
	}
	if len(knowledge.World) != 1 || knowledge.World[0].Title != "县衙" {
		t.Errorf("world = %+v", knowledge.World)
	}
	if len(knowledge.Summaries) != 2 || knowledge.Summaries[1].Summary != "第二章摘要。" {
		t.Errorf("summaries = %+v", knowledge.Summaries)
	}
	if knowledge.Style.Perspective != "第三人称限知" {
		t.Errorf("style = %+v", knowledge.Style)
	}
	mu.Lock()
	firstCalls := calls
	mu.Unlock()
	if firstCalls != 3 {
		t.Errorf("provider calls = %d, want 2 chapter analyses + 1 synthesis", firstCalls)
	}

	_, project, err = st.AppendChapter("余波", "沈青禾替林砚包扎。")
	if err != nil {
		t.Fatalf("AppendChapter: %v", err)
	}
	if project.Analysis.Status != novel.AnalysisPending || project.Analysis.AnalyzedCount != 2 {
		t.Fatalf("analysis after append = %+v", project.Analysis)
	}

	if more := manager.step(context.Background()); more {
		t.Error("step reported more work after the incremental analysis")
	}
	project, err = st.LoadProject()
	if err != nil {
		t.Fatalf("LoadProject: %v", err)
	}
	if project.Analysis.Status != novel.AnalysisReady || project.Analysis.AnalyzedCount != 3 {
		t.Fatalf("analysis after append = %+v", project.Analysis)
	}
	knowledge, err = st.LoadKnowledge()
	if err != nil {
		t.Fatalf("LoadKnowledge: %v", err)
	}
	if len(knowledge.Characters) != 2 || knowledge.Characters[0].Name != "林砚" {
		t.Errorf("characters after append = %+v", knowledge.Characters)
	}
	found := false
	for _, character := range knowledge.Characters {
		if character.Name == "沈青禾" {
			found = true
		}
	}
	if !found {
		t.Errorf("new character was not added: %+v", knowledge.Characters)
	}
	if len(knowledge.Summaries) != 3 {
		t.Errorf("summaries after append = %+v", knowledge.Summaries)
	}
	mu.Lock()
	totalCalls := calls
	mu.Unlock()
	if totalCalls != 5 {
		t.Errorf("provider calls = %d, want 3 chapter analyses + 2 syntheses", totalCalls)
	}
}

func TestManagerRecordsMissingConfig(t *testing.T) {
	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	if _, err := st.ImportChapters("雨夜", []novel.Chapter{
		{Title: "初始", Content: "雨落到青石板上。"},
	}); err != nil {
		t.Fatalf("ImportChapters: %v", err)
	}
	manager := NewManager(st, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if !manager.step(context.Background()) {
		t.Error("step should keep work queued after a configuration failure")
	}
	project, err := st.LoadProject()
	if err != nil {
		t.Fatalf("LoadProject: %v", err)
	}
	if project.Analysis.Status != novel.AnalysisError || !strings.Contains(project.Analysis.Message, "模型设置") {
		t.Errorf("analysis = %+v", project.Analysis)
	}
}

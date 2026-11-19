package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"novelagent/internal/novel"
	"novelagent/internal/store"
)

func TestGlobalCostAggregatesProjects(t *testing.T) {
	manager, router := newTestRouter(t)
	rt, err := manager.Runtime(DefaultProject)
	if err != nil {
		t.Fatalf("Runtime: %v", err)
	}
	if _, err := rt.Store.ImportChapters("书", []novel.Chapter{{Title: "一", Content: "正文。"}}); err != nil {
		t.Fatalf("ImportChapters: %v", err)
	}
	now := time.Now().UTC()
	if err := rt.Store.RecordAgentRun(store.AgentRunRecord{
		ID: "run_global", ProjectID: DefaultProject, AgentKey: "plot", AgentName: "A2",
		EventID: "ev", EventType: "chapter_saved", Permission: "observe", Status: "done",
		StartedAt: now, FinishedAt: now, PromptTokens: 7, CompletionTokens: 3, TotalTokens: 10,
	}); err != nil {
		t.Fatalf("RecordAgentRun: %v", err)
	}

	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	status, data := doJSON(t, server.Client(), server.URL, http.MethodGet, "/api/cost/global", "", nil)
	if status != http.StatusOK {
		t.Fatalf("global cost = %d: %s", status, data)
	}
	var summary struct {
		Projects []struct {
			ProjectID string `json:"projectId"`
		} `json:"projects"`
		Total struct {
			TotalTokens int64 `json:"totalTokens"`
		} `json:"total"`
	}
	if err := json.Unmarshal(data, &summary); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if summary.Total.TotalTokens != 10 || len(summary.Projects) != 1 || summary.Projects[0].ProjectID != DefaultProject {
		t.Fatalf("summary = %+v", summary)
	}
}

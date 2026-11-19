package server

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"novelagent/internal/store"
)

func TestAgentCenterAPIContract(t *testing.T) {
	h, st, _ := newReviewTestHandler(t)

	rec := serveJSON(t, h, http.MethodGet, "/api/agents/config", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("config status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var cfg struct {
		Agents []struct {
			Key    string `json:"key"`
			Level  string `json:"level"`
			Source string `json:"source"`
		} `json:"agents"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &cfg); err != nil || len(cfg.Agents) != 8 {
		t.Fatalf("agents = %+v, err = %v", cfg.Agents, err)
	}
	for _, agent := range cfg.Agents {
		if agent.Key == "" || agent.Level != "observe" {
			t.Errorf("default agent = %+v, want observe", agent)
		}
	}

	assertMemoryError(t, serveJSON(t, h, http.MethodPut, "/api/agents/config/fuse", map[string]any{"level": "root"}), http.StatusBadRequest, "invalid_level")
	assertMemoryError(t, serveJSON(t, h, http.MethodPut, "/api/agents/config/unknown", map[string]any{"level": "observe"}), http.StatusNotFound, "agent_not_found")
	rec = serveJSON(t, h, http.MethodPut, "/api/agents/config/fuse", map[string]any{"enabled": false, "level": "propose"})
	if rec.Code != http.StatusOK {
		t.Fatalf("put config = %d, body = %s", rec.Code, rec.Body.String())
	}
	saved, err := st.LoadAgentConfigs()
	if err != nil || saved["fuse"].Enabled || saved["fuse"].Level != "propose" {
		t.Errorf("saved config = %+v, err = %v", saved, err)
	}

	now := time.Now().UTC()
	if err := st.RecordAgentRun(store.AgentRunRecord{
		ID: "run_api", ProjectID: "p", AgentKey: "continuity", AgentName: "A1", EventID: "ev", EventType: "chapter_saved",
		Permission: "observe", Status: "done", StartedAt: now, FinishedAt: now, DurationMs: 12,
		PromptTokens: 4, CompletionTokens: 2, TotalTokens: 6,
	}); err != nil {
		t.Fatalf("record run: %v", err)
	}
	rec = serveJSON(t, h, http.MethodGet, "/api/agents/runs?limit=5", nil)
	if rec.Code != http.StatusOK || !json.Valid(rec.Body.Bytes()) {
		t.Fatalf("runs = %d %s", rec.Code, rec.Body.String())
	}
	rec = serveJSON(t, h, http.MethodGet, "/api/agents/stats?days=7", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("stats = %d %s", rec.Code, rec.Body.String())
	}

	report := store.AgentReportRecord{
		ID: "ar_api", AgentKey: "continuity", AgentName: "A1", ChapterID: "ch_1", ChapterIndex: 1,
		Severity: "warning", Summary: "发现冲突", Findings: json.RawMessage(`[{"type":"x","severity":"warning","detail":"d"}]`),
	}
	if err := st.SaveAgentReport(report); err != nil {
		t.Fatalf("save report: %v", err)
	}
	assertMemoryError(t, serveJSON(t, h, http.MethodGet, "/api/agents/reports/ar_missing", nil), http.StatusNotFound, "report_not_found")
	rec = serveJSON(t, h, http.MethodGet, "/api/agents/reports?agent=continuity&limit=5", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("reports = %d %s", rec.Code, rec.Body.String())
	}

	if _, err := st.SaveForeshadows([]store.ForeshadowRecord{{
		ID: "fs_api", ChapterID: "ch_1", ChapterIndex: 1, Title: "玉符", Kind: "object", Quote: "半枚玉符", Status: "candidate",
	}}); err != nil {
		t.Fatalf("save foreshadow: %v", err)
	}
	rec = serveJSON(t, h, http.MethodGet, "/api/foreshadows", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("foreshadows = %d %s", rec.Code, rec.Body.String())
	}
	assertMemoryError(t, serveJSON(t, h, http.MethodPatch, "/api/foreshadows/fs_api", map[string]string{"status": "bogus"}), http.StatusBadRequest, "invalid_status")
	assertMemoryError(t, serveJSON(t, h, http.MethodPatch, "/api/foreshadows/fs_missing", map[string]string{"status": "confirmed"}), http.StatusNotFound, "foreshadow_not_found")
	if rec := serveJSON(t, h, http.MethodPatch, "/api/foreshadows/fs_api", map[string]string{"status": "confirmed"}); rec.Code != http.StatusOK {
		t.Errorf("confirm foreshadow = %d %s", rec.Code, rec.Body.String())
	}

	proposal, _, err := st.SaveAgentProposal(store.AgentProposalRecord{
		AgentKey: "memory", Kind: "merge_state", TargetID: "state_1", Summary: "合并", Risk: "low", Payload: json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatalf("save proposal: %v", err)
	}
	rec = serveJSON(t, h, http.MethodGet, "/api/agents/proposals", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("proposals = %d %s", rec.Code, rec.Body.String())
	}
	assertMemoryError(t, serveJSON(t, h, http.MethodPatch, "/api/agents/proposals/"+proposal.ID, map[string]string{"status": "bogus"}), http.StatusBadRequest, "invalid_status")
	if rec := serveJSON(t, h, http.MethodPatch, "/api/agents/proposals/"+proposal.ID, map[string]string{"status": "accepted"}); rec.Code != http.StatusOK {
		t.Errorf("accept proposal = %d %s", rec.Code, rec.Body.String())
	}
}

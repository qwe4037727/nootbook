package agents

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"novelagent/internal/agentruntime"
	"novelagent/internal/llm"
	"novelagent/internal/novel"
	"novelagent/internal/store"
)

func newAgentFixture(t *testing.T) (*store.Store, novel.Project) {
	t.Helper()
	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	project, err := st.ImportChapters("测试书", []novel.Chapter{{
		Title:   "第一章",
		Content: "林越推门而入，雾城的冷风灌了进来。他似乎听见了什么，却不知道声音从哪里来。",
	}})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	knowledge := novel.NewKnowledge()
	knowledge.Characters = []novel.Character{{
		ID: "c_lin", Name: "林越", Role: "主角", Confidence: 0.3, LastSeen: 1,
	}}
	knowledge.World = []novel.WorldEntry{{
		ID: "w_city", Title: "雾城", Category: "地点", Content: "终年大雾。", Confidence: 0.3, LastSeen: 1,
	}}
	if _, _, err := st.CommitAnalysis(project.Revision, len(project.Chapters), knowledge); err != nil {
		t.Fatalf("commit analysis: %v", err)
	}
	return st, project
}

func chapterEvent(project novel.Project) agentruntime.Event {
	meta := project.Chapters[0]
	return agentruntime.Event{
		ID:        "ev_test",
		Type:      string(store.ChapterSaved),
		ChapterID: meta.ID,
		Title:     meta.Title,
		Index:     meta.Index,
		CharCount: meta.CharCount,
	}
}

func TestContinuityAgentReportsConflictsWithEvidence(t *testing.T) {
	st, project := newAgentFixture(t)
	before, err := st.LoadChapter(project.Chapters[0].ID)
	if err != nil {
		t.Fatalf("load chapter: %v", err)
	}
	report, err := (&ContinuityAgent{}).Run(context.Background(), chapterEvent(project), agentruntime.Input{Store: st, Permission: "observe"})
	if err != nil {
		t.Fatalf("run continuity: %v", err)
	}
	if len(report.Findings) < 2 {
		t.Fatalf("findings = %+v, want low-confidence character and world entry", report.Findings)
	}
	foundEvidence := false
	for _, finding := range report.Findings {
		if finding.ChapterID != project.Chapters[0].ID {
			t.Errorf("finding missing chapter evidence: %+v", finding)
		}
		if finding.Quote != "" {
			foundEvidence = true
		}
	}
	if !foundEvidence {
		t.Errorf("no finding carried a quote: %+v", report.Findings)
	}
	proposals, err := st.ListAgentProposals("pending", 20)
	if err != nil || len(proposals) == 0 {
		t.Errorf("proposals = %+v, err = %v", proposals, err)
	}
	after, err := st.LoadChapter(project.Chapters[0].ID)
	if err != nil || after.Content != before.Content {
		t.Errorf("agent modified chapter: before=%q after=%q err=%v", before.Content, after.Content, err)
	}
	knowledge, err := st.LoadKnowledge()
	if err != nil || len(knowledge.Characters) != 1 || knowledge.Characters[0].Confidence != 0.3 {
		t.Errorf("agent modified knowledge: %+v, err = %v", knowledge, err)
	}
}

func TestFuseAgentExtractsLedgerAndFeedsInjection(t *testing.T) {
	st, project := newAgentFixture(t)
	report, err := (&FuseAgent{}).Run(context.Background(), chapterEvent(project), agentruntime.Input{Store: st})
	if err != nil {
		t.Fatalf("run fuse: %v", err)
	}
	if !strings.Contains(report.Summary, "伏笔") {
		t.Errorf("summary = %q", report.Summary)
	}
	ledger, err := st.ListForeshadows("candidate", 20)
	if err != nil || len(ledger) == 0 {
		t.Fatalf("ledger = %+v, err = %v", ledger, err)
	}
	if ledger[0].ChapterID != project.Chapters[0].ID || ledger[0].Quote == "" {
		t.Errorf("ledger evidence = %+v", ledger[0])
	}
	knowledge, _ := st.LoadKnowledge()
	memory, err := st.RetrieveContinuationMemory(knowledge, "林越走进雾城。", 2, 8000)
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	found := false
	for _, record := range memory.Records {
		if record.Kind == novel.MemoryKindFuse && strings.Contains(record.Text, "第 1 章") {
			found = true
		}
	}
	if !found {
		t.Errorf("foreshadow not injected: %+v", memory.Records)
	}
}

func TestMemoryAgentProducesHealthReportWithoutDeleting(t *testing.T) {
	st, project := newAgentFixture(t)
	report, err := (&MemoryAgent{}).Run(context.Background(), chapterEvent(project), agentruntime.Input{Store: st})
	if err != nil {
		t.Fatalf("run memory agent: %v", err)
	}
	if report.Severity != "warning" {
		t.Errorf("severity = %q, want warning for low-confidence KB", report.Severity)
	}
	found := false
	for _, finding := range report.Findings {
		if finding.Type == "kb_low_confidence" {
			found = true
		}
	}
	if !found {
		t.Errorf("findings = %+v", report.Findings)
	}
	knowledge, err := st.LoadKnowledge()
	if err != nil || len(knowledge.Characters) != 1 || len(knowledge.World) != 1 {
		t.Errorf("knowledge mutated: %+v, err = %v", knowledge, err)
	}
}

func TestContinuityAgentRecordsLLMTokens(t *testing.T) {
	var gotPath string
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if r.URL.Path != "/chat/completions" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"message":       map[string]string{"role": "assistant", "content": `[{"severity":"warning","detail":"记忆称雾城终年大雾，正文未交代天气","quote":"林越推门而入"}]`},
				"finish_reason": "stop",
			}},
			"usage": map[string]int{"prompt_tokens": 12, "completion_tokens": 3, "total_tokens": 15},
		})
	}))
	defer provider.Close()

	st, project := newAgentFixture(t)
	if _, err := st.SaveConfig(store.Config{BaseURL: provider.URL, Model: "fake-model", APIKey: "test-key"}); err != nil {
		t.Fatalf("save config: %v", err)
	}
	client := llm.NewClient(provider.URL, "test-key", "fake-model")
	report, err := (&ContinuityAgent{}).Run(context.Background(), chapterEvent(project), agentruntime.Input{
		Store: st, Permission: "observe", Client: client, Model: "fake-model", Configured: true,
	})
	if err != nil {
		t.Fatalf("run continuity: %v", err)
	}
	if gotPath != "/chat/completions" {
		t.Errorf("llm path = %q", gotPath)
	}
	if report.PromptTokens != 12 || report.CompletionTokens != 3 || report.Tokens != 15 {
		t.Errorf("tokens = %d/%d/%d", report.PromptTokens, report.CompletionTokens, report.Tokens)
	}
	llmFinding := false
	for _, finding := range report.Findings {
		if finding.Type == "llm_conflict" && finding.Quote == "林越推门而入" {
			llmFinding = true
		}
	}
	if !llmFinding {
		t.Errorf("llm finding missing: %+v", report.Findings)
	}
}

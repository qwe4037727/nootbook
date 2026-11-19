package store

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"novelagent/internal/novel"
)

func TestAgentRunLogAndStats(t *testing.T) {
	st, dir := newTestStore(t)
	now := time.Now().UTC()
	records := []AgentRunRecord{
		{ID: "run_1", ProjectID: "p1", AgentKey: "continuity", AgentName: "A1", EventID: "ev1", EventType: "chapter_saved",
			Permission: "observe", Status: "done", StartedAt: now.Add(-time.Hour), FinishedAt: now.Add(-time.Hour + time.Second),
			DurationMs: 1000, PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15, InputChars: 100, OutputChars: 20, Summary: "ok"},
		{ID: "run_2", ProjectID: "p1", AgentKey: "continuity", AgentName: "A1", EventID: "ev2", EventType: "chapter_saved",
			Permission: "observe", Status: "failed", StartedAt: now, FinishedAt: now.Add(time.Second),
			DurationMs: 3000, Error: "boom"},
	}
	for _, rec := range records {
		if err := st.RecordAgentRun(rec); err != nil {
			t.Fatalf("RecordAgentRun: %v", err)
		}
	}
	runs, err := st.QueryAgentRuns(AgentRunFilter{AgentKey: "continuity", Limit: 10})
	if err != nil || len(runs) != 2 {
		t.Fatalf("runs = %+v, err = %v", runs, err)
	}
	if runs[0].ID != "run_2" {
		t.Errorf("runs not newest first: %+v", runs)
	}
	failed, err := st.QueryAgentRuns(AgentRunFilter{Status: "failed", Limit: 10})
	if err != nil || len(failed) != 1 || failed[0].ID != "run_2" {
		t.Errorf("failed runs = %+v, err = %v", failed, err)
	}
	stats, err := st.AgentRunStats(now.Add(-2 * time.Hour))
	if err != nil || len(stats) != 1 {
		t.Fatalf("stats = %+v, err = %v", stats, err)
	}
	if stats[0].Runs != 2 || stats[0].Failed != 1 || stats[0].TotalTokens != 15 || stats[0].AvgDurationMs != 2000 {
		t.Errorf("stats = %+v", stats[0])
	}
	if _, err := os.Stat(filepath.Join(dir, "agent.db")); err != nil {
		t.Errorf("agent.db not created: %v", err)
	}
}

func TestForeshadowLedgerAndInjection(t *testing.T) {
	st, _ := newTestStore(t)
	records := []ForeshadowRecord{
		{ID: "fs_1", ChapterID: "ch_1", ChapterIndex: 1, Title: "半枚玉符", Kind: "object", Quote: "他摸到半枚玉符", Status: "candidate", Confidence: 0.6},
		{ID: "fs_2", ChapterID: "ch_2", ChapterIndex: 2, Title: "旧信", Kind: "object", Quote: "信封没有署名", Status: "confirmed", Confidence: 0.8},
		{ID: "fs_3", ChapterID: "ch_3", ChapterIndex: 3, Title: "眼神", Kind: "mystery", Quote: "她的眼神闪烁", Status: "candidate", Confidence: 0.4},
	}
	inserted, err := st.SaveForeshadows(records)
	if err != nil || inserted != 3 {
		t.Fatalf("inserted = %d, err = %v", inserted, err)
	}
	dup := []ForeshadowRecord{{ID: "fs_dup", ChapterID: "ch_1", ChapterIndex: 1, Title: "重复", Quote: "他摸到半枚玉符"}}
	if inserted, err := st.SaveForeshadows(dup); err != nil || inserted != 0 {
		t.Errorf("duplicate insert = %d, err = %v", inserted, err)
	}
	injectable, err := st.ForeshadowsForInjection(3, 10)
	if err != nil || len(injectable) != 2 {
		t.Fatalf("injectable = %+v, err = %v", injectable, err)
	}
	if injectable[0].ID != "fs_2" {
		t.Errorf("injection order = %+v", injectable)
	}
	if err := st.SetForeshadowStatus("fs_1", "confirmed"); err != nil {
		t.Fatalf("SetForeshadowStatus: %v", err)
	}
	if err := st.SetForeshadowStatus("fs_missing", "confirmed"); !errors.Is(err, ErrAgentRecordNotFound) {
		t.Errorf("missing status error = %v", err)
	}
	all, err := st.ListForeshadows("", 10)
	if err != nil || len(all) != 3 {
		t.Errorf("list = %+v, err = %v", all, err)
	}
}

func TestAgentConfigAndProposalsPersist(t *testing.T) {
	st, _ := newTestStore(t)
	if err := st.SaveAgentConfig(AgentConfigRecord{AgentKey: "fuse", Enabled: false, Level: "observe"}); err != nil {
		t.Fatalf("SaveAgentConfig: %v", err)
	}
	if err := st.SaveAgentConfig(AgentConfigRecord{AgentKey: "fuse", Enabled: true, Level: "propose"}); err != nil {
		t.Fatalf("SaveAgentConfig upsert: %v", err)
	}
	cfgs, err := st.LoadAgentConfigs()
	if err != nil || len(cfgs) != 1 || !cfgs["fuse"].Enabled || cfgs["fuse"].Level != "propose" {
		t.Fatalf("configs = %+v, err = %v", cfgs, err)
	}

	rec := AgentProposalRecord{AgentKey: "continuity", Kind: "merge_state", TargetID: "state_1", Summary: "合并状态", Risk: "low", Payload: []byte(`{"a":1}`)}
	first, created, err := st.SaveAgentProposal(rec)
	if err != nil || !created || first.ID == "" {
		t.Fatalf("save proposal = %+v created=%v err=%v", first, created, err)
	}
	second, created, err := st.SaveAgentProposal(rec)
	if err != nil || created || second.ID != first.ID {
		t.Errorf("dedupe proposal = %+v created=%v err=%v", second, created, err)
	}
	if err := st.SetAgentProposalStatus(first.ID, "accepted"); err != nil {
		t.Fatalf("SetAgentProposalStatus: %v", err)
	}
	pending, err := st.ListAgentProposals("pending", 10)
	if err != nil || len(pending) != 0 {
		t.Errorf("pending after accept = %+v, err = %v", pending, err)
	}
	accepted, err := st.ListAgentProposals("accepted", 10)
	if err != nil || len(accepted) != 1 {
		t.Errorf("accepted = %+v, err = %v", accepted, err)
	}
}

func TestRetrievalDoesNotCreateAgentDB(t *testing.T) {
	st, dir := newTestStore(t)
	unknown := novel.NewKnowledge()
	unknown.Characters = []novel.Character{{ID: "c_1", Name: "林越"}}
	memory, err := st.RetrieveContinuationMemory(unknown, "林越推门而入。", 2, 4000)
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	if len(memory.Records) != 0 {
		t.Errorf("unexpected records: %+v", memory.Records)
	}
	if _, err := os.Stat(filepath.Join(dir, "agent.db")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("retrieval created agent.db: %v", err)
	}
}

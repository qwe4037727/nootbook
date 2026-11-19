package store

import (
	"testing"
	"time"
)

// M1 验收 7：项目隔离——两个 Store（不同项目目录）之间运行日志、伏笔、配置、提案互不可见。
func TestAgentStoresAreIsolated(t *testing.T) {
	stA, _ := newTestStore(t)
	stB, _ := newTestStore(t)

	if _, err := stA.SaveForeshadows([]ForeshadowRecord{{
		ID: "fs_a", ChapterID: "ch_a", ChapterIndex: 1, Title: "A 的伏笔", Kind: "object", Quote: "a", Status: "candidate",
	}}); err != nil {
		t.Fatalf("save foreshadow A: %v", err)
	}
	now := time.Now().UTC()
	if err := stA.RecordAgentRun(AgentRunRecord{
		ID: "run_a", ProjectID: "a", AgentKey: "fuse", AgentName: "A3", EventType: "chapter_saved",
		Status: "done", StartedAt: now, FinishedAt: now, TotalTokens: 3,
	}); err != nil {
		t.Fatalf("record run A: %v", err)
	}
	if err := stA.SaveAgentConfig(AgentConfigRecord{AgentKey: "fuse", Enabled: false, Level: "observe"}); err != nil {
		t.Fatalf("save config A: %v", err)
	}
	if _, _, err := stA.SaveAgentProposal(AgentProposalRecord{AgentKey: "memory", Kind: "merge_state", Summary: "A", Risk: "low", Payload: []byte(`{}`)}); err != nil {
		t.Fatalf("save proposal A: %v", err)
	}

	if list, err := stB.ListForeshadows("", 10); err != nil || len(list) != 0 {
		t.Errorf("project B sees A foreshadows: %+v, err = %v", list, err)
	}
	if runs, err := stB.QueryAgentRuns(AgentRunFilter{Limit: 10}); err != nil || len(runs) != 0 {
		t.Errorf("project B sees A runs: %+v, err = %v", runs, err)
	}
	if cfgs, err := stB.LoadAgentConfigs(); err != nil || len(cfgs) != 0 {
		t.Errorf("project B sees A config: %+v, err = %v", cfgs, err)
	}
	if proposals, err := stB.ListAgentProposals("", 10); err != nil || len(proposals) != 0 {
		t.Errorf("project B sees A proposals: %+v, err = %v", proposals, err)
	}
	if runs, err := stA.QueryAgentRuns(AgentRunFilter{AgentKey: "fuse", Limit: 10}); err != nil || len(runs) != 1 || runs[0].ProjectID != "a" {
		t.Errorf("project A runs = %+v, err = %v", runs, err)
	}
}

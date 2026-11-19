package agentruntime

import (
	"context"
	"strings"
	"testing"

	"novelagent/internal/store"
)

type reportingAgent struct{}

func (reportingAgent) Key() string  { return "reporting" }
func (reportingAgent) Name() string { return "报告夹具" }
func (reportingAgent) Triggers() []string {
	return []string{string(store.ChapterSaved), string(store.ChapterCreated), string(store.ChapterDeleted), string(store.ChaptersImported)}
}
func (reportingAgent) Run(context.Context, Event, Input) (Report, error) {
	return Report{
		Summary: "发现一处冲突", Severity: "warning", Tokens: 9, PromptTokens: 7, CompletionTokens: 2,
		Findings: []Finding{{Type: "test_conflict", Severity: "warning", Detail: "夹具冲突", ChapterIndex: 1, Quote: "初始正文"}},
	}, nil
}

func TestRunPersistsTokensAndSupportsDisable(t *testing.T) {
	rt, st, _ := newTestRuntime(t)
	rt.Register(reportingAgent{})
	project, err := st.LoadProject()
	if err != nil {
		t.Fatalf("load project: %v", err)
	}
	chapter := project.Chapters[0]
	rt.Publish(Event{ID: "ev_reporting", Type: string(store.ChapterSaved), ChapterID: chapter.ID, Index: chapter.Index})
	waitForRuns(t, rt, 2)

	rows, err := st.QueryAgentRuns(store.AgentRunFilter{AgentKey: "reporting", Limit: 5})
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows = %+v, err = %v", rows, err)
	}
	row := rows[0]
	if row.Status != StatusDone || row.TotalTokens != 9 || row.PromptTokens != 7 || row.CompletionTokens != 2 || row.DurationMs < 0 {
		t.Errorf("row = %+v", row)
	}
	if row.ReportID == "" {
		t.Fatalf("report not linked: %+v", row)
	}
	report, err := st.GetAgentReport(row.ReportID)
	if err != nil || !strings.Contains(report.Summary, "冲突") {
		t.Errorf("report = %+v, err = %v", report, err)
	}
	if err := st.SaveAgentConfig(store.AgentConfigRecord{AgentKey: "reporting", Enabled: false, Level: "observe"}); err != nil {
		t.Fatalf("disable: %v", err)
	}
	rt.Publish(Event{ID: "ev_reporting_disabled", Type: string(store.ChapterSaved), ChapterID: chapter.ID, Index: chapter.Index})
	waitForRuns(t, rt, 2)
	rows, err = st.QueryAgentRuns(store.AgentRunFilter{AgentKey: "reporting", Limit: 5})
	if err != nil || len(rows) != 1 {
		t.Errorf("disabled agent produced runs: %+v, err = %v", rows, err)
	}
}

package agents

import (
	"context"
	"strings"
	"testing"

	"novelagent/internal/agentruntime"
	"novelagent/internal/novel"
	"novelagent/internal/store"
)

func runAgentOnChapter(t *testing.T, st *store.Store, chapter novel.Chapter, agent agentruntime.Agent, permission string) agentruntime.Report {
	t.Helper()
	ev := agentruntime.Event{
		ID:        "ev_" + string(chapter.ID),
		Type:      string(store.ChapterSaved),
		ChapterID: chapter.ID,
		Title:     chapter.Title,
		Index:     chapter.Index,
		CharCount: chapter.CharCount,
	}
	report, err := agent.Run(context.Background(), ev, agentruntime.Input{
		Store: st, Permission: permission, Model: "test", Configured: false,
	})
	if err != nil {
		t.Fatalf("%s run: %v", agent.Key(), err)
	}
	return report
}

func TestPlotAgentReportsStaticChapter(t *testing.T) {
	st, project := newAgentFixture(t)
	content := strings.Repeat("林越沿着雾城的街道向前走，四周没有声音。", 30)
	chapter, _, err := st.AppendChapter("长章", content)
	if err != nil {
		t.Fatalf("AppendChapter: %v", err)
	}
	_ = project
	report := runAgentOnChapter(t, st, chapter, &PlotAgent{}, "observe")
	if !hasFinding(report, "plot_static") {
		t.Fatalf("findings = %+v", report.Findings)
	}
	if report.Tokens != 0 {
		t.Fatalf("unconfigured agent should not call the model: %+v", report)
	}
}

func TestStyleAgentDetectsPerspectiveDrift(t *testing.T) {
	st, project := newAgentFixture(t)
	knowledge, err := st.LoadKnowledge()
	if err != nil {
		t.Fatalf("LoadKnowledge: %v", err)
	}
	knowledge.Style.Perspective = "第一人称"
	knowledge.Style.Sentence = "短句为主"
	if _, _, err := st.CommitAnalysis(project.Revision, len(project.Chapters), knowledge); err != nil {
		t.Fatalf("CommitAnalysis: %v", err)
	}
	content := strings.Repeat("他走进雾里，她却站在原地。他听见钟声，她也听见了。", 4)
	chapter, _, err := st.AppendChapter("视角章", content)
	if err != nil {
		t.Fatalf("AppendChapter: %v", err)
	}
	report := runAgentOnChapter(t, st, chapter, &StyleAgent{}, "observe")
	if !hasFinding(report, "style_perspective_drift") {
		t.Fatalf("findings = %+v", report.Findings)
	}
}

func TestProofreadAgentProposesWithoutEditing(t *testing.T) {
	st, project := newAgentFixture(t)
	before, err := st.LoadChapter(project.Chapters[0].ID)
	if err != nil {
		t.Fatalf("LoadChapter: %v", err)
	}
	chapter := before
	chapter.Content = "他说，，这不可能。“你确定吗？" // 重复标点 + 引号不配对
	if _, _, err := st.SaveChapter(chapter); err != nil {
		t.Fatalf("SaveChapter: %v", err)
	}
	chapter, err = st.LoadChapter(before.ID)
	if err != nil {
		t.Fatalf("LoadChapter: %v", err)
	}
	report := runAgentOnChapter(t, st, chapter, &ProofreadAgent{}, "propose")
	if !hasFinding(report, "duplicate_punctuation") || !hasFinding(report, "quote_mismatch") {
		t.Fatalf("findings = %+v", report.Findings)
	}
	proposals, err := st.ListAgentProposals("pending", 20)
	if err != nil || len(proposals) == 0 || proposals[0].Kind != "proofread_issues" {
		t.Fatalf("proposals = %+v err=%v", proposals, err)
	}
	after, err := st.LoadChapter(before.ID)
	if err != nil || after.Content != chapter.Content {
		t.Fatalf("proofread agent modified chapter: %q err=%v", after.Content, err)
	}
}

func TestRelationAgentFlagsUnknownPairs(t *testing.T) {
	st, project := newAgentFixture(t)
	knowledge, err := st.LoadKnowledge()
	if err != nil {
		t.Fatalf("LoadKnowledge: %v", err)
	}
	knowledge.Characters = append(knowledge.Characters, novel.Character{ID: "c_su", Name: "苏婉", Confidence: 0.4, LastSeen: 1})
	if _, _, err := st.CommitAnalysis(project.Revision, len(project.Chapters), knowledge); err != nil {
		t.Fatalf("CommitAnalysis: %v", err)
	}
	chapter, _, err := st.AppendChapter("相遇", "林越抬头，苏婉正站在雾城的石桥上。")
	if err != nil {
		t.Fatalf("AppendChapter: %v", err)
	}
	report := runAgentOnChapter(t, st, chapter, &RelationAgent{}, "observe")
	if !hasFinding(report, "relation_unknown") {
		t.Fatalf("findings = %+v", report.Findings)
	}
}

func TestTaskAgentReportsFailedTask(t *testing.T) {
	st, project := newAgentFixture(t)
	created, err := st.CreateTask(novel.Task{ID: "task_failed_probe", Kind: novel.TaskKindAgentRun, Description: "失败任务"})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if _, ok, err := st.ClaimNextTask(); err != nil || !ok {
		t.Fatalf("ClaimNextTask err=%v ok=%v", err, ok)
	}
	if _, err := st.FinishTask(created.ID, novel.TaskFailed, nil, "模型超时"); err != nil {
		t.Fatalf("FinishTask: %v", err)
	}
	ev := agentruntime.Event{
		ID:         "ev_task_failed",
		Type:       novel.TaskEventFailed,
		TaskID:     created.ID,
		TaskStatus: string(novel.TaskFailed),
		ChapterID:  project.Chapters[0].ID,
		Index:      1,
	}
	report, err := (&TaskAgent{}).Run(context.Background(), ev, agentruntime.Input{Store: st, Permission: "observe"})
	if err != nil {
		t.Fatalf("TaskAgent: %v", err)
	}
	if !hasFinding(report, "task_failed") || !strings.Contains(report.Summary, "失败 1") {
		t.Fatalf("report = %+v", report)
	}
}

func hasFinding(report agentruntime.Report, kind string) bool {
	for _, finding := range report.Findings {
		if finding.Type == kind {
			return true
		}
	}
	return false
}

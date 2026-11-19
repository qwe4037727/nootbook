package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"novelagent/internal/agentruntime"
	"novelagent/internal/novel"
)

// TaskAgent（A9 任务执行）：巡检任务队列，报告失败/停滞/积压任务与最近产出。
// 它不直接执行或取消任务，避免 Agent 与任务工作协程互相驱动。
type TaskAgent struct{}

func (a *TaskAgent) Key() string  { return "task" }
func (a *TaskAgent) Name() string { return "A9 任务执行" }

func (a *TaskAgent) Triggers() []string {
	return []string{
		novel.TaskEventCreated, novel.TaskEventCompleted,
		novel.TaskEventFailed, novel.TaskEventCancelled,
	}
}

func (a *TaskAgent) Run(ctx context.Context, ev agentruntime.Event, in agentruntime.Input) (agentruntime.Report, error) {
	report := agentruntime.Report{Severity: "info"}
	if in.Store == nil {
		return report, fmt.Errorf("task: store is required")
	}
	tasks, err := in.Store.ListTasks("", 20)
	if err != nil {
		return report, fmt.Errorf("task: list tasks: %w", err)
	}
	counts, err := in.Store.TaskStatusCounts()
	if err != nil {
		return report, fmt.Errorf("task: count tasks: %w", err)
	}

	findings := make([]agentruntime.Finding, 0, 6)
	now := time.Now().UTC()
	for _, task := range tasks {
		switch task.Status {
		case novel.TaskFailed:
			findings = append(findings, agentruntime.Finding{
				Type:         "task_failed",
				Severity:     "warning",
				ChapterID:    ev.ChapterID,
				ChapterIndex: ev.Index,
				Detail:       fmt.Sprintf("任务 %s（%s）失败：%s", task.ID, task.Kind, truncateRunes(task.Error, 120)),
			})
		case novel.TaskRunning:
			if !task.StartedAt.IsZero() && now.Sub(task.StartedAt) > 30*time.Minute {
				findings = append(findings, agentruntime.Finding{
					Type:         "task_stuck",
					Severity:     "warning",
					ChapterID:    ev.ChapterID,
					ChapterIndex: ev.Index,
					Detail:       fmt.Sprintf("任务 %s（%s）已运行 %d 分钟，建议检查模型通道或取消重试。", task.ID, task.Kind, int(now.Sub(task.StartedAt).Minutes())),
				})
			}
		case novel.TaskPaused:
			findings = append(findings, agentruntime.Finding{
				Type:         "task_paused",
				Severity:     "info",
				ChapterID:    ev.ChapterID,
				ChapterIndex: ev.Index,
				Detail:       fmt.Sprintf("任务 %s（%s）处于暂停状态：%s", task.ID, task.Kind, truncateRunes(task.Error, 80)),
			})
		}
		if len(findings) >= 5 {
			break
		}
	}
	if counts[string(novel.TaskPending)] > 5 {
		findings = append(findings, agentruntime.Finding{
			Type:     "task_backlog",
			Severity: "info",
			Detail:   fmt.Sprintf("队列积压 %d 个待执行任务，可按优先级安排。", counts[string(novel.TaskPending)]),
		})
	}

	var current *novel.Task
	if ev.TaskID != "" {
		if task, err := in.Store.GetTask(ev.TaskID); err == nil {
			current = &task
		}
	}
	report.Findings = findings
	report.Severity = severityOf(severityList(findings)...)
	report.Summary = fmt.Sprintf("任务中心：待执行 %d / 运行中 %d / 暂停 %d / 完成 %d / 失败 %d，%d 项观察。",
		counts[string(novel.TaskPending)], counts[string(novel.TaskRunning)], counts[string(novel.TaskPaused)],
		counts[string(novel.TaskCompleted)], counts[string(novel.TaskFailed)], len(findings))
	if current != nil {
		report.Details = append(report.Details, describeTask(*current))
	}
	report.Details = append(report.Details,
		fmt.Sprintf("权限：%s（只巡检报告，不自动执行/取消任务）", in.Permission),
	)
	return report, nil
}

func describeTask(task novel.Task) string {
	base := fmt.Sprintf("当前任务 %s：%s，状态 %s，进度 %d/%d", task.ID, task.Kind, task.Status, task.Progress, task.Total)
	if len(task.Result) == 0 {
		return base
	}
	var result novel.TaskContinueResult
	if err := json.Unmarshal(task.Result, &result); err != nil {
		return base
	}
	if len(result.DraftIDs) > 0 {
		base += fmt.Sprintf("，已生成 %d 个草稿", len(result.DraftIDs))
	}
	if len(result.ReportIDs) > 0 {
		base += fmt.Sprintf("，报告 %s", strings.Join(result.ReportIDs, ","))
	}
	return base
}

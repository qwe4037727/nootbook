package novel

import (
	"encoding/json"
	"strings"
	"time"
)

// 任务调度中心领域类型（M5）。任务数据按项目隔离存放在各项目 agent.db。

type TaskStatus string

const (
	TaskPending   TaskStatus = "pending"
	TaskRunning   TaskStatus = "running"
	TaskPaused    TaskStatus = "paused"
	TaskCompleted TaskStatus = "completed"
	TaskFailed    TaskStatus = "failed"
	TaskCancelled TaskStatus = "cancelled"
)

// Terminal 表示任务已结束，不会被工作协程再次领取。
func (s TaskStatus) Terminal() bool {
	switch s {
	case TaskCompleted, TaskFailed, TaskCancelled:
		return true
	}
	return false
}

// Active 表示任务会占用工作协程。
func (s TaskStatus) Active() bool {
	return s == TaskPending || s == TaskRunning
}

type TaskPriority string

const (
	TaskPriorityLow    TaskPriority = "low"
	TaskPriorityNormal TaskPriority = "normal"
	TaskPriorityHigh   TaskPriority = "high"
)

// Rank 返回队列排序权重：high=0, normal=1, low=2。
func (p TaskPriority) Rank() int {
	switch p {
	case TaskPriorityHigh:
		return 0
	case TaskPriorityLow:
		return 2
	default:
		return 1
	}
}

func NormalizeTaskPriority(p TaskPriority) TaskPriority {
	switch p {
	case TaskPriorityLow, TaskPriorityHigh:
		return p
	default:
		return TaskPriorityNormal
	}
}

const (
	TaskKindContinueBatch = "continue_batch"
	TaskKindAgentRun      = "agent_run"

	TaskEventCreated   = "task.created"
	TaskEventCompleted = "task.completed"
	TaskEventFailed    = "task.failed"
	TaskEventCancelled = "task.cancelled"
)

type Task struct {
	ID          string          `json:"id"`
	Kind        string          `json:"kind"`
	Description string          `json:"description,omitempty"`
	Priority    TaskPriority    `json:"priority"`
	Status      TaskStatus      `json:"status"`
	Params      json.RawMessage `json:"params,omitempty"`
	Progress    int             `json:"progress"`
	Total       int             `json:"total"`
	Result      json.RawMessage `json:"result,omitempty"`
	Error       string          `json:"error,omitempty"`
	CreatedAt   time.Time       `json:"createdAt"`
	UpdatedAt   time.Time       `json:"updatedAt"`
	StartedAt   time.Time       `json:"startedAt,omitempty"`
	FinishedAt  time.Time       `json:"finishedAt,omitempty"`
}

// TaskContinueParams 是 continue_batch 任务的持久化参数。
type TaskContinueParams struct {
	ChapterCount   int     `json:"chapterCount"`
	Direction      string  `json:"direction,omitempty"`
	TargetChars    int     `json:"targetChars,omitempty"`
	StartChapterID string  `json:"startChapterId,omitempty"`
	StartOffset    int     `json:"startOffset,omitempty"`
	Temperature    float64 `json:"temperature,omitempty"`
}

// TaskAgentParams 是 agent_run 任务的持久化参数。
type TaskAgentParams struct {
	Agent     string `json:"agent"`
	ChapterID string `json:"chapterId,omitempty"`
}

// TaskContinueResult 记录批量续写已生成的草稿（逐章追加）。
type TaskContinueResult struct {
	DraftIDs   []string `json:"draftIds"`
	GroupIDs   []string `json:"groupIds,omitempty"`
	ReportIDs  []string `json:"reportIds,omitempty"`
	ChapterIDs []string `json:"chapterIds,omitempty"`
	Tokens     int      `json:"tokens,omitempty"`
}

// ValidTaskKind 限制可入队的任务类型。
func ValidTaskKind(kind string) bool {
	switch strings.TrimSpace(kind) {
	case TaskKindContinueBatch, TaskKindAgentRun:
		return true
	}
	return false
}

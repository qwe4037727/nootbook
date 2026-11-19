package store

import (
	"encoding/json"
	"errors"
	"testing"

	"novelagent/internal/novel"
)

func TestTaskQueueLifecycle(t *testing.T) {
	st, _ := newTestStore(t)
	low, err := st.CreateTask(novel.Task{ID: "task_low", Kind: novel.TaskKindAgentRun, Description: "low", Priority: novel.TaskPriorityLow})
	if err != nil {
		t.Fatalf("CreateTask low: %v", err)
	}
	high, err := st.CreateTask(novel.Task{ID: "task_high", Kind: novel.TaskKindContinueBatch, Description: "high", Priority: novel.TaskPriorityHigh, Total: 3})
	if err != nil {
		t.Fatalf("CreateTask high: %v", err)
	}
	normal, err := st.CreateTask(novel.Task{ID: "task_normal", Kind: novel.TaskKindAgentRun, Description: "normal"})
	if err != nil {
		t.Fatalf("CreateTask normal: %v", err)
	}

	first, ok, err := st.ClaimNextTask()
	if err != nil || !ok || first.ID != high.ID || first.Status != novel.TaskRunning {
		t.Fatalf("first claim = %+v ok=%v err=%v", first, ok, err)
	}
	second, ok, err := st.ClaimNextTask()
	if err != nil || !ok || second.ID != normal.ID {
		t.Fatalf("second claim = %+v ok=%v err=%v", second, ok, err)
	}
	third, ok, err := st.ClaimNextTask()
	if err != nil || !ok || third.ID != low.ID {
		t.Fatalf("third claim = %+v ok=%v err=%v", third, ok, err)
	}
	if fourth, ok, err := st.ClaimNextTask(); err != nil || ok {
		t.Fatalf("empty claim = %+v ok=%v err=%v", fourth, ok, err)
	}

	if err := st.UpdateTaskProgress(first.ID, 1, json.RawMessage(`{"draftIds":["d1"]}`)); err != nil {
		t.Fatalf("UpdateTaskProgress: %v", err)
	}
	paused, err := st.PauseTask(first.ID)
	if err != nil || paused.Status != novel.TaskPaused {
		t.Fatalf("PauseTask = %+v err=%v", paused, err)
	}
	if err := st.UpdateTaskProgress(first.ID, 2, nil); !errors.Is(err, ErrTaskStateConflict) {
		t.Fatalf("progress after pause err = %v", err)
	}
	resumed, err := st.ResumeTask(first.ID)
	if err != nil || resumed.Status != novel.TaskPending {
		t.Fatalf("ResumeTask = %+v err=%v", resumed, err)
	}
	completed, err := st.FinishTask(first.ID, novel.TaskCompleted, json.RawMessage(`{"draftIds":[]}`), "")
	if err != nil || completed.Status != novel.TaskCompleted || completed.FinishedAt.IsZero() {
		t.Fatalf("FinishTask = %+v err=%v", completed, err)
	}
	if _, err := st.FinishTask(first.ID, novel.TaskFailed, nil, "again"); !errors.Is(err, ErrTaskStateConflict) {
		t.Fatalf("double finish err = %v", err)
	}
	if _, err := st.PauseTask(first.ID); !errors.Is(err, ErrTaskStateConflict) {
		t.Fatalf("pause completed err = %v", err)
	}
	cancelled, err := st.CancelTask(second.ID)
	if err != nil || cancelled.Status != novel.TaskCancelled {
		t.Fatalf("CancelTask = %+v err=%v", cancelled, err)
	}
	if _, err := st.CancelTask(second.ID); !errors.Is(err, ErrTaskStateConflict) {
		t.Fatalf("double cancel err = %v", err)
	}
	extra, err := st.CreateTask(novel.Task{ID: "task_pending_extra", Kind: novel.TaskKindAgentRun, Description: "pending"})
	if err != nil {
		t.Fatalf("CreateTask extra: %v", err)
	}

	counts, err := st.TaskStatusCounts()
	if err != nil || counts[string(novel.TaskPending)] != 1 || counts[string(novel.TaskCompleted)] != 1 ||
		counts[string(novel.TaskCancelled)] != 1 || counts[string(novel.TaskRunning)] != 1 {
		t.Fatalf("counts = %+v err=%v", counts, err)
	}
	tasks, err := st.ListTasks(string(novel.TaskPending), 10)
	if err != nil || len(tasks) != 1 || tasks[0].ID != extra.ID {
		t.Fatalf("pending list = %+v err=%v", tasks, err)
	}
}

func TestRecoverRunningTasksPauses(t *testing.T) {
	st, _ := newTestStore(t)
	created, err := st.CreateTask(novel.Task{ID: "task_recover", Kind: novel.TaskKindAgentRun, Description: "x"})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if _, ok, err := st.ClaimNextTask(); err != nil || !ok {
		t.Fatalf("claim err=%v ok=%v", err, ok)
	}
	recovered, err := st.RecoverRunningTasks()
	if err != nil || recovered != 1 {
		t.Fatalf("RecoverRunningTasks = %d err=%v", recovered, err)
	}
	task, err := st.GetTask(created.ID)
	if err != nil || task.Status != novel.TaskPaused || task.Error == "" {
		t.Fatalf("recovered task = %+v err=%v", task, err)
	}
}

package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"novelagent/internal/novel"
)

var (
	ErrTaskNotFound      = errors.New("store: task not found")
	ErrTaskStateConflict = errors.New("store: task state conflict")
)

const taskColumns = `id, kind, description, priority, status, params_json, progress, total,
	result_json, error, created_at, updated_at, started_at, finished_at`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanTask(row rowScanner) (novel.Task, error) {
	var task novel.Task
	var params, result string
	var created, updated, started, finished int64
	if err := row.Scan(&task.ID, &task.Kind, &task.Description, &task.Priority, &task.Status,
		&params, &task.Progress, &task.Total, &result, &task.Error,
		&created, &updated, &started, &finished); err != nil {
		return novel.Task{}, err
	}
	task.Params = json.RawMessage(params)
	task.Result = json.RawMessage(result)
	task.CreatedAt = fromUnixMillis(created)
	task.UpdatedAt = fromUnixMillis(updated)
	task.StartedAt = fromUnixMillis(started)
	task.FinishedAt = fromUnixMillis(finished)
	return task, nil
}

// CreateTask 入队一个任务；状态固定为 pending，由工作协程按优先级领取。
func (s *Store) CreateTask(task novel.Task) (novel.Task, error) {
	if !novel.ValidTaskKind(task.Kind) {
		return novel.Task{}, fmt.Errorf("store: invalid task kind %q", task.Kind)
	}
	db, err := s.agentDatabase()
	if err != nil {
		return novel.Task{}, err
	}
	now := time.Now().UTC()
	if strings.TrimSpace(task.ID) == "" {
		task.ID = novel.NewID("task")
	}
	task.Description = strings.TrimSpace(task.Description)
	task.Priority = novel.NormalizeTaskPriority(task.Priority)
	task.Status = novel.TaskPending
	if len(task.Params) == 0 {
		task.Params = json.RawMessage("{}")
	}
	if len(task.Result) == 0 {
		task.Result = json.RawMessage("{}")
	}
	task.Progress = 0
	task.Error = ""
	task.CreatedAt = now
	task.UpdatedAt = now
	task.StartedAt = time.Time{}
	task.FinishedAt = time.Time{}
	if _, err := db.Exec(`INSERT INTO task
		(id, project_id, kind, description, priority, status, params_json, progress, total,
		 result_json, error, created_at, updated_at, started_at, finished_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		task.ID, "", task.Kind, task.Description, string(task.Priority), string(task.Status),
		string(task.Params), task.Progress, task.Total, string(task.Result), task.Error,
		unixMillis(task.CreatedAt), unixMillis(task.UpdatedAt), 0, 0,
	); err != nil {
		return novel.Task{}, fmt.Errorf("store: insert task: %w", err)
	}
	return task, nil
}

func (s *Store) GetTask(id string) (novel.Task, error) {
	db, err := s.agentDatabase()
	if err != nil {
		return novel.Task{}, err
	}
	row := db.QueryRow(`SELECT `+taskColumns+` FROM task WHERE id = ?`, strings.TrimSpace(id))
	task, err := scanTask(row)
	if errors.Is(err, sql.ErrNoRows) {
		return novel.Task{}, ErrTaskNotFound
	}
	if err != nil {
		return novel.Task{}, fmt.Errorf("store: load task: %w", err)
	}
	return task, nil
}

// ListTasks 按状态过滤任务（空状态返回全部），新任务在前。
func (s *Store) ListTasks(status string, limit int) ([]novel.Task, error) {
	db, err := s.agentDatabase()
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	query := `SELECT ` + taskColumns + ` FROM task`
	args := make([]any, 0, 2)
	if status = strings.TrimSpace(status); status != "" {
		query += ` WHERE status = ?`
		args = append(args, status)
	}
	query += ` ORDER BY created_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list tasks: %w", err)
	}
	defer rows.Close()
	out := make([]novel.Task, 0, limit)
	for rows.Next() {
		task, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, task)
	}
	return out, rows.Err()
}

// ClaimNextTask 以优先级 + 入队时间领取下一个 pending 任务并标记 running。
func (s *Store) ClaimNextTask() (novel.Task, bool, error) {
	db, err := s.agentDatabase()
	if err != nil {
		return novel.Task{}, false, err
	}
	tx, err := db.Begin()
	if err != nil {
		return novel.Task{}, false, fmt.Errorf("store: begin claim task: %w", err)
	}
	defer tx.Rollback()
	row := tx.QueryRow(`SELECT ` + taskColumns + ` FROM task WHERE status = 'pending'
		ORDER BY CASE priority WHEN 'high' THEN 0 WHEN 'low' THEN 2 ELSE 1 END, created_at ASC LIMIT 1`)
	task, err := scanTask(row)
	if errors.Is(err, sql.ErrNoRows) {
		return novel.Task{}, false, nil
	}
	if err != nil {
		return novel.Task{}, false, fmt.Errorf("store: select task: %w", err)
	}
	now := time.Now().UTC()
	result, err := tx.Exec(`UPDATE task SET status = 'running', started_at = ?, updated_at = ?
		WHERE id = ? AND status = 'pending'`, unixMillis(now), unixMillis(now), task.ID)
	if err != nil {
		return novel.Task{}, false, fmt.Errorf("store: claim task: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return novel.Task{}, false, nil
	}
	if err := tx.Commit(); err != nil {
		return novel.Task{}, false, fmt.Errorf("store: commit claim: %w", err)
	}
	task.Status = novel.TaskRunning
	task.StartedAt = now
	task.UpdatedAt = now
	return task, true, nil
}

// UpdateTaskProgress 仅在任务仍处于 running 时写入进度；result 为空时保留旧值。
func (s *Store) UpdateTaskProgress(id string, progress int, result json.RawMessage) error {
	db, err := s.agentDatabase()
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	payload := "{}"
	if len(result) > 0 {
		payload = string(result)
	}
	res, err := db.Exec(`UPDATE task SET progress = ?,
		result_json = CASE WHEN ? = '{}' THEN result_json ELSE ? END, updated_at = ?
		WHERE id = ? AND status = 'running'`,
		progress, payload, payload, unixMillis(now), id)
	if err != nil {
		return fmt.Errorf("store: update task progress: %w", err)
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return ErrTaskStateConflict
	}
	return nil
}

// FinishTask 将任务置为终态（completed/failed），记录结果与错误。
func (s *Store) FinishTask(id string, status novel.TaskStatus, result json.RawMessage, message string) (novel.Task, error) {
	if !status.Terminal() {
		return novel.Task{}, ErrTaskStateConflict
	}
	db, err := s.agentDatabase()
	if err != nil {
		return novel.Task{}, err
	}
	now := time.Now().UTC()
	payload := "{}"
	if len(result) > 0 {
		payload = string(result)
	}
	res, err := db.Exec(`UPDATE task SET status = ?, result_json = ?, error = ?, updated_at = ?, finished_at = ?
		WHERE id = ? AND status IN ('pending','running','paused')`,
		string(status), payload, strings.TrimSpace(message), unixMillis(now), unixMillis(now), id)
	if err != nil {
		return novel.Task{}, fmt.Errorf("store: finish task: %w", err)
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return novel.Task{}, ErrTaskStateConflict
	}
	return s.GetTask(id)
}

// PauseTask 暂停 pending/running 任务；running 协程会在下一个检查点退出。
func (s *Store) PauseTask(id string) (novel.Task, error) {
	return s.transitionTask(id, novel.TaskPaused,
		`status IN ('pending','running')`, "任务已暂停")
}

// ResumeTask 将 paused 任务重新入队。
func (s *Store) ResumeTask(id string) (novel.Task, error) {
	db, err := s.agentDatabase()
	if err != nil {
		return novel.Task{}, err
	}
	now := time.Now().UTC()
	res, err := db.Exec(`UPDATE task SET status = 'pending', error = '', updated_at = ?
		WHERE id = ? AND status = 'paused'`, unixMillis(now), id)
	if err != nil {
		return novel.Task{}, fmt.Errorf("store: resume task: %w", err)
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return novel.Task{}, ErrTaskStateConflict
	}
	return s.GetTask(id)
}

// CancelTask 取消未结束任务；已结束任务返回状态冲突。
func (s *Store) CancelTask(id string) (novel.Task, error) {
	db, err := s.agentDatabase()
	if err != nil {
		return novel.Task{}, err
	}
	now := time.Now().UTC()
	res, err := db.Exec(`UPDATE task SET status = 'cancelled', updated_at = ?, finished_at = ?
		WHERE id = ? AND status IN ('pending','running','paused')`,
		unixMillis(now), unixMillis(now), id)
	if err != nil {
		return novel.Task{}, fmt.Errorf("store: cancel task: %w", err)
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		if _, err := s.GetTask(id); err != nil {
			return novel.Task{}, err
		}
		return novel.Task{}, ErrTaskStateConflict
	}
	return s.GetTask(id)
}

func (s *Store) transitionTask(id string, status novel.TaskStatus, condition, note string) (novel.Task, error) {
	db, err := s.agentDatabase()
	if err != nil {
		return novel.Task{}, err
	}
	now := time.Now().UTC()
	res, err := db.Exec(`UPDATE task SET status = ?, error = ?, updated_at = ?
		WHERE id = ? AND `+condition, string(status), note, unixMillis(now), id)
	if err != nil {
		return novel.Task{}, fmt.Errorf("store: transition task: %w", err)
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		if _, err := s.GetTask(id); err != nil {
			return novel.Task{}, err
		}
		return novel.Task{}, ErrTaskStateConflict
	}
	return s.GetTask(id)
}

// RecoverRunningTasks 在服务启动时把上次进程遗留的 running 任务转为 paused，
// 保证中断可恢复且不会在无人监督下自动继续消耗模型调用。
func (s *Store) RecoverRunningTasks() (int, error) {
	db, err := s.agentDatabase()
	if err != nil {
		return 0, err
	}
	now := time.Now().UTC()
	res, err := db.Exec(`UPDATE task SET status = 'paused', error = '服务重启，任务已暂停，请恢复后继续', updated_at = ?
		WHERE status = 'running'`, unixMillis(now))
	if err != nil {
		return 0, fmt.Errorf("store: recover running tasks: %w", err)
	}
	affected, _ := res.RowsAffected()
	return int(affected), nil
}

// TaskStatusCounts 返回各状态任务数（任务中心/巡检 Agent 使用）。
func (s *Store) TaskStatusCounts() (map[string]int, error) {
	db, err := s.agentDatabase()
	if err != nil {
		return nil, err
	}
	rows, err := db.Query(`SELECT status, COUNT(*) FROM task GROUP BY status`)
	if err != nil {
		return nil, fmt.Errorf("store: count tasks: %w", err)
	}
	defer rows.Close()
	out := make(map[string]int)
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return nil, err
		}
		out[status] = count
	}
	return out, rows.Err()
}

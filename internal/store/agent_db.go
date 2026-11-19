package store

import (
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"novelagent/internal/novel"
)

//go:embed migrations/0001_m1_agent.sql
var agentSchema string

//go:embed migrations/0002_m2_world.sql
var worldSchema string

//go:embed migrations/0003_m3_adapt.sql
var adaptSchema string

//go:embed migrations/0004_m4_manuscript.sql
var manuscriptSchema string

//go:embed migrations/0005_m5_task.sql
var taskSchema string

var (
	ErrAgentRecordNotFound = errors.New("store: agent record not found")
)

// AgentRunRecord 是一次 Agent 调用的可审计记录（agent_run 表）。
type AgentRunRecord struct {
	ID               string    `json:"id"`
	ProjectID        string    `json:"projectId"`
	AgentKey         string    `json:"agent"`
	AgentName        string    `json:"agentName"`
	EventID          string    `json:"eventId"`
	EventType        string    `json:"eventType"`
	Permission       string    `json:"permission"`
	Status           string    `json:"status"`
	StartedAt        time.Time `json:"startedAt"`
	FinishedAt       time.Time `json:"finishedAt"`
	DurationMs       int64     `json:"durationMs"`
	PromptTokens     int       `json:"promptTokens"`
	CompletionTokens int       `json:"completionTokens"`
	TotalTokens      int       `json:"totalTokens"`
	InputChars       int       `json:"inputChars"`
	OutputChars      int       `json:"outputChars"`
	Summary          string    `json:"summary,omitempty"`
	Error            string    `json:"error,omitempty"`
	ReportID         string    `json:"reportId,omitempty"`
}

// AgentRunFilter 约束运行日志查询。
type AgentRunFilter struct {
	AgentKey string
	Status   string
	Since    time.Time
	Limit    int
}

// AgentRunStat 是 Agent 成本/稳定性汇总。
type AgentRunStat struct {
	AgentKey         string `json:"agent"`
	AgentName        string `json:"agentName"`
	Runs             int64  `json:"runs"`
	Failed           int64  `json:"failed"`
	PromptTokens     int64  `json:"promptTokens"`
	CompletionTokens int64  `json:"completionTokens"`
	TotalTokens      int64  `json:"totalTokens"`
	AvgDurationMs    int64  `json:"avgDurationMs"`
}

// ForeshadowRecord 是 A3 伏笔台账条目（foreshadow 表）。
type ForeshadowRecord struct {
	ID           string    `json:"id"`
	ChapterID    string    `json:"chapterId"`
	ChapterIndex int       `json:"chapterIndex"`
	Title        string    `json:"title"`
	Kind         string    `json:"kind"`
	Quote        string    `json:"quote"`
	Status       string    `json:"status"`
	Confidence   float64   `json:"confidence"`
	Related      []string  `json:"related,omitempty"`
	EvidenceNote string    `json:"evidenceNote,omitempty"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

// AgentReportRecord 是一次 Agent 审计报告（agent_report 表）。
type AgentReportRecord struct {
	ID           string          `json:"id"`
	AgentKey     string          `json:"agent"`
	AgentName    string          `json:"agentName"`
	EventID      string          `json:"eventId,omitempty"`
	ChapterID    string          `json:"chapterId,omitempty"`
	ChapterIndex int             `json:"chapterIndex,omitempty"`
	Severity     string          `json:"severity"`
	Summary      string          `json:"summary"`
	Findings     json.RawMessage `json:"findings"`
	CreatedAt    time.Time       `json:"createdAt"`
}

// AgentProposalRecord 是 Agent 送入审阅队列的提案（agent_proposal 表）。
type AgentProposalRecord struct {
	ID        string          `json:"id"`
	AgentKey  string          `json:"agent"`
	Kind      string          `json:"kind"`
	TargetID  string          `json:"targetId,omitempty"`
	Summary   string          `json:"summary"`
	Risk      string          `json:"risk"`
	Status    string          `json:"status"`
	Payload   json.RawMessage `json:"payload"`
	CreatedAt time.Time       `json:"createdAt"`
	UpdatedAt time.Time       `json:"updatedAt"`
}

// AgentConfigRecord 为单项目内某 Agent 的开关与权限档位。
type AgentConfigRecord struct {
	AgentKey  string    `json:"agent"`
	Enabled   bool      `json:"enabled"`
	Level     string    `json:"level"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func unixMillis(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixMilli()
}

func fromUnixMillis(v int64) time.Time {
	if v == 0 {
		return time.Time{}
	}
	return time.UnixMilli(v).UTC()
}

// agentDatabase 惰性打开并迁移本项目的 agent.db。
// 每个项目独立一个数据库文件，天然满足项目隔离。
func (s *Store) agentDatabase() (*sql.DB, error) {
	s.agentDBMu.Lock()
	defer s.agentDBMu.Unlock()
	if s.agentDB != nil {
		return s.agentDB, nil
	}
	db, err := sql.Open("sqlite", filepath.Join(s.dir, "agent.db"))
	if err != nil {
		return nil, fmt.Errorf("store: open agent db: %w", err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		db.Close()
		return nil, fmt.Errorf("store: enable agent db WAL: %w", err)
	}
	if _, err := db.Exec(`PRAGMA busy_timeout=5000`); err != nil {
		db.Close()
		return nil, fmt.Errorf("store: set agent db busy timeout: %w", err)
	}
	for _, schema := range []struct {
		name string
		sql  string
	}{
		{"0001_m1_agent", agentSchema},
		{"0002_m2_world", worldSchema},
		{"0003_m3_adapt", adaptSchema},
		{"0004_m4_manuscript", manuscriptSchema},
		{"0005_m5_task", taskSchema},
	} {
		if _, err := db.Exec(schema.sql); err != nil {
			db.Close()
			return nil, fmt.Errorf("store: migrate agent db %s: %w", schema.name, err)
		}
	}
	s.agentDB = db
	return db, nil
}

// Close 释放该 Store 持有的 agent.db 句柄；可重复调用。
func (s *Store) Close() error {
	s.agentDBMu.Lock()
	db := s.agentDB
	s.agentDB = nil
	s.agentDBMu.Unlock()
	if db == nil {
		return nil
	}
	return db.Close()
}

// RecordAgentRun 持久化一次 Agent 运行（成本与耗时审计）。
func (s *Store) RecordAgentRun(rec AgentRunRecord) error {
	db, err := s.agentDatabase()
	if err != nil {
		return err
	}
	if rec.ID == "" {
		rec.ID = novel.NewID("run")
	}
	_, err = db.Exec(`INSERT INTO agent_run (
		id, project_id, agent_key, agent_name, event_id, event_type, permission, status,
		started_at, finished_at, duration_ms, prompt_tokens, completion_tokens, total_tokens,
		input_chars, output_chars, summary, error, report_id
	) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		rec.ID, rec.ProjectID, rec.AgentKey, rec.AgentName, rec.EventID, rec.EventType,
		rec.Permission, rec.Status, unixMillis(rec.StartedAt), unixMillis(rec.FinishedAt),
		rec.DurationMs, rec.PromptTokens, rec.CompletionTokens, rec.TotalTokens,
		rec.InputChars, rec.OutputChars, rec.Summary, rec.Error, rec.ReportID,
	)
	if err != nil {
		return fmt.Errorf("store: record agent run: %w", err)
	}
	return nil
}

// QueryAgentRuns 按条件倒序查询运行日志。
func (s *Store) QueryAgentRuns(filter AgentRunFilter) ([]AgentRunRecord, error) {
	db, err := s.agentDatabase()
	if err != nil {
		return nil, err
	}
	limit := filter.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	query := `SELECT id, project_id, agent_key, agent_name, event_id, event_type, permission, status,
		started_at, finished_at, duration_ms, prompt_tokens, completion_tokens, total_tokens,
		input_chars, output_chars, summary, error, report_id FROM agent_run WHERE 1=1`
	args := make([]any, 0, 4)
	if key := strings.TrimSpace(filter.AgentKey); key != "" {
		query += ` AND agent_key = ?`
		args = append(args, key)
	}
	if status := strings.TrimSpace(filter.Status); status != "" {
		query += ` AND status = ?`
		args = append(args, status)
	}
	if !filter.Since.IsZero() {
		query += ` AND started_at >= ?`
		args = append(args, unixMillis(filter.Since))
	}
	query += ` ORDER BY started_at DESC LIMIT ?`
	args = append(args, limit)

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: query agent runs: %w", err)
	}
	defer rows.Close()
	out := make([]AgentRunRecord, 0, limit)
	for rows.Next() {
		var rec AgentRunRecord
		var started, finished int64
		if err := rows.Scan(&rec.ID, &rec.ProjectID, &rec.AgentKey, &rec.AgentName, &rec.EventID,
			&rec.EventType, &rec.Permission, &rec.Status, &started, &finished, &rec.DurationMs,
			&rec.PromptTokens, &rec.CompletionTokens, &rec.TotalTokens, &rec.InputChars,
			&rec.OutputChars, &rec.Summary, &rec.Error, &rec.ReportID); err != nil {
			return nil, fmt.Errorf("store: scan agent run: %w", err)
		}
		rec.StartedAt = fromUnixMillis(started)
		rec.FinishedAt = fromUnixMillis(finished)
		out = append(out, rec)
	}
	return out, rows.Err()
}

// AgentRunStats 汇总 since 之后每个 Agent 的运行次数、失败数与 token 成本。
func (s *Store) AgentRunStats(since time.Time) ([]AgentRunStat, error) {
	db, err := s.agentDatabase()
	if err != nil {
		return nil, err
	}
	query := `SELECT agent_key, MAX(agent_name), COUNT(*),
		SUM(CASE WHEN status = 'failed' THEN 1 ELSE 0 END),
		COALESCE(SUM(prompt_tokens),0), COALESCE(SUM(completion_tokens),0),
		COALESCE(SUM(total_tokens),0), COALESCE(CAST(AVG(duration_ms) AS INTEGER),0)
		FROM agent_run`
	args := []any{}
	if !since.IsZero() {
		query += ` WHERE started_at >= ?`
		args = append(args, unixMillis(since))
	}
	query += ` GROUP BY agent_key ORDER BY agent_key`
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: agent run stats: %w", err)
	}
	defer rows.Close()
	out := []AgentRunStat{}
	for rows.Next() {
		var stat AgentRunStat
		if err := rows.Scan(&stat.AgentKey, &stat.AgentName, &stat.Runs, &stat.Failed,
			&stat.PromptTokens, &stat.CompletionTokens, &stat.TotalTokens, &stat.AvgDurationMs); err != nil {
			return nil, fmt.Errorf("store: scan agent stats: %w", err)
		}
		out = append(out, stat)
	}
	return out, rows.Err()
}

// SaveForeshadows 批量写入伏笔候选；(chapter_id, quote) 唯一约束保证幂等。
func (s *Store) SaveForeshadows(records []ForeshadowRecord) (int, error) {
	if len(records) == 0 {
		return 0, nil
	}
	db, err := s.agentDatabase()
	if err != nil {
		return 0, err
	}
	now := time.Now().UTC()
	inserted := 0
	for _, rec := range records {
		if strings.TrimSpace(rec.Quote) == "" || strings.TrimSpace(rec.ChapterID) == "" {
			continue
		}
		if rec.ID == "" {
			rec.ID = novel.NewID("fs")
		}
		if rec.Status == "" {
			rec.Status = "candidate"
		}
		if rec.CreatedAt.IsZero() {
			rec.CreatedAt = now
		}
		if rec.UpdatedAt.IsZero() {
			rec.UpdatedAt = rec.CreatedAt
		}
		related := rec.Related
		if related == nil {
			related = []string{}
		}
		relatedJSON, _ := json.Marshal(related)
		res, err := db.Exec(`INSERT OR IGNORE INTO foreshadow
			(id, chapter_id, chapter_index, title, kind, quote, status, confidence, related_json, evidence_note, created_at, updated_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
			rec.ID, rec.ChapterID, rec.ChapterIndex, rec.Title, rec.Kind, rec.Quote,
			rec.Status, rec.Confidence, string(relatedJSON), rec.EvidenceNote,
			unixMillis(rec.CreatedAt), unixMillis(rec.UpdatedAt),
		)
		if err != nil {
			return inserted, fmt.Errorf("store: save foreshadow: %w", err)
		}
		if n, _ := res.RowsAffected(); n > 0 {
			inserted++
		}
	}
	return inserted, nil
}

// ListForeshadows 按状态（空串为全部）倒序返回伏笔台账。
func (s *Store) ListForeshadows(status string, limit int) ([]ForeshadowRecord, error) {
	db, err := s.agentDatabase()
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	query := `SELECT id, chapter_id, chapter_index, title, kind, quote, status, confidence,
		related_json, evidence_note, created_at, updated_at FROM foreshadow`
	args := []any{}
	if st := strings.TrimSpace(status); st != "" {
		query += ` WHERE status = ?`
		args = append(args, st)
	}
	query += ` ORDER BY chapter_index DESC, created_at DESC, id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list foreshadows: %w", err)
	}
	defer rows.Close()
	out := []ForeshadowRecord{}
	for rows.Next() {
		var rec ForeshadowRecord
		var relatedJSON string
		var created, updated int64
		if err := rows.Scan(&rec.ID, &rec.ChapterID, &rec.ChapterIndex, &rec.Title, &rec.Kind,
			&rec.Quote, &rec.Status, &rec.Confidence, &relatedJSON, &rec.EvidenceNote, &created, &updated); err != nil {
			return nil, fmt.Errorf("store: scan foreshadow: %w", err)
		}
		_ = json.Unmarshal([]byte(relatedJSON), &rec.Related)
		rec.CreatedAt = fromUnixMillis(created)
		rec.UpdatedAt = fromUnixMillis(updated)
		out = append(out, rec)
	}
	return out, rows.Err()
}

// ForeshadowsForInjection 返回可注入续写的未回收伏笔（已确认与待确认）。
// foreshadowsForInjectionIfPresent 在 agent.db 尚不存在时直接返回空，
// 保证只读的续写检索不会因首次注入而创建/改动项目文件。
func (s *Store) foreshadowsForInjectionIfPresent(beforeIndex, limit int) ([]ForeshadowRecord, error) {
	if _, err := os.Stat(filepath.Join(s.dir, "agent.db")); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return s.ForeshadowsForInjection(beforeIndex, limit)
}

func (s *Store) ForeshadowsForInjection(beforeIndex, limit int) ([]ForeshadowRecord, error) {
	db, err := s.agentDatabase()
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 10 {
		limit = 3
	}
	rows, err := db.Query(`SELECT id, chapter_id, chapter_index, title, kind, quote, status, confidence,
		related_json, evidence_note, created_at, updated_at FROM foreshadow
		WHERE status IN ('candidate','confirmed') AND chapter_index < ?
		ORDER BY chapter_index DESC, created_at DESC, id DESC LIMIT ?`, beforeIndex, limit)
	if err != nil {
		return nil, fmt.Errorf("store: load injectable foreshadows: %w", err)
	}
	defer rows.Close()
	out := []ForeshadowRecord{}
	for rows.Next() {
		var rec ForeshadowRecord
		var relatedJSON string
		var created, updated int64
		if err := rows.Scan(&rec.ID, &rec.ChapterID, &rec.ChapterIndex, &rec.Title, &rec.Kind,
			&rec.Quote, &rec.Status, &rec.Confidence, &relatedJSON, &rec.EvidenceNote, &created, &updated); err != nil {
			return nil, fmt.Errorf("store: scan injectable foreshadow: %w", err)
		}
		_ = json.Unmarshal([]byte(relatedJSON), &rec.Related)
		rec.CreatedAt = fromUnixMillis(created)
		rec.UpdatedAt = fromUnixMillis(updated)
		out = append(out, rec)
	}
	return out, rows.Err()
}

// SetForeshadowStatus 更新伏笔状态（confirmed/dismissed/recovered/candidate）。
func (s *Store) SetForeshadowStatus(id, status string) error {
	db, err := s.agentDatabase()
	if err != nil {
		return err
	}
	res, err := db.Exec(`UPDATE foreshadow SET status = ?, updated_at = ? WHERE id = ?`,
		status, unixMillis(time.Now().UTC()), id)
	if err != nil {
		return fmt.Errorf("store: update foreshadow: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrAgentRecordNotFound
	}
	return nil
}

// SaveAgentReport 写入审计报告。
func (s *Store) SaveAgentReport(rec AgentReportRecord) error {
	db, err := s.agentDatabase()
	if err != nil {
		return err
	}
	if rec.ID == "" {
		rec.ID = novel.NewID("ar")
	}
	if rec.CreatedAt.IsZero() {
		rec.CreatedAt = time.Now().UTC()
	}
	findings := rec.Findings
	if len(findings) == 0 {
		findings = json.RawMessage("[]")
	}
	_, err = db.Exec(`INSERT INTO agent_report
		(id, agent_key, agent_name, event_id, chapter_id, chapter_index, severity, summary, findings_json, created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?)`,
		rec.ID, rec.AgentKey, rec.AgentName, rec.EventID, rec.ChapterID, rec.ChapterIndex,
		rec.Severity, rec.Summary, string(findings), unixMillis(rec.CreatedAt),
	)
	if err != nil {
		return fmt.Errorf("store: save agent report: %w", err)
	}
	return nil
}

// ListAgentReports 按 Agent/章节/严重度筛选报告。
func (s *Store) ListAgentReports(agentKey, chapterID, severity string, limit int) ([]AgentReportRecord, error) {
	db, err := s.agentDatabase()
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	query := `SELECT id, agent_key, agent_name, event_id, chapter_id, chapter_index, severity, summary, findings_json, created_at
		FROM agent_report WHERE 1=1`
	args := []any{}
	if v := strings.TrimSpace(agentKey); v != "" {
		query += ` AND agent_key = ?`
		args = append(args, v)
	}
	if v := strings.TrimSpace(chapterID); v != "" {
		query += ` AND chapter_id = ?`
		args = append(args, v)
	}
	if v := strings.TrimSpace(severity); v != "" {
		query += ` AND severity = ?`
		args = append(args, v)
	}
	query += ` ORDER BY created_at DESC, id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list agent reports: %w", err)
	}
	defer rows.Close()
	out := []AgentReportRecord{}
	for rows.Next() {
		var rec AgentReportRecord
		var findings string
		var created int64
		if err := rows.Scan(&rec.ID, &rec.AgentKey, &rec.AgentName, &rec.EventID, &rec.ChapterID,
			&rec.ChapterIndex, &rec.Severity, &rec.Summary, &findings, &created); err != nil {
			return nil, fmt.Errorf("store: scan agent report: %w", err)
		}
		rec.Findings = json.RawMessage(findings)
		rec.CreatedAt = fromUnixMillis(created)
		out = append(out, rec)
	}
	return out, rows.Err()
}

// GetAgentReport 返回单份报告全文。
func (s *Store) GetAgentReport(id string) (AgentReportRecord, error) {
	db, err := s.agentDatabase()
	if err != nil {
		return AgentReportRecord{}, err
	}
	var rec AgentReportRecord
	var findings string
	var created int64
	err = db.QueryRow(`SELECT id, agent_key, agent_name, event_id, chapter_id, chapter_index, severity, summary, findings_json, created_at
		FROM agent_report WHERE id = ?`, id).Scan(&rec.ID, &rec.AgentKey, &rec.AgentName, &rec.EventID,
		&rec.ChapterID, &rec.ChapterIndex, &rec.Severity, &rec.Summary, &findings, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return AgentReportRecord{}, ErrAgentRecordNotFound
	}
	if err != nil {
		return AgentReportRecord{}, fmt.Errorf("store: get agent report: %w", err)
	}
	rec.Findings = json.RawMessage(findings)
	rec.CreatedAt = fromUnixMillis(created)
	return rec, nil
}

// SaveAgentProposal 将 A4 等 Agent 的提案送审；同一 (agent,kind,target,payload) 不重复入队。
func (s *Store) SaveAgentProposal(rec AgentProposalRecord) (AgentProposalRecord, bool, error) {
	db, err := s.agentDatabase()
	if err != nil {
		return AgentProposalRecord{}, false, err
	}
	if rec.ID == "" {
		rec.ID = novel.NewID("ap")
	}
	if rec.Status == "" {
		rec.Status = "pending"
	}
	if rec.Risk == "" {
		rec.Risk = "low"
	}
	now := time.Now().UTC()
	if rec.CreatedAt.IsZero() {
		rec.CreatedAt = now
	}
	rec.UpdatedAt = now
	payload := rec.Payload
	if len(payload) == 0 {
		payload = json.RawMessage("{}")
	}
	var existing string
	err = db.QueryRow(`SELECT id FROM agent_proposal WHERE agent_key = ? AND kind = ? AND target_id = ? AND payload_json = ? AND status = 'pending'`,
		rec.AgentKey, rec.Kind, rec.TargetID, string(payload)).Scan(&existing)
	if err == nil {
		rec.ID = existing
		return rec, false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return AgentProposalRecord{}, false, fmt.Errorf("store: check agent proposal: %w", err)
	}
	_, err = db.Exec(`INSERT INTO agent_proposal
		(id, agent_key, kind, target_id, payload_json, summary, risk, status, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?)`,
		rec.ID, rec.AgentKey, rec.Kind, rec.TargetID, string(payload), rec.Summary,
		rec.Risk, rec.Status, unixMillis(rec.CreatedAt), unixMillis(rec.UpdatedAt),
	)
	if err != nil {
		return AgentProposalRecord{}, false, fmt.Errorf("store: save agent proposal: %w", err)
	}
	rec.Payload = payload
	return rec, true, nil
}

// ListAgentProposals 返回提案队列。
func (s *Store) ListAgentProposals(status string, limit int) ([]AgentProposalRecord, error) {
	db, err := s.agentDatabase()
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	query := `SELECT id, agent_key, kind, target_id, payload_json, summary, risk, status, created_at, updated_at
		FROM agent_proposal`
	args := []any{}
	if v := strings.TrimSpace(status); v != "" {
		query += ` WHERE status = ?`
		args = append(args, v)
	}
	query += ` ORDER BY created_at DESC, id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list agent proposals: %w", err)
	}
	defer rows.Close()
	out := []AgentProposalRecord{}
	for rows.Next() {
		var rec AgentProposalRecord
		var payload string
		var created, updated int64
		if err := rows.Scan(&rec.ID, &rec.AgentKey, &rec.Kind, &rec.TargetID, &payload,
			&rec.Summary, &rec.Risk, &rec.Status, &created, &updated); err != nil {
			return nil, fmt.Errorf("store: scan agent proposal: %w", err)
		}
		rec.Payload = json.RawMessage(payload)
		rec.CreatedAt = fromUnixMillis(created)
		rec.UpdatedAt = fromUnixMillis(updated)
		out = append(out, rec)
	}
	return out, rows.Err()
}

// GetAgentProposal 返回单条提案。
func (s *Store) GetAgentProposal(id string) (AgentProposalRecord, error) {
	db, err := s.agentDatabase()
	if err != nil {
		return AgentProposalRecord{}, err
	}
	var rec AgentProposalRecord
	var payload string
	var created, updated int64
	err = db.QueryRow(`SELECT id, agent_key, kind, target_id, payload_json, summary, risk, status, created_at, updated_at
		FROM agent_proposal WHERE id = ?`, id).Scan(&rec.ID, &rec.AgentKey, &rec.Kind, &rec.TargetID,
		&payload, &rec.Summary, &rec.Risk, &rec.Status, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return AgentProposalRecord{}, ErrAgentRecordNotFound
	}
	if err != nil {
		return AgentProposalRecord{}, fmt.Errorf("store: get agent proposal: %w", err)
	}
	rec.Payload = json.RawMessage(payload)
	rec.CreatedAt = fromUnixMillis(created)
	rec.UpdatedAt = fromUnixMillis(updated)
	return rec, nil
}

// SetAgentProposalStatus 更新提案状态（accepted/rejected/pending）。
func (s *Store) SetAgentProposalStatus(id, status string) error {
	db, err := s.agentDatabase()
	if err != nil {
		return err
	}
	res, err := db.Exec(`UPDATE agent_proposal SET status = ?, updated_at = ? WHERE id = ?`,
		status, unixMillis(time.Now().UTC()), id)
	if err != nil {
		return fmt.Errorf("store: update agent proposal: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrAgentRecordNotFound
	}
	return nil
}

// LoadAgentConfigs 读取本项目 Agent 配置（无记录时返回空 map）。
func (s *Store) LoadAgentConfigs() (map[string]AgentConfigRecord, error) {
	db, err := s.agentDatabase()
	if err != nil {
		return nil, err
	}
	rows, err := db.Query(`SELECT agent_key, enabled, level, updated_at FROM agent_config`)
	if err != nil {
		return nil, fmt.Errorf("store: load agent configs: %w", err)
	}
	defer rows.Close()
	out := map[string]AgentConfigRecord{}
	for rows.Next() {
		var rec AgentConfigRecord
		var enabled int
		var updated int64
		if err := rows.Scan(&rec.AgentKey, &enabled, &rec.Level, &updated); err != nil {
			return nil, fmt.Errorf("store: scan agent config: %w", err)
		}
		rec.Enabled = enabled != 0
		rec.UpdatedAt = fromUnixMillis(updated)
		out[rec.AgentKey] = rec
	}
	return out, rows.Err()
}

// SaveAgentConfig 保存单个 Agent 的开关与权限档位。
func (s *Store) SaveAgentConfig(rec AgentConfigRecord) error {
	db, err := s.agentDatabase()
	if err != nil {
		return err
	}
	enabled := 0
	if rec.Enabled {
		enabled = 1
	}
	_, err = db.Exec(`INSERT INTO agent_config (agent_key, enabled, level, updated_at)
		VALUES (?,?,?,?)
		ON CONFLICT(agent_key) DO UPDATE SET enabled = excluded.enabled, level = excluded.level, updated_at = excluded.updated_at`,
		rec.AgentKey, enabled, rec.Level, unixMillis(time.Now().UTC()))
	if err != nil {
		return fmt.Errorf("store: save agent config: %w", err)
	}
	return nil
}

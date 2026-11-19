package store

import (
	"fmt"
	"strings"
)

// AgentReportFilter 支持审计中心按 Agent、章节、章节序区间、严重度与人物条目筛选。
type AgentReportFilter struct {
	AgentKey        string
	ChapterID       string
	Severity        string
	EntryID         string
	ChapterIndexMin int
	ChapterIndexMax int
	Limit           int
}

// ListAgentReportsFiltered 是审计中心的统一查询入口（findings 中的 entryId 用于人物筛选）。
func (s *Store) ListAgentReportsFiltered(filter AgentReportFilter) ([]AgentReportRecord, error) {
	db, err := s.agentDatabase()
	if err != nil {
		return nil, err
	}
	limit := filter.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	query := `SELECT id, agent_key, agent_name, event_id, chapter_id, chapter_index, severity, summary, findings_json, created_at
		FROM agent_report WHERE 1=1`
	args := []any{}
	if v := strings.TrimSpace(filter.AgentKey); v != "" {
		query += ` AND agent_key = ?`
		args = append(args, v)
	}
	if v := strings.TrimSpace(filter.ChapterID); v != "" {
		query += ` AND chapter_id = ?`
		args = append(args, v)
	}
	if v := strings.TrimSpace(filter.Severity); v != "" {
		query += ` AND severity = ?`
		args = append(args, v)
	}
	if v := strings.TrimSpace(filter.EntryID); v != "" {
		query += ` AND findings_json LIKE ?`
		args = append(args, fmt.Sprintf(`%%"entryId":"%s"%%`, v))
	}
	if filter.ChapterIndexMin > 0 {
		query += ` AND chapter_index >= ?`
		args = append(args, filter.ChapterIndexMin)
	}
	if filter.ChapterIndexMax > 0 {
		query += ` AND chapter_index <= ?`
		args = append(args, filter.ChapterIndexMax)
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
		rec.Findings = []byte(findings)
		rec.CreatedAt = fromUnixMillis(created)
		out = append(out, rec)
	}
	return out, rows.Err()
}

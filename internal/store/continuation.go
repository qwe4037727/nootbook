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

var ErrContinuationGroupNotFound = errors.New("store: continuation group not found")

// SaveContinuationGroup 持久化一次多候选续写的请求元数据与全部候选。
func (s *Store) SaveContinuationGroup(group novel.ContinuationGroup, candidates []novel.ContinuationCandidate) (novel.ContinuationGroup, error) {
	db, err := s.agentDatabase()
	if err != nil {
		return novel.ContinuationGroup{}, err
	}
	if strings.TrimSpace(group.ID) == "" {
		group.ID = novel.NewID("cg")
	}
	if group.CreatedAt.IsZero() {
		group.CreatedAt = time.Now().UTC()
	}
	if group.CandidateCount <= 0 {
		group.CandidateCount = len(candidates)
	}
	tx, err := db.Begin()
	if err != nil {
		return novel.ContinuationGroup{}, fmt.Errorf("store: begin continuation group: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO continue_group
		(id, start_mode, chapter_id, start_offset, start_index, direction, target_chars,
		 candidate_count, temperature, model, prompt_chars, context_chars, memory_chars, created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		group.ID, group.StartMode, group.ChapterID, group.StartOffset, group.StartIndex,
		group.Direction, group.TargetChars, group.CandidateCount, group.Temperature,
		group.Model, group.PromptChars, group.ContextChars, group.MemoryChars,
		unixMillis(group.CreatedAt),
	); err != nil {
		return novel.ContinuationGroup{}, fmt.Errorf("store: insert continuation group: %w", err)
	}
	for i := range candidates {
		candidate := &candidates[i]
		if candidate.ID == "" {
			candidate.ID = novel.NewID("cc")
		}
		candidate.GroupID = group.ID
		if candidate.CreatedAt.IsZero() {
			candidate.CreatedAt = group.CreatedAt
		}
		if candidate.Status == "" {
			candidate.Status = "done"
		}
		if _, err := tx.Exec(`INSERT INTO continue_candidate
			(id, group_id, index_no, content, char_count, temperature,
			 prompt_tokens, completion_tokens, total_tokens, status, error, created_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
			candidate.ID, candidate.GroupID, candidate.Index, candidate.Content, candidate.CharCount,
			candidate.Temperature, candidate.PromptTokens, candidate.CompletionTokens,
			candidate.TotalTokens, candidate.Status, candidate.Error, unixMillis(candidate.CreatedAt),
		); err != nil {
			return novel.ContinuationGroup{}, fmt.Errorf("store: insert continuation candidate: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return novel.ContinuationGroup{}, fmt.Errorf("store: commit continuation group: %w", err)
	}
	return group, nil
}

func (s *Store) GetContinuationGroup(id string) (novel.ContinuationGroup, []novel.ContinuationCandidate, error) {
	db, err := s.agentDatabase()
	if err != nil {
		return novel.ContinuationGroup{}, nil, err
	}
	var group novel.ContinuationGroup
	var created int64
	err = db.QueryRow(`SELECT id, start_mode, chapter_id, start_offset, start_index, direction,
		target_chars, candidate_count, temperature, model, prompt_chars, context_chars, memory_chars, created_at
		FROM continue_group WHERE id = ?`, strings.TrimSpace(id)).Scan(
		&group.ID, &group.StartMode, &group.ChapterID, &group.StartOffset, &group.StartIndex,
		&group.Direction, &group.TargetChars, &group.CandidateCount, &group.Temperature,
		&group.Model, &group.PromptChars, &group.ContextChars, &group.MemoryChars, &created,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return novel.ContinuationGroup{}, nil, ErrContinuationGroupNotFound
	}
	if err != nil {
		return novel.ContinuationGroup{}, nil, fmt.Errorf("store: load continuation group: %w", err)
	}
	group.CreatedAt = fromUnixMillis(created)
	rows, err := db.Query(`SELECT id, group_id, index_no, content, char_count, temperature,
		prompt_tokens, completion_tokens, total_tokens, status, error, created_at
		FROM continue_candidate WHERE group_id = ? ORDER BY index_no`, group.ID)
	if err != nil {
		return novel.ContinuationGroup{}, nil, fmt.Errorf("store: load continuation candidates: %w", err)
	}
	defer rows.Close()
	candidates := make([]novel.ContinuationCandidate, 0, group.CandidateCount)
	for rows.Next() {
		var c novel.ContinuationCandidate
		var created int64
		if err := rows.Scan(&c.ID, &c.GroupID, &c.Index, &c.Content, &c.CharCount, &c.Temperature,
			&c.PromptTokens, &c.CompletionTokens, &c.TotalTokens, &c.Status, &c.Error, &created); err != nil {
			return novel.ContinuationGroup{}, nil, err
		}
		c.CreatedAt = fromUnixMillis(created)
		candidates = append(candidates, c)
	}
	return group, candidates, rows.Err()
}

func (s *Store) ListContinuationGroups(limit int) ([]novel.ContinuationGroup, error) {
	db, err := s.agentDatabase()
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := db.Query(`SELECT id, start_mode, chapter_id, start_offset, start_index, direction,
		target_chars, candidate_count, temperature, model, prompt_chars, context_chars, memory_chars, created_at
		FROM continue_group ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("store: list continuation groups: %w", err)
	}
	defer rows.Close()
	out := make([]novel.ContinuationGroup, 0, limit)
	for rows.Next() {
		var group novel.ContinuationGroup
		var created int64
		if err := rows.Scan(&group.ID, &group.StartMode, &group.ChapterID, &group.StartOffset, &group.StartIndex,
			&group.Direction, &group.TargetChars, &group.CandidateCount, &group.Temperature,
			&group.Model, &group.PromptChars, &group.ContextChars, &group.MemoryChars, &created); err != nil {
			return nil, err
		}
		group.CreatedAt = fromUnixMillis(created)
		out = append(out, group)
	}
	return out, rows.Err()
}

// marshalTaskResult 将任意结果编为 JSON；失败时退化为空对象，避免任务终态写入失败。
func marshalTaskResult(v any) json.RawMessage {
	if v == nil {
		return json.RawMessage("{}")
	}
	data, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage("{}")
	}
	return data
}

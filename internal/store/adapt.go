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

var ErrAdaptRunNotFound = errors.New("store: adapt run not found")

// AdaptRunRecord 是一次改编调用的审计记录（含 token 成本与影响清单）。
// 只审计、不写记忆；正文结果落在草稿区。
type AdaptRunRecord struct {
	ID               string          `json:"id"`
	ChapterID        string          `json:"chapterId"`
	ChapterIndex     int             `json:"chapterIndex"`
	Mode             string          `json:"mode"`
	InstructionType  string          `json:"instructionType,omitempty"`
	Instruction      string          `json:"instruction"`
	StartOffset      int             `json:"startOffset"`
	EndOffset        int             `json:"endOffset"`
	OldChars         int             `json:"oldChars"`
	NewChars         int             `json:"newChars"`
	Style            string          `json:"style,omitempty"`
	Perspective      string          `json:"perspective,omitempty"`
	Model            string          `json:"model,omitempty"`
	PromptTokens     int             `json:"promptTokens"`
	CompletionTokens int             `json:"completionTokens"`
	TotalTokens      int             `json:"totalTokens"`
	DraftID          string          `json:"draftId,omitempty"`
	Impact           json.RawMessage `json:"impact,omitempty"`
	Status           string          `json:"status"`
	Error            string          `json:"error,omitempty"`
	CreatedAt        time.Time       `json:"createdAt"`
}

func (s *Store) SaveAdaptRun(rec AdaptRunRecord) (AdaptRunRecord, error) {
	db, err := s.agentDatabase()
	if err != nil {
		return AdaptRunRecord{}, err
	}
	if strings.TrimSpace(rec.ID) == "" {
		rec.ID = novel.NewID("adpt")
	}
	if rec.CreatedAt.IsZero() {
		rec.CreatedAt = time.Now().UTC()
	}
	if strings.TrimSpace(rec.Status) == "" {
		rec.Status = "done"
	}
	impact := rec.Impact
	if len(impact) == 0 {
		impact = json.RawMessage("{}")
	}
	_, err = db.Exec(`INSERT INTO adapt_run
		(id, chapter_id, chapter_index, mode, instruction_type, instruction, start_offset, end_offset,
		 old_chars, new_chars, style, perspective, model, prompt_tokens, completion_tokens, total_tokens,
		 draft_id, impact_json, status, error, created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		rec.ID, rec.ChapterID, rec.ChapterIndex, rec.Mode, rec.InstructionType, rec.Instruction,
		rec.StartOffset, rec.EndOffset, rec.OldChars, rec.NewChars, rec.Style, rec.Perspective,
		rec.Model, rec.PromptTokens, rec.CompletionTokens, rec.TotalTokens, rec.DraftID,
		string(impact), rec.Status, rec.Error, unixMillis(rec.CreatedAt),
	)
	if err != nil {
		return AdaptRunRecord{}, fmt.Errorf("store: save adapt run: %w", err)
	}
	rec.Impact = impact
	return rec, nil
}

func (s *Store) GetAdaptRun(id string) (AdaptRunRecord, error) {
	db, err := s.agentDatabase()
	if err != nil {
		return AdaptRunRecord{}, err
	}
	row := db.QueryRow(`SELECT id, chapter_id, chapter_index, mode, instruction_type, instruction,
		start_offset, end_offset, old_chars, new_chars, style, perspective, model,
		prompt_tokens, completion_tokens, total_tokens, draft_id, impact_json, status, error, created_at
		FROM adapt_run WHERE id = ?`, id)
	rec, err := scanAdaptRun(row)
	if errors.Is(err, sql.ErrNoRows) {
		return AdaptRunRecord{}, ErrAdaptRunNotFound
	}
	if err != nil {
		return AdaptRunRecord{}, fmt.Errorf("store: get adapt run: %w", err)
	}
	return rec, nil
}

func (s *Store) ListAdaptRuns(limit int) ([]AdaptRunRecord, error) {
	db, err := s.agentDatabase()
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := db.Query(`SELECT id, chapter_id, chapter_index, mode, instruction_type, instruction,
		start_offset, end_offset, old_chars, new_chars, style, perspective, model,
		prompt_tokens, completion_tokens, total_tokens, draft_id, impact_json, status, error, created_at
		FROM adapt_run ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("store: list adapt runs: %w", err)
	}
	defer rows.Close()
	out := []AdaptRunRecord{}
	for rows.Next() {
		rec, err := scanAdaptRun(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan adapt run: %w", err)
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

type adaptScanner interface {
	Scan(dest ...any) error
}

func scanAdaptRun(row adaptScanner) (AdaptRunRecord, error) {
	var rec AdaptRunRecord
	var impact string
	var created int64
	err := row.Scan(&rec.ID, &rec.ChapterID, &rec.ChapterIndex, &rec.Mode, &rec.InstructionType,
		&rec.Instruction, &rec.StartOffset, &rec.EndOffset, &rec.OldChars, &rec.NewChars,
		&rec.Style, &rec.Perspective, &rec.Model, &rec.PromptTokens, &rec.CompletionTokens,
		&rec.TotalTokens, &rec.DraftID, &impact, &rec.Status, &rec.Error, &created)
	if err != nil {
		return AdaptRunRecord{}, err
	}
	rec.Impact = json.RawMessage(impact)
	rec.CreatedAt = fromUnixMillis(created)
	return rec, nil
}

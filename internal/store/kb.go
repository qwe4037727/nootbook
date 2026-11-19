package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"novelagent/internal/novel"
)

// M2 大世界仓储：kb_entry / kb_relation / kb_timeline_event / kb_field_lock。
// 全部落在项目级 agent.db（一项目一库），天然满足项目隔离。

var (
	ErrKBEntryNotFound  = errors.New("store: kb entry not found")
	ErrRelationNotFound = errors.New("store: kb relation not found")
	ErrTimelineNotFound = errors.New("store: timeline event not found")
)

func jsonString(value any, fallback string) string {
	data, err := json.Marshal(value)
	if err != nil {
		return fallback
	}
	return string(data)
}

func (s *Store) SaveKBEntry(entry novel.KBEntry) (novel.KBEntry, error) {
	entry.Normalize(time.Now().UTC())
	if err := entry.Validate(); err != nil {
		return novel.KBEntry{}, err
	}
	db, err := s.agentDatabase()
	if err != nil {
		return novel.KBEntry{}, err
	}
	_, err = db.Exec(`INSERT INTO kb_entry
		(id, entry_kind, name, aliases_json, summary, fields_json, source_refs_json, confidence, last_seen, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET
			entry_kind = excluded.entry_kind,
			name = excluded.name,
			aliases_json = excluded.aliases_json,
			summary = excluded.summary,
			fields_json = excluded.fields_json,
			source_refs_json = excluded.source_refs_json,
			confidence = excluded.confidence,
			last_seen = excluded.last_seen,
			updated_at = excluded.updated_at`,
		entry.ID, string(entry.Kind), entry.Name, jsonString(entry.Aliases, "[]"), entry.Summary,
		jsonString(entry.Fields, "{}"), jsonString(entry.SourceRefs, "[]"), entry.Confidence,
		entry.LastSeen, unixMillis(entry.CreatedAt), unixMillis(entry.UpdatedAt),
	)
	if err != nil {
		return novel.KBEntry{}, fmt.Errorf("store: save kb entry: %w", err)
	}
	return s.GetKBEntry(entry.ID)
}

func (s *Store) GetKBEntry(id string) (novel.KBEntry, error) {
	db, err := s.agentDatabase()
	if err != nil {
		return novel.KBEntry{}, err
	}
	var entry novel.KBEntry
	var aliasesJSON, fieldsJSON, refsJSON string
	var kind string
	var created, updated int64
	err = db.QueryRow(`SELECT id, entry_kind, name, aliases_json, summary, fields_json, source_refs_json,
		confidence, last_seen, created_at, updated_at FROM kb_entry WHERE id = ?`, id).
		Scan(&entry.ID, &kind, &entry.Name, &aliasesJSON, &entry.Summary, &fieldsJSON, &refsJSON,
			&entry.Confidence, &entry.LastSeen, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return novel.KBEntry{}, ErrKBEntryNotFound
	}
	if err != nil {
		return novel.KBEntry{}, fmt.Errorf("store: get kb entry: %w", err)
	}
	entry.Kind = novel.EntryKind(kind)
	_ = json.Unmarshal([]byte(aliasesJSON), &entry.Aliases)
	_ = json.Unmarshal([]byte(fieldsJSON), &entry.Fields)
	_ = json.Unmarshal([]byte(refsJSON), &entry.SourceRefs)
	entry.CreatedAt = fromUnixMillis(created)
	entry.UpdatedAt = fromUnixMillis(updated)
	entry.Normalize(time.Now().UTC())
	return entry, nil
}

// ListKBEntries 返回扩展词条；kind 为空表示全部。
func (s *Store) ListKBEntries(kind string) ([]novel.KBEntry, error) {
	db, err := s.agentDatabase()
	if err != nil {
		return nil, err
	}
	query := `SELECT id, entry_kind, name, aliases_json, summary, fields_json, source_refs_json,
		confidence, last_seen, created_at, updated_at FROM kb_entry`
	args := []any{}
	if strings.TrimSpace(kind) != "" {
		query += ` WHERE entry_kind = ?`
		args = append(args, strings.TrimSpace(kind))
	}
	query += ` ORDER BY name COLLATE NOCASE ASC, id ASC`
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list kb entries: %w", err)
	}
	defer rows.Close()
	out := []novel.KBEntry{}
	for rows.Next() {
		var entry novel.KBEntry
		var aliasesJSON, fieldsJSON, refsJSON string
		var kindValue string
		var created, updated int64
		if err := rows.Scan(&entry.ID, &kindValue, &entry.Name, &aliasesJSON, &entry.Summary, &fieldsJSON,
			&refsJSON, &entry.Confidence, &entry.LastSeen, &created, &updated); err != nil {
			return nil, fmt.Errorf("store: scan kb entry: %w", err)
		}
		entry.Kind = novel.EntryKind(kindValue)
		_ = json.Unmarshal([]byte(aliasesJSON), &entry.Aliases)
		_ = json.Unmarshal([]byte(fieldsJSON), &entry.Fields)
		_ = json.Unmarshal([]byte(refsJSON), &entry.SourceRefs)
		entry.CreatedAt = fromUnixMillis(created)
		entry.UpdatedAt = fromUnixMillis(updated)
		entry.Normalize(time.Now().UTC())
		out = append(out, entry)
	}
	return out, rows.Err()
}

// DeleteKBEntry 删除扩展词条，并清理其关系边与字段锁。
func (s *Store) DeleteKBEntry(id string) error {
	db, err := s.agentDatabase()
	if err != nil {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("store: delete kb entry: %w", err)
	}
	defer tx.Rollback()
	res, err := tx.Exec(`DELETE FROM kb_entry WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("store: delete kb entry: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrKBEntryNotFound
	}
	if _, err := tx.Exec(`DELETE FROM kb_field_lock WHERE entry_id = ?`, id); err != nil {
		return fmt.Errorf("store: delete kb entry locks: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM kb_relation WHERE from_entry_id = ? OR to_entry_id = ?`, id, id); err != nil {
		return fmt.Errorf("store: delete kb entry relations: %w", err)
	}
	return tx.Commit()
}

// ---- 字段锁 ----

// SetKBFieldLocks 以全量替换语义写入某词条的锁定字段。
func (s *Store) SetKBFieldLocks(entryID string, fields []string) error {
	db, err := s.agentDatabase()
	if err != nil {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("store: set kb locks: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM kb_field_lock WHERE entry_id = ?`, entryID); err != nil {
		return fmt.Errorf("store: clear kb locks: %w", err)
	}
	now := unixMillis(time.Now().UTC())
	seen := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		if _, ok := seen[field]; ok {
			continue
		}
		seen[field] = struct{}{}
		if _, err := tx.Exec(`INSERT INTO kb_field_lock (entry_id, field, created_at) VALUES (?,?,?)`,
			entryID, field, now); err != nil {
			return fmt.Errorf("store: insert kb lock: %w", err)
		}
	}
	return tx.Commit()
}

func (s *Store) LoadKBFieldLocks(entryID string) ([]string, error) {
	all, err := s.LoadAllFieldLocks()
	if err != nil {
		return nil, err
	}
	return all.Fields(entryID), nil
}

// LoadAllFieldLocks 读取全部锁定字段（分析链路每轮读取一次）。
func (s *Store) LoadAllFieldLocks() (novel.FieldLockSet, error) {
	db, err := s.agentDatabase()
	if err != nil {
		return nil, err
	}
	rows, err := db.Query(`SELECT entry_id, field FROM kb_field_lock ORDER BY entry_id, field`)
	if err != nil {
		return nil, fmt.Errorf("store: load kb locks: %w", err)
	}
	defer rows.Close()
	out := novel.FieldLockSet{}
	for rows.Next() {
		var entryID, field string
		if err := rows.Scan(&entryID, &field); err != nil {
			return nil, fmt.Errorf("store: scan kb lock: %w", err)
		}
		if out[entryID] == nil {
			out[entryID] = map[string]struct{}{}
		}
		out[entryID][field] = struct{}{}
	}
	return out, rows.Err()
}

// LoadFieldLocksIfPresent 在 agent.db 尚不存在时返回空锁集，避免只读分析创建数据库。
func (s *Store) LoadFieldLocksIfPresent() (novel.FieldLockSet, error) {
	if _, err := os.Stat(filepath.Join(s.dir, "agent.db")); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return novel.FieldLockSet{}, nil
		}
		return nil, err
	}
	return s.LoadAllFieldLocks()
}

// ---- 关系 ----

func (s *Store) SaveRelation(rel novel.Relation) (novel.Relation, error) {
	rel.Normalize(time.Now().UTC())
	if err := rel.Validate(); err != nil {
		return novel.Relation{}, err
	}
	db, err := s.agentDatabase()
	if err != nil {
		return novel.Relation{}, err
	}
	_, err = db.Exec(`INSERT INTO kb_relation
		(id, from_entry_id, to_entry_id, relation_type, label, description, status, start_chapter, end_chapter,
		 source_refs_json, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET
			from_entry_id = excluded.from_entry_id,
			to_entry_id = excluded.to_entry_id,
			relation_type = excluded.relation_type,
			label = excluded.label,
			description = excluded.description,
			status = excluded.status,
			start_chapter = excluded.start_chapter,
			end_chapter = excluded.end_chapter,
			source_refs_json = excluded.source_refs_json,
			updated_at = excluded.updated_at`,
		rel.ID, rel.FromEntryID, rel.ToEntryID, rel.Type, rel.Label, rel.Description, string(rel.Status),
		rel.StartChapter, rel.EndChapter, jsonString(rel.SourceRefs, "[]"),
		unixMillis(rel.CreatedAt), unixMillis(rel.UpdatedAt),
	)
	if err != nil {
		return novel.Relation{}, fmt.Errorf("store: save relation: %w", err)
	}
	return s.GetRelation(rel.ID)
}

func (s *Store) GetRelation(id string) (novel.Relation, error) {
	db, err := s.agentDatabase()
	if err != nil {
		return novel.Relation{}, err
	}
	var rel novel.Relation
	var status, refsJSON string
	var created, updated int64
	err = db.QueryRow(`SELECT id, from_entry_id, to_entry_id, relation_type, label, description, status,
		start_chapter, end_chapter, source_refs_json, created_at, updated_at FROM kb_relation WHERE id = ?`, id).
		Scan(&rel.ID, &rel.FromEntryID, &rel.ToEntryID, &rel.Type, &rel.Label, &rel.Description, &status,
			&rel.StartChapter, &rel.EndChapter, &refsJSON, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return novel.Relation{}, ErrRelationNotFound
	}
	if err != nil {
		return novel.Relation{}, fmt.Errorf("store: get relation: %w", err)
	}
	rel.Status = novel.RelationStatus(status)
	_ = json.Unmarshal([]byte(refsJSON), &rel.SourceRefs)
	rel.CreatedAt = fromUnixMillis(created)
	rel.UpdatedAt = fromUnixMillis(updated)
	return rel, nil
}

// ListRelations 返回关系边；entryID 为空返回全部，否则返回与之相连的边。
func (s *Store) ListRelations(entryID, status string) ([]novel.Relation, error) {
	db, err := s.agentDatabase()
	if err != nil {
		return nil, err
	}
	query := `SELECT id, from_entry_id, to_entry_id, relation_type, label, description, status,
		start_chapter, end_chapter, source_refs_json, created_at, updated_at FROM kb_relation WHERE 1=1`
	args := []any{}
	if value := strings.TrimSpace(entryID); value != "" {
		query += ` AND (from_entry_id = ? OR to_entry_id = ?)`
		args = append(args, value, value)
	}
	if value := strings.TrimSpace(status); value != "" {
		query += ` AND status = ?`
		args = append(args, value)
	}
	query += ` ORDER BY start_chapter ASC, created_at ASC, id ASC`
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list relations: %w", err)
	}
	defer rows.Close()
	out := []novel.Relation{}
	for rows.Next() {
		var rel novel.Relation
		var statusValue, refsJSON string
		var created, updated int64
		if err := rows.Scan(&rel.ID, &rel.FromEntryID, &rel.ToEntryID, &rel.Type, &rel.Label, &rel.Description,
			&statusValue, &rel.StartChapter, &rel.EndChapter, &refsJSON, &created, &updated); err != nil {
			return nil, fmt.Errorf("store: scan relation: %w", err)
		}
		rel.Status = novel.RelationStatus(statusValue)
		_ = json.Unmarshal([]byte(refsJSON), &rel.SourceRefs)
		rel.CreatedAt = fromUnixMillis(created)
		rel.UpdatedAt = fromUnixMillis(updated)
		out = append(out, rel)
	}
	return out, rows.Err()
}

func (s *Store) DeleteRelation(id string) error {
	db, err := s.agentDatabase()
	if err != nil {
		return err
	}
	res, err := db.Exec(`DELETE FROM kb_relation WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("store: delete relation: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrRelationNotFound
	}
	return nil
}

// ---- 时间线（人工补录侧；记忆事件在 service 层合并） ----

func (s *Store) SaveTimelineEvent(event novel.TimelineEvent) (novel.TimelineEvent, error) {
	event.Normalize(time.Now().UTC())
	if event.Source == "" {
		event.Source = novel.TimelineManual
	}
	if event.Source != novel.TimelineManual {
		return novel.TimelineEvent{}, fmt.Errorf("%w: only manual timeline events are writable", novel.ErrKBValidation)
	}
	if event.ID == "" {
		event.ID = novel.NewID("tl")
	}
	if err := event.Validate(); err != nil {
		return novel.TimelineEvent{}, err
	}
	db, err := s.agentDatabase()
	if err != nil {
		return novel.TimelineEvent{}, err
	}
	_, err = db.Exec(`INSERT INTO kb_timeline_event
		(id, chapter_id, chapter_index, title, summary, event_type, participants_json, entities_json,
		 source, source_event_id, story_order, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET
			chapter_id = excluded.chapter_id,
			chapter_index = excluded.chapter_index,
			title = excluded.title,
			summary = excluded.summary,
			event_type = excluded.event_type,
			participants_json = excluded.participants_json,
			entities_json = excluded.entities_json,
			source_event_id = excluded.source_event_id,
			story_order = excluded.story_order,
			updated_at = excluded.updated_at`,
		event.ID, event.ChapterID, event.ChapterIndex, event.Title, event.Summary, event.EventType,
		jsonString(event.Participants, "[]"), jsonString(event.Entities, "[]"), string(event.Source),
		event.SourceEventID, event.StoryOrder, unixMillis(event.CreatedAt), unixMillis(event.UpdatedAt),
	)
	if err != nil {
		return novel.TimelineEvent{}, fmt.Errorf("store: save timeline event: %w", err)
	}
	return event, nil
}

func (s *Store) ListManualTimelineEvents() ([]novel.TimelineEvent, error) {
	db, err := s.agentDatabase()
	if err != nil {
		return nil, err
	}
	rows, err := db.Query(`SELECT id, chapter_id, chapter_index, title, summary, event_type,
		participants_json, entities_json, source, source_event_id, story_order, created_at, updated_at
		FROM kb_timeline_event WHERE source = 'manual' ORDER BY chapter_index ASC, story_order ASC, id ASC`)
	if err != nil {
		return nil, fmt.Errorf("store: list timeline: %w", err)
	}
	defer rows.Close()
	out := []novel.TimelineEvent{}
	for rows.Next() {
		var event novel.TimelineEvent
		var participantsJSON, entitiesJSON, source string
		var created, updated int64
		if err := rows.Scan(&event.ID, &event.ChapterID, &event.ChapterIndex, &event.Title, &event.Summary,
			&event.EventType, &participantsJSON, &entitiesJSON, &source, &event.SourceEventID,
			&event.StoryOrder, &created, &updated); err != nil {
			return nil, fmt.Errorf("store: scan timeline: %w", err)
		}
		event.Source = novel.TimelineSource(source)
		_ = json.Unmarshal([]byte(participantsJSON), &event.Participants)
		_ = json.Unmarshal([]byte(entitiesJSON), &event.Entities)
		event.CreatedAt = fromUnixMillis(created)
		event.UpdatedAt = fromUnixMillis(updated)
		out = append(out, event)
	}
	return out, rows.Err()
}

func (s *Store) DeleteTimelineEvent(id string) error {
	db, err := s.agentDatabase()
	if err != nil {
		return err
	}
	res, err := db.Exec(`DELETE FROM kb_timeline_event WHERE id = ? AND source = 'manual'`, id)
	if err != nil {
		return fmt.Errorf("store: delete timeline event: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrTimelineNotFound
	}
	return nil
}

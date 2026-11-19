package store

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"novelagent/internal/novel"
)

// knowledge.json 真源（角色/设定）的人工编辑入口。
// 人工编辑不触发重分析；是否覆盖后续分析由 kb_field_lock 决定。

var ErrKnowledgeEntryNotFound = errors.New("store: knowledge entry not found")

// UpdateKnowledgeEntry 就地更新角色或世界设定的可编辑字段。
// 扩展类型（势力/地点/物品）走 SaveKBEntry。
func (s *Store) UpdateKnowledgeEntry(patch novel.KBEntry) (novel.KBEntry, error) {
	kind := patch.Kind
	if kind == "" {
		kind = novel.EntryKindForID(patch.ID)
	}
	if kind.Extended() {
		return s.SaveKBEntry(patch)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	knowledge, err := s.loadKnowledgeLocked()
	if err != nil {
		return novel.KBEntry{}, err
	}
	switch kind {
	case novel.EntryCharacter:
		idx := -1
		for i := range knowledge.Characters {
			if knowledge.Characters[i].ID == patch.ID {
				idx = i
				break
			}
		}
		if idx < 0 {
			return novel.KBEntry{}, ErrKnowledgeEntryNotFound
		}
		character := &knowledge.Characters[idx]
		if name := strings.TrimSpace(patch.Name); name != "" {
			character.Name = name
		}
		applyCharacterFields(character, patch.Fields)
		knowledge.UpdatedAt = time.Now().UTC()
		if err := s.writeKnowledgeLocked(knowledge); err != nil {
			return novel.KBEntry{}, err
		}
		return ViewCharacter(*character), nil
	case novel.EntrySetting:
		idx := -1
		for i := range knowledge.World {
			if knowledge.World[i].ID == patch.ID {
				idx = i
				break
			}
		}
		if idx < 0 {
			return novel.KBEntry{}, ErrKnowledgeEntryNotFound
		}
		entry := &knowledge.World[idx]
		if title := strings.TrimSpace(patch.Name); title != "" {
			entry.Title = title
		}
		applySettingFields(entry, patch.Fields)
		knowledge.UpdatedAt = time.Now().UTC()
		if err := s.writeKnowledgeLocked(knowledge); err != nil {
			return novel.KBEntry{}, err
		}
		return ViewSetting(*entry), nil
	default:
		return novel.KBEntry{}, fmt.Errorf("%w: kind %q is not editable here", novel.ErrKBValidation, kind)
	}
}

// DeleteKnowledgeEntry 人工删除角色/设定词条；扩展词条走 DeleteKBEntry。
func (s *Store) DeleteKnowledgeEntry(id string) error {
	kind := novel.EntryKindForID(id)
	if kind.Extended() {
		return s.DeleteKBEntry(id)
	}

	s.mu.Lock()
	knowledge, err := s.loadKnowledgeLocked()
	if err != nil {
		s.mu.Unlock()
		return err
	}
	found := false
	switch kind {
	case novel.EntryCharacter:
		next := knowledge.Characters[:0]
		for _, item := range knowledge.Characters {
			if item.ID == id {
				found = true
				continue
			}
			next = append(next, item)
		}
		knowledge.Characters = next
	case novel.EntrySetting:
		next := knowledge.World[:0]
		for _, item := range knowledge.World {
			if item.ID == id {
				found = true
				continue
			}
			next = append(next, item)
		}
		knowledge.World = next
	default:
		s.mu.Unlock()
		return ErrKnowledgeEntryNotFound
	}
	if !found {
		s.mu.Unlock()
		return ErrKnowledgeEntryNotFound
	}
	knowledge.UpdatedAt = time.Now().UTC()
	err = s.writeKnowledgeLocked(knowledge)
	s.mu.Unlock()
	if err != nil {
		return err
	}

	// 清理锁与关系边；失败不回滚词条删除，避免留下不可见的悬挂边。
	if err := s.clearEntryLinks(id); err != nil {
		return err
	}
	return nil
}

func (s *Store) clearEntryLinks(id string) error {
	db, err := s.agentDatabase()
	if err != nil {
		return err
	}
	if _, err := db.Exec(`DELETE FROM kb_field_lock WHERE entry_id = ?`, id); err != nil {
		return fmt.Errorf("store: clear entry locks: %w", err)
	}
	if _, err := db.Exec(`DELETE FROM kb_relation WHERE from_entry_id = ? OR to_entry_id = ?`, id, id); err != nil {
		return fmt.Errorf("store: clear entry relations: %w", err)
	}
	return nil
}

func applyCharacterFields(character *novel.Character, fields map[string]string) {
	for key, value := range fields {
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "role":
			character.Role = value
		case "personality":
			character.Personality = value
		case "appearance":
			character.Appearance = value
		case "relationships":
			character.Relationships = value
		case "state":
			character.State = value
		case "notes":
			character.Notes = value
		}
	}
}

func applySettingFields(entry *novel.WorldEntry, fields map[string]string) {
	for key, value := range fields {
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "category":
			entry.Category = value
		case "content":
			entry.Content = value
		}
	}
}

func ViewCharacter(character novel.Character) novel.KBEntry {
	return novel.KBEntry{
		ID:      character.ID,
		Kind:    novel.EntryCharacter,
		Name:    character.Name,
		Aliases: character.Aliases,
		Summary: character.Notes,
		Fields: map[string]string{
			"role":          character.Role,
			"personality":   character.Personality,
			"appearance":    character.Appearance,
			"relationships": character.Relationships,
			"state":         character.State,
			"notes":         character.Notes,
		},
		Confidence: character.Confidence,
		LastSeen:   character.LastSeen,
		Evidence:   character.Evidence,
	}
}

func ViewSetting(entry novel.WorldEntry) novel.KBEntry {
	return novel.KBEntry{
		ID:      entry.ID,
		Kind:    novel.EntrySetting,
		Name:    entry.Title,
		Aliases: entry.Aliases,
		Summary: entry.Content,
		Fields: map[string]string{
			"category": entry.Category,
			"content":  entry.Content,
		},
		Confidence: entry.Confidence,
		LastSeen:   entry.LastSeen,
		Evidence:   entry.Evidence,
	}
}

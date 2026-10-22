package store

import (
	"errors"
	"fmt"
	"os"

	"novelagent/internal/novel"
)

// 记忆查询（docs/memory_model_spec.md §12.1/§12.2）。
// 锁时序：store.mu 与 memoryMu 顺序获取、不嵌套（见 memory_init.go 顶部约定）。

var ErrCharacterNotFound = errors.New("store: character not found")

// MemoryCounts 为五类记忆记录计数。
type MemoryCounts struct {
	Events     int `json:"events"`
	States     int `json:"states"`
	Cognitions int `json:"cognitions"`
	Threads    int `json:"threads"`
	Plans      int `json:"plans"`
}

// MemoryOverview 为 GET /api/memory 响应（§12.1）。
type MemoryOverview struct {
	WorkID         string       `json:"workId"`
	SchemaVersion  int          `json:"schemaVersion"`
	MemoryRevision int64        `json:"memoryRevision"`
	LastCommitID   string       `json:"lastCommitId,omitempty"`
	InitStatus     string       `json:"initStatus"`
	Counts         MemoryCounts `json:"counts"`
	SafeState      bool         `json:"safeState"`
	SafeStateError string       `json:"safeStateError,omitempty"`
}

// LoadMemoryOverview 返回记忆概览；文件不存在 → 全 0 + not_started（U5）。
func (s *Store) LoadMemoryOverview() (MemoryOverview, error) {
	s.memoryMu.Lock()
	defer s.memoryMu.Unlock()

	meta, err := s.ensureMemoryMetaLocked()
	if err != nil {
		return MemoryOverview{}, err
	}
	events, err := s.loadMemoryEventsLocked()
	if err != nil {
		return MemoryOverview{}, err
	}
	states, err := s.loadMemoryStatesLocked()
	if err != nil {
		return MemoryOverview{}, err
	}
	cognitions, err := s.loadMemoryCognitionsLocked()
	if err != nil {
		return MemoryOverview{}, err
	}
	threads, err := s.loadMemoryThreadsLocked()
	if err != nil {
		return MemoryOverview{}, err
	}
	plans, err := s.loadMemoryPlansLocked()
	if err != nil {
		return MemoryOverview{}, err
	}
	initState, err := s.loadMemoryInitStateLocked()
	if err != nil {
		return MemoryOverview{}, err
	}

	overview := MemoryOverview{
		WorkID:         novel.InternalWorkID(),
		SchemaVersion:  meta.SchemaVersion,
		MemoryRevision: meta.MemoryRevision,
		LastCommitID:   meta.LastCommitID,
		InitStatus:     initState.Status,
		SafeState:      s.memorySafe,
		SafeStateError: s.memorySafeError,
		Counts: MemoryCounts{
			Events:     len(events),
			States:     len(states),
			Cognitions: len(cognitions),
			Threads:    len(threads),
			Plans:      len(plans),
		},
	}
	return overview, nil
}

// CharacterEventSummary 为角色参与事件的摘要视图（§12.2 events(参与事件摘要)）。
type CharacterEventSummary struct {
	ID         string `json:"id"`
	Summary    string `json:"summary"`
	Status     string `json:"status"`
	StoryOrder int    `json:"storyOrder"`
	ChapterID  string `json:"chapterId,omitempty"`
}

// CharacterMemoryView 为 GET /api/memory/characters/{id} 响应（§12.2）。
type CharacterMemoryView struct {
	Character  novel.Character         `json:"character"`
	States     []novel.StoryState      `json:"states"`
	Cognitions []novel.Cognition       `json:"cognitions"`
	Events     []CharacterEventSummary `json:"events"`
}

// ValidCharacterID 校验 c_ 前缀实体 ID（结构校验，前缀不符 → 400）。
func ValidCharacterID(id string) bool {
	return novel.ValidID(id) && len(id) > 2 && id[:2] == "c_"
}

// LoadCharacterMemory 聚合角色记忆视图：character 来自 knowledge（store.mu），
// states/cognitions/events 来自 memory（memoryMu）；两锁顺序获取、不嵌套。
func (s *Store) LoadCharacterMemory(id string) (CharacterMemoryView, error) {
	if !ValidCharacterID(id) {
		return CharacterMemoryView{}, fmt.Errorf("%w: %q is not a c_ id", ErrInvalidChapter, id)
	}

	s.mu.Lock()
	knowledge, err := s.loadKnowledgeLocked()
	s.mu.Unlock()
	if err != nil {
		return CharacterMemoryView{}, err
	}
	var character *novel.Character
	for i := range knowledge.Characters {
		if knowledge.Characters[i].ID == id {
			character = &knowledge.Characters[i]
			break
		}
	}
	if character == nil {
		return CharacterMemoryView{}, fmt.Errorf("%w: %s", ErrCharacterNotFound, id)
	}

	s.memoryMu.Lock()
	defer s.memoryMu.Unlock()

	states, err := s.loadMemoryStatesLocked()
	if err != nil {
		return CharacterMemoryView{}, err
	}
	cognitions, err := s.loadMemoryCognitionsLocked()
	if err != nil {
		return CharacterMemoryView{}, err
	}
	events, err := s.loadMemoryEventsLocked()
	if err != nil {
		return CharacterMemoryView{}, err
	}

	view := CharacterMemoryView{Character: *character}
	for _, state := range states {
		if state.SubjectID == id && state.Status == novel.StateActive {
			view.States = append(view.States, state)
		}
	}
	for _, cog := range cognitions {
		if cog.SubjectID == id {
			view.Cognitions = append(view.Cognitions, cog)
		}
	}
	for _, event := range events {
		for _, participant := range event.Participants {
			if participant == id {
				view.Events = append(view.Events, CharacterEventSummary{
					ID:         event.ID,
					Summary:    event.Summary,
					Status:     string(event.Status),
					StoryOrder: event.StoryOrder,
					ChapterID:  event.ChapterID,
				})
				break
			}
		}
	}
	if view.States == nil {
		view.States = []novel.StoryState{}
	}
	if view.Cognitions == nil {
		view.Cognitions = []novel.Cognition{}
	}
	if view.Events == nil {
		view.Events = []CharacterEventSummary{}
	}
	return view, nil
}

// ---- 其余三类记忆文件读取（*Locked，供计数与查询）----

func (s *Store) loadMemoryCognitionsLocked() ([]novel.Cognition, error) {
	var cognitions []novel.Cognition
	err := s.readMemoryFileLocked("cognitions.json", &cognitions)
	if errors.Is(err, os.ErrNotExist) {
		return []novel.Cognition{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("store: load memory cognitions: %w", err)
	}
	return cognitions, nil
}

func (s *Store) loadMemoryThreadsLocked() ([]novel.StoryThread, error) {
	var threads []novel.StoryThread
	err := s.readMemoryFileLocked("threads.json", &threads)
	if errors.Is(err, os.ErrNotExist) {
		return []novel.StoryThread{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("store: load memory threads: %w", err)
	}
	return threads, nil
}

func (s *Store) loadMemoryPlansLocked() ([]novel.StoryPlan, error) {
	var plans []novel.StoryPlan
	err := s.readMemoryFileLocked("plans.json", &plans)
	if errors.Is(err, os.ErrNotExist) {
		return []novel.StoryPlan{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("store: load memory plans: %w", err)
	}
	return plans, nil
}

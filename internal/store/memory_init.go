package store

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"novelagent/internal/novel"
)

// Memory Initializer（docs/memory_model_spec.md V1.2 冻结版 §9 / R5）。
// 两级初始化的第一级：knowledge → 记忆骨架回填（幂等、origin 查重、manual_edit 保护）。
//
// 锁时序约定：本文件所有路径都遵循"store.mu 与 memoryMu 不嵌套"——
// InitializeMemory 持 memoryMu 时调用 CheckMemoryPreconditions（内部取 store.mu）是
// 全仓库唯一 memoryMu→store.mu 持序点；其余路径均为"先 store.mu 读、释放后 memoryMu 写"，
// 不存在 store.mu→memoryMu 反序持锁，无死锁。

const memoryInitStateFile = "init_state.json"

// 初始化状态机（§9.2）。
const (
	InitNotStarted     = "not_started"
	InitMapping        = "mapping"
	InitDeepExtracting = "deep_extracting"
	InitReady          = "ready"
	InitPartial        = "partial"
	InitFailed         = "failed"
)

var (
	ErrProjectNotMigrated = errors.New("store: project not migrated")
	ErrKnowledgeNotReady  = errors.New("store: knowledge not ready")
	ErrAlreadyRunning     = errors.New("store: memory initialization already running")
	ErrAlreadyInitialized = errors.New("store: memory already initialized")
)

// MemoryInitPreconditions 为 R5 前置检查结果。
type MemoryInitPreconditions struct {
	Migrated       bool `json:"migrated"`
	KnowledgeReady bool `json:"knowledgeReady"`
}

// MemoryInitStats 为回填统计（§9.2）。
type MemoryInitStats struct {
	MappedEvents           int `json:"mappedEvents"`
	MappedStates           int `json:"mappedStates"`
	SkippedInvalid         int `json:"skippedInvalid"`
	SkippedManualProtected int `json:"skippedManualProtected"`
	SkippedExistingActive  int `json:"skippedExistingActive"`
}

// MemoryInitState 为 memory/init_state.json 的磁盘结构。
type MemoryInitState struct {
	Status        string                  `json:"status"`
	StartedAt     time.Time               `json:"startedAt,omitempty"`
	FinishedAt    time.Time               `json:"finishedAt,omitempty"`
	LastError     string                  `json:"lastError,omitempty"`
	Preconditions MemoryInitPreconditions `json:"preconditions"`
	Stats         MemoryInitStats         `json:"stats"`
}

// LoadMemoryInitState 读取初始化状态；文件不存在 → not_started。
func (s *Store) LoadMemoryInitState() (MemoryInitState, error) {
	s.memoryMu.Lock()
	defer s.memoryMu.Unlock()
	return s.loadMemoryInitStateLocked()
}

func (s *Store) loadMemoryInitStateLocked() (MemoryInitState, error) {
	var state MemoryInitState
	err := s.readMemoryFileLocked(memoryInitStateFile, &state)
	if errors.Is(err, os.ErrNotExist) {
		return MemoryInitState{Status: InitNotStarted}, nil
	}
	if err != nil {
		return MemoryInitState{}, err
	}
	if state.Status == "" {
		state.Status = InitNotStarted
	}
	return state, nil
}

func (s *Store) writeMemoryInitStateLocked(state MemoryInitState) error {
	return s.writeMemoryFileLocked(memoryInitStateFile, state)
}

// resetOrphanInitStateLocked 为启动恢复附带动作（阻塞修复 #1）：
// 进程重启后不可能存在在途映射，孤儿 mapping/deep_extracting 状态重置为 failed，
// 使 §9.3 的"失败后续跑"路径可用（否则 initialize 将永远 409 already_running）。
func (s *Store) resetOrphanInitStateLocked() {
	state, err := s.loadMemoryInitStateLocked()
	if err != nil {
		return
	}
	if state.Status != InitMapping && state.Status != InitDeepExtracting {
		return
	}
	state.Status = InitFailed
	state.FinishedAt = time.Now().UTC()
	state.LastError = "interrupted_by_restart"
	_ = s.writeMemoryInitStateLocked(state)
}

// CheckMemoryPreconditions 执行 R5 前置检查（§9.0 第 1/2 项）。
// 第 3 项（输入可解码）在映射阶段执行，失败置 failed（§12.3：202 先返回）。
func (s *Store) CheckMemoryPreconditions() (MemoryInitPreconditions, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.memoryPreconditionsLocked()
}

func (s *Store) memoryPreconditionsLocked() (MemoryInitPreconditions, error) {
	pre := MemoryInitPreconditions{Migrated: false, KnowledgeReady: true}

	// 检查 1：项目已迁移——chapters/ 目录存在且非空，或 project.json 无 legacy manuscript 字段
	entries, err := os.ReadDir(s.path(chaptersDirName))
	if err == nil {
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".json") {
				pre.Migrated = true
				break
			}
		}
	}
	if !pre.Migrated {
		var legacy struct {
			Manuscript string `json:"manuscript"`
		}
		if err := s.readJSONLocked(s.path("project.json"), &legacy); err != nil || strings.TrimSpace(legacy.Manuscript) == "" {
			// 无 legacy manuscript（或 project.json 不存在）→ 视为已迁移（空项目可初始化为空记忆）
			pre.Migrated = true
		} else {
			return pre, fmt.Errorf("%w: legacy project.json still contains manuscript (start service to auto-migrate)", ErrProjectNotMigrated)
		}
	}

	// 检查 2：知识可用——characters/world/summaries 至少一项非空
	knowledge, err := s.loadKnowledgeLocked()
	if err != nil {
		// 解码失败不在此处 409（§9.0 检查 3：启动后 failed）
		return pre, nil
	}
	if len(knowledge.Characters) == 0 && len(knowledge.World) == 0 && len(knowledge.Summaries) == 0 {
		return pre, fmt.Errorf("%w: run /api/analyze first", ErrKnowledgeNotReady)
	}
	return pre, nil
}

// InitializeMemory 启动低成本回填（§9.3/§12.3 语义，错误码由 API 层翻译）：
//   - ready → (status="ready", ErrAlreadyInitialized)：幂等重试，无副作用（U30）
//   - mapping/deep_extracting → (status, ErrAlreadyRunning)
//   - not_started/failed/partial → 置 mapping 并异步映射，返回 ("mapping", nil)
//   - 前置不满足 → ErrProjectNotMigrated / ErrKnowledgeNotReady（状态不变、零写入）
//
// 锁序（阻塞修复 #4 配套）：前置检查先独立取 store.mu，释放后才进 memoryMu——
// 全仓库持序唯一为 store.mu→memoryMu（import 重置路径），杜绝死锁。
func (s *Store) InitializeMemory() (string, error) {
	pre, err := s.CheckMemoryPreconditions()
	if err != nil {
		return "", err
	}

	s.memoryMu.Lock()
	defer s.memoryMu.Unlock()

	if s.memorySafe {
		return "", fmt.Errorf("%w: %s", ErrMemorySafeState, s.memorySafeError)
	}
	state, err := s.loadMemoryInitStateLocked()
	if err != nil {
		return "", err
	}
	switch state.Status {
	case InitReady:
		return InitReady, ErrAlreadyInitialized
	case InitMapping, InitDeepExtracting:
		return state.Status, ErrAlreadyRunning
	}

	now := time.Now().UTC()
	state.Status = InitMapping
	state.StartedAt = now
	state.FinishedAt = time.Time{}
	state.LastError = ""
	state.Preconditions = pre
	if err := s.writeMemoryInitStateLocked(state); err != nil {
		return "", err
	}
	go s.runMemoryMapping()
	return InitMapping, nil
}

// runMemoryMapping 执行映射（异步；不持任何锁进入）。panic 转为 failed 状态而非带崩进程。
func (s *Store) runMemoryMapping() {
	defer func() {
		if r := recover(); r != nil {
			s.failMemoryInit(fmt.Errorf("mapping panicked: %v", r))
		}
	}()
	knowledge, err := s.LoadKnowledge()
	if err != nil {
		s.failMemoryInit(fmt.Errorf("load knowledge: %w", err))
		return
	}
	if _, err := s.LoadProject(); err != nil {
		s.failMemoryInit(fmt.Errorf("load project: %w", err))
		return
	}
	chapters, err := s.LoadChapters()
	if err != nil {
		s.failMemoryInit(fmt.Errorf("load chapters: %w", err))
		return
	}
	indexByChapter := make(map[string]int, len(chapters))
	for _, chapter := range chapters {
		indexByChapter[chapter.ID] = chapter.Index
	}

	now := time.Now().UTC()
	stats := MemoryInitStats{}

	// ---- 第一步（持 memoryMu）：origin 查重 + 构造新记录 + L-S 过滤 ----
	s.memoryMu.Lock()
	events, err := s.loadMemoryEventsLocked()
	if err != nil {
		s.memoryMu.Unlock()
		s.failMemoryInit(err)
		return
	}
	states, err := s.loadMemoryStatesLocked()
	if err != nil {
		s.memoryMu.Unlock()
		s.failMemoryInit(err)
		return
	}
	existingTotal := len(events) + len(states)

	eventOrigins := make(map[string]struct{}, len(events))
	for i := range events {
		eventOrigins[originDedupKey(events[i].Origin, "")] = struct{}{}
	}
	stateOrigins := make(map[string]struct{}, len(states))
	activeStates := make(map[string]*novel.StoryState, len(states))
	for i := range states {
		stateOrigins[originDedupKey(states[i].Origin, states[i].SubjectID)] = struct{}{}
		if states[i].Status == novel.StateActive {
			activeStates[stateTripleKey(states[i].SubjectID, states[i].Category, states[i].Attribute)] = &states[i]
		}
	}

	var newEvents []novel.StoryEvent
	var newStates []novel.StoryState

	// 映射 1：knowledge.Summaries → proposed 事件骨架（R1：无正文证据 → proposed）
	for _, summary := range knowledge.Summaries {
		key := originDedupKey(novel.Origin{Type: novel.OriginBackfill, Chapter: summary.ChapterID, Kind: "summary"}, "")
		if _, dup := eventOrigins[key]; dup {
			continue
		}
		index, ok := indexByChapter[summary.ChapterID]
		if !ok {
			stats.SkippedInvalid++ // L-S：来源章节不存在
			continue
		}
		event := novel.StoryEvent{
			MemoryMeta: novel.MemoryMeta{
				RecordVersion: 1,
				CreatedAt:     now,
				UpdatedAt:     now,
				Confidence:    0.3,
				NeedsReview:   true,
				Origin:        novel.Origin{Type: novel.OriginBackfill, Chapter: summary.ChapterID, Kind: "summary"},
				LastSource:    novel.LastSourceBackfill,
			},
			Status:            novel.EventProposed,
			EventType:         string(novel.EventOther),
			Summary:           summary.Summary,
			ChapterID:         summary.ChapterID,
			StoryOrder:        index,
			ParticipantsKnown: false, // 冻结决策 12
		}
		event.Normalize(now)
		if err := novel.ValidateStoryEvent(&event); err != nil {
			stats.SkippedInvalid++ // 回填持久化边界：单条 L-S 失败跳过（§4.7）
			continue
		}
		eventOrigins[key] = struct{}{}
		newEvents = append(newEvents, event)
		stats.MappedEvents++
	}

	// 映射 2：Character.State → active StoryState（无证据 → needsReview，R1）
	for _, character := range knowledge.Characters {
		value := strings.TrimSpace(character.State)
		if value == "" {
			continue
		}
		key := originDedupKey(novel.Origin{Type: novel.OriginBackfill, Kind: "character_state"}, character.ID)
		if _, dup := stateOrigins[key]; dup {
			continue
		}
		// manual_edit 保护（§9.4.3 / U7）：同三元组 active 且人工修订 → 跳过
		triple := stateTripleKey(character.ID, "general", "profile")
		if existing, ok := activeStates[triple]; ok {
			if existing.LastSource == novel.LastSourceManualEdit {
				stats.SkippedManualProtected++
				continue
			}
			if existing.Value == value {
				continue // 同值 no-op（R6/U29）
			}
			stats.SkippedExistingActive++ // 回填永不替代既有 active
			continue
		}
		state := novel.StoryState{
			MemoryMeta: novel.MemoryMeta{
				RecordVersion: 1,
				CreatedAt:     now,
				UpdatedAt:     now,
				Confidence:    0.5,
				NeedsReview:   true, // R1：无 sourceRefs 的 active 必须 needsReview
				ReviewReason:  "backfill_unverified",
				Origin:        novel.Origin{Type: novel.OriginBackfill, Kind: "character_state"},
				LastSource:    novel.LastSourceBackfill,
			},
			Status:    novel.StateActive,
			SubjectID: character.ID, // U8：复用既有 c_ ID
			Category:  "general",    // 冻结决策 12
			Attribute: "profile",
			Value:     value,
		}
		state.Normalize(now)
		if err := novel.ValidateStoryState(&state); err != nil {
			stats.SkippedInvalid++
			continue
		}
		stateOrigins[key] = struct{}{}
		activeStates[triple] = &state
		newStates = append(newStates, state)
		stats.MappedStates++
	}

	state, err := s.loadMemoryInitStateLocked()
	if err != nil {
		s.memoryMu.Unlock()
		s.failMemoryInit(err)
		return
	}

	// R5 红线：零结果不得 ready（U28）
	if stats.MappedEvents+stats.MappedStates == 0 && existingTotal == 0 {
		state.Status = InitPartial
		state.FinishedAt = time.Now().UTC()
		state.LastError = "no_mappable_input"
		state.Stats = stats
		if err := s.writeMemoryInitStateLocked(state); err != nil {
			s.memoryMu.Unlock()
			s.failMemoryInit(err)
			return
		}
		s.memoryMu.Unlock()
		return
	}

	// ---- 第二步：有新记录 → initialize 提交（memoryRevision+1，§8.3）----
	if stats.MappedEvents+stats.MappedStates > 0 {
		meta, err := s.ensureMemoryMetaLocked()
		if err != nil {
			s.memoryMu.Unlock()
			s.failMemoryInit(err)
			return
		}
		payload := map[string]any{}
		if len(newEvents) > 0 {
			payload["events.json"] = append(events, newEvents...)
		}
		if len(newStates) > 0 {
			payload["states.json"] = append(states, newStates...)
		}
		if _, err := s.commitMemoryLocked(CommitInitialize, meta.MemoryRevision, payload); err != nil {
			s.memoryMu.Unlock()
			s.failMemoryInit(err)
			return
		}
	}

	// 终态判定（§9.2）：部分来源为空或存在跳过 → partial；全部干净 → ready
	sourceGaps := len(knowledge.Summaries) == 0 || !knowledgeHasCharacterState(knowledge)
	switch {
	case stats.SkippedInvalid > 0 || stats.SkippedManualProtected > 0 || stats.SkippedExistingActive > 0 || sourceGaps:
		state.Status = InitPartial
	default:
		state.Status = InitReady
	}
	state.FinishedAt = time.Now().UTC()
	state.LastError = ""
	state.Stats = stats
	if err := s.writeMemoryInitStateLocked(state); err != nil {
		s.memoryMu.Unlock()
		s.failMemoryInit(err)
		return
	}
	s.memoryMu.Unlock()
}

func (s *Store) failMemoryInit(err error) {
	s.memoryMu.Lock()
	defer s.memoryMu.Unlock()
	state, err2 := s.loadMemoryInitStateLocked()
	if err2 != nil {
		return
	}
	state.Status = InitFailed
	state.FinishedAt = time.Now().UTC()
	state.LastError = truncate(err.Error(), 200)
	_ = s.writeMemoryInitStateLocked(state)
}

func knowledgeHasCharacterState(knowledge novel.Knowledge) bool {
	for _, character := range knowledge.Characters {
		if strings.TrimSpace(character.State) != "" {
			return true
		}
	}
	return false
}

func originDedupKey(origin novel.Origin, subjectID string) string {
	// §9.4.1：(origin.type, origin.chapterId, origin.kind, subjectId)
	return string(origin.Type) + "|" + origin.Chapter + "|" + origin.Kind + "|" + subjectID
}

func stateTripleKey(subjectID, category, attribute string) string {
	// §4.2 唯一键：(subjectId, category, attribute)
	return subjectID + "|" + category + "|" + attribute
}

// resetMemoryLocked 为导入新书重置记忆子系统（阻塞修复 #4，与 knowledge 重置对称）：
// 五类记录/候选/提交日志/init_state 全部清空，meta 归零——防止旧书记录混入新书面板
// 或显示"初始化完成"。调用方持 store.mu 时获取 memoryMu（全仓库唯一持序方向 store.mu→memoryMu）。
func (s *Store) resetMemoryLocked() error {
	empties := map[string]any{
		"events.json":            []novel.StoryEvent{},
		"states.json":            []novel.StoryState{},
		"cognitions.json":        []novel.Cognition{},
		"threads.json":           []novel.StoryThread{},
		"plans.json":             []novel.StoryPlan{},
		"entity_candidates.json": []novel.EntityCandidate{},
		"commits.json":           []CommitRecord{},
	}
	for name, value := range empties {
		if err := s.writeMemoryFileLocked(name, value); err != nil {
			return err
		}
	}
	if err := s.writeMemoryMetaLocked(memoryMetaFile{
		SchemaVersion:  memorySchemaVersion,
		MemoryRevision: 0,
		UpdatedAt:      time.Now().UTC(),
	}); err != nil {
		return err
	}
	return s.writeMemoryInitStateLocked(MemoryInitState{Status: InitNotStarted})
}

// ---- StoryOrder 重算（§4.1.4 / 冻结决策 16；钩子契约 R9–R11）----

// handleChapterOrderChanged 为章节序号变化回调（在 store.mu 释放后执行，持 memoryMu）：
// 按 chapterId → 新 Index 批量重算事件 StoryOrder；来源章节被删除的事件置 needsReview
// （§13.2-1 初步建议：仅 needsReview，invalidated 留用户/reconcile）。变化留痕 reconcile 提交。
// 失败仅返回 error（由 notify 记日志），不得阻断章节操作（R9/R10）。
func (s *Store) handleChapterOrderChanged(oldIndexes, newIndexes map[string]int) error {
	s.memoryMu.Lock()
	defer s.memoryMu.Unlock()

	if s.memorySafe {
		return fmt.Errorf("%w: %s", ErrMemorySafeState, s.memorySafeError)
	}
	events, err := s.loadMemoryEventsLocked()
	if err != nil {
		return err
	}
	if len(events) == 0 {
		return nil
	}

	now := time.Now().UTC()
	changed := false
	for i := range events {
		event := &events[i]
		if event.ChapterID == "" {
			continue
		}
		index, ok := newIndexes[event.ChapterID]
		if !ok {
			// 来源章节被删除：标记待复核（needsReview 变化 → recordVersion+1，R8）
			if !event.NeedsReview || event.ReviewReason != "chapter_deleted" {
				event.NeedsReview = true
				event.ReviewReason = "chapter_deleted"
				event.StoryOrder = 0
				event.RecordVersion++
				event.UpdatedAt = now
				changed = true
			}
			continue
		}
		// StoryOrder 为可重建派生字段：纯重排不递增 recordVersion（R8 穷尽表未列）
		if event.StoryOrder != index {
			event.StoryOrder = index
			event.UpdatedAt = now
			changed = true
		}
	}
	if !changed {
		return nil
	}
	meta, err := s.ensureMemoryMetaLocked()
	if err != nil {
		return err
	}
	_, err = s.commitMemoryLocked(CommitReconcile, meta.MemoryRevision, map[string]any{"events.json": events})
	return err
}

// ---- memory 数据文件访问（*Locked）----

func (s *Store) loadMemoryEventsLocked() ([]novel.StoryEvent, error) {
	var events []novel.StoryEvent
	err := s.readMemoryFileLocked("events.json", &events)
	if errors.Is(err, os.ErrNotExist) {
		return []novel.StoryEvent{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("store: load memory events: %w", err)
	}
	return events, nil
}

func (s *Store) loadMemoryStatesLocked() ([]novel.StoryState, error) {
	var states []novel.StoryState
	err := s.readMemoryFileLocked("states.json", &states)
	if errors.Is(err, os.ErrNotExist) {
		return []novel.StoryState{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("store: load memory states: %w", err)
	}
	return states, nil
}

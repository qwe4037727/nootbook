package store

import (
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"novelagent/internal/novel"
)

// Memory Initializer（docs/memory_model_spec.md V1.2 冻结版 §9 / R5）。
// 两级初始化的第一级：knowledge → 记忆骨架回填（幂等、origin 查重、manual_edit 保护）。
//
// 锁时序约定（P1-3 阻塞修复后）：全仓库 store.mu 与 memoryMu 的唯一嵌套方向为
// store.mu→memoryMu（ImportChapters 持 store.mu 时经 resetMemoryLocked 获取 memoryMu）。
// InitializeMemory 的前置检查（内部取 store.mu）先执行并释放，之后才进 memoryMu，
// 不存在 memoryMu→store.mu 反序持锁；runMemoryMapping 也是先释放 store.mu 类读取、
// 后取 memoryMu 写。单向持序，无死锁。

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
// RunID 为本次初始化任务的运行号（授权时分配，docs/issues.md #18）：
// 导入新书或新任务授权后，旧在途任务凭此判定失效，不得写入新书记忆。
type MemoryInitState struct {
	Status        string                  `json:"status"`
	StartedAt     time.Time               `json:"startedAt,omitempty"`
	FinishedAt    time.Time               `json:"finishedAt,omitempty"`
	LastError     string                  `json:"lastError,omitempty"`
	RunID         uint64                  `json:"runId,omitempty"`
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
	// 授权新的初始化运行号（docs/issues.md #18）：旧的在途任务凭此判定失效，
	// 不得把旧书快照写入新书，也不得覆盖新任务的 init_state。
	s.initRunCounter++
	runID := s.initRunCounter
	s.initCurrentRun = runID
	token := initTaskToken{Generation: s.memoryGeneration, RunID: runID}

	state.Status = InitMapping
	state.StartedAt = now
	state.FinishedAt = time.Time{}
	state.LastError = ""
	state.RunID = runID
	state.Preconditions = pre
	if err := s.writeMemoryInitStateLocked(state); err != nil {
		return "", err
	}
	go s.runMemoryMappingFor(token)
	return InitMapping, nil
}

// initTaskToken 标识一次初始化任务的归属（docs/issues.md #18）：
//   - Generation：任务开始时的记忆代次——导入新书会递增代次，旧任务据此失效；
//   - RunID：授权该任务的运行号——新任务授权后，旧任务据此失效。
//
// 校验点在"提交前"与"终态/失败回写前"两处：任一不符即放弃本次任务，
// 旧书快照不得写入新书记忆，也不得覆盖新书/新任务的 init_state。
type initTaskToken struct {
	Generation uint64
	RunID      uint64
}

// initTokenValidLocked 校验任务归属是否仍然有效（调用方须持 memoryMu）：
// 代次一致（期间未发生导入重置）且运行号仍为当前授权（未被新任务接管）。
func (s *Store) initTokenValidLocked(token initTaskToken) bool {
	return s.memoryGeneration == token.Generation && s.initCurrentRun == token.RunID
}

// runMemoryMapping 为兼容入口（外部直接置位 mapping 状态时使用，如验收探针）：
// 以"当前代次 + 当前运行号"认领任务后执行映射。
// 生产路径由 InitializeMemory 显式携带令牌（runMemoryMappingFor），
// 避免"授权后、任务真正启动前"发生导入时误用新代次。
func (s *Store) runMemoryMapping() {
	s.memoryMu.Lock()
	state, err := s.loadMemoryInitStateLocked()
	token := initTaskToken{Generation: s.memoryGeneration, RunID: s.initCurrentRun}
	s.memoryMu.Unlock()
	if err != nil {
		return
	}
	if state.Status != InitMapping && state.Status != InitDeepExtracting {
		return
	}
	s.runMemoryMappingFor(token)
}

// runMemoryMappingFor 执行映射（异步；不持任何锁进入）。panic 转为 failed 状态而非带崩进程。
// token 为任务归属凭据：快照读取后、提交前、终态回写前均需核对（docs/issues.md #18）。
func (s *Store) runMemoryMappingFor(token initTaskToken) {
	defer func() {
		if r := recover(); r != nil {
			s.failMemoryInit(token, fmt.Errorf("mapping panicked: %v", r))
		}
	}()
	knowledge, err := s.LoadKnowledge()
	if err != nil {
		s.failMemoryInit(token, fmt.Errorf("load knowledge: %w", err))
		return
	}
	if _, err := s.LoadProject(); err != nil {
		s.failMemoryInit(token, fmt.Errorf("load project: %w", err))
		return
	}
	chapters, err := s.LoadChapters()
	if err != nil {
		s.failMemoryInit(token, fmt.Errorf("load chapters: %w", err))
		return
	}
	indexByChapter := make(map[string]int, len(chapters))
	for _, chapter := range chapters {
		indexByChapter[chapter.ID] = chapter.Index
	}

	// 持锁区段独立成方法：panic 展开时由 defer 释放 memoryMu，
	// 避免外层 recover 调用 failMemoryInit 时二次获取同一把非重入锁而自死锁。
	if err := s.mapMemoryUnderLock(token, knowledge, indexByChapter); err != nil {
		s.failMemoryInit(token, err)
	}
}

// mapMemoryUnderLock 在持有 memoryMu 的前提下完成映射与提交（docs/issues.md #18）：
// 快照读取后、提交前、终态回写前三处核对任务代次；任务已失效时不写任何数据。
func (s *Store) mapMemoryUnderLock(token initTaskToken, knowledge novel.Knowledge, indexByChapter map[string]int) error {
	s.memoryMu.Lock()
	defer s.memoryMu.Unlock()

	now := time.Now().UTC()
	stats := MemoryInitStats{}

	// 关键校验点（docs/issues.md #18）：knowledge/chapters 快照是在 store.mu 下读取的，
	// 其间可能已发生导入重置——快照已属旧书，立即放弃本次任务。
	if !s.initTokenValidLocked(token) {
		return errors.New("init task invalidated: book replaced before mapping")
	}
	events, err := s.loadMemoryEventsLocked()
	if err != nil {
		return err
	}
	states, err := s.loadMemoryStatesLocked()
	if err != nil {
		return err
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
		return err
	}

	// R5 红线：零结果不得 ready（U28）
	if stats.MappedEvents+stats.MappedStates == 0 && existingTotal == 0 {
		state.Status = InitPartial
		state.FinishedAt = time.Now().UTC()
		state.LastError = "no_mappable_input"
		state.Stats = stats
		return s.writeMemoryInitStateLocked(state)
	}

	// ---- 第二步：有新记录 → initialize 提交（memoryRevision+1，§8.3）----
	if stats.MappedEvents+stats.MappedStates > 0 {
		// 提交前再核对任务代次（docs/issues.md #18）：快照构造期间可能发生导入，
		// 此时提交会把旧书人物状态写入新书。
		if !s.initTokenValidLocked(token) {
			return errors.New("init task invalidated: book replaced before commit")
		}
		meta, err := s.ensureMemoryMetaLocked()
		if err != nil {
			return err
		}
		payload := map[string]any{}
		if len(newEvents) > 0 {
			payload["events.json"] = append(events, newEvents...)
		}
		if len(newStates) > 0 {
			payload["states.json"] = append(states, newStates...)
		}
		if _, err := s.commitMemoryLocked(CommitInitialize, meta.MemoryRevision, payload); err != nil {
			return err
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
	// 终态回写前核对任务代次（docs/issues.md #18）：旧任务不得覆盖新书/新任务的 init_state。
	if !s.initTokenValidLocked(token) {
		return nil
	}
	return s.writeMemoryInitStateLocked(state)
}

// failMemoryInit 将初始化任务标记为 failed；任务代次已失效时不写任何状态
// （docs/issues.md #18：旧任务不得覆盖新书/新任务的 init_state）。
func (s *Store) failMemoryInit(token initTaskToken, err error) {
	s.memoryMu.Lock()
	defer s.memoryMu.Unlock()
	if !s.initTokenValidLocked(token) {
		return
	}
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
// 五类记录/候选/提交日志/init_state 全部清空，meta 归零，并清除安全态标志——
// 防止旧书记录混入新书面板或显示"初始化完成"。
//
// 锁语义（P1-3 阻塞修复）：本函数自行获取 memoryMu（P1-3 前"调用方持 store.mu 时
// 获取 memoryMu"仅存在于注释、从未落实，重置可与记忆读写并发）——此处补上真正的
// memoryMu 互斥；调用方持 store.mu，形成全仓库唯一持序方向 store.mu→memoryMu。
func (s *Store) resetMemoryLocked() error {
	s.memoryMu.Lock()
	defer s.memoryMu.Unlock()

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
	// 运行号保留在 init_state 中（docs/issues.md #18）：跨重启续接基线，
	// 保证运行号单调、不与历史任务撞号；Status 重置为 not_started 表示无在途任务。
	if err := s.writeMemoryInitStateLocked(MemoryInitState{
		Status: InitNotStarted,
		RunID:  s.initRunCounter,
	}); err != nil {
		return err
	}
	// 递增记忆代次并失效当前运行号（docs/issues.md #18）：在途初始化任务凭此判定失效，
	// 旧书快照不得写入新书记忆，旧任务也不得覆盖新书的 init_state。
	s.memoryGeneration++
	s.initCurrentRun = 0
	// 记忆子系统已整体重建：旧数据触发的安全态随之解除。
	s.memorySafe = false
	s.memorySafeError = ""
	return nil
}

// ---- StoryOrder 重算（§4.1.4 / 冻结决策 16；钩子契约 R9–R11；自愈契约 R13）----

// handleChapterOrderChanged 为章节序号变化回调（在 store.mu 释放后执行，持 memoryMu）：
// 按 chapterId → 新 Index 批量重算事件 StoryOrder；来源章节被删除的事件置 needsReview
// （§13.2-1 初步建议：仅 needsReview，invalidated 留用户/reconcile）。变化留痕 reconcile 提交。
// 失败仅返回 error（由 notify 记日志），不得阻断章节操作（R9/R10）。
func (s *Store) handleChapterOrderChanged(oldIndexes, newIndexes map[string]int) error {
	s.memoryMu.Lock()
	defer s.memoryMu.Unlock()
	return s.reconcileStoryOrderLocked(newIndexes)
}

// reconcileStoryOrderFromSource 以 store 自身的章节真源（project.json）做一次全量对账
// （HEAL-01 自愈主路径，docs/issues.md #21）：调用方不得持有 store.mu。
//
// 存在意义：章节变更收尾钩子是**包级全局回调**（spec R9/R10），可能被其他 Store 实例
// 覆盖，也可能因文件占用等原因失败——两种情况都会让 events.json 残留旧 StoryOrder
// 与"来源章节已删除"的失效事件。本函数是**不依赖钩子**的兜底通道：以 store 自己的
// 章节真源为准，章节变更路径与启动期都会调用，保证记忆侧最终收敛。
//
// 失败仅返回 error，由调用方记日志——不阻断章节操作（R9/R10）。
func (s *Store) reconcileStoryOrderFromSource() error {
	s.mu.Lock()
	project, err := s.loadProjectLocked()
	s.mu.Unlock()
	if err != nil {
		return fmt.Errorf("store: load chapters for reconcile: %w", err)
	}
	return s.reconcileStoryOrder(chapterIndexMap(project.Chapters))
}

// reconcileStoryOrder 以给定章节索引真源做一次全量对账（调用方不得持有 store.mu，
// 本函数自行取 memoryMu）。
func (s *Store) reconcileStoryOrder(indexes map[string]int) error {
	s.memoryMu.Lock()
	defer s.memoryMu.Unlock()
	return s.reconcileStoryOrderLocked(indexes)
}

// reconcileStoryOrderLocked 全量对账并按需留痕（调用方须持 memoryMu）：
//   - 来源章节仍存在 → StoryOrder = 当前索引（纯重排不递增 recordVersion，R8 穷尽表未列）；
//   - 来源章节已删除 → needsReview=true + reviewReason="chapter_deleted" +
//     StoryOrder=0 + recordVersion++（§13.2-1）；
//   - 无变化 → 不提交（no-op 不递增 memoryRevision）。
func (s *Store) reconcileStoryOrderLocked(indexes map[string]int) error {
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
		index, ok := indexes[event.ChapterID]
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

// reconcileStoryOrderOnStartup 启动期章节-事件对账（HEAL-01 自愈，docs/issues.md #21）：
// 章节变更钩子失败时（如 events.json 被占用导致 rename 失败），重启后若不对账则
// 失效事件永久残留。本函数在启动期以当前章节真源全量对账一次，使"钩子失败 → 重启"闭环。
// 失败仅记日志，不阻断服务启动（与崩溃恢复的降级策略一致）。
func (s *Store) reconcileStoryOrderOnStartup() {
	if err := s.reconcileStoryOrderFromSource(); err != nil {
		log.Printf("store: startup story order reconcile failed (ignored): %v", err)
	}
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

package store

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"novelagent/internal/novel"
)

// 深度提取状态机（iter2-详设 §3.1）。
// 与初始化同构的两级凭据复用（spec §9.2 R12 推广，冻结）：
//   - memoryGeneration：导入新书递增，旧书快照作废——提取任务与初始化任务共用同一代次计数；
//   - extractCurrentRun：每次授权新任务递增；extract_state.runId 持久化以跨重启续接基线。
//
// 校验点在"章节快照读取后、ChangeSet 暂存前、终态回写前"三处：失效任务不写 changesets、
// 不写候选、不回写 extract_state。
// 重启恢复：New() 内检测孤儿 extracting → 置 failed + interrupted_by_restart。
// 导入重置 resetMemoryLocked 同步清空 extract_state.json（见 memory_init.go）。

const extractStateFile = "extract_state.json"

// 提取状态机（§3.1）。
const (
	ExtractIdle    = "idle"
	ExtractRunning = "extracting"
	ExtractDone    = "done"
	ExtractFailed  = "failed"
	ExtractPartial = "partial"
)

var (
	ErrExtractAlreadyRunning = errors.New("store: deep extraction already running")
	ErrExtractConfigMissing  = errors.New("store: model config is not set")
)

// ExtractStats 为提取统计（§3.1）。
type ExtractStats struct {
	ChaptersDone      int `json:"chaptersDone"`
	ChangeSetsCreated int `json:"changeSetsCreated"`
	CandidatesCreated int `json:"candidatesCreated"`
	OpsTotal          int `json:"opsTotal"`
	SkippedUpToDate   int `json:"skippedUpToDate"`
	FailedChapters    int `json:"failedChapters"`
}

// ExtractState 为 data/memory/extract_state.json 的磁盘结构（§3.1）。
type ExtractState struct {
	Status     string       `json:"status"` // idle | extracting | done | failed | partial
	RunID      uint64       `json:"runId"`
	StartedAt  time.Time    `json:"startedAt,omitempty"`
	FinishedAt time.Time    `json:"finishedAt,omitempty"`
	LastError  string       `json:"lastError,omitempty"`
	Scope      []string     `json:"scope"` // 本次章节 ID 列表（ch_）
	Stats      ExtractStats `json:"stats"`
}

// LoadExtractState 读取提取状态；文件不存在 → idle。
func (s *Store) LoadExtractState() (ExtractState, error) {
	s.memoryMu.Lock()
	defer s.memoryMu.Unlock()
	return s.loadExtractStateLocked()
}

func (s *Store) loadExtractStateLocked() (ExtractState, error) {
	var state ExtractState
	err := s.readMemoryFileLocked(extractStateFile, &state)
	if errors.Is(err, os.ErrNotExist) {
		return ExtractState{Status: ExtractIdle}, nil
	}
	if err != nil {
		return ExtractState{}, err
	}
	if state.Status == "" {
		state.Status = ExtractIdle
	}
	return state, nil
}

func (s *Store) writeExtractStateLocked(state ExtractState) error {
	return s.writeMemoryFileLocked(extractStateFile, state)
}

// resetOrphanExtractStateLocked 为启动恢复附带动作：进程重启后不可能存在在途提取，
// 孤儿 extracting 状态重置为 failed（与 init 孤儿处理同构），避免再次 POST /extract 永远 409。
func (s *Store) resetOrphanExtractStateLocked() {
	state, err := s.loadExtractStateLocked()
	if err != nil {
		return
	}
	if state.Status != ExtractRunning {
		return
	}
	state.Status = ExtractFailed
	state.FinishedAt = time.Now().UTC()
	state.LastError = "interrupted_by_restart"
	_ = s.writeExtractStateLocked(state)
}

// ExtractRunToken 标识一次提取任务的归属（docs/issues.md #18 同构）：
// 代次一致（期间未发生导入重置）且运行号仍为当前授权（未被新任务接管）。
type ExtractRunToken struct {
	Generation uint64
	RunID      uint64
	Scope      []string
	Force      bool
	BasedOnRev int64
}

func (s *Store) extractTokenValidLocked(token ExtractRunToken) bool {
	return s.memoryGeneration == token.Generation && s.extractCurrentRun == token.RunID
}

// BeginExtract 授权并启动一次深度提取（§3.2）：
//   - 前置不满足（未迁移/知识未就绪）→ ErrProjectNotMigrated / ErrKnowledgeNotReady；
//   - 模型配置缺失 → ErrExtractConfigMissing（启动即败）；
//   - extracting → ErrExtractAlreadyRunning；
//   - 否则置 extracting、写入 scope（按 (chapterId, chapterRevision) 去重）、分配运行号，返回 token。
//
// 锁序：前置检查先独立取 store.mu（含 chapters/config），释放后才进 memoryMu 写 extract_state
// 与读取 changesets 去重——全仓库持序唯一为 store.mu→memoryMu，无死锁。
func (s *Store) BeginExtract(chapterIDs []string, force bool) (string, ExtractRunToken, error) {
	// ---- 前置检查（store.mu）----
	pre, err := s.memoryPreconditionsLocked()
	if err != nil {
		return "", ExtractRunToken{}, err
	}
	if !pre.Migrated {
		return "", ExtractRunToken{}, ErrProjectNotMigrated
	}
	if !pre.KnowledgeReady {
		return "", ExtractRunToken{}, ErrKnowledgeNotReady
	}
	cfg, err := s.loadConfigLocked()
	if err != nil {
		return "", ExtractRunToken{}, err
	}
	if cfg.BaseURL == "" || cfg.Model == "" {
		return "", ExtractRunToken{}, ErrExtractConfigMissing
	}
	project, err := s.loadProjectLocked()
	if err != nil {
		return "", ExtractRunToken{}, err
	}
	// 与故事分析互斥：分析进行中拒绝启动提取（spec §10.4）。
	if project.Analysis.Status == novel.AnalysisRunning {
		return "", ExtractRunToken{}, ErrAnalysisRunning
	}

	// 规整 scope：空 chapterIDs = 全部正式章节；否则校验为 ch_ 前缀。
	wantAll := len(chapterIDs) == 0
	requested := make(map[string]struct{}, len(chapterIDs))
	for _, id := range chapterIDs {
		if !novel.ValidID(id) || len(id) < 3 || id[:3] != "ch_" {
			return "", ExtractRunToken{}, fmt.Errorf("store: invalid chapter id %q", id)
		}
		requested[id] = struct{}{}
	}
	chapters := make([]novel.ChapterMeta, 0, len(project.Chapters))
	for _, meta := range project.Chapters {
		if wantAll || chapterInRequested(meta.ID, requested) {
			chapters = append(chapters, meta)
		}
	}

	// ---- 进入 memoryMu：去重 + 授权 + 写状态 ----
	s.memoryMu.Lock()
	defer s.memoryMu.Unlock()

	if s.memorySafe {
		return "", ExtractRunToken{}, fmt.Errorf("%w: %s", ErrMemorySafeState, s.memorySafeError)
	}
	state, err := s.loadExtractStateLocked()
	if err != nil {
		return "", ExtractRunToken{}, err
	}
	if state.Status == ExtractRunning {
		return ExtractRunning, ExtractRunToken{}, ErrExtractAlreadyRunning
	}

	// 去重（§3.2）：已 applied 同键始终跳过（同内容重复提取无意义）；pending 同键仅 force=false 跳过。
	sets, err := s.loadChangeSetsLocked()
	if err != nil {
		return "", ExtractRunToken{}, err
	}
	scope := make([]string, 0, len(chapters))
	skipped := 0
	for _, meta := range chapters {
		proposed, applied := changeSetStatesForChapter(sets, meta.ID, meta.Revision)
		if applied || (!force && proposed) {
			skipped++
			continue
		}
		scope = append(scope, meta.ID)
	}

	now := time.Now().UTC()
	meta, err := s.ensureMemoryMetaLocked()
	if err != nil {
		return "", ExtractRunToken{}, err
	}

	s.extractRunCounter++
	runID := s.extractRunCounter
	s.extractCurrentRun = runID
	token := ExtractRunToken{
		Generation: s.memoryGeneration,
		RunID:      runID,
		Scope:      scope,
		Force:      force,
		BasedOnRev: meta.MemoryRevision,
	}

	newState := ExtractState{
		Status:    ExtractRunning,
		RunID:     runID,
		StartedAt: now,
		Scope:     scope,
		Stats:     ExtractStats{SkippedUpToDate: skipped},
	}
	if err := s.writeExtractStateLocked(newState); err != nil {
		return "", ExtractRunToken{}, err
	}

	// scope 为空（全部已是最新）→ 直接置 done，无需启动 goroutine。
	if len(scope) == 0 {
		newState.Status = ExtractDone
		newState.FinishedAt = now
		if err := s.writeExtractStateLocked(newState); err != nil {
			return "", ExtractRunToken{}, err
		}
		return ExtractDone, token, nil
	}
	return ExtractRunning, token, nil
}

func chapterInRequested(id string, requested map[string]struct{}) bool {
	_, ok := requested[id]
	return ok
}

// changeSetStatesForChapter 报告同键（chapterId+chapterRevision）是否已有 pending / applied
// ChangeSet（§3.2：applied 不可被 force 绕过，pending 可被 force 绕过）。
func changeSetStatesForChapter(sets []novel.ChangeSet, chapterID string, chapterRevision int64) (proposed, applied bool) {
	for _, cs := range sets {
		if cs.Status != novel.ChangeSetProposed && cs.Status != novel.ChangeSetApplied {
			continue
		}
		if !changeSetCoversChapter(cs, chapterID, chapterRevision) {
			continue
		}
		if cs.Status == novel.ChangeSetApplied {
			applied = true
		} else {
			proposed = true
		}
	}
	return proposed, applied
}

// changeSetCoversChapter 判断 ChangeSet 是否覆盖指定章节修订：优先读结构化
// ChapterID/ChapterRevision（D3）；仅旧数据缺失结构化字段时回退解析 Reason。
func changeSetCoversChapter(cs novel.ChangeSet, chapterID string, chapterRevision int64) bool {
	if cs.ChapterID != "" || cs.ChapterRevision != 0 {
		return cs.ChapterID == chapterID && cs.ChapterRevision == chapterRevision
	}
	const prefix = "chapter "
	const marker = " rev "
	if !strings.HasPrefix(cs.Reason, prefix) {
		return false
	}
	rest := cs.Reason[len(prefix):]
	idx := strings.Index(rest, marker)
	if idx < 0 {
		return false
	}
	id := rest[:idx]
	revStr := rest[idx+len(marker):]
	if idx2 := strings.IndexByte(revStr, ' '); idx2 >= 0 {
		revStr = revStr[:idx2]
	}
	var rev int64
	if _, err := fmt.Sscanf(revStr, "%d", &rev); err != nil {
		return false
	}
	return id == chapterID && rev == chapterRevision
}

// FinishExtract 回写提取终态（§3.1）：done / partial / failed。任务代次已失效时不写任何状态
// （旧任务不得覆盖新书/新任务的 extract_state）。
func (s *Store) FinishExtract(token ExtractRunToken, status string, stats ExtractStats, lastError string) error {
	s.memoryMu.Lock()
	defer s.memoryMu.Unlock()
	if !s.extractTokenValidLocked(token) {
		return nil
	}
	state, err := s.loadExtractStateLocked()
	if err != nil {
		return err
	}
	if state.Status != ExtractRunning {
		return nil
	}
	// 保留 StashExtractResult 已累积的按章统计（ChaptersDone/OpsTotal/...），仅合并失败章节与终态错误。
	state.Stats.FailedChapters += stats.FailedChapters
	state.Status = status
	state.FinishedAt = time.Now().UTC()
	state.LastError = truncate(lastError, 200)
	return s.writeExtractStateLocked(state)
}

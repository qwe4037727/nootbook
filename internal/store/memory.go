package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"novelagent/internal/novel"
)

// 记忆子系统（memory/ 目录）：meta.json 版本管理 + 提交日志协议 + 崩溃恢复。
// 契约来源：docs/memory_model_spec.md V1.2 冻结版 §8/§10/§11。
//
// 锁模型（spec §10 / §4.1.4）：
//   - store.mu 依旧守护 project/knowledge/chapters；
//   - memoryMu 独立守护 data/memory/ 下全部文件，与 store.mu 无嵌套——
//     需要两者数据的操作（如初始化回填）必须先取 store.mu 读、释放后再取 memoryMu 写。

const memoryDirName = "memory"

// memorySchemaVersion 为 memory/meta.json 的结构版本，当前恒为 1。
const memorySchemaVersion = 1

var (
	// ErrRevisionConflict 对应 R7：basedOnMemoryRevision != 当前 memoryRevision，
	// 拒绝提交且零落盘，写方须基于最新 revision 重建载荷后重试。
	ErrRevisionConflict = errors.New("store: memory revision conflict")
	// ErrMemorySafeState 对应 R4/R7：commits.json 损坏或提交链断链/分叉时进入安全态，
	// 拒绝一切记忆写操作，由人工裁定修复。
	ErrMemorySafeState = errors.New("store: memory subsystem in safe state")
)

// CommitType 枚举（spec §11.1；manual_fix/formal 迭代 2 启用）。
const (
	CommitInitialize  = "initialize"
	CommitManualFix   = "manual_fix"
	CommitFormal      = "formal"
	CommitReconcile   = "reconcile"
	CommitEntityCheck = "entity_confirm"
)

// CommitStatus 枚举。
const (
	CommitPrepared  = "prepared"
	CommitCommitted = "committed"
)

// MemoryInfo 为 GET /api/memory 的元信息载体（spec §12.1）。
type MemoryInfo struct {
	SchemaVersion  int    `json:"schemaVersion"`
	MemoryRevision int64  `json:"memoryRevision"`
	LastCommitID   string `json:"lastCommitId"`
	UpdatedAt      time.Time `json:"updatedAt"`
	SafeState      bool   `json:"safeState"`
	SafeStateError string `json:"safeStateError,omitempty"`
}

// memoryMetaFile 为 data/memory/meta.json 的磁盘结构（spec §8.2）。
type memoryMetaFile struct {
	SchemaVersion  int       `json:"schemaVersion"`
	MemoryRevision int64     `json:"memoryRevision"`
	LastCommitID   string    `json:"lastCommitId"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

// CommitRecord 为提交日志条目（spec §11.1）：payload = 目标文件名 → 完整目标内容（覆盖写语义）。
type CommitRecord struct {
	CommitID              string                     `json:"commitId"`
	Type                  string                     `json:"type"`
	Status                string                     `json:"status"`
	BasedOnMemoryRevision int64                      `json:"basedOnMemoryRevision"`
	TargetMemoryRevision  int64                      `json:"targetMemoryRevision"`
	Payload               map[string]json.RawMessage `json:"payload"`
	CreatedAt             time.Time                  `json:"createdAt"`
	CompletedAt           time.Time                  `json:"completedAt,omitempty"`
}

// LoadMemoryInfo 读取（必要时初始化）memory/meta.json（U5：首次访问创建 revision=0 的 meta）。
func (s *Store) LoadMemoryInfo() (MemoryInfo, error) {
	s.memoryMu.Lock()
	defer s.memoryMu.Unlock()
	meta, err := s.ensureMemoryMetaLocked()
	if err != nil {
		return MemoryInfo{}, err
	}
	info := MemoryInfo{
		SchemaVersion:  meta.SchemaVersion,
		MemoryRevision: meta.MemoryRevision,
		LastCommitID:   meta.LastCommitID,
		UpdatedAt:      meta.UpdatedAt,
		SafeState:      s.memorySafe,
	}
	if s.memorySafe {
		info.SafeStateError = s.memorySafeError
	}
	return info, nil
}

// commitMemoryLocked 执行一次记忆提交（spec §11.2 全流程，调用方须已持有 memoryMu）：
//  1. R7 基线校验：basedOn != 当前 memoryRevision → ErrRevisionConflict，零落盘；
//  2. 写 prepared CommitRecord → 依次覆盖写目标文件 → 置 committed → 对齐 meta。
func (s *Store) commitMemoryLocked(commitType string, basedOn int64, payload map[string]any) (CommitRecord, error) {
	if s.memorySafe {
		return CommitRecord{}, fmt.Errorf("%w: %s", ErrMemorySafeState, s.memorySafeError)
	}
	meta, err := s.ensureMemoryMetaLocked()
	if err != nil {
		return CommitRecord{}, err
	}
	if basedOn != meta.MemoryRevision {
		return CommitRecord{}, fmt.Errorf("%w: basedOn=%d current=%d", ErrRevisionConflict, basedOn, meta.MemoryRevision)
	}

	now := time.Now().UTC()
	record := CommitRecord{
		CommitID:              novel.NewID("commit"),
		Type:                  commitType,
		Status:                CommitPrepared,
		BasedOnMemoryRevision: basedOn,
		TargetMemoryRevision:  basedOn + 1,
		Payload:               make(map[string]json.RawMessage, len(payload)),
		CreatedAt:             now,
	}
	for name, value := range payload {
		raw, err := json.Marshal(value)
		if err != nil {
			return CommitRecord{}, fmt.Errorf("store: encode commit payload %s: %w", name, err)
		}
		record.Payload[name] = raw
	}

	commits, err := s.loadCommitsLocked()
	if err != nil {
		return CommitRecord{}, err
	}
	commits = append(commits, record)
	if err := s.writeCommitsLocked(commits); err != nil {
		return CommitRecord{}, err
	}

	if err := s.applyCommitPayloadLocked(record); err != nil {
		return CommitRecord{}, err
	}

	for i := range commits {
		if commits[i].CommitID == record.CommitID {
			commits[i].Status = CommitCommitted
			commits[i].CompletedAt = time.Now().UTC()
			record = commits[i]
		}
	}
	if err := s.writeCommitsLocked(commits); err != nil {
		return CommitRecord{}, err
	}

	meta.MemoryRevision = record.TargetMemoryRevision
	meta.LastCommitID = record.CommitID
	meta.UpdatedAt = time.Now().UTC()
	if err := s.writeMemoryMetaLocked(meta); err != nil {
		return CommitRecord{}, err
	}
	return record, nil
}

// applyCommitPayloadLocked 依覆盖写语义落盘 payload（幂等：同一内容重复写无害）。
func (s *Store) applyCommitPayloadLocked(record CommitRecord) error {
	// 稳定顺序写目标文件，避免不同崩溃点留下不同交错序。
	names := make([]string, 0, len(record.Payload))
	for name := range record.Payload {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		var value any
		if err := json.Unmarshal(record.Payload[name], &value); err != nil {
			return fmt.Errorf("store: decode commit payload %s: %w", name, err)
		}
		if err := s.writeMemoryFileLocked(name, value); err != nil {
			return err
		}
	}
	return nil
}

// recoverMemoryLocked 为启动期崩溃恢复（spec §11.3，R7 两阶段：先验链、后收敛）。
// 任何链损坏/日志损坏都进入安全态而非猜测修复。
func (s *Store) recoverMemoryLocked() {
	commits, err := s.loadCommitsLocked()
	if err != nil {
		s.enterMemorySafeStateLocked(err)
		return
	}
	if len(commits) == 0 {
		return
	}

	sort.SliceStable(commits, func(i, j int) bool { return commits[i].CreatedAt.Before(commits[j].CreatedAt) })

	// 阶段一：提交链校验——basedOn→target 首尾相接，无断链、无分叉。
	expected := int64(0)
	seenTargets := make(map[int64]string, len(commits))
	for _, record := range commits {
		if record.BasedOnMemoryRevision != expected {
			s.enterMemorySafeStateLocked(fmt.Errorf("commit chain broken at %s: basedOn=%d want=%d (commit_chain_broken)", record.CommitID, record.BasedOnMemoryRevision, expected))
			return
		}
		if other, dup := seenTargets[record.TargetMemoryRevision]; dup {
			s.enterMemorySafeStateLocked(fmt.Errorf("commit chain fork: %s and %s share target %d (commit_chain_broken)", other, record.CommitID, record.TargetMemoryRevision))
			return
		}
		seenTargets[record.TargetMemoryRevision] = record.CommitID
		expected = record.TargetMemoryRevision
	}

	// 阶段二：重放 prepared → meta 收敛（对齐到链尾 target，禁止仅取最大值）。
	dirty := false
	var tail *CommitRecord
	for i := range commits {
		record := &commits[i]
		if record.Status == CommitPrepared {
			if err := s.applyCommitPayloadLocked(*record); err != nil {
				s.enterMemorySafeStateLocked(fmt.Errorf("replay commit %s: %w", record.CommitID, err))
				return
			}
			record.Status = CommitCommitted
			record.CompletedAt = time.Now().UTC()
			dirty = true
		}
		if record.Status == CommitCommitted {
			tail = record
		}
	}
	if dirty {
		if err := s.writeCommitsLocked(commits); err != nil {
			s.enterMemorySafeStateLocked(err)
			return
		}
	}
	if tail == nil {
		return
	}
	meta, err := s.ensureMemoryMetaLocked()
	if err != nil {
		s.enterMemorySafeStateLocked(err)
		return
	}
	if meta.MemoryRevision < tail.TargetMemoryRevision || meta.LastCommitID != tail.CommitID {
		meta.MemoryRevision = max(meta.MemoryRevision, tail.TargetMemoryRevision)
		meta.LastCommitID = tail.CommitID
		meta.UpdatedAt = time.Now().UTC()
		if err := s.writeMemoryMetaLocked(meta); err != nil {
			s.enterMemorySafeStateLocked(err)
		}
	}
}

func (s *Store) enterMemorySafeStateLocked(err error) {
	s.memorySafe = true
	s.memorySafeError = err.Error()
}

// ---- 磁盘访问（全部 *Locked，仅持 memoryMu 调用）----

func (s *Store) memoryPath(name string) string {
	return filepath.Join(s.dir, memoryDirName, name)
}

func (s *Store) ensureMemoryMetaLocked() (memoryMetaFile, error) {
	var meta memoryMetaFile
	err := s.readMemoryFileLocked("meta.json", &meta)
	if errors.Is(err, os.ErrNotExist) {
		meta = memoryMetaFile{SchemaVersion: memorySchemaVersion, MemoryRevision: 0, UpdatedAt: time.Now().UTC()}
		if err := s.writeMemoryFileLocked("meta.json", meta); err != nil {
			return memoryMetaFile{}, err
		}
		return meta, nil
	}
	if err != nil {
		return memoryMetaFile{}, err
	}
	return meta, nil
}

func (s *Store) writeMemoryMetaLocked(meta memoryMetaFile) error {
	return s.writeMemoryFileLocked("meta.json", meta)
}

func (s *Store) loadCommitsLocked() ([]CommitRecord, error) {
	var commits []CommitRecord
	err := s.readMemoryFileLocked("commits.json", &commits)
	if errors.Is(err, os.ErrNotExist) {
		return []CommitRecord{}, nil
	}
	if err != nil {
		// R4：commits.json 损坏（解码失败）→ 安全态，不猜测恢复。
		return nil, fmt.Errorf("commits.json corrupted: %w", err)
	}
	if commits == nil {
		commits = []CommitRecord{}
	}
	return commits, nil
}

func (s *Store) writeCommitsLocked(commits []CommitRecord) error {
	return s.writeMemoryFileLocked("commits.json", commits)
}

func (s *Store) readMemoryFileLocked(name string, dst any) error {
	if err := os.MkdirAll(filepath.Join(s.dir, memoryDirName), 0o755); err != nil {
		if errors.Is(err, os.ErrExist) {
			err = nil
		} else {
			return fmt.Errorf("store: create memory directory: %w", err)
		}
	}
	return s.readJSONLocked(s.memoryPath(name), dst)
}

func (s *Store) writeMemoryFileLocked(name string, value any) error {
	if err := os.MkdirAll(filepath.Join(s.dir, memoryDirName), 0o755); err != nil {
		return fmt.Errorf("store: create memory directory: %w", err)
	}
	if err := writeJSONFile(s.memoryPath(name), value, 0o644); err != nil {
		return fmt.Errorf("store: save memory %s: %w", name, err)
	}
	return nil
}

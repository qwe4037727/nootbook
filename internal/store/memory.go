package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
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

// CommitStatus 枚举（aborted 为提交失败终态：链校验与恢复均跳过，防重试分叉）。
const (
	CommitPrepared  = "prepared"
	CommitCommitted = "committed"
	CommitAborted   = "aborted"
)

// MemoryInfo 为 GET /api/memory 的元信息载体（spec §12.1）。
type MemoryInfo struct {
	SchemaVersion  int       `json:"schemaVersion"`
	MemoryRevision int64     `json:"memoryRevision"`
	LastCommitID   string    `json:"lastCommitId"`
	UpdatedAt      time.Time `json:"updatedAt"`
	SafeState      bool      `json:"safeState"`
	SafeStateError string    `json:"safeStateError,omitempty"`
}

// memoryMetaFile 为 data/memory/meta.json 的磁盘结构（spec §8.2）。
type memoryMetaFile struct {
	SchemaVersion  int       `json:"schemaVersion"`
	MemoryRevision int64     `json:"memoryRevision"`
	LastCommitID   string    `json:"lastCommitId"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

// CommitRecord 为提交日志条目（spec §11.1）：payload = 目标文件名 → 完整目标内容（覆盖写语义）。
// RevertPayload（P1-1 阻塞修复）：payload 各目标文件的写前快照——值为文件原始字节，
// 字面量 "null" 表示该文件提交前不存在（回滚 = 删除）；prepared 落盘前采集，
// aborted 时按其回滚已写入的部分内容，杜绝"部分写入残留 + 重启仍报健康"。
type CommitRecord struct {
	CommitID              string                     `json:"commitId"`
	Type                  string                     `json:"type"`
	Status                string                     `json:"status"`
	BasedOnMemoryRevision int64                      `json:"basedOnMemoryRevision"`
	TargetMemoryRevision  int64                      `json:"targetMemoryRevision"`
	Payload               map[string]json.RawMessage `json:"payload"`
	RevertPayload         map[string]json.RawMessage `json:"revertPayload,omitempty"`
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
//  2. 运行期链尾一致性校验（P1-2 阻塞修复）：上一条非 aborted 记录若为 prepared
//     （同进程遗留的失败残迹）或与 meta 不对齐 → 安全态，拒绝追加，防重试分叉；
//  3. 采集写前快照 → 写 prepared → 依次覆盖写目标文件 → 置 committed → 对齐 meta。
//
// 失败清理（阻塞修复 #2 + P1-1）：
//   - committed 前任何步骤失败：置 aborted 并按 RevertPayload 回滚已写入的目标文件
//     （"null" → 删除文件），使数据与版本恢复到提交前；回滚失败 → 安全态；
//   - abort 落盘失败：保留 prepared（恢复流程幂等重放，安全方向）；
//   - 已 committed 但 meta 对齐失败：进入安全态（P1-2）——同进程重试被拒绝，
//     重启后恢复流程按崩溃点 5 收敛 meta，杜绝"同进程重试造成版本链分叉"。
func (s *Store) commitMemoryLocked(commitType string, basedOn int64, payload map[string]any) (record CommitRecord, err error) {
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

	commits, err := s.loadCommitsLocked()
	if err != nil {
		return CommitRecord{}, err
	}

	// P1-2：运行期链尾一致性校验——从尾部跳过 aborted，第一条非 aborted 记录
	// 必须是 committed 且与 meta 对齐；否则拒绝提交进入安全态。
	for i := len(commits) - 1; i >= 0; i-- {
		tail := commits[i]
		if tail.Status == CommitAborted {
			continue
		}
		switch {
		case tail.Status != CommitCommitted:
			s.enterMemorySafeStateLocked(fmt.Errorf("runtime chain tail %s is %s (commit_chain_broken)", tail.CommitID, tail.Status))
			return CommitRecord{}, fmt.Errorf("%w: runtime chain tail %s is %s", ErrMemorySafeState, tail.CommitID, tail.Status)
		case tail.TargetMemoryRevision != meta.MemoryRevision || tail.CommitID != meta.LastCommitID:
			s.enterMemorySafeStateLocked(fmt.Errorf("runtime chain tail %s (rev %d) does not match meta (rev %d, last %s) (commit_chain_broken)", tail.CommitID, tail.TargetMemoryRevision, meta.MemoryRevision, meta.LastCommitID))
			return CommitRecord{}, fmt.Errorf("%w: chain tail does not match meta", ErrMemorySafeState)
		}
		break
	}

	now := time.Now().UTC()
	record = CommitRecord{
		CommitID:              novel.NewID("commit"),
		Type:                  commitType,
		Status:                CommitPrepared,
		BasedOnMemoryRevision: basedOn,
		TargetMemoryRevision:  basedOn + 1,
		Payload:               make(map[string]json.RawMessage, len(payload)),
		CreatedAt:             now,
	}
	for name, value := range payload {
		raw, marshalErr := json.Marshal(value)
		if marshalErr != nil {
			return CommitRecord{}, fmt.Errorf("store: encode commit payload %s: %w", name, marshalErr)
		}
		record.Payload[name] = raw
	}

	// P1-1：payload 各目标文件的写前快照（prepared 落盘前采集，随记录持久化）。
	// 目标文件存在但解码失败 → 拒绝提交：覆盖写会销毁唯一副本且无法回滚。
	record.RevertPayload = make(map[string]json.RawMessage, len(record.Payload))
	for name := range record.Payload {
		var raw json.RawMessage
		readErr := s.readMemoryFileLocked(name, &raw)
		switch {
		case readErr == nil:
			record.RevertPayload[name] = raw
		case errors.Is(readErr, os.ErrNotExist):
			record.RevertPayload[name] = json.RawMessage("null")
		default:
			return CommitRecord{}, fmt.Errorf("store: snapshot revert payload %s: %w", name, readErr)
		}
	}

	commits = append(commits, record)
	if err = s.writeCommitsLocked(commits); err != nil {
		return CommitRecord{}, err
	}
	preparedIndex := len(commits) - 1
	committedMarked := false

	defer func() {
		if err == nil {
			return
		}
		// 注意：命名返回值 record 已被 "return CommitRecord{}, err" 清零，
		// 必须从 commits 切片取原始 prepared 记录（含 RevertPayload）。
		prepared := commits[preparedIndex]
		if committedMarked {
			// P1-2：committed 但 meta 未对齐 → 安全态（重启恢复会收敛 meta）。
			s.enterMemorySafeStateLocked(fmt.Errorf("commit %s committed but meta alignment failed: %v", prepared.CommitID, err))
			return
		}
		prepared.Status = CommitAborted
		prepared.CompletedAt = time.Now().UTC()
		commits[preparedIndex] = prepared
		if abortErr := s.writeCommitsLocked(commits); abortErr != nil {
			log.Printf("store: abort prepared commit %s failed (kept prepared, recovery will replay): %v", prepared.CommitID, abortErr)
			return
		}
		// P1-1：回滚已写入的部分内容，使数据与版本回到提交前。
		if restoreErr := s.restoreRevertPayloadLocked(prepared.RevertPayload); restoreErr != nil {
			s.enterMemorySafeStateLocked(fmt.Errorf("commit %s aborted but revert failed: %v", prepared.CommitID, restoreErr))
		}
	}()

	if err = s.applyCommitPayloadLocked(record); err != nil {
		return CommitRecord{}, err
	}

	for i := range commits {
		if commits[i].CommitID == record.CommitID {
			commits[i].Status = CommitCommitted
			commits[i].CompletedAt = time.Now().UTC()
			record = commits[i]
		}
	}
	if err = s.writeCommitsLocked(commits); err != nil {
		return CommitRecord{}, err
	}
	committedMarked = true

	meta.MemoryRevision = record.TargetMemoryRevision
	meta.LastCommitID = record.CommitID
	meta.UpdatedAt = time.Now().UTC()
	if err = s.writeMemoryMetaLocked(meta); err != nil {
		return CommitRecord{}, err
	}
	err = nil
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

// restoreRevertPayloadLocked 按 RevertPayload 回滚目标文件（P1-1）：
// "null" → 删除文件（提交前不存在）；否则重写为快照内容。稳定顺序执行，
// 与 applyCommitPayloadLocked 的写序一致，保证中断后重放幂等。失败交由调用方裁定。
func (s *Store) restoreRevertPayloadLocked(revert map[string]json.RawMessage) error {
	if len(revert) == 0 {
		return nil
	}
	names := make([]string, 0, len(revert))
	for name := range revert {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		raw := revert[name]
		if len(raw) == 0 || string(raw) == "null" {
			if err := os.Remove(s.memoryPath(name)); err != nil && !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("store: revert remove %s: %w", name, err)
			}
			continue
		}
		if err := s.writeMemoryFileLocked(name, raw); err != nil {
			return fmt.Errorf("store: revert rewrite %s: %w", name, err)
		}
	}
	return nil
}

// recoverMemoryLocked 为启动期崩溃恢复（spec §11.3，R7 两阶段：先验链、后收敛）。
// 任何记录非法/链损坏/日志损坏都进入安全态而非猜测修复（阻塞修复 #3）。
func (s *Store) recoverMemoryLocked() {
	commits, err := s.loadCommitsLocked()
	if err != nil {
		s.enterMemorySafeStateLocked(err)
		return
	}

	// 附带恢复（阻塞修复 #1）：进程重启后遗留的孤儿 mapping 状态不可能有在途映射，
	// 重置为 failed 以允许 §9.3 的"失败后续跑"。
	s.resetOrphanInitStateLocked()

	if len(commits) == 0 {
		return
	}

	sort.SliceStable(commits, func(i, j int) bool { return commits[i].CreatedAt.Before(commits[j].CreatedAt) })

	// 阶段 0：提交记录级合法性校验（阻塞修复 #3）。
	validTypes := map[string]bool{
		CommitInitialize: true, CommitManualFix: true, CommitFormal: true,
		CommitReconcile: true, CommitEntityCheck: true,
	}
	for _, record := range commits {
		switch {
		case record.CommitID == "" || !novel.ValidID(record.CommitID):
			s.enterMemorySafeStateLocked(fmt.Errorf("invalid commit record: bad commitId %q (commit_chain_broken)", record.CommitID))
			return
		case record.Status != CommitPrepared && record.Status != CommitCommitted && record.Status != CommitAborted:
			s.enterMemorySafeStateLocked(fmt.Errorf("invalid commit record %s: status %q (commit_chain_broken)", record.CommitID, record.Status))
			return
		case !validTypes[record.Type]:
			s.enterMemorySafeStateLocked(fmt.Errorf("invalid commit record %s: type %q (commit_chain_broken)", record.CommitID, record.Type))
			return
		case record.CreatedAt.IsZero():
			s.enterMemorySafeStateLocked(fmt.Errorf("invalid commit record %s: zero createdAt (commit_chain_broken)", record.CommitID))
			return
		case record.TargetMemoryRevision != record.BasedOnMemoryRevision+1 || record.TargetMemoryRevision <= 0:
			s.enterMemorySafeStateLocked(fmt.Errorf("invalid commit record %s: basedOn=%d target=%d (commit_chain_broken)", record.CommitID, record.BasedOnMemoryRevision, record.TargetMemoryRevision))
			return
		}
	}

	// 阶段一：提交链校验——跳过 aborted 后 basedOn→target 首尾相接，无断链、无分叉。
	expected := int64(0)
	seenTargets := make(map[int64]string, len(commits))
	for _, record := range commits {
		if record.Status == CommitAborted {
			continue // aborted 提交未生效，不参与链
		}
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

	// 阶段二：重放 prepared → aborted 回滚（P1-1）→ meta 收敛（对齐到链尾 target，禁止仅取最大值）。
	//
	// 回滚只对"日志最后一条"的 aborted 记录执行：若 aborted 之后还有后续记录，
	// 说明同进程 defer 链（abort→revert）已完整跑完（memoryMu 串行化保证后续提交
	// 只能在前一次 commitMemoryLocked 返回后开始），其回滚早已完成——此时再重放
	// 会用过期快照覆盖后续提交的新数据。
	dirty := false
	var tail *CommitRecord
	for i := range commits {
		record := &commits[i]
		switch record.Status {
		case CommitAborted:
			if i == len(commits)-1 && len(record.RevertPayload) > 0 {
				// P1-1：崩溃可能发生在"标记 aborted 落盘成功、回滚未执行"的间隙，
				// 恢复时补做回滚，使数据与版本回到提交前（已回滚则幂等无害）。
				if err := s.restoreRevertPayloadLocked(record.RevertPayload); err != nil {
					s.enterMemorySafeStateLocked(fmt.Errorf("revert aborted commit %s: %w", record.CommitID, err))
					return
				}
			}
			continue // 未生效，不重放、不计链尾
		case CommitPrepared:
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

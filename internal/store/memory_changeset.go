package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	"novelagent/internal/novel"
)

// 记忆变更集（MemoryChangeSet）草稿区（iter2-详设 §2 / §4）。
// changesets.json 为整体覆盖写（与冻结决策 11 一致）；草稿区数据进出均不产生 memoryRevision 变化。
// 正式提交经统一提交协议 commitMemoryLocked（memoryMu 域内，不改协议实现）。

const changeSetsFile = "changesets.json"

var (
	ErrChangeSetNotFound   = errors.New("store: changeset not found")
	ErrChangeSetTerminal   = errors.New("store: changeset is in terminal state")
	ErrInvalidChangeSet    = errors.New("store: changeset has invalid op")
	ErrOptimisticConflict  = errors.New("store: expected record version mismatch")
	ErrChangeSetsCorrupted = errors.New("store: changesets.json corrupted")
	ErrCandidateNotFound   = errors.New("store: entity candidate not found")
	ErrCandidateNotPending = errors.New("store: entity candidate is not pending")
)

// ApplyResult 为 accept 提交结果（§6 API）。
type ApplyResult struct {
	Status         string `json:"status"`
	CommitID       string `json:"commitId,omitempty"`
	MemoryRevision int64  `json:"memoryRevision"`
}

// chapterSnap 为证据复核所需的章节正文快照（锁外预读）。
type chapterSnap struct {
	Content  string
	Revision int64
}

// ---- changesets.json 读写（memoryMu 域内）----

func (s *Store) loadChangeSetsLocked() ([]novel.ChangeSet, error) {
	var sets []novel.ChangeSet
	err := s.readMemoryFileLocked(changeSetsFile, &sets)
	if errors.Is(err, os.ErrNotExist) {
		return []novel.ChangeSet{}, nil
	}
	if err != nil {
		// R4 同构：changesets.json 损坏 → 草稿区写操作拒绝报错，不影响五类正式文件读取（I2-H）。
		return nil, fmt.Errorf("%w: %v", ErrChangeSetsCorrupted, err)
	}
	if sets == nil {
		sets = []novel.ChangeSet{}
	}
	return sets, nil
}

func (s *Store) writeChangeSetsLocked(sets []novel.ChangeSet) error {
	return s.writeMemoryFileLocked(changeSetsFile, sets)
}

// LoadChangeSets 列表（摘要视图，不含 Record 全文，§6）；statusFilter 为空返回全部。
func (s *Store) LoadChangeSets(statusFilter string) ([]novel.ChangeSetSummary, error) {
	s.memoryMu.Lock()
	defer s.memoryMu.Unlock()
	sets, err := s.loadChangeSetsLocked()
	if err != nil {
		return nil, err
	}
	summaries := make([]novel.ChangeSetSummary, 0, len(sets))
	for _, cs := range sets {
		if statusFilter != "" && string(cs.Status) != statusFilter {
			continue
		}
		summaries = append(summaries, cs.Summary())
	}
	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].CreatedAt.After(summaries[j].CreatedAt)
	})
	return summaries, nil
}

// LoadPendingEventRecords 返回指定章节在 proposed ChangeSet 中的 add_event 记录（D7：提取去重
// 需覆盖尚未提交的批次）。只读；草稿区损坏时返回错误，由调用方降级为仅按正式记录去重。
func (s *Store) LoadPendingEventRecords(chapterID string) ([]novel.StoryEvent, error) {
	s.memoryMu.Lock()
	defer s.memoryMu.Unlock()
	sets, err := s.loadChangeSetsLocked()
	if err != nil {
		return nil, err
	}
	out := make([]novel.StoryEvent, 0)
	for _, cs := range sets {
		if cs.Status != novel.ChangeSetProposed {
			continue
		}
		for _, op := range cs.Ops {
			if op.Op != novel.OpAddEvent {
				continue
			}
			var event novel.StoryEvent
			if err := json.Unmarshal(op.Record, &event); err != nil {
				continue
			}
			if event.ChapterID != "" && event.ChapterID != chapterID {
				continue
			}
			out = append(out, event)
		}
	}
	return out, nil
}

// GetChangeSet 返回完整 ChangeSet（含 ops/conflicts）；404。
func (s *Store) GetChangeSet(id string) (novel.ChangeSet, error) {
	s.memoryMu.Lock()
	defer s.memoryMu.Unlock()
	return s.getChangeSetLocked(id)
}

func (s *Store) getChangeSetLocked(id string) (novel.ChangeSet, error) {
	sets, err := s.loadChangeSetsLocked()
	if err != nil {
		return novel.ChangeSet{}, err
	}
	for _, cs := range sets {
		if cs.ID == id {
			return cs, nil
		}
	}
	return novel.ChangeSet{}, fmt.Errorf("%w: %s", ErrChangeSetNotFound, id)
}

// SaveChangeSet 直接暂存一个 ChangeSet（含候选），用于提取管线与测试（memoryMu 域内，零写入保护）。
func (s *Store) SaveChangeSet(cs novel.ChangeSet, candidates []novel.EntityCandidate) error {
	s.memoryMu.Lock()
	defer s.memoryMu.Unlock()
	_, err := s.saveChangeSetLocked(cs, candidates)
	return err
}

// saveChangeSetLocked 内部暂存：结构校验 + 追加 changeset + 合并候选。
// 返回新增候选数（供提取统计）。调用方须持 memoryMu。
func (s *Store) saveChangeSetLocked(cs novel.ChangeSet, candidates []novel.EntityCandidate) (int, error) {
	if err := novel.ValidateChangeSet(&cs); err != nil {
		return 0, err
	}
	if cs.ID == "" {
		cs.ID = novel.NewID("mcs")
	}
	if cs.WorkID == "" {
		cs.WorkID = novel.InternalWorkID()
	}
	if cs.CreatedAt.IsZero() {
		cs.CreatedAt = time.Now().UTC()
	}
	sets, err := s.loadChangeSetsLocked()
	if err != nil {
		return 0, err
	}
	sets = append(sets, cs)
	if err := s.writeChangeSetsLocked(sets); err != nil {
		return 0, err
	}
	added := 0
	if len(candidates) > 0 {
		existing, err := s.loadEntityCandidatesLocked()
		if err != nil {
			return 0, err
		}
		merged, n := mergeCandidates(existing, candidates)
		added = n
		if n > 0 {
			if err := s.writeEntityCandidatesLocked(merged); err != nil {
				return 0, err
			}
		}
	}
	return added, nil
}

// StashExtractResult 由提取管线调用：校验提取任务归属（代次/运行号）后整章原子暂存
// ChangeSet + 候选，并更新 extract_state 统计（§3.3-8）。任务失效时零写入零回写（I2-E）。
func (s *Store) StashExtractResult(token ExtractRunToken, cs novel.ChangeSet, candidates []novel.EntityCandidate) error {
	s.memoryMu.Lock()
	defer s.memoryMu.Unlock()
	if !s.extractTokenValidLocked(token) {
		return fmt.Errorf("store: extract task invalidated (book replaced before stash)")
	}
	added, err := s.saveChangeSetLocked(cs, candidates)
	if err != nil {
		return err
	}
	state, err := s.loadExtractStateLocked()
	if err != nil {
		return err
	}
	state.Stats.ChaptersDone++
	state.Stats.OpsTotal += len(cs.Ops)
	if len(cs.Ops) > 0 {
		state.Stats.ChangeSetsCreated++
	}
	state.Stats.CandidatesCreated += added
	return s.writeExtractStateLocked(state)
}

// ---- accept / reject / rebase（§4）----

// AcceptChangeSet 审核提交（§4.2）。锁序：先 store.mu 检查分析是否运行，释放后再进 memoryMu；
// 证据复核所需的章节正文在 memoryMu 外快照读入，锁内只做内存比对（§4.3）。
func (s *Store) AcceptChangeSet(id string) (ApplyResult, error) {
	// 1. store.mu：分析任务运行 → 409 analysis_running（spec §10.4）。
	s.mu.Lock()
	project, err := s.loadProjectLocked()
	if err != nil {
		s.mu.Unlock()
		return ApplyResult{}, err
	}
	if project.Analysis.Status == novel.AnalysisRunning {
		s.mu.Unlock()
		return ApplyResult{}, ErrAnalysisRunning
	}
	s.mu.Unlock()

	// 2. memoryMu：读取 ChangeSet 并校验状态/基线（首次）。
	s.memoryMu.Lock()
	meta, err := s.ensureMemoryMetaLocked()
	if err != nil {
		s.memoryMu.Unlock()
		return ApplyResult{}, err
	}
	cs, err := s.getChangeSetLocked(id)
	if err != nil {
		s.memoryMu.Unlock()
		return ApplyResult{}, err
	}
	// 幂等重放：已 applied → 返回原结果，memoryRevision 不再 +1（I2-A）。
	if cs.Status == novel.ChangeSetApplied {
		s.memoryMu.Unlock()
		return ApplyResult{Status: string(novel.ChangeSetApplied), CommitID: cs.CommitID, MemoryRevision: meta.MemoryRevision}, nil
	}
	if cs.Status.IsTerminal() {
		s.memoryMu.Unlock()
		return ApplyResult{}, fmt.Errorf("%w: %s", ErrChangeSetTerminal, cs.Status)
	}
	if cs.DraftID != "" {
		s.memoryMu.Unlock()
		return ApplyResult{}, fmt.Errorf("%w: %s requires draft confirm", ErrDraftManagedChangeSet, cs.DraftID)
	}
	// 基线校验：BasedOnRevision 过期 → 409 revision_conflict（U31）。
	if cs.BasedOnRevision != meta.MemoryRevision {
		s.memoryMu.Unlock()
		return ApplyResult{}, fmt.Errorf("%w: basedOn=%d current=%d", ErrRevisionConflict, cs.BasedOnRevision, meta.MemoryRevision)
	}
	if s.memorySafe {
		s.memoryMu.Unlock()
		return ApplyResult{}, fmt.Errorf("%w: %s", ErrMemorySafeState, s.memorySafeError)
	}
	currentRev := meta.MemoryRevision
	s.memoryMu.Unlock()

	// 3. 锁外：快照读取本 ChangeSet 所引用章节的正文（证据复核用）。
	chapterIDs := collectChapterIDs(cs)
	chapterContents := s.snapshotChaptersForEvidence(chapterIDs)

	// 4. memoryMu：重读 ChangeSet（防窗口期被改）、重校验、组织 payload、提交。
	s.memoryMu.Lock()
	defer s.memoryMu.Unlock()

	cs, err = s.getChangeSetLocked(id)
	if err != nil {
		return ApplyResult{}, err
	}
	if cs.Status == novel.ChangeSetApplied {
		// 窗口期已被其他 accept 提交 → 幂等返回。
		reloaded, _ := s.ensureMemoryMetaLocked()
		return ApplyResult{Status: string(novel.ChangeSetApplied), CommitID: cs.CommitID, MemoryRevision: reloaded.MemoryRevision}, nil
	}
	if cs.Status.IsTerminal() {
		return ApplyResult{}, fmt.Errorf("%w: %s", ErrChangeSetTerminal, cs.Status)
	}
	if cs.BasedOnRevision != currentRev {
		return ApplyResult{}, fmt.Errorf("%w: basedOn=%d current=%d", ErrRevisionConflict, cs.BasedOnRevision, currentRev)
	}

	// 载入五类正式文件（内存副本），逐 op 处理。
	events, err := s.loadMemoryEventsLocked()
	if err != nil {
		return ApplyResult{}, err
	}
	states, err := s.loadMemoryStatesLocked()
	if err != nil {
		return ApplyResult{}, err
	}
	cognitions, err := s.loadMemoryCognitionsLocked()
	if err != nil {
		return ApplyResult{}, err
	}
	threads, err := s.loadMemoryThreadsLocked()
	if err != nil {
		return ApplyResult{}, err
	}
	plans, err := s.loadMemoryPlansLocked()
	if err != nil {
		return ApplyResult{}, err
	}

	// 暂存时点产出的冲突项（B3 未知主体、I2-B 暂存期 manual_edit、resolve 降级等）随 applied 保留供审计。
	conflicts := append([]novel.ConflictItem(nil), cs.Conflicts...)
	effective := false

	for i := range cs.Ops {
		op := cs.Ops[i]
		file := op.Op.File()
		switch file {
		case "events.json":
			changed, newConflicts, opErr := s.applyEventOp(&op, &events, chapterContents)
			if opErr != nil {
				return ApplyResult{}, opErr
			}
			conflicts = append(conflicts, newConflicts...)
			effective = effective || changed
		case "states.json":
			changed, newConflicts, opErr := s.applyStateOp(&op, &states, chapterContents)
			if opErr != nil {
				return ApplyResult{}, opErr
			}
			conflicts = append(conflicts, newConflicts...)
			effective = effective || changed
		case "cognitions.json":
			changed, opErr := s.applyCognitionOp(&op, &cognitions)
			if opErr != nil {
				return ApplyResult{}, opErr
			}
			effective = effective || changed
		case "threads.json":
			changed, newConflicts, opErr := s.applyThreadOp(&op, &threads, chapterContents)
			if opErr != nil {
				return ApplyResult{}, opErr
			}
			conflicts = append(conflicts, newConflicts...)
			effective = effective || changed
		case "plans.json":
			changed, opErr := s.applyPlanOp(&op, &plans)
			if opErr != nil {
				return ApplyResult{}, opErr
			}
			effective = effective || changed
		default:
			return ApplyResult{}, fmt.Errorf("%w: unknown op file %q", ErrInvalidChangeSet, file)
		}
	}

	// 零有效 ops（全部冲突或被去重）→ 直接 applied、不产生 CommitRecord、不递增（I2-F）。
	if !effective {
		return s.markChangeSetAppliedLocked(id, "", currentRev, conflicts)
	}

	payload := map[string]any{}
	if changedEvents := (len(events) > 0); changedEvents {
		payload["events.json"] = events
	}
	if len(states) > 0 {
		payload["states.json"] = states
	}
	if len(cognitions) > 0 {
		payload["cognitions.json"] = cognitions
	}
	if len(threads) > 0 {
		payload["threads.json"] = threads
	}
	if len(plans) > 0 {
		payload["plans.json"] = plans
	}
	// 仅纳入实际发生变化的文件，避免空覆盖（保持语义纯净）。
	payload = pruneUnchangedPayload(payload, map[string]any{
		"events.json":     events,
		"states.json":     states,
		"cognitions.json": cognitions,
		"threads.json":    threads,
		"plans.json":      plans,
	})

	record, err := s.commitMemoryLocked(CommitFormal, currentRev, payload)
	if err != nil {
		return ApplyResult{}, err
	}

	// 回填 ChangeSet 终态（提交解锁后单独原子写，失败仅记日志，§4.2-7）。
	cs, err = s.getChangeSetLocked(id)
	if err != nil {
		return ApplyResult{}, err
	}
	cs.Status = novel.ChangeSetApplied
	cs.CommitID = record.CommitID
	cs.AppliedAt = time.Now().UTC()
	cs.Conflicts = conflicts
	if err := s.replaceChangeSetLocked(cs); err != nil {
		log.Printf("store: accept applied commit %s but failed to persist changeset status (kept): %v", record.CommitID, err)
	}
	return ApplyResult{Status: string(novel.ChangeSetApplied), CommitID: record.CommitID, MemoryRevision: record.TargetMemoryRevision}, nil
}

// pruneUnchangedPayload 仅保留确实变化的文件切片（与加载时的零值区分）——此处我们直接以
// "是否包含于 payload" 控制；由于 applyXxxOp 已将新切片写回指针，且 payload 由上述分支填充，
// 这里确保不含未变化的文件。为简洁，调用方已按 effective 标记构造；本函数进一步剔除空切片。
func pruneUnchangedPayload(payload, _ map[string]any) map[string]any {
	out := make(map[string]any, len(payload))
	for name, value := range payload {
		switch v := value.(type) {
		case []novel.StoryEvent:
			if len(v) > 0 {
				out[name] = v
			}
		case []novel.StoryState:
			if len(v) > 0 {
				out[name] = v
			}
		case []novel.Cognition:
			if len(v) > 0 {
				out[name] = v
			}
		case []novel.StoryThread:
			if len(v) > 0 {
				out[name] = v
			}
		case []novel.StoryPlan:
			if len(v) > 0 {
				out[name] = v
			}
		default:
			out[name] = v
		}
	}
	return out
}

// markChangeSetAppliedLocked 零有效 ops 时直接置 applied（无提交、不递增），冲突项保留供审计。
func (s *Store) markChangeSetAppliedLocked(id, commitID string, rev int64, conflicts []novel.ConflictItem) (ApplyResult, error) {
	cs, err := s.getChangeSetLocked(id)
	if err != nil {
		return ApplyResult{}, err
	}
	cs.Status = novel.ChangeSetApplied
	cs.CommitID = commitID
	cs.AppliedAt = time.Now().UTC()
	cs.Conflicts = conflicts
	if err := s.replaceChangeSetLocked(cs); err != nil {
		return ApplyResult{}, err
	}
	return ApplyResult{Status: string(novel.ChangeSetApplied), CommitID: commitID, MemoryRevision: rev}, nil
}

func (s *Store) replaceChangeSetLocked(cs novel.ChangeSet) error {
	sets, err := s.loadChangeSetsLocked()
	if err != nil {
		return err
	}
	for i := range sets {
		if sets[i].ID == cs.ID {
			sets[i] = cs
			break
		}
	}
	return s.writeChangeSetsLocked(sets)
}

// applyEventOp 处理单个事件 op（add/update）。返回是否修改文件、冲突项、硬性错误。
func (s *Store) applyEventOp(op *novel.ChangeOp, events *[]novel.StoryEvent, chapters map[string]chapterSnap) (bool, []novel.ConflictItem, error) {
	if op.Op.IsUpdate() {
		return s.applyUpdateEventOp(op, events, chapters)
	}
	var record novel.StoryEvent
	if err := jsonUnmarshalOpRecord(op.Record, &record); err != nil {
		return false, nil, fmt.Errorf("%w: event decode: %v", ErrInvalidChangeSet, err)
	}
	if err := s.grantEventStatus(&record, chapters); err != nil {
		return false, nil, err
	}
	if err := novel.ValidateStoryEvent(&record); err != nil {
		return false, nil, fmt.Errorf("%w: %v", ErrInvalidChangeSet, err)
	}
	*events = append(*events, record)
	return true, nil, nil
}

func (s *Store) applyUpdateEventOp(op *novel.ChangeOp, events *[]novel.StoryEvent, chapters map[string]chapterSnap) (bool, []novel.ConflictItem, error) {
	var record novel.StoryEvent
	if err := jsonUnmarshalOpRecord(op.Record, &record); err != nil {
		return false, nil, fmt.Errorf("%w: event decode: %v", ErrInvalidChangeSet, err)
	}
	idx := -1
	for i := range *events {
		if (*events)[i].ID == op.TargetID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return false, nil, fmt.Errorf("%w: update target %s not found", ErrInvalidChangeSet, op.TargetID)
	}
	existing := (*events)[idx]
	if existing.RecordVersion != op.ExpectedRecordVersion {
		return false, nil, fmt.Errorf("%w: event %s expected %d got %d", ErrOptimisticConflict, op.TargetID, op.ExpectedRecordVersion, existing.RecordVersion)
	}
	// 人工修订保护（I2-B）：暂存后目标变 manual_edit → 移入冲突，不提交。
	if existing.LastSource == novel.LastSourceManualEdit {
		oldRaw, _ := marshalRaw(existing)
		return false, []novel.ConflictItem{{
			Kind:     novel.ConflictManualEdit,
			TargetID: op.TargetID,
			Old:      oldRaw,
			Proposed: op.Record,
			Detail:   "目标记录在暂存后被人工修订，提交前已移入冲突，不覆盖",
		}}, nil
	}
	record.ID = op.TargetID
	record.RecordVersion = existing.RecordVersion + 1
	record.UpdatedAt = time.Now().UTC()
	record.CreatedAt = existing.CreatedAt
	if err := s.grantEventStatus(&record, chapters); err != nil {
		return false, nil, err
	}
	if err := novel.ValidateStoryEvent(&record); err != nil {
		return false, nil, fmt.Errorf("%w: %v", ErrInvalidChangeSet, err)
	}
	(*events)[idx] = record
	return true, nil, nil
}

// grantEventStatus 由服务端按校验结果定终态（§4.2-5）：有定位证据 → confirmed，
// 否则 proposed + needsReview；证据复核（锁外快照）发现 revision/quote 不符 → 降级 evidence_stale/quote_mismatch。
func (s *Store) grantEventStatus(record *novel.StoryEvent, chapters map[string]chapterSnap) error {
	now := time.Now().UTC()
	record.MemoryMeta.Normalize(now)
	if len(record.SourceRefs) == 0 {
		record.Status = novel.EventProposed
		record.NeedsReview = true
		if record.ReviewReason == "" {
			record.ReviewReason = "no_evidence"
		}
		return nil
	}
	if reason, degraded := s.reviewEvidence(record.SourceRefs, chapters); degraded {
		record.Status = novel.EventProposed
		record.NeedsReview = true
		record.ReviewReason = reason
		return nil
	}
	record.Status = novel.EventConfirmed
	record.NeedsReview = false
	record.ReviewReason = ""
	return nil
}

// reviewEvidence 对照章节快照复核 SourceRef；revision 不符 → evidence_stale，quote 不符 → quote_mismatch。
func (s *Store) reviewEvidence(refs []novel.SourceRef, chapters map[string]chapterSnap) (string, bool) {
	for _, ref := range refs {
		snap, ok := chapters[ref.ChapterID]
		if !ok || snap.Revision != ref.ChapterRevision {
			return "evidence_stale", true
		}
		if ref.Quote != "" {
			if err := novel.CheckSourceRefAgainstContent(snap.Content, ref); err != nil {
				return "quote_mismatch", true
			}
		}
	}
	return "", false
}

func (s *Store) applyStateOp(op *novel.ChangeOp, states *[]novel.StoryState, chapters map[string]chapterSnap) (bool, []novel.ConflictItem, error) {
	if op.Op == novel.OpUpdateState {
		return s.applyUpdateStateOp(op, states, chapters)
	}
	var record novel.StoryState
	if err := jsonUnmarshalOpRecord(op.Record, &record); err != nil {
		return false, nil, fmt.Errorf("%w: state decode: %v", ErrInvalidChangeSet, err)
	}
	now := time.Now().UTC()
	record.MemoryMeta.Normalize(now)
	record.Status = novel.StateActive
	// spec §5.1：State 无证据须 needsReview。
	if len(record.SourceRefs) == 0 {
		record.NeedsReview = true
		if record.ReviewReason == "" {
			record.ReviewReason = "state_unverified"
		}
	} else if reason, degraded := s.reviewEvidence(record.SourceRefs, chapters); degraded {
		record.NeedsReview = true
		record.ReviewReason = reason
	}
	if err := novel.ValidateStoryState(&record); err != nil {
		return false, nil, fmt.Errorf("%w: %v", ErrInvalidChangeSet, err)
	}
	*states = append(*states, record)
	return true, nil, nil
}

// applyUpdateStateOp 实现 §2.2 规则 4：旧 active 置 superseded + 新记录 active，合并为一个 op。
func (s *Store) applyUpdateStateOp(op *novel.ChangeOp, states *[]novel.StoryState, chapters map[string]chapterSnap) (bool, []novel.ConflictItem, error) {
	var newState novel.StoryState
	if err := jsonUnmarshalOpRecord(op.Record, &newState); err != nil {
		return false, nil, fmt.Errorf("%w: state decode: %v", ErrInvalidChangeSet, err)
	}
	idx := -1
	for i := range *states {
		if (*states)[i].ID == op.TargetID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return false, nil, fmt.Errorf("%w: update target %s not found", ErrInvalidChangeSet, op.TargetID)
	}
	existing := (*states)[idx]
	if existing.RecordVersion != op.ExpectedRecordVersion {
		return false, nil, fmt.Errorf("%w: state %s expected %d got %d", ErrOptimisticConflict, op.TargetID, op.ExpectedRecordVersion, existing.RecordVersion)
	}
	if existing.LastSource == novel.LastSourceManualEdit {
		oldRaw, _ := marshalRaw(existing)
		return false, []novel.ConflictItem{{
			Kind:     novel.ConflictManualEdit,
			TargetID: op.TargetID,
			Old:      oldRaw,
			Proposed: op.Record,
			Detail:   "目标记录在暂存后被人工修订，提交前已移入冲突，不覆盖",
		}}, nil
	}
	if existing.Status != novel.StateActive {
		// 对已非 active（如已 superseded）的目标再 update → 失败（§4.2-4）。
		return false, nil, fmt.Errorf("%w: update target %s is not active", ErrInvalidChangeSet, op.TargetID)
	}
	// U29：三元组与值均未变 → 无实际变更，不产生新版本（recordVersion 不递增）。
	if existing.SubjectID == newState.SubjectID && existing.Category == newState.Category &&
		existing.Attribute == newState.Attribute && existing.Value == newState.Value {
		return false, nil, nil
	}
	now := time.Now().UTC()
	// 旧记录置 superseded。
	existing.Status = novel.StateSuperseded
	existing.RecordVersion = existing.RecordVersion + 1
	existing.LastSource = novel.LastSourceAgentExtraction
	existing.UpdatedAt = now
	(*states)[idx] = existing
	// 新记录 active。
	newState.ID = novel.NewID("state")
	newState.RecordVersion = 1
	newState.CreatedAt = now
	newState.UpdatedAt = now
	newState.Status = novel.StateActive
	newState.WorkID = novel.InternalWorkID()
	newState.Origin = novel.Origin{Type: novel.OriginDeepExtract}
	newState.LastSource = novel.LastSourceAgentExtraction
	if len(newState.SourceRefs) == 0 {
		newState.NeedsReview = true
		if newState.ReviewReason == "" {
			newState.ReviewReason = "state_unverified"
		}
	} else if reason, degraded := s.reviewEvidence(newState.SourceRefs, chapters); degraded {
		newState.NeedsReview = true
		newState.ReviewReason = reason
	}
	if err := novel.ValidateStoryState(&newState); err != nil {
		return false, nil, fmt.Errorf("%w: %v", ErrInvalidChangeSet, err)
	}
	*states = append(*states, newState)
	return true, nil, nil
}

func (s *Store) applyCognitionOp(op *novel.ChangeOp, cognitions *[]novel.Cognition) (bool, error) {
	var record novel.Cognition
	if err := jsonUnmarshalOpRecord(op.Record, &record); err != nil {
		return false, fmt.Errorf("%w: cognition decode: %v", ErrInvalidChangeSet, err)
	}
	if op.Op.IsUpdate() {
		idx := -1
		for i := range *cognitions {
			if (*cognitions)[i].ID == op.TargetID {
				idx = i
				break
			}
		}
		if idx < 0 {
			return false, fmt.Errorf("%w: update target %s not found", ErrInvalidChangeSet, op.TargetID)
		}
		existing := (*cognitions)[idx]
		if existing.RecordVersion != op.ExpectedRecordVersion {
			return false, fmt.Errorf("%w: cognition %s expected %d got %d", ErrOptimisticConflict, op.TargetID, op.ExpectedRecordVersion, existing.RecordVersion)
		}
		if existing.LastSource == novel.LastSourceManualEdit {
			return false, fmt.Errorf("%w: cognition %s is manual_edit protected", ErrInvalidChangeSet, op.TargetID)
		}
		record.ID = op.TargetID
		record.RecordVersion = existing.RecordVersion + 1
		record.CreatedAt = existing.CreatedAt
		record.UpdatedAt = time.Now().UTC()
	}
	now := time.Now().UTC()
	record.MemoryMeta.Normalize(now)
	if err := novel.ValidateCognition(&record); err != nil {
		return false, fmt.Errorf("%w: %v", ErrInvalidChangeSet, err)
	}
	if op.Op.IsUpdate() {
		for i := range *cognitions {
			if (*cognitions)[i].ID == record.ID {
				(*cognitions)[i] = record
				break
			}
		}
	} else {
		*cognitions = append(*cognitions, record)
	}
	return true, nil
}

func (s *Store) applyThreadOp(op *novel.ChangeOp, threads *[]novel.StoryThread, chapters map[string]chapterSnap) (bool, []novel.ConflictItem, error) {
	var record novel.StoryThread
	if err := jsonUnmarshalOpRecord(op.Record, &record); err != nil {
		return false, nil, fmt.Errorf("%w: thread decode: %v", ErrInvalidChangeSet, err)
	}
	if op.Op.IsUpdate() {
		idx := -1
		for i := range *threads {
			if (*threads)[i].ID == op.TargetID {
				idx = i
				break
			}
		}
		if idx < 0 {
			return false, nil, fmt.Errorf("%w: update target %s not found", ErrInvalidChangeSet, op.TargetID)
		}
		existing := (*threads)[idx]
		if existing.RecordVersion != op.ExpectedRecordVersion {
			return false, nil, fmt.Errorf("%w: thread %s expected %d got %d", ErrOptimisticConflict, op.TargetID, op.ExpectedRecordVersion, existing.RecordVersion)
		}
		if existing.LastSource == novel.LastSourceManualEdit {
			oldRaw, _ := marshalRaw(existing)
			return false, []novel.ConflictItem{{
				Kind: novel.ConflictManualEdit, TargetID: op.TargetID, Old: oldRaw, Proposed: op.Record,
				Detail: "目标记录在暂存后被人工修订，提交前已移入冲突，不覆盖",
			}}, nil
		}
		record.ID = op.TargetID
		record.RecordVersion = existing.RecordVersion + 1
		record.CreatedAt = existing.CreatedAt
		record.UpdatedAt = time.Now().UTC()
	}
	now := time.Now().UTC()
	record.MemoryMeta.Normalize(now)
	if err := novel.ValidateStoryThread(&record); err != nil {
		return false, nil, fmt.Errorf("%w: %v", ErrInvalidChangeSet, err)
	}
	if op.Op.IsUpdate() {
		for i := range *threads {
			if (*threads)[i].ID == record.ID {
				(*threads)[i] = record
				break
			}
		}
	} else {
		*threads = append(*threads, record)
	}
	return true, nil, nil
}

func (s *Store) applyPlanOp(op *novel.ChangeOp, plans *[]novel.StoryPlan) (bool, error) {
	var record novel.StoryPlan
	if err := jsonUnmarshalOpRecord(op.Record, &record); err != nil {
		return false, fmt.Errorf("%w: plan decode: %v", ErrInvalidChangeSet, err)
	}
	if op.Op.IsUpdate() {
		idx := -1
		for i := range *plans {
			if (*plans)[i].ID == op.TargetID {
				idx = i
				break
			}
		}
		if idx < 0 {
			return false, fmt.Errorf("%w: update target %s not found", ErrInvalidChangeSet, op.TargetID)
		}
		existing := (*plans)[idx]
		if existing.RecordVersion != op.ExpectedRecordVersion {
			return false, fmt.Errorf("%w: plan %s expected %d got %d", ErrOptimisticConflict, op.TargetID, op.ExpectedRecordVersion, existing.RecordVersion)
		}
		if existing.LastSource == novel.LastSourceManualEdit {
			return false, fmt.Errorf("%w: plan %s is manual_edit protected", ErrInvalidChangeSet, op.TargetID)
		}
		record.ID = op.TargetID
		record.RecordVersion = existing.RecordVersion + 1
		record.CreatedAt = existing.CreatedAt
		record.UpdatedAt = time.Now().UTC()
	}
	now := time.Now().UTC()
	record.MemoryMeta.Normalize(now)
	if err := novel.ValidateStoryPlanStruct(&record); err != nil {
		return false, fmt.Errorf("%w: %v", ErrInvalidChangeSet, err)
	}
	if op.Op.IsUpdate() {
		for i := range *plans {
			if (*plans)[i].ID == record.ID {
				(*plans)[i] = record
				break
			}
		}
	} else {
		*plans = append(*plans, record)
	}
	return true, nil
}

// RejectChangeSet 驳回（§4.1）：Status→rejected + rejectReason，零记忆写入，revision 不变。
func (s *Store) RejectChangeSet(id, reason string) error {
	s.memoryMu.Lock()
	defer s.memoryMu.Unlock()
	cs, err := s.getChangeSetLocked(id)
	if err != nil {
		return err
	}
	if cs.Status.IsTerminal() {
		return fmt.Errorf("%w: %s", ErrChangeSetTerminal, cs.Status)
	}
	cs.Status = novel.ChangeSetRejected
	cs.RejectedAt = time.Now().UTC()
	cs.RejectReason = truncate(strings.TrimSpace(reason), 200)
	return s.replaceChangeSetLocked(cs)
}

// RebaseChangeSet 基线过期恢复路径（§4.4）：对全部 ops 重跑校验、刷新 expectedRecordVersion、
// 重检 manual_edit、重算 evidence（章节 Revision 变化 → 更新降级标记），BasedOnRevision 更新为
// 当前 memoryRevision，写回 changesets.json。
func (s *Store) RebaseChangeSet(id string) (novel.ChangeSet, error) {
	// 先在 memoryMu 内读取 ChangeSet，再于锁外快照章节正文（证据重算用）。
	s.memoryMu.Lock()
	cs, err := s.getChangeSetLocked(id)
	s.memoryMu.Unlock()
	if err != nil {
		return novel.ChangeSet{}, err
	}
	chapterContents := s.snapshotChaptersForEvidence(collectChapterIDs(cs))

	s.memoryMu.Lock()
	defer s.memoryMu.Unlock()
	cs, err = s.getChangeSetLocked(id)
	if err != nil {
		return novel.ChangeSet{}, err
	}
	if cs.Status != novel.ChangeSetProposed {
		return novel.ChangeSet{}, fmt.Errorf("%w: %s", ErrChangeSetTerminal, cs.Status)
	}
	if cs.DraftID != "" {
		return novel.ChangeSet{}, fmt.Errorf("%w: %s requires draft confirm", ErrDraftManagedChangeSet, cs.DraftID)
	}
	meta, err := s.ensureMemoryMetaLocked()
	if err != nil {
		return novel.ChangeSet{}, err
	}
	sets, err := s.loadMemoryEventsLocked()
	if err != nil {
		return novel.ChangeSet{}, err
	}
	states, err := s.loadMemoryStatesLocked()
	if err != nil {
		return novel.ChangeSet{}, err
	}
	threads, err := s.loadMemoryThreadsLocked()
	if err != nil {
		return novel.ChangeSet{}, err
	}

	var conflicts []novel.ConflictItem
	for i := range cs.Ops {
		op := &cs.Ops[i]
		if !op.Op.IsUpdate() {
			continue
		}
		target, targetRaw, found := findTargetRecord(op, sets, states, threads)
		if !found {
			op.ExpectedRecordVersion = 0 // 目标已不存在：下轮 accept 将因 not found 失败，交由用户处理
			continue
		}
		op.ExpectedRecordVersion = target.RecordVersion
		// 重检 manual_edit：暂存后变 manual_edit → 移入冲突，从 ops 移除。
		if target.LastSource == novel.LastSourceManualEdit {
			conflicts = append(conflicts, novel.ConflictItem{
				Kind: novel.ConflictManualEdit, TargetID: op.TargetID,
				ExpectedRecordVersion: target.RecordVersion, Old: targetRaw, Proposed: op.Record,
				Detail: "rebase 检出目标已被人工修订，移入冲突",
			})
			cs.Ops = removeOp(cs.Ops, i)
			i--
		}
	}
	// 重算证据降级标记（仅 add_ 且有 SourceRefs 的事件/状态；update_ 不在此处改证据，交 accept）。
	for i := range cs.Ops {
		op := &cs.Ops[i]
		if op.Op != novel.OpAddEvent && op.Op != novel.OpAddState {
			continue
		}
		s.recomputeEvidenceNote(op, chapterContents)
	}
	cs.Conflicts = conflicts
	cs.BasedOnRevision = meta.MemoryRevision
	if err := s.replaceChangeSetLocked(cs); err != nil {
		return novel.ChangeSet{}, err
	}
	return cs, nil
}

func (s *Store) recomputeEvidenceNote(op *novel.ChangeOp, chapters map[string]chapterSnap) {
	var refs []novel.SourceRef
	switch op.Op {
	case novel.OpAddEvent:
		var record novel.StoryEvent
		if err := jsonUnmarshalOpRecord(op.Record, &record); err != nil {
			return
		}
		refs = record.SourceRefs
	case novel.OpAddState:
		var record novel.StoryState
		if err := jsonUnmarshalOpRecord(op.Record, &record); err != nil {
			return
		}
		refs = record.SourceRefs
	default:
		return
	}
	if len(refs) == 0 {
		return
	}
	if reason, degraded := s.reviewEvidence(refs, chapters); degraded {
		op.EvidenceNote = reason
	}
}

// findTargetRecord 在对应文件切片中按 TargetID 查找当前记录（用于 rebase 刷新 expectedRecordVersion）。
// 返回记录的 MemoryMeta、完整记录的 JSON 快照（冲突 Old 用），以及是否找到。
func findTargetRecord(op *novel.ChangeOp, events []novel.StoryEvent, states []novel.StoryState, threads []novel.StoryThread) (novel.MemoryMeta, json.RawMessage, bool) {
	switch op.Op {
	case novel.OpUpdateEvent:
		for i := range events {
			if events[i].ID == op.TargetID {
				raw, _ := json.Marshal(events[i])
				return events[i].MemoryMeta, raw, true
			}
		}
	case novel.OpUpdateState:
		for i := range states {
			if states[i].ID == op.TargetID {
				raw, _ := json.Marshal(states[i])
				return states[i].MemoryMeta, raw, true
			}
		}
	case novel.OpUpdateThread:
		for i := range threads {
			if threads[i].ID == op.TargetID {
				raw, _ := json.Marshal(threads[i])
				return threads[i].MemoryMeta, raw, true
			}
		}
	}
	return novel.MemoryMeta{}, nil, false
}

func removeOp(ops []novel.ChangeOp, i int) []novel.ChangeOp {
	return append(ops[:i], ops[i+1:]...)
}

// collectChapterIDs 收集 ChangeSet 中各 op 记录可能引用的章节 ID（证据复核/快照用）。
func collectChapterIDs(cs novel.ChangeSet) []string {
	seen := make(map[string]struct{})
	var ids []string
	add := func(id string) {
		if id == "" || !strings.HasPrefix(id, "ch_") {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	for _, op := range cs.Ops {
		switch op.Op {
		case novel.OpAddEvent, novel.OpUpdateEvent:
			var record novel.StoryEvent
			if err := jsonUnmarshalOpRecord(op.Record, &record); err == nil {
				add(record.ChapterID)
				for _, ref := range record.SourceRefs {
					add(ref.ChapterID)
				}
			}
		case novel.OpAddState, novel.OpUpdateState:
			var record novel.StoryState
			if err := jsonUnmarshalOpRecord(op.Record, &record); err == nil {
				for _, ref := range record.SourceRefs {
					add(ref.ChapterID)
				}
			}
		case novel.OpAddThread, novel.OpUpdateThread:
			var record novel.StoryThread
			if err := jsonUnmarshalOpRecord(op.Record, &record); err == nil {
				for _, ref := range record.SourceRefs {
					add(ref.ChapterID)
				}
			}
		}
	}
	return ids
}

// snapshotChaptersForEvidence 锁外预读章节正文快照（store.mu 内 LoadChapter），供锁内证据复核比对。
func (s *Store) snapshotChaptersForEvidence(ids []string) map[string]chapterSnap {
	out := make(map[string]chapterSnap, len(ids))
	for _, id := range ids {
		chapter, err := s.LoadChapter(id)
		if err != nil {
			continue
		}
		out[id] = chapterSnap{Content: chapter.Content, Revision: chapter.Revision}
	}
	return out
}

func jsonUnmarshalOpRecord(raw json.RawMessage, dst any) error {
	return json.Unmarshal(raw, dst)
}

// ErrAnalysisRunning 当故事分析（analysis.Manager）正在运行时，记忆审核提交被拒绝（spec §10.4）。
var ErrAnalysisRunning = errors.New("store: story analysis is running")

// marshalRaw 将任意记录序列化为 json.RawMessage（冲突项 Old/Proposed 快照用）。
func marshalRaw(v any) (json.RawMessage, error) {
	return json.Marshal(v)
}

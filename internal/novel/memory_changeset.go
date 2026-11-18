package novel

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// 记忆变更集（MemoryChangeSet，mcs_）：深度提取/人工修订的草稿态载体（iter2-详设 §2）。
// 草稿区数据（data/memory/changesets.json）进出均不产生 memoryRevision 变化，
// 仅在 confirm/accept 提交时才经统一提交协议落盘五类正式文件。
//
// 与 spec §2 冻结表一致：ChangeSet 内 Ops[].Record 为完整五类模型 JSON（含 MemoryMeta 骨架），
// 提交前必须过该类型 L-N + L-S（复用 ValidateStoryEvent 等现有校验器）。

// ChangeSetStatus 为 ChangeSet 生命周期状态（§2.1）。
type ChangeSetStatus string

const (
	ChangeSetProposed ChangeSetStatus = "proposed" // 已暂存，待审核
	ChangeSetApplied  ChangeSetStatus = "applied"  // 已正式提交
	ChangeSetRejected ChangeSetStatus = "rejected" // 已驳回（保留记录供审计）
)

func (s ChangeSetStatus) Valid() bool {
	switch s {
	case ChangeSetProposed, ChangeSetApplied, ChangeSetRejected:
		return true
	}
	return false
}

// IsTerminal 报告该状态是否终态（applied/rejected 不可逆，§4.1）。
func (s ChangeSetStatus) IsTerminal() bool {
	return s == ChangeSetApplied || s == ChangeSetRejected
}

// ChangeSet 为一次批量暂存（通常为单章深度提取的结果，§3.3-8：整章原子暂存）。
type ChangeSet struct {
	ID              string          `json:"id"`     // mcs_
	WorkID          string          `json:"workId"` // 恒 work_001
	Status          ChangeSetStatus `json:"status"`
	Proposer        string          `json:"proposer"`                  // 本期恒 "deep_extract"；预留 manual 等
	Reason          string          `json:"reason"`                    // 人类可读批次说明（如 "chapter ch_xx 深度提取"）
	ChapterID       string          `json:"chapterId,omitempty"`       // 结构化来源章节（提取批次）；人工批次可空
	ChapterRevision int64           `json:"chapterRevision,omitempty"` // 来源章节修订号；与 ChapterID 同时出现
	DraftID         string          `json:"draftId,omitempty"`         // 草稿提取来源（G6 联合提交）
	DraftRevision   int64           `json:"draftRevision,omitempty"`   // 提取时的草稿版本；编辑后须重提
	BasedOnRevision int64           `json:"basedOnMemoryRevision"`     // 暂存时的 memoryRevision 快照
	Ops             []ChangeOp      `json:"ops"`
	Conflicts       []ConflictItem  `json:"conflicts,omitempty"`
	CommitID        string          `json:"commitId,omitempty"` // apply 成功后回填
	CreatedAt       time.Time       `json:"createdAt"`
	AppliedAt       time.Time       `json:"appliedAt,omitempty"`
	RejectedAt      time.Time       `json:"rejectedAt,omitempty"`
	RejectReason    string          `json:"rejectReason,omitempty"`
}

// ChangeOpType 为单条变更操作类型（§2.2）。无 remove_*：正式记录不物理删除，
// "失效"用 update_* 把 Status 置 invalidated 表达（冻结决策）。
type ChangeOpType string

const (
	OpAddEvent        ChangeOpType = "add_event"
	OpUpdateEvent     ChangeOpType = "update_event"
	OpAddState        ChangeOpType = "add_state"
	OpUpdateState     ChangeOpType = "update_state" // 语义 = 旧 active 置 superseded + 新记录 active
	OpAddCognition    ChangeOpType = "add_cognition"
	OpUpdateCognition ChangeOpType = "update_cognition"
	OpAddThread       ChangeOpType = "add_thread"
	OpUpdateThread    ChangeOpType = "update_thread"
	OpAddPlan         ChangeOpType = "add_plan"
	OpUpdatePlan      ChangeOpType = "update_plan"
)

func (t ChangeOpType) Valid() bool {
	switch t {
	case OpAddEvent, OpUpdateEvent, OpAddState, OpUpdateState,
		OpAddCognition, OpUpdateCognition, OpAddThread, OpUpdateThread,
		OpAddPlan, OpUpdatePlan:
		return true
	}
	return false
}

// targetPrefix 返回 update_* 操作的 TargetID 必须匹配的前缀；add_* 操作无 TargetID（返回 ""）。
// 不符 → L-S 拒绝整个 ChangeSet（§2.2 规则 3）。
func (t ChangeOpType) targetPrefix() string {
	switch t {
	case OpUpdateEvent:
		return "evt"
	case OpUpdateState:
		return "state"
	case OpUpdateCognition:
		return "cog"
	case OpUpdateThread:
		return "thread"
	case OpUpdatePlan:
		return "plan"
	default:
		return ""
	}
}

// IsUpdate 报告该 op 是否为更新型（update_* 前缀）。供 store 包在审核提交阶段判定。
func (t ChangeOpType) IsUpdate() bool { return strings.HasPrefix(string(t), "update_") }

// File 返回该 op 影响的五类正式文件名（提交时组织的 payload 键）。
func (t ChangeOpType) File() string {
	switch t {
	case OpAddEvent, OpUpdateEvent:
		return "events.json"
	case OpAddState, OpUpdateState:
		return "states.json"
	case OpAddCognition, OpUpdateCognition:
		return "cognitions.json"
	case OpAddThread, OpUpdateThread:
		return "threads.json"
	case OpAddPlan, OpUpdatePlan:
		return "plans.json"
	}
	return ""
}

// ChangeOp 为单条变更操作（§2.2）。
type ChangeOp struct {
	Op                    ChangeOpType    `json:"op"`
	TargetID              string          `json:"targetId,omitempty"`              // update_* 必填，前缀须与 op 类型匹配
	ExpectedRecordVersion int64           `json:"expectedRecordVersion,omitempty"` // update_* 必填，乐观校验
	Record                json.RawMessage `json:"record"`                          // add_*：完整新记录；update_*：变更后完整记录
	EvidenceNote          string          `json:"evidenceNote,omitempty"`          // 提取侧说明（不入正式记录）
}

// ConflictItem 为冲突提案项（§2.3）。冲突项永不自动提交，accept 默认只提交非冲突 ops，
// 冲突项由用户人工处理（本期：前端展示，用户在记忆记录上手动修正或忽略）。
type ConflictItem struct {
	Kind                  string          `json:"kind"` // manual_edit_conflict | evidence_unlocated | resolved_without_proof | ambiguous_participant
	TargetID              string          `json:"targetId,omitempty"`
	ExpectedRecordVersion int64           `json:"expectedRecordVersion,omitempty"`
	Proposed              json.RawMessage `json:"proposed"`      // 提议的新值（完整记录）
	Old                   json.RawMessage `json:"old,omitempty"` // 冲突时的现值快照
	Detail                string          `json:"detail"`        // 人类可读说明
}

// ConflictKind 枚举（§2.3）。
const (
	ConflictManualEdit        = "manual_edit_conflict"
	ConflictEvidenceUnlocated = "evidence_unlocated"
	ConflictResolvedNoProof   = "resolved_without_proof"
	ConflictAmbiguous         = "ambiguous_participant"
)

// ---- ChangeSet 校验（L-S 层：返回 error 即拒绝整个持久化操作） ----

var ErrChangeSetValidation = errors.New("novel: changeset validation failed")

func changeSetValidationErrorf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrChangeSetValidation, fmt.Sprintf(format, args...))
}

// ValidateChangeSet 在暂存时做结构性校验（L-S 同构）：状态/op 类型合法、
// update_* 的 TargetID 前缀与 ExpectedRecordVersion 合规、Record 为非空合法 JSON。
// 注意：Record 内嵌的五类模型完整校验推迟到 accept（spec §4.7-3：零写入保护），
// 暂存阶段不拒绝"内容待审"的草稿。
func ValidateChangeSet(cs *ChangeSet) error {
	if !cs.Status.Valid() {
		return changeSetValidationErrorf("status %q is not allowed", cs.Status)
	}
	if cs.Proposer == "" {
		return changeSetValidationErrorf("proposer is required")
	}
	if cs.ChapterID != "" || cs.ChapterRevision != 0 {
		if !validPrefixedID(cs.ChapterID, "ch") {
			return changeSetValidationErrorf("chapterId %q must be ch_ id", cs.ChapterID)
		}
		if cs.ChapterRevision <= 0 {
			return changeSetValidationErrorf("chapterRevision=%d want >0", cs.ChapterRevision)
		}
	}
	if cs.DraftID != "" || cs.DraftRevision != 0 {
		if !validPrefixedID(cs.DraftID, "draft") {
			return changeSetValidationErrorf("draftId %q must be draft_ id", cs.DraftID)
		}
		if cs.DraftRevision < 1 {
			return changeSetValidationErrorf("draftRevision=%d want >=1", cs.DraftRevision)
		}
	}
	for i, op := range cs.Ops {
		if err := validateChangeOp(&op); err != nil {
			return changeSetValidationErrorf("ops[%d]: %v", i, err)
		}
	}
	return nil
}

func validateChangeOp(op *ChangeOp) error {
	if !op.Op.Valid() {
		return changeSetValidationErrorf("op type %q is not allowed", op.Op)
	}
	if op.Record == nil || len(strings.TrimSpace(string(op.Record))) == 0 || !json.Valid(op.Record) {
		return changeSetValidationErrorf("op %q record must be valid non-empty JSON", op.Op)
	}
	if op.Op.IsUpdate() {
		prefix := op.Op.targetPrefix()
		if !validPrefixedID(op.TargetID, prefix) {
			return changeSetValidationErrorf("op %q targetId %q must be %s_ id", op.Op, op.TargetID, prefix)
		}
		if op.ExpectedRecordVersion < 1 {
			return changeSetValidationErrorf("op %q expectedRecordVersion must be >=1", op.Op)
		}
	} else if op.TargetID != "" {
		return changeSetValidationErrorf("op %q must not carry targetId", op.Op)
	}
	return nil
}

// ChangeSetSummary 为列表接口的摘要视图（不含 Record 全文，§6）。
type ChangeSetSummary struct {
	ID              string          `json:"id"`
	WorkID          string          `json:"workId"`
	Status          ChangeSetStatus `json:"status"`
	Proposer        string          `json:"proposer"`
	Reason          string          `json:"reason"`
	BasedOnRevision int64           `json:"basedOnMemoryRevision"`
	OpCount         int             `json:"opCount"`
	ConflictCount   int             `json:"conflictCount"`
	CommitID        string          `json:"commitId,omitempty"`
	CreatedAt       time.Time       `json:"createdAt"`
	AppliedAt       time.Time       `json:"appliedAt,omitempty"`
}

// Summary 由完整 ChangeSet 派生摘要视图。
func (cs ChangeSet) Summary() ChangeSetSummary {
	return ChangeSetSummary{
		ID:              cs.ID,
		WorkID:          cs.WorkID,
		Status:          cs.Status,
		Proposer:        cs.Proposer,
		Reason:          cs.Reason,
		BasedOnRevision: cs.BasedOnRevision,
		OpCount:         len(cs.Ops),
		ConflictCount:   len(cs.Conflicts),
		CommitID:        cs.CommitID,
		CreatedAt:       cs.CreatedAt,
		AppliedAt:       cs.AppliedAt,
	}
}

package store

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"novelagent/internal/novel"
)

// 计划查询与状态操作（F20–F23，docs/memory_model_spec.md §4.5）。
// 锁模型：仅 memoryMu；状态变更与同作用域 supersede、子计划暂停在同一次记忆提交内完成。

var (
	ErrPlanNotFound   = errors.New("store: plan not found")
	ErrPlanInvalid    = errors.New("store: invalid plan")
	ErrPlanTransition = errors.New("store: plan status transition not allowed")
)

// PlanNode 为计划树节点。
type PlanNode struct {
	Plan     novel.StoryPlan `json:"plan"`
	Children []PlanNode      `json:"children"`
}

// PlanTree 为 GET /api/plans 响应。
type PlanTree struct {
	MemoryRevision int64      `json:"memoryRevision"`
	Count          int        `json:"count"`
	Roots          []PlanNode `json:"roots"`
}

// PlanApplyResult 为计划写入（生成/状态变更）结果；Plans 为本次受影响记录的新值。
type PlanApplyResult struct {
	MemoryRevision int64             `json:"memoryRevision"`
	CommitID       string            `json:"commitId,omitempty"`
	Plans          []novel.StoryPlan `json:"plans"`
}

// LoadPlanTree 读取 plans.json 并组装 (level, parentPlanID) 作用域树。
// 无记录 → 空 roots；父计划缺失的孤儿记录按根节点返回，不丢记录。
func (s *Store) LoadPlanTree() (PlanTree, error) {
	s.memoryMu.Lock()
	defer s.memoryMu.Unlock()

	meta, err := s.ensureMemoryMetaLocked()
	if err != nil {
		return PlanTree{}, err
	}
	plans, err := s.loadMemoryPlansLocked()
	if err != nil {
		return PlanTree{}, err
	}
	tree := PlanTree{MemoryRevision: meta.MemoryRevision, Roots: []PlanNode{}, Count: len(plans)}
	if len(plans) == 0 {
		return tree, nil
	}

	ordered := append([]novel.StoryPlan(nil), plans...)
	sortPlans(ordered)
	index := make(map[string]int, len(ordered))
	for i := range ordered {
		index[ordered[i].ID] = i
	}
	children := make(map[string][]int, len(ordered))
	roots := make([]int, 0, len(ordered))
	for i := range ordered {
		parent := ordered[i].ParentPlanID
		if parent == "" {
			roots = append(roots, i)
			continue
		}
		if _, ok := index[parent]; !ok {
			roots = append(roots, i)
			continue
		}
		children[parent] = append(children[parent], i)
	}

	visited := make(map[int]struct{}, len(ordered))
	var build func(i int) PlanNode
	build = func(i int) PlanNode {
		visited[i] = struct{}{}
		node := PlanNode{Plan: ordered[i], Children: []PlanNode{}}
		for _, child := range children[ordered[i].ID] {
			if _, seen := visited[child]; seen {
				continue
			}
			node.Children = append(node.Children, build(child))
		}
		return node
	}
	for _, i := range roots {
		if _, seen := visited[i]; seen {
			continue
		}
		tree.Roots = append(tree.Roots, build(i))
	}
	// 环（异常数据）中不可达的节点仍作为根返回，避免静默丢记录。
	for i := range ordered {
		if _, seen := visited[i]; seen {
			continue
		}
		tree.Roots = append(tree.Roots, build(i))
	}
	return tree, nil
}

// LoadActivePlans 返回可注入续写的 active 计划（跳过待复核）与当前 memoryRevision。
// 记忆文件不存在/损坏时返回错误，由调用方降级为空注入。
func (s *Store) LoadActivePlans() ([]novel.StoryPlan, int64, error) {
	s.memoryMu.Lock()
	defer s.memoryMu.Unlock()

	meta, err := s.ensureMemoryMetaLocked()
	if err != nil {
		return nil, 0, err
	}
	plans, err := s.loadMemoryPlansLocked()
	if err != nil {
		return nil, 0, err
	}
	active := make([]novel.StoryPlan, 0, len(plans))
	for _, plan := range plans {
		if plan.Status == novel.PlanActive && !plan.NeedsReview {
			active = append(active, plan)
		}
	}
	sortPlans(active)
	return active, meta.MemoryRevision, nil
}

// AddProposedPlans 将生成器产出的 proposed 计划整批追加并提交（memoryRevision+1）。
// 生成批次必须全部为 proposed；任何一条 L-S/作用域校验失败 → 拒绝整批、零写入。
func (s *Store) AddProposedPlans(in []novel.StoryPlan) (PlanApplyResult, error) {
	if len(in) == 0 {
		return PlanApplyResult{}, fmt.Errorf("%w: empty plan batch", ErrPlanInvalid)
	}
	s.memoryMu.Lock()
	defer s.memoryMu.Unlock()

	meta, err := s.ensureMemoryMetaLocked()
	if err != nil {
		return PlanApplyResult{}, err
	}
	if s.memorySafe {
		return PlanApplyResult{}, fmt.Errorf("%w: %s", ErrMemorySafeState, s.memorySafeError)
	}
	plans, err := s.loadMemoryPlansLocked()
	if err != nil {
		return PlanApplyResult{}, err
	}
	existing := make(map[string]struct{}, len(plans))
	for _, plan := range plans {
		existing[plan.ID] = struct{}{}
	}

	now := time.Now().UTC()
	created := make([]novel.StoryPlan, 0, len(in))
	for i := range in {
		plan := in[i]
		if plan.ID == "" {
			plan.ID = novel.NewID("plan")
		}
		if _, dup := existing[plan.ID]; dup {
			return PlanApplyResult{}, fmt.Errorf("%w: duplicate plan id %s", ErrPlanInvalid, plan.ID)
		}
		existing[plan.ID] = struct{}{}
		if plan.Status == "" {
			plan.Status = novel.PlanProposed
		}
		if plan.Status != novel.PlanProposed {
			return PlanApplyResult{}, fmt.Errorf("%w: %s status must be proposed", ErrPlanInvalid, plan.ID)
		}
		plan.MemoryMeta.WorkID = novel.InternalWorkID()
		plan.MemoryMeta.RecordVersion = 1
		plan.MemoryMeta.CreatedAt = now
		plan.MemoryMeta.UpdatedAt = now
		plan.MemoryMeta.NeedsReview = false
		plan.MemoryMeta.ReviewReason = ""
		plan.MemoryMeta.Origin = novel.Origin{Type: novel.OriginDeepExtract}
		plan.MemoryMeta.LastSource = novel.LastSourceAgentExtraction
		plan.CreatedFromCommit = meta.LastCommitID
		plan.Normalize(now)
		if err := novel.ValidateStoryPlanStruct(&plan); err != nil {
			return PlanApplyResult{}, fmt.Errorf("%w: %v", ErrPlanInvalid, err)
		}
		created = append(created, plan)
	}

	all := append(append([]novel.StoryPlan{}, plans...), created...)
	if err := novel.ValidateStoryPlanScopes(all); err != nil {
		return PlanApplyResult{}, fmt.Errorf("%w: %v", ErrPlanInvalid, err)
	}
	record, err := s.commitMemoryLocked(CommitFormal, meta.MemoryRevision, map[string]any{"plans.json": all})
	if err != nil {
		return PlanApplyResult{}, err
	}
	return PlanApplyResult{MemoryRevision: record.TargetMemoryRevision, CommitID: record.CommitID, Plans: created}, nil
}

// SetPlanStatus 执行用户发起的计划状态变更（激活/暂停/完成/取消）。
// 激活 → 同作用域旧 active 自动 superseded；取消或旧 active 被 superseded 时，
// 其 active 子孙计划在同一提交内递归置 paused（U22/U23）。completed 不自动变更子计划。
func (s *Store) SetPlanStatus(id string, status novel.StoryPlanStatus) (PlanApplyResult, error) {
	if !status.Valid() || status == novel.PlanSuperseded || status == novel.PlanProposed {
		return PlanApplyResult{}, fmt.Errorf("%w: status %q is not user-settable", ErrPlanTransition, status)
	}
	s.memoryMu.Lock()
	defer s.memoryMu.Unlock()

	meta, err := s.ensureMemoryMetaLocked()
	if err != nil {
		return PlanApplyResult{}, err
	}
	plans, err := s.loadMemoryPlansLocked()
	if err != nil {
		return PlanApplyResult{}, err
	}
	target := -1
	for i := range plans {
		if plans[i].ID == id {
			target = i
			break
		}
	}
	if target < 0 {
		return PlanApplyResult{}, fmt.Errorf("%w: %s", ErrPlanNotFound, id)
	}
	if plans[target].Status == status {
		if status == novel.PlanActive && plans[target].NeedsReview {
			return s.commitPlanMutationsLocked(meta.MemoryRevision, plans, []int{target}, func(plan *novel.StoryPlan, now time.Time) {
				clearPlanReview(plan, now)
			})
		}
		return PlanApplyResult{MemoryRevision: meta.MemoryRevision, Plans: []novel.StoryPlan{plans[target]}}, nil
	}
	if !canTransitionPlan(plans[target].Status, status) {
		return PlanApplyResult{}, fmt.Errorf("%w: %s → %s", ErrPlanTransition, plans[target].Status, status)
	}
	if s.memorySafe {
		return PlanApplyResult{}, fmt.Errorf("%w: %s", ErrMemorySafeState, s.memorySafeError)
	}

	now := time.Now().UTC()
	affected := map[int]struct{}{target: {}}

	if status == novel.PlanActive {
		// 同作用域 = (level, parentPlanID)；旧 active 置 superseded 并暂停其 active 子孙。
		for i := range plans {
			if i == target || plans[i].Status != novel.PlanActive {
				continue
			}
			if plans[i].Level != plans[target].Level || plans[i].ParentPlanID != plans[target].ParentPlanID {
				continue
			}
			setPlanStatusRecord(&plans[i], novel.PlanSuperseded, now)
			affected[i] = struct{}{}
			pauseActiveDescendants(plans, plans[i].ID, now, affected)
		}
	}
	setPlanStatusRecord(&plans[target], status, now)
	clearPlanReview(&plans[target], now)
	if status == novel.PlanCancelled {
		pauseActiveDescendants(plans, plans[target].ID, now, affected)
	}

	if err := novel.ValidateStoryPlanScopes(plans); err != nil {
		return PlanApplyResult{}, fmt.Errorf("%w: %v", ErrPlanInvalid, err)
	}
	return s.commitPlanMutationsLocked(meta.MemoryRevision, plans, affectedIndexes(affected), nil)
}

func (s *Store) commitPlanMutationsLocked(basedOn int64, plans []novel.StoryPlan, affected []int, mutate func(*novel.StoryPlan, time.Time)) (PlanApplyResult, error) {
	if mutate != nil {
		now := time.Now().UTC()
		for _, idx := range affected {
			mutate(&plans[idx], now)
		}
	}
	record, err := s.commitMemoryLocked(CommitManualFix, basedOn, map[string]any{"plans.json": plans})
	if err != nil {
		return PlanApplyResult{}, err
	}
	result := PlanApplyResult{MemoryRevision: record.TargetMemoryRevision, CommitID: record.CommitID, Plans: make([]novel.StoryPlan, 0, len(affected))}
	for _, idx := range affected {
		result.Plans = append(result.Plans, plans[idx])
	}
	return result, nil
}

func setPlanStatusRecord(plan *novel.StoryPlan, status novel.StoryPlanStatus, now time.Time) {
	plan.Status = status
	plan.RecordVersion++
	plan.UpdatedAt = now
	plan.LastSource = novel.LastSourceManualEdit
}

func clearPlanReview(plan *novel.StoryPlan, now time.Time) {
	if !plan.NeedsReview {
		return
	}
	plan.NeedsReview = false
	plan.ReviewReason = ""
	plan.RecordVersion++
	plan.UpdatedAt = now
}

// pauseActiveDescendants 沿 parentPlanID 递归把 active 子孙置 paused（不改内容/不取消）。
func pauseActiveDescendants(plans []novel.StoryPlan, parentID string, now time.Time, affected map[int]struct{}) {
	queue := []string{parentID}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for i := range plans {
			if plans[i].ParentPlanID != current || plans[i].Status != novel.PlanActive {
				continue
			}
			setPlanStatusRecord(&plans[i], novel.PlanPaused, now)
			affected[i] = struct{}{}
			queue = append(queue, plans[i].ID)
		}
	}
}

func canTransitionPlan(from, to novel.StoryPlanStatus) bool {
	switch to {
	case novel.PlanActive:
		return from == novel.PlanProposed || from == novel.PlanPaused
	case novel.PlanPaused:
		return from == novel.PlanActive
	case novel.PlanCompleted:
		return from == novel.PlanActive || from == novel.PlanPaused
	case novel.PlanCancelled:
		return from == novel.PlanProposed || from == novel.PlanActive || from == novel.PlanPaused
	}
	return false
}

func affectedIndexes(set map[int]struct{}) []int {
	out := make([]int, 0, len(set))
	for idx := range set {
		out = append(out, idx)
	}
	sort.Ints(out)
	return out
}

func sortPlans(plans []novel.StoryPlan) {
	sort.SliceStable(plans, func(i, j int) bool {
		li, lj := planLevelRank(plans[i].Level), planLevelRank(plans[j].Level)
		if li != lj {
			return li < lj
		}
		si, sj := planStatusRank(plans[i].Status), planStatusRank(plans[j].Status)
		if si != sj {
			return si < sj
		}
		if !plans[i].CreatedAt.Equal(plans[j].CreatedAt) {
			return plans[i].CreatedAt.Before(plans[j].CreatedAt)
		}
		return plans[i].ID < plans[j].ID
	})
}

func planLevelRank(level string) int {
	switch novel.StoryPlanLevel(level) {
	case novel.PlanLevelGlobal:
		return 0
	case novel.PlanLevelVolume:
		return 1
	case novel.PlanLevelNearTerm:
		return 2
	}
	return 3
}

func planStatusRank(status novel.StoryPlanStatus) int {
	switch status {
	case novel.PlanActive:
		return 0
	case novel.PlanProposed:
		return 1
	case novel.PlanPaused:
		return 2
	case novel.PlanCompleted:
		return 3
	case novel.PlanSuperseded:
		return 4
	case novel.PlanCancelled:
		return 5
	}
	return 6
}

// ValidPlanID 校验 plan_ 前缀（server 参数校验用）。
func ValidPlanID(id string) bool {
	return novel.ValidID(id) && strings.HasPrefix(id, "plan_")
}

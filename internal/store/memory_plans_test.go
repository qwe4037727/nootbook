package store

import (
	"errors"
	"testing"

	"novelagent/internal/novel"
)

// 计划链路（G5/S4）：生成批次落盘、作用域激活、取消传播与树查询。

func testPlan(id, level, parent, title string) novel.StoryPlan {
	return novel.StoryPlan{
		ID:           id,
		Status:       novel.PlanProposed,
		Level:        level,
		ParentPlanID: parent,
		Title:        title,
		Objective:    title + "的目标",
		Constraints:  []string{"不提前收束"},
		CandidateBeats: []string{
			title + "的候选发展",
		},
	}
}

func loadTestPlans(t *testing.T, st *Store) []novel.StoryPlan {
	t.Helper()
	st.memoryMu.Lock()
	defer st.memoryMu.Unlock()
	plans, err := st.loadMemoryPlansLocked()
	if err != nil {
		t.Fatalf("loadMemoryPlansLocked: %v", err)
	}
	return plans
}

func findTestPlan(t *testing.T, plans []novel.StoryPlan, id string) novel.StoryPlan {
	t.Helper()
	for _, plan := range plans {
		if plan.ID == id {
			return plan
		}
	}
	t.Fatalf("plan %s not found in %+v", id, plans)
	return novel.StoryPlan{}
}

func TestAddProposedPlansCommitsBatch(t *testing.T) {
	st, _ := newTestStore(t)
	global := testPlan("plan_global", "global", "", "总纲")
	vol1 := testPlan("plan_v1", "volume", "plan_global", "第一卷")
	vol2 := testPlan("plan_v2", "volume", "plan_global", "第二卷")

	result, err := st.AddProposedPlans([]novel.StoryPlan{global, vol1, vol2})
	if err != nil {
		t.Fatalf("AddProposedPlans: %v", err)
	}
	if result.MemoryRevision != 1 || result.CommitID == "" || len(result.Plans) != 3 {
		t.Fatalf("result = %+v", result)
	}
	if result.Plans[0].CreatedFromCommit != "" {
		t.Errorf("createdFromCommit = %q, want empty before first commit", result.Plans[0].CreatedFromCommit)
	}
	stored := loadTestPlans(t, st)
	if len(stored) != 3 {
		t.Fatalf("stored plans = %d, want 3", len(stored))
	}
	for _, plan := range stored {
		if plan.Status != novel.PlanProposed || plan.RecordVersion != 1 || plan.WorkID != novel.InternalWorkID() {
			t.Errorf("stored plan = %+v", plan)
		}
	}

	tree, err := st.LoadPlanTree()
	if err != nil {
		t.Fatalf("LoadPlanTree: %v", err)
	}
	if tree.Count != 3 || tree.MemoryRevision != 1 || len(tree.Roots) != 1 {
		t.Fatalf("tree = %+v", tree)
	}
	if tree.Roots[0].Plan.ID != "plan_global" || len(tree.Roots[0].Children) != 2 {
		t.Errorf("tree root = %+v", tree.Roots[0])
	}
	if tree.Roots[0].Children[0].Children == nil {
		t.Error("children must serialize as empty array")
	}

	// 第二批生成基于修订 1，CreatedFromCommit 指向上一次提交。
	second, err := st.AddProposedPlans([]novel.StoryPlan{testPlan("plan_global2", "global", "", "新总纲")})
	if err != nil {
		t.Fatalf("second AddProposedPlans: %v", err)
	}
	if second.MemoryRevision != 2 || second.Plans[0].CreatedFromCommit == "" {
		t.Errorf("second result = %+v", second)
	}
}

func TestAddProposedPlansRejectsInvalidBatch(t *testing.T) {
	st, _ := newTestStore(t)
	cases := map[string][]novel.StoryPlan{
		"empty": {},
		"not proposed": {func() novel.StoryPlan {
			p := testPlan("plan_a", "global", "", "A")
			p.Status = novel.PlanActive
			return p
		}()},
		"missing parent": {testPlan("plan_v", "volume", "plan_ghost", "V")},
		"bad level":      {testPlan("plan_b", "chapter", "", "B")},
	}
	for name, batch := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := st.AddProposedPlans(batch); !errors.Is(err, ErrPlanInvalid) {
				t.Fatalf("error = %v, want ErrPlanInvalid", err)
			}
			if got := memoryRevisionOf(t, st); got != 0 {
				t.Errorf("revision = %d, want 0 (zero write)", got)
			}
		})
	}
}

func TestActivatePlanSupersedesSameScopeAndPausesChildren(t *testing.T) {
	st, _ := newTestStore(t)
	global := testPlan("plan_global", "global", "", "总纲")
	v1 := testPlan("plan_v1", "volume", "plan_global", "第一卷")
	n1 := testPlan("plan_n1", "near_term", "plan_v1", "第一卷近期")
	v2 := testPlan("plan_v2", "volume", "plan_global", "第二卷")
	if _, err := st.AddProposedPlans([]novel.StoryPlan{global, v1, n1, v2}); err != nil {
		t.Fatalf("AddProposedPlans: %v", err)
	}

	if _, err := st.SetPlanStatus("plan_v1", novel.PlanActive); err != nil {
		t.Fatalf("activate v1: %v", err)
	}
	if _, err := st.SetPlanStatus("plan_n1", novel.PlanActive); err != nil {
		t.Fatalf("activate n1: %v", err)
	}

	result, err := st.SetPlanStatus("plan_v2", novel.PlanActive)
	if err != nil {
		t.Fatalf("activate v2: %v", err)
	}
	if len(result.Plans) != 3 {
		t.Fatalf("affected = %+v, want v1/n1/v2", result.Plans)
	}
	plans := loadTestPlans(t, st)
	v1After := findTestPlan(t, plans, "plan_v1")
	if v1After.Status != novel.PlanSuperseded || v1After.RecordVersion != 3 {
		t.Errorf("v1 = %+v, want superseded rv3", v1After)
	}
	n1After := findTestPlan(t, plans, "plan_n1")
	if n1After.Status != novel.PlanPaused || n1After.RecordVersion != 3 {
		t.Errorf("n1 = %+v, want paused rv3", n1After)
	}
	v2After := findTestPlan(t, plans, "plan_v2")
	if v2After.Status != novel.PlanActive || v2After.RecordVersion != 2 || v2After.LastSource != novel.LastSourceManualEdit {
		t.Errorf("v2 = %+v, want active rv2 manual", v2After)
	}
	if globalAfter := findTestPlan(t, plans, "plan_global"); globalAfter.Status != novel.PlanProposed {
		t.Errorf("global should stay proposed: %+v", globalAfter)
	}

	// 已 active 再激活 → 无提交、revision 不变。
	rev := memoryRevisionOf(t, st)
	noop, err := st.SetPlanStatus("plan_v2", novel.PlanActive)
	if err != nil {
		t.Fatalf("noop activate: %v", err)
	}
	if noop.CommitID != "" || noop.MemoryRevision != rev || memoryRevisionOf(t, st) != rev {
		t.Errorf("noop = %+v rev=%d", noop, rev)
	}
}

func TestCancelPlanPausesActiveDescendants(t *testing.T) {
	st, _ := newTestStore(t)
	global := testPlan("plan_global", "global", "", "总纲")
	volume := testPlan("plan_v", "volume", "plan_global", "第一卷")
	near := testPlan("plan_n", "near_term", "plan_v", "近期")
	if _, err := st.AddProposedPlans([]novel.StoryPlan{global, volume, near}); err != nil {
		t.Fatalf("AddProposedPlans: %v", err)
	}
	for _, id := range []string{"plan_global", "plan_v", "plan_n"} {
		if _, err := st.SetPlanStatus(id, novel.PlanActive); err != nil {
			t.Fatalf("activate %s: %v", id, err)
		}
	}
	before := memoryRevisionOf(t, st)

	result, err := st.SetPlanStatus("plan_v", novel.PlanCancelled)
	if err != nil {
		t.Fatalf("cancel volume: %v", err)
	}
	if result.MemoryRevision != before+1 {
		t.Errorf("revision = %d, want %d", result.MemoryRevision, before+1)
	}
	plans := loadTestPlans(t, st)
	if got := findTestPlan(t, plans, "plan_v"); got.Status != novel.PlanCancelled {
		t.Errorf("volume = %+v", got)
	}
	if got := findTestPlan(t, plans, "plan_n"); got.Status != novel.PlanPaused {
		t.Errorf("near_term = %+v, want paused", got)
	}
	if got := findTestPlan(t, plans, "plan_global"); got.Status != novel.PlanActive {
		t.Errorf("global = %+v, want untouched active", got)
	}
	// 同一提交：只递增一次。
	if got := memoryRevisionOf(t, st); got != before+1 {
		t.Errorf("revision after = %d, want %d", got, before+1)
	}
}

func TestCompletedPlanKeepsChildrenAndReconcileHints(t *testing.T) {
	st, _ := newTestStore(t)
	global := testPlan("plan_global", "global", "", "总纲")
	volume := testPlan("plan_v", "volume", "plan_global", "第一卷")
	near := testPlan("plan_n", "near_term", "plan_v", "近期")
	if _, err := st.AddProposedPlans([]novel.StoryPlan{global, volume, near}); err != nil {
		t.Fatalf("AddProposedPlans: %v", err)
	}
	for _, id := range []string{"plan_global", "plan_v", "plan_n"} {
		if _, err := st.SetPlanStatus(id, novel.PlanActive); err != nil {
			t.Fatalf("activate %s: %v", id, err)
		}
	}
	if _, err := st.SetPlanStatus("plan_v", novel.PlanCompleted); err != nil {
		t.Fatalf("complete volume: %v", err)
	}
	plans := loadTestPlans(t, st)
	if got := findTestPlan(t, plans, "plan_n"); got.Status != novel.PlanActive {
		t.Fatalf("near_term = %+v, completed parent must not auto-change child", got)
	}
	rev := memoryRevisionOf(t, st)

	marked, revision, err := st.ReconcileMemory(nil)
	if err != nil {
		t.Fatalf("ReconcileMemory: %v", err)
	}
	if marked != 1 || revision != rev+1 {
		t.Fatalf("marked/revision = %d/%d, want 1/%d", marked, revision, rev+1)
	}
	plans = loadTestPlans(t, st)
	hinted := findTestPlan(t, plans, "plan_n")
	if !hinted.NeedsReview || hinted.ReviewReason != "parent_completed" || hinted.Status != novel.PlanActive {
		t.Errorf("child hint = %+v", hinted)
	}
	// 再次 reconcile 无变化（no-op）。
	if _, revision, err = st.ReconcileMemory(nil); err != nil || revision != rev+1 {
		t.Fatalf("second reconcile revision = %d err=%v", revision, err)
	}
}

func TestPlanStatusTransitionsRejected(t *testing.T) {
	st, _ := newTestStore(t)
	global := testPlan("plan_global", "global", "", "总纲")
	if _, err := st.AddProposedPlans([]novel.StoryPlan{global}); err != nil {
		t.Fatalf("AddProposedPlans: %v", err)
	}
	if _, err := st.SetPlanStatus("plan_global", novel.PlanCompleted); !errors.Is(err, ErrPlanTransition) {
		t.Errorf("proposed→completed error = %v", err)
	}
	if _, err := st.SetPlanStatus("plan_global", novel.PlanSuperseded); !errors.Is(err, ErrPlanTransition) {
		t.Errorf("user superseded error = %v", err)
	}
	if _, err := st.SetPlanStatus("plan_missing", novel.PlanActive); !errors.Is(err, ErrPlanNotFound) {
		t.Errorf("missing error = %v", err)
	}
	if _, err := st.SetPlanStatus("plan_global", novel.StoryPlanStatus("bogus")); !errors.Is(err, ErrPlanTransition) {
		t.Errorf("invalid status error = %v", err)
	}
	if got := memoryRevisionOf(t, st); got != 1 {
		t.Errorf("revision = %d, want 1 (no rejected writes)", got)
	}
	if got := findTestPlan(t, loadTestPlans(t, st), "plan_global"); got.Status != novel.PlanProposed {
		t.Errorf("plan mutated by rejected transitions: %+v", got)
	}
}

func TestLoadPlanTreeEmpty(t *testing.T) {
	st, _ := newTestStore(t)
	tree, err := st.LoadPlanTree()
	if err != nil {
		t.Fatalf("LoadPlanTree: %v", err)
	}
	if tree.Count != 0 || tree.MemoryRevision != 0 || tree.Roots == nil || len(tree.Roots) != 0 {
		t.Errorf("empty tree = %+v", tree)
	}
	active, revision, err := st.LoadActivePlans()
	if err != nil || len(active) != 0 || active == nil || revision != 0 {
		t.Errorf("active = %+v rev=%d err=%v", active, revision, err)
	}
}

func TestLoadActivePlansSkipsReview(t *testing.T) {
	st, _ := newTestStore(t)
	global := testPlan("plan_global", "global", "", "总纲")
	stale := testPlan("plan_stale", "volume", "plan_global", "待复核卷")
	if _, err := st.AddProposedPlans([]novel.StoryPlan{global, stale}); err != nil {
		t.Fatalf("AddProposedPlans: %v", err)
	}
	for _, id := range []string{"plan_global", "plan_stale"} {
		if _, err := st.SetPlanStatus(id, novel.PlanActive); err != nil {
			t.Fatalf("activate %s: %v", id, err)
		}
	}
	plans := loadTestPlans(t, st)
	plans[1].NeedsReview = true
	plans[1].ReviewReason = "dependency_stale"
	seedFormalCommit(t, st, map[string]any{"plans.json": plans})

	active, _, err := st.LoadActivePlans()
	if err != nil {
		t.Fatalf("LoadActivePlans: %v", err)
	}
	if len(active) != 1 || active[0].ID != "plan_global" {
		t.Errorf("active = %+v, want only plan_global", active)
	}
}

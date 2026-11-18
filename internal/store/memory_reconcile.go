package store

import (
	"fmt"
	"time"

	"novelagent/internal/novel"
)

// 证据失效复核（I2-5，iter2-详设 §5，U9/U10）。
// 触发点：SaveChapter 正文/标题变化（Revision 递增）时联动复核该章；手动 POST /api/memory/reconcile 全量复核。
// 语义：章节 Revision 与 SourceRef.ChapterRevision 不一致 → evidence_stale；quote 与现正文不符 → quote_mismatch
// （status 不变，spec §5.1 红线）。有实际标记变化才提交 reconcile（+1），无变化不提交不递增（no-op 语义，§5.2）。
// 章节正文读取在 memoryMu 外完成（锁外快照），锁内只做内存比对。

// ReconcileMemory 手动全量（chapterIDs 空）或部分章节证据失效复核。返回标记变化数与新 memoryRevision。
func (s *Store) ReconcileMemory(chapterIDs []string) (int, int64, error) {
	// 1. store.mu 内快照章节正文（锁外于 memoryMu）。
	s.mu.Lock()
	snaps := s.snapshotChaptersForReconcileLocked(chapterIDs)
	s.mu.Unlock()
	// 2. memoryMu 内复核 + 提交。
	return s.reconcileWithSnapshotsLocked(snaps)
}

// reconcileForChapter 由 SaveChapter 保存路径收尾调用（store.mu 已持），对单章做证据复核。
func (s *Store) reconcileForChapter(chapter novel.Chapter) error {
	snaps := map[string]chapterSnap{
		chapter.ID: {Content: chapter.Content, Revision: chapter.Revision},
	}
	_, _, err := s.reconcileWithSnapshotsLocked(snaps)
	return err
}

// snapshotChaptersForReconcileLocked 在 store.mu 域内读取指定章节（空=全部）正文快照（锁外于 memoryMu）。
func (s *Store) snapshotChaptersForReconcileLocked(ids []string) map[string]chapterSnap {
	out := make(map[string]chapterSnap)
	wantAll := len(ids) == 0
	want := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		want[id] = struct{}{}
	}
	project, err := s.loadProjectLocked()
	if err != nil {
		return out
	}
	for _, meta := range project.Chapters {
		if !wantAll {
			if _, ok := want[meta.ID]; !ok {
				continue
			}
		}
		ch, err := s.readChapterLocked(meta.ID)
		if err != nil {
			continue
		}
		out[meta.ID] = chapterSnap{Content: ch.Content, Revision: ch.Revision}
	}
	return out
}

// reconcileWithSnapshotsLocked 在 memoryMu 域内基于章节快照复核五类记忆的 SourceRef。
// 调用方须持 memoryMu；章节快照须在 memoryMu 外读取（避免 memoryMu→store.mu 逆向持锁）。
func (s *Store) reconcileWithSnapshotsLocked(snaps map[string]chapterSnap) (int, int64, error) {
	if s.memorySafe {
		return 0, 0, fmt.Errorf("%w: %s", ErrMemorySafeState, s.memorySafeError)
	}
	events, err := s.loadMemoryEventsLocked()
	if err != nil {
		return 0, 0, err
	}
	states, err := s.loadMemoryStatesLocked()
	if err != nil {
		return 0, 0, err
	}
	cognitions, err := s.loadMemoryCognitionsLocked()
	if err != nil {
		return 0, 0, err
	}
	threads, err := s.loadMemoryThreadsLocked()
	if err != nil {
		return 0, 0, err
	}
	plans, err := s.loadMemoryPlansLocked()
	if err != nil {
		return 0, 0, err
	}

	now := time.Now().UTC()
	eventsMarked := reconcileMetas(func(i int) *novel.MemoryMeta { return &events[i].MemoryMeta }, len(events), snaps)
	statesMarked := reconcileMetas(func(i int) *novel.MemoryMeta { return &states[i].MemoryMeta }, len(states), snaps)
	cogsMarked := reconcileMetas(func(i int) *novel.MemoryMeta { return &cognitions[i].MemoryMeta }, len(cognitions), snaps)
	threadsMarked := reconcileMetas(func(i int) *novel.MemoryMeta { return &threads[i].MemoryMeta }, len(threads), snaps)
	plansMarked := reconcileMetas(func(i int) *novel.MemoryMeta { return &plans[i].MemoryMeta }, len(plans), snaps)

	// 直接证据失效后继续沿依赖链标记下游（详设 §7.2）；仅标记，不改 status/内容。
	propEvents, propStates, propCogs, propThreads, propPlans := propagateReviewDependencies(events, states, cognitions, threads, plans, now)
	eventsMarked += propEvents
	statesMarked += propStates
	cogsMarked += propCogs
	threadsMarked += propThreads
	plansMarked += propPlans
	// §4.5：父计划 completed 不自动变更子计划，由 reconcile 生成复核提示。
	plansMarked += markCompletedParentHints(plans, now)

	total := eventsMarked + statesMarked + cogsMarked + threadsMarked + plansMarked
	if total == 0 {
		meta, err := s.ensureMemoryMetaLocked()
		if err != nil {
			return 0, 0, err
		}
		return 0, meta.MemoryRevision, nil
	}

	payload := make(map[string]any, 5)
	if eventsMarked > 0 {
		payload["events.json"] = events
	}
	if statesMarked > 0 {
		payload["states.json"] = states
	}
	if cogsMarked > 0 {
		payload["cognitions.json"] = cognitions
	}
	if threadsMarked > 0 {
		payload["threads.json"] = threads
	}
	if plansMarked > 0 {
		payload["plans.json"] = plans
	}

	meta, err := s.ensureMemoryMetaLocked()
	if err != nil {
		return 0, 0, err
	}
	record, err := s.commitMemoryLocked(CommitReconcile, meta.MemoryRevision, payload)
	if err != nil {
		return 0, 0, err
	}
	return total, record.TargetMemoryRevision, nil
}

// reconcileMetas 对一组嵌入 MemoryMeta 的记录做证据失效复核，就地修改，返回标记变化数。
func reconcileMetas(get func(i int) *novel.MemoryMeta, n int, snaps map[string]chapterSnap) int {
	marked := 0
	for i := 0; i < n; i++ {
		if reconcileMeta(get(i), snaps) {
			marked++
		}
	}
	return marked
}

// markReview 将记录标记为待复核并递增 recordVersion；同因重复标记不递增（R8/U33）。
func markReview(m *novel.MemoryMeta, reason string, now time.Time) bool {
	if m.NeedsReview && m.ReviewReason == reason {
		return false
	}
	m.NeedsReview = true
	m.ReviewReason = reason
	m.RecordVersion++
	m.UpdatedAt = now
	return true
}

// markReviewDependency 仅标记尚无复核标记的记录，保留已有更具体的失效原因。
func markReviewDependency(m *novel.MemoryMeta, now time.Time) bool {
	if m.NeedsReview {
		return false
	}
	return markReview(m, "dependency_stale", now)
}

// markMissingSourceRefs 检查记录的全部证据来源章节是否仍存在；任一缺失 → chapter_deleted。
func markMissingSourceRefs(m *novel.MemoryMeta, indexes map[string]int, now time.Time) bool {
	for _, ref := range m.SourceRefs {
		if ref.ChapterID == "" {
			continue
		}
		if _, ok := indexes[ref.ChapterID]; !ok {
			return markReview(m, "chapter_deleted", now)
		}
	}
	return false
}

// propagateReviewDependencies 沿依赖链标记下游记录（详设 §7.2）：
// 事件前置/后果 → 事件；事件 → 状态/认知/伏笔；伏笔 → 计划。
// 只置 needsReview=dependency_stale，不改 status、不覆盖计划内容；proposed 骨架事件不参与传播。
func propagateReviewDependencies(
	events []novel.StoryEvent,
	states []novel.StoryState,
	cognitions []novel.Cognition,
	threads []novel.StoryThread,
	plans []novel.StoryPlan,
	now time.Time,
) (eventsMarked, statesMarked, cogsMarked, threadsMarked, plansMarked int) {
	badEvents := make(map[string]struct{}, len(events))
	for i := range events {
		if events[i].ID != "" && events[i].NeedsReview && events[i].Status != novel.EventProposed {
			badEvents[events[i].ID] = struct{}{}
		}
	}
	for pass := 0; pass <= len(events) && len(badEvents) > 0; pass++ {
		grew := false
		for i := range events {
			if events[i].NeedsReview {
				continue
			}
			if !eventLinksIntersect(events[i].Preconditions, badEvents) && !eventLinksIntersect(events[i].Consequences, badEvents) {
				continue
			}
			if markReviewDependency(&events[i].MemoryMeta, now) {
				eventsMarked++
				if events[i].ID != "" {
					badEvents[events[i].ID] = struct{}{}
				}
				grew = true
			}
		}
		if !grew {
			break
		}
	}

	badThreads := make(map[string]struct{}, len(threads))
	for i := range threads {
		if threads[i].ID != "" && threads[i].NeedsReview {
			badThreads[threads[i].ID] = struct{}{}
		}
	}
	for i := range threads {
		if threads[i].NeedsReview {
			continue
		}
		thread := &threads[i]
		if eventInSet(thread.IntroducedByEvent, badEvents) || eventInSet(thread.ResolutionEvent, badEvents) || eventsIntersect(thread.RelatedEvents, badEvents) {
			if markReviewDependency(&thread.MemoryMeta, now) {
				threadsMarked++
				if thread.ID != "" {
					badThreads[thread.ID] = struct{}{}
				}
			}
		}
	}

	for i := range states {
		if !states[i].NeedsReview && eventInSet(states[i].ValidFromEvent, badEvents) {
			if markReviewDependency(&states[i].MemoryMeta, now) {
				statesMarked++
			}
		}
	}
	for i := range cognitions {
		if !cognitions[i].NeedsReview && eventInSet(cognitions[i].KnownSinceEvent, badEvents) {
			if markReviewDependency(&cognitions[i].MemoryMeta, now) {
				cogsMarked++
			}
		}
	}
	for i := range plans {
		if !plans[i].NeedsReview && stringsIntersect(plans[i].DependsOnThreads, badThreads) {
			if markReviewDependency(&plans[i].MemoryMeta, now) {
				plansMarked++
			}
		}
	}

	// U23（S4 未完，仅交付依赖图与标记）：父计划进入复核态 → 子计划标记 dependency_stale，
	// 绝不自动取消/改写子计划；逐层向下传播。
	badPlans := make(map[string]struct{}, len(plans))
	for i := range plans {
		if plans[i].ID != "" && plans[i].NeedsReview {
			badPlans[plans[i].ID] = struct{}{}
		}
	}
	for pass := 0; pass <= len(plans) && len(badPlans) > 0; pass++ {
		grew := false
		for i := range plans {
			if plans[i].NeedsReview || plans[i].ParentPlanID == "" {
				continue
			}
			if _, ok := badPlans[plans[i].ParentPlanID]; !ok {
				continue
			}
			if markReviewDependency(&plans[i].MemoryMeta, now) {
				plansMarked++
				if plans[i].ID != "" {
					badPlans[plans[i].ID] = struct{}{}
				}
				grew = true
			}
		}
		if !grew {
			break
		}
	}
	return
}

// markCompletedParentHints 对父计划已 completed 的子计划生成复核提示（不改 status）。
func markCompletedParentHints(plans []novel.StoryPlan, now time.Time) int {
	completed := make(map[string]struct{})
	for i := range plans {
		if plans[i].ID != "" && plans[i].Status == novel.PlanCompleted {
			completed[plans[i].ID] = struct{}{}
		}
	}
	if len(completed) == 0 {
		return 0
	}
	marked := 0
	for i := range plans {
		if plans[i].NeedsReview || plans[i].ParentPlanID == "" {
			continue
		}
		if plans[i].Status != novel.PlanActive && plans[i].Status != novel.PlanProposed {
			continue
		}
		if _, ok := completed[plans[i].ParentPlanID]; !ok {
			continue
		}
		if markReview(&plans[i].MemoryMeta, "parent_completed", now) {
			marked++
		}
	}
	return marked
}

func eventInSet(id string, set map[string]struct{}) bool {
	if id == "" {
		return false
	}
	_, ok := set[id]
	return ok
}

func eventLinksIntersect(links []novel.EventLink, set map[string]struct{}) bool {
	for _, link := range links {
		if eventInSet(link.EventID, set) {
			return true
		}
	}
	return false
}

func eventsIntersect(ids []string, set map[string]struct{}) bool {
	for _, id := range ids {
		if eventInSet(id, set) {
			return true
		}
	}
	return false
}

func stringsIntersect(ids []string, set map[string]struct{}) bool {
	for _, id := range ids {
		if _, ok := set[id]; ok {
			return true
		}
	}
	return false
}

// reconcileMeta 复核单条记录的 SourceRef：章节 Revision 不符 → evidence_stale；quote 不符 → quote_mismatch。
// 仅当标记相对现状发生变化时才置 needsReview/reviewReason 并递增 recordVersion（R8；同值不递增，§5.2）。
// 章节缺失（已删）→ 跳过（DeleteChapter 路径已处理 chapter_deleted）。
func reconcileMeta(m *novel.MemoryMeta, snaps map[string]chapterSnap) bool {
	for _, ref := range m.SourceRefs {
		snap, ok := snaps[ref.ChapterID]
		if !ok {
			continue
		}
		if snap.Revision != ref.ChapterRevision {
			// 章节已改动 → 证据失效（status 不变）。
			if m.NeedsReview && m.ReviewReason == "evidence_stale" {
				continue
			}
			m.NeedsReview = true
			m.ReviewReason = "evidence_stale"
			m.RecordVersion++
			return true
		}
		if ref.Quote != "" {
			if err := novel.CheckSourceRefAgainstContent(snap.Content, ref); err != nil {
				// quote 与现正文不符。
				if m.NeedsReview && m.ReviewReason == "quote_mismatch" {
					continue
				}
				m.NeedsReview = true
				m.ReviewReason = "quote_mismatch"
				m.RecordVersion++
				return true
			}
		}
	}
	return false
}

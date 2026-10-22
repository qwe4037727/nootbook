package store

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"novelagent/internal/novel"
)

// waitForInitTerminal 轮询等待异步映射进入终态（ready/partial/failed）。
func waitForInitTerminal(t *testing.T, st *Store) MemoryInitState {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		state, err := st.LoadMemoryInitState()
		if err != nil {
			t.Fatalf("LoadMemoryInitState: %v", err)
		}
		switch state.Status {
		case InitReady, InitPartial, InitFailed:
			return state
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("initialization did not reach terminal state in time")
	return MemoryInitState{}
}

// writeKnowledgeDirect 测试辅助：直接落盘 knowledge.json（绕过分析流程）。
func writeKnowledgeDirect(t *testing.T, st *Store, knowledge novel.Knowledge) {
	t.Helper()
	st.mu.Lock()
	defer st.mu.Unlock()
	if err := st.writeKnowledgeLocked(knowledge); err != nil {
		t.Fatalf("write knowledge: %v", err)
	}
}

// TestInitLegacyProjectRejected 覆盖 U26：legacy project.json（含 manuscript、无 chapters/）→ 409 语义、零写入。
func TestInitLegacyProjectRejected(t *testing.T) {
	st, dir := newTestStore(t)
	// New 之后手工放入 legacy project.json（启动迁移已跑过，不会自动迁）
	legacy := `{"title":"旧书","manuscript":"第一章 起点\n老正文。","style":"","characters":[],"world":[]}`
	if err := os.WriteFile(dir+"/project.json", []byte(legacy), 0o644); err != nil {
		t.Fatalf("write legacy project: %v", err)
	}

	_, err := st.InitializeMemory()
	if !errors.Is(err, ErrProjectNotMigrated) {
		t.Fatalf("error = %v, want ErrProjectNotMigrated", err)
	}
	// 零写入：memory 目录不应产生 events/states
	if _, err := os.Stat(dir + "/memory/events.json"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("events.json should not exist, err=%v", err)
	}
}

// TestInitKnowledgeNotReady 覆盖 U27：chapters 存在但 knowledge 全空 → 409 语义。
func TestInitKnowledgeNotReady(t *testing.T) {
	st, _ := newTestStore(t)
	if _, err := st.ImportChapters("书", []novel.Chapter{{Title: "一", Content: "一。"}}); err != nil {
		t.Fatalf("import: %v", err)
	}
	if _, err := st.InitializeMemory(); !errors.Is(err, ErrKnowledgeNotReady) {
		t.Fatalf("error = %v, want ErrKnowledgeNotReady", err)
	}
}

// TestInitZeroResultPartial 覆盖 U28：前置通过但映射产出 0 → partial + no_mappable_input，≠ ready。
func TestInitZeroResultPartial(t *testing.T) {
	st, dir := newTestStore(t)
	if _, err := st.ImportChapters("书", []novel.Chapter{{Title: "一", Content: "一。"}}); err != nil {
		t.Fatalf("import: %v", err)
	}
	// characters 非空（前置通过）但 State 全空、无 summaries → 零映射
	writeKnowledgeDirect(t, st, novel.Knowledge{
		Characters: []novel.Character{{ID: "c_a1", Name: "林越", State: ""}},
		World:      []novel.WorldEntry{{ID: "w_b1", Title: "青云宗"}},
	})

	if _, err := st.InitializeMemory(); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	state := waitForInitTerminal(t, st)
	if state.Status != InitPartial || state.LastError != "no_mappable_input" {
		t.Errorf("state = %+v, want partial/no_mappable_input", state)
	}
	info, _ := st.LoadMemoryInfo()
	if info.MemoryRevision != 0 {
		t.Errorf("memoryRevision = %d, want 0 (no commit for zero result)", info.MemoryRevision)
	}
	_ = dir
}

// TestInitBackfillMapping 覆盖 U8/U20：回填映射 → 事件 proposed 骨架 + active 状态（复用 c_ ID）→ ready。
func TestInitBackfillMapping(t *testing.T) {
	st, dir := newTestStore(t)
	project, err := st.ImportChapters("书", []novel.Chapter{
		{Title: "一", Content: "一。"},
		{Title: "二", Content: "二。"},
	})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	writeKnowledgeDirect(t, st, novel.Knowledge{
		Characters: []novel.Character{{ID: "c_a1", Name: "林越", State: "重伤未愈"}},
		Summaries: []novel.ChapterSummary{
			{ChapterID: project.Chapters[0].ID, Index: 1, Summary: "第一章摘要"},
			{ChapterID: project.Chapters[1].ID, Index: 2, Summary: "第二章摘要"},
		},
	})

	if _, err := st.InitializeMemory(); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	state := waitForInitTerminal(t, st)
	if state.Status != InitReady {
		t.Fatalf("status = %s (%s), want ready", state.Status, state.LastError)
	}
	if state.Stats.MappedEvents != 2 || state.Stats.MappedStates != 1 {
		t.Errorf("stats = %+v", state.Stats)
	}
	if !state.Preconditions.Migrated || !state.Preconditions.KnowledgeReady {
		t.Errorf("preconditions = %+v", state.Preconditions)
	}

	// 事件骨架：proposed + needsReview + confidence 0.3 + origin kind=summary
	events := loadEventsForTest(t, dir)
	if len(events) != 2 {
		t.Fatalf("events = %d, want 2", len(events))
	}
	for _, event := range events {
		if event.Status != novel.EventProposed || !event.NeedsReview {
			t.Errorf("event %s status=%v needsReview=%v, want proposed/true", event.ID, event.Status, event.NeedsReview)
		}
		if event.Confidence != 0.3 || event.Origin.Kind != "summary" {
			t.Errorf("event meta = %+v", event.MemoryMeta)
		}
		if event.ParticipantsKnown {
			t.Errorf("backfill skeleton must have participantsKnown=false")
		}
	}
	if events[0].StoryOrder != 1 || events[1].StoryOrder != 2 {
		t.Errorf("storyOrder = %d/%d", events[0].StoryOrder, events[1].StoryOrder)
	}

	// 状态：active + needsReview（无证据）+ subjectId 复用 c_a1（U8）
	states := loadStatesForTest(t, dir)
	if len(states) != 1 {
		t.Fatalf("states = %d, want 1", len(states))
	}
	s := states[0]
	if s.Status != novel.StateActive || !s.NeedsReview || s.ReviewReason != "backfill_unverified" {
		t.Errorf("state = %+v", s)
	}
	if s.SubjectID != "c_a1" || s.Category != "general" || s.Attribute != "profile" || s.Value != "重伤未愈" {
		t.Errorf("state content = %+v", s)
	}
	if s.Confidence != 0.5 {
		t.Errorf("confidence = %v, want 0.5", s.Confidence)
	}

	info, _ := st.LoadMemoryInfo()
	if info.MemoryRevision != 1 {
		t.Errorf("memoryRevision = %d, want 1 (one initialize commit)", info.MemoryRevision)
	}
}

// TestInitIdempotentRerun 覆盖 U6：回填完成后再触发（partial 续跑）→ 记录数不变、无重复提交。
func TestInitIdempotentRerun(t *testing.T) {
	st, dir := newTestStore(t)
	project, err := st.ImportChapters("书", []novel.Chapter{{Title: "一", Content: "一。"}})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	writeKnowledgeDirect(t, st, novel.Knowledge{
		Characters: []novel.Character{{ID: "c_a1", Name: "林越", State: "重伤"}},
		Summaries:  []novel.ChapterSummary{{ChapterID: project.Chapters[0].ID, Index: 1, Summary: "摘要"}},
	})
	if _, err := st.InitializeMemory(); err != nil {
		t.Fatalf("first init: %v", err)
	}
	waitForInitTerminal(t, st)
	info1, _ := st.LoadMemoryInfo()
	events1 := loadEventsForTest(t, dir)

	// 置回 partial 模拟续跑场景
	st.memoryMu.Lock()
	initState, _ := st.loadMemoryInitStateLocked()
	initState.Status = InitPartial
	_ = st.writeMemoryInitStateLocked(initState)
	st.memoryMu.Unlock()

	if _, err := st.InitializeMemory(); err != nil {
		t.Fatalf("second init: %v", err)
	}
	state := waitForInitTerminal(t, st)
	events2 := loadEventsForTest(t, dir)
	if len(events2) != len(events1) {
		t.Errorf("events grew on rerun: %d -> %d", len(events1), len(events2))
	}
	info2, _ := st.LoadMemoryInfo()
	if info2.MemoryRevision != info1.MemoryRevision {
		t.Errorf("memoryRevision changed on rerun with zero new records: %d -> %d", info1.MemoryRevision, info2.MemoryRevision)
	}
	if state.Stats.MappedEvents+state.Stats.MappedStates != 0 {
		t.Errorf("rerun should map nothing (origin dedup), stats = %+v", state.Stats)
	}
}

// TestInitManualEditProtected 覆盖 U7：manual_edit 的 StoryState + 重新回填 → Value 不被覆盖。
// 场景：用户手工创建（origin=manual）的状态先存在，回填随后尝试映射同三元组。
func TestInitManualEditProtected(t *testing.T) {
	st, dir := newTestStore(t)
	project, err := st.ImportChapters("书", []novel.Chapter{{Title: "一", Content: "一。"}})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	// 初始 knowledge 无 State → 回填只产出事件
	writeKnowledgeDirect(t, st, novel.Knowledge{
		Characters: []novel.Character{{ID: "c_a1", Name: "林越", State: ""}},
		Summaries:  []novel.ChapterSummary{{ChapterID: project.Chapters[0].ID, Index: 1, Summary: "摘要"}},
	})
	if _, err := st.InitializeMemory(); err != nil {
		t.Fatalf("first init: %v", err)
	}
	waitForInitTerminal(t, st)

	// 模拟人工修订：手工创建 active 状态（origin=manual / lastSource=manual_edit）
	manual := novel.StoryState{
		ID: "state_manual1",
		MemoryMeta: novel.MemoryMeta{
			RecordVersion: 1,
			CreatedAt:     time.Now().UTC(),
			UpdatedAt:     time.Now().UTC(),
			NeedsReview:   false,
			Origin:        novel.Origin{Type: novel.OriginManual},
			LastSource:    novel.LastSourceManualEdit,
		},
		Status:     novel.StateActive,
		SubjectID:  "c_a1",
		Category:   "general",
		Attribute:  "profile",
		Value:      "伤愈复出",
	}
	st.memoryMu.Lock()
	_ = st.writeMemoryFileLocked("states.json", []novel.StoryState{manual})
	// knowledge 补上 State 并把 init 置回 partial 触发续跑
	initState, _ := st.loadMemoryInitStateLocked()
	initState.Status = InitPartial
	_ = st.writeMemoryInitStateLocked(initState)
	st.memoryMu.Unlock()
	writeKnowledgeDirect(t, st, novel.Knowledge{
		Characters: []novel.Character{{ID: "c_a1", Name: "林越", State: "重伤"}},
		Summaries:  []novel.ChapterSummary{{ChapterID: project.Chapters[0].ID, Index: 1, Summary: "摘要"}},
	})

	if _, err := st.InitializeMemory(); err != nil {
		t.Fatalf("second init: %v", err)
	}
	state := waitForInitTerminal(t, st)
	if state.Stats.SkippedManualProtected != 1 {
		t.Errorf("stats = %+v, want 1 manual protected", state.Stats)
	}
	states := loadStatesForTest(t, dir)
	if len(states) != 1 || states[0].Value != "伤愈复出" {
		t.Errorf("manual edit overwritten: %+v", states)
	}
}

// TestInitReadyIdempotentRetry 覆盖 U30：ready 后重试 → ErrAlreadyInitialized、无任何写入。
func TestInitReadyIdempotentRetry(t *testing.T) {
	st, _ := newTestStore(t)
	project, err := st.ImportChapters("书", []novel.Chapter{{Title: "一", Content: "一。"}})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	writeKnowledgeDirect(t, st, novel.Knowledge{
		Characters: []novel.Character{{ID: "c_a1", Name: "林越", State: "重伤"}},
		Summaries:  []novel.ChapterSummary{{ChapterID: project.Chapters[0].ID, Index: 1, Summary: "摘要"}},
	})
	if _, err := st.InitializeMemory(); err != nil {
		t.Fatalf("first init: %v", err)
	}
	waitForInitTerminal(t, st)
	info1, _ := st.LoadMemoryInfo()

	phase, err := st.InitializeMemory()
	if phase != InitReady || !errors.Is(err, ErrAlreadyInitialized) {
		t.Fatalf("retry = (%s, %v), want (ready, ErrAlreadyInitialized)", phase, err)
	}
	info2, _ := st.LoadMemoryInfo()
	if info1.MemoryRevision != info2.MemoryRevision || info1.LastCommitID != info2.LastCommitID {
		t.Errorf("ready retry produced writes: %+v vs %+v", info1, info2)
	}
}

// TestStoryOrderRecalcOnDelete 覆盖 U17/决策 16：删除中间章 → 事件 storyOrder 批量重算留痕（reconcile 提交）。
func TestStoryOrderRecalcOnDelete(t *testing.T) {
	st, dir := newTestStore(t)
	project, err := st.ImportChapters("书", []novel.Chapter{
		{Title: "一", Content: "一。"},
		{Title: "二", Content: "二。"},
		{Title: "三", Content: "三。"},
	})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	summaries := []novel.ChapterSummary{
		{ChapterID: project.Chapters[0].ID, Index: 1, Summary: "摘要一"},
		{ChapterID: project.Chapters[1].ID, Index: 2, Summary: "摘要二"},
		{ChapterID: project.Chapters[2].ID, Index: 3, Summary: "摘要三"},
	}
	writeKnowledgeDirect(t, st, novel.Knowledge{Summaries: summaries})
	if _, err := st.InitializeMemory(); err != nil {
		t.Fatalf("init: %v", err)
	}
	waitForInitTerminal(t, st)
	infoBefore, _ := st.LoadMemoryInfo()

	// 删除第 1 章 → 第 2/3 章前移
	updated, err := st.DeleteChapter(project.Chapters[0].ID)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if len(updated.Chapters) != 2 {
		t.Fatalf("chapters after delete = %d", len(updated.Chapters))
	}

	// 回调在锁外异步执行——轮询等待重算提交落地
	deadline := time.Now().Add(5 * time.Second)
	var events []novel.StoryEvent
	for time.Now().Before(deadline) {
		events = loadEventsForTest(t, dir)
		if len(events) == 3 && events[0].StoryOrder == 0 && events[1].StoryOrder == 1 && events[2].StoryOrder == 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(events) != 3 {
		t.Fatalf("events = %d, want 3", len(events))
	}
	if events[0].StoryOrder != 0 || events[0].ReviewReason != "chapter_deleted" || !events[0].NeedsReview {
		t.Errorf("deleted-chapter event = order %d reason %q review %v", events[0].StoryOrder, events[0].ReviewReason, events[0].NeedsReview)
	}
	if events[1].StoryOrder != 1 || events[2].StoryOrder != 2 {
		t.Errorf("storyOrder not recalculated: %d/%d", events[1].StoryOrder, events[2].StoryOrder)
	}
	if events[1].RecordVersion != events[2].RecordVersion {
		t.Errorf("pure reorder must not bump recordVersion (R8)")
	}
	if events[0].RecordVersion != events[1].RecordVersion+1 {
		t.Errorf("needsReview change must bump recordVersion (R8): %d vs %d", events[0].RecordVersion, events[1].RecordVersion)
	}

	// 重算留痕：memoryRevision +1（reconcile 提交）
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		info, _ := st.LoadMemoryInfo()
		if info.MemoryRevision == infoBefore.MemoryRevision+1 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	infoAfter, _ := st.LoadMemoryInfo()
	t.Errorf("memoryRevision = %d, want %d (reconcile commit)", infoAfter.MemoryRevision, infoBefore.MemoryRevision+1)
}

// TestInitDecodeFailureFailed 覆盖 §9.0 检查 3：knowledge 解码失败 → 启动后 failed + lastError。
func TestInitDecodeFailureFailed(t *testing.T) {
	st, dir := newTestStore(t)
	if _, err := st.ImportChapters("书", []novel.Chapter{{Title: "一", Content: "一。"}}); err != nil {
		t.Fatalf("import: %v", err)
	}
	if err := os.WriteFile(dir+"/knowledge.json", []byte("{corrupt"), 0o600); err != nil {
		t.Fatalf("write corrupt knowledge: %v", err)
	}
	// 前置检查：解码失败不算 knowledge_not_ready（§9.0 检查 3 语义）
	if _, err := st.InitializeMemory(); err != nil {
		t.Fatalf("initialize should start, error = %v", err)
	}
	state := waitForInitTerminal(t, st)
	if state.Status != InitFailed || state.LastError == "" {
		t.Errorf("state = %+v, want failed with lastError", state)
	}
	if !strings.Contains(state.LastError, "knowledge") {
		t.Errorf("lastError = %q, want decode error mentioning knowledge", state.LastError)
	}
}

// ---- 测试辅助：直接读取 memory 数据文件 ----

func loadEventsForTest(t *testing.T, dir string) []novel.StoryEvent {
	t.Helper()
	raw, err := os.ReadFile(dir + "/memory/events.json")
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatalf("read events.json: %v", err)
	}
	var events []novel.StoryEvent
	if err := json.Unmarshal(raw, &events); err != nil {
		t.Fatalf("decode events.json: %v", err)
	}
	return events
}

func loadStatesForTest(t *testing.T, dir string) []novel.StoryState {
	t.Helper()
	raw, err := os.ReadFile(dir + "/memory/states.json")
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatalf("read states.json: %v", err)
	}
	var states []novel.StoryState
	if err := json.Unmarshal(raw, &states); err != nil {
		t.Fatalf("decode states.json: %v", err)
	}
	return states
}

// TestInitAlreadyRunning 覆盖 U15：mapping 中重入 → 409 already_running 语义。
func TestInitAlreadyRunning(t *testing.T) {
	st, _ := newTestStore(t)
	project, err := st.ImportChapters("书", []novel.Chapter{{Title: "一", Content: "一。"}})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	writeKnowledgeDirect(t, st, novel.Knowledge{
		Summaries: []novel.ChapterSummary{{ChapterID: project.Chapters[0].ID, Index: 1, Summary: "摘要"}},
	})

	// 人为置 mapping（模拟长映射进行中；InitializeMemory 读到 mapping 即拒绝重入）
	st.memoryMu.Lock()
	initState := MemoryInitState{Status: InitMapping, StartedAt: time.Now().UTC()}
	if err := st.writeMemoryInitStateLocked(initState); err != nil {
		st.memoryMu.Unlock()
		t.Fatalf("write state: %v", err)
	}
	st.memoryMu.Unlock()
	phase, err := st.InitializeMemory()
	if phase != InitMapping || !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("reentry = (%s, %v), want (mapping, ErrAlreadyRunning)", phase, err)
	}
	// 状态未被破坏
	st.memoryMu.Lock()
	state, _ := st.loadMemoryInitStateLocked()
	st.memoryMu.Unlock()
	if state.Status != InitMapping {
		t.Errorf("status = %q, want mapping", state.Status)
	}
}

// TestLegacyToEndMemoryFlow 端到端集成冒烟：legacy 迁移 → 知识直写 → 回填 → 查询聚合。
func TestLegacyToEndMemoryFlow(t *testing.T) {
	dir := t.TempDir()
	// 1. legacy 数据入库（New 自动迁移分章）
	legacy := `{"title":"无边悔恨","manuscript":"第一章 起点\n他睁开眼。\n\n第二章 转折\n雨停了。","style":"冷峻","characters":[{"name":"林越","state":"重伤"}],"world":[]}`
	if err := osWriteFileForTest(dir+"/project.json", []byte(legacy)); err != nil {
		t.Fatalf("write legacy: %v", err)
	}
	st, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	project, err := st.LoadProject()
	if err != nil {
		t.Fatalf("load project: %v", err)
	}
	if len(project.Chapters) != 2 {
		t.Fatalf("migrated chapters = %d, want 2", len(project.Chapters))
	}

	// 2. 模拟分析产物：迁移产生的 c_ 实体 + 章节摘要
	knowledge, err := st.LoadKnowledge()
	if err != nil {
		t.Fatalf("load knowledge: %v", err)
	}
	if len(knowledge.Characters) == 0 {
		t.Fatalf("migrated knowledge has no characters")
	}
	characterID := knowledge.Characters[0].ID
	knowledge.Characters[0].State = "重伤" // 模拟分析提取到的角色状态（迁移产物 State 为空）
	knowledge.Summaries = []novel.ChapterSummary{
		{ChapterID: project.Chapters[0].ID, Index: 1, Summary: "第一章摘要"},
		{ChapterID: project.Chapters[1].ID, Index: 2, Summary: "第二章摘要"},
	}
	writeKnowledgeDirect(t, st, knowledge)

	// 3. 回填
	if _, err := st.InitializeMemory(); err != nil {
		t.Fatalf("init: %v", err)
	}
	state := waitForInitTerminal(t, st)
	if state.Status != InitReady {
		t.Fatalf("status = %s (%s), want ready", state.Status, state.LastError)
	}

	// 4. 查询聚合
	overview, err := st.LoadMemoryOverview()
	if err != nil {
		t.Fatalf("overview: %v", err)
	}
	if overview.Counts.Events != 2 || overview.Counts.States != 1 || overview.MemoryRevision != 1 {
		t.Errorf("overview = %+v", overview)
	}
	view, err := st.LoadCharacterMemory(characterID)
	if err != nil {
		t.Fatalf("character view: %v", err)
	}
	if len(view.States) != 1 || view.States[0].SubjectID != characterID {
		t.Errorf("view states = %+v", view.States)
	}
	if view.Character.ID != characterID {
		t.Errorf("view character = %+v", view.Character)
	}
}

func osWriteFileForTest(path string, data []byte) error {
	return os.WriteFile(path, data, 0o644)
}

package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"novelagent/internal/novel"
)

func readMemoryJSON[T any](t *testing.T, dir, name string) []T {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, "memory", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	var out []T
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode %s: %v", name, err)
	}
	return out
}

func sourceMeta(chapter novel.Chapter) novel.MemoryMeta {
	return novel.MemoryMeta{
		RecordVersion: 1,
		SourceRefs: []novel.SourceRef{{
			ChapterID:       chapter.ID,
			ChapterRevision: chapter.Revision,
			StartOffset:     0,
			EndOffset:       1,
			OffsetUnit:      "rune",
		}},
	}
}

// TestDeleteChapterMarksAllMemoryKinds 覆盖 G4 删章影响面：
// 五类记忆按来源章节标记 chapter_deleted，计划沿伏笔依赖标记 dependency_stale，
// status/内容一律不变（不自动覆盖计划），未涉及章节不受影响。
func TestDeleteChapterMarksAllMemoryKinds(t *testing.T) {
	st, dir := newTestStore(t)
	if _, _, err := st.AppendChapter("第一章", "一章正文。"); err != nil {
		t.Fatalf("append ch1: %v", err)
	}
	ch2, _, err := st.AppendChapter("第二章", "二章正文。")
	if err != nil {
		t.Fatalf("append ch2: %v", err)
	}
	ch3, _, err := st.AppendChapter("第三章", "三章正文。")
	if err != nil {
		t.Fatalf("append ch3: %v", err)
	}

	writeMemoryRetrieveFile(t, dir, "events.json", []novel.StoryEvent{
		{ID: "evt_del", MemoryMeta: sourceMeta(ch2), Status: novel.EventConfirmed, EventType: "other", Summary: "已删章节事件", ChapterID: ch2.ID, StoryOrder: 2},
		{ID: "evt_keep", MemoryMeta: sourceMeta(ch3), Status: novel.EventConfirmed, EventType: "other", Summary: "保留章节事件", ChapterID: ch3.ID, StoryOrder: 3},
	})
	writeMemoryRetrieveFile(t, dir, "states.json", []novel.StoryState{
		{ID: "state_del", MemoryMeta: sourceMeta(ch2), Status: novel.StateActive, SubjectID: "c_lin", Category: "状态", Attribute: "伤势", Value: "重伤"},
		{ID: "state_keep", MemoryMeta: sourceMeta(ch3), Status: novel.StateActive, SubjectID: "c_lin", Category: "状态", Attribute: "位置", Value: "渡口"},
	})
	writeMemoryRetrieveFile(t, dir, "cognitions.json", []novel.Cognition{
		{ID: "cog_del", MemoryMeta: sourceMeta(ch2), Status: novel.CogKnown, SubjectID: "c_lin", Content: "以为师父已死"},
		{ID: "cog_keep", MemoryMeta: sourceMeta(ch3), Status: novel.CogKnown, SubjectID: "c_lin", Content: "知道铜牌下落"},
	})
	writeMemoryRetrieveFile(t, dir, "threads.json", []novel.StoryThread{
		{ID: "thread_del", MemoryMeta: sourceMeta(ch2), Status: novel.ThreadOpen, Title: "已删章节伏笔", InvolvedCharacters: []string{"c_lin"}},
		{ID: "thread_keep", MemoryMeta: sourceMeta(ch3), Status: novel.ThreadOpen, Title: "保留伏笔", InvolvedCharacters: []string{"c_lin"}},
	})
	writeMemoryRetrieveFile(t, dir, "plans.json", []novel.StoryPlan{
		{ID: "plan_dep", Status: novel.PlanActive, Level: "near_term", Title: "寻找疗伤方法", Objective: "保持原计划不变", DependsOnThreads: []string{"thread_del"}},
		{ID: "plan_child", Status: novel.PlanActive, Level: "near_term", ParentPlanID: "plan_dep", Title: "子计划", Objective: "不得被自动取消"},
		{ID: "plan_keep", Status: novel.PlanActive, Level: "near_term", Title: "南下", DependsOnThreads: []string{"thread_keep"}},
	})

	if _, err := st.DeleteChapter(ch2.ID); err != nil {
		t.Fatalf("DeleteChapter: %v", err)
	}

	events := readMemoryJSON[novel.StoryEvent](t, dir, "events.json")
	for _, event := range events {
		switch event.ID {
		case "evt_del":
			if !event.NeedsReview || event.ReviewReason != "chapter_deleted" || event.StoryOrder != 0 || event.RecordVersion != 2 {
				t.Errorf("deleted event = %+v", event)
			}
			if event.Status != novel.EventConfirmed {
				t.Errorf("deleted event status changed: %s", event.Status)
			}
		case "evt_keep":
			if event.NeedsReview {
				t.Errorf("kept event wrongly marked: %+v", event)
			}
		}
	}

	states := readMemoryJSON[novel.StoryState](t, dir, "states.json")
	for _, state := range states {
		switch state.ID {
		case "state_del":
			if !state.NeedsReview || state.ReviewReason != "chapter_deleted" || state.RecordVersion != 2 {
				t.Errorf("deleted state = %+v", state)
			}
			if state.Status != novel.StateActive || state.Value != "重伤" {
				t.Errorf("deleted state status/value changed: %+v", state)
			}
		case "state_keep":
			if state.NeedsReview {
				t.Errorf("kept state wrongly marked: %+v", state)
			}
		}
	}

	cogs := readMemoryJSON[novel.Cognition](t, dir, "cognitions.json")
	if cogs[0].ID == "cog_del" && (!cogs[0].NeedsReview || cogs[0].ReviewReason != "chapter_deleted") {
		t.Errorf("deleted cognition = %+v", cogs[0])
	}
	if cogs[1].ID == "cog_keep" && cogs[1].NeedsReview {
		t.Errorf("kept cognition wrongly marked: %+v", cogs[1])
	}

	threads := readMemoryJSON[novel.StoryThread](t, dir, "threads.json")
	if threads[0].ID == "thread_del" && (!threads[0].NeedsReview || threads[0].ReviewReason != "chapter_deleted") {
		t.Errorf("deleted thread = %+v", threads[0])
	}
	if threads[1].ID == "thread_keep" && threads[1].NeedsReview {
		t.Errorf("kept thread wrongly marked: %+v", threads[1])
	}

	plans := readMemoryJSON[novel.StoryPlan](t, dir, "plans.json")
	for _, plan := range plans {
		switch plan.ID {
		case "plan_dep":
			if !plan.NeedsReview || plan.ReviewReason != "dependency_stale" {
				t.Errorf("dependent plan = %+v, want dependency_stale", plan)
			}
			if plan.Status != novel.PlanActive || plan.Objective != "保持原计划不变" {
				t.Errorf("plan was auto-overwritten: %+v", plan)
			}
		case "plan_child":
			if !plan.NeedsReview || plan.ReviewReason != "dependency_stale" {
				t.Errorf("child plan = %+v, want dependency_stale from parent", plan)
			}
			if plan.Status != novel.PlanActive || plan.Objective != "不得被自动取消" {
				t.Errorf("child plan was auto-cancelled/overwritten: %+v", plan)
			}
		case "plan_keep":
			if plan.NeedsReview {
				t.Errorf("kept plan wrongly marked: %+v", plan)
			}
		}
	}

	if revision := readMemoryRevision(t, dir); revision != 1 {
		t.Errorf("memoryRevision = %d, want 1", revision)
	}
}

// TestEditMiddleChapterPropagatesWithoutOverwrite 覆盖 G4 改中段章节：
// 直接证据 evidence_stale → 依赖事件的状态/认知/伏笔标记 dependency_stale → 依赖伏笔的计划同样标记；
// status 与计划内容不变，检索红线不注入；再次复核 no-op 不递增 revision。
func TestEditMiddleChapterPropagatesWithoutOverwrite(t *testing.T) {
	st, dir := newTestStore(t)
	if _, _, err := st.AppendChapter("第一章", "一章正文。"); err != nil {
		t.Fatalf("append ch1: %v", err)
	}
	ch2, _, err := st.AppendChapter("第二章", "二章正文。")
	if err != nil {
		t.Fatalf("append ch2: %v", err)
	}
	ch3, _, err := st.AppendChapter("第三章", "三章正文。")
	if err != nil {
		t.Fatalf("append ch3: %v", err)
	}

	writeMemoryRetrieveFile(t, dir, "events.json", []novel.StoryEvent{
		{ID: "evt_mid", MemoryMeta: sourceMeta(ch2), Status: novel.EventConfirmed, EventType: "injury", Summary: "师父重伤", ChapterID: ch2.ID, StoryOrder: 2, Participants: []string{"c_lin"}},
		{ID: "evt_late", MemoryMeta: sourceMeta(ch3), Status: novel.EventConfirmed, EventType: "other", Summary: "第三章事件", ChapterID: ch3.ID, StoryOrder: 3, Participants: []string{"c_lin"}},
	})
	writeMemoryRetrieveFile(t, dir, "states.json", []novel.StoryState{
		{ID: "state_dep", Status: novel.StateActive, SubjectID: "c_lin", Category: "状态", Attribute: "师父", Value: "重伤", ValidFromEvent: "evt_mid"},
		{ID: "state_late", MemoryMeta: sourceMeta(ch3), Status: novel.StateActive, SubjectID: "c_lin", Category: "状态", Attribute: "位置", Value: "渡口"},
	})
	writeMemoryRetrieveFile(t, dir, "cognitions.json", []novel.Cognition{
		{ID: "cog_dep", Status: novel.CogWrong, SubjectID: "c_lin", Content: "以为师父已死", KnownSinceEvent: "evt_mid"},
	})
	writeMemoryRetrieveFile(t, dir, "threads.json", []novel.StoryThread{
		{ID: "thread_dep", Status: novel.ThreadOpen, Title: "寻找疗伤方法", IntroducedByEvent: "evt_mid", InvolvedCharacters: []string{"c_lin"}},
	})
	writeMemoryRetrieveFile(t, dir, "plans.json", []novel.StoryPlan{
		{ID: "plan_dep", Status: novel.PlanActive, Level: "near_term", Title: "寻医", Objective: "为师父找药", DependsOnThreads: []string{"thread_dep"}, InvolvedCharacters: []string{"c_lin"}},
	})

	edited := ch2
	edited.Content = "二章正文改写：师父并未受伤。"
	if _, _, err := st.SaveChapter(edited); err != nil {
		t.Fatalf("SaveChapter: %v", err)
	}

	events := readMemoryJSON[novel.StoryEvent](t, dir, "events.json")
	if events[0].ID != "evt_mid" || !events[0].NeedsReview || events[0].ReviewReason != "evidence_stale" || events[0].Status != novel.EventConfirmed {
		t.Errorf("edited event = %+v, want evidence_stale with status unchanged", events[0])
	}
	if events[1].NeedsReview {
		t.Errorf("later-chapter event wrongly marked: %+v", events[1])
	}

	states := readMemoryJSON[novel.StoryState](t, dir, "states.json")
	if states[0].ID != "state_dep" || !states[0].NeedsReview || states[0].ReviewReason != "dependency_stale" || states[0].Status != novel.StateActive || states[0].Value != "重伤" {
		t.Errorf("dependent state = %+v", states[0])
	}
	if states[1].NeedsReview {
		t.Errorf("later-chapter state wrongly marked: %+v", states[1])
	}

	cogs := readMemoryJSON[novel.Cognition](t, dir, "cognitions.json")
	if !cogs[0].NeedsReview || cogs[0].ReviewReason != "dependency_stale" || cogs[0].Status != novel.CogWrong {
		t.Errorf("dependent cognition = %+v", cogs[0])
	}
	threads := readMemoryJSON[novel.StoryThread](t, dir, "threads.json")
	if !threads[0].NeedsReview || threads[0].ReviewReason != "dependency_stale" || threads[0].Status != novel.ThreadOpen {
		t.Errorf("dependent thread = %+v", threads[0])
	}
	plans := readMemoryJSON[novel.StoryPlan](t, dir, "plans.json")
	if !plans[0].NeedsReview || plans[0].ReviewReason != "dependency_stale" {
		t.Errorf("dependent plan = %+v", plans[0])
	}
	if plans[0].Status != novel.PlanActive || plans[0].Objective != "为师父找药" || plans[0].Title != "寻医" {
		t.Errorf("plan was auto-overwritten: %+v", plans[0])
	}

	knowledge := novel.Knowledge{Characters: []novel.Character{{ID: "c_lin", Name: "林渊", Confidence: 0.9}}}
	result, err := st.RetrieveContinuationMemory(knowledge, "林渊回到渡口。", 2, 3000)
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	for _, record := range result.Records {
		switch record.ID {
		case "evt_mid", "state_dep", "cog_dep", "thread_dep", "plan_dep":
			t.Errorf("review-marked record injected: %+v", record)
		}
	}
	if result.SkippedReview == 0 {
		t.Error("SkippedReview = 0, want review-marked records counted")
	}

	revision := readMemoryRevision(t, dir)
	marked, after, err := st.ReconcileMemory(nil)
	if err != nil {
		t.Fatalf("ReconcileMemory: %v", err)
	}
	if marked != 0 || after != revision {
		t.Errorf("reconcile not no-op: marked=%d revision=%d/%d", marked, after, revision)
	}
}

// TestStartupHealsDeletedChapterAcrossKinds 覆盖 G4 启动自愈：
// 删除钩子失败后重启，非事件记忆（状态）同样按来源章节标记 chapter_deleted。
func TestStartupHealsDeletedChapterAcrossKinds(t *testing.T) {
	st, dir := newTestStore(t)
	ch1, _, err := st.AppendChapter("第一章", "一章正文。")
	if err != nil {
		t.Fatalf("append ch1: %v", err)
	}
	writeMemoryRetrieveFile(t, dir, "states.json", []novel.StoryState{
		{ID: "state_ghost", Status: novel.StateActive, SubjectID: "c_lin", Category: "状态", Attribute: "位置", Value: "旧城", MemoryMeta: novel.MemoryMeta{
			RecordVersion: 1,
			SourceRefs:    []novel.SourceRef{{ChapterID: "ch_ghost", ChapterRevision: 1, EndOffset: 1, OffsetUnit: "rune"}},
		}},
		{ID: "state_ok", MemoryMeta: sourceMeta(ch1), Status: novel.StateActive, SubjectID: "c_lin", Category: "状态", Attribute: "位置", Value: "渡口"},
	})
	_ = st

	if _, err := New(dir); err != nil {
		t.Fatalf("restart New: %v", err)
	}
	states := readMemoryJSON[novel.StoryState](t, dir, "states.json")
	if !states[0].NeedsReview || states[0].ReviewReason != "chapter_deleted" || states[0].RecordVersion != 2 {
		t.Errorf("ghost state not healed: %+v", states[0])
	}
	if states[0].Status != novel.StateActive {
		t.Errorf("ghost state status changed: %s", states[0].Status)
	}
	if states[1].NeedsReview {
		t.Errorf("valid state wrongly marked: %+v", states[1])
	}
}

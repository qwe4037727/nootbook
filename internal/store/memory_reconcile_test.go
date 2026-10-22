package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"novelagent/internal/novel"
)

// writeEventsJSON 直接写入 memory/events.json，用于构造"钩子失败导致残留"的失效状态。
func writeEventsJSON(t *testing.T, dir string, events []novel.StoryEvent) {
	t.Helper()
	data, err := json.Marshal(events)
	if err != nil {
		t.Fatalf("marshal events: %v", err)
	}
	p := filepath.Join(dir, "memory", "events.json")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir memory: %v", err)
	}
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatalf("write events.json: %v", err)
	}
}

func readEventsJSON(t *testing.T, dir string) []novel.StoryEvent {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "memory", "events.json"))
	if err != nil {
		t.Fatalf("read events.json: %v", err)
	}
	var events []novel.StoryEvent
	if err := json.Unmarshal(data, &events); err != nil {
		t.Fatalf("unmarshal events: %v", err)
	}
	return events
}

func readMemoryRevision(t *testing.T, dir string) int64 {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "memory", "meta.json"))
	if err != nil {
		// 对账 no-op 时不会提交、也不创建 meta.json：缺省视为 MemoryRevision = 0（未提交）。
		if os.IsNotExist(err) {
			return 0
		}
		t.Fatalf("read meta.json: %v", err)
	}
	var meta struct {
		MemoryRevision int64 `json:"memoryRevision"`
	}
	if err := json.Unmarshal(data, &meta); err != nil {
		t.Fatalf("unmarshal meta: %v", err)
	}
	return meta.MemoryRevision
}

// TestStoryOrderReconcileOnStartupHealsDeletedChapter 覆盖 HEAL-01 的场景一：
// 章节被删除且序号回调因故失败时，残留事件在"单纯重启"后经启动期对账被标记为 needsReview。
func TestStoryOrderReconcileOnStartupHealsDeletedChapter(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	chA, _, err := s.AppendChapter("第一章", "内容A")
	if err != nil {
		t.Fatalf("AppendChapter A: %v", err)
	}
	if _, _, err := s.AppendChapter("第二章", "内容B"); err != nil {
		t.Fatalf("AppendChapter B: %v", err)
	}

	// 模拟"章节已删除、但事件仍残留"：引用不存在的 ghost 章节 + 引用现存章节。
	// 章节序号为 1-based（appendChapterLocked: len+1），chA 索引为 1。
	writeEventsJSON(t, dir, []novel.StoryEvent{
		{ID: "evt-1", Status: novel.EventConfirmed, EventType: "other", Summary: "已删章节事件", ChapterID: "ghost-chapter", StoryOrder: 3},
		{ID: "evt-2", Status: novel.EventConfirmed, EventType: "other", Summary: "现存章节事件", ChapterID: chA.ID, StoryOrder: 1},
	})

	// 重启（新建 Store 触发启动期对账）。
	if _, err := New(dir); err != nil {
		t.Fatalf("restart New: %v", err)
	}

	events := readEventsJSON(t, dir)
	if len(events) != 2 {
		t.Fatalf("events len = %d, want 2", len(events))
	}
	for _, e := range events {
		switch e.ID {
		case "evt-1":
			if !e.NeedsReview || e.ReviewReason != "chapter_deleted" || e.StoryOrder != 0 {
				t.Errorf("ghost event not healed: %+v", e)
			}
		case "evt-2":
			if e.NeedsReview || e.StoryOrder != 1 {
				t.Errorf("valid event wrongly changed: %+v", e)
			}
		}
	}
}

// TestStoryOrderReconcileOnAppendHealsDeletedChapter 覆盖 HEAL-01 的场景二：
// 后续新增章节（仅向尾部追加）虽不改变既有章节序号、不通知全局钩子，
// 但必须执行 store 本地兜底对账，收敛此前因钩子失败而残留的失效事件。
func TestStoryOrderReconcileOnAppendHealsDeletedChapter(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, _, err := s.AppendChapter("第一章", "内容A"); err != nil {
		t.Fatalf("AppendChapter A: %v", err)
	}
	if _, _, err := s.AppendChapter("第二章", "内容B"); err != nil {
		t.Fatalf("AppendChapter B: %v", err)
	}

	writeEventsJSON(t, dir, []novel.StoryEvent{
		{ID: "evt-1", Status: novel.EventConfirmed, EventType: "other", Summary: "已删章节事件", ChapterID: "ghost-chapter", StoryOrder: 1},
	})

	// 后续新增章节：必须触发本地兜底对账。
	if _, _, err := s.AppendChapter("第三章", "内容C"); err != nil {
		t.Fatalf("AppendChapter C: %v", err)
	}

	events := readEventsJSON(t, dir)
	if len(events) != 1 {
		t.Fatalf("events len = %d, want 1", len(events))
	}
	e := events[0]
	if !e.NeedsReview || e.ReviewReason != "chapter_deleted" || e.StoryOrder != 0 {
		t.Errorf("ghost event not healed by append reconcile: %+v", e)
	}
}

// TestStoryOrderReconcileNoopDoesNotBumpRevision 验证对账 no-op 语义：
// 事件 StoryOrder 已与章节真源一致时，不提交、不递增 MemoryRevision；
// 仅有真实变化时才提交并递增（与 R8 穷尽表一致，不破坏 TestFunctionalOrderReshuffles）。
func TestStoryOrderReconcileNoopDoesNotBumpRevision(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	chA, _, err := s.AppendChapter("第一章", "内容A")
	if err != nil {
		t.Fatalf("AppendChapter A: %v", err)
	}

	// 无变化：StoryOrder 已等于章节索引 1（首章 index=1）。
	writeEventsJSON(t, dir, []novel.StoryEvent{
		{ID: "evt-1", Status: novel.EventConfirmed, EventType: "other", Summary: "现存章节事件", ChapterID: chA.ID, StoryOrder: 1},
	})
	if _, err := New(dir); err != nil {
		t.Fatalf("restart New: %v", err)
	}
	if rev := readMemoryRevision(t, dir); rev != 0 {
		t.Errorf("no-op reconcile bumped MemoryRevision = %d, want 0", rev)
	}

	// 有变化：StoryOrder 与章节索引不符 → 应提交并递增到 1。
	writeEventsJSON(t, dir, []novel.StoryEvent{
		{ID: "evt-1", Status: novel.EventConfirmed, EventType: "other", Summary: "现存章节事件", ChapterID: chA.ID, StoryOrder: 7},
	})
	if _, err := New(dir); err != nil {
		t.Fatalf("restart New: %v", err)
	}
	if rev := readMemoryRevision(t, dir); rev != 1 {
		t.Errorf("changed reconcile MemoryRevision = %d, want 1", rev)
	}
}

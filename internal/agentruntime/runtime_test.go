package agentruntime

import (
	"context"
	"strings"
	"testing"
	"time"

	"novelagent/internal/novel"
	"novelagent/internal/store"
)

func newTestRuntime(t *testing.T) (*Runtime, *store.Store, string) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.New(dir)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	if _, err := st.ImportChapters("测试书", []novel.Chapter{
		{Title: "第一章", Content: "初始正文。"},
	}); err != nil {
		t.Fatalf("import: %v", err)
	}
	rt, err := New("p_test", st, dir, nil)
	if err != nil {
		t.Fatalf("new runtime: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	rt.Start(ctx)
	t.Cleanup(rt.Stop)
	return rt, st, dir
}

func waitForRuns(t *testing.T, rt *Runtime, want int) []RunRecord {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		runs := rt.RecentRuns(50)
		if len(runs) >= want {
			return runs
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("runs did not reach %d: got %d", want, len(rt.RecentRuns(50)))
	return nil
}

func TestEchoRunsOnChapterSave(t *testing.T) {
	rt, st, _ := newTestRuntime(t)
	project, err := st.LoadProject()
	if err != nil {
		t.Fatalf("load project: %v", err)
	}
	chapter, err := st.LoadChapter(project.Chapters[0].ID)
	if err != nil {
		t.Fatalf("load chapter: %v", err)
	}
	chapter.Content = "保存后的新正文，长度足够触发一次真实变化。"
	if _, _, err := st.SaveChapter(chapter); err != nil {
		t.Fatalf("save chapter: %v", err)
	}

	runs := waitForRuns(t, rt, 1)
	run := runs[0]
	if run.Agent != "echo" || run.Permission != PermissionObserve {
		t.Fatalf("run = %+v, want echo/observe", run)
	}
	if run.Status != StatusDone {
		t.Fatalf("status = %q, want done (%s)", run.Status, run.Error)
	}
	if run.EventType != string(store.ChapterSaved) {
		t.Fatalf("event type = %q", run.EventType)
	}
	if run.Tokens != 0 {
		t.Fatalf("echo tokens = %d, want 0", run.Tokens)
	}
	if run.DurationMs < 0 || run.StartedAt.IsZero() || run.FinishedAt.IsZero() {
		t.Fatalf("timing not recorded: %+v", run)
	}
	if !strings.Contains(run.Summary, "已记录") {
		t.Fatalf("summary = %q", run.Summary)
	}
}

func TestDuplicateEventRunsOnce(t *testing.T) {
	rt, st, _ := newTestRuntime(t)
	project, err := st.LoadProject()
	if err != nil {
		t.Fatalf("load project: %v", err)
	}
	chapter := project.Chapters[0]
	ev := Event{
		ID:        "ev_fixed_duplicate",
		Type:      string(store.ChapterSaved),
		ChapterID: chapter.ID,
		Index:     chapter.Index,
		CharCount: chapter.CharCount,
	}
	if !rt.Publish(ev) {
		t.Fatalf("first publish should be accepted")
	}
	waitForRuns(t, rt, 1)
	before := len(rt.RecentRuns(50))
	if rt.Publish(ev) {
		t.Fatalf("duplicate publish should be rejected")
	}
	time.Sleep(150 * time.Millisecond)
	after := len(rt.RecentRuns(50))
	if after != before {
		t.Fatalf("duplicate event produced extra run: before %d after %d", before, after)
	}
}

type panicAgent struct{}

func (panicAgent) Name() string      { return "panic" }
func (panicAgent) CanRun(Event) bool { return true }
func (panicAgent) Run(context.Context, Event) (Report, error) {
	panic("simulated agent crash")
}

type slowAgent struct{}

func (slowAgent) Name() string      { return "slow" }
func (slowAgent) CanRun(Event) bool { return true }
func (slowAgent) Run(context.Context, Event) (Report, error) {
	time.Sleep(500 * time.Millisecond)
	return Report{Summary: "slow done"}, nil
}

func TestAgentPanicIsIsolated(t *testing.T) {
	rt, st, _ := newTestRuntime(t)
	rt.agents = []Agent{panicAgent{}, &EchoAgent{st: st}}
	project, err := st.LoadProject()
	if err != nil {
		t.Fatalf("load project: %v", err)
	}
	chapter := project.Chapters[0]
	rt.Publish(Event{ID: "ev_panic", Type: string(store.ChapterSaved), ChapterID: chapter.ID, Index: chapter.Index})

	runs := waitForRuns(t, rt, 2)
	var failed, done int
	for _, run := range runs {
		switch run.Status {
		case StatusFailed:
			failed++
			if !strings.Contains(run.Error, "panic") {
				t.Fatalf("failure error = %q", run.Error)
			}
		case StatusDone:
			done++
		}
	}
	if failed != 1 || done != 1 {
		t.Fatalf("runs = %+v, want one failed and one done", runs)
	}
}

func TestSlowAgentDoesNotBlockChapterSave(t *testing.T) {
	rt, st, _ := newTestRuntime(t)
	rt.agents = []Agent{slowAgent{}}
	project, err := st.LoadProject()
	if err != nil {
		t.Fatalf("load project: %v", err)
	}
	chapter, err := st.LoadChapter(project.Chapters[0].ID)
	if err != nil {
		t.Fatalf("load chapter: %v", err)
	}
	chapter.Content = "新的正文，确保触发保存事件。"

	started := time.Now()
	if _, _, err := st.SaveChapter(chapter); err != nil {
		t.Fatalf("save: %v", err)
	}
	elapsed := time.Since(started)
	if elapsed > 200*time.Millisecond {
		t.Fatalf("chapter save blocked by agent for %v", elapsed)
	}
}

func TestRecorderPersistsRuns(t *testing.T) {
	dir := t.TempDir()
	rec, err := NewRecorder(dir)
	if err != nil {
		t.Fatalf("new recorder: %v", err)
	}
	want := RunRecord{ID: "run_1", ProjectID: "p", Agent: "echo", Status: StatusDone, Tokens: 0}
	if err := rec.Append(want); err != nil {
		t.Fatalf("append: %v", err)
	}
	rec2, err := NewRecorder(dir)
	if err != nil {
		t.Fatalf("reopen recorder: %v", err)
	}
	got := rec2.Recent(1)
	if len(got) != 1 || got[0].ID != want.ID || got[0].Status != want.Status {
		t.Fatalf("recent = %+v, want %+v", got, want)
	}
}

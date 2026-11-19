package agentruntime

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"novelagent/internal/store"
)

type failingAgent struct {
	calls atomic.Int32
}

func (*failingAgent) Key() string  { return "failing" }
func (*failingAgent) Name() string { return "失败夹具" }
func (*failingAgent) Triggers() []string {
	return []string{string(store.ChapterSaved)}
}
func (a *failingAgent) Run(context.Context, Event, Input) (Report, error) {
	a.calls.Add(1)
	return Report{}, errors.New("upstream unavailable")
}

func TestCircuitBreakerIsolatesFailingAgent(t *testing.T) {
	rt, st, _ := newTestRuntime(t)
	agent := &failingAgent{}
	rt.Register(agent)
	project, err := st.LoadProject()
	if err != nil {
		t.Fatalf("LoadProject: %v", err)
	}
	chapterID := project.Chapters[0].ID
	for i := 0; i < breakerThreshold; i++ {
		rt.Publish(Event{ID: "ev_fail_" + string(rune('a'+i)), Type: string(store.ChapterSaved), ChapterID: chapterID})
		deadline := time.Now().Add(3 * time.Second)
		for agent.calls.Load() < int32(i+1) && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
	}
	if got := agent.calls.Load(); got != int32(breakerThreshold) {
		t.Fatalf("calls = %d", got)
	}

	rt.Publish(Event{ID: "ev_fail_open", Type: string(store.ChapterSaved), ChapterID: chapterID})
	time.Sleep(150 * time.Millisecond)
	if got := agent.calls.Load(); got != int32(breakerThreshold) {
		t.Fatalf("open circuit still called agent: %d", got)
	}
	if _, _, err := rt.RunAgentOnce(context.Background(), "failing", Event{Type: string(store.ChapterSaved), ChapterID: chapterID}); !errors.Is(err, ErrAgentCircuitOpen) {
		t.Fatalf("RunAgentOnce err = %v", err)
	}

	rt.breakerMu.Lock()
	rt.breaker["failing"].openUntil = time.Now().Add(-time.Second)
	rt.breakerMu.Unlock()
	if _, _, err := rt.RunAgentOnce(context.Background(), "failing", Event{Type: string(store.ChapterSaved), ChapterID: chapterID}); err == nil || errors.Is(err, ErrAgentCircuitOpen) {
		t.Fatalf("half-open probe err = %v", err)
	}
	if got := agent.calls.Load(); got != int32(breakerThreshold+1) {
		t.Fatalf("half-open calls = %d", got)
	}
	if _, _, err := rt.RunAgentOnce(context.Background(), "failing", Event{Type: string(store.ChapterSaved), ChapterID: chapterID}); !errors.Is(err, ErrAgentCircuitOpen) {
		t.Fatalf("circuit should reopen after failed probe, err = %v", err)
	}
}

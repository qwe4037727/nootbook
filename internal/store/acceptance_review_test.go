//go:build acceptance_review

package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"novelagent/internal/novel"
)

// Opt-in acceptance probes assert the specification, including known failures.
// Run with: go test -tags acceptance_review ./internal/store -run TestAcceptanceReview -v
func reviewSeedKnowledge(t *testing.T, st *Store) {
	t.Helper()
	p, err := st.ImportChapters("review book A", []novel.Chapter{{Title: "one", Content: "Alice is injured."}})
	if err != nil {
		t.Fatal(err)
	}
	writeKnowledgeDirect(t, st, novel.Knowledge{
		Characters: []novel.Character{{ID: "c_alice", Name: "Alice", State: "injured"}},
		Summaries:  []novel.ChapterSummary{{ChapterID: p.Chapters[0].ID, Index: 1, Summary: "Alice is injured."}},
	})
}

func TestAcceptanceReviewInterruptedInitialization(t *testing.T) {
	st, dir := newTestStore(t)
	reviewSeedKnowledge(t, st)
	st.memoryMu.Lock()
	err := st.writeMemoryInitStateLocked(MemoryInitState{Status: InitMapping, StartedAt: time.Now().UTC()})
	st.memoryMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	status, err := reopened.InitializeMemory()
	if err != nil {
		t.Fatalf("restart has no mapping worker, but retry is rejected: status=%s err=%v", status, err)
	}
	terminal := waitForInitTerminal(t, reopened)
	if terminal.Status != InitReady {
		t.Fatalf("resumed initialization = %+v", terminal)
	}
}

func TestAcceptanceReviewRetryAfterPayloadWriteFailure(t *testing.T) {
	st, dir := newTestStore(t)
	if _, err := st.LoadMemoryInfo(); err != nil {
		t.Fatal(err)
	}
	blocked := filepath.Join(dir, memoryDirName, "events.json")
	if err := os.Mkdir(blocked, 0o755); err != nil {
		t.Fatal(err)
	}
	st.memoryMu.Lock()
	_, firstErr := st.commitMemoryLocked(CommitInitialize, 0, map[string]any{"events.json": []any{}})
	st.memoryMu.Unlock()
	if firstErr == nil {
		t.Fatal("fault injection did not fail")
	}
	if err := os.Rename(blocked, blocked+".blocker"); err != nil {
		t.Fatal(err)
	}
	st.memoryMu.Lock()
	_, retryErr := st.commitMemoryLocked(CommitInitialize, 0, map[string]any{"events.json": []any{}})
	st.memoryMu.Unlock()
	if retryErr != nil {
		return // Refusing a new write until recovery is a valid outcome.
	}
	reopened, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	info, err := reopened.LoadMemoryInfo()
	if err != nil {
		t.Fatal(err)
	}
	if info.SafeState {
		t.Fatalf("successful retry created an unrecoverable commit fork: %s", info.SafeStateError)
	}
}

func TestAcceptanceReviewMalformedCommitRecords(t *testing.T) {
	for _, tc := range []struct {
		name   string
		target int64
		status string
	}{
		{"revision_jump", 9, CommitCommitted},
		{"unknown_status", 1, "invalid_status"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st, dir := newTestStore(t)
			if _, err := st.LoadMemoryInfo(); err != nil {
				t.Fatal(err)
			}
			st.memoryMu.Lock()
			err := st.writeCommitsLocked([]CommitRecord{{
				CommitID: "commit_invalid", Type: CommitInitialize, Status: tc.status,
				BasedOnMemoryRevision: 0, TargetMemoryRevision: tc.target,
				Payload: map[string]json.RawMessage{}, CreatedAt: time.Now().UTC(),
			}})
			st.memoryMu.Unlock()
			if err != nil {
				t.Fatal(err)
			}
			reopened, err := New(dir)
			if err != nil {
				t.Fatal(err)
			}
			info, err := reopened.LoadMemoryInfo()
			if err != nil {
				t.Fatal(err)
			}
			if !info.SafeState {
				t.Fatalf("invalid commit accepted: target=%d status=%s info=%+v", tc.target, tc.status, info)
			}
		})
	}
}

func TestAcceptanceReviewReplacingBookDoesNotKeepReadyMemory(t *testing.T) {
	st, _ := newTestStore(t)
	reviewSeedKnowledge(t, st)
	if _, err := st.InitializeMemory(); err != nil {
		t.Fatal(err)
	}
	if state := waitForInitTerminal(t, st); state.Status != InitReady {
		t.Fatalf("initial fixture not ready: %+v", state)
	}
	if _, err := st.ImportChapters("review book B", []novel.Chapter{{Title: "one", Content: "Bob is healthy."}}); err != nil {
		t.Fatal(err)
	}
	overview, err := st.LoadMemoryOverview()
	if err != nil {
		t.Fatal(err)
	}
	if overview.InitStatus == InitReady && overview.Counts.States > 0 {
		t.Fatalf("new book is reported ready with old book states: %+v", overview)
	}
}

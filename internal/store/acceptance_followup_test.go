//go:build acceptance_review

package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAcceptanceFollowupPartialPayloadAbort(t *testing.T) {
	st, dir := newTestStore(t)
	st.memoryMu.Lock()
	_, err := st.commitMemoryLocked(CommitInitialize, 0, map[string]any{"events.json": []any{}})
	st.memoryMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	before := string(readFileOrFail(t, dir, "events.json"))
	blocked := filepath.Join(dir, memoryDirName, "states.json")
	if err := os.Mkdir(blocked, 0o755); err != nil {
		t.Fatal(err)
	}
	st.memoryMu.Lock()
	_, commitErr := st.commitMemoryLocked(CommitReconcile, 1, map[string]any{
		"events.json": []map[string]any{{"id": "evt_partial"}},
		"states.json": []any{},
	})
	st.memoryMu.Unlock()
	if commitErr == nil {
		t.Fatal("fault injection did not fail")
	}
	if err := os.Rename(blocked, blocked+".blocker"); err != nil {
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
	if !info.SafeState && info.MemoryRevision == 1 && string(readFileOrFail(t, dir, "events.json")) != before {
		t.Fatal("aborted commit left changed events at old revision; restart reports healthy without rollback or replay")
	}
}

func TestAcceptanceFollowupMetaFailureRetry(t *testing.T) {
	st, dir := newTestStore(t)
	if _, err := st.LoadMemoryInfo(); err != nil {
		t.Fatal(err)
	}
	// Unix permits rename over an open file. Windows also denies rename when
	// another handle holds the destination without delete sharing.
	metaPath := filepath.Join(dir, memoryDirName, "meta.json")
	lockedMeta, err := os.Open(metaPath)
	if err != nil {
		t.Fatal(err)
	}
	st.memoryMu.Lock()
	_, commitErr := st.commitMemoryLocked(CommitInitialize, 0, map[string]any{"events.json": []any{}})
	st.memoryMu.Unlock()
	if err := lockedMeta.Close(); err != nil {
		t.Fatal(err)
	}
	if commitErr == nil {
		t.Skip("platform permits replacing open meta.json; no injected meta failure")
	}
	commits := decodeCommits(t, dir)
	if len(commits) != 1 || commits[0].Status != CommitCommitted {
		t.Fatalf("expected failure after committed marker, got %+v, err=%v", commits, commitErr)
	}
	st.memoryMu.Lock()
	_, retryErr := st.commitMemoryLocked(CommitInitialize, 0, map[string]any{"events.json": []any{}})
	st.memoryMu.Unlock()
	if retryErr != nil {
		return
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
		t.Fatalf("meta failure then successful retry still forks chain: %s", info.SafeStateError)
	}
}

func TestAcceptanceFollowupResetAcquiresMemoryLock(t *testing.T) {
	st, _ := newTestStore(t)
	st.memoryMu.Lock()
	done := make(chan error, 1)
	go func() {
		st.mu.Lock()
		defer st.mu.Unlock()
		done <- st.resetMemoryLocked()
	}()
	select {
	case err := <-done:
		st.memoryMu.Unlock()
		if err != nil {
			t.Fatal(err)
		}
		t.Fatal("reset rewrote memory files while another operation owned memoryMu")
	case <-time.After(200 * time.Millisecond):
		st.memoryMu.Unlock()
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("reset did not finish after releasing memoryMu")
		}
	}
}

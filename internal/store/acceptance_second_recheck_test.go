//go:build acceptance_review

package store

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"novelagent/internal/novel"
)

func TestAcceptanceSecondRecheckRealPartialWrite(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("requires Windows destination replacement denial")
	}
	st, dir := newTestStore(t)
	st.memoryMu.Lock()
	_, err := st.commitMemoryLocked(CommitInitialize, 0, map[string]any{
		"events.json": []map[string]any{{"id": "evt_old"}},
		"states.json": []any{},
	})
	st.memoryMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	locked, err := os.Open(filepath.Join(dir, memoryDirName, "states.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer locked.Close()
	st.memoryMu.Lock()
	_, commitErr := st.commitMemoryLocked(CommitFormal, 1, map[string]any{
		"events.json": []map[string]any{{"id": "evt_partial"}},
		"states.json": []any{},
	})
	st.memoryMu.Unlock()
	if commitErr == nil {
		t.Fatal("expected replacement failure")
	}
	commits := decodeCommits(t, dir)
	if len(commits) != 2 || commits[1].Status != CommitAborted {
		t.Fatalf("did not reach prepared/payload phase: %v; records=%+v", commitErr, commits)
	}
	if err := locked.Close(); err != nil {
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
	events := loadEventsForTest(t, dir)
	if info.SafeState || info.MemoryRevision != 1 || len(events) != 1 || events[0].ID != "evt_old" {
		t.Fatalf("partial write not recovered: info=%+v events=%+v", info, events)
	}
}

func TestAcceptanceSecondRecheckImportDuringMapping(t *testing.T) {
	st, dir := newTestStore(t)
	if _, err := st.ImportChapters("old", []novel.Chapter{{Title: "old", Content: "old book"}}); err != nil {
		t.Fatal(err)
	}
	// A large snapshot keeps the first mutex acquisition long enough for the
	// already-waiting import to run before the mapper's next project read.
	characters := make([]novel.Character, 40000)
	for i := range characters {
		characters[i] = novel.Character{ID: "c_old", Name: "old", State: "old state"}
	}
	writeKnowledgeDirect(t, st, novel.Knowledge{Characters: characters})
	st.memoryMu.Lock()
	err := st.writeMemoryInitStateLocked(MemoryInitState{Status: InitMapping})
	st.memoryMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	st.mu.Lock()
	mapped := make(chan struct{})
	go func() {
		defer close(mapped)
		st.runMemoryMapping()
	}()
	waitForStack := func(fragment string) bool {
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			buf := make([]byte, 1<<20)
			n := runtime.Stack(buf, true)
			if strings.Contains(string(buf[:n]), fragment) {
				return true
			}
			time.Sleep(time.Millisecond)
		}
		return false
	}
	if !waitForStack("(*Store).LoadKnowledge") {
		st.mu.Unlock()
		<-mapped
		t.Fatal("mapper did not reach knowledge acquisition")
	}
	imported := make(chan error, 1)
	go func() {
		_, err := st.ImportChapters("new", []novel.Chapter{{Title: "new", Content: "new book"}})
		imported <- err
	}()
	if !waitForStack("(*Store).ImportChapters") {
		st.mu.Unlock()
		<-mapped
		<-imported
		t.Fatal("import did not reach project acquisition")
	}
	time.Sleep(20 * time.Millisecond)
	st.mu.Unlock()
	if err := <-imported; err != nil {
		t.Fatal(err)
	}
	select {
	case <-mapped:
	case <-time.After(10 * time.Second):
		t.Fatal("mapper blocked after import")
	}
	states := loadStatesForTest(t, dir)
	for _, state := range states {
		if state.SubjectID == "c_old" {
			t.Fatalf("old mapper populated new book with old character state: %+v", state)
		}
	}
	init, err := st.LoadMemoryInitState()
	if err != nil {
		t.Fatal(err)
	}
	if init.Status != InitNotStarted {
		t.Fatalf("old mapper changed new-book initialization state to %q", init.Status)
	}
}

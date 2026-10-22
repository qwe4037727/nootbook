//go:build acceptance_review

package store

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"novelagent/internal/novel"
)

// The child stops between production persistence primitives, so the parent can
// kill it without permitting commitMemoryLocked's error cleanup to execute.
func TestFunctionalCrashChild(t *testing.T) {
	phase := os.Getenv("NOVEL_TEST_CRASH_PHASE")
	if phase == "" {
		t.Skip("subprocess only")
	}
	dir := os.Getenv("NOVEL_TEST_CRASH_DIR")
	st, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	st.memoryMu.Lock()
	if _, err = st.commitMemoryLocked(CommitInitialize, 0, map[string]any{
		"events.json": []map[string]string{{"id": "evt_old"}},
		"states.json": []map[string]string{{"id": "state_old"}},
	}); err != nil {
		t.Fatal(err)
	}
	r := CommitRecord{CommitID: novel.NewID("commit"), Type: CommitReconcile, Status: CommitPrepared,
		BasedOnMemoryRevision: 1, TargetMemoryRevision: 2, CreatedAt: time.Now().UTC(),
		Payload:       map[string]json.RawMessage{"events.json": json.RawMessage(`[{"id":"evt_new"}]`), "states.json": json.RawMessage(`[{"id":"state_new"}]`)},
		RevertPayload: map[string]json.RawMessage{"events.json": readFileOrFail(t, dir, "events.json"), "states.json": readFileOrFail(t, dir, "states.json")},
	}
	rs, err := st.loadCommitsLocked()
	if err != nil {
		t.Fatal(err)
	}
	if phase != "before_prepared" {
		rs = append(rs, r)
		if err := st.writeCommitsLocked(rs); err != nil {
			t.Fatal(err)
		}
	}
	if phase == "partial_payload" {
		if err := st.applyCommitPayloadLocked(CommitRecord{Payload: map[string]json.RawMessage{"events.json": r.Payload["events.json"]}}); err != nil {
			t.Fatal(err)
		}
	}
	if phase == "all_payload" || phase == "committed" || phase == "meta" || phase == "aborted_before_revert" {
		if err := st.applyCommitPayloadLocked(r); err != nil {
			t.Fatal(err)
		}
	}
	if phase == "committed" || phase == "meta" || phase == "aborted_before_revert" {
		rs[len(rs)-1].Status = CommitCommitted
		if phase == "aborted_before_revert" {
			rs[len(rs)-1].Status = CommitAborted
		}
		if err := st.writeCommitsLocked(rs); err != nil {
			t.Fatal(err)
		}
	}
	if phase == "meta" {
		meta, err := st.ensureMemoryMetaLocked()
		if err != nil {
			t.Fatal(err)
		}
		meta.MemoryRevision = 2
		meta.LastCommitID = r.CommitID
		if err := st.writeMemoryMetaLocked(meta); err != nil {
			t.Fatal(err)
		}
	}
	fmt.Println("STAGE_READY " + phase)
	for {
		time.Sleep(time.Minute)
	}
}

func TestFunctionalKilledProcessRecoveryMatrix(t *testing.T) {
	for _, phase := range []string{"before_prepared", "prepared", "partial_payload", "all_payload", "committed", "meta", "aborted_before_revert"} {
		t.Run(phase, func(t *testing.T) {
			dir := t.TempDir()
			binary, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(binary, "-test.run=^TestFunctionalCrashChild$", "-test.v")
			cmd.Env = append(os.Environ(), "NOVEL_TEST_CRASH_PHASE="+phase, "NOVEL_TEST_CRASH_DIR="+dir)
			pipe, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			ready := make(chan bool, 1)
			go func() {
				scanner := bufio.NewScanner(pipe)
				for scanner.Scan() {
					if scanner.Text() == "STAGE_READY "+phase {
						ready <- true
						return
					}
				}
				ready <- false
			}()
			select {
			case ok := <-ready:
				if !ok {
					_ = cmd.Wait()
					t.Fatal("child exited before checkpoint")
				}
			case <-time.After(20 * time.Second):
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
				t.Fatal("checkpoint timeout")
			}
			if err := cmd.Process.Kill(); err != nil {
				t.Fatal(err)
			}
			_ = cmd.Wait()
			want := int64(2)
			marker := "new"
			if phase == "before_prepared" || phase == "aborted_before_revert" {
				want = 1
				marker = "old"
			}
			for restart := 0; restart < 2; restart++ {
				st, err := New(dir)
				if err != nil {
					t.Fatal(err)
				}
				info, err := st.LoadMemoryInfo()
				if err != nil {
					t.Fatal(err)
				}
				if info.SafeState || info.MemoryRevision != want {
					t.Fatalf("restart %d info=%+v want rev %d", restart, info, want)
				}
				for _, file := range []string{"events.json", "states.json"} {
					if !strings.Contains(string(readFileOrFail(t, dir, file)), "_"+marker) {
						t.Fatalf("restart %d %s not converged", restart, file)
					}
				}
			}
		})
	}
}

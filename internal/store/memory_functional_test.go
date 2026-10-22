//go:build acceptance_review

package store

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"novelagent/internal/novel"
)

func functionalSeedMemory(t *testing.T) (*Store, string, novel.Project) {
	t.Helper()
	st, dir := newTestStore(t)
	project, err := st.ImportChapters("test", []novel.Chapter{{Title: "one", Content: "one"}, {Title: "two", Content: "two"}, {Title: "three", Content: "three"}})
	if err != nil {
		t.Fatal(err)
	}
	var summaries []novel.ChapterSummary
	for _, c := range project.Chapters {
		summaries = append(summaries, novel.ChapterSummary{ChapterID: c.ID, Index: c.Index, Summary: c.Title})
	}
	writeKnowledgeDirect(t, st, novel.Knowledge{Summaries: summaries})
	if _, err := st.InitializeMemory(); err != nil {
		t.Fatal(err)
	}
	waitForInitTerminal(t, st)
	return st, dir, project
}

func TestFunctionalOrderReshuffles(t *testing.T) {
	for _, mode := range []string{"prepend", "reverse", "remove_middle"} {
		t.Run(mode, func(t *testing.T) {
			st, dir, p := functionalSeedMemory(t)
			old := chapterIndexMap(p.Chapters)
			orders := map[string]int{}
			for i, c := range p.Chapters {
				switch mode {
				case "prepend":
					orders[c.ID] = i + 2
				case "reverse":
					orders[c.ID] = 3 - i
				case "remove_middle":
					if i != 1 {
						orders[c.ID] = 1 + i/2
					}
				}
			}
			before := loadEventsForTest(t, dir)
			if err := st.handleChapterOrderChanged(old, orders); err != nil {
				t.Fatal(err)
			}
			after := loadEventsForTest(t, dir)
			for i, event := range after {
				if event.StoryOrder != orders[event.ChapterID] {
					t.Errorf("order=%d want=%d", event.StoryOrder, orders[event.ChapterID])
				}
				bump := int64(0)
				if _, ok := orders[event.ChapterID]; !ok {
					bump = 1
				}
				if event.RecordVersion != before[i].RecordVersion+bump {
					t.Errorf("unexpected record revision: %+v", event)
				}
			}
			info, _ := st.LoadMemoryInfo()
			if info.MemoryRevision != 2 {
				t.Fatalf("revision=%d", info.MemoryRevision)
			}
			if err := st.handleChapterOrderChanged(orders, orders); err != nil {
				t.Fatal(err)
			}
			info, _ = st.LoadMemoryInfo()
			if info.MemoryRevision != 2 {
				t.Fatal("no-op reconciliation changed revision")
			}
		})
	}
}

func TestFunctionalFailedDeleteHookSelfHealing(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("requires Windows open-file rename semantics")
	}
	functionalFailedDeleteHookHealing(t, "restart")
}

func TestFunctionalFailedDeleteHookNextMutation(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("requires Windows open-file rename semantics")
	}
	functionalFailedDeleteHookHealing(t, "append")
}

func TestFunctionalFailedDeleteHookNextDelete(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("requires Windows open-file rename semantics")
	}
	functionalFailedDeleteHookHealing(t, "delete")
}

func functionalFailedDeleteHookHealing(t *testing.T, nextMutation string) {
	t.Helper()
	st, dir, p := functionalSeedMemory(t)
	blocked, err := os.Open(filepath.Join(dir, memoryDirName, "events.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer blocked.Close()
	if _, err := st.DeleteChapter(p.Chapters[0].ID); err != nil {
		t.Fatal(err)
	}
	if err := blocked.Close(); err != nil {
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
	if info.SafeState {
		t.Fatalf("recovery unsafe: %+v", info)
	}
	if nextMutation == "append" {
		if _, _, err := reopened.AppendChapter("healing trigger", "test"); err != nil {
			t.Fatal(err)
		}
	}
	if nextMutation == "delete" {
		if _, err := reopened.DeleteChapter(p.Chapters[2].ID); err != nil {
			t.Fatal(err)
		}
	}
	for _, event := range loadEventsForTest(t, dir) {
		if event.ChapterID == p.Chapters[0].ID && (event.StoryOrder != 0 || event.ReviewReason != "chapter_deleted") {
			t.Fatalf("missed delete hook remains (nextMutation=%s): order=%d reason=%q", nextMutation, event.StoryOrder, event.ReviewReason)
		}
	}
}

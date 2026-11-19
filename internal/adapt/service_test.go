package adapt

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"novelagent/internal/novel"
	"novelagent/internal/store"
)

func TestDiffReportsInsertAndDelete(t *testing.T) {
	text, parts := Diff("雨落到青石板上。", "细雨落在青石板上。")
	if !strings.Contains(text, "【删】") || !strings.Contains(text, "【增】") {
		t.Fatalf("diff text = %q", text)
	}
	var inserted, deleted, oldRe, newRe strings.Builder
	for _, part := range parts {
		switch part.Op {
		case "insert":
			inserted.WriteString(part.Text)
			newRe.WriteString(part.Text)
		case "delete":
			deleted.WriteString(part.Text)
			oldRe.WriteString(part.Text)
		default:
			inserted.WriteString(part.Text)
			oldRe.WriteString(part.Text)
			newRe.WriteString(part.Text)
		}
	}
	if !strings.Contains(inserted.String(), "细") {
		t.Errorf("inserted = %q", inserted.String())
	}
	if oldRe.String() != "雨落到青石板上。" || newRe.String() != "细雨落在青石板上。" {
		t.Errorf("diff reconstruction old=%q new=%q deleted=%q", oldRe.String(), newRe.String(), deleted.String())
	}

	same, _ := Diff("一样", "一样")
	if same != "一样" {
		t.Errorf("identical diff = %q", same)
	}
}

func newAdaptFixture(t *testing.T, completion string) (*Service, *store.Store, novel.Project) {
	t.Helper()
	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	project, err := st.ImportChapters("雨夜", []novel.Chapter{
		{Title: "第一章", Content: "林砚推门而入，雨声骤停。"},
		{Title: "第二章", Content: "林砚在城隍庙前停下脚步。"},
	})
	if err != nil {
		t.Fatalf("ImportChapters: %v", err)
	}
	knowledge := novel.NewKnowledge()
	knowledge.Characters = []novel.Character{{
		ID: novel.NewID("c"), Name: "林砚", Role: "捕快", Confidence: 0.9, LastSeen: 2,
	}}
	if _, ok, err := st.CommitAnalysis(project.Revision, 2, knowledge); err != nil || !ok {
		t.Fatalf("CommitAnalysis ok=%v err=%v", ok, err)
	}
	project, err = st.LoadProject()
	if err != nil {
		t.Fatalf("LoadProject: %v", err)
	}
	if _, ok, err := st.FinishAnalysis(project.Revision, knowledge); err != nil || !ok {
		t.Fatalf("FinishAnalysis ok=%v err=%v", ok, err)
	}
	project, err = st.LoadProject()
	if err != nil {
		t.Fatalf("LoadProject: %v", err)
	}

	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"choices":[{"message":{"role":"assistant","content":%q},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":6,"total_tokens":16}}`, completion)
	}))
	t.Cleanup(provider.Close)
	if _, err := st.SaveConfig(store.Config{
		BaseURL: provider.URL + "/v1", Model: "mock-model", ContextChars: 8000,
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	return NewService(st, nil), st, project
}

func TestAdaptModeACreatesDraftWithoutMemory(t *testing.T) {
	svc, st, project := newAdaptFixture(t, "他推门而入，雨声停了。")

	selection := "林砚推门而入，雨声骤停。"
	result, err := svc.Run(context.Background(), Request{
		ChapterID:   project.Chapters[0].ID,
		StartOffset: 0,
		EndOffset:   len([]rune(selection)),
		Instruction: "更克制一些",
		Mode:        "A",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Draft.ID == "" || result.Draft.TargetChapterID != project.Chapters[0].ID {
		t.Errorf("draft = %+v", result.Draft)
	}
	if !strings.Contains(result.FullContent, "他推门而入") {
		t.Errorf("full content = %q", result.FullContent)
	}
	if result.Impact.Mode != "A" || len(result.Impact.ChangedEntities) != 0 {
		t.Errorf("mode A impact = %+v", result.Impact)
	}
	if !result.MemoryUntouched {
		t.Error("memoryUntouched = false")
	}
	if result.Usage.TotalTokens != 16 {
		t.Errorf("usage = %+v", result.Usage)
	}

	events, states, cognitions, threads, plans, err := st.LoadMemoryRecords()
	if err != nil {
		t.Fatalf("LoadMemoryRecords: %v", err)
	}
	if len(events)+len(states)+len(cognitions)+len(threads)+len(plans) != 0 {
		t.Error("mode A wrote memory records")
	}

	runs, err := st.ListAdaptRuns(10)
	if err != nil {
		t.Fatalf("ListAdaptRuns: %v", err)
	}
	if len(runs) != 1 || runs[0].DraftID != result.Draft.ID || runs[0].TotalTokens != 16 {
		t.Errorf("adapt runs = %+v", runs)
	}
	var impact Impact
	if err := json.Unmarshal(runs[0].Impact, &impact); err != nil {
		t.Fatalf("decode impact: %v", err)
	}
	if impact.Mode != "A" {
		t.Errorf("stored impact mode = %q", impact.Mode)
	}
}

func TestAdaptModeBReportsImpact(t *testing.T) {
	svc, _, project := newAdaptFixture(t, "他推门而入，雨声停了。")

	selection := "林砚推门而入，雨声骤停。"
	result, err := svc.Run(context.Background(), Request{
		ChapterID:       project.Chapters[0].ID,
		StartOffset:     0,
		EndOffset:       len([]rune(selection)),
		Instruction:     "删去主角名字，只保留动作",
		InstructionType: "rewrite",
		Mode:            "B",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Impact.Mode != "B" {
		t.Errorf("impact mode = %q", result.Impact.Mode)
	}
	if len(result.Impact.ChangedEntities) != 1 || result.Impact.ChangedEntities[0].Name != "林砚" {
		t.Errorf("changed entities = %+v", result.Impact.ChangedEntities)
	}
	if len(result.Impact.AffectedChapters) != 1 || result.Impact.AffectedChapters[0].Index != 2 {
		t.Errorf("affected chapters = %+v", result.Impact.AffectedChapters)
	}
	if !strings.Contains(result.MemoryModeNote, "不自动提交") {
		t.Errorf("mode note = %q", result.MemoryModeNote)
	}
}

func TestAdaptRejectsBadSelection(t *testing.T) {
	svc, _, project := newAdaptFixture(t, "x")
	_, err := svc.Run(context.Background(), Request{
		ChapterID: project.Chapters[0].ID, StartOffset: 5, EndOffset: 3, Instruction: "改",
	})
	if err == nil {
		t.Fatal("expected invalid selection error")
	}
}

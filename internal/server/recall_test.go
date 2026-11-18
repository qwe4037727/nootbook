package server

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"novelagent/internal/novel"
	"novelagent/internal/store"
)

// R4 长篇召回的 server 侧契约：预览与续写 SSE meta 都要能观测到召回段，
// 且显式 useRecall=false 能完全关闭（A/B 用）。

func newRecallFixture(t *testing.T, chapterCount int) (http.Handler, *store.Store, string) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.New(dir)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	static := fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<!doctype html><title>recall test</title>")},
	}
	chapters := make([]novel.Chapter, 0, chapterCount)
	for i := 1; i <= chapterCount; i++ {
		content := strings.Repeat("雨夜的长街空无一人。", 150)
		if i == 3 {
			content += "\n林砚从旧宅梁下摸出半枚青玉符，符上刻着旧朝年号。"
		}
		chapters = append(chapters, novel.Chapter{
			Title:   fmt.Sprintf("第%d章 雨夜", i),
			Content: content,
		})
	}
	if _, err := st.ImportChapters("召回", chapters); err != nil {
		t.Fatalf("ImportChapters: %v", err)
	}
	if _, err := st.SaveConfig(store.Config{ContextChars: 8000}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	return New(st, static, nil, &fakeAnalyzer{}, nil), st, dir
}

func TestContinuePreviewRecallLongForm(t *testing.T) {
	h, _, dir := newRecallFixture(t, novel.RecallMinChapters+2)
	before := snapshotDataDir(t, dir)

	rec := serveJSON(t, h, http.MethodPost, "/api/continue/preview", map[string]any{
		"direction": "继续追查青玉符的来历。",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("preview = %d: %s", rec.Code, rec.Body.String())
	}
	body := decodeContinuePreview(t, rec.Body.Bytes())
	if !body.RecallEnabled {
		t.Fatalf("recallEnabled = false, want auto-enabled at %d chapters:\n%s",
			novel.RecallMinChapters, body.RecallSection)
	}
	if body.RecallBudget != novel.RecallBudgetChars(8000) {
		t.Errorf("recallBudgetChars = %d, want %d", body.RecallBudget, novel.RecallBudgetChars(8000))
	}
	if !strings.Contains(body.RecallSection, "青玉符") || !strings.Contains(body.RecallSection, "第3章") {
		t.Errorf("recallSection missing planted fact:\n%s", body.RecallSection)
	}
	if body.RecallUsed == 0 || body.RecallUsed > body.RecallBudget {
		t.Errorf("recallUsedChars = %d, budget = %d", body.RecallUsed, body.RecallBudget)
	}
	if body.RecallCount == 0 || len(body.RecallChapters) != body.RecallCount {
		t.Errorf("recallHits = %d, recallChapters = %v", body.RecallCount, body.RecallChapters)
	}
	if body.RecallChapters[0] != 3 {
		t.Errorf("recallChapters[0] = %d, want 3 (highest BM25 score)", body.RecallChapters[0])
	}
	if !strings.HasPrefix(body.RecallSection, "\n【前文相关片段】\n") {
		t.Errorf("recallSection header missing:\n%q", body.RecallSection)
	}
	if len(body.PromptSections) != 8 {
		t.Errorf("sections = %v, want 8", body.PromptSections)
	}
	if body.PromptSections["recall"] != body.RecallUsed {
		t.Errorf("sections.recall = %d, recallUsedChars = %d, want equal", body.PromptSections["recall"], body.RecallUsed)
	}
	sectionSum := 0
	for _, chars := range body.PromptSections {
		sectionSum += chars
	}
	if sectionSum == 0 || sectionSum > body.PromptTotalChars {
		t.Errorf("sum(sections) = %d, promptTotalChars = %d", sectionSum, body.PromptTotalChars)
	}
	if body.PromptOverBudget {
		t.Error("promptOverBudget = true, want false")
	}
	assertSnapshotsEqual(t, before, snapshotDataDir(t, dir))

	// 显式关闭：零注入、零预算，且正文窗口拿回召回让出的空间。
	off := decodeContinuePreview(t, serveJSON(t, h, http.MethodPost, "/api/continue/preview", map[string]any{
		"direction": "继续追查青玉符的来历。",
		"useRecall": false,
	}).Body.Bytes())
	if off.RecallEnabled || off.RecallSection != "" || off.RecallBudget != 0 || off.RecallUsed != 0 {
		t.Errorf("useRecall=false still injected: %+v", off)
	}
	if off.ManuscriptChars <= body.ManuscriptChars {
		t.Errorf("manuscript with recall off = %d, want > recall-on %d", off.ManuscriptChars, body.ManuscriptChars)
	}
	if off.PromptSections["recall"] != 0 {
		t.Errorf("sections.recall = %d, want 0", off.PromptSections["recall"])
	}
}

// TestContinueSSEMetaCarriesRecall 覆盖 R4 端到端接线：召回片段真的进入发给模型的 user prompt。
func TestContinueSSEMetaCarriesRecall(t *testing.T) {
	h, st, _ := newRecallFixture(t, novel.RecallMinChapters+2)
	provider, captured := newMockProvider(t)
	defer provider.Close()
	if _, err := st.SaveConfig(store.Config{
		BaseURL:      provider.URL + "/v1",
		Model:        "mock-model",
		ContextChars: 8000,
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	rec := serveJSON(t, h, http.MethodPost, "/api/continue", map[string]any{
		"direction":   "继续追查青玉符的来历。",
		"targetChars": 300,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/continue = %d: %s", rec.Code, rec.Body.String())
	}
	meta := sseMeta(t, rec.Body.String())
	if meta["recallEnabled"] != true {
		t.Fatalf("meta.recallEnabled = %v, want true", meta["recallEnabled"])
	}
	if sseMetaInt(t, meta, "recallBudgetChars") == 0 || sseMetaInt(t, meta, "recallUsedChars") == 0 {
		t.Errorf("recall budget/used = %v/%v, want >0", meta["recallBudgetChars"], meta["recallUsedChars"])
	}
	chapters, ok := meta["recallChapters"].([]any)
	if !ok || len(chapters) == 0 || chapters[0] != float64(3) {
		t.Errorf("meta.recallChapters = %v, want first 3", meta["recallChapters"])
	}

	select {
	case got := <-captured:
		if !strings.Contains(got.messages[1].Content, "【前文相关片段】") {
			t.Errorf("user prompt missing recall section:\n%s", got.messages[1].Content)
		}
		if !strings.Contains(got.messages[1].Content, "青玉符") {
			t.Error("user prompt recall missing planted fact")
		}
		if !strings.Contains(got.messages[0].Content, "【前文相关片段】") {
			t.Error("system prompt missing recall precedence rule")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("provider was not called")
	}
}

// TestContinueSSEMetaRecallShortNovel 短篇不启用召回，但字段仍要序列化成数组。
func TestContinueSSEMetaRecallShortNovel(t *testing.T) {
	h, st, _ := newRecallFixture(t, novel.RecallMinChapters-2)
	provider, captured := newMockProvider(t)
	defer provider.Close()
	if _, err := st.SaveConfig(store.Config{
		BaseURL:      provider.URL + "/v1",
		Model:        "mock-model",
		ContextChars: 8000,
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	rec := serveJSON(t, h, http.MethodPost, "/api/continue", map[string]any{"targetChars": 200})
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/continue = %d: %s", rec.Code, rec.Body.String())
	}
	meta := sseMeta(t, rec.Body.String())
	if meta["recallEnabled"] != false {
		t.Errorf("recallEnabled = %v, want false below %d chapters", meta["recallEnabled"], novel.RecallMinChapters)
	}
	if sseMetaInt(t, meta, "recallBudgetChars") != 0 {
		t.Errorf("recallBudgetChars = %v, want 0", meta["recallBudgetChars"])
	}
	if chapters, ok := meta["recallChapters"].([]any); !ok || len(chapters) != 0 {
		t.Errorf("recallChapters = %v (%T), want empty array", meta["recallChapters"], meta["recallChapters"])
	}
	select {
	case got := <-captured:
		if strings.Contains(got.messages[1].Content, "【前文相关片段】") {
			t.Error("short novel injected recall section")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("provider was not called")
	}
}

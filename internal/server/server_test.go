package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"novelagent/internal/llm"
	"novelagent/internal/novel"
	"novelagent/internal/store"
)

type fakeAnalyzer struct {
	notified int
	retried  int
}

func (f *fakeAnalyzer) Notify() { f.notified++ }
func (f *fakeAnalyzer) Retry()  { f.retried++ }

func newTestHandler(t *testing.T) (http.Handler, *store.Store, *fakeAnalyzer) {
	t.Helper()
	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	static := fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<!doctype html><title>novel test</title>")},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	analyzer := &fakeAnalyzer{}
	return New(st, static, logger, analyzer), st, analyzer
}

func serveJSON(t *testing.T, h http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("encode request: %v", err)
		}
		reader = bytes.NewReader(data)
	}
	return serveRaw(t, h, method, path, reader)
}

func serveRaw(t *testing.T, h http.Handler, method, path string, body io.Reader) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, body)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestProjectAPIHidesInternalState(t *testing.T) {
	h, _, _ := newTestHandler(t)

	rec := serveJSON(t, h, http.MethodGet, "/api/project", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/project = %d: %s", rec.Code, rec.Body.String())
	}
	var initial publicProject
	if err := json.Unmarshal(rec.Body.Bytes(), &initial); err != nil {
		t.Fatalf("decode default project: %v", err)
	}
	if initial.Title != "未命名小说" {
		t.Errorf("default title = %q", initial.Title)
	}
	assertNoInternalFields(t, rec.Body.Bytes())

	rec = serveJSON(t, h, http.MethodPut, "/api/project", map[string]any{"title": "  雨夜  "})
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT /api/project = %d: %s", rec.Code, rec.Body.String())
	}
	var saved publicProject
	if err := json.Unmarshal(rec.Body.Bytes(), &saved); err != nil {
		t.Fatalf("decode saved project: %v", err)
	}
	if saved.Title != "雨夜" {
		t.Errorf("title = %q", saved.Title)
	}
	assertNoInternalFields(t, rec.Body.Bytes())

	rec = serveRaw(t, h, http.MethodPut, "/api/project", strings.NewReader(`{"title":"x","unknown":true}`))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("PUT with unknown field = %d, want 400", rec.Code)
	}
}

func assertNoInternalFields(t *testing.T, body []byte) {
	t.Helper()
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatalf("decode project: %v", err)
	}
	for _, key := range []string{"manuscript", "characters", "world", "style", "knowledge", "overview", "revision"} {
		if _, ok := raw[key]; ok {
			t.Errorf("public project leaks internal field %q: %s", key, body)
		}
	}
}

func TestImportAndChapterAPI(t *testing.T) {
	h, _, analyzer := newTestHandler(t)

	importText := `书名：雨夜旧事
作者：某某

第1章 初始

雨落到青石板上。

第2章 重瞳

林砚推门而入。
`
	rec := serveJSON(t, h, http.MethodPost, "/api/import", map[string]any{"text": importText})
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/import = %d: %s", rec.Code, rec.Body.String())
	}
	var imported importResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &imported); err != nil {
		t.Fatalf("decode import response: %v", err)
	}
	if imported.Project.Title != "雨夜旧事" {
		t.Errorf("inferred title = %q", imported.Project.Title)
	}
	if imported.ImportedChapters != 2 || len(imported.Project.Chapters) != 2 {
		t.Fatalf("imported chapters = %d / %d", imported.ImportedChapters, len(imported.Project.Chapters))
	}
	if imported.ImportedChars == 0 {
		t.Error("importedChars = 0")
	}
	if imported.Project.Analysis.Status != novel.AnalysisPending || imported.Project.Analysis.TotalCount != 2 {
		t.Errorf("analysis = %+v", imported.Project.Analysis)
	}
	if analyzer.notified != 1 {
		t.Errorf("analysis notifications = %d, want 1", analyzer.notified)
	}
	for _, hidden := range []string{"雨落到青石板上", "林砚推门而入", "overview", "knowledge", "characters", "world", "style"} {
		if strings.Contains(rec.Body.String(), hidden) {
			t.Errorf("import response leaks %q: %s", hidden, rec.Body.String())
		}
	}

	rec = serveJSON(t, h, http.MethodGet, "/api/project", nil)
	if strings.Contains(rec.Body.String(), "雨落到青石板上") {
		t.Errorf("project response leaks chapter content: %s", rec.Body.String())
	}

	id := imported.Project.Chapters[0].ID
	rec = serveJSON(t, h, http.MethodGet, "/api/chapters/"+id, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET chapter = %d: %s", rec.Code, rec.Body.String())
	}
	var chapter novel.Chapter
	if err := json.Unmarshal(rec.Body.Bytes(), &chapter); err != nil {
		t.Fatalf("decode chapter: %v", err)
	}
	if chapter.Content != "雨落到青石板上。" || chapter.Title != "第1章 初始" {
		t.Errorf("chapter = %+v", chapter)
	}

	rec = serveJSON(t, h, http.MethodPut, "/api/chapters/"+id, map[string]any{
		"title":   "初始（改）",
		"content": "改过的内容。",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT chapter = %d: %s", rec.Code, rec.Body.String())
	}
	var updated chapterResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &updated); err != nil {
		t.Fatalf("decode chapter response: %v", err)
	}
	if updated.Chapter.Title != "初始（改）" || updated.Chapter.Content != "改过的内容。" {
		t.Errorf("updated chapter = %+v", updated.Chapter)
	}
	if updated.Project.Analysis.Status != novel.AnalysisPending {
		t.Errorf("analysis after edit = %+v", updated.Project.Analysis)
	}
	if analyzer.notified != 2 {
		t.Errorf("analysis notifications = %d, want 2", analyzer.notified)
	}

	rec = serveJSON(t, h, http.MethodPost, "/api/chapters", map[string]any{
		"title":   "第三章",
		"content": "新的一章。",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("POST chapter = %d: %s", rec.Code, rec.Body.String())
	}
	var appended chapterResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &appended); err != nil {
		t.Fatalf("decode appended chapter: %v", err)
	}
	if appended.Chapter.Index != 3 || appended.Project.Analysis.TotalCount != 3 {
		t.Errorf("appended chapter = %+v / analysis %+v", appended.Chapter, appended.Project.Analysis)
	}

	rec = serveJSON(t, h, http.MethodDelete, "/api/chapters/"+id, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE chapter = %d: %s", rec.Code, rec.Body.String())
	}
	var deleted struct {
		Project publicProject `json:"project"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &deleted); err != nil {
		t.Fatalf("decode delete response: %v", err)
	}
	if len(deleted.Project.Chapters) != 2 || deleted.Project.Chapters[0].Title != "第2章 重瞳" {
		t.Errorf("chapters after delete = %+v", deleted.Project.Chapters)
	}
	if analyzer.notified != 4 {
		t.Errorf("analysis notifications = %d, want 4", analyzer.notified)
	}

	rec = serveJSON(t, h, http.MethodGet, "/api/chapters/bad.id", nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("GET invalid chapter id = %d, want 404", rec.Code)
	}

	rec = serveJSON(t, h, http.MethodPost, "/api/analyze", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/analyze = %d: %s", rec.Code, rec.Body.String())
	}
	if analyzer.retried != 1 {
		t.Errorf("analysis retries = %d, want 1", analyzer.retried)
	}
}

func TestImportRejectsEmptyText(t *testing.T) {
	h, _, analyzer := newTestHandler(t)
	rec := serveJSON(t, h, http.MethodPost, "/api/import", map[string]any{"text": "   "})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST /api/import empty = %d, want 400", rec.Code)
	}
	if analyzer.notified != 0 {
		t.Errorf("notifications = %d, want 0", analyzer.notified)
	}
}

func TestAnalyzeWithoutManager(t *testing.T) {
	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	static := fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("ok")}}
	h := New(st, static, slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	rec := serveJSON(t, h, http.MethodPost, "/api/analyze", nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("POST /api/analyze without manager = %d, want 503", rec.Code)
	}
}

func TestConfigAPIMasksKeyAndRetriesAnalysis(t *testing.T) {
	h, _, analyzer := newTestHandler(t)

	rec := serveJSON(t, h, http.MethodGet, "/api/config", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/config = %d: %s", rec.Code, rec.Body.String())
	}
	var pub publicConfig
	if err := json.Unmarshal(rec.Body.Bytes(), &pub); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	if pub.ContextChars != 12000 || pub.HasAPIKey {
		t.Errorf("default config = %+v", pub)
	}

	rec = serveJSON(t, h, http.MethodPut, "/api/config", map[string]any{
		"baseUrl":      "https://api.example.com/v1/",
		"model":        " test-model ",
		"apiKey":       "sk-abcdefghijklmn",
		"clearApiKey":  false,
		"contextChars": 5000,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT /api/config = %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, "sk-abcdefghijklmn") {
		t.Fatalf("PUT response leaks the API key: %s", body)
	}
	if err := json.Unmarshal([]byte(body), &pub); err != nil {
		t.Fatalf("decode saved config: %v", err)
	}
	if pub.BaseURL != "https://api.example.com/v1" || pub.Model != "test-model" {
		t.Errorf("saved baseUrl/model = %q / %q", pub.BaseURL, pub.Model)
	}
	if !pub.HasAPIKey || pub.APIKeyHint != "sk-…klmn" {
		t.Errorf("API key metadata = has=%v hint=%q", pub.HasAPIKey, pub.APIKeyHint)
	}
	if pub.ContextChars != 5000 {
		t.Errorf("contextChars = %d", pub.ContextChars)
	}
	if analyzer.retried != 1 {
		t.Errorf("analysis retries = %d, want 1", analyzer.retried)
	}

	rec = serveJSON(t, h, http.MethodGet, "/api/config", nil)
	if strings.Contains(rec.Body.String(), "sk-abcdefghijklmn") {
		t.Fatalf("GET response leaks the API key: %s", rec.Body.String())
	}

	rec = serveJSON(t, h, http.MethodPut, "/api/config", map[string]any{
		"baseUrl":      "https://api.example.com/v1",
		"model":        "test-model",
		"apiKey":       "",
		"clearApiKey":  true,
		"contextChars": 100,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("clearing config = %d: %s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &pub); err != nil {
		t.Fatalf("decode cleared config: %v", err)
	}
	if pub.HasAPIKey || pub.APIKeyHint != "" {
		t.Errorf("API key was not cleared: %+v", pub)
	}
	if pub.ContextChars != 1000 {
		t.Errorf("contextChars = %d, want clamped to 1000", pub.ContextChars)
	}

	rec = serveJSON(t, h, http.MethodPut, "/api/config", map[string]any{
		"baseUrl":      "ftp://example.com",
		"model":        "test-model",
		"apiKey":       "",
		"clearApiKey":  false,
		"contextChars": 5000,
	})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("invalid base URL = %d, want 400", rec.Code)
	}
	if analyzer.retried != 2 {
		t.Errorf("analysis retries = %d, want 2", analyzer.retried)
	}
}

func TestConfigPrefersEnvironmentKey(t *testing.T) {
	h, st, _ := newTestHandler(t)
	if _, err := st.SaveConfig(store.Config{
		BaseURL:      "https://api.example.com/v1",
		Model:        "test-model",
		APIKey:       "sk-saved-secret",
		ContextChars: 5000,
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	t.Setenv("OPENAI_API_KEY", "sk-env-secret")

	rec := serveJSON(t, h, http.MethodGet, "/api/config", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/config = %d", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, "sk-env-secret") || strings.Contains(body, "sk-saved-secret") {
		t.Fatalf("config response leaks an API key: %s", body)
	}
	var pub publicConfig
	if err := json.Unmarshal([]byte(body), &pub); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	if !pub.APIKeyFromEnv || !pub.HasAPIKey {
		t.Errorf("API key source = %+v", pub)
	}
	if pub.APIKeyHint != "环境变量 OPENAI_API_KEY" {
		t.Errorf("apiKeyHint = %q", pub.APIKeyHint)
	}
}

type providerCapture struct {
	model    string
	messages []llm.Message
	stream   bool
	err      error
}

func newMockProvider(t *testing.T) (*httptest.Server, chan providerCapture) {
	t.Helper()
	captured := make(chan providerCapture, 1)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model    string        `json:"model"`
			Messages []llm.Message `json:"messages"`
			Stream   bool          `json:"stream"`
		}
		err := json.NewDecoder(r.Body).Decode(&body)
		captured <- providerCapture{model: body.Model, messages: body.Messages, stream: body.Stream, err: err}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"门\"}}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"开了。\"}}]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	return provider, captured
}

func TestContinueUsesAssembledKnowledge(t *testing.T) {
	h, st, _ := newTestHandler(t)

	project, err := st.ImportChapters("雨夜", []novel.Chapter{
		{Title: "初始", Content: "雨落到青石板上。"},
		{Title: "重瞳", Content: "林砚推门而入。"},
	})
	if err != nil {
		t.Fatalf("ImportChapters: %v", err)
	}
	knowledge := novel.NewKnowledge()
	knowledge.Overview = "灵气复苏后的县城。"
	knowledge.Characters = []novel.Character{
		{Name: "林砚", Role: "县衙捕快", State: "左肩受伤", Confidence: 0.9, LastSeen: 2},
	}
	knowledge.World = []novel.WorldEntry{
		{Title: "灵气复苏", Content: "三年前开始。", Confidence: 0.9, LastSeen: 2},
	}
	knowledge.Summaries = []novel.ChapterSummary{
		{ChapterID: project.Chapters[0].ID, Index: 1, Title: "初始", Summary: "雨夜旧案。"},
	}
	if _, ok, err := st.CommitAnalysis(project.Revision, 2, knowledge); err != nil || !ok {
		t.Fatalf("CommitAnalysis = ok %v, err %v", ok, err)
	}
	project, err = st.LoadProject()
	if err != nil {
		t.Fatalf("LoadProject: %v", err)
	}
	if _, ok, err := st.FinishAnalysis(project.Revision, knowledge); err != nil || !ok {
		t.Fatalf("FinishAnalysis = ok %v, err %v", ok, err)
	}

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
		"direction":   "让门外的人开口。",
		"targetChars": 500,
		"temperature": 0.9,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/continue = %d: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Errorf("Content-Type = %q", ct)
	}
	stream := rec.Body.String()
	for _, want := range []string{
		"event: meta",
		`"analysisStatus":"ready"`,
		`"analyzedChapters":2`,
		`"totalChapters":2`,
		`"characters":1`,
		`"worldEntries":1`,
		"event: delta",
		`"text":"门"`,
		`"text":"开了。"`,
		"event: done",
	} {
		if !strings.Contains(stream, want) {
			t.Errorf("SSE stream does not contain %q:\n%s", want, stream)
		}
	}

	select {
	case got := <-captured:
		if got.err != nil {
			t.Fatalf("decode provider request: %v", got.err)
		}
		if got.model != "mock-model" || !got.stream {
			t.Errorf("provider request model/stream = %q / %v", got.model, got.stream)
		}
		if len(got.messages) != 2 || got.messages[0].Role != "system" || got.messages[1].Role != "user" {
			t.Fatalf("provider messages = %+v", got.messages)
		}
		userPrompt := got.messages[1].Content
		for _, want := range []string{"灵气复苏后的县城。", "林砚", "左肩受伤", "灵气复苏", "雨落到青石板上。", "林砚推门而入。", "让门外的人开口。", "约 500 字"} {
			if !strings.Contains(userPrompt, want) {
				t.Errorf("provider user prompt does not contain %q:\n%s", want, userPrompt)
			}
		}
		if !strings.Contains(got.messages[0].Content, "最高事实来源") {
			t.Error("system prompt is missing the latest-text-authority rule")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("provider was not called")
	}

	rec = serveJSON(t, h, http.MethodGet, "/api/project", nil)
	for _, hidden := range []string{"左肩受伤", "灵气复苏后的县城。", "knowledge"} {
		if strings.Contains(rec.Body.String(), hidden) {
			t.Errorf("project response leaks hidden knowledge %q: %s", hidden, rec.Body.String())
		}
	}
}

func TestContinueWorksWhileAnalysisIsPending(t *testing.T) {
	h, st, _ := newTestHandler(t)
	if _, err := st.ImportChapters("雨夜", []novel.Chapter{
		{Title: "初始", Content: "雨落到青石板上。"},
	}); err != nil {
		t.Fatalf("ImportChapters: %v", err)
	}
	provider, captured := newMockProvider(t)
	defer provider.Close()
	if _, err := st.SaveConfig(store.Config{
		BaseURL:      provider.URL + "/v1",
		Model:        "mock-model",
		ContextChars: 8000,
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	rec := serveJSON(t, h, http.MethodPost, "/api/continue", map[string]any{"targetChars": 300})
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/continue = %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"analysisStatus":"pending"`) {
		t.Errorf("meta does not report pending analysis:\n%s", rec.Body.String())
	}
	select {
	case got := <-captured:
		if !strings.Contains(got.messages[1].Content, "雨落到青石板上。") {
			t.Error("continuation prompt is missing the manuscript")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("provider was not called")
	}
}

func TestContinueRequiresChapterContent(t *testing.T) {
	h, _, _ := newTestHandler(t)
	rec := serveJSON(t, h, http.MethodPost, "/api/continue", map[string]any{
		"direction":   "",
		"targetChars": 800,
		"temperature": 0.85,
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST /api/continue = %d: %s", rec.Code, rec.Body.String())
	}
	var payload map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if !strings.Contains(payload["error"], "正文") {
		t.Errorf("error = %q", payload["error"])
	}
}

func TestStaticFilesAndSecurityHeaders(t *testing.T) {
	h, _, _ := newTestHandler(t)
	rec := serveRaw(t, h, http.MethodGet, "/", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET / = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "novel test") {
		t.Errorf("index body = %q", rec.Body.String())
	}
	if got := rec.Header().Get("X-Frame-Options"); got != "DENY" {
		t.Errorf("X-Frame-Options = %q", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q", got)
	}
	if got := rec.Header().Get("Content-Security-Policy"); !strings.Contains(got, "default-src 'self'") {
		t.Errorf("Content-Security-Policy = %q", got)
	}
}

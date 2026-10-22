package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"

	"novelagent/internal/novel"
	"novelagent/internal/store"
)

// newMemoryTestHandler 创建可得知 data 目录的测试环境（knowledge 直写文件、初始化走公共 API）。
func newMemoryTestHandler(t *testing.T) (http.Handler, *store.Store, string) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.New(dir)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	static := fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<!doctype html><title>novel test</title>")},
	}
	return New(st, static, nil, &fakeAnalyzer{}), st, dir
}

// writeKnowledgeFile 直写 knowledge.json（store 每次读盘，无需注入方法）。
func writeKnowledgeFile(t *testing.T, dir string, knowledge novel.Knowledge) {
	t.Helper()
	raw, err := json.Marshal(knowledge)
	if err != nil {
		t.Fatalf("encode knowledge: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "knowledge.json"), raw, 0o600); err != nil {
		t.Fatalf("write knowledge.json: %v", err)
	}
}

// waitInitTerminal 轮询等待初始化进入终态（ready/partial/failed）。
func waitInitTerminal(t *testing.T, st *store.Store) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		state, err := st.LoadMemoryInitState()
		if err != nil {
			t.Fatalf("LoadMemoryInitState: %v", err)
		}
		switch state.Status {
		case "ready", "partial", "failed":
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("initialization did not reach terminal state in time")
}

// TestMemoryOverviewEmpty 覆盖 U5：无 memory/ → GET /api/memory → 全 0 + not_started。
func TestMemoryOverviewEmpty(t *testing.T) {
	h, _, _ := newMemoryTestHandler(t)

	rec := serveJSON(t, h, http.MethodGet, "/api/memory", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body struct {
		WorkID         string `json:"workId"`
		SchemaVersion  int    `json:"schemaVersion"`
		MemoryRevision int64  `json:"memoryRevision"`
		InitStatus     string `json:"initStatus"`
		Counts         struct {
			Events int `json:"events"`
			States int `json:"states"`
		} `json:"counts"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.WorkID != "work_001" || body.SchemaVersion != 1 || body.MemoryRevision != 0 {
		t.Errorf("body = %+v", body)
	}
	if body.InitStatus != "not_started" || body.Counts.Events != 0 || body.Counts.States != 0 {
		t.Errorf("body = %+v", body)
	}
}

// TestMemoryInitializeStateMachine 覆盖 §12.3/§9.3：409 knowledge_not_ready → 202 mapping → 200 ready 幂等。
func TestMemoryInitializeStateMachine(t *testing.T) {
	h, st, dir := newMemoryTestHandler(t)

	// 知识为空 → 409 knowledge_not_ready
	rec := serveJSON(t, h, http.MethodPost, "/api/memory/initialize", nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("empty knowledge status = %d, want 409", rec.Code)
	}
	if code := decodeErrorCode(t, rec.Body.String()); code != "knowledge_not_ready" {
		t.Errorf("code = %q, want knowledge_not_ready", code)
	}

	// 造数：导入章节 + knowledge 直写
	project, err := st.ImportChapters("书", []novel.Chapter{{Title: "一", Content: "一。"}})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	writeKnowledgeFile(t, dir, novel.Knowledge{
		Characters: []novel.Character{{ID: "c_a1", Name: "林越", State: "重伤"}},
		Summaries:  []novel.ChapterSummary{{ChapterID: project.Chapters[0].ID, Index: 1, Summary: "摘要"}},
	})

	// 首次 → 202 mapping
	rec = serveJSON(t, h, http.MethodPost, "/api/memory/initialize", nil)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("first init status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var accepted struct {
		InitStatus string `json:"initStatus"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &accepted); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if accepted.InitStatus != "mapping" {
		t.Errorf("initStatus = %q, want mapping", accepted.InitStatus)
	}

	// 等待终态后重试 → 200 alreadyInitialized（U30）
	waitInitTerminal(t, st)
	rec = serveJSON(t, h, http.MethodPost, "/api/memory/initialize", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("ready retry status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var done struct {
		InitStatus         string `json:"initStatus"`
		AlreadyInitialized bool   `json:"alreadyInitialized"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &done); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if done.InitStatus != "ready" || !done.AlreadyInitialized {
		t.Errorf("done = %+v", done)
	}

	// /status 端点
	rec = serveJSON(t, h, http.MethodGet, "/api/memory/initialize/status", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status endpoint = %d", rec.Code)
	}
	var state struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &state); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if state.Status != "ready" {
		t.Errorf("status = %q, want ready", state.Status)
	}
}

// TestMemoryCharacterEndpoint 覆盖 §12.2：400 前缀校验 / 404 不存在 / 200 聚合视图。
func TestMemoryCharacterEndpoint(t *testing.T) {
	h, st, dir := newMemoryTestHandler(t)

	// 非法前缀 → 400
	rec := serveJSON(t, h, http.MethodGet, "/api/memory/characters/w_b1", nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("w_ prefix status = %d, want 400", rec.Code)
	}

	// 不存在 → 404
	rec = serveJSON(t, h, http.MethodGet, "/api/memory/characters/c_missing", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing character status = %d, want 404", rec.Code)
	}

	// 造数 + 回填
	project, err := st.ImportChapters("书", []novel.Chapter{{Title: "一", Content: "一。"}})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	writeKnowledgeFile(t, dir, novel.Knowledge{
		Characters: []novel.Character{{ID: "c_a1", Name: "林越", State: "重伤"}},
		Summaries:  []novel.ChapterSummary{{ChapterID: project.Chapters[0].ID, Index: 1, Summary: "摘要"}},
	})
	if _, err := st.InitializeMemory(); err != nil {
		t.Fatalf("init: %v", err)
	}
	waitInitTerminal(t, st)

	rec = serveJSON(t, h, http.MethodGet, "/api/memory/characters/c_a1", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("character view status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var view struct {
		Character struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"character"`
		States []struct {
			SubjectID string `json:"subjectId"`
			Status    string `json:"status"`
		} `json:"states"`
		Cognitions []any `json:"cognitions"`
		Events     []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"events"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if view.Character.ID != "c_a1" || view.Character.Name != "林越" {
		t.Errorf("character = %+v", view.Character)
	}
	if len(view.States) != 1 || view.States[0].SubjectID != "c_a1" || view.States[0].Status != "active" {
		t.Errorf("states = %+v", view.States)
	}
	if len(view.Events) != 0 {
		t.Errorf("events = %+v, want empty (backfill skeleton has no participants)", view.Events)
	}
}

// ---- 测试辅助 ----

func decodeErrorCode(t *testing.T, body string) string {
	t.Helper()
	var parsed struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		t.Fatalf("decode error body %q: %v", body, err)
	}
	return parsed.Code
}

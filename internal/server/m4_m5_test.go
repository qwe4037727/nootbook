package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
	"time"

	"novelagent/internal/milestone"
	"novelagent/internal/novel"
	"novelagent/internal/store"
	"novelagent/internal/task"
)

const m4m5ImportText = "第一章 启程\n林越背剑出城。\n第二章 入山\n山雾渐浓。"

func newM4M5Handler(t *testing.T, cfg milestone.Config, tasks *task.Service) (http.Handler, *store.Store) {
	t.Helper()
	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	static := fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<!doctype html>")}}
	h := NewWithMilestone(st, static, nil, &fakeAnalyzer{}, nil, cfg, tasks)
	return h, st
}

func TestImportAppendAndChapterEnhancementRoutes(t *testing.T) {
	h, st := newM4M5Handler(t, milestone.Config{M4Enabled: true}, nil)

	rec := serveJSON(t, h, http.MethodPost, "/api/import", map[string]any{
		"title": "接口书", "text": m4m5ImportText,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("overwrite import = %d: %s", rec.Code, rec.Body.String())
	}
	var imported struct {
		ImportedChapters int `json:"importedChapters"`
		Project          struct {
			Chapters []novel.ChapterMeta `json:"chapters"`
		} `json:"project"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &imported); err != nil || imported.ImportedChapters != 2 {
		t.Fatalf("import response = %s err=%v", rec.Body.String(), err)
	}

	rec = serveJSON(t, h, http.MethodPost, "/api/import", map[string]any{
		"mode": "append", "text": "第三章 夜宿\n篝火熄灭。",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("append import = %d: %s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &imported); err != nil || imported.ImportedChapters != 1 ||
		len(imported.Project.Chapters) != 3 {
		t.Fatalf("append response = %s err=%v", rec.Body.String(), err)
	}
	project, err := st.LoadProject()
	if err != nil || len(project.Chapters) != 3 {
		t.Fatalf("project chapters = %+v err=%v", project, err)
	}
	ids := []string{project.Chapters[2].ID, project.Chapters[0].ID, project.Chapters[1].ID}
	rec = serveJSON(t, h, http.MethodPost, "/api/chapters/reorder", map[string]any{"chapterIds": ids})
	if rec.Code != http.StatusOK {
		t.Fatalf("reorder = %d: %s", rec.Code, rec.Body.String())
	}

	rec = serveJSON(t, h, http.MethodPost, "/api/chapters/merge", map[string]any{
		"chapterIds": []string{ids[1], ids[2]}, "title": "合章",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("merge = %d: %s", rec.Code, rec.Body.String())
	}
	var merged chapterResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &merged); err != nil || merged.Chapter.ID == "" {
		t.Fatalf("merge response = %s err=%v", rec.Body.String(), err)
	}
	rec = serveJSON(t, h, http.MethodPost, "/api/chapters/"+merged.Chapter.ID+"/split", map[string]any{
		"offset": 7, "firstTitle": "上", "secondTitle": "下",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("split = %d: %s", rec.Code, rec.Body.String())
	}

	rec = serveJSON(t, h, http.MethodGet, "/api/chapters/"+merged.Chapter.ID+"/versions", nil)
	if rec.Code != http.StatusOK || !json.Valid(rec.Body.Bytes()) {
		t.Fatalf("versions = %d: %s", rec.Code, rec.Body.String())
	}

	rec = serveJSON(t, h, http.MethodPost, "/api/snapshots", map[string]any{"name": "接口快照"})
	if rec.Code != http.StatusOK {
		t.Fatalf("snapshot = %d: %s", rec.Code, rec.Body.String())
	}
	var snapshot store.BookSnapshot
	if err := json.Unmarshal(rec.Body.Bytes(), &snapshot); err != nil || snapshot.ID == "" {
		t.Fatalf("snapshot response = %s err=%v", rec.Body.String(), err)
	}
	chapter, err := st.LoadChapter(merged.Chapter.ID)
	if err != nil {
		t.Fatalf("LoadChapter: %v", err)
	}
	chapter.Content = "被修改"
	if rec = serveJSON(t, h, http.MethodPut, "/api/chapters/"+chapter.ID, map[string]any{"title": chapter.Title, "content": chapter.Content}); rec.Code != http.StatusOK {
		t.Fatalf("update chapter = %d: %s", rec.Code, rec.Body.String())
	}
	rec = serveJSON(t, h, http.MethodPost, "/api/snapshots/"+snapshot.ID+"/restore", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("restore snapshot = %d: %s", rec.Code, rec.Body.String())
	}
	restored, err := st.LoadChapter(merged.Chapter.ID)
	if err != nil || restored.Content == "被修改" || restored.Content == "" {
		t.Fatalf("restored chapter = %+v err=%v", restored, err)
	}

	rec = serveJSON(t, h, http.MethodGet, "/api/snapshots?limit=5", nil)
	if rec.Code != http.StatusOK || !json.Valid(rec.Body.Bytes()) {
		t.Fatalf("list snapshots = %d: %s", rec.Code, rec.Body.String())
	}
	rec = serveJSON(t, h, http.MethodGet, "/api/snapshots/"+snapshot.ID, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get snapshot = %d: %s", rec.Code, rec.Body.String())
	}
}

func TestM4M5RoutesRespectMilestone(t *testing.T) {
	h, _ := newM4M5Handler(t, milestone.Config{}, nil)
	if rec := serveJSON(t, h, http.MethodGet, "/api/snapshots", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("disabled snapshots = %d", rec.Code)
	}
	if rec := serveJSON(t, h, http.MethodGet, "/api/tasks", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("disabled tasks = %d", rec.Code)
	}
	if rec := serveJSON(t, h, http.MethodGet, "/api/audit/cost", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("disabled audit = %d", rec.Code)
	}
}

func TestTaskCenterAndAuditRoutes(t *testing.T) {
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]any{"role": "assistant", "content": "林越继续向山深处走去。"}}},
			"usage":   map[string]any{"prompt_tokens": 20, "completion_tokens": 10, "total_tokens": 30},
		})
	}))
	t.Cleanup(model.Close)

	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if _, err := st.ImportChapters("任务接口书", []novel.Chapter{{Title: "一", Content: "山路向北。"}}); err != nil {
		t.Fatalf("ImportChapters: %v", err)
	}
	cfg := store.DefaultConfig()
	cfg.BaseURL = model.URL + "/v1"
	cfg.Model = "deepseek-chat"
	if _, err := st.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	service := task.NewService(st, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		service.Stop()
	})
	service.Start(ctx)
	static := fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<!doctype html>")}}
	h := NewWithMilestone(st, static, nil, &fakeAnalyzer{}, nil, milestone.Config{M4Enabled: true, M5Enabled: true}, service)

	rec := serveJSON(t, h, http.MethodPost, "/api/tasks", map[string]any{"description": "续写1章，主角进山", "priority": "high"})
	if rec.Code != http.StatusOK {
		t.Fatalf("create task = %d: %s", rec.Code, rec.Body.String())
	}
	var created novel.Task
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil || created.ID == "" {
		t.Fatalf("task response = %s err=%v", rec.Body.String(), err)
	}
	if rec := serveJSON(t, h, http.MethodPost, "/api/tasks", map[string]any{"kind": "unknown", "description": "x"}); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad kind = %d: %s", rec.Code, rec.Body.String())
	}

	done := false
	for i := 0; i < 200; i++ {
		rec = serveJSON(t, h, http.MethodGet, "/api/tasks/"+created.ID, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("get task = %d: %s", rec.Code, rec.Body.String())
		}
		var current novel.Task
		if err := json.Unmarshal(rec.Body.Bytes(), &current); err != nil {
			t.Fatalf("decode task: %v", err)
		}
		if current.Status == novel.TaskCompleted {
			done = true
			break
		}
		if current.Status.Terminal() {
			t.Fatalf("task failed: %+v", current)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !done {
		t.Fatal("task did not complete")
	}

	rec = serveJSON(t, h, http.MethodGet, "/api/audit/cost", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("cost = %d: %s", rec.Code, rec.Body.String())
	}
	var cost store.CostSummary
	if err := json.Unmarshal(rec.Body.Bytes(), &cost); err != nil || cost.Total.TotalTokens != 30 ||
		cost.TaskCounts[string(novel.TaskCompleted)] != 1 {
		t.Fatalf("cost = %+v err=%v", cost, err)
	}

	if err := st.SaveAgentReport(store.AgentReportRecord{
		ID: "ar_route", AgentKey: "relation", AgentName: "A6", ChapterID: "ch_route", ChapterIndex: 4,
		Severity: "warning", Summary: "关系未登记",
		Findings: []byte(`[{"type":"relation_unknown","severity":"warning","entryId":"c_lin"}]`),
	}); err != nil {
		t.Fatalf("SaveAgentReport: %v", err)
	}
	rec = serveJSON(t, h, http.MethodGet, "/api/audit/reports?entryId=c_lin&chapterIndexMin=1&chapterIndexMax=5", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("audit reports = %d: %s", rec.Code, rec.Body.String())
	}
	var audit struct {
		Reports []store.AgentReportRecord `json:"reports"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &audit); err != nil || len(audit.Reports) != 1 {
		t.Fatalf("audit reports = %s err=%v", rec.Body.String(), err)
	}
}

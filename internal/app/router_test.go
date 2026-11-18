package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
	"time"

	"novelagent/internal/agentruntime"
	"novelagent/internal/project"
)

type mockProject struct {
	Title    string `json:"title"`
	Chapters []struct {
		ID string `json:"id"`
	} `json:"chapters"`
	Analysis struct {
		Status string `json:"status"`
	} `json:"analysis"`
}

func newTestRouter(t *testing.T) (*project.Manager, http.Handler) {
	t.Helper()
	root := t.TempDir()
	manager, err := project.NewManager(root, nil)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	t.Cleanup(manager.Stop)
	static := fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("ok")}}
	return manager, NewRouter(manager, fs.FS(static), nil)
}

func doJSON(t *testing.T, client *http.Client, base, method, path, projectID string, body any) (int, []byte) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, base+path, reader)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if projectID != "" {
		req.Header.Set(ProjectHeader, projectID)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	return resp.StatusCode, data
}

func decodeProject(t *testing.T, data []byte) mockProject {
	t.Helper()
	var project mockProject
	if err := json.Unmarshal(data, &project); err != nil {
		t.Fatalf("decode project %s: %v", data, err)
	}
	return project
}

func TestRouterRoutesProjectsAndRecordsEcho(t *testing.T) {
	manager, handler := newTestRouter(t)
	first, err := manager.Create("作品甲")
	if err != nil {
		t.Fatalf("create first: %v", err)
	}
	second, err := manager.Create("作品乙")
	if err != nil {
		t.Fatalf("create second: %v", err)
	}
	firstRT, err := manager.Runtime(first.ID)
	if err != nil {
		t.Fatalf("first runtime: %v", err)
	}
	secondRT, err := manager.Runtime(second.ID)
	if err != nil {
		t.Fatalf("second runtime: %v", err)
	}
	if _, _, err := firstRT.Store.AppendChapter("第一章", "甲的旧正文。"); err != nil {
		t.Fatalf("append first: %v", err)
	}
	if _, _, err := secondRT.Store.AppendChapter("第一章", "乙的旧正文。"); err != nil {
		t.Fatalf("append second: %v", err)
	}
	if err := manager.Start(context.Background()); err != nil {
		t.Fatalf("start manager: %v", err)
	}

	server := httptest.NewServer(handler)
	defer server.Close()

	status, data := doJSON(t, server.Client(), server.URL, "GET", "/api/project", first.ID, nil)
	if status != http.StatusOK {
		t.Fatalf("GET project status = %d: %s", status, data)
	}
	projectA := decodeProject(t, data)
	if projectA.Title != "作品甲" || len(projectA.Chapters) != 1 {
		t.Fatalf("project A = %+v", projectA)
	}
	status, data = doJSON(t, server.Client(), server.URL, "GET", "/api/project", second.ID, nil)
	if status != http.StatusOK {
		t.Fatalf("GET project B status = %d: %s", status, data)
	}
	projectB := decodeProject(t, data)
	if projectB.Title != "作品乙" || len(projectB.Chapters) != 1 {
		t.Fatalf("project B = %+v", projectB)
	}
	if projectA.Chapters[0].ID == projectB.Chapters[0].ID {
		t.Fatalf("chapter ids crossed projects")
	}

	status, data = doJSON(t, server.Client(), server.URL, "GET", "/api/project", "", nil)
	if status != http.StatusOK {
		t.Fatalf("GET default status = %d: %s", status, data)
	}
	defaultProject := decodeProject(t, data)
	if len(defaultProject.Chapters) != 0 {
		t.Fatalf("default project should be empty, got %d chapters", len(defaultProject.Chapters))
	}

	chapterID := projectA.Chapters[0].ID
	status, data = doJSON(t, server.Client(), server.URL, "PUT", "/api/chapters/"+chapterID, first.ID, map[string]string{
		"title":   "第一章",
		"content": "甲的新正文，保存后应触发 Echo 观察。",
	})
	if status != http.StatusOK {
		t.Fatalf("PUT chapter status = %d: %s", status, data)
	}

	deadline := time.Now().Add(3 * time.Second)
	var runs []agentruntime.RunRecord
	for time.Now().Before(deadline) {
		status, data = doJSON(t, server.Client(), server.URL, "GET", "/api/agents/runs?limit=20", first.ID, nil)
		if status != http.StatusOK {
			t.Fatalf("GET runs status = %d: %s", status, data)
		}
		var payload struct {
			ProjectID string                   `json:"projectId"`
			Runs      []agentruntime.RunRecord `json:"runs"`
		}
		if err := json.Unmarshal(data, &payload); err != nil {
			t.Fatalf("decode runs: %v", err)
		}
		if payload.ProjectID != first.ID {
			t.Fatalf("runs project = %q, want %q", payload.ProjectID, first.ID)
		}
		runs = payload.Runs
		if len(runs) > 0 {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if len(runs) == 0 {
		t.Fatalf("Echo run not recorded after chapter save")
	}
	if runs[0].Status != agentruntime.StatusDone || runs[0].Agent != "echo" {
		t.Fatalf("echo run = %+v", runs[0])
	}

	status, data = doJSON(t, server.Client(), server.URL, "GET", "/api/agents/runs?limit=20", second.ID, nil)
	if status != http.StatusOK {
		t.Fatalf("GET runs B status = %d: %s", status, data)
	}
	var payloadB struct {
		Runs []agentruntime.RunRecord `json:"runs"`
	}
	if err := json.Unmarshal(data, &payloadB); err != nil {
		t.Fatalf("decode runs B: %v", err)
	}
	if len(payloadB.Runs) != 0 {
		t.Fatalf("project B received %d runs from project A", len(payloadB.Runs))
	}
}

func TestRouterProjectCRUD(t *testing.T) {
	_, handler := newTestRouter(t)
	server := httptest.NewServer(handler)
	defer server.Close()

	status, data := doJSON(t, server.Client(), server.URL, "GET", "/api/projects", "", nil)
	if status != http.StatusOK {
		t.Fatalf("list status = %d: %s", status, data)
	}
	var list projectListResponse
	if err := json.Unmarshal(data, &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list.Projects) != 1 || list.DefaultID != project.DefaultID {
		t.Fatalf("initial list = %+v", list)
	}

	status, data = doJSON(t, server.Client(), server.URL, "POST", "/api/projects", "", map[string]string{"name": "新作品"})
	if status != http.StatusOK {
		t.Fatalf("create status = %d: %s", status, data)
	}
	var created project.Info
	if err := json.Unmarshal(data, &created); err != nil {
		t.Fatalf("decode created: %v", err)
	}
	if created.ID == "" || created.Name != "新作品" {
		t.Fatalf("created = %+v", created)
	}

	status, data = doJSON(t, server.Client(), server.URL, "PATCH", "/api/projects/"+created.ID, "", map[string]string{"name": "改名后"})
	if status != http.StatusOK {
		t.Fatalf("rename status = %d: %s", status, data)
	}
	var renamed project.Info
	if err := json.Unmarshal(data, &renamed); err != nil {
		t.Fatalf("decode renamed: %v", err)
	}
	if renamed.Name != "改名后" || renamed.Title != "改名后" {
		t.Fatalf("renamed = %+v", renamed)
	}

	status, data = doJSON(t, server.Client(), server.URL, "DELETE", "/api/projects/"+project.DefaultID, "", nil)
	if status != http.StatusConflict {
		t.Fatalf("delete default status = %d: %s", status, data)
	}
	status, data = doJSON(t, server.Client(), server.URL, "DELETE", "/api/projects/"+created.ID, "", nil)
	if status != http.StatusOK {
		t.Fatalf("delete status = %d: %s", status, data)
	}
	status, data = doJSON(t, server.Client(), server.URL, "GET", "/api/project", created.ID, nil)
	if status != http.StatusNotFound {
		t.Fatalf("deleted project status = %d: %s", status, data)
	}
}

func TestRouterRejectsUnknownProject(t *testing.T) {
	_, handler := newTestRouter(t)
	server := httptest.NewServer(handler)
	defer server.Close()
	status, data := doJSON(t, server.Client(), server.URL, "GET", "/api/project", "p_missing", nil)
	if status != http.StatusNotFound {
		t.Fatalf("status = %d: %s", status, data)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		t.Fatalf("missing error body")
	}
}

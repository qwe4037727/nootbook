package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"novelagent/internal/agentruntime"
)

func TestRouterRunsMilestoneAgentsOnChapterSave(t *testing.T) {
	manager, handler := newTestRouter(t)
	info, err := manager.Create("M1 作品")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	rt, err := manager.Runtime(info.ID)
	if err != nil {
		t.Fatalf("runtime: %v", err)
	}
	if _, _, err := rt.Store.AppendChapter("第一章", ""); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := manager.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()

	project, err := rt.Store.LoadProject()
	if err != nil {
		t.Fatalf("load project: %v", err)
	}
	chapterID := project.Chapters[0].ID
	status, data := doJSON(t, server.Client(), server.URL, "PUT", "/api/chapters/"+chapterID, info.ID, map[string]string{
		"title":   "第一章",
		"content": "林越推门而入，雾城的冷风灌了进来。他似乎听见了什么，却不知道声音从哪里来。",
	})
	if status != http.StatusOK {
		t.Fatalf("PUT status = %d: %s", status, data)
	}

	deadline := time.Now().Add(3 * time.Second)
	seen := map[string]bool{}
	for time.Now().Before(deadline) {
		status, data = doJSON(t, server.Client(), server.URL, "GET", "/api/agents/runs?limit=50", info.ID, nil)
		if status != http.StatusOK {
			t.Fatalf("GET runs status = %d: %s", status, data)
		}
		var payload struct {
			Runs []agentruntime.RunRecord `json:"runs"`
		}
		if err := json.Unmarshal(data, &payload); err != nil {
			t.Fatalf("decode: %v", err)
		}
		for _, run := range payload.Runs {
			seen[run.Agent] = true
		}
		if seen["continuity"] && seen["fuse"] && seen["memory"] {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("milestone agents did not run, seen = %v", seen)
}

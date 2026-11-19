package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"testing/fstest"

	"novelagent/internal/export"
	"novelagent/internal/milestone"
	"novelagent/internal/novel"
	"novelagent/internal/store"
)

func TestExportRoutes(t *testing.T) {
	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if _, err := st.ImportChapters("导出接口书", []novel.Chapter{{Title: "第一章", Content: "林越出发。"}}); err != nil {
		t.Fatalf("ImportChapters: %v", err)
	}
	static := fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<!doctype html>")}}
	h := NewWithMilestone(st, static, nil, &fakeAnalyzer{}, nil, milestone.Config{M7Enabled: true})

	rec := serveJSON(t, h, http.MethodPost, "/api/export", map[string]any{"format": "md"})
	if rec.Code != http.StatusOK {
		t.Fatalf("export = %d: %s", rec.Code, rec.Body.String())
	}
	var record export.Record
	if err := json.Unmarshal(rec.Body.Bytes(), &record); err != nil || record.ID == "" || record.Format != export.FormatMD {
		t.Fatalf("export record = %s err=%v", rec.Body.String(), err)
	}
	if rec := serveJSON(t, h, http.MethodPost, "/api/export", map[string]any{"format": "pdf"}); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad format = %d: %s", rec.Code, rec.Body.String())
	}

	rec = serveJSON(t, h, http.MethodGet, "/api/exports", nil)
	var list struct {
		Exports []export.Record `json:"exports"`
	}
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &list) != nil || len(list.Exports) != 1 {
		t.Fatalf("list = %d: %s", rec.Code, rec.Body.String())
	}

	rec = serveJSON(t, h, http.MethodGet, "/api/exports/"+record.ID+"/download", nil)
	if rec.Code != http.StatusOK || rec.Body.Len() == 0 {
		t.Fatalf("download = %d: %s", rec.Code, rec.Body.String())
	}
	if disposition := rec.Header().Get("Content-Disposition"); disposition == "" || !strings.Contains(disposition, "attachment") {
		t.Fatalf("disposition = %q", disposition)
	}
	if rec := serveJSON(t, h, http.MethodGet, "/api/exports/exp_missing/download", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("missing download = %d", rec.Code)
	}

	off := NewWithMilestone(st, static, nil, &fakeAnalyzer{}, nil, milestone.Config{})
	if rec := serveJSON(t, off, http.MethodGet, "/api/exports", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("disabled export = %d", rec.Code)
	}
}

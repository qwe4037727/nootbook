package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"novelagent/internal/adapt"
	"novelagent/internal/novel"
	"novelagent/internal/store"
	"novelagent/internal/world"
)

func seedKnowledge(t *testing.T, st *store.Store) (novel.Project, novel.Character, novel.WorldEntry) {
	t.Helper()
	project, err := st.ImportChapters("雨夜", []novel.Chapter{
		{Title: "第一章", Content: "林砚走进雨巷，青石板上泛着水光。"},
		{Title: "第二章", Content: "他在城隍庙前停下脚步。"},
	})
	if err != nil {
		t.Fatalf("ImportChapters: %v", err)
	}
	character := novel.Character{
		ID: novel.NewID("c"), Name: "林砚", Role: "捕快",
		Evidence: []string{"林砚走进雨巷"}, Confidence: 0.9, LastSeen: 1,
	}
	setting := novel.WorldEntry{
		ID: novel.NewID("w"), Title: "城隍庙", Category: "地点", Content: "县城北侧古庙。",
		Evidence: []string{"城隍庙前停下脚步"}, Confidence: 0.8, LastSeen: 2,
	}
	knowledge := novel.NewKnowledge()
	knowledge.Characters = []novel.Character{character}
	knowledge.World = []novel.WorldEntry{setting}
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
	return project, character, setting
}

func TestWorldAPIEntriesLocksEvidenceAndSnapshot(t *testing.T) {
	h, st, _ := newTestHandler(t)
	project, character, setting := seedKnowledge(t, st)

	rec := serveJSON(t, h, http.MethodGet, "/api/world/entries", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET entries = %d: %s", rec.Code, rec.Body.String())
	}
	var entries []novel.KBEntry
	if err := json.Unmarshal(rec.Body.Bytes(), &entries); err != nil {
		t.Fatalf("decode entries: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %+v", entries)
	}

	rec = serveJSON(t, h, http.MethodPost, "/api/world/entries", map[string]any{
		"kind": "faction", "name": "六扇门",
		"fields": map[string]string{"leader": "沈大人"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("POST entry = %d: %s", rec.Code, rec.Body.String())
	}
	var created novel.KBEntry
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created entry: %v", err)
	}
	if created.Kind != novel.EntryFaction || created.Fields["leader"] != "沈大人" {
		t.Errorf("created entry = %+v", created)
	}

	rec = serveJSON(t, h, http.MethodPut, "/api/world/entries/"+created.ID, map[string]any{
		"fields": map[string]string{"leader": "陆大人", "region": "京城"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT extended entry = %d: %s", rec.Code, rec.Body.String())
	}
	var updatedExtended novel.KBEntry
	if err := json.Unmarshal(rec.Body.Bytes(), &updatedExtended); err != nil {
		t.Fatalf("decode extended entry: %v", err)
	}
	if updatedExtended.Fields["leader"] != "陆大人" || updatedExtended.Fields["region"] != "京城" {
		t.Errorf("updated extended entry = %+v", updatedExtended)
	}
	if err := st.DeleteKnowledgeEntry(created.ID); err != nil {
		t.Fatalf("cleanup extended entry: %v", err)
	}

	rec = serveJSON(t, h, http.MethodPut, "/api/world/entries/"+character.ID, map[string]any{
		"name":   "林砚",
		"fields": map[string]string{"role": "典史"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT entry = %d: %s", rec.Code, rec.Body.String())
	}
	var updated novel.KBEntry
	if err := json.Unmarshal(rec.Body.Bytes(), &updated); err != nil {
		t.Fatalf("decode entry: %v", err)
	}
	if updated.Fields["role"] != "典史" {
		t.Errorf("updated entry = %+v", updated)
	}

	lock := false
	rec = serveJSON(t, h, http.MethodPost, "/api/world/entries/"+character.ID+"/locks", map[string]any{
		"fields": []string{"role"}, "lock": &lock,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("POST locks = %d: %s", rec.Code, rec.Body.String())
	}
	var lockResp struct {
		LockedFields []string `json:"lockedFields"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &lockResp); err != nil {
		t.Fatalf("decode locks: %v", err)
	}
	if len(lockResp.LockedFields) != 0 {
		t.Errorf("locked fields = %v", lockResp.LockedFields)
	}

	rec = serveJSON(t, h, http.MethodGet, "/api/world/entries/"+character.ID+"/evidence", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET evidence = %d: %s", rec.Code, rec.Body.String())
	}
	var evidence []world.EvidenceItem
	if err := json.Unmarshal(rec.Body.Bytes(), &evidence); err != nil {
		t.Fatalf("decode evidence: %v", err)
	}
	if len(evidence) != 1 || evidence[0].ChapterID != project.Chapters[0].ID {
		t.Errorf("evidence = %+v", evidence)
	}

	rec = serveJSON(t, h, http.MethodGet, "/api/world/snapshot?chapter=1", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET snapshot = %d: %s", rec.Code, rec.Body.String())
	}
	var snap world.Snapshot
	if err := json.Unmarshal(rec.Body.Bytes(), &snap); err != nil {
		t.Fatalf("decode snapshot: %v", err)
	}
	if len(snap.Entries) != 1 || snap.Entries[0].ID != character.ID {
		t.Errorf("snapshot entries = %+v", snap.Entries)
	}

	rec = serveJSON(t, h, http.MethodPost, "/api/world/relations", map[string]any{
		"fromEntryId": character.ID,
		"toEntryId":   setting.ID,
		"type":        "visited",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("POST relation = %d: %s", rec.Code, rec.Body.String())
	}
	var rel novel.Relation
	if err := json.Unmarshal(rec.Body.Bytes(), &rel); err != nil {
		t.Fatalf("decode relation: %v", err)
	}
	if rel.ID == "" {
		t.Error("relation id is empty")
	}

	rec = serveJSON(t, h, http.MethodPost, "/api/world/timeline", map[string]any{
		"chapterId": project.Chapters[0].ID,
		"title":     "林砚入城",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("POST timeline = %d: %s", rec.Code, rec.Body.String())
	}
	rec = serveJSON(t, h, http.MethodGet, "/api/world/timeline", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET timeline = %d: %s", rec.Code, rec.Body.String())
	}
	var events []novel.TimelineEvent
	if err := json.Unmarshal(rec.Body.Bytes(), &events); err != nil {
		t.Fatalf("decode timeline: %v", err)
	}
	if len(events) != 1 || events[0].ChapterIndex != 1 {
		t.Errorf("timeline = %+v", events)
	}

	rec = serveJSON(t, h, http.MethodGet, "/api/world/search?q=城隍", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET search = %d: %s", rec.Code, rec.Body.String())
	}
	var search struct {
		Entries []novel.KBEntry `json:"entries"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &search); err != nil {
		t.Fatalf("decode search: %v", err)
	}
	if len(search.Entries) != 1 || search.Entries[0].ID != setting.ID {
		t.Errorf("search entries = %+v", search.Entries)
	}

	rec = serveJSON(t, h, http.MethodDelete, "/api/world/relations/"+rel.ID, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE relation = %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAdaptRewriteAPI(t *testing.T) {
	h, st, _ := newTestHandler(t)
	project, _, _ := seedKnowledge(t, st)

	selection := "林砚走进雨巷，青石板上泛着水光。"
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"choices":[{"message":{"role":"assistant","content":%q},"finish_reason":"stop"}],"usage":{"prompt_tokens":9,"completion_tokens":5,"total_tokens":14}}`,
			"他走进雨巷，水光映着青石。")
	}))
	defer provider.Close()
	if _, err := st.SaveConfig(store.Config{
		BaseURL: provider.URL + "/v1", Model: "mock-model", ContextChars: 8000,
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	rec := serveJSON(t, h, http.MethodPost, "/api/adapt/rewrite", map[string]any{
		"chapterId":   project.Chapters[0].ID,
		"startOffset": 0,
		"endOffset":   len([]rune(selection)),
		"instruction": "删去主角名字",
		"mode":        "B",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("POST adapt = %d: %s", rec.Code, rec.Body.String())
	}
	var result adapt.Result
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode adapt result: %v", err)
	}
	if result.Draft.ID == "" || result.Diff == "" {
		t.Errorf("result = draft %q diff %q", result.Draft.ID, result.Diff)
	}
	if result.Impact.Mode != "B" || len(result.Impact.ChangedEntities) == 0 {
		t.Errorf("impact = %+v", result.Impact)
	}
	if result.Usage.TotalTokens != 14 {
		t.Errorf("usage = %+v", result.Usage)
	}

	rec = serveJSON(t, h, http.MethodPost, "/api/adapt/diff", map[string]any{
		"original":  "雨落到青石板上。",
		"rewritten": "细雨落在青石板上。",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("POST diff = %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "【删】") || !strings.Contains(rec.Body.String(), "【增】") {
		t.Errorf("diff response = %s", rec.Body.String())
	}

	rec = serveJSON(t, h, http.MethodGet, "/api/adapt/runs", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET runs = %d: %s", rec.Code, rec.Body.String())
	}
	var runs []store.AdaptRunRecord
	if err := json.Unmarshal(rec.Body.Bytes(), &runs); err != nil {
		t.Fatalf("decode runs: %v", err)
	}
	if len(runs) != 1 || runs[0].DraftID != result.Draft.ID {
		t.Errorf("runs = %+v", runs)
	}

	rec = serveJSON(t, h, http.MethodPost, "/api/adapt/rewrite", map[string]any{
		"chapterId":   project.Chapters[0].ID,
		"startOffset": 4,
		"endOffset":   2,
		"instruction": "改",
		"mode":        "A",
	})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("invalid selection = %d, want 400", rec.Code)
	}

	rec = serveJSON(t, h, http.MethodGet, "/api/adapt/runs/bad.id", nil)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("bad run id = %d, want 400", rec.Code)
	}

	if !strings.Contains(rec.Body.String(), "invalid_run_id") {
		t.Errorf("bad run id body = %s", rec.Body.String())
	}
}

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"novelagent/internal/analysis"
	"novelagent/internal/novel"
	"novelagent/internal/store"
)

// newReviewTestHandler 构造带非 nil extractor 的服务实例（D1 契约需先通过 extractor 空检查）。
func newReviewTestHandler(t *testing.T) (http.Handler, *store.Store, string) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.New(dir)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	static := fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<!doctype html><title>novel test</title>")},
	}
	return New(st, static, nil, &fakeAnalyzer{}, analysis.NewExtractLauncher(st, nil)), st, dir
}

func reviewChangeSet(rev int64, ops ...novel.ChangeOp) novel.ChangeSet {
	return novel.ChangeSet{
		ID:              novel.NewID("mcs"),
		Status:          novel.ChangeSetProposed,
		Proposer:        "test",
		BasedOnRevision: rev,
		Ops:             ops,
		Conflicts:       []novel.ConflictItem{},
	}
}

func saveReviewChangeSet(t *testing.T, st *store.Store, cs novel.ChangeSet, cands []novel.EntityCandidate) {
	t.Helper()
	if err := st.SaveChangeSet(cs, cands); err != nil {
		t.Fatalf("SaveChangeSet: %v", err)
	}
}

func reviewAddStateOp(t *testing.T) novel.ChangeOp {
	t.Helper()
	record, err := json.Marshal(novel.StoryState{
		ID: "state_hero",
		MemoryMeta: novel.MemoryMeta{
			WorkID:        novel.InternalWorkID(),
			RecordVersion: 1,
			Origin:        novel.Origin{Type: novel.OriginBackfill},
			LastSource:    novel.LastSourceAgentExtraction,
		},
		Status:    novel.StateActive,
		SubjectID: "c_hero",
		Category:  "physical",
		Attribute: "伤势",
		Value:     "重伤",
	})
	if err != nil {
		t.Fatalf("marshal state: %v", err)
	}
	return novel.ChangeOp{Op: novel.OpAddState, Record: record}
}

func assertMemoryError(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, status, rec.Body.String())
	}
	if code == "" {
		return
	}
	var payload struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode error body: %v (%s)", err, rec.Body.String())
	}
	if payload.Code != code {
		t.Fatalf("code = %q, want %q, body = %s", payload.Code, code, rec.Body.String())
	}
}

// TestMemoryReviewAPIStrictBodyContract 覆盖 D1：所有写接口 4096B 上限（413）、未知字段/尾随 JSON/
// null/数组/残缺 JSON 一律 400，与 §12.3 契约同构；allowEmpty 接口的空体放行到业务层。
func TestMemoryReviewAPIStrictBodyContract(t *testing.T) {
	h, _, _ := newReviewTestHandler(t)
	oversize := `{"pad":"` + strings.Repeat("a", 5000) + `"}`

	cases := []struct {
		name       string
		method     string
		path       string
		body       string
		wantStatus int
		wantCode   string
	}{
		{"accept unknown field", http.MethodPost, "/api/memory/changesets/mcs_probe/accept", `{"force":true}`, http.StatusBadRequest, "invalid_request"},
		{"accept trailing json", http.MethodPost, "/api/memory/changesets/mcs_probe/accept", `{}{}`, http.StatusBadRequest, "invalid_request"},
		{"accept null", http.MethodPost, "/api/memory/changesets/mcs_probe/accept", `null`, http.StatusBadRequest, "invalid_request"},
		{"accept array", http.MethodPost, "/api/memory/changesets/mcs_probe/accept", `[]`, http.StatusBadRequest, "invalid_request"},
		{"accept malformed", http.MethodPost, "/api/memory/changesets/mcs_probe/accept", `{`, http.StatusBadRequest, "invalid_request"},
		{"accept oversize", http.MethodPost, "/api/memory/changesets/mcs_probe/accept", oversize, http.StatusRequestEntityTooLarge, "request_too_large"},
		{"accept empty body allowed", http.MethodPost, "/api/memory/changesets/mcs_probe/accept", ``, http.StatusNotFound, "changeset_not_found"},
		{"accept empty object allowed", http.MethodPost, "/api/memory/changesets/mcs_probe/accept", `{}`, http.StatusNotFound, "changeset_not_found"},
		{"reject trailing json", http.MethodPost, "/api/memory/changesets/mcs_probe/reject", `{"reason":"x"} {}`, http.StatusBadRequest, "invalid_request"},
		{"reject unknown field", http.MethodPost, "/api/memory/changesets/mcs_probe/reject", `{"reason":"x","extra":1}`, http.StatusBadRequest, "invalid_request"},
		{"rebase array", http.MethodPost, "/api/memory/changesets/mcs_probe/rebase", `[]`, http.StatusBadRequest, "invalid_request"},
		{"rebase oversize", http.MethodPost, "/api/memory/changesets/mcs_probe/rebase", oversize, http.StatusRequestEntityTooLarge, "request_too_large"},
		{"reconcile unknown field", http.MethodPost, "/api/memory/reconcile", `{"all":true}`, http.StatusBadRequest, "invalid_request"},
		{"reconcile trailing json", http.MethodPost, "/api/memory/reconcile", `{}{}`, http.StatusBadRequest, "invalid_request"},
		{"reconcile oversize", http.MethodPost, "/api/memory/reconcile", oversize, http.StatusRequestEntityTooLarge, "request_too_large"},
		{"reconcile empty body allowed", http.MethodPost, "/api/memory/reconcile", ``, http.StatusOK, ""},
		{"extract empty body rejected", http.MethodPost, "/api/memory/extract", ``, http.StatusBadRequest, "invalid_request"},
		{"extract unknown field", http.MethodPost, "/api/memory/extract", `{"chapterIds":[],"force":true,"extra":1}`, http.StatusBadRequest, "invalid_request"},
		{"extract trailing json", http.MethodPost, "/api/memory/extract", `{"chapterIds":[]} []`, http.StatusBadRequest, "invalid_request"},
		{"extract oversize", http.MethodPost, "/api/memory/extract", oversize, http.StatusRequestEntityTooLarge, "request_too_large"},
		{"candidate unknown field", http.MethodPost, "/api/memory/candidates/cand_probe/resolve", `{"action":"dismiss","extra":1}`, http.StatusBadRequest, "invalid_request"},
		{"candidate empty body rejected", http.MethodPost, "/api/memory/candidates/cand_probe/resolve", ``, http.StatusBadRequest, "invalid_request"},
		{"candidate null", http.MethodPost, "/api/memory/candidates/cand_probe/resolve", `null`, http.StatusBadRequest, "invalid_request"},
		{"candidate oversize", http.MethodPost, "/api/memory/candidates/cand_probe/resolve", oversize, http.StatusRequestEntityTooLarge, "request_too_large"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := serveRaw(t, h, tc.method, tc.path, strings.NewReader(tc.body))
			assertMemoryError(t, rec, tc.wantStatus, tc.wantCode)
		})
	}
}

// TestMemoryReviewAPIChangesetStatusMapping 覆盖 §6 状态映射：缺参 400/不存在 404/非法内容 422/
// 终态 409/基线过期 409→rebase→accept，以及 accept 幂等重放（I2-A）。
func TestMemoryReviewAPIChangesetStatusMapping(t *testing.T) {
	h, st, _ := newReviewTestHandler(t)

	assertMemoryError(t, serveJSON(t, h, http.MethodGet, "/api/memory/changesets/bad.id", nil), http.StatusBadRequest, "invalid_changeset_id")
	assertMemoryError(t, serveJSON(t, h, http.MethodGet, "/api/memory/changesets/mcs_missing", nil), http.StatusNotFound, "changeset_not_found")
	assertMemoryError(t, serveJSON(t, h, http.MethodPost, "/api/memory/changesets/mcs_missing/accept", nil), http.StatusNotFound, "changeset_not_found")
	assertMemoryError(t, serveJSON(t, h, http.MethodPost, "/api/memory/changesets/mcs_missing/reject", nil), http.StatusNotFound, "changeset_not_found")
	assertMemoryError(t, serveJSON(t, h, http.MethodPost, "/api/memory/changesets/mcs_missing/rebase", nil), http.StatusNotFound, "changeset_not_found")

	bad := reviewChangeSet(0, novel.ChangeOp{Op: novel.OpAddEvent, Record: json.RawMessage(`{"id":"evt_bad"}`)})
	saveReviewChangeSet(t, st, bad, nil)
	assertMemoryError(t, serveJSON(t, h, http.MethodPost, "/api/memory/changesets/"+bad.ID+"/accept", nil), http.StatusUnprocessableEntity, "invalid_changeset")

	good := reviewChangeSet(0, reviewAddStateOp(t))
	saveReviewChangeSet(t, st, good, nil)
	rec := serveJSON(t, h, http.MethodPost, "/api/memory/changesets/"+good.ID+"/accept", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("accept status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var result store.ApplyResult
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode apply result: %v", err)
	}
	if result.Status != "applied" || result.CommitID == "" || result.MemoryRevision != 1 {
		t.Fatalf("apply result = %+v, want applied/commit/rev1", result)
	}

	// 幂等重放：同 commit、revision 不再递增。
	rec = serveJSON(t, h, http.MethodPost, "/api/memory/changesets/"+good.ID+"/accept", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("replay accept status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var replay store.ApplyResult
	if err := json.Unmarshal(rec.Body.Bytes(), &replay); err != nil {
		t.Fatalf("decode replay: %v", err)
	}
	if replay.CommitID != result.CommitID || replay.MemoryRevision != 1 {
		t.Fatalf("replay = %+v, want same commit and rev1", replay)
	}

	// applied 为终态：reject/rebase 409。
	assertMemoryError(t, serveJSON(t, h, http.MethodPost, "/api/memory/changesets/"+good.ID+"/reject", map[string]string{"reason": "x"}), http.StatusConflict, "changeset_terminal")
	assertMemoryError(t, serveJSON(t, h, http.MethodPost, "/api/memory/changesets/"+good.ID+"/rebase", nil), http.StatusConflict, "changeset_terminal")

	// 基线过期 → 409 revision_conflict；rebase 后可提交（零有效 op → applied 且不递增）。
	stale := reviewChangeSet(0)
	saveReviewChangeSet(t, st, stale, nil)
	assertMemoryError(t, serveJSON(t, h, http.MethodPost, "/api/memory/changesets/"+stale.ID+"/accept", nil), http.StatusConflict, "revision_conflict")
	rec = serveJSON(t, h, http.MethodPost, "/api/memory/changesets/"+stale.ID+"/rebase", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("rebase status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var rebased novel.ChangeSet
	if err := json.Unmarshal(rec.Body.Bytes(), &rebased); err != nil {
		t.Fatalf("decode rebase: %v", err)
	}
	if rebased.BasedOnRevision != 1 {
		t.Fatalf("rebased basedOn = %d, want 1", rebased.BasedOnRevision)
	}
	rec = serveJSON(t, h, http.MethodPost, "/api/memory/changesets/"+stale.ID+"/accept", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("accept after rebase = %d, body = %s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode rebased accept: %v", err)
	}
	if result.MemoryRevision != 1 {
		t.Fatalf("zero-op accept revision = %d, want 1", result.MemoryRevision)
	}
}

// TestMemoryReviewAPICandidateStatusMapping 覆盖候选确认的 400/404/409 与成功流转。
func TestMemoryReviewAPICandidateStatusMapping(t *testing.T) {
	h, st, _ := newReviewTestHandler(t)
	cand := novel.EntityCandidate{ID: "cand_hero", Name: "林岚", Reason: "ambiguous", Status: string(novel.CandidatePending)}
	saveReviewChangeSet(t, st, reviewChangeSet(0), []novel.EntityCandidate{cand})

	rec := serveJSON(t, h, http.MethodGet, "/api/memory/candidates?status=pending", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list candidates status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var listed []novel.EntityCandidate
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode candidates: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != "cand_hero" {
		t.Fatalf("candidates = %+v, want cand_hero", listed)
	}

	assertMemoryError(t, serveJSON(t, h, http.MethodPost, "/api/memory/candidates/bad.id/resolve", map[string]string{"action": "dismiss"}), http.StatusBadRequest, "invalid_candidate_id")
	assertMemoryError(t, serveJSON(t, h, http.MethodPost, "/api/memory/candidates/cand_missing/resolve", map[string]string{"action": "dismiss"}), http.StatusNotFound, "candidate_not_found")
	assertMemoryError(t, serveJSON(t, h, http.MethodPost, "/api/memory/candidates/cand_hero/resolve", map[string]string{"action": "approve"}), http.StatusBadRequest, "invalid_action")
	assertMemoryError(t, serveJSON(t, h, http.MethodPost, "/api/memory/candidates/cand_hero/resolve", map[string]string{"action": "merge"}), http.StatusBadRequest, "invalid_resolution")

	rec = serveJSON(t, h, http.MethodPost, "/api/memory/candidates/cand_hero/resolve", map[string]string{"action": "dismiss"})
	if rec.Code != http.StatusOK {
		t.Fatalf("dismiss status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resolved struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resolved); err != nil {
		t.Fatalf("decode resolve: %v", err)
	}
	if resolved.Status != "dismissed" {
		t.Fatalf("resolved status = %q, want dismissed", resolved.Status)
	}
	assertMemoryError(t, serveJSON(t, h, http.MethodPost, "/api/memory/candidates/cand_hero/resolve", map[string]string{"action": "dismiss"}), http.StatusConflict, "candidate_not_pending")
}

// TestMemoryReviewAPIExtractStatusMapping 覆盖提取前置与 scope 映射。
func TestMemoryReviewAPIExtractStatusMapping(t *testing.T) {
	h, st, dir := newReviewTestHandler(t)

	assertMemoryError(t, serveJSON(t, h, http.MethodPost, "/api/memory/extract", map[string]any{}), http.StatusConflict, "knowledge_not_ready")

	writeKnowledgeFile(t, dir, novel.Knowledge{Characters: []novel.Character{{ID: "c_a", Name: "阿"}}})
	assertMemoryError(t, serveJSON(t, h, http.MethodPost, "/api/memory/extract", map[string]any{}), http.StatusConflict, "config_missing")

	if _, err := st.SaveConfig(store.Config{BaseURL: "http://127.0.0.1:9", Model: "test"}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	assertMemoryError(t, serveJSON(t, h, http.MethodPost, "/api/memory/extract", map[string]any{"chapterIds": []string{"bad.id"}}), http.StatusBadRequest, "invalid_chapter_id")

	rec := serveJSON(t, h, http.MethodPost, "/api/memory/extract", map[string]any{})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("extract status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var accepted map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &accepted); err != nil {
		t.Fatalf("decode extract response: %v", err)
	}
	if accepted["extractStatus"] != "done" {
		t.Fatalf("extractStatus = %q, want done", accepted["extractStatus"])
	}
}

// TestMemoryReviewAPIExtractConflictMappings 覆盖 already_running 与 analysis_running。
func TestMemoryReviewAPIExtractConflictMappings(t *testing.T) {
	t.Run("already_running", func(t *testing.T) {
		h, st, dir := newReviewTestHandler(t)
		if _, err := st.SaveConfig(store.Config{BaseURL: "http://127.0.0.1:9", Model: "test"}); err != nil {
			t.Fatalf("SaveConfig: %v", err)
		}
		if _, err := st.ImportChapters("书", []novel.Chapter{{Title: "一", Content: "雨。"}}); err != nil {
			t.Fatalf("ImportChapters: %v", err)
		}
		// 导入会重置知识库，须在导入后直写（store 每次读盘）。
		writeKnowledgeFile(t, dir, novel.Knowledge{Characters: []novel.Character{{ID: "c_a", Name: "阿"}}})
		if status, _, err := st.BeginExtract(nil, false); err != nil || status != store.ExtractRunning {
			t.Fatalf("BeginExtract = (%s, %v), want running", status, err)
		}
		assertMemoryError(t, serveJSON(t, h, http.MethodPost, "/api/memory/extract", map[string]any{}), http.StatusConflict, "already_running")
	})

	t.Run("analysis_running_blocks_accept_and_extract", func(t *testing.T) {
		h, st, dir := newReviewTestHandler(t)
		if _, err := st.SaveConfig(store.Config{BaseURL: "http://127.0.0.1:9", Model: "test"}); err != nil {
			t.Fatalf("SaveConfig: %v", err)
		}
		if _, err := st.ImportChapters("书", []novel.Chapter{{Title: "一", Content: "雨。"}}); err != nil {
			t.Fatalf("ImportChapters: %v", err)
		}
		// 导入会重置知识库；Project.Normalize 在零章节时强制 idle，须先导入章节再置分析中。
		writeKnowledgeFile(t, dir, novel.Knowledge{Characters: []novel.Character{{ID: "c_a", Name: "阿"}}})
		cs := reviewChangeSet(0)
		saveReviewChangeSet(t, st, cs, nil)

		project, err := st.LoadProject()
		if err != nil {
			t.Fatalf("LoadProject: %v", err)
		}
		knowledge, err := st.LoadKnowledge()
		if err != nil {
			t.Fatalf("LoadKnowledge: %v", err)
		}
		advanced, ok, err := st.CommitAnalysis(project.Revision, 0, knowledge)
		if err != nil || !ok {
			t.Fatalf("CommitAnalysis = (%+v, %v, %v)", advanced, ok, err)
		}
		assertMemoryError(t, serveJSON(t, h, http.MethodPost, "/api/memory/changesets/"+cs.ID+"/accept", nil), http.StatusConflict, "analysis_running")
		assertMemoryError(t, serveJSON(t, h, http.MethodPost, "/api/memory/extract", map[string]any{}), http.StatusConflict, "analysis_running")
	})
}

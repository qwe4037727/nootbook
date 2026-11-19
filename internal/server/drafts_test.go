package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"novelagent/internal/novel"
	"novelagent/internal/store"
)

// G6 草稿 API 契约（iter2-详设 §12）：创建/编辑/提取/确认/拒绝；草稿区不触碰正式区，
// 确认时正文与记忆变更一次提交。

func TestDraftAPIContractAndTerminalStates(t *testing.T) {
	h, st, _ := newReviewTestHandler(t)

	rec := serveJSON(t, h, http.MethodPost, "/api/drafts", map[string]string{"title": "第一章", "content": "林越推门而入。"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create draft status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var draft novel.Draft
	if err := json.Unmarshal(rec.Body.Bytes(), &draft); err != nil {
		t.Fatalf("decode draft: %v", err)
	}
	if draft.Status != novel.DraftPending || draft.TargetChapterID == "" {
		t.Fatalf("draft = %+v", draft)
	}
	if project, err := st.LoadProject(); err != nil || len(project.Chapters) != 0 {
		t.Fatalf("draft leaked into project: %+v, err = %v", project, err)
	}

	assertMemoryError(t, serveJSON(t, h, http.MethodPost, "/api/drafts", map[string]string{"content": "  "}), http.StatusUnprocessableEntity, "invalid_draft")
	assertMemoryError(t, serveJSON(t, h, http.MethodPost, "/api/drafts", map[string]string{"content": "x", "unknown": "y"}), http.StatusBadRequest, "invalid_request")
	assertMemoryError(t, serveJSON(t, h, http.MethodGet, "/api/drafts?status=bogus", nil), http.StatusBadRequest, "invalid_status")
	assertMemoryError(t, serveJSON(t, h, http.MethodGet, "/api/drafts/draft_missing", nil), http.StatusNotFound, "draft_not_found")
	assertMemoryError(t, serveJSON(t, h, http.MethodGet, "/api/drafts/ch_x", nil), http.StatusBadRequest, "invalid_draft_id")

	rec = serveJSON(t, h, http.MethodPatch, "/api/drafts/"+draft.ID, map[string]any{"draftRevision": 1, "content": "林越推门而入。风很冷。"})
	if rec.Code != http.StatusOK {
		t.Fatalf("patch draft status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var updated novel.Draft
	if err := json.Unmarshal(rec.Body.Bytes(), &updated); err != nil {
		t.Fatalf("decode updated draft: %v", err)
	}
	if updated.Revision != 2 || updated.Content != "林越推门而入。风很冷。" {
		t.Errorf("updated draft = %+v", updated)
	}
	assertMemoryError(t, serveJSON(t, h, http.MethodPatch, "/api/drafts/"+draft.ID, map[string]any{"draftRevision": 1, "content": "x"}), http.StatusConflict, "draft_conflict")

	// #63 闸门：正文在初始记忆同步后被修改，未显式选择时 409。
	assertMemoryError(t, serveJSON(t, h, http.MethodPost, "/api/drafts/"+draft.ID+"/confirm", map[string]int64{"draftRevision": 2}), http.StatusConflict, "draft_memory_stale")
	rec = serveJSON(t, h, http.MethodPost, "/api/drafts/"+draft.ID+"/confirm", map[string]any{"draftRevision": 2, "memorySync": "body_only"})
	if rec.Code != http.StatusOK {
		t.Fatalf("confirm draft status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var result store.DraftCommitResult
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode confirm result: %v", err)
	}
	if result.MemoryRevision != 1 || result.Chapter.Content != updated.Content || result.MemorySyncState != "body_only" {
		t.Errorf("confirm result = %+v", result)
	}
	if loaded, err := st.LoadChapter(draft.TargetChapterID); err != nil || loaded.Content != updated.Content {
		t.Errorf("chapter = %+v, err = %v", loaded, err)
	}

	// 幂等 confirm 返回同一提交；终态不可编辑/驳回。
	rec = serveJSON(t, h, http.MethodPost, "/api/drafts/"+draft.ID+"/confirm", map[string]int64{"draftRevision": 2})
	if rec.Code != http.StatusOK {
		t.Fatalf("idempotent confirm status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var again store.DraftCommitResult
	if err := json.Unmarshal(rec.Body.Bytes(), &again); err != nil {
		t.Fatalf("decode idempotent result: %v", err)
	}
	if again.CommitID != result.CommitID {
		t.Errorf("idempotent commit = %q, want %q", again.CommitID, result.CommitID)
	}
	assertMemoryError(t, serveJSON(t, h, http.MethodPatch, "/api/drafts/"+draft.ID, map[string]any{"draftRevision": 2, "content": "x"}), http.StatusConflict, "draft_terminal")
	assertMemoryError(t, serveJSON(t, h, http.MethodPost, "/api/drafts/"+draft.ID+"/reject", nil), http.StatusConflict, "draft_terminal")

	rec = serveJSON(t, h, http.MethodGet, "/api/drafts?status=committed", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list committed status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var summaries []novel.DraftSummary
	if err := json.Unmarshal(rec.Body.Bytes(), &summaries); err != nil || len(summaries) != 1 || summaries[0].ID != draft.ID {
		t.Errorf("committed summaries = %+v, err = %v", summaries, err)
	}
}

func TestDraftAPIExtractThenAtomicConfirm(t *testing.T) {
	h, st, dir := newReviewTestHandler(t)
	knowledge := novel.NewKnowledge()
	knowledge.Overview = "灵气复苏后的县城。"
	knowledge.Characters = []novel.Character{{
		ID:         "c_lin",
		Name:       "林越",
		Role:       "捕快",
		Confidence: 0.9,
		LastSeen:   1,
	}}
	writeKnowledgeFile(t, dir, knowledge)

	reply, err := json.Marshal(map[string]any{
		"events": []map[string]any{{
			"summary":                  "林越推门",
			"eventType":                "character_action",
			"participants":             []string{"林越"},
			"participantNamesProvided": true,
			"quote":                    "林越推门而入",
		}},
	})
	if err != nil {
		t.Fatalf("encode reply: %v", err)
	}
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"message": map[string]string{"role": "assistant", "content": string(reply)},
			}},
		})
	}))
	defer provider.Close()
	if _, err := st.SaveConfig(store.Config{BaseURL: provider.URL + "/v1", Model: "mock-model", APIKey: "test-key"}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	rec := serveJSON(t, h, http.MethodPost, "/api/drafts", map[string]string{"title": "第一章", "content": "林越推门而入。"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create draft status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var draft novel.Draft
	if err := json.Unmarshal(rec.Body.Bytes(), &draft); err != nil {
		t.Fatalf("decode draft: %v", err)
	}

	rec = serveJSON(t, h, http.MethodPost, "/api/drafts/"+draft.ID+"/extract", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("extract status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var extracted struct {
		Draft     novel.Draft     `json:"draft"`
		ChangeSet novel.ChangeSet `json:"changeset"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &extracted); err != nil {
		t.Fatalf("decode extract result: %v", err)
	}
	if extracted.Draft.ChangeSetID == "" || extracted.ChangeSet.ID != extracted.Draft.ChangeSetID {
		t.Fatalf("draft/changeset linkage = %+v / %+v", extracted.Draft, extracted.ChangeSet)
	}
	if extracted.ChangeSet.DraftID != draft.ID || extracted.ChangeSet.DraftRevision != 1 {
		t.Errorf("changeset draft binding = %+v", extracted.ChangeSet)
	}
	if len(extracted.ChangeSet.Ops) != 1 || extracted.ChangeSet.Ops[0].Op != novel.OpAddEvent {
		t.Fatalf("changeset ops = %+v", extracted.ChangeSet.Ops)
	}
	if project, err := st.LoadProject(); err != nil || len(project.Chapters) != 0 {
		t.Fatalf("extract leaked chapter into project: %+v, err = %v", project, err)
	}

	// 草稿派生批次不可绕过确认单独采纳。
	assertMemoryError(t, serveJSON(t, h, http.MethodPost, "/api/memory/changesets/"+extracted.ChangeSet.ID+"/accept", nil), http.StatusConflict, "draft_managed_changeset")
	assertMemoryError(t, serveJSON(t, h, http.MethodPost, "/api/memory/changesets/"+extracted.ChangeSet.ID+"/rebase", nil), http.StatusConflict, "draft_managed_changeset")

	rec = serveJSON(t, h, http.MethodPost, "/api/drafts/"+draft.ID+"/confirm", map[string]int64{"draftRevision": 1})
	if rec.Code != http.StatusOK {
		t.Fatalf("confirm status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var result store.DraftCommitResult
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode confirm: %v", err)
	}
	if result.MemoryRevision != 1 || result.ChangeSetID != extracted.ChangeSet.ID || result.CommitID == "" {
		t.Errorf("confirm result = %+v", result)
	}
	events, _, _, _, _, err := st.LoadMemoryRecords()
	if err != nil || len(events) != 1 {
		t.Fatalf("events after confirm = %+v, err = %v", events, err)
	}
	if events[0].Status != novel.EventConfirmed || events[0].ChapterID != draft.TargetChapterID {
		t.Errorf("event after confirm = %+v", events[0])
	}
	if project, err := st.LoadProject(); err != nil || len(project.Chapters) != 1 || project.Chapters[0].ID != draft.TargetChapterID {
		t.Errorf("project after confirm = %+v, err = %v", project, err)
	}
	// 已 applied 的草稿批次再 accept → 幂等返回同一提交（I2-A 契约）。
	rec = serveJSON(t, h, http.MethodPost, "/api/memory/changesets/"+extracted.ChangeSet.ID+"/accept", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("idempotent accept status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var applied struct {
		Status         string `json:"status"`
		CommitID       string `json:"commitId"`
		MemoryRevision int64  `json:"memoryRevision"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &applied); err != nil {
		t.Fatalf("decode accept result: %v", err)
	}
	if applied.Status != string(novel.ChangeSetApplied) || applied.CommitID != result.CommitID || applied.MemoryRevision != 1 {
		t.Errorf("idempotent accept = %+v", applied)
	}

	// 编辑已确认草稿 → 终态拒绝。
	assertMemoryError(t, serveJSON(t, h, http.MethodPatch, "/api/drafts/"+draft.ID, map[string]any{"draftRevision": 1, "content": "x"}), http.StatusConflict, "draft_terminal")
}

func TestDraftAPIReextractThenConfirm(t *testing.T) {
	h, st, dir := newReviewTestHandler(t)
	knowledge := novel.NewKnowledge()
	knowledge.Overview = "灵气复苏后的县城。"
	knowledge.Characters = []novel.Character{{ID: "c_lin", Name: "林越", Role: "捕快", Confidence: 0.9, LastSeen: 1}}
	writeKnowledgeFile(t, dir, knowledge)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"message": map[string]string{"role": "assistant", "content": `{"events":[{"summary":"林越推门","eventType":"character_action","participants":["林越"],"participantNamesProvided":true,"quote":"林越推门而入"}]}`},
			}},
		})
	}))
	defer provider.Close()
	if _, err := st.SaveConfig(store.Config{BaseURL: provider.URL + "/v1", Model: "mock-model", APIKey: "test-key"}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	rec := serveJSON(t, h, http.MethodPost, "/api/drafts", map[string]string{"content": "林越推门而入。"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create draft status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var draft novel.Draft
	if err := json.Unmarshal(rec.Body.Bytes(), &draft); err != nil {
		t.Fatalf("decode draft: %v", err)
	}
	rec = serveJSON(t, h, http.MethodPost, "/api/drafts/"+draft.ID+"/extract", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("extract status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var first struct {
		ChangeSet novel.ChangeSet `json:"changeset"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &first); err != nil {
		t.Fatalf("decode extract: %v", err)
	}

	rec = serveJSON(t, h, http.MethodPatch, "/api/drafts/"+draft.ID, map[string]any{"draftRevision": 1, "content": "林越推门而入。风很冷。"})
	if rec.Code != http.StatusOK {
		t.Fatalf("patch status = %d, body = %s", rec.Code, rec.Body.String())
	}
	rec = serveJSON(t, h, http.MethodPost, "/api/drafts/"+draft.ID+"/confirm", map[string]any{"draftRevision": 2, "memorySync": "reextract"})
	if rec.Code != http.StatusOK {
		t.Fatalf("reextract confirm status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var result store.DraftCommitResult
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode confirm: %v", err)
	}
	if result.MemorySyncState != "synced" || result.MemoryRevision != 1 {
		t.Errorf("confirm result = %+v", result)
	}
	if result.ChangeSetID == "" || result.ChangeSetID == first.ChangeSet.ID {
		t.Errorf("reextract changeset = %q, want new after %q", result.ChangeSetID, first.ChangeSet.ID)
	}
	if events, _, _, _, _, err := st.LoadMemoryRecords(); err != nil || len(events) != 1 {
		t.Errorf("events after reextract confirm = %+v, err = %v", events, err)
	}
	if loaded, err := st.LoadChapter(draft.TargetChapterID); err != nil || loaded.Content != "林越推门而入。风很冷。" {
		t.Errorf("chapter = %+v, err = %v", loaded.Content, err)
	}
}

func TestDraftAPIExtractRequiresEnabledService(t *testing.T) {
	h, _, _ := newMemoryTestHandler(t)
	rec := serveJSON(t, h, http.MethodPost, "/api/drafts", map[string]string{"content": "林越推门而入。"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create draft status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var draft novel.Draft
	if err := json.Unmarshal(rec.Body.Bytes(), &draft); err != nil {
		t.Fatalf("decode draft: %v", err)
	}
	if rec := serveJSON(t, h, http.MethodPost, "/api/drafts/"+draft.ID+"/extract", nil); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("extract without service status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestDraftAPIExtractConflictAfterEdit(t *testing.T) {
	h, st, dir := newReviewTestHandler(t)
	knowledge := novel.NewKnowledge()
	knowledge.Overview = "灵气复苏后的县城。"
	knowledge.Characters = []novel.Character{{ID: "c_lin", Name: "林越", Role: "捕快", Confidence: 0.9, LastSeen: 1}}
	writeKnowledgeFile(t, dir, knowledge)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"message": map[string]string{"role": "assistant", "content": `{"events":[{"summary":"林越推门","eventType":"character_action","participants":["林越"],"participantNamesProvided":true,"quote":"林越推门而入"}]}`},
			}},
		})
	}))
	defer provider.Close()
	if _, err := st.SaveConfig(store.Config{BaseURL: provider.URL + "/v1", Model: "mock-model", APIKey: "test-key"}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	rec := serveJSON(t, h, http.MethodPost, "/api/drafts", map[string]string{"content": "林越推门而入。"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create draft status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var draft novel.Draft
	if err := json.Unmarshal(rec.Body.Bytes(), &draft); err != nil {
		t.Fatalf("decode draft: %v", err)
	}
	rec = serveJSON(t, h, http.MethodPost, "/api/drafts/"+draft.ID+"/extract", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("extract status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var extracted struct {
		ChangeSet novel.ChangeSet `json:"changeset"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &extracted); err != nil {
		t.Fatalf("decode extract: %v", err)
	}

	rec = serveJSON(t, h, http.MethodPatch, "/api/drafts/"+draft.ID, map[string]any{"draftRevision": 1, "content": "林越推门而入。风很冷。"})
	if rec.Code != http.StatusOK {
		t.Fatalf("patch draft status = %d, body = %s", rec.Code, rec.Body.String())
	}
	assertMemoryError(t, serveJSON(t, h, http.MethodPost, "/api/memory/changesets/"+extracted.ChangeSet.ID+"/accept", nil), http.StatusConflict, "changeset_terminal")

	// 编辑后旧批次失效：必须显式选择“仅提交正文”，不再静默只提交正文。
	assertMemoryError(t, serveJSON(t, h, http.MethodPost, "/api/drafts/"+draft.ID+"/confirm", map[string]int64{"draftRevision": 2}), http.StatusConflict, "draft_memory_stale")
	rec = serveJSON(t, h, http.MethodPost, "/api/drafts/"+draft.ID+"/confirm", map[string]any{"draftRevision": 2, "memorySync": "body_only"})
	if rec.Code != http.StatusOK {
		t.Fatalf("confirm after edit status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var result store.DraftCommitResult
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode confirm: %v", err)
	}
	if result.ChangeSetID != "" {
		t.Errorf("confirm kept stale changeset %q", result.ChangeSetID)
	}
	if result.MemorySyncState != "body_only" {
		t.Errorf("memory sync state = %q, want body_only", result.MemorySyncState)
	}
	if events, _, _, _, _, err := st.LoadMemoryRecords(); err != nil || len(events) != 0 {
		t.Errorf("stale extraction applied: %+v, err = %v", events, err)
	}
}

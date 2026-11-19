package server

import (
	"errors"
	"net/http"

	"novelagent/internal/novel"
	"novelagent/internal/store"
)

// 正文草稿 API（G6，iter2-详设 §4/§8.2）。
// 草稿区操作不改变 memoryRevision；confirm 由 store 在一次提交中同时落正文与记忆变更。

const draftBodyLimit = 16 << 20

func (s *Server) registerDraftRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/drafts", s.handlePostDraft)
	mux.HandleFunc("GET /api/drafts", s.handleGetDrafts)
	mux.HandleFunc("GET /api/drafts/{id}", s.handleGetDraft)
	mux.HandleFunc("PATCH /api/drafts/{id}", s.handlePatchDraft)
	mux.HandleFunc("POST /api/drafts/{id}/extract", s.handlePostDraftExtract)
	mux.HandleFunc("POST /api/drafts/{id}/confirm", s.handlePostDraftConfirm)
	mux.HandleFunc("POST /api/drafts/{id}/reject", s.handlePostDraftReject)
}

func writeDraftDecodeError(w http.ResponseWriter, err error, message string) {
	if errors.Is(err, errRequestBodyTooLarge) {
		writeMemoryError(w, http.StatusRequestEntityTooLarge, "request_too_large", "请求体不能超过 16 MB")
		return
	}
	writeMemoryError(w, http.StatusBadRequest, "invalid_request", message)
}

func (s *Server) handlePostDraft(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ChapterID string `json:"chapterId"`
		Title     string `json:"title"`
		Content   string `json:"content"`
	}
	if err := decodeStrictJSON(w, r, &req, draftBodyLimit, false); err != nil {
		writeDraftDecodeError(w, err, "请求体必须为单个 JSON 对象（chapterId/title/content）")
		return
	}
	draft, err := s.store.CreateDraft(store.DraftCreateParams{
		ChapterID: req.ChapterID,
		Title:     req.Title,
		Content:   req.Content,
	})
	switch {
	case err == nil:
		writeJSON(w, http.StatusCreated, draft)
	case errors.Is(err, store.ErrDraftInvalid):
		writeMemoryError(w, http.StatusUnprocessableEntity, "invalid_draft", err.Error())
	case errors.Is(err, store.ErrChapterNotFound):
		writeMemoryError(w, http.StatusNotFound, "chapter_not_found", "目标章节不存在")
	case errors.Is(err, store.ErrMemorySafeState):
		writeMemoryError(w, http.StatusInternalServerError, "memory_safe_state", "记忆子系统处于安全态：拒绝写入")
	default:
		s.log.Error("create draft", "err", err)
		writeError(w, http.StatusInternalServerError, "创建草稿失败")
	}
}

func (s *Server) handleGetDrafts(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	switch status {
	case "", string(novel.DraftPending), string(novel.DraftCommitted), string(novel.DraftRejected):
	default:
		writeMemoryError(w, http.StatusBadRequest, "invalid_status", "status 仅支持 pending_confirmation/committed/rejected")
		return
	}
	summaries, err := s.store.LoadDrafts(status)
	if err != nil {
		s.log.Error("load drafts", "err", err)
		writeError(w, http.StatusInternalServerError, "读取草稿列表失败")
		return
	}
	writeJSON(w, http.StatusOK, summaries)
}

func (s *Server) handleGetDraft(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validMemoryID(id, "draft_") {
		writeMemoryError(w, http.StatusBadRequest, "invalid_draft_id", "草稿 ID 必须为 draft_ 前缀")
		return
	}
	draft, err := s.store.LoadDraft(id)
	if errors.Is(err, store.ErrDraftNotFound) {
		writeMemoryError(w, http.StatusNotFound, "draft_not_found", "草稿不存在")
		return
	}
	if err != nil {
		s.log.Error("load draft", "err", err)
		writeError(w, http.StatusInternalServerError, "读取草稿失败")
		return
	}
	writeJSON(w, http.StatusOK, draft)
}

func (s *Server) handlePatchDraft(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validMemoryID(id, "draft_") {
		writeMemoryError(w, http.StatusBadRequest, "invalid_draft_id", "草稿 ID 必须为 draft_ 前缀")
		return
	}
	var req struct {
		DraftRevision int64  `json:"draftRevision"`
		Title         string `json:"title"`
		Content       string `json:"content"`
	}
	if err := decodeStrictJSON(w, r, &req, draftBodyLimit, false); err != nil {
		writeDraftDecodeError(w, err, "请求体必须为单个 JSON 对象（draftRevision/title/content）")
		return
	}
	draft, err := s.store.UpdateDraft(id, req.DraftRevision, req.Title, req.Content)
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, draft)
	case errors.Is(err, store.ErrDraftNotFound):
		writeMemoryError(w, http.StatusNotFound, "draft_not_found", "草稿不存在")
	case errors.Is(err, store.ErrDraftTerminal):
		writeMemoryError(w, http.StatusConflict, "draft_terminal", "草稿已处于终态，不可编辑")
	case errors.Is(err, store.ErrDraftConflict):
		writeMemoryError(w, http.StatusConflict, "draft_conflict", err.Error())
	case errors.Is(err, store.ErrDraftInvalid):
		writeMemoryError(w, http.StatusUnprocessableEntity, "invalid_draft", err.Error())
	case errors.Is(err, store.ErrMemorySafeState):
		writeMemoryError(w, http.StatusInternalServerError, "memory_safe_state", "记忆子系统处于安全态：拒绝写入")
	default:
		s.log.Error("update draft", "err", err)
		writeError(w, http.StatusInternalServerError, "更新草稿失败")
	}
}

// handlePostDraftExtract 同步执行一次草稿提取；成功返回更新后的草稿与暂存的 ChangeSet。
func (s *Server) handlePostDraftExtract(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validMemoryID(id, "draft_") {
		writeMemoryError(w, http.StatusBadRequest, "invalid_draft_id", "草稿 ID 必须为 draft_ 前缀")
		return
	}
	if s.extractor == nil {
		writeError(w, http.StatusServiceUnavailable, "提取服务未启用")
		return
	}
	var req struct{}
	if err := decodeStrictJSON(w, r, &req, 4096, true); err != nil {
		writeDecodeError(w, err, "extract 请求体必须为空或 {}")
		return
	}
	draft, err := s.store.LoadDraft(id)
	if errors.Is(err, store.ErrDraftNotFound) {
		writeMemoryError(w, http.StatusNotFound, "draft_not_found", "草稿不存在")
		return
	}
	if err != nil {
		s.log.Error("load draft for extract", "err", err)
		writeError(w, http.StatusInternalServerError, "读取草稿失败")
		return
	}
	updated, cs, err := s.extractor.ExtractDraft(r.Context(), draft)
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, map[string]any{"draft": updated, "changeset": cs})
	case errors.Is(err, store.ErrDraftNotFound):
		writeMemoryError(w, http.StatusNotFound, "draft_not_found", "草稿不存在")
	case errors.Is(err, store.ErrDraftTerminal):
		writeMemoryError(w, http.StatusConflict, "draft_terminal", "草稿已处于终态，不可提取")
	case errors.Is(err, store.ErrDraftConflict):
		writeMemoryError(w, http.StatusConflict, "draft_conflict", err.Error())
	case errors.Is(err, store.ErrDraftNeedsRecheck):
		writeMemoryError(w, http.StatusConflict, "draft_needs_recheck", "草稿已变更：请重新提取后再确认")
	case errors.Is(err, store.ErrDraftInvalid):
		writeMemoryError(w, http.StatusUnprocessableEntity, "invalid_draft", err.Error())
	case errors.Is(err, store.ErrRevisionConflict):
		writeMemoryError(w, http.StatusConflict, "revision_conflict", "记忆基线已变化：请重新提取")
	case errors.Is(err, store.ErrAnalysisRunning):
		writeMemoryError(w, http.StatusConflict, "analysis_running", "故事分析进行中，请稍后再提取")
	case errors.Is(err, store.ErrKnowledgeNotReady):
		writeMemoryError(w, http.StatusConflict, "knowledge_not_ready", "知识库为空：请先完成 /api/analyze")
	case errors.Is(err, store.ErrExtractConfigMissing):
		writeMemoryError(w, http.StatusConflict, "config_missing", "请先在「模型设置」中填写 Base URL 和模型名")
	case errors.Is(err, store.ErrMemorySafeState):
		writeMemoryError(w, http.StatusInternalServerError, "memory_safe_state", "记忆子系统处于安全态：拒绝写入")
	case errors.Is(err, store.ErrInvalidChangeSet):
		writeMemoryError(w, http.StatusUnprocessableEntity, "invalid_changeset", err.Error())
	default:
		s.log.Error("extract draft", "draft", id, "err", err)
		writeError(w, http.StatusInternalServerError, "草稿提取失败")
	}
}

func (s *Server) handlePostDraftConfirm(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validMemoryID(id, "draft_") {
		writeMemoryError(w, http.StatusBadRequest, "invalid_draft_id", "草稿 ID 必须为 draft_ 前缀")
		return
	}
	var req struct {
		DraftRevision           int64  `json:"draftRevision"`
		ExpectedChapterRevision int64  `json:"expectedChapterRevision"`
		ExpectedMemoryRevision  int64  `json:"expectedMemoryRevision"`
		MemorySync              string `json:"memorySync"`
	}
	if err := decodeStrictJSON(w, r, &req, 4096, false); err != nil {
		writeDecodeError(w, err, "请求体必须为单个 JSON 对象（draftRevision/expectedChapterRevision/expectedMemoryRevision/memorySync）")
		return
	}
	switch req.MemorySync {
	case "", "body_only", "reextract":
	default:
		writeMemoryError(w, http.StatusBadRequest, "invalid_memory_sync", "memorySync 仅支持 reextract 或 body_only")
		return
	}

	// #63：正文在记忆同步后被修改时，先按用户选择重新提取，再联合提交。
	if req.MemorySync == "reextract" {
		if s.extractor == nil {
			writeError(w, http.StatusServiceUnavailable, "提取服务未启用")
			return
		}
		draft, err := s.store.LoadDraft(id)
		if errors.Is(err, store.ErrDraftNotFound) {
			writeMemoryError(w, http.StatusNotFound, "draft_not_found", "草稿不存在")
			return
		}
		if err != nil {
			s.log.Error("load draft for re-extract", "draft", id, "err", err)
			writeError(w, http.StatusInternalServerError, "读取草稿失败")
			return
		}
		if _, _, err := s.extractor.ExtractDraft(r.Context(), draft); err != nil {
			switch {
			case errors.Is(err, store.ErrExtractConfigMissing):
				writeMemoryError(w, http.StatusConflict, "config_missing", "请先在「模型设置」中填写 Base URL、模型名与 API Key")
			case errors.Is(err, store.ErrKnowledgeNotReady):
				writeMemoryError(w, http.StatusConflict, "knowledge_not_ready", "知识库为空：请先完成分析")
			case errors.Is(err, store.ErrDraftConflict), errors.Is(err, store.ErrDraftNeedsRecheck):
				writeMemoryError(w, http.StatusConflict, "draft_conflict", "草稿版本已变化，请刷新后重试")
			case errors.Is(err, store.ErrDraftTerminal):
				writeMemoryError(w, http.StatusConflict, "draft_terminal", "草稿已处于终态")
			case errors.Is(err, store.ErrAnalysisRunning):
				writeMemoryError(w, http.StatusConflict, "analysis_running", "故事分析进行中，请稍后再试")
			case errors.Is(err, store.ErrMemorySafeState):
				writeMemoryError(w, http.StatusInternalServerError, "memory_safe_state", "记忆子系统处于安全态：拒绝写入")
			default:
				s.log.Error("re-extract draft for confirm", "draft", id, "err", err)
				writeError(w, http.StatusInternalServerError, "重新提取记忆失败")
			}
			return
		}
	}
	result, err := s.store.ConfirmDraft(id, store.DraftConfirmParams{
		DraftRevision:           req.DraftRevision,
		ExpectedChapterRevision: req.ExpectedChapterRevision,
		ExpectedMemoryRevision:  req.ExpectedMemoryRevision,
		AllowBodyOnly:           req.MemorySync == "body_only",
	})
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, result)
	case errors.Is(err, store.ErrDraftMemoryStale):
		writeMemoryError(w, http.StatusConflict, "draft_memory_stale", "正文已修改但记忆尚未同步：请选择「重新提取记忆」或「仅提交正文（记忆不更新）」")
	case errors.Is(err, store.ErrDraftNotFound):
		writeMemoryError(w, http.StatusNotFound, "draft_not_found", "草稿不存在")
	case errors.Is(err, store.ErrDraftTerminal):
		writeMemoryError(w, http.StatusConflict, "draft_terminal", "草稿已处于终态")
	case errors.Is(err, store.ErrDraftConflict):
		writeMemoryError(w, http.StatusConflict, "draft_conflict", err.Error())
	case errors.Is(err, store.ErrDraftNeedsRecheck):
		writeMemoryError(w, http.StatusConflict, "draft_needs_recheck", "草稿变更集已失效：请重新提取后再确认")
	case errors.Is(err, store.ErrRevisionConflict):
		writeMemoryError(w, http.StatusConflict, "revision_conflict", "记忆基线已变化：请重新提取后再确认")
	case errors.Is(err, store.ErrChapterConflict):
		writeMemoryError(w, http.StatusConflict, "chapter_conflict", err.Error())
	case errors.Is(err, store.ErrChapterNotFound):
		writeMemoryError(w, http.StatusNotFound, "chapter_not_found", "目标章节不存在")
	case errors.Is(err, store.ErrDraftInvalid):
		writeMemoryError(w, http.StatusUnprocessableEntity, "invalid_draft", err.Error())
	case errors.Is(err, store.ErrAnalysisRunning):
		writeMemoryError(w, http.StatusConflict, "analysis_running", "故事分析进行中，不可提交")
	case errors.Is(err, store.ErrInvalidChangeSet):
		writeMemoryError(w, http.StatusUnprocessableEntity, "invalid_changeset", err.Error())
	case errors.Is(err, store.ErrChangeSetsCorrupted):
		writeMemoryError(w, http.StatusInternalServerError, "changesets_corrupted", "草稿区已损坏，拒绝提交")
	case errors.Is(err, store.ErrMemorySafeState):
		writeMemoryError(w, http.StatusInternalServerError, "memory_safe_state", "记忆子系统处于安全态：拒绝写入")
	case errors.Is(err, store.ErrOptimisticConflict):
		writeMemoryError(w, http.StatusConflict, "optimistic_conflict", err.Error())
	default:
		s.log.Error("confirm draft", "draft", id, "err", err)
		writeError(w, http.StatusInternalServerError, "确认草稿失败")
	}
}

func (s *Server) handlePostDraftReject(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validMemoryID(id, "draft_") {
		writeMemoryError(w, http.StatusBadRequest, "invalid_draft_id", "草稿 ID 必须为 draft_ 前缀")
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	if err := decodeStrictJSON(w, r, &req, 4096, true); err != nil {
		writeDecodeError(w, err, "reject 请求体必须为空或 {\"reason\":\"...\"}")
		return
	}
	draft, err := s.store.RejectDraft(id, req.Reason)
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, draft)
	case errors.Is(err, store.ErrDraftNotFound):
		writeMemoryError(w, http.StatusNotFound, "draft_not_found", "草稿不存在")
	case errors.Is(err, store.ErrDraftTerminal):
		writeMemoryError(w, http.StatusConflict, "draft_terminal", "草稿已处于终态")
	default:
		s.log.Error("reject draft", "draft", id, "err", err)
		writeError(w, http.StatusInternalServerError, "驳回草稿失败")
	}
}

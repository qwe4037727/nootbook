package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"novelagent/internal/novel"
	"novelagent/internal/store"
)

// 记忆审阅 API（I2-3/I2-4/I2-5/I2-6/I2-1，iter2-详设 §6）。
// 风格对齐 §12：MaxBytesReader、DisallowUnknownFields、{code,error} 机器码响应。

func (s *Server) registerMemoryReviewRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/memory/extract", s.handlePostMemoryExtract)
	mux.HandleFunc("GET /api/memory/extract/status", s.handleGetMemoryExtractStatus)
	mux.HandleFunc("GET /api/memory/changesets", s.handleGetChangesets)
	mux.HandleFunc("GET /api/memory/changesets/{id}", s.handleGetChangeset)
	mux.HandleFunc("POST /api/memory/changesets/{id}/accept", s.handlePostChangesetAccept)
	mux.HandleFunc("POST /api/memory/changesets/{id}/reject", s.handlePostChangesetReject)
	mux.HandleFunc("POST /api/memory/changesets/{id}/rebase", s.handlePostChangesetRebase)
	mux.HandleFunc("GET /api/memory/candidates", s.handleGetCandidates)
	mux.HandleFunc("POST /api/memory/candidates/{id}/resolve", s.handlePostCandidateResolve)
	mux.HandleFunc("POST /api/memory/reconcile", s.handlePostReconcile)
}

// ---- 提取 ----

func (s *Server) handlePostMemoryExtract(w http.ResponseWriter, r *http.Request) {
	if s.extractor == nil {
		writeError(w, http.StatusServiceUnavailable, "提取服务未启用")
		return
	}
	var req struct {
		ChapterIDs []string `json:"chapterIds"`
		Force      bool     `json:"force"`
	}
	if err := decodeStrictJSON(w, r, &req, 4096, false); err != nil {
		writeDecodeError(w, err, "请求体必须为单个 JSON 对象（chapterIds/force）")
		return
	}
	status, token, err := s.store.BeginExtract(req.ChapterIDs, req.Force)
	switch {
	case err == nil:
		if status == store.ExtractDone {
			writeJSON(w, http.StatusAccepted, map[string]string{"extractStatus": "done"})
			return
		}
		// ExtractRunning：启动后台提取任务（token 已授权，失效由 Stash/Finsh 零写入保证）。
		go s.extractor.Run(context.Background(), token)
		writeJSON(w, http.StatusAccepted, map[string]string{"extractStatus": "extracting"})
	case errors.Is(err, store.ErrExtractAlreadyRunning):
		writeMemoryError(w, http.StatusConflict, "already_running", "深度提取正在进行中")
	case errors.Is(err, store.ErrAnalysisRunning):
		writeMemoryError(w, http.StatusConflict, "analysis_running", "故事分析正在进行中，请稍后再提取")
	case errors.Is(err, store.ErrProjectNotMigrated):
		writeMemoryError(w, http.StatusConflict, "project_not_migrated", "项目尚未迁移：请先启动服务自动完成迁移")
	case errors.Is(err, store.ErrKnowledgeNotReady):
		writeMemoryError(w, http.StatusConflict, "knowledge_not_ready", "知识库为空：请先完成 /api/analyze")
	case errors.Is(err, store.ErrExtractConfigMissing):
		writeMemoryError(w, http.StatusConflict, "config_missing", "请先在「模型设置」中填写 Base URL 和模型名")
	default:
		if strings.Contains(err.Error(), "invalid chapter id") {
			writeMemoryError(w, http.StatusBadRequest, "invalid_chapter_id", "章节 ID 格式不正确")
			return
		}
		s.log.Error("begin extract", "err", err)
		writeError(w, http.StatusInternalServerError, "启动深度提取失败")
	}
}

func (s *Server) handleGetMemoryExtractStatus(w http.ResponseWriter, r *http.Request) {
	state, err := s.store.LoadExtractState()
	if err != nil {
		s.log.Error("load extract state", "err", err)
		writeError(w, http.StatusInternalServerError, "读取提取状态失败")
		return
	}
	writeJSON(w, http.StatusOK, state)
}

// ---- ChangeSet 审阅 ----

func (s *Server) handleGetChangesets(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	summaries, err := s.store.LoadChangeSets(status)
	if err != nil {
		s.log.Error("load changesets", "err", err)
		writeError(w, http.StatusInternalServerError, "读取变更集列表失败")
		return
	}
	writeJSON(w, http.StatusOK, summaries)
}

func (s *Server) handleGetChangeset(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validMemoryID(id, "mcs_") {
		writeMemoryError(w, http.StatusBadRequest, "invalid_changeset_id", "变更集 ID 必须为 mcs_ 前缀")
		return
	}
	cs, err := s.store.GetChangeSet(id)
	if errors.Is(err, store.ErrChangeSetNotFound) {
		writeMemoryError(w, http.StatusNotFound, "changeset_not_found", "变更集不存在")
		return
	}
	if err != nil {
		s.log.Error("get changeset", "err", err)
		writeError(w, http.StatusInternalServerError, "读取变更集失败")
		return
	}
	writeJSON(w, http.StatusOK, cs)
}

func (s *Server) handlePostChangesetAccept(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validMemoryID(id, "mcs_") {
		writeMemoryError(w, http.StatusBadRequest, "invalid_changeset_id", "变更集 ID 必须为 mcs_ 前缀")
		return
	}
	var req struct{}
	if err := decodeStrictJSON(w, r, &req, 4096, true); err != nil {
		writeDecodeError(w, err, "accept 请求体必须为空或 {}")
		return
	}
	result, err := s.store.AcceptChangeSet(id)
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, result)
	case errors.Is(err, store.ErrChangeSetNotFound):
		writeMemoryError(w, http.StatusNotFound, "changeset_not_found", "变更集不存在")
	case errors.Is(err, store.ErrChangeSetTerminal):
		writeMemoryError(w, http.StatusConflict, "changeset_terminal", "变更集已处于终态，不可重复提交")
	case errors.Is(err, store.ErrDraftManagedChangeSet):
		writeMemoryError(w, http.StatusConflict, "draft_managed_changeset", "该变更集属于草稿：请通过草稿确认一次提交正文与记忆")
	case errors.Is(err, store.ErrRevisionConflict):
		writeMemoryError(w, http.StatusConflict, "revision_conflict", "基线已过期：请先 rebase 再 accept")
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
		s.log.Error("accept changeset", "err", err)
		writeError(w, http.StatusInternalServerError, "提交变更集失败")
	}
}

func (s *Server) handlePostChangesetReject(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validMemoryID(id, "mcs_") {
		writeMemoryError(w, http.StatusBadRequest, "invalid_changeset_id", "变更集 ID 必须为 mcs_ 前缀")
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	if err := decodeStrictJSON(w, r, &req, 4096, true); err != nil {
		writeDecodeError(w, err, "reject 请求体必须为空或 {\"reason\":\"...\"}")
		return
	}
	if err := s.store.RejectChangeSet(id, req.Reason); err != nil {
		switch {
		case errors.Is(err, store.ErrChangeSetNotFound):
			writeMemoryError(w, http.StatusNotFound, "changeset_not_found", "变更集不存在")
		case errors.Is(err, store.ErrChangeSetTerminal):
			writeMemoryError(w, http.StatusConflict, "changeset_terminal", "变更集已处于终态")
		default:
			s.log.Error("reject changeset", "err", err)
			writeError(w, http.StatusInternalServerError, "驳回变更集失败")
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "rejected"})
}

func (s *Server) handlePostChangesetRebase(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validMemoryID(id, "mcs_") {
		writeMemoryError(w, http.StatusBadRequest, "invalid_changeset_id", "变更集 ID 必须为 mcs_ 前缀")
		return
	}
	var req struct{}
	if err := decodeStrictJSON(w, r, &req, 4096, true); err != nil {
		writeDecodeError(w, err, "rebase 请求体必须为空或 {}")
		return
	}
	cs, err := s.store.RebaseChangeSet(id)
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, cs)
	case errors.Is(err, store.ErrChangeSetNotFound):
		writeMemoryError(w, http.StatusNotFound, "changeset_not_found", "变更集不存在")
	case errors.Is(err, store.ErrChangeSetTerminal):
		writeMemoryError(w, http.StatusConflict, "changeset_terminal", "仅 proposed 状态可 rebase")
	case errors.Is(err, store.ErrDraftManagedChangeSet):
		writeMemoryError(w, http.StatusConflict, "draft_managed_changeset", "该变更集属于草稿：请重新提取后再走草稿确认")
	default:
		s.log.Error("rebase changeset", "err", err)
		writeError(w, http.StatusInternalServerError, "重算变更集基线失败")
	}
}

// ---- 实体候选 ----

func (s *Server) handleGetCandidates(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	cands, err := s.store.LoadCandidates(status)
	if err != nil {
		s.log.Error("load candidates", "err", err)
		writeError(w, http.StatusInternalServerError, "读取实体候选失败")
		return
	}
	writeJSON(w, http.StatusOK, cands)
}

func (s *Server) handlePostCandidateResolve(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validMemoryID(id, "cand_") {
		writeMemoryError(w, http.StatusBadRequest, "invalid_candidate_id", "候选 ID 必须为 cand_ 前缀")
		return
	}
	var req struct {
		Action     string `json:"action"`
		Resolution string `json:"resolution"`
	}
	if err := decodeStrictJSON(w, r, &req, 4096, false); err != nil {
		writeDecodeError(w, err, "请求体必须为单个 JSON 对象（action/resolution）")
		return
	}
	switch req.Action {
	case "register", "merge", "dismiss":
	default:
		writeMemoryError(w, http.StatusBadRequest, "invalid_action", "action 必须为 register|merge|dismiss")
		return
	}
	cand, err := s.store.ResolveCandidate(id, req.Action, req.Resolution)
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, map[string]string{"status": cand.Status})
	case errors.Is(err, store.ErrCandidateNotFound):
		writeMemoryError(w, http.StatusNotFound, "candidate_not_found", "实体候选不存在")
	case errors.Is(err, store.ErrCandidateNotPending):
		writeMemoryError(w, http.StatusConflict, "candidate_not_pending", "候选已处理，不可重复确认")
	case errors.Is(err, store.ErrCandidateResolutionInvalid):
		writeMemoryError(w, http.StatusBadRequest, "invalid_resolution", err.Error())
	default:
		s.log.Error("resolve candidate", "err", err)
		writeError(w, http.StatusInternalServerError, "确认实体候选失败")
	}
}

// ---- 证据失效复核 ----

func (s *Server) handlePostReconcile(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ChapterIDs []string `json:"chapterIds"`
	}
	if err := decodeStrictJSON(w, r, &req, 4096, true); err != nil {
		writeDecodeError(w, err, "reconcile 请求体必须为空或 {\"chapterIds\":[...]}")
		return
	}
	marked, revision, err := s.store.ReconcileMemory(req.ChapterIDs)
	if err != nil {
		if errors.Is(err, store.ErrMemorySafeState) {
			writeMemoryError(w, http.StatusInternalServerError, "memory_safe_state", "记忆子系统处于安全态：拒绝复核")
			return
		}
		s.log.Error("reconcile memory", "err", err)
		writeError(w, http.StatusInternalServerError, "证据复核失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"marked": marked, "memoryRevision": revision})
}

// ---- 辅助 ----

// validMemoryID 校验 ID 格式（novel.ValidID + 指定前缀）。
func validMemoryID(id, prefix string) bool {
	return novel.ValidID(id) && strings.HasPrefix(id, prefix)
}

var errRequestBodyTooLarge = errors.New("server: request body too large")

// decodeStrictJSON 按 §12.3 契约解析请求体：上限 limit 字节（超限 → errRequestBodyTooLarge）、
// 必须是单个 JSON 对象（`null`/数组/字面量均拒绝）、未知字段拒绝、尾随 JSON/垃圾拒绝。
// allowEmpty=true 时空体/纯空白视为零值。
func decodeStrictJSON(w http.ResponseWriter, r *http.Request, dst any, limit int64, allowEmpty bool) error {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return errRequestBodyTooLarge
		}
		return err
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		if allowEmpty {
			return nil
		}
		return io.EOF
	}
	if trimmed[0] != '{' {
		return errors.New("request body must be a single JSON object")
	}
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return errors.New("request body must contain exactly one JSON object")
	}
	return nil
}

// writeDecodeError 将 decodeStrictJSON 的错误映射为 413 request_too_large / 400 invalid_request。
func writeDecodeError(w http.ResponseWriter, err error, message string) {
	if errors.Is(err, errRequestBodyTooLarge) {
		writeMemoryError(w, http.StatusRequestEntityTooLarge, "request_too_large", "请求体不能超过 4096 字节")
		return
	}
	writeMemoryError(w, http.StatusBadRequest, "invalid_request", message)
}

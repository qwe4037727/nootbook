package server

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"novelagent/internal/adapt"
	"novelagent/internal/novel"
	"novelagent/internal/store"
)

// 改编工作台 API（M3）：选区改写 → diff → 模式B影响清单 → 草稿。
// 路由仅在 milestone.yaml 的 m3.enabled=true 时注册。

func (s *Server) registerAdaptRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/adapt/rewrite", s.handleAdaptRewrite)
	mux.HandleFunc("POST /api/adapt/diff", s.handleAdaptDiff)
	mux.HandleFunc("GET /api/adapt/runs", s.handleListAdaptRuns)
	mux.HandleFunc("GET /api/adapt/runs/{id}", s.handleGetAdaptRun)
}

func (s *Server) handleAdaptRewrite(w http.ResponseWriter, r *http.Request) {
	var req adapt.Request
	if err := decodeStrictJSON(w, r, &req, 32<<10, false); err != nil {
		writeDecodeError(w, err, "请求体必须为单个 JSON 对象（chapterId/startOffset/endOffset/instruction/mode）")
		return
	}
	if !novel.ValidID(req.ChapterID) || !strings.HasPrefix(req.ChapterID, "ch_") {
		writeMemoryError(w, http.StatusBadRequest, "invalid_chapter_id", "chapterId 必须为 ch_ 前缀")
		return
	}
	result, err := s.adapt.Run(r.Context(), req)
	if err != nil {
		switch {
		case errors.Is(err, adapt.ErrInvalidRequest), errors.Is(err, novel.ErrKBValidation):
			writeMemoryError(w, http.StatusBadRequest, "invalid_adapt_request", err.Error())
		case errors.Is(err, adapt.ErrChapterNotFound), errors.Is(err, store.ErrChapterNotFound), errors.Is(err, store.ErrInvalidChapter):
			writeMemoryError(w, http.StatusNotFound, "chapter_not_found", "章节不存在")
		case errors.Is(err, adapt.ErrConfigMissing):
			writeMemoryError(w, http.StatusConflict, "config_missing", "请先在「模型设置」中填写 Base URL 和模型名")
		case errors.Is(err, adapt.ErrTruncated):
			writeMemoryError(w, http.StatusUnprocessableEntity, "model_truncated", "模型输出被截断，请缩短选区或调大输出上限")
		default:
			s.log.Error("adapt rewrite failed", "chapter", req.ChapterID, "err", err)
			writeMemoryError(w, http.StatusBadGateway, "model_call_failed", "模型调用失败："+shortError(err))
		}
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleAdaptDiff(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Original  string `json:"original"`
		Rewritten string `json:"rewritten"`
	}
	if err := decodeStrictJSON(w, r, &req, 2<<20, false); err != nil {
		writeDecodeError(w, err, "请求体必须为单个 JSON 对象（original/rewritten）")
		return
	}
	if strings.TrimSpace(req.Rewritten) == "" {
		writeMemoryError(w, http.StatusBadRequest, "invalid_adapt_request", "rewritten 不能为空")
		return
	}
	text, parts := adapt.Diff(req.Original, req.Rewritten)
	writeJSON(w, http.StatusOK, map[string]any{"diff": text, "diffParts": parts})
}

func (s *Server) handleListAdaptRuns(w http.ResponseWriter, r *http.Request) {
	limit := 100
	if value := strings.TrimSpace(r.URL.Query().Get("limit")); value != "" {
		if n, err := strconv.Atoi(value); err == nil && n > 0 {
			limit = n
		}
	}
	runs, err := s.adapt.Runs(limit)
	if err != nil {
		s.log.Error("list adapt runs", "err", err)
		writeError(w, http.StatusInternalServerError, "读取改编记录失败")
		return
	}
	writeJSON(w, http.StatusOK, runs)
}

func (s *Server) handleGetAdaptRun(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !novel.ValidID(id) || !strings.HasPrefix(id, "adpt_") {
		writeMemoryError(w, http.StatusBadRequest, "invalid_run_id", "改编记录 ID 必须为 adpt_ 前缀")
		return
	}
	run, err := s.adapt.RunByID(id)
	if errors.Is(err, store.ErrAdaptRunNotFound) {
		writeMemoryError(w, http.StatusNotFound, "run_not_found", "改编记录不存在")
		return
	}
	if err != nil {
		s.log.Error("get adapt run", "err", err)
		writeError(w, http.StatusInternalServerError, "读取改编记录失败")
		return
	}
	writeJSON(w, http.StatusOK, run)
}

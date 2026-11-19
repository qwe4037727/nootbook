package server

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"novelagent/internal/store"
	"novelagent/internal/task"
)

func (s *Server) registerTaskRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/tasks", s.handleCreateTask)
	mux.HandleFunc("GET /api/tasks", s.handleListTasks)
	mux.HandleFunc("GET /api/tasks/{id}", s.handleGetTask)
	mux.HandleFunc("POST /api/tasks/{id}/pause", s.handlePauseTask)
	mux.HandleFunc("POST /api/tasks/{id}/resume", s.handleResumeTask)
	mux.HandleFunc("POST /api/tasks/{id}/cancel", s.handleCancelTask)
}

func (s *Server) taskServiceReady(w http.ResponseWriter) bool {
	if s.tasks == nil {
		writeError(w, http.StatusServiceUnavailable, "任务调度中心未启用")
		return false
	}
	return true
}

func (s *Server) handleCreateTask(w http.ResponseWriter, r *http.Request) {
	if !s.taskServiceReady(w) {
		return
	}
	var req task.CreateRequest
	if err := decodeJSON(w, r, &req, 256<<10); err != nil {
		writeError(w, http.StatusBadRequest, "任务格式不正确")
		return
	}
	created, err := s.tasks.Create(req)
	if err != nil {
		switch {
		case errors.Is(err, task.ErrInvalidTaskRequest):
			writeError(w, http.StatusBadRequest, shortError(err))
		case errors.Is(err, task.ErrConfigMissing):
			writeError(w, http.StatusBadRequest, "请先在「模型设置」中填写 Base URL 和模型名")
		case errors.Is(err, task.ErrAgentUnavailable):
			writeError(w, http.StatusBadRequest, "Agent 未注册或已停用："+shortError(err))
		default:
			s.log.Error("create task", "err", err)
			writeError(w, http.StatusInternalServerError, "创建任务失败")
		}
		return
	}
	writeJSON(w, http.StatusOK, created)
}

func (s *Server) handleListTasks(w http.ResponseWriter, r *http.Request) {
	if !s.taskServiceReady(w) {
		return
	}
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	tasks, err := s.tasks.List(status, queryLimit(r, 50, 200))
	if err != nil {
		s.log.Error("list tasks", "err", err)
		writeError(w, http.StatusInternalServerError, "读取任务失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tasks": tasks})
}

func (s *Server) handleGetTask(w http.ResponseWriter, r *http.Request) {
	if !s.taskServiceReady(w) {
		return
	}
	item, err := s.tasks.Get(r.PathValue("id"))
	if err != nil {
		s.writeTaskError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) handlePauseTask(w http.ResponseWriter, r *http.Request) {
	s.writeTaskAction(w, func() (any, error) { return s.tasks.Pause(r.PathValue("id")) })
}

func (s *Server) handleResumeTask(w http.ResponseWriter, r *http.Request) {
	s.writeTaskAction(w, func() (any, error) { return s.tasks.Resume(r.PathValue("id")) })
}

func (s *Server) handleCancelTask(w http.ResponseWriter, r *http.Request) {
	s.writeTaskAction(w, func() (any, error) { return s.tasks.Cancel(r.PathValue("id")) })
}

func (s *Server) writeTaskAction(w http.ResponseWriter, action func() (any, error)) {
	if !s.taskServiceReady(w) {
		return
	}
	updated, err := action()
	if err != nil {
		s.writeTaskError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) writeTaskError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrTaskNotFound):
		writeError(w, http.StatusNotFound, "任务不存在")
	case errors.Is(err, store.ErrTaskStateConflict):
		writeError(w, http.StatusConflict, "任务当前状态不允许该操作")
	default:
		s.log.Error("task request failed", "err", err)
		writeError(w, http.StatusInternalServerError, "任务操作失败")
	}
}

func (s *Server) registerAuditRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/audit/reports", s.handleAuditReports)
	mux.HandleFunc("GET /api/audit/cost", s.handleAuditCost)
}

func (s *Server) handleAuditReports(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	filter := store.AgentReportFilter{
		AgentKey:        strings.TrimSpace(query.Get("agent")),
		ChapterID:       strings.TrimSpace(query.Get("chapterId")),
		Severity:        strings.TrimSpace(query.Get("severity")),
		EntryID:         strings.TrimSpace(query.Get("entryId")),
		ChapterIndexMin: queryInt(query.Get("chapterIndexMin")),
		ChapterIndexMax: queryInt(query.Get("chapterIndexMax")),
		Limit:           queryLimit(r, 100, 500),
	}
	reports, err := s.store.ListAgentReportsFiltered(filter)
	if err != nil {
		s.log.Error("list audit reports", "err", err)
		writeError(w, http.StatusInternalServerError, "读取审计报告失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"reports": reports, "filter": filter})
}

func (s *Server) handleAuditCost(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	since := time.Time{}
	if value := strings.TrimSpace(query.Get("since")); value != "" {
		parsed, err := time.Parse(time.RFC3339, value)
		if err != nil {
			writeError(w, http.StatusBadRequest, "since 需要 RFC3339 时间")
			return
		}
		since = parsed
	} else if days := queryInt(query.Get("days")); days > 0 {
		since = time.Now().UTC().Add(-time.Duration(days) * 24 * time.Hour)
	}
	summary, err := s.store.CostSummary(since)
	if err != nil {
		s.log.Error("cost summary", "err", err)
		writeError(w, http.StatusInternalServerError, "读取成本看板失败")
		return
	}
	writeJSON(w, http.StatusOK, summary)
}

func queryInt(value string) int {
	number := 0
	for _, r := range strings.TrimSpace(value) {
		if r < '0' || r > '9' {
			return 0
		}
		number = number*10 + int(r-'0')
		if number > 1_000_000 {
			return 1_000_000
		}
	}
	return number
}

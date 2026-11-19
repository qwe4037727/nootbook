package server

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	agentspkg "novelagent/internal/agents"
	"novelagent/internal/milestone"
	"novelagent/internal/store"
)

// M1 Agent 中心 / 审计 / 伏笔台账 API 清单（全部按项目隔离，挂载在项目 Handler 上）：
//
//	GET    /api/agents/config                  Agent 开关与权限档位（项目覆盖 > milestone 默认 > observe）
//	PUT    /api/agents/config/{key}            更新单个 Agent 的 enabled/level
//	GET    /api/agents/runs                    运行日志（agent/status/limit），含 token 与耗时
//	GET    /api/agents/stats?days=7            按 Agent 汇总成本与失败率
//	GET    /api/agents/reports                 审计报告列表（agent/chapterId/severity/limit）
//	GET    /api/agents/reports/{id}            单份报告详情（findings 含章节证据链接）
//	GET    /api/agents/proposals               审阅提案队列（status/limit）
//	PATCH  /api/agents/proposals/{id}          提案状态：pending/accepted/rejected/deferred
//	GET    /api/foreshadows                    伏笔台账（status/limit）
//	PATCH  /api/foreshadows/{id}               台账状态：candidate/confirmed/dismissed
//
// 约束：Agent 无“修改正文/删除词条”接口；所有提案只入队，由人工审阅。

func (s *Server) registerAgentRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/agents/config", s.handleGetAgentConfig)
	mux.HandleFunc("PUT /api/agents/config/{key}", s.handlePutAgentConfig)
	mux.HandleFunc("GET /api/agents/runs", s.handleGetAgentRuns)
	mux.HandleFunc("GET /api/agents/stats", s.handleGetAgentStats)
	mux.HandleFunc("GET /api/agents/reports", s.handleGetAgentReports)
	mux.HandleFunc("GET /api/agents/reports/{id}", s.handleGetAgentReport)
	mux.HandleFunc("GET /api/agents/proposals", s.handleGetAgentProposals)
	mux.HandleFunc("PATCH /api/agents/proposals/{id}", s.handlePatchAgentProposal)
	mux.HandleFunc("GET /api/foreshadows", s.handleGetForeshadows)
	mux.HandleFunc("PATCH /api/foreshadows/{id}", s.handlePatchForeshadow)
}

type agentConfigView struct {
	Key      string   `json:"key"`
	Name     string   `json:"name"`
	Triggers []string `json:"triggers"`
	Enabled  bool     `json:"enabled"`
	Level    string   `json:"level"`
	Source   string   `json:"source"`
}

func (s *Server) agentConfigViews() ([]agentConfigView, error) {
	cfg := milestone.Load(s.store.Dir())
	saved, err := s.store.LoadAgentConfigs()
	if err != nil {
		return nil, err
	}
	out := make([]agentConfigView, 0, 3)
	for _, descriptor := range agentspkg.Descriptors() {
		view := agentConfigView{
			Key:      descriptor.Key,
			Name:     descriptor.Name,
			Triggers: descriptor.Triggers,
			Enabled:  true,
			Level:    "observe",
			Source:   "default",
		}
		if def, ok := cfg.Defaults[descriptor.Key]; ok {
			view.Enabled, view.Level, view.Source = def.Enabled, def.Level, cfg.Source
		}
		if rec, ok := saved[descriptor.Key]; ok {
			view.Enabled, view.Level, view.Source = rec.Enabled, rec.Level, "project"
		}
		out = append(out, view)
	}
	return out, nil
}

func (s *Server) handleGetAgentConfig(w http.ResponseWriter, r *http.Request) {
	views, err := s.agentConfigViews()
	if err != nil {
		s.log.Error("load agent config", "err", err)
		writeMemoryError(w, http.StatusInternalServerError, "agent_config_failed", "读取 Agent 配置失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"agents": views, "defaultLevel": "observe"})
}

func (s *Server) handlePutAgentConfig(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimSpace(r.PathValue("key"))
	if _, ok := agentspkg.Keys()[key]; !ok {
		writeMemoryError(w, http.StatusNotFound, "agent_not_found", "未知的 Agent")
		return
	}
	var req struct {
		Enabled *bool  `json:"enabled"`
		Level   string `json:"level"`
	}
	if err := decodeStrictJSON(w, r, &req, 4096, false); err != nil {
		writeDraftDecodeError(w, err, "请求体必须为单个 JSON 对象（enabled/level）")
		return
	}
	level := strings.TrimSpace(req.Level)
	switch level {
	case "", "observe", "propose", "autonomous":
	default:
		writeMemoryError(w, http.StatusBadRequest, "invalid_level", "level 仅支持 observe/propose/autonomous")
		return
	}
	views, err := s.agentConfigViews()
	if err != nil {
		writeMemoryError(w, http.StatusInternalServerError, "agent_config_failed", "读取 Agent 配置失败")
		return
	}
	var current agentConfigView
	for _, view := range views {
		if view.Key == key {
			current = view
			break
		}
	}
	enabled := current.Enabled
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	if level == "" {
		level = current.Level
	}
	if err := s.store.SaveAgentConfig(store.AgentConfigRecord{AgentKey: key, Enabled: enabled, Level: level}); err != nil {
		s.log.Error("save agent config", "agent", key, "err", err)
		writeMemoryError(w, http.StatusInternalServerError, "agent_config_failed", "保存 Agent 配置失败")
		return
	}
	updated, _ := s.agentConfigViews()
	writeJSON(w, http.StatusOK, map[string]any{"agents": updated})
}

func (s *Server) handleGetAgentRuns(w http.ResponseWriter, r *http.Request) {
	limit := queryLimit(r, 20, 200)
	runs, err := s.store.QueryAgentRuns(store.AgentRunFilter{
		AgentKey: strings.TrimSpace(r.URL.Query().Get("agent")),
		Status:   strings.TrimSpace(r.URL.Query().Get("status")),
		Limit:    limit,
	})
	if err != nil {
		s.log.Error("query agent runs", "err", err)
		writeMemoryError(w, http.StatusInternalServerError, "agent_runs_failed", "读取运行日志失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"runs": runs})
}

func (s *Server) handleGetAgentStats(w http.ResponseWriter, r *http.Request) {
	days := 7
	if raw := strings.TrimSpace(r.URL.Query().Get("days")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 && n <= 90 {
			days = n
		}
	}
	since := time.Now().UTC().AddDate(0, 0, -days)
	stats, err := s.store.AgentRunStats(since)
	if err != nil {
		s.log.Error("query agent stats", "err", err)
		writeMemoryError(w, http.StatusInternalServerError, "agent_stats_failed", "读取成本统计失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"days": days, "since": since, "stats": stats})
}

func (s *Server) handleGetAgentReports(w http.ResponseWriter, r *http.Request) {
	reports, err := s.store.ListAgentReports(
		strings.TrimSpace(r.URL.Query().Get("agent")),
		strings.TrimSpace(r.URL.Query().Get("chapterId")),
		strings.TrimSpace(r.URL.Query().Get("severity")),
		queryLimit(r, 20, 100),
	)
	if err != nil {
		s.log.Error("list agent reports", "err", err)
		writeMemoryError(w, http.StatusInternalServerError, "agent_reports_failed", "读取审计报告失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"reports": reports})
}

func (s *Server) handleGetAgentReport(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	report, err := s.store.GetAgentReport(id)
	if errors.Is(err, store.ErrAgentRecordNotFound) {
		writeMemoryError(w, http.StatusNotFound, "report_not_found", "报告不存在")
		return
	}
	if err != nil {
		s.log.Error("get agent report", "id", id, "err", err)
		writeMemoryError(w, http.StatusInternalServerError, "agent_report_failed", "读取报告失败")
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (s *Server) handleGetAgentProposals(w http.ResponseWriter, r *http.Request) {
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	if status == "" {
		status = "pending"
	}
	proposals, err := s.store.ListAgentProposals(status, queryLimit(r, 50, 200))
	if err != nil {
		s.log.Error("list agent proposals", "err", err)
		writeMemoryError(w, http.StatusInternalServerError, "agent_proposals_failed", "读取提案失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"proposals": proposals})
}

func (s *Server) handlePatchAgentProposal(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	var req struct {
		Status string `json:"status"`
	}
	if err := decodeStrictJSON(w, r, &req, 4096, false); err != nil {
		writeDraftDecodeError(w, err, "请求体必须为单个 JSON 对象（status）")
		return
	}
	switch req.Status {
	case "pending", "accepted", "rejected", "deferred":
	default:
		writeMemoryError(w, http.StatusBadRequest, "invalid_status", "status 仅支持 pending/accepted/rejected/deferred")
		return
	}
	if err := s.store.SetAgentProposalStatus(id, req.Status); errors.Is(err, store.ErrAgentRecordNotFound) {
		writeMemoryError(w, http.StatusNotFound, "proposal_not_found", "提案不存在")
		return
	} else if err != nil {
		s.log.Error("update proposal", "id", id, "err", err)
		writeMemoryError(w, http.StatusInternalServerError, "agent_proposal_failed", "更新提案失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "status": req.Status})
}

func (s *Server) handleGetForeshadows(w http.ResponseWriter, r *http.Request) {
	records, err := s.store.ListForeshadows(strings.TrimSpace(r.URL.Query().Get("status")), queryLimit(r, 100, 500))
	if err != nil {
		s.log.Error("list foreshadows", "err", err)
		writeMemoryError(w, http.StatusInternalServerError, "foreshadow_failed", "读取伏笔台账失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"foreshadows": records})
}

func (s *Server) handlePatchForeshadow(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	var req struct {
		Status string `json:"status"`
	}
	if err := decodeStrictJSON(w, r, &req, 4096, false); err != nil {
		writeDraftDecodeError(w, err, "请求体必须为单个 JSON 对象（status）")
		return
	}
	switch req.Status {
	case "candidate", "confirmed", "dismissed":
	default:
		writeMemoryError(w, http.StatusBadRequest, "invalid_status", "status 仅支持 candidate/confirmed/dismissed")
		return
	}
	if err := s.store.SetForeshadowStatus(id, req.Status); errors.Is(err, store.ErrAgentRecordNotFound) {
		writeMemoryError(w, http.StatusNotFound, "foreshadow_not_found", "伏笔不存在")
		return
	} else if err != nil {
		s.log.Error("update foreshadow", "id", id, "err", err)
		writeMemoryError(w, http.StatusInternalServerError, "foreshadow_failed", "更新伏笔状态失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "status": req.Status})
}

func queryLimit(r *http.Request, fallback, max int) int {
	raw := strings.TrimSpace(r.URL.Query().Get("limit"))
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return fallback
	}
	if n > max {
		return max
	}
	return n
}

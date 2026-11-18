package server

import (
	"errors"
	"net/http"
	"strings"

	"novelagent/internal/novel"
	"novelagent/internal/plan"
	"novelagent/internal/store"
)

// 计划 API（F20–F23，iter3-详设 §5）：生成草案、读取计划树、确认/暂停/完成/取消。
// 计划只约束未来走向，不覆盖已确认事实；同一作用域至多一个 active。

func (s *Server) registerPlanRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/plans/generate", s.handlePostPlansGenerate)
	mux.HandleFunc("GET /api/plans", s.handleGetPlans)
	mux.HandleFunc("POST /api/plans/{id}/activate", s.handlePostPlanActivate)
	mux.HandleFunc("POST /api/plans/{id}/status", s.handlePostPlanStatus)
}

func (s *Server) handlePostPlansGenerate(w http.ResponseWriter, r *http.Request) {
	if s.planner == nil {
		writeError(w, http.StatusServiceUnavailable, "规划服务未启用")
		return
	}
	var req plan.Request
	if err := decodeStrictJSON(w, r, &req, 8192, true); err != nil {
		writeDecodeError(w, err, "生成请求必须为单个 JSON 对象（endingTone/avoidTropes/volumeCount/extra）")
		return
	}
	req.EndingTone = strings.TrimSpace(req.EndingTone)
	req.AvoidTropes = strings.TrimSpace(req.AvoidTropes)
	req.Extra = strings.TrimSpace(req.Extra)
	if len([]rune(req.EndingTone)) > 200 || len([]rune(req.AvoidTropes)) > 500 || len([]rune(req.Extra)) > 1000 {
		writeMemoryError(w, http.StatusBadRequest, "invalid_request", "规划要求过长：endingTone≤200、avoidTropes≤500、extra≤1000 字")
		return
	}

	result, err := s.planner.Generate(r.Context(), req)
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, result)
	case errors.Is(err, plan.ErrConfigMissing):
		writeMemoryError(w, http.StatusConflict, "config_missing", "请先在「模型设置」中填写 Base URL 和模型名")
	case errors.Is(err, plan.ErrKnowledgeEmpty):
		writeMemoryError(w, http.StatusConflict, "knowledge_not_ready", "知识库为空：请先完成 /api/analyze")
	case errors.Is(err, plan.ErrOutputInvalid):
		writeMemoryError(w, http.StatusUnprocessableEntity, "invalid_plan_output", "模型未返回可用的计划结构，请重试或调整要求")
	case errors.Is(err, store.ErrMemorySafeState):
		writeMemoryError(w, http.StatusInternalServerError, "memory_safe_state", "记忆子系统处于安全态：拒绝写入")
	case errors.Is(err, store.ErrAnalysisRunning):
		writeMemoryError(w, http.StatusConflict, "analysis_running", "故事分析进行中，请稍后再生成计划")
	case errors.Is(err, store.ErrPlanInvalid):
		writeMemoryError(w, http.StatusUnprocessableEntity, "invalid_plan", err.Error())
	case errors.Is(err, store.ErrOptimisticConflict):
		writeMemoryError(w, http.StatusConflict, "optimistic_conflict", err.Error())
	case r.Context().Err() != nil:
		return
	default:
		s.log.Error("generate plans", "err", err)
		writeMemoryError(w, http.StatusBadGateway, "model_error", "计划生成失败："+shortError(err))
	}
}

// activePlansForContinuation 读取已激活计划；失败只记日志，绝不阻断续写。
func (s *Server) activePlansForContinuation(enabled bool) []novel.StoryPlan {
	if !enabled {
		return nil
	}
	plans, _, err := s.store.LoadActivePlans()
	if err != nil {
		s.log.Warn("load active plans for continuation", "err", err)
		return nil
	}
	return plans
}

func (s *Server) handleGetPlans(w http.ResponseWriter, r *http.Request) {
	tree, err := s.store.LoadPlanTree()
	if err != nil {
		if errors.Is(err, store.ErrMemorySafeState) {
			writeMemoryError(w, http.StatusInternalServerError, "memory_safe_state", "记忆子系统处于安全态：拒绝读取计划")
			return
		}
		s.log.Error("load plan tree", "err", err)
		writeError(w, http.StatusInternalServerError, "读取计划失败")
		return
	}
	writeJSON(w, http.StatusOK, tree)
}

func (s *Server) handlePostPlanActivate(w http.ResponseWriter, r *http.Request) {
	var req struct{}
	if err := decodeStrictJSON(w, r, &req, 4096, true); err != nil {
		writeDecodeError(w, err, "activate 请求体必须为空或 {}")
		return
	}
	s.applyPlanStatus(w, r.PathValue("id"), novel.PlanActive)
}

func (s *Server) handlePostPlanStatus(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Status string `json:"status"`
	}
	if err := decodeStrictJSON(w, r, &req, 4096, false); err != nil {
		writeDecodeError(w, err, "请求体必须为单个 JSON 对象（status）")
		return
	}
	status := novel.StoryPlanStatus(strings.TrimSpace(req.Status))
	switch status {
	case novel.PlanActive, novel.PlanPaused, novel.PlanCompleted, novel.PlanCancelled:
	default:
		writeMemoryError(w, http.StatusBadRequest, "invalid_status", "status 必须为 active|paused|completed|cancelled")
		return
	}
	s.applyPlanStatus(w, r.PathValue("id"), status)
}

func (s *Server) applyPlanStatus(w http.ResponseWriter, id string, status novel.StoryPlanStatus) {
	if !store.ValidPlanID(id) {
		writeMemoryError(w, http.StatusBadRequest, "invalid_plan_id", "计划 ID 必须为 plan_ 前缀")
		return
	}
	result, err := s.store.SetPlanStatus(id, status)
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, result)
	case errors.Is(err, store.ErrPlanNotFound):
		writeMemoryError(w, http.StatusNotFound, "plan_not_found", "计划不存在")
	case errors.Is(err, store.ErrPlanTransition):
		writeMemoryError(w, http.StatusConflict, "plan_transition", "当前状态不允许该变更：请按 proposed→active→paused/completed/cancelled 规则操作")
	case errors.Is(err, store.ErrPlanInvalid):
		writeMemoryError(w, http.StatusUnprocessableEntity, "invalid_plan", err.Error())
	case errors.Is(err, store.ErrMemorySafeState):
		writeMemoryError(w, http.StatusInternalServerError, "memory_safe_state", "记忆子系统处于安全态：拒绝写入")
	case errors.Is(err, store.ErrOptimisticConflict):
		writeMemoryError(w, http.StatusConflict, "optimistic_conflict", err.Error())
	default:
		s.log.Error("set plan status", "err", err, "plan", id, "status", status)
		writeError(w, http.StatusInternalServerError, "更新计划状态失败")
	}
}

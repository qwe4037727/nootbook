package server

import (
	"net/http"
	"unicode/utf8"

	"novelagent/internal/agent"
	"novelagent/internal/novel"
)

// 续写记忆预览（iter3-详设 §7）：只读，不建 LLM 连接、不落盘、不改 memoryRevision。

type recalledCounts struct {
	States     int `json:"states"`
	Events     int `json:"events"`
	Threads    int `json:"threads"`
	Cognitions int `json:"cognitions"`
	Plans      int `json:"plans"`
}

func recalledCountsOf(memory *novel.ContinuationMemory) recalledCounts {
	counts := recalledCounts{}
	if memory == nil {
		return counts
	}
	for _, record := range memory.Records {
		switch record.Kind {
		case novel.MemoryKindWorldState, novel.MemoryKindState:
			counts.States++
		case novel.MemoryKindEvent:
			counts.Events++
		case novel.MemoryKindThread:
			counts.Threads++
		case novel.MemoryKindCognition:
			counts.Cognitions++
		case novel.MemoryKindPlan:
			counts.Plans++
		}
	}
	return counts
}

// retrieveContinuationMemory 在 store.mu 域外执行检索；失败只记日志，绝不阻断续写。
func (s *Server) retrieveContinuationMemory(chapters []novel.Chapter, knowledge novel.Knowledge, contextChars int, enabled bool, useRecall *bool) *novel.ContinuationMemory {
	if !enabled {
		return nil
	}
	recent, startIndex := agent.ContinuationWindowFor(chapters, contextChars, useRecall)
	memory, err := s.store.RetrieveContinuationMemory(knowledge, recent, startIndex, novel.MemoryBudgetChars(contextChars))
	if err != nil {
		s.log.Warn("retrieve continuation memory", "err", err)
	}
	return &memory
}

// recallChaptersPayload 让命中章节在 JSON 里始终是数组而不是 null，前端无需判空。
func recallChaptersPayload(chapters []int) []int {
	if chapters == nil {
		return []int{}
	}
	return chapters
}

// memoryMetaPayload 生成 SSE meta 的记忆字段；memory 为 nil 表示用户关闭注入。
func memoryMetaPayload(memory *novel.ContinuationMemory, memorySection string) map[string]any {
	payload := map[string]any{
		"memoryEnabled":        memorySection != "",
		"memoryRevision":       int64(0),
		"memoryBudgetChars":    0,
		"memoryUsedChars":      0,
		"seeds":                0,
		"recalledByKind":       recalledCounts{},
		"droppedByBudget":      0,
		"skippedReview":        0,
		"skippedAmbiguous":     0,
		"memoryDisabledReason": "",
	}
	if memory == nil {
		return payload
	}
	payload["memoryRevision"] = memory.MemoryRevision
	payload["memoryBudgetChars"] = memory.BudgetChars
	// 报告实际注入量：全量预算收缩可能再裁掉部分记录（I3-7/R2）。
	payload["memoryUsedChars"] = utf8.RuneCountInString(memorySection)
	payload["seeds"] = len(memory.Seeds)
	payload["recalledByKind"] = recalledCountsOf(memory)
	payload["droppedByBudget"] = memory.DroppedByBudget
	payload["skippedReview"] = memory.SkippedReview
	payload["skippedAmbiguous"] = memory.SkippedAmbiguous
	payload["memoryDisabledReason"] = memory.DisabledReason
	return payload
}

type continuePreviewResponse struct {
	MemorySection        string               `json:"memorySection"`
	PlanSection          string               `json:"planSection"`
	Seeds                []novel.MemorySeed   `json:"seeds"`
	Records              []novel.MemoryRecord `json:"records"`
	Budget               int                  `json:"budget"`
	Used                 int                  `json:"used"`
	Counts               recalledCounts       `json:"counts"`
	ManuscriptChars      int                  `json:"manuscriptChars"`
	MemoryEnabled        bool                 `json:"memoryEnabled"`
	PlanEnabled          bool                 `json:"planEnabled"`
	PlanCount            int                  `json:"planCount"`
	MemoryDisabledReason string               `json:"memoryDisabledReason,omitempty"`
	DroppedByBudget      int                  `json:"droppedByBudget"`
	SkippedReview        int                  `json:"skippedReview"`
	SkippedAmbiguous     int                  `json:"skippedAmbiguous"`
	MemoryRevision       int64                `json:"memoryRevision"`
	PromptTotalChars     int                  `json:"promptTotalChars"`
	PromptOverBudget     bool                 `json:"promptOverBudget"`
	PromptSections       map[string]int       `json:"sections"`
	RecallSection        string               `json:"recallSection"`
	RecallEnabled        bool                 `json:"recallEnabled"`
	RecallBudget         int                  `json:"recallBudgetChars"`
	RecallUsed           int                  `json:"recallUsedChars"`
	RecallCount          int                  `json:"recallHits"`
	RecallChapters       []int                `json:"recallChapters"`
}

// handleContinuePreview 覆盖 iter3-详设 §7 的只读预览契约：
// 请求体同 /api/continue（可省略），空体合法；严格单 JSON 对象、4096B 上限。
func (s *Server) handleContinuePreview(w http.ResponseWriter, r *http.Request) {
	var req continueRequest
	if err := decodeStrictJSON(w, r, &req, 4096, true); err != nil {
		writeDecodeError(w, err, "预览请求必须为单个 JSON 对象（direction/targetChars/useMemory）")
		return
	}

	project, err := s.store.LoadProject()
	if err != nil {
		s.log.Error("load project for preview", "err", err)
		writeError(w, http.StatusInternalServerError, "读取作品失败")
		return
	}
	chapters, err := s.store.LoadChapters()
	if err != nil {
		s.log.Error("load chapters for preview", "err", err)
		writeError(w, http.StatusInternalServerError, "读取章节失败")
		return
	}
	knowledge, err := s.store.LoadKnowledge()
	if err != nil {
		s.log.Error("load knowledge for preview", "err", err)
		writeError(w, http.StatusInternalServerError, "读取知识库失败")
		return
	}
	cfg, err := s.store.LoadConfig()
	if err != nil {
		s.log.Error("load config for preview", "err", err)
		writeError(w, http.StatusInternalServerError, "读取模型设置失败")
		return
	}

	useMemory := req.UseMemory == nil || *req.UseMemory
	memory := s.retrieveContinuationMemory(chapters, knowledge, cfg.ContextChars, useMemory, req.UseRecall)
	usePlan := req.UsePlan == nil || *req.UsePlan
	plans := s.activePlansForContinuation(usePlan)
	prompt := agent.Build(agent.Input{
		Title:        project.Title,
		Knowledge:    knowledge,
		Chapters:     chapters,
		Direction:    req.Direction,
		TargetChars:  req.TargetChars,
		ContextChars: cfg.ContextChars,
		Memory:       memory,
		Plans:        plans,
		UseRecall:    req.UseRecall,
	})

	resp := continuePreviewResponse{
		MemorySection:    prompt.MemorySection,
		PlanSection:      prompt.PlanSection,
		Seeds:            []novel.MemorySeed{},
		Records:          []novel.MemoryRecord{},
		ManuscriptChars:  prompt.ContextChars,
		MemoryEnabled:    prompt.MemorySection != "",
		PlanEnabled:      prompt.PlanSection != "",
		PlanCount:        len(plans),
		PromptTotalChars: prompt.TotalChars,
		PromptOverBudget: prompt.OverBudget,
		PromptSections:   prompt.Sections,
		RecallSection:    prompt.RecallSection,
		RecallEnabled:    prompt.RecallSection != "",
		RecallBudget:     prompt.RecallBudget,
		RecallUsed:       utf8.RuneCountInString(prompt.RecallSection),
		RecallCount:      len(prompt.RecallChapters),
		RecallChapters:   recallChaptersPayload(prompt.RecallChapters),
	}
	if memory != nil {
		resp.Budget = memory.BudgetChars
		resp.Used = utf8.RuneCountInString(prompt.MemorySection)
		resp.Counts = recalledCountsOf(memory)
		resp.DroppedByBudget = memory.DroppedByBudget
		resp.SkippedReview = memory.SkippedReview
		resp.SkippedAmbiguous = memory.SkippedAmbiguous
		resp.MemoryDisabledReason = memory.DisabledReason
		resp.MemoryRevision = memory.MemoryRevision
		if memory.Seeds != nil {
			resp.Seeds = memory.Seeds
		}
		if memory.Records != nil {
			resp.Records = memory.Records
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

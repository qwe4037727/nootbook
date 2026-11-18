package novel

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"
)

// 续写「计划对齐」段（F23）。计划只有走向建议效力，不得覆盖已确认事实；
// 仅注入 active 且无需复核的计划。

const (
	PlanSectionHeader = "【计划对齐（仅约束走向，不得覆盖已确认事实）】"
	planSectionBudget = 1200
	planSectionLimit  = 8
)

// RenderPlanSection 渲染计划对齐段；无可用计划时返回空串。
// 返回文本以 "\n【计划对齐…】" 开头，可直接拼接到 user prompt。
func RenderPlanSection(plans []StoryPlan) string {
	candidates := make([]StoryPlan, 0, len(plans))
	for _, plan := range plans {
		if plan.Status == PlanActive && !plan.NeedsReview {
			candidates = append(candidates, plan)
		}
	}
	if len(candidates) == 0 {
		return ""
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		ri, rj := planLevelOrder(candidates[i].Level), planLevelOrder(candidates[j].Level)
		if ri != rj {
			return ri < rj
		}
		if !candidates[i].CreatedAt.Equal(candidates[j].CreatedAt) {
			return candidates[i].CreatedAt.Before(candidates[j].CreatedAt)
		}
		return candidates[i].ID < candidates[j].ID
	})

	var body strings.Builder
	used := utf8.RuneCountInString(PlanSectionHeader) + 2 // 前后换行
	for i, plan := range candidates {
		if i >= planSectionLimit {
			break
		}
		block := renderPlanBlock(plan)
		if block == "" {
			continue
		}
		cost := utf8.RuneCountInString(block)
		if used+cost > planSectionBudget {
			if body.Len() == 0 {
				body.WriteString(clipPlanRunes(block, planSectionBudget-used))
			}
			break
		}
		body.WriteString(block)
		used += cost
	}
	if body.Len() == 0 {
		return ""
	}
	return "\n" + PlanSectionHeader + "\n" + body.String()
}

// PlanSectionChars 返回渲染后的 rune 长度（预算/测试计量口径）。
func PlanSectionChars(plans []StoryPlan) int {
	return utf8.RuneCountInString(RenderPlanSection(plans))
}

func renderPlanBlock(plan StoryPlan) string {
	title := strings.TrimSpace(plan.Title)
	if title == "" {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "- %s《%s》", planLevelLabel(plan.Level), clipPlanRunes(title, 80))
	if objective := strings.TrimSpace(plan.Objective); objective != "" {
		fmt.Fprintf(&b, "｜目标：%s", clipPlanRunes(objective, 160))
	}
	b.WriteString("\n")
	if constraints := compactPlanList(plan.Constraints, 4, 60); constraints != "" {
		fmt.Fprintf(&b, "  约束：%s\n", constraints)
	}
	if beats := compactPlanList(plan.CandidateBeats, 5, 70); beats != "" {
		fmt.Fprintf(&b, "  候选发展：%s\n", beats)
	}
	return b.String()
}

func compactPlanList(items []string, limit, perItem int) string {
	parts := make([]string, 0, limit)
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		parts = append(parts, clipPlanRunes(item, perItem))
		if len(parts) >= limit {
			break
		}
	}
	return strings.Join(parts, "；")
}

func planLevelLabel(level string) string {
	switch StoryPlanLevel(level) {
	case PlanLevelGlobal:
		return "全书"
	case PlanLevelVolume:
		return "分卷"
	case PlanLevelNearTerm:
		return "近期"
	}
	return "计划"
}

func planLevelOrder(level string) int {
	switch StoryPlanLevel(level) {
	case PlanLevelGlobal:
		return 0
	case PlanLevelVolume:
		return 1
	case PlanLevelNearTerm:
		return 2
	}
	return 3
}

func clipPlanRunes(text string, limit int) string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) <= limit {
		return string(runes)
	}
	return string(runes[:limit]) + "…"
}

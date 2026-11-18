package novel

// R4 长篇召回策略：默认关闭，章节数与正文预算同时达标时自动启用。
const (
	RecallMinChapters   = 20
	RecallDefaultBudget = 800
	recallBudgetDivisor = 4
)

// RecallBudgetChars 返回召回段硬上限：min(800, ContextChars/4)。
// 若扣掉记忆与召回后正文窗口低于 ContextChars/2，则退化为 0，
// 保证召回不会挤占正文硬下限（与 I3-7 的预算不变量一致）。
func RecallBudgetChars(contextChars int) int {
	if contextChars <= 0 {
		contextChars = DefaultContextChars
	}
	if contextChars <= memoryBudgetFloor {
		return 0
	}
	budget := contextChars / recallBudgetDivisor
	if budget > RecallDefaultBudget {
		budget = RecallDefaultBudget
	}
	manuscript := contextChars - MemoryBudgetChars(contextChars) - budget
	if manuscript < contextChars/2 {
		return 0
	}
	return budget
}

// RecallEnabled 报告是否按策略自动启用 R4。
func RecallEnabled(chapterCount, contextChars int) bool {
	return chapterCount >= RecallMinChapters && RecallBudgetChars(contextChars) > 0
}

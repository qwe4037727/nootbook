package novel

import (
	"strings"
	"unicode/utf8"
)

// 续写记忆上下文 DTO 与预算/渲染（iter3-详设 §2/§4/§5）。
// 检索由 store 填充，agent 只做插入；渲染函数同时供 store 精确计量预算，
// 保证「预算裁剪」与实际注入文本逐字符一致。

const DefaultContextChars = 12000

const (
	defaultMemoryBudget = 3000
	minMemoryBudget     = 800
	memoryBudgetDivisor = 4
	memoryBudgetFloor   = 1600
)

// MemoryBudgetChars 返回记忆段硬上限：clamp(ContextChars/4, 800, 3000)；
// ContextChars ≤ 1600 时退化为 0（iter3-详设 §4，保护小窗口配置）。
func MemoryBudgetChars(contextChars int) int {
	if contextChars <= 0 {
		contextChars = DefaultContextChars
	}
	if contextChars <= memoryBudgetFloor {
		return 0
	}
	budget := contextChars / memoryBudgetDivisor
	if budget < minMemoryBudget {
		budget = minMemoryBudget
	}
	if budget > defaultMemoryBudget {
		budget = defaultMemoryBudget
	}
	return budget
}

// ManuscriptWindowChars 返回正文窗口 = 总预算 − 记忆预算（扣减语义，冻结）。
func ManuscriptWindowChars(contextChars int) int {
	if contextChars <= 0 {
		contextChars = DefaultContextChars
	}
	return contextChars - MemoryBudgetChars(contextChars)
}

const (
	MemoryKindWorldState = "world_state"
	MemoryKindState      = "state"
	MemoryKindEvent      = "event"
	MemoryKindThread     = "thread"
	MemoryKindFuse       = "fuse"
	MemoryKindCognition  = "cognition"
	MemoryKindPlan       = "plan"

	MemoryPriorityP0 = "P0"
	MemoryPriorityP1 = "P1"
)

const (
	MemoryDisabledSafeState = "safe_state"
	MemoryDisabledReadError = "read_error"
	MemoryDisabledAbsent    = "memory_absent"
)

type MemorySeed struct {
	EntityID   string  `json:"entityId"`
	EntityName string  `json:"entityName"`
	Score      float64 `json:"score"`
	Reason     string  `json:"reason"`
}

type MemoryRecord struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	Subject  string `json:"subject"`
	Text     string `json:"text"`
	Order    int    `json:"order"`
	Priority string `json:"priority"`
}

type ContinuationMemory struct {
	Seeds            []MemorySeed   `json:"seeds"`
	Records          []MemoryRecord `json:"records"`
	BudgetChars      int            `json:"budgetChars"`
	UsedChars        int            `json:"usedChars"`
	DroppedByBudget  int            `json:"droppedByBudget"`
	SkippedReview    int            `json:"skippedReview"`
	SkippedAmbiguous int            `json:"skippedAmbiguous"`
	DisabledReason   string         `json:"disabledReason,omitempty"`
	// MemoryRevision 为本次检索所基于的记忆版本（SSE meta 用；预览接口不落盘）。
	MemoryRevision int64 `json:"memoryRevision"`
}

// 子段预算与固定顺位（iter3-详设 §4）。计划子段在原表缺失，补 300 以保持总量可控。
const (
	memoryWorldStateBudget = 600
	memoryStateBudget      = 1000
	memoryCognitionBudget  = 400
	memoryThreadBudget     = 600
	memoryFuseBudget       = 300
	memoryEventBudget      = 400
	memoryPlanBudget       = 300
)

var memorySectionOrder = []string{
	MemoryKindWorldState,
	MemoryKindState,
	MemoryKindCognition,
	MemoryKindThread,
	MemoryKindFuse,
	MemoryKindEvent,
	MemoryKindPlan,
}

var memoryGroupHeaders = map[string]string{
	MemoryKindWorldState: "世界规则：\n",
	MemoryKindState:      "人物当前状态：\n",
	MemoryKindCognition:  "人物认知：\n",
	MemoryKindThread:     "未解决伏笔：\n",
	MemoryKindFuse:       "待回收伏笔（A3 台账，含待确认）：\n",
	MemoryKindEvent:      "关键事件（近因优先）：\n",
	MemoryKindPlan:       "近期计划（仅约束走向，不得覆盖已确认事实）：\n",
}

const memorySectionHeader = "【已确认剧情记忆（来自正文抽取，效力低于下方最近正文）】\n"

// MemorySectionKinds 返回渲染顺序（预算裁剪时也按此顺序组装 Records）。
func MemorySectionKinds() []string {
	return append([]string(nil), memorySectionOrder...)
}

// MemoryGroupHeader 返回子段固定标题（含换行）；不存在时返回空串。
func MemoryGroupHeader(kind string) string {
	return memoryGroupHeaders[kind]
}

func MemorySubBudget(kind string) int {
	switch kind {
	case MemoryKindWorldState:
		return memoryWorldStateBudget
	case MemoryKindState:
		return memoryStateBudget
	case MemoryKindCognition:
		return memoryCognitionBudget
	case MemoryKindThread:
		return memoryThreadBudget
	case MemoryKindFuse:
		return memoryFuseBudget
	case MemoryKindEvent:
		return memoryEventBudget
	case MemoryKindPlan:
		return memoryPlanBudget
	}
	return 0
}

// RenderMemoryGroup 渲染单一子段（含固定标题）；store 用其精确计量子预算。
func RenderMemoryGroup(kind string, records []MemoryRecord) string {
	header, ok := memoryGroupHeaders[kind]
	if !ok || len(records) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(header)
	for _, record := range records {
		text := strings.TrimSpace(record.Text)
		if text == "" {
			continue
		}
		b.WriteString("- ")
		b.WriteString(text)
		b.WriteString("\n")
	}
	return b.String()
}

// RenderMemorySection 渲染完整记忆段；DisabledReason 非空或无记录时返回空串。
// 返回文本以 "\n【已确认剧情记忆…】" 开头，可直接拼接到 user prompt。
func RenderMemorySection(m *ContinuationMemory) string {
	if m == nil || m.DisabledReason != "" || m.BudgetChars <= 0 || len(m.Records) == 0 {
		return ""
	}
	groups := make(map[string][]MemoryRecord, len(memorySectionOrder))
	for _, record := range m.Records {
		if _, ok := memoryGroupHeaders[record.Kind]; !ok {
			continue
		}
		groups[record.Kind] = append(groups[record.Kind], record)
	}
	var b strings.Builder
	for _, kind := range memorySectionOrder {
		group := RenderMemoryGroup(kind, groups[kind])
		if group == "" {
			continue
		}
		if b.Len() == 0 {
			b.WriteString("\n")
			b.WriteString(memorySectionHeader)
		}
		b.WriteString(group)
	}
	return b.String()
}

// MemorySectionChars 返回渲染后的 rune 长度（预算计量口径）。
func MemorySectionChars(m *ContinuationMemory) int {
	return utf8.RuneCountInString(RenderMemorySection(m))
}

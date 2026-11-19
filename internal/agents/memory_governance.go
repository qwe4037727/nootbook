package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"novelagent/internal/agentruntime"
	"novelagent/internal/novel"
	"novelagent/internal/store"
)

// MemoryAgent（A4 记忆治理）：周期性产出记忆合并 / 过期 / 知识库健康提案，
// 全部进入审阅队列（agent_proposal），从不自动删除或改写词条。
type MemoryAgent struct{}

func (a *MemoryAgent) Key() string  { return "memory" }
func (a *MemoryAgent) Name() string { return "A4 记忆治理" }

func (a *MemoryAgent) Triggers() []string {
	return []string{string(store.ChapterCreated), string(store.ChapterSaved), string(store.ChaptersImported)}
}

func (a *MemoryAgent) Run(ctx context.Context, ev agentruntime.Event, in agentruntime.Input) (agentruntime.Report, error) {
	report := agentruntime.Report{Severity: "info"}
	if in.Store == nil {
		return report, fmt.Errorf("memory: store is required")
	}
	ctx2, err := loadProjectContext(in.Store)
	if err != nil {
		return report, fmt.Errorf("memory: load context: %w", err)
	}

	chapterIndex := make(map[string]int, len(ctx2.project.Chapters))
	for _, chapter := range ctx2.project.Chapters {
		chapterIndex[chapter.ID] = chapter.Index
	}
	currentIndex := ev.Index
	if currentIndex == 0 {
		currentIndex = len(ctx2.project.Chapters)
	}

	proposals := a.mergeProposals(ctx2)
	proposals = append(proposals, a.expireProposals(ctx2, chapterIndex, currentIndex)...)
	report.Proposals = proposals
	saved := 0
	for _, proposal := range proposals {
		if _, created, err := in.Store.SaveAgentProposal(store.AgentProposalRecord{
			AgentKey: a.Key(),
			Kind:     proposal.Kind,
			TargetID: proposal.Target,
			Summary:  proposal.Summary,
			Risk:     proposal.Risk,
			Payload:  proposal.Payload,
		}); err == nil && created {
			saved++
		}
	}

	findings, warningCount, staleCount := a.healthFindings(ctx2, chapterIndex, currentIndex)
	report.Findings = findings
	if warningCount > 0 {
		report.Severity = "warning"
	}

	report.Summary = fmt.Sprintf("记忆体检：事件 %d · 状态 %d · 认知 %d · 伏笔 %d；新增提案 %d 条（合并/过期），待复核 %d 项，久未确认状态 %d 项。",
		len(ctx2.events), len(ctx2.states), len(ctx2.cogs), len(ctx2.threads), saved, warningCount, staleCount)
	report.Details = []string{
		fmt.Sprintf("知识库：%d 个角色，%d 条世界设定", len(ctx2.knowledge.Characters), len(ctx2.knowledge.World)),
		"治理动作仅生成提案，不自动删除/合并词条。",
	}
	return report, nil
}

// mergeProposals 找出同主体同属性、取值不同的 active 状态，生成合并提案。
func (a *MemoryAgent) mergeProposals(ctx2 projectContext) []agentruntime.Proposal {
	type groupKey struct{ subject, attribute string }
	groups := make(map[groupKey][]novel.StoryState)
	for _, state := range ctx2.states {
		if state.Status != novel.StateActive {
			continue
		}
		key := groupKey{state.SubjectID, strings.TrimSpace(state.Attribute)}
		groups[key] = append(groups[key], state)
	}
	out := make([]agentruntime.Proposal, 0)
	for _, states := range groups {
		if len(states) < 2 {
			continue
		}
		values := make(map[string]struct{}, len(states))
		ids := make([]string, 0, len(states))
		for _, state := range states {
			values[strings.TrimSpace(state.Value)] = struct{}{}
			ids = append(ids, state.ID)
		}
		if len(values) < 2 {
			continue
		}
		payload, _ := json.Marshal(map[string]any{
			"subjectId": states[0].SubjectID,
			"attribute": states[0].Attribute,
			"stateIds":  ids,
			"values":    keysOf(values),
		})
		out = append(out, agentruntime.Proposal{
			Kind:    "merge_state",
			Target:  states[0].ID,
			Summary: fmt.Sprintf("状态「%s」存在 %d 条不一致取值，建议合并", states[0].Attribute, len(values)),
			Risk:    "low",
			Payload: payload,
		})
	}
	return out
}

// expireProposals 找出连续 N 章未再确认的 active 状态，建议过期（人工审批后才执行）。
func (a *MemoryAgent) expireProposals(ctx2 projectContext, chapterIndex map[string]int, currentIndex int) []agentruntime.Proposal {
	const staleGap = 3
	out := make([]agentruntime.Proposal, 0)
	if currentIndex < staleGap {
		return out
	}
	for _, state := range ctx2.states {
		if state.Status != novel.StateActive || state.LastConfirmedChapter == "" {
			continue
		}
		confirmed, ok := chapterIndex[state.LastConfirmedChapter]
		if !ok || confirmed > currentIndex-staleGap {
			continue
		}
		payload, _ := json.Marshal(map[string]any{
			"stateId":              state.ID,
			"lastConfirmedChapter": state.LastConfirmedChapter,
			"lastConfirmedIndex":   confirmed,
			"currentIndex":         currentIndex,
		})
		out = append(out, agentruntime.Proposal{
			Kind:    "expire_state",
			Target:  state.ID,
			Summary: fmt.Sprintf("状态「%s=%s」已 %d 章未确认，建议标记过期", state.Attribute, state.Value, currentIndex-confirmed),
			Risk:    "low",
			Payload: payload,
		})
	}
	return out
}

// healthFindings 产出知识库健康报告：低置信词条、待复核记忆、久未推进的伏笔。
func (a *MemoryAgent) healthFindings(ctx2 projectContext, chapterIndex map[string]int, currentIndex int) ([]agentruntime.Finding, int, int) {
	findings := make([]agentruntime.Finding, 0, 8)
	warnings := 0
	stale := 0
	for _, character := range ctx2.knowledge.Characters {
		if character.Confidence > 0 && character.Confidence < 0.4 {
			warnings++
			findings = append(findings, agentruntime.Finding{
				Type:       "kb_low_confidence",
				Severity:   "warning",
				EntryID:    character.ID,
				Detail:     fmt.Sprintf("角色「%s」置信度 %.2f 偏低，建议补证或复核。", character.Name, character.Confidence),
				Confidence: character.Confidence,
			})
		}
	}
	needsReview := 0
	for _, record := range collectReviewFlags(ctx2) {
		if record {
			needsReview++
		}
	}
	if needsReview > 0 {
		warnings++
		findings = append(findings, agentruntime.Finding{
			Type:     "memory_needs_review",
			Severity: "warning",
			Detail:   fmt.Sprintf("%d 条记忆处于待复核状态，建议优先处理。", needsReview),
		})
	}
	const threadStaleGap = 10
	for _, thread := range ctx2.threads {
		if thread.Status != novel.ThreadOpen && thread.Status != novel.ThreadDeveloping {
			continue
		}
		for _, ref := range thread.SourceRefs {
			index, ok := chapterIndex[ref.ChapterID]
			if ok && index > 0 && currentIndex-index >= threadStaleGap {
				stale++
				findings = append(findings, agentruntime.Finding{
					Type:         "thread_stalled",
					Severity:     "info",
					EntryID:      thread.ID,
					ChapterID:    ref.ChapterID,
					ChapterIndex: index,
					Quote:        ref.Quote,
					Detail:       fmt.Sprintf("伏笔「%s」已 %d 章未推进，可考虑回收或标记放弃。", thread.Title, currentIndex-index),
					Evidence:     []agentruntime.Evidence{{ChapterID: ref.ChapterID, ChapterIndex: index, Quote: ref.Quote}},
				})
				break
			}
		}
	}
	if len(findings) > 20 {
		findings = findings[:20]
	}
	return findings, warnings, stale
}

func collectReviewFlags(ctx2 projectContext) []bool {
	flags := make([]bool, 0, len(ctx2.events)+len(ctx2.states)+len(ctx2.cogs)+len(ctx2.threads))
	for _, event := range ctx2.events {
		flags = append(flags, event.NeedsReview)
	}
	for _, state := range ctx2.states {
		flags = append(flags, state.NeedsReview)
	}
	for _, cog := range ctx2.cogs {
		flags = append(flags, cog.NeedsReview)
	}
	for _, thread := range ctx2.threads {
		flags = append(flags, thread.NeedsReview)
	}
	for _, plan := range ctx2.plans {
		flags = append(flags, plan.NeedsReview)
	}
	return flags
}

func keysOf(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for key := range set {
		out = append(out, key)
	}
	return out
}

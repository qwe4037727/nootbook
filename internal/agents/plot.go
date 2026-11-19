package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"novelagent/internal/agentruntime"
	"novelagent/internal/llm"
	"novelagent/internal/novel"
	"novelagent/internal/store"
)

// PlotAgent（A2 剧情推演）：检查主线推进与未解决伏笔，并用模型给出下一步走向建议；
// 建议只进入报告/审阅提案，绝不改正文。
type PlotAgent struct{}

func (a *PlotAgent) Key() string  { return "plot" }
func (a *PlotAgent) Name() string { return "A2 剧情推演" }

func (a *PlotAgent) Triggers() []string {
	return []string{
		string(store.ChapterCreated), string(store.ChapterSaved),
		string(store.ChaptersImported), string(store.ChaptersAppended),
	}
}

func (a *PlotAgent) Run(ctx context.Context, ev agentruntime.Event, in agentruntime.Input) (agentruntime.Report, error) {
	report := agentruntime.Report{Severity: "info"}
	if in.Store == nil {
		return report, fmt.Errorf("plot: store is required")
	}
	pc, err := loadProjectContext(in.Store)
	if err != nil {
		return report, fmt.Errorf("plot: load context: %w", err)
	}
	chapter, ok := chapterForAgent(in.Store, ev, pc)
	if !ok {
		report.Summary = "暂无章节可推演。"
		return report, nil
	}
	index := chapter.Index
	findings := a.scan(pc, chapter)

	if in.Configured && in.Client != nil && strings.TrimSpace(chapter.Content) != "" {
		beats, promptTokens, completionTokens, total, llmErr := a.deduce(ctx, in, pc, chapter)
		report.PromptTokens += promptTokens
		report.CompletionTokens += completionTokens
		report.Tokens += total
		if llmErr != nil {
			report.Details = append(report.Details, "模型推演未完成："+llmErr.Error())
		}
		if len(beats) > 0 {
			findings = append(findings, agentruntime.Finding{
				Type:         "plot_next_beats",
				Severity:     "info",
				ChapterID:    chapter.ID,
				ChapterIndex: index,
				Detail:       "模型给出的后续走向建议（仅参考，不自动写入）：",
				Options:      beats,
			})
			if canPropose(in) {
				payload, _ := json.Marshal(map[string]any{"chapterId": chapter.ID, "beats": beats})
				_, _, _ = in.Store.SaveAgentProposal(store.AgentProposalRecord{
					AgentKey: a.Key(),
					Kind:     "plot_beats",
					TargetID: chapter.ID,
					Summary:  fmt.Sprintf("第%d章后续走向建议", index),
					Risk:     "low",
					Payload:  payload,
				})
			}
		}
	}

	report.Findings = findings
	report.Severity = severityOf(severityList(findings)...)
	report.Summary = fmt.Sprintf("第%d章剧情推演：%d 项观察，开放伏笔 %d 条。", index, len(findings), openThreadCount(pc))
	report.Details = append(report.Details,
		fmt.Sprintf("权限：%s（只出报告/提案，不改正文）", in.Permission),
	)
	return report, nil
}

func (a *PlotAgent) scan(pc projectContext, chapter novel.Chapter) []agentruntime.Finding {
	findings := make([]agentruntime.Finding, 0, 6)
	index := chapter.Index
	for _, thread := range pc.threads {
		if thread.Status != novel.ThreadOpen && thread.Status != novel.ThreadDeveloping {
			continue
		}
		last := threadLastChapter(pc, thread)
		if last == 0 || index-last < 8 {
			continue
		}
		findings = append(findings, agentruntime.Finding{
			Type:         "plot_thread_stale",
			Severity:     "warning",
			ChapterID:    chapter.ID,
			ChapterIndex: index,
			Detail:       fmt.Sprintf("伏笔「%s」已 %d 章未推进（最近第%d章），建议安排回收或明确搁置。", thread.Title, index-last, last),
			Options:      []string{"本章推进", "安排回收", "标记搁置"},
		})
		if len(findings) >= 5 {
			break
		}
	}

	hasConflict := false
	for _, event := range pc.events {
		if event.ChapterID == chapter.ID && event.EventType == string(novel.EventConflict) && event.Status != novel.EventInvalidated {
			hasConflict = true
			break
		}
	}
	if !hasConflict && len([]rune(chapter.Content)) >= 400 {
		findings = append(findings, agentruntime.Finding{
			Type:         "plot_static",
			Severity:     "info",
			ChapterID:    chapter.ID,
			ChapterIndex: index,
			Detail:       "本章篇幅不短，但未登记冲突类事件；若有意铺垫可忽略。",
		})
	}

	for _, plan := range pc.plans {
		if plan.Status != novel.PlanActive || plan.Level != string(novel.PlanLevelNearTerm) || len(plan.CandidateBeats) == 0 {
			continue
		}
		findings = append(findings, agentruntime.Finding{
			Type:         "plot_plan_hint",
			Severity:     "info",
			ChapterID:    chapter.ID,
			ChapterIndex: index,
			Detail:       fmt.Sprintf("近期计划「%s」仍有候选桥段，可对照本章走向。", plan.Title),
			Options:      plan.CandidateBeats,
		})
		break
	}
	return findings
}

func openThreadCount(pc projectContext) int {
	count := 0
	for _, thread := range pc.threads {
		if thread.Status == novel.ThreadOpen || thread.Status == novel.ThreadDeveloping {
			count++
		}
	}
	return count
}

// threadLastChapter 取伏笔关联事件或来源章节中最大的章节序号。
func threadLastChapter(pc projectContext, thread novel.StoryThread) int {
	last := chapterIndexOf(pc, thread.Origin.Chapter)
	for _, eventID := range thread.RelatedEvents {
		for _, event := range pc.events {
			if event.ID == eventID {
				if index := chapterIndexOf(pc, event.ChapterID); index > last {
					last = index
				}
			}
		}
	}
	return last
}

func severityList(findings []agentruntime.Finding) []string {
	out := make([]string, 0, len(findings))
	for _, finding := range findings {
		out = append(out, finding.Severity)
	}
	return out
}

const plotDeductionPrompt = `你是长篇小说剧情推演助手。请只依据已给出的记忆与最新章节，给出 3 条“下一步走向”建议。
只输出 JSON 数组，元素格式：{"beat":"一句话走向","rationale":"与现有伏笔/人物动机的关联"}。
不要输出 Markdown，不要续写正文，不要修改既有事实。`

func (a *PlotAgent) deduce(ctx context.Context, in agentruntime.Input, pc projectContext, chapter novel.Chapter) ([]string, int, int, int, error) {
	digest := buildMemoryDigest(pc, 2500)
	messages := []llm.Message{
		{Role: "system", Content: plotDeductionPrompt},
		{Role: "user", Content: "记忆摘要：\n" + digest + "\n\n最新章节（第" + fmt.Sprint(chapter.Index) + "章）：\n" + compactExcerpt(chapter.Content, 2500)},
	}
	result, err := in.Client.CompleteChat(ctx, messages, 0.7)
	if err != nil {
		return nil, 0, 0, 0, err
	}
	beats := parsePlotBeats(result.Content)
	return beats, result.PromptTokens, result.CompletionTokens, result.TotalTokens, nil
}

func parsePlotBeats(content string) []string {
	start := strings.Index(content, "[")
	end := strings.LastIndex(content, "]")
	if start < 0 || end <= start {
		return nil
	}
	var raw []struct {
		Beat      string `json:"beat"`
		Rationale string `json:"rationale"`
	}
	if err := json.Unmarshal([]byte(content[start:end+1]), &raw); err != nil {
		return nil
	}
	out := make([]string, 0, 3)
	for _, item := range raw {
		beat := strings.TrimSpace(item.Beat)
		if beat == "" {
			continue
		}
		if rationale := strings.TrimSpace(item.Rationale); rationale != "" {
			beat += "（" + truncateRunes(rationale, 60) + "）"
		}
		out = append(out, truncateRunes(beat, 140))
		if len(out) >= 3 {
			break
		}
	}
	return out
}

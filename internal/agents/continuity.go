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

// ContinuityAgent（A1 世界监修）：章节写入后只读扫描知识库/记忆，产出带正文证据的
// 冲突报告；权限默认 observe。它只生成 agent_proposal，绝不修改正文或词条。
type ContinuityAgent struct{}

func (a *ContinuityAgent) Key() string  { return "continuity" }
func (a *ContinuityAgent) Name() string { return "A1 世界监修" }

func (a *ContinuityAgent) Triggers() []string {
	return []string{string(store.ChapterCreated), string(store.ChapterSaved), string(store.ChaptersImported)}
}

func (a *ContinuityAgent) Run(ctx context.Context, ev agentruntime.Event, in agentruntime.Input) (agentruntime.Report, error) {
	report := agentruntime.Report{}
	if in.Store == nil {
		return report, fmt.Errorf("continuity: store is required")
	}
	ctx2, err := loadProjectContext(in.Store)
	if err != nil {
		return report, fmt.Errorf("continuity: load context: %w", err)
	}
	chapter, hasChapter := currentChapter(in.Store, ev)
	if !hasChapter && len(ctx2.project.Chapters) == 0 {
		report.Summary = "暂无章节可扫描。"
		report.Severity = "info"
		return report, nil
	}
	content := chapter.Content
	chapterID := chapter.ID
	index := ev.Index
	if index == 0 {
		index = chapterIndexOf(ctx2, chapterID)
	}
	if index == 0 && len(ctx2.project.Chapters) > 0 {
		last := ctx2.project.Chapters[len(ctx2.project.Chapters)-1]
		index, chapterID = last.Index, last.ID
		if content == "" {
			if loaded, err := in.Store.LoadChapter(last.ID); err == nil {
				content = loaded.Content
			}
		}
	}

	findings := a.scanKnowledge(ctx2, content, chapterID, index)
	if in.Configured && in.Client != nil && strings.TrimSpace(content) != "" {
		llmFindings, promptTokens, completionTokens, total, llmErr := llmAudit(ctx, in, ctx2, content, chapterID, index)
		if llmErr != nil {
			report.Details = append(report.Details, "模型复核未完成："+llmErr.Error())
		}
		report.PromptTokens += promptTokens
		report.CompletionTokens += completionTokens
		report.Tokens += total
		findings = append(findings, llmFindings...)
	}
	if len(findings) > 20 {
		findings = findings[:20]
	}
	report.Findings = findings
	report.Severity = "info"
	if len(findings) > 0 {
		blocking, warning := 0, 0
		levels := make([]string, 0, len(findings))
		for _, finding := range findings {
			switch finding.Severity {
			case "blocking":
				blocking++
			case "warning":
				warning++
			}
			levels = append(levels, finding.Severity)
			a.saveProposal(in, finding)
		}
		report.Severity = severityOf(levels...)
		report.Summary = fmt.Sprintf("扫描第 %d 章：发现 %d 项待复核（阻塞 %d / 警告 %d）。", index, len(findings), blocking, warning)
	} else {
		report.Summary = fmt.Sprintf("扫描第 %d 章：未发现知识库冲突。", index)
	}
	report.Details = append(report.Details,
		fmt.Sprintf("角色 %d · 世界设定 %d · 状态 %d · 事件 %d", len(ctx2.knowledge.Characters), len(ctx2.knowledge.World), len(ctx2.states), len(ctx2.events)),
		"权限：observe（只出报告与审阅提案，不改正文/词条）",
	)
	return report, nil
}

// scanKnowledge 执行确定性检查：低置信实体、待复核状态、同主体状态冲突。
func (a *ContinuityAgent) scanKnowledge(ctx2 projectContext, content, chapterID string, index int) []agentruntime.Finding {
	findings := make([]agentruntime.Finding, 0, 8)
	content = strings.TrimSpace(content)
	if content == "" {
		return findings
	}
	mentionedCharacters := make(map[string]novel.Character)
	for _, character := range ctx2.knowledge.Characters {
		if !mentionsAny(content, character.Name, character.Aliases) {
			continue
		}
		mentionedCharacters[character.ID] = character
		quote := quoteAround(content, character.Name, 80)
		if character.Confidence > 0 && character.Confidence < 0.5 {
			findings = append(findings, agentruntime.Finding{
				Type:         "low_confidence_entity",
				Severity:     "warning",
				EntryID:      character.ID,
				ChapterID:    chapterID,
				ChapterIndex: index,
				Quote:        quote,
				Detail:       fmt.Sprintf("角色「%s」置信度仅 %.2f，正文已使用该角色，建议人工确认。", character.Name, character.Confidence),
				Confidence:   character.Confidence,
				Evidence:     []agentruntime.Evidence{{ChapterID: chapterID, ChapterIndex: index, Quote: quote}},
			})
		}
	}
	for _, entry := range ctx2.knowledge.World {
		if !mentionsAny(content, entry.Title, entry.Aliases) {
			continue
		}
		quote := quoteAround(content, entry.Title, 80)
		if entry.Confidence > 0 && entry.Confidence < 0.5 {
			findings = append(findings, agentruntime.Finding{
				Type:         "low_confidence_entity",
				Severity:     "warning",
				EntryID:      entry.ID,
				ChapterID:    chapterID,
				ChapterIndex: index,
				Quote:        quote,
				Detail:       fmt.Sprintf("世界设定「%s」置信度仅 %.2f，正文已使用该设定，建议人工确认。", entry.Title, entry.Confidence),
				Confidence:   entry.Confidence,
				Evidence:     []agentruntime.Evidence{{ChapterID: chapterID, ChapterIndex: index, Quote: quote}},
			})
		}
	}

	for _, state := range ctx2.states {
		if state.Status != novel.StateActive || !state.NeedsReview {
			continue
		}
		if _, ok := mentionedCharacters[state.SubjectID]; !ok {
			continue
		}
		character := mentionedCharacters[state.SubjectID]
		evidence := make([]agentruntime.Evidence, 0, 2)
		if quote := quoteAround(content, character.Name, 80); quote != "" {
			evidence = append(evidence, agentruntime.Evidence{ChapterID: chapterID, ChapterIndex: index, Quote: quote})
		}
		for _, ref := range state.SourceRefs {
			evidence = append(evidence, agentruntime.Evidence{
				ChapterID:    ref.ChapterID,
				ChapterIndex: chapterIndexOf(ctx2, ref.ChapterID),
				Quote:        ref.Quote,
			})
		}
		findings = append(findings, agentruntime.Finding{
			Type:         "stale_memory",
			Severity:     "blocking",
			EntryID:      state.ID,
			ChapterID:    chapterID,
			ChapterIndex: index,
			Quote:        quoteAround(content, character.Name, 80),
			ConflictWith: state.LastConfirmedChapter,
			Detail:       fmt.Sprintf("角色「%s」的状态「%s=%s」处于待复核（%s），本章继续使用该状态，请先复核记忆。", character.Name, state.Attribute, state.Value, state.ReviewReason),
			Evidence:     evidence,
		})
	}

	// 同主体同属性存在多条 active 状态且取值不同 → 记忆侧冲突（交 A4 合并审阅）。
	type stateKey struct{ subject, attribute string }
	seen := make(map[stateKey]novel.StoryState)
	for _, state := range ctx2.states {
		if state.Status != novel.StateActive {
			continue
		}
		if _, ok := mentionedCharacters[state.SubjectID]; !ok {
			continue
		}
		key := stateKey{state.SubjectID, state.Attribute}
		prev, ok := seen[key]
		if !ok {
			seen[key] = state
			continue
		}
		if strings.TrimSpace(prev.Value) == strings.TrimSpace(state.Value) {
			continue
		}
		character := mentionedCharacters[state.SubjectID]
		findings = append(findings, agentruntime.Finding{
			Type:         "conflicting_states",
			Severity:     "warning",
			EntryID:      state.ID,
			ChapterID:    chapterID,
			ChapterIndex: index,
			Quote:        quoteAround(content, character.Name, 80),
			ConflictWith: prev.ID,
			Detail:       fmt.Sprintf("角色「%s」的「%s」同时存在「%s」与「%s」两条状态记录，建议合并。", character.Name, state.Attribute, prev.Value, state.Value),
			Options:      []string{"保留最新状态", "保留两条", "标记待复核"},
			Evidence:     []agentruntime.Evidence{{ChapterID: chapterID, ChapterIndex: index, Quote: quoteAround(content, character.Name, 80)}},
		})
		seen[key] = state
	}
	return findings
}

func (a *ContinuityAgent) saveProposal(in agentruntime.Input, finding agentruntime.Finding) {
	if finding.Severity != "blocking" && finding.Type != "conflicting_states" && finding.Type != "low_confidence_entity" {
		return
	}
	payload, err := json.Marshal(finding)
	if err != nil {
		return
	}
	_, _, _ = in.Store.SaveAgentProposal(store.AgentProposalRecord{
		AgentKey: a.Key(),
		Kind:     finding.Type,
		TargetID: finding.EntryID,
		Summary:  finding.Detail,
		Risk:     "low",
		Payload:  payload,
	})
}

// llmReviewPrompt 让模型只输出 JSON 数组；解析失败不阻断确定性报告。
const llmReviewPrompt = `你是长篇小说的世界监修。请只依据给定“记忆摘要”和“最新章节正文”，找出正文与记忆/设定可能冲突的地方。
只输出 JSON 数组，元素格式：{"severity":"info|warning|blocking","detail":"中文说明","quote":"正文原句（不超过60字）"}。
没有冲突时输出 []。不要输出 Markdown，不要改正文。`

func llmAudit(ctx context.Context, in agentruntime.Input, ctx2 projectContext, content, chapterID string, index int) ([]agentruntime.Finding, int, int, int, error) {
	digest := buildMemoryDigest(ctx2, 3000)
	messages := []llm.Message{
		{Role: "system", Content: llmReviewPrompt},
		{Role: "user", Content: "记忆摘要：\n" + digest + "\n\n最新章节正文（第" + fmt.Sprint(index) + "章）：\n" + compactExcerpt(content, 3000)},
	}
	result, err := in.Client.CompleteChat(ctx, messages, 0.1)
	if err != nil {
		return nil, 0, 0, 0, err
	}
	findings := parseLLMFindings(result.Content, chapterID, index)
	return findings, result.PromptTokens, result.CompletionTokens, result.TotalTokens, nil
}

func buildMemoryDigest(ctx2 projectContext, limit int) string {
	var b strings.Builder
	for _, character := range ctx2.knowledge.Characters {
		fmt.Fprintf(&b, "角色：%s（%s）\n", character.Name, character.Role)
	}
	for _, entry := range ctx2.knowledge.World {
		fmt.Fprintf(&b, "设定：%s：%s\n", entry.Title, truncateRunes(entry.Content, 80))
	}
	for _, state := range ctx2.states {
		if state.Status == novel.StateActive {
			fmt.Fprintf(&b, "状态：%s｜%s=%s\n", state.SubjectID, state.Attribute, state.Value)
		}
	}
	for _, event := range ctx2.events {
		if event.Status == novel.EventConfirmed {
			fmt.Fprintf(&b, "事件：%s\n", event.Summary)
		}
	}
	return truncateRunes(b.String(), limit)
}

func parseLLMFindings(content, chapterID string, index int) []agentruntime.Finding {
	start := strings.Index(content, "[")
	end := strings.LastIndex(content, "]")
	if start < 0 || end <= start {
		return nil
	}
	var raw []struct {
		Severity string `json:"severity"`
		Detail   string `json:"detail"`
		Quote    string `json:"quote"`
	}
	if err := json.Unmarshal([]byte(content[start:end+1]), &raw); err != nil {
		return nil
	}
	out := make([]agentruntime.Finding, 0, len(raw))
	for _, item := range raw {
		detail := strings.TrimSpace(item.Detail)
		if detail == "" {
			continue
		}
		severity := strings.ToLower(strings.TrimSpace(item.Severity))
		switch severity {
		case "info", "warning", "blocking":
		default:
			severity = "warning"
		}
		out = append(out, agentruntime.Finding{
			Type:         "llm_conflict",
			Severity:     severity,
			ChapterID:    chapterID,
			ChapterIndex: index,
			Quote:        truncateRunes(strings.TrimSpace(item.Quote), 80),
			Detail:       truncateRunes(detail, 300),
			Confidence:   0.6,
			Evidence:     []agentruntime.Evidence{{ChapterID: chapterID, ChapterIndex: index, Quote: truncateRunes(strings.TrimSpace(item.Quote), 80)}},
		})
	}
	return out
}

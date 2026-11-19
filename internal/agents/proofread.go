package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"

	"novelagent/internal/agentruntime"
	"novelagent/internal/novel"
	"novelagent/internal/store"
)

// ProofreadAgent（A7 校对润色）：确定性检查标点、重复词、长句与空对白，形成校对清单；
// propose/autonomous 档位可把清单送入审阅队列，绝不直接改正文。
type ProofreadAgent struct{}

func (a *ProofreadAgent) Key() string  { return "proofread" }
func (a *ProofreadAgent) Name() string { return "A7 校对润色" }

func (a *ProofreadAgent) Triggers() []string {
	return []string{
		string(store.ChapterCreated), string(store.ChapterSaved),
		string(store.ChaptersImported), string(store.ChaptersAppended),
	}
}

func (a *ProofreadAgent) Run(ctx context.Context, ev agentruntime.Event, in agentruntime.Input) (agentruntime.Report, error) {
	report := agentruntime.Report{Severity: "info"}
	if in.Store == nil {
		return report, fmt.Errorf("proofread: store is required")
	}
	pc, err := loadProjectContext(in.Store)
	if err != nil {
		return report, fmt.Errorf("proofread: load context: %w", err)
	}
	chapter, ok := chapterForAgent(in.Store, ev, pc)
	if !ok || strings.TrimSpace(chapter.Content) == "" {
		report.Summary = "暂无可校对的正文。"
		return report, nil
	}
	findings := proofreadFindings(chapter)
	report.Findings = findings
	report.Severity = severityOf(severityList(findings)...)
	if len(findings) == 0 {
		report.Summary = fmt.Sprintf("第%d章校对：未发现常见标点/重复词问题。", chapter.Index)
	} else {
		report.Summary = fmt.Sprintf("第%d章校对：发现 %d 项可润色问题。", chapter.Index, len(findings))
	}
	report.Details = append(report.Details, fmt.Sprintf("权限：%s（校对建议仅入报告/审阅，不改正文）", in.Permission))

	if len(findings) > 0 && canPropose(in) {
		payload, _ := json.Marshal(map[string]any{
			"chapterId":       chapter.ID,
			"chapterRevision": chapter.Revision,
			"issues":          findings,
		})
		_, _, _ = in.Store.SaveAgentProposal(store.AgentProposalRecord{
			AgentKey: a.Key(),
			Kind:     "proofread_issues",
			TargetID: chapter.ID,
			Summary:  report.Summary,
			Risk:     "low",
			Payload:  payload,
		})
	}
	return report, nil
}

func proofreadFindings(chapter novel.Chapter) []agentruntime.Finding {
	content := chapter.Content
	findings := make([]agentruntime.Finding, 0, 8)

	for _, token := range []string{"，，", "。。", "；；", "、、", "：："} {
		if strings.Contains(content, token) {
			findings = append(findings, agentruntime.Finding{
				Type:         "duplicate_punctuation",
				Severity:     "warning",
				ChapterID:    chapter.ID,
				ChapterIndex: chapter.Index,
				Quote:        contextAround(content, token, 60),
				Detail:       fmt.Sprintf("检测到重复标点「%s」。", token),
			})
			break
		}
	}
	if strings.Count(content, "“") != strings.Count(content, "”") {
		findings = append(findings, agentruntime.Finding{
			Type:         "quote_mismatch",
			Severity:     "warning",
			ChapterID:    chapter.ID,
			ChapterIndex: chapter.Index,
			Detail:       fmt.Sprintf("中文引号数量不匹配：左 %d、右 %d。", strings.Count(content, "“"), strings.Count(content, "”")),
		})
	}
	if strings.Contains(content, "「") && strings.Count(content, "「") != strings.Count(content, "」") {
		findings = append(findings, agentruntime.Finding{
			Type:         "quote_mismatch",
			Severity:     "warning",
			ChapterID:    chapter.ID,
			ChapterIndex: chapter.Index,
			Detail:       fmt.Sprintf("直角引号数量不匹配：左 %d、右 %d。", strings.Count(content, "「"), strings.Count(content, "」")),
		})
	}
	if strings.Contains(content, "“”") || strings.Contains(content, "「」") {
		findings = append(findings, agentruntime.Finding{
			Type:         "empty_dialogue",
			Severity:     "warning",
			ChapterID:    chapter.ID,
			ChapterIndex: chapter.Index,
			Detail:       "检测到空对白引号。",
		})
	}
	if strings.Contains(content, "\"") {
		findings = append(findings, agentruntime.Finding{
			Type:         "ascii_quote",
			Severity:     "info",
			ChapterID:    chapter.ID,
			ChapterIndex: chapter.Index,
			Quote:        contextAround(content, "\"", 60),
			Detail:       "正文混用英文直引号，建议统一为中文引号。",
		})
	}
	if strings.Contains(content, "  ") || strings.Contains(content, "\u3000\u3000") {
		findings = append(findings, agentruntime.Finding{
			Type:         "double_space",
			Severity:     "info",
			ChapterID:    chapter.ID,
			ChapterIndex: chapter.Index,
			Detail:       "检测到连续空格/全角空格，建议清理。",
		})
	}

	allowed := repeatedWordAllowlist()
	for _, word := range repeatedHanWords(content) {
		if allowed[word] {
			continue
		}
		findings = append(findings, agentruntime.Finding{
			Type:         "repeated_word",
			Severity:     "info",
			ChapterID:    chapter.ID,
			ChapterIndex: chapter.Index,
			Quote:        contextAround(content, word, 60),
			Detail:       fmt.Sprintf("检测到相邻重复字「%s」，请确认是否为笔误。", word),
		})
		if countType(findings, "repeated_word") >= 3 {
			break
		}
	}

	long := 0
	for _, sentence := range splitSentences(content) {
		if len([]rune(sentence)) <= 120 {
			continue
		}
		findings = append(findings, agentruntime.Finding{
			Type:         "sentence_too_long",
			Severity:     "info",
			ChapterID:    chapter.ID,
			ChapterIndex: chapter.Index,
			Quote:        truncateRunes(sentence, 80),
			Detail:       fmt.Sprintf("单句长度 %d 字，建议拆分。", len([]rune(sentence))),
		})
		long++
		if long >= 3 {
			break
		}
	}
	return findings
}

func countType(findings []agentruntime.Finding, kind string) int {
	count := 0
	for _, finding := range findings {
		if finding.Type == kind {
			count++
		}
	}
	return count
}

// repeatedHanWords 返回正文中相邻重复的汉字对（去除合法叠词）。
func repeatedHanWords(content string) []string {
	allowed := repeatedWordAllowlist()
	runes := []rune(content)
	out := make([]string, 0, 4)
	seen := make(map[string]bool)
	for i := 1; i < len(runes); i++ {
		if runes[i] != runes[i-1] || !unicode.Is(unicode.Han, runes[i]) {
			continue
		}
		pair := string(runes[i-1 : i+1])
		if allowed[pair] || seen[pair] {
			i++
			continue
		}
		seen[pair] = true
		out = append(out, pair)
		i++
		if len(out) >= 5 {
			break
		}
	}
	return out
}

func repeatedWordAllowlist() map[string]bool {
	words := []string{
		"渐渐", "慢慢", "悄悄", "轻轻", "匆匆", "常常", "刚刚", "仅仅", "明明", "偏偏",
		"天天", "年年", "人人", "看看", "试试", "想想", "说说", "走走", "坐坐", "等等",
		"谢谢", "叔叔", "爷爷", "奶奶", "哥哥", "姐姐", "弟弟", "妹妹", "爸爸", "妈妈",
		"宝宝", "星星", "点点", "丝丝", "阵阵", "声声", "字字", "句句", "步步", "层层",
		"滚滚", "茫茫", "幽幽", "默默", "静静", "缓缓", "徐徐", "微微", "稍稍", "深深",
		"浅浅", "淡淡", "浓浓", "高高", "低低", "远远", "久久", "连连", "纷纷", "一一",
	}
	out := make(map[string]bool, len(words))
	for _, word := range words {
		out[word] = true
	}
	return out
}

func contextAround(content, token string, limit int) string {
	index := strings.Index(content, token)
	if index < 0 {
		return ""
	}
	runes := []rune(content)
	target := len([]rune(content[:index]))
	start := max(0, target-limit/2)
	end := min(len(runes), target+limit/2)
	return truncateRunes(string(runes[start:end]), limit)
}

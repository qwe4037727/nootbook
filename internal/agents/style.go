package agents

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"novelagent/internal/agentruntime"
	"novelagent/internal/novel"
	"novelagent/internal/store"
)

// StyleAgent（A5 文风）：用确定性指标对照知识库文风画像，发现视角/句长/对白漂移。
// 只报告，不修改正文。
type StyleAgent struct{}

func (a *StyleAgent) Key() string  { return "style" }
func (a *StyleAgent) Name() string { return "A5 文风" }

func (a *StyleAgent) Triggers() []string {
	return []string{
		string(store.ChapterCreated), string(store.ChapterSaved),
		string(store.ChaptersImported), string(store.ChaptersAppended),
	}
}

func (a *StyleAgent) Run(ctx context.Context, ev agentruntime.Event, in agentruntime.Input) (agentruntime.Report, error) {
	report := agentruntime.Report{Severity: "info"}
	if in.Store == nil {
		return report, fmt.Errorf("style: store is required")
	}
	pc, err := loadProjectContext(in.Store)
	if err != nil {
		return report, fmt.Errorf("style: load context: %w", err)
	}
	chapter, ok := chapterForAgent(in.Store, ev, pc)
	if !ok || strings.TrimSpace(chapter.Content) == "" {
		report.Summary = "暂无可检查的正文。"
		return report, nil
	}
	metrics := measureStyle(chapter.Content)
	findings := compareStyle(pc.knowledge.Style, metrics, chapter)

	report.Findings = findings
	report.Severity = severityOf(severityList(findings)...)
	report.Summary = fmt.Sprintf("第%d章文风：句均 %.1f 字、对白 %.0f%%、段落 %d，%d 项观察。",
		chapter.Index, metrics.avgSentence, metrics.dialogueRatio*100, metrics.paragraphs, len(findings))
	report.Details = append(report.Details,
		fmt.Sprintf("总字数 %d · 句数 %d · 第一人称「我」%d 次 · 第三人称「他/她」%d 次",
			metrics.chars, metrics.sentences, metrics.firstPerson, metrics.thirdPerson),
		fmt.Sprintf("权限：%s（只出报告，不改正文）", in.Permission),
	)
	return report, nil
}

type styleMetrics struct {
	chars         int
	sentences     int
	avgSentence   float64
	dialogueRatio float64
	paragraphs    int
	firstPerson   int
	thirdPerson   int
}

func measureStyle(content string) styleMetrics {
	content = strings.TrimSpace(content)
	metrics := styleMetrics{chars: utf8.RuneCountInString(content)}
	sentences := splitSentences(content)
	metrics.sentences = len(sentences)
	if metrics.sentences > 0 {
		total := 0
		for _, sentence := range sentences {
			total += utf8.RuneCountInString(sentence)
		}
		metrics.avgSentence = float64(total) / float64(metrics.sentences)
	}
	metrics.dialogueRatio = float64(dialogueRunes(content)) / float64(max(metrics.chars, 1))
	for _, line := range strings.Split(content, "\n") {
		if strings.TrimSpace(line) != "" {
			metrics.paragraphs++
		}
	}
	metrics.firstPerson = strings.Count(content, "我")
	metrics.thirdPerson = strings.Count(content, "他") + strings.Count(content, "她")
	return metrics
}

// dialogueRunes 统计中文引号/直角引号内的有效字数。
func dialogueRunes(content string) int {
	total := 0
	for _, pair := range [][2]rune{{'“', '”'}, {'「', '」'}} {
		open := false
		count := 0
		for _, r := range content {
			switch r {
			case pair[0]:
				open = true
				count = 0
			case pair[1]:
				if open {
					total += count
				}
				open = false
			default:
				if open {
					count++
				}
			}
		}
	}
	return total
}

func compareStyle(style novel.StyleProfile, metrics styleMetrics, chapter novel.Chapter) []agentruntime.Finding {
	findings := make([]agentruntime.Finding, 0, 4)
	quote := truncateRunes(firstSentence(chapter.Content), 80)
	if strings.TrimSpace(style.Perspective) == "" && strings.TrimSpace(style.Tone) == "" &&
		strings.TrimSpace(style.Sentence) == "" && strings.TrimSpace(style.Dialogue) == "" {
		findings = append(findings, agentruntime.Finding{
			Type:         "style_profile_missing",
			Severity:     "info",
			ChapterID:    chapter.ID,
			ChapterIndex: chapter.Index,
			Detail:       "尚未建立文风画像，建议先完成知识库分析或在「大世界」中补充文风字段。",
		})
		return findings
	}
	if strings.Contains(style.Perspective, "第一") && metrics.firstPerson < 3 && metrics.thirdPerson >= 10 {
		findings = append(findings, agentruntime.Finding{
			Type:         "style_perspective_drift",
			Severity:     "warning",
			ChapterID:    chapter.ID,
			ChapterIndex: chapter.Index,
			Quote:        quote,
			Detail:       fmt.Sprintf("设定为第一人称，但本章「我」仅 %d 次、「他/她」%d 次，可能出现视角漂移。", metrics.firstPerson, metrics.thirdPerson),
		})
	}
	if strings.Contains(style.Sentence, "短") && metrics.avgSentence > 45 {
		findings = append(findings, agentruntime.Finding{
			Type:         "style_sentence_deviation",
			Severity:     "info",
			ChapterID:    chapter.ID,
			ChapterIndex: chapter.Index,
			Quote:        quote,
			Detail:       fmt.Sprintf("文风设定偏短句，但本章句均 %.1f 字。", metrics.avgSentence),
		})
	}
	if strings.Contains(style.Sentence, "长") && metrics.avgSentence < 12 {
		findings = append(findings, agentruntime.Finding{
			Type:         "style_sentence_deviation",
			Severity:     "info",
			ChapterID:    chapter.ID,
			ChapterIndex: chapter.Index,
			Quote:        quote,
			Detail:       fmt.Sprintf("文风设定偏长句，但本章句均仅 %.1f 字。", metrics.avgSentence),
		})
	}
	if strings.Contains(style.Dialogue, "多") && metrics.dialogueRatio < 0.08 {
		findings = append(findings, agentruntime.Finding{
			Type:         "style_dialogue_deviation",
			Severity:     "info",
			ChapterID:    chapter.ID,
			ChapterIndex: chapter.Index,
			Quote:        quote,
			Detail:       fmt.Sprintf("文风设定对白较多，但本章对白占比约 %.0f%%。", metrics.dialogueRatio*100),
		})
	}
	if strings.Contains(style.Dialogue, "简") && metrics.dialogueRatio > 0.5 {
		findings = append(findings, agentruntime.Finding{
			Type:         "style_dialogue_deviation",
			Severity:     "info",
			ChapterID:    chapter.ID,
			ChapterIndex: chapter.Index,
			Quote:        quote,
			Detail:       fmt.Sprintf("文风设定对白简省，但本章对白占比约 %.0f%%。", metrics.dialogueRatio*100),
		})
	}
	return findings
}

func firstSentence(content string) string {
	sentences := splitSentences(content)
	if len(sentences) == 0 {
		return ""
	}
	return sentences[0]
}

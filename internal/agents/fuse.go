package agents

import (
	"context"
	"fmt"
	"strings"

	"novelagent/internal/agentruntime"
	"novelagent/internal/novel"
	"novelagent/internal/store"
)

// FuseAgent（A3 伏笔管家）：从新章节/新导入正文中识别伏笔候选，写入 foreshadow 台账。
// 台账只做候选登记（status=candidate），由人确认；候选会自动参与续写注入（#60）。
type FuseAgent struct{}

func (a *FuseAgent) Key() string  { return "fuse" }
func (a *FuseAgent) Name() string { return "A3 伏笔管家" }

func (a *FuseAgent) Triggers() []string {
	return []string{string(store.ChapterCreated), string(store.ChapterSaved), string(store.ChaptersImported)}
}

var foreshadowMarkers = []string{
	"似乎", "仿佛", "隐隐", "秘密", "神秘", "不知", "未解", "伏笔", "预感",
	"奇怪", "异样", "暗中", "悄然", "注定", "谜", "疑点", "线索", "为什么",
	"难道", "究竟", "却不知道", "没有察觉", "若有所思", "若有所思地", "欲言又止",
}

func (a *FuseAgent) Run(ctx context.Context, ev agentruntime.Event, in agentruntime.Input) (agentruntime.Report, error) {
	report := agentruntime.Report{Severity: "info"}
	if in.Store == nil {
		return report, fmt.Errorf("fuse: store is required")
	}
	project, err := in.Store.LoadProject()
	if err != nil {
		return report, fmt.Errorf("fuse: load project: %w", err)
	}
	knowledge, err := in.Store.LoadKnowledge()
	if err != nil {
		return report, fmt.Errorf("fuse: load knowledge: %w", err)
	}

	chapters := make([]novel.Chapter, 0, 4)
	if chapter, ok := currentChapter(in.Store, ev); ok && strings.TrimSpace(chapter.Content) != "" {
		chapters = append(chapters, chapter)
	} else if ev.Type == string(store.ChaptersImported) {
		// 导入事件没有单章 ID：扫描尚未登记过伏笔的最近章节。
		for i := len(project.Chapters) - 1; i >= 0; i-- {
			chapter, err := in.Store.LoadChapter(project.Chapters[i].ID)
			if err != nil || strings.TrimSpace(chapter.Content) == "" {
				continue
			}
			chapters = append(chapters, chapter)
			if len(chapters) >= 3 {
				break
			}
		}
	}
	if len(chapters) == 0 {
		report.Summary = "没有可扫描的新正文。"
		return report, nil
	}

	records := make([]store.ForeshadowRecord, 0, 8)
	for _, chapter := range chapters {
		index := chapter.Index
		if index == 0 {
			index = chapterIndexOf(projectContext{project: project}, chapter.ID)
		}
		for _, candidate := range extractForeshadowCandidates(chapter.Content, knowledge, 8) {
			records = append(records, store.ForeshadowRecord{
				ID:           novel.NewID("fs"),
				ChapterID:    chapter.ID,
				ChapterIndex: index,
				Title:        candidate.title,
				Kind:         candidate.kind,
				Quote:        candidate.quote,
				Status:       "candidate",
				Confidence:   candidate.confidence,
				Related:      candidate.related,
				EvidenceNote: candidate.note,
			})
		}
	}
	inserted, err := in.Store.SaveForeshadows(records)
	if err != nil {
		return report, fmt.Errorf("fuse: save ledger: %w", err)
	}
	report.Summary = fmt.Sprintf("伏笔扫描：识别 %d 条候选，新增入账 %d 条。", len(records), inserted)
	report.Details = make([]string, 0, len(records))
	for i, record := range records {
		if i >= 6 {
			break
		}
		report.Details = append(report.Details, fmt.Sprintf("第 %d 章「%s」：%s", record.ChapterIndex, record.Title, truncateRunes(record.Quote, 60)))
	}
	if len(records) > 0 {
		report.Details = append(report.Details, "候选已参与续写记忆注入；确认/废弃请在伏笔台账操作。")
	}
	return report, nil
}

type fuseCandidate struct {
	title      string
	kind       string
	quote      string
	confidence float64
	related    []string
	note       string
}

func extractForeshadowCandidates(content string, knowledge novel.Knowledge, limit int) []fuseCandidate {
	sentences := splitSentences(content)
	out := make([]fuseCandidate, 0, limit)
	seen := make(map[string]struct{})
	for _, sentence := range sentences {
		if len([]rune(sentence)) < 8 {
			continue
		}
		markers := make([]string, 0, 2)
		for _, marker := range foreshadowMarkers {
			if strings.Contains(sentence, marker) {
				markers = append(markers, marker)
			}
		}
		if len(markers) == 0 {
			continue
		}
		quote := truncateRunes(sentence, 120)
		if _, ok := seen[quote]; ok {
			continue
		}
		seen[quote] = struct{}{}
		related := make([]string, 0, 2)
		for _, character := range knowledge.Characters {
			if mentionsAny(sentence, character.Name, character.Aliases) {
				related = append(related, character.ID)
			}
		}
		out = append(out, fuseCandidate{
			title:      truncateRunes(sentence, 18),
			kind:       classifyForeshadow(sentence),
			quote:      quote,
			confidence: 0.55,
			related:    related,
			note:       "关键词命中：" + strings.Join(markers, "、"),
		})
		if len(out) >= limit {
			break
		}
	}
	return out
}

func classifyForeshadow(sentence string) string {
	switch {
	case strings.Contains(sentence, "身份") || strings.Contains(sentence, "秘密") || strings.Contains(sentence, "究竟是谁"):
		return "identity"
	case strings.Contains(sentence, "预言") || strings.Contains(sentence, "注定") || strings.Contains(sentence, "命运"):
		return "prophecy"
	case strings.Contains(sentence, "信") || strings.Contains(sentence, "钥匙") || strings.Contains(sentence, "玉佩") ||
		strings.Contains(sentence, "物") || strings.Contains(sentence, "匣"):
		return "object"
	default:
		return "mystery"
	}
}

package adapt

import (
	"strings"

	"novelagent/internal/novel"
	"novelagent/internal/store"
)

// Impact 是模式 B 的影响清单：只做推演与提示，不写记忆（记忆变更由草稿确认后的既有链路负责）。
type Impact struct {
	Mode             string              `json:"mode"`
	ChangedEntities  []ImpactEntity      `json:"changedEntities"`
	AffectedChapters []ImpactChapter     `json:"affectedChapters"`
	Foreshadows      []ImpactForeshadow  `json:"foreshadows"`
	MemoryEvents     []ImpactMemoryEvent `json:"memoryEvents"`
	Warnings         []string            `json:"warnings"`
}

type ImpactEntity struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	Reason string `json:"reason"`
}

type ImpactChapter struct {
	ID     string `json:"id"`
	Index  int    `json:"index"`
	Title  string `json:"title"`
	Reason string `json:"reason"`
}

type ImpactForeshadow struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Quote  string `json:"quote"`
	Status string `json:"status"`
	Reason string `json:"reason"`
}

type ImpactMemoryEvent struct {
	ID      string `json:"id"`
	Summary string `json:"summary"`
	Reason  string `json:"reason"`
}

type ImpactInput struct {
	ChapterID    string
	ChapterIndex int
	Mode         string
	OldSelection string
	NewSelection string
}

const maxImpactChapters = 20

// AnalyzeImpact 按“实体提及变化 + 后续章节引用 + 伏笔原文 + 本章记忆事件”推演影响范围。
func AnalyzeImpact(st *store.Store, input ImpactInput) (Impact, error) {
	impact := Impact{
		Mode:             input.Mode,
		ChangedEntities:  []ImpactEntity{},
		AffectedChapters: []ImpactChapter{},
		Foreshadows:      []ImpactForeshadow{},
		MemoryEvents:     []ImpactMemoryEvent{},
		Warnings:         []string{},
	}
	if strings.EqualFold(input.Mode, "A") {
		impact.Warnings = append(impact.Warnings, "模式A：不改世界状态，未执行影响推演")
		return impact, nil
	}

	knowledge, err := st.LoadKnowledge()
	if err != nil {
		return Impact{}, err
	}
	extended, err := st.ListKBEntries("")
	if err != nil {
		return Impact{}, err
	}
	chapters, err := st.LoadChapters()
	if err != nil {
		return Impact{}, err
	}

	type candidate struct {
		id      string
		name    string
		kind    string
		aliases []string
	}
	candidates := make([]candidate, 0, len(knowledge.Characters)+len(knowledge.World)+len(extended))
	for _, character := range knowledge.Characters {
		candidates = append(candidates, candidate{
			id: character.ID, name: character.Name, kind: string(novel.EntryCharacter), aliases: character.Aliases,
		})
	}
	for _, entry := range knowledge.World {
		candidates = append(candidates, candidate{
			id: entry.ID, name: entry.Title, kind: string(novel.EntrySetting), aliases: entry.Aliases,
		})
	}
	for _, entry := range extended {
		candidates = append(candidates, candidate{
			id: entry.ID, name: entry.Name, kind: string(entry.Kind), aliases: entry.Aliases,
		})
	}

	changed := map[string]candidate{}
	for _, cand := range candidates {
		oldHas := mentionsAny(input.OldSelection, cand.name, cand.aliases)
		newHas := mentionsAny(input.NewSelection, cand.name, cand.aliases)
		if oldHas == newHas {
			continue
		}
		reason := "新选区新增了对该词条的提及"
		if oldHas {
			reason = "新选区不再提及该词条"
		}
		changed[cand.id] = cand
		impact.ChangedEntities = append(impact.ChangedEntities, ImpactEntity{
			ID: cand.id, Name: cand.name, Kind: cand.kind, Reason: reason,
		})
	}

	foreshadows, err := st.ListForeshadows("", 500)
	if err != nil {
		return Impact{}, err
	}
	for _, fs := range foreshadows {
		quote := strings.TrimSpace(fs.Quote)
		if quote == "" || !strings.Contains(input.OldSelection, quote) {
			continue
		}
		if strings.Contains(input.NewSelection, quote) {
			continue
		}
		impact.Foreshadows = append(impact.Foreshadows, ImpactForeshadow{
			ID: fs.ID, Title: fs.Title, Quote: quote, Status: fs.Status,
			Reason: "选区已不再包含该伏笔原文，后续回收可能失效",
		})
	}

	if len(changed) > 0 {
		for _, chapter := range chapters {
			if chapter.Index <= input.ChapterIndex {
				continue
			}
			var hits []string
			for _, cand := range changed {
				if mentionsAny(chapter.Content, cand.name, cand.aliases) {
					hits = append(hits, cand.name)
				}
			}
			if len(hits) == 0 {
				continue
			}
			impact.AffectedChapters = append(impact.AffectedChapters, ImpactChapter{
				ID: chapter.ID, Index: chapter.Index, Title: chapter.Title,
				Reason: "后续章节提及受影响词条：" + strings.Join(hits, "、"),
			})
			if len(impact.AffectedChapters) >= maxImpactChapters {
				break
			}
		}
	}

	events, _, _, _, _, err := st.LoadMemoryRecords()
	if err != nil {
		return Impact{}, err
	}
	for _, event := range events {
		if event.ChapterID != input.ChapterID || event.Status == novel.EventInvalidated {
			continue
		}
		impact.MemoryEvents = append(impact.MemoryEvents, ImpactMemoryEvent{
			ID: event.ID, Summary: event.Summary,
			Reason: "本章记忆事件可能需要重抽取（模式B不自动写入，确认草稿后按既有链路处理）",
		})
	}

	if len(impact.ChangedEntities) == 0 && len(impact.Foreshadows) == 0 {
		impact.Warnings = append(impact.Warnings, "未检测到词条级影响；仍建议人工复核相邻章节的一致性")
	}
	return impact, nil
}

func mentionsAny(text, name string, aliases []string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return false
	}
	if name != "" && strings.Contains(text, name) {
		return true
	}
	for _, alias := range aliases {
		if alias != "" && strings.Contains(text, alias) {
			return true
		}
	}
	return false
}

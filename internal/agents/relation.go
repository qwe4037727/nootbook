package agents

import (
	"context"
	"fmt"
	"strings"

	"novelagent/internal/agentruntime"
	"novelagent/internal/novel"
	"novelagent/internal/store"
)

// RelationAgent（A6 关系情绪）：对照关系边与情绪状态，提示过期关系、未登记关系与长期未确认情绪。
// 只报告，不写关系/正文。
type RelationAgent struct{}

func (a *RelationAgent) Key() string  { return "relation" }
func (a *RelationAgent) Name() string { return "A6 关系情绪" }

func (a *RelationAgent) Triggers() []string {
	return []string{
		string(store.ChapterCreated), string(store.ChapterSaved),
		string(store.ChaptersImported), string(store.ChaptersAppended),
	}
}

func (a *RelationAgent) Run(ctx context.Context, ev agentruntime.Event, in agentruntime.Input) (agentruntime.Report, error) {
	report := agentruntime.Report{Severity: "info"}
	if in.Store == nil {
		return report, fmt.Errorf("relation: store is required")
	}
	pc, err := loadProjectContext(in.Store)
	if err != nil {
		return report, fmt.Errorf("relation: load context: %w", err)
	}
	chapter, ok := chapterForAgent(in.Store, ev, pc)
	if !ok || strings.TrimSpace(chapter.Content) == "" {
		report.Summary = "暂无可检查的正文。"
		return report, nil
	}

	names := make(map[string]string)
	mentioned := make(map[string]bool)
	for _, character := range pc.knowledge.Characters {
		if strings.TrimSpace(character.Name) == "" {
			continue
		}
		names[character.ID] = character.Name
		if mentionsAny(chapter.Content, character.Name, character.Aliases) {
			mentioned[character.ID] = true
		}
	}
	entries, _ := in.Store.ListKBEntries("")
	for _, entry := range entries {
		if strings.TrimSpace(entry.Name) == "" {
			continue
		}
		names[entry.ID] = entry.Name
		if mentionsAny(chapter.Content, entry.Name, entry.Aliases) {
			mentioned[entry.ID] = true
		}
	}

	findings := make([]agentruntime.Finding, 0, 6)
	quote := truncateRunes(firstSentence(chapter.Content), 80)
	relations, _ := in.Store.ListRelations("", string(novel.RelationActive))
	knownPairs := make(map[string]bool)
	for _, relation := range relations {
		fromName, toName := names[relation.FromEntryID], names[relation.ToEntryID]
		if fromName == "" || toName == "" {
			continue
		}
		if !mentioned[relation.FromEntryID] || !mentioned[relation.ToEntryID] {
			continue
		}
		knownPairs[relationPairKey(relation.FromEntryID, relation.ToEntryID)] = true
		if relation.EndChapter > 0 && chapter.Index > relation.EndChapter {
			findings = append(findings, agentruntime.Finding{
				Type:         "relation_expired",
				Severity:     "warning",
				EntryID:      relation.ID,
				ChapterID:    chapter.ID,
				ChapterIndex: chapter.Index,
				Quote:        quote,
				Detail: fmt.Sprintf("关系「%s ↔ %s（%s）」已登记在第%d章结束，但本章仍同场出现，请确认关系是否已变化。",
					fromName, toName, relationLabel(relation), relation.EndChapter),
			})
		}
	}

	unknown := 0
	for i, left := range pc.knowledge.Characters {
		if !mentioned[left.ID] || strings.TrimSpace(left.Name) == "" {
			continue
		}
		for j := i + 1; j < len(pc.knowledge.Characters); j++ {
			right := pc.knowledge.Characters[j]
			if !mentioned[right.ID] || strings.TrimSpace(right.Name) == "" {
				continue
			}
			if knownPairs[relationPairKey(left.ID, right.ID)] {
				continue
			}
			if strings.Contains(left.Relationships, right.Name) || strings.Contains(right.Relationships, left.Name) {
				continue
			}
			findings = append(findings, agentruntime.Finding{
				Type:         "relation_unknown",
				Severity:     "info",
				EntryID:      left.ID,
				ChapterID:    chapter.ID,
				ChapterIndex: chapter.Index,
				Quote:        quote,
				Detail:       fmt.Sprintf("「%s」与「%s」在本章同场，但未登记关系；如为首次接触可忽略，否则建议补充关系边。", left.Name, right.Name),
			})
			unknown++
			if unknown >= 2 {
				break
			}
		}
		if unknown >= 2 {
			break
		}
	}

	for _, state := range pc.states {
		if state.Status != novel.StateActive || !isEmotionAttribute(state.Attribute) {
			continue
		}
		if !mentioned[state.SubjectID] {
			continue
		}
		last := chapterIndexOf(pc, state.LastConfirmedChapter)
		if last == 0 {
			last = chapterIndexOf(pc, state.Origin.Chapter)
		}
		if last == 0 || chapter.Index-last <= 10 {
			continue
		}
		name := names[state.SubjectID]
		if name == "" {
			name = state.SubjectID
		}
		findings = append(findings, agentruntime.Finding{
			Type:         "emotion_state_stale",
			Severity:     "warning",
			EntryID:      state.ID,
			ChapterID:    chapter.ID,
			ChapterIndex: chapter.Index,
			Quote:        quote,
			Detail:       fmt.Sprintf("「%s」的%s「%s」已 %d 章未确认，建议复核当前情绪。", name, state.Attribute, state.Value, chapter.Index-last),
		})
		if len(findings) >= 8 {
			break
		}
	}

	report.Findings = findings
	report.Severity = severityOf(severityList(findings)...)
	report.Summary = fmt.Sprintf("第%d章关系情绪：同场角色 %d，关系边 %d，%d 项观察。", chapter.Index, len(mentioned), len(relations), len(findings))
	report.Details = append(report.Details, fmt.Sprintf("权限：%s（只出报告，不改关系/正文）", in.Permission))
	return report, nil
}

func relationLabel(relation novel.Relation) string {
	if strings.TrimSpace(relation.Label) != "" {
		return relation.Label
	}
	return relation.Type
}

func relationPairKey(a, b string) string {
	if a > b {
		a, b = b, a
	}
	return a + "|" + b
}

func isEmotionAttribute(attribute string) bool {
	for _, key := range []string{"情绪", "心情", "态度", "情感", "心态"} {
		if strings.Contains(attribute, key) {
			return true
		}
	}
	return false
}

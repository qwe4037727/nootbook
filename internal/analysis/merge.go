package analysis

import (
	"encoding/json"
	"errors"
	"strings"

	"novelagent/internal/novel"
)

type chapterResult struct {
	Summary    string             `json:"summary"`
	EndsAt     string             `json:"endsAt"`
	Characters []novel.Character  `json:"characters"`
	World      []novel.WorldEntry `json:"world"`
	Style      novel.StyleProfile `json:"style"`
}

type synthesisResult struct {
	Overview string             `json:"overview"`
	Style    novel.StyleProfile `json:"style"`
}

func parseJSONObject(text string, dst any) error {
	trimmed := strings.TrimSpace(strings.TrimPrefix(text, "\ufeff"))
	if strings.HasPrefix(trimmed, "```") {
		if newline := strings.IndexByte(trimmed, '\n'); newline >= 0 {
			trimmed = trimmed[newline+1:]
		}
		if fence := strings.LastIndex(trimmed, "```"); fence >= 0 {
			trimmed = trimmed[:fence]
		}
	}
	start := strings.IndexByte(trimmed, '{')
	if start < 0 {
		return errors.New("analysis: response contains no JSON object")
	}
	decoder := json.NewDecoder(strings.NewReader(trimmed[start:]))
	var raw json.RawMessage
	if err := decoder.Decode(&raw); err != nil {
		return err
	}
	return json.Unmarshal(raw, dst)
}

func applyChapter(knowledge *novel.Knowledge, chapter novel.Chapter, result chapterResult) {
	for _, incoming := range result.Characters {
		incoming.Name = clip(strings.TrimSpace(incoming.Name), 40)
		if incoming.Name == "" {
			continue
		}
		cleanCharacter(&incoming)
		index, ambiguous := novel.MatchCharacter(knowledge.Characters, incoming.Name, incoming.Aliases)
		if ambiguous {
			// spec §6.2：多实体命中不自动合并——迭代 2 由歧义实体候选提案处理（§6.3）。
			continue
		}
		if index < 0 {
			incoming.ID = novel.NewID("c")
			incoming.LastSeen = chapter.Index
			knowledge.Characters = append(knowledge.Characters, incoming)
			continue
		}

		existing := &knowledge.Characters[index]
		newer := chapter.Index >= existing.LastSeen
		if newer {
			existing.Role = firstNonEmpty(incoming.Role, existing.Role)
			existing.Personality = firstNonEmpty(incoming.Personality, existing.Personality)
			existing.Appearance = firstNonEmpty(incoming.Appearance, existing.Appearance)
			existing.Relationships = firstNonEmpty(incoming.Relationships, existing.Relationships)
			existing.State = firstNonEmpty(incoming.State, existing.State)
			existing.Notes = firstNonEmpty(incoming.Notes, existing.Notes)
		}
		existing.Aliases = mergeList(existing.Aliases, incoming.Aliases, 8, existing.Name)
		if incoming.Name != existing.Name {
			existing.Aliases = mergeList(existing.Aliases, []string{incoming.Name}, 8, existing.Name)
		}
		existing.Evidence = mergeList([]string{}, append(incoming.Evidence, existing.Evidence...), 6, "")
		existing.Confidence = max(existing.Confidence, incoming.Confidence)
		existing.LastSeen = max(existing.LastSeen, chapter.Index)
	}

	for _, incoming := range result.World {
		incoming.Title = clip(strings.TrimSpace(incoming.Title), 60)
		if incoming.Title == "" {
			continue
		}
		cleanWorldEntry(&incoming)
		index, ambiguous := novel.MatchWorldEntry(knowledge.World, incoming.Title, incoming.Aliases)
		if ambiguous {
			continue // 同上：歧义不自动合并
		}
		if index < 0 {
			incoming.ID = novel.NewID("w")
			incoming.LastSeen = chapter.Index
			knowledge.World = append(knowledge.World, incoming)
			continue
		}

		existing := &knowledge.World[index]
		newer := chapter.Index >= existing.LastSeen
		if newer {
			existing.Category = firstNonEmpty(incoming.Category, existing.Category)
			existing.Content = firstNonEmpty(incoming.Content, existing.Content)
		}
		existing.Aliases = mergeList(existing.Aliases, incoming.Aliases, 8, existing.Title)
		if incoming.Title != existing.Title {
			existing.Aliases = mergeList(existing.Aliases, []string{incoming.Title}, 8, existing.Title)
		}
		existing.Evidence = mergeList([]string{}, append(incoming.Evidence, existing.Evidence...), 6, "")
		existing.Confidence = max(existing.Confidence, incoming.Confidence)
		existing.LastSeen = max(existing.LastSeen, chapter.Index)
	}

	mergeStyle(&knowledge.Style, result.Style)

	summary := clip(strings.TrimSpace(result.Summary), 400)
	endsAt := clip(strings.TrimSpace(result.EndsAt), 100)
	if summary != "" || endsAt != "" {
		entry := novel.ChapterSummary{
			ChapterID: chapter.ID,
			Index:     chapter.Index,
			Title:     chapter.Title,
			Summary:   summary,
			EndsAt:    endsAt,
		}
		replaced := false
		for i := range knowledge.Summaries {
			if knowledge.Summaries[i].Index == chapter.Index {
				knowledge.Summaries[i] = entry
				replaced = true
				break
			}
		}
		if !replaced {
			knowledge.Summaries = append(knowledge.Summaries, entry)
		}
	}

	knowledge.Normalize()
}

func applySynthesis(knowledge *novel.Knowledge, result synthesisResult) {
	if overview := clip(strings.TrimSpace(result.Overview), 1200); overview != "" {
		knowledge.Overview = overview
	}
	mergeStyle(&knowledge.Style, result.Style)
	knowledge.Normalize()
}

func mergeStyle(style *novel.StyleProfile, incoming novel.StyleProfile) {
	style.Perspective = firstNonEmpty(clip(strings.TrimSpace(incoming.Perspective), 60), style.Perspective)
	style.Tone = firstNonEmpty(clip(strings.TrimSpace(incoming.Tone), 60), style.Tone)
	style.Sentence = firstNonEmpty(clip(strings.TrimSpace(incoming.Sentence), 60), style.Sentence)
	style.Dialogue = firstNonEmpty(clip(strings.TrimSpace(incoming.Dialogue), 60), style.Dialogue)
	style.Pacing = firstNonEmpty(clip(strings.TrimSpace(incoming.Pacing), 60), style.Pacing)
	style.Samples = mergeList(style.Samples, incoming.Samples, 8, "")
}

func cleanCharacter(character *novel.Character) {
	character.Aliases = mergeList(nil, character.Aliases, 8, character.Name)
	character.Role = clip(strings.TrimSpace(character.Role), 80)
	character.Personality = clip(strings.TrimSpace(character.Personality), 100)
	character.Appearance = clip(strings.TrimSpace(character.Appearance), 100)
	character.Relationships = clip(strings.TrimSpace(character.Relationships), 120)
	character.State = clip(strings.TrimSpace(character.State), 160)
	character.Notes = clip(strings.TrimSpace(character.Notes), 160)
	character.Evidence = mergeList(nil, character.Evidence, 6, "")
	if character.Confidence <= 0 {
		character.Confidence = 0.5
	}
	character.Confidence = min(character.Confidence, 1)
}

func cleanWorldEntry(entry *novel.WorldEntry) {
	entry.Aliases = mergeList(nil, entry.Aliases, 8, entry.Title)
	entry.Category = clip(strings.TrimSpace(entry.Category), 16)
	entry.Content = clip(strings.TrimSpace(entry.Content), 200)
	entry.Evidence = mergeList(nil, entry.Evidence, 6, "")
	if entry.Confidence <= 0 {
		entry.Confidence = 0.5
	}
	entry.Confidence = min(entry.Confidence, 1)
}

func matchKey(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(value), ""))
}

func mergeList(base, incoming []string, limit int, exclude string) []string {
	result := make([]string, 0, min(len(base)+len(incoming), limit))
	seen := make(map[string]struct{}, len(base)+len(incoming))
	excludeKey := matchKey(exclude)
	add := func(value string) {
		value = clip(strings.TrimSpace(value), 160)
		if value == "" {
			return
		}
		key := matchKey(value)
		if key == "" || key == excludeKey {
			return
		}
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		result = append(result, value)
	}
	for _, value := range base {
		add(value)
	}
	for _, value := range incoming {
		if len(result) >= limit {
			break
		}
		add(value)
	}
	if len(result) > limit {
		result = result[:limit]
	}
	return result
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

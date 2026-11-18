package novel

import "strings"

// 实体提及判定与种子打分（iter3-详设 §3.1）。
// 语义从 agent.selectCharacters/selectWorld 的私有实现抽取，抽取后必须保持等价：
// agent 继续用它选提示词实体，store 检索用它选记忆种子。

const (
	MentionReasonRecent     = "recent_mention"
	MentionReasonOverview   = "overview_mention"
	MentionReasonLastSeen   = "last_seen"
	MentionReasonAdjacent   = "adjacent"
	MentionReasonConfidence = "confidence"
)

// pronounKeys 为不能作为检索依据的泛称（与原 agent 实现一致）。
var pronounKeys = map[string]struct{}{
	"他": {}, "她": {}, "它": {}, "我": {}, "你": {}, "他们": {}, "她们": {},
}

// CompactText 去空白并小写，用于中文场景下的宽松子串匹配。
func CompactText(text string) string {
	return strings.ToLower(strings.Join(strings.Fields(text), ""))
}

// MentionKeys 返回实体的有效提及串（姓名+别名，去空、去代词、去重）。
func MentionKeys(name string, aliases []string) []string {
	keys := make([]string, 0, len(aliases)+1)
	seen := make(map[string]struct{}, len(aliases)+1)
	for _, value := range append([]string{name}, aliases...) {
		key := CompactText(value)
		if key == "" {
			continue
		}
		if _, ok := pronounKeys[key]; ok {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		keys = append(keys, key)
	}
	return keys
}

// MentionsAny 判断 compactText 是否提及实体。
func MentionsAny(compactText, name string, aliases []string) bool {
	return MentionsAnyExcept(compactText, name, aliases, nil)
}

// MentionsAnyExcept 同 MentionsAny，但跳过 skip 中的提及串（歧义串不参与种子判定）。
func MentionsAnyExcept(compactText, name string, aliases []string, skip map[string]struct{}) bool {
	if compactText == "" {
		return false
	}
	for _, key := range MentionKeys(name, aliases) {
		if _, ok := skip[key]; ok {
			continue
		}
		if strings.Contains(compactText, key) {
			return true
		}
	}
	return false
}

// ScoreCharacterMention 复现 agent 现有角色打分（行为等价，勿改权重）：
// confidence + 最近正文提及 4 + 概览提及 2 + 邻近 LastSeen 2 + LastSeen 命中 1 + 有状态 0.5。
func ScoreCharacterMention(character Character, compactRecent, compactOverview string, startIndex int, skip map[string]struct{}) (float64, string) {
	score := character.Confidence
	reason := ""
	if MentionsAnyExcept(compactRecent, character.Name, character.Aliases, skip) {
		score += 4
		reason = MentionReasonRecent
	}
	if MentionsAnyExcept(compactOverview, character.Name, character.Aliases, skip) {
		score += 2
		if reason == "" {
			reason = MentionReasonOverview
		}
	}
	adjacent := startIndex > 0 && character.LastSeen > 0 && character.LastSeen >= startIndex-2
	lastSeen := startIndex > 0 && character.LastSeen >= startIndex
	if adjacent {
		score += 2
	}
	if lastSeen {
		score += 1
	}
	if reason == "" {
		switch {
		case lastSeen:
			reason = MentionReasonLastSeen
		case adjacent:
			reason = MentionReasonAdjacent
		}
	}
	if character.State != "" {
		score += 0.5
	}
	return score, reason
}

// ScoreWorldMention 复现 agent 现有设定打分：confidence + 最近正文提及 3 + 邻近 LastSeen 2。
func ScoreWorldMention(entry WorldEntry, compactRecent string, startIndex int, skip map[string]struct{}) (float64, string) {
	score := entry.Confidence
	reason := ""
	if MentionsAnyExcept(compactRecent, entry.Title, entry.Aliases, skip) {
		score += 3
		reason = MentionReasonRecent
	}
	if startIndex > 0 && entry.LastSeen > 0 && entry.LastSeen >= startIndex-2 {
		score += 2
		if reason == "" {
			reason = MentionReasonAdjacent
		}
	}
	return score, reason
}

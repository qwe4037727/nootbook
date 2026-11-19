package store

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"unicode/utf8"

	"novelagent/internal/novel"
)

// 续写记忆检索（iter3-详设 §3/§4）。只读、只持 memoryMu：
// knowledge/recentText/startIndex 由调用方在 store.mu 域读好后传入，锁不嵌套。
// 确定性：同一输入两次调用输出逐字节相同（排序末尾均有 ID 兜底，无 time.Now）。

const (
	seedCharacterCap = 8
	seedWorldCap     = 6
	eventCap         = 6
	threadCap        = 5
	cognitionCap     = 4
)

// RetrieveContinuationMemory 返回已按预算裁剪的记忆上下文。
// budgetChars≤0（小窗口）→ 零注入；读取失败 → DisabledReason=read_error 且返回 error，
// 调用方只记日志、不得阻断续写。
func (s *Store) RetrieveContinuationMemory(knowledge novel.Knowledge, recentText string, startIndex, budgetChars int) (novel.ContinuationMemory, error) {
	result := novel.ContinuationMemory{
		Seeds:       []novel.MemorySeed{},
		Records:     []novel.MemoryRecord{},
		BudgetChars: budgetChars,
	}
	if budgetChars <= 0 {
		return result, nil
	}

	s.memoryMu.Lock()
	defer s.memoryMu.Unlock()

	if s.memorySafe {
		result.DisabledReason = novel.MemoryDisabledSafeState
		return result, nil
	}

	var meta memoryMetaFile
	metaErr := s.readJSONLocked(s.memoryPath("meta.json"), &meta)
	if metaErr != nil && !errors.Is(metaErr, os.ErrNotExist) {
		result.DisabledReason = novel.MemoryDisabledReadError
		return result, fmt.Errorf("store: load memory meta for retrieval: %w", metaErr)
	}
	if metaErr == nil {
		result.MemoryRevision = meta.MemoryRevision
	}

	events := []novel.StoryEvent{}
	states := []novel.StoryState{}
	cognitions := []novel.Cognition{}
	threads := []novel.StoryThread{}
	plans := []novel.StoryPlan{}
	loaders := []struct {
		name string
		dst  any
	}{
		{"events.json", &events},
		{"states.json", &states},
		{"cognitions.json", &cognitions},
		{"threads.json", &threads},
		{"plans.json", &plans},
	}
	for _, loader := range loaders {
		err := s.readJSONLocked(s.memoryPath(loader.name), loader.dst)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			result.DisabledReason = novel.MemoryDisabledReadError
			return result, fmt.Errorf("store: load memory %s for retrieval: %w", loader.name, err)
		}
	}
	fuseThreshold := startIndex
	if fuseThreshold <= 0 {
		fuseThreshold = 1 << 30
	}
	injectableForeshadows, _ := s.foreshadowsForInjectionIfPresent(fuseThreshold, 3)
	if len(events)+len(states)+len(cognitions)+len(threads)+len(plans)+len(injectableForeshadows) == 0 {
		result.DisabledReason = novel.MemoryDisabledAbsent
		return result, nil
	}

	compactRecent := novel.CompactText(recentText)
	charSeeds, charSeedIDs, charAmbiguous := resolveCharacterSeeds(knowledge, compactRecent, startIndex)
	worldSeeds, worldSeedIDs, worldAmbiguous := resolveWorldSeeds(knowledge, compactRecent, startIndex)
	result.Seeds = append(charSeeds, worldSeeds...)
	result.SkippedAmbiguous = charAmbiguous + worldAmbiguous

	characterByID := make(map[string]novel.Character, len(knowledge.Characters))
	for _, character := range knowledge.Characters {
		if character.ID != "" {
			characterByID[character.ID] = character
		}
	}
	worldByID := make(map[string]novel.WorldEntry, len(knowledge.World))
	for _, entry := range knowledge.World {
		if entry.ID != "" {
			worldByID[entry.ID] = entry
		}
	}

	groups := map[string][]novel.MemoryRecord{
		novel.MemoryKindWorldState: {},
		novel.MemoryKindState:      {},
		novel.MemoryKindCognition:  {},
		novel.MemoryKindThread:     {},
		novel.MemoryKindFuse:       {},
		novel.MemoryKindEvent:      {},
		novel.MemoryKindPlan:       {},
	}

	// 状态（P0）：active 且非 needsReview；world 类目归入世界规则子段。
	stateCandidates := make([]novel.StoryState, 0, len(states))
	for _, state := range states {
		if state.Status != novel.StateActive {
			continue
		}
		if _, ok := charSeedIDs[state.SubjectID]; !ok {
			if _, ok := worldSeedIDs[state.SubjectID]; !ok {
				continue
			}
		}
		if state.NeedsReview {
			result.SkippedReview++
			continue
		}
		stateCandidates = append(stateCandidates, state)
	}
	sort.SliceStable(stateCandidates, func(i, j int) bool {
		if stateCandidates[i].RecordVersion != stateCandidates[j].RecordVersion {
			return stateCandidates[i].RecordVersion > stateCandidates[j].RecordVersion
		}
		return stateCandidates[i].ID < stateCandidates[j].ID
	})
	for _, state := range stateCandidates {
		if record, ok := stateRecord(state, characterByID, worldByID); ok {
			groups[record.Kind] = append(groups[record.Kind], record)
		}
	}

	// 认知（P0）：known/wrong；forgotten/uncertain 不注入。
	cognitionCandidates := make([]novel.Cognition, 0, len(cognitions))
	for _, cog := range cognitions {
		if cog.Status != novel.CogKnown && cog.Status != novel.CogWrong {
			continue
		}
		if _, ok := charSeedIDs[cog.SubjectID]; !ok {
			continue
		}
		if cog.NeedsReview {
			result.SkippedReview++
			continue
		}
		cognitionCandidates = append(cognitionCandidates, cog)
	}
	sort.SliceStable(cognitionCandidates, func(i, j int) bool {
		if !cognitionCandidates[i].UpdatedAt.Equal(cognitionCandidates[j].UpdatedAt) {
			return cognitionCandidates[i].UpdatedAt.After(cognitionCandidates[j].UpdatedAt)
		}
		return cognitionCandidates[i].ID < cognitionCandidates[j].ID
	})
	if len(cognitionCandidates) > cognitionCap {
		cognitionCandidates = cognitionCandidates[:cognitionCap]
	}
	for _, cog := range cognitionCandidates {
		if character, ok := characterByID[cog.SubjectID]; ok {
			if record, ok := cognitionRecord(cog, character.Name); ok {
				groups[novel.MemoryKindCognition] = append(groups[novel.MemoryKindCognition], record)
			}
		}
	}

	// 未解决伏笔（P1）：open/developing。
	threadCandidates := make([]novel.StoryThread, 0, len(threads))
	for _, thread := range threads {
		if thread.Status != novel.ThreadOpen && thread.Status != novel.ThreadDeveloping {
			continue
		}
		if !mentionsText(compactRecent, thread.Title) && !mentionsText(compactRecent, thread.Question) && !intersectsIDs(thread.InvolvedCharacters, charSeedIDs) {
			continue
		}
		if thread.NeedsReview {
			result.SkippedReview++
			continue
		}
		threadCandidates = append(threadCandidates, thread)
	}
	sort.SliceStable(threadCandidates, func(i, j int) bool {
		if !threadCandidates[i].UpdatedAt.Equal(threadCandidates[j].UpdatedAt) {
			return threadCandidates[i].UpdatedAt.After(threadCandidates[j].UpdatedAt)
		}
		return threadCandidates[i].ID < threadCandidates[j].ID
	})
	if len(threadCandidates) > threadCap {
		threadCandidates = threadCandidates[:threadCap]
	}
	for _, thread := range threadCandidates {
		if record, ok := threadRecord(thread); ok {
			groups[novel.MemoryKindThread] = append(groups[novel.MemoryKindThread], record)
		}
	}

	// A3 伏笔台账（P1）：已确认/待确认的未回收伏笔，按埋设章节近因注入（修复 #60）。
	// 读取失败不阻断续写：伏笔只是增益上下文。
	for _, fs := range injectableForeshadows {
		label := fmt.Sprintf("第 %d 章", fs.ChapterIndex)
		if strings.TrimSpace(fs.Title) != "" {
			label += "「" + fs.Title + "」"
		}
		status := "待确认"
		if fs.Status == "confirmed" {
			status = "已确认"
		}
		text := fmt.Sprintf("%s（%s）：%s", label, status, fs.Quote)
		if strings.TrimSpace(fs.Kind) != "" {
			text = "[" + fs.Kind + "] " + text
		}
		groups[novel.MemoryKindFuse] = append(groups[novel.MemoryKindFuse], novel.MemoryRecord{
			ID:       fs.ID,
			Kind:     novel.MemoryKindFuse,
			Subject:  label,
			Text:     text,
			Order:    fs.ChapterIndex,
			Priority: novel.MemoryPriorityP1,
		})
	}

	// 关键事件（P1）：confirmed；proposed/conflicted/invalidated 一律不入事实集（R1）。
	eventCandidates := make([]novel.StoryEvent, 0, len(events))
	for _, event := range events {
		if event.Status != novel.EventConfirmed {
			continue
		}
		if !intersectsIDs(event.Participants, charSeedIDs) {
			continue
		}
		if event.NeedsReview {
			result.SkippedReview++
			continue
		}
		eventCandidates = append(eventCandidates, event)
	}
	sort.SliceStable(eventCandidates, func(i, j int) bool {
		if eventCandidates[i].StoryOrder != eventCandidates[j].StoryOrder {
			return eventCandidates[i].StoryOrder > eventCandidates[j].StoryOrder
		}
		return eventCandidates[i].ID < eventCandidates[j].ID
	})
	if len(eventCandidates) > eventCap {
		eventCandidates = eventCandidates[:eventCap]
	}
	for _, event := range eventCandidates {
		if record, ok := eventRecord(event); ok {
			groups[novel.MemoryKindEvent] = append(groups[novel.MemoryKindEvent], record)
		}
	}

	// 计划（P1）：near_term / volume 各至多 1 条；无 active 计划整段省略。
	planCandidates := make([]novel.StoryPlan, 0, len(plans))
	for _, plan := range plans {
		if plan.Status != novel.PlanActive {
			continue
		}
		if plan.NeedsReview {
			result.SkippedReview++
			continue
		}
		planCandidates = append(planCandidates, plan)
	}
	for _, plan := range selectPlanRecords(planCandidates, charSeedIDs) {
		groups[novel.MemoryKindPlan] = append(groups[novel.MemoryKindPlan], plan)
	}

	records, dropped := trimMemoryRecords(groups, budgetChars)
	result.Records = records
	result.DroppedByBudget = dropped
	result.UsedChars = novel.MemorySectionChars(&result)
	return result, nil
}

// ---- 种子解析 ----

type namedEntity struct {
	id      string
	name    string
	aliases []string
}

func ambiguousMentionKeys(entities []namedEntity, compactText string) (map[string]struct{}, int) {
	owners := make(map[string]map[string]struct{}, len(entities))
	for _, entity := range entities {
		for _, key := range novel.MentionKeys(entity.name, entity.aliases) {
			if owners[key] == nil {
				owners[key] = make(map[string]struct{}, 2)
			}
			owners[key][entity.id] = struct{}{}
		}
	}
	skip := make(map[string]struct{})
	count := 0
	for key, ids := range owners {
		if len(ids) < 2 || !strings.Contains(compactText, key) {
			continue
		}
		skip[key] = struct{}{}
		count++
	}
	return skip, count
}

func resolveCharacterSeeds(knowledge novel.Knowledge, compactRecent string, startIndex int) ([]novel.MemorySeed, map[string]struct{}, int) {
	entities := make([]namedEntity, 0, len(knowledge.Characters))
	for _, character := range knowledge.Characters {
		if character.ID != "" {
			entities = append(entities, namedEntity{id: character.ID, name: character.Name, aliases: character.Aliases})
		}
	}
	skip, ambiguous := ambiguousMentionKeys(entities, compactRecent)

	type candidate struct {
		character novel.Character
		score     float64
		reason    string
	}
	compactOverview := novel.CompactText(knowledge.Overview)
	candidates := make([]candidate, 0, len(knowledge.Characters))
	for _, character := range knowledge.Characters {
		if character.ID == "" {
			continue
		}
		score, reason := novel.ScoreCharacterMention(character, compactRecent, compactOverview, startIndex, skip)
		if score < 1 {
			continue
		}
		if reason == "" {
			reason = novel.MentionReasonConfidence
		}
		candidates = append(candidates, candidate{character: character, score: score, reason: reason})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].score != candidates[j].score {
			return candidates[i].score > candidates[j].score
		}
		if candidates[i].character.LastSeen != candidates[j].character.LastSeen {
			return candidates[i].character.LastSeen > candidates[j].character.LastSeen
		}
		return candidates[i].character.ID < candidates[j].character.ID
	})
	if len(candidates) > seedCharacterCap {
		candidates = candidates[:seedCharacterCap]
	}
	seeds := make([]novel.MemorySeed, 0, len(candidates))
	ids := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		seeds = append(seeds, novel.MemorySeed{
			EntityID:   candidate.character.ID,
			EntityName: candidate.character.Name,
			Score:      candidate.score,
			Reason:     candidate.reason,
		})
		ids[candidate.character.ID] = struct{}{}
	}
	return seeds, ids, ambiguous
}

func resolveWorldSeeds(knowledge novel.Knowledge, compactRecent string, startIndex int) ([]novel.MemorySeed, map[string]struct{}, int) {
	entities := make([]namedEntity, 0, len(knowledge.World))
	for _, entry := range knowledge.World {
		if entry.ID != "" {
			entities = append(entities, namedEntity{id: entry.ID, name: entry.Title, aliases: entry.Aliases})
		}
	}
	skip, ambiguous := ambiguousMentionKeys(entities, compactRecent)

	type candidate struct {
		entry  novel.WorldEntry
		score  float64
		reason string
	}
	candidates := make([]candidate, 0, len(knowledge.World))
	for _, entry := range knowledge.World {
		if entry.ID == "" {
			continue
		}
		score, reason := novel.ScoreWorldMention(entry, compactRecent, startIndex, skip)
		if score < 1.2 {
			continue
		}
		if reason == "" {
			reason = novel.MentionReasonConfidence
		}
		candidates = append(candidates, candidate{entry: entry, score: score, reason: reason})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].score != candidates[j].score {
			return candidates[i].score > candidates[j].score
		}
		if candidates[i].entry.LastSeen != candidates[j].entry.LastSeen {
			return candidates[i].entry.LastSeen > candidates[j].entry.LastSeen
		}
		return candidates[i].entry.ID < candidates[j].entry.ID
	})
	if len(candidates) > seedWorldCap {
		candidates = candidates[:seedWorldCap]
	}
	seeds := make([]novel.MemorySeed, 0, len(candidates))
	ids := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		seeds = append(seeds, novel.MemorySeed{
			EntityID:   candidate.entry.ID,
			EntityName: candidate.entry.Title,
			Score:      candidate.score,
			Reason:     candidate.reason,
		})
		ids[candidate.entry.ID] = struct{}{}
	}
	return seeds, ids, ambiguous
}

func intersectsIDs(values []string, set map[string]struct{}) bool {
	for _, value := range values {
		if _, ok := set[value]; ok {
			return true
		}
	}
	return false
}

func mentionsText(compactText, text string) bool {
	key := novel.CompactText(text)
	return key != "" && strings.Contains(compactText, key)
}

// ---- 记录渲染（不注入内部 ID，只注入自然语言）----

func stateRecord(state novel.StoryState, characters map[string]novel.Character, world map[string]novel.WorldEntry) (novel.MemoryRecord, bool) {
	subject := ""
	if character, ok := characters[state.SubjectID]; ok {
		subject = character.Name
	} else if entry, ok := world[state.SubjectID]; ok {
		subject = entry.Title
	}
	attribute := strings.TrimSpace(state.Attribute)
	value := strings.TrimSpace(state.Value)
	if subject == "" || attribute == "" || value == "" {
		return novel.MemoryRecord{}, false
	}
	kind := novel.MemoryKindState
	if strings.EqualFold(strings.TrimSpace(state.Category), "world") {
		kind = novel.MemoryKindWorldState
	}
	return novel.MemoryRecord{
		ID:       state.ID,
		Kind:     kind,
		Subject:  subject,
		Text:     subject + "｜" + attribute + "：" + value,
		Priority: novel.MemoryPriorityP0,
	}, true
}

func cognitionRecord(cog novel.Cognition, subject string) (novel.MemoryRecord, bool) {
	content := strings.TrimSpace(cog.Content)
	if subject == "" || content == "" {
		return novel.MemoryRecord{}, false
	}
	text := subject + "｜" + content
	if cog.Status == novel.CogWrong {
		text += "（该认知为错误认知，人物尚未纠正）"
	}
	return novel.MemoryRecord{
		ID:       cog.ID,
		Kind:     novel.MemoryKindCognition,
		Subject:  subject,
		Text:     text,
		Priority: novel.MemoryPriorityP0,
	}, true
}

func threadRecord(thread novel.StoryThread) (novel.MemoryRecord, bool) {
	title := strings.TrimSpace(thread.Title)
	if title == "" {
		return novel.MemoryRecord{}, false
	}
	parts := make([]string, 0, 2)
	if clue := firstRecordText(thread.KnownClues); clue != "" {
		parts = append(parts, "已知线索："+clue)
	}
	if criteria := firstRecordText(thread.ResolutionCriteria); criteria != "" {
		parts = append(parts, "解决条件："+criteria)
	}
	text := title
	if len(parts) > 0 {
		text = title + "｜" + strings.Join(parts, "；")
	}
	return novel.MemoryRecord{
		ID:       thread.ID,
		Kind:     novel.MemoryKindThread,
		Subject:  title,
		Text:     text,
		Priority: novel.MemoryPriorityP1,
	}, true
}

func eventRecord(event novel.StoryEvent) (novel.MemoryRecord, bool) {
	summary := strings.TrimSpace(event.Summary)
	if summary == "" {
		return novel.MemoryRecord{}, false
	}
	text := summary
	if event.StoryOrder > 0 {
		text = fmt.Sprintf("第%d章：%s", event.StoryOrder, summary)
	}
	return novel.MemoryRecord{
		ID:       event.ID,
		Kind:     novel.MemoryKindEvent,
		Text:     text,
		Order:    event.StoryOrder,
		Priority: novel.MemoryPriorityP1,
	}, true
}

func planRecord(plan novel.StoryPlan) (novel.MemoryRecord, bool) {
	title := strings.TrimSpace(plan.Title)
	if title == "" {
		return novel.MemoryRecord{}, false
	}
	text := title
	if objective := strings.TrimSpace(plan.Objective); objective != "" {
		text = title + "：" + objective
	}
	return novel.MemoryRecord{
		ID:       plan.ID,
		Kind:     novel.MemoryKindPlan,
		Subject:  title,
		Text:     text,
		Priority: novel.MemoryPriorityP1,
	}, true
}

func firstRecordText(values []string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// selectPlanRecords 从已按红线过滤的 active 计划中取 near_term 与 volume 各 1 条。
func selectPlanRecords(plans []novel.StoryPlan, charSeedIDs map[string]struct{}) []novel.MemoryRecord {
	nearTerm := make([]novel.StoryPlan, 0, len(plans))
	volume := make([]novel.StoryPlan, 0, len(plans))
	for _, plan := range plans {
		switch novel.StoryPlanLevel(plan.Level) {
		case novel.PlanLevelNearTerm:
			nearTerm = append(nearTerm, plan)
		case novel.PlanLevelVolume:
			volume = append(volume, plan)
		}
	}
	sort.SliceStable(nearTerm, func(i, j int) bool {
		hitI := intersectsIDs(nearTerm[i].InvolvedCharacters, charSeedIDs)
		hitJ := intersectsIDs(nearTerm[j].InvolvedCharacters, charSeedIDs)
		if hitI != hitJ {
			return hitI
		}
		if !nearTerm[i].UpdatedAt.Equal(nearTerm[j].UpdatedAt) {
			return nearTerm[i].UpdatedAt.After(nearTerm[j].UpdatedAt)
		}
		return nearTerm[i].ID < nearTerm[j].ID
	})
	sort.SliceStable(volume, func(i, j int) bool {
		if !volume[i].UpdatedAt.Equal(volume[j].UpdatedAt) {
			return volume[i].UpdatedAt.After(volume[j].UpdatedAt)
		}
		return volume[i].ID < volume[j].ID
	})
	result := make([]novel.MemoryRecord, 0, 2)
	for _, group := range [][]novel.StoryPlan{nearTerm, volume} {
		if len(group) == 0 {
			continue
		}
		if record, ok := planRecord(group[0]); ok {
			result = append(result, record)
		}
	}
	return result
}

// ---- 预算裁剪（iter3-详设 §4）----

var memoryDropOrder = []string{
	novel.MemoryKindEvent,
	novel.MemoryKindPlan,
	novel.MemoryKindThread,
	novel.MemoryKindCognition,
	novel.MemoryKindState,
	novel.MemoryKindWorldState,
}

func trimMemoryRecords(groups map[string][]novel.MemoryRecord, budgetChars int) ([]novel.MemoryRecord, int) {
	dropped := 0
	// 子段上限：按优先级顺序保留，超限整条丢弃（不截断句子中段）。
	for _, kind := range novel.MemorySectionKinds() {
		records := groups[kind]
		subBudget := novel.MemorySubBudget(kind)
		used := utf8.RuneCountInString(novel.MemoryGroupHeader(kind))
		kept := make([]novel.MemoryRecord, 0, len(records))
		for _, record := range records {
			lineLen := utf8.RuneCountInString("- " + strings.TrimSpace(record.Text) + "\n")
			if used+lineLen > subBudget {
				dropped++
				continue
			}
			used += lineLen
			kept = append(kept, record)
		}
		groups[kind] = kept
	}

	sectionChars := func() int {
		all := make([]novel.MemoryRecord, 0)
		for _, kind := range novel.MemorySectionKinds() {
			all = append(all, groups[kind]...)
		}
		return novel.MemorySectionChars(&novel.ContinuationMemory{BudgetChars: budgetChars, Records: all})
	}

	for sectionChars() > budgetChars {
		droppedOne := false
		for _, kind := range memoryDropOrder {
			records := groups[kind]
			if len(records) == 0 {
				continue
			}
			groups[kind] = records[:len(records)-1]
			dropped++
			droppedOne = true
			break
		}
		if !droppedOne {
			break
		}
	}

	records := make([]novel.MemoryRecord, 0)
	for _, kind := range novel.MemorySectionKinds() {
		records = append(records, groups[kind]...)
	}
	return records, dropped
}

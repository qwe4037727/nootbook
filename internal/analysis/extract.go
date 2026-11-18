package analysis

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"novelagent/internal/llm"
	"novelagent/internal/novel"
	"novelagent/internal/store"
)

// 深度提取管线（I2-1，iter2-详设 §3）。
// 由 server 经 store.BeginExtract 取得合法 token 后启动：遍历 scope 各章，调用 LLM 抽取 →
// 服务端组装 ChangeSet + 实体候选（§3.3）→ 经 StashExtractResult 整章原子暂存。
// 任务失效（代次/运行号不符）由 StashExtractResult / FinishExtract 的零写入零回写保证。

// ExtractLauncher 编排深度提取后台任务。
type ExtractLauncher struct {
	st       *store.Store
	log      *slog.Logger
	clientFn func() (*llm.Client, error) // 可注入（测试用）；为空则按配置构造真实客户端。
}

func NewExtractLauncher(st *store.Store, logger *slog.Logger) *ExtractLauncher {
	if logger == nil {
		logger = slog.Default()
	}
	return &ExtractLauncher{st: st, log: logger}
}

// Run 执行一次深度提取任务（应在 goroutine 中调用）。token 须为 BeginExtract 返回的合法令牌。
func (l *ExtractLauncher) Run(ctx context.Context, token store.ExtractRunToken) {
	var client *llm.Client
	if l.clientFn != nil {
		c, err := l.clientFn()
		if err != nil {
			l.log.Error("build extract client", "err", err)
			_ = l.st.FinishExtract(token, store.ExtractFailed, store.ExtractStats{}, "模型配置不可用")
			return
		}
		client = c
	} else {
		cfg, err := l.st.LoadConfig()
		if err != nil {
			l.log.Error("load config for extract", "err", err)
			_ = l.st.FinishExtract(token, store.ExtractFailed, store.ExtractStats{}, "读取配置失败")
			return
		}
		key, _ := cfg.EffectiveAPIKey()
		if key == "" || cfg.BaseURL == "" || cfg.Model == "" {
			_ = l.st.FinishExtract(token, store.ExtractFailed, store.ExtractStats{}, "请先完成模型设置")
			return
		}
		client = llm.NewClient(cfg.BaseURL, key, cfg.Model)
	}

	stats := store.ExtractStats{}
	lastError := ""
	for _, chapterID := range token.Scope {
		if ctx.Err() != nil {
			break
		}
		cs, candidates, ok := l.extractChapter(ctx, client, token, chapterID)
		if !ok {
			stats.FailedChapters++
			continue
		}
		err := l.st.StashExtractResult(token, cs, candidates)
		if err != nil {
			// 任务失效 → 零写入零回写；此处直接终止（其余章节不再处理）。
			if strings.Contains(err.Error(), "invalidated") {
				l.log.Warn("extract task invalidated, stop stashing", "chapter", chapterID)
				break
			}
			l.log.Error("stash extract result", "chapter", chapterID, "err", err)
			stats.FailedChapters++
			lastError = err.Error()
		}
	}

	status := store.ExtractDone
	switch {
	case stats.FailedChapters > 0 && stats.FailedChapters < len(token.Scope):
		status = store.ExtractPartial
	case stats.FailedChapters == len(token.Scope) && len(token.Scope) > 0:
		status = store.ExtractFailed
	}
	if err := l.st.FinishExtract(token, status, stats, lastError); err != nil {
		l.log.Error("finish extract", "err", err)
	}
}

// extractChapter 抽取单章：调用 LLM（若失败返回 ok=false）→ 解析 → 组装 ChangeSet + 候选 → 返回。
func (l *ExtractLauncher) extractChapter(ctx context.Context, client *llm.Client, token store.ExtractRunToken, chapterID string) (novel.ChangeSet, []novel.EntityCandidate, bool) {
	chapter, err := l.st.LoadChapter(chapterID)
	if err != nil {
		l.log.Error("load chapter for extract", "chapter", chapterID, "err", err)
		return novel.ChangeSet{}, nil, false
	}
	if strings.TrimSpace(chapter.Content) == "" {
		// 空章：视作已完成（无内容可抽），不计入失败。
		return novel.ChangeSet{}, nil, true
	}
	project, err := l.st.LoadProject()
	if err != nil {
		l.log.Error("load project for extract", "err", err)
		return novel.ChangeSet{}, nil, false
	}
	knowledge, err := l.st.LoadKnowledge()
	if err != nil {
		l.log.Error("load knowledge for extract", "err", err)
		return novel.ChangeSet{}, nil, false
	}
	events, states, _, threads, _, err := l.st.LoadMemoryRecords()
	if err != nil {
		l.log.Error("load memory records for extract", "err", err)
		return novel.ChangeSet{}, nil, false
	}
	// D7：事件去重还需覆盖既有 pending 提取批次，避免 force 重提产生近似重复。
	pendingEvents, err := l.st.LoadPendingEventRecords(chapter.ID)
	if err != nil {
		l.log.Warn("load pending extract events", "chapter", chapterID, "err", err)
	}
	related := relatedMemoryDigest(events, states, threads, chapter.ID)

	messages := buildExtractMessages(project.Title, chapter, knowledge, related)
	reply, err := completeWithTimeout(ctx, client, messages, 0.2)
	if err != nil {
		if ctx.Err() != nil {
			return novel.ChangeSet{}, nil, false
		}
		l.log.Error("extract chapter LLM", "chapter", chapter.Index, "err", err, "model", client.Model)
		return novel.ChangeSet{}, nil, false
	}
	var parsed extractResult
	if err := parseJSONObject(reply.Content, &parsed); err != nil {
		l.log.Error("decode extract result", "chapter", chapter.Index, "err", err,
			"truncated", llm.IsTruncated(reply.FinishReason), "head", headForLog(reply.Content, 300))
		return novel.ChangeSet{}, nil, false
	}

	dedupEvents := append(append([]novel.StoryEvent{}, events...), pendingEvents...)
	cs, candidates := assembleChapterChangeSet(chapter, knowledge, parsed, dedupEvents, states, threads, token.BasedOnRev)
	return cs, candidates, true
}

// assembleChapterChangeSet 将模型解析结果组装为单章 ChangeSet + 实体候选（纯函数，便于单测，不含 LLM）。
// 负责：实体解析（MatchCharacter/MatchWorldEntry）、证据定位（服务端定位 quote 偏移）、
// 状态/伏笔查重（add/update 判定）、事件去重（§3.5）、歧义/未知实体候选产出（§3.3-5）。
func assembleChapterChangeSet(
	chapter novel.Chapter,
	knowledge novel.Knowledge,
	parsed extractResult,
	existingEvents []novel.StoryEvent,
	existingStates []novel.StoryState,
	existingThreads []novel.StoryThread,
	basedOnRev int64,
) (novel.ChangeSet, []novel.EntityCandidate) {
	now := time.Now().UTC()
	cs := novel.ChangeSet{
		WorkID:          novel.InternalWorkID(),
		Status:          novel.ChangeSetProposed,
		Proposer:        "deep_extract",
		Reason:          fmt.Sprintf("chapter %s rev %d 深度提取", chapter.ID, chapter.Revision),
		ChapterID:       chapter.ID,
		ChapterRevision: chapter.Revision,
		BasedOnRevision: basedOnRev,
		CreatedAt:       now,
	}
	var candidates []novel.EntityCandidate

	// 已存在事件摘要集合（§3.5 跨章/force 兜底去重）。
	existingSummaries := make(map[string]struct{})
	for _, e := range existingEvents {
		if e.ChapterID == chapter.ID {
			existingSummaries[normalizeSummary(e.Summary)] = struct{}{}
		}
	}

	// ---- 事件：一律 add_event ----
	assembledEvents := make([]novel.StoryEvent, 0, len(parsed.Events))
	for _, ev := range parsed.Events {
		summary := strings.TrimSpace(ev.Summary)
		if summary == "" {
			continue
		}
		eventType := strings.TrimSpace(ev.EventType)
		if !novel.StoryEventType(eventType).Valid() {
			continue // L-S 失败条目丢弃（§3.3-4），避免一条非法事件毒化整章 ChangeSet。
		}
		if _, dup := existingSummaries[normalizeSummary(summary)]; dup {
			continue // 启发式去重：同章同摘要跳过。
		}
		existingSummaries[normalizeSummary(summary)] = struct{}{}

		participants, known, reviewReason, cand := resolveParticipants(ev.Participants, ev.ParticipantNamesProvided, knowledge)
		candidates = append(candidates, cand...)
		refs, evidenceNote := locateQuoteRefs(chapter, ev.Quote)

		record := novel.StoryEvent{
			MemoryMeta: novel.MemoryMeta{
				WorkID:      novel.InternalWorkID(),
				SourceRefs:  refs,
				Origin:      novel.Origin{Type: novel.OriginDeepExtract, Chapter: chapter.ID},
				LastSource:  novel.LastSourceAgentExtraction,
				NeedsReview: false,
			},
			Status:            novel.EventProposed,
			EventType:         eventType,
			Summary:           summary,
			ChapterID:         chapter.ID,
			ParticipantsKnown: known,
			Participants:      participants,
			Preconditions:     toEventLinks(ev.Preconditions),
			Consequences:      toEventLinks(ev.Consequences),
		}
		// 证据未定位 → 降级 proposed + needsReview（设计 §3.3-6）。
		if len(record.SourceRefs) == 0 {
			record.NeedsReview = true
			record.ReviewReason = "evidence_unlocated"
		}
		// 歧义参与者 → 标记（不覆盖 participantsKnown 的 unknown 语义）。
		if reviewReason != "" {
			record.NeedsReview = true
			if record.ReviewReason == "" {
				record.ReviewReason = reviewReason
			}
		}
		record.Normalize(now)
		assembledEvents = append(assembledEvents, record)
		cs.Ops = append(cs.Ops, novel.ChangeOp{
			Op:           novel.OpAddEvent,
			Record:       mustMarshal(record),
			EvidenceNote: evidenceNote,
		})
	}

	// ---- 状态：三元组查重 → add_state / update_state / 同值跳过 / manual_edit→ConflictItem（§3.3-7）----
	for _, st := range parsed.States {
		subject := strings.TrimSpace(st.Subject)
		category := strings.TrimSpace(st.Category)
		attribute := strings.TrimSpace(st.Attribute)
		value := strings.TrimSpace(st.Value)
		if subject == "" || category == "" || attribute == "" || value == "" {
			continue
		}
		subjectID, reviewReason, cand := resolveSubject(subject, knowledge)
		candidates = append(candidates, cand...)
		refs, evidenceNote := locateQuoteRefs(chapter, st.Quote)

		record := novel.StoryState{
			MemoryMeta: novel.MemoryMeta{
				WorkID:      novel.InternalWorkID(),
				SourceRefs:  refs,
				Origin:      novel.Origin{Type: novel.OriginDeepExtract, Chapter: chapter.ID},
				LastSource:  novel.LastSourceAgentExtraction,
				NeedsReview: false,
			},
			Status:    novel.StateActive,
			SubjectID: subjectID,
			Category:  clip(category, 16),
			Attribute: attribute,
			Value:     value,
		}
		if len(record.SourceRefs) == 0 {
			record.NeedsReview = true
			record.ReviewReason = "evidence_unlocated"
		}
		if reviewReason != "" {
			record.NeedsReview = true
			if record.ReviewReason == "" {
				record.ReviewReason = reviewReason
			}
		}
		record.Normalize(now)

		// B3：未知/歧义主体无法通过 accept 的 L-S（subjectId 必须 c_/w_），转冲突项而非毒化整章 ops。
		if subjectID == "" {
			cs.Conflicts = append(cs.Conflicts, novel.ConflictItem{
				Kind:     novel.ConflictAmbiguous,
				Proposed: mustMarshal(record),
				Detail:   fmt.Sprintf("状态主体 %q 未解析为既有实体（%s），已转冲突项并产出实体候选", subject, reviewReason),
			})
			continue
		}

		existing := matchActiveState(existingStates, subjectID, record.Category, record.Attribute)
		switch {
		case existing == nil:
			cs.Ops = append(cs.Ops, novel.ChangeOp{
				Op:           novel.OpAddState,
				Record:       mustMarshal(record),
				EvidenceNote: evidenceNote,
			})
		case existing.Value == record.Value:
			continue // U29：同三元组且同值 → 不产出 op，recordVersion 不变。
		case existing.LastSource == novel.LastSourceManualEdit:
			// I2-B 暂存时点：人工修订的状态不被自动覆盖。
			cs.Conflicts = append(cs.Conflicts, novel.ConflictItem{
				Kind:                  novel.ConflictManualEdit,
				TargetID:              existing.ID,
				ExpectedRecordVersion: existing.RecordVersion,
				Old:                   mustMarshal(*existing),
				Proposed:              mustMarshal(record),
				Detail:                "既有状态由人工修订，提取结果不覆盖",
			})
		default:
			record.Status = novel.StateActive
			cs.Ops = append(cs.Ops, novel.ChangeOp{
				Op:                    novel.OpUpdateState,
				TargetID:              existing.ID,
				ExpectedRecordVersion: existing.RecordVersion,
				Record:                mustMarshal(record),
				EvidenceNote:          evidenceNote,
			})
		}
	}

	// ---- 角色认知：仅 c_ 主体可落库；未知主体转冲突，世界条目跳过（§4.3）----
	seenCognitions := make(map[string]struct{})
	for _, cog := range parsed.Cognitions {
		subject := strings.TrimSpace(cog.Subject)
		content := strings.TrimSpace(cog.Content)
		if subject == "" || content == "" {
			continue
		}
		subjectID, reviewReason, cand := resolveSubject(subject, knowledge)
		candidates = append(candidates, cand...)
		if subjectID == "" {
			record := novel.Cognition{
				Status:    novel.CogKnown,
				SubjectID: subject,
				Content:   content,
			}
			cs.Conflicts = append(cs.Conflicts, novel.ConflictItem{
				Kind:     novel.ConflictAmbiguous,
				Proposed: mustMarshal(record),
				Detail:   fmt.Sprintf("认知主体 %q 未解析为既有角色（%s），已转冲突项并产出实体候选", subject, reviewReason),
			})
			continue
		}
		if !strings.HasPrefix(subjectID, "c_") {
			continue // 世界条目不能作为角色认知主体（ValidateCognition 仅接受 c_）。
		}
		key := subjectID + "|" + normalizeSummary(content)
		if _, dup := seenCognitions[key]; dup {
			continue
		}
		seenCognitions[key] = struct{}{}

		status := novel.CognitionStatus(strings.TrimSpace(cog.Status))
		if status == "" {
			status = novel.CogKnown
		}
		if !status.Valid() {
			continue
		}
		refs, evidenceNote := locateQuoteRefs(chapter, cog.Quote)
		record := novel.Cognition{
			MemoryMeta: novel.MemoryMeta{
				WorkID:      novel.InternalWorkID(),
				SourceRefs:  refs,
				Origin:      novel.Origin{Type: novel.OriginDeepExtract, Chapter: chapter.ID},
				LastSource:  novel.LastSourceAgentExtraction,
				NeedsReview: false,
			},
			Status:          status,
			SubjectID:       subjectID,
			Content:         content,
			KnownSinceEvent: matchResolutionEvent(assembledEvents, cog.Quote),
		}
		if len(record.SourceRefs) == 0 {
			record.NeedsReview = true
			record.ReviewReason = "evidence_unlocated"
		}
		if reviewReason != "" {
			record.NeedsReview = true
			if record.ReviewReason == "" {
				record.ReviewReason = reviewReason
			}
		}
		record.Normalize(now)
		cs.Ops = append(cs.Ops, novel.ChangeOp{
			Op:           novel.OpAddCognition,
			Record:       mustMarshal(record),
			EvidenceNote: evidenceNote,
		})
	}

	// ---- 伏笔：标题匹配 → update_thread，否则 add_thread（§3.3-7）----
	for _, th := range parsed.Threads {
		title := strings.TrimSpace(th.Title)
		if title == "" {
			continue
		}
		refs, evidenceNote := locateQuoteRefs(chapter, th.Quote)
		record := novel.StoryThread{
			MemoryMeta: novel.MemoryMeta{
				WorkID:      novel.InternalWorkID(),
				SourceRefs:  refs,
				Origin:      novel.Origin{Type: novel.OriginDeepExtract, Chapter: chapter.ID},
				LastSource:  novel.LastSourceAgentExtraction,
				NeedsReview: false,
			},
			Title:      title,
			KnownClues: []string{clip(strings.TrimSpace(th.Clue), 100)},
		}
		action := strings.TrimSpace(th.Action)
		resolved := action == "resolve"
		hasEvidence := len(record.SourceRefs) > 0
		conflictDetail := ""
		switch {
		case resolved && hasEvidence:
			// spec 要求 resolved 必须绑定解决事件；优先复用本章同证据事件，否则降级避免整章 422。
			if evID := matchResolutionEvent(assembledEvents, th.Quote); evID != "" {
				record.Status = novel.ThreadResolved
				record.ResolutionEvent = evID
			} else {
				record.Status = novel.ThreadDeveloping
				conflictDetail = "resolve 提案已定位证据但未能关联解决事件，降级为 developing"
			}
		case resolved && !hasEvidence:
			// 无证据 resolve → 降级 developing + 冲突项（§3.3-7）。
			record.Status = novel.ThreadDeveloping
			conflictDetail = "resolve 提案缺少已定位的解决证据，降级为 developing"
		case action == "update":
			record.Status = novel.ThreadDeveloping
		default:
			record.Status = novel.ThreadOpen
		}
		if !hasEvidence {
			record.NeedsReview = true
			record.ReviewReason = "evidence_unlocated"
		}
		record.Normalize(now)

		if conflictDetail != "" {
			cs.Conflicts = append(cs.Conflicts, novel.ConflictItem{
				Kind:     novel.ConflictResolvedNoProof,
				Detail:   conflictDetail,
				Proposed: mustMarshal(record),
			})
		}

		existing := matchThreadByTitle(existingThreads, title)
		if existing != nil && existing.LastSource == novel.LastSourceManualEdit {
			// I2-B 暂存时点：人工修订的伏笔不被自动覆盖。
			cs.Conflicts = append(cs.Conflicts, novel.ConflictItem{
				Kind:                  novel.ConflictManualEdit,
				TargetID:              existing.ID,
				ExpectedRecordVersion: existing.RecordVersion,
				Old:                   mustMarshal(*existing),
				Proposed:              mustMarshal(record),
				Detail:                "既有伏笔由人工修订，提取结果不覆盖",
			})
			continue
		}
		if existing != nil {
			cs.Ops = append(cs.Ops, novel.ChangeOp{
				Op:                    novel.OpUpdateThread,
				TargetID:              existing.ID,
				ExpectedRecordVersion: existing.RecordVersion,
				Record:                mustMarshal(record),
				EvidenceNote:          evidenceNote,
			})
		} else {
			cs.Ops = append(cs.Ops, novel.ChangeOp{
				Op:           novel.OpAddThread,
				Record:       mustMarshal(record),
				EvidenceNote: evidenceNote,
			})
		}
	}

	// ---- 实体提示 → 候选（pending）----
	for _, hint := range parsed.EntityHints {
		name := strings.TrimSpace(hint.Name)
		if name == "" {
			continue
		}
		// 已能唯一归并到既有实体则不重复产出候选；歧义名称保留正确的原因码。
		_, ambiguous, unknown := resolveEntity(name, hint.Aliases, knowledge)
		if !ambiguous && !unknown {
			continue
		}
		reason := "unknown_entity"
		if ambiguous {
			reason = "ambiguous_alias"
		}
		candidates = append(candidates, novel.EntityCandidate{
			Name:      name,
			Aliases:   cleanAliasSlice(hint.Aliases),
			Reason:    reason,
			Status:    string(novel.CandidatePending),
			CreatedAt: now,
		})
	}

	return cs, candidates
}

// ---- 实体解析辅助 ----

// resolveEntity 解析一个名称到既有实体：命中角色返回 c_，命中世界条目返回 w_；
// 任一歧义 → ambiguous=true；两者均未命中 → 返回空（unknown）。
func resolveEntity(name string, aliases []string, knowledge novel.Knowledge) (id string, ambiguous bool, unknown bool) {
	cIdx, cAmbiguous := novel.MatchCharacter(knowledge.Characters, name, aliases)
	if cAmbiguous {
		return "", true, false
	}
	if cIdx >= 0 {
		return knowledge.Characters[cIdx].ID, false, false
	}
	wIdx, wAmbiguous := novel.MatchWorldEntry(knowledge.World, name, aliases)
	if wAmbiguous {
		return "", true, false
	}
	if wIdx >= 0 {
		return knowledge.World[wIdx].ID, false, false
	}
	return "", false, true
}

// resolveParticipants 解析事件参与者：逐名 resolveEntity；唯一命中 → 收集 ID；
// 歧义 → 产出 ambiguous_alias 候选并标记；未知 → 产出 unknown_entity 候选且 participantsKnown=false。
func resolveParticipants(names []string, namesProvided bool, knowledge novel.Knowledge) (ids []string, known bool, reviewReason string, candidates []novel.EntityCandidate) {
	now := time.Now().UTC()
	ids = []string{}
	known = true
	hasUnknown := false
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		id, ambiguous, unknown := resolveEntity(name, nil, knowledge)
		switch {
		case ambiguous:
			candidates = append(candidates, novel.EntityCandidate{
				Name: name, Reason: "ambiguous_alias", Status: string(novel.CandidatePending), CreatedAt: now,
			})
			reviewReason = string(novel.ConflictAmbiguous)
		case unknown:
			candidates = append(candidates, novel.EntityCandidate{
				Name: name, Reason: "unknown_entity", Status: string(novel.CandidatePending), CreatedAt: now,
			})
			hasUnknown = true
		default:
			ids = append(ids, id)
		}
	}
	if !namesProvided && len(names) == 0 {
		known = true // 模型明确声明无参与者。
	} else if hasUnknown {
		known = false
	}
	return ids, known, reviewReason, candidates
}

// resolveSubject 解析状态主体（同 resolveEntity），歧义/未知均产出候选并标记。
func resolveSubject(name string, knowledge novel.Knowledge) (id string, reviewReason string, candidates []novel.EntityCandidate) {
	now := time.Now().UTC()
	id, ambiguous, unknown := resolveEntity(name, nil, knowledge)
	switch {
	case ambiguous:
		candidates = append(candidates, novel.EntityCandidate{
			Name: name, Reason: "ambiguous_alias", Status: string(novel.CandidatePending), CreatedAt: now,
		})
		reviewReason = string(novel.ConflictAmbiguous)
	case unknown:
		candidates = append(candidates, novel.EntityCandidate{
			Name: name, Reason: "unknown_entity", Status: string(novel.CandidatePending), CreatedAt: now,
		})
		reviewReason = "unknown_entity"
	}
	return id, reviewReason, candidates
}

// locateQuoteRefs 在服务端定位 quote 于正文的 rune 偏移（不信模型给的偏移）；定位失败返回空（证据缺失）。
// 多处命中取第一处并返回 evidenceNote=quote_multi_hit（§3.3-6）。
func locateQuoteRefs(chapter novel.Chapter, quote string) ([]novel.SourceRef, string) {
	quote = strings.TrimSpace(quote)
	if quote == "" {
		return nil, ""
	}
	start, end, hits, ok := locateQuote(chapter.Content, quote)
	if !ok {
		return nil, ""
	}
	note := ""
	if hits > 1 {
		note = "quote_multi_hit"
	}
	return []novel.SourceRef{{
		ChapterID:       chapter.ID,
		ChapterRevision: chapter.Revision,
		StartOffset:     start,
		EndOffset:       end,
		OffsetUnit:      "rune",
		Quote:           quote,
	}}, note
}

// locateQuote 在 content 中按字节定位 quote 子串，并换算为 rune 起止偏移与命中次数。
func locateQuote(content, quote string) (start, end, hits int, ok bool) {
	idx := strings.Index(content, quote)
	if idx < 0 {
		return 0, 0, 0, false
	}
	start = utf8.RuneCountInString(content[:idx])
	end = start + utf8.RuneCountInString(quote)
	hits = strings.Count(content, quote)
	return start, end, hits, true
}

// matchResolutionEvent 为 resolved 伏笔寻找可绑定的解决事件：取本章已组装事件中
// 证据原文与解决证据互相包含的第一条（spec：resolved 必须绑定 resolutionEvent）。
func matchResolutionEvent(events []novel.StoryEvent, quote string) string {
	quote = strings.TrimSpace(quote)
	if quote == "" {
		return ""
	}
	for _, ev := range events {
		for _, ref := range ev.SourceRefs {
			if ref.Quote == "" {
				continue
			}
			if strings.Contains(quote, ref.Quote) || strings.Contains(ref.Quote, quote) {
				return ev.ID
			}
		}
	}
	return ""
}

// matchActiveState 按三元组（subjectId+category+attribute）查找现有 active 状态。
func matchActiveState(states []novel.StoryState, subjectID, category, attribute string) *novel.StoryState {
	for i := range states {
		if states[i].Status == novel.StateActive &&
			states[i].SubjectID == subjectID &&
			states[i].Category == category &&
			states[i].Attribute == attribute {
			return &states[i]
		}
	}
	return nil
}

// matchThreadByTitle 按标题唯一匹配现有伏笔（歧义不自动合并，返回 nil 交由新增）。
func matchThreadByTitle(threads []novel.StoryThread, title string) *novel.StoryThread {
	var hit *novel.StoryThread
	count := 0
	for i := range threads {
		if threads[i].Title == title {
			hit = &threads[i]
			count++
		}
	}
	if count == 1 {
		return hit
	}
	return nil
}

// toEventLinks 将字符串列表转为 EventLink（仅保留非空）。
func toEventLinks(items []string) []novel.EventLink {
	if len(items) == 0 {
		return nil
	}
	links := make([]novel.EventLink, 0, len(items))
	for _, it := range items {
		it = strings.TrimSpace(it)
		if it == "" {
			continue
		}
		links = append(links, novel.EventLink{Text: clip(it, 120)})
	}
	return links
}

func normalizeSummary(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(s)), ""))
}

func cleanAliasSlice(aliases []string) []string {
	out := make([]string, 0, len(aliases))
	for _, a := range aliases {
		a = strings.TrimSpace(a)
		if a != "" {
			out = append(out, a)
		}
	}
	return out
}

func mustMarshal(v any) json.RawMessage {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return raw
}

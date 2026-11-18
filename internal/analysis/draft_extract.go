package analysis

import (
	"context"
	"fmt"
	"strings"

	"novelagent/internal/llm"
	"novelagent/internal/novel"
	"novelagent/internal/store"
)

// Draft extraction（G6）：在正文进入正式区之前，从待确认稿生成 ChangeSet。
// 产出的 ChangeSet 绑定 draftId + draftRevision + 目标章节的“确认后版本号”，
// 编辑草稿会使它失效，必须重新提取；确认时正文与这批记忆变更同一次提交。

// ExtractDraft 同步执行一次草稿深度提取（单次 LLM 调用）。
func (l *ExtractLauncher) ExtractDraft(ctx context.Context, draft novel.Draft) (novel.Draft, novel.ChangeSet, error) {
	if strings.TrimSpace(draft.Content) == "" {
		return novel.Draft{}, novel.ChangeSet{}, fmt.Errorf("%w: draft content is empty", store.ErrDraftInvalid)
	}
	client, err := l.configuredClient()
	if err != nil {
		return novel.Draft{}, novel.ChangeSet{}, err
	}
	return l.extractDraftWithClient(ctx, client, draft)
}

func (l *ExtractLauncher) configuredClient() (*llm.Client, error) {
	if l.clientFn != nil {
		client, err := l.clientFn()
		if err != nil {
			return nil, err
		}
		return client, nil
	}
	cfg, err := l.st.LoadConfig()
	if err != nil {
		return nil, err
	}
	key, _ := cfg.EffectiveAPIKey()
	if key == "" || cfg.BaseURL == "" || cfg.Model == "" {
		return nil, store.ErrExtractConfigMissing
	}
	return llm.NewClient(cfg.BaseURL, key, cfg.Model), nil
}

func (l *ExtractLauncher) extractDraftWithClient(ctx context.Context, client *llm.Client, draft novel.Draft) (novel.Draft, novel.ChangeSet, error) {
	project, err := l.st.LoadProject()
	if err != nil {
		return novel.Draft{}, novel.ChangeSet{}, err
	}
	if project.Analysis.Status == novel.AnalysisRunning {
		return novel.Draft{}, novel.ChangeSet{}, store.ErrAnalysisRunning
	}
	knowledge, err := l.st.LoadKnowledge()
	if err != nil {
		return novel.Draft{}, novel.ChangeSet{}, err
	}
	if strings.TrimSpace(knowledge.Overview) == "" && len(knowledge.Characters) == 0 &&
		len(knowledge.World) == 0 && len(knowledge.Summaries) == 0 {
		return novel.Draft{}, novel.ChangeSet{}, store.ErrKnowledgeNotReady
	}
	events, states, _, threads, _, err := l.st.LoadMemoryRecords()
	if err != nil {
		return novel.Draft{}, novel.ChangeSet{}, err
	}
	info, err := l.st.LoadMemoryInfo()
	if err != nil {
		return novel.Draft{}, novel.ChangeSet{}, err
	}
	pendingEvents, err := l.st.LoadPendingEventRecords(draft.TargetChapterID)
	if err != nil {
		l.log.Warn("load pending extract events for draft", "draft", draft.ID, "err", err)
	}

	index := len(project.Chapters) + 1
	if !draft.NewChapter {
		for _, meta := range project.Chapters {
			if meta.ID == draft.TargetChapterID {
				index = meta.Index
				break
			}
		}
	}
	chapter := novel.Chapter{
		ID:        draft.TargetChapterID,
		Index:     index,
		Title:     draft.Title,
		Content:   draft.Content,
		CharCount: novel.CountChars(draft.Content),
		Revision:  draft.PendingChapterRevision(),
	}
	related := relatedMemoryDigest(events, states, threads, chapter.ID)
	messages := buildExtractMessages(project.Title, chapter, knowledge, related)
	reply, err := completeWithTimeout(ctx, client, messages, 0.2)
	if err != nil {
		return novel.Draft{}, novel.ChangeSet{}, err
	}
	var parsed extractResult
	if err := parseJSONObject(reply.Content, &parsed); err != nil {
		if llm.IsTruncated(reply.FinishReason) {
			return novel.Draft{}, novel.ChangeSet{}, fmt.Errorf("draft extract: 模型输出被截断，请缩短草稿或调大输出上限")
		}
		return novel.Draft{}, novel.ChangeSet{}, fmt.Errorf("draft extract: decode model output: %w", err)
	}
	dedupEvents := append(append([]novel.StoryEvent{}, events...), pendingEvents...)
	cs, candidates := assembleChapterChangeSet(chapter, knowledge, parsed, dedupEvents, states, threads, info.MemoryRevision)

	updated, stored, err := l.st.StashDraftChangeSet(draft.ID, draft.Revision, cs, candidates)
	if err != nil {
		return novel.Draft{}, novel.ChangeSet{}, err
	}
	return updated, stored, nil
}

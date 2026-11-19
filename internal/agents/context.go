package agents

import (
	"strings"
	"unicode/utf8"

	"novelagent/internal/agentruntime"
	"novelagent/internal/novel"
	"novelagent/internal/store"
)

// 本文件放 A1/A3/A4 共用的只读上下文加载与文本工具。

type projectContext struct {
	project   novel.Project
	knowledge novel.Knowledge
	events    []novel.StoryEvent
	states    []novel.StoryState
	cogs      []novel.Cognition
	threads   []novel.StoryThread
	plans     []novel.StoryPlan
}

func loadProjectContext(st *store.Store) (projectContext, error) {
	ctx := projectContext{}
	project, err := st.LoadProject()
	if err != nil {
		return ctx, err
	}
	ctx.project = project
	knowledge, err := st.LoadKnowledge()
	if err != nil {
		return ctx, err
	}
	ctx.knowledge = knowledge
	events, states, cogs, threads, plans, err := st.LoadMemoryRecords()
	if err != nil {
		return ctx, err
	}
	ctx.events, ctx.states, ctx.cogs, ctx.threads, ctx.plans = events, states, cogs, threads, plans
	return ctx, nil
}

func currentChapter(st *store.Store, ev agentruntime.Event) (novel.Chapter, bool) {
	if ev.ChapterID == "" {
		return novel.Chapter{}, false
	}
	chapter, err := st.LoadChapter(ev.ChapterID)
	if err != nil {
		return novel.Chapter{}, false
	}
	return chapter, true
}

// chapterForAgent 优先使用事件指向的章节；无章节事件时退化为全书最后一章。
func chapterForAgent(st *store.Store, ev agentruntime.Event, ctx projectContext) (novel.Chapter, bool) {
	if chapter, ok := currentChapter(st, ev); ok {
		return chapter, true
	}
	if len(ctx.project.Chapters) == 0 {
		return novel.Chapter{}, false
	}
	last := ctx.project.Chapters[len(ctx.project.Chapters)-1]
	chapter, err := st.LoadChapter(last.ID)
	if err != nil {
		return novel.Chapter{}, false
	}
	return chapter, true
}

// canPropose 对应权限档位：observe 只报告；propose/autonomous 才允许写审阅提案。
func canPropose(in agentruntime.Input) bool {
	return in.Permission == agentruntime.PermissionPropose || in.Permission == agentruntime.PermissionAutonomous
}

func chapterIndexOf(ctx projectContext, chapterID string) int {
	for _, chapter := range ctx.project.Chapters {
		if chapter.ID == chapterID {
			return chapter.Index
		}
	}
	return 0
}

// quoteAround 返回文本中包含 key 的句子（截断到 limit 个字符），作为证据引用。
func quoteAround(content, key string, limit int) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	for _, sentence := range splitSentences(content) {
		if strings.Contains(sentence, key) {
			return truncateRunes(sentence, limit)
		}
	}
	return ""
}

func splitSentences(content string) []string {
	replacer := strings.NewReplacer("\r\n", "\n", "\r", "\n")
	content = replacer.Replace(content)
	fields := strings.FieldsFunc(content, func(r rune) bool {
		switch r {
		case '。', '！', '？', '；', '\n', '.', '!', '?':
			return true
		}
		return false
	})
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		if trimmed := strings.TrimSpace(field); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func truncateRunes(text string, limit int) string {
	if limit <= 0 || utf8.RuneCountInString(text) <= limit {
		return text
	}
	runes := []rune(text)
	return string(runes[:limit]) + "…"
}

// mentionsAny 判断正文是否提及实体名称或别名（朴素包含，确定性优先）。
func mentionsAny(content string, name string, aliases []string) bool {
	if name != "" && strings.Contains(content, name) {
		return true
	}
	for _, alias := range aliases {
		alias = strings.TrimSpace(alias)
		if alias != "" && strings.Contains(content, alias) {
			return true
		}
	}
	return false
}

func compactExcerpt(content string, limit int) string {
	content = strings.Join(strings.Fields(content), " ")
	return truncateRunes(content, limit)
}

func severityOf(levels ...string) string {
	rank := map[string]int{"": 0, "info": 1, "warning": 2, "blocking": 3}
	best := ""
	for _, level := range levels {
		if rank[level] > rank[best] {
			best = level
		}
	}
	return best
}

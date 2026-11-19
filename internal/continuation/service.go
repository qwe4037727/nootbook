// Package continuation 提供非流式多候选续写：章节内任意位置起点、1~3 版本并排生成。
// 生成结果只落 continue_group/continue_candidate 审计表，绝不自动写正文或记忆。
package continuation

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"novelagent/internal/agent"
	"novelagent/internal/llm"
	"novelagent/internal/novel"
	"novelagent/internal/store"
)

var (
	ErrConfigMissing       = errors.New("continuation: model config missing")
	ErrNoContent           = errors.New("continuation: no chapter content to continue from")
	ErrStartInvalid        = errors.New("continuation: invalid start point")
	ErrAllCandidatesFailed = errors.New("continuation: all candidates failed")
)

const candidateCallTimeout = 8 * time.Minute

type Service struct {
	st  *store.Store
	log *slog.Logger
}

func NewService(st *store.Store, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{st: st, log: logger}
}

type Request struct {
	// StartMode 为空/full 表示从全书末尾续写；chapter_offset 表示从指定章节 rune 偏移处续写。
	StartMode   string  `json:"startMode"`
	ChapterID   string  `json:"chapterId"`
	StartOffset int     `json:"startOffset"`
	Direction   string  `json:"direction"`
	TargetChars int     `json:"targetChars"`
	Candidates  int     `json:"candidates"`
	Temperature float64 `json:"temperature"`
	UseMemory   *bool   `json:"useMemory"`
	UsePlan     *bool   `json:"usePlan"`
	UseRecall   *bool   `json:"useRecall"`
}

type Result struct {
	Group      novel.ContinuationGroup       `json:"group"`
	Candidates []novel.ContinuationCandidate `json:"candidates"`
}

// SliceChapters 按章节内 rune 偏移裁剪正文视图：
// 返回目标章节之前的全部章节 + 目标章节的前 offset 个 rune，以及目标章节序号。
func SliceChapters(chapters []novel.Chapter, chapterID string, offset int) ([]novel.Chapter, int, error) {
	chapterID = strings.TrimSpace(chapterID)
	if chapterID == "" {
		return nil, 0, fmt.Errorf("%w: chapterId is required", ErrStartInvalid)
	}
	if offset < 0 {
		return nil, 0, fmt.Errorf("%w: startOffset must be >= 0", ErrStartInvalid)
	}
	targetIndex := -1
	position := -1
	for i, chapter := range chapters {
		if chapter.ID == chapterID {
			targetIndex = chapter.Index
			position = i
			break
		}
	}
	if targetIndex < 0 {
		return nil, 0, fmt.Errorf("%w: chapter not found", ErrStartInvalid)
	}
	runes := []rune(chapters[position].Content)
	if offset > len(runes) {
		return nil, 0, fmt.Errorf("%w: startOffset %d exceeds chapter length %d", ErrStartInvalid, offset, len(runes))
	}
	out := make([]novel.Chapter, 0, len(chapters))
	for _, chapter := range chapters {
		if chapter.Index < targetIndex {
			out = append(out, chapter)
		}
	}
	target := chapters[position]
	target.Content = string(runes[:offset])
	out = append(out, target)
	return out, targetIndex, nil
}

func (s *Service) Generate(ctx context.Context, req Request) (Result, error) {
	return s.generate(ctx, req, nil)
}

// GenerateWithExtra 在已落盘章节之后追加内存中的待确认章节（批量续写用），
// 使连续多章生成能读到上一章尚未提交的正文。
func (s *Service) GenerateWithExtra(ctx context.Context, req Request, extra []novel.Chapter) (Result, error) {
	return s.generate(ctx, req, extra)
}

func (s *Service) generate(ctx context.Context, req Request, extra []novel.Chapter) (Result, error) {
	mode := strings.ToLower(strings.TrimSpace(req.StartMode))
	switch mode {
	case "", novel.ContinuationStartFull:
		mode = novel.ContinuationStartFull
	case novel.ContinuationStartChapterOffset:
	default:
		return Result{}, fmt.Errorf("%w: startMode must be full or chapter_offset", ErrStartInvalid)
	}
	req.Direction = strings.TrimSpace(req.Direction)
	if utf8.RuneCountInString(req.Direction) > 2000 {
		return Result{}, fmt.Errorf("%w: direction exceeds 2000 runes", ErrStartInvalid)
	}
	if req.TargetChars <= 0 {
		req.TargetChars = 800
	}
	req.TargetChars = min(max(req.TargetChars, 200), 4000)
	candidateCount := novel.NormalizeCandidateCount(req.Candidates)
	if req.Temperature <= 0 {
		req.Temperature = 0.85
	}
	req.Temperature = min(req.Temperature, 1.5)

	chapters, err := s.st.LoadChapters()
	if err != nil {
		return Result{}, err
	}
	if len(extra) > 0 {
		chapters = append(chapters, extra...)
	}
	hasContent := false
	for _, chapter := range chapters {
		if strings.TrimSpace(chapter.Content) != "" {
			hasContent = true
			break
		}
	}
	if !hasContent {
		return Result{}, ErrNoContent
	}
	startIndex := 0
	if mode == novel.ContinuationStartChapterOffset {
		chapters, startIndex, err = SliceChapters(chapters, req.ChapterID, req.StartOffset)
		if err != nil {
			return Result{}, err
		}
		req.ChapterID = strings.TrimSpace(req.ChapterID)
	}

	project, err := s.st.LoadProject()
	if err != nil {
		return Result{}, err
	}
	cfg, err := s.st.LoadConfig()
	if err != nil {
		return Result{}, err
	}
	if cfg.BaseURL == "" || cfg.Model == "" {
		return Result{}, ErrConfigMissing
	}
	knowledge, err := s.st.LoadKnowledge()
	if err != nil {
		return Result{}, err
	}

	useMemory := req.UseMemory == nil || *req.UseMemory
	var memory *novel.ContinuationMemory
	recent, windowStart := agent.ContinuationWindowFor(chapters, cfg.ContextChars, req.UseRecall)
	if useMemory {
		retrieved, err := s.st.RetrieveContinuationMemory(knowledge, recent, windowStart, novel.MemoryBudgetChars(cfg.ContextChars))
		if err != nil {
			s.log.Warn("retrieve continuation memory", "err", err)
		}
		memory = &retrieved
	}
	if mode == novel.ContinuationStartFull {
		startIndex = windowStart
	}
	usePlan := req.UsePlan == nil || *req.UsePlan
	var plans []novel.StoryPlan
	if usePlan {
		loaded, _, err := s.st.LoadActivePlans()
		if err != nil {
			s.log.Warn("load active plans for continuation", "err", err)
		} else {
			plans = loaded
		}
	}

	prompt := agent.Build(agent.Input{
		Title:        project.Title,
		Knowledge:    knowledge,
		Chapters:     chapters,
		Direction:    req.Direction,
		TargetChars:  req.TargetChars,
		ContextChars: cfg.ContextChars,
		Memory:       memory,
		Plans:        plans,
		UseRecall:    req.UseRecall,
	})
	group := novel.ContinuationGroup{
		StartMode:      mode,
		ChapterID:      req.ChapterID,
		StartOffset:    req.StartOffset,
		StartIndex:     startIndex,
		Direction:      req.Direction,
		TargetChars:    req.TargetChars,
		CandidateCount: candidateCount,
		Temperature:    req.Temperature,
		Model:          cfg.Model,
		PromptChars:    prompt.TotalChars,
		ContextChars:   cfg.ContextChars,
		MemoryChars:    utf8.RuneCountInString(prompt.MemorySection),
	}
	messages := []llm.Message{
		{Role: "system", Content: prompt.System},
		{Role: "user", Content: prompt.User},
	}

	key, _ := cfg.EffectiveAPIKey()
	client := llm.NewClient(cfg.BaseURL, key, cfg.Model)
	type outcome struct {
		index  int
		tokens llm.Result
		err    error
	}
	outcomes := make(chan outcome, candidateCount)
	var wg sync.WaitGroup
	for i := 0; i < candidateCount; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			temperature := min(req.Temperature+0.05*float64(index), 1.5)
			callCtx, cancel := context.WithTimeout(ctx, candidateCallTimeout)
			defer cancel()
			res, err := client.CompleteChat(callCtx, messages, temperature)
			outcomes <- outcome{index: index, tokens: res, err: err}
		}(i)
	}
	wg.Wait()
	close(outcomes)

	collected := make([]outcome, candidateCount)
	for out := range outcomes {
		collected[out.index] = out
	}
	candidates := make([]novel.ContinuationCandidate, 0, candidateCount)
	var firstErr error
	done := 0
	for i, out := range collected {
		candidate := novel.ContinuationCandidate{
			Index:       i + 1,
			Temperature: min(req.Temperature+0.05*float64(i), 1.5),
			Status:      "done",
		}
		if out.err != nil {
			candidate.Status = "failed"
			candidate.Error = out.err.Error()
			if firstErr == nil {
				firstErr = out.err
			}
		} else {
			candidate.Content = cleanupContinuation(out.tokens.Content)
			candidate.CharCount = novel.CountChars(candidate.Content)
			candidate.PromptTokens = out.tokens.PromptTokens
			candidate.CompletionTokens = out.tokens.CompletionTokens
			candidate.TotalTokens = out.tokens.TotalTokens
			done++
		}
		candidates = append(candidates, candidate)
	}

	saved, err := s.st.SaveContinuationGroup(group, candidates)
	if err != nil {
		return Result{}, err
	}
	result := Result{Group: saved, Candidates: candidates}
	if done == 0 {
		if firstErr == nil {
			firstErr = errors.New("model returned no candidates")
		}
		return result, fmt.Errorf("%w: %v", ErrAllCandidatesFailed, firstErr)
	}
	return result, nil
}

// cleanupContinuation 去掉模型偶发包裹的整段代码围栏，保留正文。
func cleanupContinuation(text string) string {
	trimmed := strings.TrimSpace(text)
	if !strings.HasPrefix(trimmed, "```") || !strings.HasSuffix(trimmed, "```") {
		return trimmed
	}
	lines := strings.Split(trimmed, "\n")
	if len(lines) < 3 {
		return trimmed
	}
	lines = lines[1 : len(lines)-1]
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

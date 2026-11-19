package adapt

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"novelagent/internal/llm"
	"novelagent/internal/novel"
	"novelagent/internal/store"
)

// M3 独立改编工作台：选区重写 → diff → 影响推演（模式B）→ 落草稿（绝不自动提交记忆）。
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

var (
	ErrInvalidRequest  = errors.New("adapt: invalid request")
	ErrChapterNotFound = errors.New("adapt: chapter not found")
	ErrConfigMissing   = errors.New("adapt: model config missing")
	ErrTruncated       = errors.New("adapt: model output truncated")
)

type Request struct {
	ChapterID       string  `json:"chapterId"`
	StartOffset     int     `json:"startOffset"`
	EndOffset       int     `json:"endOffset"`
	Instruction     string  `json:"instruction"`
	InstructionType string  `json:"instructionType"`
	Mode            string  `json:"mode"`
	Style           string  `json:"style"`
	Perspective     string  `json:"perspective"`
	Temperature     float64 `json:"temperature"`
	TargetChars     int     `json:"targetChars"`
}

type Usage struct {
	PromptTokens     int `json:"promptTokens"`
	CompletionTokens int `json:"completionTokens"`
	TotalTokens      int `json:"totalTokens"`
}

type Result struct {
	Run             store.AdaptRunRecord `json:"run"`
	Draft           novel.Draft          `json:"draft"`
	ChapterID       string               `json:"chapterId"`
	ChapterIndex    int                  `json:"chapterIndex"`
	Original        string               `json:"original"`
	Rewritten       string               `json:"rewritten"`
	FullContent     string               `json:"fullContent"`
	Diff            string               `json:"diff"`
	DiffParts       []DiffPart           `json:"diffParts"`
	Impact          Impact               `json:"impact"`
	Usage           Usage                `json:"usage"`
	MemoryUntouched bool                 `json:"memoryUntouched"`
	MemoryModeNote  string               `json:"memoryModeNote"`
}

const adaptCallTimeout = 8 * time.Minute

var instructionTypes = map[string]bool{
	"": true, "rewrite": true, "polish": true, "expand": true, "shorten": true, "style": true, "perspective": true,
}

func (s *Service) Run(ctx context.Context, req Request) (Result, error) {
	mode := strings.ToUpper(strings.TrimSpace(req.Mode))
	if mode == "" {
		mode = "A"
	}
	if mode != "A" && mode != "B" {
		return Result{}, fmt.Errorf("%w: mode must be A or B", ErrInvalidRequest)
	}
	req.InstructionType = strings.ToLower(strings.TrimSpace(req.InstructionType))
	if !instructionTypes[req.InstructionType] {
		return Result{}, fmt.Errorf("%w: unsupported instructionType", ErrInvalidRequest)
	}
	req.Instruction = strings.TrimSpace(req.Instruction)
	if req.Instruction == "" {
		return Result{}, fmt.Errorf("%w: instruction is required", ErrInvalidRequest)
	}
	if utf8.RuneCountInString(req.Instruction) > 2000 {
		return Result{}, fmt.Errorf("%w: instruction exceeds 2000 runes", ErrInvalidRequest)
	}
	if req.TargetChars < 0 || req.TargetChars > 20000 {
		return Result{}, fmt.Errorf("%w: targetChars out of range", ErrInvalidRequest)
	}

	chapter, err := s.st.LoadChapter(req.ChapterID)
	if err != nil {
		if errors.Is(err, store.ErrChapterNotFound) || errors.Is(err, store.ErrInvalidChapter) {
			return Result{}, ErrChapterNotFound
		}
		return Result{}, err
	}
	runes := []rune(chapter.Content)
	if req.StartOffset < 0 || req.EndOffset <= req.StartOffset || req.EndOffset > len(runes) {
		return Result{}, fmt.Errorf("%w: invalid selection interval [%d,%d)", ErrInvalidRequest, req.StartOffset, req.EndOffset)
	}
	selection := string(runes[req.StartOffset:req.EndOffset])

	cfg, err := s.st.LoadConfig()
	if err != nil {
		return Result{}, err
	}
	if cfg.BaseURL == "" || cfg.Model == "" {
		return Result{}, ErrConfigMissing
	}
	if req.Temperature <= 0 {
		req.Temperature = 0.7
	}
	req.Temperature = min(req.Temperature, 1.5)

	key, _ := cfg.EffectiveAPIKey()
	client := llm.NewClient(cfg.BaseURL, key, cfg.Model)
	prompt := buildAdaptPrompt(chapter, req, cfg.ContextChars)

	callCtx, cancel := context.WithTimeout(ctx, adaptCallTimeout)
	defer cancel()
	reply, err := client.CompleteChat(callCtx, []llm.Message{
		{Role: "system", Content: adaptSystemPrompt},
		{Role: "user", Content: prompt},
	}, req.Temperature)
	if err != nil {
		s.recordFailure(chapter, mode, req, cfg.Model, err)
		return Result{}, err
	}
	rewritten := cleanCompletion(reply.Content)
	if rewritten == "" {
		s.recordFailure(chapter, mode, req, cfg.Model, errors.New("empty completion"))
		return Result{}, fmt.Errorf("adapt: model returned empty text")
	}
	if llm.IsTruncated(reply.FinishReason) {
		s.recordFailure(chapter, mode, req, cfg.Model, ErrTruncated)
		return Result{}, ErrTruncated
	}

	fullContent := string(runes[:req.StartOffset]) + rewritten + string(runes[req.EndOffset:])
	diffText, diffParts := Diff(selection, rewritten)

	impact := Impact{
		Mode:             mode,
		ChangedEntities:  []ImpactEntity{},
		AffectedChapters: []ImpactChapter{},
		Foreshadows:      []ImpactForeshadow{},
		MemoryEvents:     []ImpactMemoryEvent{},
		Warnings:         []string{},
	}
	if mode == "B" {
		analyzed, err := AnalyzeImpact(s.st, ImpactInput{
			ChapterID: chapter.ID, ChapterIndex: chapter.Index,
			Mode: mode, OldSelection: selection, NewSelection: rewritten,
		})
		if err != nil {
			s.log.Error("adapt impact analysis", "chapter", chapter.ID, "err", err)
			impact.Warnings = append(impact.Warnings, "影响推演失败："+err.Error())
		} else {
			impact = analyzed
		}
	}

	draft, err := s.st.CreateDraft(store.DraftCreateParams{
		ChapterID: chapter.ID,
		Title:     chapter.Title,
		Content:   fullContent,
	})
	if err != nil {
		s.recordFailure(chapter, mode, req, cfg.Model, err)
		return Result{}, err
	}

	impactJSON, _ := json.Marshal(impact)
	note := "模式A：不改世界状态，不触发记忆重抽取；结果仅存草稿"
	if mode == "B" {
		note = "模式B：已推演影响清单，但记忆变更仍不自动提交；需经草稿确认链路"
	}
	run, err := s.st.SaveAdaptRun(store.AdaptRunRecord{
		ChapterID:        chapter.ID,
		ChapterIndex:     chapter.Index,
		Mode:             mode,
		InstructionType:  req.InstructionType,
		Instruction:      req.Instruction,
		StartOffset:      req.StartOffset,
		EndOffset:        req.EndOffset,
		OldChars:         utf8.RuneCountInString(selection),
		NewChars:         utf8.RuneCountInString(rewritten),
		Style:            strings.TrimSpace(req.Style),
		Perspective:      strings.TrimSpace(req.Perspective),
		Model:            cfg.Model,
		PromptTokens:     reply.PromptTokens,
		CompletionTokens: reply.CompletionTokens,
		TotalTokens:      reply.TotalTokens,
		DraftID:          draft.ID,
		Impact:           impactJSON,
		Status:           "done",
	})
	if err != nil {
		s.log.Error("record adapt run", "err", err)
	}

	return Result{
		Run:             run,
		Draft:           draft,
		ChapterID:       chapter.ID,
		ChapterIndex:    chapter.Index,
		Original:        selection,
		Rewritten:       rewritten,
		FullContent:     fullContent,
		Diff:            diffText,
		DiffParts:       diffParts,
		Impact:          impact,
		Usage:           Usage{PromptTokens: reply.PromptTokens, CompletionTokens: reply.CompletionTokens, TotalTokens: reply.TotalTokens},
		MemoryUntouched: true,
		MemoryModeNote:  note,
	}, nil
}

func (s *Service) recordFailure(chapter novel.Chapter, mode string, req Request, model string, cause error) {
	if _, err := s.st.SaveAdaptRun(store.AdaptRunRecord{
		ChapterID:       chapter.ID,
		ChapterIndex:    chapter.Index,
		Mode:            mode,
		InstructionType: req.InstructionType,
		Instruction:     req.Instruction,
		StartOffset:     req.StartOffset,
		EndOffset:       req.EndOffset,
		OldChars:        req.EndOffset - req.StartOffset,
		Style:           strings.TrimSpace(req.Style),
		Perspective:     strings.TrimSpace(req.Perspective),
		Model:           model,
		Status:          "failed",
		Error:           truncateRunes(cause.Error(), 300),
	}); err != nil {
		s.log.Error("record failed adapt run", "err", err)
	}
}

func (s *Service) Runs(limit int) ([]store.AdaptRunRecord, error) {
	return s.st.ListAdaptRuns(limit)
}

func (s *Service) RunByID(id string) (store.AdaptRunRecord, error) {
	return s.st.GetAdaptRun(id)
}

const adaptSystemPrompt = `你是资深中文长篇小说编辑。按用户给出的选区与指令改写文字：
- 只输出改写后的文字本身，不要解释、不要寒暄、不要代码块或引号包裹；
- 严格保持人物设定与已发生事实，不得引入与上下文冲突的新设定；
- 保持与前后文的语体一致，除非指令明确要求换文风或换叙事视角。`

func buildAdaptPrompt(chapter novel.Chapter, req Request, contextChars int) string {
	if contextChars <= 0 {
		contextChars = 12000
	}
	prefix, suffix := chapterExcerpt(chapter.Content, req.StartOffset, req.EndOffset, contextChars)
	selection := string([]rune(chapter.Content)[req.StartOffset:req.EndOffset])

	var b strings.Builder
	b.WriteString("【章节】")
	b.WriteString(chapter.Title)
	b.WriteString("\n【模式】")
	if strings.EqualFold(req.Mode, "B") {
		b.WriteString("B：允许改变世界状态（本次仅推演影响，不直接写记忆）")
	} else {
		b.WriteString("A：不改世界状态")
	}
	if req.InstructionType != "" {
		b.WriteString("\n【任务类型】")
		b.WriteString(req.InstructionType)
	}
	if req.Style != "" {
		b.WriteString("\n【目标文风】")
		b.WriteString(strings.TrimSpace(req.Style))
	}
	if req.Perspective != "" {
		b.WriteString("\n【目标叙事视角】")
		b.WriteString(strings.TrimSpace(req.Perspective))
	}
	if req.TargetChars > 0 {
		fmt.Fprintf(&b, "\n【目标字数】约 %d 字", req.TargetChars)
	}
	b.WriteString("\n【改写指令】")
	b.WriteString(req.Instruction)
	b.WriteString("\n\n【上下文（<<<选区>>> 内为待改写文字）】\n")
	b.WriteString(prefix)
	b.WriteString("<<<选区>>>")
	b.WriteString(selection)
	b.WriteString("<<<选区结束>>>")
	b.WriteString(suffix)
	return b.String()
}

// chapterExcerpt 围绕选区取前后文，避免整章超预算时丢弃选区附近信息。
func chapterExcerpt(content string, start, end, budget int) (string, string) {
	runes := []rune(content)
	remaining := budget - (end - start)
	if remaining <= 0 {
		return "", ""
	}
	before := remaining / 2
	after := remaining - before
	begin := max(start-before, 0)
	finish := min(end+after, len(runes))
	return string(runes[begin:start]), string(runes[end:finish])
}

func cleanCompletion(text string) string {
	text = strings.TrimSpace(strings.TrimPrefix(text, "\ufeff"))
	if strings.HasPrefix(text, "```") {
		if newline := strings.IndexByte(text, '\n'); newline >= 0 {
			text = text[newline+1:]
		}
		if fence := strings.LastIndex(text, "```"); fence >= 0 {
			text = text[:fence]
		}
		text = strings.TrimSpace(text)
	}
	text = strings.Trim(text, "\"“”")
	return strings.TrimSpace(text)
}

func truncateRunes(text string, limit int) string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) <= limit {
		return string(runes)
	}
	return string(runes[:limit]) + "…"
}

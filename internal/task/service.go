// Package task 提供任务调度中心：自然语言解析、持久化队列、批量续写与 Agent 显式执行。
// 单项目单工作协程串行执行，任务暂停/取消在逐章检查点生效，重启后 running 任务转为 paused。
package task

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"novelagent/internal/agentruntime"
	"novelagent/internal/continuation"
	"novelagent/internal/novel"
	"novelagent/internal/store"
)

var (
	ErrInvalidTaskRequest = errors.New("task: invalid request")
	ErrConfigMissing      = errors.New("task: model config missing")
	ErrAgentUnavailable   = errors.New("task: agent unavailable")
)

// Bus 是任务中心对 Agent 运行时的最小依赖（显式执行 + 事件发布）。
type Bus interface {
	HasAgent(key string) bool
	RunAgentOnce(ctx context.Context, key string, ev agentruntime.Event) (agentruntime.Report, string, error)
	Publish(ev agentruntime.Event) bool
}

type Service struct {
	st   *store.Store
	log  *slog.Logger
	bus  Bus
	cont *continuation.Service

	mu     sync.Mutex
	cancel context.CancelFunc
	wg     sync.WaitGroup
	wake   chan struct{}

	// epochs 为每个任务维护执行代次：恢复任务时递增，旧协程在检查点发现代次过期后立即退出，
	// 避免暂停后立刻恢复时新旧两次执行并发生成重复草稿。
	epochMu sync.Mutex
	epochs  map[string]uint64
}

func NewService(st *store.Store, bus Bus, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		st:     st,
		log:    logger,
		bus:    bus,
		cont:   continuation.NewService(st, logger),
		wake:   make(chan struct{}, 1),
		epochs: make(map[string]uint64),
	}
}

type CreateRequest struct {
	Description string          `json:"description"`
	Kind        string          `json:"kind"`
	Priority    string          `json:"priority"`
	Params      json.RawMessage `json:"params"`
}

func (s *Service) Start(ctx context.Context) {
	s.mu.Lock()
	if s.cancel != nil {
		s.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.wg.Add(1)
	s.mu.Unlock()

	if recovered, err := s.st.RecoverRunningTasks(); err != nil {
		s.log.Warn("recover interrupted tasks", "err", err)
	} else if recovered > 0 {
		s.log.Warn("interrupted tasks paused", "count", recovered)
	}
	go func() {
		defer s.wg.Done()
		s.loop(ctx)
	}()
	s.Notify()
}

func (s *Service) Stop() {
	s.mu.Lock()
	cancel := s.cancel
	s.cancel = nil
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	s.wg.Wait()
}

func (s *Service) Notify() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *Service) Create(req CreateRequest) (novel.Task, error) {
	description := strings.TrimSpace(req.Description)
	kind := strings.ToLower(strings.TrimSpace(req.Kind))
	parsedKind, parsedCount, parsedAgent, parsedDirection := parseDescription(description)
	if kind == "" || kind == "auto" {
		kind = parsedKind
	}
	if !novel.ValidTaskKind(kind) {
		return novel.Task{}, fmt.Errorf("%w: unsupported kind %q", ErrInvalidTaskRequest, kind)
	}
	priority := novel.NormalizeTaskPriority(novel.TaskPriority(strings.ToLower(strings.TrimSpace(req.Priority))))
	if description != "" {
		switch {
		case containsAny(description, "紧急", "尽快", "立即", "马上", "高优先"):
			priority = novel.TaskPriorityHigh
		case containsAny(description, "不急", "有空", "低优先"):
			priority = novel.TaskPriorityLow
		}
	}
	if description == "" {
		description = kind
	}

	task := novel.Task{
		ID:          novel.NewID("task"),
		Kind:        kind,
		Description: description,
		Priority:    priority,
		Status:      novel.TaskPending,
	}
	switch kind {
	case novel.TaskKindContinueBatch:
		params := novel.TaskContinueParams{}
		if len(req.Params) > 0 {
			if err := json.Unmarshal(req.Params, &params); err != nil {
				return novel.Task{}, fmt.Errorf("%w: params must be a JSON object", ErrInvalidTaskRequest)
			}
		}
		if params.ChapterCount <= 0 {
			params.ChapterCount = parsedCount
		}
		if params.ChapterCount <= 0 {
			params.ChapterCount = 1
		}
		if params.ChapterCount > 20 {
			return novel.Task{}, fmt.Errorf("%w: chapterCount must be 1..20", ErrInvalidTaskRequest)
		}
		if strings.TrimSpace(params.Direction) == "" {
			params.Direction = parsedDirection
		}
		params.Direction = strings.TrimSpace(params.Direction)
		if len([]rune(params.Direction)) > 2000 {
			return novel.Task{}, fmt.Errorf("%w: direction exceeds 2000 runes", ErrInvalidTaskRequest)
		}
		if params.TargetChars <= 0 {
			params.TargetChars = 800
		}
		params.TargetChars = min(max(params.TargetChars, 200), 4000)
		if params.Temperature <= 0 {
			params.Temperature = 0.85
		}
		params.Temperature = min(params.Temperature, 1.5)
		if err := s.validateBatchStart(params.ChapterCount, params.StartChapterID, params.StartOffset); err != nil {
			return novel.Task{}, err
		}
		cfg, err := s.st.LoadConfig()
		if err != nil {
			return novel.Task{}, err
		}
		if strings.TrimSpace(cfg.BaseURL) == "" || strings.TrimSpace(cfg.Model) == "" {
			return novel.Task{}, ErrConfigMissing
		}
		payload, err := json.Marshal(params)
		if err != nil {
			return novel.Task{}, err
		}
		task.Params = payload
		task.Total = params.ChapterCount
	case novel.TaskKindAgentRun:
		params := novel.TaskAgentParams{}
		if len(req.Params) > 0 {
			if err := json.Unmarshal(req.Params, &params); err != nil {
				return novel.Task{}, fmt.Errorf("%w: params must be a JSON object", ErrInvalidTaskRequest)
			}
		}
		if strings.TrimSpace(params.Agent) == "" {
			params.Agent = parsedAgent
		}
		params.Agent = strings.TrimSpace(params.Agent)
		if params.Agent == "" {
			return novel.Task{}, fmt.Errorf("%w: agent is required", ErrInvalidTaskRequest)
		}
		if s.bus != nil && !s.bus.HasAgent(params.Agent) {
			return novel.Task{}, fmt.Errorf("%w: %s", ErrAgentUnavailable, params.Agent)
		}
		params.ChapterID = strings.TrimSpace(params.ChapterID)
		if params.ChapterID != "" {
			if _, err := s.st.LoadChapter(params.ChapterID); err != nil {
				return novel.Task{}, fmt.Errorf("%w: chapter not found", ErrInvalidTaskRequest)
			}
		}
		payload, err := json.Marshal(params)
		if err != nil {
			return novel.Task{}, err
		}
		task.Params = payload
		task.Total = 1
	}

	created, err := s.st.CreateTask(task)
	if err != nil {
		return novel.Task{}, err
	}
	s.publish(created, novel.TaskEventCreated)
	s.Notify()
	return created, nil
}

func (s *Service) Get(id string) (novel.Task, error) {
	return s.st.GetTask(id)
}

func (s *Service) List(status string, limit int) ([]novel.Task, error) {
	return s.st.ListTasks(status, limit)
}

func (s *Service) Pause(id string) (novel.Task, error) {
	return s.st.PauseTask(id)
}

func (s *Service) Resume(id string) (novel.Task, error) {
	// 先让仍在收尾的旧执行代次失效，再重新入队，避免同一任务被两个协程同时推进。
	if current, err := s.st.GetTask(id); err == nil && current.Status == novel.TaskPaused {
		s.bumpEpoch(id)
	}
	updated, err := s.st.ResumeTask(id)
	if err == nil {
		s.publish(updated, novel.TaskEventCreated)
		s.Notify()
	}
	return updated, err
}

func (s *Service) bumpEpoch(id string) uint64 {
	s.epochMu.Lock()
	defer s.epochMu.Unlock()
	s.epochs[id]++
	return s.epochs[id]
}

func (s *Service) epoch(id string) uint64 {
	s.epochMu.Lock()
	defer s.epochMu.Unlock()
	return s.epochs[id]
}

func (s *Service) Cancel(id string) (novel.Task, error) {
	updated, err := s.st.CancelTask(id)
	if err == nil {
		s.publish(updated, novel.TaskEventCancelled)
	}
	return updated, err
}

// validateBatchStart 批量续写只允许从最后一章的内部偏移开始（生成内容作为后续新章草稿）。
func (s *Service) validateBatchStart(count int, chapterID string, offset int) error {
	chapterID = strings.TrimSpace(chapterID)
	if chapterID == "" {
		if offset != 0 {
			return fmt.Errorf("%w: startOffset requires startChapterId", ErrInvalidTaskRequest)
		}
		return nil
	}
	project, err := s.st.LoadProject()
	if err != nil {
		return err
	}
	if len(project.Chapters) == 0 {
		return fmt.Errorf("%w: book has no chapters", ErrInvalidTaskRequest)
	}
	last := project.Chapters[len(project.Chapters)-1]
	if last.ID != chapterID {
		return fmt.Errorf("%w: batch continuation start must be the last chapter", ErrInvalidTaskRequest)
	}
	chapter, err := s.st.LoadChapter(chapterID)
	if err != nil {
		return fmt.Errorf("%w: start chapter not found", ErrInvalidTaskRequest)
	}
	length := len([]rune(chapter.Content))
	if offset <= 0 || offset > length {
		return fmt.Errorf("%w: startOffset must be within (0,%d]", ErrInvalidTaskRequest, length)
	}
	if count <= 0 {
		return fmt.Errorf("%w: chapterCount must be positive", ErrInvalidTaskRequest)
	}
	return nil
}

func (s *Service) loop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.wake:
		}
		for {
			task, ok, err := s.st.ClaimNextTask()
			if err != nil {
				s.log.Error("claim task", "err", err)
				break
			}
			if !ok {
				break
			}
			s.runTask(ctx, task)
			if ctx.Err() != nil {
				return
			}
		}
	}
}

func (s *Service) runTask(ctx context.Context, task novel.Task) {
	seq := s.bumpEpoch(task.ID)
	defer func() {
		if r := recover(); r != nil {
			s.fail(task, nil, fmt.Errorf("task panic: %v", r))
		}
	}()
	switch task.Kind {
	case novel.TaskKindContinueBatch:
		s.runContinueBatch(ctx, task, seq)
	case novel.TaskKindAgentRun:
		s.runAgent(ctx, task)
	default:
		s.fail(task, nil, fmt.Errorf("unsupported task kind %q", task.Kind))
	}
}

func (s *Service) runContinueBatch(ctx context.Context, task novel.Task, seq uint64) {
	var params novel.TaskContinueParams
	if err := json.Unmarshal(task.Params, &params); err != nil {
		s.fail(task, nil, fmt.Errorf("decode task params: %w", err))
		return
	}
	project, err := s.st.LoadProject()
	if err != nil {
		s.fail(task, nil, err)
		return
	}
	baseCount := len(project.Chapters)
	result, completed, extra := s.resumeState(task, project, params.ChapterCount)
	totalTokens := result.Tokens

	for i := completed; i < params.ChapterCount; i++ {
		if halted, status := s.halted(task.ID, seq); halted {
			s.log.Info("task halted", "task", task.ID, "status", status)
			return
		}
		if ctx.Err() != nil {
			return
		}
		req := continuation.Request{
			Direction:   params.Direction,
			TargetChars: params.TargetChars,
			Candidates:  1,
			Temperature: params.Temperature,
		}
		// 起点偏移只对首章生效；恢复执行时已有草稿作为上下文，不再重复套用偏移。
		if i == 0 && completed == 0 && strings.TrimSpace(params.StartChapterID) != "" {
			req.StartMode = novel.ContinuationStartChapterOffset
			req.ChapterID = params.StartChapterID
			req.StartOffset = params.StartOffset
		}
		generated, err := s.cont.GenerateWithExtra(ctx, req, extra)
		if err != nil && generated.Group.ID == "" {
			if ctx.Err() != nil {
				return
			}
			s.fail(task, result, err)
			return
		}
		if generated.Group.ID != "" {
			result.GroupIDs = append(result.GroupIDs, generated.Group.ID)
		}
		if err != nil {
			s.fail(task, result, err)
			return
		}
		var content string
		tokens := 0
		for _, candidate := range generated.Candidates {
			tokens += candidate.TotalTokens
			if candidate.Status == "done" && strings.TrimSpace(candidate.Content) != "" {
				content = candidate.Content
				break
			}
		}
		totalTokens += tokens
		if content == "" {
			s.fail(task, result, errors.New("模型未返回可用续写内容"))
			return
		}
		if halted, _ := s.halted(task.ID, seq); halted {
			// 暂停/取消/任务已被新一轮执行接管时，本次调用结果不再落草稿。
			return
		}
		chapterNo := baseCount + len(extra) + 1
		draft, err := s.st.CreateDraft(store.DraftCreateParams{
			Title:   novel.DefaultChapterTitle(chapterNo),
			Content: content,
		})
		if err != nil {
			s.fail(task, result, fmt.Errorf("create draft: %w", err))
			return
		}
		result.DraftIDs = append(result.DraftIDs, draft.ID)
		result.ChapterIDs = append(result.ChapterIDs, draft.TargetChapterID)
		result.Tokens = totalTokens
		extra = append(extra, novel.Chapter{
			ID:        draft.TargetChapterID,
			Index:     chapterNo,
			Title:     draft.Title,
			Content:   content,
			CharCount: novel.CountChars(content),
			Revision:  1,
			UpdatedAt: time.Now().UTC(),
		})
		payload, _ := json.Marshal(result)
		if err := s.st.UpdateTaskProgress(task.ID, i+1, payload); err != nil {
			if errors.Is(err, store.ErrTaskStateConflict) {
				return
			}
			s.log.Warn("update task progress", "task", task.ID, "err", err)
		}
	}

	payload, _ := json.Marshal(result)
	updated, err := s.st.FinishTask(task.ID, novel.TaskCompleted, payload, "")
	if err != nil {
		if errors.Is(err, store.ErrTaskStateConflict) {
			return
		}
		s.log.Error("finish task", "task", task.ID, "err", err)
		return
	}
	s.publish(updated, novel.TaskEventCompleted)
}

// resumeState 根据上次持久化的进度重建批量续写执行位置。
// 已提交到正文的章节直接作为上下文；尚未提交的草稿按顺序加入 extra，
// 从而在暂停/重启后恢复任务时不会重复生成已完成的章节。
func (s *Service) resumeState(task novel.Task, project novel.Project, total int) (novel.TaskContinueResult, int, []novel.Chapter) {
	result := novel.TaskContinueResult{DraftIDs: []string{}, GroupIDs: []string{}, ChapterIDs: []string{}}
	extra := make([]novel.Chapter, 0, max(total, 0))
	if task.Progress <= 0 || len(task.Result) == 0 {
		return result, 0, extra
	}
	var prior novel.TaskContinueResult
	if err := json.Unmarshal(task.Result, &prior); err != nil {
		return result, 0, extra
	}
	committed := make(map[string]struct{}, len(project.Chapters))
	for _, meta := range project.Chapters {
		committed[meta.ID] = struct{}{}
	}
	completed := min(task.Progress, len(prior.DraftIDs), len(prior.ChapterIDs))
	if total > 0 {
		completed = min(completed, total)
	}
	result.GroupIDs = append(result.GroupIDs, prior.GroupIDs...)
	result.DraftIDs = append(result.DraftIDs, prior.DraftIDs[:completed]...)
	result.ChapterIDs = append(result.ChapterIDs, prior.ChapterIDs[:completed]...)
	result.Tokens = prior.Tokens

	rebuilt := 0
	for i := 0; i < completed; i++ {
		chapterID := prior.ChapterIDs[i]
		if _, ok := committed[chapterID]; ok {
			rebuilt++
			continue
		}
		draft, err := s.st.LoadDraft(prior.DraftIDs[i])
		if err != nil || strings.TrimSpace(draft.Content) == "" {
			break
		}
		extra = append(extra, novel.Chapter{
			ID:        chapterID,
			Index:     len(project.Chapters) + len(extra) + 1,
			Title:     draft.Title,
			Content:   draft.Content,
			CharCount: novel.CountChars(draft.Content),
			Revision:  1,
			UpdatedAt: time.Now().UTC(),
		})
		rebuilt++
	}
	if rebuilt < completed {
		// 草稿丢失的章节需要重新生成，截断结果保持进度与草稿列表一致。
		result.DraftIDs = result.DraftIDs[:rebuilt]
		result.ChapterIDs = result.ChapterIDs[:rebuilt]
		completed = rebuilt
	}
	if len(result.GroupIDs) > completed {
		result.GroupIDs = result.GroupIDs[:completed]
	}
	return result, completed, extra
}

func (s *Service) runAgent(ctx context.Context, task novel.Task) {
	var params novel.TaskAgentParams
	if err := json.Unmarshal(task.Params, &params); err != nil {
		s.fail(task, nil, fmt.Errorf("decode task params: %w", err))
		return
	}
	if s.bus == nil {
		s.fail(task, nil, errors.New("Agent 运行时未启用"))
		return
	}
	if !s.bus.HasAgent(params.Agent) {
		s.fail(task, nil, fmt.Errorf("%w: %s", ErrAgentUnavailable, params.Agent))
		return
	}
	ev := agentruntime.Event{
		Type:       novel.TaskEventCreated,
		TaskID:     task.ID,
		TaskStatus: string(task.Status),
		Title:      task.Description,
		At:         time.Now().UTC(),
	}
	if chapter, ok := s.chapterForTask(params.ChapterID); ok {
		ev.Type = string(store.ChapterSaved)
		ev.ChapterID = chapter.ID
		ev.Title = chapter.Title
		ev.Index = chapter.Index
		ev.CharCount = chapter.CharCount
	}
	report, reportID, err := s.bus.RunAgentOnce(ctx, params.Agent, ev)
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		s.fail(task, map[string]any{"agent": params.Agent, "reportId": reportID}, err)
		return
	}
	result := map[string]any{
		"agent":    params.Agent,
		"reportId": reportID,
		"summary":  report.Summary,
		"tokens":   report.Tokens,
	}
	payload, _ := json.Marshal(result)
	updated, err := s.st.FinishTask(task.ID, novel.TaskCompleted, payload, "")
	if err != nil {
		if errors.Is(err, store.ErrTaskStateConflict) {
			return
		}
		s.log.Error("finish agent task", "task", task.ID, "err", err)
		return
	}
	s.publish(updated, novel.TaskEventCompleted)
}

func (s *Service) chapterForTask(chapterID string) (novel.Chapter, bool) {
	if strings.TrimSpace(chapterID) != "" {
		chapter, err := s.st.LoadChapter(chapterID)
		return chapter, err == nil
	}
	project, err := s.st.LoadProject()
	if err != nil || len(project.Chapters) == 0 {
		return novel.Chapter{}, false
	}
	chapter, err := s.st.LoadChapter(project.Chapters[len(project.Chapters)-1].ID)
	return chapter, err == nil
}

func (s *Service) halted(id string, seq uint64) (bool, novel.TaskStatus) {
	current, err := s.st.GetTask(id)
	if err != nil {
		return true, novel.TaskFailed
	}
	switch current.Status {
	case novel.TaskPaused, novel.TaskCancelled, novel.TaskFailed, novel.TaskCompleted:
		return true, current.Status
	}
	if s.epoch(id) != seq {
		return true, current.Status
	}
	return false, current.Status
}

func (s *Service) fail(task novel.Task, result any, cause error) {
	if cause == nil {
		cause = errors.New("unknown task error")
	}
	current, err := s.st.GetTask(task.ID)
	if err == nil {
		switch current.Status {
		case novel.TaskPaused, novel.TaskCancelled, novel.TaskCompleted, novel.TaskFailed:
			return
		}
	}
	payload, _ := json.Marshal(result)
	updated, err := s.st.FinishTask(task.ID, novel.TaskFailed, payload, cause.Error())
	if err != nil {
		if !errors.Is(err, store.ErrTaskStateConflict) {
			s.log.Error("mark task failed", "task", task.ID, "err", err)
		}
		return
	}
	s.log.Warn("task failed", "task", task.ID, "kind", task.Kind, "err", cause)
	s.publish(updated, novel.TaskEventFailed)
}

func (s *Service) publish(task novel.Task, eventType string) {
	if s.bus == nil {
		return
	}
	s.bus.Publish(agentruntime.Event{
		Type:       eventType,
		TaskID:     task.ID,
		TaskStatus: string(task.Status),
		Title:      task.Description,
		At:         time.Now().UTC(),
	})
}

var chapterCountPattern = regexp.MustCompile(`([0-9]{1,2}|[一二三四五六七八九十两]{1,3})\s*章`)

// parseDescription 把自然语言描述映射为任务类型、数量、Agent 与续写方向。
func parseDescription(description string) (kind string, count int, agent string, direction string) {
	description = strings.TrimSpace(description)
	if description == "" {
		return novel.TaskKindAgentRun, 0, "continuity", ""
	}
	if strings.Contains(description, "续写") || strings.Contains(description, "批量写") ||
		(chapterCountPattern.MatchString(description) && containsAny(description, "写", "生成", "来")) {
		count = parseChapterCount(description)
		return novel.TaskKindContinueBatch, count, "", continueDirection(description)
	}
	switch {
	case containsAny(description, "校对", "润色", "错别字", "标点", "语句"):
		agent = "proofread"
	case containsAny(description, "剧情", "推演", "走向", "桥段"):
		agent = "plot"
	case containsAny(description, "文风", "风格", "语气", "句式", "视角"):
		agent = "style"
	case containsAny(description, "关系", "情绪", "感情"):
		agent = "relation"
	case containsAny(description, "任务", "队列", "进度", "巡检"):
		agent = "task"
	case containsAny(description, "伏笔"):
		agent = "fuse"
	case containsAny(description, "记忆", "知识库", "体检"):
		agent = "memory"
	default:
		agent = "continuity"
	}
	return novel.TaskKindAgentRun, 0, agent, ""
}

func parseChapterCount(description string) int {
	match := chapterCountPattern.FindStringSubmatch(description)
	if len(match) < 2 {
		return 1
	}
	if number, err := strconv.Atoi(match[1]); err == nil {
		if number <= 0 {
			return 1
		}
		if number > 20 {
			return 20
		}
		return number
	}
	number := parseChineseNumber(match[1])
	if number <= 0 {
		return 1
	}
	if number > 20 {
		return 20
	}
	return number
}

func parseChineseNumber(text string) int {
	digits := map[rune]int{'一': 1, '二': 2, '两': 2, '三': 3, '四': 4, '五': 5, '六': 6, '七': 7, '八': 8, '九': 9}
	total, current := 0, 0
	for _, r := range text {
		if r == '十' {
			if current == 0 {
				total += 10
			} else {
				total += current * 10
			}
			current = 0
			continue
		}
		if d, ok := digits[r]; ok {
			current = d
		}
	}
	total += current
	return total
}

func continueDirection(description string) string {
	direction := strings.Replace(description, "续写", "", 1)
	if loc := chapterCountPattern.FindStringIndex(direction); loc != nil {
		direction = direction[:loc[0]] + direction[loc[1]:]
	}
	direction = strings.Replace(direction, "批量写", "", 1)
	direction = strings.TrimSpace(direction)
	for _, prefix := range []string{"请帮我", "帮忙", "请", "帮我", "批量", "写"} {
		if strings.HasPrefix(direction, prefix) {
			direction = strings.TrimSpace(strings.TrimPrefix(direction, prefix))
			break
		}
	}
	return strings.Trim(direction, " \t\r\n，,。；;：:")
}

func containsAny(text string, keywords ...string) bool {
	for _, keyword := range keywords {
		if strings.Contains(text, keyword) {
			return true
		}
	}
	return false
}

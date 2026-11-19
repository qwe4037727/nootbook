package agentruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"novelagent/internal/llm"
	"novelagent/internal/store"
)

const (
	StatusDone   = "done"
	StatusFailed = "failed"

	PermissionObserve    = "observe"
	PermissionPropose    = "propose"
	PermissionAutonomous = "autonomous"

	agentRunTimeout = 60 * time.Second

	breakerThreshold = 3
	breakerOpenFor   = 2 * time.Minute
)

var (
	ErrAgentNotRegistered = errors.New("agentruntime: agent not registered")
	ErrAgentDisabled      = errors.New("agentruntime: agent disabled")
	ErrAgentCircuitOpen   = errors.New("agentruntime: agent circuit open after repeated failures")
)

// Event 是 Agent 运行时消费的最小事件。
type Event struct {
	ID           string `json:"id"`
	Type         string `json:"type"`
	ProjectID    string `json:"projectId"`
	ChapterID    string `json:"chapterId,omitempty"`
	Title        string `json:"title,omitempty"`
	Index        int    `json:"index,omitempty"`
	CharCount    int    `json:"charCount,omitempty"`
	ChapterCount int    `json:"chapterCount,omitempty"`
	// TaskID/TaskStatus 供 A9 任务执行 Agent 消费任务中心事件。
	TaskID     string    `json:"taskId,omitempty"`
	TaskStatus string    `json:"taskStatus,omitempty"`
	At         time.Time `json:"at"`
}

// Evidence 指向冲突/伏笔的正文证据位置。
type Evidence struct {
	ChapterID    string `json:"chapterId"`
	ChapterIndex int    `json:"chapterIndex,omitempty"`
	Quote        string `json:"quote,omitempty"`
}

// Finding 是审计报告中的单条发现。
type Finding struct {
	Type         string     `json:"type"`
	Severity     string     `json:"severity"`
	EntryID      string     `json:"entryId,omitempty"`
	ChapterID    string     `json:"chapterId,omitempty"`
	ChapterIndex int        `json:"chapterIndex,omitempty"`
	Quote        string     `json:"quote,omitempty"`
	ConflictWith string     `json:"conflictWith,omitempty"`
	Detail       string     `json:"detail,omitempty"`
	Confidence   float64    `json:"confidence,omitempty"`
	Options      []string   `json:"options,omitempty"`
	Evidence     []Evidence `json:"evidence,omitempty"`
}

// Proposal 是送入审阅队列的提案（不自动落地）。
type Proposal struct {
	ID      string          `json:"id,omitempty"`
	Kind    string          `json:"kind"`
	Target  string          `json:"target,omitempty"`
	Summary string          `json:"summary,omitempty"`
	Risk    string          `json:"risk,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// Report 是 Agent 一次运行的观察结果。
type Report struct {
	Summary          string     `json:"summary"`
	Details          []string   `json:"details,omitempty"`
	Severity         string     `json:"severity,omitempty"`
	Findings         []Finding  `json:"findings,omitempty"`
	Proposals        []Proposal `json:"proposals,omitempty"`
	ReportID         string     `json:"reportId,omitempty"`
	Tokens           int        `json:"tokens,omitempty"`
	PromptTokens     int        `json:"promptTokens,omitempty"`
	CompletionTokens int        `json:"completionTokens,omitempty"`
}

// Input 是 Agent 每次运行的依赖注入；权限等级决定其可产生的副作用。
type Input struct {
	ProjectID  string
	Permission string
	Store      *store.Store
	Client     *llm.Client
	Model      string
	Configured bool
}

// Agent 是插件化 Agent 契约：Key 唯一标识，Triggers 声明订阅的事件类型。
type Agent interface {
	Key() string
	Name() string
	Triggers() []string
	Run(ctx context.Context, ev Event, in Input) (Report, error)
}

// AgentDefaults 是 milestone.yaml 提供的全局默认（项目配置可覆盖）。
type AgentDefaults struct {
	Enabled bool
	Level   string
}

// RunRecord 是可在 UI 与 JSONL 中查询的运行记录。
type RunRecord struct {
	ID               string    `json:"id"`
	ProjectID        string    `json:"projectId"`
	EventID          string    `json:"eventId"`
	EventType        string    `json:"eventType"`
	Agent            string    `json:"agent"`
	AgentName        string    `json:"agentName,omitempty"`
	Permission       string    `json:"permission"`
	Status           string    `json:"status"`
	StartedAt        time.Time `json:"startedAt"`
	FinishedAt       time.Time `json:"finishedAt"`
	DurationMs       int64     `json:"durationMs"`
	Tokens           int       `json:"tokens"`
	PromptTokens     int       `json:"promptTokens,omitempty"`
	CompletionTokens int       `json:"completionTokens,omitempty"`
	InputChars       int       `json:"inputChars"`
	OutputChars      int       `json:"outputChars"`
	Summary          string    `json:"summary,omitempty"`
	Details          []string  `json:"details,omitempty"`
	Error            string    `json:"error,omitempty"`
	ReportID         string    `json:"reportId,omitempty"`
}

// Runtime 为单个项目的事件总线与 Agent 调度器。
type Runtime struct {
	projectID string
	store     *store.Store
	log       *slog.Logger
	recorder  *Recorder
	agents    []Agent

	defaultsMu sync.RWMutex
	defaults   map[string]AgentDefaults

	mu      sync.Mutex
	started bool
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	events  chan Event

	seenMu sync.Mutex
	seen   map[string]struct{}

	// breaker 记录连续失败次数：同一 Agent 连续失败达到阈值后熔断 breakerOpenFor，
	// 期间跳过该 Agent，但不影响其他 Agent 与续写主链路。
	breakerMu sync.Mutex
	breaker   map[string]*agentBreaker
}

type agentBreaker struct {
	failures  int
	openUntil time.Time
}

func New(projectID string, st *store.Store, dir string, logger *slog.Logger) (*Runtime, error) {
	if logger == nil {
		logger = slog.Default()
	}
	recorder, err := NewRecorder(filepath.Join(dir, "agent"))
	if err != nil {
		return nil, err
	}
	rt := &Runtime{
		projectID: projectID,
		store:     st,
		log:       logger,
		recorder:  recorder,
		events:    make(chan Event, 128),
		seen:      make(map[string]struct{}),
		defaults:  make(map[string]AgentDefaults),
		breaker:   make(map[string]*agentBreaker),
	}
	rt.agents = []Agent{&EchoAgent{}}
	return rt, nil
}

// Register 注册插件化 Agent；重复 Key 忽略，便于里程碑与配置幂等装配。
func (rt *Runtime) Register(list ...Agent) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	for _, agent := range list {
		if agent == nil || agent.Key() == "" {
			continue
		}
		exists := false
		for _, existing := range rt.agents {
			if existing.Key() == agent.Key() {
				exists = true
				break
			}
		}
		if !exists {
			rt.agents = append(rt.agents, agent)
		}
	}
}

// SetDefaults 写入 milestone.yaml 的全局默认开关/权限档位。
func (rt *Runtime) SetDefaults(defaults map[string]AgentDefaults) {
	rt.defaultsMu.Lock()
	defer rt.defaultsMu.Unlock()
	rt.defaults = make(map[string]AgentDefaults, len(defaults))
	for key, value := range defaults {
		rt.defaults[key] = value
	}
}

// Agents 返回已注册 Agent 的只读快照（供 Agent 中心展示）。
func (rt *Runtime) Agents() []Agent {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	out := make([]Agent, len(rt.agents))
	copy(out, rt.agents)
	return out
}

// HasAgent 判断插件是否已注册（任务中心创建 agent_run 前校验）。
func (rt *Runtime) HasAgent(key string) bool {
	key = strings.TrimSpace(key)
	for _, agent := range rt.Agents() {
		if agent.Key() == key {
			return true
		}
	}
	return false
}

func (rt *Runtime) Start(ctx context.Context) {
	rt.mu.Lock()
	if rt.started {
		rt.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(ctx)
	rt.cancel = cancel
	rt.started = true
	rt.wg.Add(1)
	rt.mu.Unlock()

	// 章节写操作发出的事件只入队，由独立 goroutine 执行；写接口不被 Agent 阻塞。
	rt.store.SetChapterChangeHook(func(change store.ChapterChange) {
		rt.PublishFromChange(change)
	})

	go func() {
		defer rt.wg.Done()
		rt.loop(ctx)
	}()
}

func (rt *Runtime) Stop() {
	rt.mu.Lock()
	cancel := rt.cancel
	rt.cancel = nil
	rt.started = false
	rt.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	rt.wg.Wait()
	rt.store.SetChapterChangeHook(nil)
}

// Publish 将事件放入总线；同一 Event.ID 重复投递只执行一次。
// 队列满时丢弃并记日志，绝不阻塞调用方。
func (rt *Runtime) Publish(ev Event) bool {
	if ev.ID == "" {
		ev.ID = newEventID()
	}
	if ev.At.IsZero() {
		ev.At = time.Now().UTC()
	}
	if ev.ProjectID == "" {
		ev.ProjectID = rt.projectID
	}
	rt.seenMu.Lock()
	if _, ok := rt.seen[ev.ID]; ok {
		rt.seenMu.Unlock()
		return false
	}
	rt.seen[ev.ID] = struct{}{}
	if len(rt.seen) > 4096 {
		// 简单上限：清空后从当前事件重新开始，避免长期运行无限增长。
		rt.seen = map[string]struct{}{ev.ID: {}}
	}
	rt.seenMu.Unlock()

	select {
	case rt.events <- ev:
		return true
	default:
		// 未真正入队的事件不能占用去重标记，否则重试会被误判为重复。
		rt.seenMu.Lock()
		delete(rt.seen, ev.ID)
		rt.seenMu.Unlock()
		rt.log.Warn("agent event dropped: queue full", "event", ev.ID, "type", ev.Type)
		return false
	}
}

func (rt *Runtime) PublishFromChange(change store.ChapterChange) {
	rt.Publish(Event{
		ID:           newEventID(),
		Type:         string(change.Kind),
		ProjectID:    rt.projectID,
		ChapterID:    change.ChapterID,
		Title:        change.Title,
		Index:        change.Index,
		CharCount:    change.CharCount,
		ChapterCount: change.ChapterCount,
		At:           time.Now().UTC(),
	})
}

// RecentRuns 优先返回 agent_run 表的审计日志；数据库为空时回退 JSONL（兼容 M0 历史）。
func (rt *Runtime) RecentRuns(limit int) []RunRecord {
	if rows, err := rt.store.QueryAgentRuns(store.AgentRunFilter{Limit: limit}); err == nil && len(rows) > 0 {
		out := make([]RunRecord, 0, len(rows))
		for _, row := range rows {
			out = append(out, RunRecord{
				ID:               row.ID,
				ProjectID:        row.ProjectID,
				EventID:          row.EventID,
				EventType:        row.EventType,
				Agent:            row.AgentKey,
				AgentName:        row.AgentName,
				Permission:       row.Permission,
				Status:           row.Status,
				StartedAt:        row.StartedAt,
				FinishedAt:       row.FinishedAt,
				DurationMs:       row.DurationMs,
				Tokens:           row.TotalTokens,
				PromptTokens:     row.PromptTokens,
				CompletionTokens: row.CompletionTokens,
				InputChars:       row.InputChars,
				OutputChars:      row.OutputChars,
				Summary:          row.Summary,
				Error:            row.Error,
				ReportID:         row.ReportID,
			})
		}
		return out
	}
	return rt.recorder.Recent(limit)
}

func (rt *Runtime) loop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case ev := <-rt.events:
			rt.dispatch(ctx, ev)
		}
	}
}

func (rt *Runtime) dispatch(ctx context.Context, ev Event) {
	for _, agent := range rt.Agents() {
		if ctx.Err() != nil {
			return
		}
		if !triggersEvent(agent, ev) {
			continue
		}
		if !rt.allowAgent(agent.Key()) {
			rt.log.Warn("agent skipped: circuit open", "agent", agent.Key(), "event", ev.ID)
			continue
		}
		_, _, err := rt.runAgent(ctx, agent, ev)
		rt.recordAgentOutcome(agent.Key(), err)
	}
}

// RunAgentOnce 供任务中心显式执行单个 Agent；与事件分发共用超时、panic 恢复与熔断。
func (rt *Runtime) RunAgentOnce(ctx context.Context, key string, ev Event) (Report, string, error) {
	key = strings.TrimSpace(key)
	var target Agent
	for _, agent := range rt.Agents() {
		if agent.Key() == key {
			target = agent
			break
		}
	}
	if target == nil {
		return Report{}, "", ErrAgentNotRegistered
	}
	enabled, _ := rt.agentSetting(key)
	if !enabled {
		return Report{}, "", ErrAgentDisabled
	}
	if !rt.allowAgent(key) {
		return Report{}, "", ErrAgentCircuitOpen
	}
	if ev.ID == "" {
		ev.ID = newEventID()
	}
	if ev.ProjectID == "" {
		ev.ProjectID = rt.projectID
	}
	if ev.At.IsZero() {
		ev.At = time.Now().UTC()
	}
	rec, report, err := rt.runAgent(ctx, target, ev)
	rt.recordAgentOutcome(key, err)
	if err != nil {
		return report, rec.ReportID, err
	}
	return report, rec.ReportID, nil
}

// allowAgent 判断熔断器是否允许本次执行；半开状态放行一次探测。
func (rt *Runtime) allowAgent(key string) bool {
	rt.breakerMu.Lock()
	defer rt.breakerMu.Unlock()
	state, ok := rt.breaker[key]
	if !ok {
		return true
	}
	if state.openUntil.IsZero() {
		return true
	}
	if time.Now().Before(state.openUntil) {
		return false
	}
	// 熔断窗口结束：放行一次半开探测，失败会立即重新打开。
	state.openUntil = time.Time{}
	return true
}

func (rt *Runtime) recordAgentOutcome(key string, err error) {
	rt.breakerMu.Lock()
	defer rt.breakerMu.Unlock()
	if err == nil {
		delete(rt.breaker, key)
		return
	}
	state := rt.breaker[key]
	if state == nil {
		state = &agentBreaker{}
		rt.breaker[key] = state
	}
	state.failures++
	if state.failures >= breakerThreshold {
		state.openUntil = time.Now().Add(breakerOpenFor)
		rt.log.Warn("agent circuit opened", "agent", key, "failures", state.failures, "for", breakerOpenFor)
	}
}

func triggersEvent(agent Agent, ev Event) bool {
	for _, trigger := range agent.Triggers() {
		if trigger == ev.Type {
			return true
		}
	}
	return false
}

// agentSetting 解析该 Agent 的启用状态与权限档位：项目配置 > milestone 默认 > observe。
func (rt *Runtime) agentSetting(key string) (bool, string) {
	enabled, level := true, PermissionObserve
	rt.defaultsMu.RLock()
	if def, ok := rt.defaults[key]; ok {
		enabled, level = def.Enabled, def.Level
	}
	rt.defaultsMu.RUnlock()
	if cfgs, err := rt.store.LoadAgentConfigs(); err == nil {
		if cfg, ok := cfgs[key]; ok {
			enabled, level = cfg.Enabled, cfg.Level
		}
	}
	switch level {
	case PermissionObserve, PermissionPropose, PermissionAutonomous:
	default:
		level = PermissionObserve
	}
	return enabled, level
}

func (rt *Runtime) llmClient() (*llm.Client, string, bool) {
	cfg, err := rt.store.LoadConfig()
	if err != nil {
		return nil, "", false
	}
	key, _ := cfg.EffectiveAPIKey()
	if key == "" || cfg.BaseURL == "" || cfg.Model == "" {
		return nil, cfg.Model, false
	}
	return llm.NewClient(cfg.BaseURL, key, cfg.Model), cfg.Model, true
}

func (rt *Runtime) runAgent(ctx context.Context, agent Agent, ev Event) (RunRecord, Report, error) {
	enabled, level := rt.agentSetting(agent.Key())
	if !enabled {
		return RunRecord{}, Report{}, nil
	}

	started := time.Now().UTC()
	runCtx, cancel := context.WithTimeout(ctx, agentRunTimeout)
	defer cancel()

	client, model, configured := rt.llmClient()
	in := Input{
		ProjectID:  rt.projectID,
		Permission: level,
		Store:      rt.store,
		Client:     client,
		Model:      model,
		Configured: configured,
	}
	rec := RunRecord{
		ID:         "run_" + newEventID(),
		ProjectID:  rt.projectID,
		EventID:    ev.ID,
		EventType:  ev.Type,
		Agent:      agent.Key(),
		AgentName:  agent.Name(),
		Permission: level,
		Status:     StatusDone,
		StartedAt:  started,
	}
	var report Report
	var err error
	func() {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("agent panic: %v", r)
			}
		}()
		report, err = agent.Run(runCtx, ev, in)
	}()
	rec.FinishedAt = time.Now().UTC()
	rec.DurationMs = rec.FinishedAt.Sub(rec.StartedAt).Milliseconds()
	rec.Summary = report.Summary
	rec.Details = report.Details
	rec.Tokens = report.Tokens
	rec.PromptTokens = report.PromptTokens
	rec.CompletionTokens = report.CompletionTokens
	rec.OutputChars = len([]rune(report.Summary))
	rec.InputChars = ev.CharCount
	if err != nil {
		rec.Status = StatusFailed
		rec.Error = err.Error()
		rt.log.Error("agent run failed", "agent", agent.Key(), "event", ev.ID, "err", err)
	} else if len(report.Findings) > 0 || len(report.Proposals) > 0 || report.Severity != "" {
		// 报告落库；Echo 等无审计产出的 Agent 不产生空报告。
		report.ReportID = "ar_" + newEventID()
		findingsJSON, _ := json.Marshal(report.Findings)
		record := store.AgentReportRecord{
			ID:           report.ReportID,
			AgentKey:     agent.Key(),
			AgentName:    agent.Name(),
			EventID:      ev.ID,
			ChapterID:    ev.ChapterID,
			ChapterIndex: ev.Index,
			Severity:     report.Severity,
			Summary:      report.Summary,
			Findings:     findingsJSON,
			CreatedAt:    rec.FinishedAt,
		}
		if saveErr := rt.store.SaveAgentReport(record); saveErr != nil {
			rt.log.Error("save agent report", "agent", agent.Key(), "err", saveErr)
		} else {
			rec.ReportID = report.ReportID
		}
	}

	if dbErr := rt.store.RecordAgentRun(store.AgentRunRecord{
		ID:               rec.ID,
		ProjectID:        rec.ProjectID,
		AgentKey:         rec.Agent,
		AgentName:        rec.AgentName,
		EventID:          rec.EventID,
		EventType:        rec.EventType,
		Permission:       rec.Permission,
		Status:           rec.Status,
		StartedAt:        rec.StartedAt,
		FinishedAt:       rec.FinishedAt,
		DurationMs:       rec.DurationMs,
		PromptTokens:     rec.PromptTokens,
		CompletionTokens: rec.CompletionTokens,
		TotalTokens:      rec.Tokens,
		InputChars:       rec.InputChars,
		OutputChars:      rec.OutputChars,
		Summary:          rec.Summary,
		Error:            rec.Error,
		ReportID:         rec.ReportID,
	}); dbErr != nil {
		rt.log.Error("record agent run in db", "agent", agent.Key(), "err", dbErr)
	}
	if recErr := rt.recorder.Append(rec); recErr != nil {
		rt.log.Error("record agent run", "agent", agent.Key(), "err", recErr)
	}
	return rec, report, err
}

var eventSeq atomic.Uint64

func newEventID() string {
	return fmt.Sprintf("ev_%d_%d", time.Now().UnixNano(), eventSeq.Add(1))
}

// Recorder 追加 JSONL 并维护内存中的最近记录（M0 兼容审计通道）。
type Recorder struct {
	mu     sync.Mutex
	path   string
	recent []RunRecord
	max    int
}

func NewRecorder(dir string) (*Recorder, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("agent: create run directory: %w", err)
	}
	r := &Recorder{path: filepath.Join(dir, "runs.jsonl"), max: 200}
	if err := r.load(); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *Recorder) load() error {
	data, err := os.ReadFile(r.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("agent: read runs: %w", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var rec RunRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue
		}
		r.recent = append(r.recent, rec)
		if len(r.recent) > r.max {
			r.recent = r.recent[len(r.recent)-r.max:]
		}
	}
	return nil
}

func (r *Recorder) Append(rec RunRecord) error {
	data, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	f, err := os.OpenFile(r.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("agent: open runs: %w", err)
	}
	if _, err := f.Write(append(data, '\n')); err != nil {
		f.Close()
		return fmt.Errorf("agent: write run: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("agent: close runs: %w", err)
	}
	r.recent = append(r.recent, rec)
	if len(r.recent) > r.max {
		r.recent = r.recent[len(r.recent)-r.max:]
	}
	return nil
}

func (r *Recorder) Recent(limit int) []RunRecord {
	r.mu.Lock()
	defer r.mu.Unlock()
	if limit <= 0 || limit > len(r.recent) {
		limit = len(r.recent)
	}
	out := make([]RunRecord, limit)
	for i := 0; i < limit; i++ {
		out[i] = r.recent[len(r.recent)-1-i]
	}
	return out
}

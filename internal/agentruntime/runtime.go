package agentruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"novelagent/internal/store"
)

const (
	StatusDone   = "done"
	StatusFailed = "failed"

	PermissionObserve = "observe"
)

// Event 是 Agent 运行时消费的最小章节事件。
type Event struct {
	ID           string    `json:"id"`
	Type         string    `json:"type"`
	ProjectID    string    `json:"projectId"`
	ChapterID    string    `json:"chapterId,omitempty"`
	Title        string    `json:"title,omitempty"`
	Index        int       `json:"index,omitempty"`
	CharCount    int       `json:"charCount,omitempty"`
	ChapterCount int       `json:"chapterCount,omitempty"`
	At           time.Time `json:"at"`
}

// Report 是 Agent 一次运行的观察结果；M0 只读，不落正文/知识库。
type Report struct {
	Summary string   `json:"summary"`
	Details []string `json:"details,omitempty"`
	Tokens  int      `json:"tokens,omitempty"`
}

// Agent 是 M0 的最小契约：只做观察。后续 M1 再引入 Gather/Apply 与权限门。
type Agent interface {
	Name() string
	CanRun(Event) bool
	Run(ctx context.Context, ev Event) (Report, error)
}

// RunRecord 是可在 UI 与 JSONL 中查询的运行记录。
type RunRecord struct {
	ID          string    `json:"id"`
	ProjectID   string    `json:"projectId"`
	EventID     string    `json:"eventId"`
	EventType   string    `json:"eventType"`
	Agent       string    `json:"agent"`
	Permission  string    `json:"permission"`
	Status      string    `json:"status"`
	StartedAt   time.Time `json:"startedAt"`
	FinishedAt  time.Time `json:"finishedAt"`
	DurationMs  int64     `json:"durationMs"`
	Tokens      int       `json:"tokens"`
	InputChars  int       `json:"inputChars"`
	OutputChars int       `json:"outputChars"`
	Summary     string    `json:"summary,omitempty"`
	Details     []string  `json:"details,omitempty"`
	Error       string    `json:"error,omitempty"`
}

// Runtime 为单个项目的事件总线与 Agent 调度器。
type Runtime struct {
	projectID string
	store     *store.Store
	log       *slog.Logger
	recorder  *Recorder
	agents    []Agent

	mu      sync.Mutex
	started bool
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	events  chan Event

	seenMu sync.Mutex
	seen   map[string]struct{}
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
	}
	rt.agents = []Agent{&EchoAgent{st: st, log: logger}}
	return rt, nil
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

func (rt *Runtime) RecentRuns(limit int) []RunRecord {
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
	for _, agent := range rt.agents {
		if ctx.Err() != nil {
			return
		}
		if !agent.CanRun(ev) {
			continue
		}
		rt.runAgent(ctx, agent, ev)
	}
}

func (rt *Runtime) runAgent(ctx context.Context, agent Agent, ev Event) {
	started := time.Now().UTC()
	runCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	rec := RunRecord{
		ID:         "run_" + newEventID(),
		ProjectID:  rt.projectID,
		EventID:    ev.ID,
		EventType:  ev.Type,
		Agent:      agent.Name(),
		Permission: PermissionObserve,
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
		report, err = agent.Run(runCtx, ev)
	}()
	rec.FinishedAt = time.Now().UTC()
	rec.DurationMs = rec.FinishedAt.Sub(rec.StartedAt).Milliseconds()
	rec.Summary = report.Summary
	rec.Details = report.Details
	rec.Tokens = report.Tokens
	rec.OutputChars = len([]rune(report.Summary))
	rec.InputChars = ev.CharCount
	if err != nil {
		rec.Status = StatusFailed
		rec.Error = err.Error()
		rt.log.Error("agent run failed", "agent", agent.Name(), "event", ev.ID, "err", err)
	}
	if recErr := rt.recorder.Append(rec); recErr != nil {
		rt.log.Error("record agent run", "agent", agent.Name(), "err", recErr)
	}
}

var eventSeq atomic.Uint64

func newEventID() string {
	return fmt.Sprintf("ev_%d_%d", time.Now().UnixNano(), eventSeq.Add(1))
}

// Recorder 追加 JSONL 并维护内存中的最近记录。
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

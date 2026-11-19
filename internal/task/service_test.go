package task

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"novelagent/internal/agentruntime"
	"novelagent/internal/novel"
	"novelagent/internal/store"
)

func newTaskFixture(t *testing.T) (*store.Store, *Service) {
	t.Helper()
	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if _, err := st.ImportChapters("任务测试", []novel.Chapter{
		{Title: "第一章", Content: "少年背着剑走进了山谷。"},
	}); err != nil {
		t.Fatalf("ImportChapters: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload := map[string]any{
			"choices": []map[string]any{{"message": map[string]any{"role": "assistant", "content": "山谷尽头亮起微光，他继续向前。"}}},
			"usage":   map[string]any{"prompt_tokens": 20, "completion_tokens": 10, "total_tokens": 30},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(payload)
	}))
	t.Cleanup(server.Close)
	cfg := store.DefaultConfig()
	cfg.BaseURL = server.URL + "/v1"
	cfg.Model = "deepseek-chat"
	cfg.APIKey = "sk-test"
	if _, err := st.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	service := NewService(st, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		service.Stop()
	})
	service.Start(ctx)
	return st, service
}

func TestBatchContinuationTaskCreatesDrafts(t *testing.T) {
	st, service := newTaskFixture(t)
	created, err := service.Create(CreateRequest{Description: "续写2章，让少年进入山谷深处"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Kind != novel.TaskKindContinueBatch || created.Total != 2 || created.Status != novel.TaskPending {
		t.Fatalf("created = %+v", created)
	}
	var params novel.TaskContinueParams
	if err := json.Unmarshal(created.Params, &params); err != nil || params.ChapterCount != 2 ||
		params.Direction != "让少年进入山谷深处" {
		t.Fatalf("params = %+v err=%v", params, err)
	}

	done := waitTaskStatus(t, st, created.ID, novel.TaskCompleted)
	if done.Progress != 2 || done.Total != 2 {
		t.Fatalf("completed task = %+v", done)
	}
	var result novel.TaskContinueResult
	if err := json.Unmarshal(done.Result, &result); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if len(result.DraftIDs) != 2 || len(result.GroupIDs) != 2 || result.Tokens != 60 {
		t.Fatalf("result = %+v", result)
	}
	drafts, err := st.LoadDrafts("")
	if err != nil || len(drafts) != 2 {
		t.Fatalf("drafts = %+v err=%v", drafts, err)
	}
	project, err := st.LoadProject()
	if err != nil || len(project.Chapters) != 1 {
		t.Fatalf("batch task must not write the manuscript: %+v err=%v", project, err)
	}
}

func TestPauseStopsBatchAtCheckpoint(t *testing.T) {
	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if _, err := st.ImportChapters("暂停测试", []novel.Chapter{{Title: "一", Content: "起点内容。"}}); err != nil {
		t.Fatalf("ImportChapters: %v", err)
	}
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	var once sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		once.Do(func() { started <- struct{}{} })
		<-release
		payload := map[string]any{
			"choices": []map[string]any{{"message": map[string]any{"role": "assistant", "content": "暂停期间的续写内容。"}}},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(payload)
	}))
	t.Cleanup(server.Close)
	cfg := store.DefaultConfig()
	cfg.BaseURL = server.URL + "/v1"
	cfg.Model = "deepseek-chat"
	if _, err := st.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	service := NewService(st, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		unblock()
		cancel()
		service.Stop()
	})
	service.Start(ctx)

	created, err := service.Create(CreateRequest{Description: "续写1章，主角继续上路"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("model call did not start")
	}
	if _, err := service.Pause(created.ID); err != nil {
		t.Fatalf("Pause: %v", err)
	}
	unblock()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		task, err := st.GetTask(created.ID)
		if err != nil {
			t.Fatalf("GetTask: %v", err)
		}
		if task.Status == novel.TaskPaused {
			drafts, err := st.LoadDrafts("")
			if err != nil {
				t.Fatalf("LoadDrafts: %v", err)
			}
			if len(drafts) != 0 {
				t.Fatalf("paused task saved %d drafts", len(drafts))
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("task did not stay paused after checkpoint")
}

type fakeBus struct {
	mu      sync.Mutex
	reports []agentruntime.Report
	events  []string
}

func (b *fakeBus) HasAgent(key string) bool { return key == "proofread" }

func (b *fakeBus) RunAgentOnce(ctx context.Context, key string, ev agentruntime.Event) (agentruntime.Report, string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.events = append(b.events, ev.Type)
	return agentruntime.Report{Summary: "发现标点问题", Tokens: 7}, "ar_fake", nil
}

func (b *fakeBus) Publish(ev agentruntime.Event) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.events = append(b.events, ev.Type)
	return true
}

func TestAgentTaskRunsExplicitPlugin(t *testing.T) {
	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if _, err := st.ImportChapters("Agent 任务", []novel.Chapter{{Title: "一", Content: "正文。"}}); err != nil {
		t.Fatalf("ImportChapters: %v", err)
	}
	bus := &fakeBus{}
	service := NewService(st, bus, nil)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		service.Stop()
	})
	service.Start(ctx)

	created, err := service.Create(CreateRequest{Description: "请校对最新章节", Priority: "high"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Kind != novel.TaskKindAgentRun || created.Priority != novel.TaskPriorityHigh {
		t.Fatalf("created = %+v", created)
	}
	var params novel.TaskAgentParams
	if err := json.Unmarshal(created.Params, &params); err != nil || params.Agent != "proofread" {
		t.Fatalf("params = %+v err=%v", params, err)
	}
	done := waitTaskStatus(t, st, created.ID, novel.TaskCompleted)
	var result map[string]any
	if err := json.Unmarshal(done.Result, &result); err != nil || result["reportId"] != "ar_fake" {
		t.Fatalf("result = %s err=%v", done.Result, err)
	}
	if _, err := service.Create(CreateRequest{Description: "跑未知 Agent", Kind: novel.TaskKindAgentRun, Params: json.RawMessage(`{"agent":"missing"}`)}); err == nil {
		t.Fatal("expected unavailable agent error")
	}
}

func waitTaskStatus(t *testing.T, st *store.Store, id string, status novel.TaskStatus) novel.Task {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		task, err := st.GetTask(id)
		if err != nil {
			t.Fatalf("GetTask: %v", err)
		}
		if task.Status == status {
			return task
		}
		if task.Status.Terminal() && task.Status != status {
			t.Fatalf("task ended as %s: %s", task.Status, task.Error)
		}
		time.Sleep(20 * time.Millisecond)
	}
	task, _ := st.GetTask(id)
	t.Fatalf("task %s did not reach %s, last = %+v", id, status, task)
	return novel.Task{}
}

func TestParseDescription(t *testing.T) {
	cases := []struct {
		text      string
		kind      string
		count     int
		agent     string
		direction string
	}{
		{"续写三章，让主角突破", novel.TaskKindContinueBatch, 3, "", "让主角突破"},
		{"写2章 攻城战", novel.TaskKindContinueBatch, 2, "", "攻城战"},
		{"续写二十章", novel.TaskKindContinueBatch, 20, "", ""},
		{"帮忙校对最新一章", novel.TaskKindAgentRun, 0, "proofread", ""},
		{"推演后续剧情", novel.TaskKindAgentRun, 0, "plot", ""},
	}
	for _, tc := range cases {
		kind, count, agent, direction := parseDescription(tc.text)
		if kind != tc.kind || count != tc.count || agent != tc.agent || direction != tc.direction {
			t.Errorf("parse(%q) = %s %d %q %q, want %s %d %q %q",
				tc.text, kind, count, agent, direction, tc.kind, tc.count, tc.agent, tc.direction)
		}
	}
	if got := parseChineseNumber("十二"); got != 12 {
		t.Errorf("parseChineseNumber(十二) = %d", got)
	}
}

// newStubTaskService 构造一个只依赖本地模型桩的任务服务；模型桩由调用方控制阻塞时机。
func newStubTaskService(t *testing.T, handler http.HandlerFunc) (*store.Store, *Service) {
	t.Helper()
	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if _, err := st.ImportChapters("恢复测试", []novel.Chapter{{Title: "一", Content: "山谷入口静得出奇。"}}); err != nil {
		t.Fatalf("ImportChapters: %v", err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	cfg := store.DefaultConfig()
	cfg.BaseURL = server.URL + "/v1"
	cfg.Model = "deepseek-chat"
	cfg.APIKey = "sk-test"
	if _, err := st.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	service := NewService(st, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		service.Stop()
	})
	service.Start(ctx)
	return st, service
}

func TestResumeBatchTaskContinuesWithoutDuplicateDrafts(t *testing.T) {
	var calls atomic.Int32
	secondStarted := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	st, service := newStubTaskService(t, func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n == 2 {
			close(secondStarted)
			<-release
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]any{"role": "assistant", "content": fmt.Sprintf("第%d段续写正文。", n)}}},
			"usage":   map[string]any{"prompt_tokens": 20, "completion_tokens": 10, "total_tokens": 30},
		})
	})
	t.Cleanup(unblock)

	created, err := service.Create(CreateRequest{Description: "续写2章，让主角进山"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	select {
	case <-secondStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("second model call did not start")
	}
	if _, err := service.Pause(created.ID); err != nil {
		t.Fatalf("Pause: %v", err)
	}
	unblock()
	waitTaskStatus(t, st, created.ID, novel.TaskPaused)
	drafts, err := st.LoadDrafts("")
	if err != nil || len(drafts) != 1 {
		t.Fatalf("after pause drafts = %d err=%v, want 1", len(drafts), err)
	}

	// 立即恢复：旧执行代次必须失效，且只补生成第 2 章。
	if _, err := service.Resume(created.ID); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	final := waitTaskStatus(t, st, created.ID, novel.TaskCompleted)
	if final.Progress != 2 {
		t.Fatalf("resumed task progress = %d, want 2", final.Progress)
	}
	drafts, err = st.LoadDrafts("")
	if err != nil || len(drafts) != 2 {
		t.Fatalf("after resume drafts = %d err=%v, want 2", len(drafts), err)
	}
	project, err := st.LoadProject()
	if err != nil || len(project.Chapters) != 1 {
		t.Fatalf("batch task must not write manuscript: %+v err=%v", project, err)
	}
	if got := calls.Load(); got != 3 {
		t.Fatalf("model calls = %d, want 3 (resume must not regenerate finished chapters)", got)
	}
}

func TestCancelBatchTaskStopsBeforeDraft(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	st, service := newStubTaskService(t, func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]any{"role": "assistant", "content": "取消后的内容不应落草稿。"}}},
		})
	})
	t.Cleanup(unblock)

	created, err := service.Create(CreateRequest{Description: "续写2章，主角进山"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("model call did not start")
	}
	if _, err := service.Cancel(created.ID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	unblock()
	waitTaskStatus(t, st, created.ID, novel.TaskCancelled)
	time.Sleep(200 * time.Millisecond)
	drafts, err := st.LoadDrafts("")
	if err != nil || len(drafts) != 0 {
		t.Fatalf("cancelled task drafts = %d err=%v, want 0", len(drafts), err)
	}
	project, err := st.LoadProject()
	if err != nil || len(project.Chapters) != 1 {
		t.Fatalf("manuscript changed after cancel: %+v err=%v", project, err)
	}
}

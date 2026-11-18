package server

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"
	"unicode/utf8"

	"novelagent/internal/novel"
	"novelagent/internal/store"
)

// newContinueFixture 准备可续写的作品：两章正文 + 人物/世界知识（导入会清空知识，须后写）。
func newContinueFixture(t *testing.T) (http.Handler, *store.Store, string) {
	t.Helper()
	h, st, dir := newMemoryTestHandler(t)
	if _, err := st.ImportChapters("雨夜", []novel.Chapter{
		{Title: "初始", Content: "雨落到青石板上。"},
		{Title: "重瞳", Content: "林渊按住左臂的伤，铜牌贴在胸口。"},
	}); err != nil {
		t.Fatalf("ImportChapters: %v", err)
	}
	writeKnowledgeFile(t, dir, novel.Knowledge{
		Characters: []novel.Character{
			{ID: "c_lin", Name: "林渊", Confidence: 0.9, LastSeen: 2},
		},
		World: []novel.WorldEntry{
			{ID: "w_bronze", Title: "铜牌", Confidence: 0.8, LastSeen: 2},
		},
	})
	return h, st, dir
}

func writeContinueMemoryFile(t *testing.T, dir, name string, value any) {
	t.Helper()
	memoryDir := filepath.Join(dir, "memory")
	if err := os.MkdirAll(memoryDir, 0o755); err != nil {
		t.Fatalf("mkdir memory: %v", err)
	}
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("encode %s: %v", name, err)
	}
	if err := os.WriteFile(filepath.Join(memoryDir, name), raw, 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

// writeContinueMemory 直写可被检索命中的最小记忆集（meta + 状态 + 事件）。
func writeContinueMemory(t *testing.T, dir string, revision int64) {
	t.Helper()
	writeContinueMemoryFile(t, dir, "meta.json", map[string]any{
		"schemaVersion":  1,
		"memoryRevision": revision,
		"updatedAt":      time.Now().UTC(),
	})
	writeContinueMemoryFile(t, dir, "states.json", []novel.StoryState{
		{
			ID:        "state_1",
			Status:    novel.StateActive,
			SubjectID: "c_lin",
			Category:  "生理",
			Attribute: "伤势",
			Value:     "左臂贯穿伤，未处理",
			MemoryMeta: novel.MemoryMeta{
				RecordVersion: 2,
			},
		},
	})
	writeContinueMemoryFile(t, dir, "events.json", []novel.StoryEvent{
		{
			ID:           "evt_1",
			Status:       novel.EventConfirmed,
			Summary:      "林渊在雨夜夺得铜牌",
			StoryOrder:   2,
			Participants: []string{"c_lin"},
			MemoryMeta:   novel.MemoryMeta{RecordVersion: 1},
		},
	})
}

type continuePreviewBody struct {
	MemorySection        string               `json:"memorySection"`
	Seeds                []novel.MemorySeed   `json:"seeds"`
	Records              []novel.MemoryRecord `json:"records"`
	Budget               int                  `json:"budget"`
	Used                 int                  `json:"used"`
	Counts               recalledCounts       `json:"counts"`
	ManuscriptChars      int                  `json:"manuscriptChars"`
	MemoryEnabled        bool                 `json:"memoryEnabled"`
	MemoryDisabledReason string               `json:"memoryDisabledReason"`
	DroppedByBudget      int                  `json:"droppedByBudget"`
	SkippedReview        int                  `json:"skippedReview"`
	SkippedAmbiguous     int                  `json:"skippedAmbiguous"`
	MemoryRevision       int64                `json:"memoryRevision"`
	PromptTotalChars     int                  `json:"promptTotalChars"`
	PromptOverBudget     bool                 `json:"promptOverBudget"`
	PromptSections       map[string]int       `json:"sections"`
	RecallSection        string               `json:"recallSection"`
	RecallEnabled        bool                 `json:"recallEnabled"`
	RecallBudget         int                  `json:"recallBudgetChars"`
	RecallUsed           int                  `json:"recallUsedChars"`
	RecallCount          int                  `json:"recallHits"`
	RecallChapters       []int                `json:"recallChapters"`
}

func decodeContinuePreview(t *testing.T, raw []byte) continuePreviewBody {
	t.Helper()
	var body continuePreviewBody
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("decode preview: %v", err)
	}
	return body
}

// snapshotDataDir 对数据目录全部文件取内容哈希，用于断言预览零落盘。
func snapshotDataDir(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := make(map[string]string)
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		out[rel] = fmt.Sprintf("%x", sha256.Sum256(raw))
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot %s: %v", dir, err)
	}
	return out
}

func assertSnapshotsEqual(t *testing.T, before, after map[string]string) {
	t.Helper()
	if len(before) != len(after) {
		t.Fatalf("data files changed: before=%d after=%d\nbefore=%v\nafter=%v", len(before), len(after), before, after)
	}
	for name, hash := range before {
		if after[name] != hash {
			t.Errorf("data file %q mutated by read-only endpoint", name)
		}
	}
}

// TestContinuePreviewContract 覆盖 iter3-详设 §7 的预览契约：注入内容、严格 JSON、只读。
func TestContinuePreviewContract(t *testing.T) {
	h, _, dir := newContinueFixture(t)
	writeContinueMemory(t, dir, 7)

	before := snapshotDataDir(t, dir)
	rec := serveRaw(t, h, http.MethodPost, "/api/continue/preview", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("empty-body preview = %d: %s", rec.Code, rec.Body.String())
	}
	body := decodeContinuePreview(t, rec.Body.Bytes())
	if !body.MemoryEnabled || body.MemoryDisabledReason != "" {
		t.Errorf("memory = enabled %v reason %q, want enabled", body.MemoryEnabled, body.MemoryDisabledReason)
	}
	if body.Budget != 3000 || body.MemoryRevision != 7 {
		t.Errorf("budget/revision = %d/%d, want 3000/7", body.Budget, body.MemoryRevision)
	}
	if body.Counts.States != 1 || body.Counts.Events != 1 || body.Counts.Threads != 0 {
		t.Errorf("counts = %+v, want states=1 events=1", body.Counts)
	}
	if len(body.Seeds) == 0 || body.Seeds[0].EntityID != "c_lin" {
		t.Errorf("seeds = %+v, want c_lin first", body.Seeds)
	}
	if body.Used == 0 || body.Used != utf8.RuneCountInString(body.MemorySection) {
		t.Errorf("used = %d, section runes = %d", body.Used, utf8.RuneCountInString(body.MemorySection))
	}
	for _, want := range []string{
		"【已确认剧情记忆（来自正文抽取，效力低于下方最近正文）】",
		"人物当前状态：",
		"林渊｜伤势：左臂贯穿伤，未处理",
		"关键事件（近因优先）：",
		"第2章：林渊在雨夜夺得铜牌",
	} {
		if !strings.Contains(body.MemorySection, want) {
			t.Errorf("memorySection missing %q:\n%s", want, body.MemorySection)
		}
	}
	if body.ManuscriptChars == 0 {
		t.Error("manuscriptChars = 0, want the recent manuscript window size")
	}
	if len(body.Records) != 2 {
		t.Errorf("records = %+v, want 2", body.Records)
	}

	// I3-7/R2：预览必须暴露全量 prompt 计量，且八段计数不超过总量。
	if body.PromptTotalChars == 0 || body.PromptOverBudget {
		t.Errorf("prompt budget = total %d over %v, want >0 and false", body.PromptTotalChars, body.PromptOverBudget)
	}
	if len(body.PromptSections) != 8 {
		t.Errorf("promptSections = %v, want 8 sections", body.PromptSections)
	}
	sectionSum := 0
	for _, chars := range body.PromptSections {
		sectionSum += chars
	}
	if sectionSum == 0 || sectionSum > body.PromptTotalChars {
		t.Errorf("sum(promptSections) = %d, promptTotalChars = %d", sectionSum, body.PromptTotalChars)
	}
	if got := body.PromptSections["manuscript"]; got <= body.ManuscriptChars {
		t.Errorf("manuscript section = %d, want > manuscriptChars %d (heading included)", got, body.ManuscriptChars)
	}
	if body.PromptSections["memories"] != body.Used {
		t.Errorf("memories section = %d, memory used = %d, want equal", body.PromptSections["memories"], body.Used)
	}

	// 只读：预览不得创建/改写任何数据文件（含 memory/meta.json 与记忆版本）。
	assertSnapshotsEqual(t, before, snapshotDataDir(t, dir))

	// useMemory=false 完全不检索。
	rec = serveJSON(t, h, http.MethodPost, "/api/continue/preview", map[string]any{"useMemory": false})
	if rec.Code != http.StatusOK {
		t.Fatalf("useMemory=false preview = %d: %s", rec.Code, rec.Body.String())
	}
	body = decodeContinuePreview(t, rec.Body.Bytes())
	if body.MemoryEnabled || body.Budget != 0 || body.MemoryRevision != 0 || body.MemoryDisabledReason != "" {
		t.Errorf("useMemory=false body = %+v", body)
	}
	if body.MemorySection != "" || len(body.Records) != 0 || len(body.Seeds) != 0 {
		t.Errorf("useMemory=false still returned memory: %+v", body)
	}
	if body.PromptSections["memories"] != 0 {
		t.Errorf("useMemory=false prompt memories section = %d, want 0", body.PromptSections["memories"])
	}

	// 严格 JSON：未知字段 / 尾随 JSON / 数组 / 类型错误 → 400。
	for name, raw := range map[string]string{
		"unknown field": `{"bogus":1}`,
		"trailing json": `{"targetChars":300}{"targetChars":400}`,
		"array":         `[1]`,
		"null":          `null`,
		"invalid type":  `{"useMemory":"yes"}`,
	} {
		t.Run(name, func(t *testing.T) {
			rec := serveRaw(t, h, http.MethodPost, "/api/continue/preview", strings.NewReader(raw))
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
			}
			if code := decodeErrorCode(t, rec.Body.String()); code != "invalid_request" {
				t.Errorf("code = %q, want invalid_request", code)
			}
		})
	}

	// 4096B 上限 → 413。
	oversize := `{"direction":"` + strings.Repeat("雨", 1400) + `"}`
	rec = serveRaw(t, h, http.MethodPost, "/api/continue/preview", strings.NewReader(oversize))
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversize preview = %d: %s", rec.Code, rec.Body.String())
	}
	if code := decodeErrorCode(t, rec.Body.String()); code != "request_too_large" {
		t.Errorf("oversize code = %q, want request_too_large", code)
	}
	assertSnapshotsEqual(t, before, snapshotDataDir(t, dir))
}

// TestContinuePreviewDegradations 覆盖降级矩阵：memory_absent / read_error / safe_state。
func TestContinuePreviewDegradations(t *testing.T) {
	t.Run("memory_absent", func(t *testing.T) {
		h, _, dir := newContinueFixture(t)
		before := snapshotDataDir(t, dir)
		rec := serveRaw(t, h, http.MethodPost, "/api/continue/preview", nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("preview = %d: %s", rec.Code, rec.Body.String())
		}
		body := decodeContinuePreview(t, rec.Body.Bytes())
		if body.MemoryEnabled || body.MemoryDisabledReason != novel.MemoryDisabledAbsent {
			t.Errorf("body = %+v, want memory_absent", body)
		}
		if body.Budget != 3000 || body.MemoryRevision != 0 {
			t.Errorf("budget/revision = %d/%d", body.Budget, body.MemoryRevision)
		}
		assertSnapshotsEqual(t, before, snapshotDataDir(t, dir))
	})

	// I3-11：五类记忆文件逐一损坏 → 零注入 + read_error，且不阻断续写。
	t.Run("read_error", func(t *testing.T) {
		for _, corrupted := range []string{"states.json", "events.json", "threads.json", "cognitions.json", "plans.json"} {
			t.Run(corrupted, func(t *testing.T) {
				h, _, dir := newContinueFixture(t)
				writeContinueMemory(t, dir, 3)
				if err := os.WriteFile(filepath.Join(dir, "memory", corrupted), []byte("{not json"), 0o644); err != nil {
					t.Fatalf("corrupt %s: %v", corrupted, err)
				}

				rec := serveRaw(t, h, http.MethodPost, "/api/continue/preview", nil)
				if rec.Code != http.StatusOK {
					t.Fatalf("preview with corrupt memory = %d: %s", rec.Code, rec.Body.String())
				}
				body := decodeContinuePreview(t, rec.Body.Bytes())
				if body.MemoryEnabled || body.MemoryDisabledReason != novel.MemoryDisabledReadError {
					t.Errorf("body = %+v, want read_error", body)
				}
				if body.MemoryRevision != 3 || body.Budget != 3000 || body.Used != 0 || len(body.Records) != 0 {
					t.Errorf("budget/revision/used/records = %d/%d/%d/%d, want 3000/3/0/0",
						body.Budget, body.MemoryRevision, body.Used, len(body.Records))
				}

				// 显式关闭注入时不得读取记忆文件（损坏文件同样被忽略）。
				rec = serveJSON(t, h, http.MethodPost, "/api/continue/preview", map[string]any{"useMemory": false})
				if rec.Code != http.StatusOK {
					t.Fatalf("preview useMemory=false = %d: %s", rec.Code, rec.Body.String())
				}
				body = decodeContinuePreview(t, rec.Body.Bytes())
				if body.MemoryDisabledReason != "" || body.MemoryRevision != 0 || body.Budget != 0 {
					t.Errorf("useMemory=false still read memory: %+v", body)
				}
			})
		}
	})

	t.Run("safe_state", func(t *testing.T) {
		dir := t.TempDir()
		memoryDir := filepath.Join(dir, "memory")
		if err := os.MkdirAll(memoryDir, 0o755); err != nil {
			t.Fatalf("mkdir memory: %v", err)
		}
		// commits.json 损坏 → 启动恢复进入安全态（R4），一切记忆读取降级但不阻断续写。
		if err := os.WriteFile(filepath.Join(memoryDir, "commits.json"), []byte("{broken"), 0o644); err != nil {
			t.Fatalf("corrupt commits.json: %v", err)
		}
		st, err := store.New(dir)
		if err != nil {
			t.Fatalf("store.New: %v", err)
		}
		static := fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<!doctype html>")}}
		h := New(st, static, nil, &fakeAnalyzer{}, nil)

		rec := serveRaw(t, h, http.MethodPost, "/api/continue/preview", nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("preview in safe state = %d: %s", rec.Code, rec.Body.String())
		}
		body := decodeContinuePreview(t, rec.Body.Bytes())
		if body.MemoryEnabled || body.MemoryDisabledReason != novel.MemoryDisabledSafeState {
			t.Errorf("body = %+v, want safe_state", body)
		}
	})
}

func sseMeta(t *testing.T, stream string) map[string]any {
	t.Helper()
	lines := strings.Split(stream, "\n")
	for i, line := range lines {
		if line != "event: meta" {
			continue
		}
		if i+1 >= len(lines) || !strings.HasPrefix(lines[i+1], "data: ") {
			t.Fatalf("meta event has no data line:\n%s", stream)
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(strings.TrimPrefix(lines[i+1], "data: ")), &payload); err != nil {
			t.Fatalf("decode meta: %v", err)
		}
		return payload
	}
	t.Fatalf("SSE stream has no meta event:\n%s", stream)
	return nil
}

func sseMetaInt(t *testing.T, meta map[string]any, key string) int {
	t.Helper()
	value, ok := meta[key].(float64)
	if !ok {
		t.Fatalf("meta[%q] = %v (%T), want number", key, meta[key], meta[key])
	}
	return int(value)
}

func sseEvent(t *testing.T, stream, name string) map[string]any {
	t.Helper()
	lines := strings.Split(stream, "\n")
	for i, line := range lines {
		if line != "event: "+name {
			continue
		}
		if i+1 >= len(lines) || !strings.HasPrefix(lines[i+1], "data: ") {
			t.Fatalf("SSE event %q has no data line:\n%s", name, stream)
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(strings.TrimPrefix(lines[i+1], "data: ")), &payload); err != nil {
			t.Fatalf("decode SSE event %q: %v", name, err)
		}
		return payload
	}
	t.Fatalf("SSE stream has no %q event:\n%s", name, stream)
	return nil
}

// newFinishReasonProvider 模拟上游在最后一帧返回指定 finish_reason（R1 截断可见性）。
func newFinishReasonProvider(t *testing.T, finishReason string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"门\"}}]}\n\n")
		fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":%q}]}\n\n", finishReason)
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
}

// TestContinueReportsTruncatedOutput 覆盖 I3-6/R1：上游 finish_reason 决定 done.truncated，
// 截断只做可见性提示，不改变成功语义。
func TestContinueReportsTruncatedOutput(t *testing.T) {
	for _, tc := range []struct {
		name   string
		reason string
		want   bool
	}{
		{name: "length", reason: "length", want: true},
		{name: "max_tokens", reason: "max_tokens", want: true},
		{name: "stop", reason: "stop", want: false},
		{name: "missing", reason: "", want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, st, _ := newContinueFixture(t)
			provider := newFinishReasonProvider(t, tc.reason)
			defer provider.Close()
			if _, err := st.SaveConfig(store.Config{
				BaseURL:      provider.URL + "/v1",
				Model:        "mock-model",
				ContextChars: 8000,
			}); err != nil {
				t.Fatalf("SaveConfig: %v", err)
			}

			rec := serveJSON(t, h, http.MethodPost, "/api/continue", map[string]any{"targetChars": 200})
			if rec.Code != http.StatusOK {
				t.Fatalf("POST /api/continue = %d: %s", rec.Code, rec.Body.String())
			}
			done := sseEvent(t, rec.Body.String(), "done")
			if done["truncated"] != tc.want {
				t.Errorf("done.truncated = %v, want %v (finish_reason=%q)", done["truncated"], tc.want, tc.reason)
			}
			if chars, ok := done["chars"].(float64); !ok || chars <= 0 {
				t.Errorf("done.chars = %v, want >0", done["chars"])
			}
		})
	}
}

// TestContinueSafeStateDoesNotBlock 覆盖 I3-10：记忆链断链进入安全态后，
// 续写仍返回 200、零注入，meta 标记 safe_state。
func TestContinueSafeStateDoesNotBlock(t *testing.T) {
	_, _, dir := newContinueFixture(t)
	if err := os.MkdirAll(filepath.Join(dir, "memory"), 0o755); err != nil {
		t.Fatalf("mkdir memory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "memory", "commits.json"), []byte("{broken"), 0o644); err != nil {
		t.Fatalf("corrupt commits.json: %v", err)
	}
	st, err := store.New(dir)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	static := fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<!doctype html>")}}
	h := New(st, static, nil, &fakeAnalyzer{}, nil)

	provider, captured := newMockProvider(t)
	defer provider.Close()
	if _, err := st.SaveConfig(store.Config{
		BaseURL:      provider.URL + "/v1",
		Model:        "mock-model",
		ContextChars: 8000,
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	rec := serveJSON(t, h, http.MethodPost, "/api/continue", map[string]any{"targetChars": 200})
	if rec.Code != http.StatusOK {
		t.Fatalf("continue in safe state = %d: %s", rec.Code, rec.Body.String())
	}
	meta := sseMeta(t, rec.Body.String())
	if meta["memoryEnabled"] != false || meta["memoryDisabledReason"] != novel.MemoryDisabledSafeState {
		t.Errorf("safe-state meta = %v, want disabled safe_state", meta)
	}
	select {
	case got := <-captured:
		if strings.Contains(got.messages[1].Content, "已确认剧情记忆") || strings.Contains(got.messages[0].Content, "3.1 【已确认剧情记忆】") {
			t.Error("safe state injected memory into prompt")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("provider was not called in safe state")
	}
}

// TestContinueInjectsMemoryIntoPromptAndMeta 覆盖 I3 接线：SSE meta 扩展 + 提示词实际注入 + 开关。
func TestContinueInjectsMemoryIntoPromptAndMeta(t *testing.T) {
	h, st, dir := newContinueFixture(t)
	writeContinueMemory(t, dir, 7)

	provider, captured := newMockProvider(t)
	defer provider.Close()
	if _, err := st.SaveConfig(store.Config{
		BaseURL:      provider.URL + "/v1",
		Model:        "mock-model",
		ContextChars: 8000,
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	rec := serveJSON(t, h, http.MethodPost, "/api/continue", map[string]any{"targetChars": 300})
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/continue = %d: %s", rec.Code, rec.Body.String())
	}
	meta := sseMeta(t, rec.Body.String())
	if meta["memoryEnabled"] != true {
		t.Errorf("memoryEnabled = %v, want true", meta["memoryEnabled"])
	}
	if got := sseMetaInt(t, meta, "memoryRevision"); got != 7 {
		t.Errorf("memoryRevision = %d, want 7", got)
	}
	if got := sseMetaInt(t, meta, "memoryBudgetChars"); got != 2000 {
		t.Errorf("memoryBudgetChars = %d, want 2000 (8000/4)", got)
	}
	if sseMetaInt(t, meta, "memoryUsedChars") == 0 {
		t.Error("memoryUsedChars = 0, want >0")
	}
	if got := sseMetaInt(t, meta, "seeds"); got < 1 {
		t.Errorf("seeds = %d, want >=1", got)
	}
	counts, ok := meta["recalledByKind"].(map[string]any)
	if !ok {
		t.Fatalf("recalledByKind = %T", meta["recalledByKind"])
	}
	if counts["states"] != float64(1) || counts["events"] != float64(1) {
		t.Errorf("recalledByKind = %v, want states=1 events=1", counts)
	}
	if meta["memoryDisabledReason"] != "" {
		t.Errorf("memoryDisabledReason = %v, want empty", meta["memoryDisabledReason"])
	}
	// I3-7/R2：SSE meta 暴露全量 prompt 计量。
	if sseMetaInt(t, meta, "promptTotalChars") == 0 {
		t.Error("promptTotalChars = 0, want >0")
	}
	if meta["promptOverBudget"] != false {
		t.Errorf("promptOverBudget = %v, want false", meta["promptOverBudget"])
	}
	promptSections, ok := meta["promptSections"].(map[string]any)
	if !ok || len(promptSections) != 8 {
		t.Errorf("promptSections = %v (%T), want 8 sections", meta["promptSections"], meta["promptSections"])
	} else if memories, _ := promptSections["memories"].(float64); int(memories) != sseMetaInt(t, meta, "memoryUsedChars") {
		t.Errorf("promptSections.memories = %v, memoryUsedChars = %v, want equal", memories, meta["memoryUsedChars"])
	}
	// R4：短篇（<20 章）自动关闭召回，但计量字段必须存在。
	if meta["recallEnabled"] != false {
		t.Errorf("recallEnabled = %v, want false for short novel", meta["recallEnabled"])
	}
	if sseMetaInt(t, meta, "recallBudgetChars") != 0 || sseMetaInt(t, meta, "recallUsedChars") != 0 {
		t.Errorf("recall budget/used = %v/%v, want 0/0", meta["recallBudgetChars"], meta["recallUsedChars"])
	}
	if chapters, ok := meta["recallChapters"].([]any); !ok || len(chapters) != 0 {
		t.Errorf("recallChapters = %v (%T), want empty array", meta["recallChapters"], meta["recallChapters"])
	}

	select {
	case got := <-captured:
		userPrompt := got.messages[1].Content
		for _, want := range []string{
			"【已确认剧情记忆（来自正文抽取，效力低于下方最近正文）】",
			"林渊｜伤势：左臂贯穿伤，未处理",
			"第2章：林渊在雨夜夺得铜牌",
		} {
			if !strings.Contains(userPrompt, want) {
				t.Errorf("user prompt missing memory %q:\n%s", want, userPrompt)
			}
		}
		if !strings.Contains(got.messages[0].Content, "3.1 【已确认剧情记忆】") {
			t.Error("system prompt is missing the memory precedence rule")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("provider was not called")
	}

	// 关闭注入 + 损坏的记忆文件：证明完全不读取且不阻断续写。
	if err := os.WriteFile(filepath.Join(dir, "memory", "states.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatalf("corrupt states.json: %v", err)
	}
	rec = serveJSON(t, h, http.MethodPost, "/api/continue", map[string]any{"useMemory": false})
	if rec.Code != http.StatusOK {
		t.Fatalf("useMemory=false continue = %d: %s", rec.Code, rec.Body.String())
	}
	meta = sseMeta(t, rec.Body.String())
	if meta["memoryEnabled"] != false || meta["memoryDisabledReason"] != "" {
		t.Errorf("useMemory=false meta = %v", meta)
	}
	if sseMetaInt(t, meta, "memoryBudgetChars") != 0 || sseMetaInt(t, meta, "memoryRevision") != 0 {
		t.Errorf("useMemory=false budget/revision = %v/%v", meta["memoryBudgetChars"], meta["memoryRevision"])
	}
	select {
	case got := <-captured:
		if strings.Contains(got.messages[1].Content, "已确认剧情记忆") {
			t.Error("useMemory=false prompt still contains the memory section")
		}
		if strings.Contains(got.messages[0].Content, "3.1") {
			t.Error("useMemory=false system prompt still contains the memory rule")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("provider was not called for useMemory=false")
	}

	// 默认开启但读取失败：降级 read_error，续写仍返回 200 且照常调用模型。
	rec = serveJSON(t, h, http.MethodPost, "/api/continue", map[string]any{"targetChars": 200})
	if rec.Code != http.StatusOK {
		t.Fatalf("continue with corrupt memory = %d: %s", rec.Code, rec.Body.String())
	}
	meta = sseMeta(t, rec.Body.String())
	if meta["memoryEnabled"] != false || meta["memoryDisabledReason"] != novel.MemoryDisabledReadError {
		t.Errorf("degraded meta = %v, want read_error", meta)
	}
	select {
	case <-captured:
	case <-time.After(3 * time.Second):
		t.Fatal("provider was not called after memory read error")
	}
}

// TestContinueReportsReviewSkippedMemory 覆盖 G4 冲突提示链路：
// 待复核记录不注入提示词，但 skippedReview 必须出现在 SSE meta，供前端提示用户处理。
func TestContinueReportsReviewSkippedMemory(t *testing.T) {
	h, st, dir := newContinueFixture(t)
	writeContinueMemory(t, dir, 4)
	writeContinueMemoryFile(t, dir, "states.json", []novel.StoryState{
		{
			ID:        "state_stale",
			Status:    novel.StateActive,
			SubjectID: "c_lin",
			Category:  "生理",
			Attribute: "伤势",
			Value:     "左臂贯穿伤，未处理",
			MemoryMeta: novel.MemoryMeta{
				RecordVersion: 2,
				NeedsReview:   true,
				ReviewReason:  "evidence_stale",
			},
		},
	})

	provider, captured := newMockProvider(t)
	defer provider.Close()
	if _, err := st.SaveConfig(store.Config{
		BaseURL:      provider.URL + "/v1",
		Model:        "mock-model",
		ContextChars: 8000,
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	rec := serveJSON(t, h, http.MethodPost, "/api/continue", map[string]any{"targetChars": 200})
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/continue = %d: %s", rec.Code, rec.Body.String())
	}
	meta := sseMeta(t, rec.Body.String())
	if got := sseMetaInt(t, meta, "skippedReview"); got != 1 {
		t.Errorf("skippedReview = %d, want 1", got)
	}
	select {
	case got := <-captured:
		if strings.Contains(got.messages[1].Content, "左臂贯穿伤，未处理") {
			t.Error("review-marked record was injected into the prompt")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("provider was not called")
	}
}

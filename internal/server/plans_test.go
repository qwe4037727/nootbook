package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"novelagent/internal/llm"
	"novelagent/internal/novel"
	"novelagent/internal/store"
)

// 计划 API 契约测试（F20–F23）：生成 → 树 → 激活/传播 → 续写对齐。

type planProviderCapture struct {
	messages []llm.Message
	stream   bool
}

type planProviderOptions struct {
	reply string
}

// newPlanProvider 同时支持 CompleteChat（JSON）与 StreamChat（SSE），便于生成与续写共用。
func newPlanProvider(t *testing.T, opts planProviderOptions) (*httptest.Server, chan planProviderCapture) {
	t.Helper()
	captured := make(chan planProviderCapture, 8)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []llm.Message `json:"messages"`
			Stream   bool          `json:"stream"`
		}
		err := json.NewDecoder(r.Body).Decode(&body)
		captured <- planProviderCapture{messages: body.Messages, stream: body.Stream}
		if err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if body.Stream {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"门开了。\"}}]}\n\n")
			fmt.Fprint(w, "data: [DONE]\n\n")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{
				map[string]any{"message": map[string]any{"role": "assistant", "content": opts.reply}},
			},
		})
	}))
	t.Cleanup(provider.Close)
	return provider, captured
}

func defaultPlanReply() string {
	outline := map[string]any{
		"global": map[string]any{
			"title":          "黑潮全书",
			"objective":      "林渊查清铜牌来历并阻止黑潮复苏",
			"constraints":    []string{"林渊左臂伤未愈前不能全力出手"},
			"candidateBeats": []string{"追查铜牌来源", "第一次直面黑潮"},
			"characters":     []string{"林渊", "苏青"},
			"flexibility":    "high",
		},
		"volumes": []any{
			map[string]any{
				"title":          "卷一 雨夜",
				"objective":      "夺回铜牌并逃出县城",
				"constraints":    []string{"不暴露重瞳"},
				"candidateBeats": []string{"夜探县衙"},
				"characters":     []string{"林渊"},
				"flexibility":    "medium",
			},
			map[string]any{
				"title":          "卷二 黑潮",
				"objective":      "在黑潮降临前找到封印",
				"constraints":    []string{"不提前揭晓幕后黑手"},
				"candidateBeats": []string{"进入废寺"},
				"characters":     []string{"苏青"},
				"flexibility":    "low",
			},
		},
	}
	raw, _ := json.Marshal(outline)
	return "```json\n" + string(raw) + "\n```"
}

func newPlanFixture(t *testing.T, reply string) (http.Handler, *store.Store, *httptest.Server, chan planProviderCapture) {
	t.Helper()
	h, st, dir := newMemoryTestHandler(t)
	if _, err := st.ImportChapters("雨夜", []novel.Chapter{
		{Title: "初始", Content: "雨落到青石板上。"},
		{Title: "重瞳", Content: "林渊按住左臂的伤，铜牌贴在胸口。"},
	}); err != nil {
		t.Fatalf("ImportChapters: %v", err)
	}
	writeKnowledgeFile(t, dir, novel.Knowledge{
		Overview: "灵气复苏后的县城，黑潮将至。",
		Characters: []novel.Character{
			{ID: "c_lin", Name: "林渊", Aliases: []string{"阿渊"}, Role: "捕快", Confidence: 0.9, LastSeen: 2},
			{ID: "c_su", Name: "苏青", Role: "医女", Confidence: 0.8, LastSeen: 2},
		},
		World: []novel.WorldEntry{
			{ID: "w_bronze", Title: "铜牌", Content: "镇压黑潮的信物。", Confidence: 0.8, LastSeen: 2},
		},
	})
	provider, captured := newPlanProvider(t, planProviderOptions{reply: reply})
	if _, err := st.SaveConfig(store.Config{
		BaseURL:      provider.URL + "/v1",
		Model:        "mock-model",
		ContextChars: 8000,
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	return h, st, provider, captured
}

type generatedPlansBody struct {
	Plans          []novel.StoryPlan `json:"plans"`
	MemoryRevision int64             `json:"memoryRevision"`
	CommitID       string            `json:"commitId"`
}

func generatePlans(t *testing.T, h http.Handler, extra map[string]any) (*httptest.ResponseRecorder, generatedPlansBody) {
	t.Helper()
	body := map[string]any{"volumeCount": 2}
	for k, v := range extra {
		body[k] = v
	}
	rec := serveJSON(t, h, http.MethodPost, "/api/plans/generate", body)
	var out generatedPlansBody
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode generate: %v: %s", err, rec.Body.String())
		}
	}
	return rec, out
}

func nextPlanCapture(t *testing.T, captured chan planProviderCapture, wantStream bool) planProviderCapture {
	t.Helper()
	select {
	case got := <-captured:
		if got.stream != wantStream {
			t.Fatalf("provider stream = %v, want %v", got.stream, wantStream)
		}
		return got
	case <-time.After(3 * time.Second):
		t.Fatalf("provider was not called (wantStream=%v)", wantStream)
		return planProviderCapture{}
	}
}

func decodePlanTree(t *testing.T, raw []byte) store.PlanTree {
	t.Helper()
	var tree store.PlanTree
	if err := json.Unmarshal(raw, &tree); err != nil {
		t.Fatalf("decode plan tree: %v: %s", err, string(raw))
	}
	return tree
}

func TestPlanTreeEmpty(t *testing.T) {
	h, _, _ := newMemoryTestHandler(t)
	rec := serveJSON(t, h, http.MethodGet, "/api/plans", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/plans = %d: %s", rec.Code, rec.Body.String())
	}
	tree := decodePlanTree(t, rec.Body.Bytes())
	if tree.Count != 0 || len(tree.Roots) != 0 || tree.MemoryRevision != 0 {
		t.Errorf("empty tree = %+v, want zero roots", tree)
	}
}

func TestPlanGenerateActivateAndContinueAlignment(t *testing.T) {
	h, _, _, captured := newPlanFixture(t, defaultPlanReply())

	rec, generated := generatePlans(t, h, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("generate = %d: %s", rec.Code, rec.Body.String())
	}
	if len(generated.Plans) != 3 || generated.MemoryRevision != 1 || generated.CommitID == "" {
		t.Fatalf("generate body = %+v, want 3 plans at revision 1", generated)
	}
	var globalID string
	for _, p := range generated.Plans {
		if p.Status != novel.PlanProposed {
			t.Errorf("plan %s status = %s, want proposed", p.ID, p.Status)
		}
		if p.Level == string(novel.PlanLevelGlobal) {
			globalID = p.ID
		}
	}
	if globalID == "" {
		t.Fatal("no global plan generated")
	}
	for _, p := range generated.Plans {
		if p.Level == string(novel.PlanLevelVolume) && p.ParentPlanID != globalID {
			t.Errorf("volume %s parent = %q, want %q", p.ID, p.ParentPlanID, globalID)
		}
	}

	// 模型输入包含知识与防冲突规则。
	got := nextPlanCapture(t, captured, false)
	if !strings.Contains(got.messages[0].Content, "只输出一个 JSON 对象") {
		t.Errorf("system prompt missing JSON contract")
	}
	if !strings.Contains(got.messages[1].Content, "黑潮将至") {
		t.Errorf("user prompt missing knowledge overview")
	}

	// 计划树：1 根 + 2 子。
	rec = serveJSON(t, h, http.MethodGet, "/api/plans", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/plans = %d: %s", rec.Code, rec.Body.String())
	}
	tree := decodePlanTree(t, rec.Body.Bytes())
	if tree.Count != 3 || len(tree.Roots) != 1 || len(tree.Roots[0].Children) != 2 {
		t.Fatalf("tree = %+v, want 1 root with 2 children", tree)
	}
	if tree.Roots[0].Plan.ID != globalID {
		t.Errorf("root id = %s, want %s", tree.Roots[0].Plan.ID, globalID)
	}

	// 激活全局 + 卷，再生成并激活新全局 → 旧全局 superseded、旧卷 paused（U22）。
	rec = serveJSON(t, h, http.MethodPost, "/api/plans/"+globalID+"/activate", map[string]any{})
	if rec.Code != http.StatusOK {
		t.Fatalf("activate global = %d: %s", rec.Code, rec.Body.String())
	}
	var volumeID string
	for _, p := range generated.Plans {
		if p.Level == string(novel.PlanLevelVolume) {
			volumeID = p.ID
			break
		}
	}
	rec = serveJSON(t, h, http.MethodPost, "/api/plans/"+volumeID+"/status", map[string]any{"status": "active"})
	if rec.Code != http.StatusOK {
		t.Fatalf("activate volume = %d: %s", rec.Code, rec.Body.String())
	}

	rec, second := generatePlans(t, h, nil)
	if rec.Code != http.StatusOK || len(second.Plans) != 3 {
		t.Fatalf("second generate = %d: %s", rec.Code, rec.Body.String())
	}
	nextPlanCapture(t, captured, false)
	var secondGlobal string
	for _, p := range second.Plans {
		if p.Level == string(novel.PlanLevelGlobal) {
			secondGlobal = p.ID
		}
	}
	rec = serveJSON(t, h, http.MethodPost, "/api/plans/"+secondGlobal+"/activate", map[string]any{})
	if rec.Code != http.StatusOK {
		t.Fatalf("activate second global = %d: %s", rec.Code, rec.Body.String())
	}
	rec = serveJSON(t, h, http.MethodGet, "/api/plans", nil)
	tree = decodePlanTree(t, rec.Body.Bytes())
	statuses := map[string]novel.StoryPlanStatus{}
	var walk func(nodes []store.PlanNode)
	walk = func(nodes []store.PlanNode) {
		for _, n := range nodes {
			statuses[n.Plan.ID] = n.Plan.Status
			walk(n.Children)
		}
	}
	walk(tree.Roots)
	if statuses[globalID] != novel.PlanSuperseded {
		t.Errorf("old global = %s, want superseded", statuses[globalID])
	}
	if statuses[volumeID] != novel.PlanPaused {
		t.Errorf("old volume = %s, want paused", statuses[volumeID])
	}
	if statuses[secondGlobal] != novel.PlanActive {
		t.Errorf("new global = %s, want active", statuses[secondGlobal])
	}

	// 预览注入计划段；usePlan=false 关闭。
	rec = serveJSON(t, h, http.MethodPost, "/api/continue/preview", map[string]any{})
	if rec.Code != http.StatusOK {
		t.Fatalf("preview = %d: %s", rec.Code, rec.Body.String())
	}
	var preview struct {
		PlanSection string `json:"planSection"`
		PlanEnabled bool   `json:"planEnabled"`
		PlanCount   int    `json:"planCount"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &preview); err != nil {
		t.Fatalf("decode preview: %v", err)
	}
	if !preview.PlanEnabled || preview.PlanCount < 1 || preview.PlanSection == "" {
		t.Fatalf("preview plan = %+v, want enabled section", preview)
	}
	if !strings.Contains(preview.PlanSection, novel.PlanSectionHeader) || !strings.Contains(preview.PlanSection, "黑潮全书") {
		t.Errorf("planSection missing header/title:\n%s", preview.PlanSection)
	}
	rec = serveJSON(t, h, http.MethodPost, "/api/continue/preview", map[string]any{"usePlan": false})
	if err := json.Unmarshal(rec.Body.Bytes(), &preview); err != nil {
		t.Fatalf("decode preview: %v", err)
	}
	if preview.PlanEnabled || preview.PlanSection != "" || preview.PlanCount != 0 {
		t.Errorf("usePlan=false preview = %+v", preview)
	}

	// 续写：SSE meta 标记对齐，提示词包含计划段与 3.2 规则。
	rec = serveJSON(t, h, http.MethodPost, "/api/continue", map[string]any{"targetChars": 300})
	if rec.Code != http.StatusOK {
		t.Fatalf("continue = %d: %s", rec.Code, rec.Body.String())
	}
	meta := sseMeta(t, rec.Body.String())
	if meta["planAligned"] != true || sseMetaInt(t, meta, "planCount") != 1 {
		t.Errorf("continue meta plan fields = aligned %v count %v", meta["planAligned"], meta["planCount"])
	}
	last := nextPlanCapture(t, captured, true)
	if !strings.Contains(last.messages[1].Content, novel.PlanSectionHeader) {
		t.Errorf("continuation user prompt missing plan section:\n%s", last.messages[1].Content)
	}
	if !strings.Contains(last.messages[0].Content, "3.2 【计划对齐】") {
		t.Error("continuation system prompt missing plan rule")
	}

	rec = serveJSON(t, h, http.MethodPost, "/api/continue", map[string]any{"usePlan": false})
	meta = sseMeta(t, rec.Body.String())
	if meta["planAligned"] != false || sseMetaInt(t, meta, "planCount") != 0 {
		t.Errorf("usePlan=false meta = alive %v count %v", meta["planAligned"], meta["planCount"])
	}
	got = nextPlanCapture(t, captured, true)
	if strings.Contains(got.messages[1].Content, novel.PlanSectionHeader) || strings.Contains(got.messages[0].Content, "3.2") {
		t.Error("usePlan=false prompt still contains plan alignment")
	}
}

func TestPlanGenerateErrors(t *testing.T) {
	// 配置缺失优先于知识库检查。
	h, _, _ := newMemoryTestHandler(t)
	rec, _ := generatePlans(t, h, nil)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "config_missing") {
		t.Errorf("missing config = %d: %s", rec.Code, rec.Body.String())
	}

	// 知识库为空。
	h2, st2, _ := newMemoryTestHandler(t)
	provider, _ := newPlanProvider(t, planProviderOptions{reply: defaultPlanReply()})
	if _, err := st2.SaveConfig(store.Config{BaseURL: provider.URL + "/v1", Model: "mock-model"}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	rec, _ = generatePlans(t, h2, nil)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "knowledge_not_ready") {
		t.Errorf("empty knowledge = %d: %s", rec.Code, rec.Body.String())
	}

	// 模型输出不可解析。
	h3, _, _, _ := newPlanFixture(t, "这不是 JSON")
	rec, _ = generatePlans(t, h3, nil)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "invalid_plan_output") {
		t.Errorf("invalid output = %d: %s", rec.Code, rec.Body.String())
	}

	// 未知字段 → 400。
	h4, _, _, _ := newPlanFixture(t, defaultPlanReply())
	rec = serveJSON(t, h4, http.MethodPost, "/api/plans/generate", map[string]any{"bogus": 1})
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "invalid_request") {
		t.Errorf("unknown field = %d: %s", rec.Code, rec.Body.String())
	}
}

func TestPlanStatusContract(t *testing.T) {
	h, _, _, _ := newPlanFixture(t, defaultPlanReply())
	_, generated := generatePlans(t, h, nil)

	// 非法 ID / 不存在 / 非法状态 / 非法迁移。
	rec := serveJSON(t, h, http.MethodPost, "/api/plans/not-a-plan/activate", map[string]any{})
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "invalid_plan_id") {
		t.Errorf("invalid id = %d: %s", rec.Code, rec.Body.String())
	}
	rec = serveJSON(t, h, http.MethodPost, "/api/plans/plan_missing/activate", map[string]any{})
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "plan_not_found") {
		t.Errorf("missing plan = %d: %s", rec.Code, rec.Body.String())
	}
	globalID := generated.Plans[0].ID
	rec = serveJSON(t, h, http.MethodPost, "/api/plans/"+globalID+"/status", map[string]any{"status": "bogus"})
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "invalid_status") {
		t.Errorf("invalid status = %d: %s", rec.Code, rec.Body.String())
	}
	rec = serveJSON(t, h, http.MethodPost, "/api/plans/"+globalID+"/status", map[string]any{"status": "completed"})
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "plan_transition") {
		t.Errorf("proposed→completed = %d: %s", rec.Code, rec.Body.String())
	}

	// 激活 → 暂停 → 完成。
	for _, status := range []string{"active", "paused", "active", "completed"} {
		rec = serveJSON(t, h, http.MethodPost, "/api/plans/"+globalID+"/status", map[string]any{"status": status})
		if rec.Code != http.StatusOK {
			t.Fatalf("%s = %d: %s", status, rec.Code, rec.Body.String())
		}
	}
}

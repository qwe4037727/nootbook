package plan

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"novelagent/internal/llm"
	"novelagent/internal/novel"
	"novelagent/internal/store"
)

func planTestStore(t *testing.T) (*store.Store, novel.Project, novel.Knowledge) {
	t.Helper()
	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	project, err := st.ImportChapters("长夜行", []novel.Chapter{
		{Title: "雨夜", Content: "林越在雨夜护住铜牌。"},
		{Title: "追兵", Content: "苏青追至渡口，林越左臂受伤。"},
	})
	if err != nil {
		t.Fatalf("ImportChapters: %v", err)
	}
	knowledge := novel.NewKnowledge()
	knowledge.Overview = "少年林越护送铜牌，与苏青亦敌亦友。"
	knowledge.Characters = []novel.Character{
		{ID: "c_lin", Name: "林越", Aliases: []string{"阿越"}, Role: "护送者", LastSeen: 2},
		{ID: "c_su", Name: "苏青", Role: "追兵", LastSeen: 2},
	}
	knowledge.World = []novel.WorldEntry{{ID: "w_token", Title: "铜牌", Content: "开启旧朝的钥匙"}}
	if _, _, err := st.CommitAnalysis(project.Revision, 2, knowledge); err != nil {
		t.Fatalf("CommitAnalysis: %v", err)
	}
	if _, _, err := st.FinishAnalysis(project.Revision, knowledge); err != nil {
		t.Fatalf("FinishAnalysis: %v", err)
	}
	return st, project, knowledge
}

func planMockProvider(t *testing.T, content string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"message": map[string]string{"role": "assistant", "content": content},
			}},
		})
	}))
}

func TestGenerateCommitsGlobalAndVolumes(t *testing.T) {
	st, _, _ := planTestStore(t)
	provider := planMockProvider(t, "```json\n"+
		`{"global":{"title":"铜牌北行","objective":"护送铜牌北上，揭开旧朝真相","constraints":["不提前揭露铜牌全部秘密"],"candidateBeats":["渡口遭遇苏青","旧朝遗民现身"],"characters":["林越","苏青"],"flexibility":"medium"},`+
		`"volumes":[{"title":"第一卷 渡口","objective":"林越摆脱追兵并被迫与苏青同行","constraints":["林越左臂伤势持续影响行动"],"candidateBeats":["雨夜渡口","铜牌异动"],"characters":["林越","苏青","不存在的角色"],"flexibility":"high"},`+
		`{"title":"第二卷 旧都","objective":"两人进入旧都寻找铜牌线索","constraints":[],"candidateBeats":["旧都暗桩"],"characters":["阿越"],"flexibility":"low"}]}`+
		"\n```")
	defer provider.Close()
	if _, err := st.SaveConfig(store.Config{BaseURL: provider.URL + "/v1", APIKey: "test", Model: "mock"}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	gen := NewGenerator(st, nil)
	result, err := gen.Generate(context.Background(), Request{
		EndingTone: "克制的希望", AvoidTropes: "失忆", VolumeCount: 2, Extra: "保持双主角张力",
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if result.MemoryRevision != 1 || result.CommitID == "" || len(result.Plans) != 3 {
		t.Fatalf("result = %+v", result)
	}
	global, vol1, vol2 := result.Plans[0], result.Plans[1], result.Plans[2]
	if global.Level != "global" || global.ParentPlanID != "" || global.Status != novel.PlanProposed {
		t.Errorf("global = %+v", global)
	}
	if vol1.Level != "volume" || vol1.ParentPlanID != global.ID || vol2.ParentPlanID != global.ID {
		t.Errorf("volumes = %+v / %+v", vol1, vol2)
	}
	if len(vol1.InvolvedCharacters) != 2 || vol1.InvolvedCharacters[0] != "c_lin" && vol1.InvolvedCharacters[0] != "c_su" {
		t.Errorf("vol1 characters = %+v, want c_lin/c_su only", vol1.InvolvedCharacters)
	}
	if len(vol2.InvolvedCharacters) != 1 || vol2.InvolvedCharacters[0] != "c_lin" {
		t.Errorf("vol2 characters = %+v, want alias 阿越 resolved to c_lin", vol2.InvolvedCharacters)
	}
	if global.Flexibility != "medium" || vol2.Flexibility != "low" {
		t.Errorf("flexibility = %q/%q", global.Flexibility, vol2.Flexibility)
	}
	if global.CreatedFromCommit != "" {
		t.Errorf("createdFromCommit = %q, want empty", global.CreatedFromCommit)
	}

	stored, revision, err := st.LoadActivePlans()
	if err != nil || revision != 1 || len(stored) != 0 {
		t.Errorf("active plans = %+v rev=%d err=%v, want none (proposed only)", stored, revision, err)
	}
	tree, err := st.LoadPlanTree()
	if err != nil || tree.Count != 3 || len(tree.Roots) != 1 || len(tree.Roots[0].Children) != 2 {
		t.Errorf("tree = %+v err=%v", tree, err)
	}
}

func TestGenerateRejectsMissingConfig(t *testing.T) {
	st, _, _ := planTestStore(t)
	_, err := NewGenerator(st, nil).Generate(context.Background(), Request{VolumeCount: 2})
	if !errors.Is(err, ErrConfigMissing) {
		t.Fatalf("error = %v, want ErrConfigMissing", err)
	}
	if info, _ := st.LoadMemoryInfo(); info.MemoryRevision != 0 {
		t.Errorf("revision = %d, want 0", info.MemoryRevision)
	}
}

func TestGenerateRejectsEmptyKnowledge(t *testing.T) {
	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	gen := NewGenerator(st, nil)
	gen.SetClientFactory(func() (*llm.Client, error) {
		return llm.NewClient("http://127.0.0.1:9/v1", "k", "m"), nil
	})
	_, err = gen.Generate(context.Background(), Request{VolumeCount: 2})
	if !errors.Is(err, ErrKnowledgeEmpty) {
		t.Fatalf("error = %v, want ErrKnowledgeEmpty", err)
	}
}

func TestGenerateRejectsInvalidOutput(t *testing.T) {
	st, _, _ := planTestStore(t)
	provider := planMockProvider(t, `{"global":{"title":"只有总纲"},"volumes":[]}`)
	defer provider.Close()
	if _, err := st.SaveConfig(store.Config{BaseURL: provider.URL + "/v1", Model: "mock"}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	_, err := NewGenerator(st, nil).Generate(context.Background(), Request{VolumeCount: 2})
	if !errors.Is(err, ErrOutputInvalid) {
		t.Fatalf("error = %v, want ErrOutputInvalid", err)
	}
	if info, _ := st.LoadMemoryInfo(); info.MemoryRevision != 0 {
		t.Errorf("revision = %d, want 0 (zero write)", info.MemoryRevision)
	}
}

func TestBuildPlansTruncatesAndFallsBack(t *testing.T) {
	knowledge := novel.NewKnowledge()
	knowledge.Characters = []novel.Character{{ID: "c_lin", Name: "林越"}}
	outline := generatedOutline{
		Global: generatedPlan{Title: "", Objective: "总目标"},
		Volumes: []generatedPlan{
			{Title: "", Objective: "卷一"},
			{Title: "第二卷", Objective: "卷二"},
			{Title: "第三卷", Objective: "卷三"},
		},
	}
	plans, err := buildPlans(outline, knowledge, "长夜行", 2)
	if err != nil {
		t.Fatalf("buildPlans: %v", err)
	}
	if len(plans) != 3 {
		t.Fatalf("plans = %d, want 3 (global + 2 volumes)", len(plans))
	}
	if plans[0].Title != "长夜行·全书规划" {
		t.Errorf("global title = %q", plans[0].Title)
	}
	if plans[1].Title != "第1卷" || plans[2].Title != "第二卷" {
		t.Errorf("volume titles = %q/%q", plans[1].Title, plans[2].Title)
	}
	if plans[1].ParentPlanID != plans[0].ID || plans[2].ParentPlanID != plans[0].ID {
		t.Errorf("parent links = %+v", plans)
	}

	if _, err := buildPlans(generatedOutline{Global: generatedPlan{Title: "x"}}, knowledge, "书", 2); !errors.Is(err, ErrOutputInvalid) {
		t.Errorf("empty volumes error = %v, want ErrOutputInvalid", err)
	}
}

func TestParsePlanJSONToleratesFences(t *testing.T) {
	var outline generatedOutline
	raw := "\ufeff```json\n{\"global\":{\"title\":\"总纲\"},\"volumes\":[{\"title\":\"卷一\"}]}\n```\n"
	if err := parsePlanJSON(raw, &outline); err != nil {
		t.Fatalf("parsePlanJSON: %v", err)
	}
	if outline.Global.Title != "总纲" || len(outline.Volumes) != 1 {
		t.Errorf("outline = %+v", outline)
	}
	if err := parsePlanJSON("not json", &outline); err == nil {
		t.Error("parsePlanJSON accepted non-JSON")
	}
}

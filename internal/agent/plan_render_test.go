package agent

import (
	"strings"
	"testing"

	"novelagent/internal/novel"
)

// 计划对齐注入（F23）：只影响走向建议，不改变无计划时的基线输出。

func planFixture() []novel.StoryPlan {
	return []novel.StoryPlan{
		{
			ID: "plan_g", Level: string(novel.PlanLevelGlobal), Status: novel.PlanActive,
			Title: "长夜全书", Objective: "林渊查明铜牌来历并阻止边城再陷",
			Constraints:    []string{"不提前揭晓幕后黑手"},
			CandidateBeats: []string{"南下追查", "渡口重逢"},
		},
		{
			ID: "plan_v", Level: string(novel.PlanLevelVolume), Status: novel.PlanActive, ParentPlanID: "plan_g",
			Title: "第一卷 南渡", Objective: "抵达江南并找到旧朝线索",
		},
		{
			ID: "plan_p", Level: string(novel.PlanLevelVolume), Status: novel.PlanProposed, ParentPlanID: "plan_g",
			Title: "候选卷", Objective: "未确认计划不得注入",
		},
		{
			ID: "plan_r", Level: string(novel.PlanLevelVolume), Status: novel.PlanActive, ParentPlanID: "plan_g",
			Title: "待复核卷", Objective: "待复核计划不得注入",
			MemoryMeta: novel.MemoryMeta{NeedsReview: true, ReviewReason: "evidence_stale"},
		},
	}
}

func TestBuildInjectsPlanSectionAndRule(t *testing.T) {
	input := goldenBuildInput()
	input.Plans = planFixture()
	prompt := Build(input)

	if prompt.PlanSection == "" {
		t.Fatal("PlanSection is empty")
	}
	for _, want := range []string{
		novel.PlanSectionHeader,
		"全书《长夜全书》｜目标：林渊查明铜牌来历并阻止边城再陷",
		"约束：不提前揭晓幕后黑手",
		"候选发展：南下追查；渡口重逢",
		"分卷《第一卷 南渡》｜目标：抵达江南并找到旧朝线索",
	} {
		if !strings.Contains(prompt.User, want) {
			t.Errorf("user prompt missing plan %q\n%s", want, prompt.User)
		}
	}
	for _, banned := range []string{"候选卷", "待复核卷"} {
		if strings.Contains(prompt.User, banned) {
			t.Errorf("inactive/review plan %q leaked into prompt", banned)
		}
	}
	if !strings.Contains(prompt.System, "3.2 【计划对齐】") {
		t.Error("system prompt plan rule is missing")
	}
	if !strings.Contains(prompt.System, "以事实为准") {
		t.Error("plan rule must assert fact precedence")
	}
	if prompt.PlanSection != novel.RenderPlanSection(input.Plans) {
		t.Error("PlanSection does not match renderer output")
	}

	planAt := strings.Index(prompt.User, novel.PlanSectionHeader)
	recentAt := strings.Index(prompt.User, "【最近正文（末尾即续写起点）】")
	if !(planAt >= 0 && planAt < recentAt) {
		t.Errorf("plan section must precede recent manuscript: plan=%d recent=%d", planAt, recentAt)
	}
	if worldAt := strings.Index(prompt.User, "【相关设定】"); worldAt >= 0 && worldAt > planAt {
		t.Errorf("plan section should follow world entries: world=%d plan=%d", worldAt, planAt)
	}
}

func TestBuildWithoutPlansKeepsBaseline(t *testing.T) {
	base := Build(goldenBuildInput())
	withEmpty := goldenBuildInput()
	withEmpty.Plans = []novel.StoryPlan{}
	empty := Build(withEmpty)

	if base.System != empty.System || base.User != empty.User {
		t.Error("empty plan slice must not change prompt")
	}
	if base.PlanSection != "" || strings.Contains(base.System, "3.2") {
		t.Error("baseline prompt must not contain plan alignment")
	}
	if !strings.Contains(base.System, "3. 以【最近正文】为最高事实来源") {
		t.Error("baseline system prompt numbering changed")
	}
}

func TestBuildPlanAndMemoryRulesCompose(t *testing.T) {
	input := goldenBuildInput()
	input.Memory = memoryFixture()
	input.Plans = planFixture()
	prompt := Build(input)
	if !strings.Contains(prompt.System, "3.1 【已确认剧情记忆】") || !strings.Contains(prompt.System, "3.2 【计划对齐】") {
		t.Error("both memory and plan rules must be present")
	}
	memoryAt := strings.Index(prompt.User, "【已确认剧情记忆")
	planAt := strings.Index(prompt.User, novel.PlanSectionHeader)
	if !(memoryAt >= 0 && planAt > memoryAt) {
		t.Errorf("plan section should follow memory section: memory=%d plan=%d", memoryAt, planAt)
	}
}

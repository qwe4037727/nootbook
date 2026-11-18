package plan

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"novelagent/internal/llm"
	"novelagent/internal/novel"
	"novelagent/internal/store"
)

// 计划生成器（F20）：读取知识库/正文/记忆 → LLM 产出 global+volume 策划 →
// 经 store.AddProposedPlans 整批提交为 proposed 计划（memoryRevision+1）。

const (
	generateTimeout   = 8 * time.Minute
	defaultVolumes    = 3
	maxVolumes        = 12
	planTemperature   = 0.6
	maxPlanCharacters = 8
)

type Request struct {
	EndingTone  string `json:"endingTone"`
	AvoidTropes string `json:"avoidTropes"`
	VolumeCount int    `json:"volumeCount"`
	Extra       string `json:"extra"`
}

type Result struct {
	Plans          []novel.StoryPlan `json:"plans"`
	MemoryRevision int64             `json:"memoryRevision"`
	CommitID       string            `json:"commitId"`
}

type Generator struct {
	st       *store.Store
	log      *slog.Logger
	clientFn func() (*llm.Client, error) // 测试注入；为空则按模型配置构造。
}

func NewGenerator(st *store.Store, logger *slog.Logger) *Generator {
	if logger == nil {
		logger = slog.Default()
	}
	return &Generator{st: st, log: logger}
}

// SetClientFactory 供测试注入客户端工厂（导出以便 server 测试之外的包使用）。
func (g *Generator) SetClientFactory(fn func() (*llm.Client, error)) { g.clientFn = fn }

func (g *Generator) Generate(ctx context.Context, req Request) (Result, error) {
	if req.VolumeCount <= 0 {
		req.VolumeCount = defaultVolumes
	}
	if req.VolumeCount > maxVolumes {
		req.VolumeCount = maxVolumes
	}

	client, err := g.client()
	if err != nil {
		return Result{}, err
	}
	knowledge, err := g.st.LoadKnowledge()
	if err != nil {
		return Result{}, err
	}
	if strings.TrimSpace(knowledge.Overview) == "" && len(knowledge.Characters) == 0 &&
		len(knowledge.World) == 0 && len(knowledge.Summaries) == 0 {
		return Result{}, fmt.Errorf("%w: run /api/analyze first", ErrKnowledgeEmpty)
	}
	project, err := g.st.LoadProject()
	if err != nil {
		return Result{}, err
	}
	chapters, err := g.st.LoadChapters()
	if err != nil {
		return Result{}, err
	}

	var threads []novel.StoryThread
	var existing []novel.StoryPlan
	if _, _, _, loadedThreads, loadedPlans, err := g.st.LoadMemoryRecords(); err != nil {
		g.log.Warn("load memory for plan generation", "err", err)
	} else {
		threads = loadedThreads
		existing = loadedPlans
	}

	messages := buildPlanMessages(project.Title, knowledge, chapters, threads, existing, req)
	callCtx, cancel := context.WithTimeout(ctx, generateTimeout)
	defer cancel()
	reply, err := client.CompleteChat(callCtx, messages, planTemperature)
	if err != nil {
		return Result{}, fmt.Errorf("plan: model call failed: %w", err)
	}

	var outline generatedOutline
	if err := parsePlanJSON(reply.Content, &outline); err != nil {
		g.log.Error("decode plan output", "err", err, "truncated", llm.IsTruncated(reply.FinishReason), "head", clipText(reply.Content, 300))
		if llm.IsTruncated(reply.FinishReason) {
			return Result{}, fmt.Errorf("%w: 模型输出被截断，请减少卷数或稍后重试", ErrOutputInvalid)
		}
		return Result{}, fmt.Errorf("%w: %v", ErrOutputInvalid, err)
	}
	plans, err := buildPlans(outline, knowledge, project.Title, req.VolumeCount)
	if err != nil {
		return Result{}, err
	}
	applied, err := g.st.AddProposedPlans(plans)
	if err != nil {
		return Result{}, err
	}
	return Result{Plans: applied.Plans, MemoryRevision: applied.MemoryRevision, CommitID: applied.CommitID}, nil
}

func (g *Generator) client() (*llm.Client, error) {
	if g.clientFn != nil {
		client, err := g.clientFn()
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrConfigMissing, err)
		}
		return client, nil
	}
	cfg, err := g.st.LoadConfig()
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(cfg.BaseURL) == "" || strings.TrimSpace(cfg.Model) == "" {
		return nil, ErrConfigMissing
	}
	key, _ := cfg.EffectiveAPIKey()
	return llm.NewClient(cfg.BaseURL, key, cfg.Model), nil
}

type generatedPlan struct {
	Title          string   `json:"title"`
	Objective      string   `json:"objective"`
	Constraints    []string `json:"constraints"`
	CandidateBeats []string `json:"candidateBeats"`
	Characters     []string `json:"characters"`
	Flexibility    string   `json:"flexibility"`
}

type generatedOutline struct {
	Global  generatedPlan   `json:"global"`
	Volumes []generatedPlan `json:"volumes"`
}

func buildPlans(outline generatedOutline, knowledge novel.Knowledge, projectTitle string, volumeCount int) ([]novel.StoryPlan, error) {
	if len(outline.Volumes) == 0 {
		return nil, fmt.Errorf("%w: model returned no volumes", ErrOutputInvalid)
	}
	if volumeCount <= 0 {
		volumeCount = defaultVolumes
	}
	if volumeCount > maxVolumes {
		volumeCount = maxVolumes
	}
	volumes := outline.Volumes
	if len(volumes) > volumeCount {
		volumes = volumes[:volumeCount]
	}

	globalTitle := strings.TrimSpace(outline.Global.Title)
	if globalTitle == "" {
		globalTitle = displayTitle(projectTitle) + "·全书规划"
	}
	globalID := novel.NewID("plan")
	plans := []novel.StoryPlan{makeStoryPlan(globalID, "global", "", globalTitle, outline.Global, knowledge)}
	for i, volume := range volumes {
		title := strings.TrimSpace(volume.Title)
		if title == "" {
			title = fmt.Sprintf("第%d卷", i+1)
		}
		plans = append(plans, makeStoryPlan(novel.NewID("plan"), "volume", globalID, title, volume, knowledge))
	}
	return plans, nil
}

func makeStoryPlan(id, level, parent, title string, generated generatedPlan, knowledge novel.Knowledge) novel.StoryPlan {
	objective := strings.TrimSpace(generated.Objective)
	flexibility := strings.TrimSpace(generated.Flexibility)
	switch flexibility {
	case "high", "medium", "low":
	default:
		flexibility = "medium"
	}
	text := strings.Join([]string{
		title,
		objective,
		strings.Join(generated.Constraints, " "),
		strings.Join(generated.CandidateBeats, " "),
	}, " ")
	return novel.StoryPlan{
		ID:                 id,
		Status:             novel.PlanProposed,
		Level:              level,
		ParentPlanID:       parent,
		Title:              title,
		Objective:          objective,
		Constraints:        cleanPlanList(generated.Constraints, 8),
		CandidateBeats:     cleanPlanList(generated.CandidateBeats, 10),
		InvolvedCharacters: resolvePlanCharacters(knowledge, generated.Characters, text),
		Flexibility:        flexibility,
	}
}

func cleanPlanList(items []string, limit int) []string {
	out := make([]string, 0, min(len(items), limit))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, dup := seen[item]; dup {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
		if len(out) >= limit {
			break
		}
	}
	return out
}

// resolvePlanCharacters 先解析模型显式给出的角色名，再扫描计划文本中的已知角色名/别名。
func resolvePlanCharacters(knowledge novel.Knowledge, explicit []string, text string) []string {
	if len(knowledge.Characters) == 0 {
		return nil
	}
	seen := make(map[string]struct{})
	out := make([]string, 0, maxPlanCharacters)
	add := func(index int) {
		if index < 0 || len(out) >= maxPlanCharacters {
			return
		}
		id := knowledge.Characters[index].ID
		if id == "" {
			return
		}
		if _, dup := seen[id]; dup {
			return
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	for _, name := range explicit {
		index, ambiguous := novel.MatchCharacter(knowledge.Characters, strings.TrimSpace(name), nil)
		if !ambiguous {
			add(index)
		}
	}
	if len(out) >= maxPlanCharacters {
		return out
	}
	for i := range knowledge.Characters {
		character := &knowledge.Characters[i]
		for _, candidate := range append([]string{character.Name}, character.Aliases...) {
			candidate = strings.TrimSpace(candidate)
			if len([]rune(candidate)) < 2 {
				continue
			}
			if strings.Contains(text, candidate) {
				add(i)
				break
			}
		}
		if len(out) >= maxPlanCharacters {
			break
		}
	}
	return out
}

func parsePlanJSON(text string, dst any) error {
	trimmed := strings.TrimSpace(strings.TrimPrefix(text, "\ufeff"))
	if strings.HasPrefix(trimmed, "```") {
		if newline := strings.IndexByte(trimmed, '\n'); newline >= 0 {
			trimmed = trimmed[newline+1:]
		}
		if fence := strings.LastIndex(trimmed, "```"); fence >= 0 {
			trimmed = trimmed[:fence]
		}
	}
	start := strings.IndexByte(trimmed, '{')
	if start < 0 {
		return errors.New("response contains no JSON object")
	}
	decoder := json.NewDecoder(strings.NewReader(trimmed[start:]))
	var raw json.RawMessage
	if err := decoder.Decode(&raw); err != nil {
		return err
	}
	return json.Unmarshal(raw, dst)
}

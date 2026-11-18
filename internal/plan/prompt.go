package plan

import (
	"errors"
	"fmt"
	"strings"

	"novelagent/internal/llm"
	"novelagent/internal/novel"
)

// 计划生成提示词（F20）：模型只产出策划草案，状态/ID/归属由服务端决定。

var (
	ErrConfigMissing  = errors.New("plan: model config missing")
	ErrKnowledgeEmpty = errors.New("plan: knowledge is empty")
	ErrOutputInvalid  = errors.New("plan: invalid model output")
)

const planSystemPrompt = `你是资深中文小说策划编辑，负责为连载小说制定可执行的整体规划。
只输出一个 JSON 对象，不要输出 Markdown、解释或任何额外文字。

硬性规则：
1. global 是全书总纲；volumes 是分卷规划，按时间顺序推进，每卷有独立目标、冲突升级与收束点。
2. 计划面向未来，不得与给定已确认设定、人物状态、最近剧情矛盾；不得提前解决核心矛盾或草率收束（结局基调明确要求时除外）。
3. candidateBeats 是候选情节（可调整），constraints 写清不能违反的边界；不要把候选情节点写成已发生事实。
4. characters 只能填人物表中已有的名字，不得虚构新人物；没有适配人物时留空数组。
5. 分卷数量必须与要求一致；不要复述正文或输出摘要。

输出 schema：
{
  "global": {
    "title": "总纲标题（≤40字）",
    "objective": "全书核心目标与主线（≤200字）",
    "constraints": ["不可违反的边界（≤5条）"],
    "candidateBeats": ["候选关键节点（≤6条）"],
    "characters": ["人物表中的角色名"],
    "flexibility": "high|medium|low"
  },
  "volumes": [
    {
      "title": "第X卷标题（≤40字）",
      "objective": "本卷目标与主要冲突（≤200字）",
      "constraints": ["本卷边界（≤5条）"],
      "candidateBeats": ["候选情节（≤6条）"],
      "characters": ["人物表中的角色名"],
      "flexibility": "high|medium|low"
    }
  ]
}`

func buildPlanMessages(title string, knowledge novel.Knowledge, chapters []novel.Chapter, threads []novel.StoryThread, existing []novel.StoryPlan, req Request) []llm.Message {
	var b strings.Builder
	fmt.Fprintf(&b, "【书名】《%s》\n", displayTitle(title))

	if overview := strings.TrimSpace(knowledge.Overview); overview != "" {
		b.WriteString("\n【核心设定】\n")
		b.WriteString(clipText(overview, 1500))
		b.WriteString("\n")
	}
	if style := knowledge.Style; style.Tone != "" || style.Perspective != "" || style.Pacing != "" {
		b.WriteString("\n【文风基调】\n")
		var parts []string
		if style.Perspective != "" {
			parts = append(parts, "视角："+style.Perspective)
		}
		if style.Tone != "" {
			parts = append(parts, "语气："+style.Tone)
		}
		if style.Pacing != "" {
			parts = append(parts, "节奏："+style.Pacing)
		}
		b.WriteString(strings.Join(parts, "；") + "\n")
	}
	if len(knowledge.Characters) > 0 {
		b.WriteString("\n【主要人物】\n")
		for i, character := range knowledge.Characters {
			if i >= 15 {
				break
			}
			fmt.Fprintf(&b, "- %s", clipText(character.Name, 30))
			if len(character.Aliases) > 0 {
				fmt.Fprintf(&b, "（别名：%s）", clipText(strings.Join(character.Aliases, "、"), 40))
			}
			var fields []string
			if character.Role != "" {
				fields = append(fields, "身份："+clipText(character.Role, 40))
			}
			if character.State != "" {
				fields = append(fields, "当前："+clipText(character.State, 50))
			}
			if character.Relationships != "" {
				fields = append(fields, "关系："+clipText(character.Relationships, 50))
			}
			if len(fields) > 0 {
				b.WriteString("｜" + strings.Join(fields, "｜"))
			}
			b.WriteString("\n")
		}
	}
	if len(knowledge.World) > 0 {
		b.WriteString("\n【世界设定】\n")
		for i, entry := range knowledge.World {
			if i >= 10 {
				break
			}
			fmt.Fprintf(&b, "- %s", clipText(entry.Title, 30))
			if entry.Content != "" {
				fmt.Fprintf(&b, "：%s", clipText(entry.Content, 100))
			}
			b.WriteString("\n")
		}
	}
	summaries := knowledge.Summaries
	if len(summaries) > 0 {
		b.WriteString("\n【最近剧情】\n")
		start := len(summaries) - 5
		if start < 0 {
			start = 0
		}
		for _, summary := range summaries[start:] {
			fmt.Fprintf(&b, "- 第%d章 %s：%s\n", summary.Index, clipText(summary.Title, 30), clipText(summary.Summary, 160))
		}
	} else if tail := recentChapterTail(chapters, 2000); tail != "" {
		b.WriteString("\n【最近正文节选】\n")
		b.WriteString(tail)
		b.WriteString("\n")
	}
	if len(threads) > 0 {
		b.WriteString("\n【未解决伏笔】\n")
		count := 0
		for _, thread := range threads {
			if thread.Status == novel.ThreadResolved || thread.Status == novel.ThreadAbandoned {
				continue
			}
			fmt.Fprintf(&b, "- %s", clipText(thread.Title, 40))
			if thread.Question != "" {
				fmt.Fprintf(&b, "：%s", clipText(thread.Question, 80))
			}
			b.WriteString("\n")
			count++
			if count >= 8 {
				break
			}
		}
	}
	if len(existing) > 0 {
		b.WriteString("\n【已有计划（避免重复，可在其基础上推进）】\n")
		count := 0
		for _, p := range existing {
			if p.Status == novel.PlanSuperseded || p.Status == novel.PlanCancelled {
				continue
			}
			fmt.Fprintf(&b, "- [%s][%s] %s：%s\n", p.Level, p.Status, clipText(p.Title, 40), clipText(p.Objective, 100))
			count++
			if count >= 6 {
				break
			}
		}
	}

	b.WriteString("\n【本次规划要求】\n")
	volumeCount := req.VolumeCount
	if volumeCount <= 0 {
		volumeCount = 3
	}
	fmt.Fprintf(&b, "分卷数：%d\n", volumeCount)
	fmt.Fprintf(&b, "结局基调：%s\n", firstNonEmpty(strings.TrimSpace(req.EndingTone), "不指定，保持张力与合理收束"))
	fmt.Fprintf(&b, "需要回避的套路：%s\n", firstNonEmpty(strings.TrimSpace(req.AvoidTropes), "无"))
	if extra := strings.TrimSpace(req.Extra); extra != "" {
		fmt.Fprintf(&b, "补充要求：%s\n", clipText(extra, 300))
	}
	b.WriteString("\n请基于以上信息输出 JSON 规划。")

	return []llm.Message{
		{Role: "system", Content: planSystemPrompt},
		{Role: "user", Content: b.String()},
	}
}

func recentChapterTail(chapters []novel.Chapter, limit int) string {
	var b strings.Builder
	for _, chapter := range chapters {
		content := strings.TrimSpace(chapter.Content)
		if content == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(content)
	}
	return clipText(b.String(), limit)
}

func displayTitle(title string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return "未命名小说"
	}
	return title
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func clipText(text string, limit int) string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) <= limit {
		return string(runes)
	}
	return string(runes[:limit]) + "…"
}

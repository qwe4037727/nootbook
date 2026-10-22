package analysis

import (
	"fmt"
	"strings"

	"novelagent/internal/llm"
	"novelagent/internal/novel"
)

const analystSystem = `你是小说知识库分析器。只输出一个 JSON 对象，不要输出 Markdown、解释或任何额外文字。

任务：阅读给定章节，抽取人物、世界观设定、文风特征和情节摘要。

硬性规则：
1. 只记录本章明确出现或可直接推断的信息，不预测后续情节，不补写结局，不生成大纲。
2. 不确定的信息不要编造；可以省略字段，或用较低的 confidence 标注。
3. 人物只收录有叙事分量的角色，跳过无名的路人。name 使用最稳定的正式称呼；aliases 收录本章出现的其他称谓。
4. state 写本章结束时的处境、伤势、位置、目标或情绪，不要写人物小传。
5. 世界观只收录地点、势力、规则、物品、历史事件等设定性信息，不收录临时的普通物件。
6. style 分析本章的行文本身；samples 必须是原文片段，不得改写。
7. summary 概括本章发生了什么以及停在哪里，不超过 200 字；endsAt 描述结尾场景或悬念，不超过 60 字。
8. 输出必须是合法 JSON，字段名与示例一致。

输出格式：
{
  "summary": "本章摘要",
  "endsAt": "结尾停点",
  "characters": [
    {
      "name": "姓名",
      "aliases": ["别名"],
      "role": "身份或立场",
      "personality": "性格特征",
      "appearance": "外貌特征",
      "relationships": "人物关系",
      "state": "当前状态",
      "notes": "其他重要信息",
      "confidence": 0.9,
      "evidence": ["原文短句"]
    }
  ],
  "world": [
    {
      "title": "设定名称",
      "aliases": ["别称"],
      "category": "地点|势力|规则|物品|事件|其他",
      "content": "一句话说明",
      "confidence": 0.8,
      "evidence": ["原文短句"]
    }
  ],
  "style": {
    "perspective": "叙述视角与人称",
    "tone": "语气与氛围",
    "sentence": "句式与节奏",
    "dialogue": "对白风格",
    "pacing": "段落与场景推进",
    "samples": ["不超过 80 字的原文片段"]
  }
}`

func buildAnalystMessages(title string, chapter novel.Chapter, knowledge novel.Knowledge) []llm.Message {
	var user strings.Builder
	fmt.Fprintf(&user, "【作品】《%s》\n", title)
	fmt.Fprintf(&user, "【本章】第 %d 章 %s\n", chapter.Index, chapter.Title)
	if digest := knowledgeDigest(knowledge, 6000); digest != "" {
		user.WriteString("\n【已有知识摘要（用于统一称谓，不要照抄）】\n")
		user.WriteString(digest)
	}
	user.WriteString("\n【本章正文】\n")
	user.WriteString(clipWithNote(chapter.Content, 30000, "（本章过长，以上仅为前半部分）"))
	user.WriteString("\n\n请输出本章的分析 JSON。")
	return []llm.Message{
		{Role: "system", Content: analystSystem},
		{Role: "user", Content: user.String()},
	}
}

const synthesisSystem = `你是小说知识库编辑。只输出一个 JSON 对象，不要输出 Markdown、解释或任何额外文字。

任务：根据已有总览、章节摘要和文风记录，维护一份供续写使用的核心设定总览与文风指纹。

硬性规则：
1. overview 控制在 300 到 500 字，覆盖核心世界规则、主要地点或势力、核心人物及当前关系、主线冲突与当前进展。
2. 只依据提供的摘要，不预测后续，不编造未出现的信息，不写结局。
3. style 各字段保持简洁，不超过 40 字；samples 从提供的原文片段中选择或保留 2 到 3 条，不得改写。
4. 输出必须是合法 JSON。

输出格式：
{
  "overview": "核心设定与当前进展",
  "style": {
    "perspective": "叙述视角与人称",
    "tone": "语气与氛围",
    "sentence": "句式与节奏",
    "dialogue": "对白风格",
    "pacing": "段落与场景推进",
    "samples": ["原文片段"]
  }
}`

func buildSynthesisMessages(title string, knowledge novel.Knowledge) []llm.Message {
	var user strings.Builder
	fmt.Fprintf(&user, "【作品】《%s》\n", title)
	if overview := strings.TrimSpace(knowledge.Overview); overview != "" {
		user.WriteString("\n【已有总览】\n")
		user.WriteString(clip(overview, 700))
		user.WriteString("\n")
	}

	summaries := balancedSummaries(knowledge.Summaries, 30)
	if len(summaries) > 0 {
		user.WriteString("\n【章节摘要】\n")
		lastIndex := 0
		for _, summary := range summaries {
			if lastIndex > 0 && summary.Index > lastIndex+1 {
				user.WriteString("……（中间章节略）\n")
			}
			label := fmt.Sprintf("第%d章", summary.Index)
			if summary.Title != "" {
				label += " " + summary.Title
			}
			fmt.Fprintf(&user, "%s：%s\n", label, clip(summary.Summary, 160))
			lastIndex = summary.Index
		}
	}

	user.WriteString("\n【现有文风记录】\n")
	user.WriteString(styleDigest(knowledge.Style))
	user.WriteString("\n")

	if len(knowledge.Style.Samples) > 0 {
		user.WriteString("\n【原文片段】\n")
		for i, sample := range knowledge.Style.Samples {
			if i >= 4 {
				break
			}
			fmt.Fprintf(&user, "%d. %s\n", i+1, clip(sample, 120))
		}
	}
	user.WriteString("\n请输出更新后的总览与文风 JSON。")
	return []llm.Message{
		{Role: "system", Content: synthesisSystem},
		{Role: "user", Content: user.String()},
	}
}

func knowledgeDigest(knowledge novel.Knowledge, maxChars int) string {
	var b strings.Builder
	if overview := strings.TrimSpace(knowledge.Overview); overview != "" {
		b.WriteString("总览：")
		b.WriteString(clip(overview, 500))
		b.WriteString("\n")
	}
	if len(knowledge.Characters) > 0 {
		b.WriteString("人物：\n")
		for i, character := range knowledge.Characters {
			if i >= 30 {
				break
			}
			line := character.Name
			if len(character.Aliases) > 0 {
				line += "（" + strings.Join(character.Aliases, "、") + "）"
			}
			if character.Role != "" {
				line += "｜" + character.Role
			}
			if character.State != "" {
				line += "｜当前：" + character.State
			}
			b.WriteString("- " + clip(line, 150) + "\n")
			if len([]rune(b.String())) >= maxChars {
				return b.String()
			}
		}
	}
	if len(knowledge.World) > 0 {
		b.WriteString("设定：\n")
		for i, entry := range knowledge.World {
			if i >= 30 {
				break
			}
			line := entry.Title
			if entry.Category != "" {
				line += "（" + entry.Category + "）"
			}
			if entry.Content != "" {
				line += "：" + entry.Content
			}
			b.WriteString("- " + clip(line, 150) + "\n")
			if len([]rune(b.String())) >= maxChars {
				return b.String()
			}
		}
	}
	if len(knowledge.Summaries) > 0 {
		b.WriteString("最近情节：\n")
		start := max(0, len(knowledge.Summaries)-3)
		for _, summary := range knowledge.Summaries[start:] {
			fmt.Fprintf(&b, "- 第%d章：%s\n", summary.Index, clip(summary.Summary, 120))
		}
	}
	return b.String()
}

func styleDigest(style novel.StyleProfile) string {
	var parts []string
	if style.Perspective != "" {
		parts = append(parts, "视角："+style.Perspective)
	}
	if style.Tone != "" {
		parts = append(parts, "语气："+style.Tone)
	}
	if style.Sentence != "" {
		parts = append(parts, "句式："+style.Sentence)
	}
	if style.Dialogue != "" {
		parts = append(parts, "对白："+style.Dialogue)
	}
	if style.Pacing != "" {
		parts = append(parts, "节奏："+style.Pacing)
	}
	if style.Notes != "" {
		parts = append(parts, "补充："+style.Notes)
	}
	if len(parts) == 0 {
		return "（尚未归纳）"
	}
	return strings.Join(parts, "；")
}

func balancedSummaries(summaries []novel.ChapterSummary, limit int) []novel.ChapterSummary {
	if len(summaries) <= limit {
		return summaries
	}
	head := 3
	tail := limit - head
	result := make([]novel.ChapterSummary, 0, limit)
	result = append(result, summaries[:head]...)
	result = append(result, summaries[len(summaries)-tail:]...)
	return result
}

func clip(text string, limit int) string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) <= limit {
		return string(runes)
	}
	return string(runes[:limit]) + "…"
}

func clipWithNote(text string, limit int, note string) string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) <= limit {
		return string(runes)
	}
	return string(runes[:limit]) + "\n" + note
}

package analysis

import (
	"fmt"
	"strings"

	"novelagent/internal/llm"
	"novelagent/internal/novel"
)

// 深度提取提示词（iter2-详设 §3.4）：模型只输出机械 schema，状态/确认由系统决定。
// 与既有 buildAnalystMessages 共用 knowledgeDigest 知识摘要逻辑（≤6000 字），章节正文截断 30000 rune。

const extractSystem = `你是小说深度记忆提取器。只输出一个 JSON 对象，不要输出 Markdown、解释或任何额外文字。

任务：阅读给定章节，抽取"本章明确发生"的情节事件、角色处境状态、角色认知（相信/误解/遗忘）、未解决伏笔线索，以及可能的新实体提示。

硬性规则：
1. 只提取本章正文明确发生的事实；推测、隐含信息不得作为事件/状态提交。
2. quote 必须是正文原文连续片段，不得改写；无法给出原文的条目不要提交。
3. 只允许输出下列 schema；status / confirmed 等字段由系统决定，你不得输出。
4. 伏笔 resolve 必须同时给出解决性原文证据（quote）；否则视为未解决（action=update）。
5. 不知道/不确定 → 不提交该条，而非猜测填值。
6. 事件的 participants 是你识别到的人名/称谓列表；若本章明确无参与者，participantNamesProvided=false 且 participants 为空。
7. cognitions 只记录角色主观相信/误解/遗忘的内容（可与客观事实不符）；subject 必须是角色名，不得填地点/组织/物品。

输出 schema：
{
  "events": [
    {
      "summary": "一句话事实描述",
      "eventType": "character_action|discovery|conflict|revelation|other",
      "participants": ["人名或称谓"],
      "participantNamesProvided": true,
      "quote": "原文连续片段（无则空字符串）",
      "preconditions": ["前置事件简述（可选）"],
      "consequences": ["后果简述（可选）"]
    }
  ],
  "states": [
    {
      "subject": "角色名或称谓",
      "category": "physical|location|possession|relation|goal|world",
      "attribute": "状态维度（如 伤势/位置/情绪）",
      "value": "具体值",
      "quote": "原文连续片段（无则空字符串）"
    }
  ],
  "cognitions": [
    {
      "subject": "角色名或称谓",
      "content": "该角色相信/误解/遗忘的具体内容",
      "status": "known|wrong|uncertain|forgotten",
      "quote": "原文连续片段（无则空字符串）"
    }
  ],
  "threads": [
    {
      "title": "伏笔/悬念标题",
      "action": "new|update|resolve",
      "clue": "本章新增或变化的线索（resolve 时为空）",
      "quote": "原文连续片段（resolve 必须给，否则空）"
    }
  ],
  "entityHints": [
    { "name": "疑似新实体名", "aliases": ["别名"] }
  ]
}`

// buildExtractMessages 组装单章深度提取提示词。
// relatedDigest 为"相关正式记忆快照"提示词上下文（调用方从 LoadMemoryRecords 裁剪生成）。
func buildExtractMessages(title string, chapter novel.Chapter, knowledge novel.Knowledge, relatedDigest string) []llm.Message {
	var user strings.Builder
	fmt.Fprintf(&user, "【作品】《%s》\n", title)
	fmt.Fprintf(&user, "【本章】第 %d 章 %s\n", chapter.Index, chapter.Title)
	if digest := knowledgeDigest(knowledge, 6000); digest != "" {
		user.WriteString("\n【已有知识摘要（统一称谓，不要照抄）】\n")
		user.WriteString(digest)
	}
	if relatedDigest != "" {
		user.WriteString("\n【本章相关已有记忆（用于去重与伏笔衔接，不要照抄）】\n")
		user.WriteString(relatedDigest)
	}
	user.WriteString("\n【本章正文】\n")
	user.WriteString(clipWithNote(chapter.Content, 30000, "（本章过长，以上仅为前半部分）"))
	user.WriteString("\n\n请输出本章的深度提取 JSON。")
	return []llm.Message{
		{Role: "system", Content: extractSystem},
		{Role: "user", Content: user.String()},
	}
}

// extractResult 为模型输出 schema 的 Go 映射（§3.4）。
type extractEvent struct {
	Summary                 string   `json:"summary"`
	EventType               string   `json:"eventType"`
	Participants            []string `json:"participants"`
	ParticipantNamesProvided bool    `json:"participantNamesProvided"`
	Quote                   string   `json:"quote"`
	Preconditions           []string `json:"preconditions"`
	Consequences            []string `json:"consequences"`
}

type extractState struct {
	Subject  string `json:"subject"`
	Category string `json:"category"`
	Attribute string `json:"attribute"`
	Value    string `json:"value"`
	Quote    string `json:"quote"`
}

type extractCognition struct {
	Subject string `json:"subject"`
	Content string `json:"content"`
	Status  string `json:"status"`
	Quote   string `json:"quote"`
}

type extractThread struct {
	Title  string `json:"title"`
	Action string `json:"action"`
	Clue   string `json:"clue"`
	Quote  string `json:"quote"`
}

type extractEntityHint struct {
	Name    string   `json:"name"`
	Aliases []string `json:"aliases"`
}

type extractResult struct {
	Events      []extractEvent      `json:"events"`
	States      []extractState      `json:"states"`
	Cognitions  []extractCognition  `json:"cognitions"`
	Threads     []extractThread     `json:"threads"`
	EntityHints []extractEntityHint `json:"entityHints"`
}

// relatedMemoryDigest 生成"相关正式记忆快照"提示词片段：本章 events、active states、
// open/developing threads（拼接上限 4000 字）。
func relatedMemoryDigest(events []novel.StoryEvent, states []novel.StoryState, threads []novel.StoryThread, chapterID string) string {
	var b strings.Builder
	b.WriteString("--- 本章已确认事件 ---\n")
	count := 0
	for _, e := range events {
		if e.ChapterID != chapterID {
			continue
		}
		fmt.Fprintf(&b, "- %s\n", clip(e.Summary, 80))
		count++
		if len([]rune(b.String())) >= 2000 {
			break
		}
	}
	if count == 0 {
		b.WriteString("（无）\n")
	}
	b.WriteString("--- 当前活跃状态 ---\n")
	sc := 0
	for _, st := range states {
		if st.Status != novel.StateActive {
			continue
		}
		fmt.Fprintf(&b, "- %s: %s=%s\n", clip(st.SubjectID, 20), clip(st.Attribute, 20), clip(st.Value, 60))
		sc++
		if len([]rune(b.String())) >= 3200 {
			break
		}
	}
	if sc == 0 {
		b.WriteString("（无）\n")
	}
	b.WriteString("--- 未解决伏笔 ---\n")
	tc := 0
	for _, th := range threads {
		if th.Status == novel.ThreadResolved || th.Status == novel.ThreadAbandoned {
			continue
		}
		fmt.Fprintf(&b, "- %s: %s\n", clip(th.Title, 40), clip(th.Question, 80))
		tc++
		if len([]rune(b.String())) >= 4000 {
			break
		}
	}
	if tc == 0 {
		b.WriteString("（无）\n")
	}
	// 整体上限保护。
	if len([]rune(b.String())) > 4000 {
		runes := []rune(b.String())
		return string(runes[:4000])
	}
	return b.String()
}

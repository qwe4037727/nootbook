package imagegen

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"novelagent/internal/novel"
	"novelagent/internal/store"
)

const universalNegative = "低质量, 模糊, 变形, 多余手指, 肢体错乱, 文字, 水印, 签名, 边框"

// PromptSpec 是提交给后端并持久化到 image_asset.prompt_json 的提示词快照。
type PromptSpec struct {
	Positive     string   `json:"positive"`
	Negative     string   `json:"negative"`
	Features     []string `json:"features"`
	CoreFeatures []string `json:"coreFeatures"`
	StyleTag     string   `json:"styleTag"`
	Shot         string   `json:"shot,omitempty"`
}

// PromptInput 是组装提示词所需的结构化输入。
type PromptInput struct {
	Kind           string
	Entry          novel.KBEntry
	Chapter        *novel.Chapter
	ProjectTitle   string
	StyleTag       string
	Negative       string
	PromptOverride string
}

// BuildPrompt 按「画风锚 + 角色特征 + 状态 + 镜头 + 环境 + 负面」组装提示词。
func BuildPrompt(in PromptInput) PromptSpec {
	style := strings.TrimSpace(in.StyleTag)
	negative := joinNegative(in.Negative)
	base := promptParts(in)
	spec := PromptSpec{
		Features:     base.features,
		CoreFeatures: base.core,
		StyleTag:     style,
		Shot:         base.shot,
		Negative:     negative,
	}
	if override := strings.TrimSpace(in.PromptOverride); override != "" {
		spec.Positive = override
	} else {
		parts := make([]string, 0, len(base.parts)+2)
		if style != "" {
			parts = append(parts, style)
		}
		parts = append(parts, base.parts...)
		spec.Positive = strings.Join(nonEmpty(parts), "，")
	}
	return spec
}

type promptBase struct {
	parts    []string
	features []string
	core     []string
	shot     string
}

func promptParts(in PromptInput) promptBase {
	entry := in.Entry
	name := strings.TrimSpace(entry.Name)
	summary := strings.TrimSpace(entry.Summary)
	field := func(key string) string { return strings.TrimSpace(entry.Fields[key]) }

	switch strings.TrimSpace(in.Kind) {
	case "character":
		appearance := field("appearance")
		features, core := splitFeatures(appearance)
		parts := []string{name}
		if appearance != "" {
			parts = append(parts, appearance)
		}
		if state := field("state"); state != "" {
			parts = append(parts, "当前状态："+state)
		}
		if summary != "" {
			parts = append(parts, summary)
		}
		parts = append(parts, "半身立绘", "干净背景")
		return promptBase{parts: parts, features: features, core: core, shot: "半身立绘"}
	case "scene", "location":
		parts := []string{name}
		for _, value := range []string{summary, field("content"), field("region"), field("description")} {
			if value != "" {
				parts = append(parts, value)
			}
		}
		parts = append(parts, "场景全景", "电影感构图")
		return promptBase{parts: parts, shot: "场景全景"}
	case "item":
		parts := []string{name}
		for _, value := range []string{summary, field("effect"), field("description")} {
			if value != "" {
				parts = append(parts, value)
			}
		}
		parts = append(parts, "物品特写", "静物摄影")
		return promptBase{parts: parts, shot: "物品特写"}
	case "cover":
		parts := []string{strings.TrimSpace(in.ProjectTitle), summary, "书籍封面构图", "留白标题区"}
		return promptBase{parts: parts, shot: "书籍封面"}
	case "illustration":
		parts := []string{"章节叙事插图"}
		if in.Chapter != nil {
			parts = append(parts, strings.TrimSpace(in.Chapter.Title))
			if excerpt := tailRunes(in.Chapter.Content, 300); excerpt != "" {
				parts = append(parts, "场景："+excerpt)
			}
		}
		parts = append(parts, "叙事插画", "宽幅构图")
		return promptBase{parts: parts, shot: "叙事插画"}
	default:
		parts := []string{name, summary}
		return promptBase{parts: parts}
	}
}

func splitFeatures(text string) ([]string, []string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return []string{}, []string{}
	}
	parts := strings.FieldsFunc(text, func(r rune) bool {
		switch r {
		case '，', '。', '；', '、', ',', ';', '\n', '\r', '|':
			return true
		}
		return false
	})
	features := make([]string, 0, len(parts))
	core := make([]string, 0, 4)
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if utf8.RuneCountInString(part) < 2 {
			continue
		}
		features = append(features, part)
		if isCoreFeature(part) {
			core = append(core, part)
		}
		if len(features) >= 12 {
			break
		}
	}
	return features, core
}

func isCoreFeature(feature string) bool {
	for _, keyword := range []string{"发", "瞳", "眼", "痣", "疤", "体型", "身高", "年龄", "配饰", "服饰", "衣", "面具", "纹"} {
		if strings.Contains(feature, keyword) {
			return true
		}
	}
	return false
}

func joinNegative(custom string) string {
	custom = strings.TrimSpace(custom)
	if custom == "" {
		return universalNegative
	}
	return custom + ", " + universalNegative
}

func nonEmpty(parts []string) []string {
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

func tailRunes(text string, limit int) string {
	text = strings.Join(strings.Fields(text), " ")
	runes := []rune(text)
	if len(runes) > limit {
		runes = runes[len(runes)-limit:]
	}
	return string(runes)
}

// Consistency 是一次生成的结构化一致性结论。
type Consistency struct {
	Score       float64
	Note        string
	VisualProxy bool
}

// ScoreConsistency 计算结构化一致性。
// 结构化评分只衡量提示词和特征，不读取图像。
func ScoreConsistency(prompt string, current PromptSpec, anchor *store.ImageAsset) Consistency {
	core := current.CoreFeatures
	if len(core) == 0 {
		core = current.Features
	}
	var featureHit float64
	note := ""
	if anchor == nil {
		if len(core) == 0 {
			featureHit = 1
		} else {
			matched := countPresent(prompt, core)
			featureHit = float64(matched) / float64(len(core))
		}
		note = "无基准图，按提示词特征完整度评分"
	} else {
		anchorCore := anchor.CoreFeatures
		if len(anchorCore) == 0 {
			anchorCore = anchor.Features
		}
		if len(core) == 0 {
			featureHit = 1
		} else if len(anchorCore) == 0 {
			featureHit = float64(countPresent(prompt, core)) / float64(len(core))
		} else {
			featureHit = overlapRate(core, anchorCore)
		}
		note = fmt.Sprintf("与基准图 %s 比对：核心特征命中 %.0f%%", anchor.ID, featureHit*100)
	}
	styleMatch := 0.0
	if strings.TrimSpace(current.StyleTag) != "" {
		if anchor == nil || strings.EqualFold(strings.TrimSpace(current.StyleTag), strings.TrimSpace(anchor.StyleTag)) {
			styleMatch = 1
		}
	} else if anchor == nil || strings.TrimSpace(anchor.StyleTag) == "" {
		styleMatch = 0.5
	}
	score := 0.6*featureHit + 0.3*featureHit + 0.1*styleMatch
	return Consistency{
		Score:       round2(score),
		Note:        note + "（仅检查提示词与结构化特征）",
		VisualProxy: true,
	}
}

func countPresent(prompt string, features []string) int {
	matched := 0
	for _, feature := range features {
		if strings.Contains(prompt, feature) {
			matched++
		}
	}
	return matched
}

func overlapRate(current, anchor []string) float64 {
	if len(current) == 0 {
		return 1
	}
	matched := 0
	for _, feature := range current {
		for _, other := range anchor {
			if strings.Contains(feature, other) || strings.Contains(other, feature) {
				matched++
				break
			}
		}
	}
	return float64(matched) / float64(len(current))
}

func round2(v float64) float64 {
	return float64(int(v*100+0.5)) / 100
}

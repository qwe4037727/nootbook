package eval

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Case 描述一个可重放的长篇回归用例。三类数据源互斥：
// dataDir（已分析/已初始化的数据目录副本）、corpus（UTF-8 文本，导入即分章）、
// synthetic（确定性合成长篇，用于预算与规模回归）。
type Case struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`

	Source SourceSpec `json:"source"`

	DefaultContextChars int         `json:"defaultContextChars,omitempty"`
	DefaultTargetChars  int         `json:"defaultTargetChars,omitempty"`
	StopPoints          []StopPoint `json:"stopPoints"`
}

type SourceSpec struct {
	DataDir   string         `json:"dataDir,omitempty"`
	Corpus    string         `json:"corpus,omitempty"`
	Synthetic *SyntheticSpec `json:"synthetic,omitempty"`
	// Knowledge 为 corpus/synthetic 模式下的 knowledge.json 初始快照；
	// dataDir 模式直接使用目录内知识库。
	Knowledge string `json:"knowledge,omitempty"`
	// Memory 为 corpus/synthetic 模式下的 data/memory 夹具目录，
	// 导入后复制，用于在没有真实分析链路时验证记忆注入与预算。
	Memory string `json:"memory,omitempty"`
}

type SyntheticSpec struct {
	Chapters     int           `json:"chapters"`
	ChapterChars int           `json:"chapterChars"`
	Seed         int64         `json:"seed"`
	Cast         []string      `json:"cast,omitempty"`
	Planted      []PlantedFact `json:"planted,omitempty"`
}

type PlantedFact struct {
	Text    string `json:"text"`
	Chapter int    `json:"chapter"`
}

// StopPoint 是一个"续写停点"：只把第 1..AfterChapter 章交给 context 组装，
// 与真实续写一致；AfterChapter<=0 表示使用全部章节。
type StopPoint struct {
	Name         string `json:"name"`
	AfterChapter int    `json:"afterChapter,omitempty"`
	Direction    string `json:"direction,omitempty"`
	TargetChars  int    `json:"targetChars,omitempty"`
	ContextChars int    `json:"contextChars,omitempty"`
	UseMemory    *bool  `json:"useMemory,omitempty"`
	// UseRecall 缺省按章节数自动判定 R4；显式值用于召回开/关 A/B 对照。
	UseRecall *bool    `json:"useRecall,omitempty"`
	MustLand  []string `json:"mustLand,omitempty"`
	MustNot   []string `json:"mustNot,omitempty"`
	Notes     string   `json:"notes,omitempty"`
}

func LoadCase(path string) (Case, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Case{}, fmt.Errorf("eval: read case: %w", err)
	}
	var c Case
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&c); err != nil {
		return Case{}, fmt.Errorf("eval: decode case %s: %w", filepath.Base(path), err)
	}
	if err := c.Validate(); err != nil {
		return Case{}, err
	}
	return c, nil
}

func (c Case) Validate() error {
	if strings.TrimSpace(c.ID) == "" {
		return errors.New("eval: case id is required")
	}
	if len(c.StopPoints) == 0 {
		return errors.New("eval: case needs at least one stopPoint")
	}
	sources := 0
	if strings.TrimSpace(c.Source.DataDir) != "" {
		sources++
	}
	if strings.TrimSpace(c.Source.Corpus) != "" {
		sources++
	}
	if c.Source.Synthetic != nil {
		sources++
	}
	if sources != 1 {
		return errors.New("eval: source must set exactly one of dataDir/corpus/synthetic")
	}
	if spec := c.Source.Synthetic; spec != nil {
		if spec.Chapters <= 0 || spec.Chapters > 5000 {
			return errors.New("eval: synthetic.chapters must be in 1..5000")
		}
		if spec.ChapterChars < 200 || spec.ChapterChars > 50000 {
			return errors.New("eval: synthetic.chapterChars must be in 200..50000")
		}
		for _, planted := range spec.Planted {
			if strings.TrimSpace(planted.Text) == "" {
				return errors.New("eval: synthetic.planted text is empty")
			}
			if planted.Chapter <= 0 || planted.Chapter > spec.Chapters {
				return errors.New("eval: synthetic.planted chapter out of range")
			}
		}
	}
	seen := make(map[string]struct{}, len(c.StopPoints))
	for _, stop := range c.StopPoints {
		name := strings.TrimSpace(stop.Name)
		if name == "" {
			return errors.New("eval: stopPoint name is required")
		}
		if _, ok := seen[name]; ok {
			return fmt.Errorf("eval: duplicate stopPoint name %q", name)
		}
		seen[name] = struct{}{}
		if stop.TargetChars < 0 || stop.ContextChars < 0 {
			return fmt.Errorf("eval: stopPoint %q has negative budget", name)
		}
	}
	return nil
}

func boolValue(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}

package eval

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"novelagent/internal/novel"
	"novelagent/internal/store"
)

type CorpusInfo struct {
	Kind     string `json:"kind"`
	Title    string `json:"title"`
	Chapters int    `json:"chapters"`
	Chars    int    `json:"chars"`
}

type Source struct {
	Info      CorpusInfo
	Chapters  []novel.Chapter
	Knowledge novel.Knowledge
	Config    store.Config
	Store     *store.Store
}

// Prepare 在 workDir（必须是空目录或不存在）中构造隔离数据副本。
// dataDir 会被完整复制后再打开，绝不改写用例源目录。
func Prepare(c Case, caseDir, workDir string) (Source, error) {
	if err := c.Validate(); err != nil {
		return Source{}, err
	}
	resolve := func(path string) string {
		if path == "" || filepath.IsAbs(path) {
			return path
		}
		return filepath.Join(caseDir, path)
	}

	info := CorpusInfo{Kind: "synthetic"}
	var text string
	switch {
	case c.Source.DataDir != "":
		info.Kind = "data_dir"
		if c.Source.Knowledge != "" || c.Source.Memory != "" {
			return Source{}, errors.New("eval: dataDir source already contains knowledge/memory; remove knowledge/memory fields")
		}
		if err := copyTree(resolve(c.Source.DataDir), workDir); err != nil {
			return Source{}, fmt.Errorf("eval: copy data dir: %w", err)
		}
	case c.Source.Corpus != "":
		info.Kind = "corpus"
		data, err := os.ReadFile(resolve(c.Source.Corpus))
		if err != nil {
			return Source{}, fmt.Errorf("eval: read corpus: %w", err)
		}
		text = strings.TrimPrefix(string(data), "\ufeff")
		if !utf8.ValidString(text) {
			return Source{}, errors.New("eval: corpus is not valid UTF-8 (convert GB18030/UTF-16 before use)")
		}
	case c.Source.Synthetic != nil:
		text = GenerateSynthetic(*c.Source.Synthetic)
	}

	title := strings.TrimSpace(c.Title)
	if title == "" && text != "" {
		title = novel.InferTitle(text)
	}
	if title == "" {
		title = "长篇回归用例"
	}

	s, err := store.New(workDir)
	if err != nil {
		return Source{}, fmt.Errorf("eval: open isolated store: %w", err)
	}
	if text != "" {
		chapters := novel.SplitChapters(text, title)
		if len(chapters) == 0 {
			return Source{}, errors.New("eval: corpus produced no chapters")
		}
		if _, err := s.ImportChapters(title, chapters); err != nil {
			return Source{}, fmt.Errorf("eval: import corpus: %w", err)
		}
		if c.Source.Knowledge != "" {
			if err := seedKnowledge(s, resolve(c.Source.Knowledge)); err != nil {
				return Source{}, err
			}
		}
		if c.Source.Memory != "" {
			if err := seedMemory(resolve(c.Source.Memory), filepath.Join(workDir, "memory")); err != nil {
				return Source{}, err
			}
		}
	}

	project, err := s.LoadProject()
	if err != nil {
		return Source{}, fmt.Errorf("eval: load project: %w", err)
	}
	chapters, err := s.LoadChapters()
	if err != nil {
		return Source{}, fmt.Errorf("eval: load chapters: %w", err)
	}
	knowledge, err := s.LoadKnowledge()
	if err != nil {
		return Source{}, fmt.Errorf("eval: load knowledge: %w", err)
	}
	config, err := s.LoadConfig()
	if err != nil {
		return Source{}, fmt.Errorf("eval: load config: %w", err)
	}
	config.Normalize()

	info.Title = project.Title
	info.Chapters = len(chapters)
	for _, chapter := range chapters {
		info.Chars += novel.CountChars(chapter.Content)
	}
	return Source{
		Info:      info,
		Chapters:  chapters,
		Knowledge: knowledge,
		Config:    config,
		Store:     s,
	}, nil
}

func seedKnowledge(s *store.Store, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("eval: read knowledge: %w", err)
	}
	var knowledge novel.Knowledge
	if err := json.Unmarshal(data, &knowledge); err != nil {
		return fmt.Errorf("eval: decode knowledge: %w", err)
	}
	knowledge.Normalize()
	project, err := s.LoadProject()
	if err != nil {
		return fmt.Errorf("eval: load project for knowledge seed: %w", err)
	}
	if _, _, err := s.CommitAnalysis(project.Revision, len(project.Chapters), knowledge); err != nil {
		return fmt.Errorf("eval: seed knowledge: %w", err)
	}
	return nil
}

var retrievableMemoryFiles = map[string]struct{}{
	"meta.json":       {},
	"states.json":     {},
	"events.json":     {},
	"cognitions.json": {},
	"threads.json":    {},
	"plans.json":      {},
}

func seedMemory(srcDir, dstDir string) error {
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return fmt.Errorf("eval: create memory dir: %w", err)
	}
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return fmt.Errorf("eval: read memory fixtures: %w", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() {
			continue
		}
		if _, ok := retrievableMemoryFiles[name]; !ok {
			continue
		}
		if err := copyFile(filepath.Join(srcDir, name), filepath.Join(dstDir, name)); err != nil {
			return fmt.Errorf("eval: copy memory fixture %s: %w", name, err)
		}
	}
	return nil
}

func copyTree(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", src)
	}
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		return copyFile(path, target)
	})
}

func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

var syntheticCast = []string{"林砚", "沈青禾", "陆行舟", "阿箬", "老周", "顾临川"}

var syntheticPlaces = []string{"长街", "县衙", "渡口", "旧宅", "山道", "茶馆", "城门", "药铺", "河边", "后巷"}

var syntheticChapterTitles = []string{
	"雨夜", "旧信", "断桥", "灯影", "渡口", "山雨", "空宅", "长街",
	"夜行", "归人", "残卷", "铜铃", "石阶", "薄雾", "寒鸦", "远钟",
	"暗巷", "孤灯", "回廊", "霜降",
}

// GenerateSynthetic 生成确定性合成长篇：用于预算、规模与停点回归，
// 不用于文学质量评价。同名人物高频出现，便于角色种子解析。
func GenerateSynthetic(spec SyntheticSpec) string {
	cast := spec.Cast
	if len(cast) == 0 {
		cast = syntheticCast
	}
	seed := spec.Seed
	if seed == 0 {
		seed = 1
	}
	rng := rand.New(rand.NewSource(seed))
	plantedByChapter := make(map[int][]string, len(spec.Planted))
	for _, planted := range spec.Planted {
		text := strings.Join(strings.Fields(planted.Text), " ")
		plantedByChapter[planted.Chapter] = append(plantedByChapter[planted.Chapter], text)
	}

	var corpus strings.Builder
	for chapter := 1; chapter <= spec.Chapters; chapter++ {
		title := syntheticChapterTitles[(chapter-1)%len(syntheticChapterTitles)]
		fmt.Fprintf(&corpus, "第%d章 %s\n\n", chapter, title)

		paragraphs := make([]string, 0, 16)
		chars := 0
		first := true
		for chars < spec.ChapterChars {
			var paragraph strings.Builder
			sentences := 3 + rng.Intn(3)
			for i := 0; i < sentences; i++ {
				sentence := syntheticSentence(rng, cast, syntheticPlaces)
				paragraph.WriteString(sentence)
				chars += utf8.RuneCountInString(sentence)
			}
			paragraphs = append(paragraphs, paragraph.String())
			if first {
				for _, planted := range plantedByChapter[chapter] {
					paragraphs = append(paragraphs, planted)
					chars += utf8.RuneCountInString(planted)
				}
				first = false
			}
		}
		corpus.WriteString(strings.Join(paragraphs, "\n\n"))
		corpus.WriteString("\n\n")
	}
	return strings.TrimSpace(corpus.String())
}

func syntheticSentence(rng *rand.Rand, cast, places []string) string {
	a := cast[rng.Intn(len(cast))]
	b := cast[rng.Intn(len(cast))]
	place := places[rng.Intn(len(places))]
	switch rng.Intn(12) {
	case 0:
		return fmt.Sprintf("%s沿着%s往前走，雨声一直没有停。", a, place)
	case 1:
		return fmt.Sprintf("%s把手按在袖口，那里藏着一件不能示人的东西。", a)
	case 2:
		return fmt.Sprintf("%s没有立刻回答，目光落在%s的尽头。", a, place)
	case 3:
		return fmt.Sprintf("%s压低了声音，只让%s一个人听见。", a, b)
	case 4:
		return fmt.Sprintf("灯影在%s里晃了一下，%s停住脚步。", place, a)
	case 5:
		return fmt.Sprintf("%s忽然想起多年前的旧事，心口发紧。", a)
	case 6:
		return fmt.Sprintf("夜里起风了，%s替%s关上了窗。", a, b)
	case 7:
		return fmt.Sprintf("%s知道，再往前走就回不了头。", a)
	case 8:
		return fmt.Sprintf("%s在%s门口等了很久，衣角被雨水打湿。", a, place)
	case 9:
		return fmt.Sprintf("%s和%s对视一眼，谁都没有先开口。", a, b)
	case 10:
		return fmt.Sprintf("远处传来马蹄声，%s把%s往身后带了带。", a, b)
	default:
		return fmt.Sprintf("%s收起桌上的纸页，%s的光落在字迹上。", a, place)
	}
}

package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"novelagent/internal/novel"
)

const (
	defaultContextChars = 12000
	minContextChars     = 1000
	maxContextChars     = 100000
	chaptersDirName     = "chapters"
)

var (
	ErrChapterNotFound = errors.New("store: chapter not found")
	ErrInvalidChapter  = errors.New("store: invalid chapter id")
)

// chapterOrderHook 为章节序号变化收尾回调（memory_model_spec.md §4.1.4 / R9 冻结，
// 签名按 #3 审查意见 R10 携带 error 返回）：
// 由记忆模块（#7）在服务启动阶段注册，默认 nil 零开销。
// 调用点位于 store.mu 释放之后；回调返回 error 或 panic 均仅记录日志，
// 不得阻断、不得回滚章节操作；回调内部不得调用 Store 的公共加锁方法（死锁防护）。
// 注册与读取经 chapterOrderHookMu 互斥保护，避免运行期注册与章节写操作的数据竞争。
var (
	chapterOrderHookMu sync.RWMutex
	chapterOrderHook   func(oldIndexes, newIndexes map[string]int) error
)

// RegisterChapterOrderHook 注册章节序号变化回调；约定仅在服务启动阶段（记忆模块初始化时）调用。
func RegisterChapterOrderHook(fn func(oldIndexes, newIndexes map[string]int) error) {
	chapterOrderHookMu.Lock()
	defer chapterOrderHookMu.Unlock()
	chapterOrderHook = fn
}

func notifyChapterOrderChanged(oldIndexes, newIndexes map[string]int) {
	chapterOrderHookMu.RLock()
	fn := chapterOrderHook
	chapterOrderHookMu.RUnlock()
	if fn == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			log.Printf("store: chapter order hook failed (ignored): %v", r)
		}
	}()
	if err := fn(oldIndexes, newIndexes); err != nil {
		log.Printf("store: chapter order hook failed (ignored): %v", err)
	}
}

// chapterOrderChanged 判断两次 ID→Index 映射是否存在差异（含新增/删除的 ID）。
func chapterOrderChanged(oldIndexes, newIndexes map[string]int) bool {
	if len(oldIndexes) != len(newIndexes) {
		return true
	}
	for id, index := range oldIndexes {
		if newIndexes[id] != index {
			return true
		}
	}
	return false
}

func chapterIndexMap(metas []novel.ChapterMeta) map[string]int {
	m := make(map[string]int, len(metas))
	for _, meta := range metas {
		m[meta.ID] = meta.Index
	}
	return m
}

type Config struct {
	BaseURL      string `json:"baseUrl"`
	Model        string `json:"model"`
	APIKey       string `json:"apiKey,omitempty"`
	ContextChars int    `json:"contextChars"`
}

func DefaultConfig() Config {
	return Config{ContextChars: defaultContextChars}
}

func (c *Config) Normalize() {
	c.BaseURL = strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
	c.Model = strings.TrimSpace(c.Model)
	c.APIKey = strings.TrimSpace(c.APIKey)
	if c.ContextChars == 0 {
		c.ContextChars = defaultContextChars
	}
	c.ContextChars = min(max(c.ContextChars, minContextChars), maxContextChars)
}

func (c Config) EffectiveAPIKey() (string, bool) {
	if key := strings.TrimSpace(os.Getenv("OPENAI_API_KEY")); key != "" {
		return key, true
	}
	return c.APIKey, false
}

type Store struct {
	dir string
	mu  sync.Mutex
}

func New(dir string) (*Store, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, errors.New("store: data directory is empty")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("store: create data directory: %w", err)
	}
	s := &Store{dir: dir}
	if err := s.migrateLegacy(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) LoadProject() (novel.Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadProjectLocked()
}

func (s *Store) UpdateTitle(title string) (novel.Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	project, err := s.loadProjectLocked()
	if err != nil {
		return novel.Project{}, err
	}
	project.Title = strings.TrimSpace(title)
	project.Normalize()
	project.UpdatedAt = time.Now().UTC()
	if err := s.writeProjectLocked(project); err != nil {
		return novel.Project{}, err
	}
	return project, nil
}

func (s *Store) ImportChapters(title string, chapters []novel.Chapter) (novel.Project, error) {
	s.mu.Lock()
	project, oldIndexes, newIndexes, err := s.importChaptersLocked(title, chapters)
	s.mu.Unlock()
	if err == nil && chapterOrderChanged(oldIndexes, newIndexes) {
		notifyChapterOrderChanged(oldIndexes, newIndexes)
	}
	return project, err
}

func (s *Store) importChaptersLocked(title string, chapters []novel.Chapter) (novel.Project, map[string]int, map[string]int, error) {
	previous, err := s.loadProjectLocked()
	if err != nil {
		return novel.Project{}, nil, nil, err
	}
	oldIndexes := chapterIndexMap(previous.Chapters)

	now := time.Now().UTC()
	prepared := make([]novel.Chapter, 0, len(chapters))
	seenIDs := make(map[string]struct{}, len(chapters))
	for _, chapter := range chapters {
		chapter.Content = strings.TrimSpace(chapter.Content)
		if chapter.Content == "" {
			continue
		}
		chapter.Title = strings.TrimSpace(chapter.Title)
		if chapter.Title == "" {
			chapter.Title = novel.DefaultChapterTitle(len(prepared) + 1)
		}
		chapter.Index = len(prepared) + 1
		if chapter.ID == "" || !novel.ValidID(chapter.ID) {
			chapter.ID = novel.NewID("ch")
		}
		if _, exists := seenIDs[chapter.ID]; exists {
			chapter.ID = novel.NewID("ch")
		}
		seenIDs[chapter.ID] = struct{}{}
		chapter.CharCount = novel.CountChars(chapter.Content)
		chapter.Revision = 1 // 新建/导入章节固定基线（决策 8），忽略客户端携带值
		chapter.UpdatedAt = now
		prepared = append(prepared, chapter)
	}
	if len(prepared) == 0 {
		return novel.Project{}, nil, nil, errors.New("store: import has no chapter content")
	}

	if err := os.MkdirAll(filepath.Join(s.dir, chaptersDirName), 0o755); err != nil {
		return novel.Project{}, nil, nil, fmt.Errorf("store: create chapters directory: %w", err)
	}
	for _, chapter := range prepared {
		if err := s.writeChapterLocked(chapter); err != nil {
			return novel.Project{}, nil, nil, err
		}
	}
	if err := s.removeStaleChaptersLocked(seenIDs); err != nil {
		return novel.Project{}, nil, nil, err
	}

	knowledge := novel.NewKnowledge()
	knowledge.UpdatedAt = now
	if err := s.writeKnowledgeLocked(knowledge); err != nil {
		return novel.Project{}, nil, nil, err
	}

	project := novel.Project{
		Title:    strings.TrimSpace(title),
		Chapters: make([]novel.ChapterMeta, 0, len(prepared)),
		Analysis: novel.AnalysisState{
			Status:        novel.AnalysisPending,
			AnalyzedCount: 0,
			TotalCount:    len(prepared),
			UpdatedAt:     now,
		},
		Revision:  previous.Revision + 1,
		UpdatedAt: now,
	}
	if project.Title == "" {
		project.Title = previous.Title
	}
	for _, chapter := range prepared {
		project.Chapters = append(project.Chapters, chapter.Meta())
	}
	project.Normalize()
	if err := s.writeProjectLocked(project); err != nil {
		return novel.Project{}, nil, nil, err
	}
	newIndexes := chapterIndexMap(project.Chapters)
	return project, oldIndexes, newIndexes, nil
}

func (s *Store) LoadChapter(id string) (novel.Chapter, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !novel.ValidID(id) {
		return novel.Chapter{}, ErrInvalidChapter
	}
	project, err := s.loadProjectLocked()
	if err != nil {
		return novel.Chapter{}, err
	}
	for i, meta := range project.Chapters {
		if meta.ID == id {
			chapter, err := s.readChapterLocked(meta.ID)
			if err != nil {
				return novel.Chapter{}, err
			}
			chapter.Index = meta.Index
			// 真源规则：chapter 文件为 Content/Title/Revision 真源，meta 为缓存；
			// 不一致时以 chapter 文件修复 meta（缓存修复不递增任何 Revision）。
			if syncChapterMetaLocked(&project, i, chapter) {
				if err := s.writeProjectLocked(project); err != nil {
					return novel.Chapter{}, err
				}
			}
			return chapter, nil
		}
	}
	return novel.Chapter{}, ErrChapterNotFound
}

func (s *Store) LoadChapters() ([]novel.Chapter, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	project, err := s.loadProjectLocked()
	if err != nil {
		return nil, err
	}
	chapters := make([]novel.Chapter, 0, len(project.Chapters))
	repaired := false
	for i, meta := range project.Chapters {
		chapter, err := s.readChapterLocked(meta.ID)
		if err != nil {
			return nil, err
		}
		chapter.Index = meta.Index
		if syncChapterMetaLocked(&project, i, chapter) {
			repaired = true
		}
		chapters = append(chapters, chapter)
	}
	if repaired {
		if err := s.writeProjectLocked(project); err != nil {
			return nil, err
		}
	}
	return chapters, nil
}

// syncChapterMetaLocked 以 chapter 文件值修复 project.Chapters[i] 缓存；
// 返回是否发生了修复（仅修复 Title/CharCount/Revision/UpdatedAt，Index 属排序归属不动）。
func syncChapterMetaLocked(project *novel.Project, i int, chapter novel.Chapter) bool {
	meta := project.Chapters[i]
	if meta.Title == chapter.Title && meta.CharCount == chapter.CharCount &&
		meta.Revision == chapter.Revision && meta.UpdatedAt.Equal(chapter.UpdatedAt) {
		return false
	}
	meta.Title = chapter.Title
	meta.CharCount = chapter.CharCount
	meta.Revision = chapter.Revision
	meta.UpdatedAt = chapter.UpdatedAt
	project.Chapters[i] = meta
	return true
}

func (s *Store) SaveChapter(chapter novel.Chapter) (novel.Chapter, novel.Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	project, err := s.loadProjectLocked()
	if err != nil {
		return novel.Chapter{}, novel.Project{}, err
	}
	metaIndex := -1
	for i, meta := range project.Chapters {
		if meta.ID == chapter.ID {
			metaIndex = i
			break
		}
	}
	if metaIndex < 0 {
		return novel.Chapter{}, novel.Project{}, ErrChapterNotFound
	}

	// 真源规则：以 chapter 文件中的现值判定是否实际变化（而非 meta 缓存）。
	existing, err := s.readChapterLocked(chapter.ID)
	if err != nil {
		return novel.Chapter{}, novel.Project{}, err
	}

	chapter.Content = strings.TrimSpace(chapter.Content)
	chapter.Title = strings.TrimSpace(chapter.Title)
	if chapter.Title == "" {
		chapter.Title = novel.DefaultChapterTitle(project.Chapters[metaIndex].Index)
	}
	chapter.Index = project.Chapters[metaIndex].Index

	// 无变化短路（决策 8 / U2）：Title 与 Content 均未实际变化 →
	// Revision、UpdatedAt 不变，Analysis 不回退，project.Revision 不递增，零内容写入。
	if existing.Title == chapter.Title && existing.Content == chapter.Content {
		result := existing
		result.Index = chapter.Index
		// R11：短路前仍以文件值修复 meta 缓存——若此前"章节文件写成功、project.json
		// 写失败"，旧 meta 不应被后续相同内容的保存无限保留。缓存修复不递增任何 Revision。
		if syncChapterMetaLocked(&project, metaIndex, result) {
			if err := s.writeProjectLocked(project); err != nil {
				return novel.Chapter{}, novel.Project{}, err
			}
		}
		return result, project, nil
	}

	chapter.Revision = existing.Revision + 1
	chapter.CharCount = novel.CountChars(chapter.Content)
	chapter.UpdatedAt = time.Now().UTC()
	if err := s.writeChapterLocked(chapter); err != nil {
		return novel.Chapter{}, novel.Project{}, err
	}

	project.Chapters[metaIndex] = chapter.Meta()
	state := project.Analysis
	state.AnalyzedCount = min(state.AnalyzedCount, chapter.Index-1)
	state.Status = novel.AnalysisPending
	state.Message = ""
	state.UpdatedAt = chapter.UpdatedAt
	project.Analysis = state
	project.Revision++
	project.UpdatedAt = chapter.UpdatedAt
	project.Normalize()
	if err := s.writeProjectLocked(project); err != nil {
		return novel.Chapter{}, novel.Project{}, err
	}
	return chapter, project, nil
}

func (s *Store) AppendChapter(title, content string) (novel.Chapter, novel.Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	project, err := s.loadProjectLocked()
	if err != nil {
		return novel.Chapter{}, novel.Project{}, err
	}

	now := time.Now().UTC()
	index := len(project.Chapters) + 1
	title = strings.TrimSpace(title)
	if title == "" {
		title = novel.DefaultChapterTitle(index)
	}
	chapter := novel.Chapter{
		ID:        novel.NewID("ch"),
		Index:     index,
		Title:     title,
		Content:   strings.TrimSpace(content),
		CharCount: novel.CountChars(content),
		Revision:  1, // 新建章节固定基线（决策 8）
		UpdatedAt: now,
	}
	if err := s.writeChapterLocked(chapter); err != nil {
		return novel.Chapter{}, novel.Project{}, err
	}

	project.Chapters = append(project.Chapters, chapter.Meta())
	state := project.Analysis
	if chapter.CharCount > 0 {
		state.Status = novel.AnalysisPending
		state.Message = ""
	}
	state.TotalCount = len(project.Chapters)
	state.UpdatedAt = now
	project.Analysis = state
	project.Revision++
	project.UpdatedAt = now
	project.Normalize()
	if err := s.writeProjectLocked(project); err != nil {
		return novel.Chapter{}, novel.Project{}, err
	}
	return chapter, project, nil
}

func (s *Store) DeleteChapter(id string) (novel.Project, error) {
	s.mu.Lock()
	project, oldIndexes, newIndexes, err := s.deleteChapterLocked(id)
	s.mu.Unlock()
	if err == nil && chapterOrderChanged(oldIndexes, newIndexes) {
		// R9：回调在锁释放后执行，失败不阻断、不回滚章节删除。
		notifyChapterOrderChanged(oldIndexes, newIndexes)
	}
	return project, err
}

func (s *Store) deleteChapterLocked(id string) (novel.Project, map[string]int, map[string]int, error) {
	project, err := s.loadProjectLocked()
	if err != nil {
		return novel.Project{}, nil, nil, err
	}
	oldIndexes := chapterIndexMap(project.Chapters)
	deletedIndex := 0
	next := make([]novel.ChapterMeta, 0, len(project.Chapters))
	for _, meta := range project.Chapters {
		if meta.ID == id {
			deletedIndex = meta.Index
			continue
		}
		next = append(next, meta)
	}
	if deletedIndex == 0 {
		return novel.Project{}, nil, nil, ErrChapterNotFound
	}
	if path, err := s.chapterPath(id); err == nil {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return novel.Project{}, nil, nil, fmt.Errorf("store: delete chapter: %w", err)
		}
	}

	now := time.Now().UTC()
	project.Chapters = next
	state := project.Analysis
	if len(next) == 0 {
		state.Status = novel.AnalysisIdle
		state.AnalyzedCount = 0
		state.Message = ""
	} else if state.Status != novel.AnalysisIdle {
		state.Status = novel.AnalysisPending
		state.AnalyzedCount = min(state.AnalyzedCount, deletedIndex-1)
		state.Message = ""
	}
	state.TotalCount = len(next)
	state.UpdatedAt = now
	project.Analysis = state
	project.Revision++
	project.UpdatedAt = now
	project.Normalize()
	if err := s.writeProjectLocked(project); err != nil {
		return novel.Project{}, nil, nil, err
	}
	newIndexes := chapterIndexMap(project.Chapters)
	return project, oldIndexes, newIndexes, nil
}

func (s *Store) LoadKnowledge() (novel.Knowledge, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadKnowledgeLocked()
}

func (s *Store) CommitAnalysis(revision int64, analyzed int, knowledge novel.Knowledge) (novel.Project, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	project, err := s.loadProjectLocked()
	if err != nil {
		return novel.Project{}, false, err
	}
	if project.Revision != revision {
		return project, false, nil
	}
	now := time.Now().UTC()
	knowledge.Normalize()
	knowledge.UpdatedAt = now
	if err := s.writeKnowledgeLocked(knowledge); err != nil {
		return novel.Project{}, false, err
	}
	state := project.Analysis
	state.Status = novel.AnalysisRunning
	state.AnalyzedCount = min(max(analyzed, 0), state.TotalCount)
	state.Message = ""
	state.UpdatedAt = now
	project.Analysis = state
	project.UpdatedAt = now
	project.Normalize()
	if err := s.writeProjectLocked(project); err != nil {
		return novel.Project{}, false, err
	}
	return project, true, nil
}

func (s *Store) FinishAnalysis(revision int64, knowledge novel.Knowledge) (novel.Project, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	project, err := s.loadProjectLocked()
	if err != nil {
		return novel.Project{}, false, err
	}
	if project.Revision != revision {
		return project, false, nil
	}
	now := time.Now().UTC()
	knowledge.Normalize()
	knowledge.UpdatedAt = now
	if err := s.writeKnowledgeLocked(knowledge); err != nil {
		return novel.Project{}, false, err
	}
	state := project.Analysis
	state.Status = novel.AnalysisReady
	state.AnalyzedCount = state.TotalCount
	state.Message = ""
	state.UpdatedAt = now
	project.Analysis = state
	project.UpdatedAt = now
	project.Normalize()
	if err := s.writeProjectLocked(project); err != nil {
		return novel.Project{}, false, err
	}
	return project, true, nil
}

func (s *Store) FailAnalysis(revision int64, message string) (novel.Project, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	project, err := s.loadProjectLocked()
	if err != nil {
		return novel.Project{}, false, err
	}
	if project.Revision != revision {
		return project, false, nil
	}
	state := project.Analysis
	state.Status = novel.AnalysisError
	state.Message = truncate(strings.TrimSpace(message), 200)
	state.UpdatedAt = time.Now().UTC()
	project.Analysis = state
	project.UpdatedAt = state.UpdatedAt
	project.Normalize()
	if err := s.writeProjectLocked(project); err != nil {
		return novel.Project{}, false, err
	}
	return project, true, nil
}

func (s *Store) SetAnalysisPending() (novel.Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	project, err := s.loadProjectLocked()
	if err != nil {
		return novel.Project{}, err
	}
	if len(project.Chapters) == 0 {
		return project, nil
	}
	state := project.Analysis
	state.Status = novel.AnalysisPending
	state.Message = ""
	state.UpdatedAt = time.Now().UTC()
	project.Analysis = state
	project.UpdatedAt = state.UpdatedAt
	project.Normalize()
	if err := s.writeProjectLocked(project); err != nil {
		return novel.Project{}, err
	}
	return project, nil
}

func (s *Store) LoadConfig() (Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	cfg := DefaultConfig()
	err := s.readJSONLocked(s.path("config.json"), &cfg)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("store: load config: %w", err)
	}
	cfg.Normalize()
	return cfg, nil
}

func (s *Store) SaveConfig(cfg Config) (Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	cfg.Normalize()
	if err := writeJSONFile(s.path("config.json"), cfg, 0o600); err != nil {
		return Config{}, fmt.Errorf("store: save config: %w", err)
	}
	return cfg, nil
}

func (s *Store) loadProjectLocked() (novel.Project, error) {
	var project novel.Project
	err := s.readJSONLocked(s.path("project.json"), &project)
	if errors.Is(err, os.ErrNotExist) {
		return novel.NewProject(), nil
	}
	if err != nil {
		return novel.Project{}, fmt.Errorf("store: load project: %w", err)
	}
	project.Normalize()
	return project, nil
}

func (s *Store) writeProjectLocked(project novel.Project) error {
	if err := writeJSONFile(s.path("project.json"), project, 0o644); err != nil {
		return fmt.Errorf("store: save project: %w", err)
	}
	return nil
}

func (s *Store) loadKnowledgeLocked() (novel.Knowledge, error) {
	knowledge := novel.NewKnowledge()
	err := s.readJSONLocked(s.path("knowledge.json"), &knowledge)
	if errors.Is(err, os.ErrNotExist) {
		return knowledge, nil
	}
	if err != nil {
		return novel.Knowledge{}, fmt.Errorf("store: load knowledge: %w", err)
	}
	knowledge.Normalize()
	return knowledge, nil
}

func (s *Store) writeKnowledgeLocked(knowledge novel.Knowledge) error {
	if err := writeJSONFile(s.path("knowledge.json"), knowledge, 0o600); err != nil {
		return fmt.Errorf("store: save knowledge: %w", err)
	}
	return nil
}

func (s *Store) readChapterLocked(id string) (novel.Chapter, error) {
	path, err := s.chapterPath(id)
	if err != nil {
		return novel.Chapter{}, err
	}
	var chapter novel.Chapter
	if err := s.readJSONLocked(path, &chapter); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return novel.Chapter{}, fmt.Errorf("store: chapter %s is missing: %w", id, err)
		}
		return novel.Chapter{}, fmt.Errorf("store: load chapter %s: %w", id, err)
	}
	chapter.Content = strings.TrimSpace(chapter.Content)
	// 旧数据兼容（决策 8 / U4）：旧文件无 revision → 解析得 0 → 加载时置 1（幂等）。
	if chapter.Revision <= 0 {
		chapter.Revision = 1
	}
	return chapter, nil
}

func (s *Store) writeChapterLocked(chapter novel.Chapter) error {
	path, err := s.chapterPath(chapter.ID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("store: create chapters directory: %w", err)
	}
	if err := writeJSONFile(path, chapter, 0o644); err != nil {
		return fmt.Errorf("store: save chapter %s: %w", chapter.ID, err)
	}
	return nil
}

func (s *Store) removeStaleChaptersLocked(keep map[string]struct{}) error {
	entries, err := os.ReadDir(filepath.Join(s.dir, chaptersDirName))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("store: list chapters: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".json")
		if _, ok := keep[id]; ok {
			continue
		}
		if err := os.Remove(filepath.Join(s.dir, chaptersDirName, entry.Name())); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("store: remove stale chapter: %w", err)
		}
	}
	return nil
}

func (s *Store) chapterPath(id string) (string, error) {
	if !novel.ValidID(id) {
		return "", ErrInvalidChapter
	}
	return filepath.Join(s.dir, chaptersDirName, id+".json"), nil
}

func (s *Store) path(name string) string {
	return filepath.Join(s.dir, name)
}

func (s *Store) readJSONLocked(path string, dst any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, dst); err != nil {
		return fmt.Errorf("decode %s: %w", filepath.Base(path), err)
	}
	return nil
}

func writeJSONFile(path string, value any, perm os.FileMode) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", filepath.Base(path), err)
	}
	data = append(data, '\n')

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func (s *Store) migrateLegacy() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := s.path("project.json")
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("store: read legacy project: %w", err)
	}

	type legacyProject struct {
		Title      string             `json:"title"`
		Style      string             `json:"style"`
		Manuscript string             `json:"manuscript"`
		Characters []novel.Character  `json:"characters"`
		World      []novel.WorldEntry `json:"world"`
		Chapters   json.RawMessage    `json:"chapters"`
	}
	var legacy legacyProject
	if err := json.Unmarshal(data, &legacy); err != nil {
		return fmt.Errorf("store: decode legacy project: %w", err)
	}
	if len(legacy.Chapters) > 0 && string(legacy.Chapters) != "null" {
		return nil
	}
	if strings.TrimSpace(legacy.Manuscript) == "" &&
		strings.TrimSpace(legacy.Style) == "" &&
		len(legacy.Characters) == 0 &&
		len(legacy.World) == 0 {
		return nil
	}

	backup := s.path("project.v1.backup.json")
	if _, err := os.Stat(backup); errors.Is(err, os.ErrNotExist) {
		if err := os.WriteFile(backup, data, 0o644); err != nil {
			return fmt.Errorf("store: back up legacy project: %w", err)
		}
	}

	now := time.Now().UTC()
	chapters := novel.SplitChapters(legacy.Manuscript, "正文")
	for i := range chapters {
		chapters[i].Index = i + 1
		chapters[i].Revision = 1 // 迁移章节固定基线（决策 8）
		chapters[i].UpdatedAt = now
		if err := s.writeChapterLocked(chapters[i]); err != nil {
			return err
		}
	}

	knowledge := novel.NewKnowledge()
	knowledge.Style.Notes = strings.TrimSpace(legacy.Style)
	for _, character := range legacy.Characters {
		character.ID = novel.NewID("c")
		character.Aliases = nil
		character.State = ""
		character.Evidence = nil
		character.Confidence = 0.9
		character.LastSeen = 0
		knowledge.Characters = append(knowledge.Characters, character)
	}
	for _, entry := range legacy.World {
		entry.ID = novel.NewID("w")
		entry.Aliases = nil
		entry.Evidence = nil
		entry.Confidence = 0.8
		entry.LastSeen = 0
		knowledge.World = append(knowledge.World, entry)
	}
	knowledge.Normalize()
	knowledge.UpdatedAt = now
	if err := s.writeKnowledgeLocked(knowledge); err != nil {
		return err
	}

	project := novel.Project{
		Title:     legacy.Title,
		Revision:  1,
		UpdatedAt: now,
		Analysis: novel.AnalysisState{
			Status:    novel.AnalysisIdle,
			UpdatedAt: now,
		},
	}
	for _, chapter := range chapters {
		project.Chapters = append(project.Chapters, chapter.Meta())
	}
	if len(chapters) > 0 {
		project.Analysis.Status = novel.AnalysisPending
		project.Analysis.TotalCount = len(chapters)
	}
	project.Normalize()
	if err := s.writeProjectLocked(project); err != nil {
		return err
	}
	return nil
}

func truncate(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "…"
}

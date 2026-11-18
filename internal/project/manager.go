package project

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"

	"novelagent/internal/agentruntime"
	"novelagent/internal/analysis"
	"novelagent/internal/store"
)

const (
	DefaultID   = "default"
	defaultName = "我的作品"
)

var (
	ErrProjectNotFound = errors.New("project: not found")
	ErrInvalidProject  = errors.New("project: invalid id or name")
	ErrDefaultProject  = errors.New("project: default project cannot be deleted")
	projectIDPattern   = regexp.MustCompile(`^[a-z][a-z0-9_-]{3,31}$`)
)

// Info 是项目列表的公开元数据。
type Info struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Title         string    `json:"title"`
	Dir           string    `json:"dir,omitempty"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
	ChapterCount  int       `json:"chapterCount"`
	AnalysisState string    `json:"analysisState"`
}

// Runtime 为单项目运行时：独立 Store、分析器、提取器与 Agent 总线。
type Runtime struct {
	ID        string
	Dir       string
	Store     *store.Store
	Analyzer  *analysis.Manager
	Extractor *analysis.ExtractLauncher
	Agents    *agentruntime.Runtime
}

// Manager 管理项目索引与按项目隔离的运行时。
//
// 目录约定：root 本身是默认项目的数据目录（兼容旧版单项目数据，无需搬迁）；
// 新项目位于 root/projects/<id>/；索引为 root/projects.db。
type Manager struct {
	root string
	log  *slog.Logger
	db   *sql.DB

	mu       sync.Mutex
	runtimes map[string]*Runtime
	ctx      context.Context
	started  bool
	closed   bool
}

func NewManager(root string, logger *slog.Logger) (*Manager, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("project: data directory is empty")
	}
	if logger == nil {
		logger = slog.Default()
	}
	if err := os.MkdirAll(filepath.Join(root, "projects"), 0o755); err != nil {
		return nil, fmt.Errorf("project: create projects directory: %w", err)
	}
	dbPath := filepath.Join(root, "projects.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("project: open index: %w", err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		db.Close()
		return nil, fmt.Errorf("project: enable WAL: %w", err)
	}
	if _, err := db.Exec(`PRAGMA busy_timeout=5000`); err != nil {
		db.Close()
		return nil, fmt.Errorf("project: set busy timeout: %w", err)
	}
	m := &Manager{
		root:     root,
		log:      logger,
		db:       db,
		runtimes: make(map[string]*Runtime),
	}
	if err := m.initIndex(); err != nil {
		db.Close()
		return nil, err
	}
	if err := m.ensureDefault(); err != nil {
		db.Close()
		return nil, err
	}
	return m, nil
}

func (m *Manager) initIndex() error {
	_, err := m.db.Exec(`
CREATE TABLE IF NOT EXISTS projects (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	dir TEXT NOT NULL,
	created_at INTEGER NOT NULL,
	updated_at INTEGER NOT NULL
)`)
	if err != nil {
		return fmt.Errorf("project: init index: %w", err)
	}
	return nil
}

func (m *Manager) ensureDefault() error {
	var count int
	if err := m.db.QueryRow(`SELECT COUNT(*) FROM projects WHERE id = ?`, DefaultID).Scan(&count); err != nil {
		return fmt.Errorf("project: check default: %w", err)
	}
	if count > 0 {
		return nil
	}
	name := defaultName
	if legacy, err := store.New(m.root); err == nil {
		if project, err := legacy.LoadProject(); err == nil && strings.TrimSpace(project.Title) != "" {
			name = strings.TrimSpace(project.Title)
		}
	}
	now := time.Now().UTC()
	_, err := m.db.Exec(
		`INSERT INTO projects (id, name, dir, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		DefaultID, name, ".", now.Unix(), now.Unix(),
	)
	if err != nil {
		return fmt.Errorf("project: create default: %w", err)
	}
	return nil
}

// Start 预加载所有项目运行时并启动各自的分析与 Agent 总线。
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	if m.started {
		m.mu.Unlock()
		return nil
	}
	m.ctx = ctx
	m.started = true
	m.mu.Unlock()

	infos, err := m.listRows()
	if err != nil {
		return err
	}
	for _, info := range infos {
		rt, err := m.runtimeFor(info.ID)
		if err != nil {
			m.log.Error("start project runtime", "project", info.ID, "err", err)
			continue
		}
		// runtimeFor 对已缓存的项目直接返回；启动时仍需显式拉起其分析器与 Agent 总线
		// （Start 均为幂等，重复调用安全）。
		rt.Analyzer.Start(ctx)
		rt.Agents.Start(ctx)
	}
	return nil
}

func (m *Manager) Stop() {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.closed = true
	m.started = false
	m.ctx = nil
	runtimes := make([]*Runtime, 0, len(m.runtimes))
	for _, rt := range m.runtimes {
		runtimes = append(runtimes, rt)
	}
	m.runtimes = map[string]*Runtime{}
	m.mu.Unlock()

	for _, rt := range runtimes {
		rt.Agents.Stop()
		rt.Analyzer.Stop()
	}
	_ = m.db.Close()
}

func (m *Manager) List() ([]Info, error) {
	rows, err := m.listRows()
	if err != nil {
		return nil, err
	}
	out := make([]Info, 0, len(rows))
	for _, info := range rows {
		rt, err := m.runtimeFor(info.ID)
		if err != nil {
			m.log.Warn("load project metadata", "project", info.ID, "err", err)
			out = append(out, info)
			continue
		}
		if project, err := rt.Store.LoadProject(); err == nil {
			info.Title = project.Title
			info.ChapterCount = len(project.Chapters)
			info.AnalysisState = string(project.Analysis.Status)
			if !project.UpdatedAt.IsZero() {
				info.UpdatedAt = project.UpdatedAt
			}
		}
		out = append(out, info)
	}
	return out, nil
}

func (m *Manager) listRows() ([]Info, error) {
	rows, err := m.db.Query(`SELECT id, name, dir, created_at, updated_at FROM projects
ORDER BY CASE WHEN id = ? THEN 0 ELSE 1 END, updated_at DESC`, DefaultID)
	if err != nil {
		return nil, fmt.Errorf("project: list: %w", err)
	}
	defer rows.Close()
	var out []Info
	for rows.Next() {
		var (
			info              Info
			created, modified int64
		)
		if err := rows.Scan(&info.ID, &info.Name, &info.Dir, &created, &modified); err != nil {
			return nil, fmt.Errorf("project: scan: %w", err)
		}
		info.CreatedAt = time.Unix(created, 0).UTC()
		info.UpdatedAt = time.Unix(modified, 0).UTC()
		out = append(out, info)
	}
	return out, rows.Err()
}

func (m *Manager) Get(id string) (Info, error) {
	if id == "" {
		id = DefaultID
	}
	if !projectIDPattern.MatchString(id) {
		return Info{}, ErrInvalidProject
	}
	var (
		info              Info
		created, modified int64
	)
	err := m.db.QueryRow(
		`SELECT id, name, dir, created_at, updated_at FROM projects WHERE id = ?`, id,
	).Scan(&info.ID, &info.Name, &info.Dir, &created, &modified)
	if errors.Is(err, sql.ErrNoRows) {
		return Info{}, ErrProjectNotFound
	}
	if err != nil {
		return Info{}, fmt.Errorf("project: get: %w", err)
	}
	info.CreatedAt = time.Unix(created, 0).UTC()
	info.UpdatedAt = time.Unix(modified, 0).UTC()
	if rt, err := m.runtimeFor(id); err == nil {
		if project, err := rt.Store.LoadProject(); err == nil {
			info.Title = project.Title
			info.ChapterCount = len(project.Chapters)
			info.AnalysisState = string(project.Analysis.Status)
		}
	}
	return info, nil
}

func (m *Manager) Create(name string) (Info, error) {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 120 {
		return Info{}, ErrInvalidProject
	}
	id := newProjectID()
	dir := filepath.Join(m.root, "projects", id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Info{}, fmt.Errorf("project: create directory: %w", err)
	}
	st, err := store.New(dir)
	if err != nil {
		_ = os.RemoveAll(dir)
		return Info{}, fmt.Errorf("project: init store: %w", err)
	}
	if _, err := st.UpdateTitle(name); err != nil {
		_ = os.RemoveAll(dir)
		return Info{}, fmt.Errorf("project: init title: %w", err)
	}
	now := time.Now().UTC()
	_, err = m.db.Exec(
		`INSERT INTO projects (id, name, dir, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		id, name, filepath.ToSlash(filepath.Join("projects", id)), now.Unix(), now.Unix(),
	)
	if err != nil {
		_ = os.RemoveAll(dir)
		return Info{}, fmt.Errorf("project: insert: %w", err)
	}
	info := Info{
		ID:        id,
		Name:      name,
		Title:     name,
		Dir:       filepath.ToSlash(filepath.Join("projects", id)),
		CreatedAt: now,
		UpdatedAt: now,
	}
	if _, err := m.runtimeFor(id); err != nil {
		m.log.Error("start new project runtime", "project", id, "err", err)
	}
	return info, nil
}

func (m *Manager) Rename(id, name string) (Info, error) {
	name = strings.TrimSpace(name)
	if id == "" || !projectIDPattern.MatchString(id) || name == "" || len([]rune(name)) > 120 {
		return Info{}, ErrInvalidProject
	}
	rt, err := m.runtimeFor(id)
	if err != nil {
		return Info{}, err
	}
	if _, err := rt.Store.UpdateTitle(name); err != nil {
		return Info{}, fmt.Errorf("project: rename store: %w", err)
	}
	now := time.Now().UTC()
	res, err := m.db.Exec(`UPDATE projects SET name = ?, updated_at = ? WHERE id = ?`, name, now.Unix(), id)
	if err != nil {
		return Info{}, fmt.Errorf("project: rename: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return Info{}, ErrProjectNotFound
	}
	return m.Get(id)
}

func (m *Manager) Delete(id string) error {
	if id == DefaultID {
		return ErrDefaultProject
	}
	if id == "" || !projectIDPattern.MatchString(id) {
		return ErrInvalidProject
	}
	var dir string
	if err := m.db.QueryRow(`SELECT dir FROM projects WHERE id = ?`, id).Scan(&dir); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrProjectNotFound
		}
		return fmt.Errorf("project: delete lookup: %w", err)
	}
	m.mu.Lock()
	rt := m.runtimes[id]
	delete(m.runtimes, id)
	m.mu.Unlock()
	if rt != nil {
		rt.Agents.Stop()
		rt.Analyzer.Stop()
	}
	if _, err := m.db.Exec(`DELETE FROM projects WHERE id = ?`, id); err != nil {
		return fmt.Errorf("project: delete: %w", err)
	}
	target := filepath.Join(m.root, filepath.FromSlash(dir))
	rel, err := filepath.Rel(filepath.Join(m.root, "projects"), target)
	if err != nil || strings.HasPrefix(rel, "..") || rel == "." {
		return fmt.Errorf("project: refuse to delete unsafe directory %q", dir)
	}
	if err := os.RemoveAll(target); err != nil {
		return fmt.Errorf("project: remove data: %w", err)
	}
	return nil
}

// Runtime 返回项目运行时；首次访问时惰性初始化并启动。
func (m *Manager) Runtime(id string) (*Runtime, error) {
	if id == "" {
		id = DefaultID
	}
	return m.runtimeFor(id)
}

func (m *Manager) runtimeFor(id string) (*Runtime, error) {
	m.mu.Lock()
	if rt, ok := m.runtimes[id]; ok {
		m.mu.Unlock()
		return rt, nil
	}
	ctx := m.ctx
	m.mu.Unlock()

	var dir string
	if err := m.db.QueryRow(`SELECT dir FROM projects WHERE id = ?`, id).Scan(&dir); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrProjectNotFound
		}
		return nil, fmt.Errorf("project: runtime lookup: %w", err)
	}
	absDir := m.root
	if dir != "." {
		absDir = filepath.Join(m.root, filepath.FromSlash(dir))
	}
	st, err := store.New(absDir)
	if err != nil {
		return nil, err
	}
	agents, err := agentruntime.New(id, st, absDir, m.log)
	if err != nil {
		return nil, err
	}
	rt := &Runtime{
		ID:        id,
		Dir:       absDir,
		Store:     st,
		Analyzer:  analysis.NewManager(st, m.log),
		Extractor: analysis.NewExtractLauncher(st, m.log),
		Agents:    agents,
	}

	m.mu.Lock()
	if existing, ok := m.runtimes[id]; ok {
		m.mu.Unlock()
		agents.Stop()
		return existing, nil
	}
	m.runtimes[id] = rt
	if m.started && ctx != nil {
		rt.Analyzer.Start(ctx)
		rt.Agents.Start(ctx)
	}
	m.mu.Unlock()
	return rt, nil
}

func newProjectID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("p_%d", time.Now().UnixNano())
	}
	return "p_" + hex.EncodeToString(b[:])
}

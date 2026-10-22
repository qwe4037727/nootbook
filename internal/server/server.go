package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"novelagent/internal/agent"
	"novelagent/internal/llm"
	"novelagent/internal/novel"
	"novelagent/internal/store"
)

type AnalysisNotifier interface {
	Notify()
	Retry()
}

type Server struct {
	store    *store.Store
	static   fs.FS
	log      *slog.Logger
	analyzer AnalysisNotifier
}

func New(st *store.Store, static fs.FS, logger *slog.Logger, analyzer AnalysisNotifier) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	s := &Server{store: st, static: static, log: logger, analyzer: analyzer}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/project", s.handleGetProject)
	mux.HandleFunc("PUT /api/project", s.handlePutProject)
	mux.HandleFunc("POST /api/import", s.handleImport)
	mux.HandleFunc("GET /api/chapters/{id}", s.handleGetChapter)
	mux.HandleFunc("PUT /api/chapters/{id}", s.handleUpdateChapter)
	mux.HandleFunc("DELETE /api/chapters/{id}", s.handleDeleteChapter)
	mux.HandleFunc("POST /api/chapters", s.handleCreateChapter)
	mux.HandleFunc("POST /api/analyze", s.handleAnalyze)
	mux.HandleFunc("GET /api/config", s.handleGetConfig)
	mux.HandleFunc("PUT /api/config", s.handlePutConfig)
	mux.HandleFunc("POST /api/continue", s.handleContinue)
	mux.Handle("/", http.FileServer(http.FS(static)))

	return withHeaders(mux)
}

func withHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; font-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'")
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			h.Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

type publicProject struct {
	Title     string              `json:"title"`
	Chapters  []novel.ChapterMeta `json:"chapters"`
	Analysis  novel.AnalysisState `json:"analysis"`
	UpdatedAt time.Time           `json:"updatedAt"`
}

func publicProjectOf(project novel.Project) publicProject {
	chapters := project.Chapters
	if chapters == nil {
		chapters = []novel.ChapterMeta{}
	}
	return publicProject{
		Title:     project.Title,
		Chapters:  chapters,
		Analysis:  project.Analysis,
		UpdatedAt: project.UpdatedAt,
	}
}

func (s *Server) handleGetProject(w http.ResponseWriter, r *http.Request) {
	project, err := s.store.LoadProject()
	if err != nil {
		s.log.Error("load project", "err", err)
		writeError(w, http.StatusInternalServerError, "读取作品失败")
		return
	}
	writeJSON(w, http.StatusOK, publicProjectOf(project))
}

type projectRequest struct {
	Title string `json:"title"`
}

func (s *Server) handlePutProject(w http.ResponseWriter, r *http.Request) {
	var req projectRequest
	if err := decodeJSON(w, r, &req, 64<<10); err != nil {
		writeError(w, http.StatusBadRequest, "作品信息格式不正确")
		return
	}
	project, err := s.store.UpdateTitle(req.Title)
	if err != nil {
		s.log.Error("update project title", "err", err)
		writeError(w, http.StatusInternalServerError, "保存作品失败")
		return
	}
	writeJSON(w, http.StatusOK, publicProjectOf(project))
}

const maxImportChars = 5_000_000

type importRequest struct {
	Title string `json:"title"`
	Text  string `json:"text"`
}

type importResponse struct {
	Project          publicProject `json:"project"`
	ImportedChapters int           `json:"importedChapters"`
	ImportedChars    int           `json:"importedChars"`
}

func (s *Server) handleImport(w http.ResponseWriter, r *http.Request) {
	var req importRequest
	if err := decodeJSON(w, r, &req, 32<<20); err != nil {
		writeError(w, http.StatusBadRequest, "文稿格式不正确或文件过大")
		return
	}
	if strings.TrimSpace(req.Text) == "" {
		writeError(w, http.StatusBadRequest, "请先选择或粘贴要导入的文稿")
		return
	}
	if utf8.RuneCountInString(req.Text) > maxImportChars {
		writeError(w, http.StatusBadRequest, "文稿太大，请控制在 500 万字以内")
		return
	}

	chapters := novel.SplitChapters(req.Text, "正文")
	if len(chapters) == 0 {
		writeError(w, http.StatusBadRequest, "没有找到可导入的正文")
		return
	}

	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = novel.InferTitle(req.Text)
	}
	if title == "" {
		if current, err := s.store.LoadProject(); err == nil && current.Title != "" && current.Title != "未命名小说" {
			title = current.Title
		}
	}
	if title == "" {
		title = "未命名小说"
	}

	project, err := s.store.ImportChapters(title, chapters)
	if err != nil {
		s.log.Error("import chapters", "err", err)
		writeError(w, http.StatusInternalServerError, "导入文稿失败")
		return
	}
	s.notifyAnalysis()

	total := 0
	for _, chapter := range project.Chapters {
		total += chapter.CharCount
	}
	writeJSON(w, http.StatusOK, importResponse{
		Project:          publicProjectOf(project),
		ImportedChapters: len(project.Chapters),
		ImportedChars:    total,
	})
}

func (s *Server) handleGetChapter(w http.ResponseWriter, r *http.Request) {
	chapter, err := s.store.LoadChapter(r.PathValue("id"))
	if err != nil {
		s.writeChapterError(w, err, "读取章节失败")
		return
	}
	writeJSON(w, http.StatusOK, chapter)
}

type chapterRequest struct {
	Title   string `json:"title"`
	Content string `json:"content"`
}

type chapterResponse struct {
	Chapter novel.Chapter `json:"chapter"`
	Project publicProject `json:"project"`
}

func (s *Server) handleUpdateChapter(w http.ResponseWriter, r *http.Request) {
	var req chapterRequest
	if err := decodeJSON(w, r, &req, 16<<20); err != nil {
		writeError(w, http.StatusBadRequest, "章节内容格式不正确或文件过大")
		return
	}
	existing, err := s.store.LoadChapter(r.PathValue("id"))
	if err != nil {
		s.writeChapterError(w, err, "读取章节失败")
		return
	}
	existing.Title = req.Title
	existing.Content = req.Content

	chapter, project, err := s.store.SaveChapter(existing)
	if err != nil {
		s.writeChapterError(w, err, "保存章节失败")
		return
	}
	s.notifyAnalysis()
	writeJSON(w, http.StatusOK, chapterResponse{Chapter: chapter, Project: publicProjectOf(project)})
}

func (s *Server) handleCreateChapter(w http.ResponseWriter, r *http.Request) {
	var req chapterRequest
	if err := decodeJSON(w, r, &req, 16<<20); err != nil {
		writeError(w, http.StatusBadRequest, "章节内容格式不正确或文件过大")
		return
	}
	chapter, project, err := s.store.AppendChapter(req.Title, req.Content)
	if err != nil {
		s.log.Error("append chapter", "err", err)
		writeError(w, http.StatusInternalServerError, "新增章节失败")
		return
	}
	s.notifyAnalysis()
	writeJSON(w, http.StatusOK, chapterResponse{Chapter: chapter, Project: publicProjectOf(project)})
}

func (s *Server) handleDeleteChapter(w http.ResponseWriter, r *http.Request) {
	project, err := s.store.DeleteChapter(r.PathValue("id"))
	if err != nil {
		s.writeChapterError(w, err, "删除章节失败")
		return
	}
	s.notifyAnalysis()
	writeJSON(w, http.StatusOK, map[string]any{"project": publicProjectOf(project)})
}

func (s *Server) handleAnalyze(w http.ResponseWriter, r *http.Request) {
	if s.analyzer == nil {
		writeError(w, http.StatusServiceUnavailable, "分析服务未启用")
		return
	}
	s.analyzer.Retry()
	project, err := s.store.LoadProject()
	if err != nil {
		s.log.Error("load project after analysis retry", "err", err)
		writeError(w, http.StatusInternalServerError, "读取作品失败")
		return
	}
	writeJSON(w, http.StatusOK, publicProjectOf(project))
}

func (s *Server) writeChapterError(w http.ResponseWriter, err error, fallback string) {
	switch {
	case errors.Is(err, store.ErrChapterNotFound), errors.Is(err, store.ErrInvalidChapter):
		writeError(w, http.StatusNotFound, "章节不存在")
	default:
		s.log.Error("chapter request failed", "err", err)
		writeError(w, http.StatusInternalServerError, fallback)
	}
}

func (s *Server) notifyAnalysis() {
	if s.analyzer != nil {
		s.analyzer.Notify()
	}
}

func (s *Server) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.store.LoadConfig()
	if err != nil {
		s.log.Error("load config", "err", err)
		writeError(w, http.StatusInternalServerError, "读取模型设置失败")
		return
	}
	writeJSON(w, http.StatusOK, publicConfigOf(cfg))
}

type configRequest struct {
	BaseURL      string  `json:"baseUrl"`
	Model        string  `json:"model"`
	APIKey       *string `json:"apiKey"`
	ClearAPIKey  bool    `json:"clearApiKey"`
	ContextChars int     `json:"contextChars"`
}

func (s *Server) handlePutConfig(w http.ResponseWriter, r *http.Request) {
	var req configRequest
	if err := decodeJSON(w, r, &req, 64<<10); err != nil {
		writeError(w, http.StatusBadRequest, "设置格式不正确")
		return
	}

	cfg, err := s.store.LoadConfig()
	if err != nil {
		s.log.Error("load config", "err", err)
		writeError(w, http.StatusInternalServerError, "读取模型设置失败")
		return
	}

	cfg.BaseURL = strings.TrimRight(strings.TrimSpace(req.BaseURL), "/")
	cfg.Model = strings.TrimSpace(req.Model)
	cfg.ContextChars = req.ContextChars

	if req.ClearAPIKey {
		cfg.APIKey = ""
	}
	if req.APIKey != nil {
		if key := strings.TrimSpace(*req.APIKey); key != "" {
			cfg.APIKey = key
		}
	}

	if cfg.BaseURL != "" {
		u, err := url.Parse(cfg.BaseURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			writeError(w, http.StatusBadRequest, "Base URL 需要是 http:// 或 https:// 地址")
			return
		}
	}

	saved, err := s.store.SaveConfig(cfg)
	if err != nil {
		s.log.Error("save config", "err", err)
		writeError(w, http.StatusInternalServerError, "保存模型设置失败")
		return
	}
	if s.analyzer != nil {
		s.analyzer.Retry()
	}
	writeJSON(w, http.StatusOK, publicConfigOf(saved))
}

type publicConfig struct {
	BaseURL       string `json:"baseUrl"`
	Model         string `json:"model"`
	ContextChars  int    `json:"contextChars"`
	HasAPIKey     bool   `json:"hasApiKey"`
	APIKeyHint    string `json:"apiKeyHint"`
	APIKeyFromEnv bool   `json:"apiKeyFromEnv"`
}

func publicConfigOf(cfg store.Config) publicConfig {
	key, fromEnv := cfg.EffectiveAPIKey()
	pub := publicConfig{
		BaseURL:       cfg.BaseURL,
		Model:         cfg.Model,
		ContextChars:  cfg.ContextChars,
		HasAPIKey:     key != "",
		APIKeyFromEnv: fromEnv,
	}
	switch {
	case fromEnv:
		pub.APIKeyHint = "环境变量 OPENAI_API_KEY"
	case key != "":
		pub.APIKeyHint = maskKey(key)
	}
	return pub
}

func maskKey(key string) string {
	if len(key) <= 8 {
		return "已保存"
	}
	return key[:3] + "…" + key[len(key)-4:]
}

type continueRequest struct {
	Direction   string  `json:"direction"`
	TargetChars int     `json:"targetChars"`
	Temperature float64 `json:"temperature"`
}

type sseWriter struct {
	w http.ResponseWriter
	f http.Flusher
}

func (s *sseWriter) send(event string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", event, data); err != nil {
		return err
	}
	s.f.Flush()
	return nil
}

func (s *Server) handleContinue(w http.ResponseWriter, r *http.Request) {
	var req continueRequest
	if err := decodeJSON(w, r, &req, 64<<10); err != nil {
		writeError(w, http.StatusBadRequest, "续写请求格式不正确")
		return
	}

	req.Direction = strings.TrimSpace(req.Direction)
	if utf8.RuneCountInString(req.Direction) > 2000 {
		writeError(w, http.StatusBadRequest, "续写要求太长，请控制在 2000 字以内")
		return
	}
	if req.TargetChars <= 0 {
		req.TargetChars = 800
	}
	req.TargetChars = min(max(req.TargetChars, 200), 4000)
	if req.Temperature <= 0 {
		req.Temperature = 0.85
	}
	req.Temperature = min(req.Temperature, 1.5)

	project, err := s.store.LoadProject()
	if err != nil {
		s.log.Error("load project for continuation", "err", err)
		writeError(w, http.StatusInternalServerError, "读取作品失败")
		return
	}
	chapters, err := s.store.LoadChapters()
	if err != nil {
		s.log.Error("load chapters for continuation", "err", err)
		writeError(w, http.StatusInternalServerError, "读取章节失败")
		return
	}
	hasContent := false
	for _, chapter := range chapters {
		if strings.TrimSpace(chapter.Content) != "" {
			hasContent = true
			break
		}
	}
	if !hasContent {
		writeError(w, http.StatusBadRequest, "请先导入文稿或填写正文，再开始续写")
		return
	}

	cfg, err := s.store.LoadConfig()
	if err != nil {
		s.log.Error("load config for continuation", "err", err)
		writeError(w, http.StatusInternalServerError, "读取模型设置失败")
		return
	}
	if cfg.BaseURL == "" || cfg.Model == "" {
		writeError(w, http.StatusBadRequest, "请先在「模型设置」中填写 Base URL 和模型名")
		return
	}

	knowledge, err := s.store.LoadKnowledge()
	if err != nil {
		s.log.Error("load knowledge for continuation", "err", err)
		writeError(w, http.StatusInternalServerError, "读取知识库失败")
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "当前服务不支持流式输出")
		return
	}

	prompt := agent.Build(agent.Input{
		Title:        project.Title,
		Knowledge:    knowledge,
		Chapters:     chapters,
		Direction:    req.Direction,
		TargetChars:  req.TargetChars,
		ContextChars: cfg.ContextChars,
	})

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	sw := &sseWriter{w: w, f: flusher}
	if err := sw.send("meta", map[string]any{
		"contextChars":     prompt.ContextChars,
		"truncated":        prompt.Truncated,
		"characters":       prompt.Characters,
		"worldEntries":     prompt.WorldEntries,
		"summaries":        prompt.Summaries,
		"analysisStatus":   project.Analysis.Status,
		"analyzedChapters": project.Analysis.AnalyzedCount,
		"totalChapters":    project.Analysis.TotalCount,
	}); err != nil {
		return
	}

	key, _ := cfg.EffectiveAPIKey()
	client := llm.NewClient(cfg.BaseURL, key, cfg.Model)

	var full strings.Builder
	err = client.StreamChat(
		r.Context(),
		[]llm.Message{
			{Role: "system", Content: prompt.System},
			{Role: "user", Content: prompt.User},
		},
		req.Temperature,
		func(delta string) error {
			full.WriteString(delta)
			return sw.send("delta", map[string]string{"text": delta})
		},
	)
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		s.log.Error("continuation failed", "err", err, "model", cfg.Model)
		_ = sw.send("error", map[string]string{"error": "模型调用失败：" + shortError(err)})
		return
	}

	if strings.TrimSpace(full.String()) == "" {
		_ = sw.send("error", map[string]string{"error": "模型没有返回内容，请检查模型设置或稍后重试"})
		return
	}

	_ = sw.send("done", map[string]any{
		"chars": utf8.RuneCountInString(strings.TrimSpace(full.String())),
	})
}

func shortError(err error) string {
	const limit = 600
	s := err.Error()
	if utf8.RuneCountInString(s) <= limit {
		return s
	}
	runes := []rune(s)
	return string(runes[:limit]) + "…"
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any, limit int64) error {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

package app

import (
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"novelagent/internal/agentruntime"
	"novelagent/internal/project"
	"novelagent/internal/server"
)

const (
	ProjectHeader  = "X-Project-ID"
	ProjectQuery   = "projectId"
	ProjectCookie  = "project_id"
	DefaultProject = project.DefaultID
)

// Router 按项目解析请求，并把项目级 API 交给独立的 server.Handler。
type Router struct {
	manager *project.Manager
	static  fs.FS
	log     *slog.Logger
	mux     *http.ServeMux

	mu       sync.Mutex
	handlers map[string]http.Handler
}

func NewRouter(manager *project.Manager, static fs.FS, logger *slog.Logger) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	r := &Router{
		manager:  manager,
		static:   static,
		log:      logger,
		handlers: make(map[string]http.Handler),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/projects", r.handleListProjects)
	mux.HandleFunc("POST /api/projects", r.handleCreateProject)
	mux.HandleFunc("PATCH /api/projects/{id}", r.handleRenameProject)
	mux.HandleFunc("DELETE /api/projects/{id}", r.handleDeleteProject)
	mux.HandleFunc("GET /api/agents/runs", r.handleAgentRuns)
	r.mux = mux
	return r
}

func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	switch {
	case strings.HasPrefix(req.URL.Path, "/api/projects"):
		w.Header().Set("Cache-Control", "no-store")
		r.mux.ServeHTTP(w, req)
		return
	case req.URL.Path == "/api/agents/runs":
		w.Header().Set("Cache-Control", "no-store")
		r.mux.ServeHTTP(w, req)
		return
	}

	id, explicit := r.resolveProjectID(req)
	rt, err := r.manager.Runtime(id)
	if err != nil {
		if explicit {
			writeError(w, http.StatusNotFound, "作品不存在或已被删除")
			return
		}
		id = DefaultProject
		rt, err = r.manager.Runtime(id)
		if err != nil {
			r.log.Error("load default project runtime", "err", err)
			writeError(w, http.StatusInternalServerError, "读取作品失败")
			return
		}
	}
	handler, err := r.handlerFor(id, rt)
	if err != nil {
		r.log.Error("build project handler", "project", id, "err", err)
		writeError(w, http.StatusInternalServerError, "读取作品失败")
		return
	}
	handler.ServeHTTP(w, req)
}

func (r *Router) handlerFor(id string, rt *project.Runtime) (http.Handler, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if h, ok := r.handlers[id]; ok {
		return h, nil
	}
	h := server.New(rt.Store, r.static, r.log, rt.Analyzer, rt.Extractor)
	r.handlers[id] = h
	return h, nil
}

func (r *Router) resolveProjectID(req *http.Request) (string, bool) {
	if id := strings.TrimSpace(req.Header.Get(ProjectHeader)); id != "" {
		return id, true
	}
	if id := strings.TrimSpace(req.URL.Query().Get(ProjectQuery)); id != "" {
		return id, true
	}
	if cookie, err := req.Cookie(ProjectCookie); err == nil {
		if id := strings.TrimSpace(cookie.Value); id != "" {
			return id, false
		}
	}
	return DefaultProject, false
}

type projectListResponse struct {
	Projects  []project.Info `json:"projects"`
	DefaultID string         `json:"defaultId"`
	CurrentID string         `json:"currentId"`
}

func (r *Router) handleListProjects(w http.ResponseWriter, req *http.Request) {
	list, err := r.manager.List()
	if err != nil {
		r.log.Error("list projects", "err", err)
		writeError(w, http.StatusInternalServerError, "读取作品列表失败")
		return
	}
	current, _ := r.resolveProjectID(req)
	found := false
	for _, info := range list {
		if info.ID == current {
			found = true
			break
		}
	}
	if !found {
		current = DefaultProject
	}
	writeJSON(w, http.StatusOK, projectListResponse{
		Projects:  list,
		DefaultID: DefaultProject,
		CurrentID: current,
	})
}

type projectRequest struct {
	Name string `json:"name"`
}

func (r *Router) handleCreateProject(w http.ResponseWriter, req *http.Request) {
	req.Body = http.MaxBytesReader(w, req.Body, 64<<10)
	var body projectRequest
	dec := json.NewDecoder(req.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "作品名称格式不正确")
		return
	}
	info, err := r.manager.Create(body.Name)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, project.ErrInvalidProject) {
			status = http.StatusBadRequest
		}
		r.log.Error("create project", "err", err)
		writeError(w, status, "新建作品失败，请检查名称")
		return
	}
	writeJSON(w, http.StatusOK, info)
}

func (r *Router) handleRenameProject(w http.ResponseWriter, req *http.Request) {
	req.Body = http.MaxBytesReader(w, req.Body, 64<<10)
	var body projectRequest
	dec := json.NewDecoder(req.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "作品名称格式不正确")
		return
	}
	info, err := r.manager.Rename(req.PathValue("id"), body.Name)
	if err != nil {
		status := http.StatusInternalServerError
		switch {
		case errors.Is(err, project.ErrProjectNotFound):
			status = http.StatusNotFound
		case errors.Is(err, project.ErrInvalidProject):
			status = http.StatusBadRequest
		}
		r.log.Error("rename project", "err", err)
		writeError(w, status, "重命名失败")
		return
	}
	writeJSON(w, http.StatusOK, info)
}

func (r *Router) handleDeleteProject(w http.ResponseWriter, req *http.Request) {
	id := req.PathValue("id")
	if err := r.manager.Delete(id); err != nil {
		status := http.StatusInternalServerError
		switch {
		case errors.Is(err, project.ErrDefaultProject):
			status = http.StatusConflict
		case errors.Is(err, project.ErrProjectNotFound):
			status = http.StatusNotFound
		case errors.Is(err, project.ErrInvalidProject):
			status = http.StatusBadRequest
		}
		r.log.Error("delete project", "project", id, "err", err)
		writeError(w, status, "删除作品失败")
		return
	}
	r.mu.Lock()
	delete(r.handlers, id)
	r.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true, "id": id})
}

type agentRunsResponse struct {
	ProjectID string                   `json:"projectId"`
	Runs      []agentruntime.RunRecord `json:"runs"`
}

func (r *Router) handleAgentRuns(w http.ResponseWriter, req *http.Request) {
	id, explicit := r.resolveProjectID(req)
	rt, err := r.manager.Runtime(id)
	if err != nil {
		if explicit {
			writeError(w, http.StatusNotFound, "作品不存在或已被删除")
			return
		}
		id = DefaultProject
		rt, err = r.manager.Runtime(id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "读取 Agent 记录失败")
			return
		}
	}
	limit := 20
	if raw := strings.TrimSpace(req.URL.Query().Get("limit")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}
	writeJSON(w, http.StatusOK, agentRunsResponse{
		ProjectID: id,
		Runs:      rt.Agents.RecentRuns(limit),
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

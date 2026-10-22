package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"novelagent/internal/store"
)

// 记忆 API（docs/memory_model_spec.md §12 基线，任务 #8）。

func (s *Server) registerMemoryRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/memory", s.handleGetMemory)
	mux.HandleFunc("GET /api/memory/initialize/status", s.handleGetMemoryInitStatus)
	mux.HandleFunc("POST /api/memory/initialize", s.handlePostMemoryInitialize)
	mux.HandleFunc("GET /api/memory/characters/{id}", s.handleGetCharacterMemory)
}

// writeMemoryError 带机器可读错误码的响应（§12.3 project_not_migrated 等）。
func writeMemoryError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]string{"error": message, "code": code})
}

// handleGetMemory 覆盖 §12.1：概览（counts/revision/initStatus）；文件不存在 → 全 0 + not_started（U5）。
func (s *Server) handleGetMemory(w http.ResponseWriter, r *http.Request) {
	overview, err := s.store.LoadMemoryOverview()
	if err != nil {
		s.log.Error("load memory overview", "err", err)
		writeError(w, http.StatusInternalServerError, "读取记忆概览失败")
		return
	}
	writeJSON(w, http.StatusOK, overview)
}

// handleGetMemoryInitStatus 覆盖 §12.4。
func (s *Server) handleGetMemoryInitStatus(w http.ResponseWriter, r *http.Request) {
	state, err := s.store.LoadMemoryInitState()
	if err != nil {
		s.log.Error("load memory init state", "err", err)
		writeError(w, http.StatusInternalServerError, "读取初始化状态失败")
		return
	}
	writeJSON(w, http.StatusOK, state)
}

// handlePostMemoryInitialize 覆盖 §12.3/§9.3 三种重复请求语义：
//   - 202 mapping（首次/失败后续跑，异步执行）
//   - 200 ready + alreadyInitialized（幂等重试，无副作用，U30）
//   - 409 already_running / project_not_migrated / knowledge_not_ready
//   - 400 reinit_not_supported（显式全量重建；先于状态检查、零落盘）
func (s *Server) handlePostMemoryInitialize(w http.ResponseWriter, r *http.Request) {
	const requestLimit = 4096
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, requestLimit))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeMemoryError(w, http.StatusRequestEntityTooLarge, "request_too_large", "初始化请求不能超过 4096 字节")
		} else {
			writeMemoryError(w, http.StatusBadRequest, "invalid_request", "无法读取初始化请求")
		}
		return
	}
	if len(bytes.TrimSpace(raw)) > 0 {
		var request *struct {
			Reinitialize json.RawMessage `json:"reinitialize"`
		}
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields()
		var extra any
		if err := dec.Decode(&request); err != nil || request == nil || dec.Decode(&extra) != io.EOF {
			writeMemoryError(w, http.StatusBadRequest, "invalid_request", "初始化请求必须为单个 JSON 对象，仅支持 reinitialize 布尔字段")
			return
		}
		value := bytes.TrimSpace(request.Reinitialize)
		if len(value) > 0 && !bytes.Equal(value, []byte("true")) && !bytes.Equal(value, []byte("false")) {
			writeMemoryError(w, http.StatusBadRequest, "invalid_request", "reinitialize 必须为布尔值")
			return
		}
		if bytes.Equal(value, []byte("true")) {
			writeMemoryError(w, http.StatusBadRequest, "reinit_not_supported", "本版本不支持全量重新初始化")
			return
		}
	}
	phase, err := s.store.InitializeMemory()
	switch {
	case err == nil:
		writeJSON(w, http.StatusAccepted, map[string]string{"initStatus": phase})
	case errors.Is(err, store.ErrAlreadyInitialized):
		writeJSON(w, http.StatusOK, map[string]any{"initStatus": phase, "alreadyInitialized": true})
	case errors.Is(err, store.ErrAlreadyRunning):
		writeMemoryError(w, http.StatusConflict, "already_running", "记忆初始化正在进行中")
	case errors.Is(err, store.ErrProjectNotMigrated):
		writeMemoryError(w, http.StatusConflict, "project_not_migrated", "项目尚未迁移：请先启动服务自动完成迁移")
	case errors.Is(err, store.ErrKnowledgeNotReady):
		writeMemoryError(w, http.StatusConflict, "knowledge_not_ready", "知识库为空：请先完成 /api/analyze")
	case errors.Is(err, store.ErrMemorySafeState):
		s.log.Error("memory initialize in safe state", "err", err)
		writeMemoryError(w, http.StatusInternalServerError, "memory_safe_state", "记忆子系统处于安全态：拒绝写入")
	default:
		s.log.Error("memory initialize", "err", err)
		writeError(w, http.StatusInternalServerError, "启动记忆初始化失败")
	}
}

// handleGetCharacterMemory 覆盖 §12.2：c_ 前缀校验 400、不存在 404、200 聚合视图。
func (s *Server) handleGetCharacterMemory(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !store.ValidCharacterID(id) {
		writeMemoryError(w, http.StatusBadRequest, "invalid_character_id", "角色 ID 必须为 c_ 前缀")
		return
	}
	view, err := s.store.LoadCharacterMemory(id)
	if err != nil {
		if errors.Is(err, store.ErrCharacterNotFound) {
			writeMemoryError(w, http.StatusNotFound, "character_not_found", "角色不存在")
			return
		}
		s.log.Error("load character memory", "err", err)
		writeError(w, http.StatusInternalServerError, "读取角色记忆失败")
		return
	}
	writeJSON(w, http.StatusOK, view)
}

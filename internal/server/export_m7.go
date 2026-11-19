package server

import (
	"errors"
	"net/http"
	"net/url"
	"os"
	"strings"

	"novelagent/internal/export"
)

func (s *Server) registerExportRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/export", s.handleCreateExport)
	mux.HandleFunc("GET /api/exports", s.handleListExports)
	mux.HandleFunc("GET /api/exports/{id}/download", s.handleDownloadExport)
}

func (s *Server) exportService(w http.ResponseWriter) (*export.Service, bool) {
	if s.exports == nil {
		writeError(w, http.StatusServiceUnavailable, "导出服务未启用")
		return nil, false
	}
	return s.exports, true
}

func (s *Server) handleCreateExport(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.exportService(w)
	if !ok {
		return
	}
	var req export.Request
	if err := decodeJSON(w, r, &req, 64<<10); err != nil {
		writeError(w, http.StatusBadRequest, "导出请求格式不正确")
		return
	}
	record, err := svc.Create(req)
	if err != nil {
		switch {
		case errors.Is(err, export.ErrInvalidFormat):
			writeError(w, http.StatusBadRequest, "format 仅支持 txt、md、epub")
		case strings.Contains(err.Error(), "no chapters"):
			writeError(w, http.StatusBadRequest, "没有可导出的章节")
		default:
			s.log.Error("create export", "err", err)
			writeError(w, http.StatusInternalServerError, "生成导出文件失败")
		}
		return
	}
	writeJSON(w, http.StatusOK, record)
}

func (s *Server) handleListExports(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.exportService(w)
	if !ok {
		return
	}
	records, err := svc.List()
	if err != nil {
		s.log.Error("list exports", "err", err)
		writeError(w, http.StatusInternalServerError, "读取导出记录失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"exports": records})
}

func (s *Server) handleDownloadExport(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.exportService(w)
	if !ok {
		return
	}
	record, err := svc.Get(r.PathValue("id"))
	if err != nil {
		if errors.Is(err, export.ErrExportNotFound) {
			writeError(w, http.StatusNotFound, "导出记录不存在")
			return
		}
		s.log.Error("get export", "err", err)
		writeError(w, http.StatusInternalServerError, "读取导出记录失败")
		return
	}
	path, err := svc.FilePath(record)
	if err != nil {
		writeError(w, http.StatusNotFound, "导出文件不存在")
		return
	}
	if _, statErr := os.Stat(path); statErr != nil {
		writeError(w, http.StatusNotFound, "导出文件已丢失，请重新导出")
		return
	}
	w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(record.FileName))
	w.Header().Set("Cache-Control", "no-store")
	http.ServeFile(w, r, path)
}

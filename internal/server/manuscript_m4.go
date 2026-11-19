package server

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"novelagent/internal/continuation"
	"novelagent/internal/novel"
	"novelagent/internal/store"
)

func (s *Server) registerManuscriptRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/chapters/reorder", s.handleReorderChapters)
	mux.HandleFunc("POST /api/chapters/merge", s.handleMergeChapters)
	mux.HandleFunc("POST /api/chapters/{id}/split", s.handleSplitChapter)
	mux.HandleFunc("GET /api/chapters/{id}/versions", s.handleListChapterVersions)
	mux.HandleFunc("POST /api/chapters/{id}/versions/{revision}/restore", s.handleRestoreChapterVersion)
	mux.HandleFunc("POST /api/continue/candidates", s.handleContinueCandidates)
	mux.HandleFunc("GET /api/continuations", s.handleListContinuations)
	mux.HandleFunc("GET /api/continuations/{id}", s.handleGetContinuation)
	mux.HandleFunc("GET /api/snapshots", s.handleListSnapshots)
	mux.HandleFunc("POST /api/snapshots", s.handleCreateSnapshot)
	mux.HandleFunc("GET /api/snapshots/{id}", s.handleGetBookSnapshot)
	mux.HandleFunc("POST /api/snapshots/{id}/restore", s.handleRestoreSnapshot)
}

type reorderRequest struct {
	ChapterIDs []string `json:"chapterIds"`
}

func (s *Server) handleReorderChapters(w http.ResponseWriter, r *http.Request) {
	var req reorderRequest
	if err := decodeJSON(w, r, &req, 256<<10); err != nil {
		writeError(w, http.StatusBadRequest, "章节顺序格式不正确")
		return
	}
	project, err := s.store.ReorderChapters(req.ChapterIDs)
	if err != nil {
		s.writeManuscriptError(w, err, "调整章节顺序失败")
		return
	}
	s.notifyAnalysis()
	writeJSON(w, http.StatusOK, map[string]any{"project": publicProjectOf(project)})
}

type mergeRequest struct {
	ChapterIDs []string `json:"chapterIds"`
	Title      string   `json:"title"`
}

func (s *Server) handleMergeChapters(w http.ResponseWriter, r *http.Request) {
	var req mergeRequest
	if err := decodeJSON(w, r, &req, 256<<10); err != nil {
		writeError(w, http.StatusBadRequest, "合并请求格式不正确")
		return
	}
	chapter, project, err := s.store.MergeChapters(req.ChapterIDs, req.Title)
	if err != nil {
		s.writeManuscriptError(w, err, "合并章节失败")
		return
	}
	s.notifyAnalysis()
	writeJSON(w, http.StatusOK, chapterResponse{Chapter: chapter, Project: publicProjectOf(project)})
}

type splitRequest struct {
	Offset      int    `json:"offset"`
	FirstTitle  string `json:"firstTitle"`
	SecondTitle string `json:"secondTitle"`
}

func (s *Server) handleSplitChapter(w http.ResponseWriter, r *http.Request) {
	var req splitRequest
	if err := decodeJSON(w, r, &req, 64<<10); err != nil {
		writeError(w, http.StatusBadRequest, "拆分请求格式不正确")
		return
	}
	first, second, project, err := s.store.SplitChapter(r.PathValue("id"), req.Offset, req.FirstTitle, req.SecondTitle)
	if err != nil {
		s.writeManuscriptError(w, err, "拆分章节失败")
		return
	}
	s.notifyAnalysis()
	writeJSON(w, http.StatusOK, map[string]any{
		"first":   first,
		"second":  second,
		"project": publicProjectOf(project),
	})
}

func (s *Server) handleListChapterVersions(w http.ResponseWriter, r *http.Request) {
	versions, err := s.store.ListChapterVersions(r.PathValue("id"), queryLimit(r, 50, 200))
	if err != nil {
		s.writeManuscriptError(w, err, "读取章节版本失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"versions": versions})
}

func (s *Server) handleRestoreChapterVersion(w http.ResponseWriter, r *http.Request) {
	revision, err := strconv.ParseInt(r.PathValue("revision"), 10, 64)
	if err != nil || revision <= 0 {
		writeError(w, http.StatusBadRequest, "版本号不正确")
		return
	}
	chapter, project, snapshotID, err := s.store.RestoreChapterVersion(r.PathValue("id"), revision)
	if err != nil {
		s.writeManuscriptError(w, err, "回滚章节失败")
		return
	}
	s.notifyAnalysis()
	writeJSON(w, http.StatusOK, map[string]any{
		"chapter":        chapter,
		"project":        publicProjectOf(project),
		"autoSnapshotId": snapshotID,
	})
}

func (s *Server) handleContinueCandidates(w http.ResponseWriter, r *http.Request) {
	var req continuation.Request
	if err := decodeJSON(w, r, &req, 64<<10); err != nil {
		writeError(w, http.StatusBadRequest, "续写候选请求格式不正确")
		return
	}
	req.Direction = strings.TrimSpace(req.Direction)
	if utf8.RuneCountInString(req.Direction) > 2000 {
		writeError(w, http.StatusBadRequest, "续写要求太长，请控制在 2000 字以内")
		return
	}
	req.Candidates = novel.NormalizeCandidateCount(req.Candidates)
	if req.TargetChars <= 0 {
		req.TargetChars = 800
	}
	req.TargetChars = min(max(req.TargetChars, 200), 4000)
	if req.Temperature <= 0 {
		req.Temperature = 0.85
	}
	req.Temperature = min(req.Temperature, 1.5)
	if req.StartMode == novel.ContinuationStartChapterOffset && strings.TrimSpace(req.ChapterID) == "" {
		writeError(w, http.StatusBadRequest, "章节内续写需要 chapterId")
		return
	}

	result, err := s.cont.Generate(r.Context(), req)
	if err != nil {
		switch {
		case errors.Is(err, continuation.ErrConfigMissing):
			writeError(w, http.StatusBadRequest, "请先在「模型设置」中填写 Base URL 和模型名")
		case errors.Is(err, continuation.ErrNoContent):
			writeError(w, http.StatusBadRequest, "请先导入文稿或填写正文，再开始续写")
		case errors.Is(err, continuation.ErrStartInvalid):
			writeError(w, http.StatusBadRequest, "续写起点不正确："+shortError(err))
		case errors.Is(err, continuation.ErrAllCandidatesFailed):
			writeError(w, http.StatusBadGateway, "所有候选版本生成失败："+shortError(err))
		default:
			s.log.Error("generate continuation candidates", "err", err)
			writeError(w, http.StatusInternalServerError, "生成续写候选失败")
		}
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleListContinuations(w http.ResponseWriter, r *http.Request) {
	groups, err := s.store.ListContinuationGroups(queryLimit(r, 50, 200))
	if err != nil {
		s.log.Error("list continuation groups", "err", err)
		writeError(w, http.StatusInternalServerError, "读取续写候选失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"groups": groups})
}

func (s *Server) handleGetContinuation(w http.ResponseWriter, r *http.Request) {
	group, candidates, err := s.store.GetContinuationGroup(r.PathValue("id"))
	if err != nil {
		if errors.Is(err, store.ErrContinuationGroupNotFound) {
			writeError(w, http.StatusNotFound, "续写候选不存在")
			return
		}
		s.log.Error("get continuation group", "err", err)
		writeError(w, http.StatusInternalServerError, "读取续写候选失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"group": group, "candidates": candidates})
}

type snapshotRequest struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

func (s *Server) handleListSnapshots(w http.ResponseWriter, r *http.Request) {
	snapshots, err := s.store.ListSnapshots(queryLimit(r, 50, 200))
	if err != nil {
		s.log.Error("list snapshots", "err", err)
		writeError(w, http.StatusInternalServerError, "读取快照失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"snapshots": snapshots})
}

func (s *Server) handleCreateSnapshot(w http.ResponseWriter, r *http.Request) {
	var req snapshotRequest
	if err := decodeJSON(w, r, &req, 64<<10); err != nil {
		writeError(w, http.StatusBadRequest, "快照请求格式不正确")
		return
	}
	snapshot, err := s.store.CreateSnapshot(req.Name, req.Reason)
	if err != nil {
		if errors.Is(err, store.ErrSnapshotEmpty) {
			writeError(w, http.StatusBadRequest, "当前作品还没有可快照的章节")
			return
		}
		s.log.Error("create snapshot", "err", err)
		writeError(w, http.StatusInternalServerError, "创建快照失败")
		return
	}
	writeJSON(w, http.StatusOK, snapshot)
}

func (s *Server) handleGetBookSnapshot(w http.ResponseWriter, r *http.Request) {
	detail, err := s.store.GetSnapshot(r.PathValue("id"))
	if err != nil {
		if errors.Is(err, store.ErrSnapshotNotFound) {
			writeError(w, http.StatusNotFound, "快照不存在")
			return
		}
		s.log.Error("get snapshot", "err", err)
		writeError(w, http.StatusInternalServerError, "读取快照失败")
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (s *Server) handleRestoreSnapshot(w http.ResponseWriter, r *http.Request) {
	project, autoSnapshotID, err := s.store.RestoreSnapshot(r.PathValue("id"))
	if err != nil {
		if errors.Is(err, store.ErrSnapshotNotFound) {
			writeError(w, http.StatusNotFound, "快照不存在")
			return
		}
		if errors.Is(err, store.ErrSnapshotEmpty) {
			writeError(w, http.StatusBadRequest, "快照没有章节内容")
			return
		}
		s.log.Error("restore snapshot", "err", err)
		writeError(w, http.StatusInternalServerError, "回滚快照失败")
		return
	}
	s.notifyAnalysis()
	writeJSON(w, http.StatusOK, map[string]any{
		"project":        publicProjectOf(project),
		"autoSnapshotId": autoSnapshotID,
	})
}

func (s *Server) writeManuscriptError(w http.ResponseWriter, err error, fallback string) {
	switch {
	case errors.Is(err, store.ErrChapterNotFound):
		writeError(w, http.StatusNotFound, "章节不存在")
	case errors.Is(err, store.ErrChapterVersionNotFound):
		writeError(w, http.StatusNotFound, "章节版本不存在")
	case errors.Is(err, store.ErrInvalidChapterOrder):
		writeError(w, http.StatusBadRequest, "章节顺序必须包含全部章节且不重复")
	case errors.Is(err, store.ErrInvalidMerge):
		writeError(w, http.StatusBadRequest, "合并需要至少两个连续章节")
	case errors.Is(err, store.ErrInvalidSplit):
		writeError(w, http.StatusBadRequest, "拆分位置超出章节正文范围")
	case errors.Is(err, store.ErrInvalidChapter):
		writeError(w, http.StatusBadRequest, "章节参数不正确")
	default:
		s.log.Error("manuscript request failed", "err", err)
		writeError(w, http.StatusInternalServerError, fallback)
	}
}

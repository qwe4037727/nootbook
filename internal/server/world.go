package server

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"novelagent/internal/novel"
	"novelagent/internal/store"
	"novelagent/internal/world"
)

// 大世界 API（M2）：统一词条、字段锁、证据跳转、关系、时间线与第 N 章快照。
// 路由仅在 milestone.yaml 的 m2.enabled=true 时注册。

func (s *Server) registerWorldRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/world/entries", s.handleListWorldEntries)
	mux.HandleFunc("POST /api/world/entries", s.handlePostWorldEntry)
	mux.HandleFunc("GET /api/world/entries/{id}", s.handleGetWorldEntry)
	mux.HandleFunc("PUT /api/world/entries/{id}", s.handlePutWorldEntry)
	mux.HandleFunc("DELETE /api/world/entries/{id}", s.handleDeleteWorldEntry)
	mux.HandleFunc("GET /api/world/entries/{id}/evidence", s.handleGetWorldEvidence)
	mux.HandleFunc("POST /api/world/entries/{id}/locks", s.handlePostWorldLocks)
	mux.HandleFunc("GET /api/world/relations", s.handleListRelations)
	mux.HandleFunc("POST /api/world/relations", s.handlePostRelation)
	mux.HandleFunc("PUT /api/world/relations/{id}", s.handlePutRelation)
	mux.HandleFunc("DELETE /api/world/relations/{id}", s.handleDeleteRelation)
	mux.HandleFunc("GET /api/world/timeline", s.handleGetTimeline)
	mux.HandleFunc("POST /api/world/timeline", s.handlePostTimeline)
	mux.HandleFunc("PUT /api/world/timeline/{id}", s.handlePutTimeline)
	mux.HandleFunc("DELETE /api/world/timeline/{id}", s.handleDeleteTimeline)
	mux.HandleFunc("GET /api/world/snapshot", s.handleGetSnapshot)
	mux.HandleFunc("GET /api/world/search", s.handleSearchWorld)
}

func validEntryID(id string) bool {
	return novel.ValidID(id) && novel.EntryKindForID(id) != ""
}

func validRelationID(id string) bool {
	return novel.ValidID(id) && strings.HasPrefix(id, "rel_")
}

func validTimelineID(id string) bool {
	return novel.ValidID(id) && strings.HasPrefix(id, "tl_")
}

func (s *Server) writeWorldError(w http.ResponseWriter, err error, fallback string) {
	switch {
	case errors.Is(err, world.ErrEntryNotFound):
		writeMemoryError(w, http.StatusNotFound, "entry_not_found", "词条不存在")
	case errors.Is(err, world.ErrRelationNotFound):
		writeMemoryError(w, http.StatusNotFound, "relation_not_found", "关系不存在")
	case errors.Is(err, world.ErrTimelineNotFound):
		writeMemoryError(w, http.StatusNotFound, "timeline_not_found", "时间线条目不存在")
	case errors.Is(err, world.ErrChapterNotFound):
		writeMemoryError(w, http.StatusNotFound, "chapter_not_found", "章节不存在")
	case errors.Is(err, store.ErrRelationNotFound):
		writeMemoryError(w, http.StatusNotFound, "relation_not_found", "关系不存在")
	case errors.Is(err, novel.ErrKBValidation):
		writeMemoryError(w, http.StatusBadRequest, "invalid_world_request", err.Error())
	default:
		s.log.Error("world request failed", "err", err)
		writeError(w, http.StatusInternalServerError, fallback)
	}
}

func (s *Server) handleListWorldEntries(w http.ResponseWriter, r *http.Request) {
	query := world.EntryQuery{Query: r.URL.Query().Get("q")}
	if kind := strings.TrimSpace(r.URL.Query().Get("kind")); kind != "" {
		if !novel.EntryKind(kind).Valid() {
			writeMemoryError(w, http.StatusBadRequest, "invalid_kind", "kind 必须为 character|faction|location|item|setting")
			return
		}
		query.Kind = novel.EntryKind(kind)
	}
	if value := strings.TrimSpace(r.URL.Query().Get("chapter")); value != "" {
		n, err := strconv.Atoi(value)
		if err != nil || n <= 0 {
			writeMemoryError(w, http.StatusBadRequest, "invalid_chapter", "chapter 必须为正整数")
			return
		}
		query.Chapter = n
	}
	entries, err := s.world.ListEntries(query)
	if err != nil {
		s.writeWorldError(w, err, "读取词条失败")
		return
	}
	writeJSON(w, http.StatusOK, entries)
}

func (s *Server) handlePostWorldEntry(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Kind       string            `json:"kind"`
		Name       string            `json:"name"`
		Summary    string            `json:"summary"`
		Aliases    []string          `json:"aliases"`
		Fields     map[string]string `json:"fields"`
		SourceRefs []novel.SourceRef `json:"sourceRefs"`
	}
	if err := decodeStrictJSON(w, r, &req, 32<<10, false); err != nil {
		writeDecodeError(w, err, "请求体必须为单个 JSON 对象（kind/name/fields）")
		return
	}
	kind := novel.EntryKind(strings.TrimSpace(req.Kind))
	if !kind.Valid() {
		writeMemoryError(w, http.StatusBadRequest, "invalid_kind", "kind 必须为 character|faction|location|item|setting")
		return
	}
	entry, err := s.world.CreateEntry(world.EntryCreate{
		Kind:       kind,
		Name:       req.Name,
		Summary:    req.Summary,
		Aliases:    req.Aliases,
		Fields:     req.Fields,
		SourceRefs: req.SourceRefs,
	})
	if err != nil {
		s.writeWorldError(w, err, "新建词条失败")
		return
	}
	writeJSON(w, http.StatusOK, entry)
}

func (s *Server) handleGetWorldEntry(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validEntryID(id) {
		writeMemoryError(w, http.StatusBadRequest, "invalid_entry_id", "词条 ID 格式不正确")
		return
	}
	entry, err := s.world.GetEntry(id)
	if err != nil {
		s.writeWorldError(w, err, "读取词条失败")
		return
	}
	relations, err := s.world.ListRelations(id, "")
	if err != nil {
		s.writeWorldError(w, err, "读取词条关系失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"entry": entry, "relations": relations})
}

func (s *Server) handlePutWorldEntry(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validEntryID(id) {
		writeMemoryError(w, http.StatusBadRequest, "invalid_entry_id", "词条 ID 格式不正确")
		return
	}
	var req struct {
		Name   string            `json:"name"`
		Fields map[string]string `json:"fields"`
	}
	if err := decodeStrictJSON(w, r, &req, 32<<10, false); err != nil {
		writeDecodeError(w, err, "请求体必须为单个 JSON 对象（name/fields）")
		return
	}
	entry, err := s.world.UpdateEntry(id, world.EntryPatch{Name: req.Name, Fields: req.Fields})
	if err != nil {
		s.writeWorldError(w, err, "保存词条失败")
		return
	}
	writeJSON(w, http.StatusOK, entry)
}

func (s *Server) handleDeleteWorldEntry(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validEntryID(id) {
		writeMemoryError(w, http.StatusBadRequest, "invalid_entry_id", "词条 ID 格式不正确")
		return
	}
	if err := s.world.DeleteEntry(id); err != nil {
		s.writeWorldError(w, err, "删除词条失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true, "id": id})
}

func (s *Server) handleGetWorldEvidence(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validEntryID(id) {
		writeMemoryError(w, http.StatusBadRequest, "invalid_entry_id", "词条 ID 格式不正确")
		return
	}
	items, err := s.world.Evidence(id)
	if err != nil {
		s.writeWorldError(w, err, "解析证据失败")
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) handlePostWorldLocks(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validEntryID(id) {
		writeMemoryError(w, http.StatusBadRequest, "invalid_entry_id", "词条 ID 格式不正确")
		return
	}
	var req struct {
		Fields []string `json:"fields"`
		Lock   *bool    `json:"lock"`
	}
	if err := decodeStrictJSON(w, r, &req, 4096, false); err != nil {
		writeDecodeError(w, err, "请求体必须为单个 JSON 对象（fields/lock）")
		return
	}
	lock := true
	if req.Lock != nil {
		lock = *req.Lock
	}
	fields, err := s.world.SetFieldLocks(id, req.Fields, lock)
	if err != nil {
		s.writeWorldError(w, err, "保存字段锁失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "lockedFields": fields})
}

// ---- 关系 ----

func (s *Server) handleListRelations(w http.ResponseWriter, r *http.Request) {
	entryID := strings.TrimSpace(r.URL.Query().Get("entryId"))
	if entryID != "" && !validEntryID(entryID) {
		writeMemoryError(w, http.StatusBadRequest, "invalid_entry_id", "entryId 格式不正确")
		return
	}
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	if status != "" && !novel.RelationStatus(status).Valid() {
		writeMemoryError(w, http.StatusBadRequest, "invalid_status", "status 必须为 active|invalidated")
		return
	}
	relations, err := s.world.ListRelations(entryID, status)
	if err != nil {
		s.writeWorldError(w, err, "读取关系失败")
		return
	}
	writeJSON(w, http.StatusOK, relations)
}

func (s *Server) handlePostRelation(w http.ResponseWriter, r *http.Request) {
	var rel novel.Relation
	if err := decodeStrictJSON(w, r, &rel, 16<<10, false); err != nil {
		writeDecodeError(w, err, "请求体必须为单个 JSON 关系对象")
		return
	}
	if !validEntryID(rel.FromEntryID) || !validEntryID(rel.ToEntryID) {
		writeMemoryError(w, http.StatusBadRequest, "invalid_entry_id", "fromEntryId/toEntryId 格式不正确")
		return
	}
	saved, err := s.world.SaveRelation(rel)
	if err != nil {
		s.writeWorldError(w, err, "保存关系失败")
		return
	}
	writeJSON(w, http.StatusOK, saved)
}

func (s *Server) handlePutRelation(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validRelationID(id) {
		writeMemoryError(w, http.StatusBadRequest, "invalid_relation_id", "关系 ID 必须为 rel_ 前缀")
		return
	}
	var rel novel.Relation
	if err := decodeStrictJSON(w, r, &rel, 16<<10, false); err != nil {
		writeDecodeError(w, err, "请求体必须为单个 JSON 关系对象")
		return
	}
	rel.ID = id
	if !validEntryID(rel.FromEntryID) || !validEntryID(rel.ToEntryID) {
		writeMemoryError(w, http.StatusBadRequest, "invalid_entry_id", "fromEntryId/toEntryId 格式不正确")
		return
	}
	saved, err := s.world.SaveRelation(rel)
	if err != nil {
		s.writeWorldError(w, err, "保存关系失败")
		return
	}
	writeJSON(w, http.StatusOK, saved)
}

func (s *Server) handleDeleteRelation(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validRelationID(id) {
		writeMemoryError(w, http.StatusBadRequest, "invalid_relation_id", "关系 ID 必须为 rel_ 前缀")
		return
	}
	if err := s.world.DeleteRelation(id); err != nil {
		s.writeWorldError(w, err, "删除关系失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true, "id": id})
}

// ---- 时间线 ----

func (s *Server) handleGetTimeline(w http.ResponseWriter, r *http.Request) {
	events, err := s.world.Timeline()
	if err != nil {
		s.writeWorldError(w, err, "读取时间线失败")
		return
	}
	writeJSON(w, http.StatusOK, events)
}

type timelineRequest struct {
	ChapterID    string   `json:"chapterId"`
	Title        string   `json:"title"`
	Summary      string   `json:"summary"`
	EventType    string   `json:"eventType"`
	Participants []string `json:"participants"`
	Entities     []string `json:"entities"`
	StoryOrder   int      `json:"storyOrder"`
}

func (s *Server) handlePostTimeline(w http.ResponseWriter, r *http.Request) {
	var req timelineRequest
	if err := decodeStrictJSON(w, r, &req, 8<<10, false); err != nil {
		writeDecodeError(w, err, "请求体必须为单个 JSON 时间线对象")
		return
	}
	event, err := s.world.SaveTimelineEvent("", world.TimelinePatch{
		ChapterID:    req.ChapterID,
		Title:        req.Title,
		Summary:      req.Summary,
		EventType:    req.EventType,
		Participants: req.Participants,
		Entities:     req.Entities,
		StoryOrder:   req.StoryOrder,
	})
	if err != nil {
		s.writeWorldError(w, err, "保存时间线事件失败")
		return
	}
	writeJSON(w, http.StatusOK, event)
}

func (s *Server) handlePutTimeline(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validTimelineID(id) {
		writeMemoryError(w, http.StatusBadRequest, "invalid_timeline_id", "时间线 ID 必须为 tl_ 前缀")
		return
	}
	var req timelineRequest
	if err := decodeStrictJSON(w, r, &req, 8<<10, false); err != nil {
		writeDecodeError(w, err, "请求体必须为单个 JSON 时间线对象")
		return
	}
	event, err := s.world.SaveTimelineEvent(id, world.TimelinePatch{
		ChapterID:    req.ChapterID,
		Title:        req.Title,
		Summary:      req.Summary,
		EventType:    req.EventType,
		Participants: req.Participants,
		Entities:     req.Entities,
		StoryOrder:   req.StoryOrder,
	})
	if err != nil {
		s.writeWorldError(w, err, "保存时间线事件失败")
		return
	}
	writeJSON(w, http.StatusOK, event)
}

func (s *Server) handleDeleteTimeline(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validTimelineID(id) {
		writeMemoryError(w, http.StatusBadRequest, "invalid_timeline_id", "时间线 ID 必须为 tl_ 前缀")
		return
	}
	if err := s.world.DeleteTimelineEvent(id); err != nil {
		s.writeWorldError(w, err, "删除时间线事件失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true, "id": id})
}

// ---- 快照与检索 ----

func (s *Server) handleGetSnapshot(w http.ResponseWriter, r *http.Request) {
	value := strings.TrimSpace(r.URL.Query().Get("chapter"))
	chapter, err := strconv.Atoi(value)
	if err != nil || chapter <= 0 {
		writeMemoryError(w, http.StatusBadRequest, "invalid_chapter", "chapter 必须为正整数")
		return
	}
	snap, err := s.world.Snapshot(chapter)
	if err != nil {
		s.writeWorldError(w, err, "读取世界快照失败")
		return
	}
	writeJSON(w, http.StatusOK, snap)
}

func (s *Server) handleSearchWorld(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeMemoryError(w, http.StatusBadRequest, "invalid_query", "q 不能为空")
		return
	}
	kind := strings.TrimSpace(r.URL.Query().Get("kind"))
	if kind != "" && !novel.EntryKind(kind).Valid() {
		writeMemoryError(w, http.StatusBadRequest, "invalid_kind", "kind 必须为 character|faction|location|item|setting")
		return
	}
	entries, err := s.world.ListEntries(world.EntryQuery{Query: q, Kind: novel.EntryKind(kind)})
	if err != nil {
		s.writeWorldError(w, err, "检索词条失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"query": q, "entries": entries})
}

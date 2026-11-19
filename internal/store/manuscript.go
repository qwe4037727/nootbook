package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"novelagent/internal/novel"
)

// M4 章节增强：追加导入、排序、合并、拆分、单章版本、全书快照与回滚。
// 所有正文写入仍遵守 store.mu 守护；记忆/事件对账统一走 afterChapterSetChanged。

var (
	ErrChapterVersionNotFound = errors.New("store: chapter version not found")
	ErrSnapshotNotFound       = errors.New("store: book snapshot not found")
	ErrInvalidChapterOrder    = errors.New("store: chapter order must contain every chapter exactly once")
	ErrInvalidMerge           = errors.New("store: merge requires at least two contiguous chapters")
	ErrInvalidSplit           = errors.New("store: split offset out of range")
	ErrSnapshotEmpty          = errors.New("store: snapshot has no chapters")
)

const (
	maxChapterVersions = 50

	ChapterVersionSave        = "save"
	ChapterVersionDraftCommit = "draft_commit"
	ChapterVersionRollback    = "rollback"
	ChapterVersionMerge       = "merge"
	ChapterVersionSplit       = "split"
)

// ChapterVersion 是单章历史版本（chapter_version 表）。
type ChapterVersion struct {
	ID           string    `json:"id"`
	ChapterID    string    `json:"chapterId"`
	ChapterIndex int       `json:"chapterIndex"`
	Revision     int64     `json:"revision"`
	Title        string    `json:"title"`
	Content      string    `json:"content"`
	CharCount    int       `json:"charCount"`
	Reason       string    `json:"reason"`
	CreatedAt    time.Time `json:"createdAt"`
}

// BookSnapshot 是全书快照头（book_snapshot 表）。
// Knowledge 仅在单条详情中返回，列表接口会置空以控制载荷。
type BookSnapshot struct {
	ID           string          `json:"id"`
	Name         string          `json:"name"`
	Reason       string          `json:"reason"`
	Title        string          `json:"title"`
	ChapterCount int             `json:"chapterCount"`
	CharCount    int             `json:"charCount"`
	Knowledge    json.RawMessage `json:"knowledge,omitempty"`
	CreatedAt    time.Time       `json:"createdAt"`
}

// SnapshotChapter 是快照内的章节副本。
type SnapshotChapter struct {
	SnapshotID string `json:"-"`
	ChapterID  string `json:"chapterId"`
	Index      int    `json:"index"`
	Title      string `json:"title"`
	Content    string `json:"content"`
	CharCount  int    `json:"charCount"`
	Revision   int64  `json:"revision"`
}

// SnapshotDetail 是快照头与全部章节。
type SnapshotDetail struct {
	BookSnapshot
	Chapters []SnapshotChapter `json:"chapters"`
}

// AppendImport 追加导入：仅向书尾新增章节，不改写既有章节、不重置知识库/记忆。
func (s *Store) AppendImport(title string, chapters []novel.Chapter) (novel.Project, []novel.Chapter, error) {
	s.mu.Lock()
	project, appended, oldIndexes, newIndexes, err := s.appendImportLocked(title, chapters)
	s.mu.Unlock()
	if err != nil {
		return novel.Project{}, nil, err
	}
	if chapterOrderChanged(oldIndexes, newIndexes) {
		s.afterChapterSetChanged(oldIndexes, newIndexes)
	}
	change := ChapterChange{
		Kind:         ChaptersAppended,
		Title:        project.Title,
		ChapterCount: len(project.Chapters),
	}
	if n := len(appended); n > 0 {
		change.ChapterID = appended[n-1].ID
		change.Index = appended[n-1].Index
		change.CharCount = appended[n-1].CharCount
	}
	s.notifyChapterChange(change)
	return project, appended, nil
}

func (s *Store) appendImportLocked(title string, chapters []novel.Chapter) (novel.Project, []novel.Chapter, map[string]int, map[string]int, error) {
	project, err := s.loadProjectLocked()
	if err != nil {
		return novel.Project{}, nil, nil, nil, err
	}
	oldIndexes := chapterIndexMap(project.Chapters)
	now := time.Now().UTC()

	seen := make(map[string]struct{}, len(project.Chapters)+len(chapters))
	for _, meta := range project.Chapters {
		seen[meta.ID] = struct{}{}
	}
	base := len(project.Chapters)
	appended := make([]novel.Chapter, 0, len(chapters))
	for _, chapter := range chapters {
		chapter.Content = strings.TrimSpace(chapter.Content)
		if chapter.Content == "" {
			continue
		}
		index := base + len(appended) + 1
		chapter.Index = index
		chapter.Title = strings.TrimSpace(chapter.Title)
		if chapter.Title == "" {
			chapter.Title = novel.InferTitle(chapter.Content)
		}
		if chapter.Title == "" {
			chapter.Title = novel.DefaultChapterTitle(index)
		}
		if chapter.ID == "" || !novel.ValidID(chapter.ID) {
			chapter.ID = novel.NewID("ch")
		}
		if _, exists := seen[chapter.ID]; exists {
			chapter.ID = novel.NewID("ch")
		}
		seen[chapter.ID] = struct{}{}
		chapter.CharCount = novel.CountChars(chapter.Content)
		chapter.Revision = 1
		chapter.UpdatedAt = now
		if err := s.writeChapterLocked(chapter); err != nil {
			return novel.Project{}, nil, nil, nil, err
		}
		appended = append(appended, chapter)
	}
	if len(appended) == 0 {
		return novel.Project{}, nil, nil, nil, errors.New("store: append import has no chapter content")
	}

	// 追加导入不删除、不改写既有章节；仅在空书时接受标题。
	if base == 0 {
		if cleaned := strings.TrimSpace(title); cleaned != "" {
			project.Title = cleaned
		}
	}
	for _, chapter := range appended {
		project.Chapters = append(project.Chapters, chapter.Meta())
	}
	state := project.Analysis
	state.Status = novel.AnalysisPending
	state.Message = ""
	state.AnalyzedCount = min(state.AnalyzedCount, base)
	state.TotalCount = len(project.Chapters)
	state.UpdatedAt = now
	project.Analysis = state
	project.Revision++
	project.UpdatedAt = now
	project.Normalize()
	if err := s.writeProjectLocked(project); err != nil {
		return novel.Project{}, nil, nil, nil, err
	}
	return project, appended, oldIndexes, chapterIndexMap(project.Chapters), nil
}

// ReorderChapters 按给定 ID 顺序重排全书；必须包含且仅包含现有全部章节。
func (s *Store) ReorderChapters(ids []string) (novel.Project, error) {
	s.mu.Lock()
	project, oldIndexes, newIndexes, _, err := s.reorderChaptersLocked(ids)
	s.mu.Unlock()
	if err != nil {
		return novel.Project{}, err
	}
	if chapterOrderChanged(oldIndexes, newIndexes) {
		s.afterChapterSetChanged(oldIndexes, newIndexes)
	}
	if chapterOrderChanged(oldIndexes, newIndexes) {
		s.notifyChapterChange(ChapterChange{
			Kind:         ChaptersReordered,
			ChapterCount: len(project.Chapters),
		})
	}
	return project, nil
}

func (s *Store) reorderChaptersLocked(ids []string) (novel.Project, map[string]int, map[string]int, bool, error) {
	project, err := s.loadProjectLocked()
	if err != nil {
		return novel.Project{}, nil, nil, false, err
	}
	oldIndexes := chapterIndexMap(project.Chapters)
	if len(ids) != len(project.Chapters) {
		return novel.Project{}, nil, nil, false, ErrInvalidChapterOrder
	}
	byID := make(map[string]novel.ChapterMeta, len(project.Chapters))
	for _, meta := range project.Chapters {
		byID[meta.ID] = meta
	}
	seen := make(map[string]struct{}, len(ids))
	next := make([]novel.ChapterMeta, 0, len(ids))
	changedAt := -1
	for i, id := range ids {
		id = strings.TrimSpace(id)
		meta, ok := byID[id]
		if !ok {
			return novel.Project{}, nil, nil, false, ErrInvalidChapterOrder
		}
		if _, dup := seen[id]; dup {
			return novel.Project{}, nil, nil, false, ErrInvalidChapterOrder
		}
		seen[id] = struct{}{}
		if changedAt < 0 && meta.Index != i+1 {
			changedAt = i
		}
		meta.Index = i + 1
		next = append(next, meta)
	}
	if changedAt < 0 {
		return project, oldIndexes, oldIndexes, false, nil
	}
	now := time.Now().UTC()
	project.Chapters = next
	invalidateAnalysisFrom(&project.Analysis, changedAt, now)
	project.Revision++
	project.UpdatedAt = now
	project.Normalize()
	if err := s.writeProjectLocked(project); err != nil {
		return novel.Project{}, nil, nil, false, err
	}
	return project, oldIndexes, chapterIndexMap(project.Chapters), true, nil
}

// MergeChapters 将相邻的多个章节合并为一章，保留第一个章节 ID。
func (s *Store) MergeChapters(ids []string, title string) (novel.Chapter, novel.Project, error) {
	s.mu.Lock()
	var change *ChapterChange
	var afterOld, afterNew map[string]int
	defer func() {
		s.mu.Unlock()
		if afterOld != nil && chapterOrderChanged(afterOld, afterNew) {
			s.afterChapterSetChanged(afterOld, afterNew)
		}
		if change != nil {
			s.notifyChapterChange(*change)
		}
	}()

	project, err := s.loadProjectLocked()
	if err != nil {
		return novel.Chapter{}, novel.Project{}, err
	}
	oldIndexes := chapterIndexMap(project.Chapters)
	if len(ids) < 2 {
		return novel.Chapter{}, novel.Project{}, ErrInvalidMerge
	}
	unique := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if !novel.ValidID(id) {
			return novel.Chapter{}, novel.Project{}, ErrInvalidChapter
		}
		if _, dup := unique[id]; dup {
			return novel.Chapter{}, novel.Project{}, ErrInvalidMerge
		}
		unique[id] = struct{}{}
	}
	positions := make(map[string]int, len(project.Chapters))
	for i, meta := range project.Chapters {
		positions[meta.ID] = i
	}
	startPos := -1
	for _, id := range ids {
		pos, ok := positions[id]
		if !ok {
			return novel.Chapter{}, novel.Project{}, ErrChapterNotFound
		}
		if startPos < 0 || pos < startPos {
			startPos = pos
		}
	}
	for i, id := range ids {
		pos := positions[id]
		if pos != startPos+i {
			return novel.Chapter{}, novel.Project{}, ErrInvalidMerge
		}
	}

	chapters := make([]novel.Chapter, 0, len(ids))
	var builder strings.Builder
	for i, id := range ids {
		chapter, err := s.readChapterLocked(id)
		if err != nil {
			return novel.Chapter{}, novel.Project{}, err
		}
		chapter.Index = positions[id] + 1
		if i > 0 {
			builder.WriteString("\n\n")
		}
		builder.WriteString(strings.TrimSpace(chapter.Content))
		chapters = append(chapters, chapter)
	}
	first := chapters[0]
	merged := first
	merged.Content = strings.TrimSpace(builder.String())
	merged.Title = strings.TrimSpace(title)
	if merged.Title == "" {
		merged.Title = first.Title
	}
	merged.CharCount = novel.CountChars(merged.Content)
	merged.Revision = first.Revision + 1
	merged.UpdatedAt = time.Now().UTC()
	for _, chapter := range chapters {
		s.recordChapterVersion(chapter, ChapterVersionMerge)
	}
	if err := s.writeChapterLocked(merged); err != nil {
		return novel.Chapter{}, novel.Project{}, err
	}
	for _, chapter := range chapters[1:] {
		if path, err := s.chapterPath(chapter.ID); err == nil {
			if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return novel.Chapter{}, novel.Project{}, fmt.Errorf("store: remove merged chapter: %w", err)
			}
		}
	}

	next := make([]novel.ChapterMeta, 0, len(project.Chapters)-len(ids)+1)
	skip := make(map[string]struct{}, len(ids)-1)
	for _, id := range ids[1:] {
		skip[id] = struct{}{}
	}
	for i, meta := range project.Chapters {
		if i == startPos {
			next = append(next, merged.Meta())
			continue
		}
		if _, drop := skip[meta.ID]; drop {
			continue
		}
		next = append(next, meta)
	}
	project.Chapters = next
	invalidateAnalysisFrom(&project.Analysis, startPos, merged.UpdatedAt)
	project.Revision++
	project.UpdatedAt = merged.UpdatedAt
	project.Normalize()
	// Normalize 可能重排 Index，回填到返回值。
	for i := range project.Chapters {
		if project.Chapters[i].ID == merged.ID {
			merged.Index = project.Chapters[i].Index
			break
		}
	}
	if err := s.writeProjectLocked(project); err != nil {
		return novel.Chapter{}, novel.Project{}, err
	}
	afterOld = oldIndexes
	afterNew = chapterIndexMap(project.Chapters)
	change = &ChapterChange{
		Kind:         ChapterSaved,
		ChapterID:    merged.ID,
		Title:        merged.Title,
		Index:        merged.Index,
		CharCount:    merged.CharCount,
		ChapterCount: len(project.Chapters),
	}
	return merged, project, nil
}

// SplitChapter 在指定 rune 偏移处把一章拆成两章；保留原 ID 作为前半章。
func (s *Store) SplitChapter(id string, offset int, firstTitle, secondTitle string) (novel.Chapter, novel.Chapter, novel.Project, error) {
	s.mu.Lock()
	changes := make([]ChapterChange, 0, 2)
	var afterOld, afterNew map[string]int
	defer func() {
		s.mu.Unlock()
		if afterOld != nil && chapterOrderChanged(afterOld, afterNew) {
			s.afterChapterSetChanged(afterOld, afterNew)
		}
		for _, change := range changes {
			s.notifyChapterChange(change)
		}
	}()

	project, err := s.loadProjectLocked()
	if err != nil {
		return novel.Chapter{}, novel.Chapter{}, novel.Project{}, err
	}
	oldIndexes := chapterIndexMap(project.Chapters)
	metaIndex := -1
	for i, meta := range project.Chapters {
		if meta.ID == id {
			metaIndex = i
			break
		}
	}
	if metaIndex < 0 {
		return novel.Chapter{}, novel.Chapter{}, novel.Project{}, ErrChapterNotFound
	}
	original, err := s.readChapterLocked(id)
	if err != nil {
		return novel.Chapter{}, novel.Chapter{}, novel.Project{}, err
	}
	runes := []rune(original.Content)
	if offset <= 0 || offset >= len(runes) {
		return novel.Chapter{}, novel.Chapter{}, novel.Project{}, ErrInvalidSplit
	}
	firstContent := strings.TrimSpace(string(runes[:offset]))
	secondContent := strings.TrimSpace(string(runes[offset:]))
	if firstContent == "" || secondContent == "" {
		return novel.Chapter{}, novel.Chapter{}, novel.Project{}, ErrInvalidSplit
	}

	now := time.Now().UTC()
	first := original
	first.Content = firstContent
	first.Title = strings.TrimSpace(firstTitle)
	if first.Title == "" {
		first.Title = original.Title
	}
	first.CharCount = novel.CountChars(first.Content)
	first.Revision = original.Revision + 1
	first.UpdatedAt = now

	secondIndex := original.Index + 1
	second := novel.Chapter{
		ID:        novel.NewID("ch"),
		Index:     secondIndex,
		Title:     strings.TrimSpace(secondTitle),
		Content:   secondContent,
		CharCount: novel.CountChars(secondContent),
		Revision:  1,
		UpdatedAt: now,
	}
	if second.Title == "" {
		second.Title = novel.DefaultChapterTitle(secondIndex)
	}
	s.recordChapterVersion(original, ChapterVersionSplit)
	if err := s.writeChapterLocked(first); err != nil {
		return novel.Chapter{}, novel.Chapter{}, novel.Project{}, err
	}
	if err := s.writeChapterLocked(second); err != nil {
		return novel.Chapter{}, novel.Chapter{}, novel.Project{}, err
	}

	next := make([]novel.ChapterMeta, 0, len(project.Chapters)+1)
	for i, meta := range project.Chapters {
		if i == metaIndex {
			next = append(next, first.Meta())
			next = append(next, second.Meta())
			continue
		}
		next = append(next, meta)
	}
	project.Chapters = next
	invalidateAnalysisFrom(&project.Analysis, metaIndex, now)
	project.Revision++
	project.UpdatedAt = now
	project.Normalize()
	for i := range project.Chapters {
		if project.Chapters[i].ID == first.ID {
			first.Index = project.Chapters[i].Index
		}
		if project.Chapters[i].ID == second.ID {
			second.Index = project.Chapters[i].Index
		}
	}
	if err := s.writeProjectLocked(project); err != nil {
		return novel.Chapter{}, novel.Chapter{}, novel.Project{}, err
	}
	afterOld = oldIndexes
	afterNew = chapterIndexMap(project.Chapters)
	changes = append(changes,
		ChapterChange{Kind: ChapterSaved, ChapterID: first.ID, Title: first.Title, Index: first.Index, CharCount: first.CharCount, ChapterCount: len(project.Chapters)},
		ChapterChange{Kind: ChapterCreated, ChapterID: second.ID, Title: second.Title, Index: second.Index, CharCount: second.CharCount, ChapterCount: len(project.Chapters)},
	)
	return first, second, project, nil
}

// invalidateAnalysisFrom 从 0 基位置开始使分析结果失效；validPrefix 个章节保持有效。
func invalidateAnalysisFrom(state *novel.AnalysisState, validPrefix int, now time.Time) {
	state.AnalyzedCount = min(state.AnalyzedCount, validPrefix)
	state.Status = novel.AnalysisPending
	state.Message = ""
	state.UpdatedAt = now
}

// saveChapterVersionLocked 记录一章的历史版本（由 store.mu 保护，内部使用 agent.db）。
// 版本是可审计的辅助数据，写失败只记日志，不阻断正文写入。
func (s *Store) saveChapterVersionLocked(chapter novel.Chapter, reason string) error {
	db, err := s.agentDatabase()
	if err != nil {
		return err
	}
	version := ChapterVersion{
		ID:           novel.NewID("ver"),
		ChapterID:    chapter.ID,
		ChapterIndex: chapter.Index,
		Revision:     chapter.Revision,
		Title:        chapter.Title,
		Content:      chapter.Content,
		CharCount:    chapter.CharCount,
		Reason:       reason,
		CreatedAt:    time.Now().UTC(),
	}
	if _, err := db.Exec(`INSERT OR IGNORE INTO chapter_version
		(id, chapter_id, chapter_index, revision, title, content, char_count, reason, created_at)
		VALUES (?,?,?,?,?,?,?,?,?)`,
		version.ID, version.ChapterID, version.ChapterIndex, version.Revision, version.Title,
		version.Content, version.CharCount, version.Reason, unixMillis(version.CreatedAt),
	); err != nil {
		return fmt.Errorf("store: save chapter version: %w", err)
	}
	if _, err := db.Exec(`DELETE FROM chapter_version
		WHERE chapter_id = ? AND id NOT IN (
			SELECT id FROM chapter_version WHERE chapter_id = ? ORDER BY revision DESC, created_at DESC LIMIT ?
		)`, chapter.ID, chapter.ID, maxChapterVersions); err != nil {
		return fmt.Errorf("store: prune chapter versions: %w", err)
	}
	return nil
}

func (s *Store) recordChapterVersion(chapter novel.Chapter, reason string) {
	if err := s.saveChapterVersionLocked(chapter, reason); err != nil {
		log.Printf("store: chapter %s version not recorded (ignored): %v", chapter.ID, err)
	}
}

// ListChapterVersions 返回章节历史版本（新到旧）。
func (s *Store) ListChapterVersions(chapterID string, limit int) ([]ChapterVersion, error) {
	if !novel.ValidID(chapterID) {
		return nil, ErrInvalidChapter
	}
	db, err := s.agentDatabase()
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > maxChapterVersions {
		limit = 20
	}
	rows, err := db.Query(`SELECT id, chapter_id, chapter_index, revision, title, content, char_count, reason, created_at
		FROM chapter_version WHERE chapter_id = ? ORDER BY revision DESC, created_at DESC LIMIT ?`, chapterID, limit)
	if err != nil {
		return nil, fmt.Errorf("store: list chapter versions: %w", err)
	}
	defer rows.Close()
	out := make([]ChapterVersion, 0, limit)
	for rows.Next() {
		var v ChapterVersion
		var created int64
		if err := rows.Scan(&v.ID, &v.ChapterID, &v.ChapterIndex, &v.Revision, &v.Title, &v.Content, &v.CharCount, &v.Reason, &created); err != nil {
			return nil, err
		}
		v.CreatedAt = fromUnixMillis(created)
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) chapterVersionLocked(chapterID string, revision int64) (ChapterVersion, error) {
	db, err := s.agentDatabase()
	if err != nil {
		return ChapterVersion{}, err
	}
	row := db.QueryRow(`SELECT id, chapter_id, chapter_index, revision, title, content, char_count, reason, created_at
		FROM chapter_version WHERE chapter_id = ? AND revision = ?`, chapterID, revision)
	var v ChapterVersion
	var created int64
	if err := row.Scan(&v.ID, &v.ChapterID, &v.ChapterIndex, &v.Revision, &v.Title, &v.Content, &v.CharCount, &v.Reason, &created); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ChapterVersion{}, ErrChapterVersionNotFound
		}
		return ChapterVersion{}, fmt.Errorf("store: load chapter version: %w", err)
	}
	v.CreatedAt = fromUnixMillis(created)
	return v, nil
}

// RestoreChapterVersion 回滚单章到历史版本；回滚前自动创建全书快照。
func (s *Store) RestoreChapterVersion(chapterID string, revision int64) (novel.Chapter, novel.Project, string, error) {
	s.mu.Lock()
	var change *ChapterChange
	defer func() {
		s.mu.Unlock()
		if change != nil {
			s.notifyChapterChange(*change)
		}
	}()

	project, err := s.loadProjectLocked()
	if err != nil {
		return novel.Chapter{}, novel.Project{}, "", err
	}
	metaIndex := -1
	for i, meta := range project.Chapters {
		if meta.ID == chapterID {
			metaIndex = i
			break
		}
	}
	if metaIndex < 0 {
		return novel.Chapter{}, novel.Project{}, "", ErrChapterNotFound
	}
	version, err := s.chapterVersionLocked(chapterID, revision)
	if err != nil {
		return novel.Chapter{}, novel.Project{}, "", err
	}
	snapshot, err := s.createSnapshotLocked("回滚前自动快照", "auto_before_chapter_rollback")
	if err != nil {
		return novel.Chapter{}, novel.Project{}, "", err
	}
	existing, err := s.readChapterLocked(chapterID)
	if err != nil {
		return novel.Chapter{}, novel.Project{}, "", err
	}
	now := time.Now().UTC()
	chapter := existing
	if strings.TrimSpace(version.Title) != "" {
		chapter.Title = version.Title
	}
	chapter.Content = version.Content
	chapter.CharCount = novel.CountChars(version.Content)
	chapter.Revision = existing.Revision + 1
	chapter.UpdatedAt = now
	if err := s.writeChapterLocked(chapter); err != nil {
		return novel.Chapter{}, novel.Project{}, "", err
	}
	if err := s.saveChapterVersionLocked(existing, ChapterVersionRollback); err != nil {
		log.Printf("store: chapter %s pre-rollback version not recorded (ignored): %v", chapter.ID, err)
	}
	project.Chapters[metaIndex] = chapter.Meta()
	invalidateAnalysisFrom(&project.Analysis, chapter.Index-1, now)
	project.Revision++
	project.UpdatedAt = now
	project.Normalize()
	if err := s.writeProjectLocked(project); err != nil {
		return novel.Chapter{}, novel.Project{}, "", err
	}
	if err := s.reconcileForChapter(chapter); err != nil {
		log.Printf("store: chapter %s restored but evidence reconcile skipped (ignored): %v", chapter.ID, err)
	}
	change = &ChapterChange{
		Kind:         ChapterSaved,
		ChapterID:    chapter.ID,
		Title:        chapter.Title,
		Index:        chapter.Index,
		CharCount:    chapter.CharCount,
		ChapterCount: len(project.Chapters),
	}
	return chapter, project, snapshot.ID, nil
}

// CreateSnapshot 创建全书快照（正文 + 分析产物 + 标题）。
func (s *Store) CreateSnapshot(name, reason string) (BookSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.createSnapshotLocked(name, reason)
}

func (s *Store) createSnapshotLocked(name, reason string) (BookSnapshot, error) {
	project, err := s.loadProjectLocked()
	if err != nil {
		return BookSnapshot{}, err
	}
	chapters := make([]novel.Chapter, 0, len(project.Chapters))
	charCount := 0
	for _, meta := range project.Chapters {
		chapter, err := s.readChapterLocked(meta.ID)
		if err != nil {
			return BookSnapshot{}, err
		}
		chapter.Index = meta.Index
		chapters = append(chapters, chapter)
		charCount += chapter.CharCount
	}
	if len(chapters) == 0 {
		return BookSnapshot{}, ErrSnapshotEmpty
	}
	knowledge, err := s.loadKnowledgeLocked()
	if err != nil {
		return BookSnapshot{}, err
	}
	knowledgeJSON, err := json.Marshal(knowledge)
	if err != nil {
		return BookSnapshot{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = "手动快照"
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "manual"
	}
	snapshot := BookSnapshot{
		ID:           novel.NewID("snap"),
		Name:         name,
		Reason:       reason,
		Title:        project.Title,
		ChapterCount: len(chapters),
		CharCount:    charCount,
		Knowledge:    knowledgeJSON,
		CreatedAt:    time.Now().UTC(),
	}
	db, err := s.agentDatabase()
	if err != nil {
		return BookSnapshot{}, err
	}
	tx, err := db.Begin()
	if err != nil {
		return BookSnapshot{}, fmt.Errorf("store: begin snapshot: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO book_snapshot
		(id, name, reason, title, chapter_count, char_count, knowledge_json, created_at)
		VALUES (?,?,?,?,?,?,?,?)`,
		snapshot.ID, snapshot.Name, snapshot.Reason, snapshot.Title, snapshot.ChapterCount,
		snapshot.CharCount, string(knowledgeJSON), unixMillis(snapshot.CreatedAt),
	); err != nil {
		return BookSnapshot{}, fmt.Errorf("store: insert snapshot: %w", err)
	}
	for _, chapter := range chapters {
		if _, err := tx.Exec(`INSERT INTO book_snapshot_chapter
			(snapshot_id, chapter_id, index_no, title, content, char_count, revision)
			VALUES (?,?,?,?,?,?,?)`,
			snapshot.ID, chapter.ID, chapter.Index, chapter.Title, chapter.Content, chapter.CharCount, chapter.Revision,
		); err != nil {
			return BookSnapshot{}, fmt.Errorf("store: insert snapshot chapter: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return BookSnapshot{}, fmt.Errorf("store: commit snapshot: %w", err)
	}
	return snapshot, nil
}

// ListSnapshots 返回快照头列表（新到旧），不含章节正文。
func (s *Store) ListSnapshots(limit int) ([]BookSnapshot, error) {
	db, err := s.agentDatabase()
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := db.Query(`SELECT id, name, reason, title, chapter_count, char_count, created_at
		FROM book_snapshot ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("store: list snapshots: %w", err)
	}
	defer rows.Close()
	out := make([]BookSnapshot, 0, limit)
	for rows.Next() {
		var snap BookSnapshot
		var created int64
		if err := rows.Scan(&snap.ID, &snap.Name, &snap.Reason, &snap.Title, &snap.ChapterCount, &snap.CharCount, &created); err != nil {
			return nil, err
		}
		snap.CreatedAt = fromUnixMillis(created)
		out = append(out, snap)
	}
	return out, rows.Err()
}

// GetSnapshot 返回快照头与全部章节。
func (s *Store) GetSnapshot(id string) (SnapshotDetail, error) {
	return s.getSnapshot(id)
}

func (s *Store) getSnapshot(id string) (SnapshotDetail, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return SnapshotDetail{}, ErrSnapshotNotFound
	}
	db, err := s.agentDatabase()
	if err != nil {
		return SnapshotDetail{}, err
	}
	var detail SnapshotDetail
	var created int64
	var knowledge string
	err = db.QueryRow(`SELECT id, name, reason, title, chapter_count, char_count, knowledge_json, created_at
		FROM book_snapshot WHERE id = ?`, id).Scan(
		&detail.ID, &detail.Name, &detail.Reason, &detail.Title, &detail.ChapterCount,
		&detail.CharCount, &knowledge, &created,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return SnapshotDetail{}, ErrSnapshotNotFound
	}
	if err != nil {
		return SnapshotDetail{}, fmt.Errorf("store: load snapshot: %w", err)
	}
	detail.Knowledge = json.RawMessage(knowledge)
	detail.CreatedAt = fromUnixMillis(created)
	rows, err := db.Query(`SELECT chapter_id, index_no, title, content, char_count, revision
		FROM book_snapshot_chapter WHERE snapshot_id = ? ORDER BY index_no`, id)
	if err != nil {
		return SnapshotDetail{}, fmt.Errorf("store: load snapshot chapters: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var chapter SnapshotChapter
		chapter.SnapshotID = id
		if err := rows.Scan(&chapter.ChapterID, &chapter.Index, &chapter.Title, &chapter.Content, &chapter.CharCount, &chapter.Revision); err != nil {
			return SnapshotDetail{}, err
		}
		detail.Chapters = append(detail.Chapters, chapter)
	}
	return detail, rows.Err()
}

// RestoreSnapshot 回滚全书到快照；回滚前自动创建快照并返回其 ID。
func (s *Store) RestoreSnapshot(id string) (novel.Project, string, error) {
	s.mu.Lock()
	var change *ChapterChange
	var afterOld, afterNew map[string]int
	defer func() {
		s.mu.Unlock()
		if afterOld != nil && chapterOrderChanged(afterOld, afterNew) {
			s.afterChapterSetChanged(afterOld, afterNew)
		}
		if change != nil {
			s.notifyChapterChange(*change)
		}
	}()

	detail, err := s.getSnapshot(id)
	if err != nil {
		return novel.Project{}, "", err
	}
	if len(detail.Chapters) == 0 {
		return novel.Project{}, "", ErrSnapshotEmpty
	}
	project, err := s.loadProjectLocked()
	if err != nil {
		return novel.Project{}, "", err
	}
	oldIndexes := chapterIndexMap(project.Chapters)
	auto, err := s.createSnapshotLocked("回滚前自动快照", "auto_before_book_rollback")
	if err != nil {
		return novel.Project{}, "", err
	}
	var knowledge novel.Knowledge
	if len(detail.Knowledge) > 0 {
		if err := json.Unmarshal(detail.Knowledge, &knowledge); err != nil {
			return novel.Project{}, "", fmt.Errorf("store: decode snapshot knowledge: %w", err)
		}
	}
	knowledge.Normalize()

	current := make(map[string]novel.Chapter, len(project.Chapters))
	for _, meta := range project.Chapters {
		chapter, err := s.readChapterLocked(meta.ID)
		if err != nil {
			return novel.Project{}, "", err
		}
		current[meta.ID] = chapter
	}
	now := time.Now().UTC()
	keep := make(map[string]struct{}, len(detail.Chapters))
	next := make([]novel.ChapterMeta, 0, len(detail.Chapters))
	for i, snapChapter := range detail.Chapters {
		chapter := novel.Chapter{
			ID:        snapChapter.ChapterID,
			Index:     i + 1,
			Title:     snapChapter.Title,
			Content:   snapChapter.Content,
			CharCount: novel.CountChars(snapChapter.Content),
			Revision:  max(snapChapter.Revision, 1),
			UpdatedAt: now,
		}
		if existing, ok := current[chapter.ID]; ok {
			chapter.Revision = existing.Revision + 1
			// 章节版本保留了回滚前的正文，便于二次回滚/对比。
			_ = s.saveChapterVersionLocked(existing, ChapterVersionRollback)
		}
		if err := s.writeChapterLocked(chapter); err != nil {
			return novel.Project{}, "", err
		}
		keep[chapter.ID] = struct{}{}
		next = append(next, chapter.Meta())
	}
	for id := range current {
		if _, ok := keep[id]; ok {
			continue
		}
		if path, err := s.chapterPath(id); err == nil {
			if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return novel.Project{}, "", fmt.Errorf("store: remove chapter not in snapshot: %w", err)
			}
		}
	}
	if err := s.writeKnowledgeLocked(knowledge); err != nil {
		return novel.Project{}, "", err
	}
	project.Chapters = next
	if strings.TrimSpace(detail.Title) != "" {
		project.Title = detail.Title
	}
	project.Analysis = novel.AnalysisState{
		Status:        novel.AnalysisPending,
		AnalyzedCount: 0,
		TotalCount:    len(next),
		UpdatedAt:     now,
	}
	project.Revision++
	project.UpdatedAt = now
	project.Normalize()
	if err := s.writeProjectLocked(project); err != nil {
		return novel.Project{}, "", err
	}
	afterOld = oldIndexes
	afterNew = chapterIndexMap(project.Chapters)
	change = &ChapterChange{
		Kind:         BookRestored,
		Title:        project.Title,
		ChapterCount: len(project.Chapters),
	}
	return project, auto.ID, nil
}

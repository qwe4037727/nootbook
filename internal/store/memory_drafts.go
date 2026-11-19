package store

import (
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"novelagent/internal/novel"
)

// 正文草稿区（G6，详设 §4/§8.2）。drafts.json 与 changesets.json 同属草稿区：
// 创建/编辑/拒绝不改变 memoryRevision；confirm 时把正文、project.json、草稿终态与
// ChangeSet 派生记忆放进同一次 commitMemoryLocked 提交，崩溃恢复按提交日志重放/回滚。

const draftsFile = "drafts.json"

var (
	ErrDraftNotFound     = errors.New("store: draft not found")
	ErrDraftInvalid      = errors.New("store: invalid draft")
	ErrDraftConflict     = errors.New("store: draft revision conflict")
	ErrDraftTerminal     = errors.New("store: draft is already terminal")
	ErrDraftNeedsRecheck = errors.New("store: draft changed after memory extraction")
	// ErrDraftMemoryStale 表示草稿正文在记忆同步后被修改，确认时必须显式选择重提取或仅提交正文（#63）。
	ErrDraftMemoryStale = errors.New("store: draft content changed after memory sync")
	ErrChapterConflict  = errors.New("store: chapter revision conflict")
	// ErrDraftManagedChangeSet 拒绝绕过草稿确认单独采纳/重定基草稿派生批次（G6 原子性）。
	ErrDraftManagedChangeSet = errors.New("store: changeset is managed by a draft")
)

type DraftCreateParams struct {
	ChapterID string // 空 = 新建章节（服务端预分配 ch_）
	Title     string
	Content   string
}

type DraftConfirmParams struct {
	DraftRevision           int64
	ExpectedChapterRevision int64
	ExpectedMemoryRevision  int64
	// AllowBodyOnly 明确允许「仅提交正文、记忆不更新」；正文 hash 与同步基线不一致时必填（#63）。
	AllowBodyOnly bool
}

type DraftCommitResult struct {
	Draft           novel.Draft          `json:"draft"`
	Chapter         novel.Chapter        `json:"chapter"`
	Project         novel.Project        `json:"project"`
	MemoryRevision  int64                `json:"memoryRevision"`
	CommitID        string               `json:"commitId"`
	ChangeSetID     string               `json:"memoryChangeSetId,omitempty"`
	MemorySynced    bool                 `json:"memorySynced"`
	MemorySyncState string               `json:"memorySyncState"`
	Conflicts       []novel.ConflictItem `json:"conflicts,omitempty"`
}

func (s *Store) loadDraftsLocked() ([]novel.Draft, error) {
	var drafts []novel.Draft
	err := s.readMemoryFileLocked(draftsFile, &drafts)
	if errors.Is(err, os.ErrNotExist) {
		return []novel.Draft{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("store: load drafts: %w", err)
	}
	if drafts == nil {
		drafts = []novel.Draft{}
	}
	return drafts, nil
}

func (s *Store) writeDraftsLocked(drafts []novel.Draft) error {
	if drafts == nil {
		drafts = []novel.Draft{}
	}
	if err := s.writeMemoryFileLocked(draftsFile, drafts); err != nil {
		return fmt.Errorf("store: save drafts: %w", err)
	}
	return nil
}

// LoadDraft 读取单个草稿（含正文）。
func (s *Store) LoadDraft(id string) (novel.Draft, error) {
	if !novel.ValidID(id) || !strings.HasPrefix(id, "draft_") {
		return novel.Draft{}, ErrDraftNotFound
	}
	s.memoryMu.Lock()
	defer s.memoryMu.Unlock()
	return s.getDraftLocked(id)
}

func (s *Store) getDraftLocked(id string) (novel.Draft, error) {
	drafts, err := s.loadDraftsLocked()
	if err != nil {
		return novel.Draft{}, err
	}
	for _, draft := range drafts {
		if draft.ID == id {
			return draft, nil
		}
	}
	return novel.Draft{}, ErrDraftNotFound
}

// LoadDrafts 按状态过滤草稿摘要（空 = 全部），按创建时间倒序。
func (s *Store) LoadDrafts(status string) ([]novel.DraftSummary, error) {
	s.memoryMu.Lock()
	defer s.memoryMu.Unlock()
	drafts, err := s.loadDraftsLocked()
	if err != nil {
		return nil, err
	}
	out := make([]novel.DraftSummary, 0, len(drafts))
	for i := len(drafts) - 1; i >= 0; i-- {
		if status != "" && string(drafts[i].Status) != status {
			continue
		}
		out = append(out, drafts[i].Summary())
	}
	return out, nil
}

// CreateDraft 创建待确认草稿。新章节在此预分配 ch_ ID，正文在 confirm 前不进入正式区。
func (s *Store) CreateDraft(p DraftCreateParams) (novel.Draft, error) {
	content := strings.TrimSpace(p.Content)
	if content == "" {
		return novel.Draft{}, fmt.Errorf("%w: content is empty", ErrDraftInvalid)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	project, err := s.loadProjectLocked()
	if err != nil {
		return novel.Draft{}, err
	}

	now := time.Now().UTC()
	draft := novel.Draft{
		ID:               novel.NewID("draft"),
		WorkID:           novel.InternalWorkID(),
		Status:           novel.DraftPending,
		Content:          content,
		Revision:         1,
		MemorySyncedHash: novel.ContentHash(content),
		MemorySyncState:  "pending",
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if p.ChapterID == "" {
		draft.NewChapter = true
		draft.TargetChapterID = novel.NewID("ch")
		draft.Title = strings.TrimSpace(p.Title)
		if draft.Title == "" {
			draft.Title = novel.DefaultChapterTitle(len(project.Chapters) + 1)
		}
	} else {
		chapter, err := s.readChapterLocked(p.ChapterID)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return novel.Draft{}, ErrChapterNotFound
			}
			return novel.Draft{}, err
		}
		draft.TargetChapterID = chapter.ID
		draft.BaseChapterRevision = chapter.Revision
		draft.Title = strings.TrimSpace(p.Title)
		if draft.Title == "" {
			draft.Title = chapter.Title
		}
	}

	s.memoryMu.Lock()
	defer s.memoryMu.Unlock()
	if s.memorySafe {
		return novel.Draft{}, fmt.Errorf("%w: %s", ErrMemorySafeState, s.memorySafeError)
	}
	meta, err := s.ensureMemoryMetaLocked()
	if err != nil {
		return novel.Draft{}, err
	}
	draft.BaseMemoryRevision = meta.MemoryRevision
	if err := novel.ValidateDraft(&draft); err != nil {
		return novel.Draft{}, fmt.Errorf("%w: %v", ErrDraftInvalid, err)
	}
	drafts, err := s.loadDraftsLocked()
	if err != nil {
		return novel.Draft{}, err
	}
	drafts = append(drafts, draft)
	if err := s.writeDraftsLocked(drafts); err != nil {
		return novel.Draft{}, err
	}
	return draft, nil
}

// UpdateDraft 编辑待确认稿：draftRevision+1 并解除既有 ChangeSet 关联（必须重新提取）。
func (s *Store) UpdateDraft(id string, expectedRevision int64, title, content string) (novel.Draft, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return novel.Draft{}, fmt.Errorf("%w: content is empty", ErrDraftInvalid)
	}
	s.memoryMu.Lock()
	defer s.memoryMu.Unlock()

	drafts, err := s.loadDraftsLocked()
	if err != nil {
		return novel.Draft{}, err
	}
	index := -1
	for i := range drafts {
		if drafts[i].ID == id {
			index = i
			break
		}
	}
	if index < 0 {
		return novel.Draft{}, ErrDraftNotFound
	}
	draft := drafts[index]
	if draft.Status.IsTerminal() {
		return novel.Draft{}, fmt.Errorf("%w: %s", ErrDraftTerminal, draft.Status)
	}
	if expectedRevision != draft.Revision {
		return novel.Draft{}, fmt.Errorf("%w: draftRevision=%d current=%d", ErrDraftConflict, expectedRevision, draft.Revision)
	}
	if strings.TrimSpace(title) != "" {
		draft.Title = strings.TrimSpace(title)
	}
	draft.Content = content
	draft.Revision++
	draft.UpdatedAt = time.Now().UTC()
	// #63：正文变化后记忆基线失效；确认时必须显式选择重提取或仅提交正文。
	if draft.MemorySyncedHash != "" && draft.MemorySyncedHash != novel.ContentHash(content) {
		draft.MemorySyncState = "stale"
	}
	if draft.ChangeSetID != "" {
		if err := s.rejectChangeSetIfProposedLocked(draft.ChangeSetID, "superseded_by_recheck"); err != nil {
			return novel.Draft{}, err
		}
	}
	draft.ChangeSetID = ""
	if err := novel.ValidateDraft(&draft); err != nil {
		return novel.Draft{}, fmt.Errorf("%w: %v", ErrDraftInvalid, err)
	}
	drafts[index] = draft
	if err := s.writeDraftsLocked(drafts); err != nil {
		return novel.Draft{}, err
	}
	return draft, nil
}

// RejectDraft 废弃待确认稿，并驳回其关联的 proposed ChangeSet，避免孤立记忆变更被误采纳。
func (s *Store) RejectDraft(id, reason string) (novel.Draft, error) {
	s.memoryMu.Lock()
	defer s.memoryMu.Unlock()

	drafts, err := s.loadDraftsLocked()
	if err != nil {
		return novel.Draft{}, err
	}
	index := -1
	for i := range drafts {
		if drafts[i].ID == id {
			index = i
			break
		}
	}
	if index < 0 {
		return novel.Draft{}, ErrDraftNotFound
	}
	draft := drafts[index]
	if draft.Status.IsTerminal() {
		return novel.Draft{}, fmt.Errorf("%w: %s", ErrDraftTerminal, draft.Status)
	}
	now := time.Now().UTC()
	draft.Status = novel.DraftRejected
	draft.RejectedAt = now
	draft.UpdatedAt = now
	draft.RejectReason = truncate(strings.TrimSpace(reason), 200)
	drafts[index] = draft
	if err := s.writeDraftsLocked(drafts); err != nil {
		return novel.Draft{}, err
	}
	if draft.ChangeSetID != "" {
		if err := s.rejectChangeSetIfProposedLocked(draft.ChangeSetID, "draft_rejected"); err != nil {
			log.Printf("store: draft %s rejected but changeset %s reject failed: %v", draft.ID, draft.ChangeSetID, err)
		}
	}
	return draft, nil
}

func (s *Store) rejectChangeSetIfProposedLocked(id, reason string) error {
	if id == "" {
		return nil
	}
	sets, err := s.loadChangeSetsLocked()
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	for i := range sets {
		if sets[i].ID != id {
			continue
		}
		if sets[i].Status != novel.ChangeSetProposed {
			return nil
		}
		sets[i].Status = novel.ChangeSetRejected
		sets[i].RejectedAt = now
		sets[i].RejectReason = reason
		return s.writeChangeSetsLocked(sets)
	}
	return nil
}

// StashDraftChangeSet 暂存草稿提取结果并回填 draft.ChangeSetID。
// 同一草稿重复提取时，旧的 proposed ChangeSet 置 rejected（superseded_by_recheck）。
func (s *Store) StashDraftChangeSet(draftID string, expectedRevision int64, cs novel.ChangeSet, candidates []novel.EntityCandidate) (novel.Draft, novel.ChangeSet, error) {
	s.memoryMu.Lock()
	defer s.memoryMu.Unlock()
	if s.memorySafe {
		return novel.Draft{}, novel.ChangeSet{}, fmt.Errorf("%w: %s", ErrMemorySafeState, s.memorySafeError)
	}

	drafts, err := s.loadDraftsLocked()
	if err != nil {
		return novel.Draft{}, novel.ChangeSet{}, err
	}
	index := -1
	for i := range drafts {
		if drafts[i].ID == draftID {
			index = i
			break
		}
	}
	if index < 0 {
		return novel.Draft{}, novel.ChangeSet{}, ErrDraftNotFound
	}
	draft := drafts[index]
	if draft.Status.IsTerminal() {
		return novel.Draft{}, novel.ChangeSet{}, fmt.Errorf("%w: %s", ErrDraftTerminal, draft.Status)
	}
	if expectedRevision != draft.Revision {
		return novel.Draft{}, novel.ChangeSet{}, fmt.Errorf("%w: draftRevision=%d current=%d", ErrDraftConflict, expectedRevision, draft.Revision)
	}
	meta, err := s.ensureMemoryMetaLocked()
	if err != nil {
		return novel.Draft{}, novel.ChangeSet{}, err
	}
	if cs.BasedOnRevision != meta.MemoryRevision {
		return novel.Draft{}, novel.ChangeSet{}, fmt.Errorf("%w: changeset basedOn=%d current=%d", ErrRevisionConflict, cs.BasedOnRevision, meta.MemoryRevision)
	}
	if cs.ChapterID != draft.TargetChapterID || cs.ChapterRevision != draft.PendingChapterRevision() {
		return novel.Draft{}, novel.ChangeSet{}, fmt.Errorf("%w: changeset chapter %s rev %d does not match draft target %s rev %d",
			ErrDraftNeedsRecheck, cs.ChapterID, cs.ChapterRevision, draft.TargetChapterID, draft.PendingChapterRevision())
	}

	sets, err := s.loadChangeSetsLocked()
	if err != nil {
		return novel.Draft{}, novel.ChangeSet{}, err
	}
	now := time.Now().UTC()
	for i := range sets {
		if sets[i].ID == draft.ChangeSetID && sets[i].Status == novel.ChangeSetProposed {
			sets[i].Status = novel.ChangeSetRejected
			sets[i].RejectedAt = now
			sets[i].RejectReason = "superseded_by_recheck"
		}
	}
	if cs.ID == "" {
		cs.ID = novel.NewID("mcs")
	}
	cs.Status = novel.ChangeSetProposed
	cs.Proposer = "deep_extract"
	cs.DraftID = draft.ID
	cs.DraftRevision = draft.Revision
	cs.WorkID = novel.InternalWorkID()
	if cs.CreatedAt.IsZero() {
		cs.CreatedAt = now
	}
	if err := novel.ValidateChangeSet(&cs); err != nil {
		return novel.Draft{}, novel.ChangeSet{}, fmt.Errorf("%w: %v", ErrInvalidChangeSet, err)
	}
	sets = append(sets, cs)

	if _, err := s.mergeCandidatesForDraftLocked(candidates); err != nil {
		return novel.Draft{}, novel.ChangeSet{}, err
	}

	if err := s.writeChangeSetsLocked(sets); err != nil {
		return novel.Draft{}, novel.ChangeSet{}, err
	}
	draft.ChangeSetID = cs.ID
	draft.BaseMemoryRevision = meta.MemoryRevision
	draft.MemorySyncedHash = novel.ContentHash(draft.Content)
	draft.MemorySyncState = "synced"
	draft.UpdatedAt = now
	drafts[index] = draft
	if err := s.writeDraftsLocked(drafts); err != nil {
		return novel.Draft{}, novel.ChangeSet{}, err
	}
	return draft, cs, nil
}

// mergeCandidatesForDraftLocked 合并实体候选；与 saveChangeSetLocked 相同的幂等语义。
func (s *Store) mergeCandidatesForDraftLocked(candidates []novel.EntityCandidate) (int, error) {
	if len(candidates) == 0 {
		return 0, nil
	}
	existing, err := s.loadEntityCandidatesLocked()
	if err != nil {
		return 0, err
	}
	merged, n := mergeCandidates(existing, candidates)
	if n == 0 {
		return 0, nil
	}
	if err := s.writeEntityCandidatesLocked(merged); err != nil {
		return 0, err
	}
	return n, nil
}

// ConfirmDraft 原子确认草稿：正文/项目元数据/草稿终态/ChangeSet 派生记忆在一次
// memoryRevision 提交中落盘。锁序 store.mu → memoryMu（全仓库唯一方向）。
func (s *Store) ConfirmDraft(id string, p DraftConfirmParams) (DraftCommitResult, error) {
	if !novel.ValidID(id) || !strings.HasPrefix(id, "draft_") {
		return DraftCommitResult{}, ErrDraftNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	project, err := s.loadProjectLocked()
	if err != nil {
		return DraftCommitResult{}, err
	}
	if project.Analysis.Status == novel.AnalysisRunning {
		return DraftCommitResult{}, ErrAnalysisRunning
	}

	s.memoryMu.Lock()
	defer s.memoryMu.Unlock()
	if s.memorySafe {
		return DraftCommitResult{}, fmt.Errorf("%w: %s", ErrMemorySafeState, s.memorySafeError)
	}
	meta, err := s.ensureMemoryMetaLocked()
	if err != nil {
		return DraftCommitResult{}, err
	}
	drafts, err := s.loadDraftsLocked()
	if err != nil {
		return DraftCommitResult{}, err
	}
	draftIndex := -1
	for i := range drafts {
		if drafts[i].ID == id {
			draftIndex = i
			break
		}
	}
	if draftIndex < 0 {
		return DraftCommitResult{}, ErrDraftNotFound
	}
	draft := drafts[draftIndex]

	// 幂等重放：同一草稿已 committed → 返回原结果，不再递增。
	if draft.Status == novel.DraftCommitted {
		chapter, err := s.readChapterLocked(draft.TargetChapterID)
		if err != nil {
			return DraftCommitResult{}, err
		}
		return DraftCommitResult{
			Draft:           draft,
			Chapter:         chapter,
			Project:         project,
			MemoryRevision:  meta.MemoryRevision,
			CommitID:        draft.CommitID,
			ChangeSetID:     draft.ChangeSetID,
			MemorySynced:    draft.MemorySyncState == "synced",
			MemorySyncState: draft.MemorySyncState,
		}, nil
	}
	if draft.Status.IsTerminal() {
		return DraftCommitResult{}, fmt.Errorf("%w: %s", ErrDraftTerminal, draft.Status)
	}
	if p.DraftRevision != draft.Revision {
		return DraftCommitResult{}, fmt.Errorf("%w: draftRevision=%d current=%d", ErrDraftConflict, p.DraftRevision, draft.Revision)
	}
	// #63 提交闸门：正文在记忆同步后被修改 → 必须显式二选一（重提取 / 仅正文）。
	staleContent := draft.MemorySyncedHash != "" && draft.MemorySyncedHash != novel.ContentHash(draft.Content)
	if staleContent && !p.AllowBodyOnly {
		return DraftCommitResult{}, fmt.Errorf("%w: content changed after memory sync; re-extract or allow body-only commit", ErrDraftMemoryStale)
	}
	bodyOnly := staleContent || draft.ChangeSetID == ""
	if p.ExpectedMemoryRevision > 0 && p.ExpectedMemoryRevision != meta.MemoryRevision {
		return DraftCommitResult{}, fmt.Errorf("%w: expected=%d current=%d", ErrRevisionConflict, p.ExpectedMemoryRevision, meta.MemoryRevision)
	}

	now := time.Now().UTC()
	var chapter novel.Chapter
	metaIndex := -1
	for i := range project.Chapters {
		if project.Chapters[i].ID == draft.TargetChapterID {
			metaIndex = i
			break
		}
	}
	if draft.NewChapter {
		if metaIndex >= 0 {
			return DraftCommitResult{}, fmt.Errorf("%w: target chapter %s already exists", ErrChapterConflict, draft.TargetChapterID)
		}
		chapter = novel.Chapter{
			ID:        draft.TargetChapterID,
			Index:     len(project.Chapters) + 1,
			Title:     draft.Title,
			Content:   draft.Content,
			CharCount: novel.CountChars(draft.Content),
			Revision:  1,
			UpdatedAt: now,
		}
	} else {
		if metaIndex < 0 {
			return DraftCommitResult{}, ErrChapterNotFound
		}
		existing, err := s.readChapterLocked(draft.TargetChapterID)
		if err != nil {
			return DraftCommitResult{}, err
		}
		if existing.Revision != draft.BaseChapterRevision {
			return DraftCommitResult{}, fmt.Errorf("%w: basedOn=%d current=%d", ErrChapterConflict, draft.BaseChapterRevision, existing.Revision)
		}
		if p.ExpectedChapterRevision > 0 && p.ExpectedChapterRevision != existing.Revision {
			return DraftCommitResult{}, fmt.Errorf("%w: expected=%d current=%d", ErrChapterConflict, p.ExpectedChapterRevision, existing.Revision)
		}
		s.recordChapterVersion(existing, ChapterVersionDraftCommit)
		chapter = existing
		if strings.TrimSpace(draft.Title) != "" {
			chapter.Title = strings.TrimSpace(draft.Title)
		}
		chapter.Content = draft.Content
		chapter.CharCount = novel.CountChars(draft.Content)
		chapter.Revision = existing.Revision + 1
		chapter.UpdatedAt = now
	}
	if err := novel.ValidateDraft(&draft); err != nil {
		return DraftCommitResult{}, fmt.Errorf("%w: %v", ErrDraftInvalid, err)
	}

	// ChangeSet（可选）：必须是本草稿、本版本、基于当前 memoryRevision 的 proposed 批次。
	var cs *novel.ChangeSet
	sets, err := s.loadChangeSetsLocked()
	if err != nil {
		return DraftCommitResult{}, err
	}
	if draft.ChangeSetID != "" && !bodyOnly {
		for i := range sets {
			if sets[i].ID != draft.ChangeSetID {
				continue
			}
			candidate := sets[i]
			cs = &candidate
			break
		}
		if cs == nil {
			return DraftCommitResult{}, fmt.Errorf("%w: changeset %s missing", ErrDraftNeedsRecheck, draft.ChangeSetID)
		}
		if cs.Status != novel.ChangeSetProposed {
			return DraftCommitResult{}, fmt.Errorf("%w: changeset is %s", ErrDraftNeedsRecheck, cs.Status)
		}
		if cs.DraftID != draft.ID || cs.DraftRevision != draft.Revision {
			return DraftCommitResult{}, fmt.Errorf("%w: changeset was built for draft %s rev %d", ErrDraftNeedsRecheck, cs.DraftID, cs.DraftRevision)
		}
		if cs.BasedOnRevision != meta.MemoryRevision {
			return DraftCommitResult{}, fmt.Errorf("%w: changeset basedOn=%d current=%d", ErrRevisionConflict, cs.BasedOnRevision, meta.MemoryRevision)
		}
		if cs.ChapterID != chapter.ID || cs.ChapterRevision != chapter.Revision {
			return DraftCommitResult{}, fmt.Errorf("%w: changeset chapter %s rev %d, target %s rev %d",
				ErrDraftNeedsRecheck, cs.ChapterID, cs.ChapterRevision, chapter.ID, chapter.Revision)
		}
	}

	events, err := s.loadMemoryEventsLocked()
	if err != nil {
		return DraftCommitResult{}, err
	}
	states, err := s.loadMemoryStatesLocked()
	if err != nil {
		return DraftCommitResult{}, err
	}
	cognitions, err := s.loadMemoryCognitionsLocked()
	if err != nil {
		return DraftCommitResult{}, err
	}
	threads, err := s.loadMemoryThreadsLocked()
	if err != nil {
		return DraftCommitResult{}, err
	}
	plans, err := s.loadMemoryPlansLocked()
	if err != nil {
		return DraftCommitResult{}, err
	}

	chapterContents := map[string]chapterSnap{
		chapter.ID: {Content: chapter.Content, Revision: chapter.Revision},
	}
	conflicts := []novel.ConflictItem(nil)
	if cs != nil {
		conflicts = append(conflicts, cs.Conflicts...)
		for i := range cs.Ops {
			op := cs.Ops[i]
			switch op.Op.File() {
			case "events.json":
				_, newConflicts, opErr := s.applyEventOp(&op, &events, chapterContents)
				if opErr != nil {
					return DraftCommitResult{}, opErr
				}
				conflicts = append(conflicts, newConflicts...)
			case "states.json":
				_, newConflicts, opErr := s.applyStateOp(&op, &states, chapterContents)
				if opErr != nil {
					return DraftCommitResult{}, opErr
				}
				conflicts = append(conflicts, newConflicts...)
			case "cognitions.json":
				if _, opErr := s.applyCognitionOp(&op, &cognitions); opErr != nil {
					return DraftCommitResult{}, opErr
				}
			case "threads.json":
				_, newConflicts, opErr := s.applyThreadOp(&op, &threads, chapterContents)
				if opErr != nil {
					return DraftCommitResult{}, opErr
				}
				conflicts = append(conflicts, newConflicts...)
			case "plans.json":
				if _, opErr := s.applyPlanOp(&op, &plans); opErr != nil {
					return DraftCommitResult{}, opErr
				}
			default:
				return DraftCommitResult{}, fmt.Errorf("%w: unknown op file %q", ErrInvalidChangeSet, op.Op.File())
			}
		}
	}

	// G4 联动：改既有章节时，未被本批 ops 覆盖的旧证据同样需要标记（与正文同一次提交）。
	reconcileMetas(func(i int) *novel.MemoryMeta { return &events[i].MemoryMeta }, len(events), chapterContents)
	reconcileMetas(func(i int) *novel.MemoryMeta { return &states[i].MemoryMeta }, len(states), chapterContents)
	reconcileMetas(func(i int) *novel.MemoryMeta { return &cognitions[i].MemoryMeta }, len(cognitions), chapterContents)
	reconcileMetas(func(i int) *novel.MemoryMeta { return &threads[i].MemoryMeta }, len(threads), chapterContents)
	reconcileMetas(func(i int) *novel.MemoryMeta { return &plans[i].MemoryMeta }, len(plans), chapterContents)
	propagateReviewDependencies(events, states, cognitions, threads, plans, now)
	markCompletedParentHints(plans, now)

	commitID := novel.NewID("commit")
	payload := make(map[string]any, 9)
	if len(events) > 0 {
		payload["events.json"] = events
	}
	if len(states) > 0 {
		payload["states.json"] = states
	}
	if len(cognitions) > 0 {
		payload["cognitions.json"] = cognitions
	}
	if len(threads) > 0 {
		payload["threads.json"] = threads
	}
	if len(plans) > 0 {
		payload["plans.json"] = plans
	}
	if cs != nil {
		for i := range sets {
			if sets[i].ID != cs.ID {
				continue
			}
			sets[i].Status = novel.ChangeSetApplied
			sets[i].CommitID = commitID
			sets[i].AppliedAt = now
			sets[i].Conflicts = conflicts
			break
		}
		payload["changesets.json"] = sets
	}

	draft.Status = novel.DraftCommitted
	draft.CommitID = commitID
	draft.CommittedAt = now
	draft.UpdatedAt = now
	draft.ChapterRevision = chapter.Revision
	if bodyOnly {
		draft.MemorySyncState = "body_only"
	} else {
		draft.MemorySyncState = "synced"
		draft.MemorySyncedHash = novel.ContentHash(draft.Content)
	}
	drafts[draftIndex] = draft
	payload["drafts.json"] = drafts

	if draft.NewChapter {
		project.Chapters = append(project.Chapters, chapter.Meta())
	} else {
		project.Chapters[metaIndex] = chapter.Meta()
	}
	state := project.Analysis
	state.AnalyzedCount = min(state.AnalyzedCount, chapter.Index-1)
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
	payload["project.json"] = project
	payload["chapter:"+chapter.ID] = chapter

	record, err := s.commitMemoryWithIDLocked(CommitFormal, meta.MemoryRevision, payload, commitID)
	if err != nil {
		return DraftCommitResult{}, err
	}
	return DraftCommitResult{
		Draft:           draft,
		Chapter:         chapter,
		Project:         project,
		MemoryRevision:  record.TargetMemoryRevision,
		CommitID:        record.CommitID,
		ChangeSetID:     draft.ChangeSetID,
		MemorySynced:    !bodyOnly,
		MemorySyncState: draft.MemorySyncState,
		Conflicts:       conflicts,
	}, nil
}

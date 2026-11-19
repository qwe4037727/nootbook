package novel

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// Draft 为续写正文的待确认稿（详设 §4.1）。草稿区数据独立于正式章节与正式记忆：
// 创建/编辑/拒绝不改变 project.json、chapters/ 与 memoryRevision；只有 confirm
// 才通过统一提交协议一次性落正文、项目元数据与记忆变更。
type DraftStatus string

const (
	DraftPending   DraftStatus = "pending_confirmation"
	DraftCommitted DraftStatus = "committed"
	DraftRejected  DraftStatus = "rejected"
)

func (s DraftStatus) Valid() bool {
	switch s {
	case DraftPending, DraftCommitted, DraftRejected:
		return true
	}
	return false
}

func (s DraftStatus) IsTerminal() bool {
	return s == DraftCommitted || s == DraftRejected
}

type Draft struct {
	ID                  string      `json:"id"` // draft_
	WorkID              string      `json:"workId"`
	TargetChapterID     string      `json:"targetChapterId"` // 新章节预分配 ch_；既有章节为原 ID
	NewChapter          bool        `json:"newChapter"`
	BaseChapterRevision int64       `json:"baseChapterRevision"` // 既有章节的基线版本；新章节为 0
	BaseMemoryRevision  int64       `json:"baseMemoryRevision"`
	Status              DraftStatus `json:"status"`
	Title               string      `json:"title"`
	Content             string      `json:"content"`
	Revision            int64       `json:"draftRevision"`
	ChangeSetID         string      `json:"memoryChangeSetId,omitempty"`
	MemorySyncedHash    string      `json:"memorySyncedHash,omitempty"` // 最近一次记忆同步时的正文 hash（#63 提交闸门基线）
	MemorySyncState     string      `json:"memorySyncState,omitempty"`  // pending / synced / stale / body_only
	CommitID            string      `json:"commitId,omitempty"`
	ChapterRevision     int64       `json:"chapterRevision,omitempty"` // 提交成功后的正式章节版本
	CreatedAt           time.Time   `json:"createdAt"`
	UpdatedAt           time.Time   `json:"updatedAt"`
	CommittedAt         time.Time   `json:"committedAt,omitempty"`
	RejectedAt          time.Time   `json:"rejectedAt,omitempty"`
	RejectReason        string      `json:"rejectReason,omitempty"`
}

// DraftSummary 为列表视图（不含正文全文）。
type DraftSummary struct {
	ID              string      `json:"id"`
	TargetChapterID string      `json:"targetChapterId"`
	NewChapter      bool        `json:"newChapter"`
	Status          DraftStatus `json:"status"`
	Title           string      `json:"title"`
	Revision        int64       `json:"draftRevision"`
	ChangeSetID     string      `json:"memoryChangeSetId,omitempty"`
	CommitID        string      `json:"commitId,omitempty"`
	CreatedAt       time.Time   `json:"createdAt"`
	UpdatedAt       time.Time   `json:"updatedAt"`
}

func (d Draft) Summary() DraftSummary {
	return DraftSummary{
		ID:              d.ID,
		TargetChapterID: d.TargetChapterID,
		NewChapter:      d.NewChapter,
		Status:          d.Status,
		Title:           d.Title,
		Revision:        d.Revision,
		ChangeSetID:     d.ChangeSetID,
		CommitID:        d.CommitID,
		CreatedAt:       d.CreatedAt,
		UpdatedAt:       d.UpdatedAt,
	}
}

// PendingChapterRevision 返回确认后目标章节应达到的版本号。
func (d Draft) PendingChapterRevision() int64 {
	if d.NewChapter {
		return 1
	}
	return d.BaseChapterRevision + 1
}

// ContentHash 返回草稿正文的 SHA-256（十六进制），用于 #63 提交闸门判断正文是否变化。
func ContentHash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

func ValidateDraft(d *Draft) error {
	if !ValidID(d.ID) || !strings.HasPrefix(d.ID, "draft_") {
		return fmt.Errorf("draft id %q must be draft_ id", d.ID)
	}
	if !d.Status.Valid() {
		return fmt.Errorf("draft status %q is not allowed", d.Status)
	}
	if !ValidID(d.TargetChapterID) || !strings.HasPrefix(d.TargetChapterID, "ch_") {
		return fmt.Errorf("targetChapterId %q must be ch_ id", d.TargetChapterID)
	}
	if d.Revision < 1 {
		return fmt.Errorf("draftRevision=%d want >=1", d.Revision)
	}
	if d.NewChapter && d.BaseChapterRevision != 0 {
		return fmt.Errorf("new chapter draft must not carry baseChapterRevision=%d", d.BaseChapterRevision)
	}
	if !d.NewChapter && d.BaseChapterRevision < 1 {
		return fmt.Errorf("existing chapter draft requires baseChapterRevision>=1")
	}
	if utf8.RuneCountInString(d.Title) > 200 {
		return fmt.Errorf("draft title exceeds 200 runes")
	}
	if strings.TrimSpace(d.Content) == "" {
		return fmt.Errorf("draft content is empty")
	}
	if d.ChangeSetID != "" && !strings.HasPrefix(d.ChangeSetID, "mcs_") {
		return fmt.Errorf("memoryChangeSetId %q must be mcs_ id", d.ChangeSetID)
	}
	return nil
}

package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"novelagent/internal/novel"
)

// ImageAsset 是一次图像生成请求与结果的持久化记录（image_asset 表）。
// pending/running 行同时充当图像任务队列；file_key 落盘后才写入。
type ImageAsset struct {
	ID               string           `json:"id"`
	Kind             string           `json:"kind"`
	EntryID          string           `json:"entryId,omitempty"`
	ChapterID        string           `json:"chapterId,omitempty"`
	AnchorOffset     int              `json:"anchorOffset,omitempty"`
	FileKey          string           `json:"fileKey,omitempty"`
	ThumbKey         string           `json:"thumbKey,omitempty"`
	Prompt           json.RawMessage  `json:"prompt"`
	Features         []string         `json:"features,omitempty"`
	CoreFeatures     []string         `json:"coreFeatures,omitempty"`
	Model            string           `json:"model,omitempty"`
	Backend          string           `json:"backend,omitempty"`
	StyleTag         string           `json:"styleTag,omitempty"`
	NegativePrompt   string           `json:"negativePrompt,omitempty"`
	Width            int              `json:"width,omitempty"`
	Height           int              `json:"height,omitempty"`
	Steps            int              `json:"steps,omitempty"`
	Seed             int64            `json:"seed,omitempty"`
	IsAnchor         bool             `json:"isAnchor"`
	ParentAsset      string           `json:"parentAsset,omitempty"`
	VariantOf        string           `json:"variantOf,omitempty"`
	ConsistencyScore float64          `json:"consistencyScore"`
	ConsistencyNote  string           `json:"consistencyNote,omitempty"`
	VisualProxy      bool             `json:"visualProxy"`
	Appearance       *ImageAppearance `json:"appearance,omitempty"`
	Status           string           `json:"status"`
	Error            string           `json:"error,omitempty"`
	DurationMs       int64            `json:"durationMs"`
	CostUSD          float64          `json:"costUsd"`
	CreatedAt        time.Time        `json:"createdAt"`
	UpdatedAt        time.Time        `json:"updatedAt"`
}

type ImageAppearance struct {
	Status   string   `json:"status"`
	Reason   string   `json:"reason,omitempty"`
	AnchorID string   `json:"anchorId,omitempty"`
	Model    string   `json:"model,omitempty"`
	Evidence []string `json:"evidence,omitempty"`
}

// ImageAssetQuery 约束资产列表查询。
type ImageAssetQuery struct {
	Kind      string
	EntryID   string
	ChapterID string
	Status    string
	Limit     int
}

// ImageAssetResult 是生成完成后一次性写回的字段集合。
type ImageAssetResult struct {
	FileKey          string
	ThumbKey         string
	Model            string
	Backend          string
	StyleTag         string
	Width            int
	Height           int
	Steps            int
	Seed             int64
	ConsistencyScore float64
	ConsistencyNote  string
	VisualProxy      bool
	Appearance       *ImageAppearance
	DurationMs       int64
	CostUSD          float64
}

var ErrImageAssetNotFound = errors.New("store: image asset not found")

const (
	ImageAssetPending = "pending"
	ImageAssetRunning = "running"
	ImageAssetDone    = "done"
	ImageAssetFailed  = "failed"
)

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

// CreateImageAsset 写入一条 pending 资产（即入队）。
func (s *Store) CreateImageAsset(rec ImageAsset) (ImageAsset, error) {
	db, err := s.agentDatabase()
	if err != nil {
		return ImageAsset{}, err
	}
	if rec.ID == "" {
		rec.ID = novel.NewID("img")
	}
	if rec.Status == "" {
		rec.Status = "pending"
	}
	now := time.Now().UTC()
	if rec.CreatedAt.IsZero() {
		rec.CreatedAt = now
	}
	rec.UpdatedAt = now
	if len(rec.Prompt) == 0 {
		rec.Prompt = json.RawMessage("{}")
	}
	if rec.Features == nil {
		rec.Features = []string{}
	}
	if rec.CoreFeatures == nil {
		rec.CoreFeatures = []string{}
	}
	features, _ := json.Marshal(rec.Features)
	core, _ := json.Marshal(rec.CoreFeatures)
	_, err = db.Exec(`INSERT INTO image_asset (
		id, kind, entry_id, chapter_id, anchor_offset, file_key, thumb_key, prompt_json,
		features_json, core_features_json, model, backend, style_tag, negative_prompt,
		width, height, steps, seed, is_anchor, parent_asset, variant_of,
		consistency_score, consistency_note, visual_proxy, status, error,
		duration_ms, cost_usd, created_at, updated_at
	) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		rec.ID, rec.Kind, rec.EntryID, rec.ChapterID, rec.AnchorOffset, rec.FileKey, rec.ThumbKey,
		string(rec.Prompt), string(features), string(core), rec.Model, rec.Backend, rec.StyleTag,
		rec.NegativePrompt, rec.Width, rec.Height, rec.Steps, rec.Seed, boolInt(rec.IsAnchor),
		rec.ParentAsset, rec.VariantOf, rec.ConsistencyScore, rec.ConsistencyNote,
		boolInt(rec.VisualProxy), rec.Status, rec.Error, rec.DurationMs, rec.CostUSD,
		unixMillis(rec.CreatedAt), unixMillis(rec.UpdatedAt),
	)
	if err != nil {
		return ImageAsset{}, fmt.Errorf("store: create image asset: %w", err)
	}
	return rec, nil
}

const imageColumns = `id, kind, entry_id, chapter_id, anchor_offset, file_key, thumb_key, prompt_json,
	features_json, core_features_json, model, backend, style_tag, negative_prompt,
	width, height, steps, seed, is_anchor, parent_asset, variant_of,
	consistency_score, consistency_note, visual_proxy, status, error,
	duration_ms, cost_usd, created_at, updated_at, appearance_json`

func scanImageAsset(scan func(dest ...any) error) (ImageAsset, error) {
	var (
		rec                        ImageAsset
		prompt                     string
		features, core, appearance string
		isAnchor, proxy            int
		created, updated           int64
	)
	if err := scan(&rec.ID, &rec.Kind, &rec.EntryID, &rec.ChapterID, &rec.AnchorOffset,
		&rec.FileKey, &rec.ThumbKey, &prompt, &features, &core, &rec.Model, &rec.Backend,
		&rec.StyleTag, &rec.NegativePrompt, &rec.Width, &rec.Height, &rec.Steps, &rec.Seed,
		&isAnchor, &rec.ParentAsset, &rec.VariantOf, &rec.ConsistencyScore, &rec.ConsistencyNote,
		&proxy, &rec.Status, &rec.Error, &rec.DurationMs, &rec.CostUSD, &created, &updated, &appearance); err != nil {
		return ImageAsset{}, err
	}
	if err := json.Unmarshal([]byte(appearance), &rec.Appearance); err != nil {
		return ImageAsset{}, fmt.Errorf("store: decode image appearance: %w", err)
	}
	rec.Prompt = json.RawMessage(prompt)
	_ = json.Unmarshal([]byte(features), &rec.Features)
	_ = json.Unmarshal([]byte(core), &rec.CoreFeatures)
	if rec.Features == nil {
		rec.Features = []string{}
	}
	if rec.CoreFeatures == nil {
		rec.CoreFeatures = []string{}
	}
	rec.IsAnchor = isAnchor != 0
	rec.VisualProxy = proxy != 0
	rec.CreatedAt = fromUnixMillis(created)
	rec.UpdatedAt = fromUnixMillis(updated)
	return rec, nil
}

// GetImageAsset 返回单条资产。
func (s *Store) GetImageAsset(id string) (ImageAsset, error) {
	db, err := s.agentDatabase()
	if err != nil {
		return ImageAsset{}, err
	}
	row := db.QueryRow(`SELECT `+imageColumns+` FROM image_asset WHERE id = ?`, id)
	rec, err := scanImageAsset(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return ImageAsset{}, ErrImageAssetNotFound
	}
	if err != nil {
		return ImageAsset{}, fmt.Errorf("store: get image asset: %w", err)
	}
	return rec, nil
}

// ListImageAssets 按 kind/entry/chapter/status 过滤，默认倒序 100 条。
func (s *Store) ListImageAssets(q ImageAssetQuery) ([]ImageAsset, error) {
	db, err := s.agentDatabase()
	if err != nil {
		return nil, err
	}
	limit := q.Limit
	if limit <= 0 || limit > 2000 {
		limit = 100
	}
	query := `SELECT ` + imageColumns + ` FROM image_asset WHERE 1=1`
	args := make([]any, 0, 5)
	if v := strings.TrimSpace(q.Kind); v != "" {
		query += ` AND kind = ?`
		args = append(args, v)
	}
	if v := strings.TrimSpace(q.EntryID); v != "" {
		query += ` AND entry_id = ?`
		args = append(args, v)
	}
	if v := strings.TrimSpace(q.ChapterID); v != "" {
		query += ` AND chapter_id = ?`
		args = append(args, v)
	}
	if v := strings.TrimSpace(q.Status); v != "" {
		query += ` AND status = ?`
		args = append(args, v)
	}
	query += ` ORDER BY created_at DESC, id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list image assets: %w", err)
	}
	defer rows.Close()
	out := []ImageAsset{}
	for rows.Next() {
		rec, err := scanImageAsset(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("store: scan image asset: %w", err)
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

// ListExportImages 返回可写入导出包的全部已完成资产（最多 2000 条）。
func (s *Store) ListExportImages() ([]ImageAsset, error) {
	return s.ListImageAssets(ImageAssetQuery{Status: "done", Limit: 2000})
}

// MarkImageRunning 原子认领一条 pending 资产，避免多个 worker 重复生成。
func (s *Store) MarkImageRunning(id string) (bool, error) {
	db, err := s.agentDatabase()
	if err != nil {
		return false, err
	}
	res, err := db.Exec(`UPDATE image_asset SET status = 'running', error = '', updated_at = ? WHERE id = ? AND status = 'pending'`,
		unixMillis(time.Now().UTC()), id)
	if err != nil {
		return false, fmt.Errorf("store: mark image running: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// CompleteImageAsset 写入生成结果并置为 done。
func (s *Store) CompleteImageAsset(id string, res ImageAssetResult) error {
	db, err := s.agentDatabase()
	if err != nil {
		return err
	}
	appearance, err := json.Marshal(res.Appearance)
	if err != nil {
		return err
	}
	result, err := db.Exec(`UPDATE image_asset SET status = 'done', error = '',
		file_key = ?, thumb_key = ?, model = ?, backend = ?, style_tag = ?,
		width = ?, height = ?, steps = ?, seed = ?, consistency_score = ?,
		consistency_note = ?, visual_proxy = ?, duration_ms = ?, cost_usd = ?, updated_at = ?, appearance_json = ?
		WHERE id = ?`,
		res.FileKey, res.ThumbKey, res.Model, res.Backend, res.StyleTag,
		res.Width, res.Height, res.Steps, res.Seed, res.ConsistencyScore,
		res.ConsistencyNote, boolInt(res.VisualProxy), res.DurationMs, res.CostUSD,
		unixMillis(time.Now().UTC()), string(appearance), id)
	if err != nil {
		return fmt.Errorf("store: complete image asset: %w", err)
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return ErrImageAssetNotFound
	}
	return nil
}

// FailImageAsset 记录失败原因；prompt 保留以便单条重试。
func (s *Store) FailImageAsset(id, message string, durationMs int64) error {
	db, err := s.agentDatabase()
	if err != nil {
		return err
	}
	res, err := db.Exec(`UPDATE image_asset SET status = 'failed', error = ?, duration_ms = ?, updated_at = ? WHERE id = ?`,
		strings.TrimSpace(message), durationMs, unixMillis(time.Now().UTC()), id)
	if err != nil {
		return fmt.Errorf("store: fail image asset: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrImageAssetNotFound
	}
	return nil
}

// ResetImageAsset 把失败资产重新置为 pending（单条重试）。
func (s *Store) ResetImageAsset(id string) error {
	db, err := s.agentDatabase()
	if err != nil {
		return err
	}
	res, err := db.Exec(`UPDATE image_asset SET status = 'pending', error = '',
		is_anchor = CASE WHEN kind = 'character' THEN 0 ELSE is_anchor END, appearance_json = 'null', updated_at = ?
		WHERE id = ? AND status IN ('failed','done')`, unixMillis(time.Now().UTC()), id)
	if err != nil {
		return fmt.Errorf("store: reset image asset: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrImageAssetNotFound
	}
	return nil
}

// RecoverRunningImageAssets 把上次进程中断时仍在 running 的资产退回 pending。
func (s *Store) RecoverRunningImageAssets() (int64, error) {
	db, err := s.agentDatabase()
	if err != nil {
		return 0, err
	}
	res, err := db.Exec(`UPDATE image_asset SET status = 'pending', updated_at = ? WHERE status = 'running'`,
		unixMillis(time.Now().UTC()))
	if err != nil {
		return 0, fmt.Errorf("store: recover image assets: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// NextPendingImage 取出最早入队的一条 pending 资产（FIFO）。
func (s *Store) NextPendingImage() (ImageAsset, bool, error) {
	db, err := s.agentDatabase()
	if err != nil {
		return ImageAsset{}, false, err
	}
	row := db.QueryRow(`SELECT ` + imageColumns + ` FROM image_asset WHERE status = 'pending'
		ORDER BY created_at ASC, id ASC LIMIT 1`)
	rec, err := scanImageAsset(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return ImageAsset{}, false, nil
	}
	if err != nil {
		return ImageAsset{}, false, fmt.Errorf("store: next pending image: %w", err)
	}
	return rec, true, nil
}

// PendingImageCount 返回队列长度（含 running），用于测试与状态展示。
func (s *Store) PendingImageCount() (int64, error) {
	db, err := s.agentDatabase()
	if err != nil {
		return 0, err
	}
	var n int64
	if err := db.QueryRow(`SELECT COUNT(*) FROM image_asset WHERE status IN ('pending','running')`).Scan(&n); err != nil {
		return 0, fmt.Errorf("store: count image queue: %w", err)
	}
	return n, nil
}

// DeleteImageAsset 删除记录并返回被删资产（调用方负责删除磁盘文件）。
func (s *Store) DeleteImageAsset(id string) (ImageAsset, error) {
	rec, err := s.GetImageAsset(id)
	if err != nil {
		return ImageAsset{}, err
	}
	db, err := s.agentDatabase()
	if err != nil {
		return ImageAsset{}, err
	}
	if _, err := db.Exec(`DELETE FROM image_asset WHERE id = ?`, id); err != nil {
		return ImageAsset{}, fmt.Errorf("store: delete image asset: %w", err)
	}
	return rec, nil
}

// ImageAnchorScope 返回基准图的唯一范围：
// 章节插图按 chapter 绑定，其余按 kind+entry 绑定（封面按 kind 全局）。
func ImageAnchorScope(kind, entryID, chapterID string) (string, string) {
	kind = strings.TrimSpace(kind)
	if kind == "illustration" {
		return kind, strings.TrimSpace(chapterID)
	}
	return kind, strings.TrimSpace(entryID)
}

// SetImageAnchor 在同一范围内把目标资产设为唯一基准图，并返回旧基准 ID（可能为空）。
func (s *Store) SetImageAnchor(id string) (string, error) {
	rec, err := s.GetImageAsset(id)
	if err != nil {
		return "", err
	}
	db, err := s.agentDatabase()
	if err != nil {
		return "", err
	}
	kind, scope := ImageAnchorScope(rec.Kind, rec.EntryID, rec.ChapterID)
	tx, err := db.Begin()
	if err != nil {
		return "", fmt.Errorf("store: begin image anchor: %w", err)
	}
	defer tx.Rollback()
	var previous string
	row := tx.QueryRow(`SELECT id FROM image_asset WHERE is_anchor = 1 AND status = 'done'
		AND kind = ? AND (CASE WHEN ? = 'illustration' THEN chapter_id ELSE entry_id END) = ?`,
		kind, kind, scope)
	if err := row.Scan(&previous); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("store: find previous anchor: %w", err)
	}
	if _, err := tx.Exec(`UPDATE image_asset SET is_anchor = 0, updated_at = ?
		WHERE is_anchor = 1 AND kind = ? AND (CASE WHEN ? = 'illustration' THEN chapter_id ELSE entry_id END) = ?`,
		unixMillis(time.Now().UTC()), kind, kind, scope); err != nil {
		return "", fmt.Errorf("store: clear image anchor: %w", err)
	}
	if _, err := tx.Exec(`UPDATE image_asset SET is_anchor = 1, updated_at = ? WHERE id = ?`,
		unixMillis(time.Now().UTC()), id); err != nil {
		return "", fmt.Errorf("store: set image anchor: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("store: commit image anchor: %w", err)
	}
	if previous == id {
		previous = ""
	}
	return previous, nil
}

// FindImageAnchor 返回同范围内已完成的基准图；不存在时返回 ErrImageAssetNotFound。
func (s *Store) FindImageAnchor(kind, entryID, chapterID string) (ImageAsset, error) {
	db, err := s.agentDatabase()
	if err != nil {
		return ImageAsset{}, err
	}
	kind, scope := ImageAnchorScope(kind, entryID, chapterID)
	row := db.QueryRow(`SELECT `+imageColumns+` FROM image_asset
		WHERE is_anchor = 1 AND status = 'done' AND kind = ?
		AND (CASE WHEN ? = 'illustration' THEN chapter_id ELSE entry_id END) = ?
		ORDER BY updated_at DESC LIMIT 1`, kind, kind, scope)
	rec, err := scanImageAsset(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return ImageAsset{}, ErrImageAssetNotFound
	}
	if err != nil {
		return ImageAsset{}, fmt.Errorf("store: find image anchor: %w", err)
	}
	return rec, nil
}

package imagegen

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	_ "image/png"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"novelagent/internal/store"
	"novelagent/internal/world"
)

var (
	ErrInvalidKind    = errors.New("imagegen: invalid asset kind")
	ErrEntryRequired  = errors.New("imagegen: entryId is required")
	ErrChapterNeeded  = errors.New("imagegen: chapterId is required for illustration")
	ErrEntryNotFound  = errors.New("imagegen: entry not found")
	ErrChapterMissing = errors.New("imagegen: chapter not found")
)

// GenerateInput 是提交预览或生成请求的入参。
type GenerateInput struct {
	Kind           string `json:"kind"`
	EntryID        string `json:"entryId"`
	ChapterID      string `json:"chapterId"`
	AnchorOffset   int    `json:"anchorOffset"`
	PromptOverride string `json:"prompt"`
	Negative       string `json:"negative"`
	StyleTag       string `json:"styleTag"`
	Width          int    `json:"width"`
	Height         int    `json:"height"`
	Steps          int    `json:"steps"`
	Seed           int64  `json:"seed"`
}

// Service 管理单项目的图像队列与生成链路。
type Service struct {
	st        *store.Store
	world     *world.Service
	log       *slog.Logger
	projectID string
	workers   int

	backendFactory func(store.ImageConfig, string) (Backend, error)

	mu     sync.Mutex
	cancel context.CancelFunc
	wg     sync.WaitGroup
	wake   chan struct{}
}

func NewService(st *store.Store, projectID string, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		st:             st,
		world:          world.NewService(st, logger),
		log:            logger,
		projectID:      projectID,
		workers:        2,
		backendFactory: NewBackend,
		wake:           make(chan struct{}, 1),
	}
}

// SetBackendFactory 供测试注入桩后端。
func (s *Service) SetBackendFactory(factory func(store.ImageConfig, string) (Backend, error)) {
	if factory != nil {
		s.backendFactory = factory
	}
}

func (s *Service) Notify() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *Service) Start(ctx context.Context) {
	s.mu.Lock()
	if s.cancel != nil {
		s.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.mu.Unlock()

	if n, err := s.st.RecoverRunningImageAssets(); err != nil {
		s.log.Warn("recover interrupted image assets", "err", err)
	} else if n > 0 {
		s.log.Warn("interrupted image assets requeued", "count", n)
	}
	for i := 0; i < s.workers; i++ {
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.loop(ctx)
		}()
	}
	s.Notify()
}

func (s *Service) Stop() {
	s.mu.Lock()
	cancel := s.cancel
	s.cancel = nil
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	s.wg.Wait()
}

func (s *Service) loop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.wake:
		}
		for {
			worked, err := s.RunOnce(ctx)
			if err != nil {
				s.log.Warn("image worker", "err", err)
			}
			if !worked || ctx.Err() != nil {
				break
			}
		}
	}
}

// RunOnce 处理一条 pending 资产；返回是否认领到任务（队列空时 false）。
func (s *Service) RunOnce(ctx context.Context) (bool, error) {
	asset, ok, err := s.st.NextPendingImage()
	if err != nil || !ok {
		return false, err
	}
	claimed, err := s.st.MarkImageRunning(asset.ID)
	if err != nil || !claimed {
		return true, err
	}
	started := time.Now()
	cfg, err := s.st.LoadConfig()
	if err != nil {
		s.fail(asset, started, err)
		return true, err
	}
	key, _ := cfg.EffectiveImageAPIKey()
	backend, err := s.backendFactory(cfg.Image, key)
	if err != nil {
		s.fail(asset, started, err)
		return true, err
	}
	var spec PromptSpec
	if err := json.Unmarshal(asset.Prompt, &spec); err != nil {
		s.fail(asset, started, fmt.Errorf("imagegen: prompt json: %w", err))
		return true, err
	}
	anchor, anchorErr := s.st.FindImageAnchor(asset.Kind, asset.EntryID, asset.ChapterID)
	if anchorErr != nil && !errors.Is(anchorErr, store.ErrImageAssetNotFound) {
		s.log.Warn("find image anchor", "err", anchorErr)
	}
	req := Request{
		Prompt:   spec.Positive,
		Negative: spec.Negative,
		Width:    asset.Width,
		Height:   asset.Height,
		Steps:    asset.Steps,
		Seed:     asset.Seed,
		Model:    cfg.Image.Model,
	}
	var anchorPtr *store.ImageAsset
	if anchorErr == nil {
		anchorPtr = &anchor
		if req.Seed == 0 {
			req.Seed = anchor.Seed
		}
		req.Reference = s.readKey(anchor.FileKey)
	}
	timeout := time.Duration(cfg.Image.Normalized().TimeoutSeconds) * time.Second
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	result, err := backend.Generate(runCtx, req)
	if err != nil {
		s.fail(asset, started, err)
		return true, err
	}
	fileKey, thumbKey, err := s.saveImage(asset.ID, result.Data, result.MIME)
	if err != nil {
		s.fail(asset, started, err)
		return true, err
	}
	consistency := ScoreConsistency(spec.Positive, spec, anchorPtr)
	duration := time.Since(started).Milliseconds()
	if err := s.st.CompleteImageAsset(asset.ID, store.ImageAssetResult{
		FileKey:          fileKey,
		ThumbKey:         thumbKey,
		Model:            cfg.Image.Model,
		Backend:          backend.Name(),
		StyleTag:         spec.StyleTag,
		Width:            asset.Width,
		Height:           asset.Height,
		Steps:            asset.Steps,
		Seed:             firstNonZero(result.Seed, req.Seed),
		ConsistencyScore: consistency.Score,
		ConsistencyNote:  consistency.Note,
		VisualProxy:      consistency.VisualProxy,
		DurationMs:       duration,
		CostUSD:          result.CostUSD,
	}); err != nil {
		return true, err
	}
	s.recordRun(asset, spec, backend.Name(), consistency, duration, "", "done")
	s.maybeReport(asset, consistency)
	return true, nil
}

func firstNonZero(values ...int64) int64 {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}

func (s *Service) fail(asset store.ImageAsset, started time.Time, cause error) {
	duration := time.Since(started).Milliseconds()
	if err := s.st.FailImageAsset(asset.ID, cause.Error(), duration); err != nil {
		s.log.Warn("record image failure", "id", asset.ID, "err", err)
	}
	var spec PromptSpec
	_ = json.Unmarshal(asset.Prompt, &spec)
	s.recordRun(asset, spec, asset.Backend, Consistency{}, duration, cause.Error(), "failed")
}

// Preview 只组装提示词，不创建任务、不调用模型。
func (s *Service) Preview(in GenerateInput) (PromptSpec, error) {
	cfg, err := s.st.LoadConfig()
	if err != nil {
		return PromptSpec{}, err
	}
	spec, _, err := s.resolve(in, cfg)
	return spec, err
}

func (s *Service) resolve(in GenerateInput, cfg store.Config) (PromptSpec, *store.ImageAsset, error) {
	kind, err := normalizeKind(in.Kind)
	if err != nil {
		return PromptSpec{}, nil, err
	}
	entryID := strings.TrimSpace(in.EntryID)
	chapterID := strings.TrimSpace(in.ChapterID)
	input := PromptInput{
		Kind:           kind,
		StyleTag:       firstNonEmpty(strings.TrimSpace(in.StyleTag), cfg.Image.StyleTag),
		Negative:       firstNonEmpty(strings.TrimSpace(in.Negative), cfg.Image.NegativePrompt),
		PromptOverride: strings.TrimSpace(in.PromptOverride),
	}
	switch kind {
	case "character", "scene", "item":
		if entryID == "" {
			return PromptSpec{}, nil, ErrEntryRequired
		}
		entry, err := s.world.GetEntry(entryID)
		if err != nil {
			return PromptSpec{}, nil, fmt.Errorf("%w: %v", ErrEntryNotFound, err)
		}
		input.Entry = entry
	case "illustration":
		if chapterID == "" {
			return PromptSpec{}, nil, ErrChapterNeeded
		}
		chapter, err := s.st.LoadChapter(chapterID)
		if err != nil {
			if errors.Is(err, store.ErrChapterNotFound) {
				return PromptSpec{}, nil, ErrChapterMissing
			}
			return PromptSpec{}, nil, err
		}
		input.Chapter = &chapter
		if entryID != "" {
			if entry, err := s.world.GetEntry(entryID); err == nil {
				input.Entry = entry
			}
		}
	case "cover":
		project, err := s.st.LoadProject()
		if err != nil {
			return PromptSpec{}, nil, err
		}
		input.ProjectTitle = project.Title
	}
	spec := BuildPrompt(input)
	var anchorPtr *store.ImageAsset
	anchor, err := s.st.FindImageAnchor(kind, entryID, chapterID)
	if err != nil {
		if !errors.Is(err, store.ErrImageAssetNotFound) {
			return PromptSpec{}, nil, err
		}
	} else {
		anchorPtr = &anchor
		if spec.StyleTag == "" {
			spec.StyleTag = anchor.StyleTag
		}
	}
	return spec, anchorPtr, nil
}

// Submit 创建 pending 资产并入队；模型配置缺失时在入队前拒绝。
func (s *Service) Submit(in GenerateInput) (store.ImageAsset, error) {
	cfg, err := s.st.LoadConfig()
	if err != nil {
		return store.ImageAsset{}, err
	}
	imageCfg := cfg.Image.Normalized()
	if strings.TrimSpace(imageCfg.BaseURL) == "" {
		return store.ImageAsset{}, ErrImageConfigMissing
	}
	if imageCfg.Backend == store.ImageBackendOpenAI && strings.TrimSpace(imageCfg.Model) == "" {
		return store.ImageAsset{}, ErrImageConfigMissing
	}
	spec, anchor, err := s.resolve(in, cfg)
	if err != nil {
		return store.ImageAsset{}, err
	}
	kind, _ := normalizeKind(in.Kind)
	if in.Width <= 0 {
		in.Width = imageCfg.Width
	}
	if in.Height <= 0 {
		in.Height = imageCfg.Height
	}
	if in.Steps <= 0 {
		in.Steps = imageCfg.Steps
	}
	in.Width = min(max(in.Width, 256), 2048)
	in.Height = min(max(in.Height, 256), 2048)
	in.Steps = min(max(in.Steps, 1), 80)
	seed := in.Seed
	if seed == 0 && anchor != nil {
		seed = anchor.Seed
	}
	promptJSON, err := json.Marshal(spec)
	if err != nil {
		return store.ImageAsset{}, err
	}
	asset, err := s.st.CreateImageAsset(store.ImageAsset{
		Kind:           kind,
		EntryID:        strings.TrimSpace(in.EntryID),
		ChapterID:      strings.TrimSpace(in.ChapterID),
		AnchorOffset:   max(in.AnchorOffset, 0),
		Prompt:         promptJSON,
		Features:       spec.Features,
		CoreFeatures:   spec.CoreFeatures,
		Model:          imageCfg.Model,
		Backend:        imageCfg.Backend,
		StyleTag:       spec.StyleTag,
		NegativePrompt: spec.Negative,
		Width:          in.Width,
		Height:         in.Height,
		Steps:          in.Steps,
		Seed:           seed,
	})
	if err != nil {
		return store.ImageAsset{}, err
	}
	s.Notify()
	return asset, nil
}

// Reset 把失败/已完成资产重新入队（单条重试）。
func (s *Service) Reset(id string) error {
	if err := s.st.ResetImageAsset(id); err != nil {
		return err
	}
	s.Notify()
	return nil
}

// SetAnchor 将资产设为同范围唯一基准图。
func (s *Service) SetAnchor(id string) (store.ImageAsset, error) {
	if _, err := s.st.SetImageAnchor(id); err != nil {
		return store.ImageAsset{}, err
	}
	return s.st.GetImageAsset(id)
}

// Delete 删除资产记录与磁盘文件。
func (s *Service) Delete(id string) (store.ImageAsset, error) {
	asset, err := s.st.DeleteImageAsset(id)
	if err != nil {
		return store.ImageAsset{}, err
	}
	s.removeFiles(asset)
	return asset, nil
}

// FilePath 返回资产原图的绝对路径。
func (s *Service) FilePath(asset store.ImageAsset) string {
	return s.absKey(asset.FileKey)
}

// ThumbPath 返回缩略图的绝对路径。
func (s *Service) ThumbPath(asset store.ImageAsset) string {
	return s.absKey(asset.ThumbKey)
}

func (s *Service) absKey(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	return filepath.Join(s.st.Dir(), filepath.FromSlash(key))
}

func (s *Service) readKey(key string) []byte {
	path := s.absKey(key)
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return data
}

func (s *Service) removeFiles(asset store.ImageAsset) {
	for _, key := range []string{asset.FileKey, asset.ThumbKey} {
		if path := s.absKey(key); path != "" {
			_ = os.Remove(path)
		}
	}
}

func (s *Service) saveImage(id string, data []byte, mime string) (string, string, error) {
	mime = strings.ToLower(strings.TrimSpace(mime))
	if idx := strings.Index(mime, ";"); idx >= 0 {
		mime = strings.TrimSpace(mime[:idx])
	}
	ext := ".png"
	switch mime {
	case "image/jpeg":
		ext = ".jpg"
	case "image/gif":
		ext = ".gif"
	case "image/webp":
		ext = ".webp"
	case "image/bmp":
		ext = ".bmp"
	}
	dir := filepath.Join(s.st.Dir(), "images")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", fmt.Errorf("imagegen: create images dir: %w", err)
	}
	fileKey := "images/" + id + ext
	if err := os.WriteFile(filepath.Join(dir, id+ext), data, 0o644); err != nil {
		return "", "", fmt.Errorf("imagegen: write image: %w", err)
	}
	thumbKey, err := s.writeThumbnail(dir, id, data)
	if err != nil {
		s.log.Warn("image thumbnail skipped", "id", id, "err", err)
		thumbKey = ""
	}
	return fileKey, thumbKey, nil
}

func (s *Service) writeThumbnail(dir, id string, data []byte) (string, error) {
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	thumb := resizeNearest(src, 320)
	path := filepath.Join(dir, id+"_thumb.png")
	file, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	if err := png.Encode(file, thumb); err != nil {
		return "", err
	}
	return "images/" + id + "_thumb.png", nil
}

func resizeNearest(src image.Image, maxSide int) image.Image {
	bounds := src.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w <= maxSide && h <= maxSide {
		return src
	}
	scale := float64(maxSide) / float64(max(w, h))
	nw, nh := max(int(float64(w)*scale), 1), max(int(float64(h)*scale), 1)
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	for y := 0; y < nh; y++ {
		sy := bounds.Min.Y + y*h/nh
		for x := 0; x < nw; x++ {
			sx := bounds.Min.X + x*w/nw
			dst.Set(x, y, src.At(sx, sy))
		}
	}
	return dst
}

func (s *Service) recordRun(asset store.ImageAsset, spec PromptSpec, backend string, consistency Consistency, durationMs int64, errText, status string) {
	if !s.imageAgentEnabled() {
		return
	}
	summary := fmt.Sprintf("%s 图像（%s）", asset.Kind, asset.EntryID)
	if asset.EntryID == "" && asset.ChapterID != "" {
		summary = fmt.Sprintf("%s 图像（章节 %s）", asset.Kind, asset.ChapterID)
	}
	if status == "done" {
		summary += fmt.Sprintf(" · 一致性 %.2f · %s", consistency.Score, backend)
	}
	now := time.Now().UTC()
	started := now.Add(-time.Duration(durationMs) * time.Millisecond)
	_ = s.st.RecordAgentRun(store.AgentRunRecord{
		ID:          "run_" + asset.ID,
		ProjectID:   s.projectID,
		AgentKey:    "image",
		AgentName:   "图像一致性 Agent",
		EventID:     asset.ID,
		EventType:   "image_request",
		Permission:  "observe",
		Status:      status,
		StartedAt:   started,
		FinishedAt:  now,
		DurationMs:  durationMs,
		InputChars:  len([]rune(spec.Positive)),
		OutputChars: len(asset.CoreFeatures),
		Summary:     summary,
		Error:       errText,
	})
}

func (s *Service) imageAgentEnabled() bool {
	configs, err := s.st.LoadAgentConfigs()
	if err != nil {
		return true
	}
	if rec, ok := configs["image"]; ok {
		return rec.Enabled
	}
	return true
}

func (s *Service) maybeReport(asset store.ImageAsset, consistency Consistency) {
	if !s.imageAgentEnabled() || consistency.Score >= 0.75 {
		return
	}
	severity := "warning"
	if consistency.Score < 0.6 {
		severity = "critical"
	}
	findings, _ := json.Marshal([]map[string]any{{
		"type":      "image_consistency",
		"assetId":   asset.ID,
		"entryId":   asset.EntryID,
		"chapterId": asset.ChapterID,
		"score":     consistency.Score,
		"detail":    consistency.Note,
	}})
	_ = s.st.SaveAgentReport(store.AgentReportRecord{
		AgentKey:  "image",
		AgentName: "图像一致性 Agent",
		EventID:   asset.ID,
		ChapterID: asset.ChapterID,
		Severity:  severity,
		Summary:   fmt.Sprintf("图像一致性偏低：%.2f（阈值 0.75）", consistency.Score),
		Findings:  findings,
	})
}

func normalizeKind(kind string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "character":
		return "character", nil
	case "scene", "location", "setting":
		return "scene", nil
	case "item":
		return "item", nil
	case "illustration", "chapter":
		return "illustration", nil
	case "cover":
		return "cover", nil
	}
	return "", ErrInvalidKind
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

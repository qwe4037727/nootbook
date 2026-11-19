package server

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"novelagent/internal/imagegen"
	"novelagent/internal/store"
)

func (s *Server) registerImageRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/images/generate", s.handleImageGenerate)
	mux.HandleFunc("POST /api/images/preview-prompt", s.handleImagePreview)
	mux.HandleFunc("GET /api/images/assets", s.handleImageAssets)
	mux.HandleFunc("GET /api/images/{id}", s.handleImageGet)
	mux.HandleFunc("GET /api/images/{id}/file", s.handleImageFile)
	mux.HandleFunc("GET /api/images/{id}/thumb", s.handleImageThumb)
	mux.HandleFunc("GET /api/images/{id}/similarity", s.handleImageSimilarity)
	mux.HandleFunc("POST /api/images/{id}/anchor", s.handleImageAnchor)
	mux.HandleFunc("POST /api/images/{id}/retry", s.handleImageRetry)
	mux.HandleFunc("DELETE /api/images/{id}", s.handleImageDelete)
	mux.HandleFunc("GET /api/config/image-model", s.handleGetImageConfig)
	mux.HandleFunc("PUT /api/config/image-model", s.handlePutImageConfig)
}

func (s *Server) imageService(w http.ResponseWriter) (*imagegen.Service, bool) {
	if s.images == nil {
		writeError(w, http.StatusServiceUnavailable, "图像服务未启用")
		return nil, false
	}
	return s.images, true
}

func (s *Server) handleImageGenerate(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.imageService(w)
	if !ok {
		return
	}
	var req imagegen.GenerateInput
	if err := decodeJSON(w, r, &req, 64<<10); err != nil {
		writeError(w, http.StatusBadRequest, "图像请求格式不正确")
		return
	}
	asset, err := svc.Submit(req)
	if err != nil {
		s.writeImageError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, asset)
}

func (s *Server) handleImagePreview(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.imageService(w)
	if !ok {
		return
	}
	var req imagegen.GenerateInput
	if err := decodeJSON(w, r, &req, 64<<10); err != nil {
		writeError(w, http.StatusBadRequest, "预览请求格式不正确")
		return
	}
	spec, err := svc.Preview(req)
	if err != nil {
		s.writeImageError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, spec)
}

func (s *Server) handleImageAssets(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.imageService(w)
	if !ok {
		return
	}
	_ = svc
	query := r.URL.Query()
	assets, err := s.store.ListImageAssets(store.ImageAssetQuery{
		Kind:      strings.TrimSpace(query.Get("kind")),
		EntryID:   strings.TrimSpace(query.Get("entryId")),
		ChapterID: strings.TrimSpace(query.Get("chapterId")),
		Status:    strings.TrimSpace(query.Get("status")),
		Limit:     queryLimit(r, 200, 2000),
	})
	if err != nil {
		s.log.Error("list image assets", "err", err)
		writeError(w, http.StatusInternalServerError, "读取图像资产失败")
		return
	}
	pending, _ := s.store.PendingImageCount()
	writeJSON(w, http.StatusOK, map[string]any{"assets": assets, "pending": pending})
}

func (s *Server) handleImageGet(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.imageService(w); !ok {
		return
	}
	asset, err := s.store.GetImageAsset(r.PathValue("id"))
	if err != nil {
		s.writeImageError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, asset)
}

func (s *Server) handleImageSimilarity(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.imageService(w)
	if !ok {
		return
	}
	result, err := svc.CompareCharacterImages(r.Context(), r.PathValue("id"), strings.TrimSpace(r.URL.Query().Get("otherId")))
	if err != nil {
		if r.Context().Err() == nil {
			s.writeImageError(w, err)
		}
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleImageFile(w http.ResponseWriter, r *http.Request) {
	s.serveImageFile(w, r, false)
}

func (s *Server) handleImageThumb(w http.ResponseWriter, r *http.Request) {
	s.serveImageFile(w, r, true)
}

func (s *Server) serveImageFile(w http.ResponseWriter, r *http.Request, thumb bool) {
	svc, ok := s.imageService(w)
	if !ok {
		return
	}
	asset, err := s.store.GetImageAsset(r.PathValue("id"))
	if err != nil {
		s.writeImageError(w, err)
		return
	}
	path := svc.FilePath(asset)
	if thumb {
		path = svc.ThumbPath(asset)
	}
	if path == "" {
		writeError(w, http.StatusNotFound, "图像文件尚未生成")
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=300")
	http.ServeFile(w, r, path)
}

func (s *Server) handleImageAnchor(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.imageService(w)
	if !ok {
		return
	}
	asset, err := svc.SetAnchor(r.PathValue("id"))
	if err != nil {
		s.writeImageError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, asset)
}

func (s *Server) handleImageRetry(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.imageService(w)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if err := svc.Reset(id); err != nil {
		s.writeImageError(w, err)
		return
	}
	asset, err := s.store.GetImageAsset(id)
	if err != nil {
		s.writeImageError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, asset)
}

func (s *Server) handleImageDelete(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.imageService(w)
	if !ok {
		return
	}
	if _, err := svc.Delete(r.PathValue("id")); err != nil {
		s.writeImageError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true, "id": r.PathValue("id")})
}

func (s *Server) writeImageError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrImageAssetNotFound), errors.Is(err, imagegen.ErrEntryNotFound), errors.Is(err, imagegen.ErrChapterMissing):
		writeError(w, http.StatusNotFound, "对象不存在或已被删除")
	case errors.Is(err, imagegen.ErrImageConfigMissing):
		writeError(w, http.StatusBadRequest, "请先在「图像模型」中填写 Base URL 与模型名")
	case errors.Is(err, imagegen.ErrInvalidImagePair),
		errors.Is(err, imagegen.ErrEntryRequired), errors.Is(err, imagegen.ErrChapterNeeded),
		errors.Is(err, imagegen.ErrInvalidKind), errors.Is(err, imagegen.ErrPromptRequired):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		s.log.Error("image request failed", "err", err)
		writeError(w, http.StatusInternalServerError, "图像处理失败")
	}
}

type publicImageConfig struct {
	Backend           string `json:"backend"`
	BaseURL           string `json:"baseUrl"`
	Model             string `json:"model"`
	StyleTag          string `json:"styleTag"`
	NegativePrompt    string `json:"negativePrompt"`
	Width             int    `json:"width"`
	Height            int    `json:"height"`
	Steps             int    `json:"steps"`
	TimeoutSeconds    int    `json:"timeoutSeconds"`
	HasAPIKey         bool   `json:"hasApiKey"`
	APIKeyHint        string `json:"apiKeyHint"`
	APIKeyFromEnv     bool   `json:"apiKeyFromEnv"`
	AppearanceEnabled bool   `json:"appearanceEnabled"`
	AppearanceModel   string `json:"appearanceModel"`
}

func publicImageConfigOf(cfg store.Config) publicImageConfig {
	image := cfg.Image.Normalized()
	key, fromEnv := cfg.EffectiveImageAPIKey()
	pub := publicImageConfig{
		Backend:           image.Backend,
		BaseURL:           image.BaseURL,
		Model:             image.Model,
		StyleTag:          image.StyleTag,
		NegativePrompt:    image.NegativePrompt,
		Width:             image.Width,
		Height:            image.Height,
		Steps:             image.Steps,
		TimeoutSeconds:    image.TimeoutSeconds,
		HasAPIKey:         key != "",
		APIKeyFromEnv:     fromEnv,
		AppearanceEnabled: image.AppearanceEnabled,
		AppearanceModel:   image.AppearanceModel,
	}
	switch {
	case fromEnv:
		pub.APIKeyHint = "环境变量 IMAGE_API_KEY"
	case key != "":
		pub.APIKeyHint = maskKey(key)
	}
	return pub
}

func (s *Server) handleGetImageConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.store.LoadConfig()
	if err != nil {
		s.log.Error("load image config", "err", err)
		writeError(w, http.StatusInternalServerError, "读取图像模型设置失败")
		return
	}
	writeJSON(w, http.StatusOK, publicImageConfigOf(cfg))
}

type imageConfigRequest struct {
	Backend           string  `json:"backend"`
	BaseURL           string  `json:"baseUrl"`
	Model             string  `json:"model"`
	StyleTag          string  `json:"styleTag"`
	NegativePrompt    string  `json:"negativePrompt"`
	Width             int     `json:"width"`
	Height            int     `json:"height"`
	Steps             int     `json:"steps"`
	TimeoutSeconds    int     `json:"timeoutSeconds"`
	APIKey            *string `json:"apiKey"`
	ClearAPIKey       bool    `json:"clearApiKey"`
	AppearanceEnabled *bool   `json:"appearanceEnabled"`
	AppearanceModel   *string `json:"appearanceModel"`
}

func (s *Server) handlePutImageConfig(w http.ResponseWriter, r *http.Request) {
	var req imageConfigRequest
	if err := decodeJSON(w, r, &req, 64<<10); err != nil {
		writeError(w, http.StatusBadRequest, "图像模型设置格式不正确")
		return
	}
	backend := strings.ToLower(strings.TrimSpace(req.Backend))
	if backend == "" {
		backend = store.ImageBackendOpenAI
	}
	if backend != store.ImageBackendOpenAI && backend != store.ImageBackendSDWebUI {
		writeError(w, http.StatusBadRequest, "backend 仅支持 openai 或 sdwebui")
		return
	}
	baseURL := strings.TrimRight(strings.TrimSpace(req.BaseURL), "/")
	if baseURL != "" {
		u, err := url.Parse(baseURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			writeError(w, http.StatusBadRequest, "Base URL 需要是 http:// 或 https:// 地址")
			return
		}
	}
	cfg, err := s.store.LoadConfig()
	if err != nil {
		s.log.Error("load config", "err", err)
		writeError(w, http.StatusInternalServerError, "读取模型设置失败")
		return
	}
	appearanceEnabled, appearanceModel := cfg.Image.AppearanceEnabled, cfg.Image.AppearanceModel
	if req.AppearanceEnabled != nil {
		appearanceEnabled = *req.AppearanceEnabled
	}
	if req.AppearanceModel != nil {
		appearanceModel = *req.AppearanceModel
	}
	cfg.Image = store.ImageConfig{
		AppearanceEnabled: appearanceEnabled,
		AppearanceModel:   appearanceModel,
		Backend:           backend,
		BaseURL:           baseURL,
		Model:             strings.TrimSpace(req.Model),
		StyleTag:          strings.TrimSpace(req.StyleTag),
		NegativePrompt:    strings.TrimSpace(req.NegativePrompt),
		Width:             req.Width,
		Height:            req.Height,
		Steps:             req.Steps,
		TimeoutSeconds:    req.TimeoutSeconds,
		APIKey:            cfg.Image.APIKey,
	}
	if req.ClearAPIKey {
		cfg.Image.APIKey = ""
	}
	if req.APIKey != nil {
		if key := strings.TrimSpace(*req.APIKey); key != "" {
			cfg.Image.APIKey = key
		}
	}
	saved, err := s.store.SaveConfig(cfg)
	if err != nil {
		s.log.Error("save image config", "err", err)
		writeError(w, http.StatusInternalServerError, "保存图像模型设置失败")
		return
	}
	writeJSON(w, http.StatusOK, publicImageConfigOf(saved))
}

type featureFlags struct {
	M1     bool `json:"m1"`
	M2     bool `json:"m2"`
	M3     bool `json:"m3"`
	M4     bool `json:"m4"`
	M5     bool `json:"m5"`
	M6     bool `json:"m6"`
	M7     bool `json:"m7"`
	Images bool `json:"images"`
	Export bool `json:"export"`
}

func (s *Server) handleFeatures(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, featureFlags{
		M1:     s.milestone.M1Enabled,
		M2:     s.milestone.M2Enabled,
		M3:     s.milestone.M3Enabled,
		M4:     s.milestone.M4Enabled,
		M5:     s.milestone.M5Enabled,
		M6:     s.milestone.M6Enabled,
		M7:     s.milestone.M7Enabled,
		Images: s.milestone.M6Enabled && s.images != nil,
		Export: s.milestone.M7Enabled,
	})
}

// Package imagegen 提供图像生成链路：双后端适配、提示词组装、结构化一致性评分与独立队列。
//
// 后端约定：
//   - openai：Base URL 指向 OpenAI 兼容 /v1，请求 {base}/images/generations；
//   - sdwebui：Base URL 指向 Stable Diffusion WebUI 根路径，请求 {base}/sdapi/v1/txt2img
//     （有参考图时改走 /sdapi/v1/img2img）。
package imagegen

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"novelagent/internal/store"
)

var (
	ErrImageConfigMissing = errors.New("imagegen: image model not configured")
	ErrPromptRequired     = errors.New("imagegen: prompt is empty")
	ErrUnsupportedBackend = errors.New("imagegen: unsupported backend")
)

const maxImageBytes = 32 << 20

// Request 是与后端无关的生成请求。
type Request struct {
	Prompt    string
	Negative  string
	Width     int
	Height    int
	Steps     int
	Seed      int64
	Model     string
	Reference []byte
	// ReferenceStrength 仅在支持图生图的后端生效（SD WebUI）。
	ReferenceStrength float64
}

// Result 是后端返回的图像字节与元信息。
type Result struct {
	Data     []byte
	MIME     string
	Seed     int64
	Provider string
	CostUSD  float64
}

// Backend 是图像模型适配接口。
type Backend interface {
	Name() string
	Generate(ctx context.Context, req Request) (Result, error)
}

// NewBackend 根据配置创建后端；apiKey 由调用方解析（支持环境变量）。
func NewBackend(cfg store.ImageConfig, apiKey string) (Backend, error) {
	cfg = cfg.Normalized()
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return nil, ErrImageConfigMissing
	}
	client := &http.Client{Timeout: time.Duration(cfg.TimeoutSeconds) * time.Second}
	switch cfg.Backend {
	case store.ImageBackendSDWebUI:
		return &sdBackend{baseURL: cfg.BaseURL, apiKey: apiKey, client: client}, nil
	case store.ImageBackendOpenAI, "":
		if strings.TrimSpace(cfg.Model) == "" {
			return nil, fmt.Errorf("%w: openai 后端需要模型名", ErrImageConfigMissing)
		}
		return &openAIBackend{baseURL: cfg.BaseURL, apiKey: apiKey, client: client}, nil
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedBackend, cfg.Backend)
	}
}

type openAIBackend struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

func (b *openAIBackend) Name() string { return store.ImageBackendOpenAI }

type openAIRequest struct {
	Model          string `json:"model"`
	Prompt         string `json:"prompt"`
	N              int    `json:"n"`
	Size           string `json:"size"`
	ResponseFormat string `json:"response_format"`
}

type openAIResponse struct {
	Data []struct {
		B64JSON string `json:"b64_json"`
		URL     string `json:"url"`
	} `json:"data"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (b *openAIBackend) Generate(ctx context.Context, req Request) (Result, error) {
	if strings.TrimSpace(req.Prompt) == "" {
		return Result{}, ErrPromptRequired
	}
	body := openAIRequest{
		Model:          req.Model,
		Prompt:         withNegative(req.Prompt, req.Negative),
		N:              1,
		Size:           fmt.Sprintf("%dx%d", req.Width, req.Height),
		ResponseFormat: "b64_json",
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return Result{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(b.baseURL, "/")+"/images/generations", bytes.NewReader(payload))
	if err != nil {
		return Result{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if b.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+b.apiKey)
	}
	resp, err := b.client.Do(httpReq)
	if err != nil {
		return Result{}, fmt.Errorf("imagegen: openai request: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return Result{}, fmt.Errorf("imagegen: openai read: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Result{}, fmt.Errorf("imagegen: openai status %d: %s", resp.StatusCode, snippet(data))
	}
	var parsed openAIResponse
	if err := json.Unmarshal(data, &parsed); err != nil {
		return Result{}, fmt.Errorf("imagegen: openai decode: %w", err)
	}
	if parsed.Error != nil && parsed.Error.Message != "" {
		return Result{}, fmt.Errorf("imagegen: openai error: %s", parsed.Error.Message)
	}
	if len(parsed.Data) == 0 {
		return Result{}, errors.New("imagegen: openai returned no image")
	}
	item := parsed.Data[0]
	if item.B64JSON != "" {
		raw, err := base64.StdEncoding.DecodeString(item.B64JSON)
		if err != nil {
			return Result{}, fmt.Errorf("imagegen: openai base64: %w", err)
		}
		return Result{Data: raw, MIME: detectMIME(raw), Seed: req.Seed, Provider: b.Name()}, nil
	}
	if item.URL != "" {
		raw, mime, err := b.fetch(ctx, item.URL)
		if err != nil {
			return Result{}, err
		}
		return Result{Data: raw, MIME: mime, Seed: req.Seed, Provider: b.Name()}, nil
	}
	return Result{}, errors.New("imagegen: openai returned empty image payload")
}

func (b *openAIBackend) fetch(ctx context.Context, rawURL string) ([]byte, string, error) {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, "", fmt.Errorf("imagegen: invalid image url")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, "", err
	}
	resp, err := b.client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("imagegen: fetch image: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("imagegen: fetch image status %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxImageBytes+1))
	if err != nil {
		return nil, "", fmt.Errorf("imagegen: fetch image: %w", err)
	}
	if len(data) > maxImageBytes {
		return nil, "", errors.New("imagegen: image exceeds 32MB")
	}
	return data, detectMIME(data), nil
}

type sdBackend struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

func (b *sdBackend) Name() string { return store.ImageBackendSDWebUI }

type sdTxt2ImgRequest struct {
	Prompt         string  `json:"prompt"`
	NegativePrompt string  `json:"negative_prompt"`
	Width          int     `json:"width"`
	Height         int     `json:"height"`
	Steps          int     `json:"steps"`
	Seed           int64   `json:"seed"`
	CfgScale       float64 `json:"cfg_scale"`
	BatchSize      int     `json:"batch_size"`
	NIter          int     `json:"n_iter"`
}

type sdImg2ImgRequest struct {
	sdTxt2ImgRequest
	InitImages        []string `json:"init_images"`
	DenoisingStrength float64  `json:"denoising_strength"`
}

type sdResponse struct {
	Images []string `json:"images"`
	Info   string   `json:"info"`
	Detail string   `json:"detail"`
}

func (b *sdBackend) Generate(ctx context.Context, req Request) (Result, error) {
	if strings.TrimSpace(req.Prompt) == "" {
		return Result{}, ErrPromptRequired
	}
	base := strings.TrimRight(b.baseURL, "/")
	endpoint := base + "/sdapi/v1/txt2img"
	text := sdTxt2ImgRequest{
		Prompt:         req.Prompt,
		NegativePrompt: req.Negative,
		Width:          req.Width,
		Height:         req.Height,
		Steps:          req.Steps,
		Seed:           req.Seed,
		CfgScale:       7,
		BatchSize:      1,
		NIter:          1,
	}
	var payload any = text
	if len(req.Reference) > 0 {
		strength := req.ReferenceStrength
		if strength <= 0 || strength >= 1 {
			strength = 0.65
		}
		payload = sdImg2ImgRequest{
			sdTxt2ImgRequest:  text,
			InitImages:        []string{base64.StdEncoding.EncodeToString(req.Reference)},
			DenoisingStrength: strength,
		}
		endpoint = base + "/sdapi/v1/img2img"
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return Result{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return Result{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if b.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+b.apiKey)
	}
	resp, err := b.client.Do(httpReq)
	if err != nil {
		return Result{}, fmt.Errorf("imagegen: sd request: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxImageBytes+1))
	if err != nil {
		return Result{}, fmt.Errorf("imagegen: sd read: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Result{}, fmt.Errorf("imagegen: sd status %d: %s", resp.StatusCode, snippet(data))
	}
	var parsed sdResponse
	if err := json.Unmarshal(data, &parsed); err != nil {
		return Result{}, fmt.Errorf("imagegen: sd decode: %w", err)
	}
	if len(parsed.Images) == 0 {
		msg := parsed.Detail
		if msg == "" {
			msg = "SD 返回空图像"
		}
		return Result{}, fmt.Errorf("imagegen: %s", msg)
	}
	raw, err := base64.StdEncoding.DecodeString(parsed.Images[0])
	if err != nil {
		return Result{}, fmt.Errorf("imagegen: sd base64: %w", err)
	}
	return Result{Data: raw, MIME: detectMIME(raw), Seed: req.Seed, Provider: b.Name()}, nil
}

func withNegative(prompt, negative string) string {
	negative = strings.TrimSpace(negative)
	if negative == "" {
		return prompt
	}
	return prompt + "\n\nAvoid: " + negative
}

func detectMIME(data []byte) string {
	if len(data) == 0 {
		return "application/octet-stream"
	}
	return http.DetectContentType(data)
}

func snippet(data []byte) string {
	text := strings.TrimSpace(string(data))
	runes := []rune(text)
	if len(runes) > 240 {
		return string(runes[:240]) + "…"
	}
	return text
}

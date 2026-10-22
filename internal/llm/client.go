package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// RetryConfig 描述瞬时网络故障的重试策略（MODEL-01，docs/issues.md #22）。
// 仅对可重建的瞬时故障重试（超时/连接中断/EOF/5xx/429/408），
// 业务级 4xx、回调错误（已向对端写出部分内容）与调用方主动取消不重试。
type RetryConfig struct {
	MaxAttempts int           // 总尝试次数（含首次）；<=0 表示使用默认值
	BaseDelay   time.Duration // 首次退避基数
	MaxDelay    time.Duration // 退避上限（含 Retry-After）
}

// DefaultRetryConfig 返回生产默认值：最多 3 次、指数退避 500ms→1s→2s（上限 5s）。
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxAttempts: 3,
		BaseDelay:   500 * time.Millisecond,
		MaxDelay:    5 * time.Second,
	}
}

type Client struct {
	BaseURL string
	APIKey  string
	Model   string
	HTTP    *http.Client
	Retry   RetryConfig
	// sleep 用于退避等待，便于测试注入即时返回；nil 时使用默认 ctx 感知实现。
	sleep func(ctx context.Context, d time.Duration) error
}

var sharedHTTPClient = func() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = 120 * time.Second
	return &http.Client{Transport: transport}
}()

func NewClient(baseURL, apiKey, model string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		APIKey:  strings.TrimSpace(apiKey),
		Model:   strings.TrimSpace(model),
		HTTP:    sharedHTTPClient,
		Retry:   DefaultRetryConfig(),
	}
}

// retryConfig 规整零值与缺省字段，保证调用方可显式设 MaxAttempts=1 关闭重试。
func (c *Client) retryConfig() RetryConfig {
	cfg := c.Retry
	def := DefaultRetryConfig()
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = def.MaxAttempts
	}
	if cfg.BaseDelay <= 0 {
		cfg.BaseDelay = def.BaseDelay
	}
	if cfg.MaxDelay <= 0 {
		cfg.MaxDelay = def.MaxDelay
	}
	return cfg
}

func (c *Client) sleepFn() func(ctx context.Context, d time.Duration) error {
	if c.sleep != nil {
		return c.sleep
	}
	return func(ctx context.Context, d time.Duration) error {
		t := time.NewTimer(d)
		defer t.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			return nil
		}
	}
}

type chatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Temperature float64   `json:"temperature"`
	Stream      bool      `json:"stream"`
}

// statusError 表示上游返回了非 2xx 状态码，携带状态码与可选的 Retry-After。
type statusError struct {
	StatusCode int
	Message    string
	RetryAfter time.Duration
}

func (e *statusError) Error() string {
	return fmt.Sprintf("llm: upstream returned %d: %s", e.StatusCode, e.Message)
}

// retryable 仅 408/429/5xx 可重试；其余 4xx（含 401/403/404）为业务错误，不可重试。
func (e *statusError) retryable() bool {
	switch e.StatusCode {
	case http.StatusRequestTimeout, // 408
		http.StatusTooManyRequests,     // 429
		http.StatusInternalServerError, // 500
		http.StatusBadGateway,          // 502
		http.StatusServiceUnavailable,  // 503
		http.StatusGatewayTimeout:      // 504
		return true
	}
	return e.StatusCode >= 500
}

// callbackError 包裹 onDelta 回调返回的错误，标记其不可重试（已向对端写出内容）。
type callbackError struct{ err error }

func (e *callbackError) Error() string { return e.err.Error() }
func (e *callbackError) Unwrap() error { return e.err }

// isRetryableError 判断错误是否属于可重试的瞬时故障（MODEL-01）。
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var ce *callbackError
	if errors.As(err, &ce) {
		return false
	}
	var se *statusError
	if errors.As(err, &se) {
		return se.retryable()
	}
	// 连接/读取层面的瞬时故障
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	if errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, syscall.EPIPE) || errors.Is(err, syscall.ETIMEDOUT) ||
		errors.Is(err, syscall.EHOSTUNREACH) || errors.Is(err, syscall.ENETUNREACH) ||
		errors.Is(err, syscall.ECONNABORTED) {
		return true
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return true
	}
	// http.Transport 内部错误（如 timeout awaiting response headers）可能未经 net.Error 包装，
	// 兜底按特征字符串匹配。
	msg := strings.ToLower(err.Error())
	for _, s := range []string{
		"timeout awaiting response headers",
		"unexpected eof",
		"connection refused",
		"connection reset",
		"broken pipe",
		"server closed idle connection",
		"http2: server sent goaway",
	} {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
}

// backoff 计算并等待第 attempt 次失败后的退避时长（指数退避，上限 MaxDelay，
// 若上游 Retry-After 更长则采用之，仍受 MaxDelay 约束），等待过程感知 ctx 取消。
func (c *Client) backoff(ctx context.Context, cfg RetryConfig, attempt int, retryAfter time.Duration) error {
	delay := cfg.BaseDelay
	for i := 1; i < attempt; i++ {
		delay *= 2
	}
	if delay > cfg.MaxDelay {
		delay = cfg.MaxDelay
	}
	if retryAfter > delay {
		delay = retryAfter
	}
	if delay > cfg.MaxDelay {
		delay = cfg.MaxDelay
	}
	return c.sleepFn()(ctx, delay)
}

func (c *Client) StreamChat(ctx context.Context, messages []Message, temperature float64, onDelta func(string) error) error {
	if c.BaseURL == "" {
		return errors.New("llm: base URL is empty")
	}
	if c.Model == "" {
		return errors.New("llm: model is empty")
	}

	body, err := json.Marshal(chatRequest{
		Model:       c.Model,
		Messages:    messages,
		Temperature: temperature,
		Stream:      true,
	})
	if err != nil {
		return fmt.Errorf("llm: encode request: %w", err)
	}

	cfg := c.retryConfig()
	var lastErr error
	for attempt := 1; attempt <= cfg.MaxAttempts; attempt++ {
		// delivered 标记本次尝试是否已向对端写出内容：一旦写出则不可重试，
		// 否则部分内容会被重复下发（MODEL-01 流式安全约束）。
		delivered := false
		cb := func(delta string) error {
			delivered = true
			if err := onDelta(delta); err != nil {
				return &callbackError{err: err}
			}
			return nil
		}
		err, retryAfter := c.streamAttempt(ctx, body, cb)
		if err == nil {
			return nil
		}
		lastErr = err
		if !delivered && isRetryableError(err) && attempt < cfg.MaxAttempts {
			if werr := c.backoff(ctx, cfg, attempt, retryAfter); werr != nil {
				return werr
			}
			continue
		}
		return err
	}
	return lastErr
}

func (c *Client) CompleteChat(ctx context.Context, messages []Message, temperature float64) (string, error) {
	if c.BaseURL == "" {
		return "", errors.New("llm: base URL is empty")
	}
	if c.Model == "" {
		return "", errors.New("llm: model is empty")
	}

	body, err := json.Marshal(chatRequest{
		Model:       c.Model,
		Messages:    messages,
		Temperature: temperature,
		Stream:      false,
	})
	if err != nil {
		return "", fmt.Errorf("llm: encode request: %w", err)
	}

	cfg := c.retryConfig()
	var lastErr error
	for attempt := 1; attempt <= cfg.MaxAttempts; attempt++ {
		content, err, retryAfter := c.completeAttempt(ctx, body)
		if err == nil {
			return content, nil
		}
		lastErr = err
		if isRetryableError(err) && attempt < cfg.MaxAttempts {
			if werr := c.backoff(ctx, cfg, attempt, retryAfter); werr != nil {
				return "", werr
			}
			continue
		}
		return "", err
	}
	return "", lastErr
}

// buildRequest 每次尝试重建请求（请求体读器在上一尝试已被消费）。
func (c *Client) buildRequest(ctx context.Context, body []byte, accept string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("llm: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", accept)
	req.Header.Set("User-Agent", "novel-agent/0.1")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	return req, nil
}

func (c *Client) streamAttempt(ctx context.Context, body []byte, onDelta func(string) error) (error, time.Duration) {
	req, err := c.buildRequest(ctx, body, "text/event-stream")
	if err != nil {
		return err, 0
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("llm: request failed: %w", err), 0
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return upstreamStatusError(resp), 0
	}
	if !isEventStream(resp) {
		return readNonStreaming(resp.Body, onDelta), 0
	}
	return scanSSE(resp.Body, onDelta), 0
}

func (c *Client) completeAttempt(ctx context.Context, body []byte) (string, error, time.Duration) {
	req, err := c.buildRequest(ctx, body, "application/json")
	if err != nil {
		return "", err, 0
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", fmt.Errorf("llm: request failed: %w", err), 0
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", upstreamStatusError(resp), 0
	}

	if isEventStream(resp) {
		var content strings.Builder
		if err := scanSSE(resp.Body, func(delta string) error {
			content.WriteString(delta)
			return nil
		}); err != nil {
			return "", err, 0
		}
		if strings.TrimSpace(content.String()) == "" {
			return "", errors.New("llm: response has no content"), 0
		}
		return content.String(), nil, 0
	}

	var completion completionResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(&completion); err != nil {
		return "", fmt.Errorf("llm: decode response: %w", err), 0
	}
	if completion.Error != nil && completion.Error.Message != "" {
		return "", fmt.Errorf("llm: %s", completion.Error.Message), 0
	}
	if len(completion.Choices) == 0 {
		return "", errors.New("llm: response has no choices"), 0
	}
	content := completion.Choices[0].Message.Content
	if strings.TrimSpace(content) == "" {
		return "", errors.New("llm: response has no content"), 0
	}
	return content, nil, 0
}

func isEventStream(resp *http.Response) bool {
	return strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/event-stream")
}

func upstreamStatusError(resp *http.Response) error {
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	message := strings.TrimSpace(string(data))
	if message == "" {
		message = http.StatusText(resp.StatusCode)
	}
	se := &statusError{StatusCode: resp.StatusCode, Message: message}
	if ra := strings.TrimSpace(resp.Header.Get("Retry-After")); ra != "" {
		if secs, err := strconv.Atoi(ra); err == nil && secs > 0 {
			se.RetryAfter = time.Duration(secs) * time.Second
		}
	}
	return se
}

func scanSSE(body io.Reader, onDelta func(string) error) error {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 32<<10), 8<<20)

	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		data, ok := strings.CutPrefix(line, "data:")
		if !ok {
			continue
		}
		data = strings.TrimSpace(data)
		if data == "[DONE]" {
			return nil
		}

		var event streamEvent
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			// Some compatible servers interleave non-JSON keepalive payloads.
			continue
		}
		if event.Error != nil && event.Error.Message != "" {
			return fmt.Errorf("llm: %s", event.Error.Message)
		}
		if len(event.Choices) == 0 {
			continue
		}
		if delta := event.Choices[0].Delta.Content; delta != "" {
			if err := onDelta(delta); err != nil {
				return err
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("llm: read stream: %w", err)
	}
	return nil
}

func readNonStreaming(body io.Reader, onDelta func(string) error) error {
	var resp completionResponse
	if err := json.NewDecoder(io.LimitReader(body, 16<<20)).Decode(&resp); err != nil {
		return fmt.Errorf("llm: decode response: %w", err)
	}
	if resp.Error != nil && resp.Error.Message != "" {
		return fmt.Errorf("llm: %s", resp.Error.Message)
	}
	if len(resp.Choices) == 0 {
		return errors.New("llm: response has no choices")
	}
	if content := resp.Choices[0].Message.Content; content != "" {
		return onDelta(content)
	}
	return nil
}

type upstreamError struct {
	Message string `json:"message"`
}

type streamEvent struct {
	Error   *upstreamError `json:"error"`
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
	} `json:"choices"`
}

type completionResponse struct {
	Error   *upstreamError `json:"error"`
	Choices []struct {
		Message Message `json:"message"`
	} `json:"choices"`
}

package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type Client struct {
	BaseURL string
	APIKey  string
	Model   string
	HTTP    *http.Client
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
	}
}

type chatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Temperature float64   `json:"temperature"`
	Stream      bool      `json:"stream"`
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

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("llm: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("User-Agent", "novel-agent/0.1")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("llm: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return upstreamStatusError(resp)
	}

	if !isEventStream(resp) {
		return readNonStreaming(resp.Body, onDelta)
	}
	return scanSSE(resp.Body, onDelta)
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

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("llm: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "novel-agent/0.1")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", fmt.Errorf("llm: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", upstreamStatusError(resp)
	}

	if isEventStream(resp) {
		var content strings.Builder
		if err := scanSSE(resp.Body, func(delta string) error {
			content.WriteString(delta)
			return nil
		}); err != nil {
			return "", err
		}
		if strings.TrimSpace(content.String()) == "" {
			return "", errors.New("llm: response has no content")
		}
		return content.String(), nil
	}

	var completion completionResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(&completion); err != nil {
		return "", fmt.Errorf("llm: decode response: %w", err)
	}
	if completion.Error != nil && completion.Error.Message != "" {
		return "", fmt.Errorf("llm: %s", completion.Error.Message)
	}
	if len(completion.Choices) == 0 {
		return "", errors.New("llm: response has no choices")
	}
	content := completion.Choices[0].Message.Content
	if strings.TrimSpace(content) == "" {
		return "", errors.New("llm: response has no content")
	}
	return content, nil
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
	return fmt.Errorf("llm: upstream returned %d: %s", resp.StatusCode, message)
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

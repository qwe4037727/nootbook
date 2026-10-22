package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestStreamChatReadsSSE(t *testing.T) {
	type capturedRequest struct {
		path   string
		auth   string
		accept string
		body   chatRequest
	}
	captured := make(chan capturedRequest, 1)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body chatRequest
		err := json.NewDecoder(r.Body).Decode(&body)
		captured <- capturedRequest{
			path:   r.URL.Path,
			auth:   r.Header.Get("Authorization"),
			accept: r.Header.Get("Accept"),
			body:   body,
		}
		if err != nil {
			t.Errorf("decode upstream request: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		fmt.Fprint(w, ": keepalive\n\n")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"雨声\"}}]}\n\n")
		w.(http.Flusher).Flush()
		fmt.Fprint(w, "data: not-json\n\n")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"更密。\"}}]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	client := NewClient(server.URL+"/v1/", "sk-test", "deepseek-chat")
	var deltas []string
	err := client.StreamChat(
		context.Background(),
		[]Message{{Role: "system", Content: "rules"}, {Role: "user", Content: "continue"}},
		0.7,
		func(delta string) error {
			deltas = append(deltas, delta)
			return nil
		},
	)
	if err != nil {
		t.Fatalf("StreamChat returned error: %v", err)
	}
	if got := strings.Join(deltas, ""); got != "雨声更密。" {
		t.Errorf("deltas = %q, want %q", got, "雨声更密。")
	}

	got := <-captured
	if got.path != "/v1/chat/completions" {
		t.Errorf("path = %q", got.path)
	}
	if got.auth != "Bearer sk-test" {
		t.Errorf("Authorization = %q", got.auth)
	}
	if got.accept != "text/event-stream" {
		t.Errorf("Accept = %q", got.accept)
	}
	if got.body.Model != "deepseek-chat" || !got.body.Stream {
		t.Errorf("upstream body = %+v", got.body)
	}
	if got.body.Temperature != 0.7 {
		t.Errorf("temperature = %v", got.body.Temperature)
	}
	if len(got.body.Messages) != 2 || got.body.Messages[1].Content != "continue" {
		t.Errorf("messages = %+v", got.body.Messages)
	}
}

func TestStreamChatReportsHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"error":{"message":"invalid api key"}}`)
	}))
	defer server.Close()

	client := NewClient(server.URL, "bad-key", "test-model")
	err := client.StreamChat(context.Background(), []Message{{Role: "user", Content: "hi"}}, 0.5, func(string) error {
		return nil
	})
	if err == nil {
		t.Fatal("StreamChat returned nil error")
	}
	if !strings.Contains(err.Error(), "401") || !strings.Contains(err.Error(), "invalid api key") {
		t.Errorf("error = %q, want status and upstream message", err)
	}
}

func TestStreamChatReportsSSEErrorEvent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"error\":{\"message\":\"rate limited\"}}\n\n")
	}))
	defer server.Close()

	client := NewClient(server.URL, "", "test-model")
	err := client.StreamChat(context.Background(), []Message{{Role: "user", Content: "hi"}}, 0.5, func(string) error {
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "rate limited") {
		t.Fatalf("error = %v, want rate limited", err)
	}
}

func TestStreamChatFallsBackToNonStreamingJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"整段续写。"}}]}`)
	}))
	defer server.Close()

	client := NewClient(server.URL, "", "test-model")
	var deltas []string
	err := client.StreamChat(context.Background(), []Message{{Role: "user", Content: "hi"}}, 0.5, func(delta string) error {
		deltas = append(deltas, delta)
		return nil
	})
	if err != nil {
		t.Fatalf("StreamChat returned error: %v", err)
	}
	if got := strings.Join(deltas, ""); got != "整段续写。" {
		t.Errorf("deltas = %q", got)
	}
}

func TestCompleteChatReadsJSONResponse(t *testing.T) {
	type capturedRequest struct {
		auth   string
		accept string
		body   chatRequest
	}
	captured := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body chatRequest
		err := json.NewDecoder(r.Body).Decode(&body)
		if err != nil {
			t.Errorf("decode upstream request: %v", err)
		}
		captured <- capturedRequest{auth: r.Header.Get("Authorization"), accept: r.Header.Get("Accept"), body: body}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"{\"summary\":\"雨夜\"}"}}]}`)
	}))
	defer server.Close()

	client := NewClient(server.URL+"/v1", "sk-test", "deepseek-chat")
	content, err := client.CompleteChat(context.Background(), []Message{{Role: "system", Content: "json"}, {Role: "user", Content: "analyze"}}, 0.2)
	if err != nil {
		t.Fatalf("CompleteChat returned error: %v", err)
	}
	if content != `{"summary":"雨夜"}` {
		t.Errorf("content = %q", content)
	}

	got := <-captured
	if got.auth != "Bearer sk-test" || got.accept != "application/json" {
		t.Errorf("headers = auth %q accept %q", got.auth, got.accept)
	}
	if got.body.Stream || got.body.Model != "deepseek-chat" || got.body.Temperature != 0.2 {
		t.Errorf("upstream body = %+v", got.body)
	}
}

func TestCompleteChatAggregatesSSEResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"第一段\"}}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"第二段\"}}]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	client := NewClient(server.URL, "", "test-model")
	content, err := client.CompleteChat(context.Background(), []Message{{Role: "user", Content: "hi"}}, 0.3)
	if err != nil {
		t.Fatalf("CompleteChat returned error: %v", err)
	}
	if content != "第一段第二段" {
		t.Errorf("content = %q", content)
	}
}

func TestCompleteChatReportsUpstreamErrorPayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"error":{"message":"quota exceeded"}}`)
	}))
	defer server.Close()

	client := NewClient(server.URL, "", "test-model")
	_, err := client.CompleteChat(context.Background(), []Message{{Role: "user", Content: "hi"}}, 0.3)
	if err == nil || !strings.Contains(err.Error(), "quota exceeded") {
		t.Fatalf("error = %v, want quota exceeded", err)
	}
}

// ---- MODEL-01 重试 / 退避 / 错误分类 ----

func TestIsRetryableError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"ctx canceled", context.Canceled, false},
		{"ctx deadline", context.DeadlineExceeded, false},
		{"callback error", &callbackError{err: errors.New("x")}, false},
		{"status 401", &statusError{StatusCode: 401}, false},
		{"status 403", &statusError{StatusCode: 403}, false},
		{"status 408", &statusError{StatusCode: 408}, true},
		{"status 429", &statusError{StatusCode: 429}, true},
		{"status 500", &statusError{StatusCode: 500}, true},
		{"status 503", &statusError{StatusCode: 503}, true},
		{"io EOF", io.EOF, true},
		{"io unexpected EOF", io.ErrUnexpectedEOF, true},
		{"timeout awaiting response headers", errors.New("net/http: timeout awaiting response headers"), true},
		{"connection refused", errors.New("dial tcp: connection refused"), true},
		{"unexpected eof wrapped", fmt.Errorf("llm: read stream: %w", io.ErrUnexpectedEOF), true},
		{"plain json error", errors.New("llm: decode response: invalid character"), false},
	}
	for _, tc := range cases {
		if got := isRetryableError(tc.err); got != tc.want {
			t.Errorf("%s: isRetryableError = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestStreamChatRetriesTransientStatus(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&attempts, 1)
		if n == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprint(w, `{"error":{"message":"temporarily unavailable"}}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"雨声\"}}]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	client := NewClient(server.URL, "", "test-model")
	client.Retry.BaseDelay = time.Millisecond
	client.sleep = func(context.Context, time.Duration) error { return nil }

	var deltas []string
	err := client.StreamChat(context.Background(), []Message{{Role: "user", Content: "hi"}}, 0.5, func(delta string) error {
		deltas = append(deltas, delta)
		return nil
	})
	if err != nil {
		t.Fatalf("StreamChat returned error: %v", err)
	}
	if got := strings.Join(deltas, ""); got != "雨声" {
		t.Errorf("deltas = %q, want 雨声", got)
	}
	if got := atomic.LoadInt32(&attempts); got != 2 {
		t.Errorf("attempts = %d, want 2", got)
	}
}

func TestCompleteChatRetriesTransientStatus(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&attempts, 1)
		if n == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprint(w, `{"error":{"message":"temporarily unavailable"}}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"重连成功。"}}]}`)
	}))
	defer server.Close()

	client := NewClient(server.URL, "", "test-model")
	client.Retry.BaseDelay = time.Millisecond
	client.sleep = func(context.Context, time.Duration) error { return nil }

	content, err := client.CompleteChat(context.Background(), []Message{{Role: "user", Content: "hi"}}, 0.3)
	if err != nil {
		t.Fatalf("CompleteChat returned error: %v", err)
	}
	if content != "重连成功。" {
		t.Errorf("content = %q", content)
	}
	if got := atomic.LoadInt32(&attempts); got != 2 {
		t.Errorf("attempts = %d, want 2", got)
	}
}

func TestCompleteChatDoesNotRetryClientError(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"error":{"message":"bad request"}}`)
	}))
	defer server.Close()

	client := NewClient(server.URL, "", "test-model")
	client.Retry.BaseDelay = time.Millisecond
	client.sleep = func(context.Context, time.Duration) error { return nil }

	_, err := client.CompleteChat(context.Background(), []Message{{Role: "user", Content: "hi"}}, 0.3)
	if err == nil || !strings.Contains(err.Error(), "400") {
		t.Fatalf("error = %v, want status 400", err)
	}
	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Errorf("attempts = %d, want 1 (4xx must not retry)", got)
	}
}

func TestStreamChatDoesNotRetryAfterDelta(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		hj := w.(http.Hijacker)
		conn, _, err := hj.Hijack()
		if err != nil {
			return
		}
		fmt.Fprintf(conn, "HTTP/1.1 200 OK\r\nContent-Type: text/event-stream\r\n\r\n")
		fmt.Fprintf(conn, "data: {\"choices\":[{\"delta\":{\"content\":\"半句\"}}]}\n\n")
		time.Sleep(20 * time.Millisecond)
		if tc, ok := conn.(*net.TCPConn); ok {
			_ = tc.SetLinger(0)
		}
		conn.Close()
	}))
	defer server.Close()

	client := NewClient(server.URL, "", "test-model")
	client.Retry.MaxAttempts = 3
	client.Retry.BaseDelay = time.Millisecond
	client.sleep = func(context.Context, time.Duration) error { return nil }

	var deltas []string
	err := client.StreamChat(context.Background(), []Message{{Role: "user", Content: "hi"}}, 0.5, func(delta string) error {
		deltas = append(deltas, delta)
		return nil
	})
	if err == nil {
		t.Fatalf("expected error after mid-stream drop, got nil")
	}
	if got := strings.Join(deltas, ""); got != "半句" {
		t.Errorf("deltas = %q, want 半句", got)
	}
	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Errorf("attempts = %d, want 1 (no retry after content delivered)", got)
	}
}

func TestStreamChatRespectsContextCancel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	client := NewClient(server.URL, "", "test-model")
	client.Retry.MaxAttempts = 3
	client.Retry.BaseDelay = time.Millisecond
	client.sleep = func(context.Context, time.Duration) error { return nil }

	err := client.StreamChat(ctx, []Message{{Role: "user", Content: "hi"}}, 0.5, func(string) error { return nil })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

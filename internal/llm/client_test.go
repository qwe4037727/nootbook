package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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

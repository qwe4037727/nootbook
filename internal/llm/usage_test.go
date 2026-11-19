package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCompleteChatParsesUsage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":11,"completion_tokens":4,"total_tokens":15}}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "key", "model")
	result, err := client.CompleteChat(context.Background(), []Message{{Role: "user", Content: "hi"}}, 0)
	if err != nil {
		t.Fatalf("CompleteChat: %v", err)
	}
	if result.Content != "ok" || result.PromptTokens != 11 || result.CompletionTokens != 4 || result.TotalTokens != 15 {
		t.Errorf("result = %+v", result)
	}
}

func TestStreamChatParsesUsageEvent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"he\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"llo\"},\"finish_reason\":\"stop\"}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[],\"usage\":{\"prompt_tokens\":7,\"completion_tokens\":2,\"total_tokens\":9}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	client := NewClient(server.URL, "key", "model")
	var content strings.Builder
	result, err := client.StreamChat(context.Background(), []Message{{Role: "user", Content: "hi"}}, 0, func(delta string) error {
		content.WriteString(delta)
		return nil
	})
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}
	if content.String() != "hello" || result.FinishReason != "stop" {
		t.Errorf("content = %q result = %+v", content.String(), result)
	}
	if result.PromptTokens != 7 || result.CompletionTokens != 2 || result.TotalTokens != 9 {
		t.Errorf("usage = %+v", result)
	}
}

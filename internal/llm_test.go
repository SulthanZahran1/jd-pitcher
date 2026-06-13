package internal

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestChatRetriesWhenReasoningConsumesTokenBudget(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++

		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}

		if requests == 1 {
			if req.MaxTokens != 900 {
				t.Fatalf("first request MaxTokens = %d, want 900 minimum", req.MaxTokens)
			}
			writeChatResponse(t, w, "", "length")
			return
		}

		if req.MaxTokens != 1800 {
			t.Fatalf("retry request MaxTokens = %d, want doubled budget 1800", req.MaxTokens)
		}
		writeChatResponse(t, w, "• Built SEC financial data pipeline using Python and LLM APIs.", "stop")
	}))
	defer server.Close()

	client := &DeepSeekClient{
		BaseURL:     server.URL,
		APIKey:      "test-key",
		Model:       "deepseek-v4-flash",
		MaxTokens:   600,
		Temperature: 0.2,
		HTTPClient:  server.Client(),
	}

	pitch, err := client.Chat("system", "jd")
	if err != nil {
		t.Fatalf("Chat returned error: %v", err)
	}
	if pitch == "" {
		t.Fatal("expected non-empty pitch after retry")
	}
	if requests != 2 {
		t.Fatalf("requests = %d, want 2", requests)
	}
}

func TestChatRejectsEmptySuccessfulResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeChatResponse(t, w, "   ", "stop")
	}))
	defer server.Close()

	client := &DeepSeekClient{
		BaseURL:     server.URL,
		APIKey:      "test-key",
		Model:       "deepseek-v4-flash",
		MaxTokens:   1200,
		Temperature: 0.2,
		HTTPClient:  server.Client(),
	}

	if _, err := client.Chat("system", "jd"); err == nil {
		t.Fatal("expected empty LLM content to be treated as an error")
	}
}

func writeChatResponse(t *testing.T, w http.ResponseWriter, content, finishReason string) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	resp := map[string]any{
		"choices": []map[string]any{
			{
				"message": map[string]any{
					"content": content,
				},
				"finish_reason": finishReason,
			},
		},
	}
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		t.Fatalf("encode response: %v", err)
	}
}

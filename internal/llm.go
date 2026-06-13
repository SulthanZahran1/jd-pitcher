package internal

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// DeepSeekClient talks to any OpenAI-compatible chat completions endpoint.
type DeepSeekClient struct {
	BaseURL     string
	APIKey      string
	Model       string
	MaxTokens   int
	Temperature float64
	HTTPClient  *http.Client
}

type chatRequest struct {
	Model       string    `json:"model"`
	Messages    []message `json:"messages"`
	MaxTokens   int       `json:"max_tokens"`
	Temperature float64   `json:"temperature"`
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func NewDeepSeekClient(cfg *Config) *DeepSeekClient {
	return &DeepSeekClient{
		BaseURL:     cfg.LLMBaseURL,
		APIKey:      cfg.LLMAPIKey,
		Model:       cfg.LLMModel,
		MaxTokens:   cfg.LLMMaxTokens,
		Temperature: cfg.LLMTemperature,
		HTTPClient:  &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *DeepSeekClient) Chat(systemPrompt, userContent string) (string, error) {
	maxTokens := c.MaxTokens
	if maxTokens < 900 {
		maxTokens = 900
	}

	content, finishReason, err := c.chatOnce(systemPrompt, userContent, maxTokens)
	if err != nil {
		return "", err
	}
	content = strings.TrimSpace(content)
	if content != "" {
		return content, nil
	}

	// DeepSeek v4-flash can spend the whole completion budget on reasoning tokens
	// and return HTTP 200 with an empty final message. Retry once with a larger
	// budget instead of logging a false-success empty pitch.
	if finishReason == "length" {
		retryTokens := maxTokens * 2
		if retryTokens < 1600 {
			retryTokens = 1600
		}
		content, _, err = c.chatOnce(systemPrompt, userContent, retryTokens)
		if err != nil {
			return "", err
		}
		content = strings.TrimSpace(content)
	}
	if content == "" {
		return "", fmt.Errorf("LLM returned empty content")
	}
	return content, nil
}

func (c *DeepSeekClient) chatOnce(systemPrompt, userContent string, maxTokens int) (string, string, error) {
	reqBody := chatRequest{
		Model: c.Model,
		Messages: []message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userContent},
		},
		MaxTokens:   maxTokens,
		Temperature: c.Temperature,
	}

	b, err := json.Marshal(reqBody)
	if err != nil {
		return "", "", err
	}

	req, err := http.NewRequest("POST", c.BaseURL+"/chat/completions", bytes.NewReader(b))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", err
	}

	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("LLM HTTP %d: %s", resp.StatusCode, string(body))
	}

	var chatResp chatResponse
	if err := json.Unmarshal(body, &chatResp); err != nil {
		return "", "", err
	}

	if chatResp.Error != nil {
		return "", "", fmt.Errorf("LLM error: %s", chatResp.Error.Message)
	}

	if len(chatResp.Choices) == 0 {
		return "", "", fmt.Errorf("no choices returned from LLM")
	}

	choice := chatResp.Choices[0]
	return choice.Message.Content, choice.FinishReason, nil
}

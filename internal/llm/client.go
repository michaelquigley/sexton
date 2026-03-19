package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/michaelquigley/sexton/internal/config"
)

type Client struct {
	endpoint   string
	model      string
	apiKey     string
	maxTokens  int
	httpClient *http.Client
}

// NewClient returns a Client for the given LLM config, or nil if the config is
// nil or has no endpoint configured.
func NewClient(cfg *config.LLMConfig) *Client {
	if cfg == nil || cfg.Endpoint == "" {
		return nil
	}

	var apiKey string
	if cfg.APIKeyEnv != "" {
		apiKey = os.Getenv(cfg.APIKeyEnv)
	}

	maxTokens := cfg.MaxTokens
	if maxTokens == 0 {
		maxTokens = 512
	}

	return &Client{
		endpoint:   cfg.Endpoint,
		model:      cfg.Model,
		apiKey:     apiKey,
		maxTokens:  maxTokens,
		httpClient: &http.Client{},
	}
}

type chatRequest struct {
	Model     string        `json:"model"`
	Messages  []chatMessage `json:"messages"`
	MaxTokens int           `json:"max_tokens,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// Complete sends a chat completion request and returns the response content.
// if maxTokens is 0, the client's configured default is used.
func (c *Client) Complete(ctx context.Context, systemPrompt string, userMessage string, maxTokens int) (string, error) {
	if maxTokens == 0 {
		maxTokens = c.maxTokens
	}

	req := chatRequest{
		Model: c.model,
		Messages: []chatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userMessage},
		},
		MaxTokens: maxTokens,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("marshaling request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	httpClient := c.httpClient
	if httpClient == nil {
		httpClient = &http.Client{}
	}

	resp, err := httpClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("sending request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("llm returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var chatResp chatResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return "", fmt.Errorf("parsing response: %w", err)
	}

	if len(chatResp.Choices) == 0 {
		return "", fmt.Errorf("llm returned no choices")
	}

	return strings.TrimSpace(chatResp.Choices[0].Message.Content), nil
}

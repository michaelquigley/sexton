package llm

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/michaelquigley/sexton/internal/config"
)

func TestNewClientAPIKeyFromConfig(t *testing.T) {
	client := NewClient(&config.LLMConfig{
		Endpoint: "http://localhost:8080/v1/chat/completions",
		Model:    "test-model",
		APIKey:   "direct-key",
	})
	if client == nil {
		t.Fatal("NewClient() = nil, want client")
	}
	if client.apiKey != "direct-key" {
		t.Fatalf("client.apiKey = %q, want %q", client.apiKey, "direct-key")
	}
}

func TestNewClientAPIKeyEnvWinsOverConfig(t *testing.T) {
	t.Setenv("SEXTON_TEST_LLM_API_KEY", "env-key")

	client := NewClient(&config.LLMConfig{
		Endpoint:  "http://localhost:8080/v1/chat/completions",
		Model:     "test-model",
		APIKey:    "direct-key",
		APIKeyEnv: "SEXTON_TEST_LLM_API_KEY",
	})
	if client == nil {
		t.Fatal("NewClient() = nil, want client")
	}
	if client.apiKey != "env-key" {
		t.Fatalf("client.apiKey = %q, want %q", client.apiKey, "env-key")
	}
}

func TestNewClientAPIKeyEnvEmptyFallsBackToConfig(t *testing.T) {
	t.Setenv("SEXTON_TEST_LLM_API_KEY", "")

	client := NewClient(&config.LLMConfig{
		Endpoint:  "http://localhost:8080/v1/chat/completions",
		Model:     "test-model",
		APIKey:    "direct-key",
		APIKeyEnv: "SEXTON_TEST_LLM_API_KEY",
	})
	if client == nil {
		t.Fatal("NewClient() = nil, want client")
	}
	if client.apiKey != "direct-key" {
		t.Fatalf("client.apiKey = %q, want %q", client.apiKey, "direct-key")
	}
}

func TestNewClientWithoutAnyAPIKey(t *testing.T) {
	client := NewClient(&config.LLMConfig{
		Endpoint: "http://localhost:8080/v1/chat/completions",
		Model:    "test-model",
	})
	if client == nil {
		t.Fatal("NewClient() = nil, want client")
	}
	if client.apiKey != "" {
		t.Fatalf("client.apiKey = %q, want empty", client.apiKey)
	}
}

func TestCompleteUsesDefaultClientWithoutHardTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"ok"}}]}`)
	}))
	defer server.Close()

	client := &Client{
		endpoint:   server.URL,
		model:      "test-model",
		httpClient: &http.Client{},
	}

	got, err := client.Complete(context.Background(), "system", "user", 32)
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if got != "ok" {
		t.Fatalf("Complete() result = %q, want %q", got, "ok")
	}
}

func TestCompleteRespectsCanceledContext(t *testing.T) {
	requestStarted := make(chan struct{})
	releaseRequest := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(requestStarted)
		<-releaseRequest
	}))
	defer server.Close()
	defer close(releaseRequest)

	client := &Client{
		endpoint:   server.URL,
		model:      "test-model",
		httpClient: &http.Client{},
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := client.Complete(ctx, "system", "user", 32)
		done <- err
	}()

	<-requestStarted
	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Complete() error = nil, want context cancellation")
		}
		if !strings.Contains(err.Error(), "sending request") {
			t.Fatalf("Complete() error = %q, want wrapped request cancellation", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Complete() did not return after context cancellation")
	}
}

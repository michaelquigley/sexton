package llm

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

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

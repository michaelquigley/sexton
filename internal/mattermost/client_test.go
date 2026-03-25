package mattermost

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/michaelquigley/sexton/internal/config"
)

func TestNewClientTokenFromEnv(t *testing.T) {
	origLookup := lookupEnv
	defer func() { lookupEnv = origLookup }()

	lookupEnv = func(key string) string {
		if key == "MM_TOKEN" {
			return "env-token"
		}
		return ""
	}

	cfg := &config.MattermostConfig{
		URL:      "http://localhost",
		TokenEnv: "MM_TOKEN",
		Token:    "fallback-token",
	}
	c := NewClient(cfg)
	if c.token != "env-token" {
		t.Errorf("expected env token, got %q", c.token)
	}
}

func TestNewClientTokenFallback(t *testing.T) {
	origLookup := lookupEnv
	defer func() { lookupEnv = origLookup }()

	lookupEnv = func(key string) string { return "" }

	cfg := &config.MattermostConfig{
		URL:      "http://localhost",
		TokenEnv: "MM_TOKEN",
		Token:    "direct-token",
	}
	c := NewClient(cfg)
	if c.token != "direct-token" {
		t.Errorf("expected direct token, got %q", c.token)
	}
}

func TestNewClientDefaultTriggerWords(t *testing.T) {
	origLookup := lookupEnv
	defer func() { lookupEnv = origLookup }()
	lookupEnv = func(key string) string { return "" }

	cfg := &config.MattermostConfig{URL: "http://localhost", Token: "t"}
	c := NewClient(cfg)
	if len(c.cfg.TriggerWords) != 1 || c.cfg.TriggerWords[0] != "sexton" {
		t.Errorf("expected default trigger word 'sexton', got %v", c.cfg.TriggerWords)
	}
}

func TestStartMissingToken(t *testing.T) {
	origLookup := lookupEnv
	defer func() { lookupEnv = origLookup }()
	lookupEnv = func(key string) string { return "" }

	cfg := &config.MattermostConfig{URL: "http://localhost"}
	c := NewClient(cfg)
	err := c.Start(&mockHandler{})
	if err == nil {
		t.Fatal("expected error for missing token")
	}
	if !strings.Contains(err.Error(), "token is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestPostMessage(t *testing.T) {
	var gotBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v4/posts" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("unexpected auth header: %s", r.Header.Get("Authorization"))
		}
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := &Client{
		cfg:        &config.MattermostConfig{URL: srv.URL},
		token:      "test-token",
		httpClient: srv.Client(),
	}
	err := c.PostMessage("chan123", "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotBody["channel_id"] != "chan123" {
		t.Errorf("expected channel_id 'chan123', got %q", gotBody["channel_id"])
	}
	if gotBody["message"] != "hello" {
		t.Errorf("expected message 'hello', got %q", gotBody["message"])
	}
}

func TestPostMessagePreservesBasePath(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := &Client{
		cfg:        &config.MattermostConfig{URL: srv.URL + "/mattermost/"},
		token:      "test-token",
		httpClient: srv.Client(),
	}
	err := c.PostMessage("chan123", "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotPath != "/mattermost/api/v4/posts" {
		t.Fatalf("request path = %q, want %q", gotPath, "/mattermost/api/v4/posts")
	}
}

func TestSelfMessageSuppression(t *testing.T) {
	c := &Client{
		cfg:       &config.MattermostConfig{URL: "http://localhost"},
		botUserID: "bot123",
		handler:   &mockHandler{},
	}

	postJSON, _ := json.Marshal(map[string]string{
		"user_id":    "bot123",
		"channel_id": "chan",
		"message":    "sexton status",
	})
	dataJSON, _ := json.Marshal(map[string]string{
		"post": string(postJSON),
	})
	eventJSON, _ := json.Marshal(map[string]interface{}{
		"event": "posted",
		"data":  json.RawMessage(dataJSON),
	})

	// should not panic or dispatch — just silently ignore
	c.handleMessage(eventJSON)
}

func TestAllowedUserFiltering(t *testing.T) {
	userSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"id":       "user456",
			"username": "stranger",
		})
	}))
	defer userSrv.Close()

	dispatched := false
	h := &mockHandler{
		statusFn: func(repo string) ([]RepoStatus, error) {
			dispatched = true
			return nil, nil
		},
	}

	c := &Client{
		cfg: &config.MattermostConfig{
			URL:          userSrv.URL,
			AllowedUsers: []string{"michael"},
			TriggerWords: []string{"sexton"},
		},
		botUserID:  "bot123",
		httpClient: userSrv.Client(),
		handler:    h,
		userCache:  make(map[string]string),
	}

	postJSON, _ := json.Marshal(map[string]string{
		"user_id":    "user456",
		"channel_id": "chan",
		"message":    "sexton status",
	})
	dataJSON, _ := json.Marshal(map[string]string{
		"post": string(postJSON),
	})
	eventJSON, _ := json.Marshal(map[string]interface{}{
		"event": "posted",
		"data":  json.RawMessage(dataJSON),
	})

	c.handleMessage(eventJSON)
	if dispatched {
		t.Error("expected command to be filtered for non-allowed user")
	}
}

func TestAllowedUserEmpty(t *testing.T) {
	var posted bool
	postSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		posted = true
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer postSrv.Close()

	h := &mockHandler{
		statusFn: func(repo string) ([]RepoStatus, error) {
			return nil, nil
		},
	}

	c := &Client{
		cfg: &config.MattermostConfig{
			URL:          postSrv.URL,
			AllowedUsers: nil, // empty = allow all
			TriggerWords: []string{"sexton"},
		},
		botUserID:  "bot123",
		httpClient: postSrv.Client(),
		handler:    h,
		userCache:  make(map[string]string),
	}

	postJSON, _ := json.Marshal(map[string]string{
		"user_id":    "user789",
		"channel_id": "chan",
		"message":    "sexton status",
	})
	dataJSON, _ := json.Marshal(map[string]string{
		"post": string(postJSON),
	})
	eventJSON, _ := json.Marshal(map[string]interface{}{
		"event": "posted",
		"data":  json.RawMessage(dataJSON),
	})

	c.handleMessage(eventJSON)
	if !posted {
		t.Error("expected command to be dispatched when allowed users is empty")
	}
}

func TestUsernameCaching(t *testing.T) {
	apiCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiCalls++
		_ = json.NewEncoder(w).Encode(map[string]string{
			"id":       "user1",
			"username": "michael",
		})
	}))
	defer srv.Close()

	c := &Client{
		cfg:        &config.MattermostConfig{URL: srv.URL},
		httpClient: srv.Client(),
		userCache:  make(map[string]string),
	}

	u1, err := c.resolveUsername("user1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if u1 != "michael" {
		t.Errorf("expected 'michael', got %q", u1)
	}

	u2, err := c.resolveUsername("user1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if u2 != "michael" {
		t.Errorf("expected 'michael', got %q", u2)
	}

	if apiCalls != 1 {
		t.Errorf("expected 1 api call (cached), got %d", apiCalls)
	}
}

func TestExtractCommandMention(t *testing.T) {
	c := &Client{
		cfg:       &config.MattermostConfig{TriggerWords: []string{"sexton"}},
		botUserID: "bot123",
	}

	mentions, _ := json.Marshal([]string{"bot123"})
	text, ok := c.extractCommand("@sexton-laptop status", string(mentions))
	if !ok {
		t.Fatal("expected match")
	}
	if text != "status" {
		t.Errorf("expected 'status', got %q", text)
	}
}

func TestExtractCommandTriggerWord(t *testing.T) {
	c := &Client{
		cfg:       &config.MattermostConfig{TriggerWords: []string{"sexton"}},
		botUserID: "bot123",
	}

	text, ok := c.extractCommand("sexton sync notes", "")
	if !ok {
		t.Fatal("expected match")
	}
	if text != "sync notes" {
		t.Errorf("expected 'sync notes', got %q", text)
	}
}

func TestExtractCommandNoMatch(t *testing.T) {
	c := &Client{
		cfg:       &config.MattermostConfig{TriggerWords: []string{"sexton"}},
		botUserID: "bot123",
	}

	_, ok := c.extractCommand("hello world", "")
	if ok {
		t.Fatal("expected no match")
	}
}

func TestExtractCommandMentionMultipleBots(t *testing.T) {
	c := &Client{
		cfg:       &config.MattermostConfig{TriggerWords: []string{"sexton"}},
		botUserID: "bot123",
	}

	mentions, _ := json.Marshal([]string{"bot123", "bot456"})
	text, ok := c.extractCommand("@sexton-laptop @sexton-desktop sync grimoire", string(mentions))
	if !ok {
		t.Fatal("expected match")
	}
	if text != "sync grimoire" {
		t.Errorf("expected 'sync grimoire', got %q", text)
	}
}

// mockHandler is defined in commands_test.go but this file needs its own
// since tests in the same package share the type.

func TestBuildWebSocketURL(t *testing.T) {
	tests := []struct {
		url    string
		expect string
	}{
		{"https://mm.local", "wss://mm.local/api/v4/websocket"},
		{"https://mm.local/", "wss://mm.local/api/v4/websocket"},
		{"https://mm.local/mattermost", "wss://mm.local/mattermost/api/v4/websocket"},
		{"https://mm.local/mattermost/", "wss://mm.local/mattermost/api/v4/websocket"},
		{"http://mm.local", "ws://mm.local/api/v4/websocket"},
		{"http://mm.local:8065", "ws://mm.local:8065/api/v4/websocket"},
	}
	for _, tt := range tests {
		c := &Client{cfg: &config.MattermostConfig{URL: tt.url}}
		got, err := c.buildWebSocketURL()
		if err != nil {
			t.Fatalf("unexpected error for %q: %v", tt.url, err)
		}
		if got != tt.expect {
			t.Errorf("for %q: expected %q, got %q", tt.url, tt.expect, got)
		}
	}
}

// stub to satisfy the type needed by Start - we can't use the mockHandler from
// commands_test.go since that would be a duplicate definition. However since
// mockHandler is already defined in commands_test.go and we are in the same
// package, we can use it directly. Removing this duplicate comment.

func newTestClient(srv *httptest.Server) *Client {
	return &Client{
		cfg: &config.MattermostConfig{
			URL:          srv.URL,
			TriggerWords: []string{"sexton"},
		},
		token:      "test-token",
		httpClient: srv.Client(),
		userCache:  make(map[string]string),
		stopCh:     make(chan struct{}),
		doneCh:     make(chan struct{}),
	}
}

func TestStartAuthSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v4/users/me":
			_ = json.NewEncoder(w).Encode(map[string]string{
				"id":       "bot123",
				"username": "sexton-test",
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := newTestClient(srv)
	// Start will fail at WebSocket connect (httptest doesn't support WS upgrade)
	// but it should succeed past authentication
	err := c.Start(&mockHandler{})
	if err == nil {
		// if it somehow succeeded (shouldn't with httptest), stop it
		c.Stop()
	} else {
		// expected: websocket connection failure after successful auth
		if strings.Contains(err.Error(), "authenticate") {
			t.Errorf("auth should have succeeded, got: %v", err)
		}
	}
	if c.botUserID != "bot123" {
		t.Errorf("expected bot id 'bot123', got %q", c.botUserID)
	}
	if c.botUsername != "sexton-test" {
		t.Errorf("expected bot username 'sexton-test', got %q", c.botUsername)
	}
}

func TestStartAuthFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message": "invalid token"}`))
	}))
	defer srv.Close()

	c := newTestClient(srv)
	err := c.Start(&mockHandler{})
	if err == nil {
		c.Stop()
		t.Fatal("expected error for auth failure")
	}
	if !strings.Contains(err.Error(), "authenticate") {
		t.Errorf("expected auth error, got: %v", err)
	}
}

// ensure the mockHandler satisfies CommandHandler at compile time
var _ CommandHandler = (*mockHandler)(nil)

// note: mockHandler is defined in commands_test.go and is available here
// since both files are in the same test package.

// unused import guard
var _ = time.Second

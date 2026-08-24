package mattermost

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/michaelquigley/sexton/internal/agent"
	"github.com/michaelquigley/sexton/internal/config"
)

type recordedPost struct {
	ChannelID string `json:"channel_id"`
	Message   string `json:"message"`
}

// dmTestServer serves the username, direct-channel, and post endpoints,
// recording every post and counting resolution calls.
func dmTestServer(t *testing.T, posts *[]recordedPost, resolves *int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/v4/users/username/"):
			*resolves++
			_ = json.NewEncoder(w).Encode(map[string]string{"id": "user-1"})
		case r.URL.Path == "/api/v4/channels/direct":
			_ = json.NewEncoder(w).Encode(map[string]string{"id": "dm-1"})
		case r.URL.Path == "/api/v4/posts":
			var p recordedPost
			_ = json.NewDecoder(r.Body).Decode(&p)
			*posts = append(*posts, p)
			_ = json.NewEncoder(w).Encode(map[string]string{"id": "post-1"})
		default:
			http.NotFound(w, r)
		}
	}))
}

func dmTestClient(srv *httptest.Server) *Client {
	c := NewClient(&config.MattermostConfig{URL: srv.URL, Token: "t"})
	c.botUserID = "bot123"
	return c
}

func attentionEvent() agent.AlertEvent {
	return agent.AlertEvent{
		Severity:  "attention",
		RepoName:  "grimoire",
		Message:   `"concepts/idea.md" (pulls paused)`,
		Timestamp: time.Now(),
	}
}

func TestAlertAttentionDeliversDMInsteadOfChannel(t *testing.T) {
	var posts []recordedPost
	resolves := 0
	srv := dmTestServer(t, &posts, &resolves)
	defer srv.Close()

	a := NewAlerter(dmTestClient(srv), "alerts", []string{"michael"}, []string{"michael"})
	if err := a.Alert(context.Background(), attentionEvent()); err != nil {
		t.Fatalf("Alert() error = %v", err)
	}
	if len(posts) != 1 || posts[0].ChannelID != "dm-1" {
		t.Fatalf("posts = %#v, want one post to the dm channel", posts)
	}
	if strings.Contains(posts[0].Message, "@michael") {
		t.Fatalf("dm message carries a mention prefix: %q", posts[0].Message)
	}
	if !strings.Contains(posts[0].Message, "**attention**") {
		t.Fatalf("dm message lost the attention marker: %q", posts[0].Message)
	}
}

func TestAlertAttentionDMChannelCached(t *testing.T) {
	var posts []recordedPost
	resolves := 0
	srv := dmTestServer(t, &posts, &resolves)
	defer srv.Close()

	a := NewAlerter(dmTestClient(srv), "alerts", nil, []string{"michael"})
	for i := 0; i < 2; i++ {
		if err := a.Alert(context.Background(), attentionEvent()); err != nil {
			t.Fatalf("Alert() error = %v", err)
		}
	}
	if resolves != 1 {
		t.Fatalf("username resolutions = %d, want 1 (cached)", resolves)
	}
	if len(posts) != 2 {
		t.Fatalf("posts = %d, want 2", len(posts))
	}
}

func TestAlertAttentionFallsBackToChannelWhenDMFails(t *testing.T) {
	var posts []recordedPost
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/v4/users/username/"):
			http.Error(w, "not found", http.StatusNotFound)
		case r.URL.Path == "/api/v4/posts":
			var p recordedPost
			_ = json.NewDecoder(r.Body).Decode(&p)
			posts = append(posts, p)
			_ = json.NewEncoder(w).Encode(map[string]string{"id": "post-1"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	a := NewAlerter(dmTestClient(srv), "alerts", []string{"michael"}, []string{"nobody"})
	if err := a.Alert(context.Background(), attentionEvent()); err != nil {
		t.Fatalf("Alert() error = %v, want nil after channel fallback", err)
	}
	if len(posts) != 1 || posts[0].ChannelID != "alerts" {
		t.Fatalf("posts = %#v, want one fallback post to the channel", posts)
	}
	if !strings.Contains(posts[0].Message, "@michael") {
		t.Fatalf("fallback post lost the configured mention: %q", posts[0].Message)
	}
}

func TestAlertNonAttentionIgnoresDMUsers(t *testing.T) {
	var posts []recordedPost
	resolves := 0
	srv := dmTestServer(t, &posts, &resolves)
	defer srv.Close()

	a := NewAlerter(dmTestClient(srv), "alerts", nil, []string{"michael"})
	event := attentionEvent()
	event.Severity = "error"
	if err := a.Alert(context.Background(), event); err != nil {
		t.Fatalf("Alert() error = %v", err)
	}
	if len(posts) != 1 || posts[0].ChannelID != "alerts" {
		t.Fatalf("posts = %#v, want one post to the channel", posts)
	}
	if resolves != 0 {
		t.Fatalf("username resolutions = %d, want 0 for non-attention severity", resolves)
	}
}

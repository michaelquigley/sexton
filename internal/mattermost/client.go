package mattermost

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/michaelquigley/df/dl"
	"github.com/michaelquigley/sexton/internal/config"
)

// Client manages a connection to a Mattermost server for posting messages
// and listening for commands via WebSocket.
type Client struct {
	cfg         *config.MattermostConfig
	token       string
	botUserID   string
	botUsername string
	handler     CommandHandler
	httpClient  *http.Client
	userCache   map[string]string
	mu          sync.Mutex
	ws          *websocket.Conn
	stopCh      chan struct{}
	doneCh      chan struct{}
}

// NewClient creates a new Mattermost client. The token is resolved from the
// environment variable named by cfg.TokenEnv, falling back to cfg.Token.
func NewClient(cfg *config.MattermostConfig) *Client {
	token := ""
	if cfg.TokenEnv != "" {
		token = lookupEnv(cfg.TokenEnv)
	}
	if token == "" {
		token = cfg.Token
	}
	if len(cfg.TriggerWords) == 0 {
		cfg.TriggerWords = []string{"sexton"}
	}
	return &Client{
		cfg:        cfg,
		token:      token,
		httpClient: &http.Client{Timeout: 10 * time.Second},
		userCache:  make(map[string]string),
		stopCh:     make(chan struct{}),
		doneCh:     make(chan struct{}),
	}
}

// Start authenticates with the Mattermost server, resolves the bot identity,
// opens a WebSocket connection, and begins listening for commands. Startup
// failures (missing token, auth failure) are fatal — no silent degradation.
func (c *Client) Start(handler CommandHandler) error {
	if c.token == "" {
		return fmt.Errorf("mattermost token is required (set token_env or token in config)")
	}
	c.handler = handler

	// resolve bot identity
	me, err := c.apiGet("/api/v4/users/me")
	if err != nil {
		return fmt.Errorf("failed to authenticate with mattermost: %w", err)
	}
	c.botUserID, _ = me["id"].(string)
	c.botUsername, _ = me["username"].(string)
	if c.botUserID == "" {
		return fmt.Errorf("mattermost /api/v4/users/me did not return a user id")
	}
	dl.Infof("mattermost bot identity: @%s (%s)", c.botUsername, c.botUserID)

	// open websocket
	if err := c.connectWebSocket(); err != nil {
		return fmt.Errorf("failed to connect mattermost websocket: %w", err)
	}

	go c.listen()
	return nil
}

// Stop closes the WebSocket connection and waits for the listen goroutine to exit.
func (c *Client) Stop() {
	close(c.stopCh)
	c.mu.Lock()
	if c.ws != nil {
		c.ws.Close()
	}
	c.mu.Unlock()
	<-c.doneCh
}

// PostMessage posts a message to the given channel via the REST API.
func (c *Client) PostMessage(channelID, text string) error {
	body, err := json.Marshal(map[string]string{
		"channel_id": channelID,
		"message":    text,
	})
	if err != nil {
		return err
	}
	postURL, err := c.buildAPIURL("/api/v4/posts")
	if err != nil {
		return err
	}
	req, err := http.NewRequest("POST", postURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("post message failed (status %d): %s", resp.StatusCode, string(respBody))
	}
	return nil
}

func (c *Client) connectWebSocket() error {
	wsURL, err := c.buildWebSocketURL()
	if err != nil {
		return err
	}
	header := http.Header{}
	header.Set("Authorization", "Bearer "+c.token)
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, header)
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.ws = conn
	c.mu.Unlock()
	return nil
}

func (c *Client) buildWebSocketURL() (string, error) {
	u, err := c.buildURL("/api/v4/websocket")
	if err != nil {
		return "", err
	}
	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	case "http":
		u.Scheme = "ws"
	}
	return u.String(), nil
}

func (c *Client) buildAPIURL(apiPath string) (string, error) {
	u, err := c.buildURL(apiPath)
	if err != nil {
		return "", err
	}
	return u.String(), nil
}

func (c *Client) buildURL(endpointPath string) (*url.URL, error) {
	u, err := url.Parse(c.cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("invalid mattermost url: %w", err)
	}
	u.Path = path.Join("/", u.Path, endpointPath)
	u.RawPath = ""
	return u, nil
}

func (c *Client) listen() {
	defer close(c.doneCh)
	for {
		select {
		case <-c.stopCh:
			return
		default:
		}

		c.mu.Lock()
		ws := c.ws
		c.mu.Unlock()
		if ws == nil {
			if !c.reconnect() {
				return
			}
			continue
		}

		_, message, err := ws.ReadMessage()
		if err != nil {
			select {
			case <-c.stopCh:
				return
			default:
			}
			dl.Warnf("mattermost websocket read error: %v", err)
			c.mu.Lock()
			c.ws = nil
			c.mu.Unlock()
			if !c.reconnect() {
				return
			}
			continue
		}

		c.handleMessage(message)
	}
}

func (c *Client) reconnect() bool {
	delays := []time.Duration{
		1 * time.Second,
		2 * time.Second,
		4 * time.Second,
		8 * time.Second,
		15 * time.Second,
		30 * time.Second,
	}
	for i := 0; ; i++ {
		select {
		case <-c.stopCh:
			return false
		default:
		}

		delay := delays[len(delays)-1]
		if i < len(delays) {
			delay = delays[i]
		}
		dl.Infof("mattermost reconnecting in %s...", delay)

		select {
		case <-c.stopCh:
			return false
		case <-time.After(delay):
		}

		if err := c.connectWebSocket(); err != nil {
			dl.Warnf("mattermost reconnect failed: %v", err)
			continue
		}
		dl.Infof("mattermost reconnected")
		return true
	}
}

var mentionPattern = regexp.MustCompile(`@\S+`)

type wsEvent struct {
	Event string          `json:"event"`
	Data  json.RawMessage `json:"data"`
}

type postedData struct {
	Post     string `json:"post"`
	Mentions string `json:"mentions"`
}

type post struct {
	UserID    string `json:"user_id"`
	ChannelID string `json:"channel_id"`
	Message   string `json:"message"`
}

func (c *Client) handleMessage(raw []byte) {
	var event wsEvent
	if err := json.Unmarshal(raw, &event); err != nil {
		return
	}
	if event.Event != "posted" {
		return
	}

	var data postedData
	if err := json.Unmarshal(event.Data, &data); err != nil {
		return
	}

	var p post
	if err := json.Unmarshal([]byte(data.Post), &p); err != nil {
		return
	}

	// ignore own messages
	if p.UserID == c.botUserID {
		return
	}

	// check allowed users
	if len(c.cfg.AllowedUsers) > 0 {
		username, err := c.resolveUsername(p.UserID)
		if err != nil {
			dl.Warnf("mattermost failed to resolve user '%s': %v", p.UserID, err)
			return
		}
		if !c.isAllowedUser(username) {
			return
		}
	}

	// determine command text via mention or trigger word path
	commandText, matched := c.extractCommand(p.Message, data.Mentions)
	if !matched {
		return
	}

	resp, recognized := Dispatch(commandText, c.handler)
	if !recognized {
		return
	}

	if err := c.PostMessage(p.ChannelID, resp); err != nil {
		dl.Warnf("mattermost failed to post response: %v", err)
	}
}

func (c *Client) extractCommand(message, mentionsJSON string) (string, bool) {
	// mention path: check if bot user ID is in the mentions list
	if mentionsJSON != "" {
		var mentions []string
		if err := json.Unmarshal([]byte(mentionsJSON), &mentions); err == nil {
			for _, id := range mentions {
				if id == c.botUserID {
					// strip all @mentions from the message
					text := mentionPattern.ReplaceAllString(message, "")
					text = strings.TrimSpace(text)
					return text, true
				}
			}
		}
	}

	// trigger word path
	text, ok := StripTriggerWord(message, c.cfg.TriggerWords)
	return text, ok
}

func (c *Client) isAllowedUser(username string) bool {
	for _, allowed := range c.cfg.AllowedUsers {
		if strings.EqualFold(allowed, username) {
			return true
		}
	}
	return false
}

func (c *Client) resolveUsername(userID string) (string, error) {
	c.mu.Lock()
	if username, ok := c.userCache[userID]; ok {
		c.mu.Unlock()
		return username, nil
	}
	c.mu.Unlock()

	data, err := c.apiGet("/api/v4/users/" + userID)
	if err != nil {
		return "", err
	}
	username, _ := data["username"].(string)
	if username == "" {
		return "", fmt.Errorf("mattermost user '%s' has no username", userID)
	}

	c.mu.Lock()
	c.userCache[userID] = username
	c.mu.Unlock()

	return username, nil
}

func (c *Client) apiGet(path string) (map[string]interface{}, error) {
	apiURL, err := c.buildAPIURL(path)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("mattermost api %s failed (status %d): %s", path, resp.StatusCode, string(body))
	}
	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result, nil
}

// lookupEnv is a variable to allow testing without modifying the real environment.
var lookupEnv = os.Getenv

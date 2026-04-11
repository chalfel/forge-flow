package cloud

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// ---------------------------------------------------------------------------
// Configuration
// ---------------------------------------------------------------------------

const (
	DefaultCloudURL     = "https://api.forgeflow.dev"
	DefaultWSURL        = "wss://api.forgeflow.dev/ws"
	DefaultTimeout      = 30 * time.Second
	ReconnectBaseDelay  = 1 * time.Second
	ReconnectMaxDelay   = 60 * time.Second
	HeartbeatInterval   = 30 * time.Second
	WriteWait           = 10 * time.Second
	PongWait            = 60 * time.Second
)

// Config holds client configuration.
type Config struct {
	CloudURL string
	WSURL    string
	Timeout  time.Duration
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		CloudURL: DefaultCloudURL,
		WSURL:    DefaultWSURL,
		Timeout:  DefaultTimeout,
	}
}

// ConfigFromEnv loads config from environment variables.
func ConfigFromEnv() Config {
	cfg := DefaultConfig()
	// TODO: read FORGE_CLOUD_URL, FORGE_WS_URL from env
	return cfg
}

// ---------------------------------------------------------------------------
// HTTP Client
// ---------------------------------------------------------------------------

// Client is the main Forge Cloud API client.
type Client struct {
	baseURL    string
	wsURL      string
	httpClient *http.Client
	creds      *Credentials

	// WebSocket state
	ws         *websocket.Conn
	wsMu       sync.Mutex
	wsHandlers map[string][]EventHandler
	wsCtx      context.Context
	wsCancel   context.CancelFunc
}

// EventHandler is a callback for WebSocket events.
type EventHandler func(msg WSMessage)

// NewClient creates a new Forge Cloud client.
func NewClient(cfg Config) *Client {
	return &Client{
		baseURL: cfg.CloudURL,
		wsURL:   cfg.WSURL,
		httpClient: &http.Client{
			Timeout: cfg.Timeout,
		},
		wsHandlers: make(map[string][]EventHandler),
	}
}

// NewClientWithCreds creates a client with pre-loaded credentials.
func NewClientWithCreds(cfg Config, creds Credentials) *Client {
	c := NewClient(cfg)
	c.creds = &creds
	return c
}

// NewClientFromStored creates a client using stored credentials.
func NewClientFromStored() (*Client, error) {
	creds, err := LoadCredentials()
	if err != nil {
		return nil, err
	}
	cfg := DefaultConfig()
	if creds.CloudURL != "" {
		cfg.CloudURL = creds.CloudURL
		// Derive WS URL from cloud URL
		u, _ := url.Parse(creds.CloudURL)
		if u != nil {
			wsScheme := "wss"
			if u.Scheme == "http" {
				wsScheme = "ws"
			}
			cfg.WSURL = fmt.Sprintf("%s://%s/ws", wsScheme, u.Host)
		}
	}
	return NewClientWithCreds(cfg, creds), nil
}

// SetCredentials updates the client's credentials.
func (c *Client) SetCredentials(creds Credentials) {
	c.creds = &creds
}

// ---------------------------------------------------------------------------
// HTTP methods
// ---------------------------------------------------------------------------

func (c *Client) request(method, path string, body any, result any) error {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshaling request: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, c.baseURL+path, bodyReader)
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "forge-cli/1.0")

	if c.creds != nil && c.creds.AccessToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.creds.AccessToken)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("unauthorized (run: forge login)")
	}

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API error %d: %s", resp.StatusCode, string(body))
	}

	if result != nil {
		if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
			return fmt.Errorf("decoding response: %w", err)
		}
	}

	return nil
}

func (c *Client) get(path string, result any) error {
	return c.request(http.MethodGet, path, nil, result)
}

func (c *Client) post(path string, body any, result any) error {
	return c.request(http.MethodPost, path, body, result)
}

func (c *Client) put(path string, body any, result any) error {
	return c.request(http.MethodPut, path, body, result)
}

func (c *Client) patch(path string, body any, result any) error {
	return c.request(http.MethodPatch, path, body, result)
}

func (c *Client) delete(path string, result any) error {
	return c.request(http.MethodDelete, path, nil, result)
}

// ---------------------------------------------------------------------------
// API methods
// ---------------------------------------------------------------------------

// GetMe returns the current user info.
func (c *Client) GetMe() (*UserInfo, error) {
	var user UserInfo
	if err := c.get("/users/me", &user); err != nil {
		return nil, err
	}
	return &user, nil
}

// UserInfo represents the current user.
type UserInfo struct {
	ID       string `json:"id"`
	Email    string `json:"email"`
	Name     string `json:"name"`
	TeamID   string `json:"teamId,omitempty"`
	TeamName string `json:"teamName,omitempty"`
}

// ListProjects returns all projects the user has access to.
func (c *Client) ListProjects() ([]CloudProject, error) {
	var projects []CloudProject
	if err := c.get("/projects", &projects); err != nil {
		return nil, err
	}
	return projects, nil
}

// GetProject returns a single project by ID.
func (c *Client) GetProject(projectID string) (*CloudProject, error) {
	var project CloudProject
	if err := c.get("/projects/"+projectID, &project); err != nil {
		return nil, err
	}
	return &project, nil
}

// ListAssignedTasks returns tasks assigned to the current user.
func (c *Client) ListAssignedTasks() ([]CloudTask, error) {
	var tasks []CloudTask
	if err := c.get("/tasks/assigned", &tasks); err != nil {
		return nil, err
	}
	return tasks, nil
}

// GetTask returns a single task by ID.
func (c *Client) GetTask(taskID string) (*CloudTask, error) {
	var task CloudTask
	if err := c.get("/tasks/"+taskID, &task); err != nil {
		return nil, err
	}
	return &task, nil
}

// UpdateTaskStatus updates a task's status.
func (c *Client) UpdateTaskStatus(taskID string, update TaskStatusUpdate) error {
	return c.patch("/tasks/"+taskID+"/status", update, nil)
}

// SendTaskProgress sends a progress event for a task.
func (c *Client) SendTaskProgress(event TaskProgressEvent) error {
	return c.post("/tasks/"+event.TaskID+"/progress", event, nil)
}

// GetSpec returns a spec by ID.
func (c *Client) GetSpec(specID string) (*CloudSpec, error) {
	var spec CloudSpec
	if err := c.get("/specs/"+specID, &spec); err != nil {
		return nil, err
	}
	return &spec, nil
}

// GetKnowledge fetches the project knowledge base content.
func (c *Client) GetKnowledge(projectID string) (*KnowledgeBase, error) {
	var kb KnowledgeBase
	if err := c.get("/projects/"+projectID+"/knowledge", &kb); err != nil {
		return nil, err
	}
	return &kb, nil
}

// KnowledgeBase holds all KB sections for a project.
type KnowledgeBase struct {
	ProjectID    string            `json:"projectId"`
	Constraints  string            `json:"constraints"`
	Memory       string            `json:"memory"`
	Architecture string            `json:"architecture"`
	Business     string            `json:"business"`
	Conventions  string            `json:"conventions"`
	Custom       map[string]string `json:"custom,omitempty"`
	UpdatedAt    time.Time         `json:"updatedAt"`
}

// ---------------------------------------------------------------------------
// WebSocket connection
// ---------------------------------------------------------------------------

// Connect establishes a WebSocket connection to Forge Cloud.
func (c *Client) Connect(ctx context.Context) error {
	if c.creds == nil || c.creds.AccessToken == "" {
		return fmt.Errorf("not authenticated")
	}

	header := http.Header{}
	header.Set("Authorization", "Bearer "+c.creds.AccessToken)

	conn, _, err := websocket.DefaultDialer.DialContext(ctx, c.wsURL, header)
	if err != nil {
		return fmt.Errorf("connecting to WebSocket: %w", err)
	}

	c.wsMu.Lock()
	c.ws = conn
	c.wsCtx, c.wsCancel = context.WithCancel(ctx)
	c.wsMu.Unlock()

	// Start read and heartbeat goroutines
	go c.wsReadLoop()
	go c.wsHeartbeatLoop()

	return nil
}

// Disconnect closes the WebSocket connection.
func (c *Client) Disconnect() {
	c.wsMu.Lock()
	defer c.wsMu.Unlock()

	if c.wsCancel != nil {
		c.wsCancel()
	}
	if c.ws != nil {
		c.ws.Close()
		c.ws = nil
	}
}

// OnEvent registers a handler for a specific message type.
func (c *Client) OnEvent(msgType string, handler EventHandler) {
	c.wsMu.Lock()
	defer c.wsMu.Unlock()
	c.wsHandlers[msgType] = append(c.wsHandlers[msgType], handler)
}

// Send sends a message over WebSocket.
func (c *Client) Send(msg WSMessage) error {
	c.wsMu.Lock()
	ws := c.ws
	c.wsMu.Unlock()

	if ws == nil {
		return fmt.Errorf("not connected")
	}

	ws.SetWriteDeadline(time.Now().Add(WriteWait))
	return ws.WriteJSON(msg)
}

func (c *Client) wsReadLoop() {
	defer c.Disconnect()

	c.wsMu.Lock()
	ws := c.ws
	ctx := c.wsCtx
	c.wsMu.Unlock()

	if ws == nil {
		return
	}

	ws.SetReadDeadline(time.Now().Add(PongWait))
	ws.SetPongHandler(func(string) error {
		ws.SetReadDeadline(time.Now().Add(PongWait))
		return nil
	})

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		var msg WSMessage
		if err := ws.ReadJSON(&msg); err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				// TODO: reconnect logic
			}
			return
		}

		c.dispatchMessage(msg)
	}
}

func (c *Client) wsHeartbeatLoop() {
	ticker := time.NewTicker(HeartbeatInterval)
	defer ticker.Stop()

	c.wsMu.Lock()
	ws := c.ws
	ctx := c.wsCtx
	c.wsMu.Unlock()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.wsMu.Lock()
			ws = c.ws
			c.wsMu.Unlock()

			if ws == nil {
				return
			}

			ws.SetWriteDeadline(time.Now().Add(WriteWait))
			if err := ws.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (c *Client) dispatchMessage(msg WSMessage) {
	c.wsMu.Lock()
	handlers := c.wsHandlers[msg.Type]
	c.wsMu.Unlock()

	for _, h := range handlers {
		h(msg)
	}
}

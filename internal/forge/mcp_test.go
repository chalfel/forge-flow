package forge

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

// mcpSession helps run the server in a goroutine and send/receive messages.
type mcpSession struct {
	svc    *Service
	stdin  *io.PipeWriter
	stdout *bytes.Buffer
	mu     sync.Mutex
	done   chan struct{}
	err    error
}

func newMCPSession(svc *Service) *mcpSession {
	inR, inW := io.Pipe()
	var out bytes.Buffer
	s := &mcpSession{
		svc:    svc,
		stdin:  inW,
		stdout: &out,
		done:   make(chan struct{}),
	}
	go func() {
		s.err = svc.ServeMCPOn(inR, &threadSafeWriter{w: &out, mu: &s.mu})
		close(s.done)
	}()
	return s
}

type threadSafeWriter struct {
	w  io.Writer
	mu *sync.Mutex
}

func (t *threadSafeWriter) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.w.Write(p)
}

func (s *mcpSession) send(t *testing.T, req MCPRequest) {
	t.Helper()
	req.JSONRPC = "2.0"
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	if _, err := s.stdin.Write(append(data, '\n')); err != nil {
		t.Fatalf("write stdin: %v", err)
	}
}

func (s *mcpSession) close() {
	_ = s.stdin.Close()
	<-s.done
}

func TestMCPInitialize(t *testing.T) {
	svc, _ := setupRunTestProject(t)
	sess := newMCPSession(svc)
	defer sess.close()

	sess.send(t, MCPRequest{ID: json.RawMessage(`1`), Method: "initialize"})

	resp := waitForResponse(t, sess, 1)
	if resp.Error != nil {
		t.Fatalf("initialize error: %+v", resp.Error)
	}
	result := resp.Result.(map[string]any)
	if result["protocolVersion"] != mcpProtocolVersion {
		t.Fatalf("expected protocolVersion %s, got %v", mcpProtocolVersion, result["protocolVersion"])
	}
	serverInfo := result["serverInfo"].(map[string]any)
	if serverInfo["name"] != "forge" {
		t.Fatalf("expected serverInfo.name=forge, got %v", serverInfo["name"])
	}
}

func TestMCPToolsList(t *testing.T) {
	svc, _ := setupRunTestProject(t)
	sess := newMCPSession(svc)
	defer sess.close()

	sess.send(t, MCPRequest{ID: json.RawMessage(`2`), Method: "tools/list"})

	resp := waitForResponse(t, sess, 1)
	if resp.Error != nil {
		t.Fatalf("tools/list error: %+v", resp.Error)
	}
	result := resp.Result.(map[string]any)
	toolsRaw, ok := result["tools"].([]any)
	if !ok {
		t.Fatalf("expected tools array, got %T", result["tools"])
	}
	if len(toolsRaw) < 5 {
		t.Fatalf("expected at least 5 tools, got %d", len(toolsRaw))
	}

	// Verify the core tools are present.
	names := map[string]bool{}
	for _, t := range toolsRaw {
		tool := t.(map[string]any)
		names[tool["name"].(string)] = true
	}
	required := []string{"forge_board", "forge_spec_lint", "forge_run_plan", "forge_run_preview", "forge_spec_detail"}
	for _, name := range required {
		if !names[name] {
			t.Fatalf("missing tool %s in registry", name)
		}
	}
}

func TestMCPToolCallBoard(t *testing.T) {
	svc, webRepo := setupRunTestProject(t)
	sess := newMCPSession(svc)
	defer sess.close()

	params := map[string]any{
		"name": "forge_board",
		"arguments": map[string]any{
			"cwd": webRepo,
		},
	}
	paramsJSON, _ := json.Marshal(params)

	sess.send(t, MCPRequest{
		ID:     json.RawMessage(`3`),
		Method: "tools/call",
		Params: paramsJSON,
	})

	resp := waitForResponse(t, sess, 1)
	if resp.Error != nil {
		t.Fatalf("tools/call error: %+v", resp.Error)
	}
	result := resp.Result.(map[string]any)
	content := result["content"].([]any)
	if len(content) == 0 {
		t.Fatal("expected content in tool result")
	}
	text := content[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "projectName") {
		t.Fatalf("expected board JSON in text, got: %s", text[:100])
	}
}

func TestMCPToolCallUnknownTool(t *testing.T) {
	svc, _ := setupRunTestProject(t)
	sess := newMCPSession(svc)
	defer sess.close()

	params := map[string]any{"name": "nonexistent_tool", "arguments": map[string]any{}}
	paramsJSON, _ := json.Marshal(params)
	sess.send(t, MCPRequest{ID: json.RawMessage(`4`), Method: "tools/call", Params: paramsJSON})

	resp := waitForResponse(t, sess, 1)
	if resp.Error == nil {
		t.Fatal("expected error for unknown tool")
	}
	if !strings.Contains(resp.Error.Message, "unknown tool") {
		t.Fatalf("expected 'unknown tool' error, got: %s", resp.Error.Message)
	}
}

func TestMCPNotificationIgnored(t *testing.T) {
	svc, _ := setupRunTestProject(t)
	sess := newMCPSession(svc)
	defer sess.close()

	// Send notification (no ID) — should produce no response.
	sess.send(t, MCPRequest{Method: "notifications/initialized"})
	// Then send a real request to verify the server is still alive.
	sess.send(t, MCPRequest{ID: json.RawMessage(`5`), Method: "tools/list"})

	resp := waitForResponse(t, sess, 1)
	if resp.Error != nil {
		t.Fatalf("tools/list after notification: %+v", resp.Error)
	}
}

func TestMCPUnknownMethod(t *testing.T) {
	svc, _ := setupRunTestProject(t)
	sess := newMCPSession(svc)
	defer sess.close()

	sess.send(t, MCPRequest{ID: json.RawMessage(`6`), Method: "bogus/method"})

	resp := waitForResponse(t, sess, 1)
	if resp.Error == nil {
		t.Fatal("expected error for unknown method")
	}
	if resp.Error.Code != -32601 {
		t.Fatalf("expected code -32601, got %d", resp.Error.Code)
	}
}

// waitForResponse polls the session buffer for at least N responses and returns the Nth.
func waitForResponse(t *testing.T, sess *mcpSession, n int) MCPResponse {
	t.Helper()
	for attempt := 0; attempt < 200; attempt++ {
		sess.mu.Lock()
		text := sess.stdout.String()
		sess.mu.Unlock()
		if text != "" {
			lines := strings.Split(strings.TrimSpace(text), "\n")
			if len(lines) >= n {
				var resp MCPResponse
				if err := json.Unmarshal([]byte(lines[n-1]), &resp); err != nil {
					t.Fatalf("unmarshal response: %v", err)
				}
				return resp
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for response #%d", n)
	return MCPResponse{}
}

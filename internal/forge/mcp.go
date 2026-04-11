package forge

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
)

// MCP protocol version supported by this server.
const mcpProtocolVersion = "2024-11-05"

// MCPRequest is a JSON-RPC 2.0 request.
type MCPRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// MCPResponse is a JSON-RPC 2.0 response.
type MCPResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *MCPError       `json:"error,omitempty"`
}

// MCPError is a JSON-RPC 2.0 error.
type MCPError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// MCPTool describes a tool exposed via MCP.
type MCPTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

// MCPContent is a piece of content returned from a tool call.
type MCPContent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// MCPToolResult wraps content returned from a tool execution.
type MCPToolResult struct {
	Content []MCPContent `json:"content"`
	IsError bool         `json:"isError,omitempty"`
}

type mcpToolExecutor func(s *Service, args map[string]any) (string, error)

type mcpToolDef struct {
	tool     MCPTool
	executor mcpToolExecutor
}

// ServeMCP runs the MCP server loop on stdin/stdout.
// This is the entry point called by `forge mcp serve`.
func (s *Service) ServeMCP() error {
	return s.ServeMCPOn(os.Stdin, os.Stdout)
}

// ServeMCPOn runs the MCP server loop on the given reader/writer.
// Useful for testing with in-memory pipes.
func (s *Service) ServeMCPOn(in io.Reader, out io.Writer) error {
	tools := buildMCPToolRegistry()

	reader := bufio.NewReader(in)
	writer := bufio.NewWriter(out)
	var writeMu sync.Mutex

	writeResponse := func(resp MCPResponse) {
		writeMu.Lock()
		defer writeMu.Unlock()
		resp.JSONRPC = "2.0"
		data, err := json.Marshal(resp)
		if err != nil {
			return
		}
		_, _ = writer.Write(data)
		_ = writer.WriteByte('\n')
		_ = writer.Flush()
	}

	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}

		var req MCPRequest
		if err := json.Unmarshal(line, &req); err != nil {
			// Invalid JSON — respond with parse error if we can extract an ID
			writeResponse(MCPResponse{
				Error: &MCPError{Code: -32700, Message: "parse error"},
			})
			continue
		}

		isNotification := len(req.ID) == 0

		switch req.Method {
		case "initialize":
			if isNotification {
				continue
			}
			writeResponse(MCPResponse{
				ID: req.ID,
				Result: map[string]any{
					"protocolVersion": mcpProtocolVersion,
					"capabilities": map[string]any{
						"tools": map[string]any{},
					},
					"serverInfo": map[string]any{
						"name":    "forge",
						"version": "0.1.0",
					},
				},
			})

		case "notifications/initialized", "initialized":
			// Notification — no response.

		case "tools/list":
			if isNotification {
				continue
			}
			toolList := make([]MCPTool, 0, len(tools))
			for _, t := range tools {
				toolList = append(toolList, t.tool)
			}
			writeResponse(MCPResponse{
				ID: req.ID,
				Result: map[string]any{
					"tools": toolList,
				},
			})

		case "tools/call":
			if isNotification {
				continue
			}
			var params struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			}
			if err := json.Unmarshal(req.Params, &params); err != nil {
				writeResponse(MCPResponse{
					ID:    req.ID,
					Error: &MCPError{Code: -32602, Message: "invalid params"},
				})
				continue
			}
			def, ok := tools[params.Name]
			if !ok {
				writeResponse(MCPResponse{
					ID:    req.ID,
					Error: &MCPError{Code: -32601, Message: fmt.Sprintf("unknown tool: %s", params.Name)},
				})
				continue
			}
			output, err := def.executor(s, params.Arguments)
			if err != nil {
				writeResponse(MCPResponse{
					ID: req.ID,
					Result: MCPToolResult{
						Content: []MCPContent{{Type: "text", Text: err.Error()}},
						IsError: true,
					},
				})
				continue
			}
			writeResponse(MCPResponse{
				ID: req.ID,
				Result: MCPToolResult{
					Content: []MCPContent{{Type: "text", Text: output}},
				},
			})

		default:
			if !isNotification {
				writeResponse(MCPResponse{
					ID:    req.ID,
					Error: &MCPError{Code: -32601, Message: fmt.Sprintf("method not found: %s", req.Method)},
				})
			}
		}
	}
}

func buildMCPToolRegistry() map[string]mcpToolDef {
	cwdProp := map[string]any{
		"type":        "string",
		"description": "Optional working directory to resolve Forge context from (defaults to current)",
	}

	return map[string]mcpToolDef{
		"forge_board": {
			tool: MCPTool{
				Name:        "forge_board",
				Description: "Get the Forge project board with all specs grouped by status lane (Ready, In Progress, In Review, Blocked, Done). Returns progress, task counts, and per-spec details.",
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"cwd": cwdProp,
					},
				},
			},
			executor: func(s *Service, args map[string]any) (string, error) {
				cwd, _ := args["cwd"].(string)
				board, err := s.Board(cwd)
				if err != nil {
					return "", err
				}
				return jsonPretty(board)
			},
		},

		"forge_context_resolve": {
			tool: MCPTool{
				Name:        "forge_context_resolve",
				Description: "Resolve the Forge context (workspace, project, repo, KB/specs paths) from a directory. Call this first to confirm where specs and runs live before other operations.",
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"cwd": cwdProp,
					},
				},
			},
			executor: func(s *Service, args map[string]any) (string, error) {
				cwd, _ := args["cwd"].(string)
				ctx, err := s.ResolveContext(cwd)
				if err != nil {
					return "", err
				}
				return jsonPretty(ctx)
			},
		},

		"forge_spec_lint": {
			tool: MCPTool{
				Name:        "forge_spec_lint",
				Description: "Lint Forge specs to detect missing metadata, scheduling conflicts, overlapping touches, and missing scaffolding tasks. Run this after creating or editing a spec.",
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"cwd": cwdProp,
						"spec": map[string]any{
							"type":        "string",
							"description": "Optional specific spec file or directory to lint",
						},
					},
				},
			},
			executor: func(s *Service, args map[string]any) (string, error) {
				cwd, _ := args["cwd"].(string)
				spec, _ := args["spec"].(string)
				report, err := s.LintSpecs(cwd, spec)
				if err != nil {
					return "", err
				}
				return jsonPretty(report)
			},
		},

		"forge_spec_detail": {
			tool: MCPTool{
				Name:        "forge_spec_detail",
				Description: "Get the full detail of a single spec: metadata, demo, expected behavior, test scenarios, validation plan, and all tasks with done-when criteria.",
				InputSchema: map[string]any{
					"type":     "object",
					"required": []string{"specFile"},
					"properties": map[string]any{
						"cwd": cwdProp,
						"specFile": map[string]any{
							"type":        "string",
							"description": "The relative spec file name, e.g. '2026-04-10-user-can-export-csv.md'",
						},
					},
				},
			},
			executor: func(s *Service, args map[string]any) (string, error) {
				cwd, _ := args["cwd"].(string)
				specFile, _ := args["specFile"].(string)
				if specFile == "" {
					return "", fmt.Errorf("specFile is required")
				}
				detail, err := s.SpecDetail(cwd, specFile)
				if err != nil {
					return "", err
				}
				return jsonPretty(detail)
			},
		},

		"forge_run_plan": {
			tool: MCPTool{
				Name:        "forge_run_plan",
				Description: "Compute the run plan for all specs: which tasks are safe to run in parallel (Safe), which must run serially (Serial), which are blocked by deps (Blocked), which have conflicts (Conflict), and which are invalid (Invalid).",
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"cwd": cwdProp,
						"spec": map[string]any{
							"type":        "string",
							"description": "Optional spec file to plan (defaults to all specs)",
						},
					},
				},
			},
			executor: func(s *Service, args map[string]any) (string, error) {
				cwd, _ := args["cwd"].(string)
				spec, _ := args["spec"].(string)
				plan, err := s.PlanRun(cwd, spec)
				if err != nil {
					return "", err
				}
				return jsonPretty(plan)
			},
		},

		"forge_run_preview": {
			tool: MCPTool{
				Name:        "forge_run_preview",
				Description: "Preview a run execution without mutating state. Shows what would be executed, which tasks would be skipped, and why. Safe to call anytime.",
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"cwd":  cwdProp,
						"spec": map[string]any{"type": "string"},
					},
				},
			},
			executor: func(s *Service, args map[string]any) (string, error) {
				cwd, _ := args["cwd"].(string)
				spec, _ := args["spec"].(string)
				result, err := s.PreviewRun(cwd, spec)
				if err != nil {
					return "", err
				}
				return jsonPretty(result)
			},
		},

		"forge_run_execute": {
			tool: MCPTool{
				Name:        "forge_run_execute",
				Description: "Execute a run in state-only apply mode. Persists a run record under .forge/runs/ and returns the result. Does NOT spawn agents — use this to record state transitions. For live agent spawning, use the CLI directly with --live.",
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"cwd":  cwdProp,
						"spec": map[string]any{"type": "string"},
					},
				},
			},
			executor: func(s *Service, args map[string]any) (string, error) {
				cwd, _ := args["cwd"].(string)
				spec, _ := args["spec"].(string)
				result, err := s.ExecuteRun(cwd, spec)
				if err != nil {
					return "", err
				}
				return jsonPretty(result)
			},
		},

		"forge_run_list": {
			tool: MCPTool{
				Name:        "forge_run_list",
				Description: "List all persisted run records for the current project, most recent first.",
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"cwd": cwdProp,
					},
				},
			},
			executor: func(s *Service, args map[string]any) (string, error) {
				cwd, _ := args["cwd"].(string)
				records, err := s.ListRunRecords(cwd)
				if err != nil {
					return "", err
				}
				return jsonPretty(records)
			},
		},

		"forge_history_list": {
			tool: MCPTool{
				Name:        "forge_history_list",
				Description: "List all tasks that have recorded execution attempts, grouped by spec.",
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"cwd": cwdProp,
					},
				},
			},
			executor: func(s *Service, args map[string]any) (string, error) {
				cwd, _ := args["cwd"].(string)
				history, err := s.ListTaskHistory(cwd)
				if err != nil {
					return "", err
				}
				return jsonPretty(history)
			},
		},

		"forge_history_show": {
			tool: MCPTool{
				Name:        "forge_history_show",
				Description: "Show all execution attempts and review feedback for a specific task.",
				InputSchema: map[string]any{
					"type":     "object",
					"required": []string{"specFile", "taskName"},
					"properties": map[string]any{
						"cwd":      cwdProp,
						"specFile": map[string]any{"type": "string", "description": "Spec file name"},
						"taskName": map[string]any{"type": "string", "description": "Task name"},
					},
				},
			},
			executor: func(s *Service, args map[string]any) (string, error) {
				cwd, _ := args["cwd"].(string)
				specFile, _ := args["specFile"].(string)
				taskName, _ := args["taskName"].(string)
				if specFile == "" || taskName == "" {
					return "", fmt.Errorf("specFile and taskName are required")
				}
				attempts, err := s.LoadAttempts(cwd, specFile, taskName)
				if err != nil {
					return "", err
				}
				feedback, _ := s.LoadFeedback(cwd, specFile, taskName)
				return jsonPretty(map[string]any{"attempts": attempts, "feedback": feedback})
			},
		},

		"forge_decisions_list": {
			tool: MCPTool{
				Name:        "forge_decisions_list",
				Description: "List all recorded architectural decisions across the project, grouped by spec.",
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"cwd": cwdProp,
					},
				},
			},
			executor: func(s *Service, args map[string]any) (string, error) {
				cwd, _ := args["cwd"].(string)
				grouped, err := s.ListAllDecisions(cwd)
				if err != nil {
					return "", err
				}
				return jsonPretty(grouped)
			},
		},

		"forge_memory_review": {
			tool: MCPTool{
				Name:        "forge_memory_review",
				Description: "List proposed memory entries from inbox.md pending human review.",
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"cwd": cwdProp,
					},
				},
			},
			executor: func(s *Service, args map[string]any) (string, error) {
				cwd, _ := args["cwd"].(string)
				memories, err := s.ListProposedMemories(cwd)
				if err != nil {
					return "", err
				}
				return jsonPretty(memories)
			},
		},
	}
}

func jsonPretty(v any) (string, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

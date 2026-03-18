package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync/atomic"
	"time"

	"github.com/First008/yap/internal/ipc"
)

// JSONRPCRequest represents an incoming MCP JSON-RPC request.
type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// JSONRPCResponse represents an outgoing MCP JSON-RPC response.
type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
}

type JSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// ToolDef describes an MCP tool for the tools/list response.
type ToolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

// Server implements a minimal MCP stdio server.
type Server struct {
	tools    []ToolDef
	handlers *ToolHandlers
	ready    atomic.Bool
}

func NewServer(projectDir string) (*Server, error) {
	// Retry connecting to the TUI socket — it may not be running yet.
	// This allows Claude Code to spawn the MCP process before the TUI starts.
	var client *ipc.Client
	var err error
	for i := 0; i < 30; i++ {
		client, err = ipc.NewClientFromProject(projectDir)
		if err == nil {
			break
		}
		time.Sleep(time.Second)
	}
	if err != nil {
		return nil, fmt.Errorf("connect to TUI (is ./review running?): %w", err)
	}

	s := &Server{
		handlers: NewToolHandlers(client),
	}
	s.registerTools()
	return s, nil
}

func (s *Server) registerTools() {
	s.tools = []ToolDef{
		{
			Name:        "get_changed_files",
			Description: "Returns a lightweight list of changed files with path, status, line count, and review status. No diffs — diffs are loaded on demand by review_file. Call this first to plan the review order.",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			Name:        "review_file",
			Description: "The main review tool. Shows a file's diff in the TUI, speaks your explanation aloud, then waits for the user to press SPACE and give voice feedback. Returns the transcribed response. Use this for every file — do NOT use separate show_diff/speak/listen calls.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"file_path":   map[string]any{"type": "string", "description": "Path to the file to review"},
					"file_index":  map[string]any{"type": "integer", "description": "Current file index (0-based)"},
					"total_files": map[string]any{"type": "integer", "description": "Total number of files to review"},
					"explanation": map[string]any{"type": "string", "description": "Plain English explanation of the changes. 1-2 sentences max. No markdown, no symbols."},
					"scroll_to":   map[string]any{"type": "integer", "description": "Optional line number to scroll to in the diff. Use this to point the user to the most important change."},
				},
				"required": []string{"file_path", "explanation"},
			},
		},
		{
			Name:        "mark_reviewed",
			Description: "Marks a file as reviewed and stages it with git add. Call after the user approves a file.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"file_path": map[string]any{"type": "string", "description": "Path to mark as reviewed"},
				},
				"required": []string{"file_path"},
			},
		},
		{
			Name:        "speak",
			Description: "Speaks text aloud. Only use for follow-up responses to user questions — NOT for file reviews (use review_file instead).",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"text": map[string]any{"type": "string", "description": "Plain English text to speak. No markdown."},
				},
				"required": []string{"text"},
			},
		},
		{
			Name:        "listen",
			Description: "Listens for user voice input via push-to-talk. Only use for follow-up conversation — NOT for file reviews (use review_file instead).",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			Name:        "get_review_status",
			Description: "Returns review progress: reviewed count, pending count, and file list.",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			Name:        "batch_review",
			Description: "Submit a complete review plan with grouped files. The TUI processes all files locally with zero latency between files. Simple responses (next, looks good, skip) are handled locally. Complex feedback interrupts and returns to Claude. ALWAYS prefer this over individual review_file calls.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"groups": map[string]any{
						"type":        "array",
						"description": "Groups of related files to review together",
						"items": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"name": map[string]any{"type": "string", "description": "Group name, e.g. 'Push-to-talk support'"},
								"files": map[string]any{
									"type": "array",
									"items": map[string]any{
										"type": "object",
										"properties": map[string]any{
											"path":        map[string]any{"type": "string", "description": "File path"},
											"explanation": map[string]any{"type": "string", "description": "1-2 sentence explanation. Plain English, no markdown."},
											"scroll_to":   map[string]any{"type": "integer", "description": "Optional line to scroll to"},
										},
										"required": []string{"path", "explanation"},
									},
								},
							},
							"required": []string{"name", "files"},
						},
					},
				},
				"required": []string{"groups"},
			},
		},
		{
			Name:        "finish_review",
			Description: "Call this when the review session is complete. Exits the TUI and hands control back to the user. Pass a brief summary of the review.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"summary": map[string]any{"type": "string", "description": "Brief summary of the review session. Plain English."},
				},
				"required": []string{"summary"},
			},
		},
	}
}

func (s *Server) Run(ctx context.Context) error {
	reader := bufio.NewReader(os.Stdin)
	writer := os.Stdout

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		line, err := reader.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("read: %w", err)
		}

		var req JSONRPCRequest
		if err := json.Unmarshal(line, &req); err != nil {
			continue
		}

		resp := s.handleRequest(req)
		respBytes, _ := json.Marshal(resp)
		respBytes = append(respBytes, '\n')
		writer.Write(respBytes)
	}
}

func (s *Server) handleRequest(req JSONRPCRequest) JSONRPCResponse {
	switch req.Method {
	case "initialize":
		return s.handleInitialize(req)
	case "initialized":
		s.ready.Store(true)
		return JSONRPCResponse{JSONRPC: "2.0", ID: req.ID}
	case "tools/list":
		return s.handleToolsList(req)
	case "tools/call":
		return s.handleToolsCall(req)
	default:
		return JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &JSONRPCError{Code: -32601, Message: "method not found: " + req.Method},
		}
	}
}

func (s *Server) handleInitialize(req JSONRPCRequest) JSONRPCResponse {
	result, _ := json.Marshal(map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities": map[string]any{
			"tools": map[string]any{},
		},
		"serverInfo": map[string]any{
			"name":    "yap",
			"version": "0.1.0",
		},
	})
	return JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: result}
}

func (s *Server) handleToolsList(req JSONRPCRequest) JSONRPCResponse {
	result, _ := json.Marshal(map[string]any{"tools": s.tools})
	return JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: result}
}

func (s *Server) handleToolsCall(req JSONRPCRequest) JSONRPCResponse {
	var call struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &call); err != nil {
		return JSONRPCResponse{
			JSONRPC: "2.0", ID: req.ID,
			Error: &JSONRPCError{Code: -32602, Message: "invalid params"},
		}
	}

	var result json.RawMessage
	var err error

	switch call.Name {
	case "get_changed_files":
		result, err = s.handlers.GetChangedFiles()
	case "show_diff":
		params, e := ParseParams[ShowDiffParams](call.Arguments)
		if e != nil {
			err = e
		} else {
			result, err = s.handlers.ShowDiff(params)
		}
	case "speak":
		params, e := ParseParams[SpeakParams](call.Arguments)
		if e != nil {
			err = e
		} else {
			result, err = s.handlers.Speak(params)
		}
	case "listen":
		result, err = s.handlers.Listen()
	case "mark_reviewed":
		params, e := ParseParams[MarkReviewedParams](call.Arguments)
		if e != nil {
			err = e
		} else {
			result, err = s.handlers.MarkReviewed(params)
		}
	case "get_review_status":
		result, err = s.handlers.GetReviewStatus()
	case "show_message":
		params, e := ParseParams[ShowMessageParams](call.Arguments)
		if e != nil {
			err = e
		} else {
			result, err = s.handlers.ShowMessage(params)
		}
	case "batch_review":
		params, e := ParseParams[BatchReviewParams](call.Arguments)
		if e != nil {
			err = e
		} else {
			result, err = s.handlers.BatchReview(params)
		}
	case "finish_review":
		params, e := ParseParams[FinishReviewParams](call.Arguments)
		if e != nil {
			err = e
		} else {
			result, err = s.handlers.FinishReview(params)
		}
	case "review_file":
		params, e := ParseParams[ReviewFileParams](call.Arguments)
		if e != nil {
			err = e
		} else {
			result, err = s.handlers.ReviewFile(params)
		}
	default:
		return JSONRPCResponse{
			JSONRPC: "2.0", ID: req.ID,
			Error: &JSONRPCError{Code: -32602, Message: "unknown tool: " + call.Name},
		}
	}

	if err != nil {
		// MCP tool errors are returned as content, not JSON-RPC errors
		errResult, _ := json.Marshal(map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": "Error: " + err.Error()},
			},
			"isError": true,
		})
		return JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: errResult}
	}

	// Wrap result in MCP content format
	text := string(result)
	contentResult, _ := json.Marshal(map[string]any{
		"content": []map[string]any{
			{"type": "text", "text": text},
		},
	})
	return JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: contentResult}
}

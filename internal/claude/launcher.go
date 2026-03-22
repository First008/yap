package claude

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
)

// Session holds the Claude CLI subprocess and its session ID.
type Session struct {
	cmd       *exec.Cmd
	sessionID string
	mu        sync.Mutex
	done      chan struct{}
}

// Launch starts Claude CLI headlessly with the given prompt.
// It reads Claude's stream-json output to capture the session ID.
// mcpConfigPath is an optional path to an MCP config JSON file; if non-empty,
// it is passed via --mcp-config so the headless session has MCP tools.
func Launch(prompt string, mcpConfigPath string) (*Session, error) {
	claudePath, err := exec.LookPath("claude")
	if err != nil {
		return nil, fmt.Errorf("claude CLI not found in PATH: %w", err)
	}

	args := []string{"-p", prompt, "--output-format", "stream-json", "--verbose"}
	if mcpConfigPath != "" {
		args = append(args, "--mcp-config", mcpConfigPath)
	}
	cmd := exec.Command(claudePath, args...)
	cmd.Stderr = os.Stderr // let Claude's errors show

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}

	s := &Session{
		cmd:  cmd,
		done: make(chan struct{}),
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start claude: %w", err)
	}

	// Read stream-json output in background to capture session ID
	go s.readOutput(stdout)

	return s, nil
}

func (s *Session) readOutput(r io.Reader) {
	defer close(s.done)
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024) // 1MB buffer for large outputs

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		// Parse stream-json events to find session ID
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}

		// Look for session_id in the init or result events
		if sid, ok := extractSessionID(event); ok {
			s.mu.Lock()
			s.sessionID = sid
			s.mu.Unlock()
		}
	}
}

func extractSessionID(event map[string]any) (string, bool) {
	// Claude stream-json format has session_id in various event types
	if sid, ok := event["session_id"].(string); ok && sid != "" {
		return sid, true
	}

	// Check nested structures
	if result, ok := event["result"].(map[string]any); ok {
		if sid, ok := result["session_id"].(string); ok && sid != "" {
			return sid, true
		}
	}

	// Check in message events
	if msg, ok := event["message"].(map[string]any); ok {
		if sid, ok := msg["session_id"].(string); ok && sid != "" {
			return sid, true
		}
	}

	return "", false
}

// SessionID returns the captured session ID (may be empty if not yet received).
func (s *Session) SessionID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sessionID
}

// Wait blocks until the Claude process exits.
func (s *Session) Wait() error {
	return s.cmd.Wait()
}

// Stop gracefully terminates the Claude process.
func (s *Session) Stop() {
	if s.cmd.Process != nil {
		s.cmd.Process.Signal(os.Interrupt)
	}
}

// Done returns a channel that's closed when the Claude process exits.
func (s *Session) Done() <-chan struct{} {
	return s.done
}

// ResumeCommand returns the command to resume this Claude session.
func (s *Session) ResumeCommand() string {
	sid := s.SessionID()
	if sid == "" {
		return ""
	}
	return fmt.Sprintf("claude --resume %s", sid)
}

// ExecResume replaces the current process with `claude --resume <session_id>`.
// This hands the terminal over to Claude interactively.
func (s *Session) ExecResume() error {
	sid := s.SessionID()
	if sid == "" {
		return fmt.Errorf("no session ID available")
	}

	claudePath, err := exec.LookPath("claude")
	if err != nil {
		return fmt.Errorf("claude CLI not found: %w", err)
	}

	fmt.Fprintf(os.Stderr, "\nResuming Claude session...\n")
	fmt.Fprintf(os.Stderr, "To resume later: claude --resume %s\n\n", sid)

	// Replace current process with claude --resume
	args := []string{"claude", "--resume", sid}
	env := os.Environ()

	// Filter out TERM-related vars that might conflict
	var cleanEnv []string
	for _, e := range env {
		if !strings.HasPrefix(e, "BUBBLE_") {
			cleanEnv = append(cleanEnv, e)
		}
	}

	return execSyscall(claudePath, args, cleanEnv)
}

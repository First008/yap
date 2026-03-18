package ipc

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
)

type Command struct {
	Name   string          `json:"command"`
	Params json.RawMessage `json:"params"`
}

type Response struct {
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
}

type Handler func(ctx context.Context, params json.RawMessage) (json.RawMessage, error)

type Server struct {
	listener net.Listener
	sockPath string
	handlers map[string]Handler
	mu       sync.RWMutex
}

// NewServer creates an IPC server with a fixed socket path in the project directory.
// Using a fixed path means the MCP process can always find the socket even after
// a TUI restart, without needing to reconnect.
func NewServer(projectDir string) (*Server, error) {
	sockPath := filepath.Join(projectDir, ".yap.sock")

	// Remove stale socket from a previous run
	os.Remove(sockPath)

	listener, err := net.Listen("unix", sockPath)
	if err != nil {
		return nil, fmt.Errorf("listen on %s: %w", sockPath, err)
	}

	return &Server{
		listener: listener,
		sockPath: sockPath,
		handlers: make(map[string]Handler),
	}, nil
}

func (s *Server) SocketPath() string {
	return s.sockPath
}

func (s *Server) Register(name string, handler Handler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers[name] = handler
}

func (s *Server) Serve(ctx context.Context) error {
	go func() {
		<-ctx.Done()
		s.listener.Close()
	}()

	for {
		conn, err := s.listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("accept: %w", err)
		}
		go s.handleConn(ctx, conn)
	}
}

func (s *Server) handleConn(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	decoder := json.NewDecoder(conn)
	encoder := json.NewEncoder(conn)

	for {
		var cmd Command
		if err := decoder.Decode(&cmd); err != nil {
			return
		}

		s.mu.RLock()
		handler, ok := s.handlers[cmd.Name]
		s.mu.RUnlock()

		var resp Response
		if !ok {
			resp.Error = fmt.Sprintf("unknown command: %s", cmd.Name)
		} else {
			result, err := handler(ctx, cmd.Params)
			if err != nil {
				resp.Error = err.Error()
			} else {
				resp.Result = result
			}
		}

		if err := encoder.Encode(resp); err != nil {
			return
		}
	}
}

func (s *Server) Close() error {
	s.listener.Close()
	return os.Remove(s.sockPath)
}

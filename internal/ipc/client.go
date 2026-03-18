package ipc

import (
	"encoding/json"
	"fmt"
	"net"
	"path/filepath"
	"sync"
	"time"
)

type Client struct {
	sockPath string
	conn     net.Conn
	encoder  *json.Encoder
	decoder  *json.Decoder
	mu       sync.Mutex
}

func NewClient(sockPath string) (*Client, error) {
	c := &Client{sockPath: sockPath}
	if err := c.connect(); err != nil {
		return nil, err
	}
	return c, nil
}

// NewClientFromProject connects to the fixed socket path in the project directory.
func NewClientFromProject(projectDir string) (*Client, error) {
	sockPath := filepath.Join(projectDir, ".yap.sock")
	return NewClient(sockPath)
}

func (c *Client) connect() error {
	conn, err := net.Dial("unix", c.sockPath)
	if err != nil {
		return fmt.Errorf("connect to %s: %w", c.sockPath, err)
	}
	c.conn = conn
	c.encoder = json.NewEncoder(conn)
	c.decoder = json.NewDecoder(conn)
	return nil
}

// reconnect closes the old connection and tries to establish a new one,
// retrying for up to 30 seconds to handle TUI restarts.
func (c *Client) reconnect() error {
	if c.conn != nil {
		c.conn.Close()
	}

	for i := 0; i < 30; i++ {
		if err := c.connect(); err == nil {
			return nil
		}
		time.Sleep(time.Second)
	}
	return fmt.Errorf("reconnect to %s failed after 30s", c.sockPath)
}

func (c *Client) Call(command string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	result, err := c.callOnce(command, params)
	if err != nil {
		// Connection might be broken — try reconnecting once
		if reconnErr := c.reconnect(); reconnErr != nil {
			return nil, fmt.Errorf("send command: %w (reconnect failed: %v)", err, reconnErr)
		}
		// Retry the call after reconnecting
		return c.callOnce(command, params)
	}
	return result, nil
}

func (c *Client) callOnce(command string, params any) (json.RawMessage, error) {
	paramsJSON, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("marshal params: %w", err)
	}

	cmd := Command{
		Name:   command,
		Params: paramsJSON,
	}

	if err := c.encoder.Encode(cmd); err != nil {
		return nil, err
	}

	var resp Response
	if err := c.decoder.Decode(&resp); err != nil {
		return nil, err
	}

	if resp.Error != "" {
		return nil, fmt.Errorf("server error: %s", resp.Error)
	}

	return resp.Result, nil
}

func (c *Client) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

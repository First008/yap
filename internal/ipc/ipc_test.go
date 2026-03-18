package ipc

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestServerClientRoundTrip(t *testing.T) {
	server, err := NewServer(t.TempDir())
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer server.Close()

	server.Register("echo", func(ctx context.Context, params json.RawMessage) (json.RawMessage, error) {
		return params, nil
	})

	server.Register("greet", func(ctx context.Context, params json.RawMessage) (json.RawMessage, error) {
		var p struct {
			Name string `json:"name"`
		}
		json.Unmarshal(params, &p)
		result, _ := json.Marshal(map[string]string{"greeting": "hello " + p.Name})
		return result, nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go server.Serve(ctx)

	// Give server a moment to start
	time.Sleep(10 * time.Millisecond)

	client, err := NewClient(server.SocketPath())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer client.Close()

	// Test echo
	input, _ := json.Marshal(map[string]string{"msg": "test"})
	result, err := client.Call("echo", json.RawMessage(input))
	if err != nil {
		t.Fatalf("echo call: %v", err)
	}
	if string(result) != string(input) {
		t.Fatalf("echo: expected %s, got %s", input, result)
	}

	// Test greet
	result, err = client.Call("greet", map[string]string{"name": "world"})
	if err != nil {
		t.Fatalf("greet call: %v", err)
	}
	var greeting struct {
		Greeting string `json:"greeting"`
	}
	json.Unmarshal(result, &greeting)
	if greeting.Greeting != "hello world" {
		t.Fatalf("greet: expected 'hello world', got %q", greeting.Greeting)
	}

	// Test unknown command
	_, err = client.Call("nonexistent", nil)
	if err == nil {
		t.Fatal("expected error for unknown command")
	}
}

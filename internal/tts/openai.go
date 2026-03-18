package tts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"time"
)

type OpenAIAdapter struct {
	apiKey string
	voice  string // alloy, echo, fable, onyx, nova, shimmer
	model  string // tts-1, tts-1-hd
	speed  float64

	mu  sync.Mutex
	cmd *exec.Cmd
}

func NewOpenAIAdapter(apiKey, voice, model string, speed float64) *OpenAIAdapter {
	if apiKey == "" {
		apiKey = os.Getenv("OPENAI_API_KEY")
	}
	if voice == "" {
		voice = "nova"
	}
	if model == "" {
		model = "tts-1"
	}
	if speed == 0 {
		speed = 1.0
	}
	return &OpenAIAdapter{apiKey: apiKey, voice: voice, model: model, speed: speed}
}

func (o *OpenAIAdapter) Speak(ctx context.Context, text string) error {
	if o.apiKey == "" {
		return fmt.Errorf("OPENAI_API_KEY not set")
	}

	// Request audio from OpenAI TTS API
	body, _ := json.Marshal(map[string]any{
		"model": o.model,
		"input": text,
		"voice": o.voice,
		"speed": o.speed,
	})

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.openai.com/v1/audio/speech", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+o.apiKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("openai tts: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		errBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("openai tts: %s: %s", resp.Status, string(errBody))
	}

	// Write to temp file and play with afplay (macOS)
	tmpFile, err := os.CreateTemp("", "review-tts-*.mp3")
	if err != nil {
		return err
	}
	defer os.Remove(tmpFile.Name())

	if _, err := io.Copy(tmpFile, resp.Body); err != nil {
		tmpFile.Close()
		return err
	}
	tmpFile.Close()

	// Play the audio
	o.mu.Lock()
	o.cmd = exec.CommandContext(ctx, "afplay", tmpFile.Name())
	cmd := o.cmd
	o.mu.Unlock()

	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("afplay: %w", err)
	}

	o.mu.Lock()
	o.cmd = nil
	o.mu.Unlock()

	return nil
}

func (o *OpenAIAdapter) Stop() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.cmd != nil && o.cmd.Process != nil {
		return o.cmd.Process.Kill()
	}
	return nil
}

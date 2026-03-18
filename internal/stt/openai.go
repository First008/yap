package stt

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

type OpenAIAdapter struct {
	apiKey string
	model  string // "whisper-1"

	mu  sync.Mutex
	cmd *exec.Cmd
}

func NewOpenAIAdapter(apiKey string) *OpenAIAdapter {
	if apiKey == "" {
		apiKey = os.Getenv("OPENAI_API_KEY")
	}
	return &OpenAIAdapter{apiKey: apiKey, model: "whisper-1"}
}

func (o *OpenAIAdapter) Listen(ctx context.Context) (string, error) {
	tmpDir, err := os.MkdirTemp("", "review-stt-*")
	if err != nil {
		return "", fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	wavFile := filepath.Join(tmpDir, "recording.wav")
	if err := o.recordSilence(ctx, wavFile); err != nil {
		return "", fmt.Errorf("record: %w", err)
	}

	return o.transcribe(ctx, wavFile)
}

func (o *OpenAIAdapter) ListenPTT(ctx context.Context, stopCh <-chan struct{}) (string, error) {
	tmpDir, err := os.MkdirTemp("", "review-stt-*")
	if err != nil {
		return "", fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	wavFile := filepath.Join(tmpDir, "recording.wav")
	if err := o.recordUntilStop(ctx, wavFile, stopCh); err != nil {
		return "", fmt.Errorf("record: %w", err)
	}

	return o.transcribe(ctx, wavFile)
}

func (o *OpenAIAdapter) recordSilence(ctx context.Context, outputPath string) error {
	args := []string{
		"-d", "-t", "wav", "-r", "16000", "-c", "1", "-b", "16",
		outputPath,
		"silence", "1", "0.1", "1%", "1", "3.0", "1%",
	}
	o.mu.Lock()
	o.cmd = exec.CommandContext(ctx, "sox", args...)
	cmd := o.cmd
	o.mu.Unlock()

	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("sox: %w", err)
	}
	o.mu.Lock()
	o.cmd = nil
	o.mu.Unlock()
	return nil
}

func (o *OpenAIAdapter) recordUntilStop(ctx context.Context, outputPath string, stopCh <-chan struct{}) error {
	time.Sleep(150 * time.Millisecond) // Bluetooth mic delay

	args := []string{
		"-d", "-t", "wav", "-r", "16000", "-c", "1", "-b", "16",
		outputPath,
	}
	o.mu.Lock()
	o.cmd = exec.CommandContext(ctx, "sox", args...)
	cmd := o.cmd
	o.mu.Unlock()

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("sox start: %w", err)
	}

	select {
	case <-stopCh:
		if cmd.Process != nil {
			cmd.Process.Signal(os.Interrupt)
		}
	case <-ctx.Done():
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
		return nil
	}

	cmd.Wait()
	o.mu.Lock()
	o.cmd = nil
	o.mu.Unlock()
	return nil
}

func (o *OpenAIAdapter) transcribe(ctx context.Context, wavPath string) (string, error) {
	if o.apiKey == "" {
		return "", fmt.Errorf("OPENAI_API_KEY not set")
	}

	// Validate audio before sending to API (avoid burning credits on silence)
	if err := validateAudio(wavPath); err != nil {
		return "", nil
	}

	// Build multipart form
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	// Add the audio file
	file, err := os.Open(wavPath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	part, err := writer.CreateFormFile("file", "recording.wav")
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(part, file); err != nil {
		return "", err
	}

	if err := writer.WriteField("model", o.model); err != nil {
		return "", fmt.Errorf("write model field: %w", err)
	}
	if err := writer.WriteField("language", "en"); err != nil {
		return "", fmt.Errorf("write language field: %w", err)
	}
	if err := writer.WriteField("prompt", "Code review feedback. Short commands like: next, continue, looks good, fix that, skip, stop."); err != nil {
		return "", fmt.Errorf("write prompt field: %w", err)
	}
	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("close multipart writer: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.openai.com/v1/audio/transcriptions", &buf)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+o.apiKey)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return "", nil
		}
		return "", fmt.Errorf("openai whisper: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("openai whisper: %s: %s", resp.Status, string(body))
	}

	var result struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}

	// Run shared validation pipeline (hallucination, repetition, word rate)
	dur, _ := getAudioDuration(wavPath)
	text, err := validateTranscription(result.Text, dur)
	if err != nil {
		return "", err
	}

	return filterHallucination(text), nil
}

func (o *OpenAIAdapter) Stop() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.cmd != nil && o.cmd.Process != nil {
		return o.cmd.Process.Kill()
	}
	return nil
}

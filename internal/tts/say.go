package tts

import (
	"context"
	"fmt"
	"os/exec"
	"sync"
)

type SayAdapter struct {
	voice string
	rate  int

	mu  sync.Mutex
	cmd *exec.Cmd
}

func NewSayAdapter(voice string, rate int) *SayAdapter {
	if voice == "" {
		voice = "Daniel"
	}
	if rate == 0 {
		rate = 195
	}
	return &SayAdapter{voice: voice, rate: rate}
}

func (s *SayAdapter) Speak(ctx context.Context, text string) error {
	s.mu.Lock()

	args := []string{"-v", s.voice, "-r", fmt.Sprintf("%d", s.rate), text}
	s.cmd = exec.CommandContext(ctx, "say", args...)
	cmd := s.cmd

	s.mu.Unlock()

	if err := cmd.Run(); err != nil {
		// Context cancellation is not an error from caller's perspective
		if ctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("say: %w", err)
	}

	s.mu.Lock()
	s.cmd = nil
	s.mu.Unlock()

	return nil
}

func (s *SayAdapter) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cmd != nil && s.cmd.Process != nil {
		return s.cmd.Process.Kill()
	}
	return nil
}

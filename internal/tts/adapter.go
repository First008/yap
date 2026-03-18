package tts

import "context"

type TTSAdapter interface {
	Speak(ctx context.Context, text string) error
	Stop() error
}

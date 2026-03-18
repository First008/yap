package stt

import "context"

type STTAdapter interface {
	Listen(ctx context.Context) (string, error)
	ListenPTT(ctx context.Context, stopCh <-chan struct{}) (string, error)
	Stop() error
}

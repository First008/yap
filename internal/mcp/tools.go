package mcp

import (
	"encoding/json"
	"fmt"

	"github.com/First008/yap/internal/ipc"
)

type ToolHandlers struct {
	client *ipc.Client
}

func NewToolHandlers(client *ipc.Client) *ToolHandlers {
	return &ToolHandlers{client: client}
}

func (h *ToolHandlers) GetChangedFiles() (json.RawMessage, error) {
	return h.client.Call("get_changed_files", nil)
}

func (h *ToolHandlers) ShowDiff(params ShowDiffParams) (json.RawMessage, error) {
	return h.client.Call("show_diff", params)
}

func (h *ToolHandlers) Speak(params SpeakParams) (json.RawMessage, error) {
	return h.client.Call("speak", params)
}

func (h *ToolHandlers) Listen() (json.RawMessage, error) {
	return h.client.Call("listen", nil)
}

func (h *ToolHandlers) MarkReviewed(params MarkReviewedParams) (json.RawMessage, error) {
	return h.client.Call("mark_reviewed", params)
}

func (h *ToolHandlers) GetReviewStatus() (json.RawMessage, error) {
	return h.client.Call("get_review_status", nil)
}

func (h *ToolHandlers) ShowMessage(params ShowMessageParams) (json.RawMessage, error) {
	if params.Source == "" {
		params.Source = "system"
	}
	return h.client.Call("show_message", params)
}

func (h *ToolHandlers) BatchReview(params BatchReviewParams) (json.RawMessage, error) {
	return h.client.Call("batch_review", params)
}

func (h *ToolHandlers) FinishReview(params FinishReviewParams) (json.RawMessage, error) {
	return h.client.Call("finish_review", params)
}

func (h *ToolHandlers) ReviewFile(params ReviewFileParams) (json.RawMessage, error) {
	return h.client.Call("review_file", params)
}

// ParseParams is a helper to unmarshal tool arguments.
func ParseParams[T any](raw json.RawMessage) (T, error) {
	var params T
	if err := json.Unmarshal(raw, &params); err != nil {
		return params, fmt.Errorf("parse params: %w", err)
	}
	return params, nil
}

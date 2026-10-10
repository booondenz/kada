package assistant

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cloudwego/eino-ext/components/model/deepseek"
	"github.com/cloudwego/eino/components/model"
)

// chatTemperature is the Python service's 0.7. How the assistant sounds is a product decision rather than
// a detail of the port, so it moved across unchanged.
const chatTemperature float32 = 0.7

// chatTimeout is generous because an answer arrives token by token: a short deadline would cut a long one
// off mid-sentence. The Python client's own default was ten minutes, and this matches it.
const chatTimeout = 10 * time.Minute

// ChatModelConfig is what the assistant's model needs to be built.
type ChatModelConfig struct {
	APIKey    string
	BaseURL   string
	Model     string
	MaxTokens int
}

// NewChatModel builds the DeepSeek chat model through Eino.
//
// An empty key is an error rather than a client that fails on the first question. The caller decides what
// to do about it - cmd/server logs it and serves the assistant anyway, so the AI page reports a
// configuration problem instead of the API refusing to start.
func NewChatModel(ctx context.Context, cfg ChatModelConfig) (model.ToolCallingChatModel, error) {
	if cfg.APIKey == "" {
		return nil, errors.New("the chat model API key is empty")
	}

	chatModel, err := deepseek.NewChatModel(ctx, &deepseek.ChatModelConfig{
		APIKey:      cfg.APIKey,
		BaseURL:     cfg.BaseURL,
		Model:       cfg.Model,
		MaxTokens:   cfg.MaxTokens,
		Temperature: chatTemperature,
		Timeout:     chatTimeout,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to build the chat model: %w", err)
	}
	return chatModel, nil
}

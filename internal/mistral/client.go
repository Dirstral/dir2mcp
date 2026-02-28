package mistral

import (
	"context"

	"github.com/Dirstral/dir2mcp/internal/model"
)

type Client struct {
	BaseURL string
	APIKey  string
}

func NewClient(baseURL, apiKey string) *Client {
	return &Client{
		BaseURL: baseURL,
		APIKey:  apiKey,
	}
}

func (c *Client) Embed(ctx context.Context, modelName string, inputs []string) ([][]float32, error) {
	_ = ctx
	_ = modelName
	_ = inputs
	return nil, model.ErrNotImplemented
}

func (c *Client) Extract(ctx context.Context, relPath string, data []byte) (string, error) {
	_ = ctx
	_ = relPath
	_ = data
	return "", model.ErrNotImplemented
}

func (c *Client) Transcribe(ctx context.Context, relPath string, data []byte) (string, error) {
	_ = ctx
	_ = relPath
	_ = data
	return "", model.ErrNotImplemented
}

func (c *Client) Generate(ctx context.Context, prompt string) (string, error) {
	_ = ctx
	_ = prompt
	return "", model.ErrNotImplemented
}

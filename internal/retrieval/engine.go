package retrieval

import (
	"fmt"

	"github.com/Dirstral/dir2mcp/internal/config"
)

// Engine for RAG search/ask (stub until Ark implements).
type Engine struct{}

// NewEngine creates a retrieval engine (stub).
func NewEngine(stateDir, rootDir string, cfg *config.Config) (*Engine, error) {
	return &Engine{}, nil
}

// Close releases resources.
func (e *Engine) Close() {}

// AskOptions for Ask.
type AskOptions struct {
	K int
}

// AskResult is the result of Ask.
type AskResult struct {
	Answer    string
	Citations []Citation
}

// Citation references a source span.
type Citation struct {
	RelPath string
	Span    interface{}
}

// Ask runs RAG (stub: returns not implemented).
func (e *Engine) Ask(question string, opts AskOptions) (*AskResult, error) {
	return nil, fmt.Errorf("retrieval not implemented yet (Day 1 afternoon)")
}

package retrieval

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"dir2mcp/internal/config"
	"dir2mcp/internal/index"
	"dir2mcp/internal/mistral"
	"dir2mcp/internal/model"
	"dir2mcp/internal/store"
)

type engineRetriever interface {
	Ask(ctx context.Context, question string, query model.SearchQuery) (model.AskResult, error)
}

type embeddedChunkMetadataSource interface {
	ListEmbeddedChunkMetadata(ctx context.Context, indexKind string, limit, offset int) ([]model.ChunkTask, error)
}

const defaultEngineAskTimeout = 120 * time.Second

// Engine provides a convenience wrapper around retrieval.Service for callers
// that still rely on the legacy Engine API.
type Engine struct {
	retriever  engineRetriever
	closeFns   []func()
	closeOnce  sync.Once
	askTimeout time.Duration
}

// NewEngine creates a retrieval engine backed by the on-disk state.
func NewEngine(stateDir, rootDir string, cfg *config.Config) (*Engine, error) {
	effective := config.Default()
	if cfg != nil {
		effective = *cfg
	}
	if trimmed := strings.TrimSpace(stateDir); trimmed != "" {
		effective.StateDir = trimmed
	}
	if trimmed := strings.TrimSpace(rootDir); trimmed != "" {
		effective.RootDir = trimmed
	}
	if strings.TrimSpace(effective.StateDir) == "" {
		effective.StateDir = filepath.Join(".", ".dir2mcp")
	}
	if strings.TrimSpace(effective.RootDir) == "" {
		effective.RootDir = "."
	}

	metadataStore := store.NewSQLiteStore(filepath.Join(effective.StateDir, "meta.sqlite"))
	if err := metadataStore.Init(context.Background()); err != nil && !errors.Is(err, model.ErrNotImplemented) {
		_ = metadataStore.Close()
		return nil, fmt.Errorf("initialize metadata store: %w", err)
	}

	textIndexPath := filepath.Join(effective.StateDir, "vectors_text.hnsw")
	textIndex := index.NewHNSWIndex(textIndexPath)
	if err := textIndex.Load(textIndexPath); err != nil && !errors.Is(err, model.ErrNotImplemented) && !errors.Is(err, os.ErrNotExist) {
		_ = metadataStore.Close()
		_ = textIndex.Close()
		return nil, fmt.Errorf("load text index: %w", err)
	}

	codeIndexPath := filepath.Join(effective.StateDir, "vectors_code.hnsw")
	codeIndex := index.NewHNSWIndex(codeIndexPath)
	if err := codeIndex.Load(codeIndexPath); err != nil && !errors.Is(err, model.ErrNotImplemented) && !errors.Is(err, os.ErrNotExist) {
		_ = metadataStore.Close()
		_ = textIndex.Close()
		_ = codeIndex.Close()
		return nil, fmt.Errorf("load code index: %w", err)
	}

	client := mistral.NewClient(effective.MistralBaseURL, effective.MistralAPIKey)
	if strings.TrimSpace(effective.ChatModel) != "" {
		client.DefaultChatModel = strings.TrimSpace(effective.ChatModel)
	}

	svc := NewService(metadataStore, textIndex, client, client)
	svc.SetCodeIndex(codeIndex)
	svc.SetRootDir(effective.RootDir)
	svc.SetStateDir(effective.StateDir)
	svc.SetProtocolVersion(effective.ProtocolVersion)

	if source, ok := interface{}(metadataStore).(embeddedChunkMetadataSource); ok {
		if _, err := preloadEngineChunkMetadata(context.Background(), source, svc); err != nil {
			_ = metadataStore.Close()
			_ = textIndex.Close()
			_ = codeIndex.Close()
			return nil, fmt.Errorf("preload chunk metadata: %w", err)
		}
	}

	return &Engine{
		retriever: svc,
		closeFns: []func(){
			func() { _ = metadataStore.Close() },
			func() { _ = textIndex.Close() },
			func() { _ = codeIndex.Close() },
		},
		askTimeout: defaultEngineAskTimeout,
	}, nil
}

// Close releases resources.
func (e *Engine) Close() {
	if e == nil {
		return
	}
	e.closeOnce.Do(func() {
		for _, closeFn := range e.closeFns {
			if closeFn != nil {
				closeFn()
			}
		}
	})
}

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

// Ask runs retrieval + generation and returns answer/citations.
func (e *Engine) Ask(question string, opts AskOptions) (*AskResult, error) {
	if e == nil || e.retriever == nil {
		return nil, fmt.Errorf("retrieval engine not initialized")
	}
	question = strings.TrimSpace(question)
	if question == "" {
		return nil, fmt.Errorf("question is required")
	}

	k := opts.K
	if k <= 0 {
		k = 10
	}
	timeout := e.askTimeout
	if timeout <= 0 {
		timeout = defaultEngineAskTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	res, err := e.retriever.Ask(ctx, question, model.SearchQuery{
		Query: question,
		K:     k,
	})
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, fmt.Errorf("ask timed out after %s: %w", timeout, context.DeadlineExceeded)
		}
		return nil, err
	}

	citations := make([]Citation, 0, len(res.Citations))
	for _, citation := range res.Citations {
		citations = append(citations, Citation{
			RelPath: citation.RelPath,
			Span:    citation.Span,
		})
	}

	return &AskResult{
		Answer:    res.Answer,
		Citations: citations,
	}, nil
}

func preloadEngineChunkMetadata(ctx context.Context, source embeddedChunkMetadataSource, ret *Service) (int, error) {
	if source == nil || ret == nil {
		return 0, nil
	}
	const pageSize = 500
	total := 0
	for _, kind := range []string{"text", "code"} {
		offset := 0
		for {
			tasks, err := source.ListEmbeddedChunkMetadata(ctx, kind, pageSize, offset)
			if err != nil {
				if errors.Is(err, model.ErrNotImplemented) {
					break
				}
				return total, err
			}
			for _, task := range tasks {
				ret.SetChunkMetadataForIndex(kind, task.Metadata.ChunkID, model.SearchHit{
					ChunkID: task.Metadata.ChunkID,
					RelPath: task.Metadata.RelPath,
					DocType: task.Metadata.DocType,
					RepType: task.Metadata.RepType,
					Snippet: task.Metadata.Snippet,
					Span:    task.Metadata.Span,
				})
				total++
			}
			if len(tasks) < pageSize {
				break
			}
			offset += len(tasks)
		}
	}
	return total, nil
}

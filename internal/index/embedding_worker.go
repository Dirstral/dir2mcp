package index

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/Dirstral/dir2mcp/internal/model"
)

type ChunkSource interface {
	NextPending(ctx context.Context, limit int, indexKind string) ([]model.ChunkTask, error)
	MarkEmbedded(ctx context.Context, labels []uint64) error
	MarkFailed(ctx context.Context, labels []uint64, reason string) error
}

type EmbeddingWorker struct {
	Source         ChunkSource
	Index          model.Index
	Embedder       model.Embedder
	ModelForText   string
	ModelForCode   string
	BatchSize      int
	OnIndexedChunk func(label uint64, metadata model.SearchHit)
}

func (w *EmbeddingWorker) RunOnce(ctx context.Context, indexKind string) (int, error) {
	if w.Source == nil || w.Index == nil || w.Embedder == nil {
		return 0, errors.New("source, index, and embedder are required")
	}

	batchSize := w.BatchSize
	if batchSize <= 0 {
		batchSize = 32
	}

	tasks, err := w.Source.NextPending(ctx, batchSize, indexKind)
	if err != nil {
		return 0, err
	}
	if len(tasks) == 0 {
		return 0, nil
	}

	modelName := w.modelForKind(indexKind)
	inputs := make([]string, len(tasks))
	labels := make([]uint64, len(tasks))
	for idx, task := range tasks {
		inputs[idx] = task.Text
		labels[idx] = task.Label
	}

	vectors, err := w.Embedder.Embed(ctx, modelName, inputs)
	if err != nil {
		_ = w.Source.MarkFailed(ctx, labels, err.Error())
		return 0, err
	}
	if len(vectors) != len(tasks) {
		_ = w.Source.MarkFailed(ctx, labels, "embedding vector count mismatch")
		return 0, errors.New("embedding vector count mismatch")
	}

	for idx := range tasks {
		if addErr := w.Index.Add(tasks[idx].Label, vectors[idx]); addErr != nil {
			_ = w.Source.MarkFailed(ctx, labels[idx:idx+1], addErr.Error())
			return idx, addErr
		}
		if w.OnIndexedChunk != nil {
			w.OnIndexedChunk(tasks[idx].Label, tasks[idx].Metadata)
		}
	}

	if err := w.Source.MarkEmbedded(ctx, labels); err != nil {
		return len(labels), err
	}

	return len(labels), nil
}

func (w *EmbeddingWorker) Run(ctx context.Context, interval time.Duration, indexKind string) error {
	if interval <= 0 {
		interval = 2 * time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			_, err := w.RunOnce(ctx, indexKind)
			if err != nil {
				return err
			}
		}
	}
}

func (w *EmbeddingWorker) modelForKind(indexKind string) string {
	kind := strings.ToLower(strings.TrimSpace(indexKind))
	switch kind {
	case "code":
		if strings.TrimSpace(w.ModelForCode) != "" {
			return w.ModelForCode
		}
		return "codestral-embed"
	default:
		if strings.TrimSpace(w.ModelForText) != "" {
			return w.ModelForText
		}
		return "mistral-embed"
	}
}

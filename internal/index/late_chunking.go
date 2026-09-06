package index

import (
	"context"
	"fmt"

	"github.com/dirstral/dir2mcp/internal/latechunk"
	"github.com/dirstral/dir2mcp/internal/model"
	"github.com/dirstral/dir2mcp/internal/store"
)

// lateChunkBatch is the late-chunking state of ONE embed batch (SPEC 8.1.9,
// dir2mcp #565): the resolved capability decision and the document text of every
// representation the batch's text chunks belong to, fetched once per batch so a
// representation with forty pending chunks is read once, not forty times. It is
// nil for a batch that does not pool (mode off, or the embedder lacks token
// embeddings), and the embed path then runs byte-for-byte as before.
type lateChunkBatch struct {
	dec  latechunk.Decision
	docs map[int64]string
}

// lateChunkUnpoolableReason is the terminal failure reason for a text chunk the
// pooling path cannot place: it has no persisted rune span, or its representation
// has no persisted document text. Both mean the row predates the columns SPEC
// §5.3 / §5.2 gained for this mode. Embedding such a chunk chunk-then-embed would
// put an unpooled vector into a pooled corpus under an identity that says
// otherwise (SPEC 8.1.9 "Pre-feature rows"), so it fails with the remediation.
const lateChunkUnpoolableReason = "late chunking is on and the embedder exposes token embeddings, but this chunk has no persisted rune span or representation text (indexed before they were recorded); run `dir2mcp reindex` to re-derive it (SPEC 8.1.9)"

// prepareLateChunkBatch resolves the late-chunking inputs for one batch. With
// the pooling path inactive it returns the tasks unchanged and a nil batch. With
// it active it loads each text chunk's document text through the source's
// model.RepresentationTextReader, marks every text chunk it cannot place
// (lateChunkUnpoolableReason) terminally failed and drops it, and returns the
// remaining tasks with the loaded texts. Media chunks pass through untouched:
// they embed from bytes and are outside the mode (SPEC 8.1.7 / 8.1.9).
//
// A source that cannot supply representation text while the mode is active is
// ErrFatal: the worker must stop rather than run a corpus whose identity says
// "pooled" through a path that cannot pool.
func (w *EmbeddingWorker) prepareLateChunkBatch(ctx context.Context, tasks []model.ChunkTask, labels []uint64) ([]model.ChunkTask, []uint64, *lateChunkBatch, error) {
	dec := w.LateChunkDecision()
	if !dec.Active {
		return tasks, labels, nil, nil
	}
	reader, ok := w.Source.(model.RepresentationTextReader)
	if !ok {
		return nil, nil, nil, fmt.Errorf("%w: late chunking is active but the chunk source %T cannot supply representation text (SPEC 8.1.9)", ErrFatal, w.Source)
	}
	lc := &lateChunkBatch{dec: dec, docs: make(map[int64]string)}
	kept := make([]model.ChunkTask, 0, len(tasks))
	keptLabels := make([]uint64, 0, len(labels))
	var unpoolable []uint64
	for i, t := range tasks {
		if isMediaModality(t.Modality) {
			kept = append(kept, t)
			keptLabels = append(keptLabels, labels[i])
			continue
		}
		poolable, err := lc.admit(ctx, reader, t)
		if err != nil {
			return nil, nil, nil, err
		}
		if !poolable {
			unpoolable = append(unpoolable, labels[i])
			continue
		}
		kept = append(kept, t)
		keptLabels = append(keptLabels, labels[i])
	}
	if len(unpoolable) > 0 {
		w.logf("late chunking: %d chunk(s) have no rune span or representation text and cannot be pooled; marking failed with a reindex remediation labels=%v", len(unpoolable), unpoolable)
		if mfErr := w.Source.MarkFailedWithCategory(ctx, unpoolable, string(store.ErrorCategoryEmbeddingFailure), lateChunkUnpoolableReason); mfErr != nil {
			w.logf("mark unpoolable failed update error: %v labels=%v", mfErr, unpoolable)
		}
	}
	return kept, keptLabels, lc, nil
}

// admit reports whether a text task can be pooled: it carries a rune span and
// its representation's document text is persisted (loaded here, once per
// representation per batch). A store read error propagates unchanged so a
// transient database hiccup leaves the batch pending rather than failing chunks.
func (lc *lateChunkBatch) admit(ctx context.Context, reader model.RepresentationTextReader, t model.ChunkTask) (bool, error) {
	if !t.HasRuneSpan() || t.RepID <= 0 {
		return false, nil
	}
	if _, loaded := lc.docs[t.RepID]; loaded {
		return true, nil
	}
	text, ok, err := reader.RepresentationText(ctx, t.RepID)
	if err != nil {
		return false, fmt.Errorf("load representation text for rep %d: %w", t.RepID, err)
	}
	if !ok {
		return false, nil
	}
	lc.docs[t.RepID] = text
	return true, nil
}

// poolLateChunkGroups runs the pooling path (latechunk.EmbedDocument) for the
// text tasks at textIdx, grouped by representation in first-appearance order,
// writing each pooled vector into vectors at the task's index. It returns the
// indices that must still be embedded chunk-then-embed: a span no token overlaps
// (per chunk, SPEC 8.1.9 "Pooling"), or every chunk of a document whose token
// embedding failed NON-transiently (per document, "Failure classification").
//
// A transient failure (network, 429, 5xx, cancellation) is returned as is, so
// the caller leaves the chunks pending: degrading it into chunk-then-embed
// vectors would put unpooled vectors into a pooled corpus without a trace. Any
// other error (a structurally invalid token embedding) is returned too; the
// batch machinery then bisects and marks the offending chunks failed.
func (w *EmbeddingWorker) poolLateChunkGroups(ctx context.Context, modelName string, lc *lateChunkBatch, validTasks []model.ChunkTask, textIdx []int, vectors [][]float32) ([]int, error) {
	groups, order := groupByRep(validTasks, textIdx)
	var plainIdx []int
	for _, repID := range order {
		idxs := groups[repID]
		spans := make([]latechunk.Span, len(idxs))
		for k, idx := range idxs {
			spans[k] = latechunk.Span{Start: validTasks[idx].RuneStart, End: validTasks[idx].RuneEnd}
		}
		pooled, fallbackIdx, err := latechunk.EmbedDocument(ctx, lc.dec, modelName, lc.docs[repID], spans)
		if err != nil {
			if isTransientEmbedError(err) {
				return nil, err
			}
			if latechunk.IsEmbedFallback(err) {
				w.logf("late chunking: token embedding of rep %d failed non-transiently (%s); embedding its %d chunk(s) chunk-then-embed", repID, store.SanitizeReason(err.Error()), len(idxs))
				plainIdx = append(plainIdx, idxs...)
				continue
			}
			return nil, err
		}
		for k, idx := range idxs {
			if pooled[k] != nil {
				vectors[idx] = pooled[k]
			}
		}
		if len(fallbackIdx) > 0 {
			w.logf("late chunking: %d chunk span(s) of rep %d overlap no token; embedding those chunk-then-embed", len(fallbackIdx), repID)
			for _, k := range fallbackIdx {
				plainIdx = append(plainIdx, idxs[k])
			}
		}
	}
	return plainIdx, nil
}

// groupByRep partitions the task indices in textIdx by representation, keeping
// first-appearance order so the pooling path is deterministic for a given batch.
func groupByRep(validTasks []model.ChunkTask, textIdx []int) (map[int64][]int, []int64) {
	groups := make(map[int64][]int)
	var order []int64
	for _, idx := range textIdx {
		repID := validTasks[idx].RepID
		if _, seen := groups[repID]; !seen {
			order = append(order, repID)
		}
		groups[repID] = append(groups[repID], idx)
	}
	return groups, order
}

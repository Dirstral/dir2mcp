package index

import (
	"context"
	"fmt"

	"github.com/dirstral/dir2mcp/internal/latechunk"
	"github.com/dirstral/dir2mcp/internal/model"
	"github.com/dirstral/dir2mcp/internal/store"
)

// lateChunkBatch is the late-chunking state of ONE embed batch (SPEC 8.1.9,
// dir2mcp #565). prepareLateChunkBatch fills it: the resolved capability
// decision, the document text of every representation the batch's text chunks
// belong to (read once per representation), and the POOLED vector of every chunk
// whose representation token-embedded successfully. The token embedding runs
// there, before any vector is indexed, so a document's chunks are pooled from one
// operation or not at all. It is nil for a batch that does not pool (mode off, or
// the embedder lacks token embeddings), and the embed path then runs
// byte-for-byte as before.
type lateChunkBatch struct {
	dec  latechunk.Decision
	docs map[int64]string
	// pooled holds the L2-normalized pooled vector per chunk id. A text chunk
	// absent from it after preparation is one whose span no token overlapped
	// (SPEC 8.1.9 "Pooling"): it is embedded chunk-then-embed on its own.
	pooled map[uint64][]float32
}

// lateChunkUnpoolableReason is the terminal failure reason for a text chunk the
// pooling path cannot place: it has no persisted rune span, or its representation
// has no persisted document text. Both mean the row predates the columns SPEC
// §5.3 / §5.2 gained for this mode. Embedding such a chunk chunk-then-embed would
// put an unpooled vector into a pooled corpus under an identity that says
// otherwise (SPEC 8.1.9 "Pre-feature rows"), so it fails with the remediation.
const lateChunkUnpoolableReason = "late chunking is on and the embedder exposes token embeddings, but this chunk has no persisted rune span or representation text (indexed before they were recorded); run `dir2mcp reindex` to re-derive it (SPEC 8.1.9)"

// prepareLateChunkBatch resolves the late-chunking inputs for one batch and runs
// the token embedding of every document in it. With the pooling path inactive it
// returns the tasks unchanged and a nil batch. With it active it:
//
//   - loads each text chunk's document text through the source's
//     model.RepresentationTextReader and marks every text chunk it cannot place
//     (lateChunkUnpoolableReason) terminally failed, dropping it;
//   - token-embeds each representation ONCE (latechunk.EmbedDocument) and pools
//     every chunk of it. A TRANSIENT failure (network, 429, 5xx, cancellation) is
//     returned as is, so the caller leaves the whole batch pending; degrading it
//     into chunk-then-embed vectors would put unpooled vectors into a pooled
//     corpus without a trace. A NON-transient failure is a terminal failure of
//     every chunk of that document (SPEC 8.1.9 "Failure classification"):
//     recorded with its category and a reason naming the remediation, dropped
//     from the batch, never embedded chunk-then-embed.
//
// Media chunks pass through untouched: they embed from bytes and are outside the
// mode (SPEC 8.1.7 / 8.1.9). A source that cannot supply representation text
// while the mode is active is ErrFatal: the worker must stop rather than run a
// corpus whose identity says "pooled" through a path that cannot pool.
func (w *EmbeddingWorker) prepareLateChunkBatch(ctx context.Context, modelName string, tasks []model.ChunkTask, labels []uint64) ([]model.ChunkTask, []uint64, *lateChunkBatch, error) {
	dec := w.LateChunkDecision()
	if !dec.Active {
		return tasks, labels, nil, nil
	}
	reader, ok := w.Source.(model.RepresentationTextReader)
	if !ok {
		return nil, nil, nil, fmt.Errorf("%w: late chunking is active but the chunk source %T cannot supply representation text (SPEC 8.1.9)", ErrFatal, w.Source)
	}
	lc := &lateChunkBatch{dec: dec, docs: make(map[int64]string), pooled: make(map[uint64][]float32)}
	kept, keptLabels, err := w.admitLateChunkTasks(ctx, reader, lc, tasks, labels)
	if err != nil {
		return nil, nil, nil, err
	}
	return w.poolLateChunkDocuments(ctx, modelName, lc, kept, keptLabels)
}

// admitLateChunkTasks partitions the batch into the chunks the pooling path can
// place (returned) and the text chunks it cannot (marked failed with
// lateChunkUnpoolableReason and dropped), loading each representation's text once.
func (w *EmbeddingWorker) admitLateChunkTasks(ctx context.Context, reader model.RepresentationTextReader, lc *lateChunkBatch, tasks []model.ChunkTask, labels []uint64) ([]model.ChunkTask, []uint64, error) {
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
			return nil, nil, err
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
		w.markLateChunkFailed(ctx, unpoolable, string(store.ErrorCategoryEmbeddingFailure), lateChunkUnpoolableReason)
	}
	return kept, keptLabels, nil
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

// poolLateChunkDocuments token-embeds every representation in the batch once and
// pools its chunks (SPEC 8.1.9 "Pooling": each text representation embeds its own
// persisted text and pools only its own chunks, keyed by rep_id, so two
// representations of one file never share a document text). It returns the tasks
// that survive: a document whose token embedding failed non-transiently is
// marked failed, every chunk of it, and dropped ("Failure classification"); a
// transient failure aborts the batch so it stays pending.
func (w *EmbeddingWorker) poolLateChunkDocuments(ctx context.Context, modelName string, lc *lateChunkBatch, tasks []model.ChunkTask, labels []uint64) ([]model.ChunkTask, []uint64, *lateChunkBatch, error) {
	groups, order := groupByRep(tasks)
	failed := make(map[uint64]struct{})
	for _, repID := range order {
		idxs := groups[repID]
		spans := make([]latechunk.Span, len(idxs))
		for k, idx := range idxs {
			spans[k] = latechunk.Span{Start: tasks[idx].RuneStart, End: tasks[idx].RuneEnd}
		}
		pooled, fallbackIdx, err := latechunk.EmbedDocument(ctx, lc.dec, modelName, lc.docs[repID], spans)
		if err != nil {
			if isTransientEmbedError(err) {
				return nil, nil, nil, err
			}
			w.failLateChunkDocument(ctx, repID, idxs, labels, err, failed)
			continue
		}
		for k, idx := range idxs {
			if pooled[k] != nil {
				lc.pooled[labels[idx]] = pooled[k]
			}
		}
		if len(fallbackIdx) > 0 {
			w.logf("late chunking: %d chunk span(s) of rep %d overlap no token; embedding those chunk-then-embed", len(fallbackIdx), repID)
		}
	}
	if len(failed) == 0 {
		return tasks, labels, lc, nil
	}
	kept := make([]model.ChunkTask, 0, len(tasks))
	keptLabels := make([]uint64, 0, len(labels))
	for i, t := range tasks {
		if _, gone := failed[labels[i]]; gone {
			continue
		}
		kept = append(kept, t)
		keptLabels = append(keptLabels, labels[i])
	}
	return kept, keptLabels, lc, nil
}

// failLateChunkDocument records a NON-transient token-embedding failure as a
// terminal failure of every chunk of the document (SPEC 8.1.9 "Failure
// classification"): category from the shared classifier, a reason that names the
// cause and the remediation. The chunks are requeueable once the cause is fixed
// (`dir2mcp reindex`, or the failed-chunk requeue), and they show up in the
// failed_chunks surfaces like any other terminal embedding failure.
func (w *EmbeddingWorker) failLateChunkDocument(ctx context.Context, repID int64, idxs []int, labels []uint64, err error, failed map[uint64]struct{}) {
	docLabels := make([]uint64, 0, len(idxs))
	for _, idx := range idxs {
		docLabels = append(docLabels, labels[idx])
		failed[labels[idx]] = struct{}{}
	}
	category := string(store.ClassifyError(err))
	reason := fmt.Sprintf("late chunking: token embedding of representation %d failed non-transiently (%s); every chunk of the document is marked failed rather than embedded chunk-then-embed (SPEC 8.1.9); fix the cause, then run `dir2mcp reindex` or wait for the failed-chunk requeue",
		repID, store.SanitizeReason(err.Error()))
	w.logf("late chunking: rep %d token embedding failed non-transiently (%s); marking its %d chunk(s) failed labels=%v", repID, store.SanitizeReason(err.Error()), len(docLabels), docLabels)
	w.markLateChunkFailed(ctx, docLabels, category, reason)
}

// markLateChunkFailed records a terminal late-chunking failure; a write error is
// logged, never fatal: the chunks then simply stay pending for a later cycle.
func (w *EmbeddingWorker) markLateChunkFailed(ctx context.Context, labels []uint64, category, reason string) {
	if mfErr := w.Source.MarkFailedWithCategory(ctx, labels, category, reason); mfErr != nil {
		w.logf("late chunking: mark failed update error: %v labels=%v", mfErr, labels)
	}
}

// groupByRep partitions the text tasks of a batch by representation (rep_id, the
// pooling unit), keeping first-appearance order so the pooling path is
// deterministic for a given batch. Media tasks are not grouped.
func groupByRep(tasks []model.ChunkTask) (map[int64][]int, []int64) {
	groups := make(map[int64][]int)
	var order []int64
	for idx, t := range tasks {
		if isMediaModality(t.Modality) {
			continue
		}
		key := t.RepID
		if _, seen := groups[key]; !seen {
			order = append(order, key)
		}
		groups[key] = append(groups[key], idx)
	}
	return groups, order
}

// pooledVector returns the precomputed pooled vector for a chunk, if the
// document path produced one; ok=false means the chunk goes through Embed.
func (lc *lateChunkBatch) pooledVector(chunkID uint64) ([]float32, bool) {
	if lc == nil {
		return nil, false
	}
	v, ok := lc.pooled[chunkID]
	return v, ok
}

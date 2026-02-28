package index

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"strings"
	"time"

	"github.com/Dirstral/dir2mcp/internal/model"
)

type ChunkSource interface {
	NextPending(ctx context.Context, limit int, indexKind string) ([]model.ChunkTask, error)
	MarkEmbedded(ctx context.Context, labels []int64) error
	MarkFailed(ctx context.Context, labels []int64, reason string) error
}

type EmbeddingWorker struct {
	Source         ChunkSource
	Index          model.Index
	Embedder       model.Embedder
	ModelForText   string
	ModelForCode   string
	BatchSize      int
	OnIndexedChunk func(label int64, metadata model.ChunkMetadata)

	// Logger is optional; if non‑nil its Printf method will be used for
	// informational messages. When nil the standard library's log package
	// is used. Logging is only performed for transient/retryable errors or
	// when a fatal condition occurs in Run().
	Logger *log.Logger

	// ErrCh is an optional channel that will receive fatal errors before
	// Run returns. The caller may provide a buffered channel if it wants to
	// monitor errors asynchronously; Run will still return the error as its
	// return value. The channel is never closed by EmbeddingWorker.
	ErrCh chan error

	// RunOnceFunc, if non‑nil, is invoked by Run instead of the receiver's
	// own RunOnce method. This hook exists primarily for testing and
	// allows callers that embed EmbeddingWorker to override the behaviour
	// without having to duplicate the entire Run implementation.
	//
	// Production code should rarely set this field.
	RunOnceFunc func(ctx context.Context, indexKind string) (int, error)
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
	// sanity-check tasks returned by the source.  they should already be
	// consistent, but validating here guards against misbehaving or
	// hand‑constructed implementations.
	for _, t := range tasks {
		if err := t.Validate(); err != nil {
			return 0, fmt.Errorf("%w: invalid chunk task: %v", ErrFatal, err)
		}
	}

	modelName := w.modelForKind(indexKind)
	inputs := make([]string, len(tasks))
	labels := make([]int64, len(tasks))
	for idx, task := range tasks {
		// always prefer the metadata value; Label exists only for API
		// compatibility and must mirror Metadata.ChunkID.  The prior
		// validation loop already checked this invariant, but using the
		// metadata field directly removes the need to reference Label at
		// every call site.
		chunkID := task.Metadata.ChunkID
		// validate chunk ID early so we can fail fast before expending
		// resources on embedding or indexing.  A negative ID indicates a
		// corrupt or otherwise unusable chunk.  We mark it failed and then
		// return a fatal error so the Run loop does not treat it as retryable.
		if chunkID < 0 {
			reason := "negative label not supported"
			w.logf("corrupt chunk skipped: %s label=%d", reason, chunkID)
			return 0, fmt.Errorf("%w: %s", ErrFatal, reason)
		}
		inputs[idx] = task.Text
		labels[idx] = chunkID
	}

	vectors, err := w.Embedder.Embed(ctx, modelName, inputs)
	if err != nil {
		// distinguish between transient errors (which we want to retry later)
		// and permanent failures for which the chunks should be marked as
		// irrecoverable.  A transient error could be a network timeout,
		// rate‑limit response, or context cancellation.  We intentionally keep
		// the interface simple; by returning the error without marking the
		// chunks as failed they will remain in the pending state and be
		// re‑fetched on the next cycle.  Permanent errors fall through to the
		// existing MarkFailed behaviour.
		if isTransientEmbedError(err) {
			return 0, err
		}
		if mfErr := w.Source.MarkFailed(ctx, labels, err.Error()); mfErr != nil {
			w.logf("mark failed update error: %v (source error: %v) labels=%v", mfErr, err, labels)
		}
		return 0, err
	}
	if len(vectors) != len(tasks) {
		reason := "embedding vector count mismatch"
		if mfErr := w.Source.MarkFailed(ctx, labels, reason); mfErr != nil {
			w.logf("mark failed update error: %v (reason: %s) labels=%v", mfErr, reason, labels)
		}
		return 0, errors.New(reason)
	}

	for idx := range tasks {
		if addErr := w.Index.Add(uint64(tasks[idx].Metadata.ChunkID), vectors[idx]); addErr != nil {
			if idx > 0 {
				if err := w.Source.MarkEmbedded(ctx, labels[:idx]); err != nil {
					w.logf("mark embedded warning: failed to mark %d chunks as embedded before index error: %v labels=%v", idx, err, labels[:idx])
				}
			}
			if mfErr := w.Source.MarkFailed(ctx, labels[idx:idx+1], addErr.Error()); mfErr != nil {
				w.logf("mark failed update error: %v (index error: %v) labels=%v", mfErr, addErr, labels[idx:idx+1])
			}
			return idx, addErr
		}
		if w.OnIndexedChunk != nil {
			w.OnIndexedChunk(tasks[idx].Metadata.ChunkID, tasks[idx].Metadata)
		}
	}

	// Attempt to mark all successfully indexed chunks as embedded.
	// Because the vectors are already in the index, a transient DB hiccup
	// should not cause them to be re-indexed on the next cycle – so retry
	// with exponential backoff before giving up.
	{
		const maxRetries = 3
		retryDelay := 100 * time.Millisecond
		var meErr error
		for attempt := 0; attempt < maxRetries; attempt++ {
			meErr = w.Source.MarkEmbedded(ctx, labels)
			if meErr == nil {
				break
			}
			w.logf("mark embedded attempt %d/%d failed: %v labels=%v", attempt+1, maxRetries, meErr, labels)
			if attempt < maxRetries-1 {
				select {
				case <-ctx.Done():
					return len(labels), ctx.Err()
				case <-time.After(retryDelay):
				}
				retryDelay *= 2
			}
		}
		if meErr != nil {
			w.logf("mark embedded final failure after %d attempts: %v labels=%v", maxRetries, meErr, labels)
			return len(labels), meErr
		}
	}

	return len(labels), nil
}

// Run starts a background loop that periodically calls RunOnce. A small
// tick interval is used to check for context cancellation and to space
// invocations; the caller may choose a large interval if they only want to
// poll infrequently. If RunOnce returns an error the behaviour depends on
// whether the error is retryable. Retryable errors are logged and the
// method sleeps with exponential backoff before trying again. Fatal errors
// are logged, propagated via ErrCh (if provided) and cause Run to return.
//
// Note that Run does not attempt to restart itself if a fatal error occurs;
// callers that want resilient workers should either monitor ErrCh or simply
// re‑invoke Run in a supervising goroutine.
func (w *EmbeddingWorker) Run(ctx context.Context, interval time.Duration, indexKind string) error {
	if interval <= 0 {
		interval = 2 * time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// start backoff at the same interval passed in so tests using very small
	// intervals won't sleep for a full second on the first retry.
	// interval is guaranteed positive above, so we can assign directly.
	backoff := interval
	const maxBackoff = 30 * time.Second

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			// allow an override for testing or specialised behaviour
			runOnce := w.RunOnce
			if w.RunOnceFunc != nil {
				runOnce = w.RunOnceFunc
			}
			_, err := runOnce(ctx, indexKind)
			if err != nil {
				if isRetryable(err) {
					w.logf("run once failed (retryable): %v; backing off %v", err, backoff)
					// wait either for context cancel or the backoff timer
					select {
					case <-ctx.Done():
						return ctx.Err()
					case <-time.After(backoff):
					}
					backoff *= 2
					if backoff > maxBackoff {
						backoff = maxBackoff
					}
					continue
				}
				// fatal
				w.logf("run once failed (fatal): %v", err)
				if w.ErrCh != nil {
					select {
					case w.ErrCh <- err:
					default:
					}
				}
				return err
			}
			// success, reset backoff to minimum
			backoff = interval
		}
	}
}

// logf is a small helper that routes messages to the configured logger or
// the global log package.
func (w *EmbeddingWorker) logf(format string, args ...interface{}) {
	if w != nil && w.Logger != nil {
		w.Logger.Printf(format, args...)
		return
	}
	log.Printf(format, args...)
}

// ErrFatal can be returned by RunOnce to signal that the worker should
// not retry and should exit immediately. It is exported so callers can wrap
// or compare against it if they produce fatal conditions themselves.
var ErrFatal = errors.New("fatal")

// isRetryable determines whether RunOnce should be retried when it returns
// the provided error. The predicate is intentionally conservative; context
// cancellation, deadline errors, and ErrFatal are considered fatal because
// re‑running after they have occurred is unlikely to succeed.
func isRetryable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if errors.Is(err, ErrFatal) {
		return false
	}
	return true
}

// isTransientEmbedError categorises errors returned by an Embedder.  If the
// error is considered transient the worker should not mark the associated
// chunks as failed; the caller can simply return the error and the chunk
// will remain pending.  The heuristics here are intentionally conservative –
// anything that looks like a network hiccup, timeout, rate limit, or
// cancellation is treated as transient.  Other errors are assumed to be
// permanent and callers may safely mark the work item failed.
func isTransientEmbedError(err error) bool {
	if err == nil {
		return false
	}
	// context package errors are usually propagated from the caller and
	// indicate the operation stopped; leave the chunk pending rather than
	// declare it failed.
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	// net.Error can indicate timeouts or temporary network failures.
	var ne net.Error
	if errors.As(err, &ne) {
		// timeout errors are almost always transient
		if ne.Timeout() {
			return true
		}
		// Temporary() is deprecated (see staticcheck) but in practice a
		// few drivers/clients still return it to indicate retryable network
		// glitches that are not strictly timeouts.  We check it here and
		// silence the linter rather than accidentally dropping those cases.
		if ne.Temporary() { //nolint:staticcheck
			return true
		}
	}
	// some embedder implementations return textual hints for rate limits or
	// timeouts; look for those substrings so the behaviour is still correct
	// even if they don't implement the net.Error interface.
	lower := strings.ToLower(err.Error())
	if strings.Contains(lower, "rate limit") || strings.Contains(lower, "timeout") {
		return true
	}
	return false
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

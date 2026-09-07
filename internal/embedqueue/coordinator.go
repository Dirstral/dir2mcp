package embedqueue

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/dirstral/dir2mcp/internal/model"
)

// PendingSource is the read side of the chunk store the coordinator drains: it
// returns chunks whose embedding_status is "pending" (SPEC §5.3). The metadata
// store (internal/store.SQLiteStore) already satisfies it via NextPending, so the
// coordinator reuses the exact pending-selection the in-process loop uses.
type PendingSource interface {
	NextPending(ctx context.Context, limit int, indexKind string) ([]model.ChunkTask, error)
}

// RepPendingSource is the read the coordinator needs under late chunking (SPEC
// §8.1.9 "Distributed workers"): EVERY pending chunk of one representation, not
// the page NextPending happens to return, so a document job never misses a
// pending chunk of its document. The metadata store satisfies it via
// PendingChunkTasksByRep. A coordinator with LateChunking on requires it.
type RepPendingSource interface {
	PendingChunkTasksByRep(ctx context.Context, repID int64, indexKind string) ([]model.ChunkTask, error)
}

// Coordinator enqueues embedding jobs for pending chunks (SPEC §8.7.1). It owns
// no embedding compute — it only translates the store's pending chunks into
// broker jobs bound to the current corpus reference and embed identity (§8.7.2).
// It is opt-in: the in-process loop remains the default and this type is only
// constructed when distributed mode is enabled.
type Coordinator struct {
	Source PendingSource
	Broker Broker
	// CorpusID MUST be the corpus's stable persisted identity (SPEC §5.5 —
	// identity.ResolveCorpusID), not a root path or any other incidental string.
	// It is what namespaces this corpus's jobs inside a broker several corpora
	// may share, and what a worker checks before it executes one. A coordinator
	// that leaves it empty can only be used against a broker it does not share
	// (#708).
	CorpusID      string
	SourceKind    string
	EmbedIdentity string
	// BatchSize bounds how many pending chunks are read per drain pass. A
	// non-positive value defaults to 256.
	BatchSize int
	// LateChunking is the resolved ingest.late_chunking flag (SPEC §8.1.9). On,
	// the coordinator enqueues one DOCUMENT job per representation carrying every
	// pending chunk of that representation (Job.RepID / Job.ChunkIDs) instead of
	// one job per chunk, so exactly one worker token-embeds the document and pools
	// all of its chunks. Source must then also implement RepPendingSource.
	LateChunking bool
}

// EnqueuePending enqueues the currently-pending chunks of indexKind ("text"/
// "code", or "" for both) into the broker and returns the number of jobs
// submitted. NextPending keeps returning the same pending head until those chunks
// leave the pending state (a worker marks them ok/error out-of-band), and the
// interface has no offset cursor, so one call enqueues the head it observes — NOT
// necessarily the entire backlog. The coordinator loop (runCoordinatorLoop) calls
// this on a ticker, so as the head drains the next pending chunks are picked up;
// the broker dedups by corpus_id+chunk_id+index_kind, so repeated ticks never
// pile up duplicate live jobs and a chunk id that collides with another corpus's
// is still enqueued (SPEC §8.7.3, #708). Re-running is always safe: an already-
// embedded chunk is no longer pending, and a duplicate job is idempotent at the
// embed layer (vector writes keyed by chunk_id).
func (c *Coordinator) EnqueuePending(ctx context.Context, indexKind string) (int, error) {
	if c.Source == nil || c.Broker == nil {
		return 0, fmt.Errorf("embedqueue: coordinator requires a source and broker")
	}
	if strings.TrimSpace(c.EmbedIdentity) == "" {
		return 0, fmt.Errorf("embedqueue: coordinator requires an embed identity")
	}
	batch := c.BatchSize
	if batch <= 0 {
		batch = 256
	}

	total := 0
	// NextPending returns the same chunks until they leave the pending state, so
	// we track which chunk_ids we have already enqueued this call to avoid a
	// re-read of the same head re-enqueuing them in a tight loop. Embedding marks
	// them ok/error out-of-band; within one call we simply stop once a batch
	// yields nothing new.
	seen := make(map[uint64]struct{})
	for {
		select {
		case <-ctx.Done():
			return total, ctx.Err()
		default:
		}
		tasks, err := c.Source.NextPending(ctx, batch, indexKind)
		if err != nil {
			return total, fmt.Errorf("embedqueue: read pending: %w", err)
		}
		enqueuedThisPass := 0
		for _, t := range tasks {
			id := t.Metadata.ChunkID
			if id == 0 {
				id = t.Label
			}
			if _, dup := seen[id]; dup {
				continue
			}
			job, err := c.jobFor(ctx, t, indexKind)
			if err != nil {
				return total, err
			}
			if err := c.Broker.Enqueue(ctx, job); err != nil {
				return total, fmt.Errorf("embedqueue: enqueue chunk %d: %w", id, err)
			}
			for _, cid := range job.AllChunkIDs() {
				seen[cid] = struct{}{}
			}
			seen[id] = struct{}{}
			total++
			enqueuedThisPass++
		}
		// Stop when a pass enqueued nothing new: either the store is empty or it
		// keeps returning the same already-enqueued head (status not yet updated).
		if enqueuedThisPass == 0 {
			return total, nil
		}
	}
}

// jobFor builds the job for a pending task: a per-chunk job by default, or, with
// LateChunking on and the task bound to a representation, one DOCUMENT job that
// carries every pending chunk of that representation (SPEC §8.1.9 "Distributed
// workers"). A pending chunk that belongs to no representation (a legacy row)
// cannot be grouped and is enqueued per chunk; a pooling worker then fails it
// with a reason rather than token-embedding a document for one chunk.
func (c *Coordinator) jobFor(ctx context.Context, t model.ChunkTask, indexKind string) (Job, error) {
	if !c.LateChunking || t.RepID <= 0 {
		return c.jobFromTask(t), nil
	}
	src, ok := c.Source.(RepPendingSource)
	if !ok {
		return Job{}, fmt.Errorf("embedqueue: late chunking is on but the pending source %T cannot list a representation's pending chunks (SPEC 8.1.9)", c.Source)
	}
	kind := strings.TrimSpace(t.IndexKind)
	if kind == "" {
		kind = strings.TrimSpace(indexKind)
	}
	repTasks, err := src.PendingChunkTasksByRep(ctx, t.RepID, kind)
	if err != nil {
		return Job{}, fmt.Errorf("embedqueue: read pending chunks of rep %d: %w", t.RepID, err)
	}
	if len(repTasks) == 0 {
		// The head moved under us (the chunk left pending between the two reads);
		// enqueue the one we hold so the pass still makes progress.
		repTasks = []model.ChunkTask{t}
	}
	return c.documentJob(t.RepID, repTasks), nil
}

// documentJob projects every pending chunk of one representation into ONE job
// (SPEC §8.1.9 "Distributed workers"). The job's ChunkID is the smallest chunk id
// so the broker's corpus_id+chunk_id+index_kind dedup still applies, and the
// payload identity is the first chunk's, which is enough for the worker to detect
// a superseded document (it re-reads every chunk by id and skips the stale ones).
func (c *Coordinator) documentJob(repID int64, repTasks []model.ChunkTask) Job {
	ids := make([]uint64, 0, len(repTasks))
	for _, rt := range repTasks {
		id := rt.Metadata.ChunkID
		if id == 0 {
			id = rt.Label
		}
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	first := repTasks[0]
	for _, rt := range repTasks {
		if rid := rt.Metadata.ChunkID; rid == ids[0] || (rid == 0 && rt.Label == ids[0]) {
			first = rt
		}
	}
	job := c.jobFromTask(first)
	job.ChunkID = ids[0]
	job.RepID = repID
	job.ChunkIDs = ids
	return job
}

// jobFromTask projects a pending chunk task into a broker Job (SPEC §8.7.2):
// corpus ref + chunk identity + payload identity + the enqueue-time embed
// identity. No bytes are carried — the worker reads them via CorpusFS (§7.10).
func (c *Coordinator) jobFromTask(t model.ChunkTask) Job {
	id := t.Metadata.ChunkID
	if id == 0 {
		id = t.Label
	}
	idxKind := strings.TrimSpace(t.IndexKind)
	if idxKind == "" {
		idxKind = "text"
	}
	span := t.Metadata.Span
	return Job{
		CorpusID:  c.CorpusID,
		Source:    c.SourceKind,
		ChunkID:   id,
		IndexKind: idxKind,
		// TextHash is the job's PAYLOAD identity (SPEC §8.7.2). Re-reading the
		// authoritative task by chunk_id is not sufficient on its own, which is
		// why this used to be left empty and no longer is (#710): a chunk_id is
		// stable across an in-place re-ingest of the same (rep_id, ordinal) while
		// the text, hash and index_kind are rewritten under it, so a worker that
		// only re-read the task could not tell "the chunk this job named" from "a
		// different chunk that inherited the id". The hash makes a superseded
		// delivery recognisable instead of silently executable.
		TextHash: t.TextHash,
		Modality: t.Modality,
		RelPath:  t.MediaRef,
		Span: Span{
			Kind:    span.Kind,
			Page:    span.Page,
			StartMS: span.StartMS,
			EndMS:   span.EndMS,
		},
		EmbedIdentity: c.EmbedIdentity,
	}
}

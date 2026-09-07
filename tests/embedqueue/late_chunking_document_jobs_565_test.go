package embedqueue_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dirstral/dir2mcp/internal/embedqueue"
	"github.com/dirstral/dir2mcp/internal/model"
)

const lcIdentity = "tei|http://gpu-box:8080|m|m|0|0|off|on|off"

// repTask builds a pending text chunk bound to a representation with a rune span,
// as the store hands them out under late chunking (SPEC §8.1.9).
func repTask(id uint64, repID int64, text string) model.ChunkTask {
	t := model.NewChunkTask(id, text, "text", model.ChunkMetadata{ChunkID: id, RelPath: "a.txt"})
	t.RepID = repID
	t.RuneStart = 0
	t.RuneEnd = len([]rune(text))
	return t
}

// pagedRepSource models the store under late chunking: NextPending returns the
// same pending HEAD, cut at the page limit, until chunks leave pending (they never
// do here), and PendingChunkTasksByRep returns EVERY pending chunk of one
// representation. A page limit smaller than a representation is the case that
// separates "group the page" from "group the representation".
type pagedRepSource struct {
	mu      sync.Mutex
	pending []model.ChunkTask
	repCall int
}

func (s *pagedRepSource) NextPending(_ context.Context, limit int, _ string) ([]model.ChunkTask, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit > len(s.pending) {
		limit = len(s.pending)
	}
	return append([]model.ChunkTask(nil), s.pending[:limit]...), nil
}

func (s *pagedRepSource) PendingChunkTasksByRep(_ context.Context, repID int64, _ string) ([]model.ChunkTask, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.repCall++
	var out []model.ChunkTask
	for _, t := range s.pending {
		if t.RepID == repID {
			out = append(out, t)
		}
	}
	return out, nil
}

// drainJobs leases every queued job.
func drainJobs(t *testing.T, broker embedqueue.Broker, n int) []embedqueue.Job {
	t.Helper()
	ctx := context.Background()
	var jobs []embedqueue.Job
	for i := 0; i < n; i++ {
		lease, err := broker.Lease(ctx, 0)
		if err != nil {
			t.Fatalf("Lease %d: %v", i, err)
		}
		jobs = append(jobs, lease.Job)
	}
	return jobs
}

// lcPagedCoordinator builds a coordinator over a source whose page (BatchSize 2)
// cuts representation 7 (chunks 1,2,3) after chunk 2; representation 8 holds
// chunk 4.
func lcPagedCoordinator(broker embedqueue.Broker) (*embedqueue.Coordinator, *pagedRepSource) {
	src := &pagedRepSource{pending: []model.ChunkTask{
		repTask(1, 7, "alpha"), repTask(2, 7, "beta"), repTask(3, 7, "gamma"),
		repTask(4, 8, "delta"),
	}}
	return &embedqueue.Coordinator{
		Source: src, Broker: broker, CorpusID: "corpus-x", SourceKind: "local",
		EmbedIdentity: lcIdentity, LateChunking: true, BatchSize: 2,
	}, src
}

// assertDocumentJob checks a job is a document job for repID carrying exactly ids.
func assertDocumentJob(t *testing.T, j embedqueue.Job, repID int64, ids ...uint64) {
	t.Helper()
	if !j.IsDocument() {
		t.Fatalf("a per-chunk job slipped through under late chunking: %+v", j)
	}
	if j.RepID != repID || j.ChunkID != ids[0] || len(j.ChunkIDs) != len(ids) {
		t.Fatalf("job for rep %d must carry chunks %v with chunk_id %d, got %+v", repID, ids, ids[0], j)
	}
	for i, id := range ids {
		if j.ChunkIDs[i] != id {
			t.Fatalf("job for rep %d chunk ids = %v, want %v in order", repID, j.ChunkIDs, ids)
		}
	}
}

// TestCoordinator_LateChunkingEnqueuesOneJobPerRepresentation pins SPEC §8.1.9
// "Distributed workers": with ingest.late_chunking on the coordinator enqueues ONE
// job per document representation carrying EVERY pending chunk of it, even when
// the NextPending page cuts the representation in half, and never a per-chunk job
// for a chunk that has a representation. One call enqueues the pending HEAD it
// observes (the existing contract): the page shows chunks 1 and 2, both of rep 7,
// so the call yields one job, and that job must carry chunk 3 too.
func TestCoordinator_LateChunkingEnqueuesOneJobPerRepresentation(t *testing.T) {
	ctx := context.Background()
	broker := embedqueue.NewMemBroker(3)
	coord, src := lcPagedCoordinator(broker)
	n, err := coord.EnqueuePending(ctx, "")
	if err != nil || n != 1 {
		t.Fatalf("EnqueuePending = %d, %v; want 1 job (one per representation in the observed head)", n, err)
	}
	j7 := drainJobs(t, broker, 1)[0]
	assertDocumentJob(t, j7, 7, 1, 2, 3)
	if j7.IndexKind != "text" || j7.EmbedIdentity != lcIdentity || j7.CorpusID != "corpus-x" {
		t.Fatalf("rep 7 job identity wrong: %+v", j7)
	}
	if src.repCall != 1 {
		t.Fatalf("PendingChunkTasksByRep calls = %d, want one for the one representation seen", src.repCall)
	}
}

// TestCoordinator_LateChunkingHeadMovesToNextRepresentation pins that once a
// representation's chunks leave pending, the next tick reaches the next
// representation and enqueues its document job.
func TestCoordinator_LateChunkingHeadMovesToNextRepresentation(t *testing.T) {
	ctx := context.Background()
	broker := embedqueue.NewMemBroker(3)
	coord, src := lcPagedCoordinator(broker)
	if _, err := coord.EnqueuePending(ctx, ""); err != nil {
		t.Fatalf("first tick: %v", err)
	}
	_ = drainJobs(t, broker, 1)
	src.mu.Lock()
	src.pending = src.pending[3:] // rep 7 embedded; rep 8 is now the head
	src.mu.Unlock()
	if n, err := coord.EnqueuePending(ctx, ""); err != nil || n != 1 {
		t.Fatalf("second tick: EnqueuePending = %d, %v; want 1", n, err)
	}
	assertDocumentJob(t, drainJobs(t, broker, 1)[0], 8, 4)
}

// TestCoordinator_LateChunkingOff_StaysPerChunk pins that the default is
// unchanged: with the flag off the same pending set yields one job per chunk and
// no document job.
func TestCoordinator_LateChunkingOff_StaysPerChunk(t *testing.T) {
	ctx := context.Background()
	broker := embedqueue.NewMemBroker(3)
	src := &pagedRepSource{pending: []model.ChunkTask{repTask(1, 7, "alpha"), repTask(2, 7, "beta")}}
	coord := &embedqueue.Coordinator{Source: src, Broker: broker, CorpusID: "c", SourceKind: "local", EmbedIdentity: testIdentity}
	n, err := coord.EnqueuePending(ctx, "")
	if err != nil || n != 2 {
		t.Fatalf("EnqueuePending = %d, %v; want 2 per-chunk jobs", n, err)
	}
	for _, j := range drainJobs(t, broker, 2) {
		if j.IsDocument() || j.RepID != 0 || len(j.ChunkIDs) != 0 {
			t.Fatalf("flag off must enqueue per-chunk jobs, got %+v", j)
		}
	}
	if src.repCall != 0 {
		t.Fatal("PendingChunkTasksByRep must not be consulted with the flag off")
	}
}

// TestCoordinator_LateChunkingRequiresRepPendingSource pins that a source which
// cannot list a representation's pending chunks is an error under the flag,
// never a silent fall back to per-chunk jobs.
func TestCoordinator_LateChunkingRequiresRepPendingSource(t *testing.T) {
	broker := embedqueue.NewMemBroker(3)
	src := &fakePendingSource{pending: []model.ChunkTask{repTask(1, 7, "alpha")}}
	coord := &embedqueue.Coordinator{Source: src, Broker: broker, CorpusID: "c", EmbedIdentity: lcIdentity, LateChunking: true}
	if _, err := coord.EnqueuePending(context.Background(), ""); err == nil || !strings.Contains(err.Error(), "pending chunks") {
		t.Fatalf("want an error naming the missing capability, got %v", err)
	}
	if st, _ := broker.Stats(context.Background()); st.Pending != 0 {
		t.Fatalf("no job may be enqueued: pending=%d", st.Pending)
	}
}

// lcStatus records terminal per-chunk failures the worker writes.
type lcStatus struct {
	mu     sync.Mutex
	labels []uint64
	reason string
}

func (s *lcStatus) MarkFailedWithCategory(_ context.Context, labels []uint64, _ string, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.labels = append(s.labels, labels...)
	s.reason = reason
	return nil
}

func (s *lcStatus) snapshot() ([]uint64, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]uint64(nil), s.labels...), s.reason
}

func documentJob(repID int64, ids ...uint64) embedqueue.Job {
	return embedqueue.Job{CorpusID: "c", Source: "local", ChunkID: ids[0], IndexKind: "text", EmbedIdentity: lcIdentity, RepID: repID, ChunkIDs: ids}
}

// TestWorker_LateChunkingRejectsPerChunkJob pins the worker half of document
// ownership (SPEC §8.1.9): a pooling worker MUST fail a per-chunk job rather than
// token-embed a whole document for one chunk. The job is returned, never embedded,
// and on its final delivery the chunk is recorded failed with a reason that names
// the requirement.
func TestWorker_LateChunkingRejectsPerChunkJob(t *testing.T) {
	ctx := context.Background()
	broker := embedqueue.NewMemBroker(1) // one delivery, then dead-letter
	perChunk := embedqueue.Job{CorpusID: "c", Source: "local", ChunkID: 1, IndexKind: "text", EmbedIdentity: lcIdentity}
	if err := broker.Enqueue(ctx, perChunk); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	fetch := &fakeFetcher{tasks: map[uint64]model.ChunkTask{1: repTask(1, 7, "alpha")}}
	emb := &fakeEmbedStep{}
	status := &lcStatus{}
	cfg := embedqueue.Config{
		Broker: broker, Fetcher: fetch, Embedders: map[string]embedqueue.Embedder{"text": emb},
		Status: status, EmbedIdentity: lcIdentity, LateChunking: true,
		PollInterval: time.Millisecond, RetryAfter: time.Millisecond,
	}
	runWorkerUntil(t, cfg, func() bool {
		st, _ := broker.Stats(ctx)
		return st.DeadLettered == 1
	})
	if w := emb.writes(); len(w) != 0 {
		t.Fatalf("a per-chunk job must not be embedded under late chunking, wrote %v", w)
	}
	labels, reason := status.snapshot()
	if len(labels) != 1 || labels[0] != 1 {
		t.Fatalf("the chunk must be recorded failed on the final delivery: %v", labels)
	}
	if !strings.Contains(reason, "per-chunk") || !strings.Contains(reason, "document") {
		t.Fatalf("reason must name the per-chunk / document-job requirement: %q", reason)
	}
	if fetch.calls != 0 {
		t.Fatal("the per-chunk job must be rejected before the store is read")
	}
}

// TestWorker_LateChunkingDocumentJobEmbedsAllChunksOnce pins document
// execution: one job carrying three chunks is embedded in ONE EmbedAndIndex call
// with all three tasks (so the document is token-embedded once and pooled
// together), then Acked once.
func TestWorker_LateChunkingDocumentJobEmbedsAllChunksOnce(t *testing.T) {
	ctx := context.Background()
	broker := embedqueue.NewMemBroker(3)
	if err := broker.Enqueue(ctx, documentJob(7, 1, 2, 3)); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	fetch := &fakeFetcher{tasks: map[uint64]model.ChunkTask{
		1: repTask(1, 7, "alpha"), 2: repTask(2, 7, "beta"), 3: repTask(3, 7, "gamma"),
	}}
	emb := &batchRecordingEmbedStep{}
	cfg := embedqueue.Config{
		Broker: broker, Fetcher: fetch, Embedders: map[string]embedqueue.Embedder{"text": emb},
		EmbedIdentity: lcIdentity, LateChunking: true, PollInterval: time.Millisecond,
	}
	runWorkerUntil(t, cfg, func() bool {
		st, _ := broker.Stats(ctx)
		return st.Pending == 0 && st.InFlight == 0
	})
	writes, batches := emb.snapshot()
	if len(batches) != 1 || batches[0] != 3 {
		t.Fatalf("document job must embed its 3 chunks in ONE call, got batches %v", batches)
	}
	if len(writes) != 3 || writes[0] != 1 || writes[1] != 2 || writes[2] != 3 {
		t.Fatalf("writes = %v, want chunks 1,2,3", writes)
	}
	if st, _ := broker.Stats(ctx); st.DeadLettered != 0 {
		t.Fatalf("document job must be acked, not dead-lettered: %+v", st)
	}
}

// TestWorker_LateChunkingDocumentJobSkipsTombstonedChunk pins tombstone safety at
// document granularity: a chunk of the document that no longer exists is skipped,
// the live chunks are still pooled together, and the job is acked.
func TestWorker_LateChunkingDocumentJobSkipsTombstonedChunk(t *testing.T) {
	ctx := context.Background()
	broker := embedqueue.NewMemBroker(3)
	if err := broker.Enqueue(ctx, documentJob(7, 1, 2, 3)); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	fetch := &fakeFetcher{
		tasks:   map[uint64]model.ChunkTask{1: repTask(1, 7, "alpha"), 3: repTask(3, 7, "gamma")},
		missing: map[uint64]bool{2: true},
	}
	emb := &batchRecordingEmbedStep{}
	cfg := embedqueue.Config{
		Broker: broker, Fetcher: fetch, Embedders: map[string]embedqueue.Embedder{"text": emb},
		EmbedIdentity: lcIdentity, LateChunking: true, PollInterval: time.Millisecond,
	}
	runWorkerUntil(t, cfg, func() bool {
		st, _ := broker.Stats(ctx)
		return st.Pending == 0 && st.InFlight == 0
	})
	writes, batches := emb.snapshot()
	if len(batches) != 1 || batches[0] != 2 || len(writes) != 2 || writes[0] != 1 || writes[1] != 3 {
		t.Fatalf("live chunks 1 and 3 must be embedded together, got writes=%v batches=%v", writes, batches)
	}
}

// TestJob_DocumentValidation pins the document-job shape rules.
func TestJob_DocumentValidation(t *testing.T) {
	good := documentJob(7, 1, 2)
	if err := good.Validate(); err != nil {
		t.Fatalf("valid document job rejected: %v", err)
	}
	bad := documentJob(7, 1, 2)
	bad.ChunkID = 2 // must be the first chunk id
	if err := bad.Validate(); err == nil {
		t.Fatal("chunk_id != chunk_ids[0] must be rejected")
	}
	half := embedqueue.Job{CorpusID: "c", ChunkID: 1, IndexKind: "text", EmbedIdentity: lcIdentity, RepID: 7}
	if err := half.Validate(); err == nil {
		t.Fatal("rep_id without chunk_ids must be rejected")
	}
	if ids := good.AllChunkIDs(); len(ids) != 2 || ids[1] != 2 {
		t.Fatalf("AllChunkIDs = %v", ids)
	}
	if ids := (embedqueue.Job{ChunkID: 9}).AllChunkIDs(); len(ids) != 1 || ids[0] != 9 {
		t.Fatalf("per-chunk AllChunkIDs = %v", ids)
	}
}

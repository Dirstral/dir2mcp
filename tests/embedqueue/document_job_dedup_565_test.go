package embedqueue_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/dirstral/dir2mcp/internal/embedqueue"
	"github.com/dirstral/dir2mcp/internal/model"
)

// TestBroker_DocumentJobDedupsPerRepresentation pins the document-ownership rule
// (SPEC §8.1.9 "Distributed workers": no two workers pool chunks of one
// representation concurrently) at the broker: while a document job for a
// representation is live (pending or in flight), a second document job for the
// SAME representation with a DIFFERENT first chunk id is dropped, on both shipped
// brokers. A per-chunk job keeps its per-chunk key, and another representation's
// document job still enqueues.
func TestBroker_DocumentJobDedupsPerRepresentation(t *testing.T) {
	ctx := context.Background()
	sqlBroker, err := embedqueue.NewSQLiteBroker(ctx, filepath.Join(t.TempDir(), "queue.sqlite"), 3)
	if err != nil {
		t.Fatalf("NewSQLiteBroker: %v", err)
	}
	t.Cleanup(func() { _ = sqlBroker.Close() })
	for name, broker := range map[string]embedqueue.Broker{"mem": embedqueue.NewMemBroker(3), "sqlite": sqlBroker} {
		t.Run(name, func(t *testing.T) {
			// Job A for rep 7 goes in flight (leased) with chunks 5,6,7.
			if err := broker.Enqueue(ctx, documentJob(7, 5, 6, 7)); err != nil {
				t.Fatalf("enqueue A: %v", err)
			}
			if _, err := broker.Lease(ctx, time.Minute); err != nil {
				t.Fatalf("lease A: %v", err)
			}
			// Chunk 5 left pending meanwhile; the next coordinator pass builds job B
			// for the same representation with first chunk 6. It must be dropped.
			if err := broker.Enqueue(ctx, documentJob(7, 6, 7)); err != nil {
				t.Fatalf("enqueue B: %v", err)
			}
			st, _ := broker.Stats(ctx)
			if st.Pending != 0 || st.InFlight != 1 {
				t.Fatalf("second document job for the same representation must be dropped while one is live: %+v", st)
			}
			// Another representation, and a per-chunk job, are independent units.
			if err := broker.Enqueue(ctx, documentJob(8, 9)); err != nil {
				t.Fatalf("enqueue rep 8: %v", err)
			}
			if err := broker.Enqueue(ctx, embedqueue.Job{CorpusID: "c", Source: "local", ChunkID: 6, IndexKind: "text", EmbedIdentity: lcIdentity}); err != nil {
				t.Fatalf("enqueue per-chunk: %v", err)
			}
			st, _ = broker.Stats(ctx)
			if st.Pending != 2 {
				t.Fatalf("rep 8 document job and the per-chunk job must both enqueue: %+v", st)
			}
		})
	}
}

// TestWorker_CorruptDocumentJobRowIsNamedAsCorrupt pins the reason a worker
// records for a document job row whose chunk_ids column is unreadable: it is a
// corrupt row, not a per-chunk job from an old coordinator, so the reason must
// say "corrupt document job row" and never the coordinator-upgrade text. The row
// is written through the broker's own database handle, as corruption would be.
func TestWorker_CorruptDocumentJobRowIsNamedAsCorrupt(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "queue.sqlite"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	broker, err := embedqueue.NewSQLiteBrokerWithDB(ctx, db, 1) // one delivery, then dead-letter
	if err != nil {
		t.Fatalf("NewSQLiteBrokerWithDB: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO embed_jobs(corpus_id, source, chunk_id, index_kind, text_hash, modality, rel_path,
  span_kind, span_page, span_start_ms, span_end_ms, embed_identity, rep_id, chunk_ids, state)
VALUES ('c', 'local', 5, 'text', '', 'text', '', '', 0, 0, 0, ?, 7, 'not-json', 'pending')`, lcIdentity); err != nil {
		t.Fatalf("insert corrupt row: %v", err)
	}
	fetch := &fakeFetcher{tasks: map[uint64]model.ChunkTask{5: repTask(5, 7, "alpha")}}
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
		t.Fatalf("a corrupt row must not be embedded, wrote %v", w)
	}
	labels, reason := status.snapshot()
	if len(labels) != 1 || labels[0] != 5 {
		t.Fatalf("the row's chunk must be recorded failed: %v", labels)
	}
	if !strings.Contains(reason, "corrupt document job row") {
		t.Fatalf("reason must name the corrupt row: %q", reason)
	}
	if strings.Contains(reason, "upgrade the coordinator") {
		t.Fatalf("reason must not blame the coordinator for a corrupt row: %q", reason)
	}
}

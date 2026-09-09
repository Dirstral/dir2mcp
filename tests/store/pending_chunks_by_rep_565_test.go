package tests

import (
	"context"
	"testing"

	"github.com/dirstral/dir2mcp/internal/model"
)

// TestPendingChunkTasksByRep_ReturnsEveryPendingChunkOfOneRepresentation pins
// the coordinator's late-chunking read (SPEC §8.1.9 "Distributed workers"): every
// pending chunk of the representation, none of another representation's, none
// that already embedded, in chunk_id order and in the NextPending task shape
// (rep_id and rune span included).
func TestPendingChunkTasksByRep_ReturnsEveryPendingChunkOfOneRepresentation(t *testing.T) {
	ctx := context.Background()
	st, relPath := newContextStore(t)
	insertChunk(t, st, relPath, 0, model.Chunk{Text: "alpha", TextHash: "a", RuneStart: 0, RuneEnd: 5})
	insertChunk(t, st, relPath, 1, model.Chunk{Text: "beta", TextHash: "b", RuneStart: 6, RuneEnd: 10, EmbeddingStatus: "ok"})
	insertChunk(t, st, relPath, 2, model.Chunk{Text: "gamma", TextHash: "c", RuneStart: 11, RuneEnd: 16})

	// A second document with its own representation and a pending chunk.
	if err := st.UpsertDocument(ctx, model.Document{RelPath: "docs/b.md", DocType: "md", SourceType: "local", Status: "ok"}); err != nil {
		t.Fatalf("UpsertDocument: %v", err)
	}
	insertChunk(t, st, "docs/b.md", 0, model.Chunk{Text: "other", TextHash: "o", RuneStart: 0, RuneEnd: 5})

	all, err := st.NextPending(ctx, 10, "text")
	if err != nil || len(all) != 3 {
		t.Fatalf("NextPending = %d tasks, %v; want 3 pending across both docs", len(all), err)
	}
	repID := all[0].RepID

	got, err := st.PendingChunkTasksByRep(ctx, repID, "text")
	if err != nil {
		t.Fatalf("PendingChunkTasksByRep: %v", err)
	}
	if len(got) != 2 || got[0].Text != "alpha" || got[1].Text != "gamma" {
		t.Fatalf("want the two PENDING chunks of rep %d in order, got %+v", repID, got)
	}
	for _, tk := range got {
		if tk.RepID != repID || !tk.HasRuneSpan() {
			t.Fatalf("task must carry rep_id and rune span like NextPending: %+v", tk)
		}
	}
	if _, err := st.PendingChunkTasksByRep(ctx, 0, "text"); err == nil {
		t.Fatal("rep_id 0 must be rejected")
	}
	if none, err := st.PendingChunkTasksByRep(ctx, repID+1000, ""); err != nil || len(none) != 0 {
		t.Fatalf("unknown rep: %v %v", none, err)
	}
}

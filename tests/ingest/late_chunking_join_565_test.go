package tests

import (
	"testing"

	"github.com/dirstral/dir2mcp/internal/ingest"
)

// TestLateChunkInputs_JoinFallbackAndVerifiedWindows pins the two persisted-
// input forms directly (SPEC §8.1.9 "Persisted inputs"). Segments without a
// window (OCR pages, structured blocks, transcripts) get positions in the
// ordinal "\n" join; segments with windows use the source only when EVERY window
// reproduces its text, and one wrong window sends the whole representation to
// the join, so a chunker bug can never pool the wrong runes.
func TestLateChunkInputs_JoinFallbackAndVerifiedWindows(t *testing.T) {
	// Join: no windows at all.
	segs := []ingest.ChunkSegment{{Text: "página uno"}, {Text: "page two"}, {Text: "第三页"}}
	doc, starts, ends := ingest.LateChunkInputsForTest("", segs)
	if doc != "página uno\npage two\n第三页" {
		t.Fatalf("join text = %q", doc)
	}
	runes := []rune(doc)
	for i, seg := range segs {
		if got := string(runes[starts[i]:ends[i]]); got != seg.Text {
			t.Fatalf("join segment %d: doc[%d:%d] = %q, want %q", i, starts[i], ends[i], got, seg.Text)
		}
	}
	if starts[1] != 11 || starts[2] != 20 {
		t.Fatalf("join positions must skip the single separator rune: starts=%v", starts)
	}

	// Source windows: exact windows use the source itself.
	source := "  alpha beta  gamma"
	exact := []ingest.ChunkSegment{
		{Text: "alpha beta", RuneStart: 2, RuneEnd: 12, RuneSpanKnown: true},
		{Text: "gamma", RuneStart: 14, RuneEnd: 19, RuneSpanKnown: true},
	}
	doc, starts, ends = ingest.LateChunkInputsForTest(source, exact)
	if doc != source || starts[0] != 2 || ends[1] != 19 {
		t.Fatalf("exact windows must keep the source: doc=%q starts=%v ends=%v", doc, starts, ends)
	}

	// One wrong window (off by one) sends the representation to the join.
	wrong := []ingest.ChunkSegment{
		{Text: "alpha beta", RuneStart: 2, RuneEnd: 12, RuneSpanKnown: true},
		{Text: "gamma", RuneStart: 13, RuneEnd: 18, RuneSpanKnown: true},
	}
	doc, starts, ends = ingest.LateChunkInputsForTest(source, wrong)
	if doc != "alpha beta\ngamma" {
		t.Fatalf("a wrong window must fall back to the join, got doc=%q", doc)
	}
	runes = []rune(doc)
	for i, seg := range wrong {
		if got := string(runes[starts[i]:ends[i]]); got != seg.Text {
			t.Fatalf("fallback segment %d: doc[%d:%d] = %q, want %q", i, starts[i], ends[i], got, seg.Text)
		}
	}
}

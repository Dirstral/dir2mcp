package tests

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/dirstral/dir2mcp/internal/ingest"
	"github.com/dirstral/dir2mcp/internal/model"
)

// assertExactWindows checks every segment carries a rune span into content that
// reproduces its Text exactly (SPEC §8.1.9 "Persisted inputs").
func assertExactWindows(t *testing.T, content string, segs []ingest.ChunkSegment) {
	t.Helper()
	runes := []rune(content)
	if len(segs) == 0 {
		t.Fatal("no segments")
	}
	for i, seg := range segs {
		if !seg.RuneSpanKnown {
			t.Fatalf("segment %d has no rune span", i)
		}
		if seg.RuneStart < 0 || seg.RuneEnd > len(runes) || seg.RuneEnd <= seg.RuneStart {
			t.Fatalf("segment %d span [%d,%d) out of range (len %d)", i, seg.RuneStart, seg.RuneEnd, len(runes))
		}
		if got := string(runes[seg.RuneStart:seg.RuneEnd]); got != seg.Text {
			t.Fatalf("segment %d: content[%d:%d] = %q, want Text %q", i, seg.RuneStart, seg.RuneEnd, got, seg.Text)
		}
		if seg.RuneEnd-seg.RuneStart != utf8.RuneCountInString(seg.Text) {
			t.Fatalf("segment %d span width %d != rune count %d", i, seg.RuneEnd-seg.RuneStart, utf8.RuneCountInString(seg.Text))
		}
	}
}

// TestChunkTextByChars_RuneSpansAreExactWindows pins that the char chunker
// reports each (trimmed) chunk's exact rune window in the source, with
// multi-byte runes and whitespace at window edges, so content[start:end] is the
// chunk text byte for byte.
func TestChunkTextByChars_RuneSpansAreExactWindows(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&b, "  Absatz %d: Grüße, naïve façade, 日本語のテキスト.  \n", i)
	}
	content := b.String()
	segs := ingest.ChunkTextByChars(content, 120, 20, 30)
	if len(segs) < 5 {
		t.Fatalf("expected several overlapping chunks, got %d", len(segs))
	}
	assertExactWindows(t, content, segs)
	// Overlap means neighbouring windows share source runes; the second chunk
	// must start before the first ends and after it starts.
	if segs[1].RuneStart <= segs[0].RuneStart || segs[1].RuneStart >= segs[0].RuneEnd {
		t.Fatalf("chunks must overlap in the source: %+v / %+v", segs[0], segs[1])
	}
}

// TestChunkCodeByLines_RuneSpansAreExactWindows pins the line chunker,
// including a window whose long line is sub-split by characters: every emitted
// chunk's span reproduces its text from the source.
func TestChunkCodeByLines_RuneSpansAreExactWindows(t *testing.T) {
	var lines []string
	for i := 0; i < 250; i++ {
		lines = append(lines, fmt.Sprintf("\tfunc f%d() { return \"ünïcode %d\" }", i, i))
	}
	content := strings.Join(lines, "\n")
	segs := ingest.ChunkCodeByLines(content, 100, 10)
	if len(segs) < 3 {
		t.Fatalf("expected several windows, got %d", len(segs))
	}
	assertExactWindows(t, content, segs)

	// A minified single line inside a window is split by characters; the
	// sub-windows must be rebased to the source.
	long := "short header\n" + strings.Repeat("ö", 6000) + "\nshort footer"
	subSegs := ingest.ChunkCodeByLines(long, 200, 30)
	if len(subSegs) < 3 {
		t.Fatalf("expected the long line to be sub-split, got %d segments", len(subSegs))
	}
	assertExactWindows(t, long, subSegs)
}

// textRepStore is a fake RepresentationStore that also implements
// model.RepresentationTextStore, recording what the chunk writer persisted.
type textRepStore struct {
	fakeRepStore
	texts   map[int64]string
	deleted []int64
}

func (s *textRepStore) UpsertRepresentationText(_ context.Context, repID int64, text string) error {
	if s.texts == nil {
		s.texts = make(map[int64]string)
	}
	s.texts[repID] = text
	return nil
}

func (s *textRepStore) DeleteRepresentationText(_ context.Context, repID int64) error {
	s.deleted = append(s.deleted, repID)
	delete(s.texts, repID)
	return nil
}

func (s *textRepStore) WithTx(ctx context.Context, fn func(tx model.RepresentationStore) error) error {
	// Hand the callback THIS store (not the embedded fake) so the text
	// capability is visible inside the transaction, as it is on the SQLite tx.
	return fn(s)
}

func newTextRepStore() *textRepStore {
	s := &textRepStore{}
	s.failAfter = -1
	s.nextRepID = 1
	return s
}

// TestGenerateRawText_PersistsSourceWindowsAndTextWhenLateChunkingOn pins the
// ingest half of SPEC §8.1.9: with late chunking on, every raw_text chunk is
// written with its exact rune window into the normalized source, and the
// representation's document text (the source itself, not a duplicated join) is
// persisted through model.RepresentationTextStore.
func TestGenerateRawText_PersistsSourceWindowsAndTextWhenLateChunkingOn(t *testing.T) {
	st := newTextRepStore()
	rg := ingest.NewRepresentationGenerator(st)
	rg.SetLateChunking(true)
	var b strings.Builder
	for i := 0; i < 300; i++ {
		fmt.Fprintf(&b, "Zeile %d mit Umlauten äöü und Emoji 🚀.\r\n", i)
	}
	doc := model.Document{DocID: 1, RelPath: "notes.md", DocType: "md"}
	if err := rg.GenerateRawTextFromContent(context.Background(), doc, []byte(b.String())); err != nil {
		t.Fatalf("GenerateRawTextFromContent: %v", err)
	}
	if len(st.chunks) < 2 {
		t.Fatalf("expected several chunks, got %d", len(st.chunks))
	}
	text, ok := st.texts[st.chunks[0].RepID]
	if !ok {
		t.Fatal("representation text must be persisted while late chunking is on")
	}
	if strings.Contains(text, "\r") {
		t.Fatal("the persisted document text must be the NORMALIZED source (LF line endings), the string the chunker windowed")
	}
	runes := []rune(text)
	for i, c := range st.chunks {
		if !c.RuneSpanKnown() {
			t.Fatalf("chunk %d has no rune span", i)
		}
		if got := string(runes[c.RuneStart:c.RuneEnd]); got != c.Text {
			t.Fatalf("chunk %d: text[%d:%d] = %q, want %q", i, c.RuneStart, c.RuneEnd, got, c.Text)
		}
	}
	// Source windows, not a join: the document text is shorter than the chunks
	// concatenated (they overlap), so nothing was duplicated.
	total := 0
	for _, c := range st.chunks {
		total += utf8.RuneCountInString(c.Text)
	}
	if total <= len(runes) {
		t.Fatalf("overlapping chunks (%d runes) should exceed the source (%d runes); the text looks like a join", total, len(runes))
	}
}

// TestGenerateRawText_NoTextPersistedWhenLateChunkingOff pins the default: rune
// spans are still written (cheap, reindex-free), but no document text is
// persisted, so a corpus with the mode off carries no duplicate of its text.
func TestGenerateRawText_NoTextPersistedWhenLateChunkingOff(t *testing.T) {
	st := newTextRepStore()
	rg := ingest.NewRepresentationGenerator(st)
	doc := model.Document{DocID: 1, RelPath: "a.md", DocType: "md"}
	if err := rg.GenerateRawTextFromContent(context.Background(), doc, []byte("hello world, this is a small document")); err != nil {
		t.Fatalf("GenerateRawTextFromContent: %v", err)
	}
	if len(st.texts) != 0 {
		t.Fatalf("no representation text may be persisted with the mode off: %v", st.texts)
	}
	if len(st.chunks) == 0 || !st.chunks[0].RuneSpanKnown() {
		t.Fatalf("rune spans must still be written with the mode off: %+v", st.chunks)
	}
}

// TestGenerateRawText_StoreWithoutTextCapabilityFailsLoudlyWhenOn pins that a
// store which cannot persist representation text while the mode is on is an
// error, not a silent skip: otherwise the embedding worker would find no text
// under an identity that says the corpus is pooled (SPEC §8.1.9).
func TestGenerateRawText_StoreWithoutTextCapabilityFailsLoudlyWhenOn(t *testing.T) {
	st := &fakeRepStore{failAfter: -1, nextRepID: 1}
	rg := ingest.NewRepresentationGenerator(st)
	rg.SetLateChunking(true)
	doc := model.Document{DocID: 1, RelPath: "a.md", DocType: "md"}
	err := rg.GenerateRawTextFromContent(context.Background(), doc, []byte("hello world"))
	if err == nil || !strings.Contains(err.Error(), "representation text") {
		t.Fatalf("want an error naming the missing text capability, got %v", err)
	}
}

// TestPersistSummary_JoinFallbackKeepsSpansExact pins the second persisted-input
// form (SPEC §8.1.9): when a representation's chunks cannot be placed in one
// source string, the document text is the ordinal "\n" join and every span is
// exact in it. PersistSummary hands the chunker its source, so this test drives
// the join through a representation whose source is NOT the chunk text: a
// summary whose chunker trims a leading run of whitespace shifts every window
// and the verified-window rule must still hold, or fall back to the join.
func TestPersistSummary_JoinFallbackKeepsSpansExact(t *testing.T) {
	st := newTextRepStore()
	rg := ingest.NewRepresentationGenerator(st)
	rg.SetLateChunking(true)
	summary := "   A summary that starts with spaces.\n\nIt has two paragraphs and a modest length so it is one chunk."
	rep := model.Representation{DocID: 1, RepType: model.SummaryRepType, RepHash: "h", CreatedUnix: 1}
	if err := rg.PersistSummary(context.Background(), rep, summary); err != nil {
		t.Fatalf("PersistSummary: %v", err)
	}
	text, ok := st.texts[st.chunks[0].RepID]
	if !ok {
		t.Fatal("summary representation text must be persisted")
	}
	runes := []rune(text)
	for i, c := range st.chunks {
		if !c.RuneSpanKnown() {
			t.Fatalf("chunk %d has no rune span", i)
		}
		if got := string(runes[c.RuneStart:c.RuneEnd]); got != c.Text {
			t.Fatalf("chunk %d: text[%d:%d] = %q, want %q", i, c.RuneStart, c.RuneEnd, got, c.Text)
		}
	}
}

// TestGenerateRawText_LateChunkingOffDeletesStaleText pins the #951 review
// finding: a representation rewritten with late chunking OFF must not keep the
// document text an earlier late-chunking run persisted under the same rep_id,
// or a later late-chunking run pairs that stale text with the new chunks' spans
// and pools the wrong runes without any error. Off, the writer deletes the
// text through the same store handle that rewrites the chunks.
func TestGenerateRawText_LateChunkingOffDeletesStaleText(t *testing.T) {
	st := newTextRepStore()
	rg := ingest.NewRepresentationGenerator(st)
	doc := model.Document{DocID: 1, RelPath: "notes.md", DocType: "md"}

	// An earlier run with the mode on persisted the text.
	rg.SetLateChunking(true)
	if err := rg.GenerateRawTextFromContent(context.Background(), doc, []byte("alpha beta gamma delta")); err != nil {
		t.Fatalf("on: %v", err)
	}
	if len(st.texts) != 1 {
		t.Fatalf("the on run must persist one text, got %v", st.texts)
	}
	var repID int64
	for id := range st.texts {
		repID = id
	}

	// The rewrite with the mode off must delete the text under the rep_id it
	// writes the new chunks to. The fake mints a fresh rep_id per upsert (the real
	// store reuses it per document and rep_type, which is exactly why the stale
	// text is dangerous; the store-level delete is pinned in tests/store), so the
	// assertion is on the delete call: exactly one, for the representation the
	// off run wrote, issued through the same handle as the chunks.
	rg.SetLateChunking(false)
	if err := rg.GenerateRawTextFromContent(context.Background(), doc, []byte("alpha beta gamma delta epsilon")); err != nil {
		t.Fatalf("off: %v", err)
	}
	if len(st.chunks) == 0 {
		t.Fatal("the off run must have written chunks")
	}
	offRep := st.chunks[len(st.chunks)-1].RepID
	if offRep == repID {
		t.Fatalf("test fixture: the fake must mint a fresh rep id per upsert, got %d twice", repID)
	}
	if len(st.deleted) != 1 || st.deleted[0] != offRep {
		t.Fatalf("the off run must delete the representation text of rep %d exactly once, deletes=%v", offRep, st.deleted)
	}
	if _, still := st.texts[offRep]; still {
		t.Fatalf("no text may remain under the rewritten representation: %v", st.texts)
	}
}

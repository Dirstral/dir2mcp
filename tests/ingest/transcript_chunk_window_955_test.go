package tests

import (
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/dirstral/dir2mcp/internal/config"
	"github.com/dirstral/dir2mcp/internal/ingest"
	"github.com/dirstral/dir2mcp/internal/model"
	"github.com/dirstral/dir2mcp/internal/subtitle"
)

// SPEC 8.6.1 transcript chunk window (issue #955). A transcript segment is one
// breath group, about eight seconds, which is too fine a retrieval unit for
// speech. These tests pin the merge rules, and — the property that matters most
// — that subtitle export is not affected by the merge at all.

// seg builds one per-segment transcript chunk.
func seg(startMS, endMS int, text string) ingest.ChunkSegment {
	return ingest.ChunkSegment{
		Text: text,
		Span: model.Span{Kind: "time", StartMS: startMS, EndMS: endMS},
	}
}

// speech is a plausible run of whisper segments: eight short breath groups, two
// seconds of silence before the sixth, then four more.
func speech() []ingest.ChunkSegment {
	return []ingest.ChunkSegment{
		seg(0, 8000, "That's one small step for man."),
		seg(8000, 15000, "One giant leap for mankind."),
		seg(15000, 23000, "Beautiful, beautiful."),
		seg(23000, 31000, "Magnificent desolation."),
		seg(31000, 38000, "Isn't that something."),
		seg(48000, 55000, "Hello, Neil and Buzz."),
		seg(55000, 62000, "I'm talking to you by telephone."),
		seg(62000, 70000, "From the Oval Office."),
	}
}

func TestChunkWindowMergesConsecutiveSegments(t *testing.T) {
	got := ingest.MergeTranscriptChunkWindows(speech(), 40, 6)
	if len(got) >= len(speech()) {
		t.Fatalf("merge produced %d chunks from %d segments, expected fewer", len(got), len(speech()))
	}
	first := got[0]
	if first.Span.StartMS != 0 {
		t.Errorf("window start = %d, want the first segment's start 0", first.Span.StartMS)
	}
	if !strings.HasPrefix(first.Text, "That's one small step for man. One giant leap") {
		t.Errorf("members must join with one space, got %q", first.Text)
	}
	for i, c := range got {
		if c.Span.EndMS-c.Span.StartMS > 40000 {
			t.Errorf("chunk %d spans %d ms, over the 40 s window", i, c.Span.EndMS-c.Span.StartMS)
		}
		if c.Span.EndMS < c.Span.StartMS {
			t.Errorf("chunk %d span runs backwards: [%d,%d]", i, c.Span.StartMS, c.Span.EndMS)
		}
	}
}

func TestChunkWindowClosesOnSilence(t *testing.T) {
	// The ten-second silence before "Hello, Neil and Buzz" is well over the 6 s
	// gap, so no chunk may contain text from both sides of it.
	got := ingest.MergeTranscriptChunkWindows(speech(), 120, 6)
	for _, c := range got {
		if strings.Contains(c.Text, "Isn't that something") && strings.Contains(c.Text, "Hello, Neil") {
			t.Fatalf("window crossed a %d ms silence: %q", 48000-38000, c.Text)
		}
	}
	// Without the gap rule the same input merges across it, which is what proves
	// the silence, not the duration cap, closed the window above.
	nogap := ingest.MergeTranscriptChunkWindows(speech(), 120, 0)
	if len(nogap) >= len(got) {
		t.Fatalf("gap rule made no difference: %d chunks with it, %d without", len(got), len(nogap))
	}
}

func TestChunkWindowZeroIsIdentity(t *testing.T) {
	in := speech()
	got := ingest.MergeTranscriptChunkWindows(in, 0, 6)
	if !reflect.DeepEqual(got, in) {
		t.Fatalf("transcript_chunk_sec 0 must return the input unchanged (pre-0.62 behavior)")
	}
}

func TestChunkWindowNeverCrossesASpeakerChange(t *testing.T) {
	in := []ingest.ChunkSegment{
		{Text: "Good morning.", Span: model.Span{Kind: "time", StartMS: 0, EndMS: 3000, Speaker: "S1"}},
		{Text: "Thanks for coming.", Span: model.Span{Kind: "time", StartMS: 3000, EndMS: 6000, Speaker: "S1"}},
		{Text: "Glad to be here.", Span: model.Span{Kind: "time", StartMS: 6000, EndMS: 9000, Speaker: "S2"}},
	}
	got := ingest.MergeTranscriptChunkWindows(in, 40, 6)
	if len(got) != 2 {
		t.Fatalf("got %d chunks, want 2 (one per speaker turn)", len(got))
	}
	if got[0].Span.Speaker != "S1" || got[1].Span.Speaker != "S2" {
		t.Fatalf("speakers = %q/%q, want S1/S2", got[0].Span.Speaker, got[1].Span.Speaker)
	}
	if strings.Contains(got[0].Text, "Glad to be here") {
		t.Fatalf("a chunk was attributed to a speaker who did not say half of it: %q", got[0].Text)
	}
}

func TestChunkWindowRecordsExactCueLengths(t *testing.T) {
	got := ingest.MergeTranscriptChunkWindows(speech(), 40, 6)
	merged := 0
	for _, c := range got {
		if len(c.Span.Cues) == 0 {
			continue
		}
		merged++
		total := 0
		for _, cue := range c.Span.Cues {
			if cue.N <= 0 {
				t.Errorf("recorded cue length %d is not usable", cue.N)
			}
			total += cue.N
		}
		// members plus one joining space between each pair
		if want := utf8.RuneCountInString(c.Text); total+len(c.Span.Cues)-1 != want {
			t.Errorf("cue lengths sum to %d (+%d separators), chunk text is %d runes",
				total, len(c.Span.Cues)-1, want)
		}
	}
	if merged == 0 {
		t.Fatal("no chunk recorded its merged cue boundaries")
	}
}

func TestChunkWindowKeepsWordTiming(t *testing.T) {
	in := []ingest.ChunkSegment{
		{Text: "one two", Span: model.Span{Kind: "time", StartMS: 0, EndMS: 2000,
			Words: []model.WordSpan{{T: 0, D: 900, W: "one"}, {T: 1000, D: 900, W: "two"}}}},
		{Text: "three", Span: model.Span{Kind: "time", StartMS: 2000, EndMS: 3000,
			Words: []model.WordSpan{{T: 2000, D: 900, W: "three"}}}},
	}
	got := ingest.MergeTranscriptChunkWindows(in, 40, 6)
	if len(got) != 1 {
		t.Fatalf("got %d chunks, want 1", len(got))
	}
	if len(got[0].Span.Words) != 3 {
		t.Fatalf("merged window carries %d words, want the 3 its members had", len(got[0].Span.Words))
	}
	if got[0].Span.Words[2].W != "three" {
		t.Errorf("word order changed: %v", got[0].Span.Words)
	}
}

func TestChunkWindowLeavesNonTimeSpansAlone(t *testing.T) {
	in := []ingest.ChunkSegment{
		{Text: "a", Span: model.Span{Kind: "lines", StartLine: 1, EndLine: 1}},
		seg(0, 3000, "spoken"),
		seg(3000, 6000, "words"),
	}
	got := ingest.MergeTranscriptChunkWindows(in, 40, 6)
	if len(got) != 2 {
		t.Fatalf("got %d chunks, want 2 (the lines chunk, then one merged window)", len(got))
	}
	if got[0].Span.Kind != "lines" || got[0].Text != "a" {
		t.Errorf("a non-time chunk must pass through untouched, got %+v", got[0])
	}
}

func TestChunkWindowDoesNotMergeBackwards(t *testing.T) {
	in := []ingest.ChunkSegment{
		seg(10000, 13000, "later"),
		seg(0, 3000, "earlier"),
	}
	got := ingest.MergeTranscriptChunkWindows(in, 60, 0)
	if len(got) != 2 {
		t.Fatalf("an out-of-order segment must open a new window, got %d chunks", len(got))
	}
	for _, c := range got {
		if c.Span.EndMS < c.Span.StartMS {
			t.Errorf("span runs backwards: [%d,%d]", c.Span.StartMS, c.Span.EndMS)
		}
	}
}

// TestChunkWindowDoesNotChangeSubtitleExport is the property SPEC 8.6.3 turns
// on: retrieval may score forty-second windows, but the viewer must still get
// the transcript's own cues. A merged corpus and an unmerged corpus must export
// byte-for-byte identical subtitles.
func TestChunkWindowDoesNotChangeSubtitleExport(t *testing.T) {
	unmerged := speech()
	merged := ingest.MergeTranscriptChunkWindows(speech(), 40, 6)
	if len(merged) == len(unmerged) {
		t.Fatal("nothing merged, so this proves nothing")
	}
	want := subtitle.RenderSRT(subtitle.BuildCues(toTranscriptChunks(unmerged)))
	got := subtitle.RenderSRT(subtitle.BuildCues(toTranscriptChunks(merged)))
	if got != want {
		t.Fatalf("subtitle export changed under the chunk window.\n--- merged ---\n%s\n--- unmerged ---\n%s", got, want)
	}
}

// TestSubtitleExportKeepsWholeChunkWithoutCueRecord covers the degrade path: a
// chunk carrying no recorded boundaries (every corpus indexed before the chunk
// window existed) renders as one cue, exactly as it always did.
func TestSubtitleExportKeepsWholeChunkWithoutCueRecord(t *testing.T) {
	merged := ingest.MergeTranscriptChunkWindows(speech(), 40, 6)
	for i := range merged {
		merged[i].Span.Cues = nil
	}
	cues := subtitle.BuildCues(toTranscriptChunks(merged))
	if len(cues) != len(merged) {
		t.Fatalf("got %d cues from %d chunks, want one cue per chunk", len(cues), len(merged))
	}
}

// TestSubtitleExportKeepsChunkWhenCueRecordDoesNotFit pins the conservative
// rule: boundaries that do not describe the text are ignored rather than used to
// cut in the wrong place.
func TestSubtitleExportKeepsChunkWhenCueRecordDoesNotFit(t *testing.T) {
	merged := ingest.MergeTranscriptChunkWindows(speech(), 40, 6)
	target := -1
	for i := range merged {
		if len(merged[i].Span.Cues) > 1 {
			target = i
			break
		}
	}
	if target < 0 {
		t.Fatal("no merged chunk to corrupt")
	}
	merged[target].Span.Cues[0].N += 7 // lengths no longer sum to the text
	cues := subtitle.BuildCues(toTranscriptChunks(merged[target : target+1]))
	if len(cues) != 1 {
		t.Fatalf("got %d cues, want the whole chunk kept as 1", len(cues))
	}
	if cues[0].Text != merged[target].Text {
		t.Errorf("chunk text was cut despite unusable boundaries: %q", cues[0].Text)
	}
}

func toTranscriptChunks(segs []ingest.ChunkSegment) []subtitle.TranscriptChunk {
	out := make([]subtitle.TranscriptChunk, 0, len(segs))
	for _, s := range segs {
		out = append(out, subtitle.TranscriptChunk{Text: s.Text, Span: s.Span})
	}
	return out
}

// SPEC 8.6.1 transcript chunk window config (dir2mcp #955). The default ships
// ON, and 0 is a real value (disable), not "unset, use the default".
func TestTranscriptChunkWindowDefaults(t *testing.T) {
	cfg := config.Default()
	if cfg.MediaTranscriptChunkSec != config.DefaultTranscriptChunkSec {
		t.Errorf("media.transcript_chunk_sec default = %d, want %d",
			cfg.MediaTranscriptChunkSec, config.DefaultTranscriptChunkSec)
	}
	if cfg.MediaTranscriptChunkGapSec != config.DefaultTranscriptChunkGapSec {
		t.Errorf("media.transcript_chunk_gap_sec default = %d, want %d",
			cfg.MediaTranscriptChunkGapSec, config.DefaultTranscriptChunkGapSec)
	}
	if config.DefaultTranscriptChunkSec <= 0 {
		t.Error("the chunk window must ship enabled: one chunk per provider segment is the defect #955 reports")
	}
}

// The leading-silence trim (#258) moves a span's bounds and its word timings. It
// must move the merged-cue record too. The chunk window runs after the trim
// today, so this is a latent trap rather than a live bug: a helper that claims
// to move a span's timing and leaves one carrier behind would place subtitle
// text where nobody speaks as soon as the order changed.
func TestChunkWindowCuesFollowTheLeadingSilenceTrim(t *testing.T) {
	merged := ingest.MergeTranscriptChunkWindows(speech(), 40, 6)
	before := make([][]model.CueSpan, len(merged))
	recorded := 0
	for i, c := range merged {
		before[i] = append([]model.CueSpan(nil), c.Span.Cues...)
		recorded += len(c.Span.Cues)
	}
	if recorded < 2 {
		t.Fatal("no merged cue record to trim")
	}

	const offset = 5000
	shifted := ingest.ShiftTranscriptSpans(merged, offset)
	if len(shifted) != len(merged) {
		t.Fatalf("shift returned %d chunks, want %d", len(shifted), len(merged))
	}
	checked := 0
	for i, c := range shifted {
		if len(c.Span.Cues) != len(before[i]) {
			t.Fatalf("chunk %d: cue count changed from %d to %d", i, len(before[i]), len(c.Span.Cues))
		}
		for j, cue := range c.Span.Cues {
			want := before[i][j].T - offset
			if want < 0 {
				want = 0
			}
			if cue.T != want {
				t.Errorf("chunk %d cue %d: start %d, want %d (was %d, offset %d)",
					i, j, cue.T, want, before[i][j].T, offset)
			}
			if cue.D != before[i][j].D || cue.N != before[i][j].N {
				t.Errorf("chunk %d cue %d: the trim changed duration or length: %+v was %+v",
					i, j, cue, before[i][j])
			}
			checked++
		}
	}
	if checked < 2 {
		t.Fatalf("only %d cues checked, the case is not covered", checked)
	}
}

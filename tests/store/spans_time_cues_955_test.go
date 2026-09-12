package tests

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/dirstral/dir2mcp/internal/model"
	"github.com/dirstral/dir2mcp/internal/store"
)

// TestTimeSpanCuesRoundTrip pins that the transcript segment boundaries a chunk
// window merged survive SpanToRow -> SpanFromRow via the extra_json `cues` array
// (SPEC 8.6.1, dir2mcp #955). Subtitle export reads them to cut a merged chunk
// back into the segments it came from, so losing them in storage would silently
// turn a forty-second retrieval window into a forty-second subtitle cue.
func TestTimeSpanCuesRoundTrip(t *testing.T) {
	in := model.Span{
		Kind:    "time",
		StartMS: 1000,
		EndMS:   9000,
		Cues: []model.CueSpan{
			{T: 1000, D: 3000, N: 11},
			{T: 4000, D: 5000, N: 14},
		},
	}

	kind, start, end, extra, err := store.SpanToRow(in)
	if err != nil {
		t.Fatalf("SpanToRow: %v", err)
	}
	var decoded struct {
		Cues []map[string]any `json:"cues"`
	}
	if err := json.Unmarshal([]byte(extra), &decoded); err != nil {
		t.Fatalf("extra_json not valid JSON: %v (%s)", err, extra)
	}
	if len(decoded.Cues) != 2 {
		t.Fatalf("extra_json cues = %d, want 2 (%s)", len(decoded.Cues), extra)
	}
	for _, key := range []string{"t", "d", "n"} {
		if _, ok := decoded.Cues[0][key]; !ok {
			t.Errorf("extra_json cue missing %q key: %s", key, extra)
		}
	}

	out := store.SpanFromRow(kind, start, end, extra)
	if !reflect.DeepEqual(out.Cues, in.Cues) {
		t.Errorf("round-trip cues = %+v, want %+v", out.Cues, in.Cues)
	}
}

// TestTimeSpanWithoutCuesHasNoExtraJSON pins backward compatibility: an unmerged
// time span produces an empty extra_json (stored as SQL NULL), so a corpus
// indexed before the chunk window existed round-trips byte-identically.
func TestTimeSpanWithoutCuesHasNoExtraJSON(t *testing.T) {
	kind, start, end, extra, err := store.SpanToRow(model.Span{Kind: "time", StartMS: 0, EndMS: 2000})
	if err != nil {
		t.Fatalf("SpanToRow: %v", err)
	}
	if extra != "" {
		t.Fatalf("extra_json = %q, want empty for a span with no optional metadata", extra)
	}
	if out := store.SpanFromRow(kind, start, end, extra); len(out.Cues) != 0 {
		t.Errorf("round-trip invented %d cues", len(out.Cues))
	}
}

// TestTimeSpanCuesRejectAnUnusableEntry pins the read-side guard: an entry that
// can cut nothing invalidates the whole record. Dropping it instead would shift
// every later cut point and put half a word on screen, which is worse than
// exporting the chunk whole.
func TestTimeSpanCuesRejectAnUnusableEntry(t *testing.T) {
	extra := `{"cues":[{"t":0,"d":1000,"n":5},{"t":1000,"d":1000,"n":0},{"t":2000,"d":-5,"n":4}]}`
	if out := store.SpanFromRow("time", 0, 3000, extra); len(out.Cues) != 0 {
		t.Fatalf("cues = %+v, want the whole record rejected", out.Cues)
	}
}

// A negative duration on an otherwise complete entry is clamped, not rejected:
// the entry still says where the cue starts and how much text it covers, and a
// zero-width cue is legible where a missing one is not.
func TestTimeSpanCuesClampANegativeDuration(t *testing.T) {
	out := store.SpanFromRow("time", 0, 3000, `{"cues":[{"t":0,"d":1000,"n":5},{"t":2000,"d":-5,"n":4}]}`)
	if len(out.Cues) != 2 {
		t.Fatalf("cues = %+v, want 2", out.Cues)
	}
	if out.Cues[1].D != 0 {
		t.Errorf("negative duration = %d, want clamped to 0", out.Cues[1].D)
	}
}

// TestTimeSpanCuesTolerateMalformedPayload pins the degrade path: a corrupt
// extra_json yields no cues, so the chunk exports whole instead of erroring.
func TestTimeSpanCuesTolerateMalformedPayload(t *testing.T) {
	if out := store.SpanFromRow("time", 0, 3000, `{"cues":`); len(out.Cues) != 0 {
		t.Errorf("malformed payload produced %d cues", len(out.Cues))
	}
}

// A cue that omits a timing field is not a cue. Numbers missing from JSON decode
// as zero, and zero is a legal start for the first cue of a recording, so the
// record must be rejected on PRESENCE rather than on value. Without this,
// {"n":5} reads as a cue at 0 ms lasting 0 ms and puts five runes of subtitle
// text at the start of the file.
func TestTimeSpanCuesRejectRecordMissingTiming(t *testing.T) {
	for name, extra := range map[string]string{
		"no t":     `{"cues":[{"d":1000,"n":5},{"t":1000,"d":1000,"n":4}]}`,
		"no d":     `{"cues":[{"t":0,"n":5},{"t":1000,"d":1000,"n":4}]}`,
		"no n":     `{"cues":[{"t":0,"d":1000},{"t":1000,"d":1000,"n":4}]}`,
		"empty":    `{"cues":[{}]}`,
		"negative": `{"cues":[{"t":-5,"d":1000,"n":5}]}`,
	} {
		if out := store.SpanFromRow("time", 0, 3000, extra); len(out.Cues) != 0 {
			t.Errorf("%s: expected the whole record rejected, got %+v", name, out.Cues)
		}
	}
}

// A zero start IS valid: the first cue of a recording begins at 0 ms. The
// presence check must not turn into a value check.
func TestTimeSpanCuesAcceptAZeroStart(t *testing.T) {
	out := store.SpanFromRow("time", 0, 3000, `{"cues":[{"t":0,"d":1000,"n":5},{"t":1000,"d":900,"n":4}]}`)
	if len(out.Cues) != 2 {
		t.Fatalf("cues = %+v, want 2", out.Cues)
	}
	if out.Cues[0].T != 0 || out.Cues[0].D != 1000 || out.Cues[0].N != 5 {
		t.Errorf("first cue = %+v, want {0 1000 5}", out.Cues[0])
	}
}

// One unusable entry invalidates the record rather than being skipped: the
// lengths are read in order and summed, so dropping one shifts every later cut.
func TestTimeSpanCuesRejectRecordWithOneBadEntry(t *testing.T) {
	out := store.SpanFromRow("time", 0, 3000, `{"cues":[{"t":0,"d":1000,"n":5},{"t":1000,"d":1000,"n":0}]}`)
	if len(out.Cues) != 0 {
		t.Errorf("cues = %+v, want the whole record rejected", out.Cues)
	}
}

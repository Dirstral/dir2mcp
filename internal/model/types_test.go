package model

import (
	"encoding/json"
	"testing"
)

func TestStatsJSONFlattening(t *testing.T) {
	s := Stats{
		Root:            "r",
		StateDir:        "s",
		ProtocolVersion: "v",
		// deliberately use a value with non-empty DocCounts so that the
		// test exercises the normal flattening behaviour.
		CorpusStats: CorpusStats{
			DocCounts:       map[string]int64{"a": 1},
			TotalDocs:       2,
			Scanned:         3,
			Indexed:         4,
			Skipped:         5,
			Deleted:         6,
			Representations: 7,
			ChunksTotal:     8,
			EmbeddedOK:      9,
			Errors:          10,
		},
	}
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	var out map[string]interface{}
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal of json map failed: %v", err)
	}
	// ensure the embedded struct was flattened
	if _, ok := out["CorpusStats"]; ok {
		t.Fatalf("expected embedded struct to be flattened, got CorpusStats key")
	}
	expected := []string{
		"root",
		"state_dir",
		"protocol_version",
		"doc_counts",
		"total_docs",
		"scanned",
		"indexed",
		"skipped",
		"deleted",
		"representations",
		"chunks_total",
		"embedded_ok",
		"errors",
	}
	for _, key := range expected {
		if _, ok := out[key]; !ok {
			t.Errorf("expected key %q in json output", key)
		}
	}
}

// Ensure that a zero-value CorpusStats (with nil DocCounts) encodes to an
// object rather than null. This guards against clients breaking when they
// receive stats from components that didn't pre-populate the map.
func TestCorpusStatsMarshalNilDocCounts(t *testing.T) {
	cs := CorpusStats{} // DocCounts is nil here
	data, err := json.Marshal(cs)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	var out map[string]interface{}
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal of json map failed: %v", err)
	}
	v, ok := out["doc_counts"]
	if !ok {
		t.Fatalf("json output missing doc_counts key")
	}
	if v == nil {
		t.Fatalf("expected doc_counts object, got null")
	}
	if _, ok := v.(map[string]interface{}); !ok {
		t.Fatalf("expected doc_counts to be object, got %T", v)
	}
}

func TestStatsMarshalNilDocCounts(t *testing.T) {
	s := Stats{Root: "r", StateDir: "s", ProtocolVersion: "v"}
	// CorpusStats is zero value; DocCounts == nil
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("stats marshal failed: %v", err)
	}
	var out map[string]interface{}
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal of json map failed: %v", err)
	}
	// metadata should still be present
	for _, key := range []string{"root", "state_dir", "protocol_version"} {
		if _, ok := out[key]; !ok {
			t.Fatalf("expected metadata key %q in json output", key)
		}
	}
	// doc_counts should be an object, not null
	v, ok := out["doc_counts"]
	if !ok {
		t.Fatalf("json output missing doc_counts key")
	}
	if v == nil {
		t.Fatalf("expected doc_counts object, got null")
	}
	if _, ok := v.(map[string]interface{}); !ok {
		t.Fatalf("expected doc_counts to be object, got %T", v)
	}
}

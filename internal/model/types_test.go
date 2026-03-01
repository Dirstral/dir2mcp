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

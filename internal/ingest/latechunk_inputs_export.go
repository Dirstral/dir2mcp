package ingest

// LateChunkInputsForTest exposes lateChunkInputs so the two persisted-input
// forms (SPEC §8.1.9: verified source windows, else the ordinal join) can be
// pinned from tests/ as AGENTS.md requires. It returns the document text and
// one [start,end) rune span per segment. Production code never calls this.
func LateChunkInputsForTest(source string, segments []ChunkSegment) (docText string, starts, ends []int) {
	raw := make([]chunkSegment, len(segments))
	for i, seg := range segments {
		raw[i] = chunkSegment(seg)
	}
	docText, spans := lateChunkInputs(source, raw)
	for _, sp := range spans {
		starts = append(starts, sp.start)
		ends = append(ends, sp.end)
	}
	return docText, starts, ends
}

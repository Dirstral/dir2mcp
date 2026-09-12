package ingest

import "sort"

// TranscriptCoverage records WHICH PART of a recording a windowed decode actually
// produced text for (SPEC §8.6.13, issue #961).
//
// A recording too long or too large for one STT request is decoded in several
// overlapping windows and merged into one transcript. Windows fail
// independently: a provider timeout, a cancelled context, a cut ffmpeg refuses.
// Before this type the merged transcript carried no trace of that, so a
// 73-minute recording that decoded one window of eight was indexed exactly like
// a complete one: status ok, chunks searchable, and nothing anywhere saying
// that 88% of the audio was never transcribed. An editor then reads "not found"
// for minute eleven onward and cannot tell it from "not said".
//
// It is recorded on the transcript representation's meta_json ONLY for a decode
// of two or more windows. A single-request decode records nothing, and per
// §5.2 absence means "no assertion", never "complete".
type TranscriptCoverage struct {
	// WindowsAttempted is how many windows the decode scheduled (>= 2 whenever
	// this value is recorded at all).
	WindowsAttempted int `json:"windows_attempted"`
	// WindowsDecoded is how many of them yielded transcript content.
	WindowsDecoded int `json:"windows_decoded"`
	// DecodedMS is the summed length of Ranges: how much of the recording the
	// transcript actually covers.
	DecodedMS int `json:"decoded_ms"`
	// DurationMS is the recording's length. 0 when the duration probe failed, in
	// which case Fraction falls back to the window counts.
	DurationMS int `json:"duration_ms"`
	// Ranges are the decoded stretches in ABSOLUTE recording time, coalesced,
	// non-overlapping and ascending (§8.6.13), so a consumer reads the gaps
	// directly instead of reconstructing them from window arithmetic.
	Ranges []CoverageRange `json:"ranges,omitempty"`
}

// CoverageRange is one decoded stretch of a recording, in absolute milliseconds
// from the start of the whole media, end-exclusive.
type CoverageRange struct {
	StartMS int `json:"start_ms"`
	EndMS   int `json:"end_ms"`
}

// Fraction is how much of the recording decoded, in [0,1]. It prefers the
// measured time (DecodedMS/DurationMS), because that is the honest quantity an
// operator cares about: eight equal windows of which one decoded is 12% of the
// audio, but a final short window skews the count. When the duration probe
// failed (DurationMS == 0) there is no time to measure against, so it degrades
// to the window counts rather than reporting a false 0.
//
// A nil coverage, or one that attempted nothing, reports 1: no windowing
// happened, so there is no partial-decode claim to make.
func (c *TranscriptCoverage) Fraction() float64 {
	if c == nil || c.WindowsAttempted <= 0 {
		return 1
	}
	if c.DurationMS > 0 {
		f := float64(c.DecodedMS) / float64(c.DurationMS)
		if f > 1 {
			// Overlapping windows are coalesced before they are summed, so this can
			// only come from a duration probe that under-reports the media. Clamp
			// rather than report more than whole.
			return 1
		}
		return f
	}
	return float64(c.WindowsDecoded) / float64(c.WindowsAttempted)
}

// Complete reports whether the transcript covers the whole recording. It is the
// positive statement §8.6.13 requires a fully decoded multi-window transcript to
// make, so that absence of the whole object keeps its §5.2 "no assertion" meaning.
//
// With a known duration it asks the measured question ("does the decoded time
// reach the end of the recording?") rather than the bookkeeping one ("did every
// window I scheduled come back?"). The two normally agree, because the scheduled
// windows tile the recording. When they disagree the measured answer is the one
// that matters: a caller uses this to decide whether to ANNOUNCE a gap, and a gap
// that the window counts cannot see is exactly the gap worth announcing.
func (c *TranscriptCoverage) Complete() bool {
	if c == nil {
		return true
	}
	if c.DurationMS > 0 {
		return c.DecodedMS >= c.DurationMS
	}
	return c.WindowsDecoded >= c.WindowsAttempted
}

// coalesceCoverageRanges sorts ranges by start and merges every overlapping or
// adjacent pair, returning non-overlapping ascending ranges (§8.6.13). Decode
// windows overlap by design (TranscriptWindowOverlapMS), so without this a fully
// decoded recording would report more decoded milliseconds than it is long.
// Empty and inverted ranges are dropped.
func coalesceCoverageRanges(ranges []CoverageRange) []CoverageRange {
	cleaned := make([]CoverageRange, 0, len(ranges))
	for _, r := range ranges {
		if r.EndMS > r.StartMS {
			cleaned = append(cleaned, r)
		}
	}
	if len(cleaned) == 0 {
		return nil
	}
	sort.Slice(cleaned, func(a, b int) bool {
		if cleaned[a].StartMS != cleaned[b].StartMS {
			return cleaned[a].StartMS < cleaned[b].StartMS
		}
		return cleaned[a].EndMS < cleaned[b].EndMS
	})
	out := []CoverageRange{cleaned[0]}
	for _, r := range cleaned[1:] {
		last := &out[len(out)-1]
		// `>=` merges a range that merely TOUCHES the previous one: two windows
		// scheduled back to back cover one continuous stretch, and reporting it as
		// two ranges would read as a gap of zero milliseconds.
		if r.StartMS >= last.StartMS && r.StartMS <= last.EndMS {
			if r.EndMS > last.EndMS {
				last.EndMS = r.EndMS
			}
			continue
		}
		out = append(out, r)
	}
	return out
}

// newTranscriptCoverage builds the §8.6.13 coverage record from a windowed
// decode's raw per-piece ranges. It coalesces the ranges, clamps them to the
// recording, and sums what is left.
//
// It returns nil when fewer than two windows were attempted: §8.6.13 scopes the
// record to a MULTI-window decode, and recording it for a single request would
// turn "no assertion" into a claim about a decode that was never windowed.
func newTranscriptCoverage(attempted, decoded, totalMS int, ranges []CoverageRange) *TranscriptCoverage {
	if attempted < 2 {
		return nil
	}
	if totalMS > 0 {
		clamped := make([]CoverageRange, 0, len(ranges))
		for _, r := range ranges {
			if r.StartMS < 0 {
				r.StartMS = 0
			}
			if r.EndMS > totalMS {
				r.EndMS = totalMS
			}
			clamped = append(clamped, r)
		}
		ranges = clamped
	}
	merged := coalesceCoverageRanges(ranges)
	decodedMS := 0
	for _, r := range merged {
		decodedMS += r.EndMS - r.StartMS
	}
	return &TranscriptCoverage{
		WindowsAttempted: attempted,
		WindowsDecoded:   decoded,
		DecodedMS:        decodedMS,
		DurationMS:       totalMS,
		Ranges:           merged,
	}
}

package ingest

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/dirstral/dir2mcp/internal/avutil"
	"github.com/dirstral/dir2mcp/internal/model"
)

// TranscriptWindow is one decoded audio window: the offset (ms from the start of
// the whole recording) at which the window began, and the window-local decode
// result (its timestamps start at 0 for the window). Exported so the pure merge
// logic can be unit-tested from the tests/ tree (AGENTS.md: no new _test.go under
// internal/).
//
// The same window carries a plain transcription (issue #954) and a Whisper
// translate decode: both produce the `[mm:ss] text` segment format plus optional
// word timings, so one scheduler, one decode loop and one merger serve both.
type TranscriptWindow struct {
	StartMS int
	Res     model.TranscriptResult
}

// TranscriptWindowOverlapMS derives the overlap between consecutive decode windows
// from the window length: enough lookahead that a sentence straddling a boundary
// is fully decoded in the window it starts in, capped so the overlap never
// dominates the window. Deriving it (rather than exposing a second knob) keeps the
// public surface to a single media.translate.whisper_window_sec setting, and gives
// the plain-transcription path (which has no knob at all) the same rule.
func TranscriptWindowOverlapMS(windowMS int) int {
	overlap := windowMS / 5
	if overlap > 10000 {
		overlap = 10000
	}
	if overlap < 0 {
		overlap = 0
	}
	return overlap
}

// MergeTranscriptWindows stitches per-window decode results (each in
// window-local time) into one transcript in absolute time. Each window's segment
// lines and words are offset by the window's start, then de-duplicated against the
// overlap by keeping only those whose absolute start falls in the window's CORE
// [startMS, startMS+stepMS); the final window keeps everything through the end.
// The result is a segment-formatted transcript string (the same `[mm:ss] text`
// shape a single decode returns) plus the flat, time-ordered word list.
//
// It is a pure function of its inputs so the windowing/merge logic is unit-tested
// without a live transcriber.
func MergeTranscriptWindows(windows []TranscriptWindow, stepMS int) (string, []model.TimedWord) {
	if stepMS <= 0 {
		stepMS = 1
	}
	// Collect whole SEGMENTS (a [mm:ss] line plus the words that fall in it) rather
	// than filtering lines and words separately: keeping a segment's text and word
	// timings together lets the overlap de-duplication drop both as a unit.
	var segs []mergedSegment
	for i, w := range windows {
		last := i == len(windows)-1
		// A window's core normally ends where the next scheduled window begins
		// (startMS+stepMS), so its overlap tail is dropped as the next window's
		// core re-decodes it. But `windows` holds only the windows that SURVIVED
		// (translateStructuredWindowed skips a window whose decode failed — silence,
		// music, a transient error), so the next surviving window may start LATER
		// than the scheduled one. In that case nothing re-decodes this window's
		// overlap tail, so extend the core to the actual next surviving window and
		// keep this window's real segments instead of silently dropping them.
		coreEnd := w.StartMS + stepMS
		if !last && windows[i+1].StartMS > coreEnd {
			coreEnd = windows[i+1].StartMS
		}
		for _, s := range windowSegments(w.Res, w.StartMS) {
			if s.startMS < w.StartMS {
				s.startMS = w.StartMS
			}
			if !last && s.startMS >= coreEnd {
				continue
			}
			segs = append(segs, s)
		}
	}
	sort.SliceStable(segs, func(a, b int) bool { return segs[a].startMS < segs[b].startMS })
	segs = dedupMergedSegments(segs)
	segs = groupMistimedSegments(segs)

	var lines []string
	var words []model.TimedWord
	for _, s := range segs {
		if strings.TrimSpace(s.body) == "" {
			continue
		}
		lines = append(lines, formatTimestampMarker(s.startMS)+" "+s.body)
		words = append(words, s.words...)
	}
	sort.SliceStable(words, func(a, b int) bool { return words[a].StartMS < words[b].StartMS })
	return strings.Join(lines, "\n"), words
}

// mergedSegment is one transcript segment in absolute time: its [mm:ss] start, the
// spoken text, and the per-word timings that fall within it. Carrying words with
// their segment lets de-duplication move text and timing together.
type mergedSegment struct {
	startMS int
	body    string
	words   []model.TimedWord
}

// windowSegments splits one window's decode result into absolute-time segments,
// pairing each timestamped line with the words whose (window-local) start falls in
// its span. Timestamps and word starts are offset by the window's start so the
// result is in absolute time. A leading run of un-timestamped text is attached to
// a synthetic segment at the window start.
func windowSegments(res model.TranscriptResult, offsetMS int) []mergedSegment {
	type lineT struct {
		start int
		body  string
	}
	var lns []lineT
	for _, raw := range strings.Split(res.Text, "\n") {
		t := strings.TrimSpace(raw)
		if t == "" {
			continue
		}
		st, body, ok := parseTranscriptTimestamp(t)
		if !ok {
			if len(lns) > 0 {
				lns[len(lns)-1].body = strings.TrimSpace(lns[len(lns)-1].body + " " + t)
			} else {
				lns = append(lns, lineT{start: 0, body: t})
			}
			continue
		}
		if strings.TrimSpace(body) == "" {
			continue
		}
		lns = append(lns, lineT{start: st, body: body})
	}
	if len(lns) == 0 {
		return nil
	}
	segs := make([]mergedSegment, len(lns))
	for i, l := range lns {
		segs[i] = mergedSegment{startMS: l.start + offsetMS, body: l.body}
	}
	// Assign each word to the last segment whose local start is <= the word's local
	// start (segments are in playback order).
	for _, wd := range res.Words {
		idx := 0
		for i := range lns {
			if lns[i].start <= wd.StartMS {
				idx = i
			} else {
				break
			}
		}
		segs[idx].words = append(segs[idx].words, model.TimedWord{
			Word: wd.Word, StartMS: wd.StartMS + offsetMS, EndMS: wd.EndMS + offsetMS,
		})
	}
	return segs
}

// dedupMergedSegments removes near-duplicate segments left by overlapping decode
// windows. Core-boundary de-duplication (in MergeTranscriptWindows) misses a
// sentence that the two windows time slightly differently, so its start straddles
// the boundary and both copies survive. Here a segment is dropped when it shares
// >= 0.75 of its words with a recent kept segment (within 8 s); the more complete
// wording — and its word timings — is retained, so the transcript never doubles a
// sentence.
func dedupMergedSegments(segs []mergedSegment) []mergedSegment {
	kept := make([]mergedSegment, 0, len(segs))
	for _, s := range segs {
		dup := false
		for j := len(kept) - 1; j >= 0 && j >= len(kept)-4; j-- {
			if s.startMS-kept[j].startMS > 8000 {
				break
			}
			if segmentWordOverlap(kept[j].body, s.body) >= 0.75 {
				if len(s.body) > len(kept[j].body) { // keep the fuller decode + its words
					kept[j].body = s.body
					kept[j].words = s.words
				}
				dup = true
				break
			}
		}
		if !dup {
			kept = append(kept, s)
		}
	}
	return kept
}

// segmentWordOverlap is the fraction of the smaller segment's distinct words that
// also appear in the other, comparing case- and punctuation-insensitively.
func segmentWordOverlap(a, b string) float64 {
	sa, sb := segmentWordSet(a), segmentWordSet(b)
	if len(sa) == 0 || len(sb) == 0 {
		return 0
	}
	common := 0
	for w := range sa {
		if sb[w] {
			common++
		}
	}
	den := len(sa)
	if len(sb) < den {
		den = len(sb)
	}
	return float64(common) / float64(den)
}

// mergeTargetCPS is the reading speed (characters per second) below which a
// segment's display span is considered adequate for its text; segments timed
// tighter than this are merged by groupMistimedSegments.
const mergeTargetCPS = 17.0

// groupMistimedSegments merges consecutive segments whose display span — the gap
// until the next segment starts — is too short to read the accumulated text at
// mergeTargetCPS. These are window-boundary timing artifacts: a segment carries a
// couple of seconds of speech but the next segment's start lands implausibly
// close, so a segment-timed export (reflow) would crush it into an unreadable
// sub-second cue. Merging gives the combined text the combined span, so reflow
// then splits it into legible, comfortably-paced cues. A genuinely long dense run
// still breaks into groups once the accumulated text passes a cap, and the group
// keeps the FIRST (spoken) start as its anchor so timing is preserved.
func groupMistimedSegments(segs []mergedSegment) []mergedSegment {
	const maxGroupChars = 300
	out := make([]mergedSegment, 0, len(segs))
	for i := 0; i < len(segs); {
		cur := segs[i]
		j := i + 1
		for j < len(segs) {
			spanToNext := segs[j].startMS - cur.startMS
			need := int(float64(len(cur.body)) / mergeTargetCPS * 1000)
			if spanToNext >= need || len(cur.body) > maxGroupChars {
				break
			}
			cur.body = strings.TrimSpace(cur.body + " " + segs[j].body)
			cur.words = append(cur.words, segs[j].words...)
			j++
		}
		out = append(out, cur)
		i = j
	}
	return out
}

func segmentWordSet(s string) map[string]bool {
	m := make(map[string]bool)
	for _, w := range strings.Fields(strings.ToLower(s)) {
		w = strings.Trim(w, ".,!?;:\"'()[]…»«")
		if w != "" {
			m[w] = true
		}
	}
	return m
}

// TranscriptWindowStarts returns the window start offsets (ms) that tile
// [0, totalMS) at stepMS. A trailing window shorter than overlapMS is dropped:
// such a stub is both unreliable to decode (a sub-second Whisper clip yields
// empty/hallucinated text, and avutil.ExtractSegment errors on an empty segment,
// which would fail the whole file) AND redundant — whenever the tail is below the
// overlap the preceding window's extent already reaches totalMS, so as the new
// last window it keeps the tail. Pure so the drop rule is unit-tested.
func TranscriptWindowStarts(totalMS, stepMS, overlapMS int) []int {
	if stepMS <= 0 {
		stepMS = 1
	}
	var starts []int
	for start := 0; start < totalMS; start += stepMS {
		starts = append(starts, start)
	}
	if n := len(starts); n >= 2 && totalMS-starts[n-1] < overlapMS {
		starts = starts[:n-1]
	}
	return starts
}

// DefaultSTTWindowMS is the decode window used when a plain transcription has to
// be windowed (issue #954). Ten minutes keeps one request comfortably inside every
// shipped STT payload cap (self-hosted whisper 50 MB, Mistral 20 MB, OpenAI 25 MB)
// at ordinary speech bitrates and inside the default 120 s request timeout, while
// staying long enough that window boundaries remain rare.
const DefaultSTTWindowMS = 10 * 60 * 1000

// minSTTWindowMS floors the window derived from a provider payload cap. A shorter
// window decodes badly (a very short clip makes Whisper hallucinate) and multiplies
// requests, so an extremely dense payload is windowed at the floor even when a
// window may still exceed the cap: the provider then reports its cap honestly
// instead of dir2mcp slicing the audio into rubble.
const minSTTWindowMS = 30 * 1000

// sttWindowCapHeadroomPct is the share of the provider cap a window may occupy.
// A re-encoded slice is only ROUGHLY proportional to its duration (container
// headers, a variable bitrate, a keyframe-aligned cut), so the window is sized
// against 80% of the cap rather than all of it.
const sttWindowCapHeadroomPct = 80

// STTWindowMS decides the decode window (ms) for one plain transcription: totalMS
// is the recording length, payloadBytes the extracted audio a single request would
// carry, and capBytes the per-request payload cap the provider declares (0 when it
// declares none). It returns 0 when the recording is sent as ONE request, which is
// the pre-#954 behaviour and stays the common case.
//
// Either rule triggers windowing:
//   - the payload exceeds the provider cap. This is the #954 failure: the request
//     is refused outright (WHISPER_FAILED "transcription input too large") and the
//     document lands at status=error, never transcribed.
//   - the recording runs longer than DefaultSTTWindowMS. One request for hours of
//     audio strains the request timeout, and a timeout loses the whole document
//     rather than one window.
//
// When the cap is known the window is shrunk further so a window's share of the
// payload fits inside it with headroom, because a 10-minute window of dense audio
// (uncompressed WAV) can exceed the cap on its own.
//
// Pure, so both trigger rules are unit-tested without a provider.
func STTWindowMS(totalMS, payloadBytes, capBytes int) int {
	if totalMS <= 0 {
		return 0 // duration unknown: there is nothing to schedule windows over
	}
	overCap := capBytes > 0 && payloadBytes > capBytes
	if !overCap && totalMS <= DefaultSTTWindowMS {
		return 0
	}
	windowMS := DefaultSTTWindowMS
	if capBytes > 0 && payloadBytes > 0 {
		budget := int64(capBytes) / 100 * sttWindowCapHeadroomPct
		// Project the default window's share of the payload and shrink the window
		// only when that share does not fit the budget. Comparing first is also what
		// keeps a very large configured cap (media.stt.max_payload_mb clamps to
		// math.MaxInt) from overflowing the multiplication below and collapsing the
		// window to the floor.
		projected := int64(payloadBytes) * int64(DefaultSTTWindowMS) / int64(totalMS)
		if projected > budget {
			windowMS = int(budget * int64(totalMS) / int64(payloadBytes))
		}
	}
	if windowMS < minSTTWindowMS {
		windowMS = minSTTWindowMS
	}
	return windowMS
}

// sttPayloadCapBytes reports the per-request payload cap the transcriber enforces,
// or 0 when it declares none. model.PayloadLimitedTranscriber is an OPTIONAL
// capability: a transcriber that does not implement it counts as uncapped and is
// windowed on the duration rule alone.
func sttPayloadCapBytes(stt model.Transcriber) int {
	limited, ok := stt.(model.PayloadLimitedTranscriber)
	if !ok {
		return 0
	}
	limit := limited.MaxTranscribePayloadBytes()
	if limit < 0 {
		return 0
	}
	return limit
}

// transcribeStructuredWindowed is the payload- and duration-aware wrapper around
// the plain transcription call (issue #954). A recording that fits in one request
// is still sent as one request, byte for byte as before. A recording that does not
// (its audio exceeds the provider's payload cap, or it runs longer than
// DefaultSTTWindowMS) is decoded in overlapping windows and merged back into one
// absolute-time transcript, so a long file yields a transcript instead of a
// document stamped status=error.
//
// The transcript cache key is deliberately unchanged: windowing is DERIVED from
// the media and the provider, not configured, and both paths return the same
// `[mm:ss] text` contract, so a cached transcript stays valid either way.
func (s *Service) transcribeStructuredWindowed(ctx context.Context, relPath string, content []byte) (string, []model.TimedWord, error) {
	if len(content) == 0 {
		return s.transcribeWith(ctx, s.transcriber, relPath, content)
	}
	capBytes := sttPayloadCapBytes(s.transcriber)
	tmpPath, cleanup, err := stageMediaTemp(content, filepath.Ext(relPath))
	if err != nil {
		// Staging exists only to SLICE the audio; failing it must not lose a
		// document that the single request would have transcribed.
		s.getLogger().Printf("windowed transcription %s: stage failed (%v); sending one request", relPath, err)
		return s.transcribeWith(ctx, s.transcriber, relPath, content)
	}
	defer cleanup()

	totalMS := s.probeStagedDurationMS(ctx, tmpPath)
	windowMS := STTWindowMS(totalMS, len(content), capBytes)
	if windowMS <= 0 {
		if capBytes > 0 && len(content) > capBytes {
			// Over the cap, but the duration probe failed (no ffprobe, undecodable
			// container), so there is nothing to schedule windows over. Say so: the
			// provider is about to refuse this request.
			s.getLogger().Printf("windowed transcription %s: payload %d bytes exceeds the provider cap %d bytes but the duration probe failed; sending one request",
				relPath, len(content), capBytes)
		}
		return s.transcribeWith(ctx, s.transcriber, relPath, content)
	}
	text, words, err := s.decodeWindowedTranscript(ctx, relPath, tmpPath, s.transcriber, totalMS, windowMS, "transcription")
	if errors.Is(err, avutil.ErrToolNotFound) {
		// ffmpeg is what SLICES the audio. Without it the recording cannot be
		// windowed, but it can still be sent whole, exactly as before #954: a
		// missing binary must not turn a transcript into a failed document. A
		// provider that then refuses the payload says so in its own error.
		s.getLogger().Printf("windowed transcription %s: ffmpeg is not installed; sending one request", relPath)
		return s.transcribeWith(ctx, s.transcriber, relPath, content)
	}
	return text, words, err
}

// translateStructuredWindowed is the media.translate.whisper_window_sec-aware
// wrapper around translateStructured. With no window configured (<= 0) it is a
// straight pass-through, so existing corpora are unchanged. With a window it
// decodes the audio in overlapping windows via Whisper's translate task and
// merges them, so timestamp drift cannot accumulate across a long recording.
// Windowing requires the structured (word-timing) transcriber; when the
// transcriber is text-only it transparently falls back to the single-pass decode.
func (s *Service) translateStructuredWindowed(ctx context.Context, doc model.Document, content []byte) (string, []model.TimedWord, error) {
	windowMS := s.cfg.MediaTranslateWhisperWindowSec * 1000
	_, structured := s.translateSTT.(model.StructuredTranscriber)
	if windowMS <= 0 || !structured || len(content) == 0 {
		return s.translateStructured(ctx, doc, content)
	}
	tmpPath, cleanup, err := stageMediaTemp(content, filepath.Ext(doc.RelPath))
	if err != nil {
		s.getLogger().Printf("windowed translate %s: stage failed (%v); decoding in one pass", doc.RelPath, err)
		return s.translateStructured(ctx, doc, content)
	}
	defer cleanup()

	// A recording that fits in a single window would be decoded once anyway, so skip
	// the slicing and the duplicate calls (timestamp drift accumulates over LENGTH,
	// so a sub-window recording has nothing to correct). A failed duration probe
	// lands here too and degrades to the single pass, because a missing ffprobe must
	// not fail a document that one request can still translate.
	totalMS := s.probeStagedDurationMS(ctx, tmpPath)
	if totalMS <= windowMS {
		return s.translateStructured(ctx, doc, content)
	}
	text, words, err := s.decodeWindowedTranscript(ctx, doc.RelPath, tmpPath, s.translateSTT, totalMS, windowMS, "translate")
	if errors.Is(err, avutil.ErrToolNotFound) {
		s.getLogger().Printf("windowed translate %s: ffmpeg is not installed; decoding in one pass", doc.RelPath)
		return s.translateStructured(ctx, doc, content)
	}
	return text, words, err
}

// decodeWindowedTranscript decodes the staged audio at tmpPath in overlapping
// windows through stt and merges them into one absolute-time transcript. label
// names the operation in logs ("transcription" or "translate"); it is the only
// difference between the two callers, because a translate decode returns the same
// segment/word shape a transcription does.
func (s *Service) decodeWindowedTranscript(ctx context.Context, relPath, tmpPath string, stt model.Transcriber, totalMS, windowMS int, label string) (string, []model.TimedWord, error) {
	overlapMS := TranscriptWindowOverlapMS(windowMS)
	stepMS := windowMS - overlapMS
	if stepMS <= 0 {
		stepMS = windowMS
	}
	windows, attempted, err := s.decodeTranscriptWindows(ctx, relPath, tmpPath, stt, totalMS, windowMS, stepMS, label)
	if err != nil {
		return "", nil, err
	}
	// One progress line per document: how much of the recording actually decoded.
	s.getLogger().Printf("windowed %s %s: %d/%d windows decoded (window %ds, overlap %ds, duration %ds)",
		label, relPath, len(windows), attempted, windowMS/1000, overlapMS/1000, totalMS/1000)
	text, words := MergeTranscriptWindows(windows, stepMS)
	return text, words, nil
}

// decodeTranscriptWindows extracts and decodes each scheduled window from the
// staged audio at tmpPath, returning the windows that decoded to content (in
// schedule order) and how many were attempted. It prefers the structured
// (word-timing) capability per window and degrades to text-only, exactly as a
// single-request decode does.
//
// A window whose decode fails (silence, music, a provider error that survived the
// client's retries) is skipped with a warning rather than aborting the whole
// recording, because a long recording routinely has stretches with nothing to
// transcribe. If EVERY attempted window fails that is systemic (provider down, bad
// credentials, unsupported media), so it is returned as an error and the document
// fails exactly as it does today.
func (s *Service) decodeTranscriptWindows(ctx context.Context, relPath, tmpPath string, stt model.Transcriber, totalMS, windowMS, stepMS int, label string) ([]TranscriptWindow, int, error) {
	var windows []TranscriptWindow
	var firstErr error
	attempted, failed := 0, 0
	for _, start := range TranscriptWindowStarts(totalMS, stepMS, TranscriptWindowOverlapMS(windowMS)) {
		end := start + windowMS
		if end > totalMS {
			end = totalMS
		}
		seg, err := s.extractMediaSegment(ctx, tmpPath, start, end)
		if err != nil {
			return nil, attempted, fmt.Errorf("extract %s window [%d,%d]ms: %w", label, start, end, err)
		}
		attempted++
		text, words, err := s.transcribeWith(ctx, stt, relPath, seg)
		if err != nil {
			failed++
			if firstErr == nil {
				firstErr = err
			}
			s.getLogger().Printf("windowed %s: skip window [%d,%d]ms of %s: %v", label, start, end, relPath, err)
			continue
		}
		windows = append(windows, TranscriptWindow{StartMS: start, Res: model.TranscriptResult{Text: text, Words: words}})
	}
	if attempted > 0 && failed == attempted {
		// Wrap the first failure so the caller's retryable/terminal classification
		// (a transient provider error leaves the document pending) survives the
		// aggregation instead of being flattened into an opaque string.
		return nil, attempted, fmt.Errorf("windowed %s %s: all %d windows failed: %w", label, relPath, failed, firstErr)
	}
	return windows, attempted, nil
}

// stageMediaTemp writes in-memory media to a temp file that keeps the original
// extension, because avutil probes and slices by PATH. The returned cleanup
// removes the file and is always safe to call.
func stageMediaTemp(content []byte, ext string) (string, func(), error) {
	noop := func() {}
	// CreateTemp treats "*" as the random-string placeholder and rejects path
	// separators, so an odd extension is dropped rather than corrupting the pattern.
	if strings.ContainsAny(ext, `*/\`) {
		ext = ""
	}
	tmp, err := os.CreateTemp("", "dir2mcp-window-*"+ext)
	if err != nil {
		return "", noop, fmt.Errorf("stage audio for windowed decode: %w", err)
	}
	path := tmp.Name()
	cleanup := func() { _ = os.Remove(path) }
	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		cleanup()
		return "", noop, fmt.Errorf("write staged audio: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return "", noop, fmt.Errorf("flush staged audio: %w", err)
	}
	return path, cleanup, nil
}

// probeStagedDurationMS probes the staged media's duration through
// ProbeDurationFunc (default avutil.Duration, ffprobe). It reports 0 instead of an
// error when the probe fails or the binary is absent: the caller then keeps the
// single-request path, so a missing ffprobe never costs a transcript.
func (s *Service) probeStagedDurationMS(ctx context.Context, path string) int {
	probe := s.ProbeDurationFunc
	if probe == nil {
		probe = avutil.Duration
	}
	dur, err := probe(ctx, path)
	if err != nil || dur <= 0 {
		return 0
	}
	return int(dur.Milliseconds())
}

// extractMediaSegment slices [startMS,endMS) out of the staged media through
// ExtractSegmentFunc (default avutil.ExtractSegment, ffmpeg).
func (s *Service) extractMediaSegment(ctx context.Context, path string, startMS, endMS int) ([]byte, error) {
	extract := s.ExtractSegmentFunc
	if extract == nil {
		extract = avutil.ExtractSegment
	}
	return extract(ctx, path, startMS, endMS)
}

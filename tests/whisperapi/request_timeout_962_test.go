package tests

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dirstral/dir2mcp/internal/model"
	"github.com/dirstral/dir2mcp/internal/providerhttp"
	"github.com/dirstral/dir2mcp/internal/whisperapi"
)

// slowWhisperServer answers /v1/audio/transcriptions after delay and counts the
// requests it received, so a test can prove both the timeout and how often the
// client came back for another decode.
func slowWhisperServer(t *testing.T, delay time.Duration) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	var calls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		select {
		case <-time.After(delay):
		case <-r.Context().Done():
			// The client hung up. A real STT server keeps decoding here; the test
			// only needs to stop the handler.
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(jsonSegments))
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

// TestRequestTimeoutForAudio pins the derived timeout: 10x the audio a request
// carries, with the old 120 s constant as the FLOOR and a six-hour ceiling.
//
// The measured numbers behind the factor (issue #962, an L40S running
// faster-whisper large-v3): a 10-minute window decodes in 34 s of Russian or
// Ukrainian speech and in 110 to 140 s of Kyrgyz. The shipped 120 s constant sat
// between those two, so the harder language timed out.
//
// Mutants killed: dropping the floor (a short clip would get a sub-second
// timeout), dropping the factor (the 10-minute window would stay at 120 s), and
// an unguarded multiplication (a huge duration would overflow to a negative
// timeout, which means "no timeout at all" to net/http).
func TestRequestTimeoutForAudio(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		audioMS int
		want    time.Duration
	}{
		{"unknown duration keeps the floor", 0, 120 * time.Second},
		{"negative duration keeps the floor", -1, 120 * time.Second},
		{"a short clip keeps the floor", 5 * 1000, 120 * time.Second},
		{"the floor holds right up to 12 s of audio", 12 * 1000, 120 * time.Second},
		{"a 60 s window clears the measured Kyrgyz rate", 60 * 1000, 600 * time.Second},
		{"the 10-minute STT window", 10 * 60 * 1000, 100 * time.Minute},
		{"an absurd duration is capped, never negative", 1 << 40, 6 * time.Hour},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := whisperapi.RequestTimeoutForAudio(tc.audioMS)
			if got != tc.want {
				t.Errorf("RequestTimeoutForAudio(%d) = %v, want %v", tc.audioMS, got, tc.want)
			}
			if got <= 0 {
				t.Errorf("RequestTimeoutForAudio(%d) = %v, which net/http reads as no timeout at all", tc.audioMS, got)
			}
		})
	}
}

// TestForAudioDuration_WidensTheRequestTimeout is the fix itself: a decode that
// runs past the client's configured timeout still completes, because the request
// carrying a window of audio is given a timeout sized to that window.
//
// The server here is 200 ms slow against a 50 ms client timeout; the shapes are
// the measured ones (a 140 s decode against a 120 s timeout) scaled down so the
// test is fast.
//
// Mutant killed: ForAudioDuration returning the receiver unchanged (the request
// is cut off and the window becomes a hole in the transcript).
func TestForAudioDuration_WidensTheRequestTimeout(t *testing.T) {
	t.Parallel()
	srv, calls := slowWhisperServer(t, 200*time.Millisecond)

	c := newClient(srv.URL, "")
	c.HTTPClient = providerhttp.NewClient(50 * time.Millisecond)

	sized, ok := model.TranscriberForAudioDuration(c, 10*60*1000).(model.StructuredTranscriber)
	if !ok {
		t.Fatalf("the sized transcriber lost the structured (word-timing) capability")
	}
	res, err := sized.TranscribeStructured(context.Background(), "talk.m4a", []byte("audio"))
	if err != nil {
		t.Fatalf("a 10-minute window must survive a decode slower than the configured timeout: %v", err)
	}
	if !strings.Contains(res.Text, "hello there") {
		t.Errorf("transcript = %q, want the server's segments", res.Text)
	}
	if n := calls.Load(); n != 1 {
		t.Errorf("server saw %d requests, want exactly 1", n)
	}
	// The receiver must not be mutated: one client serves concurrent documents.
	if c.HTTPClient.Timeout != 50*time.Millisecond {
		t.Errorf("ForAudioDuration mutated the shared client: timeout is now %v", c.HTTPClient.Timeout)
	}
}

// TestForAudioDuration_ExplicitTimeoutWins pins the override contract of #511:
// an operator who set media.stt.request_timeout_sec gets exactly that number,
// and the duration-derived timeout never overrules it, in either direction.
//
// Mutant killed: deriving the timeout unconditionally (the operator's setting
// would be silently replaced by 10x the window).
func TestForAudioDuration_ExplicitTimeoutWins(t *testing.T) {
	t.Parallel()
	srv, _ := slowWhisperServer(t, 200*time.Millisecond)

	c := newClient(srv.URL, "")
	c.HTTPClient = providerhttp.NewClient(50 * time.Millisecond)
	c.RequestTimeout = 50 * time.Millisecond // what applyWhisperLimits records

	sized := model.TranscriberForAudioDuration(c, 10*60*1000)
	if sized != model.Transcriber(c) {
		t.Fatalf("an explicit media.stt.request_timeout_sec must be used as is, got a re-sized client")
	}
	if _, err := c.Transcribe(context.Background(), "talk.m4a", []byte("audio")); err == nil {
		t.Fatal("the explicit 50 ms timeout must still cut off a 200 ms decode")
	}
}

// TestForAudioDuration_NeverShortensTheTimeout guards the other direction: a
// client already configured to wait longer than the derived value keeps its own
// timeout, and a client deliberately left unbounded stays unbounded.
func TestForAudioDuration_NeverShortensTheTimeout(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		timeout time.Duration
	}{
		{"a longer configured timeout is kept", 8 * time.Hour},
		{"an unbounded client stays unbounded", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newClient("http://127.0.0.1:1", "")
			c.HTTPClient = providerhttp.NewClient(tc.timeout)
			if got := model.TranscriberForAudioDuration(c, 10*60*1000); got != model.Transcriber(c) {
				t.Errorf("timeout %v was re-sized down to the derived value", tc.timeout)
			}
		})
	}
}

// TestTranscribeTimeout_NamesItselfAndIsNotRetried covers the two reporting
// halves of #962. A timed-out request must say it TIMED OUT and name the knob
// that changes it (the plain "transcription request failed" read like a refusal),
// and it must not be retried: the server is still decoding the same audio, so a
// retry queues a second decode behind the first and spends the GPU minute again.
//
// Mutants killed: the generic failure message (no timeout, no knob), and
// Retryable: true (the server would see three decodes of one window).
func TestTranscribeTimeout_NamesItselfAndIsNotRetried(t *testing.T) {
	t.Parallel()
	srv, calls := slowWhisperServer(t, 500*time.Millisecond)

	c := newClient(srv.URL, "")
	c.HTTPClient = providerhttp.NewClient(50 * time.Millisecond)
	c.RequestTimeout = 50 * time.Millisecond // explicit, so nothing widens it

	_, err := c.Transcribe(context.Background(), "talk.m4a", []byte("audio"))
	if err == nil {
		t.Fatal("a decode slower than the timeout must fail")
	}
	var provErr *model.ProviderError
	if !errors.As(err, &provErr) {
		t.Fatalf("error %v is not a *model.ProviderError", err)
	}
	if provErr.Code != "WHISPER_FAILED" {
		t.Errorf("code = %q, want the unchanged WHISPER_FAILED", provErr.Code)
	}
	if !strings.Contains(provErr.Message, "timed out") {
		t.Errorf("message %q does not say the request timed out", provErr.Message)
	}
	if !strings.Contains(provErr.Message, "media.stt.request_timeout_sec") {
		t.Errorf("message %q does not name the knob that changes the timeout", provErr.Message)
	}
	if provErr.Retryable {
		t.Error("a timed-out decode must not be retried: the server is still decoding it")
	}
	if n := calls.Load(); n != 1 {
		t.Errorf("server saw %d decodes of one window, want exactly 1", n)
	}
}

// TestTranscribeTimeout_CallerCancellationIsNotReportedAsATimeout keeps the
// timeout message honest: a caller that cancels (daemon shutdown, a cancelled
// reindex) is not the provider being slow, so it must not be blamed on
// media.stt.request_timeout_sec.
func TestTranscribeTimeout_CallerCancellationIsNotReportedAsATimeout(t *testing.T) {
	t.Parallel()
	srv, _ := slowWhisperServer(t, 500*time.Millisecond)

	c := newClient(srv.URL, "")
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	_, err := c.Transcribe(ctx, "talk.m4a", []byte("audio"))
	if err == nil {
		t.Fatal("a cancelled call must fail")
	}
	if strings.Contains(err.Error(), "media.stt.request_timeout_sec") {
		t.Errorf("a caller cancellation was reported as a provider timeout: %v", err)
	}
}

// dialTimeoutError is a connect timeout: a net.Error that reports Timeout() but
// never reached the server.
type dialTimeoutError struct{}

func (dialTimeoutError) Error() string   { return "i/o timeout" }
func (dialTimeoutError) Timeout() bool   { return true }
func (dialTimeoutError) Temporary() bool { return true }

// dialTimeoutTransport fails every round trip the way a blackholed endpoint
// does, and counts the attempts.
type dialTimeoutTransport struct{ calls atomic.Int64 }

func (d *dialTimeoutTransport) RoundTrip(*http.Request) (*http.Response, error) {
	d.calls.Add(1)
	return nil, &net.OpError{Op: "dial", Net: "tcp", Err: dialTimeoutError{}}
}

// TestDialTimeoutStaysRetryable keeps the new classification narrow: only a
// server that took too long to ANSWER is treated as still decoding. A connection
// that was never established is an ordinary network failure, so it keeps the
// retryable classification it always had and the client still retries it.
func TestDialTimeoutStaysRetryable(t *testing.T) {
	t.Parallel()
	transport := &dialTimeoutTransport{}
	c := newClient("http://127.0.0.1:1", "")
	c.HTTPClient = &http.Client{Transport: transport, Timeout: time.Minute}

	_, err := c.Transcribe(context.Background(), "talk.m4a", []byte("audio"))
	if err == nil {
		t.Fatal("an unreachable endpoint must fail")
	}
	if strings.Contains(err.Error(), "timed out") {
		t.Errorf("a connect failure was reported as a decode timeout: %v", err)
	}
	if n := transport.calls.Load(); n < 2 {
		t.Errorf("the client made %d attempts, want a retried network failure", n)
	}
}

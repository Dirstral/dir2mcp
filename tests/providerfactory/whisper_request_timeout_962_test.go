package tests

import (
	"net/http"
	"testing"
	"time"

	"github.com/dirstral/dir2mcp/internal/model"
	"github.com/dirstral/dir2mcp/internal/provider"
	"github.com/dirstral/dir2mcp/internal/providerfactory"
	"github.com/dirstral/dir2mcp/internal/whisperapi"
)

// whisperClient builds the whisper transcriber for a profile.
func whisperClient(t *testing.T, p provider.Profile) *whisperapi.Client {
	t.Helper()
	tr, err := providerfactory.Transcriber(p)
	if err != nil {
		t.Fatalf("Transcriber(whisper): %v", err)
	}
	c, ok := tr.(*whisperapi.Client)
	if !ok {
		t.Fatalf("whisper Transcriber is %T, want *whisperapi.Client", tr)
	}
	return c
}

// TestWhisperRequestTimeoutSizing checks the two halves of the #962 contract
// where config meets the client: with media.stt.request_timeout_sec set, the
// operator's number is used as is for every request; with it unset, each request
// gets a timeout derived from the audio it carries.
//
// Mutant killed: applyWhisperLimits setting HTTPClient.Timeout without recording
// that the operator chose it (the derived timeout would then silently replace an
// explicit setting).
func TestWhisperRequestTimeoutSizing(t *testing.T) {
	const windowMS = 10 * 60 * 1000

	t.Run("an explicit timeout is used for every request", func(t *testing.T) {
		p := prof(provider.KindWhisper)
		p.STTRequestTimeoutSec = 1800
		c := whisperClient(t, p)
		if want := 1800 * time.Second; c.RequestTimeout != want {
			t.Errorf("RequestTimeout = %v, want the operator's %v", c.RequestTimeout, want)
		}
		if got := model.TranscriberForAudioDuration(c, windowMS); got != model.Transcriber(c) {
			t.Error("the derived timeout overruled an explicit media.stt.request_timeout_sec")
		}
	})

	t.Run("no explicit timeout derives one per request", func(t *testing.T) {
		c := whisperClient(t, prof(provider.KindWhisper))
		if c.RequestTimeout != 0 {
			t.Errorf("RequestTimeout = %v, want 0 (nothing configured)", c.RequestTimeout)
		}
		sized, ok := model.TranscriberForAudioDuration(c, windowMS).(*whisperapi.Client)
		if !ok {
			t.Fatal("the sized transcriber is not a whisper client")
		}
		want := whisperapi.RequestTimeoutForAudio(windowMS)
		if sized.HTTPClient.Timeout != want {
			t.Errorf("a %d ms window got a %v timeout, want %v", windowMS, sized.HTTPClient.Timeout, want)
		}
		if sized == c {
			t.Error("the shared client was re-sized in place; one client serves concurrent documents")
		}
		if c.HTTPClient.Timeout == want {
			t.Error("sizing mutated the shared client's timeout")
		}
		if sized.HTTPClient.CheckRedirect == nil {
			t.Error("the sized client lost the providerhttp redirect policy")
		}
		if sized.HTTPClient.Transport != http.RoundTripper(c.HTTPClient.Transport) {
			t.Error("the sized client lost the shared transport, so it also lost the connection pool")
		}
	})
}

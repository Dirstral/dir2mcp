package test

import (
	"context"
	"strings"
	"testing"

	"dir2mcp/internal/dirstral/tempest"
)

func TestTempestRunFailsFastWithoutAudioPrereqs(t *testing.T) {
	t.Setenv("ELEVENLABS_API_KEY", "")

	err := tempest.Run(context.Background(), tempest.Options{Mute: true})
	if err == nil {
		t.Fatal("expected tempest run to fail without local audio prerequisites")
	}

	msg := strings.ToLower(err.Error())
	if !strings.Contains(msg, "ffmpeg is required") && !strings.Contains(msg, "elevenlabs_api_key is required") {
		t.Fatalf("unexpected tempest preflight error: %v", err)
	}
}

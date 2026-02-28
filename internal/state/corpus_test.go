package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Dirstral/dir2mcp/internal/config"
)

func TestWriteCorpusJSON_InitialCorpus_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	jobID := "job_test-123"
	corpus := InitialCorpus("/abs/root", jobID, &cfg)

	if err := WriteCorpusJSON(dir, corpus); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "corpus.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var read CorpusJSON
	if err := json.Unmarshal(data, &read); err != nil {
		t.Fatal(err)
	}
	if read.Root != "/abs/root" {
		t.Errorf("root: got %q", read.Root)
	}
	if read.Indexing.JobID != jobID {
		t.Errorf("job_id: got %q", read.Indexing.JobID)
	}
	if read.Indexing.Mode != "incremental" {
		t.Errorf("mode: got %q", read.Indexing.Mode)
	}
	if read.Indexing.Running != true {
		t.Errorf("running: got %v", read.Indexing.Running)
	}
	if read.Models.EmbedText != cfg.Mistral.EmbedTextModel {
		t.Errorf("embed_text: got %q", read.Models.EmbedText)
	}
	if read.Models.Chat != cfg.Mistral.ChatModel {
		t.Errorf("chat: got %q", read.Models.Chat)
	}
}

func TestInitialCorpus_STTProvider(t *testing.T) {
	cfg := config.Default()
	cfg.STT.Provider = "elevenlabs"
	corpus := InitialCorpus("/root", "job_1", &cfg)
	if corpus.Models.STTProvider != "elevenlabs" {
		t.Errorf("stt_provider: got %q", corpus.Models.STTProvider)
	}
	if corpus.Models.STTModel != cfg.STT.ElevenLabs.Model {
		t.Errorf("stt_model (elevenlabs): got %q", corpus.Models.STTModel)
	}
}

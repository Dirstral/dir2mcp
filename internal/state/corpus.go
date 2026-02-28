package state

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/Dirstral/dir2mcp/internal/config"
)

// CorpusJSON is the schema written to corpus.json (SPEC §4.4).
type CorpusJSON struct {
	Root     string         `json:"root"`
	Profile  CorpusProfile  `json:"profile,omitempty"`
	Models   CorpusModels   `json:"models"`
	Indexing CorpusIndexing `json:"indexing"`
}

// CorpusProfile holds doc counts and code ratio.
type CorpusProfile struct {
	DocCounts map[string]int `json:"doc_counts,omitempty"`
	CodeRatio float64        `json:"code_ratio,omitempty"`
}

// CorpusModels holds model names from config.
type CorpusModels struct {
	EmbedText   string `json:"embed_text"`
	EmbedCode   string `json:"embed_code"`
	OCR         string `json:"ocr"`
	STTProvider string `json:"stt_provider"`
	STTModel    string `json:"stt_model"`
	Chat        string `json:"chat"`
}

// CorpusIndexing holds job and progress (SPEC §4.4, §15.6).
type CorpusIndexing struct {
	JobID          string `json:"job_id"`
	Running        bool   `json:"running"`
	Mode           string `json:"mode"` // incremental | full
	Scanned        int    `json:"scanned"`
	Indexed        int    `json:"indexed"`
	Skipped        int    `json:"skipped"`
	Deleted        int    `json:"deleted"`
	Representations int   `json:"representations"`
	ChunksTotal   int    `json:"chunks_total"`
	EmbeddedOk    int    `json:"embedded_ok"`
	Errors        int    `json:"errors"`
}

// WriteCorpusJSON writes corpus.json to stateDir.
func WriteCorpusJSON(stateDir string, c *CorpusJSON) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(stateDir, "corpus.json"), data, 0600)
}

// InitialCorpus returns a new CorpusJSON with zeros and mode "incremental", filled from cfg.
func InitialCorpus(rootDir, jobID string, cfg *config.Config) *CorpusJSON {
	sttModel := cfg.STT.Mistral.Model
	if cfg.STT.Provider == "elevenlabs" {
		sttModel = cfg.STT.ElevenLabs.Model
	}
	return &CorpusJSON{
		Root:    rootDir,
		Profile: CorpusProfile{DocCounts: map[string]int{}, CodeRatio: 0},
		Models: CorpusModels{
			EmbedText:   cfg.Mistral.EmbedTextModel,
			EmbedCode:   cfg.Mistral.EmbedCodeModel,
			OCR:         cfg.Mistral.OCRModel,
			STTProvider: cfg.STT.Provider,
			STTModel:    sttModel,
			Chat:        cfg.Mistral.ChatModel,
		},
		Indexing: CorpusIndexing{
			JobID:   jobID,
			Running: true,
			Mode:    "incremental",
		},
	}
}

package config

// Allowed enum values for config validation (SPEC §16).
var (
	IngestModePDF     = []string{"off", "ocr", "auto"}
	IngestModeImages  = []string{"off", "ocr_auto", "ocr_on"}
	IngestModeAudio   = []string{"off", "auto", "on"}
	IngestModeArchives = []string{"off", "shallow", "deep"}
	STTProviders      = []string{"mistral", "elevenlabs"}
	X402Modes         = []string{"off", "on", "required"}
	SecretsProviders  = []string{"auto", "keychain", "file", "env", "session"}
	SecurityAuthModes = []string{"auto", "none", "file"}
)

func stringIn(s string, allowed []string) bool {
	for _, a := range allowed {
		if s == a {
			return true
		}
	}
	return false
}

// Config holds the full resolved configuration.
// Precedence: CLI flags > env vars > .dir2mcp.yaml > defaults (SPEC §16.1).
// RootDir and StateDir are set at load time from Options; not in YAML.
type Config struct {
	RootDir  string `yaml:"-"` // Set from Options at load
	StateDir  string `yaml:"-"` // Set from Options at load
	Version  int     `yaml:"version"`
	Mistral  Mistral `yaml:"mistral"`
	RAG      RAG     `yaml:"rag"`
	Ingest   Ingest  `yaml:"ingest"`
	Chunking Chunking `yaml:"chunking"`
	STT      STT     `yaml:"stt"`
	X402     X402    `yaml:"x402"`
	Server   Server  `yaml:"server"`
	Secrets  Secrets `yaml:"secrets"`
	Security Security `yaml:"security"`
}

// Mistral holds API keys and model names for Mistral services.
type Mistral struct {
	APIKey         string `yaml:"api_key"`
	ChatModel      string `yaml:"chat_model"`
	EmbedTextModel string `yaml:"embed_text_model"`
	EmbedCodeModel string `yaml:"embed_code_model"`
	OCRModel       string `yaml:"ocr_model"`
}

// RAG holds retrieval-augmented generation settings.
type RAG struct {
	GenerateAnswer  bool   `yaml:"generate_answer"`
	KDefault        int    `yaml:"k_default"`
	SystemPrompt    string `yaml:"system_prompt"`
	MaxContextChars int    `yaml:"max_context_chars"`
	OversampleFactor int   `yaml:"oversample_factor"`
}

// Ingest holds file ingestion and modality settings.
type Ingest struct {
	Gitignore      bool        `yaml:"gitignore"`
	PDF            IngestMode  `yaml:"pdf"`
	Images         IngestMode  `yaml:"images"`
	Audio          IngestMode  `yaml:"audio"`
	Archives       IngestMode  `yaml:"archives"`
	FollowSymlinks bool        `yaml:"follow_symlinks"`
	MaxFileMB      int         `yaml:"max_file_mb"`
}

// IngestMode specifies how a modality (PDF, images, etc.) is processed.
type IngestMode struct {
	Mode string `yaml:"mode"` // Allowed: off, ocr, auto, deep, etc. (see IngestMode* constants)
}

// Chunking holds text chunking parameters.
type Chunking struct {
	MaxChars     int           `yaml:"max_chars"`
	OverlapChars int           `yaml:"overlap_chars"`
	MinChars     int           `yaml:"min_chars"`
	Code         ChunkingCode  `yaml:"code"`
	Transcript   ChunkingTranscript `yaml:"transcript"`
}

// ChunkingCode holds code-specific chunking parameters (max lines, overlap).
type ChunkingCode struct {
	MaxLines     int `yaml:"max_lines"`
	OverlapLines int `yaml:"overlap_lines"`
}

// ChunkingTranscript holds transcript/audio segment parameters (ms).
type ChunkingTranscript struct {
	SegmentMs int `yaml:"segment_ms"`
	OverlapMs int `yaml:"overlap_ms"`
}

// STT holds speech-to-text provider and model settings.
type STT struct {
	Provider   string      `yaml:"provider"` // mistral | elevenlabs
	Mistral    STTMistral  `yaml:"mistral"`
	ElevenLabs STTElevenLabs `yaml:"elevenlabs"`
}

// STTMistral holds Mistral STT model and options.
type STTMistral struct {
	APIKey     string `yaml:"api_key"`
	Model      string `yaml:"model"`
	Timestamps bool   `yaml:"timestamps"`
}

// STTElevenLabs holds ElevenLabs STT model and options.
type STTElevenLabs struct {
	APIKey     string `yaml:"api_key"`
	Model      string `yaml:"model"`
	Timestamps bool   `yaml:"timestamps"`
}

// X402 holds optional payment protocol settings.
type X402 struct {
	Enabled         bool           `yaml:"enabled"`
	Mode            string         `yaml:"mode"` // off | on | required
	FacilitatorURL  string         `yaml:"facilitator_url"`
	ResourceBaseURL string         `yaml:"resource_base_url"`
	RoutePolicy     X402RoutePolicy `yaml:"route_policy"`
	Bazaar          X402Bazaar     `yaml:"bazaar"`
}

// X402RoutePolicy holds per-route payment settings.
type X402RoutePolicy struct {
	ToolsCall X402ToolsCall `yaml:"tools_call"`
}

// X402ToolsCall holds tool-call payment settings.
type X402ToolsCall struct {
	Enabled bool   `yaml:"enabled"`
	Price   string `yaml:"price"`
	Network string `yaml:"network"`
	Scheme  string `yaml:"scheme"`
	Asset   string `yaml:"asset"`
	PayTo   string `yaml:"pay_to"`
}

// X402BazaarMetadata holds bazaar listing metadata.
type X402BazaarMetadata struct {
	Description string `yaml:"description"`
}

// X402Bazaar holds bazaar configuration.
type X402Bazaar struct {
	Enabled  bool              `yaml:"enabled"`
	Metadata X402BazaarMetadata `yaml:"metadata"`
}

// Server holds HTTP server and MCP path settings.
type Server struct {
	Listen          string    `yaml:"listen"`
	MCPPath         string    `yaml:"mcp_path"`
	ProtocolVersion string    `yaml:"protocol_version"`
	TLS             ServerTLS `yaml:"tls"`
	Public          bool      `yaml:"public"`
	Auth            string    `yaml:"-"` // set from CLI/state; not in YAML
}

// ServerTLS holds TLS certificate paths.
type ServerTLS struct {
	Enabled  bool   `yaml:"enabled"`
	CertFile string `yaml:"cert_file"`
	KeyFile  string `yaml:"key_file"`
}

// Secrets holds secret storage provider configuration.
type Secrets struct {
	Provider  string        `yaml:"provider"` // auto | keychain | file | env | session
	Keychain  SecretsKeychain `yaml:"keychain"`
	File      SecretsFile   `yaml:"file"`
}

// SecretsKeychain holds keychain provider settings.
type SecretsKeychain struct {
	Service string `yaml:"service"`
	Account string `yaml:"account"`
}

// SecretsFile holds file-based secret storage settings.
type SecretsFile struct {
	Path string `yaml:"path"`
	Mode string `yaml:"mode"`
}

// Security holds auth and CORS settings.
type Security struct {
	Auth            SecurityAuth `yaml:"auth"`
	AllowedOrigins  []string     `yaml:"allowed_origins"`
	PathExcludes    []string    `yaml:"path_excludes"`
	SecretPatterns  []string    `yaml:"secret_patterns"`
}

// SecurityAuth holds auth mode and token env name.
type SecurityAuth struct {
	Mode     string `yaml:"mode"`      // auto | none | file
	TokenFile string `yaml:"token_file"`
	TokenEnv  string `yaml:"token_env"`
}

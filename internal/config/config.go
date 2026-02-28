package config

// Config holds the full resolved configuration.
// Precedence: CLI flags > env vars > .dir2mcp.yaml > defaults (SPEC §16.1).
type Config struct {
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

type Mistral struct {
	APIKey         string `yaml:"api_key"`
	ChatModel      string `yaml:"chat_model"`
	EmbedTextModel string `yaml:"embed_text_model"`
	EmbedCodeModel string `yaml:"embed_code_model"`
	OCRModel       string `yaml:"ocr_model"`
}

type RAG struct {
	GenerateAnswer  bool   `yaml:"generate_answer"`
	KDefault        int    `yaml:"k_default"`
	SystemPrompt    string `yaml:"system_prompt"`
	MaxContextChars int    `yaml:"max_context_chars"`
	OversampleFactor int   `yaml:"oversample_factor"`
}

type Ingest struct {
	Gitignore      bool        `yaml:"gitignore"`
	PDF            IngestMode  `yaml:"pdf"`
	Images         IngestMode  `yaml:"images"`
	Audio          IngestMode  `yaml:"audio"`
	Archives       IngestMode  `yaml:"archives"`
	FollowSymlinks bool        `yaml:"follow_symlinks"`
	MaxFileMB      int         `yaml:"max_file_mb"`
}

type IngestMode struct {
	Mode string `yaml:"mode"` // e.g. "ocr", "off", "auto", "deep"
}

type Chunking struct {
	MaxChars     int           `yaml:"max_chars"`
	OverlapChars int           `yaml:"overlap_chars"`
	MinChars     int           `yaml:"min_chars"`
	Code         ChunkingCode  `yaml:"code"`
	Transcript   ChunkingTranscript `yaml:"transcript"`
}

type ChunkingCode struct {
	MaxLines     int `yaml:"max_lines"`
	OverlapLines int `yaml:"overlap_lines"`
}

type ChunkingTranscript struct {
	SegmentMs int `yaml:"segment_ms"`
	OverlapMs int `yaml:"overlap_ms"`
}

type STT struct {
	Provider   string      `yaml:"provider"` // mistral | elevenlabs
	Mistral    STTMistral  `yaml:"mistral"`
	ElevenLabs STTElevenLabs `yaml:"elevenlabs"`
}

type STTMistral struct {
	APIKey     string `yaml:"api_key"`
	Model      string `yaml:"model"`
	Timestamps bool   `yaml:"timestamps"`
}

type STTElevenLabs struct {
	APIKey     string `yaml:"api_key"`
	Model      string `yaml:"model"`
	Timestamps bool   `yaml:"timestamps"`
}

type X402 struct {
	Enabled         bool           `yaml:"enabled"`
	Mode            string         `yaml:"mode"` // off | on | required
	FacilitatorURL  string         `yaml:"facilitator_url"`
	ResourceBaseURL string         `yaml:"resource_base_url"`
	RoutePolicy     X402RoutePolicy `yaml:"route_policy"`
	Bazaar          X402Bazaar     `yaml:"bazaar"`
}

type X402RoutePolicy struct {
	ToolsCall X402ToolsCall `yaml:"tools_call"`
}

type X402ToolsCall struct {
	Enabled bool   `yaml:"enabled"`
	Price   string `yaml:"price"`
	Network string `yaml:"network"`
	Scheme  string `yaml:"scheme"`
	Asset   string `yaml:"asset"`
	PayTo   string `yaml:"pay_to"`
}

type X402Bazaar struct {
	Enabled  bool   `yaml:"enabled"`
	Metadata struct {
		Description string `yaml:"description"`
	} `yaml:"metadata"`
}

type Server struct {
	Listen          string    `yaml:"listen"`
	MCPPath         string    `yaml:"mcp_path"`
	ProtocolVersion string    `yaml:"protocol_version"`
	TLS             ServerTLS `yaml:"tls"`
	Public          bool      `yaml:"public"`
	Auth            string    `yaml:"-"` // set from CLI/state; not in YAML
}

type ServerTLS struct {
	Enabled  bool   `yaml:"enabled"`
	CertFile string `yaml:"cert_file"`
	KeyFile  string `yaml:"key_file"`
}

type Secrets struct {
	Provider  string        `yaml:"provider"` // auto | keychain | file | env | session
	Keychain  SecretsKeychain `yaml:"keychain"`
	File      SecretsFile   `yaml:"file"`
}

type SecretsKeychain struct {
	Service string `yaml:"service"`
	Account string `yaml:"account"`
}

type SecretsFile struct {
	Path string `yaml:"path"`
	Mode string `yaml:"mode"`
}

type Security struct {
	Auth            SecurityAuth `yaml:"auth"`
	AllowedOrigins  []string     `yaml:"allowed_origins"`
	PathExcludes    []string    `yaml:"path_excludes"`
	SecretPatterns  []string    `yaml:"secret_patterns"`
}

type SecurityAuth struct {
	Mode     string `yaml:"mode"`      // auto | none | file
	TokenFile string `yaml:"token_file"`
	TokenEnv  string `yaml:"token_env"`
}

package config

// Default returns a config with SPEC §16.2 default values.
func Default() Config {
	return Config{
		Version: 1,
		Mistral: Mistral{
			APIKey:         "",
			ChatModel:      "mistral-small-2506",
			EmbedTextModel: "mistral-embed",
			EmbedCodeModel: "codestral-embed",
			OCRModel:       "mistral-ocr-latest",
		},
		RAG: RAG{
			GenerateAnswer:   true,
			KDefault:         10,
			SystemPrompt:      "You are a retrieval-augmented assistant.\nUse citations and never invent sources.",
			MaxContextChars:  20000,
			OversampleFactor: 5,
		},
		Ingest: Ingest{
			Gitignore:      true,
			PDF:            IngestMode{Mode: "ocr"},
			Images:         IngestMode{Mode: "ocr_auto"},
			Audio:          IngestMode{Mode: "auto"},
			Archives:       IngestMode{Mode: "deep"},
			FollowSymlinks: false,
			MaxFileMB:      20,
		},
		Chunking: Chunking{
			MaxChars:     2500,
			OverlapChars: 250,
			MinChars:     200,
			Code:         ChunkingCode{MaxLines: 200, OverlapLines: 30},
			Transcript:   ChunkingTranscript{SegmentMs: 30000, OverlapMs: 5000},
		},
		STT: STT{
			Provider: "mistral",
			Mistral:  STTMistral{Model: "voxtral-mini-latest", Timestamps: true},
			ElevenLabs: STTElevenLabs{Model: "scribe", Timestamps: true},
		},
		X402: X402{
			Enabled: false,
			Mode:    "off",
			RoutePolicy: X402RoutePolicy{
				ToolsCall: X402ToolsCall{
					Enabled: false,
					Price:   "0.001",
					Network: "eip155:8453",
					Scheme:  "exact",
				},
			},
		},
		Server: Server{
			Listen:          "127.0.0.1:0",
			MCPPath:         "/mcp",
			ProtocolVersion: "2025-11-25",
		},
		Secrets: Secrets{
			Provider: "auto",
			Keychain: SecretsKeychain{Service: "dir2mcp", Account: "default"},
			File:     SecretsFile{Path: ".dir2mcp/secret.env", Mode: "0600"},
		},
		Security: Security{
			Auth: SecurityAuth{
				Mode:     "auto",
				TokenEnv: "DIR2MCP_AUTH_TOKEN",
			},
			AllowedOrigins: []string{"http://localhost", "http://127.0.0.1"},
			PathExcludes: []string{
				"**/.git/**", "**/node_modules/**", "**/.dir2mcp/**",
				"**/.env", "**/*.pem", "**/*.key", "**/id_rsa",
			},
			SecretPatterns: []string{
				`AKIA[0-9A-Z]{16}`,
				`(?i)aws(.{0,20})?secret|([0-9a-zA-Z/+=]{40})`,
				`(?i)(?:authorization\s*[:=]\s*bearer\s+|(?:access|id|refresh)_token\s*[:=]\s*)[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}`,
				`(?i)token\s*[:=]\s*[A-Za-z0-9_.-]{20,}`,
				`sk_[a-z0-9]{32}|api_[A-Za-z0-9]{32}`,
			},
		},
	}
}

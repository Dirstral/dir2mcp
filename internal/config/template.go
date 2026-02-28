package config

// DefaultYAML is the template written by "dir2mcp config init" (SPEC §16.2).
// Placeholders like ${MISTRAL_API_KEY} are resolved from env at load time.
const DefaultYAML = `version: 1

mistral:
  api_key: ${MISTRAL_API_KEY}
  chat_model: mistral-small-2506
  embed_text_model: mistral-embed
  embed_code_model: codestral-embed
  ocr_model: mistral-ocr-latest

rag:
  generate_answer: true
  k_default: 10
  system_prompt: |
    You are a retrieval-augmented assistant.
    Use citations and never invent sources.
  max_context_chars: 20000
  oversample_factor: 5

ingest:
  gitignore: true
  pdf:
    mode: ocr
  images:
    mode: ocr_auto
  audio:
    mode: auto
    cache: true
  archives:
    mode: deep
  follow_symlinks: false
  max_file_mb: 20

chunking:
  max_chars: 2500
  overlap_chars: 250
  min_chars: 200
  code:
    max_lines: 200
    overlap_lines: 30
  transcript:
    segment_ms: 30000
    overlap_ms: 5000

stt:
  provider: mistral
  mistral:
    api_key: ${MISTRAL_API_KEY}
    model: voxtral-mini-latest
    timestamps: true
  elevenlabs:
    api_key: ${ELEVENLABS_API_KEY}
    model: scribe
    timestamps: true

x402:
  enabled: false
  mode: off
  facilitator_url: ""
  resource_base_url: ""
  route_policy:
    tools_call:
      enabled: false
      price: "0.001"
      network: "eip155:8453"
      scheme: "exact"
      asset: ""
      pay_to: ""
  bazaar:
    enabled: false
    metadata:
      description: ""

server:
  listen: "127.0.0.1:0"
  mcp_path: "/mcp"
  protocol_version: "2025-11-25"
  tls:
    enabled: false
    cert_file: ""
    key_file: ""
  public: false

secrets:
  provider: auto
  keychain:
    service: "dir2mcp"
    account: "default"
  file:
    path: ".dir2mcp/secret.env"
    mode: "0600"

security:
  auth:
    mode: auto
    token_file: ""
    token_env: "DIR2MCP_AUTH_TOKEN"
  allowed_origins:
    - "http://localhost"
    - "http://127.0.0.1"
  path_excludes:
    - "**/.git/**"
    - "**/node_modules/**"
    - "**/.dir2mcp/**"
    - "**/.env"
    - "**/*.pem"
    - "**/*.key"
    - "**/id_rsa"
  secret_patterns:
    - 'AKIA[0-9A-Z]{16}'
    - '(?i)aws(.{0,20})?secret|([0-9a-zA-Z/+=]{40})'
    - '(?i)(?:authorization\s*[:=]\s*bearer\s+|(?:access|id|refresh)_token\s*[:=]\s*)[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}'
    - '(?i)token\s*[:=]\s*[A-Za-z0-9_.-]{20,}'
    - 'sk_[a-z0-9]{32}|api_[A-Za-z0-9]{32}'
`

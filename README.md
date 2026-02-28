# dir2mcp

`dir2mcp` is a deploy-first MCP server for private directory data.

## Current status

This repository is currently spec-first. The core implementation is a Go single binary.

## Build and run

**1. Install Go** (if needed, macOS):

```bash
brew install go
```

Or download from [go.dev/dl](https://go.dev/dl/).

**2. Build the binary:**

```bash
cd /path/to/dir2mcp
make build
```

Or: `go build -o dir2mcp ./cmd/dir2mcp/`

**3. Run:**

```bash
export MISTRAL_API_KEY=your_key_here
./dir2mcp up
```

## Documentation

- `VISION.md`: product vision, principles, use cases, and roadmap
- `SPEC.md`: output/integration spec, tool contracts, config, security, and x402 requirements
- `ECOSYSTEM.md`: discovery/trust/metering/payment ecosystem framing
- `x402-payment-adapter-spec.md`: facilitator adapter contract for optional x402 mode

## Hackathon sequencing

- Day 1: core MCP + indexing + retrieval + citations
- Day 2 (optional): native x402 request gating via facilitator integration

## Demo direction (current)

For the hackathon demo we are not building a custom web frontend.

- Voice UX is hosted by ElevenLabs Agents (talk-to page)
- `dir2mcp` remains the MCP knowledge server and tool provider
- Agent calls MCP tools over remote MCP (SSE/streamable HTTP)
- Optional: keep `ask_audio` for direct tool-level TTS experiments

## MCP setup by client

Recommended servers:

- `everything`: MCP protocol reference server
- `sequential-thinking`: structured planning helper
- `playwright`: browser automation for local checks
- `github`: GitHub MCP endpoint for issues/PR/repo workflows
- `context7`: up-to-date library/framework docs

### Codex

```bash
codex mcp add everything -- npx -y @modelcontextprotocol/server-everything
codex mcp add sequential-thinking -- npx -y @modelcontextprotocol/server-sequential-thinking
codex mcp add playwright -- npx -y @playwright/mcp
codex mcp add github --url https://api.githubcopilot.com/mcp/ --bearer-token-env-var GITHUB_PERSONAL_ACCESS_TOKEN
codex mcp add context7 -- npx -y @upstash/context7-mcp
```

### Claude Code

```bash
claude mcp add --transport stdio everything -- npx -y @modelcontextprotocol/server-everything
claude mcp add --transport stdio sequential-thinking -- npx -y @modelcontextprotocol/server-sequential-thinking
claude mcp add --transport stdio playwright -- npx -y @playwright/mcp
claude mcp add --transport stdio github -- npx -y @modelcontextprotocol/server-github
claude mcp add --transport stdio context7 -- npx -y @upstash/context7-mcp

# Verify
claude mcp list
```

### Mistral Vibe

```bash
mkdir -p ~/.vibe
touch ~/.vibe/config.toml

cat >> ~/.vibe/config.toml <<'EOCFG'
[[mcp_servers]]
name = "everything"
transport = "stdio"
command = "npx"
args = ["-y", "@modelcontextprotocol/server-everything"]

[[mcp_servers]]
name = "sequential-thinking"
transport = "stdio"
command = "npx"
args = ["-y", "@modelcontextprotocol/server-sequential-thinking"]

[[mcp_servers]]
name = "playwright"
transport = "stdio"
command = "npx"
args = ["-y", "@playwright/mcp"]

[[mcp_servers]]
name = "github"
transport = "stdio"
command = "npx"
args = ["-y", "@modelcontextprotocol/server-github"]

[[mcp_servers]]
name = "context7"
transport = "streamable-http"
url = "https://mcp.context7.com/mcp"
api_key_env = "CONTEXT7_API_KEY"
api_key_header = "Authorization"
api_key_format = "Bearer {token}"
EOCFG

# Reload Vibe after editing config
```

## Environment variables

```bash
export GITHUB_PERSONAL_ACCESS_TOKEN=your_pat_here
export CONTEXT7_API_KEY=your_context7_api_key
export MISTRAL_API_KEY=your_mistral_api_key
export MISTRAL_BASE_URL=https://api.mistral.ai
export ELEVENLABS_API_KEY=your_elevenlabs_api_key
export ELEVENLABS_BASE_URL=https://api.elevenlabs.io
export DIR2MCP_ALLOWED_ORIGINS=https://elevenlabs.io
```

ElevenLabs integrations should read `ELEVENLABS_API_KEY` and `ELEVENLABS_BASE_URL` from env-backed config, not hardcoded literals.

`DIR2MCP_ALLOWED_ORIGINS` appends extra allowed origins for browser requests while keeping localhost defaults (`http://localhost`, `http://127.0.0.1`) enabled.

## Local `.env` support

For local development, `dir2mcp` automatically reads `.env` and `.env.local` from the working directory.

Precedence:
- Existing shell environment variables win
- Then `.env.local`
- Then `.env`

Quick start:

```bash
cp .env.example .env
```

## Development checks

```bash
make fmt
make vet
make lint
make test
make check
```

Notes:
- `make lint` requires `golangci-lint` installed locally.
- CI runs lint + build + vet + test on pushes and PRs to `main`.

### Optional integration test (Mistral API)

Test organization:
- `tests/` contains black-box/integration-style tests and uses `package tests`.
- `internal/<pkg>/*_test.go` contains package-level white-box tests and may use unexported symbols.

By default, integration tests are skipped. To run the live Mistral embedding integration test:

```bash
RUN_INTEGRATION_TESTS=1 go test -v ./internal/mistral -run Integration
```

Required env vars:
- `MISTRAL_API_KEY`
- optional `MISTRAL_BASE_URL` (defaults to `https://api.mistral.ai`)

To run the live OCR integration test as well:

```bash
RUN_INTEGRATION_TESTS=1 \
MISTRAL_OCR_SAMPLE=/absolute/path/to/sample.pdf \
go test -v ./tests -run MistralOCR
```

`MISTRAL_OCR_SAMPLE` can be a local `.pdf`, `.png`, `.jpg`, or `.jpeg` file.

## Ingestion notes

- Incremental hashing:
  - Document-level: `content_hash` decides whether representation regeneration is needed.
  - `reindex` mode forces regeneration regardless of hash match.
- `raw_text` chunking defaults:
  - code: `200` lines with `30` line overlap
  - text/md/data/html: `2500` chars with `250` char overlap (`min 200` chars)
- Span persistence:
  - raw text chunks persist `lines` spans
  - OCR chunks persist `page` spans
- OCR cache:
  - OCR outputs are cached in `.dir2mcp/cache/ocr/<content-hash>.md`
  - cache lifecycle supports automatic TTL and max-size pruning when limits are configured
  - default behavior is unbounded (`ttl=0`, `maxBytes=0`)
  - currently, cache-policy values are set programmatically by the embedding runtime (no dedicated CLI/config keys are exposed yet)
  - repeat processing of unchanged OCR input reuses cache before provider calls
  - manual pruning can still be useful, but automatic pruning reclaims space when TTL or max-size limits are set
  - to check cache size: `du -sh .dir2mcp/cache/ocr/`
  - to clear the cache: `rm -rf .dir2mcp/cache/ocr/*` (cache will be rebuilt on next OCR operation)

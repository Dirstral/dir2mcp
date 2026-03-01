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

### ElevenLabs approval settings (hosted demo)

For predictable live-demo behavior, auto-approve only read-only knowledge tools:

- `dir2mcp.search`
- `dir2mcp.ask`
- `dir2mcp.ask_audio`
- `dir2mcp.open_file`
- `dir2mcp.list_files`
- `dir2mcp.stats`

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

## Public deployment hardening (Issue #59)

### `--public` behavior and auth requirements

- `./dir2mcp up --public` sets `public=true` and (when `--listen` is not provided) binds to `0.0.0.0:<port>`.
- `--public` with `--auth none` is blocked by default:
  - `ERROR: CONFIG_INVALID: --public requires auth. Use --auth auto or --force-insecure to override (unsafe).`
- `--auth auto` is the secure default for public exposure:
  - if `DIR2MCP_AUTH_TOKEN` is set, that token is used
  - otherwise a token is generated/stored at `.dir2mcp/secret.token`
- Allowed origins are deny-by-default except local development origins (`http://localhost`, `http://127.0.0.1`).
  Add hosted browser origins explicitly with `DIR2MCP_ALLOWED_ORIGINS` or `--allowed-origins`.

### Deployment pattern (Cloudflare Tunnel)

#### Cloudflare Tunnel

You can publish an HTTPS hostname through Cloudflare Tunnel while keeping MCP on a private local listener.

```yaml
tunnel: dir2mcp-prod
credentials-file: /etc/cloudflared/dir2mcp-prod.json
ingress:
  - hostname: mcp.example.com
    service: http://127.0.0.1:8080
  - service: http_status:404
```

Recommended hardening with tunnel deployments:

- keep MCP auth enabled (`--auth auto` or `--auth file:<path>`)
- keep origin allowlist minimal (`https://elevenlabs.io` plus your own hosted app domains)
- avoid `--force-insecure` for any internet-reachable endpoint

### Remote MCP verification checklist (local/reproducible)

Start server:

```bash
export MISTRAL_API_KEY=your_key_here
export DIR2MCP_ALLOWED_ORIGINS=https://elevenlabs.io
./dir2mcp up --public --auth auto --listen 0.0.0.0:8080
```

Prepare endpoint + token:

```bash
BASE_URL="https://mcp.example.com/mcp"
# read token, fail explicitly if file inaccessible
TOKEN="${DIR2MCP_AUTH_TOKEN:-$(cat .dir2mcp/secret.token 2>/dev/null)}"
if [ -z "$TOKEN" ]; then
  echo "error: could not read auth token (check .dir2mcp/secret.token or DIR2MCP_AUTH_TOKEN)" >&2
  exit 1
fi
```

1. Initialize and capture `MCP-Session-Id`:

```bash
INIT_HEADERS="$(mktemp)"
INIT_BODY="$(mktemp)"
# ensure temporary headers/body files are removed on exit or interrupt
trap 'rm -f "$INIT_HEADERS" "$INIT_BODY"' EXIT
curl -sS -D "$INIT_HEADERS" -o "$INIT_BODY" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $TOKEN" \
  -H "MCP-Protocol-Version: 2025-11-25" \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}' \
  "$BASE_URL" || {
    echo "error: initialize request failed" >&2
    cat "$INIT_HEADERS" >&2
    cat "$INIT_BODY" >&2
    exit 1
}

# HTTP header names are case-insensitive per RFC 7230.  The awk
# command lowercases each header line, matches a prefix of
# "mcp-session-id:", strips any CR characters from the second field,
# and then prints that field (the session ID) once found.
SESSION_ID="$(awk -F': ' 'tolower($0) ~ /^mcp-session-id:/{gsub("\r","",$2); print $2; exit}' "$INIT_HEADERS")"
if [ -z "$SESSION_ID" ]; then
  echo "error: could not extract MCP-Session-Id from initialization response" >&2
  cat "$INIT_HEADERS" >&2
  exit 1
fi
```

2. Verify tool discovery (`tools/list`):

```bash
curl -sS \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $TOKEN" \
  -H "MCP-Protocol-Version: 2025-11-25" \
  -H "MCP-Session-Id: $SESSION_ID" \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}' \
  "$BASE_URL"
```

Expected: JSON-RPC success response containing `dir2mcp.*` tool names.

3. Verify tool call success (`dir2mcp.stats`):

```bash
curl -sS \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $TOKEN" \
  -H "MCP-Protocol-Version: 2025-11-25" \
  -H "MCP-Session-Id: $SESSION_ID" \
  -d '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"dir2mcp.stats","arguments":{}}}' \
  "$BASE_URL"
```

Expected: JSON-RPC success response with `result` payload from `dir2mcp.stats`.

### Remote MCP verification checklist (ElevenLabs hosted)

1. Configure MCP server URL to your public HTTPS endpoint (including the exact MCP path, typically `/mcp`).
2. Configure `Authorization: Bearer <token>` in ElevenLabs MCP connector settings.
3. Ensure hosted origin(s) are allowlisted:
   - minimum: `DIR2MCP_ALLOWED_ORIGINS=https://elevenlabs.io`
4. From the ElevenLabs side, verify:
   - tool discovery succeeds
   - one tool call (for example `dir2mcp.stats`) succeeds
5. Check server output/logs for:
   - no `UNAUTHORIZED` errors
   - no `FORBIDDEN_ORIGIN` errors
   - successful MCP calls on the expected route path

### Troubleshooting

| Symptom | Likely cause | Fix |
|---|---|---|
| `401` with `UNAUTHORIZED` | Missing/invalid Bearer token | Send `Authorization: Bearer <token>`. If using `--auth auto`, verify `DIR2MCP_AUTH_TOKEN` or `.dir2mcp/secret.token`. |
| `403` with `FORBIDDEN_ORIGIN` | Hosted origin not allowlisted | Add origin via `DIR2MCP_ALLOWED_ORIGINS` or `--allowed-origins`, then restart server. |
| `404` / `405` / method mismatch | Wrong MCP path or HTTP method | Use `POST` against exact MCP route (`/mcp` unless overridden by `--mcp-path`). |
| Session-related request errors | Missing or stale `MCP-Session-Id` | Run `initialize` first, then pass returned `MCP-Session-Id` for subsequent calls. |
| Browser preflight appears successful but call still blocked | Disallowed origin receives no CORS allow-origin header | Check response headers; add exact origin to allowlist and retry. |

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

Performance benchmark (large corpus retrieval path):

```bash
make benchmark
```

The `benchmark` target is preferred for consistency with other checks like
`make fmt`/`make vet`/`make lint` and ensures the correct `go test`
invocation (`-bench BenchmarkSearchBothLargeCorpus -run '^$' ./internal/retrieval`).

Notes:
- `make lint` requires `golangci-lint` installed locally.
- CI runs lint + build + vet + test on pushes and PRs to `main`.

### Optional integration test (Mistral API)

Test organization:
- `tests/` contains black-box/integration-style tests and uses `package tests`.
- `internal/<pkg>/*_test.go` contains package-level white-box tests and may use unexported symbols.

By default, integration tests are skipped. To run the live Mistral embedding integration test:

```bash
RUN_INTEGRATION_TESTS=1 go test -v ./tests/mistral -run Embed_Integration
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

To run the live transcription integration test (use your own audio file for speech‑to‑text):

```bash
RUN_INTEGRATION_TESTS=1 \
MISTRAL_STT_SAMPLE=/absolute/path/to/sample.mp3 \
go test -v ./tests/mistral -run Transcribe_Integration
```

`MISTRAL_STT_SAMPLE` may point to any local audio file that the Mistral STT service accepts (e.g. `.mp3`, `.wav`, `.m4a` or other supported codecs); the example above shows an MP3 sample together with the `Transcribe_Integration` test name so you can see both the env var and test at once.

To run the live generation integration test:

```bash
RUN_INTEGRATION_TESTS=1 \
go test -v ./tests/mistral -run Generate_Integration
```

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
- Transcript cache:
  - audio transcript outputs are cached in `.dir2mcp/cache/transcribe/<content-hash>.txt`
  - cache misses invoke the transcription provider; cache hits reuse the normalized transcript text
  - the transcript cache shares the same TTL and maxBytes policy as the OCR cache. when either of those limits is configured, automatic pruning runs during normal cache access to evict stale or oversized entries. behavior and policy are otherwise identical to the OCR cache – there are no special differences.
  - as with OCR, the default values (`ttl=0`, `maxBytes=0`) mean the cache is effectively unbounded.
  - automatic pruning happens lazily when the cache is accessed and limits are exceeded; if you need to reclaim space immediately (for example before a large batch ingest) you can manually prune the cache by removing files under `.dir2mcp/cache/transcribe/`.
  - to check cache size: `du -sh .dir2mcp/cache/transcribe/`
  - to clear the cache (manual pruning): `rm -rf .dir2mcp/cache/transcribe/*` – the cache will be rebuilt on the next audio ingest

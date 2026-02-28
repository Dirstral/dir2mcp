# dir2mcp — 2-Day Hackathon Plan

## What We're Building

`dir2mcp` is a **Go single-binary CLI** that turns any directory into a standard **MCP (Model Context Protocol) Streamable HTTP server** in one command (`dir2mcp up`). It:

1. Scans & incrementally indexes a directory in the background
2. Normalizes non-text into searchable text: PDFs/images → OCR markdown (Mistral OCR), audio → transcripts (Mistral STT / ElevenLabs Scribe), structured docs → JSON extraction
3. Embeds everything via Mistral embeddings into two embedded HNSW vector indices (text + code), stored in `.dir2mcp/` alongside SQLite metadata
4. Serves a spec-compliant MCP server immediately (before indexing finishes), exposing tools: `search`, `ask`, `open_file`, `list_files`, `stats`, `transcribe`, `annotate`, `transcribe_and_ask`
5. Optionally gates tool calls via **x402 HTTP 402 payment protocol** with facilitator-backed settlement

**Pivot note (2026-02-28):**
- We are shelving custom Web UI scope for now (previous issue tracks: `#11`, `#18`).
- Demo path is now hosted ElevenLabs Agent + remote MCP integration into `dir2mcp`.

**Key Mistral integrations:**
- `mistral-embed` + `codestral-embed` — embeddings
- `mistral-ocr-latest` — PDFs/images
- `voxtral-mini-latest` — audio STT
- `mistral-small-2506` — RAG chat answers

---

## Team

| Person | Role | Strength |
|--------|------|----------|
| **Ali** | Lead / Glue / MCP Protocol | Full-stack Go, can unblock anyone |
| **Ark** | RAG Core | Embeddings, vector search, retrieval, answer generation |
| **Tia** | Backend / Data Pipeline | File ingestion, SQLite, Mistral API integrations |
| **Samet** | Demo UX / CLI | Hosted voice demo flow, CLI output, docs/demo polish |

---

## Architecture Overview

```
┌─────────────────────────────────────────────────────┐
│  CLI (cmd/)                    ← Samet + Ali         │
│  Config (YAML+env+flags)       ← Samet               │
├─────────────────────────────────────────────────────┤
│  MCP Streamable HTTP Server    ← Ali                 │
│  (JSON-RPC, sessions, auth,                          │
│   origin checks, x402 gating)                        │
├─────────────────────────────────────────────────────┤
│  Ingestion Pipeline            ← Tia                 │
│  (discovery, OCR, STT, chunk,                        │
│   hash-based incremental)                            │
├─────────────────────────────────────────────────────┤
│  SQLite metadata store         ← Tia                 │
│  (documents/reps/chunks/spans)                       │
├─────────────────────────────────────────────────────┤
│  HNSW vector indices           ← Ark                 │
│  (vectors_text.hnsw +                                │
│   vectors_code.hnsw)                                 │
├─────────────────────────────────────────────────────┤
│  RAG retrieval + answer gen    ← Ark                 │
│  (search, ask, citations)                            │
├─────────────────────────────────────────────────────┤
│  Mistral API client            ← Ark + Tia           │
│  (embed, OCR, STT, chat)                             │
├─────────────────────────────────────────────────────┤
│  Hosted voice demo ops         ← Samet               │
└─────────────────────────────────────────────────────┘
```

---

---

# DAY 1 — Core Pipeline + Working MCP Server

**Goal by end of Day 1:** `dir2mcp up` starts, scans a directory, indexes text+code files, embeds chunks, and serves a live MCP endpoint where an agent (or curl) can call `search`, `ask` (search_only mode), `open_file`, `list_files`, `stats`. OCR for PDFs should also be working. `ask` with full RAG answer generation completes on Day 2.

---

## Ali — MCP Server + Go Project Foundation

### Morning (9am–1pm)

**Task 1.1 — Go project scaffold and shared interfaces** (~1.5h)
- Initialize Go module, set up directory structure:
  ```
  cmd/dir2mcp/          ← CLI entrypoint
  internal/config/      ← config loading
  internal/ingest/      ← file discovery + pipeline
  internal/store/       ← SQLite operations
  internal/index/       ← HNSW wrapper
  internal/retrieval/   ← search/ask logic
  internal/mcp/         ← MCP HTTP server
  internal/mistral/     ← Mistral API client
  ```
- Define Go interfaces for `Store`, `Index`, `Retriever`, `Ingestor` that each team member's code implements against — **this is critical to enable parallel work from the start**
- Set up `go.mod` with dependencies: sqlite (`modernc.org/sqlite`), HTTP router (`net/http` or `chi`), HNSW library, CLI (`spf13/cobra`), prompt wizard (`charmbracelet/huh`), output styling (`charmbracelet/lipgloss`), TTY detection (`golang.org/x/term`), OS keychain (`github.com/zalando/go-keyring`)

**Task 1.2 — MCP Streamable HTTP server core** (~2.5h)
- Implement the HTTP server at `/mcp` (POST endpoint)
- JSON-RPC 2.0 dispatcher: route by `method` field
- Session management: generate `MCP-Session-Id` on `initialize`, store in-memory map, return HTTP 404 for unknown sessions
- Auth middleware: read bearer token from `Authorization` header, compare to `secret.token` (0600 perms)
- Origin check middleware: if `Origin` header present, validate against allowlist, return 403 otherwise
- Handle `initialize` request → return server capabilities, protocol version `2025-11-25`, assign session
- Handle `notifications/initialized` → return HTTP 202

### Afternoon (1pm–6pm)

**Task 1.3 — tools/list + tool registry** (~1h)
- Implement `tools/list` handler
- Build a tool registry that maps tool names to handlers with full JSON Schema `inputSchema` + `outputSchema`
- Register all Day 1 tools as stubs initially (return partial/empty results) so the server is valid and testable immediately
- **Must register `dir2mcp.ask` on Day 1** — initially backed by search-only mode (no LLM call); full RAG answer generation added Day 2

**Task 1.4 — `dir2mcp.stats` tool** (~1h)
- Read `corpus.json` + live indexing state (shared atomic counters from ingest goroutine)
- Return full stats schema: root, state_dir, protocol_version, indexing progress, models config
- `indexing` object must include `mode` field (`"incremental"` or `"full"`) — required by SPEC §15.6

**Task 1.5 — `dir2mcp.list_files` tool** (~1h)
- Query SQLite `documents` table
- Support `path_prefix`, `glob`, `limit`, `offset`
- Return files with `rel_path`, `doc_type`, `size_bytes`, `mtime_unix`, `status`, `deleted`

**Task 1.6 — CLI `up` command wiring** (~1h)
- Wire `dir2mcp up` to: init config → preflight checks → init state dir → load/create SQLite → start MCP server → spawn ingest goroutine → print connection block to stdout
- Token generation and writing to `secret.token` (chmod 0600)
- Write `connection.json` to state dir — include a `session` object: `{ "uses_mcp_session_id": true, "header_name": "MCP-Session-Id", "assigned_on_initialize": true }`
- If `--auth file:<path>`, populate `connection.json` and NDJSON `connection.data` with `"token_source": "file"` and `"token_file": "<path>"`
- Print human-readable connection block (URL, auth header, token location)
- New global flag: `--non-interactive` (disables prompts; exits code 2 with actionable instructions on missing config)
- New `up` flags: `--read-only`, `--x402-resource-base-url <url>`
- Exit codes: 0=success, 1=generic, 2=config invalid, 3=root inaccessible, 4=bind failure, 5=index load failure, 6=ingestion fatal error

**Task 1.7 — NDJSON structured output mode** (~30min)
- `--json` flag: emit NDJSON events for `index_loaded`, `server_started`, `connection`, `scan_progress`, `embed_progress`, `file_error`
- `connection` event data must include `token_source` field; include `token_file` when `--auth file:` is used

---

## Tia — Ingestion Pipeline + SQLite + Mistral OCR

### Morning (9am–1pm)

**Task 1.8 — SQLite schema + all CRUD operations** (~2.5h)

Create `meta.sqlite` with these tables:

- `documents` — `doc_id`, `rel_path`, `source_type`, `doc_type`, `size_bytes`, `mtime_unix`, `content_hash`, `status`, `error`, `deleted`
- `representations` — `rep_id`, `doc_id`, `rep_type`, `rep_hash`, `created_unix`, `meta_json`, `deleted`
- `chunks` — `chunk_id`, `rep_id`, `ordinal`, `text`, `text_hash`, `tokens_est`, `index_kind`, `embedding_status`, `embedding_error`, `deleted`
- `spans` — `chunk_id`, `span_kind`, `start`, `end`, `extra_json`
- `settings` — `key`, `value`

Write Go functions for: insert/upsert document, insert representation, insert chunk + span batch, mark deleted, get chunks by rep, get document by path, list files with filters. Initialize `settings` with model config values.

**Task 1.9 — File discovery + type classification** (~1.5h)
- Recursive walk from root directory
- Default ignore list: `.git/`, `node_modules/`, `dist/`, `build/`, `.venv/`, `.dir2mcp/`
- Optional `.gitignore` parsing (read `.gitignore` and apply patterns)
- Path-based safety exclusions: `**/.env`, `**/*.pem`, `**/*.key`, `**/id_rsa`, `**/*credentials*` — configurable via `security.path_excludes`
- **Content-based secret pattern detection** (regex scan of file contents, default on): AWS key IDs (`AKIA[0-9A-Z]{16}`), AWS secret heuristic, JWTs (context-anchored), generic bearer tokens (`(?i)token\s*[:=]\s*[A-Za-z0-9_.-]{20,}`), common API key formats (`sk_[a-z0-9]{32}`, `api_[A-Za-z0-9]{32}`) — patterns configurable via `security.secret_patterns`; files matching are excluded from indexing and from `open_file` responses
- Max file size check (configurable per doc_type, default 20MB)
- Type classification: extension lookup + MIME sniff + binary heuristics → classify to `code|md|text|data|html|pdf|image|audio|archive|binary_ignored`
- Symlink policy: default no-follow

### Afternoon (1pm–6pm)

**Task 1.10 — raw_text representation + chunking** (~1.5h)
- Text/code/md/data/html → read file, normalize to UTF-8, `\n` line endings → `raw_text` representation
- Character-based chunking (max_chars=2500, overlap_chars=250, min_chars=200) for text types
- Code line-window chunking (max_lines=200, overlap_lines=30) → store `lines` spans
- Compute `content_hash` (sha256), `rep_hash`, `text_hash` for incremental indexing
- Write chunks + spans to SQLite, set `embedding_status=pending`

**Task 1.11 — Hash-based incremental indexing logic** (~1h)
- Document-level: if `content_hash` unchanged + not deleted → skip rep generation
- Representation-level: if `rep_hash` unchanged → skip chunk rebuild
- Chunk-level: if `text_hash` unchanged → skip embedding

**Task 1.12 — Mistral OCR integration (mistral-ocr-latest)** (~1.5h)
- POST to Mistral OCR API with PDF/image file
- Parse OCR markdown response (page-aware)
- Create `ocr_markdown` representation
- Chunk per page, then within page by size constraints
- Store `page` spans in `spans` table
- Cache OCR output to `.dir2mcp/cache/ocr/` (hash-keyed)
- Handle OCR failures gracefully (mark doc `status=error`, log, continue)

---

## Ark — Mistral API Client + HNSW + Retrieval + search tool

### Morning (9am–1pm)

**Task 1.13 — Mistral embeddings API client** (~1.5h)
- Go HTTP client for Mistral API
- `POST /v1/embeddings` with `model: mistral-embed` (for text chunks)
- `POST /v1/embeddings` with `model: codestral-embed` (for code chunks)
- Batch embedding calls (respect rate limits)
- Retry logic with backoff for rate limit errors
- Return `[]float32` vectors

**Task 1.14 — HNSW index wrapper** (~2.5h)
- Use a Go HNSW library (recommend `github.com/coder/hnsw` — pure Go, no CGo)
- Implement `IndexWrapper` with: `Add(label uint64, vector []float32)`, `Search(vector []float32, k int) ([]uint64, []float32)`, `Save(path string)`, `Load(path string)`
- Two instances: `vectors_text.hnsw` and `vectors_code.hnsw`
- Label = `chunk_id` (so ANN result maps directly to SQLite)
- Persist index to disk after batches
- Oversampling logic: query `k * oversample_factor` (default 5) → filter `deleted=1` in SQLite → return first `k`

### Afternoon (1pm–6pm)

**Task 1.15 — Embedding pipeline (SQLite → embed → HNSW)** (~1.5h)
- Background goroutine that polls for `embedding_status=pending` chunks from SQLite
- Batch pending chunks by `index_kind` (text vs code)
- Call Mistral embed API in batches
- Write vectors to HNSW index
- Update `embedding_status=ok` in SQLite

**Task 1.16 — `dir2mcp.search` tool** (~1.5h)
- Accept: `query`, `k`, `index` (auto/text/code/both), `path_prefix`, `file_glob`, `doc_types`
- Embed query using appropriate model
- Route to correct HNSW index(es)
- `auto`: classify query as code vs text (simple keyword heuristic)
- `both`: query both indices, normalize scores (min-max per index), merge, re-rank
- Filter by SQLite: `path_prefix`, `file_glob`, `doc_types`, `deleted=0`
- Build Hit objects: `chunk_id`, `rel_path`, `doc_type`, `rep_type`, `score`, `snippet`, `span`
- Format citations: `[path:L1-L25]` for lines, `[path#p=3]` for pages
- Return `structuredContent` + `content[text]`

**Task 1.17 — `dir2mcp.open_file` tool** (~1h)
- Accept: `rel_path`, optional `start_line`/`end_line`, `page`, `start_ms`/`end_ms`, `max_chars`
- Validate `rel_path` resolves under root (PATH_OUTSIDE_ROOT check — no path traversal)
- **Before returning any content**, run `rel_path` AND extracted content through the exclusion engine (path_excludes + secret_patterns) — return `FORBIDDEN` error (not the content) if a match is found; this prevents tool-level bypass of ingestion filters
- If `page`: look up OCR representation → return page text
- If `start_ms/end_ms`: look up transcript → slice
- If `start_line/end_line`: read file lines directly
- Default: return first `max_chars` or first page or transcript beginning
- Truncation handling and `truncated: bool` in response

---

## Samet — CLI UX + Config + State Files + Demo Integration

### Morning (9am–1pm)

**Task 1.18 — Config loading (YAML + env + flags)** (~1.5h)
- Define `Config` struct covering all fields (server, mistral, rag, ingest, chunking, stt, x402, security, secrets)
- **Correct precedence order: CLI flags → env vars → `.dir2mcp.yaml` → defaults** (flags win, not lose)
- Secret source precedence (for API keys/tokens): env var → OS keychain → config file reference → interactive session-only
- New top-level `secrets:` config block: `provider: auto|keychain|file|env|session`, keychain service/account, file path+mode
- New top-level `security:` config block: `auth` (mode, token_file, token_env), `allowed_origins`, `path_excludes`, `secret_patterns`
- Config snapshot (`.dir2mcp.yaml.snapshot`) MUST record secret source metadata and MUST NOT contain plaintext secrets
- `dir2mcp config init`: **interactive TTY wizard** using `charmbracelet/huh`; prompts masked for secret inputs; fast path skips prompts if all required config already present; supports `--non-interactive` (exits code 2 with actionable instructions)
- `dir2mcp config print`: print effective resolved config as YAML
- Validate config (missing API key, invalid fields) → exit code 2

**Task 1.19 — State directory + file outputs** (~1h)
- On `dir2mcp up`: create `.dir2mcp/` with subdirs (`cache/ocr`, `cache/transcribe`, `cache/annotations`, `payments/`, `locks/`)
- Write `secret.token` (generate random 32-byte hex token, chmod 0600)
- Write `connection.json` with full schema including `session: { uses_mcp_session_id: true, header_name: "MCP-Session-Id", assigned_on_initialize: true }`; if `--auth file:` include `token_source: "file"` + `token_file: "<path>"`
- Write `.dir2mcp.yaml.snapshot` (resolved config snapshot — no plaintext secrets, only source metadata)
- Lock file: `locks/index.lock` to prevent concurrent `up` instances

**Task 1.20 — CLI progress output** (~1h)
- Human-readable live progress line:
  ```
  Progress: scanned=412 indexed=55 skipped=340 deleted=2 reps=88 chunks=1480 embedded=920 errors=1
  ```
- Print connection block on start (URL, auth header format, token file location)
- NDJSON mode: route all output to structured events (coordinate with Ali on event schema)

### Afternoon (1pm–6pm)

**Task 1.21 — `dir2mcp status` and local `dir2mcp ask` commands** (~1h)
- `status`: read `corpus.json` from disk, print human-readable progress + model config
- `ask "QUESTION"`: local convenience call into retrieval engine (bypasses MCP), prints answer + citations to stdout

**Task 1.22 — Voice demo integration scaffold (no custom frontend)** (~2h)
- Keep demo surface frontend-free: use ElevenLabs hosted talk-to page
- Confirm `dir2mcp up` exposes a remotely reachable MCP endpoint (`/mcp`) with stable auth mode for integration
- Add operator docs for required headers/secret token configuration in ElevenLabs MCP integration setup
- Add a minimal validation checklist: tools enumerate correctly and at least one read-only query tool executes end-to-end from hosted agent

**Task 1.23 — Demo corpus preparation** (~1h)
- Assemble a demo corpus: mix of PDF docs, audio files, code files, markdown notes
- Run `dir2mcp up` against it to verify end-to-end flow once Day 1 is integrated
- Document any integration issues found

---

---

# DAY 2 — Multimodal + RAG + x402 + Polish

**Goal by end of Day 2:** Full multimodal RAG working (OCR confirmed, audio STT, annotations), `ask` generates real answers with citations, `transcribe`/`annotate`/`transcribe_and_ask` tools working, x402 payment gating live, hosted voice demo ready.

---

## Ali — x402 Payment Gating + ElevenLabs TTS + Integration

### Morning (9am–1pm)

**Task 2.1 — x402 HTTP middleware** (~2.5h)
- Read config `x402.mode` (off/on/required)
- If `on`/`required`: intercept `tools/call` requests at HTTP layer before MCP dispatch
- Use **x402 v2 header semantics** (not JSON body):
  - Return HTTP 402 with `PAYMENT-REQUIRED` header containing machine-readable payment requirements (network MUST use CAIP-2 format, e.g. `eip155:8453`)
  - Client retries with `PAYMENT-SIGNATURE` header containing payment proof
  - On successful payment, include `PAYMENT-RESPONSE` header with facilitator settlement metadata
- POST payment payload to `x402.facilitator_url` endpoints (`/v2/x402/verify` then `/v2/x402/settle`) for verification and settlement — dir2mcp remains non-custodial
- Keep `initialize` and `tools/list` ungated; only gate `tools/call`
- On success: continue to MCP handler; on failure: return 402 with failure reason
- Emit NDJSON events: `payment_required`, `payment_verified`, `payment_settled`, `payment_failed`
- Write settlement outcome to `.dir2mcp/payments/settlement.log`
- Optional: if `x402.bazaar.enabled`, emit discovery metadata via x402 extension metadata to facilitator discovery API (`GET {facilitator_url}/discovery/resources`)

**Task 2.2 — Rate limiting middleware** (~1h)
- When `--public` is set: apply token bucket rate limiter (requests/minute + burst)
- Per-IP rate limiting using in-memory map
- Return HTTP 429 on breach

### Afternoon (1pm–6pm)

**Task 2.3 — ElevenLabs TTS + `dir2mcp.ask_audio` tool** (~1.5h)
- ElevenLabs TTS API client: POST answer text to `/v1/text-to-speech/{voice_id}`
- Return base64-encoded MP3/WAV
- Implement `dir2mcp.ask_audio` tool: run `ask` then pipe answer text to TTS
- Tool result `content[]` includes both a `text` item and an `audio` item with base64 payload + mimeType

**Task 2.4 — End-to-end integration testing + binary build** (~1.5h)
- Test full flow: `dir2mcp up` → index mixed corpus → MCP client connect → call all 8 tools
- Verify MCP protocol compliance: session lifecycle, correct JSON-RPC error codes, `isError` flag
- Test `--public` + x402 gating flow
- Build final binary: `go build -o dir2mcp ./cmd/dir2mcp/`
- Test on a fresh directory from scratch

---

## Tia — Audio STT + `ask` RAG Generation + Bug Fixes

### Morning (9am–1pm)

**Task 2.5 — Mistral STT integration (voxtral-mini-latest)** (~2h)
- POST audio file to Mistral STT API
- Parse transcript response: segments with `start_ms`, `end_ms`, `text`
- Create `transcript` representation with `meta_json`: `{provider: "mistral", model: "voxtral-mini-latest", timestamps: true, duration_ms: N}`
- Segment into time windows (30s, 5s overlap) → chunks with `time` spans
- If timestamps unavailable: fall back to text-size chunking, omit time spans
- Cache transcript to `.dir2mcp/cache/transcribe/`

**Task 2.6 — ElevenLabs STT (Scribe) alternate provider** (~1h)
- ElevenLabs Scribe API client
- Same output interface as Mistral STT — both produce the same normalized `transcript` representation format
- Config `stt.provider: elevenlabs` switches the provider at startup

### Afternoon (1pm–6pm)

**Task 2.7 — `dir2mcp.ask` RAG generation** (~2h)
- If `mode=answer` (default): build RAG prompt:
  - System prompt from config
  - Retrieved context chunks with citations interleaved
  - User question
- POST to Mistral chat completions (`mistral-small-2506`)
- Parse response → `answer` string
- Extract inline citations, map back to `chunk_id` + `span`
- Return `{question, answer, citations[], hits[], indexing_complete}`
- If `mode=search_only`: skip generation, return hits only

**Task 2.8 — Archive ingestion (zip/tar)** (~1h)
- For `archive` doc_type: extract members, ingest each member as a sub-document
- Path safety: reject members with path traversal (zip-slip prevention)
- Each archive member becomes a `documents` row with `source_type=archive_member`

**Task 2.9 — Bug fixes + stability** (~1h)
- Handle Mistral API errors gracefully (rate limits, auth failures) with canonical error codes
- Fix any SQLite concurrency issues (WAL mode, write lock from ingest goroutine vs reads from HTTP handlers)
- Ensure `deleted` tombstones work correctly in all tool responses

---

## Ark — `annotate` Tool + Index Fusion + RAG Quality

### Morning (9am–1pm)

**Task 2.10 — `dir2mcp.transcribe` MCP tool** (~1h)
- Expose `dir2mcp.transcribe` as a standalone MCP tool (SPEC §15.7 — listed in "recommended" tools)
- Accept: `rel_path`, `language`, `timestamps` (default true), `retranscribe` (default false)
- Check if transcript already exists in SQLite; if `retranscribe=false` and transcript exists, return cached result
- Otherwise call the STT pipeline (Tia's Task 2.5/2.6) and wait for completion
- Return: `rel_path`, `provider`, `model`, `indexed`, `segments[]` (start_ms, end_ms, text)
- This is separate from `transcribe_and_ask` — it only transcribes, does not run ask

**Task 2.11 — `dir2mcp.annotate` tool** (~2h)
- Accept: `rel_path`, `schema_json` (user-provided JSON Schema), `index_flattened_text`
- Read file content (or OCR/transcript if already indexed)
- POST to Mistral chat: "Extract structured data matching this schema from the document"
- Parse response as `annotation_json`
- Store `annotation_json` representation in SQLite
- If `index_flattened_text=true`: flatten `{key: value}` → `annotation_text` representation → chunk → embed
- Cache to `.dir2mcp/cache/annotations/`
- Return: `{rel_path, stored, flattened_indexed, annotation_json, annotation_text_preview}`

**Task 2.12 — `dir2mcp.transcribe_and_ask` tool** (~1.5h)
- Accept: `rel_path`, `question`, `k`
- Check if transcript exists in SQLite for `rel_path` → if not, call transcription pipeline (reuse logic from Task 2.10)
- Run `ask` with transcript chunks filtered to `rel_path`
- Return `ask` output schema + `{transcript_provider, transcript_model, transcribed: bool}`

### Afternoon (1pm–6pm)

**Task 2.13 — `both` index fusion quality** (~1h)
- Test `index=both` with queries that span code + text
- Tune per-index score normalization (min-max normalization per index before merge)
- Ensure no duplicate chunks in merged results

**Task 2.14 — RAG quality tuning** (~1h)
- Test RAG `ask` responses with the demo corpus
- Tune system prompt (refine for citation accuracy and answer quality)
- Test citation accuracy — ensure cited spans actually contain the relevant content
- Confirm `oversample_factor=5` is sufficient for typical deletion rates

**Task 2.15 — `dir2mcp reindex` + corpus.json writer** (~1h)
- `dir2mcp reindex`: force full rebuild (clear `content_hash` in SQLite to trigger re-indexing of all docs)
- Periodic `corpus.json` writer goroutine (update every 5 seconds during indexing)
- Ensure `corpus.json` has correct `code_ratio` calculation from `doc_counts`

**Task 2.16 — Performance test with larger corpus** (~1h)
- Test with a corpus of 200+ files
- Ensure embedding batching doesn't overwhelm Mistral rate limits
- Ensure SQLite WAL mode is enabled for concurrent read/write

---

## Samet — Hosted Voice Demo + Documentation

### Morning (9am–1pm)

**Task 2.17 — ElevenLabs MCP integration + approval policy** (~2h)
- Add custom MCP integration in ElevenLabs dashboard to the deployed `dir2mcp` endpoint
- Configure secure headers/token in integration settings (no secrets in prompts)
- Attach integration to demo agent and set fine-grained approvals for read-only tools
- Verify the agent reliably calls MCP tools for project/domain questions

**Task 2.18 — Hosted talk-to demo flow + prompt policy** (~1.5h)
- Use ElevenLabs hosted talk-to link as demo entrypoint (no custom Next.js UI)
- Add agent system prompt policy: call MCP knowledge tools first for repo/project questions
- Require concise citations (`rel_path` + line/page/time span) in spoken responses where possible
- Add fallback guidance when no matching source is found

### Afternoon (1pm–6pm)

**Task 2.19 — Demo corpus setup** (~1h)
- Curate a compelling demo corpus showing all modalities:
  - A technical PDF (research paper or documentation)
  - An audio file (meeting recording or lecture clip)
  - A code repository (this repo itself works well)
  - Some structured data or markdown notes
- Run full index, verify all doc types indexed correctly

**Task 2.20 — Demo script + talking points** (~1h)
- Write a step-by-step demo script (target 3–5 minutes):
  1. `dir2mcp up` on demo corpus → show live progress
  2. Expose `/mcp` publicly (tunnel or reverse proxy) and verify `initialize` + `tools/list`
  3. Open hosted ElevenLabs talk-to page (agent with MCP integration)
  4. Ask a repo/corpus question that triggers MCP tool call and citations
  5. Show fallback behavior (no-evidence or timeout path)
  6. Optional: enable x402 and show HTTP 402 payment-required flow
- Rehearse and time the demo

**Task 2.21 — README and quick-start docs** (~1h)
- Update `README.md` with: install instructions, quick-start example, env vars required (`MISTRAL_API_KEY`, optional `ELEVENLABS_API_KEY`)
- Add example config YAML snippet
- Add hosted-talk-to runbook details:
  - MCP endpoint field mapping for ElevenLabs custom MCP server setup
  - secure header/token setup guidance
  - pre-demo smoke checklist

**Task 2.22 — Final integration smoke test** (~1h)
- Full clean-room test: fresh directory, set `MISTRAL_API_KEY`, run `dir2mcp up ./demo-corpus`
- Verify every tool works via MCP client (or curl)
- Verify hosted ElevenLabs talk-to can call at least one read-only MCP tool end-to-end

---

## Summary Table

| | Ali | Ark | Tia | Samet |
|---|---|---|---|---|
| **Day 1 AM** | Go scaffold + shared interfaces + MCP server core (JSON-RPC, sessions, auth) | Mistral embed client + HNSW index wrapper | SQLite schema + file discovery + type classification (incl. content-based secret patterns) | Config loading (correct precedence: flags→env→yaml→defaults) + secrets/keychain block + interactive wizard |
| **Day 1 PM** | `tools/list`, `stats` (with `mode` field), `list_files`, `ask` stub (search_only), `up` wiring (exit codes, new flags, extended connection.json) + NDJSON mode | Embedding pipeline + `search` tool + `open_file` tool (with exclusion engine enforcement) | `raw_text` rep + chunking + incremental hash logic + Mistral OCR | CLI progress output + `status`/`ask` commands + hosted demo integration scaffold |
| **Day 2 AM** | x402 v2 middleware (PAYMENT-REQUIRED/SIGNATURE/RESPONSE headers) + rate limiting | `transcribe` MCP tool + `annotate` tool + `transcribe_and_ask` tool | Mistral STT + ElevenLabs STT (both normalized) | ElevenLabs MCP integration + read-only tool approval policy |
| **Day 2 PM** | ElevenLabs TTS + `ask_audio` + integration tests + binary build | Index fusion tuning + RAG quality + `reindex` + perf test | `ask` RAG generation (full LLM call) + archive ingestion + bug fixes | Hosted talk-to flow + public endpoint runbook/checklist + demo corpus/script + docs |

---

## Critical Dependencies (things that block other work)

1. **Shared Go interfaces (Ali, Day 1 AM first 30min)** — everyone needs these to code in parallel without stepping on each other
2. **SQLite schema (Tia, Day 1 AM)** — blocks Ark's embedding pipeline (needs `chunks` table) and Ali's `list_files` tool (needs `documents` table)
3. **Mistral embed client (Ark, Day 1 AM)** — blocks the embedding pipeline
4. **HNSW wrapper (Ark, Day 1 AM)** — blocks the `search` tool
5. **MCP server + tool dispatch (Ali, Day 1 PM)** — blocks any tool being actually callable
6. **Config (Samet, Day 1 AM)** — blocks everyone needing API keys and model name constants; must use correct precedence (flags→env→yaml→defaults)
7. **`search` tool (Ark, Day 1 PM)** — blocks `ask` stub on Day 1 (search_only) and full RAG `ask` on Day 2
8. **Exclusion engine (Tia, Day 1 AM)** — blocks `open_file` (Ark, Day 1 PM) which must call it before returning content
9. **STT integration (Tia, Day 2 AM)** — blocks `transcribe` tool (Ark, Day 2 AM) and `transcribe_and_ask` tool (Ark, Day 2 AM)

---

## Technical Decisions to Lock Down Together (Day 1, first 30 min)

| Decision | Recommendation | Who decides |
|----------|---------------|-------------|
| HNSW Go library | `github.com/coder/hnsw` — pure Go, no CGo | Ali + Ark |
| HTTP router | `chi` or standard `net/http` + mux | Ali |
| SQLite driver | `modernc.org/sqlite` — pure Go, no CGo | Tia |
| File hashing | `crypto/sha256` (standard lib) | Tia |
| CLI framework | `spf13/cobra` (SPEC §20 recommendation) | Samet |
| Prompt wizard | `charmbracelet/huh` for interactive config init | Samet |
| Output styling | `charmbracelet/lipgloss` | Samet |
| TTY detection | `golang.org/x/term` | Samet |
| OS keychain | `github.com/zalando/go-keyring` | Samet |
| Demo surface | ElevenLabs hosted talk-to page (no custom frontend) | Samet |
| Embedding batch size | 32 chunks per API call (tune later) | Ark |

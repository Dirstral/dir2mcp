# Issue #11 — State outputs + status/ask CLI + web UI scaffold/proxy

## Scope (Day 1–2, Samet)

From hackathon plan:
- **Task 1.19** (partially done): State dir + connection.json ✓; **corpus.json** not yet written on `up`.
- **Task 1.21**: `status` (read corpus.json) ✓; **ask** (local retrieval) ✓. Need **corpus.json** to exist so status shows data.
- **Task 1.22**: Web UI scaffold: **ui/** (Next.js), **Go proxy** `/api/mcp` (forward to MCP with token), pages `/`, `/search`, `/ask`.

## 1. State outputs

### 1.1 corpus.json (SPEC §4.4)

- **Add** `internal/state/corpus.go`:
  - Structs: `CorpusJSON` (Root, Profile, Models, Indexing) matching SPEC.
  - `Profile`: DocCounts (map[string]int), CodeRatio (float64).
  - `Models`: EmbedText, EmbedCode, OCR, STTProvider, STTModel, Chat.
  - `Indexing`: JobID, Running, **Mode** ("incremental"|"full"), Scanned, Indexed, Skipped, Deleted, Representations, ChunksTotal, EmbeddedOk, Errors.
  - `WriteCorpusJSON(stateDir, root string, corpus *CorpusJSON) error`.
- **On `up`**: After `WriteConnectionJSON`, call `WriteCorpusJSON` with:
  - root = rootDir
  - profile = zeros (or empty)
  - models = from cfg (Mistral, STT, RAG chat model)
  - indexing = job_id (e.g. `"job_" + time.Now().Format(time.RFC3339)`), running=true, mode="incremental", rest 0.

Result: `dir2mcp status` will find corpus.json and display progress; indexer (later) can update the same file.

## 2. Status CLI

- **Extend** `internal/cli/status.go`:
  - Parse full SPEC schema: `profile` (doc_counts, code_ratio), `models` (embed_text, embed_code, ocr, stt_provider, stt_model, chat), `indexing.mode`.
  - Print "Mode: incremental" (or full).
  - If profile/models present, print a short "Models: ..." and "Profile: ..." section.

## 3. Ask CLI

- No change: already uses retrieval engine and `cfg.RAG.KDefault`; stub returns "not implemented" until Ark wires it.

## 4. Web UI scaffold + proxy

### 4.1 Go server changes

- **Refactor MCP server** (`internal/mcp/server.go`):
  - Add `MCPHandler() http.Handler` so the MCP path handler can be mounted on an external mux.
  - Keep `Serve(listener)` for backward compatibility **or** remove it and have `up` build the mux (chosen: up builds mux so we have one place for all routes).
- **In `up`** (internal/cli/up.go):
  - Build a single `http.ServeMux`:
    - `mcpPath` → MCP handler (from server.MCPHandler()).
    - `POST /api/mcp` → **proxy**: forward request to `mcpURL` with `Authorization: Bearer <token>` (and same body). Target = same server (mcpURL).
    - `GET /api/corpus` → read `stateDir/corpus.json`, return JSON (CORS allowed).
    - `GET /api/connection` → read `stateDir/connection.json`, redact token in response (optional; or omit token for UI).
  - Add **CORS** for `/api/*`: `Access-Control-Allow-Origin` from config `security.allowed_origins` or default `http://localhost:3000` for dev.
  - Use one `http.Server` with timeouts and this mux; `Serve(listener)`.

### 4.2 Next.js UI (ui/)

- **Create** `ui/` with Next.js (App Router, TypeScript, Tailwind).
- **Pages**:
  - **`/`** (Dashboard): Fetch `GET /api/corpus` (and optionally `/api/connection`), show root, state, indexing progress (scanned, indexed, …), mode, models.
  - **`/search`**: Form with text input; on submit `POST /api/mcp` with JSON-RPC `dir2mcp.search` (or show "use MCP when implemented"); scaffold only.
  - **`/ask`**: Form with question; on submit `POST /api/mcp` with JSON-RPC `dir2mcp.ask` (or show "use MCP when implemented"); scaffold only.
- **Env**: `NEXT_PUBLIC_API_URL=http://localhost:PORT` (user sets PORT to the one printed by `dir2mcp up`), or read from connection.json via /api/connection.

## 5. Implementation order

1. state/corpus.go + WriteCorpusJSON; call from up.
2. status.go: extend struct + print mode, profile, models.
3. mcp/server.go: add MCPHandler(); up: build mux (MCP, /api/mcp, /api/corpus), CORS, single http.Server.
4. ui/: create-next-app, dashboard (fetch /api/corpus), search/ask placeholder pages.

## 6. Acceptance

- `dir2mcp up` writes corpus.json; `dir2mcp status` shows mode + indexing + models.
- `dir2mcp ask "foo"` still returns stub message.
- Browser: open ui (npm run dev), set NEXT_PUBLIC_API_URL to up URL; dashboard shows corpus stats; /search and /ask are present and call /api/mcp when tools exist.

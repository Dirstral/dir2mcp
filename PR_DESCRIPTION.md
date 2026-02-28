# Add Web UI with search, ask, and dashboard pages

**Suggested PR title** (update on GitHub): `Add Web UI with search, ask, and dashboard pages`

## Summary

Adds a Web UI with Search, Ask, and Dashboard pages that connect to the `dir2mcp up` MCP server. Users can search the corpus, ask questions with inline citations, and view indexing progress.

## What's New

### Search Page (`/search`)
- Text input → calls `dir2mcp.search` via Next.js `/api/mcp` proxy
- Hit cards with file path, span, snippet, and `doc_type` badges
- Clickable citations open an `open_file` modal
- AbortController, focus management, Escape to close

### Ask Page (`/ask`)
- Question input → calls `dir2mcp.ask` via Next.js `/api/mcp` proxy
- Answer with inline citations, collapsible sources
- Banner when indexing is incomplete

### Dashboard (`/`)
- Live stats polling (every 2s), progress bar, doc_type breakdown
- Uses `/api/corpus` proxy (server-side, requires `API_TOKEN`)

### API & Auth
- **`/api/corpus`** — Next.js proxy to dir2mcp with Bearer token
- **`/api/mcp`** — Next.js proxy to dir2mcp; client calls relative URL; server adds token
- dir2mcp `/api/mcp` and `/api/corpus` require Bearer

## Setup

1. Run `dir2mcp up` and note the URL + Bearer token
2. Copy `ui/.env.example` → `ui/.env.local`
3. Set `NEXT_PUBLIC_API_URL` and `API_TOKEN`
4. Run `npm run dev` in `ui/`

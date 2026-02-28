# Add Web UI with search, ask, and dashboard pages

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
- **`/api/corpus`** — Next.js server-side proxy that forwards requests to dir2mcp and injects the Bearer token from the `API_TOKEN` env var
- **`/api/mcp`** — Next.js client-callable API route; the Next.js server adds the Bearer token before forwarding to dir2mcp

## Prerequisites

- Node.js 18+ and npm or yarn

## Setup

1. Run `dir2mcp up` and note the URL + Bearer token
2. Copy `ui/.env.example` → `ui/.env.local`
3. Set `NEXT_PUBLIC_API_URL` and `API_TOKEN`
4. Run `npm run dev` in `ui/`

## Verification

Visit http://localhost:3000. The dashboard should load; check the browser Network tab for successful `/api/corpus` calls (200) and that requests include the `Authorization: Bearer` header.

## Troubleshooting

- **Dashboard shows "Failed to load"** — Ensure `dir2mcp up` is running and `NEXT_PUBLIC_API_URL` matches the printed URL
- **401 on API calls** — Verify `API_TOKEN` in `.env.local` matches the Bearer token from `dir2mcp up`
- **Search/Ask fail** — Same as above; both use the Next.js proxy which requires `API_TOKEN`
- Restart the Next.js dev server after changing env vars

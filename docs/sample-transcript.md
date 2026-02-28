# Sample Q&A Transcript

> This transcript demonstrates how the ElevenLabs agent uses dir2mcp MCP tools to answer repository questions. Tool invocations are shown in `[Tool Call]` / `[Tool Result]` blocks.

---

## Example 1: "How does CORS work in this project?"

**User:** How does CORS work in this project?

```
[Tool Call] dir2mcp.search
  { "query": "CORS middleware preflight", "k": 5 }

[Tool Result]
  {
    "hits": [
      {
        "rel_path": "internal/mcp/server.go",
        "score": 0.87,
        "snippet": "func (s *Server) corsMiddleware(next http.Handler) http.Handler ...",
        "span": { "kind": "lines", "start_line": 125, "end_line": 144 }
      },
      {
        "rel_path": "tests/issue37/cors_public_test.go",
        "score": 0.72,
        "snippet": "func TestCORS_PreflightReturns204(t *testing.T) ...",
        "span": { "kind": "lines", "start_line": 1, "end_line": 30 }
      }
    ]
  }
```

```
[Tool Call] dir2mcp.open_file
  { "rel_path": "internal/mcp/server.go", "start_line": 125, "end_line": 144 }

[Tool Result]
  {
    "rel_path": "internal/mcp/server.go",
    "doc_type": "code",
    "content": "func corsMiddleware(next http.Handler, allowedOrigins []string) http.Handler {\n\treturn http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {\n\t\torigin := r.Header.Get(\"Origin\")\n\t\tif origin != \"\" && isOriginAllowed(origin, allowedOrigins) {\n\t\t\tw.Header().Set(\"Access-Control-Allow-Origin\", origin)\n\t\t\tw.Header().Set(\"Access-Control-Allow-Methods\", \"POST, OPTIONS\")\n\t\t\tw.Header().Set(\"Access-Control-Allow-Headers\", \"Content-Type, Authorization, MCP-Protocol-Version, MCP-Session-Id\")\n\t\t\tw.Header().Set(\"Access-Control-Expose-Headers\", \"MCP-Session-Id\")\n\t\t\tw.Header().Set(\"Access-Control-Max-Age\", \"86400\")\n\t\t\tw.Header().Set(\"Vary\", \"Origin\")\n\t\t}\n\t\tif r.Method == http.MethodOptions {\n\t\t\tw.WriteHeader(http.StatusNoContent)\n\t\t\treturn\n\t\t}\n\t\tnext.ServeHTTP(w, r)\n\t})\n}",
    "truncated": false
  }
```

**Agent:** CORS is handled by a middleware function in `internal/mcp/server.go`, lines 125 through 144. Here's how it works:

1. The middleware checks if the request's `Origin` header is in the allowed origins list.
2. If the origin is allowed, it sets the standard CORS headers — `Access-Control-Allow-Origin`, `Allow-Methods` (POST and OPTIONS), `Allow-Headers` (including `Authorization` and `MCP-Session-Id`), and a max-age of 86,400 seconds (24 hours).
3. For OPTIONS preflight requests, it returns HTTP 204 with no body.
4. For all other requests, it passes through to the next handler.

The origin validation itself is in the `isOriginAllowed` function, which does case-insensitive hostname matching and supports both full URLs and simple hostname entries. There are also tests for this in `tests/issue37/cors_public_test.go` that verify preflight returns 204, disallowed origins get 403, and allowed origins get the correct headers.

---

## Example 2: "What files are in the project?"

**User:** What's the project structure? What kind of files are in here?

```
[Tool Call] dir2mcp.list_files
  { "limit": 30 }

[Tool Result]
  {
    "total": 47,
    "files": [
      { "rel_path": "cmd/dir2mcp/main.go", "doc_type": "code", "size_bytes": 1240, "status": "ok" },
      { "rel_path": "internal/cli/app.go", "doc_type": "code", "size_bytes": 18500, "status": "ok" },
      { "rel_path": "internal/mcp/server.go", "doc_type": "code", "size_bytes": 15200, "status": "ok" },
      { "rel_path": "internal/mcp/tools.go", "doc_type": "code", "size_bytes": 28800, "status": "ok" },
      { "rel_path": "internal/config/config.go", "doc_type": "code", "size_bytes": 5600, "status": "ok" },
      { "rel_path": "internal/retrieval/service.go", "doc_type": "code", "size_bytes": 7200, "status": "ok" },
      { "rel_path": "docs/elevenlabs-integration.md", "doc_type": "md", "size_bytes": 2800, "status": "ok" },
      { "rel_path": "docs/smoke-test.md", "doc_type": "md", "size_bytes": 3100, "status": "ok" },
      { "rel_path": "docs/approval-policy.md", "doc_type": "md", "size_bytes": 4200, "status": "ok" },
      { "rel_path": "go.mod", "doc_type": "unknown", "size_bytes": 450, "status": "ok" }
    ]
  }
```

**Agent:** The project has 47 indexed files. Here's the high-level structure:

- **`cmd/dir2mcp/`** — the entry point (`main.go`).
- **`internal/cli/`** — CLI commands and flag parsing (`app.go`).
- **`internal/mcp/`** — the MCP server implementation (`server.go`) and tool definitions (`tools.go`).
- **`internal/config/`** — configuration loading and origin allowlist merging.
- **`internal/retrieval/`** — the search and retrieval service.
- **`docs/`** — integration guides, smoke tests, and policy documentation.
- **`tests/`** — integration and unit tests.

It's a Go project with a `go.mod` file. Would you like me to look deeper into any specific directory or file?

---

## Example 3: "Is the search index ready?"

**User:** Is the indexing done yet?

```
[Tool Call] dir2mcp.stats
  {}

[Tool Result]
  {
    "root": "/home/user/my-project",
    "protocol_version": "2025-11-25",
    "indexing": {
      "running": false,
      "scanned": 47,
      "indexed": 45,
      "skipped": 2,
      "deleted": 0,
      "chunks_total": 312,
      "embedded_ok": 312,
      "errors": 0
    }
  }
```

**Agent:** Yes, indexing is complete. The server scanned 47 files, successfully indexed 45 of them, and skipped 2. All 312 chunks are embedded with zero errors. Search results should be fully up to date. What would you like to look up?

---

## Example 4: "How does authentication work?"

**User:** How does auth work when running in public mode?

```
[Tool Call] dir2mcp.search
  { "query": "public mode authentication bearer token", "k": 5 }

[Tool Result]
  {
    "hits": [
      {
        "rel_path": "internal/mcp/server.go",
        "score": 0.82,
        "snippet": "authHeader := strings.TrimSpace(r.Header.Get(\"Authorization\")) ...",
        "span": { "kind": "lines", "start_line": 308, "end_line": 334 }
      },
      {
        "rel_path": "internal/cli/app.go",
        "score": 0.76,
        "snippet": "if cfg.Public && cfg.AuthMode == \"none\" ...",
        "span": { "kind": "lines", "start_line": 231, "end_line": 248 }
      }
    ]
  }
```

```
[Tool Call] dir2mcp.open_file
  { "rel_path": "internal/mcp/server.go", "start_line": 308, "end_line": 334 }

[Tool Result]
  {
    "content": "// authorize checks the Bearer token.\nfunc (s *Server) authorize(w http.ResponseWriter, r *http.Request) bool {\n\tif strings.EqualFold(s.cfg.AuthMode, \"none\") {\n\t\treturn true\n\t}\n\texpectedToken := s.authToken\n\tauthHeader := strings.TrimSpace(r.Header.Get(\"Authorization\"))\n\tconst bearerPrefix = \"bearer \"\n\tif len(authHeader) < len(bearerPrefix) || strings.ToLower(authHeader[:len(bearerPrefix)]) != bearerPrefix {\n\t\twriteError(w, http.StatusUnauthorized, nil, -32000, \"missing or invalid bearer token\", \"UNAUTHORIZED\", false)\n\t\treturn false\n\t}\n\tprovidedToken := strings.TrimSpace(authHeader[len(bearerPrefix):])\n\tif expectedToken == \"\" || providedToken == \"\" {\n\t\twriteError(w, http.StatusUnauthorized, nil, -32000, \"missing or invalid bearer token\", \"UNAUTHORIZED\", false)\n\t\treturn false\n\t}\n\tif subtle.ConstantTimeCompare([]byte(providedToken), []byte(expectedToken)) != 1 {\n\t\twriteError(w, http.StatusUnauthorized, nil, -32000, \"missing or invalid bearer token\", \"UNAUTHORIZED\", false)\n\t\treturn false\n\t}\n\treturn true\n}"
  }
```

**Agent:** Authentication in public mode works like this:

1. **Public mode requires auth.** In `internal/cli/app.go` (lines 231–248), the CLI rejects the combination of `--public` and `--auth none` — unless you explicitly pass `--force-insecure`. This prevents accidentally exposing the server without a token.

2. **Bearer token validation** is in `internal/mcp/server.go` (lines 308–334). Every request must include an `Authorization: Bearer <token>` header. The server extracts the token, trims whitespace, and compares it using `subtle.ConstantTimeCompare` to prevent timing attacks.

3. **Token generation** happens automatically on first run — it generates a 64-character hex string (32 random bytes) and stores it in `.dir2mcp/secret.token` with owner-only permissions (0600).

So in short: public mode always requires a Bearer token, the token is compared securely, and there's a safety guard that prevents you from running public without auth by accident.

---

## Example 5: No results found

**User:** Does this project support GraphQL?

```
[Tool Call] dir2mcp.search
  { "query": "GraphQL schema resolver", "k": 5 }

[Tool Result]
  {
    "hits": []
  }
```

**Agent:** I searched the repository for GraphQL-related code — schemas, resolvers, and related keywords — but didn't find any results. This project doesn't appear to include GraphQL support. It uses JSON-RPC 2.0 over HTTP for its MCP protocol instead.

Is there something specific you're looking for? I can search with different terms if that would help.

---

## Key Behaviors Demonstrated

| Behavior | Examples |
|---|---|
| **Tool called before answering** | All five examples call a tool before the agent responds |
| **Citations with path + span** | Examples 1 and 4 cite specific files and line ranges |
| **Multi-step tool chains** | Examples 1 and 4 use `search` → `open_file` |
| **Honest "not found"** | Example 5 reports no results without guessing |
| **Clarifying follow-up** | Examples 2, 3, and 5 end with a follow-up offer |
| **Conversational voice style** | Paths spoken naturally ("internal slash mcp slash server dot go") are formatted as code in transcript |

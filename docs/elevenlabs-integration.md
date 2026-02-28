# ElevenLabs MCP Integration Guide

## Prerequisites

- dir2mcp binary built (`make build` or `go build -o dir2mcp ./cmd/dir2mcp/`)
- A directory with content to index
- `MISTRAL_API_KEY` set (required for embeddings/search)
- [ngrok](https://ngrok.com/) or [cloudflared](https://developers.cloudflare.com/cloudflare-one/connections/connect-apps/) for public tunneling

## 1. Start dir2mcp

```bash
export MISTRAL_API_KEY=sk-...

# Start in public mode on a fixed port, allowing ElevenLabs origins
./dir2mcp up --public --listen 0.0.0.0:8080 \
  --allowed-origins "https://elevenlabs.io,https://api.elevenlabs.io"
```

The server prints:

```
MCP endpoint:
  URL:    http://0.0.0.0:8080/mcp
  Auth:   Bearer (source=secret.token)
```

Your Bearer token is in `.dir2mcp/secret.token`:

```bash
cat .dir2mcp/secret.token
```

## 2. Expose publicly via ngrok

In a second terminal:

```bash
ngrok http 8080
```

ngrok prints a public URL like `https://abc123.ngrok-free.app`. Your MCP endpoint is:

```
https://abc123.ngrok-free.app/mcp
```

## 3. ElevenLabs Dashboard: Add Custom MCP Server

In the ElevenLabs dashboard, go to your Conversational AI agent settings and add a custom MCP server:

| Field | Value |
|---|---|
| **Server URL** | `https://<ngrok-id>.ngrok-free.app/mcp` |
| **Authentication Type** | Bearer Token |
| **Token** | Contents of `.dir2mcp/secret.token` |

### Headers (if configurable)

| Header | Value |
|---|---|
| `Authorization` | `Bearer <token>` |
| `Content-Type` | `application/json` |

## 4. Tool Approval Policy

For the demo, auto-approve these read-only tools:

| Tool | Action | Approve? |
|---|---|---|
| `dir2mcp.search` | Semantic search | Auto-approve |
| `dir2mcp.ask` | RAG Q&A | Auto-approve |
| `dir2mcp.open_file` | Read file slice | Auto-approve |
| `dir2mcp.list_files` | List indexed files | Auto-approve |
| `dir2mcp.stats` | Indexing progress | Auto-approve |

All tools are read-only. No write operations are exposed.

## 5. Verify MCP Connection

After adding the server in ElevenLabs, the platform will call:

1. `initialize` - handshake and capability negotiation
2. `tools/list` - discover available tools

If both succeed, the tools appear in your agent's tool list.

## MCP Protocol Details

- **Transport:** Streamable HTTP (POST to `/mcp`)
- **Protocol version:** `2025-11-25`
- **Wire format:** JSON-RPC 2.0
- **Session:** Server assigns `MCP-Session-Id` header on initialize response; all subsequent requests require this header
- **CORS:** Server responds to OPTIONS preflight with correct Access-Control headers for allowed origins
- **Auth:** Bearer token in `Authorization` header (required unless `--auth none`)

## Alternative: Cloudflare Tunnel

If ngrok is not available:

```bash
cloudflared tunnel --url http://localhost:8080
```

This provides a `*.trycloudflare.com` URL. Use it the same way as the ngrok URL.

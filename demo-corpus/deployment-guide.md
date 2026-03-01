# dir2mcp Deployment Guide

## Prerequisites

- Go 1.24 or later
- A Mistral API key (for embeddings, OCR, and generation)
- The directory you want to index

## Quick Start

### 1. Build

```bash
cd dir2mcp
make build
```

### 2. Configure

Create a `.env` file or set environment variables:

```bash
export MISTRAL_API_KEY=your-key-here
```

Optional configuration via `.dir2mcp.yaml`:

```yaml
root_dir: ./my-docs
listen_addr: 127.0.0.1:8087
auth_mode: auto
```

### 3. Run

```bash
./dir2mcp up --listen 127.0.0.1:8087
```

The server will:
1. Start background indexing of the current directory
2. Generate a bearer token at `.dir2mcp/secret.token`
3. Begin accepting MCP requests at `http://127.0.0.1:8087/mcp`

## Public Deployment

For external access (e.g., from ElevenLabs, Claude, or other cloud services):

### Option A: ngrok

```bash
ngrok http 8087
```

This gives you an HTTPS URL like `https://abc123.ngrok.io`. Your MCP endpoint becomes `https://abc123.ngrok.io/mcp`.

### Option B: Cloudflare Tunnel

```bash
cloudflared tunnel --url http://127.0.0.1:8087
```

### Security Considerations

- Always use `--auth auto` (default) in public mode to require bearer token authentication
- Add trusted origins for CORS: `--allowed-origins https://elevenlabs.io`
- Rate limiting is enabled by default (60 req/s, burst 20)
- Never expose with `--auth none` in production

## Integrating with AI Agents

### ElevenLabs Conversational AI

1. Expose dir2mcp via HTTPS tunnel
2. Register the MCP server URL with ElevenLabs API
3. Create an agent with `native_mcp_server_ids` referencing your server
4. The agent automatically discovers and can call all dir2mcp tools

### Claude Code

```bash
claude mcp add --transport stdio dir2mcp -- dir2mcp up
```

### Mistral Vibe

Configure in your Vibe project settings with the streamable-http transport URL.

## Monitoring

Use `dir2mcp status` to check indexing progress at any time. The `dir2mcp.stats` MCP tool provides the same information programmatically.

## Troubleshooting

- **"MISTRAL_API_KEY not set"**: Ensure the environment variable is exported in the shell running dir2mcp
- **"connection refused"**: Check that the listen address matches what you're connecting to
- **"401 Unauthorized"**: Verify the bearer token matches `.dir2mcp/secret.token`
- **Slow indexing**: Large files (PDFs, audio) require API calls for OCR/transcription; this is expected

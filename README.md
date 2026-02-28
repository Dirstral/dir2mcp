# dir2mcp

`dir2mcp` is a deploy-first MCP server for private directory data.

## Current status

This repository is currently spec-first. The core implementation is planned as a Go single binary.

## Documentation

- `VISION.md`: product vision, principles, use cases, and roadmap
- `SPEC.md`: output/integration spec, tool contracts, config, security, and x402 requirements
- `ECOSYSTEM.md`: discovery/trust/metering/payment ecosystem framing
- `x402-payment-adapter-spec.md`: facilitator adapter contract for optional x402 mode

## Hackathon sequencing

- Day 1: core MCP + indexing + retrieval + citations
- Day 2 (optional): native x402 request gating via facilitator integration

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

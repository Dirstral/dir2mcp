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

## Recommended Codex MCP servers

For working on this repo in Codex, these MCP servers are useful:

- `everything`: MCP protocol reference server for compatibility checks
- `sequential-thinking`: structured planning/reasoning helper
- `playwright`: browser automation for local HTTP/docs smoke checks
- `github`: GitHub MCP endpoint for issues/PR/repo workflows

Install commands:

```bash
codex mcp add everything -- npx -y @modelcontextprotocol/server-everything
codex mcp add sequential-thinking -- npx -y @modelcontextprotocol/server-sequential-thinking
codex mcp add playwright -- npx -y @playwright/mcp
codex mcp add github --url https://api.githubcopilot.com/mcp/ --bearer-token-env-var GITHUB_PERSONAL_ACCESS_TOKEN
```

GitHub auth:

```bash
export GITHUB_PERSONAL_ACCESS_TOKEN=your_pat_here
```

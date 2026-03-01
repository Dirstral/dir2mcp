<p align="center">
  <img src="assets/logo.png" alt="dir2mcp logo" width="720" />
</p>

# dir2mcp

Deploy any local directory as an MCP knowledge server with indexing, retrieval, citations, and optional x402 request gating.

## Why dir2mcp

- Single Go binary (`dir2mcp`) with local-first state in `.dir2mcp/`
- MCP Streamable HTTP server with a stable tool surface
- Multimodal ingestion: text/code, OCR, transcripts, structured annotations
- Citation-aware retrieval and RAG-style answering
- Optional facilitator-backed x402 payment gating for `tools/call`

## Quickstart

```bash
make build
export MISTRAL_API_KEY=your_key_here
./dir2mcp up
```

Expected output includes endpoint and progress; connect your MCP client to the printed URL.

## CLI Commands

`dir2mcp` supports:

- `up`
- `status`
- `ask`
- `reindex`
- `config` (`init`, `print`)
- `version`

Notes:

- Running `dir2mcp` with no args prints usage.
- There is no `help` subcommand; use command-specific flags (for example `dir2mcp up --help`).

## MCP Tools

Current server tool names:

- `dir2mcp.search`
- `dir2mcp.ask`
- `dir2mcp.ask_audio`
- `dir2mcp.transcribe`
- `dir2mcp.annotate`
- `dir2mcp.transcribe_and_ask`
- `dir2mcp.open_file`
- `dir2mcp.list_files`
- `dir2mcp.stats`

## Configuration

Primary config file: `.dir2mcp.yaml`

Core environment variables:

- `MISTRAL_API_KEY` (required for Mistral-backed flows)
- `MISTRAL_BASE_URL` (optional, default `https://api.mistral.ai`)
- `DIR2MCP_AUTH_TOKEN` (optional auth token override)
- `DIR2MCP_ALLOWED_ORIGINS` (comma-separated additional browser origins)
- `DIR2MCP_X402_FACILITATOR_TOKEN` (optional x402 facilitator bearer token)
- `ELEVENLABS_API_KEY` / `ELEVENLABS_BASE_URL` (optional TTS/STT edge integration)

For `up`, token precedence is:

1. `--x402-facilitator-token-file`
2. `DIR2MCP_X402_FACILITATOR_TOKEN`
3. `--x402-facilitator-token`

## Security Defaults

- Default listen address is local (`127.0.0.1:0`)
- `--public` binds to `0.0.0.0` (unless explicit `--listen` provided)
- `--public` with `--auth none` is rejected unless `--force-insecure` is provided
- Browser origins are allowlisted (localhost defaults + explicit additions)

## Optional x402 Mode

x402 is optional and additive. Configure with `--x402 off|on|required` and facilitator settings.

- `off`: disabled
- `on`: fail-open if config is incomplete
- `required`: strict validation and gating

Reference: [x402-payment-adapter-spec.md](docs/x402-payment-adapter-spec.md)

## Project Status

### Implemented now

- Core CLI and MCP server lifecycle
- Local metadata/index state management
- Search/open/list/stats tooling
- Ask pipeline baseline with citations
- Transcript and annotation tool routes
- Optional x402 gating path and facilitator client integration

### In progress / milestone tracking

- [#12](https://github.com/Dirstral/dir2mcp/issues/12) Release-completion epic
- [#19](https://github.com/Dirstral/dir2mcp/issues/19) Hosted demo runbook + smoke script
- [#58](https://github.com/Dirstral/dir2mcp/issues/58) Docs alignment (this pass)
- [#70](https://github.com/Dirstral/dir2mcp/issues/70) Retrieval `Ask()` quality/completion
- [#71](https://github.com/Dirstral/dir2mcp/issues/71) Implement `retrieval.Service.Stats()`
- [#73](https://github.com/Dirstral/dir2mcp/issues/73) Merge `dirstral-cli` into monorepo

Release-complete target: all milestone issues above closed, `make check` green on `main`, and reproducible hosted smoke run passing.

## Documentation Map

- [docs/SPEC.md](docs/SPEC.md): output contracts, interfaces, schema and operational details
- [docs/VISION.md](docs/VISION.md): product direction and architectural principles
- [docs/ECOSYSTEM.md](docs/ECOSYSTEM.md): discovery, trust, metering, payment ecosystem framing
- [docs/x402-payment-adapter-spec.md](docs/x402-payment-adapter-spec.md): facilitator-facing x402 adapter contract

## Development

```bash
make check
```

For focused suites:

```bash
go test ./tests/mcp -run X402
go test ./tests/x402
```

Contributor/agent guides:

- [AGENTS.md](AGENTS.md)
- [CLAUDE.md](CLAUDE.md)

## License

MIT. See [LICENSE](LICENSE).

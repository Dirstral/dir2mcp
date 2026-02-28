# Tool Approval Policy

## Overview

This document defines the ElevenLabs tool approval policy for the dir2mcp MCP integration.
The policy controls which tools the agent can invoke automatically and which require explicit user consent.

## Policy: `require_approval_per_tool`

We use **per-tool approval** (not `auto_approve_all` or `require_approval_all`) because it provides the best balance of usability and safety.

### ElevenLabs Dashboard Configuration

| Field | Value |
|---|---|
| **Server-level approval mode** | `require_approval_per_tool` |

### Per-Tool Settings

| Tool | Approval | Execution Mode | Rationale |
|---|---|---|---|
| `dir2mcp.search` | `auto_approved` | `immediate` | Read-only semantic search. No side effects. Core to every Q&A flow. |
| `dir2mcp.ask` | `auto_approved` | `immediate` | Read-only RAG Q&A. Returns answers with citations. No side effects. |
| `dir2mcp.open_file` | `auto_approved` | `immediate` | Read-only file reader. Bounded by `max_chars` (default 20 KB, max 50 KB). No side effects. |
| `dir2mcp.list_files` | `auto_approved` | `immediate` | Read-only directory listing. Bounded by `limit` (max 5000). No side effects. |
| `dir2mcp.stats` | `auto_approved` | `immediate` | Read-only health check. Returns indexing counters and model names. No side effects. |

**All five tools are auto-approved** because:

1. **Every tool is read-only.** The dir2mcp server exposes zero write operations — no file creation, modification, deletion, or execution.
2. **Outputs are bounded.** Each tool has hard limits on response size (`max_chars`, `limit`, `k`) preventing unbounded data exfiltration.
3. **Secrets are redacted.** The server applies secret-pattern filtering (AWS keys, JWTs, API tokens, private keys) before returning content.
4. **Path traversal is blocked.** `open_file` rejects paths outside the indexed root directory (`PATH_OUTSIDE_ROOT` error).
5. **Sensitive files are excluded.** The indexer skips `.git/`, `.env`, `*.pem`, `*.key`, `id_rsa`, and other sensitive paths by default.

## Why Not `auto_approve_all`?

Although all current tools are safe, `require_approval_per_tool` provides a critical safeguard: if a future server update adds a write-capable tool, it will **default to requiring approval** until explicitly reviewed and approved. With `auto_approve_all`, any new tool — including destructive ones — would be auto-approved silently.

Additionally, ElevenLabs computes a SHA-256 hash of each tool's schema. If a tool's parameters or description change, the approval status can be re-evaluated, preventing a compromised or updated server from silently altering tool behavior.

## Why Not `require_approval_all`?

Requiring approval for every tool call would break the conversational flow. The agent needs to call `search` → `open_file` chains multiple times per question. Prompting the user for permission on each call creates friction that makes the demo unusable for voice conversations.

## Security Boundaries

### What the server already enforces

| Layer | Protection |
|---|---|
| **Authentication** | Bearer token required (constant-time comparison) |
| **CORS** | Origin allowlist (only `elevenlabs.io` and `api.elevenlabs.io` in production) |
| **Session management** | `MCP-Session-Id` with 24-hour TTL, automatic cleanup |
| **Path containment** | All file access scoped to the indexed root directory |
| **Secret redaction** | Patterns for AWS keys, JWTs, Bearer tokens, API keys, private keys |
| **File exclusion** | `.git/`, `.env`, `*.pem`, `*.key`, `node_modules/`, `vendor/` skipped during indexing |
| **Request size limit** | 1 MB maximum request body |
| **Public mode guard** | `--public` requires authentication; `--auth none` blocked unless `--force-insecure` |

### What the approval policy adds

| Layer | Protection |
|---|---|
| **Per-tool granularity** | New tools default to `requires_approval` |
| **Schema hash integrity** | Tool changes trigger re-review |
| **Execution mode control** | `immediate` mode prevents unnecessary latency |

## Recommended `tool_config_overrides`

```json
{
  "dir2mcp.search": {
    "execution_mode": "immediate",
    "approval_policy": "auto_approved"
  },
  "dir2mcp.ask": {
    "execution_mode": "immediate",
    "approval_policy": "auto_approved"
  },
  "dir2mcp.open_file": {
    "execution_mode": "immediate",
    "approval_policy": "auto_approved"
  },
  "dir2mcp.list_files": {
    "execution_mode": "immediate",
    "approval_policy": "auto_approved"
  },
  "dir2mcp.stats": {
    "execution_mode": "immediate",
    "approval_policy": "auto_approved"
  }
}
```

## Review Checklist

Before going live, verify:

- [ ] Server-level policy is set to `require_approval_per_tool` (not `auto_approve_all`)
- [ ] All five tools appear in the ElevenLabs dashboard after `tools/list` discovery
- [ ] Each tool shows `auto_approved` status
- [ ] Bearer token is configured in the dashboard Secret Token field
- [ ] Test a conversation: the agent should call tools without prompting for permission
- [ ] Add a new dummy tool to the server and verify it defaults to `requires_approval`

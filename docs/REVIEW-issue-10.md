# Review: Issue #10 (Config system + CLI UX)

Quick checklist vs [issue #10](https://github.com/Dirstral/dir2mcp/issues/10) and Samet’s Day 1 scope.

---

## Scope (issue #10)

| Requirement | Status | Notes |
|-------------|--------|--------|
| Precedence: flags > env > .dir2mcp.yaml > defaults | Done | `config.Load` + `Overrides` in `internal/config/load.go`; `up` passes flag overrides |
| `secrets` + `security` config blocks | Done | In `Config` struct and default YAML template |
| `config init` interactive wizard (masked, TTY aware) | Done | `cli/wizard.go` (ReadSecret, IsTTY); `config init` prompts when TTY and not `--non-interactive` |
| `config print` effective resolved config | Done | Load + SnapshotConfig (redacted) + YAML output |
| Non-interactive missing-config → exit 2 | Done | `Validate()` returns actionable error; CLI uses `ExitConfigInvalid` |

## Acceptance criteria

| Criterion | Status | Notes |
|-----------|--------|--------|
| Precedence behavior verified with tests | Done | `load_test.go`: TestPrecedence_FlagsOverrideEnv, TestPrecedence_EnvOverridesFile |
| Snapshot never stores plaintext secrets | Done | `SnapshotConfig` redacts; TestSnapshot_NeverStoresPlaintextSecrets |
| Missing required config yields actionable output | Done | `validate_test.go`: TestValidate_MissingConfigYieldsActionableOutput |

---

## Gaps / not in scope for #10

- **Progress line on `up`** — SPEC §3.1 wants a line like `Progress: scanned=… indexed=…`. Not implemented; indexer is a stub. Will come with Task 1.20 when the real indexer feeds progress.
- **NDJSON `index_loaded`** — Emit on `up` when index is loaded. We only emit `server_started` and `connection` in `--json` mode; `index_loaded` can be added when store/index exist.
- **config init fast path** — Hackathon plan: “fast path skips prompts if all required config already present”. We always show the optional API key prompt when TTY; we could skip it if `MISTRAL_API_KEY` is already set.
- **charmbracelet/huh** — Plan suggests huh for the wizard; we use `golang.org/x/term` for masked input. Acceptable for issue #10.

---

## CodeRabbit

There is no CodeRabbit config in this repo. To use Code Rabbit:

1. Install [CodeRabbit](https://github.com/apps/coderabbit) on the **Dirstral/dir2mcp** GitHub repo.
2. Open a PR (e.g. branch `fix/issue-10-config-system` → `main`).
3. CodeRabbit will comment on the PR with review suggestions.

Optional: add `.coderabbit.yaml` in the repo root to tune what it reviews (e.g. paths, severity).

---

## Summary

Issue #10 scope and acceptance criteria are covered. Remaining items (progress line, NDJSON index_loaded, optional wizard fast path) are follow-ups or depend on other tasks (indexer, store).

# Roadmap

Kern aims to be a fast, open, production-grade coding agent for the terminal.

## v0.2 — solid core (in progress)

- [x] Single static Go binary (Linux/Windows, x64/arm64), no cgo.
- [x] `opencode.json(c)` drop-in + `kern.json(c)` overlay, JSONC, `{env:}`, `{file:}`.
- [x] OpenAI-compatible and Anthropic streaming providers.
- [x] Tool loop: read, write, edit, bash, grep, glob, ls, todo, webfetch.
- [x] Permission rules (allow/ask/deny), workspace sandbox, `--auto`.
- [x] Sessions with resume, fork, list, export, import.
- [x] Console UI plus a full-screen TUI (unreleased; line interface via `kern mini`).
- [ ] Byte-safe output truncation in tools and agent summaries.
- [ ] Tool result pairing and stream-recovery fixes in the agent loop.
- [ ] Provider middleware: retry with backoff, response-timeout handling.
- [ ] Context compaction (summarize old turns, keep recent ones).
- [ ] Error classification with stable exit codes (`kerr`).
- [ ] CI on Go 1.26 with `go test -race` on Linux.
- [ ] First public release (v0.2.0-rc.1) and installer scripts.

## v0.2.x — surfaces

- [ ] Headless server (`kern serve`, HTTP/SSE events, token auth) and `kern attach`.
- [ ] Web UI (Vite + TypeScript, embedded into the binary).
- [ ] Desktop app (Wails v2 shell around the same web UI, Windows + Linux).

## v0.3 — multi-agent

- [ ] Role presets: plan (read-only), build, test, review.
- [ ] `task` tool: isolated subagent context with token/step/time budgets.
- [ ] Plan → build → test loop with bounded iterations.
- [ ] Google Gemini provider.
- [ ] MCP servers (`kern mcp`).

## v0.4 — depth

- [ ] LSP integration (diagnostics, hover, references).
- [ ] Plugin API (WASM or JSON-RPC subprocess).
- [ ] Git worktrees and PR workflows.
- [ ] Diff review UI and snapshots/undo.

## v1.0 — production

- [ ] Security audit and fuzzing.
- [ ] Benchmarks with performance budgets in CI.
- [ ] Docs site (EN + RU).
- [ ] Homebrew, Scoop, winget, AUR, Docker.
- [ ] Reproducible builds, SBOM, signed releases.

## Non-goals

- Telemetry by default.
- Vendor lock-in. Any OpenAI-compatible endpoint works.

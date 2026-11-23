# Roadmap

Kern aims to be a fast, open, production-grade coding agent for the terminal.

## v0.2 — solid core (done)

- [x] Single static Go binary (Linux/macOS/Windows, amd64/arm64), no cgo.
- [x] `opencode.json(c)` drop-in + `kern.json(c)` overlay, JSONC, `{env:}`, `{file:}`.
- [x] OpenAI-compatible, Anthropic and Google Gemini streaming providers.
- [x] Tool loop: read, write, edit, bash, grep, glob, ls, todo, webfetch,
      process (live shell output), joke.
- [x] Byte-safe output truncation in tools and agent summaries.
- [x] Tool result pairing repair after an interrupted turn.
- [x] Provider middleware: retry with backoff and jitter, response timeout.
- [x] Error classification with stable exit codes (`internal/kerr`).
- [x] Session event log with `Fold`/`AppendEvent`/`AppendCompact`.
- [x] Permission rules (allow/ask/deny), workspace sandbox, `--auto`.
- [x] Symlink-aware sandbox (component walk, catches dangling-link escapes).
- [x] Skills: markdown + frontmatter, project and global, `--skill`, `/skill`.
- [x] Full-screen TUI: Chat, Files, Diff, Procs, Skills tabs.
- [x] Four themes (amber, dark, light, blue) chosen in setup.
- [x] First-run wizard (`kern init` / `kern setup`) with live key check.
- [x] `~/.kern` home with `~/.config/kern` read as a legacy fallback.
- [x] CI on Go 1.27, `go test -race` on Linux, six-target cross-compile.
- [x] First preview release and installer scripts.

## v0.2.x — surfaces (done)

- [x] Headless server (`kern serve`, HTTP/SSE events, token auth).
- [x] Web UI (embedded, no build step, SSE streaming, permission modal).
- [x] Desktop launcher (`kern desktop`) opening the web UI in an app window.
- [x] Multi-agent roles: plan (read-only), build, test, review.
- [x] `task` tool: isolated subagent context with token/step/time budgets.
- [x] Credential discovery from opencode, `.env`, Codex, Continue, Gemini CLI, Aider.

## v0.3 — depth

- [ ] Context compaction (summarize old turns, keep recent ones).
- [ ] Plan → build → test loop with bounded iterations and automatic retry.
- [ ] MCP servers (`kern mcp`).
- [ ] LSP integration (diagnostics, hover, references).
- [ ] Native Wails desktop build (bundled window, no external browser).
- [ ] Plugin API (WASM or JSON-RPC subprocess).

## v1.0 — production

- [ ] Security audit and fuzzing.
- [ ] Benchmarks with performance budgets in CI.
- [ ] Docs site (EN + RU).
- [ ] Homebrew, Scoop, winget, AUR, Docker.
- [ ] Reproducible builds, SBOM, signed releases.

## Non-goals

- Telemetry by default.
- Vendor lock-in. Any OpenAI-compatible endpoint works.

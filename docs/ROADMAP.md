# Roadmap

Kern aims to be a fast, open, production-grade replacement for closed terminal coding agents.

## v0.1 — core (in progress)

- [x] Single static Go binary (Linux/Windows, x64/arm64).
- [x] opencode `opencode.json(c)` drop-in + `kern.json(c)` overlay.
- [x] JSONC, `{env:}` and `{file:}` substitution.
- [x] OpenAI-compatible and Anthropic streaming providers.
- [x] Tool loop: read, write, edit, bash, grep, glob, ls, todo, webfetch.
- [x] Permission rules (allow/ask/deny), workspace sandbox, `--auto`.
- [x] Sessions with resume, fork, list, export, import.
- [x] Console UI with Amber/Ember theme.
- [x] `run`, `models`, `config`, `doctor`, `auth`, `session`, `stats`.
- [ ] Full-screen TUI (panels, diff viewer, command palette).
- [ ] Image attachments.
- [ ] Context compaction.
- [ ] First public release and installers.

## v0.2 — ecosystem

- [ ] Google Gemini provider.
- [ ] MCP servers (`mcp` command).
- [ ] Subagents and the `task` tool.
- [ ] Slash commands and skills.
- [ ] Headless server (`serve`), `attach`, ACP.
- [ ] Native provider compaction.
- [ ] Plugin API (WASM or JSON-RPC subprocess).

## v0.3 — depth

- [ ] LSP integration (diagnostics, hover, references).
- [ ] Web search providers.
- [ ] Git worktrees and PR workflows.
- [ ] Diff review UI and snapshots/undo.
- [ ] Web UI (`web`).

## v1.0 — production

- [ ] Security audit and fuzzing.
- [ ] Benchmarks and performance budget in CI.
- [ ] Docs site (EN + RU).
- [ ] Homebrew, Scoop, winget, AUR, Docker.
- [ ] Reproducible builds, SBOM, signed releases.

## Non-goals

- A GUI-first experience. Kern is a terminal tool; a web UI comes later.
- Telemetry by default.
- Vendor lock-in. Any OpenAI-compatible endpoint works.

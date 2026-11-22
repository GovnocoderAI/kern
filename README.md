# Kern

> **The core of your terminal.**

Kern is a fast, open-source coding agent that lives in your terminal. It is built in Go, ships as a single static binary, and works as a drop-in companion for existing `opencode` configuration.

```
  ▄▄
 █  █  K E R N
 █▄▄█  the core of your terminal
```

[![CI](https://github.com/GovnocoderAI/kern/actions/workflows/ci.yml/badge.svg)](https://github.com/GovnocoderAI/kern/actions/workflows/ci.yml)
[![License](https://img.shields.io/badge/license-Apache--2.0-FFB000.svg)](LICENSE)
[![Go](https://img.shields.io/badge/go-1.23%2B-22D3EE.svg)](https://go.dev)

## Status

Kern is currently in **v0.1 development**. The core agent, providers, tools, permissions and console UI work end to end. The full-screen TUI, MCP, subagents, LSP and a web UI are on the [roadmap](docs/ROADMAP.md).

## Why Kern

- **Fast** — a single Go binary, no Node runtime, low memory, quick startup.
- **Compatible** — reads `opencode.json` / `opencode.jsonc` and `kern.json` / `kern.jsonc` without migration.
- **Capable** — streaming chat, tool calling, file editing, shell, search, sessions and permissions.
- **Portable** — Linux and Windows on x64 and arm64, with no cgo.
- **Yours** — Apache-2.0, no telemetry, keys stay on your machine.

## Features

- Streaming chat with **OpenAI-compatible** and **Anthropic** providers.
- Built-in tools: `read`, `write`, `edit`, `bash`, `grep`, `glob`, `ls`, `todo`, `webfetch`.
- Permission model: `allow` / `ask` / `deny` rules, workspace sandbox, `--auto` mode.
- Sessions: resume with `-c`, target with `-s`, fork, list, export and import.
- Console UI with the Amber/Ember palette, `/model`, `/models`, `/tools`, `/cost`.
- `kern run` for scripts and CI, plus a full interactive REPL.
- Config variable substitution: `{env:NAME}` and `{file:path}`.

## Install

### From source

Requires Go 1.23 or newer.

```bash
git clone https://github.com/GovnocoderAI/kern.git
cd kern
go build -ldflags "-s -w" -o bin/kern ./cmd/kern
```

### Scripts

Once releases are published:

```bash
# Linux / macOS (WSL)
curl -fsSL https://raw.githubusercontent.com/GovnocoderAI/kern/main/scripts/install.sh | sh
```

```powershell
# Windows (PowerShell)
irm https://raw.githubusercontent.com/GovnocoderAI/kern/main/scripts/install.ps1 | iex
```

## Quick start

```bash
kern                      # interactive session in the current project
kern ~/code/my-project    # start in a different project
kern run "explain this repository"
kern models               # list configured models
kern doctor               # diagnose config, providers and keys
```

Inside the interactive session:

```
/help              show commands
/model [ref]       show or switch model (provider/model)
/models            list configured models
/tools             list available tools
/session           show the current session
/cost              show token usage
/clear             clear the conversation
/exit              quit
```

## Configuration

Kern loads, from lowest to highest precedence:

1. System config (`%ProgramData%\opencode`, `%ProgramData%\kern`, `/etc/opencode`, `/etc/kern`).
2. Global config (`~/.config/opencode/opencode.json[c]`, `~/.config/kern/kern.json[c]`).
3. Project config (`opencode.json[c]`, `kern.json[c]`, `.opencode/`, `.kern/`) from the filesystem root down to your working directory.

Both the legacy opencode `provider` shape and the newer `providers` shape are supported.

```jsonc
{
  "$schema": "https://opencode.ai/config.json",
  "model": "plusvibe/claude-opus-5.5",
  "provider": {
    "plusvibe": {
      "npm": "@ai-sdk/openai-compatible",
      "options": {
        "baseURL": "https://plusvibeapi.ru/v1",
        "apiKey": "{env:PLUSVIBE_API_KEY}"
      },
      "models": {
        "claude-opus-5.5": { "name": "Claude Opus 5.5", "attachment": true }
      }
    }
  }
}
```

Overlay anything with `~/.config/kern/kern.json` or a project-level `kern.json`:

```jsonc
{
  "model": "plusvibe/claude-opus-5.5",
  "permissions": [
    { "action": "shell", "resource": "git push *", "effect": "ask" },
    { "action": "shell", "resource": "rm -rf *", "effect": "deny" }
  ]
}
```

### Credentials

Keys are resolved in this order:

1. `apiKey` in the provider config.
2. `~/.config/kern/auth.json` (created by `kern auth set`).
3. opencode's `~/.local/share/opencode/auth.json`.
4. Environment variables such as `OPENAI_API_KEY`, `ANTHROPIC_API_KEY`, `<PROVIDER>_API_KEY`.

```bash
kern auth set plusvibe sk-...   # stored with 0600 permissions
kern auth list
kern auth remove plusvibe
```

## Commands

| Command | Description |
| --- | --- |
| `kern [project]` | Start the interactive session |
| `kern mini [project]` | Start the minimal interactive interface |
| `kern run [message..]` | Run one prompt non-interactively |
| `kern models [provider]` | List models (`--remote` fetches from the provider) |
| `kern config path\|show\|validate` | Inspect resolved configuration |
| `kern doctor` | Check environment, config, providers and keys |
| `kern auth set\|list\|remove` | Manage provider credentials |
| `kern session list\|show\|delete\|export\|import` | Manage saved sessions |
| `kern stats` | Token usage across sessions |
| `kern agent` | List configured agents |
| `kern completion <shell>` | Generate shell completion |
| `kern init` | Create a starter `kern.json` |

### Global flags

`-m/--model`, `-c/--continue`, `-s/--session`, `--fork`, `--prompt`, `--agent`, `--auto`, `--mini`, `--pure`, `--print-logs`, `--log-level`, `--port`, `--hostname`, `--mdns`, `--mdns-domain`, `--cors`, `--no-replay`, `--replay-limit`.

## Permissions

By default, reads inside the workspace are allowed. Writes, edits, shell commands and web fetches ask for approval. Rules are evaluated last-match-first and support globs:

```jsonc
{
  "permissions": [
    { "action": "shell", "resource": "git status", "effect": "allow" },
    { "action": "shell", "resource": "rm -rf *", "effect": "deny" }
  ]
}
```

`--auto` approves anything not explicitly denied. Paths are confined to the workspace; symlink and `..` escapes are rejected.

## Architecture

```
cmd/kern            entrypoint
internal/cli        cobra commands and flags
internal/config     opencode + kern config loader, auth
internal/provider   OpenAI-compatible and Anthropic streaming
internal/agent      tool-calling loop and system prompt
internal/tools      file, shell and search tools
internal/permission allow/ask/deny rules
internal/session    persisted sessions
internal/ui         console presentation
```

## Security

- No telemetry. Nothing leaves your machine except provider requests you configure.
- Keys are never printed by `config show` and are stored with `0600` permissions.
- Tool paths are sandboxed to the workspace.
- See [SECURITY.md](SECURITY.md) for reporting vulnerabilities.

## Roadmap

See [docs/ROADMAP.md](docs/ROADMAP.md). Highlights: full-screen TUI, MCP, subagents, plugins, LSP, web search, headless server and a web UI.

## Contributing

Contributions are welcome. Start with [CONTRIBUTING.md](CONTRIBUTING.md).

## License

Apache-2.0. See [LICENSE](LICENSE).

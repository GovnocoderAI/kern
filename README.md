# Kern

Kern is a fast, open-source coding agent for the terminal. Built in Go, it ships
as a single static binary, streams chat and tool calls from any OpenAI-compatible
or Anthropic provider, and works as a drop-in companion for existing `opencode`
configuration files.

[![CI](https://github.com/GovnocoderAI/kern/actions/workflows/ci.yml/badge.svg)](https://github.com/GovnocoderAI/kern/actions/workflows/ci.yml)
[![License](https://img.shields.io/badge/license-Apache--2.0-FFB000.svg)](LICENSE)

## Status

Kern is in **v0.2**: the core agent, providers, tools, permissions, sessions,
the full-screen TUI, multi-agent roles, a local HTTP/SSE server, a web UI and a
desktop launcher all work end to end. LSP, plugins and MCP are on the
[roadmap](docs/ROADMAP.md).

## Features

- Streaming chat with **OpenAI-compatible**, **Anthropic** and **Google Gemini** providers.
- Built-in tools: `read`, `write`, `edit`, `bash`, `grep`, `glob`, `ls`, `todo`,
  `webfetch`, `process`, `joke`, plus `task` for delegation.
- **Multi-agent roles**: `plan` (read-only), `build`, `test`, `review`. The
  orchestrator delegates through the `task` tool with isolated context and
  token/step budgets; `--agent <role>` runs as a role directly.
- Permission model: `allow` / `ask` / `deny` rules, workspace sandbox, `--auto` mode.
- Sessions: resume with `-c`, target with `-s`, fork, list, export and import.
- **Three front ends**: full-screen TUI, `kern run` for scripts, and a local
  **web UI** served over HTTP/SSE (`kern serve`) with a **desktop** launcher
  (`kern desktop`) that opens it in an app window.
- **Skills**: markdown prompts with frontmatter, activated with `--skill` or `/skill`.
- **Credential discovery**: keys are read from Kern and opencode config, and also
  discovered from `.env` files, Codex, Continue, Gemini CLI and Aider.
- Reads `opencode.json` / `opencode.jsonc` and `kern.json` / `kern.jsonc` without migration,
  with `{env:NAME}` and `{file:path}` substitution.
- Four themes (amber, dark, light, blue) chosen in the first-run wizard.

## Install

### From source

Requires Go 1.26 or newer.

```bash
git clone https://github.com/GovnocoderAI/kern.git
cd kern
make build
```

### Once releases are published

```bash
# Linux (and macOS via a static build)
curl -fsSL https://raw.githubusercontent.com/GovnocoderAI/kern/main/scripts/install.sh | sh
```

```powershell
# Windows
irm https://raw.githubusercontent.com/GovnocoderAI/kern/main/scripts/install.ps1 | iex
```

## Quick start

```bash
kern                      # interactive session in the current project
kern mini ~/code/project  # minimal line interface
kern run "explain this repository"
kern models               # list configured models
kern doctor               # diagnose config, providers and keys
```

Inside an interactive session:

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
2. Global config (`~/.config/opencode/opencode.json[c]`, `~/.kern/config/kern.json[c]`);
   the older `~/.config/kern` is still read as a lower-precedence fallback.
3. Project config (`opencode.json[c]`, `kern.json[c]`, `.opencode/`, `.kern/`) from the filesystem root down to your working directory.

```jsonc
{
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

Overlay anything with `~/.kern/config/kern.json` or a project-level `kern.json`:

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

Keys are resolved in this order (first match wins):

1. `apiKey` in the provider config (`kern.json` / `opencode.json`).
2. `~/.kern/config/auth.json` (created by `kern auth set`).
3. opencode's `~/.local/share/opencode/auth.json`.
4. **Discovered from other tools** (lowest precedence): project and home `.env`
   files, OpenAI Codex (`~/.codex/auth.json`), Continue
   (`~/.continue/config.json`), Gemini CLI (`~/.gemini`), and Aider
   (`~/.aider.conf.yml`). A bare `<NAME>_API_KEY` from a known provider
   (`OPENAI_API_KEY`, `DEEPSEEK_API_KEY`, `GROQ_API_KEY`, …) also becomes a
   ready-to-use provider.
5. Environment variables such as `OPENAI_API_KEY`, `ANTHROPIC_API_KEY`,
   `GEMINI_API_KEY`, `<PROVIDER>_API_KEY`.

`kern doctor` lists every discovered credential and where it came from.

```bash
kern auth set plusvibe sk-...   # stored with 0600 permissions
kern auth list
kern auth remove plusvibe
```

## Commands

| Command | Description |
| --- | --- |
| `kern [project]` | Start the session (full-screen TUI where available) |
| `kern "prompt"` | Run one prompt non-interactively and print the answer |
| `kern mini [project]` | Start the minimal line interface |
| `kern tui [project]` | Start the full-screen terminal interface |
| `kern serve [project]` | Serve the session over HTTP/SSE (`--port`, `--open`) |
| `kern desktop [project]` | Serve and open the UI in an app window |
| `kern run [message..]` | Run one prompt non-interactively |
| `kern models [provider]` | List models (`--remote` fetches from the provider) |
| `kern config path\|show\|validate` | Inspect resolved configuration |
| `kern doctor` | Check environment, config, providers, keys and roles |
| `kern auth set\|list\|remove` | Manage provider credentials |
| `kern session list\|show\|delete\|export\|import` | Manage saved sessions |
| `kern stats` | Token usage across sessions |
| `kern agent` | List agent roles (plan/build/test/review) and custom agents |
| `kern skill list\|validate` | List and validate skills |
| `kern setup` / `kern init` | First-run wizard: theme, provider, key, model |
| `kern completion <shell>` | Generate shell completion |

### Global flags

`-m/--model`, `--agent <role>`, `-c/--continue`, `-s/--session`, `--fork`,
`--prompt`, `--auto`, `--mini`, `--skill`.

## Multi-agent roles

The orchestrator can delegate work to focused sub-agents through the `task`
tool. Each sub-agent runs with an isolated context and a bounded budget, and
returns only its condensed result, so the main conversation stays small.

| Role | Access | Steps | Purpose |
| --- | --- | --- | --- |
| `plan` | read-only | 15 | Inspect the repo and propose an ordered plan |
| `build` | read-write | 40 | Implement the smallest correct change |
| `test` | commands | 20 | Run tests and linters, report failures |
| `review` | read-only | 15 | Critique diffs for bugs and style |

Run as a role directly with `kern --agent plan "..."`, or let the orchestrator
delegate by calling `task` with a role and a self-contained prompt.

## Web UI and desktop

```bash
kern serve --port 8080 --open   # local server, prints a tokenised URL
kern desktop                    # opens the UI in an app window
```

The server streams turns over Server-Sent Events and requires the token it
prints (Bearer header or `?token=`) unless started with `--no-token`. The web
UI is embedded in the binary, so there is nothing to build or install.

## Permissions

By default, reads inside the workspace are allowed. Writes, edits, shell commands
and web fetches ask for approval. Rules are evaluated last-match-first and support
globs:

```jsonc
{
  "permissions": [
    { "action": "shell", "resource": "git status", "effect": "allow" },
    { "action": "shell", "resource": "rm -rf *", "effect": "deny" }
  ]
}
```

`--auto` approves anything not explicitly denied. File paths are confined to the
workspace; `..` escapes are rejected. Note: the sandbox resolves logical paths,
not the filesystem symlinks beneath them.

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
internal/app        composition root shared by the frontends
internal/tui        full-screen terminal interface
internal/ui         console presentation
```

## Security

- No telemetry. Nothing leaves your machine except the provider requests you configure.
- API keys are never printed by `config show` and are stored with `0600` permissions.
- File tool paths are sandboxed to the workspace.
- See [SECURITY.md](SECURITY.md) for reporting vulnerabilities.

## Roadmap

See [docs/ROADMAP.md](docs/ROADMAP.md). Next up: finishing the TUI, context
compaction, the headless server, a web UI, role-based subagents, LSP, plugins
and MCP.

## Contributing

Contributions are welcome. Start with [CONTRIBUTING.md](CONTRIBUTING.md).

## License

Apache-2.0. See [LICENSE](LICENSE).

# opencode parity

Kern mirrors the opencode command-line surface where it makes sense for a console-first agent.

## Global flags

| opencode | Kern | Status |
| --- | --- | --- |
| `-h, --help` | `-h, --help` | Implemented |
| `-v, --version` | `-v, --version` | Implemented |
| `--print-logs` | `--print-logs` | Accepted |
| `--log-level` | `--log-level` | Accepted |
| `--pure` | `--pure` | Accepted |
| `--port` | `--port` | Accepted (server planned) |
| `--hostname` | `--hostname` | Accepted (server planned) |
| `--mdns` | `--mdns` | Accepted (server planned) |
| `--mdns-domain` | `--mdns-domain` | Accepted (server planned) |
| `--cors` | `--cors` | Accepted (server planned) |
| `-m, --model` | `-m, --model` | Implemented |
| `-c, --continue` | `-c, --continue` | Implemented |
| `-s, --session` | `-s, --session` | Implemented |
| `--fork` | `--fork` | Implemented |
| `--prompt` | `--prompt` | Implemented |
| `--agent` | `--agent` | Accepted (single agent in v0.1) |
| `--auto` | `--auto` | Implemented |
| `--mini` | `--mini` | Implemented |
| `--no-replay` | `--no-replay` | Accepted |
| `--replay-limit` | `--replay-limit` | Accepted |

## Commands

| opencode | Kern | Status |
| --- | --- | --- |
| `opencode [project]` | `kern [project]` | Implemented |
| `run` | `run` | Implemented |
| `models` | `models` | Implemented |
| `providers` / `auth` | `auth` (+ `providers` alias) | Implemented |
| `agent` | `agent` | Lists agents |
| `session` | `session` | Implemented |
| `stats` | `stats` | Implemented |
| `export` / `import` | `session export` / `session import` | Implemented |
| `config` | `config` | Implemented (path/show/validate) |
| `doctor` | `doctor` | Implemented |
| `completion` | `completion` | Implemented |
| `init` | `init` | Implemented |
| `serve` | — | Planned (v0.2) |
| `web` | — | Planned (v0.3) |
| `attach` | — | Planned (v0.2) |
| `mcp` | — | Planned (v0.2) |
| `plugin` | — | Planned (v0.2) |
| `acp` | — | Planned (v0.2) |
| `github` / `pr` | — | Planned (v0.3) |
| `db` / `debug` | — | Planned |
| `upgrade` / `uninstall` | — | Planned (v1.0) |

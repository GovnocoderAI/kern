# Changelog

All notable changes to Kern are documented here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

- Single static Go binary for Linux and Windows (amd64, arm64).
- Drop-in `opencode.json` / `opencode.jsonc` loading with a higher-precedence `kern.json` / `kern.jsonc` overlay.
- JSONC support, `{env:NAME}` and `{file:path}` substitution.
- OpenAI-compatible and Anthropic streaming providers with tool calling.
- Tools: `read`, `write`, `edit`, `bash`, `grep`, `glob`, `ls`, `todo`, `webfetch`.
- Permission rules (`allow` / `ask` / `deny`), workspace sandbox and `--auto`.
- Interactive console session with the Amber/Ember theme.
- Session persistence, resume, fork, list, export and import.
- Commands: `run`, `models`, `config`, `doctor`, `auth`, `session`, `stats`, `agent`, `completion`, `init`, `mini`.
- Unit tests for config, permissions, providers, tools, sessions and the agent loop.

### Security

- No telemetry.
- API keys masked in `config show` and stored with `0600` permissions.
- Path sandbox rejects escapes outside the workspace.

## [0.1.0] - TBD

First public release.

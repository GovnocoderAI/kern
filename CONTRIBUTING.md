# Contributing to Kern

Thanks for helping build an open terminal coding agent.

## Prerequisites

- Go 1.26 or newer
- Git

## Development

```bash
git clone https://github.com/GovnocoderAI/kern.git
cd kern
make build
go test ./...
go vet ./...
```

Before sending a PR, run:

```bash
gofmt -w .
go vet ./...
go test ./...
go mod tidy -diff   # must produce no output
```

CI runs the same checks on Linux and Windows (plus `go test -race` on Linux),
and cross-compiles the binary for linux/windows on amd64/arm64.

## Project layout

| Path | Purpose |
| --- | --- |
| `cmd/kern` | Entrypoint |
| `internal/cli` | Cobra commands and flags |
| `internal/config` | opencode + kern config, auth |
| `internal/provider` | Streaming providers |
| `internal/agent` | Tool-calling loop and prompts |
| `internal/tools` | File, shell and search tools |
| `internal/permission` | allow/ask/deny rules |
| `internal/session` | Persisted sessions |
| `internal/app` | Composition root shared by the frontends |
| `internal/tui` | Full-screen terminal interface |
| `internal/ui` | Console presentation |

## Adding a provider

1. Implement `provider.Provider` (see `internal/provider/openai.go`).
2. Map it in `buildProvider` in `internal/cli/root.go`.
3. Add an SSE parsing test with `httptest`.
4. Document it in the README.

## Adding a tool

1. Implement `tools.Tool` and register it in `tools.NewRegistry`.
2. Map it to a permission action in `tools.Action`.
3. Keep output capped (see `maxOutputBytes`).
4. Add a test in `internal/tools`.

## Commit style

Use Conventional Commits:

```
feat: add Gemini provider
fix: correct shell permission glob
docs: document RU quickstart
test: cover Anthropic tool streaming
```

## Pull requests

- Keep the change focused.
- Add or update tests.
- Update `README.md` and `docs/` when behavior changes.
- Make sure `gofmt`, `go vet` and `go test ./...` pass.

## Code of conduct

By participating you agree to follow [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md).

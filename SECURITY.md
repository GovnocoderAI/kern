# Security Policy

## Supported versions

Kern is pre-1.0. Security fixes land on the `main` branch and the latest release.

## Reporting a vulnerability

Please do **not** open a public issue for security problems.

Use GitHub's private vulnerability reporting:
https://github.com/kern-ai/kern/security/advisories/new

Include:

- affected version or commit,
- a description of the issue and impact,
- reproduction steps or a proof of concept,
- any suggested fix.

You will get an acknowledgement as soon as possible. We aim to publish an advisory and a fix release after the report is validated.

## Security model

- **No telemetry.** Kern only talks to the model providers you configure (plus an update check if enabled manually).
- **Credentials.** API keys are resolved from provider config, `~/.config/kern/auth.json`, opencode's `auth.json`, or environment variables. Kern never prints full keys (`config show` masks them) and writes `auth.json` with `0600` permissions.
- **Workspace sandbox.** File tools reject paths that resolve outside the working directory, including `..` escapes.
- **Permissions.** Writes, edits, shell commands and web fetches require approval by default. Explicit `deny` rules always win, even with `--auto`.
- **Shell execution.** Commands run with a timeout and are killed on cancellation.

If you find a gap in these guarantees, report it through the private channel above.

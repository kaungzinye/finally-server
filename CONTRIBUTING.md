# Contributing to Finally Server

Finally Server is a public fork of Vikunja. This repository accepts changes for the narrow Finally API, private planning context, deployment safety, and compatibility with Finally clients.

Changes that apply to Vikunja in general should be proposed to [go-vikunja/vikunja](https://github.com/go-vikunja/vikunja). Keeping general fixes upstream reduces fork drift and benefits both projects.

## Before you start

- Search this fork and the upstream repository for related work.
- Open an issue before a large feature or data-model change.
- Use the private process in [SECURITY.md](SECURITY.md) for vulnerabilities.
- Never include credentials, OAuth codes, tokens, event content, private task data, or production configuration in an issue or pull request.

## Branch names

Use `<type>/<author>/<short-description>`.

- `feat/alice/calendar-provider`
- `fix/alice/token-revocation`
- `doc/alice/redis-setup`
- `feat/agent/finally-project-task-list` for an automated coding agent

Use `feat`, `fix`, or `doc` for the type. Use your GitHub handle for the author segment. Automated agents use `agent`.

## Development setup

Follow [README.md](README.md) for the local server setup. Tool versions come from `go.mod`, `package.json`, and the repository tool configuration.

Build and run focused backend tests through Mage:

```bash
mage build
mage test:web
mage test:feature
mage test:filter 'TestName'
```

For frontend work:

```bash
cd frontend
pnpm install
pnpm typecheck
pnpm test:unit
```

Use `mage lint` for Go changes. Use the frontend lint commands defined in `package.json` for Vue, TypeScript, and style changes.

New client routes belong under `/api/v2/finally`. Test successful requests, unauthenticated requests, and insufficient permissions. Preserve the narrow contract at `/api/v2/finally/openapi.json`.

## Pull requests

- Keep the change focused and explain whether it is fork-specific or suitable for upstream.
- Link the issue with `Fixes #123` when applicable.
- Include the exact test and build commands you ran.
- Document configuration, data retention, authorization, and source-availability effects.
- Preserve upstream copyright headers and license notices.
- Disclose meaningful AI assistance. You remain responsible for every submitted line.
- Use Conventional Commit subjects such as `feat:`, `fix:`, `docs:`, `test:`, or `chore:`.

## Contribution terms

By submitting a contribution, you confirm that you have the right to provide it and agree that it is licensed under the license covering the files you change. Most of this repository uses [AGPL-3.0-or-later](LICENSE). Some subdirectories carry separate license files. Identify copied or adapted work and preserve every required notice.

Participation is governed by the [Code of Conduct](CODE_OF_CONDUCT.md). Project decisions follow [GOVERNANCE.md](GOVERNANCE.md).

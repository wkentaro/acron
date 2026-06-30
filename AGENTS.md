# acron

## Development

Go project. The toolchain (go, gofumpt, golangci-lint, dprint, yamlfmt, yamllint) lives in the nix devshell, so run targets through it: `nix develop -c make help` lists the targets, and verify changes with `nix develop -c make lint && nix develop -c make test` before committing.

## Changelog

User-facing changes go in `CHANGELOG.md` under `## [Unreleased]` ([Keep a Changelog](https://keepachangelog.com/) format), with the PR number. At release, that section is promoted to the new version.

## Agent skills

### Issue tracker

Issues are tracked in GitHub Issues via the `gh` CLI; external PRs are also a triage surface. See `docs/agents/issue-tracker.md`.

### Triage labels

Default label vocabulary (needs-triage, needs-info, ready-for-agent, ready-for-human, wontfix). See `docs/agents/triage-labels.md`.

### Domain docs

Single-context: one `CONTEXT.md` + `docs/adr/` at the repo root. See `docs/agents/domain.md`.

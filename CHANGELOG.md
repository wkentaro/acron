# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- `acron config --help` now documents the full `[[job]]` config schema, rendered from the same source that seeds the `config edit` template (#92).
- README install section gives a concrete binary-download command (#90).

### Changed

- Reworked the README for clarity and completeness (#91).

## [0.1.0] - 2026-06-30

Initial release. acron is a command-line runtime that schedules unattended agent runs across systemd (Linux) and launchd (macOS), staying in the runtime path so every firing gets uniform supervision: overlap prevention, timeout, and log capture with run history.

### Added

- Wrapper execution model: the generated OS unit runs `acron run <job>`, which supervises each firing in-process rather than launching the agent directly (#1, #3, #6).
- Declarative single-file config: all Jobs live in one TOML file (default `~/.config/acron/config.toml`, overridable via `ACRON_CONFIG`), with `acron config show` and a validating, template-seeded `acron config edit` to inspect and modify it (#11, #43).
- `acron apply` reconciles the OS scheduler units to the config and auto-prunes acron-owned units no longer declared; `acron destroy` tears them down while leaving the config intact (#17, #36, #42).
- `acron status` reports each Job's apply state (applied / drifted / unapplied / orphaned / disabled) alongside its latest run, with next-fire and relative-time columns (#17, #24, #32, #39).
- `acron trigger <job>` fires a Job once, immediately and out of schedule, through the same lock, condition, timeout, and history as a scheduled firing (#21).
- Cron schedules with lists, ranges, and steps, named-field forms, and POSIX OR-semantics when both day-of-month and day-of-week are set (#10, #14, #18, #33).
- Optional condition command run before the agent at fire time; a firing it drops is recorded as a `skipped` run, and a broken condition is surfaced rather than silently swallowed (#12, #16, #55).
- Run history in `history.jsonl`: `acron history` shows an interleaved timeline across all Jobs, including in-flight runs (#25, #48, #52).
- `acron logs` selects and prints a run's log and follows a live run (#20, #29, #35).
- `acron show` prints a Job's generated unit and any drift; `acron apply --dry-run` previews the change as a git-style diff (#37, #40, #42).
- `acron run` resolves and reports the command it invokes, reports an `interrupted` status on Ctrl-C, and exits cleanly (#50, #58, #83).
- Per-Job environment propagation with override merging, and the working directory as a first-class field (#61, #62).
- Shell completion for job names, with cwd hints and install instructions (#45, #49, #53).
- Long help text and examples across the command set (#77, #78, #79, #80, #82, #87).
- Version stamped via ldflags at release, with a module-version fallback for `go install` builds (#70, #81, #89).

[unreleased]: https://github.com/wkentaro/acron/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/wkentaro/acron/releases/tag/v0.1.0

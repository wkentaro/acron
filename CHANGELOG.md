# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Fixed

- `acron apply`/`config edit` now reject a Job whose `agent` command name is empty (`agent = [""]`) at validation time, instead of accepting the config and failing at every firing ([#103](https://github.com/wkentaro/acron/pull/103)).
- `acron apply`/`config edit` now reject a negative `timeout` (e.g. `timeout = "-5m"`) at validation time; it previously passed validation but silently ran the agent unbounded, defeating the timeout guarantee (`0` remains the explicit opt-out) ([#108](https://github.com/wkentaro/acron/pull/108)).
- `acron apply`/`config edit` now reject a non-absolute `cwd` at validation time; a relative path previously passed validation (resolved against the shell that ran the command) but the generated unit resolves `WorkingDirectory` against `/`, so every scheduled firing failed silently ([#111](https://github.com/wkentaro/acron/pull/111)).
- `acron apply`/`config edit` now reject a Job `name` starting with `-` (e.g. `-nightly`) at validation time; such a name previously passed validation but the generated unit's `acron run -nightly` (and any manual `acron run`/`trigger`/`show`/`logs`/`history`) parses it as a flag, so the job could never fire ([#128](https://github.com/wkentaro/acron/pull/128)).

## [0.1.1] - 2026-07-01

### Added

- `acron config --help` now documents the full `[[job]]` config schema, rendered from the same source that seeds the `config edit` template ([#92](https://github.com/wkentaro/acron/pull/92)).
- README install section gives a concrete binary-download command ([#90](https://github.com/wkentaro/acron/pull/90)).

### Changed

- Reworked the README for clarity and completeness ([#91](https://github.com/wkentaro/acron/pull/91)).

## [0.1.0] - 2026-06-30

Initial release. acron is a command-line runtime that schedules unattended agent runs across systemd (Linux) and launchd (macOS), staying in the runtime path so every firing gets uniform supervision: overlap prevention, timeout, and log capture with run history.

### Added

- Wrapper execution model: the generated OS unit runs `acron run <job>`, which supervises each firing in-process rather than launching the agent directly ([#1](https://github.com/wkentaro/acron/pull/1), [#3](https://github.com/wkentaro/acron/pull/3), [#6](https://github.com/wkentaro/acron/pull/6)).
- Declarative single-file config: all Jobs live in one TOML file (default `~/.config/acron/config.toml`, overridable via `ACRON_CONFIG`), with `acron config show` and a validating, template-seeded `acron config edit` to inspect and modify it ([#11](https://github.com/wkentaro/acron/pull/11), [#43](https://github.com/wkentaro/acron/pull/43)).
- `acron apply` reconciles the OS scheduler units to the config and auto-prunes acron-owned units no longer declared; `acron destroy` tears them down while leaving the config intact ([#17](https://github.com/wkentaro/acron/pull/17), [#36](https://github.com/wkentaro/acron/pull/36), [#42](https://github.com/wkentaro/acron/pull/42)).
- `acron status` reports each Job's apply state (applied / drifted / unapplied / orphaned / disabled) alongside its latest run, with next-fire and relative-time columns ([#17](https://github.com/wkentaro/acron/pull/17), [#24](https://github.com/wkentaro/acron/pull/24), [#32](https://github.com/wkentaro/acron/pull/32), [#39](https://github.com/wkentaro/acron/pull/39)).
- `acron trigger <job>` fires a Job once, immediately and out of schedule, through the same lock, condition, timeout, and history as a scheduled firing ([#21](https://github.com/wkentaro/acron/pull/21)).
- Cron schedules with lists, ranges, and steps, named-field forms, and POSIX OR-semantics when both day-of-month and day-of-week are set ([#10](https://github.com/wkentaro/acron/pull/10), [#14](https://github.com/wkentaro/acron/pull/14), [#18](https://github.com/wkentaro/acron/pull/18), [#33](https://github.com/wkentaro/acron/pull/33)).
- Optional condition command run before the agent at fire time; a firing it drops is recorded as a `skipped` run, and a broken condition is surfaced rather than silently swallowed ([#12](https://github.com/wkentaro/acron/pull/12), [#16](https://github.com/wkentaro/acron/pull/16), [#55](https://github.com/wkentaro/acron/pull/55)).
- Run history in `history.jsonl`: `acron history` shows an interleaved timeline across all Jobs, including in-flight runs ([#25](https://github.com/wkentaro/acron/pull/25), [#48](https://github.com/wkentaro/acron/pull/48), [#52](https://github.com/wkentaro/acron/pull/52)).
- `acron logs` selects and prints a run's log and follows a live run ([#20](https://github.com/wkentaro/acron/pull/20), [#29](https://github.com/wkentaro/acron/pull/29), [#35](https://github.com/wkentaro/acron/pull/35)).
- `acron show` prints a Job's generated unit and any drift; `acron apply --dry-run` previews the change as a git-style diff ([#37](https://github.com/wkentaro/acron/pull/37), [#40](https://github.com/wkentaro/acron/pull/40), [#42](https://github.com/wkentaro/acron/pull/42)).
- `acron run` resolves and reports the command it invokes, reports an `interrupted` status on Ctrl-C, and exits cleanly ([#50](https://github.com/wkentaro/acron/pull/50), [#58](https://github.com/wkentaro/acron/pull/58), [#83](https://github.com/wkentaro/acron/pull/83)).
- Per-Job environment propagation with override merging, and the working directory as a first-class field ([#61](https://github.com/wkentaro/acron/pull/61), [#62](https://github.com/wkentaro/acron/pull/62)).
- Shell completion for job names, with cwd hints and install instructions ([#45](https://github.com/wkentaro/acron/pull/45), [#49](https://github.com/wkentaro/acron/pull/49), [#53](https://github.com/wkentaro/acron/pull/53)).
- Long help text and examples across the command set ([#77](https://github.com/wkentaro/acron/pull/77), [#78](https://github.com/wkentaro/acron/pull/78), [#79](https://github.com/wkentaro/acron/pull/79), [#80](https://github.com/wkentaro/acron/pull/80), [#82](https://github.com/wkentaro/acron/pull/82), [#87](https://github.com/wkentaro/acron/pull/87)).
- Version stamped via ldflags at release, with a module-version fallback for `go install` builds ([#70](https://github.com/wkentaro/acron/pull/70), [#81](https://github.com/wkentaro/acron/pull/81), [#89](https://github.com/wkentaro/acron/pull/89)).

[unreleased]: https://github.com/wkentaro/acron/compare/v0.1.1...HEAD
[0.1.1]: https://github.com/wkentaro/acron/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/wkentaro/acron/releases/tag/v0.1.0

# acron — Implementation TODO

Roadmap for the work remaining after the macOS (launchd) runtime. See
`DESIGN.md` for the full spec and `docs/adr/` for the decisions behind these.

## Done

- **Phase 0** — Go module; `config.toml` parsing + atomic validation; cobra
  command tree with the lipgloss help (matches `ihq`/`git-hunk`).
- **Phase 1 (macOS)** — `apply`/`destroy` (launchd plist generation,
  `launchctl bootstrap`/`bootout`, auto-prune); the `run` pipeline (overlap
  lock, timeout with SIGTERM→SIGKILL, combined-log capture, `history.jsonl`,
  retention); `status`; `logs`; cron→launchd calendar translation (single
  values).

## Next

### Linux / systemd

- [x] Implement the `systemd --user` scheduler (replace
      `internal/scheduler/unsupported.go`): generate `.service` + `.timer`,
      `daemon-reload`, `enable --now`, and prune within acron's namespace.
- [x] cron → systemd `OnCalendar` translation.
- [x] Catch-up: `Persistent=true` on the timer (launchd already catches up).

### Schedule completeness

- [x] Support lists, ranges, and steps (`*/15`, `1,2,3`, `9-17`): enumerate into
      multiple launchd `StartCalendarInterval` dicts; map to `OnCalendar`.

### Commands

- [x] `edit`: open the config in `$EDITOR`, validate on save (currently a stub).
- [ ] `status` / `list`: show next fire time.

### Conditions

- [x] `condition`: optional per-Job precondition command run in the wrapper before
      the agent (lock → condition → agent). Mirror systemd `ExecCondition=`: exit
      `0` proceeds, `1`-`254` records a `skipped` Run (`reason: condition`), `255`/
      signal is a `failure`. Add a `reason` field to `history.jsonl` (overlap
      skips now carry `reason: overlap`); skip caps independent from real Runs (50 each).
      Inherit `cwd`/`env`, no `{prompt}` substitution, bound by the Job `timeout`.
      See ADR-0010.

### Runtime polish

- [ ] Same-second log filename collision: sub-second timestamp or per-run
      suffix so back-to-back runs don't share a `<ts>.log`.
- [ ] Configurable retention (currently fixed at 50 runs).
- [ ] Decide `acron run` exit-code semantics for the scheduler (today it records
      status and exits 0).

### Release / docs

- [ ] `README.md` (replace the one-line stub): install, config schema, examples.
- [ ] goreleaser + release artifacts (and a Homebrew tap?).

## Deferred (per ADRs — only on a real need)

- Relative-interval scheduling (`every = "6h"`) — ADR-0005.
- Overlap policies other than skip (`queue`/`allow`) — ADR-0007.
- Failure notifications.
- System (root) privilege tier.
- Windows.

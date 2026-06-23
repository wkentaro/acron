# `logs`: select a run by its fired time, not an ordinal index

`acron logs` identifies a past Run by the fired-time timestamp the CLI displays,
never by a positional ordinal. The selector grammar is `acron logs <job>`
(newest Run with output), `acron logs <job> latest` (the same, explicit), or
`acron logs <job> "2026-06-23 19:20:57"` (a specific Run by its displayed
`WHEN`). The earlier `acron logs <job> 3` form ("the 3rd most recent Run") is
removed.

The governing principle is **a Run's handle must be stable**:

- **Stable, not positional.** An ordinal drifts: Run `3` today is Run `4` after
  the next firing, so a number a user copied moments ago silently points
  elsewhere. The fired-time timestamp never moves, and it round-trips — the exact
  string `history` and `status` print is the string `logs` accepts. This follows
  the prior art for run/log tooling: `journalctl` selects by time window,
  `gh run view` by a stable run ID; neither exposes "the Nth most recent."
- **Newest is the default, so the timestamp form is rare.** `logs <job>` with no
  selector resolves to the newest Run that produced output, covering the
  overwhelmingly common case. The explicit timestamp is only needed to reach an
  older Run — exactly the case where an ordinal's instability would bite hardest.
- **A one-line run summary prints to stderr.** Before streaming the log body,
  `logs` writes `job  WHEN  STATUS  in DURATION` (e.g.
  `process-prs  2026-06-23 19:20:57  success  in 4m12s`) to stderr, orienting the
  reader the way `gh run view`'s header does. It goes to stderr, not stdout, so
  the body stays a clean payload for `acron logs <job> | grep …` or `> run.log`,
  matching the convention the `--follow` footer already set.

## Considered Options

- **Ordinal vs. timestamp as the canonical selector.** Timestamp. Stability beats
  the few keystrokes an ordinal saves; an unstable selector is a correctness
  hazard, not merely an ergonomic one.
- **Keep the ordinal as convenience sugar alongside the timestamp.** Rejected. Two
  grammars for one selection, one of them subtly wrong under concurrent firings,
  is not worth the saved typing — and the newest-Run default already removes the
  need to count back to recent Runs.
- **Fuzzy / prefix timestamps (`logs job 19:20`).** Deferred. Speculative until
  the full-timestamp form proves painful in practice; it adds parsing ambiguity
  for a selector that is rarely reached.
- **Summary header on stdout vs. stderr.** stderr. Keeping stdout the pure log
  body preserves pipe and redirect behavior; a terminal user still sees the
  header inline.

## Consequences

- `resolveLog` loses its integer-index branch (`logByIndex`) and the matching
  error messages, reducing to newest / `latest` / timestamp.
- `history` no longer needs an index column, which frees it to become a flat
  interleaved table (ADR 0016).
- Reaching a specific older Run requires typing its timestamp; the no-arg /
  `latest` default keeps the common case a single word.

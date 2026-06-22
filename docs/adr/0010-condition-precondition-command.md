# Condition: a fire-time precondition command gating the agent

A Job may declare an optional `condition`: a command run at fire time, in the
`acron run` wrapper, before the agent. Its exit code decides whether the agent
runs at all, letting a Job be scheduled frequently but do work only when there
is work to do (e.g. `gh pr list | grep -q .` — skip unless a PR exists).

The contract mirrors systemd's `ExecCondition=`, since acron already compiles to
systemd units:

- **Exit code → Run outcome.** `0` proceeds to the agent; `1`–`254` is a clean
  skip recorded as a `skipped` Run; `255` or death by signal/timeout is a
  `failure` (the check itself is broken, so it surfaces rather than masquerading
  as "no work to do").
- **Order in the wrapper:** lock → condition → agent. Overlap-skip takes
  precedence: a held lock drops the firing before the condition runs, so an
  in-progress Run never pays the condition's cost (often a network call).
- **Recording.** One `skipped` status, with a new `reason` field on the
  `history.jsonl` record distinguishing `overlap` from `condition`. A clean skip
  writes no log file (like overlap-skip, and like systemd's quiet
  `inactive (dead)`); a condition-`failure` writes a log so the broken check can
  be diagnosed.
- **Retention.** Two independent caps — last 50 real Runs and last 50 skipped
  Runs — so a frequently-skipping Job can never evict its real Runs from history.
- **Execution context.** The condition inherits the Job's `cwd` and `env`, gets
  no `{prompt}` substitution (it is a shell check, not an agent invocation), and
  is bounded by the same `timeout` value applied to the condition phase
  independently; `timeout = 0` leaves it unbounded.

## Considered Options

- **Where it runs — wrapper vs. delegating to systemd `ExecCondition=`.** We run
  it in the `acron run` wrapper. launchd has no `ExecCondition` equivalent, so
  delegating would fork the semantics across OSes, and a systemd-level skip never
  reaches acron to be written into Run history (ADR-0001, ADR-0007 keep acron in
  the runtime path). One wrapper path, identical cross-OS, owns its own history.
- **Exit-code contract.** Rejected the plain shell `&&` model (any nonzero =
  skip, never a failure) because it cannot tell "no work to do" from "the check
  is broken." Rejected a stricter "only `1` skips, `2`+ fails" variant because it
  is our invention and would surprise anyone expecting systemd semantics. Adopted
  the documented `ExecCondition=` split verbatim.
- **A distinct `gated` status.** Rejected. Across systemd, GitHub Actions,
  Jenkins, Airflow, and GitLab CI, skip-vs-fail is always a distinct status but
  the skip _cause_ never is — it lives in a separate field. We followed suit.

## Consequences

- **A broken check that exits `1`–`254` skips silently forever.** A typo'd
  condition or an unauthenticated `gh` (exit `127`) reads as a clean skip, not a
  failure. This is inherited from systemd's contract; only `255`/signal is loud.
- The `history.jsonl` schema gains a `reason` field. New overlap skips carry
  `reason: "overlap"`; older records simply omit it (the empty reason is the
  zero value and is tolerated on read). Existing history is not migrated.

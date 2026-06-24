# `logs --follow`: tail the live Run, named by the lock file

`acron logs <job> --follow` (`-f`) attaches to the Run in flight and streams its
agent transcript to stdout from the start of the Run until the Run finishes,
then prints a one-line status footer to stderr and exits 0. The mental model is
`journalctl -f`: follow the live Run, leave when it ends. This realizes the
feature ADR-0012 deferred.

A live Run is invisible to plain `logs`: the history Record is appended only
when the Run finishes (ADR-0007's end-written, append-only history), so the
in-flight Run has a log file on disk but no Record to select. `--follow` instead
reads which log is live from the Job's lock file, which now **carries the
in-flight Run's log file name**. The lock file was contentless; the runner
truncates it at lock-acquire and stamps the agent log's name when the agent
starts. The reader's view of the lock becomes:

| Lock | Contents | Meaning          | `--follow` action                        |
| ---- | -------- | ---------------- | ---------------------------------------- |
| free | —        | no Run in flight | error at attach / Run done while tailing |
| held | empty    | Condition check  | wait for the agent to start              |
| held | filename | agent streaming  | tail it from the start                   |

The governing principle is **`--follow` attaches to the live Run, and only the
live Run**:

- **Errors when nothing is live at attach time.** `no run in progress for
  "<job>"`. `--follow` never degrades to printing the last finished Run; that is
  what plain `logs` is for.
- **Refuses an explicit run selector.** An index or timestamp names a finished
  Run, which never grows, so following it is meaningless. `latest`/no-selector
  is fine (it resolves to the live Run).
- **Streams from the start of the Run**, not the tail. An agent Run is a
  bounded transcript; `logs <job>` and `logs <job> -f` show the same bytes,
  differing only in that `-f` keeps streaming past the current end.
- **Exit code 0, status footer on stderr.** Consistent with plain `logs` (exit
  code means "the command worked", not "the Run succeeded"). Stdout stays
  byte-for-byte the agent's own output (pipe-safe); the footer (`run failure
  (exit 1) in 4m12s`) tells an interactive watcher how it ended without a
  follow-up `acron status`.
- **Waits through the Condition check, then degrades.** While the lock is held
  but no agent log exists yet (the Run is still in `condition`), `--follow` waits
  with a `waiting for condition...` notice on stderr. If the Run ends before any
  agent transcript streams (a skip, a buffered condition-failure, or a Run that
  finished as we attached), it falls back to printing that Run's complete log if
  it has one, then the footer, rather than erroring after having reported the
  Run as live.

## Considered Options

- **Identifying the live log — runner records it vs. reader heuristic.** The
  runner stamps the live log name into the lock file; the reader reads a fact.
  The alternative is a pure-reader heuristic: "the newest `<timestamp>.log` not
  yet in `history.jsonl`, gated by the lock." That heuristic already existed —
  ADR-0012 used it for `status`'s in-flight start time — but it guesses: it must
  special-case the Condition window (no log yet), and it cannot distinguish a
  genuinely live log from one a killed Run orphaned without a Record (excluded
  only because the lock happens to be free). Stamping the name removes the guess
  and gives one source of truth. This ADR therefore **supersedes the in-flight
  log identification in ADR-0012/0011**: `RunningSince` (what `status` reports)
  now reads the lock file too, so the directory-scan heuristic is gone and both
  readers agree by construction. The cost is that `--follow` and `status` now
  depend on a runner-side on-disk convention (the lock file's contents), which
  the runner and readers must keep in step — the reason this is an ADR.
- **Nothing live → error vs. wait-for-next-run.** We error. Blocking until the
  scheduler next fires (possibly hours later) is a real feature but a separable
  one; folding it in would make `--follow` sometimes hang with no clear exit.
- **Status footer vs. Run-status-as-exit-code.** The footer is informational and
  keeps the exit code meaning "the command worked". Mirroring the Run's status
  into the exit code (for `logs -f && deploy`) conflates two things and diverges
  from plain `logs`; if wanted, it is a separable explicit flag.
- **Poll vs. `fsnotify`.** A ~200ms stdlib poll, using the existing lock probe
  as the authoritative "Run finished" signal. For a single-file tail a human is
  watching, 200ms is imperceptible, and it keeps acron's dependency set small.

## Consequences

- **The lock file is no longer contentless.** It holds the in-flight Run's log
  name (empty during the Condition check). Stale contents from a finished Run
  are harmless: every reader gates on the held lock first, and the next Run
  truncates the file at acquire.
- **`status`'s in-flight start time now comes from the lock file**, not a
  directory scan. Behavior is unchanged for a normally streaming agent; during
  the Condition check the start is unknown (zero) exactly as before.
- **`--follow` depends on the runner stamping the lock file.** An agent log
  produced by a runner that did not stamp the name (e.g. an in-flight Run
  started by an older binary across an upgrade) reads as "Condition check" until
  it finishes, then degrades to the finished-Run path — no crash, just no live
  tail for that one Run.

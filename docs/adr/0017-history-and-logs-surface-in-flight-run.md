# `history` and `logs` surface the in-flight Run

`acron history` shows a Job's in-flight **agent-phase** Run as a `running` row at
the top of the timeline, and `acron logs <job> <timestamp>` resolves that Run by
its fired time. Both read the same live state `status` already uses (ADR 0014):
the held lock, with the start time recovered from the in-flight log name.
Nothing new is written — a Run still gains its `history.jsonl` record only at
completion (ADR 0007), and `running` is never a stored status (CONTEXT: Run).

Before this, only `status` knew a Run was live. `history` read `history.jsonl`
alone, so an in-flight Run was invisible until it finished, and its fired-time
handle did not resolve in `logs` until the record existed — a hole in the
round-trip the timeline promises (ADR 0016).

The governing principle is **the in-flight Run is part of a Job's history, the
moment it is addressable**. This follows the container-lifecycle prior art: `docker
ps -a` lists running and exited instances in one table distinguished by status,
and `docker logs <id>` reads either.

- **`history` adds at most one `running` row per Job**, since one lock means one
  in-flight agent-phase Run. It sorts to the top by start time and is subject to
  `--limit` like any other row (it is the newest, so the cap never drops it).
  `STATUS` is `running`; `DURATION` is `—` (no final duration yet, as for a skip,
  ADR 0016); `PASSED` is elapsed-since-start, which doubles as how long the
  agent has been running.
- **A Run still in `condition` is omitted**, not shown. Its start time is
  unknown until the agent log exists (CONTEXT: Run), so it has no `WHEN` — and
  `WHEN` is both the sort key and the `logs` selector (ADR 0015), which must
  round-trip. A row you cannot address is worse than a row deferred for the few
  seconds until the agent starts; the Run is fully captured once it finishes.
  `status` legitimately shows that phase as `condition` because it is a
  one-row-per-Job current-state view, not a time-keyed log.
- **`logs` resolves the in-flight Run by timestamp, but `latest` does not.** An
  explicit fired-time selector returns a snapshot of the partial live log (with a
  `running` summary reporting elapsed-so-far instead of a final duration), keeping
  the timeline's timestamp addressable. `latest` stays on the newest finished Run,
  because `--follow` is the purpose-built verb for attaching to a live Run (ADR
  0013); redefining `latest` would overlap it and surprise anyone wanting the last
  completed output.

## Considered Options

- **Synthesize at read vs. write a `running` record at trigger time.** Synthesize.
  Persisting a mutable in-progress record would fight `history.jsonl`'s
  append-at-completion model (ADR 0007) and duplicate the live state `status`
  already derives from the lock.
- **Show the Condition-check Run with a blank `WHEN` vs. omit it.** Omit. A history
  row with no addressable timestamp breaks the round-trip ADR 0016 and 0015 rest
  on; the window is seconds.
- **Make `latest` resolve the running Run vs. leave it on finished output.** Leave
  it. `--follow` is the live verb; the round-trip is satisfied by the explicit
  timestamp without redefining `latest`.

## Consequences

- `runHistory` probes `RunningSince` per Job (the same cheap lock read `status`
  does) and appends one synthesized row when the agent start is known; that row
  renders `running`/`—` instead of going through
  `renderStatus`/`renderRunDuration`, since `running` is not a stored `Status`.
- `resolveLog` reports whether the chosen Run is in flight so `logs` can render the
  live summary; `logByTimestamp` falls through to the in-flight Run, ahead of the
  empty-history guard so a Job's first, still-running Run is addressable.

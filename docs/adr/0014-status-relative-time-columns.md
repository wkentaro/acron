# `status`: relative-time columns `PASSED` and `LEFT`

`acron status` pairs each absolute timestamp column with a relative one, in the
style of `systemctl list-timers`: `LAST` gains `PASSED` (elapsed since the run
start, e.g. `1h 35min ago`) and `NEXT` gains `LEFT` (time until the next fire,
e.g. `12min 30s`). The table reads `JOB APPLY STATUS LAST PASSED NEXT LEFT`, with
each relative cell immediately to the right of the absolute column it annotates.
Relative time answers "is this overdue?" / "did it just run?" at a glance,
without arithmetic against the wall clock.

The governing principle is **the relative columns are additive, never a
replacement**:

- **Paired, not replacing.** The absolute `LAST`/`NEXT` cells are byte-for-byte
  unchanged and remain the copy-pasteable run selectors that `acron logs` and
  `acron history` accept. A relative column would be useless as a selector (it
  drifts every second), so it annotates rather than supplants the absolute one,
  rendered in the same dim style so the timestamp stays the visual anchor.
- **Always on, no flag.** Both columns render unconditionally. A `--relative`
  flag was considered (the 5-column table was already wide) but rejected: the
  signal is the point of the screen, a hidden-by-default glance value is a
  contradiction, and a flag is a separable addition if width ever forces it.
- **A relative cell is shown exactly when its absolute partner has a value**, so
  the two columns always agree on which rows are populated. A never-run job has a
  blank `LAST`, so `PASSED` is blank. A job with no computable next fire (drifted
  / unapplied / orphaned / disabled, all rendering `NEXT` = `—`) has `LEFT` = `—`,
  mirroring `NEXT`. A running job's `LAST` is its start timestamp, so `PASSED`
  shows elapsed-since-start, doubling as how long it has been running. Each cell
  derives from the same source as its partner (`PASSED` from the `LAST`
  timestamp, `LEFT` from the computed next fire) and all rows use the single
  `now` snapshot `status` already threads, so a relative cell can never disagree
  with the absolute one beside it.
- **Hand-rolled duration formatter, ladder capped at days.** Units are `s`,
  `min`, `h`, `day`/`days` — no weeks/months/years. A duration coarsens to the
  largest non-zero unit plus the next-smaller unit when it is non-zero
  (`2min 34s`, `1h 35min`, `3 days 4h`, `45 days`); sub-minute durations render
  as bare seconds (`12s`, `0s ago`). The `day` unit pluralizes and carries a
  space; `s`/`min`/`h` do not, matching systemd.

## Considered Options

- **Add vs. replace the absolute columns.** Add. The absolute timestamps must
  stay because they are the `logs`/`history` selector grammar; relative time
  cannot be parsed back into a selector. This is the whole reason the column
  rename that split this work out (`LAST RUN`/`WHEN` -> `STATUS`/`LAST`) kept the
  absolute values.
- **Always-on vs. behind `--relative`.** Always-on. Hiding the at-a-glance signal
  behind a flag defeats its purpose, and the two extra columns fit. A flag is a
  clean, separable follow-up if terminal width ever forces a choice.
- **Unit ceiling at days vs. weeks/months/years.** Days. Every unit up to a day
  is exact; weeks are arguable and months/years require approximate-calendar
  arithmetic (a "month" is not a fixed span). A 45-day gap reads `45 days ago`,
  never `1 month 15 days`. The ceiling keeps the formatter pure integer math with
  no calendar dependency.
- **Hand-rolled formatter vs. a humanize dependency.** Hand-rolled. The format is
  a few dozen lines with one focused unit test, and ADR-0009 favors a minimal
  dependency set; pulling a humanize library for this would be disproportionate.
- **No "overdue" highlighting.** For an applied job `NEXT` is always a future
  fire time (the scheduler computes the next fire strictly after `now`), so there
  is no overdue state to color. `LEFT` is therefore always a positive remaining
  time; the formatter clamps any non-positive input to `0s` defensively against
  clock skew.

## Consequences

- **The status table is 7 columns wide.** Wider output; on a narrow terminal the
  table may wrap. This is the cost of always-on relative time, accepted over a
  flag.
- **`renderLastRun` and `renderNext` each emit their relative cell.** They now
  take / use the shared `now` and return the paired value alongside the absolute
  one, so the two cannot diverge. `renderLastRun` parses the run-start timestamp
  it previously only reformatted, to compute `PASSED` from the same instant.
- **A new internal duration formatter** (`formatDuration`) implements the ladder
  rules, covered by its own unit test (sub-minute, minutes+seconds, hours+minutes,
  multi-day, the `1 day`/`2 days` boundary, `0s`, and negative-clamp).

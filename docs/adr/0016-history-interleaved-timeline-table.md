# `history`: one interleaved, time-ordered table

`acron history` renders every job's Runs as a single flat table interleaved by
fired time, newest first, with columns `JOB WHEN PASSED STATUS DURATION`. It
defaults to the 20 most recent Runs across all jobs; `--limit N` sets the count
and `--limit 0` removes the cap. `acron history <job>` is the same table
filtered to one job, with the `JOB` column kept. This replaces the previous per-job sections with the
one-row-per-Run shape that mirrors `status`'s one-row-per-job table.

The governing principle is **run history is a timeline, narrowed by filtering**:

- **Interleaved, not grouped.** A multi-run-across-entities view is, in the prior
  art, a single time-ordered timeline: `journalctl` (no `-u`), `gh run list`, and
  `kubectl get events` all interleave and expect you to filter to narrow.
  Grouping is the idiom for one-row-per-entity _status_ views (`systemctl
  list-timers`, `acron status` itself), not for run history. `history <job>` is
  the filter, mirroring `journalctl -u` / `gh run list --workflow`.
- **A default limit of 20, `--limit N` to override.** Interleaving makes a default
  limit load-bearing. Condition-gated jobs fire far more often than they do
  work — an every-5-minute `process-prs` whose firings are mostly `skipped` would
  otherwise bury every other job in an unbounded dump. A numeric `--limit`
  (default 20, `0` for the full record) mirrors `gh run list -L` / `journalctl
  -n`: one knob that subsumes a boolean `--all` and also serves "show me 100."
- **Every outcome shown, including skips.** A `skipped` Run is a real outcome, not
  noise; hiding it would let `history` and `status` disagree about the latest
  Run. acron's structurally-high skip rate is the Condition feature working as
  intended. A `--status` filter is the prior-art escape hatch (`gh run list` has
  one) if the volume ever bites, deferred until it does.
- **The `JOB` column stays even when filtered.** `history <job>` is then literally
  the unfiltered table with rows removed — one render path, no special case. The
  repeated value is mild; `gh run list --workflow` keeps its `WORKFLOW` column the
  same way.

## Columns

- **`JOB`** — required by the interleaved view.
- **`WHEN`** — the absolute fired time (`2006-01-02 15:04:05`). It is the `logs`
  selector (ADR 0015), so it must round-trip; it is the visual anchor.
- **`PASSED`** — relative elapsed since the Run started, the same `PASSED` column
  `status` carries (ADR 0014), reusing its `formatDuration` output. Named to match
  `status` rather than a history-local `AGO` — one name for one concept. It
  annotates `WHEN`, as `status`'s `PASSED` annotates `LAST`.
- **`STATUS`** — the outcome with its reason, e.g. `success`, `failure`,
  `skipped (condition)`.
- **`DURATION`** — how long the agent ran; `—` for a skipped Run, whose start
  equals its end.

`EXIT` is omitted (subsumed by `STATUS`, and visible in the `logs` summary), and
there is no log-path column — that is what `logs` is for.

## Considered Options

- **Interleaved vs. grouped-by-job.** Interleaved. Grouping is the
  one-row-per-entity idiom leaking into a multi-run view; the run-history prior
  art (journalctl / gh / kubectl events) uniformly interleaves and narrows by
  filtering.
- **Numeric `--limit N` vs. a boolean `--all`.** `--limit N` (default 20). It
  matches `gh run list -L` / `journalctl -n`, and a single numeric knob subsumes
  `--all` — `--limit 0` removes the cap — while also serving "show me 100."
  Without any cap the highest-frequency job drowns the table.
- **Hide skips vs. show all outcomes.** Show all. Suppressing the dominant
  outcome would desync `history` from `status`'s latest Run and surprise the
  reader; a status filter is a separable later addition.
- **Drop the `JOB` column when filtered.** Keep it. A second render path to
  remove one repeated column is not worth it.

## Consequences

- `renderJobHistory` and per-job sectioning are replaced by a single table built
  like `statusTable()`, reusing the status table styling and the `formatDuration`
  "ago" formatter.
- `history` gains a `--limit N` flag (default 20, `0` for no cap); the default
  path sorts all jobs' Records by start time descending and truncates to the
  limit.
- No index column means nothing in `history` feeds a positional selector,
  consistent with ADR 0015.

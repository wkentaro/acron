# status reports Apply state; list removed

acron's read surface collapses to one command, `acron status`, which reports two
independent axes per Job: its **Apply state** (the Job's relationship to the
acron-owned units installed on this machine) and its latest **Run status** (the
outcome of its last execution, from Run history). `acron list` is removed.

A read command earns a place in the CLI only if it shows something `cat`-ing the
Config cannot — either by joining another source or by computing a translation.
`list` did neither: it pretty-printed the Config (name, schedule, enabled), which
is `cat` with lipstick. `status` already joined Run history, and now also joins
the installed units, so it earns its keep on both axes.

The four **Apply states** are computed from the _same comparison `apply`
performs_, so they can never diverge from `apply`'s own behavior: a Job is
`applied` exactly when `apply` would be a no-op for it.

- `applied` — units match the Config and the timer is loaded and active.
- `drifted` — `apply` would rewrite or restart the units: the Config, the
  apply-time environment snapshot (ADR-0006), or the timer's active state no
  longer matches what is installed.
- `unapplied` — declared in the Config, not yet installed.
- `orphaned` — units still installed for a Job no longer in the Config (what
  `apply` auto-prunes, ADR-0008).
- `disabled` — the Job sets `enabled = false`, so its reconcile target is
  "absent"; `apply` keeps it uninstalled. Units lingering from before it was
  disabled read as `drifted` (apply would remove them). A fifth state is needed
  because the other four assume a Job _wants_ to be installed, which inverts for
  a disabled Job: labeling it `unapplied` would wrongly imply `apply` installs it.

Because orphans have no Config entry to iterate from, `status`'s rows are the
union of Config Jobs and acron-owned installed units, not just the Config.

## Considered Options

- **Removing `list` vs. repurposing it as a sync view.** Removed outright. The
  alternative — making `list` show installed/drift state — is real future value,
  but folding that signal into `status` (which already reads beyond the Config)
  keeps the read surface to one command. `list` can be revived as a dedicated
  sync view only if one command proves too crowded.
- **Drift detection: "`apply` would change it" vs. "the Config text changed."**
  Chose the former. Defining `drifted` as "the Config text changed" needs a
  second notion of "matching" that excludes the apply-time env snapshot and so
  diverges from what `apply` actually does. Reusing `apply`'s comparison verbatim
  means `status` and `apply` can never disagree, and a stale env snapshot _is_ a
  genuine reason the installed unit no longer reflects reality.
- **Liveness in the comparison (A-strict) vs. file-comparison only.** Chose
  A-strict: `applied` requires the unit files to match _and_ the timer to be
  active, because `apply` itself restarts an inactive timer. This costs a
  `systemctl --user is-active` (and the `launchctl` equivalent) per Job, so
  `status` is not offline-pure — but catching a timer that has quietly stopped
  firing is the highest-value case of a drift view, not an edge case.
- **A `show` command now vs. deferred.** Deferred. `show` would render a Job's
  generated OS unit (the cron-to-`OnCalendar` translation, the resolved
  `ExecStart`/env) purely from the Config — a translation, so it is not redundant
  with `cat`. But drift-aware `status` plus Run history covers the common "is
  everything current and healthy?" question; `show` is a power-user escape hatch
  for debugging a wrong-looking translation, and we revive it only if that need
  proves real.

## Consequences

- `acron status` is no longer offline-pure: computing Apply state shells out to
  `systemctl --user` / `launchctl` for liveness, in addition to reading unit
  files and Run history. It stays read-only.
- A manually `stop`ped or `disable`d timer reads as `drifted`, prompting the user
  to re-`apply` rather than silently failing to fire.
- The glossary term is **Apply state**; "sync"/"reconciliation state" are
  explicitly avoided (CONTEXT.md), keeping `apply` the only reconcile verb
  (ADR-0008).

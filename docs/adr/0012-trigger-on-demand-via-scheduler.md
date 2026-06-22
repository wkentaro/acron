# Trigger: an on-demand firing driven through the OS scheduler

`acron trigger <job>` fires a Job once, now, out of schedule, without holding
the terminal. It does this by asking the OS scheduler to start the Job's
already-installed unit immediately and detached (`systemctl --user start
--no-block acron-<job>.service`; `launchctl kickstart` on macOS, which is
already async). The run is owned by the scheduler, so it inherits the three
pillars (overlap lock, timeout, Run history) from the exact same `acron run`
codepath a scheduled firing uses. `trigger` returns at once with a confirmation
line pointing at `acron logs`.

The governing principle is that **a Trigger changes only the _when_ of a firing,
never the _what_ or the _whether_**:

- **Requires `applied`; refuses otherwise.** `trigger` checks the Job's Apply
  state up front and refuses with a state-specific hint unless the Job is
  cleanly `applied`: `unapplied`/`disabled` have no unit to start, and
  `drifted` would silently run the _stale last-applied_ unit (old prompt, env,
  cwd) rather than the Config you can see. Refusing on drift keeps "what runs"
  honest at the cost of an extra `apply` in the tweak-and-test loop.
- **Honors the lock.** A Trigger is subject to the same overlap policy as any
  firing. Because the lock is acquired inside the detached run after `trigger`
  has returned, `trigger` does a best-effort, non-destructive pre-probe of the
  per-Job `flock`: if a Run is already in flight it reports "already running"
  and skips the handoff, so the interactive caller gets immediate feedback
  instead of a silent `skipped` Run it cannot see. The detached run's own
  overlap check remains the real backstop if the probe races.
- **Honors the Condition.** No bypass. A Trigger evaluates the Condition like
  any firing; "run regardless" is a deliberate non-feature for now (it would
  require threading a flag through the fixed unit `ExecStart`).

`acron status` gains a live `running` indicator: it probes each Job's lock and,
when held, reports `running` (with the start time read from the in-flight log
filename) instead of the last finished Run. This makes "trigger it, then check
on it" actually work without which `trigger` fires into a void.

## Considered Options

- **Mechanism — OS scheduler vs. self-detach.** We drive the OS scheduler. The
  alternative (acron double-forks its own background `acron run`) reinvents
  daemonization — process supervision, surviving terminal close, reparenting —
  which ADR-0001 deliberately delegates to systemd/launchd. Driving the
  scheduler is a one-line shell-out per OS that inherits the three pillars for
  free, at the cost that a Trigger requires the Job to be `applied`. We treat
  that constraint as honest rather than limiting: a Trigger and a scheduled
  firing should not behave differently.
- **Drift handling — refuse vs. auto-apply.** We refuse on `drifted` and point
  at `apply` rather than silently `apply`ing the one Job before triggering.
  Auto-apply is more convenient but folds two verbs into one and can surprise
  the user by restarting the timer. A `--force`/`--apply` affordance is left as
  a clean follow-up if demand appears.
- **In-flight visibility — lock-probe vs. a `running` history record.** `status`
  derives `running` live from the held lock; it does _not_ write a `running`
  start-record to `history.jsonl`. Keeping Run statuses terminal
  (`success`/`failure`/`timeout`/`skipped`) preserves the append-only,
  end-written history model (ADR-0007); a start-then-rewrite record would break
  it. `logs --follow` (tailing the live log) is a separate, deferred feature.

## Consequences

- **`trigger` only runs cleanly-`applied` Jobs.** Triggering right after editing
  the Config requires an `apply` first. This is the deliberate price of never
  running a stale or absent unit.
- **The overlap pre-probe is advisory, not a guarantee.** A scheduled firing can
  still slip into the TOCTOU window between probe and handoff; the detached
  run's `flock` records the honest `skipped`/`overlap` if so.
- **`systemctl start` must use `--no-block`.** The generated service is
  `Type=oneshot`, so a blocking `start` would hang the terminal for the whole
  run — the opposite of the intent. launchd's `kickstart` is already async.

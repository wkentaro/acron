# acron

A command-line runtime for unattended agent runs that happens to be scheduled cross-OS. acron translates a Job into the native OS scheduler (systemd timer on Linux, launchd on macOS) and stays in the runtime path. Its reason to exist is not cross-OS scheduling (that is just the delivery mechanism) but owning three things an ordinary cron job cannot do well for a long-running agent: overlap prevention, timeout, and log capture with run history. Around those pillars sits an ergonomics layer (one declarative config, easy env/PATH/keys, working directory as a field) that makes scheduling a single agent run painless.

## Language

**Agent**:
The underlying coding-agent CLI that acron triggers (e.g. Claude Code, Codex, opencode). acron is agent-agnostic: it treats the agent as a command to invoke.
_Avoid_: Assistant, model, bot

**Run**:
A single execution of an agent triggered by the scheduler at a fired time. A Run ends with a status: `success`, `failure`, `timeout`, or `skipped` (the firing was dropped without running the agent — either the previous Run still held the lock, or the Condition was not met).
_Avoid_: Execution, invocation, trigger (as a noun)

**Condition**:
An optional user-supplied command run at fire time, before the agent, whose outcome decides whether the agent runs at all. Lets a Job be scheduled frequently but do work only when there is work to do. A firing dropped by its Condition is recorded as a `skipped` Run.
_Avoid_: Gate, guard, predicate, when, unless

**Run history**:
The append-only record of a Job's past Runs (start, end, exit code, status, duration, log path), kept as `history.jsonl` alongside the per-Run log files. What `acron status` and `acron logs` read.
_Avoid_: Audit log, journal

**Job**:
A single scheduled agent invocation: a schedule plus the agent command, prompt, and working directory to run when it fires. The central entity of acron.
_Avoid_: Task, cron entry, timer

**Config**:
The single TOML file declaring all Jobs. Defaults to `~/.config/acron/config.toml`; overridable with the `ACRON_CONFIG` environment variable. The source of truth from which OS scheduler units are derived.
_Avoid_: Manifest, jobfile, crontab

**Apply**:
The reconcile operation (`acron apply`) that makes the OS scheduler units match the Config: creating, updating, and removing units so they agree with the declared Jobs. Auto-prunes acron-owned units no longer in the Config.
_Avoid_: Sync, install, reload

**Apply state**:
A Job's state relative to `apply`, comparing the Config against the acron-owned units installed on this machine. One of: `applied` (the units match the Config and the timer is loaded and active, so running `apply` would be a no-op), `drifted` (running `apply` would rewrite or restart the units — the Config, the apply-time environment snapshot, or the timer's active state no longer matches what is installed), `unapplied` (declared in the Config but not yet installed), `orphaned` (units still installed for a Job no longer in the Config), or `disabled` (the Job sets `enabled = false`, so `apply` keeps it uninstalled — lingering units from before it was disabled read as `drifted`, since `apply` would remove them). What `acron status` reports for each Job alongside its latest Run status; computed from the same comparison `apply` performs, so a Job is `applied` exactly when `apply` is a no-op for it.
_Avoid_: Sync, sync status, reconciliation state, install status, drift (as the umbrella term; it names one state)

**Destroy**:
The teardown operation (`acron destroy`) that removes all acron-owned units from the current machine while leaving the Config intact, so a later `apply` reinstalls them.
_Avoid_: Uninstall, purge, clean

**Schedule**:
A Job's firing times, expressed as a cron expression (calendar/wall-clock semantics). acron translates it into the native scheduler form (systemd `OnCalendar`, launchd `StartCalendarInterval`).
_Avoid_: Timer, interval, frequency

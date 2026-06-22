# acron Design

Implementation-facing specification for acron, a command-line scheduler that
triggers an agent CLI periodically. This document consolidates the decisions
recorded individually in `docs/adr/` and the vocabulary in `CONTEXT.md` into one
buildable spec.

For the meaning of capitalized terms (Job, Run, Apply, Destroy, Schedule,
Config, Run history), see `CONTEXT.md`.

## Overview

acron translates declaratively-defined Jobs into the native per-user OS
scheduler (systemd timers on Linux, launchd LaunchAgents on macOS) and stays in
the runtime path: the scheduler invokes `acron run <job>`, which runs the agent
and owns overlap prevention, timeout, and log capture with run history
(ADR-0001).

```
scheduler (systemd/launchd)  -->  acron run <job>  -->  agent CLI
```

acron is agent-agnostic. It knows nothing about Claude Code, Codex, or opencode
specifically; an agent is just a command to invoke (ADR-0004).

Windows is out of scope.

## Concepts

- **Job**: one scheduled agent invocation (schedule + agent command + prompt +
  working directory).
- **Run**: one execution of a Job's agent, ending in a status: `success`,
  `failure`, `timeout`, or `skipped`.
- **Apply**: reconcile OS units to the Config (create + update + prune).
- **Destroy**: remove all acron-owned units from this machine, keep the Config.

## Config

Jobs are declared in a single TOML file. There is no mutable state store and no
imperative `add`/`remove`; the file is the single source of truth (ADR-0003).

Resolution order for the Config path:

1. `$ACRON_CONFIG` if set.
2. `~/.config/acron/config.toml` (or `$XDG_CONFIG_HOME/acron/config.toml`).

Project-local Jobs are not auto-discovered. A project carries its own Config and
points acron at it via `ACRON_CONFIG=<repo>/acron_config.toml` (ADR-0003).

### Schema

```toml
[[job]]
name     = "nightly-triage"   # required, unique, [a-z0-9_-]
schedule = "0 2 * * *"        # required, 5-field cron (calendar semantics)
agent    = ["claude", "-p", "{prompt}", "--dangerously-skip-permissions"]
prompt   = "Triage open issues"  # required
cwd      = "~/src/acron"      # required, absolute or ~-expanded
enabled  = true               # optional, default true
timeout  = "1h"               # optional, default "1h"; 0 disables the timeout
env      = { TZ = "Asia/Tokyo" }  # optional, portable extra vars
condition = ["sh", "-c", "gh pr list | grep -q ."]  # optional gate; skip unless exit 0
```

### Fields

- **name**: identifies the Job. Becomes part of unit names, lock file names, and
  log directory names, so it is restricted to `[a-z0-9_-]`. Must be unique
  within the Config.
- **schedule**: a standard 5-field cron expression with calendar (wall-clock)
  semantics. Relative-interval scheduling is not supported (ADR-0005).
- **agent**: the argv array (command plus flags) acron executes directly, with
  no shell. No agent presets exist; the user supplies the flags (ADR-0004).
- **prompt**: substituted for a `{prompt}` token in `agent`. If no `{prompt}`
  token is present, the prompt is appended as the final argument (ADR-0004).
- **cwd**: required. `acron apply` errors if a Job omits it. acron `chdir`s here
  before exec. `~` is expanded.
- **enabled**: optional, default `true`. `enabled = false` reconciles the unit
  off without removing the Job from the Config. There are no `enable`/`disable`
  commands; the field is the off switch (declarative reconcile has no imperative
  toggle).
- **timeout**: optional, default `"1h"`. Go duration string. `0` opts out
  explicitly (accepting the wedge risk below).
- **env**: optional table of extra environment variables, merged on top of the
  baked PATH and HOME/USER.
- **condition**: optional argv run at fire time, before the agent, with the same
  `cwd` and `env` (no `{prompt}` substitution). Exit `0` runs the agent; `1`-`254`
  drops the firing as `skipped`; `255`/signal records a `failure`. Lets a Job be
  scheduled often but do work only when there is work to do (ADR-0010).

### Validation

`acron apply` validates the whole Config before touching any unit, and fails
atomically (no partial apply) on:

- missing required field (`name`, `schedule`, `agent`, `prompt`, `cwd`)
- duplicate or malformed `name`
- unparseable `schedule`
- `agent` empty
- `cwd` that does not exist

## CLI

| Command                                  | Purpose                                                            |
| ---------------------------------------- | ------------------------------------------------------------------ |
| `acron apply [--dry-run]`                | Reconcile OS units to the Config.                                  |
| `acron destroy`                          | Remove all acron-owned units from this machine; keep the Config.   |
| `acron run <job>`                        | The entry the scheduler invokes; also runs a Job now, for testing. |
| `acron list`                             | List Jobs from the Config (name, schedule, next fire, enabled).    |
| `acron status`                           | Table of each Job's last Run status and time (reads Run history).  |
| `acron logs <job> [--run <ts>] [--list]` | Show a Run's captured output.                                      |
| `acron edit`                             | Open the Config in `$EDITOR`, validate on save.                    |

Verb choice follows the on-demand declarative-reconcile idiom (`apply`/`destroy`
from Terraform; `apply` also matches chezmoi). `install`/`uninstall`/`sync` were
rejected (ADR-0008).

### apply

1. Load and validate the Config (see Validation).
2. Compute the desired set of units from the enabled Jobs.
3. Snapshot the calling shell's `PATH` (see Environment).
4. For each Job: create or update its unit(s).
5. Prune: remove acron-owned units no longer in the Config.
6. Reload the scheduler and enable timers (see Unit generation).

`apply` is idempotent and only ever touches units in acron's own namespace; it
never modifies hand-written units. `--dry-run` prints the create/update/remove
plan without applying it. Pruning is automatic (ADR-0008).

### destroy

Removes every acron-owned unit on the current machine and leaves the Config
intact, so a later `apply` reinstalls them. Useful for decommissioning one
machine while keeping the Config in dotfiles.

## Runtime: `acron run <job>`

The wrapper that the scheduler invokes. It owns the three pillars (ADR-0007):

1. **Acquire the lock** `~/.local/state/acron/locks/<job>.lock`. If it is already
   held, the previous Run is still going: drop this firing, append a `skipped`
   record (reason `overlap`) to Run history, exit 0. This overlap policy is fixed
   (no `queue`/`allow`), and takes precedence over the condition.
2. **Set up the environment** (see Environment), `chdir` to `cwd`, and redirect
   stdin from `/dev/null` so a non-interactive agent never blocks on input.
3. **Evaluate the condition** (if set): run the `condition` argv before the agent,
   bounded by the same `timeout`. Exit `0` proceeds; `1`-`254` drops the firing as
   `skipped` (reason `condition`, no log); `255`/signal records a `failure` with
   the check's output logged for diagnosis. Mirrors systemd `ExecCondition=`
   (ADR-0010).
4. **Exec the agent**: substitute `prompt` for `{prompt}` in `agent` (or append),
   then exec directly (no shell).
5. **Capture output**: combined stdout+stderr (interleaved) to the per-Run log
   `~/.local/state/acron/runs/<job>/<timestamp>.log`.
6. **Enforce timeout**: on expiry send SIGTERM, then SIGKILL after a short grace.
   Default 1h; `0` disables. A killed Run is recorded as `timeout`. The default
   exists because skip-if-running means a hung Run would otherwise hold the lock
   and wedge the Job silently forever; skip and timeout are coupled by design.
7. **Record history**: append one line to `runs/<job>/history.jsonl` with start,
   end, exit code, status, duration, log path, and (for skips and condition
   failures) a `reason`. Prune to the most recent 50 real Runs and, independently,
   the most recent 50 skipped Runs, so skips never evict real Runs.
8. **Release the lock.**

### Run history record

One JSON object per line in `runs/<job>/history.jsonl`:

```json
{"start":"2026-06-21T02:00:00Z","end":"2026-06-21T02:13:48Z","status":"success","exit":0,"duration_s":828,"log":"2026-06-21T02:00:00.log"}
{"start":"2026-06-21T03:00:00Z","end":"2026-06-21T03:00:00Z","status":"skipped","reason":"condition","exit":0,"duration_s":0,"log":""}
```

A `skipped` record carries a `reason` (`overlap` or `condition`) and no `log`;
condition failures carry `reason: condition` with `status: failure` and a log.
`acron status` reads the last record per Job; `acron logs` reads the records to
resolve `--run` and `--list`.

## Environment

Both `systemctl --user` and launchd LaunchAgents start with a minimal
environment, so the agent and its tools (`claude`, `gh`, `node`) are typically
not on `PATH`. acron handles this as follows (ADR-0006):

- **PATH**: `acron apply` snapshots the calling shell's `PATH` and bakes it into
  each generated unit. `PATH` is machine-specific (the Homebrew prefix differs
  across Apple Silicon, Intel, and Linux), so capturing it per-machine at apply
  time is correct. A baked `PATH` is stale until the next `apply`; re-applying
  after installing new tools is cheap and routine.
- **HOME and USER**: always set, so agents find their own credential storage
  (`~/.claude`, `~/.config/gh`). Secrets are not put in the Config.
- **env**: the Job's `env` table is merged on top, for portable extras.

## Filesystem layout

```
config   $ACRON_CONFIG  or  ~/.config/acron/config.toml
state    ~/.local/state/acron/runs/<job>/<timestamp>.log
         ~/.local/state/acron/runs/<job>/history.jsonl
         ~/.local/state/acron/locks/<job>.lock
units    Linux:  ~/.config/systemd/user/acron-<job>.service
                 ~/.config/systemd/user/acron-<job>.timer
         macOS:  ~/Library/LaunchAgents/com.acron.<job>.plist
```

State honors `$XDG_STATE_HOME` when set, config honors `$XDG_CONFIG_HOME`.

## Privilege level

User-level only. systemd `--user` units and launchd LaunchAgents run as the
logged-in user with `$HOME` set, so the agent has the user's credentials and
config. There is no system tier (no root, no LaunchDaemons). An agent run needs
the user's own auth, so the user tier is the correct and only target.

## Scheduling and unit generation

acron owns the per-OS translation; the cron expression is the single portable
form the user writes (ADR-0005). Catch-up is on by default: a firing missed
because the machine was off or asleep runs once at the next wake or boot, and
multiple missed firings coalesce into a single Run.

### Linux: systemd timer

`acron-<job>.timer`:

```ini
[Unit]
Description=acron job <job>

[Timer]
OnCalendar=<translated from cron>
Persistent=true

[Install]
WantedBy=timers.target
```

`acron-<job>.service` (Type=oneshot):

```ini
[Unit]
Description=acron job <job>

[Service]
Type=oneshot
ExecStart=<abs path to acron> run <job>
Environment=PATH=<snapshot>
Environment=HOME=<home> USER=<user>
WorkingDirectory=<cwd>
```

`apply` writes the unit pair, runs `systemctl --user daemon-reload`, and
`systemctl --user enable --now acron-<job>.timer`. `Persistent=true` gives
catch-up.

### macOS: launchd LaunchAgent

`com.acron.<job>.plist`:

```xml
<dict>
  <key>Label</key>              <string>com.acron.<job></string>
  <key>ProgramArguments</key>   <array>
    <string><abs path to acron></string><string>run</string><string><job></string>
  </array>
  <key>StartCalendarInterval</key>  <!-- translated from cron, steps enumerated -->
  <key>WorkingDirectory</key>   <string><cwd></string>
  <key>EnvironmentVariables</key>
  <dict><key>PATH</key><string><snapshot></string>...</dict>
  <key>StandardOutPath</key>    <string>/dev/null</string>
  <key>StandardErrorPath</key>  <string>/dev/null</string>
</dict>
```

acron does its own logging in `acron run`, so the plist's stdout/stderr go to
`/dev/null`. `apply` writes the plist and `launchctl bootstrap gui/<uid>` (and
`bootout` on prune/destroy). launchd runs a missed `StartCalendarInterval` on
wake, giving catch-up.

### cron to launchd translation

launchd `StartCalendarInterval` matches fixed points, not cron steps. acron
expands cron steps and lists into enumerated match dicts (for example `*/15`
in the minute field becomes four dicts at minutes 0, 15, 30, 45). A cron
expression that cannot be expressed as launchd match points is rejected at
`apply` time on macOS.

## Implementation

Go, distributed as a single static binary (ADR-0009). A unit references acron by
absolute path, so there is no interpreter or virtualenv to resolve on the
unattended run path.

CLI: cobra for command parsing and the command tree, with help output fully
overridden (`SetHelpFunc`/`SetUsageTemplate`) and rendered with lipgloss to
match the rich-click help style of `ihq` and `git-hunk` (description line,
`Usage:`, colored `Commands:`, `Options:`, aligned `Examples:` with dimmed
`# comments`). charmbracelet/fang is deliberately not used. lipgloss degrades to
plain text under `NO_COLOR` or a non-TTY (ADR-0009).

## Non-goals (deferred)

- Windows.
- Relative-interval scheduling (`every = "6h"`).
- Overlap policies other than skip (`queue`, `allow`).
- Per-Job retention configuration (fixed at 50 Runs).
- Failure notifications.
- A system (root) privilege tier.
- Shell features in `agent` (pipes, env expansion); argv is exec'd directly.

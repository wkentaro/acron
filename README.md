# acron

acron is a command-line runtime for unattended agent runs that happens to be
scheduled cross-OS. It translates a Job (a schedule plus an agent command) into
the native OS scheduler (systemd timers on Linux, launchd LaunchAgents on
macOS) and stays in the runtime path: the generated unit runs `acron run
<job>`, not the agent directly, so acron supervises every firing in-process.
That is its reason to exist. Rather than leave overlap prevention, timeout, and
log capture to schedulers that cover them unevenly, acron owns them itself and
gives every Run one uniform set of guarantees plus a queryable run history.
acron is agent-agnostic: the agent is just a command to invoke, so it works with
Claude Code, Codex, opencode, or any CLI.

Platform support: Linux (systemd user units) and macOS (launchd LaunchAgents).
Windows is out of scope.

`acron status` shows every Job's apply state, last Run, and next firing at a
glance, color-coded green for healthy, red for drift or failure, and dim for
times and pending work:

![acron status listing three jobs: an applied job whose last run succeeded, an applied job that has never run, and a drifted job whose last run failed](docs/images/status.svg)

## Install

```sh
go install github.com/wkentaro/acron@latest
```

Or download a prebuilt binary from the [GitHub Releases][releases]:

```sh
os=$(uname -s | tr '[:upper:]' '[:lower:]')
arch=$(uname -m); [ "$arch" = x86_64 ] && arch=amd64
curl -fL https://github.com/wkentaro/acron/releases/latest/download/acron-$os-$arch -o acron
chmod +x acron && sudo mv acron /usr/local/bin/
```

To build from source, run `nix develop -c make build` inside the repository.

[releases]: https://github.com/wkentaro/acron/releases

## Quickstart

```sh
acron config edit                 # 1. open $VISUAL/$EDITOR on a commented template (created if absent)
                                  # 2. uncomment the [[job]] block and edit its values
acron apply                       # 3. install the OS scheduler units
acron status                      # 4. verify the job is applied and see when it fires next
acron trigger nightly-triage      # 5. fire it now for a manual test
acron logs nightly-triage         # 6. view the output
```

A minimal Job looks like this:

```toml
[[job]]
name     = "nightly-triage"       # required, unique, [a-z0-9_-]
schedule = "0 2 * * *"            # required, 5-field cron
agent    = ["claude", "-p", "{prompt}", "--dangerously-skip-permissions"]  # required argv; {prompt} is substituted
prompt   = "Triage open issues"   # required
cwd      = "~/src/acron"          # required, absolute or ~-expanded
```

## Config reference

The Config is a single TOML file declaring all Jobs. Its path is resolved in
order: `$ACRON_CONFIG` if set, otherwise `~/.config/acron/config.toml`
(honoring `$XDG_CONFIG_HOME` when set).

| Field       | Type     | Required | Default | Description                                                                                         |
| ----------- | -------- | -------- | ------- | --------------------------------------------------------------------------------------------------- |
| `name`      | string   | yes      | —       | Unique Job name, restricted to `[a-z0-9_-]`.                                                        |
| `schedule`  | string   | yes      | —       | 5-field cron expression with calendar (wall-clock) semantics.                                       |
| `agent`     | array    | yes      | —       | Argv (command plus flags) run directly, no shell. `{prompt}` is substituted.                        |
| `prompt`    | string   | yes      | —       | Substituted for the `{prompt}` token, or appended if no token is present.                           |
| `cwd`       | string   | yes      | —       | Working directory acron chdirs into. Absolute or `~`-expanded.                                      |
| `enabled`   | bool     | no       | `true`  | `false` reconciles the unit off without removing the Job.                                           |
| `timeout`   | duration | no       | `"1h"`  | Go duration string; `"0"` disables the timeout.                                                     |
| `env`       | table    | no       | `{}`    | Extra environment variables merged on top of the baked PATH and HOME/USER.                          |
| `condition` | array    | no       | —       | Argv run before the agent; exit `0` runs it, `1`-`254` skips the firing, `255`/signal is a failure. |

## PATH

`acron apply` snapshots the calling shell's `PATH` and bakes it into each
generated OS unit, because user schedulers start with a minimal environment in
which tools like `claude`, `gh`, and `node` are not on `PATH`. A baked `PATH` is
machine-specific and goes stale when you install new tools, so re-run `acron
apply` after a `brew install` or after updating your agent CLI.

## Commands

| Command               | Description                                                           |
| --------------------- | --------------------------------------------------------------------- |
| `acron apply`         | Reconcile OS scheduler units to the config.                           |
| `acron destroy`       | Remove all acron-owned units from this machine.                       |
| `acron run <job>`     | Run a job now (the entry the scheduler invokes).                      |
| `acron trigger <job>` | Fire a job now, out of schedule, in the background.                   |
| `acron status`        | Show each job's apply state and last run.                             |
| `acron show <job>`    | Show a job's generated unit and whether it matches what is installed. |
| `acron logs <job>`    | Show a job's captured output.                                         |
| `acron logs <job> -f` | Stream the run in progress until it finishes.                         |
| `acron history [job]` | List past runs, newest first.                                         |
| `acron config show`   | Print the config to stdout.                                           |
| `acron config edit`   | Open the config in `$VISUAL`/`$EDITOR`, validating on save.           |

Every command has a `--help` with full column and output documentation.

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

State honors `$XDG_STATE_HOME` and config honors `$XDG_CONFIG_HOME` when set.

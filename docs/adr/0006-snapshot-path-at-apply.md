# PATH is snapshotted into units at apply time

`acron apply` captures the calling shell's `PATH` and bakes it into each generated unit; acron also always sets `HOME` and `USER`. A per-job `env` table in the Config can add portable variables. Secrets are not put in Config: acron relies on each agent's own credential storage under `$HOME` (e.g. `~/.claude`, `~/.config/gh`).

We do this because `systemctl --user` and launchd LaunchAgents start with a minimal environment, so `claude`/`gh`/`node` are typically not on `PATH` and an unattended run dies with "command not found". `PATH` is legitimately machine-specific (Homebrew prefix differs across Apple Silicon, Intel, and Linux), so capturing it per-machine at apply time is correct rather than a hack. The cost: `apply` output depends on ambient shell, not only on the Config, and a baked `PATH` is stale until the next `apply` after installing new tools. That trade-off is acceptable because re-applying is cheap and routine.

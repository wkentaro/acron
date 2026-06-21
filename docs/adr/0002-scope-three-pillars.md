# acron's scope: own the unattended-agent-run runtime, not just cross-OS scheduling

acron's value is not generalizing a schedule across systemd and launchd (a wrapper script plus two hand-written units could do that, and macOS even still has cron). Its reason to exist is owning three things an ordinary cron job cannot do well for a long-running agent run:

1. **Overlap prevention** — agent runs take minutes and may exceed the interval; acron enforces single-instance so two runs do not fight over the same repo. Neither cron nor launchd gives this cleanly.
2. **Timeout / kill** — a looping agent burns tokens until noticed; acron bounds every run uniformly (systemd has `RuntimeMaxSec`, launchd has nothing comparable).
3. **Log capture + run history** — an agent run is a narrative ("what did it actually do?"), not a pass/fail; acron captures output and keeps history, surfaced via `acron logs`.

Around these sits an ergonomics layer that is not unique but is the reason to reach for acron: kill cron-tedium with one declarative config, make env/PATH/keys for the headless context easy, expose working directory as a field, and generate the cross-OS units. Cross-OS scheduling is the delivery mechanism, not the point.

Explicitly out of scope as pillars (considered and demoted): _guaranteeing non-interactivity_ is the user's own agent flag (`claude -p`, `codex exec`, `opencode run`), not acron's job; acron only runs the agent with stdin from `/dev/null` so a misconfigured job fails fast instead of hanging, which folds into the timeout. _Working directory_ is a config-ergonomics field, not a pillar (both schedulers already support it).

Every pillar requires acron to stay in the runtime path, so this confirms ADR-0001.

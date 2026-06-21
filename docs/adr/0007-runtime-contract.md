# Runtime contract: per-Job lock, default timeout, per-Run logs with history

`acron run <job>` owns the three things an ordinary cron job does poorly for a long-running agent:

- **Overlap prevention.** A per-Job lockfile (`~/.local/state/acron/locks/<job>.lock`). If a firing arrives while the previous Run holds the lock, the firing is dropped and recorded as a `skipped` Run. Fixed policy for now (no `queue`/`allow` override).
- **Timeout.** A per-Job `timeout` (default 1h, overridable, `0` to opt out). On expiry, SIGTERM then SIGKILL after a short grace; recorded as a `timeout` Run. The default exists because skip-if-running means a hung Run would otherwise hold the lock and wedge the Job silently forever.
- **Log capture with history.** The agent's combined stdout+stderr is written to a per-Run file `~/.local/state/acron/runs/<job>/<timestamp>.log`, and one metadata line is appended to `runs/<job>/history.jsonl`. Retained to the last N Runs per Job (default ~50).

These three are the reason acron stays in the runtime path rather than only generating units (see ADR-0001). Skip-if-running and the default timeout are coupled by design: neither is safe without the other.

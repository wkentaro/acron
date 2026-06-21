# Schedules are cron expressions, calendar mode only to start

A Job's `schedule` is a standard cron expression with calendar (wall-clock) semantics. acron translates it to systemd `OnCalendar` and to launchd `StartCalendarInterval` (enumerating cron steps/lists like `*/15` into launchd match points). Relative-interval scheduling ("every 6h since the last run") is deliberately not supported yet.

We chose cron over systemd's `OnCalendar` dialect (which does not fully map to launchd) and over a custom friendly DSL (which would be ours to design, parse, and document). Cron is the lingua franca, maps cleanly to both targets, and "every 6h on the hour" is already expressible as `0 */6 * * *`, so calendar mode covers most periodic needs. Interval mode is a separate code path and a separate set of unit fields; it is deferred until a real need appears.

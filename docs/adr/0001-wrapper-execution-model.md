# acron stays in the runtime path (wrapper, not just unit generator)

The OS scheduler (systemd timer / launchd) invokes `acron run <job>`, which in turn invokes the agent CLI; acron does not merely generate units that call the agent directly.

We chose this because an agent run is not an ordinary cron job: it is long-running, can overlap with a previous run, fails in interesting ways, and the user wants to see what it did. Concentrating logging, single-instance locking, exit-code handling, and env setup in one runtime codepath keeps behavior identical across both OSes; the alternative (generate units that call the agent directly) pushes all of that onto two OS mechanisms that disagree with each other, so we would end up writing per-OS runtime logic anyway. The cost is that acron becomes a dependency at every run.

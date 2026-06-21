# Jobs are declared in a single config file, reconciled by `acron apply`

Jobs are defined declaratively in one TOML file (`~/.config/acron/config.toml`, overridable via `ACRON_CONFIG`). The file is the source of truth; `acron apply` reconciles the OS scheduler units to match it. acron does not own a mutable state store, and there is no `acron add`/`remove` that imperatively mutates jobs.

We chose declarative over an imperative crontab-style CLI so that Jobs are plain config that lives in dotfiles/git, reviews cleanly, and reproduces across machines. We chose a single file over per-job drop-in files to start as simple as possible and to make shared defaults natural; the cost (merge conflicts when editing many jobs across machines, no per-host file selection) is acceptable at this scale.

Project-local Jobs are deliberately _not_ a discovery feature: instead of acron scanning repos for job files, a project simply points acron at its own config via `ACRON_CONFIG=<repo>/acron_config.toml`. This keeps the model to "one config file at a time" while still allowing a repo to carry its own jobs. A future reader expecting automatic per-repo discovery should know it was left out on purpose.

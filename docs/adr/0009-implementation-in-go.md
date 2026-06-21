# acron is implemented in Go

acron is written in Go and distributed as a single static binary.

acron is a cross-platform (macOS + Linux) system CLI whose core path is being invoked unattended by launchd/systemd. A single static binary at a known absolute path is the most robust thing to reference from a generated unit file: there is no interpreter to resolve and no virtualenv to locate, which matters precisely because the minimal scheduler environment (the same reason we snapshot PATH, ADR-0006) is hostile to interpreter-based launchers. Go also gives trivial cross-compilation for both targets and is the idiomatic choice for tools of this shape (gh, terraform). We considered Python+uv (matches the author's usual stack and would reuse the click+rich help patterns from the author's other CLIs, but adds a console-script/venv interpreter layer to the unattended run path) and Rust (equally robust binary, but heavier and slower to develop for a tool this size).

## CLI presentation

cobra owns command parsing and the command tree (the analog of Python's click). Help output is fully overridden via `SetHelpFunc`/`SetUsageTemplate` and rendered with lipgloss (the analog of rich) to match the rich-click help aesthetic of the author's other CLIs (`ihq`, `git-hunk`): a description line, `Usage:`, a `Commands:` block with colored command names, `Options:`, and an aligned `Examples:` block with dimmed `# comments`. We deliberately do not use charmbracelet/fang; its opinionated theme is not the look we want. lipgloss handles `NO_COLOR` and non-TTY degradation.

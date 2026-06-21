# acron is a generic command runner, not an agent-preset registry

A Job's `agent` is an argv array (command plus flags) that acron executes directly, with no shell. The `prompt` is a separate first-class string that acron substitutes for a `{prompt}` token in the argv array, or appends as the final argument if the token is absent. acron ships no built-in knowledge of any specific agent CLI.

We chose this over shipping presets (`agent = "claude"` -> acron knows `-p`) so that acron stays genuinely agent-agnostic: it works with any current or future agent CLI, there is no registry to maintain as agents change their flags, and there is no preset that can silently drift from reality. The cost is slightly more verbose config (the user supplies the flags themselves). Executing the argv directly rather than through a shell avoids quoting/escaping bugs with prompts that contain spaces or special characters; shell features (pipes, env expansion) are intentionally unavailable for now.

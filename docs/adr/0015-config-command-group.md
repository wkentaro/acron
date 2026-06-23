# Config operations group under a `config` noun: `acron config show` and `acron config edit`

Reading and editing the Config live under a single `config` command group:
`acron config show` prints the raw Config file to stdout, and `acron config edit`
opens it in `$EDITOR` and validates on save. This is the CLI's first noun-grouped
subcommand; every other command (`apply`, `destroy`, `run`, `trigger`, `status`,
`show`, `logs`, `history`) is a flat top-level verb. The former top-level
`acron edit` is removed in favor of `acron config edit`.

`config show` is the read-only counterpart to `edit`: it answers "what is in my
Config right now?" without opening an editor. It prints the file **verbatim, with
no validation**, so a Config that fails to parse can still be inspected (the
common case for `show` is diagnosing a broken file). A missing Config is not an
empty print but an error that points at the fix:
`no config at <path>; run "acron config edit" to create one`.

The governing principle is **a distinct object with multiple operations earns a
noun group, even though the rest of the surface is flat**. The Config is such an
object, and noun-then-verb is the established idiom for it (`git config
list`/`edit`, `gh config get`/`set`/`list`). Grouping also resolves a verb
collision: `acron show <job>` already means "show a job's generated unit", so a
bare top-level `acron show`/`acron cat` for the Config would overload `show` or
introduce a lone unix-tool noun next to the verb siblings. The `config` namespace
disambiguates without touching the existing job `show`.

## Considered Options

- **`config` group vs. a flat `acron cat`/`acron print`.** Group. A flat command
  would reinvent around the `git config` / `gh config` idiom the feature is
  modeled on, and would leave `edit` as a lone off-grammar verb whose subject
  (the Config) is implicit. Grouping names the subject once and gives both
  operations an obvious home; it is also where any future Config-scoped operation
  belongs. The cost is breaking the otherwise-flat verb surface, accepted because
  the Config is the one object in the CLI with more than one operation on it.
- **`show` vs. `list`/`cat` as the verb.** `show`. `git config` uses `--list`
  because it prints parsed key/value pairs; `acron config show` prints a whole
  TOML file verbatim, for which `show` reads more naturally than `list`. `cat` is
  a unix-tool noun that reads oddly beside the verb siblings, and its
  disambiguating value over `show` disappears once the `config` namespace already
  separates it from `acron show <job>`.
- **Print verbatim vs. validate first.** Verbatim. The point of `show` is to
  inspect the file as written, including a broken one; gating output on a
  successful parse would make the command useless exactly when it is most needed.
  Validation stays with `edit` (on save) and with the commands that consume the
  Config (`apply`, `status`, ...).
- **Keep `acron edit` as an alias vs. a hard migration.** Hard migration. acron
  is pre-1.0 with no stability contract (ADR-0009 keeps the surface small), and a
  deprecation shim would carry a second spelling indefinitely for a command with
  no known external callers. `acron edit` now errors with cobra's
  `unknown command`.

## Consequences

- **The CLI gains its first subcommand group.** The flat-verb surface is no
  longer uniform; `config` is the documented exception, and future Config-scoped
  operations (none planned) would extend it rather than add top-level verbs.
- **`acron edit` is a breaking change.** Anything calling `acron edit` must move
  to `acron config edit`.
- **`config show` deliberately bypasses validation.** It is the one Config-reading
  path that does not call `loadAndValidate`, by design; its job is to surface
  bytes, not to judge them.
- **Extends ADR-0008.** That ADR fixed the lifecycle verb vocabulary
  (`apply`/`destroy`); this one adds a noun-grouped read/edit pair for the Config
  object alongside those verbs.

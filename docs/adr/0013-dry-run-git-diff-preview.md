# `apply --dry-run` renders planned actions as git diffs; `show` stays a plain inspector

The same comparison `apply` performs (ADR-0011) feeds two read surfaces under
opposite framings. `apply --dry-run` is a preview of _planned actions_: every
entry is something `apply` would do, so each renders as a git-style unified diff —
a create as an all-green add, an update as a hunk, a remove as an all-red delete.
`show <job>` is an _inspector_: it prints the job's generated unit plainly so the
user can read the Config-to-unit translation (the `OnCalendar`, the resolved
`ExecStart` and env), and it surfaces a diff _only_ when the installed unit has
drifted from desired. A real (non-dry-run) `apply` keeps only a terse `+`/`~`/`-`
summary.

The two surfaces share **one** diff renderer for the delta case — git unified
diff, three lines of context, `---` / `+++` header and `@@` hunks, red `-` and
green `+` — but they default differently because inspecting a unit and diffing one
are different operations. `show` defaults to plain content and reaches for the
diff renderer only on drift; `--dry-run` renders every planned action as a diff.
They are not one surface at two context widths.

The point of `--dry-run` is to answer "what exactly would change?" before
committing. The pre-existing one-line `~ acron-process-prs` told the user _that_ a
job would change but never _what_, forcing a follow-up `show <job>` per job.
Rendering each planned action as a diff makes `--dry-run` self-contained: the
whole plan, with every line that would move, in one read.

We match `git diff`'s format because the on-disk-vs-desired comparison is a
before/after of text files and users read unified diffs fluently. That means
git's defaults, not an invented variant: three lines of context (`-U3`, which on
a ~9-12 line unit spans nearly the whole file anyway), and the absent side of a
created or removed unit shown as `/dev/null` — `--- /dev/null` for a create,
`+++ /dev/null` for a remove — exactly as git renders new and deleted files, so
the `+`/`~`/`-` plan symbols and the diff bodies tell one consistent story.

## Considered Options

- **Inspection-vs-action framing vs. "one surface at two context widths."** Chose
  the framing. The tempting unification was to call `show` an `apply --dry-run`
  with full context and have everything flow through the diff renderer. But git
  does not work that way: `git show` and `git diff` _both_ default to `-U3`, and
  git's actual whole-file inspection (`git show <rev>:<path>`) prints the file
  plain, with no diff chrome at all. chezmoi (acron's closest analog, ADR-0008)
  splits the same way — `chezmoi cat` prints the target plainly, `chezmoi diff`
  shows a unified diff. Forcing `show` through the diff renderer when nothing has
  drifted yields a zero-change diff: `cat` wearing `@@` headers, serving the rare
  drift case at the expense of the common inspect case. So the axis is the
  framing (inspect vs. preview an action), not the context width.

- **git unified diff vs. a Terraform-style structured diff.** Chose git unified
  diff. Terraform diffs _structured_ resources, so it renders per-attribute
  (`~ attr = old -> new`); acron diffs systemd unit _files_, which are text. The
  tools that manage text files — chezmoi and `kubectl diff` — both emit unified
  text diffs. The artifact is text, so the format follows text-diff prior art.

- **Diffs by default vs. behind a `--diff` flag.** Chose default. `--dry-run` is
  already the explicit "let me inspect before I commit" gesture; gating its
  substance behind a second flag is ceremony. A first-time `apply` does dump every
  unit in full green, but that is the one moment the user most wants to see what is
  about to be installed.

- **Created/removed: git new/deleted rendering vs. plain.** In `--dry-run` a
  create and a remove are _actions_, so they render as git does a new and a deleted
  file: all-green against `--- /dev/null` and all-red against `+++ /dev/null`. A
  plain body under a `+`/`-` plan symbol would read as "no change," contradicting
  the symbol. In `show` the same states are _inspection_, not actions — a
  not-yet-installed unit is read plainly (there is nothing installed to diff
  against, only a unit to read), and an orphaned unit is the leftover installed
  file read plainly. Same states, opposite framing, opposite rendering.

- **A timer that drifts only by being inactive — state the reason vs. show
  `(no changes)`.** Chose to state the reason. A job whose unit files are
  byte-identical but whose timer is not active still lands in the plan, because
  `apply` would reload and restart it (ADR-0011), and there is no textual diff to
  render. Printing `(no changes)` under a `~` is self-contradictory: it says
  "nothing changed" about something `apply` will act on. Terraform — the prior art
  for reconcile previews — annotates _why_ it will act even with no attribute diff
  (`# forces replacement`); we follow that and print the reason on the job line:
  `~ acron-process-prs (timer inactive — would reload and restart)`, with no diff
  body.

- **Diffs on a real `apply` too vs. only on `--dry-run`.** Chose dry-run only.
  After a real `apply` the change has already happened; a full post-hoc diff is
  volume the user cannot act on (reading it will not un-apply anything). The terse
  `Applied:` summary reports what was done; the diff belongs to the _preview_,
  where it can still change a decision.

## Consequences

- **One delta renderer, plus a plain path for `show`.** `diff.go` ends with a
  single git-style unified-diff renderer — context window, `@@` ranges, `---` /
  `+++` with `/dev/null` on an absent side. `--dry-run` routes every plan entry
  through it; `show` routes through it only on drift and otherwise prints the unit
  plainly (the existing plain-or-diff branch in `renderUnit`, preserved). This is
  real new logic over today's whole-file line diff: git hunks need context
  grouping and line-number ranges the current renderer does not compute.

- **`show`'s plain cases are unchanged; its drift case gains git chrome.** A
  not-yet-installed, orphaned, or applied unit still prints plainly. A _drifted_
  unit, today rendered as a whole-file line diff, now renders as git hunks
  (`@@`, `---`/`+++`) — visually near-identical on these short units, but in the
  shared format.

- **`Plan` must carry content, not just names.** Today `scheduler.Plan` holds
  `[]string` job names; rendering diffs requires it to also carry the
  `(installed, desired)` text per changed job. `Apply` already has both in scope at
  the point it appends to the plan, so this is plumbing, not new computation.

- **Path labels follow git literally.** A two-sided update shows `a/<unit>` /
  `b/<unit>` (both the same unit path — git's shape, kept for recognizability); a
  create or remove shows `/dev/null` on the absent side. No invented half-form.

- **No glossary change.** This refines the behavior of `apply` (ADR-0008) and
  reuses the comparison behind Apply state (ADR-0011); it introduces no new term.

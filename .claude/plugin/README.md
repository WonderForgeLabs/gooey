# The gooey Claude Code plugin

Every agent that works this repo re-learns the same things and gets the same
things wrong. On a single day this repo produced four instances of a
hand-maintained claim outliving what it described, three of a correct treatment
applied to one branch and not its sibling, and at least four tests that could
not distinguish the two answers they were written to tell apart. Those are not
accidents; they are what this codebase's failure modes look like.

This plugin is where that stops being oral tradition. `CLAUDE.md` is already
the best writeup in the repo — most of this is making it **executable** rather
than readable.

## Install

```sh
/plugin marketplace add WonderForgeLabs/gooey
/plugin install gooey@gooey
```

## Skills

| Skill | Reach for it when |
| --- | --- |
| `gooey:verify-a-change` | Before claiming anything is done, before a PR, or before writing "tests pass" |
| `gooey:write-a-component` | Adding or editing anything in `components/`, any `Render`/`Measure`/`Arrange`, any container, event, or goroutine |
| `gooey:tests-that-can-fail` | Writing or reviewing any test, fixture, sweep, or negative assertion |
| `gooey:capture-a-demo` | Driving a real binary under a pty, or recording a GIF |
| `gooey:land-a-pr` | Opening a PR, reading the gate, stacking, or waiting on CI |

Their governing rule: **wherever a fact could be derived by a command, the
skill gives the command instead of the number.** That is the single biggest
lesson this repo produced. `scripts/verify.sh` takes it furthest — it holds no
copy of the module list *or of the discovery command*, and instead extracts the
loop out of `CLAUDE.md` and runs it, because `CLAUDE.md`'s copy is the one three
root-module tests already pin against a walk of the tree.

## Hooks

Six checks in two scripts. One process per Bash call and one per file edit —
with five to fifteen agents live in separate worktrees, per-check subprocesses
would be a real tax.

| # | Check | Event | Mode |
| --- | --- | --- | --- |
| 1 | `go build ./...` without `-o` | PreToolUse / Bash | advisory |
| 2 | `git add -A` / `.` / `--all` | PreToolUse / Bash | **blocking** |
| 3 | a compiled executable in an `add` or `commit` pathspec | PreToolUse / Bash | **blocking** |
| 4 | `git stash clear` | PreToolUse / Bash | **blocking** |
| 4b | `git stash drop` / `pop` / `apply` | PreToolUse / Bash | advisory |
| 5 | recursive `grep` | PreToolUse / Bash | advisory, once per session |
| 6 | an unformatted `.go` file | PostToolUse / edit | advisory |

**Everything is scoped to a gooey checkout**, identified by the module path in
`go.mod` rather than by a directory name — so a worktree, a clone under another
name and a fork all resolve, and `git add -A` in any other repo on the machine
is untouched. A plugin that blocks ordinary practice globally is a plugin
people uninstall.

**Advisory hooks deliberately omit `permissionDecision`.** Emitting `"allow"`
would also auto-approve a command the user would otherwise be prompted about; a
warning must not double as a permission grant.

### Why only three of them block

The default is advisory, because a hook that blocks wrongly does not
inconvenience one agent — it wedges several, and the author is not around to
debug it. Each block has to earn it on three counts: the action is
unambiguously wrong *here*, a better alternative is always available, and the
cost of it landing exceeds the cost of a false block.

- **`git add -A`** is the mechanism by which build artifacts entered this
  history. `git status --porcelain` lists an untracked *directory* once and
  never descends, so a binary inside one is invisible to the check you would
  have run first. Explicit pathspecs are the house rule anyway; a false block
  costs one round trip.
- **A compiled executable in a pathspec** has no legitimate case in this repo.
  Detection is by **magic bytes, not the mode bit** — this tree is full of
  legitimately-755 shell scripts and blocking those would make the hook worse
  than useless.
- **`git stash clear`** destroys every worktree's stash at once; the stack is
  repo-global and this repo runs five to fifteen worktrees. There is no version
  of that which is right. `drop` and `pop` only *warn*, with the live listing
  attached, because an agent dropping its own stash is ordinary.

### These are not gates

A `PreToolUse` hook that times out is canceled, its output discarded, and the
call continues through the normal permission flow. Every block above catches
the honest mistake and stops nothing determined or unlucky. Do not read any of
this as a guarantee — a hook documented as a guarantee that isn't one is
precisely the class of defect this plugin exists to catch.

## Testing the hooks

```sh
sh .claude/plugin/tests/run.sh             # every rule paired with its near-miss
sh .claude/plugin/tests/mutate.sh          # proves those cases can actually fail
sh .claude/plugin/tests/verify-grading.sh  # verify.sh's verdict + block selector
```

Both print their own totals on the last line and exit non-zero on any failure.
Read that line; the counts are not written down here for the same reason the
module count is not written down in `verify-a-change`.

`run.sh` alone proves very little — a suite of "stays silent" expectations
passes perfectly against a hook that does nothing at all. `mutate.sh` breaks
each check in a **named direction** and asserts that the *right* case goes red:

- **under-generalise** — the check stops firing → a WARN/BLOCK case reds
- **over-generalise** — the check fires on everything → a NEAR-MISS case reds

It also **refuses to score a mutation whose `sed` did not change the file**,
which is not hypothetical: four of the mutations were silent no-ops on the
first run — `||` inside an `s|…|…|` expression — so `sed` errored, the file was
untouched, the suite stayed green, and four hooks would have shipped reported
as *proved* while never being exercised. A green mutation suite without that
guard is indistinguishable from one that mutates nothing. An A/B whose arms
agree is a harness result, not a finding.

`verify-grading.sh` covers the other half — that `verify.sh` grades the verdict
line and **fails shut** when there is no verdict, and that its
select-the-block-by-content rule errors on zero *and* on two rather than
picking one. It carries a positive control (exactly one block → exit 0), which
is what stops the three error cases from passing against a script that always
errored.

## Validating the manifests

```sh
claude plugin validate .                          # marketplace, recursing into the plugin
claude plugin validate ./.claude/plugin --strict   # manifest, skills, hooks
```

`--strict` fails on warnings too. Confirmed discriminating: removing `name`
from `plugin.json` fails both, and dropping a skill's `description` fails
`--strict` — "I wrote valid JSON" is a different claim from "it loads".

## Layout

```
.claude-plugin/marketplace.json     at the REPO root — makes the repo installable
.claude/plugin/
  .claude-plugin/plugin.json
  hooks/{hooks.json,lib.sh,bash-guard.sh,go-format.sh}
  scripts/verify.sh
  skills/*/SKILL.md
  tests/{run.sh,mutate.sh}
```

`${CLAUDE_PLUGIN_ROOT}` is a **cache** directory on an installed machine, not
this checkout. Nothing here derives a repo path from it: `verify.sh` resolves
the repo from `CLAUDE_PROJECT_DIR` or `git rev-parse`, and the hooks take it
from the tool input's `cwd`.

---
name: verify-a-change
description: Verify a change to the gooey repo across the root module and every nested module. Use before claiming a change is done, before opening or updating a PR, when a test fails and you want to know whether it is yours, or whenever you are about to write the words "tests pass". Covers the derived module-discovery loop, why the module list must never be transcribed, reading the verdict rather than the scroll, and the gap between what CI runs and what the loop runs.
---

# Verify a change to gooey

## The one-liner

```sh
"${CLAUDE_PLUGIN_ROOT}"/scripts/verify.sh
```

Root module `go vet` + `go test`, then every nested module. Exit 0 means
green. `--root-only` and `--nested-only` split it; `--print` shows the loop it
is about to run without running it.

**Read the exit code.** Not the last screenful, not "I didn't see any FAIL".
The nested-module loop pipes `find` into `while read`, which puts the body in
a subshell, so a counter set in there dies with it — that is how a previous
version of this loop printed `FAIL handlers/exec` somewhere inside ten
thousand lines of `go` output and still exited 0. The current loop writes
failures to a `mktemp` file and prints one verdict line. `verify.sh` grades
that line and converts it into an exit status; if you run the loop by hand
instead, the last line is the check.

## Why there is no module list in this skill

There is no list here, and adding one is not allowed.

`CLAUDE.md`'s Verify section holds the discovery command, and `verify.sh`
**extracts that block out of `CLAUDE.md` and runs it** rather than carrying a
copy. That is deliberate. A transcribed list is stale the first time someone
adds a module, and the failure is silent — the loop still exits 0, so you
report a verified tree having never compiled the new module. `CLAUDE.md`
itself named eight modules across two loops while skipping seven
`packs/temporal-*`; `ci.yml` made the same mistake from the other end and lost
twice, with `paint/` and every `examples/*` module going unbuilt behind a wall
of green.

Three tests in the root module now pin all of this, and they are the reason
this skill can afford to state nothing:

| Test | File | What it does |
| --- | --- | --- |
| `TestCLAUDEMDVerifyLoopReachesEveryNestedModule` | `claudemd_test.go` | Extracts the `find` out of `CLAUDE.md`, **executes it**, diffs its real output against an independent `filepath.WalkDir` — in both directions |
| `TestCIWorkflowDiscoversEveryNestedModule` | `ciworkflow_test.go` | Same mechanism against `.github/workflows/ci.yml` |
| `TestCIWorkflowAndCLAUDEMDShareOneDiscovery` | `ciworkflow_test.go` | Normalises both extracted commands and fails if they have drifted by a character |
| `TestCIWorkflowRaceTierMatchesCLAUDEMD` | `ciworkflow_test.go` | Same for the `case` arm that selects `-race`, and additionally requires namespaced entries to be globs (`handlers/*`) so a new module under an existing namespace is not silently untested |

Note the shape: they run the *documented command* and compare it against an
independent walk. A Go reimplementation of the command would have passed while
the documented command was broken — that is the whole design point, and it is
the pattern to copy whenever you pin a doc against reality.

**Want the number of modules?** Run the `find`. Do not write it down here,
in a PR body, or in a commit message as a fact that outlives the run:

```sh
"${CLAUDE_PLUGIN_ROOT}"/scripts/verify.sh --print   # the loop
cd "$(git rev-parse --show-toplevel)" &&
  find . -mindepth 1 -name '.?*' -prune -o -name go.mod -print | wc -l
```

If it walks noticeably fewer than the tree looks like it holds, suspect the
loop before you trust the green.

## Never `go build ./...`

`go build ./...` writes one executable per main package into the tree.
`.gitignore` covers some of them and has never covered all of them, which is
how five got committed. Use `go vet ./...` for a whole-repo compile check, or
`go build -o /tmp/gooey-bin/ ./...` when you actually want binaries — `-o`
takes a *directory* for a multi-package build and leaves the tree clean.

A single-package build has the same problem in miniature: `go build ./cmd/browser`
writes `./browser` into the current directory.

Which names would land unignored **today** is derived, not remembered:

```sh
cd "$(git rev-parse --show-toplevel)" &&
go list -f '{{if eq .Name "main"}}{{.Dir}}{{end}}' ./... |
  sed 's|.*/||' | sort -u |
  while read -r b; do git check-ignore -q "$b" || echo "UNIGNORED: $b"; done
```

The `bash-guard` hook warns when it sees `go build ./...` without `-o`, and
prints that same command rather than a count.

## What CI runs, and the two places it differs

`.github/workflows/ci.yml` is the authority. Two differences worth holding:

- **CI vets `examples/*` without running their suites.** The loop runs them.
  So a green CI does not mean an example's own tests passed — `examples/wysiwyg`
  has about a dozen — and a green loop here does. The loop is deliberately
  wider than CI in exactly this one place.
- **The root module's tests do not run under `-race` in CI.** The loop does
  not race them either. If your change touches goroutines, the property graph,
  or the Dispatcher, run `go test -race ./...` at the root by hand and say so.

The `-race` tier is not a nicety. Those tests exist to prove that no RPC, tool
body, activity goroutine, or child-process callback touches the property graph
off the UI goroutine, and without the detector that assertion is half made.
The tier is the `case` arm in the loop — read the arm, not a sentence
restating it.

## A red suite is yours

Main is expected green. **There is no known-bad list, and writing one is not
allowed.** If a test fails it is your problem until an issue that is still
**open** says otherwise, and you establish that by reading the issue's state,
not by reading prose.

That rule exists because the prose version failed open: a "Known-bad on main"
section named two issues as pre-existing failures that had *already been
fixed in that same file's ancestry*, so for its whole life it told every
reader to wave through a `-race` failure in `handlers/temporal` and a SIGWINCH
timing failure at the root — the two places a concurrency regression is most
likely to land. A stale dismissal is worse than no list, because it spends the
attention that would have caught the bug.

If you believe a failure is pre-existing:

```sh
git stash list                       # the stack is repo-global; look before you touch it
git worktree add /tmp/gooey-clean origin/main
cd /tmp/gooey-clean && go test ./...
```

Confirm it against `origin/main` in a clean checkout and report *that*, rather
than inheriting the belief. If you must record an expected failure, use
`t.Skip` with the issue number in the skip message — the claim then lives next
to the test and dies with it in the same commit.

## Before you say "verified"

- [ ] `verify.sh` exited 0 (or you read the verdict line yourself)
- [ ] You did not run `go build ./...`; `git status --porcelain` is clean of
      binaries — and remember porcelain lists an untracked *directory* once and
      never descends, so `git status --porcelain --untracked-files=all` is the
      one that shows you what is inside
- [ ] If you touched an invariant (see the `write-a-component` skill), you
      said so explicitly rather than letting the green imply it
- [ ] Any number you are about to write down came from a command you ran in
      this session, not from this file

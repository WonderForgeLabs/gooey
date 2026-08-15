---
name: land-a-pr
description: Get a gooey PR from branch to merged through this repo's actual gate. Use when opening a PR, reading review feedback, deciding whether a red or green check means anything, stacking dependent PRs, or waiting on CI. Covers the review gate failing OPEN, distinguishing infrastructure failures from verdicts, clearing a finding with a pr-feedback issue, the house commit and PR conventions, and why gh stack refuses an atomic merge.
---

# Land a gooey PR

## Before you push

```sh
git status --porcelain --untracked-files=all    # porcelain alone never descends
                                                # into an untracked directory
"${CLAUDE_PLUGIN_ROOT}"/scripts/verify.sh
gofmt -l $(git diff --name-only origin/main -- '*.go')
```

That last one matters more than it looks: **nothing in this repo checks
formatting.** `ci.yml` runs `go vet ./...`, which says nothing about gofmt; no
test in the tree invokes it; the only mentions in `.github/` are
`Bash(gofmt:*)` in the two Claude bots' `allowed_tools`. Re-derive that rather
than believing it:

```sh
command grep -rn gofmt .github/ ; command grep -rln gofmt --include='*_test.go' .
```

So an unformatted file gets to main unless a *reviewer* notices — which has
cost a PR a gate failure. The plugin's PostToolUse hook warns at edit time; this
is the second net.

## House conventions

- **Never `git add -A`.** Explicit pathspecs, always. This is how build
  artifacts entered this history. Commit with `-- <paths>` too, because another
  agent's staged change in a shared checkout otherwise rides along.
- Commit messages end with:
  `Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>`
- PR bodies: **explain the why, name the evidence, state what you did not do.**
  Read a few recent merges (`git log --format=%B -3`) — the bar is high and it
  is the repo's actual style, not a slogan. End the body with:
  `🤖 Generated with [Claude Code](https://claude.com/claude-code)`
- **Stack related PRs.** Standing instruction: base a dependent PR on the PR it
  depends on, never on `main`, and put "Stacked on #N" in the body. Emergency
  solo fixes are the only exception.
- Never commit, amend, push or merge unless you were asked to. Never force-push
  `main`.

## The gate, as it actually behaves

Run `gh pr checks <N>` and expect roughly: one `discover` job, one matrix leg
per module named after it (`. (test)`, `mcp (race)`, `paint (test)`,
`examples/wysiwyg (vet)`, …), `modules`, `contract`, `image`, plus
`review / pr-review`, `review / merge-gate` and the `review-with-tracking`
mirror.

**Do not learn the leg names from this file.** They are derived from the module
discovery, so they change when the tree does:

```sh
gh pr checks <N>
```

### `review / pr-review` fails OPEN — a green check is not a review

Two separate mechanisms end in `success` without a verdict ever being rendered.
So:

> **Decide from a rendered verdict, never from a green check and never from the
> absence of unchecked boxes.**

Require *both*: the run reached completion, **and** there is a verdict you can
read. If the tracking comment has no verdict in it, the review did not happen —
re-trigger it.

**Only close/reopen re-runs the review.** `workflow_dispatch` runs just the
mirror job; a label event skips the review; `gh run rerun --failed` skips it
too. (The "PR too big to ever review" theory is falsified — it has completed on
about the fifth attempt.)

### `merge-gate` failing in ~60s is infrastructure, not a verdict

A `merge-gate` that fails fast with "Failing closed" / "evaluator did not
complete" means **the evaluator never ran**. That is an OIDC/runner flake, not
a judgement about your PR. Read the job log first, then:

```sh
gh run rerun --failed <run-id>
```

Related: the bots run on forge's self-hosted pool (ADR-048 shims inherit
`forge-runners-default-vsphere`), so when the homelab ARC listener is down they
queue forever rather than failing. A job pending for a long time with no log is
that, not your PR.

The `review-with-tracking` mirror exists because a `workflow_call`'s jobs get
two-segment check names, so the literal required-status-check string has to live
in the shim. Its `if: !cancelled()` guard treats `skipped` as satisfied — safe
only while gooey has **no required checks**; adding branch protection turns that
into a merge hole. Note that #261 merged with `merge-gate` and
`review-with-tracking` both red, which is what "not required" looks like in
practice.

### Clearing a finding

A review finding clears **either** by a fix **or** by an issue that is open and
labelled `pr-feedback`. The gate counts nits, and filing the issue clears it —
proven on #231: file the issue, re-run, no push needed. Which means a green gate
may mean *filed*, not *fixed*; if you inherit a green PR, check whether the
findings were deferred.

To file them in bulk, apply the `claude-make-follow-up-issues` label: a bot
reads every review surface, groups the findings, files one issue per group, and
removes the label so it can be re-applied.

**Read every comment surface before merging.** Issue-level comments, inline
review comments, and thread replies are three different API endpoints, and a
green check is not consent:

```sh
gh pr view <N> --comments
gh api --paginate repos/WonderForgeLabs/gooey/pulls/<N>/comments
gh api --paginate repos/WonderForgeLabs/gooey/pulls/<N>/reviews
```

Also read the **first** attempt: `gh run view` and the check-runs API show only
the latest attempt, so a re-run hides the run that actually decided. Fetch
`/attempts/1/jobs`.

## Stacks

`gh stack link` **retargets PR bases and does not rebase.** The branches
therefore do not form a real linear chain, and the atomic `gh stack merge`
refuses until they do. Rebase them into a chain first, or merge bottom-up by
hand.

`gh stack submit` saying "up to date" is about **PR metadata, not the branch
tip** — confirm with `git rev-parse` against the remote.

**A squash merge orphans the stack above it.** After the bottom PR squash-lands,
the ones above it need `git rebase --onto`; find `<oldbase>` as the commit whose
*tree* diff against the squash commit is empty. A two-dot tree diff predicts
nothing about how a merge will go, and branch-head ancestry reads "not merged"
for every squash-merged PR whether it landed or was stranded — check
`merge_commit_sha` ancestry instead, and validate your method against a
known-good case before trusting it.

One landmine: **a workflow `uses:` pointing at a feature branch fails at LOAD
time.** Deleting that branch kills every job in the file, including ones that
never needed it. Split workflow commits out rather than merging that in.

## Waiting

```sh
gh run watch <run-id>     # not a polling loop
```

Back off rather than hammering; the self-hosted pool is shared with the whole
org.

## Before you say it is ready

- [ ] `verify.sh` exited 0, and `gofmt -l` on the changed `.go` files is empty
- [ ] `git status --porcelain --untracked-files=all` shows no binaries
- [ ] Every commit used explicit pathspecs
- [ ] The PR body says why, names the evidence, and states what you did not do
- [ ] Any damage-count number the change moved is justified in the body, not
      silently updated
- [ ] You read a **rendered verdict**, not a green check
- [ ] You read all three comment surfaces, and attempt 1 of any re-run
- [ ] Every finding is fixed, or has an **open** `pr-feedback` issue
- [ ] If this is part of a stack, its base is the PR below it and the body says
      "Stacked on #N"

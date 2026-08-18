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

Run `gh pr checks <N>` and expect roughly: one `discover` job, three matrix
legs named for their TIER and the number of modules in it — `vet (N
modules)`, `test (N modules)`, `race (N modules)` — then `modules`,
`contract`, `image`, plus `review / pr-review`, `review / merge-gate` and
the `review-with-tracking` mirror. The counts are rendered from discovery
at run time, so do not memorise them; they move whenever a module is
added.

A leg is a batch, so a red `race (N modules)` does not tell you WHICH
module broke — the leg names them in its `::error::` annotations and step
summary, and keeps going past its first failure so the list is complete.
Read the annotations, not just the check name.

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
too.

**`pr-review` completing is not the workflow going green**, and conflating the
two is the same mistake as the section below. On this repo's most-retried PR the
review job started succeeding early and kept succeeding; what held the workflow
red for another ten runs was `merge-gate` — infrastructure, not a judgement. So
"the review still hasn't run" and "the checks are still red" are different
claims needing different evidence. Read the jobs, not the run conclusion:

```sh
gh api repos/WonderForgeLabs/gooey/actions/runs/<id>/jobs \
  --jq '.jobs[] | "\(.conclusion)\t\(.name)"'
```

Note also that `cancel-in-progress` supersedes runs, so "the Nth attempt" is not
a well-defined quantity — count runs that were not cancelled, or don't count.

### `merge-gate` failing in ~60s is infrastructure, not a verdict

A `merge-gate` that fails fast with "Failing closed" / "evaluator did not
complete" means **the evaluator never ran**. That is an OIDC/runner flake, not
a judgement about your PR. Read the job log first, then:

```sh
gh run rerun --failed <run-id>
```

Related, and the exception is the actionable half: **which pool a job runs on
decides what a long pending means, and it is not one answer for all the bots.**
`review / pr-review`, `review / merge-gate` and the @claude bot come from
forge's reusable workflows and inherit `forge-runners-default-vsphere`
(ADR-048), so when the homelab ARC listener is down they queue forever rather
than failing — a job pending a long time with no log is that, not your PR.

`ci.yml`'s own jobs are self-hosted too, and that is recent — they were
`ubuntu-latest` until the pools took them over. So a pending `race (N
modules)` with no log now has the same ARC-listener diagnosis as the review
bots, where it used to be impossible.

But the `review-with-tracking` mirror is `ubuntu-latest` ("Trivially cheap
mirror step"), and so are **both** jobs of the `claude-make-follow-up-issues`
bot recommended above, `remove-label` included. Those are unaffected by the
homelab entirely, so the "ARC listener is down" diagnosis is simply wrong for
them. `issue-intake` and `issue-reopen-audit` use the `-small-` and `-agent-`
pools, and `ci.yml` spreads across `-small-`, `-default-` and `-medium-`.
Derive it rather than remembering it:

```sh
grep -rn 'runs-on\|uses: WonderForgeLabs' .github/workflows/
```

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

**Stack related PRs.** Base a dependent PR on the PR it depends on, never on
`main`. That is the standing rule here and it is the only thing in this section
that is a decision rather than a behaviour.

Everything else about `gh stack` is behaviour of a **v0.1.0 extension**, which
makes it the one place in this plugin where remembered facts would be written
down for a component guaranteed to drift — the exact defect the plugin exists to
remove. So: run the help, do not trust this file.

```sh
gh stack link --help      # retargets bases, and PUSHES; does not rebase
gh stack rebase --help    # the cascading rebase — the fix for a broken chain
gh stack merge --help     # what is checked locally vs. by GitHub
gh stack submit --help
```

The judgement worth carrying, which `--help` will not tell you:

- `link` gets you a stack whose branches need not form a real linear chain, and
  the atomic `merge` will not paper over that. **Note which layer refuses**:
  `merge` does no client-side linearity check at all — "Only basic pull request
  state is checked before merging (open and not a draft); GitHub evaluates
  branch protection and repository rules when the merge runs" — so the failure
  comes back from GitHub, that is where to read it, and no local flag bypasses
  it ("Bypassing merge requirements is not supported for stacks").
- The built-in fix is **`gh stack rebase`**, a cascading rebase that ensures
  each branch has the previous layer's tip in its history. Reach for that before
  rebasing by hand or merging bottom-up.
- `gh stack submit` saying "up to date" has been *observed* to be about PR
  metadata rather than the branch tip — but that observation is **uncertified**,
  and `submit --help` says the opposite ("1. Pushes all branches to the
  remote"). Reproducing it means running `submit` against a live stack, which is
  a write nobody should make to check a doc. The mitigation is cheap either way,
  and the docs promising a push makes it *more* worth doing, not less: confirm
  the tip with `git rev-parse` against the remote instead of trusting the
  message.

**A squash merge orphans the stack above it.** After the bottom PR squash-lands,
the ones above it need `git rebase --onto`; find `<oldbase>` as the commit whose
*tree* diff against the squash commit is empty. A two-dot tree diff predicts
nothing about how a merge will go, and branch-head ancestry reads "not merged"
for every squash-merged PR whether it landed or was stranded — check
`merge_commit_sha` ancestry instead, and validate your method against a
known-good case before trusting it.

**`gh stack sync` is the obvious tool for that and it is not sufficient.** Its
documented step 4 cascade-rebases stack branches onto their updated parents and
step 5 force-pushes them atomically; nothing in its contract is
*boundary-aware*, and a boundary-unaware replay after a squash is exactly what
manufactures the spurious conflicts above. (Reasoned from
`gh stack sync --help`, not observed — certifying it would mean force-pushing a
stranded stack.) This is the **opposite** call from the `link`/`merge` case
further up, where `gh stack rebase` *is* the built-in answer you should reach
for, and the two situations look alike without being alike: the hard part of a
post-squash recovery is not the rebase, it is finding the commit at which
main's squash and the stack's originals are the same tree — a judgement no
cascade makes for you.

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

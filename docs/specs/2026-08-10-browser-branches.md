# Demo browser: worktree/branch sources (issue #131)

**Status:** executed ([PR #134](https://github.com/WonderForgeLabs/gooey/pull/134)).

**Date:** 2026-08-10

> **One line below is superseded:** "last child of the Grid — document
> order is z-order". The picker's surface is a `gooey.Overlay` now and
> is lifted whatever its position; see
> `2026-08-30-overlay-layer.md` and `2026-09-05-overlay-ranks.md`. The
> Collapsed-until-`b` shape and everything else here still holds.

## What was asked for

The demo browser could only browse the tree it was launched from. The
ask (Elan): *"add to demo browser git worktree/branch support — so we
can see demos across branches/etc."* — compare a demo on `main` against
the version on a feature branch, from one running browser.

## Plan

A **source** is a checkout root. Three kinds, one type:

- the **launch tree** — source zero, always present, even outside git;
- any other **worktree** of the repository, enumerated with
  `git worktree list --porcelain`;
- a **local branch** with no worktree (`git for-each-ref refs/heads`),
  materialized on demand as a throwaway **detached** worktree under
  `os.MkdirTemp` and removed on switch-away and on exit.

Detached is load-bearing: the branch ref is never checked out, so the
browser can never collide with a real worktree that has (or wants) the
branch, and never moves anything. Real worktrees are read — `list`,
`status --porcelain --untracked-files=no` for the dirty marker, `log -1`
for tip subjects — and never written. The only mutating git commands
(`worktree add --detach`, `worktree remove --force`, `worktree prune`)
target directories this process created.

Everything the UI derives — demo list, README/doc preview, `.gooey`
badges, checked-in GIF fallback, the watcher fingerprint, `enter`'s
`go run` (with the existing `ownDir`/`modDir` resolution, so
nested-module demos and `go.mod` differences come along for free) —
resolves against the selected source. A demo that does not exist on an
older branch simply is not listed: the scan is per-source truth.

**Recordings are the deliberate exception.** `r` runs the demo in the
source's checkout but writes `recordings/` in the **launch tree**: a
recording is an artifact the user keeps, and writing it into an
ephemeral worktree would delete it minutes later — while writing it into
somebody's real worktree would violate "never touch existing worktrees".
`scan` therefore reads through a two-root `scanEnv` (source for content,
launch for recordings), and a GIF path carries the host root it resolved
under, so the player can tell two branches' same-named GIFs apart (the
decoded-clip cache keys on the host path now, not the `fs.FS` name).

The picker is the MenuBar overlay recipe applied in an app:

- last child of the Grid — document order is z-order — spanning the page
  but **Collapsed** until `b`; Collapsed is what keeps it out of tab
  order and hit-testing, and the composer's bounds/visibility sweep is
  what turns the flips into damage;
- a container (never pre-clears) whose popup child is the leaf, so the
  pre-clear paints exactly the box;
- modal while open: focus moves to the picker (the focus walk records
  stops by `AcceptsFocus`, not visibility, so `SetFocus` works on a
  collapsed-at-walk component), unhandled keys are swallowed so `q`
  cannot quit under the popup, and the pointer is captured — clicks on
  rows choose, clicks outside dismiss, wheel scrolls;
- dismissing restores focus to whatever had it.

Concurrency follows the house rule twice over. All git work runs on one
**worker goroutine** owned by a `sourceMgr` — enumeration, materialize,
release, in UI order, so a switch-away's `remove` can never race a
switch-back's `add` — and completions marshal back via `App.Post`. A
materialization superseded by a later pick adopts nothing; its worktree
stays registered for reuse and is reaped at exit. Cleanup runs after
`App.Run` returns and **before** `gooey.Exit`, which re-raises fatal
signals and never returns — a deferred cleanup would be skipped exactly
when the user hit ctrl+c.

Damage guarantees, pinned by tests: opening paints 2 nodes (popup +
the picker whose bounds went zero→page), navigating paints 1 (the
popup), dismissing paints 4 (the two vanished overlay nodes evaluate
without cells; grid and list are restored beneath). The first open is
scheduled by the **hint** computed reading `picker.IsOpen()` — the
popup's own node has no dependencies before its first evaluation, so an
always-painted node must carry the subscription (the same Get-order
lesson as the player's `Current`).

## Executed

- `cmd/browser/source.go` — `source` model and identity (a branch keeps
  its id across materialization), porcelain/for-each-ref parsers,
  `listSources` (launch first, then worktrees, then unclaimed branches;
  our ephemeral checkouts are hidden — the branch entry represents
  them), `sourceMgr` worker + ephemeral lifecycle.
- `cmd/browser/picker.go` — `sourcePicker`/`sourcePopup` overlay,
  grouped rows with ● active / `*` dirty / tip subjects, scroll window.
- `cmd/browser/main.go` — `cur` source property; `scanEnv` split roots;
  switch/adopt with supersession generation; `b` command enumerating on
  the worker; per-source launch/record; bound border title
  (`⎇ branch (ephemeral worktree)`), info-pane source line, hint swap.
- `cmd/browser/gifplay.go` — host-path clip identity (`gifFor` returns
  the root each candidate resolved under; cache, `Toggle`, `Stale` use
  it).
- `cmd/browser/watch.go` — `watchKey(srcRoot, launchRoot)`.
- Tests: parser fixtures, source identity, real-repo lifecycle in
  `t.TempDir` (materialize detached, reuse, per-source scan, hidden from
  enumeration, release, `Close` reaps everything, non-repo fallback),
  picker damage pins + modality + focus restore + capture routing,
  cross-root `gifFor`/`Stale`.
- Verified end to end against a throwaway repo under a pty: switch to a
  branch (worktree count 1→2), switch back (2→1), quit — no stale
  registrations, no temp dirs — and read-only against this repository
  (5 sources listed, active source marked and dirty-starred).

## Limits and follow-ups

- Enumeration cost is one `status` per worktree and one `log -1` per
  detached head, on the worker, at `b`-time. Fine at repo scale; a
  monorepo with hundreds of worktrees would want caching.
- A SIGKILL (not ctrl+c) leaks the temp worktree until the next
  `git worktree prune`; the directory itself is under the system temp
  dir. `git`'s own locking makes the stale registration harmless.
- Ephemeral checkouts share the launch tree's build cache (same
  `GOCACHE`), so `enter` on a branch is a warm build, not a cold one.

# FileWatcher: what a change is, and who is allowed to look

**Date:** 2026-08-23
**Issue:** [#272](https://github.com/WonderForgeLabs/gooey/issues/272)
**Status:** implemented — `components/filewatcher.go`, `markup/elements.go` (`defFileWatcher`)

`Timer` made "run something on an interval" a thing you *declare*. There
was no equivalent for "run something when a file changes", so five
implementations grew instead — `markup.Watch`, `markup.WatchAll`,
`cmd/browser`'s `watchKey`, the intro deck's counter stamp, and a shell
script polling `stat -c %Y` — and no two agreed on what a change is.

This record is the disagreements, decided.

## A directory is not watched by its ModTime

A directory's own mtime moves when an entry is **added or removed** and
not when one is **edited**. Watching a directory by `ModTime` therefore
misses every edit, silently — the exact failure a watcher exists to
prevent, and the reason `cmd/browser` already used a fingerprint.

So the fingerprint has three domains, and they are distinguishable from
each other rather than only from themselves:

| the path resolves to | folded from |
|---|---|
| a file | its own size and mtime |
| a directory | every entry's name, size and mtime (subdirectories by name only) |
| nothing | a distinct **absent** marker |

Measured on this machine (WSL2, 2026-08-23) rather than assumed, because
the rule has a second half nobody states. With a second between the
operations the textbook holds exactly — a directory's mtime does not move
on an in-place edit, and does move on an add and on a remove:

    edit in place : dir mtime moved = false
    add an entry  : dir mtime moved = true
    remove entry  : dir mtime moved = true

Run the same three events back to back with no delay and **all three read
false**, on both the temp filesystem and the worktree. So a `ModTime`
watcher has two blind spots, not one: the edit it can never see, and the
add-or-remove it misses whenever the events land inside a single
timestamp tick — which is most of the time, since that is what a save
burst and a test both look like. The entry fold sees all of them, because
a new name changes the fold whatever the clock did.

Absence being a *state* rather than an error is what makes "watch a file
that does not exist yet" and "deleted and recreated" ordinary changes.
A file replaced by a directory of the same name is a change because the
domain tags differ, which no comparison of sizes or times would catch.

The walk is **one level deep**. A recursive walk of a source tree several
times a second is a cost a declarable component cannot impose on every
page that wants to notice a save; to follow a subtree, name its
directories.

## One hit per poll, and the window is the interval

Coalescing is by **state comparison**, not event counting. However many
watched paths changed however many times between two polls, `Changed`
runs once, and `Path` names the first of them in `Paths` order.

This is the structural advantage of polling and it is why the component
does not wait for a filesystem-notification API to be useful. An editor's
rename-and-replace save produces several raw *events* but only two
observable *states* inside one window, and only the endpoints are
compared. The residual is stated rather than hidden: a poll that lands
between the temp file's arrival and the rename sees the intermediate
state, and that save produces two hits. At 300 ms that window is on the
order of a millisecond in three hundred.

The mechanism has one load-bearing line. `scan` restamps **every** path on
the poll that reports a change, including the paths after the one
reported. `WatchAll` breaks out of its loop after the first change; doing
that here would leave the rest looking changed on the next poll and turn
one save of three files into three hits. `TestScanCoalescesEveryChangeInOnePollIntoOneHit`
asserts the second poll is silent, which is the half that fails against
the `break`.

A trailing-edge debounce was considered and rejected: it turns a
continuously-appended file into a watcher that never fires at all, and
buys only the ~0.3% rename case.

## The baseline is taken inside Start, on the UI goroutine

`markup.Watch` takes its baseline inside its own goroutine, so a write
that races the launch is swallowed with nothing to say it was. Here the
first scan is synchronous inside `Start`, which the `Composer` calls on
the UI goroutine: **everything true when `Start` returns is the
baseline.** The same rule covers a path that joins a bound `Paths` list
later — its current state is recorded silently, because you changed the
list and do not need to be told.

The test for this needs a long interval and an immediate write, and that
is not incidental. With a short interval, a baseline taken on the
goroutine's *first tick* is still earlier than any write a test could
manage, so the obvious version of the test passes against the bug it is
named for.

## Confinement: the poll goroutine cannot even read Paths

Nothing in the framework catches a property touched off the UI goroutine.
So the poll goroutine holds no handles at all: it **posts a closure** that
reads `Paths` on the loop and replies over a buffered channel, and scans
whatever comes back. `Enabled` is read at fire time, on the loop, exactly
as `Timer` reads it.

Because `Enabled` gates the **hit** and not the poll, a change made while
disabled advances the baseline and is dropped, and re-enabling resumes
without replaying it. Fire time is *drain* time, which has a consequence
worth knowing: a hit generated while disabled sits in the dispatcher
queue until something drains it, so flipping `Enabled` before draining
delivers it. That is the contract working, and a test that does not pump
the queue empty first will read it as a bug.

The goroutine also may not **block** on the loop. `Composer.Close` runs on
the UI goroutine, so a join against a goroutine parked on a post the loop
will never drain is a deadlock; the wait for the reply is a `select` that
`done` wins.

## Why not gooey.Every

`gooey.Every` owns the close-and-join contract and the default answer to
"a Startable needs a ticker" is to use it. It does not fit here, for two
reasons that are the same reason twice:

- `Every` runs `fn` **on the UI goroutine**, and the entire point of a
  watcher is to keep filesystem I/O — a `ReadDir` over a directory that
  may be large, cold, or on a network mount — off the render loop.
- `Every` would post a closure per tick forever; this posts one per
  actual change.

`Delays` is a group of one-shot deadlines and fits less well still. So
`FileWatcher.Start` hand-rolls its channels, like `Companion.Start`, and
pays the debt `Every` exists to collect: the stop func **closes and
joins**, pinned by `TestFileWatcherStopIsABarrierNotASignal`, which runs
twenty rounds against a deliberately slow sink because a single
stop-then-check passes against a signal-only stop most of the time.

## Zero paths, and where the load errors are

`Paths` is required as written and inert as resolved — the same split
`Timer` uses for `Interval` and `Tick`. `Paths=""` is a typo and a load
error; a **bound** list that resolves empty is a page under construction
and is legal, which is what makes `Paths="{{.MaybeEmpty}}"` safe.

Everything else the loader can see fails at load, because a watcher's one
unacceptable failure mode is being silently deaf:

- a literal path that is not an `fs.FS` path (rooted, or with `..`
  elements) — it would `fs.Stat` as `ErrInvalid` forever, which the
  component cannot distinguish from "not there yet";
- a non-positive or unparseable `Interval`;
- a tree with **no `fs.FS`** at all, which is what a page built from raw
  bytes is. `ctx.assets()` supplies the page's FS or `Context.Includes`;
  neither means the element cannot do its job and says so.

A **bound** path list cannot be checked at load and is not: an invalid
path in one arrives as the absent state, and the component's doc comment
says so.

## The fs.FS seam

The component takes an `fs.FS`, not a filesystem path. That is what keeps
`os.DirFS` + watcher (dev) and `embed.FS` (release) the same code path: an
`embed.FS` reports a constant zero `ModTime` for every file, so a watcher
over one is a natural no-op rather than an error, and the same markup
runs in both tiers.

## Adoption

`apps/introdeck` loses its counter stamp (`Deck.counterStamp`, a
`size + "@" + ModTime` string compared inside `syncCounter` on the deck's
own one-second `Timer`). It becomes a `<FileWatcher>` in `deck.gooey`
whose `Enabled` is a computed over the same condition the gauges use, so
the pause is the graph's rather than an `if` statement's, and the
first-sample guard the stamp needed is now the baseline rule.

Four hand-rolled watchers remain, and two of them are not this
component's job at any point: `markup.Watch` and `markup.WatchAll` drive
the rebuild that *creates* a composition, so they cannot be owned by a
component inside one. `cmd/browser`'s `watchKey` fingerprints several
roots with suffix filters and one level of subdirectory folding — a
superset of this component's directory case that would have to become
configuration. The shell script (`apps/introdeck/watchgo.sh`) rebuilds a
Go program on save and needs a `<Companion>` beside the watcher to
replace it.

## Relationship to #53

Different layer, both wanted. [#53](https://github.com/WonderForgeLabs/gooey/issues/53)
replaces the *mechanism* — polling → filesystem notifications — under the
existing markup watcher. This is the *declarable surface*, and it
inherits #53's fix for free when it lands, because the mechanism is
behind the same seam either way. Two of the decisions above would need
revisiting then: coalescing becomes event-shaped rather than state-shaped
(so the rename-and-replace residual gets *worse*, not better, without an
explicit debounce), and the absent state stops being free.

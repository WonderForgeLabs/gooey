# The retired z-order rule, swept — and a guard so it stays swept

**Issue:** [#443](https://github.com/WonderForgeLabs/gooey/issues/443) — "~15 sites still teach 'declare it last, document order is z-order' after the Overlay layer"
**Date:** 2026-09-05
**Follows:** [#437](https://github.com/WonderForgeLabs/gooey/pull/437) (the layer), `2026-09-05-overlay-ranks.md` ([#439](https://github.com/WonderForgeLabs/gooey/issues/439)), `2026-09-05-one-shot-overlay-order.md` ([#438](https://github.com/WonderForgeLabs/gooey/issues/438))

## What the rule is now

Three changes in a week moved it twice, and the current statement has
three parts:

1. **Overlays are lifted.** A subtree whose root implements
   `gooey.Overlay` comes out of document order into a second paint
   layer, so it paints above the page *from wherever it is declared*.
2. **Ranks order that layer.** `OverlayRankPopup` (0) →
   `OverlayRankToast` (10) → `OverlayRankAdornment` (20). `appendByRank`
   is a **bucket pass**, not a stable sort — the word "stable" stood here
   until review of #458, and `docs/architecture.md` retires it in this
   same PR. What the buckets buy is the same observable property: equal
   ranks keep document order, and nothing else does. The rank is asked of
   the lifting root only.
3. **Input was not lifted.** Hit-testing still walks plain document
   order, last sibling first. `Overlay` moves paint, not input.
   ([#465](https://github.com/WonderForgeLabs/gooey/issues/465): this
   item is what was true on 2026-09-05 and is superseded — that change
   made `FocusManager.HitTest` ask `overlayOf`, the same membership-and-rank
   rule the paint order is derived from, so the hit walk is lifted and
   ranked too. It is left in place because this heading enumerates the
   three facts the sweep was written against — deleting the third would
   make the sweep's own reasoning unreadable — but a reader landing on
   this section must not take item 3 as current.)

The retired rule — *"declare the overlay LAST, because document order is
z-order"* — is false about (1), silent about (2), and only ever
accidentally right about (3).

## What the sweep found

The issue named ~15 sites and gave a table. **The sweep fixed about
thirty-five.** That gap is the finding, not a footnote: the table was a
sample taken by searching for the phrasings already in the table, and
the first pass of this sweep repeated the mistake by deriving its grep
from the issue. Sites the table missed included

- `docs/learn/howto/howto-popup.md` — the how-to for *building* one of
  these, teaching the retired rule as wiring step 1 of 4;
- `docs/learn/howto/howto-forms.md`, `docs/learn/index.md`,
  `README.md`'s adornments row;
- `apps/wysiwyg/wysiwyg.gooey` (twice), `cmd/browser/browser.gooey`,
  `cmd/colors/colors.gooey`, `cmd/toolkit/toolkit.gooey` — including
  two strings the flagship demo puts **on screen**;
- seven test-fixture comments across `components/`, `markup/` and
  `cmd/browser/`, each explaining that a page was arranged a certain way
  for a reason that had stopped being true;
- the recap and next-steps sections of `docs/learn/07-app-chrome.md`,
  whose *body* #437 had already corrected — one file teaching both rules.

**Scope decision** (Elan raised it explicitly on the issue): the sweep
covers Go doc comments, test comments and `.gooey` demo markup, not just
`docs/**`. Nothing else looks for those, and they are the copies read at
the moment somebody is writing the code that depends on them.

## Dated specs were not rewritten

`2026-08-10-toolkit-wave2.md`, `2026-08-10-adornments.md` and
`2026-08-10-browser-branches.md` chose the retired mechanism, and that
is what they are *for*. Each got a **superseded banner in its head**
naming what replaced it and what still holds; their bodies are
untouched. A spec records what was decided on its date.

## The guard

`TestNoFileTeachesTheRetiredOverlayRule` (`zorderdocs_test.go`) walks
every `.go`, `.md` and `.gooey` file in the tree and fails on any line
stating the retired rule **without a qualifier within two lines** —
either the correction that makes it true (the second layer, a rank,
`gooey.Overlay`) or a marker that the sentence is quoted as history
("used to say", "superseded", "no longer").

Three deliberate shapes:

- **Not a ban on the phrase.** A dozen sites quote the old rule in order
  to bury it, and a test demanding absence would delete the history with
  the error.
- **Not an allow-list.** Nothing here names a file. A file added next
  year is covered without anyone remembering this test exists — which is
  the property the issue's own table lacked.
- **A file-level exemption for a superseded banner in the head**, which
  is what lets the dated specs keep their bodies.

### Why the window is two lines

It was six first, and **mutation testing killed that version.** A stale
sentence re-introduced into `components/menu.go` and another into
`cmd/browser/browser.gooey` both passed, because each landed within six
lines of prose correctly explaining the new rule.

That is not a hypothetical failure mode — it is the exact one #443
describes. The issue calls `components/popup.go` the costliest site
*because* it said "LAST, because document order is z-order" eight lines
above a type implementing `OverlaysPage`. The file documented two
contradictory rules and looked well-maintained doing it. A six-line
window would have let any file that explains the current rule become a
safe harbor for the old one.

Two lines is the width of the epitaph every corrected site actually
carries, and too narrow to reach a neighbouring paragraph that happens
to be right.

### How the guard is itself checked

A negative assertion over a tree that already satisfies it passes for
any reason at all, including a regex that matches nothing.

| Failure mode | What catches it |
|---|---|
| the predicate matches nothing | `TestTheRetiredRuleGuardCanActuallyFire` feeds it the sentences the sweep really removed, plus a `neverShipped` list of phrasings the repo never used |
| the predicate matches everything | the same test feeds it correct sentences from the same files |
| a single pattern is dead or subsumed | the per-pattern loop in that test requires EVERY `retiredRule` entry to match at least one sample — the list-as-a-whole loop above it passes while an individual pattern matches nothing |
| a pattern needs a word the prefilter drops | the prefilter contract loop in that test requires every sample to contain a `prefilterWords` word, so a pattern cannot be switched off for the whole tree by being added to |
| a rule statement is WRAPPED across two comment lines | the scan tests line i joined to i+1 as well as line i, pinned by `TestAWrappedRuleStatementIsStillCaught`; every sample in the fire test is a single line, so nothing else could show the hole |
| a CORRECTION is wrapped and stops exempting | the qualifier window is joined as prose rather than with newlines, pinned by `TestAWrappedQualifierStillExempts` — otherwise the join above turns the sweep's own corrected comments into failures |
| the walk visits no files | a floor asserted before any line is read |
| a qualifier is accepted from too far away | mutations Z1/Z2/Z3 re-introduce a stale line into Go, markdown and `.gooey` next to correct prose |
| the hit-test exemption excuses a paint claim beside it | ~~TestTheHitTestExemptionIsLineScoped~~ — that one qualifier read a single line, not the ±2 window, and reverting it to the window was otherwise silent. EXPIRED: [#465](https://github.com/WonderForgeLabs/gooey/issues/465) made the hit walk rank-aware, so the exemption it guarded was deleted and the guard with it. Spelled without backticks for the reason specclaims_test.go gives — a live citation to a dead test reads as a check while checking nothing |

All of them fire. Z1 and Z3 **passed** against the six-line version,
which is how the window got narrowed.

**The list sizes and the floor are deliberately not written here.** This
table said "eight sentences", "five correct sentences" and "a floor of
100" while the guard held different numbers and a floor of 500 — drift
inside the same commit range as the sweep that exists to catch drift
(raised in review of #458). Read them from `zorderdocs_test.go`; a count
in prose is a sample taken once.

## What is deliberately not guarded

The word "z-order" on its own, and "last child" on its own. Ordinary
document order is still the rule for everything not lifted: dozens of
comments correctly describe the forward pass, the restore sweep, and
overlapping `Canvas` children, and a `VStack`'s last child really does
get the remainder. Only the two together, or the bare equation, ever
meant the thing that stopped being true.

One consequence worth naming: `apps/wysiwyg/components/preview/overlay.go`
is called `Overlay` and is **not** a `gooey.Overlay`. Its correctness
depends on ordinary document order — it must be `Pane`'s second child —
so making it implement the marker would lift the design guides above the
whole page instead of above the previewed document. That is now written
in the file.

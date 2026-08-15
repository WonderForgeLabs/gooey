---
name: tests-that-can-fail
description: Write a test that can distinguish the two answers it was written to tell apart. Use whenever adding or reviewing a test, pinning a bug fix, writing a fixture, sweeping a table or vocabulary, or asserting that something does NOT happen. Covers the mutate-and-watch-it-fail rule, the over-generalising direction that gets missed, discriminating fixtures, and the four ways a green test in this repo has reported success without having checked.
---

# Write a test that can fail

## The rule

**Mutate the thing you claim to pin, and watch the test go red.** Not the
suite — *that test*, and ideally only that test. A test you have never seen
fail is a hypothesis, not a pin.

Then mutate in the **over-generalising direction**, which is the one that gets
missed. It is easy to check that a test fails when the behaviour is *removed*.
The failure mode this repo keeps hitting is a test that also passes when the
behaviour is applied *everywhere* — no guard at all, always-draw, always-accept,
always-succeed.

```
        under-generalise            over-generalise
        (behaviour removed)         (behaviour applied unconditionally)
        "return an error"           "return an error for everything"
        "draw cells"                "always draw cells"
        "the patch is rejected"     "the server rejects nothing"
        "no repaint happened"       "the harness cannot report a repaint"
```

Most tests catch the left column by construction. The right column needs a
deliberate second case, usually called the *discrimination arm* or the
*control* in this repo's tests.

## Four ways it has actually gone wrong here

These are not hypotheticals. Each cost real time and each shipped green.

**1. The assertion was on a quantity that could not discriminate.** A
degenerate-size test checked pixels where only *bounds* could tell the two
answers apart. `graphics/scale_test.go` records the general version: every
existing test in the package encoded at its source's exact pixel size, so
`Scale` ran at 1:1 and *any filter whatsoever* passed. `Scale` had effectively
no tests while looking well covered. Same for `gifplay`:
`TestDecodeClipScalesDown` only checked `fitDown`'s arithmetic, so `decodeClip`
could subsample every frame with the file green.

**2. The fixture could not tell the answers apart.** A sixel alpha test used
`color.RGBA{a, a, a, a}` — which is **white at every alpha**, so "its own
colour" and "its premultiplied contribution" differed only in brightness and no
count could separate them. The fix is in `graphics/halfblock_test.go`:
saturated primaries at low alpha, where 25%-alpha red is stored `(64,0,0)`
against an own-colour `(255,0,0)` — 191 of 255 apart on a channel, nowhere near
rounding. Read that file before writing a fixture; it also labels its
non-discriminating column as such:

> Fully transparent. This column CANNOT tell the two answers apart … so it is
> not evidence for the claim above.

**3. A second mechanism caught it, so the guard under test was never
exercised.** `Image`'s pixel-vs-cell tier guard passed with the guard deleted,
because `App.caps` backfills `term.DefaultCellW/H` for a pinned protocol
(`app.go`) and closed the hole on the App path. The bug was only reachable
through a `Composer` driven directly, where `SetGraphics` takes an encoder and
no metrics at all. If your test goes through a convenience wrapper, ask what
else that wrapper does.

**4. A sweep over a table checks only the rows that are there.** A
vocabulary sweep looked complete and stayed green when a row was deleted,
because `for _, e := range table` simply iterates one fewer time. In this repo
today, `examples/wysiwyg`'s `TestPaletteComesFromTheCatalog` guards
completeness with `len(ed.palette) < 20` against a catalog holding
substantially more than 20 — several rows of slack, so deleting most single
elements from `markup/elements.go` trips nothing. The only real completeness
guards nearby (`markup/catalog_test.go`) check a **fixed list of named
elements**, which catches deleting one of *those* and nothing else.

`examples/wysiwyg/perkind_test.go` names this class in its own comment, and is
the model for what to do instead — assert synthetically when the real data has
no instance of the case:

> The required half is asserted SYNTHETICALLY, because the vocabulary
> currently contains no instance of it — every required attribute is free text
> or a binding, so a sweep over the real elements checks zero rows and passes
> for that reason alone. Writing the sweep and reading its green would have
> been a check that reported success without having checked.

## The procedure

1. **Say out loud what the two answers are.** "With the fix, X; without it,
   Y." If you cannot state Y concretely, you do not yet have a test.
2. **Check that your assertion can separate X from Y numerically.** If X and Y
   differ by less than rounding, or differ only in a quantity you are not
   measuring, change the fixture — not the tolerance.
3. **Apply the mutation and run.** Diff-prove the mutation actually landed
   (see the harness trap below), run the suite, and record *which* tests went
   red and what they said.
4. **Apply the over-generalising mutation and run.** Delete the guard, accept
   everything, always take the branch. If nothing goes red, you need a
   discrimination arm.
5. **Revert, and write down the A/B in the commit message.** This repo's
   commit messages do this — see `a41fcd3`, which quotes the exact failure text
   the mutation produced.

### The harness trap that makes both arms agree

An A/B whose two arms agree is a **harness result**, not a finding. The
specific way it has happened here: `git stash push -- <file>` on a *committed*
file reverts nothing, so both arms ran the fix and of course agreed. Before
trusting an A/B, **observe the revert changing something** — `git diff` after
applying the mutation, and confirm the bytes moved.

## Discrimination arms already in the tree

Copy these shapes rather than inventing one. Each names, in its own comment,
the wrong implementation it exists to fail against.

| File | Arm | The wrong answer it rejects |
| --- | --- | --- |
| `components/image_test.go` | fourth case, `{"both known", 8, 16, true}` | "always draw cells" — which would pass every other line |
| `graphics/sixel_test.go` | `TestAPaletteBiggerThanTheLimitIsCutRatherThanTruncated` | an encoder that keeps the first 256 colours it sees |
| `graphics/scale_test.go` | `TestTheFilterWidensWithTheReductionRatio` | `ApproxBiLinear`'s fixed 2×2 tap — and the ratio is deliberately non-integer, because at 4:1 the fixed tap straddles two light and two dark pixels and averages correctly *by luck* |
| `graphics/scale_test.go` | `TestScaleNeverOvershootsTheSourceRange` | a cubic kernel's negative lobes (ringing on hard edges) |
| `examples/wysiwyg/serve_test.go` | the third test | "a patch that always succeeds proves nothing" |
| `grpc/grant_test.go` | the two arms | "a fix that returned NOT_FOUND for everything would pass" |
| `settings/settings_test.go` | the second half | "if the store simply never wrote, the test above would pass for the wrong reason" |
| `markup/frozen_dynamic_test.go` | the control | proves the harness *can* report a repaint, so an asserted 0 is meaningful rather than an unfalsifiable small number |

That last row is the general form for any **negative** assertion. "Nothing
repainted", "no error was raised", "the event did not fire" — all of them pass
identically when the measuring apparatus is broken. Pair every negative
assertion with a positive control proving the apparatus fires at all.

## gooey-specific: a damage-count assertion is the only pin for a repaint claim

`Composer.Frame()` returns `(*Frame, int)`, where the int is how many
components repainted. `App.PaintedLastFrame()` is the same number on the
runtime; `Composer.Damage()` gives the rects.

A bounds assertion, or a "the cell says X" assertion, **passes just as well
when the entire tree repainted** — so it proves nothing about damage. If your
claim is "this change repaints exactly the components that read it", the
assertion is a count.

These are contract tests. Find how many there are rather than trusting a
number in prose:

```sh
cd "$(git rev-parse --show-toplevel)" &&
  command grep -rn 'painted' --include='*_test.go' . | command grep -c 'want\|!='
```

**If your change moves one of those numbers, that *is* the change**, and it
needs justifying in the PR body rather than quietly updating.

The bug they catch and nothing else does: **dependencies are recorded by the
`Get` that actually runs**. A `Get` behind an early `return`, or on the
short-circuit side of `&&`/`||`, drops out of the dependency set on the frames
where it does not execute, and the component goes deaf to that property — no
error, no panic, just a stale cell. Hoist `Get`s above early returns and OR the
results afterward.

## Pinning a document against reality

When the thing you are pinning is prose — a doc, a workflow, a config — do not
reimplement what it says. **Extract the literal command out of the document,
execute it, and compare against an independent oracle.** A reimplementation
passes while the documented command is broken, which is the failure the pin
exists to prevent.

`claudemd_test.go` and `ciworkflow_test.go` are the worked examples: regex the
`find` out of `CLAUDE.md` and out of `ci.yml`, run both, diff each against a
`filepath.WalkDir` in **both directions**, and separately compare the two
extracted strings to each other.

## Checklist

- [ ] I can state the wrong answer this test rejects, in one sentence
- [ ] I applied that wrong answer and watched this test — and ideally only this
      test — go red
- [ ] I applied the **over-generalising** wrong answer (no guard, accept
      everything) and something went red
- [ ] I confirmed the mutation actually landed in the file before believing the
      arms
- [ ] Any negative assertion has a positive control next to it
- [ ] A sweep over a table has a completeness guard against an **independent**
      source, not a threshold with slack in it
- [ ] Any repaint claim is pinned by a count, not by a cell value
- [ ] The test's comment says which wrong implementation it fails against, so
      the next reader does not have to re-derive it

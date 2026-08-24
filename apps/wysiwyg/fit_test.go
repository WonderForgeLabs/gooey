package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/prop"
)

// The editor's minimum size, and the failure it used to produce.
//
// # The debt this file recorded, and what collecting it cost
//
// This header used to say: four tests that read the SHIPPED page were
// removed at b41aa2a, deliberately, because the page had gone empty and
// "a test that cannot fail for the reason it names is worse than an
// absent one". It named the condition for their return in capitals —
// WHAT THE NEW SHELL OWES THIS FILE: the same four assertions, pointed at
// whatever it declares.
//
// The new shell arrived. Nobody collected, and nothing went red to say
// so, because the debt was prose. Worse, the header also said "watchFit
// already no-ops when no element is named Shell, which is why the empty
// page costs nothing at runtime" — true when written, and the sentence
// that hid issue #355 for the whole life of the file. watchFit was not
// no-opping harmlessly against an empty page; it was failing to resolve
// a name the page had simply never declared, forever, with a comment
// calling it a startup transient.
//
// So the four are back, below, against the shipped markup. What changed
// in how they are written is only where the numbers come from — see
// TestBothMinimumsComeFromTheDeclaredTracks — and the file has grown two
// tests the old set could not have had:
//
//   - TestTheFitCheckActuallyRunsAgainstTheShippedPage. The defect was
//     never a wrong number. Every arithmetic test in this file passed
//     throughout, because the arithmetic was right; what was broken was
//     that nothing called it. An assertion about a computed minimum is
//     satisfied vacuously by a watcher that never fires, so the firing
//     is now pinned separately.
//   - TestTheShellNamesResolveInTheShippedPage. #355 was a string typed
//     twice in two files and agreeing with nothing. This makes the two
//     files check each other.

// buildFitPage builds the real page and returns the editor, its shell
// Grid, its dock, and the root — so every number below comes from the
// shipped markup rather than from a copy of it in a test.
//
// It resolves through the SAME constants fit.go uses. A test with its own
// spelling of "Page" would go green against a page that had renamed it,
// which is the bug this file exists to have caught.
func buildFitPage(t *testing.T) (*editor, *components.Grid, *dockHost, gooey.Component) {
	t.Helper()
	ed, root := buildPage(t)
	g, err := markup.Find[*components.Grid](ed.ctx, ShellName)
	if err != nil {
		t.Fatalf("no <Grid Name=%q> in the shipped page: %v", ShellName, err)
	}
	d, err := markup.Find[*dockHost](ed.ctx, DockName)
	if err != nil {
		t.Fatalf("no <DockHost Name=%q> in the shipped page: %v", DockName, err)
	}
	return ed, g, d, root
}

// TestTheShellNamesResolveInTheShippedPage is the direct pin for #355.
//
// The bug was `markup.Find(ed.ctx, "Shell")` against a page whose root is
// `Name="Page"` — two files holding one fact and never compared. Both
// names now come from fit.go's constants and are required to resolve
// against the shipped markup, so renaming the element without renaming
// the constant is red rather than silent.
//
// It also asserts the TYPES, because Find is generic: a `Name="Page"`
// that stopped being a Grid would resolve as a name and fail as a lookup,
// which is the same defect wearing a different hat.
func TestTheShellNamesResolveInTheShippedPage(t *testing.T) {
	ed, root := buildPage(t)
	if _, err := markup.Find[*components.Grid](ed.ctx, ShellName); err != nil {
		t.Errorf("fit.go looks up <Grid Name=%q> and the shipped page has no such element: %v",
			ShellName, err)
	}
	if _, err := markup.Find[*dockHost](ed.ctx, DockName); err != nil {
		t.Errorf("fit.go looks up <DockHost Name=%q> and the shipped page has no such element: %v",
			DockName, err)
	}
	// Control: a name the page really does not declare must fail, or the
	// two assertions above would pass against a Find that returns
	// anything it is asked for.
	if _, err := markup.Find[*components.Grid](ed.ctx, "NoSuchElementAnywhere"); err == nil {
		t.Fatal("Find resolved a name the page does not declare; the assertions above prove nothing")
	}
	_ = root
}

// TestTheFitCheckActuallyRunsAgainstTheShippedPage is the test #355 most
// needed and could not have had.
//
// watchFit's body lived behind a *gooey.App, `go test` has no terminal,
// and so the one thing that mattered — whether the check ever gets past
// its own first line — was unreachable. It asserts EXECUTION, in both
// directions, against the real page:
//
//	small terminal  -> fits false, PROBLEMS pane says something
//	large terminal  -> fits true,  PROBLEMS pane says nothing
//
// Either arm alone is vacuous. A watcher that never fires leaves fits at
// its initial true and FitMsg at its initial "", which is exactly what
// the large-terminal arm expects — so without the small arm this test
// passes against the bug it was written for.
func TestTheFitCheckActuallyRunsAgainstTheShippedPage(t *testing.T) {
	ed, _, _, _ := buildFitPage(t)
	_, usable, err := ed.shellMinimum()
	if err != nil {
		t.Fatal(err)
	}

	// Preconditions: the initial state must be the OPPOSITE of what the
	// small-terminal arm asserts, or that arm cannot tell a run from a
	// no-op.
	if !ed.fits.Get() || ed.fitMsg.Get() != "" {
		t.Fatalf("before any check, fits=%v fitMsg=%q; this test cannot distinguish "+
			"a check that ran from one that did not", ed.fits.Get(), ed.fitMsg.Get())
	}

	ed.checkFit(usable.Cols-1, usable.Rows-1, true)
	if ed.fits.Get() {
		t.Error("one cell under the usable minimum, the editor still claims to fit: " +
			"the check did not run")
	}
	if ed.fitMsg.Get() == "" {
		t.Error("the PROBLEMS pane is empty at a size that does not fit. FitMsg is bound " +
			"twice in wysiwyg.gooey and could never say anything at all before #355")
	}

	ed.checkFit(usable.Cols, usable.Rows, true)
	if !ed.fits.Get() {
		t.Errorf("at exactly the usable minimum %s the editor claims not to fit", usable)
	}
	if got := ed.fitMsg.Get(); got != "" {
		t.Errorf("the PROBLEMS pane still reads %q at a size that fits; a stale complaint "+
			"about a size the terminal no longer is is worse than silence", got)
	}
}

// TestAnUnresolvableShellNameIsLoudAfterTheFirstFrame.
//
// The whole of #355 is that this case returned silently behind a comment
// claiming it was a startup transient. The two cases are now separated by
// a FACT — whether a frame has been composed — rather than by a hopeful
// sentence, and this asserts both sides of that fact.
func TestAnUnresolvableShellNameIsLoudAfterTheFirstFrame(t *testing.T) {
	ed, _ := buildPage(t)
	// Break the lookup the way a rename would: remove the name.
	delete(ed.ctx.Named, ShellName)

	// Before the first frame, a miss is genuinely ambiguous and stays quiet.
	ed.checkFit(200, 60, false)
	if got := ed.fitMsg.Get(); got != "" {
		t.Errorf("before the first frame an unresolved name said %q; that case really is "+
			"a transient and must stay quiet, or every startup shows an error", got)
	}

	// After one, it is a programming error and must be impossible to miss.
	ed.checkFit(200, 60, true)
	msg := ed.fitMsg.Get()
	if msg == "" {
		t.Fatal("after a frame has been composed, an unresolvable shell name said NOTHING. " +
			"That is #355 exactly: a permanent failure wearing a startup transient's comment")
	}
	if !strings.Contains(msg, ShellName) {
		t.Errorf("the complaint does not name the element it could not find: %q", msg)
	}
	if !strings.Contains(ed.status.Get(), ShellName) {
		t.Errorf("the status bar does not carry the complaint: %q", ed.status.Get())
	}
}

// TestAFixedTrackIsTruncatedBelowTheHardMinimum replaces the fourth owed
// assertion, and the replacement is the whole finding.
//
// # The assertion that was owed no longer has a defect to name
//
// b41aa2a's TestTheShellIsArrangedOffScreenBelowItsHardMinimum asserted
// that one row below the hard minimum the shell arranges children PAST
// THE BOTTOM of the screen: Grid.offsets accumulated fixed tracks
// unclamped, so once the fixed demand exceeded the extent, every track
// from that point on got a rect outside the parent and painted there.
//
// That defect is gone. components.Grid gained clampToExtent in 1c119ff
// (PR #242), and its doc comment names this very app as where it was
// found — "apps/wysiwyg, whose shell is Rows=\"1,1*,12,1\" ... the 12-row
// markup pane runs past the bottom and the status bar is arranged
// entirely outside the screen". Measured on the shipped page at one row
// below the hard minimum, nothing overflows: the status bar is arranged
// at Y=1 with H=0, truncated rather than displaced.
//
// So restoring that test verbatim would have restored a test that CANNOT
// FAIL FOR THE REASON IT NAMES — the exact sin b41aa2a's author refused
// to commit when they deleted it rather than rewrite it against a
// test-built Grid. Its own text anticipated this and prescribed the
// answer: "either Grid now clamps its fixed tracks (in which case this
// red test has outlived its defect and should go) or the hard minimum is
// computed wrong". Grid clamps. It goes.
//
// # What is asserted instead
//
// The hard minimum still marks a real boundary and still comes from the
// declared tracks; only the failure on the far side of it changed.
// Content is now LOST rather than MISPLACED: below the hard minimum a
// declared fixed track is truncated toward zero, so the status bar is
// simply not drawn. That is worth pinning for the same reason the
// original was — it is what the number means — and it is asserted with
// the same shape, defect then control.
func TestAFixedTrackIsTruncatedBelowTheHardMinimum(t *testing.T) {
	_, shell, _, _ := buildFitPage(t)
	hard, usable, err := minimumFor(shell)
	if err != nil {
		t.Fatal(err)
	}
	if hard.Rows >= usable.Rows {
		t.Fatalf("hard %s is not below usable %s: the two-tier model has collapsed and "+
			"the assertions below cannot tell them apart", hard, usable)
	}

	sized := func(h int) map[string]gooey.Rect {
		shell.Measure(gooey.Size{W: 150, H: h})
		shell.Arrange(gooey.Rect{X: 0, Y: 0, W: 150, H: h})
		out := map[string]gooey.Rect{}
		for _, ch := range shell.ChildComponents() {
			if b, ok := ch.(gooey.Bounded); ok {
				out[fmt.Sprintf("%T", ch)] = b.Bounds()
			}
		}
		return out
	}

	// THE DEFECT: one row below the hard minimum, a fixed track is
	// truncated to nothing. The status bar occupies the last fixed row,
	// so it is the one that goes.
	const bar = "*components.StatusBar"
	short := sized(hard.Rows - 1)
	r, ok := short[bar]
	if !ok {
		t.Fatalf("no %s among the shell's children; this test is looking at the wrong "+
			"component. Children: %v", bar, keysOf(short))
	}
	if r.H != 0 {
		t.Errorf("at %d rows, one below the hard minimum %s, the status bar is %+v — "+
			"it should have been truncated to nothing", hard.Rows-1, hard, r)
	}
	// And nothing is arranged OUTSIDE the grid, which is the clamp's own
	// contract and the thing that replaced the old defect. If this ever
	// fails, clampToExtent has regressed and the old test should come
	// back rather than this one.
	for name, rr := range short {
		if rr.Y+rr.H > hard.Rows-1 {
			t.Errorf("%s is arranged past the bottom at %d rows: %+v — clampToExtent has "+
				"regressed, and the off-screen defect this test replaced is back",
				name, hard.Rows-1, rr)
		}
	}

	// THE CONTROL: at the hard minimum, every fixed track gets its full
	// declared size. Without this the assertion above is satisfied by a
	// shell whose status bar is zero-height at every size.
	full := sized(hard.Rows)
	if r := full[bar]; r.H == 0 {
		t.Errorf("at the hard minimum %s the status bar is still empty: %+v — the "+
			"truncation above proves nothing", hard, r)
	}
	t.Logf("hard %s: status bar %+v at %d rows, %+v at %d",
		hard, short[bar], hard.Rows-1, full[bar], hard.Rows)
}

func keysOf(m map[string]gooey.Rect) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// TestBothMinimumsComeFromTheDeclaredTracks — restored, and this is the
// one the dock genuinely changed.
//
// A hardcoded number here would be a second copy of a fact wysiwyg.gooey
// already states, and the second copy is the one that goes stale. That
// argument is unchanged. What changed is WHICH declarations state it.
//
// The old shell put every pane in a fixed Grid track, so the track list
// was the whole answer. The new shell is `Rows="1,1*,1" Cols="4,1*"` —
// a menu row, a status row, a rail column, and one star track holding
// everything. Its fixed tracks are a true statement about the grid and a
// useless one about the editor: they would report that it fits in 4x2.
//
// So the answer is composed from TWO declared sources, and both are still
// markup: the grid's `Rows=`/`Cols=`, and each `<DockPane>`'s `Slot=` and
// `Size=`. This recomputes both halves independently of fit.go and
// requires the same total, so a change to either declaration moves the
// number and cannot drift from it.
func TestBothMinimumsComeFromTheDeclaredTracks(t *testing.T) {
	ed, shell, dock, _ := buildFitPage(t)
	hard, usable, err := ed.shellMinimum()
	if err != nil {
		t.Fatal(err)
	}

	// Half one: the grid's fixed tracks, recomputed here.
	wantHard, stars := fitSize{}, 0
	for _, d := range shell.Rows {
		if d.Star > 0 {
			stars++
			continue
		}
		wantHard.Rows += d.Fixed
	}
	for _, d := range shell.Cols {
		if d.Star > 0 {
			stars++
			continue
		}
		wantHard.Cols += d.Fixed
	}
	if hard != wantHard {
		t.Errorf("hard %s, want %s from the grid's declared fixed tracks", hard, wantHard)
	}

	// Half two: the dock's own declared sizes, recomputed here from the
	// panes rather than by calling Minimum, so the two derivations have to
	// agree.
	left, right, bottom := 0, 0, 0
	nBottom, upper := 0, 0
	for _, p := range dock.dock.panes {
		switch dockSlot(p.slot.Get()) {
		case dockLeft:
			if v := p.size.Get(); v > left {
				left = v
			}
		case dockRight:
			if v := p.size.Get(); v > right {
				right = v
			}
		case dockBottom:
			nBottom++
			if v := p.size.Get(); v > bottom {
				bottom = v
			}
		}
	}
	for _, s := range []dockSlot{dockLeft, dockCenter, dockRight} {
		if n := len(dock.dock.slotPanes(s)); n*(headerH+starMin) > upper {
			upper = n * (headerH + starMin)
		}
	}
	wantCols := left + right + starMin
	if w := nBottom * starMin; w > wantCols {
		wantCols = w
	}
	wantRows := upper
	if nBottom > 0 {
		if min := nBottom * (headerH + starMin); bottom < min {
			bottom = min
		}
		wantRows += bottom
	}
	want := fitSize{Cols: wantHard.Cols + wantCols, Rows: wantHard.Rows + wantRows}
	if usable != want {
		t.Errorf("usable %s, want %s — the grid's fixed tracks plus the dock's declared "+
			"pane sizes", usable, want)
	}

	// Preconditions, or every assertion here is satisfied by a shell with
	// no star tracks, no fixed ones, and no panes.
	if stars == 0 {
		t.Fatal("the shell declares no star track: the two tiers coincide and nothing " +
			"here distinguishes them")
	}
	if len(dock.dock.panes) == 0 {
		t.Fatal("the dock declares no panes: the composed half of the minimum is zero " +
			"and this test is checking the grid twice")
	}
	if hard == usable {
		t.Error("the two minimums coincide, so nothing here distinguishes them")
	}
	if usable.Rows < 10 || usable.Cols < 40 {
		t.Errorf("usable minimum %s is implausibly small: the check would never trigger "+
			"and would prove nothing", usable)
	}
	t.Logf("shipped page: hard %s, usable %s (grid fixed %s + dock %s)",
		hard, usable, wantHard, dock.dock.Minimum())
}

// TestOnlyOneRootIsVisibleAtATime — restored.
//
// The swap is two bound Visibilities over ONE fact, and the failure mode
// of two independent sources is a frame showing both roots or neither.
//
// NOTE ON WHAT THIS CANNOT ASSERT YET: the shipped page binds neither
// {{.Fits}} nor {{.Cramped}} to anything, so there is no cramped-message
// root in the tree to swap TO. That is the remaining half of #355's
// user-visible story and it is a page change, not a fit.go change. What
// is asserted here is the part that exists: the two properties are one
// fact and move together, so whatever binds them cannot show both.
func TestOnlyOneRootIsVisibleAtATime(t *testing.T) {
	ed, _ := buildPage(t)
	for _, fits := range []bool{true, false, true} {
		ed.fits.Set(fits)
		if ed.cramped.Get() == fits {
			t.Errorf("fits=%v and cramped=%v: the two agree, so a frame can show both "+
				"roots or neither", fits, ed.cramped.Get())
		}
	}
	// And cramped must be a COMPUTED, not a second source. Two sources
	// for one fact drift, and the frame where they disagree is the bug.
	if ed.cramped.Settable() {
		t.Error("cramped is a source property: it can be written independently of fits, " +
			"which is exactly the drift the computed exists to prevent")
	}
}

// TestTheCrampedMessageSaysBothSizes — restored to reading the SHIPPED
// page's numbers rather than two invented ones. "Too small" without the
// numbers leaves the user guessing how far to drag.
func TestTheCrampedMessageSaysBothSizes(t *testing.T) {
	ed, _, _, _ := buildFitPage(t)
	_, usable, err := ed.shellMinimum()
	if err != nil {
		t.Fatal(err)
	}
	have := fitSize{Cols: usable.Cols - 6, Rows: usable.Rows - 2}
	ed.checkFit(have.Cols, have.Rows, true)

	msg := ed.fitMsg.Get()
	if msg == "" {
		t.Fatal("no message at a size below the minimum")
	}
	for _, want := range []int{have.Cols, have.Rows, usable.Cols, usable.Rows} {
		if !strings.Contains(msg, itoa(want)) {
			t.Errorf("the message must name %d; got:\n%s", want, msg)
		}
	}
}

// TestTheFitsGuardHoldsWithNoConsumer is the pin a damage count cannot
// provide, and finding that out is worth the extra test.
//
// checkFit guards both of its writes because prop.Set does not compare
// values and this runs before EVERY frame. A mutation removing the guard
// on ed.fits.Set SURVIVED the damage test next door — and correctly so:
// nothing in the shipped page binds {{.Fits}} or {{.Cramped}} yet, so an
// unguarded Set invalidates zero dependents and repaints nothing. The
// damage instrument is blind to it, not because the guard does not
// matter, but because its consumer has not been built.
//
// That is exactly the situation where a guard rots: correct, load-bearing
// the moment somebody binds the property, and untested until then. So it
// is pinned at the property level instead — a computed over ed.fits
// counts its own evaluations, and a guarded Set produces none.
//
// (The missing consumer is the remaining half of #355's user-visible
// story: FitMsg has two live bindings and Fits has none, so the cramped
// swap has nothing to swap. See TestOnlyOneRootIsVisibleAtATime.)
func TestTheFitsGuardHoldsWithNoConsumer(t *testing.T) {
	ed, _, _, _ := buildFitPage(t)
	_, usable, err := ed.shellMinimum()
	if err != nil {
		t.Fatal(err)
	}

	evals := 0
	obs := prop.NewComputed(func() int {
		evals++
		ed.fits.Get()
		return evals
	})
	obs.Get() // prime: the dependency is recorded by the Get that RUNS

	// Steady state at a size that fits: repeated checks must not write.
	settled := evals
	for i := 0; i < 3; i++ {
		ed.checkFit(usable.Cols, usable.Rows, true)
		obs.Get()
	}
	if evals != settled {
		t.Errorf("a fit check at an unchanged size re-evaluated the observer %d times; "+
			"ed.fits.Set is firing on every frame, and prop.Set does not compare values",
			evals-settled)
	}

	// The discrimination: a real change MUST invalidate, or the loop above
	// is satisfied by a checkFit that never writes anything at all.
	ed.checkFit(usable.Cols-1, usable.Rows-1, true)
	obs.Get()
	if evals == settled {
		t.Fatal("dropping below the minimum did not invalidate the observer: this test " +
			"cannot tell a guarded write from an absent one")
	}

	// And the new state is steady too.
	settled = evals
	for i := 0; i < 3; i++ {
		ed.checkFit(usable.Cols-1, usable.Rows-1, true)
		obs.Get()
	}
	if evals != settled {
		t.Errorf("repeated checks at the same cramped size re-evaluated %d times", evals-settled)
	}
}

// TestTheFitCheckIsFreeWhenNothingChanges is the damage pin.
//
// checkFit runs BEFORE EVERY FRAME, and prop.Set does not compare values
// — so a single unguarded write here invalidates every dependent on every
// frame and turns a cheap size check into a permanent full repaint. A
// bounds or cell assertion cannot see that; only a painted count can.
func TestTheFitCheckIsFreeWhenNothingChanges(t *testing.T) {
	ed, _, _, root := buildFitPage(t)
	c := gooey.NewComposer(root, 150, 44)
	c.Frame()
	settle(t, c)

	// A size that fits: the steady state, and the one that runs every
	// frame in a normal session.
	for i := 0; i < 3; i++ {
		ed.checkFit(150, 44, true)
		if _, painted := c.Frame(); painted != 0 {
			t.Fatalf("a fit check at an unchanged size repainted %d components; "+
				"prop.Set does not compare values, so an unguarded write on a "+
				"before-every-frame hook is a permanent full repaint", painted)
		}
	}

	// A size that does not: the first check must cost something, or the
	// loop above is measuring a check that does nothing at all.
	ed.checkFit(20, 6, true)
	if _, painted := c.Frame(); painted == 0 {
		t.Fatal("dropping below the minimum repainted nothing: the assertions above " +
			"would be satisfied by a check that never writes anything")
	}
	// And repeating it is free again.
	for i := 0; i < 3; i++ {
		ed.checkFit(20, 6, true)
		if _, painted := c.Frame(); painted != 0 {
			t.Fatalf("a repeated fit check at the same cramped size repainted %d", painted)
		}
	}
}

// TestAnAutoTrackIsRefusedRatherThanGuessed. Every way of guessing an
// Auto track's extent makes the minimum too SMALL, which means the fit
// check passes at a size that does not fit — the silent misfit this
// whole mechanism removes, reintroduced by the fix.
func TestAnAutoTrackIsRefusedRatherThanGuessed(t *testing.T) {
	g := &components.Grid{
		Rows: []components.GridLen{components.Fixed(1), components.Auto()},
		Cols: []components.GridLen{components.Star(1)},
	}
	if _, _, err := minimumFor(g); err == nil {
		t.Fatal("an Auto track must be refused, not guessed")
	} else if !strings.Contains(err.Error(), "Auto") {
		t.Errorf("the error must name the problem, got: %v", err)
	}
	// Control: without the Auto track the same shape computes fine.
	g.Rows = []components.GridLen{components.Fixed(1), components.Star(1)}
	if _, _, err := minimumFor(g); err != nil {
		t.Fatalf("the control failed too, so the refusal proved nothing: %v", err)
	}
}

// TestTheTwoMinimumsDifferByTheStarTracks keeps the distinction as a
// property of trackMinimum rather than of any particular page: a star
// track contributes NOTHING to the hard minimum — it absorbs the
// shortfall down to zero — and starMin to the usable one.
//
// This is the part that was written wrong first, with one number and the
// other one's mechanism.
func TestTheTwoMinimumsDifferByTheStarTracks(t *testing.T) {
	g := &components.Grid{
		Rows: []components.GridLen{components.Fixed(1), components.Star(1), components.Fixed(12)},
		Cols: []components.GridLen{components.Star(2), components.Fixed(46)},
	}
	hard, usable, err := minimumFor(g)
	if err != nil {
		t.Fatal(err)
	}
	if hard.Rows != 13 || hard.Cols != 46 {
		t.Errorf("hard = %s, want 46×13: the fixed tracks alone, star tracks contributing nothing", hard)
	}
	if usable.Rows != 13+starMin || usable.Cols != 46+starMin {
		t.Errorf("usable = %s, want the fixed tracks plus %d per star track", usable, starMin)
	}
	if hard == usable {
		t.Error("the two minimums coincide, so nothing here distinguishes them")
	}
}

// TestTheDockContributesItsDeclaredSizes — the dock half on its own, so a
// failure says which of the two composed sources moved.
func TestTheDockContributesItsDeclaredSizes(t *testing.T) {
	_, _, dock, _ := buildFitPage(t)
	before := dock.dock.Minimum()
	if before.Cols <= 0 || before.Rows <= 0 {
		t.Fatalf("the shipped dock's minimum is %s; nothing below discriminates", before)
	}
	// Widening a pane must widen the minimum. This is the property that
	// makes Size= a DECLARATION rather than decoration.
	p := dock.dock.ByID("properties")
	if p == nil {
		t.Fatal("no properties pane in the shipped dock")
	}
	p.size.Set(p.size.Get() + 10)
	after := dock.dock.Minimum()
	if after.Cols != before.Cols+10 {
		t.Errorf("widening a right-slot pane by 10 moved the minimum from %s to %s; the "+
			"declared size is not reaching the minimum", before, after)
	}
	// An empty dock asks for nothing, rather than for a floor nobody declared.
	empty := newDockModel()
	if got := empty.Minimum(); got != (fitSize{}) {
		t.Errorf("an empty dock's minimum is %s, want zero", got)
	}
}

package main

import (
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/prop"
)

// The four assertions fit_test.go's header records as owed by "the new
// shell", plus the one whose absence let them stay owed.
//
// THE GAP WAS NOT A WRONG NUMBER, IT WAS ZERO EXECUTIONS. watchFit looked
// up Name="Shell"; the page declares Name="Page", so the lookup failed on
// every frame and the early return fired forever (#355). Nothing was red,
// because nothing asserted that the machinery ran at all — every test
// here reads the SHIPPED page, so the numbers come from the markup and
// cannot drift, but a test of the numbers alone would have gone on
// passing while the watcher was dead.

// shellGrid is the page's root Grid, resolved the same way watchFit
// resolves it. If this cannot find it, watchFit cannot either.
func shellGrid(t *testing.T) (*editor, *components.Grid) {
	t.Helper()
	ed, _ := buildPage(t)
	g, err := markup.Find[*components.Grid](ed.ctx, ShellName)
	if err != nil {
		t.Fatalf("the shipped page has no <Grid Name=%q>: %v\n\n"+
			"watchFit resolves this exact name, so a page that does not declare "+
			"it is a fit watcher that never runs — which is #355, and it was "+
			"invisible for exactly as long as nothing asserted this", ShellName, err)
	}
	return ed, g
}

// ONE: the name watchFit looks up is a name the shipped page declares.
// This is the whole of #355 and it is deliberately its own test, so the
// failure says "the page does not declare this" rather than surfacing as
// a wrong number three tests down.
func TestTheFitWatcherResolvesTheShippedShell(t *testing.T) {
	_, g := shellGrid(t)
	if len(g.Rows) == 0 || len(g.Cols) == 0 {
		t.Fatalf("the shell Grid declares %d rows and %d cols; a track-less "+
			"shell makes every minimum below vacuous", len(g.Rows), len(g.Cols))
	}
}

// TWO: both minimums come from the live track definitions, so editing
// Rows= or Cols= in the markup moves them and they cannot drift.
func TestBothMinimumsComeFromTheShippedTracks(t *testing.T) {
	_, g := shellGrid(t)
	hard, usable, err := minimumFor(g)
	if err != nil {
		t.Fatalf("the shipped shell's own tracks are refused: %v", err)
	}
	// Derived here the same way fit.go derives them, from the same Grid —
	// this asserts correspondence, not a copied constant.
	var wantHardC, wantUsableC, wantHardR, wantUsableR int
	for _, d := range g.Cols {
		if d.Star > 0 {
			wantUsableC += starMin
			continue
		}
		wantHardC += d.Fixed
		wantUsableC += d.Fixed
	}
	for _, d := range g.Rows {
		if d.Star > 0 {
			wantUsableR += starMin
			continue
		}
		wantHardR += d.Fixed
		wantUsableR += d.Fixed
	}
	if hard != (fitSize{wantHardC, wantHardR}) {
		t.Errorf("hard minimum %s, want %s", hard, fitSize{wantHardC, wantHardR})
	}
	if usable != (fitSize{wantUsableC, wantUsableR}) {
		t.Errorf("usable minimum %s, want %s", usable, fitSize{wantUsableC, wantUsableR})
	}
	// And the two are ordered, which is the distinction the file exists to
	// keep: a star track absorbs the shortfall down to zero, so usable is
	// strictly larger wherever there is one.
	if usable.Cols < hard.Cols || usable.Rows < hard.Rows {
		t.Errorf("usable %s is smaller than hard %s on some axis; the two "+
			"minimums have been conflated", usable, hard)
	}
}

// THREE: the cramped message names BOTH sizes — the one the user has and
// the one they need. "Too small" without the numbers leaves them guessing
// how far to drag.
func TestTheCrampedMessageCarriesBothShippedSizes(t *testing.T) {
	_, g := shellGrid(t)
	_, usable, err := minimumFor(g)
	if err != nil {
		t.Fatal(err)
	}
	have := fitSize{usable.Cols - 1, usable.Rows - 1}
	msg := cramMsg(have, usable)
	for _, want := range []string{have.String(), usable.String()} {
		if !strings.Contains(msg, want) {
			t.Errorf("the cramped message does not contain %q: %q", want, msg)
		}
	}
}

// FOUR: the swap fires at the USABLE minimum, and it fires against the
// real page rather than a Grid built here — which is the assertion that
// the machinery RUNS. One cell under on either axis flips it; exactly at
// the minimum it fits.
func TestTheFitWatcherRunsAgainstTheShippedPage(t *testing.T) {
	ed, g := shellGrid(t)
	_, usable, err := minimumFor(g)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		cols, rows int
		want       bool
		why        string
	}{
		{usable.Cols, usable.Rows, true, "exactly the usable minimum fits"},
		{usable.Cols - 1, usable.Rows, false, "one column short"},
		{usable.Cols, usable.Rows - 1, false, "one row short"},
		{usable.Cols + 20, usable.Rows + 20, true, "comfortably larger"},
	} {
		got, _, err := ed.fitAt(c.cols, c.rows)
		if err != nil {
			t.Fatalf("at %dx%d the fit check errored: %v", c.cols, c.rows, err)
		}
		if got != c.want {
			t.Errorf("at %dx%d fits=%v, want %v — %s (usable minimum is %s)",
				c.cols, c.rows, got, c.want, c.why, usable)
		}
	}

	// AND THE PUBLISHED STATE FOLLOWS, which is the half that was dead:
	// fitAt could be perfect while nothing ever called it. applyFit is
	// what watchFit's BeforeFrame hook runs.
	ed.applyFit(usable.Cols-1, usable.Rows-1)
	if ed.fits.Get() {
		t.Error("one cell under the minimum, and Fits is still true — the " +
			"watcher body did not publish")
	}
	if ed.fitMsg.Get() == "" {
		t.Error("FitMsg is empty at a size that does not fit. It is bound twice " +
			"in the PROBLEMS pane, so a pane the user can open says nothing")
	}
	ed.applyFit(usable.Cols, usable.Rows)
	if !ed.fits.Get() {
		t.Error("back at the minimum, Fits did not return to true")
	}
}

// The damage contract. prop.Set does not compare values, so a per-frame
// size check that Set unconditionally would invalidate every dependent on
// every frame — a permanent full repaint bought with a comparison nobody
// notices is missing. Both Sets in applyFit are guarded; this is the pin.
//
// THE INSTRUMENT IS A COMPUTED DEPENDENT, NOT THE SOURCE ITSELF, and the
// first version of this test got that wrong in a way that could not fail.
// prop.Set invalidates a source's DEPENDENTS (prop/prop.go:117) and never
// marks the source's own node dirty, and OnInvalidate fires only from
// node.invalidate() — so OnInvalidate on a source property never fires at
// all, and a counter hung on ed.fits reads 0 whether the guard is there
// or not. Both arms were vacuous.
//
// node.invalidate() also latches on n.dirty, so the watcher has to be
// READ between writes; a dependent that is already dirty absorbs the
// next invalidation silently.
func TestASettledSizeCostsNoInvalidation(t *testing.T) {
	ed, g := shellGrid(t)
	_, usable, err := minimumFor(g)
	if err != nil {
		t.Fatal(err)
	}
	// Settle at a size that does NOT fit, so both properties have been
	// written once and the second pass has something to be idempotent
	// about.
	small := fitSize{usable.Cols - 1, usable.Rows - 1}
	ed.applyFit(small.Cols, small.Rows)

	fitsSeen, msgSeen := prop.NewComputed(func() bool { return ed.fits.Get() }),
		prop.NewComputed(func() string { return ed.fitMsg.Get() })
	settle := func() { fitsSeen.Get(); msgSeen.Get() }
	settle() // records the dependency AND leaves both clean

	fitsWrites, msgWrites := 0, 0
	fitsSeen.OnInvalidate(func() { fitsWrites++ })
	msgSeen.OnInvalidate(func() { msgWrites++ })

	for range 5 {
		ed.applyFit(small.Cols, small.Rows)
		settle()
	}
	if fitsWrites != 0 || msgWrites != 0 {
		t.Errorf("five frames at an unchanged size invalidated fits %d times and "+
			"fitMsg %d times, want 0 each — prop.Set does not compare values, so "+
			"an unguarded Set here is a permanent full repaint",
			fitsWrites, msgWrites)
	}

	// And the guard is not simply "never write": a real change still gets
	// through. A guard that only says no is untested, and this arm is what
	// makes the zero above mean something.
	ed.applyFit(usable.Cols+10, usable.Rows+10)
	settle()
	if fitsWrites != 1 {
		t.Errorf("growing past the minimum invalidated fits %d times, want exactly "+
			"1 — the guard is refusing a write it should let through, or the "+
			"instrument is not observing one", fitsWrites)
	}
}

// An unresolvable shell is a PROGRAMMING ERROR, and it says so where the
// user is already looking rather than returning silently.
//
// The silent return is what made #355 invisible for the whole life of the
// feature, and its comment — "not built yet; the first frame settles it"
// — was not merely stale but never true: App.Run builds the content
// (app.go:473) before it runs any BeforeFrame hook (app.go:614), so there
// is no frame in which the page is unbuilt. There was no transient to
// protect.
//
// Deliberately NOT gooey.Exit, which is os.Exit: called from a frame hook
// it skips App.Run's deferred teardown and leaves the terminal in raw
// mode with no echo — trading a silent misfit for an unusable shell.
func TestAnUnresolvableShellIsReportedNotSwallowed(t *testing.T) {
	// An editor whose page was never built: ctx.Named is empty, so the
	// lookup fails exactly as it did against the real page under the bug.
	ed := newEditor(editorFS())
	before := ed.status.Get()

	ed.applyFit(200, 60)

	got := ed.status.Get()
	if got == before {
		t.Fatalf("the status is unchanged at %q after the shell failed to "+
			"resolve — this is the silent return that hid #355 for the "+
			"feature's entire life", got)
	}
	// And it names what could not be found, so the reader can act on it.
	if !strings.Contains(got, ShellName) {
		t.Errorf("the message %q does not name %q; an error that does not say "+
			"which element is missing sends the reader looking", got, ShellName)
	}
}

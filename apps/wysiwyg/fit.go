package main

import (
	"fmt"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/markup"
)

// The editor has an undeclared minimum size, and it used to fail at that
// size in the worst available way: silently, off-screen.
//
// # There are TWO minimums, and conflating them is how this got written
// # wrong the first time
//
// The shell is a <Grid Rows="1,1*,12,1" Cols="2*,46">.
//
// The HARD minimum is the sum of the FIXED tracks — 14 rows here.
// Grid.Arrange sizes star tracks with distributeStars
// (components/grid.go), which hands them max(0, extent-used); once the
// fixed tracks alone exceed the extent the star track goes to zero and
// the fixed tracks keep their full declared size. offsets() then
// accumulates them unclamped and the tail is arranged past b.Y+b.H.
// Measured, not reasoned: at 49x13 the StatusBar and the help Text are
// both arranged at Y=13 on a 13-row screen, with no error anywhere.
//
// The USABLE minimum adds starMin per star track — 17 rows. Between the
// two nothing overflows; the preview and palette simply have a <Border>
// with no interior left to draw in.
//
// The first version of this file had only one number, the usable one,
// and described it with the hard one's mechanism. The red test skipped
// (nothing overflowed one row below 17) which is what caught it — a skip
// is not a pass, and reading it as one would have shipped a confident
// wrong explanation next to working code.
//
// # Why the numbers are derived, not written down
//
// A constant here would be a second copy of a fact the markup already
// states, and the second copy is the one that goes stale — the argument
// that put the element vocabulary beside the code that reads it. Both
// minimums come from the live <Grid>'s own track definitions, so editing
// Rows= or Cols= moves them and they cannot drift.
//
// The only judgement is starMin.

// starMin is the smallest useful extent for a star track. A <Border>
// spends two cells per axis on its own frame, leaving one for content;
// below that the pane is chrome with nothing in it.
const starMin = 3

// fitSize is a terminal size the shell needs.
type fitSize struct{ Cols, Rows int }

func (f fitSize) String() string { return fmt.Sprintf("%d\u00d7%d", f.Cols, f.Rows) }

// minimumFor returns both minimums: hard, below which the shell is laid
// out off the screen, and usable, below which it fits but has no room
// left for the panes the star tracks hold.
//
// An AUTO track is a hard error rather than a guess. Its extent is
// whatever its content measured, which lives in the Grid's unexported
// measure cache — so this cannot compute it, and every way of guessing
// makes the minimum too SMALL, which means the check passes at a size
// that does not fit. That is the exact silent misfit this file removes,
// reintroduced by the fix. The shell has no Auto track today; the day one
// appears, this says so instead of quietly being wrong.
func minimumFor(g *components.Grid) (hard, usable fitSize, err error) {
	hc, uc, err := trackMinimum("Cols", g.Cols)
	if err != nil {
		return fitSize{}, fitSize{}, err
	}
	hr, ur, err := trackMinimum("Rows", g.Rows)
	if err != nil {
		return fitSize{}, fitSize{}, err
	}
	return fitSize{hc, hr}, fitSize{uc, ur}, nil
}

func trackMinimum(axis string, defs []components.GridLen) (hard, usable int, err error) {
	for i, d := range defs {
		switch {
		case d.Auto:
			return 0, 0, fmt.Errorf(
				"%s track %d is Auto: its extent is the measure cache, which this "+
					"cannot read, and every guess makes the minimum too small — "+
					"which is the silent misfit the check exists to catch", axis, i)
		case d.Star > 0:
			// A star track contributes nothing to the hard minimum: it is
			// what absorbs the shortfall, all the way down to zero.
			usable += starMin
		default:
			hard += d.Fixed
			usable += d.Fixed
		}
	}
	return hard, usable, nil
}

// ShellName and DockName are what the fit check looks up, and they are
// declared here so there is exactly ONE spelling of each. The bug this
// pair replaces (issue #355) was a lookup for "Shell" against a page that
// declares "Page": a string typed twice in two files, agreeing with
// nothing.
//
// TestTheShellNamesResolveInTheShippedPage is what stops that recurring —
// it reads these constants and requires the shipped markup to declare
// both, so a rename that misses one goes red instead of quiet.
const (
	ShellName = "Page"
	DockName  = "Dock"
)

// shellMinimum is the editor's minimum terminal size, composed from the
// two things that declare it: the shell grid's fixed tracks and the
// dock's own panes.
//
// hard is the grid's alone and keeps its original meaning — the size
// below which Grid.offsets accumulates its fixed tracks unclamped and
// arranges children off the bottom of the screen. usable adds the dock's
// requirement in place of the generic starMin allowance the star track
// would otherwise get, because that star track holds the whole editor.
// See dockModel.Minimum for why a grid track list can no longer answer
// this on its own.
func (ed *editor) shellMinimum() (hard, usable fitSize, err error) {
	shell, err := markup.Find[*components.Grid](ed.ctx, ShellName)
	if err != nil {
		return fitSize{}, fitSize{}, fmt.Errorf("no <Grid Name=%q> in the page: %w", ShellName, err)
	}
	hard, usable, err = minimumFor(shell)
	if err != nil {
		return fitSize{}, fitSize{}, err
	}
	dock, err := markup.Find[*dockHost](ed.ctx, DockName)
	if err != nil {
		return fitSize{}, fitSize{}, fmt.Errorf("no <DockHost Name=%q> in the page: %w", DockName, err)
	}
	d := dock.dock.Minimum()
	// hard IS the sum of the fixed tracks, so this is "the fixed tracks
	// plus what the dock needs" without recomputing either.
	usable = fitSize{Cols: hard.Cols + d.Cols, Rows: hard.Rows + d.Rows}
	return hard, usable, nil
}

// watchFit swaps the shell for a legible message whenever the terminal is
// too small to lay the shell out, and back when it grows again.
//
// It runs from BeforeFrame — UI goroutine, before layout — because
// App.Size() is only updated by resized() on a SIGWINCH, so there is no
// property to observe and a per-frame read is the honest way to notice.
func (ed *editor) watchFit(app *gooey.App) {
	app.BeforeFrame(func() {
		cols, rows := app.Size()
		// composed says a frame has already been painted, which is the
		// only thing that distinguishes "the page is not built yet" from
		// "the page will never contain that name". See checkFit.
		ed.checkFit(cols, rows, app.Frames() > 0)
	})
}

// checkFit is the per-frame body, split out from watchFit because THE
// DEFECT THIS FIXES WAS ZERO EXECUTIONS AND NOTHING COULD SEE IT.
//
// watchFit takes a *gooey.App, `go test` has no terminal, and so no test
// could ever run the thing that mattered — the entire body sat behind an
// App the suite could not construct. Every fit test asserted the
// arithmetic instead, and the arithmetic was right; what was broken was
// that nobody called it. Taking (cols, rows) makes the body reachable, so
// "does this run at all against the real page" becomes a test rather than
// a hope.
//
// # Failing loudly
//
// An unresolvable shell name is a PROGRAMMING ERROR, not a frame-1
// condition, and the previous version could not tell those apart:
//
//	shell, err := markup.Find[*components.Grid](ed.ctx, "Shell")
//	if err != nil {
//	    return // not built yet; the first frame settles it
//	}
//
// That comment is true of frame 1 and false of every frame after it. The
// page never declared "Shell", so the return fired forever, and the
// sentence explaining it away is the whole reason nobody looked — the
// same shape as issue #207's stale known-bad list, where the prose spends
// the attention that would have found the bug.
//
// So the two cases are now DISTINGUISHED BY A FACT rather than by a
// hopeful comment: before the first frame is composed a miss is a
// transient and is ignored; after one, it is reported, in the PROBLEMS
// pane the user can actually open, and it keeps saying so. There is no
// wording available to this branch that could disguise a permanent
// failure as a startup one, because the branch no longer decides — the
// frame count does.
//
// # The Sets stay guarded
//
// prop.Set does not compare values, so an unguarded write here — on a
// hook that runs BEFORE EVERY FRAME — would invalidate every dependent on
// every frame and turn a cheap size check into a permanent full repaint.
// TestTheFitCheckIsFreeWhenNothingChanges pins that with a damage count,
// which is the only instrument that can see it.
func (ed *editor) checkFit(cols, rows int, composed bool) {
	_, usable, err := ed.shellMinimum()
	if err != nil {
		if !composed {
			return // genuinely not built yet — the first frame settles it
		}
		ed.sayFitProblem("✗ the editor cannot find its own shell: " + err.Error())
		return
	}
	// The swap fires at the USABLE minimum, not the hard one. Between
	// them the layout is technically valid and shows two empty borders,
	// which is not a thing worth showing anybody.
	fits := cols >= usable.Cols && rows >= usable.Rows
	if fits != ed.fits.Get() {
		ed.fits.Set(fits)
	}
	if fits {
		// Clearing matters as much as setting: the PROBLEMS pane binds
		// FitMsg, and a stale complaint about a size the terminal no
		// longer is would be worse than saying nothing.
		if ed.fitMsg.Get() != "" {
			ed.fitMsg.Set("")
		}
		return
	}
	if want := cramMsg(fitSize{cols, rows}, usable); want != ed.fitMsg.Get() {
		ed.fitMsg.Set(want)
	}
}

// sayFitProblem puts a diagnostic where the user is already looking: the
// status bar AND the PROBLEMS pane, which binds FitMsg twice and until
// now could never say anything at all.
//
// Both writes are guarded, for the same reason every other write on this
// path is — this runs before every frame.
func (ed *editor) sayFitProblem(msg string) {
	if ed.status.Get() != msg {
		ed.status.Set(msg)
	}
	if ed.fitMsg.Get() != msg {
		ed.fitMsg.Set(msg)
	}
}

// cramMsg is what the user sees instead of a broken layout. Both sizes,
// because "too small" without the numbers leaves them guessing how far to
// drag.
func cramMsg(have, want fitSize) string {
	return fmt.Sprintf(
		"This terminal is %s.\nThe editor needs %s.\n\nResize, or press q to quit.",
		have, want)
}

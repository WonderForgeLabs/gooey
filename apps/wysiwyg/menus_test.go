package main

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/apps/wysiwyg/components/panel"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

func theMenuBar(t *testing.T, ed *editor) *components.MenuBar {
	t.Helper()
	bar, err := markup.Find[*components.MenuBar](ed.ctx, "Menus")
	if err != nil {
		t.Fatalf("the shell has no <MenuBar Name=\"Menus\">: %v", err)
	}
	return bar
}

// shellRegion resolves a named <Panel> region out of the built page.
// Named for the region rather than for the lookup, because seedpalette
// already owns findNamed for a different question — which component the
// palette just added.
func shellRegion(t *testing.T, ed *editor, name string) *panel.Pane {
	t.Helper()
	p, err := markup.Find[*panel.Pane](ed.ctx, name)
	if err != nil {
		t.Fatalf("no <Panel Name=%q> in the shell: %v", name, err)
	}
	return p
}

func menuNamed(t *testing.T, bar *components.MenuBar, title string) (int, components.Menu) {
	t.Helper()
	for i, m := range bar.Menus {
		if strings.ReplaceAll(m.Title, "_", "") == title {
			return i, m
		}
	}
	t.Fatalf("no %q menu; the bar has %d menus", title, len(bar.Menus))
	return 0, components.Menu{}
}

// TestTheCheckAndTheAcceleratorAreOneState is the requirement, asserted
// as a STRUCTURAL fact rather than a behavioural coincidence.
//
// A behavioural test — press the key, look at the box — passes against an
// implementation with two bools that the commands happen to keep in step
// today. What makes the claim durable is that the checks are COMPUTEDS
// over one source: a computed is the read-only projection, so there is no
// way to write one, and therefore no way for the two to disagree.
func TestTheCheckAndTheAcceleratorAreOneState(t *testing.T) {
	ed, _ := buildPage(t)

	for name, p := range map[string]*prop.Property[bool]{
		"BuiltinChecked": ed.builtinChecked,
		"EditorChecked":  ed.editorChecked,
		"DesignChecked":  ed.designChecked,
		"CodeChecked":    ed.codeChecked,
	} {
		if p == nil {
			t.Fatalf("%s is nil", name)
		}
		if p.Settable() {
			t.Errorf("%s is a SOURCE property: a check that can be written directly is a "+
				"second copy of the state it displays, and the two will disagree", name)
		}
	}

	// The radio property: exactly one of the pair, always, for every
	// value the state can take — including one it should never take.
	for _, which := range []int{codeBuiltin, codeExternal, 99} {
		ed.codeView.Set(which)
		b, e := ed.builtinChecked.Get(), ed.editorChecked.Get()
		if which == 99 {
			if b || e {
				t.Errorf("codeView=%d checked something; an out-of-range state must check "+
					"neither rather than default one on", which)
			}
			continue
		}
		if b == e {
			t.Errorf("codeView=%d has builtin=%v editor=%v; exactly one must be checked",
				which, b, e)
		}
	}
}

// TestTheAcceleratorAndTheMenuItemNameOneBinding — the identity claim,
// asserted where identity is actually DECIDED.
//
// Comparing the two gooey.Action values directly is not available and
// should not be faked: gooey.Command is a func type, so `==` on it panics
// with "comparing uncomparable type", and the pointer tricks that get
// around that compare CODE addresses — two evaluations of one literal
// share an address, so the check would pass exactly when it should fail.
//
// The real question is upstream of the values anyway. markup resolves
// Command="{{.Name}}" through ctx.Command(name), which is a lookup in one
// map, so two sites naming the same binding get the same value BY
// CONSTRUCTION. What can drift is the NAMES — a key wired to UseBuiltin
// and an item wired to something else — and that is a fact about the
// page, checkable exactly.
//
// The behavioural arm is the discrimination half: a binding that resolves
// to an action which does not move the state would satisfy the name check
// and still be wrong.
func TestTheAcceleratorAndTheMenuItemNameOneBinding(t *testing.T) {
	ed, _ := buildPage(t)
	src, err := os.ReadFile("wysiwyg.gooey")
	if err != nil {
		t.Fatal(err)
	}
	page := string(src)

	bound := map[string]bool{}
	for _, m := range keyBindingRe.FindAllStringSubmatch(page, -1) {
		bound[m[1]] = true
	}

	bar := theMenuBar(t, ed)
	_, view := menuNamed(t, bar, "View")

	// text -> the binding both the item and the key must name.
	for text, binding := range map[string]string{
		"Built in": "UseBuiltin",
		"Design":   "ShowDesign",
	} {
		var item components.MenuItem
		found := false
		for _, it := range view.Items {
			if strings.ReplaceAll(it.Text, "_", "") == text {
				item, found = it, true
			}
		}
		if !found || item.Action == nil {
			t.Fatalf("the View menu has no %q item with an action", text)
		}
		if !strings.Contains(page, `Command="{{.`+binding+`}}"`) {
			t.Errorf("the page never names {{.%s}}", binding)
		}
		if !bound[binding] && binding != "ShowDesign" {
			t.Errorf("{{.%s}} has no KeyBinding; the accelerator and the item cannot be "+
				"one action if only one of them exists", binding)
		}
	}

	// And the item really moves the ONE state, so a correctly-named
	// binding pointing at an inert action still fails.
	ed.codeView.Set(codeExternal)
	for _, it := range view.Items {
		if strings.ReplaceAll(it.Text, "_", "") == "Built in" {
			it.Action.Run()
		}
	}
	if ed.codeView.Get() != codeBuiltin {
		t.Error(`activating the "Built in" item did not move codeView; the item is wired ` +
			"to something that is not the state its check displays")
	}
	if !ed.builtinChecked.Get() || ed.editorChecked.Get() {
		t.Error("after activating the item the two checks disagree with the state")
	}
}

// TestTogglingTheViewerRepaintsOnlyTheOpenDropdown is the damage pin for
// the check state. The box is read inside MenuBar.drawDropdown, which
// runs inside the dropdown's own paint node, so flipping the state must
// cost the dropdown and nothing else.
func TestTogglingTheViewerRepaintsOnlyTheOpenDropdown(t *testing.T) {
	ed, root := buildPage(t)
	c := gooey.NewComposer(root, 150, 44)
	c.Frame()
	settle(t, c)

	bar := theMenuBar(t, ed)
	i, _ := menuNamed(t, bar, "View")
	bar.Open(i, nil)
	settle(t, c)
	if !bar.IsOpen() {
		t.Fatal("the View menu did not open; every count below would measure nothing")
	}

	ed.codeView.Set(codeExternal)
	_, painted := c.Frame()
	if painted == 0 {
		t.Fatal("flipping the code viewer with the menu OPEN repainted nothing: the check " +
			"box on screen is now stale, which means the box is not read while painting")
	}
	t.Logf("check flip with the dropdown open repainted %d", painted)
	if painted != 1 {
		t.Errorf("flipping the check repainted %d components, want 1 (the dropdown); "+
			"damage %v", painted, c.Damage())
	}

	// And with the menu CLOSED it costs nothing at all, because nothing on
	// screen is showing the box.
	bar.Dismiss()
	settle(t, c)
	ed.codeView.Set(codeBuiltin)
	_, closed := c.Frame()
	if closed > 1 {
		t.Errorf("flipping the check with the menu closed repainted %d; the box is not on "+
			"screen, so nothing should be reading it", closed)
	}
}

// TestTheCheckBoxIsDrawn — the state has to be VISIBLE, in the cell
// plane, or a pty transcript can never show it.
//
// AND THE ASSERTIONS ARE PER ROW. They were a Contains over nineteen
// joined rows, which cannot say WHICH row carries the box — a menu that
// drew the item twice, or put the box on its neighbour, passed. The
// negative arm was worse than imprecise: it looked for "  $EDITOR" in a
// blob where the only line holding "$EDITOR" begins "│[ ] ", so no
// rendering of this menu could have produced it and the arm could not
// fail. What replaces it reads the four cells in front of the label,
// which tells "[x] " (wrong state), "[ ] " (right) and "    " (no box at
// all) apart by their text instead of by their absence.
func TestTheCheckBoxIsDrawn(t *testing.T) {
	dropdown := viewMenuRows(t, codeBuiltin)

	// THE EXPECTED BOX IS SPELLED ONCE, here and in every assertion
	// below it. Spelling it again inside the message lets the two
	// drift: with the comparison mutated to "[x] " one run printed
	// `carries "[x] ", want "[x] "` — a tautology, because the literal
	// in the message no longer came from the check. Raised in review of
	// #502.
	got, row := boxBefore(t, dropdown, "Built in")
	if want := "[x] "; got != want {
		t.Errorf("the \"Built in\" row carries %q in front of its label, want a "+
			"checked %q; the row reads %q", got, want, row)
	}
	// The unchecked box must be a real box, not blank: "[ ]" and nothing
	// at all read very differently to a user deciding which is selected.
	got, row = boxBefore(t, dropdown, "$EDITOR")
	if want := "[ ] "; got != want {
		t.Errorf("the $EDITOR row carries %q in front of its label, want an "+
			"unchecked %q; the row reads %q", got, want, row)
	}
	// AND A ROW WITH NO STATE TO SHOW. Without this the third answer
	// boxBefore distinguishes — four blanks — is asserted nowhere: a
	// boxBefore that got a boxless row wrong, or a menu that started
	// drawing "[ ] " in front of plain commands, passes this whole file.
	// Raised in review of #502.
	got, row = boxBefore(t, dropdown, "Next Pane")
	if want := "    "; got != want {
		t.Errorf("the \"Next Pane\" row carries %q in front of its label, want %q "+
			"— a command item has no state to check: %q", got, want, row)
	}
}

// TestTheCheckBoxFollowsTheSelection is the other half, and what it adds
// is narrower than a second selection looks.
//
// A GLOBAL constant in the template is already caught one test up, which
// asserts "[x] " on one row and "[ ] " on another IN THE SAME FRAME.
// What needs a second selection is ONE mutation no single frame can
// reach: a PER-ITEM constant that ignores it.Checked. Measured:
//
//	--- PASS: TestTheCheckBoxIsDrawn
//	--- FAIL: TestTheCheckBoxFollowsTheSelection
//
// AND NOT A SECOND MUTATION, though one looks as though it belongs
// here: an item bound to the wrong *prop.Property[bool] is not unique
// to this test. Swapping BuiltinChecked and EditorChecked in menus.go
// fails BOTH, because the test above reads two rows of one frame and a
// swap inverts both of them; so does binding the pair to a single
// property, which renders "[x] [x]" or "[ ] [ ]". Raised in review of
// #502.
//
// Neither this test nor TestTheCheckAndTheAcceleratorAreOneState covers
// the per-item constant otherwise.
func TestTheCheckBoxFollowsTheSelection(t *testing.T) {
	dropdown := viewMenuRows(t, codeExternal)

	got, row := boxBefore(t, dropdown, "$EDITOR")
	if want := "[x] "; got != want {
		t.Errorf("with $EDITOR selected its row carries %q, want %q; the row reads %q",
			got, want, row)
	}
	got, row = boxBefore(t, dropdown, "Built in")
	if want := "[ ] "; got != want {
		t.Errorf("with $EDITOR selected the \"Built in\" row carries %q, want %q; "+
			"the row reads %q", got, want, row)
	}
}

// viewMenuRows opens the View menu with the code viewer set to which,
// and returns the dropdown's own rows — its interior, one string per
// row, border excluded.
//
// THE WINDOW IS THE MENU'S, and that is the point of the helper rather
// than the fourteen duplicated lines it replaces. Reading
// `rowText(f, y, 0, 60)` for nineteen rows below the bar takes a 60x19
// slab of the PAGE: the explorer pane, the "EDITOR" pane title, the
// tools palette and the tab strip are all inside it. dropdownRow's
// "exactly one" is then an assertion about the whole screen, and the day
// any other pane renders "Built in" or "$EDITOR" it fails and blames the
// menu for a change somewhere else. Requiring a rarer search string
// works around one instance; the leak is general.
//
// MenuBar.DropdownBounds() is the rect the menu actually painted into,
// and clipping to it also makes boxBefore's third answer real: with the
// page's border out of the window, a row with no check box returns four
// spaces instead of the border glyph plus one.
//
// THE ENVIRONMENT IS STATED, NOT INHERITED, and it is worth being exact
// about what that buys, because the obvious answer is wrong here.
//
// It does NOT make an env-dependent test deterministic. The assertions
// below match on `$EDITOR`, which editorItemText renders as a constant
// prefix of `$EDITOR (…)`; the resolved program only ever appears inside
// the parentheses. Replayed under six values — unset, vim,
// `/usr/bin/env -i`, an uninstalled name, a long path, and an
// emacsclient invocation with a quoted alternate — every arm returned
// the same booleans. Whoever removes this Setenv will find the tests
// still passing, which is why the real reason is written down:
//
// THE DIAGNOSTICS QUOTE THE ROW, so an inherited $EDITOR makes a failure
// message differ per machine and a reader cannot compare one with
// anyone else's. The label is where it gets in: editorItemText renders
// `$EDITOR (env)` today and the assertions match only the constant
// `$EDITOR` prefix, so what an inherited value changes is exactly what
// a failure PRINTS. Present tense on purpose: the interpolation ships
// today, and #477 does not ask for a label change. Raised in review of
// #502.
//
// resolveEditor reads only EDITOR (no VISUAL fallback), so the one
// variable pins the label. `/usr/bin/env -i` is the value the sibling
// test uses: it exists on any machine that can run this suite, and
// resolves to the basename "env".
func viewMenuRows(t *testing.T, which int) []string {
	t.Helper()
	// BEFORE buildPage, which is where resolveEditor runs — and here
	// rather than in each test, so the two callers cannot drift.
	t.Setenv("EDITOR", "/usr/bin/env -i")
	ed, root := buildPage(t)
	c := gooey.NewComposer(root, 150, 44)
	t.Cleanup(c.Close)
	c.Frame()
	settle(t, c)

	bar := theMenuBar(t, ed)
	i, _ := menuNamed(t, bar, "View")
	// THE PROPERTY, NOT setCodeView, and this is the one place in this
	// file that does not go in through a shipped verb. setCodeView
	// (menus.go) calls launchEditor for codeExternal — App.Suspend plus
	// exec.Command on whatever $EDITOR names — and
	// TestTheCheckBoxFollowsTheSelection passes codeExternal while the
	// Setenv above points EDITOR at a program that really exists, so
	// driving the shipped verb would spawn a subprocess under go test.
	// The property is what the menu template reads, which is the whole
	// input these assertions need. Raised in review of #502.
	ed.codeView.Set(which)
	bar.Open(i, nil)
	settle(t, c)
	f, _ := c.Frame()

	d := bar.DropdownBounds()
	if d.W <= 2 || d.H <= 2 {
		t.Fatalf("the open View menu reports bounds %v; nothing was painted, so "+
			"every assertion below would be about an empty window", d)
	}
	var rows []string
	for y := d.Y + 1; y < d.Y+d.H-1; y++ {
		rows = append(rows, rowText(f, y, d.X+1, d.W-2))
	}
	return rows
}

// dropdownRow returns the ONE row of an open menu whose text contains
// want, and fails if there is any other number of them.
//
// Exactly one is the assertion, not a convenience: zero and two are both
// real defects a Contains over the joined rows reports as a pass — an
// item drawn twice, or a label that has migrated to a neighbouring row,
// or a search string loose enough to also match the chrome around the
// menu.
//
// THE WINDOW IS WHAT BUYS UNIQUENESS, not the search string.
// viewMenuRows clips to MenuBar.DropdownBounds, so nothing outside the
// dropdown interior is in rows at all — measured on the twelve rows it
// returns, "EDITOR" without the dollar hits exactly 1. The dollar is
// kept because it is the label a user reads, not because anything
// depends on it. Raised in review of #502.
func dropdownRow(t fataler, rows []string, want string) string {
	t.Helper()
	var hits []string
	for _, r := range rows {
		if strings.Contains(r, want) {
			hits = append(hits, r)
		}
	}
	if len(hits) != 1 {
		// THE MESSAGE NAMES NO RENDER. The signature is `rows []string`
		// precisely so a caller can hand it a synthetic fixture with no
		// menu and no frame — two tests in this file do — and a message
		// naming a render that was never involved sends the reader to
		// the wrong place on the day the fixture changes shape.
		//
		// AND IT NAMES NO TEST. A test name inside a diagnostic is a
		// citation nothing checks: a rename leaves it pointing at
		// nothing, and a reader who greps for it gets this comment back
		// rather than a test. gooey.TestEveryCitedTestNameResolves
		// guards that class for CLAUDE.md only. Raised in review of
		// #502.
		t.Fatalf("%d of the %d rows given contain %q, want exactly 1:\n%s",
			len(hits), len(rows), want, strings.Join(rows, "\n"))
	}
	return hits[0]
}

// boxBefore returns the four entries immediately in front of want on its
// row — the check box, if the menu drew one there — and the row itself.
//
// Returned as TEXT so the caller compares it against the box it expects,
// rather than testing for a box's absence. "    " is a real answer and a
// distinct failure from "[x] ": one is a missing box, the other is the
// wrong state, and a negative Contains reports both as the same thing
// while also passing when the label has moved somewhere the search never
// looked. TestTheCheckBoxIsDrawn asks this of the "Next Pane" row, which
// returns exactly "    ", so the three-way distinction is exercised
// rather than hypothetical. Raised in review of #502.
//
// THE ROW COMES BACK WITH THE BOX because the caller needs it for the
// failure message, and finding it again there means a second
// dropdownRow — which holds a t.Fatalf, evaluated inside a t.Errorf's
// argument list. A Fatal from there ends the test while the Errorf that
// asked for it never runs, so the diagnostic the caller was building is
// lost at exactly the moment it is wanted.
//
// A CELL CONTRIBUTES ANY NUMBER OF RUNES, which is why this helper
// indexes neither runes nor bytes. rowText yields one entry per cell: a
// continuation contributes none, and a cell carrying a grapheme cluster
// (render.Cell.Text returns Cluster when one is set) contributes as many
// as the cluster holds — a base plus a combining mark is two runes in
// one narrow column. "Narrow glyphs" is not the fence that closes the
// cluster case, and neither is any precondition over totals; the body
// below says what replaced them and what was measured to get there.
//
// AND THE WITHIN-ROW HALF OF dropdownRow's GUARANTEE. That helper buys
// "exactly one ROW contains want" and argues for why it matters;
// strings.Index gives the same guarantee back inside the row, taking
// the leftmost of two copies and reporting the box in front of it as
// the answer — a label echoed into the accelerator column would read
// clean. Asserted rather than documented as out of scope, because the
// uniqueness argument one helper up is the reason the omission is
// conspicuous. Raised in review of #502.
func boxBefore(t fataler, rows []string, want string) (box, row string) {
	t.Helper()
	row = dropdownRow(t, rows, want)
	at := strings.Index(row, want)
	if last := strings.LastIndex(row, want); last != at {
		t.Fatalf("%q appears at byte %d and again at %d of %q, so the box in front "+
			"of the left copy is not the answer to which box precedes the label",
			want, at, last, row)
	}
	// NO PRECONDITION, BECAUSE THE QUESTION IS ANSWERABLE DIRECTLY.
	//
	// TOTALS-EQUALITY IS NOT A POSITION MAPPING, which is why there is
	// nothing to require: a rune slice guarded by
	// render.StringWidth(prefix) == len([]rune(prefix)) still answers
	// the wrong four characters. A wide glyph before the boundary and a
	// zero-width combining mark after it cancel in the sum, so the
	// totals agree while the mapping is 1:1 nowhere except at the end.
	// Measured on "世abcé" with a decomposed é — 6 runes, 6 columns, the
	// guard satisfied — the rune slice answers "bcé", three columns,
	// where the four cells in front of the label are "abcé". A wrong
	// answer, not an error, and
	// TestTheFourCellsInFrontAreNotTheFourRunes is that fixture.
	// Raised in review of #502.
	//
	// EachCluster walks columns, so the boundary is found rather than
	// assumed, and the helper has nothing left to require of its input.
	prefix := row[:at]
	total := render.StringWidth(prefix)
	if total < 4 {
		t.Fatalf("%q starts at column %d of %q, with no room for a check box in "+
			"front of it — which is how a row with no box at all reads when the "+
			"label sits within four columns of the start", want, total, row)
	}
	cut := -1
	render.EachCluster(prefix, func(_ string, off, col, _ int) bool {
		if col == total-4 {
			cut = off
			return false
		}
		return true
	})
	if cut < 0 {
		// Reachable only if a glyph STRADDLES the four-cell boundary,
		// which no check box can: every box this menu draws is four
		// narrow cells. Reported rather than rounded, because half a
		// glyph is not an answer.
		t.Fatalf("no character in %q starts at column %d, so the four cells in front "+
			"of %q begin inside a wide glyph and there is no four-cell box to read",
			prefix, total-4, want)
	}
	return prefix[cut:], row
}

// fataler is *testing.T's failure surface, narrowed to what these two
// helpers use — which is what lets a test OBSERVE a Fatalf instead of
// dying of it.
//
// testing.TB cannot be implemented outside the testing package (it has
// an unexported method), and a t.Run subtest that fails on purpose is
// still a failing subtest. A two-method interface is the smallest thing
// that makes a guard's own message assertable, and *testing.T satisfies
// it, so every real call site is unchanged. Raised in review of #502.
type fataler interface {
	Helper()
	Fatalf(format string, args ...any)
}

// caughtFatal stands in for *testing.T and records the first Fatalf.
//
// It PANICS with itself rather than returning, because a Fatalf that
// returns is not a Fatalf: the helpers under test carry on past their
// guards and produce a second, misleading failure. runtime.Goexit is
// what the real one does; a panic recovered by fatalFrom is the closest
// thing available to a caller that wants to keep running.
type caughtFatal struct{ msg string }

func (c *caughtFatal) Helper() {}

func (c *caughtFatal) Fatalf(format string, args ...any) {
	c.msg = fmt.Sprintf(format, args...)
	panic(c)
}

// fatalFrom runs fn with a stand-in T and returns the Fatalf message it
// raised. A run that does NOT fatal is itself a failure — a guard that
// no longer fires is the state these fixtures exist to detect, and
// returning "" for it would let every Contains below pass vacuously.
func fatalFrom(t *testing.T, fn func(fataler)) (msg string) {
	t.Helper()
	c := &caughtFatal{}
	// NAMED RETURN, set here. A panic recovered in a deferred function
	// leaves an UNNAMED result at its ZERO VALUE, so an unnamed one
	// hands back "" for every guard that fired correctly — and every
	// Contains at the call site then fails with an empty message, which
	// reads as the guard not firing. The opposite mistake to the one
	// the t.Fatal below catches, and just as quiet.
	defer func() {
		r := recover()
		if r == nil {
			return
		}
		if r != any(c) {
			panic(r)
		}
		msg = c.msg
	}()
	fn(c)
	t.Fatal("the helper returned without raising a Fatalf; the guard this " +
		"arm is about did not fire, so asserting on its message would pass " +
		"over a guard that has stopped guarding")
	return ""
}

// TestTheRowHelpersRefuseWhatTheyCannotAnswerFor covers every Fatal
// branch the two helpers have: four of them, which is the arithmetic a
// "covers the branches with no fixture" claim invites and cannot settle
// on its own. Five arms, because dropdownRow's single guard fails in two
// directions and a fixture can only be on one side of it at a time.
//
// NAMED, NOT NUMBERED. The ordinals this paragraph used to carry did not
// survive their own list — "both are reachable" with no first, a third,
// a fourth, and then one more appended outside the count — and a reader
// could map them onto neither the table nor the Fatalfs. The arms are
// the enumeration; each is described by what it reaches.
//
// THE LABEL TWICE ON ONE ROW is the within-row uniqueness guard, which
// boxBefore's doc argues is "the within-row half of dropdownRow's
// guarantee, asserted rather than documented as out of scope". No
// fixture put a label twice on one row, so it had never run.
//
// THE LABEL WITHIN FOUR COLUMNS OF THE ROW is `total < 4`, and it is the
// one branch reachable from the REAL menus rather than only
// synthetically: it fires if the dropdown ever loses its two-column
// indent. It is a Fatal raised from TestTheCheckBoxIsDrawn's FIRST
// assertion, so in that regression the $EDITOR and "Next Pane" arms
// below it never run and the failure reports one row of a three-row
// story — which is why the message is worth pinning rather than left to
// be read once.
//
// THE STRADDLE's own comment is the reason it went uncovered:
// "reachable only if a glyph STRADDLES the four-cell boundary, which no
// check box can". True of the menus, and boxBefore takes `rows []string`
// exactly so a synthetic row can reach what the menus cannot — which is
// the argument every arm here rests on. A branch excused because
// production cannot reach it, in a helper built to be driven directly,
// is the shape this test exists to close.
//
// NO ROW AND TWO ROWS are dropdownRow's own guard, one branch from each
// side. The "exactly one is the assertion, not a convenience" paragraph
// rests on it and every fixture in this file hands it exactly one hit,
// so neither side had ever run. Raised in review of #502.
func TestTheRowHelpersRefuseWhatTheyCannotAnswerFor(t *testing.T) {
	for _, tc := range []struct {
		name  string
		rows  []string
		label string
		want  []string
	}{
		{
			// The leftmost copy wins strings.Index, so the box in front
			// of it is reported as "the" answer while a second copy —
			// a label echoed into the accelerator column — sits
			// unexamined on the same row.
			name:  "the label appears twice on one row",
			rows:  []string{"  [x] Wrap  Wrap", "  ( ) Other"},
			label: "Wrap",
			want:  []string{"appears at byte", "and again at", "not the answer"},
		},
		{
			// dropdownRow is satisfied — exactly one row contains it —
			// and there is still no box to read.
			name:  "the label starts within four columns of the row",
			rows:  []string{"[x]Wrap", "  ( ) Other"},
			label: "Wrap",
			want:  []string{"starts at column 3", "no room for a check box"},
		},
		{
			// THE STRADDLE. "世世a" is five columns, so the four-cell
			// boundary falls at column 1 — inside the first glyph, where
			// no cluster starts. Half a glyph is not an answer, and the
			// message is the one thing that tells a reader "the box
			// straddles" apart from "there is no box".
			name:  "the four cells begin inside a wide glyph",
			rows:  []string{"世世aWrap", "  ( ) Other"},
			label: "Wrap",
			want:  []string{"starts at column 1", "inside a wide glyph"},
		},
		{
			// dropdownRow's own guard, low side: no row contains the
			// label at all.
			name:  "no row carries the label",
			rows:  []string{"  [x] Wrap", "  ( ) Other"},
			label: "Missing",
			want:  []string{"0 of the 2 rows given contain", "want exactly 1"},
		},
		{
			// And the high side: two rows do. The leftmost would win
			// strings.Index silently, which is what "exactly one is the
			// assertion, not a convenience" is about.
			name:  "two rows carry the label",
			rows:  []string{"  [x] Wrap", "  ( ) Wrap"},
			label: "Wrap",
			want:  []string{"2 of the 2 rows given contain", "want exactly 1"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			msg := fatalFrom(t, func(f fataler) { boxBefore(f, tc.rows, tc.label) })
			for _, w := range tc.want {
				if !strings.Contains(msg, w) {
					t.Errorf("the refusal reads %q and does not mention %q", msg, w)
				}
			}
		})
	}
}

// TestTheCheckBoxIsReadPastAWideGlyph is the half a passing suite cannot
// show on its own: every fixture the real menus produce is ASCII, and
// CLAUDE.md's rule is that an ASCII fixture agrees with itself under the
// rune rule and the column rule alike. So both rows here are synthetic
// and deliberately fail that agreement — same column width, different
// rune count.
//
// TWO ARMS, NEITHER OF THEM THE DISCRIMINATING ONE, and saying so is the
// point of this paragraph. A glyph behind the label cannot move any cell
// the helper reads. A glyph in front of it but OUTSIDE the last four
// columns does not move the boundary either — measured, and the inline
// comment on that arm records the measurement. What both arms show is
// that the helper does not refuse a CJK menu row, which is not
// hypothetical in an editor that lays out whatever markup it is handed.
// The arm that discriminates is
// TestTheFourCellsInFrontAreNotTheFourRunes, below, whose row is not a
// menu row at all.
func TestTheCheckBoxIsReadPastAWideGlyph(t *testing.T) {
	for _, tc := range []struct {
		name  string
		rows  []string
		label string
	}{
		{"behind the label", []string{"  [x] Wrap 世界", "  ( ) Other"}, "Wrap"},
		// A wide glyph in front of the label but OUTSIDE the last four
		// columns does not discriminate — measured: the rune slice and
		// the column walk both answer "[x] " here, because the glyph is
		// left of the boundary either rule puts the box at. Kept
		// because it is the shape a real CJK menu row has, and dropped
		// as the discriminating arm because it is not one.
		{"in front of the label", []string{"  世 [x] Wrap", "  ( ) Other"}, "Wrap"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			box, row := boxBefore(t, tc.rows, tc.label)
			// THE GUARD ESTABLISHES THE ROW, NOT THE ARM. A row whose
			// columns and runes agree is an ASCII one, and an ASCII row
			// would make this arm a duplicate of the boxBefore fixtures
			// the file already has. It does NOT make the arm
			// discriminating — the doc above measures that neither arm
			// is — and a guard that implied otherwise would be the
			// totals-for-a-position substitution boxBefore exists
			// without. Raised in review of #502.
			if cols, runes := render.StringWidth(row), len([]rune(row)); cols == runes {
				t.Fatalf("the fixture row %q measures %d columns and %d runes — equal, "+
					"so it is an ASCII row and this arm repeats one the file already "+
					"has", row, cols, runes)
			}
			if want := "[x] "; box != want {
				t.Errorf("boxBefore read %q in front of %q on %q, want %q. The glyph "+
					"moves no cell the helper reads, so what can fail here is a "+
					"reader that refuses a CJK row or slices it by the wrong unit",
					box, tc.label, row, want)
			}
		})
	}
}

// TestTheFourCellsInFrontAreNotTheFourRunes is the DISCRIMINATING arm,
// and it is separate because its row is not a menu row at all.
//
// For the two rules to disagree, something inside the last four columns
// of the prefix has to make a rune and a column different — and for a
// totals comparison to be SATISFIED while they disagree, the prefix's
// totals have to come out equal anyway. "世abcé" does both at once: 世
// is one rune and two columns, a decomposed é is two runes and one
// column, and they cancel, so the string measures 6 runes and 6 columns
// while the rune slice answers "bcé" — three columns — where the four
// cells in front of the label are "abcé".
//
// So this fixture is the wrong answer itself, asserted, and it is the
// reason boxBefore requires nothing of its input. Raised in review of
// #502.
func TestTheFourCellsInFrontAreNotTheFourRunes(t *testing.T) {
	const acute = "\u0301"
	// ONE SOURCE, and rows[0] is built FROM it. The prefix was spelled a
	// second time beside the row, and the fixture guard below plus the
	// counterexample both measured that copy — so dropping 世 from the
	// row would leave the guard measuring a six-column, six-rune string
	// the helper never sees, boxBefore returning the hand-spelled want,
	// and the test green over a fixture with no wide glyph in it at all.
	// That is the rule the comment below states, applied to itself.
	// Raised in review of #502.
	const prefix = "世abce" + acute
	rows := []string{prefix + "Wrap", "  ( ) Other"}
	if cols, runes := render.StringWidth(prefix), len([]rune(prefix)); cols != runes {
		t.Fatalf("the fixture prefix %q measures %d columns and %d runes. They must be "+
			"EQUAL, or this arm proves only that an unequal prefix is handled, where "+
			"a totals comparison would already have refused it and the interesting "+
			"case is the one it lets through", prefix, cols, runes)
	}
	box, row := boxBefore(t, rows, "Wrap")
	// THE COUNTEREXAMPLE IS READ, NOT SPELLED, for the reason dock_test's
	// rowText message is: a literal about the same fixture is a second
	// answer free to disagree with it, and this one was written twice.
	// The moment the prefix changes shape the message asserts a wrong
	// "four RUNES back from the label" with nothing red.
	r := []rune(prefix)
	runeSlice := string(r[len(r)-4:])
	if want := "abce" + acute; box != want {
		// NO CLOSING CLAUSE NAMING THE CELLS. It printed want a fourth
		// time as "the four CELLS are %q", in the one branch where box
		// != want — so on a real failure the message said two different
		// things about the same four cells and the second was false.
		// box is the four cells by the helper's contract, and it opens
		// the message. The contrast the sentence wanted is the widths.
		// Raised in review of #502.
		t.Errorf("boxBefore read %q in front of %q on %q, want %q. Four RUNES back "+
			"from the label is %q, which is %d columns, not four.",
			box, "Wrap", row, want, runeSlice, render.StringWidth(runeSlice))
	}
	if got := render.StringWidth(box); got != 4 {
		t.Errorf("boxBefore returned %q, %d columns — the contract is the four CELLS "+
			"in front of the label", box, got)
	}
}

// TestEditorLabelResolvesTheProgram is the second thing that was
// specified: an item reading "$EDITOR" tells you nothing about what will
// open.
func TestEditorLabelResolvesTheProgram(t *testing.T) {
	t.Setenv("EDITOR", "")
	if got := editorItemText(mustResolve(t)); !strings.Contains(got, "unset") {
		t.Errorf("with EDITOR empty the item reads %q; it must read as unset", got)
	}

	// A program that certainly exists on any machine running these tests.
	t.Setenv("EDITOR", "/usr/bin/env -i")
	got := editorItemText(mustResolve(t))
	if !strings.Contains(got, "(env)") {
		t.Errorf("with EDITOR=%q the item reads %q; it must name the resolved program, and "+
			"the BASENAME of it — a full path with flags is not a label", "/usr/bin/env -i", got)
	}
	if strings.Contains(got, "/usr/bin") || strings.Contains(got, "-i") {
		t.Errorf("the item reads %q; it is printing the raw variable rather than resolving it", got)
	}

	// An $EDITOR naming something that is not installed is the case the
	// resolved label exists to expose. Claiming it is there would be
	// worse than printing "$EDITOR".
	t.Setenv("EDITOR", "definitely-not-a-real-program-xyzzy")
	if got := editorItemText(mustResolve(t)); !strings.Contains(got, "not found") {
		t.Errorf("with an uninstalled EDITOR the item reads %q; it must say so", got)
	}
}

func mustResolve(t *testing.T) string {
	t.Helper()
	label, _ := resolveEditor()
	return label
}

// TestAnUnavailableEditorItemIsDisabled — the menu dims it rather than
// offering a key that fails. MenuBar reads CanExecute while PAINTING, so
// this also un-dims with no event anywhere.
func TestAnUnavailableEditorItemIsDisabled(t *testing.T) {
	t.Setenv("EDITOR", "")
	ed, _ := buildPage(t)
	bar := theMenuBar(t, ed)
	_, view := menuNamed(t, bar, "View")
	for _, it := range view.Items {
		if strings.Contains(it.Text, "EDITO") {
			if gooey.CanExecute(it.Action) {
				t.Error("with $EDITOR unset the item is still executable; a menu that offers " +
					"an action it cannot perform is worse than one that dims it")
			}
			return
		}
	}
	t.Fatal("no $EDITOR item in the View menu")
}

// TestSaveIsDisabledUntilThereIsSomethingToSaveTo — the honest greyed
// Save. Both arms, so the test cannot pass against an always-disabled or
// an always-enabled command.
func TestSaveIsDisabledUntilThereIsSomethingToSaveTo(t *testing.T) {
	ed, _ := buildPage(t)
	save, ok := ed.ctx.Values["Save"].(gooey.Action)
	if !ok {
		t.Fatal("Save is not an Action")
	}
	if gooey.CanExecute(save) {
		t.Error("Save is available with no folder open; there is nowhere for it to write")
	}

	dir := t.TempDir()
	if err := os.WriteFile(dir+"/doc.gooey", []byte("<Gooey><Canvas Name=\"R\"/></Gooey>\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ed.setWorkspace(dir)
	ed.openWorkspaceFile("doc.gooey")
	if !gooey.CanExecute(save) {
		t.Error("Save is still disabled with a file open from a real directory; the " +
			"condition is not reading the state it claims to")
	}
}

// TestTheRegionSwapChangesWhatTheEditorAreaShows — the region is the same
// region, not a second panel. Asserted through the two panels' bounds:
// the visible one occupies the pane, the other occupies nothing.
func TestTheRegionSwapChangesWhatTheEditorAreaShows(t *testing.T) {
	ed, root := buildPage(t)
	c := gooey.NewComposer(root, 150, 44)
	c.Frame()
	settle(t, c)

	area := shellRegion(t, ed, "EditorArea")
	code := shellRegion(t, ed, "CodeArea")

	ed.region.Set(regionDesign)
	settle(t, c)
	db, cb := area.Bounds(), code.Bounds()
	if db.W <= 0 || db.H <= 0 {
		t.Fatalf("in DESIGN the designer occupies %+v", db)
	}
	if cb.W > 0 && cb.H > 0 {
		t.Errorf("in DESIGN the code view still occupies %+v; the two are side by side "+
			"rather than sharing one region", cb)
	}

	ed.region.Set(regionCode)
	settle(t, c)
	db2, cb2 := area.Bounds(), code.Bounds()
	if cb2.W <= 0 || cb2.H <= 0 {
		t.Errorf("in CODE the code view occupies %+v; the swap did nothing", cb2)
	}
	if db2.W > 0 && db2.H > 0 {
		t.Errorf("in CODE the designer still occupies %+v", db2)
	}
	// And the code view lands where the designer was: SAME REGION.
	if cb2.X != db.X || cb2.Y != db.Y {
		t.Errorf("the code view is at %+v where the designer was at %+v; Code is supposed "+
			"to be the same region showing something else", cb2, db)
	}
}

// TestTheRegionSwapIsOneState — the menu's Design/Code checks and the
// swap are the same property, so they cannot disagree.
func TestTheRegionSwapIsOneState(t *testing.T) {
	ed, _ := buildPage(t)
	ed.region.Set(regionDesign)
	if !ed.designChecked.Get() || ed.codeChecked.Get() {
		t.Error("in DESIGN the menu checks disagree with the region")
	}
	ed.swapRegion()
	if ed.designChecked.Get() || !ed.codeChecked.Get() {
		t.Error("after swapRegion the menu checks disagree with the region")
	}
	ed.swapRegion()
	if !ed.designChecked.Get() {
		t.Error("swapRegion is not a toggle")
	}
}

// TestNoMenuItemAdvertisesAnUndeliverableKey.
//
// A MenuItem's Gesture is a DISPLAY hint — showing it does not bind it —
// so nothing in the framework checks that the key it names can ever
// arrive. `ctrl+“ is the trap: it PARSES fine and can never be decoded,
// because the decoder maps control bytes through `c | 0x40`, which covers
// @ A-Z [ \ ] ^ _ and stops well short of 0x60.
//
// An editor whose menu advertises a key that does nothing is an editor
// lying about its own keyboard, and this is the only thing that would
// notice.
func TestNoMenuItemAdvertisesAnUndeliverableKey(t *testing.T) {
	ed, _ := buildPage(t)
	bar := theMenuBar(t, ed)
	checked := 0
	for _, m := range bar.Menus {
		for _, it := range m.Items {
			if it.Gesture == "" {
				continue
			}
			checked++
			ev, err := input.ParseGesture(it.Gesture)
			if err != nil {
				t.Errorf("menu item %q advertises %q, which does not parse: %v", it.Text, it.Gesture, err)
				continue
			}
			if ev.Mods&input.ModCtrl != 0 && ev.Key == input.KeyRune {
				// The inverse of the decoder's own mapping: a control
				// byte becomes `c | 0x40` lowercased, so only runes in
				// @ A-Z [ \ ] ^ _ (lowercased) can ever be produced.
				r := ev.Rune
				if r == ' ' {
					continue // ctrl+space is NUL, which does arrive
				}
				up := r
				if r >= 'a' && r <= 'z' {
					up = r - 'a' + 'A'
				}
				if up < 0x40 || up > 0x5f {
					t.Errorf("menu item %q advertises %q, which PARSES but can never be "+
						"delivered: the decoder maps control bytes through c|0x40, which "+
						"never reaches %q", it.Text, it.Gesture, r)
				}
			}
		}
	}
	if checked == 0 {
		t.Fatal("no menu item carries a Gesture; this test would pass vacuously")
	}
	t.Logf("checked %d advertised gestures", checked)
}

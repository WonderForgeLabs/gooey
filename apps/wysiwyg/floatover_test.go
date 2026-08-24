package main

import (
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/paint"
)

// THE FLOAT-OVER EDITOR.
//
// One rule underneath all of these: the editor appears ON the row you
// selected. The arrangement this replaced put a single TextBox in a fixed
// track at the BOTTOM of the pane, so the value you were editing was some
// forty rows from the row you had selected — which was rejected outright,
// and rightly.
//
// That bottom track was not an oversight, and the tests here have to keep
// its lesson: as a plain VStack the pane could not be edited AT ALL,
// because ItemsView measures greedily, took every row, and left the input
// arranged at W:0 H:0 below the panel. The float-over arrangement is
// immune to that for a structural reason — the editor is not a sibling
// competing for rows, it is an overlay in the SAME cell placed by
// explicit geometry — and TestTheEditorIsNeverAZeroSizedInput is the
// guard that says so in the terms the old bug was reported in.

// propsPane is the shipped page composed at a size the shell fits in,
// with the inspector pointed at a real element.
func propsPane(t *testing.T) (*editor, *gooey.Composer, *valueEditor) {
	t.Helper()
	ed, root := buildPage(t)
	c := gooey.NewComposer(root, 150, 44)
	t.Cleanup(c.Close)
	c.Frame()
	if ed.props == nil {
		t.Fatal("the page did not mount <ValueEditor>; the pane has no editing surface")
	}
	return ed, c, ed.props
}

// rowAt selects the inspector row called name and returns its index.
func rowAt(t *testing.T, ed *editor, c *gooey.Composer, name string) int {
	t.Helper()
	i, _ := rowIndex(t, ed, name)
	ed.attrSel.Set(i)
	c.Frame()
	return i
}

// TestTheEditorFloatsOverTheSelectedRow is the headline claim, and it is
// asserted for EVERY editor that has geometry — not for one — because
// "the caret lands on the row" and "the dropdown lands somewhere else
// entirely" would both pass a single-mode test.
func TestTheEditorFloatsOverTheSelectedRow(t *testing.T) {
	ed, c, p := propsPane(t)
	ed.sel = ed.doc().Kids[1] // the Button: enum, command, text, identity rows
	ed.rebuild()
	c.Frame()

	// name → the editor it should open, and whether the editor sits ON
	// the row (inline) or anchored to it (a floated surface).
	for _, tc := range []struct {
		attr   string
		want   editorKind
		inline bool
	}{
		{"Name", editRename, true},
		{"Content", editCaret, true},
		{"Chrome", editChoice, false},
		{"Click", editBinding, false},
	} {
		rowAt(t, ed, c, tc.attr)
		ed.beginEdit()
		c.Frame()

		if got := p.Mode(); got != tc.want {
			t.Errorf("%s opened %v, want %v", tc.attr, got, tc.want)
			p.Cancel()
			c.Frame()
			continue
		}
		anchor, float := p.AnchorBounds(), p.FloatBounds()
		if anchor.W <= 0 || anchor.H <= 0 {
			t.Errorf("%s: the editor has no anchor row", tc.attr)
			p.Cancel()
			c.Frame()
			continue
		}
		if float.W <= 0 || float.H <= 0 {
			t.Errorf("%s: the editor floated at %+v — a zero-sized editor cannot be seen "+
				"or clicked, which is the bug the bottom row was itself a fix for",
				tc.attr, float)
		}
		if tc.inline {
			// ON the row: same line, and starting at or after the value
			// column so the attribute's own name stays readable.
			if float.Y != anchor.Y {
				t.Errorf("%s: the inline editor is at y=%d and its row is at y=%d",
					tc.attr, float.Y, anchor.Y)
			}
			if float.X <= anchor.X {
				t.Errorf("%s: the inline editor starts at x=%d, at or left of the row's own "+
					"x=%d, so it covers the attribute name", tc.attr, float.X, anchor.X)
			}
		} else {
			// ANCHORED to the row: adjacent to it, never across the pane.
			if float.Y == anchor.Y {
				t.Errorf("%s: the floated surface covers its own row at y=%d", tc.attr, float.Y)
			}
			if d := float.Y - anchor.Y; d < -anchor.H*2 && d > 40 {
				t.Errorf("%s: the surface is %d rows from its anchor", tc.attr, d)
			}
		}
		// Inside the pane, always. A surface that escaped would paint
		// over the designer.
		pane := p.Bounds()
		if float.X < pane.X || float.Y < pane.Y ||
			float.X+float.W > pane.X+pane.W || float.Y+float.H > pane.Y+pane.H {
			t.Errorf("%s: the editor at %+v escaped the pane at %+v", tc.attr, float, pane)
		}
		p.Cancel()
		c.Frame()
	}
}

// TestTheEditorFollowsTheSelection — the same editor opened on two
// different rows must land on two different lines. Without this the
// geometry test above passes for an editor pinned to the first row.
func TestTheEditorFollowsTheSelection(t *testing.T) {
	ed, c, p := propsPane(t)
	ed.sel = ed.doc().Kids[1]
	ed.rebuild()
	c.Frame()

	rowAt(t, ed, c, "Name")
	ed.beginEdit()
	c.Frame()
	first := p.FloatBounds()
	p.Cancel()
	c.Frame()

	rowAt(t, ed, c, "Content")
	ed.beginEdit()
	c.Frame()
	second := p.FloatBounds()
	p.Cancel()

	if first.Y == second.Y {
		t.Errorf("the editor opened at y=%d for both rows; it is pinned rather than floating "+
			"over the selection", first.Y)
	}
}

// TestTheEditorIsNeverAZeroSizedInput is the old bug restated for the new
// arrangement.
//
// "I can't modify values of attributes" was a LAYOUT failure with a
// working mechanism behind it: every unit worked and the input was
// arranged at W:0 H:0 off the panel. A behavioural test drives the
// component directly and never asks whether a human could have reached
// it, so the assertion has to be about geometry.
func TestTheEditorIsNeverAZeroSizedInput(t *testing.T) {
	ed, c, p := propsPane(t)
	tb := theTextBox(t, ed)
	ed.sel = ed.doc().Kids[1]
	ed.rebuild()
	c.Frame()

	rowAt(t, ed, c, "Content")
	ed.beginEdit()
	c.Frame()

	b := tb.Bounds()
	if b.W <= 0 || b.H <= 0 {
		t.Fatalf("the caret editor is %dx%d", b.W, b.H)
	}
	if b.X+b.W > 150 || b.Y+b.H > 44 {
		t.Errorf("the caret editor at %+v paints off a 150x44 screen", b)
	}
	// And the pane still gives the LIST the elastic track, which is the
	// half of the original fix that has to survive: a greedy ItemsView
	// beside a plain sibling leaves the sibling nothing.
	if lb := p.list.Bounds(); lb.H < 10 {
		t.Errorf("the inspector list is only %d rows tall; something is competing with it "+
			"for the star track", lb.H)
	}
	// Closed, the editor is out of the way — not merely invisible.
	p.Cancel()
	c.Frame()
	if cb := tb.Bounds(); cb.W > 0 && cb.H > 0 {
		t.Errorf("the caret editor still occupies %+v after closing", cb)
	}
}

// TestClosingTakesTheEditorOutOfTheTabOrder.
//
// Collapsed rather than zero-sized, and the difference is exactly this: a
// zero-rect focus stop is still a stop, so tab would land on an invisible
// TextBox and the keyboard would go dead with nothing on screen to
// explain it. FocusManager.move skips a Collapsed subtree; Order() still
// contains it, which is what lets SetFocus reach it in the same event
// that opens the editor, before any frame has run.
func TestClosingTakesTheEditorOutOfTheTabOrder(t *testing.T) {
	ed, c, p := propsPane(t)
	tb := theTextBox(t, ed)
	fm := c.Focus()

	fm.SetFocus(p.list)
	c.Frame()
	for i := 0; i < len(fm.Order())+1; i++ {
		fm.FocusNext()
		if fm.Focused() == tb {
			t.Fatal("tab reached the closed attribute editor: an invisible focus stop is a " +
				"keyboard that goes dead with nothing on screen to explain it")
		}
	}
	// Open, and it is reachable — the discrimination half. Without it
	// this test passes for a TextBox that is never focusable at all.
	rowAt(t, ed, c, "Name")
	ed.beginEdit()
	c.Frame()
	if fm.Focused() != tb {
		t.Error("opening the caret editor did not give it focus")
	}
}

// TestOpeningAnEditorDoesNotRepaintTheTree is the damage pin, and in this
// repo it is the ONLY thing that pins a repaint claim: a bounds
// assertion or a "the cell says X" assertion passes just as well when the
// entire tree repainted.
//
// Two halves, and the second is the one with teeth. The COUNT says how
// many components repainted; the RECTS say where, and confining every one
// of them to the PROPERTIES pane is what proves the designer, the
// toolbox, the outline and the status bar were left alone. A count alone
// would be satisfied by a change that repainted five components scattered
// across the shell.
func TestOpeningAnEditorDoesNotRepaintTheTree(t *testing.T) {
	ed, c, p := propsPane(t)
	ed.sel = ed.doc().Kids[1]
	ed.rebuild()
	c.Frame()
	rowAt(t, ed, c, "Content")

	// Focus starts on the shell's first stop (the activity rail), and
	// opening a caret editor MOVES it — which legitimately repaints the
	// component that lost it, wherever that is. That is the framework's
	// two-widget focus guarantee doing its job, not this pane leaking
	// damage, so the user's real starting position is set up first: on
	// the list they are editing from.
	c.Focus().SetFocus(p.list)
	c.Frame()

	// Settle: a frame that paints nothing is the baseline the next one
	// is measured against.
	if _, n := c.Frame(); n != 0 {
		t.Fatalf("the composition had not settled: %d components repainted with no input", n)
	}
	total := countComponents(c.Root())

	row, _ := p.list.RowBounds(ed.attrSel.Get())

	ed.beginEdit()
	_, opened := c.Frame()

	// EXACTLY THREE, and the budget is worth naming because a bound
	// ("fewer than half") is satisfied by a change that quietly doubles
	// the cost:
	//
	//   1. this component, whose Render reads mode — the subscription
	//      carrier, and the only reason opening schedules a frame at all.
	//      It paints nothing, and as a chrome-less container it
	//      pre-clears nothing either, so the rows under it are not wiped;
	//   2. the TextBox, which appeared on the row;
	//   3. the description line, which now describes the attribute.
	//
	// The LIST is not among them, and neither is any row. That is the
	// claim: an editor opening over one row does not repaint the grid it
	// opened on.
	if opened != 3 {
		t.Errorf("opening the caret editor repainted %d components of %d, want exactly 3 "+
			"(this component, the input, the description line). If your change moved this "+
			"number, that IS the change.", opened, total)
	}
	for _, r := range c.Damage() {
		if r == row {
			t.Errorf("the selected row itself repainted (%+v); nothing about the row changed", r)
		}
		if r == p.list.Bounds() && r != p.Bounds() {
			t.Errorf("the inspector list repainted (%+v)", r)
		}
	}
	pane := p.Bounds()
	for _, r := range c.Damage() {
		if r.W == 0 || r.H == 0 {
			continue
		}
		if r.X < pane.X || r.X+r.W > pane.X+pane.W {
			t.Errorf("opening the editor damaged %+v, outside the PROPERTIES pane %+v: "+
				"the rest of the shell should not have repainted at all", r, pane)
		}
	}
	t.Logf("opening the caret editor over one row: %d of %d components repainted", opened, total)

	// TYPING is the other half, and it is deliberately NOT confined to
	// the pane: a committed keystroke rebuilds the designer, which is
	// the entire point of editing live. What it must not do is repaint
	// the shell.
	p.Write("hello")
	_, typed := c.Frame()
	t.Logf("one committed keystroke: %d of %d components repainted", typed, total)
	if typed >= total/2 {
		t.Errorf("one keystroke repainted %d of %d components", typed, total)
	}
}

// TestMovingTheDropdownCursorRepaintsOnlyTheDropdown.
//
// The surface's draw func runs inside its own paint node, so the
// properties it reads are that node's dependencies. Moving the cursor
// must therefore be one repaint — the surface — and not the list
// underneath it.
func TestMovingTheDropdownCursorRepaintsOnlyTheDropdown(t *testing.T) {
	ed, c, p := propsPane(t)
	ed.sel = ed.doc().Kids[1]
	ed.rebuild()
	c.Frame()
	rowAt(t, ed, c, "Chrome")

	ed.beginEdit()
	c.Frame()
	if _, n := c.Frame(); n != 0 {
		t.Fatalf("the open dropdown had not settled: %d repainted", n)
	}
	p.PreviewKey(input.Named(input.KeyDown))
	_, moved := c.Frame()
	if moved != 1 {
		t.Errorf("moving the dropdown cursor repainted %d components, want exactly 1 (the "+
			"surface). The list underneath did not change and must not repaint.", moved)
	}
}

func countComponents(root gooey.Component) int {
	n := 0
	walkTree(root, func(gooey.Component) { n++ })
	return n
}

// TestTheTunnelBeatsTheListAndTheRootBindings.
//
// This is the one that has to go through FocusManager.Dispatch rather
// than calling PreviewKey, because what it asserts is about ROUTING. The
// list is the focused component and owns the arrows; the page root binds
// bare letters (q quits, x deletes, d toggles mode). An open editor has
// to beat both, and only the tunnelling phase runs before either.
func TestTheTunnelBeatsTheListAndTheRootBindings(t *testing.T) {
	ed, c, p := propsPane(t)
	fm := c.Focus()
	ed.sel = ed.doc().Kids[1]
	ed.rebuild()
	c.Frame()

	i := rowAt(t, ed, c, "Chrome")
	fm.SetFocus(p.list)
	c.Frame()
	ed.beginEdit()
	c.Frame()

	before := p.pick.Get()
	if !fm.Dispatch(input.Named(input.KeyDown)) {
		t.Fatal("down reached nothing while the dropdown was open")
	}
	if p.pick.Get() == before {
		t.Error("down did not move the dropdown cursor: the ItemsView consumed it first, " +
			"which is what a bubble-phase handler would have allowed")
	}
	if ed.attrSel.Get() != i {
		t.Errorf("down moved the LIST selection to %d while a dropdown was open", ed.attrSel.Get())
	}

	// x is Delete on the page root. With an editor open it must not
	// delete the selected element.
	kids := len(ed.doc().Kids)
	fm.Dispatch(input.Rune('x'))
	if len(ed.doc().Kids) != kids {
		t.Error("x deleted an element while an editor was open: a page-root KeyBinding fired " +
			"underneath the editor")
	}

	// esc closes the editor rather than selecting the parent, which is
	// what esc means on the page root.
	sel := ed.sel
	fm.Dispatch(input.Named(input.KeyEsc))
	if p.Mode() != editNone {
		t.Error("esc did not close the editor")
	}
	if ed.sel != sel {
		t.Error("esc selected the parent instead of closing the editor")
	}
	// And once closed, esc goes back to meaning what the page says.
	fm.Dispatch(input.Named(input.KeyEsc))
	if ed.sel == sel && ed.parentOf(sel) != nil {
		t.Error("with the editor closed, esc no longer selects the parent: the preview " +
			"handler is claiming keys it does not own")
	}
}

// TestAnEditorWhoseElementMovedDoesNotWriteToTheNewOne.
//
// The bug this pins was found by asking what happens if the SELECTION
// moves while an editor is open, and it is a live one — no undo, no
// concurrency, one keystroke.
//
// The caret editor is deliberately non-modal, because the TextBox has to
// see runes. So a page-root KeyBinding fires underneath it, and ctrl+n is
// bound to Next Element. Re-resolving the target through ed.target() —
// which is the rule, and is right — then makes it worse rather than
// better: ed.target() answers "what is selected NOW", so the next
// keystroke committed the value onto the element ctrl+n had just moved
// to, while the editor on screen still showed the old element's value.
//
// Measured before the fix: open the caret on <Button Content>, ctrl+n,
// type — and <Text> came back with Content set on it.
//
// The fix is an identity comparison against the node the editor OPENED
// on. It is a pointer held and never dereferenced, which is what keeps it
// compatible with the rule that forbids caching a node to write through:
// a tree replaced wholesale (an undo restoring a deep copy) fails the
// same comparison and gets the same answer.
//
// BOTH ENTRY POINTS are driven, and that is not symmetry for its own
// sake. `enter` and the `e` escape hatch open the caret through different
// functions, and a mutation that removed the identity record from
// OpenAsText alone SURVIVED a version of this test that only pressed
// enter — the escape hatch reaches every row, including every row whose
// own Kind would have opened something modal, so it is if anything the
// wider door onto this bug.
func TestAnEditorWhoseElementMovedDoesNotWriteToTheNewOne(t *testing.T) {
	for _, tc := range []struct {
		name string
		open func(*editor)
	}{
		{"enter", func(ed *editor) { ed.beginEdit() }},
		{"e (edit as text)", func(ed *editor) { ed.editSelectedAsText() }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ed, c, p := propsPane(t)
			fm := c.Focus()
			button := ed.doc().Kids[1]
			ed.sel = button
			ed.rebuild()
			c.Frame()
			rowAt(t, ed, c, "Content")

			tc.open(ed)
			c.Frame()
			if p.Mode() != editCaret || fm.Focused() != p.text {
				t.Fatalf("fixture: the caret editor did not open (mode %v, focused-is-input %v)",
					p.Mode(), fm.Focused() == p.text)
			}
			was := button.Attrs["Content"]

			// ctrl+n is Next Element on the page root. The caret editor
			// does not swallow it, by design, so the selection moves.
			fm.Dispatch(input.KeyEvent{Key: input.KeyRune, Rune: 'n', Mods: input.ModCtrl})
			moved := ed.sel
			if moved == button {
				t.Fatal("ctrl+n did not move the selection; this test would assert nothing")
			}

			// The keystroke that used to land on the wrong element.
			ed.editValue.Set("TYPED")
			ed.commitEdit()
			c.Frame()

			if got := moved.Attrs["Content"]; got == "TYPED" {
				t.Errorf("typing after the selection moved wrote Content=%q onto <%s>, which "+
					"is NOT the element the editor was opened on (<%s>)",
					got, moved.Elem, button.Elem)
			}
			if got := button.Attrs["Content"]; got != was {
				t.Errorf("the element being edited had Content changed from %q to %q by a "+
					"keystroke aimed at a different selection", was, got)
			}
			if p.Mode() != editNone {
				t.Errorf("the editor is still open (%v) on an element that is no longer "+
					"selected", p.Mode())
			}
			// Retired, not cancelled: the pending value belonged to the
			// old element and must not be written back to either one.
			if p.name != "" {
				t.Errorf("the retired editor still points at %q", p.name)
			}
		})
	}
}

// TestTypingDoesNotRetireTheEditorItIsTypingInto is MY side of a
// two-directional contract, and it is the direction that is easy to lose.
//
// The staleness guard retires an editor whenever ed.sel changes identity.
// That is exactly right when the document was replaced — and it means
// every path that does NOT replace the document must leave ed.sel
// strictly alone, or the editor is retired out from under a user who did
// nothing wrong.
//
// The path that makes this urgent is ed.rebuild(), because the caret
// editor commits LIVE: every rune runs a full rebuild. So "rebuild
// reassigns the selection" would not be a rare edge case, it would retire
// the editor on the second character of every value anyone ever typed.
//
// It holds today because rebuild only READS ed.sel (for the outline
// marker). Nothing declares that, which is the whole reason this test
// exists: rebuilding the tree and re-resolving the selection is a
// perfectly reasonable optimisation with no local sign that it has
// broken editing.
func TestTypingDoesNotRetireTheEditorItIsTypingInto(t *testing.T) {
	ed, c, p := propsPane(t)
	fm := c.Focus()
	button := ed.doc().Kids[1]
	ed.sel = button
	ed.rebuild()
	c.Frame()
	rowAt(t, ed, c, "Content")

	ed.beginEdit()
	c.Frame()
	if p.Mode() != editCaret || fm.Focused() != p.text {
		t.Fatalf("fixture: the caret editor did not open (mode %v)", p.Mode())
	}
	ed.editValue.Set("")
	ed.commitEdit()

	// Checked after EVERY rune, not just at the end: a retire on rune two
	// and a retire on rune six are the same bug, and only a per-step
	// assertion says which.
	for i, r := range "widget" {
		if !fm.Dispatch(input.Rune(r)) {
			t.Fatalf("rune %d (%q) reached nothing", i, r)
		}
		if p.stale() {
			t.Fatalf("after rune %d (%q) the editor went stale: a rebuild moved ed.sel, so "+
				"every value typed here is abandoned partway through", i, r)
		}
		if p.Mode() != editCaret {
			t.Fatalf("after rune %d (%q) the editor closed (mode %v)", i, r, p.Mode())
		}
		if p.on != ed.sel {
			t.Fatalf("after rune %d (%q) the editor's element and the selection are different "+
				"pointers; a rebuild that does not replace the document must leave the "+
				"selection ALONE", i, r)
		}
		if fm.Focused() != p.text {
			t.Fatalf("after rune %d (%q) focus left the input", i, r)
		}
	}
	c.Frame()

	// And the whole word landed, on the element it was typed into.
	if got := button.Attrs["Content"]; got != "widget" {
		t.Errorf("Content = %q, want %q", got, "widget")
	}
}

// TestRebuildNeverMovesTheSelectionPointer covers rebuild's BRANCHES,
// which the typing test above does not.
//
// That test drives six runes down one path and is the honest statement of
// what a user feels. But rebuild() has three exits — a remote push, a
// failed build, and the success path — and typing only ever reaches the
// third. An `ed.sel = …` added to either early return would leave the
// typing test green and break editing for everyone in remote mode, or for
// everyone the moment their document stopped compiling, which is a state
// you pass through constantly while editing.
//
// This is the same obligation slice-undo declared on its side (a restore
// is the ONLY thing that may move ed.sel) asserted from mine, and it is
// deliberately behavioural rather than a grep over main.go: a source scan
// would go red on an assignment in an unrelated function and stay green
// on a helper called from rebuild that does the same thing.
//
// THE ARMS ARE INDEPENDENT, and that is a property to re-check rather
// than a number to trust: each subtest is reachable only through its own
// exit, so a mutation planted on one exit must redden exactly one arm and
// leave the other two green. Three subtests behind one name is where an
// arm that can never fire hides — the parent goes red because a SIBLING
// caught the mutation, and nobody notices the third was decoration. Plant
// a reseat of ed.sel on each exit in turn and confirm the diagonal; each
// arm also fatals if its own precondition (a green build, a failed build,
// an attached remote) does not hold, so it cannot silently drift onto a
// neighbour's path either.
func TestRebuildNeverMovesTheSelectionPointer(t *testing.T) {
	t.Run("build succeeds", func(t *testing.T) {
		ed, _ := buildPage(t)
		sel := ed.doc().Kids[1]
		ed.sel = sel
		ed.rebuild()
		if !strings.HasPrefix(ed.status.Get(), "✓") {
			t.Fatalf("fixture: wanted a successful build, got %q", ed.status.Get())
		}
		if ed.sel != sel {
			t.Error("a successful rebuild replaced the selection pointer")
		}
	})

	t.Run("build fails", func(t *testing.T) {
		ed, _ := buildPage(t)
		sel := ed.doc().Kids[1]
		ed.sel = sel
		// An unknown attribute is a load error, which is an ordinary
		// state to be in while typing — you are in it between two
		// keystrokes of every attribute name.
		sel.Attrs["NoSuchAttributeAnywhere"] = "x"
		ed.rebuild()
		if !strings.HasPrefix(ed.status.Get(), "✗") {
			t.Fatalf("fixture: wanted a failed build, got %q; this subtest would exercise "+
				"the success path instead", ed.status.Get())
		}
		if ed.sel != sel {
			t.Error("a rebuild that failed to build replaced the selection pointer: the " +
				"editor open on that element would retire mid-word, every time the " +
				"document briefly stopped compiling")
		}
	})

	t.Run("remote", func(t *testing.T) {
		ed, _ := attachedEditor(t)
		if ed.remote == nil {
			t.Fatal("fixture: not attached, so the remote branch is not exercised")
		}
		sel := ed.doc().Kids[0]
		ed.sel = sel
		ed.rebuild()
		if ed.sel != sel {
			t.Error("a rebuild in remote mode replaced the selection pointer")
		}
	})
}

// TestAFloatedEditorDoesNotLetTheSelectionMoveAtAll is the other half,
// and it is why only the caret editor needed the guard above: every
// floated editor is modal, so the gesture never reaches the page root and
// the selection cannot move out from under it in the first place.
//
// Both halves are needed. Without this one, "the caret editor retires"
// would look like the general answer, and the real invariant — an editor
// never commits to an element the user is not looking at — would have
// only one of its two mechanisms checked.
func TestAFloatedEditorDoesNotLetTheSelectionMoveAtAll(t *testing.T) {
	ed, c, p := propsPane(t)
	fm := c.Focus()
	ed.sel = ed.doc().Kids[1]
	ed.rebuild()
	c.Frame()
	rowAt(t, ed, c, "Chrome")
	fm.SetFocus(p.list)
	c.Frame()

	ed.beginEdit()
	c.Frame()
	if p.Mode() != editChoice {
		t.Fatalf("fixture: Chrome opened %v", p.Mode())
	}
	before := ed.sel
	if !fm.Dispatch(input.KeyEvent{Key: input.KeyRune, Rune: 'n', Mods: input.ModCtrl}) {
		t.Fatal("ctrl+n reached nothing")
	}
	if ed.sel != before {
		t.Errorf("ctrl+n moved the selection to <%s> while a dropdown was open; a modal "+
			"editor must not let a page-root gesture through", ed.sel.Elem)
	}
	if p.Mode() != editChoice {
		t.Errorf("the dropdown closed on a key it should have swallowed: %v", p.Mode())
	}
}

// TestARowScrolledOutOfViewGetsNoEditor.
//
// RowBounds has no honest answer for a row outside the realized window,
// and the alternative to saying so is an editor floated at the zero rect
// — a live caret in the pane's top-left corner, over a row it does not
// belong to. That is the silent failure this whole component exists to
// remove, so it is asserted directly.
func TestARowScrolledOutOfViewGetsNoEditor(t *testing.T) {
	ed, c, p := propsPane(t)
	ed.sel = ed.doc().Kids[1]
	ed.rebuild()
	c.Frame()

	rowAt(t, ed, c, "Content")
	ed.beginEdit()
	c.Frame()
	if p.FloatBounds().W == 0 {
		t.Fatal("fixture: the editor did not open on a visible row")
	}

	// Point the selection past the end of the list. The row does not
	// exist, so it is certainly not realized.
	ed.attrSel.Set(len(ed.attrRows()) + 50)
	c.Frame()
	if got := p.FloatBounds(); got != (gooey.Rect{}) {
		t.Errorf("the editor floated at %+v for a row that is not on screen", got)
	}
}

// THE PER-KIND EDITORS, driven end to end.
//
// Each asserts the same shape: the editor opens, the KEY the user would
// press changes the document, and what lands in the markup is something
// the loader accepts. The last clause is the one that matters — an editor
// that writes a value the loader rejects is the catalog lying about the
// target.

func TestTheColourPickerWritesAHexLiteralOnEveryPress(t *testing.T) {
	ed, c, p := propsPane(t)
	// Background is KindColor on a Canvas.
	ed.sel = ed.doc()
	ed.rebuild()
	c.Frame()
	rowAt(t, ed, c, "Background")

	ed.beginEdit()
	c.Frame()
	if p.Mode() != editColor {
		t.Fatalf("Background opened %v, want the colour picker", p.Mode())
	}
	if !p.PreviewKey(input.Named(input.KeyRight)) {
		t.Fatal("the colour picker declined ▸")
	}
	_, _, target := ed.target()
	got := target.Attrs["Background"]
	if got == "" {
		t.Fatal("adjusting a channel wrote nothing: the picker is not live")
	}
	if _, err := paint.ParseColor(got); err != nil {
		t.Errorf("the picker wrote %q, which is not a colour literal: %v", got, err)
	}
	if got != strings.ToLower(got) {
		t.Errorf("the picker wrote %q; #rrggbb is the spelling the rest of the repo uses", got)
	}
	if !strings.HasPrefix(ed.status.Get(), "✓") {
		t.Errorf("the document does not build after a colour edit: %s", ed.status.Get())
	}
	// And it keeps moving — one write could be the value it opened with.
	before := got
	p.PreviewKey(input.Named(input.KeyRight))
	if target.Attrs["Background"] == before {
		t.Error("a second ▸ changed nothing: the picker committed once and went inert")
	}
}

func TestTheTrackEditorWritesOnEveryKeystroke(t *testing.T) {
	ed, c, p := propsPane(t)
	// A Grid, so Rows/Cols are on offer.
	ed.doc().Kids = append(ed.doc().Kids, &node{
		Elem:  "Grid",
		Attrs: map[string]string{"Name": "G", "Rows": "Auto,1*,20", "Canvas.Left": "0", "Canvas.Top": "0"},
	})
	ed.sel = ed.doc().Kids[len(ed.doc().Kids)-1]
	ed.rebuild()
	c.Frame()
	rowAt(t, ed, c, "Rows")

	ed.beginEdit()
	c.Frame()
	if p.Mode() != editTracks {
		t.Fatalf("Rows opened %v, want the track editor", p.Mode())
	}
	target := ed.sel

	// ▸ on the selected track. Track 0 is Auto, which has no size — the
	// editor must SAY so rather than swallow the key.
	p.PreviewKey(input.Named(input.KeyRight))
	if target.Attrs["Rows"] != "Auto,1*,20" {
		t.Errorf("resizing an Auto track changed the spec to %q", target.Attrs["Rows"])
	}
	if !strings.Contains(ed.status.Get(), "Auto") {
		t.Errorf("resizing an Auto track said %q; it must explain itself", ed.status.Get())
	}

	// Down to the star track, then ▸: the document follows the key.
	p.PreviewKey(input.Named(input.KeyDown))
	p.PreviewKey(input.Named(input.KeyRight))
	if got := target.Attrs["Rows"]; got != "Auto,2*,20" {
		t.Errorf("Rows = %q after one ▸ on the star track, want %q", got, "Auto,2*,20")
	}

	// k cycles the kind, a adds, x removes — each written immediately.
	p.PreviewKey(input.Rune('a'))
	if got := target.Attrs["Rows"]; !strings.HasPrefix(got, "Auto,2*,20,") {
		t.Errorf("adding a track produced %q", got)
	}
	p.PreviewKey(input.Rune('x'))
	if got := target.Attrs["Rows"]; got != "Auto,2*,20" {
		t.Errorf("removing the added track produced %q", got)
	}

	// Everything it wrote has to load.
	if _, err := components.ParseGridLens(target.Attrs["Rows"]); err != nil {
		t.Errorf("the track editor wrote %q, which markup cannot parse: %v", target.Attrs["Rows"], err)
	}
	if !strings.HasPrefix(ed.status.Get(), "✓") {
		t.Errorf("the document does not build after a track edit: %s", ed.status.Get())
	}
}

// TestAFloatedSurfaceFollowsTheDocumentItIsShowing asserts the OUTCOME —
// what the surface has on screen after a live edit — and deliberately not
// the route by which it got there. The distinction is the whole comment,
// because the obvious stronger claim cannot be made here.
//
// The track editor draws the document's own tracks through
// ed.attrRows(), which is plain Go state and invisible to the property
// graph, so the surface's paint node reads ed.rev to subscribe. That read
// is real and it is correct. What it is NOT is observable: a commit ends
// in ed.rebuild(), which currently dirties the page root, and once the
// root repaints the z-ordered pass carries every overlay above it. A
// surface with the subscription deleted still shows the right value, and
// deleting it was mutation-tested against this test and SURVIVED.
//
// So this is an equivalence check, and it is written down as one rather
// than named for a mechanism it does not exercise. It goes red if the
// track editor ever draws something other than the document it is
// editing, which is what a user would report; it will start carrying the
// subscription too, unaided, if rebuild() ever stops repainting the
// shell.
func TestAFloatedSurfaceFollowsTheDocumentItIsShowing(t *testing.T) {
	ed, c, p := propsPane(t)
	ed.doc().Kids = append(ed.doc().Kids, &node{
		Elem:  "Grid",
		Attrs: map[string]string{"Name": "G", "Rows": "Auto,1*", "Canvas.Left": "0", "Canvas.Top": "0"},
	})
	ed.sel = ed.doc().Kids[len(ed.doc().Kids)-1]
	ed.rebuild()
	c.Frame()
	rowAt(t, ed, c, "Rows")

	ed.beginEdit()
	f, _ := c.Frame()
	if p.Mode() != editTracks {
		t.Fatalf("Rows opened %v", p.Mode())
	}
	box := p.FloatBounds()
	before := cellLine(f, box.X+1, box.Y+1, box.W-2)
	if !strings.Contains(before, "Auto") {
		t.Fatalf("the surface's first track row reads %q, want the Auto track", before)
	}

	// k cycles the track's kind and writes it. The surface must follow.
	p.PreviewKey(input.Rune('k'))
	f, _ = c.Frame()
	after := cellLine(f, box.X+1, box.Y+1, box.W-2)
	if after == before {
		t.Errorf("the surface still reads %q after the track changed to %q; it is showing a "+
			"stale answer", after, ed.sel.Attrs["Rows"])
	}
	if !strings.Contains(after, "Star") {
		t.Errorf("the surface reads %q; the track is now %q", after, ed.sel.Attrs["Rows"])
	}
}

// cellLine reads w cells of row y as a string.
func cellLine(f *gooey.Frame, x, y, w int) string {
	var sb strings.Builder
	for i := 0; i < w; i++ {
		sb.WriteRune(f.Cells.At(x+i, y).Rune)
	}
	return strings.TrimRight(sb.String(), " ")
}

// TestTheLastTrackCannotBeRemoved. components.Grid defaults a missing
// definition to a single star track, so removing the last one would be a
// no-op the user reads as a broken key.
func TestTheLastTrackCannotBeRemoved(t *testing.T) {
	ed, c, p := propsPane(t)
	ed.doc().Kids = append(ed.doc().Kids, &node{
		Elem:  "Grid",
		Attrs: map[string]string{"Name": "G", "Rows": "1*", "Canvas.Left": "0", "Canvas.Top": "0"},
	})
	ed.sel = ed.doc().Kids[len(ed.doc().Kids)-1]
	ed.rebuild()
	c.Frame()
	rowAt(t, ed, c, "Rows")
	ed.beginEdit()

	p.PreviewKey(input.Rune('x'))
	if got := ed.sel.Attrs["Rows"]; got != "1*" {
		t.Errorf("Rows = %q after removing the only track", got)
	}
	if !strings.Contains(ed.status.Get(), "at least one track") {
		t.Errorf("refusing to remove the last track said %q", ed.status.Get())
	}
}

// TestTheGestureEditorCapturesTheChordItWasGiven.
//
// input.KeyEvent.String and input.ParseGesture round-trip, so what is
// captured is what the loader will parse — which is the whole reason to
// capture rather than to ask the user to spell "ctrl+shift+pageup".
// TestTheGestureEditorCapturesTheChordItWasGiven.
//
// input.KeyEvent.String and input.ParseGesture round-trip, so what is
// captured is what the loader will parse — which is the whole reason to
// capture rather than to ask the user to spell "ctrl+shift+pageup".
//
// IT CANNOT BE DRIVEN THROUGH `enter` ON A ROW TODAY, and the reason is
// worth writing down rather than working around: the only two elements in
// the vocabulary with a KindGesture attribute are <KeyBinding> and
// <Tooltip>, both NonVisual, and the palette excludes every non-visual
// element — so the inspector has no way to select one and no Gesture row
// exists to press enter on. That is a gap in what the property browser
// can REACH, not in the editor, and the sweep at the end reports the
// population so this test starts carrying the gesture end-to-end the day
// an attachment becomes selectable.
//
// Writing the row-driven version and reading its green would have been a
// check that reported success without having checked, so the editor is
// driven at its own seam instead and the reachability is asserted
// separately.
func TestTheGestureEditorCapturesTheChordItWasGiven(t *testing.T) {
	ed, c, p := propsPane(t)
	ed.doc().Kids = append(ed.doc().Kids, &node{
		Elem:  "KeyBinding",
		Attrs: map[string]string{"Gesture": "ctrl+s", "Command": "{{.Noop}}"},
	})
	target := ed.doc().Kids[len(ed.doc().Kids)-1]
	ed.sel = target
	ed.rebuild()
	c.Frame()

	// The state Open() would have put the editor in for a KindGesture
	// row, set here because no such row is reachable — see above.
	p.name, p.body, p.undo = "Gesture", false, "ctrl+s"
	p.mode.Set(int(editGesture))

	chord := input.KeyEvent{Key: input.KeyPageUp, Mods: input.ModCtrl | input.ModShift}
	if !p.PreviewKey(chord) {
		t.Fatal("the capture declined the chord")
	}
	got := target.Attrs["Gesture"]
	if got == "ctrl+s" {
		t.Fatal("the capture wrote nothing")
	}
	back, err := input.ParseGesture(got)
	if err != nil {
		t.Fatalf("the capture wrote %q, which is not a gesture: %v", got, err)
	}
	if back != chord {
		t.Errorf("the capture wrote %q, which parses back to %+v rather than the chord pressed "+
			"(%+v)", got, back, chord)
	}
	if p.Mode() != editNone {
		t.Error("the capture stayed open after catching a chord")
	}

	// Esc must remain the way out, or the capture is a trap: every other
	// key it can catch commits and closes.
	p.name, p.undo = "Gesture", got
	p.mode.Set(int(editGesture))
	p.PreviewKey(input.Named(input.KeyEsc))
	if p.Mode() != editNone {
		t.Error("esc did not leave the chord capture")
	}
	if target.Attrs["Gesture"] != got {
		t.Errorf("esc changed the gesture to %q", target.Attrs["Gesture"])
	}

	// The population, reported rather than asserted. When this stops
	// being zero the gesture editor has a row to open from and the
	// row-driven version of this test becomes writable.
	reachable := 0
	for _, e := range ed.palette {
		for _, a := range markup.AttrsFor(e, "Canvas") {
			if a.Kind == markup.KindGesture {
				reachable++
			}
		}
	}
	t.Logf("KindGesture attributes reachable from the inspector: %d "+
		"(zero is why the capture above is driven at its seam; both carriers are NonVisual "+
		"and the palette excludes those)", reachable)
}

func TestTheStepperMovesANumberWithoutATextBox(t *testing.T) {
	ed, c, p := propsPane(t)
	ed.sel = ed.doc().Kids[1]
	ed.rebuild()
	c.Frame()
	rowAt(t, ed, c, "Width")

	ed.beginEdit()
	c.Frame()
	if p.Mode() != editStepper {
		t.Fatalf("Width opened %v, want the stepper", p.Mode())
	}
	tb := theTextBox(t, ed)
	if b := tb.Bounds(); b.W > 0 && b.H > 0 {
		t.Errorf("the stepper put a caret at %+v; a number does not need a text box", b)
	}
	p.PreviewKey(input.Named(input.KeyRight))
	first := ed.sel.Attrs["Width"]
	if first == "" {
		t.Fatal("▸ wrote nothing")
	}
	p.PreviewKey(input.Named(input.KeyRight))
	if ed.sel.Attrs["Width"] == first {
		t.Error("a second ▸ changed nothing")
	}
	if !strings.HasPrefix(ed.status.Get(), "✓") {
		t.Errorf("the document does not build after a stepper edit: %s", ed.status.Get())
	}
}

// TestAKindWithNoEditorSaysSoRatherThanDoingNothing is the runtime half
// of the exhaustiveness rule. The table is checked at test time; this is
// what a user would see if one ever slipped through, and it must not be
// silence.
func TestAKindWithNoEditorSaysSoRatherThanDoingNothing(t *testing.T) {
	ed, c, p := propsPane(t)
	ed.sel = ed.doc().Kids[1]
	ed.rebuild()
	c.Frame()

	// A row whose Kind nothing maps. Reached through the pane's own
	// dispatch, not by calling an arm directly.
	i := rowAt(t, ed, c, "Content")
	_ = i
	before := ed.status.Get()
	saved := editors[markup.KindText]
	delete(editors, markup.KindText)
	t.Cleanup(func() { editors[markup.KindText] = saved })

	ed.beginEdit()
	if p.Mode() != editNone {
		t.Errorf("a Kind with no editor opened %v", p.Mode())
	}
	if s := ed.status.Get(); s == before || !strings.Contains(s, "no editor") {
		t.Errorf("a Kind with no editor left the status at %q; the user must be told rather "+
			"than watch enter do nothing", s)
	}
}

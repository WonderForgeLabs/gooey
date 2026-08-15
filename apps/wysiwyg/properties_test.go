package main

import (
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/input"
)

// The PROPERTIES pane is where a value is actually changed, and these are
// the pins for it.
//
// They exist because of a defect that every other kind of check passed
// through. "I can't modify values of attributes" was reported against a
// running editor whose model was FINE: beginEdit loaded the row, a
// keystroke dispatched to the TextBox updated the bound property, and
// commitEdit wrote the attribute. What was broken was geometry — the pane
// was a VStack, ItemsView measures greedily, so the list took all 41 rows
// and the input underneath it was arranged at W:0 H:0 on a row below the
// panel. Invisible and unclickable, with every unit of the mechanism
// working perfectly.
//
// So the assertion that catches it is about LAYOUT, not behaviour, and it
// has to be: a behavioural test drives the component directly and never
// asks whether a human could have reached it.

// shellTree builds the shipped page and composes it at a realistic size.
func shellTree(t *testing.T) (*editor, gooey.Component, *gooey.Composer) {
	t.Helper()
	ed, root := buildPage(t)
	comp := gooey.NewComposer(root, 150, 44)
	comp.Frame()
	return ed, root, comp
}

// walkTree visits every component in the retained tree.
func walkTree(root gooey.Component, fn func(gooey.Component)) {
	fn(root)
	if c, ok := root.(gooey.Container); ok {
		for _, k := range c.ChildComponents() {
			if k != nil {
				walkTree(k, fn)
			}
		}
	}
}

func theTextBox(t *testing.T, root gooey.Component) *components.TextBox {
	t.Helper()
	var found []*components.TextBox
	walkTree(root, func(c gooey.Component) {
		if tb, ok := c.(*components.TextBox); ok {
			found = append(found, tb)
		}
	})
	if len(found) != 1 {
		t.Fatalf("the shell has %d TextBoxes; these tests assume the one in PROPERTIES", len(found))
	}
	return found[0]
}

// TestTheAttributeEditorIsOnScreenAndUsable is the direct pin for the
// reported bug. Every clause failed before the fix.
func TestTheAttributeEditorIsOnScreenAndUsable(t *testing.T) {
	_, root, _ := shellTree(t)
	tb := theTextBox(t, root)
	b := tb.Bounds()

	if b.W <= 0 || b.H <= 0 {
		t.Fatalf("the attribute editor is %dx%d; a zero-sized input cannot be seen or clicked", b.W, b.H)
	}
	// Inside the 150x44 screen, in full. The bug put it at row 43 with the
	// panel ending at 32; an input that paints past the edge is as unusable
	// as one with no size, and it corrupts whatever it overlaps.
	if b.X < 0 || b.Y < 0 || b.X+b.W > 150 || b.Y+b.H > 44 {
		t.Errorf("the attribute editor at %+v is not fully on a 150x44 screen", b)
	}
	// Inside the PROPERTIES region specifically: Grid.Col=3 of a
	// Cols="4,38,1*,46" page, so columns 104..149.
	if b.X < 104 {
		t.Errorf("the attribute editor at x=%d is left of the PROPERTIES region; it has escaped its pane", b.X)
	}
	if !tb.AcceptsFocus() {
		t.Error("the attribute editor does not accept focus")
	}
}

// TestTheAttributeEditorIsAFocusStop — reachable by keyboard, which is the
// only way it is reachable at all under a recording pty.
func TestTheAttributeEditorIsAFocusStop(t *testing.T) {
	_, root, comp := shellTree(t)
	tb := theTextBox(t, root)
	for _, c := range comp.Focus().Order() {
		if c == tb {
			return
		}
	}
	t.Error("the attribute editor is not in the focus order; nothing can tab to it")
}

// TestTypingIntoTheEditorChangesTheAttribute is the end-to-end half,
// driven through the focus manager rather than by calling the editor's
// methods — so it exercises the dispatch path a keystroke really takes,
// including the page-root KeyBindings it has to get past. `x` is bound to
// Delete on the root, which makes it the interesting character to type.
func TestTypingIntoTheEditorChangesTheAttribute(t *testing.T) {
	ed, root, comp := shellTree(t)
	tb := theTextBox(t, root)
	fm := comp.Focus()

	// Point the editor at the selected component's Name.
	ed.attrSel.Set(0)
	ed.beginEdit()
	comp.Frame()
	if ed.editName.Get() != "Name" {
		t.Fatalf("beginEdit loaded %q, want the first row (Name)", ed.editName.Get())
	}

	fm.SetFocus(tb)
	comp.Frame()
	if !tb.IsFocused() {
		t.Fatal("could not focus the attribute editor")
	}
	// Clear, then type a value containing `x`.
	ed.editValue.Set("")
	comp.Frame()
	for _, r := range "box" {
		if !fm.Dispatch(input.Rune(r)) {
			t.Fatalf("keystroke %q reached nothing", r)
		}
	}
	comp.Frame()
	if got := ed.editValue.Get(); got != "box" {
		t.Fatalf("after typing, the bound value is %q, want %q — a root KeyBinding may be eating the keystroke", got, "box")
	}

	fm.Dispatch(input.Named(input.KeyEnter))
	comp.Frame()
	_, _, target := ed.target()
	if target == nil {
		t.Fatal("no target component")
	}
	if got := target.Attrs["Name"]; got != "box" {
		t.Errorf("the component's Name is %q after committing; want %q", got, "box")
	}
}

// TestEditorInputsAreSiblingsOfThePreview is REVIVED, per the note left
// where it was retired: restore it on the first commit that adds a TextBox
// back. The shell's PROPERTIES pane is that TextBox.
//
// The hazard it guards: the designer's subtree is discarded and rebuilt on
// every edit, replacing a subtree resets component-local state, and a caret
// is component-local. An input inside the island would reset its caret to 0
// on every keystroke, so the user's next character lands mid-word.
func TestEditorInputsAreSiblingsOfThePreview(t *testing.T) {
	_, root, _ := shellTree(t)
	island := findPreview(root)
	if island == nil {
		t.Fatal("the shell does not mount the designer")
	}
	// The island must contain no input. Checked by walking the island
	// rather than by trusting the page's shape, because the page is markup
	// and can be rearranged without touching any Go.
	walkTree(island, func(c gooey.Component) {
		if tb, ok := c.(*components.TextBox); ok {
			t.Errorf("a TextBox (%p) lives inside the rebuilt designer island; "+
				"its caret resets on every edit and the user's next character lands mid-word", tb)
		}
	})
	// And the assertion is only meaningful while there IS an input to
	// misplace — the reason the retired version would have passed
	// vacuously. This is the discrimination half.
	theTextBox(t, root)
}

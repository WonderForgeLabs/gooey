package main

import (
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/render"
)

// The per-Kind editors. The inspector's `enter` DISPATCHES BY KIND: a row
// with a finite value set advances to the next one, everything else loads
// the text input. What these pin is not the gesture but the source of the
// values — every list comes from the running app, so every value the
// editor offers is one the loader accepts.

// rowIndex is the inspector row for name, and its position — the
// selection index the editor's commands read.
func rowIndex(t *testing.T, ed *editor, name string) (int, attrRow) {
	t.Helper()
	for i, r := range ed.attrRows() {
		if r.name == name {
			return i, r
		}
	}
	t.Fatalf("no inspector row for %s", name)
	return 0, attrRow{}
}

// TestEnterOpensThePerKindEditorAndNothingElse pins the dispatch that IS
// the per-Kind editor.
//
// It used to assert that enter CYCLED a finite-valued row to its next
// value. That was the whole editor for those Kinds and it is now the
// dropdown's commit: enter opens a list positioned over the row, the
// arrows move inside it, and enter again writes. The claim underneath is
// unchanged and is the one that matters — a row whose values the catalog
// knows is never typed at — so it is asserted against the gesture that
// now carries it.
func TestEnterOpensThePerKindEditorAndNothingElse(t *testing.T) {
	ed, _ := buildPage(t)
	ed.retype("Canvas")
	ed.rebuild()
	ed.sel = ed.doc().Kids[1] // the Button

	// KindEnum: Chrome. Enter must OPEN the dropdown and change nothing
	// yet — a list that committed on open would make cancelling
	// impossible.
	i, before := rowIndex(t, ed, "Chrome")
	ed.attrSel.Set(i)
	ed.beginEdit()
	if got := ed.props.Mode(); got != editChoice {
		t.Fatalf("enter on a KindEnum row opened %v, want the dropdown", got)
	}
	if _, still := rowIndex(t, ed, "Chrome"); still.value != before.value {
		t.Errorf("opening the dropdown changed the value %q -> %q; nothing is committed until enter",
			before.value, still.value)
	}
	// Down, then enter, must land on a member of the row's own cycle.
	ed.props.PreviewKey(input.Named(input.KeyDown))
	ed.props.PreviewKey(input.Named(input.KeyEnter))
	_, after := rowIndex(t, ed, "Chrome")
	if after.value == before.value {
		t.Fatalf("committing the dropdown did not move the value, stayed %q", before.value)
	}
	if !contains(after.cycle(), after.value) {
		t.Fatalf("committed %q, which is not in the offered set %v", after.value, after.cycle())
	}
	if ed.props.Mode() != editNone {
		t.Errorf("the dropdown is still open after committing: %v", ed.props.Mode())
	}

	// KindText: Content. Enter must open the CARET editor, load the
	// input, and leave the document alone — typing is the editor for
	// free text.
	j, content := rowIndex(t, ed, "Content")
	ed.attrSel.Set(j)
	ed.beginEdit()
	if got := ed.props.Mode(); got != editCaret {
		t.Fatalf("enter on a KindText row opened %v, want the caret editor", got)
	}
	if _, again := rowIndex(t, ed, "Content"); again.value != content.value {
		t.Errorf("enter on a free-text row must not change the value: %q -> %q",
			content.value, again.value)
	}
	if ed.editName.Get() != "Content" || ed.editValue.Get() != content.value {
		t.Errorf("enter on a free-text row must load the input, got %q=%q",
			ed.editName.Get(), ed.editValue.Get())
	}
}

// TestEscapeRestoresWhatTheRowHeld. Every editor that writes LIVE — the
// stepper, the colour picker, the track editor, the caret — has already
// changed the document by the time the user decides against it, so esc
// has to put the old value back rather than merely closing.
func TestEscapeRestoresWhatTheRowHeld(t *testing.T) {
	ed, _ := buildPage(t)
	ed.retype("Canvas")
	ed.rebuild()
	ed.sel = ed.doc().Kids[1]

	i, before := rowIndex(t, ed, "Chrome")
	ed.attrSel.Set(i)
	ed.beginEdit()
	ed.props.PreviewKey(input.Named(input.KeyDown))
	ed.props.PreviewKey(input.Named(input.KeyEnter)) // commit something else
	_, changed := rowIndex(t, ed, "Chrome")
	if changed.value == before.value {
		t.Fatal("fixture: the value did not move, so esc has nothing to undo")
	}

	// Re-open, move, then esc.
	ed.beginEdit()
	ed.props.PreviewKey(input.Named(input.KeyDown))
	ed.props.PreviewKey(input.Named(input.KeyEnter))
	ed.beginEdit()
	held := ed.props.undo
	ed.props.Write("Rounded")
	ed.props.PreviewKey(input.Named(input.KeyEsc))
	if _, back := rowIndex(t, ed, "Chrome"); back.value != held {
		t.Errorf("esc left the row at %q; it held %q when the editor opened", back.value, held)
	}
	if ed.props.Mode() != editNone {
		t.Error("esc did not close the editor")
	}
}

// TestTheCycleOffersUnsetOnlyWhereUnsetIsLegal — an optional attribute
// that could be set and never cleared would make the cycling editor a
// one-way door, and a required one has no unset state at all: removing it
// is a load error.
func TestTheCycleOffersUnsetOnlyWhereUnsetIsLegal(t *testing.T) {
	ed, _ := buildPage(t)
	ed.retype("Canvas")
	ed.rebuild()
	ed.sel = ed.doc().Kids[1]

	_, chrome := rowIndex(t, ed, "Chrome") // optional KindEnum
	if !contains(chrome.cycle(), "") {
		t.Errorf("an optional row's cycle must include unset, got %v", chrome.cycle())
	}
	if len(chrome.cycle()) != len(chrome.values)+1 {
		t.Errorf("unset must be added exactly once: %v vs %v", chrome.cycle(), chrome.values)
	}

	// The required half is asserted SYNTHETICALLY, because the vocabulary
	// currently contains no instance of it — every required attribute is
	// free text or a binding, so a sweep over the real elements checks
	// zero rows and passes for that reason alone.
	//
	// Writing the sweep and reading its green would have been a check that
	// reported success without having checked, which is the defect this
	// project has now hit four times. So the rule gets a constructed case,
	// and the sweep below merely reports the population it found.
	req := attrRow{name: "Synthetic", req: true, values: []string{"a", "b"}}
	if contains(req.cycle(), "") {
		t.Errorf("a required row's cycle must not offer unset, got %v", req.cycle())
	}
	if len(req.cycle()) != 2 {
		t.Errorf("a required row cycles exactly its values, got %v", req.cycle())
	}

	// The population, reported rather than asserted. If this ever stops
	// being zero the synthetic case above has a real counterpart and the
	// sweep starts carrying weight on its own.
	found := 0
	for _, spec := range ed.palette {
		for _, a := range markup.AttrsFor(spec, "Canvas") {
			r := attrRow{name: a.Name, req: a.Required, values: ed.valueSet(a)}
			if !a.Required || len(r.values) == 0 {
				continue
			}
			found++
			if contains(r.cycle(), "") {
				t.Errorf("<%s %s> is required; its cycle must not offer unset", spec.Name, a.Name)
			}
		}
	}
	t.Logf("required finite-valued rows in the live vocabulary: %d "+
		"(zero is why the rule above is asserted synthetically)", found)
}

// TestEveryOfferedValueProducesMarkupThatBuilds is the one that matters.
//
// The editor supplies these values, so a value the loader rejects would
// be the editor handing the user a load error out of its own list — the
// catalog lying about the target, which is the defect this whole project
// exists to remove.
//
// It is driven through the REAL GESTURE — open, arrow down j times,
// enter — rather than by calling a commit helper, because the previous
// version of this test survived the move from cycling to a dropdown by
// passing vacuously: beginEdit no longer changed anything, the status
// line stayed green, and the loop reported success having committed
// nothing. The lap count below is the discrimination half.
func TestEveryOfferedValueProducesMarkupThatBuilds(t *testing.T) {
	ed, _ := buildPage(t)
	ed.retype("Canvas")
	ed.rebuild()

	laps, committed := 0, 0
	for _, sel := range []int{0, 1} {
		ed.sel = ed.doc().Kids[sel]
		for _, r := range ed.attrRows() {
			cyc := r.cycle()
			if len(cyc) == 0 {
				continue
			}
			for j := range cyc {
				i, cur := rowIndex(t, ed, r.name)
				ed.attrSel.Set(i)
				ed.beginEdit()
				if ed.props.Mode() == editNone {
					t.Fatalf("<%s %s> (kind %s) offers %d values and no editor opened",
						ed.doc().Kids[sel].Elem, r.name, r.kind, len(cyc))
				}
				// The dropdown opens on the CURRENT value, the way a
				// property grid does, so "press down j times" is not
				// where option j is. Walking until the cursor is on it
				// keeps the gesture real — every step is a dispatched
				// arrow, including the wrap — while staying
				// deterministic about which option gets committed.
				want := cyc[j]
				for k := 0; ed.props.pick.Get() != j; k++ {
					if k > len(cyc) {
						t.Fatalf("the dropdown cursor never reached option %d of %d on <%s %s>",
							j, len(cyc), ed.doc().Kids[sel].Elem, r.name)
					}
					ed.props.PreviewKey(input.Named(input.KeyDown))
				}
				ed.props.PreviewKey(input.Named(input.KeyEnter))
				_, now := rowIndex(t, ed, r.name)
				laps++
				if now.value == want {
					committed++
				} else {
					t.Errorf("choosing option %d of <%s %s> committed %q, want %q",
						j, ed.doc().Kids[sel].Elem, r.name, now.value, want)
				}
				if s := ed.status.Get(); strings.HasPrefix(s, "✗") {
					t.Errorf("setting <%s %s> from %q to %q does not build: %s",
						ed.doc().Kids[sel].Elem, r.name, cur.value, now.value, s)
				}
			}
		}
	}
	if laps == 0 {
		t.Fatal("no finite-valued rows were exercised: the test would prove nothing")
	}
	if committed != laps {
		t.Fatalf("only %d of %d options were actually committed; the rest were a green "+
			"result from a gesture that did nothing", committed, laps)
	}
	t.Logf("options committed through the dropdown: %d", laps)
}

// TestCommandRowsOfferOnlyActions — a Click= offered a name that is not a
// command would produce a load error from a list the editor supplied.
func TestCommandRowsOfferOnlyActions(t *testing.T) {
	ed := newEditor(editorFS())
	offered := ed.commandBindings()
	if len(offered) == 0 {
		t.Fatal("no command bindings offered: the test would prove nothing")
	}
	for _, spelling := range offered {
		name := strings.TrimSuffix(strings.TrimPrefix(spelling, "{{."), "}}")
		v, ok := ed.docCtx.Values[name]
		if !ok {
			t.Errorf("%s is offered but not bindable", spelling)
			continue
		}
		if _, isAction := v.(gooey.Action); !isAction {
			t.Errorf("%s is offered as a command but is a %T", spelling, v)
		}
	}
	// The converse. Without it the test passes for an implementation that
	// offers nothing at all.
	for name, v := range ed.docCtx.Values {
		if _, isAction := v.(gooey.Action); isAction {
			continue
		}
		if contains(offered, "{{."+name+"}}") {
			t.Errorf("%s is a %T, not an Action, and must not be offered as a command", name, v)
		}
	}
}

// TestStyleRowsComeFromTheLiveContext — a hardcoded style list would
// offer names the app does not have and omit the ones it does.
func TestStyleRowsComeFromTheLiveContext(t *testing.T) {
	ed := newEditor(editorFS())
	got := ed.valueSet(markup.AttrSpec{Kind: markup.KindStyle})
	for name := range ed.docCtx.Styles {
		if !contains(got, name) {
			t.Errorf("registered style %q is not offered", name)
		}
	}
	ed.docCtx.Styles["invented"] = render.Style{}
	if !contains(ed.valueSet(markup.AttrSpec{Kind: markup.KindStyle}), "invented") {
		t.Error("a style registered after startup must be offered: " +
			"the list is the live table, not a snapshot")
	}
}

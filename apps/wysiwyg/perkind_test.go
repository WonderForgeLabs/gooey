package main

import (
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
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

// TestEnterCyclesFiniteValuesAndTypesEverythingElse pins the dispatch
// that IS the per-Kind editor. A row with a finite value set advances; a
// free-text row loads the input and changes nothing.
func TestEnterCyclesFiniteValuesAndTypesEverythingElse(t *testing.T) {
	ed := newEditor(editorFS())
	ed.retype("Canvas")
	ed.rebuild()
	ed.sel = ed.doc().Kids[1] // the Button

	// KindEnum: Chrome. Enter must move it.
	i, before := rowIndex(t, ed, "Chrome")
	ed.attrSel.Set(i)
	ed.beginEdit()
	_, after := rowIndex(t, ed, "Chrome")
	if after.value == before.value {
		t.Fatalf("enter on a KindEnum row must advance the value, stayed %q", before.value)
	}
	if !contains(after.cycle(), after.value) {
		t.Fatalf("cycled to %q, which is not in the cycle %v", after.value, after.cycle())
	}

	// KindText: Content. Enter must load the input and leave the document
	// alone — typing is the editor for free text.
	j, content := rowIndex(t, ed, "Content")
	ed.attrSel.Set(j)
	ed.beginEdit()
	if _, again := rowIndex(t, ed, "Content"); again.value != content.value {
		t.Errorf("enter on a free-text row must not change the value: %q -> %q",
			content.value, again.value)
	}
	if ed.editName.Get() != "Content" || ed.editValue.Get() != content.value {
		t.Errorf("enter on a free-text row must load the input, got %q=%q",
			ed.editName.Get(), ed.editValue.Get())
	}
}

// TestTheCycleOffersUnsetOnlyWhereUnsetIsLegal — an optional attribute
// that could be set and never cleared would make the cycling editor a
// one-way door, and a required one has no unset state at all: removing it
// is a load error.
func TestTheCycleOffersUnsetOnlyWhereUnsetIsLegal(t *testing.T) {
	ed := newEditor(editorFS())
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

// TestEveryCycledValueProducesMarkupThatBuilds is the one that matters.
// The editor supplies these values, so a value the loader rejects would
// be the editor handing the user a load error out of its own list — the
// catalog lying about the target, which is the defect this whole project
// exists to remove.
func TestEveryCycledValueProducesMarkupThatBuilds(t *testing.T) {
	ed := newEditor(editorFS())
	ed.retype("Canvas")
	ed.rebuild()

	laps := 0
	for _, sel := range []int{0, 1} {
		ed.sel = ed.doc().Kids[sel]
		for _, r := range ed.attrRows() {
			cyc := r.cycle()
			if len(cyc) == 0 {
				continue
			}
			for range cyc { // one full lap, whatever the starting point
				i, cur := rowIndex(t, ed, r.name)
				ed.attrSel.Set(i)
				ed.beginEdit()
				_, now := rowIndex(t, ed, r.name)
				laps++
				if s := ed.status.Get(); strings.HasPrefix(s, "✗") {
					t.Errorf("cycling <%s %s> from %q to %q does not build: %s",
						ed.doc().Kids[sel].Elem, r.name, cur.value, now.value, s)
				}
			}
		}
	}
	if laps == 0 {
		t.Fatal("no finite-valued rows were cycled: the test would prove nothing")
	}
	t.Logf("values cycled through: %d", laps)
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

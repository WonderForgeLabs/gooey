package main

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/markup"
)

// THE EXHAUSTIVENESS PIN.
//
// "Every Kind in the catalog must resolve to an editor — no default
// case" is only a rule if something fails when it is broken, and the
// thing that has to fail is the arrival of a NEW Kind. A test that walks
// the editors table and checks each entry is well-formed passes happily
// for a table missing half the vocabulary; a test that walks the KINDS
// and looks each one up is the one that goes red.
//
// So the list of Kinds is DERIVED, from the const block in
// markup/catalog.go that declares them. Not from a slice written here —
// that is the same hand-maintained list wearing a smaller hat, and it
// would be stale the first time somebody added a Kind without touching
// this file, which is exactly the moment the test exists for. Not from
// reflection either: this repo has none, and a property browser is the
// last place to introduce it.
//
// Reading the source is the technique the root module already uses to
// keep CLAUDE.md and ci.yml from drifting. The path is relative because
// this module sits two levels down and is always run from its own
// directory.
const catalogPath = "../../markup/catalog.go"

var kindDecl = regexp.MustCompile(`(?m)^\s*Kind(\w+)\s+Kind\s*=\s*"([^"]+)"`)

// declaredKinds is every markup.Kind constant, read out of the file that
// declares them, as (Go constant name, wire value) pairs.
func declaredKinds(t *testing.T) map[string]markup.Kind {
	t.Helper()
	src, err := os.ReadFile(catalogPath)
	if err != nil {
		t.Fatalf("cannot read the Kind declarations: %v", err)
	}
	out := map[string]markup.Kind{}
	for _, m := range kindDecl.FindAllStringSubmatch(string(src), -1) {
		out["Kind"+m[1]] = markup.Kind(m[2])
	}
	return out
}

func TestEveryDeclaredKindHasAnEditor(t *testing.T) {
	kinds := declaredKinds(t)

	// The discrimination half, and it is not a formality: a regex that
	// silently stopped matching would return an empty map and this test
	// would report success having checked nothing. The floor is well
	// under the real count so it does not become a second number to
	// maintain — it only has to be high enough that a broken parse
	// cannot slip under it.
	if len(kinds) < 10 {
		t.Fatalf("only %d Kind constants parsed out of %s (%v); the declarations moved and "+
			"this test is checking nothing", len(kinds), catalogPath, kinds)
	}

	var missing []string
	for name, k := range kinds {
		if _, ok := editorForKind(k); !ok {
			missing = append(missing, name+" ("+string(k)+")")
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("%d declared Kind(s) have no editor: %s\n\nEvery Kind must resolve to one — "+
			"a Kind with no entry falls through to nothing on enter, or (worse, if a default "+
			"arm is ever added) into a text box that invites a value the loader rejects. Add "+
			"the arm to `editors` in editors.go.", len(missing), strings.Join(missing, ", "))
	}

	// The converse: an editor mapped from a Kind that no longer exists.
	// Harmless at runtime, but it is a claim about the vocabulary that
	// has quietly stopped being true, and this is the only place that
	// would notice.
	live := map[markup.Kind]bool{}
	for _, k := range kinds {
		live[k] = true
	}
	for k := range editors {
		if !live[k] {
			t.Errorf("editors maps %q, which markup no longer declares", k)
		}
	}
}

// TestEveryKindTheVocabularyActuallyUSESHasAnEditor is the same claim
// asked of the live catalog rather than of the source. It is narrower —
// a Kind declared but unused would pass it — and that is why both exist:
// this one cannot be fooled by a parse, and the one above cannot be
// fooled by a Kind nothing happens to use yet.
func TestEveryKindTheVocabularyActuallyUSESHasAnEditor(t *testing.T) {
	ed := newEditor(editorFS())
	seen, checked := map[markup.Kind]bool{}, 0
	for _, spec := range ed.docCtx.Catalog() {
		for _, a := range markup.AttrsFor(spec, "Canvas") {
			checked++
			if seen[a.Kind] {
				continue
			}
			seen[a.Kind] = true
			if _, ok := editorForKind(a.Kind); !ok {
				t.Errorf("<%s %s> has Kind %q and no editor", spec.Name, a.Name, a.Kind)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no attributes examined; this test asserts nothing")
	}
	t.Logf("attributes examined: %d, distinct Kinds in use: %d", checked, len(seen))
}

// TestEveryEditorHasAnAffordanceAndTheGlyphsAreDistinct.
//
// The affordance column is the user's only warning that enter is about
// to open something. Two editors sharing a glyph would say "these behave
// alike" about a colour picker and a dropdown, and editNone sharing one
// with a real editor would hide the gap the table exists to expose.
func TestEveryEditorHasAnAffordanceAndTheGlyphsAreDistinct(t *testing.T) {
	byGlyph := map[string][]editorKind{}
	for _, e := range editors {
		byGlyph[e.affordance()] = append(byGlyph[e.affordance()], e)
	}
	for glyph, es := range byGlyph {
		if glyph == "" {
			t.Errorf("editors %v render no affordance at all", es)
		}
	}
	// A floating editor and an inline one must not look the same.
	for _, e := range editors {
		for _, o := range editors {
			if e == o || e.affordance() != o.affordance() {
				continue
			}
			if e.floats() != o.floats() {
				t.Errorf("editors %v and %v share the glyph %q but one floats a surface and "+
					"the other does not", e, o, e.affordance())
			}
		}
	}
	// And the unmapped case is visibly wrong on the grid.
	if got := rowAffordance(attrRow{kind: "no-such-kind"}); got != "!" {
		t.Errorf("a row whose Kind has no editor renders %q; it must be visibly wrong", got)
	}
}

// TestGoTypeOfMirrorsPlaceholderFor pins the two tables that must agree.
//
// markup.PlaceholderFor turns a declared GoType into a live handle;
// goTypeOf turns a live handle back into a GoType. The binding picker
// only offers a name when the second answers what the first was asked,
// so a GoType taught to one and not the other makes the picker silently
// offer nothing for that attribute — no error, an empty dropdown, and
// the user concluding the attribute cannot be bound.
func TestGoTypeOfMirrorsPlaceholderFor(t *testing.T) {
	types := markup.PlaceholderTypes()
	if len(types) == 0 {
		t.Fatal("markup declares no placeholder types; this test asserts nothing")
	}
	for _, want := range types {
		h := markup.PlaceholderFor(want)
		if h == nil {
			t.Errorf("markup.PlaceholderTypes lists %q but PlaceholderFor answers nil for it", want)
			continue
		}
		if got := goTypeOf(h); got != want {
			t.Errorf("goTypeOf(PlaceholderFor(%q)) = %q; the two tables have drifted, and the "+
				"binding picker will offer nothing for a %s attribute", want, got, want)
		}
	}
	// The converse: something that is not a handle at all must not be
	// claimed as one, or the picker would offer a command as a binding.
	if got := goTypeOf("a plain string"); got != "" {
		t.Errorf("goTypeOf on a non-handle returned %q", got)
	}
}

// TestTrackSpecsRoundTrip pins lensSpec against the parser markup itself
// uses. The track editor writes with one and the loader reads with the
// other, so a spelling either of them disagrees about is a document that
// stops loading halfway through an edit.
func TestTrackSpecsRoundTrip(t *testing.T) {
	for _, spec := range []string{"Auto", "*", "2*", "10", "Auto,1*,20", "Auto,*,2*,1,999"} {
		ls, err := components.ParseGridLens(spec)
		if err != nil {
			t.Fatalf("fixture %q does not parse: %v", spec, err)
		}
		got := lensSpec(ls)
		back, err := components.ParseGridLens(got)
		if err != nil {
			t.Errorf("lensSpec produced %q from %q, which does not parse: %v", got, spec, err)
			continue
		}
		if len(back) != len(ls) {
			t.Errorf("%q -> %q changed the track count %d -> %d", spec, got, len(ls), len(back))
			continue
		}
		for i := range ls {
			if back[i] != ls[i] {
				t.Errorf("%q -> %q changed track %d: %+v -> %+v", spec, got, i, ls[i], back[i])
			}
		}
	}
}

// TestTrackKindCycleVisitsAllThreeAndKeepsAUsableValue.
//
// A track that becomes Star with weight zero, or Fixed with zero cells,
// takes no space — so the user presses k, the grid silently loses a
// column, and nothing anywhere says why.
func TestTrackKindCycleVisitsAllThreeAndKeepsAUsableValue(t *testing.T) {
	l := components.Fixed(10)
	seen := map[string]bool{}
	for i := 0; i < 3; i++ {
		l = nextLensKind(l)
		seen[lensKind(l)] = true
		if lensKind(l) == "Star" && l.Star <= 0 {
			t.Error("cycling to Star left a zero weight: the track takes no space")
		}
		if lensKind(l) == "Fixed" && l.Fixed <= 0 {
			t.Error("cycling to Fixed left zero cells: the track takes no space")
		}
	}
	for _, want := range []string{"Auto", "Star", "Fixed"} {
		if !seen[want] {
			t.Errorf("the kind cycle never reaches %s: %v", want, seen)
		}
	}
}

// TestAutoTracksRefuseToBeResized. An Auto track has no number, so ◂ and
// ▸ have nothing to move — and the editor has to SAY so rather than
// swallow the key, which reads as the editor being broken.
func TestAutoTracksRefuseToBeResized(t *testing.T) {
	if _, moved := adjustLens(components.Auto(), 1); moved {
		t.Error("an Auto track reported a size change")
	}
	if l, moved := adjustLens(components.Fixed(1), -5); !moved || l.Fixed < 1 {
		t.Errorf("a Fixed track clamped to %+v; a zero-cell track is invisible", l)
	}
	if l, moved := adjustLens(components.Star(1), -5); !moved || l.Star < 1 {
		t.Errorf("a Star track clamped to %+v; a zero-weight track takes no space", l)
	}
}

// TestBindingPickerOffersOnlyHandlesOfTheDeclaredType.
//
// A bind-only attribute takes {{.Path}} and nothing else, and the loader
// checks the handle's element type. Offering every bindable name would
// hand the user a load error out of the editor's own list.
func TestBindingPickerOffersOnlyHandlesOfTheDeclaredType(t *testing.T) {
	ed := newEditor(editorFS())

	// Asked through valueSet, the way the inspector asks it — NOT by
	// calling typedBindings directly. Written that way after a mutation
	// survived: pointing valueSet's KindBinding arm at typedBindings("")
	// left the direct version green, because it proved the filter works
	// without ever proving the filter is what a binding row is given.
	// The fixture has no KindBinding row of its own, so nothing else
	// covers the wiring.
	spec := markup.AttrSpec{Kind: markup.KindBinding, GoType: "string"}
	offered := ed.valueSet(spec)
	if len(offered) == 0 {
		t.Fatal("a KindBinding attribute declaring GoType \"string\" was offered nothing; " +
			"either the picker is empty or valueSet does not route KindBinding to it")
	}
	if direct := ed.typedBindings("string"); len(direct) != len(offered) {
		t.Errorf("valueSet offered %d values for a string binding and typedBindings offered "+
			"%d; the inspector is not asking the picker", len(offered), len(direct))
	}
	for _, spelling := range offered {
		name := strings.TrimSuffix(strings.TrimPrefix(spelling, "{{."), "}}")
		v, ok := ed.docCtx.Values[name]
		if !ok {
			t.Errorf("%s is offered but not bindable", spelling)
			continue
		}
		if got := goTypeOf(v); got != "string" {
			t.Errorf("%s is offered for a string attribute but is a %s handle", spelling, got)
		}
	}
	// The converse. Without it the test passes for an implementation
	// that offers nothing.
	for name, v := range ed.docCtx.Values {
		if goTypeOf(v) == "string" {
			continue
		}
		if contains(offered, "{{."+name+"}}") {
			t.Errorf("%s is a %T and must not be offered for a string attribute", name, v)
		}
	}
	// An attribute whose catalog entry does not declare a Go type gets
	// NOTHING, rather than unchecked offers. "This catalog entry does
	// not say" is the honest answer to "which of these will load?".
	if got := ed.valueSet(markup.AttrSpec{Kind: markup.KindBinding}); got != nil {
		t.Errorf("an attribute with no declared GoType was offered %v", got)
	}
	// And a COMMAND is not a binding: the two Kinds share an editor but
	// not a source, and offering an Action where a typed handle belongs
	// is a load error out of the editor's own list.
	cmds := ed.valueSet(markup.AttrSpec{Kind: markup.KindCommand})
	if len(cmds) == 0 {
		t.Fatal("no commands offered; the converse below would prove nothing")
	}
	for _, c := range cmds {
		if contains(offered, c) {
			t.Errorf("%s is a command and was also offered for a string binding", c)
		}
	}
}

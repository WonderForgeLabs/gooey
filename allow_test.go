package gooey

import (
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey/input"
)

// The Allow lattice itself. Everything here is about the value type — the
// routing it drives is pinned in markup/frozenallow_test.go, against the
// <Frozen> element a page actually writes.

// TestAllowAllIsEveryCategory is the invariant the whole "AllowAll means
// not frozen" identity rests on: if any category sat outside AllowAll,
// then a component that does not freeze would be withholding it, and
// isFrozen would be true for everything in the tree.
//
// It is derived rather than restated — AllowNames is the vocabulary, and
// the loop asks it — so a category added without extending the bit range
// fails here rather than at some routing call site months later.
func TestAllowAllIsEveryCategory(t *testing.T) {
	for _, name := range AllowNames() {
		cat, err := ParseAllow(name)
		if err != nil {
			t.Fatalf("ParseAllow(%q): %v", name, err)
		}
		if !AllowAll.Has(cat) {
			t.Errorf("AllowAll does not contain %q; a category outside AllowAll makes "+
				"every unfrozen component withhold it", name)
		}
	}
	if AllowNone.Has(AllowFocus) {
		t.Error("AllowNone contains Focus, so the empty set is not empty")
	}
}

// TestEveryKeyClassCarriesFocus is the closure rule, and it is the whole
// reason the exported constants are composite masks rather than raw bits.
//
// A key class without Focus is a spelling that does nothing: nothing
// inside a subtree with no focus stops can ever be focused, so no key
// routes there. Making the constants closed means no order of
// composition — ParseAllow, sets:Concat, Go code — can produce one.
func TestEveryKeyClassCarriesFocus(t *testing.T) {
	classes := map[string]Allow{
		"Alpha": AllowAlpha, "Numeric": AllowNumeric, "Punct": AllowPunct,
		"Space": AllowSpace, "Nav": AllowNav, "Edit": AllowEdit,
		"Escape": AllowEscape, "Chords": AllowChords, "Bindings": AllowBindings,
	}
	for name, cat := range classes {
		if !cat.Has(AllowFocus) {
			t.Errorf("Allow%s does not carry Focus, so writing it alone permits nothing", name)
		}
	}
	// The discriminating half: the categories that are NOT routed through
	// focus must not carry it, or the distinction is not being made at all
	// and the test above passes vacuously.
	for name, cat := range map[string]Allow{
		"Mnemonics": AllowMnemonics, "Pointer": AllowPointer,
		"Hover": AllowHover, "Start": AllowStart,
	} {
		if cat.Has(AllowFocus) {
			t.Errorf("Allow%s carries Focus; it is not routed through focus and must "+
				"not widen into it", name)
		}
	}
}

func TestAllowRoundTripsThroughItsText(t *testing.T) {
	for _, a := range []Allow{
		AllowNone, AllowAll, AllowFocus, AllowText, AllowKeys, AllowMouse,
		AllowMnemonics | AllowStart,
		AllowAlpha | AllowNumeric | AllowHover,
	} {
		got, err := ParseAllow(a.String())
		if err != nil {
			t.Fatalf("ParseAllow(%q): %v", a.String(), err)
		}
		if got != a {
			t.Errorf("%q parsed back to %q, want the same set", a.String(), got.String())
		}
	}
	if AllowNone.String() != "None" {
		t.Errorf("the empty set renders as %q; it must not render as the empty string, "+
			"which is how markup says an attribute was not written", AllowNone.String())
	}
}

func TestParseAllowAcceptsCommasSpacesAndAnyOrder(t *testing.T) {
	want := AllowHover | AllowPointer | AllowStart
	for _, s := range []string{
		"Hover Pointer Start", "Start,Pointer,Hover", " Hover , Start   Pointer ",
		"Mouse Start",
	} {
		got, err := ParseAllow(s)
		if err != nil {
			t.Fatalf("ParseAllow(%q): %v", s, err)
		}
		if got != want {
			t.Errorf("ParseAllow(%q) = %q, want %q", s, got, want)
		}
	}
}

func TestAnUnknownCategoryIsAnErrorNamingTheVocabulary(t *testing.T) {
	_, err := ParseAllow("Focus Clicks")
	if err == nil {
		t.Fatal("ParseAllow accepted an unknown category; a silently ignored token is a "+
			"surface that is mysteriously sealed", err)
	}
	if !strings.Contains(err.Error(), "Clicks") {
		t.Errorf("the error does not name the bad token: %v", err)
	}
	if !strings.Contains(err.Error(), "Pointer") {
		t.Errorf("the error does not list the vocabulary: %v", err)
	}
	// Fail closed, not partially open: the categories that DID parse must
	// not survive the failure.
	got, _ := ParseAllow("Focus Clicks")
	if got != AllowNone {
		t.Errorf("a failed parse returned %q; it must return the empty set", got)
	}
}

func TestAllowForClassifiesKeys(t *testing.T) {
	cases := []struct {
		ev   input.KeyEvent
		want Allow
	}{
		{input.Rune('a'), AllowAlpha},
		{input.Rune('Z'), AllowAlpha},
		{input.Rune('7'), AllowNumeric},
		{input.Rune(' '), AllowSpace},
		{input.Rune('!'), AllowPunct},
		{input.Named(input.KeyTab), AllowNav},
		{input.KeyEvent{Key: input.KeyTab, Mods: input.ModShift}, AllowNav},
		{input.Named(input.KeyUp), AllowNav},
		{input.Named(input.KeyPageDown), AllowNav},
		{input.Named(input.KeyEnter), AllowEdit},
		{input.Named(input.KeyBackspace), AllowEdit},
		{input.Named(input.KeyEsc), AllowEscape},
		// A chord is a chord whatever the key is. This is the row that
		// makes "let the user type" safe: without it, Alpha would admit
		// ctrl+s and a read-only preview would save the document.
		{input.KeyEvent{Key: input.KeyRune, Rune: 's', Mods: input.ModCtrl}, AllowChords},
		{input.KeyEvent{Key: input.KeyRune, Rune: 'g', Mods: input.ModAlt}, AllowChords},
		{input.KeyEvent{Key: input.KeyEnter, Mods: input.ModCtrl}, AllowChords},
	}
	for _, c := range cases {
		if got := AllowFor(c.ev); got != c.want {
			t.Errorf("AllowFor(%v) = %q, want %q", c.ev, got, c.want)
		}
	}
	// Shift is NOT a chord: terminals cannot report it on printable
	// characters at all, and shift+tab is navigation.
	if got := AllowFor(input.KeyEvent{Key: input.KeyTab, Mods: input.ModShift}); got != AllowNav {
		t.Errorf("shift+tab classified as %q, want Nav", got)
	}
}

func TestIntersectIsWhatNestingDoes(t *testing.T) {
	outer := AllowHover
	inner := AllowPointer | AllowHover
	if got := outer.Intersect(inner); got != AllowHover {
		t.Errorf("nesting gave %q, want %q: an inner host must not hand out permission "+
			"its container withheld", got, AllowHover)
	}
	if got := AllowNone.Intersect(AllowAll); got != AllowNone {
		t.Errorf("AllowNone ∩ AllowAll = %q, want None: the bool case must degenerate "+
			"exactly", got)
	}
}

func TestSortAllowNamesIsStableAndKeepsUnknownNames(t *testing.T) {
	names := []string{"zzz", "Pointer", "Focus", "aaa", "Alpha"}
	SortAllowNames(names)
	want := []string{"Focus", "Alpha", "Pointer", "aaa", "zzz"}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("SortAllowNames = %v, want %v: known names in vocabulary order, "+
				"unknown ones after, alphabetically — a generic set pack must not lose "+
				"a token it does not recognize", names, want)
		}
	}
}

// TestFrozenAllowProjectsTheBoolInterface pins the compatibility claim:
// an implementer of the OLD interface must land on the two endpoints of
// the lattice, and isFrozen must stay its exact projection.
func TestFrozenAllowProjectsTheBoolInterface(t *testing.T) {
	if got := frozenAllow(&boolFrozen{v: true}); got != AllowNone {
		t.Errorf("a bool Frozen()==true answered %q, want None", got)
	}
	if got := frozenAllow(&boolFrozen{v: false}); got != AllowAll {
		t.Errorf("a bool Frozen()==false answered %q, want All", got)
	}
	if got := frozenAllow(&plainLeaf{}); got != AllowAll {
		t.Errorf("a component that does not implement Frozen answered %q, want All", got)
	}
	if !isFrozen(&boolFrozen{v: true}) || isFrozen(&boolFrozen{v: false}) || isFrozen(&plainLeaf{}) {
		t.Error("isFrozen is no longer the bool projection of frozenAllow")
	}
	// A set host that is not Active answers AllowAll however permissive
	// its set is — the "not frozen" endpoint is reachable from either
	// interface.
	if got := frozenAllow(&setFrozen{active: false, allow: AllowNone}); got != AllowAll {
		t.Errorf("an inactive FrozenAllows answered %q, want All", got)
	}
	if got := frozenAllow(&setFrozen{active: true, allow: AllowHover}); got != AllowHover {
		t.Errorf("an active FrozenAllows answered %q, want Hover", got)
	}
}

type plainLeaf struct{ Base }

func (p *plainLeaf) Measure(Size) Size { return Size{} }
func (p *plainLeaf) Render(*Frame)     {}

type boolFrozen struct {
	plainLeaf
	v bool
}

func (b *boolFrozen) Frozen() bool { return b.v }

type setFrozen struct {
	plainLeaf
	active bool
	allow  Allow
	// reads counts FrozenAllow calls, so the hoisting test below can see
	// whether the read happened on a frame where the answer did not need
	// it.
	reads int
}

func (s *setFrozen) Frozen() bool { return s.active }
func (s *setFrozen) FrozenAllow() Allow {
	s.reads++
	return s.allow
}

// TestFrozenAllowReadsBothMethodsEvenWhenInactive is the dependency-set
// guard, and it is the one assertion here that is about WHEN a read
// happens rather than what it returns.
//
// frozenAllow runs inside Composer.armFrozen's computed, so a Get behind
// an early return drops out of the dependency set on the frames where it
// does not execute. Reading FrozenAllow() only when Frozen() came back
// true would leave the observer deaf to a change in the allow set on
// exactly the frames where the answer is about to start mattering.
func TestFrozenAllowReadsBothMethodsEvenWhenInactive(t *testing.T) {
	h := &setFrozen{active: false, allow: AllowHover}
	if got := frozenAllow(h); got != AllowAll {
		t.Fatalf("an inactive host answered %q, want All", got)
	}
	if h.reads != 1 {
		t.Errorf("frozenAllow called FrozenAllow() %d times on an inactive host, want 1: "+
			"a read behind the branch is a dropped dependency", h.reads)
	}
}

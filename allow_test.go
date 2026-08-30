package gooey

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
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
	// The projection itself, spelled out rather than delegated to a
	// helper. There used to be an isFrozen(w) — `frozenAllow(w) !=
	// AllowAll` — whose only caller was this line: a function that
	// exists to be asserted equal to its own body proves nothing and
	// reads as API. The CLAIM is worth keeping, so it is written here.
	for _, tc := range []struct {
		name   string
		w      Component
		frozen bool
	}{
		{"Frozen()==true", &boolFrozen{v: true}, true},
		{"Frozen()==false", &boolFrozen{v: false}, false},
		{"no Frozen at all", &plainLeaf{}, false},
	} {
		if got := frozenAllow(tc.w) != AllowAll; got != tc.frozen {
			t.Errorf("%s: withholds-anything = %v, want %v", tc.name, got, tc.frozen)
		}
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

// TestAllowForClassifiesEveryDeclaredKey derives the list of keys from
// the file that declares them, rather than agreeing with a list written
// beside the switch.
//
// AllowFor used to end in `return AllowPunct`, and its test walked the
// same hand-written set of keys the switch did — so the two agreed by
// construction and neither could see a key that was in neither. An
// input.Key added later (a function key, insert, a keypad) would have
// been silently classified as punctuation, which is the category a
// text-entry surface is most likely to have granted: a frozen preview
// that let typing through would have let the new key through too, and
// nothing would go red about it.
//
// So the source is the authority. Adding a Key to input/key.go now
// fails here until somebody decides what class it belongs to.
func TestAllowForClassifiesEveryDeclaredKey(t *testing.T) {
	// PARSED, AND ANCHORED TO THE iota BLOCK, not matched by shape.
	//
	// This read the names with `(?m)^\t(Key[A-Z]\w*)`, which matches any
	// tab-indented identifier starting with Key anywhere in the file —
	// and the loop below maps the i'th name to input.Key(i). Today the
	// file holds exactly the iota constants and nothing else that
	// matches, so the mapping happens to be right. The ordinary
	// companion to a key enum would end that quietly:
	//
	//	var keyNames = map[Key]string{
	//		KeyEnter: "enter",
	//		...
	//	}
	//
	// Every match after the const block shifts the positional mapping,
	// and the test starts asserting about the wrong Key values. The len
	// floor below cannot notice — it only sees the list shrink.
	//
	// Checked rather than assumed, and what it does is worth recording:
	// with such a map added, the OLD regex reported KeyEnter, KeyTab and
	// KeyEsc as reaching no arm of AllowFor — three keys that are
	// classified, named by a test that had walked off the end of the
	// enum into the fallback. So the failure mode here is a MISLEADING
	// RED rather than the silent green a shift usually buys, which is
	// worse in its own way: it sends the reader to allow.go to fix three
	// keys that were never broken.
	//
	// The regex survived this long by an accident of style. input/key.go
	// already carries a companion table — `var keyNames = []struct{…}`,
	// whose lines begin `\t{KeyEnter,` — and the leading brace is the
	// only reason it never matched. A map literal, the more usual
	// spelling, would have. Found in the review of PR #425.
	//
	// go/ast reads the block that actually defines the enum: the const
	// declaration whose first spec is `… Key = iota`. Nothing outside it
	// can be mistaken for a member.
	names := declaredKeyConstants(t)
	// Discrimination: a walk that stopped finding the block would leave
	// this empty and the loop below would check nothing. The floor is
	// well under the real count so it does not become a second number to
	// maintain.
	if len(names) < 10 {
		t.Fatalf("only %d Key constants parsed out of input/key.go (%v); "+
			"the declarations moved and this test is checking nothing",
			len(names), names)
	}

	// The constants are an iota block starting at KeyRune, so the i'th
	// name is input.Key(i). Taken from the ORDER in the source for the
	// same reason as the names: a map written here would be the hand
	// list again.
	for i, name := range names {
		ev := input.KeyEvent{Key: input.Key(i)}
		if input.Key(i) == input.KeyRune {
			ev.Rune = 'a' // KeyRune is classified by its rune
		}
		if _, ok := allowForKey(ev); !ok {
			t.Errorf("input.%s reaches no arm of AllowFor, so it falls "+
				"through to AllowNone and cannot pass any Frozen surface. "+
				"Give it a class in allow.go.", name)
		}
	}

	// And the fallback must still BE a fallback: a key past the end of
	// the block is unknown, and an unknown key must not ride in on a
	// permission the author granted for something else.
	unknown := input.KeyEvent{Key: input.Key(len(names) + 50)}
	if got, ok := allowForKey(unknown); ok {
		t.Errorf("an undeclared Key was classified as %v; the fallback is "+
			"supposed to fail closed", got)
	}
	if got := AllowFor(unknown); got != AllowNone {
		t.Errorf("AllowFor gave an undeclared Key %v, want AllowNone", got)
	}
}

// declaredKeyConstants is the names of input.Key's iota block, in
// declaration order, so the i'th is input.Key(i).
//
// It finds the block by its DEFINITION — a const declaration whose first
// spec has type Key and value iota — rather than by what its lines look
// like. See TestAllowForClassifiesEveryDeclaredKey for what the
// shape-matching version got wrong.
func declaredKeyConstants(t *testing.T) []string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filepath.Join("input", "key.go"), nil, 0)
	if err != nil {
		t.Fatalf("parsing input/key.go: %v", err)
	}
	for _, d := range f.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST || len(gd.Specs) == 0 {
			continue
		}
		first, ok := gd.Specs[0].(*ast.ValueSpec)
		if !ok {
			continue
		}
		id, ok := first.Type.(*ast.Ident)
		if !ok || id.Name != "Key" {
			continue
		}
		if len(first.Values) != 1 {
			continue
		}
		if v, ok := first.Values[0].(*ast.Ident); !ok || v.Name != "iota" {
			continue
		}
		var names []string
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for _, n := range vs.Names {
				names = append(names, n.Name)
			}
		}
		return names
	}
	t.Fatal("no `const ( … Key = iota )` block found in input/key.go — the " +
		"declarations moved and nothing below is checking the real enum")
	return nil
}

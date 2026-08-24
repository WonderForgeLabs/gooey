package gooey

import (
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/WonderForgeLabs/gooey/input"
)

// Allow is what a frozen subtree still lets through: a SET of interaction
// categories, held as a bitmask.
//
// # Why freezing needed a set at all
//
// Frozen's original answer was a bool, and "renders but does not act" is
// the right default for a preview. It is the wrong shape for the thing
// that motivated freezing in the first place — a design surface. An
// editor that freezes its canvas cannot let ANYTHING through, so the
// resize handles on the selected element have to be drawn by the editor
// and hit-tested by the editor, outside the tree they decorate. Widening
// the answer from "does it act" to "what still acts" is what lets a
// handle be an ordinary component inside the picture.
//
// # Why a bitmask rather than a map or a slice
//
// Composer.armFrozen wraps the answer in a prop.NewComputed and the
// per-frame sweep compares this frame's answer against last frame's to
// decide whether the composition must be re-synced. That comparison has
// to be cheap, total and exact. A bitmask is comparable with ==, so the
// sweep stays one instruction and the "flip" test stays honest for a
// change from {Pointer} to {Pointer, Hover} — which really is structural,
// because it changes the hover-watcher registrations. A map or a slice is
// not comparable, would have needed a deep compare per node per frame,
// and would have put the sweep's correctness in the hands of whoever
// built the set.
//
// # The lattice, and where "not frozen" lives in it
//
// AllowAll is a member of this lattice and means "not frozen": a
// component that does not implement Frozen answers AllowAll, and
// isFrozen is exactly `allow != AllowAll`. That is what keeps ONE
// observed value per node instead of two that can disagree — the bool
// interface is now a projection of the set, not a parallel fact.
//
// # Composition is union, and the exported constants are CLOSED
//
// Sets compose by union: naming more categories permits strictly more.
// The exported constants are therefore not raw bits — several of them
// carry an implication, so that a union of ergonomic names cannot
// produce a set that is silently a no-op.
//
// AllowAlpha and every other key class include AllowFocus, because a key
// that reaches nothing is not an allowance. Nothing inside a subtree
// whose focus stops were withheld can ever be focused, so keys never
// route there and the "allowance" would be a spelling that does nothing —
// the exact trap this vocabulary exists to avoid. AllowBindings carries
// the same implication for the same reason: a scoped KeyBinding fires
// only while the focused chain passes through its host.
//
// AllowMnemonics deliberately does NOT imply AllowFocus, and that is the
// distinction that earns it a category of its own: Dispatch offers every
// mnemonic to every MnemonicHandler in the tree no matter what holds
// focus, so a mnemonic is reachable inside a subtree that is otherwise
// completely sealed. Neither do AllowPointer, AllowHover or AllowStart —
// none of the three is routed through focus.
//
// Because the implications live in the CONSTANTS rather than in a
// normalizing pass, union is closed by construction: there is no order of
// composition, and no path through ParseAllow, sets:Concat or Go code,
// that yields a key class without its focus bit.
type Allow uint32

// The raw bits. They are unexported because an exported bit could be
// OR'd into a mask without its implication — see the closure argument on
// Allow. Everything outside this file composes the exported constants.
const (
	bitFocus Allow = 1 << iota
	bitAlpha
	bitNumeric
	bitPunct
	bitSpace
	bitNav
	bitEdit
	bitEscape
	bitChords
	bitBindings
	bitMnemonics
	bitPointer
	bitHover
	bitStart

	bitLast // not a category: the end marker AllowAll derives from
)

// The category vocabulary. These are the values a host composes, the
// names markup writes, and the names handlers/sets unions.
const (
	// AllowNone is a fully frozen subtree: today's Frozen() == true.
	AllowNone Allow = 0

	// AllowFocus makes descendants focus stops again — tab, shift+tab and
	// spatial arrow navigation reach them, and SetFocus into the subtree
	// succeeds. It is the gate every key category depends on.
	AllowFocus = bitFocus

	// The key classes. Each names which KEYS reach a focused descendant,
	// and each implies AllowFocus.
	AllowAlpha   = bitAlpha | bitFocus   // unmodified letters
	AllowNumeric = bitNumeric | bitFocus // unmodified digits
	AllowPunct   = bitPunct | bitFocus   // other unmodified printable runes
	AllowSpace   = bitSpace | bitFocus   // the space bar
	AllowNav     = bitNav | bitFocus     // tab, arrows, home/end, page up/down
	AllowEdit    = bitEdit | bitFocus    // enter, backspace, delete
	AllowEscape  = bitEscape | bitFocus  // esc
	AllowChords  = bitChords | bitFocus  // anything held with ctrl or alt

	// AllowBindings lets scoped KeyBindings attached inside the subtree
	// fire. Implies AllowFocus, which is what makes it reachable.
	AllowBindings = bitBindings | bitFocus

	// AllowMnemonics lets mnemonics declared inside the subtree fire.
	// Page-scoped, so it needs no focus.
	AllowMnemonics = bitMnemonics

	// AllowPointer lets press, release, click, motion, capture and the
	// wheel reach descendants instead of being retargeted to the host.
	AllowPointer = bitPointer

	// AllowHover lets hover state and HoverWatchers track descendants.
	AllowHover = bitHover

	// AllowStart lets Startables inside the subtree start. It is the one
	// category with a safety argument rather than a UX one — see
	// Composer.collect, where Companion.Start spawns a child process —
	// so it is never implied by anything and must always be asked for by
	// name.
	AllowStart = bitStart
)

// The groups: ergonomic unions, spelled the same way in markup.
const (
	// AllowText is what a form needs to stay typeable inside a picture.
	AllowText = AllowAlpha | AllowNumeric | AllowPunct | AllowSpace
	// AllowKeys is every key class, chords included.
	AllowKeys = AllowText | AllowNav | AllowEdit | AllowEscape | AllowChords
	// AllowMouse is the pointer and the hover that goes with it.
	AllowMouse = AllowPointer | AllowHover
	// AllowAll is not frozen at all. isFrozen is `allow != AllowAll`, so
	// <Frozen Allow="All"> and no <Frozen> at all mean the same thing —
	// which is correct, and is why the two are one value rather than two.
	AllowAll = bitLast - 1
)

// Has reports whether a permits everything cat names. Set containment,
// not bit overlap: Has(AllowAlpha) asks for the alpha bit AND the focus
// bit, which is what makes the closed constants above mean anything.
func (a Allow) Has(cat Allow) bool { return a&cat == cat }

// Union is the composition rule, spelled out so callers do not have to
// know the representation.
func (a Allow) Union(b Allow) Allow { return a | b }

// Intersect is what NESTING does. A frozen host inside another frozen
// host cannot hand out permission its own container withheld, so the
// effective set at any depth is the intersection down the chain — the
// set form of frozenHostFor's "outermost wins".
func (a Allow) Intersect(b Allow) Allow { return a & b }

// allowNames is the vocabulary, in canonical order. Order is the order
// String emits, so it is declaration order rather than alphabetical:
// reading `Focus Alpha Numeric` groups the key classes together.
//
// The table is the single source of truth for every consumer — markup's
// <Frozen Allow>, handlers/sets' inventory, and the error text for an
// unknown name all derive from it, so none of them can drift from the
// constants above.
var allowNames = []struct {
	name string
	cat  Allow
}{
	{"Focus", AllowFocus},
	{"Alpha", AllowAlpha},
	{"Numeric", AllowNumeric},
	{"Punct", AllowPunct},
	{"Space", AllowSpace},
	{"Nav", AllowNav},
	{"Edit", AllowEdit},
	{"Escape", AllowEscape},
	{"Chords", AllowChords},
	{"Bindings", AllowBindings},
	{"Mnemonics", AllowMnemonics},
	{"Pointer", AllowPointer},
	{"Hover", AllowHover},
	{"Start", AllowStart},
}

// allowGroups are the unions, kept apart from allowNames because String
// emits primitives only: rendering `Text` would be a second spelling of
// the same set and Parse(String(x)) == x would depend on which one won.
var allowGroups = []struct {
	name string
	cat  Allow
}{
	{"None", AllowNone},
	{"Text", AllowText},
	{"Keys", AllowKeys},
	{"Mouse", AllowMouse},
	{"All", AllowAll},
}

// AllowNames lists every category name, primitives then groups. It is the
// inventory a palette shows and the list an error message quotes.
func AllowNames() []string {
	out := make([]string, 0, len(allowNames)+len(allowGroups))
	for _, n := range allowNames {
		out = append(out, n.name)
	}
	for _, g := range allowGroups {
		out = append(out, g.name)
	}
	return out
}

// AllowGroups maps each group name to the primitive names it expands to.
// handlers/sets serves sets:Group from this, so the expansion cannot
// disagree with the constants.
func AllowGroups() map[string][]string {
	out := make(map[string][]string, len(allowGroups))
	for _, g := range allowGroups {
		out[g.name] = g.cat.Names()
	}
	return out
}

// Names renders a as the primitive category names it contains.
func (a Allow) Names() []string {
	var out []string
	for _, n := range allowNames {
		if a.Has(n.cat) {
			out = append(out, n.name)
		}
	}
	return out
}

// String is the canonical text encoding: the primitive names, separated
// by single spaces, in declaration order. ParseAllow(a.String()) == a for
// every a built from the exported constants.
//
// The empty set renders as "None" rather than as the empty string,
// because an attribute value of "" is how markup says "not written" and
// the two must not look alike.
func (a Allow) String() string {
	if a == AllowNone {
		return "None"
	}
	return strings.Join(a.Names(), " ")
}

// ParseAllow reads the text encoding: category names separated by spaces
// or commas, in any order, unioned. Case-sensitive, because every other
// name in markup is.
//
// An unknown name is an ERROR rather than a silently ignored token. That
// choice is what makes Allow="Clicks" a failure naming the vocabulary
// instead of a surface that is mysteriously sealed — the same bargain the
// rest of markup makes. A LITERAL Allow gets that at load time; a BOUND
// one cannot, so it fails closed to AllowNone and reports through
// components.Frozen.AllowError.
func ParseAllow(s string) (Allow, error) {
	out := AllowNone
	for _, f := range strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || unicode.IsSpace(r)
	}) {
		cat, ok := allowByName(f)
		if !ok {
			return AllowNone, fmt.Errorf("unknown Allow category %q; the vocabulary is: %s",
				f, strings.Join(AllowNames(), ", "))
		}
		out = out.Union(cat)
	}
	return out, nil
}

func allowByName(name string) (Allow, bool) {
	for _, n := range allowNames {
		if n.name == name {
			return n.cat, true
		}
	}
	for _, g := range allowGroups {
		if g.name == name {
			return g.cat, true
		}
	}
	return AllowNone, false
}

// SortAllowNames orders a list of category names the way String does, so
// a set built out of NAMES — which is what handlers/sets manipulates —
// has one canonical spelling. Unknown names sort after the known ones,
// alphabetically, because sets is generic and must not lose a token it
// does not recognize.
func SortAllowNames(names []string) {
	rank := func(n string) int {
		for i, e := range allowNames {
			if e.name == n {
				return i
			}
		}
		for i, g := range allowGroups {
			if g.name == n {
				return len(allowNames) + i
			}
		}
		return len(allowNames) + len(allowGroups)
	}
	sort.SliceStable(names, func(i, j int) bool {
		ri, rj := rank(names[i]), rank(names[j])
		if ri != rj {
			return ri < rj
		}
		return names[i] < names[j]
	})
}

// AllowFor classifies one key event: the category a frozen subtree must
// permit for this keystroke to reach a focused descendant.
//
// Ctrl and Alt make a CHORD whatever the key is, and that is the point of
// having the class at all: "let typing through" must not also let ctrl+s
// through, or a read-only preview saves the document. Shift is not a
// chord — terminals cannot report it on printable characters at all, and
// shift+tab is navigation.
func AllowFor(ev input.KeyEvent) Allow {
	if ev.Has(input.ModCtrl) || ev.Has(input.ModAlt) {
		return AllowChords
	}
	switch ev.Key {
	case input.KeyRune:
		switch {
		case ev.Rune == ' ':
			return AllowSpace
		case unicode.IsLetter(ev.Rune):
			return AllowAlpha
		case unicode.IsDigit(ev.Rune):
			return AllowNumeric
		default:
			return AllowPunct
		}
	case input.KeyTab, input.KeyUp, input.KeyDown, input.KeyLeft, input.KeyRight,
		input.KeyHome, input.KeyEnd, input.KeyPageUp, input.KeyPageDown:
		return AllowNav
	case input.KeyEnter, input.KeyBackspace, input.KeyDelete:
		return AllowEdit
	case input.KeyEsc:
		return AllowEscape
	}
	return AllowPunct
}

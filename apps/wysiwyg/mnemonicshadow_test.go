package main

import (
	"encoding/xml"
	"io"
	"os"
	"strings"
	"testing"
	"unicode"

	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/input"
)

// TestNoRootKeyBindingShadowsAMenuTitleMnemonic is the guard for the
// hazard the alt+ move cluster introduced.
//
// Dispatch offers a KeyBinding the event BEFORE the mnemonics get their
// turn, and a binding on the page root is on every path — so a root
// `alt+h` beats a `_Help` menu to alt+h, and the menu simply stops
// opening. Nothing is a load error and nothing goes red: two claims on
// one gesture, resolved by dispatch order, exactly the silent-collision
// class #427 is about.
//
// The check is derived from the shipped page rather than from a list
// here: every top-level <Menu> contributes its accelerator as alt+<that
// letter>, and every root-scoped <KeyBinding Gesture="alt+x"> is a
// collision with it. Adding a menu whose letter is already bound fails
// here, which is the direction that will actually happen — the bindings
// are older than the menus that will be added next to them.
//
// # Two ways this guard was blind, both found in review of #428
//
// THE LETTER CAME FROM A LOCAL RULE, not the framework's. It required an
// explicit "_" marker; components.splitMnemonic falls back to the first
// letter or digit, because "a menu bar without accelerators is broken
// furniture". So <Menu Title="Help"> — the likelier spelling, given the
// fallback — claimed alt+h, lost it to the root binding, and this test
// stayed green about the exact collision it is named for. It also read
// "__Tools" (an escaped underscore) as claiming '_' and indexed BYTES,
// so "_Über" produced half a UTF-8 sequence. It now asks
// components.MenuMnemonic, which is a window onto the same splitMnemonic
// the painter and the dispatcher use: a second copy of this rule is the
// defect, not the fix.
//
// AND "ROOT-SCOPED" WAS NOT CHECKED. The <Menu> arm tracked depth to
// reject submenus while the <KeyBinding> arm collected every binding in
// the file. That is not the docstring's premise: a KeyBinding is scoped
// by its HOST component and only fires while the focused chain passes
// through it, so a pane-scoped alt+<letter> is not on every path and is
// not a shadow. Every binding in the page is a root child today, so this
// changes nothing now and stops a future pane-scoped binding being
// reported as a collision it isn't.
func TestNoRootKeyBindingShadowsAMenuTitleMnemonic(t *testing.T) {
	src, err := os.ReadFile("wysiwyg.gooey")
	if err != nil {
		t.Fatal(err)
	}

	dec := xml.NewDecoder(strings.NewReader(string(src)))
	depth := 0
	menuDepth := -1
	// hostDepth is the depth of the page's root COMPONENT — the single
	// element inside <Gooey>. A KeyBinding one level below it is scoped
	// to the root and therefore on every path; anything deeper belongs to
	// a pane.
	hostDepth := -1
	mnemonics := map[rune]string{} // accelerator -> menu title
	// src is kept alongside the letter purely so the failure can quote the
	// spelling the page actually used — "meta+h" reads as a different
	// thing from "alt+h" until you know they parse to the same event.
	var altBindings []struct {
		src string
		r   rune
	}
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("the shipped page does not parse as XML: %v", err)
		}
		switch e := tok.(type) {
		case xml.StartElement:
			depth++
			// depth is incremented BEFORE this runs, so <Gooey> is at
			// depth 1 and the root component at depth 2. The condition
			// used to also test `e.Name.Local != "Gooey"`, which can
			// never be false here — a guard doing no work. Dropped in
			// review of #428.
			if depth == 2 && hostDepth == -1 {
				hostDepth = depth
			}
			switch e.Name.Local {
			case "Menu":
				// TOP-LEVEL menus only: a nested one is a submenu whose
				// mnemonic is scoped to its open parent, not to the page.
				if menuDepth == -1 {
					menuDepth = depth
					title := xmlAttr(e, "Title")
					if a, ok := components.MenuMnemonic(title); ok {
						mnemonics[a] = title
					}
				}
			case "KeyBinding":
				if depth != hostDepth+1 {
					continue // pane-scoped: not on every path
				}
				g := xmlAttr(e, "Gesture")
				// ASK THE PARSER, don't match the string. `alt+` as a
				// prefix is a third copy of a rule the framework already
				// owns, and it is wrong in five reachable ways:
				// ParseGesture lower-cases modifiers and accepts meta and
				// option as aliases, and folds shift into the rune — so
				// "meta+h", "option+h", "Alt+h", "ALT+H" and "shift+alt+h"
				// all parse to a ModAlt-only rune event that
				// MenuBar.HandleMnemonic matches, and every one of them
				// slipped past the prefix test as a perfectly loadable
				// root binding.
				//
				// This is the same move that produced MenuMnemonic, and the
				// docstring above already draws the lesson for the mnemonic
				// half: a second copy of the rule is the defect, not the
				// fix. The gesture half was still a local rule. Found in
				// review of #428.
				ev, err := input.ParseGesture(g)
				if err != nil || ev.Key != input.KeyRune || ev.Mods != input.ModAlt {
					continue // not a single-rune alt gesture: not mnemonic territory
				}
				altBindings = append(altBindings, struct {
					src string
					r   rune
				}{g, unicode.ToLower(ev.Rune)})
			}
		case xml.EndElement:
			if menuDepth == depth {
				menuDepth = -1
			}
			depth--
		}
	}

	if len(mnemonics) == 0 || len(altBindings) == 0 {
		t.Fatalf("found %d menu mnemonics and %d single-letter root alt bindings; "+
			"with either at zero this test cannot fail and proves nothing",
			len(mnemonics), len(altBindings))
	}

	for _, b := range altBindings {
		if title, clash := mnemonics[b.r]; clash {
			t.Errorf("a KeyBinding on %q collides with the %q menu's mnemonic "+
				"(both are alt+%c once parsed). Dispatch offers bindings before "+
				"mnemonics, so the binding wins and the menu silently stops "+
				"opening — move one of them",
				b.src, title, b.r)
		}
	}
	t.Logf("checked %d root alt bindings against %d menu mnemonics",
		len(altBindings), len(mnemonics))
}

// TestTheGuardReadsMnemonicsTheWayTheFrameworkDoes is the discrimination
// half, and it is what the local rule could not have: the four spellings
// where a hand-written "character after the underscore" disagrees with
// what a running MenuBar actually accelerates.
//
// Every row is a title the guard above may meet. A guard that agrees
// with the framework on all four is one whose green means something.
func TestTheGuardReadsMnemonicsTheWayTheFrameworkDoes(t *testing.T) {
	for _, tc := range []struct {
		title string
		want  rune
	}{
		{"_File", 'f'},
		{"E_xit", 'x'},
		// NO MARKER — the fallback, and the case the old guard could not
		// see at all.
		{"Help", 'h'},
		// AN ESCAPED underscore is a literal, so the accelerator falls
		// back to the first letter. The old guard answered '_'.
		{"__Tools", 't'},
		// NON-ASCII, where byte indexing returned half a sequence.
		{"_Über", 'ü'},
		{"Über", 'ü'},
		// A digit is an accelerator too.
		{"3D", '3'},
	} {
		got, ok := components.MenuMnemonic(tc.title)
		if !ok {
			t.Errorf("MenuMnemonic(%q) claims no accelerator; the framework "+
				"accelerates it with %q", tc.title, tc.want)
			continue
		}
		if got != tc.want {
			t.Errorf("MenuMnemonic(%q) = %q, want %q", tc.title, got, tc.want)
		}
	}
	// And a title with nothing to accelerate really does report none, or
	// the loop above passes against a function that always answers true.
	if _, ok := components.MenuMnemonic("!!!"); ok {
		t.Error("a title with no letter or digit claims an accelerator")
	}
}

// TestTheGuardReadsGesturesTheWayTheFrameworkDoes is the same
// discrimination applied to the OTHER half, and it is the half that was
// still a local rule until the review of #428.
//
// The guard used to detect an alt gesture with strings.CutPrefix(g,
// "alt+"). Every row below is a spelling that test rejected and that
// MenuBar.HandleMnemonic accepts — a loadable root binding that would
// beat a menu to its own letter with the guard staying green.
//
// The rows are not hypothetical spellings: ParseGesture lower-cases
// modifiers, accepts meta and option as aliases of alt, and folds shift
// into the rune, so each of these is a thing a page may legitimately
// say.
func TestTheGuardReadsGesturesTheWayTheFrameworkDoes(t *testing.T) {
	for _, tc := range []struct {
		gesture string
		want    rune
		missed  bool // did the old `alt+` prefix rule drop it?
	}{
		{"alt+h", 'h', false},
		{"meta+h", 'h', true},
		{"option+h", 'h', true},
		{"Alt+h", 'h', true},
		{"ALT+H", 'h', true},
		{"shift+alt+h", 'h', true},
	} {
		ev, err := input.ParseGesture(tc.gesture)
		if err != nil {
			t.Errorf("ParseGesture(%q) failed: %v — the page could not have "+
				"shipped this spelling, and the row is wrong", tc.gesture, err)
			continue
		}
		if ev.Key != input.KeyRune || ev.Mods != input.ModAlt {
			t.Errorf("ParseGesture(%q) = {Key:%v Mods:%v}, want a ModAlt-only "+
				"rune. If this is right, the guard's filter is now too wide",
				tc.gesture, ev.Key, ev.Mods)
			continue
		}
		if got := unicode.ToLower(ev.Rune); got != tc.want {
			t.Errorf("ParseGesture(%q) accelerates %q, want %q",
				tc.gesture, got, tc.want)
		}
		// The claim that makes this test worth having: the old rule
		// really did drop these.
		if _, prefixed := strings.CutPrefix(tc.gesture, "alt+"); prefixed == tc.missed {
			t.Errorf("%q: the superseded `alt+` prefix rule %s it, but the row "+
				"says otherwise — the widening this test pins is mis-stated",
				tc.gesture, map[bool]string{true: "dropped", false: "caught"}[tc.missed])
		}
	}
}

// xmlAttr reads one attribute off a start element. Named apart from the
// docs-tab branch's identically-shaped helper so the two can land in
// either order.
func xmlAttr(e xml.StartElement, name string) string {
	for _, a := range e.Attr {
		if a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}

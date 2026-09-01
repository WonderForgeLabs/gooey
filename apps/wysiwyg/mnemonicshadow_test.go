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

// TestNoKeyBindingShadowsAMenuTitleMnemonic is the guard for the hazard
// the alt+ move cluster introduced.
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
// AND THE GESTURE HALF WAS A LOCAL RULE TOO — see the KeyBinding arm.
//
// # And one blindness I added while removing another
//
// A round of this narrowed the sweep to root-scoped bindings, arguing
// that "a pane-scoped alt+<letter> is not on every path and is not a
// shadow". That is wrong, and the review of #428 caught it.
//
// Dispatch's own doc says the bubble matches "the KeyBindings attached
// at each level" of the focused chain, and the mnemonics run only after
// the whole chain has declined. So a binding on a PANE beats the menu
// for as long as focus is inside that pane — which in this editor is
// most of the time. It is a CONDITIONAL shadow, not the absence of one,
// and conditional is the worse bug to chase: the menu opens until you
// click the explorer, and then quietly stops.
//
// Both scopes are swept now, and the failure says which it found,
// because "never opens" and "stops opening once you click there" send a
// reader to different places.
func TestNoKeyBindingShadowsAMenuTitleMnemonic(t *testing.T) {
	src, err := os.ReadFile("wysiwyg.gooey")
	if err != nil {
		t.Fatal(err)
	}

	dec := xml.NewDecoder(strings.NewReader(string(src)))
	depth := 0
	menuDepth := -1
	mnemonics := map[rune]string{} // accelerator -> menu title
	// src is kept alongside the letter purely so the failure can quote the
	// spelling the page actually used — "meta+h" reads as a different
	// thing from "alt+h" until you know they parse to the same event.
	//
	// root distinguishes the two shadow classes. A binding one level
	// inside <Gooey> is on the root component and so on every path; a
	// deeper one fires only while focus is in that subtree. Both beat the
	// mnemonic — see the KeyBinding arm — but the failure should say
	// which, because "the menu never opens" and "the menu stops opening
	// once you click the explorer" are different bugs to chase.
	//
	// DEPTH 3 IS WRITTEN, not derived, and the review of #428 was right
	// that the `hostDepth` variable it replaces only ever held -1 or 2:
	// `depth != hostDepth+1` was `depth != 3` with extra steps, while its
	// comment claimed a derivation from the file. <Gooey> is depth 1, the
	// root component depth 2, so a binding on the root is depth 3.
	const rootBindingDepth = 3
	var altBindings []struct {
		src  string
		r    rune
		root bool
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
			// INCREMENTED FIRST, so <Gooey> is depth 1, the root
			// component depth 2, and a binding on the root depth 3 — see
			// rootBindingDepth above.
			depth++
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
				// EVERY DEPTH, and narrowing this to root-scoped
				// bindings was a defect I introduced and the review of
				// #428 caught.
				//
				// The reasoning I wrote for the narrowing — "a
				// pane-scoped alt+<letter> is not on every path and is
				// not a shadow" — is wrong. Dispatch's own doc
				// (input.go, "It TUNNELS first") says the bubble matches
				// "the KeyBindings attached at each level" of the
				// focused chain, and only then do the mnemonics get
				// their turn. So a binding on a PANE beats the menu for
				// as long as focus is inside that pane, which in this
				// editor is most of the time.
				//
				// It is a CONDITIONAL shadow rather than no shadow, and
				// a conditional one is worse to debug: the menu opens
				// until you click the explorer, and then stops.
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
					src  string
					r    rune
					root bool
				}{g, unicode.ToLower(ev.Rune), depth == rootBindingDepth})
			}
		case xml.EndElement:
			if menuDepth == depth {
				menuDepth = -1
			}
			depth--
		}
	}

	if len(mnemonics) == 0 || len(altBindings) == 0 {
		t.Fatalf("found %d menu mnemonics and %d single-letter alt bindings; "+
			"with either at zero this test cannot fail and proves nothing",
			len(mnemonics), len(altBindings))
	}

	for _, b := range altBindings {
		if title, clash := mnemonics[b.r]; clash {
			scope := "a pane-scoped KeyBinding (it wins while focus is inside " +
				"that subtree, so the menu stops opening once the user clicks there)"
			if b.root {
				scope = "a root-scoped KeyBinding (it is on every path, so the " +
					"menu never opens at all)"
			}
			t.Errorf("%s on %q collides with the %q menu's mnemonic (both are "+
				"alt+%c once parsed). Dispatch matches the bindings at each level "+
				"of the focused chain BEFORE the mnemonics get their turn, so the "+
				"binding wins — move one of them",
				scope, b.src, title, b.r)
		}
	}
	t.Logf("checked %d alt bindings (root and pane-scoped) against %d menu mnemonics",
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

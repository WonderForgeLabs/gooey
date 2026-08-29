package main

import (
	"strings"
	"testing"
	"unicode"

	"github.com/WonderForgeLabs/gooey/input"
)

// titleAccel is the letter alt+<letter> must open, derived the way
// components.splitMnemonic derives it: the rune after the underscore
// marker, falling back to the first letter when a title carries none.
//
// Reimplemented rather than exported, because the alternative is worse.
// A test that asked the bar which letter it listens on would agree with
// the bar by construction — including when the bar is wrong — and the
// thing under test here is whether the KEYMAP and the MENU TITLES agree,
// which needs two independent readings of the title.
func titleAccel(title string) (rune, bool) {
	if i := strings.IndexRune(title, '_'); i >= 0 {
		for _, r := range title[i+len("_"):] {
			return unicode.ToLower(r), true
		}
	}
	for _, r := range title {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r), true
		}
	}
	return 0, false
}

// TestEveryMenuTitleStillOpensByItsMnemonic is the regression guard for a
// collision that is invisible in every other instrument.
//
// alt+p was bound at the ROOT to PinPane while `_Project` wore p as its
// mnemonic. FocusManager.Dispatch runs the entire KeyBinding bubble before
// it offers a key to any MnemonicHandler, and a root-scoped binding sits in
// every focused chain — so alt+p pinned a pane and the Project menu could
// not be opened from the keyboard at all. Nothing is red when that happens:
// the binding works, the menu still opens by mouse, and both components
// behave exactly as written. Only the pairing is wrong.
//
// The assertion is behavioural rather than a scan of the markup for
// `alt+<letter>` on purpose. A source scan pins the one shape that caused
// this bug; pressing the key catches ANY consumer that gets there first —
// a KeyBinding, a HandleKey, a PreviewKey tunnel — which is the property
// that actually has to hold.
func TestEveryMenuTitleStillOpensByItsMnemonic(t *testing.T) {
	ed, c := designPage(t)
	bar := theMenuBar(t, ed)

	checked := 0
	for _, m := range bar.Menus {
		r, ok := titleAccel(m.Title)
		if !ok {
			continue
		}
		// Dismiss first: with a menu already open, HandleMnemonic takes
		// its switch-menu branch instead of the open branch, and the test
		// would pass having exercised the wrong path.
		bar.Dismiss()
		c.Frame()
		if bar.IsOpen() {
			t.Fatalf("the bar is still open after Dismiss; %q cannot be tested from here", m.Title)
		}

		c.Handle(input.KeyOf(input.KeyEvent{Key: input.KeyRune, Rune: r, Mods: input.ModAlt}))
		c.Frame()
		if !bar.IsOpen() {
			t.Errorf("alt+%c does not open the %q menu.\n\n"+
				"Something consumed the key before the mnemonic seam saw it. The usual "+
				"cause is a root <KeyBinding Gesture=\"alt+%c\">: Dispatch runs every "+
				"KeyBinding in the focused chain before any MnemonicHandler, and a "+
				"root-scoped binding is always in that chain, so it wins permanently and "+
				"silently. Menu titles own their alt+letter — give the command a gesture "+
				"that is not one (ctrl+alt+%c stays clear, because HandleMnemonic tests "+
				"Mods for equality with ModAlt).", r, m.Title, r, r)
		}
		checked++
	}

	bar.Dismiss()
	if checked == 0 {
		t.Fatal("no menu title carries a mnemonic; this test would pass vacuously")
	}
	t.Logf("checked %d menu-title mnemonics", checked)
}

// TestMenuTitleMnemonicsAreDistinct is the other half, and it is not
// implied by the test above: two titles wearing the same letter both
// "open a menu", so every assertion up there passes while one of them can
// never be reached — titleWithAccel returns the first match.
//
// Nothing in the shipped page collides, so this guard only ever says
// "fine" and could guard nothing without anyone noticing. To make it
// fire, retitle one menu so its accelerator duplicates another's —
// `_View` to `_Fiew` collides with `_File` — and it reports the pair.
// Checked that way rather than assumed: a sibling test on #406 passed
// with the very fix it claimed to guard reverted, because the direction
// where it must say NO was never run.
func TestMenuTitleMnemonicsAreDistinct(t *testing.T) {
	ed, _ := buildPage(t)
	bar := theMenuBar(t, ed)

	seen := map[rune]string{}
	for _, m := range bar.Menus {
		r, ok := titleAccel(m.Title)
		if !ok {
			t.Errorf("menu %q has no mnemonic letter at all", m.Title)
			continue
		}
		if prev, dup := seen[r]; dup {
			t.Errorf("menus %q and %q both answer to alt+%c; only %q will ever open, "+
				"because titleWithAccel returns the first match", prev, m.Title, r, prev)
			continue
		}
		seen[r] = m.Title
	}
	if len(seen) == 0 {
		t.Fatal("no menu titles were checked; this test would pass vacuously")
	}
}

package components

import (
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

func altKey(r rune) input.KeyEvent {
	return input.KeyEvent{Key: input.KeyRune, Rune: r, Mods: input.ModAlt}
}

// A marked button is a page-scoped accelerator: alt+letter clicks it
// while focus sits somewhere else entirely — the whole point of the
// MnemonicHandler seam.
func TestButtonMnemonicClicksFromAnywhere(t *testing.T) {
	clicked := 0
	text := prop.NewSource("hello")
	btn := &Button{
		Content: prop.NewSource("_Save"),
		Click:   gooey.Command(func() { clicked++ }),
	}
	root := &VStack{Children: []gooey.Component{
		&TextBox{Text: text}, // first focus stop: focus is NOT on the button
		btn,
	}}
	c := gooey.NewComposer(root, 30, 5)
	c.Frame()

	if !c.HandleKey(altKey('s')) {
		t.Fatal("alt+s was not consumed")
	}
	if clicked != 1 {
		t.Fatalf("alt+s clicked %d times, want 1", clicked)
	}
	// Case-insensitive, as menus are: the marker names _S, alt+S works too.
	if !c.HandleKey(altKey('S')) || clicked != 2 {
		t.Fatalf("alt+S clicked %d times total, want 2", clicked)
	}
	// A letter no button wears keeps bubbling (nothing else wants it, so
	// the dispatch reports unconsumed).
	if c.HandleKey(altKey('x')) {
		t.Fatal("alt+x was consumed by nothing in particular")
	}
	// And the plain rune must still be a TextBox edit, not a click.
	if !c.HandleKey(input.Rune('s')) || clicked != 2 {
		t.Fatalf("plain 's' should edit the focused TextBox, not click (clicked=%d)", clicked)
	}
}

// No marker, no mnemonic: buttons take only the explicit underscore.
// Auto-registering first letters would hand every button a page-global
// gesture nobody declared.
func TestButtonWithoutMarkerHasNoMnemonic(t *testing.T) {
	clicked := 0
	btn := &Button{
		Content: prop.NewSource("save"),
		Click:   gooey.Command(func() { clicked++ }),
	}
	root := &VStack{Children: []gooey.Component{&TextBox{Text: prop.NewSource("")}, btn}}
	c := gooey.NewComposer(root, 30, 5)
	c.Frame()

	if c.HandleKey(altKey('s')) {
		t.Fatal("an unmarked button consumed alt+s")
	}
	if clicked != 0 {
		t.Fatalf("an unmarked button clicked %d times", clicked)
	}
}

// Dispatch order: a KeyBinding on the same alt gesture outranks the
// mnemonic — bindings run in the bubble phase, mnemonics only see what
// the focused chain declined.
func TestKeyBindingOutranksButtonMnemonic(t *testing.T) {
	clicked, bound := 0, 0
	btn := &Button{
		Content: prop.NewSource("_Save"),
		Click:   gooey.Command(func() { clicked++ }),
	}
	root := &VStack{Children: []gooey.Component{&TextBox{Text: prop.NewSource("")}, btn}}
	root.Attach(&gooey.KeyBinding{Gesture: altKey('s'), Command: gooey.Command(func() { bound++ })})
	c := gooey.NewComposer(root, 30, 5)
	c.Frame()

	if !c.HandleKey(altKey('s')) {
		t.Fatal("alt+s was not consumed")
	}
	if bound != 1 || clicked != 0 {
		t.Fatalf("binding ran %d times, click %d — the KeyBinding must win", bound, clicked)
	}
}

// A disabled button declines its mnemonic, exactly like its HandleKey:
// the gesture is not consumed, so an outer handler can still have it.
func TestDisabledButtonDeclinesMnemonic(t *testing.T) {
	clicked := 0
	can := prop.NewSource(false)
	btn := &Button{
		Content: prop.NewSource("_Save"),
		Click:   gooey.NewCommand(func() { clicked++ }).When(can),
	}
	root := &VStack{Children: []gooey.Component{&TextBox{Text: prop.NewSource("")}, btn}}
	c := gooey.NewComposer(root, 30, 5)
	c.Frame()

	if c.HandleKey(altKey('s')) {
		t.Fatal("a disabled button consumed its mnemonic")
	}
	if clicked != 0 {
		t.Fatalf("a disabled button ran its click %d times", clicked)
	}
	can.Set(true)
	c.Frame()
	if !c.HandleKey(altKey('s')) || clicked != 1 {
		t.Fatalf("after enabling, alt+s clicked %d times, want 1", clicked)
	}
}

// The marker is syntax, not text: the label shows "Save" with the S
// underlined — always, held ALT being invisible to a terminal — and the
// underscore never reaches the screen. Sizing uses the display text.
func TestButtonMnemonicRendersStrippedAndUnderlined(t *testing.T) {
	btn := &Button{Content: prop.NewSource("_Save"), Click: gooey.Command(func() {})}
	c := gooey.NewComposer(btn, 30, 3)
	f, _ := c.Frame()

	var line []rune
	for x := 0; x < 10; x++ {
		line = append(line, f.Cells.At(x, 0).Rune)
	}
	if got := string(line[:8]); got != "[ Save ]" {
		t.Fatalf("label = %q, want %q — the marker must be stripped", got, "[ Save ]")
	}
	// "[ Save ]": the S sits at x=2.
	if !f.Cells.At(2, 0).Style.Underline {
		t.Fatal("the accelerator cell is not underlined")
	}
	if f.Cells.At(3, 0).Style.Underline {
		t.Fatal("a non-accelerator cell is underlined")
	}
	if w := btn.Measure(gooey.Size{W: 80, H: 1}).W; w != len("[ Save ]") {
		t.Fatalf("measured width %d counts the marker; want %d", w, len("[ Save ]"))
	}
}

// TestAnAcceleratorKeepsItsClusterWhenUnderlined pins that underlining
// a letter is not a reason for the letter to change.
//
// The four painters re-derived the accelerator as []rune(text)[pos] and
// handed that single rune to Buffer.Set. A cell holding a grapheme
// CLUSTER cannot survive that: a decomposed "é" is 'e' followed by a
// combining acute, so the label painted "é" and the underline painted a
// bare "e" over it — the accent disappeared the moment the accelerator
// was drawn, which is always.
//
// A DECOMPOSED character on purpose. The precomposed U+00E9 is a single
// rune and survives the lossy spelling, so a fixture using it would
// have passed against the code this replaces.
func TestAnAcceleratorKeepsItsClusterWhenUnderlined(t *testing.T) {
	const acute = "é" // one cluster, two runes, one column

	btn := &Button{
		Content: prop.NewSource("_" + acute + "dit"),
		Click:   gooey.Command(func() {}),
	}
	c := gooey.NewComposer(btn, 30, 3)
	t.Cleanup(c.Close)
	f, _ := c.Frame()

	// "[ édit ]": the accelerator sits at x=2.
	cell := f.Cells.At(2, 0)
	if !cell.Style.Underline {
		t.Fatal("the accelerator cell is not underlined")
	}
	if got := cell.Text(); got != acute {
		t.Errorf("the underlined cell draws %q, want %q — the combining "+
			"mark was dropped when the underline was painted", got, acute)
	}
	if got := render.RowText(f.Cells, 0); !strings.HasPrefix(got, "[ "+acute+"dit ]") {
		t.Errorf("the row reads %q, want it to start with %q",
			got, "[ "+acute+"dit ]")
	}
}

// The damage pin: an accelerator click is input, not paint. A mnemonic
// whose command touches no properties repaints NOTHING, and one that
// Sets a single Text repaints exactly that Text — the button itself has
// no press flash to pay for.
func TestButtonMnemonicDamage(t *testing.T) {
	label := prop.NewSource("idle")
	noop := &Button{Content: prop.NewSource("_Quiet"), Click: gooey.Command(func() {})}
	writer := &Button{Content: prop.NewSource("_Write"), Click: gooey.Command(func() { label.Set("wrote") })}
	root := &VStack{Children: []gooey.Component{
		&TextBox{Text: prop.NewSource("")},
		noop, writer,
		&Text{Content: label},
	}}
	c := gooey.NewComposer(root, 30, 6)
	c.Frame()

	c.HandleKey(altKey('q'))
	if _, painted := c.Frame(); painted != 0 {
		t.Fatalf("a no-op mnemonic repainted %d nodes, want 0", painted)
	}
	c.HandleKey(altKey('w'))
	if _, painted := c.Frame(); painted != 1 {
		t.Fatalf("a one-Set mnemonic repainted %d nodes, want 1 (the Text that read it)", painted)
	}
}

// splitExplicitMnemonic: marker-only parsing, no first-letter fallback.
func TestSplitExplicitMnemonic(t *testing.T) {
	cases := []struct {
		in    string
		text  string
		accel rune
		pos   int
		ok    bool
	}{
		{"_Refresh", "Refresh", 'r', 0, true},
		{"E_xit", "Exit", 'x', 1, true},
		{"refresh ^r", "refresh ^r", 0, -1, false},
		{"a__b", "a_b", 0, -1, false},
		{"_a_b", "a_b", 'a', 0, true}, // only the first marker is syntax; later ones are literal
		{"", "", 0, -1, false},
	}
	for _, tc := range cases {
		text, accel, pos, ok := splitExplicitMnemonic(tc.in)
		if text != tc.text || pos != tc.pos || ok != tc.ok || (ok && accel != tc.accel) {
			t.Errorf("splitExplicitMnemonic(%q) = (%q, %q, %d, %v), want (%q, %q, %d, %v)",
				tc.in, text, accel, pos, ok, tc.text, tc.accel, tc.pos, tc.ok)
		}
	}
}

// TestMenuMnemonicIsPinnedWhereCIRunsIt covers the EXPORTED accelerator
// rule, and it lives here rather than in apps/wysiwyg for one reason:
// CLAUDE.md's Verify section says CI vets `apps/*` without running their
// suites.
//
// MenuMnemonic is new root-module public surface. Its only tests were in
// apps/wysiwyg — so the contract of an exported framework function was
// green only in someone's local loop, and a change to splitMnemonic
// would have reached CI unopposed. Found in the re-review of #428.
//
// The rows are the ones that separate this from a hand-written "the
// character after the underscore": the marker-less fallback, an escaped
// underscore, and a non-ASCII accelerator that byte indexing would cut
// in half.
func TestMenuMnemonicIsPinnedWhereCIRunsIt(t *testing.T) {
	for _, tc := range []struct {
		title string
		want  rune
	}{
		{"_File", 'f'},
		{"E_xit", 'x'},
		// NO MARKER: splitMnemonic falls back to the first letter or
		// digit, because a menu bar without accelerators is broken
		// furniture. This is the likelier spelling in a real page.
		{"Help", 'h'},
		// An ESCAPED underscore is a literal, so the accelerator falls
		// back to the first letter rather than claiming '_'.
		{"__Tools", 't'},
		// NON-ASCII, where indexing bytes returns half a sequence.
		{"_Über", 'ü'},
		{"Über", 'ü'},
		// A digit accelerates too.
		{"3D", '3'},
	} {
		got, ok := MenuMnemonic(tc.title)
		if !ok {
			t.Errorf("MenuMnemonic(%q) reports no accelerator; the menu bar "+
				"accelerates it with %q", tc.title, tc.want)
			continue
		}
		if got != tc.want {
			t.Errorf("MenuMnemonic(%q) = %q, want %q", tc.title, got, tc.want)
		}
	}
	// And a title with nothing to accelerate really reports none, or the
	// loop above passes against a function that always answers true.
	if _, ok := MenuMnemonic("!!!"); ok {
		t.Error("a title with no letter or digit claims an accelerator")
	}
}

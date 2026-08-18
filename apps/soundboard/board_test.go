package main

// The gesture a component advertises has to be a gesture that reaches
// the thing it names.
//
// board.go's own comment says everything but the pad and step keys is
// declined "so it bubbles to the KeyBindings, which is where the
// transport lives, visibly, in markup" — and nothing checked that.
// Dispatch is target-first (FocusManager.Dispatch): a component that
// consumes a key consumes it before any binding sees it, so a focus stop
// owns every letter it claims. `apps/synth` shipped exactly this bug —
// a documented quit key eaten as a data key — and pinned it afterwards
// with two dedicated tests. This is that pin, here, before the bug.
//
// It matters most for the two quit gestures, because the failure mode is
// an app you cannot leave. It matters for the rest because the help text
// on screen is a promise.

import (
	"os"
	"regexp"
	"testing"

	"github.com/WonderForgeLabs/gooey/input"
)

// A Board over a real App, minus the gooey.App the transport commands
// would need. Nothing here reaches Quit() — that is the binding's job,
// and whether the binding is reachable is exactly what is in question.
func testBoard() *Board { return &Board{app: NewApp(30, 120)} }

// The gestures come from the MARKUP, not from a list written here. A
// list would pass forever after someone adds the ninth binding — which
// is the failure this whole file exists to prevent, one level up.
func declaredGestures(t *testing.T) []string {
	t.Helper()
	src, err := os.ReadFile("soundboard.gooey")
	if err != nil {
		t.Fatalf("soundboard.gooey: %v", err)
	}
	m := regexp.MustCompile(`<KeyBinding[^>]*\bGesture="([^"]+)"`).FindAllStringSubmatch(string(src), -1)
	if len(m) == 0 {
		t.Fatal("no KeyBinding gestures found in soundboard.gooey — " +
			"either the markup moved or this regex did, and a test that " +
			"checks nothing passes quietly")
	}
	out := make([]string, 0, len(m))
	for _, g := range m {
		out = append(out, g[1])
	}
	return out
}

func TestBoardDeclinesEveryGestureTheMarkupBinds(t *testing.T) {
	gestures := declaredGestures(t)
	b := testBoard()

	for _, g := range gestures {
		ev, err := input.ParseGesture(g)
		if err != nil {
			t.Errorf("Gesture=%q does not parse: %v — the binding is dead in the markup", g, err)
			continue
		}
		if b.HandleKey(ev) {
			t.Errorf("Board consumed %q, which soundboard.gooey binds to a command; "+
				"dispatch is target-first, so the binding never fires", g)
		}
	}
	t.Logf("checked %d declared gestures", len(gestures))
}

// And the other half: the keys the Board DOES own must still be owned.
// Without this, "decline everything" satisfies the test above and the
// instrument stops making noise.
func TestBoardOwnsItsPadAndStepKeys(t *testing.T) {
	b := testBoard()

	for _, s := range b.app.mix.Kit() {
		if !b.HandleKey(input.KeyEvent{Key: input.KeyRune, Rune: s.Key}) {
			t.Errorf("pad key %q was declined — %s cannot be played", s.Key, s.Name)
		}
	}
	// Against Steps, NOT against len(stepKeys). Ranging over stepKeys to
	// check stepKeys is a test that shrinks with its own subject: delete
	// a key and the loop simply runs one fewer time, green, while step
	// sixteen becomes unreachable from the keyboard. Steps is the
	// sequencer's own count and knows nothing about this keymap, which is
	// what makes it an authority rather than an echo.
	if len(stepKeys) != Steps {
		t.Fatalf("%d step keys for %d steps — some step has no key, or some key no step",
			len(stepKeys), Steps)
	}
	seen := map[int]rune{}
	for _, r := range stepKeys {
		i := stepKey(r)
		if i < 0 || i >= Steps {
			t.Errorf("step key %q maps to %d, outside 0..%d", r, i, Steps-1)
			continue
		}
		if prev, dup := seen[i]; dup {
			t.Errorf("step keys %q and %q both toggle step %d", prev, r, i)
		}
		seen[i] = r
		if !b.HandleKey(input.KeyEvent{Key: input.KeyRune, Rune: r}) {
			t.Errorf("step key %q was declined", r)
		}
	}
	if len(seen) != Steps {
		t.Errorf("%d of %d steps are reachable from the keyboard", len(seen), Steps)
	}
	for _, k := range []input.Key{input.KeyUp, input.KeyDown} {
		if !b.HandleKey(input.KeyEvent{Key: k}) {
			t.Errorf("%v was declined — channel selection is broken", k)
		}
	}
}

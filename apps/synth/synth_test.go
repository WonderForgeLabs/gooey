package main

import (
	"os"
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/render"
)

// The claim this instrument makes is that a 48 kHz audio engine costs
// the property graph one change per FRAME, and that the change repaints
// the visualiser and the on-screen keyboard and nothing else. Only a
// damage count pins that: "the bars moved" is equally true when the
// whole page repainted.
func TestFrameRepaintsOnlyTheVisualisers(t *testing.T) {
	s := NewSynth(30)
	ctx := s.Context()
	root, err := markup.Page(os.DirFS("."), "synth.gooey", ctx).Build()
	if err != nil {
		t.Fatal(err)
	}
	c := gooey.NewComposer(root, 80, 24)
	c.Frame()

	s.rev.Set(s.rev.Get() + 1)
	_, painted := c.Frame()

	// Viz, Keys, and the two computeds that read rev (Peak's ProgressBar
	// and the Voices label). Anything more means a component went into
	// the damage set that has no business there.
	if painted < 2 || painted > 4 {
		t.Fatalf("one frame painted %d components, want 2–4 (Viz, Keys, and the rev-reading labels)", painted)
	}
}

// The engine must be usable with no audio server at all — every machine
// this deck is rehearsed on is one — so its whole control surface has to
// be safe before Start and after a failed Start.
func TestEngineControlsAreSafeWithoutAudio(t *testing.T) {
	e := NewEngine()
	e.NoteOn(0, 440)
	e.SetCutoff(0.8)
	e.SetRes(0.5)
	e.CycleWave()
	e.Release(0)
	if got := e.Snapshot(); got.Peak != 0 {
		t.Errorf("peak = %v before any block rendered, want 0", got.Peak)
	}
	if len(e.Expired()) == 0 {
		t.Log("no expired voices yet, which is fine")
	}
}

// A terminal sends no key-up, so notes release themselves. If that
// mechanism breaks, every note becomes a drone and the instrument is
// unusable — and nothing else would catch it, because the sound is the
// only symptom.
func TestNotesReleaseThemselves(t *testing.T) {
	e := NewEngine()
	e.NoteOn(0, 440)
	block := make([]float64, Block)
	for i := 0; i <= noteBlocks+1; i++ {
		e.age()
		e.render(block)
	}
	if e.voices[0].on {
		t.Fatalf("voice still held after %d blocks; the fixed lifetime is not firing", noteBlocks+1)
	}
	if e.voices[0].stage != 2 {
		t.Fatalf("voice stage = %d after its lifetime, want 2 (release)", e.voices[0].stage)
	}
}

// The keyboard owns every letter, so the way out of the instrument has
// to be a key the keyboard DECLINES — otherwise the documented quit key
// plays a note and the app cannot be left.
//
// This is a regression pin, not a style check: synth.gooey advertised
// "q quit" while q was mapped to C an octave up, and Board consumed it
// before any KeyBinding saw it. Pressing the documented exit made a
// sound and nothing else.
func TestQuitGesturesAreDeclinedByTheKeyboard(t *testing.T) {
	b := &Board{synth: NewSynth(30)}
	for _, ev := range []input.KeyEvent{
		{Key: input.KeyEsc},
		{Key: input.KeyRune, Rune: 'q', Mods: input.ModCtrl},
	} {
		if b.HandleKey(ev) {
			t.Fatalf("Board consumed %v; a quit gesture must bubble to the page's KeyBindings", ev)
		}
	}
	// And the bare letter really is a note, which is why the exit could
	// not be a bare letter in the first place.
	if !b.HandleKey(input.KeyEvent{Key: input.KeyRune, Rune: 'q'}) {
		t.Fatal("bare q should be consumed as a note")
	}
}

// The markup must actually carry the gestures the test above assumes,
// and the help line must not name one it does not.
func TestMarkupBindsTheQuitGesturesItAdvertises(t *testing.T) {
	src, err := os.ReadFile("synth.gooey")
	if err != nil {
		t.Fatal(err)
	}
	s := string(src)
	for _, want := range []string{`Gesture="esc"`, `Gesture="ctrl+q"`} {
		if !strings.Contains(s, want) {
			t.Errorf("synth.gooey has no %s", want)
		}
	}
	// The separator matters: "ctrl+q quits" contains "q quit" as a
	// substring, so the naive check fails on the corrected line. What is
	// wrong is q as a WHOLE gesture, and in this help line a gesture is
	// whatever follows a middle dot.
	if strings.Contains(s, "· q quit") {
		t.Error("synth.gooey still advertises bare q as the exit; q is a note key")
	}
}

// A leaf that writes one cell past its own bounds is the worst kind of
// rendering bug: the cell it lands on belongs to a node that is clean
// and will not repaint over it, so a stray character sits there until
// something unrelated forces a full redraw. It reads as "characters
// left around randomly", and it is not random — it is arithmetic.
//
// Keys draws three cells per key starting at x, so the last one is x+2
// and it must satisfy x+2 <= right edge. The guard was `x+2 > b.X+b.W`,
// which permits x+2 == b.X+b.W exactly: one cell into the neighbour,
// whenever the pane width leaves the stride landing there.
func TestKeysNeverWritesOutsideItsBounds(t *testing.T) {
	const pad = 4
	for w := 4; w <= 100; w++ {
		for _, h := range []int{3, 5} {
			k := &Keys{synth: NewSynth(30)}
			k.Arrange(gooey.Rect{X: pad, Y: 1, W: w, H: h})

			buf := render.NewBuffer(w+2*pad, h+4)
			k.Render(&gooey.Frame{Cells: buf})

			for y := 0; y < buf.H; y++ {
				for x := 0; x < buf.W; x++ {
					inside := x >= pad && x < pad+w && y >= 1 && y < 1+h
					if inside {
						continue
					}
					if c := buf.At(x, y); c.Rune != 0 && c.Rune != ' ' {
						t.Fatalf("w=%d h=%d: Keys wrote %q at (%d,%d), outside its bounds x∈[%d,%d) y∈[1,%d)",
							w, h, c.Rune, x, y, pad, pad+w, 1+h)
					}
				}
			}
		}
	}
}

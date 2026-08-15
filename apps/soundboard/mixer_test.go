package main

import (
	"os"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/markup"
)

// The sequencer clock lives inside the audio callback, counted in
// SAMPLES rather than in frames — that is what makes the groove steady
// — and nothing else in the program would notice if it stopped. A test
// on the step is the only thing standing between "the transport says
// playing" and the transport actually playing.
func TestStepAdvancesInsideTheAudioBlock(t *testing.T) {
	m := NewMixer(128)
	m.TogglePlay()

	l := make([]float64, Block)
	r := make([]float64, Block)

	start := m.Snapshot().Step
	// One second of audio.
	for i := 0; i < SampleRate/Block; i++ {
		m.mu.Lock()
		m.render(l, r)
		m.mu.Unlock()
	}
	got := m.Snapshot()
	if got.Step == start {
		t.Fatalf("step still %d after a second of audio; the sequencer clock is not running", got.Step)
	}
	if got.Peak <= 0 {
		t.Fatalf("peak = %v after a second of a pattern with four channels on it; nothing is being mixed", got.Peak)
	}
}

// A pad hit must reach the output even with the transport stopped —
// that is the difference between a soundboard and a drum machine.
func TestHitMakesSoundWithTransportStopped(t *testing.T) {
	m := NewMixer(128)
	m.Hit(0)
	l := make([]float64, Block)
	r := make([]float64, Block)
	peak := 0.0
	for i := 0; i < 20; i++ {
		m.mu.Lock()
		m.render(l, r)
		m.mu.Unlock()
		for _, v := range l {
			if v > peak {
				peak = v
			}
		}
	}
	if peak <= 0.01 {
		t.Fatalf("peak %v after hitting the kick; the pad is silent", peak)
	}
}

// The frame-clock claim: a sixteen-step sequencer costs the property
// graph one change per frame, and that change repaints the grid, the
// scope and the handful of labels that read it — not the page.
func TestFrameRepaintsOnlyTheMeters(t *testing.T) {
	a := NewApp(30, 128)
	ctx := a.Context()
	root, err := markup.Page(os.DirFS("."), "soundboard.gooey", ctx).Build()
	if err != nil {
		t.Fatal(err)
	}
	c := gooey.NewComposer(root, 100, 28)
	c.Frame()

	a.rev.Set(a.rev.Get() + 1)
	_, painted := c.Frame()

	// Board, Scope, the peak meter, and the two transport labels. The
	// border, the title, the logo and the help line have no business in
	// this set — the logo especially, because re-encoding an image every
	// frame is exactly the cost this architecture exists to avoid.
	if painted < 2 || painted > 6 {
		t.Fatalf("one frame painted %d components, want 2–6", painted)
	}
}

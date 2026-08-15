package main

// Synth is the app: the engine, the keyboard map, and the one property
// the visualiser subscribes to.
//
// # The single Set
//
// The engine runs at 48 kHz. The graph learns about it thirty times a
// second, and it learns exactly one thing: rev changed. Everything the
// picture needs is in a plain struct copied out under the engine's mutex
// — not in properties — because a property per band would be
// twenty-eight Sets a frame to redraw one component that was going to
// repaint anyway.
//
// That is the general shape for anything sampled: ONE handle for "there
// is new data", and the data itself in ordinary memory the renderer
// reads. Properties are for things the graph has to reason about, and
// the graph has no opinion about band nineteen.

import (
	"time"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

type Synth struct {
	app *gooey.App
	eng *Engine
	fps int

	// rev is bumped once per frame by the sampler. It is the whole
	// subscription surface of the visualiser.
	rev  *prop.Property[int]
	snap Snapshot

	wave   *prop.Property[string]
	octave *prop.Property[int]
	cutoff *prop.Property[int] // 0..100, because a slider label is a percent
	res    *prop.Property[int]
	status *prop.Property[string]

	held map[int]bool
}

func NewSynth(fps int) *Synth {
	if fps < 5 {
		fps = 5
	}
	if fps > 60 {
		fps = 60
	}
	return &Synth{
		eng:    NewEngine(),
		fps:    fps,
		rev:    prop.NewSource(0),
		wave:   prop.NewSource(Saw.String()),
		octave: prop.NewSource(4),
		cutoff: prop.NewSource(55),
		res:    prop.NewSource(25),
		status: prop.NewSource(""),
		held:   map[int]bool{},
	}
}

func (s *Synth) HeldNotes() map[int]bool { return s.held }

// --- the keyboard --------------------------------------------------

// key is one computer key mapped to a semitone offset. The layout is the
// tracker one — z-row for the lower octave, q-row for the upper — which
// is what anyone who has touched a DAW already has in their fingers.
type key struct {
	label rune
	note  int // semitones above the base C
	sharp bool
}

var keyboard = []key{
	{'z', 0, false}, {'s', 1, true}, {'x', 2, false}, {'d', 3, true}, {'c', 4, false},
	{'v', 5, false}, {'g', 6, true}, {'b', 7, false}, {'h', 8, true}, {'n', 9, false},
	{'j', 10, true}, {'m', 11, false},
	{'q', 12, false}, {'2', 13, true}, {'w', 14, false}, {'3', 15, true}, {'e', 16, false},
	{'r', 17, false}, {'5', 18, true}, {'t', 19, false}, {'6', 20, true}, {'y', 21, false},
	{'7', 22, true}, {'u', 23, false},
}

var noteNames = [12]string{"C", "C#", "D", "D#", "E", "F", "F#", "G", "G#", "A", "A#", "B"}

func noteName(n int) string { return noteNames[((n%12)+12)%12] }

// Press starts a note. Held notes are tracked here rather than in the
// engine because a terminal has no key-up event: there is no way to know
// a key was released, so every note gets a fixed lifetime and the
// keyboard display follows the same timer.
//
// That limitation is worth stating out loud rather than hiding, because
// it is the same class of thing as "mouse cannot be injected through a
// recording pty" — a property of the medium, not of the framework.
func (s *Synth) Press(note int) {
	freq := 440 * pow2((float64(note)+float64(s.octave.Get()-4)*12-9)/12)
	s.eng.NoteOn(note, freq)
	s.held[note] = true
}

func (s *Synth) CycleWave() {
	w := s.eng.CycleWave()
	s.wave.Set(w.String())
}

func (s *Synth) Octave(d int) {
	n := s.octave.Get() + d
	if n < 1 {
		n = 1
	}
	if n > 7 {
		n = 7
	}
	s.octave.Set(n)
}

func (s *Synth) Cutoff(d int) {
	n := clamp(s.cutoff.Get()+d, 0, 100)
	s.cutoff.Set(n)
	s.eng.SetCutoff(float64(n) / 100)
}

func (s *Synth) Res(d int) {
	n := clamp(s.res.Get()+d, 0, 100)
	s.res.Set(n)
	s.eng.SetRes(float64(n) / 100)
}

func (s *Synth) Quit() { s.app.Quit() }

// --- the frame clock ------------------------------------------------

// Sampler is the bridge. It is a Startable, so its goroutine is owned by
// the tree, and post is the only route it has to the graph.
type Sampler struct {
	gooey.Base
	synth *Synth
}

func (s *Sampler) Measure(gooey.Size) gooey.Size { return gooey.Size{} }
func (s *Sampler) Render(*gooey.Frame)           {}

func (s *Sampler) Start(post func(func())) (stop func()) {
	done := make(chan struct{})
	stopped := make(chan struct{})
	tick := time.NewTicker(time.Second / time.Duration(s.synth.fps))

	go func() {
		defer close(stopped)
		defer tick.Stop()
		for {
			select {
			case <-done:
				return
			case <-tick.C:
				// The Snapshot is taken HERE, off the UI goroutine,
				// because it takes the engine's mutex and the UI
				// goroutine must not block on an audio lock. What gets
				// posted is a value.
				snap := s.synth.eng.Snapshot()
				expired := s.synth.eng.Expired()
				post(func() {
					s.synth.snap = snap
					for _, n := range expired {
						delete(s.synth.held, n)
					}
					s.synth.rev.Set(s.synth.rev.Get() + 1)
				})
			}
		}
	}()
	// close AND join: close alone lets a tick that already won its select
	// post after Close, which is the lifetime flake this repo has fixed
	// three times.
	return func() { close(done); <-stopped }
}

func (s *Synth) Context() *markup.Context {
	ctx := &markup.Context{
		Values: map[string]any{
			// Every text binding is a string handle, and the int
			// handles stay behind computeds. A Text interpolates
			// strings; markup will not silently stringify an int for
			// you, which is the same rule that keeps a bound Style from
			// accepting a style NAME.
			"Wave":   s.wave,
			"Octave": prop.NewComputed(func() string { return itoa(s.octave.Get()) }),
			"Cutoff": prop.NewComputed(func() string { return itoa(s.cutoff.Get()) }),
			"Res":    prop.NewComputed(func() string { return itoa(s.res.Get()) }),
			"Status": s.status,
			"Peak": prop.NewComputed(func() int {
				s.rev.Get()
				return int(s.snap.Peak * 100)
			}),
			"Voices": prop.NewComputed(func() string {
				s.rev.Get()
				return itoa(s.snap.Active)
			}),

			"CycleWave": gooey.Command(s.CycleWave),
			"OctaveUp":  gooey.Command(func() { s.Octave(1) }),
			"OctaveDn":  gooey.Command(func() { s.Octave(-1) }),
			"CutoffUp":  gooey.Command(func() { s.Cutoff(6) }),
			"CutoffDn":  gooey.Command(func() { s.Cutoff(-6) }),
			"ResUp":     gooey.Command(func() { s.Res(6) }),
			"ResDn":     gooey.Command(func() { s.Res(-6) }),
			"Quit":      gooey.Command(s.Quit),
		},
		Styles: map[string]render.Style{
			"panel":    {Fg: render.RGB(90, 110, 150)},
			"headline": {Fg: render.RGB(240, 244, 252), Bold: true},
			"body":     {Fg: render.RGB(206, 212, 224)},
			"dim":      {Fg: render.RGB(118, 126, 142)},
			"hot":      {Fg: render.RGB(255, 190, 90), Bold: true},
			"warn":     {Fg: render.RGB(240, 120, 100), Bold: true},
		},
	}
	RegisterViz(ctx, s)
	ctx.Components["Sampler"] = func(e markup.Element, c *markup.Context) (gooey.Component, error) {
		return &Sampler{synth: s}, nil
	}
	ctx.Components["Board"] = func(e markup.Element, c *markup.Context) (gooey.Component, error) {
		return &Board{synth: s}, nil
	}
	return ctx
}

func clamp(n, lo, hi int) int {
	if n < lo {
		return lo
	}
	if n > hi {
		return hi
	}
	return n
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

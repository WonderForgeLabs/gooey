package components

import (
	"time"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

// Named frame sets. A spinner is a cycle of glyphs, so the set IS the
// design: braille reads as smooth motion, line as mechanical, arc as a
// sweep, dot as a pulse. They are exported so an app can start from one
// and hand back its own.
var (
	SpinnerBraille = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	SpinnerLine    = []string{"|", "/", "-", "\\"}
	SpinnerArc     = []string{"◜", "◠", "◝", "◞", "◡", "◟"}
	SpinnerDot     = []string{"·", "•", "●", "•"}
)

// SpinnerFrames resolves a frame set by name, for markup and anywhere
// else the choice arrives as text. The bool is the load-error signal:
// there is no "close enough" spinner.
func SpinnerFrames(name string) ([]string, bool) {
	switch name {
	case "braille":
		return SpinnerBraille, true
	case "line":
		return SpinnerLine, true
	case "arc":
		return SpinnerArc, true
	case "dot":
		return SpinnerDot, true
	}
	return nil, false
}

// SpinnerNames is every frame set SpinnerFrames accepts, for error
// messages that tell the author what they could have written.
var SpinnerNames = []string{"braille", "line", "arc", "dot"}

// SpinnerTick is the default frame interval.
const SpinnerTick = 100 * time.Millisecond

// Spinner is an activity indicator: one glyph from a cycling set, plus
// an optional label.
//
// It is the smallest possible Startable, and the clearest statement of
// the discipline. The ticker goroutine advances nothing itself — it
// posts a step to the dispatcher, the step runs on the UI loop, and only
// there does a property get Set. Render reads the frame index, so one
// tick repaints exactly this component and nothing else on the page
// notices.
//
// Enabled is read while painting rather than only at fire time, which is
// the one place Spinner differs from Timer: a paused spinner should
// *look* paused, so it parks at its first frame. That read is what makes
// the flip repaint it.
type Spinner struct {
	gooey.Base
	// Frames is the glyph cycle; empty means SpinnerBraille. It is a
	// plain field, not a property: the set is an author's declaration,
	// fixed for the life of the component like Timer's Interval.
	Frames   []string
	Interval time.Duration // 0 = SpinnerTick
	Label    *prop.Property[string]
	Style    *prop.Property[render.Style]
	Enabled  *prop.Property[bool] // nil = always spinning

	frame *prop.Property[int]
}

func (s *Spinner) frames() []string {
	if len(s.Frames) == 0 {
		return SpinnerBraille
	}
	return s.Frames
}

func (s *Spinner) frameProp() *prop.Property[int] {
	if s.frame == nil {
		s.frame = prop.NewSource(0)
	}
	return s.frame
}

func (s *Spinner) enabled() bool {
	return s.Enabled == nil || s.Enabled.Get()
}

func (s *Spinner) interval() time.Duration {
	if s.Interval <= 0 {
		return SpinnerTick
	}
	return s.Interval
}

// Glyph is the frame currently showing. Read from a Render it is a paint
// dependency; it is exported because a test — and an app that wants the
// same animation somewhere else — should not have to guess.
func (s *Spinner) Glyph() string {
	fr := s.frames()
	if !s.enabled() {
		return fr[0]
	}
	return fr[s.frameProp().Get()%len(fr)]
}

// Start runs the frame ticker until the returned stop func is called.
func (s *Spinner) Start(post func(func())) func() {
	// gooey.Every owns the close-and-join contract — see startable.go.
	return gooey.Every(post, s.interval(), s.step)
}

// step runs on the UI goroutine, having been posted there. A disabled
// spinner advances nothing, so pausing one costs no damage at all.
func (s *Spinner) step() {
	if !s.enabled() {
		return
	}
	s.frameProp().Set(s.frameProp().Get() + 1)
}

func (s *Spinner) Measure(avail gooey.Size) gooey.Size {
	w := 1
	if label := getStr(s.Label); label != "" {
		w += 1 + len([]rune(label))
	}
	return gooey.Size{W: min(w, avail.W), H: min(1, avail.H)}
}

func (s *Spinner) Render(f *gooey.Frame) {
	b := s.Bounds()
	if b.W <= 0 || b.H <= 0 {
		return
	}
	st := getSty(s.Style)
	f.Cells.SetString(b.X, b.Y, clipRunes(s.Glyph(), b.W), st)
	if label := getStr(s.Label); label != "" {
		if x := b.X + 2; x < b.X+b.W {
			f.Cells.SetString(x, b.Y, clipRunes(label, b.X+b.W-x), st)
		}
	}
}

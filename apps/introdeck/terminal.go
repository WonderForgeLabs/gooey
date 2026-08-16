package main

// A terminal, inside the terminal.
//
// The `(live)` beats used to say "the real app is on screen here" while
// showing nothing, which is the worst kind of slide: a caption for a
// thing that is not there. This is the thing.
//
// <Terminal Cmd="…"/> runs a child program on a pty, models its output,
// and paints the model as an island in the slide. It FILLS the space it
// is given and follows it: on a resize the guest gets a TIOCSWINSZ and a
// SIGWINCH, redraws itself, and the island reflows. A fixed 88×14
// rectangle inside a panel is not a slide, it is a screenshot with a
// process behind it — which is the whole of the sizing complaint.
//
// # Pieces that already existed
//
//   - render.Screen is a terminal emulator. Write([]byte) feeds it bytes
//     and it maintains a cell Buffer through CSI, SGR, erase and cursor
//     motion. It was written to model a pty log for tests; it turns out
//     to be a perfectly good VT.
//   - Startable is the legal way to own a goroutine. It receives post,
//     the ONLY route from that goroutine back to the property graph.
//   - The pty is pty_linux.go: three ioctls, no new dependency. A pty
//     library would be a whole graph for something the kernel already
//     exposes, and a terminal widget is not grounds for pulling one into
//     the root module.
//
// # The one property that matters
//
// rev is bumped when output arrives, and Render reads it. That read is
// the subscription — it is why bytes from a child process repaint this
// component and nothing else on the slide. There is no invalidate call
// here because there is nowhere to put one.

import (
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

// A squeezed island must not ask the guest for a 0×0 terminal: most
// full-screen programs answer that by drawing nothing, and a few answer
// it by exiting.
const (
	MinCols = 20
	MinRows = 4
)

type Terminal struct {
	gooey.Base
	gooey.FocusState

	// Cmd is a command LINE, run through sh -c. That is the element's
	// contract — Cmd="vim counter.gooey" has to work, and so does a
	// pipeline — and the string comes from the deck's own markup files,
	// which is to say from the same author as the rest of the page.
	// There is no privilege boundary here for an injection to cross: a
	// writer who can set this attribute can already edit the program.
	Cmd string

	// Cols and Rows are the STARTING size and the fallback for an
	// unbounded offer. The steady state is fill-and-follow.
	Cols, Rows int

	// Script is an optional keystroke schedule (see script.go). It plays
	// once, on its own goroutine, and stops the moment the component
	// does — a slide that has been advanced past must not still be
	// typing into a guest nobody can see.
	Script []Step

	// Loop replays the script from the top. Wanted by the ambient beats
	// (the green screen, the filter programs) and emphatically not by the
	// editor one, which ends in a joke that only works once.
	Loop bool

	scr  *render.Screen
	rev  *prop.Property[int]
	fail *prop.Property[string]

	// gone is set when the guest's output ends. It is a separate handle
	// from fail because they are different facts: fail is "this never
	// ran", gone is "this ran and finished", and on a slide the second
	// one is often the point.
	gone *prop.Property[bool]

	// live is input ownership, and it is a MODE rather than a focus state
	// on purpose.
	//
	// The obvious design was "focused ⇒ the guest gets the keys", with a
	// gesture that blurs to hand them back. That cannot be built:
	// FocusManager.SetFocus only accepts a component already in its
	// order and returns false for nil, so there is no way to clear focus
	// at all — and tab cannot rescue you either, because a Terminal that
	// is the only focus stop on a slide wraps to itself.
	//
	// So ownership is explicit, and the gesture pair is the one every
	// virtual machine has used for twenty years, because it is the one
	// people already know:
	//
	//	CLICK the island        → the guest gets keyboard and mouse
	//	ctrl+alt+<any key>      → you get them back
	//
	// The first version had only ctrl+] and no click, which failed twice
	// over: nothing on screen said the gesture existed, and ctrl+] did
	// not work at all because the decoder mapped 0x1d to ctrl+} (fixed
	// in input/decode.go). A capture mode with no visible way in is
	// indistinguishable from an app that ignores you — which is exactly
	// what it was reported as.
	//
	// ctrl+alt+KEY rather than bare ctrl+alt is not a choice. A terminal
	// reports keys, and a modifier held on its own is not a key: there is
	// no wire event for "ctrl and alt are down". The chord needs a third
	// key to ride on, and ctrl+alt+anything is a combination no TUI
	// program uses, so nothing is stolen from the guest.
	//
	// While the guest does not have input the island renders dim and
	// carries a hint — a mode you cannot see is a mode you get stuck in.
	live *prop.Property[bool]

	pty *pty
	cmd *exec.Cmd

	// takeover retires the typist the first time a person takes input.
	//
	// Two writers to one pty master is two byte streams into one stdin,
	// and the guest has no way to tell them apart: a presenter who clicks
	// into a Loop="true" island mid-script gets their keystrokes spliced
	// through whatever the script was in the middle of typing. Nothing is
	// corrupted and the demo is ruined, which is the worse of the two.
	//
	// So the human wins and the script stops — permanently, not until the
	// next loop. Someone who took the keyboard did not want to race it
	// back. A Once because both ways in (click, ctrl+]) reach it and both
	// run on the UI goroutine, but the typist reads it from its own.
	takeover     chan struct{}
	takeoverOnce sync.Once

	// the size last negotiated with the guest
	cols, rows int
}

// yield hands input to the person and retires the script. Safe to call
// on every keystroke; only the first one does anything.
func (t *Terminal) yield() {
	if t.takeover == nil {
		return
	}
	t.takeoverOnce.Do(func() { close(t.takeover) })
}

func NewTerminal(cmdline string, cols, rows int) *Terminal {
	return &Terminal{
		Cmd:  cmdline,
		Cols: cols,
		Rows: rows,
		scr:  render.NewScreen(cols, rows),
		rev:  prop.NewSource(0),
		fail: prop.NewSource(""),
		gone: prop.NewSource(false),
		live: prop.NewSource(false),
		cols: cols,
		rows: rows,
	}
}

// Measure asks for everything on offer. In a Grid star track or a
// stretched VStack that is the whole cell, which is the point: the
// island is as big as the slide lets it be, and it follows the window.
func (t *Terminal) Measure(avail gooey.Size) gooey.Size {
	return gooey.Size{W: fit(avail.W, t.Cols, MinCols), H: fit(avail.H, t.Rows, MinRows)}
}

// Arrange is where the guest is told, and Measure is NOT — which is the
// non-obvious half of this component.
//
// Grid measures its children against the FULL available box and only
// then arranges them into tracks (issue #208), so a Terminal in a `*,*`
// column is offered the whole width and given half of it. Resizing from
// the measured size would hand the guest a pty twice as wide as the box
// it is painted into, every frame, and the symptom would be a guest that
// renders correctly and is clipped down the middle.
//
// Arrange has the final rect. It runs on the UI goroutine and outside
// any evaluation context, so nothing below records a dependency edge.
// The repaint comes back around the long way, as bytes from a guest that
// redrew on SIGWINCH.
func (t *Terminal) Arrange(b gooey.Rect) {
	t.Base.Arrange(b)

	w, h := fit(b.W, t.Cols, MinCols), fit(b.H, t.Rows, MinRows)
	if w == t.cols && h == t.rows {
		return
	}
	t.cols, t.rows = w, h
	// Resize clears the model. That is correct: a full-screen guest
	// answers SIGWINCH with a full redraw, and painting the old content
	// stretched into the new box would be a lie during the gap.
	t.scr.Resize(w, h)
	if t.pty != nil {
		_ = t.pty.resize(w, h)
	}
}

// fit clamps one axis. An unbounded offer — a Grid Auto track, an HStack
// sizing to content — arrives as a huge number, and asking a pty for
// 32767 columns is not a resize, it is a memory event; that case falls
// back to the declared preferred size.
func fit(avail, pref, floor int) int {
	n := avail
	if n <= 0 || n > 1<<12 {
		n = pref
	}
	if n < floor {
		n = floor
	}
	return n
}

// Render blits the modelled screen. Reading rev is what subscribes this
// paint node to the guest's output; reading fail is what makes a launch
// error appear instead of a blank rectangle; reading live is what makes
// the dimming follow the mode.
func (t *Terminal) Render(f *gooey.Frame) {
	// Every Get first and unconditionally. One of these behind the early
	// return below would drop out of the dependency set on the frames
	// where it does not run, and the pane would go deaf to it — no
	// error, no panic, a stale rectangle.
	t.rev.Get()
	msg := t.fail.Get()
	live := t.live.Get()
	gone := t.gone.Get()

	b := t.Bounds()
	if msg != "" {
		f.Cells.SetString(b.X, b.Y, clip(msg, b.W), render.Style{Fg: render.RGB(240, 110, 90)})
		return
	}
	for y := 0; y < b.H && y < t.scr.Buf.H; y++ {
		for x := 0; x < b.W && x < t.scr.Buf.W; x++ {
			c := t.scr.Buf.At(x, y)
			st := c.Style
			if !live {
				st.Dim = true
				st.Bold = false
			}
			f.Cells.Set(b.X+x, b.Y+y, c.Rune, st)
		}
	}
	// The caret. gooey hides the terminal's real cursor for the whole
	// frame, so a guest's caret does not exist unless it is painted —
	// which is why every island read as a screenshot rather than as a
	// live program, prompt included.
	//
	// It is drawn as a REVERSE cell rather than as a glyph so it works
	// on top of whatever the guest already put there: a block over a
	// letter still shows the letter. Solid while captured, hollow
	// (underline) while inert, which makes the capture state legible
	// from the caret alone and not only from the dimming.
	if !gone && t.scr.CursorVisible() {
		if cx, cy := t.scr.Cursor(); cx < b.W && cy < b.H {
			c := t.scr.Buf.At(cx, cy)
			st := c.Style
			if live {
				st.Reverse = true
			} else {
				st.Underline, st.Dim = true, true
			}
			f.Cells.Set(b.X+cx, b.Y+cy, c.Rune, st)
		}
	}
	// After the blit, not before it: these go ON TOP of the guest's last
	// frame, and drawing them first would simply have them overwritten by
	// the very screen they annotate.
	if gone && b.H > 0 {
		f.Cells.SetString(b.X, b.Y+b.H-1, clip("— guest exited —", b.W),
			render.Style{Fg: render.RGB(150, 120, 90), Dim: true})
		return
	}
	// The affordance. Without it the capture gesture is a secret, and a
	// secret gesture is why this was reported as "slide 5 doesn't accept
	// input" — the app was fine, there was just no way to find out how to
	// talk to it. Shown only while INERT: once the guest has input the
	// undimmed island is the signal, and a caption pasted over a running
	// program is just damage.
	if !live && b.H > 0 && b.W > 6 {
		// The label names the chord EXACTLY. "ctrl+alt to release" was
		// what it said first, and it is not true — a terminal reports
		// keys, and holding two modifiers produces no event at all, so
		// the chord always needs a third key. Someone pressing ctrl+alt,
		// getting nothing, and then finding that ctrl+alt+q worked has
		// been taught the gesture by accident rather than by the label.
		hint := " click to capture input · ctrl+alt+any key to release "
		x := b.X + (b.W-len([]rune(hint)))/2
		if x < b.X {
			x = b.X
		}
		f.Cells.SetString(x, b.Y+b.H-1, clip(hint, b.W),
			render.Style{Fg: render.RGB(18, 20, 28), Bg: render.RGB(150, 165, 195), Bold: true})
	}
}

// Start opens the pty, launches the guest, and pumps its output into the
// model.
//
// The stop func closes AND joins. close alone lets a read that already
// won its race post after Close, which is the lifetime bug this repo has
// hit in three components; the idiom is components/timer.go's.
func (t *Terminal) Start(post func(func())) (stop func()) {
	stopped := make(chan struct{})

	p, err := openPty(t.cols, t.rows)
	if err != nil {
		t.fail.Set("pty: " + err.Error())
		close(stopped)
		return func() {}
	}
	cmd, err := p.start(t.Cmd)
	if err != nil {
		p.Close()
		t.fail.Set("cannot start: " + err.Error())
		close(stopped)
		return func() {}
	}
	t.pty, t.cmd = p, cmd

	// The typist. It writes to the pty master and never touches the
	// property graph, so it needs no post — the bytes come back round as
	// output and the reader below does the posting.
	//
	// It selects on done rather than just sleeping, because a script with
	// a four-second pause in it would otherwise keep the stop func
	// waiting four seconds, on the UI goroutine, for a slide that has
	// already gone.
	typed := make(chan struct{})
	done := make(chan struct{})
	t.takeover = make(chan struct{})
	go func() {
		defer close(typed)
		if len(t.Script) == 0 {
			return
		}
		for {
			for _, st := range t.Script {
				select {
				case <-done:
					return
				case <-t.takeover:
					// A person has the keyboard. Two writers to one pty
					// is two byte streams into one stdin.
					return
				case <-time.After(st.After):
				}
				// Re-checked after the wait, not only before it: the
				// takeover almost always lands DURING a step's pause,
				// and a check that only ran before the sleep would still
				// send this step's keystrokes into a guest someone else
				// is already typing into.
				select {
				case <-t.takeover:
					return
				default:
				}
				if _, err := p.master.Write(st.Send); err != nil {
					return
				}
			}
			if !t.Loop {
				return
			}
		}
	}()

	go func() {
		defer close(stopped)
		buf := make([]byte, 16384)
		for {
			n, err := p.master.Read(buf)
			if n > 0 {
				// Copy: the slice is reused on the next Read, and the
				// closure runs later, on the UI goroutine.
				chunk := append([]byte(nil), buf[:n]...)
				post(func() {
					_, _ = t.scr.Write(chunk)
					t.rev.Set(t.rev.Get() + 1)
				})
			}
			if err != nil {
				// The guest is gone. Say so, in the pane, instead of
				// leaving an empty rectangle — a full-screen program
				// clears the display on its way out, so "exited" and
				// "never started" look identical, and on a slide that
				// reads as a broken demo rather than a finished one.
				post(func() { t.gone.Set(true) })
				return
			}
		}
	}()

	return func() {
		close(done)
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		// Deadline first, then Close. The deadline is what actually wakes
		// a parked Read; Close alone raced with a read that had just
		// entered the poller.
		_ = p.master.SetReadDeadline(time.Now())
		p.Close()

		// Bounded join, the shape term.Screen.Restore uses. An unbounded
		// one is the correct barrier — Close ⇒ no further posts — right
		// up until the read cannot be woken, at which point it is a
		// permanently frozen UI, because stop runs ON the UI goroutine.
		// A bound turns the worst case from a hang into a leaked
		// goroutine and a line on the status bar.
		select {
		case <-stopped:
		case <-time.After(joinTimeout):
			t.fail.Set("terminal: reader did not stop")
		}
		select {
		case <-typed:
		case <-time.After(joinTimeout):
			t.fail.Set("terminal: typist did not stop")
		}
		_ = cmd.Wait()
	}
}

// joinTimeout bounds the wait for the reader goroutine. It is generous
// because exceeding it means something is wrong, not that something is
// slow: a woken read returns in microseconds.
const joinTimeout = 2 * time.Second

// isRelease is the way back out: ANY key held with both Ctrl and Alt.
//
// Any key, rather than one specific chord, because the label on screen
// says "ctrl+alt" and a user who presses ctrl+alt and then something —
// which is what people do — should get out rather than discover that
// only ctrl+alt+g counts. Nothing is taken from the guest: a TUI program
// that binds ctrl+alt+anything does not exist.
//
// ctrl+] is kept as a second door because it is a single-hand gesture
// and because it is what the earlier narration told people to press.
//
// Matched on fields rather than by comparing whole KeyEvents: a struct
// equality here fails silently the moment the decoder sets any other
// field, and the failure looks like "the key does nothing" rather than
// like a bug. That is not hypothetical — see input/decode.go, where
// ctrl+] arrived as ctrl+} for exactly one character's worth of
// arithmetic error.
func isRelease(ev input.KeyEvent) bool {
	if ev.Has(input.ModCtrl) && ev.Has(input.ModAlt) {
		return true
	}
	return ev.Key == input.KeyRune && ev.Rune == ']' && ev.Has(input.ModCtrl)
}

// HandleMouse is the way IN, and it is a click because that is what a
// person already reaches for when a rectangle contains another machine.
//
// A press anywhere on the island captures. The framework has already
// moved focus here by the time this runs (FocusManager.DispatchMouse
// focuses the press target), so the keyboard arrives without this having
// to reach for the focus manager.
//
// Once captured, presses are forwarded — but only if the guest asked for
// mouse reporting. Sending `ESC[<0;5;5M` to a program that never enabled
// tracking does not do nothing: the bytes land on its stdin and it types
// them into itself.
func (t *Terminal) HandleMouse(ev input.MouseEvent) bool {
	if !t.live.Get() {
		if ev.Kind == input.MousePress {
			t.live.Set(true)
			t.yield()
			return true
		}
		return false
	}
	if t.pty == nil || !t.scr.MouseTracking() {
		return ev.Kind == input.MousePress
	}
	b := t.encodeMouse(ev)
	if len(b) == 0 {
		return ev.Kind == input.MousePress
	}
	_, err := t.pty.master.Write(b)
	return err == nil
}

// encodeMouse renders an event in the guest's own coordinate space. The
// island's origin is subtracted and the result is 1-based, because that
// is what a terminal reports and the guest has no idea it is a guest.
func (t *Terminal) encodeMouse(ev input.MouseEvent) []byte {
	b := t.Bounds()
	x, y := ev.X-b.X+1, ev.Y-b.Y+1
	if x < 1 || y < 1 || x > b.W || y > b.H {
		return nil
	}
	code := 0
	switch ev.Button {
	case input.ButtonMiddle:
		code = 1
	case input.ButtonRight:
		code = 2
	}
	switch ev.Kind {
	case input.WheelUp:
		code = 64
	case input.WheelDown:
		code = 65
	case input.MouseMove:
		code += 32
	}
	if ev.Mods&input.ModShift != 0 {
		code += 4
	}
	if ev.Mods&input.ModAlt != 0 {
		code += 8
	}
	if ev.Mods&input.ModCtrl != 0 {
		code += 16
	}
	final := byte('M')
	if ev.Kind == input.MouseRelease {
		final = 'm'
	}
	// SGR (1006) only. The legacy X10 encoding cannot express a column
	// past 223, and every program that asks for tracking today also asks
	// for 1006 — so a guest that did not is one this refuses to guess at.
	if !t.scr.MouseSGR() {
		return nil
	}
	return []byte(fmt.Sprintf("\x1b[<%d;%d;%d%c", code, x, y, final))
}

// HandleKey forwards to the guest, but only while the guest owns the
// keyboard. Returning false the rest of the time is what keeps a slide
// with a terminal on it navigable.
func (t *Terminal) HandleKey(ev input.KeyEvent) bool {
	if isRelease(ev) {
		t.live.Set(!t.live.Get())
		if t.live.Get() {
			t.yield()
		}
		return true
	}
	if !t.live.Get() || t.pty == nil {
		return false
	}
	b := encode(ev)
	if len(b) == 0 {
		return false
	}
	_, err := t.pty.master.Write(b)
	return err == nil
}

// encode is a small VT encoder — enough for a full-screen editor, which
// is the most demanding guest this hosts.
func encode(ev input.KeyEvent) []byte {
	switch ev.Key {
	case input.KeyRune:
		if ev.Has(input.ModCtrl) {
			r := ev.Rune
			if r >= 'a' && r <= 'z' {
				return []byte{byte(r - 'a' + 1)}
			}
		}
		return []byte(string(ev.Rune))
	case input.KeyEnter:
		return []byte{'\r'}
	case input.KeyTab:
		// Forwarded, unlike the first version of this file, because vim
		// needs it and ctrl+] is now the way out. Tab was only reserved
		// back when blurring was the escape hatch, and blurring turned
		// out to be unbuildable.
		return []byte{'\t'}
	case input.KeyEsc:
		return []byte{0x1b}
	case input.KeyBackspace:
		return []byte{0x7f}
	case input.KeyDelete:
		return []byte("\x1b[3~")
	case input.KeyUp:
		return []byte("\x1b[A")
	case input.KeyDown:
		return []byte("\x1b[B")
	case input.KeyRight:
		return []byte("\x1b[C")
	case input.KeyLeft:
		return []byte("\x1b[D")
	case input.KeyHome:
		return []byte("\x1b[H")
	case input.KeyEnd:
		return []byte("\x1b[F")
	case input.KeyPageUp:
		return []byte("\x1b[5~")
	case input.KeyPageDown:
		return []byte("\x1b[6~")
	}
	return nil
}

// RegisterTerminal grants the deck's markup the <Terminal> element.
//
// It is registered on the CONTEXT rather than in markup's element
// vocabulary because it belongs to this example, not to the framework.
// A component that hosts a child process is a doctrine question for
// core; here it is one map entry.
func RegisterTerminal(ctx *markup.Context) {
	if ctx.Components == nil {
		ctx.Components = map[string]markup.Builder{}
	}
	ctx.Components["Terminal"] = func(e markup.Element, c *markup.Context) (gooey.Component, error) {
		cmdline := strings.TrimSpace(e.Attrs["Cmd"])
		if cmdline == "" {
			return nil, fmt.Errorf("markup: <Terminal> needs Cmd — the command line to run")
		}
		cols, err := attrInt(e, "Cols", 80)
		if err != nil {
			return nil, err
		}
		rows, err := attrInt(e, "Rows", 20)
		if err != nil {
			return nil, err
		}
		t := NewTerminal(cmdline, cols, rows)

		// The schedule is resolved HERE, at load, through the same fs.FS
		// the document came from. A missing or malformed script file is
		// then a load error like any other — which is the rule this
		// framework holds to everywhere: anything resolvable fails when
		// the page loads, never as a surprise mid-take.
		if src := strings.TrimSpace(e.Attrs["Script"]); src != "" {
			if c.Includes == nil {
				return nil, fmt.Errorf("markup: <Terminal Script=%q> needs a Context.Includes to resolve against", src)
			}
			steps, err := LoadScript(c.Includes, src)
			if err != nil {
				return nil, err
			}
			t.Script = steps
		}
		switch strings.TrimSpace(e.Attrs["Loop"]) {
		case "", "false":
		case "true":
			t.Loop = true
		default:
			return nil, fmt.Errorf("markup: <Terminal Loop=%q>: want true or false", e.Attrs["Loop"])
		}
		return t, nil
	}
}

func attrInt(e markup.Element, name string, def int) (int, error) {
	raw := strings.TrimSpace(e.Attrs[name])
	if raw == "" {
		return def, nil
	}
	n, err := atoi(raw)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("markup: <Terminal %s=%q>: want a positive cell count", name, raw)
	}
	return n, nil
}

func clip(s string, w int) string {
	r := []rune(s)
	if len(r) <= w {
		return s
	}
	if w <= 1 {
		return "…"
	}
	return string(r[:w-1]) + "…"
}

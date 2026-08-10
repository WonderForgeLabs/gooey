package components

import (
	"unicode"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

// TextBox is a single-line editor: a focus stop that owns printable
// runes and the editing keys while focused. Text is a shared property
// handle, so the viewmodel and the component edit the same value — the
// same two-way arrangement Checkbox uses for its bool.
//
// Promoted from cmd/finder's query line, which drove editing from the
// app's main loop. The framework version does its own key handling and
// adds a cursor: the caret is a source property, so moving it is
// ordinary paint damage and repaints only this component.
//
// Editing is mid-string, not append-only. The caret moves by character,
// by word (ctrl+arrow) and to either end; shift extends a selection from
// an anchor; typing, backspace and delete apply to the selection when
// there is one. Cut, copy and paste use a process-local kill buffer
// shared by every TextBox in the app — see KillBuffer, and note that
// this is NOT the system clipboard.
//
// The mouse places the caret, drags to select, and selects a word on
// double click. The drag is the pointer-capture machinery's first
// consumer: a press captures, so motion past the field's own bounds
// still arrives here and the selection keeps tracking.
//
// Changed, if set, runs after every edit. It exists because an edit
// usually invalidates something derived — finder resets its selection to
// the top whenever the query changes — and a command is a cheaper way to
// say that than making the caller watch the text property.
type TextBox struct {
	gooey.Base
	gooey.FocusState
	gooey.HoverState
	Text        *prop.Property[string]
	Prompt      *prop.Property[string]       // optional prefix, e.g. "> "
	Style       *prop.Property[render.Style] // the edited text
	AccentStyle *prop.Property[render.Style] // prompt and caret
	Changed     gooey.Action

	caret  *prop.Property[int]
	anchor *prop.Property[int] // selection anchor; noAnchor when there is none

	// scroll is the first visible rune index. It is derived state, not a
	// property: Render recomputes it from the caret and the width it is
	// already reading, so anything that can move it has dirtied this
	// paint node anyway and a property here would only add a second way
	// to say the same thing.
	scroll int
}

// noAnchor is the anchor value meaning "no selection". A real anchor is
// an index into the text, which is never negative.
const noAnchor = -1

func (t *TextBox) value() []rune { return []rune(getStr(t.Text)) }

func (t *TextBox) caretProp() *prop.Property[int] {
	if t.caret == nil {
		t.caret = prop.NewSource(0)
	}
	return t.caret
}

func (t *TextBox) anchorProp() *prop.Property[int] {
	if t.anchor == nil {
		t.anchor = prop.NewSource(noAnchor)
	}
	return t.anchor
}

// Caret is the insertion index, clamped on read: the bound text can
// change underneath the component (a viewmodel reset, a hot reload) and the
// caret must never point past the end.
func (t *TextBox) Caret() int { return clamp(t.caretProp().Get(), 0, len(t.value())) }

// SetCaret moves the insertion point and drops any selection.
func (t *TextBox) SetCaret(i int) {
	t.setCaret(i)
	t.clearSelection()
}

// setCaret and setAnchor compare before they Set. prop.Set does not —
// it invalidates every dependent unconditionally — so an unguarded write
// would repaint this component on every keystroke that did not actually
// move anything, left arrow at column zero included.
func (t *TextBox) setCaret(i int) { setInt(t.caretProp(), clamp(i, 0, len(t.value()))) }

func (t *TextBox) setAnchor(i int) { setInt(t.anchorProp(), i) }

func setInt(p *prop.Property[int], v int) {
	if p.Get() != v {
		p.Set(v)
	}
}

// Selection is the selected range as [lo, hi), and whether there is one.
// An anchor equal to the caret is not a selection — that is what a plain
// click leaves behind, ready for a shift+arrow to extend from.
func (t *TextBox) Selection() (lo, hi int, ok bool) {
	a := t.anchorProp().Get()
	if a == noAnchor {
		return 0, 0, false
	}
	n := len(t.value())
	a, c := clamp(a, 0, n), t.Caret()
	if a == c {
		return 0, 0, false
	}
	if a > c {
		a, c = c, a
	}
	return a, c, true
}

func (t *TextBox) clearSelection() { t.setAnchor(noAnchor) }

// anchorHere starts a selection at the caret if one is not already
// running. Every shift+movement calls it before moving, which is what
// makes the first shifted key define the anchor and the rest extend it.
func (t *TextBox) anchorHere() {
	if t.anchorProp().Get() == noAnchor {
		t.setAnchor(t.Caret())
	}
}

func (t *TextBox) Measure(avail gooey.Size) gooey.Size {
	return gooey.Size{W: avail.W, H: min(1, avail.H)}
}

func (t *TextBox) Render(f *gooey.Frame) {
	b := t.Bounds()
	if b.W <= 0 || b.H <= 0 {
		return
	}
	accent := getSty(t.AccentStyle)
	prompt := clipRunes(getStr(t.Prompt), b.W)
	runes := t.value()
	caret := t.Caret()
	lo, hi, selected := t.Selection()

	x := b.X
	if prompt != "" {
		f.Cells.SetString(x, b.Y, prompt, accent)
		x += len([]rune(prompt))
	}
	avail := b.X + b.W - x
	if avail <= 0 {
		return
	}
	// Scroll horizontally so the caret stays visible in a field narrower
	// than its content — on both sides, which is what mid-string editing
	// needs: walking left off the window has to pull it back, not just
	// walking right off the end.
	t.scroll = scrollFor(t.scroll, caret, len(runes), avail)

	textSty := getSty(t.Style)
	for i := t.scroll; i < len(runes) && x < b.X+b.W; i++ {
		st := textSty
		switch {
		case selected && i >= lo && i < hi:
			st.Reverse = true
		case !selected && t.IsFocused() && i == caret:
			st.Reverse = true // the caret sits ON the character it precedes
		}
		f.Cells.Set(x, b.Y, runes[i], st)
		x++
	}
	if t.IsFocused() && !selected && caret >= len(runes) && x < b.X+b.W {
		f.Cells.Set(x, b.Y, '█', accent)
	}
}

// scrollFor keeps caret inside a window of avail cells over n runes,
// moving the window as little as possible. The caret may sit one past
// the last rune, so the window has to be able to show n as a position.
func scrollFor(cur, caret, n, avail int) int {
	if avail <= 0 {
		return 0
	}
	if caret < cur {
		cur = caret
	}
	if caret > cur+avail-1 {
		cur = caret - avail + 1
	}
	if max := n - avail + 1; cur > max {
		cur = max
	}
	if cur < 0 {
		cur = 0
	}
	return cur
}

// HandleKey owns text editing while focused. Keys it does not use bubble
// on, so page gestures (enter to accept, esc to quit) keep working from
// inside the field.
func (t *TextBox) HandleKey(ev input.KeyEvent) bool {
	if t.Text == nil {
		return false
	}
	if t.moveKey(ev) {
		return true // a caret move is not an edit
	}
	if !t.editKey(ev) {
		return false
	}
	if gooey.CanExecute(t.Changed) {
		t.Changed.Run()
	}
	return true
}

// moveKey handles everything that only moves the caret or the selection.
// Shift extends from the anchor, ctrl moves by word, and the two compose.
func (t *TextBox) moveKey(ev input.KeyEvent) bool {
	// Anything carrying a modifier the box does not use — alt+left, say —
	// is somebody else's gesture and must keep bubbling.
	if ev.Mods&^(input.ModShift|input.ModCtrl) != 0 {
		return false
	}
	runes := t.value()
	caret := t.Caret()
	shift := ev.Has(input.ModShift)
	word := ev.Has(input.ModCtrl)

	var to int
	switch ev.Key {
	case input.KeyLeft:
		if word {
			to = wordLeft(runes, caret)
		} else if lo, _, ok := t.Selection(); ok && !shift {
			to = lo // an unshifted arrow collapses a selection to its edge
		} else {
			to = caret - 1
		}
	case input.KeyRight:
		if word {
			to = wordRight(runes, caret)
		} else if _, hi, ok := t.Selection(); ok && !shift {
			to = hi
		} else {
			to = caret + 1
		}
	case input.KeyHome:
		to = 0
	case input.KeyEnd:
		to = len(runes)
	default:
		return false
	}
	if shift {
		t.anchorHere()
		t.setCaret(to)
	} else {
		t.SetCaret(to)
	}
	return true
}

// editKey handles everything that changes the text. Each branch that
// touches a selection deletes it first, so "typing replaces the
// selection" is one rule rather than four.
func (t *TextBox) editKey(ev input.KeyEvent) bool {
	switch {
	case ev.Key == input.KeyRune && ev.Mods == 0:
		t.replace(ev.Rune)
	case ev == input.Named(input.KeyBackspace):
		if _, _, ok := t.Selection(); ok {
			t.deleteSelection()
			break
		}
		caret := t.Caret()
		if caret == 0 {
			return true // consumed: backspace at the start is a no-op, not a page gesture
		}
		runes := t.value()
		t.setText(append(append([]rune{}, runes[:caret-1]...), runes[caret:]...), caret-1)
	case ev == input.Named(input.KeyDelete):
		if _, _, ok := t.Selection(); ok {
			t.deleteSelection()
			break
		}
		caret, runes := t.Caret(), t.value()
		if caret >= len(runes) {
			return true
		}
		t.setText(append(append([]rune{}, runes[:caret]...), runes[caret+1:]...), caret)
	case ev == ctrlRune('x'):
		if !t.copySelection() {
			return true
		}
		t.deleteSelection()
	case ev == ctrlRune('c'):
		// Copy only claims the key when there IS something to copy. The
		// framework quit key is ctrl+c and it is checked on what bubbles
		// out of the tree, so an unconditional copy would make a focused
		// TextBox swallow every quit in the house style.
		if !t.copySelection() {
			return false
		}
		return true
	case ev == ctrlRune('v'):
		text := KillBuffer()
		if text == "" {
			return true
		}
		if _, _, ok := t.Selection(); ok {
			t.deleteSelection()
		}
		caret, runes := t.Caret(), t.value()
		ins := []rune(text)
		next := append(append(append([]rune{}, runes[:caret]...), ins...), runes[caret:]...)
		t.setText(next, caret+len(ins))
	default:
		return false
	}
	return true
}

// ctrlRune is the event a terminal sends for ctrl+letter.
func ctrlRune(r rune) input.KeyEvent {
	return input.KeyEvent{Key: input.KeyRune, Rune: r, Mods: input.ModCtrl}
}

func (t *TextBox) replace(r rune) {
	if _, _, ok := t.Selection(); ok {
		t.deleteSelection()
	}
	caret, runes := t.Caret(), t.value()
	next := append(append(append([]rune{}, runes[:caret]...), r), runes[caret:]...)
	t.setText(next, caret+1)
}

func (t *TextBox) deleteSelection() {
	lo, hi, ok := t.Selection()
	if !ok {
		return
	}
	runes := t.value()
	t.setText(append(append([]rune{}, runes[:lo]...), runes[hi:]...), lo)
}

// copySelection puts the selection in the kill buffer and reports
// whether there was one.
func (t *TextBox) copySelection() bool {
	lo, hi, ok := t.Selection()
	if !ok {
		return false
	}
	SetKillBuffer(string(t.value()[lo:hi]))
	return true
}

// setText is the one place the bound property is written: text, then
// caret, then the selection dropped, in that order so the caret clamps
// against the new value rather than the old one.
func (t *TextBox) setText(runes []rune, caret int) {
	t.Text.Set(string(runes))
	t.setCaret(caret)
	t.clearSelection()
}

// ---- kill buffer ----

// killBuffer is the process-local cut/copy target. It is UI-goroutine
// state like every other, so it needs no lock.
//
// It is deliberately not the system clipboard. Reaching the terminal's
// clipboard means OSC 52, which is a write-only channel on most
// terminals (you can set it, you cannot read it back), is disabled by
// default in several, and is a genuine exfiltration vector — so it is a
// decision to make on purpose, not a side effect of adding cut and
// paste. Until then, cut and copy move text between fields of the same
// app and nowhere else.
var killBuffer string

// KillBuffer is the text the last cut or copy put aside.
func KillBuffer() string { return killBuffer }

// SetKillBuffer replaces it. Exported so an app can seed the buffer, and
// so a future clipboard integration has one place to hook.
func SetKillBuffer(s string) { killBuffer = s }

// ---- mouse ----

// HandleMouse places the caret, starts a drag selection, and selects a
// word on double click.
func (t *TextBox) HandleMouse(ev input.MouseEvent) bool {
	switch ev.Kind {
	case input.MousePress:
		// The anchor is dropped at the press even though nothing is
		// selected yet: it costs nothing while the pointer has not moved
		// and it is what a drag extends from.
		i := t.indexAt(ev.X)
		t.setCaret(i)
		t.setAnchor(i)
		return true
	case input.MouseClick:
		if ev.Count >= 2 {
			t.selectWord()
		}
		return true
	}
	return false
}

// HandleMouseMove extends the selection while a button is held. It only
// ever runs for this component because the press captured the pointer,
// which is also why dragging past the field's edge keeps working.
func (t *TextBox) HandleMouseMove(ev input.MouseEvent) bool {
	if ev.Button == input.ButtonNone {
		return false
	}
	t.setCaret(t.indexAt(ev.X))
	return true
}

// indexAt maps a screen column to a rune index, through the prompt and
// the current horizontal scroll.
func (t *TextBox) indexAt(x int) int {
	promptW := len([]rune(clipRunes(getStr(t.Prompt), t.Bounds().W)))
	return clamp(t.scroll+x-t.Bounds().X-promptW, 0, len(t.value()))
}

// selectWord selects the run of like characters around the caret: a
// word, a stretch of whitespace, or a stretch of punctuation.
func (t *TextBox) selectWord() {
	runes := t.value()
	if len(runes) == 0 {
		return
	}
	i := clamp(t.Caret(), 0, len(runes)-1)
	c := class(runes[i])
	lo := i
	for lo > 0 && class(runes[lo-1]) == c {
		lo--
	}
	hi := i + 1
	for hi < len(runes) && class(runes[hi]) == c {
		hi++
	}
	t.setAnchor(lo)
	t.setCaret(hi)
}

// ---- word boundaries ----

// runeClass sorts a rune into the three kinds word motion cares about.
// Punctuation is its own class rather than a separator, so ctrl+left
// through "a.b" stops at the dot the way an editor does.
type runeClass uint8

const (
	classSpace runeClass = iota
	classWord
	classPunct
)

func class(r rune) runeClass {
	switch {
	case unicode.IsSpace(r):
		return classSpace
	case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_':
		return classWord
	}
	return classPunct
}

// wordLeft is the start of the word at or before i: skip whatever
// separates, then skip the run it lands in.
func wordLeft(runes []rune, i int) int {
	i = clamp(i, 0, len(runes))
	for i > 0 && class(runes[i-1]) == classSpace {
		i--
	}
	if i == 0 {
		return 0
	}
	c := class(runes[i-1])
	for i > 0 && class(runes[i-1]) == c {
		i--
	}
	return i
}

// wordRight is the end of the word at or after i.
func wordRight(runes []rune, i int) int {
	i = clamp(i, 0, len(runes))
	for i < len(runes) && class(runes[i]) == classSpace {
		i++
	}
	if i == len(runes) {
		return i
	}
	c := class(runes[i])
	for i < len(runes) && class(runes[i]) == c {
		i++
	}
	return i
}

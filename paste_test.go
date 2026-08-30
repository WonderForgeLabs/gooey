package gooey_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

// pasteSink is a focusable leaf that records what it was handed. It
// paints its own text so it has a real paint node, which is what the
// damage assertions below are counting.
type pasteSink struct {
	gooey.Base
	gooey.FocusState
	got   *prop.Property[string]
	take  bool
	calls int
}

func newPasteSink(take bool) *pasteSink {
	return &pasteSink{got: prop.NewSource(""), take: take}
}

func (p *pasteSink) Measure(a gooey.Size) gooey.Size { return gooey.Size{W: a.W, H: 1} }
func (p *pasteSink) Render(f *gooey.Frame) {
	b := p.Bounds()
	f.Cells.SetString(b.X, b.Y, p.got.Get(), render.Style{})
}
func (p *pasteSink) HandlePaste(ev input.PasteEvent) bool {
	p.calls++
	if !p.take {
		return false
	}
	p.got.Set(ev.Text)
	return true
}

// pasteBox is a container that is NOT a PasteHandler, so a paste routed
// to its child bubbles straight past it.
type pasteBox struct {
	gooey.Base
	kids []gooey.Component
}

func (b *pasteBox) ChildComponents() []gooey.Component { return b.kids }
func (b *pasteBox) Measure(a gooey.Size) gooey.Size {
	for _, k := range b.kids {
		gooey.MeasureChild(k, a)
	}
	return a
}
func (b *pasteBox) Arrange(r gooey.Rect) {
	b.Base.Arrange(r)
	for i, k := range b.kids {
		gooey.ArrangeChild(k, gooey.Rect{X: r.X, Y: r.Y + i, W: r.W, H: 1})
	}
}
func (b *pasteBox) Render(*gooey.Frame) {}

func TestPasteReachesTheFocusedComponent(t *testing.T) {
	a, bx := newPasteSink(true), newPasteSink(true)
	root := &pasteBox{kids: []gooey.Component{a, bx}}
	c := gooey.NewComposer(root, 20, 4)
	c.Frame()
	c.Focus().SetFocus(bx)

	if !c.Handle(input.PasteOf(input.PasteEvent{Text: "payload"})) {
		t.Fatal("the paste was not consumed")
	}
	if bx.got.Get() != "payload" {
		t.Errorf("focused component got %q, want %q", bx.got.Get(), "payload")
	}
	// The discriminating half: it went to the FOCUSED one, not to the
	// first handler in the tree.
	if a.calls != 0 {
		t.Errorf("the unfocused component was offered the paste %d times", a.calls)
	}
}

func TestPasteBubblesToAnAncestorWhenTheTargetDeclines(t *testing.T) {
	leaf := newPasteSink(false) // declines
	outer := &pasteAncestor{pasteBox: pasteBox{}}
	outer.kids = []gooey.Component{leaf}
	c := gooey.NewComposer(outer, 20, 4)
	c.Frame()
	c.Focus().SetFocus(leaf)

	if !c.Handle(input.PasteOf(input.PasteEvent{Text: "up"})) {
		t.Fatal("a declined paste did not bubble to the ancestor")
	}
	if leaf.calls != 1 {
		t.Errorf("the target was offered it %d times, want 1", leaf.calls)
	}
	if outer.got != "up" {
		t.Errorf("ancestor got %q, want %q", outer.got, "up")
	}
}

type pasteAncestor struct {
	pasteBox
	got string
}

func (p *pasteAncestor) HandlePaste(ev input.PasteEvent) bool { p.got = ev.Text; return true }

func TestUnhandledPasteReportsFalseRatherThanLookingConsumed(t *testing.T) {
	// This false is what tells App.handle the tree did not want it. A
	// dispatch that returned true unconditionally would make "nothing
	// implements PasteHandler" indistinguishable from "something took
	// it", which is the state in which a dropped paste has no symptom.
	leaf := newPasteSink(false)
	root := &pasteBox{kids: []gooey.Component{leaf}}
	c := gooey.NewComposer(root, 20, 4)
	c.Frame()
	c.Focus().SetFocus(leaf)

	if c.Handle(input.PasteOf(input.PasteEvent{Text: "nobody wants this"})) {
		t.Error("an unhandled paste reported itself consumed")
	}
}

// A paste is NOT a key, and Composer.Handle must not route it as one.
// Before the switch in Handle was exhaustive, the default arm was the
// key arm: a paste dispatched as ev.Key is a zero KeyEvent — KeyRune,
// rune 0 — which matches no binding, consumes nothing and reports
// nothing. The paste vanished with no error anywhere.
func TestPasteIsNotDispatchedAsAKey(t *testing.T) {
	leaf := &pasteKeySpy{}
	root := &pasteBox{kids: []gooey.Component{leaf}}
	c := gooey.NewComposer(root, 20, 4)
	c.Frame()
	c.Focus().SetFocus(leaf)

	c.Handle(input.PasteOf(input.PasteEvent{Text: "text"}))
	if leaf.keys != 0 {
		t.Fatalf("HandleKey was called %d times for a paste (with %+v)", leaf.keys, leaf.lastKey)
	}
	if leaf.pastes != 1 {
		t.Fatalf("HandlePaste was called %d times, want 1", leaf.pastes)
	}
}

type pasteKeySpy struct {
	gooey.Base
	gooey.FocusState
	keys, pastes int
	lastKey      input.KeyEvent
}

func (p *pasteKeySpy) Measure(a gooey.Size) gooey.Size { return gooey.Size{W: a.W, H: 1} }
func (p *pasteKeySpy) Render(*gooey.Frame)             {}
func (p *pasteKeySpy) HandleKey(ev input.KeyEvent) bool {
	p.keys++
	p.lastKey = ev
	return true
}
func (p *pasteKeySpy) HandlePaste(input.PasteEvent) bool { p.pastes++; return true }

// A paste repaints exactly the component that consumed it.
func TestPasteRepaintsOnlyTheConsumer(t *testing.T) {
	a, bx := newPasteSink(true), newPasteSink(true)
	root := &pasteBox{kids: []gooey.Component{a, bx}}
	c := gooey.NewComposer(root, 20, 4)
	c.Frame() // first frame paints everything

	c.Focus().SetFocus(bx)
	c.Frame()

	c.Handle(input.PasteOf(input.PasteEvent{Text: "one"}))
	if _, n := c.Frame(); n != 1 {
		t.Fatalf("a paste repainted %d components, want exactly 1 (the consumer)", n)
	}
	// And a paste that changes nothing costs nothing: prop.Set does not
	// compare values, so this is the SAME text and still dirties the
	// node — which is the documented trap, asserted here so the number
	// is not mistaken for value-equality.
	c.Handle(input.PasteOf(input.PasteEvent{Text: "one"}))
	if _, n := c.Frame(); n != 1 {
		t.Fatalf("re-pasting the same text repainted %d, want 1 (Set does not compare)", n)
	}
}

// TextBox implements PasteHandler, which is what keeps bracketed paste
// being ON by default from silently breaking every text field: with the
// mode on, a paste arrives as one event nothing else would consume.
func TestTextBoxConsumesAPasteAndFlattensIt(t *testing.T) {
	text := prop.NewSource("")
	tb := &components.TextBox{Text: text}
	root := &pasteBox{kids: []gooey.Component{tb}}
	c := gooey.NewComposer(root, 40, 4)
	c.Frame()
	c.Focus().SetFocus(tb)

	if !c.Handle(input.PasteOf(input.PasteEvent{Text: "one\ntwo\tthree\x00"})) {
		t.Fatal("TextBox did not consume the paste")
	}
	// A single-line field has no way to show a second line, so newlines
	// and tabs become spaces and other control bytes are dropped rather
	// than inserted invisibly into the value.
	if got := text.Get(); got != "one two three" {
		t.Errorf("value = %q, want %q", got, "one two three")
	}
}

// declaredEventKinds counts the EventKind constants the input package
// declares, by parsing them rather than by keeping a number here.
//
// DERIVED FOR THE USUAL REASON: a number written beside the thing it
// counts agrees with nothing. The test below needs a kind that does NOT
// exist, and "one past the last declared" is the only spelling of that
// which stays true when a kind is added — which is the moment the test
// exists for.
func declaredEventKinds(t *testing.T) int {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filepath.Join("input", "mouse.go"), nil, 0)
	if err != nil {
		t.Fatalf("parsing input/mouse.go: %v", err)
	}
	n := 0
	for _, d := range f.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		block := false
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			// The type appears on the iota line only, so the whole block
			// belongs to EventKind once its first spec names it.
			if id, ok := vs.Type.(*ast.Ident); ok && id.Name == "EventKind" {
				block = true
			}
			if block {
				n += len(vs.Names)
			}
		}
	}
	if n < 3 {
		t.Fatalf("found %d EventKind constants in input/mouse.go, want at least the "+
			"three that exist — the parse is broken, not the vocabulary", n)
	}
	return n
}

// TestAnUnknownEventKindIsNotDispatchedAsAKey is the guard on Composer.Handle's
// switch, and it is the assertion its comment already claimed.
//
// The switch routes mouse and paste and had `default` fall through to
// Dispatch(ev.Key) — so a kind it did not know about arrived as a ZERO
// KeyEvent, which is a KeyRune of rune 0: a plausible-looking key that
// matches nothing and reports nothing. That is precisely how a paste was
// swallowed before EventPaste got an arm, and the comment describing
// that fix sat directly above the arm that kept it possible. Found in
// the review of #391 (issue #419).
//
// The unknown kind is DERIVED as one past the last declared, so adding a
// fourth kind without an arm in Handle fails here instead of silently
// becoming rune 0.
func TestAnUnknownEventKindIsNotDispatchedAsAKey(t *testing.T) {
	leaf := &pasteKeySpy{}
	root := &pasteBox{kids: []gooey.Component{leaf}}
	c := gooey.NewComposer(root, 20, 4)
	c.Frame()
	c.Focus().SetFocus(leaf)

	// The floor: a real key still reaches the component, so a "nothing
	// arrived" result below means the routing refused it rather than the
	// fixture being deaf.
	if !c.Handle(input.KeyOf(input.Named(input.KeyEnter))) {
		t.Fatal("a key event was not consumed, so this fixture proves nothing")
	}
	if leaf.keys != 1 {
		t.Fatalf("HandleKey ran %d times for one key event, want 1", leaf.keys)
	}

	unknown := input.EventKind(declaredEventKinds(t))
	if got := c.Handle(input.Event{Kind: unknown}); got {
		t.Errorf("Handle reported an unknown event kind %d as consumed", unknown)
	}
	if leaf.keys != 1 {
		t.Errorf("an unknown event kind reached HandleKey as %+v — the default arm is "+
			"still the key arm, so the kind arrived as a zero KeyEvent (a KeyRune of "+
			"rune 0) that matches nothing and reports nothing", leaf.lastKey)
	}
	if leaf.pastes != 0 {
		t.Errorf("an unknown event kind reached HandlePaste %d times", leaf.pastes)
	}
}

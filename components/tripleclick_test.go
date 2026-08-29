package components

import (
	"testing"
	"time"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/prop"
)

// Triple-click selects the line, which is the third step of the
// progressive selection every text field shares: caret, word, line.
//
// Driven IN-PROCESS through Composer.HandleMouse, not through a pty.
// Mouse reports cannot be injected through a recording pty at all, so
// the click path has no capture-based test anywhere in this repo and
// this is the only way to exercise it.

// clickBox stands up a focused TextBox on a composer with a controllable
// clock, and returns a func that performs one click on it.
func clickBox(t *testing.T, value string) (*TextBox, *gooey.Composer, func()) {
	t.Helper()
	text := prop.NewSource(value)
	tb := &TextBox{Text: text}
	c := gooey.NewComposer(&VStack{Children: []gooey.Component{tb}}, 40, 3)
	c.Frame()
	fm := c.Focus()
	now := time.Unix(0, 0)
	fm.Now = func() time.Time { return now }
	fm.SetFocus(tb)

	click := func() {
		b := tb.Bounds()
		// Land mid-word so the double-click case has a word to find:
		// column 6 of "hello brave world" is inside "brave".
		x := b.X + 6
		c.HandleMouse(press(x, b.Y))
		c.HandleMouse(release(x, b.Y))
		now = now.Add(100 * time.Millisecond)
	}
	return tb, c, click
}

func TestClickWordLineIsProgressive(t *testing.T) {
	tb, _, click := clickBox(t, "hello brave world")

	click()
	if got := selText(tb); got != "" {
		t.Errorf("one click selected %q, want nothing (it places the caret)", got)
	}
	click()
	if got := selText(tb); got != "brave" {
		t.Errorf("double click selected %q, want the word under the caret", got)
	}
	click()
	if got := selText(tb); got != "hello brave world" {
		t.Errorf("triple click selected %q, want the whole line", got)
	}
}

// The discriminating half of the above: a triple click must select MORE
// than the double click did. Without this, an implementation where
// triple fell through to selectWord passes every "it selected
// something" assertion.
func TestTripleClickSelectsMoreThanDouble(t *testing.T) {
	tb, _, click := clickBox(t, "hello brave world")
	click()
	click()
	double := selText(tb)
	click()
	triple := selText(tb)

	if double == triple {
		t.Fatalf("triple click selected the same range as double (%q); it fell through to selectWord", double)
	}
	if len(triple) <= len(double) {
		t.Errorf("triple selected %q (%d) which is not wider than double's %q (%d)",
			triple, len(triple), double, len(double))
	}
}

// A fourth rapid click starts over, so the gesture cycles rather than
// sticking at "everything selected" forever.
func TestAFourthClickStartsANewSequence(t *testing.T) {
	tb, _, click := clickBox(t, "hello brave world")
	click()
	click()
	click()
	if got := selText(tb); got != "hello brave world" {
		t.Fatalf("setup: triple click selected %q", got)
	}
	click() // the fourth
	if got := selText(tb); got != "" {
		t.Errorf("a fourth click left %q selected, want the caret placed and the sequence restarted", got)
	}
}

// The interval is measured click-to-click, not from the start of the
// run: a slow third click begins a new sequence rather than completing a
// triple. Without this the gesture would be a race against a deadline
// that started two clicks ago.
func TestASlowThirdClickIsNotATriple(t *testing.T) {
	text := prop.NewSource("hello brave world")
	tb := &TextBox{Text: text}
	c := gooey.NewComposer(&VStack{Children: []gooey.Component{tb}}, 40, 3)
	c.Frame()
	fm := c.Focus()
	now := time.Unix(0, 0)
	fm.Now = func() time.Time { return now }
	fm.SetFocus(tb)
	click := func() {
		b := tb.Bounds()
		c.HandleMouse(press(b.X+6, b.Y))
		c.HandleMouse(release(b.X+6, b.Y))
	}

	click()
	now = now.Add(100 * time.Millisecond)
	click()
	if got := selText(tb); got != "brave" {
		t.Fatalf("setup: double click selected %q", got)
	}
	now = now.Add(time.Second) // past the interval
	click()
	if got := selText(tb); got == "hello brave world" {
		t.Error("a slow third click completed a triple; the interval must be measured click-to-click")
	}
}

// An empty box reports no selection after a triple click. Not because
// selectLine guards against it — it does not, deliberately (see its
// doc) — but because an anchor equal to the caret IS "no selection" to
// Selection(). The test is here to pin that outcome, so that a future
// change to Selection() treating a zero-width range as real would show
// up as a triple click on an empty field claiming a selection.
func TestTripleClickOnAnEmptyBoxSelectsNothing(t *testing.T) {
	tb, _, click := clickBox(t, "")
	click()
	click()
	click()
	if _, _, ok := tb.Selection(); ok {
		t.Error("triple click on an empty box reported a selection")
	}
}

// DAMAGE: a triple click repaints the box it selected in, and nothing
// else. Selection is ordinary paint damage because the anchor and caret
// are source properties, so this is the same guarantee focus and hover
// carry — asserted as an exact count, since a bounds or cell assertion
// passes just as well when the whole tree repainted.
func TestTripleClickRepaintsOnlyTheBox(t *testing.T) {
	text := prop.NewSource("hello brave world")
	tb := &TextBox{Text: text}
	other := &TextBox{Text: prop.NewSource("untouched")}
	c := gooey.NewComposer(&VStack{Children: []gooey.Component{tb, other}}, 40, 4)
	c.Frame()
	fm := c.Focus()
	now := time.Unix(0, 0)
	fm.Now = func() time.Time { return now }
	fm.SetFocus(tb)
	c.Frame()

	click := func() {
		b := tb.Bounds()
		c.HandleMouse(press(b.X+6, b.Y))
		c.HandleMouse(release(b.X+6, b.Y))
		now = now.Add(100 * time.Millisecond)
	}

	click()
	c.Frame()
	click()
	c.Frame()

	click() // the triple
	_, n := c.Frame()
	if n != 1 {
		t.Fatalf("a triple click repainted %d components, want exactly 1 (the box it selected in)", n)
	}
	if got := selText(tb); got != "hello brave world" {
		t.Fatalf("the triple did not select the line (got %q); the count above proves nothing", got)
	}
}

// MaxClickCount is the ceiling the dispatcher enforces, and it is
// exported so a consumer can write `ev.Count >= gooey.MaxClickCount`
// rather than a bare 3.
func TestMaxClickCountIsTheCeilingActuallyEnforced(t *testing.T) {
	if gooey.MaxClickCount != 3 {
		t.Fatalf("MaxClickCount = %d; this test's fixtures assume 3", gooey.MaxClickCount)
	}
	a, _, c := twoPanes(t)
	fm := c.Focus()
	now := time.Unix(0, 0)
	fm.Now = func() time.Time { return now }

	for i := 0; i < 8; i++ {
		y := a.Bounds().Y
		c.HandleMouse(press(2, y))
		c.HandleMouse(release(2, y))
		now = now.Add(50 * time.Millisecond)
	}
	for _, e := range a.got {
		if e.Kind == input.MouseClick && e.Count > gooey.MaxClickCount {
			t.Fatalf("a click reported count %d, above the ceiling %d", e.Count, gooey.MaxClickCount)
		}
	}
}

// The interval is measured CLICK-TO-CLICK, not from the start of the
// run. The distinction only shows up in a window the obvious test
// misses: three clicks 300ms apart, with a 400ms interval, are a triple
// under the real rule (each gap is inside the interval) and are NOT one
// if the deadline is measured from the first click (600ms elapsed).
//
// Written after a mutation run: TestASlowThirdClickIsNotATriple uses a
// one-second gap, which both rules reject, so it did not discriminate
// and the start-of-run mutant stayed green.
func TestTheIntervalIsMeasuredBetweenConsecutiveClicks(t *testing.T) {
	text := prop.NewSource("hello brave world")
	tb := &TextBox{Text: text}
	c := gooey.NewComposer(&VStack{Children: []gooey.Component{tb}}, 40, 3)
	c.Frame()
	fm := c.Focus()
	now := time.Unix(0, 0)
	fm.Now = func() time.Time { return now }
	fm.SetFocus(tb)

	gap := 300 * time.Millisecond
	if gap <= gooey.DefaultDoubleClickInterval/2 || 2*gap <= gooey.DefaultDoubleClickInterval {
		t.Fatalf("the fixture does not straddle the boundary: gap %v, interval %v",
			gap, gooey.DefaultDoubleClickInterval)
	}
	click := func() {
		b := tb.Bounds()
		c.HandleMouse(press(b.X+6, b.Y))
		c.HandleMouse(release(b.X+6, b.Y))
	}

	click()
	now = now.Add(gap)
	click()
	now = now.Add(gap) // 600ms since the FIRST click, 300ms since the last
	click()

	if got := selText(tb); got != "hello brave world" {
		t.Errorf("three clicks %v apart selected %q, want the whole line.\n"+
			"Each gap is inside the %v interval, so this is a triple; measuring the "+
			"deadline from the start of the run would reject it at %v elapsed.",
			gap, got, gooey.DefaultDoubleClickInterval, 2*gap)
	}
}

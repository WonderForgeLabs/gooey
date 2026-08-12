package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/markup"
)

// The editor's minimum size, and the failure it used to produce.

// shellGrid builds the real page and returns its <Grid Name="Shell">, so
// every number below comes from the shipped markup rather than from a
// copy of it in a test.
func shellGrid(t *testing.T) (*editor, *components.Grid, gooey.Component) {
	t.Helper()
	ed, root := buildShell(t)
	g, err := markup.Find[*components.Grid](ed.ctx, "Shell")
	if err != nil {
		t.Fatal(err)
	}
	return ed, g, root
}

// TestTheShellIsArrangedOffScreenBelowItsHardMinimum is the RED half,
// kept, and it is the test that corrected the design.
//
// It asserts the DEFECT, not the fix: one row below the hard minimum,
// the shell arranges children past the bottom of the screen. Nothing
// errors, nothing is clipped at the Grid, and the status bar is simply
// not there.
//
// The first version of this test used the USABLE minimum and skipped —
// nothing overflows at 16 rows, because 14 fixed + 2 left for the star
// track still fits. That skip is what revealed there are two different
// numbers with two different mechanisms, and that the file's explanation
// had welded one to the other. A skip is not a pass.
func TestTheShellIsArrangedOffScreenBelowItsHardMinimum(t *testing.T) {
	_, shell, _ := shellGrid(t)
	hard, usable, err := minimumFor(shell)
	if err != nil {
		t.Fatal(err)
	}
	if hard.Rows >= usable.Rows {
		t.Fatalf("hard %s is not below usable %s: the two-tier model has "+
			"collapsed and the tests below cannot tell them apart", hard, usable)
	}

	short := hard.Rows - 1
	shell.Measure(gooey.Size{W: hard.Cols, H: short})
	shell.Arrange(gooey.Rect{X: 0, Y: 0, W: hard.Cols, H: short})

	var over []string
	for _, ch := range shell.ChildComponents() {
		b, ok := ch.(gooey.Bounded)
		if !ok {
			continue
		}
		if r := b.Bounds(); r.Y+r.H > short {
			over = append(over, fmt.Sprintf("%T%+v", ch, r))
		}
	}
	if len(over) == 0 {
		t.Fatalf("nothing overflowed at %dx%d, one row below the hard minimum: "+
			"either Grid now clamps its fixed tracks (in which case this red "+
			"test has outlived its defect and should go) or the hard minimum "+
			"is computed wrong", hard.Cols, short)
	}
	t.Logf("at %dx%d, %d children are arranged past the bottom: %v",
		hard.Cols, short, len(over), over)

	// And the control: AT the hard minimum, nothing overflows. Without it
	// the assertion above would also pass for a shell that overflows at
	// every size.
	shell.Measure(gooey.Size{W: hard.Cols, H: hard.Rows})
	shell.Arrange(gooey.Rect{X: 0, Y: 0, W: hard.Cols, H: hard.Rows})
	for _, ch := range shell.ChildComponents() {
		b, ok := ch.(gooey.Bounded)
		if !ok {
			continue
		}
		if r := b.Bounds(); r.Y+r.H > hard.Rows {
			t.Errorf("%T is arranged past the bottom AT the hard minimum %s: %+v",
				ch, hard, r)
		}
	}
}

// TestBothMinimumsComeFromTheDeclaredTracks — a hardcoded number here
// would be a second copy of a fact wysiwyg.gooey already states, and the
// second copy is the one that goes stale.
//
// It also pins the DIFFERENCE between the two tiers, which is the part
// that was wrong before: a star track contributes nothing to the hard
// minimum (it absorbs the shortfall down to zero) and starMin to the
// usable one.
func TestBothMinimumsComeFromTheDeclaredTracks(t *testing.T) {
	_, shell, _ := shellGrid(t)
	hard, usable, err := minimumFor(shell)
	if err != nil {
		t.Fatal(err)
	}

	wantHard, wantUsable := fitSize{}, fitSize{}
	stars := 0
	for _, d := range shell.Rows {
		if d.Star > 0 {
			stars++
			wantUsable.Rows += starMin
			continue
		}
		wantHard.Rows += d.Fixed
		wantUsable.Rows += d.Fixed
	}
	for _, d := range shell.Cols {
		if d.Star > 0 {
			stars++
			wantUsable.Cols += starMin
			continue
		}
		wantHard.Cols += d.Fixed
		wantUsable.Cols += d.Fixed
	}
	if hard != wantHard || usable != wantUsable {
		t.Errorf("hard %s usable %s, want %s and %s from the declared tracks",
			hard, usable, wantHard, wantUsable)
	}
	// Preconditions, or every assertion here is satisfied by a shell with
	// no star tracks and no fixed ones.
	if stars == 0 {
		t.Fatal("the shell declares no star track: the two tiers coincide " +
			"and nothing here distinguishes them")
	}
	if hard.Rows < 10 || hard.Cols < 40 {
		t.Errorf("hard minimum %s is implausibly small: the check would never "+
			"trigger and would prove nothing", hard)
	}
}

// TestAnAutoTrackIsRefusedRatherThanGuessed. Every way of guessing an
// Auto track's extent makes the minimum too SMALL, which means the fit
// check passes at a size that does not fit — the silent misfit this
// whole file removes, reintroduced by the fix.
func TestAnAutoTrackIsRefusedRatherThanGuessed(t *testing.T) {
	g := &components.Grid{
		Rows: []components.GridLen{components.Fixed(1), components.Auto()},
		Cols: []components.GridLen{components.Star(1)},
	}
	if _, _, err := minimumFor(g); err == nil {
		t.Fatal("an Auto track must be refused, not guessed")
	} else if !strings.Contains(err.Error(), "Auto") {
		t.Errorf("the error must name the problem, got: %v", err)
	}
	// Control: without the Auto track the same shape computes fine.
	g.Rows = []components.GridLen{components.Fixed(1), components.Star(1)}
	if _, _, err := minimumFor(g); err != nil {
		t.Fatalf("the control failed too, so the refusal proved nothing: %v", err)
	}
}

// TestOnlyOneRootIsVisibleAtATime — the swap is two bound Visibilities
// over ONE fact, and the failure mode of two independent sources is a
// frame showing both roots or neither.
func TestOnlyOneRootIsVisibleAtATime(t *testing.T) {
	ed, shell, root := shellGrid(t)
	msg := findByStyle(root, "warn")
	if msg == nil {
		t.Fatal("no cramped-message component in the page")
	}

	vis := func(w gooey.Component) gooey.Visibility {
		l := gooey.LayoutOf(w)
		if l == nil {
			t.Fatalf("%T carries no Layout", w)
		}
		return l.Visibility
	}
	// A bound Visibility is synced by the framework at defined points;
	// composing a frame is the one every consumer relies on.
	c := gooey.NewComposer(root, 100, 40)

	ed.fits.Set(true)
	c.Frame()
	if vis(shell) != gooey.Visible || vis(msg) == gooey.Visible {
		t.Errorf("fits: shell=%v msg=%v, want the shell alone", vis(shell), vis(msg))
	}
	// The swap wraps the shell in a <Canvas>, and a Canvas arranges each
	// child at its MEASURED size rather than stretching it. If the Grid
	// ever stops asking for everything it is offered, the whole editor
	// silently shrinks into a corner and every other test still passes.
	if b := shell.Bounds(); b.W != 100 || b.H != 40 {
		t.Errorf("the shell is %+v, want the full 100x40: the Canvas wrapper "+
			"must not shrink it", b)
	}

	ed.fits.Set(false)
	c.Frame()
	if vis(shell) == gooey.Visible || vis(msg) != gooey.Visible {
		t.Errorf("cramped: shell=%v msg=%v, want the message alone", vis(shell), vis(msg))
	}
}

// TestTheCrampedMessageSaysBothSizes — "too small" without the numbers
// leaves the user guessing how much to drag.
func TestTheCrampedMessageSaysBothSizes(t *testing.T) {
	ed, shell, root := shellGrid(t)
	_, usable, err := minimumFor(shell)
	if err != nil {
		t.Fatal(err)
	}
	have := fitSize{Cols: usable.Cols - 6, Rows: usable.Rows - 2}
	ed.fits.Set(false)
	ed.fitMsg.Set(cramMsg(have, usable))

	c := gooey.NewComposer(root, have.Cols, have.Rows)
	c.Frame()
	out := screen(c)
	for _, want := range []string{itoa(have.Cols), itoa(have.Rows), itoa(usable.Cols), itoa(usable.Rows)} {
		if !strings.Contains(out, want) {
			t.Errorf("the message must name %q; got:\n%s", want, out)
		}
	}
}

func screen(c *gooey.Composer) string {
	var b strings.Builder
	cells := c.Cells()
	cols, rows := c.Size()
	for y := 0; y < rows; y++ {
		for x := 0; x < cols; x++ {
			r := cells.At(x, y).Rune
			if r == 0 {
				r = ' '
			}
			b.WriteRune(r)
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func findByStyle(w gooey.Component, _ string) gooey.Component {
	// The cramped message is the only <Text> that is a direct child of
	// the root <Canvas>; the shell is the other child.
	c, ok := w.(gooey.Container)
	if !ok {
		return nil
	}
	for _, ch := range c.ChildComponents() {
		if _, isText := ch.(*components.Text); isText {
			return ch
		}
	}
	return nil
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

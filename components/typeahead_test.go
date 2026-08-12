package components

import (
	"testing"
	"time"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/prop"
)

// fruit is the fixture collection. Order matters in every test below:
// type-ahead searches the collection in the order it is presented, which
// is the behaviour "the first match in the current sort order" names.
var fruit = []string{"apple", "apricot", "banana", "blueberry", "cherry", "Damson"}

// typeAheadRig builds a focused list with a TypeAhead attached and a fake
// clock, and returns everything a test needs to drive it. The clock is a
// pointer the test advances — expiry is simulated, never slept.
type typeAheadRig struct {
	c    *gooey.Composer
	view *ItemsView
	ta   *TypeAhead
	sel  *prop.Property[int]
	now  *time.Time
}

func newTypeAheadRig(t *testing.T, items []string) *typeAheadRig {
	t.Helper()
	clock := time.Unix(0, 0)
	sel := prop.NewSource(0)
	src := Items(prop.NewSource(items), func(s string) map[string]any {
		return map[string]any{"Name": s}
	})
	view := &ItemsView{
		Items:    src,
		Selected: sel,
		Template: func(vals map[string]any) (gooey.Component, error) {
			return &Text{Content: vals["Name"].(*prop.Property[string])}, nil
		},
	}
	ta := &TypeAhead{Key: "Name", Now: func() time.Time { return clock }}
	view.Attach(ta)
	c := gooey.NewComposer(view, 20, len(items))
	c.Frame() // realize rows, so the view has a window and a row height
	return &typeAheadRig{c: c, view: view, ta: ta, sel: sel, now: &clock}
}

// typeStr sends each rune as a keystroke, the way a user produces them.
func (r *typeAheadRig) typeStr(s string) {
	for _, ch := range s {
		r.c.HandleKey(input.Rune(ch))
	}
}

func (r *typeAheadRig) selected() string {
	i := r.sel.Get()
	if i < 0 || i >= len(fruit) {
		return "<out of range>"
	}
	return fruit[i]
}

// idle advances the fake clock and runs the expiry check the ticker would
// have posted. Together these are exactly what the goroutine does, minus
// the goroutine.
func (r *typeAheadRig) idle(d time.Duration) {
	*r.now = r.now.Add(d)
	r.ta.expire()
}

func TestTypeAheadSelectsFirstPrefixMatch(t *testing.T) {
	r := newTypeAheadRig(t, fruit)
	r.typeStr("b")
	if got := r.selected(); got != "banana" {
		t.Fatalf("selected %q, want banana", got)
	}
}

// Refining a search keeps the item already landed on when it still
// matches — the buffer GREW, so the search starts at the selection rather
// than after it.
func TestTypeAheadRefinesWithoutSkipping(t *testing.T) {
	r := newTypeAheadRig(t, fruit)
	r.typeStr("b")
	if got := r.selected(); got != "banana" {
		t.Fatalf("after b: selected %q, want banana", got)
	}
	r.typeStr("l")
	if got := r.selected(); got != "blueberry" {
		t.Fatalf("after bl: selected %q, want blueberry", got)
	}
	r.typeStr("u")
	if got := r.selected(); got != "blueberry" {
		t.Fatalf("after blu: selected %q, want blueberry — refining must not skip the match", got)
	}
}

// Explorer's defining detail: one letter repeated cycles through the
// items starting with it, rather than searching for the repetition.
func TestTypeAheadRepeatedLetterCycles(t *testing.T) {
	r := newTypeAheadRig(t, fruit)
	// Start on cherry, so the first `a` has somewhere unambiguous to go.
	// Starting on an a-item would make the first keystroke itself a cycle
	// step, which is correct but proves nothing about cycling.
	r.sel.Set(4)

	r.typeStr("a")
	if got := r.selected(); got != "apple" {
		t.Fatalf("after a: selected %q, want apple", got)
	}
	r.typeStr("a")
	if got := r.selected(); got != "apricot" {
		t.Fatalf("after aa: selected %q, want apricot — a repeated letter must cycle", got)
	}
	r.typeStr("a")
	if got := r.selected(); got != "apple" {
		t.Fatalf("after aaa: selected %q, want apple — cycling must wrap", got)
	}
}

// A lone letter searches from AFTER the selection, so pressing it again
// advances instead of sitting still. This is the same mechanism cycling
// uses, stated on its own because it governs every single-letter press.
func TestTypeAheadSingleLetterStartsAfterSelection(t *testing.T) {
	r := newTypeAheadRig(t, fruit) // selection starts on apple, index 0
	r.typeStr("a")
	if got := r.selected(); got != "apricot" {
		t.Fatalf("selected %q, want apricot — a lone letter must not re-find the item already selected", got)
	}
}

// The idle reset is what makes an implicitly-armed mode survivable:
// a, pause, b lands on the first b rather than searching for "ab".
func TestTypeAheadBufferExpiresWhenIdle(t *testing.T) {
	r := newTypeAheadRig(t, fruit)
	r.typeStr("a")
	r.idle(2 * time.Second)
	r.typeStr("b")
	if got := r.selected(); got != "banana" {
		t.Fatalf("selected %q, want banana — the idle buffer must have been dropped", got)
	}
}

// Within the timeout the buffer accumulates, which is the same test run
// without the pause. Both directions matter: an always-expiring buffer
// would pass the test above and be just as broken.
func TestTypeAheadBufferSurvivesWithinTimeout(t *testing.T) {
	r := newTypeAheadRig(t, fruit)
	r.typeStr("d")
	if got := r.selected(); got != "Damson" {
		t.Fatalf("after d: selected %q, want Damson (matching is case-insensitive)", got)
	}
	r.idle(100 * time.Millisecond)
	r.typeStr("a")
	if got := r.selected(); got != "Damson" {
		t.Fatalf("after da: selected %q, want Damson — the buffer must survive a short pause", got)
	}
}

func TestTypeAheadNoMatchLeavesSelectionAndReports(t *testing.T) {
	r := newTypeAheadRig(t, fruit)
	miss := prop.NewSource(false)
	r.ta.NoMatch = miss

	r.typeStr("c")
	if got := r.selected(); got != "cherry" {
		t.Fatalf("after c: selected %q, want cherry", got)
	}
	r.typeStr("z")
	if got := r.selected(); got != "cherry" {
		t.Fatalf("after cz: selected %q — a miss must leave the selection alone", got)
	}
	if !miss.Get() {
		t.Fatal("NoMatch was not set on a failed search")
	}
	// The next hit clears it. Pause first, so this is a fresh search
	// rather than a continuation of the failed one.
	r.idle(2 * time.Second)
	r.typeStr("a")
	if miss.Get() {
		t.Fatal("NoMatch survived a successful search")
	}
}

// Search is the mode indicator: an implicitly-armed mode that shows
// nothing is a UI misrepresenting what the next keystroke will do.
func TestTypeAheadPublishesTheBuffer(t *testing.T) {
	r := newTypeAheadRig(t, fruit)
	buf := prop.NewSource("")
	r.ta.Search = buf

	r.typeStr("bl")
	if buf.Get() != "bl" {
		t.Fatalf("Search = %q, want \"bl\"", buf.Get())
	}
	r.idle(2 * time.Second)
	if buf.Get() != "" {
		t.Fatalf("Search = %q after expiry, want empty", buf.Get())
	}
}

// A navigation key resets the buffer AND still reaches the list: the
// attachment declines it rather than consuming it.
func TestTypeAheadNavigationResetsAndStillNavigates(t *testing.T) {
	r := newTypeAheadRig(t, fruit)
	r.typeStr("b") // banana, index 2
	r.c.HandleKey(input.Named(input.KeyDown))
	if got := r.selected(); got != "blueberry" {
		t.Fatalf("selected %q, want blueberry — down must still reach the list", got)
	}
	// The buffer is gone, so this b starts a fresh search from after the
	// selection and wraps to banana rather than extending to "bb".
	r.typeStr("b")
	if got := r.selected(); got != "banana" {
		t.Fatalf("selected %q, want banana — navigation must have reset the buffer", got)
	}
}

// Movement by any OTHER method — here a viewmodel write, standing in for
// a click or the wheel — also resets, detected as the selection having
// drifted from where the attachment last put it.
func TestTypeAheadForeignSelectionResetsBuffer(t *testing.T) {
	r := newTypeAheadRig(t, fruit)
	miss := prop.NewSource(false)
	r.ta.NoMatch = miss

	r.typeStr("b") // banana, index 2
	r.sel.Set(0)   // apple, by some other hand entirely

	// The discriminating keystroke. Reset, this is a fresh "l" search and
	// nothing starts with l. Not reset, the buffer would read "bl" and
	// land on blueberry — so the two outcomes cannot be confused.
	r.typeStr("l")
	if got := r.selected(); got != "apple" {
		t.Fatalf("selected %q, want apple — a foreign move must reset the buffer", got)
	}
	if !miss.Get() {
		t.Fatal("a fresh 'l' search should have missed; the buffer was not reset")
	}
}

// Modified keys are never search input: ctrl+s belongs to whatever bound
// it, and must keep bubbling.
func TestTypeAheadDeclinesModifiedKeys(t *testing.T) {
	r := newTypeAheadRig(t, fruit)
	fired := 0
	r.view.Attach(&gooey.KeyBinding{
		Gesture: input.KeyEvent{Key: input.KeyRune, Rune: 's', Mods: input.ModCtrl},
		Command: gooey.Command(func() { fired++ }),
	})
	r.c.Focus().Resync()

	r.c.HandleKey(input.KeyEvent{Key: input.KeyRune, Rune: 's', Mods: input.ModCtrl})
	if fired != 1 {
		t.Fatalf("binding fired %d times, want 1 — a modified key is not search input", fired)
	}
	if got := r.selected(); got != "apple" {
		t.Fatalf("selected %q, want apple — ctrl+s must not have searched", got)
	}
}

// A leading space is handed back, since lists commonly give space to
// activation and no one means to search for one. Once the buffer has
// something in it a space is an ordinary character.
func TestTypeAheadSpaceIsSearchOnlyMidWord(t *testing.T) {
	r := newTypeAheadRig(t, []string{"alpha beta", "zulu"})
	if r.c.HandleKey(input.Rune(' ')) {
		t.Fatal("a leading space was consumed as search input")
	}
	r.typeStr("alpha b")
	if r.sel.Get() != 0 {
		t.Fatalf("selected index %d, want 0 — a mid-word space must be search input", r.sel.Get())
	}
}

// An empty list must not panic or select anything.
func TestTypeAheadOnEmptyList(t *testing.T) {
	r := newTypeAheadRig(t, nil)
	miss := prop.NewSource(false)
	r.ta.NoMatch = miss
	r.typeStr("a")
	if !miss.Get() {
		t.Fatal("typing into an empty list should report no match")
	}
}

// The idle clock's stop is a barrier, not a signal: Close must guarantee
// no further posts. A stop that only closed a channel would let a tick
// that already won its select land afterwards.
func TestTypeAheadStopJoins(t *testing.T) {
	ta := &TypeAhead{Key: "Name", Timeout: 40 * time.Millisecond}
	posts := make(chan func(), 64)
	stop := ta.Start(func(fn func()) { posts <- fn })
	time.Sleep(60 * time.Millisecond) // let the ticker run for real here
	stop()
	drained := len(posts)
	time.Sleep(60 * time.Millisecond)
	if len(posts) != drained {
		t.Fatalf("posts arrived after stop: %d then %d — stop must join, not just signal", drained, len(posts))
	}
}

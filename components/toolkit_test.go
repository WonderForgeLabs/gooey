package components

import (
	"strings"
	"testing"
	"time"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/prop"
)

// The damage-count assertions in this file are contract tests, not
// implementation detail. "A state flip repaints exactly the component
// that changed" is the guarantee the whole property graph exists to
// make, and a component that quietly reads something it should not —
// or fails to read something it should — shows up here as a number and
// nowhere else.

// ---- ProgressBar ----

func TestProgressBarValueRepaintsOnlyItself(t *testing.T) {
	pct := prop.NewSource(10)
	bar := &ProgressBar{Value: pct, Width: 20}
	other := &Text{Content: Str("neighbour")}
	c := gooey.NewComposer(&VStack{Children: []gooey.Component{bar, other}}, 30, 4)
	c.Frame()

	pct.Set(60)
	if _, painted := c.Frame(); painted != 1 {
		t.Fatalf("a value change painted %d components, want 1", painted)
	}
	if got := row(c.Cells(), 0); !strings.Contains(got, "60%") {
		t.Fatalf("row = %q, want the readout to show 60%%", got)
	}
	if _, painted := c.Frame(); painted != 0 {
		t.Fatalf("a clean frame painted %d components, want 0", painted)
	}
}

// A determinate bar must not depend on the animation phase, or every
// tick of a bar that happens to be determinate would repaint it.
func TestProgressBarDeterminateIgnoresTheAnimation(t *testing.T) {
	busy := prop.NewSource(false)
	bar := &ProgressBar{Value: prop.NewSource(40), Indeterminate: busy, Width: 20}
	c := gooey.NewComposer(bar, 30, 3)
	c.Frame()

	bar.step()
	if _, painted := c.Frame(); painted != 0 {
		t.Fatalf("a step while determinate painted %d components, want 0", painted)
	}

	// Switching to indeterminate is itself one repaint, and from then on
	// each step is one more.
	busy.Set(true)
	if _, painted := c.Frame(); painted != 1 {
		t.Fatalf("switching to indeterminate painted %d, want 1", painted)
	}
	bar.step()
	if _, painted := c.Frame(); painted != 1 {
		t.Fatalf("a step while indeterminate painted %d, want 1", painted)
	}
}

func TestProgressBarIndeterminateBandMarches(t *testing.T) {
	bar := &ProgressBar{Indeterminate: prop.NewSource(true), Width: 20}
	c := gooey.NewComposer(bar, 20, 3)
	c.Frame()
	first := row(c.Cells(), 0)
	if !strings.Contains(first, "█") || !strings.Contains(first, "░") {
		t.Fatalf("the indeterminate track is not a band on a track: %q", first)
	}
	if strings.Contains(first, "%") {
		t.Fatalf("an indeterminate bar claimed a percentage: %q", first)
	}
	for i := 0; i < 3; i++ {
		bar.step()
	}
	c.Frame()
	if second := row(c.Cells(), 0); second == first {
		t.Fatalf("three steps did not move the band: still %q", second)
	}
}

// The ticker is the Timer discipline: it posts, it does not run. And a
// bar that can never be indeterminate does not start a goroutine at all.
func TestProgressBarTickerPostsAndJoins(t *testing.T) {
	d := gooey.NewDispatcher()
	bar := &ProgressBar{Indeterminate: prop.NewSource(true), Tick: time.Millisecond, Width: 10}
	stop := bar.Start(d.Post)
	waitFor(t, "a posted animation step", func() bool { return d.Pending() > 0 })
	if bar.phaseProp().Get() != 0 {
		t.Fatal("the ticker advanced the phase on its own goroutine")
	}
	d.Drain()
	if bar.phaseProp().Get() == 0 {
		t.Fatal("draining the dispatcher did not advance the animation")
	}
	stop()

	plain := &ProgressBar{Value: prop.NewSource(1)}
	if stop := plain.Start(d.Post); stop == nil {
		t.Fatal("Start returned no stop func")
	} else {
		stop()
	}
	if d.Pending() != 0 {
		t.Fatal("a determinate-only bar started a ticker")
	}
}

// The Composer owns the lifetime: it finds Startables while walking and
// stops them on Close.
func TestProgressBarLifetimeBelongsToTheComposition(t *testing.T) {
	d := gooey.NewDispatcher()
	bar := &ProgressBar{Indeterminate: prop.NewSource(true), Tick: time.Millisecond}
	c := gooey.NewComposer(bar, 20, 3)
	c.Start(d)
	waitFor(t, "the composition to start the bar", func() bool { return d.Pending() > 0 })
	c.Close()

	d.Drain()
	before := d.Pending()
	time.Sleep(20 * time.Millisecond)
	if after := d.Pending(); after > before {
		t.Fatalf("the ticker kept posting after Close (%d → %d)", before, after)
	}
}

// ---- Spinner ----

func TestSpinnerStepRepaintsOnlyItself(t *testing.T) {
	s := &Spinner{Frames: SpinnerLine, Label: Str("working")}
	c := gooey.NewComposer(&VStack{Children: []gooey.Component{s, &Text{Content: Str("x")}}}, 20, 4)
	c.Frame()
	if got := s.Glyph(); got != "|" {
		t.Fatalf("first glyph = %q, want %q", got, "|")
	}

	s.step()
	if _, painted := c.Frame(); painted != 1 {
		t.Fatalf("a frame step painted %d components, want 1", painted)
	}
	if got := s.Glyph(); got != "/" {
		t.Fatalf("glyph after one step = %q, want %q", got, "/")
	}
}

// A paused spinner parks at its first frame and advances nothing —
// including when the ticker keeps posting.
func TestSpinnerEnabledFalseParksAndSuppresses(t *testing.T) {
	on := prop.NewSource(true)
	s := &Spinner{Frames: SpinnerLine, Enabled: on}
	c := gooey.NewComposer(s, 20, 3)
	c.Frame()
	s.step()
	c.Frame()

	on.Set(false)
	if _, painted := c.Frame(); painted != 1 {
		t.Fatalf("pausing painted %d components, want 1", painted)
	}
	if got := s.Glyph(); got != "|" {
		t.Fatalf("a paused spinner shows %q, want the first frame %q", got, "|")
	}
	s.step()
	if _, painted := c.Frame(); painted != 0 {
		t.Fatalf("a step while paused painted %d components, want 0", painted)
	}
}

func TestSpinnerFrameSetsResolveByName(t *testing.T) {
	for _, name := range SpinnerNames {
		if fr, ok := SpinnerFrames(name); !ok || len(fr) == 0 {
			t.Fatalf("frame set %q did not resolve", name)
		}
	}
	if _, ok := SpinnerFrames("swirly"); ok {
		t.Fatal("an unknown frame set resolved instead of failing")
	}
}

// ---- Toggle ----

func TestToggleRockerArrowsOnlyConsumeWhenTheyMove(t *testing.T) {
	on := prop.NewSource(false)
	tg := &Toggle{Checked: on, Label: Str("dark mode")}

	if !tg.HandleKey(input.Named(input.KeyRight)) {
		t.Fatal("right did not switch an off rocker on")
	}
	if !on.Get() {
		t.Fatal("right did not set the property")
	}
	// Pushing the side it is already on is not an operation, so the key
	// keeps bubbling and moves focus instead.
	if tg.HandleKey(input.Named(input.KeyRight)) {
		t.Fatal("right consumed the key with nowhere to move")
	}
	if !tg.HandleKey(input.Named(input.KeyLeft)) {
		t.Fatal("left did not switch it off")
	}
	if !tg.HandleKey(input.Rune(' ')) || !on.Get() {
		t.Fatal("space did not flip it")
	}
}

func TestToggleRepaintsOnlyItselfAndShowsItsPosition(t *testing.T) {
	on := prop.NewSource(false)
	tg := &Toggle{Checked: on}
	c := gooey.NewComposer(&VStack{Children: []gooey.Component{tg, &Text{Content: Str("y")}}}, 20, 4)
	c.Frame()
	if got := row(c.Cells(), 0); !strings.HasPrefix(got, "(●") {
		t.Fatalf("off rocker painted %q, want the knob on the left", got)
	}

	on.Set(true)
	if _, painted := c.Frame(); painted != 1 {
		t.Fatalf("flipping the switch painted %d components, want 1", painted)
	}
	if got := row(c.Cells(), 0); !strings.HasPrefix(got, "(··●") {
		t.Fatalf("on rocker painted %q, want the knob on the right", got)
	}
}

// A Toggle with a conditional Changed is disabled the moment the
// condition says no: dim, and refusing every gesture. The condition is
// read while PAINTING, so the flip repaints exactly this switch.
func TestToggleDisabledByCanExecute(t *testing.T) {
	allowed := prop.NewSource(true)
	on := prop.NewSource(false)
	tg := &Toggle{Checked: on, Changed: gooey.NewCommand(func() {}).When(allowed)}
	c := gooey.NewComposer(&VStack{Children: []gooey.Component{tg, &Text{Content: Str("z")}}}, 20, 4)
	c.Frame()

	allowed.Set(false)
	if _, painted := c.Frame(); painted != 1 {
		t.Fatalf("disabling painted %d components, want 1", painted)
	}
	if !c.Cells().At(0, 0).Style.Dim {
		t.Fatal("a disabled toggle is not dim")
	}
	if tg.HandleKey(input.Rune(' ')) {
		t.Fatal("a disabled toggle consumed space")
	}
	if on.Get() {
		t.Fatal("a disabled toggle changed its property")
	}
}

func TestToggleClickPicksTheSideItLandedOn(t *testing.T) {
	on := prop.NewSource(true)
	tg := &Toggle{Checked: on}
	tg.Arrange(gooey.Rect{X: 4, Y: 0, W: 12, H: 1})

	tg.HandleMouse(input.MouseEvent{Kind: input.MouseClick, X: 5, Y: 0}) // left of the track
	if on.Get() {
		t.Fatal("clicking the left of the track did not switch it off")
	}
	tg.HandleMouse(input.MouseEvent{Kind: input.MouseClick, X: 8, Y: 0}) // right of the track
	if !on.Get() {
		t.Fatal("clicking the right of the track did not switch it on")
	}
}

// ---- Segmented ----

func TestSegmentedArrowsStopAtTheEnds(t *testing.T) {
	sel := prop.NewSource(0)
	sg := &Segmented{Options: Strs([]string{"Day", "Week", "Month"}), Selected: sel}

	if sg.HandleKey(input.Named(input.KeyLeft)) {
		t.Fatal("left consumed the key at the first segment")
	}
	if !sg.HandleKey(input.Named(input.KeyRight)) || sel.Get() != 1 {
		t.Fatalf("right did not advance the selection (now %d)", sel.Get())
	}
	if !sg.HandleKey(input.Named(input.KeyEnd)) || sel.Get() != 2 {
		t.Fatalf("end did not select the last segment (now %d)", sel.Get())
	}
	if sg.HandleKey(input.Named(input.KeyRight)) {
		t.Fatal("right consumed the key at the last segment")
	}
	// Space cycles, so the control is operable without arrows at all.
	if !sg.HandleKey(input.Rune(' ')) || sel.Get() != 0 {
		t.Fatalf("space did not wrap to the first segment (now %d)", sel.Get())
	}
}

func TestSegmentedClickSelectsTheSegmentUnderThePointer(t *testing.T) {
	sel := prop.NewSource(0)
	sg := &Segmented{Options: Strs([]string{"Day", "Week", "Month"}), Selected: sel}
	sg.Arrange(gooey.Rect{X: 0, Y: 0, W: 30, H: 1})

	// " Day " is 0-4, the separator is 5, " Week " is 6-11.
	sg.HandleMouse(input.MouseEvent{Kind: input.MouseClick, X: 8, Y: 0})
	if sel.Get() != 1 {
		t.Fatalf("clicking the second segment selected %d", sel.Get())
	}
	if sg.HandleMouse(input.MouseEvent{Kind: input.MouseClick, X: 5, Y: 0}) {
		t.Fatal("a click on the separator selected something")
	}
}

func TestSegmentedRepaintsOnlyItself(t *testing.T) {
	sel := prop.NewSource(0)
	sg := &Segmented{Options: Strs([]string{"Day", "Week"}), Selected: sel}
	c := gooey.NewComposer(&VStack{Children: []gooey.Component{sg, &Text{Content: Str("q")}}}, 30, 4)
	c.Frame()

	sel.Set(1)
	if _, painted := c.Frame(); painted != 1 {
		t.Fatalf("moving the selection painted %d components, want 1", painted)
	}
	if got := row(c.Cells(), 0); !strings.Contains(got, "Day") || !strings.Contains(got, "Week") {
		t.Fatalf("segments painted %q", got)
	}
}

// ---- StatusBar ----

func TestStatusBarPutsSectionsAgainstItsEdges(t *testing.T) {
	bar := &StatusBar{
		Left:   &Text{Content: Str("ready")},
		Center: &Text{Content: Str("mid")},
		Right:  &Text{Content: Str("q: quit")},
	}
	c := gooey.NewComposer(bar, 40, 1)
	c.Frame()
	got := row(c.Cells(), 0)
	if !strings.HasPrefix(got, "ready") {
		t.Fatalf("row = %q, want the left section against the left edge", got)
	}
	if !strings.HasSuffix(got, "q: quit") {
		t.Fatalf("row = %q, want the right section against the right edge", got)
	}
	if i := strings.Index(got, "mid"); i < 12 || i > 24 {
		t.Fatalf("row = %q, the centre section is at %d, not near the middle", got, i)
	}
}

func TestStatusBarSectionRepaintsAlone(t *testing.T) {
	status := prop.NewSource("ready")
	bar := &StatusBar{
		Left:  &Text{Content: status},
		Right: &Text{Content: Str("q: quit")},
	}
	c := gooey.NewComposer(bar, 40, 1)
	c.Frame()

	status.Set("saved")
	if _, painted := c.Frame(); painted != 1 {
		t.Fatalf("a status change painted %d components, want 1 (the left section only)", painted)
	}
	if got := row(c.Cells(), 0); !strings.HasPrefix(got, "saved") || !strings.HasSuffix(got, "q: quit") {
		t.Fatalf("row = %q — the untouched section did not survive", got)
	}
}

// A long left section must shorten the middle rather than push the right
// section off the edge: the edges are what a status bar is for.
func TestStatusBarEdgesWinOverTheCentre(t *testing.T) {
	bar := &StatusBar{
		Left:   &Text{Content: Str(strings.Repeat("L", 14))},
		Center: &Text{Content: Str("centre-section")},
		Right:  &Text{Content: Str("RIGHT")},
	}
	c := gooey.NewComposer(bar, 24, 1)
	c.Frame()
	if got := row(c.Cells(), 0); !strings.HasSuffix(got, "RIGHT") {
		t.Fatalf("row = %q, want RIGHT still against the right edge", got)
	}
}

// ---- ButtonBar ----

func barOf(labels ...string) (*ButtonBar, []*Button) {
	var kids []gooey.Component
	var btns []*Button
	for _, l := range labels {
		b := &Button{Content: Str(l), Click: gooey.Command(func() {})}
		btns = append(btns, b)
		kids = append(kids, b)
	}
	return &ButtonBar{Children: kids, Gap: 1}, btns
}

func TestButtonBarUniformSizingEqualisesMembers(t *testing.T) {
	bar, btns := barOf("ok", "cancel", "help me")
	bar.Uniform = true
	c := gooey.NewComposer(bar, 60, 3)
	c.Frame()

	want := btns[2].Bounds().W
	for i, b := range btns {
		if got := b.Bounds().W; got != want {
			t.Fatalf("member %d is %d cells wide, want the uniform %d", i, got, want)
		}
	}
	if btns[0].Bounds().X >= btns[1].Bounds().X {
		t.Fatal("members are not laid out left to right")
	}
}

func TestButtonBarDrawsSeparatorsBetweenMembers(t *testing.T) {
	bar, _ := barOf("one", "two")
	bar.Separator = "│"
	c := gooey.NewComposer(bar, 40, 3)
	c.Frame()
	if got := row(c.Cells(), 0); !strings.Contains(got, "│") {
		t.Fatalf("row = %q, want a separator between the members", got)
	}
}

// Overflow collapses what does not fit, which is what keeps the keyboard
// honest: focus traversal skips a collapsed member, so tab never lands
// on a button nobody can see. Widening the bar brings it back.
func TestButtonBarOverflowCollapsesAndRecovers(t *testing.T) {
	bar, btns := barOf("alpha", "bravo", "charlie", "delta")
	c := gooey.NewComposer(bar, 20, 3)
	c.Frame()

	if !bar.over {
		t.Fatal("a bar too narrow for its members did not report overflow")
	}
	if got := c.Cells().At(19, 0).Rune; got != OverflowMark {
		t.Fatalf("the overflow mark is %q, want %q", got, OverflowMark)
	}
	last := btns[len(btns)-1]
	if gooey.LayoutOf(last).Visibility != gooey.Collapsed {
		t.Fatal("an overflowing member was not collapsed")
	}
	if n := len(c.Focus().Order()); n != len(btns) {
		t.Fatalf("focus order has %d stops, want %d (collapsed members stay stops, they are just unreachable)", n, len(btns))
	}

	c.Resize(80, 3)
	c.Frame()
	if bar.over {
		t.Fatal("a bar wide enough for its members still reports overflow")
	}
	if v := gooey.LayoutOf(last).Visibility; v != gooey.Visible {
		t.Fatalf("widening did not restore the collapsed member (visibility %v)", v)
	}
}

// The bar is a focus scope: left and right walk along it and wrap,
// instead of escaping into the rest of the page.
func TestButtonBarArrowsTraverseAndWrap(t *testing.T) {
	bar, btns := barOf("one", "two", "three")
	page := &VStack{Children: []gooey.Component{
		bar, &Button{Content: Str("elsewhere"), Click: gooey.Command(func() {})},
	}}
	c := gooey.NewComposer(page, 60, 4)
	c.Frame()
	c.Focus().SetFocus(btns[0])

	c.HandleKey(input.Named(input.KeyRight))
	if c.Focus().Focused() != gooey.Component(btns[1]) {
		t.Fatal("right did not move to the next member")
	}
	c.HandleKey(input.Named(input.KeyRight))
	c.HandleKey(input.Named(input.KeyRight))
	if c.Focus().Focused() != gooey.Component(btns[0]) {
		t.Fatal("right at the last member did not wrap to the first")
	}
	c.HandleKey(input.Named(input.KeyLeft))
	if c.Focus().Focused() != gooey.Component(btns[2]) {
		t.Fatal("left at the first member did not wrap to the last")
	}
}

// Moving focus along the bar repaints exactly two components: the one
// that lost it and the one that gained it.
func TestButtonBarFocusMoveRepaintsExactlyTwo(t *testing.T) {
	bar, btns := barOf("one", "two", "three")
	c := gooey.NewComposer(bar, 60, 3)
	c.Frame()
	c.Focus().SetFocus(btns[0])
	c.Frame()

	c.HandleKey(input.Named(input.KeyRight))
	if _, painted := c.Frame(); painted != 2 {
		t.Fatalf("moving focus along the bar painted %d components, want 2", painted)
	}
}

// A member disabled by its command's condition is dim and declines,
// while the rest of the bar carries on.
func TestButtonBarMemberDisabledByCondition(t *testing.T) {
	dirty := prop.NewSource(false)
	save := &Button{Content: Str("save"), Click: gooey.NewCommand(func() {}).When(dirty)}
	bar := &ButtonBar{Children: []gooey.Component{
		&Button{Content: Str("open"), Click: gooey.Command(func() {})},
		save,
	}, Gap: 2}
	c := gooey.NewComposer(bar, 40, 3)
	c.Frame()
	if !c.Cells().At(save.Bounds().X, 0).Style.Dim {
		t.Fatal("a member whose condition says no is not dim")
	}

	dirty.Set(true)
	if _, painted := c.Frame(); painted != 1 {
		t.Fatalf("enabling one member painted %d components, want 1", painted)
	}
	if c.Cells().At(save.Bounds().X, 0).Style.Dim {
		t.Fatal("an enabled member is still dim")
	}
}

// The threshold ramp is available but off by default, because "96% done"
// is good news and the crit-red of a Gauge would say the opposite.
func TestProgressBarThresholdsAreOptIn(t *testing.T) {
	plain := &ProgressBar{Value: prop.NewSource(95), Width: 20}
	ramped := &ProgressBar{Value: prop.NewSource(95), Width: 20, Thresholds: true}
	c := gooey.NewComposer(&VStack{Children: []gooey.Component{plain, ramped}}, 30, 4)
	c.Frame()

	if got := c.Cells().At(0, 0).Style; got != styleGood {
		t.Fatalf("a plain bar at 95%% is styled %+v, want the flat accent", got)
	}
	if got := c.Cells().At(0, 1).Style; got != styleCrit {
		t.Fatalf("a Thresholds bar at 95%% is styled %+v, want the crit ramp", got)
	}
}

// Overflow collapsing must not quietly un-hide a member the author set
// Hidden: the bar restores what was there, not Visible.
func TestButtonBarOverflowPreservesAuthorVisibility(t *testing.T) {
	bar, btns := barOf("alpha", "bravo", "charlie", "delta")
	gooey.LayoutOf(btns[3]).Visibility = gooey.Hidden
	c := gooey.NewComposer(bar, 20, 3)
	c.Frame()
	if gooey.LayoutOf(btns[3]).Visibility != gooey.Collapsed {
		t.Fatal("the overflowing member was not collapsed")
	}
	c.Resize(80, 3)
	c.Frame()
	if v := gooey.LayoutOf(btns[3]).Visibility; v != gooey.Hidden {
		t.Fatalf("widening restored the member as %v, want Hidden — the author's setting", v)
	}
}

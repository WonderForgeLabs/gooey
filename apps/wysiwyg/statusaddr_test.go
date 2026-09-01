package main

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/render"
	"github.com/WonderForgeLabs/gooey/term"
)

// The control-plane addresses in the status bar, end to end through the
// SHIPPED page — because the thing most likely to be wrong is not the
// component, it is whether the markup actually mounts it and whether the
// page's own KeyBindings reach it.

const (
	testGrpc = "grpc 127.0.0.1:45783"
	testMCP  = "mcp http://127.0.0.1:46271/mcp"
)

// addrPage is buildPage with the endpoint list main() would have filled
// in by the time the page is first built. The servers start before
// app.Run, so a live editor always has these; a test has to say so.
func addrPage(t *testing.T, endpoints ...string) (*editor, *gooey.Composer) {
	t.Helper()
	ed := newEditor(editorFS())
	ed.serving = endpoints
	src, err := os.ReadFile("wysiwyg.gooey")
	if err != nil {
		t.Fatal(err)
	}
	root, err := markup.Build(src, ed.ctx)
	if err != nil {
		t.Fatalf("the editor's own page does not load: %v", err)
	}
	ed.rebuild()
	c := gooey.NewComposer(root, 160, 48)
	t.Cleanup(c.Close)
	c.Frame()
	// Fail HERE, with a sentence, rather than leaving every caller to
	// nil-deref ed.addrs. Reverting the <StatusBar.Right> slot in the
	// page is the mutation this guards, and a panic three tests deep
	// names the symptom instead of the cause.
	if len(endpoints) > 0 && ed.addrs == nil {
		t.Fatal("the shipped page did not mount <ServeAddrs/> in <StatusBar.Right>, so " +
			"there is no endpoint strip to test")
	}
	return ed, c
}

// swapClipboard substitutes the ONE clipboard seam this package has —
// the package-level writeSystemClipboard in clipboard.go, the same var
// clipEditor swaps — and restores it. Deliberately not a second seam on
// the strip: two seams for one behaviour is how two tests come to
// disagree about what a copy does.
func swapClipboard(t *testing.T, err error) *string {
	t.Helper()
	var got string
	prev := writeSystemClipboard
	writeSystemClipboard = func(_ *editor, text string) error {
		got = text
		return err
	}
	t.Cleanup(func() { writeSystemClipboard = prev })
	return &got
}

// okCopy makes the copy succeed and reports what was handed over.
func okCopy(t *testing.T) *string { return swapClipboard(t, nil) }

// noticeText is what the CLIPBOARD is saying. There is one notice for
// the whole strip rather than a flash per chip, which is the point: the
// copy outcome no longer has a per-endpoint position and so cannot be
// read as a per-endpoint fact.
func noticeText(s *addrStrip) string { return s.notice.text.Get() }

// stubServer is a control-plane server that never opens a socket, so
// the transitions — and above all the DISCONNECT, which is the one that
// races — can be driven deterministically. Its fields are read from
// whatever goroutine a callback fired on, so they are mutex-guarded;
// without that the -race arm of the disconnect test would fail on the
// stub rather than on the code under test.
type stubServer struct {
	mu       sync.Mutex
	serving  bool
	err      error
	sessions int
	requests int64
}

func (s *stubServer) Serving() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.serving
}

func (s *stubServer) ServeError() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

func (s *stubServer) Sessions() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sessions
}

func (s *stubServer) Requests() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.requests
}

func (s *stubServer) set(fn func(*stubServer)) {
	s.mu.Lock()
	fn(s)
	s.mu.Unlock()
}

// ---- 1. the affordance exists and is one component per address ----

func TestEachAddressIsItsOwnChip(t *testing.T) {
	ed, _ := addrPage(t, testGrpc, testMCP)
	if ed.addrs == nil {
		t.Fatal("the shipped page did not mount <ServeAddrs/>: the markup edit did not take")
	}
	if n := len(ed.addrs.chips); n != 2 {
		t.Fatalf("the strip built %d chips for 2 endpoints", n)
	}
	for i, want := range [][2]string{
		{"grpc", "127.0.0.1:45783"},
		{"mcp", "http://127.0.0.1:46271/mcp"},
	} {
		c := ed.addrs.chips[i]
		if c.label != want[0] || c.addr != want[1] {
			t.Errorf("chip %d is %q/%q, want %q/%q", i, c.label, c.addr, want[0], want[1])
		}
	}
	// The point of the split: each chip is separately hittable. One
	// string could never be.
	a, b := ed.addrs.chips[0].Bounds(), ed.addrs.chips[1].Bounds()
	if a.W == 0 || b.W == 0 {
		t.Fatalf("a chip measured zero: %+v %+v", a, b)
	}
	if a.X+a.W > b.X {
		t.Errorf("the chips overlap: %+v then %+v", a, b)
	}
}

// TestNoEndpointsKeepsTheServingText is the regression guard on the case
// the split could most easily have destroyed. With -serve "" -mcp "" the
// old Right section carried the "no control plane" explanation, and a
// strip of zero chips would have silently replaced it with nothing.
func TestNoEndpointsKeepsTheServingText(t *testing.T) {
	ed, c := addrPage(t)
	if ed.addrs != nil {
		t.Fatal("a strip was built for zero endpoints")
	}
	f, _ := c.Frame()
	if !strings.Contains(screenRow(f, 47), "no control plane") {
		t.Errorf("the status bar's last row is %q, and no longer explains that nothing is serving",
			strings.TrimSpace(screenRow(f, 47)))
	}
}

// screenRow reads one row of the cell plane back as a string.
func screenRow(f *gooey.Frame, y int) string {
	var b strings.Builder
	for x := 0; x < f.Cells.W; x++ {
		b.WriteRune(f.Cells.At(x, y).Rune)
	}
	return b.String()
}

// ---- 2. the copy tells the truth ----

// TestAFailedCopyNeverShowsSuccess is the single most important
// assertion in this file.
//
// A confirmation shown for a copy that did not happen is worse than no
// affordance at all: the user walks away believing they have the
// address. So the failing arm checks BOTH halves — that the error is
// shown, and that the success state is not — because a chip that showed
// "grpc copied" alongside a red dot would pass a test that only looked
// for the error.
//
// Mutation-checked by making copyText ignore the error (setting copyDone
// unconditionally); the assertions below go red. See the report.
func TestAFailedCopyNeverShowsSuccess(t *testing.T) {
	ed, _ := addrPage(t, testGrpc, testMCP)
	swapClipboard(t, fmt.Errorf("terminal suspended"))

	ed.addrs.CopyCurrent()

	if got := ed.addrs.notice.outcome.Get(); got != copyFailed {
		t.Errorf("after a copy that returned an error the notice reports %v, want "+
			"copyFailed: a confirmation for a copy that did not happen is the silent "+
			"failure this whole affordance is arranged around", got)
	}
	msg := noticeText(ed.addrs)
	if !strings.Contains(msg, "terminal suspended") {
		t.Errorf("the notice reads %q and does not carry the reason the copy failed", msg)
	}
	if strings.Contains(msg, "copied") {
		t.Errorf("the notice reads %q, which claims a copy that did not happen", msg)
	}
}

// TestWithNoTerminalTheChipReportsFailureNotSuccess is the same claim
// made against the REAL seam, with nothing substituted.
//
// It is not redundant with the test above and the difference is the
// point: that one proves the component handles an error, this one proves
// the SHIPPED WIRING actually produces one. `go test` has no terminal,
// so ed.copyToSystem genuinely fails here — and a version of this file
// that had accidentally stubbed the copy out, or swallowed its error at
// the seam, would show a cheerful "grpc copied" and this test would
// catch it.
func TestWithNoTerminalTheChipReportsFailureNotSuccess(t *testing.T) {
	ed, _ := addrPage(t, testGrpc, testMCP)
	// Deliberately NOT substituting copyFn.
	ed.addrs.CopyCurrent()

	if got := ed.addrs.notice.outcome.Get(); got != copyFailed {
		t.Fatalf("with no terminal the notice reports %v, want copyFailed: the real copy "+
			"path cannot have succeeded here, so anything else is a fabricated "+
			"confirmation", got)
	}
	if got := noticeText(ed.addrs); !strings.HasPrefix(got, "copy failed:") {
		t.Errorf("the notice reads %q, want it to lead with the failure", got)
	}
}

func TestASuccessfulCopySendsTheAddressAndConfirmsIt(t *testing.T) {
	ed, _ := addrPage(t, testGrpc, testMCP)
	sent := okCopy(t)

	ed.addrs.CopyCurrent()

	if *sent != "127.0.0.1:45783" {
		t.Errorf("the clipboard was handed %q, want the bare address: the label is a "+
			"caption, not part of what a client pastes into its config", *sent)
	}
	if got := ed.addrs.notice.outcome.Get(); got != copyDone {
		t.Errorf("outcome is %v after a copy that succeeded, want copyDone", got)
	}
	if got := noticeText(ed.addrs); !strings.Contains(got, "copied") {
		t.Errorf("the chip reads %q and does not confirm the copy", got)
	}
}

// ---- 3. damage ----

// TestACopyRepaintsTheNoticeAndNoChip is the pin on the property the
// status bar exists for, AND on the separation this file was rewritten
// to establish.
//
// Two claims in one count. The narrow one: a copy must not repaint the
// build status on the left or the DESIGN/LIVE indicator in the centre,
// or StatusBar's three separate paint nodes are paid for and not used.
// The load-bearing one: it must not repaint a CHIP either. A chip that
// repainted on a copy would be a chip that reads a copy property, which
// is the arrangement this whole change exists to undo — and a count of
// exactly 1, taken together with the identity check below, is what says
// the one thing that moved was the notice.
//
// A COUNT, not a cell or bounds assertion. "The notice says copied"
// passes exactly as well when the entire tree repainted, so it proves
// nothing about damage; only Composer.Frame's second return does.
func TestACopyRepaintsTheNoticeAndNoChip(t *testing.T) {
	ed, c := addrPage(t, testGrpc, testMCP)
	okCopy(t)

	if _, quiet := c.Frame(); quiet != 0 {
		t.Fatalf("the page was not settled before the measurement: %d components repainted", quiet)
	}

	// The CURRENT chip, so the current index does not move. Copying the
	// other one also moves the index, which both chips read — that case
	// is pinned separately below.
	ed.addrs.CopyCurrent()

	_, painted := c.Frame()
	if painted != 1 {
		t.Errorf("a copy repainted %d components, want exactly 1 (the notice).\n"+
			"The right section's notice and chips, the centre's mode indicator and the "+
			"left's build status are separate paint nodes on purpose; a number above 1 "+
			"means the copy is reaching a property something else reads.", painted)
	}
	// WHICH one, not just how many. A count of 1 is satisfied by the
	// wrong component repainting, and the wrong component here is exactly
	// the one this change moved the feedback off.
	dmg := c.Damage()
	if !damaged(dmg, ed.addrs.notice.Bounds()) {
		t.Errorf("the copy did not repaint the notice (damage %v), so the count above was "+
			"something else entirely", dmg)
	}
	for i, ch := range ed.addrs.chips {
		if damaged(dmg, ch.Bounds()) {
			t.Errorf("chip %d repainted on a copy (damage %v).\n"+
				"That means it reads a clipboard property — which is the arrangement "+
				"this file exists to undo: a chip's dot reports the SERVICE, and the "+
				"clipboard reports in the notice. See servelink.go.", i, dmg)
		}
	}
}

// damaged reports whether any repainted rect overlaps r. Overlap rather
// than equality: a component's damage rect is its own bounds, so an
// intersection with a chip's cells is that chip having repainted (or
// something covering it having done so), which is what the assertion
// means either way.
func damaged(rects []gooey.Rect, r gooey.Rect) bool {
	for _, d := range rects {
		if d.X < r.X+r.W && r.X < d.X+d.W && d.Y < r.Y+r.H && r.Y < d.Y+d.H {
			return true
		}
	}
	return false
}

// TestOneEndpointChangingRepaintsOneChip is the same discipline from the
// other side, and it is the pin on the dot actually being observed
// rather than sampled.
//
// A gRPC client attaching repaints the gRPC chip. It must not repaint
// the MCP chip, the notice, or anything in the other two sections — and
// it must not need a frame to be asked for by anything else, which is
// what "observed" means here: the Get in Render IS the subscription.
func TestOneEndpointChangingRepaintsOneChip(t *testing.T) {
	ed, c := addrPage(t, testGrpc, testMCP)
	if _, quiet := c.Frame(); quiet != 0 {
		t.Fatalf("not settled: %d repainted", quiet)
	}
	ed.link("grpc").set(linkActive)

	_, painted := c.Frame()
	if painted != 1 {
		t.Errorf("one endpoint's state changing repainted %d components, want exactly 1 "+
			"(that endpoint's chip)", painted)
	}
	dmg := c.Damage()
	if !damaged(dmg, ed.addrs.chips[0].Bounds()) {
		t.Errorf("the grpc chip did not repaint when its endpoint changed (damage %v): "+
			"the dot is not observed, and a dot that only refreshes when something else "+
			"dirties it will go on showing a server that has gone", dmg)
	}
	if damaged(dmg, ed.addrs.chips[1].Bounds()) {
		t.Error("the mcp chip repainted when the grpc endpoint changed: the two chips " +
			"share a property they should not")
	}
	if damaged(dmg, ed.addrs.notice.Bounds()) {
		t.Error("the notice repainted when an endpoint's state changed, so the clipboard " +
			"slot reads service state")
	}
}

// TestAnUnchangedEndpointStateRepaintsNothing is the prop.Set trap, on
// the path where it bites hardest: gRPC fires OnSessions(0) both when
// the last client leaves and again when the accept loop ends, so one
// visible transition can arrive twice. prop.Set does not compare, so
// without endpointLink.set's guard the second one costs a repaint for
// nothing.
func TestAnUnchangedEndpointStateRepaintsNothing(t *testing.T) {
	ed, c := addrPage(t, testGrpc, testMCP)
	ed.link("grpc").set(linkIdle)
	c.Frame()
	if _, quiet := c.Frame(); quiet != 0 {
		t.Fatalf("not settled: %d repainted", quiet)
	}

	ed.link("grpc").set(linkIdle) // already idle

	if _, painted := c.Frame(); painted != 0 {
		t.Errorf("re-asserting the state a chip already holds repainted %d components, "+
			"want 0", painted)
	}
}

// TestMovingBetweenChipsRepaintsExactlyTwo pins the OTHER number, which
// is two rather than one because both chips read the current index —
// the same shape as focus moving between two components, and for the
// same reason.
func TestMovingBetweenChipsRepaintsExactlyTwo(t *testing.T) {
	ed, c := addrPage(t, testGrpc, testMCP)
	okCopy(t)
	if _, quiet := c.Frame(); quiet != 0 {
		t.Fatalf("not settled: %d repainted", quiet)
	}

	ed.addrs.setCurrent(1)

	if _, painted := c.Frame(); painted != 2 {
		t.Errorf("moving the current chip repainted %d components, want exactly 2 — "+
			"the one leaving and the one arriving", painted)
	}
}

// TestAnUnchangedCurrentIndexRepaintsNothing is the guard on the trap
// prop.Set lays: it does not compare, so re-setting the index to what it
// already holds would invalidate both chips and cost two repaints for
// nothing. Clicking the already-current chip is the common case.
func TestAnUnchangedCurrentIndexRepaintsNothing(t *testing.T) {
	ed, c := addrPage(t, testGrpc, testMCP)
	if _, quiet := c.Frame(); quiet != 0 {
		t.Fatalf("not settled: %d repainted", quiet)
	}
	ed.addrs.setCurrent(0) // already 0
	if _, painted := c.Frame(); painted != 0 {
		t.Errorf("re-selecting the current chip repainted %d components, want 0", painted)
	}
}

// ---- 4. the width may not move ----

// TestTheStripWidthNeverMoves is the pin on what keeps the damage counts
// above where they are.
//
// Measure runs in layout. Anything on this row that sized itself to its
// message would re-Arrange the whole StatusBar, and StatusBar centres
// its middle section in the gap between the two edges — so the
// DESIGN/LIVE indicator would shift and repaint every time an address
// was copied, in a different section of the bar entirely.
//
// It measures the STRIP rather than a chip, and that is the stronger
// claim now: a chip has no message left to be sized by, so measuring one
// would be measuring a constant. The notice is where a message can
// still change, and copyNoticeWidth is the reservation that absorbs it.
func TestTheStripWidthNeverMoves(t *testing.T) {
	ed, _ := addrPage(t, testGrpc, testMCP)
	s := ed.addrs
	avail := gooey.Size{W: 160, H: 1}
	idle := gooey.MeasureChild(s, avail).W

	for _, st := range []struct {
		name string
		out  copyOutcome
		text string
	}{
		{"success", copyDone, "grpc copied"},
		{"caveat", copyCaveat, "grpc copy unverified"},
		{"failure", copyFailed, "copy failed: a very long explanation indeed, far longer than the address"},
	} {
		s.notice.outcome.Set(st.out)
		s.notice.text.Set(st.text)
		if got := gooey.MeasureChild(s, avail).W; got != idle {
			t.Errorf("with a %s notice the strip measures %d, idle it measures %d: a width "+
				"that moves re-arranges the status bar and repaints the mode indicator "+
				"in the centre section", st.name, got, idle)
		}
	}
}

// TestTheNoticeIsReservedWhenItIsEmpty is the other half. A slot that
// only took cells when it had something to say would move the strip's
// edge on every copy, which is what the test above forbids — so the
// reservation has to be there when the notice is blank.
func TestTheNoticeIsReservedWhenItIsEmpty(t *testing.T) {
	ed, _ := addrPage(t, testGrpc, testMCP)
	if got := noticeText(ed.addrs); got != "" {
		t.Fatalf("the notice already reads %q, so this measures nothing", got)
	}
	if got := ed.addrs.notice.Bounds().W; got != copyNoticeWidth {
		t.Errorf("an empty notice occupies %d cells, want the reserved %d", got, copyNoticeWidth)
	}
}

// TestANarrowRowKeepsTheAddressesAndDropsTheNotice is the degradation
// rule, and the direction is the point rather than the arithmetic. The
// ports are ephemeral and cannot be retyped from memory; a copy message
// can be summoned again by repeating the gesture. So the notice is the
// one that gives way.
func TestANarrowRowKeepsTheAddressesAndDropsTheNotice(t *testing.T) {
	ed, _ := addrPage(t, testGrpc, testMCP)
	s := ed.addrs
	full := s.naturalChipsWidth()

	// Exactly enough for the chips and not one cell more.
	gooey.MeasureChild(s, gooey.Size{W: full, H: 1})
	gooey.ArrangeChild(s, gooey.Rect{X: 0, Y: 0, W: full, H: 1})

	if got := s.notice.Bounds().W; got != 0 {
		t.Errorf("with room for the addresses alone the notice still took %d cells", got)
	}
	for i, c := range s.chips {
		if got := c.Bounds().W; got != c.chipWidth() {
			t.Errorf("chip %d was squeezed to %d of its %d cells: the notice must give "+
				"way first, because an ephemeral port is the thing a user cannot retype",
				i, got, c.chipWidth())
		}
	}
}

// ---- 5. the glyph ----

// TestTheStateDotIsNarrow is the wide-rune guard, and it is a real trap
// rather than a stylistic one.
//
// NOTHING in render/, components/ or term/ consults a character-width
// table: render.Buffer.SetString advances x by one per rune and
// Text.Measure sizes with len([]rune(...)). A two-cell grapheme written
// into a one-cell slot leaves the cell plane and the terminal's cursor
// permanently out of step for the rest of the row — and by the damage
// model every cell it corrupts belongs to a CLEAN node that will not
// repaint, so the corruption stays until something unrelated dirties it.
//
// The ranges below are the unambiguously East-Asian-Wide blocks plus the
// emoji planes. U+25CF sits below all of them; a 🟢 (U+1F7E2) does not,
// which is exactly the substitution this exists to refuse.
func TestTheStateDotIsNarrow(t *testing.T) {
	wide := []struct {
		lo, hi rune
		name   string
	}{
		{0x1100, 0x115F, "Hangul Jamo"},
		{0x2E80, 0xA4CF, "CJK"},
		{0xAC00, 0xD7A3, "Hangul syllables"},
		{0xF900, 0xFAFF, "CJK compatibility ideographs"},
		{0xFE30, 0xFE6F, "CJK compatibility forms"},
		{0xFF00, 0xFF60, "fullwidth forms"},
		{0xFFE0, 0xFFE6, "fullwidth signs"},
		{0x1F300, 0x1FAFF, "emoji and pictographs"},
		{0x20000, 0x3FFFD, "CJK extension planes"},
	}
	for _, w := range wide {
		if addrDot >= w.lo && addrDot <= w.hi {
			t.Fatalf("the state dot %q (U+%04X) is in the %s block, which terminals draw "+
				"two cells wide. Nothing in this repo is rune-width aware — SetString "+
				"advances one cell per rune — so it would shift every cell to its right "+
				"out of step with the terminal, permanently, because those cells are "+
				"clean and never repaint.", addrDot, addrDot, w.name)
		}
	}
}

// TestTheDotOccupiesOneCellAndTheAddressFollowsIt is the cell assertion
// that pins the layout the width guard above assumes: the plane is
// written on a one-cell-per-rune model, so the dot takes cell 0, a
// space takes cell 1, and the text starts at cell 2.
func TestTheDotOccupiesOneCellAndTheAddressFollowsIt(t *testing.T) {
	ed, c := addrPage(t, testGrpc, testMCP)
	f, _ := c.Frame()
	b := ed.addrs.chips[0].Bounds()

	if got := f.Cells.At(b.X, b.Y).Rune; got != addrDot {
		t.Fatalf("cell %d,%d holds %q, want the state dot %q", b.X, b.Y, got, addrDot)
	}
	if got := f.Cells.At(b.X+1, b.Y).Rune; got != ' ' {
		t.Errorf("cell %d,%d holds %q, want the separating space", b.X+1, b.Y, got)
	}
	var text strings.Builder
	for x := b.X + 2; x < b.X+b.W; x++ {
		text.WriteRune(f.Cells.At(x, b.Y).Rune)
	}
	if got := strings.TrimRight(text.String(), " "); got != testGrpc {
		t.Errorf("the chip reads %q from cell %d, want %q: the address must begin exactly "+
			"one cell after the dot", got, b.X+2, testGrpc)
	}
}

// TestTheDotColourFollowsTheConnection is the traffic light itself. The
// three claims a chip can make must be three distinguishable colours, or
// the cue carries no information.
//
// linkServing is deliberately NOT required to differ from linkActive:
// both mean "up", they are worn by different endpoints, and MCP has
// nothing to say about clients. What must differ is down from up, and
// idle from active.
func TestTheDotColourFollowsTheConnection(t *testing.T) {
	down, idle, active := linkDown.dotStyle(), linkIdle.dotStyle(), linkActive.dotStyle()
	if down == idle || idle == active || down == active {
		t.Fatalf("two connection states share a colour: down=%+v idle=%+v active=%+v",
			down, idle, active)
	}
	for name, st := range map[string]render.Style{
		"down": down, "idle": idle, "active": active, "serving": linkServing.dotStyle(),
	} {
		if !st.Fg.Set {
			t.Errorf("the %s colour is unset, so the dot would render in the terminal default", name)
		}
	}
}

// TestTheCopyNoticeAndTheServiceDotDoNotShareAStyleFunction is the
// structural half of the separation.
//
// The two facts are carried by two functions with two receivers, and
// nothing converts between them — so an edit that put the copy outcome
// back on a dot has to be written out deliberately rather than reached
// by passing the wrong value to the right function. This is a
// compile-time property being asserted at run time on purpose: it is
// cheap, and the alternative is a comment nobody reads.
func TestTheCopyNoticeAndTheServiceDotDoNotShareAStyleFunction(t *testing.T) {
	// copyOutcome answers textStyle and has no dotStyle; linkState
	// answers dotStyle and has no textStyle. If either grew the other's
	// method this stops compiling, which is the point.
	var _ func() render.Style = copyDone.textStyle
	var _ func() render.Style = linkActive.dotStyle

	// The idle notice is DIM and uncoloured, so a blank slot cannot read
	// as a state at all.
	if idle := copyIdle.textStyle(); idle.Fg.Set {
		t.Errorf("the idle notice sets a foreground colour (%+v): an empty slot with a "+
			"colour is a cue for nothing", idle)
	}
}

// ---- 5b. the connection state model ----

// TestGRPCHasThreeStatesAndMCPHasTwo is the asymmetry, asserted rather
// than described.
//
// gRPC has a long-lived streaming Attach RPC, so "nothing attached" and
// "something attached" are both real and both observable. MCP's handler
// is Stateless: true — every POST is independent, no session id is ever
// minted — so "is a client connected" has no answer, and the two states
// it does have are the whole truth.
//
// linkStateOf spells that with a nil session function. A future edit
// that gives MCP a third state has to change this test to do it.
func TestGRPCHasThreeStatesAndMCPHasTwo(t *testing.T) {
	srv := &stubServer{serving: true}

	// gRPC: three.
	got := map[linkState]bool{}
	for _, n := range []int{0, 1, 7} {
		srv.set(func(s *stubServer) { s.sessions = n })
		got[linkStateOf(srv, srv.Sessions)] = true
	}
	srv.set(func(s *stubServer) { s.serving = false })
	got[linkStateOf(srv, srv.Sessions)] = true
	if len(got) != 3 {
		t.Errorf("the gRPC endpoint produced %d distinct states %v, want 3 "+
			"(down, idle, active)", len(got), got)
	}

	// MCP: two, and never the gRPC pair.
	mgot := map[linkState]bool{}
	m := &stubServer{serving: true}
	for _, n := range []int64{0, 1, 999} {
		m.set(func(s *stubServer) { s.requests = n })
		st := linkStateOf(m, nil)
		mgot[st] = true
		if st == linkIdle || st == linkActive {
			t.Fatalf("the MCP endpoint reported %v, which is a claim about live clients. "+
				"This server is stateless by design: a client is connected for the few "+
				"milliseconds of a request and not connected the rest of the time, so "+
				"idle/active is a fact being invented. See servelink.go.", st)
		}
	}
	m.set(func(s *stubServer) { s.serving = false })
	mgot[linkStateOf(m, nil)] = true
	if len(mgot) != 2 {
		t.Errorf("the MCP endpoint produced %d distinct states %v, want exactly 2 "+
			"(down, serving)", len(mgot), mgot)
	}
}

// TestTheMCPDotIgnoresTheRequestCount is the mutation pin named in the
// brief, and it is the specific wrong implementation that would look
// right in a demo.
//
// mcp.Server.Requests is CUMULATIVE — it never decreases. Driving the
// dot from it produces a light that goes green on the first call and
// stays green forever, including after every client has gone and
// including after the listener has died. A count that only goes up is a
// broken gauge.
func TestTheMCPDotIgnoresTheRequestCount(t *testing.T) {
	srv := &stubServer{serving: true}

	quiet := linkStateOf(srv, nil)
	srv.set(func(s *stubServer) { s.requests = 4096 })
	busy := linkStateOf(srv, nil)

	if quiet != busy {
		t.Errorf("4096 requests moved the MCP dot from %v to %v. The count is cumulative, "+
			"so a dot driven by it can only ever go one way and never comes back — it "+
			"would still be green after the last client left.", quiet, busy)
	}

	// And the count going up must not survive the listener dying, which
	// is the failure a cumulative-count dot gets exactly backwards.
	srv.set(func(s *stubServer) { s.serving = false })
	if got := linkStateOf(srv, nil); got != linkDown {
		t.Errorf("with the accept loop gone and 4096 requests behind it the dot reports "+
			"%v, want linkDown", got)
	}
}

// TestTheRequestCountIsSurfacedAsALabelledNumber is the other half:
// refusing to make it a colour is not the same as hiding it. It is
// genuinely useful — "has anything ever talked to this endpoint" — so it
// is shown, in words, where words fit.
func TestTheRequestCountIsSurfacedAsALabelledNumber(t *testing.T) {
	srv := &stubServer{serving: true, requests: 12}
	got := statelessDetail(srv)
	if !strings.Contains(got, "12") {
		t.Errorf("the MCP detail reads %q and does not carry the request count", got)
	}
	if !strings.Contains(got, "call") {
		t.Errorf("the MCP detail reads %q: the number needs a label saying what it "+
			"counts, or it reads as a client count", got)
	}
	if !strings.Contains(got, "so far") {
		t.Errorf("the MCP detail reads %q and does not say the count is cumulative", got)
	}
	if strings.Contains(got, "client") || strings.Contains(got, "attached") {
		t.Errorf("the MCP detail reads %q, which claims something about live clients", got)
	}
	// gRPC's count IS live, and says so.
	if g := sessionDetail(&stubServer{serving: true, sessions: 2}); !strings.Contains(g, "2 clients attached") {
		t.Errorf("the gRPC detail reads %q, want a live client count", g)
	}
}

// TestADeadListenerIsReportedWithItsReason closes the reachable "down"
// case. A bind failure never gets here — main calls gooey.Exit before
// the page is built — so the state that matters is an accept loop that
// died under a running app, and the status bar going on advertising a
// port that will refuse every connection is the symptom this replaces.
func TestADeadListenerIsReportedWithItsReason(t *testing.T) {
	srv := &stubServer{serving: false, err: fmt.Errorf("use of closed network connection")}
	if got := linkStateOf(srv, srv.Sessions); got != linkDown {
		t.Fatalf("a dead listener reports %v, want linkDown", got)
	}
	if got := sessionDetail(srv); !strings.Contains(got, "use of closed network connection") {
		t.Errorf("the detail reads %q and does not say why the endpoint stopped", got)
	}
	// A clean Close leaves no error, and must not be dressed up as one.
	if got := sessionDetail(&stubServer{}); strings.Contains(got, "stopped:") {
		t.Errorf("a clean shutdown reads %q, want the plain not-serving wording", got)
	}
}

// ---- 6. the keyboard, which is the half a pty can verify ----

// openMenuKey is the menu's keyboard route: focus the strip, press m.
// The menu deliberately has NO global gesture — see copyEndpoint.
func openMenuKey(t *testing.T, ed *editor, c *gooey.Composer) {
	t.Helper()
	c.Focus().SetFocus(ed.addrs)
	if !c.Handle(input.KeyOf(input.Rune('m'))) {
		t.Fatal("m was not consumed by the focused strip, so the menu has no keyboard route")
	}
}

func ctrlKey(c *gooey.Composer, r rune) bool {
	return c.Handle(input.KeyOf(input.KeyEvent{Key: input.KeyRune, Rune: r, Mods: input.ModCtrl}))
}

// TestTheGlobalGesturesCopyEachAddress drives the SHIPPED page's own
// KeyBindings rather than calling copyEndpoint, because a binding placed
// on the wrong element never fires and a direct call would pass for a
// page that has none. Mouse reports cannot be injected through a
// recording pty, so this is the only route that can ever be captured.
func TestTheGlobalGesturesCopyEachAddress(t *testing.T) {
	ed, c := addrPage(t, testGrpc, testMCP)
	sent := okCopy(t)

	if !ctrlKey(c, 'g') {
		t.Fatal("ctrl+g was not consumed: the page has no binding for it")
	}
	if *sent != "127.0.0.1:45783" {
		t.Errorf("ctrl+g copied %q, want the gRPC address", *sent)
	}
	if got := noticeText(ed.addrs); !strings.Contains(got, "copied") {
		t.Errorf("the grpc chip reads %q after ctrl+g", got)
	}

	if !ctrlKey(c, 't') {
		t.Fatal("ctrl+t was not consumed: the page has no binding for it")
	}
	if *sent != "http://127.0.0.1:46271/mcp" {
		t.Errorf("ctrl+t copied %q, want the MCP URL", *sent)
	}
	if got := noticeText(ed.addrs); !strings.Contains(got, "copied") {
		t.Errorf("the mcp chip reads %q after ctrl+t", got)
	}
}

// TestTheGesturesAreNotAlreadyClaimed keeps the three new bindings
// honest against the page they were added to. A gesture bound twice
// resolves to whichever KeyBinding the dispatcher reaches first, and the
// loser fires never — with no error anywhere.
func TestTheGesturesAreNotAlreadyClaimed(t *testing.T) {
	src, err := os.ReadFile("wysiwyg.gooey")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]int{}
	for _, line := range strings.Split(string(src), "\n") {
		_, rest, ok := strings.Cut(line, `<KeyBinding Gesture="`)
		if !ok {
			continue
		}
		g, _, _ := strings.Cut(rest, `"`)
		seen[g]++
	}
	if len(seen) == 0 {
		t.Fatal("no KeyBindings were found in the page; this test asserts nothing")
	}
	for _, g := range []string{"ctrl+g", "ctrl+t"} {
		switch seen[g] {
		case 0:
			t.Errorf("the page has no %s binding", g)
		case 1:
		default:
			t.Errorf("%s is bound %d times; only the first would ever fire", g, seen[g])
		}
	}
	// ctrl+m is Enter on the wire and must never be the MCP mnemonic,
	// however obvious it looks.
	if seen["ctrl+m"] > 0 {
		t.Error("the page binds ctrl+m, which a terminal sends as byte 0x0d — " +
			"indistinguishable from Enter")
	}
	// The dock work owns these two. A duplicate resolves to whichever
	// KeyBinding the dispatcher reaches first and the loser fires NEVER,
	// with no error anywhere — so the collision has to be asserted here,
	// not remembered. Both were this file's first choices.
	for _, g := range []string{"ctrl+u", "ctrl+e"} {
		if seen[g] > 0 {
			t.Errorf("this page binds %s, which the dock work claims; one of the two "+
				"would silently never fire", g)
		}
	}
	// F-keys never decode (no F1-F12 in finalKey/tildeKey) and
	// ctrl+shift+X parses identically to ctrl+X. Either is a dead
	// binding with no error anywhere.
	for g := range seen {
		if strings.Contains(g, "ctrl+shift+") {
			t.Errorf("the page binds %q, which parses to the same KeyEvent as its "+
				"plain ctrl+ form; one of the two can never fire", g)
		}
		if len(g) >= 2 && g[0] == 'f' && g[1] >= '0' && g[1] <= '9' {
			t.Errorf("the page binds %q; F-keys are not decoded at all and the "+
				"sequence is silently swallowed", g)
		}
	}
}

// TestEnterCopiesTheCurrentChip is the focus route: tab to the strip,
// arrows to choose, enter to copy.
func TestEnterCopiesTheCurrentChip(t *testing.T) {
	ed, c := addrPage(t, testGrpc, testMCP)
	sent := okCopy(t)

	c.Focus().SetFocus(ed.addrs)
	if c.Focus().Focused() != gooey.Component(ed.addrs) {
		t.Fatal("the strip did not take focus, so it is not keyboard-reachable by tab")
	}
	if !c.Handle(input.KeyOf(input.Named(input.KeyRight))) {
		t.Fatal("right arrow was not consumed by the focused strip")
	}
	if !c.Handle(input.KeyOf(input.Named(input.KeyEnter))) {
		t.Fatal("enter was not consumed by the focused strip")
	}
	if *sent != "http://127.0.0.1:46271/mcp" {
		t.Errorf("right-then-enter copied %q, want the second endpoint", *sent)
	}
}

// ---- 7. right click and the menu ----

// TestARightPressDecodesFromTheWire is the half that has nothing to do
// with this component: before designing around right-click at all, the
// SGR button field has to survive decoding. It does — a press carries
// MouseButton(cb&3), so cb=2 is ButtonRight.
func TestARightPressDecodesFromTheWire(t *testing.T) {
	ev, n, ok := input.Decode([]byte("\x1b[<2;6;11M"), true)
	if !ok || n == 0 {
		t.Fatalf("the SGR right-press did not decode: ok=%v n=%d", ok, n)
	}
	if ev.Kind != input.EventMouse {
		t.Fatalf("decoded a %v, want a mouse event", ev.Kind)
	}
	if ev.Mouse.Kind != input.MousePress || ev.Mouse.Button != input.ButtonRight {
		t.Errorf("decoded kind=%v button=%v, want MousePress/ButtonRight — right-click "+
			"cannot be designed around if the button does not survive the wire",
			ev.Mouse.Kind, ev.Mouse.Button)
	}
	if ev.Mouse.X != 5 || ev.Mouse.Y != 10 {
		t.Errorf("decoded at %d,%d, want 5,10 (SGR is 1-based)", ev.Mouse.X, ev.Mouse.Y)
	}
}

// TestARightPressOpensTheMenuOnThatChip is the routing half, through
// Composer.HandleMouse — the same call the terminal's report makes.
func TestARightPressOpensTheMenuOnThatChip(t *testing.T) {
	ed, c := addrPage(t, testGrpc, testMCP)
	b := ed.addrs.chips[1].Bounds()

	if !c.HandleMouse(input.MouseEvent{
		Kind: input.MousePress, Button: input.ButtonRight, X: b.X + 1, Y: b.Y,
	}) {
		t.Fatal("the right press was not consumed")
	}
	if !ed.addrs.IsMenuOpen() {
		t.Fatal("the right press did not open the menu")
	}
	if ed.addrs.menuFor != 1 {
		t.Errorf("the menu opened on chip %d, want the one that was clicked (1)", ed.addrs.menuFor)
	}
	if len(ed.addrs.items) == 0 {
		t.Fatal("the menu opened with no items")
	}
	// The surface must be placed ABOVE the bar: the status bar is the
	// last row, so a menu opening downwards would be entirely off screen.
	c.Frame()
	sb := ed.addrs.popup().SurfaceBounds()
	if sb.H == 0 || sb.Y+sb.H > b.Y+1 {
		t.Errorf("the menu surface is at %+v with the chip on row %d: it must open upwards",
			sb, b.Y)
	}
}

// TestALeftPressDoesNotOpenTheMenu is the discrimination half. Without
// it the test above would pass for a handler that opened the menu on
// ANY press and never checked the button at all.
func TestALeftPressDoesNotOpenTheMenu(t *testing.T) {
	ed, c := addrPage(t, testGrpc, testMCP)
	b := ed.addrs.chips[0].Bounds()
	c.HandleMouse(input.MouseEvent{
		Kind: input.MousePress, Button: input.ButtonLeft, X: b.X + 1, Y: b.Y,
	})
	if ed.addrs.IsMenuOpen() {
		t.Error("a LEFT press opened the context menu, so the button field is not being read")
	}
}

// TestTheMenuKeyOpensAndEscDismisses is the keyboard equivalent for the
// menu — mandatory for the same pty reason as the copy gestures.
func TestTheMenuKeyOpensAndEscDismisses(t *testing.T) {
	ed, c := addrPage(t, testGrpc, testMCP)
	openMenuKey(t, ed, c)
	if !ed.addrs.IsMenuOpen() {
		t.Fatal("m did not open the endpoint menu")
	}
	if ed.addrs.menuFor != 0 {
		t.Errorf("m opened the menu on chip %d, want the current one (grpc)", ed.addrs.menuFor)
	}
	if !c.Handle(input.KeyOf(input.Named(input.KeyEsc))) {
		t.Fatal("esc was not consumed by the open menu")
	}
	if ed.addrs.IsMenuOpen() {
		t.Error("esc did not dismiss the menu")
	}
}

// TestAnOpenMenuSwallowsThePagesBareLetters is why the popup is Modal.
// The page binds bare "q" to Quit and bare "x" to Delete; a menu that
// let those through would quit the editor while a menu was open.
func TestAnOpenMenuSwallowsThePagesBareLetters(t *testing.T) {
	ed, c := addrPage(t, testGrpc, testMCP)
	openMenuKey(t, ed, c)
	if !ed.addrs.IsMenuOpen() {
		t.Fatal("the menu did not open")
	}
	before := len(ed.doc().Kids)
	if !c.Handle(input.KeyOf(input.Rune('x'))) {
		t.Error("a bare x escaped the open menu; the page's Delete binding would have fired")
	}
	if got := len(ed.doc().Kids); got != before {
		t.Errorf("the document lost a child (%d then %d) to a key typed under an open menu",
			before, got)
	}
}

// TestTheMenuCopiesTheAddress walks the whole menu route: open, choose,
// activate, and the clipboard gets the address.
func TestTheMenuCopiesTheAddress(t *testing.T) {
	ed, c := addrPage(t, testGrpc, testMCP)
	sent := okCopy(t)

	openMenuKey(t, ed, c)
	if !c.Handle(input.KeyOf(input.Named(input.KeyEnter))) {
		t.Fatal("enter was not consumed by the open menu")
	}
	if ed.addrs.IsMenuOpen() {
		t.Error("activating an item left the menu open")
	}
	if *sent != "127.0.0.1:45783" {
		t.Errorf("the first menu item copied %q, want the gRPC address", *sent)
	}
}

// TestTheMenuCanCopyBothEndpoints is the second item, which exists
// because a client config usually wants both and the row used to show
// them as one string.
func TestTheMenuCanCopyBothEndpoints(t *testing.T) {
	ed, c := addrPage(t, testGrpc, testMCP)
	sent := okCopy(t)

	openMenuKey(t, ed, c)
	c.Handle(input.KeyOf(input.Named(input.KeyDown)))
	c.Handle(input.KeyOf(input.Named(input.KeyEnter)))

	for _, want := range []string{"127.0.0.1:45783", "http://127.0.0.1:46271/mcp"} {
		if !strings.Contains(*sent, want) {
			t.Errorf("copy-all produced %q, which is missing %q", *sent, want)
		}
	}
}

// TestAFailedCopyFromTheMenuAlsoRefusesToConfirm closes the last route
// into the success state. Every path that can set copyDone goes through
// copyText, and this checks the menu reaches it the same way.
func TestAFailedCopyFromTheMenuAlsoRefusesToConfirm(t *testing.T) {
	ed, c := addrPage(t, testGrpc, testMCP)
	swapClipboard(t, fmt.Errorf("over the OSC 52 limit"))

	openMenuKey(t, ed, c)
	c.Handle(input.KeyOf(input.Named(input.KeyEnter)))

	if got := ed.addrs.notice.outcome.Get(); got != copyFailed {
		t.Errorf("a menu copy that failed reports %v, want copyFailed", got)
	}
	if got := noticeText(ed.addrs); strings.Contains(got, "copied") {
		t.Errorf("the chip reads %q, confirming a copy that did not happen", got)
	}
}

// ---- 8. lifetime ----

// TestTheFlashRevertsThroughTheDelayGroup is the animation actually
// running: with a dispatcher attached, the flash clears itself.
func TestTheFlashRevertsThroughTheDelayGroup(t *testing.T) {
	restore := shortHolds(t)
	defer restore()

	ed, c := addrPage(t, testGrpc, testMCP)
	okCopy(t)
	disp := gooey.NewDispatcher()
	c.Start(disp)

	ed.addrs.CopyCurrent()
	if noticeText(ed.addrs) == "" {
		t.Fatal("no flash was set, so the revert below would be vacuous")
	}

	if !drainUntil(disp, 2*time.Second, func() bool { return noticeText(ed.addrs) == "" }) {
		t.Errorf("the flash still reads %q after its hold elapsed: the delay never posted",
			noticeText(ed.addrs))
	}
	if got := ed.addrs.notice.outcome.Get(); got != copyIdle {
		t.Errorf("the dot is still %v after the flash cleared", got)
	}
}

// TestClosingTheCompositionCancelsAPendingRevert is the lifetime
// contract, and the reason gooey.Delays is used rather than a
// hand-rolled done/stopped pair: stop CLOSES AND JOINS, so once the
// Composer has closed, no revert can still arrive and touch a component
// the composition has finished with.
//
// It discriminates because the hold is short: without the cancel the
// timer fires well inside the wait below and the flash clears.
func TestClosingTheCompositionCancelsAPendingRevert(t *testing.T) {
	restore := shortHolds(t)
	defer restore()

	ed, c := addrPage(t, testGrpc, testMCP)
	okCopy(t)
	disp := gooey.NewDispatcher()
	c.Start(disp)

	ed.addrs.CopyCurrent()
	flash := noticeText(ed.addrs)
	if flash == "" {
		t.Fatal("no flash was set, so this test asserts nothing")
	}

	c.Close() // closes the gate and joins every delay in flight

	// Long enough that an uncancelled timer would have fired many times
	// over, and every closure it could have posted would have run.
	deadline := time.Now().Add(20 * addrFlashHold)
	for time.Now().Before(deadline) {
		disp.Drain()
		time.Sleep(time.Millisecond)
	}
	if got := noticeText(ed.addrs); got != flash {
		t.Errorf("the chip reads %q after Close, want the flash %q left untouched.\n"+
			"Close must be a barrier: a revert arriving afterwards is a closure running "+
			"against a component the Composer has already stopped.", got, flash)
	}
}

// shortHolds shrinks the flash durations so a test can watch one
// elapse. See the vars' comment for why they are not consts.
func shortHolds(t *testing.T) func() {
	t.Helper()
	flash, fail := addrFlashHold, addrFailHold
	addrFlashHold, addrFailHold = 5*time.Millisecond, 5*time.Millisecond
	return func() { addrFlashHold, addrFailHold = flash, fail }
}

// drainUntil pumps the dispatcher until cond holds or the deadline
// passes. The delays post onto the UI goroutine and this test IS that
// goroutine, so nothing runs until something drains.
func drainUntil(disp *gooey.Dispatcher, within time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		disp.Drain()
		if cond() {
			return true
		}
		time.Sleep(time.Millisecond)
	}
	return cond()
}

// ---- 9. structure ----

// TestTheStripIsAStartable is a claim about which contract this code
// signed. gooey.Delays owns the close-and-join barrier for a group of
// one-shot delays; a Startable that hand-rolls its own done/stopped
// pair is a claim that neither framework shape fits, and the half people
// get wrong is the join.
func TestTheStripIsAStartable(t *testing.T) {
	var _ gooey.Startable = (*addrStrip)(nil)
	var _ gooey.MouseHandler = (*addrStrip)(nil)
	var _ gooey.Container = (*addrStrip)(nil)
	var _ gooey.Focusable = (*addrStrip)(nil)

	// The chips must NOT be focus stops: one affordance may not spend N
	// slots in the page's single tab ring.
	var chip any = &addrChip{}
	if _, ok := chip.(gooey.Focusable); ok {
		t.Error("a chip is focusable; the strip is meant to be the only stop")
	}
	// And they must be LEAVES, or the Composer would stop pre-clearing
	// their bounds and a short flash would leave the tail of the old
	// address behind it.
	if _, ok := chip.(gooey.Container); ok {
		t.Error("a chip is a Container, so the Composer no longer pre-clears its bounds: " +
			"a flash shorter than the address would strand the characters it did not cover")
	}
}

// bareStrip builds a strip with no editor behind it. Every endpoint gets
// a fresh link, which means the zero value: linkDown. That is the right
// default for a fabricated endpoint — nothing is behind it — and the
// direction the real default fails in too.
func bareStrip(endpoints ...string) *addrStrip {
	links := map[string]*endpointLink{}
	return newAddrStrip(endpoints, func(label string) *endpointLink {
		if links[label] == nil {
			links[label] = newEndpointLink()
		}
		return links[label]
	})
}

// TestAnEmptyStripRefusesFocus keeps a dead tab stop out of the ring.
func TestAnEmptyStripRefusesFocus(t *testing.T) {
	if bareStrip().AcceptsFocus() {
		t.Error("a strip with no endpoints accepts focus: the user lands on a stop that " +
			"can do nothing")
	}
	if !bareStrip(testGrpc).AcceptsFocus() {
		t.Error("a strip with an endpoint refuses focus, so it is unreachable by tab")
	}
}

// TestAnEndpointWithNoSpaceStillBuilds guards the parse. ed.serving is
// built by main() as "<label> <address>", but a shape this code cannot
// split must degrade to showing the whole string rather than losing it.
func TestAnEndpointWithNoSpaceStillBuilds(t *testing.T) {
	s := bareStrip("unix:///tmp/x.sock")
	if len(s.chips) != 1 {
		t.Fatalf("built %d chips", len(s.chips))
	}
	if !strings.Contains(s.chips[0].idleText(), "unix:///tmp/x.sock") {
		t.Errorf("the chip reads %q and lost the address", s.chips[0].idleText())
	}
}

// TestEllipsizeNeverExceedsItsWidth is the guard on the one place a flash
// meets a fixed width. Overrunning would write into the sibling chip's
// cells, whose node is clean and would not repaint over it.
//
// It named clipTo until the panes were stacked, and dock.go turned out to
// have a clipTo of its OWN that hard-truncates. The rename is not
// cosmetic: this test is what proved the difference is observable. With
// the call still spelled clipTo it bound to the other function and the
// second assertion below failed with `truncated to "abcd", want an
// ellipsis marking the loss` — which is exactly what deleting this
// function as a "duplicate" would have shipped, silently, everywhere in
// the address strip.
func TestEllipsizeNeverExceedsItsWidth(t *testing.T) {
	for _, w := range []int{0, 1, 2, 5, 40} {
		for _, s := range []string{"", "x", "copy failed: terminal suspended", strings.Repeat("é", 60)} {
			if got := len([]rune(ellipsize(s, w))); got > w {
				t.Errorf("ellipsize(%q, %d) is %d runes wide", s, w, got)
			}
		}
	}
	// The DISCRIMINATING half: width alone is satisfied by a hard cut, so
	// without this clause dock.go's clipTo would pass this test.
	if got := ellipsize("abcdef", 4); got != "abc…" {
		t.Errorf("ellipsize truncated to %q, want an ellipsis marking the loss", got)
	}
}

// menuItemsAreShared records what was agreed with the MenuBar's owner:
// components.MenuItem is reused as the item shape, so the two menus
// describe an item the same way, but the MenuBar's dropdown is drawn by
// its own unexported drawDropdown and there is no reusable item-list
// component to instantiate. See the report.
var _ = []components.MenuItem(nil)

// ---- 10. written is not landed ----

// TestInsideTmuxTheChipDoesNotClaimSuccess is the second-hardest
// assertion here, and it is one this file originally got WRONG — caught
// in review by the agent that owns clipboard.go.
//
// ed.copyToSystem returns nil when the OSC 52 escape was WRITTEN. That
// is all OSC 52 can ever report; there is no acknowledgement. But tmux
// and GNU screen swallow the sequence by DEFAULT, so inside one the
// write succeeds, the clipboard does not change, and a green tick is a
// confirmation for a copy that did not land — the exact silent failure
// this affordance exists to prevent, reached through the SUCCESS branch
// rather than the error branch. A suite that only exercised the error
// branch would have missed it entirely.
func TestInsideTmuxTheChipDoesNotClaimSuccess(t *testing.T) {
	ed, _ := addrPage(t, testGrpc, testMCP)
	okCopy(t) // the write itself succeeds
	ed.addrs.caveatFn = func() string { return "inside tmux: needs `set -g set-clipboard on`" }

	ed.addrs.CopyCurrent()

	if got := ed.addrs.notice.outcome.Get(); got != copyCaveat {
		t.Errorf("with a clipboard caveat in force the notice reports %v, want "+
			"copyCaveat: the escape was written but tmux swallows it by default, so a "+
			"confirmation here is for a copy that did not land", got)
	}
	if got := noticeText(ed.addrs); strings.Contains(got, "copied") {
		t.Errorf("the notice reads %q and claims the copy landed; it can only ever know "+
			"that the sequence was written", got)
	}
	if copyCaveat.textStyle() == copyDone.textStyle() {
		t.Error("the caveat state wears the success colour, so the cue carries no warning")
	}
}

// TestWithNoCaveatTheChipDoesConfirm is the discrimination half. Without
// it the test above would pass for a chip that never confirmed anything
// at all, which would be useless in the ordinary case.
func TestWithNoCaveatTheChipDoesConfirm(t *testing.T) {
	ed, _ := addrPage(t, testGrpc, testMCP)
	okCopy(t)
	ed.addrs.caveatFn = func() string { return "" }

	ed.addrs.CopyCurrent()

	if got := ed.addrs.notice.outcome.Get(); got != copyDone {
		t.Errorf("with no caveat the dot is %v, want copyDone", got)
	}
	if got := noticeText(ed.addrs); !strings.Contains(got, "copied") {
		t.Errorf("the chip reads %q and never confirms an ordinary copy", got)
	}
}

// TestTheMenuCarriesTheFullCaveat is where the reason fits. The chip's
// width is fixed, so it can only afford "sent, unverified"; a popup is
// sized to its own content, so the sentence that tells the user what to
// change goes there rather than being clipped away.
func TestTheMenuCarriesTheFullCaveat(t *testing.T) {
	ed, c := addrPage(t, testGrpc, testMCP)
	const caveat = "inside tmux: needs `set -g set-clipboard on`"
	ed.addrs.caveatFn = func() string { return caveat }

	openMenuKey(t, ed, c)

	var found bool
	for _, it := range ed.addrs.items {
		if it.Text == caveat {
			found = true
			if gooey.CanExecute(it.Action) {
				t.Error("the caveat row is activatable; it is a statement, not a command")
			}
		}
	}
	if !found {
		t.Errorf("the menu does not carry the caveat text; its items are %v", ed.addrs.items)
	}
}

// TestNoCaveatAddsNoMenuRow keeps the row from becoming permanent
// furniture in the ordinary case.
func TestNoCaveatAddsNoMenuRow(t *testing.T) {
	ed, c := addrPage(t, testGrpc, testMCP)
	ed.addrs.caveatFn = func() string { return "" }
	openMenuKey(t, ed, c)
	if n := len(ed.addrs.items); n != 2 {
		t.Errorf("the menu has %d items with no caveat in force, want the 2 real commands", n)
	}
}

// TestTheCaveatIsWiredToTheRealDetector guards the wiring rather than
// the behaviour: a strip whose caveatFn the builder never set would be
// silently exempt from everything above, however well tested.
func TestTheCaveatIsWiredToTheRealDetector(t *testing.T) {
	ed, _ := addrPage(t, testGrpc, testMCP)
	if ed.addrs.caveatFn == nil {
		t.Fatal("the builder left caveatFn nil, so the tmux/screen check never runs in " +
			"the shipped app")
	}
	if got, want := ed.addrs.caveat(), term.ClipboardCaveat(); got != want {
		t.Errorf("the strip reports caveat %q, the detector says %q", got, want)
	}
}

// TestASqueezedChipPaintsItsDotAndNothingElse walks the widths where a
// chip has fewer cells than its dot-space-text layout needs.
//
// The review of #391 (issue #419) reported this as a chip writing
// OUTSIDE its bounds at one cell wide: Render's guard is `b.W <= 0`, and
// the space goes to b.X+1. That does not reach the plane — Composer.build
// clips every paint node to its own rect (#357) and refuses the cell,
// and in the shipped strip a one-wide chip only ever falls at the right
// edge of the row where the column is off-buffer as well. So there is
// nothing to fix and nothing a bounds assertion could fail on.
//
// What the width DOES reach is the arithmetic: b.W-2 goes negative and
// is handed to ellipsize and then to padTo, one of which builds a
// strings.Repeat. Those guards are the fireable part, and this is what
// stands on them — a page that renders rather than panics, with the dot
// in the one cell the chip has. The row is swept from one column up so
// the narrow cases are reached by the real layout rather than by an
// Arrange the next Frame would undo.
func TestASqueezedChipPaintsItsDotAndNothingElse(t *testing.T) {
	narrow := 0
	for w := 1; w <= 40; w++ {
		ed := newEditor(editorFS())
		ed.serving = []string{testGrpc, testMCP}
		src, err := os.ReadFile("wysiwyg.gooey")
		if err != nil {
			t.Fatal(err)
		}
		root, err := markup.Build(src, ed.ctx)
		if err != nil {
			t.Fatalf("w=%d: the editor's own page does not load: %v", w, err)
		}
		ed.rebuild()
		c := gooey.NewComposer(root, w, 12)
		f, _ := c.Frame()
		if ed.addrs == nil {
			c.Close()
			t.Fatalf("w=%d: no endpoint strip", w)
		}
		for i, ch := range ed.addrs.chips {
			b := ch.Bounds()
			if b.W <= 0 || b.W >= 3 {
				continue
			}
			narrow++
			if got := f.Cells.At(b.X, b.Y).Rune; got != addrDot {
				t.Errorf("w=%d chip %d has %d cells and holds %q at its origin, want the "+
					"state dot %q", w, i, b.W, got, addrDot)
			}
		}
		c.Close()
	}
	// The discrimination floor: without a width that actually squeezes a
	// chip below three cells, the loop above asserts nothing at all and
	// would stay green if the guards were removed.
	if narrow == 0 {
		t.Fatal("no terminal width between 1 and 40 gave a chip one or two cells, so " +
			"the sub-three-cell path was never entered and this test checked nothing")
	}
	t.Logf("%d squeezed chips exercised", narrow)
}

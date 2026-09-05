package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
	"github.com/WonderForgeLabs/gooey/term"
)

// The status bar's RIGHT section: one clickable chip per control-plane
// endpoint, instead of the single joined string it used to be.
//
// The addresses are the one thing on this screen a user cannot retype
// from memory and cannot guess — the ports are ephemeral (-serve
// 127.0.0.1:0), so they are different every run and the only way to get
// one into an MCP client's config was to read it off the terminal and
// type it back. Making each one its own component is what lets a click
// land on ONE of them.
//
// WHY NOT <Button>. Button has exactly two chromes (components/
// buttonchrome.go): `cell` wraps the label in "[ ]" and `pixel` is a
// three-row pill. There is no chrome-less variant. A three-row pill
// cannot fit a one-row status bar at all, and "[ 127.0.0.1:45783 ]"
// twice over spends eight cells on brackets in the most crowded row of
// the app while reading as a form control rather than as status. The
// chips below are a small MouseHandler of their own instead — which is
// also what lets one chip carry a state dot, a flash message and a
// context menu without any of those being a Button.Content string.
//
// ONE FOCUS STOP, NOT THREE. The strip is the focus stop; the chips are
// leaves and the strip keeps a "current" index, exactly the way MenuBar
// keeps a current title. Making each chip focusable would have put N
// new stops into the app's single tab ring for an affordance that is
// already reachable by two global gestures, and tab order is a
// page-wide resource.
//
// TWO KINDS OF FACT, TWO PLACES ON THE ROW. The chips report the
// SERVICE — is this endpoint up, is anything driving it — and nothing
// else. The clipboard reports the COPY, in copyNotice, which sits at
// the far end of the strip with its own cells, its own words and no
// dot.
//
// They were one thing once, and that was the mistake: the state dot
// beside "grpc" carried the last copy outcome, because nothing about
// either server's liveness was askable from this process at the time.
// A dot next to a service name is read as a connection light no matter
// what it was wired to, so it was a light that went green when the
// clipboard worked. See servelink.go for the whole argument and for
// where the liveness now comes from. What matters HERE is the shape of
// the fix: the two facts do not share a glyph and do not share a
// position, and TestTheNoticeCarriesNoServiceDot pins both halves.
//
// EACH CHIP IS ITS OWN PAINT NODE, which is the property the status bar
// was built for (components/statusbar.go) and which this must not
// spend. A copy repaints the notice and NOT either chip; one endpoint
// going quiet repaints that chip and NOT the sibling, NOT the notice,
// NOT the mode indicator in the centre and NOT the build status on the
// left. TestACopyRepaintsTheNoticeAndNoChip and
// TestOneEndpointChangingRepaintsOneChip are the pins, and they are
// damage counts rather than cell assertions because a cell assertion
// passes just as well when the whole tree repainted.
//
// NOTHING ON THIS ROW CHANGES WIDTH, and that is load-bearing rather
// than tidy. Measure runs in layout, so anything that grew to fit its
// message would re-Arrange the whole StatusBar; Left keeps its rect but
// Centre is positioned from the gap between the two edges, so the mode
// indicator would move and repaint on every copy. A chip is sized from
// its address alone and now has no other content to be sized by; the
// notice reserves copyNoticeWidth cells whether or not it has anything
// to say, and clips into them when it does — the same reserved-slot
// rule an adornment row follows. The cost is real and is stated where
// it is paid: a long error message is truncated with an ellipsis, and
// twenty cells of the row are spent on a notice that is usually blank.

// addrDot is the SERVICE state glyph, and the choice of rune is a
// correctness question rather than a taste one.
//
// It belongs to the chips and to nothing else on this row. copyNotice
// must never paint it — that is the position-and-glyph separation the
// file comment describes, and a notice that grew a dot would put the
// clipboard back on a connection light by a different route.
//
// NOTHING IN THIS REPO IS RUNE-WIDTH AWARE. render.Buffer.SetString
// advances x by exactly one per rune (render/cell.go), Text.Measure
// sizes with len([]rune(line)) (components/text.go), and there is no
// import of any width table anywhere in render/, components/ or term/.
// A two-cell emoji written into a one-cell slot therefore desynchronises
// the cell plane from the terminal's cursor for the rest of the row —
// and by the damage model the cells it corrupts belong to clean nodes
// that will not repaint, so the corruption persists until something
// unrelated dirties them.
//
// U+25CF BLACK CIRCLE is East Asian Ambiguous, i.e. one cell outside a
// CJK locale, and this app already ships two glyphs of exactly that
// class in the same frame: "•" in the attribute pane's modified marker
// and "✓" in the build status. Colour, not shape, carries the meaning,
// which also keeps it legible where the terminal has no colour at all.
const addrDot = '●'

// addrGap is the run of spaces between two chips — the same three the
// joined string used, so the row reads identically when nothing is
// flashing.
const addrGap = 3

// How long a chip wears its flash. Failure holds longer than success on
// purpose: a tick is a confirmation you already expected, an error is
// something you have to read.
//
// Vars rather than consts so the tests can shorten them. That is not a
// hole for production code to reach through — it is what lets the
// revert and the stop-is-a-barrier assertions be made AT ALL. Without
// it the only affordable test is "the flash is set", which passes
// whether or not the delay is ever cancelled, and an assertion that
// cannot fail is not evidence.
var (
	addrFlashHold = 1200 * time.Millisecond
	addrFailHold  = 3000 * time.Millisecond
)

// copyOutcome is what the NOTICE says — never what a chip says. All
// four states are OBSERVED: each is written by the copy path that
// actually ran, never sampled on a clock.
//
// IT IS NOT CONNECTION STATE AND IT NO LONGER SITS WHERE CONNECTION
// STATE SITS. The clipboard succeeding tells you nothing about whether
// a gRPC client is attached, and the two answer questions a user asks
// at different moments — "did that land" once, right after a
// keystroke, against "can I still drive this thing" at any time. See
// the file comment and servelink.go.
type copyOutcome int

const (
	copyIdle copyOutcome = iota
	copyDone
	// copyCaveat is the state slice-clipboard's review added, and it is
	// the one this file would otherwise have got wrong.
	//
	// ed.copyToSystem returns nil when the OSC 52 escape was WRITTEN,
	// which is all OSC 52 can ever report — there is no acknowledgement.
	// But tmux and GNU screen swallow the sequence BY DEFAULT, so inside
	// one the write succeeds, the clipboard does not change, and a green
	// tick is a confirmation for a copy that did not land: precisely the
	// silent failure this affordance exists to avoid, reached from the
	// success branch rather than the error branch.
	//
	// term.ClipboardCaveat() names that condition. Amber, not green —
	// and amber now means something real, which it did not before.
	copyCaveat
	copyFailed
)

// textStyle colours the notice's WORDS. Deliberately not named dotStyle
// and deliberately not returning a style for a glyph: the notice paints
// text, the chips paint a dot, and keeping the two style functions
// distinct is what stops a later edit from quietly reuniting them.
func (o copyOutcome) textStyle() render.Style {
	switch o {
	case copyDone:
		return render.Style{Fg: render.RGB(120, 200, 140)}
	case copyCaveat:
		// The editor's existing "warn" colour.
		return render.Style{Fg: render.RGB(255, 170, 60)}
	case copyFailed:
		return render.Style{Fg: render.RGB(220, 90, 90)}
	default:
		// Idle. There is nothing to read, so there is nothing to colour.
		return render.Style{Dim: true}
	}
}

// dotStyle is the SERVICE traffic light, and the four colours are the
// four claims in servelink.go's linkState — one colour per claim, no
// colour reused for two different facts within one chip.
//
// Green means "up" in both columns, and the extra thing it means for
// gRPC is "and something is attached". That asymmetry is load-bearing:
// grey beside "grpc" says the endpoint is fine and nobody is driving
// it, which is a state MCP does not have and must not be given a
// colour for. Colour rather than shape carries all of it, for the
// reason addrDot gives.
//
// ONE COLOUR, ONE MEANING, ACROSS BOTH VOCABULARIES ON THIS ROW. The
// notice and the chips share a row, so they have to share a reading of
// colour or the row has two conflicting legends: red is "this is
// broken", green is "this is fine", amber is "this may not have worked
// and you should check". The SUBJECT is disambiguated by position, by
// glyph and by the notice's words — never by the colour, which means
// the same thing in both places.
//
// THAT IS WHY linkIdle IS GREY AND NOT AMBER, and this is the arm where
// somebody will reach for amber. "Serving, nobody attached" is not a
// caution — nothing is wrong and there is nothing to check; it is the
// ordinary state of an endpoint waiting for a client. Painting it amber
// would put amber on this row meaning "fine, just quiet" one place and
// "your copy may not have landed" three cells away, which is the
// one-cue-two-facts collapse this whole file exists to undo, moved up a
// layer. TestNoChipStateWearsTheCautionColour is the pin, and it is
// deliberately about the caution colour specifically rather than about
// palette overlap in general: red and green ARE shared, correctly, and
// a test that forbade all overlap would be forbidding the consistency
// that makes the row readable.
func (s linkState) dotStyle() render.Style {
	switch s {
	case linkActive, linkServing:
		return render.Style{Fg: render.RGB(120, 200, 140)}
	case linkIdle:
		return render.Style{Fg: render.RGB(140, 140, 150)}
	default: // linkDown
		return render.Style{Fg: render.RGB(220, 90, 90)}
	}
}

// addrChip is one endpoint: a connection dot, a label and an address,
// on one row, at a width that cannot move.
type addrChip struct {
	gooey.Base

	label string // "grpc", "mcp"
	addr  string // "127.0.0.1:45783", "http://127.0.0.1:46271/mcp"

	strip *addrStrip
	idx   int

	// link is this endpoint's connection state, and it belongs to the
	// EDITOR rather than to the chip — a hot reload builds new chips
	// against the same still-running servers, and state held here would
	// be reset to "down" by every save of wysiwyg.gooey. Reading it in
	// Render is what makes a transition repaint this chip alone.
	//
	// Never nil: newAddrStrip takes a factory that makes one on demand.
	link *endpointLink
}

func newAddrChip(strip *addrStrip, idx int, label, addr string, link *endpointLink) *addrChip {
	return &addrChip{label: label, addr: addr, strip: strip, idx: idx, link: link}
}

// idleText is what the chip reads when nothing has happened — the same
// "grpc 127.0.0.1:45783" the joined string produced.
func (c *addrChip) idleText() string { return c.label + " " + c.addr }

// chipWidth is fixed for the life of the chip: dot, space, then the
// text. Now that the flash lives in copyNotice there is nothing else a
// chip could be sized by, which turns the file comment's rule from a
// discipline into a structural fact.
func (c *addrChip) chipWidth() int { return 2 + len([]rune(c.idleText())) }

func (c *addrChip) Measure(avail gooey.Size) gooey.Size {
	return gooey.Size{W: min(c.chipWidth(), avail.W), H: min(1, avail.H)}
}

// Render paints the chip. Every Get is HOISTED above the bounds check
// and above every branch, because a dependency is recorded by the Get
// that actually RUNS: one left behind an early return drops out of the
// set on the frames it is skipped, and the chip goes silently deaf to
// that property.
func (c *addrChip) Render(f *gooey.Frame) {
	state := c.link.state.Get()
	current, focused := c.strip.currentIndex(), c.strip.IsFocused()

	b := c.Bounds()
	if b.W <= 0 || b.H <= 0 {
		return
	}

	st := render.Style{Dim: true}
	dot := state.dotStyle()
	if focused && current == c.idx {
		st.Reverse = true
		dot.Reverse = true
	}

	f.Cells.Set(b.X, b.Y, addrDot, dot)
	f.Cells.Set(b.X+1, b.Y, ' ', st)
	f.Cells.SetString(b.X+2, b.Y, padTo(ellipsize(c.idleText(), b.W-2), b.W-2), st)
}

// copyAddr is the default gesture: copy this endpoint's address. The
// feedback goes to the strip's notice, not to this chip — see the file
// comment.
func (c *addrChip) copyAddr() { c.strip.copyText(c.label, c.addr, c.label+" copied") }

// copyNoticeWidth is the notice's reserved width, in cells. Fixed, and
// paid for whether or not there is anything to say, because a slot that
// appeared and vanished would change the strip's measured width and
// re-Arrange the whole StatusBar — moving the centre mode indicator on
// every copy.
//
// TWENTY-FOUR IS MEASURED, NOT CHOSEN. It is the width of the failure
// message this app actually produces — "copy failed: no terminal", what
// ed.copyToSystem returns whenever there is no tty, which is every run
// under `go test`, every run under a pipe, and the single most likely
// failure a user will see. It also fits the caveat wording ("grpc copy
// unverified", 20) and every confirmation.
//
// It was 20 first, and 20 clipped that message to "copy failed: no
// ter…" — an ellipsis exactly where the reason starts, on the one
// message whose entire value is the reason. A reserved slot that cannot
// hold its own worst case is a slot sized to the happy path.
//
// A longer reason from some other failure still clips, and the full
// sentence is in the context menu. Widen this if a common message
// outgrows it; do not narrow it to save cells, because the cells come
// back on their own — Arrange gives the notice only what the addresses
// do not need, so on a narrow terminal this reservation is already 0.
const copyNoticeWidth = 24

// copyNotice is where the CLIPBOARD reports, and its separateness from
// the chips is the whole point rather than a layout preference.
//
// It has its own cells at the opposite end of the strip from the
// service names, it says its message in words that name the clipboard
// ("copied", "copy failed", "copy unverified"), and it paints NO DOT.
// A user glancing at the row cannot mistake it for a statement about
// grpc or mcp being up, which is exactly what the previous arrangement
// invited.
type copyNotice struct {
	gooey.Base

	// outcome and text are the notice's OWN sources, so writing either
	// dirties this paint node and no other — in particular not a chip's.
	// TestACopyRepaintsTheNoticeAndNoChip is the pin.
	outcome *prop.Property[copyOutcome]
	text    *prop.Property[string]
}

func newCopyNotice() *copyNotice {
	return &copyNotice{outcome: prop.NewSource(copyIdle), text: prop.NewSource("")}
}

func (n *copyNotice) Measure(avail gooey.Size) gooey.Size {
	return gooey.Size{W: min(copyNoticeWidth, avail.W), H: min(1, avail.H)}
}

// Render paints the message, or blanks the reserved cells.
//
// Both Gets are HOISTED above the bounds check, because a dependency is
// recorded by the Get that actually RUNS: one behind the early return
// would drop out of the set on any frame where the notice was squeezed
// to zero width, and the notice would go permanently deaf to it.
func (n *copyNotice) Render(f *gooey.Frame) {
	outcome, text := n.outcome.Get(), n.text.Get()

	b := n.Bounds()
	if b.W <= 0 || b.H <= 0 {
		return
	}
	// Blanking the reserved cells is not optional. A leaf pre-clears to
	// the nearest ancestor's background, but the notice's own bounds do
	// not shrink when its message does, so a shorter message written
	// without padding would leave the tail of a longer one behind.
	f.Cells.SetString(b.X, b.Y, padTo(ellipsize(text, b.W), b.W), outcome.textStyle())
}

// addrStrip lays the chips out and owns everything shared between them:
// the focus stop, the current index, the delay group, and the context
// menu's popup.
type addrStrip struct {
	gooey.Base
	gooey.FocusState

	chips  []*addrChip
	notice *copyNotice
	kids   []gooey.Component

	// gen stales a pending revert. A second copy while the first is
	// still showing must not have its message cleared early by the
	// first one's delay arriving. One counter for the strip because
	// there is one notice — it was per chip when every chip carried its
	// own flash.
	gen int

	// copyFn and caveatFn are the strip's collaborators, injected by the
	// builder rather than reached for. Note copyFn is NOT a clipboard
	// test seam — apps/wysiwyg/clipboard.go owns the only one of those
	// (the package-level writeSystemClipboard var, swapped by
	// clipEditor in clipboard_test.go). A second seam for the same
	// behaviour is how two tests come to disagree about what a copy
	// does.
	copyFn   func(string) error
	caveatFn func() string

	// ONE delay group for the whole strip, per gooey.Delays' contract:
	// any number of flashes may be in flight and they all stop together
	// when the composition closes. Hand-rolling a done/stopped pair here
	// would be the seventh copy of a barrier this framework already owns
	// — and the half people get wrong is the JOIN, without which a
	// revert can post after Close.
	delays gooey.Delays

	pop     *components.Popup
	items   []components.MenuItem
	menuFor int // the chip index the open menu belongs to

	currentP *prop.Property[int] // highlighted chip
	menuSelP *prop.Property[int] // highlighted item in the open menu
}

// newAddrStrip builds the strip. linkFor resolves a label to the
// endpoint's connection state — ed.link in the app, a stub in a test —
// and is a parameter rather than a reach into the editor so the strip
// can be built without one.
func newAddrStrip(endpoints []string, linkFor func(string) *endpointLink) *addrStrip {
	s := &addrStrip{
		notice:   newCopyNotice(),
		currentP: prop.NewSource(0),
		menuSelP: prop.NewSource(0),
	}
	for i, e := range endpoints {
		// ed.serving entries are "<label> <address>" — the same shape
		// main() appends and the same one the joined string showed.
		label, addr, ok := strings.Cut(e, " ")
		if !ok {
			label, addr = "serving", e
		}
		s.chips = append(s.chips, newAddrChip(s, i, label, addr, linkFor(label)))
	}
	return s
}

// copyText runs a copy and REPORTS WHAT ACTUALLY HAPPENED, in the
// notice.
//
// This is the whole point of the affordance and the one thing that must
// not be got wrong: a confirmation shown for a copy that did not occur
// is worse than no affordance at all, because the user walks away
// believing they have the address. The success branch is therefore
// reachable ONLY through a nil error, and TestAFailedCopyNeverShowsSuccess
// is the mutation-checked pin on it.
func (s *addrStrip) copyText(label, text, okMsg string) {
	s.gen++
	gen := s.gen
	if err := s.copy(text); err != nil {
		s.show(copyFailed, "copy failed: "+err.Error(), addrFailHold, gen)
		return
	}
	// WRITTEN is not LANDED. See copyCaveat.
	if s.caveat() != "" {
		// Short, because the notice's width is fixed; the full sentence
		// goes in the context menu, which is sized to its content.
		s.show(copyCaveat, label+" copy unverified", addrFailHold, gen)
		return
	}
	s.show(copyDone, okMsg, addrFlashHold, gen)
}

func (s *addrStrip) show(o copyOutcome, text string, hold time.Duration, gen int) {
	s.notice.outcome.Set(o)
	s.notice.text.Set(text)
	s.after(hold, func() { s.revert(gen) })
}

// revert takes the notice back to blank, unless a newer copy has
// claimed it since this delay was armed.
//
// The already-idle guard is not decoration: prop.Set does not compare,
// so setting a property to what it already holds still invalidates
// every dependent and still costs a repaint (prop/prop.go). These reads
// happen in a dispatcher closure, outside any evaluation, so they
// subscribe to nothing.
func (s *addrStrip) revert(gen int) {
	if gen != s.gen {
		return
	}
	if s.notice.text.Get() == "" && s.notice.outcome.Get() == copyIdle {
		return
	}
	s.notice.outcome.Set(copyIdle)
	s.notice.text.Set("")
}

// Start arms the flash delays. post is the only legal route back to the
// property graph, and the returned stop closes the gate and JOINS every
// delay still in flight — once it returns, no revert can arrive.
func (s *addrStrip) Start(post func(func())) func() { return s.delays.Start(post) }

func (s *addrStrip) after(d time.Duration, fn func()) {
	if !s.delays.Armed() {
		// No dispatcher (a unit test driving Frame by hand). Declining is
		// right: a revert that ran immediately would erase the flash the
		// caller just set, and a test could never observe it.
		return
	}
	s.delays.After(d, fn)
}

func (s *addrStrip) copy(text string) error {
	if s.copyFn != nil {
		return s.copyFn(text)
	}
	return fmt.Errorf("clipboard not available")
}

// caveat names the reason a written copy might not have landed, or "".
func (s *addrStrip) caveat() string {
	if s.caveatFn != nil {
		return s.caveatFn()
	}
	return ""
}

// SetFocusManager receives the input tree (gooey.FocusHost) and forwards
// it to the popup, which is how focus restore and pointer capture reach
// the framework.
func (s *addrStrip) SetFocusManager(fm *gooey.FocusManager) { s.popup().SetFocusManager(fm) }

// AcceptsFocus refuses focus when there is nothing to act on. An empty
// strip that was still a tab stop would be a stop the user lands on and
// cannot leave a mark from.
func (s *addrStrip) AcceptsFocus() bool { return len(s.chips) > 0 }

func (s *addrStrip) popup() *components.Popup {
	if s.pop == nil {
		s.pop = components.NewPopup(s, s.drawMenu)
		s.pop.Modal = true // an open menu swallows the page's bare-letter gestures
	}
	return s.pop
}

func (s *addrStrip) ChildComponents() []gooey.Component {
	p := s.popup()
	s.kids = s.kids[:0]
	// The notice first, which is document order and therefore the order
	// the row reads: clipboard feedback at the far end, then the
	// service names.
	s.kids = append(s.kids, s.notice)
	for _, c := range s.chips {
		s.kids = append(s.kids, c)
	}
	// Last by convention, not by necessity: the surface is a
	// gooey.Overlay and paints over whatever it covers from anywhere in
	// this slice. Position still orders the HIT-TEST walk, which runs in
	// document order, and an open popup takes the pointer capture — so
	// neither half depends on this being the append that comes last.
	s.kids = append(s.kids, p.Surface())
	return s.kids
}

func (s *addrStrip) currentIndex() int {
	i := s.currentP.Get()
	if i < 0 || i >= len(s.chips) {
		return 0
	}
	return i
}

func (s *addrStrip) setCurrent(i int) {
	if len(s.chips) == 0 || i == s.currentP.Get() {
		return // prop.Set does not compare; an unchanged index must not repaint
	}
	s.currentP.Set(clampInt(i, 0, len(s.chips)-1))
}

// naturalChipsWidth is what the chips want, ignoring what is available
// — the reservation Arrange works back from.
func (s *addrStrip) naturalChipsWidth() int {
	w := 0
	for i, c := range s.chips {
		if i > 0 {
			w += addrGap
		}
		w += c.chipWidth()
	}
	return w
}

func (s *addrStrip) Measure(avail gooey.Size) gooey.Size {
	// MeasureChild on every child, not chipWidth arithmetic: skipping it
	// silently drops the margin/size/align/visibility sandwich.
	w := gooey.MeasureChild(s.notice, avail).W + addrGap
	for i, c := range s.chips {
		if i > 0 {
			w += addrGap
		}
		w += gooey.MeasureChild(c, avail).W
	}
	return gooey.Size{W: min(w, avail.W), H: min(1, avail.H)}
}

// Arrange places the notice, then the chips left to right, then the
// menu above the row.
//
// THE CHIPS GET THE CELLS FIRST. The addresses are the one thing on
// this screen a user cannot retype from memory — the ports are
// ephemeral — while the notice is a message they can simply repeat the
// gesture to see again. So a row too narrow for both eats the notice,
// which is why its width is computed from what is left rather than
// taken off the front.
//
// The open flag is read HERE, in layout, which is outside any
// evaluation — a plain read that records no dependency. The surface's
// appear and vanish are the Composer's bounds sweep, not this.
func (s *addrStrip) Arrange(r gooey.Rect) {
	s.Base.Arrange(r)
	nw := clampInt(r.W-s.naturalChipsWidth()-addrGap, 0, copyNoticeWidth)
	gooey.MeasureChild(s.notice, gooey.Size{W: nw, H: r.H})
	gooey.ArrangeChild(s.notice, gooey.Rect{X: r.X, Y: r.Y, W: nw, H: min(1, r.H)})

	x := r.X + nw
	for i, c := range s.chips {
		if i > 0 || nw > 0 {
			x += addrGap
		}
		w := min(c.chipWidth(), max(0, r.X+r.W-x))
		gooey.MeasureChild(c, gooey.Size{W: w, H: r.H})
		gooey.ArrangeChild(c, gooey.Rect{X: x, Y: r.Y, W: w, H: min(1, r.H)})
		x += w
	}
	p := s.popup()
	show := p.IsOpen() && len(s.items) > 0
	pr := gooey.Rect{X: r.X, Y: r.Y}
	if show {
		pr = s.menuRect()
	}
	p.ArrangeSurface(show, pr)
}

// menuRect is where the dropdown goes: ABOVE the chip it belongs to,
// because the status bar is the last row of the page and a menu opening
// downwards would be entirely off screen.
func (s *addrStrip) menuRect() gooey.Rect {
	b := s.Bounds()
	w := 4
	for _, it := range s.items {
		if n := len([]rune(it.Text)) + 4; n > w {
			w = n
		}
	}
	h := len(s.items) + 2
	x := b.X
	if i := s.menuFor; i >= 0 && i < len(s.chips) {
		x = s.chips[i].Bounds().X
	}
	return gooey.Rect{X: x, Y: max(0, b.Y-h), W: w, H: h}
}

// Render paints nothing. The strip is a chrome-only container: its
// bounds enclose the chips' cells, and filling them would blank content
// whose own clean nodes will not repaint. That is the same rule
// StatusBar itself follows.
func (s *addrStrip) Render(*gooey.Frame) {}

// drawMenu paints the popup surface. It runs inside the SURFACE's paint
// node, so the selection read here makes moving the highlight repaint
// the menu alone.
func (s *addrStrip) drawMenu(f *gooey.Frame, b gooey.Rect) {
	if len(s.items) == 0 || b.W <= 2 || b.H <= 2 {
		return
	}
	sel := clampInt(s.menuSelP.Get(), 0, len(s.items)-1)
	st := render.Style{}
	components.DrawBoxRunes(f.Cells, b, st)
	inner := b.W - 2
	for i, it := range s.items {
		y := b.Y + 1 + i
		if y >= b.Y+b.H-1 {
			break
		}
		is := st
		// The CanExecute read happens while painting, so a condition
		// flipping repaints the open menu with no event anywhere.
		if !gooey.CanExecute(it.Action) {
			is.Dim = true
		}
		if i == sel {
			is.Reverse = true
		}
		f.Cells.SetString(b.X+1, y, padTo(ellipsize(" "+it.Text, inner), inner), is)
	}
}

// IsMenuOpen reports whether the context menu is showing.
func (s *addrStrip) IsMenuOpen() bool { return s.popup().IsOpen() }

// openMenu builds the items for a chip and shows the menu over it.
//
// The items are rebuilt per open rather than kept, because they close
// over the chip they act on and the chip set can change on a hot
// reload.
func (s *addrStrip) openMenu(i int, restore gooey.Component) {
	if i < 0 || i >= len(s.chips) {
		return
	}
	c := s.chips[i]
	s.menuFor = i
	s.items = []components.MenuItem{
		{
			Text:    "Copy address",
			Gesture: "enter",
			Action:  gooey.Command(func() { c.copyAddr() }),
		},
		{
			// Both endpoints at once, which is what a client config
			// usually wants and what the row used to show as one string.
			Text:   "Copy all endpoints",
			Action: gooey.Command(func() { s.copyText("all", s.allEndpoints(), "all copied") }),
		},
	}
	// THE COUNTS LIVE HERE AND ONLY HERE, as an inert row. A popup is
	// sized to its content, so a sentence costs no status-bar width —
	// which is what makes it the right home for the two numbers that
	// must not become colours: gRPC's live client count, and MCP's
	// CUMULATIVE request count, spelled "N calls so far" so that a
	// number which only ever goes up cannot be read as a live client.
	// See servelink.go.
	//
	// Read at OPEN time rather than held in a property: it is off the
	// paint path, so nothing here is sampled on a clock and nothing
	// here repaints anything.
	//
	// Action nil => CanExecute false => drawn dim and not activatable.
	if d := c.link.detail; d != nil {
		s.items = append(s.items, components.MenuItem{Text: c.label + ": " + d()})
	}
	// The full caveat sentence. The notice can only afford "copy
	// unverified"; this is where the reason fits.
	if cav := s.caveat(); cav != "" {
		s.items = append(s.items, components.MenuItem{Text: cav})
	}
	s.menuSelP.Set(0)
	s.setCurrent(i)
	s.popup().Open(restore)
}

// allEndpoints is every chip's "<label> <address>", joined the way the
// single Text used to join them.
func (s *addrStrip) allEndpoints() string {
	parts := make([]string, len(s.chips))
	for i, c := range s.chips {
		parts[i] = c.idleText()
	}
	return strings.Join(parts, strings.Repeat(" ", addrGap))
}

func (s *addrStrip) dismissMenu() { s.popup().Dismiss() }

func (s *addrStrip) activateItem(i int) {
	if i < 0 || i >= len(s.items) {
		return
	}
	a := s.items[i].Action
	s.dismissMenu()
	if gooey.CanExecute(a) {
		a.Run()
	}
}

// CopyCurrent copies the highlighted chip's address — the target of the
// enter key and of the CopyAddr command.
func (s *addrStrip) CopyCurrent() { s.CopyIndex(s.currentIndex()) }

// CopyIndex copies one chip by position. Out of range is a no-op rather
// than a panic: the global gestures are bound whether or not the
// endpoint they name came up.
func (s *addrStrip) CopyIndex(i int) {
	if i < 0 || i >= len(s.chips) {
		return
	}
	s.setCurrent(i)
	s.chips[i].copyAddr()
}

// CopyLabelled copies the endpoint whose label matches — "grpc", "mcp"
// — which is what the global gestures name. A missing endpoint is a
// no-op.
func (s *addrStrip) CopyLabelled(label string) {
	for i, c := range s.chips {
		if c.label == label {
			s.CopyIndex(i)
			return
		}
	}
}

// OpenMenuFor opens the context menu on the endpoint with this label,
// for the keyboard route. A key open passes nil restore: the strip is
// not necessarily focused, and Popup.Dismiss only restores while focus
// is still on the owner.
func (s *addrStrip) OpenMenuFor(label string) {
	for i, c := range s.chips {
		if c.label == label {
			s.openMenu(i, nil)
			return
		}
	}
}

// chipAt is which chip covers a cell.
func (s *addrStrip) chipAt(x, y int) (int, bool) {
	for i, c := range s.chips {
		b := c.Bounds()
		if x >= b.X && x < b.X+b.W && y >= b.Y && y < b.Y+b.H {
			return i, true
		}
	}
	return 0, false
}

// itemAt is which menu item covers a cell, inside the open surface.
func (s *addrStrip) itemAt(x, y int) (int, bool) {
	b := s.popup().SurfaceBounds()
	if x <= b.X || x >= b.X+b.W-1 {
		return 0, false
	}
	i := y - b.Y - 1
	if i < 0 || i >= len(s.items) || y >= b.Y+b.H-1 {
		return 0, false
	}
	return i, true
}

// HandleKey is the strip's keyboard half. It only ever sees keys while
// the focused chain passes through it, EXCEPT for the modal
// fall-through below: while the menu is open the strip holds focus, so
// the menu's own keys arrive here.
func (s *addrStrip) HandleKey(ev input.KeyEvent) bool {
	p := s.popup()
	if p.IsOpen() {
		switch ev {
		case input.Named(input.KeyUp):
			s.moveMenuSel(-1)
			return true
		case input.Named(input.KeyDown):
			s.moveMenuSel(1)
			return true
		case input.Named(input.KeyEnter):
			s.activateItem(clampInt(s.menuSelP.Get(), 0, len(s.items)-1))
			return true
		}
		// Esc dismisses; Modal swallows the rest so the page's bare "q"
		// and "x" cannot fire under an open menu.
		return p.HandleKey(ev)
	}
	switch ev {
	case input.Named(input.KeyLeft):
		s.setCurrent(s.currentIndex() - 1)
		return true
	case input.Named(input.KeyRight):
		s.setCurrent(s.currentIndex() + 1)
		return true
	case input.Named(input.KeyEnter), input.Rune(' '):
		s.CopyCurrent()
		return true
	case input.Rune('m'):
		// The context menu, strip-scoped. A bare letter is safe HERE
		// for the same reason the page's q/x/d are safe there: this
		// only ever runs while the strip is focused, and the strip has
		// no text entry. It costs nothing from the page-wide gesture
		// budget, which the dock and clipboard work have nearly spent.
		s.openMenu(s.currentIndex(), nil)
		return true
	}
	return false
}

func (s *addrStrip) moveMenuSel(d int) {
	if len(s.items) == 0 {
		return
	}
	n := len(s.items)
	s.menuSelP.Set(((clampInt(s.menuSelP.Get(), 0, n-1)+d)%n + n) % n)
}

// HandleMouse is the pointer half. The chips are leaves and handle
// nothing themselves, so every pointer event bubbles here.
//
// RIGHT-CLICK DECODES. The SGR reader puts the button in
// MouseEvent.Button for a press (input/mouse.go: MousePress carries
// MouseButton(cb&3), so cb=2 is ButtonRight), and DispatchMouse routes
// a press to the hit component and then bubbles it — the button field
// survives untouched. TestARightPressDecodesAndOpensTheMenu checks both
// halves, the decode from the real byte sequence and the routing.
func (s *addrStrip) HandleMouse(ev input.MouseEvent) bool {
	p := s.popup()
	if p.IsOpen() {
		if ev.Kind == input.MouseClick {
			if i, ok := s.itemAt(ev.X, ev.Y); ok {
				s.activateItem(i)
				return true
			}
		}
		// Everything else: an outside press dismisses and is consumed,
		// the residue of the dismissing gesture is swallowed.
		return p.HandleMouse(ev)
	}
	switch {
	case ev.Kind == input.MousePress && ev.Button == input.ButtonRight:
		if i, ok := s.chipAt(ev.X, ev.Y); ok {
			s.openMenu(i, p.MouseOpenRestore())
			return true
		}
	case ev.Kind == input.MouseClick && ev.Button == input.ButtonLeft:
		if i, ok := s.chipAt(ev.X, ev.Y); ok {
			s.CopyIndex(i)
			return true
		}
	}
	return false
}

// HandleMouseMove tracks the menu highlight under the pointer, the way
// a dropdown is expected to behave. It opts in explicitly because
// motion is high-frequency and is only delivered to components that
// ask.
func (s *addrStrip) HandleMouseMove(ev input.MouseEvent) bool {
	if !s.popup().IsOpen() {
		return false
	}
	if i, ok := s.itemAt(ev.X, ev.Y); ok {
		if i != s.menuSelP.Get() {
			s.menuSelP.Set(i)
		}
		return true
	}
	return false
}

// serveAddrsBuilder registers <ServeAddrs/> — the editor's own chrome,
// so it goes on ctx.Components and never on docCtx (a document that
// could build the editor's furniture is the recursion the two-context
// split exists to prevent).
//
// The endpoint list is read at BUILD time, and that is correct rather
// than a shortcut: the servers are started once in main() before the
// page is first built, and no endpoint is ever added or removed while
// the app runs. A hot reload rebuilds against the same list.
//
// With no endpoints at all it falls back to the plain bound Text the
// section used to be, so the "no control plane: started with -serve ..."
// message the user needs when they disabled both flags is unchanged.
func serveAddrsBuilder(ed *editor) markup.Builder {
	return func(e markup.Element, ctx *markup.Context) (gooey.Component, error) {
		if len(ed.serving) == 0 {
			return components.StatusText(ed.serveInfo), nil
		}
		// ed.link, so the connection state survives this build: a hot
		// reload makes new chips against servers that never restarted.
		s := newAddrStrip(ed.serving, ed.link)
		s.copyFn = ed.copyToSystem
		s.caveatFn = term.ClipboardCaveat
		ed.addrs = s
		return s, nil
	}
}

// copyEndpoint is what the page's global KEYBINDINGS call. They are the mandatory keyboard half of a
// pointer affordance: mouse reports cannot be injected through a
// recording pty at all, so a click-only feature is one that can never
// be captured or verified.
//
//	ctrl+g  copy the gRPC address   (g for grpc)
//	ctrl+t  copy the MCP URL        (t for tools — MCP is the tool
//	                                 endpoint; see below for why not u)
//
// The menu has NO global gesture. It opens with a bare `m` while the
// strip is FOCUSED (see HandleKey), which is the conventional place for
// a context menu and spends nothing from the page-wide gesture budget.
// Right-click opens it too, but a pty cannot inject mouse reports, so
// `m` is the route a capture can exercise.
//
// THE BUDGET IS SHARED AND ALREADY TIGHT. ctrl+u and ctrl+e were the
// first choices here and BOTH WERE WRONG — the dock work claims ctrl+u
// (hide unpinned) and ctrl+e (open $EDITOR), on top of the page's
// existing q, ctrl+c, x, ctrl+n, ctrl+p, esc, d and the clipboard's y,
// ctrl+x, p. Nothing in the framework reports a duplicate gesture: two
// KeyBindings on one gesture resolve to whichever the dispatcher
// reaches first and the loser fires NEVER, silently.
// TestTheGesturesAreNotAlreadyClaimed is the only guard, and it can
// only see this page — a collision with another page is unguarded.
//
// Three decode traps ruled out the obvious alternatives, all confirmed
// against input/decode.go and input/gesture.go:
//
//   - F-KEYS DO NOT DECODE AT ALL. There are no F1-F12 entries in
//     finalKey/tildeKey, so decodeCSI returns false and the sequence is
//     swallowed. An F-key binding is dead with no error anywhere.
//   - ctrl+shift+<letter> COLLAPSES to ctrl+<letter>. gesture.go folds
//     shift+printable to the uppercase rune and clears ModShift, then
//     ctrl lowercases it — so ctrl+shift+g parses to exactly the same
//     KeyEvent as ctrl+g.
//   - ctrl+m is byte 0x0d, indistinguishable from Enter, and
//     ctrl+i/j/h/[ are likewise taken as tab/enter/backspace/esc before
//     the control-byte arm is reached.
//
// They live on the ROOT of the page, which is not a style choice — a
// KeyBinding only fires while the focused chain passes through its
// host, so a binding anywhere further in would be silent whenever focus
// sat elsewhere.
//
// A nil strip is a no-op: the bindings exist whether or not any
// endpoint came up.
func (ed *editor) copyEndpoint(label string) {
	if ed.addrs == nil {
		return
	}
	ed.addrs.CopyLabelled(label)
}

// ellipsize, NOT clipTo, and the rename is a bug that did not happen.
//
// dock.go has a clipTo of its own that HARD-TRUNCATES. This one guards
// w <= 0 and ends the result with "…". They collided as a package-level
// redeclaration when the panes were stacked, and the landing plan said to
// delete this one — which would have compiled, passed, and silently
// turned every ellipsis in this strip into a hard cut.
//
// `go vet` reports the SYMBOL, which is the one thing the two bodies
// agree on, so the collision report cannot say which answer is right.
// Reading both bodies is the only thing that can.
func ellipsize(s string, w int) string {
	if w <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= w {
		return s
	}
	if w == 1 {
		return "…"
	}
	return string(r[:w-1]) + "…"
}

func padTo(s string, w int) string {
	if n := w - len([]rune(s)); n > 0 {
		return s + strings.Repeat(" ", n)
	}
	return s
}

package components

import (
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
	"github.com/WonderForgeLabs/gooey/validate"
)

// The ordering between the framework's THREE page-wide overlay hosts,
// which nothing in the suite asked about until #439.
//
// ToastHost, AdornmentLayer and popupSurface all documented themselves as
// "declare it last, document order is z-order", and for the two hosts
// that is still all they had after #437 gave popupSurface the overlay
// layer. A lifted popup therefore sat above both of them, reversing
// three written claims at once: toast.go's "puts every toast above the
// page", markup-reference.md's "tooltips paint above toasts too", and
// menu_live_test.go's "the toast layer is topmost".
//
// Every assertion below reads a ROW rather than a damage count on
// purpose — the question here is only who painted last over one strip of
// cells, and the damage numbers these shapes produce are already pinned
// by toast_test.go and tooltip_test.go. render.RowText, not the row()
// helper: a continuation marker must not read back as a rune.

// toastOverPopupPage puts a toast on the popup's own cells. The popup
// drops at {X: 0, Y: 1, W: 6, H: 2} (toyOwner.Arrange), so confining the
// host to a 7-cell box on row 1 lands its banner at exactly X=0 — the
// two write the same cells, which is the only arrangement in which one
// can be read off the other.
func toastOverPopupPage() (*toyOwner, *ToastHost, gooey.Component) {
	owner := &toyOwner{}
	host := &ToastHost{}
	page := &Canvas{Children: []gooey.Component{
		owner,
		gooey.L(host, gooey.Layout{Top: 1, Width: 7}),
	}}
	return owner, host, page
}

func TestAToastPaintsAboveAnOpenPopup(t *testing.T) {
	owner, host, page := toastOverPopupPage()
	c := gooey.NewComposer(page, 20, 5)
	c.Focus().SetFocus(owner)
	c.Frame()

	if !c.HandleKey(input.Named(input.KeyEnter)) {
		t.Fatal("enter on the focused owner was not consumed")
	}
	c.Frame()
	// Without this the test could pass against a popup that never opened.
	if got := render.RowText(c.Cells(), 1); !strings.HasPrefix(got, "POPUP!") {
		t.Fatalf("row 1 = %q right after opening, want the popup", got)
	}

	host.Show("TOAST")
	c.Frame()
	if got := render.RowText(c.Cells(), 1); !strings.HasPrefix(got, " TOAST ") {
		t.Errorf("row 1 = %q with a toast over an open popup, want the toast "+
			"(%q). A notification the user must see is under the dropdown "+
			"that happens to be open — ToastHost is an ordinary-layer node "+
			"and every lifted overlay outranks it", got, " TOAST ")
	}
}

// A toast must still UNCOVER the popup when it goes down: the
// counterweight that stops the fix from being "let nothing paint over a
// toast's rect", which would strand its cells on screen.
//
// This one carries a DAMAGE COUNT and the others do not, which is the
// line CLAUDE.md draws: the tests above make an ORDERING claim, and a
// count says nothing about who is on top. This one makes a REPAINT
// claim — that restoreUnder force-dirtied the node beneath the vacated
// rect and the forward pass laid it back down — and for that a row read
// passes just as well when the entire tree repainted.
//
// It is also a shape no existing count covers, which is why it cannot
// borrow one from toast_test.go: this is a restore INSIDE the overlay
// layer, one lifted node vacating over another, and that path did not
// exist before this change. Raised in review of #444.
func TestADismissedToastUncoversTheOpenPopup(t *testing.T) {
	owner, host, page := toastOverPopupPage()
	c := gooey.NewComposer(page, 20, 5)
	c.Focus().SetFocus(owner)
	c.Frame()
	if !c.HandleKey(input.Named(input.KeyEnter)) {
		t.Fatal("enter on the focused owner was not consumed")
	}
	c.Frame()

	toast := host.Show("TOAST")
	c.Frame()
	host.Dismiss(toast)
	_, painted := c.Frame()

	// ONE: the popup surface, forced by the restore sweep over the cells
	// the toast gave up. Not two — the host owns no cells
	// (PassesCellsThrough) and the toast has already left the
	// composition, so neither is a candidate. Not the node count: a fix
	// that bought the ordering by repainting the tree every frame
	// satisfies every RowText assertion in this file and reports four
	// here.
	if painted != 1 {
		t.Errorf("dismissing the toast repainted %d components, want 1 (the "+
			"popup surface the restore pass forces back down). More means "+
			"the vacated cells were bought with a repaint wider than the "+
			"rect required", painted)
	}
	if !owner.popup().IsOpen() {
		t.Fatal("the popup closed on its own")
	}
	if got := render.RowText(c.Cells(), 1); !strings.HasPrefix(got, "POPUP!") {
		t.Errorf("row 1 after the toast was dismissed = %q; the popup "+
			"underneath did not come back", got)
	}
	if _, settled := c.Frame(); settled != 0 {
		t.Errorf("the frame after the dismissal painted %d components, want 0", settled)
	}
}

// markerOverPopupPage is the same question for the adornment layer,
// which Tooltip, ValidationMarker and DragGhost all reach through.
//
// A ValidationMarker rather than a Tooltip, and that is forced rather
// than chosen: Popup.Open calls mgr.CaptureMouse unconditionally — not
// only when Modal — so the hover that raised a tip is out the moment any
// dropdown opens, and Tooltip.IsShown goes false. The overlap this test
// needs cannot be built out of a tooltip at all. A marker is the
// PERSISTENT customer — up for as long
// as its field is invalid, no pointer anywhere in it — which is also
// the case the bug hurts most: a form telling the user what is wrong,
// erased by the menu they opened to fix it.
//
// The field is empty and Required, so the marker is up from the first
// frame and floats its message on the row BELOW its anchor.
//
// Both the field and the popup's owner take the Canvas's default 0,0,
// and that is load-bearing twice over rather than incidental. The field
// on row 0 is what puts its marker on row 1; the owner on row 0 is what
// puts its dropdown on rows 1-2 (toyOwner.Arrange). Row 1 is therefore
// the one strip of cells both write, which is the only arrangement in
// which one can be read off the other.
//
// The cost of that is a row 0 with two components stacked on it — the
// owner's rule of '-' and the field over it. Nothing asserts on row 0
// and nothing needs to; it is noted because a reader debugging a
// failure here should not have to rediscover it.
func markerOverPopupPage() (*toyOwner, *ValidationMarker, gooey.Component) {
	owner := &toyOwner{}
	name := prop.NewSource("")
	tb := &TextBox{Text: name, Error: validate.Field(name, validate.Required(""))}
	m := &ValidationMarker{}
	tb.Attach(m)
	page := &Canvas{Children: []gooey.Component{
		owner,
		tb,
		&AdornmentLayer{},
	}}
	return owner, m, page
}

func TestAValidationMarkerPaintsAboveAnOpenPopup(t *testing.T) {
	owner, m, page := markerOverPopupPage()
	c := gooey.NewComposer(page, 20, 5)
	c.Focus().SetFocus(owner)
	c.Frame()
	if !m.IsShown() {
		t.Fatal("an empty required field should show its marker from the first frame")
	}
	if got := render.RowText(c.Cells(), 1); !strings.Contains(got, " required ") {
		t.Fatalf("row 1 = %q before the popup opened, want the marker's message", got)
	}

	c.HandleKey(input.Named(input.KeyEnter))
	c.Frame()
	if !owner.popup().IsOpen() {
		t.Fatal("enter on the focused owner did not open the popup")
	}
	// The popup owns columns 0-5 of the same row. Whole message or
	// nothing: under the bug its first cells are the ones overwritten.
	if got := render.RowText(c.Cells(), 1); !strings.Contains(got, " required ") {
		t.Errorf("row 1 = %q with a marker under an open popup, want the "+
			"message intact. AdornmentLayer is an ordinary-layer node, so "+
			"every lifted overlay paints above the validation markers, "+
			"tooltips and drag ghosts it hosts", got)
	}
}

// The order WITHIN the overlay layer, which is declaration order and
// nothing else. docs/markup-reference.md tells apps to declare the
// AdornmentLayer after the ToastHost so "tooltips paint above toasts
// too"; once both are overlays that promise rests entirely on the two
// staying in that order in c.paint.
//
// This one is green before the fix as well as after — it is here so that
// giving the hosts a layer cannot silently swap them.
func TestATooltipPaintsAboveAToast(t *testing.T) {
	tipHost := &Text{Content: Str("save")}
	tipHost.LayoutProps().Left, tipHost.LayoutProps().Top = 2, 0
	tip := &Tooltip{Text: Str("TIP")}
	tipHost.Attach(tip)
	host := &ToastHost{}
	page := &Canvas{Children: []gooey.Component{
		tipHost,
		// Row 1 is where the tip lands; a 20-wide box puts the toast's
		// banner across it rather than off in the corner.
		gooey.L(host, gooey.Layout{Top: 1, Width: 20}),
		&AdornmentLayer{},
	}}

	c := gooey.NewComposer(page, 20, 5)
	c.Frame()
	host.Show("TOASTTOASTTOASTTO")
	c.Frame()
	hoverAt(c, 3, 0)
	c.Frame()
	if !tip.IsShown() {
		t.Fatal("the tooltip is not shown")
	}
	if got := render.RowText(c.Cells(), 1); !strings.Contains(got, " TIP ") {
		t.Errorf("row 1 = %q, want the tooltip visible over the toast. "+
			"The layer is declared after the host, which is the whole of "+
			"what puts tooltips above toasts", got)
	}
}

// hitAdornment is an adornment that is NOT HitTestTransparent, which no
// adornment the framework ships is — tipPopup, markerPopup and DragGhost
// are all decoration and all opt out. That uniformity is exactly why the
// claim below had nothing pinning it: with every shipped adorner invisible
// to the pointer, no existing test can tell "hit order agrees with paint
// order" apart from "the press went somewhere else entirely".
//
// Two comments in adorn.go argue at length that on the documented shape
// the two agree and an interactive adorner works, and that off it they
// diverge silently. That is a written claim about z-order with nothing
// going red when it reverses — the shape of defect this whole change
// exists to remove. Raised in review of #444.
type hitAdornment struct {
	gooey.Base
	anchor gooey.Component
}

func (a *hitAdornment) Anchor() gooey.Component { return a.anchor }

// Place puts it on the row BELOW its anchor, the way a tooltip or a
// validation marker floats. That keeps the anchor itself off the contested
// cells, so the only two candidates there are this adornment and the
// ordinary sibling — which is what makes the hit a statement about z-order
// rather than about which component happens to be on top of the anchor.
func (a *hitAdornment) Place(against, _ gooey.Rect) gooey.Rect {
	return gooey.Rect{X: against.X, Y: against.Y + 1, W: 6, H: 1}
}

func (a *hitAdornment) Measure(gooey.Size) gooey.Size { return gooey.Size{W: 6, H: 1} }

func (a *hitAdornment) Render(f *gooey.Frame) {
	b := a.Bounds()
	if b.W > 0 && b.H > 0 {
		f.Cells.SetString(b.X, b.Y, clipCols("ADORN!", b.W), render.Style{})
	}
}

// hitPage builds the same page twice over, differing only in whether the
// AdornmentLayer is the root's LAST child. `over` is an ordinary Text on
// the same cells as the adornment, and the question both tests ask is
// which of the two the pointer finds there.
func hitPage(layerLast bool) (*Text, *hitAdornment, gooey.Component) {
	// The anchor sits on row 0 and the contested cells are on row 1, so the
	// anchor itself is never a candidate for the hit — otherwise it, not the
	// sibling, is what the pointer finds, and the negative assertion below
	// would pass for a reason that has nothing to do with z-order.
	anchor := &Text{Content: Str("anchor")}
	over := gooey.L(&Text{Content: Str("SIBLING")}, gooey.Layout{Top: 1}).(*Text)
	layer := &AdornmentLayer{}
	a := &hitAdornment{anchor: anchor}
	layer.Add(a)

	kids := []gooey.Component{anchor, over, layer}
	if !layerLast {
		kids = []gooey.Component{anchor, layer, over}
	}
	return over, a, &Canvas{Children: kids}
}

// The documented shape: layer LAST, so hitTest — which walks each
// container's children from last to first — descends it first and finds
// the adornment before the ordinary sibling covering the same cells. Hit
// order agrees with the lifted paint order, which is what makes an
// interactive adorner work on the shape the docs mandate.
func TestAnAdornmentIsHitFirstWhenTheLayerIsLast(t *testing.T) {
	_, a, page := hitPage(true)
	c := gooey.NewComposer(page, 20, 3)
	c.Frame()

	b := a.Bounds()
	if b.W == 0 || b.H == 0 {
		t.Fatal("the adornment was never placed, so this test hit-tests nothing")
	}
	if hit := c.Focus().HitTest(b.X, b.Y); hit != gooey.Component(a) {
		t.Errorf("HitTest over the adornment found %T, want the adornment. With "+
			"the layer declared last, hit order and the overlay paint order "+
			"agree — which is what makes an interactive adorner work on the "+
			"shape the docs mandate", hit)
	}
}

// And the divergence the same comments warn about, worth pinning precisely
// because it is the half a reader is asked to take on trust: move the layer
// off the end and the adornment still PAINTS above (it is lifted
// regardless) while the later sibling takes the press.
func TestAnAdornmentLosesThePressWhenTheLayerIsNotLast(t *testing.T) {
	over, a, page := hitPage(false)
	c := gooey.NewComposer(page, 20, 3)
	c.Frame()

	b := a.Bounds()
	if got := render.RowText(c.Cells(), b.Y); !strings.HasPrefix(got, "ADORN!") {
		t.Fatalf("row %d = %q; the adornment should paint above the sibling "+
			"wherever the layer sits — that is what the marker does", b.Y, got)
	}
	// NAMES THE WINNER rather than asserting `!= a`. A negative assertion
	// here passes whenever anything at all other than the adornment is hit,
	// including when the walk finds some third component for reasons that
	// have nothing to do with z-order — which is how the first draft of this
	// test stayed green under a mutation that reversed the walk direction.
	if hit := c.Focus().HitTest(b.X, b.Y); hit != gooey.Component(over) {
		t.Errorf("HitTest over the adornment found %T, want the later sibling. "+
			"With the layer declared BEFORE something that overlaps it, the "+
			"adornment paints on top and the sibling takes the press — the "+
			"divergence AdornmentLayer.Add and ToastHost.OverlaysPage warn "+
			"about. If the adornment now wins, the marker has started moving "+
			"input as well as paint: good news, and those comments need "+
			"rewriting rather than this test deleting", hit)
	}
}

// The ToastHost twin of the two adornment hit-tests above, and it is not
// redundant with them even though the mechanism is identical.
//
// ToastHost.OverlaysPage claims a DIFFERENT consequence from
// AdornmentLayer's: not "an interactive adorner loses its press", but "the
// covered Button takes the hover and highlights under a toast the user can
// plainly see over it". A Toast is a real hit target — it declares no
// HitTestTransparent, and TestAShownToastStillCatchesThePointer pins that —
// where every adornment the framework ships is decoration. Only one of the
// two claims was a fact about the code; this makes the other one one too.
// Raised in review of #444.
func toastHitPage(hostLast bool) (*Button, *ToastHost, gooey.Component) {
	// A Button rather than a Text for the sibling, because the claim is
	// about a real hit target taking a hover it should not have.
	btn := gooey.L(&Button{Content: Str("BUTTON")}, gooey.Layout{Top: 0}).(*Button)
	host := gooey.L(&ToastHost{}, gooey.Layout{Top: 0, Width: 8}).(*ToastHost)

	kids := []gooey.Component{btn, host}
	if !hostLast {
		kids = []gooey.Component{host, btn}
	}
	return btn, host, &Canvas{Children: kids}
}

// Host LAST — the documented shape. hitTest descends it first, so the
// toast takes the press over its own cells and hit order agrees with the
// lifted paint order.
func TestAToastIsHitFirstWhenTheHostIsLast(t *testing.T) {
	_, host, page := toastHitPage(true)
	c := gooey.NewComposer(page, 20, 3)
	c.Frame()
	toast := host.Show("TOAST")
	c.Frame()

	b := toast.Bounds()
	if b.W == 0 || b.H == 0 {
		t.Fatal("the toast was never arranged, so this test hit-tests nothing")
	}
	if hit := c.Focus().HitTest(b.X, b.Y); hit != gooey.Component(toast) {
		t.Errorf("HitTest over the toast found %T, want the toast", hit)
	}
}

// And the divergence, which is the half ToastHost.OverlaysPage warns about
// in its own words: the toast paints on top either way, but with the host
// declared BEFORE an overlapping sibling the Button underneath takes the
// pointer — visibly covered, and still the thing that highlights.
func TestAButtonUnderAToastTakesTheHoverWhenTheHostIsNotLast(t *testing.T) {
	btn, host, page := toastHitPage(false)
	c := gooey.NewComposer(page, 20, 3)
	c.Frame()
	toast := host.Show("TOAST")
	c.Frame()

	b := toast.Bounds()
	// "TOAST", not "T", and the difference is the whole precondition. The
	// sibling renders "[ BUTTON ]", which CONTAINS "T" — so the first
	// version of this line passed off the Button's own text while the toast
	// was entirely covered, and the test stayed green under the very
	// mutation it exists to catch (removing ToastHost.OverlaysPage).
	// Verified by re-running that mutation. It is the failure recorded two
	// tests above, arriving through the FIXTURE rather than the assertion's
	// shape. Raised in review of #444.
	if got := render.RowText(c.Cells(), b.Y); !strings.Contains(got, "TOAST") {
		t.Fatalf("row %d = %q; the toast should paint above the button "+
			"wherever the host sits — that is what the marker does", b.Y, got)
	}
	// Names the winner rather than asserting != toast, for the reason the
	// adornment twin records: a negative assertion passes whenever anything
	// else wins, including for reasons unrelated to z-order.
	if hit := c.Focus().HitTest(b.X, b.Y); hit != gooey.Component(btn) {
		t.Errorf("HitTest over the toast found %T, want the Button underneath. "+
			"With the host declared before an overlapping sibling, the toast "+
			"paints on top and the button still takes the pointer — the "+
			"divergence ToastHost.OverlaysPage warns about, and the reason "+
			"the host must be declared last. If the toast now wins, the "+
			"marker has started moving input as well as paint and that "+
			"comment needs rewriting rather than this test deleting", hit)
	}
}

// A ValidationMarker places its lifted adornment inside a FULLY FROZEN
// subtree, with no gesture anywhere — the pin for a claim the wysiwyg
// preview overlay's doc comment makes about its own safety.
//
// That comment used to give "design mode is Frozen" as one of three
// reasons the previewed tree places nothing in the page's overlay
// hosts. It is not a reason. Frozen bounds DISPATCH and Startables;
// placement here runs from SetFocusManager through attachAdornment, on
// the input-tree walk, and the walk still reaches a frozen subtree — it
// must, because a frozen component is still a focus candidate the
// manager has to EVICT rather than never see.
//
// So a previewed document holding a Required field and a marker floats
// an adornment on its FIRST FRAME: frozen, unclicked. Nothing in the
// suite said so, the comment was the only statement of it, and it said
// the opposite. Caught in review of #444.
func TestAValidationMarkerPlacesItsAdornmentWhileFrozen(t *testing.T) {
	name := prop.NewSource("")
	errP := validate.Field(name, validate.Required("required"))
	tb := &TextBox{Text: name, Error: errP}
	m := &ValidationMarker{}
	tb.Attach(m)
	// A plain <Frozen> is AllowNone — the strongest freeze there is.
	root := &VStack{Children: []gooey.Component{
		&Frozen{Child: tb},
		&AdornmentLayer{},
	}}
	c := gooey.NewComposer(root, 30, 5)
	c.Frame()
	// Discriminating half: without this the test passes just as well
	// with no Frozen in the tree at all, and its name would be a claim
	// about a wrapper that was doing nothing.
	if c.Focus().SetFocus(tb) {
		t.Fatal("the TextBox took focus, so the subtree is not frozen and " +
			"this test proves nothing about Frozen")
	}
	if !m.IsShown() {
		t.Fatal("the marker did not place while frozen — if Frozen has " +
			"grown a gate on the input-tree walk that is a real change, " +
			"and preview/overlay.go's comment can drop the correction " +
			"this test exists to hold")
	}
}

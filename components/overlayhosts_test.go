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
// than chosen: Popup.Open takes pointer capture unconditionally
// (Popup.Open calls mgr.CaptureMouse unconditionally — not only when
// Modal), so the hover that
// raised a tip is out the moment any dropdown opens and Tooltip.IsShown
// goes false. The overlap this test needs cannot be built out of a
// tooltip at all. A marker is the PERSISTENT customer — up for as long
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

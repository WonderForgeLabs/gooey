package components

import (
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/validate"
)

// The TextBox's own error state is a paint dependency like any other:
// flipping the Error property repaints exactly the TextBox, and the
// text wears the invalid visual (red + underline by default).
func TestTextBoxErrorFlipRepaintsTheTextBoxAlone(t *testing.T) {
	text := prop.NewSource("bob")
	errP := prop.NewSource("")
	tb := &TextBox{Text: text, Error: errP}
	other := &Text{Content: Str("label")}
	root := &VStack{Children: []gooey.Component{tb, other}}
	c := gooey.NewComposer(root, 20, 3)
	c.Frame()
	if c.Cells().At(0, 0).Style.Underline {
		t.Fatal("a valid field painted underlined")
	}

	errP.Set("required")
	_, painted := c.Frame()
	if painted != 1 {
		t.Fatalf("error flip painted %d components, want exactly the TextBox", painted)
	}
	cell := c.Cells().At(0, 0)
	if !cell.Style.Underline || cell.Style.Fg != errorRed {
		t.Fatalf("invalid text style = %+v, want the red underline convention", cell.Style)
	}

	errP.Set("")
	if _, painted := c.Frame(); painted != 1 {
		t.Fatalf("clearing the error painted %d components, want 1", painted)
	}
	if c.Cells().At(0, 0).Style.Underline {
		t.Fatal("the invalid visual survived a cleared error")
	}
	if _, painted := c.Frame(); painted != 0 {
		t.Fatalf("settled frame painted %d, want 0", painted)
	}
}

// A form page in the shape an app declares it: the field, a filler row
// beneath (where the marker floats), a gated button, the layer last.
func formPage(w int) (name *prop.Property[string], tb *TextBox, m *ValidationMarker, btn *Button, page *Canvas) {
	name = prop.NewSource("")
	errP := validate.Field(name, validate.Required(""), validate.Len(3, 0, ""))
	tb = &TextBox{Text: name, Error: errP}
	tb.LayoutProps().Left, tb.LayoutProps().Top = 0, 0
	m = &ValidationMarker{}
	tb.Attach(m)
	filler := &Text{Content: Str(strings.Repeat("#", w))}
	filler.LayoutProps().Top = 1
	btn = &Button{Content: Str("save"), Click: gooey.NewCommand(func() {}).When(validate.All(errP))}
	btn.LayoutProps().Left, btn.LayoutProps().Top = 0, 2
	layer := &AdornmentLayer{}
	page = &Canvas{Children: []gooey.Component{tb, filler, btn, layer}}
	return
}

func typeRune(c *gooey.Composer, r rune) {
	c.HandleKey(input.KeyEvent{Key: input.KeyRune, Rune: r})
}

// The whole loop, pinned by damage counts: a keystroke that leaves
// validity where it was repaints exactly the TextBox and the marker —
// the gated button is untouched, because validate.All only propagates
// an actual flip — and the flip itself reaches the button exactly once.
func TestValidationLoopDamage(t *testing.T) {
	name, tb, m, _, page := formPage(30)
	c := gooey.NewComposer(page, 30, 4)
	c.Frame()
	if !m.IsShown() {
		t.Fatal("an empty required field should show its marker from the first frame")
	}
	if got := row(c.Cells(), 1); !strings.Contains(got, " required ") {
		t.Fatalf("row 1 = %q, want the floating message under the field", got)
	}
	if got := row(c.Cells(), 2); !strings.Contains(got, "[ save ]") {
		t.Fatalf("row 2 = %q, want the button", got)
	}
	if !c.Cells().At(1, 2).Style.Dim {
		t.Fatal("the gated button should paint dim while the form is invalid")
	}
	c.Focus().SetFocus(tb)
	c.Frame() // absorb the focus repaint

	// "a": required → at least 3 characters. Still invalid: the button
	// must not repaint. The message RESIZED, so beyond TextBox + marker
	// the frame restores beneath the float's old rect — the restored
	// filler leaf; transparent containers own no cells, the same
	// moved-overlay cost the tooltip dismissal pins.
	typeRune(c, 'a')
	_, painted := c.Frame()
	if painted != 3 {
		t.Fatalf("invalid→invalid resize painted %d components, want 3 (TextBox + marker + restored filler, and NO button)", painted)
	}
	if got := row(c.Cells(), 1); !strings.Contains(got, " at least 3 characters ") {
		t.Fatalf("row 1 = %q, want the Len message", got)
	}

	// "b": the message itself is unchanged, so no resize: exactly the
	// TextBox and the marker repaint — the gated button is untouched,
	// which is the stabilization contract made visible.
	typeRune(c, 'b')
	if _, painted := c.Frame(); painted != 2 {
		t.Fatalf("same-message edit painted %d components, want 2 (TextBox + marker)", painted)
	}

	// "c": the flip. The marker vacates its row (restored filler), and
	// the button repaints enabled — exactly
	// once, this frame.
	typeRune(c, 'c')
	if _, painted := c.Frame(); painted != 4 {
		t.Fatalf("the valid flip painted %d components, want 4 (TextBox + vacated marker + restored filler + the button, once)", painted)
	}
	if got := row(c.Cells(), 1); !strings.Contains(got, "####") || strings.Contains(got, "at least") {
		t.Fatalf("row 1 = %q, want the filler restored where the message floated", got)
	}
	if c.Cells().At(1, 2).Style.Dim {
		t.Fatal("the button is still dim after the form went valid")
	}
	if m.IsShown() {
		t.Fatal("marker still shown with no error")
	}
	if _, painted := c.Frame(); painted != 0 {
		t.Fatalf("settled frame painted %d, want 0", painted)
	}

	// Typing on: still valid, button untouched. The zero-size marker's
	// node still evaluates (its Render reads the error — subscribed is
	// subscribed) but owns no cells; the button stays clean.
	typeRune(c, 'd')
	if _, painted := c.Frame(); painted != 2 {
		t.Fatalf("valid→valid edit painted %d components, want 2 (TextBox + the empty marker's no-op node)", painted)
	}

	// And back across the threshold the other way.
	name.Set("")
	c.Frame()
	if !c.Cells().At(1, 2).Style.Dim {
		t.Fatal("the button did not re-disable when the form went invalid")
	}
	if got := row(c.Cells(), 1); !strings.Contains(got, " required ") {
		t.Fatalf("row 1 = %q, want the message back", got)
	}
}

// The marker is a PERSISTENT adornment: hiding the field hides the
// message (zero rect, cells restored) but does not drop it — no
// re-adding gesture exists — and showing the field brings it back
// through plain layout, no structural walk required.
func TestMarkerPersistsThroughHiddenAnchor(t *testing.T) {
	_, tb, m, _, page := formPage(30)
	c := gooey.NewComposer(page, 30, 4)
	c.Frame()
	if !m.IsShown() {
		t.Fatal("marker should be up")
	}

	gooey.LayoutOf(tb).Visibility = gooey.Hidden
	c.Frame()
	if got := row(c.Cells(), 1); !strings.Contains(got, "####") {
		t.Fatalf("row 1 = %q, want the filler restored while the field is hidden", got)
	}
	if m.pop == nil {
		t.Fatal("hiding the anchor DROPPED the persistent marker; it must only hide it")
	}

	gooey.LayoutOf(tb).Visibility = gooey.Visible
	c.Frame()
	if got := row(c.Cells(), 1); !strings.Contains(got, " required ") {
		t.Fatalf("row 1 = %q, want the message back with its anchor", got)
	}
	if _, painted := c.Frame(); painted != 0 {
		t.Fatalf("settled frame painted %d, want 0", painted)
	}
}

// An anchor that truly leaves the tree still takes the marker down (the
// layer's orphan sweep), and the next structural walk places a fresh
// popup when the host returns — the attachment seam, not a gesture, is
// what re-raises a persistent adornment.
func TestMarkerOrphanedWhenHostLeavesAndReturns(t *testing.T) {
	_, _, m, _, page := formPage(30)
	c := gooey.NewComposer(page, 30, 4)
	c.Frame()

	kids := page.Children
	page.Children = append([]gooey.Component{}, kids[1:]...) // drop the TextBox
	c.InvalidateStructure()
	c.Frame()
	if m.pop != nil {
		t.Fatal("host left the tree and the marker popup was not orphaned")
	}
	if got := row(c.Cells(), 1); strings.Contains(got, "required") {
		t.Fatalf("row 1 = %q, message must vanish with its host", got)
	}

	page.Children = kids // the host returns
	c.InvalidateStructure()
	c.Frame() // the re-sync walk re-places the popup…
	if m.pop == nil {
		t.Fatal("host returned and the re-sync walk did not re-place the marker")
	}
	c.Frame() // …and the Add's structural flag realizes it next frame
	if got := row(c.Cells(), 1); !strings.Contains(got, " required ") {
		t.Fatalf("row 1 = %q, want the message back", got)
	}
}

// A page without an AdornmentLayer degrades to inline-only error
// display: the marker simply never floats, nothing breaks.
func TestMarkerWithoutLayerShowsNothing(t *testing.T) {
	name := prop.NewSource("")
	errP := validate.Field(name, validate.Required(""))
	tb := &TextBox{Text: name, Error: errP}
	m := &ValidationMarker{}
	tb.Attach(m)
	root := &VStack{Children: []gooey.Component{tb, &Text{Content: Str("below")}}}
	c := gooey.NewComposer(root, 20, 3)
	c.Frame()
	if m.IsShown() {
		t.Fatal("no layer on the page; the marker cannot be shown")
	}
	if got := row(c.Cells(), 1); !strings.Contains(got, "below") {
		t.Fatalf("row 1 = %q, want the layout untouched", got)
	}
}

// A marker page with NO TextBox: the host is an inert Text and the
// Error is the marker's own handle, so the damage counts below are the
// marker's alone — no field repainting alongside it, no validator, no
// gated button.
func markerPage(w int) (*prop.Property[string], *ValidationMarker, *Canvas) {
	errP := prop.NewSource("")
	host := &Text{Content: Str("name")}
	host.LayoutProps().Left, host.LayoutProps().Top = 0, 0
	m := &ValidationMarker{Error: errP}
	host.Attach(m)
	filler := &Text{Content: Str(strings.Repeat("#", w))}
	filler.LayoutProps().Top = 1
	layer := &AdornmentLayer{}
	return errP, m, &Canvas{Children: []gooey.Component{host, filler, layer}}
}

// THE pin for the read-before-early-return discipline in
// markerPopup.Render. While the error is empty the popup is arranged to
// a zero rect and its Render returns having painted nothing — and the
// ONLY reason the very first failing edit ever reaches the screen is
// that the read happened above that return, on every one of those
// no-op frames. Move the read below the guard (or into a banner helper
// that Gets after its own bounds check) and this frame paints 0: no
// error, no panic, a marker that is simply deaf forever.
func TestMarkerEmptyToMessageSchedulesItsOwnFrame(t *testing.T) {
	errP, m, page := markerPage(30)
	c := gooey.NewComposer(page, 30, 4)
	c.Frame()
	if m.IsShown() {
		t.Fatal("an empty error should show no message")
	}
	if _, painted := c.Frame(); painted != 0 {
		t.Fatalf("settled frame painted %d, want 0", painted)
	}

	// The invalidation is the half a damage count cannot see: c.Frame()
	// composes whether or not anything asked for it, so "it repaints" is
	// satisfied by the layout sweep noticing the rect grew. Only
	// OnInvalidate can distinguish a marker that was SUBSCRIBED from one
	// whose appear was rescued by the bounds sweep — in a real App.Run
	// the unsubscribed one never gets a frame composed at all.
	scheduled := 0
	c.OnInvalidate(func() { scheduled++ })
	errP.Set("required")
	if scheduled == 0 {
		t.Fatal("the first error scheduled no frame — the marker's subscription carrier is broken")
	}

	_, painted := c.Frame()
	// Exactly ONE: appearing is zero rect → a rect, which is paint damage
	// on the marker's own node and nothing else. The filler underneath is
	// covered, not vacated, so it stays clean — the same appear cost the
	// tooltip and the toast pin. (The 5 in TestValidationLoopDamage is a
	// RESIZE, where cells are also given back.)
	if painted != 1 {
		t.Fatalf("empty→message painted %d components, want 1 (the marker alone)", painted)
	}
	if !m.IsShown() {
		t.Fatal("the marker is not shown after the error appeared")
	}
	if got := row(c.Cells(), 1); !strings.Contains(got, " required ") {
		t.Fatalf("row 1 = %q, want the floating message", got)
	}
	if _, painted := c.Frame(); painted != 0 {
		t.Fatalf("settled frame painted %d, want 0", painted)
	}
}

// A message that changes without resizing repaints the marker and
// NOTHING else — the live-text half of the same subscription, pinned
// clear of the TextBox that carries it in the real form.
func TestMarkerLiveMessageRepaintsTheMarkerAlone(t *testing.T) {
	errP, _, page := markerPage(30)
	c := gooey.NewComposer(page, 30, 4)
	errP.Set("required")
	c.Frame()
	if _, painted := c.Frame(); painted != 0 {
		t.Fatalf("settled frame painted %d, want 0", painted)
	}

	errP.Set("REQUIRED") // same rune count: same rect, nothing to restore
	_, painted := c.Frame()
	if painted != 1 {
		t.Fatalf("a live message change painted %d components, want 1 (the marker)", painted)
	}
	if got := row(c.Cells(), 1); !strings.Contains(got, " REQUIRED ") {
		t.Fatalf("row 1 = %q, want the marker repainted with the new message", got)
	}
	if _, painted := c.Frame(); painted != 0 {
		t.Fatalf("settled frame painted %d, want 0", painted)
	}
}

// The marker adopts the host TextBox's Error handle when it has none of
// its own — the property is named once in the common form.
func TestMarkerAdoptsHostError(t *testing.T) {
	errP := prop.NewSource("bad")
	tb := &TextBox{Text: prop.NewSource(""), Error: errP}
	m := &ValidationMarker{}
	tb.Attach(m)
	layer := &AdornmentLayer{}
	root := &Canvas{Children: []gooey.Component{tb, layer}}
	c := gooey.NewComposer(root, 20, 3)
	c.Frame()
	if m.Error != errP {
		t.Fatal("the marker did not adopt its host's Error handle")
	}
	if got := row(c.Cells(), 1); got != " bad" {
		t.Fatalf("row 1 = %q, want the adopted message floating below", got)
	}
}

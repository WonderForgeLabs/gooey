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
// ITS NAME OUTRUNS ITS ASSERTIONS, and that was measured rather than
// suspected: flipping markerPopup.AdornmentPersists to false reddens
// nothing here. The drop is self-healing within one frame — orphaned()
// nils m.pop and the same frame's ensurePlaced builds a fresh popup — so
// `m.pop == nil` cannot see it and neither can the cell plane. What this
// test does hold is the FILLER and the message coming back, which is
// worth keeping. The persist flag itself is pinned by identity in
// TestAValidationMarkerSurvivesAFreezeTurningOn. Raised in review of
// #498.
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

// TestAValidationMarkerPlacesItsAdornmentWhileFrozen holds the one claim
// worth keeping out of the superseded #444: Frozen gates INPUT, not
// adornment placement.
//
// It is salvaged rather than rewritten because its discriminating half is
// the part that is easy to leave out. A marker showing over a frozen
// field is exactly what a test with no <Frozen> in the tree would also
// report, so the name would be a claim about a wrapper that was doing
// nothing. Asking the FocusManager to focus the field first, and
// requiring the refusal, is what makes the rest of the test mean what it
// says.
//
// THE SEAM IT HOLDS DOWN IS IN THIS REPO, not in a comment somewhere
// else. FocusManager.walk gates the focus order on the freeze —
// `allow.Has(AllowFocus)` before appending to m.order (input.go:493) —
// and calls SetFocusManager on ATTACHMENTS unconditionally a few lines
// later (input.go:541-542). Those two lines are the whole of "Frozen
// gates input, not adornment placement".
//
// TWO TESTS REDDEN when the second is gated the way the first is, not
// one — this comment claimed "the only test in the tree" until review of
// #498 measured it:
//
//	--- FAIL: TestAValidationMarkerPlacesItsAdornmentWhileFrozen  components
//	--- FAIL: TestValidatorsStayLiveInsideAFrozenSubtree          markup
//
// The markup sibling (frozen_input_test.go:544) was already on main and
// holds the same line. What is unique here is WHAT is asserted: that one
// counts the layer's adornments, this one reads the rendered message off
// the cell plane and refuses focus first. A reader who deleted the
// markup test on the strength of the word "only" would have lost a pin,
// which is why the word is gone.
//
// It cited apps/wysiwyg/components/preview/overlay.go until review of
// #498 pointed out that no such comment exists there — it lived in
// #444's tree and was not salvaged with the test, and `git log -S Frozen`
// on that path returns nothing. Nothing catches a dead file path inside
// a Go comment: TestEveryCitedTestNameResolves reads Markdown and
// resolves test NAMES. So a citation that leaves this package is a
// citation nobody can tell has rotted, and the seam above needs no
// second file.
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
	assertMarkerShows(t, c, m, "while frozen")
}

// assertMarkerShows is both halves of "the user can see it", and the
// second half is the finding.
//
// IsShown() is `m.pop != nil && getStr(m.Error) != ""` (validation.go:98)
// — placed in a layer, plus a non-empty string. It is not a cell. A
// regression that placed the popup inside a frozen subtree and then
// arranged or painted it to nothing keeps that green while the form
// says nothing about what is wrong with it, which is the entire reason
// the claim is worth salvaging. Both siblings in this file pair the two
// already (TestValidationLoopDamage, TestMarkerAdoptsHostError). Raised
// in review of #498.
func assertMarkerShows(t *testing.T, c *gooey.Composer, m *ValidationMarker, when string) {
	t.Helper()
	if !m.IsShown() {
		t.Fatalf("the marker did not place %s — if Frozen has grown a gate on "+
			"the input-tree walk that is a real change, and the SetFocusManager "+
			"call at input.go:541-542 can take the `allow` check that guards "+
			"m.order at input.go:493", when)
	}
	if got := row(c.Cells(), 1); !strings.Contains(got, "required") {
		t.Fatalf("the marker reports itself shown %s but row 1 of the cell "+
			"plane is %q — placed in the layer and painting nothing is what "+
			"the user experiences as the form refusing to say what is wrong",
			when, got)
	}
}

// TestAValidationMarkerSurvivesAFreezeTurningOn is the designer's actual
// case, and it is a different code path.
//
// The test above freezes at build time. wysiwyg's Pane.BindDesignMode
// makes Frozen() a property read, so the freeze FLIPS — and a flip runs
// FocusManager.evictFrozen (input.go:430), which clears hover, captor,
// prev and lastClick. Nothing there drops adornments today.
//
// AND THE LAST PHASE IS ABOUT IDENTITY, because that is the only thing
// markerPopup.AdornmentPersists (validation.go:129) changes. This
// comment cited it as a held seam until review of #498 measured the
// mutation and found it reddened NOTHING in the root module — not this
// test, and not TestMarkerPersistsThroughHiddenAnchor below, whose name
// claims exactly that seam. The reason is that the drop is self-healing
// within one frame: the layer calls orphaned(), which nils m.pop, and
// the same frame's ensurePlaced builds a fresh popup. A/B on a hidden
// anchor:
//
//	                      pop != nil  IsShown  adornments  row 1
//	AdornmentPersists()     true       true        1       "####…"
//	          -> false      true       true        1       "####…"
//
// Every observable agrees; only the POINTER differs — persisting keeps
// the same *markerPopup, dropping replaces it. So this test hides the
// anchor while frozen and compares identity, and the mutation reddens it
// by name. Two arms agreeing is a harness result, not a passing test.
// Raised in review of #498.
func TestAValidationMarkerSurvivesAFreezeTurningOn(t *testing.T) {
	name := prop.NewSource("")
	errP := validate.Field(name, validate.Required("required"))
	tb := &TextBox{Text: name, Error: errP}
	m := &ValidationMarker{}
	tb.Attach(m)
	active := prop.NewSource(false)
	root := &VStack{Children: []gooey.Component{
		&Frozen{Child: tb, Active: active},
		&AdornmentLayer{},
	}}
	c := gooey.NewComposer(root, 30, 5)
	c.Frame()
	if !c.Focus().SetFocus(tb) {
		t.Fatal("the TextBox refused focus while Active is false, so the " +
			"freeze is already on and the flip below is not the thing " +
			"being measured")
	}
	assertMarkerShows(t, c, m, "before the freeze turned on")

	active.Set(true)
	c.Frame()
	if c.Focus().SetFocus(tb) {
		t.Fatal("the TextBox still took focus after Active flipped to true, " +
			"so the freeze did not take effect and the assertion below is " +
			"about an unfrozen tree")
	}
	assertMarkerShows(t, c, m, "after the freeze turned on")

	// THE DROP POLICY, exercised: an anchor that is present but not
	// visibly reachable is what sends the layer down the branch
	// AdornmentPersists opts out of. Identity is the assertion, for the
	// reason in the comment above — everything else is restored before
	// anyone can look.
	kept := m.pop
	if kept == nil {
		t.Fatal("no popup to hold onto, so the identity check below would " +
			"compare two nils and pass over the policy it is here for")
	}
	gooey.LayoutOf(tb).Visibility = gooey.Hidden
	c.Frame()
	if m.pop != kept {
		t.Error("hiding the frozen field's anchor REPLACED the marker's popup " +
			"instead of keeping it. markerPopup.AdornmentPersists opts out of " +
			"the adornment layer's drop-on-invisible policy; without it the " +
			"layer calls orphaned() and the next ensurePlaced builds a new one, " +
			"which every other observable — pop != nil, IsShown, the layer's " +
			"count, the cell plane — cannot tell from the popup surviving")
	}
}

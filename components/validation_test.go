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
// THE POINTER IS THE ONLY OBSERVABLE THAT SEES THE DROP, and that is
// measured rather than suspected: flipping
// markerPopup.AdornmentPersists to false reddens nothing that reads the
// visible state. The drop is self-healing within one frame — orphaned()
// nils m.pop and the same frame's ensurePlaced builds a fresh popup — so
// `m.pop == nil` could not see it, and neither can the cell plane or the
// layer's count. The popup POINTER is the one observable that separates
// the two states, so the check below is an identity comparison against
// the popup taken before the anchor was hidden. The same seam is pinned
// from the freeze side in
// TestAFrozenFieldsMarkerSurvivesItsAnchorBeingHidden, which is the test
// that hides an anchor INSIDE a frozen subtree.
func TestMarkerPersistsThroughHiddenAnchor(t *testing.T) {
	_, tb, m, _, page := formPage(30)
	c := gooey.NewComposer(page, 30, 4)
	c.Frame()
	if !m.IsShown() {
		t.Fatal("marker should be up")
	}

	kept := keepPopup(t, m)
	gooey.LayoutOf(tb).Visibility = gooey.Hidden
	c.Frame()
	// BOTH SIDES, and the second clause is the finding. Asserting only
	// that the filler came back is blind to the regression this test's
	// frozen sibling exists for: a persistent adornment arranged at its
	// anchor's full rect instead of a zero one leaves the message
	// painting over cells the field has given up. Measured — changing
	// adorn.go's persist branch to `a.Place(ab, b)` gives row 1 as
	// " required ####################", and Contains("####") passes
	// straight over it because the message is shorter than the filler,
	// so the FROZEN test reddens and this one does not. The two-sided
	// form is what TestValidationLoopDamage already uses. Raised in
	// review of #498, which also corrected the frozen sibling's comment
	// claiming this assertion was already making the stronger claim.
	if got := row(c.Cells(), 1); !strings.Contains(got, "####") ||
		strings.Contains(got, "required") {
		t.Fatalf("row 1 = %q, want the filler restored AND the message gone while "+
			"the field is hidden", got)
	}
	if m.pop != kept {
		t.Fatal("hiding the anchor REPLACED the persistent marker's popup " +
			"instead of keeping it — the layer dropped it and the same " +
			"frame's ensurePlaced built a fresh one, which is the drop " +
			"AdornmentPersists opts out of")
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

// frozenMarkerPage is the tree all three frozen-marker tests need: a
// TextBox with a required validator, a ValidationMarker attached to it,
// the pair inside a <Frozen>, and the AdornmentLayer that places the
// popup beside them. A nil active gives a plain <Frozen> — AllowNone
// from the first frame; a handle gives one whose freeze can be flipped.
//
// Written once because three copies of a fixture drift silently: one
// that quietly lost its <AdornmentLayer> still builds, still frames, and
// reports the marker missing as though the freeze had dropped it.
//
// The first frame is taken here, so what a caller asserts afterwards is
// its own claim and nothing else.
//
// THE LAYER IS KEPT AND READ BY THE ASSERTION THIS RETURNS, and that
// pairing is the point rather than a convenience. Every "the marker did
// not place" message in this file blames Frozen — that is what these
// tests are about — and a fixture whose AdornmentLayer never hosted
// anything produces exactly the same symptom from a cause that has
// nothing to do with freezing. Without the layer in hand, three tests
// would report a broken fixture as a Frozen gating bug.
func frozenMarkerPage(t *testing.T, active *prop.Property[bool]) (*TextBox, *ValidationMarker, *gooey.Composer, func(when string)) {
	t.Helper()
	name := prop.NewSource("")
	tb := &TextBox{Text: name, Error: validate.Field(name, validate.Required("required"))}
	m := &ValidationMarker{}
	tb.Attach(m)
	// Assigned unconditionally: Frozen.Active is a *prop.Property[bool]
	// and nil means "always frozen" (frozen.go), so a nil assignment and
	// no assignment are the same fixture. The one load-bearing
	// active != nil branch in this helper is the precondition below.
	frozen := &Frozen{Child: tb, Active: active}
	layer := &AdornmentLayer{}
	root := &VStack{Children: []gooey.Component{frozen, layer}}
	c := gooey.NewComposer(root, 30, 5)
	c.Frame()
	// THE PRECONDITION, AND ONLY FOR A CALLER WHOSE FIRST FRAME IS
	// UNFROZEN. A caller passing a handle starts with Active false, so an
	// empty layer here really does mean the fixture is broken. A nil
	// Active is a plain <Frozen> — AllowNone from this very frame — so
	// placement-while-frozen is what the frame above MEASURES, and
	// fataling on it reports the flagship test's own regression as a
	// broken fixture: "nothing was frozen" and "a page that never had a
	// marker" are both false there, and the fatal also puts
	// assertFrozenMarkerShows' correctly-worded arm out of reach for the one
	// test it was written for.
	prechecked := active != nil
	if prechecked && len(layer.Adornments()) == 0 {
		t.Fatal("the fixture placed no adornment on its first, unfrozen frame, " +
			"so every assertion about surviving a freeze is about a page that " +
			"never had a marker")
	}
	// THE ASSERTION COMES BACK BOUND TO THE FIXTURE, rather than the
	// layer and a bool coming back for every caller to re-pair by hand.
	// `prechecked` IS `active != nil` — the same condition three lines
	// up — and it was passed as a positional literal at four call sites
	// with nothing keeping the two in step: a caller switching from a
	// handle to nil and forgetting the literal got a fatal telling the
	// reader the opposite of the truth. That is the defect this branch
	// fixed one layer out and reintroduced one layer in. Binding it here
	// also retires the helper's hardcoded row 1, which is this fixture's
	// geometry and not any composer's.
	return tb, m, c, func(when string) {
		t.Helper()
		assertFrozenMarkerShows(t, c, layer, m, prechecked, when)
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
// the `allow.Has(AllowFocus)` test before it appends to m.order — and
// calls SetFocusManager on ATTACHMENTS unconditionally a few lines
// later. Those two statements inside FocusManager.walk are the whole of
// "Frozen gates input, not adornment placement".
//
// TWO TESTS REDDEN when the second is gated the way the first is, not
// one, measured:
//
//	--- FAIL: TestAValidationMarkerPlacesItsAdornmentWhileFrozen  components
//	--- FAIL: TestValidatorsStayLiveInsideAFrozenSubtree          markup
//
// markup.TestValidatorsStayLiveInsideAFrozenSubtree was already on main
// and holds the same line. What is unique here is WHAT is asserted: that
// one counts the layer's adornments, this one reads the rendered message
// off the cell plane and refuses focus first. A reader who deleted the
// markup test on the strength of the word "only" would have lost a pin,
// which is why the word is gone.
func TestAValidationMarkerPlacesItsAdornmentWhileFrozen(t *testing.T) {
	// A nil Active is a plain <Frozen>: AllowNone, the strongest freeze
	// there is, and frozen from the first frame.
	tb, _, c, shows := frozenMarkerPage(t, nil)
	// Discriminating half: without this the test passes just as well
	// with no Frozen in the tree at all, and its name would be a claim
	// about a wrapper that was doing nothing.
	if c.Focus().SetFocus(tb) {
		t.Fatal("the TextBox took focus, so the subtree is not frozen and " +
			"this test proves nothing about Frozen")
	}
	shows("while frozen")
}

// keepPopup is the precondition every identity assertion in this file
// needs: hold the popup, and refuse to proceed without one.
//
// A nil here makes the comparison below it two nils, which passes — so
// the guard is the assertion's other half, and it was written out by
// hand in both callers with a character-identical message. That is the
// shape assertFrozenMarkerShows was extracted for; this is the same
// duplication on the other assertion.
func keepPopup(t *testing.T, m *ValidationMarker) *markerPopup {
	t.Helper()
	if m.pop == nil {
		t.Fatal("no popup to hold onto, so the identity check below would " +
			"compare two nils and pass over the policy it is here for")
	}
	return m.pop
}

// assertFrozenMarkerShows is both halves of "the user can see it", and the
// second half is the finding.
//
// IsShown() is `m.pop != nil && getStr(m.Error) != ""`
// — placed in a layer, plus a non-empty string. It is not a cell. A
// regression that placed the popup inside a frozen subtree and then
// arranged or painted it to nothing keeps that green while the form
// says nothing about what is wrong with it, which is the entire reason
// the claim is worth salvaging. Both siblings in this file pair the two
// already (TestValidationLoopDamage, TestMarkerAdoptsHostError).
func assertFrozenMarkerShows(t *testing.T, c *gooey.Composer, layer *AdornmentLayer, m *ValidationMarker, prechecked bool, when string) {
	t.Helper()
	if !m.IsShown() {
		// IsShown() is a CONJUNCTION — a popup in a layer AND a non-empty
		// error string — so "not shown" is two states, and blaming one of
		// them sends the reader to the wrong file. Say which.
		switch {
		case m.pop == nil && len(layer.Adornments()) == 0:
			// AN EMPTY LAYER IS NOT PROOF OF A BROKEN FIXTURE, and which
			// of the two things it means depends on whether this
			// caller's fixture checked placement before now.
			// frozenMarkerPage can only check it for a caller whose
			// first frame is UNFROZEN, so the flagship test — which
			// passes nil, and is frozen from the first frame — has no
			// earlier success to contradict. Telling that reader a
			// build-time assertion passed or failed names two legs
			// neither of which happened.
			if !prechecked {
				t.Fatalf("the marker did not place %s and the layer hosts NO "+
					"adornment. This caller's FIRST frame was already frozen, so "+
					"no placement was asserted before now and there is no earlier "+
					"success to contradict: either the freeze is gating placement "+
					"— FocusManager.walk's unconditional SetFocusManager call on "+
					"attachments is where a new `allow` gate would do this — or "+
					"the page never had a marker at all", when)
			}
			t.Fatalf("the marker did not place %s and the layer hosts NO adornment. "+
				"frozenMarkerPage asserted a placement at build time for this "+
				"caller and it passed, so one WAS placed and something dropped it "+
				"— FocusManager.walk's unconditional SetFocusManager call on "+
				"attachments is where a new `allow` gate would do this", when)
		case m.pop == nil:
			t.Fatalf("the marker did not place %s: the layer hosts %d adornment(s) "+
				"and this marker's popup is nil, so it is THIS marker that was not "+
				"placed rather than the layer being empty",
				when, len(layer.Adornments()))
		default:
			// The other conjunct. A placed popup with an empty error is a
			// validation result, not a placement problem, and reading the
			// walk for it wastes the reader's time.
			t.Fatalf("the marker placed a popup %s and reports nothing to say: its "+
				"Error is empty, so the rule stopped failing rather than the "+
				"adornment being dropped", when)
		}
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
// FocusManager.evictFrozen, which clears hover, captor, prev and
// lastClick. Nothing there drops adornments today.
//
// SO THIS IS A FORWARD GUARD, and what it is forward OF was measured:
// the obvious eviction to write next — evictFrozen walking a frozen
// host's attachments and handing each a nil manager — leaves it green,
// because ensurePlaced returns early while m.pop is non-nil. Dropping a
// PLACED adornment needs the layer's own orphaned(), unexported here and
// unreachable from the framework side, so no one-line edit in
// evictFrozen reddens this today. It holds the door for the seam that
// would have to be added.
//
// THE FLIP ALONE: the freeze turns on, evictFrozen runs, and the marker
// is still placed and still painting. Hiding the anchor is
// TestAFrozenFieldsMarkerSurvivesItsAnchorBeingHidden's, because two
// causes behind one name cannot be told apart from a red run.
func TestAValidationMarkerSurvivesAFreezeTurningOn(t *testing.T) {
	active := prop.NewSource(false)
	tb, m, c, shows := frozenMarkerPage(t, active)
	if !c.Focus().SetFocus(tb) {
		t.Fatal("the TextBox refused focus while Active is false, so the " +
			"freeze is already on and the flip below is not the thing " +
			"being measured")
	}
	shows("before the freeze turned on")
	kept := keepPopup(t, m)

	active.Set(true)
	c.Frame()
	if c.Focus().SetFocus(tb) {
		t.Fatal("the TextBox still took focus after Active flipped to true, " +
			"so the freeze did not take effect and the assertion below is " +
			"about an unfrozen tree")
	}
	shows("after the freeze turned on")
	// IDENTITY, because the three observables above cannot see the
	// failure this test is named for. The table in
	// TestAFrozenFieldsMarkerSurvivesItsAnchorBeingHidden measured it:
	// IsShown, the layer's count and the cell plane all read the same for
	// a popup that SURVIVED and for one dropped and rebuilt in the same
	// frame. A forward guard asserting with the one instrument known to
	// be blind to its own subject is not a guard.
	if m.pop != kept {
		t.Error("the freeze flip REPLACED the marker's popup instead of keeping " +
			"it: evictFrozen must not drop a placed adornment, and a rebuild in " +
			"the same frame is invisible to every other assertion here")
	}
}

// TestAFrozenFieldsMarkerSurvivesItsAnchorBeingHidden is the drop policy
// on the frozen path, and it is its own test because it reddens for its
// own reason.
//
// The flip test above is named for the FLIP, and a reader who saw it red
// with this phase inside it had no way to tell "the freeze dropped the
// marker" from "hiding the anchor replaced the popup" without reading
// the body. Two causes behind one name is the shape this file has
// already been corrected for once, in
// TestMarkerPersistsThroughHiddenAnchor, whose name outran its
// assertions.
//
// IDENTITY IS THE ASSERTION, because it is the only thing
// markerPopup.AdornmentPersists changes. The drop is self-healing within
// one frame — the layer calls orphaned(), which nils m.pop, and the same
// frame's ensurePlaced builds a fresh popup — so on a hidden anchor:
//
//	                      pop != nil  IsShown  adornments  row 1
//	AdornmentPersists()     true       true        1       ""
//	          -> false      true       true        1       ""
//
// Measured on frozenMarkerPage with the anchor hidden, both arms. Row 1
// is empty because a hidden anchor vacates the cells the message was
// painting in. (Not formPage's "####…" filler: that is the SIBLING
// page's row, and transplanting it into a table that reads as measured
// is how a table stops being one.)
//
// Every observable agrees; only the POINTER differs. Two arms agreeing
// is a harness result, not a passing test.
//
// The unfrozen half of the same policy is
// TestMarkerPersistsThroughHiddenAnchor. This one takes the frozen path
// to it, which is the designer's case: a field that cannot be reached
// and then goes invisible under a collapsing pane.
func TestAFrozenFieldsMarkerSurvivesItsAnchorBeingHidden(t *testing.T) {
	active := prop.NewSource(false)
	tb, m, c, shows := frozenMarkerPage(t, active)
	active.Set(true)
	c.Frame()
	if c.Focus().SetFocus(tb) {
		t.Fatal("the TextBox took focus after Active flipped to true, so this " +
			"is not the frozen path and the unfrozen sibling already covers it")
	}

	// THE HELPER, not a hand copy of two of its three arms. An empty
	// layer HERE cannot be the page's fault — frozenMarkerPage asserted a
	// placement on the unfrozen frame and the freeze has been flipped on
	// since — so the helper's wording is the one that reaches the reader
	// with the right cause, and its third arm is the half a copy leaves
	// out: pop != nil is not "the user can see it".
	shows("while frozen, before the anchor was hidden")
	kept := keepPopup(t, m)
	gooey.LayoutOf(tb).Visibility = gooey.Hidden
	c.Frame()
	// THE CELLS TOO, not the pointer alone. Identity is what
	// AdornmentPersists changes and it is why this test exists — but a
	// regression that keeps the popup alive and arranges it at a full
	// rect instead of a zero one leaves an error message floating over a
	// field that is not on screen, and every pointer assertion passes
	// over it. Its unfrozen sibling asserts the same thing, two-sidedly:
	// checking only that the filler came back leaves the regression
	// intact, because the message is shorter than the filler.
	// EMPTY, not merely "does not say required": the table above measured
	// "" in both arms, and Contains accepts a popup arranged at a partial
	// rect painting `requir`, or painting the message one column over.
	if got := row(c.Cells(), 1); got != "" {
		t.Errorf("row 1 = %q while the frozen field is hidden, want it vacated: "+
			"the message is still painting over cells its anchor has given up", got)
	}
	if m.pop != kept {
		t.Error("hiding the frozen field's anchor REPLACED the marker's popup " +
			"instead of keeping it. markerPopup.AdornmentPersists opts out of " +
			"the adornment layer's drop-on-invisible policy; without it the " +
			"layer calls orphaned() and the next ensurePlaced builds a new one, " +
			"which every other observable — pop != nil, IsShown, the layer's " +
			"count, the cell plane — cannot tell from the popup surviving")
	}
}

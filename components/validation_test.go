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

// formW is formPage's width wherever these tests build one.
//
// NAMED BECAUSE THE ROW ASSERTIONS DERIVE FROM IT. The set is every
// formPage call in this file, every gooey.NewComposer built over one,
// and the whole-row equality expectations built from strings.Repeat —
// NAMED RATHER THAN COUNTED, because a count in prose is a sample taken
// once and this doc had already drifted: it said four while there were
// five, in a file whose sibling helper refuses to count its own callers
// for exactly that reason (see frozenMarkerPage). Raised in review of
// #498.
//
// THE COMPOSER IS PART OF THE SET, which is the half the first version
// of this const missed — and missing it produced the fatal this doc
// claims to prevent. The row expectations read the COMPOSER's row, so
// they hold only while the composer and the filler are the same width;
// with formW raised to 40 and the three composers still literal 30s:
//
//	validation_test.go:235: row 1 = "##############################",
//	  want "########################################"
//
// a fixture-width defect reported as the marker painting the wrong
// thing. Raised in review of #498.
const formW = 30

// requiredMsg is what the required validator paints for an empty field,
// and what every frozen-marker test but the tracking one passes to
// shows(). Named so the message and the fixture's validator move
// together; the tracking test writes its own, because its whole subject
// is the message changing.
const requiredMsg = " required"

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
	name, tb, m, _, page := formPage(formW)
	c := gooey.NewComposer(page, formW, 4)
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
// TWO OBSERVABLES SEE THE DROP: the pointer, and the HIDE frame's
// damage count. `m.pop == nil` sees nothing because the drop is
// self-healing within one frame (see settleAndHold), and neither does
// the cell plane or the layer's count. This test and its frozen sibling
// now hold the same policy with the same pair of instruments — the
// counts differ (2 here, 3 there) only because the pages differ.
//
// So the checks below are the hide frame's count and an identity
// comparison against the popup taken before the anchor was hidden. The
// same seam is pinned
// from the freeze side in
// TestAFrozenFieldsMarkerSurvivesItsAnchorBeingHidden, which is the test
// that hides an anchor INSIDE a frozen subtree.
func TestMarkerPersistsThroughHiddenAnchor(t *testing.T) {
	_, tb, m, _, page := formPage(formW)
	c := gooey.NewComposer(page, formW, 4)
	c.Frame()
	if !m.IsShown() {
		t.Fatal("marker should be up")
	}

	kept := keepPopup(t, m)
	gooey.LayoutOf(tb).Visibility = gooey.Hidden
	// THE HIDE FRAME'S COUNT IS AN INSTRUMENT, not bookkeeping: a
	// dropped-and-rebuilt popup is a FRESH component, so it paints. The
	// frozen sibling has asserted this since it was written and the
	// symmetric assertion was missing here, which left the pointer as
	// this test's only witness to the drop. Measured on formPage(formW):
	// 2 with markerPopup.AdornmentPersists() true, 1 with it false.
	//
	// THE NUMBER IS INLINE, beside its own assertion, like the frozen
	// sibling's 3. It was a file-scope const justified by a symmetry
	// that ran in neither direction, and CLAUDE.md treats a damage count
	// as the assertion itself — "if your change moves a number, that IS
	// the change" — which a name a scroll away and reachable from a
	// second test works against.
	//
	// AND Errorf, NOT Fatalf, which is the other half of the claim this
	// test's own doc makes: it says the two hide tests hold the same
	// policy with the same PAIR of instruments, and under the mutation
	// both exist for, a fatal here meant the pointer comparison below
	// was never reached. The frozen sibling reports both on one run.
	// Both raised in review of #498.
	if _, painted := c.Frame(); painted != 2 {
		t.Errorf("hiding the anchor repainted %d component(s), want 2 — a "+
			"marker dropped and rebuilt in the same frame is a fresh "+
			"component and one fewer repaint", painted)
	}
	// TWO FRAMES BEFORE THE POINTER IS READ, the second one inside
	// settleAndHold below, whose doc carries why one frame is the whole
	// window either instrument can see. What that costs this test: a
	// sweep that deferred orphaned() to the next layout pass would
	// rebuild the popup on frame two and this would read it as survival.
	// BOTH SIDES, and the second clause is the finding. Asserting only
	// that the filler came back is blind to the regression this test's
	// frozen sibling exists for: a persistent adornment arranged at its
	// anchor's full rect instead of a zero one leaves the message
	// painting over cells the field has given up. Measured — changing
	// adorn.go's persist branch to `a.Place(ab, b)` gives row 1 as
	// " required ####################", and Contains("####") passes
	// straight over it because the message is shorter than the filler,
	// so the FROZEN test reddens and this one does not. The two-sided
	// form is what TestValidationLoopDamage already uses.
	//
	// AND THE ROW IS COMPARED WHOLE, which is the same argument one step
	// further. Contains("####") && !Contains("required") accepts a popup
	// arranged at a PARTIAL rect painting `requir` — neither clause
	// fires, and the test is green over a message painting on top of a
	// field that is not on screen. The row here is a known constant,
	// formPage's own strings.Repeat("#", w), so equality costs no new
	// fixture.
	if got, want := row(c.Cells(), 1), strings.Repeat("#", formW); got != want {
		t.Fatalf("row 1 = %q, want %q — the filler restored AND the message "+
			"gone while the field is hidden", got, want)
	}
	// THE SETTLED FRAME AND THE POINTER, which are one claim — see
	// settleAndHold. The policy under test here is
	// markerPopup.AdornmentPersists opting out of the layer's
	// drop-on-invisible sweep.
	settleAndHold(t, c, m, kept, "hiding the anchor")

	gooey.LayoutOf(tb).Visibility = gooey.Visible
	c.Frame()
	// WHOLE HERE TOO, and this is the side where Contains was sharpest.
	// The argument three assertions up — a popup at a PARTIAL rect
	// painting `requir` satisfies the loose form — applies to the restore
	// unchanged, and worse: " required ####…" is the EXACT row the
	// a.Place(ab, b) regression named at the top of this test produces,
	// so the one assertion still written with the blind instrument had
	// the regression's own signature as a passing condition. The row is
	// the same constant, message over formPage's filler.
	//
	// COLUMNS, NOT BYTES: len() on a string is a byte count and CLAUDE.md
	// forbids one as a width. It is ASCII here and would be right by
	// accident; this is the package whose `row` helper was rewritten onto
	// render.RowText for exactly that class.
	if got, want := row(c.Cells(), 1),
		" required "+strings.Repeat("#", formW-render.StringWidth(" required ")); got != want {
		t.Fatalf("row 1 = %q, want %q — the message back with its anchor, over "+
			"the filler it does not cover", got, want)
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
	_, _, m, _, page := formPage(formW)
	c := gooey.NewComposer(page, formW, 4)
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
	c := gooey.NewComposer(page, formW, 4)
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
	// tooltip and the toast pin. (The 3 in TestValidationLoopDamage is a
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
	c := gooey.NewComposer(page, formW, 4)
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

// frozenMarkerFixture is what frozenMarkerPage hands back, and it is a
// STRUCT because the positional form does not survive growth. Each new
// value rewrote every call site and left callers discarding the rest
// with `_`; the two funcs have different signatures, so mis-ordering
// THOSE is a compile error, but the three pointers are interchangeable
// and a swap of them is not. Named fields make an addition additive and
// the ordering hazard unspellable.
type frozenMarkerFixture struct {
	tb   *TextBox
	name *prop.Property[string]
	m    *ValidationMarker
	c    *gooey.Composer
	// shows is both halves of "the user can see it": IsShown() and the
	// rendered row, bound to this fixture's own composer and layer.
	//
	// IT TAKES THE MESSAGE, and that is not ceremony. It compared row 1
	// against a hardcoded " required" on the grounds that the row is a
	// constant of the FIXTURE — true when it was written, and untrue
	// from the commit that gave frozenMarkerPage a validate.Len so the
	// message could change. A caller using it after name.Set("ab")
	// would have been told the marker is "painting something other than
	// the message" while it painted exactly the right one: a broken
	// caller reported as a Frozen gating bug, which is the class this
	// fixture's own doc exists to stop. Raised in review of #498.
	shows func(want, when string)
	// freeze flips Active, takes the flip frame and confirms the
	// refusal, returning that frame's painted count.
	freeze func() int
}

// frozenMarkerPage is the tree every frozen-marker test in this file
// needs: a TextBox with a required validator, a ValidationMarker
// attached to it, the pair inside a <Frozen>, and the AdornmentLayer
// that places the popup beside them. A nil active gives a plain <Frozen> — AllowNone
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
// nothing to do with freezing. Without the layer in hand, a caller would
// report a broken fixture as a Frozen gating bug.
//
// NO COUNT IN EITHER SENTENCE, because the next caller makes one wrong.
// That is the counted-in-prose failure CLAUDE.md's Verify section is
// explicit about, in the one file whose review history is entirely
// about claims outrunning what is enforced.
func frozenMarkerPage(t *testing.T, active *prop.Property[bool]) frozenMarkerFixture {
	t.Helper()
	name := prop.NewSource("")
	// TWO RULES, because one of them cannot make the message CHANGE.
	// With Required alone the error only ever moves between "required"
	// and "", and both of those move the popup's RECT — size() is
	// gooey.Size{} on an empty message and Render returns early on
	// b.W <= 0 — so every cell read in the tracking test is satisfied by
	// geometry, and a popup whose text() had gone stale would repaint the
	// correct string on the way back because the stale value and the
	// correct one are the same. Len gives the one transition where both
	// values are non-empty and only the CONTENT differs, which is the
	// transition that test is named for. formPage already pairs the two.
	tb := &TextBox{Text: name, Error: validate.Field(name,
		validate.Required("required"), validate.Len(3, 0, ""))}
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
	// THE PRECONDITION BELOW CLAIMS AN UNFROZEN FIRST FRAME, so a handle
	// that arrives already true is refused rather than accommodated: that
	// fixture would have the precondition measuring
	// placement-while-frozen and reporting the flagship test's own
	// regression as "a page that never had a marker". No caller wants it
	// — the frozen-first case is what a nil Active already is.
	// Active.Get() here is a plain read, helper code running outside any
	// evaluation, so it records no dependency.
	if active != nil && active.Get() {
		t.Fatal("frozenMarkerPage was handed an Active that is already true, so " +
			"its first frame is FROZEN and the precondition below would be " +
			"measuring placement-while-frozen. Pass a handle starting false " +
			"and flip it, or pass nil for a page frozen from the first frame")
	}
	// THE PRECONDITION, AND ONLY FOR A CALLER WHOSE FIRST FRAME IS
	// UNFROZEN. A caller passing a handle starts with Active false — the
	// fatal above is what makes that a fact rather than a convention — so
	// an empty layer here really does mean the fixture is broken. A nil
	// Active is a plain <Frozen> — AllowNone from this very frame — so
	// placement-while-frozen is what the frame above MEASURES, and
	// fataling on it reports the flagship test's own regression as a
	// broken fixture: "nothing was frozen" and "a page that never had a
	// marker" are both false there, and the fatal also puts
	// the assertion's correctly-worded arm out of reach for the one test it
	// was written for.
	prechecked := active != nil
	if prechecked && len(layer.Adornments()) == 0 {
		t.Fatal("the fixture placed no adornment on its first, unfrozen frame, " +
			"so every assertion about surviving a freeze is about a page that " +
			"never had a marker")
	}
	// THE ASSERTION COMES BACK BOUND TO THE FIXTURE, rather than the
	// layer and a bool coming back for every caller to re-pair by hand:
	// `prechecked` IS `active != nil`, the same condition three lines up,
	// so a caller who could pass it separately could pass it wrongly and
	// get a fatal telling the reader the opposite of the truth.
	//
	// THE HARDCODED ROW 1 STAYS. The assertion still reads
	// row(c.Cells(), 1), and that row is this fixture's geometry rather
	// than the package's. What the binding changes is who can reach it —
	// see the closure below, which is the assertion itself and not a call
	// to something a second page could also call.
	// name COMES BACK TOO, because every test here took its assertions
	// against an error string that never moved while frozen. The leg a
	// designer actually hits is the bound property changing UNDER the
	// frozen pane: the message appears, changes or clears while nothing
	// can be typed into the field. Without the handle a caller cannot
	// reach it, and the set proves the marker arrives and survives under
	// a freeze while saying nothing about it staying correct.
	// THE ASSERTION IS THIS CLOSURE'S OWN BODY, and that is the whole of
	// the binding: there is no package-level symbol, so no second page can
	// call it. A callable helper taking (c, layer, m, prechecked, when)
	// would be reachable from formPage — where row 1 is the "####…"
	// filler rather than the message — and would fatal about a frozen
	// marker on a page with no <Frozen> in it.
	//
	// WHAT IT ASSERTS is both halves of "the user can see it", and the
	// second half is the finding. IsShown() is `m.pop != nil &&
	// getStr(m.Error) != ""` — placed in a layer, plus a non-empty string.
	// It is not a cell. A regression that placed the popup inside a frozen
	// subtree and then arranged or painted it to nothing keeps that green
	// while the form says nothing about what is wrong with it, which is the
	// entire reason the claim is worth salvaging. Both siblings in this file
	// pair the two already (TestValidationLoopDamage,
	// TestMarkerAdoptsHostError).
	//
	// THE SECOND STEP IS THE FLIP, for the same reason. Two tests below
	// opened with a character-identical five-line prologue — build with a
	// false handle, Set(true), a Frame, then the focus refusal — differing
	// only in the tail of the fatal, which is exactly the drift this
	// fixture's own comment argues against for the tree. Nothing kept the
	// two messages in step and the next test that needed a frozen page
	// would have written a third.
	//
	// IT RETURNS THE FLIP FRAME'S PAINTED COUNT, which is what lets
	// TestAValidationMarkerSurvivesAFreezeTurningOn use it rather than
	// hand-write the prologue for the sake of counting the frame: one
	// prologue, one refusal message, and the flip test keeps its
	// painted==1 claim. Callers with no count to make ignore it.
	f := frozenMarkerFixture{tb: tb, name: name, m: m, c: c}
	f.shows = func(want, when string) {
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
		// WHOLE ROW, NOT Contains. A popup arranged one column narrow,
		// painting " requir", satisfies neither clause of the two-sided
		// form and passes the Contains form by accident. Equality is
		// available because the caller knows the field's value and so
		// knows its message — which is why it passes one in rather than
		// this closure assuming the empty-field one.
		if got := row(c.Cells(), 1); got != want {
			t.Fatalf("the marker reports itself shown %s but row 1 of the cell "+
				"plane is %q, want %q — placed in the layer and painting "+
				"something other than the message is what the user experiences "+
				"as the form refusing to say what is wrong", when, got, want)
		}
	}
	f.freeze = func() int {
		t.Helper()
		if active == nil {
			t.Fatal("freeze() was called on a page whose Active is nil. A plain " +
				"<Frozen> is AllowNone from its first frame, so there is no flip " +
				"to make and the refusal below would pass without measuring one")
		}
		active.Set(true)
		// THE FLIP FRAME, and its count goes back to the caller. It
		// exists so evictFrozen runs before anything is asserted;
		// only the test named for the flip's damage reads the number.
		_, painted := c.Frame()
		if c.Focus().SetFocus(tb) {
			t.Fatal("the TextBox took focus after Active flipped to true, so the " +
				"subtree is not frozen: whatever the caller asserts next is about " +
				"an ordinary field, and the unfrozen sibling test already covers it")
		}
		// A SECOND READING AT THE END, FOR EVERY CALLER. The refusal
		// above is taken once, on the flip frame, and every assertion a
		// caller makes afterwards reads IDENTICALLY on a subtree that
		// stopped being frozen partway through — measured on the
		// tracking test, where under active.Set(false) every observable
		// passed and only this closing refusal caught it. One caller
		// had it written out and the other two did not, which is the
		// same defect one scroll apart.
		//
		// IN A CLEANUP RATHER THAN AS A CALLER'S LAST STATEMENT,
		// because that is what lets the legs below use shows(), which
		// FATALS: as a closing statement it is unreachable from any
		// failing assertion, so the leg most likely to be failing is
		// the one that never gets the second reading. Raised in review
		// of #498, from the one caller that had it.
		t.Cleanup(func() {
			if c.Focus().SetFocus(tb) {
				t.Error("the TextBox took focus at the END of the test, so the " +
					"subtree stopped being frozen somewhere above. Every " +
					"assertion between here and freeze() was taken on an " +
					"ordinary field, and reads the same either way")
			}
		})
		return painted
	}
	return f
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
	f := frozenMarkerPage(t, nil)
	tb, c, shows := f.tb, f.c, f.shows
	// Discriminating half: without this the test passes just as well
	// with no Frozen in the tree at all, and its name would be a claim
	// about a wrapper that was doing nothing.
	if c.Focus().SetFocus(tb) {
		t.Fatal("the TextBox took focus, so the subtree is not frozen and " +
			"this test proves nothing about Frozen")
	}
	shows(requiredMsg, "while frozen")
}

// keepPopup is the precondition every identity assertion in this file
// needs: hold the popup, and refuse to proceed without one.
//
// A nil here makes the comparison below it two nils, which passes — so
// the guard is the assertion's other half, and it was written out by
// hand in its callers with a character-identical message. That is the
// shape frozenMarkerPage's own closures were written for; this is the
// same duplication on the other assertion.
//
// No count in that sentence, for the reason frozenMarkerPage's doc gives
// about its own.
func keepPopup(t *testing.T, m *ValidationMarker) *markerPopup {
	t.Helper()
	if m.pop == nil {
		t.Fatal("no popup to hold onto, so the identity check below would " +
			"compare two nils and pass over the policy it is here for")
	}
	return m.pop
}

// settleAndHold is keepPopup's other half: take the second frame, assert the page
// settled at zero on it, and compare the pointer against what keepPopup
// held.
//
// ONE FUNCTION BECAUSE THE TWO ASSERTIONS ARE ONE CLAIM, and they were
// written out at four sites with four bespoke fatals saying the same
// thing. Both halves are about one window: the drop is self-healing
// WITHIN a frame — the layer calls orphaned() and that same frame's
// ensurePlaced builds a fresh popup — so a second frame is the whole of
// what the identity check can see, and the settled count is free at that
// read. That is the same duplication keepPopup was extracted for on the
// nil-guard half, and frozenMarkerPage's closures on the prologue.
//
// THE COUNT IS PART OF THE CLAIM, not a neighbour of it: a rebuilt popup
// is a fresh component, so the frame that rebuilt it does not settle —
// it is the one observable besides the pointer that can see a rebuild at
// all. pop != nil, IsShown, the layer's count and the cell plane all
// read identically for a popup that survived and one dropped and rebuilt
// in the same frame, which the table in
// TestAFrozenFieldsMarkerSurvivesItsAnchorBeingHidden measured.
//
// ZERO AT EVERY CALLER, so `when` is the only thing that varies.
func settleAndHold(t *testing.T, c *gooey.Composer, m *ValidationMarker, kept *markerPopup, when string) {
	t.Helper()
	if _, painted := c.Frame(); painted != 0 {
		t.Errorf("the frame after %s repainted %d component(s), want a settled "+
			"page: it is one damage event, not a loop", when, painted)
	}
	if m.pop != kept {
		t.Errorf("%s REPLACED the marker's popup instead of keeping it — the "+
			"layer dropped it and the same frame's ensurePlaced built a fresh "+
			"one. Read after a SECOND frame, so a rebuild deferred to the next "+
			"layout pass is caught too", when)
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
// SO THIS IS A FORWARD GUARD, and what it is forward OF is stated in the
// commit message rather than here: a mutation result is a sample taken
// once, and this paragraph described what a hypothetical edit to a file
// this test does not touch would do — unfalsifiable from inside the
// suite, and silently wrong the day somebody makes a different edit.
// CLAUDE.md's Verify section legislates the same thing one level up
// about counts in prose. What survives is why the assertion exists.
//
// What it guards is a seam that does not exist yet: dropping a PLACED
// adornment needs the layer's own orphaned(), which is unexported and
// unreachable from the framework side. The assertion holds the door for
// the day something reaches it.
//
// THE FLIP ALONE: the freeze turns on, evictFrozen runs, and the marker
// is still placed and still painting. Hiding the anchor is
// TestAFrozenFieldsMarkerSurvivesItsAnchorBeingHidden's, because two
// causes behind one name cannot be told apart from a red run.
func TestAValidationMarkerSurvivesAFreezeTurningOn(t *testing.T) {
	active := prop.NewSource(false)
	f := frozenMarkerPage(t, active)
	tb, m, c, shows, freeze := f.tb, f.m, f.c, f.shows, f.freeze
	if !c.Focus().SetFocus(tb) {
		t.Fatal("the TextBox refused focus while Active is false, so the " +
			"freeze is already on and the flip below is not the thing " +
			"being measured")
	}
	shows(requiredMsg, "before the freeze turned on")
	kept := keepPopup(t, m)

	// THROUGH THE HELPER, WITH ITS COUNT, so the focus-refusal fatal has
	// one spelling in this file rather than one per test that needs the
	// frame.
	//
	// THE COUNT, because this is the frame the test is named for and no
	// other assertion here can see it. evictFrozen clears hover, captor,
	// prev and lastClick on the flip; a re-sync that grew into a
	// full-page repaint would leave IsShown, the layer count, the cell
	// plane AND the pointer below all green, and CLAUDE.md is explicit
	// that the damage count is the only pin for a repaint claim.
	// Measured on this fixture: the flip paints one component and the
	// next frame settles at zero.
	if painted := freeze(); painted != 1 {
		t.Errorf("the freeze flip repainted %d component(s), want 1 — the "+
			"frozen host is what changed, and a wider repaint means the "+
			"re-sync is rebuilding more of the page than the flip touched",
			painted)
	}
	shows(requiredMsg, "after the freeze turned on")
	// THE SETTLED FRAME AND THE POINTER — see settleAndHold. What this one is a
	// forward guard for is evictFrozen: it must not drop a placed
	// adornment, and a rebuild in the same frame is invisible to every
	// other assertion here.
	settleAndHold(t, c, m, kept, "the freeze flip")
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
// IDENTITY IS THE ASSERTION, and the DAMAGE COUNT is the second
// instrument that sees the same thing. Why one frame is the whole window
// either can see — the drop being self-healing within it — is on
// settleAndHold, which is where every caller's identity check goes. On a
// hidden anchor:
//
//	                      pop != nil  IsShown  adornments  row 1  painted
//	AdornmentPersists()     true       true        1       ""        3
//	          -> false      true       true        1       ""        2
//
// Measured on frozenMarkerPage with the anchor hidden, both arms. Row 1
// is empty because a hidden anchor vacates the cells the message was
// painting in. (Not formPage's "####…" filler: that is the SIBLING
// page's row, and transplanting it into a table that reads as measured
// is how a table stops being one.)
//
// EVERY VISIBLE-STATE OBSERVABLE AGREES, and the two that do not are the
// pointer and the count — a dropped-and-rebuilt popup is a fresh
// component, so the hide paints two rather than three. Both are t.Errorf
// rather than t.Fatalf, so under the mutation BOTH report on the same
// run, which is the stronger reason for both columns being in the table:
// two independent instruments seeing one event, not one shadowing the
// other.
//
// Two arms agreeing is a harness result, not a passing test.
//
// The unfrozen half of the same policy is
// TestMarkerPersistsThroughHiddenAnchor. This one takes the frozen path
// to it, which is the designer's case: a field that cannot be reached
// and then goes invisible under a collapsing pane.
func TestAFrozenFieldsMarkerSurvivesItsAnchorBeingHidden(t *testing.T) {
	active := prop.NewSource(false)
	f := frozenMarkerPage(t, active)
	tb, m, c, shows, freeze := f.tb, f.m, f.c, f.shows, f.freeze
	freeze()

	// THE HELPER, not a hand copy of two of its three arms. An empty
	// layer HERE cannot be the page's fault — frozenMarkerPage asserted a
	// placement on the unfrozen frame and the freeze has been flipped on
	// since — so the helper's wording is the one that reaches the reader
	// with the right cause, and its third arm is the half a copy leaves
	// out: pop != nil is not "the user can see it".
	shows(requiredMsg, "while frozen, before the anchor was hidden")
	kept := keepPopup(t, m)
	gooey.LayoutOf(tb).Visibility = gooey.Hidden
	// THE COUNT, for the reason the flip test gives. Measured on this
	// fixture: hiding the anchor paints three — the field, the adornment
	// and the stack that has to re-fill the vacated cells — and the next
	// frame settles at zero. A hide that repainted the whole page would
	// leave every assertion below green.
	if _, painted := c.Frame(); painted != 3 {
		t.Errorf("hiding the frozen field repainted %d component(s), want 3", painted)
	}
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
	// THE SETTLED FRAME AND THE POINTER — see settleAndHold. The policy is
	// markerPopup.AdornmentPersists again, here on the frozen path.
	settleAndHold(t, c, m, kept, "hiding the frozen field's anchor")
}

// The leg the other three do not hold: the error MOVING while the
// subtree is frozen.
//
// "Frozen gates INPUT, not adornment placement" is the claim this set
// salvages, and placement, a freeze flipping on, and an anchor going
// hidden are three ways of asking whether the marker is THERE. Every
// assertion in them is taken against an error string that never changes
// under the freeze. What a designer hits is the other one: a bound
// property changes underneath a pane nothing can be typed into, and the
// message has to appear, change or clear with it. A regression that
// froze the popup's CONTENT would leave the form showing a message
// about a value it no longer holds, with every other assertion in this
// file green.
//
// THE COUNTS ARE THE POINT, not decoration. Measured on this fixture:
//
//	name.Set("ab")    row 1 = " at least 3 characters"  IsShown=true   painted=3
//	name.Set("abc")   row 1 = ""                        IsShown=false  painted=3
//	name.Set("")      row 1 = " required"               IsShown=true   painted=2
//
// Three going valid — the field, the adornment, and the stack refilling
// the cells the message vacates — and two coming back, because the
// stack's row is already blank. A change that repainted the whole page
// would satisfy the cell reads and IsShown alike.
//
// AND THE FIRST ROW IS THE LEG THE OTHER TWO CANNOT HOLD. Both of those
// move the error between a string and nothing, which moves the popup's
// RECT, so a popup frozen at its CONTENT satisfies them by repainting
// the string it is stuck on. Only a transition with BOTH values
// non-empty separates the two, which is why the fixture carries a
// second rule.
func TestAFrozenFieldsMarkerTracksItsErrorWhileFrozen(t *testing.T) {
	active := prop.NewSource(false)
	f := frozenMarkerPage(t, active)
	name, m, c, shows, freeze := f.name, f.m, f.c, f.shows, f.freeze
	freeze()
	// THE FREEZE AGAIN, at the END, and registered here so it runs even
	// when an assertion below fatals. freeze() established it once,
	// before anything moved, and every assertion in this test reads
	// identically on a tree that stopped being frozen somewhere in the
	// frames it drives — which would make "WhileFrozen" in the name a
	// claim held only at the first line. The window is not hypothetical
	// in kind: the input tree re-syncs per frame and evictFrozen runs off
	// a property read, which is the machinery the two tests above are
	// named for. Same discriminator, second reading.
	//
	// The cleanup that takes it is registered by freeze() itself now,
	// for every caller — it was written out here and nowhere else.
	// THE HELPER, for the reason its siblings give: an empty layer here
	// cannot be the page's fault, and its third arm is the one a hand
	// copy leaves out.
	shows(requiredMsg, "while frozen, before the error moved")

	// AND THE POINTER, HELD ACROSS THE WINDOW THIS TEST OPENS. The
	// measured table at the hidden-anchor test establishes that
	// `pop != nil`, IsShown(), the layer's count and the cell plane all
	// read IDENTICALLY for a popup that survived and one that was
	// dropped and rebuilt in the same frame; the pointer is the only
	// thing that separates them. Driving the error to empty and back
	// arranges this popup to a zero rect and back, so a layer sweep that
	// dropped zero-rect adornments and let the next ensurePlaced build a
	// fresh one would leave every other assertion below green — counts
	// included, because a rebuilt popup reaching the same state paints
	// the same cells. This is the same guard the other three tests in
	// this file already carry.
	kept := keepPopup(t, m)

	// THE MESSAGE CHANGING, WITH BOTH VALUES NON-EMPTY. The three legs
	// below move the error between a string and nothing, and both of
	// those move the popup's rect — so a popup frozen at its CONTENT
	// would satisfy every one of them, repainting the same string it was
	// stuck on. "required" to "at least 3 characters" is the only
	// transition in which the two are distinguishable, and it is the one
	// this test is named for.
	name.Set("ab")
	// THREE, measured — the same number as the going-valid frame below,
	// and for the same reason TestValidationLoopDamage's resize is three:
	// a message that changes width hands cells back and takes others,
	// so the row underneath repaints with the field and the adornment.
	if _, painted := c.Frame(); painted != 3 {
		t.Errorf("the frozen field's message CHANGING repainted %d "+
			"component(s), want 3 (the field, the adornment, and the row the "+
			"resized message hands cells back to)", painted)
	}
	if got, want := row(c.Cells(), 1), " at least 3 characters"; got != want {
		t.Errorf("row 1 = %q while the frozen field's error moved, want %q — "+
			"the popup is painting a message about a value the field no "+
			"longer holds", got, want)
	}
	if !m.IsShown() {
		t.Error("the marker stopped reporting itself shown while its message " +
			"merely changed")
	}
	if _, painted := c.Frame(); painted != 0 {
		t.Errorf("the frame after the message changed repainted %d "+
			"component(s), want a settled page", painted)
	}

	name.Set("abc")
	if _, painted := c.Frame(); painted != 3 {
		t.Errorf("the value going VALID under a freeze repainted %d "+
			"component(s), want 3", painted)
	}
	if m.IsShown() {
		t.Error("the marker is still shown after the frozen field's value became " +
			"valid: the message outlived the error it was about, which is what " +
			"a user reads as the form refusing input it has already accepted")
	}
	if got := row(c.Cells(), 1); got != "" {
		t.Errorf("row 1 = %q after the frozen field became valid, want it "+
			"vacated: the message is still painting over cells nothing owns", got)
	}
	if _, painted := c.Frame(); painted != 0 {
		t.Errorf("the frame after the value became valid repainted %d "+
			"component(s), want a settled page", painted)
	}

	name.Set("")
	// TWO, not three. The stack's row is already blank by now, so only
	// the field and the adornment repaint — which is why this is not the
	// same number as the frame above and why writing one constant for
	// both would pin nothing.
	if _, painted := c.Frame(); painted != 2 {
		t.Errorf("the value going INVALID again under a freeze repainted %d "+
			"component(s), want 2", painted)
	}
	// THE HELPER HERE TOO. A hand copy of two of its three arms reports
	// "the marker did not come back" where shows() reports which of
	// empty layer / this popup nil / placed-but-empty-Error it is — the
	// diagnosis the first line of this test already paid for. shows()
	// FATALS, which is what the cleanup above is positioned to survive.
	shows(requiredMsg, "after the error returned")
	// THE SETTLED FRAME AND THE POINTER — see settleAndHold. Here the window the
	// identity check watches is the error going empty and back, which
	// arranges this popup to a zero rect and out of it again.
	settleAndHold(t, c, m, kept, "the error going empty and coming back")
}

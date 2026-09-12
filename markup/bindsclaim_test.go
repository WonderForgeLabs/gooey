package markup

import (
	"image"
	"slices"
	"sort"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
	"github.com/WonderForgeLabs/gooey/term"
)

// An AttrSpec's Binds is a PROMISE TO EVERY CATALOG CONSUMER — the
// control-plane element surface, the MCP tool schema, the wysiwyg
// property grid — and #314's third finding is that nothing checked it.
//
// StatusBar's Left/Center/Right said `Kind: KindString, Binds:
// BindsLiteral`, which catalog.go defines as "literal only, used
// verbatim". The loader runs all three through literalOrBound and the
// reference doc says they bind. So every consumer was told an attribute
// takes no binding while the runtime honoured one: a property grid
// offering a plain text field for a value the app drives from a handle.
//
// THE CHECK IS THE BUILD, not a second reading of the code. For every
// attribute the catalog declares bindable, markup that binds it must
// load; the sibling test below takes the other direction.
func TestEveryBindableAttributeReallyBinds(t *testing.T) {
	var checked int
	var offHarness []string
	for _, a := range bindableAttrs(t) {
		name := a.el + "." + a.attr.Name
		if !reachable(t, a) {
			offHarness = append(offHarness, name)
			continue
		}
		checked++
		t.Run(name, func(t *testing.T) {
			src := bindsHarness(t, a, a.attr.Name, bindSpelling(a.attr))
			if _, err := Build([]byte("<Gooey>"+src+"</Gooey>"), bindsContext()); err != nil {
				t.Errorf("the catalog declares %s %s, so every consumer offers "+
					"a binding for it, and the loader refuses one: %v",
					name, a.attr.Binds, err)
			}
		})
	}
	if checked == 0 {
		t.Fatal("no bindable attribute was reachable, so this test passed over nothing")
	}
	// THE HARNESS GAP IS REPORTED, NOT SWALLOWED. An element the probe
	// cannot build is unchecked, and an unchecked attribute nobody is
	// told about is the same as no test. Named rather than counted, so a
	// new gap is red instead of fitting under a cap, and a closed one
	// has to be deleted.
	sort.Strings(offHarness)
	for _, name := range offHarness {
		if _, ok := unseedable[elementOf(name)]; !ok {
			t.Errorf("%s cannot be built by this probe and is therefore "+
				"unchecked. Either seed what its element needs, or add the "+
				"element to unseedable with the reason", name)
		}
	}
	// And the other direction: an entry that stops being needed is a
	// note about a gap that closed.
	off := map[string]bool{}
	for _, name := range offHarness {
		off[elementOf(name)] = true
	}
	for el := range unseedable {
		if !off[el] {
			t.Errorf("unseedable names <%s>, which the probe now builds — "+
				"delete the entry", el)
		}
	}
}

// TestALiteralOnlyAttributeIsNotSilentlyBindable is the other direction,
// and it is the one that found more than #314 reported.
//
// BindsLiteral means the value is used VERBATIM. Thirteen attributes
// declared it while the loader took a `{{...}}` without complaint —
// StatusBar's three because they genuinely bind (the spec was wrong, now
// fixed), and TEN because the binding was accepted and DROPPED.
//
// Measured, and the measurement is what separates the two cases:
// `<HStack Gap="7">` renders differently from `<HStack>`, and
// `<HStack Gap="{{.I}}">` with I bound to 7 renders IDENTICALLY to
// `<HStack>`. The value never arrives. Same for Text.Bold, and for Name
// the element registers under the literal string "{{.I}}", so
// markup.Find and PatchMarkup cannot reach it afterwards.
//
// That class is issue #488 and is NOT fixed here: refusing a binding at
// load time is a behaviour change across eleven attributes and belongs
// in its own commit. What this test does is stop the LIST growing —
// every remaining offender is named in silentlyBindable below, so a
// twelfth cannot arrive quietly, and closing #488 means deleting entries
// rather than discovering them.
func TestALiteralOnlyAttributeIsNotSilentlyBindable(t *testing.T) {
	known := map[string]bool{}
	for _, s := range silentlyBindable {
		known[s] = true
	}
	seen := map[string]bool{}
	for _, a := range literalAttrs(t) {
		name := a.el + "." + a.attr.Name
		if !reachable(t, a) {
			continue
		}
		src := bindsHarness(t, a, a.attr.Name, "{{.S}}")
		_, err := Build([]byte("<Gooey>"+src+"</Gooey>"), bindsContext())
		if err != nil {
			if known[name] {
				t.Errorf("%s is listed in silentlyBindable and now REFUSES a "+
					"binding — the defect is fixed, so delete the entry "+
					"rather than leaving a note about a bug that is gone",
					name)
			}
			continue
		}
		seen[name] = true
		if !known[name] {
			t.Errorf("%s declares Binds: BindsLiteral — \"literal only, used "+
				"verbatim\" — and the loader accepts `{{.S}}`. Either the "+
				"value is honoured, and the spec lies to every catalog "+
				"consumer, or it is dropped, which is the silent-drop class "+
				"the declared vocabulary exists to close. Decide which, then "+
				"fix the spec or refuse the binding (#488)", name)
		}
	}
	// THE MUST-FIRE HALF. A list of known offenders that no longer
	// matches anything would let this test pass while checking nothing —
	// a renamed element, a probe that stopped building, a harness that
	// started erroring for an unrelated reason all look the same from
	// here.
	if len(seen) == 0 {
		t.Fatal("no literal-only attribute accepted a binding, so either " +
			"#488 is fixed — in which case empty silentlyBindable and delete " +
			"this arm — or the probe stopped reaching the loader")
	}
	for _, s := range silentlyBindable {
		if !seen[s] {
			t.Errorf("silentlyBindable names %s, which this run did not "+
				"reach at all — the entry is describing something the probe "+
				"no longer builds", s)
		}
	}
}

// silentlyBindable is the measured set of attributes that accept a
// binding and drop it: issue #488. Not a suppression — the test above
// fails BOTH ways, so an entry that gets fixed must be deleted and one
// that appears must be added deliberately.
//
// NINE OF THE ELEVEN ARE GONE, and they went the other way than this
// list expected. #470 swept `e.Attrs["X"] == "true"` and its integer
// twin into litBool/litInt, which make an unreadable value a LOAD ERROR
// — so ButtonBar.Gap, ButtonBar.Uniform, Gauge.BarWidth, HStack.Gap,
// ProgressBar.BarWidth, ProgressBar.Thresholds, Sparkline.BarWidth,
// Text.Bold and VStack.Gap now REFUSE `{{.S}}` rather than honouring
// it. Refusing is the better half of #488's "decide which": a binding
// the catalog says is not a binding should not load, and honouring it
// would have made BindsLiteral a lie in the other direction.
//
// The ones that remain are the ones that route through neither helper:
// TypeAhead.Key reads a rune, ButtonBar.Separator a string, and
// Companion.Log a string — none of which litBool or litInt can refuse,
// because every one of those spellings is a readable value. Deleting
// the nine is what the test above demands, and it is why it demands it:
// a note about a bug that is gone spends the attention that would find
// the next one.
//
// "DROP IT" IS TOO NARROW A NAME FOR WHAT THEY DO. Measured on
// ButtonBar.Separator, whose three states are visible on the row:
//
//	<ButtonBar>                    "[ a ][ b ]"
//	<ButtonBar Separator="x">      "[ a ] x [ b ]"
//	<ButtonBar Separator="{{.S}}"> "[ a ] { [ b ]"
//
// The binding is not honoured and not dropped — the template text is
// taken verbatim and then cut to the separator's one column, so the
// page gets a stray brace. Whether an attribute drops the value or
// paints a fragment of the template is a detail of the consumer; the
// defect this list tracks is the one thing they share, that a document
// the catalog says cannot bind is accepted as if it could.
var silentlyBindable = []string{
	"TypeAhead.Key",
	"ButtonBar.Separator",
	"Companion.Log",
}

// unseedable is every element probeElement cannot construct, with the
// reason: AttrSpec.Required does not match what the loader actually
// demands, so seeding "every required attribute" is not enough to build
// the element.
//
// That is a defect in its own right rather than a quirk of this test —
// the wysiwyg palette seeds an inserted element from exactly this data,
// so <FileWatcher/> dropped from a palette produces markup that will not
// load, naming an attribute the user was never offered. It is issue
// #489.
//
// IT IS EMPTY NOW, and that is a result rather than a deletion. The four
// entries — FileWatcher, Companion, KeyBinding, MenuBar — all build
// since this branch met main: #470's sweeps reached elements the
// defaults probe never did and seeded them, and the test above fails on
// an entry naming an element the probe now constructs, which is how
// these were found rather than guessed.
//
// The map stays, with its type and its Skip, because #489 is still open
// and the NEXT element to fall out of the probe should arrive as an
// entry here rather than as a silently unchecked element. An empty map
// makes the must-fire arm above range over nothing, which the arm
// itself reports.
var unseedable = map[string]string{}

// elementOf splits "Element.Attr" back to the element.
func elementOf(qualified string) string {
	for i := 0; i < len(qualified); i++ {
		if qualified[i] == '.' {
			return qualified[:i]
		}
	}
	return qualified
}

type attrProbe struct {
	el   string
	def  *ElementDef
	attr AttrSpec
}

// bindableAttrs and literalAttrs partition the builtin vocabulary by the
// Binds each attribute declares. Both walk BuiltinElements() rather than
// a list, so a new element joins whichever test its declaration puts it
// in, on the commit that declares it.
func bindableAttrs(t *testing.T) []attrProbe { return attrsWhere(t, false) }
func literalAttrs(t *testing.T) []attrProbe  { return attrsWhere(t, true) }

func attrsWhere(t *testing.T, literal bool) []attrProbe {
	t.Helper()
	var out []attrProbe
	for _, spec := range BuiltinElements() {
		def := elementDefs[spec.Name]
		if def == nil || def.Proto == nil {
			continue
		}
		for _, a := range def.Attrs {
			if (a.Binds == BindsLiteral) != literal {
				continue
			}
			if !literal && bindSpelling(a) == "" {
				// No handle of that shape is seeded in defaultsContext, so
				// this attribute cannot be probed. Reported rather than
				// skipped: an unchecked attribute nobody is told about is
				// the same as no test.
				t.Errorf("%s.%s is declared bindable (Kind %q, GoType %q) and "+
					"no placeholder handle exists for it, so it is unchecked "+
					"here and will stay unchecked", spec.Name, a.Name, a.Kind, a.GoType)
				continue
			}
			out = append(out, attrProbe{spec.Name, def, a})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].el != out[j].el {
			return out[i].el < out[j].el
		}
		return out[i].attr.Name < out[j].attr.Name
	})
	return out
}

// bindSpelling is the placeholder binding for an attribute, and it does
// NOT reuse bindingFor: that one switches on GoType alone and t.Fatals on
// an empty one, which is what most KindText attributes declare — Label,
// Prompt, Center. Fataling there would abort the whole run over a
// harness gap rather than reporting it.
//
// An empty string means "this probe cannot seed it", and the caller
// reports that rather than skipping quietly.
func bindSpelling(a AttrSpec) string {
	switch a.GoType {
	case "string":
		return "{{.S}}"
	case "int":
		if a.Name == "Value" {
			return "{{.Pct}}"
		}
		return "{{.I}}"
	case "bool":
		return "{{.B}}"
	case "[]float64":
		return "{{.F64}}"
	case "[]string":
		return "{{.SS}}"
	case "render.Color":
		return "{{.C}}"
	case "components.ItemSource":
		return "{{.IS}}"
	}
	switch a.Kind {
	case KindText, KindString:
		return "{{.S}}"
	case KindInt:
		return "{{.I}}"
	case KindBool:
		return "{{.B}}"
	case KindColor:
		return "{{.C}}"
	case KindCommand:
		return "{{.Noop}}"
	case KindStyle:
		return "{{.Sty}}"
	}
	if a.GoType == "image.Image" {
		return "{{.Img}}"
	}
	return ""
}

// bindsContext extends defaultsContext with the handles this file needs
// and that one does not: a bound STYLE and a bound IMAGE.
//
// It extends rather than edits, because defaultsContext is the fixture
// TestDeclaredDefaultsRenderIdenticallyToOmission renders against and a
// new key there is a new thing that could move a picture. Nothing here
// renders — these tests only ask whether markup LOADS — so the extra
// handles cost that test nothing by staying out of it.
func bindsContext() *Context {
	ctx := defaultsContext()
	ctx.Values["Sty"] = prop.NewSource(render.Style{Fg: render.RGB(10, 20, 30)})
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	ctx.Values["Img"] = prop.NewSource(image.Image(img))
	return ctx
}

// reachable reports whether the generic harness can build this element
// at all, with the attribute under test ABSENT. Some elements cannot —
// <TypeAhead> is an attachment that belongs on an <ItemsView>, so an
// <HStack> harness refuses it — and that is a limit of the probe, not a
// finding about Binds. Separating the two is what stops a harness gap
// being reported as a defect, and a defect being written off as a
// harness gap.
func reachable(t *testing.T, a attrProbe) bool {
	t.Helper()
	// EVERY required attribute seeded, including the one under test —
	// that is what the empty attr name does, since probeElement skips
	// seeding whichever attribute it is asked about. Passing the real
	// name instead asks a different and wrong question: for a REQUIRED
	// attribute it omits the very thing the element needs, so the build
	// fails and a perfectly reachable element reads as off-harness.
	// Measured: that spelling reported 23 unreachable attributes, of
	// which 21 were merely required.
	_, err := Build([]byte("<Gooey>"+bindsHarness(t, a, "", "")+"</Gooey>"), bindsContext())
	return err == nil
}

// bindsHarness places the element under test somewhere it can legally
// live, and the NON-VISUAL arm is why it exists rather than harnessFor
// alone.
//
// <TypeAhead>, <KeyBinding>, <Companion> and <FileWatcher> are
// ATTACHMENTS: they occupy no space and a container refuses them as
// visual children, so the generic <HStack> harness reported ten
// attributes as unreachable when the only thing wrong was where they
// were being put. NonVisual is a derived catalog fact, so this routes on
// the catalog rather than on a list of element names.
//
// An <ItemsView> is the host because it accepts attachments AND is what
// <TypeAhead> specifically requires; the others are happy anywhere that
// attaches.
func bindsHarness(t *testing.T, a attrProbe, attr, value string) string {
	t.Helper()
	el := probeElement(t, a.def, attr, value)
	if spec, ok := (&Context{}).spec(a.el); ok && spec.NonVisual {
		return `<ItemsView Items="{{.IS}}">` +
			`<ItemsView.ItemTemplate><Text>{{.Label}}</Text></ItemsView.ItemTemplate>` +
			el + `</ItemsView>`
	}
	return harnessFor(a.attr.Name, el)
}

// TestABoundValueActuallyArrives is what makes "silently bindable" a
// measurement rather than a label.
//
// Accepting a binding and honouring one are different claims, and only
// the second is visible on the cell plane. The discriminator is a THREE-
// WAY comparison: the attribute absent, set to a literal, and bound to a
// handle holding that same literal's value. A working attribute makes
// the second and third agree and both differ from the first; a dropped
// one makes the FIRST and third agree.
//
// DERIVED FROM silentlyBindable, and it was pinned to HStack.Gap until
// #470 landed. That is the churn worth designing against rather than
// re-pointing at: Gap stopped accepting a binding at all, so the
// hardcoded fixture did not report a fixed bug — it made Build return a
// load error inside a helper that Fatalf's on one, and the test died
// with a message about the wrong thing. Reading the list means the
// entries and their evidence move together.
//
// A FOURTH STATE EXISTS NOW and is handled rather than assumed away: an
// attribute may REFUSE the binding. That belongs to the test above,
// which names the entry to delete, so this one skips it instead of
// re-reporting it in a worse message.
func TestABoundValueActuallyArrives(t *testing.T) {
	shot := func(t *testing.T, src string) (*render.Buffer, error) {
		w, err := Build([]byte("<Gooey>"+src+"</Gooey>"), bindsContext())
		if err != nil {
			return nil, err
		}
		return gooey.Compose(w, term.Caps{Cols: defaultsCols, Rows: defaultsRows}, nil).Cells, nil
	}

	var observed int
	for _, a := range literalAttrs(t) {
		name := a.el + "." + a.attr.Name
		if !slices.Contains(silentlyBindable, name) || !reachable(t, a) {
			continue
		}

		absent, err := shot(t, bindsHarness(t, a, "", ""))
		if err != nil {
			t.Errorf("%s: the element does not build without the attribute "+
				"under test, so nothing here is measuring it: %v", name, err)
			continue
		}
		literal, err := shot(t, bindsHarness(t, a, a.attr.Name, literalFor(a.attr)))
		if err != nil {
			t.Errorf("%s: the element does not build with a LITERAL value, "+
				"which is the one spelling its spec promises: %v", name, err)
			continue
		}
		bound, err := shot(t, bindsHarness(t, a, a.attr.Name, "{{.S}}"))
		if err != nil {
			continue // refused: the test above owns this, and names it
		}

		// NON-VACUITY, PER ATTRIBUTE. If the literal paints exactly what
		// its absence paints, the comparison below cannot tell a dropped
		// binding from a working one — and the count at the end is what
		// stops every entry being skipped in silence.
		if _, _, differs := cellsDiffer(absent, literal); !differs {
			continue
		}
		observed++

		// THE CLAIM IS "NOT HONOURED", and asserting "dropped" was too
		// narrow. A bound BindsLiteral attribute may drop the value or
		// may take the template TEXT verbatim and render a fragment of
		// it — ButtonBar.Separator paints a bare "{" — and which one
		// happens is the consumer's business. What every entry here
		// shares is that the binding is accepted and its VALUE never
		// arrives, so that is what is asserted.
		if _, _, differs := cellsDiffer(literal, bound); !differs {
			t.Errorf("%s now HONOURS its binding — delete it from "+
				"silentlyBindable and from #488", name)
			continue
		}
		if _, _, dropped := cellsDiffer(absent, bound); !dropped {
			t.Logf("%s: dropped (bound renders as if absent)", name)
		} else {
			t.Logf("%s: accepted and mangled (bound renders as neither the "+
				"literal nor its absence — the template text reaches the "+
				"consumer)", name)
		}
	}

	if observed == 0 {
		t.Skipf("no entry in silentlyBindable (%v) paints differently with "+
			"its literal than without it, so the drop is not observable on "+
			"the cell plane for any of them. That is a gap in the FIXTURE, "+
			"not a pass: the claim is unmeasured", silentlyBindable)
	}
}

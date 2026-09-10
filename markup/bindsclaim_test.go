package markup

import (
	"image"
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
var silentlyBindable = []string{
	"TypeAhead.Key",
	"ButtonBar.Gap",
	"ButtonBar.Separator",
	"ButtonBar.Uniform",
	"Gauge.BarWidth",
	"HStack.Gap",
	"ProgressBar.BarWidth",
	"ProgressBar.Thresholds",
	"Sparkline.BarWidth",
	"Text.Bold",
	"VStack.Gap",
}

// unseedable is every element probeElement cannot construct, with the
// reason. All four are the SAME underlying gap and it is worth naming:
// AttrSpec.Required does not match what the loader actually demands, so
// seeding "every required attribute" is not enough to build the element.
//
// That is a defect in its own right rather than a quirk of this test —
// the wysiwyg palette seeds an inserted element from exactly this data,
// so <FileWatcher/> dropped from a palette produces markup that will not
// load, naming an attribute the user was never offered. It is issue
// #489, and these entries are its measured list.
var unseedable = map[string]string{
	"FileWatcher": "the loader needs Paths; the spec marks nothing Required",
	"Companion":   "the loader needs Path; the spec marks nothing Required",
	"KeyBinding":  "the loader takes Gesture, and Key is not an attribute at all",
	"MenuBar":     "Style is only reachable with a <Menu> child probeElement does not emit",
}

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
func TestABoundValueActuallyArrives(t *testing.T) {
	shot := func(src string) *render.Buffer {
		w, err := Build([]byte("<Gooey>"+src+"</Gooey>"), bindsContext())
		if err != nil {
			t.Fatalf("%s: %v", src, err)
		}
		return gooey.Compose(w, term.Caps{Cols: defaultsCols, Rows: defaultsRows}, nil).Cells
	}
	// defaultsContext binds I to 1, so the literal is "1".
	absent := shot(`<HStack><Text>a</Text><Text>b</Text></HStack>`)
	literal := shot(`<HStack Gap="1"><Text>a</Text><Text>b</Text></HStack>`)
	bound := shot(`<HStack Gap="{{.I}}"><Text>a</Text><Text>b</Text></HStack>`)

	if _, _, differs := cellsDiffer(absent, literal); !differs {
		t.Fatal("Gap=\"1\" renders like no Gap at all, so this test cannot " +
			"tell a dropped binding from a working one")
	}
	if _, _, differs := cellsDiffer(literal, bound); !differs {
		t.Log("HStack.Gap now honours its binding — delete it from " +
			"silentlyBindable and from #488")
		return
	}
	if _, _, differs := cellsDiffer(absent, bound); differs {
		t.Error("HStack.Gap bound to 1 renders like neither Gap=\"1\" nor no " +
			"Gap: the value is neither honoured nor dropped, which is a third " +
			"state this test does not model")
	}
}

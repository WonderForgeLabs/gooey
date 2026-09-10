package markup

import (
	"go/ast"
	"go/parser"
	gotoken "go/token"
	"sort"
	"testing"
	"testing/fstest"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/render"
	"github.com/WonderForgeLabs/gooey/validate"
)

// The control boundary is a PARTITION of Context, and issue #314 is what
// happens when it is maintained by hand.
//
// Everything a page registers either crosses into a control or is
// deliberately withheld, and both halves are contracts. Components,
// Handlers, Styles, Includes, Dispatcher and Declared crossed; Rules,
// Elements, Dir and Variant did not, and every one of those four was a
// silent wrong answer rather than an error:
//
//   - Rules — `<Validate Email="true"/>` inside a control failed naming
//     only the built-ins, against a doc that calls the registration
//     "exactly like Components and Handlers";
//   - Elements — the DECLARED spelling of a host element was unknown
//     inside a control while the undeclared Components spelling worked,
//     which is the incentive backwards;
//   - Dir — a <Companion> in a control resolved its paths against the
//     process working directory;
//   - Variant — worked at depth 1 and stopped at depth 2, because the
//     resolveVariant call reads parent.Variant and one level down the
//     parent IS the child context.
//
// #314 FOUND TWO OF THE FOUR, and that is the argument for this test
// rather than for four more lines in the block. The issue lists Rules
// and Dir because those are what somebody hit; Elements and Variant sat
// beside them, in the same block, with the same shape. A list of fields
// somebody noticed is not the set.
//
// So the set is DERIVED — from the struct declaration, by parsing it —
// and the partition below has to account for every exported field. Add a
// field to Context and this test fails until you say which side of the
// boundary it is on.

// boundaryPartition is the CONTRACT, and the only hand-maintained thing
// here. Each field is inherit or isolate, with the reason, and the test
// checks the claim behaviourally rather than taking it.
var boundaryPartition = map[string]struct {
	inherit bool
	why     string
}{
	"Styles":     {true, "a theme is ambient: a control paints in the app's style table"},
	"Components": {true, "a host builder registration is app-wide"},
	"Elements":   {true, "the declared form of the same registration, and it must not be weaker"},
	"Handlers":   {true, "a code-behind name resolves the same everywhere"},
	"Rules":      {true, "documented as a registration exactly like Components and Handlers"},
	"Declared":   {true, "the declared-surface registry is page-wide by construction"},
	"Includes":   {true, "a control may instantiate another control"},
	"Dispatcher": {true, "there is one UI goroutine, so there is one dispatcher"},
	"Dir":        {true, "the document directory <Companion> resolves against"},
	"Variant":    {true, "the pixel protocol is a property of the app, not of one file"},

	"Values": {false, "VALUES ISOLATE — the whole point of the boundary. They " +
		"cross only through the declared surface (<x:Property>), which is " +
		"what makes a control a contract rather than a macro"},
	"Named": {false, "Name is the page's ADDRESS BOOK. A control's internals " +
		"are not page-addressable, or PatchMarkup and markup.Find would " +
		"reach inside a control and two instances of it would collide"},
}

// TestTheControlBoundaryPartitionsEveryContextField is the derived half:
// the partition above must account for exactly the exported fields
// Context declares, no more and no fewer.
//
// AST, not reflection — CLAUDE.md's first invariant is that core carries
// none, and a test that imported it to read a struct would be the first
// exception in the package. The declaration is right there in the source
// and parsing it costs nothing.
func TestTheControlBoundaryPartitionsEveryContextField(t *testing.T) {
	declared := contextFields(t)
	for _, name := range declared {
		if _, ok := boundaryPartition[name]; !ok {
			t.Errorf("Context.%s is not in boundaryPartition: every field a "+
				"page can set either crosses into a control or is "+
				"deliberately withheld, and which one it is has to be "+
				"decided rather than defaulted. Add it with the reason, "+
				"then let the behavioural test below check the claim", name)
		}
	}
	have := map[string]bool{}
	for _, n := range declared {
		have[n] = true
	}
	for name := range boundaryPartition {
		if !have[name] {
			t.Errorf("boundaryPartition names %s, which Context no longer "+
				"declares — a rule about a field that is gone", name)
		}
	}
}

// TestEveryInheritedRegistrationReachesAControl is the behavioural half,
// and it is the one that would have caught all four.
//
// It sets every field on the page's context, instantiates a control, and
// reads the context the control's own children are built with — which is
// the child context itself, reached through a registered builder rather
// than inferred from an error message. An error-message probe answers
// "did this particular markup load", which is a different and weaker
// question: Elements failed with `unknown element`, Rules with `unknown
// rule`, and Dir with nothing at all.
//
// THE SWITCH IS EXHAUSTIVE BY CONSTRUCTION. It has to name fields one at
// a time — without reflection there is no other way to read them — but
// the test above pins the partition against the struct, and the default
// arm fails on a field nobody wired up here. So forgetting one is a red
// test rather than a quiet gap, which is the property #314 shows the
// hand-maintained block did not have.
func TestEveryInheritedRegistrationReachesAControl(t *testing.T) {
	fsys := fstest.MapFS{
		"page.gooey": {Data: []byte(`<Gooey><Card/></Gooey>`)},
		"card.gooey": {Data: []byte(`<Gooey><Probe/></Gooey>`)},
	}
	var child *Context
	page := &Context{
		Includes:   fsys,
		Dir:        "/tmp/anchor",
		Variant:    "sixel",
		Values:     map[string]any{"N": 1},
		Named:      map[string]gooey.Component{"PageOnly": &components.Text{}},
		Declared:   map[gooey.Component]DeclaredSurface{},
		Styles:     map[string]render.Style{"s": {}},
		Handlers:   map[string]gooey.Action{"H": gooey.Command(func() {})},
		Elements:   map[string]*ElementDef{"Meter": meterDef()},
		Dispatcher: gooey.NewDispatcher(),
		Rules: map[string]RuleFunc{
			"Zonk": func(string) (validate.Rule[string], error) { return nil, nil },
		},
		Components: map[string]Builder{
			"Probe": func(e Element, c *Context) (gooey.Component, error) {
				child = c
				return &components.Text{}, nil
			},
		},
	}
	if _, err := Load(fsys, "page.gooey", page); err != nil {
		t.Fatalf("the page did not load, so nothing below was observed: %v", err)
	}
	if child == nil {
		t.Fatal("the probe builder never ran, so no child context was reached")
	}

	for _, name := range contextFields(t) {
		rule, ok := boundaryPartition[name]
		if !ok {
			continue // the test above reports this
		}
		var crossed bool
		switch name {
		case "Styles":
			crossed = child.Styles != nil
		case "Components":
			crossed = child.Components != nil
		case "Elements":
			crossed = child.Elements != nil
		case "Handlers":
			crossed = child.Handlers != nil
		case "Rules":
			crossed = child.Rules != nil
		case "Declared":
			crossed = child.Declared != nil
		case "Includes":
			crossed = child.Includes != nil
		case "Dispatcher":
			crossed = child.Dispatcher != nil
		case "Dir":
			crossed = child.Dir == page.Dir
		case "Variant":
			crossed = child.Variant == page.Variant
		case "Values":
			// The declared surface is the only road, and this control
			// declares nothing, so the page's N must not be visible.
			_, crossed = child.Values["N"]
		case "Named":
			// A SENTINEL, not a nil check. The child gets its own map —
			// named() makes one lazily — so `!= nil` reads as "crossed"
			// for a correctly isolated control. The contract is that the
			// PAGE's entries are not visible from inside, which is what
			// the sentinel asks. Measured: the nil check reported Named
			// crossing, against a boundary that was working.
			_, crossed = child.Named["PageOnly"]
		default:
			t.Errorf("Context.%s is partitioned but this switch does not read "+
				"it, so its half of the contract is unchecked", name)
			continue
		}
		if crossed != rule.inherit {
			verb := "did not cross into the control"
			if crossed {
				verb = "crossed into the control and must not have"
			}
			t.Errorf("Context.%s %s — %s", name, verb, rule.why)
		}
	}
}

// TestTheBoundaryProbeCanActuallySeeAFailure is the must-fire arm.
//
// The test above compares a computed bool against a declared one and
// reports nothing when they agree — a negative assertion, which passes
// for any reason, including a probe that never observed anything. It
// already fails loudly on a nil child, so what is left to prove is that
// the page's context really did carry every field into the comparison:
// a field left zero on the page would read as "did not cross" for a
// correct implementation, and a field the page set to the same zero the
// child has would read as "crossed" for a broken one.
func TestTheBoundaryProbeCanActuallySeeAFailure(t *testing.T) {
	fsys := fstest.MapFS{
		"page.gooey": {Data: []byte(`<Gooey><Card/></Gooey>`)},
		"card.gooey": {Data: []byte(`<Gooey><Probe/></Gooey>`)},
	}
	var child *Context
	page := &Context{
		Includes: fsys,
		Components: map[string]Builder{
			"Probe": func(e Element, c *Context) (gooey.Component, error) {
				child = c
				return &components.Text{}, nil
			},
		},
	}
	if _, err := Load(fsys, "page.gooey", page); err != nil {
		t.Fatalf("the page did not load: %v", err)
	}
	if child == nil {
		t.Fatal("the probe builder never ran")
	}
	// With Rules unset on the page, the inheriting arm must read FALSE.
	// If it reads true the probe is looking at something other than the
	// child — the page itself, most likely — and every "crossed" verdict
	// above is meaningless.
	if child.Rules != nil {
		t.Error("the child reports an inherited Rules map the page never " +
			"set, so the probe is not observing the child context")
	}
	if child.Components == nil {
		t.Error("the child reports no Components, but the probe builder that " +
			"set it came from exactly that map — the observation is not of a " +
			"real child context")
	}
}

// contextFields parses Context's exported field names out of the source.
func contextFields(t *testing.T) []string {
	t.Helper()
	file, err := parser.ParseFile(gotoken.NewFileSet(), "markup.go", nil, 0)
	if err != nil {
		t.Fatalf("markup.go does not parse: %v", err)
	}
	var out []string
	ast.Inspect(file, func(n ast.Node) bool {
		ts, ok := n.(*ast.TypeSpec)
		if !ok || ts.Name.Name != "Context" {
			return true
		}
		st, ok := ts.Type.(*ast.StructType)
		if !ok {
			return false
		}
		for _, f := range st.Fields.List {
			for _, nm := range f.Names {
				if nm.IsExported() {
					out = append(out, nm.Name)
				}
			}
		}
		return false
	})
	if len(out) == 0 {
		t.Fatal("no exported field was found on Context, so every loop over " +
			"this list would pass over nothing")
	}
	sort.Strings(out)
	return out
}

// TestVariantResolutionSurvivesNesting is #314's fourth defect on its
// own, because the partition test cannot see it.
//
// child.Variant being empty was HARMLESS AT DEPTH 1 — resolveVariant
// reads parent.Variant, and for a page-level control the parent is the
// page. One level down the parent is the child context, so the file
// choice silently fell back to the unspecialized name. A field-level
// check on the child would have flagged it, but only because the field
// happened to be readable; the thing that actually broke is which FILE
// was loaded, and that is what this asserts.
func TestVariantResolutionSurvivesNesting(t *testing.T) {
	base := map[string]string{
		"outer.gooey":       `<Gooey><Inner/></Gooey>`,
		"outer.sixel.gooey": `<Gooey><Inner/></Gooey>`,
		"inner.gooey":       `<Gooey><Text>PLAIN</Text></Gooey>`,
		"inner.sixel.gooey": `<Gooey><Text>SIXEL</Text></Gooey>`,
	}
	for _, tc := range []struct{ name, page string }{
		{"page to control", `<Gooey><Inner/></Gooey>`},
		{"page to control to control", `<Gooey><Outer/></Gooey>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fsys := fstest.MapFS{}
			for k, v := range base {
				fsys[k] = &fstest.MapFile{Data: []byte(v)}
			}
			fsys["page.gooey"] = &fstest.MapFile{Data: []byte(tc.page)}
			ctx := &Context{Includes: fsys, Variant: "sixel"}
			root, err := Load(fsys, "page.gooey", ctx)
			if err != nil {
				t.Fatalf("did not load: %v", err)
			}
			got := firstText(root)
			if got == "" {
				t.Fatal("no <Text> was built, so this test read nothing")
			}
			if got != "SIXEL" {
				t.Errorf("the control resolved to %q — the app asked for the "+
					"sixel variant and got the unspecialized file, with no "+
					"error anywhere. Variant is read off the parent, and one "+
					"level down the parent is the child context", got)
			}
		})
	}
}

func firstText(c gooey.Component) string {
	if tx, ok := c.(*components.Text); ok && tx.Content != nil {
		return tx.Content.Get()
	}
	ct, ok := c.(gooey.Container)
	if !ok {
		return ""
	}
	for _, k := range ct.ChildComponents() {
		if s := firstText(k); s != "" {
			return s
		}
	}
	return ""
}

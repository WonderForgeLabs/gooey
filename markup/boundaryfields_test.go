package markup

import (
	"go/ast"
	"go/parser"
	gotoken "go/token"
	"io/fs"
	"sort"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/prop"
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

	// THE UNEXPORTED HALF, and leaving it out was not a scoping choice —
	// it was the filter in contextFields quietly deciding the contract.
	// Every one of these six is DECIDED at this boundary
	// (usercontrol.go), and the class has a track record: `arms` is
	// #459's fix, whose doc comment had PROMISED it crossed while the
	// code did not. That is the same sentence-versus-behaviour gap this
	// file exists to close, and the guard as first written would not
	// have caught it. Raised in review of #490.
	"arms": {true, "the <Frozen AllowError> scope is page-wide per build: a " +
		"control that arrived with its own must not opt out of the page's set"},
	"res": {true, "the resource chain is lexical, and a control's markup " +
		"resolves Style= against the document scope it was instantiated in"},
	"controls": {true, "the ancestry EXTENDS rather than resets — a control " +
		"appearing twice in it is the load-time cycle check"},

	"fsys": {false, "REPLACED, not inherited: a control's literal asset paths " +
		"resolve against the FS its OWN markup came from, the same isolation " +
		"its bindings get"},
	"ns": {false, "the xmlns table is per-DOCUMENT — an included file cannot " +
		"borrow a prefix the page happened to declare"},
	"declared": {false, "the dependency properties of the control being " +
		"instantiated, installed for the duration of one setup call"},
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
	// TWO FILE SYSTEMS, because one cannot tell `fsys` apart from
	// Includes: the page is loaded from pageFS and the control resolves
	// out of ctlFS, so "the FS this control's own markup came from" is a
	// question with a different answer from "the page's".
	//
	// The page also declares a RESOURCE SCOPE and an XMLNS PREFIX. Both
	// exist for the unexported arms below: `res` is only non-nil in the
	// child because it crossed, and `ns` is only interesting if the page
	// has a prefix for the child to fail to inherit. A fixture that
	// leaves a field zero on the page reads "did not cross" for a
	// correct implementation and proves nothing — which is the argument
	// TestTheBoundaryProbeCanActuallySeeAFailure already makes.
	pageFS := fstest.MapFS{
		"page.gooey": {Data: []byte(`<Gooey xmlns:probe="urn:boundary-probe">
  <Gooey.Resources>
    <Style Key="pageRes" Fg="#ffaa3c"/>
  </Gooey.Resources>
  <Card/>
</Gooey>`)},
	}
	ctlFS := fstest.MapFS{
		"card.gooey": {Data: []byte(`<Gooey><Probe/></Gooey>`)},
	}
	// A SENTINEL ENTRY, not an empty map. Declared's own doc calls it
	// page-wide — "child contexts inherit the same map, so nested
	// control instances are visible from the context the page was built
	// against" — which is what lets a tree snapshot report a nested
	// control's surface. `child.Declared != nil` cannot see that
	// promise break: swap the assignment for a fresh empty map and the
	// arm still reads "crossed" while the registry silently becomes
	// per-control. Raised in review of #490.
	declSentinel := &components.Text{}
	var child *Context
	page := &Context{
		Includes:   ctlFS,
		Dir:        "/tmp/anchor",
		Variant:    "sixel",
		Values:     map[string]any{"N": 1},
		Named:      map[string]gooey.Component{"PageOnly": &components.Text{}},
		Declared:   map[gooey.Component]DeclaredSurface{declSentinel: {Control: "PageOnly"}},
		Styles:     map[string]render.Style{"s": {}},
		Handlers:   map[string]gooey.Action{"H": gooey.Command(func() {})},
		Elements:   map[string]*ElementDef{"Meter": meterDef()},
		Dispatcher: gooey.NewDispatcher(),
		// THE LAST INERT ARM. declared was the one unexported field left
		// on `!= nil` with nothing behind it: the fixture never set it,
		// so the arm read false whatever control() did. Measured —
		// adding `child.declared = parent.declared` to control() and the
		// test still PASSED, which is the exact failure the rest of this
		// fixture exists to remove. The leak it could not see is real:
		// runSetup saves and restores declared so a setup that itself
		// instantiates a control cannot see the wrong declarations, and
		// a crossing declared breaks that. Raised in review of #490.
		declared: map[string]any{"PageDecl": nil},
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
	if _, err := Load(pageFS, "page.gooey", page); err != nil {
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
		// EVERY ARM ASKS FOR THE PAGE'S OWN VALUE, never merely whether
		// something is there. Eight of these read `!= nil` until review
		// of #490, which answers "is there a map here" rather than "is it
		// the page's map" — so replacing an inherited registry with a
		// fresh empty one of the same type read as "crossed" and the
		// contract broke silently. The PR had already found that
		// reasoning wrong for Named and stopped at the one field where
		// it visibly misfired. Every fixture key below is a sentinel the
		// page set and nothing else could have produced.
		var crossed bool
		switch name {
		case "Styles":
			_, crossed = child.Styles["s"]
		case "Components":
			_, crossed = child.Components["Probe"]
		case "Elements":
			_, crossed = child.Elements["Meter"]
		case "Handlers":
			_, crossed = child.Handlers["H"]
		case "Rules":
			_, crossed = child.Rules["Zonk"]
		case "Declared":
			_, crossed = child.Declared[declSentinel]
		case "Includes":
			// The FS itself, asked a question only the page's answers —
			// behind a nil check, because dropping the propagation
			// leaves a nil interface and fs.ReadFile PANICS on one. A
			// panic takes the rest of the package's run with it, which
			// is a worse answer than the report this arm was written to
			// give. Raised in review of #490.
			if child.Includes != nil {
				_, err := fs.ReadFile(child.Includes, "card.gooey")
				crossed = err == nil
			}
		case "Dispatcher":
			// A POINTER COMPARE, which is available here and is the
			// whole claim: there is one UI goroutine, so a control
			// posting to a different dispatcher is the defect.
			crossed = child.Dispatcher == page.Dispatcher
		case "arms":
			// Non-nil IS the sentinel here, and for a reason worth
			// stating: a child Context is constructed fresh at this
			// boundary, so its armScope is zero unless the assignment
			// ran. The page's set exists because Load made one.
			crossed = child.arms.sinks != nil
		case "res":
			// Same shape: the page declares <Gooey.Resources>, so a
			// non-nil scope in the child can only have come across.
			crossed = child.res.cur != nil
		case "controls":
			// It EXTENDS rather than copies, so the claim is that the
			// ancestry names the control being built. The entries are
			// FILE names, not element names — <Card/> resolves through
			// the Includes convention to card.gooey, and the cycle check
			// is about which document is already on the stack.
			crossed = len(child.controls) > 0 &&
				child.controls[len(child.controls)-1] == "card.gooey"
		case "fsys":
			// Not "is it set" but "is it the CONTROL's": the page's own
			// file must not be readable through it.
			_, viaCtl := fs.ReadFile(child.fsys, "card.gooey")
			_, viaPage := fs.ReadFile(child.fsys, "page.gooey")
			crossed = viaCtl != nil || viaPage == nil
		case "ns":
			// The page declares xmlns:probe; a control's document
			// declares its own namespaces or has none.
			_, crossed = child.ns["probe"]
		case "declared":
			_, crossed = child.declared["PageDecl"]
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

// contextFields parses Context's field names out of the source — ALL of
// them, exported or not.
//
// It filtered on nm.IsExported() until review of #490, which put six
// fields outside the contract: fsys, arms, ns, declared, res and
// controls. Every one is decided at this boundary, and `arms` is the
// case with a record — it is #459's fix, and its own doc comment had
// PROMISED it crossed while the code did not, which is exactly the gap
// this file was written to close. A filter that drops the fields most
// likely to rot is the guard choosing not to look where the bug was.
//
// Reading them is legal because this test is IN package markup; the
// switch below names each one directly, no reflection.
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
			out = append(out, fieldNames(f)...)
		}
		return false
	})
	if len(out) == 0 {
		t.Fatal("no field was found on Context, so every loop over this list " +
			"would pass over nothing. NOT \"no exported field\": this walk " +
			"stopped filtering on IsExported when the unexported half of the " +
			"boundary came under the partition, and a message naming the old " +
			"filter is the strongest comment in the file pointing at the wrong " +
			"rule")
	}
	sort.Strings(out)
	return out
}

// fieldNames returns the declared names of one struct field, which is a
// list rather than a name because `a, b int` is one ast.Field. An
// EMBEDDED field has none, and is skipped: Context declares none today,
// and one added later would need its own decision here rather than a
// name invented from its type.
func fieldNames(f *ast.Field) []string {
	out := make([]string, 0, len(f.Names))
	for _, nm := range f.Names {
		out = append(out, nm.Name)
	}
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

// TestEveryInheritedRegistrationReachesATemplateRow is the same contract
// at the seam the partition forgot it had.
//
// boundaryPartition and docs/markup-reference.md both state the rule
// globally — "everything a page registers inherits". control() honours
// it. The ITEM-TEMPLATE row context did not: markup/itemsview.go built
// its own literal, copied ten fields and dropped six, so a row was a
// control boundary nobody had declared, with a different partition and
// no statement of it anywhere.
//
// Two of the six cost more than a missing convenience. Elements is the
// DECLARED vocabulary, so <Meter Level="{{.N}}"/> in a template failed
// with `unknown element <Meter>` while the undeclared Components
// spelling worked — the incentive backwards, which is the defect #314
// exists to remove, reproduced one seam over from the one it fixed. And
// controls is the cycle ancestry: dropping it RESET it, so a control
// whose template instantiates itself recursed to `fatal error: stack
// overflow` rather than the load error indexOf(parent.controls, name)
// exists to give — and a fatal skips Screen.Restore.
//
// NOT A COPY OF THE SWITCH ABOVE, because a row is not a control and
// three fields diverge for reasons of their own: Values IS the item,
// Named is row-scoped (names are unique per document, not per row), and
// arms is constructed member by member with four separate arguments
// recorded in itemsview.go. What this asserts is the six that had no
// reason at all. Raised in review of #490.
func TestEveryInheritedRegistrationReachesATemplateRow(t *testing.T) {
	declSentinel := &components.Text{}
	var row *Context
	page := &Context{
		Dir:      "/tmp/anchor",
		Variant:  "sixel",
		Declared: map[gooey.Component]DeclaredSurface{declSentinel: {Control: "PageOnly"}},
		Elements: map[string]*ElementDef{"Meter": meterDef()},
		Rules: map[string]RuleFunc{
			"Zonk": func(string) (validate.Rule[string], error) { return nil, nil },
		},
		Values: map[string]any{
			"Items": components.Items(prop.NewSource([]string{"a"}),
				func(s string) map[string]any { return map[string]any{"S": s, "N": 1} }),
		},
		Components: map[string]Builder{
			"Probe": func(e Element, c *Context) (gooey.Component, error) {
				row = c
				return &components.Text{}, nil
			},
		},
	}
	// The ancestry is normally pushed by control(); there is no control
	// here, so it is set directly — the question is whether the ROW
	// keeps it, not how it got onto the page.
	page.controls = []string{"page.gooey"}

	src := `<Gooey xmlns="wonderforge.io/gooey/2026">` +
		`<ItemsView Items="{{.Items}}">` +
		`<ItemsView.ItemTemplate><Probe/></ItemsView.ItemTemplate>` +
		`</ItemsView></Gooey>`
	if _, err := Build([]byte(src), page); err != nil {
		t.Fatalf("the page did not load, so nothing below was observed: %v", err)
	}
	if row == nil {
		t.Fatal("the probe builder never ran, so no row context was reached. " +
			"ItemsView.Validate realizes one throwaway row during the build; " +
			"if that stopped happening this test sees nothing")
	}

	for _, c := range []struct {
		field   string
		crossed bool
	}{
		{"Elements", func() bool { _, ok := row.Elements["Meter"]; return ok }()},
		{"Rules", func() bool { _, ok := row.Rules["Zonk"]; return ok }()},
		{"Declared", func() bool { _, ok := row.Declared[declSentinel]; return ok }()},
		{"Dir", row.Dir == page.Dir},
		{"Variant", row.Variant == page.Variant},
		{"controls", len(row.controls) > 0 && row.controls[len(row.controls)-1] == "page.gooey"},
	} {
		if !c.crossed {
			t.Errorf("Context.%s did not reach an item-template row. A row is not "+
				"a boundary — boundaryPartition says this inherits, and the only "+
				"fields itemsview.go may scope to a row are Named and arms, each "+
				"with its reason written beside it", c.field)
		}
	}
}

// TestADeclaredElementWorksInsideARow is the symptom, and it is the one
// a user reports.
//
// The test above reads the row's context, which is the strong form. This
// is the weak form kept deliberately: <Meter> is registered in Elements
// and nowhere else, so a row that cannot see Elements answers `unknown
// element <Meter>` — the same message #314 was filed for, from the seam
// that PR did not reach. An error-message probe alone would be a worse
// test; beside the context read it is what ties the contract to the
// complaint. Raised in review of #490.
func TestADeclaredElementWorksInsideARow(t *testing.T) {
	ctx := &Context{
		Elements: map[string]*ElementDef{"Meter": meterDef()},
		Values: map[string]any{
			"Items": components.Items(prop.NewSource([]string{"a"}),
				func(s string) map[string]any { return map[string]any{"N": 1} }),
		},
	}
	src := `<Gooey xmlns="wonderforge.io/gooey/2026">` +
		`<ItemsView Items="{{.Items}}">` +
		`<ItemsView.ItemTemplate><Meter Level="{{.N}}"/></ItemsView.ItemTemplate>` +
		`</ItemsView></Gooey>`
	if _, err := Build([]byte(src), ctx); err != nil {
		t.Fatalf("a DECLARED element was not usable inside an item template, "+
			"while the same component registered under the undeclared "+
			"Components spelling is: %v", err)
	}
}

// TestARowCannotResetTheCycleAncestry is the sharp half of the row seam,
// and the difference between a load error and a fatal.
//
// controls is the ancestry indexOf(parent.controls, name) reads to turn
// "a control includes itself" into a load error naming the loop — the
// #216 crash, caught. The item-template row context did not copy it, so
// a row RESET it: every row started a fresh ancestry and the check could
// not see across one.
//
// Both directions measured on this fixture, which is a passthrough
// control whose own template instantiates it, fed a projection that
// supplies itself at every depth:
//
//	with the propagation:    markup: control loop.gooey includes itself
//	without it:              fatal error: stack overflow
//
// The second is not merely worse, it is unreportable: a Go fatal is not
// a panic, so nothing recovers it and Screen.Restore never runs — the
// terminal is left in raw mode with the alternate screen up. That is the
// whole reason the load-time check exists. Raised in review of #490.
func TestARowCannotResetTheCycleAncestry(t *testing.T) {
	ctlFS := fstest.MapFS{
		"loop.gooey": {Data: []byte(`<Gooey xmlns="wonderforge.io/gooey/2026">` +
			`<ItemsView Items="{{.Items}}">` +
			`<ItemsView.ItemTemplate><Loop Items="{{.Items}}"/></ItemsView.ItemTemplate>` +
			`</ItemsView></Gooey>`)},
	}
	// SELF-SUPPLYING, and it has to be. A projection that stops handing
	// down an item source ends the recursion for a reason that has
	// nothing to do with the ancestry — the build fails at depth two with
	// `"Items" not found in context` and the test agrees with the bug.
	var proj func(string) map[string]any
	proj = func(string) map[string]any {
		return map[string]any{"Items": components.Items(prop.NewSource([]string{"a"}), proj)}
	}
	ctx := &Context{
		Includes: ctlFS,
		Values:   map[string]any{"Items": components.Items(prop.NewSource([]string{"a"}), proj)},
	}

	_, err := Build([]byte(`<Gooey xmlns="wonderforge.io/gooey/2026">`+
		`<Loop Items="{{.Items}}"/></Gooey>`), ctx)
	if err == nil {
		t.Fatal("a control whose item template instantiates itself built cleanly")
	}
	if !strings.Contains(err.Error(), "includes itself") {
		t.Errorf("a control whose item template instantiates itself failed for "+
			"some other reason than the cycle check, so this test is not "+
			"reaching it: %v", err)
	}
}

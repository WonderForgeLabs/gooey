package markup

import (
	"bytes"
	"go/ast"
	"go/parser"
	gotoken "go/token"
	"image"
	gopng "image/png"
	"io/fs"
	"path/filepath"
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

// rowPartition is the SAME question at the other seam, and it exists
// because that seam's answer was a six-entry literal in a test while
// this one was a table over every field.
//
// The asymmetry is not academic. It is what left `Elements` and
// `Variant` out of the row context in the first place — the defect the
// previous round of this PR fixed — and then, one round later, left
// `fsys` and `declared` scoped to the row with no reason written
// anywhere, while the test's own error message asserted that "the only
// fields itemsview.go may scope to a row are Named and arms". Sixteen of
// eighteen accounted for and two decided by omission is the shape this
// file exists to remove. Raised in review of #490.
//
// A ROW IS NOT A BOUNDARY, which is why most of this is `true`: the row
// is the same document, so the default is "inherits" and every `false`
// owes a reason. The control partition's defaults run the other way for
// the fields that make a control a contract.
var rowPartition = map[string]struct {
	inherit bool
	why     string
}{
	"Styles":     {true, "same document, same theme"},
	"Components": {true, "a builder registration is app-wide"},
	"Elements": {true, "the DECLARED vocabulary: without it <Meter> in a " +
		"template was unknown while the undeclared spelling worked"},
	"Handlers":   {true, "a code-behind name resolves the same in a row"},
	"Rules":      {true, "a validation rule is a registration like the rest"},
	"Includes":   {true, "a template may instantiate a control"},
	"Dispatcher": {true, "one UI goroutine, one dispatcher"},
	"Dir":        {true, "the row's markup is in the same document directory"},
	"Variant":    {true, "the pixel protocol is a property of the app"},
	"controls": {true, "the cycle ancestry: resetting it turned the #216 " +
		"load error back into a stack overflow"},
	"res": {true, "the resource chain is lexical and the row is lexically " +
		"inside the document"},
	"fsys": {true, "a row's markup CAME FROM the document's FS, so a literal " +
		"<Image Src> must resolve the same inside a template as outside one"},
	"ns": {true, "the xmlns table is per-DOCUMENT, and a template is part of " +
		"the document that declared the prefixes — the opposite answer from " +
		"the control seam, where an included file cannot borrow the page's. " +
		"Found by this table rather than written into it: the walk reported " +
		"ns unaccounted for on its first run"},

	"Values": {false, "the row's Values ARE the item — that is what a template is"},
	"Named": {false, "uniqueness is per DOCUMENT, and a scrolling list would " +
		"collide with itself"},
	"arms": {false, "CONSTRUCTED member by member rather than inherited: sinks " +
		"and allows are row-local, outer and nested are the page's, and " +
		"pending is the row's own. Four members, four reasons, in itemsview.go"},
	"declared": {false, "the dependency properties of the control being " +
		"instantiated, installed for the duration of one runSetup call. A row " +
		"is not that call, and the save/restore exists so a nested " +
		"instantiation cannot see the wrong declarations"},
	"Declared": {false, "TAKEN BACK after one round of inheriting it. " +
		"usercontrol.go writes parent.Declared[w] per declaring control, this " +
		"factory runs per row realization and never unregisters, and nothing " +
		"sweeps retired rows — so a scrolling list pinned one entry and one " +
		"row subtree per row ever shown. Page-wide visibility of a row's " +
		"declared surface is a real goal and cannot be bought with unbounded " +
		"retention"},
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
			//
			// Behind a nil check, because dropping `child.fsys = fsys`
			// from usercontrol.go leaves a nil interface and fs.ReadFile
			// PANICS on one — taking the rest of the package's run with
			// it instead of giving the report this arm exists for. Its
			// two siblings (Includes above, the row's fsys below) already
			// guard it; this one did not. Raised in review of #490.
			//
			// AND THE NIL CASE IS ITS OWN FAULT, not "did not cross".
			// The guard alone would turn the panic into a SILENT PASS:
			// the partition says fsys must not inherit, so `crossed =
			// false` is the expected answer and a control handed no FS
			// at all satisfies it. Measured — with only the guard,
			// dropping `child.fsys = fsys` left this test green. A
			// control always gets an FS; which one is the question the
			// arm below asks.
			if child.fsys == nil {
				t.Errorf("the control context has NO file system at all, so its " +
					"markup could not resolve an <Image Src> of its own. This is " +
					"not the same as \"the page's did not cross\" — usercontrol.go " +
					"must REPLACE fsys, and dropping the assignment reads as " +
					"withheld to a partition that only asks whether the page's " +
					"value arrived")
				continue
			}
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
// IT WALKS THE PACKAGE rather than naming markup.go. Pinning the
// filename meant that moving Context to another file in this package
// tripped the len(out) == 0 fatal below, whose message diagnoses the
// removed IsExported filter — a red test pointing at the wrong cause,
// which costs more than the walk does. Raised in review of #490.
func contextFields(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("globbing this package: %v", err)
	}
	var out []string
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(gotoken.NewFileSet(), f, nil, 0)
		if err != nil {
			t.Fatalf("%s does not parse: %v", f, err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			ts, ok := n.(*ast.TypeSpec)
			if !ok || ts.Name.Name != "Context" {
				return true
			}
			st, ok := ts.Type.(*ast.StructType)
			if !ok {
				return false
			}
			for _, fl := range st.Fields.List {
				out = append(out, fieldNames(fl)...)
			}
			return false
		})
		if len(out) > 0 {
			break
		}
	}
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
	// LOADED FROM AN FS, not built from bytes, because fsys is one of the
	// fields under test and Build leaves it nil — which would read as
	// "did not cross" for a correct implementation and prove nothing.
	// The page also declares a RESOURCE SCOPE and an XMLNS PREFIX for the
	// unexported arms, exactly as the control fixture next door does.
	pageFS := fstest.MapFS{
		"page.gooey": {Data: []byte(`<Gooey xmlns:probe="urn:boundary-probe">
  <Gooey.Resources>
    <Style Key="pageRes" Fg="#ffaa3c"/>
  </Gooey.Resources>
  <VStack>
    <PageProbe/>
    <ItemsView Items="{{.Items}}">
      <ItemsView.ItemTemplate><Probe/></ItemsView.ItemTemplate>
    </ItemsView>
  </VStack>
</Gooey>`)},
	}
	ctlFS := fstest.MapFS{"card.gooey": {Data: []byte(`<Gooey><Text>x</Text></Gooey>`)}}

	declSentinel := &components.Text{}
	// THE ARMS SENTINEL, written into the page's sink map WHILE IT IS
	// LIVE by a page-level probe that builds before the <ItemsView> does.
	//
	// Reading page.arms after Load cannot answer this: document.build
	// restores the whole arm scope in its defer (markup.go:781), so
	// page.arms.sinks is nil by the time the switch runs, and the arm's
	// old form — `len(page.arms.sinks) > 0 && sameSinks(...)` — was
	// therefore false whatever itemsview.go did. Measured in review of
	// #490: giving the row the page's own map, exactly what this arm
	// says must not happen, left the test PASSING.
	armsSentinel := prop.NewSource("")
	var row *Context
	page := &Context{
		Dir:      "/tmp/anchor",
		Variant:  "sixel",
		Includes: ctlFS,
		Styles:   map[string]render.Style{"s": {}},
		Handlers: map[string]gooey.Action{"H": gooey.Command(func() {})},
		Named:    map[string]gooey.Component{"PageOnly": &components.Text{}},
		Declared: map[gooey.Component]DeclaredSurface{declSentinel: {Control: "PageOnly"}},
		Elements: map[string]*ElementDef{"Meter": meterDef()},
		Rules: map[string]RuleFunc{
			"Zonk": func(string) (validate.Rule[string], error) { return nil, nil },
		},
		Values: map[string]any{
			"PageOnly": prop.NewSource("page"),
			"Items": components.Items(prop.NewSource([]string{"a"}),
				func(s string) map[string]any { return map[string]any{"S": s, "N": 1} }),
		},
		Components: map[string]Builder{
			"Probe": func(e Element, c *Context) (gooey.Component, error) {
				row = c
				return &components.Text{}, nil
			},
			// Builds BEFORE the <ItemsView> — document order — so the
			// sentinel is in the page's map before any row is realized.
			// The fixture declares no <Frozen>, so the map may not exist
			// yet; a row that reads the page's sinks would see this key.
			"PageProbe": func(e Element, c *Context) (gooey.Component, error) {
				if c.arms.sinks == nil {
					c.arms.sinks = map[*prop.Property[string]]string{}
				}
				c.arms.sinks[armsSentinel] = "page"
				return &components.Text{}, nil
			},
		},
		declared: map[string]any{"PageDecl": nil},
	}
	// The ancestry is normally pushed by control(); there is no control
	// here, so it is set directly — the question is whether the ROW
	// keeps it, not how it got onto the page.
	page.controls = []string{"page.gooey"}

	if _, err := Load(pageFS, "page.gooey", page); err != nil {
		t.Fatalf("the page did not load, so nothing below was observed: %v", err)
	}
	if row == nil {
		t.Fatal("the probe builder never ran, so no row context was reached. " +
			"ItemsView.Validate realizes one throwaway row during the build; " +
			"if that stopped happening this test sees nothing")
	}

	// EVERY FIELD, driven by contextFields — the same walk the control
	// seam uses, so Context growing a field is red at BOTH seams. The
	// six-entry literal this replaces could not have been red for fsys
	// or declared, which is how they came to be row-scoped by omission.
	for _, name := range contextFields(t) {
		rule, ok := rowPartition[name]
		if !ok {
			t.Errorf("Context.%s is not in rowPartition, so nothing says "+
				"whether an item-template row inherits it. That omission is "+
				"how Elements and Variant came to be missing from the row "+
				"context, and fsys and declared came to be scoped to it with "+
				"no reason written anywhere", name)
			continue
		}
		var crossed bool
		switch name {
		case "Styles":
			_, crossed = row.Styles["s"]
		case "Components":
			_, crossed = row.Components["Probe"]
		case "Elements":
			_, crossed = row.Elements["Meter"]
		case "Handlers":
			_, crossed = row.Handlers["H"]
		case "Rules":
			_, crossed = row.Rules["Zonk"]
		case "Declared":
			_, crossed = row.Declared[declSentinel]
		case "Includes":
			// THE FS ITSELF, asked a question only the page's answers.
			// `!= nil` says "there is an FS here", not "it is the
			// page's" — the weak form this file's control switch spent
			// eight arms replacing, kept in the row switch by oversight.
			// Raised in review of #490.
			if row.Includes != nil {
				_, err := fs.ReadFile(row.Includes, "card.gooey")
				crossed = err == nil
			}
		case "Dispatcher":
			crossed = row.Dispatcher == page.Dispatcher
		case "Dir":
			crossed = row.Dir == page.Dir
		case "Variant":
			crossed = row.Variant == page.Variant
		case "controls":
			crossed = len(row.controls) > 0 &&
				row.controls[len(row.controls)-1] == "page.gooey"
		case "res":
			crossed = row.res.cur != nil
		case "fsys":
			// The DOCUMENT's FS, asked a question only it answers —
			// behind a nil check, because dropping the propagation leaves
			// a nil interface and fs.ReadFile PANICS on one, taking the
			// package's run with it instead of reporting. Same trap the
			// Includes arm above carries.
			if row.fsys != nil {
				_, err := fs.ReadFile(row.fsys, "page.gooey")
				crossed = err == nil
			}
		case "Values":
			// The row's Values are the ITEM, so the page's own key must
			// not be visible through them.
			_, crossed = row.Values["PageOnly"]
		case "Named":
			_, crossed = row.Named["PageOnly"]
		case "arms":
			// Not "is it set" — the row builds its own — but whether the
			// page's map IS the row's. The sentinel is the whole answer:
			// it was written into the page's live sink map by PageProbe,
			// so a row sharing that map sees it and a row with its own
			// does not. Two empty maps cannot fake agreement here, which
			// is what the old membership comparison allowed.
			_, crossed = row.arms.sinks[armsSentinel]
		case "ns":
			_, crossed = row.ns["probe"]
		case "declared":
			_, crossed = row.declared["PageDecl"]
		default:
			t.Errorf("Context.%s is partitioned for a row but this switch does "+
				"not read it, so its half of the contract is unchecked", name)
			continue
		}
		if crossed != rule.inherit {
			verb := "did not reach an item-template row"
			if crossed {
				verb = "reached an item-template row and must not have"
			}
			t.Errorf("Context.%s %s — %s", name, verb, rule.why)
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

// TestAPageRelativeAssetPathWorksInsideARow is the symptom, and it is
// the one an author reports.
//
// fsys is what Context.assets resolves a literal path against, falling
// back to Includes when it is nil. With the row context leaving it nil,
// <Image Src="logo.png"> inside an <ItemsView.ItemTemplate> failed with
// "no file system to load from — this tree was built from bytes; use
// markup.Load", which is advice the author had already taken, while the
// identical element one line outside the template loaded. <MenuItem
// Icon> and <FileWatcher Paths> read the same seam.
//
// BOTH ARMS, because the template arm alone would pass against a fixture
// whose asset is simply unreadable everywhere. The page arm is what says
// the FS and the file are fine and the SEAM is the difference. Raised in
// review of #490.
func TestAPageRelativeAssetPathWorksInsideARow(t *testing.T) {
	var png bytes.Buffer
	if err := gopng.Encode(&png, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatalf("building the fixture image: %v", err)
	}
	const el = `<Image Src="logo.png" Cols="2" Rows="1"/>`
	fsys := fstest.MapFS{
		"logo.png": {Data: png.Bytes()},
		"page.gooey": {Data: []byte(`<Gooey xmlns="wonderforge.io/gooey/2026">` +
			el + `</Gooey>`)},
		"row.gooey": {Data: []byte(`<Gooey xmlns="wonderforge.io/gooey/2026">` +
			`<ItemsView Name="List" Items="{{.Items}}"><ItemsView.ItemTemplate>` +
			`<VStack><Probe/>` + el + `</VStack>` +
			`</ItemsView.ItemTemplate></ItemsView></Gooey>`)},
	}
	src := prop.NewSource([]string{"a"})
	var realized int
	ctx := &Context{
		Named: map[string]gooey.Component{},
		Values: map[string]any{
			"Items": components.Items(src,
				func(string) map[string]any { return map[string]any{} }),
		},
		Components: map[string]Builder{
			// Beside the <Image> rather than instead of it: a row that
			// fails on the Image still builds this first, so it counts
			// the realizations that HAPPENED, which is what keeps the
			// Err() check below from passing over an empty list.
			"Probe": func(Element, *Context) (gooey.Component, error) {
				realized++
				return &components.Text{}, nil
			},
		},
	}
	if _, err := Load(fsys, "page.gooey", &Context{Values: ctx.Values}); err != nil {
		t.Fatalf("the same element failed at PAGE level, so this fixture cannot "+
			"tell the seam from a broken asset: %v", err)
	}
	root, err := Load(fsys, "row.gooey", ctx)
	if err != nil {
		t.Fatalf("a page-relative asset path did not resolve inside an item "+
			"template, while the identical element at page level did: %v\n"+
			"The row context is built in markup/itemsview.go and must carry "+
			"the document's fsys — a row's markup came from the same "+
			"document the <ItemsView> did", err)
	}

	// AND THEN A ROW NOBODY REALIZED AT LOAD TIME, which is the arm that
	// matters and the one this test did not have.
	//
	// Load succeeding proves only that ItemsView.Validate's throwaway
	// probe row built, and that row is realized DURING the page build,
	// while ctx.fsys is still installed. Every row a user scrolls to is
	// realized by the composer after Load returned and its defer put
	// ctx.fsys back — so with the FS read inside the factory rather than
	// captured, the arm above passed against the bug. Raised in review of
	// #490.
	list, _ := ctx.Named["List"].(*components.ItemsView)
	if list == nil {
		t.Fatal("the <ItemsView> is not in the page's Named map, so the rows " +
			"below cannot be asked whether they built")
	}
	c := gooey.NewComposer(root, 40, 10)
	c.Frame()
	src.Set([]string{"a", "b", "c"})
	c.Frame()
	if realized < 2 {
		t.Fatalf("only %d template rows were realized, and one of those is the "+
			"load-time probe: the composer never built a row after Load "+
			"returned, so nothing here measures the seam", realized)
	}
	if err := list.Err(); err != nil {
		t.Errorf("a row realized AFTER Load returned could not resolve a "+
			"page-relative asset path: %v\n"+
			"itemsview.go must CAPTURE ctx.fsys beside pagePending rather than "+
			"read it inside the factory — Load restores it in a defer, so a "+
			"row built at scroll time sees nil", err)
	}
}

// TestAControlCannotShadowAPageDeclaredElement is the behaviour change
// that came free with inheriting Elements, stated rather than
// discovered.
//
// Before this branch a control's context could not see the page's
// Elements, so a setup registering Components["Meter"] privately, on a
// page that declares Elements["Meter"], simply won: the two names lived
// in different scopes. Now Elements crosses the boundary, and
// markup.buildComponent refuses a name present in BOTH maps
// because one of them would be unreachable and which one would depend on
// the order those ifs happen to be written in.
//
// That refusal is the intended answer — the alternative is a control
// silently shadowing a declared element, which is the vocabulary problem
// #314 exists to remove — but it is a document that used to load and now
// does not, and the error names a collision the control author did not
// create. So: pinned here, and the way out is written in the Elements
// arm's comment in usercontrol.go. Raised in review of #490.
func TestAControlCannotShadowAPageDeclaredElement(t *testing.T) {
	ctlFS := fstest.MapFS{
		"card.gooey": {Data: []byte(
			`<Gooey xmlns="wonderforge.io/gooey/2026"><Meter/></Gooey>`)},
	}
	page := &Context{
		Elements: map[string]*ElementDef{"Meter": meterDef()},
		Components: map[string]Builder{
			"Card": UserControl(ctlFS, "card.gooey",
				func(e Element, parent *Context) (*Context, error) {
					// The control's OWN idea of <Meter>, private to it.
					return &Context{Components: map[string]Builder{
						"Meter": func(Element, *Context) (gooey.Component, error) {
							return &components.Text{}, nil
						},
					}}, nil
				}),
		},
	}
	_, err := Build([]byte(
		`<Gooey xmlns="wonderforge.io/gooey/2026"><Card/></Gooey>`), page)
	if err == nil {
		t.Fatal("a control registered its own <Meter> builder on a page that " +
			"DECLARES <Meter>, and the document loaded. One of the two is " +
			"unreachable, and which one would depend on the order of the ifs " +
			"in markup.buildComponent — that is the silent shadowing the declared " +
			"vocabulary exists to prevent")
	}
	if !strings.Contains(err.Error(), "registered in both") {
		t.Errorf("the load failed for some other reason than the both-maps "+
			"collision, so this test is not reaching the seam it is about: %v", err)
	}
	// AND IT NAMES THE CONTROL. buildComponent's message is written for one
	// author holding both maps; here they are two, and neither wrote a
	// duplicate. Without the control's name the person who can act on it —
	// whoever wrote card.gooey's setup — is handed a sentence about a page
	// they may not own. Pinning only the refusal pins that the load fails,
	// not that the report is usable. Raised in review of #490.
	if !strings.Contains(err.Error(), "card.gooey") {
		t.Errorf("the collision is reported as %q — it names neither the control "+
			"nor its file, so it reads as a page-level duplicate that nobody wrote", err)
	}
}

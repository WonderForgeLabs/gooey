package main

import (
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/markup"
)

// The designer builds the document being edited with a DIFFERENT context
// from the one it builds its own chrome with, and only the chrome's got
// the Dispatcher. So the canvas refused markup that is legal in a real
// app, and refused it at LOAD — while the palette went on offering the
// attribute, because the properties pane is driven from ElementDef.Attrs
// and knows nothing about the context the thing will be built in.
//
// #462. Found by review of #459, whose <Frozen AllowError="{{.Err}}"> is
// the first PLAIN ATTRIBUTE to need a Dispatcher; the class is older —
// `{{ns:Fn}}` has needed one since handlers landed
// (markup/handlers.go:193), which is what the tests here use, so they
// pin the property on main rather than waiting for that PR.

// TestEveryContextTheEditorBuildsWithGetsTheDispatcher is the general
// version, and the general version is the point: the bug is not "AllowError
// is broken", it is "a context the editor builds trees with can be wired
// up short". Naming ed.ctx and ed.docCtx individually would pass again the
// day a third context is added and forgotten, which is exactly how this one
// arrived.
func TestEveryContextTheEditorBuildsWithGetsTheDispatcher(t *testing.T) {
	ed := newEditor(editorFS())
	d := gooey.NewDispatcher()

	for i, c := range ed.contexts() {
		if c == nil {
			t.Fatalf("contexts()[%d] is nil", i)
		}
		if c.Dispatcher != nil {
			t.Fatalf("contexts()[%d] already has a Dispatcher before wiring; "+
				"this test cannot see the bug it exists for", i)
		}
	}

	ed.setDispatcher(d)

	for i, c := range ed.contexts() {
		if c.Dispatcher == nil {
			t.Errorf("contexts()[%d] has no Dispatcher after wiring", i)
		}
	}
}

// TestTheContextListCoversTheOnesTheEditorActuallyUses is the half that
// stops contexts() being satisfied by returning a SHORT LIST — a known
// context dropped from it.
//
// THAT IS ALL IT PINS, and its comment claimed more until review of #469:
// "if a third is added, this fails" is the one thing an enumerated list
// cannot do, because a field absent from both contexts() and this literal
// is invisible to both. It hardens against the mutation a reviewer would
// make and misses the one history made — #462 was a context that existed
// and was never wired. TestEveryContextFieldIsInTheList below is the
// derived half, and it is where the add-and-forget claim now lives.
func TestTheContextListCoversTheOnesTheEditorActuallyUses(t *testing.T) {
	ed := newEditor(editorFS())
	got := ed.contexts()

	for _, want := range []struct {
		name string
		c    *markup.Context
	}{
		{"ed.ctx (the editor's own chrome)", ed.ctx},
		{"ed.docCtx (the document being edited)", ed.docCtx},
	} {
		found := false
		for _, c := range got {
			if c == want.c {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("contexts() does not include %s, so nothing wires it", want.name)
		}
	}
}

// TestTheCanvasBuildsMarkupARealAppAccepts is the behavioural half, and
// the one that would actually have caught #462: it goes through docCtx
// with markup whose ONLY problem was the missing Dispatcher.
//
// A handler expression rather than <Frozen AllowError>, because the
// defect is the context and not the attribute — and because this way the
// test pins the property on main instead of on the PR that exposed it.
//
// WHAT IT DOES NOT SAY is that a user can type this into the designer.
// The page here is handed to markup.Build directly, and it carries an
// xmlns the editor's own canvas path cannot produce: ed.rebuild emits the
// literal "<Gooey>" envelope with no declarations, and nodeOf — what
// openWorkspaceFile parses a real document with — drops xmlns attributes
// as it reads. So handler markup opened through the file browser still
// fails to load, now with "undeclared namespace prefix" rather than a
// missing Dispatcher. Two different bugs that produce the same screen;
// this test is about the first, and the comment used to read as though it
// were about both. Raised in review of #469, which reproduced the second
// one; the xmlns gap is separate work.
func TestTheCanvasBuildsMarkupARealAppAccepts(t *testing.T) {
	const uri = "urn:gooey:test:462"
	markup.RegisterHandlers(uri, markup.HandlerFunc(
		func(c *markup.Call) (gooey.Command, error) {
			return gooey.Command(func() {}), nil
		}))
	t.Cleanup(func() { markup.RegisterHandlers(uri, nil) })

	page := `<Gooey xmlns:t="` + uri + `"><VStack>` +
		`<Button Name="B" Content="go" Click="{{t:Fire}}"/>` +
		`</VStack></Gooey>`

	ed := newEditor(editorFS())
	ed.setDispatcher(gooey.NewDispatcher())

	// THE DOCUMENT context, which is the one the canvas uses. Building
	// through ed.ctx instead would pass with the bug present.
	if _, err := markup.Build([]byte(page), ed.docCtx); err != nil {
		t.Fatalf("the canvas refuses markup a real app accepts: %v", err)
	}

}

// TestARebuildCannotCarryAHandlerNamespaceYet is the limitation above,
// LATCHED — the review of #469 asked for an assertion driven through
// ed.rebuild, and this is the honest form of one.
//
// A rebuild that merely loads clean would not do: the shipped default
// document contains no handler expression, so an unwired context cannot
// make it fail, and the assertion would pass for a reason unrelated to
// its name. That is the trap the fifth test in the previous round fell
// into and was deleted for.
//
// So this asserts the REFUSAL instead, which is real and reproducible:
// put a handler expression on a node, rebuild, and the load fails on the
// namespace before the Dispatcher is ever consulted, because ed.rebuild
// emits a bare "<Gooey>" envelope and nodeOf drops xmlns declarations as
// it reads. Two bugs, one screen. #462 is the one this PR fixes; the
// second is why no rebuild-driven test can pin the first today.
//
// The second bug is tracked as #472. WHEN THAT IS CLOSED THIS TEST GOES
// RED, which is the point of writing it as a latch rather than as a
// comment: the person who fixes it is told, here, that a dispatcher
// assertion through rebuild has become possible and should replace this.
func TestARebuildCannotCarryAHandlerNamespaceYet(t *testing.T) {
	// The provider is INERT and deliberately so. Registration is by URI,
	// while the refusal below happens on the PREFIX `t:` — which the
	// rebuilt envelope never declares, so nothing ever resolves to this
	// URI and the handler body cannot run. It is here to remove the
	// alternative reading of a red result: without it, "the namespace is
	// undeclared" and "no handler is registered for it" are two
	// explanations for one message, and only the first is the gap #472
	// tracks. Registering it makes the second impossible. Raised in
	// review of #469.
	const uri = "urn:gooey:test:462:rebuild"
	markup.RegisterHandlers(uri, markup.HandlerFunc(
		func(c *markup.Call) (gooey.Command, error) {
			return gooey.Command(func() {}), nil
		}))
	t.Cleanup(func() { markup.RegisterHandlers(uri, nil) })

	ed := newEditor(editorFS())
	ed.setDispatcher(gooey.NewDispatcher())
	ed.doc().Kids = append(ed.doc().Kids, &node{
		Elem: "Button",
		Attrs: map[string]string{
			"Name": "B", "Content": "go", "Click": "{{t:Fire}}",
		},
	})
	ed.rebuild()

	got := ed.status.Get()
	if !strings.Contains(got, "undeclared namespace prefix") {
		t.Errorf("rebuilding a document with a handler expression reports %q.\n"+
			"If it now LOADS, the xmlns gap (#472) is closed and this test has done its "+
			"job: replace it with one that asserts the rebuild built, which finally "+
			"pins the Dispatcher through the path the running editor takes.\n"+
			"If it fails some other way, the canvas has a third problem.", got)
	}
	if ed.docRoot != nil {
		t.Error("the canvas built a document whose load it reported as failed")
	}
}

// TestTheEditorsOwnContextWasNeverTheBrokenOne pins the asymmetry the
// issue describes, so a "fix" that wired docCtx by unwiring ed.ctx — or a
// test above that passed because both were nil — is caught.
func TestTheEditorsOwnContextWasNeverTheBrokenOne(t *testing.T) {
	ed := newEditor(editorFS())
	ed.setDispatcher(gooey.NewDispatcher())

	if ed.ctx.Dispatcher == nil {
		t.Error("the editor's own context lost its Dispatcher")
	}
	if ed.docCtx.Dispatcher == nil {
		t.Error("the document context has no Dispatcher")
	}
	if ed.ctx.Dispatcher != ed.docCtx.Dispatcher {
		t.Error("the two contexts hold different Dispatchers; handler results " +
			"must land on the one UI goroutine, not on two")
	}
}

// TestEveryContextFieldIsInTheList is the derived half of the obligation
// contexts() states, and the reason it is derived rather than written out
// is the whole of #462: the failure was a context that EXISTED and was
// never wired, so a check assembled from the contexts somebody remembered
// is a check calibrated against the bug.
//
// SYNTACTIC, not reflective. It parses main.go and compares the editor
// struct's *markup.Context fields against the identifiers contexts()
// names, so the no-reflection invariant is untouched — and the repo
// already tests this way (TestCIWorkflowAndCLAUDEMDShareOneDiscovery
// reads a workflow file). Raised in review of #469.
//
// WHAT THE WALK DOES NOT SEE, stated because a syntactic check reads as
// exhaustive and this one is not. It matches exactly `*markup.Context`
// fields declared on the `editor` struct in THIS file: a context held in
// a []*markup.Context or a map, a markup.Context stored by value, one
// reached through an embedded struct, and an editor field declared in
// another file of the package are all invisible to it. Each of those is
// a way to reintroduce #462 that this test would not catch. It is still
// worth having — the shape the bug actually arrived in is a plain
// pointer field beside the two that are there — but "green" here means
// "no new plain pointer field", not "every context is wired". Raised in
// review of #469.
func TestEveryContextFieldIsInTheList(t *testing.T) {
	file := parseMainGo(t)

	var fields []string
	ast.Inspect(file, func(n ast.Node) bool {
		ts, ok := n.(*ast.TypeSpec)
		if !ok || ts.Name.Name != "editor" {
			return true
		}
		st, ok := ts.Type.(*ast.StructType)
		if !ok {
			return true
		}
		for _, f := range st.Fields.List {
			star, ok := f.Type.(*ast.StarExpr)
			if !ok {
				continue
			}
			sel, ok := star.X.(*ast.SelectorExpr)
			if !ok {
				continue
			}
			pkg, ok := sel.X.(*ast.Ident)
			if !ok || pkg.Name != "markup" || sel.Sel.Name != "Context" {
				continue
			}
			for _, name := range f.Names {
				fields = append(fields, name.Name)
			}
		}
		return false
	})
	// NON-VACUITY, and the floor is DERIVED. A walk that found nothing
	// would report every field covered, which is the failure this whole
	// test exists to make impossible one level down — but a written
	// number is a sample: `< 2` would misread a legitimate merge down to
	// one context as a broken parse, and would go on passing if the
	// struct grew to four and the walk started finding two.
	//
	// contexts() is the right comparand because it is the list under
	// test. The parse must find at least as many fields as that list has
	// entries; finding MORE is the bug this test is for, and is left to
	// the loop below. Raised in review of #469.
	want := len(newEditor(editorFS()).contexts())
	if len(fields) < want {
		t.Fatalf("found %d *markup.Context field(s) on the editor struct (%v) but "+
			"contexts() returns %d entries; the parse is broken and the assertion "+
			"below is about nothing", len(fields), fields, want)
	}

	named := identsIn(t, file, "contexts")
	for _, f := range fields {
		if !named[f] {
			t.Errorf("the editor has a *markup.Context field %q that contexts() does "+
				"not return, so setDispatcher never reaches it and anything built "+
				"with it refuses handler expressions at LOAD — silently, because "+
				"the palette goes on offering the attribute. That is #462. Add it "+
				"to contexts(), or say in a comment there why it is exempt.", f)
		}
	}
}

// TestMainWiresTheDispatcher pins the ONE line the fix is, and nothing
// else can: ed.setDispatcher(app.Dispatcher()) is reached only from
// main(), every test wires its own, and deleting it leaves the suite
// green — the exact regression this change exists to stop recurring.
// This pre-dates the change (the single assignment it replaced was just
// as uncovered), which is why it is worth writing down rather than
// assuming the mutation matrix covered it. Raised in review of #469.
func TestMainWiresTheDispatcher(t *testing.T) {
	file := parseMainGo(t)
	body := funcBody(t, file, "main")

	// The variable gooey.NewApp's result is bound to, DERIVED rather than
	// spelled: renaming it must not quietly turn the argument check off.
	appVar := ""
	ast.Inspect(body, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok || len(as.Lhs) != 1 || len(as.Rhs) != 1 {
			return true
		}
		if _, is := selectorCall(as.Rhs[0], "gooey", "NewApp"); !is {
			return true
		}
		if id, ok := as.Lhs[0].(*ast.Ident); ok {
			appVar = id.Name
		}
		return false
	})
	if appVar == "" {
		t.Fatal("main() does not bind gooey.NewApp's result to a plain variable, so " +
			"nothing here can tell which dispatcher is the app's and the assertion " +
			"below would be about nothing")
	}

	var arg ast.Expr
	called := false
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "setDispatcher" {
			return true
		}
		called = true
		if len(call.Args) == 1 {
			arg = call.Args[0]
		}
		return false
	})
	if !called {
		t.Fatal("main() does not call ed.setDispatcher, so the shipped editor builds " +
			"every document with contexts that have no Dispatcher — however well the " +
			"tests wire their own")
	}
	// THE ARGUMENT IS THE ASSERTION. Checking only that the identifier
	// appears lets the call be fed gooey.NewDispatcher() — a second,
	// never-drained dispatcher — and every handler result posted to it is
	// dropped on the floor while the editor looks correctly wired. That
	// mutation passed the identifier check unchanged. Raised in review of
	// #469.
	if arg == nil || !isCallOf(arg, appVar, "Dispatcher") {
		t.Errorf("main() calls setDispatcher(%s), not %s.Dispatcher(). A dispatcher "+
			"that is not the app's is never Drained, so every handler result "+
			"posted to it is dropped and the editor looks wired while nothing "+
			"it schedules ever runs.", exprString(arg), appVar)
	}
}

// selectorCall reports whether e is a call of `x.sel`, whatever its
// arguments, and hands back the call so a caller can look at them.
func selectorCall(e ast.Expr, x, sel string) (*ast.CallExpr, bool) {
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return nil, false
	}
	s, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || s.Sel.Name != sel {
		return nil, false
	}
	id, ok := s.X.(*ast.Ident)
	if !ok || id.Name != x {
		return nil, false
	}
	return call, true
}

// isCallOf reports whether e is exactly `x.sel()` — a call of a selector
// on a plain identifier, taking NO arguments. The arity matters here:
// Dispatcher() takes none, so anything passed to it is a different
// expression and must not read as the app's own dispatcher.
func isCallOf(e ast.Expr, x, sel string) bool {
	call, ok := selectorCall(e, x, sel)
	return ok && len(call.Args) == 0
}

// exprString renders an expression for a failure message. nil is spelled
// out rather than left blank, because "calls setDispatcher() with " reads
// as a truncated message rather than as the wrong arity it means.
func exprString(e ast.Expr) string {
	if e == nil {
		return "<no single argument>"
	}
	var b strings.Builder
	if err := printer.Fprint(&b, token.NewFileSet(), e); err != nil {
		return "<unprintable>"
	}
	return b.String()
}

// parseMainGo reads the file both derived checks are about. It fails
// rather than skipping: a missing main.go means the test package moved,
// not that the property stopped mattering.
func parseMainGo(t *testing.T) *ast.File {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), "main.go", nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parsing main.go: %v", err)
	}
	return f
}

// identsIn is every identifier named in the body of one top-level method
// or function. Coarse on purpose — it answers "does this body mention
// x", which is what both callers ask, and a finer answer would need type
// information neither needs.
func identsIn(t *testing.T, file *ast.File, fn string) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	ast.Inspect(funcBody(t, file, fn), func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok {
			out[id.Name] = true
		}
		return true
	})
	return out
}

// funcBody is the "or this test checks nothing" half, factored out: every
// caller here reads a named function's body, and a rename that makes the
// walk find nothing must be fatal rather than vacuously green.
func funcBody(t *testing.T, file *ast.File, fn string) *ast.BlockStmt {
	t.Helper()
	for _, d := range file.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if ok && fd.Name.Name == fn && fd.Body != nil {
			return fd.Body
		}
	}
	t.Fatalf("main.go has no func %s, so this test checks nothing", fn)
	return nil
}

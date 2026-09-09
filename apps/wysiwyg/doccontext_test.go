package main

import (
	"go/ast"
	"go/parser"
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
// WHEN THE XMLNS GAP IS CLOSED THIS TEST GOES RED, which is the point of
// writing it as a latch rather than as a comment: the person who fixes it
// is told, here, that a dispatcher assertion through rebuild has become
// possible and should replace this.
func TestARebuildCannotCarryAHandlerNamespaceYet(t *testing.T) {
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
			"If it now LOADS, the xmlns gap is closed and this test has done its "+
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
	// NON-VACUITY. A walk that found nothing would report every field
	// covered, which is the failure this whole test exists to make
	// impossible one level down.
	if len(fields) < 2 {
		t.Fatalf("found %d *markup.Context fields on the editor struct (%v); the "+
			"parse is broken and the assertion below is about nothing",
			len(fields), fields)
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
	if !identsIn(t, parseMainGo(t), "main")["setDispatcher"] {
		t.Error("main() does not call ed.setDispatcher, so the shipped editor builds " +
			"every document with contexts that have no Dispatcher — however well the " +
			"tests wire their own")
	}
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
	for _, d := range file.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || fd.Name.Name != fn || fd.Body == nil {
			continue
		}
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok {
				out[id.Name] = true
			}
			return true
		})
		return out
	}
	t.Fatalf("main.go has no func %s, so this test checks nothing", fn)
	return nil
}

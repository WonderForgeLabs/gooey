package markup

import (
	"errors"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/prop"
)

const testURI = "gooey.dev/handlers/test"

// recorder is a provider that keeps the Calls it was asked to build, so
// tests can assert on what the grammar handed the provider.
type recorder struct {
	calls []*Call
	fired []string // args, joined, as of each invocation
}

func (r *recorder) NewCommand(c *Call) (gooey.Command, error) {
	if c.Fn == "Boom" {
		return nil, errBoom
	}
	r.calls = append(r.calls, c)
	return func() {
		var vals []string
		for _, a := range c.Args {
			vals = append(vals, a.String())
		}
		joined := strings.Join(vals, "|")
		r.fired = append(r.fired, joined)
		c.Target.Deliver(c.Fn + ":" + joined)
	}, nil
}

var errBoom = errors.New("provider said no")

func loadWith(t *testing.T, src string, vals map[string]any) (gooey.Widget, *Context, *recorder) {
	t.Helper()
	r := &recorder{}
	RegisterHandlers(testURI, r)
	t.Cleanup(func() { RegisterHandlers(testURI, nil) })
	ctx := &Context{Values: vals, Dispatcher: gooey.NewDispatcher()}
	w, err := Load(fstest.MapFS{"page.gooey": {Data: []byte(src)}}, "page.gooey", ctx)
	if err != nil {
		t.Fatal(err)
	}
	return w, ctx, r
}

func TestXmlnsTableIsCapturedPerDocument(t *testing.T) {
	src := `<Gooey xmlns="wonderforge.io/gooey/2026"
	       xmlns:t="gooey.dev/handlers/test"
	       xmlns:other="gooey.dev/handlers/other">
	  <Text Name="body">hi</Text>
	</Gooey>`
	root, ns, err := parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if got := ns["t"]; got != testURI {
		t.Fatalf("ns[t]=%q, want %q", got, testURI)
	}
	if got := ns["other"]; got != "gooey.dev/handlers/other" {
		t.Fatalf("ns[other]=%q", got)
	}
	if _, ok := ns["xmlns"]; ok {
		t.Fatal("the default namespace leaked into the prefix table")
	}
	if len(ns) != 2 {
		t.Fatalf("captured %d prefixes, want 2", len(ns))
	}

	// The table is scoped to the build and restored afterwards, so a
	// context outlives no document's namespaces.
	_, ctx, _ := loadWith(t, src, map[string]any{})
	if len(ctx.ns) != 0 {
		t.Fatalf("ctx.ns=%v after Load, want it restored", ctx.ns)
	}

	// Declarations configure the document; they are not properties, so
	// they must not show up as attributes on the root element.
	for _, bad := range []string{"xmlns", "t", "other"} {
		if v, ok := root.Attrs[bad]; ok {
			t.Errorf("Attrs[%q]=%q — xmlns declarations should not become attributes", bad, v)
		}
	}
}

func TestHandlerExpressionReachesProviderWithResolvedArgs(t *testing.T) {
	name := prop.NewSource("gooey")
	out := prop.NewSource("")
	src := `<Gooey xmlns:t="gooey.dev/handlers/test">
	  <Button Content="go" Click="{{t:Run ` + "`Slugify`" + ` .Name | into .Out}}"/>
	</Gooey>`
	w, _, r := loadWith(t, src, map[string]any{"Name": name, "Out": out})

	if len(r.calls) != 1 {
		t.Fatalf("provider built %d commands, want 1", len(r.calls))
	}
	c := r.calls[0]
	if c.Prefix != "t" || c.URI != testURI || c.Fn != "Run" {
		t.Fatalf("call = %s:%s via %q, want t:Run via %q", c.Prefix, c.Fn, c.URI, testURI)
	}
	if len(c.Args) != 2 {
		t.Fatalf("got %d args, want 2", len(c.Args))
	}
	if !c.Args[0].IsLiteral() || c.Args[0].String() != "Slugify" {
		t.Fatalf("arg 0 = %q (literal=%v), want the backtick literal Slugify", c.Args[0].String(), c.Args[0].IsLiteral())
	}
	if c.Args[1].IsLiteral() || c.Args[1].Path != "Name" {
		t.Fatalf("arg 1 = path %q (literal=%v), want the bound .Name", c.Args[1].Path, c.Args[1].IsLiteral())
	}
	if c.Target.Path() != "Out" || !c.Target.Valid() {
		t.Fatalf("target = %q valid=%v, want .Out", c.Target.Path(), c.Target.Valid())
	}
	_ = w
}

// Bound arguments are handles: the value is whatever the property holds
// when the event fires, not what it held at load.
func TestBoundArgumentsAreReadAtInvokeTime(t *testing.T) {
	name := prop.NewSource("first")
	out := prop.NewSource("")
	src := `<Gooey xmlns:t="gooey.dev/handlers/test">
	  <Button Content="go" Click="{{t:Run .Name | into .Out}}"/>
	</Gooey>`
	w, ctx, r := loadWith(t, src, map[string]any{"Name": name, "Out": out})
	comp := gooey.NewComposer(w, 20, 3)

	comp.HandleKey(input.Named(input.KeyEnter))
	name.Set("second")
	comp.HandleKey(input.Named(input.KeyEnter))

	if len(r.fired) != 2 || r.fired[0] != "first" || r.fired[1] != "second" {
		t.Fatalf("provider saw %v, want [first second]", r.fired)
	}
	ctx.Dispatcher.Drain()
	if got := out.Get(); got != "Run:second" {
		t.Fatalf("out=%q, want the second delivery", got)
	}
}

// Delivery is queued, not immediate: the property changes on the UI
// goroutine when the loop drains, which is what keeps handler results
// inside the confinement rule.
func TestDeliveryGoesThroughTheDispatcher(t *testing.T) {
	out := prop.NewSource("before")
	src := `<Gooey xmlns:t="gooey.dev/handlers/test">
	  <Button Content="go" Click="{{t:Run ` + "`x`" + ` | into .Out}}"/>
	</Gooey>`
	w, ctx, _ := loadWith(t, src, map[string]any{"Out": out})
	comp := gooey.NewComposer(w, 20, 3)

	comp.HandleKey(input.Named(input.KeyEnter))
	if got := out.Get(); got != "before" {
		t.Fatalf("out=%q — the target was Set without a drain", got)
	}
	if n := ctx.Dispatcher.Drain(); n != 1 {
		t.Fatalf("drained %d, want 1", n)
	}
	if got := out.Get(); got != "Run:x" {
		t.Fatalf("out=%q after drain, want %q", got, "Run:x")
	}
}

// A handler expression produces a Command, so it works anywhere a
// Command does — including a declared gesture.
func TestKeyBindingTakesAHandlerExpression(t *testing.T) {
	out := prop.NewSource("")
	src := `<Gooey xmlns:t="gooey.dev/handlers/test">
	  <VStack>
	    <Text>body</Text>
	    <KeyBinding Gesture="ctrl+r" Command="{{t:Run ` + "`bound`" + ` | into .Out}}"/>
	  </VStack>
	</Gooey>`
	w, ctx, _ := loadWith(t, src, map[string]any{"Out": out})
	comp := gooey.NewComposer(w, 20, 3)

	if !comp.HandleKey(input.KeyEvent{Key: input.KeyRune, Rune: 'r', Mods: input.ModCtrl}) {
		t.Fatal("the gesture did not fire the handler")
	}
	ctx.Dispatcher.Drain()
	if got := out.Get(); got != "Run:bound" {
		t.Fatalf("out=%q", got)
	}
}

// The expression needs no target: a provider may act for effect only.
func TestTargetIsOptional(t *testing.T) {
	src := `<Gooey xmlns:t="gooey.dev/handlers/test">
	  <Button Content="go" Click="{{t:Run ` + "`fire`" + `}}"/>
	</Gooey>`
	w, _, r := loadWith(t, src, map[string]any{})
	if r.calls[0].Target.Valid() {
		t.Fatal("Target reported valid with no into stage")
	}
	// Deliver on an absent target is a no-op, not a panic.
	gooey.NewComposer(w, 20, 3).HandleKey(input.Named(input.KeyEnter))
	if len(r.fired) != 1 {
		t.Fatalf("fired %d times, want 1", len(r.fired))
	}
}

func TestExpressionSyntaxErrors(t *testing.T) {
	cases := map[string]struct{ expr, want string }{
		"unterminated literal": {"{{t:Run `oops | into .Out}}", "unterminated"},
		"bare word argument":   {"{{t:Run Slugify | into .Out}}", "bare word"},
		"unknown stage":        {"{{t:Run `x` | onto .Out}}", "unknown pipeline stage"},
		"trailing pipe":        {"{{t:Run `x` |}}", "trailing |"},
		"into without target":  {"{{t:Run `x` | into}}", "exactly one .Path"},
		"into a literal":       {"{{t:Run `x` | into `Out`}}", "exactly one .Path"},
		"two into stages":      {"{{t:Run `x` | into .Out | into .Out}}", "more than one"},
		"empty path":           {"{{t:Run . | into .Out}}", "empty path"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			r := &recorder{}
			RegisterHandlers(testURI, r)
			defer RegisterHandlers(testURI, nil)
			src := `<Gooey xmlns:t="gooey.dev/handlers/test"><Button Content="x" Click="` + tc.expr + `"/></Gooey>`
			ctx := &Context{
				Values:     map[string]any{"Out": prop.NewSource("")},
				Dispatcher: gooey.NewDispatcher(),
			}
			_, err := Load(fstest.MapFS{"page.gooey": {Data: []byte(src)}}, "page.gooey", ctx)
			if err == nil {
				t.Fatalf("expected an error mentioning %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

// A provider rejecting a call fails the load — the whole point of
// building commands at load time.
func TestProviderErrorFailsTheLoad(t *testing.T) {
	r := &recorder{}
	RegisterHandlers(testURI, r)
	defer RegisterHandlers(testURI, nil)
	src := `<Gooey xmlns:t="gooey.dev/handlers/test"><Button Content="x" Click="{{t:Boom}}"/></Gooey>`
	ctx := &Context{Values: map[string]any{}, Dispatcher: gooey.NewDispatcher()}
	_, err := Load(fstest.MapFS{"page.gooey": {Data: []byte(src)}}, "page.gooey", ctx)
	if err == nil || !strings.Contains(err.Error(), "provider said no") {
		t.Fatalf("err=%v, want the provider's own message", err)
	}
}

// Namespaces are per-document. An included control declares its own, and
// cannot inherit a prefix the page happened to declare — otherwise a
// control's capabilities would depend on who included it.
func TestNamespacesDoNotLeakIntoIncludes(t *testing.T) {
	r := &recorder{}
	RegisterHandlers(testURI, r)
	defer RegisterHandlers(testURI, nil)
	fsys := fstest.MapFS{
		"page.gooey": {Data: []byte(`<Gooey xmlns:t="gooey.dev/handlers/test">
	  <Card Label="hi"/>
	</Gooey>`)},
		"card.gooey": {Data: []byte(`<Gooey>
	  <Button Content="{{.Label}}" Click="{{t:Run ` + "`inner`" + `}}"/>
	</Gooey>`)},
	}
	ctx := &Context{
		Values: map[string]any{}, Includes: fsys,
		Dispatcher: gooey.NewDispatcher(),
	}
	_, err := Load(fsys, "page.gooey", ctx)
	if err == nil || !strings.Contains(err.Error(), "undeclared namespace prefix") {
		t.Fatalf("err=%v, want the include to lack the page's prefix", err)
	}
}

// The other half of that rule: a handler expression written on an
// include's attribute is resolved in the PARENT — the document that
// declared the prefix — and crosses as a Command like any delegate.
func TestIncludeAttributeCarriesAHandlerCommand(t *testing.T) {
	out := prop.NewSource("")
	r := &recorder{}
	RegisterHandlers(testURI, r)
	defer RegisterHandlers(testURI, nil)
	fsys := fstest.MapFS{
		"page.gooey": {Data: []byte(`<Gooey xmlns:t="gooey.dev/handlers/test">
	  <Card Label="press" Action="{{t:Run ` + "`fromparent`" + ` | into .Out}}"/>
	</Gooey>`)},
		"card.gooey": {Data: []byte(`<Gooey>
	  <Button Content="{{.Label}}" Click="{{.Action}}"/>
	</Gooey>`)},
	}
	ctx := &Context{
		Values: map[string]any{"Out": out}, Includes: fsys,
		Dispatcher: gooey.NewDispatcher(),
	}
	w, err := Load(fsys, "page.gooey", ctx)
	if err != nil {
		t.Fatal(err)
	}
	gooey.NewComposer(w, 20, 3).HandleKey(input.Named(input.KeyEnter))
	ctx.Dispatcher.Drain()
	if got := out.Get(); got != "Run:fromparent" {
		t.Fatalf("out=%q, want the parent-resolved handler to have run", got)
	}
}

// A UserControl whose setup hands back the PARENT context (legal — the
// control just wants the page's bindings) must not leave its own
// document's namespace table installed. If it did, the page's later
// elements would resolve prefixes against the control's document, which
// silently changes which capability a name reaches.
func TestNestedBuildRestoresTheParentNamespaceTable(t *testing.T) {
	r := &recorder{}
	RegisterHandlers(testURI, r)
	defer RegisterHandlers(testURI, nil)

	fsys := fstest.MapFS{
		"page.gooey": {Data: []byte(`<Gooey xmlns:t="gooey.dev/handlers/test">
	  <VStack>
	    <Inner/>
	    <Button Content="after" Click="{{t:Run ` + "`sibling`" + ` | into .Out}}"/>
	  </VStack>
	</Gooey>`)},
		// The control declares no namespaces at all, so building it
		// installs an empty table on the shared context.
		"inner.gooey": {Data: []byte(`<Gooey><Text>inner</Text></Gooey>`)},
	}
	out := prop.NewSource("")
	ctx := &Context{
		Values:     map[string]any{"Out": out},
		Dispatcher: gooey.NewDispatcher(),
	}
	ctx.Widgets = map[string]Builder{
		"Inner": UserControl(fsys, "inner.gooey", func(e Element, parent *Context) (*Context, error) {
			return parent, nil // deliberately shares the page's context
		}),
	}

	w, err := Load(fsys, "page.gooey", ctx)
	if err != nil {
		t.Fatalf("the sibling after a nested build lost the page's namespace: %v", err)
	}
	comp := gooey.NewComposer(w, 30, 4)
	comp.Focus().SetFocus(comp.Focus().Order()[len(comp.Focus().Order())-1])
	comp.HandleKey(input.Named(input.KeyEnter))
	ctx.Dispatcher.Drain()
	if got := out.Get(); got != "Run:sibling" {
		t.Fatalf("out=%q, want the sibling's handler to have run", got)
	}
}

func TestRegisteredHandlersListsGrants(t *testing.T) {
	RegisterHandlers(testURI, &recorder{})
	defer RegisterHandlers(testURI, nil)
	found := false
	for _, u := range RegisteredHandlers() {
		if u == testURI {
			found = true
		}
	}
	if !found {
		t.Fatalf("RegisteredHandlers()=%v, missing %q", RegisteredHandlers(), testURI)
	}
}

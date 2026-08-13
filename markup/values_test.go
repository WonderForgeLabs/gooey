package markup

import (
	"errors"
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/prop"
)

const valueURI = "gooey.dev/values/test"

// echoProvider is a value namespace with three functions, chosen to
// exercise the three things NewValue can do: return a computed over its
// arguments, fail the load, and (wrongly) return nothing.
type echoProvider struct{ calls []*Call }

var errValueBoom = errors.New("value provider said no")

func (p *echoProvider) NewValue(c *Call) (*prop.Property[string], error) {
	p.calls = append(p.calls, c)
	switch c.Fn {
	case "Boom":
		return nil, errValueBoom
	case "Nothing":
		return nil, nil
	}
	args := c.Args
	return prop.NewComputed(func() string {
		parts := make([]string, len(args))
		for i, a := range args {
			parts[i] = a.String()
		}
		return strings.Join(parts, "|")
	}), nil
}

func withValues(t *testing.T, p ValueProvider) {
	t.Helper()
	RegisterValues(valueURI, p)
	t.Cleanup(func() { RegisterValues(valueURI, nil) })
}

// loadValue builds a page against the value namespace and returns the
// error verbatim, so tests can assert on the SHAPE of the message.
func loadValue(t *testing.T, body string, vals map[string]any) (gooey.Component, error) {
	t.Helper()
	src := `<Gooey xmlns:v="` + valueURI + `">` + body + `</Gooey>`
	ctx := &Context{Values: vals, Dispatcher: gooey.NewDispatcher()}
	return Build([]byte(src), ctx)
}

func TestValueExpressionReachesProviderWithResolvedArgs(t *testing.T) {
	p := &echoProvider{}
	withValues(t, p)
	name := prop.NewSource("gooey")

	if _, err := loadValue(t, "<Text>{{v:Echo `lit` .Name}}</Text>", map[string]any{"Name": name}); err != nil {
		t.Fatal(err)
	}
	if len(p.calls) != 1 {
		t.Fatalf("provider built %d values, want 1", len(p.calls))
	}
	c := p.calls[0]
	if c.Prefix != "v" || c.URI != valueURI || c.Fn != "Echo" {
		t.Fatalf("call = %s:%s via %q, want v:Echo via %q", c.Prefix, c.Fn, c.URI, valueURI)
	}
	if len(c.Args) != 2 {
		t.Fatalf("got %d args, want 2", len(c.Args))
	}
	if !c.Args[0].IsLiteral() || c.Args[0].String() != "lit" {
		t.Fatalf("arg 0 = %q literal=%v, want the backtick literal", c.Args[0].String(), c.Args[0].IsLiteral())
	}
	if c.Args[1].IsLiteral() || c.Args[1].Path != "Name" {
		t.Fatalf("arg 1 = path %q literal=%v, want the bound .Name", c.Args[1].Path, c.Args[1].IsLiteral())
	}
	// A value position has no push target, and the provider must be able
	// to see that rather than infer it.
	if c.Target.Valid() {
		t.Fatal("a value expression handed the provider a push Target")
	}
}

// A value call composes with literals and path bindings in one run of
// interpolated content — the whole point of resolving it where a
// binding goes rather than on an event attribute.
func TestValueExpressionInterpolatesWithLiteralsAndPaths(t *testing.T) {
	withValues(t, &echoProvider{})
	who := prop.NewSource("ada")
	w, err := loadValue(t, "<Text>hi {{v:Echo .Who}} and {{.Who}}!</Text>", map[string]any{"Who": who})
	if err != nil {
		t.Fatal(err)
	}
	c := gooey.NewComposer(w, 30, 1)
	c.Frame()
	if got := cellRow(c.Cells(), 0); got != "hi ada and ada!" {
		t.Fatalf("row = %q, want %q", got, "hi ada and ada!")
	}
}

// The damage contract. A value expression is a computed over its
// argument handles, so the argument's Get happens inside an evaluation
// and IS a subscription. Setting the argument must repaint exactly the
// one component that displays it — not the sibling, not the stack.
func TestValueExpressionRepaintsOnlyItsReaders(t *testing.T) {
	withValues(t, &echoProvider{})
	who := prop.NewSource("ada")
	src := `<VStack>
	  <Text>static</Text>
	  <Text>{{v:Echo .Who}}</Text>
	  <Text>also static</Text>
	</VStack>`
	w, err := loadValue(t, src, map[string]any{"Who": who})
	if err != nil {
		t.Fatal(err)
	}
	c := gooey.NewComposer(w, 30, 3)
	c.Frame()
	if got := cellRow(c.Cells(), 1); got != "ada" {
		t.Fatalf("row 1 = %q, want ada", got)
	}

	who.Set("grace")
	_, painted := c.Frame()
	if painted != 1 {
		t.Errorf("changing a value expression's argument painted %d components, want 1", painted)
	}
	if got := cellRow(c.Cells(), 1); got != "grace" {
		t.Fatalf("row 1 = %q, want grace", got)
	}

	// And a property nothing reads costs nothing.
	other := prop.NewSource("x")
	other.Set("y")
	_, painted = c.Frame()
	if painted != 0 {
		t.Errorf("an unread property painted %d components, want 0", painted)
	}
}

// Two bindings over the same argument are two readers, and both
// repaint — the count is a property of who reads, not of how many
// expressions exist.
func TestTwoValueExpressionsOverOneArgumentPaintTwice(t *testing.T) {
	withValues(t, &echoProvider{})
	who := prop.NewSource("ada")
	src := `<VStack>
	  <Text>{{v:Echo .Who}}</Text>
	  <Text>{{v:Echo .Who}}</Text>
	  <Text>static</Text>
	</VStack>`
	w, err := loadValue(t, src, map[string]any{"Who": who})
	if err != nil {
		t.Fatal(err)
	}
	c := gooey.NewComposer(w, 30, 3)
	c.Frame()
	who.Set("grace")
	_, painted := c.Frame()
	if painted != 2 {
		t.Errorf("painted %d components, want 2", painted)
	}
}

// --- load errors. Each asserts the SHAPE of the message, because in
// this package almost everything fails at load and err != nil proves
// nothing about which rule caught it. ---

func TestValueLoadErrors(t *testing.T) {
	cases := []struct {
		name, body, want string
	}{
		{"undeclared prefix",
			"<Text>{{zz:Echo `x`}}</Text>",
			`undeclared namespace prefix "zz"`},
		{"into stage in a value position",
			"<Text>{{v:Echo `x` | into .Out}}</Text>",
			"drop the `| into` stage"},
		{"unknown pipeline stage",
			"<Text>{{v:Echo `x` | onto .Out}}</Text>",
			"unknown pipeline stage"},
		{"bare word argument",
			"<Text>{{v:Echo bare}}</Text>",
			"bare word"},
		{"unresolvable argument path",
			"<Text>{{v:Echo .Missing}}</Text>",
			`"Missing" not found in context`},
		{"provider rejects the call",
			"<Text>{{v:Boom}}</Text>",
			"value provider said no"},
		{"provider returns no handle",
			"<Text>{{v:Nothing}}</Text>",
			"returned no handle"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withValues(t, &echoProvider{})
			_, err := loadValue(t, tc.body, map[string]any{"Out": prop.NewSource("")})
			if err == nil {
				t.Fatalf("expected a load error mentioning %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

// An unregistered value namespace names the call the host has to make.
// Registration is the capability grant, so the error is the grant's
// documentation.
func TestUnregisteredValueNamespaceNamesRegisterValues(t *testing.T) {
	_, err := loadValue(t, "<Text>{{v:Echo `x`}}</Text>", map[string]any{})
	if err == nil {
		t.Fatal("an ungranted value namespace loaded")
	}
	for _, want := range []string{"no registered value provider", "markup.RegisterValues"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not mention %q", err, want)
		}
	}
}

// The push/pull crossover, both directions. These two messages are the
// ones a person actually hits, because the natural mistake is to reach
// for the half that exists.
func TestHandlerNamespaceInValuePositionSaysWhichHalfIsMissing(t *testing.T) {
	r := &recorder{}
	RegisterHandlers(testURI, r)
	defer RegisterHandlers(testURI, nil)
	src := `<Gooey xmlns:t="` + testURI + `"><Text>{{t:Run ` + "`x`" + `}}</Text></Gooey>`
	_, err := Build([]byte(src), &Context{Values: map[string]any{}, Dispatcher: gooey.NewDispatcher()})
	if err == nil {
		t.Fatal("a handler namespace resolved in a value position")
	}
	for _, want := range []string{"registered as a HANDLER namespace", "event attribute"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not mention %q", err, want)
		}
	}
}

func TestValueNamespaceOnAnEventAttributeIsALoadError(t *testing.T) {
	withValues(t, &echoProvider{})
	_, err := loadValue(t, "<Button Content=\"go\" Click=\"{{v:Echo `x`}}\"/>", map[string]any{})
	if err == nil {
		t.Fatal("a value namespace bound to Click")
	}
	if !strings.Contains(err.Error(), "no registered handler provider") {
		t.Fatalf("error %q does not name the missing handler grant", err)
	}
}

// The regression pin for the silent-drop bug this seam was built on
// top of: before scan.go, every one of these loaded clean and painted
// its own source text on the terminal.
func TestUnresolvableBraceExpressionIsALoadError(t *testing.T) {
	cases := []struct{ name, body, want string }{
		{"ungranted namespace call", "<Text>{{env:Get `HOME`}}</Text>", "undeclared namespace prefix"},
		{"not a binding at all", "<Text>{{ nonsense }}</Text>", "neither a binding"},
		{"empty path", "<Text>{{.}}</Text>", "not a valid binding path"},
		{"path with a dash", "<Text>{{.Bad-Name}}</Text>", "not a valid binding path"},
		{"unterminated braces", "<Text>{{.Name</Text>", "unterminated {{"},
		{"unterminated literal", "<Text>{{v:Echo `oops}}</Text>", "unterminated ` literal"},
		{"in a Content attribute", "<Button Content=\"{{ nope }}\"/>", "neither a binding"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withValues(t, &echoProvider{})
			_, err := loadValue(t, tc.body, map[string]any{"Name": prop.NewSource("n")})
			if err == nil {
				t.Fatalf("%s loaded clean — it should be a load error mentioning %q", tc.body, tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

// The scanner respects backtick literals, so a literal may contain the
// closing braces without ending the expression early. This is why the
// scan is hand-rolled instead of a regexp.
func TestBacktickLiteralMayContainBraces(t *testing.T) {
	withValues(t, &echoProvider{})
	w, err := loadValue(t, "<Text>{{v:Echo `}}` `{{`}}</Text>", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	c := gooey.NewComposer(w, 20, 1)
	c.Frame()
	if got := cellRow(c.Cells(), 0); got != "}}|{{" {
		t.Fatalf("row = %q, want %q", got, "}}|{{")
	}
}

// Content with no braces at all still returns no handle, which is what
// keeps a static Text off the dependency graph entirely.
func TestPureLiteralContentStaysStatic(t *testing.T) {
	h, err := bindText("just text", &Context{Values: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	if h != nil {
		t.Fatal("literal content produced a computed")
	}
}

func TestRegisteredValuesListsGrants(t *testing.T) {
	withValues(t, &echoProvider{})
	found := false
	for _, u := range RegisteredValues() {
		if u == valueURI {
			found = true
		}
	}
	if !found {
		t.Fatalf("RegisteredValues()=%v, missing %q", RegisteredValues(), valueURI)
	}
	RegisterValues(valueURI, nil)
	for _, u := range RegisteredValues() {
		if u == valueURI {
			t.Fatal("revoking the grant left the URI registered")
		}
	}
}

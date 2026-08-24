package markup

import (
	"errors"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

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

// An unterminated brace expression cannot know where it ends, so both
// error paths quote from the opening `{{` to the end of the remaining
// content. Uncapped, an early stray brace in a large page put the rest of
// the file into a message read while staring at one attribute (#232).
//
// The discriminating case is the SHORT one: a cap that always elides
// would satisfy every "the message is bounded" assertion and would have
// made the common error worse, not better.
func TestUnterminatedExpressionQuotesABoundedSnippet(t *testing.T) {
	const limit = errSnippetRunes

	for _, tc := range []struct {
		name    string
		in      string
		want    string // must appear
		absent  string // must not
		elision bool
	}{
		{
			name:    "short enough to quote whole",
			in:      "{{.Name",
			want:    `"{{.Name"`,
			absent:  "more characters",
			elision: false,
		},
		{
			name:    "run-on line is cut at the rune cap",
			in:      "{{" + strings.Repeat("x", 500) + "TAIL",
			want:    "(+" + strconv.Itoa(2+500+4-limit) + " more characters)",
			absent:  "TAIL",
			elision: true,
		},
		{
			name:    "the rest of the document is not quoted",
			in:      "{{.Name\nSECRET REST OF FILE",
			want:    `"{{.Name"`,
			absent:  "SECRET",
			elision: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := errSnippet(tc.in)
			if !strings.Contains(got, tc.want) {
				t.Errorf("errSnippet(…) = %s, which does not contain %s", got, tc.want)
			}
			if strings.Contains(got, tc.absent) {
				t.Errorf("errSnippet(…) = %s, which still contains %q", got, tc.absent)
			}
			if elided := strings.Contains(got, "more characters"); elided != tc.elision {
				t.Errorf("errSnippet(…) = %s; elided=%v, want %v", got, elided, tc.elision)
			}
		})
	}
}

// The cap counts runes, and cutting on bytes instead would quote a
// replacement glyph the author never typed — an error message that
// misreports the text it is complaining about. Every rune here is three
// bytes, so a byte cap lands mid-character by construction.
func TestBoundedSnippetDoesNotSplitARune(t *testing.T) {
	got := errSnippet("{{" + strings.Repeat("日", 200))
	if !utf8.ValidString(got) {
		t.Fatalf("errSnippet produced invalid UTF-8: %q", got)
	}
	if strings.ContainsRune(got, utf8.RuneError) {
		t.Errorf("errSnippet(…) = %s — it cut a multi-byte character in half", got)
	}
	// The quoted body is exactly the cap, so the cut is on runes and not
	// on the bytes that happen to reach the same offset.
	quoted, rest, ok := strings.Cut(got, "… (+")
	if !ok {
		t.Fatalf("errSnippet(…) = %s, which never elided; the cap did not fire", got)
	}
	body, err := strconv.Unquote(quoted)
	if err != nil {
		t.Fatalf("errSnippet's quoted half %q does not unquote: %v", quoted, err)
	}
	if n := utf8.RuneCountInString(body); n != errSnippetRunes {
		t.Errorf("quoted %d rune(s), want %d (the byte cap would give %d)",
			n, errSnippetRunes, errSnippetRunes/3)
	}
	if want := strconv.Itoa(2 + 200 - errSnippetRunes); !strings.HasPrefix(rest, want+" ") {
		t.Errorf("elided count in %s is not %s — it is counting bytes, not characters", got, want)
	}
}

// Both call sites, end to end through the loader, because the cap is
// worth nothing if only one path uses it.
func TestUnterminatedLoadErrorsAreBounded(t *testing.T) {
	tail := "NEEDLE"
	for _, tc := range []struct{ name, body, kind string }{
		{"unterminated braces", "<Text>{{.Name " + strings.Repeat("y", 400) + tail + "</Text>", "unterminated {{"},
		{"unterminated literal", "<Text>{{v:Echo `oops " + strings.Repeat("y", 400) + tail + "</Text>", "unterminated ` literal"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			withValues(t, &echoProvider{})
			_, err := loadValue(t, tc.body, map[string]any{"Name": prop.NewSource("n")})
			if err == nil {
				t.Fatal("loaded clean; this should be a load error")
			}
			if !strings.Contains(err.Error(), tc.kind) {
				t.Fatalf("error %q is not the one under test", err)
			}
			if strings.Contains(err.Error(), tail) {
				t.Errorf("the load error still reaches the end of the document:\n%s", err)
			}
			if !strings.Contains(err.Error(), "more characters") {
				t.Errorf("the load error was not capped at all:\n%s", err)
			}
		})
	}
}

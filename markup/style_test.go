package markup

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

func styleCtx() *Context {
	return &Context{
		Values: map[string]any{
			"Text":  prop.NewSource("hello"),
			"Err":   prop.NewSource(""),
			"Live":  prop.NewSource(render.Style{Bold: true}),
			"Click": gooey.Command(func() {}),
		},
		Styles: map[string]render.Style{
			"dim": {Dim: true},
		},
	}
}

func loadStylePage(t *testing.T, body string) error {
	t.Helper()
	fsys := fstest.MapFS{"page.gooey": {Data: []byte("<Gooey>\n" + body + "\n</Gooey>")}}
	_, err := Load(fsys, "page.gooey", styleCtx())
	return err
}

// styleSites is every place a style NAME is resolved. All six were bare
// map indexes, so all six are listed here by construction rather than by
// testing one and trusting the rest — a fix applied to five of six leaves
// the same silent hole, and nothing else in the suite would notice.
var styleSites = []struct {
	name string
	good string // the same markup with a registered style name
	bad  string // ... and with one that was never registered
}{
	{
		"Style, the common path through bindStyle",
		`<Text Style="dim">x</Text>`,
		`<Text Style="dmi">x</Text>`,
	},
	{
		"TextBox AccentStyle",
		`<TextBox Text="{{.Text}}" AccentStyle="dim"/>`,
		`<TextBox Text="{{.Text}}" AccentStyle="dmi"/>`,
	},
	{
		"TextBox InvalidStyle",
		`<TextBox Text="{{.Text}}" InvalidStyle="dim"/>`,
		`<TextBox Text="{{.Text}}" InvalidStyle="dmi"/>`,
	},
	{
		"ToastHost Style",
		`<ToastHost Style="dim"/>`,
		`<ToastHost Style="dmi"/>`,
	},
	{
		// On a Button, not a Text. <Text> is a LEAF, and a leaf discards
		// its children silently — so the nested Tooltip is never built and
		// the case tests nothing. Cost me a red run to notice, which is
		// the same silent-drop class this whole fix is about.
		"Tooltip Style",
		`<VStack><Button Content="save" Click="{{.Click}}"><Tooltip Text="tip" Style="dim"/></Button><AdornmentLayer/></VStack>`,
		`<VStack><Button Content="save" Click="{{.Click}}"><Tooltip Text="tip" Style="dmi"/></Button><AdornmentLayer/></VStack>`,
	},
	{
		"ValidationMarker Style",
		`<ValidationMarker Error="{{.Err}}" Style="dim"/>`,
		`<ValidationMarker Error="{{.Err}}" Style="dmi"/>`,
	},
}

// The bug: a misspelled style name loaded clean and painted unstyled, so
// the symptom read as somebody's deliberate choice. A mistyped BINDING in
// the same file failed the load instantly — that inconsistency, in the
// attribute most likely to be hand-typed, is what makes it worth fixing.
func TestAnUnregisteredStyleNameIsALoadError(t *testing.T) {
	for _, s := range styleSites {
		t.Run(s.name, func(t *testing.T) {
			err := loadStylePage(t, s.bad)
			if err == nil {
				t.Fatal("a style name that was never registered loaded clean — the page paints unstyled and nothing says why")
			}
			if !strings.Contains(err.Error(), "dmi") {
				t.Errorf("error %q does not quote the offending name", err)
			}
		})
	}
}

// The near-miss twin, and it is not optional: an implementation that
// rejected EVERY style name would pass the test above completely.
func TestARegisteredStyleNameStillLoads(t *testing.T) {
	for _, s := range styleSites {
		t.Run(s.name, func(t *testing.T) {
			if err := loadStylePage(t, s.good); err != nil {
				t.Fatalf("a registered style name failed to load: %v", err)
			}
		})
	}
}

// Absent and empty both mean "no style" and must stay valid — the fix
// rejects a name that was typed and does not exist, not the absence of
// one. Without this, adding the check would make Style effectively
// required everywhere.
func TestAnAbsentOrEmptyStyleIsNotAnError(t *testing.T) {
	for _, body := range []string{
		`<Text>x</Text>`,
		`<Text Style="">x</Text>`,
		`<ToastHost/>`,
		`<ValidationMarker Error="{{.Err}}"/>`,
	} {
		t.Run(body, func(t *testing.T) {
			if err := loadStylePage(t, body); err != nil {
				t.Fatalf("%s: %v", body, err)
			}
		})
	}
}

// A BOUND style is a binding, not a name, and must not be looked up in
// the name table — that path is what makes a style reactive.
func TestABoundStyleIsNotResolvedAsAName(t *testing.T) {
	if err := loadStylePage(t, `<Text Style="{{.Live}}">x</Text>`); err != nil {
		t.Fatalf("a bound style was rejected: %v", err)
	}
	// ... and a binding to a name that does not exist in Values still
	// fails, through the binding path, with the binding's own message.
	err := loadStylePage(t, `<Text Style="{{.Nope}}">x</Text>`)
	if err == nil {
		t.Fatal("a binding to an unknown value loaded clean")
	}
	if strings.Contains(err.Error(), "no style named") {
		t.Fatalf("error %q treats a binding as a style name", err)
	}
}

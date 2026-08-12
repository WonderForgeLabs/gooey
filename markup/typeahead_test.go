package markup

import (
	"strings"
	"testing"
	"time"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/prop"
)

// typeAheadPage is listPage with the attachment declared inside the list,
// which is where it belongs: <ItemsView>'s children are attachments.
const typeAheadPage = `<Gooey>
  <ItemsView Items="{{.Posts}}" Selected="{{.Sel}}">
    <TypeAhead Key="Title" Search="{{.Typed}}" NoMatch="{{.Missed}}" Timeout="750ms"/>
    <ItemsView.ItemTemplate>
      <HStack Gap="1">
        <Text>{{.Title}}</Text>
        <Text Style="dim">{{.Date}}</Text>
      </HStack>
    </ItemsView.ItemTemplate>
  </ItemsView>
</Gooey>`

func TestTypeAheadBuildsAsAnAttachment(t *testing.T) {
	p := postSource(post{"first", "jan"}, post{"second", "feb"})
	typed, missed := prop.NewSource(""), prop.NewSource(false)
	ctx := listCtx(p, map[string]any{"Typed": typed, "Missed": missed})
	w, err := Build([]byte(typeAheadPage), ctx)
	if err != nil {
		t.Fatal(err)
	}
	v := w.(*components.ItemsView)
	att := v.Attachments()
	if len(att) != 1 {
		t.Fatalf("list has %d attachments, want 1", len(att))
	}
	ta, ok := att[0].(*components.TypeAhead)
	if !ok {
		t.Fatalf("attachment is %T, want *components.TypeAhead", att[0])
	}
	if ta.Key != "Title" {
		t.Errorf("Key = %q, want Title", ta.Key)
	}
	if got, want := ta.Timeout, 750*time.Millisecond; got != want {
		t.Errorf("Timeout = %v, want %v", got, want)
	}
	// The bindings are live handles onto the viewmodel's own properties,
	// not copies — that is what lets a page render the search state.
	if ta.Search == nil || ta.NoMatch == nil {
		t.Fatal("Search/NoMatch bindings were dropped")
	}
	typed.Set("se")
	if ta.Search.Get() != "se" {
		t.Error("Search is not a live handle onto the bound property")
	}
}

// End to end through the real dispatcher: markup in, keystroke, selection
// moved. This is the test that would fail if the attachment key seam in
// FocusManager.Dispatch were removed.
func TestTypeAheadSearchesFromMarkup(t *testing.T) {
	p := postSource(post{"alpha", "jan"}, post{"beta", "feb"}, post{"gamma", "mar"})
	sel := prop.NewSource(0)
	typed := prop.NewSource("")
	ctx := listCtx(p, map[string]any{"Typed": typed, "Missed": prop.NewSource(false)})
	ctx.Values["Sel"] = sel

	w, err := Build([]byte(typeAheadPage), ctx)
	if err != nil {
		t.Fatal(err)
	}
	c := gooey.NewComposer(w, 30, 3)
	c.Frame()

	if !c.HandleKey(inputRune('g')) {
		t.Fatal("the keystroke was not consumed by the type-ahead")
	}
	if sel.Get() != 2 {
		t.Fatalf("Selected = %d, want 2 (gamma)", sel.Get())
	}
	if typed.Get() != "g" {
		t.Errorf("Search = %q, want \"g\"", typed.Get())
	}
}

// A misplaced <TypeAhead> fails the LOAD rather than sitting inert in a
// tree, the same way a misplaced <Validate> does.
func TestTypeAheadOnANonListIsALoadError(t *testing.T) {
	src := `<Gooey><VStack><TypeAhead Key="Title"/></VStack></Gooey>`
	_, err := Build([]byte(src), &Context{Values: map[string]any{}})
	if err == nil {
		t.Fatal("expected a load-time error for <TypeAhead> outside a list")
	}
	if !strings.Contains(err.Error(), "belongs on an <ItemsView>") {
		t.Errorf("error %q does not say where it belongs", err)
	}
}

func TestTypeAheadKeyAndTimeoutAreValidatedAtLoad(t *testing.T) {
	cases := []struct{ name, attrs, want string }{
		{"missing key", ``, "needs a Key"},
		{"blank key", `Key="  "`, "needs a Key"},
		{"unparseable timeout", `Key="Title" Timeout="soon"`, "Timeout"},
		{"zero timeout", `Key="Title" Timeout="0s"`, "positive"},
		{"negative timeout", `Key="Title" Timeout="-1s"`, "positive"},
		{"unknown attribute", `Key="Title" Fuzzy="true"`, "Fuzzy"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := `<Gooey>
  <ItemsView Items="{{.Posts}}" Selected="{{.Sel}}">
    <TypeAhead ` + tc.attrs + `/>
    <ItemsView.ItemTemplate><Text>{{.Title}}</Text></ItemsView.ItemTemplate>
  </ItemsView>
</Gooey>`
			p := postSource(post{"first", "jan"})
			_, err := Build([]byte(src), listCtx(p, nil))
			if err == nil {
				t.Fatal("expected a load-time error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

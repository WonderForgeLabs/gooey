package markup

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/prop"
)

// The two markup forms of #92 on one page: the child form on a Button
// (with a Delay and a validated Gesture hint), the shorthand on a Text,
// and the AdornmentLayer declared last. Everything below is pure markup
// — the Include tier — with no code-behind anywhere.
const tooltipPage = `<Gooey>
  <VStack>
    <Button Content="save" Click="{{.Save}}">
      <Tooltip Text="{{.Hint}}" Delay="250ms" Gesture="ctrl+s"/>
    </Button>
    <Text Tooltip="just a label">plain</Text>
    <AdornmentLayer/>
  </VStack>
</Gooey>`

func tooltipCtx() *Context {
	return &Context{Values: map[string]any{
		"Save": gooey.Command(func() {}),
		"Hint": prop.NewSource("write the file"),
	}}
}

func attachmentsOf(w gooey.Component) []gooey.Component {
	a, ok := w.(gooey.Attacher)
	if !ok {
		return nil
	}
	return a.Attachments()
}

func TestTooltipBothMarkupFormsBuild(t *testing.T) {
	root, err := Build([]byte(tooltipPage), tooltipCtx())
	if err != nil {
		t.Fatal(err)
	}
	stack, ok := root.(*components.VStack)
	if !ok {
		t.Fatalf("root is %T, want *components.VStack", root)
	}
	kids := stack.ChildComponents()
	if len(kids) != 3 {
		t.Fatalf("the stack has %d children, want 3", len(kids))
	}
	if _, ok := kids[2].(*components.AdornmentLayer); !ok {
		t.Fatalf("the last child is %T, want *components.AdornmentLayer", kids[2])
	}

	var btnTip, textTip *components.Tooltip
	for _, at := range attachmentsOf(kids[0]) {
		if tip, ok := at.(*components.Tooltip); ok {
			btnTip = tip
		}
	}
	for _, at := range attachmentsOf(kids[1]) {
		if tip, ok := at.(*components.Tooltip); ok {
			textTip = tip
		}
	}
	if btnTip == nil {
		t.Fatal("the child-form <Tooltip> did not attach to the Button")
	}
	if btnTip.Delay.Milliseconds() != 250 {
		t.Fatalf("Delay = %v, want 250ms", btnTip.Delay)
	}
	if btnTip.Gesture != "ctrl+s" {
		t.Fatalf("Gesture = %q, want the canonical %q", btnTip.Gesture, "ctrl+s")
	}
	if btnTip.Text.Get() != "write the file" {
		t.Fatalf("bound Text = %q", btnTip.Text.Get())
	}
	if textTip == nil {
		t.Fatal("the Tooltip=\"...\" shorthand did not attach to the Text")
	}
	if textTip.Text.Get() != "just a label" {
		t.Fatalf("shorthand Text = %q", textTip.Text.Get())
	}
}

// The whole path, from markup to cells: hover the button, the tip shows
// in the layer with its gesture hint; hover away, the screen restores.
// (Un-started composition ⇒ no delay timer ⇒ the show is immediate.)
func TestTooltipShowsFromPureMarkup(t *testing.T) {
	root, err := Build([]byte(tooltipPage), tooltipCtx())
	if err != nil {
		t.Fatal(err)
	}
	c := gooey.NewComposer(root, 40, 5)
	c.Frame()
	before := markupScreen(c, 40, 5)

	c.HandleMouse(input.MouseEvent{Kind: input.MouseMove, X: 2, Y: 0})
	c.Frame()
	after := markupScreen(c, 40, 5)
	if !strings.Contains(after, " write the file ") || !strings.Contains(after, "ctrl+s") {
		t.Fatalf("the tooltip (with hint) is not on screen:\n%s", after)
	}

	c.HandleMouse(input.MouseEvent{Kind: input.MouseMove, X: 30, Y: 4})
	c.Frame()
	if got := markupScreen(c, 40, 5); got != before {
		t.Fatalf("dismissing the tooltip left a scar.\nbefore:\n%s\nafter:\n%s", before, got)
	}
}

// Load-time strictness: a bad gesture, children on the layer, and a
// visual child inside a Button are all errors you hear at startup.
func TestTooltipMarkupLoadErrors(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"bad gesture",
			`<Gooey><Button Content="x"><Tooltip Text="t" Gesture="ctrl+"/></Button></Gooey>`,
			"Gesture"},
		{"layer children",
			`<Gooey><AdornmentLayer><Text>x</Text></AdornmentLayer></Gooey>`,
			"takes no children"},
		{"button visual child",
			`<Gooey><Button Content="x"><Text>y</Text></Button></Gooey>`,
			"no visual children"},
	}
	for _, tc := range cases {
		_, err := Build([]byte(tc.src), &Context{})
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want mention of %q", tc.name, err, tc.want)
		}
	}
}

// The shorthand decorates the INSTANCE of a markup-only control and does
// not leak into the control's context as a value.
func TestTooltipShorthandDoesNotCrossTheControlBoundary(t *testing.T) {
	inc := fstest.MapFS{
		"card.gooey": {Data: []byte(`<Gooey><Text>{{.Title}}</Text></Gooey>`)},
	}
	root, err := Build([]byte(`<Gooey><Card Title="hey" Tooltip="a card"/></Gooey>`),
		&Context{Includes: inc})
	if err != nil {
		t.Fatal(err)
	}
	var tip *components.Tooltip
	for _, at := range attachmentsOf(root) {
		if tt, ok := at.(*components.Tooltip); ok {
			tip = tt
		}
	}
	if tip == nil {
		t.Fatal("the shorthand did not attach to the control instance")
	}
	if tip.Text.Get() != "a card" {
		t.Fatalf("instance tooltip text = %q", tip.Text.Get())
	}
}

func markupScreen(c *gooey.Composer, w, h int) string {
	var sb strings.Builder
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			sb.WriteRune(c.Cells().At(x, y).Rune)
		}
		sb.WriteByte('\n')
	}
	return sb.String()
}

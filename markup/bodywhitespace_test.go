package markup

import (
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

// Whitespace in an element BODY.
//
// The bug these pin: every body went through strings.TrimSpace, so
// leading and trailing spaces could not be expressed in markup AT ALL.
// An ASCII drawing written one <Text> per line slid every line to
// column 0, silently — no error, no warning, the text just moved. The
// only workaround was abandoning bodies for a <Canvas> and an explicit
// Canvas.Left per line.
//
// The fix needs no opt-in attribute because the discriminator is
// already in the data, and TestDocumentIndentationDoesNotReachTheBody
// below is the one that says why: the file's indentation lands BEFORE
// the start tag, so the only thing that puts whitespace inside a body
// is the author wrapping it onto its own line — which is visible as a
// NEWLINE. That test is load-bearing; if it ever fails, the whole
// no-opt-in design is invalid and the rule has to become explicit.
//
// See bodyText in toolkit.go for the rule itself.

func bodyOf(t *testing.T, ctx *Context, src string) string {
	t.Helper()
	if ctx.Named == nil {
		ctx.Named = map[string]gooey.Component{}
	}
	buildOne(t, src, ctx)
	txt, ok := ctx.Named["t"].(*components.Text)
	if !ok {
		t.Fatalf("Named[t] is %T, want *components.Text", ctx.Named["t"])
	}
	return txt.Content.Get()
}

// A body written on one line is CONTENT, verbatim, spaces and all.
// Three shapes, because leading and trailing whitespace failed
// identically before and could regress independently.
func TestAOneLineBodyIsVerbatim(t *testing.T) {
	for _, tc := range []struct {
		name, body, want string
	}{
		{"leading", `<Text Name="t">    Hello</Text>`, "    Hello"},
		{"trailing", `<Text Name="t">Hello    </Text>`, "Hello    "},
		{"both", `<Text Name="t">  Hello  </Text>`, "  Hello  "},
		{"neither", `<Text Name="t">Hello</Text>`, "Hello"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := bodyOf(t, &Context{Values: map[string]any{}}, doc(tc.body)); got != tc.want {
				t.Errorf("body = %q, want %q", got, tc.want)
			}
		})
	}
}

// THE REGRESSION PIN, and the one that would hurt most to lose.
//
// Markup is indented, and a body the author wrapped across lines for
// readability must still render trimmed. Blanket-removing the trim
// would break the whole tree; this is what says so.
func TestAWrappedBodyIsStillTrimmed(t *testing.T) {
	src := `<Gooey xmlns="wonderforge.io/gooey/2026">
      <VStack>
        <Border>
          <Text Name="t">
            Hello
          </Text>
        </Border>
      </VStack>
    </Gooey>`
	if got := bodyOf(t, &Context{Values: map[string]any{}}, src); got != "Hello" {
		t.Errorf("body = %q, want %q: a body wrapped across lines is source formatting, not content", got, "Hello")
	}
}

// THE LOAD-BEARING ONE — the fact the no-opt-in design rests on.
//
// Nesting the element ten levels deep must not put a single space in
// its body: the document's indentation lands before the start tag. If
// this ever fails, "one line means verbatim" is unsound, because an
// author's deliberate spaces would be indistinguishable from the
// pretty-printer's — and the rule would have to become an explicit
// xml:space="preserve" instead.
func TestDocumentIndentationDoesNotReachTheBody(t *testing.T) {
	deep := `<Gooey xmlns="wonderforge.io/gooey/2026">
	  <VStack>
	    <Border>
	      <Grid Rows="1" Cols="1*">
	        <Canvas>
	          <VStack>
	            <HStack>
	              <Text Name="t">Hello</Text>
	            </HStack>
	          </VStack>
	        </Canvas>
	      </Grid>
	    </Border>
	  </VStack>
	</Gooey>`
	got := bodyOf(t, &Context{Values: map[string]any{}}, deep)
	if got != "Hello" {
		t.Fatalf("body = %q, want %q: document indentation leaked into the body, "+
			"which invalidates the one-line-means-verbatim rule entirely", got, "Hello")
	}
}

// A one-line body of nothing but spaces is a DELIBERATE SPACER, not an
// empty element. This is a choice, not a fallout: apps/wysiwyg's
// properties header writes <Text Width="1" Style="sel"> </Text> as the
// unlabelled lead column, and it means one styled blank cell.
func TestAWhitespaceOnlyOneLineBodyIsASpacer(t *testing.T) {
	if got := bodyOf(t, &Context{Values: map[string]any{}}, doc(`<Text Name="t"> </Text>`)); got != " " {
		t.Errorf("body = %q, want %q: a lone space on one line is content", got, " ")
	}
}

// ...and the cell-level half of that, because a content assertion says
// nothing about what reaches the screen. components.Text paints
// len(content) cells, so an empty body paints NOTHING and the styled
// header gap stays at the ancestor's background. This is the one site
// in the tree whose rendered cells actually change.
//
// The STYLE assertion is the discriminating half and the rune one is
// not — measured, not assumed. Under a revert to always-trim this test
// fails on Style while the rune check still PASSES, because the leaf
// pre-cleared its bounds to a space anyway. Do not "simplify" this to
// the rune check; that leaves a test that cannot fail.
func TestTheSpacerBodyPaintsAStyledCell(t *testing.T) {
	sel := render.Style{Fg: render.RGB(0x10, 0x20, 0x30), Bg: render.RGB(0x40, 0x50, 0x60)}
	ctx := &Context{
		Values: map[string]any{},
		Styles: map[string]render.Style{"sel": sel},
	}
	// The wysiwyg properties header, reduced to the part under test.
	w := buildOne(t, doc(`<HStack Gap="0">`+
		`<Text Width="1" Style="sel"> </Text>`+
		`<Text Width="4" Style="sel">ATTR</Text>`+
		`</HStack>`), ctx)
	c := gooey.NewComposer(w, 10, 1)
	f, _ := c.Frame()

	got := f.Cells.At(0, 0)
	if got.Rune != ' ' {
		t.Errorf("lead cell rune = %q, want a space", got.Rune)
	}
	if got.Style != sel {
		t.Errorf("lead cell style = %+v, want the %q style %+v: the spacer painted no cell, "+
			"so the header gap fell through to the ancestor background", got.Style, "sel", sel)
	}
}

// A binding surrounded by spaces must stay LIVE. The trim used to run
// BEFORE bindText, so " {{.Title}} " arrived as a pure binding; keeping
// the spaces makes it literal+binding+literal instead, and a naive fix
// could silently downgrade it to a literal string.
//
// This is the shape twelve real bodies in the tree have —
// handlers/temporal/internal/wizard/ui/stage-*.gooey write
// <Text>  ticket:    {{.Ticket}}</Text> and were rendering flush left.
func TestABoundBodyWithSurroundingSpacesStaysLiveAndKeepsThem(t *testing.T) {
	src := prop.NewSource("T-1")
	ctx := &Context{Values: map[string]any{"Ticket": src}}
	if got := bodyOf(t, ctx, doc(`<Text Name="t">  ticket:    {{.Ticket}} </Text>`)); got != "  ticket:    T-1 " {
		t.Fatalf("body = %q, want %q", got, "  ticket:    T-1 ")
	}
	txt := ctx.Named["t"].(*components.Text)
	src.Set("T-2")
	if got := txt.Content.Get(); got != "  ticket:    T-2 " {
		t.Errorf("body = %q after the source changed, want %q: the body was flattened to a literal",
			got, "  ticket:    T-2 ")
	}
}

// The rule belongs to BODIES, not to <Text>. <Arg> is the other element
// whose content is its body, and it shares the helper — so an argv
// token is not quietly rewritten either, and there is only ever one
// rule to keep in step.
func TestAnArgBodyFollowsTheSameRule(t *testing.T) {
	src := `<Gooey xmlns="wonderforge.io/gooey/2026">
	  <VStack>
	    <Companion Name="w" Path="echo">
	      <Companion.Args>
	        <Arg>  --lead</Arg>
	        <Arg>trail  </Arg>
	        <Arg>
	          wrapped
	        </Arg>
	      </Companion.Args>
	    </Companion>
	    <Text>ui</Text>
	  </VStack>
	</Gooey>`
	ctx := &Context{Values: map[string]any{}, Named: map[string]gooey.Component{}}
	buildOne(t, src, ctx)
	cmp, ok := ctx.Named["w"].(*components.Companion)
	if !ok {
		t.Fatalf("Named[w] is %T, want *components.Companion", ctx.Named["w"])
	}
	want := []string{"  --lead", "trail  ", "wrapped"}
	if len(cmp.Args) != len(want) {
		t.Fatalf("got %d args, want %d", len(cmp.Args), len(want))
	}
	for i, w := range want {
		if got := cmp.Args[i].Get(); got != w {
			t.Errorf("arg %d = %q, want %q", i, got, w)
		}
	}
}

// The declaration must not outlive the behaviour. BodySpec.Doc asserted
// "Trimmed, so leading and trailing spaces cannot be expressed" as
// settled fact, and that string is what the wysiwyg palette shows a
// user. A doc that survives its own fix is the failure this repo treats
// as a defect in its own right.
func TestTheDeclaredBodyDocDoesNotStillClaimBodiesAreTrimmed(t *testing.T) {
	var body *BodySpec
	for _, e := range (&Context{}).Catalog() {
		if e.Name == "Text" {
			body = e.Body
		}
	}
	if body == nil {
		t.Fatal("<Text> declares no Body")
	}
	if body.Doc == "" {
		t.Fatal("the Body carries no Doc, so the palette row has nothing to say")
	}
	if strings.Contains(body.Doc, "cannot be expressed") {
		t.Errorf("BodySpec.Doc still says leading and trailing spaces %q: %q",
			"cannot be expressed", body.Doc)
	}
}

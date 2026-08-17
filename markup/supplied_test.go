package markup

import (
	"os"
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

// The OPTIONAL half of "absent is reported as absent", and the bug that
// the required half created on its way in.
//
// requiredAttr counts empty as absent, deliberately: `Checked=""` is not
// a binding, and the author's mistake is the same as omitting it. But
// nine optional handle attributes were guarded by a bare
// `if _, ok := e.Attrs[name]; ok`, which counts empty as PRESENT. The
// two disagreed, and the disagreement fell out as a load error telling
// the author to supply an attribute they had just supplied:
//
//	<TextBox Text="{{.Name}}" Error=""/>
//	  -> markup: <TextBox>: attribute Error is required
//
// Error is optional on TextBox. suppliedAttr is the one definition that
// makes both halves agree.
//
// FOUND BY THE MERGE GATE on #290, and its site list was short: it named
// six, and buildItemsView's Selected was a seventh with the identical
// guard. That is the ordinary failure mode of a hand-listed set, so the
// table below is built from the ELEMENTS, and the guard-shape audit that
// found the extra site is pinned separately below.

func suppliedCtx() *Context {
	return &Context{
		Values: map[string]any{
			"Name": prop.NewSource("n"),
			"N":    prop.NewSource(1),
			"Err":  prop.NewSource("boom"),
			"Items": components.Items(prop.NewSource([]string{"a", "b"}),
				func(s string) map[string]any { return map[string]any{"S": s} }),
		},
		Styles: map[string]render.Style{},
		Named:  map[string]gooey.Component{},
	}
}

// Every element with an optional handle attribute, written empty.
func TestAnOptionalAttributeWrittenEmptyMeansOmitted(t *testing.T) {
	for _, c := range []struct{ name, src string }{
		{"TextBox.Error", `<TextBox Text="{{.Name}}" Error=""/>`},
		{"Companion.Error", `<Companion Name="w" Path="/bin/true" Error=""/>`},
		{"ProgressBar.Indeterminate", `<ProgressBar Value="{{.N}}" Indeterminate=""/>`},
		{"Spinner.Enabled", `<Spinner Enabled=""/>`},
		{"Timer.Enabled", `<Timer Interval="600ms" Enabled=""/>`},
		{"TypeAhead.Search", `<TypeAhead Key="S"Search=""/>`},
		{"TypeAhead.NoMatch", `<TypeAhead Key="S"NoMatch=""/>`},
		{"Tabs.Selected", `<Tabs Selected=""><Tab Header="a"><Text>x</Text></Tab></Tabs>`},
		{"ItemsView.Selected", `<ItemsView Items="{{.Items}}" Selected="">` +
			`<ItemsView.ItemTemplate><Text>{{.S}}</Text></ItemsView.ItemTemplate></ItemsView>`},
	} {
		t.Run(c.name, func(t *testing.T) {
			src := `<Gooey xmlns="wonderforge.io/gooey/2026">` + c.src + `</Gooey>`
			if _, err := Build([]byte(src), suppliedCtx()); err != nil {
				t.Errorf("an optional attribute written empty was a load error: %v", err)
			}
		})
	}
}

// THE DISCRIMINATION HALF. Without it the test above passes against a
// requiredAttr that accepts empty from everyone, which would put the
// original bad message back on <Checkbox Checked=""/> by way of
// BindingValue — the exact regression this pair exists to prevent.
func TestARequiredAttributeWrittenEmptyIsStillAnOmission(t *testing.T) {
	src := `<Gooey xmlns="wonderforge.io/gooey/2026"><Checkbox Checked=""/></Gooey>`
	_, err := Build([]byte(src), suppliedCtx())
	if err == nil {
		t.Fatal("<Checkbox Checked=\"\"/> loaded clean; empty is now accepted everywhere")
	}
	if !strings.Contains(err.Error(), "attribute Checked is required") {
		t.Errorf("error is not the omission message: %v", err)
	}
}

// The other way the fix could be wrong: skipping the attribute is only
// correct if a SUPPLIED one is still bound. Otherwise every optional
// handle goes quietly dead and no error says so.
func TestASuppliedOptionalAttributeStillBinds(t *testing.T) {
	ctx := suppliedCtx()
	src := `<Gooey xmlns="wonderforge.io/gooey/2026">` +
		`<TextBox Name="tb" Text="{{.Name}}" Error="{{.Err}}"/></Gooey>`
	if _, err := Build([]byte(src), ctx); err != nil {
		t.Fatalf("load: %v", err)
	}
	tb, ok := ctx.Named["tb"].(*components.TextBox)
	if !ok {
		t.Fatalf("Named[tb] is %T, want *components.TextBox", ctx.Named["tb"])
	}
	if tb.Error == nil {
		t.Fatal("a supplied Error was not bound at all")
	}
	if got := tb.Error.Get(); got != "boom" {
		t.Fatalf("Error = %q, want %q", got, "boom")
	}
	// Live, not flattened: the page property is the component's.
	ctx.Values["Err"].(*prop.Property[string]).Set("later")
	if got := tb.Error.Get(); got != "later" {
		t.Errorf("Error = %q after the source changed, want %q", got, "later")
	}
}

// THE GUARD-SHAPE AUDIT, which is what caught the site the gate's list
// missed. The bare key check is the bug's shape, so no optional handle
// may reuse it — a new element copying the old idiom is the way this
// comes back, and it would come back silently.
//
// What makes a guard wrong is not its shape alone but where it LEADS: a
// bare key check in front of BoundStyle or optBool is fine, because
// neither reaches requiredAttr. So this pairs the guard with the call it
// opens, rather than flagging every `e.Attrs[…]; ok` in the file — the
// first draft did that and reported five guards on Prompt, AccentStyle,
// InvalidStyle, Frames and Duration that resolve literals and have
// nothing to do with this bug.
func TestNoOptionalHandleGuardsOnBareKeyPresence(t *testing.T) {
	const lookahead = 3 // guard, assignment, and one line of slack
	for _, f := range []string{"elements.go", "companion.go", "itemsview.go", "toolkit.go"} {
		body, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		lines := strings.Split(string(body), "\n")
		for i, line := range lines {
			if !strings.Contains(line, "e.Attrs[") || !strings.Contains(line, "; ok {") {
				continue
			}
			leadsToBound := false
			for j := i + 1; j < len(lines) && j <= i+lookahead; j++ {
				if strings.Contains(lines[j], "Bound[") {
					leadsToBound = true
					break
				}
			}
			if !leadsToBound {
				continue
			}
			t.Errorf("%s:%d guards a Bound handle on bare key presence: %s\n"+
				"    use suppliedAttr — an attribute written empty must read as omitted",
				f, i+1, strings.TrimSpace(line))
		}
	}
}

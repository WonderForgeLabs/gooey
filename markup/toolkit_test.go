package markup

import (
	"strings"
	"testing"
	"time"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/prop"
)

// buildFails is the load-error assertion these builders exist to make:
// a typed attribute that does not type-check, an unknown enum name, an
// interval that is not an interval — all of it fails at LOAD, naming
// what was wrong, rather than producing a component that quietly does
// nothing.
func buildFails(t *testing.T, src string, ctx *Context, want string) {
	t.Helper()
	_, err := Build([]byte(src), ctx)
	if err == nil {
		t.Fatalf("expected a load error mentioning %q, got none", want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error %q does not mention %q", err, want)
	}
}

func doc(body string) string {
	return `<Gooey xmlns="wonderforge.io/gooey/2026">` + body + `</Gooey>`
}

func TestProgressBarMarkup(t *testing.T) {
	pct := prop.NewSource(42)
	busy := prop.NewSource(true)
	ctx := &Context{Values: map[string]any{"Pct": pct, "Busy": busy}}
	w := buildOne(t, doc(`<ProgressBar Value="{{.Pct}}" Indeterminate="{{.Busy}}" Label="sync " BarWidth="20" Tick="50ms"/>`), ctx)

	p, ok := w.(*components.ProgressBar)
	if !ok {
		t.Fatalf("root is %T, want *components.ProgressBar", w)
	}
	if p.Value != pct || p.Indeterminate != busy {
		t.Fatal("the bindings did not resolve to the viewmodel's own handles")
	}
	if p.Width != 20 || p.Tick != 50*time.Millisecond {
		t.Fatalf("BarWidth/Tick parsed as %d/%v", p.Width, p.Tick)
	}
	if got := p.Label.Get(); got != "sync " {
		t.Fatalf("Label = %q", got)
	}
}

func TestProgressBarMarkupErrors(t *testing.T) {
	ctx := &Context{Values: map[string]any{"Pct": prop.NewSource(1), "Name": prop.NewSource("x")}}
	buildFails(t, doc(`<ProgressBar Value="{{.Name}}"/>`), ctx, "need *prop.Property[int]")
	buildFails(t, doc(`<ProgressBar Value="{{.Pct}}" Tick="soon"/>`), ctx, "Tick=\"soon\"")
	buildFails(t, doc(`<ProgressBar Value="{{.Pct}}" Tick="-1s"/>`), ctx, "must be positive")
}

func TestSpinnerMarkup(t *testing.T) {
	busy := prop.NewSource(false)
	ctx := &Context{Values: map[string]any{"Busy": busy}}
	w := buildOne(t, doc(`<Spinner Frames="line" Interval="40ms" Label="loading" Enabled="{{.Busy}}"/>`), ctx)

	s, ok := w.(*components.Spinner)
	if !ok {
		t.Fatalf("root is %T, want *components.Spinner", w)
	}
	if len(s.Frames) != len(components.SpinnerLine) || s.Frames[1] != "/" {
		t.Fatalf("Frames resolved to %v", s.Frames)
	}
	if s.Interval != 40*time.Millisecond || s.Enabled != busy {
		t.Fatalf("Interval/Enabled = %v/%v", s.Interval, s.Enabled)
	}
}

func TestSpinnerMarkupRejectsUnknownFrameSet(t *testing.T) {
	ctx := &Context{Values: map[string]any{}}
	buildFails(t, doc(`<Spinner Frames="swirly"/>`), ctx, "unknown frame set")
}

func TestToggleMarkup(t *testing.T) {
	on := prop.NewSource(false)
	ran := 0
	ctx := &Context{Values: map[string]any{
		"Dark":    on,
		"Changed": gooey.Command(func() { ran++ }),
	}}
	w := buildOne(t, doc(`<Toggle Checked="{{.Dark}}" Label="dark mode" Changed="{{.Changed}}"/>`), ctx)

	tg, ok := w.(*components.Toggle)
	if !ok {
		t.Fatalf("root is %T, want *components.Toggle", w)
	}
	if tg.Checked != on {
		t.Fatal("Checked did not resolve to the viewmodel's handle")
	}
	if !tg.Toggle() || !on.Get() || ran != 1 {
		t.Fatalf("toggling did not set the property and run Changed (ran=%d)", ran)
	}
}

func TestSegmentedMarkupTakesALiteralList(t *testing.T) {
	sel := prop.NewSource(1)
	ctx := &Context{Values: map[string]any{"Range": sel}}
	w := buildOne(t, doc(`<Segmented Options="Day | Week | Month" Selected="{{.Range}}"/>`), ctx)

	sg, ok := w.(*components.Segmented)
	if !ok {
		t.Fatalf("root is %T, want *components.Segmented", w)
	}
	got := sg.Options.Get()
	want := []string{"Day", "Week", "Month"}
	if len(got) != len(want) {
		t.Fatalf("Options = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Options = %v, want %v", got, want)
		}
	}
	if sg.Index() != 1 {
		t.Fatalf("Index = %d, want 1", sg.Index())
	}
}

func TestSegmentedMarkupTakesABoundList(t *testing.T) {
	opts := components.Strs([]string{"a", "b"})
	ctx := &Context{Values: map[string]any{"Opts": opts, "Sel": prop.NewSource(0)}}
	w := buildOne(t, doc(`<Segmented Options="{{.Opts}}" Selected="{{.Sel}}"/>`), ctx)
	if sg := w.(*components.Segmented); sg.Options != opts {
		t.Fatal("a bound Options did not resolve to the viewmodel's handle")
	}
}

func TestSegmentedMarkupNeedsOptions(t *testing.T) {
	ctx := &Context{Values: map[string]any{"Sel": prop.NewSource(0)}}
	buildFails(t, doc(`<Segmented Selected="{{.Sel}}"/>`), ctx, "needs Options")
}

func TestStatusBarMarkupShorthandAndSlots(t *testing.T) {
	status := prop.NewSource("ready")
	ctx := &Context{Values: map[string]any{"Status": status}}
	w := buildOne(t, doc(`<StatusBar Left="{{.Status}}">
	    <StatusBar.Right><Spinner Frames="dot"/></StatusBar.Right>
	  </StatusBar>`), ctx)

	bar, ok := w.(*components.StatusBar)
	if !ok {
		t.Fatalf("root is %T, want *components.StatusBar", w)
	}
	left, ok := bar.Left.(*components.Text)
	if !ok {
		t.Fatalf("the Left shorthand built %T, want a *components.Text", bar.Left)
	}
	if got := left.Content.Get(); got != "ready" {
		t.Fatalf("Left content = %q", got)
	}
	status.Set("saved")
	if got := left.Content.Get(); got != "saved" {
		t.Fatalf("the shorthand section is not live: %q", got)
	}
	if _, ok := bar.Right.(*components.Spinner); !ok {
		t.Fatalf("the Right slot holds %T, want a *components.Spinner", bar.Right)
	}
	if bar.Center != nil {
		t.Fatal("an unfilled slot is not nil")
	}
}

func TestStatusBarMarkupErrors(t *testing.T) {
	ctx := &Context{Values: map[string]any{}}
	buildFails(t, doc(`<StatusBar Left="x"><StatusBar.Left><Text>y</Text></StatusBar.Left></StatusBar>`),
		ctx, "both an attribute and")
	buildFails(t, doc(`<StatusBar><Text>loose</Text></StatusBar>`),
		ctx, "takes no direct children")
	buildFails(t, doc(`<StatusBar><StatusBar.Middle><Text>y</Text></StatusBar.Middle></StatusBar>`),
		ctx, "does not accept the property element")
}

func TestButtonBarMarkup(t *testing.T) {
	ctx := &Context{Values: map[string]any{"Save": gooey.Command(func() {})}}
	w := buildOne(t, doc(`<ButtonBar Gap="2" Uniform="true" Separator="│">
	    <Button Content="open" Click="{{.Save}}"/>
	    <Button Content="save as…" Click="{{.Save}}"/>
	  </ButtonBar>`), ctx)

	bar, ok := w.(*components.ButtonBar)
	if !ok {
		t.Fatalf("root is %T, want *components.ButtonBar", w)
	}
	if len(bar.Children) != 2 || !bar.Uniform || bar.Separator != "│" {
		t.Fatalf("bar = %+v", bar)
	}
	// Uniform sizing is a measure-pass decision, so it shows up in bounds.
	c := gooey.NewComposer(bar, 60, 3)
	c.Frame()
	a := bar.Children[0].(*components.Button).Bounds().W
	b := bar.Children[1].(*components.Button).Bounds().W
	if a != b {
		t.Fatalf("uniform members are %d and %d cells wide", a, b)
	}
}

func TestButtonChromeMarkup(t *testing.T) {
	ctx := &Context{Values: map[string]any{"Save": gooey.Command(func() {})}}
	w := buildOne(t, doc(`<Button Content="Save" Chrome="pixel" Click="{{.Save}}"/>`), ctx)
	if b := w.(*components.Button); b.Chrome != components.ChromePixel {
		t.Fatalf("Chrome = %v, want ChromePixel", b.Chrome)
	}
	// The default is unchanged: a Button with no Chrome is the one-row
	// cell button every existing page already has.
	w = buildOne(t, doc(`<Button Content="Save" Click="{{.Save}}"/>`), ctx)
	if b := w.(*components.Button); b.Chrome != components.ChromeCell {
		t.Fatalf("the default chrome is %v, want ChromeCell", b.Chrome)
	}
	buildFails(t, doc(`<Button Content="Save" Chrome="neon"/>`), ctx, "unknown chrome")
}

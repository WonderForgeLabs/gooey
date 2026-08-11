package main

import (
	"image"
	"os"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

// The contract these tests exercise is the SHIPPED markup, not a
// fixture that resembles it: the demo is the proof that every component
// in the wave can be spelled in markup, so the demo's own file is what
// gets loaded.

func demoFS(t *testing.T) fstest.MapFS {
	t.Helper()
	b, err := os.ReadFile("toolkit.gooey")
	if err != nil {
		t.Fatal(err)
	}
	return fstest.MapFS{"toolkit.gooey": &fstest.MapFile{Data: b}}
}

func demoCtx() *markup.Context {
	rows := prop.NewSource(catalogue)
	presetIdx := prop.NewSource(0)
	accent := prop.NewSource(presets[0].color)
	return &markup.Context{
		Values: map[string]any{
			"Pct":        prop.NewSource(40),
			"Busy":       prop.NewSource(false),
			"Running":    prop.NewSource(true),
			"StageIndex": prop.NewSource(1),
			"Stage":      prop.NewSource("Fetch"),
			"Log":        prop.NewSource("log"),
			"Status":     prop.NewSource("ready"),
			"Clock":      prop.NewSource("00:00:00"),
			"Tier":       prop.NewSource("chrome: kitty"),
			"Tab":        prop.NewSource(0),
			"Hints":      prop.NewSource(true),
			"Greeting":   prop.NewSource(""),
			"Load":       prop.NewSource(42),
			"History":    prop.NewSource([]float64{10, 40, 70, 30}),
			"Kit": components.Items(rows, func(r kitRow) map[string]any {
				return map[string]any{"Name": r.Name, "Where": r.Where, "Note": r.Note}
			}),
			"KitSel":      prop.NewSource(0),
			"KitName":     prop.NewSource(catalogue[0].Name),
			"KitNote":     prop.NewSource(catalogue[0].Note),
			"Accent":      accent,
			"AccentStyle": prop.NewComputed(func() render.Style { return render.Style{Fg: accent.Get()} }),
			"Gradient":    prop.NewComputed(func() image.Image { return gradientImage(accent.Get()) }),
			"Preset":      presetIdx,
			"FormName":    prop.NewSource(""),
			"FormEmail":   prop.NewSource(""),
			"FormTag":     prop.NewSource(""),
			"FormStatus":  prop.NewSource("status"),
			"Advance":     gooey.Command(func() {}),
			"Sample":      gooey.Command(func() {}),
			"TickClock":   gooey.Command(func() {}),
			"StageChanged": gooey.Command(func() {
			}),
			"Start":         gooey.Command(func() {}),
			"Abort":         gooey.Command(func() {}),
			"Reset":         gooey.Command(func() {}),
			"Deploy":        gooey.Command(func() {}),
			"Quit":          gooey.Command(func() {}),
			"Notify":        gooey.Command(func() {}),
			"Sticky":        gooey.Command(func() {}),
			"ClearToasts":   gooey.Command(func() {}),
			"TabChanged":    gooey.Command(func() {}),
			"ClearGreeting": gooey.Command(func() {}),
			"KitSelected":   gooey.Command(func() {}),
			"KitActivate":   gooey.Command(func() {}),
			"PresetChanged": gooey.Command(func() {}),
			"OpenPresets":   gooey.Command(func() {}),
			"Submit":        gooey.Command(func() {}),
		},
		Styles: map[string]render.Style{
			"panel":  {},
			"accent": {},
			"dim":    {},
			"err":    {},
		},
		Components: map[string]markup.Builder{"ColorPreset": presetBuilder},
	}
}

// tabProp is the page's tab selection — the tests drive it directly,
// which is what the strip's keys and clicks do underneath.
func tabProp(t *testing.T, ctx *markup.Context) *prop.Property[int] {
	t.Helper()
	p, ok := ctx.Values["Tab"].(*prop.Property[int])
	if !ok {
		t.Fatal("the demo context has no Tab property")
	}
	return p
}

func TestDemoPageLoads(t *testing.T) {
	if _, err := markup.Load(demoFS(t), "toolkit.gooey", demoCtx()); err != nil {
		t.Fatal(err)
	}
}

// Every component the kit ships has to actually be on the page — that
// is the demo's whole contract (issue #179), and a page that quietly
// lost one would still load. Collapsed tab pages are ordinary children,
// so one walk sees every tab.
func TestDemoShowsEveryComponentInTheKit(t *testing.T) {
	ctx := demoCtx()
	root, err := markup.Load(demoFS(t), "toolkit.gooey", ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]int{}
	var walk func(gooey.Component)
	walk = func(w gooey.Component) {
		switch c := w.(type) {
		case *components.ProgressBar:
			found["ProgressBar"]++
		case *components.Spinner:
			found["Spinner"]++
		case *components.Toggle:
			found["Toggle"]++
		case *components.Segmented:
			found["Segmented"]++
		case *components.StatusBar:
			found["StatusBar"]++
		case *components.ButtonBar:
			found["ButtonBar"]++
		case *components.Button:
			found["Button"]++
			if c.Chrome == components.ChromePixel {
				found["PixelButton"]++
			}
		case *components.MenuBar:
			found["MenuBar"]++
		case *components.ToastHost:
			found["ToastHost"]++
		case *components.AdornmentLayer:
			found["AdornmentLayer"]++
		case *components.Tabs:
			found["Tabs"]++
		case *components.Canvas:
			found["Canvas"]++
		case *components.Checkbox:
			found["Checkbox"]++
		case *components.ColorPicker:
			found["ColorPicker"]++
		case *components.Gauge:
			found["Gauge"]++
		case *components.Sparkline:
			found["Sparkline"]++
		case *components.Image:
			found["Image"]++
		case *components.ItemsView:
			found["ItemsView"]++
		case *components.TextBox:
			found["TextBox"]++
		case *components.VStack:
			found["VStack"]++
		case *components.HStack:
			found["HStack"]++
		case *components.Grid:
			found["Grid"]++
		case *components.Border:
			found["Border"]++
		case *components.Text:
			found["Text"]++
		case *colorPreset:
			// The Popup adopter: the primitive has no markup element, so
			// its owner is what proves Popup is on the page.
			found["Popup"]++
		}
		if a, ok := w.(gooey.Attacher); ok {
			for _, at := range a.Attachments() {
				switch at.(type) {
				case *components.Tooltip:
					found["Tooltip"]++
				case *components.ValidationMarker:
					found["ValidationMarker"]++
				case *components.Timer:
					found["Timer"]++
				case *gooey.KeyBinding:
					found["KeyBinding"]++
				}
			}
		}
		if ct, ok := w.(gooey.Container); ok {
			for _, ch := range ct.ChildComponents() {
				walk(ch)
			}
		}
	}
	walk(root)
	for _, want := range []string{
		"Text", "Border", "Grid", "VStack", "HStack", "Canvas",
		"Button", "PixelButton", "ButtonBar", "Checkbox", "Toggle", "Segmented", "TextBox",
		"Tabs", "ItemsView", "ProgressBar", "Gauge", "Sparkline", "Spinner",
		"Timer", "StatusBar", "MenuBar", "ToastHost", "AdornmentLayer", "Tooltip",
		"Popup", "ColorPicker", "Image", "ValidationMarker", "KeyBinding",
	} {
		if found[want] == 0 {
			t.Errorf("the demo page has no %s", want)
		}
	}

	// <Validate> is a behavior, not a component: what proves it is on the
	// page is the error property it PUBLISHES into the context, derived
	// from each field's Text binding path.
	for _, k := range []string{"FormNameErr", "FormEmailErr", "FormTagErr"} {
		if _, ok := ctx.Values[k].(*prop.Property[string]); !ok {
			t.Errorf("no <Validate> behavior published %s", k)
		}
	}

	// And the catalogue on the "data" tab is the page's own index, so it
	// has to name everything the walk just found.
	named := map[string]bool{}
	for _, r := range catalogue {
		named[r.Name] = true
	}
	for want := range found {
		if want == "PixelButton" {
			continue
		}
		if !named[want] {
			t.Errorf("the data tab's catalogue does not list %s", want)
		}
	}
}

// Each tab paints its own components when it is the selected one. A
// switch is a Set on the bound int — exactly what the strip's keys and
// clicks do — and the composer's visibility sweep does the rest.
func TestDemoEveryTabPaints(t *testing.T) {
	ctx := demoCtx()
	root, err := markup.Load(demoFS(t), "toolkit.gooey", ctx)
	if err != nil {
		t.Fatal(err)
	}
	tab := tabProp(t, ctx)
	c := gooey.NewComposer(root, 96, 22)
	for i, want := range [][]string{
		// job
		{"build ", "job running", "[ start ]", "Deploy"},
		// basics: the VStack/Grid panels and the plain inputs
		{"VStack — top to bottom", "captions on the job tab", "Grid + HStack", "[ clear the field ]"},
		// data: Gauge, Sparkline, and rows realized from the template
		{"load ", "history", "ItemsView", "selected"},
		// visual: the Canvas captions around the ColorPicker and the Image
		{"ColorPicker", "generated in Go, never a file", "Canvas"},
		// forms: the prompts, and the message a Validate behavior
		// publishes for an empty required field
		{"name:", "email:", "we need a working email address", "[ submit ]"},
		// overlays: the popup owner's closed row and the bar beside it
		{"ember", "sticky toast", "clear toasts"},
	} {
		tab.Set(i)
		c.Frame()
		screen := screenOf(c, 22)
		for _, s := range want {
			if !strings.Contains(screen, s) {
				t.Errorf("tab %d (%s) does not show %q:\n%s", i, tabNames[i], s, screen)
			}
		}
		// The strip itself is always up, whichever page is showing.
		for _, name := range tabNames {
			if !strings.Contains(screen, name) {
				t.Errorf("tab %d: the strip lost its %q header", i, name)
			}
		}
	}
}

// The status row is a budget, and the demo has to stay inside it. A
// StatusBar sizes Left and Right to their content and centres Center in
// what is left over, so it never clips and never complains — it just
// runs the clock into the status text when the three sections no longer
// fit, which is what the longest status ("tab: overlays") did against
// the old key-hint string. Nothing about that is a StatusBar bug, so
// the guard belongs here, on the page that has to keep the budget.
func TestDemoStatusRowKeepsItsSectionsApart(t *testing.T) {
	ctx := demoCtx()
	root, err := markup.Load(demoFS(t), "toolkit.gooey", ctx)
	if err != nil {
		t.Fatal(err)
	}
	status, ok := ctx.Values["Status"].(*prop.Property[string])
	if !ok {
		t.Fatal("the demo context has no Status property")
	}
	longest := ""
	for _, n := range tabNames {
		if s := "tab: " + n; len(s) > len(longest) {
			longest = s
		}
	}
	status.Set(longest)
	c := gooey.NewComposer(root, 96, 22)
	c.Frame()

	row := strings.Split(screenOf(c, 22), "\n")[20]
	i := strings.Index(row, longest)
	if i < 0 {
		t.Fatalf("the status row does not show %q:\n%s", longest, row)
	}
	if rest := row[i+len(longest):]; !strings.HasPrefix(rest, "  ") {
		t.Errorf("the clock is jammed against the status text — the three sections no longer fit in 96 columns:\n%s", row)
	}
}

// The forms tab's floating ValidationMarker gets its own row. The
// adornment plane paints above everything, so a marker whose lane is
// also a content row does not lose the argument — it simply erases what
// is under it, and the recording of this page showed the marker sitting
// squarely on `[ submit ]`. The empty fixed row 7 is what keeps them
// apart, and an Auto row would not do: with no children it sizes to
// nothing and the marker lands on the button again.
func TestDemoValidationMarkerDoesNotCoverSubmit(t *testing.T) {
	ctx := demoCtx()
	root, err := markup.Load(demoFS(t), "toolkit.gooey", ctx)
	if err != nil {
		t.Fatal(err)
	}
	tabProp(t, ctx).Set(4)
	tag, ok := ctx.Values["FormTag"].(*prop.Property[string])
	if !ok {
		t.Fatal("the demo context has no FormTag property")
	}
	c := gooey.NewComposer(root, 96, 22)
	c.Frame()

	tag.Set("toolongtag") // MaxLen=8: the marker floats
	c.Frame()
	screen := screenOf(c, 22)
	if !strings.Contains(screen, "at most 8") {
		t.Fatalf("the ValidationMarker is not showing at all:\n%s", screen)
	}
	if !strings.Contains(screen, "[ submit ]") {
		t.Errorf("the floating marker painted over the submit button:\n%s", screen)
	}
}

// Switching tabs must leave the screen byte-identical to composing that
// tab from scratch: the outgoing page's cells are erased by the
// Composer's bounds sweep, and nothing of it may survive underneath the
// incoming one. This is the damage contract for the whole
// reorganization, and it is what caught three framework bugs — a Grid
// arranged into nothing handing out its stale tracks, and Border and
// Gauge painting a row outside their own (zero-height) bounds.
func TestDemoTabSwitchLeavesNoScar(t *testing.T) {
	ctx := demoCtx()
	root, err := markup.Load(demoFS(t), "toolkit.gooey", ctx)
	if err != nil {
		t.Fatal(err)
	}
	tab := tabProp(t, ctx)
	c := gooey.NewComposer(root, 96, 22)
	for i := range tabNames {
		tab.Set(i)
		c.Frame()
		got := screenOf(c, 22)

		// The same tab, composed from nothing — no history to smear.
		clean := demoCtx()
		cleanRoot, err := markup.Load(demoFS(t), "toolkit.gooey", clean)
		if err != nil {
			t.Fatal(err)
		}
		tabProp(t, clean).Set(i)
		cc := gooey.NewComposer(cleanRoot, 96, 22)
		cc.Frame()
		if want := screenOf(cc, 22); got != want {
			t.Fatalf("switching to tab %d (%s) left the previous page showing.\ngot:\n%s\nwant:\n%s",
				i, tabNames[i], got, want)
		}
	}
}

// The Popup on the "overlays" tab: opening drops the list over the page
// and dismissing restores exactly what it covered — the primitive's
// damage contract, exercised through the shipped page.
func TestDemoPresetPopupOpensAndRestores(t *testing.T) {
	ctx := demoCtx()
	root, err := markup.Load(demoFS(t), "toolkit.gooey", ctx)
	if err != nil {
		t.Fatal(err)
	}
	tabProp(t, ctx).Set(5)
	c := gooey.NewComposer(root, 96, 22)
	pick, err := markup.Find[*colorPreset](ctx, "Presets")
	if err != nil {
		t.Fatal(err)
	}
	// Focus the owner before the baseline: a key open leaves focus where
	// it already was, so this keeps the comparison about the cells the
	// list covered rather than about which component wears the ring.
	c.Focus().SetFocus(pick)
	c.Frame()
	before := screenOf(c, 22)

	pick.Open()
	c.Frame()
	if got := screenOf(c, 22); !strings.Contains(got, "orchid") {
		t.Fatalf("the open popup does not show its list:\n%s", got)
	}
	if !pick.HandleKey(input.Named(input.KeyEsc)) {
		t.Fatal("esc did not dismiss the popup")
	}
	c.Frame()
	if got := screenOf(c, 22); got != before {
		t.Fatalf("dismissing the popup left a scar.\nbefore:\n%s\nafter:\n%s", before, got)
	}
}

// The page composes into a real frame at the size the GIF is recorded
// at, and the parts that make the demo legible are on screen.
func TestDemoComposes(t *testing.T) {
	root, err := markup.Load(demoFS(t), "toolkit.gooey", demoCtx())
	if err != nil {
		t.Fatal(err)
	}
	c := gooey.NewComposer(root, 96, 22)
	c.Frame()
	var sb strings.Builder
	cells := c.Cells()
	for y := 0; y < 22; y++ {
		for x := 0; x < 96; x++ {
			sb.WriteRune(cells.At(x, y).Rune)
		}
		sb.WriteByte('\n')
	}
	screen := sb.String()
	for _, want := range []string{"toolkit — every component in the kit", "build ", "job running", "Fetch", "start", "Deploy", "ready", "q: quit", " Job ", " Notify "} {
		if !strings.Contains(screen, want) {
			t.Errorf("the composed screen does not show %q", want)
		}
	}
}

// Wave 2 on the shipped page: the menu drops OVER the content rows,
// esc restores them, and a toast pops over the top-right corner and
// leaves no scar when dismissed.
func TestDemoOverlaysDropAndRestore(t *testing.T) {
	ctx := demoCtx()
	root, err := markup.Load(demoFS(t), "toolkit.gooey", ctx)
	if err != nil {
		t.Fatal(err)
	}
	var bar *components.MenuBar
	var walk func(gooey.Component)
	walk = func(w gooey.Component) {
		if b, ok := w.(*components.MenuBar); ok {
			bar = b
		}
		if ct, ok := w.(gooey.Container); ok {
			for _, ch := range ct.ChildComponents() {
				walk(ch)
			}
		}
	}
	walk(root)
	if bar == nil {
		t.Fatal("the demo page has no MenuBar")
	}
	c := gooey.NewComposer(root, 96, 22)
	// Focus first, THEN take the baseline: a key-opened menu leaves
	// focus on the bar when it dismisses, so the before/after comparison
	// is only about the cells the dropdown covered — which is the
	// contract — once the focus ring is already parked on the bar. (The
	// page's first focus stop is now the Tabs strip, which paints focus
	// markers; before the tabs it was an unfocused page.)
	c.Focus().SetFocus(bar)
	c.Frame()
	before := screenOf(c, 22)

	c.HandleKey(input.Named(input.KeyEnter))
	c.Frame()
	open := screenOf(c, 22)
	if !strings.Contains(open, "Start") || !strings.Contains(open, "ctrl+s") {
		t.Fatalf("the open menu does not show its items and gesture hints:\n%s", open)
	}

	c.HandleKey(input.Named(input.KeyEsc))
	c.Frame()
	if got := screenOf(c, 22); got != before {
		t.Fatalf("dismissing the menu did not restore the screen.\nbefore:\n%s\nafter:\n%s", before, got)
	}

	toasts, err := markup.Find[*components.ToastHost](ctx, "Toasts")
	if err != nil {
		t.Fatal(err)
	}
	tst := toasts.Show("job deployed")
	c.Frame()
	if got := screenOf(c, 22); !strings.Contains(got, " job deployed ") {
		t.Fatalf("the toast is not on screen:\n%s", got)
	}
	toasts.Dismiss(tst)
	c.Frame()
	if got := screenOf(c, 22); got != before {
		t.Fatal("dismissing the toast left a scar on the screen")
	}
}

// Tooltips on the shipped page: resting the pointer on the toast button
// shows its tip — with the ctrl+t hint — through the AdornmentLayer,
// and moving away restores the exact screen. The composition is not
// started, so the show is immediate (no dispatcher, no delay timer);
// the delay discipline itself is pinned in the components package.
func TestDemoTooltipShowsAndRestores(t *testing.T) {
	root, err := markup.Load(demoFS(t), "toolkit.gooey", demoCtx())
	if err != nil {
		t.Fatal(err)
	}
	var toastBtn *components.Button
	var walk func(gooey.Component)
	walk = func(w gooey.Component) {
		if b, ok := w.(*components.Button); ok && b.Content.Get() == "toast" {
			toastBtn = b
		}
		if ct, ok := w.(gooey.Container); ok {
			for _, ch := range ct.ChildComponents() {
				walk(ch)
			}
		}
	}
	walk(root)
	if toastBtn == nil {
		t.Fatal("the demo page has no toast button")
	}
	c := gooey.NewComposer(root, 96, 22)
	c.Frame()
	before := screenOf(c, 22)

	b := toastBtn.Bounds()
	c.HandleMouse(input.MouseEvent{Kind: input.MouseMove, X: b.X + 1, Y: b.Y})
	c.Frame()
	shown := screenOf(c, 22)
	if !strings.Contains(shown, " pop the status as a toast ") || !strings.Contains(shown, "ctrl+t") {
		t.Fatalf("the tooltip (with its gesture hint) is not on screen:\n%s", shown)
	}

	c.HandleMouse(input.MouseEvent{Kind: input.MouseMove, X: 0, Y: 21})
	c.Frame()
	if got := screenOf(c, 22); got != before {
		t.Fatalf("dismissing the tooltip did not restore the screen.\nbefore:\n%s\nafter:\n%s", before, got)
	}
}

func screenOf(c *gooey.Composer, rows int) string {
	var sb strings.Builder
	cells := c.Cells()
	for y := 0; y < rows; y++ {
		for x := 0; x < 96; x++ {
			sb.WriteRune(cells.At(x, y).Rune)
		}
		sb.WriteByte('\n')
	}
	return sb.String()
}

// The disabled member is the conditional-command demonstration, so it
// has to be genuinely disabled when the condition says no.
func TestDemoAbortIsConditional(t *testing.T) {
	ctx := demoCtx()
	running := prop.NewSource(false)
	ctx.Values["Running"] = running
	ctx.Values["Abort"] = gooey.NewCommand(func() {}).When(running)
	root, err := markup.Load(demoFS(t), "toolkit.gooey", ctx)
	if err != nil {
		t.Fatal(err)
	}
	var abort *components.Button
	var walk func(gooey.Component)
	walk = func(w gooey.Component) {
		if b, ok := w.(*components.Button); ok && b.Content.Get() == "abort" {
			abort = b
		}
		if ct, ok := w.(gooey.Container); ok {
			for _, ch := range ct.ChildComponents() {
				walk(ch)
			}
		}
	}
	walk(root)
	if abort == nil {
		t.Fatal("the demo has no abort button")
	}
	c := gooey.NewComposer(root, 96, 22)
	c.Frame()
	if !c.Cells().At(abort.Bounds().X, abort.Bounds().Y).Style.Dim {
		t.Fatal("abort is not dim while its condition is false")
	}
	running.Set(true)
	c.Frame()
	if c.Cells().At(abort.Bounds().X, abort.Bounds().Y).Style.Dim {
		t.Fatal("abort is still dim after its condition became true")
	}
}

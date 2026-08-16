package main

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

// The shipped markup is what gets loaded — including swatch.gooey, which
// is only reachable by CONVENTION (<Swatch/> → swatch.gooey through
// Context.Includes). A fixture that registered a builder instead would
// pass while the demo failed to start.
func demoFS(t *testing.T) fstest.MapFS {
	t.Helper()
	fsys := fstest.MapFS{}
	for _, name := range []string{"colors.gooey", "swatch.gooey"} {
		b, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		fsys[name] = &fstest.MapFile{Data: b}
	}
	return fsys
}

func demoCtx(fsys fstest.MapFS) *markup.Context {
	accent := prop.NewSource(render.RGB(255, 170, 60))
	style := func(f float64) *prop.Property[render.Style] {
		return prop.NewComputed(func() render.Style {
			return render.Style{Fg: scaleColor(accent.Get(), f)}
		})
	}
	return &markup.Context{
		Values: map[string]any{
			"Accent":      accent,
			"AccentStyle": style(1),
			"Tint0":       style(1), "Tint1": style(0.82), "Tint2": style(0.64),
			"Tint3": style(0.46), "Tint4": style(0.28),
			// A computed over accent, like the real one: the status line
			// reports the picked color, so it is a reader and has to be one
			// here too or the damage count below models the wrong page.
			"Status": prop.NewComputed(func() string {
				c := accent.Get()
				return fmt.Sprintf("depth truecolor   picked #%02X%02X%02X", c.R, c.G, c.B)
			}),
			"Quit": gooey.Command(func() {}),
		},
		Styles: map[string]render.Style{"dim": {}},
		Components: map[string]markup.Builder{
			"TierStrip": func(markup.Element, *markup.Context) (gooey.Component, error) {
				return &tierStrip{accent: accent}, nil
			},
		},
		Includes: fsys,
	}
}

func TestPageComposes(t *testing.T) {
	fsys := demoFS(t)
	root, err := markup.Load(fsys, "colors.gooey", demoCtx(fsys))
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
	for _, want := range []string{
		"gooey — color", "tier simulation", "truecolor",
		"overlapping cascade", "depth truecolor",
	} {
		if !strings.Contains(screen, want) {
			t.Errorf("the composed screen does not show %q:\n%s", want, screen)
		}
	}
	// Five swatch instances, two rows each, stepping 4 right and 1 down.
	// The last card is placed at Canvas.Left=62 inside a Canvas the Border
	// indents by one, so its lower row is screen row 8 across columns
	// 63..72 — cells no other card reaches.
	if got := cells.At(70, 8).Rune; got != '█' {
		t.Errorf("the fifth swatch is not on screen: cell (70,8) is %q\nrow: %q", got, strings.Split(screen, "\n")[8])
	}
}

// A declared surface is a contract, so a misspelled attribute has to fail
// at LOAD. Without the declaration in swatch.gooey it would silently
// paint an unstyled card.
func TestSwatchRejectsAnUndeclaredAttribute(t *testing.T) {
	fsys := demoFS(t)
	fsys["bad.gooey"] = &fstest.MapFile{Data: []byte(
		`<Gooey xmlns="wonderforge.io/gooey/2026"><Swatch Tnit="{{.Tint0}}"/></Gooey>`)}
	_, err := markup.Load(fsys, "bad.gooey", demoCtx(fsys))
	if err == nil {
		t.Fatal("a misspelled attribute loaded without complaint")
	}
	if !strings.Contains(err.Error(), "Tnit") {
		t.Errorf("the load error does not name the offending attribute: %v", err)
	}
}

// accentDamage composes name, moves the accent, and reports how many
// components repainted on the second frame. The count is the only thing
// that pins a repaint claim: a cell assertion passes just as well when the
// entire tree painted.
func accentDamage(t *testing.T, fsys fstest.MapFS, name string) int {
	t.Helper()
	ctx := demoCtx(fsys)
	root, err := markup.Load(fsys, name, ctx)
	if err != nil {
		t.Fatal(err)
	}
	c := gooey.NewComposer(root, 96, 22)
	c.Frame()
	ctx.Values["Accent"].(*prop.Property[render.Color]).Set(render.RGB(10, 200, 90))
	_, painted := c.Frame()
	return painted
}

// variant writes a copy of the shipped page with one substitution applied,
// failing loudly if the substitution matched nothing — an arm that is
// secretly the same document as its control agrees with it for free.
func variant(t *testing.T, fsys fstest.MapFS, name, old, new string) string {
	t.Helper()
	src := string(fsys["colors.gooey"].Data)
	out := strings.Replace(src, old, new, 1)
	if out == src {
		t.Fatalf("%s: the substitution matched nothing — this arm is the control", name)
	}
	fsys[name] = &fstest.MapFile{Data: []byte(out)}
	return name
}

// The page's headline claim is that ONE property styles everything. It
// does — and the bill for that is the whole tree, every time an arrow key
// moves a channel.
//
// The three arms separate the two causes, because the shipped number alone
// cannot tell them apart:
//
//	shipped                      14 — every component on the page
//	Border style unbound          9 — the components that actually read accent
//	Border accent-bound, no Bg   10 — those nine, plus the Border itself
//
// So nine components read the accent, and the tenth is the Border that
// wears it. The remaining four repaint for a reason that has nothing to do
// with the property graph: a container that repaints AND carries a
// declared Background fills its bounds and is marked `covered`, which
// forces its whole subtree to repaint above it in the same frame
// (composer.go:263-299). Dropping either half of that pair — the binding
// or the Background — takes the page off the full-tree path.
//
// This is not a bug and the page is not changed to avoid it: an
// accent-colored frame around a filled surface is what the demo is for.
// It is pinned so that the day someone moves one of these numbers, the
// arms say which half moved.
func TestAccentRepaintsTheWholePageAndWhy(t *testing.T) {
	fsys := demoFS(t)
	unbound := variant(t, fsys, "unbound.gooey",
		`<Border Title="gooey — color" Style="{{.AccentStyle}}" Background="#12121e">`,
		`<Border Title="gooey — color" Style="dim" Background="#12121e">`)
	noBg := variant(t, fsys, "nobg.gooey", ` Background="#12121e">`, `>`)

	for _, tc := range []struct {
		name string
		page string
		want int
	}{
		{"the shipped page repaints wholly", "colors.gooey", 14},
		{"only nine components read the accent", unbound, 9},
		{"without a Background the subtree is not forced", noBg, 10},
	} {
		if got := accentDamage(t, fsys, tc.page); got != tc.want {
			t.Errorf("%s: %d components repainted, want %d", tc.name, got, tc.want)
		}
	}
}

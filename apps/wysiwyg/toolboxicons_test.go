package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/apps/wysiwyg/components/toolbox"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/markup"
)

// The toolbox draws the icon each catalog entry declares, and these are
// the pins for the two claims that are easy to satisfy in APPEARANCE
// only.
//
// The first is that the icon comes from the catalog rather than from a
// table in this file: markup.ElementSpec.Icon is the source, so an
// element that gains an icon in markup/elements.go gains one here with
// no change to the editor.
//
// The second is the one worth counting. A theme flip must repaint the
// icons and NOTHING else. A screen assertion cannot see the difference —
// "the icon is the new colour" is just as true when every component in
// the shell repainted — so the assertion has to be the damage count that
// Composer.Frame returns. If a later change moves that number, the
// change IS that: a theme that repaints the tree has stopped being a
// property and become an invalidate.

// toolboxImages collects the realized rows' pictures, reached through
// the list's Name rather than by guessing which of the shell's
// ItemsViews is the toolbox — the other one is the properties grid, and
// an unnamed search would silently start passing against it.
func toolboxImages(t *testing.T, ed *editor) []*components.Image {
	t.Helper()
	list, ok := ed.ctx.Named["Toolbox"]
	if !ok {
		t.Fatal(`the shell has no element named "Toolbox"; these tests reach the palette by name`)
	}
	var out []*components.Image
	walkTree(list, func(c gooey.Component) {
		if im, ok := c.(*components.Image); ok {
			out = append(out, im)
		}
	})
	return out
}

func TestPaletteRowsCarryTheDeclaredIcon(t *testing.T) {
	ed, _, _ := shellTree(t)

	imgs := toolboxImages(t, ed)
	if len(imgs) == 0 {
		t.Fatal("no realized palette row carries an <Image>: the toolbox is not drawing icons at all")
	}
	if len(imgs) < 2 {
		// One realized row is the signature of a template that measures
		// the whole list: a Grid with no Rows takes everything it is
		// offered, and ItemsView then fits exactly one. It renders, it
		// loads, and the toolbox shows a single component.
		t.Fatalf("only %d row realized; the row template is measuring more than one cell tall", len(imgs))
	}

	// The realized window starts at the top of an unscrolled list, so
	// row i is palette entry i. Checked in BOTH directions, because the
	// interesting claim is not "there are pictures" — it is that a
	// picture appears exactly where the catalog declares one.
	var withIcon, withPicture int
	for i, im := range imgs {
		if i >= len(ed.palette) {
			t.Fatalf("row %d has no palette entry: the window is larger than the list", i)
		}
		e := ed.palette[i]
		if im.Src == nil {
			t.Fatalf("<%s>'s row has an <Image> with no Src at all", e.Name)
		}
		pic := im.Src.Get() != nil
		switch {
		case e.Icon != "" && !pic:
			t.Errorf("<%s> declares icon %q but its row drew nothing", e.Name, e.Icon)
		case e.Icon == "" && pic:
			t.Errorf("<%s> declares no icon but its row drew a picture; an absence must stay an absence", e.Name)
		}
		if e.Icon != "" {
			withIcon++
		}
		if pic {
			withPicture++
		}
	}
	// Both ends have to be non-empty or the loop above is vacuous: an
	// all-blank toolbox and an all-iconned one each satisfy half of it.
	if withIcon == 0 {
		t.Fatal("no realized row's element declares an icon; this test cannot see a regression")
	}
	if withIcon == len(imgs) {
		t.Fatal("every realized row declares an icon, so the absence half of this test never fires")
	}
	if withPicture != withIcon {
		t.Fatalf("%d rows declare an icon but %d drew one", withIcon, withPicture)
	}

	// And the names really are the catalog's. Reading them back off
	// ed.palette rather than off a list written here is what makes this
	// a check on the wiring instead of a second copy of the mapping.
	for _, e := range ed.palette {
		if e.Origin != markup.OriginBuiltin {
			continue
		}
		if e.Icon == "" {
			t.Errorf("builtin <%s> reached the palette with no icon", e.Name)
		}
	}

	// THE REGISTERED HALF, and it is the half worth a separate
	// assertion. Everything above would pass with Context.Elements
	// contributing nothing but a name — which is the state this issue
	// existed to leave behind. A palette showing icons for gooey's own
	// elements and blanks for the host's would be describing the build
	// rather than the app.
	var registeredWithIcon []string
	for _, e := range ed.palette {
		if e.Origin == markup.OriginRegistered && e.Icon != "" {
			registeredWithIcon = append(registeredWithIcon, e.Name)
		}
	}
	if len(registeredWithIcon) == 0 {
		t.Fatal("no REGISTERED element declares an icon; the Context.Elements seam is contributing a name and nothing else")
	}
}

func TestAnElementWithNoDeclaredIconGetsNoPicture(t *testing.T) {
	// <LogPane> is registered as a plain Builder — a func, not a schema —
	// so it declares nothing, including no icon. Its row must reach the
	// template with a nil handle so the <Image> renders nothing.
	// Substituting a default here would make "unknown" look like a
	// choice, which is the same collapse describeAttrs exists to prevent
	// for attributes.
	ed, _, _ := shellTree(t)
	var found bool
	for _, e := range ed.palette {
		if e.Name != "LogPane" {
			continue
		}
		found = true
		if e.Icon != "" {
			t.Fatalf("<LogPane> is a Builder registration; it cannot declare an icon, got %q", e.Icon)
		}
		if h := ed.icons.For(e.Icon); h != nil {
			t.Fatal("an undeclared icon produced a handle; the absence must survive to the row")
		}
	}
	if !found {
		t.Fatal("<LogPane> is missing from the palette; this test's subject is gone")
	}
}

func TestThemeFlipRepaintsOnlyTheToolboxIcons(t *testing.T) {
	ed, _, comp := shellTree(t)

	// Settle first. A count taken on a frame that still had work
	// outstanding measures the backlog, not the flip.
	for i := 0; ; i++ {
		if _, painted := comp.Frame(); painted == 0 {
			break
		}
		if i > 8 {
			t.Fatal("the shell never settles: no frame repainted zero components")
		}
	}

	// The rows that can repaint are the ones actually holding a picture.
	// A row whose element declares no icon binds a nil handle, so its
	// <Image> returns before it reads anything and never subscribes to
	// the tint — it is not damage a flip can cause, and counting it
	// would make this assertion fail for a correct implementation.
	var icons, blanks int
	for _, im := range toolboxImages(t, ed) {
		if im.Src != nil && im.Src.Get() != nil {
			icons++
		} else {
			blanks++
		}
	}
	if icons == 0 {
		t.Fatal("no icons are realized, so this test cannot tell a correct flip from a no-op")
	}
	if blanks == 0 {
		t.Fatal("every realized row has a picture; the blank-row arm of this count is untested")
	}

	was := ed.themeDark.Get()
	ed.themeDark.Set(!was)
	_, painted := comp.Frame()
	if painted != icons {
		t.Fatalf("a theme flip repainted %d components; the %d realized toolbox icons and nothing else were expected.\n"+
			"More means the tint reached something that is not an icon — a style, a container background, "+
			"or a computed the shell reads. Fewer means an icon stopped subscribing to the tint, which is silent: "+
			"it keeps drawing its old colour forever.", painted, icons)
	}

	// Every icon still HAS a picture in the second theme. This is what
	// makes preloading both tints, rather than only the current one,
	// checkable from here: a tint whose raster cannot be produced turns
	// into a nil picture inside the computed, because a paint has
	// nowhere to put an error. The count above would still be right —
	// the components repainted — and the column would be blank.
	for i, im := range toolboxImages(t, ed) {
		if im.Src == nil || im.Src.Get() != nil {
			continue
		}
		if i < len(ed.palette) && ed.palette[i].Icon != "" {
			t.Fatalf("<%s> lost its picture in the other theme: that tint was never rasterized",
				ed.palette[i].Name)
		}
	}

	// And back, so the assertion is not passing on one direction only.
	// A flip that repaints going dark and nothing coming back is a
	// dependency dropped on one arm of the computed's branch — exactly
	// the shape that a Get behind an early return produces.
	ed.themeDark.Set(was)
	if _, back := comp.Frame(); back != icons {
		t.Fatalf("flipping back repainted %d components, want %d", back, icons)
	}
}

func TestTheHouseHighlightIsStoodDownOverTheIcons(t *testing.T) {
	// The row template mentions _selected, which turns ItemsView's own
	// highlight off. That is required rather than stylistic: the house
	// highlight re-styles the cells a row painted, and the icon's cells
	// are pixel content. With a graphics protocol the reverse flag is
	// invisible (the picture is on a plane above the cells); in the
	// halfblock fallback the picture IS the cells, so it would be drawn
	// as a photo-negative.
	//
	// Pinned here because it is invisible in every other kind of check:
	// deleting the marker <Text> from the template leaves a page that
	// loads, lays out and renders, with the house highlight quietly back
	// on.
	ed, _, _ := shellTree(t)
	list, ok := ed.ctx.Named["Toolbox"].(*components.ItemsView)
	if !ok {
		t.Fatalf(`"Toolbox" is %T, not an *components.ItemsView`, ed.ctx.Named["Toolbox"])
	}
	if list.Highlight {
		t.Fatal("the house highlight is on over pixel content: the row template no longer mentions _selected")
	}
}

// TestShippedIconsAreDistinctAssets.
//
// Two elements drawing the SAME glyph defeats the point of the column: a
// designer scanning the palette reads shapes, not the word beside them.
//
// THE DIRECTORY, NOT ed.palette, and that is the whole reason this test
// can see anything. The wysiwyg toolbox filters <Tab> out of its list
// (main.go), so a duplicate involving Tab's icon is invisible to any
// check that walks the realized palette — which is exactly how
// browser.svg and window.svg sat identical without a test noticing. The
// filter is one app's editorial choice; the catalog and its assets are
// the shared surface, so the assets are what get hashed.
//
// upstreamAliases is the one fact this check cannot derive. Codicons
// ships `browser` as an ALIAS of `window`: the two files are
// byte-identical in microsoft/vscode-codicons itself — fetched from
// src/icons/ and compared, both f2ed5124abc7b6231c7fd47a4f516369 — so the
// duplication here is faithful and the LICENSE's "unmodified on disk"
// claim holds. Recording it as an allowance rather than deleting a file
// keeps File()'s convention (a catalog name IS a filename) working for
// both names. Any pair NOT listed here is a curation mistake: a file
// copied from the wrong source glyph.
func TestShippedIconsAreDistinctAssets(t *testing.T) {
	upstreamAliases := map[string]string{"browser": "window"}
	canonical := func(name string) string {
		if a, ok := upstreamAliases[name]; ok {
			return a
		}
		return name
	}

	entries, err := os.ReadDir(toolbox.Dir)
	if err != nil {
		t.Fatalf("the icon directory does not read: %v", err)
	}

	byDigest := map[string][]string{}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".svg" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(toolbox.Dir, e.Name()))
		if err != nil {
			t.Errorf("%s does not read: %v", e.Name(), err)
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".svg")
		d := fmt.Sprintf("%x", sha256.Sum256(b))
		byDigest[d] = append(byDigest[d], name)
	}
	if len(byDigest) == 0 {
		t.Fatal("no .svg assets found; this test cannot see a regression")
	}

	for _, names := range byDigest {
		distinct := map[string]bool{}
		for _, n := range names {
			distinct[canonical(n)] = true
		}
		if len(distinct) > 1 {
			sort.Strings(names)
			t.Errorf("icons %v are byte-identical assets, so any two elements declaring them are "+
				"indistinguishable in the palette. Either one was copied from the wrong upstream "+
				"glyph — re-fetch it from microsoft/vscode-codicons src/icons/ — or they are a "+
				"genuine upstream alias, in which case add the pair to upstreamAliases above with "+
				"the comparison written down.", names)
		}
	}
}

// TestEveryDeclaredIconHasAnAsset covers the other direction, and it
// covers the WHOLE catalog rather than the toolbox's filtered view — for
// the same reason as above: an element the wysiwyg palette happens to
// hide still declares an icon that another consumer of
// markup.BuiltinElements will ask for.
func TestEveryDeclaredIconHasAnAsset(t *testing.T) {
	var declared int
	for _, e := range markup.BuiltinElements() {
		if e.Icon == "" {
			continue
		}
		declared++
		if _, err := os.Stat(filepath.Join(toolbox.Dir, toolbox.File(e.Icon))); err != nil {
			t.Errorf("<%s> declares icon %q but no asset resolves: %v", e.Name, e.Icon, err)
		}
	}
	if declared == 0 {
		t.Fatal("no builtin element declares an icon; this test cannot see a regression")
	}
}

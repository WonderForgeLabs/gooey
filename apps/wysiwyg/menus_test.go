package main

import (
	"os"
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/apps/wysiwyg/components/panel"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/prop"
)

func theMenuBar(t *testing.T, ed *editor) *components.MenuBar {
	t.Helper()
	bar, err := markup.Find[*components.MenuBar](ed.ctx, "Menus")
	if err != nil {
		t.Fatalf("the shell has no <MenuBar Name=\"Menus\">: %v", err)
	}
	return bar
}

// shellRegion resolves a named <Panel> region out of the built page.
// Named for the region rather than for the lookup, because seedpalette
// already owns findNamed for a different question — which component the
// palette just added.
func shellRegion(t *testing.T, ed *editor, name string) *panel.Pane {
	t.Helper()
	p, err := markup.Find[*panel.Pane](ed.ctx, name)
	if err != nil {
		t.Fatalf("no <Panel Name=%q> in the shell: %v", name, err)
	}
	return p
}

func menuNamed(t *testing.T, bar *components.MenuBar, title string) (int, components.Menu) {
	t.Helper()
	for i, m := range bar.Menus {
		if strings.ReplaceAll(m.Title, "_", "") == title {
			return i, m
		}
	}
	t.Fatalf("no %q menu; the bar has %d menus", title, len(bar.Menus))
	return 0, components.Menu{}
}

// TestTheCheckAndTheAcceleratorAreOneState is the requirement, asserted
// as a STRUCTURAL fact rather than a behavioural coincidence.
//
// A behavioural test — press the key, look at the box — passes against an
// implementation with two bools that the commands happen to keep in step
// today. What makes the claim durable is that the checks are COMPUTEDS
// over one source: a computed is the read-only projection, so there is no
// way to write one, and therefore no way for the two to disagree.
func TestTheCheckAndTheAcceleratorAreOneState(t *testing.T) {
	ed, _ := buildPage(t)

	for name, p := range map[string]*prop.Property[bool]{
		"BuiltinChecked": ed.builtinChecked,
		"EditorChecked":  ed.editorChecked,
		"DesignChecked":  ed.designChecked,
		"CodeChecked":    ed.codeChecked,
	} {
		if p == nil {
			t.Fatalf("%s is nil", name)
		}
		if p.Settable() {
			t.Errorf("%s is a SOURCE property: a check that can be written directly is a "+
				"second copy of the state it displays, and the two will disagree", name)
		}
	}

	// The radio property: exactly one of the pair, always, for every
	// value the state can take — including one it should never take.
	for _, which := range []int{codeBuiltin, codeExternal, 99} {
		ed.codeView.Set(which)
		b, e := ed.builtinChecked.Get(), ed.editorChecked.Get()
		if which == 99 {
			if b || e {
				t.Errorf("codeView=%d checked something; an out-of-range state must check "+
					"neither rather than default one on", which)
			}
			continue
		}
		if b == e {
			t.Errorf("codeView=%d has builtin=%v editor=%v; exactly one must be checked",
				which, b, e)
		}
	}
}

// TestTheAcceleratorAndTheMenuItemNameOneBinding — the identity claim,
// asserted where identity is actually DECIDED.
//
// Comparing the two gooey.Action values directly is not available and
// should not be faked: gooey.Command is a func type, so `==` on it panics
// with "comparing uncomparable type", and the pointer tricks that get
// around that compare CODE addresses — two evaluations of one literal
// share an address, so the check would pass exactly when it should fail.
//
// The real question is upstream of the values anyway. markup resolves
// Command="{{.Name}}" through ctx.Command(name), which is a lookup in one
// map, so two sites naming the same binding get the same value BY
// CONSTRUCTION. What can drift is the NAMES — a key wired to UseBuiltin
// and an item wired to something else — and that is a fact about the
// page, checkable exactly.
//
// The behavioural arm is the discrimination half: a binding that resolves
// to an action which does not move the state would satisfy the name check
// and still be wrong.
func TestTheAcceleratorAndTheMenuItemNameOneBinding(t *testing.T) {
	ed, _ := buildPage(t)
	src, err := os.ReadFile("wysiwyg.gooey")
	if err != nil {
		t.Fatal(err)
	}
	page := string(src)

	bound := map[string]bool{}
	for _, m := range keyBindingRe.FindAllStringSubmatch(page, -1) {
		bound[m[1]] = true
	}

	bar := theMenuBar(t, ed)
	_, view := menuNamed(t, bar, "View")

	// text -> the binding both the item and the key must name.
	for text, binding := range map[string]string{
		"Built in": "UseBuiltin",
		"Design":   "ShowDesign",
	} {
		var item components.MenuItem
		found := false
		for _, it := range view.Items {
			if strings.ReplaceAll(it.Text, "_", "") == text {
				item, found = it, true
			}
		}
		if !found || item.Action == nil {
			t.Fatalf("the View menu has no %q item with an action", text)
		}
		if !strings.Contains(page, `Command="{{.`+binding+`}}"`) {
			t.Errorf("the page never names {{.%s}}", binding)
		}
		if !bound[binding] && binding != "ShowDesign" {
			t.Errorf("{{.%s}} has no KeyBinding; the accelerator and the item cannot be "+
				"one action if only one of them exists", binding)
		}
	}

	// And the item really moves the ONE state, so a correctly-named
	// binding pointing at an inert action still fails.
	ed.codeView.Set(codeExternal)
	for _, it := range view.Items {
		if strings.ReplaceAll(it.Text, "_", "") == "Built in" {
			it.Action.Run()
		}
	}
	if ed.codeView.Get() != codeBuiltin {
		t.Error(`activating the "Built in" item did not move codeView; the item is wired ` +
			"to something that is not the state its check displays")
	}
	if !ed.builtinChecked.Get() || ed.editorChecked.Get() {
		t.Error("after activating the item the two checks disagree with the state")
	}
}

// TestTogglingTheViewerRepaintsOnlyTheOpenDropdown is the damage pin for
// the check state. The box is read inside MenuBar.drawDropdown, which
// runs inside the dropdown's own paint node, so flipping the state must
// cost the dropdown and nothing else.
func TestTogglingTheViewerRepaintsOnlyTheOpenDropdown(t *testing.T) {
	ed, root := buildPage(t)
	c := gooey.NewComposer(root, 150, 44)
	c.Frame()
	settle(t, c)

	bar := theMenuBar(t, ed)
	i, _ := menuNamed(t, bar, "View")
	bar.Open(i, nil)
	settle(t, c)
	if !bar.IsOpen() {
		t.Fatal("the View menu did not open; every count below would measure nothing")
	}

	ed.codeView.Set(codeExternal)
	_, painted := c.Frame()
	if painted == 0 {
		t.Fatal("flipping the code viewer with the menu OPEN repainted nothing: the check " +
			"box on screen is now stale, which means the box is not read while painting")
	}
	t.Logf("check flip with the dropdown open repainted %d", painted)
	if painted != 1 {
		t.Errorf("flipping the check repainted %d components, want 1 (the dropdown); "+
			"damage %v", painted, c.Damage())
	}

	// And with the menu CLOSED it costs nothing at all, because nothing on
	// screen is showing the box.
	bar.Dismiss()
	settle(t, c)
	ed.codeView.Set(codeBuiltin)
	_, closed := c.Frame()
	if closed > 1 {
		t.Errorf("flipping the check with the menu closed repainted %d; the box is not on "+
			"screen, so nothing should be reading it", closed)
	}
}

// TestTheCheckBoxIsDrawn — the state has to be VISIBLE, in the cell
// plane, or a pty transcript can never show it.
func TestTheCheckBoxIsDrawn(t *testing.T) {
	ed, root := buildPage(t)
	c := gooey.NewComposer(root, 150, 44)
	c.Frame()
	settle(t, c)

	bar := theMenuBar(t, ed)
	i, _ := menuNamed(t, bar, "View")
	ed.codeView.Set(codeBuiltin)
	bar.Open(i, nil)
	settle(t, c)
	f, _ := c.Frame()

	b := bar.Bounds()
	var dropdown []string
	for y := b.Y + 1; y < b.Y+20 && y < 44; y++ {
		dropdown = append(dropdown, rowText(f, y, 0, 60))
	}
	all := strings.Join(dropdown, "\n")
	if !strings.Contains(all, "[x] Built in") {
		t.Errorf("the open View menu does not show a checked \"Built in\"; got:\n%s", all)
	}
	if !strings.Contains(all, "[ ] $EDITOR") {
		t.Errorf("the open View menu does not show an unchecked $EDITOR item; got:\n%s", all)
	}
	// The unchecked box must be a real box, not blank: "[ ]" and nothing
	// at all read very differently to a user deciding which is selected.
	if strings.Contains(all, "  $EDITOR") {
		t.Error("the $EDITOR item has no check box at all, only indentation")
	}
}

// TestEditorLabelResolvesTheProgram is the second thing that was
// specified: an item reading "$EDITOR" tells you nothing about what will
// open.
func TestEditorLabelResolvesTheProgram(t *testing.T) {
	t.Setenv("EDITOR", "")
	if got := editorItemText(mustResolve(t)); !strings.Contains(got, "unset") {
		t.Errorf("with EDITOR empty the item reads %q; it must read as unset", got)
	}

	// A program that certainly exists on any machine running these tests.
	t.Setenv("EDITOR", "/usr/bin/env -i")
	got := editorItemText(mustResolve(t))
	if !strings.Contains(got, "(env)") {
		t.Errorf("with EDITOR=%q the item reads %q; it must name the resolved program, and "+
			"the BASENAME of it — a full path with flags is not a label", "/usr/bin/env -i", got)
	}
	if strings.Contains(got, "/usr/bin") || strings.Contains(got, "-i") {
		t.Errorf("the item reads %q; it is printing the raw variable rather than resolving it", got)
	}

	// An $EDITOR naming something that is not installed is the case the
	// resolved label exists to expose. Claiming it is there would be
	// worse than printing "$EDITOR".
	t.Setenv("EDITOR", "definitely-not-a-real-program-xyzzy")
	if got := editorItemText(mustResolve(t)); !strings.Contains(got, "not found") {
		t.Errorf("with an uninstalled EDITOR the item reads %q; it must say so", got)
	}
}

func mustResolve(t *testing.T) string {
	t.Helper()
	label, _ := resolveEditor()
	return label
}

// TestAnUnavailableEditorItemIsDisabled — the menu dims it rather than
// offering a key that fails. MenuBar reads CanExecute while PAINTING, so
// this also un-dims with no event anywhere.
func TestAnUnavailableEditorItemIsDisabled(t *testing.T) {
	t.Setenv("EDITOR", "")
	ed, _ := buildPage(t)
	bar := theMenuBar(t, ed)
	_, view := menuNamed(t, bar, "View")
	for _, it := range view.Items {
		if strings.Contains(it.Text, "EDITO") {
			if gooey.CanExecute(it.Action) {
				t.Error("with $EDITOR unset the item is still executable; a menu that offers " +
					"an action it cannot perform is worse than one that dims it")
			}
			return
		}
	}
	t.Fatal("no $EDITOR item in the View menu")
}

// TestSaveIsDisabledUntilThereIsSomethingToSaveTo — the honest greyed
// Save. Both arms, so the test cannot pass against an always-disabled or
// an always-enabled command.
func TestSaveIsDisabledUntilThereIsSomethingToSaveTo(t *testing.T) {
	ed, _ := buildPage(t)
	save, ok := ed.ctx.Values["Save"].(gooey.Action)
	if !ok {
		t.Fatal("Save is not an Action")
	}
	if gooey.CanExecute(save) {
		t.Error("Save is available with no folder open; there is nowhere for it to write")
	}

	dir := t.TempDir()
	if err := os.WriteFile(dir+"/doc.gooey", []byte("<Gooey><Canvas Name=\"R\"/></Gooey>\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ed.setWorkspace(dir)
	ed.openWorkspaceFile("doc.gooey")
	if !gooey.CanExecute(save) {
		t.Error("Save is still disabled with a file open from a real directory; the " +
			"condition is not reading the state it claims to")
	}
}

// TestTheRegionSwapChangesWhatTheEditorAreaShows — the region is the same
// region, not a second panel. Asserted through the two panels' bounds:
// the visible one occupies the pane, the other occupies nothing.
func TestTheRegionSwapChangesWhatTheEditorAreaShows(t *testing.T) {
	ed, root := buildPage(t)
	c := gooey.NewComposer(root, 150, 44)
	c.Frame()
	settle(t, c)

	area := shellRegion(t, ed, "EditorArea")
	code := shellRegion(t, ed, "CodeArea")

	ed.region.Set(regionDesign)
	settle(t, c)
	db, cb := area.Bounds(), code.Bounds()
	if db.W <= 0 || db.H <= 0 {
		t.Fatalf("in DESIGN the designer occupies %+v", db)
	}
	if cb.W > 0 && cb.H > 0 {
		t.Errorf("in DESIGN the code view still occupies %+v; the two are side by side "+
			"rather than sharing one region", cb)
	}

	ed.region.Set(regionCode)
	settle(t, c)
	db2, cb2 := area.Bounds(), code.Bounds()
	if cb2.W <= 0 || cb2.H <= 0 {
		t.Errorf("in CODE the code view occupies %+v; the swap did nothing", cb2)
	}
	if db2.W > 0 && db2.H > 0 {
		t.Errorf("in CODE the designer still occupies %+v", db2)
	}
	// And the code view lands where the designer was: SAME REGION.
	if cb2.X != db.X || cb2.Y != db.Y {
		t.Errorf("the code view is at %+v where the designer was at %+v; Code is supposed "+
			"to be the same region showing something else", cb2, db)
	}
}

// TestTheRegionSwapIsOneState — the menu's Design/Code checks and the
// swap are the same property, so they cannot disagree.
func TestTheRegionSwapIsOneState(t *testing.T) {
	ed, _ := buildPage(t)
	ed.region.Set(regionDesign)
	if !ed.designChecked.Get() || ed.codeChecked.Get() {
		t.Error("in DESIGN the menu checks disagree with the region")
	}
	ed.swapRegion()
	if ed.designChecked.Get() || !ed.codeChecked.Get() {
		t.Error("after swapRegion the menu checks disagree with the region")
	}
	ed.swapRegion()
	if !ed.designChecked.Get() {
		t.Error("swapRegion is not a toggle")
	}
}

// TestNoMenuItemAdvertisesAnUndeliverableKey.
//
// A MenuItem's Gesture is a DISPLAY hint — showing it does not bind it —
// so nothing in the framework checks that the key it names can ever
// arrive. `ctrl+“ is the trap: it PARSES fine and can never be decoded,
// because the decoder maps control bytes through `c | 0x40`, which covers
// @ A-Z [ \ ] ^ _ and stops well short of 0x60.
//
// An editor whose menu advertises a key that does nothing is an editor
// lying about its own keyboard, and this is the only thing that would
// notice.
func TestNoMenuItemAdvertisesAnUndeliverableKey(t *testing.T) {
	ed, _ := buildPage(t)
	bar := theMenuBar(t, ed)
	checked := 0
	for _, m := range bar.Menus {
		for _, it := range m.Items {
			if it.Gesture == "" {
				continue
			}
			checked++
			ev, err := input.ParseGesture(it.Gesture)
			if err != nil {
				t.Errorf("menu item %q advertises %q, which does not parse: %v", it.Text, it.Gesture, err)
				continue
			}
			if ev.Mods&input.ModCtrl != 0 && ev.Key == input.KeyRune {
				// The inverse of the decoder's own mapping: a control
				// byte becomes `c | 0x40` lowercased, so only runes in
				// @ A-Z [ \ ] ^ _ (lowercased) can ever be produced.
				r := ev.Rune
				if r == ' ' {
					continue // ctrl+space is NUL, which does arrive
				}
				up := r
				if r >= 'a' && r <= 'z' {
					up = r - 'a' + 'A'
				}
				if up < 0x40 || up > 0x5f {
					t.Errorf("menu item %q advertises %q, which PARSES but can never be "+
						"delivered: the decoder maps control bytes through c|0x40, which "+
						"never reaches %q", it.Text, it.Gesture, r)
				}
			}
		}
	}
	if checked == 0 {
		t.Fatal("no menu item carries a Gesture; this test would pass vacuously")
	}
	t.Logf("checked %d advertised gestures", checked)
}

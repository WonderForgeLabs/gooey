package main

// The bindable surface IS the vendor grant, so it gets a test.
//
// This exists because of a bug it would have caught in one second and
// which instead cost twenty minutes: a field declared on Store and never
// initialised leaves a TYPED NIL in Context.Values. Markup accepts it —
// boundProp's type assertion succeeds on a typed nil — so the page loads,
// the Border builds, the app runs, and the first Get() on that handle
// takes down whatever called it. Here that was list_values, which is to
// say: the app worked perfectly until a vendor looked at it.
//
// Nothing in the framework will catch this. The test walks the whole
// surface and Gets everything, which is exactly what a client does.

import (
	"os"
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/control"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

// stubHost is a Service host with no run loop: Post runs inline, and the
// tree operations are unreachable from Values.
type stubHost struct{}

func (stubHost) Post(fn func())            { fn() }
func (stubHost) Composer() *gooey.Composer { return nil }
func (stubHost) Swap(gooey.Component)      {}

func TestBindableSurfaceHasNoNilHandles(t *testing.T) {
	s := NewStore(nil)
	svc := control.NewService(stubHost{}, s.Context(s.logo))

	entries, _, err := svc.Values()
	if err != nil {
		t.Fatalf("Values: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no entries — the grant is empty")
	}
	for _, e := range entries {
		t.Logf("%-13s kind=%v type=%v %s", e.Name, e.Kind, e.Type, e.GoType)
	}
}

// The seam Chromatica is sold has to exist, with the type its markup
// binds. A rename here is a broken product, not a broken test.
func TestTintIsAColorHandle(t *testing.T) {
	s := NewStore(nil)
	svc := control.NewService(stubHost{}, s.Context(s.logo))

	entries, _, err := svc.Values()
	if err != nil {
		t.Fatalf("Values: %v", err)
	}
	for _, e := range entries {
		if e.Name != "Tint" {
			continue
		}
		if e.Type != control.KindColor {
			t.Fatalf("Tint is %v, want color", e.Type)
		}
		return
	}
	t.Fatal("no Tint in the bindable surface")
}

// A refusal has to appear on the pane that owns the button that caused
// it. Subscribe() raises both of its refusals while the PURCHASE SHEET
// is up, and the receipt Text used to exist only on the store pane —
// which the sheet covers. The app declined the purchase correctly,
// wrote the reason correctly, and showed nothing, so the button read as
// dead. "I clicked subscribe and nothing happens" was the report.
func TestARefusalIsVisibleOnTheSheetThatRaisedIt(t *testing.T) {
	s := NewStore(os.DirFS("."))

	// Ledgerline is $49.00 against a $40.00 wallet: a real decline.
	s.itemSel.Set(2)
	s.Buy()
	if got := s.pane.Get(); got != panePurchase {
		t.Fatalf("Buy should open the sheet, got pane %q", got)
	}
	s.Subscribe()

	if s.pane.Get() != panePurchase {
		t.Fatal("a declined purchase must leave the sheet up, or the reason scrolls past")
	}
	if s.receipt.Get() == "" {
		t.Fatal("a decline wrote no reason at all")
	}
	if !s.bad.Get() {
		t.Error("a decline must be marked bad, or it renders in the confirmation colour")
	}

	// The sheet is what is visible, so the receipt must be reachable
	// from the sheet's own markup — not only from the store pane's.
	src, err := os.ReadFile("store.gooey")
	if err != nil {
		t.Fatal(err)
	}
	sheet := sheetMarkup(t, string(src))
	if !strings.Contains(sheet, "{{.Receipt}}") {
		t.Error("the purchase sheet has no {{.Receipt}} line; a refusal raised there is invisible")
	}
	if !strings.Contains(sheet, "{{.ReceiptStyle}}") {
		t.Error("the sheet's receipt is not styled by outcome; a decline reads as a confirmation")
	}
}

// A confirmation and a refusal must not look the same.
func TestRefusalAndConfirmationDifferInStyle(t *testing.T) {
	s := NewStore(os.DirFS("."))
	vals := s.Context(s.logo).Values

	style := func() render.Style {
		p, ok := vals["ReceiptStyle"].(*prop.Property[render.Style])
		if !ok {
			t.Fatalf("ReceiptStyle is %T, not a bound style handle", vals["ReceiptStyle"])
		}
		return p.Get()
	}

	s.confirm("subscribed")
	good := style()
	s.refuse("card declined")
	if style() == good {
		t.Fatal("a refusal renders in the same style as a confirmation")
	}
}

// sheetMarkup returns the <Border Name="Sheet"> subtree, so the
// assertions above cannot be satisfied by the store pane's copy.
func sheetMarkup(t *testing.T, src string) string {
	t.Helper()
	i := strings.Index(src, `Name="Sheet"`)
	if i < 0 {
		t.Fatal(`no element named "Sheet" in store.gooey`)
	}
	j := strings.Index(src[i:], "</Border>")
	if j < 0 {
		t.Fatal("unterminated Sheet element")
	}
	return src[i : i+j]
}

package main

// The bindable surface: values, never sentences.
//
// Two things here are load-bearing for the demo rather than for the app.
//
// First, everything a vendor can reach is in this map. There is no other
// door. A vendor that wants to change a colour can only change a colour
// this app already had a handle for, and a vendor that wants to add
// state has to register it — visibly, by name, all-or-nothing.
//
// Second, Logo is an image.Image handle rather than a path string. That
// is not a convenience: <Image Src="…"> takes either a literal path
// resolved against THIS app's filesystem, or a handle THIS app already
// holds. A vendor can restructure the whole interface over MCP and
// cannot introduce a single pixel that the app owner did not ship.

import (
	"image"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

func (s *Store) Context(logo *prop.Property[image.Image]) *markup.Context {
	text := func(f func() string) *prop.Property[string] { return prop.NewComputed(f) }
	is := func(name string) *prop.Property[bool] {
		return prop.NewComputed(func() bool { return s.pane.Get() == name })
	}

	buying := func() Integration {
		items := s.items.Get()
		i := s.buying.Get()
		if i < 0 || i >= len(items) {
			return Integration{}
		}
		return items[i]
	}
	selected := func() Integration {
		items := s.items.Get()
		i := clamp(s.itemSel.Get(), 0, len(items)-1)
		if len(items) == 0 {
			return Integration{}
		}
		return items[i]
	}

	return &markup.Context{
		Values: map[string]any{
			// --- the host app's own screen ---------------------------
			"Services": components.Items(s.services, func(v Service) map[string]any {
				return map[string]any{
					"Name":   v.Name,
					"Region": v.Region,
					"State":  v.State,
					"Load":   itoa(v.Load) + "%",
					// A bound Style needs a render.Style HANDLE, not a
					// style name: bindStyle resolves {{.X}} through
					// boundProp[render.Style]. Sty wraps the value.
					"Style": sty(ifElse(v.State == "degraded", "warn", "body")),
				}
			}),
			"ServiceSel": s.svcSel,

			// --- the marketplace -------------------------------------
			"Items": components.Items(s.items, func(v Integration) map[string]any {
				return map[string]any{
					"Name":   v.Name,
					"Vendor": v.Vendor,
					"Price":  money(v.Cents) + "/mo",
					"State":  ifElse(v.Active, "active", "available"),
					"Usage":  ifElse(v.Active, itoa(v.Calls)+" calls", "—"),
					"Style":  sty(ifElse(v.Active, "ok", "dim")),
				}
			}),
			"ItemSel": s.itemSel,

			// --- what the sheet is selling ---------------------------
			"BuyName":   text(func() string { return buying().Name }),
			"BuyVendor": text(func() string { return buying().Vendor }),
			"BuyBlurb":  text(func() string { return buying().Blurb }),
			"BuyPrice":  text(func() string { return money(buying().Cents) }),
			"Logo":      logo,

			// --- the selected row, for the store footer --------------
			"SelVendor": text(func() string { return selected().Vendor }),
			"SelActive": prop.NewComputed(func() bool { return selected().Active }),

			// --- money, mocked, and moving ---------------------------
			"Wallet":  text(func() string { return money(s.walletC.Get()) }),
			"Monthly": text(func() string { return money(s.monthlyC.Get()) }),
			"Receipt": s.receipt,
			"HasReceipt": prop.NewComputed(func() bool {
				return s.receipt.Get() != ""
			}),
			// A bound style HANDLE, not a style name: markup will not
			// resolve a string against the palette for you, which is the
			// same rule that stops a vendor naming a style it invented.
			//
			// It exists because a refusal and a confirmation were the
			// same green line. "card declined" in the colour reserved for
			// "subscribed" is a message that reads as its own opposite at
			// a glance, which on a slide is worse than no message.
			"ReceiptStyle": prop.NewComputed(func() render.Style {
				if s.bad.Get() {
					return palette()["warn"]
				}
				return palette()["receipt"]
			}),

			// The one handle a vendor is going to be paid to move. It is
			// here because the app has a theme, not because anyone
			// anticipated a marketplace.
			"Tint": s.tint,

			// The dialog's own fill. It exists so the sheet can be a box
			// with the app visible behind it: without a declared
			// background the Border paints only its chrome and the store
			// list shows straight through the dialog.
			"SheetBg": prop.NewComputed(func() render.Color {
				// Two steps up from the shell tint, so the dialog reads
				// as being in front of the app rather than beside it —
				// and derived from Tint rather than fixed, because
				// Chromatica is going to move Tint and a dialog that did
				// not follow would look like a different program.
				c := s.tint.Get()
				return render.RGB(lift(c.R), lift(c.G), lift(c.B))
			}),

			// --- which pane -------------------------------------------
			"ShowOps": is(paneOps),
			// The store list stays on screen UNDER the purchase sheet.
			// It used to collapse, which is what made the sheet fill the
			// window: a dialog with nothing behind it has nothing to be a
			// dialog over, so it took the whole row. The <Modal> wrapper
			// is what makes leaving it visible safe.
			"ShowStore": prop.NewComputed(func() bool {
				p := s.pane.Get()
				return p == paneStore || p == panePurchase
			}),
			"ShowPurchase": is(panePurchase),

			// --- verbs ------------------------------------------------
			"OpenStore":  gooey.Command(s.OpenStore),
			"CloseStore": gooey.Command(s.CloseStore),
			"Buy":        gooey.Command(s.Buy),
			"Dismiss":    gooey.Command(s.Dismiss),
			"Subscribe":  gooey.Command(s.Subscribe),
			"Cancel":     gooey.Command(s.Cancel),
			"Tick":       gooey.Command(s.Tick),
			"Quit":       gooey.Command(s.Quit),
		},
		Styles: palette(),
	}
}

// sty resolves a style name to a handle for an item template. The
// palette is the app owner's; an item row picks from it and cannot
// invent one.
func sty(name string) *prop.Property[render.Style] {
	return components.Sty(palette()[name])
}

// lift brightens one channel for the dialog fill, saturating rather
// than wrapping — a naive +24 on a channel already near 255 rolls over
// to black, which shows up as one corner of the dialog being a different
// colour from the rest.
func lift(v uint8) uint8 {
	if v > 255-24 {
		return 255
	}
	return v + 24
}

func ifElse(cond bool, a, b string) string {
	if cond {
		return a
	}
	return b
}

func palette() map[string]render.Style {
	return map[string]render.Style{
		"panel":   {Fg: render.RGB(96, 118, 158)},
		"chrome":  {Fg: render.RGB(140, 150, 168)},
		"head":    {Fg: render.RGB(232, 236, 244), Bold: true},
		"body":    {Fg: render.RGB(206, 212, 224)},
		"dim":     {Fg: render.RGB(122, 130, 146)},
		"ok":      {Fg: render.RGB(110, 200, 150)},
		"warn":    {Fg: render.RGB(240, 160, 90), Bold: true},
		"price":   {Fg: render.RGB(255, 200, 90), Bold: true},
		"vendor":  {Fg: render.RGB(150, 130, 235), Bold: true},
		"sheet":   {Fg: render.RGB(180, 190, 210)},
		"receipt": {Fg: render.RGB(110, 200, 150), Bold: true},
	}
}

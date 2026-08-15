package main

// Store is the running app: Northwind Ops, plus the integrations
// marketplace bolted onto its toolbar.
//
// State lives here as typed handles. The markup composes it. The billing
// numbers are invented, and they mutate for real — subscribing moves the
// wallet, flips the row to active, and starts the usage counter, because
// a purchase flow that does nothing is not a purchase flow, it is a
// screenshot.

import (
	"image"
	"io/fs"
	"strconv"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/control"
	"github.com/WonderForgeLabs/gooey/imaging"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

// pane names. The app shows exactly one at a time; each is a Collapsed
// row when it is not the one, so a hidden pane reserves no height.
const (
	paneOps      = "ops"
	paneStore    = "store"
	panePurchase = "purchase"
)

type Store struct {
	app *gooey.App
	svc *control.Service
	dir fs.FS

	// logo is the sheet's artwork, loaded from the app's OWN filesystem.
	// It is a handle rather than a path because <Image Src> only accepts
	// a literal path or a handle the app already holds — which is exactly
	// why a vendor cannot supply its own imagery over the wire.
	logo *prop.Property[image.Image]

	// Tint is the shell's background colour. The app owner shipped it —
	// it is an ordinary theme handle and nothing in this program has
	// heard of Chromatica. What Chromatica sells is the UI for changing
	// it, which is a fair description of most of this industry.
	tint *prop.Property[render.Color]

	// --- the app's own domain ---
	services *prop.Property[[]Service]
	svcSel   *prop.Property[int]

	// --- the marketplace ---
	items   *prop.Property[[]Integration]
	itemSel *prop.Property[int]
	pane    *prop.Property[string]
	buying  *prop.Property[int] // index into items, while the sheet is up
	receipt *prop.Property[string]

	// bad separates a refusal from a confirmation. Both used to be the
	// same green line, and both used to be written to a Text that lives
	// on the STORE pane — so a refusal raised while the purchase sheet
	// was up went to an element the sheet was covering. Clicking
	// subscribe with an insufficient balance set
	// "card declined — insufficient balance" and showed nothing at all,
	// which is the worst possible reading of a refusal: the app looks
	// broken instead of looking careful.
	//
	// A message has to appear where the button that caused it is.
	bad *prop.Property[bool]

	// resync forces a structural re-sync. It is a func rather than a
	// reach through s.app because the thing that has to be invalidated
	// is a Composer, and a test drives one of those without an App.
	// Wired in main.go; nil until then, which is correct — nothing is
	// mounted before the app exists.
	resync func()

	walletC  *prop.Property[int] // cents
	monthlyC *prop.Property[int] // cents committed per month

	ticks int
}

func NewStore(dir fs.FS) *Store {
	items := catalog()
	s := &Store{
		dir:      dir,
		logo:     prop.NewSource[image.Image](nil),
		tint:     prop.NewSource(render.RGB(18, 20, 28)),
		services: prop.NewSource(services()),
		svcSel:   prop.NewSource(0),
		items:    prop.NewSource(items),
		itemSel:  prop.NewSource(0),
		pane:     prop.NewSource(paneOps),
		buying:   prop.NewSource(-1),
		receipt:  prop.NewSource(""),
		bad:      prop.NewSource(false),
		walletC:  prop.NewSource(4000),
	}
	s.monthlyC = prop.NewSource(s.committed(items))
	return s
}

func (s *Store) committed(items []Integration) int {
	total := 0
	for _, it := range items {
		if it.Active {
			total += it.Cents
		}
	}
	return total
}

// --- navigation ------------------------------------------------------

func (s *Store) OpenStore()  { s.setPane(paneStore) }
func (s *Store) CloseStore() { s.setPane(paneOps); s.clearReceipt() }

// setPane guards the Set rather than forcing anything. prop.Set does not
// compare values, so setting the pane to what it already holds
// invalidates every dependent and costs a repaint — and one of those
// dependents is now the observer behind <Modal>.
func (s *Store) setPane(p string) {
	if s.pane.Get() == p {
		return
	}
	s.pane.Set(p)
}

// Blocked reports whether the backdrop is inert — the Modal's predicate,
// and the reason it is a method that READS A PROPERTY rather than a
// captured bool.
//
// That is the whole contract now. Composer.armFrozen wraps every Frozen
// implementer in an observer whose evaluation calls this method, so
// s.pane becomes a dependency by the ordinary call-site rule; the Set
// above schedules a frame, the sweep sees the answer flip, and the
// re-sync — focus order, scoped bindings, mnemonics, hover watchers,
// Startables — runs in the SAME frame, before anything paints.
//
// This used to call Composer.InvalidateStructure by hand through an
// injected func, because Frozen was sampled at a structural re-sync and
// nothing raised one. A bare bool field written by a handler would still
// need that: the observer subscribes to what Frozen() READS, and plain
// Go state records no dependency.
func (s *Store) Blocked() bool { return s.pane.Get() == panePurchase }

// Buy opens the sheet for the selected row. An already-active
// subscription is not re-sold; the row is the config screen instead.
func (s *Store) Buy() {
	items := s.items.Get()
	i := clamp(s.itemSel.Get(), 0, len(items)-1)
	s.buying.Set(i)
	s.clearReceipt()
	// The app owner loads the app owner's asset. Nothing about this path
	// is reachable from the control plane.
	if i >= 0 && i < len(items) {
		if img, err := imaging.Load(s.dir, items[i].Logo); err == nil {
			s.logo.Set(img)
		}
	}
	s.setPane(panePurchase)
}

func (s *Store) Dismiss() {
	s.buying.Set(-1)
	s.clearReceipt()
	s.setPane(paneStore)
}

// --- the mocked half -------------------------------------------------

// Subscribe is where the money would move. It does not move; everything
// downstream of it does. The row flips active, the monthly commitment
// grows, the wallet is charged once, and usage starts counting — so the
// list afterwards is a list of things that are genuinely running.
func (s *Store) Subscribe() {
	i := s.buying.Get()
	items := append([]Integration(nil), s.items.Get()...)
	if i < 0 || i >= len(items) {
		return
	}
	it := &items[i]
	if it.Active {
		s.refuse("already subscribed")
		return
	}
	if s.walletC.Get() < it.Cents {
		s.refuse("card declined — insufficient balance · balance " + money(s.walletC.Get()) + ", this plan " + money(it.Cents))
		return
	}
	it.Active = true
	it.Calls = 0
	s.items.Set(items)
	s.walletC.Set(s.walletC.Get() - it.Cents)
	s.monthlyC.Set(s.committed(items))
	s.confirm("subscribed — " + it.Vendor + " may now modify this app")
	s.setPane(paneStore)
	s.buying.Set(-1)
}

// Cancel is the other half, and it exists because a subscription you
// cannot leave is a different kind of demo.
func (s *Store) Cancel() {
	i := clamp(s.itemSel.Get(), 0, len(s.items.Get())-1)
	items := append([]Integration(nil), s.items.Get()...)
	if i < 0 || i >= len(items) || !items[i].Active {
		return
	}
	items[i].Active = false
	items[i].Calls = 0
	s.items.Set(items)
	s.monthlyC.Set(s.committed(items))
	s.confirm("cancelled " + items[i].Vendor)
}

// Tick jitters the host app's own numbers and advances usage on the
// active integrations. Both are mocked; both are the app behaving like
// something you would actually be looking at.
func (s *Store) Tick() {
	s.ticks++

	svcs := append([]Service(nil), s.services.Get()...)
	for i := range svcs {
		svcs[i].Load = clamp(svcs[i].Load+drift(s.ticks, i), 1, 99)
	}
	s.services.Set(svcs)

	items := append([]Integration(nil), s.items.Get()...)
	touched := false
	for i := range items {
		if items[i].Active {
			items[i].Calls += 3 + (s.ticks+i)%7
			touched = true
		}
	}
	if touched {
		s.items.Set(items)
	}
}

// drift is a deterministic wobble. Math.random would be the obvious
// choice and is the wrong one: a demo that cannot be replayed identically
// is a demo you cannot cut a video against.
func drift(tick, i int) int {
	switch (tick*7 + i*13) % 5 {
	case 0:
		return 2
	case 1:
		return -1
	case 2:
		return 1
	case 3:
		return -2
	}
	return 0
}

// The three ways the receipt line is written. They exist so that no call
// site sets the text without also saying whether it is a refusal —
// which is what let a decline render in the confirmation colour.
func (s *Store) refuse(msg string)  { s.receipt.Set(msg); s.bad.Set(true) }
func (s *Store) confirm(msg string) { s.receipt.Set(msg); s.bad.Set(false) }
func (s *Store) clearReceipt()      { s.receipt.Set(""); s.bad.Set(false) }

func (s *Store) Quit() { s.app.Quit() }

func clamp(n, lo, hi int) int {
	if hi < lo {
		return lo
	}
	if n < lo {
		return lo
	}
	if n > hi {
		return hi
	}
	return n
}

func itoa(n int) string { return strconv.Itoa(n) }

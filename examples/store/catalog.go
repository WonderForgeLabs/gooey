package main

// The three parties, as data.
//
// This demo exists to make one thing visible: the person using the app,
// the company that shipped it, and the company being paid to change it
// are three different entities. Every other model collapses two of them
// — a browser extension is authorised by the user with no say from the
// app owner, an in-app purchase is sold by the app owner to the user
// with no third party at all, an enterprise plugin is authorised by the
// app owner with no say from the user.
//
// So: Northwind Ops is the app owner. `elan` is the user. Chromatica,
// Vestibule and Ledgerline are vendors, and none of them work for either
// of the other two.
//
// The billing is mocked. Everything gooey does — the injection, the
// patching, the property registration, the control plane the vendors
// reach the app through — is real and local.

// Service is the host app's own domain. It has nothing to do with the
// store, and that is the point: the user opened this program to look at
// these, not to buy anything.
type Service struct {
	Name   string
	Region string
	State  string
	Load   int
}

func services() []Service {
	return []Service{
		{"ingest-api", "us-east-1", "healthy", 34},
		{"ledger-writer", "us-east-1", "healthy", 61},
		{"reconciler", "eu-west-2", "degraded", 88},
		{"notify-fanout", "us-west-2", "healthy", 12},
		{"archive-sweep", "eu-west-2", "healthy", 7},
		{"edge-cache", "ap-south-1", "healthy", 45},
	}
}

// Integration is one thing on sale. Vendor is a separate field from Name
// on purpose: the list has to make it obvious that the thing changing
// your UI is not the thing you bought the app from.
type Integration struct {
	ID     string
	Name   string
	Vendor string
	Blurb  string
	Logo   string // path in the app's own FS — see the note below
	Cents  int    // per month
	Active bool
	Calls  int // usage this billing period
}

// The logo path matters more than it looks. <Image Src="…"> resolves a
// literal against the PAGE'S filesystem, and a vendor reaching the app
// over MCP can only ever name a path that is already there. An agent can
// restructure this entire interface and cannot add one byte of imagery
// to it. Every logo below was shipped by the app owner.
func catalog() []Integration {
	return []Integration{
		{
			ID:     "chromatica",
			Name:   "UI Customization",
			Vendor: "Chromatica",
			Blurb:  "Recolour any panel. Adds a picker to the toolbar you already use.",
			Logo:   "assets/chromatica.svg",
			Cents:  199,
		},
		{
			ID:     "vestibule",
			Name:   "On-call Routing",
			Vendor: "Vestibule Systems",
			Blurb:  "Pages the right human. Reads service state; writes nothing.",
			Logo:   "assets/vestibule.svg",
			Cents:  1200,
			Active: true,
			Calls:  8143,
		},
		{
			ID:     "ledgerline",
			Name:   "Cost Attribution",
			Vendor: "Ledgerline",
			Blurb:  "Spend per service, per region, per team.",
			Logo:   "assets/ledgerline.svg",
			Cents:  4900,
		},
	}
}

// money renders cents. A value conversion, not a layout decision — the
// markup decides where it goes and how wide the column is.
func money(cents int) string {
	return "$" + itoa(cents/100) + "." + pad2(cents%100)
}

func pad2(n int) string {
	if n < 10 {
		return "0" + itoa(n)
	}
	return itoa(n)
}

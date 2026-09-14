package mcp

import (
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/control"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/term"
)

// screenSizeRootMarkup declares every shape that is supposed to detach a
// root from the screen: a margin, a fixed size, and a non-stretch
// alignment on both axes.
//
// All five, because the claim this fixture backs names all five. An
// earlier version declared Width/Height/Margin while the prose said
// alignment had been measured too — an overclaim of exactly the kind
// this test exists to retire, caught in review of #504.
const screenSizeRootMarkup = `<Gooey>
  <Border Name="Inset" Width="20" Height="5" Margin="2" HAlign="Start" VAlign="Start">
    <Text Name="InsetText">inset</Text>
  </Border>
</Gooey>`

// TestTheRootAlwaysFillsTheScreen corrects this feature's own stated
// rationale, and is the reason the correction cannot rot.
//
// ScreenSize's doc comment and issue #204 both said the root-bounds
// inference "equals the terminal only while the root happens to fill it —
// give the root a margin, a fixed Width or a non-stretch alignment and
// the client silently computes coordinates against a screen that is not
// there." That is FALSE, and measurably so: Composer.Frame arranges the
// root with `c.root.Arrange(Rect{0, 0, c.cols, c.rows})` and Base.Arrange
// is `e.bounds = b`, so the root stores the screen whatever it declares.
// Margin, Width and Height are applied by MeasureChild/ArrangeChild — the
// sandwich the root, being nobody's child, never passes through.
//
// So the inference was RELIABLE for an unscoped session, and screen_size
// earns its place for the other three reasons instead: a scoped session's
// island genuinely is not the screen (the case with a wrong answer, not
// merely an unproven one), screen_text's lines are trailing-trimmed so
// the width it implies is the longest PAINTED line, and learning two
// integers should not cost a whole tree.
//
// If someone ever makes the root honour its own size, this test fails and
// the old justification becomes true again — which is the point of
// pinning it rather than deleting the sentence.
func TestTheRootAlwaysFillsTheScreen(t *testing.T) {
	app := newTestApp(t, screenSizeRootMarkup, nil)
	s, err := New(app, Options{Context: app.ctx, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c := newClient(t, s)

	wantCols, wantRows := screenOf(t, app)
	b := rootBounds(t, c.json("tree_snapshot", nil))
	if b.W != wantCols || b.H != wantRows {
		t.Errorf("a root declaring Width=20 Height=5 Margin=2 HAlign=Start VAlign=Start "+
			"reports bounds %dx%d; "+
			"want the full %dx%d, because Composer.Frame arranges the root to the "+
			"screen and Base.Arrange stores what it is given",
			b.W, b.H, wantCols, wantRows)
	}

	sz := c.json("screen_size", nil)
	if int(sz["cols"].(float64)) != wantCols || int(sz["rows"].(float64)) != wantRows {
		t.Errorf("screen_size reports %vx%v, want the terminal's %dx%d",
			sz["cols"], sz["rows"], wantCols, wantRows)
	}
	// THE ORIGIN IS THE OTHER HALF OF THE CONTRACT, and it was the half
	// no test read. The schema promises x/y are 0 for an unscoped
	// session — "the screen is the region" — and a client adds them to
	// every coordinate it sends, so a non-zero pair here would displace
	// every press by the same offset, silently. Its scoped twin is
	// TestAGuestIsToldWhereItsIslandIs. Raised in review of #504.
	if x, ok := sz["x"].(float64); !ok || x != 0 {
		t.Errorf("x = %v on an unscoped session, want 0: the whole screen is the "+
			"region, so its origin is the screen's", sz["x"])
	}
	if y, ok := sz["y"].(float64); !ok || y != 0 {
		t.Errorf("y = %v on an unscoped session, want 0: the whole screen is the "+
			"region, so its origin is the screen's", sz["y"])
	}
}

// TestTheCellMetricsSayWhenNobodyMeasured pins BOTH arms, because the
// interesting one is the zero.
//
// The probe that fills these is opt-in (gooey.WithCapabilityProbe), so
// an UNPROBED host reports 0 — and a client doing
// `pixels = cols * cellWidth` gets 0 while one doing `cols / cellWidth`
// divides by zero. The schema says 0 means "never probed"; this is what
// makes that a checked claim rather than a sentence.
//
// NOT "a cell-plane app reports 0", which is what this said and is
// false the moment the probe runs. term.Screen.Detect substitutes
// DefaultCellW/H on `caps.CellW == 0` with no plane test at all, so a
// probed cell-plane app reports a cell size it never measured; App's
// pixel-plane backfill is a SECOND substitution site that only fires
// where Detect did not. The unprobed arm below is therefore about the
// probe, not about the plane. Corrected in review of #504.
//
// The earlier version of this assertion was `got["cellWidth"] == nil`,
// which a JSON 0 satisfies — so it was green over exactly the case it
// looked like it was covering.
func TestTheCellMetricsSayWhenNobodyMeasured(t *testing.T) {
	t.Run("unprobed reports zero", func(t *testing.T) {
		_, _, _, c := setup(t)
		got := c.json("screen_size", nil)
		if w, ok := got["cellWidth"].(float64); !ok || w != 0 {
			t.Errorf("cellWidth = %v on a host that never probed, want 0", got["cellWidth"])
		}
		if h, ok := got["cellHeight"].(float64); !ok || h != 0 {
			t.Errorf("cellHeight = %v on a host that never probed, want 0", got["cellHeight"])
		}
	})

	t.Run("probed reports what the terminal said", func(t *testing.T) {
		app, _, _, c := setup(t)
		const wantW, wantH = 7, 15
		done := make(chan struct{})
		app.Post(func() {
			app.comp.SetCaps(term.Caps{CellW: wantW, CellH: wantH})
			close(done)
		})
		<-done

		got := c.json("screen_size", nil)
		if int(got["cellWidth"].(float64)) != wantW || int(got["cellHeight"].(float64)) != wantH {
			t.Errorf("cell metrics %vx%v, want the probed %dx%d — the values are passed "+
				"through from Composer.Caps, so a zero here means they were dropped",
				got["cellWidth"], got["cellHeight"], wantW, wantH)
		}
	})
}

// islandOffOriginMarkup puts the island SECOND, so its origin is not
// (0,0).
//
// mcpIslandMarkup puts the Border first, which makes the island's origin
// (0,0) — and there the island-relative and absolute coordinate spaces
// coincide, so every origin bug is invisible. This is the same fixture
// blind spot in the other axis.
const islandOffOriginMarkup = `<Gooey>
  <VStack Gap="0">
    <Text Name="Theirs">{{.Host.Secret}}</Text>
    <Border Name="Mine" Title="mine">
      <Text Name="MineText">{{.Mine.Body}}</Text>
    </Border>
  </VStack>
</Gooey>`

// islandCollapsedMarkup is islandOffOriginMarkup with the island
// collapsed, DERIVED rather than copied so the two fixtures cannot drift
// into being two different pages — which would make "one reports 0x0 and
// the other does not" a statement about the markup instead of about the
// collapse. If the anchor below ever stops matching, Replace returns the
// original and the collapsed arm reports a live size, which is a
// failure, not a silent pass.
var islandCollapsedMarkup = strings.Replace(islandOffOriginMarkup,
	`<Border Name="Mine" Title="mine">`,
	`<Border Name="Mine" Title="mine" Visibility="Collapsed">`, 1)

// islandGuest builds a scoped session over src and returns its client.
//
// The three off-origin cases below each wrote out the same four steps —
// two sources, newTestApp, New with an island grant, newClient — and the
// copies had already begun to differ: one seeded Host.Secret with "s"
// rather than "hunter2" for no reason it states. islandServer
// (grant_test.go:30) is the same shape for the ORIGIN-AT-ZERO fixture and
// cannot serve here; it hard-codes the markup and the island name and
// returns a host client nothing here wants.
//
// BOTH the markup and the island name are parameters, because the cases
// vary along both axes independently: "Ghost" over the ordinary fixture
// is the island that is gone, "Mine" over the collapsed fixture is the
// island that is merely degenerate, and those are the two answers
// islandRect is careful to keep apart.
// It returns the app as well as the client, because a caller that wants
// to probe the terminal's cell size has to Post SetCaps onto the UI
// goroutine and there is no other handle on it.
func islandGuest(t *testing.T, src, island string) (*client, *testApp) {
	t.Helper()
	app := newTestApp(t, src, map[string]any{
		"Mine": map[string]any{"Body": prop.NewSource("m0")},
		"Host": map[string]any{"Secret": prop.NewSource("hunter2")},
	})
	gs, err := New(app, Options{
		Context: app.ctx,
		Timeout: 5 * time.Second,
		Grant:   control.Island(island, island),
	})
	if err != nil {
		t.Fatalf("New (guest): %v", err)
	}
	return newClient(t, gs), app
}

// TestAGuestIsToldWhereItsIslandIs is the half that makes the size
// actionable rather than merely honest.
//
// SendPointer takes ABSOLUTE screen cells and mayPoint refuses anything
// landing outside the island. So a guest told "your screen is 60x3", with
// an island that actually starts at y=1, has one row it cannot reach and
// one the host refuses — the tool would be handing out coordinates its
// own pointer call rejects. The origin is what closes that, and the
// assertion below is the round trip: convert with x/y, and send_mouse
// must accept every corner.
func TestAGuestIsToldWhereItsIslandIs(t *testing.T) {
	guest, _ := islandGuest(t, islandOffOriginMarkup, "Mine")

	sz := guest.json("screen_size", nil)
	x0, y0 := int(sz["x"].(float64)), int(sz["y"].(float64))
	cols, rows := int(sz["cols"].(float64)), int(sz["rows"].(float64))

	if y0 == 0 {
		t.Fatalf("the island reports origin y=0, so this fixture cannot tell an "+
			"origin-aware answer from one that assumes (0,0); sz=%v", sz)
	}
	// Every corner of the island, converted through the reported origin,
	// must be a coordinate send_mouse accepts. Without x/y a guest can
	// only guess these, and the guess is wrong by exactly y0.
	for _, p := range [][2]int{{0, 0}, {cols - 1, 0}, {0, rows - 1}, {cols - 1, rows - 1}} {
		guest.ok("send_mouse", map[string]any{
			"kind": "click", "x": x0 + p[0], "y": y0 + p[1],
		})
	}
	// And the row directly above the island is NOT the guest's, which is
	// what proves the conversion is a translation rather than a blanket
	// permit.
	guest.fails("send_mouse", map[string]any{
		"kind": "click", "x": x0, "y": y0 - 1,
	}, "outside this session's island")
}

// TestAGuestIsToldItsIslandsSize is the half that makes the tool safe to
// add to a scoped session.
//
// A guest's whole screen IS its island — that is the fiction Screen
// already maintains by cropping, and a size tool that answered with the
// terminal would break it in the one direction that matters: a client
// told the screen is 60x14 when it may only touch a 60x3 border computes
// coordinates for cells it cannot reach, and send_mouse answers those
// with silence.
//
// The assertion that the two DIFFER is not decoration. If the island
// happened to fill the terminal, both arms would read the same and this
// test would pass against a tool that ignored the grant entirely.
func TestAGuestIsToldItsIslandsSize(t *testing.T) {
	guest, host := islandServer(t)

	g := guest.json("screen_size", nil)
	h := host.json("screen_size", nil)

	if g["cols"] == h["cols"] && g["rows"] == h["rows"] {
		t.Fatalf("the guest and the host are told the same size (%vx%v), so this test "+
			"cannot tell a scoped answer from an unscoped one", g["cols"], g["rows"])
	}
	// The island is a Border inside a VStack that also holds a Text, so
	// it is strictly shorter than the screen and no wider.
	if int(g["rows"].(float64)) >= int(h["rows"].(float64)) {
		t.Errorf("the guest is told %v rows and the host %v; the island is one of two "+
			"children of a VStack and cannot be as tall as the screen", g["rows"], h["rows"])
	}
	if int(g["cols"].(float64)) > int(h["cols"].(float64)) {
		t.Errorf("the guest is told %v columns, wider than the host's %v", g["cols"], h["cols"])
	}
}

// TestTheTutorialsToolInventoryIsComplete is the class, not the
// instance. The tutorial lists every tool by name in prose, and prose
// enumerating a set is the thing this repo keeps being wrong about — the
// inventory said "no tool reports terminal size" for as long as that was
// true and would have gone on saying it, because nothing reads the list
// but a human.
//
// DERIVED FROM v1Tools, so a tool added tomorrow fails here rather than
// leaving a tutorial that is quietly missing one. The reverse direction
// is deliberately NOT asserted: the page is free to mention a name that
// is not a tool.
//
// It SKIPS rather than fails when the page is out of reach, because
// `mcp` is its own module: the zip a proxy serves contains mcp/ and
// nothing above it, so `../docs` does not exist for anyone consuming the
// module standalone. A guard that cannot run there must say so rather
// than report the repo's docs as broken on someone else's machine.
//
// THE QUESTION IS ASKED ONCE, UP FRONT, and not derived from the read —
// see pageOrSkip. Skipping on fs.ErrNotExist covered "this module is
// being consumed standalone" and "somebody renamed the page and nothing
// follows it any more" with the same green, which is the defect
// TestTheAgentWorkflowsToolInventoriesAreComplete had already fixed for
// the workflow blobs. A guard whose whole premise is that a prose
// inventory goes stale silently cannot disable itself on a rename.
// Raised in review of #504.
func TestTheTutorialsToolInventoryIsComplete(t *testing.T) {
	const page = "../docs/learn/08-remote-control.md"
	body := pageOrSkip(t, page)
	// THE INVENTORY PARAGRAPH, not the page. This handed over the whole
	// tutorial until review of #504, which is the page-wide vacuous pass
	// TestTheGRPCContractTableNamesEveryTool declines by name below: the
	// paragraph after this one explains `screen_size`, so deleting the
	// name from the inventory left the guard green on a mention that is
	// not a list. An inventory is a list, and the assertion has to be
	// made against the list.
	para, ok := paragraphWith(string(body), inventoryLead)
	if !ok {
		t.Fatalf("%s no longer carries a %q paragraph, so either the tutorial "+
			"was restructured and this guard has to follow it, or the inventory "+
			"is gone", page, inventoryLead)
	}
	assertNamesEveryTool(t, para, page+"'s inventory paragraph")
}

// inventoryLead is how the tutorial opens its enumeration. Written once
// because the guard and its own counterfactual both need it.
const inventoryLead = "The tool inventory:"

// paragraphWith returns the blank-line-delimited paragraph containing
// marker. A prose inventory is a sentence, not a line — the tutorial's
// wraps over four — so a line-wise read of it would see one comma-cut
// fragment and call the rest undocumented.
func paragraphWith(body, marker string) (string, bool) {
	for _, para := range strings.Split(body, "\n\n") {
		if strings.Contains(para, marker) {
			return para, true
		}
	}
	return "", false
}

// declaresTool reports whether body carries an ENTRY for name — a list
// item or a bold lead-in that introduces the tool — rather than a
// sentence that happens to mention it. The lead is everything before the
// entry's em dash, which is the shape every inventory in this repo's
// records uses:
//
//   - `screen_size` — the visible surface in cells
//   - `send_keys` / `send_mouse` — inject input.Events
//     **`validate_markup(source)`** — swap_markup's parse-and-bind
//
// The name has to be backtick-opened and may not run into another
// identifier character, which is what keeps `register_properties` from
// matching the `unregister_properties` beside it and lets
// `validate_markup(source)` match all the same.
//
// THE LINE ALSO HAS TO LOOK LIKE AN ENTRY, and it did not until the
// counterfactual below said so: an em dash is ordinary punctuation in
// this repo's prose, so "Prose about `gamma` — which is a mention"
// scored as an entry for gamma. An entry OPENS with its subject — a
// list marker, a bold lead-in, or the backticked name itself — and a
// sentence about something else does not.
func declaresTool(body, name string) bool {
	for _, line := range strings.Split(body, "\n") {
		l := strings.TrimSpace(line)
		if !strings.HasPrefix(l, "- ") && !strings.HasPrefix(l, "* ") &&
			!strings.HasPrefix(l, "**`") && !strings.HasPrefix(l, "`") {
			continue
		}
		head, _, ok := strings.Cut(l, " — ")
		if !ok {
			continue
		}
		// EVERY OCCURRENCE IN THE LEAD, not the first. This took
		// strings.Index once and moved to the next LINE when the byte
		// after it was an identifier byte — so an entry leading with a
		// longer name that contains this one ("- `screen_size_v2` /
		// `screen_size` — …") reported screen_size as undocumented,
		// while the two-names-per-entry shape the record actually uses
		// ("- `send_keys` / `send_mouse` — …") is the very reason this
		// function reads the whole lead. One false match ended the line.
		// Raised in review of #504.
		for rest := head; ; {
			i := strings.Index(rest, "`"+name)
			if i < 0 {
				break
			}
			rest = rest[i+len(name)+1:]
			if rest == "" || !isIdentByte(rest[0]) {
				return true
			}
		}
	}
	return false
}

// isIdentByte is what separates `screen_size` from a longer name that
// merely starts with it. UPPERCASE INCLUDED: it was a-z, 0-9 and
// underscore, so `screen_sizeV2` would have read as an entry for
// screen_size. Tool names are lower_snake today, which is what made the
// gap invisible — and a rule that is correct only for the names that
// happen to exist is the kind that stops being correct silently. Raised
// in review of #504.
func isIdentByte(b byte) bool {
	return b == '_' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') ||
		(b >= '0' && b <= '9')
}

// TestTheInventoryReadsAreNarrowerThanThePage is the counterfactual for
// the two narrowings above, and it is the assertion neither guard can
// make about itself: a slice that quietly returned the whole document,
// or an entry test that matched any mention, would leave both of them
// exactly as vacuous as they were — and green.
func TestTheInventoryReadsAreNarrowerThanThePage(t *testing.T) {
	const page = "# Doc\n\n" + inventoryLead + " `alpha`, `beta`.\n\n" +
		"`gamma` is explained down here, outside the list.\n"

	para, ok := paragraphWith(page, inventoryLead)
	if !ok {
		t.Fatal("paragraphWith did not find the inventory it was pointed at")
	}
	if !namesTool(para, "beta") {
		t.Error("the inventory paragraph lost a name that is in it")
	}
	if namesTool(para, "gamma") {
		t.Error("the inventory paragraph reaches a name from the NEXT paragraph, " +
			"so deleting a tool from the list would still read as documented")
	}

	const record = "- `alpha` — the first one\n" +
		"- `send_keys` / `send_mouse` — two on one entry\n" +
		"**`delta(source)`** — a bold lead-in with an argument\n" +
		"Prose about `gamma` — which is a mention and not an entry.\n" +
		"- `unregister_epsilon` — the inverse\n"
	for _, name := range []string{"alpha", "send_mouse", "delta"} {
		if !declaresTool(record, name) {
			t.Errorf("declaresTool missed the entry for %s, so a record that "+
				"documents a tool would read as omitting it", name)
		}
	}
	if declaresTool(record, "gamma") {
		t.Error("a sentence mentioning `gamma` counts as an entry, which is the " +
			"page-wide vacuous pass this shape exists to close")
	}
	if declaresTool(record, "epsilon") {
		t.Error("`unregister_epsilon` counts as an entry for epsilon — the same " +
			"substring pass namesTool's backticks close one surface over")
	}

	// A LONGER NAME FIRST IN THE SAME LEAD. This read as undocumented
	// while declaresTool took only the first backtick match on a line and
	// moved on, which is the shape the two-names-per-entry rows in the
	// real record would hit the moment a `screen_size_v2` joined them.
	const shadowed = "- `alpha_v2` / `alpha` — the longer name leads\n"
	if !declaresTool(shadowed, "alpha") {
		t.Error("an entry whose lead opens with a LONGER name containing this " +
			"one reads as no entry at all, so a tool listed second on its own " +
			"row would be reported as undocumented")
	}
	if declaresTool(shadowed, "alpha_v3") {
		t.Error("declaresTool matched a name the entry does not carry")
	}
	// The uppercase half of the same boundary.
	if declaresTool("- `alphaV2` — a camel sibling\n", "alpha") {
		t.Error("`alphaV2` counts as an entry for alpha: the identifier test " +
			"stops at lowercase, so a camel-cased sibling reads as the name it " +
			"merely starts with")
	}

	// AND THE TABLE READ HOLDS THE SAME NAME SHAPE. toolColumn is the
	// fourth reader in this file and the only one with its own character
	// class, which sat at [a-z_] while isIdentByte above was widened —
	// so a row for screen_size2 was invisible to the guard and reported
	// as a missing row. Raised in review of #504.
	rows := toolColumn("| `alpha` | one |\n| `screen_size2` | two |\n| `sendKeys` | three |\n")
	for _, name := range []string{"alpha", "screen_size2", "sendKeys"} {
		if !rows[name] {
			t.Errorf("toolColumn dropped the row for %s, so a table that names "+
				"every tool would be reported as missing one: %v", name, rows)
		}
	}

	// AND THE TABLE READ IS NARROWED TO ONE TABLE. The contract spec
	// carries two, and reading both folded the kinds column into the
	// coverage map — so a tool sharing a name with a kind would read as
	// documented with no row of its own.
	const twoTables = "# Doc\n\n| kind | proto |\n|---|---|\n| `image` | `bytes` |\n\n" +
		toolTableHeader + "\n|---|---|---|---|\n| `alpha` | — | none | |\n"
	narrow, ok := tableRows(twoTables)
	if !ok {
		t.Fatal("tableRows did not find the table it is pointed at")
	}
	if !narrow["alpha"] {
		t.Errorf("the narrowed read lost a row that is in the table: %v", narrow)
	}
	if narrow["image"] {
		t.Error("a name from ANOTHER table on the page counts as a documented " +
			"tool, so deleting a tool's own row would still read as covered")
	}
}

// TestTheMCPSpecsToolInventoryIsComplete is the fourth surface, and the
// one this change edited by hand while closing the other three.
//
// docs/specs/2026-08-10-mcp-server.md is the decision record — its
// "Tools (v1)" section is the same shape as the tutorial's, backticked
// names in prose read only by humans. Adding screen_size to it and not
// guarding it means the next tool lands with three surfaces reddening
// and the record quietly wrong, which is the exact outcome the tutorial
// guard exists to prevent.
//
// ADDITIVE, NOT A FIX: this passed as written before it was committed.
// Saying so matters because a guard added beside a repair reads as the
// repair's pin, and this one pins nothing that was broken. Raised in
// review of #504.
//
// Skips when the file is absent, for the module-boundary reason
// TestTheTutorialsToolInventoryIsComplete gives.
func TestTheMCPSpecsToolInventoryIsComplete(t *testing.T) {
	const page = "../docs/specs/2026-08-10-mcp-server.md"
	body := pageOrSkip(t, page)
	// AN ENTRY, not a mention — and a SECTION SLICE IS THE WRONG
	// NARROWING HERE, which is a measurement and not a preference.
	// "## Tools (v1)" is the ORIGINAL set and this record extends
	// itself in dated sections: list_styles, validate_markup,
	// register_properties and unregister_properties are each
	// introduced in one of them. Slicing to the v1 section reported
	// four tools as undocumented in a record that documents all four,
	// and the repair would have been to backdate them into a decision
	// that did not include them. So the page stays the subject and the
	// SHAPE does the narrowing: a tool has to be the subject of an
	// entry, which a sentence in "Extended 2026-08-10" mentioning it
	// is not. Raised in review of #504.
	s := &Server{}
	tools := s.v1Tools()
	if len(tools) == 0 {
		t.Fatal("v1Tools is empty, so this guard would pass vacuously")
	}
	for _, tl := range tools {
		if !declaresTool(string(body), tl.Name) {
			t.Errorf("%s mentions %s nowhere as an ENTRY of its own — no list item "+
				"and no bold lead-in introducing it. The record is where somebody "+
				"asks what the tool surface IS; a tool that only appears inside a "+
				"sentence about another one is not in the inventory", page, tl.Name)
		}
	}
}

// TestTheGRPCContractTableNamesEveryTool is the third surface, and the
// one that states its own completeness out loud.
//
// docs/specs/2026-08-10-grpc-contract.md's "#112 table" opens "Every v1
// MCP tool, argument-for-argument" and closes with the rule that any new
// tool must name the RPC it fronts. screen_size had no row — so the
// reader most in need of it, somebody implementing the rest of #112
// looking for which tools still lack an RPC, would find the ONE tool
// that lacks one missing from the list of tools. A doc that asserts
// completeness more loudly than the tutorial did went stale in exactly
// the way this change exists to stop. Raised in review of #504.
//
// It reads the tool column of THAT table, not of the whole page: this
// file names tools in prose elsewhere, so a page-wide Contains would be
// satisfied by a mention outside the table — and the page carries a
// second table, the TypedValue kinds one, whose own first column would
// otherwise be folded into the same coverage map. Skips when the file is
// absent, for the module-boundary reason
// TestTheTutorialsToolInventoryIsComplete gives.
func TestTheGRPCContractTableNamesEveryTool(t *testing.T) {
	const page = "../docs/specs/2026-08-10-grpc-contract.md"
	body := pageOrSkip(t, page)
	rows, ok := tableRows(string(body))
	if !ok {
		t.Fatalf("%s no longer carries a %q header, so either the #112 table was "+
			"restructured and this guard has to follow it, or the table is gone",
			page, toolTableHeader)
	}
	if len(rows) == 0 {
		t.Fatalf("found no tool rows in %s's #112 table, so this guard would pass "+
			"vacuously — the row shape changed and toolColumn no longer recognizes "+
			"it", page)
	}
	s := &Server{}
	for _, tl := range s.v1Tools() {
		if !rows[tl.Name] {
			t.Errorf("%s claims \"Every v1 MCP tool\" and has no row for %s. A tool "+
				"with no RPC behind it yet still needs the row — that is what somebody "+
				"implementing #112 reads the table to find", page, tl.Name)
		}
	}
}

// tableRows narrows body to the #112 table and reads its tool column.
// The narrowing and the read are one function because they are one
// claim — a tool is documented when THAT table carries a row for it —
// and because a call site that narrowed for itself could stop and no
// counterfactual would see it.
func tableRows(body string) (map[string]bool, bool) {
	table, ok := paragraphWith(body, toolTableHeader)
	if !ok {
		return nil, false
	}
	return toolColumn(table), true
}

// toolTableHeader is the #112 table's header row. A markdown table is a
// blank-line-delimited block, so paragraphWith slices exactly this one.
const toolTableHeader = "| MCP tool | args | RPC | notes |"

// toolColumn returns the backticked name in the first cell of every
// markdown table row in body. Hand it ONE table: it cannot tell which
// table a row came from, and every name it finds counts as covered, so a
// whole-page read of the contract spec folds the TypedValue kinds table's
// `string`, `int`, `bool`, `image` … into the coverage map, where a tool
// that ever shares one of those names reads as documented with no row of
// its own. Adding names to a coverage map is how it passes over an
// absence, not why it cannot. Raised in review of #504.
func toolColumn(body string) map[string]bool {
	out := map[string]bool{}
	for _, line := range strings.Split(body, "\n") {
		m := tableToolRe.FindStringSubmatch(line)
		if m != nil {
			out[m[1]] = true
		}
	}
	return out
}

// THE SAME NAME SHAPE isIdentByte USES, which is the point rather than
// a coincidence: this PR widened that one to uppercase and digits on the
// ground that "a rule that is correct only for the names that happen to
// exist is the kind that stops being correct silently", and left this
// copy of the same rule at [a-z_]. A tool named screen_size2 would
// capture screen_size, hit a digit where the closing backtick was
// demanded, and be dropped from the column — so the guard would report a
// missing row for a table that has one. It fails red rather than green,
// which makes it a false alarm rather than a hole, and it is still the
// class the PR just closed one function over. Raised in review of #504.
var tableToolRe = regexp.MustCompile("^\\|\\s*`([A-Za-z0-9_]+)`\\s*\\|")

// TestTheServerInstructionsNameEveryTool is the same guard one surface
// closer to the client.
//
// `instructions` is a hand-written prose enumeration shipped to every MCP
// client, and an agent reads it BEFORE the tutorial. It omitted
// screen_size for as long as nothing checked it — the identical failure
// TestTheTutorialsToolInventoryIsComplete exists to end, in the string
// that reaches clients first. Unlike the tutorial this lives in the
// module, so it never skips.
func TestTheServerInstructionsNameEveryTool(t *testing.T) {
	assertNamesEveryTool(t, instructions, "the server instructions string")
}

// assertNamesEveryTool derives the expectation from v1Tools, which is
// what keeps both callers from becoming lists of their own.
//
// IT ASKS FOR THE NAME IN BACKTICKS, and that is the finding rather than
// a style preference. `strings.Contains` was the first spelling and it
// passed vacuously for two of the fifteen names: `register_properties`
// is a substring of the `unregister_properties` that sits beside it in
// both surfaces, and `focus` is an ordinary English word that appears in
// prose about focus whether or not a tool has that name. So a guard
// written to end prose inventories going stale was itself checking
// thirteen of fifteen — the same shape of defect, one level up.
//
// A non-identifier BOUNDARY closes the first half and not the second:
// "…, send_mouse and focus act on it" delimits the word exactly as a
// tool name would be delimited. The backtick is the mark that means "this
// is a name and not a word", the tutorial already used it on every one of
// them, and mcp/transport.go now does too. TestTheToolNameMatchIsDelimited
// pins both halves. Raised in review of #504.
func assertNamesEveryTool(t *testing.T, body, what string) {
	t.Helper()
	s := &Server{}
	tools := s.v1Tools()
	if len(tools) == 0 {
		t.Fatal("v1Tools is empty, so this guard would pass vacuously")
	}
	for _, tl := range tools {
		if !namesTool(body, tl.Name) {
			t.Errorf("%s never names %s in backticks. The inventory is what a reader "+
				"uses to find a tool, and a tool missing from it does not exist as far "+
				"as they are concerned. Backticks because an unmarked name cannot be "+
				"told from the prose around it — see assertNamesEveryTool", what, tl.Name)
		}
	}
}

func namesTool(body, name string) bool {
	return strings.Contains(body, "`"+name+"`")
}

// TestTheToolNameMatchIsDelimited measures the two vacuous passes rather
// than asserting they are gone, because both are still a `strings.Contains`
// away.
func TestTheToolNameMatchIsDelimited(t *testing.T) {
	const nested = "`unregister_properties` removes names again"
	if namesTool(nested, "register_properties") {
		t.Error("a body naming only unregister_properties reports register_properties " +
			"as documented, which is the substring pass this match exists to close")
	}
	if !namesTool("`register_properties` grows the bindable state", "register_properties") {
		t.Error("the marked form is not recognized, so the guard asks for something " +
			"nobody can write")
	}
	if namesTool("keys go to whatever has focus at the time", "focus") {
		t.Error("ordinary prose about focus satisfies the focus tool's entry, which is " +
			"the word-not-name pass this match exists to close")
	}
	if !namesTool("send_mouse and `focus` act on it", "focus") {
		t.Error("the marked form of a name that is also an English word is not " +
			"recognized")
	}
}

// rootBounds reads the root component's arranged bounds out of a
// tree_snapshot — the inference #204 exists to replace, kept here only so
// a test can assert screen_size is NOT it.
func rootBounds(t *testing.T, snap map[string]any) gooey.Rect {
	t.Helper()
	tree, ok := snap["tree"].(map[string]any)
	if !ok {
		t.Fatalf("tree_snapshot carries no tree: %v", snap)
	}
	b, ok := tree["bounds"].(map[string]any)
	if !ok {
		t.Fatalf("tree_snapshot root carries no bounds: %v", tree)
	}
	return gooey.Rect{
		X: int(b["x"].(float64)), Y: int(b["y"].(float64)),
		W: int(b["w"].(float64)), H: int(b["h"].(float64)),
	}
}

// screenOf reads the test app's terminal size THROUGH the UI loop.
//
// testApp documents cols/rows as "written and read only by run(), or by a
// closure run() drained", and a test reading them directly commits the
// same violation the tools are forbidden — it just happens not to race
// today because nothing resizes. Asserting against a value fetched the
// illegal way would make this file the one place the contract is not
// kept. Raised in review of #504.
func screenOf(t *testing.T, app *testApp) (cols, rows int) {
	t.Helper()
	done := make(chan struct{})
	app.Post(func() {
		cols, rows = app.cols, app.rows
		close(done)
	})
	<-done
	return cols, rows
}

// TestAnIslandThatIsGoneIsDeniedByName covers islandRect's first error
// path, which had no test anywhere in the repo — the extraction that
// created it moved three copies of the check into one place and left the
// one place uncovered, which is the usual way a refactor loses an
// assertion.
//
// It is asserted on screen_size AND screen_text because islandRect is
// what both now call: a regression that broke the resolution would
// otherwise show up on whichever tool nobody tested.
func TestAnIslandThatIsGoneIsDeniedByName(t *testing.T) {
	// "Ghost" is a name the tree does not contain. The grant is
	// well-formed; the element simply is not there, which is the state a
	// swap or a patch can produce at runtime.
	guest, _ := islandGuest(t, islandOffOriginMarkup, "Ghost")

	// The message names the island, because a client that cannot see the
	// tree has no other way to tell "you may not" from "it is gone".
	guest.fails("screen_size", nil, `island "Ghost", which names no element`)
	guest.fails("screen_text", nil, `island "Ghost", which names no element`)
}

// TestACollapsedIslandIsZeroSizedAndNotAnError is the OTHER arm of the
// same resolution, and the one three surfaces describe and none pinned.
//
// islandRect deliberately does not test W/H: a collapsed or not-yet-
// arranged island resolves SUCCESSFULLY to a zero-size rect, because it
// is not gone and islandGone would be a lie about it. So screen_size
// answers 0x0 and screen_text answers "" — both without an error.
// islandRect's doc comment says so, and both screenSizeSchema's cols and
// its rows tell clients "0 is a real answer, not an error". Nothing
// asserted it, which means the next reader to see cols:0 in a trace is
// free to "fix" it into a denial and every one of those three sentences
// goes quietly false.
//
// THE LIVE ARM IS THE NON-VACUITY. Zero is what an app that never
// composed reports too, and a fixture that never arranged would satisfy
// every assertion below against a tool that answered 0x0 unconditionally.
// The same markup with the island visible reports a real size, so the
// zero is attributable to the collapse and to nothing else.
func TestACollapsedIslandIsZeroSizedAndNotAnError(t *testing.T) {
	const capW, capH = 7, 15
	liveGuest, _ := islandGuest(t, islandOffOriginMarkup, "Mine")
	live := liveGuest.json("screen_size", nil)
	if int(live["cols"].(float64)) == 0 || int(live["rows"].(float64)) == 0 {
		t.Fatalf("the uncollapsed fixture already reports a zero extent (%v), so this "+
			"test cannot tell a collapsed island from one that never arranged", live)
	}

	guest, app := islandGuest(t, islandCollapsedMarkup, "Mine")
	done := make(chan struct{})
	app.Post(func() {
		app.comp.SetCaps(term.Caps{CellW: capW, CellH: capH})
		close(done)
	})
	<-done

	// Not fails(): the call must SUCCEED. An error here is the regression
	// this exists to catch — islandRect learning to refuse a degenerate
	// rect, which would deny a guest whose island is merely closed.
	sz := guest.json("screen_size", nil)
	if cols, rows := int(sz["cols"].(float64)), int(sz["rows"].(float64)); cols != 0 || rows != 0 {
		t.Errorf("a collapsed island reports %dx%d; islandRect and screenSizeSchema both "+
			"say a collapsed island is 0x0", cols, rows)
	}
	// The cell metrics are the terminal's and have nothing to do with the
	// island, so they must NOT have been zeroed along with it — which is
	// what a blanket "return an empty ScreenSize" would do.
	//
	// THE CAPS WERE SET BEFORE THAT CALL, and the assertion reads their
	// VALUES off it. Asking whether the keys are present could never
	// fail: screenSize writes all six unconditionally, so the map has
	// them whatever ScreenSize returned, and the fixture's caps were zero
	// anyway — the arm agreed with a blanket zeroing and with the correct
	// answer alike.
	//
	// ONE CALL, not two. This read the values off a SECOND
	// guest.json("screen_size", nil) taken immediately after the first,
	// with the caps already set above both and nothing changed in
	// between — an extra round trip and one more thing for a later
	// reader to reconcile. Raised in review of #504, both halves.
	if int(sz["cellWidth"].(float64)) != capW || int(sz["cellHeight"].(float64)) != capH {
		t.Errorf("a collapsed island reports cell metrics %vx%v, want the probed "+
			"%dx%d: the terminal's cell size is not the island's, and zeroing it "+
			"with the extent is what a blanket empty ScreenSize would do",
			sz["cellWidth"], sz["cellHeight"], capW, capH)
	}

	if txt := guest.ok("screen_text", nil); txt != "" {
		t.Errorf("a collapsed island renders %q; it crops to nothing", txt)
	}
	if txt := guest.ok("screen_text", map[string]any{"styled": true}); txt != "" {
		t.Errorf("a collapsed island renders %q styled; both forms crop to nothing", txt)
	}
}

// TestATreeSnapshotBoundIsAlreadyAbsolute is the other half of the
// origin's contract, and the half a client can get wrong in the same
// direction the tool exists to fix.
//
// A scoped session has TWO coordinate sources and they do not agree.
// screen_text is homed at (0,0) deliberately — a guest's screen dump is
// not a set of absolute cursor moves that betray where on the host's
// page its island sits — so a position read off it is what x/y converts.
// tree_snapshot emits Bounds() from the live tree, which are already
// absolute even when the snapshot is rooted at the island. An agent that
// obeys screen_size unconditionally adds y0 to a bound that already
// carries it and clicks y0 rows low: on a real component, silently, or
// outside the island, refused by a message saying the point is outside
// an island whose own snapshot it came from.
//
// THE CONVERTED ARM IS WHAT MAKES THIS DISCRIMINATING. Accepting the raw
// bound would pass just as well against a session that permitted the
// whole screen; the fixture's island starts below y=0, so double
// conversion walks off the bottom and must be refused. Raised in review
// of #504.
func TestATreeSnapshotBoundIsAlreadyAbsolute(t *testing.T) {
	guest, _ := islandGuest(t, islandOffOriginMarkup, "Mine")

	sz := guest.json("screen_size", nil)
	x0, y0 := int(sz["x"].(float64)), int(sz["y"].(float64))
	if y0 == 0 {
		t.Fatalf("the island reports origin y=0, so this fixture cannot tell an "+
			"already-absolute bound from a converted one; sz=%v", sz)
	}

	node := findName(guest.json("tree_snapshot", nil)["tree"].(map[string]any), "Mine")
	if node == nil {
		t.Fatal("the guest's snapshot does not contain its own island")
	}
	b, ok := node["bounds"].(map[string]any)
	if !ok {
		t.Fatalf("the island node carries no bounds: %v", node)
	}
	bx, by := int(b["x"].(float64)), int(b["y"].(float64))
	bh := int(b["h"].(float64))
	if by != y0 {
		t.Fatalf("the snapshot reports the island at y=%d and screen_size reports "+
			"origin y=%d; if these ever diverge the advice in screenSizeSchema is "+
			"wrong in a way no client can detect", by, y0)
	}

	// THE LAST ROW OF THE ISLAND, not the first. A double conversion of
	// the TOP row lands y0 rows down and is still inside a 3-row island —
	// silently wrong, and a fixture that cannot tell the two apart. The
	// bottom row is the one that leaves the island when it is converted
	// again, which is the whole point: a client obeying the rule
	// unconditionally loses its own last row exactly the way a client
	// that never heard of the origin loses its first.
	last := by + bh - 1

	// Unconverted: accepted, because the bound is already in send_mouse's
	// frame.
	guest.ok("send_mouse", map[string]any{"kind": "click", "x": bx, "y": last})

	// Converted: refused, because it has been offset twice.
	guest.fails("send_mouse", map[string]any{
		"kind": "click", "x": bx + x0, "y": last + y0,
	}, "outside this session's island")
}

// TestTheAgentWorkflowsToolInventoriesAreComplete is the fifth and sixth
// surfaces, and they are the ones whose readers drive the app.
//
// .claude/workflows/gooey-new-component.js and gooey-new-demo.js each
// carry a "The tools:" line inside the prompt they hand an agent that
// then prototypes a component over MCP — the audience with the most
// direct use for the screen's size, and the one least able to discover a
// tool the list omits, because the list IS its documentation. Both
// omitted screen_size, and both had omitted unregister_properties since
// before this change.
//
// IT READS THE COMMITTED BLOB, not the working tree, and that is the
// whole reason this still lives in the mcp module. These two files are
// outside it and are rewritten by tooling: the review of #504 ran in a
// checkout where both had been restored to origin/main's content, and
// this test failed with thirty errors about a repo that was fine. The
// fs.ErrNotExist skip could not help — the file was present, just not
// ours. What the guard is actually about is whether the REPOSITORY's
// inventory is complete, so HEAD's copy is the honest subject and a
// dirty worktree is none of its business.
//
// Moving it to the root module, where .claude/ is in-tree, was the other
// option and is worse: v1Tools() lives here, and a root-module copy would
// have to enumerate the tools by hand — which is the defect all six of
// these surfaces exist to prevent.
//
// THE BACKTICKS ARE ESCAPED IN THE SOURCE, because the inventory sits
// inside a JavaScript template literal: an unescaped ` would end the
// string. \` is what the file holds and a backtick is what the agent
// reads, so this unescapes before asking assertNamesEveryTool — the
// alternative, a second matching rule, would let these two surfaces drift
// apart from the other four.
func TestTheAgentWorkflowsToolInventoriesAreComplete(t *testing.T) {
	pages := []string{
		".claude/workflows/gooey-new-component.js",
		".claude/workflows/gooey-new-demo.js",
	}
	// THE CHECKOUT QUESTION IS ASKED ONCE, AND OUT HERE. It is the only
	// reason a blob may be missing without anybody having done anything
	// wrong — the zip a proxy serves contains mcp/ and nothing above it
	// — and asking it per file made the two answers indistinguishable:
	// a bare continue covered "this module is being consumed
	// standalone" and "somebody renamed a workflow and nothing follows
	// it any more" with the same green. Raised in review of #504, which
	// is the second time this loop has lost a page quietly.
	if !inRepoCheckout(t) {
		t.Skipf("HEAD is not readable from here, so this guard only runs inside "+
			"the repo checkout (%v)", pages)
	}
	for _, page := range pages {
		src, ok := committedBlob(t, page)
		if !ok {
			t.Errorf("%s is not in HEAD, and git can read HEAD here — so it was "+
				"renamed, moved or deleted. The inventory it carries is what an "+
				"agent driving this server reads, and a surface the guard cannot "+
				"find is a surface nobody is checking: follow the file, or delete "+
				"the entry deliberately", page)
			continue
		}
		line, found := toolsLine(src)
		if !found {
			t.Errorf("%s no longer carries a \"The tools:\" line, so either the "+
				"prompt was restructured and this guard has to follow it, or the "+
				"inventory is gone", page)
			continue
		}
		// THE LINE, not the page. assertNamesEveryTool used to be handed
		// the whole file, which is the page-wide vacuous pass
		// TestTheGRPCContractTableNamesEveryTool declines by name two
		// tests up: these prompts mention tool names elsewhere, so a
		// name could be "documented" by a sentence outside the
		// inventory. Raised in review of #504.
		// NAMED AS WHAT WAS ACTUALLY READ. The label said
		// ".claude/workflows/…", which is the path in the worktree, and
		// this test deliberately does not read that file — so a
		// developer with the fix already applied was told their own
		// open editor was missing a tool. Raised in review of #504.
		//
		// AND WHEN HEAD IS BEHIND THE WORKTREE, SAY SO. Reading the
		// committed blob is right — see the head of this comment — but
		// it has a cost the failure message has to carry: a developer
		// who has ALREADY written the missing tool into the file and
		// not committed it gets a red suite describing a repository
		// they have fixed, with no way to tell that from a real gap.
		// Asking the worktree only in the failure path keeps HEAD the
		// subject and turns "you are missing a tool" into "commit what
		// you have". Raised in review of #504.
		if missing := toolsMissingFrom(t, line); len(missing) > 0 {
			what := "HEAD:" + page + "'s \"The tools:\" line"
			if wt, err := os.ReadFile(filepath.Join("..", page)); err == nil {
				// THE SAME UNESCAPE committedBlob does, and forgetting
				// it made this branch unreachable: the inventory lives
				// inside a JavaScript template literal, so the file
				// holds \` where the agent reads a backtick, and
				// namesTool matches on backticks. Measured — without
				// this the worktree copy reads as missing every tool and
				// the fallback never fires, which is the same kind of
				// silent pass the guard above declines by name.
				wtSrc := strings.ReplaceAll(string(wt), "\\`", "`")
				if wtLine, ok := toolsLine(wtSrc); ok &&
					len(toolsMissingFrom(t, wtLine)) == 0 {
					t.Errorf("%s never names %s in backticks — but your WORKING TREE "+
						"copy of %s names them all. Nothing is wrong with the file "+
						"in front of you; this guard reads the committed blob, "+
						"because a checkout where tooling has restored these pages "+
						"to origin/main's content is not evidence about the "+
						"repository. Commit the change", what,
						strings.Join(missing, ", "), page)
					continue
				}
			}
			assertNamesEveryTool(t, line, what)
		}
	}
}

// toolsMissingFrom is assertNamesEveryTool's question without the
// assertion: which registered tools this body does not name. The two
// share namesTool, so a change to what "names" means cannot make them
// disagree.
func toolsMissingFrom(t *testing.T, body string) []string {
	t.Helper()
	s := &Server{}
	tools := s.v1Tools()
	if len(tools) == 0 {
		t.Fatal("v1Tools is empty, so this guard would pass vacuously")
	}
	var missing []string
	for _, tl := range tools {
		if !namesTool(body, tl.Name) {
			missing = append(missing, tl.Name)
		}
	}
	return missing
}

// inRepoCheckout reports whether git can read HEAD from the parent
// directory. It separates "this module was consumed standalone" — the
// one blameless reason a committed blob is unreadable — from a path
// that is simply no longer there.
// THE ROOT, NOT MERELY A REPOSITORY. `git -C .. rev-parse HEAD` succeeds
// for ANY enclosing repository, so a gooey checkout nested inside a
// larger one answered yes — and committedBlob's `show HEAD:<path>`
// resolves paths from that outer repo's root, where `.claude/workflows/…`
// does not exist. The guard then reported the workflows as renamed or
// deleted in a tree where they are present and correct. Comparing the
// toplevel against `..` itself is what asks the question the caller
// means. Raised in review of #504.
func inRepoCheckout(t *testing.T) bool {
	t.Helper()
	out, err := exec.Command("git", "-C", "..", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return false
	}
	top, err := filepath.EvalSymlinks(strings.TrimSpace(string(out)))
	if err != nil {
		return false
	}
	// Abs BEFORE EvalSymlinks. EvalSymlinks does not absolutise: handed
	// ".." it returns ".." unchanged, which never equals a toplevel and
	// silently skipped this guard everywhere — the first version of this
	// fix did exactly that, and the test reported itself skipped in a
	// checkout where it should have run.
	abs, err := filepath.Abs("..")
	if err != nil {
		return false
	}
	parent, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return false
	}
	return top == parent
}

// committedBlob reads one repo-root-relative path out of HEAD. It returns
// false — rather than failing — when git cannot answer, which covers the
// module being consumed standalone (the zip a proxy serves contains mcp/
// and nothing above it) as well as a path not yet committed.
func committedBlob(t *testing.T, path string) (string, bool) {
	t.Helper()
	out, err := exec.Command("git", "-C", "..", "show", "HEAD:"+path).Output()
	if err != nil {
		return "", false
	}
	return strings.ReplaceAll(string(out), "\\`", "`"), true
}

// toolsLine is the inventory itself: the one line beginning the "The
// tools:" enumeration. Everything else in these prompts is prose that
// happens to mention tools.
func toolsLine(src string) (string, bool) {
	for _, line := range strings.Split(src, "\n") {
		if strings.Contains(line, "The tools:") {
			return line, true
		}
	}
	return "", false
}

// TestTheScreenSizeSchemaAndItsResultNameTheSameKeys is the derived
// guard the six wire names did not have.
//
// screenSizeSchema declares them twice — once as properties, once in the
// required list — and Server.screenSize writes them a third time, as a
// map literal in tools.go. Three hand-written copies of one vocabulary,
// and nothing compared them: renaming "cellWidth" in the schema alone
// leaves a published contract promising a key no result carries, and a
// client that branches on its absence reads "the host never probed" for
// every host. The schema is data and the result is data, so the
// comparison needs no third list here. Raised in review of #504.
func TestTheScreenSizeSchemaAndItsResultNameTheSameKeys(t *testing.T) {
	_, _, _, c := setup(t)
	got := c.json("screen_size", nil)

	schema := screenSizeSchema()
	props, ok := schema["properties"].(map[string]any)
	if !ok || len(props) == 0 {
		t.Fatalf("screenSizeSchema declares no properties (%v), so this test "+
			"would compare the result against an empty set", schema)
	}
	for name := range props {
		if _, ok := got[name]; !ok {
			t.Errorf("the schema publishes %q and screen_size's result does not "+
				"carry it: a client reading the contract asks for a key that is "+
				"never there", name)
		}
	}
	for name := range got {
		if _, ok := props[name]; !ok {
			t.Errorf("screen_size returns %q and the schema does not publish it, "+
				"so a client generated from the contract cannot see it", name)
		}
	}

	// REQUIRED IS THE THIRD COPY, and it is the one a client's decoder
	// actually enforces. A key published as a property but left out of
	// required is optional to every generated client, which is exactly
	// wrong for six fields that are always present.
	req, ok := schema["required"].([]string)
	if !ok {
		if anys, isAny := schema["required"].([]any); isAny {
			for _, v := range anys {
				req = append(req, v.(string))
			}
		} else {
			t.Fatalf("screenSizeSchema's required is %T, not a list of names", schema["required"])
		}
	}
	if len(req) != len(props) {
		t.Errorf("the schema publishes %d properties and requires %d of them; "+
			"every field of this result is always present, so a key missing "+
			"from required is optional to every generated client for no reason",
			len(props), len(req))
	}
	for _, name := range req {
		if _, ok := props[name]; !ok {
			t.Errorf("required names %q, which is not a published property", name)
		}
	}
}

// pageOrSkip reads a page that lives ABOVE this module, skipping when
// the module is being consumed standalone and failing when the page is
// simply not where the guard says it is.
//
// The two are different answers and os.ReadFile gives them the same
// error. THE PAGE'S OWN DIRECTORY is what separates them, and it is
// checked first: consumed standalone the module is `mcp/` and nothing
// above it, so the directory is absent and there is nothing to compare
// against; with the directory present, a missing page is a rename the
// guard has to follow.
//
// NOT inRepoCheckout, which asks a git question these three guards do
// not have. It returns false when git is absent, when git refuses
// ("detected dubious ownership"), or when `..` is not a repo root — so a
// `git archive`, a Download ZIP, or a Docker build whose context omits
// `.git` carries all three pages, readable, and skipped all three guards
// green. That is the shape of the fs.ErrNotExist skip this branch
// already removed for the same tests: a guard whose premise is that a
// prose inventory goes stale silently cannot disable itself. The split
// is between reading a file that is right there and reading a COMMIT —
// TestTheAgentWorkflowsToolInventoriesAreComplete does the second and
// keeps inRepoCheckout. Raised in review of #504.
func pageOrSkip(t *testing.T, page string) []byte {
	t.Helper()
	dir := filepath.Dir(page)
	if _, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
		t.Skipf("%s is outside this module and %s is not present, so the module "+
			"is being consumed standalone and this guard cannot run here",
			page, dir)
	} else if err != nil {
		t.Fatalf("stat %s: %v", dir, err)
	}
	body, err := os.ReadFile(page)
	if errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("%s is missing from the repo checkout — renamed, moved or "+
			"deleted. This guard is the only thing keeping that page's tool "+
			"inventory in step with the server, so it follows the page rather "+
			"than skipping: point it at the new path, or delete it with the page",
			page)
	}
	if err != nil {
		t.Fatalf("reading %s: %v", page, err)
	}
	return body
}

package mcp

import (
	"errors"
	"io/fs"
	"os"
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
// The probe that fills these is opt-in (gooey.WithCapabilityProbe), and
// App's backfill to term.DefaultCellW/H fires only for a pixel-plane app,
// so an ordinary cell-plane host reports 0 — and a client doing
// `pixels = cols * cellWidth` gets 0 while one doing `cols / cellWidth`
// divides by zero. The schema says 0 means "never probed"; this is what
// makes that a checked claim rather than a sentence.
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
	mine := prop.NewSource("m0")
	secret := prop.NewSource("hunter2")
	app := newTestApp(t, islandOffOriginMarkup, map[string]any{
		"Mine": map[string]any{"Body": mine},
		"Host": map[string]any{"Secret": secret},
	})
	gs, err := New(app, Options{
		Context: app.ctx,
		Timeout: 5 * time.Second,
		Grant:   control.Island("Mine", "Mine"),
	})
	if err != nil {
		t.Fatalf("New (guest): %v", err)
	}
	guest := newClient(t, gs)

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
// It SKIPS rather than fails when the page is absent, because `mcp` is
// its own module: the zip a proxy serves contains mcp/ and nothing above
// it, so `../docs` does not exist for anyone consuming the module
// standalone. A guard that cannot run there must say so rather than
// report the repo's docs as broken on someone else's machine.
func TestTheTutorialsToolInventoryIsComplete(t *testing.T) {
	const page = "../docs/learn/08-remote-control.md"
	body, err := os.ReadFile(page)
	if errors.Is(err, fs.ErrNotExist) {
		t.Skipf("%s is outside this module and absent, so this guard only runs "+
			"inside the repo checkout", page)
	}
	if err != nil {
		t.Fatalf("reading %s: %v", page, err)
	}
	assertNamesEveryTool(t, string(body), page)
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
// It reads the TOOL COLUMN rather than the whole page: this file names
// tools in prose elsewhere, and a page-wide Contains would be satisfied
// by a mention outside the table, which is the vacuous pass one surface
// over. Skips when the file is absent, for the module-boundary reason
// TestTheTutorialsToolInventoryIsComplete gives.
func TestTheGRPCContractTableNamesEveryTool(t *testing.T) {
	const page = "../docs/specs/2026-08-10-grpc-contract.md"
	body, err := os.ReadFile(page)
	if errors.Is(err, fs.ErrNotExist) {
		t.Skipf("%s is outside this module and absent, so this guard only runs "+
			"inside the repo checkout", page)
	}
	if err != nil {
		t.Fatalf("reading %s: %v", page, err)
	}
	rows := toolColumn(string(body))
	if len(rows) == 0 {
		t.Fatalf("found no tool rows in %s, so this guard would pass vacuously — "+
			"the table's shape changed and toolColumn no longer recognizes it", page)
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

// toolColumn returns the backticked name in the first cell of every
// markdown table row on the page. It is deliberately not anchored to one
// heading: a second table would only ADD names, and the assertion is one
// of coverage, so a looser read cannot produce a false pass.
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

var tableToolRe = regexp.MustCompile("^\\|\\s*`([a-z_]+)`\\s*\\|")

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
	mine := prop.NewSource("m0")
	app := newTestApp(t, islandOffOriginMarkup, map[string]any{
		"Mine": map[string]any{"Body": mine},
		"Host": map[string]any{"Secret": prop.NewSource("s")},
	})
	gs, err := New(app, Options{
		Context: app.ctx,
		Timeout: 5 * time.Second,
		// A name the tree does not contain. The grant is well-formed;
		// the element simply is not there, which is the state a swap or
		// a patch can produce at runtime.
		Grant: control.Island("Ghost", "Ghost"),
	})
	if err != nil {
		t.Fatalf("New (guest): %v", err)
	}
	guest := newClient(t, gs)

	// The message names the island, because a client that cannot see the
	// tree has no other way to tell "you may not" from "it is gone".
	guest.fails("screen_size", nil, `island "Ghost", which names no element`)
	guest.fails("screen_text", nil, `island "Ghost", which names no element`)
}

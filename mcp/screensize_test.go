package mcp

import (
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

// islandOffXOriginMarkup is the same page turned on its side, so the
// island's origin is off zero in X instead of in Y.
//
// DERIVED, not copied: one fixture with two stack elements would be two
// pages that happen to look alike, and a change to one of them would
// leave the other axis quietly testing something else. The VStack is the
// only difference between the two cases, which is the point.
var islandOffXOriginMarkup = strings.NewReplacer(
	"<VStack Gap=\"0\">", "<HStack Gap=\"0\">",
	"</VStack>", "</HStack>",
).Replace(islandOffOriginMarkup)

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
//
// BOTH AXES, and the second one is not symmetry for its own sake. The
// only off-origin fixture here was a VStack, so the island's x was zero
// on every run and `size.X` could have been hard-coded, dropped, or
// swapped with Y and nothing would have gone red — the same blind spot
// the VStack fixture was added to close in the other direction, left
// open one axis over. The arms differ in exactly one thing, the stack,
// and each names the coordinate its own fixture makes non-zero.
func TestAGuestIsToldWhereItsIslandIs(t *testing.T) {
	for _, tc := range []struct {
		name, axis, markup string
		origin             func(x0, y0 int) int
		outside            func(x0, y0 int) (int, int)
	}{
		{"stacked vertically", "y", islandOffOriginMarkup,
			func(_, y0 int) int { return y0 },
			func(x0, y0 int) (int, int) { return x0, y0 - 1 }},
		{"stacked horizontally", "x", islandOffXOriginMarkup,
			func(x0, _ int) int { return x0 },
			func(x0, y0 int) (int, int) { return x0 - 1, y0 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			guest, _ := islandGuest(t, tc.markup, "Mine")

			sz := guest.json("screen_size", nil)
			x0, y0 := int(sz["x"].(float64)), int(sz["y"].(float64))
			cols, rows := int(sz["cols"].(float64)), int(sz["rows"].(float64))

			if tc.origin(x0, y0) == 0 {
				t.Fatalf("the island reports origin %s=0, so this fixture cannot tell "+
					"an origin-aware answer from one that assumes (0,0); sz=%v",
					tc.axis, sz)
			}
			// Every corner of the island, converted through the reported
			// origin, must be a coordinate send_mouse accepts. Without
			// x/y a guest can only guess these, and the guess is wrong by
			// exactly the origin.
			for _, p := range [][2]int{{0, 0}, {cols - 1, 0}, {0, rows - 1}, {cols - 1, rows - 1}} {
				guest.ok("send_mouse", map[string]any{
					"kind": "click", "x": x0 + p[0], "y": y0 + p[1],
				})
			}
			// And the cell directly outside the island along that axis is
			// NOT the guest's, which is what proves the conversion is a
			// translation rather than a blanket permit.
			ox, oy := tc.outside(x0, y0)
			guest.fails("send_mouse", map[string]any{
				"kind": "click", "x": ox, "y": oy,
			}, "outside this session's island")
		})
	}
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

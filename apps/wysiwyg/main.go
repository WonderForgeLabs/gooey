// wysiwyg: a terminal UI builder that edits gooey markup, driven
// entirely by the component catalog.
//
// This is the catalog's first real consumer, and it exists to test the
// SHAPE rather than to be a finished editor. Three claims from
// docs/specs/2026-08-11-component-catalog-and-wysiwyg-builder.md are
// exercised here against running code:
//
//  1. The palette comes from (*markup.Context).Catalog(), and an element
//     whose attributes are not knowable renders DIFFERENTLY from one
//     that takes no attributes. A registered component is deliberately
//     present so that case is visible rather than theoretical.
//
//  2. The inspector comes from ed.grantOf(parent).AttrsFor(spec) — never
//     from spec.Attrs directly, which is a true statement about the
//     element and a misleading answer to "what can I set here".
//
//     Through ed.grantOf and not markup.AttrsFor(spec, parent), which
//     is the same question asked of the wrong table: the package-level
//     form resolves the parent NAME in the builtin registry and answers
//     "no attached attributes" for a container the host registered, so
//     the drag wrote Table.R onto a child and the grid had no row for
//     it. See attrRows. Fixed in #390 (issue #418) and described
//     wrongly here until the review of #425.
//
//  3. Attached properties are scoped to the PARENT. Press `c` and `v` to
//     retype the container between <Canvas> and <VStack> and watch
//     Canvas.Left enter and leave the selected child's attribute list.
//     That is the rule a flat per-element list cannot express, and the
//     one whose absence would have the editor offering positioning that
//     applyLayout silently discards.
//
// # The page is empty, and that is the current state of the work
//
// The four-pane shell this file used to arrange is gone:
//
//	┌────────────────────────────┬──────────────────┐
//	│ Preview   (REBUILT)        │ Palette          │
//	├────────────────────────────┼──────────────────┤
//	│ Markup output              │ Inspector INPUTS │
//	└────────────────────────────┴──────────────────┘
//
// It worked, and it was the wrong shape: it made you operate a markup
// TREE and showed you a canvas as a side effect, when the canvas is what
// you want to touch. Rearranging four boxes would not have fixed that, so
// the layout is being built again rather than edited — canvas first.
//
// What survives, and where:
//
//   - the four panes, as controls in components/<pane>/, each declaring
//     its own surface with <x:Property> and owning its own chrome. They
//     are unmounted, not deleted: wysiwyg.gooey no longer instantiates
//     them. panes_test.go is what still holds them to their contracts.
//   - everything below the panes — the document model, the catalog-driven
//     palette and inspector, the per-Kind editors, remote mode — is
//     untouched. It is the part that was never the problem.
//
// The one structural rule the old layout existed to enforce is now
// enforced by construction: the preview subtree is thrown away and rebuilt
// on every edit, and replacing a subtree resets component-local state — a
// caret most visibly. <Preview> is a control whose markup is a fixed
// Border around its host, with no slot for children, so an input inside
// the rebuilt island is no longer an arrangement a page can express.
//
// # Building the next one
//
// The editor serves its own control plane (-serve, -mcp) as well as
// attaching to another app's (-attach). That is not symmetry for its own
// sake: with no UI left, patching the running editor through its own
// control plane is how the next UI arrives, and serve_test.go is the
// evidence that the empty page can actually receive one.
//
//	go run . -serve 127.0.0.1:7777 -mcp 127.0.0.1:7778
//
// # Keys
//
//	q, ctrl+c        quit
//	d                DESIGN ↔ LIVE
//	t                flip the theme — today, the toolbox icons' tint
//	x                delete the selected element
//	ctrl+n, ctrl+p   select the next / previous element
//	esc              select the PARENT of the selection
//	alt+enter        select the FIRST CHILD — the inverse, and the only
//	                 way to reach a <Menu> or <MenuItem>, which build no
//	                 component for the pointer to hit
//	alt+k, alt+j     move the selection up / down among its siblings
//	alt+h            PROMOTE — lift the selection out to its grandparent
//	alt+l            DEMOTE — nest the selection into the sibling above it
//	ctrl+d           duplicate the selection, and select the copy
//	ctrl+z, ctrl+y   undo / redo, bounded by -history
//
// CTRL+Z IS NOT SUSPEND HERE, and taking it costs no suspend. term.MakeRaw
// clears ISIG, so while the app holds the terminal the tty driver makes no
// SIGTSTP from it — the byte arrives and input.Decode turns it into an
// ordinary key nothing was binding. App.Suspend and the SIGTSTP dance in
// signals_unix.go are untouched, and an external `kill -TSTP` still
// suspends the editor. Redo is ctrl+y and deliberately NOT also
// ctrl+shift+z, which parses to the identical event as ctrl+z. See
// undo.go.
//
// # Selecting
//
// SHALLOW-FIRST, THE BLEND MODEL, and it is worth being precise about
// which parts of it were verified rather than remembered:
//
//   - a single click selects the OUTERMOST element under the pointer that
//     is a child of the current drill scope — not of the document root.
//     The scope moves as you drill, which is what makes repeated drilling
//     work at all;
//   - a double-click drills EXACTLY ONE LEVEL, not to the deepest hit.
//     Going straight to the bottom would put the intermediate containers
//     back out of reach, which is the defect this model exists to remove;
//   - esc selects the PARENT. That spelling is Microsoft's — "To select
//     the parent of a current selection in the designer, press the ESC
//     key" — and not Figma's, where esc deselects entirely. It walks the
//     scope up with it, so the next click at the same cell re-selects
//     what esc landed on instead of drilling straight back in.
//
// This INVERTED the deepest-first policy that came before it, and the
// reason was a real defect rather than a preference: a <Border> is
// covered entirely by its own child, so every press inside one selected
// the child and the container was reachable only through its one-cell
// chrome — effectively unselectable, and after the drag work effectively
// unmovable.
//
// THE DRILL SCOPE IS DERIVED, not stored: it is parentOf(selection),
// clamped to the user's root. A stored scope would need resetting by hand
// from four places (a click outside it, a delete, a retype, a rebuild)
// and each one it was forgotten in is a designer that selects something
// with no visible reason. See select.go.
//
// A press that lands on no element selects NOTHING, and the properties
// grid goes empty. That is deliberate rather than a gap: the design
// surface is the editor's workspace and is never written to the saved
// document, so selecting it would point the grid at something the user
// cannot save and offer attributes that never reach their file. ctrl+n
// and ctrl+p are the way back out of the empty state — with nothing
// selected they start from whichever end the direction implies, so a
// stray click on the background cannot strand you.
//
// The selection is a NODE, not an index. That is what lets it hold
// "nothing" without a sentinel and name a node at any depth.
//
// # The surface, and what a save writes
//
// The designer draws the document on a <Canvas> that is the editor's
// WORKSPACE — it exists so everything on it has free geometry to be moved
// in, and it is not part of what the user is building. The user's own
// root lives inside it:
//
//	<Canvas Name="Surface">      the workspace, never saved
//	  <Canvas Name="Root">       the USER'S root — this is the document
//	    <Text/> <Button/>        what they placed
//
// A save writes from the user's root down. So does the CODE tab, the
// OUTPUT tab and pushRemote — one artifact seen three ways. Only the
// preview builds the wrapper, because only the preview needs the
// geometry.
//
// Two consequences worth knowing before they surprise you. Pressing what
// looks like empty background usually selects the user's ROOT, because a
// <Canvas> root fills the surface — there is bare surface to click only
// when the root does not cover it. And `c`/`v` retype the USER'S root,
// never the surface; both are Canvases by default, so retyping the wrong
// one would change how the workspace lays out and leave the saved
// document untouched.
//
// # Moving
//
// Drag an element on the surface and it moves. WHAT THAT MEANS IS DECIDED
// BY THE PARENT, because free geometry belongs to the parent and not to
// the element:
//
//   - a child of a <Canvas> has Canvas.Left/Canvas.Top and goes wherever
//     the pointer goes;
//   - a child of a <Grid> has Grid.Row/Grid.Col and SNAPS to whichever
//     cell the pointer is in;
//   - a child of a <VStack> has no geometry at all — its position is its
//     index — so a drag there means reorder, which is not implemented.
//
// The snap happens DURING the drag, cell to cell, not on release. An
// element that floated under the pointer and jumped into a cell when the
// button came up would be a preview that lies about what the release is
// going to do, which is what people report as a bug.
//
// TWO ELEMENTS MAY LAND IN THE SAME CELL. Grid renders that as an overlap
// and reports nothing, and it is accepted rather than solved: bumping the
// second element would move something the user did not drag, and refusing
// the drop would fail the gesture for a reason the pointer gives no cue
// about. The overlap is at least visible on screen.
//
// A REFUSED DRAG SAYS SO. A press on a child of a stack selects it and
// then writes a sentence into the status bar naming the element and the
// container that decided it — because a gesture that silently does
// nothing is what a broken editor looks like too, and there was no
// diagnostic anywhere to tell them apart.
//
// The gesture is two-speed, and the split is the whole design: each
// motion writes the attached properties on the LIVE COMPONENT's
// gooey.Layout (Left/Top, or Row/Col) and asks for a frame, and only the
// release writes markup and rebuilds. Writing markup per motion would
// re-mount the entire designer subtree per pointer report and would look
// identical on screen — which is why drag_test.go pins per-motion and
// on-release damage as a COMPARISON rather than as constants. Measured on
// a two-element document: 6 per motion, 12 on release, 7 when the motion
// drags across another element.
//
// THE POSITIONS HAVE NOWHERE TO LIVE, and that is stated rather than
// solved. Things are positioned on a surface that is never saved, so the
// in-flight offset exists only in memory (dragState) until a release
// records it as Canvas.Left/Top on the element itself. Nothing here
// invents a home for design-time state — no attribute, no comment, no
// property element.
//
// The keyboard reaches all four of these without the pointer: alt+k and
// alt+j reorder among siblings, alt+h promotes, alt+l demotes, and
// ctrl+d duplicates. See move.go and duplicate.go; wysiwyg.gooey holds
// the bindings, and the # Keys table above is the whole surface.
//
// The pointer is a second way in, never the only one. ctrl+n and ctrl+p
// remain the whole gesture from the keyboard, which is not politeness:
// mouse reports cannot be injected through a recording pty at all, so a
// designer that could only be driven by pointer could never be captured.
// See select.go for how the press finds its node, and why the framework
// needed no new seam for it.
//
// # DESIGN and LIVE
//
// The designer pane is a gooey.Frozen host, which is what a design surface
// is FOR: in DESIGN mode (the default) the document lays out and paints
// exactly as it will, and nothing in it is tabbable, clickable, hoverable
// or running — you cannot accidentally operate the thing you are drawing,
// and a <Companion> dropped on the canvas does not spawn its process.
// Press d and the same tree gets its behaviour back so you can try it.
//
// Nothing is re-mounted for that. `design` is one source property; the
// pane's Frozen() reads it, the Composer observes the read, and the frame
// the keystroke schedules re-derives the focus order, the scoped bindings,
// the mnemonics, the hover watchers and the Startable set before anything
// paints. This editor is that mechanism's first consumer, and the mechanism
// stayed a documented constraint until there was one.
//
// The bindings live on the page ROOT. A KeyBinding only fires while the
// focused chain passes through its host, and an empty page has no focus
// stop — on an inner element it would never fire and the app could not be
// quit.
//
// Everything must stay keyboard-operable: mouse events cannot be
// exercised under a recording pty, so a demo that needed one could never
// be captured.
package main

import (
	"context"
	"encoding/xml"
	"flag"
	"fmt"
	"image/color"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/apps/wysiwyg/components/activitybar"
	"github.com/WonderForgeLabs/gooey/apps/wysiwyg/components/panel"
	"github.com/WonderForgeLabs/gooey/apps/wysiwyg/components/preview"
	"github.com/WonderForgeLabs/gooey/apps/wysiwyg/components/toolbox"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/graphics"
	gooeygrpc "github.com/WonderForgeLabs/gooey/grpc"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/mcp"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

func main() {
	addr := flag.String("attach", "", "drive a remote gooey app at this address instead of previewing locally")
	island := flag.String("island", "", "the Name= of the element in the remote app this editor owns")
	// The editor SERVES a control plane as well as attaching to one.
	// Those are opposite directions on the same protocol and both are
	// wanted: -attach makes the editor drive another app, -serve/-mcp
	// make the editor itself drivable, which is how its own UI gets
	// built — patch_markup into the running editor instead of
	// edit-rebuild-relaunch.
	//
	// Port 0 by default, on loopback, and read back from Addr(): a fixed
	// port is a collision the day two of these run at once (#188).
	// Neither endpoint is authenticated and neither restricts its bind
	// address, so a non-loopback one exposes this editor's control
	// plane; that is the operator's choice.
	serveAddr := flag.String("serve", "127.0.0.1:0", "bind address for this editor's OWN gRPC control plane; empty disables it. UNAUTHENTICATED — a non-loopback address exposes it")
	mcpAddr := flag.String("mcp", "127.0.0.1:0", "bind address for this editor's OWN MCP endpoint; empty disables it. UNAUTHENTICATED — a non-loopback address exposes it")
	// Chrome is drawn in PIXELS where the terminal has them, and an app
	// gets a pixel protocol only when capabilities say so — the
	// environment ladder reports colour depth but can never report
	// graphics support, so without a probe the answer is always "none"
	// and every sixel component silently falls back to cells.
	//
	// The probe is the honest route: DA1 reports sixel support AND the
	// cell size sixel scales by. -graphics forces one for a terminal that
	// supports it without saying so, and then the cell size has to be
	// assumed, because only a probe could have known it.
	gfx := flag.String("graphics", "", `force a pixel protocol: "sixel", "kitty", "iterm2", or "cells" for the halfblock fallback; empty probes`)
	// THE WORKSPACE IS A DIRECTORY, VS Code's model, and it is a flag
	// rather than a positional so that starting with no folder open stays
	// the default. Empty means no workspace: the Explorer pane says so and
	// Save is greyed, which is the honest empty state rather than an
	// editor that silently has nowhere to write.
	wsDir := flag.String("workspace", "", "open this directory as the workspace; empty starts with no folder")
	// Every undo step holds a whole copy of the document and an editing
	// session has no length limit, so the stack needs a ceiling or it
	// grows for as long as the editor is open. 0 turns undo off; a
	// negative depth is an error rather than a silent clamp, because
	// -history -1 is someone reaching for "unlimited" and unlimited is
	// the thing the bound exists to refuse.
	histMax := flag.Int("history", DefaultHistoryLimit, "how many undo steps to keep; 0 disables ctrl+z")
	flag.Parse()
	if *histMax < 0 {
		gooey.Exit(fmt.Errorf("wysiwyg: -history %d: want a depth of 0 or more; there is no unlimited", *histMax))
	}

	opts := []gooey.Option{}
	switch *gfx {
	case "":
		opts = append(opts, gooey.WithCapabilityProbe())
	case "cells":
		opts = append(opts, gooey.WithGraphics(nil))
	default:
		enc, err := encoderNamed(*gfx)
		if err != nil {
			gooey.Exit(err)
		}
		// Just the protocol. "A forced protocol still needs a cell size"
		// was a hand-written 10x20 here; App.caps owns that rule now and
		// backfills term.DefaultCellW/H for a pinned non-nil encoder,
		// having already defaulted Color to term.DetectColorDepth() — so
		// the Caps this used to pass is exactly what it now computes
		// (#322, TestPinnedProtocolNeedsNoHandWrittenCaps).
		opts = append(opts, gooey.WithGraphics(enc))
	}

	root := editorFS()
	ed := newEditor(root)
	// A missing or malformed icon is a STARTUP failure naming the file.
	// It cannot be reported from a Render, and a toolbox that silently
	// lost its icons is the least diagnosable outcome available.
	if ed.iconErr != nil {
		gooey.Exit(ed.iconErr)
	}
	// Before the first rebuild, which is what establishes the baseline.
	ed.setHistoryLimit(*histMax)
	pageSrc := func() []byte {
		b, _ := fs.ReadFile(root, PageFile)
		return b
	}
	// Page WATCHES, and it has to be told what to watch: it cannot infer
	// that a <Palette> will resolve to a file. Every pane's markup is
	// listed, so editing any of them rebuilds the page in place — which is
	// the reason the panes read from disk rather than from an embed.
	app := gooey.NewApp(markup.Page(root, PageFile, ed.ctx, paneFiles...), opts...)
	ed.app = app
	ed.setDispatcher(app.Dispatcher())
	ed.watchFit(app)
	ed.bindClipboard(app)
	// Click-to-select. The composer is resolved per press rather than
	// captured: a hot reload of the page builds a new one.
	//
	// A Visibility="Hidden" element on the canvas is NOT selectable this
	// way, and that is the walk's decision rather than an oversight here:
	// since #465 hitTest skips a node that renders no content, so a press
	// over one selects the ancestor beneath it. Pointing at a component
	// that draws nothing has no answer, and the alternative — the canvas
	// resolving a click differently from the running app — is the
	// divergence that work exists to remove. Reaching an element that is
	// not on screen is a tree-pane job, the same as it has always been
	// for Collapsed. See hitTest in mouse.go.
	ed.bindPicking(func(x, y int) gooey.Component {
		c := app.Composer()
		if c == nil {
			return nil
		}
		return c.Focus().HitTest(x, y)
	}, app.Invalidate)

	// Both servers are handed ed.ctx — the EDITOR's context, not docCtx.
	// A control-plane client is driving the editor, so the vocabulary it
	// validates and patches against is the editor's chrome. Without a
	// Context the name-addressed RPCs report FAILED_PRECONDITION and the
	// whole point is lost.
	//
	// Started BEFORE app.Run so a client that connects immediately finds
	// a listener; the session hooks are posted to the UI goroutine and
	// run when the loop reaches them.
	if *serveAddr != "" {
		// The watch exists BEFORE Serve because OnSessions has to be in
		// the Options, and Serve's accept goroutine can fire it before
		// Serve has returned. It marshals every notification with
		// app.Post — see servelink.go for why that is unconditional.
		grpcWatch := newLinkWatch(ed.link("grpc"))
		gsrv, err := gooeygrpc.Serve(app, gooeygrpc.Options{
			Addr:       *serveAddr,
			Context:    ed.ctx,
			Doc:        pageSrc,
			Name:       "gooey-wysiwyg",
			Version:    "1",
			OnSessions: grpcWatch.onSessions,
		})
		if err != nil {
			gooey.Exit(err)
		}
		// Close joins rather than merely signalling.
		defer gsrv.Close()
		ed.serving = append(ed.serving, "grpc "+gsrv.Addr())
		grpcWatch.bindSessions(app.Post, gsrv)
	}
	if *mcpAddr != "" {
		// No Doc here: mcp.Options has no equivalent — the declared-schema
		// path is the gRPC server's, and swap_markup builds against
		// Context instead.
		// bindStateless, not bindSessions: this endpoint is stateless by
		// design and has TWO states, serving or not. servelink.go says
		// why inventing a third means inventing a fact.
		mcpWatch := newLinkWatch(ed.link("mcp"))
		msrv, err := mcp.Serve(app, mcp.Options{
			Addr:       *mcpAddr,
			Context:    ed.ctx,
			Name:       "gooey-wysiwyg",
			Version:    "1",
			OnServeEnd: mcpWatch.onServeEnd,
		})
		if err != nil {
			gooey.Exit(err)
		}
		defer msrv.Close()
		ed.serving = append(ed.serving, "mcp "+msrv.URL())
		mcpWatch.bindStateless(app.Post, msrv)
	}
	if len(ed.serving) > 0 {
		// Joined with SPACES, not a newline. This lands in a one-row status
		// bar, so "grpc …\nmcp …" showed the gRPC address and silently ate
		// the MCP URL — the one string a client needs and cannot guess,
		// since the port is 0 by default and therefore different every run.
		ed.serveInfo.Set(strings.Join(ed.serving, "   "))
	}

	if *addr != "" {
		if *island == "" {
			gooey.Exit(fmt.Errorf("-attach needs -island: the editor owns exactly one named element in the target and never writes outside it"))
		}
		// The context handed to attach governs the STREAM's lifetime,
		// not just the handshake — Attach(ctx) lives as long as ctx
		// does. A timeout here silently kills every session when it
		// expires, which reads to a user as the editor freezing.
		// Cancellation belongs to the app's lifetime instead.
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		if err := ed.attach(ctx, *addr, *island); err != nil {
			gooey.Exit(err)
		}
		defer ed.remote.r.Close()
	}
	// The folder box is resolved by NAME from the built page rather than
	// held from construction: markup.Page WATCHES, so a save to the .gooey
	// builds a new tree and the component held from the first build is
	// stale. Re-resolving on every swap keeps ctrl+o pointing at the box
	// that is actually on screen.
	bindPathBox := func() {
		if b, err := markup.Find[*components.TextBox](ed.ctx, "PathBox"); err == nil {
			ed.pathBox = b
		}
	}
	bindPathBox()
	app.OnSwap(func(gooey.Component) { bindPathBox() })
	if *wsDir != "" {
		ed.setWorkspace(*wsDir)
	}
	ed.rebuild()
	if err := app.Run(context.Background()); err != nil {
		gooey.Exit(err)
	}
}

// PageFile is the editor's own page, and paneFiles is every control's
// markup. Both are paths in the editor's root FS.
const PageFile = "wysiwyg.gooey"

var paneFiles []string

// editorFS is the editor's root: the directory holding wysiwyg.gooey and
// components/. `go run .` puts that at the working directory; an
// installed binary carries it beside the executable.
//
// os.DirFS rather than embed.FS is a development choice with a cost and a
// payoff. The cost is that the binary is not self-contained. The payoff is
// that every .gooey in the tree hot reloads — the page and all three
// markup panes — which is what makes editing the UI a save rather than a
// rebuild. Swapping in an embed.FS for a release changes this function
// and nothing else.
func editorFS() fs.FS {
	dir := "."
	if _, err := os.Stat(filepath.Join(dir, PageFile)); err != nil {
		exe, _ := os.Executable()
		dir = filepath.Dir(exe)
	}
	return os.DirFS(dir)
}

// encoderNamed resolves -graphics. The list is the encoders that exist,
// not a guess: an unknown name is an error at startup rather than a
// silent fallback to cells, because "my sixel chrome did not appear" is
// the least diagnosable failure this flag has.
func encoderNamed(name string) (graphics.Encoder, error) {
	switch name {
	case "sixel":
		return graphics.Sixel{}, nil
	case "kitty":
		return graphics.Kitty{}, nil
	case "iterm2":
		return graphics.ITerm2{}, nil
	}
	return nil, fmt.Errorf("wysiwyg: -graphics %q: want sixel, kitty, iterm2 or cells", name)
}

// ---- the document being edited ----

// node is one element of the edited document. It is deliberately not a
// gooey component: the editor manipulates a document, and the tree is
// derived from it.
type node struct {
	Elem string
	// Space is the element's resolved XML NAMESPACE — the default
	// xmlns in scope for an unprefixed element, the prefix's binding
	// for a prefixed one.
	//
	// IT IS NOT EMPTY FOR ORDINARY COMPONENTS, which is what this said
	// until review of #522 and is wrong about every file the editor
	// opens. nodeOf tracks the inherited default xmlns in its
	// `defaults` stack precisely so it can resolve them. Measured on
	// the shape every in-tree .gooey under apps/ uses — the count that
	// stood here was wrong at the commit that wrote it, and the
	// property is what matters: `find apps -name '*.gooey' | xargs
	// grep -L 'xmlns='` comes back empty.
	//
	//	<Gooey xmlns="wonderforge.io/gooey/2026">   Gooey  Space="wonderforge.io/gooey/2026"
	//	  <Canvas Name="Root">                      Canvas Space="wonderforge.io/gooey/2026"
	//	    <Button Name="B"/>                      Button Space="wonderforge.io/gooey/2026"
	//
	// Space is empty only for a document with no default xmlns at all,
	// and that is NOT only fixtures — this said "palette seed strings
	// and hand-written fixtures" until review of #522 listed three
	// shipped, editor-openable documents with no default xmlns
	// (cmd/typeahead, grpc/cmd/grpcdemo, presentations/the-rectangle;
	// derive them with `grep -L 'xmlns='` over the tree's .gooey
	// files). The workspace browser scans whatever directory it is
	// pointed at, so all three open here with Space == "" on every
	// node. So `n.Space == ""` does not mean "not namespaced" AND it
	// does not mean "fixture"; it means the document declares no
	// default xmlns, and this paragraph is the one place a reader
	// learns what the field holds. Nothing is broken by the old
	// sentence today because splitDecls keys on k.Elem == "Property"
	// (matching markup's own c.Name == "Property") rather than on
	// Space == "".
	//
	// It is kept because one partition depends on it and cannot be made
	// from Elem: <x:Property> is a language declaration and <Property>
	// is a component name somebody could register, and encoding/xml has
	// already resolved the prefix by the time this model is built.
	// markup's own splitDeclarations keys on the same thing (c.Space ==
	// markup.XNamespace, markup/property.go), so the editor is asking
	// the same question rather than a lookalike. Added for #517.
	//
	// It is NOT written back out by markup(): a declaration is emitted
	// by the envelope, which re-derives the prefix from the xmlns the
	// document declares.
	//
	// EVERY element carries this, not only a declaration — nodeOf sets
	// it from whatever encoding/xml resolved — and for anything but a
	// declaration the prefix is DROPPED on write: <t:Thing/> under the
	// content root is re-emitted as <Thing/>. That loss predates this
	// field and is out of scope here, but this field is the first thing
	// in the model that can detect it, and this comment used to say
	// "nothing else in the tree carries a namespace", which asserts the
	// loss cannot happen. Raised in review of #522.
	Space string
	Attrs map[string]string
	// Body is the element's TEXT CONTENT — the "hello" in
	// <Text>hello</Text> — and it is a field rather than an entry in
	// Attrs because it is genuinely not an attribute. <Text>'s own
	// catalog entry says so ("The content is the element's body, not an
	// attribute", markup/elements.go defText.Doc), and until this field
	// existed the editor could not express it at all: every <Text> the
	// toolbox added serialized as <Text Name="Text3"/>, which builds
	// fine, measures zero and is therefore invisible on the canvas AND
	// unhittable — hitTest never returns a zero-size component. A user
	// who reached for Text first would have concluded that selecting was
	// broken, and reported a serialization bug.
	Body string
	Kids []*node
	// Slots are property elements — <ItemsView.ItemTemplate> — which
	// are structured attributes rather than children, and which the
	// catalog can report as REQUIRED.
	Slots map[string]*node
}

// bodySpec is the catalog's answer to "is this element's content its
// body", and nil means it is not.
//
// THIS USED TO BE `elem == "Text"`. The fact was real but lived only in
// defText's Doc prose, so an editor had no data to read and had to
// hardcode the one name — the denylist shape this project keeps
// deleting. markup.BodySpec is that fact moved into data, and this is
// now a lookup in the SAME catalog the palette is built from, so an
// element that gains a body is offered one here without this file
// changing.
//
// Asked of the CATALOG (ed.specs), not of the palette, and it is the
// fourth reader of that class to be corrected — after target(),
// specFor/specOrBare and grantOf, each for the same sentence: the palette
// is the catalog minus what may not be PLACED on its own, and this asks
// what may be SET. bodySpec is asked of target.Elem, which since this
// PR's alt+enter can be a Nested element the palette does not contain, so
// a nested element declaring a Body would lose its body row from the
// inspector with no error.
//
// Latent today and measured rather than assumed: <Text> is the only
// element in the catalog declaring a Body, and it is neither Nested nor
// NonVisual, so ed.specs gives an identical answer. The old comment
// justified the palette by "the editor's own chrome is deliberately not
// in it" — true, and a property of ed.docCtx, which ed.specs comes from
// too, so it never separated the two.
//
// It is also a map lookup where the palette was a linear scan, and
// attrRows calls this inside a prop.NewComputed's evaluation. Found in
// review of #454.
func (ed *editor) bodySpec(elem string) *markup.BodySpec {
	e, ok := ed.specOf(elem)
	if !ok {
		return nil
	}
	return e.Body
}

// takesBody is the boolean form. Nothing on the seeding path calls it any
// more — the seed itself decides whether an element carries a body, and
// addSelected keys off n.Body rather than off the declaration. It stays
// because body_test.go uses it to cross-check the two against each other:
// a node with a body whose element declares none, or the reverse, is a
// disagreement between the seed and the vocabulary.
func (ed *editor) takesBody(elem string) bool { return ed.bodySpec(elem) != nil }

// grantOf is the catalog's answer to "what geometry does this element
// give its children" — the attached-property surface a parent
// contributes — and it is the ONLY thing in this editor that decides what
// dragging means.
//
// THIS USED TO BE `switch p.Elem { case "Canvas": ...; case "Grid": ... }`
// in dragKind, with everything else falling through to "reorder". The
// rule was right and the key was wrong: an editor that knows the two
// container names it was written against cannot be extended by an app
// that registers a container of its own, and gooey's whole markup story
// is that a host adds elements. Reading it from the same catalog the
// palette is built from means a third-party <Table> declaring
// GrantCell is designable here with no change to this file.
//
// THE CATALOG RATHER THAN THE PALETTE, which is the second half of the
// opening above and not a second definition of the function.
//
// Same distinction target() was just corrected for, one line away and
// missed: the palette is the catalog minus what may not be PLACED on its
// own, and this asks what may be SET. A <MenuItem>'s parent is a <Menu>,
// which is Nested and therefore absent from the palette — so the scan
// returned the empty grant and every attached row vanished from the
// inspector with no error. Inert only because defMenu grants nothing;
// the first nested container that grants an attached property would lose
// them all, which is #418's defect returning through the fix for #429's.
// Found in review of #454.
func (ed *editor) grantOf(elem string) markup.Grant {
	e, ok := ed.specOf(elem)
	if !ok {
		return markup.Grant{}
	}
	return e.Grants
}

// attrValue is an attribute value written as XML — quoted, and escaped
// the way n.Body is escaped below.
//
// %q IS GO QUOTING, NOT XML, and the difference is a file the designer
// cannot reopen. A value the loader accepts, `Content="Save &amp; Exit"`,
// came back out of this emitter as `Content="Save & Exit"`: the canvas
// refused its own rebuild with "markup: no root element" and a save
// wrote a document whose reopen fails on "invalid character entity &".
// A `"` in a value is the same class. gooeyOpen's comment argued the two
// emitters "must agree about quoting", which is true and is why both are
// fixed rather than a reason to keep the lossy one. Seeds are ASCII
// markup this repo controls; since #472 this also writes files somebody
// else wrote. Raised in review of #501.
func attrValue(v string) string {
	var esc strings.Builder
	xml.EscapeText(&esc, []byte(v))
	return `"` + esc.String() + `"`
}

func (n *node) markup(indent string) string {
	var b strings.Builder
	b.WriteString(indent + "<" + n.Elem)
	for _, k := range sortedKeys(n.Attrs) {
		fmt.Fprintf(&b, " %s=%s", k, attrValue(n.Attrs[k]))
	}
	// A body and children are mutually exclusive here: no element in the
	// catalog takes both, and emitting both would make the body's meaning
	// depend on where the parser happened to put it.
	if n.Body != "" && len(n.Kids) == 0 && len(n.Slots) == 0 {
		// ESCAPED, because the body is free text the user typed. An
		// unescaped "<" or "&" turns the whole document into a parse
		// error, and a .gooey parse error surfaces as "no root element"
		// rather than as anything pointing at the character.
		var esc strings.Builder
		xml.EscapeText(&esc, []byte(n.Body))
		b.WriteString(">" + esc.String() + "</" + n.Elem + ">\n")
		return b.String()
	}
	if len(n.Kids) == 0 && len(n.Slots) == 0 {
		b.WriteString("/>\n")
		return b.String()
	}
	b.WriteString(">\n")
	for _, s := range sortedKeys(n.Slots) {
		fmt.Fprintf(&b, "%s  <%s.%s>\n", indent, n.Elem, s)
		b.WriteString(n.Slots[s].markup(indent + "    "))
		fmt.Fprintf(&b, "%s  </%s.%s>\n", indent, n.Elem, s)
	}
	for _, k := range n.Kids {
		b.WriteString(k.markup(indent + "  "))
	}
	b.WriteString(indent + "</" + n.Elem + ">\n")
	return b.String()
}

// carryDeclarations copies a <Gooey> envelope's namespace declarations
// onto the document root about to be promoted in its place.
//
// The envelope is NOT a node — gooeyOpen re-emits it from ed.envAttrs,
// which holds what did NOT come down here — so a declaration left on it
// and recorded nowhere is discarded by the unwrap, which is the half of
// #472 that survived nodeOf keeping them. A hand-written document puts
// xmlns on the envelope, because that is where markup's error tells the
// author to put it, and so does every file saved before this change — so
// this is the spelling the editor has to read. It is not the spelling the
// editor now WRITES: this function moves an attribute prefix down onto
// the root and RETURNS THE SET IT MOVED, envelopeAttrs subtracts that
// set, and gooeyOpen writes what is left back. Not "computes the
// complement", which is what this said and what envelopeAttrs' own doc
// spends a paragraph refusing to do: a second predicate over the same
// attributes is the shape three rounds of this branch kept reaching for
// and getting wrong. Raised in review of #501.
//
// THE ROOT'S OWN DECLARATION IS LEFT ALONE, and the reason is not XML
// subtree scoping — markup.parse keeps one flat, document-wide ns map
// and takes the LAST declaration of a prefix in document order
// (the `a.Name.Space == "xmlns"` arm of markup.parse's attribute loop).
// The envelope is parsed before its child, so
// for openWorkspaceFile last-wins and child-wins give the same answer;
// not overwriting is what keeps the editor agreeing with the loader
// about which URI a prefix has.
//
// THAT REASONING IS THE OPEN PATH'S AND DOES NOT CROSS TO PASTE, which
// is the half this comment claimed for both call sites and had no right
// to. unwrapGooey lands the declaration on a node INSIDE the open
// document, later in document order than the root's own, so a pasted
// prefix bound to a different URI wins for the whole document — every
// expression already using it included. reconcileNamespaces
// (clipboard.go) is where that is settled, before the subtree is
// attached; this function's job is only to keep the envelope's
// declaration from being lost with the envelope. Raised in review of
// #501.
//
// THE TWO SHAPES nodeOf WRITES, not a prefix test: it emits "xmlns" and
// "xmlns:"+local (the two `continue` arms in nodeOf's attribute loop)
// and nothing else, while
// HasPrefix(k, "xmlns") also matches a plain attribute spelled
// xmlnsFoo — which would be copied onto the user's root and turn an
// envelope-level mistake into an unknown-attribute error reported
// against the child.
//
// CITED BY SYMBOL, NOT BY LINE, and that is a measurement about this
// file rather than a style: every self-referencing line number added in
// the round before this one was stale by the next commit, because the
// commit that added this function shifted main.go by 51 lines and
// nothing in the suite checks a citation inside a Go comment
// (claudemd_test.go and specclaims_test.go cover CLAUDE.md and
// docs/specs/). A citation into the file you are currently growing is
// the one that rots first. Raised in review of #501.
//
// AN ELEMENT PREFIX DOES NOT COME DOWN, and XNamespace is the test
// because the prefix spelling is the author's. `x:` prefixes an ELEMENT,
// and the elements it prefixes — <x:Property> — are children of the
// ENVELOPE, not of the content root: markup.parseDocument hands the whole
// <Gooey> to splitDeclarations and only afterwards requires one visual
// kid. encoding/xml resolved that prefix with real subtree scoping before
// markup saw it, so moving the declaration onto the content root puts it
// out of scope at its own sibling and the saved file stops loading. The
// flat-table reasoning above belongs to a prefix inside an attribute
// VALUE — a handler or value expression — and does not reach here.
//
// THIS ARM IS LIVE ON BOTH UNWRAPS, and the round that added it said the
// opposite: that openWorkspaceFile refuses such a document as a second
// root (#517), so only a direct call could reach the skip. What #517
// refuses is a document CONTAINING an <x:Property> — that is two kids of
// <Gooey>, and the len(n.Kids) != 1 arm turns it away. A document that
// merely DECLARES xmlns:x has one kid and opens like any other, measured
// through openWorkspaceFile in
// TestAnElementPrefixStaysOnTheEnvelopeThroughAnOpen; pasteMarkup ->
// unwrapGooey reaches the same skip the other way. So this is not
// pre-work for #517: delete it and every <Gooey xmlns:x> document already
// on disk gets its element prefix moved onto the content root, where it
// no longer scopes the siblings of that root.
//
// THAT LAST CLAUSE USED TO NAME <x:Property>, and nodeOf's element
// refusal has taken the live example away: a document CONTAINING one is
// now turned back at the read, before this function is reached. The
// guard is unchanged and still right — the declaration is the author's
// and the move is a fidelity loss on every `<Gooey xmlns:x>` file on
// disk today, none of which need contain an <x:Property> — but the
// justification had drifted back to a state no document reaching here
// can be in, which is the same correction beb3501 made arriving from
// the other direction. Corrected in review of #501, twice.
//
// ONE FUNCTION BECAUSE THERE ARE TWO UNWRAPS. openWorkspaceFile had
// this inline and unwrapGooey (clipboard.go) had nothing, so #472
// survived through paste: the CODE tab's own output, copied whole and
// pasted back, lost its prefixes and the canvas refused the handler
// expression it had just rendered. A fix applied at one of two
// identical seams is the shape that leaves the other one open. Raised
// in review of #501.
//
// IT RETURNS WHAT IT MOVED, which is the only way its complement can be
// exact. envelopeAttrs re-derived the answer from values and the two
// predicates disagreed in one place — a prefix declared at both levels
// with the SAME URI, which this function skips on key presence and that
// one dropped on value equality. See there. Raised in review of #501.
func carryDeclarations(env, root *node) map[string]bool {
	var moved map[string]bool
	for k, v := range env.Attrs {
		if !isNamespaceAttr(k) || v == markup.XNamespace {
			continue
		}
		if _, ok := root.Attrs[k]; !ok {
			root.Attrs[k] = v
			if moved == nil {
				moved = map[string]bool{}
			}
			moved[k] = true
		}
	}
	return moved
}

// envelopeAttrs is everything on a <Gooey> that did NOT move down with
// carryDeclarations — kept so gooeyOpen can write it back. Call it AFTER
// carryDeclarations and pass the set it returned: the question it
// answers is what actually carried, not what was eligible to.
//
// THE COMPLEMENT IS OBSERVED, NOT ASSUMED. This dropped every xmlns:
// unconditionally while carryDeclarations skips a prefix the root
// already declares, so a document declaring one prefix at BOTH levels
// lost the envelope's copy outright — <Gooey xmlns:t="urn:A"> over
// <Canvas xmlns:t="urn:B"> saved as <Gooey> and urn:A was gone from the
// file. The resolved binding was unchanged, because the loader's flat
// last-wins map had already picked urn:B, so this was fidelity rather
// than meaning — but the comment here claimed immunity from exactly that
// class, and the claim was the stated reason nobody need cross-check the
// two predicates. Comparing against what the root now holds makes the
// complement true instead of asserted. Raised in review of #501.
//
// AND THE SIBLING'S OWN EXCEPTION CAME WITH IT, which the paragraph
// above had just finished arguing was the whole point. carryDeclarations
// refuses to move markup.XNamespace down; this compared values and fell
// into the skip whenever the content root happened to declare the same
// URI, whether or not anything had put it there. Measured end to end
// through the file browser on a document declaring xmlns:x at BOTH
// levels:
//
//	envAttrs = map[]
//	rebuilt  = "<Gooey>"  over  <Canvas … xmlns:x="…">
//
// — the exact relocation the sibling guard exists to prevent, reached
// through the complement instead. Raised in review of #501.
//
// SO IT IS NO LONGER DERIVED AT ALL. Both repairs above narrowed a
// re-derivation of the sibling's decision, and a third route was left:
// an ordinary prefix declared at both levels with the SAME URI.
// carryDeclarations skips it on key presence and this dropped it on
// value equality, so <Gooey xmlns:t="urn:a"> over <Canvas
// xmlns:t="urn:a"> saved as a bare <Gooey> — the same relocation again,
// bracketed on both sides by tests that could not see it (one uses two
// URIs, the other uses one URI but only markup.XNamespace, which the
// exception short-circuits). Two predicates cannot be kept in agreement
// by a comment saying they agree; this now takes the SET
// carryDeclarations actually moved, so there is one decision and no
// complement to get wrong. Raised in review of #501, three times.
func envelopeAttrs(env *node, moved map[string]bool) map[string]string {
	out := make(map[string]string, len(env.Attrs))
	for k, v := range env.Attrs {
		if moved[k] {
			continue
		}
		out[k] = v
	}
	return out
}

// envelopeHead is the document's opening <Gooey …> tag together with
// the declarations that belong to the envelope rather than to the tree.
//
// <x:Property> is a child of the ENVELOPE, not of the content root —
// markup hands the whole <Gooey> element to splitDeclarations, which
// partitions its children and only then requires one visual kid. The
// editor's document is the content root, so a declaration has nowhere
// in the tree to live and rides with envAttrs instead, written back
// here. Added for #517.
func envelopeHead(attrs map[string]string, decls []*node) string {
	attrs, prefix := envelopeParts(attrs, decls)
	var b strings.Builder
	b.WriteString(gooeyOpen(attrs))
	for _, d := range decls {
		q := *d
		q.Elem = prefix + ":" + d.Elem
		q.Attrs = declAttrs(d.Attrs, prefix)
		b.WriteString(q.markup("  "))
	}
	return b.String()
}

// envelopeParts is the <Gooey> attributes the save will actually write
// and the prefix its declarations go out under — the half of
// envelopeHead that decides what the FILE binds, split out from the half
// that renders it.
//
// It is split because a second caller needs the decision and not the
// bytes: reconcileNamespaces has to know which prefixes the saved
// envelope binds in order to refuse a paste that rebinds one, and
// deriving that from the returned string would mean parsing markup this
// function has just finished writing. Splitting it is also what stops
// the two drifting — a change to the minting rule below moves the
// refusal with it, where a mirror of the rule in clipboard.go would go
// one scope short the next time this grows, which is exactly how it
// went one scope short this time. Raised in review of #522.
//
// THE PREFIX AND THE BINDING TRAVEL TOGETHER. Writing x: without an
// xmlns:x on the tag saves a file markup.Build refuses, and
// saveOpenFile is not gated on the build, so the editor reported
// "✓ saved" over it.
//
// WHICH DOCUMENTS REACH IT IS declPrefix'S ANSWER, NOT A LIST HERE.
// Every shape whose declPrefix reports bound == false arrives at the
// write: a declaration binding the namespace as its own default xmlns,
// and each of the decline-and-mint routes — the envelope's prefix
// spent by a sibling declaration, an adoptable declaration prefix
// spent by another, a minted prefix the declaration itself binds.
//
// This paragraph named a fixed set twice and was short both times:
// first "two", corrected to "one" at the base merge, then left at
// "one" while f525472 and 5fa3a62 added the mint routes in this same
// branch — four of the seven arms of
// TestASavedDeclarationCarriesTheBindingThatNamesIt reach it today.
// Nothing went red either time, because the behaviour is pinned by
// other arms; what rotted was the reason, in the one file whose other
// arms reason from these comments. So the rule is the bool, and the
// population is a run of the fixture rather than a sentence.
//
// The correction that is still worth keeping is the one about a
// document binding the same prefix on <Gooey> AND on the content root,
// which does NOT reach it: carryDeclarations skips
// v == markup.XNamespace, so the envelope's xmlns:x is never in the
// moved SET, and envelopeAttrs — which since #501 takes that set rather
// than re-deriving the answer — keeps it. Measured through
// openWorkspaceFile: envAttrs holds xmlns:x, bound is true, and
// withDeclBinding is not called.
func envelopeParts(attrs map[string]string, decls []*node) (map[string]string, string) {
	prefix, bound := declPrefix(attrs, decls)
	if len(decls) > 0 && !bound {
		attrs = withDeclBinding(attrs, prefix)
	} else {
		// A COPY ON THIS PATH TOO, for the reason declAttrs' doc gives
		// about its own: both callers hand this ed.envAttrs, the
		// editor's live document state, and a fast path that returns
		// the caller's map when nothing needs changing makes the copy
		// conditional on the input — so the guarantee holds for
		// whichever document the test picked and not for the one a
		// future caller writes through. Inert today (envelopeHead and
		// envelopeNamespaces only read it), which is the same "inert
		// for the same reason" the declAttrs round declined to rely
		// on. Raised in review of #522.
		out := make(map[string]string, len(attrs))
		for k, v := range attrs {
			out[k] = v
		}
		attrs = out
	}
	// AND NO OTHER BINDING OF THE DECLARATION NAMESPACE SURVIVES ON
	// <Gooey>. Only declarations use that namespace and every one of
	// them is written under `prefix`, so a second envelope binding of
	// it names nothing — and it is not merely tidiness: the author's
	// binding of the SAME prefix to a value namespace, on a
	// declaration, comes later in markup's one flat document-order map
	// and is what {{p:Thing}} already resolves through. Keeping the
	// envelope's copy while writing the declaration as <x:Property>
	// leaves a dead binding whose only effect is to look live.
	// Reached when declPrefix declines the envelope's own prefix
	// because a declaration spends it. Raised in review of #522.
	for k, v := range attrs {
		if v != markup.XNamespace || !strings.HasPrefix(k, "xmlns:") {
			continue
		}
		if strings.TrimPrefix(k, "xmlns:") == prefix {
			continue
		}
		out := make(map[string]string, len(attrs))
		for kk, vv := range attrs {
			if kk != k {
				out[kk] = vv
			}
		}
		attrs = out
	}
	return attrs, prefix
}

// envelopeNamespaces adds to into every prefix binding the saved
// envelope carries — the ones on <Gooey> itself, minted binding
// included, and the ones each declaration is written out with.
//
// INTO, and not a fresh map, because these are seeded before the
// document's own and the order is the contract: markup.parse merges
// every declaration into one flat map in document order and the last
// one parsed wins, and the envelope and its declaration children are
// both outside — and before — the content root.
//
// declAttrs, not d.Attrs: a declaration's binding of markup.XNamespace
// under some OTHER prefix is dropped on the way out, so it is not a
// binding the file has and a paste that re-points it conflicts with
// nothing. Reading the node's own attrs instead would refuse pastes the
// saved document has no quarrel with. Added for #522.
func envelopeNamespaces(attrs map[string]string, decls []*node, into map[string]string) {
	head, prefix := envelopeParts(attrs, decls)
	for k, v := range head {
		if isNamespaceAttr(k) {
			into[k] = v
		}
	}
	for _, d := range decls {
		for k, v := range declAttrs(d.Attrs, prefix) {
			if isNamespaceAttr(k) {
				into[k] = v
			}
		}
	}
}

// declPrefix is the prefix these declarations are written back under,
// and whether the document already binds it somewhere the saved file
// will still carry — so an envelope binding is added only when there is
// nothing else naming the namespace.
//
// TWO PLACEMENTS ARE LEGAL, which is what this exists for and what
// declBinding alone could not see. XML scoping puts xmlns:p on <Gooey>
// OR on the <p:Property> element itself; markup/property.go's own
// comment records both and TestTheXPropertyRefusalNamesTheRoot pins all
// three placements. Reading only the envelope, the element-level
// placement round-tripped LOSSILY: the author's p: became a minted x:,
// a new xmlns:x appeared on <Gooey>, and the xmlns:p left on the
// declaration named nothing. The file still loaded, so nothing went
// red — the PR's stated invariant ("a file that binds p: is saved with
// p:") simply stopped holding one placement over. Raised in review of
// #522.
//
// The envelope wins when it binds one, because that is the binding
// every declaration is under. Otherwise the first declaration carrying
// its own binding names them all, and the envelope gains a binding only
// if some declaration does not carry that same one — which is the only
// case where writing the prefix alone would save a file markup refuses.
func declPrefix(attrs map[string]string, decls []*node) (string, bool) {
	// ONE SCAN OF attrs FOR THE BINDING. declBinding answers the
	// envelope's own question here — which prefix, if any, it binds to
	// markup.XNamespace. The mint below calls mintDeclPrefix
	// instead, because the spelling to mint depends on what the
	// DECLARATIONS have spent as well, which this call cannot see.
	// Raised in review of #522, twice: the first round collapsed two
	// calls into one on the grounds that they could not disagree, and
	// they were not asking the same question.
	envPrefix, envBound := declBinding(attrs)
	if envBound && !spentElsewhere(envPrefix, decls) {
		return envPrefix, true
	}
	// NOT A PREFIX A SIBLING HAS SPENT, which is the half the mint's
	// own fix did not reach. This loop adopts the first declaration
	// carrying a binding; declAttrs' first clause then drops any
	// xmlns:<prefix> on the OTHER declarations, because on an emitted
	// <p:Property> a xmlns:p naming anything else would unname the
	// element. So a second declaration binding the same prefix to a
	// value namespace lost that binding, and a document using
	// {{p:Thing}} was saved as bytes markup.Build refuses — the third
	// shape reaching that clause, where declAttrs' doc said there were
	// two. Measured in review of #522.
	//
	// Falling through to the mint is the whole fix: mintDeclPrefix is
	// handed every prefix the declarations hold and avoids all of
	// them, so it picks a free one and both bindings survive.
	prefix := ""
	for _, d := range decls {
		p, ok := declBinding(d.Attrs)
		if !ok {
			continue
		}
		if spentElsewhere(p, decls) {
			break
		}
		prefix = p
		break
	}
	if prefix == "" {
		// MINTED AGAINST THE DECLARATIONS TOO, not just the envelope —
		// the second call is what the paragraph above used to say was
		// unnecessary, and it is necessary for a different question:
		// this one avoids prefixes the DECLARATIONS have spent, which
		// declBinding(attrs) above cannot see. Raised in review of #522.
		also := make([]map[string]string, 0, len(decls))
		for _, d := range decls {
			also = append(also, d.Attrs)
		}
		return mintDeclPrefix(attrs, also), false
	}
	for _, d := range decls {
		if p, ok := declBinding(d.Attrs); !ok || p != prefix {
			return prefix, false
		}
	}
	return prefix, true
}

// spentElsewhere reports whether any declaration binds p to something
// OTHER than the declaration namespace — a value namespace the document
// may be resolving {{p:Thing}} through, which this editor may not
// reclaim. Raised in review of #522.
func spentElsewhere(p string, decls []*node) bool {
	for _, d := range decls {
		if v, ok := d.Attrs["xmlns:"+p]; ok && v != markup.XNamespace {
			return true
		}
	}
	return false
}

// withDeclBinding is attrs plus the envelope's binding for
// markup.XNamespace. A copy, because attrs is the editor's own envAttrs
// and writing the binding into it would make the next save look as
// though the file had always carried one.
func withDeclBinding(attrs map[string]string, prefix string) map[string]string {
	out := make(map[string]string, len(attrs)+1)
	for k, v := range attrs {
		out[k] = v
	}
	out["xmlns:"+prefix] = markup.XNamespace
	return out
}

// declAttrs is a declaration's attributes with every binding of
// markup.XNamespace that no longer names it removed: a default xmlns,
// and any prefixed binding other than the one it is being written under.
//
// A declaration is re-emitted PREFIXED, so a default binding it carried
// is no longer what names it — and left in place it also re-binds the
// default namespace for the declaration's own attributes, which is a
// different document from the one that was opened.
//
// The same sentence covers a PREFIXED binding other than the one being
// written: left in place it goes out as <q:Property … xmlns:p="…"> with
// p: naming nothing.
//
// AND THE THIRD CLAUSE IS DIFFERENT IN KIND. On the copy,
// xmlns:<prefix> must be ABSENT OR EQUAL to markup.XNamespace, whatever
// it used to say — not "dropped if it names the x namespace", which is
// the predicate that let a declaration carrying xmlns:<prefix> bound to
// something ELSE survive onto the element envelopeHead emits as
// <prefix:Property>, so the prefix resolved to the something else and
// the declaration stopped being one. That was a bug rather than a
// residue, and it is why the prefix mint in declPrefix is left alone:
// this clause closes both shapes that reach it.
//
// Returned as a copy, UNCONDITIONALLY, for the same reason as
// withDeclBinding: these attrs are ed.envDecls[i].Attrs, the editor's
// own document state, and a fast path that returned the caller's map
// when nothing needed dropping made the asserted copy conditional on
// the input. envelopeHead's `q := *d` aliases d.Kids and d.Slots the
// same way and is inert for the same reason — nothing writes through
// it. Stated here because that is the other place a future caller
// would assume a copy.
//
// The rounds that got to this shape, and what each earlier predicate
// did wrong, are in
// docs/specs/2026-08-10-markup-declared-properties.md. Raised in
// review of #522.
func declAttrs(attrs map[string]string, prefix string) map[string]string {
	dead := func(k, v string) bool {
		if k == "xmlns:"+prefix {
			// The one spelling that names the emitted element. Bound to
			// anything else it unnames it, so it goes whatever it says.
			return v != markup.XNamespace
		}
		if v != markup.XNamespace {
			return false
		}
		return k == "xmlns" || strings.HasPrefix(k, "xmlns:")
	}
	out := make(map[string]string, len(attrs))
	for k, v := range attrs {
		if !dead(k, v) {
			out[k] = v
		}
	}
	return out
}

// declBinding is the prefix this envelope binds to markup.XNamespace,
// and whether it already binds it — so a declaration is written back
// with the spelling its author chose, and the binding is written with
// it when there is none to find.
//
// THE BOOL IS THE HALF THAT WAS MISSING. This returned "x" as a
// fallback and called it unreachable, on the grounds that a document
// with declarations always carries the binding. That is true of the
// FILE and not of these attrs — but the document it was true of is not
// the one this paragraph named.
//
// It said "envelopeAttrs drops a namespace attribute the content root
// repeats". carryDeclarations skips v == markup.XNamespace, so the
// envelope's binding is never in the moved set and envelopeAttrs keeps
// it; envelopeParts' own paragraph carried the same false premise and
// was corrected in the same commit that left this one. The document
// where the binding really is absent here is the one whose declaration
// names the namespace as its own default xmlns. Raised in review of
// #522, twice — the second time for this copy.
//
// Over sortedKeys, not a range: a document binding two prefixes to the
// one namespace is legal and rare, and picking whichever the map handed
// back first would rewrite the file differently on different runs.
//
// IT RETURNS NO PREFIX WHEN IT FINDS NONE, and the minted spelling it
// used to hand back was observed by nobody. declPrefix calls
// mintDeclPrefix itself; every other caller either checks the bool or
// replaces the prefix with declFallbackPrefix. Measured in review of
// #522: returning "ZZZDEAD" when unbound left the whole apps/wysiwyg
// suite green, so this function's doc was where a reader learned a
// mint rule no caller here could reach.
//
// IT THEN DELEGATED THE SCAN, and the delegate carried the same defect
// one level down: declBindingAvoiding took an `also` argument that was
// nil at its only call site — this one — so the mint it computed on
// the unbound path was discarded by the only thing that could see it,
// and replacing that return with "MUTANT" left the whole apps/wysiwyg
// suite green a second time. The scan is the four lines below; the
// mint rule is on mintDeclPrefix, which is what declPrefix calls.
// Measured in review of #522.
func declBinding(attrs map[string]string) (string, bool) {
	for _, k := range sortedKeys(attrs) {
		if attrs[k] == markup.XNamespace && strings.HasPrefix(k, "xmlns:") {
			return strings.TrimPrefix(k, "xmlns:"), true
		}
	}
	return "", false
}

// declFallbackPrefix is what a MESSAGE says when the document binds the
// namespace nowhere: markup's own literal, which every example uses and
// which markup/property.go's <x:%s> refusal is spelled with.
//
// It is not a save-path answer and must not become one — declPrefix
// mints against what the document has already spent, and a message that
// named the mint would name a prefix the file does not contain.
const declFallbackPrefix = "x"

// declBindingOr is declBinding for the message sites: the document's own
// prefix where there is one, and fallback where there is not.
//
// TWO CALLERS, NOT THREE. Three sites spelled `if !bound { prefix = "x" }`
// by hand — the one-question-N-answers shape the declared-properties
// spec catalogues — and two of them route through here. alienDeclMsg
// still writes it out, because it is handed a prefix and a bool rather
// than an attrs map and has nothing to look the binding up in; what it
// shares with these two is declFallbackPrefix, which is the answer
// rather than the lookup. Raised in review of #522.
func declBindingOr(attrs map[string]string, fallback string) string {
	if p, ok := declBinding(attrs); ok {
		return p
	}
	return fallback
}

// mintDeclPrefix is the MINT ALONE, with no adopt scan in front of it,
// and `also` is the maps beyond the envelope that it must not collide
// with — because avoiding the envelope alone was not enough.
//
// markup's namespace table for value expressions is ONE FLAT
// DOCUMENT-WIDE MAP, so a prefix spent anywhere in the file is spent.
// The mint checked `attrs` — the envelope's own attributes — and
// declAttrs then drops `xmlns:<minted>` off the declaration whatever it
// used to say, so a document whose <Property> carried
// xmlns:x="urn:probe:handlers" was rewritten with that binding GONE and
// <Gooey xmlns:x="…/gooey/x"> in its place. Measured in review of #522:
// the editor reports the load error, ctrl+s is not gated on the build,
// and the file it writes no longer loads.
//
// The declarations' own attrs are the set that has to be added, and
// only them: a binding on the content root or below comes later in
// document order, so last-wins keeps it, and XML scoping keeps
// <x:Property> resolving against the envelope.
//
// "x" IS THE MINTED SPELLING, because every example uses it. x2, x3 …
// are the way out of the case where the document binds x to something
// else — legal, strange, and not worth clobbering the author over.
//
// THE ADOPT SCAN IS NOT IN FRONT OF IT because declPrefix has a case
// where that scan is exactly wrong: the envelope binds p to the
// declaration namespace and a declaration binds p to a VALUE
// namespace, so declPrefix declines p — and then called a function
// whose first loop found the envelope's p and handed it straight back.
// The decline was undone by the function it delegated to. That
// delegate is gone; declPrefix calls this directly. Measured in review
// of #522.
func mintDeclPrefix(attrs map[string]string, also []map[string]string) string {
	taken := func(p string) bool {
		if attrs["xmlns:"+p] != "" {
			return true
		}
		for _, m := range also {
			if m["xmlns:"+p] != "" {
				return true
			}
		}
		return false
	}
	p := "x"
	for i := 2; taken(p); i++ {
		p = "x" + strconv.Itoa(i)
	}
	return p
}

// gooeyOpen is the envelope's opening tag, carrying whatever the opened
// file wrote on it.
//
// ONE FUNCTION FOR THREE LITERALS. `"<Gooey>\n"` was spelled
// independently in ed.rebuild (twice) and in saveOpenFile, and nothing
// crossed them — which is the gap TestReopeningTheRebuiltSourceIsStable
// was added to close from the other end.
//
// attrValue like node.markup, because these attributes go back out the
// way every other attribute in this document does and the two must agree
// about quoting. They agreed on %q until review of #501, which is how
// they came to agree about being wrong.
//
// THAT PARAGRAPH IS envelopeHead'S NOW. All three call sites moved
// there when #517 gave the envelope declarations to write, so this
// function has exactly one caller and envelopeHead is where the three
// literals were collapsed. The comment kept saying otherwise because
// envelopeHead was inserted directly below this block with no blank
// line, which also made godoc read the whole of it as envelopeHead's
// doc and left gooeyOpen with none. Raised in review of #522.
func gooeyOpen(attrs map[string]string) string {
	var b strings.Builder
	b.WriteString("<Gooey")
	for _, k := range sortedKeys(attrs) {
		fmt.Fprintf(&b, " %s=%s", k, attrValue(attrs[k]))
	}
	b.WriteString(">\n")
	return b.String()
}

// splitDecls partitions an envelope's children into the property
// DECLARATIONS and the rest, on the same key markup's splitDeclarations
// uses (markup/property.go).
//
// ONE FUNCTION BECAUSE THERE ARE TWO READERS, which is carryDeclarations'
// argument three files over and applies unchanged: openWorkspaceFile
// needs both halves and pasteMarkup needs the declarations, and they had
// a copy of the split each. markup refuses any <x:Foo> that is not
// Property, so the day that predicate moves the editor would otherwise
// have two places to follow it to. Raised in review of #522.
//
// THREE WAYS OUT, BECAUSE markup'S SWITCH HAS THREE ARMS. The editor
// kept only two of them, and the missing one is the likely typo: a
// <Property> with no namespace at all. markup diagnoses it by name —
// "write it as <x:Property> and add xmlns:x=… to the <Gooey> root
// element" — and the editor counted it as a root element, so
// <Gooey><Property …/><Canvas/></Gooey> was refused with "needs exactly
// one root element, found 2" and the author was told to delete a root
// that was never there. That is #517's own shape, one case over.
//
// The one-kid spelling is no better, and the review that raised this
// assumed it was: <Gooey><Property …/></Gooey> passes the count, gets
// unwrapped, and the Build that follows sees the declaration INSIDE the
// editor's surface rather than on a root, so it answers "markup:
// unknown element <Property>". Measured both ways before this arm
// existed. Neither spelling reached markup's advice, so the editor has
// to give it. Raised in review of #522.
func splitDecls(n *node) (decls, kids, bare []*node) {
	for _, k := range n.Kids {
		switch {
		case k.Space == markup.XNamespace:
			decls = append(decls, k)
		case k.Elem == "Property":
			bare = append(bare, k)
		default:
			kids = append(kids, k)
		}
	}
	return decls, kids, bare
}

// alienDecls is every child splitDecls filed as a declaration that is
// NOT <x:Property> — the arm markup answers with "unknown language
// element".
//
// splitDecls keys on the NAMESPACE, exactly as markup's
// splitDeclarations does, so <x:Foo> lands in decls beside a real
// declaration. Every message built from that slice then described it as
// a declaration, and one of them spelled the element literally: a
// document holding <x:Foo/> was refused with "its 1 <x:Property>
// declaration is not a root element", naming an element the file does
// not contain and calling a thing markup rejects outright a declaration.
// That is the defect this branch exists to remove, one arm over. Raised
// in review of #522.
func alienDecls(decls []*node) []*node {
	var out []*node
	for _, d := range decls {
		if d.Elem != "Property" {
			out = append(out, d)
		}
	}
	return out
}

// declElemName is how one declaration is SPELLED in a message: its own
// xmlns binding if it carries one, otherwise the envelope's, otherwise
// bare.
//
// PER ELEMENT, because a document may bind more than one prefix to
// markup.XNamespace and XML scoping puts the binding wherever the author
// wrote it. alienDeclMsg has asked per element since review of #522;
// browser.go's root-count refusal read decls[0] and printed that
// spelling with len(decls), so a file holding one <p:Property> and one
// <q:Property> was told it held "2 <p:Property> declarations". This is
// that question asked once, in one place, so the two messages cannot
// drift again.
//
// A BARE <Property> IS THE ANSWER WHEN NOTHING BINDS THE NAMESPACE,
// and it is the file's own spelling rather than a guess: this path
// passes declSpelling an empty fallback, so an envelope that binds no
// prefix produces exactly what the document contains. The arm is
// pinned by TestTheRootCountRefusalSaysWhatItCounted's "no prefix
// bound" row.
//
// This paragraph argued the opposite until review of #522 — against
// spelling the element from "declBinding's fallback", which stopped
// existing when 995eae8 retired the unobserved mint and left
// declBinding returning ("", false). It was the site missed when
// alienDeclMsg's sibling paragraph got the same correction.
func declElemName(d *node, envelope map[string]string) string {
	p, ok := declBinding(envelope)
	return declSpelling(d, p, ok, "")
}

// declSpelling is the question itself, taking the envelope's answer
// already resolved because its two callers arrive with it in different
// shapes: declElemName has the envelope's attribute map, alienDeclMsg
// has the prefix and a bool it was handed.
//
// IT EXISTS BECAUSE THE CLAIM ABOVE WAS NOT TRUE. declElemName's doc
// said the spelling was "asked once, in one place, so the two messages
// cannot drift again" while alienDeclMsg still carried its own
// per-element loop and never called it — an invariant asserted in prose
// and not provided by the code, which is the shape this branch keeps
// removing (see declAttrs' doc). Now it is one function and the
// sentence holds.
//
// `unbound` IS A PARAMETER RATHER THAN A CONSTANT, and that is the
// deliberate difference the unification had to preserve rather than
// flatten. With no binding anywhere, the browser's refusal wants the
// bare <Property> — bareDeclMsg defines that spelling as the
// missing-namespace typo, so naming a prefix the file does not contain
// would be a guess. The alien refusal wants <x:Foo>, because it has to
// match markup's own <x:%s> message, which
// TestAnEnvelopeInTheXNamespaceGetsTheAlienRefusal pins. Both answer
// the same question; they differ only in what to say when nothing
// answers it. Raised in review of #522.
//
// AND THE PARAMETER WAS DEAD WHILE THAT PARAGRAPH STOOD. Both callers
// passed "", and alienDeclMsg got its <x:Foo> by normalising its own
// prefix to "x" and then telling this function envBound was true — so
// the arm below was statically unreachable and the difference was
// preserved somewhere other than where it was documented. alienDeclMsg
// now threads its real bool and names declFallbackPrefix here. Raised
// in review of #522, again.
func declSpelling(d *node, envPrefix string, envBound bool, unbound string) string {
	if p, ok := declBinding(d.Attrs); ok {
		return "<" + p + ":" + d.Elem + ">"
	}
	if envBound {
		return "<" + envPrefix + ":" + d.Elem + ">"
	}
	if unbound != "" {
		return "<" + unbound + ":" + d.Elem + ">"
	}
	return "<" + d.Elem + ">"
}

// alienDeclMsg is markup's own answer for those elements, said by the
// editor for the reason bareDeclMsg gives: the author is looking here.
//
// DERIVED FROM markup.XNamespace, like its sibling, so the URI cannot
// drift from the one splitDeclarations compares against.
//
// bound IS THE AUTHOR'S PREFIX OR NOBODY'S. When the document binds the
// namespace, the editor can do better than markup — which spells the
// refusal <x:%s> whatever the file says (markup/property.go) — and name
// the elements the way the author wrote them. When it does not, the
// prefix in hand is declBinding's MINTED one, and writing it named a
// prefix the file does not contain: a declaration bound by its own
// default xmlns under an envelope binding xmlns:x elsewhere was refused
// with "<x2:Foo> … declares <x2:Property> only", sending the author to
// look for an element nobody had written and to invent a prefix to fix
// it with. Unbound, this says exactly what markup says. That is the
// same rule the root-count refusal states at length one file over
// (browser.go), which this arm had stopped one short of.
//
// There used to be an `if prefix == "" { prefix = "x" }` here, which
// could not fire at the time — declBinding returned its minted "x"
// rather than "" — and read as the handling that was in fact absent.
//
// BOTH HALVES OF THAT SENTENCE HAVE SINCE TURNED OVER, and leaving it
// would tell a reader the guard three lines below is dead code.
// declBinding returns ("", false) when it finds no binding now, and
// alienDeclMsg carries the real handling: declSpelling is given the
// true `bound` so its own unbound arm can fire, and the TAIL — a
// statement about what the namespace offers rather than about this
// element — is normalised to declFallbackPrefix afterwards. Raised in
// review of #522, twice.
//
// AND THE PREFIX IS A PER-ELEMENT QUESTION, which is why this takes the
// nodes rather than their names. Both call sites hand it the ENVELOPE's
// binding, and XML scoping lets the binding sit on the element: a file
// binding xmlns:x on the envelope and writing <d:Foo> with its own
// xmlns:d was refused as <x:Foo>. That is worse than the unbound case
// above rather than the same size — x: IS bound in that file, so the
// message reads as a quote from the document and the author goes looking
// for an x:Foo nobody wrote. The element's own binding wins; the
// envelope's is the fallback, and markup's literal "x" the fallback for
// that. Raised in review of #522.
//
// THE TAIL STAYS ON THE ENVELOPE'S PREFIX, deliberately: "the …
// namespace declares <x:Property> only" is a statement about what the
// namespace offers, not about what this element is called, and the
// spelling an author would write a Property under is the document's
// binding rather than the alien element's.
//
// WHICH SAYS WHICH SPELLING WINS, NOT WHAT TO DO WITH NO ENVELOPE
// BINDING AT ALL. The tail fell straight to declFallbackPrefix there,
// so a file binding the namespace only on the element — legal, and the
// placement declPrefix exists for — was told the namespace "declares
// <x:Property> only" while containing no x: anywhere. That is the same
// invent-a-prefix harm the element half two paragraphs up was fixed
// for, one step further out, and it reached the paste refusal too. The
// ladder is now the same three steps declSpelling walks: the envelope's
// binding, then the first alien element's, then markup's literal.
// Raised in review of #522.
func alienDeclMsg(alien []*node, prefix string, bound bool) string {
	elems := make([]string, len(alien))
	for i, d := range alien {
		elems[i] = declSpelling(d, prefix, bound, declFallbackPrefix)
	}
	if !bound {
		prefix = declFallbackPrefix
		for _, d := range alien {
			if p, ok := declBinding(d.Attrs); ok {
				prefix = p
				break
			}
		}
	}
	verb := "is an unknown language element"
	if len(elems) > 1 {
		verb = "are unknown language elements"
	}
	return strings.Join(elems, ", ") + " " + verb + "; the " + markup.XNamespace +
		" namespace declares <" + prefix + ":Property> only"
}

// bareDeclMsg is what markup's splitDeclarations says about an
// unprefixed <Property>, said by the editor because the editor is where
// the author is looking.
//
// DERIVED FROM markup.XNamespace rather than spelled, so the URI cannot
// drift from the one the loader compares against;
// TestTheEditorSaysWhatMarkupWouldAboutABareProperty pins the rest of
// the sentence against markup's own error for the same document rather
// than against a copy of it.
func bareDeclMsg(n int) string {
	if n == 1 {
		return "<Property> is a dependency property declaration; write it as " +
			"<x:Property> and add xmlns:x=\"" + markup.XNamespace +
			"\" to the <Gooey> root element"
	}
	return "these " + strconv.Itoa(n) + " <Property> elements are dependency " +
		"property declarations; write them as <x:Property> and add xmlns:x=\"" +
		markup.XNamespace + "\" to the <Gooey> root element"
}

// nodeOf parses markup into the editor's document model — a palette
// seed's, and since #472 a USER'S DOCUMENT too.
//
// It reads for openWorkspaceFile (browser.go) and for pasteMarkup
// (clipboard.go), which is why its strictness and its error wording
// are a user-facing surface rather than an internal check on this
// repo's own seeds: it is now the load-bearing reader for namespace
// declarations in files somebody else wrote. The doc below described
// only the seed half until review of #501, and the namespaced-attribute
// error said "seeds are plain markup" to a user who had just opened a
// file.
//
// markup.Seeded answers "what should a NEW <X> be" in MARKUP, because
// the answer has to cover more than attributes — an empty <VStack>
// measures nothing whatever its attributes say. The editor's model is a
// node tree, so something has to bridge the two, and this is the
// smallest thing that can: the inverse of node.markup, over the subset
// of XML a seed can contain.
//
// It is deliberately strict. A seed is markup this repo authored, so
// anything surprising in one is a bug in the seed and must surface as
// an error the palette can show — not as a node tree that quietly
// dropped half of it, which is the failure mode the whole catalog
// effort exists to delete. The strictness is right for a user's file
// too; only the WORDING had to change. EVERY refusal this function had
// began "seed …" — which is this repo's word for its own palette markup
// and means nothing to someone who just opened a document — "seed does
// not parse" for a file the user wrote. They now name the thing that is
// wrong and leave the noun to the caller, which already supplies one:
// the browser prefixes the path, paste prefixes "pasted text is not
// markup", and the palette prefixes "<Button>". The prefixed-element
// refusal below is new on this branch and never carried the word.
//
// Stated as a property rather than counted: this said "five of the six
// refusals below", and `git show origin/main:apps/wysiwyg/main.go |
// grep -c 'fmt.Errorf("seed'` answers six, while the set below is now
// seven. Raised in review of #501, twice.
func nodeOf(src string) (*node, error) {
	dec := xml.NewDecoder(strings.NewReader(src))
	var stack []*node
	// The DEFAULT namespace in scope, one entry per open element. It is
	// what makes the prefixed-element refusal below possible at all: see
	// there.
	defaults := []string{""}
	var root *node
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("markup does not parse: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			// Read BEFORE the element's own name is judged, because a
			// default declared on an element applies to that element:
			// `<Gooey xmlns="U">` gives Gooey itself Space "U".
			def := defaults[len(defaults)-1]
			for _, a := range t.Attr {
				if a.Name.Local == "xmlns" && a.Name.Space == "" {
					def = a.Value
				}
			}
			defaults = append(defaults, def)
			// A PREFIXED ELEMENT IS REFUSED, the way a prefixed
			// attribute is twenty lines down, and the asymmetry was the
			// one shape that got accepted anyway under a different name:
			// `<t:Thing/>` became a node called `Thing`, measured, with
			// nothing said. The model cannot write a prefix back out
			// either way — node.markup writes n.Elem — so accepting one
			// is the silent drop this whole reader exists to delete.
			// What made it consequential rather than theoretical is
			// carryDeclarations: it now KEEPS `xmlns:x` on the envelope,
			// so a paste produced `<Thing xmlns:t="urn:a"/>` — the
			// declaration preserved and the prefix it scoped discarded.
			// And once #517/#522 lifts the two-roots refusal, an
			// `<x:Property>` read through here becomes a node named
			// `Property`, which saveOpenFile writes as `<Property>` —
			// the exact spelling splitDeclarations' `c.Name ==
			// "Property"` arm (markup/property.go) refuses.
			// Raised in review of #501.
			//
			// IT ALSO CHANGES WHICH REFUSAL FIRES TODAY, on files in
			// this tree, and the paragraph above read as though the
			// consequence were entirely forward-looking. nodeOf runs
			// BEFORE openWorkspaceFile's `len(n.Kids) != 1` check
			// (browser.go), so every document here that declares a
			// dependency property now gets this message instead of "a
			// <Gooey> document needs exactly one root element, found N".
			// Which documents those are is a grep, not a list —
			//
			//	git grep -l "<x:Property" -- "*.gooey"
			//
			// — and both messages refuse a document the loader accepts,
			// so this is a wording change rather than a regression:
			// #517 is the refusal itself and predates this branch. What
			// is worth recording is that the new wording reads as a
			// fault in the author's file where the old one read as the
			// designer's own gap, and that the window is the life of
			// this branch: #522's markup.XNamespace exemption removes
			// it. Raised in review of #501, round after.
			//
			// THE TEST IS NOT `t.Name.Space != ""`, and that is the
			// whole reason this needs a scope stack. encoding/xml
			// RESOLVES a name to its URI and hands back no prefix, so
			// under a default declaration every element carries one:
			// `<Gooey xmlns="wonderforge.io/gooey/2026">` gives Gooey,
			// Canvas and Button all Space "wonderforge.io/gooey/2026" —
			// measured, and refusing on non-empty Space would turn away
			// every document this editor has ever opened. A prefix is
			// visible only as a DIFFERENCE from the default in scope.
			//
			// The residue: a prefix bound to the same URI as the default
			// (`xmlns:g="U"` under `xmlns="U"`, then `<g:Canvas/>`)
			// passes and is written back as `<Canvas/>`. That is not a
			// gap being tolerated — by XML namespaces the two ARE the
			// same element name, so the rewrite preserves meaning. It is
			// only the spelling that moves, and no spelling survives a
			// document model that holds none.
			//
			// ONE NAMESPACE IS EXEMPT, AND ONLY AS A CHILD OF THE
			// ENVELOPE. The refusal's premise is that the model cannot
			// hold the namespace, so writing the element back out
			// renames it. What answers that premise is envelopeHead
			// re-deriving the prefix from declPrefix — and that exists
			// for the envelope's own children and nothing else. Below
			// the content root node.markup writes n.Elem and the prefix
			// is gone, exactly as node.Space's own doc says.
			//
			// NOT WIDENED TO "any namespace node.Space can hold", which
			// is every namespace: Space is set on every element and
			// DROPPED on write for everything but a declaration.
			//
			// AND THE WIDENING THAT MATTERS IS POSITIONAL, NOT BY URI,
			// which the paragraph above claimed to have ruled out while
			// applying the exemption at every DEPTH. Measured on a file
			// with an <x:Property> under the content root: the document
			// OPENED, with a build error — and saveOpenFile is not gated
			// on the build while canSave gates on openPath, which the
			// open had set. So ctrl+s on a file the editor was reporting
			// an error for rewrote it to <Property> on disk, losing the
			// prefix. Before this branch nodeOf refused the open and
			// nothing could rewrite anything, so the exemption made a
			// silent data loss out of a refusal. Raised in review of
			// #522.
			//
			// len(stack) == 1 is the test because the envelope is on the
			// stack and this element is not yet: a direct child of
			// <Gooey>, which is where a declaration lives and the only
			// place the save path can put its prefix back.
			//
			// AND len(stack) == 0 — the parsed root — because that is
			// PASTE's shape, where the node is one element with no
			// envelope and bareDeclWhy owns the refusal with a message
			// that says what a declaration is. Refusing here instead
			// replaced it with "element … is namespaced", which is true
			// and useless to someone who copied an <x:Property> out of
			// a document. Nothing writes such a node back: pasteMarkup
			// refuses it before insertSubtree.
			//
			// That leaves ONE way a root-position declaration reaches
			// disk — a FILE whose root element is one — and
			// openWorkspaceFile refuses that directly, because measuring
			// it here is what found it: the file opened, was wrapped in
			// a <Gooey>, and saved as <Property> with the prefix gone.
			// See the guard there. Raised in review of #522.
			//
			// AND THE ROOT ARM ASKS THE ELEMENT NAME, which it did not:
			// it exempted every x-namespaced element in root position,
			// so a file rooted at <x:Gooey> had no arm anywhere. Its
			// children are unprefixed, so splitDecls files them as kids
			// and alienDecls never fires; openWorkspaceFile's own guard
			// excludes n.Elem == "Gooey" on the grounds that the
			// default-xmlns shape has an answer of its own, which the
			// PREFIXED shape does not reach. The editor reported
			// "✓ builds" and ctrl+s rewrote the root element's resolved
			// namespace on disk — markup.Build accepts both forms,
			// since its root check is on the LOCAL name, so nothing
			// downstream stops it either.
			//
			// THE ARM EXCLUDES Gooey AND NOTHING ELSE, deliberately:
			// <x:Property> and <x:Foo> in root position both have
			// answers already — bareDeclWhy's two arms — and reaching
			// them requires nodeOf to hand the node back rather than
			// refuse it here. Narrowing to Property alone takes the
			// alien answer away from <x:Foo> and hands it this generic
			// sentence instead, which
			// TestADeclarationOutsideTheEnvelopeIsRefusedBeforeItCanBeSaved
			// and TestAPastedAlienElementKeepsItsOwnPrefix both catch.
			// Measured in review of #522.
			envelopeChild := (len(stack) == 0 && t.Name.Local != "Gooey") ||
				(len(stack) == 1 && stack[0].Elem == "Gooey")
			if t.Name.Space != def && !(envelopeChild && t.Name.Space == markup.XNamespace) {
				return nil, fmt.Errorf("element %q is namespaced, and the designer's document model holds only plain element names; it would be written back out as <%s>, which is a different element", namespacedAttrName(t.Name), t.Name.Local)
			}
			n := &node{Elem: t.Name.Local, Space: t.Name.Space, Attrs: map[string]string{}}
			for _, a := range t.Attr {
				// A NAMESPACE DECLARATION IS KEPT, AS AN ORDINARY
				// ATTRIBUTE, and that spelling is the whole fix for
				// #472 rather than an implementation detail.
				//
				// node.markup already writes every entry in Attrs back
				// out verbatim, so a declaration that survives the read
				// survives the write, the save, and an edit made
				// through the properties pane. What the pane cannot do
				// is ADD one: valueEditor.Write (properties.go) only
				// ever writes p.name, and p.name comes from the
				// element's DECLARED attributes, so there is nowhere to
				// type a free-form attribute name — that is #500. This
				// comment claimed the opposite until review of #501,
				// contradicting both the PR body and
				// doccontext_test.go's own note, in a repo that treats
				// a comment as a claim under test.
				//
				// Dropping it here was the only end that leaked: a
				// document opened through the file browser lost its
				// prefixes before it became a node tree, and the canvas
				// then refused the handler expression it had just read
				// with "undeclared namespace prefix".
				//
				// KEPT ON THE ELEMENT THAT DECLARED IT rather than
				// hoisted to a document-wide list. The reason is NOT
				// XML subtree scoping, which this comment used to
				// claim: markup.parse keeps ONE FLAT, document-wide ns
				// map and merges every declaration into it in document
				// order (the `a.Name.Space == "xmlns"` arm of
				// markup.parse's attribute loop), so a redeclared prefix
				// wins by being parsed later rather than by being
				// inner, and two sibling subtrees cannot bind one
				// prefix to two URIs. The outcomes coincide for every
				// shape this editor writes — the envelope is always
				// parsed before its child, so "child wins" and "last
				// wins" agree — and keeping the declaration where the
				// author put it is what makes the editor round-trip the
				// document rather than rewrite it. The envelope is the
				// one place that is not a node, and openWorkspaceFile
				// carries its declarations down.
				if a.Name.Space == "xmlns" {
					n.Attrs["xmlns:"+a.Name.Local] = a.Value
					continue
				}
				if a.Name.Local == "xmlns" && a.Name.Space == "" {
					n.Attrs["xmlns"] = a.Value
					continue
				}
				// A PREFIXED ATTRIBUTE IS REFUSED, which is the same
				// answer markup's own parser gives —
				// markup.namespacedAttrError. Neither side drops it and
				// neither keys by Local.
				//
				// BY SYMBOL, and the "defined at :993" half this
				// replaces is why the paragraph below insists on it:
				// that number was the first line of the doc block the
				// same commit added above the function, so the citation
				// was stale in the commit that wrote it. Raised in
				// review of #501.
				//
				// This comment said "the namespace is dropped by the
				// same key-by-Local rule markup's own parser uses" until
				// review of #501, and both halves of that were false,
				// inside the very loop the commit above it rewrote for
				// claiming things the code does not do.
				//
				// Nothing authored here has a prefixed attribute, so one
				// appearing is worth failing on rather than accepting
				// into a model that cannot write it back out.
				if a.Name.Space != "" {
					// NAMED THE WAY markup NAMES IT, which is two
					// spellings and not one. markup.namespacedAttrError
					// writes {uri}local for an ordinary prefix and
					// `xml:local` for the XML namespace, because that
					// one is bound by the spec rather than declared and
					// an author who wrote `xml:space` would not
					// recognise it back as
					// {http://www.w3.org/XML/1998/namespace}space.
					//
					// The claim above — that this is the same answer
					// markup's parser gives — is what makes the
					// difference a defect rather than a variation: it
					// formatted a.Name.Local alone until review of #501,
					// telling the author `attribute "Thing" is
					// namespaced` with no way to tell WHICH prefix, and
					// then formatted {uri}local for every case, which is
					// the wrong half of markup's rule for xml:*.
					return nil, fmt.Errorf("attribute %q is namespaced, and the designer's document model holds only plain attributes; markup's own loader refuses these too", namespacedAttrName(a.Name))
				}
				n.Attrs[a.Name.Local] = a.Value
			}
			stack = append(stack, n)
		case xml.CharData:
			if len(stack) > 0 {
				stack[len(stack)-1].Body += string(t)
			}
		case xml.EndElement:
			if len(defaults) > 1 {
				defaults = defaults[:len(defaults)-1]
			}
			if len(stack) == 0 {
				return nil, fmt.Errorf("unbalanced </%s>", t.Name.Local)
			}
			n := stack[len(stack)-1]
			// stack is a LOCAL parse stack whose last reference dies
			// with this function. The high-water mark it leaves behind
			// is freed with the slice itself at return, so there is
			// nothing for a clear to release — unlike the reused FIELDS
			// this guard is about.
			//
			// NOT SPELLED AS THE `retains nothing:` ESCAPE: the guard
			// skips a local before it reads an escape, so the marker
			// was inert here — measured in review of #456, where
			// replacing its text left the guard green. An inert marker
			// with semantics is worse than none, because it arrives
			// already-exempt the day the slice becomes a field.
			// Raised in review of #456.
			stack = stack[:len(stack)-1]
			// The SAME body rule the loader applies, called through the
			// package that owns it rather than restated here. A seed's
			// one-line body is verbatim, so restating this as a
			// TrimSpace would make the editor's idea of <Text>Text</Text>
			// differ from the loader's — silently, and only for bodies
			// with incidental whitespace.
			n.Body = markup.BodyText(n.Body)
			if len(stack) == 0 {
				root = n
				continue
			}
			p := stack[len(stack)-1]
			// A dotted name is a property element — <ItemsView.ItemTemplate>
			// — which is a structured attribute, not a child.
			if owner, slot, ok := strings.Cut(n.Elem, "."); ok {
				if owner != p.Elem {
					return nil, fmt.Errorf("<%s> is inside <%s>, and a property element belongs to the element it names", n.Elem, p.Elem)
				}
				if p.Slots == nil {
					p.Slots = map[string]*node{}
				}
				if len(n.Kids) != 1 {
					return nil, fmt.Errorf("slot <%s> needs exactly one child, got %d", n.Elem, len(n.Kids))
				}
				p.Slots[slot] = n.Kids[0]
				continue
			}
			p.Kids = append(p.Kids, n)
		}
	}
	if root == nil {
		return nil, fmt.Errorf("no root element")
	}
	return root, nil
}

// namespacedAttrName spells a namespaced attribute the way
// markup.namespacedAttrError does, so the designer and the loader name
// the same attribute the same way in their refusals.
//
// nodeOf's prefixed-ELEMENT refusal calls it too, for the same spelling
// rather than a second one. The `xml:` arm cannot fire from there — the
// XML namespace is bound to attributes, and no element of this
// vocabulary is in it — so that call always takes the {uri}local path.
// The guard below pins the attribute halves, which are the ones markup
// has an opinion about.
//
// A SECOND COPY OF ONE RULE, deliberately: the markup package does not
// export it, and apps/wysiwyg is a nested module that cannot reach into
// it. TestTheDesignerNamesANamespacedAttributeLikeMarkupDoes is what
// keeps the two in step — it builds both refusals for the same
// attribute and requires this spelling inside markup's message.
//
// AND THAT GUARD IS HERE, where CI vets without running, so it cannot
// see the copy it watches drift first: markup is upstream, and it could
// change its spelling with every check green. The half that runs in CI
// is markup's own TestNamespacedAttributesAreLoadErrors, whose arms
// spell out both forms, and markup.namespacedAttrError's comment names
// this function as what moves with it. Raised in review of #501.
func namespacedAttrName(n xml.Name) string {
	if n.Space == "http://www.w3.org/XML/1998/namespace" {
		return "xml:" + n.Local
	}
	return "{" + n.Space + "}" + n.Local
}

// ---- the editor ----

type editor struct {
	// The grid-overlay probe memo. guideProbes/guideHits are counters
	// rather than debug cruft: "the overlay costs nothing on an idle
	// frame" is a claim about how often probeUncached RUNS, and a damage
	// count cannot see it — a re-probe that produces the same rects
	// repaints nothing and is invisible to every effect-level
	// instrument. TestTheGuideProbeIsNotRepeatedOnAnIdleFrame reads them.
	guideKey    string
	guideCells  [][]gooey.Rect
	guideProbes int
	guideHits   int
	// ctx builds the EDITOR; docCtx is the vocabulary the DOCUMENT is
	// authored in. See newEditor for why they are separate.
	ctx    *markup.Context
	docCtx *markup.Context

	root *node
	// sel is the selected node, or NIL FOR NOTHING SELECTED — which is a
	// real state rather than a missing value, and is what a press on bare
	// canvas produces.
	//
	// It is a node POINTER and not an index, and that is the change that
	// made the empty state cheap rather than a sentinel. An int index into
	// root.Kids can name a top-level child and nothing else: not "nothing",
	// which it has to spell as a magic -1 or -2, and not anything nested,
	// which the design surface will need the moment the user drops a
	// container and expects to select what is inside it. A *node names any
	// node at any depth, survives its siblings being reordered or deleted,
	// and survives the node being renamed — none of which an index does.
	sel *node

	palette    []markup.ElementSpec
	paletteSel *prop.Property[int]
	attrSel    *prop.Property[int]
	// themeDark is the theme switch, and iconTint is derived from it.
	//
	// A BOOL AND A COMPUTED rather than one colour property, because the
	// two say different things and the difference is the point of the
	// acceptance test. themeDark is what a user flips; iconTint is what
	// the icons read. Nothing else in the editor reads either, so a flip
	// dirties exactly the icon handles and through them exactly the
	// <Image> in each realized palette row — see
	// TestThemeFlipRepaintsOnlyTheToolboxIcons, which counts them.
	//
	// A colour baked into a raster at load would have been simpler and
	// silently wrong: the icons would keep their first tint forever and
	// nothing in the framework reports a picture that stopped changing.
	themeDark *prop.Property[bool]
	iconTint  *prop.Property[color.Color]
	// icons resolves the icon NAMES the catalog declares into the tinted
	// pictures the palette binds. The names are markup.ElementSpec.Icon;
	// the assets, the rasterizer and the colour all live app-side, which
	// is what keeps imagefmt/svg out of core's dependency graph.
	icons *toolbox.Icons
	// iconErr is a load-time asset failure, reported by main rather than
	// here: newEditor is called by a dozen tests and growing an error
	// return would rewrite all of them for a condition none of them can
	// hit. A paint cannot report a missing icon, so this is the only
	// place the failure can be stated.
	iconErr error
	// activitySel is which icon on the rail is lit. It is an ordinary
	// source property like any other selection — the rail happens to be
	// drawn in pixels, which changes how it is painted and nothing about
	// how it is bound.
	activitySel *prop.Property[int]

	// The documentation tree, and the page being read. docsRoot is an
	// fs.FS so dev and release differ only in what is passed here — see
	// docs.go, which also says why embedding is not the one-line change
	// #288 assumed.
	//
	// docList is the LIST, walked once rather than per frame: a Render
	// that walked a directory would put filesystem latency inside a
	// paint.
	//
	// IT IS A SOURCE PROPERTY, NOT A SLICE FIELD, and that is the fix for
	// a defect the review of PR #426 found rather than a preference.
	// docsItems closed over a plain field, so the computed read nothing
	// observable — it cached on first evaluation and could never be
	// invalidated by anything. Today the list is written once, which
	// makes that invisible; the moment anything refreshes it (a watcher,
	// a -workspace change) the pane would silently keep the old list.
	// Going through a property means "the list changed" is expressible,
	// and the only reason the tests' post-construction override worked
	// was that nothing had evaluated the computed yet.
	//
	// docsRoot and docsSkipped are SOURCE PROPERTIES for the same reason,
	// and the review of #426 found them sitting under that paragraph
	// still plain: docsBody reads both, so a refresh that changed either
	// without also writing docList would invalidate nothing. That every
	// refresh happens to write all three today was a coupling nobody had
	// written down, which is what the docsItems defect was made of.
	//
	// IT IS WRITTEN DOWN NOW, AND ENFORCED (#442): setDocsTree in docs.go
	// is the only writer of the three, and TestTheDocsTreeHasOneWriter
	// derives that from the source rather than asking anyone to keep the
	// rule in mind. A second writer is a red test naming the function it
	// found. Do not Set these directly — go through setDocsTree, which
	// is what makes "the tree changed" one event rather than three.
	docsRoot    *prop.Property[fs.FS]
	docList     *prop.Property[[]docPage]
	docsSkipped *prop.Property[int]
	docsItems   *prop.Property[components.ItemSource]
	docsSel     *prop.Property[int]
	docsBody    *prop.Property[string]

	// Bindable surface.
	paletteItems *prop.Property[components.ItemSource]
	attrItems    *prop.Property[components.ItemSource]
	source       *prop.Property[string]
	status       *prop.Property[string]
	// dragHint is what a REFUSED drag says, and statusText is the one
	// string the status bar's left section actually shows: the hint when
	// there is one, the build status otherwise.
	//
	// Two sources and a computed rather than one property both writers
	// share, because they answer different questions and the shared
	// property loses one of them. "✓ builds" is a standing claim about
	// the document; "you cannot move this, and here is why" is about the
	// gesture that just happened. Writing the hint into ed.status would
	// discard the build state, and the user would have to make an edit to
	// find out whether their document still loads.
	//
	// The hint is cleared by any drag that proceeds and by any rebuild,
	// so the build status comes back on its own.
	dragHint   *prop.Property[string]
	statusText *prop.Property[string]
	editName   *prop.Property[string]
	editValue  *prop.Property[string]
	editDoc    *prop.Property[string]
	treeText   *prop.Property[string]
	// fits is false while the terminal is too small to lay the shell out,
	// and it drives Visibility on both roots. cramped is its inverse, and
	// it is a computed rather than a second source: two sources for one
	// fact drift, and the frame where they disagree shows either both
	// roots or neither.
	fits    *prop.Property[bool]
	cramped *prop.Property[bool]
	fitMsg  *prop.Property[string]
	// design is the mode switch, and it is the editor's first consumer of
	// gooey.Frozen. True (the default) means the designer pane is a
	// PICTURE: the document lays out and paints exactly as it will, and
	// nothing in it is tabbable, clickable or running. False hands the
	// document back its behaviour so you can try what you just built.
	//
	// It is one source property read from two places — preview.Pane.Frozen
	// and modeText — which is what makes the flip land in the same frame as
	// the keystroke. Nothing else is needed: the framework observes the
	// read Frozen() makes and re-syncs the composition.
	//
	// Freezing by default also fixes something the editor had wrong before
	// there was a switch at all. Every focusable component in the DOCUMENT
	// was a focus stop in the EDITOR, so tab walked out of the shell and
	// into the thing being edited, and a document containing a <TextBox>
	// gave the user two carets with no way to tell which one the keyboard
	// was in.
	design   *prop.Property[bool]
	modeText *prop.Property[string]

	// rev ticks on every edit. The list sources are computed over the
	// document, which is plain Go state and therefore invisible to the
	// property graph — a computed that reads no property records no
	// dependency and caches forever. Reading rev is what gives them
	// something to invalidate on.
	rev *prop.Property[int]

	// props is the PROPERTIES pane's editing surface — the thing that
	// floats an editor over the selected inspector row. It is set when
	// the page is built (newValueEditor registers itself here), so it is
	// nil for an editor that has never mounted a page and every caller
	// checks. See properties.go.
	props *valueEditor

	// fsys is where the page and every pane's markup is read from.
	fsys fs.FS
	// art is the panel frame cache, ONE per app: it is keyed by size and
	// colour, so the shell's panels and any panel a document contains share
	// a raster instead of rasterizing the same frame twice.
	art *panel.Art

	pv  *preview.Pane
	app *gooey.App

	// hitTest is the framework's under-the-pointer query — since #465 the
	// component that PAINTS last where you clicked, not the deepest one.
	// docRoot / nodeOf are what make its answer mean something: the tree
	// rebuild BUILT for this document, and which design node every
	// component in it came from. Together they are click-to-select — see
	// select.go.
	//
	// Both are nil whenever the picture on screen is NOT this document: a
	// build that failed (the previous preview stays up, deliberately) or
	// remote mode (there is no local tree at all). Mapping a press against
	// a stale tree would select a neighbour of what was clicked, silently,
	// which is worse than selecting the container.
	//
	// nodeOf is keyed on the COMPONENT and valued with the *node pointer.
	// Not a Name, which the user can change or delete, and not a position,
	// which under a chrome-only design surface never reaches the saved
	// file at all.
	hitTest func(x, y int) gooey.Component
	docRoot gooey.Component
	nodeOf  map[gooey.Component]*node
	// compOf is nodeOf INVERTED, built in the same walk rather than
	// searched for afterwards.
	//
	// componentFor used to be a linear scan of nodeOf justified by "it
	// runs once per gesture rather than once per motion", and that had
	// stopped being true: buildGuide calls it TWICE and buildGuide runs
	// from Overlay.Arrange, which is every frame a grid is in scope. Two
	// O(n) scans per frame over every mapped component is not a cost
	// worth carrying for a map the same walk could fill in.
	//
	// The pairing is one component per node by construction — mapNodes
	// descends the two trees in lockstep and writes both directions at
	// the same point — so the inverse cannot disagree with nodeOf
	// without mapNodes being wrong about both.
	compOf map[*node]gooey.Component
	// pseudo is the set of element names that build no component of
	// their own, derived with the palette from one Catalog() read. See
	// loadPalette for why it is not asked per node, and pairAgrees for
	// what it answers.
	pseudo map[string]bool
	// specs is the catalog BY NAME, from the same read. It is what
	// specOf and target() answer from — see loadPalette. The catalog
	// itself is a rebuild, not a lookup, and both of those are on paths
	// that must not pay for one.
	specs map[string]markup.ElementSpec

	// drag is the move gesture in flight, and invalidateFn is what asks
	// for the frame it needs — see drag.go. invalidateFn is injected for
	// the same reason hitTest is: the tests drive Composer.Frame()
	// directly and have no *gooey.App.
	drag         dragState
	invalidateFn func()

	// cursor is the track the grid-structure verbs act on.
	//
	// Plain Go state, like the document itself — which is why moving it
	// needs an explicit App.Invalidate (see setCursor) exactly as
	// writing Layout.Left does. The overlay notices the change by
	// recomputing its guide during the frame that call schedules; it
	// does not subscribe to this.
	cursor trackCursor

	// serving names the control-plane endpoints this editor is listening
	// on, in the order they came up; serveInfo is the same thing bound
	// into the page. With no UI left, the address you would drive the
	// editor from is the only thing worth putting on screen.
	serving   []string
	serveInfo *prop.Property[string]

	// links is each endpoint's CONNECTION state, keyed by the label the
	// status bar shows. It lives on the editor rather than on the strip
	// because a hot reload builds a new strip against servers that never
	// restarted — see servelink.go, which owns the whole of this. Use
	// ed.link(label) rather than the map.
	links map[string]*endpointLink

	// addrs is the status bar's clickable endpoint strip, set by the
	// <ServeAddrs/> builder each time the page is built — a hot reload
	// makes a new one, so the global gestures resolve it per press
	// rather than capturing it. Nil until the page has been built, and
	// nil for good when no endpoint came up (see serveAddrsBuilder).
	addrs *addrStrip

	// remote, when set, means the editor is driving ANOTHER app: the
	// document is patched into that app's island instead of previewed
	// in this process. Nil is local mode.
	remote *remoteTarget

	// THE IDE SHELL — dock, workspace, and the region swap. See dock.go,
	// browser.go and menus.go; only the handles live here, because
	// Context.Values captures each one BY VALUE and a property created
	// after the map is populated leaves nil in the map.
	dock    *dockModel
	ws      *workspace
	pathBox gooey.Component

	// region is WHICH THING THE EDITOR AREA SHOWS, and codeView is which
	// code viewer it uses when that thing is code. Two questions, two
	// properties — but each is ONE property with several renderings, not
	// several properties kept in step. See menus.go.
	region   *prop.Property[int]
	codeView *prop.Property[int]

	builtinChecked *prop.Property[bool]
	editorChecked  *prop.Property[bool]
	designChecked  *prop.Property[bool]
	codeChecked    *prop.Property[bool]

	wsLabel  *prop.Property[string]
	wsPath   *prop.Property[string]
	wsQuery  *prop.Property[string]
	wsSel    *prop.Property[int]
	wsRev    *prop.Property[int]
	wsFiles  *prop.Property[components.ItemSource]
	openPath *prop.Property[string]

	// envAttrs is what the opened file's <Gooey> carried, minus the
	// prefixed namespace declarations carryDeclarations moves down onto
	// the document root.
	//
	// THE ENVELOPE IS NOT A NODE — gooeyOpen re-emits it as a literal
	// around ed.doc() — so anything written on it in the user's file
	// has nowhere in the document model to live and was simply dropped
	// on the first save. Graphics is the one that matters: it forces the
	// image protocol (markup.Graphics), apps/dynamic-activities/zoom.gooey
	// carries Graphics="halfblock", and opening that file in the designer
	// and saving it silently took the demo's graphics mode away under a
	// "✓ saved". Measured before the fix. Raised in review of #501.
	envAttrs map[string]string
	// envDecls are the <x:Property> declarations the opened file's
	// <Gooey> carried. They travel with envAttrs and with ed.root.Kids —
	// assigned at the one site that assigns those, for the reason
	// TestEnvAttrsIsAssignedWhereTheDocumentIs exists. Added for #517.
	envDecls []*node
	// seededDecls are the declared names seedDeclared last put into
	// ed.docCtx.Values, so the next rebuild can take exactly those back
	// out and no others. See seedDeclared for why the map is shared and
	// why that makes retirement this method's job. Added in review of
	// #522.
	seededDecls []string
	// shadowedDecls are the declared names seedDeclared could NOT seed
	// because the editor's own chrome already binds them. The skip is
	// right — see seedDeclared — but it is invisible in the preview, so
	// rebuild appends them to the build status. Raised in review of
	// #522.
	shadowedDecls []string

	// hist is the undo/redo stacks over the DOCUMENT MODEL. It is
	// recorded from rebuild rather than from each mutator, so a mutation
	// added later is undoable without opting in — see undo.go, which owns
	// everything about it including its lazy construction.
	hist *history

	// mu guards lost, which the stream reader writes and the UI reads.
	mu      sync.Mutex
	lost    error
	swapped bool

	// clip is the component clipboard: a DEEP COPY of a copied subtree,
	// never a pointer into the document. See clipboard.go.
	clip clipboard
}

// emptyDocsBody names WHICH empty this is, and that it has to is a fix
// from the review of PR #426. The pane keyed its message off an empty
// list alone, so three unrelated states — no docs/ tree anywhere, a
// docs/ tree holding no markdown, and a docs/ tree that could not be
// read — all told the reader the same thing, and for two of the three it
// was false. The tree in front of them existed.
//
// The skipped count is the same fix seen from docsPages' end: it is the
// difference between "there is nothing here" and "I could not look".
//
// One case is still NOT surfaced — a partially readable tree that DID
// yield pages says nothing about the ones it lost, because the pane is
// one string and that string is the page you selected. That wants a
// status line, which the docs tab does not have yet.
//
// A PLAIN FUNCTION TAKING WHAT IT NEEDS, and it has been three shapes
// across two review rounds because each one was wrong in a way the next
// exposed. A func field assigned during construction came first, and it
// worked only because docsBody's closure is lazy and nothing evaluated
// the computed before the assignment ran — the same accident-of-ordering
// the review found in docsItems. A method fixed that and introduced the
// next one: it read ed.docsRoot and ed.docsSkipped from inside an
// evaluating computed, so the dependency set depended on which branch of
// docsBody had run. Taking both as arguments settles it, because now
// every property read belongs to docsBody and is hoisted above its
// branch. See docsBody.
//
// TWO STATES, not three. A "Select a page." case sat below these until
// the review of #426: it could only be entered with a non-empty list,
// and docsBody now CLAMPS such an index to a real page rather than
// asking for a message, so nothing could reach it. A branch nothing can
// enter is a claim about behaviour that never happens.
func emptyDocsBody(root fs.FS, skipped int) string {
	skipnote := ""
	if skipped > 0 {
		unit := "entries"
		if skipped == 1 {
			unit = "entry"
		}
		skipnote = fmt.Sprintf(" %d %s could not be read.", skipped, unit)
	}
	if root == nil {
		return "No docs/ directory was found beside the editor." + skipnote
	}
	// UNREADABILITY LEADS, because with nothing read the emptiness is not
	// a fact this function has. "holds no markdown pages" is a claim
	// about what is there; when the walk skipped something — and when
	// the ROOT itself was unreadable, docsPages returns (nil, 1) — the
	// honest answer is that it could not look, and appending that after
	// the claim made the pane say both:
	//
	//	The docs/ directory beside the editor holds no markdown pages.
	//	1 entry could not be read.
	//
	// The first sentence is contradicted by the second, and this
	// function's whole purpose is the difference between "there is
	// nothing here" and "I could not look". docs_test.go asserted only
	// that the skip note appeared, so the false half shipped past the
	// test written for it. Found in review of #426.
	if skipped > 0 {
		unit, verb := "entries", "were"
		if skipped == 1 {
			unit, verb = "entry", "was"
		}
		return fmt.Sprintf("%d %s under the docs/ directory %s unreadable, "+
			"so what it holds is unknown.", skipped, unit, verb)
	}
	return "The docs/ directory beside the editor holds no markdown pages."
}

// newEditor takes the editor's root FS because the panes are markup on
// disk, not markup compiled in. One FS for the page and every control:
// os.DirFS in development, so editing a pane's .gooey hot reloads it, and
// the same line takes an embed.FS for a release build. That seam is the
// whole reason markup loading is an fs.FS rather than a path.
func newEditor(fsys fs.FS) *editor {
	ed := &editor{
		fsys: fsys,
		art:  panel.NewArt(),
		// THE SURFACE, then the user's document inside it. Two Canvases
		// that mean different things: the outer one is the editor's
		// workspace and is never saved, the inner one is the user's root
		// and is the whole of what a save writes. retype changes the
		// INNER one.
		root: &node{Elem: "Canvas", Attrs: map[string]string{"Name": "Surface"}, Kids: []*node{
			{Elem: "Canvas", Attrs: map[string]string{"Name": "Root"}, Kids: []*node{
				// The Text carries a BODY, for the same reason addSelected
				// seeds one: without it this element measures zero and the
				// document the editor opens with contains something the
				// user can neither see nor click.
				{Elem: "Text", Body: "T1", Attrs: map[string]string{"Name": "T1", "Canvas.Left": "2", "Canvas.Top": "1"}},
				{Elem: "Button", Attrs: map[string]string{"Name": "B1", "Content": "click", "Canvas.Left": "2", "Canvas.Top": "3"}},
			}},
		}},
		paletteSel:  prop.NewSource(0),
		attrSel:     prop.NewSource(0),
		activitySel: prop.NewSource(1), // the toolbox, which is what the side bar shows
		docsSel:     prop.NewSource(0),
		source:      prop.NewSource(""),
		status:      prop.NewSource(""),
		dragHint:    prop.NewSource(""),
		editName:    prop.NewSource(""),
		editValue:   prop.NewSource(""),
		editDoc:     prop.NewSource(""),
		treeText:    prop.NewSource(""),
		fits:        prop.NewSource(true),
		fitMsg:      prop.NewSource(""),
		design:      prop.NewSource(true),
		rev:         prop.NewSource(0),
		serveInfo:   prop.NewSource("no control plane: started with -serve \"\" -mcp \"\""),
		pv:          &preview.Pane{},
		dock:        newDockModel(),
		region:      prop.NewSource(regionDesign),
		codeView:    prop.NewSource(codeBuiltin),
		wsLabel:     prop.NewSource(""),
		wsPath:      prop.NewSource(""),
		wsQuery:     prop.NewSource(""),
		wsSel:       prop.NewSource(0),
		wsRev:       prop.NewSource(0),
		openPath:    prop.NewSource(""),
	}

	// Set after the literal because it points INTO it: the selection is a
	// node pointer, so it cannot be written in the same composite literal
	// that creates the node it names.
	ed.sel = ed.doc().Kids[0]

	ed.cramped = prop.NewComputed(func() bool { return !ed.fits.Get() })

	// Both Gets are HOISTED above the branch, because a dependency is
	// recorded by the Get that actually RUNS: leaving the ed.status.Get()
	// behind the `if` would drop it from the dependency set on every
	// frame a hint is showing, and the moment the hint cleared the status
	// bar would go deaf to the build status with no error anywhere.
	ed.statusText = prop.NewComputed(func() string {
		hint, build := ed.dragHint.Get(), ed.status.Get()
		// AN ERROR OUTRANKS A HINT, and the ordering is the whole point
		// rather than a preference. The hint is set BY THE PRESS, so a
		// user whose document has stopped building presses an element,
		// gets no selection, and that failed press installs a hint which
		// displaces the one line explaining why — the diagnostic is
		// destroyed by the act of hitting the problem. It cost an hour of
		// "i can't select any components on canvas" against a status bar
		// that had held `✗ markup: <Tabs> children must be <Tab>
		// elements` the entire time and never showed it once.
		if strings.HasPrefix(build, "✗") {
			return build
		}
		if hint != "" {
			return hint
		}
		return build
	})

	// The pane is the frozen host. Binding it here rather than at its
	// construction keeps the property and its two readers in one place.
	ed.pv.BindDesignMode(ed.design)

	// The design-time layout overlay, handed two things: the function
	// that builds the guide, and the design-mode property that gates the
	// whole thing off in LIVE mode.
	//
	// It is handed NO revision. Everything the overlay draws is derived
	// from plain Go state the property graph cannot see, so a Render
	// that only called the guide function would record no dependency and
	// go permanently deaf. The overlay solves that itself: it owns a
	// `rev` and bumps it from Arrange, but ONLY when the guide actually
	// changed — see the comment on Overlay.rev in
	// components/preview/overlay.go, which explains why the comparison
	// is what makes the frame terminate rather than an optimisation.
	//
	// That is the more interesting design and it belongs there rather
	// than here, because the caller cannot get it wrong.
	ed.pv.BindOverlay(preview.NewOverlay(ed.buildGuide, ed.design))

	// The status bar's centre, and it is the only cue the user gets that
	// clicking the designer will or will not do anything. A mode with no
	// indicator is a mode you find out about by being surprised.
	ed.modeText = prop.NewComputed(func() string {
		if ed.design.Get() {
			return ModeDesign
		}
		return ModeLive
	})

	// THE THEME, and the icon tint derived from it.
	//
	// Built before the palette's projection because that projection hands
	// out icon handles, and a handle created against a nil tint would
	// rasterize `currentColor` unsubstituted — which oksvg answers with
	// `param mismatch`, not a black glyph. The failure is loud, but it
	// would be loud at PAINT time, where nothing can report it.
	//
	// The Get is inside the computed, which is what subscribes it. Read
	// out here and closed over, the tint would be fixed for the life of
	// the process and the theme switch would silently do nothing.
	ed.themeDark = prop.NewSource(true)
	ed.iconTint = prop.NewComputed(func() color.Color {
		if ed.themeDark.Get() {
			return iconOnDark
		}
		return iconOnLight
	})
	ed.icons = toolbox.New(fsys, ed.iconTint)

	// The list sources are built BEFORE the context, because
	// Context.Values captures each handle BY VALUE: a property created
	// after the map is populated leaves nil in the map, the bindings
	// resolve to nothing, and the palette renders empty with no error
	// anywhere. Both computeds read ed.palette lazily, so it is fine
	// that the palette itself is filled further down.
	// THE DOCS TREE, resolved once. docsRoot is nil when there is none
	// beside the editor, which is a legal state the pane says out loud
	// rather than a startup failure — see docsFS.
	// The three handles are minted EMPTY and filled by setDocsTree, so
	// that function is the only writer from the first frame rather than
	// from the second — see docs.go for why one writer is the whole
	// point (#442). A construction that wrote the values directly would
	// be an exception to the rule on the line that establishes it.
	ed.docsRoot = prop.NewSource[fs.FS](nil)
	ed.docList = prop.NewSource[[]docPage](nil)
	ed.docsSkipped = prop.NewSource(0)
	ed.setDocsTree(docsFS())
	ed.docsItems = prop.NewComputed(func() components.ItemSource {
		return components.ItemsOf(ed.docList.Get(), func(d docPage) map[string]any {
			return map[string]any{"Name": d.Label, "Bar": "▌"}
		})
	})
	// docsBody READS THE FILE INSIDE AN EVALUATION, which breaks "no I/O
	// in a paint", and the review of PR #426 was right that the previous
	// version of this comment defended it on the wrong axis.
	//
	// It argued reportability — a paint cannot report an error, so
	// svg.IconSet needs Preload while docBody has the pane's one string
	// to put the failure in. That is true and it is not the objection.
	// THE RULE EXISTS FOR LATENCY: this runs on the UI goroutine while a
	// <Text> renders, so the read is synchronous inside the frame, and on
	// a cold cache or a network filesystem selecting a page stalls the
	// whole editor rather than just this pane.
	//
	// Accepted here, deliberately and with the bound stated: the read is
	// one local file of a few kilobytes, on a selection change rather
	// than per frame, and it happens off the render path for every other
	// pane. The framework's answer if that stops holding is written down
	// and is not this — do the read off the loop and Dispatcher.Post the
	// result into a source property, which also needs a sequence number
	// so a slow read of page A cannot land after a fast read of page B.
	//
	// THE READ IS NOT THE EXPENSIVE PART, and this comment used to imply
	// it was by answering the whole objection with "on a keystroke rather
	// than per frame". That is true of the read and false of what the
	// string then costs. Layout is UNCONDITIONAL — Composer measures and
	// arranges every frame, damage or no damage — so a <Text> holding a
	// whole markdown file is re-measured on every frame for as long as
	// the tab is open, not once per selection. markup-reference.md is
	// ~1600 lines; the specs tree has a page longer still.
	//
	// docsBodyMaxLines is the bound, and it is a stopgap with the real
	// fix named: a pane-local viewport (#67,
	// docs/specs/2026-08-23-scrolling.md). Until then nothing below the
	// clip is reachable by any gesture anyway, so measuring it buys the
	// reader nothing and costs them every frame.
	//
	// THERE IS NO CACHE, and its absence is the point. One used to sit
	// here keyed by path and never invalidated, which made the sentence
	// above it false: "a page deleted under the editor renders its own
	// read error" held only for a page never opened, and the common
	// path — read a page, then edit or delete it — served the stale body
	// forever. Re-reading costs one file read per selection change, which
	// is the same order as the read the cache was saving.
	ed.docsBody = prop.NewComputed(func() string {
		// HOISTED, ALL FOUR, above every branch. A Get behind an early
		// return drops out of the dependency set on the frames it does
		// not run, and the pane goes deaf to that property with no error
		// anywhere — the trap CLAUDE.md names and the one the two fields
		// promoted above were already an instance of.
		root := ed.docsRoot.Get()
		list := ed.docList.Get()
		i := ed.docsSel.Get()
		skipped := ed.docsSkipped.Get()
		if len(list) == 0 {
			return emptyDocsBody(root, skipped)
		}
		// CLAMPED, the way ItemsView.selection clamps its own read
		// (components/itemsview.go:472). The view and the pane read the
		// same index and must agree about what it means: the view clamps
		// and writes back only on a gesture, so a docList that refreshes
		// SHORTER leaves the list highlighting a row while an
		// out-of-range branch here rendered nothing — a blank pane that
		// is indistinguishable from a page with nothing in it, which is
		// the exact failure docBody's own comment argues against.
		//
		// A shorter list is not hypothetical: it is what docList was
		// promoted to a source property to support. Found in review of
		// #426.
		i = min(max(i, 0), len(list)-1)
		return clampLines(docBody(root, list[i].Path), docsBodyMaxLines)
	})

	ed.paletteItems = prop.NewComputed(func() components.ItemSource {
		ed.rev.Get() // hoisted above everything: the dependency is the point
		return components.ItemsOf(ed.palette, func(e markup.ElementSpec) map[string]any {
			return map[string]any{
				"Name":  e.Name,
				"Attrs": describeAttrs(e),
				"Kids":  string(e.Children.Mode),
				// THE ICON, straight off the catalog entry. An element
				// that declares none gets a nil handle, which ItemsView's
				// projection case tolerates and <Image> renders as
				// nothing — the honest answer, and the same rule
				// AttrsKnown follows: an absence must not be dressed up
				// as a default.
				//
				// The HANDLE is cached per name, so this projection hands
				// back the same property on every revision and the row's
				// picture stays damage-free by pointer compare rather
				// than by the raster cache happening to return the same
				// image.
				"Icon": ed.icons.For(e.Icon),
				// The selection marker's content. A template's context is
				// the ITEM and nothing else, so a constant every row
				// needs has to arrive as a projected value — the same
				// reason cmd/typeahead projects its "Bar".
				"Bar": "▌",
			}
		})
	})
	ed.attrItems = prop.NewComputed(func() components.ItemSource {
		// Hoisted ABOVE the rows, and above any early return: a
		// dependency is recorded by the Get that actually runs, so a
		// Get placed after a branch can be skipped and the subscription
		// silently lost.
		ed.rev.Get()
		return components.ItemsOf(ed.attrRows(), func(r attrRow) map[string]any {
			name := r.name
			if r.req {
				// Required is a property of the attribute, so it belongs
				// on the attribute rather than in a separate column that
				// is blank for nine rows in ten.
				name += "*"
			}
			// The modified-from-default marker. A leading glyph rather
			// than a colour, because the one colour cue on this pane
			// already means "loaded into the editor" and giving it a
			// second meaning is the collapse this project keeps
			// cataloguing.
			mark := " "
			if r.modified {
				mark = "•"
			}
			return map[string]any{
				"Mark": mark, "Name": name, "Kind": r.kind,
				"Legal": r.legal, "Value": r.value,
				// The EDITOR AFFORDANCE, derived from the Kind through
				// the per-Kind table in editors.go and never listed
				// here: "…" for a value you cannot type correctly from
				// memory, "▾" for a finite list, "⇕" for a number, and
				// a bang for a Kind nobody gave an editor.
				"More": rowAffordance(r),
			}
		})
	})

	// The workspace list source, built here for the same reason the other
	// two are: Context.Values captures handles BY VALUE, so a property
	// created after the map is populated resolves to nothing and the pane
	// renders empty with no error anywhere.
	ed.wsFiles = prop.NewComputed(func() components.ItemSource { return ed.browserItems() })

	// A registered component with NO schema, on purpose. Its palette row
	// must read differently from an element that simply takes no
	// attributes — that distinction is the catalog's central honesty
	// rule, and an editor is where it either shows up or is lost.
	// TWO CONTEXTS, and the split is the fix for a crash rather than
	// tidiness.
	//
	// docCtx is the vocabulary the DOCUMENT is authored in. ctx is what
	// the EDITOR's own shell is built with, and it additionally carries
	// the editor's furniture — <Preview>, <LogPane>. Those are not user
	// vocabulary; they are chrome that happens to live in a
	// markup.Context because the editor's page is markup too.
	//
	// Sourcing the palette from ctx conflated "elements this context can
	// build" with "elements the user is authoring with", and those differ
	// by exactly the editor's own furniture. Dropping <Preview> into the
	// document then made the document contain the thing that renders the
	// document: Measure recursed until the stack overflowed and took the
	// terminal with it.
	//
	// A denylist of the two names would re-break the day a third
	// component is added. Separating the contexts cannot: chrome is
	// registered in one place and the palette reads the other.
	ed.ctx = &markup.Context{
		Values: map[string]any{
			"Noop":         gooey.Command(func() {}),
			"PaletteItems": ed.paletteItems,
			"PaletteSel":   ed.paletteSel,
			"AttrItems":    ed.attrItems,
			"AttrSel":      ed.attrSel,
			"Source":       ed.source,
			// The COMPUTED, not the source: the page asks for one string
			// and the editor has two writers for it. See statusText.
			"Status":      ed.statusText,
			"EditName":    ed.editName,
			"EditValue":   ed.editValue,
			"EditDoc":     ed.editDoc,
			"TreeText":    ed.treeText,
			"ActivitySel": ed.activitySel,
			"DocsItems":   ed.docsItems,
			"DocsSel":     ed.docsSel,
			"DocsBody":    ed.docsBody,
			"Serving":     ed.serveInfo,
			"Fits":        ed.fits,
			"Cramped":     ed.cramped,
			"FitMsg":      ed.fitMsg,
			"ModeText":    ed.modeText,
			"ToggleMode":  gooey.Command(func() { ed.toggleMode() }),
			"ToggleTheme": gooey.Command(func() { ed.themeDark.Set(!ed.themeDark.Get()) }),
			// The grid-structure verbs. Every one of them is a KEY, not
			// only a pointer gesture, because mouse input cannot be
			// injected through a recording pty — a pointer-only feature
			// cannot be verified in a capture at all.
			"NextTrack":    gooey.Command(func() { ed.cycleTrackCursor(1) }),
			"PrevTrack":    gooey.Command(func() { ed.cycleTrackCursor(-1) }),
			"GrowTrack":    gooey.Command(func() { ed.resizeTrack(1) }),
			"ShrinkTrack":  gooey.Command(func() { ed.resizeTrack(-1) }),
			"CycleTrack":   gooey.Command(func() { ed.cycleTrackKind() }),
			"AddTrack":     gooey.Command(func() { ed.addTrack() }),
			"RemoveTrack":  gooey.Command(func() { ed.removeTrack() }),
			"Add":          gooey.Command(func() { ed.addSelected() }),
			"Delete":       gooey.Command(func() { ed.deleteSelected() }),
			"NextEl":       gooey.Command(func() { ed.selectNext(1) }),
			"PrevEl":       gooey.Command(func() { ed.selectNext(-1) }),
			"SelectParent": gooey.Command(func() { ed.selectParent() }),
			"SelectChild":  gooey.Command(func() { ed.selectChild() }),
			"MoveUp":       gooey.Command(func() { ed.moveSelected(-1) }),
			"MoveDown":     gooey.Command(func() { ed.moveSelected(1) }),
			"Promote":      gooey.Command(func() { ed.promoteSelected() }),
			"Demote":       gooey.Command(func() { ed.demoteSelected() }),
			"Duplicate":    gooey.Command(func() { ed.duplicateSelected() }),
			"ToCanvas":     gooey.Command(func() { ed.retype("Canvas") }),
			"ToVStack":     gooey.Command(func() { ed.retype("VStack") }),
			"BeginEdit":    gooey.Command(func() { ed.beginEdit() }),
			"EditText":     gooey.Command(func() { ed.editSelectedAsText() }),
			"CommitEdit":   gooey.Command(func() { ed.commitEdit() }),
			"Quit":         gooey.Command(func() { ed.app.Quit() }),
			"Copy":         gooey.Command(func() { ed.copySelected() }),
			"Cut":          gooey.Command(func() { ed.cutSelected() }),
			"Paste":        gooey.Command(func() { ed.pasteClip() }),
			// The control-plane addresses, copyable from the keyboard.
			// Resolved through ed.addrs at PRESS time rather than
			// captured: a hot reload builds a new strip, and a captured
			// one would go on copying a dead page's addresses.
			"CopyGrpc": gooey.Command(func() { ed.copyEndpoint("grpc") }),
			"CopyMCP":  gooey.Command(func() { ed.copyEndpoint("mcp") }),
			// PLAIN Commands rather than .When(canUndo): a disabled
			// Action keeps bubbling rather than being consumed
			// (input.go), so a gated ctrl+z at the bottom of the stack
			// would simply fall through and do nothing — which is the
			// one behaviour undo must not have, because a keystroke that
			// silently does nothing is indistinguishable from a broken
			// editor. Handling the empty case inside undo/redo is what
			// lets it say "nothing to undo" instead.
			"Undo": gooey.Command(func() { ed.undo() }),
			"Redo": gooey.Command(func() { ed.redo() }),
		},
		Styles: map[string]render.Style{
			"dim":  {Fg: render.RGB(140, 140, 150)},
			"warn": {Fg: render.RGB(255, 170, 60)},
			// The attribute currently loaded into the editor. It is a
			// SELECTION cue, so it must not wear a warning colour —
			// orange on a perfectly healthy row reads as an error, which
			// is what it did and what got asked about.
			//
			// Deliberately NOT also the modified-from-default
			// indicator. That needs AttrSpec.Default, which does not
			// exist yet, and one cue meaning two things is the collapse
			// this project keeps cataloguing.
			"sel":   {Fg: render.RGB(180, 200, 255), Bold: true},
			"ok":    {Fg: render.RGB(120, 200, 140)},
			"title": {Fg: render.RGB(180, 200, 255), Bold: true},
		},
		// THE EDITOR'S OWN CHROME, and all of it. Each pane is a control
		// in components/<pane>/, so the shell is four instantiations and
		// the panes' internals are checked contracts rather than markup
		// that happened to bind the right names.
		//
		// These live on ctx and NEVER on docCtx: a document that could
		// build the editor's panes would contain the thing that renders
		// it. See the two-context comment above.
		// ActivityBar is registered through Elements, not Components: it
		// DECLARES Sel, so the catalog can describe it and the attribute
		// check applies to it. See the docCtx registration below, where
		// the difference is load-bearing rather than tidy.
		Elements: map[string]*markup.ElementDef{
			"ActivityBar": activitybar.Def(ed.fsys, nil),
			// <DockHost> is EDITOR CHROME and is registered here only,
			// never on docCtx — for the same reason <Preview> is. A
			// document that could build the dock host would contain the
			// thing laying it out, and Measure would recurse until the
			// stack overflowed.
			"DockHost": dockDef(ed.dock),
		},
		Components: map[string]markup.Builder{
			"Preview": preview.Builder(ed.pv),
			// The PROPERTIES pane's editing surface. It wraps the
			// inspector list and floats a per-Kind editor over the
			// selected row; see properties.go.
			"ValueEditor": ValueEditorBuilder(ed),
			// One Art per app: the frame cache is keyed by size and colour,
			// so panes of the same size share a raster.
			"Panel": panel.Builder(ed.art),
			// The status bar's endpoint strip — chrome, so ctx only.
			"ServeAddrs": serveAddrsBuilder(ed),
		},
	}

	// docCtx shares the bindings and styles — a document authored here
	// binds the same names — but deliberately NOT Components. Every
	// entry there is editor chrome; see the comment above.
	//
	// It does NOT get the Dispatcher here, because there is no app yet:
	// newEditor runs before gooey.NewApp, so the wiring is an assignment
	// afterwards. See setDispatcher.
	ed.docCtx = &markup.Context{
		Values: ed.ctx.Values,
		Styles: ed.ctx.Styles,
		// The DOCUMENT's own registered vocabulary. <LogPane> stands in
		// for what a target app registers — a component the user
		// legitimately authors with, and whose attributes are
		// unknowable because a Builder is a func. That is the case the
		// palette must render differently from "takes no attributes",
		// so it belongs here rather than among the editor's chrome.
		Components: map[string]markup.Builder{
			"LogPane": func(e markup.Element, ctx *markup.Context) (gooey.Component, error) {
				return &components.Text{Content: components.Str("[LogPane]")}, nil
			},
			// <Preview> IS placeable in a document — the palette offers
			// it honestly. It simply builds something else here: an
			// Escher mirror rather than the real pane, so previewing a
			// document that contains a preview is a visual instead of a
			// stack overflow. See components/preview/mirror.go.
			"Preview": preview.MirrorBuilder(ed.ctx.Styles["dim"]),

			// THE EDITOR'S REUSABLE COMPONENTS ARE DOCUMENT VOCABULARY TOO,
			// and the distinction is recursion, not provenance.
			//
			// The two-context split exists because <Preview> renders the
			// document: a document containing one contains the thing
			// drawing it, and Measure recursed until the stack overflowed.
			// Nothing of the kind is true of <Panel> or <ActivityBar>. They
			// are ordinary components that happen to have been written here
			// first, and a document is entitled to a framed region or an
			// icon rail like any other.
			//
			// Withholding them made the toolbox misdescribe the app: it
			// listed every builtin and the two registered stand-ins while
			// silently omitting the components this editor is actually
			// built out of, which is the same class of dishonesty as
			// rendering "unknown" as "none".
			//
			// The SAME builders as the editor's chrome, sharing one Art
			// cache: a panel in the document and a panel in the shell are
			// the same component at the same size, so they should be the
			// same raster too.
			"Panel": panel.Builder(ed.art),
		},
		// ActivityBar DECLARES its surface, and on the document context
		// that is the difference between an element the palette can offer
		// and one it cannot.
		//
		// The bug this fixes was reported from the running editor:
		// clicking ActivityBar emitted <ActivityBar Name="ActivityBar1"/>
		// and the insert failed to load with "needs Sel=". Seeding reads
		// the spec, and a Builder registration contributes no Attrs at
		// all — Catalog gives it AttrsKnown false, because a Builder is
		// a func and not a schema. So the palette offered an element it
		// could not produce valid markup for.
		//
		// With the declaration in hand the element also carries a Seed,
		// and markup.Seeded turns Sel's {Required, BindsBinding,
		// GoType:"int"} into a prop.NewSource(0) registered under
		// ActivityBar1_Sel with Sel="{{.ActivityBar1_Sel}}" written into
		// the markup. See editor.seed.
		//
		// <Panel> stays a Builder deliberately: its only attribute is a
		// Style name it reads by hand, nothing about it is required, and
		// adding a declaration it does not need would suggest the two
		// registrations are ranked when they are not. Declare when there
		// is a contract worth checking.
		Elements: map[string]*markup.ElementDef{
			"ActivityBar": activitybar.Def(ed.fsys, nil),
		},
	}

	// The IDE shell's bindings, merged into the SAME map docCtx already
	// shares by reference — so a document authored here binds the same
	// names, exactly as it does for every other value. Merged rather than
	// written into the literal because they are built from ed's own
	// handles, several of which are computeds over the fields above.
	//
	// A collision is a BUG, not a last-writer-wins: two bindings under one
	// name means one of them is unreachable and nothing would say which.
	for k, v := range ed.menuValues() {
		if _, dup := ed.ctx.Values[k]; dup {
			panic("wysiwyg: duplicate binding name " + k)
		}
		ed.ctx.Values[k] = v
	}

	ed.loadPalette()

	return ed
}

// contexts is every markup.Context the editor builds trees with.
//
// It exists so the wiring below can LOOP rather than name one context and
// miss the other, which is the whole of #462: docCtx — the context the
// DOCUMENT BEING EDITED is built with — never got the Dispatcher, so the
// canvas refused markup that is legal in a real app, at load, while the
// palette went on offering the attribute. The properties pane is driven
// from ElementDef.Attrs and knows nothing about the context a thing will
// be built in, so nothing anywhere connected the two.
//
// EVERY ENTRY IS CONSTRUCTED IN newEditor, and that is a contract rather
// than an observation: setDispatcher writes through each pointer without
// checking it, so a lazily-built context left nil here panics on the
// first line of main's wiring. That is deliberate and is not an oversight
// to be patched with a nil skip — skipping a nil entry silently is
// exactly #462 again, a context that exists and is never wired, arriving
// this time through the guard meant to prevent it. A startup panic names
// the field on the first run; a silent skip is found by a user typing a
// handler expression into the canvas. The obligation is enforced by
// TestEveryContextTheEditorBuildsWithGetsTheDispatcher, which fails on a
// nil entry before it wires anything. Raised in review of #469.
//
// A CONTEXT ADDED LATER MUST BE ADDED HERE, which is why this sits
// directly under newEditor rather than in the palette neighbourhood it
// was first written in: the instruction has to be in front of the person
// adding the field, and it was three hundred lines away from the two
// literals it is about. Raised in review of #469, which also caught what
// the misplacement did to gofmt-invisible prose — the block landed with
// no blank line after grantOf's comment, so `go doc` read the two as one
// and served grantOf's drag-geometry rationale as the reason for the
// dispatcher seam.
//
// The obligation is ENFORCED for the shape the bug arrived in, and that
// qualifier is load-bearing. TestEveryContextFieldIsInTheList parses this
// file and fails if the editor struct grows a plain `*markup.Context`
// field this body does not name. What it cannot see is a context held in
// a slice or a map, one stored by value, one reached through an embedded
// struct, or an editor field declared in another file of the package —
// its own doc comment enumerates those. So "green" means "no new plain
// pointer field", not "every context is wired", and the unqualified
// sentence that used to be here read as the second. Corrected in review
// of #469, which is the same PR that added the test the sentence
// overclaimed for.
//
// The literal list below cannot enforce anything itself: a field absent
// from both the list and a hand-written test is invisible to both, and
// add-and-forget is precisely the direction #462 came from — a context
// that existed and was never wired. Enumerating hardens against the
// mutation a reviewer would make and leaves the one history made.
//
// EVERY ENTRY MUST BE CONSTRUCTED IN newEditor. That is a requirement on
// what may go in this list, not an observation about what is in it today
// — see setDispatcher, which skips a nil rather than crashing, and says
// why a lazily built context needs its own wiring point instead of a slot
// here.
func (ed *editor) contexts() []*markup.Context { return []*markup.Context{ed.ctx, ed.docCtx} }

// setDispatcher wires the app's dispatcher into every context.
//
// ONE dispatcher across all of them, not one each: handler results are
// Set on the UI goroutine (handlerCommand's `ctx.Dispatcher == nil` arm
// in markup/handlers.go), and two dispatchers would be two routes to a
// graph that tolerates exactly one.
//
// BY SYMBOL, NOT BY LINE. This said `:193` and a nine-line comment added
// to that function in this same branch moved the arm to 202 — a citation
// broken by an edit in another module, which CI vets without running, so
// nothing here would have said. carryDeclarations argues the same rule
// three files over; it applies to its own branch. Raised in review of
// #501.
//
// Separate from newEditor because the dispatcher does not exist yet when
// the contexts are built — gooey.NewApp comes after.
//
// THE NIL SKIP IS NOT DEFENSIVE, and it is the difference between a
// diagnostic and a crash. Both entries are non-nil today, and
// TestEveryContextTheEditorBuildsWithGetsTheDispatcher asserts that. But
// TestEveryContextFieldIsInTheList tells the next author to add any new
// *markup.Context field to contexts(), and a field built LAZILY is nil
// at this moment — so the seam that exists to turn add-and-forget into a
// red test would instead turn it into a nil dereference in main(), before
// the screen comes up. A seam may not convert the mistake it catches into
// a worse one.
//
// Skipping is the honest half, and contexts()' own comment carries the
// other: a context this list names must be constructed in newEditor, and
// one built later needs its own wiring point rather than a slot here —
// because a nil skipped here is a context that silently never gets a
// dispatcher, which is #462 again. Raised in review of #469.
func (ed *editor) setDispatcher(d *gooey.Dispatcher) {
	for _, c := range ed.contexts() {
		if c == nil {
			continue
		}
		c.Dispatcher = d
	}
}

// loadPalette derives the palette from the document context's catalog.
//
// Extracted from newEditor so it can be re-run after the vocabulary
// changes. Nothing in the shipped editor changes it yet — but the
// palette is now the source of the drag taxonomy as well as of the add
// list (see grantOf), so "what containers exist" and "what designing in
// them means" are one refresh rather than two.
func (ed *editor) loadPalette() {
	// The palette IS the catalog. Only elements that can appear in a
	// container are offered; the non-visual ones are attachments and
	// belong to a different gesture than "add a child".
	//
	// Nested replaces a hardcoded `e.Name == "Tab"`. The name was right
	// when it was written and wrong by the time <Menu> and <MenuItem>
	// were declared, in the way a name list always goes wrong: it did
	// not fail, it just started offering a <Menu> that produces markup
	// refusing to load. The catalog answers this now — see
	// markup.ElementSpec.Nested — so the second one costs nothing here.
	// An ElementSpec holds maps and strings, so the tail keeps a whole
	// catalog's worth of them alive past len. See clearToCap in the
	// gooey package. Raised in review of #456.
	ed.palette = ed.palette[:0]
	ed.pseudo = map[string]bool{}
	ed.specs = map[string]markup.ElementSpec{}
	// ONE Catalog() CALL, and that is load-bearing rather than tidy.
	// Catalog() is not a getter: it re-derives every builtin spec with
	// fresh Attrs copies, re-runs markNested and sorts — 73us and 52KB
	// on this checkout — and it globs and parses every include file when
	// a context has them. mapNodes asks "is this element pseudo?" once
	// per document node on every rebuild, which is every drag frame,
	// every alt+k and every property edit, so asking the catalog there
	// would put that cost and that garbage on the inner loop. The set is
	// derived HERE because this is where the vocabulary changes: the
	// palette and the pseudo set answer two questions about one catalog
	// read, and cannot come from different reads of it.
	for _, e := range ed.docCtx.Catalog() {
		ed.specs[e.Name] = e
		if e.Pseudo {
			ed.pseudo[e.Name] = true
		}
		if e.NonVisual || e.Nested {
			continue
		}
		ed.palette = append(ed.palette, e)
	}
	// AFTER THE REFILL, which costs cap - len rather than cap and never
	// leaves a live slot holding nil. Corrected in review of #456.
	clear(ed.palette[len(ed.palette):cap(ed.palette)])

	// THE LOAD-TIME GATE for every icon the palette will draw, in BOTH
	// tints the theme switches between.
	//
	// Preloading only the current tint would leave the other theme's
	// first raster to happen inside a paint, and a Render has nowhere to
	// put an error: a broken asset would show up as a column of blanks
	// after the user pressed the theme key, with nothing on any surface
	// to explain it. Rasterizing both here turns that into a startup
	// failure naming the file.
	//
	// It sits INSIDE loadPalette rather than in newEditor, which is where
	// it was before the palette became reloadable, and the move is
	// load-bearing rather than tidy: what it preloads is THIS palette's
	// icons, so a reload that added an element would leave that element's
	// row blank until something else happened to rasterize it. Preloading
	// where the list is derived keeps the two in step by construction.
	names := make([]string, 0, len(ed.palette))
	for _, e := range ed.palette {
		names = append(names, e.Icon)
	}
	ed.iconErr = ed.icons.Preload(names, iconOnDark, iconOnLight)
}

// The two icon tints, and they are the ONLY thing the theme switch
// changes today. Naming them for the ground they sit on rather than for
// the theme ("light"/"dark") is deliberate: an icon on a dark panel is a
// light icon, and a name that says which is which survives somebody
// adding a third theme.
//
// Colours rather than render.Style, because the rasterizer substitutes a
// colour into the SVG before it draws — the tint is part of the source,
// not something applied to the pixels afterwards. See svg.IconSet.At.
var (
	iconOnDark  = color.RGBA{0xc8, 0xcd, 0xdc, 0xff}
	iconOnLight = color.RGBA{0x3a, 0x3f, 0x4c, 0xff}
)

// describeAttrs is the palette's honesty column, and the reason the
// catalog splits AttrsKnown from Origin. "no attributes" and "attributes
// unknown" are different facts about the app and must not render alike.
func describeAttrs(e markup.ElementSpec) string {
	if !e.AttrsKnown {
		return "? unknown"
	}
	n := len(e.Attrs)
	if e.Open {
		return fmt.Sprintf("%d +open", n)
	}
	if n == 0 {
		return "none"
	}
	return fmt.Sprintf("%d", n)
}

// legalValues says how an attribute may be written: the enum's members
// where there are members, otherwise how it binds. A KindEnum row that
// does not list its own values is the catalog knowing something the user
// has to guess.
func legalValues(a markup.AttrSpec) string {
	if len(a.Enum) > 0 {
		return strings.Join(a.Enum, "|")
	}
	switch a.Binds {
	case markup.BindsBinding:
		// Binding-only: a literal is a load error, so say so before the
		// user types one.
		if a.GoType != "" {
			return "{{." + a.GoType + "}}"
		}
		return "{{binding}}"
	case markup.BindsEither:
		return "text or {{…}}"
	}
	return ""
}

// attrRow is one inspector line. kind/legal/value came from AttrSpec
// all along — Enum, Binds and Required are populated by the catalog and
// were simply never read, which is most of what "the inspector doesn't
// show binding or enum" meant.
type attrRow struct {
	name  string
	kind  string
	legal string // the accepted values, or how the attribute may be written
	value string
	doc   string
	req   bool
	// cat is the property-grid group, from markup.CategoryOf.
	cat string
	// def is the declared default, empty where the catalog declares none.
	def string
	// values is the finite set this attribute may cycle through, or nil
	// where the value is free text. It is the per-Kind editor, resolved
	// against the LIVE context — a style list is whatever the app
	// registered, not a guess — and it is what makes enter mean "next
	// legal value" on the rows that have one.
	values []string
	// modified is the Visual Studio rule: a row differs from its default.
	// It is deliberately NOT the same cue as the selection highlight —
	// one colour meaning two things is the collapse this project keeps
	// cataloguing, and it is why the selection cue was left neutral when
	// this field did not yet exist.
	//
	// An attribute with no declared Default is never "modified", because
	// there is nothing to differ from. That is a third state, not a
	// false: markup.AttrSpec.Default is empty exactly where nothing can
	// check it.
	modified bool
	// body marks the one row that edits node.Body rather than an entry in
	// node.Attrs. Carried as a FLAG rather than inferred from the name at
	// each use, because "is this row the body" is asked in three places
	// and a string comparison repeated three times is three chances to
	// spell it differently.
	body bool
}

// BodyRowName is how the body row is labelled in the properties grid.
//
// Parenthesised on purpose, following Visual Studio's (Name): it is the
// grid's spelling for an entry that is NOT a property of the object. A
// row called Content or Text would be copied into markup as
// Content="hello" and produce "no such attribute" from a name the editor
// itself put on screen.
const BodyRowName = "(text)"

// isModified compares the document's value against the declared default.
// Absent means default, and a value equal to the default is not a
// modification even though it was written — which matches what a VS grid
// shows and, more importantly, matches what the markup does.
func isModified(a markup.AttrSpec, value string) bool {
	if a.Default == "" {
		return false
	}
	if value == "" {
		return false
	}
	return value != a.Default
}

// cycle is the list enter walks for a row: the legal values, preceded by
// UNSET for anything not required.
//
// Unset has to be in the cycle or an optional attribute could be given a
// value and never taken back, which would make the cycling editor a
// one-way door. A required attribute has no unset state — removing it is
// a load error — so it is not offered one.
func (r attrRow) cycle() []string {
	if len(r.values) == 0 {
		return nil
	}
	if r.req {
		return r.values
	}
	return append([]string{""}, r.values...)
}

// attrRows is claim 2 and claim 3 together: the inspector asks the
// PARENT'S GRANT for the element's attributes, so the answer depends on
// what the element is currently inside.
//
// ed.grantOf(parent).AttrsFor(spec), not markup.AttrsFor(spec, parent) —
// the difference is the whole point of #418 and is spelled out at the
// call site below. This comment named the old form for two rounds after
// the code stopped using it, which is the failure mode a stale comment
// has here specifically: it sends the reader to the function whose
// wrong answer the change existed to stop taking.
func (ed *editor) attrRows() []attrRow {
	spec, parent, target := ed.target()
	if target == nil {
		return nil
	}
	var rows []attrRow
	// The BODY row, where the element has one. It is not an attribute and
	// must not be dressed as one: BodyRowName is parenthesised, the way
	// Visual Studio writes (Name), so nobody copies it into markup as
	// Content="…" and gets a load error from a list the editor supplied.
	if bs := ed.bodySpec(target.Elem); bs != nil {
		// Kind and legal come from the SPEC, and Binds is the load-bearing
		// one: <Text>'s body goes through bindText, so {{.Title}} typed
		// here is a live binding rather than the literal text "{{.Title}}".
		// legalValues already renders that distinction for attributes, and
		// a body is an attribute in every respect but where it is written
		// — so it is rendered by the same function rather than by a second
		// description that could disagree with it.
		rows = append(rows, attrRow{
			name:  BodyRowName,
			kind:  string(bs.Kind),
			legal: legalValues(markup.AttrSpec{Kind: bs.Kind, Binds: bs.Binds, GoType: bs.GoType}),
			value: target.Body,
			doc:   bs.Doc,
			cat:   markup.CategoryCommon,
			body:  true,
		})
	}
	// THE PARENT'S GRANT, resolved in the CATALOG. markup.AttrsFor takes a
	// parent NAME and resolves it in the builtin registry, which answers
	// "no attached attributes" for a container the host registered — so
	// the drag wrote Table.R onto a child and the properties grid had no
	// row for it, in the same editor. ed.grantOf reads the catalog, which
	// IS the document's vocabulary, so the inspector and the drag now ask
	// one question. Found in review of #390 (issue #418).
	//
	// THE CATALOG AND NOT THE PALETTE, which is a distinction this
	// paragraph got wrong for two rounds after grantOf itself was
	// corrected: a <MenuItem>'s parent is a <Menu>, which is Nested and
	// therefore absent from the palette, so a palette lookup returns the
	// empty grant and every attached row vanishes with no error. The
	// comment described the defect as the design. Found in review of
	// #454.
	for _, a := range ed.grantOf(parent).AttrsFor(spec) {
		v := target.Attrs[a.Name]
		rows = append(rows, attrRow{
			name:     a.Name,
			kind:     string(a.Kind),
			legal:    legalValues(a),
			value:    v,
			doc:      a.Doc,
			req:      a.Required,
			cat:      markup.CategoryOf(a),
			def:      a.Default,
			values:   ed.valueSet(a),
			modified: isModified(a, v),
		})
	}
	// Grouped by category, then by name inside a group — the Visual
	// Studio categorised view. AttrsFor returns name order, so this is a
	// stable re-sort rather than a different answer.
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].cat != rows[j].cat {
			return categoryRank(rows[i].cat) < categoryRank(rows[j].cat)
		}
		return rows[i].name < rows[j].name
	})
	return rows
}

// categoryRank puts the groups in the order a person reads them rather
// than alphabetically: what the thing is, then where it sits, then how it
// looks, then what it does.
func categoryRank(c string) int {
	switch c {
	case markup.CategoryDesign:
		return 0
	case markup.CategoryCommon:
		return 1
	case markup.CategoryLayout:
		return 2
	case markup.CategoryAppearance:
		return 3
	case markup.CategoryEvents:
		return 4
	}
	return 5
}

// target resolves the selection to (spec, parent element name, node).
//
// A nil node is NOTHING SELECTED, and every caller already treated a nil
// target as "no rows" — that is why the empty state cost so little: the
// grid, the editors and the description pane all bottom out here.
func (ed *editor) target() (markup.ElementSpec, string, *node) {
	n := ed.sel
	if n == nil {
		return markup.ElementSpec{}, "", nil
	}
	// The PARENT is what decides which attached properties are on offer —
	// Canvas.Left under a <Canvas>, Grid.Row under a <Grid> — so it is
	// looked up rather than assumed to be the root. That lookup is what
	// lets a nested node be selected without the inspector lying about
	// what can be set on it.
	parent := ""
	if p := ed.parentOf(n); p != nil {
		parent = p.Elem
	}
	// THE CATALOG, NOT ed.palette. The palette is the catalog minus what
	// may not be PLACED on its own; this asks what may be SET on what is
	// already there, and those stopped being one question the moment a
	// nested element could be selected. Asking the palette for a
	// <MenuItem> finds nothing and falls through to the bare spec below,
	// so the grid shows an empty list for a node whose vocabulary this
	// same change declared.
	//
	// THE MAP, THOUGH, NOT Catalog(), BECAUSE THIS IS ON THE PAINT PATH.
	// attrRows reaches here from ed.attrItems, a prop.NewComputed bound
	// to <ItemsView Items="{{.AttrItems}}">, so it evaluates inside that
	// ItemsView's paint node on every repaint after an ed.rev bump. A
	// Catalog() call there rebuilds every builtin spec while painting,
	// and does filesystem I/O and XML parsing the moment a document
	// context sets Includes.
	if e, ok := ed.specOf(n.Elem); ok {
		return e, parent, n
	}
	// Still reachable, and it is not dead code: an element the document
	// names and the catalog does not. The node is returned so the grid
	// says which element is selected rather than going blank.
	return markup.ElementSpec{Name: n.Elem}, parent, n
}

// doc is the USER'S ROOT — the element a save writes, and the only child
// of the surface.
//
// ed.root is the design SURFACE: a <Canvas> that exists so everything
// dropped on it has free geometry to be dragged around in. It is the
// editor's workspace and is never serialized, which is what makes "where
// do the positions live" a real question rather than a detail — under a
// save that writes doc() and nothing above it, a position on the surface
// has no home in the file at all. Nothing here invents one.
func (ed *editor) doc() *node { return ed.root.Kids[0] }

// isSurface reports whether n is the design surface rather than part of
// the user's document. Nothing selectable, deletable or saveable.
func (ed *editor) isSurface(n *node) bool { return n == ed.root }

// parentOf returns the node holding n, or nil for the root and for a node
// that is not in this document.
func (ed *editor) parentOf(n *node) *node { return parentIn(ed.root, n) }

// parentIn walks KIDS ONLY, never Slots — so a node inside a property
// element (<ItemsView.ItemTemplate>) has no findable parent.
//
// That is safe, and it is safe for one specific reason rather than by
// luck: such a node cannot become the selection. mapNodes, outline and
// selectNext all walk Kids alone too, so nothing puts a slot interior in
// ed.nodeOf, in the outline, or under ctrl+n.
//
// Do NOT "fix" this in isolation. Teaching it about Slots on its own
// makes things worse: deleteSelected searches p.Kids and would still
// find nothing (silently doing nothing, exactly as now), and addTarget
// would start returning a slot OWNER as an append target, so a palette
// click would push a child into an element whose content is a slot. The
// three learn about slots together or not at all.
// TestSlotInteriorsAreNotSelectableWhichIsWhatMakesParentInSafe pins the
// precondition and says so when it breaks.
func parentIn(at, n *node) *node {
	for _, k := range at.Kids {
		if k == n {
			return at
		}
		if p := parentIn(k, n); p != nil {
			return p
		}
	}
	return nil
}

// ---- edits ----

func (ed *editor) rebuild() {
	// UNDO IS RECORDED HERE, and this is the only place it is recorded.
	// Every mutator ends in a rebuild — it has to, or the preview, the
	// outline, the CODE tab and the build status all go stale — so
	// deriving the history at this one choke point means a mutation added
	// later is undoable without its author knowing undo exists. See
	// undo.go for why it is a hook rather than a wrapper each caller must
	// remember, and for why it must run BEFORE the remote branch below.
	ed.recordHistory()
	// A REBUILD RETIRES THE DRAG HINT, because the hint describes a
	// GESTURE and the document has just moved on from it.
	//
	// statusText prefers a non-empty hint over a healthy build status
	// (only a "✗" outranks it), so a hint nothing ever cleared buries
	// "✓ builds" for the rest of the session. undo/redo made that
	// reachable without a drag: they end in sayDrag("undone: …"), and on
	// a plain edit afterwards the bar still read "undone: …" while the
	// build status it was covering had changed underneath. Found in
	// review of #392.
	//
	// SAFE AT THIS END OF THE CALL because every sayDrag in the editor
	// comes AFTER its rebuild, not before — checked at each site, not
	// assumed: the drag release (drag.go), every track verb (which
	// announces via sayTrack after writeTracks), and undo/redo itself,
	// whose restore() rebuilds and then says. A hint set after this line
	// survives; one left over from a previous gesture does not.
	//
	// Guarded because prop.Set does not compare: setting "" over "" would
	// invalidate the status bar's dependents on every edit, which is the
	// same repaint sayDrag guards against at its own call site.
	if ed.dragHint != nil && ed.dragHint.Get() != "" {
		ed.dragHint.Set("")
	}
	// AND RE-ESTABLISHED IF IT WAS A MODE RATHER THAN AN EVENT. The line
	// above retires a hint that described a GESTURE, which is right; the
	// track cursor's line is not one. It states which track is picked and
	// what -/=/g/a/r will do to it, and that stays true for exactly as
	// long as the cursor is on — so retiring it leaves the gutter
	// highlighting a track the bar no longer names and the verbs
	// unlisted, with the mode still live.
	//
	// The reason this looked safe is that every track verb announces
	// through sayTrack AFTER its writeTracks, so the clear was always
	// overwritten on those paths. It is any other edit — an add, a
	// rename, an undo — that lands here and stops. Found in review
	// of #392.
	//
	// Re-derived from ed.cursor rather than saved and restored around
	// the clear: the document may have just lost the track the cursor
	// pointed at, and sayTrack is where that is already handled.
	if ed.cursor.on {
		if n := ed.gridNode(); n != nil {
			ed.sayTrack(n)
		}
	}
	// Tick FIRST, so every derived list recomputes even if the build
	// below fails and returns early.
	ed.rev.Set(ed.rev.Get() + 1)
	// TWO MARKUP STRINGS, and which one is which is the whole wrapping
	// model:
	//
	//   src  — the USER'S DOCUMENT, from doc() down. This is what a save
	//          writes, what the CODE and OUTPUT tabs show, and what
	//          pushRemote sends. The surface Canvas is the editor's
	//          workspace and appears in none of them: a remote app
	//          rendering the designer's own scaffolding would be showing
	//          the user something they cannot save, cannot edit and did
	//          not write, and it would make the wire contract depend on an
	//          editor implementation detail.
	//   full — the same document INSIDE the surface, which is the only
	//          thing built for the preview, because the surface is what
	//          gives everything on it free geometry.
	src := envelopeHead(ed.envAttrs, ed.envDecls) + ed.doc().markup("  ") + "</Gooey>\n"
	full := envelopeHead(ed.envAttrs, ed.envDecls) + ed.root.markup("  ") + "</Gooey>\n"
	ed.source.Set(src)
	ed.treeText.Set(ed.outline())
	// Dropped up front, on every path: from here until the swap below
	// succeeds there is no tree that corresponds to this document, and
	// click-to-select must not map a press against one that does not.
	ed.docRoot, ed.nodeOf, ed.compOf = nil, nil, nil

	if ed.remote != nil {
		// Driving another app: the target's live binding context is the
		// only authority on whether this document loads, so validate
		// against IT rather than against the editor's own context.
		//
		// SO seedDeclared DOES NOT RUN HERE, and that exclusion is the
		// scope of #517's build half: under -attach, opening a control's
		// own defining document still reports the target's `"Title" not
		// found in context`. It is the right call for the same reason
		// the branch exists — the editor cannot seed a context it does
		// not own, and a name it invented locally would make the preview
		// disagree with the app — but seedDeclared's doc reads as
		// unconditional, so the exclusion is stated at the branch that
		// causes it. Raised in review of #522.
		ed.pushRemote(src)
		return
	}

	// The document's own declarations are part of the vocabulary it is
	// built against, and only the editor can put them there — see
	// seedDeclared. Before the Build, because that is what consumes
	// them.
	if !ed.seedDeclared(full) {
		return
	}

	// Built against the DOCUMENT vocabulary, so a document can never
	// contain the editor's own chrome. FULL, not src: the preview is the
	// document on its surface.
	w, err := markup.Build([]byte(full), ed.docCtx)
	if err != nil {
		// A load error is normal while editing and must never take the
		// editor down with it. The previous preview stays on screen.
		ed.status.Set("✗ " + err.Error())
		return
	}
	ed.status.Set("✓ builds" + shadowedNote(ed.shadowedDecls))
	ed.pv.Swap(w)
	// The one moment the document and the built tree are known to
	// correspond: w is what markup.Build made of THIS document. Inverted
	// here rather than re-derived at click time, because a later edit moves
	// one and not the other. See mapNodes for what happens where the two
	// stop lining up.
	ed.docRoot = w
	ed.nodeOf = map[gooey.Component]*node{}
	ed.compOf = map[*node]gooey.Component{}
	ed.mapNodes(ed.root, w)
}

// seedDeclared gives the open document's own <x:Property> declarations
// a live handle in the vocabulary the preview is built against, so a
// file that DECLARES a property and then binds it — {{.Title}} in the
// body of the control that declares Title — previews instead of
// refusing to load.
//
// NOTHING ELSE DOES THIS, and the reason is structural rather than an
// oversight. declarations.instantiate runs at an INSTANTIATION SITE
// (markup/usercontrol.go): the page that writes <Card Title="…"/>
// resolves the attribute and hands the result down. The editor is
// holding card.gooey itself, which has no site above it, so the
// declared names never reach Context.Values and the control's own
// binding is the error the user sees:
//
//	✗ markup: "Title" not found in context
//
// #517 opened such a file and #522's first rounds made it round-trip;
// this is the half that makes it BUILD. Raised in review of #522.
//
// markup.Declaration.AbsentValue picks the value, and that call is the
// whole of the policy: it is the handle an omitted optional attribute
// would have got, Default included. A Required property has no default
// and previews as its type's zero — the only stand-in available to a
// tool that is not an instantiation site, and a visible one, since an
// empty string on the surface is what a caller who forgot it would see.
//
// RETIRED AND RESEEDED WHOLE, every rebuild. docCtx.Values is the
// editor's one binding map (newEditor gives docCtx ed.ctx.Values), so a
// name the previous file declared would otherwise still resolve in the
// next one — and a Type edited in place would keep the handle of the
// type it used to be, which builds and then paints the wrong thing.
// Reseeding costs one parse of a document the rebuild is about to parse
// again; the early return keeps that off every document that declares
// nothing.
//
// A name Values already holds is LEFT ALONE, and that check is load
// bearing rather than defensive. menuValues registers the IDE shell's
// own bindings under BARE names — Region, CodeView, Save — in this same
// map, and a declaration may legally be called any of them. Without the
// check such a document would replace the editor's handle with a string
// source and then, on the next open, DELETE it: the menus would be
// bound to nothing, in a session that had merely opened a file.
// markup.Seeded's placeholders are safe by spelling (<Name>_<Attr>) and
// these are not.
//
// THE SHARED MAP HAS A SECOND CONSEQUENCE, and it reaches further than
// menuValues. ed.docCtx.Values IS ed.ctx.Values, and both the gRPC and
// the MCP server are handed ed.ctx (see the comment at their start), so
// the open document's declared names are in the vocabulary the CONTROL
// PLANE validates and patches against. Two halves, one wanted and one
// not:
//
//   - the binding pickers see them, which is why typedBindings and
//     commandBindings (editors.go) offer {{.Title}} while the declaring
//     document is open — the reason this is not simply filtered out;
//   - a client's set_value against a seeded name takes, and is then
//     DISCARDED on the next rebuild, because the loop above installs a
//     fresh handle from Default every time.
//
// Transient by construction, in other words, and the transience is the
// part a client cannot see. It is recorded rather than removed because
// removing it costs the first half.
// TestASeededNameIsVisibleToTheControlPlaneAndIsTransient measures both.
//
// LOCAL PREVIEW ONLY. rebuild returns on the remote path before it
// reaches this, so none of the above is true under -attach; the comment
// at that branch carries the reasoning.
//
// AND Type="any" IS NOT PREVIEWED, which is the other scope boundary
// and belongs here for the same reason the remote path does. The
// absent-optional answer for `any` is a *prop.Property[any], and every
// consumer one level down wants the concrete handle — so a defining
// document that uses the escape hatch opens and does NOT build:
//
//	✗ markup: <Text Style="{{.Tint}}"> is *prop.Property[interface {}];
//	  need *prop.Property[render.Style]
//
// Measured through openWorkspaceFile in review of #522, on both
// markup-only controls this tree ships (cmd/colors/swatch.gooey and
// cmd/cards/card.gooey) and on all four consumer positions the escape
// hatch exists for. It is not a defect in AbsentValue, which returns
// exactly what resolve returns for an absent optional `any`; a
// Declaration does not know its consumer, so a preview for these is a
// separate decision rather than a fix. isHandlerExpr requires the type
// for behaviour crossing a control boundary and propKinds has no row
// for a slice, so it is also the only spelling a series handle has.
// TestAnAnyDeclarationSeedsAHandleItsConsumersRefuse pins the state of
// play, so the day it changes the docs go with it.
func (ed *editor) seedDeclared(src string) bool {
	// CLEARED BEFORE THE EARLY RETURN, not inside the loop below: a
	// document that shadowed a name and is then replaced by one
	// declaring nothing takes the early return, and a note left over
	// from the previous document would name a declaration the open file
	// does not contain.
	ed.shadowedDecls = ed.shadowedDecls[:0]
	if len(ed.envDecls) == 0 && len(ed.seededDecls) == 0 {
		return true
	}
	for _, name := range ed.seededDecls {
		delete(ed.docCtx.Values, name)
	}
	ed.seededDecls = ed.seededDecls[:0]
	decls, err := markup.Declarations([]byte(src))
	if err != nil {
		// The build this precedes reports it, with the same message and
		// in the place the user is already looking. Parsing twice is
		// what needing the declarations BEFORE the build costs.
		return true
	}
	for _, d := range decls {
		if _, taken := ed.docCtx.Values[d.Name]; taken {
			// SKIPPED, AND SAID. The skip is the behaviour
			// TestADeclarationDoesNotCaptureAnEditorBinding earns; the
			// silence was the defect. Measured on a document declaring
			// Name="Region" Type="string" Default="zzz" and binding
			// {{.Region}}: status "✓ builds", the handle still the
			// IDE's own *prop.Property[int] region enum, the <Text>
			// rendering "0" — an editor implementation detail shown
			// inside the user's document, under a green status, with
			// the author's only route to the diagnosis being to know
			// menuValues' name list.
			//
			// newEditor PANICS on a duplicate binding name for the very
			// reason that went unsaid here: one of the two is
			// unreachable and nothing would say which. A panic is
			// obviously wrong for a name a USER'S FILE chose; a line in
			// the status is not. Raised in review of #522.
			ed.shadowedDecls = append(ed.shadowedDecls, d.Name)
			continue
		}
		v, err := d.AbsentValue()
		if err != nil {
			// SAID, NOT SWALLOWED, and the caller stops. A `continue`
			// here left the name unbound and the Build that follows
			// then failed with `"Title" not found in context` — the
			// exact pre-#517 error, reported as though nothing had been
			// attempted, about the one document this whole function
			// exists to make load. The arm is unreachable today (a
			// Declaration from Declarations always carries a type-table
			// row, and Default was coerced at parse time), which is the
			// argument for reporting it rather than for hiding it:
			// nothing will ever have seen this message, so it must
			// carry its own cause. Raised in review of #522.
			ed.status.Set("✗ " + err.Error())
			return false
		}
		ed.docCtx.Values[d.Name] = v
		ed.seededDecls = append(ed.seededDecls, d.Name)
	}
	return true
}

// shadowedNote is what the build status says about declared names the
// editor's own chrome already binds, and "" when there are none.
//
// APPENDED TO "✓ builds" RATHER THAN REPLACING IT, because the document
// really does build: the binding resolves, the tree is made, the preview
// is live. What it is not is the author's value — the handle is the
// IDE's, so Default never appears and the declared Type is not what the
// binding resolved to. That is a narrower claim than a failure and the
// status says the narrower thing.
//
// IT NAMES THE NAMES, because the author's only other route to the
// diagnosis is knowing menuValues' list, which is not in their document.
func shadowedNote(names []string) string {
	if len(names) == 0 {
		return ""
	}
	verb := " is a name the editor itself binds"
	if len(names) > 1 {
		verb = " are names the editor itself binds"
	}
	return " — " + strings.Join(names, ", ") + verb + "; the preview shows the " +
		"editor's value, not this declaration's"
}

func (ed *editor) outline() string {
	var b strings.Builder
	mark := func(n *node) string {
		if ed.sel == n {
			return "> "
		}
		return "  "
	}
	// From the USER'S ROOT down, and to full depth. The surface is not in
	// the outline for the same reason it is not in the save: it is not
	// part of what the user is building.
	var walk func(n *node, indent string)
	walk = func(n *node, indent string) {
		fmt.Fprintf(&b, "%s%s<%s Name=%q>\n", mark(n), indent, n.Elem, n.Attrs["Name"])
		for _, k := range n.Kids {
			walk(k, indent+"  ")
		}
	}
	walk(ed.doc(), "")
	return b.String()
}

func (ed *editor) addSelected() {
	i := ed.paletteSel.Get()
	if i < 0 || i >= len(ed.palette) {
		return
	}
	spec := ed.palette[i]
	// WHERE IT LANDS AND WHAT WRAPS IT, both from the catalog. See
	// addplan.go — the climb is what stops an insert silently relocating
	// to the document root, and the wrap is what makes "add a <Button>
	// with a <Tabs> selected" mean a new tab holding the button rather
	// than an illegal child that stops the document building.
	plan := ed.planAdd(spec.Name)
	into := plan.into
	// planAdd REFUSES rather than landing a nested element on the root
	// (planAdd, in addplan.go), and a refusal is an empty addPlan. Today
	// loadPalette keeps Nested elements out of the palette so this arm is
	// unreachable from here; the dereference below is one `ed.specs`
	// range away from a nil panic the moment that stops being true, and
	// "currently unreachable" is not a thing to leave a deref resting on.
	if into == nil {
		ed.status.Set("✗ <" + spec.Name + "> has no legal parent on this page")
		return
	}
	// The name comes from what is IN USE, never from a count. Counting
	// children re-issues a live name as soon as one is deleted from the
	// middle: three adds then a delete then an add produced two
	// <Text Name="Text3">, and the document still built, so nothing said
	// so. Name is what markup.Find resolves against, which makes the
	// second one unaddressable rather than merely untidy.
	//
	// Against ed.root, not `into`: uniqueness has to hold across the whole
	// document because that is the scope markup.Find searches. Scoping it
	// to the insertion parent would let two siblings-of-different-parents
	// collide, which is the same bug in a smaller box.
	name := uniqueName(ed.root, spec.Name)

	n, err := ed.seed(spec, name)
	if err != nil {
		ed.status.Set("✗ " + err.Error())
		return
	}
	// The Name is the editor's, never the seed's: it is the ADDRESS the
	// outline, the property grid and hitTest all resolve by, and it has
	// to be unique among siblings. A seed that carried one would collide
	// with the second copy of itself.
	n.Attrs["Name"] = name

	// And so is the BODY, for a body-bearing element — but only the
	// inserted element's OWN body, never its seed's children.
	//
	// A seed is one instance, so <Text>'s is the literal
	// "<Text>Text</Text>": every palette-inserted copy reads the same
	// word. Name will not do instead — it is the ADDRESS the outline and
	// hitTest resolve by, and it never appears on the canvas. The body is
	// the only thing DRAWN, so two of them sharing one make the elements
	// indistinguishable in the one place the user is working.
	//
	// The guard is n.Body != "": an element whose seed gave it no body
	// did not want one, and a container's inline children keep theirs
	// (<VStack>'s One and Two, <Grid>'s A and B) because a seed names its
	// children deliberately and they are taken verbatim.
	if n.Body != "" {
		n.Body = name
	}

	// Free geometry only where the PARENT gives it. Under a <Grid> or a
	// <VStack> a Canvas.Left is silently discarded, which is the defect
	// the catalog work exists to delete.
	// ASKED OF THE GRANT, for the reason retype and pasteClip now do:
	// the name told you about one container and the two literals told
	// you about one spelling of its axes, and neither survives a
	// third-party container or a catalog rename.
	if g := ed.grantOf(into.Elem); g.Kind == markup.GrantOffset {
		if x, y := g.Attr(markup.RoleX), g.Attr(markup.RoleY); x != "" && y != "" {
			n.Attrs[x] = "2"
			n.Attrs[y] = fmt.Sprint(len(into.Kids)*2 + 1)
		}
	}
	// TRANSACTIONAL, and the loader is what decides. A container's legal
	// children are enforced INSIDE its builder — `<Tabs> children must be
	// <Tab> elements` is a line in components/tabs.go, not a field on an
	// ElementDef — so there is nothing declarative here to consult, and any
	// table this file kept would be a second copy drifting from the first.
	// Building the candidate document asks the only authority there is.
	//
	// Refusing MATTERS because the failure is not local to the insert. A
	// rebuild that errors leaves docRoot nil (see rebuild), which kills
	// click-to-select for the WHOLE document while the previous tree stays
	// on screen looking pressable — and the offending node is then
	// reachable by no gesture at all, because ctrl+n walks siblings and
	// only a click descends. One palette press into the wrong container
	// stranded a live editor exactly that way.
	prev := ed.sel
	// The wrapper, when the container takes the element only through one
	// of its own declared children. The SELECTION stays on what the user
	// asked for, not on the scaffolding: they picked a <Button>, so the
	// properties grid must show the button and not the <Tab> that had to
	// exist to hold it.
	add := n
	if plan.wrap != "" {
		// wrapperNode, NOT ed.seed. A pseudo-element declares no Seed of
		// its own — <Tabs> parses <Tab> itself, so <Tab> has no builder
		// and nothing to seed from — and seeding it produced a bare
		// <Tab/>, which markup.Build refuses with `<Tab> needs a Header`.
		// The transactional revert below then caught that and reported it
		// as the USER's insert being illegal, which it was not. The
		// attributes come from the container's own seed instead; see
		// wrapperNode.
		w := ed.wrapperNode(into.Elem, plan.wrap)
		w.Kids = []*node{n}
		add = w
	}
	// The accelerator, beside the name. A second <Menu> in a <MenuBar>
	// claiming the same alt gesture is unreachable by keyboard, and so is
	// a second <MenuItem> in a <Menu> claiming the same letter — the
	// guard covers BOTH levels since round 11, and these three comments
	// still named only the first. unshadowMnemonic is the one place all
	// three insertion routes share.
	unshadowMnemonic(into, add)
	into.Kids = append(into.Kids, add)
	ed.sel = n
	ed.rebuild()
	// docRoot is the signal rather than a second Build: it is what rebuild
	// sets on success and leaves nil on failure, so this reads the outcome
	// of the build that already ran. Remote mode returns before ever
	// setting it, and there the target app is the authority, not this one.
	if ed.remote == nil && ed.docRoot == nil {
		refused := strings.TrimPrefix(ed.status.Get(), "✗ ")
		into.Kids = into.Kids[:len(into.Kids)-1]
		// THE POP RETAINS, exactly as the delete-splice did: len drops
		// and the refused subtree stays in the vacated slot, reachable
		// from a live parent, with nothing that re-inserts it. Raised in
		// review of #456.
		clear(into.Kids[len(into.Kids):cap(into.Kids)])
		ed.sel = prev
		// BEFORE the rebuild: the refused mutation must not stay on the
		// undo stack, or one ctrl+z re-enters the docRoot==nil state this
		// revert exists to prevent (#454 review).
		ed.abortHistory()
		ed.rebuild()
		// AFTER the second rebuild, which sets the status to "✓ builds":
		// the document is whole again, and the sentence explaining what
		// was refused has to survive it saying so.
		//
		// NEUTRAL VERB, for insertSubtree's reasons — this is the same
		// backstop at a second seam, and a fix applied at one of three
		// identical seams is the shape that leaves the others open.
		// "does not go inside" asserts a parenting cause this line
		// cannot have established: the palette add reaches it when the
		// SEED fails to build and when the document was already broken
		// elsewhere, neither of which is about <into>. It still names
		// both elements, which is the concession canHold's "Permissive
		// where the catalog is silent, because the build is the gate"
		// makes its permissiveness on. Raised in review of #501.
		ed.status.Set("✗ <" + spec.Name + "> was not added to <" + into.Elem +
			">: " + refused)
	}
}

// holdsChildren asks whether an element may contain children at all. An
// element the palette does not describe is treated as a leaf — the safe
// answer, because the cost of being wrong the other way is a silent drop:
// a leaf discards children with no error at load and nothing at runtime,
// so a Button added while a <Text> is selected would simply not exist.
//
// PREFER canHold (addplan.go) FOR ANY NEW CALLER. This function answers a
// coarser question and gets two things wrong for a restricted container:
// it never consults ChildSpec.Only, so it says yes to putting a <Text> in
// a <Tabs>; and it scans ed.palette rather than the catalog, so every
// element loadPalette filters out is unknowable to it. The doc comment
// that used to sit above this one described `addTarget`, which moved to
// addplan.go and now climbs and wraps rather than checking one node and
// its parent; the explanation lives in that file's header.
//
// TWO CORRECTIONS FROM REVIEW OF #454, and the second is the one worth
// reading. The filter used to be `e.Name == "Tab"` and this paragraph
// named <Tab> as "exactly that"; this PR generalised it to `e.Nested`,
// which is three elements today and derived — so naming one was a count
// in prose that had already gone stale.
//
// And it does NOT remain because fit.go asks it. That sentence was here
// and it is false: a grep for this name finds surface_test.go and this
// definition, and nothing else. It is a test-facing predicate — two
// fixtures use it to state "this element is/is not a leaf" before
// asserting anything, and TestEveryPaletteElementIsClassifiedForContainment
// pins the classification across the whole toolbox. That is a real use and
// a smaller one than the old sentence claimed, so it says what it is
// rather than reading like production wiring. It goes when they go.
func (ed *editor) holdsChildren(elem string) bool {
	for _, e := range ed.palette {
		if e.Name != elem {
			continue
		}
		switch e.Children.Mode {
		case markup.ModeLeaf, markup.ModeNone, markup.ModeAttachments:
			return false
		}
		return true
	}
	return false
}

// seed turns a palette entry into the node the canvas will hold, and
// registers whatever bindings that node needs in order to load.
//
// It used to be three tables in THIS FILE, none of them per element, and
// each was wrong in a way that only showed up as an element the user
// could not add:
//
//   - literalFor guessed a value per KIND, with `default: "x"` — which
//     is where "x" came from as the value of every required string;
//   - newHandle switched on GoType and had NO image.Image arm, so
//     <Image> printed "✗ no placeholder for image.Image" and did
//     nothing at all;
//   - a switch on Children.Mode hardcoded Header="tab" for every
//     restricted child, because it was written for <Tabs> — so
//     <MenuBar>'s <Menu>, which wants Title, would not load.
//
// All three are now markup.Seeded, which answers per element and answers
// in MARKUP, because the answer has to cover more than attributes: an
// empty <VStack> measures 0x0 whatever its attributes say, and a 0x0
// element is invisible AND unselectable. The editor's remaining job is
// the two things Seeded cannot know — the address, and the geometry,
// both of which belong to whoever is doing the inserting.
func (ed *editor) seed(spec markup.ElementSpec, name string) (*node, error) {
	if strings.TrimSpace(spec.Seed) == "" {
		// A Components-registered Builder is a func: its attributes and
		// its shape are both unknowable, so the bare element is the only
		// honest seed. Saying that here — rather than letting Seeded's
		// error reach the user — keeps "we know nothing about this
		// element" distinct from "this element's seed is broken".
		return &node{Elem: spec.Name, Attrs: map[string]string{}}, nil
	}
	src, values, err := markup.Seeded(spec, name)
	if err != nil {
		return nil, err
	}
	n, err := nodeOf(src)
	if err != nil {
		return nil, fmt.Errorf("<%s>: %w", spec.Name, err)
	}
	// The editor grows its viewmodel to match — the in-process
	// equivalent of register_properties. ctx.Values and docCtx.Values
	// are the SAME map (see newEditor), so this is one registration and
	// not a choice between two.
	for k, v := range values {
		ed.ctx.Values[k] = v
	}
	return n, nil
}

// deleteSelected removes the selected node from whatever holds it, and
// reports whether the delete STOOD.
//
// The bool is not decoration: cut is copy-then-delete, and a cut that
// reports success for a delete the loader refused leaves the node on the
// page AND on the clipboard, so the next paste duplicates it — with a
// colliding Name, which is the one thing markup.Find cannot resolve.
// promoteSelected and demoteSelected already return this for the same
// reason. Reported in review of #454.
//
// What it selects afterwards is the node that took the deleted one's
// place, or the last one when the end was deleted, or NOTHING when the
// parent is now empty. That last case is the one an index could not
// express: the old code left selected at -1, which meant "the container",
// so deleting the last child silently promoted the selection to the root.
func (ed *editor) deleteSelected() bool {
	n := ed.sel
	if n == nil {
		return false
	}
	p := ed.parentOf(n)
	if p == nil || ed.isSurface(p) {
		// The user's ROOT is not deletable: a document has to have one,
		// and removing it would leave the surface holding nothing while
		// doc() still expected a child. Deleting the surface is not
		// expressible at all — it is not in the outline and cannot be
		// selected.
		return false
	}
	at := unlink(p, n)
	if at < 0 {
		return false
	}
	switch {
	case len(p.Kids) == 0:
		ed.sel = nil
	case at < len(p.Kids):
		ed.sel = p.Kids[at]
	default:
		ed.sel = p.Kids[len(p.Kids)-1]
	}
	ed.rebuild()
	// TRANSACTIONAL, the same way promote, demote, move, paste and add
	// are — and delete was the last mutator without it. A container's
	// legal children are enforced INSIDE its builder, so REMOVING a child
	// can break the parent just as adding an illegal one can:
	// `<Tab Header=… needs exactly one content child, got 0`
	// (markup/toolkit.go:190) fires on one ctrl+x against a node the
	// outline offers you. Without the revert that delete reported success
	// and left docRoot nil, which kills click-to-select for the WHOLE
	// document while the last good tree stays on screen looking pressable
	// — #403's shape, reached through the one door it was not fixed at.
	//
	// The selection goes back to n, not to whatever took its place: the
	// node was not deleted, so leaving the cursor on its neighbour would
	// report the refusal against something the user did not act on.
	if ed.remote == nil && ed.docRoot == nil {
		refused := strings.TrimPrefix(ed.status.Get(), "✗ ")
		insertAt(p, at, n)
		ed.sel = n
		// BEFORE the rebuild, so the rebuild's own recordHistory sees a
		// document identical to the baseline and adds nothing. Without it
		// the refused delete stays on the undo stack and one ctrl+z walks
		// back into the docRoot==nil state this revert exists to prevent.
		ed.abortHistory()
		ed.rebuild()
		ed.status.Set("✗ <" + n.Elem + "> cannot be deleted from <" + p.Elem +
			">: " + refused)
		return false
	}
	return true
}

// retype is the experiment. Changing the container changes which
// ATTACHED properties its children may carry, and the inspector has to
// follow — Canvas.Left is meaningful under a <Canvas> and silently
// dropped under a <VStack>.
func (ed *editor) retype(elem string) {
	// THE USER'S ROOT, not the surface. Both are <Canvas> by default and
	// retyping the wrong one is invisible until you look at the saved
	// file: changing the surface would alter how the workspace lays out
	// and leave the document untouched, which is the exact opposite of
	// what c and v are for.
	root := ed.doc()
	if root.Elem == elem {
		return
	}
	root.Elem = elem
	for _, k := range root.Kids {
		// Attributes the new parent does not contribute are removed
		// rather than left to be ignored. Leaving them is what the old
		// loader did, and it is the defect this whole change deletes.
		//
		// OVER THE CATALOG, and the two reasons stack.
		//
		// Not markup.AttachedParents, which lists BUILTINS: a child
		// retyped out of a third-party container kept that container's
		// attributes, which the new parent discards in silence — the
		// exact defect this loop exists to delete, surviving for every
		// element the host registered.
		//
		// And not ed.palette either, which is the correction round 10
		// asked for and a DIFFERENT distinction from the one above. The
		// palette is what may be PLACED from the toolbox; loadPalette
		// drops every Nested and NonVisual element from it, so a child
		// retyped out of one of those kept its grants — the same silent
		// leftover, through the other filter. This asks what elements
		// DECLARE, so it reads the declarations.
		for _, e := range ed.specs {
			if e.Name == elem {
				continue
			}
			for _, a := range e.Grants.Attached {
				delete(k.Attrs, a.Name)
			}
		}
		// SEEDED BY ROLE, not by name. This was `if elem == "Canvas"`
		// writing "Canvas.Left" and "Canvas.Top" as literals, which
		// hardcoded three things at once: which container offsets its
		// children, and both attribute spellings. A host-registered
		// container granting an offset got no seed at all — its children
		// retyped into it landed stacked at the origin — and renaming
		// either attribute in the catalog would have left this writing
		// the old name with nothing to notice.
		//
		// The grant answers all three, and it is the SAME question the
		// drag's Release asks through the same roles, so the seed and
		// the write cannot disagree about where a child's position
		// lives. Found in review of #390.
		if g := ed.grantOf(elem); g.Kind == markup.GrantOffset {
			x, y := g.Attr(markup.RoleX), g.Attr(markup.RoleY)
			// Both or neither: a grant declaring one axis and not the
			// other cannot place anything, and seeding half of it would
			// write an attribute the loader then rejects for having no
			// partner.
			if x != "" && y != "" {
				if _, ok := k.Attrs[x]; !ok {
					k.Attrs[x], k.Attrs[y] = "2", "1"
				}
			}
		}
	}
	ed.rebuild()
}

// ModeDesign and ModeLive are the status bar's centre section, and they
// are THE SAME WIDTH on purpose — measured, not decorative.
//
// A label that changed width would move the section's bounds, and a
// bounds change vacates cells: the Composer clears the old rect and
// force-repaints everything that sat beneath it, which here is the status
// bar and the page's root Grid. Measured on the shipped page at 160x48,
// that turned a one-component flip into three
// (damage [{0 0 160 48} {0 47 160 1} {48 47 24 1}]). All three repaints
// are correct — they restore what the wider label used to cover — and all
// three are avoidable by not changing width for a word.
//
// TestTheTwoModeLabelsAreTheSameWidth is what stops the next edit
// undoing that silently; TestTheModeFlipRepaintsOnlyTheIndicator is what
// it buys.
const (
	ModeDesign = "DESIGN — d for LIVE"
	ModeLive   = "LIVE — d for DESIGN"
)

// toggleMode flips the designer between a picture and a working UI.
//
// This is the whole switch. There is no re-mount, no rebuild and no second
// tree: the pane's Frozen() reads this property, the Composer observes that
// read, and the frame this Set schedules re-derives the focus order, the
// scoped bindings, the mnemonics, the hover watchers and the Startable set
// before anything paints. The next keystroke or click is already routed the
// new way.
//
// Inverting rather than assigning is what makes it idempotence-safe:
// prop.Set does not compare values, so a Set to the value already held
// would still invalidate the observer — harmless, because the Composer's
// sweep compares the ANSWER, but there is no reason to spend it.
func (ed *editor) toggleMode() { ed.design.Set(!ed.design.Get()) }

// selectNext walks the selection through the root's children — and it is
// the KEYBOARD's whole route to the empty state's exit. With nothing
// selected there is no "next" relative to anything, so it starts at
// whichever end the direction implies, which is what keeps ctrl+n usable
// after a press on bare canvas.
func (ed *editor) selectNext(d int) {
	// Siblings of the current selection, so ctrl+n walks the level you are
	// on rather than always the top. With nothing selected there is no
	// level yet, so it starts at the user's root's children.
	kids := ed.doc().Kids
	if p := ed.parentOf(ed.sel); p != nil && !ed.isSurface(p) {
		kids = p.Kids
	}
	if len(kids) == 0 {
		return
	}
	at := -1
	for i, k := range kids {
		if k == ed.sel {
			at = i
			break
		}
	}
	if at < 0 {
		if d > 0 {
			ed.setSelection(kids[0])
		} else {
			ed.setSelection(kids[len(kids)-1])
		}
		return
	}
	// setSelection, not rebuild: ctrl+n and a click in the designer are the
	// same edit to the same field, and there is no reason for the keyboard
	// to cost a re-mount of the whole designer when the pointer does not.
	ed.setSelection(kids[(at+d+len(kids))%len(kids)])
}

// selectedRow is the inspector row under the cursor, and whether there
// is one.
func (ed *editor) selectedRow() (attrRow, bool) {
	rows := ed.attrRows()
	i := ed.attrSel.Get()
	if i < 0 || i >= len(rows) {
		return attrRow{}, false
	}
	return rows[i], true
}

// beginEdit is enter on a row, and it DISPATCHES BY KIND — which is what
// "behave like the Visual Studio property grid" decomposes into.
//
// The dispatch itself, and every editor it opens, live in properties.go
// and editors.go. What stays here is the binding: <ItemsView Activate=>
// resolves {{.BeginEdit}} to this, and this hands the row to the pane's
// editing surface, which floats the right editor OVER the row rather
// than loading it into a text box in a fixed track at the bottom of the
// panel — the arrangement this replaced, where the value you were
// editing appeared some forty rows from the row you had selected.
func (ed *editor) beginEdit() {
	if ed.props == nil {
		return
	}
	ed.props.Open()
}

// editSelectedAsText is the escape hatch: the raw value in a caret
// editor, whatever the Kind. A per-Kind editor must not be the only way
// in — KindStyle and KindCommand are BindsEither, so their finite lists
// are the common case and not the whole grammar.
func (ed *editor) editSelectedAsText() {
	if ed.props == nil {
		return
	}
	ed.props.OpenAsText()
}

// describe fills the description pane: Doc where the catalog has prose,
// the legal values where it does not — so it is never blank while Doc is
// unpopulated. The category and the default ride along here rather than
// as two more columns: the pane has room and a 46-cell list does not.
func (ed *editor) describe(r attrRow) {
	head := r.cat + " · " + r.name
	if d := r.doc; d != "" {
		ed.editDoc.Set(head + " — " + d)
		return
	}
	tail := r.legal
	if r.def != "" {
		if tail != "" {
			tail += ", "
		}
		tail += "default " + r.def
	}
	if n := len(r.cycle()); n > 0 {
		if tail != "" {
			tail += ", "
		}
		tail += fmt.Sprintf("enter cycles %d", n)
	}
	if tail != "" {
		ed.editDoc.Set(head + ": " + tail)
		return
	}
	ed.editDoc.Set(head)
}

// commitEdit is what the caret editor's Changed binding runs: every
// keystroke in the floated TextBox writes the attribute, so the document
// follows the key rather than waiting for enter.
//
// It routes through valueEditor.Write, which is the pane's ONE mutation
// seam — the target is re-resolved rather than cached (undo replaces the
// node tree wholesale, so a captured pointer dangles) and the write ends
// in ed.rebuild(), which is the choke point undo, the preview, the
// outline and the CODE tab all hang off.
func (ed *editor) commitEdit() {
	if ed.props == nil {
		return
	}
	ed.props.Write(ed.editValue.Get())
}

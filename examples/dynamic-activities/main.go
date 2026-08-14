// dynamic-activities: a gooey app whose buttons run Python that did not
// exist when the app started.
//
//	cd examples/dynamic-activities && go run .
//
// # The loop
//
// One Python process is both a Temporal worker and an MCP server. Its
// MCP tools are CRUD over ACTIVITIES: create_activity takes a name and a
// blob of Python source, execs it into a callable, and puts it in a
// runtime registry that a single dynamic activity dispatcher
// (@activity.defn(dynamic=True)) answers from. There is no per-activity
// Python function to deploy and no worker restart — the activity exists
// the moment the tool call returns.
//
// Having created it, the same tool call reaches back into THIS app over
// the control-plane gRPC (gooey.control.v1) and:
//
//   - RegisterProperties a result property for it, Activity.<Name>.Result;
//   - PatchMarkup the ActivityList element with a button per activity,
//     each bound to {{temporal:Activity `<Name>` .Input | into
//     .Activity.<Name>.Result}};
//   - SetProperty Selected, so the big star button — bound to
//     {{temporal:Activity .Selected .Input | into .Output}}, an activity
//     type name read from a PROPERTY rather than a literal — runs the
//     newest activity without any rebinding at all.
//
// delete_activity is the exact inverse, and the last step is the
// UnregisterNames RPC this demo is the reason for: without it every
// invented name leaks for the life of the process and list_values grows
// monotonically into noise.
//
// So: commands still cannot be registered over the control plane —
// behavior needs code, not storage — but the temporal: handler namespace
// means markup can bind an activity CALL directly, and the activity is
// the behavior. That is the whole unlock.
//
// # DANGER: this is remote code execution, on purpose
//
// create_activity runs arbitrary Python, supplied by whoever can reach
// the MCP endpoint, in the worker process with that process's full
// privileges. There is no sandbox and none is planned here: the point of
// the demo is the round trip, and pretending otherwise would be worse
// than saying it plainly. Both servers this app starts, and the Python
// MCP server it launches, are loopback-only and unauthenticated. Do not
// expose any of them, do not run this on a shared host, and do not point
// it at anything you care about.
//
// # The two servers this app starts
//
// -mcp is gooey's own MCP endpoint (drive the UI as an agent would).
// -grpc is the control plane the Python side talks to. Both default to
// port 0, and both addresses are read back from the LISTENER, never from
// the flag, so several instances coexist and the companion always gets
// the port that was actually bound.
package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/graphics"
	gooeygrpc "github.com/WonderForgeLabs/gooey/grpc"
	// SVG rasterization is opt-in: a separate module so oksvg/rasterx stay
	// out of core gooey's dependency graph. Blank-importing it registers
	// the format with the imaging registry, which is what lets an
	// agent-authored <Image Src="diagram.svg"> resolve here — vector
	// artwork rather than block glyphs.
	_ "github.com/WonderForgeLabs/gooey/imagefmt/svg"
	temporalhandlers "github.com/WonderForgeLabs/gooey/handlers/temporal"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/mcp"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
	"go.temporal.io/sdk/client"
)

// zoomBy steps an image's cell size, keeping the two axes in the ~2:1
// ratio that makes a terminal cell roughly square, so zooming does not
// stretch the picture. It clamps rather than refusing: a button that goes
// dead at the limit is worse to use than one that stops moving, and there
// is no room on a pill to explain why it declined.
//
// The step is multiplicative because the useful range spans an order of
// magnitude — 20 cells wide to 240 — and a fixed increment is either
// uselessly slow at the top or unusably coarse at the bottom.
func zoomBy(cols, rows *prop.Property[int], dir int) gooey.Command {
	const (
		minCols, maxCols = 20, 400
		factorNum        = 5 // 1.25x per press
		factorDen        = 4
	)
	return gooey.Command(func() {
		c := cols.Get()
		if dir > 0 {
			c = c * factorNum / factorDen
		} else {
			c = c * factorDen / factorNum
		}
		c = min(max(c, minCols), maxCols)
		cols.Set(c)
		// Cells are about twice as tall as they are wide, so half the
		// columns is a square-ish picture.
		rows.Set(max(c/2, 4))
	})
}

// encoderFor maps the -graphics flag to an encoder; "" means let the
// terminal's capabilities decide, and "halfblock" means the cell-plane
// fallback (a nil encoder), which is what an unforced app degrades to.
func encoderFor(mode string) (graphics.Encoder, error) {
	switch mode {
	case "kitty":
		return graphics.Kitty{}, nil
	case "sixel":
		return graphics.Sixel{}, nil
	case "iterm2":
		return graphics.ITerm2{}, nil
	case "halfblock":
		return nil, nil
	}
	return nil, fmt.Errorf("dynamic-activities: unknown -graphics %q; want kitty, sixel, iterm2 or halfblock", mode)
}

// checkLoopback refuses a bind that is not loopback. gooeygrpc.Serve
// enforces this itself; the MCP side is checked here because this app
// hands mcp.Serve an address, and because the Python companion's own MCP
// server takes one too — the danger is identical for all three: no
// authentication, and a control handle on this terminal (and, for the
// Python one, on this machine).
func checkLoopback(what, addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("dynamic-activities: bad %s address %q: %w", what, addr, err)
	}
	if host == "" {
		return fmt.Errorf("dynamic-activities: %s %q binds every interface; loopback only (use 127.0.0.1:port)", what, addr)
	}
	if host == "localhost" {
		return nil
	}
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("dynamic-activities: %s %q is not a loopback address; these servers have no authentication", what, addr)
	}
	return nil
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// sourceDir resolves the directory holding this demo's markup: the
// working directory under `go run .`, else the built binary's own
// directory. It is also what ctx.Includes and ctx.Dir are rooted at.
func sourceDir(marker string) string {
	if fileExists(marker) {
		return "."
	}
	exe, err := os.Executable()
	if err != nil {
		return "."
	}
	return filepath.Dir(exe)
}

// activityNames splits the Python-owned Activities property — the
// registry as this app sees it. The Go side deliberately keeps NO
// registry of its own: the worker owns the activities, and this string
// is the view it publishes.
func activityNames(joined string) []string {
	var out []string
	for _, n := range strings.Split(joined, "\n") {
		if n = strings.TrimSpace(n); n != "" {
			out = append(out, n)
		}
	}
	return out
}

func main() {
	mcpAddr := flag.String("mcp", "127.0.0.1:0", "loopback address for this app's MCP server; port 0 picks a free port; empty disables it")
	grpcAddr := flag.String("grpc", "127.0.0.1:0", "loopback address for the control-plane gRPC server; port 0 picks a free port; empty disables it")
	temporalAddr := flag.String("temporal", "127.0.0.1:7233", "Temporal frontend address")
	taskQueue := flag.String("task-queue", "gooey-dynamic-activities", "Temporal task queue the worker polls and the star button schedules on")
	withWorker := flag.Bool("with-worker", true, "launch the Python worker + activity MCP server as a companion sharing this app's process lifetime")
	workerPython := flag.String("worker-python", "python3", "python interpreter for the companion; point it at a venv's bin/python if system python lacks requirements.txt")
	activityMCP := flag.String("activity-mcp", "127.0.0.1:7802", "loopback address the COMPANION's activity-CRUD MCP server binds")
	page := flag.String("page", "dynamicactivities.gooey", "which .gooey page to load from the example directory")
	graphicsMode := flag.String("graphics", "", "force the image protocol: kitty|sixel|iterm2|halfblock; empty lets terminal capabilities decide")
	flag.Parse()

	dir := sourceDir(*page)

	// --- viewmodel. Everything the star needs is a plain source property;
	// what makes this demo different is only that .Selected is read as the
	// ACTIVITY TYPE NAME rather than as an argument.
	input := prop.NewSource("hello dynamic world")
	output := prop.NewSource("press the star — the result of the selected activity lands here")
	selected := prop.NewSource("")
	activities := prop.NewSource("")
	note := prop.NewSource("create one: call create_activity on the companion's MCP endpoint")

	starColor := prop.NewSource(render.RGB(255, 170, 60))
	starGlow := prop.NewSource(render.RGB(255, 214, 120))

	// The size of whatever <Image> an agent puts on the page. Held as two
	// plain ints so generated markup can bind Cols/Rows to them, and so
	// zooming is a Set rather than a re-patch — which matters because a
	// patch rebuilds the subtree and takes the caret and any component
	// state in it with it (mcp/focuspatch_test.go pins that behavior).
	diagCols := prop.NewSource(120)
	diagRows := prop.NewSource(32)

	selectedLine := prop.NewComputed(func() string {
		if s := selected.Get(); s != "" {
			return "selected: " + s
		}
		return "selected: (none yet)"
	})

	statusLine := prop.NewSource("")

	// Cycle walks the Python-published list. It is the only reason this
	// app parses Activities at all — the registry itself stays remote.
	cycle := gooey.Command(func() {
		names := activityNames(activities.Get())
		if len(names) == 0 {
			return
		}
		cur := selected.Get()
		for i, n := range names {
			if n == cur {
				selected.Set(names[(i+1)%len(names)])
				return
			}
		}
		selected.Set(names[0])
	})

	var app *gooey.App
	clear := gooey.Command(func() { output.Set("") })

	ctx := &markup.Context{
		Values: map[string]any{
			"Input":        input,
			"Output":       output,
			"Selected":     selected,
			"SelectedLine": selectedLine,
			"Activities":   activities,
			"Note":         note,
			"Status":       statusLine,
			"StarColor":    starColor,
			"StarGlow":     starGlow,
			"Cycle":        cycle,
			"Clear":        clear,
			"Quit":         gooey.Command(func() { app.Quit() }),

			// Zoom for an <Image> an agent placed on the page. The SIZE is
			// two ordinary int properties, so agent-authored markup binds
			// Cols/Rows to them; the COMMANDS have to be here, because the
			// control plane registers properties and not behavior — an
			// attached client can grow the viewmodel's state but cannot put
			// a closure in it. So the app supplies the verbs and the agent
			// supplies the page that uses them.
			"DiagCols": diagCols,
			"DiagRows": diagRows,
			"ZoomIn":   zoomBy(diagCols, diagRows, +1),
			"ZoomOut":  zoomBy(diagCols, diagRows, -1),
		},
		Styles: map[string]render.Style{
			"panel":  {Fg: render.RGB(120, 90, 220)},
			"accent": {Fg: render.RGB(255, 170, 60), Bold: true},
			"dim":    {Fg: render.RGB(140, 140, 150)},
		},
		// Includes is what makes AGENT-AUTHORED markup able to load assets.
		// A page loaded from an fs.FS resolves <Image Src="…"> against that
		// FS, but only for the duration of the load; markup that arrives
		// later as bytes (swap_markup, patch_markup) has no document FS at
		// all and falls back to Includes (markup.Context.assets). Without
		// this line every <Image> in a patched fragment fails to build.
		Includes: os.DirFS(dir),
		// Dir is the host-side path a <Companion> would resolve against.
		Dir: dir,
	}

	// --- Temporal. The client is a capability grant: registering the
	// provider is what lets any markup in this process — including markup
	// patched in later by the Python side — bind {{temporal:Activity …}}.
	tc, err := client.Dial(client.Options{HostPort: *temporalAddr, Logger: temporalhandlers.NopLogger})
	if err != nil {
		gooey.Exit(fmt.Errorf("dynamic-activities: cannot reach Temporal at %s: %w\n"+
			"start one with: temporal server start-dev", *temporalAddr, err))
	}
	markup.RegisterHandlers(temporalhandlers.URI, temporalhandlers.New(tc, *taskQueue))

	// The graphics protocol is a property of the PAGE, so the page states
	// it: <Gooey Graphics="sixel">. The flag is only the fallback for a
	// document that says nothing, which is why the document wins — a page
	// built around a detailed SVG wants real pixels wherever it runs, and
	// that fact belongs next to the artwork rather than in whatever command
	// line happened to start the process.
	//
	// Forcing matters at all because detection answers for the terminal
	// that LAUNCHED the app: started from a script, a recording pty or a
	// supervisor, it inherits that terminal's answer rather than the one
	// you are looking at.
	settings, err := markup.ReadPageSettings(os.DirFS(dir), *page)
	if err != nil {
		gooey.Exit(err)
	}
	mode := settings.Graphics
	if mode == "" {
		mode = *graphicsMode
	}
	//
	// Pinning is all it takes: App.caps supplies the assumed cell size a
	// pinned protocol needs, so "sixel" here emits real pixels rather than
	// the zero-sized image a missing CellW used to produce.
	var appOpts []gooey.Option
	if mode != "" {
		enc, err := encoderFor(mode)
		if err != nil {
			gooey.Exit(err)
		}
		appOpts = append(appOpts, gooey.WithGraphics(enc))
	} else {
		// "Let capabilities decide" is a probe, not an absence: without one
		// an app has a color depth and no graphics answer at all, so every
		// image would silently take the halfblock path no matter what the
		// terminal can do.
		appOpts = append(appOpts, gooey.WithCapabilityProbe())
	}
	app = gooey.NewApp(markup.Page(os.DirFS(dir), *page, ctx), appOpts...)
	// Handler results (an activity completing on some worker) land on the
	// run loop's goroutine through this — the confinement rule.
	ctx.Dispatcher = app.Dispatcher()

	var mcpURL, grpcURL string

	if *mcpAddr != "" {
		if err := checkLoopback("-mcp", *mcpAddr); err != nil {
			gooey.Exit(err)
		}
		srv, err := mcp.Serve(app, mcp.Options{
			Addr:    *mcpAddr,
			Context: ctx,
			Name:    "gooey-dynamic-activities",
		})
		if err != nil {
			gooey.Exit(err)
		}
		defer srv.Close()
		mcpURL = srv.URL()
	}

	if *grpcAddr != "" {
		// Serve checks loopback itself and returns the bound address, so
		// -grpc 127.0.0.1:0 still yields a usable GOOEY_GRPC_ADDR.
		srv, err := gooeygrpc.Serve(app, gooeygrpc.Options{
			Addr:    *grpcAddr,
			Context: ctx,
			Doc:     func() []byte { b, _ := os.ReadFile(filepath.Join(dir, "dynamicactivities.gooey")); return b },
			Name:    "gooey-dynamic-activities",
			Version: "1",
		})
		if err != nil {
			gooey.Exit(err)
		}
		defer srv.Close()
		grpcURL = srv.Addr()
	}

	if *withWorker {
		if grpcURL == "" {
			gooey.Exit(fmt.Errorf("dynamic-activities: -with-worker needs the control plane it registers names through; do not pass -grpc \"\""))
		}
		if err := checkLoopback("-activity-mcp", *activityMCP); err != nil {
			gooey.Exit(err)
		}
		logPath := filepath.Join(dir, "worker.log")
		logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			gooey.Exit(fmt.Errorf("dynamic-activities: cannot open worker log %s: %w", logPath, err))
		}
		// Held open for the app's lifetime: app.Run returns only after
		// teardown has stopped and waited for every companion.
		defer logFile.Close()

		// Prefer the demo's own venv when the flag is untouched — system
		// python3 almost never has temporalio, mcp and gooey-control, and a
		// companion that cannot import them exits, which takes the app down
		// with it. An explicit -worker-python always wins.
		python := *workerPython
		if python == "python3" {
			if venv := filepath.Join(dir, ".venv", "bin", "python"); fileExists(venv) {
				python = venv
			}
		}
		cmd := exec.Command(python, "worker.py")
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GOOEY_GRPC_ADDR="+grpcURL,
			"GOOEY_ACTIVITY_MCP_ADDR="+*activityMCP,
			"TEMPORAL_ADDRESS="+*temporalAddr,
			"TEMPORAL_TASK_QUEUE="+*taskQueue,
		)
		app.AddCompanion(gooey.CompanionCmd("activity-worker", cmd, gooey.CompanionOutput(logFile)))
		note.Set("create one: call create_activity on http://" + *activityMCP + "/mcp  (worker log: " + logPath + ")")
	}

	status := "temporal " + *temporalAddr + " queue " + *taskQueue
	if grpcURL != "" {
		status += " · grpc " + grpcURL
	}
	if mcpURL != "" {
		status += " · mcp " + mcpURL
	}
	if *withWorker {
		status += " · activities " + *activityMCP
	}
	statusLine.Set(status + "   ctrl+n: next activity   ctrl+l: clear   ctrl+c: quit")

	err = app.Run(context.Background())
	tc.Close()
	gooey.Exit(err)
}

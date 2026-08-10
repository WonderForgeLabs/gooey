// kanbandemo: a real Kanban board — Todo, Doing, Done — built on
// components.ItemsView, driven live over MCP.
//
// Every column is backed by an ordinary Go slice (*prop.Property[[]Card]);
// components.Items adapts it to the ItemSource each ItemsView reads.
// Adding, moving and removing cards mutate that slice and Set it back,
// which is the whole mechanism — the ItemsView windowing and row reuse
// in components/itemsview.go does the rest.
//
//	cd examples/kanbandemo && go run . -mcp 127.0.0.1:7778
//
// It lives in its own module for the same reason mcp/cmd/mcpdemo does:
// importing gooey/mcp pulls in the MCP SDK's dependency graph, and core
// gooey's `go build ./...` / `go test ./...` should never see it.
//
// # The worker companion
//
// examples/temporal-worker is a Python Temporal worker that pushes
// generated markup into a running gooey app's swap_markup — a second
// shell, hand-started and hand-killed, the same operational annoyance
// docs/specs/2026-08-10-companions.md describes for the Temporal wizard
// demo. -with-worker collapses it into this one, the same way
// cmd/wizardui --with-dev-server does for its own sidecar: the worker
// becomes a gooey.CompanionCmd, started before the first frame and
// killed (process group, not just the direct child) when this app quits.
// It is opt-in — the worker needs a Python venv with the deps in
// examples/temporal-worker/requirements.txt and a reachable Temporal
// server, neither of which the base demo should require:
//
//	cd examples/kanbandemo && go run . -mcp 127.0.0.1:7778 -with-worker \
//	    -worker-python /path/to/.venv/bin/python
//
// # Tabs and the MCP traffic log
//
// The bottom panel is a hand-rolled tab switcher local to this demo (not
// a new framework component): a "mcp" tab with the endpoint + tool-usage
// text this file always had, and a "log" tab showing every raw MCP
// request/response this server has handled, live. ctrl+t (or clicking
// the [ MCP ]/[ Log ] header buttons) flips ActiveTab, and each panel's
// own Visibility="{{...}}" binding — a *prop.Property[bool] through
// layout.go's BindVisibilityBool — is the whole switching mechanism; no
// structural rebuild. The log tab's ItemsView is Go-composed (like
// cmd/logview) and registered as the "LogPanel" markup element because
// its Scroll (tail-anchored) field has no markup attribute yet.
//
// Traffic capture is pure HTTP-layer instrumentation: mcpTrafficLogger
// wraps mcp.New's Handler() (kanbandemo switched from the mcp.Serve
// convenience to mcp.New + its own net.Listener/http.Server so it could
// wrap the handler at all) and tees the request body and response body
// into the log. It runs on net/http goroutines, never the UI loop —
// appendLogEntry marshals the append back through app.Post, same as any
// other async source per the framework's confinement rule.
package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/mcp"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

// Card is a task. ID exists so a future template could show it; today
// only Title is projected onto the row.
type Card struct {
	ID    int
	Title string
}

func cardFields(c Card) map[string]any {
	return map[string]any{"Title": c.Title}
}

// logEntry is one captured MCP request or response — this demo's own
// local state, in the mold of cmd/logview's `line` struct, not a
// reusable framework type.
type logEntry struct {
	Time time.Time
	Dir  string // "in" (request) or "out" (response)
	Text string // the full raw JSON-RPC body, never truncated
}

// maxLogEntries bounds the captured log; oldest entries drop first.
const maxLogEntries = 200

// logDisplayTruncate caps how many bytes of a log entry's raw text show
// in its row — the captured Text itself (logEntry.Text) is kept whole.
const logDisplayTruncate = 500

var (
	logInStyle  = render.Style{Fg: render.RGB(120, 200, 140)}
	logOutStyle = render.Style{Fg: render.RGB(255, 170, 60)}
)

// projectLogEntry is the ItemsView projection: what a captured message
// looks like as one row. Mirrors cmd/logview's projectLine.
func projectLogEntry(e logEntry) map[string]any {
	text := strings.ReplaceAll(e.Text, "\n", " ")
	if n := len(text); n > logDisplayTruncate {
		text = fmt.Sprintf("%s...(%d more bytes)", text[:logDisplayTruncate], n-logDisplayTruncate)
	}
	arrow, style := "← out", logOutStyle
	if e.Dir == "in" {
		arrow, style = "→ in ", logInStyle
	}
	return map[string]any{
		"Text":  fmt.Sprintf("%s %s %s", e.Time.Format("15:04:05.000"), arrow, text),
		"Style": style,
	}
}

// logEntryTemplate is the Go spelling of an <ItemsView.ItemTemplate>: one
// row is one Text bound to the row's live handles. Mirrors cmd/logview's
// lineTemplate.
func logEntryTemplate(values map[string]any) (gooey.Component, error) {
	text, ok := values["Text"].(*prop.Property[string])
	if !ok {
		return nil, fmt.Errorf("Text is %T", values["Text"])
	}
	style, ok := values["Style"].(*prop.Property[render.Style])
	if !ok {
		return nil, fmt.Errorf("Style is %T", values["Style"])
	}
	return &components.Text{Content: text, Style: style}, nil
}

// respCapture tees everything written to an http.ResponseWriter into a
// buffer while still writing it through. gooey/mcp's streamable-HTTP
// handler runs in JSONResponse mode — one-shot application/json, never a
// held-open stream — so there is exactly one write to tee, which is what
// makes this simple tee viable at all.
type respCapture struct {
	http.ResponseWriter
	buf bytes.Buffer
}

func (r *respCapture) Write(b []byte) (int, error) {
	r.buf.Write(b)
	return r.ResponseWriter.Write(b)
}

// mcpTrafficLogger wraps the MCP endpoint's handler with pure HTTP-layer
// instrumentation: it buffers the request body and replaces r.Body so the
// real handler still reads it, then tees the response through
// respCapture. It has nothing to do with the terminal or the UI-goroutine
// confinement rule except respecting it — capture runs on whatever
// net/http goroutine served the request, and posts back through the UI
// loop rather than touching a property directly.
func mcpTrafficLogger(next http.Handler, capture func(dir, text string)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody []byte
		if r.Body != nil {
			reqBody, _ = io.ReadAll(r.Body)
			r.Body.Close()
			r.Body = io.NopCloser(bytes.NewReader(reqBody))
		}
		if len(reqBody) > 0 {
			capture("in", string(reqBody))
		}
		rc := &respCapture{ResponseWriter: w}
		next.ServeHTTP(rc, r)
		if rc.buf.Len() > 0 {
			capture("out", rc.buf.String())
		}
	})
}

// checkLoopbackAddr is a light stand-in for gooey/mcp's own unexported
// checkLoopback. mcp.New's doc says the loopback guarantee becomes the
// host's problem once it owns the listener itself, which switching to
// mcp.New (below, so the handler can be wrapped) now makes this app's.
// Same posture as the package this replaces: v1 MCP has no
// authentication, so a non-loopback bind is a remote-control handle on
// this terminal.
func checkLoopbackAddr(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("kanbandemo: bad -mcp address %q: %w", addr, err)
	}
	if host == "" {
		return fmt.Errorf("kanbandemo: -mcp %q binds every interface; loopback only (use 127.0.0.1:port)", addr)
	}
	if host == "localhost" {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("kanbandemo: -mcp %q is not a loopback address; this server has no authentication", addr)
	}
	return nil
}

func main() {
	addr := flag.String("mcp", "127.0.0.1:7778", "loopback address for the MCP server; empty disables it")
	withWorker := flag.Bool("with-worker", false, "launch the Python Temporal dynamic-UI worker (examples/temporal-worker) as a companion, sharing this app's process lifetime")
	workerPython := flag.String("worker-python", "python3", "python interpreter for the worker companion; point it at a venv's bin/python if system python lacks examples/temporal-worker/requirements.txt")
	workerTaskQueue := flag.String("worker-task-queue", "kanbandemo-dynamic-ui", "Temporal task queue the worker companion polls")
	flag.Parse()

	// --- board state: three plain slices, nothing fancier. Moving a card
	// is "remove from one slice, append to another, Set both" — the
	// ItemsView on each side repaints because Items() reads the slice
	// inside a computed.
	todo := prop.NewSource([]Card{
		{ID: 1, Title: "Write kanban demo"},
		{ID: 2, Title: "Wire up MCP"},
	})
	doing := prop.NewSource([]Card{
		{ID: 3, Title: "Pick component approach"},
	})
	done := prop.NewSource([]Card{
		{ID: 4, Title: "Confirm ItemsView exists"},
	})
	nextID := 5

	todoSel := prop.NewSource(0)
	doingSel := prop.NewSource(0)
	doneSel := prop.NewSource(0)

	newTitle := prop.NewSource("")

	// move takes the selected card out of src and appends it to dst,
	// keeping srcSel pointing at a valid index (or 0 on an empty list).
	// A no-op when nothing is selected, which is the only guard it needs:
	// Selected is already clamped into [0, len) by the ItemsView itself.
	move := func(src, dst *prop.Property[[]Card], srcSel *prop.Property[int]) gooey.Action {
		return gooey.Command(func() {
			cards := src.Get()
			i := srcSel.Get()
			if i < 0 || i >= len(cards) {
				return
			}
			moved := cards[i]
			rest := make([]Card, 0, len(cards)-1)
			rest = append(rest, cards[:i]...)
			rest = append(rest, cards[i+1:]...)
			src.Set(rest)
			dst.Set(append(append([]Card{}, dst.Get()...), moved))
			if i >= len(rest) {
				i = len(rest) - 1
			}
			if i < 0 {
				i = 0
			}
			srcSel.Set(i)
		})
	}

	remove := func(src *prop.Property[[]Card], srcSel *prop.Property[int]) gooey.Action {
		return gooey.Command(func() {
			cards := src.Get()
			i := srcSel.Get()
			if i < 0 || i >= len(cards) {
				return
			}
			rest := make([]Card, 0, len(cards)-1)
			rest = append(rest, cards[:i]...)
			rest = append(rest, cards[i+1:]...)
			src.Set(rest)
			if i >= len(rest) {
				i = len(rest) - 1
			}
			if i < 0 {
				i = 0
			}
			srcSel.Set(i)
		})
	}

	addTask := gooey.Command(func() {
		title := strings.TrimSpace(newTitle.Get())
		if title == "" {
			return
		}
		nextID++
		todo.Set(append(append([]Card{}, todo.Get()...), Card{ID: nextID, Title: title}))
		newTitle.Set("")
	})

	// --- tab state: which of the bottom panel's two tabs is showing. A
	// hand-rolled switcher local to this demo (see the package doc) — no
	// new framework component, no structural rebuild. Each panel's own
	// Visibility="{{...}}" binds to a *prop.Property[bool]
	// (layout.go's BindVisibilityBool, the XAML
	// BooleanToVisibilityConverter default: true is Visible, false is
	// Collapsed), so the switch IS the mechanism.
	activeTab := prop.NewSource("mcp") // "mcp" | "log"
	mcpTabVisible := prop.NewComputed(func() bool { return activeTab.Get() == "mcp" })
	logTabVisible := prop.NewComputed(func() bool { return activeTab.Get() == "log" })
	showMcpTab := gooey.Command(func() { activeTab.Set("mcp") })
	showLogTab := gooey.Command(func() { activeTab.Set("log") })
	toggleTab := gooey.Command(func() {
		if activeTab.Get() == "log" {
			activeTab.Set("mcp")
		} else {
			activeTab.Set("log")
		}
	})

	// --- log tab: every MCP request/response, captured by
	// mcpTrafficLogger (below) through appendLogEntry.
	logEntries := prop.NewSource([]logEntry{})
	logScroll := prop.NewSource(0)

	help := prop.NewSource("")

	panelStyle := render.Style{Fg: render.RGB(120, 90, 220)}
	accentStyle := render.Style{Fg: render.RGB(255, 170, 60), Bold: true}
	dimStyle := render.Style{Fg: render.RGB(140, 140, 150)}

	mcpTabStyle := prop.NewComputed(func() render.Style {
		if activeTab.Get() == "mcp" {
			return accentStyle
		}
		return dimStyle
	})
	logTabStyle := prop.NewComputed(func() render.Style {
		if activeTab.Get() == "log" {
			return accentStyle
		}
		return dimStyle
	})

	var app *gooey.App

	ctx := &markup.Context{
		Values: map[string]any{
			"NewTitle": newTitle,
			"AddTask":  addTask,

			"TodoItems": components.Items(todo, cardFields),
			"TodoSel":   todoSel,

			"DoingItems": components.Items(doing, cardFields),
			"DoingSel":   doingSel,

			"DoneItems": components.Items(done, cardFields),
			"DoneSel":   doneSel,

			"TodoMoveRight": move(todo, doing, todoSel),
			"TodoRemove":    remove(todo, todoSel),

			"DoingMoveLeft":  move(doing, todo, doingSel),
			"DoingMoveRight": move(doing, done, doingSel),
			"DoingRemove":    remove(doing, doingSel),

			"DoneMoveLeft": move(done, doing, doneSel),
			"DoneRemove":   remove(done, doneSel),

			"Help": help,
			"Quit": gooey.Command(func() { app.Quit() }),

			"ActiveTab":     activeTab,
			"ShowMcpTab":    showMcpTab,
			"ShowLogTab":    showLogTab,
			"ToggleTab":     toggleTab,
			"McpTabVisible": mcpTabVisible,
			"LogTabVisible": logTabVisible,
			"McpTabStyle":   mcpTabStyle,
			"LogTabStyle":   logTabStyle,
		},
		Styles: map[string]render.Style{
			"panel":  panelStyle,
			"accent": accentStyle,
			"dim":    dimStyle,
		},
		Components: map[string]markup.Builder{
			// LogPanel is Go-composed rather than authored in
			// kanbandemo.gooey because ItemsView.Scroll (the
			// tail-anchored scroll mode cmd/logview uses) has no markup
			// attribute yet — markup/itemsview.go only wires Items,
			// Selected, Activate, SelectionChanged and Focusable. This
			// mirrors cmd/markuplog's LogPane: a custom element whose
			// Builder returns a hand-built subtree, registered only in
			// this app's own ctx.Components.
			"LogPanel": func(markup.Element, *markup.Context) (gooey.Component, error) {
				return &components.Border{
					Title: components.Str("log"),
					Style: components.Sty(panelStyle),
					Child: &components.ItemsView{
						Items:    components.Items(logEntries, projectLogEntry),
						Scroll:   logScroll,
						Template: logEntryTemplate,
					},
				}, nil
			},
		},
	}

	dir := "."
	if _, err := os.Stat(filepath.Join(dir, "kanbandemo.gooey")); err != nil {
		exe, _ := os.Executable()
		dir = filepath.Dir(exe)
	}

	app = gooey.NewApp(markup.Page(os.DirFS(dir), "kanbandemo.gooey", ctx))

	// appendLogEntry is the only thing mcpTrafficLogger calls. It runs on
	// whatever net/http goroutine served the request, so it may not touch
	// logEntries directly (properties are UI-goroutine-confined) —
	// app.Post marshals the append onto the loop, same as any other async
	// source per the framework's Dispatcher.
	appendLogEntry := func(direction, text string) {
		app.Post(func() {
			cur := logEntries.Get()
			next := append(append([]logEntry{}, cur...), logEntry{Time: time.Now(), Dir: direction, Text: text})
			if len(next) > maxLogEntries {
				next = next[len(next)-maxLogEntries:]
			}
			logEntries.Set(next)
		})
	}

	if *withWorker && *addr == "" {
		gooey.Exit(fmt.Errorf("kanbandemo: -with-worker needs the MCP endpoint it pushes markup into; do not pass -mcp \"\""))
	}

	if *addr != "" {
		if err := checkLoopbackAddr(*addr); err != nil {
			gooey.Exit(err)
		}
		// mcp.New builds the server without listening — unlike the
		// mcp.Serve convenience this demo used before — so this app can
		// own the net.Listener and http.Server and wrap srv.Handler()
		// with mcpTrafficLogger before anything is served. That
		// wrapping is the only reason for the switch.
		srv, err := mcp.New(app, mcp.Options{
			Context: ctx,
			Name:    "gooey-kanbandemo",
		})
		if err != nil {
			gooey.Exit(err)
		}
		ln, err := net.Listen("tcp", *addr)
		if err != nil {
			gooey.Exit(fmt.Errorf("kanbandemo: listen %s: %w", *addr, err))
		}
		// srv.URL()/srv.Addr() read a listener mcp.New never sets (only
		// mcp.Serve does), so the endpoint this app actually bound is
		// built from ln instead.
		mcpURL := "http://" + ln.Addr().String() + "/mcp"
		httpSrv := &http.Server{Handler: mcpTrafficLogger(srv.Handler(), appendLogEntry)}
		go httpSrv.Serve(ln)
		defer httpSrv.Close()
		helpText := "MCP endpoint: " + mcpURL + "\n\n" +
			"tools/call list_values      — TodoItems/DoingItems/DoneItems (lists), TodoSel/DoingSel/DoneSel (int), NewTitle (string),\n" +
			"                               ActiveTab (string, \"mcp\"/\"log\")\n" +
			"tools/call invoke_command   — {\"name\": \"AddTask\"} (after set_value NewTitle), or TodoMoveRight/DoingMoveLeft/\n" +
			"                               DoingMoveRight/DoneMoveLeft/TodoRemove/DoingRemove/DoneRemove/ShowMcpTab/ShowLogTab/ToggleTab\n" +
			"tools/call set_value        — {\"name\": \"NewTitle\", \"value\": \"typed by an agent\"} or {\"name\": \"ActiveTab\", \"value\": \"log\"}\n" +
			"tools/call focus/send_keys  — {\"name\": \"NewTitle\"} then {\"text\": \"buy milk\"}; or focus a list, then keys: [\"down\"]\n" +
			"tools/call tree_snapshot    — Name= identities: NewTitle, AddBtn, TodoList, DoingList, DoneList, McpTabBtn, LogTabBtn,\n" +
			"                               LogPanel, and each column's buttons\n\n" +
			"tab: ctrl+t (or click [ MCP ] / [ Log ]) switches this panel between this help text and a live log of every raw\n" +
			"MCP request/response this server has handled — including the very call that is reading this."

		if *withWorker {
			// examples/temporal-worker relative to kanbandemo's own source
			// directory, not whatever cwd this binary happened to launch
			// from — dir already resolved that split above (`.` under
			// `go run`, the executable's directory otherwise).
			workerDir := filepath.Join(dir, "..", "temporal-worker")
			logPath := filepath.Join(workerDir, "kanbandemo-worker.log")
			logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
			if err != nil {
				gooey.Exit(fmt.Errorf("kanbandemo: cannot open worker log %s: %w", logPath, err))
			}
			// Held open for the app's lifetime, not per-write: app.Run
			// returns only after teardown has stopped and waited for every
			// companion (docs/specs/2026-08-10-companions.md, "Teardown"),
			// so the worker is done writing by the time this defer fires.
			defer logFile.Close()

			cmd := exec.Command(*workerPython, "worker.py")
			cmd.Dir = workerDir
			// Forward the parent's full environment — any ANTHROPIC_API_KEY /
			// CLAUDE_CODE_OAUTH_TOKEN / TEMPORAL_ADDRESS already exported in
			// this shell reaches the worker — plus the two overrides that
			// point it at this app's MCP endpoint and a task queue distinct
			// from the generic one other demos default to.
			cmd.Env = append(os.Environ(),
				"GOOEY_MCP_URL="+mcpURL,
				"TEMPORAL_TASK_QUEUE="+*workerTaskQueue,
			)
			app.AddCompanion(gooey.CompanionCmd("temporal-worker", cmd, gooey.CompanionOutput(logFile)))

			helpText += "\n\nworker companion: running on task queue " + *workerTaskQueue + "; log at " + logPath + "\n" +
				"trigger it from examples/temporal-worker: TEMPORAL_TASK_QUEUE=" + *workerTaskQueue +
				" python trigger.py GenerateUI \"some topic\""
		}
		help.Set(helpText)
	} else {
		help.Set("started with -mcp \"\": no server is listening")
	}

	if err := app.Run(context.Background()); err != nil {
		gooey.Exit(err)
	}
}

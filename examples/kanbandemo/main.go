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
// The bottom panel is a components.Tabs — this demo's switcher was the
// hand-rolled pressure that produced the framework component, and now
// uses it: a "mcp" tab with the endpoint + tool-usage text this file
// always had, and a "log" tab showing every raw MCP request/response
// this server has handled, live. ActiveTab is the bound int selection
// (0=mcp, 1=log); ctrl+t still toggles it from code, and the strip
// itself takes clicks, the wheel, left/right while focused, and
// ctrl+pgup/pgdn from anywhere in its subtree. Under the hood the
// switch is still the bindable-Visibility machinery — Tabs binds each
// page's Visibility to "selected == me" — so nothing structural ever
// rebuilds. The log tab's ItemsView is Go-composed (like cmd/logview)
// and registered as the "LogPanel" markup element because its Scroll
// (tail-anchored) field has no markup attribute yet.
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
	"encoding/json"
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
	gooeygrpc "github.com/WonderForgeLabs/gooey/grpc"
	"github.com/WonderForgeLabs/gooey/input"
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
// reusable framework type. Lines is computed once at capture time
// (appendLogEntry): the pretty-printed JSON, one string per line, or —
// defensively, since this is JSON-RPC and should always be JSON — a
// single flat truncated line when Text does not parse.
type logEntry struct {
	Time   time.Time
	Dir    string // "in" (request) or "out" (response)
	Text   string // the full raw JSON-RPC body, never truncated
	Lines  []string
	IsJSON bool
}

// maxLogRows bounds the captured log by displayed ROWS, not messages —
// pretty-printing turns one message into a header row plus one row per
// JSON line, so a handful of large messages can otherwise grow the log
// without bound. Oldest messages drop first, whole messages at a time.
const maxLogRows = 2000

// logDisplayTruncate caps how many bytes of a non-JSON entry's raw text
// show in its fallback row.
const logDisplayTruncate = 500

// maxLineRunes defensively caps a single pretty-printed JSON line —
// normally short, but a huge inline string value (base64, say) would
// otherwise become one very long line.
const maxLineRunes = 2000

// logHScrollStep is how many runes ScrollLogLeft/ScrollLogRight move the
// horizontal window per press.
const logHScrollStep = 8

var (
	logInStyle  = render.Style{Fg: render.RGB(120, 200, 140)}
	logOutStyle = render.Style{Fg: render.RGB(255, 170, 60)}
	panelStyle  = render.Style{Fg: render.RGB(120, 90, 220)}
	accentStyle = render.Style{Fg: render.RGB(255, 170, 60), Bold: true}
	dimStyle    = render.Style{Fg: render.RGB(140, 140, 150)}
)

// truncateLine caps a single line at maxLineRunes, marking that it was
// cut — the horizontal scroll can still reach anything under that cap;
// this only guards against a pathological single field.
func truncateLine(s string) string {
	r := []rune(s)
	if len(r) <= maxLineRunes {
		return s
	}
	return string(r[:maxLineRunes]) + fmt.Sprintf("...(%d more runes)", len(r)-maxLineRunes)
}

// prettyJSONLines pretty-prints raw as indented JSON and splits it into
// lines, or reports ok=false when raw is not valid JSON. json.Indent
// (rather than an unmarshal/remarshal round trip) is deliberate: it
// re-indents the ORIGINAL bytes, so key order and number precision
// (a JSON-RPC id can exceed float64's exact integer range) survive
// untouched.
func prettyJSONLines(raw string) (lines []string, ok bool) {
	if !json.Valid([]byte(raw)) {
		return nil, false
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, []byte(raw), "", "  "); err != nil {
		return nil, false
	}
	lines = strings.Split(buf.String(), "\n")
	for i, l := range lines {
		lines[i] = truncateLine(l)
	}
	return lines, true
}

// colorRun is one styled fragment of a rendered log line — the unit a
// colorized row is built from, since components.Text only paints one
// uniform Style for its whole content (components/text.go).
type colorRun struct {
	Text  string
	Style render.Style
}

// logRow is one DISPLAYED row: a message's header, or one line of its
// pretty-printed body. buildLogRows expands logEntries (one per
// message) into these (one or more per message) — the ItemsView's rows
// are these, not logEntries directly.
type logRow struct {
	Runs []colorRun
}

// maxLineRuns bounds how many colored fragments one row can carry. The
// tokenizer below never produces more than four (indent+key, colon,
// value, trailing comma) but the fifth slot is spare rather than a hard
// ceiling on what the scanner could see.
const maxLineRuns = 5

// scanQuoted reads a JSON string literal starting at s[0] == '"',
// honoring backslash escapes, and returns the quoted text (with its
// quotes) and whatever follows it.
func scanQuoted(s string) (tok, rest string, ok bool) {
	if len(s) == 0 || s[0] != '"' {
		return "", s, false
	}
	for i := 1; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++ // the escaped character is not a delimiter either
		case '"':
			return s[:i+1], s[i+1:], true
		}
	}
	return s, "", true // unterminated: defensively take the rest
}

// scanKey recognizes `"key": ` at the start of s. json.Indent always
// puts the colon immediately after the closing quote and exactly one
// space after the colon, so that shape is all this needs to check.
func scanKey(s string) (key, sep, after string, ok bool) {
	tok, rest, qok := scanQuoted(s)
	if !qok || !strings.HasPrefix(rest, ":") {
		return "", "", "", false
	}
	sep, rest = ":", rest[1:]
	if strings.HasPrefix(rest, " ") {
		sep, rest = sep+" ", rest[1:]
	}
	return tok, sep, rest, true
}

// scanValue splits a value — a string, a number/bool/null literal, or
// bare structural punctuation — from its optional trailing comma.
func scanValue(s string) (val, trail string) {
	if s == "" {
		return "", ""
	}
	if s[0] == '"' {
		tok, rest, _ := scanQuoted(s)
		return tok, rest
	}
	if s[0] == '{' || s[0] == '[' || s[0] == '}' || s[0] == ']' {
		return s[:1], s[1:]
	}
	if i := strings.IndexByte(s, ','); i >= 0 {
		return s[:i], s[i:]
	}
	return s, ""
}

// tokenizeJSONLine turns one line of json.Indent output into styled
// runs: object/array punctuation dim, quoted keys accented, string
// values in logInStyle, numeric/bool/null literals in panelStyle. It is
// a line-level scanner, not a JSON lexer — json.Indent's regular
// per-line shape is all a log viewer needs (see the package doc for
// what routing raw text through the markdown renderer would have
// bought instead: nothing, because it does not tokenize fences either).
func tokenizeJSONLine(line string) []colorRun {
	var runs []colorRun
	add := func(s string, st render.Style) {
		if s != "" {
			runs = append(runs, colorRun{Text: s, Style: st})
		}
	}

	i := 0
	for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
		i++
	}
	prefix, rest := line[:i], line[i:]

	if key, sep, after, ok := scanKey(rest); ok {
		add(prefix+key, accentStyle)
		add(sep, dimStyle)
		rest = after
	} else {
		add(prefix, dimStyle)
	}

	val, trail := scanValue(rest)
	switch {
	case val == "":
	case val[0] == '"':
		add(val, logInStyle)
	case val[0] == '{' || val[0] == '[' || val[0] == '}' || val[0] == ']':
		add(val, dimStyle)
	default:
		add(val, panelStyle)
	}
	add(trail, dimStyle)

	if len(runs) > maxLineRuns {
		// Defensive: the shapes above never actually produce this many,
		// but keep the row's key-set fixed rather than losing a token.
		merged := runs[maxLineRuns-1]
		for _, r := range runs[maxLineRuns:] {
			merged.Text += r.Text
		}
		runs = append(runs[:maxLineRuns-1], merged)
	}
	return runs
}

// buildLogRows flattens captured messages into displayed rows: one
// header row (the existing timestamp/direction line) followed by one
// row per pretty-printed JSON line, colorized, in chronological order.
// A non-JSON entry (defensive; should not happen for JSON-RPC) falls
// back to its single flat line in the plain direction style, unchanged
// from the old one-row-per-message display.
func buildLogRows(entries []logEntry) []logRow {
	rows := make([]logRow, 0, len(entries)*2)
	for _, e := range entries {
		arrow, style := "← out", logOutStyle
		if e.Dir == "in" {
			arrow, style = "→ in ", logInStyle
		}
		header := fmt.Sprintf("%s %s", e.Time.Format("15:04:05.000"), arrow)
		rows = append(rows, logRow{Runs: []colorRun{{Text: header, Style: style}}})
		for _, line := range e.Lines {
			if e.IsJSON {
				rows = append(rows, logRow{Runs: tokenizeJSONLine(line)})
			} else {
				rows = append(rows, logRow{Runs: []colorRun{{Text: line, Style: style}}})
			}
		}
	}
	return rows
}

// sliceRunsFrom drops the first offset RUNES from a run sequence,
// trimming whichever run offset lands inside and dropping the ones
// before it whole — the multi-run analogue of TextBox's scrollFor
// (components/textbox.go), except driven by an explicit offset rather
// than a caret, since a log view has none.
func sliceRunsFrom(runs []colorRun, offset int) []colorRun {
	if offset <= 0 {
		return runs
	}
	out := make([]colorRun, 0, len(runs))
	skip := offset
	for _, r := range runs {
		rr := []rune(r.Text)
		if skip >= len(rr) {
			skip -= len(rr)
			continue
		}
		out = append(out, colorRun{Text: string(rr[skip:]), Style: r.Style})
		skip = 0
	}
	return out
}

// runeLen is the total rune width of a row's content, unscrolled — used
// to clamp how far ScrollLogRight can go.
func runeLen(runs []colorRun) int {
	n := 0
	for _, r := range runs {
		n += len([]rune(r.Text))
	}
	return n
}

// projectLogRow is the ItemsView projection: a row's runs, scrolled by
// hOff and padded out to maxLineRuns fixed slots (RunNText/RunNStyle) —
// a fixed key set is what lets ItemsView reuse a realized row in place
// (components/itemsview.go, itemRow.accepts) instead of rebuilding its
// template every time a row's content changes shape.
func projectLogRow(r logRow, hOff int) map[string]any {
	visible := sliceRunsFrom(r.Runs, hOff)
	vals := make(map[string]any, maxLineRuns*2)
	for i := range maxLineRuns {
		text, style := "", render.Style{}
		if i < len(visible) {
			text, style = visible[i].Text, visible[i].Style
		}
		vals[fmt.Sprintf("Run%dText", i)] = text
		vals[fmt.Sprintf("Run%dStyle", i)] = style
	}
	return vals
}

// logRowTemplate is the Go spelling of an <ItemsView.ItemTemplate> for
// one displayed row. Because components.Text carries only one uniform
// Style for its whole content, a colorized row with several differently
// styled fragments cannot be one Text — this returns an HStack of small
// Text runs instead, one per fixed slot projectLogRow filled.
func logRowTemplate(values map[string]any) (gooey.Component, error) {
	children := make([]gooey.Component, 0, maxLineRuns)
	for i := range maxLineRuns {
		textKey, styleKey := fmt.Sprintf("Run%dText", i), fmt.Sprintf("Run%dStyle", i)
		text, ok := values[textKey].(*prop.Property[string])
		if !ok {
			return nil, fmt.Errorf("%s is %T", textKey, values[textKey])
		}
		style, ok := values[styleKey].(*prop.Property[render.Style])
		if !ok {
			return nil, fmt.Errorf("%s is %T", styleKey, values[styleKey])
		}
		children = append(children, &components.Text{Content: text, Style: style})
	}
	return &components.HStack{Gap: 0, Children: children}, nil
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
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

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
	// Port 0 by default: the kernel picks a free one, so several instances
	// (and several agents) coexist without colliding on a well-known port.
	// The address is only ever read back from the listener — never from
	// this flag — so the resolved port reaches the help panel and the
	// worker companion's GOOEY_MCP_URL alike. Pass -mcp 127.0.0.1:7778 for
	// a fixed port when a client is registered against one.
	addr := flag.String("mcp", "127.0.0.1:0", "loopback address for the MCP server; port 0 picks a free port; empty disables it")
	withWorker := flag.Bool("with-worker", true, "launch the Python Temporal dynamic-UI worker (examples/temporal-worker) as a companion, sharing this app's process lifetime; pass -with-worker=false to disable")
	workerPython := flag.String("worker-python", "python3", "python interpreter for the worker companion; point it at a venv's bin/python if system python lacks examples/temporal-worker/requirements.txt")
	workerTaskQueue := flag.String("worker-task-queue", "kanbandemo-dynamic-ui", "Temporal task queue the worker companion polls")
	// The gRPC control plane, alongside MCP rather than instead of it:
	// the two surfaces share one control.Service and answer different
	// clients. An Attach client (examples/wysiwyg) needs this one — its
	// editing loop is a subscribed stream, which MCP has no shape for.
	//
	// Random port by default for the same reason -mcp takes one (#188):
	// a fixed default is how two demos on one machine collide.
	grpcAddr := flag.String("grpc", "127.0.0.1:0", "loopback address for the gRPC control plane; port 0 picks a free port; empty disables it")
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

	// --- tab state: which of the bottom panel's two tabs is showing.
	// The int handle bound to <Tabs Selected="{{.ActiveTab}}"> — the
	// component owns the strip, the gestures, and each page's
	// Visibility; this viewmodel owns nothing but the selection itself.
	activeTab := prop.NewSource(0) // 0 = mcp, 1 = log
	toggleTab := gooey.Command(func() { activeTab.Set(1 - activeTab.Get()) })

	// --- log tab: every MCP request/response, captured by
	// mcpTrafficLogger (below) through appendLogEntry, flattened into
	// displayed rows by logRows/logItems below.
	logEntries := prop.NewSource([]logEntry{})
	logScroll := prop.NewSource(0)  // vertical: rows back from the tail
	logHScroll := prop.NewSource(0) // horizontal: runes scrolled into a row

	// logHScroll resets on the way IN to the tab — least surprising: a
	// message inspected mid-scroll keeps its horizontal position while
	// you stay on the tab (new traffic arriving should not yank it back),
	// but returning to the tab later starts from the left margin again
	// rather than silently hiding whatever is now at that stale offset.
	// It hangs off the Tabs' Changed action, so every entry path — header
	// click, ctrl+pgup/pgdn, ctrl+t's Set, an MCP set_value — resets it.
	tabChanged := gooey.Command(func() {
		if activeTab.Get() == 1 && logHScroll.Get() != 0 {
			logHScroll.Set(0)
		}
	})

	// logRows re-flattens on every change to logEntries (not on every
	// horizontal scroll — see logItems below): pretty-printing and
	// tokenizing is the expensive part, and it does not depend on hOff.
	logRows := prop.NewComputed(func() []logRow { return buildLogRows(logEntries.Get()) })

	// logItems is the ItemsOf shape (components/itemsview.go's doc
	// comment on Items vs ItemsOf) rather than plain Items: the
	// projection needs to read logHScroll too, and Items only reads the
	// slice it was given.
	logItems := prop.NewComputed(func() components.ItemSource {
		hOff := logHScroll.Get()
		rows := logRows.Get()
		return components.ItemsOf(rows, func(r logRow) map[string]any {
			return projectLogRow(r, hOff)
		})
	})

	scrollLogLeft := gooey.Command(func() {
		cur := logHScroll.Get()
		next := max(0, cur-logHScrollStep)
		if next != cur {
			logHScroll.Set(next)
		}
	})
	scrollLogRight := gooey.Command(func() {
		cur := logHScroll.Get()
		// Clamped to content: past every row's rune length there is
		// nothing left to reveal, so further presses would only be
		// scrolling into blank space.
		limit := 0
		for _, r := range logRows.Get() {
			if n := runeLen(r.Runs) - 1; n > limit {
				limit = n
			}
		}
		next := max(min(cur+logHScrollStep, limit), 0)
		if next != cur {
			logHScroll.Set(next)
		}
	})

	help := prop.NewSource("")

	panelStyle := render.Style{Fg: render.RGB(120, 90, 220)}
	accentStyle := render.Style{Fg: render.RGB(255, 170, 60), Bold: true}
	dimStyle := render.Style{Fg: render.RGB(140, 140, 150)}

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

			"ActiveTab":  activeTab,
			"ToggleTab":  toggleTab,
			"TabChanged": tabChanged,

			// LogPanel is Go-composed (below), so nothing in
			// kanbandemo.gooey binds these with {{...}} — they are here
			// so an MCP client can read/drive the log's horizontal
			// scroll the same way it drives everything else: set_value
			// LogHScroll, or invoke_command ScrollLogLeft/ScrollLogRight.
			"LogHScroll":     logHScroll,
			"ScrollLogLeft":  scrollLogLeft,
			"ScrollLogRight": scrollLogRight,
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
				border := &components.Border{
					Title: components.Str("log"),
					Style: components.Sty(panelStyle),
					Child: &components.ItemsView{
						Items:    logItems,
						Scroll:   logScroll,
						Template: logRowTemplate,
					},
				}
				// Horizontal scroll is handled here, not through a
				// markup <KeyBinding> (LogPanel has no markup subtree to
				// put one in) and not globally: attaching it to the
				// border scopes it to this component's own subtree —
				// per input.go's KeyBinding doc, the dispatcher only
				// reaches an attachment while the focused component's
				// ancestor chain passes through the component it hangs
				// off. The ItemsView inside is the only focus stop in
				// that chain, so these only fire while the log list
				// itself has focus. left/right are otherwise unclaimed
				// on this page (the card lists use j/k/up/down; the
				// footer text says so), and ItemsView's own scroll-mode
				// HandleKey (components/itemsview.go) only owns
				// j/k/up/down/pgup/pgdn/home/end, so left/right reach
				// this binding rather than falling through to the
				// framework's spatial arrow-key focus navigation.
				border.Attach(&gooey.KeyBinding{Gesture: input.Named(input.KeyLeft), Command: scrollLogLeft})
				border.Attach(&gooey.KeyBinding{Gesture: input.Named(input.KeyRight), Command: scrollLogRight})
				return border, nil
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
	// source per the framework's Dispatcher. Pretty-printing happens here,
	// once, at capture time, rather than being redone on every repaint.
	appendLogEntry := func(direction, text string) {
		app.Post(func() {
			lines, isJSON := prettyJSONLines(text)
			if !isJSON {
				flat := strings.ReplaceAll(text, "\n", " ")
				if n := len(flat); n > logDisplayTruncate {
					flat = fmt.Sprintf("%s...(%d more bytes)", flat[:logDisplayTruncate], n-logDisplayTruncate)
				}
				lines = []string{flat}
			}
			entry := logEntry{Time: time.Now(), Dir: direction, Text: text, Lines: lines, IsJSON: isJSON}
			cur := logEntries.Get()
			next := append(append([]logEntry{}, cur...), entry)
			// Row cap, not message cap (see maxLogRows): drop the oldest
			// whole messages until the flattened row count fits.
			rows := 0
			for _, e := range next {
				rows += 1 + len(e.Lines)
			}
			for rows > maxLogRows && len(next) > 1 {
				rows -= 1 + len(next[0].Lines)
				next = next[1:]
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
			"                               ActiveTab (int, 0=mcp 1=log)\n" +
			"tools/call invoke_command   — {\"name\": \"AddTask\"} (after set_value NewTitle), or TodoMoveRight/DoingMoveLeft/\n" +
			"                               DoingMoveRight/DoneMoveLeft/TodoRemove/DoingRemove/DoneRemove/ToggleTab/\n" +
			"                               ScrollLogLeft/ScrollLogRight (moves LogHScroll, the log panel's horizontal window)\n" +
			"tools/call set_value        — {\"name\": \"NewTitle\", \"value\": \"typed by an agent\"} or {\"name\": \"ActiveTab\", \"value\": 1}\n" +
			"tools/call focus/send_keys  — {\"name\": \"NewTitle\"} then {\"text\": \"buy milk\"}; or focus a list, then keys: [\"down\"]\n" +
			"tools/call tree_snapshot    — Name= identities: NewTitle, AddBtn, TodoList, DoingList, DoneList, PanelTabs,\n" +
			"                               LogPanel, and each column's buttons\n\n" +
			"tab: ctrl+t (or click the mcp / log headers; ctrl+pgup/pgdn while this panel has focus) switches between this help\n" +
			"text and a live log of every raw MCP request/response this server has handled — including the call reading this."

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

			// Prefer the worker's own venv when the flag is left at its
			// default: examples/temporal-worker/.venv is where its deps
			// (temporalio, claude-agent-sdk) are installed, and system
			// python3 almost never has them — a worker that can't import
			// them exits, and a dead companion tears the app down. An
			// explicit -worker-python always wins; a missing venv falls
			// straight back to python3, unchanged.
			python := *workerPython
			if python == "python3" {
				if venv := filepath.Join(workerDir, ".venv", "bin", "python"); fileExists(venv) {
					python = venv
				}
			}
			cmd := exec.Command(python, "worker.py")
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

	if *grpcAddr != "" {
		// Options.Context is not optional: without it the service has no
		// binding context, so ValidateMarkup and the declared-schema
		// path lose the very thing a remote editor validates against.
		// Name/Version surface in every session's Welcome, which is how
		// an attached client identifies what it is driving.
		gsrv, err := gooeygrpc.Serve(app, gooeygrpc.Options{
			Addr:    *grpcAddr,
			Context: ctx,
			Name:    "gooey-kanbandemo",
			Version: "1",
		})
		if err != nil {
			gooey.Exit(err)
		}
		// Close joins rather than merely signalling — the framework rule
		// that a stop must wait, not just ask.
		defer gsrv.Close()
		help.Set(help.Get() + "\n\ngRPC control plane: " + gsrv.Addr() + "  (gooey.control.v1)\n" +
			"attach the markup editor:  cd examples/wysiwyg && go run . -attach " + gsrv.Addr() + " -island Help")
	}

	if err := app.Run(context.Background()); err != nil {
		gooey.Exit(err)
	}
}

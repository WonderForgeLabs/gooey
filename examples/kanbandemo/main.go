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
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

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

	help := prop.NewSource("")

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
		},
		Styles: map[string]render.Style{
			"panel":  {Fg: render.RGB(120, 90, 220)},
			"accent": {Fg: render.RGB(255, 170, 60), Bold: true},
			"dim":    {Fg: render.RGB(140, 140, 150)},
		},
	}

	dir := "."
	if _, err := os.Stat(filepath.Join(dir, "kanbandemo.gooey")); err != nil {
		exe, _ := os.Executable()
		dir = filepath.Dir(exe)
	}

	app = gooey.NewApp(markup.Page(os.DirFS(dir), "kanbandemo.gooey", ctx))

	if *withWorker && *addr == "" {
		gooey.Exit(fmt.Errorf("kanbandemo: -with-worker needs the MCP endpoint it pushes markup into; do not pass -mcp \"\""))
	}

	if *addr != "" {
		srv, err := mcp.Serve(app, mcp.Options{
			Addr:    *addr,
			Context: ctx,
			Name:    "gooey-kanbandemo",
		})
		if err != nil {
			gooey.Exit(err)
		}
		defer srv.Close()
		helpText := "MCP endpoint: " + srv.URL() + "\n\n" +
			"tools/call list_values      — TodoItems/DoingItems/DoneItems (lists), TodoSel/DoingSel/DoneSel (int), NewTitle (string)\n" +
			"tools/call invoke_command   — {\"name\": \"AddTask\"} (after set_value NewTitle), or TodoMoveRight/DoingMoveLeft/\n" +
			"                               DoingMoveRight/DoneMoveLeft/TodoRemove/DoingRemove/DoneRemove\n" +
			"tools/call set_value        — {\"name\": \"NewTitle\", \"value\": \"typed by an agent\"} or {\"name\": \"TodoSel\", \"value\": 1}\n" +
			"tools/call focus/send_keys  — {\"name\": \"NewTitle\"} then {\"text\": \"buy milk\"}; or focus a list, then keys: [\"down\"]\n" +
			"tools/call tree_snapshot    — Name= identities: NewTitle, AddBtn, TodoList, DoingList, DoneList, and each column's buttons"

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
				"GOOEY_MCP_URL="+srv.URL(),
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

// server: a small gooey app that is also an MCP server.
//
// Run it and an agent can attach to http://127.0.0.1:7777/mcp and read
// the tree, screenshot the terminal, click the buttons by name, set the
// viewmodel, type into the text box, and replace the whole page with new
// markup — while the app keeps running and a Timer keeps ticking.
//
//	cd mcp && go run ./cmd/server -mcp 127.0.0.1:7777
//
// It lives inside mcp/ because mcp/ is a nested module: the MCP SDK's
// dependency graph is quarantined there, and a binary that imports it
// has to be built from there too.
//
// The point of the demo is the pairing: the UI here is ordinary markup
// with named elements and a viewmodel of typed property handles, and the
// MCP surface falls out of that with no extra declaration. Names come
// from Name= attributes, the bindable state IS the Context's Values map,
// and the commands the buttons already use are the commands an agent
// invokes. Nothing in this file is written for the agent's benefit except
// the single mcp.Serve call.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/mcp"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

var messages = []string{"hello, gooey", "the tree is the API", "state is properties", "drive me over MCP"}

func main() {
	addr := flag.String("mcp", "127.0.0.1:7777", "bind address for the MCP server; empty disables it. UNAUTHENTICATED — a non-loopback address exposes it")
	flag.Parse()

	// --- viewmodel: the same typed handles the markup binds to, and the
	// same ones list_values/set_value address by name. There is no second
	// "automation model" anywhere.
	count := prop.NewSource(0)
	msgIdx := prop.NewSource(0)
	note := prop.NewSource("")
	auto := prop.NewSource(false)

	readout := prop.NewComputed(func() string {
		n := note.Get()
		if n == "" {
			n = "(none)"
		}
		return fmt.Sprintf("count=%d   message=%q   note=%s",
			count.Get(), messages[msgIdx.Get()%len(messages)], n)
	})

	help := prop.NewSource("")

	var app *gooey.App

	ctx := &markup.Context{
		Values: map[string]any{
			"Readout":   readout,
			"Note":      note,
			"Auto":      auto,
			"Help":      help,
			"Increment": gooey.Command(func() { count.Set(count.Get() + 1) }),
			"Reset":     gooey.Command(func() { count.Set(0) }),
			"Cycle":     gooey.Command(func() { msgIdx.Set(msgIdx.Get() + 1) }),
			"Quit":      gooey.Command(func() { app.Quit() }),
		},
		Styles: map[string]render.Style{
			"panel":  {Fg: render.RGB(120, 90, 220)},
			"accent": {Fg: render.RGB(255, 170, 60), Bold: true},
			"dim":    {Fg: render.RGB(140, 140, 150)},
		},
	}

	dir := "cmd/server"
	if _, err := os.Stat(filepath.Join(dir, "server.gooey")); err != nil {
		exe, _ := os.Executable()
		dir = filepath.Dir(exe)
	}

	app = gooey.NewApp(markup.Page(os.DirFS(dir), "server.gooey", ctx))

	if *addr != "" {
		// Serving is one call and it is opt-in, which is the whole
		// security posture of v1: an MCP client can do anything the
		// keyboard can, so nothing listens unless the app said so.
		// There is no authentication, and the bind address is used as
		// given — a non-loopback one exposes this handle.
		srv, err := mcp.Serve(app, mcp.Options{
			Addr:    *addr,
			Context: ctx,
			Name:    "gooey-mcp-server",
		})
		if err != nil {
			gooey.Exit(err)
		}
		defer srv.Close()
		help.Set("MCP endpoint: " + srv.URL() + "\n\n" +
			"tools/call tree_snapshot   — the live component tree, with Name= identities\n" +
			"tools/call screen_text     — this screen as text\n" +
			"tools/call list_values     — Readout, Note, Auto, Increment, Reset, Cycle, Quit\n" +
			"tools/call invoke_command  — {\"name\": \"Increment\"}\n" +
			"tools/call set_value       — {\"name\": \"Note\", \"value\": \"typed by an agent\"}\n" +
			"tools/call focus/send_keys — {\"name\": \"Note\"} then {\"text\": \"hi\"}\n" +
			"tools/call swap_markup     — replace this page; the viewmodel survives")
	} else {
		help.Set("started with -mcp \"\": no server is listening")
	}

	if err := app.Run(context.Background()); err != nil {
		gooey.Exit(err)
	}
}

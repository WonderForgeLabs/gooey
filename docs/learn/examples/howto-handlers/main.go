// How-to: handler packs — fetch a URL and read files from markup.
//
// None of the handler buttons on this page has a delegate. Their
// behavior is declared in app.gooey with handler expressions —
// {{net:Get .Url | into .Body}}, {{fs:Read .Path | into .Content}} —
// and what THIS file contributes is the capability grants: it
// registers the net provider (an http.Client) and the fs provider
// (an fs.FS rooted at this directory). Delete either registration and
// the page stops loading, naming the URI it wanted.
//
//	cd docs/learn/examples/howto-handlers && go run .
//
// The exec pack (sys:Run) is deliberately absent: it lives in its own
// Go module (it depends on gojq), and the learn examples stay inside
// the root module. The walkthrough shows it as snippets and points at
// the full sample.
//
// Walkthrough: docs/learn/howto/howto-handlers.md
package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/WonderForgeLabs/gooey"
	fshandlers "github.com/WonderForgeLabs/gooey/handlers/fs"
	nethandlers "github.com/WonderForgeLabs/gooey/handlers/net"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

// paths is what the "cycle path" button steps through. The handler
// argument .Path is a HANDLE read at invoke time, so cycling changes
// what the next fs:Read press opens — nothing was captured at load.
var paths = []string{"app.gooey", "main.go"}

func main() {
	// The thing net:Get fetches: this example's own loopback server, so
	// it needs no internet and no fixture host.
	srvURL, err := serveLocal()
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot start the local http server:", err)
		os.Exit(1)
	}

	// --- capability grants ---
	// Registration is what markup's xmlns declarations resolve against.
	// The fs grant names a ROOT: os.DirFS(".") is this directory, so
	// every path the page can name resolves inside it and nowhere else.
	markup.RegisterHandlers(nethandlers.URI, nethandlers.New())
	markup.RegisterHandlers(fshandlers.URI, fshandlers.New(os.DirFS(".")))

	// --- viewmodel: only data and two ordinary commands ---
	var app *gooey.App

	url := prop.NewSource(srvURL)
	body := prop.NewSource("press [ net:Get ] — the response body lands here")
	idx := prop.NewSource(0)
	path := prop.NewComputed(func() string { return paths[idx.Get()%len(paths)] })
	pathLine := prop.NewComputed(func() string { return ".Path = " + path.Get() })
	content := prop.NewSource("press [ fs:Read ] — the file's contents land here")
	entries := prop.NewSource("press [ fs:List ] or [ fs:Glob ] — JSON lands here")

	ctx := &markup.Context{
		Values: map[string]any{
			"Url": url, "Body": body,
			"Path": path, "PathLine": pathLine, "Content": content,
			"Entries": entries,
			"Cycle":   gooey.Command(func() { idx.Set(idx.Get() + 1) }),
			"Quit":    gooey.Command(func() { app.Quit() }),
		},
		Styles: map[string]render.Style{
			"panel":  {Fg: render.RGB(120, 90, 220)},
			"accent": {Fg: render.RGB(255, 170, 60), Bold: true},
			"dim":    {Fg: render.RGB(140, 140, 150)},
		},
	}

	app = gooey.NewApp(markup.Page(os.DirFS("."), "app.gooey", ctx))
	// Handler results are Set on the UI goroutine — the Dispatcher is
	// how they get there, and a document using handler namespaces fails
	// to load without one. The App drains its own, so hand that one over.
	ctx.Dispatcher = app.Dispatcher()

	if err := app.Run(context.Background()); err != nil {
		gooey.Exit(err)
	}
}

// serveLocal starts a one-route HTTP server on a loopback port and
// returns its URL, so net:Get has something real to fetch offline.
func serveLocal() (string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "200 OK from this example's own server\n"+
			"path:   %s\n"+
			"time:   %s\n"+
			"fetched by markup, not by Go code in this binary.",
			r.URL.Path, time.Now().Format("15:04:05"))
	})
	go http.Serve(ln, mux)
	return "http://" + ln.Addr().String() + "/hello", nil
}

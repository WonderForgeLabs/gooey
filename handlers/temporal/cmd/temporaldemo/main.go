// temporaldemo is the handler-namespace demo: two buttons whose
// behavior is declared entirely in markup.
//
//	<Button Content="net:Get"  Click="{{net:Get .Url | into .Body}}"/>
//	<Button Content="Slugify"  Click="{{temporal:Activity `Slugify` .Input | into .Output}}"/>
//
// No delegate in this file implements either one. What this file does
// is *grant the capabilities*: it constructs the two providers — one
// with an http.Client, one with a Temporal client and a task queue —
// and registers them under the URIs the markup declares with xmlns. Take
// a registration away and the same markup stops loading, naming the URI
// it wanted. That is the sandbox boundary: markup can reach exactly the
// capabilities its host chose to hand it.
//
// The Cycle and Quit commands are ordinary viewmodel delegates, the same
// no-code-behind arrangement as cmd/statedemo — the contrast is the
// point: those are app behavior, the two handler buttons are framework
// behavior parameterized by markup.
//
// Running it:
//
//	temporal server start-dev --headless   # shell 1
//	go run ./cmd/temporalworker            # shell 2
//	go run ./cmd/temporaldemo              # shell 3
package main

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/WonderForgeLabs/gooey"
	nethandlers "github.com/WonderForgeLabs/gooey/handlers/net"
	temporalhandlers "github.com/WonderForgeLabs/gooey/handlers/temporal"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
	"github.com/WonderForgeLabs/gooey/term"
	"go.temporal.io/sdk/client"
)

var phrases = []string{
	"Hello There, Gooey!",
	"XAML for the terminal",
	"Properties are the app",
	"Workers Anywhere Decide How",
}

func main() {
	hostPort := envOr("TEMPORAL_ADDRESS", client.DefaultHostPort)
	taskQueue := envOr("GOOEY_TASK_QUEUE", "gooey-demo")

	// --- the thing net:Get fetches: a local server, so the demo needs
	// no internet and no fixture host ---
	srvURL, err := serveLocal()
	if err != nil {
		fatal("cannot start the local http server: %v", err)
	}

	// --- capability grants ---
	tc, err := client.Dial(client.Options{HostPort: hostPort})
	if err != nil {
		fatal("cannot reach the Temporal server at %s: %v\n"+
			"start one with: temporal server start-dev --headless", hostPort, err)
	}
	defer tc.Close()

	markup.RegisterHandlers(nethandlers.URI, nethandlers.New())
	markup.RegisterHandlers(temporalhandlers.URI,
		temporalhandlers.New(tc, taskQueue, temporalhandlers.WithTimeout(30*time.Second)))

	// --- viewmodel ---
	url := prop.NewSource(srvURL)
	body := prop.NewSource("press [ net:Get ] — the response body lands here")
	idx := prop.NewSource(0)
	output := prop.NewSource("press [ temporal:Activity Slugify ] — the worker's result lands here")

	inputText := prop.NewComputed(func() string { return phrases[idx.Get()%len(phrases)] })
	inputLine := prop.NewComputed(func() string { return ".Input = " + inputText.Get() })
	status := prop.NewComputed(func() string {
		return fmt.Sprintf("net → %s     temporal → %s  queue %q", url.Get(), hostPort, taskQueue)
	})

	running := true
	disp := gooey.NewDispatcher()

	ctx := &markup.Context{
		// .Input is a computed the handler reads at invoke time, so
		// cycling changes what the NEXT press sends — the argument is a
		// handle, not a value captured at load.
		Values: map[string]any{
			"Url": url, "Body": body,
			"Input": inputText, "InputLine": inputLine, "Output": output,
			"Status": status,
			"Cycle":  gooey.Command(func() { idx.Set(idx.Get() + 1) }),
			"Quit":   gooey.Command(func() { running = false }),
		},
		Styles: map[string]render.Style{
			"panel":  {Fg: render.RGB(120, 90, 220)},
			"accent": {Fg: render.RGB(255, 170, 60), Bold: true},
			"dim":    {Fg: render.RGB(140, 140, 150)},
		},
		// Handler results are Set here, on the loop goroutine.
		Dispatcher: disp,
	}

	fsys := os.DirFS(sourceDir("temporaldemo.gooey"))
	tree, err := markup.Load(fsys, "temporaldemo.gooey", ctx)
	if err != nil {
		fatal("%v", err)
	}

	screen, err := term.Open()
	if err != nil {
		fatal("no tty: %v", err)
	}
	cols, rows := screen.Size()

	needsFrame := true
	var comp *gooey.Composer
	attach := func(w gooey.Widget) {
		comp = gooey.NewComposer(w, cols, rows)
		comp.OnInvalidate(func() { needsFrame = true })
		needsFrame = true
	}
	attach(tree)

	swaps := make(chan gooey.Widget, 1)
	stopWatch := markup.Watch(fsys, "temporaldemo.gooey", ctx, func(w gooey.Widget) { swaps <- w })
	defer stopWatch()

	if err := screen.Raw(); err != nil {
		fatal("%v", err)
	}
	defer screen.Restore()
	screen.EnableMouse()

	events := make(chan input.Event, 64)
	go term.DecodeEvents(screen, events)

	for running {
		if needsFrame {
			comp.Frame()
			comp.Flush(screen.File())
			needsFrame = false
		}
		select {
		case <-disp.Wake():
			// A completed fetch or activity, marshaled back onto this
			// goroutine. Draining Sets the target properties; the widgets
			// that read them go dirty and repaint next pass. Nothing here
			// knows which widgets those are.
			disp.Drain()
		case w := <-swaps:
			attach(w)
		case ev := <-events:
			comp.Handle(ev)
		}
	}
}

// serveLocal starts the demo's own HTTP server on a loopback port and
// returns its URL, so net:Get has something real to fetch offline.
func serveLocal() (string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "200 OK from the demo's own server\n"+
			"path:   %s\n"+
			"time:   %s\n"+
			"fetched by markup, not by Go code in this binary.",
			r.URL.Path, time.Now().Format("15:04:05"))
	})
	go http.Serve(ln, mux)
	return "http://" + ln.Addr().String() + "/hello", nil
}

// sourceDir finds the markup next to the source in a dev checkout, and
// next to the binary otherwise — the same convention the other demos use.
func sourceDir(name string) string {
	for _, dir := range []string{".", "cmd/temporaldemo", "handlers/temporal/cmd/temporaldemo"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			return dir
		}
	}
	exe, _ := os.Executable()
	return filepath.Dir(exe)
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "temporaldemo: "+format+"\n", args...)
	os.Exit(1)
}

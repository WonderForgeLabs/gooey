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
// no-code-behind arrangement as cmd/state — the contrast is the
// point: those are app behavior, the two handler buttons are framework
// behavior parameterized by markup.
//
// The worker that serves Slugify is a gooey COMPANION by default: it
// starts before the first frame and stops when the app does, so the demo
// needs one shell instead of two. Nothing about the demo depends on
// that. Pass --with-worker=false and run workers/temporalworker yourself —
// anywhere with a route to the server — and the same button reaches it.
//
// Running it:
//
//	temporal server start-dev --headless   # shell 1
//	go run ./cmd/temporaldemo              # shell 2
//
//	# workers elsewhere, the deployment this models:
//	go run ./workers/temporalworker        # shell 2 (or another machine)
//	go run ./cmd/temporaldemo --with-worker=false
package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/WonderForgeLabs/gooey"
	nethandlers "github.com/WonderForgeLabs/gooey/handlers/net"
	temporalhandlers "github.com/WonderForgeLabs/gooey/handlers/temporal"
	"github.com/WonderForgeLabs/gooey/handlers/temporal/internal/slugify"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
	"go.temporal.io/sdk/client"
)

var phrases = []string{
	"Hello There, Gooey!",
	"XAML for the terminal",
	"Properties are the app",
	"Workers Anywhere Decide How",
}

func main() {
	var (
		address    = flag.String("address", envOr("TEMPORAL_ADDRESS", client.DefaultHostPort), "Temporal server host:port")
		taskQueue  = flag.String("queue", envOr("GOOEY_TASK_QUEUE", slugify.DefaultTaskQueue), "task queue Slugify is served on")
		withWorker = flag.Bool("with-worker", true, "run the Slugify worker in-process for this app's lifetime")
	)
	flag.Parse()

	// --- the thing net:Get fetches: a local server, so the demo needs
	// no internet and no fixture host ---
	srvURL, err := serveLocal()
	if err != nil {
		fatal("cannot start the local http server: %v", err)
	}

	// --- capability grants ---
	// NopLogger: the SDK's default logger writes to stderr, which in
	// raw mode prints straight over the UI's bottom rows. The companion
	// worker below shares this client, so it inherits the silence — a
	// worker in the UI's own process has the same claim on stderr as
	// the UI does, which is none.
	tc, err := client.Dial(client.Options{HostPort: *address, Logger: temporalhandlers.NopLogger})
	if err != nil {
		fatal("cannot reach the Temporal server at %s: %v\n"+
			"start one with: temporal server start-dev --headless", *address, err)
	}

	markup.RegisterHandlers(nethandlers.URI, nethandlers.New())
	markup.RegisterHandlers(temporalhandlers.URI,
		temporalhandlers.New(tc, *taskQueue, temporalhandlers.WithTimeout(30*time.Second)))

	// --- viewmodel ---
	var app *gooey.App

	url := prop.NewSource(srvURL)
	body := prop.NewSource("press [ net:Get ] — the response body lands here")
	idx := prop.NewSource(0)
	output := prop.NewSource("press [ temporal:Activity Slugify ] — the worker's result lands here")

	inputText := prop.NewComputed(func() string { return phrases[idx.Get()%len(phrases)] })
	inputLine := prop.NewComputed(func() string { return ".Input = " + inputText.Get() })
	// The worker's whereabouts lead the line: it is the only part that
	// changes between runs, and a status line that outgrows the terminal
	// loses its tail, not its head.
	where := "worker in-process"
	if !*withWorker {
		where = "worker elsewhere"
	}
	status := prop.NewComputed(func() string {
		return fmt.Sprintf("%s · net → %s · temporal → %s queue %q",
			where, url.Get(), *address, *taskQueue)
	})

	ctx := &markup.Context{
		// .Input is a computed the handler reads at invoke time, so
		// cycling changes what the NEXT press sends — the argument is a
		// handle, not a value captured at load.
		Values: map[string]any{
			"Url": url, "Body": body,
			"Input": inputText, "InputLine": inputLine, "Output": output,
			"Status": status,
			"Cycle":  gooey.Command(func() { idx.Set(idx.Get() + 1) }),
			"Quit":   gooey.Command(func() { app.Quit() }),
		},
		Styles: map[string]render.Style{
			"panel":  {Fg: render.RGB(120, 90, 220)},
			"accent": {Fg: render.RGB(255, 170, 60), Bold: true},
			"dim":    {Fg: render.RGB(140, 140, 150)},
		},
	}

	fsys := os.DirFS(sourceDir("temporaldemo.gooey"))
	app = gooey.NewApp(markup.Page(fsys, "temporaldemo.gooey", ctx))
	// The Dispatcher is the App's, so handler results land on the run
	// loop's goroutine — the confinement rule, and the reason a fetch
	// completing mid-frame is not a data race.
	ctx.Dispatcher = app.Dispatcher()

	if *withWorker {
		// The whole worker deployment, as one option. It starts before
		// the first frame and its context is cancelled at teardown; the
		// activity in it cannot tell it is not in another process.
		app.AddCompanion(gooey.CompanionFunc("slugify-worker",
			func(ctx context.Context) error { return slugify.Run(ctx, tc, *taskQueue) }))
	}

	// Not deferred: gooey.Exit calls os.Exit. Run has already stopped the
	// companion by the time it returns, so the client outlives the worker
	// that borrowed it.
	err = app.Run(context.Background())
	tc.Close()
	gooey.Exit(err)
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

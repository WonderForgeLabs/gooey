// temporalops is the Temporal ops dashboard: live visibility data in a
// gooey TUI, with every Temporal call declared in markup.
//
// The execution list, the count, and the describe pane are all
// {{temporal:Activity …}} expressions over the temporal-visibility
// pack's convenience activities — scalar arguments read from properties
// at invoke time, protojson results delivered into properties:
//
//	Click="{{temporal:Activity `visibility.Query` .Query .PageSize .PageToken | into .RowsJSON}}"
//
// This binary contributes the capability grant (a connected client and
// one task queue, registered under the temporal namespace URI), the
// viewmodel that parses what the activities deliver (internal/ops), and
// the deployments:
//
//	temporal server start-dev --headless   # shell 1
//	go run ./cmd/temporalops               # shell 2 — worker runs in-process
//
// or one shell, with the dev server as a child process:
//
//	go run ./cmd/temporalops --with-dev-server
//
// or the real shape, workers where the compute is:
//
//	temporal server start-dev --headless        # shell 1
//	go run ./workers/visibilityworker           # shell 2 (or another machine)
//	go run ./cmd/temporalops --with-worker=false
//
// Keys: type in the query bar (Temporal's visibility query language),
// enter runs it; tab reaches the buttons and the list; up/down selects
// (describing the selection); ctrl+n / ctrl+p page; ctrl+r refreshes;
// ctrl+c quits.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"os/exec"
	"time"

	"github.com/WonderForgeLabs/gooey"
	temporalhandlers "github.com/WonderForgeLabs/gooey/handlers/temporal"
	"github.com/WonderForgeLabs/gooey/handlers/temporal/internal/ops"
	"github.com/WonderForgeLabs/gooey/markup"
	visibility "github.com/WonderForgeLabs/gooey/packs/temporal-visibility"
	"go.temporal.io/sdk/client"
)

func main() {
	var (
		address    = flag.String("address", envOr("TEMPORAL_ADDRESS", client.DefaultHostPort), "Temporal server host:port")
		namespace  = flag.String("namespace", envOr("TEMPORAL_NAMESPACE", visibility.DefaultNamespace), "namespace to dial (and default queries to)")
		taskQueue  = flag.String("queue", envOr("GOOEY_TASK_QUEUE", ops.DefaultTaskQueue), "task queue the visibility activities are served on")
		query      = flag.String("query", ops.DefaultQuery, "the query bar's initial visibility query")
		exitAfter  = flag.Duration("exit-after", 0, "quit on a timer, for scripted captures (0 = never)")
		withWorker = flag.Bool("with-worker", true, "run the visibility pack's worker in-process for this app's lifetime")
		withDev    = flag.Bool("with-dev-server", false, "run `temporal server start-dev --headless` as a child process for this app's lifetime")
		devLog     = flag.String("dev-server-log", "", "send the dev server's output to this file (default: discarded)")
	)
	flag.Parse()

	var companions []gooey.Companion
	opts := []gooey.Option{}
	if *withDev {
		companions = append(companions, devServer(*address, *devLog))
		// A server that has to bind a socket takes about a second to
		// discover it cannot; the default 100ms grace is for goroutines.
		opts = append(opts, gooey.WithCompanionGrace(2*time.Second))
	}

	var app *gooey.App
	vm := ops.NewVM(workerWhere(*withWorker), func() { app.Quit() })
	vm.Query.Set(*query)
	vm.PageToken.Set("")

	ctx := &markup.Context{
		Values: vm.Values(),
		Styles: ops.Theme(),
	}
	vm.Attach(ctx)

	// The connection, the capability grant, and the first fetch all
	// belong AFTER the companions are up and BEFORE the first frame —
	// which is exactly what a Content's Build is. session.connect in
	// cmd/wizardui is the same arrangement.
	content := &connectFirst{
		inner: markup.Page(ops.Files, ops.PageFile, ctx),
		dial: func() (client.Client, error) {
			dctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			return dialRetry(dctx, *address, *namespace)
		},
		granted: func(c client.Client) {
			markup.RegisterHandlers(temporalhandlers.URI,
				temporalhandlers.New(c, *taskQueue, temporalhandlers.WithTimeout(30*time.Second), temporalhandlers.WithIDPrefix("temporalops")))
		},
		address: *address,
	}

	app = gooey.NewApp(content, append(opts, gooey.WithCompanions(companions...))...)
	ctx.Dispatcher = app.Dispatcher()

	if *withWorker {
		app.AddCompanion(gooey.CompanionFunc("visibility-worker",
			func(cctx context.Context) error {
				c, err := dialRetry(cctx, *address, *namespace)
				if err != nil {
					return err
				}
				defer c.Close()
				return ops.RunWorker(cctx, c, *taskQueue, *namespace)
			}))
	}

	// The opening fetch: posted, so it runs on the UI loop after the
	// page is built, through the same markup-built commands enter runs.
	app.Post(vm.RunQuery)
	if *exitAfter > 0 {
		time.AfterFunc(*exitAfter, app.Quit)
	}

	err := app.Run(context.Background())
	if content.client != nil {
		content.client.Close()
	}
	var ce *gooey.CompanionError
	if errors.As(err, &ce) && ce.Name == "temporal-dev" {
		fmt.Fprintf(os.Stderr, "%v\n"+
			"the dev server would not run — most often something is already on %s.\n"+
			"re-run with --dev-server-log FILE to see what it said, or drop --with-dev-server\n"+
			"and start it yourself: temporal server start-dev --headless\n", err, *address)
		os.Exit(1)
	}
	gooey.Exit(err)
}

// connectFirst dials and grants before delegating to the markup page.
// Build runs on the UI goroutine after companions start and before the
// terminal is taken, so a missing server is a sentence on a cooked
// terminal, not a corrupted alt screen.
type connectFirst struct {
	inner   gooey.Content
	dial    func() (client.Client, error)
	granted func(client.Client)
	address string
	client  client.Client
}

func (c *connectFirst) Build() (gooey.Component, error) {
	if c.client == nil {
		cl, err := c.dial()
		if err != nil {
			return nil, fmt.Errorf("cannot reach the Temporal server at %s: %w\n"+
				"start one with: temporal server start-dev --headless (or pass --with-dev-server)", c.address, err)
		}
		c.client = cl
		c.granted(cl)
	}
	return c.inner.Build()
}

func (c *connectFirst) Watch(changed func()) func() { return c.inner.Watch(changed) }

// dialRetry reaches the server, retrying until it answers or ctx is
// done — the wizard.Dial arrangement, plus a namespace.
func dialRetry(ctx context.Context, hostPort, namespace string) (client.Client, error) {
	var last error
	for {
		c, err := client.Dial(client.Options{
			HostPort: hostPort, Namespace: namespace, Logger: temporalhandlers.NopLogger,
		})
		if err == nil {
			return c, nil
		}
		last = err
		select {
		case <-ctx.Done():
			return nil, last
		case <-time.After(250 * time.Millisecond):
		}
	}
}

// devServer is the CompanionCmd deployment: the dev server as a child
// process with this app's lifetime — the same arrangement, and the same
// caveats, as cmd/wizardui's.
func devServer(address, logPath string) gooey.Companion {
	if _, err := exec.LookPath("temporal"); err != nil {
		fatal("--with-dev-server needs the Temporal CLI on PATH: %v\n"+
			"install it from https://docs.temporal.io/cli, or start the server yourself:\n"+
			"  temporal server start-dev --headless", err)
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		fatal("--address %q is not host:port: %v", address, err)
	}
	if host == "" {
		host = "127.0.0.1"
	}
	cmd := exec.Command("temporal", "server", "start-dev", "--headless", "--ip", host, "--port", port)
	var opts []gooey.CmdOption
	if logPath != "" {
		f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			fatal("cannot open the dev server log: %v", err)
		}
		opts = append(opts, gooey.CompanionOutput(f))
	}
	return gooey.CompanionCmd("temporal-dev", cmd, opts...)
}

func workerWhere(inProcess bool) string {
	if inProcess {
		return "worker in-process"
	}
	return "worker elsewhere"
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "temporalops: "+format+"\n", args...)
	os.Exit(1)
}

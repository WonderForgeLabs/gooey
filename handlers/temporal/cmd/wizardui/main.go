// wizardui is a terminal that has no application in it.
//
// It knows how to render gooey markup, how to poll a query, and how to
// send a signal. It does not know what a wizard is, what stages exist,
// what a "tier" is, or what any button does. Every screen it draws
// arrived as the payload of a Temporal query a moment earlier, and every
// press it handles was described by that same payload:
//
//	<Button Content="approve" Click="{{wf:Signal `approve` | into .Notice}}"/>
//
// The one thing this program contributes to behavior is the CAPABILITY
// GRANT — it registers the workflow handler namespace against one client
// and one workflow ID. Served markup can therefore signal that workflow
// and nothing else: it cannot start activities, cannot fetch a URL,
// cannot name a different workflow. Delete the RegisterHandlers call and
// the served markup stops loading, naming the URI it wanted.
//
// The other thing it contributes is a theme. Structure and data come from
// the workflow; what a "panel" or an "accent" looks like on THIS terminal
// is the client's business, and unknown style names degrade to plain
// text rather than failing.
//
// # Shells
//
// The application's worker is a gooey COMPANION by default: it starts
// before the first frame, stops when the app does, and is otherwise the
// same registration workers/wizardworker serves. So the demo is two shells:
//
//	temporal server start-dev --headless   # shell 1
//	go run ./cmd/wizardui                  # shell 2
//
// or one, if the Temporal CLI is installed — the dev server can be a
// companion too, as a CHILD PROCESS rather than a goroutine:
//
//	go run ./cmd/wizardui --with-dev-server
//
// or three, which is what a real deployment looks like and why the
// standalone binaries still exist — workers belong where the compute is:
//
//	temporal server start-dev --headless   # shell 1
//	go run ./workers/wizardworker          # shell 2
//	go run ./cmd/wizardui --with-worker=false
//
// The UI cannot tell the difference between the three. Every screen it
// renders came back through the server either way.
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
	"github.com/WonderForgeLabs/gooey/handlers/temporal/internal/wizard"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
	"go.temporal.io/sdk/client"
)

// uiState is the wire contract, and the entire extent of what this
// client knows about the application on the other end: a markup version,
// a state revision, the source to render, and the values it binds.
//
// Duplicating the struct rather than importing the worker's is the point.
// Nothing but these field names is shared between the two programs.
type uiState struct {
	Version  int               `json:"version"`
	Revision int               `json:"revision"`
	Stage    string            `json:"stage"`
	Markup   string            `json:"gooeyMarkup"`
	Values   map[string]string `json:"values"`
	Done     bool              `json:"done"`
}

// echoKey is the one property the SHELL contributes to every screen, and
// the only name it reserves. Handler receipts land here —
//
//	Click="{{wf:Signal `approve` | into .Echo}}"
//
// — which keeps the client's voice out of the workflow's. Everything else
// in the values map came from the workflow, and a served value gets the
// last word on itself: if the workflow changes a value the client had
// optimistically written, the next poll reconciles it.
const echoKey = "Echo"

// theme is client-side, deliberately. A workflow serving a screen should
// not be picking colors for a terminal it has never seen.
var theme = map[string]render.Style{
	"panel":  {Fg: render.RGB(120, 90, 220)},
	"accent": {Fg: render.RGB(255, 170, 60), Bold: true},
	"dim":    {Fg: render.RGB(140, 140, 150)},
	"ok":     {Fg: render.RGB(90, 210, 130), Bold: true},
	"warn":   {Fg: render.RGB(240, 120, 110), Bold: true},
}

func main() {
	var (
		address    = flag.String("address", envOr("TEMPORAL_ADDRESS", client.DefaultHostPort), "Temporal server host:port")
		taskQueue  = flag.String("queue", envOr("GOOEY_TASK_QUEUE", wizard.DefaultTaskQueue), "task queue the application's worker serves")
		wfType     = flag.String("workflow", envOr("GOOEY_WORKFLOW", "ProvisionWizard"), "workflow type to start or attach to")
		wfID       = flag.String("id", envOr("GOOEY_WORKFLOW_ID", "gooey-wizard"), "workflow ID of the session")
		queryName  = flag.String("query", envOr("GOOEY_UI_QUERY", "ui"), "query that returns the screen")
		pollEvery  = flag.Duration("poll", 400*time.Millisecond, "how often to ask for the current screen")
		exitAfter  = flag.Duration("exit-after", 0, "quit on a timer, for scripted captures (0 = never)")
		frameTrace = flag.String("trace", "", "append one line per screen change to this file")
		startup    = flag.Duration("startup", 20*time.Second, "how long to wait for the application to serve its first screen")

		withWorker = flag.Bool("with-worker", true, "run the application's worker in-process for this app's lifetime")
		withDev    = flag.Bool("with-dev-server", false, "run `temporal server start-dev --headless` as a child process for this app's lifetime")
		devLog     = flag.String("dev-server-log", "", "send the dev server's output to this file (default: discarded)")
	)
	flag.Parse()

	ui := &session{
		wfID:    *wfID,
		wfType:  *wfType,
		address: *address,
		queue:   *taskQueue,
		query:   *queryName,
		every:   *pollEvery,
		budget:  *startup,
		cur:     uiState{Version: -1},
		echo:    prop.NewSource("(nothing sent yet)"),
	}
	if *frameTrace != "" {
		f, err := os.OpenFile(*frameTrace, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			fatal("cannot open trace file: %v", err)
		}
		ui.trace = f // closed after Run; this main always ends in os.Exit
	}

	// --- the companions: this app's services, with this app's lifetime ---
	//
	// Order is start order, and it matters here: the worker dials the
	// server the line above it starts. Everything below runs before the
	// terminal is opened and before the first screen is asked for, which
	// is why session.Build can simply assume both are up.
	var companions []gooey.Companion
	opts := []gooey.Option{}
	if *withDev {
		companions = append(companions, devServer(*address, *devLog))
		// A server that has to bind a socket takes about a second to
		// discover it cannot. The framework's default grace window is
		// 100ms, which is right for a goroutine and far too short for
		// this: without the wider window, "port already in use" arrives
		// AFTER the screen is taken, and the user sees the wizard flash
		// up and vanish instead of a sentence explaining itself. The
		// window costs nothing here — it overlaps the time Build would
		// have spent retrying the dial anyway.
		opts = append(opts, gooey.WithCompanionGrace(2*time.Second))
	}
	if *withWorker {
		// The application itself, in this process, for exactly as long as
		// the UI is on screen. NopLogger because a worker sharing a
		// terminal with a TUI has no more claim on stderr than the TUI
		// does; the standalone workers/wizardworker keeps the default logger,
		// since stderr is its whole UI.
		companions = append(companions, gooey.CompanionFunc("wizard-worker",
			func(ctx context.Context) error {
				c, err := wizard.Dial(ctx, *address, temporalhandlers.NopLogger)
				if err != nil {
					return err
				}
				defer c.Close()
				return wizard.Run(ctx, c, *taskQueue)
			}))
	}

	app := gooey.NewApp(ui, append(opts, gooey.WithCompanions(companions...))...)
	ui.app = app

	// The shell's own chrome — the one gesture that belongs to the
	// terminal rather than to the served screen. OnEvent is an OBSERVER:
	// it cannot consume the key, so the tree still sees it, but the app
	// quits regardless. That is the difference between this and the
	// framework's ordinary quit key, which fires only on what the tree
	// declines, and it is what makes "a workflow cannot serve a page you
	// cannot leave" true rather than merely usual.
	app.OnEvent(func(ev input.Event) {
		if ev.IsKey() && ev.Key == (input.KeyEvent{Key: input.KeyRune, Rune: 'c', Mods: input.ModCtrl}) {
			app.Quit()
		}
	})
	if *exitAfter > 0 {
		time.AfterFunc(*exitAfter, app.Quit) // Quit is safe from any goroutine
	}

	err := app.Run(context.Background())

	// Run has returned, so the terminal is cooked, the companions are
	// stopped and the poller is joined. Ordinary printing from here.
	if ui.client != nil {
		ui.client.Close()
	}
	if ui.trace != nil {
		ui.trace.Close()
	}
	// Nothing to report about a session that never had a screen; the
	// error below is the whole story, and a row of zeros above it only
	// makes it harder to read.
	if ui.builds > 0 {
		fmt.Printf("wizardui: %s · polls %d · screens built %d · value updates %d · last stage %q\n",
			ui.wfID, ui.polls, ui.builds, ui.updates, ui.cur.Stage)
	}
	if ui.lastErr != nil {
		fmt.Fprintf(os.Stderr, "wizardui: last build error: %v\n", ui.lastErr)
	}
	// A child process's exit status is a number, and a number is a poor
	// explanation. The app knows what it asked for, so it is the one that
	// can say what usually goes wrong with it.
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

// devServer is the flagship CompanionCmd: a whole server, owned by a
// terminal UI, for the length of one demo.
//
// It is opt-in and it is NOT the default, on purpose. A dev server holds
// state that outlives any one client — you want to `temporal workflow
// show` after the UI closes — and a program that silently deletes the
// thing you were about to inspect is a bad neighbor. The worker is the
// opposite case: it holds nothing, so owning its lifetime costs nothing.
//
// LookPath first so the failure reads like a missing tool rather than a
// failed exec; either way it happens before the screen is taken.
//
// It is bound to the same address the client dials, rather than to the
// CLI's default. A server and a client in one process disagreeing about
// where the server is would be an absurd way to fail, and --address is
// the only place either of them gets told.
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
	// Without an output option the child's stdout and stderr go to
	// os.DevNull. A server logging onto a raw-mode alt screen is the
	// SDK-logger problem one level up, and there is no repairing it from
	// here: those bytes are not ours to repaint.
	return gooey.CompanionCmd("temporal-dev", cmd, opts...)
}

// session is the shell's whole state — a connection, the current screen,
// and the property sources that screen is bound to — and it is also the
// App's Content.
//
// Being Content is what puts the connection in the right place. Build
// runs on the UI goroutine AFTER the companions are up and BEFORE the
// terminal is opened, which is exactly the window in which "dial the
// server, attach to the workflow, wait for a screen" belongs: it may
// take seconds, it may fail, and either way it should be ordinary text
// on a cooked terminal rather than something happening behind an alt
// screen.
type session struct {
	app    *gooey.App
	client client.Client

	address string
	queue   string
	wfID    string
	wfType  string
	query   string
	every   time.Duration
	budget  time.Duration

	cur     uiState
	sources map[string]*prop.Property[string]
	echo    *prop.Property[string] // the shell's own, survives stage swaps

	trace   *os.File
	polls   int
	builds  int
	updates int
	lastErr error
}

// Build connects if it has not yet, waits for the application to serve a
// screen, and returns that screen as a widget tree.
func (s *session) Build() (gooey.Widget, error) {
	if s.client == nil {
		if err := s.connect(); err != nil {
			return nil, err
		}
	}
	st, err := s.await(s.budget)
	if err != nil {
		return nil, fmt.Errorf("the workflow never served a screen: %w\n"+
			"is a worker running on task queue %q?  pass --with-worker, or run ./workers/wizardworker", err, s.queue)
	}
	return s.tree(st)
}

// Watch is where the poller lives. The App calls it once the tree is on
// screen and calls the returned stop during teardown, which is exactly
// the poller's useful life — and the cancellation joins it, so nothing
// is still querying a client main is about to close.
//
// It ignores the `changed` callback. That callback means "rebuild from
// Content", and this content rebuilds itself: a new screen is a Swap
// with a tree built from the payload that announced it, not a re-read of
// a source that has no idea what changed.
func (s *session) Watch(func()) func() {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.poll(ctx)
	}()
	return func() { cancel(); <-done }
}

// connect dials, starts or attaches the session, and grants the one
// capability served markup gets.
func (s *session) connect() error {
	ctx, cancel := context.WithTimeout(context.Background(), s.budget)
	defer cancel()

	// Retrying rather than failing on the first refusal: with
	// --with-dev-server the server started milliseconds ago and is not
	// listening yet, which is not an error, it is a startup.
	c, err := wizard.Dial(ctx, s.address, temporalhandlers.NopLogger)
	if err != nil {
		return fmt.Errorf("cannot reach the Temporal server at %s: %w\n"+
			"start one with: temporal server start-dev --headless (or pass --with-dev-server)", s.address, err)
	}

	// Start the session, or attach to the one already running under this
	// ID. Attaching is the interesting case: the application's state lives
	// on the server, so a second terminal — or the same terminal after a
	// crash — picks up exactly the screen the workflow is on.
	if _, err := c.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:        s.wfID,
		TaskQueue: s.queue,
	}, s.wfType); err != nil {
		c.Close()
		return fmt.Errorf("cannot start or attach to workflow %q: %w", s.wfID, err)
	}
	s.client = c

	// --- the capability grant ---
	markup.RegisterHandlers(temporalhandlers.WorkflowURI,
		temporalhandlers.NewWorkflowUI(c, s.wfID))
	return nil
}

// await retries the first query until a worker answers it WITH a screen.
// There are two distinct waits here: between ExecuteWorkflow returning
// and the workflow's first task being picked up the query has nowhere to
// go, and after that the workflow answers with an empty state until it
// has served its first markup. Neither is an error — an application is
// allowed to take a moment to have a UI.
func (s *session) await(limit time.Duration) (uiState, error) {
	ctx, cancel := context.WithTimeout(context.Background(), limit)
	defer cancel()
	last := fmt.Errorf("no answer from the %q query", s.query)
	for {
		st, err := s.fetch(ctx)
		switch {
		case err != nil:
			last = err
		case st.Markup == "":
			last = fmt.Errorf("the workflow has not served a screen yet")
		default:
			return st, nil
		}
		select {
		case <-ctx.Done():
			return uiState{}, last
		case <-time.After(250 * time.Millisecond):
		}
	}
}

func (s *session) fetch(ctx context.Context) (uiState, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	enc, err := s.client.QueryWorkflow(ctx, s.wfID, "", s.query)
	if err != nil {
		return uiState{}, err
	}
	var st uiState
	if err := enc.Get(&st); err != nil {
		return uiState{}, err
	}
	return st, nil
}

// poll asks for the current screen on an interval and hands each answer
// to the UI goroutine, which is the only place a screen may be applied:
// applying one Sets properties, and properties belong to the loop.
//
// Failures are silent on purpose: a query that fails during a worker
// restart should not tear down a UI whose state lives on the server
// anyway.
func (s *session) poll(ctx context.Context) {
	t := time.NewTicker(s.every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			st, err := s.fetch(ctx)
			if err != nil {
				continue
			}
			s.app.Post(func() {
				if err := s.apply(st); err != nil {
					// A served screen that will not build leaves the last
					// good one on display. The client has no place to draw
					// its own error — the screen belongs to the workflow —
					// so it is reported on the way out.
					s.lastErr = err
				}
			})
		}
	}
}

// apply is the swap rule, and the reason the contract carries two
// counters.
//
// A new markup VERSION means a different screen: build a fresh widget
// tree against fresh sources and hand it to the App, which closes the
// outgoing composition and attaches the new one. A new REVISION on the
// same markup means the same screen with new numbers: Set the sources
// that changed and let the property graph repaint exactly the widgets
// that read them — which, on the provisioning screen, is one line per
// completed activity rather than a whole page.
func (s *session) apply(st uiState) error {
	s.polls++
	if st.Markup == "" {
		// The application is between screens. Keep showing the last one.
		return nil
	}
	if st.Version < s.cur.Version {
		// A query that was answered late, from before the last swap.
		// Rebuilding from it would walk the UI backwards.
		return nil
	}
	if st.Version != s.cur.Version || !s.boundTo(st.Values) {
		w, err := s.tree(st)
		if err != nil {
			return err
		}
		s.app.Swap(w)
		return nil
	}
	if st.Revision == s.cur.Revision {
		return nil
	}
	for k, v := range st.Values {
		p := s.sources[k]
		// Read outside any evaluation, so this records no dependency —
		// and skipping unchanged values keeps the repaint honest.
		if p != nil && p.Get() != v {
			p.Set(v)
			s.updates++
		}
	}
	s.cur = st
	s.tracef("values r%d stage=%s", st.Revision, st.Stage)
	return nil
}

// boundTo reports whether the current tree's sources still cover exactly
// the served key set. They normally do — the workflow keeps the shape of
// its values constant — but a served map that grew a key would leave a
// binding with nothing behind it, so that case forces a rebuild.
func (s *session) boundTo(values map[string]string) bool {
	if len(values) != len(s.sources) {
		return false
	}
	for k := range values {
		if _, ok := s.sources[k]; !ok {
			return false
		}
	}
	return true
}

// tree builds a widget tree from a served screen and adopts its sources
// as the current ones. It runs on the UI goroutine either way — from
// Build at startup, from a posted apply afterwards — because resolving
// bindings touches the property graph.
func (s *session) tree(st uiState) (gooey.Widget, error) {
	sources := make(map[string]*prop.Property[string], len(st.Values))
	values := make(map[string]any, len(st.Values))
	for k, v := range st.Values {
		p := prop.NewSource(v)
		sources[k] = p
		values[k] = p
	}
	// The shell's contribution, added after the served values so the
	// reserved name always resolves to the client's own property.
	values[echoKey] = s.echo

	ctx := &markup.Context{
		Values:     values,
		Styles:     theme,
		Dispatcher: s.app.Dispatcher(),
	}
	// Build, not Load: this markup never touched a filesystem.
	w, err := markup.Build([]byte(st.Markup), ctx)
	if err != nil {
		return nil, fmt.Errorf("stage %q markup v%d: %w", st.Stage, st.Version, err)
	}
	s.sources = sources
	s.cur = st
	s.builds++
	s.tracef("build v%d r%d stage=%s bytes=%d", st.Version, st.Revision, st.Stage, len(st.Markup))
	return w, nil
}

func (s *session) tracef(format string, args ...any) {
	if s.trace == nil {
		return
	}
	fmt.Fprintf(s.trace, "%s  %s\n", time.Now().Format("15:04:05.000"), fmt.Sprintf(format, args...))
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "wizardui: "+format+"\n", args...)
	os.Exit(1)
}

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
//	temporal server start-dev --headless   # shell 1
//	go run ./cmd/wizardworker              # shell 2
//	go run ./cmd/wizardui                  # shell 3
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/WonderForgeLabs/gooey"
	temporalhandlers "github.com/WonderForgeLabs/gooey/handlers/temporal"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
	"github.com/WonderForgeLabs/gooey/term"
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
		taskQueue  = flag.String("queue", envOr("GOOEY_TASK_QUEUE", "gooey-wizard"), "task queue the application's worker serves")
		wfType     = flag.String("workflow", envOr("GOOEY_WORKFLOW", "ProvisionWizard"), "workflow type to start or attach to")
		wfID       = flag.String("id", envOr("GOOEY_WORKFLOW_ID", "gooey-wizard"), "workflow ID of the session")
		queryName  = flag.String("query", envOr("GOOEY_UI_QUERY", "ui"), "query that returns the screen")
		pollEvery  = flag.Duration("poll", 400*time.Millisecond, "how often to ask for the current screen")
		exitAfter  = flag.Duration("exit-after", 0, "quit on a timer, for scripted captures (0 = never)")
		frameTrace = flag.String("trace", "", "append one line per screen change to this file")
	)
	flag.Parse()

	// NopLogger: the SDK's default logger writes to stderr, which in
	// raw mode prints straight over the UI's bottom rows.
	tc, err := client.Dial(client.Options{HostPort: *address, Logger: temporalhandlers.NopLogger})
	if err != nil {
		fatal("cannot reach the Temporal server at %s: %v\n"+
			"start one with: temporal server start-dev --headless", *address, err)
	}
	defer tc.Close()

	// Start the session, or attach to the one already running under this
	// ID. Attaching is the interesting case: the application's state lives
	// on the server, so a second terminal — or the same terminal after a
	// crash — picks up exactly the screen the workflow is on.
	run, err := tc.ExecuteWorkflow(context.Background(), client.StartWorkflowOptions{
		ID:        *wfID,
		TaskQueue: *taskQueue,
	}, *wfType)
	if err != nil {
		fatal("cannot start or attach to workflow %q: %v\n"+
			"is the worker running?  go run ./cmd/wizardworker", *wfID, err)
	}

	// --- the capability grant ---
	markup.RegisterHandlers(temporalhandlers.WorkflowURI,
		temporalhandlers.NewWorkflowUI(tc, *wfID))

	ui := &session{
		client: tc,
		wfID:   *wfID,
		query:  *queryName,
		disp:   gooey.NewDispatcher(),
		cur:    uiState{Version: -1},
		echo:   prop.NewSource("(nothing sent yet)"),
	}
	if *frameTrace != "" {
		f, err := os.OpenFile(*frameTrace, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			fatal("cannot open trace file: %v", err)
		}
		defer f.Close()
		ui.trace = f
	}

	// The first screen has to arrive before the terminal is worth opening:
	// a worker that is not up yet is a startup error, not a blank UI.
	first, err := ui.await(20 * time.Second)
	if err != nil {
		fatal("the workflow never served a screen: %v\n"+
			"is the worker running on task queue %q?", err, *taskQueue)
	}

	screen, err := term.Open()
	if err != nil {
		fatal("no tty: %v", err)
	}
	ui.cols, ui.rows = screen.Size()

	if err := ui.apply(first); err != nil {
		fatal("the first served screen did not build: %v", err)
	}

	if err := screen.Raw(); err != nil {
		fatal("%v", err)
	}
	defer screen.Restore()
	screen.EnableMouse()

	events := make(chan input.Event, 64)
	go term.DecodeEvents(screen, events)

	states := make(chan uiState, 1)
	stopPoll := make(chan struct{})
	go ui.poll(*pollEvery, states, stopPoll)
	defer close(stopPoll)

	var deadline <-chan time.Time
	if *exitAfter > 0 {
		deadline = time.After(*exitAfter)
	}

	running := true
	for running {
		if ui.needsFrame {
			ui.comp.Frame()
			ui.comp.Flush(screen.File())
			ui.needsFrame = false
		}
		select {
		case <-ui.disp.Wake():
			// Signal receipts, marshaled back onto this goroutine.
			ui.disp.Drain()
		case st := <-states:
			if err := ui.apply(st); err != nil {
				// A served screen that will not build leaves the last good
				// one on display. The client has no place to draw its own
				// error — the screen belongs to the workflow — so it is
				// reported on the way out.
				ui.lastErr = err
			}
		case ev := <-events:
			if isQuit(ev) {
				running = false
				continue
			}
			ui.comp.Handle(ev)
		case <-deadline:
			running = false
		}
	}

	if ui.comp != nil {
		ui.comp.Close()
	}
	screen.Restore()
	fmt.Printf("wizardui: %s · polls %d · screens built %d · value updates %d · last stage %q\n",
		run.GetID(), ui.polls, ui.builds, ui.updates, ui.cur.Stage)
	if ui.lastErr != nil {
		fmt.Fprintf(os.Stderr, "wizardui: last build error: %v\n", ui.lastErr)
	}
}

// session is the shell's whole state: a connection, the current screen,
// and the property sources the current screen is bound to.
type session struct {
	client client.Client
	wfID   string
	query  string

	disp       *gooey.Dispatcher
	comp       *gooey.Composer
	cols, rows int
	needsFrame bool

	cur     uiState
	sources map[string]*prop.Property[string]
	echo    *prop.Property[string] // the shell's own, survives stage swaps

	trace   *os.File
	polls   int
	builds  int
	updates int
	lastErr error
}

// await retries the first query until a worker answers it WITH a screen.
// There are two distinct waits here: between ExecuteWorkflow returning
// and the workflow's first task being picked up the query has nowhere to
// go, and after that the workflow answers with an empty state until it
// has served its first markup. Neither is an error — an application is
// allowed to take a moment to have a UI.
func (s *session) await(limit time.Duration) (uiState, error) {
	deadline := time.Now().Add(limit)
	last := fmt.Errorf("no answer from the %q query", s.query)
	for time.Now().Before(deadline) {
		st, err := s.fetch()
		switch {
		case err != nil:
			last = err
		case st.Markup == "":
			last = fmt.Errorf("the workflow has not served a screen yet")
		default:
			return st, nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	return uiState{}, last
}

func (s *session) fetch() (uiState, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
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

// poll asks for the current screen on an interval. Failures are silent
// on purpose: a query that fails during a worker restart should not tear
// down a UI whose state lives on the server anyway.
func (s *session) poll(every time.Duration, out chan<- uiState, stop <-chan struct{}) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			st, err := s.fetch()
			if err != nil {
				continue
			}
			select {
			case out <- st:
			case <-stop:
				return
			}
		}
	}
}

// apply is the swap rule, and the reason the contract carries two
// counters.
//
// A new markup VERSION means a different screen: build a fresh widget
// tree against fresh sources and hand it to a new Composer. A new
// REVISION on the same markup means the same screen with new numbers:
// Set the sources that changed and let the property graph repaint
// exactly the widgets that read them — which, on the provisioning
// screen, is one line per completed activity rather than a whole page.
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
		return s.rebuild(st)
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

func (s *session) rebuild(st uiState) error {
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
		Dispatcher: s.disp,
	}
	// Build, not Load: this markup never touched a filesystem.
	w, err := markup.Build([]byte(st.Markup), ctx)
	if err != nil {
		return fmt.Errorf("stage %q markup v%d: %w", st.Stage, st.Version, err)
	}

	if s.comp != nil {
		s.comp.Close() // the outgoing screen's timers stop with it
	}
	s.sources = sources
	s.comp = gooey.NewComposer(w, s.cols, s.rows)
	s.comp.OnInvalidate(func() { s.needsFrame = true })
	s.comp.Start(s.disp)
	s.needsFrame = true
	s.cur = st
	s.builds++
	s.tracef("build v%d r%d stage=%s bytes=%d", st.Version, st.Revision, st.Stage, len(st.Markup))
	return nil
}

func (s *session) tracef(format string, args ...any) {
	if s.trace == nil {
		return
	}
	fmt.Fprintf(s.trace, "%s  %s\n", time.Now().Format("15:04:05.000"), fmt.Sprintf(format, args...))
}

// isQuit is the shell's own chrome — the one gesture that belongs to the
// terminal rather than to the served screen, so a workflow can never
// serve a page you cannot leave.
func isQuit(ev input.Event) bool {
	return ev.IsKey() && ev.Key == input.KeyEvent{Key: input.KeyRune, Rune: 'c', Mods: input.ModCtrl}
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

package gooey

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"
)

// Companions are the app's background services: work that must be
// running for the UI to mean anything, and must not outlive it.
//
// The demo that motivated this needed three shells — a Temporal dev
// server, a worker, the UI — and the middle one is not a separate
// program in any interesting sense. It is a service the UI depends on,
// whose lifetime is exactly the UI's. Every attempt to express that by
// hand produced the same two bugs: a worker started before the terminal
// was known to be openable, so a startup failure flashed an alt screen
// and vanished; and a worker that outlived the app, because "stop it on
// the way out" was a defer nobody wired to the panic path.
//
// A Companion is that lifetime, made explicit:
//
//   - started BEFORE the tree is built and before raw mode, so a service
//     that cannot start prints its error onto a cooked terminal, and so a
//     tree whose construction talks to the service (gooey's own
//     server-driven demo builds its first screen from a workflow query)
//     finds it already up;
//   - supervised WHILE the app runs, so a service that dies takes the app
//     down with it rather than leaving a UI wired to nothing;
//   - stopped and WAITED for during teardown on every path — quit,
//     signal, context cancellation, panic — after the terminal has been
//     handed back, so a slow shutdown happens on a cooked screen.
//
// Two constructors cover the two kinds. CompanionFunc runs Go code on a
// goroutine; CompanionCmd runs a child process. Anything else implements
// the interface directly — it is three methods and no reflection.

// Companion is lifecycle-bound background work owned by an App.
//
// Start launches the work and returns once it is running, or an error if
// it could not be launched at all. The ctx it receives is the app's: it
// is cancelled at teardown, and cancelling it is the ONLY way the App
// asks a companion to stop. There is no separate Stop method, because
// two stop mechanisms is one too many.
//
// Wait blocks until the work has finished and reports how. It is called
// exactly once per successful Start, on a goroutine the App owns.
//
// Name identifies the companion in errors. It is a label, not a key —
// nothing looks companions up by it.
type Companion interface {
	Name() string
	Start(ctx context.Context) error
	Wait() error
}

// CompanionPhase says which half of a companion's life an error came
// from, because the two mean very different things to a user: one is
// "your environment is not ready", the other is "something that was
// working stopped".
type CompanionPhase string

const (
	// PhaseStart is a companion that never got going. Run returns before
	// touching the terminal.
	PhaseStart CompanionPhase = "start"
	// PhaseRun is a companion that was running and is not anymore. The
	// app quits and Run reports it.
	PhaseRun CompanionPhase = "run"
)

// CompanionError is what Run returns when a companion is the reason the
// app is not running.
//
// A PhaseRun error with a nil Err is not a contradiction: it is a
// companion that exited zero while the app still needed it. A service
// that stops is a failed service, and the app has no more business being
// on screen than if it had crashed.
type CompanionError struct {
	Name  string
	Phase CompanionPhase
	Err   error
}

func (e *CompanionError) Error() string {
	switch {
	case e.Phase == PhaseStart:
		return fmt.Sprintf("gooey: companion %q failed to start: %v", e.Name, e.Err)
	case e.Err == nil:
		return fmt.Sprintf("gooey: companion %q stopped while the app was running", e.Name)
	default:
		return fmt.Sprintf("gooey: companion %q stopped while the app was running: %v", e.Name, e.Err)
	}
}

func (e *CompanionError) Unwrap() error { return e.Err }

// WithCompanions registers background services to run for exactly this
// app's lifetime. Order matters: they are started in the order given and
// stopped in reverse, so a companion may depend on one declared before
// it.
func WithCompanions(cs ...Companion) Option {
	return func(o *options) { o.companions = append(o.companions, cs...) }
}

// WithCompanionGrace sets how long Run watches freshly started
// companions before it builds a tree and takes the screen.
//
// It exists because "started" is not "running" for anything interesting.
// exec.Cmd.Start reports a missing binary, but not a binary that starts
// and exits two milliseconds later complaining about a port; a goroutine
// reports nothing at all. The grace window catches that class — the
// error prints on a cooked terminal instead of behind an alt screen that
// appears and disappears — and it ends EARLY on the first failure, so it
// only ever costs its full duration when nothing is wrong.
//
// Zero disables the wait; a companion that dies during startup is then
// an ordinary mid-run exit.
func WithCompanionGrace(d time.Duration) Option {
	return func(o *options) { o.compGrace = d }
}

// WithCompanionStopTimeout bounds the wait for companions to finish
// after teardown cancels their context. Past it the app gives up and
// returns, and CompanionLeaked reports that it did.
//
// It is deliberately longer than CompanionCmd's own kill delay: a child
// process should get SIGKILLed by its companion rather than abandoned by
// the app.
func WithCompanionStopTimeout(d time.Duration) Option {
	return func(o *options) { o.compStopTO = d }
}

// AddCompanion registers a companion before Run. It is WithCompanions
// for the case where the companion closes over the App itself — a
// service that Posts its progress into the UI cannot be constructed
// until the App exists.
//
// Adding after Run has started is a no-op: companions are started once,
// before the first frame, and a service that joins later has a different
// lifetime than the one this whole mechanism is about.
func (a *App) AddCompanion(c Companion) {
	if c == nil || a.compStarted != nil {
		return
	}
	a.opt.companions = append(a.opt.companions, c)
}

// Companions reports the companions this app will run, in start order.
func (a *App) Companions() []Companion {
	return append([]Companion(nil), a.opt.companions...)
}

// CompanionLeaked reports whether teardown gave up waiting for a
// companion. Like DecoderLeaked it should always be false; it is the
// tripwire for a service that ignored its cancelled context, which
// otherwise shows up as a mysteriously surviving process long after the
// terminal came back.
func (a *App) CompanionLeaked() bool { return a.compLeaked }

type companionExit struct {
	name string
	err  error
}

// startCompanions starts every companion in order and leaves a
// supervisor watching each. A failure to start stops what did start —
// the caller runs stopCompanions — and aborts Run before any terminal is
// opened.
func (a *App) startCompanions(ctx context.Context) error {
	a.compStarted = []Companion{}
	if len(a.opt.companions) == 0 {
		return nil
	}
	a.compCtx, a.compCancel = context.WithCancel(ctx)
	a.compExit = make(chan companionExit, len(a.opt.companions))

	for _, c := range a.opt.companions {
		if err := c.Start(a.compCtx); err != nil {
			return &CompanionError{Name: c.Name(), Phase: PhaseStart, Err: err}
		}
		a.compStarted = append(a.compStarted, c)
		go a.superviseCompanion(c)
	}
	return a.companionGrace()
}

// companionGrace is the "verify liveness where knowable" window: wait a
// beat, and if anything has already died, report it as a start failure
// rather than a mid-run one. To the user those are the same event — the
// thing never came up — and only this code can tell them apart.
func (a *App) companionGrace() error {
	if a.opt.compGrace <= 0 {
		return nil
	}
	t := time.NewTimer(a.opt.compGrace)
	defer t.Stop()
	select {
	case ex := <-a.compExit:
		a.compDone++
		return &CompanionError{Name: ex.name, Phase: PhaseStart, Err: ex.err}
	case <-t.C:
		return nil
	}
}

// superviseCompanion turns "this service finished" into UI-goroutine
// work, the same way every other asynchronous thing in this framework
// reaches the loop. The exit is recorded on the channel teardown drains
// AND posted, because the two readers want different things: teardown
// wants to know the goroutine is gone, the loop wants to quit.
func (a *App) superviseCompanion(c Companion) {
	err := c.Wait()
	a.compExit <- companionExit{name: c.Name(), err: err}
	a.disp.Post(func() {
		if a.compStopping {
			return // we asked for this one
		}
		if a.compErr == nil {
			a.compErr = &CompanionError{Name: c.Name(), Phase: PhaseRun, Err: err}
		}
		a.Quit()
	})
}

// stopCompanions cancels the companion context and waits, bounded, for
// every supervisor to report in. Run defers it AFTER teardown, so by the
// time a service is asked to stop the terminal is already cooked and
// whatever it prints on the way out is readable.
//
// It runs on the UI goroutine and is idempotent: the panic path reaches
// teardown twice.
func (a *App) stopCompanions() {
	if a.compStopped {
		return
	}
	a.compStopped = true
	a.compStopping = true
	if a.compCancel != nil {
		a.compCancel()
	}
	if len(a.compStarted) == 0 {
		return
	}
	timeout := a.opt.compStopTO
	if timeout <= 0 {
		timeout = defaultCompanionStopTimeout
	}
	t := time.NewTimer(timeout)
	defer t.Stop()
	for a.compDone < len(a.compStarted) {
		select {
		case <-a.compExit:
			a.compDone++
		case <-t.C:
			a.compLeaked = true
			return
		}
	}
}

const (
	defaultCompanionGrace       = 100 * time.Millisecond
	defaultCompanionStopTimeout = 10 * time.Second
	defaultCompanionKillDelay   = 5 * time.Second
)

// CompanionFunc is a companion that runs Go code on a goroutine for the
// app's lifetime.
//
//	gooey.CompanionFunc("worker", func(ctx context.Context) error {
//	    return worker.Run(ctx, client, "task-queue")
//	})
//
// run must return when ctx is done — that is the whole contract, and the
// stop-timeout tripwire is what catches code that does not. It runs on
// its own goroutine, so it must NOT touch the property graph: results
// reach the UI through App.Post like every other background thing.
//
// A panic inside run is converted to an error rather than allowed to
// kill the process, because a process that dies here dies with the
// terminal in raw mode on the alternate screen. The app quits reporting
// the panic, having restored the terminal first.
func CompanionFunc(name string, run func(ctx context.Context) error) Companion {
	return &funcCompanion{name: name, run: run}
}

type funcCompanion struct {
	name string
	run  func(context.Context) error
	done chan error
}

func (c *funcCompanion) Name() string { return c.name }

func (c *funcCompanion) Start(ctx context.Context) error {
	if c.run == nil {
		return errors.New("no run function")
	}
	c.done = make(chan error, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				c.done <- fmt.Errorf("panic: %v", r)
			}
		}()
		c.done <- c.run(ctx)
	}()
	return nil
}

func (c *funcCompanion) Wait() error { return <-c.done }

// CmdOption configures a CompanionCmd.
type CmdOption func(*cmdCompanion)

// CompanionOutput sends the child's stdout and stderr to w.
//
// The default is os.DevNull, and the reason is the SDK-logger lesson at
// process level: a gooey app owns the terminal, and a child that writes
// to the inherited stdout paints over the UI's bottom rows in raw mode
// with no way to repair the damage until the next full flush. A child's
// output goes to a file or a buffer or nowhere — never to the tty the
// app is drawing on.
//
// An *os.File is passed to the child directly; anything else means
// os/exec pipes the output and copies it, which also means the child
// inherits a pipe rather than a file.
func CompanionOutput(w io.Writer) CmdOption {
	return func(c *cmdCompanion) { c.out = w }
}

// CompanionKillDelay sets how long the child gets between the polite
// stop signal and SIGKILL. Default five seconds.
func CompanionKillDelay(d time.Duration) CmdOption {
	return func(c *cmdCompanion) { c.kill = d }
}

// CompanionCmd is a companion that runs a child process for the app's
// lifetime — a sidecar the app would otherwise ask its user to start in
// another shell:
//
//	gooey.CompanionCmd("temporal-dev",
//	    exec.Command("temporal", "server", "start-dev", "--headless"),
//	    gooey.CompanionOutput(logFile))
//
// Check the binary exists first (exec.LookPath) if you want to explain
// its absence in your own words; otherwise Start's error does, and Run
// returns it before taking the screen.
//
// Three things it does that a hand-rolled exec.Cmd usually does not:
//
//  1. Output goes to os.DevNull unless CompanionOutput says otherwise.
//     The terminal belongs to the UI.
//  2. The child gets its own process GROUP, and teardown signals the
//     group rather than the process. A service that spawns children —
//     most dev servers do — otherwise leaves them orphaned and holding
//     the port, and the next run of your app fails for a reason that has
//     nothing to do with your app.
//  3. Teardown escalates: stop signal, wait, then SIGKILL. A child that
//     ignores the polite version delays the app by CompanionKillDelay,
//     not forever.
//
// Note what is NOT here: nothing restarts the child, and its exit —
// even a zero one — quits the app. A companion is a service, not a job.
func CompanionCmd(name string, cmd *exec.Cmd, opts ...CmdOption) Companion {
	c := &cmdCompanion{name: name, cmd: cmd, kill: defaultCompanionKillDelay}
	for _, o := range opts {
		o(c)
	}
	return c
}

type cmdCompanion struct {
	name string
	cmd  *exec.Cmd
	out  io.Writer
	kill time.Duration

	devnull *os.File
	done    chan error
}

func (c *cmdCompanion) Name() string { return c.name }

// Process is the child, or nil before Start. For tests and for callers
// that want to signal it themselves.
func (c *cmdCompanion) Process() *os.Process {
	if c.cmd == nil {
		return nil
	}
	return c.cmd.Process
}

func (c *cmdCompanion) Start(ctx context.Context) error {
	if c.cmd == nil {
		return errors.New("no command")
	}
	out := c.out
	if out == nil {
		f, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
		if err != nil {
			return err
		}
		c.devnull, out = f, f
	}
	c.cmd.Stdout, c.cmd.Stderr = out, out
	setProcessGroup(c.cmd)

	if err := c.cmd.Start(); err != nil {
		c.closeDevNull()
		return err
	}

	exited := make(chan error, 1)
	go func() { exited <- c.cmd.Wait() }()

	c.done = make(chan error, 1)
	go func() {
		defer c.closeDevNull()
		select {
		case err := <-exited:
			c.done <- err
			return
		case <-ctx.Done():
		}
		terminateProcess(c.cmd.Process)
		t := time.NewTimer(c.kill)
		defer t.Stop()
		select {
		case err := <-exited:
			c.done <- err
		case <-t.C:
			killProcess(c.cmd.Process)
			c.done <- <-exited
		}
	}()
	return nil
}

func (c *cmdCompanion) Wait() error { return <-c.done }

func (c *cmdCompanion) closeDevNull() {
	if c.devnull != nil {
		c.devnull.Close()
		c.devnull = nil
	}
}

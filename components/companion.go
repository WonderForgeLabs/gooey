package components

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/prop"
)

// Companion is a child-process service declared in the tree — the markup
// tier of gooey.Companion (see docs/specs/2026-08-10-markup-companions.md).
//
//	<Companion Name="worker" Path="python3" Dir="../worker" Log="worker.log">
//	  <Companion.Args><Arg>worker.py</Arg></Companion.Args>
//	  <Companion.Env><Var Name="URL" Value="{{.Endpoint}}"/></Companion.Env>
//	</Companion>
//
// Like Timer and KeyBinding it is non-visual: it lives in the tree as an
// attachment, is never measured, arranged or painted, and its LIFETIME
// BELONGS TO THE COMPOSER. Composer.Start launches the child;
// Composer.Close — reached on every teardown path, and on every tree
// replacement — stops it and waits.
//
// That is the one thing it does differently from an App-level companion,
// which starts before the tree exists and stops after the terminal is
// restored. The trade is uniformity: every path that installs a tree
// (startup, hot reload, an MCP swap, a patched subtree) starts and stops
// these identically, where an App-level registration is only reachable
// before Run.
//
// The process machinery is gooey.CompanionCmd's, unchanged: its own
// process group, kill escalation, and output to os.DevNull unless Log
// says otherwise. There is no way to spell "inherit the app's stdout",
// because a child writing to a raw-mode terminal paints over the UI.
//
// Failure is reported into the property graph rather than being fatal: a
// child that cannot start, or exits without being asked to, sets Error
// and runs Exited. An app that wants the App-level behavior — a dead
// service takes the UI with it — binds Exited to its quit command.
type Companion struct {
	gooey.Base

	// Name labels the companion in errors. It is not a key; nothing
	// looks a companion up by it.
	Name string
	// Path is the executable, already resolved (the markup builder runs
	// exec.LookPath at load time so a missing binary is a load error).
	Path string
	// Args is the argv after Path, one handle per argument. Each is read
	// ONCE, when the child starts: an argv is a value a process was
	// launched with, not a property it observes.
	Args []*prop.Property[string]
	// Env are environment entries applied on top of the inherited
	// environment, or the whole environment under CleanEnv. Values are
	// read at start time like Args.
	Env []EnvVar
	// CleanEnv starts the child from an empty environment instead of
	// inheriting the app's.
	CleanEnv bool
	// Dir is the child's working directory ("" = the app's).
	Dir string
	// Log is a file path the child's stdout and stderr are truncated
	// into, opened at start and closed once the child has stopped. Empty
	// means os.DevNull.
	Log string
	// KillDelay is the grace between the polite stop signal and SIGKILL
	// (0 = CompanionCmd's default).
	KillDelay time.Duration
	// StopTimeout bounds how long stopping waits for the child after
	// cancelling it. It runs on the UI goroutine during teardown, so it
	// must be bounded; past it Leaked reports that the wait gave up.
	StopTimeout time.Duration
	// Error, when bound, receives a *gooey.CompanionError's message when
	// the child fails to start or exits unbidden, and "" on a successful
	// start.
	Error *prop.Property[string]
	// Exited runs on the UI goroutine when the child is gone for a
	// reason nobody asked for — including never having started.
	Exited gooey.Action

	err    error
	leaked bool
}

// EnvVar is one environment entry: a fixed name and a value that may be
// bound.
type EnvVar struct {
	Name  string
	Value *prop.Property[string]
}

// DefaultCompanionStopTimeout bounds the teardown wait for a
// tree-declared companion. It is deliberately longer than
// CompanionCmd's kill delay, for the App-level reason: a child should be
// SIGKILLed by its companion rather than abandoned by the composition.
const DefaultCompanionStopTimeout = 10 * time.Second

func (c *Companion) Measure(gooey.Size) gooey.Size { return gooey.Size{} }
func (c *Companion) Render(*gooey.Frame)           {}
func (c *Companion) NonVisual() bool               { return true }

// Err is the last failure reported, or nil. Set on the UI goroutine.
func (c *Companion) Err() error { return c.err }

// Leaked reports whether stopping gave up waiting for the child. Like
// App.CompanionLeaked it should always be false; it is the tripwire for
// a process that ignored its cancelled context.
func (c *Companion) Leaked() bool { return c.leaked }

// Start launches the child and returns the stop func the Composer will
// call. It runs on the UI goroutine, which is what makes reading the Arg
// and Env handles legal here — and the reads happen outside any computed
// evaluation, so they record no dependency: this is a snapshot, not a
// binding.
//
// A companion with no Path is inert rather than an error, the same
// latitude Timer gives a page being built.
func (c *Companion) Start(post func(func())) func() {
	if c.Path == "" || post == nil {
		return func() {}
	}
	argv := make([]string, 0, len(c.Args))
	for _, a := range c.Args {
		argv = append(argv, a.Get())
	}
	env := c.environ()

	out, closeOut, err := c.output()
	if err != nil {
		c.fail(post, gooey.PhaseStart, err)
		return func() {}
	}

	cmd := exec.Command(c.Path, argv...)
	cmd.Dir = c.Dir
	cmd.Env = env
	opts := []gooey.CmdOption{}
	if out != nil {
		opts = append(opts, gooey.CompanionOutput(out))
	}
	if c.KillDelay > 0 {
		opts = append(opts, gooey.CompanionKillDelay(c.KillDelay))
	}
	svc := gooey.CompanionCmd(c.Name, cmd, opts...)

	ctx, cancel := context.WithCancel(context.Background())
	if err := svc.Start(ctx); err != nil {
		cancel()
		closeOut()
		c.fail(post, gooey.PhaseStart, err)
		return func() {}
	}
	c.clear(post)

	// stopping is written by the stop func and read by the posted exit
	// report. Both run on the UI goroutine, so the dispatcher orders
	// them and no lock is involved.
	stopping := false
	done := make(chan struct{})
	go func() {
		err := svc.Wait()
		close(done)
		post(func() {
			if stopping {
				return // we asked for this one
			}
			c.report(gooey.PhaseRun, err)
		})
	}()

	return func() {
		stopping = true
		cancel()
		timeout := c.StopTimeout
		if timeout <= 0 {
			timeout = DefaultCompanionStopTimeout
		}
		t := time.NewTimer(timeout)
		defer t.Stop()
		select {
		case <-done:
		case <-t.C:
			c.leaked = true
		}
		closeOut()
	}
}

// environ builds the child's environment: the app's plus the declared
// entries, or only the declared entries under CleanEnv. Later entries
// win, which is how os/exec reads a duplicated name, so a <Var> named
// like an inherited variable overrides it.
func (c *Companion) environ() []string {
	if len(c.Env) == 0 && !c.CleanEnv {
		return nil // nil Env means "inherit", exec.Cmd's own default
	}
	// Non-nil from here, and that is the whole trick: os/exec reads a NIL
	// Env as "use the current process's environment", so a clean
	// environment has to be an empty slice rather than no slice. Under
	// CleanEnv the difference is the entire feature.
	env := []string{}
	if !c.CleanEnv {
		env = append(env, os.Environ()...)
	}
	for _, v := range c.Env {
		val := ""
		if v.Value != nil {
			val = v.Value.Get()
		}
		env = append(env, v.Name+"="+val)
	}
	return env
}

// output opens the log file, or reports that there is none — in which
// case CompanionCmd's own default (os.DevNull) applies. The returned
// closer is always safe to call.
func (c *Companion) output() (*os.File, func(), error) {
	if c.Log == "" {
		return nil, func() {}, nil
	}
	f, err := os.OpenFile(c.Log, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, func() {}, fmt.Errorf("cannot open the companion log %s: %w", filepath.Clean(c.Log), err)
	}
	return f, func() { f.Close() }, nil
}

// fail reports a start failure. It posts rather than setting directly
// because Start runs inside the Composer's tree walk, and a Set (or an
// Exited that quits the app) belongs at a defined point in the loop
// rather than mid-walk.
func (c *Companion) fail(post func(func()), phase gooey.CompanionPhase, err error) {
	post(func() { c.report(phase, err) })
}

func (c *Companion) report(phase gooey.CompanionPhase, err error) {
	ce := &gooey.CompanionError{Name: c.Name, Phase: phase, Err: err}
	c.err = ce
	if c.Error != nil {
		c.Error.Set(ce.Error())
	}
	if gooey.CanExecute(c.Exited) {
		c.Exited.Run()
	}
}

// clear resets a bound Error after a successful start, so a page that
// showed the previous failure stops showing it.
func (c *Companion) clear(post func(func())) {
	if c.Error == nil {
		return
	}
	post(func() {
		c.err = nil
		c.Error.Set("")
	})
}

package gooey

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/term"
)

// These tests run a REAL App with REAL companions over a pty. What they
// assert is ordering and lifetime — a companion is up before the screen
// exists and down after it is given back — which is the whole reason the
// mechanism is in the framework rather than in each app's main.

// eventLog records what happened in what order, from any goroutine.
type eventLog struct {
	mu sync.Mutex
	ev []string
}

func (l *eventLog) add(s string) {
	l.mu.Lock()
	l.ev = append(l.ev, s)
	l.mu.Unlock()
}

func (l *eventLog) all() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.ev...)
}

// before reports whether a happened before b, and that both happened.
func (l *eventLog) before(a, b string) bool {
	ev := l.all()
	ai, bi := -1, -1
	for i, e := range ev {
		if e == a && ai < 0 {
			ai = i
		}
		if e == b && bi < 0 {
			bi = i
		}
	}
	return ai >= 0 && bi >= 0 && ai < bi
}

func (l *eventLog) has(s string) bool {
	for _, e := range l.all() {
		if e == s {
			return true
		}
	}
	return false
}

// probe is a Companion whose whole life the test steers: it can refuse
// to start, run whatever the test wants, and it logs both ends.
type probe struct {
	name     string
	startErr error
	run      func(ctx context.Context) error
	log      *eventLog
	done     chan error
}

func (p *probe) Name() string { return p.name }

func (p *probe) Start(ctx context.Context) error {
	p.log.add("start " + p.name)
	if p.startErr != nil {
		return p.startErr
	}
	p.done = make(chan error, 1)
	go func() {
		var err error
		if p.run != nil {
			err = p.run(ctx)
		} else {
			<-ctx.Done() // the well-behaved default: stop when asked
		}
		p.log.add("exit " + p.name)
		p.done <- err
	}()
	return nil
}

func (p *probe) Wait() error { return <-p.done }

// recordingContent notes when the tree was built, so a test can place
// that moment against the companions' starts.
type recordingContent struct {
	log *eventLog
	w   Component
}

func (c recordingContent) Build() (Component, error) { c.log.add("build"); return c.w, nil }
func (c recordingContent) Watch(func()) func()       { return func() {} }

func companionApp(t *testing.T, log *eventLog, opts ...Option) (*App, *testTTY) {
	t.Helper()
	tty := newTestTTY(t)
	open := func() (*term.Screen, error) {
		log.add("open terminal")
		return tty.open()
	}
	content := recordingContent{log: log, w: &label{text: prop.NewSource("hi")}}
	opts = append([]Option{WithTerminal(open)}, opts...)
	return NewApp(content, opts...), tty
}

// A companion is running before the tree is built and before anything
// touches the terminal. Both halves matter: the demo this was built for
// constructs its first screen from a query the companion answers, and a
// service that fails to start must be able to say so on a cooked screen.
func TestCompanionsStartBeforeTheTreeAndTheTerminal(t *testing.T) {
	log := &eventLog{}
	first := &probe{name: "first", log: log}
	second := &probe{name: "second", log: log}
	app, tty := companionApp(t, log, WithCompanions(first, second), WithCompanionGrace(0))

	start(t, app)
	tty.waitForFrame(t)

	for _, pair := range [][2]string{
		{"start first", "start second"},
		{"start second", "build"},
		{"build", "open terminal"},
	} {
		if !log.before(pair[0], pair[1]) {
			t.Errorf("%q did not happen before %q; log was %v", pair[0], pair[1], log.all())
		}
	}
}

// A companion that cannot start aborts the run before the terminal is
// opened at all — no alt screen, no raw mode, an error on stderr like
// any other program that could not start.
func TestCompanionFailingToStartAbortsBeforeRawMode(t *testing.T) {
	log := &eventLog{}
	boom := errors.New("port already in use")
	ok := &probe{name: "ok", log: log}
	bad := &probe{name: "bad", log: log, startErr: boom}
	never := &probe{name: "never", log: log}
	app, tty := companionApp(t, log, WithCompanions(ok, bad, never))

	err := <-start(t, app)

	var ce *CompanionError
	if !errors.As(err, &ce) {
		t.Fatalf("Run returned %v, want a *CompanionError", err)
	}
	if ce.Name != "bad" || ce.Phase != PhaseStart || !errors.Is(err, boom) {
		t.Errorf("got %+v, want the start failure of %q wrapping %v", ce, "bad", boom)
	}
	if tty.openCount() != 0 {
		t.Errorf("the terminal was opened %d times; a companion that never started must not cost a screen", tty.openCount())
	}
	if log.has("build") {
		t.Error("the tree was built even though a companion failed to start")
	}
	if log.has("start never") {
		t.Error("a companion after the failing one was started anyway")
	}
	// The one that DID start is stopped on the way out. A half-started
	// app that leaves a service running is the bug this whole thing is
	// about.
	if !log.has("exit ok") {
		t.Errorf("the already-started companion was left running; log was %v", log.all())
	}
}

// A companion that dies while the app is running takes the app with it,
// and Run says which one and why. A UI wired to a service that is gone
// is worse than no UI.
func TestCompanionExitingMidRunQuitsTheApp(t *testing.T) {
	log := &eventLog{}
	boom := errors.New("connection to the server lost")
	die := make(chan struct{})
	c := &probe{name: "worker", log: log, run: func(context.Context) error {
		<-die
		return boom
	}}
	app, tty := companionApp(t, log, WithCompanions(c))

	done := start(t, app)
	tty.waitForFrame(t)
	close(die)

	select {
	case err := <-done:
		var ce *CompanionError
		if !errors.As(err, &ce) {
			t.Fatalf("Run returned %v, want a *CompanionError", err)
		}
		if ce.Name != "worker" || ce.Phase != PhaseRun || !errors.Is(err, boom) {
			t.Errorf("got %+v, want the mid-run failure of %q wrapping %v", ce, "worker", boom)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("a dead companion did not end the run loop")
	}
	// Whatever else happened, the terminal came back.
	if !tty.waitForBytes(t, "\x1b[?1049l") {
		t.Error("the terminal was not restored after a companion died")
	}
}

// Exiting zero mid-run is a failure too. A companion is a service, and a
// service that decides it is finished while its app is still on screen
// has failed at being a service — silently, which is worse.
func TestCompanionCleanExitIsStillAFailure(t *testing.T) {
	log := &eventLog{}
	stop := make(chan struct{})
	c := &probe{name: "worker", log: log, run: func(context.Context) error {
		<-stop
		return nil
	}}
	app, tty := companionApp(t, log, WithCompanions(c))

	done := start(t, app)
	tty.waitForFrame(t)
	close(stop)

	select {
	case err := <-done:
		var ce *CompanionError
		if !errors.As(err, &ce) {
			t.Fatalf("Run returned %v, want a *CompanionError", err)
		}
		if ce.Phase != PhaseRun || ce.Err != nil {
			t.Errorf("got %+v, want a run-phase error with no cause", ce)
		}
		if !strings.Contains(err.Error(), "stopped while the app was running") {
			t.Errorf("error reads %q, which does not explain a clean exit", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("a companion that returned nil did not end the run loop")
	}
}

// The grace window is what turns "started, then died two milliseconds
// later" into a start failure. Without it that is an ordinary mid-run
// exit, and the user sees an alt screen appear and vanish instead of a
// message.
func TestCompanionGraceCatchesAnImmediateExit(t *testing.T) {
	log := &eventLog{}
	boom := errors.New("no config file")
	c := &probe{name: "worker", log: log, run: func(context.Context) error { return boom }}
	app, tty := companionApp(t, log, WithCompanions(c), WithCompanionGrace(500*time.Millisecond))

	began := time.Now()
	err := <-start(t, app)

	var ce *CompanionError
	if !errors.As(err, &ce) || ce.Phase != PhaseStart || !errors.Is(err, boom) {
		t.Fatalf("Run returned %v, want a start-phase companion error wrapping %v", err, boom)
	}
	if tty.openCount() != 0 {
		t.Error("a companion that died in the grace window still cost a screen")
	}
	// It ends on the failure, not on the timer.
	if d := time.Since(began); d > 400*time.Millisecond {
		t.Errorf("the grace window took %v; it should end early on a failure", d)
	}
}

// Teardown cancels the companion context and WAITS. "Stopped" means the
// goroutine is gone by the time Run returns, not that somebody asked.
func TestTeardownCancelsAndWaitsForCompanions(t *testing.T) {
	log := &eventLog{}
	slow := &probe{name: "slow", log: log, run: func(ctx context.Context) error {
		<-ctx.Done()
		time.Sleep(150 * time.Millisecond) // a real service closing connections
		return ctx.Err()
	}}
	fast := &probe{name: "fast", log: log}
	app, tty := companionApp(t, log, WithCompanions(slow, fast))

	done := start(t, app)
	tty.waitForFrame(t)
	tty.send("\x03")

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned %v, want nil: a quit key is not a companion failure", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the run loop did not stop")
	}
	// Run has returned. Both companions must already be finished — this
	// is the guarantee, and reading the log after the fact is how it is
	// checked without a race.
	for _, name := range []string{"slow", "fast"} {
		if !log.has("exit " + name) {
			t.Errorf("companion %q was still running when Run returned; log was %v", name, log.all())
		}
	}
	if app.CompanionLeaked() {
		t.Error("teardown gave up waiting for a companion that was cooperating")
	}
}

// A companion that ignores its cancelled context does not hang the app
// forever: the wait is bounded and the tripwire records that it fired.
func TestCompanionIgnoringItsContextTripsTheWire(t *testing.T) {
	log := &eventLog{}
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	stubborn := &probe{name: "stubborn", log: log, run: func(context.Context) error {
		<-release // never returns on its own
		return nil
	}}
	app, tty := companionApp(t, log,
		WithCompanions(stubborn), WithCompanionStopTimeout(100*time.Millisecond))

	done := start(t, app)
	tty.waitForFrame(t)
	tty.send("\x03")

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("a companion that ignores its context hung the app")
	}
	if !app.CompanionLeaked() {
		t.Error("the tripwire did not fire for a companion that outlived the wait")
	}
}

// Companions are background services, not part of the UI. Handing the
// terminal to a child process has nothing to do with them.
func TestCompanionsKeepRunningAcrossSuspend(t *testing.T) {
	log := &eventLog{}
	c := &probe{name: "worker", log: log}
	app, tty := companionApp(t, log, WithCompanions(c))

	start(t, app)
	tty.waitForFrame(t)

	suspended := make(chan error, 1)
	app.Post(func() {
		suspended <- app.Suspend(func() error { return nil })
	})
	select {
	case err := <-suspended:
		if err != nil {
			t.Fatalf("Suspend returned %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Suspend never returned")
	}
	if log.has("exit worker") {
		t.Error("the companion was stopped by a terminal hand-off it has nothing to do with")
	}
}

// --- CompanionCmd ---

// A missing binary is a start failure like any other: reported before
// the screen, naming the thing that is not installed.
func TestCompanionCmdMissingBinaryFailsBeforeRawMode(t *testing.T) {
	log := &eventLog{}
	cmd := exec.Command("gooey-no-such-binary-exists", "--serve")
	app, tty := companionApp(t, log, WithCompanions(CompanionCmd("sidecar", cmd)))

	err := <-start(t, app)

	var ce *CompanionError
	if !errors.As(err, &ce) || ce.Phase != PhaseStart || ce.Name != "sidecar" {
		t.Fatalf("Run returned %v, want the start failure of the sidecar", err)
	}
	if tty.openCount() != 0 {
		t.Error("a command that could not be executed still cost a screen")
	}
}

// The discriminating test for process groups: a child that BACKGROUNDS a
// grandchild. Signalling the child alone kills the shell and orphans the
// sleeper, which survives holding whatever it holds; signalling the
// GROUP takes both.
func TestCompanionCmdStopsTheWholeProcessGroup(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh")
	}
	pidFile := filepath.Join(t.TempDir(), "grandchild.pid")
	log := &eventLog{}
	cmd := exec.Command("sh", "-c", "sleep 300 & echo $! > "+pidFile+"; wait")
	sidecar := CompanionCmd("sidecar", cmd, CompanionKillDelay(500*time.Millisecond))
	app, tty := companionApp(t, log, WithCompanions(sidecar))

	done := start(t, app)
	tty.waitForFrame(t)

	grandchild := waitForPID(t, pidFile)
	if !alive(grandchild) {
		t.Fatalf("the grandchild (pid %d) was never running", grandchild)
	}
	child := cmd.Process.Pid

	tty.send("\x03")
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the run loop did not stop")
	}

	// Run has returned, so teardown already waited for the companion.
	// Reaping is the kernel's business and can lag a moment.
	for _, p := range []struct {
		pid  int
		what string
	}{{child, "child shell"}, {grandchild, "backgrounded grandchild"}} {
		if !waitForDeath(p.pid) {
			t.Errorf("the %s (pid %d) outlived the app", p.what, p.pid)
		}
	}
	if app.CompanionLeaked() {
		t.Error("the tripwire fired on a child that was killed properly")
	}
}

func waitForPID(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		b, err := os.ReadFile(path)
		if err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil && pid > 0 {
				return pid
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("the child never wrote its grandchild's pid to %s", path)
	return 0
}

// alive asks the kernel whether a pid exists, which is what signal 0 is
// for. A grandchild is not our child, so it is never a zombie to us.
func alive(pid int) bool { return syscall.Kill(pid, 0) == nil }

func waitForDeath(pid int) bool {
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if !alive(pid) {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

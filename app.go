package gooey

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/WonderForgeLabs/gooey/graphics"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/term"
)

// App is the framework-owned run loop: the terminal's lifetime, the
// input decoder, the dispatcher, frame scheduling, hot-reload swaps and
// the console signal story, in one object.
//
// It exists because every app had been hand-writing the same sixty lines
// — open the screen, raw mode, mouse, dispatcher, decoder goroutine,
// select loop, deferred restore — and each copy had its own subtly
// different set of bugs. The parts that were merely tedious (setup) and
// the parts that were genuinely hard (signals, suspend, resize, dying
// with the terminal intact) are the same parts, so they move together.
//
// The loop is deliberately not extensible by adding select cases: an app
// cannot hand the framework another channel to wait on, because a
// dynamic select needs reflection. It does not need to. Everything
// asynchronous — timers, signals, watchers, network handlers — reaches
// the UI through the Dispatcher, which is the confinement rule anyway
// (see dispatch.go). Every hook here runs on the UI goroutine, where
// touching properties is legal.
//
// Typical shape:
//
//	var app *gooey.App
//	ctx := &markup.Context{Values: map[string]any{
//	    "Quit": gooey.Command(func() { app.Quit() }),
//	}}
//	app = gooey.NewApp(markup.Page(os.DirFS("."), "page.gooey", ctx))
//	if err := app.Run(context.Background()); err != nil {
//	    gooey.Exit(err)
//	}
type App struct {
	content Content
	opt     options

	screen *term.Screen
	comp   *Composer
	disp   *Dispatcher
	events <-chan input.Event

	cols, rows int
	needsFrame bool
	frames     int
	painted    int

	before   []func()
	after    []func()
	onEvent  []func(input.Event)
	afterEv  []func(input.Event, bool)
	onSwap   []func(Component)
	stops    []func()
	quit     chan struct{}
	quitOnce sync.Once

	sig       signalHandle
	exitSig   os.Signal
	leaked    bool
	suspended bool

	// Companions: services with this app's exact lifetime. See
	// companion.go — every field here is UI-goroutine-only except the
	// channel, which is how the supervisors report in.
	compCtx      context.Context
	compCancel   context.CancelFunc
	compStarted  []Companion
	compExit     chan companionExit
	compDone     int
	compErr      error
	compStopping bool
	compStopped  bool
	compLeaked   bool
}

// Content is where an App's component tree comes from, and the seam hot
// reload lives behind. Build is called on the UI goroutine — at startup
// and again for every reload — so a Build may touch the property graph
// freely, which is exactly what markup loading does when it resolves
// bindings to live handles.
//
// Watch reports only THAT the source changed, never the new tree: the
// rebuild has to happen on the UI goroutine, and a watcher runs on its
// own. (This is a real fix, not a formality — markup.Watch used to build
// the replacement tree on the polling goroutine, resolving bindings
// against properties nobody else was allowed to touch from there.)
type Content interface {
	Build() (Component, error)
	Watch(changed func()) (stop func())
}

// Tree is the Content for an app whose component tree is built in Go and
// never replaced. Nothing about the run loop changes; there is simply
// nothing to reload.
func Tree(w Component) Content { return staticTree{w} }

type staticTree struct{ w Component }

func (s staticTree) Build() (Component, error) { return s.w, nil }
func (s staticTree) Watch(func()) func()       { return func() {} }

type options struct {
	mouse    bool
	probe    bool
	caps     *term.Caps
	gfx      graphics.Encoder
	gfxSet   bool
	quitKeys []input.KeyEvent
	shutdown func(context.Context) error
	shutTO   time.Duration
	onError  func(error)
	eventBuf int
	open     func() (*term.Screen, error)

	companions []Companion
	compGrace  time.Duration
	compStopTO time.Duration
}

// Option configures an App. Options are typed funcs over an unexported
// struct: explicit, no reflection, no string keys.
type Option func(*options)

// WithoutMouse leaves SGR mouse reporting off. Apps that decode input
// with this framework want it on (the default) — hover and click are
// ordinary events — but a program that shells out constantly, or one
// whose terminal mangles motion reports, can decline.
func WithoutMouse() Option { return func(o *options) { o.mouse = false } }

// WithCapabilityProbe runs term.Screen.Detect at startup: a real query
// to the terminal for graphics protocol support and cell pixel size.
//
// It is opt-in because it is a round trip that only graphics apps need,
// and under recording ptys and other half-terminals the answer is a
// timeout. Without it the app still gets a color depth, read from the
// environment, which is what every cell-plane app actually uses.
func WithCapabilityProbe() Option { return func(o *options) { o.probe = true } }

// WithCaps supplies capabilities outright, skipping both the probe and
// the environment ladder. For hosts that already know.
func WithCaps(c term.Caps) Option { return func(o *options) { o.caps = &c } }

// WithGraphics pins the pixel protocol instead of letting capabilities
// choose it. A nil encoder forces the halfblock fallback, where pixel
// content draws itself into the cell plane.
//
// Without it, an app gets a protocol only when capabilities say so —
// which means only under WithCapabilityProbe or WithCaps, since the
// environment ladder can report color depth but never graphics support.
// That default is deliberate: emitting an image protocol at a terminal
// that does not speak it puts garbage on the user's screen, and a probe
// is the only thing that can tell.
func WithGraphics(enc graphics.Encoder) Option {
	return func(o *options) { o.gfx, o.gfxSet = enc, true }
}

// WithQuitKeys replaces the default quit key (ctrl+c) with the given
// set. Pass none to disable the framework quit key entirely and own the
// whole key surface.
//
// Quit keys are checked only AFTER the tree declines the event, like
// directional focus navigation: a component that wants ctrl+c gets it.
func WithQuitKeys(keys ...input.KeyEvent) Option {
	return func(o *options) { o.quitKeys = keys }
}

// WithShutdown registers a graceful-shutdown hook run when SIGINT or
// SIGTERM arrives, bounded by timeout. It runs on the UI goroutine with
// the terminal still up, so it may touch properties; it must not block
// forever, and past the timeout the app exits regardless.
func WithShutdown(fn func(context.Context) error, timeout time.Duration) Option {
	return func(o *options) { o.shutdown, o.shutTO = fn, timeout }
}

// WithErrorHandler receives non-fatal errors — a hot reload that failed
// to parse, a terminal that could not be re-acquired after suspend.
// Without one they are dropped, which is what every demo did by hand.
func WithErrorHandler(fn func(error)) Option { return func(o *options) { o.onError = fn } }

// WithEventBuffer sizes the decoded input channel.
func WithEventBuffer(n int) Option { return func(o *options) { o.eventBuf = n } }

// WithTerminal replaces term.Open as the way this app acquires a
// terminal. It is called once at startup and again after every Suspend
// and every ctrl+z, so it must hand back a FRESH Screen each time —
// teardown closes the one it was given.
//
// The framework's own tests drive a real app over a pty this way, which
// is the point: the run loop, the signal dance and the suspend cycle are
// testable only if the terminal is a parameter rather than /dev/tty.
func WithTerminal(open func() (*term.Screen, error)) Option {
	return func(o *options) { o.open = open }
}

// NewApp creates an app that runs content. It touches no terminal and
// starts no goroutine until Run.
func NewApp(content Content, opts ...Option) *App {
	a := &App{
		content: content,
		opt: options{
			mouse:      true,
			quitKeys:   []input.KeyEvent{{Key: input.KeyRune, Rune: 'c', Mods: input.ModCtrl}},
			eventBuf:   64,
			compGrace:  defaultCompanionGrace,
			compStopTO: defaultCompanionStopTimeout,
		},
		disp: NewDispatcher(),
		quit: make(chan struct{}),
	}
	for _, o := range opts {
		o(&a.opt)
	}
	return a
}

// Dispatcher is the marshaling seam for this app's background work.
func (a *App) Dispatcher() *Dispatcher { return a.disp }

// Post queues fn to run on the UI goroutine. Safe from any goroutine —
// the one legal way for another goroutine to reach the property graph.
func (a *App) Post(fn func()) { a.disp.Post(fn) }

// Composer is the live composition. It is replaced by every hot reload,
// so read it when you need it rather than holding it.
func (a *App) Composer() *Composer { return a.comp }

// Screen is the terminal this app currently holds. Nil while suspended.
func (a *App) Screen() *term.Screen { return a.screen }

// Size reports the terminal size the composition is laid out at.
func (a *App) Size() (cols, rows int) { return a.cols, a.rows }

// Frames counts frames flushed since Run started.
func (a *App) Frames() int { return a.frames }

// PaintedLastFrame is the damage count of the most recent frame: how
// many components actually repainted, not how many exist.
func (a *App) PaintedLastFrame() int { return a.painted }

// FlushBytes is what the most recent frame cost on the wire. It is the
// other half of the damage number: PaintedLastFrame says how little was
// recomposed, this says how little was sent.
func (a *App) FlushBytes() int {
	if a.comp == nil {
		return 0
	}
	return a.comp.FlushBytes()
}

// Invalidate asks for a frame without any property having changed. Rare
// — the property graph normally schedules frames by itself — but a
// resize or a resumed terminal needs one.
func (a *App) Invalidate() { a.needsFrame = true }

// BeforeFrame registers a hook run immediately before each frame is
// composed, in registration order.
//
// This is where "stats about the previous frame" belong. Setting a
// property from here folds its repaint into the frame about to happen
// instead of dirtying the tree again afterwards, which would schedule a
// second frame and never settle.
func (a *App) BeforeFrame(fn func()) { a.before = append(a.before, fn) }

// AfterFrame registers a hook run immediately after each frame has been
// composed and flushed, in registration order. It is the observation
// point for "what did this frame change": Composer.Damage and
// PaintedLastFrame describe exactly the frame that just went out — the
// seam a control-plane session collects its frame deltas from.
//
// It runs on the UI goroutine, so it may read properties freely (reads
// here are outside any evaluation and record nothing). Setting a
// property from an AfterFrame hook schedules ANOTHER frame — do that
// unconditionally and the app never settles; stats about a frame belong
// in BeforeFrame, which folds them into the frame about to happen.
//
// Register before Run, or from the UI goroutine (a Post): the hook list
// is read by the frame path without a lock, like every other App hook.
func (a *App) AfterFrame(fn func()) { a.after = append(a.after, fn) }

// AfterEvent registers an observer run after an input event has been
// routed, with whether the tree consumed it. Where OnEvent sees the
// stream before routing, AfterEvent sees the outcome — the seam an
// input-echoing session needs, since "consumed" does not exist until
// dispatch has happened. Like OnEvent it cannot consume anything.
//
// Register before Run, or from the UI goroutine (a Post).
func (a *App) AfterEvent(fn func(ev input.Event, consumed bool)) {
	a.afterEv = append(a.afterEv, fn)
}

// Done returns a channel closed when the app has quit — by Quit, a quit
// key, or a signal. Safe from any goroutine; a control-plane session
// selects on it to tell its clients the app is closing.
func (a *App) Done() <-chan struct{} { return a.quit }

// OnEvent registers an OBSERVER of the input stream, run for every
// decoded event before it is routed. It cannot consume anything — the
// return value would be a second, invisible input path competing with
// the tree, and handling belongs in components and KeyBindings.
//
// It is for the things that are about the stream rather than about any
// component: counting events, logging them, driving an idle timer.
func (a *App) OnEvent(fn func(input.Event)) { a.onEvent = append(a.onEvent, fn) }

// OnSwap registers a hook run after the tree is attached — once at
// startup and again after every hot reload — with the new root. Anything
// resolved out of the tree by name has to be re-resolved here: a reload
// builds new components, and the old handles point at a composition that is
// no longer on screen.
func (a *App) OnSwap(fn func(Component)) { a.onSwap = append(a.onSwap, fn) }

// Every runs fn on the UI goroutine on an interval until Run returns.
// The ticker itself lives on its own goroutine and only ever Posts, so
// fn is ordinary UI code.
//
// For a tick that belongs to the UI, declare a <Timer> in the tree
// instead — its lifetime is then the composition's, and a hot reload
// replaces it. Every is for the app's own clock: a data generator, a
// poll, anything that must outlive the tree on screen.
func (a *App) Every(d time.Duration, fn func()) (stop func()) {
	if d <= 0 || fn == nil {
		return func() {}
	}
	done := make(chan struct{})
	go func() {
		t := time.NewTicker(d)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				a.disp.Post(fn)
			}
		}
	}()
	var once sync.Once
	s := func() { once.Do(func() { close(done) }) }
	a.stops = append(a.stops, s)
	return s
}

// Quit ends the run loop. Safe from any goroutine and idempotent, so a
// Command, a signal handler and a shutdown hook may all call it.
func (a *App) Quit() { a.quitOnce.Do(func() { close(a.quit) }) }

func (a *App) stopped() bool {
	select {
	case <-a.quit:
		return true
	default:
		return false
	}
}

func (a *App) fail(err error) {
	if err != nil && a.opt.onError != nil {
		a.opt.onError(err)
	}
}

// Run owns the terminal for the duration and returns when the app quits,
// ctx is cancelled, or a signal ends it. An App runs once: Quit is
// permanent, so a second Run would return immediately.
//
// It returns *SignalError when SIGINT or SIGTERM ended the run — the
// caller decides the exit code from it (gooey.Exit implements the usual
// 128+n convention). Quit and ctx cancellation return nil.
//
// A panic anywhere under Run restores the terminal FIRST and then
// re-panics with the original value, so the stack trace prints onto a
// cooked screen instead of scrolling sideways through a raw-mode alt
// buffer that nobody can scroll back through.
//
// Companions run for exactly this call. They are started first — before
// the tree is built and before the terminal is touched, so a service
// that cannot start reports it on a cooked screen and so a Build that
// talks to one finds it up — and stopped last, after the terminal has
// been handed back. A companion that dies while the app is running quits
// the app, and Run returns a *CompanionError saying which one.
func (a *App) Run(ctx context.Context) error {
	defer func() {
		if r := recover(); r != nil {
			a.teardown()
			panic(r)
		}
	}()

	// The defer order from here down is the teardown order reversed, and
	// it is load-bearing: stopCompanions is registered BEFORE teardown so
	// it runs AFTER it. Services are asked to stop once the terminal is
	// cooked again, never while the UI is frozen mid-frame waiting on
	// them.
	if err := a.startCompanions(ctx); err != nil {
		a.stopCompanions()
		return err
	}
	defer a.stopCompanions()

	root, err := a.content.Build()
	if err != nil {
		return err
	}
	if err := a.acquire(); err != nil {
		return err
	}
	defer a.teardown()

	a.attach(root)
	a.stops = append(a.stops, a.content.Watch(func() { a.disp.Post(a.reload) }))
	a.startSignals()

	for !a.stopped() {
		select {
		case <-ctx.Done():
			return a.exitErr()
		default:
		}
		if a.needsFrame {
			a.frame()
		}
		select {
		case <-a.quit:
		case <-ctx.Done():
		case <-a.disp.Wake():
			a.disp.Drain()
		case ev := <-a.events:
			a.handle(ev)
		}
	}
	return a.exitErr()
}

// gracefulExit runs the app's shutdown hook, bounded, and ends the loop.
// The terminal is still up while the hook runs, so it may set properties
// and even paint a farewell; what it may not do is outlast its timeout,
// because whoever sent the signal has already stated their intent.
//
// The signal is recorded so Run can report it: a program killed by a
// signal should not exit 0, and applying the exit code is the caller's
// job (see SignalError and Exit).
func (a *App) gracefulExit(sig os.Signal) {
	a.exitSig = sig
	if fn := a.opt.shutdown; fn != nil {
		ctx := context.Background()
		if a.opt.shutTO > 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, a.opt.shutTO)
			defer cancel()
		}
		done := make(chan error, 1)
		go func() { done <- fn(ctx) }()
		select {
		case err := <-done:
			a.fail(err)
		case <-ctx.Done():
			a.fail(ctx.Err())
		}
	}
	a.Quit()
}

// exitErr is why the loop ended. A dead companion outranks a signal:
// where both happened, the signal is usually the shell reacting to the
// same failure, and the companion is the fact that explains it.
func (a *App) exitErr() error {
	if a.compErr != nil {
		return a.compErr
	}
	if a.exitSig != nil {
		return &SignalError{Signal: a.exitSig}
	}
	return nil
}

// frame is the whole scheduling contract, and the ordering in it is
// load-bearing: hooks first (so what they set paints in THIS frame),
// then compose, then flush, then consume the request. Clearing
// needsFrame last is what makes an invalidation raised during the frame
// schedule another one instead of being swallowed.
func (a *App) frame() {
	a.frames++
	for _, fn := range a.before {
		fn()
	}
	_, a.painted = a.comp.Frame()
	if a.screen != nil {
		a.fail(a.comp.Flush(a.screen.File()))
	}
	a.needsFrame = false
	for _, fn := range a.after {
		fn()
	}
}

// handle routes one event. The tree gets first refusal; the app-level
// quit key is checked only on what bubbles out, the same rule that makes
// unconsumed arrows move focus. A component that binds ctrl+c keeps it.
func (a *App) handle(ev input.Event) {
	for _, fn := range a.onEvent {
		fn(ev)
	}
	consumed := a.comp.Handle(ev)
	if !consumed && ev.IsKey() {
		for _, k := range a.opt.quitKeys {
			if ev.Key == k {
				a.Quit()
				break
			}
		}
	}
	for _, fn := range a.afterEv {
		fn(ev, consumed)
	}
}

// attach makes w the live composition: a new Composer over the same
// terminal, sized as we are now, with this app's caps and scheduler
// hook. The OUTGOING composer is closed first — a replaced tree's timers
// must not keep ticking against a viewmodel nobody is showing.
func (a *App) attach(w Component) {
	if a.comp != nil {
		a.comp.Close()
	}
	a.comp = NewComposer(w, a.cols, a.rows)
	a.comp.SetCaps(a.caps())
	if a.opt.gfxSet {
		a.comp.SetGraphics(a.opt.gfx)
	}
	a.comp.OnInvalidate(func() { a.needsFrame = true })
	a.comp.Start(a.disp)
	a.needsFrame = true
	for _, fn := range a.onSwap {
		fn(w)
	}
}

// Swap replaces the live composition with root, exactly as a hot reload
// does: the outgoing Composer is closed (its timers stop), a new one is
// built and started, and the OnSwap hooks run with the new tree.
//
// It is the seam for a tree that arrives from somewhere other than
// Content — a control plane pushing markup, an automation client editing
// the running app. Callers that BUILD the tree (markup.Build resolves
// bindings, which touches the property graph) must do that on the UI
// goroutine too, so the whole build-then-swap belongs inside one Post.
//
// Content is not replaced, so the next reload still rebuilds from the
// original source and discards what was swapped in.
func (a *App) Swap(root Component) {
	if root == nil {
		return
	}
	a.attach(root)
}

// reload rebuilds the tree from Content on the UI goroutine. A build
// error leaves the running composition alone — a markup file is broken
// for the second between two saves, and dropping the UI for it would
// make hot reload unusable.
func (a *App) reload() {
	w, err := a.content.Build()
	if err != nil {
		a.fail(err)
		return
	}
	a.attach(w)
}

func (a *App) caps() term.Caps {
	if a.opt.caps != nil {
		c := *a.opt.caps
		c.Cols, c.Rows = a.cols, a.rows
		return c
	}
	return term.Caps{Cols: a.cols, Rows: a.rows, Color: term.DetectColorDepth()}
}

// acquire takes the terminal: open, size, raw, mouse, decoder. It is the
// half of the lifecycle that Suspend and SIGTSTP replay, so it lives in
// one place and startup is just its first call.
func (a *App) acquire() error {
	open := a.opt.open
	if open == nil {
		open = term.Open
	}
	s, err := open()
	if err != nil {
		return fmt.Errorf("gooey: no terminal: %w", err)
	}
	cols, rows := s.Size()
	if a.opt.probe && a.opt.caps == nil {
		if c, err := s.Detect(); err == nil {
			a.opt.caps = &c
			cols, rows = c.Cols, c.Rows
		}
	}
	if err := s.Raw(); err != nil {
		s.Restore()
		return err
	}
	if a.opt.mouse {
		s.EnableMouse()
	}
	a.screen = s
	a.events = s.Events(a.opt.eventBuf)
	a.resized(cols, rows)
	// The screen we just took is not the screen we last flushed to. Raw
	// enters the alternate screen, which comes back BLANK — after a
	// suspend, after ctrl+z, after a child process had the terminal. The
	// retained buffer is still right and no component needs to repaint;
	// what is wrong is the flush's belief about what the terminal shows,
	// and that is exactly what Invalidate corrects.
	if a.comp != nil {
		a.comp.Invalidate()
	}
	a.needsFrame = true
	return nil
}

// release hands the terminal back: full restore, decoder joined. After
// it returns, nothing of ours is reading the tty — that is the invariant
// term.Screen.Restore now guarantees, and the reason a child process can
// safely be given this terminal.
func (a *App) release() {
	if a.screen == nil {
		return
	}
	a.screen.Restore()
	if a.screen.DecoderLeaked() {
		a.leaked = true
	}
	a.screen, a.events = nil, nil
}

// resized re-targets the composition at a new size.
func (a *App) resized(cols, rows int) {
	if cols == a.cols && rows == a.rows {
		return
	}
	a.cols, a.rows = cols, rows
	if a.comp != nil {
		a.comp.Resize(cols, rows)
		a.comp.SetCaps(a.caps())
		a.needsFrame = true
	}
}

// teardown gives back everything the run loop took EXCEPT the
// companions: Run stops those after this returns, so a service shutting
// down slowly does it on a restored terminal (see Run's defer order).
func (a *App) teardown() {
	a.stopSignals()
	for _, stop := range a.stops {
		stop()
	}
	a.stops = nil
	if a.comp != nil {
		a.comp.Close()
	}
	a.release()
}

// Suspend gives the terminal to fn and takes it back afterwards.
//
// This is the terminal hand-off, and it is only correct because teardown
// joins the input decoder: a reader left parked on the tty would eat the
// child process's keystrokes, and every suspend would add another one.
// The composition survives untouched — Flush writes the whole buffer, so
// re-entering a blank alternate screen repaints correctly from the
// retained buffer with nothing dirty — and a size change while away is
// picked up on the way back in.
//
// While fn runs, interrupts are SHIELDED: the tty driver signals the
// whole foreground process group, so the ctrl+c a user aims at the child
// arrives here too, and acting on it would kill the host along with the
// thing it launched. The child gets its own SIGINT either way — this
// only stops ours from being fatal.
//
// Companions keep running throughout. They are background services, not
// part of the UI: the child fn launches owns the terminal for a while,
// and a companion never did.
//
// fn's error is returned as-is. An error re-acquiring the terminal takes
// precedence, because at that point there is no UI left to report into.
func (a *App) Suspend(fn func() error) error {
	a.release()
	a.suspended = true
	err := fn()
	a.suspended = false
	if aerr := a.acquire(); aerr != nil {
		return aerr
	}
	return err
}

// DecoderLeaked reports whether any terminal teardown in this app's life
// timed out waiting for the input decoder to die. It should always be
// false; it is the tripwire for the bug this lifecycle was built to
// eliminate.
func (a *App) DecoderLeaked() bool { return a.leaked }

// SignalError reports that a signal ended the run.
type SignalError struct{ Signal os.Signal }

func (e *SignalError) Error() string { return "interrupted by " + e.Signal.String() }

// ExitCode is the shell convention for death by signal: 128 plus the
// signal number, so ctrl+c is 130 and SIGTERM is 143.
func (e *SignalError) ExitCode() int { return 128 + signalNumber(e.Signal) }

// Exit ends the process for an error returned by Run: it prints real
// errors to stderr and exits 1, and exits quietly with 128+n for a
// signal, which is what a shell expects from a program that was
// interrupted rather than one that failed.
func Exit(err error) {
	if err == nil {
		os.Exit(0)
	}
	if se, ok := err.(*SignalError); ok {
		os.Exit(se.ExitCode())
	}
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

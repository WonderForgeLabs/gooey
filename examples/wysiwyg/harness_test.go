package main

import (
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/markup"
)

// testApp hand-runs the app loop rather than using gooey.App, the same
// way grpc's and mcp's own tests do — and for the same reason plus one
// more. The reason they share: everything below the Host interface is
// UI-goroutine state, so a control-plane call that touched it from a
// transport goroutine is a -race failure, and a hand-run loop is what
// makes that boundary the thing under test.
//
// The extra reason here: gooey.App wants a terminal. `go test` has none,
// so an App-based target never reaches the point of listening.
//
// The wire is not simulated. Calls travel over a real loopback listener
// and the generated client.
type testApp struct {
	ctx  *markup.Context
	disp *gooey.Dispatcher

	comp       *gooey.Composer
	cols, rows int
	needsFrame bool
	after      []func()
	onSwap     []func(gooey.Component)
	afterEv    []func(input.Event, bool)

	quit chan struct{}
	done chan struct{}
}

func newTestApp(t *testing.T, src string, values map[string]any) *testApp {
	t.Helper()
	a := &testApp{
		disp: gooey.NewDispatcher(),
		cols: 80, rows: 24,
		quit: make(chan struct{}),
		done: make(chan struct{}),
	}
	a.ctx = &markup.Context{Values: values, Dispatcher: a.disp}
	root, err := markup.Build([]byte(src), a.ctx)
	if err != nil {
		t.Fatalf("build markup: %v", err)
	}
	a.attach(root)
	go a.run()
	t.Cleanup(func() {
		close(a.quit)
		<-a.done
	})
	return a
}

func (a *testApp) attach(root gooey.Component) {
	if a.comp != nil {
		a.comp.Close()
	}
	a.comp = gooey.NewComposer(root, a.cols, a.rows)
	a.comp.OnInvalidate(func() { a.needsFrame = true })
	a.comp.Start(a.disp)
	a.needsFrame = true
	for _, fn := range a.onSwap {
		fn(root)
	}
}

func (a *testApp) run() {
	defer close(a.done)
	for {
		if a.needsFrame {
			a.comp.Frame()
			a.needsFrame = false
			for _, fn := range a.after {
				fn()
			}
		}
		select {
		case <-a.quit:
			a.comp.Close()
			return
		case <-a.disp.Wake():
			a.disp.Drain()
		}
	}
}

// Host
func (a *testApp) Post(fn func())            { a.disp.Post(fn) }
func (a *testApp) Composer() *gooey.Composer { return a.comp }
func (a *testApp) Swap(root gooey.Component) { a.attach(root) }

// SessionHost
func (a *testApp) AfterFrame(fn func())                  { a.after = append(a.after, fn) }
func (a *testApp) OnSwap(fn func(gooey.Component))       { a.onSwap = append(a.onSwap, fn) }
func (a *testApp) AfterEvent(fn func(input.Event, bool)) { a.afterEv = append(a.afterEv, fn) }
func (a *testApp) Done() <-chan struct{}                 { return a.quit }

package temporalhandlers

import (
	"context"
	"errors"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/prop"
	"go.temporal.io/sdk/client"
)

// fakeStarter records what the provider asked Temporal to run and hands
// back a canned result — everything up to the wire, with no server.
type fakeStarter struct {
	calls    []startedActivity
	result   any
	startErr error
	getErr   error
}

type startedActivity struct {
	opts client.StartActivityOptions
	name any
	args []any
}

func (f *fakeStarter) ExecuteActivity(ctx context.Context, opts client.StartActivityOptions, activity any, args ...any) (client.ActivityHandle, error) {
	f.calls = append(f.calls, startedActivity{opts: opts, name: activity, args: args})
	if f.startErr != nil {
		return nil, f.startErr
	}
	return &fakeHandle{id: opts.ID, result: f.result, err: f.getErr}, nil
}

type fakeHandle struct {
	id     string
	result any
	err    error
}

func (h *fakeHandle) GetID() string    { return h.id }
func (h *fakeHandle) GetRunID() string { return "run-" + h.id }

func (h *fakeHandle) Get(ctx context.Context, valuePtr any) error {
	if h.err != nil {
		return h.err
	}
	p, ok := valuePtr.(*any)
	if !ok {
		return errors.New("fakeHandle: provider decoded into an unexpected type")
	}
	*p = h.result
	return nil
}

func (h *fakeHandle) Describe(context.Context, client.DescribeActivityOptions) (*client.ActivityExecutionDescription, error) {
	return nil, errors.New("not implemented")
}
func (h *fakeHandle) Cancel(context.Context, client.CancelActivityOptions) error       { return nil }
func (h *fakeHandle) Terminate(context.Context, client.TerminateActivityOptions) error { return nil }

const page = `<Gooey xmlns="wonderforge.io/gooey/2026"
       xmlns:temporal="gooey.dev/handlers/temporal">
  <VStack>
    <Button Content="run" Click="{{temporal:Activity ` + "`Slugify`" + ` .Input | into .Output}}"/>
    <Text>{{.Output}}</Text>
  </VStack>
</Gooey>`

type harness struct {
	t    *testing.T
	in   *prop.Property[string]
	out  *prop.Property[string]
	disp *gooey.Dispatcher
	comp *gooey.Composer
}

func build(t *testing.T, src string, f *fakeStarter) *harness {
	t.Helper()
	markup.RegisterHandlers(URI, newProvider(f, "gooey-test", WithIDPrefix("test")))
	t.Cleanup(func() { markup.RegisterHandlers(URI, nil) })

	h := &harness{
		t:    t,
		in:   prop.NewSource("Hello There"),
		out:  prop.NewSource("(nothing yet)"),
		disp: gooey.NewDispatcher(),
	}
	ctx := &markup.Context{
		Values:     map[string]any{"Input": h.in, "Output": h.out},
		Dispatcher: h.disp,
	}
	w, err := markup.Load(fstest.MapFS{"page.gooey": {Data: []byte(src)}}, "page.gooey", ctx)
	if err != nil {
		t.Fatal(err)
	}
	h.comp = gooey.NewComposer(w, 60, 6)
	return h
}

func (h *harness) clickAndSettle() {
	h.t.Helper()
	if !h.comp.HandleKey(input.Named(input.KeyEnter)) {
		h.t.Fatal("enter did not reach the focused button")
	}
	select {
	case <-h.disp.Wake():
		h.disp.Drain()
	case <-time.After(5 * time.Second):
		h.t.Fatal("the activity result never reached the dispatcher")
	}
}

func TestActivityIsStartedWithMarkupSuppliedNameAndArgs(t *testing.T) {
	f := &fakeStarter{result: "hello-there"}
	h := build(t, page, f)
	h.clickAndSettle()

	if len(f.calls) != 1 {
		t.Fatalf("started %d activities, want 1", len(f.calls))
	}
	c := f.calls[0]
	if c.name != "Slugify" {
		t.Fatalf("activity type = %v, want the backtick literal Slugify", c.name)
	}
	if len(c.args) != 1 || c.args[0] != "Hello There" {
		t.Fatalf("args = %v, want the current value of .Input", c.args)
	}
	if c.opts.TaskQueue != "gooey-test" {
		t.Fatalf("task queue = %q, want the provider's, never markup's", c.opts.TaskQueue)
	}
	if c.opts.ScheduleToCloseTimeout != DefaultTimeout {
		t.Fatalf("ScheduleToCloseTimeout = %v, want %v", c.opts.ScheduleToCloseTimeout, DefaultTimeout)
	}
	if !strings.HasPrefix(c.opts.ID, "test-Slugify-") {
		t.Fatalf("activity ID = %q, want the configured prefix", c.opts.ID)
	}
	if got := h.out.Get(); got != "hello-there" {
		t.Fatalf("output = %q, want the activity result", got)
	}
}

// Two clicks must be two executions: reusing an activity ID collides
// with the still-running one.
func TestEachInvocationGetsAFreshActivityID(t *testing.T) {
	f := &fakeStarter{result: "ok"}
	h := build(t, page, f)
	h.clickAndSettle()
	h.clickAndSettle()

	if len(f.calls) != 2 {
		t.Fatalf("started %d activities, want 2", len(f.calls))
	}
	if f.calls[0].opts.ID == f.calls[1].opts.ID {
		t.Fatalf("both executions used ID %q", f.calls[0].opts.ID)
	}
}

// The argument is a handle: whatever .Input holds at click time is what
// the worker receives.
func TestArgumentsAreReadAtInvokeTime(t *testing.T) {
	f := &fakeStarter{result: "ok"}
	h := build(t, page, f)
	h.in.Set("first value")
	h.clickAndSettle()
	h.in.Set("second value")
	h.clickAndSettle()

	if f.calls[0].args[0] != "first value" || f.calls[1].args[0] != "second value" {
		t.Fatalf("worker saw %v then %v", f.calls[0].args, f.calls[1].args)
	}
}

// The activity's return type is unknown to the provider — markup named
// it, not Go — so a non-string result is rendered as JSON.
func TestNonStringResultsRenderAsJSON(t *testing.T) {
	f := &fakeStarter{result: map[string]any{"slug": "hello-there", "len": float64(11)}}
	h := build(t, page, f)
	h.clickAndSettle()

	got := h.out.Get()
	if !strings.Contains(got, `"slug": "hello-there"`) {
		t.Fatalf("output = %q, want indented JSON of the result", got)
	}
}

func TestFailuresLandInTheTarget(t *testing.T) {
	t.Run("start fails", func(t *testing.T) {
		h := build(t, page, &fakeStarter{startErr: errors.New("no worker on that queue")})
		h.clickAndSettle()
		if got := h.out.Get(); !strings.HasPrefix(got, "ERROR:") || !strings.Contains(got, "no worker") {
			t.Fatalf("output = %q", got)
		}
	})
	t.Run("activity fails", func(t *testing.T) {
		h := build(t, page, &fakeStarter{getErr: errors.New("activity error: boom")})
		h.clickAndSettle()
		if got := h.out.Get(); !strings.HasPrefix(got, "ERROR:") || !strings.Contains(got, "boom") {
			t.Fatalf("output = %q", got)
		}
	})
}

// The result reaches the screen through the ordinary graph — one component
// repaints, the one that read the property.
func TestResultRepaintsOnlyTheBoundComponent(t *testing.T) {
	h := build(t, page, &fakeStarter{result: "slugged"})
	h.comp.Frame()
	h.clickAndSettle()

	frame, painted := h.comp.Frame()
	if painted != 1 {
		t.Fatalf("repainted %d components, want exactly the bound Text", painted)
	}
	var sb strings.Builder
	for y := 0; y < 6; y++ {
		for x := 0; x < 60; x++ {
			sb.WriteRune(frame.Cells.At(x, y).Rune)
		}
		sb.WriteByte('\n')
	}
	if !strings.Contains(sb.String(), "slugged") {
		t.Fatalf("result never reached the screen:\n%s", sb.String())
	}
}

func TestLoadErrors(t *testing.T) {
	cases := map[string]struct{ expr, want string }{
		"unknown function": {"{{temporal:Workflow `X` | into .Output}}", "unknown function"},
		"no activity name": {"{{temporal:Activity | into .Output}}", "at least the activity type name"},
		"missing target":   {"{{temporal:Activity `Slugify` .Input}}", "needs a result target"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			markup.RegisterHandlers(URI, newProvider(&fakeStarter{}, "gooey-test"))
			defer markup.RegisterHandlers(URI, nil)
			src := `<Gooey xmlns:temporal="gooey.dev/handlers/temporal"><Button Content="x" Click="` + tc.expr + `"/></Gooey>`
			ctx := &markup.Context{
				Values: map[string]any{
					"Input": prop.NewSource(""), "Output": prop.NewSource(""),
				},
				Dispatcher: gooey.NewDispatcher(),
			}
			_, err := markup.Load(fstest.MapFS{"page.gooey": {Data: []byte(src)}}, "page.gooey", ctx)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err=%v, want one mentioning %q", err, tc.want)
			}
		})
	}
}

// A provider built without a client or queue is a misconfigured host,
// and that shows up when its markup loads rather than on the first click.
func TestMisconfiguredProviderFailsAtLoad(t *testing.T) {
	for name, p := range map[string]*Provider{
		"no client":     New(nil, "queue"),
		"no task queue": newProvider(&fakeStarter{}, ""),
	} {
		t.Run(name, func(t *testing.T) {
			markup.RegisterHandlers(URI, p)
			defer markup.RegisterHandlers(URI, nil)
			ctx := &markup.Context{
				Values: map[string]any{
					"Input": prop.NewSource(""), "Output": prop.NewSource(""),
				},
				Dispatcher: gooey.NewDispatcher(),
			}
			_, err := markup.Load(fstest.MapFS{"page.gooey": {Data: []byte(page)}}, "page.gooey", ctx)
			if err == nil {
				t.Fatal("expected a load error")
			}
		})
	}
}

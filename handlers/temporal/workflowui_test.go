package temporalhandlers

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/prop"
)

// fakeSignaler records what served markup asked the workflow to be told.
//
// It is locked because a handler runs on its own goroutine by design —
// that is what the Dispatcher exists to marshal back — so the recording
// side and the asserting side are genuinely concurrent. Tests that wait
// on a receipt are synchronized by that receipt; the one that cannot
// (a signal with no `into` target posts nothing) polls, and polling an
// unlocked slice is a data race whatever the timing looks like.
type fakeSignaler struct {
	mu   sync.Mutex
	sent []sentSignal
	err  error
}

type sentSignal struct {
	workflowID string
	runID      string
	name       string
	arg        any
}

func (f *fakeSignaler) SignalWorkflow(ctx context.Context, workflowID, runID, name string, arg any) error {
	f.mu.Lock()
	f.sent = append(f.sent, sentSignal{workflowID, runID, name, arg})
	err := f.err
	f.mu.Unlock()
	return err
}

func (f *fakeSignaler) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sent)
}

func (f *fakeSignaler) at(i int) sentSignal {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.sent[i]
}

// servedPage stands in for markup that arrived from a query rather than
// from the shell's own filesystem.
const servedPage = `<Gooey xmlns="wonderforge.io/gooey/2026"
       xmlns:wf="gooey.dev/handlers/temporal/workflow">
  <VStack>
    <Button Content="approve" Click="{{wf:Signal ` + "`approve`" + ` .Tier | into .Notice}}"/>
    <Text>{{.Notice}}</Text>
  </VStack>
</Gooey>`

type wfHarness struct {
	t      *testing.T
	tier   *prop.Property[string]
	notice *prop.Property[string]
	disp   *gooey.Dispatcher
	comp   *gooey.Composer
}

func buildServed(t *testing.T, src string, f *fakeSignaler) *wfHarness {
	t.Helper()
	markup.RegisterHandlers(WorkflowURI, newWorkflowUI(f, "wizard-1"))
	t.Cleanup(func() { markup.RegisterHandlers(WorkflowURI, nil) })

	h := &wfHarness{
		t:      t,
		tier:   prop.NewSource("medium"),
		notice: prop.NewSource("(nothing yet)"),
		disp:   gooey.NewDispatcher(),
	}
	ctx := &markup.Context{
		Values:     map[string]any{"Tier": h.tier, "Notice": h.notice},
		Dispatcher: h.disp,
	}
	// Build, not Load: served markup is a byte slice off the wire, and
	// there is no file anywhere for it.
	w, err := markup.Build([]byte(src), ctx)
	if err != nil {
		t.Fatal(err)
	}
	h.comp = gooey.NewComposer(w, 60, 6)
	return h
}

func (h *wfHarness) pressAndSettle() {
	h.t.Helper()
	if !h.comp.HandleKey(input.Named(input.KeyEnter)) {
		h.t.Fatal("enter did not reach the focused button")
	}
	select {
	case <-h.disp.Wake():
		h.disp.Drain()
	case <-time.After(5 * time.Second):
		h.t.Fatal("the signal receipt never reached the dispatcher")
	}
}

func TestSignalCarriesMarkupSuppliedNameAndPayload(t *testing.T) {
	f := &fakeSignaler{}
	h := buildServed(t, servedPage, f)
	h.pressAndSettle()

	if f.count() != 1 {
		t.Fatalf("sent %d signals, want 1", f.count())
	}
	s := f.at(0)
	if s.name != "approve" {
		t.Fatalf("signal name = %q, want the backtick literal approve", s.name)
	}
	if s.workflowID != "wizard-1" || s.runID != "" {
		t.Fatalf("target = %q/%q, want the host's workflow and the latest run", s.workflowID, s.runID)
	}
	payload, ok := s.arg.([]string)
	if !ok || len(payload) != 1 || payload[0] != "medium" {
		t.Fatalf("payload = %#v, want the current value of .Tier", s.arg)
	}
	if got := h.notice.Get(); !strings.Contains(got, "approve") {
		t.Fatalf("receipt = %q, want it to name the signal", got)
	}
}

// The payload is read at press time, not at load time — same lvalue
// semantics as every other binding.
func TestSignalPayloadIsReadAtPressTime(t *testing.T) {
	f := &fakeSignaler{}
	h := buildServed(t, servedPage, f)
	h.tier.Set("small")
	h.pressAndSettle()
	h.tier.Set("large")
	h.pressAndSettle()

	first := f.at(0).arg.([]string)
	second := f.at(1).arg.([]string)
	if first[0] != "small" || second[0] != "large" {
		t.Fatalf("workflow saw %v then %v", first, second)
	}
}

// A signal with no payload is still a valid press: the name alone is the
// whole message.
func TestSignalWithoutPayload(t *testing.T) {
	f := &fakeSignaler{}
	src := `<Gooey xmlns:wf="gooey.dev/handlers/temporal/workflow">` +
		"<Button Content=\"go\" Click=\"{{wf:Signal `continue`}}\"/></Gooey>"
	h := buildServed(t, src, f)
	if !h.comp.HandleKey(input.Named(input.KeyEnter)) {
		t.Fatal("enter did not reach the button")
	}
	deadline := time.After(5 * time.Second)
	for f.count() == 0 {
		select {
		case <-time.After(5 * time.Millisecond):
		case <-deadline:
			t.Fatal("no signal was sent")
		}
	}
	if got := f.at(0).arg.([]string); len(got) != 0 {
		t.Fatalf("payload = %#v, want empty", got)
	}
}

func TestSignalFailureLandsInTheTarget(t *testing.T) {
	f := &fakeSignaler{err: errors.New("workflow not found")}
	h := buildServed(t, servedPage, f)
	h.pressAndSettle()

	if got := h.notice.Get(); !strings.HasPrefix(got, "ERROR:") || !strings.Contains(got, "not found") {
		t.Fatalf("receipt = %q", got)
	}
}

// The receipt reaches the screen through the ordinary graph: one component
// repaints, the one that read the property.
func TestReceiptRepaintsOnlyTheBoundComponent(t *testing.T) {
	h := buildServed(t, servedPage, &fakeSignaler{})
	h.comp.Frame()
	h.pressAndSettle()

	if _, painted := h.comp.Frame(); painted != 1 {
		t.Fatalf("repainted %d components, want exactly the bound Text", painted)
	}
}

func TestWorkflowLoadErrors(t *testing.T) {
	cases := map[string]struct{ expr, want string }{
		"unknown function": {"{{wf:Query `ui` | into .Notice}}", "unknown function"},
		"no signal name":   {"{{wf:Signal | into .Notice}}", "at least the signal name"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			markup.RegisterHandlers(WorkflowURI, newWorkflowUI(&fakeSignaler{}, "wizard-1"))
			defer markup.RegisterHandlers(WorkflowURI, nil)
			src := `<Gooey xmlns:wf="gooey.dev/handlers/temporal/workflow"><Button Content="x" Click="` + tc.expr + `"/></Gooey>`
			ctx := &markup.Context{
				Values:     map[string]any{"Tier": prop.NewSource(""), "Notice": prop.NewSource("")},
				Dispatcher: gooey.NewDispatcher(),
			}
			_, err := markup.Load(fstest.MapFS{"page.gooey": {Data: []byte(src)}}, "page.gooey", ctx)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err=%v, want one mentioning %q", err, tc.want)
			}
		})
	}
}

// Served markup that names a namespace the shell never granted must fail
// to load, naming what was missing. This is the whole sandbox story: a
// workflow cannot talk its shell into capabilities it was not given.
func TestUngrantedNamespaceFailsToLoad(t *testing.T) {
	src := `<Gooey xmlns:wf="gooey.dev/handlers/temporal/workflow">` +
		"<Button Content=\"x\" Click=\"{{wf:Signal `approve`}}\"/></Gooey>"
	ctx := &markup.Context{Values: map[string]any{}, Dispatcher: gooey.NewDispatcher()}
	_, err := markup.Build([]byte(src), ctx)
	if err == nil || !strings.Contains(err.Error(), "no registered handler provider") {
		t.Fatalf("err=%v, want a load error naming the ungranted namespace", err)
	}
}

// A served page binding a value the served VALUES map does not carry is a
// load-time failure, not a blank on screen. Markup and values are one
// payload for exactly this reason.
func TestServedMarkupMustMatchServedValues(t *testing.T) {
	markup.RegisterHandlers(WorkflowURI, newWorkflowUI(&fakeSignaler{}, "wizard-1"))
	defer markup.RegisterHandlers(WorkflowURI, nil)
	ctx := &markup.Context{
		Values:     map[string]any{"Notice": prop.NewSource("")},
		Dispatcher: gooey.NewDispatcher(),
	}
	_, err := markup.Build([]byte(servedPage), ctx)
	if err == nil || !strings.Contains(err.Error(), "Tier") {
		t.Fatalf("err=%v, want a load error naming the missing value", err)
	}
}

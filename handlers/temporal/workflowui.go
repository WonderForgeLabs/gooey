package temporalhandlers

// Workflow-served UI: the other direction of the same idea.
//
// The Activity provider above lets a terminal-owned page reach out for
// compute. This provider is for the inverse arrangement, where a
// WORKFLOW owns the application and the terminal is a generic shell:
// the workflow serves its own gooey markup from a query, the shell
// renders whatever arrived, and every interaction goes back as a signal.
//
//	<Gooey xmlns:wf="gooey.dev/handlers/temporal/workflow">
//	  <Button Content="approve" Click="{{wf:Signal `approve` | into .Notice}}"/>
//
// That markup is DATA — it was never compiled into the shell, and the
// shell has no delegate named approve. What the shell contributes is the
// capability grant: it constructs this provider around a client and one
// workflow ID and registers it under the URI. Served markup can therefore
// signal that workflow and do nothing else — it cannot pick a different
// workflow, cannot start activities, cannot reach the network. The
// workflow decides what the screen says; the shell decides what the
// screen is allowed to do.
//
// Signals are one-way and at-least-once, exactly like the activity side:
// a button press may deliver twice, so signal handlers should be written
// so a repeat is harmless.

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/markup"
	"go.temporal.io/sdk/client"
)

// WorkflowURI is the namespace URI markup declares to reach this
// provider. It is deliberately distinct from URI: granting a page the
// right to signal one workflow is not the same capability as granting it
// the right to start arbitrary activities.
const WorkflowURI = "gooey.dev/handlers/temporal/workflow"

// DefaultSignalTimeout bounds one signal round trip. A signal is a small
// write to an open workflow, so a slow one means the server is unwell —
// better to surface that in the UI than to hang the send goroutine.
const DefaultSignalTimeout = 10 * time.Second

// workflowSignaler is the slice of client.Client this provider uses:
// it signals one workflow and does nothing else. Tests substitute it.
type workflowSignaler interface {
	SignalWorkflow(ctx context.Context, workflowID, runID, signalName string, arg any) error
}

// WorkflowUI implements markup.HandlerProvider for the workflow
// namespace. It is bound to ONE workflow at construction: the target is
// host configuration, never something markup can name.
type WorkflowUI struct {
	client     workflowSignaler
	workflowID string
	runID      string
	timeout    time.Duration
}

// WorkflowOption configures a WorkflowUI.
type WorkflowOption func(*WorkflowUI)

// WithRunID pins signals to one run. The default — the empty run ID —
// targets the latest run, which is what a UI wants: a workflow that
// continued-as-new is still the same application to the person looking
// at it.
func WithRunID(id string) WorkflowOption { return func(w *WorkflowUI) { w.runID = id } }

// WithSignalTimeout bounds one signal round trip.
func WithSignalTimeout(d time.Duration) WorkflowOption {
	return func(w *WorkflowUI) { w.timeout = d }
}

// NewWorkflowUI grants served markup the capability to signal workflowID.
func NewWorkflowUI(c client.Client, workflowID string, opts ...WorkflowOption) *WorkflowUI {
	return newWorkflowUI(c, workflowID, opts...)
}

func newWorkflowUI(c workflowSignaler, workflowID string, opts ...WorkflowOption) *WorkflowUI {
	w := &WorkflowUI{client: c, workflowID: workflowID, timeout: DefaultSignalTimeout}
	for _, o := range opts {
		o(w)
	}
	return w
}

// NewCommand resolves one {{wf:…}} expression at load time.
func (w *WorkflowUI) NewCommand(c *markup.Call) (gooey.Command, error) {
	switch c.Fn {
	case "Signal":
		return w.signal(c)
	}
	return nil, fmt.Errorf("unknown function %q; the workflow namespace provides: Signal", c.Fn)
}

func (w *WorkflowUI) signal(c *markup.Call) (gooey.Command, error) {
	if w.client == nil {
		return nil, fmt.Errorf("provider has no Temporal client")
	}
	if w.workflowID == "" {
		return nil, fmt.Errorf("provider has no workflow ID")
	}
	if len(c.Args) < 1 {
		return nil, fmt.Errorf("Signal needs at least the signal name, e.g. {{wf:Signal `approve`}}")
	}
	name, rest, target := c.Args[0], c.Args[1:], c.Target
	return func() {
		// On the UI goroutine: read every handle now. The payload is a
		// snapshot of what the screen showed when it was pressed, which is
		// the only reading of it that means anything to the person who
		// pressed it.
		signalName := name.String()
		payload := make([]string, len(rest))
		for i, a := range rest {
			payload[i] = a.String()
		}
		go func() { target.Deliver(w.send(signalName, payload)) }()
	}, nil
}

// send performs the signal off the UI goroutine and renders the outcome
// as the string an optional `| into .Target` receives. Delivering to an
// absent target is a no-op, so markup that does not care about the
// receipt simply omits the stage.
func (w *WorkflowUI) send(name string, payload []string) string {
	if strings.TrimSpace(name) == "" {
		return "ERROR: empty signal name"
	}
	ctx, cancel := context.WithTimeout(context.Background(), w.timeout)
	defer cancel()

	if err := w.client.SignalWorkflow(ctx, w.workflowID, w.runID, name, payload); err != nil {
		return "ERROR: " + err.Error()
	}
	if len(payload) == 0 {
		return "sent " + name
	}
	return "sent " + name + " " + strings.Join(payload, " ")
}

// Package temporalhandlers makes Temporal the backend compute plane for
// a gooey app: markup names an activity, workers anywhere run it.
//
//	<Gooey xmlns:temporal="gooey.dev/handlers/temporal">
//	  <Button Content="slugify"
//	          Click="{{temporal:Activity `Slugify` .Input | into .Output}}"/>
//
// The terminal declares WHAT runs — an activity type and arguments read
// from properties — and never learns how or where. The host app builds
// the provider with a connected client and a task queue and registers
// it; connection config, credentials, and queue routing stay out of the
// markup entirely:
//
//	markup.RegisterHandlers(temporalhandlers.URI,
//	    temporalhandlers.New(c, "gooey-demo"))
//
// # Standalone activities
//
// This is built on Temporal STANDALONE ACTIVITIES (client.ExecuteActivity,
// SDK ≥ v1.41, server ≥ 1.31): a top-level activity execution started
// directly by a client with no workflow orchestrating it. That is the
// right primitive here because a button press is exactly one durable,
// retryable unit of work — a workflow would add an orchestration layer
// with nothing to orchestrate. The activity function itself is ordinary;
// the same code runs from a workflow unchanged.
//
// # At-least-once
//
// Activities retry by default, so an activity invoked from markup may
// run MORE THAN ONCE for a single click. Bind these handlers to
// idempotent activities. The delivery side is naturally last-write-wins:
// each completion Sets the target property, so a retried run simply
// overwrites with the same answer.
package temporalhandlers

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/markup"
	"go.temporal.io/sdk/client"
)

// URI is the namespace URI markup declares to reach this provider.
const URI = "gooey.dev/handlers/temporal"

// DefaultTimeout is the ScheduleToClose applied when the host sets none:
// long enough for real work, short enough that a wedged worker shows up
// as an error in the UI rather than a box that never fills.
const DefaultTimeout = 60 * time.Second

// activityStarter is the slice of client.Client this provider actually
// uses. Naming it keeps the dependency honest — the provider starts
// activities and does nothing else with the client — and gives tests a
// seam that needs no server and no mocking framework.
type activityStarter interface {
	ExecuteActivity(ctx context.Context, options client.StartActivityOptions, activity any, args ...any) (client.ActivityHandle, error)
}

// Provider implements markup.HandlerProvider for the temporal namespace.
type Provider struct {
	client    activityStarter
	taskQueue string
	timeout   time.Duration
	idPrefix  string
	seq       atomic.Uint64
}

// Option configures a Provider.
type Option func(*Provider)

// WithTimeout sets the ScheduleToCloseTimeout for executions, which
// bounds the total time including retries.
func WithTimeout(d time.Duration) Option { return func(p *Provider) { p.timeout = d } }

// WithIDPrefix sets the prefix of generated activity IDs, which is what
// these executions look like in the Temporal UI and CLI.
func WithIDPrefix(s string) Option { return func(p *Provider) { p.idPrefix = s } }

// New builds the provider from a connected client and the task queue
// its activities are scheduled on. Register it to grant the namespace.
func New(c client.Client, taskQueue string, opts ...Option) *Provider {
	return newProvider(c, taskQueue, opts...)
}

func newProvider(c activityStarter, taskQueue string, opts ...Option) *Provider {
	p := &Provider{
		client:    c,
		taskQueue: taskQueue,
		timeout:   DefaultTimeout,
		idPrefix:  "gooey",
	}
	for _, o := range opts {
		o(p)
	}
	return p
}

// NewCommand resolves one {{temporal:…}} expression at load time.
func (p *Provider) NewCommand(c *markup.Call) (gooey.Command, error) {
	switch c.Fn {
	case "Activity":
		return p.activity(c)
	}
	return nil, fmt.Errorf("unknown function %q; temporal provides: Activity", c.Fn)
}

func (p *Provider) activity(c *markup.Call) (gooey.Command, error) {
	if p.client == nil {
		return nil, fmt.Errorf("provider has no Temporal client")
	}
	if p.taskQueue == "" {
		return nil, fmt.Errorf("provider has no task queue")
	}
	if len(c.Args) < 1 {
		return nil, fmt.Errorf("Activity needs at least the activity type name, e.g. {{temporal:Activity `Slugify` .Input | into .Out}}")
	}
	if !c.Target.Valid() {
		return nil, fmt.Errorf("Activity needs a result target — add `| into .SomeProperty`")
	}
	name, rest, target := c.Args[0], c.Args[1:], c.Target
	return func() {
		// On the UI goroutine: read the handles now, snapshot the values.
		// Nothing beyond this point may touch a property.
		typeName := name.String()
		args := make([]any, len(rest))
		for i, a := range rest {
			args[i] = a.String()
		}
		go func() { target.Deliver(p.run(typeName, args)) }()
	}, nil
}

// run executes the activity off the UI goroutine and renders either
// outcome as the string the target property will hold.
func (p *Provider) run(typeName string, args []any) string {
	if strings.TrimSpace(typeName) == "" {
		return "ERROR: empty activity type name"
	}
	// The context outlives ScheduleToClose slightly so a server-side
	// timeout comes back as Temporal's own error rather than a local
	// deadline, which says far more about what went wrong.
	ctx, cancel := context.WithTimeout(context.Background(), p.timeout+10*time.Second)
	defer cancel()

	handle, err := p.client.ExecuteActivity(ctx, client.StartActivityOptions{
		ID:                     p.nextID(typeName),
		TaskQueue:              p.taskQueue,
		ScheduleToCloseTimeout: p.timeout,
		Summary:                "gooey " + typeName,
	}, typeName, args...)
	if err != nil {
		return "ERROR: " + err.Error()
	}

	// Decode into any: the payload converter fills it from the activity's
	// JSON result, so a string result arrives as a string and a struct
	// arrives as a map we can re-render. This avoids the provider having
	// to know the activity's return type — which it cannot, since the
	// type name came from markup.
	var out any
	if err := handle.Get(ctx, &out); err != nil {
		return "ERROR: " + err.Error()
	}
	return render(out)
}

func render(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}

// nextID makes each invocation its own execution. Reusing an ID would
// collide with a still-running execution (or be rejected by the reuse
// policy), so clicking the button twice must not produce the same ID.
func (p *Provider) nextID(typeName string) string {
	return fmt.Sprintf("%s-%s-%d-%d", p.idPrefix, typeName, time.Now().UnixNano(), p.seq.Add(1))
}

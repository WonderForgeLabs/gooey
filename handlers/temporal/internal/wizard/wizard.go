package wizard

// ProvisionWizard is a workflow that owns an application, screens and
// all.
//
// The usual arrangement has a UI that calls a backend. This is the
// inverse: the workflow holds the state, serves the markup for whatever
// stage it is in, and advances only when a signal arrives. Everything
// that decides, computes, or takes time is an activity — including the
// activity that hands out the markup. The terminal contributes a
// renderer, a theme, and one capability (signal this workflow).
//
// "Modifying itself" is literal. Each stage transition swaps the markup
// the ui query returns; the client notices the version change and
// rebuilds its widget tree from the new source. No client release, no
// hot-reloaded file on the client's disk — the screen changed because a
// workflow moved on.
//
// # The wire contract
//
// The query answers with UIState: a version, a revision, the markup, and
// the values that markup binds. Markup and values travel TOGETHER for a
// reason — markup binding a value the map does not carry is a load-time
// error in gooey, so a torn read of the two would take the screen down.
// One query, one consistent pair.
//
// # Determinism
//
// Nothing here reads a clock, a random source, or the network:
// workflow.Now is the replay-safe time, and every other fact on screen
// arrived as an activity result recorded in history. A replay of this
// workflow reconstructs, byte for byte, the UI the user was looking at.

import (
	"fmt"
	"strings"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

// QueryUI is the query the client polls. Its name is client
// configuration, not client knowledge: the shell is told which query to
// ask, and asks it.
const QueryUI = "ui"

// The signals the wizard understands. Markup names these as backtick
// literals; the client never sees them in its own source.
const (
	SignalChoose   = "choose"
	SignalContinue = "continue"
	SignalApprove  = "approve"
	SignalReject   = "reject"
	SignalRestart  = "restart"
	SignalFinish   = "finish"
)

// Stage names double as the markup file names the loader activity serves.
const (
	stageChoose    = "choose"
	stageReview    = "review"
	stageProvision = "provision"
	stageDone      = "done"
	stageClosed    = "closed"
)

// UIState is the whole client contract: one screen, ready to render.
//
// Version identifies the MARKUP. Revision identifies the state. A client
// that sees a new Version must rebuild its widget tree; a client that
// sees only a new Revision sets the changed values and lets the property
// graph repaint the widgets that read them. Splitting the two is what
// keeps a progress screen from being rebuilt eight times while it counts.
type UIState struct {
	Version  int               `json:"version"`
	Revision int               `json:"revision"`
	Stage    string            `json:"stage"`
	Markup   string            `json:"gooeyMarkup"`
	Values   map[string]string `json:"values"`
	Done     bool              `json:"done"`
}

// Summary is the workflow's return value — the durable record of what the
// session produced, independent of anything that was on screen.
type Summary struct {
	Ticket string `json:"ticket"`
	Tier   string `json:"tier"`
	Region string `json:"region"`
	At     string `json:"at"`
}

type wizard struct {
	ctx  workflow.Context
	acts workflow.Context // ctx with activity options applied

	st      UIState
	req     Request
	ticket  string
	log     []string
	summary Summary
}

// initialValues is the full key set the UI values map ever carries. It is
// constant across stages on purpose: a stage swap should change the
// markup, not the shape of the data, so the client can tell "new screen"
// from "new numbers" without guessing.
var initialValues = []struct{ key, val string }{
	{"Stage", ""},
	{"Served", ""},
	{"Tier", "(not chosen)"},
	{"Region", "(not chosen)"},
	{"Notice", "pick a size and a region, then continue"},
	{"Estimate", "—"},
	{"Lead", "—"},
	{"Step1", "· queued   ReserveCapacity"},
	{"Step2", "· queued   ProvisionResource"},
	{"Step3", "· queued   NotifyOwner"},
	{"Ticket", "—"},
	{"Summary", "—"},
	{"Log", ""},
}

func ProvisionWizard(ctx workflow.Context) (Summary, error) {
	w := &wizard{
		ctx: ctx,
		st:  UIState{Values: map[string]string{}},
	}
	w.acts = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
		RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 3},
	})
	// Registered before anything else runs, so a client that queries
	// during the very first activity gets an answer rather than an error.
	if err := workflow.SetQueryHandler(ctx, QueryUI, w.query); err != nil {
		return Summary{}, err
	}
	w.reset()

	stage := stageChoose
	for {
		var err error
		switch stage {
		case stageChoose:
			stage, err = w.choose()
		case stageReview:
			stage, err = w.review()
		case stageProvision:
			stage, err = w.provision()
		case stageDone:
			stage, err = w.done()
		case stageClosed:
			if err := w.enter(stageClosed); err != nil {
				return Summary{}, err
			}
			w.st.Done = true
			w.st.Revision++
			// A closed workflow still answers queries — the server replays
			// the history and the handler above re-registers with this
			// final state — so the last screen the workflow served outlives
			// the workflow itself.
			return w.summary, nil
		default:
			return Summary{}, fmt.Errorf("unknown stage %q", stage)
		}
		if err != nil {
			return Summary{}, err
		}
	}
}

// query is the whole client-facing API. It hands back a copy: the state
// keeps mutating as the workflow advances, and a client should never hold
// a map the workflow is still writing.
func (w *wizard) query() (UIState, error) {
	st := w.st
	st.Values = make(map[string]string, len(w.st.Values))
	for k, v := range w.st.Values {
		st.Values[k] = v
	}
	return st, nil
}

// enter swaps the served UI. Fetching the markup through an activity is
// what makes this a deploy-time asset rather than a workflow-code
// constant — and it puts one more visible execution in the history for
// every screen the user sees.
func (w *wizard) enter(stage string) error {
	var src string
	if err := workflow.ExecuteActivity(w.acts, LoadStageMarkup, stage).Get(w.acts, &src); err != nil {
		return err
	}
	w.st.Stage = stage
	w.st.Markup = src
	w.st.Version++
	w.st.Revision++
	w.set("Stage", stage)

	info := workflow.GetInfo(w.ctx)
	w.set("Served", fmt.Sprintf("served by workflow %s · run %s · stage %s · markup v%d · %s UTC",
		info.WorkflowExecution.ID, shortID(info.WorkflowExecution.RunID), stage, w.st.Version,
		workflow.Now(w.ctx).UTC().Format("15:04:05")))
	return nil
}

// set records a value change. The revision only moves when something
// actually changed, so a client polling a screen nobody is touching does
// no work at all.
func (w *wizard) set(key, val string) {
	// Presence, not just equality: a key whose first value is the empty
	// string still has to APPEAR in the map. Markup binding a key the
	// values map does not carry is a load-time failure in the client, so
	// "absent" and "empty" are very different answers.
	if cur, ok := w.st.Values[key]; ok && cur == val {
		return
	}
	w.st.Values[key] = val
	w.st.Revision++
}

func (w *wizard) logf(format string, args ...any) {
	line := workflow.Now(w.ctx).UTC().Format("15:04:05") + "  " + fmt.Sprintf(format, args...)
	w.log = append(w.log, line)
	if len(w.log) > 8 {
		w.log = w.log[len(w.log)-8:]
	}
	w.set("Log", strings.Join(w.log, "\n"))
}

func (w *wizard) reset() {
	w.req = Request{}
	w.ticket = ""
	w.log = nil
	for _, kv := range initialValues {
		w.set(kv.key, kv.val)
	}
}

// choose is stage one: buttons, and a validation activity standing
// between them and stage two.
func (w *wizard) choose() (string, error) {
	if err := w.enter(stageChoose); err != nil {
		return "", err
	}
	picks := workflow.GetSignalChannel(w.ctx, SignalChoose)
	next := workflow.GetSignalChannel(w.ctx, SignalContinue)

	for {
		var picked []string
		advance := false

		sel := workflow.NewSelector(w.ctx)
		sel.AddReceive(picks, func(c workflow.ReceiveChannel, _ bool) {
			c.Receive(w.ctx, &picked)
		})
		sel.AddReceive(next, func(c workflow.ReceiveChannel, _ bool) {
			var ignored []string
			c.Receive(w.ctx, &ignored)
			advance = true
		})
		sel.Select(w.ctx)

		switch {
		case len(picked) >= 2:
			var ch Choice
			err := workflow.ExecuteActivity(w.acts, DescribeChoice, picked[0], picked[1]).Get(w.acts, &ch)
			if err != nil {
				w.set("Notice", "that choice was refused: "+err.Error())
				w.logf("DescribeChoice(%s,%s) failed", picked[0], picked[1])
				continue
			}
			switch ch.Field {
			case "tier":
				w.req.Tier = ch.Value
				w.set("Tier", ch.Label)
			case "region":
				w.req.Region = ch.Value
				w.set("Region", ch.Label)
			}
			w.set("Notice", ch.Notice)
			w.logf("DescribeChoice → %s", ch.Notice)

		case advance:
			var v Validation
			if err := workflow.ExecuteActivity(w.acts, ValidateRequest, w.req).Get(w.acts, &v); err != nil {
				w.set("Notice", "validation failed: "+err.Error())
				w.logf("ValidateRequest failed: %v", err)
				continue
			}
			w.set("Notice", v.Message)
			w.logf("ValidateRequest → %s", v.Message)
			if v.OK {
				return stageReview, nil
			}
		}
	}
}

// review is stage two: different markup, priced by an activity, waiting
// on one of two signals.
func (w *wizard) review() (string, error) {
	if err := w.enter(stageReview); err != nil {
		return "", err
	}
	var q Quote
	if err := workflow.ExecuteActivity(w.acts, PriceRequest, w.req).Get(w.acts, &q); err != nil {
		return "", err
	}
	w.set("Estimate", q.Estimate)
	w.set("Lead", q.Lead)
	w.set("Notice", "approve to provision, or send it back to change the request")
	w.logf("PriceRequest → %s, lead %s", q.Estimate, q.Lead)

	approve := workflow.GetSignalChannel(w.ctx, SignalApprove)
	reject := workflow.GetSignalChannel(w.ctx, SignalReject)

	var next string
	sel := workflow.NewSelector(w.ctx)
	sel.AddReceive(approve, func(c workflow.ReceiveChannel, _ bool) {
		var ignored []string
		c.Receive(w.ctx, &ignored)
		next = stageProvision
	})
	sel.AddReceive(reject, func(c workflow.ReceiveChannel, _ bool) {
		var ignored []string
		c.Receive(w.ctx, &ignored)
		next = stageChoose
	})
	sel.Select(w.ctx)

	if next == stageChoose {
		w.set("Notice", "sent back — change the request and continue again")
	}
	w.logf("signal → %s", next)
	return next, nil
}

// provision is stage three: no buttons at all. The screen changes because
// activities complete, which is the clearest form the whole idea takes —
// a UI whose next frame is decided by a worker.
func (w *wizard) provision() (string, error) {
	if err := w.enter(stageProvision); err != nil {
		return "", err
	}
	w.set("Notice", "provisioning — this screen has no controls, the workflow is driving")

	steps := []struct {
		key, name string
		fn        any
	}{
		{"Step1", "ReserveCapacity", ReserveCapacity},
		{"Step2", "ProvisionResource", ProvisionResource},
		{"Step3", "NotifyOwner", NotifyOwner},
	}
	for _, s := range steps {
		w.set(s.key, "· queued   "+s.name)
	}

	for _, s := range steps {
		w.set(s.key, "▸ running  "+s.name+" …")
		w.logf("%s started", s.name)

		var r StepResult
		if err := workflow.ExecuteActivity(w.acts, s.fn, w.req, w.ticket).Get(w.acts, &r); err != nil {
			w.set(s.key, "✗ failed   "+s.name)
			w.logf("%s failed: %v", s.name, err)
			w.set("Notice", "provisioning failed — "+err.Error())
			w.set("Summary", "provisioning did not complete")
			return stageDone, nil
		}
		if r.Ticket != "" {
			w.ticket = r.Ticket
			w.set("Ticket", r.Ticket)
		}
		w.set(s.key, fmt.Sprintf("✓ done     %s — %s", s.name, r.Detail))
		w.logf("%s → %s [attempt %d on %s]", s.name, r.Detail, r.Attempt, r.Worker)
	}

	w.summary = Summary{
		Ticket: w.ticket,
		Tier:   w.req.Tier,
		Region: w.req.Region,
		At:     workflow.Now(w.ctx).UTC().Format(time.RFC3339),
	}
	w.set("Summary", fmt.Sprintf("%s provisioned in %s", w.req.Tier, w.req.Region))
	w.set("Notice", "done — three activities ran, none of them here")
	return stageDone, nil
}

// done is stage four, and the loop back: restart rewinds to stage one
// with the same workflow, finish closes it.
func (w *wizard) done() (string, error) {
	if err := w.enter(stageDone); err != nil {
		return "", err
	}
	restart := workflow.GetSignalChannel(w.ctx, SignalRestart)
	finish := workflow.GetSignalChannel(w.ctx, SignalFinish)

	var next string
	sel := workflow.NewSelector(w.ctx)
	sel.AddReceive(restart, func(c workflow.ReceiveChannel, _ bool) {
		var ignored []string
		c.Receive(w.ctx, &ignored)
		next = stageChoose
	})
	sel.AddReceive(finish, func(c workflow.ReceiveChannel, _ bool) {
		var ignored []string
		c.Receive(w.ctx, &ignored)
		next = stageClosed
	})
	sel.Select(w.ctx)

	if next == stageChoose {
		// A real deployment would ContinueAsNew here rather than loop, so
		// history stays bounded across many requests. The loop is kept for
		// the demo because a continued run is a new run ID, and the point
		// being made is that this is one long-lived application.
		w.reset()
		w.set("Notice", "new request — the workflow rewound its own UI to step 1")
	}
	w.logf("signal → %s", next)
	return next, nil
}

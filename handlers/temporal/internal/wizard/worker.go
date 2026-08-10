package wizard

import (
	"context"
	"time"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/log"
	"go.temporal.io/sdk/worker"
)

// The wizard application's worker, in one function, so that the same
// registration serves two very different deployments:
//
//   - workers/wizardworker runs it as its own process, which is what a real
//     deployment looks like — workers live wherever the compute is, and
//     the terminal is one of many clients;
//   - cmd/wizardui runs it as a gooey COMPANION, in-process, for exactly
//     as long as the UI is on screen, which is what a demo wants: one
//     shell instead of two, and nothing left running afterwards.
//
// Neither mode is a special case of the other in the code. The worker
// does not know which it is in; all it sees is a context that will be
// cancelled and a client somebody else dialed.

// DefaultTaskQueue is the queue the wizard's activities and workflow are
// served on, and the one cmd/wizardui starts sessions against.
const DefaultTaskQueue = "gooey-wizard"

// Run serves ProvisionWizard and its seven activities until ctx is done,
// then drains and returns.
//
// The client is a parameter rather than something this function dials
// because the logger travels with it, and the logger is the difference
// between the two deployments: a standalone worker logs to stderr (that
// is its whole UI), while a worker running inside a TUI must not, since
// stderr in raw mode paints over the bottom of the screen. Hand it a
// client built with temporalhandlers.NopLogger and the worker goes
// quiet without this file knowing why.
func Run(ctx context.Context, c client.Client, taskQueue string) error {
	if taskQueue == "" {
		taskQueue = DefaultTaskQueue
	}
	w := worker.New(c, taskQueue, worker.Options{})
	w.RegisterWorkflow(ProvisionWizard)
	w.RegisterActivity(LoadStageMarkup)
	w.RegisterActivity(DescribeChoice)
	w.RegisterActivity(ValidateRequest)
	w.RegisterActivity(PriceRequest)
	w.RegisterActivity(ReserveCapacity)
	w.RegisterActivity(ProvisionResource)
	w.RegisterActivity(NotifyOwner)

	// worker.Run takes a channel rather than a context, so this is the
	// adapter. The goroutine is joined by the deferred close: without it
	// a Run that returned on its own — a poller failing fatally — would
	// leave it parked on ctx.Done forever.
	interrupt := make(chan interface{})
	returned := make(chan struct{})
	defer close(returned)
	go func() {
		select {
		case <-ctx.Done():
			close(interrupt)
		case <-returned:
		}
	}()
	return w.Run(interrupt)
}

// Dial reaches the Temporal server, retrying until it answers or ctx is
// done.
//
// The retry is not defensive padding: a companion worker starts at the
// same moment as the dev server it talks to (see cmd/wizardui's
// --with-dev-server), and "the server is not listening yet" is a normal
// two seconds of a demo's life, not a failure. Where the server really
// is absent, the last dial error comes back and the app reports it
// before taking the screen.
func Dial(ctx context.Context, hostPort string, logger log.Logger) (client.Client, error) {
	var last error
	for {
		c, err := client.Dial(client.Options{HostPort: hostPort, Logger: logger})
		if err == nil {
			return c, nil
		}
		last = err
		select {
		case <-ctx.Done():
			return nil, last
		case <-time.After(250 * time.Millisecond):
		}
	}
}

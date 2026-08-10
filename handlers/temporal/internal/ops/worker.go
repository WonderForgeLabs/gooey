package ops

import (
	"context"

	visibility "github.com/WonderForgeLabs/gooey/packs/temporal-visibility"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
)

// RunWorker serves the visibility pack on taskQueue until ctx is done —
// the registration workers/visibilityworker runs as a process, here as
// a function so cmd/temporalops can run it as a gooey companion: one
// shell, nothing left running afterwards. The pack's Register is
// registry-shaped precisely so both deployments are this one call.
//
// The client is a parameter for the same reason wizard.Run's is: the
// logger travels with it, and a worker sharing a terminal with a TUI
// must be dialed with temporalhandlers.NopLogger.
func RunWorker(ctx context.Context, c client.Client, taskQueue, namespace string) error {
	if taskQueue == "" {
		taskQueue = DefaultTaskQueue
	}
	w := worker.New(c, taskQueue, worker.Options{})
	visibility.Register(w, visibility.New(c, visibility.WithNamespace(namespace)))

	// worker.Run takes a channel rather than a context; the adapter is
	// joined by the deferred close so a Run that returns on its own does
	// not leak the goroutine.
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

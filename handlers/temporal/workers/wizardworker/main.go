// wizardworker runs the wizard application — a workflow that serves its
// own UI, the activities that do every piece of its work, and the markup
// for the screens — as its own process, on a task queue, with nothing
// but stderr for a face.
//
// The application itself is internal/wizard. Nothing in it knows what a
// terminal is: the workflow hands out gooey source because that is the
// renderer its client happens to have, and swapping the markup files
// would drive a different surface entirely.
//
// This is the "workers run elsewhere" deployment, and it is the honest
// one — a worker belongs where the compute is, and a terminal is one
// client among however many. For a demo on one machine, cmd/wizardui
// runs the same registration as a companion and needs no second shell:
//
//	temporal server start-dev --headless   # shell 1
//	go run ./workers/wizardworker          # shell 2   (this program)
//	go run ./cmd/wizardui --with-worker=false
//
//	# or, the two-shell version — the UI brings its own worker:
//	temporal server start-dev --headless   # shell 1
//	go run ./cmd/wizardui                  # shell 2
//
//	temporal workflow show --workflow-id gooey-wizard   # what actually ran
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/WonderForgeLabs/gooey/handlers/temporal/internal/wizard"
	"go.temporal.io/sdk/client"
)

func main() {
	hostPort := envOr("TEMPORAL_ADDRESS", client.DefaultHostPort)
	taskQueue := envOr("GOOEY_TASK_QUEUE", wizard.DefaultTaskQueue)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// No logger override: stderr IS this program's UI, so the SDK's
	// default is what we want. The companion deployment passes
	// NopLogger instead, for the opposite reason.
	c, err := client.Dial(client.Options{HostPort: hostPort})
	if err != nil {
		log.Fatalf("wizardworker: cannot reach the Temporal server at %s: %v\n"+
			"start one with: temporal server start-dev --headless", hostPort, err)
	}
	defer c.Close()

	log.Printf("wizardworker: serving ProvisionWizard and 7 activities on task queue %q via %s",
		taskQueue, hostPort)
	if err := wizard.Run(ctx, c, taskQueue); err != nil {
		log.Fatalln("wizardworker:", err)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

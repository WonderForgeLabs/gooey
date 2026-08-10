// wizardworker is the whole application: a workflow that serves its own
// UI, the activities that do every piece of its work, and the markup for
// the screens — all in one worker binary, none of it in the terminal.
//
// Nothing here knows what a terminal is. The workflow hands out gooey
// source because that is the renderer its client happens to have; swap
// the markup files and the same workflow drives a different surface.
//
//	temporal server start-dev --headless   # shell 1
//	go run ./cmd/wizardworker              # shell 2
//	go run ./cmd/wizardui                  # shell 3
//
//	temporal workflow show --workflow-id gooey-wizard   # what actually ran
package main

import (
	"log"
	"os"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
)

func main() {
	hostPort := envOr("TEMPORAL_ADDRESS", client.DefaultHostPort)
	taskQueue := envOr("GOOEY_TASK_QUEUE", "gooey-wizard")

	c, err := client.Dial(client.Options{HostPort: hostPort})
	if err != nil {
		log.Fatalf("wizardworker: cannot reach the Temporal server at %s: %v\n"+
			"start one with: temporal server start-dev --headless", hostPort, err)
	}
	defer c.Close()

	w := worker.New(c, taskQueue, worker.Options{})
	w.RegisterWorkflow(ProvisionWizard)
	w.RegisterActivity(LoadStageMarkup)
	w.RegisterActivity(DescribeChoice)
	w.RegisterActivity(ValidateRequest)
	w.RegisterActivity(PriceRequest)
	w.RegisterActivity(ReserveCapacity)
	w.RegisterActivity(ProvisionResource)
	w.RegisterActivity(NotifyOwner)

	log.Printf("wizardworker: serving ProvisionWizard and 7 activities on task queue %q via %s",
		taskQueue, hostPort)
	if err := w.Run(worker.InterruptCh()); err != nil {
		log.Fatalln("wizardworker:", err)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

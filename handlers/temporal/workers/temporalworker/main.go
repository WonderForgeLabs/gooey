// temporalworker runs the Slugify activity as its own process, waiting
// on a task queue for whatever asks.
//
// The activity itself is internal/slugify. This binary is the "workers
// run elsewhere" deployment: start it on another machine, or start five
// of them, and cmd/temporaldemo's button fans out across whatever is
// listening. For a demo on one machine the UI can run the same
// registration as a companion instead — `go run ./cmd/temporaldemo`
// does, and needs no second shell.
//
//	temporal server start-dev --headless   # in another shell
//	go run ./workers/temporalworker
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/WonderForgeLabs/gooey/handlers/temporal/internal/slugify"
	"go.temporal.io/sdk/client"
)

func main() {
	hostPort := envOr("TEMPORAL_ADDRESS", client.DefaultHostPort)
	taskQueue := envOr("GOOEY_TASK_QUEUE", slugify.DefaultTaskQueue)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	c, err := client.Dial(client.Options{HostPort: hostPort})
	if err != nil {
		log.Fatalf("temporalworker: cannot reach the Temporal server at %s: %v\n"+
			"start one with: temporal server start-dev --headless", hostPort, err)
	}
	defer c.Close()

	log.Printf("temporalworker: serving Slugify on task queue %q via %s", taskQueue, hostPort)
	if err := slugify.Run(ctx, c, taskQueue); err != nil {
		log.Fatalln("temporalworker:", err)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// visibilityworker serves the temporal-visibility activity pack — the
// full Temporal Visibility API as standalone activities, proto-true —
// on a task queue, for whatever asks: gooey markup through the
// temporal:Activity provider, or any Temporal client in any language.
//
// The activities themselves live in the standalone module
// packs/temporal-visibility, which imports no gooey at all; this binary
// is the gooey repo's deployment of it. It lives in workers/ rather
// than cmd/ because a worker is not a demo — it paints nothing — and
// the demo browser scans only cmd/.
//
//	temporal server start-dev --headless   # in another shell
//	go run ./workers/visibilityworker
package main

import (
	"log"
	"os"

	visibility "github.com/WonderForgeLabs/gooey/packs/temporal-visibility"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
)

// DefaultTaskQueue is where the pack's activities are served unless
// GOOEY_TASK_QUEUE says otherwise.
const DefaultTaskQueue = "gooey-visibility"

func main() {
	hostPort := envOr("TEMPORAL_ADDRESS", client.DefaultHostPort)
	namespace := envOr("TEMPORAL_NAMESPACE", visibility.DefaultNamespace)
	taskQueue := envOr("GOOEY_TASK_QUEUE", DefaultTaskQueue)

	c, err := client.Dial(client.Options{HostPort: hostPort, Namespace: namespace})
	if err != nil {
		log.Fatalf("visibilityworker: cannot reach the Temporal server at %s: %v\n"+
			"start one with: temporal server start-dev --headless", hostPort, err)
	}
	defer c.Close()

	// The worker's namespace is what fills requests that don't name one —
	// the same namespace this client was dialed with.
	w := worker.New(c, taskQueue, worker.Options{})
	visibility.Register(w, visibility.New(c, visibility.WithNamespace(namespace)))

	log.Printf("visibilityworker: serving %d visibility activities on task queue %q (namespace %q) via %s",
		len(visibility.AllNames()), taskQueue, namespace, hostPort)
	if err := w.Run(worker.InterruptCh()); err != nil {
		log.Fatalln("visibilityworker:", err)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

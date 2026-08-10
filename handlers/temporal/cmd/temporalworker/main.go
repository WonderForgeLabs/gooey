// temporalworker is the compute half of the temporaldemo pair: an
// ordinary Temporal worker that registers the Slugify activity and
// waits on a task queue.
//
// Nothing here knows a UI exists. The demo's markup names "Slugify" as
// a string and the worker happens to serve that name on the same queue
// — that indirection is the whole point. Run more copies of this
// binary, anywhere with a route to the server, and the terminal's
// button starts fanning out across them.
//
//	temporal server start-dev --headless   # in another shell
//	go run ./cmd/temporalworker
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"
	"unicode"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
)

// SlugResult is what the activity returns. It is deliberately a struct,
// not a string: the provider does not know the activity's return type
// (markup named it, so Go never saw it), so a composite result comes
// back as JSON — and the worker fields are visible proof in the UI that
// the work happened out here rather than in the terminal.
type SlugResult struct {
	Slug      string `json:"slug"`
	Worker    string `json:"worker"`
	TaskQueue string `json:"taskQueue"`
	Attempt   int32  `json:"attempt"`
	At        string `json:"at"`
}

// Slugify is an ordinary activity function: the same code would run
// unchanged from inside a workflow. Standalone execution is a property
// of how it is *invoked*, not of how it is written.
func Slugify(ctx context.Context, in string) (SlugResult, error) {
	info := activity.GetInfo(ctx)
	host, _ := os.Hostname()

	var b strings.Builder
	prevDash := true // leading dashes are suppressed
	for _, r := range strings.ToLower(in) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			prevDash = false
		case !prevDash:
			b.WriteByte('-')
			prevDash = true
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		return SlugResult{}, fmt.Errorf("nothing sluggable in %q", in)
	}

	return SlugResult{
		Slug:      slug,
		Worker:    host + " pid " + fmt.Sprint(os.Getpid()),
		TaskQueue: info.TaskQueue,
		Attempt:   info.Attempt,
		At:        time.Now().Format("15:04:05"),
	}, nil
}

func main() {
	hostPort := envOr("TEMPORAL_ADDRESS", client.DefaultHostPort)
	taskQueue := envOr("GOOEY_TASK_QUEUE", "gooey-demo")

	c, err := client.Dial(client.Options{HostPort: hostPort})
	if err != nil {
		log.Fatalf("temporalworker: cannot reach the Temporal server at %s: %v\n"+
			"start one with: temporal server start-dev --headless", hostPort, err)
	}
	defer c.Close()

	w := worker.New(c, taskQueue, worker.Options{})
	// Registered under its function name, which is the string the
	// markup asks for.
	w.RegisterActivity(Slugify)

	log.Printf("temporalworker: serving Slugify on task queue %q via %s", taskQueue, hostPort)
	if err := w.Run(worker.InterruptCh()); err != nil {
		log.Fatalln("temporalworker:", err)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

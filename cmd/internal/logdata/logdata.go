// Package logdata is the synthetic log firehose that cmd/logview and
// cmd/markuplog stream.
//
// Those two demos are deliberately near-duplicates: one composes its tree
// as Go literals, the other loads the same viewmodel from markup, and the
// diff between the two files IS the lesson. Everything that carries that
// lesson stays duplicated on purpose. This does not: it is random test
// data with no framework content at all, so having it twice only added
// noise to the diff a reader is trying to read.
//
// Not safe for concurrent use, and deliberately so — Next mutates a
// package counter with no lock. Both demos call it from the UI goroutine
// (logview from its main select, markuplog from an App.Every callback the
// Dispatcher runs on the loop), which is the same confinement the property
// graph itself relies on. A caller that wants to generate lines off the
// main goroutine has to marshal them back anyway.
package logdata

import (
	"fmt"
	"math/rand"
	"strings"
	"time"
)

// Line is one generated log line: a severity and its already-formatted
// text. The demos hold these in a prop.Property[[]Line] and decide for
// themselves how a Level becomes a color.
type Line struct {
	Level, Text string
}

var services = []string{"api-gateway", "auth", "billing", "search", "notifier"}

// count is every line Next has ever produced, which the demos show in
// their stats line to make "arrived" visibly diverge from "rendered"
// while paused.
var count int

// Count reports how many lines Next has produced.
func Count() int { return count }

// Next produces the next line of traffic: mostly INFO request logs, with
// a realistic tail of DEBUG cache chatter, WARN retries and ERROR
// timeouts, so a level filter has something to filter.
func Next() Line {
	count++
	ts := time.Now().Format("15:04:05.000")
	svc := services[rand.Intn(len(services))]
	switch r := rand.Float64(); {
	case r < 0.08:
		return Line{Level: "ERROR", Text: fmt.Sprintf("%s %s: upstream timeout after %dms (attempt %d)", ts, svc, 800+rand.Intn(2200), 1+rand.Intn(3))}
	case r < 0.20:
		return Line{Level: "WARN", Text: fmt.Sprintf("%s %s: retrying request, backoff %dms", ts, svc, 50<<rand.Intn(5))}
	case r < 0.35:
		return Line{Level: "DEBUG", Text: fmt.Sprintf("%s %s: cache %s key=%s", ts, svc, pick("hit", "miss"), randKey())}
	default:
		return Line{Level: "INFO", Text: fmt.Sprintf("%s %s: %s /v1/%s %d %dms", ts, svc, pick("GET", "POST"), pick("users", "orders", "events"), pick(200, 201, 204), 2+rand.Intn(120))}
	}
}

func pick[T any](xs ...T) T { return xs[rand.Intn(len(xs))] }
func randKey() string       { return strings.ToLower(fmt.Sprintf("%x", rand.Intn(1<<24))) }

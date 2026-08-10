package format

import (
	"fmt"
	"time"

	"github.com/WonderForgeLabs/gooey/prop"
)

// The constructors below are the graph-facing surface: each takes typed
// source handles and returns a *prop.Property[string] built with
// prop.NewComputed. The Get inside the computed is what subscribes —
// read-vs-subscription is decided by call site, as everywhere in the
// graph — so a Set on the source dirties exactly this string, and a
// repaint reaches exactly the components reading it. Constructors
// allocate nothing per frame; laziness means the closure runs only when
// something actually reads the formatted value.

// Bytes derives an IEC byte-size string ("1.5 KiB") from an integer
// property. See FormatBytes for the rendering rules.
func Bytes[T Integer](p *prop.Property[T]) *prop.Property[string] {
	return prop.NewComputed(func() string { return FormatBytes(int64(p.Get())) })
}

// BytesSI derives an SI byte-size string ("1.5 kB") from an integer
// property. See FormatBytesSI.
func BytesSI[T Integer](p *prop.Property[T]) *prop.Property[string] {
	return prop.NewComputed(func() string { return FormatBytesSI(int64(p.Get())) })
}

// Count derives a compact count string ("12.3k") from an integer
// property. See FormatCount.
func Count[T Integer](p *prop.Property[T]) *prop.Property[string] {
	return prop.NewComputed(func() string { return FormatCount(int64(p.Get())) })
}

// Percent derives a percentage string from a FRACTION property
// (0.423 → "42.3%") — the same convention as the pipeline grammar's
// progress values, so one float64 source can feed a ProgressBar and a
// Percent label and mean the same thing to both. See FormatPercent.
func Percent(p *prop.Property[float64], digits int) *prop.Property[string] {
	return prop.NewComputed(func() string { return FormatPercent(p.Get(), digits) })
}

// DurationShort derives a short duration string ("1h32m") from a
// duration property. See FormatDurationShort.
func DurationShort(p *prop.Property[time.Duration]) *prop.Property[string] {
	return prop.NewComputed(func() string { return FormatDurationShort(p.Get()) })
}

// RelTime derives a relative-time string ("3 minutes ago") from a time
// property — and it is the one formatter whose output DRIFTS while its
// input holds still. A computed re-evaluates only when a dependency
// changes; nothing in the graph re-runs it because wall time passed.
// So RelTime takes the clock as a second property:
//
//	now := prop.NewSource(time.Now())
//	app.Every(30*time.Second, func() { now.Set(time.Now()) })
//	label := format.RelTime(created, now)
//
// (or a <Timer> in markup whose Tick sets the now source — same
// pattern, composition-owned lifetime). Each tick re-evaluates every
// RelTime reading that clock; pick an interval matching the coarsest
// granularity you display, because prop.Set never compares and a tick
// repaints the label even when the text comes out unchanged.
//
// A nil clock is allowed and honest about its limits: the computed
// falls back to time.Now() AT EVALUATION time, so the string is exact
// whenever t changes and then freezes until the next invalidation.
// That is fine for values whose source updates anyway (a "last
// updated" beside live data) and wrong for a static timestamp — give
// those a clock. This constructor never starts a goroutine: ticking is
// Startable discipline and belongs to components.Timer or app.Every,
// where lifetime is owned.
func RelTime(t *prop.Property[time.Time], clock *prop.Property[time.Time]) *prop.Property[string] {
	return prop.NewComputed(func() string {
		when := t.Get()
		now := time.Now()
		if clock != nil {
			now = clock.Get()
		}
		return FormatRelTime(when, now)
	})
}

// Sprintf is the general escape hatch: one property formatted through
// a fmt verb ("%x", "%06.2f", "q=%q").
func Sprintf[T any](verb string, p *prop.Property[T]) *prop.Property[string] {
	return prop.NewComputed(func() string { return fmt.Sprintf(verb, p.Get()) })
}

// Func is the fully general form: any one-property derivation whose
// rendering this package does not know. The function must be pure over
// its argument — properties it reads become dependencies (call-site
// subscription applies inside f exactly as in any computed).
func Func[T any](p *prop.Property[T], f func(T) string) *prop.Property[string] {
	return prop.NewComputed(func() string { return f(p.Get()) })
}

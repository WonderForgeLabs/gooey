// Package format turns typed values into display strings — byte sizes,
// counts, percentages, durations, relative times — as one
// implementation with two surfaces:
//
//   - Plain functions (FormatBytes, FormatDurationShort, …): pure
//     value→string, usable anywhere, and the exact functions the future
//     markup converter stages (#99, `{{.Size | bytes}}`) will call.
//   - Computed-property constructors (Bytes, DurationShort, …): each
//     wraps a plain function in prop.NewComputed over a typed source
//     handle, so the property graph does the updating — a Set on the
//     source repaints exactly the components reading the formatted
//     string.
//
// This is gooey's answer to XAML's IValueConverter and
// Binding.StringFormat: instead of an interface implemented per
// conversion and dispatched at binding time, a formatter is an ordinary
// computed property built at wiring time. The no-reflection rule holds
// — constructors are generic over the property's value type, and the
// markup surface, when it lands, resolves stage names through a
// registry of these same plain functions.
//
// The semantics match dustin/go-humanize where both cover a case
// (FormatBytes ≙ humanize.IBytes, FormatBytesSI ≙ humanize.Bytes), but
// the implementations are in-repo and dependency-free: every formatter
// is a few dozen lines of stdlib, and a format package earns a place in
// every viewmodel only if importing it costs nothing.
//
// One formatter needs care: relative time drifts while its inputs hold
// still. See RelTime for the clock-property pattern that keeps
// "3 minutes ago" honest.
package format

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// Integer constrains the numeric constructors and plain helpers that
// format whole quantities. Signed only: byte sizes and counts from
// viewmodels are int or int64 in practice, and keeping the constraint
// signed makes negative handling explicit rather than an overflow.
type Integer interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64
}

var (
	iecUnits = []string{"B", "KiB", "MiB", "GiB", "TiB", "PiB", "EiB"}
	siUnits  = []string{"B", "kB", "MB", "GB", "TB", "PB", "EB"}
)

// FormatBytes renders a byte count in IEC units: "512 B", "1.5 KiB",
// "79 MiB". IEC is the default because a TUI lives beside free -h,
// df -h, and htop, which are all binary. FormatBytesSI is the
// 1000-based variant. Semantics match humanize.IBytes, extended to
// negative counts ("-1.5 KiB").
func FormatBytes(n int64) string { return formatBytes(n, 1024, iecUnits) }

// FormatBytesSI renders a byte count in SI units: "1.5 kB", "83 MB".
// Semantics match humanize.Bytes, extended to negative counts.
func FormatBytesSI(n int64) string { return formatBytes(n, 1000, siUnits) }

func formatBytes(n int64, base float64, units []string) string {
	neg := ""
	v := float64(n)
	if v < 0 {
		neg, v = "-", -v
	}
	if v < base {
		return fmt.Sprintf("%s%.0f B", neg, v)
	}
	e := int(math.Floor(math.Log(v) / math.Log(base)))
	if e >= len(units) {
		e = len(units) - 1
	}
	val := v / math.Pow(base, float64(e))
	verb := "%.0f %s"
	if val < 10 {
		verb = "%.1f %s"
	}
	return neg + fmt.Sprintf(verb, val, units[e])
}

var countUnits = []string{"", "k", "M", "B", "T"}

// FormatCount renders a quantity in the compact social-count style:
// "999", "1k", "12.3k", "1.2M", "3.4B". One decimal, trailing ".0"
// trimmed; values that would round to 1000 of a unit promote to the
// next ("999950" is "1M", not "1000.0k"). Beyond T the value stays in
// T ("9223372T" for MaxInt64) — a count that large is not a display
// problem this package can solve.
func FormatCount(n int64) string {
	neg := ""
	v := float64(n)
	if v < 0 {
		neg, v = "-", -v
	}
	e := 0
	// 999.95 is the promotion threshold: anything at or above it
	// rounds to "1000.0" at one decimal, which must carry instead.
	for e < len(countUnits)-1 && v >= 999.95 {
		v /= 1000
		e++
	}
	if e == 0 {
		return neg + strconv.FormatFloat(v, 'f', 0, 64)
	}
	s := strconv.FormatFloat(v, 'f', 1, 64)
	s = strings.TrimSuffix(s, ".0")
	return neg + s + countUnits[e]
}

// FormatPercent renders a FRACTION as a percentage: 0.423 with one
// digit is "42.3%". Fraction-in matches the pipeline grammar's
// canonical progress value (float64 in [0,1]), so a `| progress`
// target and a Percent label agree on what the number means. The input
// is not clamped — "150%" is a legal thing to display. Negative digits
// are treated as 0.
func FormatPercent(f float64, digits int) string {
	if digits < 0 {
		digits = 0
	}
	return strconv.FormatFloat(f*100, 'f', digits, 64) + "%"
}

// FormatDurationShort renders a duration in at most two units, the way
// a human says it: "320ms", "1.5s", "45s", "1m30s", "1h32m", "3d4h".
// Sub-second values pick one unit (ns, µs, ms) with one decimal where
// it earns its place ("1.5µs", "15µs"); at a minute and above the two
// largest units appear and a zero second unit is dropped ("3m", "2h",
// "5d"). Negative durations carry a leading "-".
func FormatDurationShort(d time.Duration) string {
	if d == 0 {
		return "0s"
	}
	neg := ""
	if d < 0 {
		neg = "-"
		if d == math.MinInt64 {
			d++ // -MinInt64 overflows; one ns is far below display precision
		}
		d = -d
	}
	const day = 24 * time.Hour
	switch {
	case d < time.Microsecond:
		return neg + strconv.FormatInt(int64(d), 10) + "ns"
	case d < time.Millisecond:
		return neg + scaled(float64(d)/float64(time.Microsecond)) + "µs"
	case d < time.Second:
		return neg + scaled(float64(d)/float64(time.Millisecond)) + "ms"
	case d < time.Minute:
		return neg + scaled(float64(d)/float64(time.Second)) + "s"
	case d < time.Hour:
		return neg + twoUnit(int64(d/time.Minute), "m", int64(d%time.Minute/time.Second), "s")
	case d < day:
		return neg + twoUnit(int64(d/time.Hour), "h", int64(d%time.Hour/time.Minute), "m")
	default:
		return neg + twoUnit(int64(d/day), "d", int64(d%day/time.Hour), "h")
	}
}

// scaled formats with one decimal, trimming a trailing ".0".
func scaled(v float64) string {
	return strings.TrimSuffix(strconv.FormatFloat(v, 'f', 1, 64), ".0")
}

func twoUnit(major int64, majorUnit string, minor int64, minorUnit string) string {
	s := strconv.FormatInt(major, 10) + majorUnit
	if minor != 0 {
		s += strconv.FormatInt(minor, 10) + minorUnit
	}
	return s
}

// FormatRelTime renders how far t sits from now: "just now",
// "42 seconds ago", "in 3 hours", "2 days ago", "1 month ago". The
// ladder is seconds, minutes, hours, days, months (30 days), years
// (365 days); anything under a second either way is "just now". The
// caller supplies now — this function never reads the wall clock,
// which is what lets the computed constructor (RelTime) route "now"
// through the property graph.
func FormatRelTime(t, now time.Time) string {
	d := now.Sub(t)
	future := d < 0
	if future {
		d = -d
	}
	if d < time.Second {
		return "just now"
	}
	const day = 24 * time.Hour
	var n int64
	var unit string
	switch {
	case d < time.Minute:
		n, unit = int64(d/time.Second), "second"
	case d < time.Hour:
		n, unit = int64(d/time.Minute), "minute"
	case d < day:
		n, unit = int64(d/time.Hour), "hour"
	case d < 30*day:
		n, unit = int64(d/day), "day"
	case d < 365*day:
		n, unit = int64(d/(30*day)), "month"
	default:
		n, unit = int64(d/(365*day)), "year"
	}
	if n != 1 {
		unit += "s"
	}
	if future {
		return fmt.Sprintf("in %d %s", n, unit)
	}
	return fmt.Sprintf("%d %s ago", n, unit)
}

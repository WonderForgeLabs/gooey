package format

import (
	"math"
	"testing"
	"time"
)

func TestFormatBytesIEC(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{0, "0 B"},
		{1, "1 B"},
		{1023, "1023 B"},
		{1024, "1.0 KiB"},
		{1536, "1.5 KiB"},
		{10 * 1024, "10 KiB"},
		{1048576, "1.0 MiB"},
		{82854982, "79 MiB"}, // humanize.IBytes's doc example
		{5 << 30, "5.0 GiB"},
		{3 << 40, "3.0 TiB"},
		{-1, "-1 B"},
		{-1536, "-1.5 KiB"},
		{math.MaxInt64, "8.0 EiB"},
		{math.MinInt64, "-8.0 EiB"},
	}
	for _, c := range cases {
		if got := FormatBytes(c.in); got != c.want {
			t.Errorf("FormatBytes(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestFormatBytesSI(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{0, "0 B"},
		{999, "999 B"},
		{1000, "1.0 kB"},
		{1500, "1.5 kB"},
		{82854982, "83 MB"}, // humanize.Bytes's doc example
		{5_000_000_000, "5.0 GB"},
		{-1500, "-1.5 kB"},
		{math.MaxInt64, "9.2 EB"},
	}
	for _, c := range cases {
		if got := FormatBytesSI(c.in); got != c.want {
			t.Errorf("FormatBytesSI(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestFormatCount(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{0, "0"},
		{7, "7"},
		{999, "999"},
		{1000, "1k"},
		{1500, "1.5k"},
		{12345, "12.3k"},
		{999_949, "999.9k"},
		{999_950, "1M"}, // rounds to 1000.0k → promotes
		{1_234_567, "1.2M"},
		{2_000_000_000, "2B"},
		{1_234_000_000_000, "1.2T"},
		{math.MaxInt64, "9223372T"}, // clamped at T by design
		{-42, "-42"},
		{-12345, "-12.3k"},
	}
	for _, c := range cases {
		if got := FormatCount(c.in); got != c.want {
			t.Errorf("FormatCount(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestFormatPercent(t *testing.T) {
	cases := []struct {
		in     float64
		digits int
		want   string
	}{
		{0, 0, "0%"},
		{0.423, 1, "42.3%"},
		{1, 0, "100%"},
		{0.5, 2, "50.00%"},
		{1.5, 0, "150%"}, // not clamped
		{-0.05, 0, "-5%"},
		{0.423, -3, "42%"}, // negative digits treated as 0
	}
	for _, c := range cases {
		if got := FormatPercent(c.in, c.digits); got != c.want {
			t.Errorf("FormatPercent(%v, %d) = %q, want %q", c.in, c.digits, got, c.want)
		}
	}
}

func TestFormatDurationShort(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want string
	}{
		{0, "0s"},
		{500 * time.Nanosecond, "500ns"},
		{1500 * time.Nanosecond, "1.5µs"},
		{15 * time.Microsecond, "15µs"},
		{320 * time.Millisecond, "320ms"},
		{1500 * time.Millisecond, "1.5s"},
		{45 * time.Second, "45s"},
		{90 * time.Second, "1m30s"},
		{3 * time.Minute, "3m"},
		{92 * time.Minute, "1h32m"},
		{2 * time.Hour, "2h"},
		{76 * time.Hour, "3d4h"},
		{5 * 24 * time.Hour, "5d"},
		{-90 * time.Second, "-1m30s"},
		{-320 * time.Millisecond, "-320ms"},
		{math.MaxInt64, "106751d23h"},
		{math.MinInt64, "-106751d23h"}, // negation overflow handled
	}
	for _, c := range cases {
		if got := FormatDurationShort(c.in); got != c.want {
			t.Errorf("FormatDurationShort(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestFormatRelTime(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		at   time.Time
		want string
	}{
		{now, "just now"},
		{now.Add(-500 * time.Millisecond), "just now"},
		{now.Add(900 * time.Millisecond), "just now"},
		{now.Add(-time.Second), "1 second ago"},
		{now.Add(-42 * time.Second), "42 seconds ago"},
		{now.Add(-time.Minute), "1 minute ago"},
		{now.Add(-5 * time.Minute), "5 minutes ago"},
		{now.Add(-3 * time.Hour), "3 hours ago"},
		{now.Add(-48 * time.Hour), "2 days ago"},
		{now.Add(-45 * 24 * time.Hour), "1 month ago"},
		{now.Add(-400 * 24 * time.Hour), "1 year ago"},
		{now.Add(-800 * 24 * time.Hour), "2 years ago"},
		{now.Add(2 * time.Hour), "in 2 hours"},
		{now.Add(30 * time.Second), "in 30 seconds"},
	}
	for _, c := range cases {
		if got := FormatRelTime(c.at, now); got != c.want {
			t.Errorf("FormatRelTime(%v) = %q, want %q", c.at, got, c.want)
		}
	}
}

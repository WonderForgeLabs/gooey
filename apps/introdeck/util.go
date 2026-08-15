package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

func clamp(n, lo, hi int) int {
	if hi < lo {
		return lo
	}
	if n < lo {
		return lo
	}
	if n > hi {
		return hi
	}
	return n
}

func secs(n int) time.Duration { return time.Duration(n) * time.Second }

func clockOf(d time.Duration) string {
	total := int(d.Seconds())
	return fmt.Sprintf("%d:%02d", total/60, total%60)
}

// wrapText re-wraps prose to width, preserving blank lines as paragraph
// breaks. The script is written as long paragraphs because it is meant to
// be read aloud; the prompter is the only place it needs to be shaped to
// a column.
func wrapText(s string, width int) string {
	if width < 20 {
		width = 20
	}
	var out []string
	for _, para := range strings.Split(s, "\n\n") {
		words := strings.Fields(para)
		if len(words) == 0 {
			out = append(out, "")
			continue
		}
		line := words[0]
		for _, w := range words[1:] {
			if len(line)+1+len(w) > width {
				out = append(out, line)
				line = w
				continue
			}
			line += " " + w
		}
		out = append(out, line, "")
	}
	return strings.TrimRight(strings.Join(out, "\n"), "\n")
}

func itoa(n int) string { return strconv.Itoa(n) }

// lines joins for display. Named for what it produces rather than for
// strings.Join, because every call site is building the content of one
// Text and the separator is never in question.
func lines(ss []string) string { return strings.Join(ss, "\n") }

func join(sep string, parts ...string) string { return strings.Join(parts, sep) }

func atoi(s string) (int, error) { return strconv.Atoi(s) }

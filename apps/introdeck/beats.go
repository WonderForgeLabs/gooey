package main

// The deck's content is NARRATION.md. Not a copy of it, not a table
// generated from it — the file itself, parsed at load time and re-parsed
// on every hot reload. Editing the script edits the deck, which is the
// only arrangement where the words that get spoken and the words on
// screen cannot drift apart.

import (
	"bufio"
	"fmt"
	"io/fs"
	"strconv"
	"strings"
	"time"
)

// Beat is one narration block: what is said, what has to be on screen
// while it is said, and how long it runs.
type Beat struct {
	ID     string // "3.6"
	Part   string // "Part 3 — Building it, from nothing"
	Title  string // "Proving the claim"
	Slide  string // deck slide name, or "(live)" for a real-app segment
	Voice  string // "" for the narrator, "second voice" for Claude
	Dur    time.Duration
	Hold   time.Duration
	Speak  string   // what is said — never on camera
	Lines  []string // what is on camera — never said
	Markup string   // a slide built from real components, not text
}

// Staged reports whether this beat's slide is gooey markup rather than
// lines of text. A staged slide is patched into the running deck through
// the same control-plane call an agent makes over MCP — the deck does not
// have a second, private way to change itself.
func (b Beat) Staged() bool { return b.Markup != "" }

// Live reports whether this beat is performed against the real app
// rather than shown on a deck slide. The parenthesised SLIDE form in the
// script is the marker.
func (b Beat) Live() bool { return strings.HasPrefix(b.Slide, "(") }

// Claude reports whether the second voice speaks this beat. The script
// names both voices explicitly rather than leaving the narrator's blank,
// because "whose voice is this" is the piece's structural spine and a
// beat that forgot to say should read as an error, not as Elan.
func (b Beat) Claude() bool { return strings.EqualFold(b.Voice, "claude") }

// ParseNarration reads the script. Every ```speak fence inside a beat is
// verbatim audio copy; everything else is production metadata and never
// reaches the screen.
//
// The one subtlety: the file's own header documents the extraction awk
// with a ```speak pattern inside it. A fence only opens once a beat
// heading has been seen, which is what keeps the documentation from
// parsing as content.
func ParseNarration(fsys fs.FS, name string) ([]Beat, error) {
	f, err := fsys.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var (
		out    []Beat
		cur    *Beat
		part   string
		fence  string // "speak", "screen", or "" when outside a fence
		body   []string
		lineNo int
	)

	flush := func() {
		if cur == nil {
			return
		}
		out = append(out, *cur)
		cur = nil
	}

	closeFence := func() {
		switch fence {
		case "speak":
			cur.Speak = strings.TrimSpace(strings.Join(body, "\n"))
		case "screen":
			for _, l := range body {
				cur.Lines = append(cur.Lines, strings.TrimRight(l, " "))
			}
		case "gooey":
			cur.Markup = strings.TrimSpace(strings.Join(body, "\n"))
		}
		fence, body = "", nil
	}

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		lineNo++

		if fence != "" {
			if strings.TrimSpace(line) == "```" {
				closeFence()
				continue
			}
			body = append(body, line)
			continue
		}

		switch {
		case strings.HasPrefix(line, "## "):
			flush()
			part = strings.TrimSpace(strings.TrimPrefix(line, "## "))

		case strings.HasPrefix(line, "### "):
			flush()
			id, title := splitHeading(strings.TrimSpace(strings.TrimPrefix(line, "### ")))
			cur = &Beat{ID: id, Title: title, Part: part}

		case cur != nil && strings.Contains(line, "**SLIDE:**"):
			if err := cur.parseMeta(line); err != nil {
				return nil, fmt.Errorf("%s:%d: %w", name, lineNo, err)
			}

		case cur != nil && strings.TrimSpace(line) == "```speak":
			fence = "speak"

		case cur != nil && strings.TrimSpace(line) == "```screen":
			fence = "screen"

		case cur != nil && strings.TrimSpace(line) == "```gooey":
			fence = "gooey"
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	flush()

	if len(out) == 0 {
		return nil, fmt.Errorf("%s: no beats found", name)
	}
	// A beat with no words is a script error, not an empty slide: it
	// means a ```speak fence was mistyped and the words are sitting in
	// the file unspoken.
	for _, b := range out {
		if b.Speak == "" {
			return nil, fmt.Errorf("%s: beat %s has no speak block", name, b.ID)
		}
	}
	return out, nil
}

// splitHeading pulls "3.6 · Proving the claim" apart. The separator is a
// middle dot so that a title may contain a hyphen or an em dash.
func splitHeading(s string) (id, title string) {
	if i := strings.Index(s, " · "); i >= 0 {
		return strings.TrimSpace(s[:i]), strings.TrimSpace(s[i+len(" · "):])
	}
	return s, s
}

// parseMeta reads the metadata line:
//
//	**SLIDE:** `third` · **DURATION:** 1:10 · **HOLD:** 0:05 · **VOICE:** second voice
//
// Fields are order-independent and all but SLIDE are optional.
func (b *Beat) parseMeta(line string) error {
	for _, field := range strings.Split(line, " · ") {
		key, val, ok := strings.Cut(strings.TrimSpace(field), ":**")
		if !ok {
			continue
		}
		key = strings.TrimPrefix(strings.TrimSpace(key), "**")
		val = strings.Trim(strings.TrimSpace(val), "`*_ ")

		switch strings.ToUpper(key) {
		case "SLIDE":
			b.Slide = val
		case "VOICE":
			b.Voice = val
		case "DURATION":
			d, err := parseClock(val)
			if err != nil {
				return fmt.Errorf("DURATION: %w", err)
			}
			b.Dur = d
		case "HOLD":
			// A hold may carry a parenthesised stage direction:
			// "0:08 *(app launches, black screen)*". Only the clock
			// is machine-readable; the direction is for whoever is
			// holding the camera.
			clock := val
			if i := strings.IndexAny(clock, " ("); i >= 0 {
				clock = clock[:i]
			}
			d, err := parseClock(clock)
			if err != nil {
				return fmt.Errorf("HOLD: %w", err)
			}
			b.Hold = d
		}
	}
	if b.Slide == "" {
		return fmt.Errorf("SLIDE is empty")
	}
	return nil
}

// parseClock reads "1:15" as 75 seconds. Deliberately strict: a script
// typo here would silently shorten a beat, and the whole point of the
// timings is that they are trustworthy.
func parseClock(s string) (time.Duration, error) {
	mm, ss, ok := strings.Cut(strings.TrimSpace(s), ":")
	if !ok {
		return 0, fmt.Errorf("want m:ss, got %q", s)
	}
	m, err := strconv.Atoi(mm)
	if err != nil {
		return 0, fmt.Errorf("bad minutes in %q", s)
	}
	sec, err := strconv.Atoi(ss)
	if err != nil {
		return 0, fmt.Errorf("bad seconds in %q", s)
	}
	if sec < 0 || sec > 59 || m < 0 {
		return 0, fmt.Errorf("out of range: %q", s)
	}
	return time.Duration(m)*time.Minute + time.Duration(sec)*time.Second, nil
}

// Runtime totals the spoken time and the holds separately, because they
// are different things in an edit: speech is the audio track's length,
// holds are footage with no narration over it.
func Runtime(beats []Beat) (speech, holds time.Duration) {
	for _, b := range beats {
		speech += b.Dur
		holds += b.Hold
	}
	return speech, holds
}

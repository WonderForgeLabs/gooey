package main

// Scripted guests: a <Terminal> that types into itself.
//
// The chronology beats each host a real program, and most of them have
// to *do* something without anyone touching the keyboard — a shell
// running a few commands, an editor being edited, a filter being fed. A
// slide that needs the presenter to remember a keystroke is a slide that
// will be wrong on camera once.
//
// So a Terminal may carry Script="era/greenscreen.keys", and the file is
// a schedule:
//
//	# after 1.2s, type this and press return
//	1.2  ls -l\n
//	2.0  date\n
//	1.0  \e:q!\n
//
// One line, one beat: seconds to wait FIRST, then the bytes to send.
// Escapes are the two that matter for a terminal — \n is carriage return
// (what the key sends, not what the file holds) and \e is escape — plus
// \t, \\ and \s for a trailing space that an editor would otherwise
// strip.
//
// # Why the delays are relative
//
// Absolute timestamps read better in a file and are worse to edit: retime
// one beat and every line after it is wrong. Relative delays mean the
// only thing a change touches is the thing that changed, which is the
// same reason the narration script holds durations rather than a running
// clock.
//
// # Why this is not a recording
//
// A recording would be simpler and would be a lie. Everything on screen
// is a real program on a real pty producing real output — the script
// only supplies the keystrokes a person would have typed. When the guest
// is slow, the slide is slow. When the guest fails, the slide shows the
// failure, which has already caught two missing binaries.

import (
	"bufio"
	"fmt"
	"io/fs"
	"strconv"
	"strings"
	"time"
)

// Step is one scheduled keystroke burst.
type Step struct {
	After time.Duration
	Send  []byte
}

// LoadScript reads a schedule. Errors are load errors on purpose: a
// mistyped delay in a slide should stop the deck opening, not surface as
// a guest that sits there during the take.
func LoadScript(fsys fs.FS, name string) ([]Step, error) {
	f, err := fsys.Open(name)
	if err != nil {
		return nil, fmt.Errorf("script %s: %w", name, err)
	}
	defer f.Close()

	var out []Step
	sc := bufio.NewScanner(f)
	for line := 1; sc.Scan(); line++ {
		raw := sc.Text()
		if t := strings.TrimSpace(raw); t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		// Split on the FIRST run of whitespace only: everything after it
		// is payload, including further spaces, which is the whole point
		// of a line like `1.0  echo hello world\n`.
		delay, rest, ok := strings.Cut(strings.TrimLeft(raw, " \t"), " ")
		if !ok {
			delay, rest, ok = strings.Cut(strings.TrimLeft(raw, " \t"), "\t")
		}
		if !ok {
			return nil, fmt.Errorf("script %s:%d: want `<seconds> <keys>`, got %q", name, line, raw)
		}
		secs, err := strconv.ParseFloat(delay, 64)
		if err != nil || secs < 0 {
			return nil, fmt.Errorf("script %s:%d: %q is not a delay in seconds", name, line, delay)
		}
		out = append(out, Step{
			After: time.Duration(secs * float64(time.Second)),
			Send:  []byte(unescape(strings.TrimLeft(rest, " \t"))),
		})
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("script %s: %w", name, err)
	}
	return out, nil
}

// unescape expands the handful of sequences a keystroke schedule needs.
//
// \n becomes CARRIAGE RETURN, not newline. That is not a typo: a line
// discipline in canonical mode ends a line on \r, which is what the
// Enter key actually sends, and a guest fed \n will sit there looking
// broken. It is the single most common way to get this wrong.
func unescape(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 >= len(s) {
			b.WriteByte(s[i])
			continue
		}
		i++
		switch s[i] {
		case 'n':
			b.WriteByte('\r')
		case 'r':
			b.WriteByte('\r')
		case 't':
			b.WriteByte('\t')
		case 'e':
			b.WriteByte(0x1b)
		case 's':
			b.WriteByte(' ')
		case '\\':
			b.WriteByte('\\')
		default:
			// Unknown escapes pass through with the backslash intact
			// rather than being swallowed: a script that meant to send a
			// literal backslash should get one, and a typo should be
			// visible on screen instead of silently vanishing.
			b.WriteByte('\\')
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

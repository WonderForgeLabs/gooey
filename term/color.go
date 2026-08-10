package term

import (
	"os"
	"strconv"
	"strings"

	"github.com/WonderForgeLabs/gooey/render"
)

// Color depth detection is a LADDER, not a single signal, because no one
// signal is both available and trustworthy everywhere.
//
// The rungs, most authoritative first:
//
//  1. COLORTERM=truecolor|24bit — an explicit statement, by the terminal
//     or by the user's shell profile. Nothing outranks it.
//  2. TERM ending in -direct — terminfo's direct-color entries
//     (xterm-direct, *-direct2). Also explicit configuration.
//  3. An XTGETTCAP answer of "yes" to the RGB or Tc capability — the
//     terminal itself, asked directly.
//  4. Known-truecolor terminals identified by their own environment
//     variables (WT_SESSION, KITTY_WINDOW_ID, TERM_PROGRAM, …).
//  5. TERM containing 256color → 256.
//  6. Otherwise 16, the only safe claim.
//
// Rung 4 exists because rung 1 fails constantly in practice: COLORTERM
// is set by the terminal for its own child shell, and anything that
// re-enters from outside that environment loses it — WSL, tmux, ssh,
// and multiplexers generally. Windows Terminal is the case that
// motivated this: it renders 24-bit color, propagates WT_SESSION into
// WSL through WSLENV, and does not set COLORTERM there.
//
// A "no" from rung 3 deliberately does NOT veto rung 4. XTGETTCAP is
// unevenly implemented, and a terminal that answers "unsupported" for a
// capability it merely does not describe is weaker evidence than its own
// unambiguous identity variable. A positive answer promotes; a negative
// one just declines to promote.

// capAnswer is a terminal's reply about a capability: it may not have
// answered at all, which is different from answering no.
type capAnswer uint8

const (
	capUnknown capAnswer = iota
	capYes
	capNo
)

// DetectColorDepth reads the color depth from the environment alone. It
// needs no tty, so it is the cheap answer available to any process;
// Screen.Detect runs the same ladder with the terminal query filled in.
func DetectColorDepth() render.ColorDepth {
	return colorDepthFrom(osEnv, capUnknown)
}

func colorDepthFrom(env func(string) string, rgb capAnswer) render.ColorDepth {
	// 1. Explicit declaration.
	switch strings.ToLower(strings.TrimSpace(env("COLORTERM"))) {
	case "truecolor", "24bit":
		return render.TrueColor
	}

	term := strings.ToLower(env("TERM"))

	// 2. terminfo direct-color entries.
	if strings.Contains(term, "direct") {
		return render.TrueColor
	}

	// 3. The terminal answered the capability query.
	if rgb == capYes {
		return render.TrueColor
	}

	// 4. Terminals that identify themselves and are known to be 24-bit.
	if knownTrueColorTerminal(env) {
		return render.TrueColor
	}

	// 5/6. Fall back to what TERM claims.
	if strings.Contains(term, "256color") {
		return render.Color256
	}
	return render.Color16
}

// knownTrueColorTerminal recognizes terminals by the environment
// variables they set for themselves. Every entry here is a terminal that
// has rendered 24-bit color for years; the list is deliberately a
// whitelist of identities rather than a guess from TERM, which is why
// Apple_Terminal — which sets TERM_PROGRAM but is genuinely 256-color —
// is absent rather than special-cased.
func knownTrueColorTerminal(env func(string) string) bool {
	for _, v := range []string{
		"WT_SESSION",       // Windows Terminal (propagated into WSL via WSLENV)
		"KITTY_WINDOW_ID",  // kitty
		"ALACRITTY_SOCKET", // Alacritty
		"ALACRITTY_WINDOW_ID",
		"KONSOLE_VERSION",    // Konsole
		"WEZTERM_EXECUTABLE", // WezTerm
		"GHOSTTY_RESOURCES_DIR",
	} {
		if env(v) != "" {
			return true
		}
	}
	switch env("TERM_PROGRAM") {
	case "iTerm.app", "WezTerm", "vscode", "Hyper", "ghostty", "Tabby", "rio":
		return true
	}
	if env("LC_TERMINAL") == "iTerm2" {
		return true
	}
	// VTE-based terminals (GNOME Terminal, Tilix, …) gained 24-bit color
	// in 0.36; VTE_VERSION is that version as a packed integer.
	if v := env("VTE_VERSION"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 3600 {
			return true
		}
	}
	return false
}

// XTGETTCAP: query terminfo capabilities by name, hex-encoded.
//
//	query    ESC P + q <hex-name>[;<hex-name>…] ESC \
//	success  ESC P 1 + r <hex-name>[=<hex-value>] ESC \
//	failure  ESC P 0 + r <hex-name> ESC \
//
// "RGB" and "Tc" are the two capability names terminals use to advertise
// direct color; different terminals describe one or the other, so both
// are asked and either answer counts.
const (
	hexRGB = "524742" // "RGB"
	hexTc  = "5463"   // "Tc"

	xtgettcapQuery = "\x1bP+q" + hexRGB + ";" + hexTc + "\x1b\\"
)

// parseXTGETTCAP reads a terminal's capability reply out of a response
// blob that also contains answers to other queries.
//
// Only a POSITIVE answer for RGB or Tc counts as yes. A negative answer
// for one name while the other is missing is reported as no, but callers
// treat no as "did not promote" rather than as a veto — see the ladder.
func parseXTGETTCAP(resp string) capAnswer {
	up := strings.ToUpper(resp)
	answer := capUnknown
	for i := 0; ; {
		j := strings.Index(up[i:], "\x1bP")
		if j < 0 {
			return answer
		}
		i += j + 2
		// The status digit precedes "+R".
		k := strings.Index(up[i:], "+R")
		if k < 0 {
			return answer
		}
		status := strings.TrimSpace(up[i : i+k])
		body := up[i+k+2:]
		// The payload ends at the string terminator (ESC \ or the 8-bit ST).
		if e := strings.IndexAny(body, "\x1b\x9c"); e >= 0 {
			body = body[:e]
		}
		if strings.Contains(body, hexRGB) || strings.Contains(body, strings.ToUpper(hexTc)) {
			if status == "1" {
				return capYes // a positive answer is decisive
			}
			answer = capNo
		}
		i += k + 2
	}
}

// osEnv is the real environment lookup; tests substitute their own.
func osEnv(k string) string { return os.Getenv(k) }

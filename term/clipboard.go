package term

import (
	"encoding/base64"
	"fmt"
	"os"
)

// The system clipboard, from inside a terminal.
//
// There is exactly one mechanism — OSC 52 — and it is ASYMMETRIC in a
// way that decides the whole feature:
//
//	WRITE  ESC ] 52 ; c ; <base64> ST      broadly supported
//	READ   ESC ] 52 ; c ; ? ST             refused by most terminals
//
// The refusal is deliberate and correct on the terminals' part: a
// readable clipboard means any program that can write a byte to your tty
// can exfiltrate whatever you last copied, including the password you
// pasted into something else. xterm gates it behind allowWindowOps,
// kitty behind clipboard_control read-clipboard, VTE (GNOME Terminal,
// and everything built on it) refuses outright with no setting, and
// iTerm2 prompts. A terminal that refuses does so SILENTLY: the query
// goes out and no reply ever comes, which is indistinguishable from a
// reply that is merely slow.
//
// So this file implements the WRITE half only, and there is deliberately
// no read function here at all. A `ReadClipboard() (string, bool)` would
// be worse than nothing: it would have to block on a timeout, and every
// caller would treat the timeout as "the clipboard is empty" — a silent
// wrong answer where an absent API is a compile error. The absence IS
// the documentation.
//
// What replaces the read is bracketed paste (EnablePaste, and
// input/paste.go). Pasting INTO an app does not require reading the
// clipboard at all: the user presses their terminal's own paste key, the
// terminal writes the clipboard's bytes to our tty, and mode 2004 tells
// us where they start and stop. The terminal does the privileged read;
// we just recognise the result. That path needs no permission, works on
// every terminal that implements 2004, and is the one to point a user at
// when they ask why the app cannot paste on its own.

// ClipboardLimit caps the base64 payload of a single OSC 52 write.
//
// It is NOT a protocol constant — OSC 52 defines no maximum — but every
// implementation has a buffer, and the failure when you exceed it is the
// bad kind: the terminal drops the sequence and says nothing, so the
// clipboard silently keeps its old contents while the app reports a
// successful copy. 74994 is the figure xterm's own limit works out to
// and the one tmux adopted, so it is the widest value that is safe
// across the terminals people actually run.
//
// ClipboardSeq refuses past it rather than truncating. A truncated copy
// is the same silent-wrong-answer shape: the user gets 74KB of a 100KB
// document with no indication that the tail is missing.
const ClipboardLimit = 74994

// ClipboardSeq is the OSC 52 sequence that puts text on the system
// clipboard, or an error explaining why no sequence can be built.
//
// Split out from SetClipboard so the encoding is testable without a tty:
// everything that can go wrong is decided here, and SetClipboard is only
// the write.
//
// The terminator is ST (ESC \) rather than BEL. Both are accepted by
// xterm and by everything modern; ST is what the standard specifies and
// what the kitty graphics escapes in this package already use, so the
// two agree.
//
// "c" is the clipboard selection. Not "p" (primary) and not the empty
// string, which means "both c and p on xterm and c alone elsewhere" —
// an ambiguity nobody needs.
func ClipboardSeq(text string) (string, error) {
	if text == "" {
		// Refused rather than sent. OSC 52 with an empty payload CLEARS
		// the clipboard on most terminals, so a caller that copied an
		// empty selection by accident would destroy whatever the user
		// had there — a data loss with no undo, caused by a no-op.
		return "", fmt.Errorf("clipboard: nothing to copy (an empty OSC 52 write would CLEAR the clipboard)")
	}
	enc := base64.StdEncoding.EncodeToString([]byte(text))
	if len(enc) > ClipboardLimit {
		return "", fmt.Errorf("clipboard: %d bytes is too large to copy (the terminal accepts about %d bytes of base64, and drops a larger sequence without reporting it)",
			len(text), ClipboardLimit)
	}
	return "\x1b]52;c;" + enc + "\x1b\\", nil
}

// SetClipboard writes text to the system clipboard through the terminal.
//
// UI-goroutine only, like every other escape this package emits: it
// writes to the same tty the frame flush writes to, and two goroutines
// interleaving escapes produce neither.
//
// A nil error means the sequence was WRITTEN, not that the clipboard
// changed — OSC 52 has no acknowledgement, and asking for one would mean
// the read half this file explains it does not have. See
// ClipboardCaveat for the environments where a written sequence is
// most likely not to arrive, which is as close to honest as this can be
// made.
func (s *Screen) SetClipboard(text string) error {
	seq, err := ClipboardSeq(text)
	if err != nil {
		return err
	}
	_, err = s.tty.WriteString(seq)
	return err
}

// ClipboardCaveat names the reason a copy might not land, or "" when
// there is no known one.
//
// This exists because the two multiplexers swallow OSC 52 by DEFAULT,
// and a user inside one sees a confirmation and an unchanged clipboard
// with nothing anywhere to connect the two. Environment detection is a
// weak signal — TMUX being set does not prove set-clipboard is off — so
// the string is phrased as a condition to check, not as a failure.
// Saying "if the paste did not arrive, here is why" is honest; saying
// "copy failed" would be a guess, and suppressing it entirely is the
// silent failure.
func ClipboardCaveat() string {
	if os.Getenv("TMUX") != "" {
		return "inside tmux: needs `set -g set-clipboard on`"
	}
	// STY, not TERM. GNU screen sets STY for every session it owns,
	// whereas TERM=screen-256color is what TMUX sets too — keying off
	// TERM would name the wrong multiplexer for most of the people who
	// saw the message.
	if os.Getenv("STY") != "" {
		return "inside GNU screen, which does not forward OSC 52"
	}
	return ""
}

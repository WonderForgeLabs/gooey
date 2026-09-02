package term

import (
	"testing"
	"time"

	"github.com/WonderForgeLabs/gooey/input"
)

// The decoder going deaf WITHOUT dying — the failure DecoderDone cannot
// see, and the dual of TestDecoderDeathIsObservable next door.
//
// That test's comment describes a live wysiwyg session as "display
// updating, MCP-injected events dispatching normally, keyboard
// completely dead", and fixes the cause it found: a decoder that
// returned. This is the same symptom reached the other way. The decoder
// is alive, its goroutine is parked on the tty exactly as it should be,
// DecoderDone never fires, and the app still never sees another key —
// because DecodeEvents' drain loop stops on a Decode that consumes
// nothing, and the buffer it stopped on can never be resolved by another
// byte. Every keystroke after it queues behind it forever.
//
// Testing it HERE rather than in input is the whole point. input's own
// test pins the decoding contract; only the loop can show that violating
// it strands live input, and only a real tty makes the loop the thing
// under test.
func TestEscBeforeAMouseReportDoesNotStrandTheDecoder(t *testing.T) {
	master, slave := openPTY(t)
	s := FromFile(slave)
	if err := s.Raw(); err != nil {
		t.Fatalf("raw: %v", err)
	}
	evs := s.Events(16)

	// Prove the decoder lives before asking whether it went deaf, for
	// the same reason the death test does: a decoder that never started
	// would pass every assertion below by never contradicting one.
	if _, err := master.Write([]byte("a")); err != nil {
		t.Fatalf("write to master: %v", err)
	}
	if ev := next(t, evs, "the decoder never delivered a keystroke, so this test "+
		"is measuring a decoder that never lived"); !ev.IsKey() || ev.Key.Rune != 'a' {
		t.Fatalf("got %#v, want the 'a' we typed", ev)
	}

	// One write, because that is what makes it one read: an Esc and a
	// mouse report reaching the decoder together is ordinary — press
	// Escape and click, or click twice while a dangling Esc has not yet
	// timed out. The report decodes perfectly. It simply is not a KEY,
	// and that alone used to strand the buffer.
	if _, err := master.Write([]byte("\x1b\x1b[<0;10;5M")); err != nil {
		t.Fatalf("write to master: %v", err)
	}
	// Then an ordinary keystroke, which is the actual assertion: it is
	// behind the stranding sequence in the same buffer, so it arrives
	// only if the decoder got past it.
	if _, err := master.Write([]byte("z")); err != nil {
		t.Fatalf("write to master: %v", err)
	}

	deadline := time.After(3 * time.Second)
	for {
		select {
		case ev := <-evs:
			if ev.IsKey() && ev.Key.Rune == 'z' {
				return // got past it
			}
		case <-deadline:
			t.Fatal("the 'z' typed after an Esc-then-mouse-report never arrived: " +
				"the decoder is alive and reading the tty, and every keystroke is " +
				"stranded behind a buffer its drain loop refuses to advance past. " +
				"DecoderDone cannot see this — the goroutine never returns.")
		}
	}
}

// The SECOND way to go deaf without dying, and the one that also burns a
// wakeup every 40ms while it does it.
//
// ESC [ 2 is a strict prefix of the bracketed-paste marker ESC [ 200 ~,
// so input.Decode holds it under idle rather than resolving it to Esc —
// on the reasoning that the rest of a real marker is already on the wire.
// That reasoning covers a paste and not a PERSON, and a person pressing
// Escape and then typing `[` and `2` produces the same three bytes with
// nothing behind them. The buffer then never shrinks, so DecodeEvents
// re-arms its escape timer on every iteration for the rest of the
// process's life, and the next unrelated keystroke is appended behind
// the prefix and absorbed into the CSI parse — ESC [ 2 z is one unmapped
// four-byte sequence and no event at all.
//
// PasteMarkerGrace is the fix: the grace is bounded rather than removed,
// and the pass after it withdraws the exception (input.DecodeFinal).
// Here rather than in input for the reason the test above gives — only
// the loop can show that a decoding contract stranding live input, and
// only a real tty makes the loop the thing under test. In particular the
// GAP is the fixture: these bytes have to arrive in their own read and
// then nothing for two timeouts, which is exactly what a keyboard does
// and what a single Write in one test cannot fake.
func TestATypedPasteMarkerPrefixDoesNotStrandTheDecoder(t *testing.T) {
	master, slave := openPTY(t)
	s := FromFile(slave)
	if err := s.Raw(); err != nil {
		t.Fatalf("raw: %v", err)
	}
	evs := s.Events(16)

	if _, err := master.Write([]byte("a")); err != nil {
		t.Fatalf("write to master: %v", err)
	}
	if ev := next(t, evs, "the decoder never delivered a keystroke, so this test "+
		"is measuring a decoder that never lived"); !ev.IsKey() || ev.Key.Rune != 'a' {
		t.Fatalf("got %#v, want the 'a' we typed", ev)
	}

	if _, err := master.Write([]byte("\x1b[2")); err != nil {
		t.Fatalf("write to master: %v", err)
	}
	// Longer than PasteMarkerGrace timeouts, and generously so: the
	// assertion is about what happens AFTER the grace expires, and a
	// margin here cannot make a broken decoder pass.
	time.Sleep((PasteMarkerGrace + 2) * EscTimeout)
	if _, err := master.Write([]byte("z")); err != nil {
		t.Fatalf("write to master: %v", err)
	}

	// Esc first — the key the user actually pressed, which the grace
	// swallowed — then the two bytes they typed after it as themselves,
	// then the keystroke that proves the buffer drained.
	var got []rune
	sawEsc := false
	deadline := time.After(3 * time.Second)
	for {
		select {
		case ev := <-evs:
			if !ev.IsKey() {
				continue
			}
			if ev.Key.Key == input.KeyEsc {
				sawEsc = true
				continue
			}
			got = append(got, ev.Key.Rune)
			if ev.Key.Rune == 'z' {
				if !sawEsc {
					t.Errorf("the 'z' arrived but the Esc never did: the key that "+
						"started the sequence was consumed by the paste grace "+
						"instead of being delivered when it expired (saw %q)", string(got))
				}
				if string(got) != "[2z" {
					t.Errorf("keys after the Esc were %q, want \"[2z\" — the bytes "+
						"the user typed must arrive as themselves, not be "+
						"swallowed with the escape", string(got))
				}
				return
			}
		case <-deadline:
			t.Fatalf("the 'z' typed after ESC [ 2 never arrived (saw esc=%v, keys %q): "+
				"the decoder is alive and reading the tty, and every keystroke is "+
				"stranded behind three bytes its drain loop refuses to advance past. "+
				"DecoderDone cannot see this — the goroutine never returns.",
				sawEsc, string(got))
		}
	}
}

func next(t *testing.T, evs <-chan input.Event, msg string) input.Event {
	t.Helper()
	select {
	case ev := <-evs:
		return ev
	case <-time.After(2 * time.Second):
		t.Fatal(msg)
	}
	return input.Event{}
}

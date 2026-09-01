package input

import (
	"runtime"
	"strings"
	"testing"
)

// decodeAll runs the decoder to exhaustion the way term.DecodeEvents
// does, one byte-slice at a time, so a test asserts on the SEQUENCE of
// events rather than on one call.
func decodeAll(t *testing.T, b []byte, idle bool) []Event {
	t.Helper()
	var out []Event
	for len(b) > 0 {
		ev, n, ok := Decode(b, idle)
		if n == 0 && !ok {
			break // incomplete
		}
		b = b[n:]
		if ok {
			out = append(out, ev)
		}
	}
	return out
}

func TestBracketedPasteIsOneEvent(t *testing.T) {
	// THE defect bracketed paste exists to prevent: three lines of
	// markup pasted into a designer. Without the mode, this is 40-odd
	// key events of which two are Enter, and Enter means "activate".
	body := "<VStack>\n  <Text>hi</Text>\n</VStack>"
	evs := decodeAll(t, []byte(pasteStart+body+pasteEnd), false)

	if len(evs) != 1 {
		t.Fatalf("a paste must decode to ONE event, got %d: %+v", len(evs), evs)
	}
	if !evs[0].IsPaste() {
		t.Fatalf("kind = %v, want EventPaste", evs[0].Kind)
	}
	if evs[0].Paste.Text != body {
		t.Errorf("payload = %q, want %q", evs[0].Paste.Text, body)
	}
	// The discrimination half: a paste must not also read as a key.
	if evs[0].IsKey() || evs[0].IsMouse() {
		t.Error("a paste reports itself as a key or a mouse event")
	}
}

func TestPastePayloadIsVerbatim(t *testing.T) {
	// No trimming, no newline normalisation, no filtering — the policy
	// belongs to the consumer, and a decoder that TrimSpace'd here would
	// silently change markup whose body has deliberate leading spaces
	// (the exact bug markup.BodyText exists to stop restating).
	body := "  leading\ttab\r\nCRLF  "
	evs := decodeAll(t, []byte(pasteStart+body+pasteEnd), false)
	if len(evs) != 1 || evs[0].Paste.Text != body {
		t.Fatalf("payload = %q, want it byte-for-byte", evs[0].Paste.Text)
	}
}

func TestPasteKeepsTheKeysAroundItInOrder(t *testing.T) {
	// The ordering invariant, asserted rather than asserted-about: keys
	// and pastes share one stream, so the key before and the key after
	// must arrive on either side of the paste.
	in := "a" + pasteStart + "XY" + pasteEnd + "b"
	evs := decodeAll(t, []byte(in), false)
	if len(evs) != 3 {
		t.Fatalf("got %d events, want key,paste,key: %+v", len(evs), evs)
	}
	if !evs[0].IsKey() || evs[0].Key.Rune != 'a' {
		t.Errorf("first = %+v, want key 'a'", evs[0])
	}
	if !evs[1].IsPaste() || evs[1].Paste.Text != "XY" {
		t.Errorf("second = %+v, want paste XY", evs[1])
	}
	if !evs[2].IsKey() || evs[2].Key.Rune != 'b' {
		t.Errorf("third = %+v, want key 'b'", evs[2])
	}
}

func TestUnterminatedPasteWaitsEvenWhenIdle(t *testing.T) {
	// THE BOUNDARY. A large paste crosses many 128-byte reads and will
	// routinely outlast the 40ms escape timeout, so idle must NOT
	// resolve an open bracket the way it resolves a truncated CSI into
	// Esc. If this regresses, a big paste is delivered as a stray Esc
	// followed by its own payload as keystrokes — which is precisely the
	// failure the mode was turned on to prevent, now with an extra Esc.
	partial := []byte(pasteStart + "half a document")
	for _, idle := range []bool{false, true} {
		ev, n, ok := Decode(partial, idle)
		if ok || n != 0 {
			t.Fatalf("idle=%v: got (%+v, n=%d, ok=%v), want (_, 0, false) = wait for more",
				idle, ev, n, ok)
		}
	}
	// And it completes once the terminator arrives.
	evs := decodeAll(t, append(partial, []byte(pasteEnd)...), true)
	if len(evs) != 1 || evs[0].Paste.Text != "half a document" {
		t.Fatalf("completed paste = %+v", evs)
	}
}

// TestASplitOpeningMarkerIsNotTheEscapeKey is the other half of the
// boundary above, and it was the half that did not hold.
//
// TestUnterminatedPasteWaitsEvenWhenIdle covers a COMPLETE bracket with
// an incomplete payload. The bracket itself is six bytes and can also
// straddle a read, and there the truncated-CSI rule ran first: every
// prefix resolved to Esc, consuming one byte and leaving "[200~" to
// decode as five ordinary keys with the whole payload behind it as
// keystrokes. That is exactly the burst mode 2004 is turned on to
// prevent, and it needs only a 40ms stall inside six bytes — an
// ordinary event on a laggy link. Found in the review of #391 (issue
// #419).
//
// THE CUT STARTS AT 3 ON PURPOSE. "\x1b" and "\x1b[" are genuinely
// ambiguous with the Esc key, and no amount of context resolves them —
// idle exists for exactly that ambiguity and must go on answering Esc.
// From the third byte the buffer is ESC [ followed by digits, which
// nothing but a CSI spells, so the only readings are a truncated key
// sequence or a split marker. That boundary is also what keeps the
// exhaustive 1- and 2-byte liveness check in decodeidle_test.go intact,
// and the assertion below pins it from the other side.
func TestASplitOpeningMarkerIsNotTheEscapeKey(t *testing.T) {
	for cut := 1; cut < 3; cut++ {
		ev, n, ok := Decode([]byte(pasteStart[:cut]), true)
		if !ok || n != 1 || ev.Key.Key != KeyEsc {
			t.Errorf("a %d-byte prefix of the paste marker decoded to (%+v, n=%d, ok=%v) "+
				"— it is indistinguishable from the Esc key and must stay Esc",
				cut, ev, n, ok)
		}
	}
	for cut := 3; cut < len(pasteStart); cut++ {
		ev, n, ok := Decode([]byte(pasteStart[:cut]), true)
		if ok || n != 0 {
			t.Errorf("the marker split after %d bytes (%q) decoded to (%+v, n=%d, ok=%v), "+
				"want (_, 0, false) = wait for more. Resolving it delivers the paste "+
				"as keystrokes", cut, pasteStart[:cut], ev, n, ok)
		}
	}

	// The consequence, at the level the user meets it: the drain loop
	// runs on the prefix, goes idle, and the rest arrives. One paste,
	// and NOT a stray Esc followed by the payload as keys.
	for cut := 3; cut < len(pasteStart); cut++ {
		full := pasteStart + "rm -rf /\n" + pasteEnd
		pend := []byte(full[:cut])
		var got []Event
		drain := func(idle bool) {
			for len(pend) > 0 {
				ev, n, ok := Decode(pend, idle)
				if n == 0 && !ok {
					return
				}
				pend = pend[n:]
				if ok {
					got = append(got, ev)
				}
			}
		}
		drain(false)
		drain(true) // the escape timeout fires mid-marker
		pend = append(pend, full[cut:]...)
		drain(false)

		if len(got) != 1 || !got[0].IsPaste() {
			t.Fatalf("cut %d: got %d events %+v, want exactly one paste", cut, len(got), got)
		}
		if got[0].Paste.Text != "rm -rf /\n" {
			t.Errorf("cut %d: payload %q", cut, got[0].Paste.Text)
		}
	}
}

func TestPasteArrivingInChunksDecodesOnce(t *testing.T) {
	// The way it actually arrives: DecodeEvents reads 128 bytes at a
	// time and appends to a pending buffer, calling Decode after each.
	body := strings.Repeat("abcdefgh", 60) // 480 bytes, four reads
	full := []byte(pasteStart + body + pasteEnd)

	var pend []byte
	var got []Event
	for i := 0; i < len(full); i += 128 {
		pend = append(pend, full[i:min(i+128, len(full))]...)
		for len(pend) > 0 {
			ev, n, ok := Decode(pend, false)
			if n == 0 && !ok {
				break
			}
			pend = pend[n:]
			if ok {
				got = append(got, ev)
			}
		}
	}
	if len(got) != 1 {
		t.Fatalf("chunked paste decoded to %d events, want 1", len(got))
	}
	if got[0].Paste.Text != body {
		t.Errorf("payload length %d, want %d", len(got[0].Paste.Text), len(body))
	}
}

// TestDrainingALargePasteDoesNotCopyItPerRead is a MEMORY assertion
// rather than a timing one, on purpose.
//
// The defect was a full copy of the pending buffer on every read while a
// paste was open — strings.Index(string(rest), …) — so a 1MB paste
// arriving in 128-byte chunks allocated gigabytes and spent its time in
// the collector while the decoder took no input. A wall-clock threshold
// would pin that too, and would flake on a loaded shared runner; bytes
// allocated is the same fact measured deterministically.
//
// The bound is generous and still catches the bug by three orders of
// magnitude: draining the paste must allocate a small multiple of its
// size, where the copy-per-read allocated roughly len/chunk times it.
func TestDrainingALargePasteDoesNotCopyItPerRead(t *testing.T) {
	const size = 1 << 20
	body := strings.Repeat("abcdefgh", size/8)
	full := []byte(pasteStart + body + pasteEnd)

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)

	var pend []byte
	var got []Event
	for i := 0; i < len(full); i += 128 {
		pend = append(pend, full[i:min(i+128, len(full))]...)
		for len(pend) > 0 {
			ev, n, ok := Decode(pend, false)
			if n == 0 && !ok {
				break
			}
			pend = pend[n:]
			if ok {
				got = append(got, ev)
			}
		}
	}
	runtime.ReadMemStats(&after)

	if len(got) != 1 || !got[0].IsPaste() || len(got[0].Paste.Text) != size {
		t.Fatalf("got %d events, want one paste of %d bytes", len(got), size)
	}

	// pend itself grows to the paste's size and the payload is copied out
	// once, so a handful of multiples is the honest floor. The copy-per-
	// read version allocated about 4GB here.
	alloc := after.TotalAlloc - before.TotalAlloc
	if limit := uint64(size) * 16; alloc > limit {
		t.Errorf("draining a %dKB paste allocated %dMB, want under %dMB — the "+
			"decoder is copying the pending buffer on every read",
			size/1024, alloc>>20, limit>>20)
	}
	t.Logf("allocated %d bytes draining a %dKB paste", alloc, size/1024)
}

func TestStrayCloseBracketIsSkippedNotDelivered(t *testing.T) {
	// It happens for real: the mode goes off around a suspend and a
	// paste that straddled the window leaves its tail behind. The
	// contract for "complete but unmapped" is n>0, ok=false — skip and
	// carry on — and the key AFTER it must still arrive.
	ev, n, ok := Decode([]byte(pasteEnd+"z"), false)
	if ok {
		t.Fatalf("a stray close bracket produced an event: %+v", ev)
	}
	if n != len(pasteEnd) {
		t.Fatalf("consumed %d bytes, want %d — a smaller n rescans the ESC forever",
			n, len(pasteEnd))
	}
	evs := decodeAll(t, []byte(pasteEnd+"z"), false)
	if len(evs) != 1 || !evs[0].IsKey() || evs[0].Key.Rune != 'z' {
		t.Errorf("the key after a stray bracket = %+v, want 'z'", evs)
	}
}

func TestEmptyPasteIsStillAnEvent(t *testing.T) {
	// Pasting an empty clipboard is a real gesture with a real answer
	// ("nothing to paste"), and it must reach a handler to say so. A
	// decoder that dropped it would leave the app looking broken.
	evs := decodeAll(t, []byte(pasteStart+pasteEnd), false)
	if len(evs) != 1 || !evs[0].IsPaste() || evs[0].Paste.Text != "" {
		t.Fatalf("empty paste = %+v, want one EventPaste with an empty payload", evs)
	}
}

func TestPasteWithAnEscapeInsideIsNotResplit(t *testing.T) {
	// A payload may contain ESC — pasting a file of terminal escapes is
	// a normal thing to do. Only ESC [ 201 ~ ends the paste, and a
	// terminal implementing 2004 filters that out of the payload.
	body := "\x1b[31mred\x1b[0m"
	evs := decodeAll(t, []byte(pasteStart+body+pasteEnd), false)
	if len(evs) != 1 {
		t.Fatalf("got %d events, want 1 — the payload was re-split: %+v", len(evs), evs)
	}
	if evs[0].Paste.Text != body {
		t.Errorf("payload = %q, want %q", evs[0].Paste.Text, body)
	}
}

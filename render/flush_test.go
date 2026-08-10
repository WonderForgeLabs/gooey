package render

import (
	"strings"
	"testing"
)

// apply replays emitted bytes onto the model terminal, and equal is the
// question every one of these tests is really asking: after sending only
// the difference, is the terminal showing the whole buffer?
func apply(t *testing.T, s *Screen, b []byte) {
	t.Helper()
	if _, err := s.Write(b); err != nil {
		t.Fatal(err)
	}
}

func equal(s *Screen, b *Buffer) (int, int, bool) {
	for y := 0; y < b.H; y++ {
		for x := 0; x < b.W; x++ {
			if s.Buf.At(x, y) != b.At(x, y) {
				return x, y, false
			}
		}
	}
	return 0, 0, true
}

func fill(b *Buffer, s Style) {
	for y := 0; y < b.H; y++ {
		b.SetString(0, y, strings.Repeat("x", b.W), s)
	}
}

func TestFlusherFirstFrameIsFull(t *testing.T) {
	b := NewBuffer(80, 24)
	fill(b, Style{})
	f := NewFlusher()
	out := f.Encode(nil, b, TrueColor)

	if !f.WasFull() {
		t.Fatal("first frame was not a full frame")
	}
	if f.Bytes() < 80*24 {
		t.Fatalf("first frame = %d bytes, want at least one per cell (%d)", f.Bytes(), 80*24)
	}
	sc := NewScreen(80, 24)
	apply(t, sc, out)
	if x, y, ok := equal(sc, b); !ok {
		t.Fatalf("terminal differs from buffer at %d,%d", x, y)
	}
}

func TestFlusherCleanFrameEmitsNothing(t *testing.T) {
	b := NewBuffer(80, 24)
	fill(b, Style{})
	f := NewFlusher()
	f.Encode(nil, b, TrueColor)

	out := f.Encode(nil, b, TrueColor)
	if len(out) != 0 || f.Bytes() != 0 {
		t.Fatalf("clean frame emitted %d bytes (%q), want 0", f.Bytes(), string(out))
	}
	if f.WasFull() {
		t.Fatal("clean frame reported itself full")
	}
}

// The acceptance number from the decision record: a one-row change costs
// O(row), not O(screen).
func TestFlusherOneRowChangeIsRowSized(t *testing.T) {
	b := NewBuffer(80, 24)
	fill(b, Style{})
	f := NewFlusher()
	first := f.Encode(nil, b, TrueColor)
	fullBytes := f.Bytes()

	b.SetString(0, 7, strings.Repeat("y", 80), Style{})
	out := f.Encode(nil, b, TrueColor)

	// One cursor move, one SGR, 80 runes, one reset: comfortably under
	// two rows' worth, and an order of magnitude under the full frame.
	if f.Bytes() > 2*80 {
		t.Fatalf("one-row change = %d bytes, want <= %d", f.Bytes(), 2*80)
	}
	if f.Bytes()*10 > fullBytes {
		t.Fatalf("one-row change = %d bytes vs full frame %d: not an order of magnitude", f.Bytes(), fullBytes)
	}
	if n := strings.Count(string(out), "\x1b["); n > 3 {
		t.Fatalf("one-row change used %d escape sequences, want <= 3", n)
	}
	sc := NewScreen(80, 24)
	apply(t, sc, first)
	apply(t, sc, out)
	if x, y, ok := equal(sc, b); !ok {
		t.Fatalf("terminal differs from buffer at %d,%d", x, y)
	}
}

func mustFilled(w, h int) *Buffer {
	b := NewBuffer(w, h)
	fill(b, Style{})
	return b
}

func TestFlusherOneCellChangeAddressesThatCell(t *testing.T) {
	b := NewBuffer(80, 24)
	fill(b, Style{})
	f := NewFlusher()
	f.Encode(nil, b, TrueColor)

	b.Set(40, 12, 'Z', Style{})
	out := string(f.Encode(nil, b, TrueColor))

	if !strings.Contains(out, "\x1b[13;41H") {
		t.Fatalf("output %q does not position at row 13 col 41", out)
	}
	if !strings.Contains(out, "Z") {
		t.Fatalf("output %q does not contain the new rune", out)
	}
	if f.Bytes() > 32 {
		t.Fatalf("one-cell change = %d bytes, want <= 32", f.Bytes())
	}
	if got := f.Touched(); len(got) != 1 || got[0] != (Rect{40, 12, 1, 1}) {
		t.Fatalf("touched = %v, want one 1x1 rect at 40,12", got)
	}
}

func TestFlusherMergesNearbySpansOnARow(t *testing.T) {
	b := NewBuffer(80, 24)
	fill(b, Style{})
	f := NewFlusher()
	f.Encode(nil, b, TrueColor)

	// Two changes two cells apart: jumping costs more than crossing.
	b.Set(10, 3, 'a', Style{})
	b.Set(13, 3, 'b', Style{})
	out := string(f.Encode(nil, b, TrueColor))
	if n := strings.Count(out, "H"); n != 1 {
		t.Fatalf("nearby changes used %d cursor moves, want 1: %q", n, out)
	}

	// Far apart: two spans, because crossing 40 cells is the expensive one.
	f.Encode(nil, b, TrueColor)
	b.Set(2, 5, 'a', Style{})
	b.Set(60, 5, 'b', Style{})
	out = string(f.Encode(nil, b, TrueColor))
	if n := strings.Count(out, "H"); n != 2 {
		t.Fatalf("distant changes used %d cursor moves, want 2: %q", n, out)
	}
}

func TestFlusherResizeIsFull(t *testing.T) {
	f := NewFlusher()
	f.Encode(nil, mustFilled(80, 24), TrueColor)
	f.Encode(nil, mustFilled(100, 30), TrueColor)
	if !f.WasFull() {
		t.Fatal("a differently sized buffer did not force a full frame")
	}
}

func TestFlusherInvalidateIsFull(t *testing.T) {
	b := mustFilled(80, 24)
	f := NewFlusher()
	f.Encode(nil, b, TrueColor)
	if out := f.Encode(nil, b, TrueColor); len(out) != 0 {
		t.Fatal("expected a clean frame before the invalidate")
	}
	f.Invalidate()
	out := f.Encode(nil, b, TrueColor)
	if !f.WasFull() || len(out) < 80*24 {
		t.Fatalf("after Invalidate: full=%v, %d bytes", f.WasFull(), len(out))
	}
}

func TestFlusherDamageReSendsUnchangedCells(t *testing.T) {
	b := mustFilled(80, 24)
	f := NewFlusher()
	f.Encode(nil, b, TrueColor)

	f.Damage(Rect{X: 4, Y: 2, W: 6, H: 3})
	out := string(f.Encode(nil, b, TrueColor))
	if n := strings.Count(out, "H"); n != 3 {
		t.Fatalf("damage rect emitted %d rows, want 3: %q", n, out)
	}
	if !strings.Contains(out, "\x1b[3;5H") {
		t.Fatalf("damage rect did not address its top-left: %q", out)
	}
	// Damage is consumed: the next frame is clean again.
	if out := f.Encode(nil, b, TrueColor); len(out) != 0 {
		t.Fatalf("damage persisted into the next frame: %q", string(out))
	}
}

func TestFlusherStyleRunsCoalesce(t *testing.T) {
	b := NewBuffer(40, 3)
	f := NewFlusher()
	f.Encode(nil, b, TrueColor)

	red := Style{Fg: RGB(200, 30, 30)}
	b.SetString(0, 1, strings.Repeat("r", 20), red)
	out := string(f.Encode(nil, b, TrueColor))
	// One SGR for the run, one reset at the end.
	if n := strings.Count(out, "m"); n != 2 {
		t.Fatalf("20 same-styled cells used %d SGR sequences, want 2: %q", n, out)
	}
}

// The whole-diff correctness proof: a sequence of unrelated edits, each
// flushed incrementally, must leave the model screen equal to the buffer.
func TestFlusherIncrementalEditsMatchFullRepaint(t *testing.T) {
	b := NewBuffer(60, 20)
	fill(b, Style{})
	f := NewFlusher()
	sc := NewScreen(60, 20)
	apply(t, sc, f.Encode(nil, b, TrueColor))

	styles := []Style{{}, {Bold: true}, {Fg: RGB(10, 200, 90)}, {Bg: RGB(40, 40, 60), Underline: true}}
	for i := 0; i < 40; i++ {
		x, y := (i*17)%60, (i*7)%20
		b.SetString(x, y, strings.Repeat(string(rune('A'+i%26)), 1+i%9), styles[i%len(styles)])
		if i%5 == 4 {
			b.Set((i*3)%60, (i*11)%20, ' ', Style{})
		}
		apply(t, sc, f.Encode(nil, b, TrueColor))
		if x, y, ok := equal(sc, b); !ok {
			t.Fatalf("edit %d: terminal differs from buffer at %d,%d", i, x, y)
		}
	}
}

func TestFlushCellsUnchangedForOneShotPath(t *testing.T) {
	b := NewBuffer(4, 2)
	b.SetString(0, 0, "ab", Style{Bold: true})
	var sb strings.Builder
	if err := FlushCells(&sb, b, TrueColor, false); err != nil {
		t.Fatal(err)
	}
	want := "\x1b[H" + sgr(Style{Bold: true}, TrueColor) + "ab" +
		sgr(Style{}, TrueColor) + "  \r\n    " + "\x1b[0m"
	if sb.String() != want {
		t.Fatalf("FlushCells = %q, want %q", sb.String(), want)
	}
}

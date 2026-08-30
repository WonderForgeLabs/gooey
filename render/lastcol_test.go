package render

import "testing"

// TestAWideLeadInTheLastColumnIsReportedDisplaced closes the blind spot
// in the model's own instrument.
//
// Displacement is defined against the NEXT cell — "this cell's terminal
// column is not its index" — so a glyph that overflows the END of the
// row has nothing after it to push, and the loop walked off the edge
// reporting the row faithful. The row is not faithful: the terminal
// either wraps the glyph to column 0 of the next line or drops it.
//
// This is the one case the docs call the remaining sharp edge, and an
// instrument that cannot see it makes the documentation worse than
// useless: a test asserting `!bad` passed on exactly the arrangement
// the sharp edge describes.
func TestAWideLeadInTheLastColumnIsReportedDisplaced(t *testing.T) {
	b := NewBuffer(3, 1)
	b.Clear()
	// Direct assignment, because that is now the ONLY way to reach it —
	// see the two tests below.
	b.Cells[2] = Cell{Rune: '世'}

	if got := TerminalWidth(b, 0); got != 4 {
		t.Fatalf("the row measures %d columns on a 3-wide buffer, want 4 — "+
			"the fixture does not overflow", got)
	}
	x, by, bad := Displaced(b, 0)
	if !bad {
		t.Fatal("Displaced calls the row faithful while its last glyph " +
			"needs a column the buffer does not have")
	}
	if x != 2 || by != 1 {
		t.Errorf("Displaced reports cell %d over by %d, want cell 2 over by 1",
			x, by)
	}
}

// TestNeitherWriterLeavesAWideLeadInTheLastColumn is the other half:
// the sharp edge above must not be reachable through the public writers.
// Both answer with a space, which is the only value that draws correctly
// in the single column actually available.
func TestNeitherWriterLeavesAWideLeadInTheLastColumn(t *testing.T) {
	for _, tc := range []struct {
		name  string
		write func(b *Buffer)
	}{
		{"Set", func(b *Buffer) { b.Set(2, 0, '世', Style{}) }},
		{"SetString", func(b *Buffer) { b.SetString(2, 0, "世", Style{}) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := NewBuffer(3, 1)
			b.Clear()
			tc.write(b)

			if got := b.At(2, 0).Width(); got > 1 {
				t.Errorf("the last column holds a %d-column glyph", got)
			}
			if _, _, bad := Displaced(b, 0); bad {
				t.Error("the row overflows the buffer")
			}
		})
	}
}

// TestEveryWriterHonoursTheClipNotJustTheBuffer is the divergence the
// tests above could not see, because they clip to the whole buffer —
// where cx1 == b.W and all three writers agree by coincidence.
//
// Composer.build brackets every Render with a Clip to that component's
// rect (#357), so for every component but the root the clip is NARROWER
// than the buffer. Set and SetCell tested it; SetString tested b.W and
// wrote through put, which tested b.W too. A clipped SetString therefore
// wrote over a neighbour's cells — whose paint node is clean, so nothing
// ever repaints over the damage — and could leave a Continuation one
// column past the clip, marking a cell the flusher skips forever.
//
// Found in the review of PR #425, which is also where the PR ADDED the
// disagreement: teaching Set the clip rule while leaving SetString on
// b.W is what made two writers answer one question differently.
func TestEveryWriterHonoursTheClipNotJustTheBuffer(t *testing.T) {
	// The clip is the middle of the row, so there is a neighbour on each
	// side. Sentinels mark them: anything but '.' outside the clip is a
	// write that escaped.
	const w = 10
	setup := func() *Buffer {
		b := NewBuffer(w, 1)
		for x := 0; x < w; x++ {
			b.Cells[x] = Cell{Rune: '.'}
		}
		return b
	}
	// row renders the plane for a failure message with the continuation
	// marker made visible: it is rune -1, which prints as the
	// replacement character and reads like a decoding fault rather than
	// like the deliberate sentinel it is.
	row := func(b *Buffer) string {
		out := []rune{}
		for x := 0; x < w; x++ {
			r := b.Cells[x].Rune
			if r == Continuation {
				r = '#'
			}
			out = append(out, r)
		}
		return string(out)
	}

	// Only the cells OUTSIDE the clip are asserted, because what a
	// writer does inside its own rect is the other tests' business —
	// this one is about the boundary.
	for _, tc := range []struct {
		name  string
		clip  Rect
		write func(b *Buffer)
	}{
		{
			// The headline: a wide glyph whose lead is the clip's last
			// column. Set answers with a space; SetString must too.
			name:  "SetString wide glyph at the clip's right edge",
			clip:  Rect{X: 2, Y: 0, W: 3, H: 1},
			write: func(b *Buffer) { b.SetString(4, 0, "世", Style{}) },
		},
		{
			name:  "Set wide glyph at the clip's right edge",
			clip:  Rect{X: 2, Y: 0, W: 3, H: 1},
			write: func(b *Buffer) { b.Set(4, 0, '世', Style{}) },
		},
		{
			// A run that overruns the clip on the right.
			name:  "SetString overruns the clip",
			clip:  Rect{X: 2, Y: 0, W: 3, H: 1},
			write: func(b *Buffer) { b.SetString(2, 0, "ABCDEFGH", Style{}) },
		},
		{
			// And on the left: the straddle branch blanked column 0 of
			// the SCREEN regardless of where the caller's clip started.
			name:  "SetString straddling the clip's left edge",
			clip:  Rect{X: 3, Y: 0, W: 4, H: 1},
			write: func(b *Buffer) { b.SetString(1, 0, "ABCDEFGH", Style{}) },
		},
	} {
		b := setup()
		prev := b.Clip(tc.clip)
		tc.write(b)
		b.Unclip(prev)

		got := row(b)
		for x := 0; x < w; x++ {
			inside := x >= tc.clip.X && x < tc.clip.X+tc.clip.W
			if inside {
				continue
			}
			if r := b.Cells[x].Rune; r != '.' {
				t.Errorf("%s: column %d is outside the clip %v and holds %q — "+
					"row %q. That cell belongs to a neighbour whose paint node is "+
					"clean, so nothing will ever repaint over it",
					tc.name, x, tc.clip, r, got)
			}
		}
	}
}

// TestTheTwoWritersAgreeUnderAClip states the invariant directly: for
// every column of a narrowed clip, writing a wide glyph through Set and
// through SetString must leave the same row.
//
// The tests above check specific arrangements; this checks the RULE, and
// it is the one that fails the moment the two implementations drift
// again for a reason nobody anticipated.
func TestTheTwoWritersAgreeUnderAClip(t *testing.T) {
	const w = 10
	clip := Rect{X: 2, Y: 0, W: 5, H: 1}
	for x := 0; x < w; x++ {
		a, bb := NewBuffer(w, 1), NewBuffer(w, 1)
		a.Clear()
		bb.Clear()

		prev := a.Clip(clip)
		a.Set(x, 0, '世', Style{})
		a.Unclip(prev)

		prev = bb.Clip(clip)
		bb.SetString(x, 0, "世", Style{})
		bb.Unclip(prev)

		for c := 0; c < w; c++ {
			if a.Cells[c] != bb.Cells[c] {
				t.Errorf("writing a wide glyph at column %d under clip %v: Set left "+
					"%+v at column %d, SetString left %+v — one rule, two answers",
					x, clip, a.Cells[c], c, bb.Cells[c])
			}
		}
	}
}

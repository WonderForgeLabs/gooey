package main

import (
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

// row renders one matchLine and hands back the buffer.
func row(t *testing.T, path string, hits []int, w int) *render.Buffer {
	t.Helper()
	ml := &matchLine{
		path: prop.NewSource(path),
		hits: prop.NewSource(hits),
	}
	c := gooey.NewComposer(ml, w, 1)
	t.Cleanup(c.Close)
	f, _ := c.Frame()
	return f.Cells
}

// TestAMatchLineIsLaidOutInColumnsNotBytes is the bug that could not be
// seen from an ASCII fixture.
//
// The paint loop met three index spaces and used one for all of them.
// fuzzy reports BYTE offsets into the path; a cell is addressed by
// COLUMN; the unit that occupies a column is a grapheme CLUSTER. On an
// ASCII path the three coincide exactly, so every fixture agreed with
// the bug.
//
// Here they do not: "é" is two bytes and one column, so the byte cursor
// runs one ahead of the column cursor from the second character on. The
// old loop placed everything after it one cell too far right and left a
// hole behind.
func TestAMatchLineIsLaidOutInColumnsNotBytes(t *testing.T) {
	const path = "éxyz" // 5 bytes, 4 columns

	b := row(t, path, nil, 20)

	if got := render.RowText(b, 0); got[:len(path)] != path {
		t.Errorf("the row reads %q, want it to start with %q",
			render.RowText(b, 0), path)
	}
	// The cell-level statement of the same thing: every character sits
	// at its own column, with no gap opened by the two-byte one.
	for i, want := range []rune{'é', 'x', 'y', 'z'} {
		if got := b.At(i, 0).Rune; got != want {
			t.Errorf("column %d holds %q, want %q — the row has been "+
				"laid out by byte offset", i, got, want)
		}
	}
	if x, by, bad := render.Displaced(b, 0); bad {
		t.Errorf("the row is displaced: cell %d moved by %d", x, by)
	}
}

// TestAMatchLineHighlightsTheClusterTheMatchLandedIn pins the other half
// of the same confusion: the highlight is selected by a byte offset and
// applied to a column, so on a multi-byte path it used to land on
// whichever character happened to sit at that byte number.
//
// fuzzy can only ever point at ONE byte of a character. Highlighting the
// whole cluster is the only answer that is well-defined — a character
// cannot be half-matched.
func TestAMatchLineHighlightsTheClusterTheMatchLandedIn(t *testing.T) {
	const path = "éxyz"

	// Byte 2 is 'x': "é" occupies bytes 0 and 1.
	b := row(t, path, []int{2}, 20)

	if !b.At(1, 0).Style.Bold {
		t.Error("the match on 'x' is not highlighted")
	}
	if b.At(0, 0).Style.Bold {
		t.Error("the highlight landed on 'é', which the match did not " +
			"point at — the byte offset was used as a column")
	}

	// And a match INSIDE the two-byte character highlights that
	// character, not the one after it.
	b2 := row(t, path, []int{1}, 20)
	if !b2.At(0, 0).Style.Bold {
		t.Error("a match on the second byte of 'é' did not highlight it")
	}
	if b2.At(1, 0).Style.Bold {
		t.Error("the highlight spilled onto the following character")
	}
}

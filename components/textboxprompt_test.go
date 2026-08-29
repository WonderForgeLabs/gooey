package components

import (
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/prop"
)

// Clicking in a TextBox maps a screen COLUMN to a rune index, and the
// prompt in front of the text has to be subtracted in the same unit.
//
// A prompt holding a wide glyph is where those units diverge: "世> " is
// 3 runes and 4 columns, so a rune-counting offset put every click one
// index to the right of the character actually under the pointer. The
// symptom is a caret that lands beside the letter you clicked, which
// reads as imprecision rather than as a bug — and gets worse the more
// wide glyphs the prompt has.
//
// Asserted through indexAt rather than through a synthesized mouse
// event: the mapping is the claim, and a click would drag focus,
// selection and scroll into a test about arithmetic.
func TestClickingPastAWidePromptLandsOnTheCharacterClicked(t *testing.T) {
	tb := &TextBox{
		Text:   prop.NewSource("abcdef"),
		Prompt: prop.NewSource("世> "), // 3 runes, 4 columns
	}
	tb.Arrange(gooey.Rect{X: 0, Y: 0, W: 20, H: 1})

	// The prompt occupies columns 0..3, so 'a' is drawn at column 4.
	// Clicking column 4 must select index 0.
	if got := tb.indexAt(4); got != 0 {
		t.Errorf("indexAt(4) = %d, want 0 — the prompt \"世> \" is 4 columns, so "+
			"the first character sits at column 4. A rune count gives 3 and "+
			"every click lands one character right of the pointer", got)
	}
	// And the character after it.
	if got := tb.indexAt(5); got != 1 {
		t.Errorf("indexAt(5) = %d, want 1", got)
	}

	// An ASCII prompt is the control: the two units agree there, so this
	// case passes against the rune-counting version and proves only that
	// the fix did not break the ordinary path.
	plain := &TextBox{
		Text:   prop.NewSource("abcdef"),
		Prompt: prop.NewSource("> "), // 2 runes, 2 columns
	}
	plain.Arrange(gooey.Rect{X: 0, Y: 0, W: 20, H: 1})
	if got := plain.indexAt(2); got != 0 {
		t.Errorf("with an ASCII prompt, indexAt(2) = %d, want 0", got)
	}
}

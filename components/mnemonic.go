package components

import (
	"unicode"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/render"
)

// The mnemonic marker is one convention shared by every component that
// takes an accelerator in its text — MenuBar titles and items, Button
// content: an underscore names the letter ("_File", "E_xit"), "__" is a
// literal underscore, and the accelerator is rendered underlined ALWAYS,
// because a terminal cannot see a held ALT (no key-up events), so
// "show while ALT is down" is not implementable and always-on is the
// honest convention.
//
// What differs per component is the DEFAULT. Menus fall back to the
// first letter — a menu bar without accelerators is broken furniture,
// and titles are one word. Buttons take only an explicit marker: every
// button would otherwise claim a page-global alt gesture its author
// never declared, and two buttons starting with the same letter ("run",
// "refresh") would collide silently.

// splitMnemonic parses the accelerator out of a title or item text:
// "_File" → ("File", 'f', 0), "E_xit" → ("Exit", 'x', 1), "__" is a
// literal underscore, and only the first marker counts. Without a
// marker the first letter or digit is the implicit accelerator. pos is
// the rune index of the accelerator in the returned display text, -1
// when the string has no letter to accelerate with.
func splitMnemonic(s string) (text string, accel rune, pos int) {
	text, accel, pos, ok := splitExplicitMnemonic(s)
	if !ok {
		for i, r := range []rune(text) {
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				accel, pos = unicode.ToLower(r), i
				break
			}
		}
	}
	return text, accel, pos
}

// splitExplicitMnemonic is the marker-only half of splitMnemonic: ok
// reports whether the string actually carries an underscore marker, and
// there is no first-letter fallback. It is what Button uses, per the
// explicit-only rule above.
func splitExplicitMnemonic(s string) (text string, accel rune, pos int, ok bool) {
	in := []rune(s)
	out := make([]rune, 0, len(in))
	pos = -1
	for i := 0; i < len(in); i++ {
		if in[i] == '_' && i+1 < len(in) {
			if in[i+1] == '_' {
				out = append(out, '_')
				i++
				continue
			}
			if pos < 0 {
				pos = len(out)
				accel = unicode.ToLower(in[i+1])
				out = append(out, in[i+1])
				i++
				continue
			}
		}
		out = append(out, in[i])
	}
	return string(out), accel, pos, pos >= 0
}

// mnemonicCol converts a mnemonic's RUNE index into the COLUMN offset of
// the cell it is painted in.
//
// splitMnemonic reports pos as an index into []rune(text) — it has to,
// because that is how the caller indexes back into the text to re-style
// the letter. Four painters then used that number directly as a column
// offset from where the text starts, which is only the same number while
// every rune before the accelerator is one column wide. Put a CJK
// character or an emoji earlier in the label and the underline drifts
// left of the letter it names, by one column per wide glyph.
//
// Here rather than four times over: it is one idea, and a local copy in
// each painter is how they come to disagree.
func mnemonicCol(text string, pos int) int {
	if pos <= 0 {
		return 0
	}
	r := []rune(text)
	if pos > len(r) {
		pos = len(r)
	}
	return render.StringWidth(string(r[:pos]))
}

// underlineAt underlines the accelerator by RESTYLING the cell the label
// already painted, rather than painting the character a second time.
//
// The four painters re-derived it as []rune(text)[pos] and handed that
// to Buffer.Set, which loses everything a rune cannot carry: a cell
// holding a grapheme CLUSTER — a decomposed "é", an emoji with a
// variation selector — came back as its first rune alone, so a wide
// accelerator narrowed to one column and a combining mark vanished.
// Underlining an accelerator is not a reason for the letter to change.
//
// It also removes the last place a rune index and a column met. The
// character is no longer re-derived at all, so there is nothing left to
// index with the wrong number; mnemonicCol above owns the column and
// this owns the style. Found in review of #413.
func underlineAt(f *gooey.Frame, x, y int, st render.Style) {
	st.Underline = true
	f.Cells.SetCell(x, y, f.Cells.At(x, y).WithStyle(st))
}

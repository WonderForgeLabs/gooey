package components

import "unicode"

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

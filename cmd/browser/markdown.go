package main

// A demo directory that carries a README.md has already said what it is,
// in the format the rest of the world reads it in. The preview pane
// therefore prefers the README over the main.go doc comment — which
// means the pane has to render markdown.
//
// This is a deliberately small renderer: the constructs that actually
// appear in a demo README (headings, bold, inline code, fenced blocks,
// lists, links) and nothing else. Anything it does not recognise falls
// through as literal text, so no README can break the browser — the
// failure mode of a hand-rolled parser must be "looks plain", never
// "returns an error into a paint".
//
// Output is styled LINES, not a string. A heading that is accent-bold
// while an inline code span inside a sentence is mint cannot survive a
// round trip through plain text, and the pane paints cells anyway.

import (
	"strings"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/render"
)

// mdSpan is a run of text sharing one style — the unit the painter
// writes and the unit the wrapper moves between lines.
type mdSpan struct {
	text  string
	style render.Style
}

// mdLine is one painted row. A nil line is a blank row.
type mdLine []mdSpan

// mdStyles is the palette. It is a parameter rather than a set of
// globals so the tests can assert "this span got the code style" by
// identity instead of by matching colour literals.
type mdStyles struct {
	heading, bold, code, fence, link, bullet, text render.Style
}

func markdownStyles() mdStyles {
	return mdStyles{
		heading: accent,
		bold:    render.Style{Bold: true},
		code:    render.Style{Fg: render.RGB(130, 220, 180)},
		fence:   dim,
		link:    render.Style{Fg: render.RGB(120, 170, 255), Underline: true},
		bullet:  accent,
		text:    render.Style{},
	}
}

const mdTab = "    "

// renderMarkdown turns a README into painted lines fitted to w columns.
//
// Paragraphs are joined before wrapping — that is the one place where
// markdown differs from the plain doc-comment path, and getting it wrong
// makes a hard-wrapped README render as a ragged column inside a pane
// that is a different width than the author's editor was.
func renderMarkdown(src string, w int, st mdStyles) []mdLine {
	if w < 4 {
		return nil
	}
	var out []mdLine
	var para []string
	flush := func() {
		if len(para) == 0 {
			return
		}
		out = append(out, wrapSpans(mdInline(strings.Join(para, " "), st.text, st), w, nil, nil)...)
		para = para[:0]
	}

	lines := strings.Split(src, "\n")
	for i := 0; i < len(lines); i++ {
		raw := strings.TrimRight(strings.ReplaceAll(lines[i], "\t", mdTab), " ")
		trim := strings.TrimSpace(raw)

		if isFence(trim) {
			// A fence is verbatim: indented, dim, and NOT wrapped. Code
			// that soft-wraps stops being readable as code, so it clips
			// at the pane edge instead. An unterminated fence simply runs
			// to the end of the file.
			flush()
			for i++; i < len(lines) && !isFence(strings.TrimSpace(lines[i])); i++ {
				body := strings.TrimRight(strings.ReplaceAll(lines[i], "\t", mdTab), " ")
				out = append(out, mdLine{{text: clip("  "+body, w), style: st.fence}})
			}
			continue
		}
		if trim == "" {
			flush()
			out = append(out, nil)
			continue
		}
		if _, text, ok := splitHeading(trim); ok {
			flush()
			out = append(out, wrapSpans(mdInline(text, st.heading, st), w, nil, nil)...)
			continue
		}
		if indent, marker, body, ok := bulletOf(raw); ok {
			flush()
			// Lazy continuation: a list item hard-wrapped in the source is
			// still ONE item. Without this, every line after the first
			// falls back to the paragraph branch and renders flush left,
			// which reads as prose interrupting a list — the single most
			// common shape in a real README, since any item longer than
			// the author's editor width is written this way.
			body = strings.Join(append([]string{body}, continuation(lines, &i)...), " ")
			pad := strings.Repeat(" ", indent)
			first := []mdSpan{{text: pad, style: st.text}, {text: marker + " ", style: st.bullet}}
			cont := []mdSpan{{text: pad + strings.Repeat(" ", colWidth(marker)+1), style: st.text}}
			out = append(out, wrapSpans(mdInline(body, st.text, st), w, first, cont)...)
			continue
		}
		para = append(para, trim)
	}
	flush()
	return out
}

// continuation consumes the lines that belong to the list item ending at
// *i and returns their text. It advances *i past them.
//
// It accepts an unindented continuation as well as an indented one, which
// is markdown's "lazy" rule and also what people actually write. The stop
// conditions are the things that unambiguously begin a new block: a blank
// line, a fence, another bullet, a heading.
func continuation(lines []string, i *int) []string {
	var out []string
	for *i+1 < len(lines) {
		next := strings.TrimRight(strings.ReplaceAll(lines[*i+1], "\t", mdTab), " ")
		trim := strings.TrimSpace(next)
		if trim == "" || isFence(trim) {
			break
		}
		if _, _, _, ok := bulletOf(next); ok {
			break
		}
		if _, _, ok := splitHeading(trim); ok {
			break
		}
		out = append(out, trim)
		*i++
	}
	return out
}

func isFence(trim string) bool {
	return strings.HasPrefix(trim, "```") || strings.HasPrefix(trim, "~~~")
}

// splitHeading recognises an ATX heading. A run of '#' not followed by a
// space is not a heading — `#3` in a sentence is a sentence.
func splitHeading(trim string) (level int, text string, ok bool) {
	n := 0
	for n < len(trim) && trim[n] == '#' {
		n++
	}
	if n == 0 || n > 6 {
		return 0, "", false
	}
	rest := trim[n:]
	if rest != "" && rest[0] != ' ' {
		return 0, "", false
	}
	return n, strings.TrimSpace(strings.TrimRight(rest, "#")), true
}

// bulletOf recognises an unordered or ordered list item, reporting the
// leading indent so nesting survives.
func bulletOf(raw string) (indent int, marker, body string, ok bool) {
	i := 0
	for i < len(raw) && raw[i] == ' ' {
		i++
	}
	rest := raw[i:]
	for _, m := range []string{"- ", "* ", "+ "} {
		if strings.HasPrefix(rest, m) {
			return i, "•", strings.TrimSpace(rest[2:]), true
		}
	}
	j := 0
	for j < len(rest) && rest[j] >= '0' && rest[j] <= '9' {
		j++
	}
	if j > 0 && j+1 < len(rest) && rest[j] == '.' && rest[j+1] == ' ' {
		return i, rest[:j+1], strings.TrimSpace(rest[j+2:]), true
	}
	return 0, "", "", false
}

// mdInline splits one logical line into styled spans. Every construct
// has the same shape: find the opener, find the closer, and if the
// closer is missing emit the opener as the literal text it is.
func mdInline(s string, base render.Style, st mdStyles) []mdSpan {
	var out []mdSpan
	var lit strings.Builder
	flush := func() {
		if lit.Len() > 0 {
			out = append(out, mdSpan{text: lit.String(), style: base})
			lit.Reset()
		}
	}
	for i := 0; i < len(s); {
		switch {
		case s[i] == '`':
			if k := strings.IndexByte(s[i+1:], '`'); k >= 0 {
				flush()
				out = append(out, mdSpan{text: s[i+1 : i+1+k], style: st.code})
				i += k + 2
				continue
			}
		case strings.HasPrefix(s[i:], "**"):
			if k := strings.Index(s[i+2:], "**"); k >= 0 {
				flush()
				b := base
				b.Bold = true
				// Recursion, not a flag: `code` inside **bold** is still
				// code, and the inner span keeps its own style.
				out = append(out, mdInline(s[i+2:i+2+k], b, st)...)
				i += k + 4
				continue
			}
		case s[i] == '[':
			if label, n, ok := mdLinkAt(s[i:]); ok {
				flush()
				// The URL is dropped on purpose: a terminal cannot follow
				// it, and printing it doubles the length of every line.
				// Underline is the affordance that says "this was a link".
				out = append(out, mdSpan{text: label, style: st.link})
				i += n
				continue
			}
		}
		lit.WriteByte(s[i])
		i++
	}
	flush()
	return out
}

func mdLinkAt(s string) (label string, n int, ok bool) {
	end := strings.IndexByte(s, ']')
	if end < 0 || !strings.HasPrefix(s[end:], "](") {
		return "", 0, false
	}
	rparen := strings.IndexByte(s[end+2:], ')')
	if rparen < 0 {
		return "", 0, false
	}
	return s[1:end], end + 2 + rparen + 1, true
}

// mdWord is one wrappable token. glue marks a word that must stay
// attached to its predecessor with no space — `**bold**text` is one
// word painted in two styles, and breaking it would insert a space that
// the author did not write.
type mdWord struct {
	text  string
	style render.Style
	glue  bool
}

func mdWords(spans []mdSpan) []mdWord {
	var out []mdWord
	prevOpen := false // previous span ended flush against the next one
	for _, sp := range spans {
		fields := strings.Fields(sp.text)
		if len(fields) == 0 {
			prevOpen = false
			continue
		}
		leadWS := sp.text != strings.TrimLeft(sp.text, " \t")
		for i, f := range fields {
			out = append(out, mdWord{
				text:  f,
				style: sp.style,
				glue:  i == 0 && prevOpen && !leadWS && len(out) > 0,
			})
		}
		prevOpen = sp.text == strings.TrimRight(sp.text, " \t")
	}
	return out
}

// wrapSpans greedily fills lines of w columns. first prefixes the first
// line and cont every continuation, which is what gives a list item its
// hanging indent.
func wrapSpans(spans []mdSpan, w int, first, cont []mdSpan) []mdLine {
	words := mdWords(spans)
	if len(words) == 0 && len(first) == 0 {
		return nil
	}
	var out []mdLine
	line := append(mdLine{}, first...)
	lw := spanWidth(first)
	contW := spanWidth(cont)
	placed := false
	for i := 0; i < len(words); {
		j := i + 1
		for j < len(words) && words[j].glue {
			j++
		}
		cw := 0
		for _, wd := range words[i:j] {
			cw += colWidth(wd.text)
		}
		if placed && lw+1+cw > w {
			out = append(out, line)
			line = append(mdLine{}, cont...)
			lw, placed = contW, false
		}
		if placed {
			line = append(line, mdSpan{text: " ", style: words[i].style})
			lw++
		}
		for _, wd := range words[i:j] {
			line = append(line, mdSpan{text: wd.text, style: wd.style})
			lw += colWidth(wd.text)
		}
		placed = true
		i = j
	}
	return append(out, line)
}

func spanWidth(spans []mdSpan) int {
	n := 0
	for _, sp := range spans {
		n += colWidth(sp.text)
	}
	return n
}

// colWidth is the one place this file decides how wide a string is, and
// it answers in display COLUMNS.
//
// It was runeLen, and the rename is the point rather than a tidy-up: the
// wrapper it feeds is documented as filling "lines of w columns", so the
// old name recorded the bug — a name asserting one unit under a comment
// promising another. A README is arbitrary prose from whoever wrote the
// demo, so an emoji in a heading is an ordinary input here, not an
// exotic one.
func colWidth(s string) int { return render.StringWidth(s) }

// drawLines paints styled lines into a rect, clipping rather than
// wrapping — wrapping already happened, at the width this rect has.
//
// Through SetString, one span at a time, rather than the rune-at-a-time
// Cells.Set loop that used to be here. That loop advanced one column per
// rune, which is the whole of #358 in four lines: a wide glyph was
// written into one cell with the NEXT rune in the cell its second column
// covers, and because the marker that suppresses the overwritten cell is
// laid by SetString, this path could not have produced one. Clipping is
// by column for the same reason.
func drawLines(f *gooey.Frame, x, y, w, h int, lines []mdLine) {
	for i, ln := range lines {
		if i >= h {
			return
		}
		cx := x
		for _, sp := range ln {
			if cx >= x+w {
				break
			}
			vis := render.ClipCols(sp.text, x+w-cx)
			f.Cells.SetString(cx, y+i, vis, sp.style)
			cx += render.StringWidth(vis)
		}
	}
}

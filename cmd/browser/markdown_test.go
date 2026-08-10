package main

import (
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey/render"
)

// text flattens painted lines back to strings, which is what the
// assertions about wrapping and bullets are actually about.
func text(lines []mdLine) []string {
	out := make([]string, len(lines))
	for i, ln := range lines {
		var sb strings.Builder
		for _, sp := range ln {
			sb.WriteString(sp.text)
		}
		out[i] = sb.String()
	}
	return out
}

// styleOf finds the style the renderer gave a piece of text.
func styleOf(t *testing.T, lines []mdLine, want string) render.Style {
	t.Helper()
	for _, ln := range lines {
		for _, sp := range ln {
			if sp.text == want {
				return sp.style
			}
		}
	}
	t.Fatalf("no span %q in %q", want, text(lines))
	return render.Style{}
}

func TestHeadingIsAccentBold(t *testing.T) {
	st := markdownStyles()
	lines := renderMarkdown("# Reader\n", 40, st)
	if got := styleOf(t, lines, "Reader"); got != st.heading {
		t.Fatalf("heading style = %+v, want %+v", got, st.heading)
	}
	if !st.heading.Bold {
		t.Fatal("heading style is not bold")
	}
	// The '#' markers are consumed, not painted.
	if strings.Contains(strings.Join(text(lines), ""), "#") {
		t.Fatalf("heading markers survived: %q", text(lines))
	}
}

func TestNotAHeadingWithoutSpace(t *testing.T) {
	st := markdownStyles()
	lines := renderMarkdown("#3 is a number", 40, st)
	if got := styleOf(t, lines, "#3"); got != st.text {
		t.Fatalf("#3 styled as %+v, want plain", got)
	}
}

func TestInlineConstructs(t *testing.T) {
	st := markdownStyles()
	lines := renderMarkdown("press **q** to quit the `reader` see [docs](http://x/y)", 60, st)
	if got := styleOf(t, lines, "q"); !got.Bold {
		t.Fatalf("**q** not bold: %+v", got)
	}
	if got := styleOf(t, lines, "reader"); got != st.code {
		t.Fatalf("`reader` style = %+v, want code", got)
	}
	if got := styleOf(t, lines, "docs"); !got.Underline {
		t.Fatalf("link not underlined: %+v", got)
	}
	// A terminal cannot follow a URL; printing it only costs columns.
	if joined := strings.Join(text(lines), " "); strings.Contains(joined, "http://x/y") {
		t.Fatalf("link URL painted: %q", joined)
	}
}

func TestCodeInsideBoldKeepsCodeStyle(t *testing.T) {
	st := markdownStyles()
	lines := renderMarkdown("**run `go test` now**", 40, st)
	if got := styleOf(t, lines, "test"); got != st.code {
		t.Fatalf("nested code style = %+v, want code", got)
	}
}

func TestFenceIsVerbatimAndUnwrapped(t *testing.T) {
	st := markdownStyles()
	src := "intro\n\n```sh\ngo run ./cmd/reader --opml feeds.opml --verbose --extra\n```\nafter\n"
	lines := renderMarkdown(src, 24, st)
	var fenced []string
	for _, ln := range lines {
		if len(ln) == 1 && ln[0].style == st.fence {
			fenced = append(fenced, ln[0].text)
		}
	}
	if len(fenced) != 1 {
		t.Fatalf("fence produced %d lines, want 1: %q", len(fenced), fenced)
	}
	// Clipped to the pane, never wrapped onto a second line.
	if runeLen(fenced[0]) > 24 {
		t.Fatalf("fence line %d wide in a 24 column pane: %q", runeLen(fenced[0]), fenced[0])
	}
	if !strings.HasPrefix(fenced[0], "  go run") {
		t.Fatalf("fence body not indented verbatim: %q", fenced[0])
	}
	// The fence markers themselves are not painted.
	if joined := strings.Join(text(lines), "\n"); strings.Contains(joined, "```") {
		t.Fatalf("fence markers painted: %q", joined)
	}
}

func TestListsGetBulletsAndHangingIndent(t *testing.T) {
	st := markdownStyles()
	lines := text(renderMarkdown("- keys are j and k and q and r and p\n- second\n", 20, st))
	if len(lines) < 3 {
		t.Fatalf("want a wrapped item plus a second, got %q", lines)
	}
	if !strings.HasPrefix(lines[0], "• ") {
		t.Fatalf("first item missing bullet: %q", lines[0])
	}
	if !strings.HasPrefix(lines[1], "  ") || strings.HasPrefix(strings.TrimSpace(lines[1]), "•") {
		t.Fatalf("continuation is not a hanging indent: %q", lines[1])
	}
	if !strings.HasPrefix(lines[2], "• second") {
		t.Fatalf("second item missing: %q", lines)
	}
}

// A README written at 80 columns hard-wraps its long list items. Every
// line of one is still the same item, and must keep the hanging indent
// rather than falling out into a flush-left paragraph.
func TestHardWrappedListItemStaysOneItem(t *testing.T) {
	src := "- **Focus** is a source property. Only the focused pane shows\n" +
		"  the indicator, and only it receives keys.\n" +
		"- second item\n"
	lines := text(renderMarkdown(src, 30, markdownStyles()))
	bullets := 0
	for _, ln := range lines {
		if strings.HasPrefix(ln, "• ") {
			bullets++
			continue
		}
		if strings.TrimSpace(ln) != "" && !strings.HasPrefix(ln, "  ") {
			t.Fatalf("continuation escaped the list: %q in %q", ln, lines)
		}
	}
	if bullets != 2 {
		t.Fatalf("got %d bullets, want 2: %q", bullets, lines)
	}
}

func TestListItemStopsAtTheNextBlock(t *testing.T) {
	for _, next := range []string{"", "# heading", "- another", "```"} {
		src := "- item\n" + next + "\nplain\n"
		lines := text(renderMarkdown(src, 40, markdownStyles()))
		if lines[0] != "• item" {
			t.Fatalf("item swallowed %q: %q", next, lines)
		}
	}
}

func TestOrderedListKeepsItsNumber(t *testing.T) {
	lines := text(renderMarkdown("1. first\n2. second\n", 30, markdownStyles()))
	if lines[0] != "1. first" || lines[1] != "2. second" {
		t.Fatalf("ordered list = %q", lines)
	}
}

func TestParagraphsJoinThenWrap(t *testing.T) {
	// Hard-wrapped source, re-wrapped to the pane: a README written at
	// 80 columns must not render as a ragged column in a 30 column pane.
	src := "one two three\nfour five six\nseven eight\n"
	lines := text(renderMarkdown(src, 20, markdownStyles()))
	for _, ln := range lines {
		if runeLen(ln) > 20 {
			t.Fatalf("line over width: %q", ln)
		}
	}
	if joined := strings.Join(lines, " "); !strings.Contains(joined, "three four") {
		t.Fatalf("paragraph lines were not joined: %q", lines)
	}
}

func TestAdjacentSpansAreNotSeparated(t *testing.T) {
	// `**bold**text` is one word in two styles. Wrapping must not insert
	// a space the author did not write.
	lines := text(renderMarkdown("**re**loaded", 40, markdownStyles()))
	if lines[0] != "reloaded" {
		t.Fatalf("glued spans = %q, want %q", lines[0], "reloaded")
	}
}

func TestUnknownSyntaxRendersAsPlainText(t *testing.T) {
	st := markdownStyles()
	for _, src := range []string{
		"an **unterminated bold",
		"an `unterminated code",
		"a [broken](link",
		"| a | table |\n|---|---|\n| 1 | 2 |",
		"> a blockquote",
		"![an image](x.png)",
		"",
		"\x00\x01 control bytes",
	} {
		lines := renderMarkdown(src, 30, st)
		if len(lines) == 0 && src != "" {
			t.Fatalf("%q rendered nothing", src)
		}
		for _, ln := range lines {
			for _, sp := range ln {
				if sp.style == st.heading {
					t.Fatalf("%q produced a heading span %q", src, sp.text)
				}
			}
		}
	}
}

func TestNarrowPaneRendersNothingRatherThanLooping(t *testing.T) {
	if got := renderMarkdown("# x\n\n- y\n", 2, markdownStyles()); got != nil {
		t.Fatalf("width 2 = %q, want nil", text(got))
	}
}

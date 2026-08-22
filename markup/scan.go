package markup

import (
	"fmt"
	"regexp"
	"strings"
)

// Scanning interpolated content.
//
// Every "literal text, possibly with {{…}} in it" position in the
// markup — element content, Content, Title, Label, Prompt — funnels
// through bindText, and bindText funnels through scanBindings. Two
// forms are legal inside the braces:
//
//	{{.Path}}          a binding to a context value (lvalue semantics)
//	{{ns:Func args…}}  a value namespace call (see values.go)
//
// Anything else is a LOAD ERROR. That strictness is the point of this
// file. Before it existed, bindText matched {{.Path}} with a regexp and
// left every non-matching {{…}} as literal text, so
//
//	<Text>{{env:Get `HOME`}}</Text>
//
// loaded clean and painted the sixteen characters "{{env:Get `HOME`}}"
// on the terminal — no error, no provider call, no way to tell a typo
// from a decision. The house rule is that everything resolvable fails
// at load time; a brace expression is resolvable, so it does.
//
// Scanning is hand-rolled rather than regexp-driven because a backtick
// literal may legally contain a brace: {{str:Replace .S `}}` `--`}} has
// to find the LAST }} , not the first.

// segKind distinguishes the three things content splits into.
type segKind uint8

const (
	segLiteral segKind = iota
	segPath
	segCall
)

// bindingSeg is one segment of scanned content.
type bindingSeg struct {
	kind segKind
	text string       // literal text, or the path (without its dot)
	call *handlerExpr // segCall only
}

// pathRe is the whole-body form of a value binding. It is deliberately
// the same character class bindRe used, so no document that loaded
// before this file existed stops loading because of it.
var pathRe = regexp.MustCompile(`^\.([A-Za-z0-9_.]+)$`)

// callHeadRe recognizes a value-namespace call by its head, so that a
// malformed one reports the namespace error rather than "not a
// binding". Body-level, unanchored at the tail.
var callHeadRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*\s*:\s*[A-Za-z_][A-Za-z0-9_]*`)

// scanBindings splits content into literal, path and call segments.
func scanBindings(content string) ([]bindingSeg, error) {
	var segs []bindingSeg
	pos := 0
	for {
		open := strings.Index(content[pos:], "{{")
		if open < 0 {
			break
		}
		open += pos
		close, err := closingBraces(content, open+2)
		if err != nil {
			return nil, err
		}
		if open > pos {
			segs = append(segs, bindingSeg{kind: segLiteral, text: content[pos:open]})
		}
		body := strings.TrimSpace(content[open+2 : close])
		seg, err := classifyBinding(body, content[open:close+2])
		if err != nil {
			return nil, err
		}
		segs = append(segs, seg)
		pos = close + 2
	}
	if pos < len(content) {
		segs = append(segs, bindingSeg{kind: segLiteral, text: content[pos:]})
	}
	return segs, nil
}

// errSnippetRunes bounds how much of the document an unterminated-
// expression error quotes back.
//
// Both error paths below quote from the opening `{{` to the end of the
// remaining content, and they have to: neither knows where the mistake
// ends, which is exactly what is wrong with the input. Uncapped, an
// early stray brace in a large page put the rest of the FILE inside a
// message somebody reads while staring at one attribute (#232).
const errSnippetRunes = 60

// errSnippet quotes at most one line and at most errSnippetRunes of s,
// and says how much it dropped. The result is already quoted — callers
// use %s, not %q, or the elision note lands inside the quotes and reads
// as part of the document.
//
// Two cuts, because they catch different mistakes: the length bound is
// for a run-on single line, and the newline bound is for the ordinary
// case, where an unterminated expression is a one-line error and the
// line it sits on is the whole of the useful context.
//
// Cutting on RUNES rather than bytes is not fussiness. A byte cap lands
// mid-character often enough in a document with any non-ASCII in it, and
// what it quotes back is a replacement glyph the author never wrote —
// an error message that misreports the text it is complaining about.
func errSnippet(s string) string {
	full := []rune(s)
	kept := full
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		kept = []rune(s[:i])
	}
	if len(kept) > errSnippetRunes {
		kept = kept[:errSnippetRunes]
	}
	if len(kept) == len(full) {
		return fmt.Sprintf("%q", string(kept))
	}
	return fmt.Sprintf("%q… (+%d more characters)", string(kept), len(full)-len(kept))
}

// closingBraces finds the "}}" that ends a brace expression opened at
// from, skipping over backtick literals so a brace inside one does not
// terminate the expression early.
func closingBraces(content string, from int) (int, error) {
	for i := from; i < len(content); {
		switch {
		case content[i] == '`':
			j := strings.IndexByte(content[i+1:], '`')
			if j < 0 {
				return 0, fmt.Errorf("markup: %s: unterminated ` literal", errSnippet(content[from-2:]))
			}
			i += j + 2
		case strings.HasPrefix(content[i:], "}}"):
			return i, nil
		default:
			i++
		}
	}
	return 0, fmt.Errorf("markup: %s: unterminated {{ — a brace expression must be closed with }}", errSnippet(content[from-2:]))
}

// classifyBinding decides what one brace expression is, and refuses to
// guess. raw is the full "{{…}}" text, for error messages.
func classifyBinding(body, raw string) (bindingSeg, error) {
	if m := pathRe.FindStringSubmatch(body); m != nil {
		return bindingSeg{kind: segPath, text: m[1]}, nil
	}
	if callHeadRe.MatchString(body) {
		x, err := parseHandlerExpr(raw)
		if err != nil {
			return bindingSeg{}, err
		}
		return bindingSeg{kind: segCall, call: x}, nil
	}
	if strings.HasPrefix(body, ".") {
		return bindingSeg{}, fmt.Errorf("markup: %s is not a valid binding path — a path is .Name or .Outer.Inner, letters, digits and underscores only", raw)
	}
	return bindingSeg{}, fmt.Errorf("markup: %s is neither a binding ({{.Path}}) nor a value-namespace call ({{ns:Func …}})", raw)
}

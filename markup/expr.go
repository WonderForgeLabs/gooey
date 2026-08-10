package markup

import (
	"fmt"
	"regexp"
	"strings"
)

// The extension-expression grammar — the one form the binding DSL grows
// for handler namespaces:
//
//	{{ns:Func arg… | into .Target}}
//
// It is deliberately not general: no nesting, no arithmetic, no
// user-defined pipeline stages. Arguments are the same atoms the rest of
// the DSL uses — `.Path` (a property handle, read at invoke time) and a
// backtick literal — and the only stage in v1 is `into`, naming the
// property the result is Set into.
//
// A value binding ({{.Path}}) and a handler expression ({{ns:Func …}})
// are disjoint by the colon, so an attribute is unambiguously one or the
// other.

// handlerExprRe recognizes the handler form and splits off its body. The
// body is lexed rather than pattern-matched, so argument errors can name
// the offending token.
var handlerExprRe = regexp.MustCompile(`^\s*\{\{\s*([A-Za-z_][A-Za-z0-9_-]*)\s*:\s*([A-Za-z_][A-Za-z0-9_]*)\s*(.*)\}\}\s*$`)

// handlerExpr is a parsed {{ns:Func args | into .Target}} expression.
// Paths are stored without their leading dot, matching resolve().
type handlerExpr struct {
	Prefix string
	Fn     string
	Args   []token
	Into   string
}

// isHandlerExpr reports whether an attribute value is a handler
// expression, so callers can pick this path over the value-binding one.
func isHandlerExpr(attr string) bool { return handlerExprRe.MatchString(attr) }

func parseHandlerExpr(attr string) (*handlerExpr, error) {
	m := handlerExprRe.FindStringSubmatch(attr)
	if m == nil {
		return nil, fmt.Errorf("markup: %q is not a handler expression", attr)
	}
	x := &handlerExpr{Prefix: m[1], Fn: m[2]}
	toks, err := lex(m[3])
	if err != nil {
		return nil, fmt.Errorf("markup: {{%s:%s …}}: %w", x.Prefix, x.Fn, err)
	}

	// Arguments run up to the first pipe; stages follow.
	i := 0
	for ; i < len(toks) && toks[i].kind != tokPipe; i++ {
		t := toks[i]
		if t.kind == tokBare {
			return nil, fmt.Errorf("markup: {{%s:%s …}}: bare word %q — arguments are .Paths or `literals`", x.Prefix, x.Fn, t.text)
		}
		x.Args = append(x.Args, t)
	}

	for i < len(toks) {
		i++ // step over the pipe
		if i >= len(toks) {
			return nil, fmt.Errorf("markup: {{%s:%s …}}: trailing | with no stage", x.Prefix, x.Fn)
		}
		stage := toks[i]
		if stage.kind != tokBare {
			return nil, fmt.Errorf("markup: {{%s:%s …}}: expected a pipeline stage after |, got %s", x.Prefix, x.Fn, stage.describe())
		}
		i++
		var operands []token
		for ; i < len(toks) && toks[i].kind != tokPipe; i++ {
			operands = append(operands, toks[i])
		}
		switch stage.text {
		case "into":
			if len(operands) != 1 || operands[0].kind != tokPath {
				return nil, fmt.Errorf("markup: {{%s:%s …}}: `| into` takes exactly one .Path target (multiple targets are not in v1)", x.Prefix, x.Fn)
			}
			if x.Into != "" {
				return nil, fmt.Errorf("markup: {{%s:%s …}}: more than one `| into` stage", x.Prefix, x.Fn)
			}
			x.Into = operands[0].text
		default:
			return nil, fmt.Errorf("markup: {{%s:%s …}}: unknown pipeline stage %q; v1 supports `into`", x.Prefix, x.Fn, stage.text)
		}
	}
	return x, nil
}

type tokenKind uint8

const (
	tokPath    tokenKind = iota // .Some.Path — text excludes the dot
	tokLiteral                  // `quoted` — text is the contents
	tokBare                     // an unquoted word (a stage name, or a mistake)
	tokPipe                     // |
)

type token struct {
	kind tokenKind
	text string
}

func (t token) describe() string {
	switch t.kind {
	case tokPath:
		return "path ." + t.text
	case tokLiteral:
		return "literal `" + t.text + "`"
	case tokPipe:
		return "|"
	}
	return "word " + t.text
}

// lex splits an expression body into tokens. Backtick literals are taken
// verbatim (no escapes — a literal cannot contain a backtick), which is
// why they are the quoting form: XML attributes already spend both
// quote characters.
func lex(s string) ([]token, error) {
	var toks []token
	r := []rune(s)
	for i := 0; i < len(r); {
		switch c := r[i]; {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c == '|':
			toks = append(toks, token{kind: tokPipe, text: "|"})
			i++
		case c == '`':
			j := i + 1
			for j < len(r) && r[j] != '`' {
				j++
			}
			if j >= len(r) {
				return nil, fmt.Errorf("unterminated ` literal")
			}
			toks = append(toks, token{kind: tokLiteral, text: string(r[i+1 : j])})
			i = j + 1
		default:
			j := i
			for j < len(r) && !isSep(r[j]) {
				j++
			}
			word := string(r[i:j])
			if strings.HasPrefix(word, ".") {
				path := word[1:]
				if path == "" {
					return nil, fmt.Errorf("empty path %q", word)
				}
				toks = append(toks, token{kind: tokPath, text: path})
			} else {
				toks = append(toks, token{kind: tokBare, text: word})
			}
			i = j
		}
	}
	return toks, nil
}

func isSep(c rune) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '|' || c == '`'
}

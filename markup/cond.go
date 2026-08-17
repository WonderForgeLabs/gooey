package markup

import (
	"fmt"
	"regexp"
	"strconv"

	"github.com/WonderForgeLabs/gooey/prop"
)

// The conditional-expression grammar — the third form the binding DSL
// grows, and the one that lets a page say "hidden while there is
// nothing to show" without a computed in the viewmodel:
//
//	Visibility="{{not .Empty}}"
//	IsEnabled="{{and (eq .Name ``) (eq .Email ``)}}"
//
// It is a PREDICATE grammar, not a template language. The result type
// is ALWAYS bool — never text, never a number — which is what keeps it
// type-checkable at load with no reflection:
//
//	{{not X}}              arity 1
//	{{and X Y …}}          arity ≥ 2
//	{{or  X Y …}}          arity ≥ 2
//	{{eq A B}} {{ne A B}}  arity 2
//
//	X := .Path (a *prop.Property[bool]) | ( subexpr )
//	A := .Path | `literal`
//
// Nesting is allowed, and ONLY through parentheses. That is not a
// slippery slope back to a full expression language: predicates
// compose, an `and` that cannot take an `or` is an arbitrary cliff, and
// it costs nothing to check because every subexpression's result type
// is statically bool. The line this grammar does not cross is the VALUE
// DOMAIN — there is no arithmetic, no string building, no way to
// produce anything but a bool — not tree depth.
//
// What is deliberately EXCLUDED, and why:
//
//   - Ordering (lt, gt, le, ge). Sound over int, meaningless over the
//     bool the rest of the grammar is made of, and the first operator
//     whose operand types stop being obvious from the operator name.
//     It is additive later; shipping it wrong is not.
//   - float64 in eq/ne. Exact float equality is a bug in almost every
//     document that would write it, and a tolerance is a policy this
//     layer has no way to ask about.
//   - Text output. {{if}}/{{else}} around markup is a TEMPLATE, which
//     means a build-time tree transformation and a whole second answer
//     to "what is my element vocabulary". Visibility already collapses
//     a subtree out of layout; that is the conditional the retained
//     tree is built for.
//   - Bare-word operands. `{{and A B}}` reads like the operands are
//     paths and they are not; requiring the dot keeps one spelling for
//     "read this property" across all three grammars.
//
// A conditional is disjoint from the other two forms by its first
// token: a value binding starts with `.`, a handler expression has a
// colon after its prefix, and a conditional starts with a bare operator
// word FOLLOWED BY OPERANDS. That last clause matters — a lone bare
// word ({{ nonsense }}) stays the "neither a binding nor a
// value-namespace call" error it already was, rather than becoming a
// confusing complaint about conditional functions.

// condExprRe recognizes the conditional form: {{ word operands… }}.
// The whole body including the operator is captured, then lexed, so an
// argument error can name the offending token.
//
// The operand run is `.*?` rather than a `}`-excluding class, and that
// is the division of labour this file is built on: the REGEX decides
// which grammar an attribute belongs to, and the LEXER decides whether
// it is well-formed. Excluding `}` made the regex adjudicate
// well-formedness, badly — a backtick literal containing a brace,
// {{eq .Name `a}b`}}, stopped matching here and fell through to
// whatever the call site does with a non-conditional. On Visibility
// that is parseVisibility, so the user got
//
//	attribute Visibility="{{eq .Name `a}b`}}": unknown visibility
//
// naming the wrong thing entirely. It still failed at load — the
// diagnostic was misattributed, not the check missing — but a load
// error that points at the wrong mechanism is the exact failure this
// grammar was added to remove, so it is worth the widening.
//
// Non-greedy against an anchored tail means the capture ends at the
// FIRST `}}` with only whitespace after it, which is the intended
// terminator. (?s:) so a newline inside an operand run is lexed and
// reported rather than silently unmatching; the old class admitted
// newlines too, and losing that would have been a second regression
// hidden inside the fix for the first.
var condExprRe = regexp.MustCompile(`^\s*\{\{\s*([A-Za-z_][A-Za-z0-9_]*\s+\S(?s:.*?))\s*\}\}\s*$`)

// isCondExpr reports whether an attribute value is a conditional
// expression, so callers can pick this path over the value-binding one.
//
// The handler check comes FIRST and is load-bearing: {{net : Get .Url}}
// — legal, because handlerExprRe allows space around the colon — would
// otherwise match here as the operator "net" applied to ": Get .Url".
// Asking the older grammar first makes the two disjoint by construction
// rather than by a lookahead this regexp would have to grow.
func isCondExpr(attr string) bool {
	if isHandlerExpr(attr) {
		return false
	}
	return condExprRe.MatchString(attr)
}

// condHandle compiles a conditional expression into a live
// *prop.Property[bool] resolved against this context.
//
// The handle is a COMPUTED, so two things follow. First, every operand
// read happens inside its evaluation, which is what makes the operands
// dependencies (prop.node.recordRead) — the returned handle behaves
// exactly like a hand-written computed in a viewmodel, including
// waking a paint node that reads it. Second, Set on it panics
// (prop/prop.go:119), so a conditional belongs on a ONE-WAY attribute.
//
// That second point is a hazard this grammar INHERITS rather than
// introduces, and it is worth naming here so the next reader does not
// rediscover it as a bug in this file. Checked="{{not .X}}" on a
// Checkbox loads, paints, and panics on the first click — but so does
// Checked="{{.AnyComputedBool}}", which has always been accepted. The
// primitive that would reject both at LOAD is already in the tree:
// prop.Property[T].Settable() (prop/prop.go:114). The fix belongs in
// the two-way binders, not here, and has to cover computed handles and
// conditionals in one change or it will look like conditionals caused
// it.
//
// A fresh computed per call, deliberately not memoized per (ctx, attr):
// prop.OnInvalidate holds a single hook, not a list, so sharing one
// handle between two elements that each arm an observer would silently
// drop the first one's.
func (ctx *Context) condHandle(attr string) (*prop.Property[bool], error) {
	m := condExprRe.FindStringSubmatch(attr)
	if m == nil {
		return nil, fmt.Errorf("markup: %q is not a conditional expression", attr)
	}
	toks, err := lex(m[1])
	if err != nil {
		return nil, fmt.Errorf("markup: %s: %w", attr, err)
	}
	p := &condParser{ctx: ctx, attr: attr, toks: toks}
	h, err := p.call(false)
	if err != nil {
		return nil, err
	}
	if p.i < len(p.toks) {
		return nil, p.errf("trailing %s — a nested call needs parentheses, as {{and .A (or .B .C)}}", p.toks[p.i].describe())
	}
	return h, nil
}

type condParser struct {
	ctx  *Context
	attr string
	toks []token
	i    int
}

func (p *condParser) errf(format string, args ...any) error {
	return fmt.Errorf("markup: %s: %w", p.attr, fmt.Errorf(format, args...))
}

// atEnd reports whether the current call's operand list is finished:
// at the closing paren when nested, at the end of the token stream at
// top level.
func (p *condParser) atEnd(nested bool) bool {
	if p.i >= len(p.toks) {
		return true
	}
	return nested && p.toks[p.i].kind == tokRParen
}

// call parses one operator application, leaving p.i on the token after
// it (the closing paren, when nested).
func (p *condParser) call(nested bool) (*prop.Property[bool], error) {
	if p.i >= len(p.toks) {
		return nil, p.errf("expected a conditional operator (and, or, not, eq, ne)")
	}
	t := p.toks[p.i]
	if t.kind != tokBare {
		return nil, p.errf("expected a conditional operator (and, or, not, eq, ne), got %s", t.describe())
	}
	p.i++
	switch op := t.text; op {
	case "not":
		h, err := p.boolOperand()
		if err != nil {
			return nil, err
		}
		if !p.atEnd(nested) {
			return nil, p.errf("`not` takes exactly 1 operand; write {{and X Y}} to combine two")
		}
		return condNot(h), nil

	case "and", "or":
		var kids []*prop.Property[bool]
		for !p.atEnd(nested) {
			h, err := p.boolOperand()
			if err != nil {
				return nil, err
			}
			kids = append(kids, h)
		}
		if len(kids) < 2 {
			return nil, p.errf("`%s` takes at least 2 operands, got %d", op, len(kids))
		}
		if op == "and" {
			return condAnd(kids), nil
		}
		return condOr(kids), nil

	case "eq", "ne":
		var ops []token
		for !p.atEnd(nested) {
			a := p.toks[p.i]
			if a.kind != tokPath && a.kind != tokLiteral {
				return nil, p.errf("`%s` compares .Paths and `literals`; got %s", op, a.describe())
			}
			ops = append(ops, a)
			p.i++
		}
		if len(ops) != 2 {
			return nil, p.errf("`%s` takes exactly 2 operands, got %d", op, len(ops))
		}
		return p.compare(op == "ne", ops[0], ops[1])
	}
	return nil, p.errf("unknown conditional function %q; the predicate grammar is and, or, not, eq, ne", t.text)
}

// boolOperand parses one X — a bool-handle path, or a parenthesised
// subexpression.
func (p *condParser) boolOperand() (*prop.Property[bool], error) {
	if p.i >= len(p.toks) {
		return nil, p.errf("expected an operand")
	}
	switch t := p.toks[p.i]; t.kind {
	case tokLParen:
		p.i++
		h, err := p.call(true)
		if err != nil {
			return nil, err
		}
		if p.i >= len(p.toks) || p.toks[p.i].kind != tokRParen {
			return nil, p.errf("unclosed ( — a nested call is written (and .A .B)")
		}
		p.i++
		return h, nil
	case tokPath:
		p.i++
		v, err := p.lookup(t)
		if err != nil {
			return nil, err
		}
		h, ok := v.(*prop.Property[bool])
		if !ok {
			return nil, p.errf(".%s is %T; and/or/not take a *prop.Property[bool] or a parenthesised subexpression — compare a non-bool with {{eq .%s `…`}}", t.text, v, t.text)
		}
		return h, nil
	case tokLiteral:
		return nil, p.errf("`%s` is a literal; and/or/not take bool .Paths or parenthesised subexpressions, and a literal is only an eq/ne operand", t.text)
	default:
		return nil, p.errf("unexpected %s — expected a conditional operator's operand: a bool .Path or a parenthesised subexpression", t.describe())
	}
}

func (p *condParser) lookup(t token) (any, error) {
	v, err := resolve(p.ctx.Values, t.text)
	if err != nil {
		// Wrapped, not reformatted: the control plane matches
		// *UnresolvedError with errors.As to tell "reached past the
		// grant" apart from "typo", and a conditional must not hide
		// that behind a new message.
		return nil, fmt.Errorf("markup: %s: %w", p.attr, err)
	}
	return v, nil
}

// compare builds an eq/ne node. THIS is where "type-check at load
// without reflection" is actually paid for: the PATH operand's handle
// type, recovered by a type switch, decides the comparison type, and
// the other operand is then either parsed into that type (literal) or
// asserted to the same handle type (path). Two `any`s are never
// compared — by the time == runs, T is a compile-time type.
func (p *condParser) compare(negate bool, a, b token) (*prop.Property[bool], error) {
	if a.kind == tokLiteral && b.kind == tokLiteral {
		return nil, p.errf("comparing two literals is a constant; at least one operand must be a .Path")
	}
	// Normalize so the path is on the left. eq/ne are symmetric, so
	// this loses nothing and means the type switch below is written
	// once instead of twice.
	if a.kind == tokLiteral {
		a, b = b, a
	}
	av, err := p.lookup(a)
	if err != nil {
		return nil, err
	}
	switch h := av.(type) {
	case *prop.Property[bool]:
		return condCmp(p, negate, a, h, b, parseCondBool)
	case *prop.Property[int]:
		return condCmp(p, negate, a, h, b, strconv.Atoi)
	case *prop.Property[string]:
		return condCmp(p, negate, a, h, b, func(s string) (string, error) { return s, nil })
	}
	return nil, p.errf(".%s is %T; eq/ne compare *prop.Property[bool], [int] or [string] (float equality is deliberately not in the grammar)", a.text, av)
}

// condCmp finishes one comparison once T is known.
func condCmp[T comparable](p *condParser, negate bool, at token, left *prop.Property[T], b token, parse func(string) (T, error)) (*prop.Property[bool], error) {
	if b.kind == tokLiteral {
		want, err := parse(b.text)
		if err != nil {
			var zero T
			return nil, p.errf("`%s` is not a %T: %v", b.text, zero, err)
		}
		return prop.NewComputed(func() bool { return (left.Get() == want) != negate }), nil
	}
	bv, err := p.lookup(b)
	if err != nil {
		return nil, err
	}
	right, ok := bv.(*prop.Property[T])
	if !ok {
		return nil, p.errf(".%s is %T but .%s is %T; eq/ne compare two handles of the SAME type", at.text, left, b.text, bv)
	}
	return prop.NewComputed(func() bool {
		// Both Gets hoisted for the same reason as condAnd's.
		l, r := left.Get(), right.Get()
		return (l == r) != negate
	}), nil
}

func parseCondBool(s string) (bool, error) {
	switch s {
	case "true":
		return true, nil
	case "false":
		return false, nil
	}
	// Not strconv.ParseBool: it accepts "1", "t", "TRUE", "F" …, and a
	// document that can spell a bool five ways is a document where the
	// same predicate reads differently in two files. Text bindings
	// already render a bool as exactly "true"/"false" (textSource), so
	// this is the round-trip of that.
	return false, fmt.Errorf("want `true` or `false`")
}

// condNot, condAnd and condOr are the evaluation nodes, and the shape
// of the loops is the whole point of this comment.
//
// EVERY OPERAND'S Get RUNS ON EVERY EVALUATION. The natural spelling —
// `out = out && k.Get()` — short-circuits, and prop rebuilds a
// computed's dependency set from scratch on each evaluation (Get
// detaches from p.n.deps before re-recording, prop/prop.go:87), so a
// skipped Get is a DROPPED SUBSCRIPTION, not merely a skipped read.
//
// For these particular operators that turns out to be value-safe —
// and/or are monotone, so the operand that decided the result is itself
// subscribed and will wake the node before the skipped one could matter
// (TestCondAndSurvivesTheShortCircuitMutationByValue pins that, so the
// claim is checked rather than asserted). It is still written hoisted,
// for three reasons: the safety argument is a proof about THESE
// operators that the next operator added here would silently inherit
// and might not satisfy; the dropped subscription is directly
// observable as a missing re-evaluation
// (TestCondAndKeepsEveryOperandSubscribed); and a reader should not
// have to reconstruct a monotonicity argument to know whether a line of
// this file is correct.
func condNot(x *prop.Property[bool]) *prop.Property[bool] {
	return prop.NewComputed(func() bool { return !x.Get() })
}

func condAnd(kids []*prop.Property[bool]) *prop.Property[bool] {
	return prop.NewComputed(func() bool {
		out := true
		for _, k := range kids {
			if !k.Get() { // never `out = out && k.Get()` — see above
				out = false
			}
		}
		return out
	})
}

func condOr(kids []*prop.Property[bool]) *prop.Property[bool] {
	return prop.NewComputed(func() bool {
		out := false
		for _, k := range kids {
			if k.Get() { // never `out = out || k.Get()` — see above
				out = true
			}
		}
		return out
	})
}

package markup

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/validate"
)

// Validate is the markup validation behavior — MAUI's ValidationBehavior
// in the attachment slot KeyBinding and Tooltip already occupy. It hangs
// off an input element (bare, or inside the <X.Behaviors> slot), reads
// the HOST's bound text source, and materializes the SAME validate.Field
// computed the Go API builds — one engine, two surfaces:
//
//	<TextBox Prompt="name: " Text="{{.Name}}">
//	  <TextBox.Behaviors>
//	    <Validate Required="true" MinLen="3" Into=".NameErr"/>
//	  </TextBox.Behaviors>
//	</TextBox>
//	<Text Visibility="{{.NameHasErr}}">{{.NameErr}}</Text>
//
// The rule vocabulary is .NET's DataAnnotations set (see
// validateBuiltins for the running order and docs/markup-reference.md
// for the parity table): Required, MinLen/MaxLen, Pattern,
// EmailAddress, Url, Phone, CreditCard, Digits, Integer,
// MinValue/MaxValue, and Compare. Attribute literals follow the
// propKinds grammar — bools for the fixed-shape rules, ints for
// lengths, numbers for values, a regular expression for Pattern
// (compiled and checked AT LOAD), a field path for Compare. Rules run
// in a fixed order regardless of attribute order — presence, length,
// shape, value, agreement — so the first failure is the most
// fundamental one, per validate.Field. Message="…" overrides every
// rule's default message with one field-level sentence; ctx.Rules
// covers domain rules beyond this set.
//
// Into names where the error property publishes in the page context
// (the leading dot is the binding spelling and optional): later
// bindings — an inline error <Text>, a submit gate — reach it as
// {{.NameErr}}. Omitted, it derives from the host's Text binding path:
// Text="{{.Name}}" publishes NameErr. Publication OVERWRITES an
// existing key, deliberately: a hot reload re-registers the same name
// on every rebuild, and a collision error here would make every second
// load fail.
//
// The host's Error handle is wired automatically (a TextBox flips into
// its InvalidStyle visual), so the behavior alone gives the field its
// invalid look; Error="…" and <Validate> together are ambiguous and
// refuse to load.
type Validate struct {
	gooey.Base
	// Error is the materialized field error — set at load, empty string
	// meaning valid, the handle published under Into.
	Error *prop.Property[string]

	rules []validate.Rule[string]
	into  string
}

func (v *Validate) Measure(gooey.Size) gooey.Size { return gooey.Size{} }
func (v *Validate) Render(*gooey.Frame)           {}
func (v *Validate) NonVisual() bool               { return true }

// RuleFunc builds one registered rule from its attribute literal. It
// runs at LOAD, once per <Validate> that names the rule, so a bad
// argument is a load error and an expensive setup (compiling a pattern)
// happens per document, never per keystroke.
type RuleFunc func(arg string) (validate.Rule[string], error)

// validateBuiltins is the fixed half of the <Validate> vocabulary — the
// DataAnnotations set — in the order the rules RUN, which is also the
// order a person fixing the field should hear about problems:
// presence, then length, then shape, then value, then agreement. (Into
// and Message are not rules but names the element owns.)
//
// One spelling per rule, deliberately: markup says Pattern, not
// RegularExpression. The annotation's name is longer, and gooey has one
// canonical spelling per concept everywhere else (one gesture syntax,
// one Style attribute) — an alias would double the vocabulary a reader
// has to recognize to buy nothing. The parity table in
// docs/markup-reference.md names the annotation each attribute answers.
var validateBuiltins = []string{
	"Required",
	"MinLen", "MaxLen",
	"Pattern", "EmailAddress", "Url", "Phone", "CreditCard", "Digits", "Integer",
	"MinValue", "MaxValue",
	"Compare",
	"Into", "Message",
}

// boolRules are the fixed-shape annotation rules: the attribute's value
// is a bool, and true adds the rule.
var boolRules = map[string]func(msg string) validate.Rule[string]{
	"EmailAddress": validate.EmailAddress,
	"Url":          validate.URL,
	"Phone":        validate.Phone,
	"CreditCard":   validate.CreditCard,
	"Digits":       validate.Digits,
	"Integer":      validate.Integer,
}

// boolRuleOrder is the order they run in, independent of attribute
// order.
var boolRuleOrder = []string{"EmailAddress", "Url", "Phone", "CreditCard", "Digits", "Integer"}

// validateRuleNames is every name a <Validate> attribute may use in
// this context — the built-ins plus the registered rules, for the
// unknown-attribute error.
func validateRuleNames(ctx *Context) string {
	names := append([]string{}, validateBuiltins...)
	reg := make([]string, 0, len(ctx.Rules))
	for n := range ctx.Rules {
		reg = append(reg, n)
	}
	sort.Strings(reg)
	return strings.Join(append(names, reg...), ", ")
}

// buildValidate parses the rule attributes. The host is not known yet —
// children build before their parent — so the result carries parsed
// rules until the host's builder calls wireValidate. Rule order is
// fixed regardless of attribute order (XML attributes carry none):
// built-ins first — Required, then MinLen/MaxLen, then Pattern — then
// registered rules (ctx.Rules) in name order.
func buildValidate(e Element, ctx *Context) (*Validate, error) {
	v := &Validate{}
	minLen, maxLen := 0, 0
	builtin := map[string]bool{}
	for _, n := range validateBuiltins {
		builtin[n] = true
	}
	for name, raw := range e.Attrs {
		if builtin[name] {
			continue
		}
		if _, ok := ctx.Rules[name]; ok {
			continue
		}
		return nil, fmt.Errorf("markup: <Validate %s=%q>: unknown rule (have %s)", name, raw, validateRuleNames(ctx))
	}
	// Message is a FIELD-level override: every rule on this behavior
	// reports it instead of its own default, which is how a form says
	// "e-mail address, please" once rather than leaking which check
	// tripped. Per-rule messages are a Go-side validate.Field away.
	msg := e.Attrs["Message"]
	if raw, ok := e.Attrs["Required"]; ok {
		req, err := parseRuleBool("Required", raw)
		if err != nil {
			return nil, err
		}
		if req {
			v.rules = append(v.rules, validate.Required(msg))
		}
	}
	var err error
	if raw, ok := e.Attrs["MinLen"]; ok {
		if minLen, err = strconv.Atoi(raw); err != nil {
			return nil, fmt.Errorf("markup: <Validate MinLen=%q>: want an int", raw)
		}
	}
	if raw, ok := e.Attrs["MaxLen"]; ok {
		if maxLen, err = strconv.Atoi(raw); err != nil {
			return nil, fmt.Errorf("markup: <Validate MaxLen=%q>: want an int", raw)
		}
	}
	if minLen > 0 || maxLen > 0 {
		v.rules = append(v.rules, validate.Len(minLen, maxLen, msg))
	}
	if raw, ok := e.Attrs["Pattern"]; ok {
		// Checked here so a bad expression is a LOAD error naming the
		// element, not a construction panic from validate.Pattern.
		if _, err := regexp.Compile(raw); err != nil {
			return nil, fmt.Errorf("markup: <Validate Pattern=%q>: %v", raw, err)
		}
		v.rules = append(v.rules, validate.Pattern(raw, msg))
	}
	// The fixed-shape annotation rules, in their declared order.
	for _, name := range boolRuleOrder {
		raw, ok := e.Attrs[name]
		if !ok {
			continue
		}
		on, err := parseRuleBool(name, raw)
		if err != nil {
			return nil, err
		}
		if on {
			v.rules = append(v.rules, boolRules[name](msg))
		}
	}
	// MinValue/MaxValue are RangeAttribute over a text field: either
	// bound alone is legal, and the pair becomes one rule.
	minV, maxV := math.Inf(-1), math.Inf(1)
	haveNum := false
	for _, b := range []struct {
		name string
		into *float64
	}{{"MinValue", &minV}, {"MaxValue", &maxV}} {
		raw, ok := e.Attrs[b.name]
		if !ok {
			continue
		}
		f, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
		if err != nil {
			return nil, fmt.Errorf("markup: <Validate %s=%q>: want a number", b.name, raw)
		}
		*b.into = f
		haveNum = true
	}
	if haveNum {
		if minV > maxV {
			return nil, fmt.Errorf("markup: <Validate MinValue=%q MaxValue=%q>: the range is empty", e.Attrs["MinValue"], e.Attrs["MaxValue"])
		}
		v.rules = append(v.rules, validate.NumberRange(minV, maxV, msg))
	}
	// Compare names the OTHER field's binding path: the rule reads that
	// property, which is what subscribes this field to it.
	if raw, ok := e.Attrs["Compare"]; ok {
		other, err := comparePath(raw, ctx)
		if err != nil {
			return nil, err
		}
		v.rules = append(v.rules, validate.Compare(other, msg))
	}
	// Registered rules, in name order for determinism. The constructor
	// may reject its argument — a typed load error naming the element.
	reg := make([]string, 0, len(ctx.Rules))
	for n := range ctx.Rules {
		if _, ok := e.Attrs[n]; ok && !builtin[n] {
			reg = append(reg, n)
		}
	}
	sort.Strings(reg)
	for _, n := range reg {
		rule, err := ctx.Rules[n](e.Attrs[n])
		if err != nil {
			return nil, fmt.Errorf("markup: <Validate %s=%q>: %v", n, e.Attrs[n], err)
		}
		if rule != nil {
			v.rules = append(v.rules, rule)
		}
	}
	if raw, ok := e.Attrs["Into"]; ok {
		name := strings.TrimPrefix(raw, ".")
		if name == "" || strings.Contains(name, ".") {
			return nil, fmt.Errorf("markup: <Validate Into=%q>: want a single context name like \".NameErr\"", raw)
		}
		v.into = name
	}
	return v, nil
}

// wireValidate is the host side: called by an input element's builder
// with its bound text source. It materializes the field computed,
// publishes it under Into (derived from textPath when Into is absent),
// and returns the handle for the host's own Error slot. Host-generic on
// purpose: a future input component wires the same way.
func wireValidate(v *Validate, host string, src *prop.Property[string], textPath string, ctx *Context) (*prop.Property[string], error) {
	if src == nil {
		return nil, fmt.Errorf("markup: <%s> has no bound text source for <Validate> to watch", host)
	}
	field := validate.Field(src, v.rules...)
	v.Error = field
	into := v.into
	if into == "" {
		if textPath == "" || strings.Contains(textPath, ".") {
			return nil, fmt.Errorf("markup: <%s><Validate> cannot derive a context name from Text=%q; say Into=\".SomeErr\"", host, textPath)
		}
		into = textPath + "Err"
	}
	if ctx.Values == nil {
		ctx.Values = map[string]any{}
	}
	ctx.Values[into] = field
	return field, nil
}

// parseRuleBool reads a rule's on/off literal, naming the element in the
// error the way every other markup literal does.
func parseRuleBool(name, raw string) (bool, error) {
	b, err := strconv.ParseBool(strings.TrimSpace(raw))
	if err != nil {
		return false, fmt.Errorf("markup: <Validate %s=%q>: want a bool", name, raw)
	}
	return b, nil
}

// comparePath resolves Compare="{{.Password}}" or the terser
// Compare=".Password" to the other field's handle. Both spellings are
// accepted because this attribute names a PROPERTY rather than carrying
// a value, and a reader reaching for the binding braces should find
// them working.
func comparePath(raw string, ctx *Context) (*prop.Property[string], error) {
	path := bindingPath(raw)
	if path == "" {
		path = strings.TrimPrefix(strings.TrimSpace(raw), ".")
	}
	if path == "" {
		return nil, fmt.Errorf("markup: <Validate Compare=%q>: name the other field, e.g. Compare=\".Password\"", raw)
	}
	val, err := resolve(ctx.Values, path)
	if err != nil {
		return nil, fmt.Errorf("markup: <Validate Compare=%q>: %w", raw, err)
	}
	other, ok := val.(*prop.Property[string])
	if !ok {
		return nil, fmt.Errorf("markup: <Validate Compare=%q> is %T; need *prop.Property[string]", raw, val)
	}
	return other, nil
}

// bindingPath is the bare path of a single {{.Path}} binding attribute,
// or "" — what Into derivation works from.
func bindingPath(attr string) string {
	m := bindRe.FindStringSubmatch(attr)
	if m == nil {
		return ""
	}
	return m[1]
}

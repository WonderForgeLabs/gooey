package markup

import (
	"fmt"
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
// Rule attributes follow the propKinds literal grammar (Required is a
// bool, MinLen/MaxLen ints, Pattern a regular expression checked at
// load) and run in a fixed order regardless of attribute order:
// Required, then MinLen/MaxLen, then Pattern — fundamentals before
// specifics, the validate.Field first-failure rule.
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

// validateBuiltins is the fixed half of the <Validate> vocabulary, in
// the order the rules run (Into is not a rule, but it is a name the
// element owns).
var validateBuiltins = []string{"Required", "MinLen", "MaxLen", "Pattern", "Into"}

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
	if raw, ok := e.Attrs["Required"]; ok {
		req, err := strconv.ParseBool(raw)
		if err != nil {
			return nil, fmt.Errorf("markup: <Validate Required=%q>: want a bool", raw)
		}
		if req {
			v.rules = append(v.rules, validate.Required(""))
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
		v.rules = append(v.rules, validate.Len(minLen, maxLen, ""))
	}
	if raw, ok := e.Attrs["Pattern"]; ok {
		// Checked here so a bad expression is a LOAD error naming the
		// element, not a construction panic from validate.Pattern.
		if _, err := regexp.Compile(raw); err != nil {
			return nil, fmt.Errorf("markup: <Validate Pattern=%q>: %v", raw, err)
		}
		v.rules = append(v.rules, validate.Pattern(raw, ""))
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

// bindingPath is the bare path of a single {{.Path}} binding attribute,
// or "" — what Into derivation works from.
func bindingPath(attr string) string {
	m := bindRe.FindStringSubmatch(attr)
	if m == nil {
		return ""
	}
	return m[1]
}

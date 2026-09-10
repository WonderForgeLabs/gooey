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

// unboundedWhy names what a non-finite bound actually does, which is
// three different things and not one.
//
// It exists because the refusal's first version said all four cases
// "can never fire". That is true of NaN and exactly backwards for
// MinValue="+Inf" and MaxValue="-Inf", which fire on every value there
// is — an author told the opposite of what their document does goes
// looking in the wrong place. Raised in review of #470.
func unboundedWhy(name string, f float64) string {
	switch {
	case math.IsNaN(f):
		return "a rule that never fires, because every comparison against NaN is false"
	case (name == "MinValue") == math.IsInf(f, -1):
		// -Inf as a MINIMUM, +Inf as a MAXIMUM: the default, written out.
		return "the bound you already had — it is the default this attribute " +
			"carries when it is absent, so it declares nothing"
	default:
		return "a rule nothing can satisfy — it fires on every value there is, " +
			"so the field can never become valid"
	}
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
		// Name is UNIVERSAL, not a rule. Context.vocabulary permits it on
		// every element unconditionally (attrcheck.go), so the loader
		// said yes and this loop said "unknown rule (have Required,
		// MinLen, …)" — one vocabulary answering two ways about the same
		// attribute, which is what #460 is about. Found by the sweep
		// harness in review of #470, when every probe started naming
		// itself and <Validate> was the only element that refused.
		if name == "Name" || builtin[name] {
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
	// MinLen/MaxLen through the HOUSE INT READER, for the reason
	// parseRuleBool now goes through the house bool one. These were a
	// bare strconv.Atoi on the UNTRIMMED value: MinLen=" 3 " was a load
	// error while Gap=" 3 " was fine, MinLen="-3" loaded and meant a
	// negative minimum, and MinLen="+3" loaded — three ways for the same
	// declared KindInt/BindsLiteral attribute to disagree with every
	// other one. Found while fixing the leading-+ finding in review of
	// #470, which is the same defect one element over.
	var err error
	for _, b := range []struct {
		name string
		into *int
	}{{"MinLen", &minLen}, {"MaxLen", &maxLen}} {
		raw, ok := e.Attrs[b.name]
		if !ok {
			continue
		}
		if *b.into, err = litInt(e, b.name); err != nil {
			return nil, err
		}
		// ZERO IS NOT A LENGTH BOUND, and writing one installed nothing
		// at all. validate.Len reads 0 as "no bound in this direction",
		// which is what makes either half optional — so <Validate
		// MaxLen="0"/> parsed, passed every check, and produced a rule
		// list with no length rule in it. The author asked for "must be
		// empty" and got no validation whatsoever, with no error
		// anywhere.
		//
		// MinLen="0" is the same shape from the other side: it is the
		// default spelled out, so it also declares nothing. Accepted-
		// but-ignored markup is the failure mode this package refuses,
		// and a bound that cannot be expressed has to say so rather
		// than be dropped. Raised in review of #470.
		if *b.into == 0 {
			return nil, fmt.Errorf("markup: <Validate %s=%q>: a length bound has to "+
				"be positive — validate.Len reads 0 as \"no bound in this "+
				"direction\", which is how the other half of the pair is made "+
				"optional, so this installs no rule at all", b.name, raw)
		}
	}
	// AN INVERTED PAIR IS A RULE NOTHING CAN SATISFY, and this block had
	// the zero refusal above and not this one while the NUMERIC block
	// thirty lines down had exactly this one.
	//
	// validate.Len tests `n < min || (max > 0 && n > max)`, so
	// MinLen="5" MaxLen="3" rejects every non-empty value there is and
	// the field can never become valid. That is verbatim the third
	// outcome unboundedWhy was written to name, and the commit that
	// added the zero refusal added it to this very loop and stopped.
	// Same class, same element, same block, opposite answer for lengths
	// and numbers. Raised in review of #470.
	//
	// Both bounds have to be POSITIVE for the comparison to mean
	// anything: a missing bound is 0, and 0 is how validate.Len spells
	// "no bound in this direction", so `minLen > maxLen` alone would
	// fire on a lone MinLen. The zero refusal above means a bound that
	// is present is never 0, so this reads as "both present".
	if minLen > 0 && maxLen > 0 && minLen > maxLen {
		return nil, fmt.Errorf("markup: <Validate MinLen=%q MaxLen=%q>: the range "+
			"is empty — validate.Len rejects anything shorter than the minimum or "+
			"longer than the maximum, so with the minimum above the maximum every "+
			"non-empty value fails and the field can never become valid",
			e.Attrs["MinLen"], e.Attrs["MaxLen"])
	}
	if minLen > 0 || maxLen > 0 {
		v.rules = append(v.rules, validate.Len(minLen, maxLen, msg))
	}
	if raw, ok := e.Attrs["Pattern"]; ok {
		// THE EMPTY EXPRESSION COMPILES, and it matches at every
		// position of every string — so <Validate Pattern=""/> is a rule
		// that can never fire, installed and running. It is the same
		// class as a NaN bound below and refused for the same reason:
		// nothing downstream can tell it from a pattern the field
		// happens to satisfy.
		//
		// raw == "", not TrimSpace(raw) == "". A Pattern of one space is
		// an ordinary expression that matches a space, and trimming
		// would refuse it. Raised in review of #470.
		if raw == "" {
			return nil, fmt.Errorf("markup: <Validate Pattern=\"\">: the empty " +
				"expression matches every string, so this installs a rule that can " +
				"never fire — write the expression, or drop the attribute")
		}
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
		// EMPTY IS NOT UNREADABLE, and this reader gave both the same
		// sentence — the distinction litIntGrammar spent a commit
		// drawing, stopping one element short of the <Validate> bounds.
		// A single ParseFloat cannot tell them apart, so the split has
		// to be here. Raised in review of #470.
		if strings.TrimSpace(raw) == "" {
			return nil, fmt.Errorf("markup: <Validate %s=%q>: %s",
				b.name, raw, emptyLiteralWhy)
		}
		f, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
		if err != nil {
			return nil, fmt.Errorf("markup: <Validate %s=%q>: want a number", b.name, raw)
		}
		// NaN AND Inf PARSE, and neither is a rule. The refusal is one
		// sentence; the CONSEQUENCE is not, and the first version of
		// this message got it backwards for half its own cases by
		// saying every one of them "can never fire".
		//
		// NaN never fires: every comparison against it is false, so the
		// field validates whatever is typed and the marker never
		// appears. An infinity fires in whichever direction it points —
		// MinValue="-Inf" and MaxValue="+Inf" are the defaults spelled
		// out, declaring nothing, while MinValue="+Inf" and
		// MaxValue="-Inf" fire on EVERYTHING, so the field can never be
		// valid. Three outcomes, none of them a bound, and the message
		// now names the one the author actually wrote.
		//
		// The empty-range check below cannot see the NaN case for the
		// same reason the rule cannot — NaN > NaN is false — so it has
		// to be refused here. Raised in review of #470, twice.
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return nil, fmt.Errorf("markup: <Validate %s=%q>: a bound has to be a "+
				"finite number, and %s is %s. Nothing downstream would refuse it: "+
				"the empty-range check beside this one compares the two bounds, and "+
				"a comparison against NaN is false whichever way it is written",
				b.name, raw, strings.TrimSpace(raw), unboundedWhy(b.name, f))
		}
		// THE CANONICAL-SPELLING RULE THAT GOVERNS INTS IS DELIBERATELY
		// NOT APPLIED HERE, and the asymmetry is a decision rather than
		// an omission. A whole number has exactly one honest spelling,
		// so "007" is a second way to write 7 and nothing else. A float
		// has several: "1.50" says something about precision and "1e6"
		// something about scale, and refusing them would refuse an
		// author writing the bound the way the domain writes it.
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

// parseRuleBool reads a rule's on/off literal through THE HOUSE BOOL
// GRAMMAR — "true" or "false", nothing else.
//
// It read strconv.ParseBool until review of #470, which is the same
// defect optBool had one file over and the same argument litBool makes:
// a bool the document can spell five ways is a bool that reads
// differently in two files. <Validate Required> is a declared
// KindBool/BindsLiteral component attribute like any other, so
// <Validate Required="1"> loading while <ProgressBar Thresholds="1">
// was a load error is one vocabulary answering two ways.
//
// It was not merely inconsistent, it was UNGUARDED. The sweep arm
// written to catch exactly this counted <Validate> probes as verified
// while every one of them was failing on "<HStack> does not support
// <Validate>" — the attachment needs an input host, and the harness gave
// it a stack. Seven attributes' worth of false credit, in the arm whose
// job was the strictness. The harness hosts a <Validate> in a <TextBox>
// now and the arm reports what it could not reach, which is how this
// became reproducible.
//
// Delegates to litBool rather than restating the switch: the value of
// one grammar is that there is one implementation of it. The element is
// always <Validate>, so the synthetic Element is exact, not a stand-in.
//
// The readers that are NOT component attributes are still ParseBool and
// are deliberately untouched here — property.go's kindOf("bool"),
// resources.go, companion.go's environment variable, and this file's own
// declared Default. Whether they should agree is #473.
func parseRuleBool(name, raw string) (bool, error) {
	return litBool(Element{Name: "Validate", Attrs: map[string]string{name: raw}}, name)
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

package markup

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

// The element vocabulary, one ElementDef per element.
//
// Each literal carries what may be set on the element AND the code that
// reads it. That adjacency is the whole point: a new attribute is added
// by editing one expression, and forgetting to declare it means editing
// half of something you are looking at. See ElementDef's doc comment for
// why proximity rather than a drift test is the mechanism.
//
// Build returns the RAW component. Naming (Context.Named) is applied
// once by the dispatcher, so no arm repeats it.

func init() {
	registerElements(
		defText,
		defButton,
		defCompanion,
		defValidate,
		defTab,
		defBorder,
		defGrid,
		defVStack,
		defHStack,
		defCanvas,
		defItemsView,
		defCheckbox,
		defGauge,
		defSparkline,
		defTextBox,
		defColorPicker,
		defProgressBar,
		defSpinner,
		defToggle,
		defSegmented,
		defStatusBar,
		defTabs,
		defButtonBar,
		defMenuBar,
		defToastHost,
		defAdornmentLayer,
		defTooltip,
		defValidationMarker,
		defKeyBinding,
		defTimer,
		defTypeAhead,
		defImage,
	)
}

var defText = &ElementDef{
	Name:  "Text",
	Proto: &components.Text{},
	Known: true,
	Doc:   "A run of text. The content is the element's body, not an attribute.",
	Attrs: []AttrSpec{
		{Name: "Bold", Kind: KindBool, Binds: BindsLiteral, Default: "false", Origin: OriginBuiltin},
		{Name: "Style", Kind: KindStyle, Binds: BindsEither, Origin: OriginBuiltin},
	},
	Children: ChildSpec{Mode: ModeLeaf},
	Build: func(e Element, ctx *Context) (gooey.Component, error) {
		style, err := bindStyle(e, ctx)
		if err != nil {
			return nil, err
		}
		if e.Attrs["Bold"] == "true" {
			// Bold composes over either form of Style, so it wraps the
			// handle rather than mutating a value — a bound style stays
			// live and still gets its bold.
			base := style
			style = prop.NewComputed(func() render.Style {
				s := base.Get()
				s.Bold = true
				return s
			})
		}
		t := &components.Text{Style: style}
		content := strings.TrimSpace(e.Text)
		if src, err := bindText(content, ctx); err != nil {
			return nil, err
		} else if src != nil {
			t.Content = src
		} else {
			t.Content = components.Str(content)
		}
		return t, nil
	},
}

var defButton = &ElementDef{
	Name:  "Button",
	Proto: &components.Button{},
	Known: true,
	Doc:   "A clickable button. Its label is Content; nested text is ignored.",
	Attrs: []AttrSpec{
		{Name: "Chrome", Kind: KindEnum, Binds: BindsLiteral, Enum: components.ButtonChromeNames, Default: "cell", Origin: OriginBuiltin},
		{Name: "Click", Kind: KindCommand, Binds: BindsEither, Origin: OriginBuiltin},
		{Name: "Content", Kind: KindText, Binds: BindsEither, Origin: OriginBuiltin},
		{Name: "Style", Kind: KindStyle, Binds: BindsEither, Origin: OriginBuiltin},
	},
	Children: ChildSpec{Mode: ModeAttachments},
	Build: func(e Element, ctx *Context) (gooey.Component, error) {
		// A Button takes no visual children, but non-visual attachments
		// — <Tooltip>, <KeyBinding> — hang off it the way they hang off
		// any container (issue #92's canonical form).
		kids, attach, err := buildChildren(e, ctx)
		if err != nil {
			return nil, err
		}
		if len(kids) > 0 {
			return nil, fmt.Errorf("markup: <Button> takes no visual children; only attachments like <Tooltip> and <KeyBinding> may nest here")
		}
		content, err := bindText(e.Attrs["Content"], ctx)
		if err != nil {
			return nil, err
		}
		if content == nil {
			content = components.Str(e.Attrs["Content"])
		}
		click, err := ctx.Command(e.Attrs["Click"])
		if err != nil {
			return nil, fmt.Errorf("markup: <Button Click=%q>: %w", e.Attrs["Click"], err)
		}
		style, err := bindStyle(e, ctx)
		if err != nil {
			return nil, err
		}
		chrome, ok := components.ParseButtonChrome(e.Attrs["Chrome"])
		if !ok {
			return nil, fmt.Errorf("markup: <Button Chrome=%q>: unknown chrome; want one of %s",
				e.Attrs["Chrome"], strings.Join(components.ButtonChromeNames, ", "))
		}
		b := &components.Button{
			Content: content,
			Style:   style,
			Click:   click,
			Chrome:  chrome,
		}
		if err := attachAll(e, b, attach); err != nil {
			return nil, err
		}
		return b, nil
	},
}

// defCompanion is one of the two elements that ALREADY declared a
// vocabulary before this restructure. Its table moves here verbatim, and
// checkCompanionAttrs keeps validating against companionAttrs — the two
// must agree, which TestCompanionDefMatchesItsLegacyTable pins.
//
// Note what it deliberately omits: the layout attributes. A non-visual
// element has no bounds to place, and that omission is preserved by
// TakesLayout rather than by this list.
var defCompanion = &ElementDef{
	Name:  "Companion",
	Proto: &components.Companion{},
	Known: true,
	Doc:   "Runs a child process for the life of the page. It names a binary: read companion.go before changing it.",
	Attrs: []AttrSpec{
		{Name: "CleanEnv", Kind: KindBool, Binds: BindsLiteral, Origin: OriginBuiltin},
		{Name: "Dir", Kind: KindString, Binds: BindsLiteral, Origin: OriginBuiltin},
		{Name: "Error", Kind: KindBinding, Binds: BindsBinding, GoType: "string", Origin: OriginBuiltin},
		{Name: "Exited", Kind: KindCommand, Binds: BindsEither, Origin: OriginBuiltin},
		{Name: "KillDelay", Kind: KindDuration, Binds: BindsLiteral, Origin: OriginBuiltin},
		{Name: "Log", Kind: KindString, Binds: BindsLiteral, Origin: OriginBuiltin},
		{Name: "Path", Kind: KindString, Binds: BindsLiteral, Origin: OriginBuiltin},
		{Name: "StopTimeout", Kind: KindDuration, Binds: BindsLiteral, Origin: OriginBuiltin},
	},
	Slots:    []SlotSpec{{Name: "Args"}, {Name: "Env"}},
	Children: ChildSpec{Mode: ModeRestricted, Only: []string{"Arg", "Var"}},
	Build: func(e Element, ctx *Context) (gooey.Component, error) {
		return buildCompanion(e, ctx)
	},
}

// defValidate is the ONE element whose accepted attribute names depend
// on the Context — measured across all builtin arms, exactly one.
//
// Attrs below is the BUILTIN half only. Context.Rules supplies the rest
// at Catalog() time, which is what Open declares. Flattening this to the
// fifteen literals would not merely under-report: with unknown
// attributes rejected, a host that registers an Email rule would find
// <Validate Email="true"/> REFUSED. TestOpenVocabularyElementAcceptsContextRules
// is red if that ever happens.
var defValidate = &ElementDef{
	Name:         "Validate",
	Proto:        &Validate{},
	Known:        true,
	Open:         true,
	Doc:          "A validation behavior on an input. Its rule vocabulary is the builtins plus Context.Rules.",
	Attrs:        validateBuiltinAttrs(),
	DynamicAttrs: "rules are consumed by ranging over e.Attrs and checked against validateBuiltins ∪ ctx.Rules, so no read names a literal",
	Children:     ChildSpec{Mode: ModeNone},
	Build: func(e Element, ctx *Context) (gooey.Component, error) {
		// Non-visual like KeyBinding; the HOST's builder wires it to its
		// bound text source (wireValidate) — building it here only parses
		// the rule attributes.
		if len(e.Children) > 0 {
			return nil, fmt.Errorf("markup: <Validate> takes no children")
		}
		return buildValidate(e, ctx)
	},
}

// validateBuiltinAttrs derives the builtin half from validateBuiltins,
// the list buildValidate itself checks against, so the two cannot
// disagree. The rule kinds differ (MinLen is an int, Required a bool,
// Pattern a regexp) and are named here because the loop that consumes
// them cannot say so.
func validateBuiltinAttrs() []AttrSpec {
	kinds := map[string]Kind{
		"MinLen": KindInt, "MaxLen": KindInt,
		"Required": KindBool, "EmailAddress": KindBool, "Url": KindBool,
		"Phone": KindBool, "CreditCard": KindBool, "Digits": KindBool,
		"Integer": KindBool,
	}
	out := make([]AttrSpec, 0, len(validateBuiltins))
	for _, n := range validateBuiltins {
		k, ok := kinds[n]
		if !ok {
			k = KindString
		}
		out = append(out, AttrSpec{Name: n, Kind: k, Binds: BindsLiteral, Origin: OriginBuiltin})
	}
	return out
}

var defTab = &ElementDef{
	Name:     "Tab",
	Known:    false,
	Opaque:   "a pseudo-element: <Tabs> parses a <Tab>'s Header and content itself, so this definition exists only to reject one used anywhere else",
	Children: ChildSpec{Mode: ModeUnknown},
	Build: func(e Element, ctx *Context) (gooey.Component, error) {
		return nil, fmt.Errorf("markup: <Tab> is only valid directly inside <Tabs>")
	},
}

var defBorder = &ElementDef{
	Name:  "Border",
	Proto: &components.Border{},
	Known: true,
	Attrs: []AttrSpec{
		{Name: "Background", Kind: KindColor, Binds: BindsEither, Origin: OriginBuiltin},
		{Name: "Style", Kind: KindStyle, Binds: BindsEither, Origin: OriginBuiltin},
		{Name: "Title", Kind: KindText, Binds: BindsEither, Origin: OriginBuiltin},
	},
	Children: ChildSpec{Mode: ModeOne},
	Build: func(e Element, ctx *Context) (gooey.Component, error) {
		kids, attach, err := buildChildren(e, ctx)
		if err != nil {
			return nil, err
		}
		if len(kids) != 1 {
			return nil, fmt.Errorf("markup: <Border> needs exactly one child")
		}
		child := kids[0]
		title, err := bindText(e.Attrs["Title"], ctx)
		if err != nil {
			return nil, err
		}
		if title == nil {
			title = components.Str(e.Attrs["Title"])
		}
		style, err := bindStyle(e, ctx)
		if err != nil {
			return nil, err
		}
		background, err := bindColor(e, ctx, "Background")
		if err != nil {
			return nil, err
		}
		b := &components.Border{
			Child:      child,
			Title:      title,
			Style:      style,
			Background: background,
		}
		if err := attachAll(e, b, attach); err != nil {
			return nil, err
		}
		return b, nil
	},
}

var defGrid = &ElementDef{
	Name:  "Grid",
	Proto: &components.Grid{},
	Known: true,
	Attrs: []AttrSpec{
		{Name: "Background", Kind: KindColor, Binds: BindsEither, Origin: OriginBuiltin},
		{Name: "Cols", Kind: KindGridLens, Binds: BindsLiteral, Origin: OriginBuiltin},
		{Name: "Rows", Kind: KindGridLens, Binds: BindsLiteral, Origin: OriginBuiltin},
	},
	Children: ChildSpec{Mode: ModeMany},
	Build: func(e Element, ctx *Context) (gooey.Component, error) {
		rows, err := components.ParseGridLens(e.Attrs["Rows"])
		if err != nil {
			return nil, err
		}
		cols, err := components.ParseGridLens(e.Attrs["Cols"])
		if err != nil {
			return nil, err
		}
		kids, attach, err := buildChildren(e, ctx)
		if err != nil {
			return nil, err
		}
		background, err := bindColor(e, ctx, "Background")
		if err != nil {
			return nil, err
		}
		g := &components.Grid{Rows: rows, Cols: cols, Children: kids, Background: background}
		if err := attachAll(e, g, attach); err != nil {
			return nil, err
		}
		return g, nil
	},
}

var defVStack = &ElementDef{
	Name:  "VStack",
	Proto: &components.VStack{},
	Known: true,
	Attrs: []AttrSpec{
		{Name: "Background", Kind: KindColor, Binds: BindsEither, Origin: OriginBuiltin},
		{Name: "Gap", Kind: KindInt, Binds: BindsLiteral, Default: "0", Origin: OriginBuiltin},
	},
	Children: ChildSpec{Mode: ModeMany},
	Build: func(e Element, ctx *Context) (gooey.Component, error) {
		gap, _ := strconv.Atoi(e.Attrs["Gap"])
		kids, attach, err := buildChildren(e, ctx)
		if err != nil {
			return nil, err
		}
		background, err := bindColor(e, ctx, "Background")
		if err != nil {
			return nil, err
		}
		var w gooey.Component = &components.HStack{Children: kids, Gap: gap, Background: background}
		if e.Name == "VStack" {
			w = &components.VStack{Children: kids, Gap: gap, Background: background}
		}
		if err := attachAll(e, w, attach); err != nil {
			return nil, err
		}
		return w, nil
	},
}

var defHStack = &ElementDef{
	Name:  "HStack",
	Proto: &components.HStack{},
	Known: true,
	Attrs: []AttrSpec{
		{Name: "Background", Kind: KindColor, Binds: BindsEither, Origin: OriginBuiltin},
		{Name: "Gap", Kind: KindInt, Binds: BindsLiteral, Default: "0", Origin: OriginBuiltin},
	},
	Children: ChildSpec{Mode: ModeMany},
	Build: func(e Element, ctx *Context) (gooey.Component, error) {
		gap, _ := strconv.Atoi(e.Attrs["Gap"])
		kids, attach, err := buildChildren(e, ctx)
		if err != nil {
			return nil, err
		}
		background, err := bindColor(e, ctx, "Background")
		if err != nil {
			return nil, err
		}
		var w gooey.Component = &components.HStack{Children: kids, Gap: gap, Background: background}
		if e.Name == "VStack" {
			w = &components.VStack{Children: kids, Gap: gap, Background: background}
		}
		if err := attachAll(e, w, attach); err != nil {
			return nil, err
		}
		return w, nil
	},
}

var defCanvas = &ElementDef{
	Name:  "Canvas",
	Proto: &components.Canvas{},
	Known: true,
	Attrs: []AttrSpec{
		{Name: "Background", Kind: KindColor, Binds: BindsEither, Origin: OriginBuiltin},
	},
	Children: ChildSpec{Mode: ModeMany},
	Build: func(e Element, ctx *Context) (gooey.Component, error) {
		// Children carry their own Canvas.Left/Canvas.Top, parsed into
		// Layout by applyLayout like any other attached property.
		kids, attach, err := buildChildren(e, ctx)
		if err != nil {
			return nil, err
		}
		background, err := bindColor(e, ctx, "Background")
		if err != nil {
			return nil, err
		}
		c := &components.Canvas{Children: kids, Background: background}
		if err := attachAll(e, c, attach); err != nil {
			return nil, err
		}
		return c, nil
	},
}

var defItemsView = &ElementDef{
	Name:  "ItemsView",
	Proto: &components.ItemsView{},
	Known: true,
	Attrs: []AttrSpec{
		{Name: "Activate", Kind: KindCommand, Binds: BindsEither, Origin: OriginBuiltin},
		{Name: "Focusable", Kind: KindBool, Binds: BindsLiteral, Origin: OriginBuiltin},
		{Name: "Items", Kind: KindBinding, Binds: BindsBinding, GoType: "components.ItemSource", Required: true, Origin: OriginBuiltin},
		{Name: "Selected", Kind: KindBinding, Binds: BindsBinding, GoType: "int", Origin: OriginBuiltin},
		{Name: "SelectionChanged", Kind: KindCommand, Binds: BindsEither, Origin: OriginBuiltin},
	},
	Slots: []SlotSpec{
		{Name: "ItemTemplate", Required: true},
	},
	Children: ChildSpec{Mode: ModeAttachments},
	Build: func(e Element, ctx *Context) (gooey.Component, error) {
		v, err := buildItemsView(e, ctx)
		return v, err
	},
}

var defCheckbox = &ElementDef{
	Name:  "Checkbox",
	Proto: &components.Checkbox{},
	Known: true,
	Attrs: []AttrSpec{
		{Name: "Checked", Kind: KindBinding, Binds: BindsBinding, GoType: "bool", Required: true, Origin: OriginBuiltin},
		{Name: "Label", Kind: KindText, Binds: BindsEither, Origin: OriginBuiltin},
		{Name: "Style", Kind: KindStyle, Binds: BindsEither, Origin: OriginBuiltin},
	},
	Children: ChildSpec{Mode: ModeLeaf},
	Build: func(e Element, ctx *Context) (gooey.Component, error) {
		checked, err := boundProp[bool](e, ctx, "Checked")
		if err != nil {
			return nil, err
		}
		label, err := bindText(e.Attrs["Label"], ctx)
		if err != nil {
			return nil, err
		}
		if label == nil {
			label = components.Str(e.Attrs["Label"])
		}
		style, err := bindStyle(e, ctx)
		if err != nil {
			return nil, err
		}
		return named(e, ctx, &components.Checkbox{
			Checked: checked,
			Label:   label,
			Style:   style,
		}, nil)
	},
}

var defGauge = &ElementDef{
	Name:  "Gauge",
	Proto: &components.Gauge{},
	Known: true,
	Attrs: []AttrSpec{
		{Name: "BarWidth", Kind: KindInt, Binds: BindsLiteral, Default: "0", Origin: OriginBuiltin},
		{Name: "Label", Kind: KindText, Binds: BindsEither, Origin: OriginBuiltin},
		{Name: "Style", Kind: KindStyle, Binds: BindsEither, Origin: OriginBuiltin},
		{Name: "Value", Kind: KindBinding, Binds: BindsBinding, GoType: "int", Required: true, Origin: OriginBuiltin},
	},
	Children: ChildSpec{Mode: ModeLeaf},
	Build: func(e Element, ctx *Context) (gooey.Component, error) {
		value, err := boundProp[int](e, ctx, "Value")
		if err != nil {
			return nil, err
		}
		label, err := bindText(e.Attrs["Label"], ctx)
		if err != nil {
			return nil, err
		}
		if label == nil {
			label = components.Str(e.Attrs["Label"])
		}
		g := &components.Gauge{Value: value, Label: label}
		g.Width, _ = strconv.Atoi(e.Attrs["BarWidth"])
		// Style is an override for the threshold ramp, so it is applied
		// only when the attribute is actually present.
		if _, ok := e.Attrs["Style"]; ok {
			if g.Style, err = bindStyle(e, ctx); err != nil {
				return nil, err
			}
		}
		return g, nil
	},
}

var defSparkline = &ElementDef{
	Name:  "Sparkline",
	Proto: &components.Sparkline{},
	Known: true,
	Attrs: []AttrSpec{
		{Name: "BarWidth", Kind: KindInt, Binds: BindsLiteral, Default: "0", Origin: OriginBuiltin},
		{Name: "Height", Kind: KindInt, Binds: BindsLiteral, Default: "0", Origin: OriginBuiltin},
		{Name: "Style", Kind: KindStyle, Binds: BindsEither, Origin: OriginBuiltin},
		{Name: "Values", Kind: KindBinding, Binds: BindsBinding, GoType: "[]float64", Required: true, Origin: OriginBuiltin},
	},
	Children: ChildSpec{Mode: ModeLeaf},
	Build: func(e Element, ctx *Context) (gooey.Component, error) {
		series, err := boundProp[[]float64](e, ctx, "Values")
		if err != nil {
			return nil, err
		}
		s := &components.Sparkline{Values: series}
		s.Rows, _ = strconv.Atoi(e.Attrs["Height"])
		s.Width, _ = strconv.Atoi(e.Attrs["BarWidth"])
		if _, ok := e.Attrs["Style"]; ok {
			if s.Style, err = bindStyle(e, ctx); err != nil {
				return nil, err
			}
		}
		return s, nil
	},
}

var defTextBox = &ElementDef{
	Name:  "TextBox",
	Proto: &components.TextBox{},
	Known: true,
	Attrs: []AttrSpec{
		{Name: "AccentStyle", Kind: KindStyle, Binds: BindsLiteral, Origin: OriginBuiltin},
		{Name: "Changed", Kind: KindCommand, Binds: BindsEither, Origin: OriginBuiltin},
		{Name: "Error", Kind: KindBinding, Binds: BindsBinding, GoType: "string", Origin: OriginBuiltin},
		{Name: "InvalidStyle", Kind: KindStyle, Binds: BindsLiteral, Origin: OriginBuiltin},
		{Name: "Prompt", Kind: KindText, Binds: BindsEither, Origin: OriginBuiltin},
		{Name: "Style", Kind: KindStyle, Binds: BindsEither, Origin: OriginBuiltin},
		{Name: "Text", Kind: KindBinding, Binds: BindsBinding, GoType: "string", Required: true, Origin: OriginBuiltin},
	},
	Children: ChildSpec{Mode: ModeAttachments},
	Build: func(e Element, ctx *Context) (gooey.Component, error) {
		// Like Button, a TextBox takes no visual children — but the
		// non-visual attachments (<ValidationMarker>, <Tooltip>,
		// <KeyBinding>) hang off it the way they hang off any element.
		kids, attach, err := buildChildren(e, ctx)
		if err != nil {
			return nil, err
		}
		if len(kids) > 0 {
			return nil, fmt.Errorf("markup: <TextBox> takes no visual children; only attachments like <ValidationMarker> and <Tooltip> may nest here")
		}
		text, err := boundProp[string](e, ctx, "Text")
		if err != nil {
			return nil, err
		}
		changed, err := ctx.Command(e.Attrs["Changed"])
		if err != nil {
			return nil, fmt.Errorf("markup: <TextBox Changed=%q>: %w", e.Attrs["Changed"], err)
		}
		tb := &components.TextBox{Text: text, Changed: changed}
		if p, ok := e.Attrs["Prompt"]; ok {
			prompt, err := bindText(p, ctx)
			if err != nil {
				return nil, err
			}
			if prompt == nil {
				prompt = components.Str(p)
			}
			tb.Prompt = prompt
		}
		if _, ok := e.Attrs["Style"]; ok {
			if tb.Style, err = bindStyle(e, ctx); err != nil {
				return nil, err
			}
		}
		if a, ok := e.Attrs["AccentStyle"]; ok {
			tb.AccentStyle = components.Sty(ctx.Styles[a])
		}
		// Error is the validation handle: a typed binding to the field's
		// error property (empty = valid), never literal text.
		if _, ok := e.Attrs["Error"]; ok {
			if tb.Error, err = boundProp[string](e, ctx, "Error"); err != nil {
				return nil, err
			}
		}
		if a, ok := e.Attrs["InvalidStyle"]; ok {
			tb.InvalidStyle = components.Sty(ctx.Styles[a])
		}
		// A <Validate> behavior (bare or in <TextBox.Behaviors>) wires
		// against the bound Text source and takes over the Error slot.
		var vb *Validate
		for _, a := range attach {
			v, ok := a.(*Validate)
			if !ok {
				continue
			}
			if vb != nil {
				return nil, fmt.Errorf("markup: <TextBox> takes one <Validate>")
			}
			vb = v
		}
		if vb != nil {
			if tb.Error != nil {
				return nil, fmt.Errorf("markup: <TextBox> declares both Error=%q and a <Validate>; the behavior owns the error property, drop one", e.Attrs["Error"])
			}
			if tb.Error, err = wireValidate(vb, "TextBox", tb.Text, bindingPath(e.Attrs["Text"]), ctx); err != nil {
				return nil, err
			}
		}
		if err := attachAll(e, tb, attach); err != nil {
			return nil, err
		}
		return tb, nil
	},
}

var defColorPicker = &ElementDef{
	Name:  "ColorPicker",
	Proto: &components.ColorPicker{},
	Known: true,
	Attrs: []AttrSpec{
		{Name: "Value", Kind: KindBinding, Binds: BindsBinding, GoType: "render.Color", Required: true, Origin: OriginBuiltin},
	},
	Children: ChildSpec{Mode: ModeLeaf},
	Build: func(e Element, ctx *Context) (gooey.Component, error) {
		color, err := boundProp[render.Color](e, ctx, "Value")
		if err != nil {
			return nil, err
		}
		return &components.ColorPicker{Value: color}, nil
	},
}

var defProgressBar = &ElementDef{
	Name:  "ProgressBar",
	Proto: &components.ProgressBar{},
	Known: true,
	Attrs: []AttrSpec{
		{Name: "BarWidth", Kind: KindInt, Binds: BindsLiteral, Default: "0", Origin: OriginBuiltin},
		{Name: "Indeterminate", Kind: KindBinding, Binds: BindsBinding, GoType: "bool", Origin: OriginBuiltin},
		{Name: "Label", Kind: KindText, Binds: BindsEither, Origin: OriginBuiltin},
		{Name: "Style", Kind: KindStyle, Binds: BindsEither, Origin: OriginBuiltin},
		{Name: "Thresholds", Kind: KindBool, Binds: BindsLiteral, Default: "false", Origin: OriginBuiltin},
		{Name: "Tick", Kind: KindDuration, Binds: BindsLiteral, Origin: OriginBuiltin},
		{Name: "Value", Kind: KindBinding, Binds: BindsBinding, GoType: "int", Required: true, Origin: OriginBuiltin},
	},
	Children: ChildSpec{Mode: ModeLeaf},
	Build: func(e Element, ctx *Context) (gooey.Component, error) {
		value, err := boundProp[int](e, ctx, "Value")
		if err != nil {
			return nil, err
		}
		label, err := literalOrBound(e.Attrs["Label"], ctx)
		if err != nil {
			return nil, err
		}
		p := &components.ProgressBar{Value: value, Label: label}
		p.Width, _ = strconv.Atoi(e.Attrs["BarWidth"])
		p.Thresholds = e.Attrs["Thresholds"] == "true"
		// Indeterminate is optional, and its absence is load-bearing: a
		// bar that can never be indeterminate starts no goroutine.
		if _, ok := e.Attrs["Indeterminate"]; ok {
			if p.Indeterminate, err = boundProp[bool](e, ctx, "Indeterminate"); err != nil {
				return nil, err
			}
		}
		if p.Tick, err = optDuration(e, "Tick"); err != nil {
			return nil, err
		}
		if _, ok := e.Attrs["Style"]; ok {
			if p.Style, err = bindStyle(e, ctx); err != nil {
				return nil, err
			}
		}
		return p, nil
	},
}

var defSpinner = &ElementDef{
	Name:  "Spinner",
	Proto: &components.Spinner{},
	Known: true,
	Attrs: []AttrSpec{
		{Name: "Enabled", Kind: KindBinding, Binds: BindsBinding, GoType: "bool", Origin: OriginBuiltin},
		{Name: "Frames", Kind: KindEnum, Binds: BindsLiteral, Enum: []string{"braille", "line", "arc", "dot"}, Default: "braille", Origin: OriginBuiltin},
		{Name: "Interval", Kind: KindDuration, Binds: BindsLiteral, Origin: OriginBuiltin},
		{Name: "Label", Kind: KindText, Binds: BindsEither, Origin: OriginBuiltin},
		{Name: "Style", Kind: KindStyle, Binds: BindsEither, Origin: OriginBuiltin},
	},
	Children: ChildSpec{Mode: ModeLeaf},
	Build: func(e Element, ctx *Context) (gooey.Component, error) {
		label, err := literalOrBound(e.Attrs["Label"], ctx)
		if err != nil {
			return nil, err
		}
		s := &components.Spinner{Label: label}
		if raw, ok := e.Attrs["Frames"]; ok {
			frames, known := components.SpinnerFrames(raw)
			if !known {
				return nil, fmt.Errorf("markup: <Spinner Frames=%q>: unknown frame set; want one of %s",
					raw, strings.Join(components.SpinnerNames, ", "))
			}
			s.Frames = frames
		}
		if s.Interval, err = optDuration(e, "Interval"); err != nil {
			return nil, err
		}
		if _, ok := e.Attrs["Enabled"]; ok {
			if s.Enabled, err = boundProp[bool](e, ctx, "Enabled"); err != nil {
				return nil, err
			}
		}
		if _, ok := e.Attrs["Style"]; ok {
			if s.Style, err = bindStyle(e, ctx); err != nil {
				return nil, err
			}
		}
		return s, nil
	},
}

var defToggle = &ElementDef{
	Name:  "Toggle",
	Proto: &components.Toggle{},
	Known: true,
	Attrs: []AttrSpec{
		{Name: "Changed", Kind: KindCommand, Binds: BindsEither, Origin: OriginBuiltin},
		{Name: "Checked", Kind: KindBinding, Binds: BindsBinding, GoType: "bool", Required: true, Origin: OriginBuiltin},
		{Name: "Label", Kind: KindText, Binds: BindsEither, Origin: OriginBuiltin},
		{Name: "Style", Kind: KindStyle, Binds: BindsEither, Origin: OriginBuiltin},
	},
	Children: ChildSpec{Mode: ModeLeaf},
	Build: func(e Element, ctx *Context) (gooey.Component, error) {
		checked, err := boundProp[bool](e, ctx, "Checked")
		if err != nil {
			return nil, err
		}
		label, err := literalOrBound(e.Attrs["Label"], ctx)
		if err != nil {
			return nil, err
		}
		changed, err := ctx.Command(e.Attrs["Changed"])
		if err != nil {
			return nil, fmt.Errorf("markup: <Toggle Changed=%q>: %w", e.Attrs["Changed"], err)
		}
		style, err := bindStyle(e, ctx)
		if err != nil {
			return nil, err
		}
		return named(e, ctx, &components.Toggle{
			Checked: checked, Label: label, Changed: changed, Style: style,
		}, nil)
	},
}

var defSegmented = &ElementDef{
	Name:  "Segmented",
	Proto: &components.Segmented{},
	Known: true,
	Attrs: []AttrSpec{
		{Name: "Changed", Kind: KindCommand, Binds: BindsEither, Origin: OriginBuiltin},
		{Name: "Options", Kind: KindBinding, Binds: BindsEither, GoType: "[]string", Required: true, Origin: OriginBuiltin},
		{Name: "Selected", Kind: KindBinding, Binds: BindsBinding, GoType: "int", Required: true, Origin: OriginBuiltin},
		{Name: "Style", Kind: KindStyle, Binds: BindsEither, Origin: OriginBuiltin},
	},
	Children: ChildSpec{Mode: ModeLeaf},
	Build: func(e Element, ctx *Context) (gooey.Component, error) {
		selected, err := boundProp[int](e, ctx, "Selected")
		if err != nil {
			return nil, err
		}
		options, err := optionList(e, ctx)
		if err != nil {
			return nil, err
		}
		changed, err := ctx.Command(e.Attrs["Changed"])
		if err != nil {
			return nil, fmt.Errorf("markup: <Segmented Changed=%q>: %w", e.Attrs["Changed"], err)
		}
		style, err := bindStyle(e, ctx)
		if err != nil {
			return nil, err
		}
		return named(e, ctx, &components.Segmented{
			Options: options, Selected: selected, Changed: changed, Style: style,
		}, nil)
	},
}

var defStatusBar = &ElementDef{
	Name:         "StatusBar",
	DynamicAttrs: "the three slots are consumed by ranging over statusSections, so no read names a literal",
	Proto:        &components.StatusBar{},
	Known:        true,
	Attrs: []AttrSpec{
		{Name: "Center", Kind: KindString, Binds: BindsLiteral, Origin: OriginBuiltin},
		{Name: "Left", Kind: KindString, Binds: BindsLiteral, Origin: OriginBuiltin},
		{Name: "Right", Kind: KindString, Binds: BindsLiteral, Origin: OriginBuiltin},
	},
	Children: ChildSpec{Mode: ModeAttachments},
	Build: func(e Element, ctx *Context) (gooey.Component, error) {
		//gooey:catalog-attrs statusSections
		// The three slots are consumed by ranging over statusSections,
		// so the names come from that table rather than from any
		// by-name read.
		bar, err := buildStatusBar(e, ctx)
		return bar, err
	},
}

var defTabs = &ElementDef{
	Name:  "Tabs",
	Proto: &components.Tabs{},
	Known: true,
	Attrs: []AttrSpec{
		{Name: "Changed", Kind: KindCommand, Binds: BindsEither, Origin: OriginBuiltin},
		{Name: "Selected", Kind: KindBinding, Binds: BindsBinding, GoType: "int", Origin: OriginBuiltin},
		{Name: "Style", Kind: KindStyle, Binds: BindsEither, Origin: OriginBuiltin},
	},
	Children: ChildSpec{Mode: ModeRestricted, Only: []string{"Tab"}},
	Build: func(e Element, ctx *Context) (gooey.Component, error) {
		tb, err := buildTabs(e, ctx)
		return tb, err
	},
}

var defButtonBar = &ElementDef{
	Name:  "ButtonBar",
	Proto: &components.ButtonBar{},
	Known: true,
	Attrs: []AttrSpec{
		{Name: "Gap", Kind: KindInt, Binds: BindsLiteral, Default: "0", Origin: OriginBuiltin},
		{Name: "Separator", Kind: KindString, Binds: BindsLiteral, Origin: OriginBuiltin},
		{Name: "Uniform", Kind: KindBool, Binds: BindsLiteral, Default: "false", Origin: OriginBuiltin},
	},
	Children: ChildSpec{Mode: ModeMany},
	Build: func(e Element, ctx *Context) (gooey.Component, error) {
		kids, attach, err := buildChildren(e, ctx)
		if err != nil {
			return nil, err
		}
		bar := &components.ButtonBar{Children: kids, Separator: e.Attrs["Separator"]}
		bar.Gap, _ = strconv.Atoi(e.Attrs["Gap"])
		bar.Uniform = e.Attrs["Uniform"] == "true"
		if err := attachAll(e, bar, attach); err != nil {
			return nil, err
		}
		return bar, nil
	},
}

var defMenuBar = &ElementDef{
	Name:  "MenuBar",
	Proto: &components.MenuBar{},
	Known: true,
	Attrs: []AttrSpec{
		{Name: "Style", Kind: KindStyle, Binds: BindsEither, Origin: OriginBuiltin},
	},
	Children: ChildSpec{Mode: ModeRestricted, Only: []string{"Menu", "MenuItem"}},
	Build: func(e Element, ctx *Context) (gooey.Component, error) {
		bar, err := buildMenuBar(e, ctx)
		return bar, err
	},
}

var defToastHost = &ElementDef{
	Name:  "ToastHost",
	Proto: &components.ToastHost{},
	Known: true,
	Attrs: []AttrSpec{
		{Name: "Duration", Kind: KindDuration, Binds: BindsLiteral, Origin: OriginBuiltin},
		{Name: "Style", Kind: KindStyle, Binds: BindsLiteral, Origin: OriginBuiltin},
	},
	Children: ChildSpec{Mode: ModeNone},
	Build: func(e Element, ctx *Context) (gooey.Component, error) {
		if len(e.Children) > 0 {
			return nil, fmt.Errorf("markup: <ToastHost> takes no children; toasts are shown from code (Show), not declared")
		}
		h := &components.ToastHost{Style: ctx.Styles[e.Attrs["Style"]]}
		if raw, ok := e.Attrs["Duration"]; ok {
			d, err := time.ParseDuration(strings.TrimSpace(raw))
			if err != nil {
				return nil, fmt.Errorf("markup: <ToastHost Duration=%q>: %w", raw, err)
			}
			h.Duration = d
		}
		return h, nil
	},
}

var defAdornmentLayer = &ElementDef{
	Name:     "AdornmentLayer",
	Proto:    &components.AdornmentLayer{},
	Known:    true,
	Children: ChildSpec{Mode: ModeNone},
	Build: func(e Element, ctx *Context) (gooey.Component, error) {
		if len(e.Children) > 0 {
			return nil, fmt.Errorf("markup: <AdornmentLayer> takes no children; adornments attach themselves at runtime (a Tooltip finds the layer on its own)")
		}
		return &components.AdornmentLayer{}, nil
	},
}

var defTooltip = &ElementDef{
	Name:  "Tooltip",
	Proto: &components.Tooltip{},
	Known: true,
	Attrs: []AttrSpec{
		{Name: "Delay", Kind: KindDuration, Binds: BindsLiteral, Origin: OriginBuiltin},
		{Name: "Gesture", Kind: KindGesture, Binds: BindsLiteral, Origin: OriginBuiltin},
		{Name: "Style", Kind: KindStyle, Binds: BindsLiteral, Origin: OriginBuiltin},
		{Name: "Text", Kind: KindText, Binds: BindsEither, Origin: OriginBuiltin},
	},
	Children: ChildSpec{Mode: ModeLeaf},
	Build: func(e Element, ctx *Context) (gooey.Component, error) {
		// Non-visual like KeyBinding: buildChildren routes it to the
		// parent as an attachment, and the framework's hover routing
		// (gooey.HoverWatcher) drives it.
		text, err := literalOrBound(e.Attrs["Text"], ctx)
		if err != nil {
			return nil, err
		}
		t := &components.Tooltip{Text: text, Style: ctx.Styles[e.Attrs["Style"]]}
		if t.Delay, err = optDuration(e, "Delay"); err != nil {
			return nil, err
		}
		if g := e.Attrs["Gesture"]; g != "" {
			// Validated at load, stored in the canonical spelling — the
			// hint on screen is byte-identical to what a KeyBinding
			// declares, the MenuItem rule.
			ev, err := input.ParseGesture(g)
			if err != nil {
				return nil, fmt.Errorf("markup: <Tooltip Gesture=%q>: %w", g, err)
			}
			t.Gesture = ev.String()
		}
		return t, nil
	},
}

var defValidationMarker = &ElementDef{
	Name:  "ValidationMarker",
	Proto: &components.ValidationMarker{},
	Known: true,
	Attrs: []AttrSpec{
		{Name: "Error", Kind: KindBinding, Binds: BindsBinding, GoType: "string", Origin: OriginBuiltin},
		{Name: "Style", Kind: KindStyle, Binds: BindsLiteral, Origin: OriginBuiltin},
	},
	Children: ChildSpec{Mode: ModeNone},
	Build: func(e Element, ctx *Context) (gooey.Component, error) {
		// Non-visual like Tooltip: buildChildren routes it to the parent
		// as an attachment; its floating message shows in the page's
		// AdornmentLayer. An omitted Error adopts the host TextBox's own
		// handle, so the common form is just <ValidationMarker/>.
		if len(e.Children) > 0 {
			return nil, fmt.Errorf("markup: <ValidationMarker> takes no children")
		}
		m := &components.ValidationMarker{Style: ctx.Styles[e.Attrs["Style"]]}
		if _, ok := e.Attrs["Error"]; ok {
			var err error
			if m.Error, err = boundProp[string](e, ctx, "Error"); err != nil {
				return nil, err
			}
		}
		return m, nil
	},
}

var defKeyBinding = &ElementDef{
	Name:  "KeyBinding",
	Proto: &gooey.KeyBinding{},
	Known: true,
	Attrs: []AttrSpec{
		{Name: "Command", Kind: KindCommand, Binds: BindsEither, Origin: OriginBuiltin},
		{Name: "Gesture", Kind: KindGesture, Binds: BindsLiteral, Origin: OriginBuiltin},
	},
	Children: ChildSpec{Mode: ModeLeaf},
	Build: func(e Element, ctx *Context) (gooey.Component, error) {
		g, err := input.ParseGesture(e.Attrs["Gesture"])
		if err != nil {
			return nil, fmt.Errorf("markup: <KeyBinding Gesture=%q>: %w", e.Attrs["Gesture"], err)
		}
		cmd, err := ctx.Command(e.Attrs["Command"])
		if err != nil {
			return nil, fmt.Errorf("markup: <KeyBinding Gesture=%q>: %w", e.Attrs["Gesture"], err)
		}
		return named(e, ctx, &gooey.KeyBinding{Gesture: g, Command: cmd}, nil)
	},
}

var defTimer = &ElementDef{
	Name:  "Timer",
	Proto: &components.Timer{},
	Known: true,
	Attrs: []AttrSpec{
		{Name: "Enabled", Kind: KindBinding, Binds: BindsBinding, GoType: "bool", Origin: OriginBuiltin},
		{Name: "Interval", Kind: KindDuration, Binds: BindsLiteral, Required: true, Origin: OriginBuiltin},
		{Name: "Tick", Kind: KindCommand, Binds: BindsEither, Origin: OriginBuiltin},
	},
	Children: ChildSpec{Mode: ModeLeaf},
	Build: func(e Element, ctx *Context) (gooey.Component, error) {
		// Non-visual like KeyBinding: buildChildren routes it to the
		// parent as an attachment, and the Composer starts it.
		raw := strings.TrimSpace(e.Attrs["Interval"])
		if raw == "" {
			return nil, fmt.Errorf("markup: <Timer> needs an Interval (e.g. Interval=\"600ms\")")
		}
		d, err := time.ParseDuration(raw)
		if err != nil {
			return nil, fmt.Errorf("markup: <Timer Interval=%q>: %w", raw, err)
		}
		if d <= 0 {
			return nil, fmt.Errorf("markup: <Timer Interval=%q>: must be positive", raw)
		}
		tick, err := ctx.Command(e.Attrs["Tick"])
		if err != nil {
			return nil, fmt.Errorf("markup: <Timer Tick=%q>: %w", e.Attrs["Tick"], err)
		}
		t := &components.Timer{Interval: d, Tick: tick}
		// Enabled is optional; absent means always enabled. When present
		// it is a live bool handle, so the graph can pause the timer.
		if _, ok := e.Attrs["Enabled"]; ok {
			if t.Enabled, err = boundProp[bool](e, ctx, "Enabled"); err != nil {
				return nil, err
			}
		}
		return t, nil
	},
}

var defTypeAhead = &ElementDef{
	Name:  "TypeAhead",
	Proto: &components.TypeAhead{},
	Known: true,
	Doc:   "Windows Explorer's type-ahead find on a list: typing selects the first item whose Key value has that prefix.",
	Attrs: []AttrSpec{
		{Name: "Key", Kind: KindString, Binds: BindsLiteral, Required: true, Origin: OriginBuiltin},
		{Name: "NoMatch", Kind: KindBinding, Binds: BindsBinding, GoType: "bool", Origin: OriginBuiltin},
		{Name: "Search", Kind: KindBinding, Binds: BindsBinding, GoType: "string", Origin: OriginBuiltin},
		{Name: "Timeout", Kind: KindDuration, Binds: BindsLiteral, Origin: OriginBuiltin},
	},
	Children: ChildSpec{Mode: ModeLeaf},
	Build: func(e Element, ctx *Context) (gooey.Component, error) {
		// Non-visual like Timer: buildChildren routes it to the parent as
		// an attachment, and the Composer starts its idle clock.
		key := strings.TrimSpace(e.Attrs["Key"])
		if key == "" {
			return nil, fmt.Errorf("markup: <TypeAhead> needs a Key naming the item value to search (e.g. Key=\"Title\")")
		}
		t := &components.TypeAhead{Key: key}
		if raw := strings.TrimSpace(e.Attrs["Timeout"]); raw != "" {
			d, err := time.ParseDuration(raw)
			if err != nil {
				return nil, fmt.Errorf("markup: <TypeAhead Timeout=%q>: %w", raw, err)
			}
			if d <= 0 {
				return nil, fmt.Errorf("markup: <TypeAhead Timeout=%q>: must be positive", raw)
			}
			t.Timeout = d
		}
		var err error
		if _, ok := e.Attrs["Search"]; ok {
			if t.Search, err = boundProp[string](e, ctx, "Search"); err != nil {
				return nil, err
			}
		}
		if _, ok := e.Attrs["NoMatch"]; ok {
			if t.NoMatch, err = boundProp[bool](e, ctx, "NoMatch"); err != nil {
				return nil, err
			}
		}
		return t, nil
	},
}

var defImage = &ElementDef{
	Name:  "Image",
	Proto: &components.Image{},
	Known: true,
	Attrs: []AttrSpec{
		{Name: "Cols", Kind: KindBinding, Binds: BindsEither, GoType: "int", Required: true, Origin: OriginBuiltin},
		{Name: "Rows", Kind: KindBinding, Binds: BindsEither, GoType: "int", Required: true, Origin: OriginBuiltin},
		{Name: "Src", Kind: KindBinding, Binds: BindsEither, GoType: "image.Image", Required: true, Origin: OriginBuiltin},
	},
	Children: ChildSpec{Mode: ModeLeaf},
	Build: func(e Element, ctx *Context) (gooey.Component, error) {
		im, err := buildImage(e, ctx)
		return im, err
	},
}

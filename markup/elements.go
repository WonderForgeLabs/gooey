package markup

import (
	"fmt"
	"io/fs"
	"strconv"
	"strings"

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
		defFrozen,
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
		defMenu,
		defMenuItem,
		defToastHost,
		defAdornmentLayer,
		defTooltip,
		defValidationMarker,
		defKeyBinding,
		defTimer,
		defFileWatcher,
		defTypeAhead,
		defImage,
	)
}

var defText = &ElementDef{
	Name:  "Text",
	Icon:  "symbol-string",
	Seed:  "<Text>Text</Text>",
	Proto: &components.Text{},
	Known: true,
	Doc:   "A run of text. The content is the element's body, not an attribute.",
	Attrs: []AttrSpec{
		{Name: "Bold", Kind: KindBool, Binds: BindsLiteral, Default: "false", Origin: OriginBuiltin},
		{Name: "Style", Kind: KindStyle, Binds: BindsEither, Origin: OriginBuiltin},
	},
	// The one builtin whose content is its body. KindText/BindsEither
	// because the Build below hands the body to bindText: a literal and
	// a {{.Path}} binding are both legal there, exactly as on Tooltip.
	Body: &BodySpec{
		Kind: KindText, Binds: BindsEither, GoType: "string",
		Doc: "The run of text. On one line it is verbatim, so leading and trailing spaces count; wrapped across lines it is trimmed.",
	},
	Children: ChildSpec{Mode: ModeLeaf},
	Build: func(e Element, ctx *Context) (gooey.Component, error) {
		style, err := BoundStyle(e, ctx)
		if err != nil {
			return nil, err
		}
		bold, err := litBool(e, "Bold")
		if err != nil {
			return nil, err
		}
		if bold {
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
		content := bodyText(e.Text)
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
	Icon:  "primitive-square",
	Seed:  "<Button Content=\"Button\" Click=\"{{.Click}}\"/>",
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
		style, err := BoundStyle(e, ctx)
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
	Name: "Companion",
	Icon: "terminal",
	// PATH="TRUE" IS A REAL DEPENDENCY ON THE MACHINE, and it is named
	// here rather than left for somebody to discover.
	//
	// companionPath resolves a bare name through exec.LookPath and a
	// pathful one through os.Stat, so EVERY loadable value of this
	// attribute has to exist — there is no placeholder that merely
	// parses. A seed must load (TestEverySeededElementLoadsAndOccupies
	// Space), so the seed names something a POSIX box has. `./job` was
	// tried and fails on the stat.
	//
	// What that cost was invisible: probeElement reads this seed for the
	// required Path, so on a box without coreutils every <Companion>
	// probe in every sweep arm came back UNVERIFIED — which reads as
	// "the harness cannot construct this element" and is not what
	// happened, and unverified is not a failure. TestNoSweepProbe
	// DependsOnAnInstalledBinary is the guard: it fails BY CAUSE, naming
	// the missing binary, instead of letting a whole element go quietly
	// unchecked. Raised in review of #470.
	Seed:  "<Companion Name=\"job\" Path=\"true\" Exited=\"{{.Exited}}\"/>",
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
		// REQUIRED, and declared so. buildCompanion refuses a
		// <Companion> without one ("needs a Path (the executable)"), and
		// this said otherwise — so every consumer that reads Required to
		// decide what a new element must carry got it wrong, and the
		// sweep harness could not build the element at all. No Default:
		// TestDeclaredDefaultsRenderIdenticallyToOmission checks a
		// declared default by RENDERING it, which a non-visual element
		// cannot do, and there is no honest default for "which binary"
		// anyway. Found in review of #470.
		{Name: "Path", Kind: KindString, Binds: BindsLiteral, Required: true, Origin: OriginBuiltin},
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
	Icon:         "checklist",
	Seed:         "<Validate Required=\"true\"/>",
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
//
// THE FALLTHROUGH IS PART OF THE ANSWER, and this comment used to read
// as though every kind were named. Three are not: Pattern is a regular
// expression, and MinValue/MaxValue are floating-point bounds. There is
// no Kind for either, so they land on KindString — "literal only, used
// verbatim", which is true and says nothing about the grammar.
//
// Inventing KindRegexp and KindFloat is not a local change: Kind is read
// by the wysiwyg property grid, by catalogen, and by the reference
// generator, and each would need to learn an editor and a validator for
// a kind with three attributes in it. What the vocabulary loses by not
// having them is one thing — a probe value the sweeps can derive — and
// narrowerThanItsKind carries that instead, per attribute, with
// TestEveryNarrowedLiteralIsReached keeping the rows honest. Recorded as
// a decision rather than left as a default. Raised in review of #470.
func validateBuiltinAttrs() []AttrSpec {
	kinds := map[string]Kind{
		"MinLen": KindInt, "MaxLen": KindInt,
		"Required": KindBool, "EmailAddress": KindBool, "Url": KindBool,
		"Phone": KindBool, "CreditCard": KindBool, "Digits": KindBool,
		"Integer": KindBool,
		// NO KIND OF THEIR OWN — see the paragraph above. Spelled out
		// rather than left to the fallthrough, so the reader is not left
		// deciding whether the omission was deliberate.
		"Pattern": KindString, "MinValue": KindString, "MaxValue": KindString,
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
	Icon:     "browser",
	Known:    false,
	Opaque:   "a pseudo-element: <Tabs> parses a <Tab>'s Header and content itself, so this definition exists only to reject one used anywhere else",
	Children: ChildSpec{Mode: ModeUnknown},
	Build: func(e Element, ctx *Context) (gooey.Component, error) {
		return nil, fmt.Errorf("markup: <Tab> is only valid directly inside <Tabs>")
	},
}

var defBorder = &ElementDef{
	Name:  "Border",
	Icon:  "window",
	Seed:  "<Border Title=\"Border\"><Text>content</Text></Border>",
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
		style, err := BoundStyle(e, ctx)
		if err != nil {
			return nil, err
		}
		background, err := BoundColor(e, ctx, "Background")
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

// defFrozen puts freezing in markup. Until this element, a page could
// only be made a picture by a Go type implementing gooey.Frozen — which
// meant the feature built FOR a design surface was unreachable from the
// markup a design surface edits.
//
// Neither attribute declares a Default, and that is the catalog's rule
// rather than an omission: AttrSpec.Default claims "writing this is the
// same as writing nothing" and TestDeclaredDefaultsRenderIdenticallyTo
// Omission checks it by RENDERING. Freezing changes what the tree means,
// not what it looks like, so no value of either attribute is
// discriminable in a static frame and declaring a Default would be an
// unfalsifiable claim.
var defFrozen = &ElementDef{
	Name: "Frozen",
	// SHARED WITH <Image>, and that is a stated compromise rather than an
	// oversight. Every name in the vendored codicon set is already claimed
	// by another element, and this pane landed after the rule that every
	// builtin must declare one (#287) — so the choice was a shared name or
	// a blank toolbox row.
	//
	// file-media is the semantically exact one: a frozen region renders
	// and does not act, which is what component.go's own comment calls
	// "click a button and it sits there like a picture". The toolbox will
	// show <Frozen> and <Image> with the same glyph until a lock codicon
	// is vendored, which is the real fix and is not this PR's.
	Icon:  "file-media",
	Seed:  "<Frozen><Text>frozen</Text></Frozen>",
	Proto: &components.Frozen{},
	Known: true,
	Doc:   "A region that renders but does not act. Allow names the interaction categories that still do.",
	Attrs: []AttrSpec{
		// Bind-only. A literal Active would be a constant, and a constant
		// false is a <Frozen> that does nothing — which is a spelling the
		// page should delete rather than write. Omitting it is how you say
		// "always frozen".
		{Name: "Active", Kind: KindBinding, Binds: BindsBinding, GoType: "bool", Origin: OriginBuiltin},
		{Name: "Allow", Kind: KindText, Binds: BindsEither, Origin: OriginBuiltin},
		// Bind-only, and for a different reason than Active above: this
		// is a WRITE target. The framework Sets it, so a literal has
		// nowhere for the message to go — it would read as configured
		// and report nothing forever.
		{Name: "AllowError", Kind: KindBinding, Binds: BindsBinding, GoType: "string", Origin: OriginBuiltin},
	},
	Children: ChildSpec{Mode: ModeOne},
	Build: func(e Element, ctx *Context) (gooey.Component, error) {
		kids, attach, err := buildChildren(e, ctx)
		if err != nil {
			return nil, err
		}
		if len(kids) != 1 {
			return nil, fmt.Errorf("markup: <Frozen> needs exactly one child")
		}
		f := &components.Frozen{Child: kids[0]}
		if raw := e.Attrs["Active"]; raw != "" {
			active, err := Bound[bool](e, ctx, "Active")
			if err != nil {
				return nil, err
			}
			f.Active = active
		}
		// Whether Allow is BOUND is asked twice: here, to decide whether
		// it is checkable at load, and again by AllowError, which accepts
		// nothing else. Spelled twice, the two guards have to agree or the
		// second one is wrong about the first — so it is computed once.
		allowBound := false
		if raw := e.Attrs["Allow"]; raw != "" {
			allowBound = strings.Contains(raw, "{{")
			// A LITERAL Allow is checked here, at load time, which is the
			// bargain the rest of markup makes: everything resolvable
			// resolves before the UI is live. An interpolated one cannot
			// be — its value does not exist yet — so it is left to
			// components.Frozen, which fails closed and reports through
			// AllowError. Checking only what is checkable is the point;
			// pretending the bound case is checkable would be worse than
			// admitting it is not.
			if !allowBound {
				if _, err := gooey.ParseAllow(raw); err != nil {
					return nil, fmt.Errorf("markup: <Frozen Allow=%q>: %w", raw, err)
				}
			}
			allow, err := BoundText(e, ctx, "Allow")
			if err != nil {
				return nil, err
			}
			f.Allow = allow
			// RECORDED FOR EVERY <Frozen> THAT BINDS ONE, whether or not
			// this element also has an AllowError. The collision is
			// "somebody else's sink is my source", so the element that
			// gets erased need not be arming anything itself — see
			// armScope.allows, and document.build for where the two maps
			// meet. Raised in review of #459.
			for h, path := range allowSources(ctx, raw) {
				if _, seen := ctx.arms.allows[h]; !seen {
					ctx.arms.allows[h] = path
				}
			}
		}
		if raw := e.Attrs["AllowError"]; raw != "" {
			// It reports the parse of a BOUND Allow, and only that. With
			// no Allow there is no parse; with a LITERAL one the parse
			// already happened, twenty lines up, and produced a load
			// error naming the attribute — so the channel could never
			// carry anything and would read as configured forever.
			//
			// ONE term, not two. allowBound is false whenever Allow is
			// absent (it is only assigned inside the branch that also
			// assigns f.Allow), so it already covers both the absent and
			// the literal case. Spelling it as `f.Allow == nil || …`
			// read as two independent conditions and invited the next
			// reader to delete whichever looked redundant.
			if !allowBound {
				return nil, fmt.Errorf(
					"markup: <Frozen AllowError=%q> without a BOUND Allow: the only failure "+
						"it can report is an unparseable set, and a set that is absent or "+
						"literal cannot become one after load", raw)
			}
			sink, err := Bound[string](e, ctx, "AllowError")
			if err != nil {
				return nil, err
			}
			// ALIASED to Allow. <Frozen Allow="{{.X}}" AllowError="{{.X}}">
			// builds, and then the priming publish overwrites the author's
			// own allow set with the parse message before the UI is live —
			// measured: X goes "Focus" -> "" during Build.
			//
			// THIS COMPARES RESOLVED HANDLES, and it used to compare
			// binding TEXT. The reasoning for the text compare was that
			// "pointer identity cannot catch it, because BoundText wraps a
			// dynamic attribute in a FRESH computed every call" — true of
			// the computed, and the wrong handle to compare. The SOURCE
			// the binding resolves to is stable, and it is available here.
			//
			// The text compare missed two spellings that both build
			// cleanly and both destroy the allow set during Build:
			//
			//   Allow="{{.A}} {{.X}}" AllowError="{{.X}}"
			//       bindingPath takes only the FIRST binding, reads "A",
			//       never matches "X" (measured: X "Hover" -> "").
			//   Allow="{{.X}}" AllowError="{{.Y}}", Values[X] == Values[Y]
			//       two names, one property; the text differs and the
			//       handle does not (measured: "Focus" -> "").
			//
			// The dup-sink guard forty lines below already refuses the
			// second shape for its OWN question, because it keys by
			// pointer — so the two guards on one line of defence
			// disagreed about what "the same property" means, and the
			// weaker one was the one protecting page state.
			//
			// It sits BELOW Bound so `sink` exists. Safe: Bound only
			// reads, and armAllowError is still the last statement, so
			// the author's set still survives every refusal above it.
			// Raised in review of #459.
			if ap, aliased := aliasesSink(ctx, e.Attrs["Allow"], sink); aliased {
				return nil, fmt.Errorf(
					"markup: <Frozen Allow=%q AllowError=%q>: one property cannot be both "+
						"the allow set and the place its parse failure is reported — "+
						"publishing would overwrite the set it just read "+
						"(both resolve to %s)",
					e.Attrs["Allow"], raw, ap)
			}
			// A WRITE target has to be writable, and Bound does not ask —
			// it resolves handles for reading, which is what every other
			// attribute on this element wants. A computed derives its
			// value and has no setter, so armAllowError's priming Set
			// would panic INSIDE Build: in the one package whose contract
			// is that everything resolvable resolves before the UI is
			// live, and under the os.DirFS watcher a rebuild panic takes
			// the app down instead of showing a load error.
			//
			// markup/cond.go names this gap and says the fix belongs in
			// the two-way binders. AllowError is the package's first
			// write target, so this is the first place to honour it.
			if !sink.Settable() {
				return nil, fmt.Errorf(
					"markup: <Frozen AllowError=%q> is a COMPUTED property: it derives its "+
						"value and has no setter, so the failure has nowhere to go", raw)
			}
			if ctx.Dispatcher == nil {
				return nil, fmt.Errorf(
					"markup: <Frozen AllowError=%q> needs ctx.Dispatcher: the failure is "+
						"published from an invalidation, and a Set from inside one would "+
						"mutate the graph mid-invalidation", raw)
			}
			// A SECOND arm on the same sink. Two <Frozen> publishing to
			// one property erase each other — see armScope.sinks.
			//
			// arms.outer as well, because an ItemsView row's map is
			// deliberately row-local and would otherwise not see the
			// PAGE's arms. Checking both while registering only in
			// arms.sinks is what lets every row arm the same template
			// sink — not a collision — while a row arming a page-owned
			// handle still is. Raised in review of #459.
			was, dup := ctx.arms.sinks[sink]
			if !dup {
				was, dup = ctx.arms.outer[sink]
			}
			if dup {
				return nil, fmt.Errorf(
					"markup: <Frozen AllowError=%q>: already the failure channel for "+
						"<Frozen AllowError=%q> in this document — two sealed subtrees "+
						"writing one property erase each other's message, leaving a "+
						"subtree sealed with nothing to show for it", raw, was)
			}
			ctx.arms.sinks[sink] = raw
			// AND, when this is a nested scope, on the document's record.
			// The immediate arms.outer check above catches a row realized
			// AFTER the page's <Frozen>; this is what catches one realized
			// before, which ItemsView.Validate's throwaway row always is
			// when the list is declared first. Raised in review of #459.
			if ctx.arms.outer != nil {
				if was, dup := ctx.arms.nested.record(sink, raw); dup {
					return nil, fmt.Errorf(
						"markup: <Frozen AllowError=%q>: already the failure channel for "+
							"<Frozen AllowError=%q> in another item template on this page "+
							"— two sealed subtrees writing one property erase each other's "+
							"message, and neither template is the page, so nothing else in "+
							"this build can see the pair", raw, was)
				}
			}
			// THROUGH arms.pending, so a load error later in this build
			// takes the arm with it. armAllowError both subscribes and
			// PUBLISHES, and neither is undoable — see armScope.pending
			// for what a refused build used to leave behind. A row is a
			// build too: the ItemsView factory opens a carrier per
			// realization, so this reaches an open one whether it is the
			// page being loaded or a row being scrolled into view.
			ctx.arms.pending.arm(func() { armAllowError(f, sink, ctx.Dispatcher) })
		}
		if err := attachAll(e, f, attach); err != nil {
			return nil, err
		}
		return f, nil
	},
}

var defGrid = &ElementDef{
	Name:  "Grid",
	Icon:  "table",
	Seed:  "<Grid Rows=\"1,1\" Cols=\"1*,1*\"><Text Grid.Row=\"0\" Grid.Col=\"0\">A</Text><Text Grid.Row=\"1\" Grid.Col=\"1\">B</Text></Grid>",
	Proto: &components.Grid{},
	Known: true,
	Attrs: []AttrSpec{
		{Name: "Background", Kind: KindColor, Binds: BindsEither, Origin: OriginBuiltin},
		{Name: "Cols", Role: RoleColTracks, Kind: KindGridLens, Binds: BindsLiteral, Origin: OriginBuiltin},
		{Name: "Rows", Role: RoleRowTracks, Kind: KindGridLens, Binds: BindsLiteral, Origin: OriginBuiltin},
	},
	Children: ChildSpec{Mode: ModeMany},
	Grants: Grant{
		Kind: GrantCell,
		Attached: []AttrSpec{
			// A zero span means one track (layout.go:47), so "0" — not
			// "1" — is the value that reproduces omission.
			{Name: "Grid.Col", Role: RoleCol, Kind: KindInt, Binds: BindsLiteral, Default: "0", Category: CategoryLayout, Origin: OriginBuiltin},
			{Name: "Grid.ColSpan", Role: RoleColSpan, Kind: KindInt, Binds: BindsLiteral, Default: "0", Category: CategoryLayout, Origin: OriginBuiltin},
			{Name: "Grid.Row", Role: RoleRow, Kind: KindInt, Binds: BindsLiteral, Default: "0", Category: CategoryLayout, Origin: OriginBuiltin},
			{Name: "Grid.RowSpan", Role: RoleRowSpan, Kind: KindInt, Binds: BindsLiteral, Default: "0", Category: CategoryLayout, Origin: OriginBuiltin},
		},
	},
	Build: func(e Element, ctx *Context) (gooey.Component, error) {
		rows, err := gridLens(e, "Rows")
		if err != nil {
			return nil, err
		}
		cols, err := gridLens(e, "Cols")
		if err != nil {
			return nil, err
		}
		kids, attach, err := buildChildren(e, ctx)
		if err != nil {
			return nil, err
		}
		background, err := BoundColor(e, ctx, "Background")
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
	Icon:  "split-horizontal",
	Seed:  "<VStack><Text>One</Text><Text>Two</Text></VStack>",
	Proto: &components.VStack{},
	Known: true,
	Attrs: []AttrSpec{
		{Name: "Background", Kind: KindColor, Binds: BindsEither, Origin: OriginBuiltin},
		{Name: "Gap", Kind: KindInt, Binds: BindsLiteral, Default: "0", Origin: OriginBuiltin},
	},
	Children: ChildSpec{Mode: ModeMany},
	Grants:   Grant{Kind: GrantOrder},
	Build: func(e Element, ctx *Context) (gooey.Component, error) {
		gap, err := litInt(e, "Gap")
		if err != nil {
			return nil, err
		}
		kids, attach, err := buildChildren(e, ctx)
		if err != nil {
			return nil, err
		}
		background, err := BoundColor(e, ctx, "Background")
		if err != nil {
			return nil, err
		}
		var w gooey.Component = &components.VStack{Children: kids, Gap: gap, Background: background}
		if err := attachAll(e, w, attach); err != nil {
			return nil, err
		}
		return w, nil
	},
}

var defHStack = &ElementDef{
	Name:  "HStack",
	Icon:  "split-vertical",
	Seed:  "<HStack Gap=\"1\"><Text>One</Text><Text>Two</Text></HStack>",
	Proto: &components.HStack{},
	Known: true,
	Attrs: []AttrSpec{
		{Name: "Background", Kind: KindColor, Binds: BindsEither, Origin: OriginBuiltin},
		{Name: "Gap", Kind: KindInt, Binds: BindsLiteral, Default: "0", Origin: OriginBuiltin},
	},
	Children: ChildSpec{Mode: ModeMany},
	Grants:   Grant{Kind: GrantOrder},
	Build: func(e Element, ctx *Context) (gooey.Component, error) {
		gap, err := litInt(e, "Gap")
		if err != nil {
			return nil, err
		}
		kids, attach, err := buildChildren(e, ctx)
		if err != nil {
			return nil, err
		}
		background, err := BoundColor(e, ctx, "Background")
		if err != nil {
			return nil, err
		}
		var w gooey.Component = &components.HStack{Children: kids, Gap: gap, Background: background}
		if err := attachAll(e, w, attach); err != nil {
			return nil, err
		}
		return w, nil
	},
}

var defCanvas = &ElementDef{
	Name:  "Canvas",
	Icon:  "symbol-ruler",
	Seed:  "<Canvas Width=\"24\" Height=\"6\"><Text Canvas.Left=\"1\" Canvas.Top=\"1\">Canvas</Text></Canvas>",
	Proto: &components.Canvas{},
	Known: true,
	Attrs: []AttrSpec{
		{Name: "Background", Kind: KindColor, Binds: BindsEither, Origin: OriginBuiltin},
	},
	Children: ChildSpec{Mode: ModeMany},
	Grants: Grant{
		Kind: GrantOffset,
		Attached: []AttrSpec{
			{Name: "Canvas.Left", Role: RoleX, Kind: KindInt, Binds: BindsLiteral, Default: "0", Category: CategoryLayout, Origin: OriginBuiltin},
			{Name: "Canvas.Top", Role: RoleY, Kind: KindInt, Binds: BindsLiteral, Default: "0", Category: CategoryLayout, Origin: OriginBuiltin},
		},
	},
	Build: func(e Element, ctx *Context) (gooey.Component, error) {
		// Children carry their own Canvas.Left/Canvas.Top, parsed into
		// Layout by applyLayout like any other attached property.
		kids, attach, err := buildChildren(e, ctx)
		if err != nil {
			return nil, err
		}
		background, err := BoundColor(e, ctx, "Background")
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
	Icon:  "list-unordered",
	Seed:  "<ItemsView Items=\"{{.Items}}\" Selected=\"{{.Selected}}\"><ItemsView.ItemTemplate><Text>{{.Label}}</Text></ItemsView.ItemTemplate></ItemsView>",
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
	Icon:  "check",
	Seed:  "<Checkbox Label=\"Checkbox\" Checked=\"{{.Checked}}\"/>",
	Proto: &components.Checkbox{},
	Known: true,
	Attrs: []AttrSpec{
		{Name: "Checked", Kind: KindBinding, Binds: BindsBinding, GoType: "bool", Required: true, Origin: OriginBuiltin},
		{Name: "Label", Kind: KindText, Binds: BindsEither, Origin: OriginBuiltin},
		{Name: "Style", Kind: KindStyle, Binds: BindsEither, Origin: OriginBuiltin},
	},
	Children: ChildSpec{Mode: ModeLeaf},
	Build: func(e Element, ctx *Context) (gooey.Component, error) {
		checked, err := Bound[bool](e, ctx, "Checked")
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
		style, err := BoundStyle(e, ctx)
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
	Icon:  "dashboard",
	Seed:  "<Gauge Value=\"{{.Value}}\"/>",
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
		value, err := Bound[int](e, ctx, "Value")
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
		if g.Width, err = litInt(e, "BarWidth"); err != nil {
			return nil, err
		}
		// Style is an override for the threshold ramp, so it is applied
		// only when the attribute is actually present.
		if _, ok := e.Attrs["Style"]; ok {
			if g.Style, err = BoundStyle(e, ctx); err != nil {
				return nil, err
			}
		}
		return g, nil
	},
}

var defSparkline = &ElementDef{
	Name:  "Sparkline",
	Icon:  "graph-line",
	Seed:  "<Sparkline Values=\"{{.Values}}\"/>",
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
		series, err := Bound[[]float64](e, ctx, "Values")
		if err != nil {
			return nil, err
		}
		s := &components.Sparkline{Values: series}
		if s.Rows, err = litInt(e, "Height"); err != nil {
			return nil, err
		}
		if s.Width, err = litInt(e, "BarWidth"); err != nil {
			return nil, err
		}
		if _, ok := e.Attrs["Style"]; ok {
			if s.Style, err = BoundStyle(e, ctx); err != nil {
				return nil, err
			}
		}
		return s, nil
	},
}

var defTextBox = &ElementDef{
	Name:  "TextBox",
	Icon:  "edit",
	Seed:  "<TextBox Text=\"{{.Text}}\"/>",
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
		text, err := Bound[string](e, ctx, "Text")
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
			if tb.Style, err = BoundStyle(e, ctx); err != nil {
				return nil, err
			}
		}
		if a, ok := e.Attrs["AccentStyle"]; ok {
			st, err := styleValue(e, ctx, "AccentStyle", a)
			if err != nil {
				return nil, err
			}
			tb.AccentStyle = components.Sty(st)
		}
		// Error is the validation handle: a typed binding to the field's
		// error property (empty = valid), never literal text.
		if suppliedAttr(e, "Error") {
			if tb.Error, err = Bound[string](e, ctx, "Error"); err != nil {
				return nil, err
			}
		}
		if a, ok := e.Attrs["InvalidStyle"]; ok {
			st, err := styleValue(e, ctx, "InvalidStyle", a)
			if err != nil {
				return nil, err
			}
			tb.InvalidStyle = components.Sty(st)
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
	Icon:  "symbol-color",
	Seed:  "<ColorPicker Value=\"{{.Value}}\"/>",
	Proto: &components.ColorPicker{},
	Known: true,
	Attrs: []AttrSpec{
		{Name: "Value", Kind: KindBinding, Binds: BindsBinding, GoType: "render.Color", Required: true, Origin: OriginBuiltin},
	},
	Children: ChildSpec{Mode: ModeLeaf},
	Build: func(e Element, ctx *Context) (gooey.Component, error) {
		color, err := Bound[render.Color](e, ctx, "Value")
		if err != nil {
			return nil, err
		}
		return &components.ColorPicker{Value: color}, nil
	},
}

var defProgressBar = &ElementDef{
	Name:  "ProgressBar",
	Icon:  "loading",
	Seed:  "<ProgressBar Value=\"{{.Value}}\"/>",
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
		value, err := Bound[int](e, ctx, "Value")
		if err != nil {
			return nil, err
		}
		label, err := BoundText(e, ctx, "Label")
		if err != nil {
			return nil, err
		}
		p := &components.ProgressBar{Value: value, Label: label}
		if p.Width, err = litInt(e, "BarWidth"); err != nil {
			return nil, err
		}
		if p.Thresholds, err = litBool(e, "Thresholds"); err != nil {
			return nil, err
		}
		// Indeterminate is optional, and its absence is load-bearing: a
		// bar that can never be indeterminate starts no goroutine.
		if suppliedAttr(e, "Indeterminate") {
			if p.Indeterminate, err = Bound[bool](e, ctx, "Indeterminate"); err != nil {
				return nil, err
			}
		}
		if p.Tick, err = optDuration(e, "Tick"); err != nil {
			return nil, err
		}
		if _, ok := e.Attrs["Style"]; ok {
			if p.Style, err = BoundStyle(e, ctx); err != nil {
				return nil, err
			}
		}
		return p, nil
	},
}

var defSpinner = &ElementDef{
	Name:  "Spinner",
	Icon:  "sync",
	Seed:  "<Spinner/>",
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
		label, err := BoundText(e, ctx, "Label")
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
		if suppliedAttr(e, "Enabled") {
			if s.Enabled, err = Bound[bool](e, ctx, "Enabled"); err != nil {
				return nil, err
			}
		}
		if _, ok := e.Attrs["Style"]; ok {
			if s.Style, err = BoundStyle(e, ctx); err != nil {
				return nil, err
			}
		}
		return s, nil
	},
}

var defToggle = &ElementDef{
	Name:  "Toggle",
	Icon:  "arrow-swap",
	Seed:  "<Toggle Label=\"Toggle\" Checked=\"{{.Checked}}\"/>",
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
		checked, err := Bound[bool](e, ctx, "Checked")
		if err != nil {
			return nil, err
		}
		label, err := BoundText(e, ctx, "Label")
		if err != nil {
			return nil, err
		}
		changed, err := ctx.Command(e.Attrs["Changed"])
		if err != nil {
			return nil, fmt.Errorf("markup: <Toggle Changed=%q>: %w", e.Attrs["Changed"], err)
		}
		style, err := BoundStyle(e, ctx)
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
	Icon:  "list-selection",
	Seed:  "<Segmented Options=\"One,Two\" Selected=\"{{.Selected}}\"/>",
	Proto: &components.Segmented{},
	Known: true,
	Attrs: []AttrSpec{
		{Name: "Changed", Kind: KindCommand, Binds: BindsEither, Origin: OriginBuiltin},
		// Hovered is an OUTPUT: the control writes the index the pointer
		// is over, and -1 when it is outside. Bind a handle in to read
		// it — the usual case is a Tooltip whose Text is a computed over
		// it, since a rail of wordless icons has no other way to say what
		// a slot does (#398).
		//
		// BindsBinding, like Selected: a literal is meaningless for a
		// value the control assigns, and accepting one would silently
		// discard every write.
		{Name: "Hovered", Kind: KindBinding, Binds: BindsBinding, GoType: "int", Origin: OriginBuiltin,
			Doc: "The segment index under the pointer, -1 when outside. Written by the control."},
		{Name: "Options", Kind: KindBinding, Binds: BindsEither, GoType: "[]string", Required: true, Origin: OriginBuiltin},
		{Name: "Selected", Kind: KindBinding, Binds: BindsBinding, GoType: "int", Required: true, Origin: OriginBuiltin},
		{Name: "Style", Kind: KindStyle, Binds: BindsEither, Origin: OriginBuiltin},
		// NO DECLARED DEFAULT, deliberately, even though the effective
		// default is true.
		//
		// TestDeclaredDefaultsAreDiscriminating requires that a declared
		// Default make a STATIC RENDER difference — otherwise the identity
		// test that guards it can never fail, and the declaration is a claim
		// nothing checks. Wrap changes what an arrow key DOES; it paints
		// nothing either way. So the fact lives in Doc, where it is prose
		// rather than an unguarded assertion.
		{Name: "Wrap", Kind: KindBool, Binds: BindsLiteral, Origin: OriginBuiltin,
			Doc: "Cycle the selection at the ends: down at the last segment returns to the first. On unless set to false."},
	},
	Children: ChildSpec{Mode: ModeLeaf},
	Build: func(e Element, ctx *Context) (gooey.Component, error) {
		selected, err := Bound[int](e, ctx, "Selected")
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
		style, err := BoundStyle(e, ctx)
		if err != nil {
			return nil, err
		}
		sg := &components.Segmented{
			Options: options, Selected: selected, Changed: changed, Style: style,
		}
		// Optional, unlike Selected: a strip nobody asks the hover of
		// should not require the page to declare a property for it. The
		// control lazily makes its own when the field is nil.
		// suppliedAttr, not bare key presence: an attribute written empty
		// must read as OMITTED, or Hovered="" would take the bound path
		// and fail on an empty path rather than being ignored.
		if suppliedAttr(e, "Hovered") {
			hovered, err := Bound[int](e, ctx, "Hovered")
			if err != nil {
				return nil, err
			}
			sg.Hovered = hovered
		}
		// Only an explicit Wrap="false" turns cycling off. Absent means nil
		// means on, so the attribute is never written for the default —
		// which keeps "unset" and "set to the default" the same tree.
		//
		// litBool, not `== "true"`: a bool attribute that fell back to
		// false on an unrecognized spelling would turn Wrap="yes" into
		// "stop cycling" silently, and this is the attribute where the two
		// answers are hardest to tell apart by looking. It was optBool
		// until review of #470 — same intent, a laxer grammar, so Wrap="1"
		// loaded here and the identical spelling of ProgressBar Thresholds
		// did not.
		if _, ok := e.Attrs["Wrap"]; ok {
			w, err := litBool(e, "Wrap")
			if err != nil {
				return nil, err
			}
			sg.Wrap = &w
		}
		return named(e, ctx, sg, nil)
	},
}

var defStatusBar = &ElementDef{
	Name:         "StatusBar",
	Icon:         "layout-statusbar",
	Seed:         "<StatusBar Left=\"left\" Center=\"center\" Right=\"right\"/>",
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
	Icon:  "multiple-windows",
	Seed:  "<Tabs><Tab Header=\"One\"><Text>First</Text></Tab><Tab Header=\"Two\"><Text>Second</Text></Tab></Tabs>",
	Proto: &components.Tabs{},
	Known: true,
	Attrs: []AttrSpec{
		{Name: "Changed", Kind: KindCommand, Binds: BindsEither, Origin: OriginBuiltin},
		{Name: "Selected", Kind: KindBinding, Binds: BindsBinding, GoType: "int", Origin: OriginBuiltin},
		{Name: "Style", Kind: KindStyle, Binds: BindsEither, Origin: OriginBuiltin},
	},
	Children: ChildSpec{Mode: ModeRestricted, Only: []string{"Tab"}},
	// GrantOrder: a tab's position IS its index in the strip, and
	// reordering tabs is one of the things an editor is for. Declaring
	// nothing here read as GrantNone — "placed by its parent, nothing to
	// edit" — which the designer showed as DragFixed. Found in review of
	// #390 (issue #418).
	Grants: Grant{Kind: GrantOrder},
	Build: func(e Element, ctx *Context) (gooey.Component, error) {
		tb, err := buildTabs(e, ctx)
		return tb, err
	},
}

var defButtonBar = &ElementDef{
	Name:  "ButtonBar",
	Icon:  "tools",
	Seed:  "<ButtonBar><Button Content=\"One\"/><Button Content=\"Two\"/></ButtonBar>",
	Proto: &components.ButtonBar{},
	Known: true,
	Attrs: []AttrSpec{
		{Name: "Gap", Kind: KindInt, Binds: BindsLiteral, Default: "0", Origin: OriginBuiltin},
		{Name: "Separator", Kind: KindString, Binds: BindsLiteral, Origin: OriginBuiltin},
		{Name: "Uniform", Kind: KindBool, Binds: BindsLiteral, Default: "false", Origin: OriginBuiltin},
	},
	Children: ChildSpec{Mode: ModeMany},
	Grants:   Grant{Kind: GrantOrder},
	Build: func(e Element, ctx *Context) (gooey.Component, error) {
		kids, attach, err := buildChildren(e, ctx)
		if err != nil {
			return nil, err
		}
		bar := &components.ButtonBar{Children: kids, Separator: e.Attrs["Separator"]}
		if bar.Gap, err = litInt(e, "Gap"); err != nil {
			return nil, err
		}
		if bar.Uniform, err = litBool(e, "Uniform"); err != nil {
			return nil, err
		}
		if err := attachAll(e, bar, attach); err != nil {
			return nil, err
		}
		return bar, nil
	},
}

var defMenuBar = &ElementDef{
	Name:  "MenuBar",
	Icon:  "layout-menubar",
	Seed:  "<MenuBar><Menu Title=\"File\"><MenuItem Text=\"Open\"/></Menu></MenuBar>",
	Proto: &components.MenuBar{},
	Known: true,
	Attrs: []AttrSpec{
		{Name: "Style", Kind: KindStyle, Binds: BindsEither, Origin: OriginBuiltin},
	},
	// ONLY <Menu>. The list used to read {"Menu", "MenuItem"}, which
	// declared a child the builder refuses one function below
	// ("<MenuBar> children must be <Menu> elements") — so the vocabulary
	// permitted markup that could not load, and an editor offering it
	// was offering a load error. <MenuItem> is a child of <Menu>, and
	// defMenu below is where it is now declared.
	Children: ChildSpec{Mode: ModeRestricted, Only: []string{"Menu"}},
	// GrantOrder, for the reason <Tabs> carries it: a menu's position on
	// the bar is its index among its siblings, and nothing else about it
	// is geometry.
	Grants: Grant{Kind: GrantOrder},
	Build: func(e Element, ctx *Context) (gooey.Component, error) {
		bar, err := buildMenuBar(e, ctx)
		return bar, err
	},
}

// defMenu and defMenuItem declare the menu vocabulary that <MenuBar>
// consumes as DATA.
//
// Neither builds a component — buildMenuBar reads them into
// []components.Menu and the bar draws the items itself — so both are
// pseudo-elements in exactly the sense <Tab> is, and their Build refuses
// a standalone use the same way.
//
// WHAT IS NEW IS THAT THEY ARE DECLARED AT ALL. They existed only as two
// strings inside MenuBar's ChildSpec.Only, so nothing in the vocabulary
// knew a <MenuItem> has a Text: AttrsFor returned an empty set, the
// designer's property grid had nothing to show for a selected item, and
// the only way to give a menu item its label was to type it in $EDITOR.
// That is #429 as reported — "why don't I see the child properties to
// set content for menu item?"
//
// Known is TRUE, unlike <Tab>'s, and the difference is real rather than
// an oversight either way. <Tab>'s attributes are whatever <Tabs> cares
// to read out of the element and its content is an arbitrary subtree, so
// its vocabulary genuinely is not knowable from here. A <MenuItem>'s is:
// buildMenuBar reads exactly the attributes below and rejects anything
// else about them at load. Declaring an exhaustive set is what
// lets the property grid offer it.
//
// Every Kind and Binds below is READ OFF buildMenuBar rather than
// chosen, which is why two of them are not what they look like:
//
//   - Text is KindText/BindsEither, but a binding resolves to a STATIC
//     string at load and a property HANDLE is refused. The motivating
//     case is a label the markup cannot know ("$EDITOR (nvim)"); the
//     refusal is there because MenuItem.Text is a plain field read while
//     painting, so a handle would be sampled once and never update.
//   - Checked is BindsBinding, not BindsEither, and the builder says why
//     in an error rather than by omission: "a literal check can never
//     change". The same handle the accelerator's KeyBinding writes is
//     the one the box renders, so the check and the key are one state
//     shown twice.
//   - Icon is BindsLiteral where <Image Src> — the same GoType, the same
//     loader, the same fs.FS — is BindsEither. The difference is the
//     FIELD, not the attribute: components.Image.Src is a
//     *prop.Property[image.Image] and can track, MenuItem.Icon is a
//     plain image.Image read while painting and cannot. Declaring
//     BindsEither here would put a binding in a designer's dropdown that
//     the loader then refuses.
var defMenu = &ElementDef{
	Name: "Menu",
	Icon: "list-unordered",
	// A SEED, WHICH THIS DEF ARGUED AGAINST UNTIL SOMETHING READ IT.
	// The argument was that seed_test's walk skips every def with a nil
	// Proto and the palette skips every Nested one, so a seed here would
	// be built by nothing and composed by nothing. That was true when it
	// was written and stopped being true when #460's attribute sweep
	// landed: probeElement seeds a ModeRestricted element's children
	// FROM ITS SEED (defaults_test.go's seedChildren), so with none,
	// every probe of <Menu Title> builds a childless menu and the sweep
	// fails rather than covering it.
	//
	// Kept minimal and identical in shape to <MenuBar>'s: one child, the
	// one the restriction names. It is still not a palette entry — that
	// is the gap #429 names and does not fill — and nothing above needs
	// changing for it, because seedable() filters on a nil Proto.
	Seed:     "<Menu Title=\"File\"><MenuItem Text=\"Open\"/></Menu>",
	ParsedBy: "MenuBar",
	Known:    true,
	Attrs: []AttrSpec{
		{Name: "Title", Kind: KindString, Binds: BindsLiteral, Required: true, Origin: OriginBuiltin,
			Doc: "The name on the menu bar. Required: a menu with no title has nothing to click."},
	},
	// NO GRANT. Order would read as "these can be dragged around", and a
	// menu item is not a component to drag — the bar draws the list
	// itself from data. TestOnlyMultiChildElementsGrantGeometry says the
	// same thing from the other side.
	Children: ChildSpec{Mode: ModeRestricted, Only: []string{"MenuItem"}},
	Build: func(e Element, ctx *Context) (gooey.Component, error) {
		return nil, fmt.Errorf("markup: <Menu> is only valid directly inside <MenuBar>")
	},
}

var defMenuItem = &ElementDef{
	Name:     "MenuItem",
	Icon:     "list-selection",
	ParsedBy: "MenuBar", // and no Seed, for the reason on defMenu
	Known:    true,
	Attrs: []AttrSpec{
		{Name: "Checked", Kind: KindBinding, Binds: BindsBinding, GoType: "bool", Origin: OriginBuiltin,
			Doc: "Makes this a check item. Binds only — a literal check can never change, and this is the same handle the accelerator's KeyBinding writes."},
		{Name: "Command", Kind: KindCommand, Binds: BindsEither, Origin: OriginBuiltin,
			Doc: "What choosing the item does."},
		{Name: "Gesture", Kind: KindGesture, Binds: BindsLiteral, Origin: OriginBuiltin,
			Doc: "The accelerator shown beside the item. Parsed at load, so a typo is a startup error rather than a key that never fires."},
		{Name: "Icon", Kind: KindString, Binds: BindsLiteral, Origin: OriginBuiltin,
			Doc: "A picture for the item, as a path in the page's own FS — the same assets <Image Src> loads from. Drawn only where the terminal has a graphics protocol. Set IconRune as well: an Icon without one is a LOAD ERROR, because the gutter is reserved whatever the terminal can do, so an Icon alone would draw blank columns forever on a terminal with no protocol."},
		{Name: "IconRune", Kind: KindString, Binds: BindsLiteral, Origin: OriginBuiltin,
			Doc: "One glyph for the item, drawn on the cell plane when there is no graphics protocol. Not a fallback rendering of Icon — a one-cell-tall halfblock is two vertical samples, so the two tiers draw different things (#400)."},
		{Name: "Separator", Kind: KindBool, Binds: BindsLiteral, Origin: OriginBuiltin,
			Doc: "A rule instead of an item, and it carries nothing else: EVERY other attribute this element declares is a load error on a separator, because a separator draws none of them. Phrased that way rather than listing them, so the sentence cannot fall behind the declaration it describes — which is what the refusal in markup.go had done."},
		{Name: "Text", Kind: KindText, Binds: BindsEither, Origin: OriginBuiltin,
			Doc: "The label. A binding resolves to a static string ONCE at load — enough for a value the markup cannot know, but a property handle is refused because the label would never update."},
	},
	Children: ChildSpec{Mode: ModeLeaf},
	Build: func(e Element, ctx *Context) (gooey.Component, error) {
		return nil, fmt.Errorf("markup: <MenuItem> is only valid directly inside <Menu>")
	},
}

var defToastHost = &ElementDef{
	Name:  "ToastHost",
	Icon:  "bell",
	Seed:  "<ToastHost Width=\"18\" Height=\"3\"/>",
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
		st, err := styleValue(e, ctx, "Style", e.Attrs["Style"])
		if err != nil {
			return nil, err
		}
		h := &components.ToastHost{Style: st}
		// signedDuration, and the SIGNED half is the point.
		//
		// This read the attribute itself, so an empty value answered
		// with time's own wording rather than the sentence that tells
		// the author omitting it is how to ask for the default. The
		// first fix routed it through optDuration, which also brought
		// the positivity rule — and a NEGATIVE Duration is documented
		// behaviour here: components.ToastHost says "negative means
		// sticky — toasts stay until dismissed". Unifying a vocabulary
		// means asking one question one way, not giving every attribute
		// the same answer. Raised in review of #470, twice.
		d, err := signedDuration(e, "Duration")
		if err != nil {
			return nil, err
		}
		h.Duration = d
		return h, nil
	},
}

var defAdornmentLayer = &ElementDef{
	Name:     "AdornmentLayer",
	Icon:     "layers",
	Seed:     "<AdornmentLayer Width=\"18\" Height=\"3\"/>",
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
	Icon:  "comment",
	Seed:  "<Tooltip Text=\"Tooltip\"/>",
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
		text, err := BoundText(e, ctx, "Text")
		if err != nil {
			return nil, err
		}
		st, err := styleValue(e, ctx, "Style", e.Attrs["Style"])
		if err != nil {
			return nil, err
		}
		t := &components.Tooltip{Text: text, Style: st}
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
	Icon:  "warning",
	Seed:  "<ValidationMarker Error=\"{{.Error}}\"/>",
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
		st, err := styleValue(e, ctx, "Style", e.Attrs["Style"])
		if err != nil {
			return nil, err
		}
		m := &components.ValidationMarker{Style: st}
		if suppliedAttr(e, "Error") {
			var err error
			if m.Error, err = Bound[string](e, ctx, "Error"); err != nil {
				return nil, err
			}
		}
		return m, nil
	},
}

var defKeyBinding = &ElementDef{
	Name:  "KeyBinding",
	Icon:  "record-keys",
	Seed:  "<KeyBinding Gesture=\"ctrl+k\" Command=\"{{.Command}}\"/>",
	Proto: &gooey.KeyBinding{},
	Known: true,
	Attrs: []AttrSpec{
		{Name: "Command", Kind: KindCommand, Binds: BindsEither, Origin: OriginBuiltin},
		// REQUIRED, and it always was in fact: the builder's first
		// statement is ParseGesture, which refuses "" with "empty
		// gesture". Undeclared, the probe harness had nothing to seed it
		// from, so every probe of <KeyBinding Command> failed on the
		// missing Gesture and the Command declaration went unverified in
		// every sweep arm. <Tooltip Gesture> is genuinely optional and
		// is left alone. Raised in review of #470.
		{Name: "Gesture", Kind: KindGesture, Binds: BindsLiteral, Required: true, Origin: OriginBuiltin},
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
	Icon:  "watch",
	Seed:  "<Timer Interval=\"1s\" Tick=\"{{.Tick}}\"/>",
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
		// REQUIRED, so absence is this element's own answer; everything
		// else is optDuration's. The hand-rolled reader this replaced
		// gave the same three refusals in different words, and treated
		// PRESENT-AND-EMPTY as absent — so <Timer Interval=""> got "needs
		// an Interval" where the seven other KindDuration attributes say
		// an empty one is a typo and name the spelling that asks for the
		// default. Raised in review of #470.
		if _, ok := e.Attrs["Interval"]; !ok {
			return nil, fmt.Errorf("markup: <Timer> needs an Interval (e.g. Interval=\"600ms\")")
		}
		d, err := optDuration(e, "Interval")
		if err != nil {
			return nil, err
		}
		tick, err := ctx.Command(e.Attrs["Tick"])
		if err != nil {
			return nil, fmt.Errorf("markup: <Timer Tick=%q>: %w", e.Attrs["Tick"], err)
		}
		t := &components.Timer{Interval: d, Tick: tick}
		// Enabled is optional; absent means always enabled. When present
		// it is a live bool handle, so the graph can pause the timer.
		if suppliedAttr(e, "Enabled") {
			if t.Enabled, err = Bound[bool](e, ctx, "Enabled"); err != nil {
				return nil, err
			}
		}
		return t, nil
	},
}

var defFileWatcher = &ElementDef{
	Name: "FileWatcher",
	// Shared with <Timer> deliberately: the shipped set has no glyph for
	// watching a file, and Doc below already frames this as Timer's other
	// half. Worth a dedicated codicon if one is ever vendored.
	Icon: "watch",
	// Paths is Required, so the seed has to carry one — and it is a
	// literal rather than a binding because a palette drop has no
	// binding to offer yet, and Paths="" is the one value watchPaths
	// rejects outright.
	Seed:  "<FileWatcher Paths=\"notes.md\" Changed=\"{{.Changed}}\"/>",
	Proto: &components.FileWatcher{},
	Known: true,
	Doc:   "Timer's other half: a command run when a watched file or directory changes.",
	Attrs: []AttrSpec{
		{Name: "Changed", Kind: KindCommand, Binds: BindsEither, Origin: OriginBuiltin},
		{Name: "Enabled", Kind: KindBinding, Binds: BindsBinding, GoType: "bool", Origin: OriginBuiltin},
		{Name: "Interval", Kind: KindDuration, Binds: BindsLiteral, Origin: OriginBuiltin},
		{Name: "Path", Kind: KindBinding, Binds: BindsBinding, GoType: "string", Origin: OriginBuiltin},
		{Name: "Paths", Kind: KindBinding, Binds: BindsEither, GoType: "[]string", Required: true, Origin: OriginBuiltin},
	},
	Children: ChildSpec{Mode: ModeLeaf},
	Build: func(e Element, ctx *Context) (gooey.Component, error) {
		// Non-visual like Timer: buildChildren routes it to the parent
		// as an attachment, and the Composer starts and stops it.
		paths, err := watchPaths(e, ctx)
		if err != nil {
			return nil, err
		}
		// The fs.FS seam, and the reason it is an error rather than a
		// silent no-op: a watcher that quietly watches nothing is the
		// failure this component exists to remove. A tree built from
		// bytes has no FS, so it has to say so at LOAD time.
		fsys := ctx.assets()
		if fsys == nil {
			return nil, fmt.Errorf("markup: <FileWatcher Paths=%q>: no file system to watch — this tree was built from bytes; use markup.Load or set Context.Includes", e.Attrs["Paths"])
		}
		changed, err := ctx.Command(e.Attrs["Changed"])
		if err != nil {
			return nil, fmt.Errorf("markup: <FileWatcher Changed=%q>: %w", e.Attrs["Changed"], err)
		}
		w := &components.FileWatcher{FS: fsys, Paths: paths, Changed: changed}
		// Interval is optional here where Timer's is required: a timer
		// with no interval has no meaning, and a watcher with none has
		// the framework's own 300ms hot-reload poll.
		//
		// THROUGH optDuration, not hand-rolled. This was
		// `if raw != "" { … }`, which reads an EMPTY value as "use the
		// default" — the silent fallback optDuration's own doc comment
		// refuses, in the branch whose thesis is that one value has one
		// grammar. Two of the eight duration declarations kept a third
		// answer for an empty string while five were a load error and
		// one was ParseDuration's. Raised in review of #470; the derived
		// arm that finds it is TestALiteralDurationCannotBeEmpty.
		iv, err := optDuration(e, "Interval")
		if err != nil {
			return nil, err
		}
		if iv > 0 {
			w.Interval = iv
		}
		if suppliedAttr(e, "Path") {
			if w.Path, err = Bound[string](e, ctx, "Path"); err != nil {
				return nil, err
			}
		}
		if suppliedAttr(e, "Enabled") {
			if w.Enabled, err = Bound[bool](e, ctx, "Enabled"); err != nil {
				return nil, err
			}
		}
		return w, nil
	},
}

// watchPaths resolves <FileWatcher Paths=…>, which takes either form:
// a binding to the viewmodel's own []string handle, or a literal
// pipe-separated list, which is what a fixed set of sources actually is
// and does not deserve a property in the viewmodel. The separator is
// optionList's, not Grid's comma, because a comma is a legal character
// in a filename and a pipe is one nobody types on purpose.
//
// ZERO PATHS IS INERT, and only through the bound form. A list that
// resolves empty at runtime is a page under construction and is legal,
// which is what makes Paths="{{.MaybeEmpty}}" safe; Paths="" written
// literally is a typo, and gets the same treatment Timer gives a
// missing Interval.
//
// A literal is checked against fs.ValidPath here, at load. An fs.FS
// path is slash-separated, unrooted and never contains "." or ".."
// elements, so Paths="/etc/hosts" or Paths="../x" would fs.Stat as
// ErrInvalid forever — which this component cannot distinguish from
// "not there yet", so it would poll a path that can never exist and
// report nothing. That is precisely the silent watcher, and it is
// cheap to refuse. A BOUND list cannot be checked here and is not:
// the same path arrives as the absent state, and the component's doc
// says so.
func watchPaths(e Element, ctx *Context) (*prop.Property[[]string], error) {
	raw := e.Attrs["Paths"]
	if bindRe.MatchString(raw) {
		return Bound[[]string](e, ctx, "Paths")
	}
	if strings.TrimSpace(raw) == "" {
		return nil, fmt.Errorf(`markup: <FileWatcher> needs Paths (e.g. Paths="notes.md|assets" or Paths="{{.Sources}}")`)
	}
	parts := strings.Split(raw, "|")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
		if !fs.ValidPath(parts[i]) {
			return nil, fmt.Errorf("markup: <FileWatcher Paths=%q>: %q is not a path in this file system — fs.FS paths are slash-separated, unrooted, and have no \".\" or \"..\" elements", raw, parts[i])
		}
	}
	return components.Strs(parts), nil
}

// litInt reads a KindInt / BindsLiteral attribute, and REFUSES what it
// cannot parse or cannot lay out.
//
// Every one of these was `n, _ := strconv.Atoi(e.Attrs["X"])`. Atoi
// returns 0 on failure, so `Gap="wide"`, `BarWidth="8px"` and
// `Height="{{.Rows}}"` all loaded clean and laid out as if the attribute
// had been omitted — the silent drop this vocabulary exists to refuse,
// in the one place nobody was looking, because the discarded error was
// spelled `_`.
//
// ABSENT means the declared Default. PRESENT AND EMPTY does not: `Gap=""`
// is a load error, the same answer the universal literal ints already
// gave — `Width=""` and `Margin=""` fail inside applyLayout — so one
// element could not answer two ways about the same empty string. The
// first version of this helper accepted empty and justified it as "how
// the designer writes 'not set'", which is not what the designer does:
// apps/wysiwyg deletes the attribute on an empty value and never emits
// `X=""`. A rationale that cited a mechanism doing the opposite was
// worse than no rationale. Raised in review of #470.
//
// NEGATIVE IS REFUSED for the same reason unreadable is. Every call site
// is a measured extent — a gap, a bar width, a row count, a grid index —
// and `Gap="-3"` parses cleanly, reaches `y += v.Gap`, and overlaps the
// children it was meant to separate; `BarWidth="-5"` hands layout a
// gooey.Size{W: -5}. The error text already said "a whole number", and
// accepting a negative was the same silent-wrong one arithmetic step
// later. If a signed literal attribute ever exists, it needs its own
// helper and its own reason — and apps/wysiwyg DERIVES the floor from
// this rule (properties.go, stepperKey) rather than keeping a list, so
// that helper is where the exemption would be recorded.
//
// ONE SPELLING PER VALUE is enforced below against strconv.Itoa's
// canonical form, which is the parser's own inverse. It replaces a
// hand-named refusal of a leading `+` that missed leading zeros — review
// of #470 measured `Gap="007"` loading and meaning 7, which is verbatim
// the argument the `+` refusal was making.
//
// The value is quoted UNTRIMMED, so an author who typed `Gap=" wide "`
// is shown the spaces rather than a tidied version that does not match
// their file.
//
// Found by the derived Kind/Binds sweep in #460 — the eleven-row
// spot-check it replaces named none of these.
func litInt(e Element, name string) (int, error) {
	raw, ok := e.Attrs[name]
	if !ok {
		return 0, nil
	}
	return litIntGrammar(e, name, raw)
}

// emptyLiteralWhy is what BOTH literal-int readers say about an empty
// value, in the same words, because it is the same mistake.
//
// They did not. litIntGrammar told the author their value "would
// silently lay out as 0" — which is what would happen if it were
// accepted, not what does happen — and parseThickness said `"" is not a
// whole number of cells`, which is true and no help to somebody who
// wrote Margin="" meaning "none". Review of #470 found the pair
// disagreeing about the one value an author is most likely to leave
// behind mid-edit.
const emptyLiteralWhy = "an empty value is not a zero — it is an attribute " +
	"nobody finished writing, and reading it as 0 would make a half-typed " +
	"document indistinguishable from a deliberate default"

// intSpelling parses a whole number and reports its ONE spelling.
//
// It is shared rather than copied because the two readers of a literal
// int in this package ask the same question: litIntGrammar for every
// declared count, extent, index and offset, and parseThickness for
// Margin. Review of #470 found them answering differently — <Border
// Margin="007"> loaded and meant 7 while <HStack Gap="007"> was refused,
// which is the whole of the argument the Gap refusal makes, one
// attribute across.
//
// The canonical form comes from strconv.Itoa, the parser's own inverse,
// so it covers every second spelling there is and cannot drift from what
// Atoi accepted. The MESSAGES are not shared: each caller knows what its
// number is for and what a wrong one costs, and a sentence general
// enough for both would say neither.
func intSpelling(raw string) (n int, canon, trimmed string, ok bool) {
	trimmed = strings.TrimSpace(raw)
	n, err := strconv.Atoi(trimmed)
	if err != nil {
		return 0, "", trimmed, false
	}
	return n, strconv.Itoa(n), trimmed, true
}

// litIntGrammar is the grammar alone, on an attribute already known to
// be PRESENT. litInt owns absence; cellCount owns required-ness and the
// binding alternative; the rules about what a present literal may say
// live here once.
//
// SPLIT BECAUSE THERE WERE TWO OF THEM. <Image Cols> and <Image Rows>
// went through cellCount, which read its value with a bare strconv.Atoi
// — so `Cols="007"` loaded and meant 7, `Cols="+7"` loaded and meant 7,
// and `Cols=" 3 "` was quoted back to the author tidied. Every one of
// those is refused three lines away for <VStack Gap>, and the whole
// argument of #460 is that one vocabulary may not answer two ways. The
// helper being shaped for litInt's OWN call shape — absent means the
// declared default — is what kept cellCount out of it, since a cell
// count is required and may also be a binding.
//
// Raised in review of #470, which is where the second grammar was found:
// the sweep could not see it, because <Image Cols> is declared
// KindBinding and the literal sweep reads Kind.
func litIntGrammar(e Element, name, raw string) (int, error) {
	n, canon, trimmed, ok := intSpelling(raw)
	if !ok {
		if trimmed == "" {
			return 0, fmt.Errorf("markup: <%s %s=%q>: %s", e.Name, name, raw, emptyLiteralWhy)
		}
		return 0, fmt.Errorf("markup: <%s %s=%q>: %s takes a whole number written "+
			"literally — it is not a binding, and an unreadable value would "+
			"silently lay out as %s=\"0\"", e.Name, name, raw, name, name)
	}
	if n < 0 {
		return 0, fmt.Errorf("markup: <%s %s=%q>: %s is a measurement in cells — an "+
			"extent, a count, an index or an offset — and cannot be negative. It "+
			"parses, so nothing would refuse it: layout overlaps what it was meant "+
			"to separate, addresses no cell, or places a child outside the rect "+
			"that clips it", e.Name, name, raw, name)
	}
	// ONE SPELLING PER VALUE, and it is a canonical-form check rather
	// than a list of bad prefixes.
	//
	// The first version refused a leading `+` by name and shared the
	// UNREADABLE message with it — which claimed the value "would
	// silently lay out as 0", and +2 is perfectly readable and lays out
	// as 2. Review of #470 caught both halves of that: a message stating
	// a consequence that does not happen, and a rule that named one
	// second spelling while `Gap="007"` went on loading and meaning 7 —
	// verbatim the argument the `+` refusal was making.
	//
	// Comparing against strconv.Itoa's output covers every second
	// spelling there is, including ones nobody has thought of, and it
	// cannot drift from the parser because it IS the parser's inverse.
	// It is intSpelling's job now, shared with parseThickness.
	if canon != trimmed {
		return 0, fmt.Errorf("markup: <%s %s=%q>: %s is spelled %q — %q is a second "+
			"way to write the same number, and two documents meaning the same "+
			"layout should not differ in their text", e.Name, name, raw, name,
			canon, trimmed)
	}
	return n, nil
}

// gridLens reads a track list and NAMES the attribute when it will not
// parse.
//
// components.ParseGridLens says only `grid: bad length "{{.B}}"`. That is
// true of the value and silent about which attribute carried it — on an
// element that always has both Rows and Cols, so the author is told a
// string is bad and left to find it. Every other literal in this file
// refuses in the house form, which names the element and the attribute.
//
// Found by the bindsweep discriminator, which requires the refusal to
// name the attribute before it counts as one: <Grid Rows> and <Grid Cols>
// were the only two declarations in the whole vocabulary it could not
// verify. That is the discriminator earning its tightening — the arm was
// green with the loose form and the message was still unusable.
func gridLens(e Element, name string) ([]components.GridLen, error) {
	raw := e.Attrs[name]
	ls, err := components.ParseGridLens(raw)
	if err != nil {
		// %w, NOT %v. Every sibling wrap in this file carries the cause
		// forward, and this one dropped it — so errors.Is and errors.As
		// stopped at the markup layer for track lists alone, while the
		// text looked identical. A caller cannot tell those apart by
		// reading the message, which is why it survived. Raised in
		// review of #470.
		return nil, fmt.Errorf("markup: <%s %s=%q>: %w", e.Name, name, raw, err)
	}
	return ls, nil
}

// litBool reads a KindBool / BindsLiteral attribute, and REFUSES what is
// neither "true" nor "false".
//
// Same defect as litInt in a different spelling: `e.Attrs["X"] == "true"`
// makes every other value mean false, so `Uniform="yes"`,
// `Thresholds="1"` and `Bold="{{.Loud}}"` were accepted and ignored.
//
// THE HOUSE BOOL GRAMMAR, and it is now the only one a component
// attribute uses. optBool read the same attributes through
// strconv.ParseBool, so `Wrap="1"` loaded while `Thresholds="1"` was a
// load error — one vocabulary answering two ways, which is what #460 is
// about. parseCondBool (cond.go) already made the argument for strict:
// a document that can spell a bool five ways is a document where the
// same predicate reads differently in two files, and text bindings
// render a bool as exactly "true"/"false", so this is that round trip.
// Raised in review of #470, which also caught that
// docs/markup-reference.md was already documenting the strict rule for
// <Segmented Wrap> that the code did not implement.
//
// ParseBool survives elsewhere and deliberately is not swept here:
// property.go's kindOf("bool") and resources.go read x:Property and
// resource literals, companion.go:61 reads an environment variable, and
// validate.go reads a declared Default. Those are not component
// attributes; whether they should agree is #473.
//
// Absent means false. PRESENT AND EMPTY is a load error, for litInt's
// reason.
func litBool(e Element, name string) (bool, error) {
	raw, ok := e.Attrs[name]
	if !ok {
		return false, nil
	}
	switch strings.TrimSpace(raw) {
	case "true":
		return true, nil
	case "false":
		return false, nil
	}
	return false, fmt.Errorf("markup: <%s %s=%q>: %s takes \"true\" or \"false\" "+
		"written literally — it is not a binding, and any other value would "+
		"silently mean \"false\"", e.Name, name, raw, name)
}

var defTypeAhead = &ElementDef{
	Name:  "TypeAhead",
	Icon:  "search",
	Seed:  "<TypeAhead Key=\"f\"/>",
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
		// THROUGH optDuration — see <FileWatcher Interval> for why the
		// hand-rolled form was a third grammar for an empty value.
		to, err := optDuration(e, "Timeout")
		if err != nil {
			return nil, err
		}
		if to > 0 {
			t.Timeout = to
		}
		if suppliedAttr(e, "Search") {
			if t.Search, err = Bound[string](e, ctx, "Search"); err != nil {
				return nil, err
			}
		}
		if suppliedAttr(e, "NoMatch") {
			if t.NoMatch, err = Bound[bool](e, ctx, "NoMatch"); err != nil {
				return nil, err
			}
		}
		return t, nil
	},
}

var defImage = &ElementDef{
	Name:  "Image",
	Icon:  "file-media",
	Seed:  "<Image Src=\"{{.Src}}\" Cols=\"8\" Rows=\"4\"/>",
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

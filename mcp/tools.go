package mcp

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/WonderForgeLabs/gooey/control"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/render"
)

// Tool is one MCP tool: what it is called, what it takes, and what it
// does.
//
// Schema is hand-written JSON Schema, handed to the SDK verbatim through
// mcp.Server.AddTool. The SDK's ergonomic path would derive it from a Go
// argument struct by reflection; these tools take nine arguments between
// them, so a literal says the same thing more directly and keeps the
// argument errors this package's own rather than the validator's.
type Tool struct {
	Name        string
	Description string
	Schema      map[string]any

	// OutputSchema, when set, is published to clients as the tool's MCP
	// outputSchema, and successful results are ALSO returned as
	// structuredContent (the same value the text content renders as
	// JSON), so a non-Go client can consume the result schema-checked
	// instead of parsing rendered text. Text content is always kept —
	// clients that only read text keep working. Set it on tools whose
	// results are data; screen_text stays text because its result IS
	// text.
	OutputSchema map[string]any

	// Run executes the tool ON THE UI GOROUTINE. The server marshals
	// every call through the bridge before invoking it, so Run may call
	// control.Service methods freely — they carry the same
	// UI-goroutine-only contract — and there is no path by which it runs
	// anywhere else, which is how this package keeps the confinement
	// rule structural instead of remembered.
	//
	// What Run returns must be plain data. Handing back a component or a
	// property handle would let the http goroutine read the graph after
	// the response went out.
	Run func(a args) (any, error)
}

// v1Tools is the tool inventory. Read: tree_snapshot, screen_text,
// list_values, list_styles. Act: invoke_command, set_value, send_keys,
// send_mouse, focus. Grow the viewmodel: register_properties (#89).
// Mutate structure: swap_markup (optionally registering first),
// patch_markup. Check: validate_markup.
//
// Every body is a thin adapter (issue #112): parse the MCP arguments,
// call the shared control.Service, render the result exactly as this
// surface always has. The v1 ceiling is preserved deliberately — where
// the service can do more than these tools ever did (duration and any
// properties, conditional Actions), the adapter still answers what v1
// answered, byte for byte; growing the tool surface is a separate
// decision, not a refactoring side effect.
func (s *Server) v1Tools() []*Tool {
	return []*Tool{
		{
			Name: "tree_snapshot",
			Description: "The live component tree: element types, Name= identities, arranged bounds, " +
				"layout, visibility, focus and hover flags, the interesting properties of each " +
				"known component kind, and the declared (<x:Property>) surface of markup-built " +
				"controls. This is the app's structure as it exists right now.",
			Schema: object(map[string]any{
				"depth": prop_("integer", "Maximum depth to walk; 0 or absent means the whole tree."),
			}),
			OutputSchema: treeSnapshotSchema(),
			Run:          s.treeSnapshot,
		},
		{
			Name: "screen_text",
			Description: "The current screen as text — one line per terminal row, trailing blanks " +
				"trimmed. This is the retained cell plane as of the last composed frame, i.e. exactly " +
				"what a user is looking at. Set styled for the raw ANSI bytes instead.",
			Schema: object(map[string]any{
				"styled": prop_("boolean", "Return the ANSI escape sequences the terminal would receive instead of plain text."),
			}),
			Run: s.screenText,
		},
		{
			Name: "list_values",
			Description: "The bindable surface: every name in the app's markup context, its kind " +
				"(property, command, or literal), its Go type, and its current value where it has one. " +
				"These are the names set_value and invoke_command take.",
			OutputSchema: listValuesSchema(),
			Run:          s.listValues,
		},
		{
			Name: "list_styles",
			Description: "The registered style names — what a Style=\"...\" attribute in markup can " +
				"refer to — with each style's set attributes (fg, bg, bold, dim, underline, reverse). " +
				"An unknown style name in markup silently renders unstyled, so generate only from these.",
			OutputSchema: listStylesSchema(),
			Run:          s.listStyles,
		},
		{
			Name:        "invoke_command",
			Description: "Run a named command from the markup context — a button's action without needing its coordinates.",
			Schema: object(map[string]any{
				"name": prop_("string", "The command's name in the markup context, e.g. Increment."),
			}, "name"),
			Run: s.invokeCommand,
		},
		{
			Name: "set_value",
			Description: "Set a named source property in the markup context. The value must match the " +
				"property's type; a mismatch is reported with both types and nothing is changed.",
			Schema: object(map[string]any{
				"name":  prop_("string", "The property's name in the markup context."),
				"value": map[string]any{"description": "The new value: string, boolean, number, or a #rrggbb string for a color property."},
			}, "name", "value"),
			Run: s.setValue,
		},
		{
			Name: "send_keys",
			Description: "Inject keystrokes into the app's input stream, routed exactly as the terminal's " +
				"would be: to the focused component, then up its ancestors, then to focus navigation. " +
				"text is typed first, then keys.",
			Schema: object(map[string]any{
				"text": prop_("string", "Literal text to type, one key event per character."),
				"keys": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "Gestures in markup syntax: tab, enter, esc, up, ctrl+s, shift+tab, space.",
				},
			}),
			Run: s.sendKeys,
		},
		{
			Name:        "send_mouse",
			Description: "Inject a pointer event at a cell coordinate. Hit-testing, hover and focus-follows-click all happen as they would from a real terminal.",
			Schema: object(map[string]any{
				"kind":   enum_("What the pointer did.", "click", "press", "release", "move", "wheelup", "wheeldown"),
				"x":      prop_("integer", "Column, 0-based."),
				"y":      prop_("integer", "Row, 0-based."),
				"button": enum_("Which button; default left.", "left", "middle", "right", "none"),
			}, "kind", "x", "y"),
			Run: s.sendMouse,
		},
		{
			Name:        "focus",
			Description: "Move keyboard focus to the element with the given Name. The element must be a focus stop.",
			Schema: object(map[string]any{
				"name": prop_("string", "The element's Name= attribute in the markup."),
			}, "name"),
			Run: s.focus,
		},
		{
			Name: "register_properties",
			Description: "Grow the viewmodel without swapping: register new typed source properties " +
				"into the app's binding context, so later markup (swap_markup, patch_markup) can bind " +
				"names the app never pre-registered. Types are markup's propKinds rows. A name that " +
				"already exists is refused — the context is the one source of truth — and a batch is " +
				"all-or-nothing. Commands cannot be registered; behavior needs code, not storage.",
			Schema: object(map[string]any{
				"properties": registrationsArg("The properties to register."),
			}, "properties"),
			OutputSchema: registerPropertiesSchema(),
			Run:          s.registerProperties,
		},
		{
			Name: "swap_markup",
			Description: "Replace the whole page with new gooey markup, built against the app's existing " +
				"binding context — so the viewmodel, and therefore the app's state, survives the swap. " +
				"register, when given, grows the viewmodel FIRST with new typed source properties, so " +
				"the new page may bind names the app never pre-registered. Atomic: markup that fails to " +
				"parse or bind is reported, the running tree is left untouched, and the registrations " +
				"are rolled back with it.",
			Schema: object(map[string]any{
				"source":   prop_("string", "Complete markup source, rooted at <Gooey> with exactly one child."),
				"register": registrationsArg("New typed source properties to add to the binding context before the build. Rolled back if the build fails."),
			}, "source"),
			Run: s.swapMarkup,
		},
		{
			Name: "patch_markup",
			Description: "Replace ONE named element's subtree with new markup, leaving the rest of the " +
				"page — and every sibling's state — untouched. The fragment is built against the app's " +
				"existing binding context, and its root must carry the same Name= as the element it " +
				"replaces, so the address survives iteration. Layout attributes the fragment does not " +
				"restate (Grid.Row, Width, Margin, ...) are preserved from the old element. Atomic: any " +
				"failure leaves the running tree, the name table and focus exactly as they were.",
			Schema: object(map[string]any{
				"name":   prop_("string", "The Name= of the element to replace."),
				"source": prop_("string", "Markup for the replacement subtree, rooted at <Gooey> with exactly one child carrying the same Name."),
			}, "name", "source"),
			Run: s.patchMarkup,
		},
		{
			Name: "validate_markup",
			Description: "Check markup without touching the app: the exact parse-and-bind path " +
				"swap_markup runs — against the live binding context, including declared properties — " +
				"but nothing is attached and no frame is painted. Invalid markup is a normal result " +
				"carrying the load error text, so a generation loop can retry cheaply without ever " +
				"flickering the live page.",
			Schema: object(map[string]any{
				"source": prop_("string", "Complete markup source, rooted at <Gooey> with exactly one child."),
			}, "source"),
			OutputSchema: validateMarkupSchema(),
			Run:          s.validateMarkup,
		},
	}
}

// ---- read ----

func (s *Server) treeSnapshot(a args) (any, error) {
	n, err := s.svc.Tree(a.optInt("depth", 0))
	if err != nil {
		return nil, err
	}
	return map[string]any{"tree": renderNode(n)}, nil
}

func (s *Server) screenText(a args) (any, error) {
	text, err := s.svc.Screen(a.optBool("styled", false))
	if err != nil {
		return nil, err
	}
	return text, nil
}

func (s *Server) listValues(args) (any, error) {
	entries, named, err := s.svc.Values()
	if err != nil {
		return nil, err
	}
	// Non-nil even when empty: this result is also structuredContent, and
	// the published schema says array, which a nil slice would break by
	// encoding as null.
	out := make([]map[string]any, 0, len(entries))
	for _, ent := range entries {
		out = append(out, renderEntry(ent))
	}
	return map[string]any{"values": out, "named": named}, nil
}

// listStyles reports the style table: the names a Style attribute
// resolves, each with only the attributes the style actually sets — the
// same "report what was set" convention renderLayout uses. This exists
// because an unknown style name renders as zero style with no error, so
// a markup generator that cannot see the table can only guess.
func (s *Server) listStyles(args) (any, error) {
	entries, err := s.svc.Styles()
	if err != nil {
		return nil, err
	}
	styles := make([]map[string]any, 0, len(entries))
	for _, se := range entries {
		st := se.Style
		e := map[string]any{"name": se.Name}
		if st.Fg.Set {
			e["fg"] = hexColor(st.Fg)
		}
		if st.Bg.Set {
			e["bg"] = hexColor(st.Bg)
		}
		if st.Bold {
			e["bold"] = true
		}
		if st.Dim {
			e["dim"] = true
		}
		if st.Underline {
			e["underline"] = true
		}
		if st.Reverse {
			e["reverse"] = true
		}
		styles = append(styles, e)
	}
	return map[string]any{"styles": styles}, nil
}

// renderEntry is one binding-context name as list_values has always
// spelled it. The vocabulary is JSON's ("boolean", "integer", "number"),
// not markup's, and the ceiling is v1's: duration and any properties —
// and conditional (*gooey.Cmd) Actions — which the service can address
// but this tool surface never grew, stay plain "value" entries exactly
// as they did before the service existed.
func renderEntry(ent control.ValueEntry) map[string]any {
	e := map[string]any{"name": ent.Name, "goType": ent.GoType}
	switch ent.Kind {
	case control.EntryProperty:
		switch ent.Type {
		case control.KindString:
			e["kind"], e["type"], e["value"] = "property", "string", ent.Value.Str
		case control.KindBool:
			e["kind"], e["type"], e["value"] = "property", "boolean", ent.Value.Bool
		case control.KindInt:
			e["kind"], e["type"], e["value"] = "property", "integer", ent.Value.Int
		case control.KindFloat:
			e["kind"], e["type"], e["value"] = "property", "number", ent.Value.Float
		case control.KindColor:
			e["kind"], e["type"], e["value"] = "property", "color", hexColor(ent.Value.Color)
		case control.KindDuration, control.KindAny:
			e["kind"] = "value" // the v1 ceiling: never surfaced as properties here
		default:
			// Off the propKinds table: descriptor only, spelled the way
			// this tool always has.
			e["kind"] = "property"
			switch ent.GoType {
			case "*prop.Property[render.Style]":
				e["type"] = "style"
			case "*prop.Property[[]float64]":
				e["type"] = "number[]"
			}
		}
	case control.EntryCommand:
		if plainCommand(ent.GoType) {
			e["kind"] = "command"
		} else {
			e["kind"] = "value" // a conditional Action; v1 listed only plain commands
		}
	case control.EntryLiteral:
		e["kind"], e["type"], e["value"] = "literal", "string", ent.Value.Str
	default:
		e["kind"] = "value"
	}
	return e
}

// plainCommand reports whether a command entry is one of the two shapes
// the v1 tools accepted — gooey.Command and a bare func() — as opposed
// to the Action interface the service also runs.
func plainCommand(goType string) bool {
	return goType == "gooey.Command" || goType == "func()"
}

// ---- act ----

func (s *Server) invokeCommand(a args) (any, error) {
	name, err := a.str("name")
	if err != nil {
		return nil, err
	}
	// The v1 ceiling: the service runs any Action, but this tool only
	// ever ran plain commands, and a refactoring must not widen it.
	if ent, verr := s.svc.Value(name); verr == nil && ent.Kind == control.EntryCommand && !plainCommand(ent.GoType) {
		return nil, fmt.Errorf("%q is %s, not a command; list_values shows which names are commands", name, ent.GoType)
	}
	if err := s.svc.Invoke(name); err != nil {
		return nil, err
	}
	return map[string]any{"invoked": name}, nil
}

func (s *Server) setValue(a args) (any, error) {
	name, err := a.str("name")
	if err != nil {
		return nil, err
	}
	raw, ok := a["value"]
	if !ok {
		return nil, fmt.Errorf("set_value needs a value")
	}
	ent, err := s.svc.Value(name)
	if err != nil {
		return nil, err
	}
	// Coercing the JSON argument is this adapter's job — the service
	// takes a typed control.Value, so the target's kind decides how the
	// raw value must arrive, and a mismatch is an error naming both
	// sides, exactly as before.
	var v control.Value
	if ent.Kind != control.EntryProperty {
		return nil, setValueCeiling(name, ent.GoType)
	}
	switch ent.Type {
	case control.KindString:
		sv, ok := raw.(string)
		if !ok {
			return nil, mismatch(name, "string", raw)
		}
		v = control.StringValue(sv)
	case control.KindBool:
		bv, ok := raw.(bool)
		if !ok {
			return nil, mismatch(name, "boolean", raw)
		}
		v = control.BoolValue(bv)
	case control.KindInt:
		n, ok := jsonInt(raw)
		if !ok {
			return nil, mismatch(name, "integer", raw)
		}
		v = control.IntValue(int64(n))
	case control.KindFloat:
		f, ok := raw.(float64)
		if !ok {
			return nil, mismatch(name, "number", raw)
		}
		v = control.FloatValue(f)
	case control.KindColor:
		sv, ok := raw.(string)
		if !ok {
			return nil, mismatch(name, "color (#rrggbb string)", raw)
		}
		c, err := parseHexColor(sv)
		if err != nil {
			return nil, fmt.Errorf("%q: %w", name, err)
		}
		v = control.ColorValue(c)
	default:
		// duration, any, and off-table handles: the v1 ceiling again.
		return nil, setValueCeiling(name, ent.GoType)
	}
	if err := s.svc.Set(name, v); err != nil {
		return nil, err
	}
	return map[string]any{"set": name, "value": raw}, nil
}

// setValueCeiling is the rejection this tool has always given for a name
// it cannot write, with the handle's %T (the entry's GoType) so both
// sides are named.
func setValueCeiling(name, goType string) error {
	return fmt.Errorf("%q is %s; set_value handles string, boolean, integer, number and color properties", name, goType)
}

func (s *Server) sendKeys(a args) (any, error) {
	consumed, err := s.svc.SendKeys(a.optStr("text", ""), a.strSlice("keys"))
	if err != nil {
		return nil, err
	}
	return map[string]any{"sent": len(consumed), "consumed": consumed}, nil
}

func (s *Server) sendMouse(a args) (any, error) {
	kind, err := a.str("kind")
	if err != nil {
		return nil, err
	}
	x, err := a.intVal("x")
	if err != nil {
		return nil, err
	}
	y, err := a.intVal("y")
	if err != nil {
		return nil, err
	}
	button, err := parseButton(a.optStr("button", "left"))
	if err != nil {
		return nil, err
	}
	p := control.Pointer{X: x, Y: y, Button: button}
	switch strings.ToLower(kind) {
	case "press":
		p.Kind = control.PointerPress
	case "release":
		p.Kind = control.PointerRelease
	case "move":
		p.Kind = control.PointerMove
	case "wheelup":
		p.Kind = control.PointerWheelUp
	case "wheeldown":
		p.Kind = control.PointerWheelDown
	case "click":
		p.Kind = control.PointerClick
		consumed, err := s.svc.SendPointer(p)
		if err != nil {
			return nil, err
		}
		return map[string]any{"kind": "click", "consumed": consumed}, nil
	default:
		return nil, fmt.Errorf("unknown mouse kind %q; want click, press, release, move, wheelup or wheeldown", kind)
	}
	consumed, err := s.svc.SendPointer(p)
	if err != nil {
		return nil, err
	}
	return map[string]any{"kind": kind, "consumed": consumed}, nil
}

func (s *Server) focus(a args) (any, error) {
	name, err := a.str("name")
	if err != nil {
		return nil, err
	}
	if err := s.svc.Focus(name); err != nil {
		return nil, err
	}
	return map[string]any{"focused": name}, nil
}

// ---- grow the viewmodel (#89) ----

// registerProperties is the standalone registration path — the MCP face
// of ControlService.RegisterProperties — for the iterate-on-a-live-page
// loop: register once, then bind the names from as many swaps and
// patches as it takes. The service refuses existing names and applies a
// batch all-or-nothing; both arrive here as ordinary tool errors.
func (s *Server) registerProperties(a args) (any, error) {
	if _, ok := a["properties"]; !ok {
		return nil, fmt.Errorf("missing required argument %q", "properties")
	}
	regs, err := a.registrations("properties")
	if err != nil {
		return nil, err
	}
	if len(regs) == 0 {
		return nil, fmt.Errorf("register_properties needs at least one property")
	}
	if err := s.svc.Register(regs); err != nil {
		return nil, err
	}
	return map[string]any{"registered": registeredNames(regs)}, nil
}

func registeredNames(regs []control.Registration) []string {
	out := make([]string, 0, len(regs))
	for _, r := range regs {
		out = append(out, r.Name)
	}
	return out
}

// ---- mutate structure ----

func (s *Server) swapMarkup(a args) (any, error) {
	src, err := a.str("source")
	if err != nil {
		return nil, err
	}
	regs, err := a.registrations("register")
	if err != nil {
		return nil, err
	}
	named, err := s.svc.SwapMarkup(src, regs)
	if err != nil {
		return nil, err
	}
	out := map[string]any{"swapped": true, "named": named}
	if len(regs) > 0 {
		out["registered"] = registeredNames(regs)
	}
	return out, nil
}

func (s *Server) patchMarkup(a args) (any, error) {
	name, err := a.str("name")
	if err != nil {
		return nil, err
	}
	src, err := a.str("source")
	if err != nil {
		return nil, err
	}
	named, err := s.svc.PatchMarkup(name, src)
	if err != nil {
		return nil, err
	}
	return map[string]any{"patched": name, "named": named}, nil
}

func (s *Server) validateMarkup(a args) (any, error) {
	src, err := a.str("source")
	if err != nil {
		return nil, err
	}
	valid, loadErr, named, err := s.svc.Validate(src)
	if err != nil {
		return nil, err
	}
	if !valid {
		return map[string]any{"valid": false, "error": loadErr}, nil
	}
	return map[string]any{"valid": true, "named": named}, nil
}

// ---- helpers ----

func mismatch(name, want string, got any) error {
	return fmt.Errorf("%q is a %s property; got %T", name, want, got)
}

func parseButton(s string) (input.MouseButton, error) {
	switch strings.ToLower(s) {
	case "", "left":
		return input.ButtonLeft, nil
	case "middle":
		return input.ButtonMiddle, nil
	case "right":
		return input.ButtonRight, nil
	case "none":
		return input.ButtonNone, nil
	}
	return 0, fmt.Errorf("unknown button %q; want left, middle, right or none", s)
}

func hexColor(c render.Color) string {
	if !c.Set {
		return ""
	}
	return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B)
}

func parseHexColor(s string) (render.Color, error) {
	t := strings.TrimPrefix(strings.TrimSpace(s), "#")
	if len(t) != 6 {
		return render.Color{}, fmt.Errorf("%q is not a #rrggbb color", s)
	}
	var v [3]uint8
	for i := 0; i < 3; i++ {
		n, err := hexByte(t[i*2], t[i*2+1])
		if err != nil {
			return render.Color{}, fmt.Errorf("%q is not a #rrggbb color", s)
		}
		v[i] = n
	}
	return render.RGB(v[0], v[1], v[2]), nil
}

func hexByte(hi, lo byte) (uint8, error) {
	h, err := hexDigit(hi)
	if err != nil {
		return 0, err
	}
	l, err := hexDigit(lo)
	if err != nil {
		return 0, err
	}
	return h<<4 | l, nil
}

func hexDigit(b byte) (uint8, error) {
	switch {
	case b >= '0' && b <= '9':
		return b - '0', nil
	case b >= 'a' && b <= 'f':
		return b - 'a' + 10, nil
	case b >= 'A' && b <= 'F':
		return b - 'A' + 10, nil
	}
	return 0, fmt.Errorf("bad hex digit")
}

// ---- argument access ----

// args is a tools/call arguments object. JSON numbers arrive as float64,
// so integers are checked for integrality rather than assumed.
type args map[string]any

func (a args) str(k string) (string, error) {
	v, ok := a[k]
	if !ok {
		return "", fmt.Errorf("missing required argument %q", k)
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("argument %q must be a string, got %T", k, v)
	}
	return s, nil
}

func (a args) intVal(k string) (int, error) {
	v, ok := a[k]
	if !ok {
		return 0, fmt.Errorf("missing required argument %q", k)
	}
	n, ok := jsonInt(v)
	if !ok {
		return 0, fmt.Errorf("argument %q must be a whole number, got %T", k, v)
	}
	return n, nil
}

func (a args) optStr(k, def string) string {
	if s, ok := a[k].(string); ok {
		return s
	}
	return def
}

func (a args) optBool(k string, def bool) bool {
	if b, ok := a[k].(bool); ok {
		return b
	}
	return def
}

func (a args) optInt(k string, def int) int {
	if n, ok := jsonInt(a[k]); ok {
		return n
	}
	return def
}

func (a args) strSlice(k string) []string {
	raw, ok := a[k].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// registrations decodes an array of {name, type, value} objects into
// []control.Registration — the MCP form of the contract's
// PropertyRegistration. An absent key is nil; whether that is allowed
// is the caller's call (swap_markup's register is optional,
// register_properties' properties is required).
func (a args) registrations(k string) ([]control.Registration, error) {
	raw, ok := a[k]
	if !ok {
		return nil, nil
	}
	list, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("argument %q must be an array of {name, type, value} objects, got %T", k, raw)
	}
	out := make([]control.Registration, 0, len(list))
	for _, el := range list {
		m, ok := el.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("argument %q must be an array of {name, type, value} objects; got a %T element", k, el)
		}
		name, _ := m["name"].(string)
		if strings.TrimSpace(name) == "" {
			return nil, fmt.Errorf("a property registration needs a name")
		}
		typ, ok := m["type"].(string)
		if !ok {
			return nil, fmt.Errorf("registration %q needs a type: string, int, bool, float, duration, color or any", name)
		}
		kind := control.KindOf(typ)
		if kind == control.KindUnspecified {
			return nil, fmt.Errorf("registration %q: unknown type %q; want string, int, bool, float, duration, color or any", name, typ)
		}
		reg := control.Registration{Name: name, Kind: kind}
		if rv, ok := m["value"]; ok && rv != nil {
			v, err := registrationInitial(name, kind, rv)
			if err != nil {
				return nil, err
			}
			reg.Initial = &v
		}
		out = append(out, reg)
	}
	return out, nil
}

// registrationInitial coerces a registration's JSON initial value into
// the typed control.Value its declared kind calls for — the same
// adapter job set_value's coercion does, plus the two kinds this NEW
// surface exposes beyond set_value's kept ceiling: duration arrives as
// a Go duration string ("750ms"), and any takes any JSON value, stored
// as decoded JSON.
func registrationInitial(name string, kind control.Kind, raw any) (control.Value, error) {
	switch kind {
	case control.KindString:
		s, ok := raw.(string)
		if !ok {
			return control.Value{}, initialMismatch(name, "a string", raw)
		}
		return control.StringValue(s), nil
	case control.KindInt:
		n, ok := jsonInt(raw)
		if !ok {
			return control.Value{}, initialMismatch(name, "an integer", raw)
		}
		return control.IntValue(int64(n)), nil
	case control.KindBool:
		b, ok := raw.(bool)
		if !ok {
			return control.Value{}, initialMismatch(name, "a boolean", raw)
		}
		return control.BoolValue(b), nil
	case control.KindFloat:
		f, ok := raw.(float64)
		if !ok {
			return control.Value{}, initialMismatch(name, "a number", raw)
		}
		return control.FloatValue(f), nil
	case control.KindDuration:
		s, ok := raw.(string)
		if !ok {
			return control.Value{}, initialMismatch(name, `a duration string (e.g. "750ms")`, raw)
		}
		d, err := time.ParseDuration(strings.TrimSpace(s))
		if err != nil {
			return control.Value{}, fmt.Errorf("registration %q: %q is not a duration; want Go syntax such as 750ms or 1.5s", name, s)
		}
		return control.DurationValue(d), nil
	case control.KindColor:
		s, ok := raw.(string)
		if !ok {
			return control.Value{}, initialMismatch(name, "a color (#rrggbb string)", raw)
		}
		c, err := parseHexColor(s)
		if err != nil {
			return control.Value{}, fmt.Errorf("registration %q: %w", name, err)
		}
		return control.ColorValue(c), nil
	case control.KindAny:
		b, err := json.Marshal(raw)
		if err != nil {
			return control.Value{}, fmt.Errorf("registration %q: %v", name, err)
		}
		return control.JSONValue(b), nil
	}
	return control.Value{}, fmt.Errorf("registration %q: unknown kind", name)
}

func initialMismatch(name, want string, got any) error {
	return fmt.Errorf("registration %q: the initial value must be %s, got %T", name, want, got)
}

func jsonInt(v any) (int, bool) {
	f, ok := v.(float64)
	if !ok {
		if n, ok := v.(int); ok {
			return n, true
		}
		return 0, false
	}
	n := int(f)
	if float64(n) != f {
		return 0, false
	}
	return n, true
}

// ---- schema construction ----

func object(props map[string]any, required ...string) map[string]any {
	m := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		m["required"] = required
	}
	return m
}

func prop_(typ, desc string) map[string]any {
	return map[string]any{"type": typ, "description": desc}
}

// registrationsArg is the {name, type, value} array shape shared by
// swap_markup's register argument and register_properties' properties
// argument — one schema, so the two registration paths can never drift.
func registrationsArg(desc string) map[string]any {
	return map[string]any{
		"type":        "array",
		"description": desc,
		"items": object(map[string]any{
			"name": prop_("string", "The dotted name to create. Nested scopes (A.B) materialize as needed; a name that already exists is refused."),
			"type": enum_("The property's markup type — a propKinds row.", "string", "int", "bool", "float", "duration", "color", "any"),
			"value": map[string]any{"description": "Initial value; absent means the type's zero value. " +
				"string/int/bool/float take the matching JSON value, color a #rrggbb string, " +
				"duration a Go duration string such as \"750ms\", and any takes any JSON value, stored as decoded JSON."},
		}, "name", "type"),
	}
}

func enum_(desc string, values ...string) map[string]any {
	vs := make([]any, len(values))
	for i, v := range values {
		vs[i] = v
	}
	return map[string]any{"type": "string", "description": desc, "enum": vs}
}

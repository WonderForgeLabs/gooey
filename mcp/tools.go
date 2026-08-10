package mcp

import (
	"fmt"
	"sort"
	"strings"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/prop"
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
	// every call through the Dispatcher before invoking it, so Run may
	// read and Set properties, walk the tree and dispatch input freely —
	// and there is no path by which it runs anywhere else, which is how
	// this package keeps the confinement rule structural instead of
	// remembered.
	//
	// What Run returns must be plain data. Handing back a component or a
	// property handle would let the http goroutine read the graph after
	// the response went out.
	Run func(a args) (any, error)
}

// v1Tools is the tool inventory. Read: tree_snapshot, screen_text,
// list_values, list_styles. Act: invoke_command, set_value, send_keys,
// send_mouse, focus. Mutate structure: swap_markup, patch_markup.
// Check: validate_markup.
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
			Name: "swap_markup",
			Description: "Replace the whole page with new gooey markup, built against the app's existing " +
				"binding context — so the viewmodel, and therefore the app's state, survives the swap. " +
				"Markup that fails to parse or bind is reported and the running tree is left untouched.",
			Schema: object(map[string]any{
				"source": prop_("string", "Complete markup source, rooted at <Gooey> with exactly one child."),
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
	c, err := s.composer()
	if err != nil {
		return nil, err
	}
	depth := a.optInt("depth", 0)
	root := c.Root()
	if root == nil {
		return nil, fmt.Errorf("the composition has no root")
	}
	return map[string]any{
		"tree": s.walk(root, names(s.bind), c.Focus(), depth, 1),
	}, nil
}

func (s *Server) screenText(a args) (any, error) {
	c, err := s.composer()
	if err != nil {
		return nil, err
	}
	if a.optBool("styled", false) {
		var sb strings.Builder
		// Snapshot, not Flush: Flush sends the difference since the last
		// frame, and a screenshot wants the screen.
		if err := c.Snapshot(&sb); err != nil {
			return nil, err
		}
		return sb.String(), nil
	}
	buf := c.Cells()
	lines := make([]string, 0, buf.H)
	for y := 0; y < buf.H; y++ {
		row := make([]rune, 0, buf.W)
		for x := 0; x < buf.W; x++ {
			r := buf.At(x, y).Rune
			if r == 0 {
				r = ' '
			}
			row = append(row, r)
		}
		lines = append(lines, strings.TrimRight(string(row), " "))
	}
	return strings.Join(lines, "\n"), nil
}

func (s *Server) listValues(args) (any, error) {
	if s.bind == nil {
		return nil, errNoContext
	}
	// Non-nil even when empty: this result is also structuredContent, and
	// the published schema says array, which a nil slice would break by
	// encoding as null.
	out := make([]map[string]any, 0)
	collectValues(s.bind.Values, "", &out)
	sort.Slice(out, func(i, j int) bool {
		return out[i]["name"].(string) < out[j]["name"].(string)
	})
	return map[string]any{"values": out, "named": namesOf(s.bind.Named)}, nil
}

// listStyles reports markup.Context.Styles: the names a Style attribute
// resolves, each with only the attributes the style actually sets — the
// same "report what was set" convention layoutOf uses. This exists
// because an unknown style name renders as zero style with no error, so
// a markup generator that cannot see the table can only guess.
func (s *Server) listStyles(args) (any, error) {
	if s.bind == nil {
		return nil, errNoContext
	}
	names := make([]string, 0, len(s.bind.Styles))
	for n := range s.bind.Styles {
		names = append(names, n)
	}
	sort.Strings(names)
	styles := make([]map[string]any, 0, len(names))
	for _, n := range names {
		st := s.bind.Styles[n]
		e := map[string]any{"name": n}
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

// collectValues describes the binding context by type switch, the same
// way everything else in gooey inspects a value. A path that resolves to
// a nested map recurses, matching how {{.A.B}} resolves.
func collectValues(vals map[string]any, prefix string, out *[]map[string]any) {
	for k, v := range vals {
		name := k
		if prefix != "" {
			name = prefix + "." + k
		}
		if m, ok := v.(map[string]any); ok {
			collectValues(m, name, out)
			continue
		}
		e := map[string]any{"name": name, "goType": fmt.Sprintf("%T", v)}
		switch h := v.(type) {
		case *prop.Property[string]:
			e["kind"], e["type"], e["value"] = "property", "string", h.Get()
		case *prop.Property[bool]:
			e["kind"], e["type"], e["value"] = "property", "boolean", h.Get()
		case *prop.Property[int]:
			e["kind"], e["type"], e["value"] = "property", "integer", h.Get()
		case *prop.Property[float64]:
			e["kind"], e["type"], e["value"] = "property", "number", h.Get()
		case *prop.Property[render.Color]:
			e["kind"], e["type"], e["value"] = "property", "color", hexColor(h.Get())
		case *prop.Property[render.Style]:
			e["kind"], e["type"] = "property", "style"
		case *prop.Property[[]float64]:
			e["kind"], e["type"], e["value"] = "property", "number[]", len(h.Get())
		case gooey.Command:
			e["kind"] = "command"
		case func():
			e["kind"] = "command"
		case string:
			e["kind"], e["type"], e["value"] = "literal", "string", h
		default:
			e["kind"] = "value"
		}
		*out = append(*out, e)
	}
}

// ---- act ----

func (s *Server) invokeCommand(a args) (any, error) {
	name, err := a.str("name")
	if err != nil {
		return nil, err
	}
	v, err := s.lookup(name)
	if err != nil {
		return nil, err
	}
	switch cmd := v.(type) {
	case gooey.Command:
		cmd()
	case func():
		cmd()
	default:
		return nil, fmt.Errorf("%q is %T, not a command; list_values shows which names are commands", name, v)
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
	v, err := s.lookup(name)
	if err != nil {
		return nil, err
	}
	// The type switch IS the type check, exactly as it is for markup
	// bindings: the handle's T is known at each case, so a mismatch is an
	// error naming both sides rather than a reflective coercion.
	switch h := v.(type) {
	case *prop.Property[string]:
		sv, ok := raw.(string)
		if !ok {
			return nil, mismatch(name, "string", raw)
		}
		h.Set(sv)
	case *prop.Property[bool]:
		bv, ok := raw.(bool)
		if !ok {
			return nil, mismatch(name, "boolean", raw)
		}
		h.Set(bv)
	case *prop.Property[int]:
		n, ok := jsonInt(raw)
		if !ok {
			return nil, mismatch(name, "integer", raw)
		}
		h.Set(n)
	case *prop.Property[float64]:
		f, ok := raw.(float64)
		if !ok {
			return nil, mismatch(name, "number", raw)
		}
		h.Set(f)
	case *prop.Property[render.Color]:
		sv, ok := raw.(string)
		if !ok {
			return nil, mismatch(name, "color (#rrggbb string)", raw)
		}
		c, err := parseHexColor(sv)
		if err != nil {
			return nil, fmt.Errorf("%q: %w", name, err)
		}
		h.Set(c)
	default:
		return nil, fmt.Errorf("%q is %T; set_value handles string, boolean, integer, number and color properties", name, v)
	}
	return map[string]any{"set": name, "value": raw}, nil
}

func (s *Server) sendKeys(a args) (any, error) {
	c, err := s.composer()
	if err != nil {
		return nil, err
	}
	var events []input.Event
	for _, r := range a.optStr("text", "") {
		events = append(events, input.KeyOf(input.Rune(r)))
	}
	for _, g := range a.strSlice("keys") {
		ev, err := input.ParseGesture(g)
		if err != nil {
			return nil, err
		}
		events = append(events, input.KeyOf(ev))
	}
	if len(events) == 0 {
		return nil, fmt.Errorf("send_keys needs text or keys")
	}
	// Composer.Handle, not the App's handler: the app-level quit key is
	// checked on what the tree declines, and an automation client should
	// not be able to end the app by typing ctrl+c at it.
	consumed := make([]bool, len(events))
	for i, ev := range events {
		consumed[i] = c.Handle(ev)
	}
	return map[string]any{"sent": len(events), "consumed": consumed}, nil
}

func (s *Server) sendMouse(a args) (any, error) {
	c, err := s.composer()
	if err != nil {
		return nil, err
	}
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
	ev := input.MouseEvent{X: x, Y: y, Button: button}
	switch strings.ToLower(kind) {
	case "press":
		ev.Kind = input.MousePress
	case "release":
		ev.Kind = input.MouseRelease
	case "move":
		ev.Kind, ev.Button = input.MouseMove, input.ButtonNone
	case "wheelup":
		ev.Kind, ev.Button = input.WheelUp, input.ButtonNone
	case "wheeldown":
		ev.Kind, ev.Button = input.WheelDown, input.ButtonNone
	case "click":
		// A terminal never sends a click: the dispatcher synthesizes one
		// from a press and a release on the same component. Sending the pair
		// is therefore what "click" has to mean here, and it also gets the
		// press-state visual and focus-follows-click for free.
		press, release := ev, ev
		press.Kind, release.Kind = input.MousePress, input.MouseRelease
		h1 := c.HandleMouse(press)
		h2 := c.HandleMouse(release)
		return map[string]any{"kind": "click", "consumed": h1 || h2}, nil
	default:
		return nil, fmt.Errorf("unknown mouse kind %q; want click, press, release, move, wheelup or wheeldown", kind)
	}
	return map[string]any{"kind": kind, "consumed": c.HandleMouse(ev)}, nil
}

func (s *Server) focus(a args) (any, error) {
	c, err := s.composer()
	if err != nil {
		return nil, err
	}
	if s.bind == nil {
		return nil, errNoContext
	}
	name, err := a.str("name")
	if err != nil {
		return nil, err
	}
	w, ok := s.bind.Named[name]
	if !ok {
		return nil, fmt.Errorf("no element named %q; tree_snapshot lists the named elements", name)
	}
	if !c.Focus().SetFocus(w) {
		return nil, fmt.Errorf("element %q (%T) is not a focus stop", name, w)
	}
	return map[string]any{"focused": name}, nil
}

// ---- mutate structure ----

func (s *Server) swapMarkup(a args) (any, error) {
	if s.bind == nil {
		return nil, errNoContext
	}
	src, err := a.str("source")
	if err != nil {
		return nil, err
	}
	// The name table and the declared-surface registry are rebuilt by the
	// load, so a FAILED load must not be allowed to leave them
	// half-written: the running tree is still on screen and `focus` and
	// `tree_snapshot` still name its elements. Build into fresh maps and
	// commit only on success.
	root, restore, err := s.scratchBuild(src)
	if err != nil {
		restore()
		return nil, err
	}
	s.host.Swap(root)
	return map[string]any{"swapped": true, "named": namesOf(s.bind.Named)}, nil
}

// validateMarkup is swap_markup's build path with the attach cut off:
// parse and bind against the live context — declared properties, styles,
// includes, all of it — then throw the tree away and put the context
// back exactly as it was. Nothing is attached, nothing is Set, so no
// paint node dirties and no frame is composed: the check is invisible to
// the running app.
//
// An INVALID document is a normal result, not a tool error: the tool was
// asked whether the markup is valid and it answered. The error text is
// the same typed load error swap_markup would have reported.
func (s *Server) validateMarkup(a args) (any, error) {
	if s.bind == nil {
		return nil, errNoContext
	}
	src, err := a.str("source")
	if err != nil {
		return nil, err
	}
	_, restore, err := s.scratchBuild(src)
	named := namesOf(s.bind.Named)
	restore()
	if err != nil {
		return map[string]any{"valid": false, "error": err.Error()}, nil
	}
	return map[string]any{"valid": true, "named": named}, nil
}

// scratchBuild builds markup source against the binding context with the
// Named table and Declared registry swapped for fresh ones. restore puts
// the previous maps back — call it on failure (or, for a validation, on
// every path); on success the fresh maps stay committed and restore must
// not be called.
func (s *Server) scratchBuild(src string) (root gooey.Component, restore func(), err error) {
	prevNamed, prevDecl := s.bind.Named, s.bind.Declared
	s.bind.Named = map[string]gooey.Component{}
	s.bind.Declared = nil
	restore = func() { s.bind.Named, s.bind.Declared = prevNamed, prevDecl }
	root, err = markup.Build([]byte(src), s.bind)
	return root, restore, err
}

// ---- helpers ----

var errNoContext = fmt.Errorf("this app was served without a markup context, so it has no named values to address")

// namesOf is the sorted Name= identities of a name table.
func namesOf(named map[string]gooey.Component) []string {
	out := make([]string, 0, len(named))
	for n := range named {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

func (s *Server) composer() (*gooey.Composer, error) {
	c := s.host.Composer()
	if c == nil {
		return nil, fmt.Errorf("the app has no live composition yet: it is not running")
	}
	return c, nil
}

// lookup resolves a dotted path in the binding context, the same way a
// {{.A.B}} binding does.
func (s *Server) lookup(path string) (any, error) {
	if s.bind == nil {
		return nil, errNoContext
	}
	var cur any = s.bind.Values
	for _, seg := range strings.Split(path, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("cannot resolve %q past %T", path, cur)
		}
		cur, ok = m[seg]
		if !ok {
			return nil, fmt.Errorf("no value named %q in the app's context; list_values shows what there is", path)
		}
	}
	return cur, nil
}

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

func enum_(desc string, values ...string) map[string]any {
	vs := make([]any, len(values))
	for i, v := range values {
		vs[i] = v
	}
	return map[string]any{"type": "string", "description": desc, "enum": vs}
}

package mcp

import "fmt"

// Output schemas for the tools whose results are data. Publishing one
// makes a result consumable as structuredContent by non-Go MCP clients
// (the Python SDK validates structured results against these), so a
// schema here is a CONTRACT: permissive where the shape is open (extra
// keys allowed everywhere), strict only about what is always present.
//
// screen_text has no schema — its result IS text — and the structural
// mutation tools' small ack objects stay text-only for now; adding
// schemas later is additive. register_properties gets one because its
// result is data an agent acts on: the names now bindable.

// treeSnapshotSchema is the recursive TreeNode shape walk() produces.
func treeSnapshotSchema() map[string]any {
	node := map[string]any{
		"type":        "object",
		"description": "One component in the live tree.",
		"properties": map[string]any{
			"type":      prop_("string", "The Go type, e.g. *components.Button. Diagnostic identity; the durable identity is name."),
			"name":      prop_("string", "The Name= identity from markup; absent if unnamed."),
			"bounds":    boundsSchema(),
			"layout":    map[string]any{"type": "object", "description": "Only the layout fields that were explicitly set (width, margin, gridRow, ...)."},
			"focusable": prop_("boolean", "Present and true when the component is a focus stop."),
			"focused":   prop_("boolean", "Present and true on the focused component."),
			"hovered":   prop_("boolean", "Present and true on the hovered component."),
			"props":     map[string]any{"type": "object", "description": "The type-switched interesting fields of known component kinds."},
			"control":   prop_("string", "For a markup-built control instance: the control file its declarations came from, e.g. card.gooey."),
			"declared": map[string]any{
				"type":        "array",
				"description": "The control's markup-declared (<x:Property>) properties with current values.",
				"items": object(map[string]any{
					"name":  prop_("string", "The declared property name."),
					"type":  prop_("string", "The declared markup type: string, int, bool, float, duration, color, any."),
					"value": map[string]any{"description": "The current value, for types with a markup literal."},
					// NOT "%T", WHICH IS WHAT THIS SAID. These descriptions are
					// plain literals that nothing renders, so the verb was
					// shipped to every generated client verbatim — and it is
					// Go jargon a client reading JSON Schema has no use for.
					// Escaping it as %%T would ship "%%T" instead, which is
					// worse. The sweep that found it could not see this
					// string at all until review of #504 widened it past the
					// top level; rewording is what keeps that widening free
					// of an exemption table.
					"goType": prop_("string", "For Type=\"any\" handles: the dynamic Go type of what the handle holds."),
				}, "name", "type"),
			},
			"childrenElided": prop_("integer", "When a depth limit elided this node's children, how many there were."),
			"attached":       map[string]any{"type": "array", "items": map[string]any{"$ref": "#/$defs/node"}, "description": "Non-visual attachments (KeyBindings, Timers)."},
			"children":       map[string]any{"type": "array", "items": map[string]any{"$ref": "#/$defs/node"}},
		},
		"required": []string{"type"},
	}
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{"tree": map[string]any{"$ref": "#/$defs/node"}},
		"required":   []string{"tree"},
		"$defs":      map[string]any{"node": node},
	}
}

func boundsSchema() map[string]any {
	return object(map[string]any{
		"x": prop_("integer", "Column of the arranged rect, 0-based."),
		"y": prop_("integer", "Row of the arranged rect, 0-based."),
		"w": prop_("integer", "Width in cells."),
		"h": prop_("integer", "Height in cells."),
	}, "x", "y", "w", "h")
}

func listValuesSchema() map[string]any {
	return object(map[string]any{
		"values": map[string]any{
			"type":        "array",
			"description": "Every dotted name in the binding context.",
			"items": object(map[string]any{
				"name":  prop_("string", "The dotted name set_value and invoke_command take."),
				"kind":  enum_("What the name is.", "property", "command", "literal", "value"),
				"type":  prop_("string", "The property's wire type where it has one: string, boolean, integer, number, color, style, number[]."),
				"value": map[string]any{"description": "The current value, where representable."},
				// The same reword as $defs.node's goType above, for the
				// same reason.
				"goType": prop_("string", "The Go type. Diagnostic only."),
			}, "name", "goType"),
		},
		"named": map[string]any{
			"type":        "array",
			"items":       map[string]any{"type": "string"},
			"description": "The Name= identities in the current tree — what focus and patch_markup take.",
		},
	}, "values", "named")
}

func listStylesSchema() map[string]any {
	return object(map[string]any{
		"styles": map[string]any{
			"type":        "array",
			"description": "The registered styles, sorted by name. Only explicitly set attributes appear.",
			"items": object(map[string]any{
				"name":      prop_("string", "The name a Style attribute refers to."),
				"fg":        prop_("string", "Foreground as #rrggbb, when set."),
				"bg":        prop_("string", "Background as #rrggbb, when set."),
				"bold":      prop_("boolean", "Present and true when the style sets bold."),
				"dim":       prop_("boolean", "Present and true when the style sets dim."),
				"underline": prop_("boolean", "Present and true when the style sets underline."),
				"reverse":   prop_("boolean", "Present and true when the style sets reverse video."),
			}, "name"),
		},
	}, "styles")
}

func registerPropertiesSchema() map[string]any {
	return object(map[string]any{
		"registered": map[string]any{
			"type":        "array",
			"items":       map[string]any{"type": "string"},
			"description": "The names just registered, in request order — what list_values will now show and markup can now bind.",
		},
	}, "registered")
}

func unregisterPropertiesSchema() map[string]any {
	return object(map[string]any{
		"unregistered": map[string]any{
			"type":        "array",
			"items":       map[string]any{"type": "string"},
			"description": "The names just removed, in request order — what list_values will no longer show and markup can no longer bind.",
		},
	}, "unregistered")
}

func validateMarkupSchema() map[string]any {
	return object(map[string]any{
		"valid": prop_("boolean", "Whether the markup would build against the app's live binding context."),
		"error": prop_("string", "The typed load error, when valid is false — the same text swap_markup would report."),
		"named": map[string]any{
			"type":        "array",
			"items":       map[string]any{"type": "string"},
			"description": "The Name= identities the document declares, when valid.",
		},
	}, "valid")
}

// THE THREE PAIRS SHARE THEIR TAILS RATHER THAN REPEATING THEM, and
// this paragraph is the const group's own. Each of
// cols/rows, x/y and cellWidth/cellHeight differed in one leading noun
// phrase and was then byte-identical for the rest — 60 to 370
// characters of it, with nothing comparing the copies.
// TestTheScreenSizeSchemaAndItsResultNameTheSameKeys compares KEYS, so
// sharpening the double-conversion warning on x and leaving y behind
// would ship a generated client two different rules for one axis pair,
// and y is the axis the fixtures make non-zero. That is the same move
// this branch spent five rounds making on its prose inventories
// (islandGoneFmt and friends); the schema was the surface where the
// copies were left standing. Raised in review of #504.
const (
	extentTail = " in cells. For a scoped session this is the island's %s, not the " +
		"terminal's. 0 is a real answer, not an error, and it has two causes: a " +
		"scoped session whose island is collapsed or not yet arranged reports 0x0, " +
		"and ANY session reports 0x0 where the terminal itself has no size — a pty " +
		"nobody ran stty on is 0x0 and paints nothing. Neither is a fault to report " +
		"back, and an unscoped session is not evidence of a collapsed island. It may also " +
		"EXCEED the terminal's: the island's arranged rect is reported unclipped, so " +
		"one arranged partly offscreen names cells no terminal has, and the far " +
		"corner converted through x/y is outside the screen."
	originTail = " of the surface's %s edge — add it to a position within the surface " +
		"to put the coordinate in the space send_mouse reads. 0 when unscoped. " +
		"The converse does NOT hold: a session scoped to an island arranged at " +
		"the screen origin reports 0 too, so x/y do not tell a scoped session " +
		"from an unscoped one and 0 is no evidence that this surface is the " +
		"terminal. Which of the two you have is answered by CONTRACT — the " +
		"result is this session's visible surface, whatever its scope — not by " +
		"reading it off the numbers. It " +
		"fixes the coordinate space, not the outcome: whether a point is acted on " +
		"still depends on what is under it. Applies to a position read off " +
		"`screen_text`, which is homed at (0,0); bounds from `tree_snapshot` are " +
		"ALREADY absolute and must not be converted twice."
	cellTail = " of one cell in pixels, for sizing graphics. " + cellProbeRule
)

// pointerFrameRule is the ONE statement of what send_mouse's x and y are
// measured against, and it sits here rather than inline for the reason
// cellProbeRule gives: the caller reads the tool it is about to call,
// not the tool beside it.
//
// originTail already tells a screen_size caller to convert. Nothing told
// a send_mouse caller there was anything to convert FROM — the arguments
// read "Column, 0-based", with no statement of what 0 is. So a scoped
// agent calls screen_text, gets three lines for an island arranged at
// y=1, sends y=0 for the first of them, and is refused with "element …
// is outside this session's island" for a row it can see in its own
// screenshot. Everything else on this branch moved: the description, the
// schema tails, the instructions, the tutorial, both spec records, both
// workflow prompts. The tool that CONSUMES the coordinate did not, which
// is the one surface the agent reads immediately before committing it.
// Raised in review of #504.
const pointerFrameRule = ", 0-based, in ABSOLUTE screen cells — the host's screen, " +
	"not the island. For a scoped session add screen_size's %s to a position read " +
	"off `screen_text`, which is homed at (0,0); bounds from `tree_snapshot` are " +
	"ALREADY absolute and must not be converted twice. Unscoped the two spaces " +
	"coincide and there is nothing to add."

// cellProbeRule is the ONE statement of what a zero cell metric means,
// and it is a const rather than two sentences because the two surfaces
// that carry it had drifted in DIRECTION. This schema said "0 means the
// host never probed"; the tool description said "cell metrics are 0 when
// the host never probed", which reads as never-probed ⇒ 0 and is false —
// App.caps substitutes term.DefaultCellW/H for a pixel-plane host, so a
// graphics app with a pinned encoder and no probe reports 10x20. An
// agent applying the wrong direction sizes a picture against an invented
// measurement, which is the move screen_size exists to replace. Two
// halves of one contract disagreeing is not a thing to assert about; it
// is a thing to make unwritable. Raised in review of #504.
//
// AND THERE ARE TWO SUBSTITUTION SITES, not one. This const named the
// narrower — and it is the copy that ships to clients, while
// control.ScreenSize's doc and docs/learn/08-remote-control.md both name
// both. term.Screen.Detect substitutes DefaultCellW/H on `caps.CellW ==
// 0` alone, with NO plane test (term/term.go), so a cell-plane app run
// with WithCapabilityProbe in a terminal that ignores CSI 16 t reports
// 10x20 too. An agent applying the narrow rule — invented only for a
// graphics host with a pinned encoder, and this host is neither —
// concludes the terminal was measured and sizes a picture against a
// number nobody took. App.caps' backfill is the second site and fires
// only where the first did not. Raised in review of #504, one round
// after the direction was.
const cellProbeRule = "0 MEANS the host never probed the terminal — branch on that " +
	"rather than dividing by it. The converse does not hold, at two separate " +
	"sites: the probe itself substitutes a default whenever the terminal did not " +
	"answer, whatever the app paints on, and a graphics host with a pinned " +
	"encoder substitutes one without probing at all — so non-zero means usable, " +
	"not necessarily measured."

// screenSizeSchema publishes what screen_size answers. Every field is
// required: a client asking for the screen cannot act on a partial one,
// and a field nobody measured says so with 0 rather than going missing —
// an absent key and a zero are different questions for a client, and only
// one of them can be asked without branching on presence.
//
// Nothing here promises ACCEPTANCE, and that is the second correction.
// An earlier draft said adding x/y yields "the coordinate send_mouse
// accepts", which overshoots: mayPoint gates on what the pointer would
// actually reach — hit-test order, an active captor, a frozen host — not
// on the island rectangle. A point inside the rect can still be refused.
// What x/y buy is the right coordinate SPACE; the routing is its own
// question.
//
// A ZERO cols/rows IS AN ANSWER, WITH TWO CAUSES. A scoped session whose
// island is collapsed or not yet arranged resolves successfully to a
// zero-size rect — control.islandRect returns it rather than the
// islandGone denial, because the island is not gone — so screen_size
// reports 0x0 and screen_text is empty. And an UNSCOPED session reports
// 0x0 whenever the terminal has no size: the unscoped arm reads buf.W/H
// straight off the composer, and a pty nobody ran stty on is 0x0 (this
// repo's own demo workflows say so in as many words). Naming only the
// island left a client one explanation for a value with two causes, so
// it would answer "your island is collapsed" to a session that holds no
// grant. The schema said 0 only for the cell metrics, leaving a client
// to read cols:0 as a bug in the host. Raised in review
// of #504.
//
// cols/rows deliberately do NOT claim to be "the range send_mouse
// accepts". For a scoped session they are the island's extent while
// send_mouse takes absolute screen cells, so that sentence — which this
// schema used to carry — was false exactly where it mattered: a guest
// reading it computed coordinates its own pointer call would refuse. x/y
// are what close the gap, so they are described as the conversion rather
// than as decoration.
func screenSizeSchema() map[string]any {
	return object(map[string]any{
		"cols":       prop_("integer", "Width of the visible surface"+fmt.Sprintf(extentTail, "width")),
		"rows":       prop_("integer", "Height of the visible surface"+fmt.Sprintf(extentTail, "height")),
		"x":          prop_("integer", "Absolute screen column"+fmt.Sprintf(originTail, "left")),
		"y":          prop_("integer", "Absolute screen row"+fmt.Sprintf(originTail, "top")),
		"cellWidth":  prop_("integer", "Width"+cellTail),
		"cellHeight": prop_("integer", "Height"+cellTail),
	}, "cols", "rows", "x", "y", "cellWidth", "cellHeight")
}

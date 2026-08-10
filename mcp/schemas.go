package mcp

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
					"name":   prop_("string", "The declared property name."),
					"type":   prop_("string", "The declared markup type: string, int, bool, float, duration, color, any."),
					"value":  map[string]any{"description": "The current value, for types with a markup literal."},
					"goType": prop_("string", "For Type=\"any\" handles: the %T of what the handle holds."),
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
				"name":   prop_("string", "The dotted name set_value and invoke_command take."),
				"kind":   enum_("What the name is.", "property", "command", "literal", "value"),
				"type":   prop_("string", "The property's wire type where it has one: string, boolean, integer, number, color, style, number[]."),
				"value":  map[string]any{"description": "The current value, where representable."},
				"goType": prop_("string", "The Go type (%T). Diagnostic only."),
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

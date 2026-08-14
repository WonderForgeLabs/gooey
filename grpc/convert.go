package grpc

import (
	"bytes"
	"errors"
	"strings"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/control"
	"github.com/WonderForgeLabs/gooey/imaging"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/render"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"

	controlv1 "github.com/WonderForgeLabs/gooey/grpc/gen/gooey/control/v1"
)

// Conversions between the control package's plain-Go types and the
// generated proto messages. Mechanical by design: the two sides mirror
// each other because both mirror markup's propKinds table, so every
// function here is a switch with one case per row and nothing else.

// statusOf maps a service or bridge error onto the contract's status
// codes: NOT_FOUND for unknown names, INVALID_ARGUMENT for type
// mismatches / bad markup / bad gestures, FAILED_PRECONDITION for a
// missing context or composition, DEADLINE_EXCEEDED for a blocked run
// loop, and INTERNAL for a panic the bridge recovered — a client must
// not be able to kill the app, and it also must not mistake the app's
// bug for its own.
func statusOf(err error) error {
	if err == nil {
		return nil
	}
	switch e := err.(type) {
	case *control.Error:
		switch e.Kind {
		case control.KindNotFound:
			return status.Error(codes.NotFound, e.Msg)
		case control.KindFailedPrecondition:
			return status.Error(codes.FailedPrecondition, e.Msg)
		case control.KindPermissionDenied:
			// PERMISSION_DENIED, never NOT_FOUND. A guest reaching outside
			// its island has usually named something that really exists,
			// and answering "no such name" would be a lie that a client
			// reasonably retries.
			return status.Error(codes.PermissionDenied, e.Msg)
		default:
			return status.Error(codes.InvalidArgument, e.Msg)
		}
	case *control.TimeoutError:
		return status.Error(codes.DeadlineExceeded, e.Error())
	case *control.PanicError:
		return status.Error(codes.Internal, e.Error())
	}
	return status.Error(codes.Unknown, err.Error())
}

// ---- value kinds ----

func kindToProto(k control.Kind) controlv1.ValueKind {
	switch k {
	case control.KindString:
		return controlv1.ValueKind_VALUE_KIND_STRING
	case control.KindInt:
		return controlv1.ValueKind_VALUE_KIND_INT
	case control.KindBool:
		return controlv1.ValueKind_VALUE_KIND_BOOL
	case control.KindFloat:
		return controlv1.ValueKind_VALUE_KIND_FLOAT
	case control.KindDuration:
		return controlv1.ValueKind_VALUE_KIND_DURATION
	case control.KindColor:
		return controlv1.ValueKind_VALUE_KIND_COLOR
	case control.KindAny:
		return controlv1.ValueKind_VALUE_KIND_ANY
	}
	return controlv1.ValueKind_VALUE_KIND_UNSPECIFIED
}

func kindFromProto(k controlv1.ValueKind) control.Kind {
	switch k {
	case controlv1.ValueKind_VALUE_KIND_STRING:
		return control.KindString
	case controlv1.ValueKind_VALUE_KIND_INT:
		return control.KindInt
	case controlv1.ValueKind_VALUE_KIND_BOOL:
		return control.KindBool
	case controlv1.ValueKind_VALUE_KIND_FLOAT:
		return control.KindFloat
	case controlv1.ValueKind_VALUE_KIND_DURATION:
		return control.KindDuration
	case controlv1.ValueKind_VALUE_KIND_COLOR:
		return control.KindColor
	case controlv1.ValueKind_VALUE_KIND_ANY:
		return control.KindAny
	}
	return control.KindUnspecified
}

// ---- typed values ----

func valueToProto(v control.Value) *controlv1.TypedValue {
	tv := &controlv1.TypedValue{}
	switch v.Kind {
	case control.KindString:
		tv.Kind = &controlv1.TypedValue_StringValue{StringValue: v.Str}
	case control.KindInt:
		tv.Kind = &controlv1.TypedValue_IntValue{IntValue: v.Int}
	case control.KindBool:
		tv.Kind = &controlv1.TypedValue_BoolValue{BoolValue: v.Bool}
	case control.KindFloat:
		tv.Kind = &controlv1.TypedValue_FloatValue{FloatValue: v.Float}
	case control.KindDuration:
		tv.Kind = &controlv1.TypedValue_DurationValue{DurationValue: durationpb.New(v.Duration)}
	case control.KindColor:
		tv.Kind = &controlv1.TypedValue_ColorValue{ColorValue: colorToProto(v.Color)}
	case control.KindAny:
		tv.Kind = &controlv1.TypedValue_AnyJson{AnyJson: v.JSON}
	case control.KindImage:
		// Exactly what arrived, when the picture came from bytes: a
		// SourceImage kept them, so this is a slice header rather than an
		// encode. A picture built in-process has no source and reports
		// none — never a re-encoding, which would be a different file and
		// would put an encoder on the read path.
		tv.Kind = &controlv1.TypedValue_ImageBytes{ImageBytes: control.ImageBytesOf(v.Image)}
	default:
		return nil
	}
	return tv
}

func valueFromProto(tv *controlv1.TypedValue) (control.Value, error) {
	if tv == nil || tv.Kind == nil {
		return control.Value{}, status.Error(codes.InvalidArgument, "the request carries no typed value")
	}
	switch k := tv.Kind.(type) {
	case *controlv1.TypedValue_StringValue:
		return control.StringValue(k.StringValue), nil
	case *controlv1.TypedValue_IntValue:
		return control.IntValue(k.IntValue), nil
	case *controlv1.TypedValue_BoolValue:
		return control.BoolValue(k.BoolValue), nil
	case *controlv1.TypedValue_FloatValue:
		return control.FloatValue(k.FloatValue), nil
	case *controlv1.TypedValue_DurationValue:
		return control.DurationValue(k.DurationValue.AsDuration()), nil
	case *controlv1.TypedValue_ColorValue:
		return control.ColorValue(colorFromProto(k.ColorValue)), nil
	case *controlv1.TypedValue_AnyJson:
		return control.JSONValue(k.AnyJson), nil
	case *controlv1.TypedValue_ImageBytes:
		// NO BYTES MEANS NO PICTURE, and it round-trips as one.
		//
		// valueToProto emits ImageBytesOf(v.Image) for KindImage, which is
		// empty in two ordinary cases: the value holds no image at all, and
		// the image was built in-process so it never had source bytes to
		// keep. Both then came back through here and were handed to the
		// decoder, which reported "image bytes did not decode" and listed
		// the host's formats — a malformed-file answer to a value that was
		// never malformed, for a picture this server itself had just
		// serialized. Encode/decode has to be a round trip or read-back
		// lies about what was sent.
		if len(k.ImageBytes) == 0 {
			return control.ImageValue(nil), nil
		}
		img, err := imaging.DecodeLimited(bytes.NewReader(k.ImageBytes), "image_bytes", control.ImageLimits())
		if lerr := (*imaging.LimitError)(nil); errors.As(err, &lerr) {
			// A refusal for size is not a malformed-file answer, and
			// saying so is what stops a caller retrying the same bomb.
			return control.Value{}, status.Error(codes.InvalidArgument, lerr.Error())
		}
		if err != nil {
			// Naming what the host can read matters more than the decode
			// error: formats are registered by blank import, so "this
			// build has no SVG" is a host configuration answer, not a
			// malformed-file answer.
			return control.Value{}, status.Errorf(codes.InvalidArgument,
				"image bytes did not decode (%v); this host reads %s", err,
				strings.Join(imaging.Names(), ", "))
		}
		return control.DecodedImageValue(img, k.ImageBytes), nil
	}
	return control.Value{}, status.Error(codes.InvalidArgument, "unknown typed value case")
}

func colorToProto(c render.Color) *controlv1.Color {
	return &controlv1.Color{Set: c.Set, Red: uint32(c.R), Green: uint32(c.G), Blue: uint32(c.B)}
}

func colorFromProto(c *controlv1.Color) render.Color {
	if c == nil {
		return render.Color{}
	}
	return render.Color{Set: c.Set, R: uint8(c.Red), G: uint8(c.Green), B: uint8(c.Blue)}
}

// ---- binding-context entries ----

func entryToProto(e control.ValueEntry) *controlv1.ValueInfo {
	vi := &controlv1.ValueInfo{
		Name:   e.Name,
		GoType: e.GoType,
		Type:   kindToProto(e.Type),
	}
	switch e.Kind {
	case control.EntryProperty:
		vi.Kind = controlv1.EntryKind_ENTRY_KIND_PROPERTY
	case control.EntryCommand:
		vi.Kind = controlv1.EntryKind_ENTRY_KIND_COMMAND
	case control.EntryLiteral:
		vi.Kind = controlv1.EntryKind_ENTRY_KIND_LITERAL
	default:
		vi.Kind = controlv1.EntryKind_ENTRY_KIND_OTHER
	}
	if e.Value != nil {
		vi.Value = valueToProto(*e.Value)
	}
	return vi
}

// ---- the tree ----

func nodeToProto(n *control.Node) *controlv1.TreeNode {
	if n == nil {
		return nil
	}
	tn := &controlv1.TreeNode{
		Type:           n.Type,
		Name:           n.Name,
		Focusable:      n.Focusable,
		Focused:        n.Focused,
		Hovered:        n.Hovered,
		ChildrenElided: int32(n.ChildrenElided),
		Control:        n.Control,
	}
	if n.Bounds != nil {
		tn.Bounds = rectToProto(*n.Bounds)
	}
	if n.Layout != nil {
		tn.Layout = layoutToProto(n.Layout)
	}
	if len(n.Props) > 0 {
		tn.Props = make(map[string]*controlv1.TypedValue, len(n.Props))
		for k, v := range n.Props {
			tn.Props[k] = valueToProto(v)
		}
	}
	for _, a := range n.Attached {
		tn.Attached = append(tn.Attached, nodeToProto(a))
	}
	for _, c := range n.Children {
		tn.Children = append(tn.Children, nodeToProto(c))
	}
	for _, d := range n.Declared {
		dv := &controlv1.DeclaredValue{Name: d.Name, Type: kindToProto(d.Type), GoType: d.GoType}
		if d.Value != nil {
			dv.Value = valueToProto(*d.Value)
		}
		tn.Declared = append(tn.Declared, dv)
	}
	return tn
}

func rectToProto(r gooey.Rect) *controlv1.Rect {
	return &controlv1.Rect{X: int32(r.X), Y: int32(r.Y), Width: int32(r.W), Height: int32(r.H)}
}

func layoutToProto(l *gooey.Layout) *controlv1.Layout {
	out := &controlv1.Layout{
		Width:       int32(l.Width),
		Height:      int32(l.Height),
		HAlign:      alignToProto(l.HAlign),
		VAlign:      alignToProto(l.VAlign),
		GridRow:     int32(l.Row),
		GridCol:     int32(l.Col),
		GridRowSpan: int32(l.RowSpan),
		GridColSpan: int32(l.ColSpan),
		CanvasLeft:  int32(l.Left),
		CanvasTop:   int32(l.Top),
	}
	if l.Margin != (gooey.Thickness{}) {
		out.Margin = &controlv1.Margin{
			Left: int32(l.Margin.L), Top: int32(l.Margin.T),
			Right: int32(l.Margin.R), Bottom: int32(l.Margin.B),
		}
	}
	switch l.Visibility {
	case gooey.Hidden:
		out.Visibility = controlv1.Visibility_VISIBILITY_HIDDEN
	case gooey.Collapsed:
		out.Visibility = controlv1.Visibility_VISIBILITY_COLLAPSED
	}
	return out
}

func alignToProto(a gooey.Align) controlv1.Align {
	switch a {
	case gooey.AlignStart:
		return controlv1.Align_ALIGN_START
	case gooey.AlignCenter:
		return controlv1.Align_ALIGN_CENTER
	case gooey.AlignEnd:
		return controlv1.Align_ALIGN_END
	}
	return controlv1.Align_ALIGN_UNSPECIFIED // Stretch, the framework default
}

// ---- styles and schemas ----

func styleToProto(e control.StyleEntry) *controlv1.StyleInfo {
	si := &controlv1.StyleInfo{
		Name:      e.Name,
		Bold:      e.Style.Bold,
		Dim:       e.Style.Dim,
		Underline: e.Style.Underline,
		Reverse:   e.Style.Reverse,
	}
	if e.Style.Fg.Set {
		si.Fg = colorToProto(e.Style.Fg)
	}
	if e.Style.Bg.Set {
		si.Bg = colorToProto(e.Style.Bg)
	}
	return si
}

func schemaToProto(sc *control.Schema) *controlv1.ControlSchema {
	out := &controlv1.ControlSchema{Control: sc.Control}
	for _, p := range sc.Props {
		out.Properties = append(out.Properties, &controlv1.PropertyDeclaration{
			Name:           p.Name,
			Type:           kindToProto(p.Type),
			DefaultLiteral: p.DefaultLiteral,
			Required:       p.Required,
		})
	}
	return out
}

// ---- registrations ----

func registrationsFromProto(regs []*controlv1.PropertyRegistration) ([]control.Registration, error) {
	out := make([]control.Registration, 0, len(regs))
	for _, r := range regs {
		if r == nil {
			continue
		}
		reg := control.Registration{Name: r.Name, Kind: kindFromProto(r.Kind)}
		if r.Initial != nil {
			v, err := valueFromProto(r.Initial)
			if err != nil {
				return nil, err
			}
			reg.Initial = &v
		}
		out = append(out, reg)
	}
	return out, nil
}

// ---- pointer events ----

func pointerFromProto(ev *controlv1.PointerEvent) (control.Pointer, error) {
	if ev == nil {
		return control.Pointer{}, status.Error(codes.InvalidArgument, "SendPointer needs an event")
	}
	p := control.Pointer{X: int(ev.X), Y: int(ev.Y)}
	switch ev.Kind {
	case controlv1.PointerKind_POINTER_KIND_CLICK:
		p.Kind = control.PointerClick
	case controlv1.PointerKind_POINTER_KIND_PRESS:
		p.Kind = control.PointerPress
	case controlv1.PointerKind_POINTER_KIND_RELEASE:
		p.Kind = control.PointerRelease
	case controlv1.PointerKind_POINTER_KIND_MOVE:
		p.Kind = control.PointerMove
	case controlv1.PointerKind_POINTER_KIND_WHEEL_UP:
		p.Kind = control.PointerWheelUp
	case controlv1.PointerKind_POINTER_KIND_WHEEL_DOWN:
		p.Kind = control.PointerWheelDown
	default:
		return control.Pointer{}, status.Error(codes.InvalidArgument,
			"the pointer event needs a kind: click, press, release, move, wheel_up or wheel_down")
	}
	switch ev.Button {
	case controlv1.MouseButton_MOUSE_BUTTON_UNSPECIFIED, controlv1.MouseButton_MOUSE_BUTTON_LEFT:
		p.Button = input.ButtonLeft // UNSPECIFIED means left, the common case
	case controlv1.MouseButton_MOUSE_BUTTON_MIDDLE:
		p.Button = input.ButtonMiddle
	case controlv1.MouseButton_MOUSE_BUTTON_RIGHT:
		p.Button = input.ButtonRight
	case controlv1.MouseButton_MOUSE_BUTTON_NONE:
		p.Button = input.ButtonNone
	default:
		return control.Pointer{}, status.Error(codes.InvalidArgument, "unknown mouse button")
	}
	return p, nil
}

// inputEventToProto echoes one event from the app's single ordered
// input stream — keys as the gesture spelling ParseGesture reads,
// pointers as PointerEvents (a press is a press; CLICK is a request
// spelling, never an echo).
func inputEventToProto(ev input.Event) *controlv1.InputEvent {
	if ev.IsKey() {
		return &controlv1.InputEvent{Event: &controlv1.InputEvent_Key{
			Key: &controlv1.KeyEvent{Gesture: ev.Key.String()},
		}}
	}
	if !ev.IsMouse() {
		return nil
	}
	m := ev.Mouse
	pe := &controlv1.PointerEvent{X: int32(m.X), Y: int32(m.Y)}
	switch m.Kind {
	case input.MousePress:
		pe.Kind = controlv1.PointerKind_POINTER_KIND_PRESS
	case input.MouseRelease:
		pe.Kind = controlv1.PointerKind_POINTER_KIND_RELEASE
	case input.MouseMove:
		pe.Kind = controlv1.PointerKind_POINTER_KIND_MOVE
	case input.WheelUp:
		pe.Kind = controlv1.PointerKind_POINTER_KIND_WHEEL_UP
	case input.WheelDown:
		pe.Kind = controlv1.PointerKind_POINTER_KIND_WHEEL_DOWN
	}
	switch m.Button {
	case input.ButtonLeft:
		pe.Button = controlv1.MouseButton_MOUSE_BUTTON_LEFT
	case input.ButtonMiddle:
		pe.Button = controlv1.MouseButton_MOUSE_BUTTON_MIDDLE
	case input.ButtonRight:
		pe.Button = controlv1.MouseButton_MOUSE_BUTTON_RIGHT
	case input.ButtonNone:
		pe.Button = controlv1.MouseButton_MOUSE_BUTTON_NONE
	}
	return &controlv1.InputEvent{Event: &controlv1.InputEvent_Pointer{Pointer: pe}}
}

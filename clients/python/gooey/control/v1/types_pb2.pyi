import datetime

from google.protobuf import duration_pb2 as _duration_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class ValueKind(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    VALUE_KIND_UNSPECIFIED: _ClassVar[ValueKind]
    VALUE_KIND_STRING: _ClassVar[ValueKind]
    VALUE_KIND_INT: _ClassVar[ValueKind]
    VALUE_KIND_BOOL: _ClassVar[ValueKind]
    VALUE_KIND_FLOAT: _ClassVar[ValueKind]
    VALUE_KIND_DURATION: _ClassVar[ValueKind]
    VALUE_KIND_COLOR: _ClassVar[ValueKind]
    VALUE_KIND_ANY: _ClassVar[ValueKind]

class EntryKind(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    ENTRY_KIND_UNSPECIFIED: _ClassVar[EntryKind]
    ENTRY_KIND_PROPERTY: _ClassVar[EntryKind]
    ENTRY_KIND_COMMAND: _ClassVar[EntryKind]
    ENTRY_KIND_LITERAL: _ClassVar[EntryKind]
    ENTRY_KIND_OTHER: _ClassVar[EntryKind]

class Align(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    ALIGN_UNSPECIFIED: _ClassVar[Align]
    ALIGN_START: _ClassVar[Align]
    ALIGN_CENTER: _ClassVar[Align]
    ALIGN_END: _ClassVar[Align]

class Visibility(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    VISIBILITY_UNSPECIFIED: _ClassVar[Visibility]
    VISIBILITY_HIDDEN: _ClassVar[Visibility]
    VISIBILITY_COLLAPSED: _ClassVar[Visibility]

class PointerKind(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    POINTER_KIND_UNSPECIFIED: _ClassVar[PointerKind]
    POINTER_KIND_CLICK: _ClassVar[PointerKind]
    POINTER_KIND_PRESS: _ClassVar[PointerKind]
    POINTER_KIND_RELEASE: _ClassVar[PointerKind]
    POINTER_KIND_MOVE: _ClassVar[PointerKind]
    POINTER_KIND_WHEEL_UP: _ClassVar[PointerKind]
    POINTER_KIND_WHEEL_DOWN: _ClassVar[PointerKind]

class MouseButton(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    MOUSE_BUTTON_UNSPECIFIED: _ClassVar[MouseButton]
    MOUSE_BUTTON_LEFT: _ClassVar[MouseButton]
    MOUSE_BUTTON_MIDDLE: _ClassVar[MouseButton]
    MOUSE_BUTTON_RIGHT: _ClassVar[MouseButton]
    MOUSE_BUTTON_NONE: _ClassVar[MouseButton]
VALUE_KIND_UNSPECIFIED: ValueKind
VALUE_KIND_STRING: ValueKind
VALUE_KIND_INT: ValueKind
VALUE_KIND_BOOL: ValueKind
VALUE_KIND_FLOAT: ValueKind
VALUE_KIND_DURATION: ValueKind
VALUE_KIND_COLOR: ValueKind
VALUE_KIND_ANY: ValueKind
ENTRY_KIND_UNSPECIFIED: EntryKind
ENTRY_KIND_PROPERTY: EntryKind
ENTRY_KIND_COMMAND: EntryKind
ENTRY_KIND_LITERAL: EntryKind
ENTRY_KIND_OTHER: EntryKind
ALIGN_UNSPECIFIED: Align
ALIGN_START: Align
ALIGN_CENTER: Align
ALIGN_END: Align
VISIBILITY_UNSPECIFIED: Visibility
VISIBILITY_HIDDEN: Visibility
VISIBILITY_COLLAPSED: Visibility
POINTER_KIND_UNSPECIFIED: PointerKind
POINTER_KIND_CLICK: PointerKind
POINTER_KIND_PRESS: PointerKind
POINTER_KIND_RELEASE: PointerKind
POINTER_KIND_MOVE: PointerKind
POINTER_KIND_WHEEL_UP: PointerKind
POINTER_KIND_WHEEL_DOWN: PointerKind
MOUSE_BUTTON_UNSPECIFIED: MouseButton
MOUSE_BUTTON_LEFT: MouseButton
MOUSE_BUTTON_MIDDLE: MouseButton
MOUSE_BUTTON_RIGHT: MouseButton
MOUSE_BUTTON_NONE: MouseButton

class TypedValue(_message.Message):
    __slots__ = ("string_value", "int_value", "bool_value", "float_value", "duration_value", "color_value", "any_json")
    STRING_VALUE_FIELD_NUMBER: _ClassVar[int]
    INT_VALUE_FIELD_NUMBER: _ClassVar[int]
    BOOL_VALUE_FIELD_NUMBER: _ClassVar[int]
    FLOAT_VALUE_FIELD_NUMBER: _ClassVar[int]
    DURATION_VALUE_FIELD_NUMBER: _ClassVar[int]
    COLOR_VALUE_FIELD_NUMBER: _ClassVar[int]
    ANY_JSON_FIELD_NUMBER: _ClassVar[int]
    string_value: str
    int_value: int
    bool_value: bool
    float_value: float
    duration_value: _duration_pb2.Duration
    color_value: Color
    any_json: bytes
    def __init__(self, string_value: _Optional[str] = ..., int_value: _Optional[int] = ..., bool_value: _Optional[bool] = ..., float_value: _Optional[float] = ..., duration_value: _Optional[_Union[datetime.timedelta, _duration_pb2.Duration, _Mapping]] = ..., color_value: _Optional[_Union[Color, _Mapping]] = ..., any_json: _Optional[bytes] = ...) -> None: ...

class Color(_message.Message):
    __slots__ = ("set", "red", "green", "blue")
    SET_FIELD_NUMBER: _ClassVar[int]
    RED_FIELD_NUMBER: _ClassVar[int]
    GREEN_FIELD_NUMBER: _ClassVar[int]
    BLUE_FIELD_NUMBER: _ClassVar[int]
    set: bool
    red: int
    green: int
    blue: int
    def __init__(self, set: _Optional[bool] = ..., red: _Optional[int] = ..., green: _Optional[int] = ..., blue: _Optional[int] = ...) -> None: ...

class PropertyDeclaration(_message.Message):
    __slots__ = ("name", "type", "default_literal", "required")
    NAME_FIELD_NUMBER: _ClassVar[int]
    TYPE_FIELD_NUMBER: _ClassVar[int]
    DEFAULT_LITERAL_FIELD_NUMBER: _ClassVar[int]
    REQUIRED_FIELD_NUMBER: _ClassVar[int]
    name: str
    type: ValueKind
    default_literal: str
    required: bool
    def __init__(self, name: _Optional[str] = ..., type: _Optional[_Union[ValueKind, str]] = ..., default_literal: _Optional[str] = ..., required: _Optional[bool] = ...) -> None: ...

class ControlSchema(_message.Message):
    __slots__ = ("control", "properties")
    CONTROL_FIELD_NUMBER: _ClassVar[int]
    PROPERTIES_FIELD_NUMBER: _ClassVar[int]
    control: str
    properties: _containers.RepeatedCompositeFieldContainer[PropertyDeclaration]
    def __init__(self, control: _Optional[str] = ..., properties: _Optional[_Iterable[_Union[PropertyDeclaration, _Mapping]]] = ...) -> None: ...

class PropertyRegistration(_message.Message):
    __slots__ = ("name", "kind", "initial")
    NAME_FIELD_NUMBER: _ClassVar[int]
    KIND_FIELD_NUMBER: _ClassVar[int]
    INITIAL_FIELD_NUMBER: _ClassVar[int]
    name: str
    kind: ValueKind
    initial: TypedValue
    def __init__(self, name: _Optional[str] = ..., kind: _Optional[_Union[ValueKind, str]] = ..., initial: _Optional[_Union[TypedValue, _Mapping]] = ...) -> None: ...

class ValueInfo(_message.Message):
    __slots__ = ("name", "kind", "type", "value", "go_type")
    NAME_FIELD_NUMBER: _ClassVar[int]
    KIND_FIELD_NUMBER: _ClassVar[int]
    TYPE_FIELD_NUMBER: _ClassVar[int]
    VALUE_FIELD_NUMBER: _ClassVar[int]
    GO_TYPE_FIELD_NUMBER: _ClassVar[int]
    name: str
    kind: EntryKind
    type: ValueKind
    value: TypedValue
    go_type: str
    def __init__(self, name: _Optional[str] = ..., kind: _Optional[_Union[EntryKind, str]] = ..., type: _Optional[_Union[ValueKind, str]] = ..., value: _Optional[_Union[TypedValue, _Mapping]] = ..., go_type: _Optional[str] = ...) -> None: ...

class PropertyChange(_message.Message):
    __slots__ = ("name", "value")
    NAME_FIELD_NUMBER: _ClassVar[int]
    VALUE_FIELD_NUMBER: _ClassVar[int]
    name: str
    value: TypedValue
    def __init__(self, name: _Optional[str] = ..., value: _Optional[_Union[TypedValue, _Mapping]] = ...) -> None: ...

class Rect(_message.Message):
    __slots__ = ("x", "y", "width", "height")
    X_FIELD_NUMBER: _ClassVar[int]
    Y_FIELD_NUMBER: _ClassVar[int]
    WIDTH_FIELD_NUMBER: _ClassVar[int]
    HEIGHT_FIELD_NUMBER: _ClassVar[int]
    x: int
    y: int
    width: int
    height: int
    def __init__(self, x: _Optional[int] = ..., y: _Optional[int] = ..., width: _Optional[int] = ..., height: _Optional[int] = ...) -> None: ...

class Margin(_message.Message):
    __slots__ = ("left", "top", "right", "bottom")
    LEFT_FIELD_NUMBER: _ClassVar[int]
    TOP_FIELD_NUMBER: _ClassVar[int]
    RIGHT_FIELD_NUMBER: _ClassVar[int]
    BOTTOM_FIELD_NUMBER: _ClassVar[int]
    left: int
    top: int
    right: int
    bottom: int
    def __init__(self, left: _Optional[int] = ..., top: _Optional[int] = ..., right: _Optional[int] = ..., bottom: _Optional[int] = ...) -> None: ...

class Layout(_message.Message):
    __slots__ = ("width", "height", "margin", "h_align", "v_align", "visibility", "grid_row", "grid_col", "grid_row_span", "grid_col_span", "canvas_left", "canvas_top")
    WIDTH_FIELD_NUMBER: _ClassVar[int]
    HEIGHT_FIELD_NUMBER: _ClassVar[int]
    MARGIN_FIELD_NUMBER: _ClassVar[int]
    H_ALIGN_FIELD_NUMBER: _ClassVar[int]
    V_ALIGN_FIELD_NUMBER: _ClassVar[int]
    VISIBILITY_FIELD_NUMBER: _ClassVar[int]
    GRID_ROW_FIELD_NUMBER: _ClassVar[int]
    GRID_COL_FIELD_NUMBER: _ClassVar[int]
    GRID_ROW_SPAN_FIELD_NUMBER: _ClassVar[int]
    GRID_COL_SPAN_FIELD_NUMBER: _ClassVar[int]
    CANVAS_LEFT_FIELD_NUMBER: _ClassVar[int]
    CANVAS_TOP_FIELD_NUMBER: _ClassVar[int]
    width: int
    height: int
    margin: Margin
    h_align: Align
    v_align: Align
    visibility: Visibility
    grid_row: int
    grid_col: int
    grid_row_span: int
    grid_col_span: int
    canvas_left: int
    canvas_top: int
    def __init__(self, width: _Optional[int] = ..., height: _Optional[int] = ..., margin: _Optional[_Union[Margin, _Mapping]] = ..., h_align: _Optional[_Union[Align, str]] = ..., v_align: _Optional[_Union[Align, str]] = ..., visibility: _Optional[_Union[Visibility, str]] = ..., grid_row: _Optional[int] = ..., grid_col: _Optional[int] = ..., grid_row_span: _Optional[int] = ..., grid_col_span: _Optional[int] = ..., canvas_left: _Optional[int] = ..., canvas_top: _Optional[int] = ...) -> None: ...

class TreeNode(_message.Message):
    __slots__ = ("type", "name", "bounds", "layout", "focusable", "focused", "hovered", "props", "attached", "children", "children_elided", "declared", "control")
    class PropsEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: TypedValue
        def __init__(self, key: _Optional[str] = ..., value: _Optional[_Union[TypedValue, _Mapping]] = ...) -> None: ...
    TYPE_FIELD_NUMBER: _ClassVar[int]
    NAME_FIELD_NUMBER: _ClassVar[int]
    BOUNDS_FIELD_NUMBER: _ClassVar[int]
    LAYOUT_FIELD_NUMBER: _ClassVar[int]
    FOCUSABLE_FIELD_NUMBER: _ClassVar[int]
    FOCUSED_FIELD_NUMBER: _ClassVar[int]
    HOVERED_FIELD_NUMBER: _ClassVar[int]
    PROPS_FIELD_NUMBER: _ClassVar[int]
    ATTACHED_FIELD_NUMBER: _ClassVar[int]
    CHILDREN_FIELD_NUMBER: _ClassVar[int]
    CHILDREN_ELIDED_FIELD_NUMBER: _ClassVar[int]
    DECLARED_FIELD_NUMBER: _ClassVar[int]
    CONTROL_FIELD_NUMBER: _ClassVar[int]
    type: str
    name: str
    bounds: Rect
    layout: Layout
    focusable: bool
    focused: bool
    hovered: bool
    props: _containers.MessageMap[str, TypedValue]
    attached: _containers.RepeatedCompositeFieldContainer[TreeNode]
    children: _containers.RepeatedCompositeFieldContainer[TreeNode]
    children_elided: int
    declared: _containers.RepeatedCompositeFieldContainer[DeclaredValue]
    control: str
    def __init__(self, type: _Optional[str] = ..., name: _Optional[str] = ..., bounds: _Optional[_Union[Rect, _Mapping]] = ..., layout: _Optional[_Union[Layout, _Mapping]] = ..., focusable: _Optional[bool] = ..., focused: _Optional[bool] = ..., hovered: _Optional[bool] = ..., props: _Optional[_Mapping[str, TypedValue]] = ..., attached: _Optional[_Iterable[_Union[TreeNode, _Mapping]]] = ..., children: _Optional[_Iterable[_Union[TreeNode, _Mapping]]] = ..., children_elided: _Optional[int] = ..., declared: _Optional[_Iterable[_Union[DeclaredValue, _Mapping]]] = ..., control: _Optional[str] = ...) -> None: ...

class DeclaredValue(_message.Message):
    __slots__ = ("name", "type", "value", "go_type")
    NAME_FIELD_NUMBER: _ClassVar[int]
    TYPE_FIELD_NUMBER: _ClassVar[int]
    VALUE_FIELD_NUMBER: _ClassVar[int]
    GO_TYPE_FIELD_NUMBER: _ClassVar[int]
    name: str
    type: ValueKind
    value: TypedValue
    go_type: str
    def __init__(self, name: _Optional[str] = ..., type: _Optional[_Union[ValueKind, str]] = ..., value: _Optional[_Union[TypedValue, _Mapping]] = ..., go_type: _Optional[str] = ...) -> None: ...

class StyleInfo(_message.Message):
    __slots__ = ("name", "fg", "bg", "bold", "dim", "underline", "reverse")
    NAME_FIELD_NUMBER: _ClassVar[int]
    FG_FIELD_NUMBER: _ClassVar[int]
    BG_FIELD_NUMBER: _ClassVar[int]
    BOLD_FIELD_NUMBER: _ClassVar[int]
    DIM_FIELD_NUMBER: _ClassVar[int]
    UNDERLINE_FIELD_NUMBER: _ClassVar[int]
    REVERSE_FIELD_NUMBER: _ClassVar[int]
    name: str
    fg: Color
    bg: Color
    bold: bool
    dim: bool
    underline: bool
    reverse: bool
    def __init__(self, name: _Optional[str] = ..., fg: _Optional[_Union[Color, _Mapping]] = ..., bg: _Optional[_Union[Color, _Mapping]] = ..., bold: _Optional[bool] = ..., dim: _Optional[bool] = ..., underline: _Optional[bool] = ..., reverse: _Optional[bool] = ...) -> None: ...

class PointerEvent(_message.Message):
    __slots__ = ("kind", "x", "y", "button")
    KIND_FIELD_NUMBER: _ClassVar[int]
    X_FIELD_NUMBER: _ClassVar[int]
    Y_FIELD_NUMBER: _ClassVar[int]
    BUTTON_FIELD_NUMBER: _ClassVar[int]
    kind: PointerKind
    x: int
    y: int
    button: MouseButton
    def __init__(self, kind: _Optional[_Union[PointerKind, str]] = ..., x: _Optional[int] = ..., y: _Optional[int] = ..., button: _Optional[_Union[MouseButton, str]] = ...) -> None: ...

class KeyEvent(_message.Message):
    __slots__ = ("gesture",)
    GESTURE_FIELD_NUMBER: _ClassVar[int]
    gesture: str
    def __init__(self, gesture: _Optional[str] = ...) -> None: ...

class InputEvent(_message.Message):
    __slots__ = ("key", "pointer")
    KEY_FIELD_NUMBER: _ClassVar[int]
    POINTER_FIELD_NUMBER: _ClassVar[int]
    key: KeyEvent
    pointer: PointerEvent
    def __init__(self, key: _Optional[_Union[KeyEvent, _Mapping]] = ..., pointer: _Optional[_Union[PointerEvent, _Mapping]] = ...) -> None: ...

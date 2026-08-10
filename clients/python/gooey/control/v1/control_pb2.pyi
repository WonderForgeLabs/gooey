from gooey.control.v1 import types_pb2 as _types_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class SnapshotTreeRequest(_message.Message):
    __slots__ = ("depth",)
    DEPTH_FIELD_NUMBER: _ClassVar[int]
    depth: int
    def __init__(self, depth: _Optional[int] = ...) -> None: ...

class SnapshotTreeResponse(_message.Message):
    __slots__ = ("root",)
    ROOT_FIELD_NUMBER: _ClassVar[int]
    root: _types_pb2.TreeNode
    def __init__(self, root: _Optional[_Union[_types_pb2.TreeNode, _Mapping]] = ...) -> None: ...

class ScreenTextRequest(_message.Message):
    __slots__ = ("styled",)
    STYLED_FIELD_NUMBER: _ClassVar[int]
    styled: bool
    def __init__(self, styled: _Optional[bool] = ...) -> None: ...

class ScreenTextResponse(_message.Message):
    __slots__ = ("text",)
    TEXT_FIELD_NUMBER: _ClassVar[int]
    text: str
    def __init__(self, text: _Optional[str] = ...) -> None: ...

class ListValuesRequest(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class ListValuesResponse(_message.Message):
    __slots__ = ("values", "named")
    VALUES_FIELD_NUMBER: _ClassVar[int]
    NAMED_FIELD_NUMBER: _ClassVar[int]
    values: _containers.RepeatedCompositeFieldContainer[_types_pb2.ValueInfo]
    named: _containers.RepeatedScalarFieldContainer[str]
    def __init__(self, values: _Optional[_Iterable[_Union[_types_pb2.ValueInfo, _Mapping]]] = ..., named: _Optional[_Iterable[str]] = ...) -> None: ...

class GetPropertyRequest(_message.Message):
    __slots__ = ("name",)
    NAME_FIELD_NUMBER: _ClassVar[int]
    name: str
    def __init__(self, name: _Optional[str] = ...) -> None: ...

class GetPropertyResponse(_message.Message):
    __slots__ = ("value",)
    VALUE_FIELD_NUMBER: _ClassVar[int]
    value: _types_pb2.ValueInfo
    def __init__(self, value: _Optional[_Union[_types_pb2.ValueInfo, _Mapping]] = ...) -> None: ...

class SetPropertyRequest(_message.Message):
    __slots__ = ("name", "value")
    NAME_FIELD_NUMBER: _ClassVar[int]
    VALUE_FIELD_NUMBER: _ClassVar[int]
    name: str
    value: _types_pb2.TypedValue
    def __init__(self, name: _Optional[str] = ..., value: _Optional[_Union[_types_pb2.TypedValue, _Mapping]] = ...) -> None: ...

class SetPropertyResponse(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class InvokeCommandRequest(_message.Message):
    __slots__ = ("name",)
    NAME_FIELD_NUMBER: _ClassVar[int]
    name: str
    def __init__(self, name: _Optional[str] = ...) -> None: ...

class InvokeCommandResponse(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class SendKeysRequest(_message.Message):
    __slots__ = ("text", "gestures")
    TEXT_FIELD_NUMBER: _ClassVar[int]
    GESTURES_FIELD_NUMBER: _ClassVar[int]
    text: str
    gestures: _containers.RepeatedScalarFieldContainer[str]
    def __init__(self, text: _Optional[str] = ..., gestures: _Optional[_Iterable[str]] = ...) -> None: ...

class SendKeysResponse(_message.Message):
    __slots__ = ("sent", "consumed")
    SENT_FIELD_NUMBER: _ClassVar[int]
    CONSUMED_FIELD_NUMBER: _ClassVar[int]
    sent: int
    consumed: _containers.RepeatedScalarFieldContainer[bool]
    def __init__(self, sent: _Optional[int] = ..., consumed: _Optional[_Iterable[bool]] = ...) -> None: ...

class SendPointerRequest(_message.Message):
    __slots__ = ("event",)
    EVENT_FIELD_NUMBER: _ClassVar[int]
    event: _types_pb2.PointerEvent
    def __init__(self, event: _Optional[_Union[_types_pb2.PointerEvent, _Mapping]] = ...) -> None: ...

class SendPointerResponse(_message.Message):
    __slots__ = ("consumed",)
    CONSUMED_FIELD_NUMBER: _ClassVar[int]
    consumed: bool
    def __init__(self, consumed: _Optional[bool] = ...) -> None: ...

class SetFocusRequest(_message.Message):
    __slots__ = ("name",)
    NAME_FIELD_NUMBER: _ClassVar[int]
    name: str
    def __init__(self, name: _Optional[str] = ...) -> None: ...

class SetFocusResponse(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class SwapMarkupRequest(_message.Message):
    __slots__ = ("source", "register")
    SOURCE_FIELD_NUMBER: _ClassVar[int]
    REGISTER_FIELD_NUMBER: _ClassVar[int]
    source: str
    register: _containers.RepeatedCompositeFieldContainer[_types_pb2.PropertyRegistration]
    def __init__(self, source: _Optional[str] = ..., register: _Optional[_Iterable[_Union[_types_pb2.PropertyRegistration, _Mapping]]] = ...) -> None: ...

class SwapMarkupResponse(_message.Message):
    __slots__ = ("named",)
    NAMED_FIELD_NUMBER: _ClassVar[int]
    named: _containers.RepeatedScalarFieldContainer[str]
    def __init__(self, named: _Optional[_Iterable[str]] = ...) -> None: ...

class RegisterPropertiesRequest(_message.Message):
    __slots__ = ("properties",)
    PROPERTIES_FIELD_NUMBER: _ClassVar[int]
    properties: _containers.RepeatedCompositeFieldContainer[_types_pb2.PropertyRegistration]
    def __init__(self, properties: _Optional[_Iterable[_Union[_types_pb2.PropertyRegistration, _Mapping]]] = ...) -> None: ...

class RegisterPropertiesResponse(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class GetDeclaredSchemaRequest(_message.Message):
    __slots__ = ("source",)
    SOURCE_FIELD_NUMBER: _ClassVar[int]
    source: str
    def __init__(self, source: _Optional[str] = ...) -> None: ...

class GetDeclaredSchemaResponse(_message.Message):
    __slots__ = ("schema",)
    SCHEMA_FIELD_NUMBER: _ClassVar[int]
    schema: _types_pb2.ControlSchema
    def __init__(self, schema: _Optional[_Union[_types_pb2.ControlSchema, _Mapping]] = ...) -> None: ...

class PatchMarkupRequest(_message.Message):
    __slots__ = ("name", "source")
    NAME_FIELD_NUMBER: _ClassVar[int]
    SOURCE_FIELD_NUMBER: _ClassVar[int]
    name: str
    source: str
    def __init__(self, name: _Optional[str] = ..., source: _Optional[str] = ...) -> None: ...

class PatchMarkupResponse(_message.Message):
    __slots__ = ("named",)
    NAMED_FIELD_NUMBER: _ClassVar[int]
    named: _containers.RepeatedScalarFieldContainer[str]
    def __init__(self, named: _Optional[_Iterable[str]] = ...) -> None: ...

class ListStylesRequest(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class ListStylesResponse(_message.Message):
    __slots__ = ("styles",)
    STYLES_FIELD_NUMBER: _ClassVar[int]
    styles: _containers.RepeatedCompositeFieldContainer[_types_pb2.StyleInfo]
    def __init__(self, styles: _Optional[_Iterable[_Union[_types_pb2.StyleInfo, _Mapping]]] = ...) -> None: ...

class ValidateMarkupRequest(_message.Message):
    __slots__ = ("source",)
    SOURCE_FIELD_NUMBER: _ClassVar[int]
    source: str
    def __init__(self, source: _Optional[str] = ...) -> None: ...

class ValidateMarkupResponse(_message.Message):
    __slots__ = ("valid", "error", "named")
    VALID_FIELD_NUMBER: _ClassVar[int]
    ERROR_FIELD_NUMBER: _ClassVar[int]
    NAMED_FIELD_NUMBER: _ClassVar[int]
    valid: bool
    error: str
    named: _containers.RepeatedScalarFieldContainer[str]
    def __init__(self, valid: _Optional[bool] = ..., error: _Optional[str] = ..., named: _Optional[_Iterable[str]] = ...) -> None: ...

from gooey.control.v1 import control_pb2 as _control_pb2
from gooey.control.v1 import types_pb2 as _types_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class AttachRequest(_message.Message):
    __slots__ = ("subscribe", "act")
    SUBSCRIBE_FIELD_NUMBER: _ClassVar[int]
    ACT_FIELD_NUMBER: _ClassVar[int]
    subscribe: Subscription
    act: Act
    def __init__(self, subscribe: _Optional[_Union[Subscription, _Mapping]] = ..., act: _Optional[_Union[Act, _Mapping]] = ...) -> None: ...

class Subscription(_message.Message):
    __slots__ = ("properties", "names", "frames", "input", "lifecycle")
    PROPERTIES_FIELD_NUMBER: _ClassVar[int]
    NAMES_FIELD_NUMBER: _ClassVar[int]
    FRAMES_FIELD_NUMBER: _ClassVar[int]
    INPUT_FIELD_NUMBER: _ClassVar[int]
    LIFECYCLE_FIELD_NUMBER: _ClassVar[int]
    properties: bool
    names: _containers.RepeatedScalarFieldContainer[str]
    frames: bool
    input: bool
    lifecycle: bool
    def __init__(self, properties: _Optional[bool] = ..., names: _Optional[_Iterable[str]] = ..., frames: _Optional[bool] = ..., input: _Optional[bool] = ..., lifecycle: _Optional[bool] = ...) -> None: ...

class Act(_message.Message):
    __slots__ = ("id", "set_property", "invoke_command", "send_keys", "send_pointer", "set_focus", "swap_markup", "register_properties")
    ID_FIELD_NUMBER: _ClassVar[int]
    SET_PROPERTY_FIELD_NUMBER: _ClassVar[int]
    INVOKE_COMMAND_FIELD_NUMBER: _ClassVar[int]
    SEND_KEYS_FIELD_NUMBER: _ClassVar[int]
    SEND_POINTER_FIELD_NUMBER: _ClassVar[int]
    SET_FOCUS_FIELD_NUMBER: _ClassVar[int]
    SWAP_MARKUP_FIELD_NUMBER: _ClassVar[int]
    REGISTER_PROPERTIES_FIELD_NUMBER: _ClassVar[int]
    id: int
    set_property: _control_pb2.SetPropertyRequest
    invoke_command: _control_pb2.InvokeCommandRequest
    send_keys: _control_pb2.SendKeysRequest
    send_pointer: _control_pb2.SendPointerRequest
    set_focus: _control_pb2.SetFocusRequest
    swap_markup: _control_pb2.SwapMarkupRequest
    register_properties: _control_pb2.RegisterPropertiesRequest
    def __init__(self, id: _Optional[int] = ..., set_property: _Optional[_Union[_control_pb2.SetPropertyRequest, _Mapping]] = ..., invoke_command: _Optional[_Union[_control_pb2.InvokeCommandRequest, _Mapping]] = ..., send_keys: _Optional[_Union[_control_pb2.SendKeysRequest, _Mapping]] = ..., send_pointer: _Optional[_Union[_control_pb2.SendPointerRequest, _Mapping]] = ..., set_focus: _Optional[_Union[_control_pb2.SetFocusRequest, _Mapping]] = ..., swap_markup: _Optional[_Union[_control_pb2.SwapMarkupRequest, _Mapping]] = ..., register_properties: _Optional[_Union[_control_pb2.RegisterPropertiesRequest, _Mapping]] = ...) -> None: ...

class AttachResponse(_message.Message):
    __slots__ = ("welcome", "result", "frame", "lifecycle", "input")
    WELCOME_FIELD_NUMBER: _ClassVar[int]
    RESULT_FIELD_NUMBER: _ClassVar[int]
    FRAME_FIELD_NUMBER: _ClassVar[int]
    LIFECYCLE_FIELD_NUMBER: _ClassVar[int]
    INPUT_FIELD_NUMBER: _ClassVar[int]
    welcome: Welcome
    result: ActResult
    frame: FrameDelta
    lifecycle: LifecycleEvent
    input: InputEcho
    def __init__(self, welcome: _Optional[_Union[Welcome, _Mapping]] = ..., result: _Optional[_Union[ActResult, _Mapping]] = ..., frame: _Optional[_Union[FrameDelta, _Mapping]] = ..., lifecycle: _Optional[_Union[LifecycleEvent, _Mapping]] = ..., input: _Optional[_Union[InputEcho, _Mapping]] = ...) -> None: ...

class Welcome(_message.Message):
    __slots__ = ("app_name", "app_version", "columns", "rows", "frame")
    APP_NAME_FIELD_NUMBER: _ClassVar[int]
    APP_VERSION_FIELD_NUMBER: _ClassVar[int]
    COLUMNS_FIELD_NUMBER: _ClassVar[int]
    ROWS_FIELD_NUMBER: _ClassVar[int]
    FRAME_FIELD_NUMBER: _ClassVar[int]
    app_name: str
    app_version: str
    columns: int
    rows: int
    frame: int
    def __init__(self, app_name: _Optional[str] = ..., app_version: _Optional[str] = ..., columns: _Optional[int] = ..., rows: _Optional[int] = ..., frame: _Optional[int] = ...) -> None: ...

class ActResult(_message.Message):
    __slots__ = ("id", "code", "message", "set_property", "invoke_command", "send_keys", "send_pointer", "set_focus", "swap_markup", "register_properties")
    ID_FIELD_NUMBER: _ClassVar[int]
    CODE_FIELD_NUMBER: _ClassVar[int]
    MESSAGE_FIELD_NUMBER: _ClassVar[int]
    SET_PROPERTY_FIELD_NUMBER: _ClassVar[int]
    INVOKE_COMMAND_FIELD_NUMBER: _ClassVar[int]
    SEND_KEYS_FIELD_NUMBER: _ClassVar[int]
    SEND_POINTER_FIELD_NUMBER: _ClassVar[int]
    SET_FOCUS_FIELD_NUMBER: _ClassVar[int]
    SWAP_MARKUP_FIELD_NUMBER: _ClassVar[int]
    REGISTER_PROPERTIES_FIELD_NUMBER: _ClassVar[int]
    id: int
    code: int
    message: str
    set_property: _control_pb2.SetPropertyResponse
    invoke_command: _control_pb2.InvokeCommandResponse
    send_keys: _control_pb2.SendKeysResponse
    send_pointer: _control_pb2.SendPointerResponse
    set_focus: _control_pb2.SetFocusResponse
    swap_markup: _control_pb2.SwapMarkupResponse
    register_properties: _control_pb2.RegisterPropertiesResponse
    def __init__(self, id: _Optional[int] = ..., code: _Optional[int] = ..., message: _Optional[str] = ..., set_property: _Optional[_Union[_control_pb2.SetPropertyResponse, _Mapping]] = ..., invoke_command: _Optional[_Union[_control_pb2.InvokeCommandResponse, _Mapping]] = ..., send_keys: _Optional[_Union[_control_pb2.SendKeysResponse, _Mapping]] = ..., send_pointer: _Optional[_Union[_control_pb2.SendPointerResponse, _Mapping]] = ..., set_focus: _Optional[_Union[_control_pb2.SetFocusResponse, _Mapping]] = ..., swap_markup: _Optional[_Union[_control_pb2.SwapMarkupResponse, _Mapping]] = ..., register_properties: _Optional[_Union[_control_pb2.RegisterPropertiesResponse, _Mapping]] = ...) -> None: ...

class FrameDelta(_message.Message):
    __slots__ = ("frame", "changes", "damage", "repainted")
    FRAME_FIELD_NUMBER: _ClassVar[int]
    CHANGES_FIELD_NUMBER: _ClassVar[int]
    DAMAGE_FIELD_NUMBER: _ClassVar[int]
    REPAINTED_FIELD_NUMBER: _ClassVar[int]
    frame: int
    changes: _containers.RepeatedCompositeFieldContainer[_types_pb2.PropertyChange]
    damage: _containers.RepeatedCompositeFieldContainer[_types_pb2.Rect]
    repainted: int
    def __init__(self, frame: _Optional[int] = ..., changes: _Optional[_Iterable[_Union[_types_pb2.PropertyChange, _Mapping]]] = ..., damage: _Optional[_Iterable[_Union[_types_pb2.Rect, _Mapping]]] = ..., repainted: _Optional[int] = ...) -> None: ...

class LifecycleEvent(_message.Message):
    __slots__ = ("resized", "swapped", "closing")
    RESIZED_FIELD_NUMBER: _ClassVar[int]
    SWAPPED_FIELD_NUMBER: _ClassVar[int]
    CLOSING_FIELD_NUMBER: _ClassVar[int]
    resized: Resized
    swapped: Swapped
    closing: Closing
    def __init__(self, resized: _Optional[_Union[Resized, _Mapping]] = ..., swapped: _Optional[_Union[Swapped, _Mapping]] = ..., closing: _Optional[_Union[Closing, _Mapping]] = ...) -> None: ...

class Resized(_message.Message):
    __slots__ = ("columns", "rows")
    COLUMNS_FIELD_NUMBER: _ClassVar[int]
    ROWS_FIELD_NUMBER: _ClassVar[int]
    columns: int
    rows: int
    def __init__(self, columns: _Optional[int] = ..., rows: _Optional[int] = ...) -> None: ...

class Swapped(_message.Message):
    __slots__ = ("named",)
    NAMED_FIELD_NUMBER: _ClassVar[int]
    named: _containers.RepeatedScalarFieldContainer[str]
    def __init__(self, named: _Optional[_Iterable[str]] = ...) -> None: ...

class Closing(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class InputEcho(_message.Message):
    __slots__ = ("event", "consumed")
    EVENT_FIELD_NUMBER: _ClassVar[int]
    CONSUMED_FIELD_NUMBER: _ClassVar[int]
    event: _types_pb2.InputEvent
    consumed: bool
    def __init__(self, event: _Optional[_Union[_types_pb2.InputEvent, _Mapping]] = ..., consumed: _Optional[bool] = ...) -> None: ...

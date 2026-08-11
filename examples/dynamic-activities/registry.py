"""The runtime activity registry — activities that did not exist at startup.

This is the part of the demo that is genuinely dynamic. `create` takes a
name and a blob of Python SOURCE, execs it, and keeps the resulting
callable in a dict. The Temporal worker registers exactly one activity —
a dynamic dispatcher (`@activity.defn(dynamic=True)`) that answers to any
activity type name — and that dispatcher looks the name up here. So a
tool call adds a new activity to a RUNNING worker: no redeploy, no
restart, no per-name Python function.

    DANGER: `create` runs arbitrary code with this process's full
    privileges. There is no sandbox. See the module docstring in
    worker.py and the README — this is remote code execution by design,
    and the whole reason the endpoint is loopback-only.

The name doubles as a Temporal activity type name AND as a segment of a
gooey binding path (`.Activity.<Name>.Result`), so it is restricted to
the intersection: an identifier.
"""

from __future__ import annotations

import inspect
import re
import time
from dataclasses import dataclass, field
from typing import Any, Callable
from xml.sax.saxutils import escape

# An identifier, because this string is used as a Go-template path
# segment in generated markup as well as an activity type name. Anything
# else would build markup that cannot parse.
NAME_RE = re.compile(r"^[A-Za-z][A-Za-z0-9]*$")

# The entry point the supplied source must define. Not a convention this
# code could discover safely — "the last function defined" would silently
# pick a helper — so it is a contract, stated once and checked at create
# time rather than at first invocation.
ENTRY_POINT = "run"


class ActivityError(Exception):
    """A tool-visible failure: bad name, bad source, unknown activity."""


@dataclass
class Activity:
    name: str
    source: str
    description: str
    fn: Callable[..., Any] = field(repr=False)
    created: float = field(default_factory=time.time)

    def summary(self) -> dict[str, Any]:
        return {
            "name": self.name,
            "description": self.description,
            "created": self.created,
            "result_property": result_property(self.name),
            "lines": len(self.source.splitlines()),
        }


def result_property(name: str) -> str:
    """The bindable name an activity's result lands in.

    Dotted, so every activity's result nests under one scope the app can
    show as a group — and so the whole family can be swept up by name
    when the activity is deleted.
    """
    return f"Activity.{name}.Result"


def compile_activity(name: str, source: str, description: str = "") -> Activity:
    """Exec `source` and pull ENTRY_POINT out of it.

    Compiles first so a syntax error is reported as a syntax error, with
    the filename naming the activity — a traceback that says
    "<activity Sparkle>" beats one that says "<string>".
    """
    if not NAME_RE.match(name):
        raise ActivityError(
            f"activity name {name!r} must be an identifier (letters and digits, "
            "starting with a letter): it is used both as a Temporal activity type "
            "and as a gooey binding path segment"
        )
    try:
        code = compile(source, f"<activity {name}>", "exec")
    except SyntaxError as exc:
        raise ActivityError(f"activity {name!r} does not compile: {exc}") from exc

    namespace: dict[str, Any] = {"__name__": f"activity_{name}"}
    try:
        exec(code, namespace)  # noqa: S102 - arbitrary code execution is the feature
    except Exception as exc:  # noqa: BLE001 - report anything the source did
        raise ActivityError(f"activity {name!r} failed while defining itself: {exc!r}") from exc

    fn = namespace.get(ENTRY_POINT)
    if not callable(fn):
        raise ActivityError(
            f"activity {name!r} defines no callable {ENTRY_POINT}(); the source must "
            f"define `def {ENTRY_POINT}(text):` (async is fine) — that is the entry point "
            "the dynamic dispatcher calls"
        )
    return Activity(name=name, source=source, description=description, fn=fn)


class Registry:
    """Name → Activity, plus the markup view the gooey app renders."""

    def __init__(self) -> None:
        self._items: dict[str, Activity] = {}

    def __contains__(self, name: object) -> bool:
        return name in self._items

    def names(self) -> list[str]:
        return sorted(self._items)

    def get(self, name: str) -> Activity:
        try:
            return self._items[name]
        except KeyError:
            raise ActivityError(
                f"no activity named {name!r}; have: {', '.join(self.names()) or '(none)'}"
            ) from None

    def create(self, name: str, source: str, description: str = "") -> Activity:
        if name in self._items:
            raise ActivityError(f"activity {name!r} already exists; delete it first")
        act = compile_activity(name, source, description)
        self._items[name] = act
        return act

    def delete(self, name: str) -> Activity:
        act = self.get(name)
        del self._items[name]
        return act

    def summaries(self) -> list[dict[str, Any]]:
        return [self._items[n].summary() for n in self.names()]

    async def invoke(self, name: str, args: list[str]) -> str:
        """Call an activity's entry point, awaiting it if it is a coroutine."""
        act = self.get(name)
        result = act.fn(*args)
        if inspect.isawaitable(result):
            result = await result
        return result if isinstance(result, str) else repr(result)


def reconcile_selection(live: str, names: list[str], select: str | None = None) -> str:
    """What the app's Selected property should be, given what it IS.

    The app owns the selection between tool calls — ctrl+n cycles it with
    no tool call involved — so this worker must not simply overwrite it.
    The rule, in order:

    * an explicit `select` wins (create knows what it just made);
    * a live selection that still names something is left alone;
    * a dangling selection falls back to the first name;
    * an empty registry means an empty selection.

    Pure, so the interesting half of the sync story is testable without
    a running app.
    """
    if select is not None:
        return select
    if live in names:
        return live
    return names[0] if names else ""


def activity_list_markup(registry: Registry) -> str:
    """The <VStack Name="ActivityList"> subtree, as a patch_markup fragment.

    One row per activity: a Button bound straight to
    {{temporal:Activity `Name` .Input | into .Activity.Name.Result}} and a
    Text showing that result. The fragment declares the temporal namespace
    itself because an xmlns table is per-DOCUMENT — a patched fragment
    cannot borrow the prefix the page happened to declare.

    This is where property registration becomes load-bearing rather than
    bookkeeping: if Activity.<Name>.Result were not registered first, the
    fragment would not build and PatchMarkup would refuse it, leaving the
    running page untouched.
    """
    rows: list[str] = []
    for name in registry.names():
        act = registry.get(name)
        prop = result_property(name)
        label = escape(f" {name} ")
        desc = escape(act.description or "(no description)")
        rows.append(
            f'    <HStack Gap="1">\n'
            f'      <Button Content="{label}" '
            f'Click="{{{{temporal:Activity `{name}` .Input | into .{prop}}}}}"/>\n'
            f'      <Text Style="dim">{desc}</Text>\n'
            f"    </HStack>\n"
            f'    <Text Margin="2,0,0,0">{{{{.{prop}}}}}</Text>'
        )
    if not rows:
        rows.append('    <Text Style="dim">nothing yet — call create_activity</Text>')
    body = "\n".join(rows)
    return (
        '<Gooey xmlns="wonderforge.io/gooey/2026"\n'
        '       xmlns:temporal="gooey.dev/handlers/temporal">\n'
        '  <VStack Name="ActivityList" Gap="0">\n'
        f"{body}\n"
        "  </VStack>\n"
        "</Gooey>\n"
    )

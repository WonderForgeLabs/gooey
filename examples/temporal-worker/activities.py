import os
from collections.abc import Sequence

from claude_agent_sdk import AssistantMessage, ClaudeAgentOptions, ResultMessage, TextBlock, query
from mcp import ClientSession
from mcp.client.streamable_http import streamable_http_client
from temporalio import activity
from temporalio.common import RawValue

# Fallback used only when the target app can't be reached at all (discovery
# fails). Without a discovered bindable surface, swap_markup rejects the whole
# page if a {{ }} expression names something that isn't already registered in
# the app's viewmodel — so the safe fallback sticks to literal-only elements,
# which are valid against *any* app's markup context.
FALLBACK_SYSTEM_PROMPT = """You write UI markup for gooey, a small XAML-like markup language for terminal apps.
Output ONLY the markup, nothing else — no prose, no code fences, no explanation.

Rules:
- Root element is always <Gooey xmlns="wonderforge.io/gooey/2026"> with exactly one child.
- Available elements: Border, Grid, VStack, HStack, Text, Button. Nothing else.
- Border takes Title="..." and Style="panel"|"accent"|"dim", and wraps exactly one child.
- Grid takes Rows="Auto,Auto,*" (comma-separated row sizes); children set Grid.Row="N".
- VStack and HStack take Gap="N" (an integer cell count) and any number of children.
- Text holds literal text content and takes Style="panel"|"accent"|"dim".
- Button takes Content="..." only — never a Click attribute.
- Never use {{ }} binding expressions, and never use Checkbox, TextBox, ProgressBar,
  Segmented, Toggle, Spinner, StatusBar, ButtonBar, Timer, or KeyBinding — those bind to
  application state this activity has no access to, and including one will make the
  whole page fail to load.
- Keep it visually clear: a Border with a title, and a Grid or stack of Text/Button
  content that actually summarizes the given topic — not a generic placeholder.
"""

# Used once the app's real bindable surface has been discovered via list_values.
# This unlocks genuinely interactive elements (Toggle, TextBox, ButtonBar, ...)
# because every {{ }} expression the model writes is checked against a name
# list it was actually given, not invented.
INTERACTIVE_SYSTEM_PROMPT = """You write UI markup for gooey, a small XAML-like markup language for terminal apps.
Output ONLY the markup, nothing else — no prose, no code fences, no explanation.

You have just been given the EXACT bindable surface of the running app — every
property and command already registered in its viewmodel, as JSON from the
app's own `list_values` tool: {"named": [...]} lists elements with a Name=
already assigned; {"values": [...]} lists each property (kind="property",
with a "type" of "string" or "boolean", and a current "value") and each
command (kind="command").

Rules:
- Root element is always <Gooey xmlns="wonderforge.io/gooey/2026"> with exactly one child.
- Elements: Border, Grid, VStack, HStack, Text, Button, Checkbox, TextBox,
  Toggle, Spinner, ButtonBar, StatusBar, KeyBinding.
- Border: Title="...", Style="panel"|"accent"|"dim"; wraps exactly one child.
- Grid: Rows="Auto,Auto,*" (comma-separated); children set Grid.Row="N".
- VStack / HStack: Gap="N" (integer cells); any number of children.
- Text: literal content, Style="panel"|"accent"|"dim", or {{.Name}} for a string property.
- Button: Content="...", optional Click="{{.CommandName}}".
- Checkbox / Toggle: REQUIRED Checked="{{.BoolPropertyName}}", optional Label="...".
- TextBox: REQUIRED Text="{{.StringPropertyName}}", optional Prompt="...", AccentStyle="...".
- Spinner: Frames="braille"|"arc"|"dot"|"line", Interval="90ms", Label="...", optional Enabled="{{.BoolPropertyName}}".
- ButtonBar: Gap="N", Uniform="true"|"false", Separator="..."; child Buttons each with Content= and optional Click=.
- StatusBar: Left="{{.StringPropertyName}}", Center="literal or {{.Name}}"; optional <StatusBar.Right> child.
- KeyBinding: Gesture="q"|"ctrl+c"|..., Command="{{.CommandName}}".
- EVERY {{.Name}} you write MUST be a name from the "named"/"values" lists you were
  given, used at its correct kind (command → Click/Command; boolean property →
  Checked/Enabled; string property → Text/Left/Center) — inventing a name, or
  using a property where a command belongs, makes the whole page fail to load.
- Wire real interactivity: at least one Button/ButtonBar member should Click a
  real command, and if a boolean property exists, bind it to a Toggle/Checkbox
  so the panel is genuinely operable, not just decorative.
- Keep it visually clear: a Border with a title, laid out with Grid/VStack/HStack,
  that actually reflects the given topic — not a generic placeholder.
"""


def _extract_text(result) -> str:
    return "".join(block.text for block in result.content if getattr(block, "type", None) == "text")


async def _generate_markup(topic: str, activity_kind: str, bindable_surface_json: str | None) -> str:
    # Goes through the Claude Agent SDK (the Claude Code harness as a library)
    # rather than calling the Messages API directly. A CLAUDE_CODE_OAUTH_TOKEN
    # (from `claude setup-token`) is scoped for this path — driving it straight
    # at client.messages.create() authenticates fine but trips a much tighter
    # rate limit than the CLI's own traffic pattern gets.
    if bindable_surface_json is not None:
        system_prompt = INTERACTIVE_SYSTEM_PROMPT
        prompt = (
            f"Activity type: {activity_kind}\n"
            f"Bindable surface (list_values output):\n{bindable_surface_json}\n\n"
            f"Build a UI panel about: {topic}"
        )
    else:
        system_prompt = FALLBACK_SYSTEM_PROMPT
        prompt = f"Activity type: {activity_kind}\nBuild a UI panel about: {topic}"

    options = ClaudeAgentOptions(
        system_prompt=system_prompt,
        model="claude-opus-5",
        allowed_tools=[],
        max_turns=1,
    )
    text_parts: list[str] = []
    async for message in query(prompt=prompt, options=options):
        if isinstance(message, AssistantMessage):
            text_parts.extend(block.text for block in message.content if isinstance(block, TextBlock))
        elif isinstance(message, ResultMessage) and message.is_error:
            raise RuntimeError(f"claude agent sdk query failed for topic {topic!r}: {message.result}")
    return "".join(text_parts).strip()


async def _discover_generate_and_push(topic: str, activity_kind: str) -> str:
    url = os.environ.get("GOOEY_MCP_URL", "http://127.0.0.1:7777/mcp")
    try:
        async with streamable_http_client(url) as (read, write, _get_session_id):
            async with ClientSession(read, write) as session:
                await session.initialize()
                surface = await session.call_tool("list_values", arguments={})
                bindable_surface_json = _extract_text(surface)
                markup = await _generate_markup(topic, activity_kind, bindable_surface_json)
                swap_result = await session.call_tool("swap_markup", arguments={"source": markup})
                if swap_result.isError:
                    activity.logger.warning("swap_markup rejected the generated markup: %s", swap_result.content)
                return markup
    except Exception as exc:
        # Best-effort: no gooey app running, it's on a different port, or the
        # generated markup didn't validate. Fall back to a markup generated
        # without any discovered bindings, so the activity still returns
        # *something* for whoever called it — the push is a bonus, not the
        # contract this activity guarantees.
        activity.logger.warning("could not use live bindable surface at %s: %s", url, exc)
        return await _generate_markup(topic, activity_kind, bindable_surface_json=None)


@activity.defn(dynamic=True)
async def generate_ui(args: Sequence[RawValue]) -> str:
    """Any activity type name routes here — the name itself is the request.

    A caller invokes this by executing an activity whose name it invents on
    the spot (`GenerateUI`, `BuildDashboard`, `SummarizeThread`, ...); there is
    no per-name Python function to add. The single argument is the topic —
    "what we're talking about" — and the activity type name is passed through
    to the model as a hint about what *kind* of panel to build. Before
    generating, the activity discovers the target app's real bindable surface
    via its own `list_values` MCP tool, so the generated markup can wire real
    Toggle/Checkbox/Button interactivity to real state instead of only ever
    producing decorative, bindingless panels.
    """
    topic = activity.payload_converter().from_payload(args[0].payload, str)
    kind = activity.info().activity_type
    return await _discover_generate_and_push(topic, kind)

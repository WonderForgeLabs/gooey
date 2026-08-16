#!/usr/bin/env python3
"""Drive The Rectangle over a running gooey app's MCP endpoint.

The talk has no presentation program. It is a `patch_markup` into whatever
gooey app is listening — in practice `apps/wysiwyg` — plus one `set_value`
per field to advance. This script is the remote control, and it is the only
executable in the directory.

    ./present.py setup      register the Deck.* properties, install deck.gooey
    ./present.py 1          show slide 1
    ./present.py next       advance (state in .current, next to this file)
    ./present.py prev
    ./present.py keys       presenter mode: pgdn/pgup (or arrows/space) in
                            THIS terminal drive the slides. q quits.
    ./present.py rehearse   the whole talk, unattended: each slide, its
                            narration, then its HOLD of dead air
    ./present.py teardown   put the editor's own designer pane back

Slide data comes from NARRATION.md's ```slide blocks, so the script and the
script are the same file. Nothing is duplicated here.

The endpoint is discovered by probing loopback listeners for a gooey MCP
server; override with GOOEY_MCP=http://127.0.0.1:PORT/mcp when that guess is
wrong (two gooey apps running, say).
"""

import json
import os
import pathlib
import re
import subprocess
import sys
import urllib.error
import urllib.request

HERE = pathlib.Path(__file__).resolve().parent
NARRATION = HERE / "NARRATION.md"
DECK = HERE / "deck.gooey"
STATE = HERE / ".current"

# Bg and Card are the only non-string, non-int fields, and they never change
# between slides — they are the scrim and the spotlight, set once at setup.
COLORS = {"Deck.Bg": "#0e1018", "Deck.Card": "#1b2233"}
INTS = {"Deck.Pct"}


# ---- the endpoint -----------------------------------------------------------

def rpc(url, method, params, timeout=20):
    """One JSON-RPC round, and the ONLY place a transport failure is caught.

    The guard lives here rather than at each call site, and that is the
    whole design of it: there are a dozen call sites across setup, show,
    tabs, teardown and the keys loop, and the ones that would have been
    left unguarded are exactly the ones nobody thinks about — the goto
    inside the key handler, the teardown on the way out. A tool driven
    live in front of a room must not answer a dropped connection with a
    stack trace, and "wrap the call sites" is a rule that holds until
    someone adds the thirteenth.
    """
    body = json.dumps({"jsonrpc": "2.0", "id": 1, "method": method,
                       "params": params}).encode()
    req = urllib.request.Request(url, data=body, headers={
        "Content-Type": "application/json",
        "Accept": "application/json, text/event-stream",
    })
    try:
        with urllib.request.urlopen(req, timeout=timeout) as r:
            return json.loads(r.read().decode())
    except (urllib.error.URLError, OSError, TimeoutError) as e:
        sys.exit(f"  ! {method} to {url} failed: {e}\n"
                 f"    the app is not answering — is it still running?")
    except json.JSONDecodeError as e:
        sys.exit(f"  ! {method} to {url} answered something that is not JSON: {e}")


def call(url, tool, args):
    d = rpc(url, "tools/call", {"name": tool, "arguments": args})
    if "error" in d:
        sys.exit(f"{tool}: {d['error']}")
    # A tool that fails inside the app still returns a normal result whose
    # text carries the load error, so surface anything that smells like one.
    for c in d.get("result", {}).get("content", []):
        t = c.get("text", "")
        if t.startswith("✗") or "error" in t[:80].lower():
            print(f"  ! {tool}: {t[:200]}", file=sys.stderr)
    return d


def discover():
    """Find a loopback port answering as a gooey MCP server."""
    if env := os.environ.get("GOOEY_MCP"):
        return env
    try:
        out = subprocess.run(["ss", "-ltn"], capture_output=True, text=True,
                             timeout=10).stdout
    except (OSError, subprocess.SubprocessError):
        sys.exit("cannot probe for a gooey app: set GOOEY_MCP=http://host:port/mcp")
    ports = sorted({int(m) for m in re.findall(r"127\.0\.0\.1:(\d+)\s", out)})
    for p in ports:
        url = f"http://127.0.0.1:{p}/mcp"
        try:
            d = rpc(url, "initialize", {
                "protocolVersion": "2025-06-18", "capabilities": {},
                "clientInfo": {"name": "the-rectangle", "version": "1"}}, timeout=2)
        except Exception:
            # Deliberately broad: this is a PROBE, and loopback holds things
            # that are not HTTP at all. A Temporal frontend answers a JSON POST
            # with bytes that make http.client raise BadStatusLine, which is an
            # HTTPException and not an OSError — so an exception list here is a
            # list of the services you happened to have running that day.
            continue
        name = d.get("result", {}).get("serverInfo", {}).get("name", "")
        if name.startswith("gooey"):
            print(f"  → {name} at {url}")
            return url
    sys.exit("no gooey MCP server on loopback: set GOOEY_MCP=http://host:port/mcp")


# ---- the talk ---------------------------------------------------------------

def fenced(kind):
    md = NARRATION.read_text()
    return [m.group(1) for m in
            re.finditer(rf"```{kind}\n(.*?)```", md, re.S)]


def slides():
    return [json.loads(b) for b in fenced("slide")]


def ghost():
    g = fenced("ghost")
    return g[0].rstrip("\n") if g else ""


def field_type(name):
    if name in COLORS:
        return "color"
    return "int" if name in INTS else "string"


def register(url, props):
    """Register properties one at a time, tolerating the already-there ones.

    register_properties refuses a name that already exists and is
    all-or-nothing, so a batch containing one known name fails as a batch
    and registers none of the rest. One at a time, letting the refusals
    fall through, is what makes every command here safe to re-run mid-talk.

    This is a function because it was a loop inside setup() and a DIFFERENT,
    guardless loop inside setup_tabs() — which registers three of the same
    names two screens away. Running `setup` and then `tabs`, the ordinary
    thing to do while deciding which deck to use, aborted the second on
    Deck.Ghost. The same rule written twice in different words is the
    signature of a missing function; here the second copy did not have the
    rule at all.

    Returns how many were actually new.
    """
    made = 0
    for p in props:
        d = rpc(url, "tools/call", {"name": "register_properties",
                                    "arguments": {"properties": [p]}})
        if "error" not in d:
            made += 1
    return made


def setup(url):
    decks = slides()
    names = {k for s in decks for k in s} | {"Deck.Ghost"} | set(COLORS)
    props = [{"name": n, "type": field_type(n)} for n in sorted(names)]

    print(f"  registered {register(url, props)} new properties "
          f"({len(props)} total)")

    for name, value in COLORS.items():
        call(url, "set_value", {"name": name, "value": value})
    call(url, "set_value", {"name": "Deck.Ghost", "value": ghost()})

    src = DECK.read_text()
    d = call(url, "validate_markup", {"source": src})
    txt = "".join(c.get("text", "") for c in d["result"]["content"])
    if '"valid": true' not in txt and '"valid":true' not in txt:
        sys.exit(f"deck.gooey does not build against this app:\n{txt}")

    call(url, "patch_markup", {"name": "EditorArea", "source": src})
    print("  patched EditorArea")


def has_value(url, name):
    """Is `name` a registered property right now?

    list_values answers with JSON, so the name arrives as a "name" field and
    never at the start of a line — an anchored regex here quietly answered
    False for a property that plainly existed.
    """
    d = call(url, "list_values", {})
    text = "".join(c.get("text", "")
                   for c in d.get("result", {}).get("content", []))
    try:
        vals = json.loads(text).get("values", [])
    except json.JSONDecodeError:
        return f'"{name}"' in text
    return any(v.get("name") == name for v in vals)


def show(url, n):
    decks = slides()
    if not 1 <= n <= len(decks):
        sys.exit(f"slide {n} out of range (1..{len(decks)})")
    for name, value in decks[n - 1].items():
        call(url, "set_value", {"name": name, "value": value})
    # Both decks answer to the same commands. The property deck reads the
    # Deck.* fields above; the tab deck carries its slides literally and
    # selects by index, so a `present.py 5` against it moved nothing at all
    # and said "slide 5/24" anyway. Only set Sel when it exists — setting an
    # unregistered name is an in-app error, not a no-op.
    if has_value(url, "Deck.Sel"):
        call(url, "set_value", {"name": "Deck.Sel", "value": n - 1})
    STATE.write_text(str(n))
    print(f"  slide {n}/{len(decks)}")


def holds():
    """The HOLD of each beat, in seconds, in document order.

    HOLD is dead air after the words, where the thing being described actually
    happens on screen — so in a rehearsal it is not padding, it is the beat.
    Beat 8's HOLD is ∞ (the talk ends on it), which lands here as 0: the run
    stops rather than hanging.
    """
    out = []
    for m in re.finditer(r"\*\*HOLD:\*\*\s*([0-9]+):([0-9]+)|\*\*HOLD:\*\*\s*∞",
                         NARRATION.read_text()):
        out.append(0 if m.group(1) is None else int(m.group(1)) * 60 + int(m.group(2)))
    return out


def spoken():
    """Each ```speak block flattened to one line — what say.sh feeds piper."""
    return [" ".join(b.split()) for b in fenced("speak")]


# The **VOICE:** marker maps to a piper voice, and say.sh names the file
# after the SHORT name. Kept in step with say.sh's `case` by hand, which is
# two lines in two files and the alternative was parsing shell.
VOICE_TAG = {"claude": "lessac", "narrator": "ryan"}


def voices():
    """The voice tag of each beat, in document order."""
    return [VOICE_TAG.get(m, "ryan")
            for m in re.findall(r"\*\*VOICE:\*\*\s*(\w+)", NARRATION.read_text())]


def take_for(i, tags=None):
    """The audio file for beat `i`, chosen by its VOICE marker.

    This used to be `sorted(glob(f"{i:02d}-*.wav"))[0]`, and the sort is
    the bug: `NN-lessac.wav` precedes `NN-ryan.wav`, so a beat with both
    takes silently played lessac no matter which voice the script assigns.
    Both takes existing is not hypothetical — the README suggests rendering
    a second voice to spot-check beats 4 and 7, and the talk hands over
    from the presenter to the agent at beat 12, so which voice you hear IS
    the content. Getting it wrong is the failure mode the **VOICE:**
    receipts exist to prevent, arriving through the back door.

    Falls back to whatever take exists, loudly. A missing canonical take is
    a real answer; substituting the other voice without saying so is not.
    """
    tags = tags or voices()
    want = tags[i - 1] if i <= len(tags) else "ryan"
    exact = HERE / "audio" / f"{i:02d}-{want}.wav"
    if exact.exists():
        return exact
    others = sorted((HERE / "audio").glob(f"{i:02d}-*.wav"))
    if not others:
        return None
    print(f"  ! beat {i} has no {want} take — falling back to {others[0].name}",
          file=sys.stderr)
    return others[0]


def stale_takes():
    """Beats whose rendered audio says something the script no longer says.

    Rehearsing against a stale take is the worst kind of wrong, because it
    sounds completely fine: you hear a confident reading of a sentence you
    deleted, time the beat against it, and find out in the room.

    Compared by TEXT, against the .txt receipt say.sh writes next to each
    wav — not by mtime. mtime was the first attempt and it was useless:
    re-measuring the DURATION markers rewrites NARRATION.md and marked all
    24 takes stale over a change that touched no speech at all. A check
    that fires on every routine edit is a check you learn to skip.

    A take with no receipt counts as stale: it was rendered before this
    existed, so what it says is unknown, and unknown is not fine.
    """
    now, tags, out = spoken(), voices(), []
    for i in range(1, len(now) + 1):
        take = take_for(i, tags)
        if take is None:
            continue          # missing is a different problem; rehearse says so
        receipt = take.with_suffix(".txt")
        if not receipt.exists() or receipt.read_text().strip() != now[i - 1]:
            out.append(i)
    return out


def rehearse(url):
    """Play the talk to an empty room, at its real pace."""
    import time
    n = len(slides())
    hs = holds()
    if len(hs) != n:
        print(f"  ! {len(hs)} HOLD markers for {n} slides — using 4s where absent",
              file=sys.stderr)
        hs = (hs + [4] * n)[:n]
    if stale := stale_takes():
        sys.exit(f"  ! audio older than NARRATION.md for beat(s) "
                 f"{', '.join(map(str, stale))} — run ./say.sh all first.\n"
                 f"    (rehearsing against a take of words you have since "
                 f"changed sounds fine and times wrong.)")
    tags = voices()
    for i in range(1, n + 1):
        show(url, i)
        # By the beat's OWN voice. The talk hands over from the presenter to
        # the agent at beat 12, so which voice you hear is the content and
        # not a detail of which file happened to sort first.
        if take := take_for(i, tags):
            # Blocking: the next slide must not arrive over the last sentence.
            subprocess.run(["paplay", str(take)], check=False)
        else:
            print(f"  (no audio for beat {i} — run ./say.sh all)")
        if hs[i - 1]:
            print(f"  … hold {hs[i - 1]}s")
            time.sleep(hs[i - 1])
    print("  end of talk")


# Presenter keys, and why they are read HERE rather than by the app.
#
# The obvious implementation is <KeyBinding Gesture="pagedown" Command="..."/>
# in deck.gooey. gooey decodes both keys (input/decode.go:213) and the gesture
# parses, so the markup half is fine — but Command needs a gooey.Action out of
# the host's binding context, and the control plane cannot create one.
# register_properties is explicit that commands are not registerable: behavior
# needs code, not storage. The editor's own commands are Add, Delete, NextEl,
# PrevEl, ToggleMode, Quit and friends; none of them advance a slide, and
# hijacking one would mutate the document on screen.
#
# So the keys are read in the presenter's terminal instead. That terminal needs
# focus for the keys to land, which is the real cost — worth knowing before you
# stand up. A presenter remote sends exactly these two sequences, so it works
# with one as long as this window has focus.
KEYS = {
    "\x1b[6~": +1, "\x1b[5~": -1,     # page down / page up — presenter remotes
    "\x1b[C": +1, "\x1b[D": -1,       # right / left
    "\x1b[B": +1, "\x1b[A": -1,       # down / up
    " ": +1, "n": +1, "p": -1, "b": -1,
}


def keys(url):
    """Drive the deck from this terminal's arrow / page keys.

    Reads with os.read on the raw fd, NOT sys.stdin.read. That is the whole
    difference between this working and not: pagedown arrives as the four
    bytes \x1b[6~ in one burst, and sys.stdin is a buffered text stream, so
    reading one character pulls all four into Python's buffer and leaves the
    OS-level fd empty. Any select() for "is there more?" then says no, the
    sequence is truncated to a bare esc, and every arrow and page key is
    silently ignored while ordinary letters still work — which is exactly how
    the first version of this failed.
    """
    import termios
    import tty

    n = len(slides())
    cur = int(STATE.read_text()) if STATE.exists() else 1
    fd = sys.stdin.fileno()
    if not os.isatty(fd):
        sys.exit("./present.py keys needs a terminal (run it in your own window)")
    old = termios.tcgetattr(fd)
    # Probed once, not per keypress: which deck is installed cannot change
    # while this loop owns the keyboard, and an extra RPC per press is latency
    # the presenter feels.
    sel = has_value(url, "Deck.Sel")
    print(f"  presenter: pgdn/pgup, arrows, space · q quits · on slide {cur}/{n}")

    def goto(i):
        for name, value in slides()[i - 1].items():
            call(url, "set_value", {"name": name, "value": value})
        if sel:
            call(url, "set_value", {"name": "Deck.Sel", "value": i - 1})
        STATE.write_text(str(i))

    try:
        tty.setraw(fd)
        buf = ""
        while True:
            data = os.read(fd, 32)
            if not data:
                break
            buf += data.decode(errors="ignore")
            quit_ = False
            while buf:
                if buf[0] in ("q", "\x03"):
                    quit_ = True
                    break
                # Longest match first, so \x1b[6~ is never mistaken for a
                # shorter prefix that happens to be bound.
                hit = max((k for k in KEYS if buf.startswith(k)),
                          key=len, default=None)
                if hit is None:
                    if any(k.startswith(buf) for k in KEYS):
                        break          # a partial sequence — wait for the rest
                    buf = buf[1:]      # nothing can start with this: drop it
                    continue
                buf = buf[len(hit):]
                nxt = min(max(cur + KEYS[hit], 1), n)
                if nxt != cur:
                    cur = nxt
                    goto(cur)
                # \r and no newline: the terminal is raw, nothing translates.
                sys.stdout.write(f"\r  slide {cur}/{n}    ")
                sys.stdout.flush()
            if quit_:
                break
    finally:
        termios.tcsetattr(fd, termios.TCSADRAIN, old)
    print("\n  presenter closed")


def esc(s):
    return s.replace("&", "&amp;").replace("<", "&lt;").replace(">", "&gt;")


def tabdeck():
    """The deck as a <Tabs>, so ctrl+pgdn/pgup work with no host code.

    Tabs.HandleKey consumes ctrl+pageup / ctrl+pagedown from anywhere in its
    subtree (components/tabs.go:265) — they bubble up and mean nothing else, so
    the strip does not even need focus for them. That is a real KeyBinding-free
    answer to "advance the slide from the keyboard", and it needs nothing from
    the host: no gooey.Action, no registered command, no Go.

    The catch, and it is the reason the property-bound deck still exists: the
    control plane registers SOURCE properties, never computeds. A tab index
    cannot recompute Deck.Title, so an eight-tab deck has to carry its eight
    slides LITERALLY. Content moves out of the property graph and into the
    markup — which is why this is generated from NARRATION.md rather than
    hand-written, so there is still exactly one source for the words.

    Deck.Ghost stays a binding: the scrim is the editor's own markup and is
    full of {{...}}, which a Text body would try to resolve.
    """
    tabs = []
    for i, s in enumerate(slides(), 1):
        tabs.append(f"""    <Tab Header="{i}">
      <Canvas>
        <Text Canvas.Left="0" Canvas.Top="1" Style="warn">{esc(s['Deck.Spot'])}</Text>
        <Border Canvas.Left="14" Canvas.Top="4" Width="110" Height="30"
                Background="{{{{.Deck.Card}}}}" Style="sel" Title="{esc(s['Deck.Kicker'])}">
          <VStack Gap="1">
            <Text Bold="true" Style="title">{esc(s['Deck.Title'])}</Text>
            <Text>{esc(s['Deck.Body'])}</Text>
          </VStack>
        </Border>
        <Text Canvas.Left="16" Canvas.Top="35" Style="warn">{esc(s['Deck.Arrow'])}</Text>
        <Text Canvas.Left="16" Canvas.Top="37" Style="ok">{esc(s['Deck.Blurb'])}</Text>
        <Text Canvas.Left="16" Canvas.Top="41" Style="dim">{esc(s['Deck.Meme'])}</Text>
        <Text Canvas.Left="14" Canvas.Top="45" Style="dim">{esc(s['Deck.Pos'])}</Text>
        <ProgressBar Canvas.Left="24" Canvas.Top="45" Width="60" Value="{{{{.Deck.Pct{i}}}}}"/>
        <Text Canvas.Left="88" Canvas.Top="45" Style="ok">{esc(s['Deck.Hint'])}</Text>
        <Text Canvas.Left="88" Canvas.Top="46" Style="warn">{esc(s['Deck.Line1'])}</Text>
      </Canvas>
    </Tab>""")
    body = "\n".join(tabs)
    return f"""<Gooey>
  <Canvas Name="EditorArea" Background="{{{{.Deck.Bg}}}}">
    <Text Canvas.Left="0" Canvas.Top="0" Style="dim">{{{{.Deck.Ghost}}}}</Text>
    <Tabs Name="Deck" Selected="{{{{.Deck.Sel}}}}">
{body}
    </Tabs>
  </Canvas>
</Gooey>
"""


def setup_tabs(url):
    """Install the keyboard-drivable deck and put focus where keys bubble."""
    # Slides that SHOW markup contain bindings as prose — beat 16's example has
    # a literal {{.Count}} in it. In the property deck that is harmless: the
    # text is a property VALUE and nothing re-parses it. Here it is markup, and
    # scanBindings finds bindings anywhere in a Text body (markup.go:984), so
    # the build fails with `"Count" not found in context`. There is no escape
    # syntax.
    #
    # So each such name is registered as a string whose value is its own
    # spelling: {{.Count}} resolves to the literal text "{{.Count}}", which is
    # exactly what the slide meant to show. The binding is satisfied and the
    # rendering is honest — the alternative was to mangle the example.
    quoted = set()
    for s in slides():
        for field in ("Deck.Title", "Deck.Body", "Deck.Spot", "Deck.Blurb",
                      "Deck.Meme", "Deck.Kicker", "Deck.Hint"):
            quoted.update(re.findall(r"\{\{\.([A-Za-z_][\w.]*)\}\}", s[field]))
    props = [{"name": q, "type": "string", "value": "{{." + q + "}}"}
             for q in sorted(quoted)]
    if quoted:
        print(f"  self-quoting {len(quoted)} binding(s) shown as prose: "
              f"{', '.join(sorted(quoted))}")
    props += [{"name": "Deck.Sel", "type": "int", "value": 0},
             {"name": "Deck.Ghost", "type": "string"},
             {"name": "Deck.Bg", "type": "color"},
             {"name": "Deck.Card", "type": "color"}]
    # ProgressBar.Value is BindsBinding and Required — a literal is a load
    # error — so each tab gets its own int rather than the bar becoming drawn
    # text. Keeping it a real component is the point: it is the cheapest proof
    # on screen that a slide is a live tree and not a rendered picture.
    for i, s in enumerate(slides(), 1):
        props.append({"name": f"Deck.Pct{i}", "type": "int", "value": s["Deck.Pct"]})
    register(url, props)
    for i, s in enumerate(slides(), 1):
        call(url, "set_value", {"name": f"Deck.Pct{i}", "value": s["Deck.Pct"]})
    for name, value in COLORS.items():
        call(url, "set_value", {"name": name, "value": value})
    call(url, "set_value", {"name": "Deck.Ghost", "value": ghost()})

    src = tabdeck()
    d = call(url, "validate_markup", {"source": src})
    txt = "".join(c.get("text", "") for c in d["result"]["content"])
    if '"valid": true' not in txt and '"valid":true' not in txt:
        sys.exit(f"generated tab deck does not build:\n{txt[:800]}")
    call(url, "patch_markup", {"name": "EditorArea", "source": src})
    # ctrl+pgdn bubbles UP from the focused component, so the Tabs has to be on
    # that path. Focusing it directly is the shortest way to guarantee it —
    # otherwise focus sits on the ActivityBar, which is EditorArea's sibling,
    # and the keys never reach here.
    call(url, "focus", {"name": "Deck"})
    print("  tab deck installed, focused — ctrl+pgdn / ctrl+pgup advance")


def teardown(url):
    call(url, "patch_markup", {"name": "EditorArea", "source":
         '<Gooey>\n  <Panel Name="EditorArea" Title="DESIGNER" Pad="1">\n'
         "    <Preview Name=\"Island\"/>\n  </Panel>\n</Gooey>\n"})
    print("  designer pane restored")


def main():
    if len(sys.argv) < 2:
        sys.exit(__doc__)
    cmd = sys.argv[1]
    url = discover()
    cur = int(STATE.read_text()) if STATE.exists() else 1
    if cmd == "setup":
        setup(url)
        show(url, 1)
    elif cmd == "teardown":
        teardown(url)
    elif cmd == "rehearse":
        rehearse(url)
    elif cmd == "keys":
        keys(url)
    elif cmd == "tabs":
        setup_tabs(url)
    elif cmd == "next":
        show(url, min(cur + 1, len(slides())))
    elif cmd == "prev":
        show(url, max(cur - 1, 1))
    elif cmd.isdigit():
        show(url, int(cmd))
    else:
        sys.exit(__doc__)


if __name__ == "__main__":
    main()

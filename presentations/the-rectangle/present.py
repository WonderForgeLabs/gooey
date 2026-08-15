#!/usr/bin/env python3
"""Drive The Rectangle over a running gooey app's MCP endpoint.

The talk has no presentation program. It is a `patch_markup` into whatever
gooey app is listening — in practice `examples/wysiwyg` — plus one `set_value`
per field to advance. This script is the remote control, and it is the only
executable in the directory.

    ./present.py setup      register the Deck.* properties, install deck.gooey
    ./present.py 1          show slide 1
    ./present.py next       advance (state in .current, next to this file)
    ./present.py prev
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
    body = json.dumps({"jsonrpc": "2.0", "id": 1, "method": method,
                       "params": params}).encode()
    req = urllib.request.Request(url, data=body, headers={
        "Content-Type": "application/json",
        "Accept": "application/json, text/event-stream",
    })
    with urllib.request.urlopen(req, timeout=timeout) as r:
        return json.loads(r.read().decode())


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


def setup(url):
    decks = slides()
    names = {k for s in decks for k in s} | {"Deck.Ghost"} | set(COLORS)
    props = [{"name": n, "type": field_type(n)} for n in sorted(names)]

    # register_properties refuses a name that already exists and is
    # all-or-nothing, so a re-run of setup would fail as a batch. Register one
    # at a time and let the already-there ones fall through — this script is
    # meant to be safe to re-run mid-talk.
    made = 0
    for p in props:
        try:
            d = rpc(url, "tools/call", {"name": "register_properties",
                                        "arguments": {"properties": [p]}})
            if "error" not in d:
                made += 1
        except (urllib.error.URLError, OSError, TimeoutError) as e:
            sys.exit(f"register_properties: {e}")
    print(f"  registered {made} new properties ({len(props)} total)")

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


def show(url, n):
    decks = slides()
    if not 1 <= n <= len(decks):
        sys.exit(f"slide {n} out of range (1..{len(decks)})")
    for name, value in decks[n - 1].items():
        call(url, "set_value", {"name": name, "value": value})
    STATE.write_text(str(n))
    print(f"  slide {n}/{len(decks)}")


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

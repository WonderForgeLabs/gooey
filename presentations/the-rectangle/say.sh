#!/bin/sh
# say.sh — render the narration offline, with piper.
#
#   ./say.sh all              every beat, in order, to audio/NN-<voice>.wav
#   ./say.sh 3                just beat 3
#   ./say.sh 3 claude         beat 3 in the second voice
#   ./say.sh play 03-ryan     play a rendered take
#
# Why piper and not a cloud voice: this is a rehearsal tool. It has no API key,
# no per-character budget and no network, so the script can be re-cut fifty
# times while the timings settle. Spend a paid voice on the final take, if the
# final take even wants one.
#
# First run downloads two ~115MB voice models into voices/, which is gitignored.
# The download is explicit on purpose: piper's CLI does NOT fetch a missing
# voice, it just fails with "Unable to find voice".
set -e
here=$(cd "$(dirname "$0")" && pwd)
cd "$here"

voices=voices
out=audio
narrator=en_US-ryan-high
claude=en_US-lessac-high

ensure_voices() {
  [ -f "$voices/$narrator.onnx" ] && [ -f "$voices/$claude.onnx" ] && return 0
  echo "downloading voice models into $voices/ (~230MB, once)…"
  mkdir -p "$voices"
  ( cd "$voices" && uvx --from piper-tts python -m piper.download_voices \
      "$narrator" "$claude" )
}

# beat N -> the Nth ```speak block, spoken by $2
render() {
  n=$1
  case "$2" in
    claude) voice=$claude; tag=lessac ;;
    *)      voice=$narrator; tag=ryan ;;
  esac
  file=$(printf '%s/%02d-%s.wav' "$out" "$n" "$tag")
  python3 - "$n" <<'PY' | uvx --from piper-tts piper -m "$voice" --data-dir "$voices" -f "$file"
import re, sys, pathlib
blocks = re.findall(r"```speak\n(.*?)```", pathlib.Path("NARRATION.md").read_text(), re.S)
n = int(sys.argv[1])
if not 1 <= n <= len(blocks):
    sys.exit(f"beat {n} out of range (1..{len(blocks)})")
# One line: piper treats newlines as separate utterances and the seams are
# audible. Paragraph breaks become sentence pauses instead.
print(" ".join(blocks[n - 1].split()))
PY
  printf '%s  ' "$file"
  ffprobe -v error -show_entries format=duration -of default=nw=1 "$file" 2>/dev/null || echo
}

count() {
  python3 -c "import re,pathlib;print(len(re.findall(r'\`\`\`speak',pathlib.Path('NARRATION.md').read_text())))"
}

case "$1" in
  play) paplay "$out/$2.wav" ;;
  all)
    ensure_voices; mkdir -p "$out"
    n=1; last=$(count)
    while [ "$n" -le "$last" ]; do render "$n" "$2"; n=$((n + 1)); done
    ;;
  ''|*[!0-9]*) echo "usage: say.sh {all|<beat>|play <basename>} [claude]" >&2; exit 2 ;;
  *) ensure_voices; mkdir -p "$out"; render "$1" "$2" ;;
esac

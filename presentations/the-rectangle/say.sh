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

# The Nth ```speak block as ONE line, on stdout.
#
# One line because piper treats newlines as separate utterances and the
# seams are audible; paragraph breaks become sentence pauses instead.
line() {
  python3 - "$1" <<'PY'
import re, sys, pathlib
blocks = re.findall(r"```speak\n(.*?)```", pathlib.Path("NARRATION.md").read_text(), re.S)
n = int(sys.argv[1])
if not 1 <= n <= len(blocks):
    sys.exit(f"beat {n} out of range (1..{len(blocks)})")
print(" ".join(blocks[n - 1].split()))
PY
}

# beat N -> the Nth ```speak block, spoken by $2
render() {
  n=$1
  case "$2" in
    claude) voice=$claude; tag=lessac ;;
    *)      voice=$narrator; tag=ryan ;;
  esac
  file=$(printf '%s/%02d-%s.wav' "$out" "$n" "$tag")
  txt=${file%.wav}.txt

  # Extract to a FILE first, then feed piper from it. The obvious shape --
  # `line "$n" | piper` -- cannot fail correctly: `set -e` judges a
  # pipeline by its LAST command, so when the extract side died piper
  # still ran, got empty stdin, wrote a header-only wav and exited 0.
  # That is how a 0-byte 25-*.wav appeared while `all` reported success.
  if ! line "$n" > "$txt"; then
    rm -f "$txt"
    echo "say.sh: beat $n -- could not extract the speak block" >&2
    return 1
  fi
  uvx --from piper-tts piper -m "$voice" --data-dir "$voices" -f "$file" < "$txt"

  # The .txt is not a by-product, it is the receipt: it records exactly
  # what this wav says, so staleness can be decided by COMPARING TEXT
  # rather than by comparing mtimes. mtime was the first attempt and it
  # was useless -- re-measuring the DURATION markers rewrites NARRATION.md
  # and invalidated all 24 takes on a change that touched no speech. A
  # check that cries wolf on every routine edit trains you to ignore it,
  # which is worse than not having it.
  size=$(wc -c < "$file" 2>/dev/null || echo 0)
  if [ "$size" -lt 4096 ]; then
    echo "say.sh: beat $n rendered $size bytes -- that is silence, not narration" >&2
    return 1
  fi
  printf '%s  ' "$file"
  ffprobe -v error -show_entries format=duration -of default=nw=1 "$file" 2>/dev/null || echo
}

# The **VOICE:** marker for beat $1, unless $2 overrides it.
voice_of() {
  [ -n "$2" ] && { printf '%s' "$2"; return; }
  python3 -c "
import re,pathlib,sys
m=re.findall(r'\*\*VOICE:\*\*\s*(\w+)',pathlib.Path('NARRATION.md').read_text())
i=int(sys.argv[1])
print(m[i-1] if len(m)>=i else 'narrator')" "$1"
}

# Whole blocks, not opening fences. The preamble explains the format and so
# mentions ```speak in prose; counting fences made `all` walk two beats past
# the end, and piper wrote an empty 25-*.wav rather than failing.
count() {
  python3 -c "
import re,pathlib
print(len(re.findall(r'\`\`\`speak\n(.*?)\`\`\`',pathlib.Path('NARRATION.md').read_text(),re.S)))"
}

case "$1" in
  play) paplay "$out/$2.wav" ;;
  all)
    # Per-beat voice, from the **VOICE:** markers. The talk hands over from the
    # presenter to the agent at beat 12 and never hands back, and beat 11 says
    # so out loud — so a single-voice render makes that beat a lie. An explicit
    # role as $2 overrides, for checking one voice end to end.
    ensure_voices; mkdir -p "$out"
    n=1; last=$(count)
    while [ "$n" -le "$last" ]; do
      render "$n" "$(voice_of "$n" "$2")"
      n=$((n + 1))
    done
    ;;
  ''|*[!0-9]*) echo "usage: say.sh {all|<beat>|play <basename>} [claude]" >&2; exit 2 ;;
  *) ensure_voices; mkdir -p "$out"; render "$1" "$(voice_of "$1" "$2")" ;;
esac

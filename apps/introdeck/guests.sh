#!/bin/sh
# Build every guest binary the deck runs, DERIVED from NARRATION.md.
#
# Several beats host a real program, and each lives in its own module —
# `go run ../scene` cannot cross that boundary, so the binary has to exist
# before the take. NARRATION.md used to say so in a five-line shell block
# a human had to read and run, and this is what that costs: on the machine
# where this script was written, all four guests were missing and three of
# the deck's slides would have opened showing a red island instead of the
# app they exist to show.
#
# So the list is not written here either. A guest is a token in a `Cmd=`
# attribute of the form `./name` or `../dir/name` whose directory under
# apps/ holds a go.mod — which is exactly what "a sibling module the deck
# runs as a prebuilt binary" means, and it is read off the deck's own
# content. Add a beat that hosts a new app and this builds it unasked.
#
# Two things it deliberately does NOT match, and both are the reason the
# rule keys on go.mod rather than on the shape of the path:
#
#   - `./watchgo.sh ../intro main.go intro` — watchgo.sh is not a module,
#     and it builds `intro` itself on every change, which is the point of
#     beat 3.2. Pre-building it here would be a second, staler copy.
#   - `../intro/main.go` in the vim command — an argument, not a binary.
#
# TestGuestScriptBuildsEveryGuestTheDeckRuns pins this script's answer
# against an independent walk of NARRATION.md, so the two cannot drift.
set -eu

cd "$(dirname "$0")"

guests=$(
	sed -n 's/.*Cmd="\([^"]*\)".*/\1/p' NARRATION.md |
		tr ' ' '\n' |
		sed -n -e 's|^\.\./\([A-Za-z0-9_-]*\)/\1$|\1|p' \
			-e 's|^\./\([A-Za-z0-9_-]*\)$|\1|p' |
		sort -u
)

if [ -z "$guests" ]; then
	echo "no guests found in NARRATION.md — the Cmd= scan is broken, not the deck" >&2
	exit 1
fi

status=0
for g in $guests; do
	if [ ! -f "../$g/go.mod" ]; then
		# Not a module: watchgo.sh and friends. Skipped by the rule above,
		# and said out loud rather than silently dropped.
		echo "  --  $g is not a sibling module; nothing to build"
		continue
	fi
	printf '  ..  %s\n' "$g"
	if ! out=$(cd "../$g" && go build -o "$g" . 2>&1); then
		printf '  XX  %s\n%s\n' "$g" "$out" >&2
		status=1
		continue
	fi
	printf '  ok  %s\n' "$g"
done

exit "$status"

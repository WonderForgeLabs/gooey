#!/bin/sh
# Mutation sweep over the hooks: prove each test can actually fail.
#
# run.sh passing tells you nothing on its own -- a suite of SILENT
# expectations passes perfectly against a hook that does nothing at all. So
# each mutation below breaks exactly one check, in a NAMED direction, and this
# script asserts that run.sh goes red and that the RIGHT case is the red one.
#
# Both directions are covered, because the over-generalising one is the one
# that gets missed:
#   under-generalise  -- the check stops firing        -> a WARN/BLOCK case reds
#   over-generalise   -- the check fires on everything -> a NEAR-MISS case reds
#
#   sh .claude/plugin/tests/mutate.sh

set -u
here=$(cd "$(dirname "$0")" && pwd)
plugin=$(dirname "$here")
G="$plugin/hooks/bash-guard.sh"
F="$plugin/hooks/go-format.sh"
L="$plugin/hooks/lib.sh"

pass=0; fail=0
work=$(mktemp -d "${TMPDIR:-/tmp}/gooey-mutate.XXXXXX")
cp "$G" "$work/G.orig"; cp "$F" "$work/F.orig"; cp "$L" "$work/L.orig"
restore() { cp "$work/G.orig" "$G"; cp "$work/F.orig" "$F"; cp "$work/L.orig" "$L"; }
trap 'restore; rm -rf "$work"' EXIT INT TERM

# mutate <direction> <name> <expected-failing-case-substring> <file> <sed-expr>
mutate() {
  dir=$1; name=$2; wantcase=$3; file=$4; expr=$5
  restore
  before=$(cat "$file")
  sed -i "$expr" "$file"
  after=$(cat "$file")
  if [ "$before" = "$after" ]; then
    fail=$((fail+1))
    printf '  FAIL %-46s [%s] MUTATION DID NOT APPLY -- the arms would have\n' "$name" "$dir"
    printf '       agreed and this whole result would have been a harness artifact\n'
    return
  fi
  out=$(sh "$here/run.sh" 2>&1)
  restore
  if printf '%s' "$out" | grep -q '0 failed'; then
    fail=$((fail+1))
    printf '  FAIL %-46s [%s] suite stayed GREEN under mutation\n' "$name" "$dir"
    return
  fi
  reds=$(printf '%s\n' "$out" | grep '^  FAIL' | sed 's/^  FAIL *//;s/ *\[.*//')
  if printf '%s\n' "$reds" | grep -qF "$wantcase"; then
    pass=$((pass+1))
    printf '  ok   %-46s [%s] red: %s\n' "$name" "$dir" \
      "$(printf '%s' "$reds" | tr '\n' ',' | cut -c1-58)"
  else
    fail=$((fail+1))
    printf '  FAIL %-46s [%s] wrong case red\n       wanted: %s\n       got:    %s\n' \
      "$name" "$dir" "$wantcase" "$(printf '%s' "$reds" | tr '\n' ',')"
  fi
}

echo "== the checks stop firing (under-generalise) =="
mutate under "go build: drop the ./... requirement means it never fires" \
  "go build ./..." "$G" "s|hook_has_word \"\$s\" '\\\\./\\\\.\\\\.\\\\.' \|\| continue|continue|"
mutate under "git add -A: the -A arm removed" \
  "git add -A" "$G" "s|hook_has_word \"\$args\" '\\\\-A'|false|"
mutate under "binary check: magic-byte test always says no" \
  "git add <dir> holding an ELF" "$L" 's|^  case "\$magic" in|  case "NOPE" in|'
mutate under "stash clear: the block arm removed" \
  "git stash clear" "$G" 's|git\[\[:space:\]\]+stash\[\[:space:\]\]+clear|__never__|'
mutate under "grep: the advisory removed" \
  "grep -rn PATTERN ." "$G" 's|^  if \[ ! -f "\$mark" \]; then|  if false; then|'
mutate under "gofmt: never reports" \
  "an unformatted .go file" "$F" 's#^\[ -n "$out" \] .*#exit 0#'

echo
echo "== the checks fire on everything (over-generalise) =="
mutate over  "go build: -o no longer exempts" \
  "NEAR-MISS go build -o /tmp/x ./..." "$G" "s|&& continue  # -o is the fix|\&\& true|"
mutate over  "go build: any go subcommand, not just build" \
  "NEAR-MISS go vet ./..." "$G" "s|go\[\[:space:\]\]+build(\[\[:space:\]\]|go[[:space:]]+[a-z]+([[:space:]]|"
mutate over  "git add: fires on any git add at all" \
  "NEAR-MISS git add explicit paths" "$G" "s|if hook_has_word \"\$args\" '\\\\-A'|if true \|\|  hook_has_word \"\$args\" '\\\\-A'|"
mutate over  "binary check: mode bit instead of magic bytes" \
  "NEAR-MISS same dir, only a 755 .sh" "$L" 's|^  magic=.*|  [ -x "$1" ] \&\& return 0|'
mutate over  "the gooey gate removed: every repo is gooey" \
  "NEAR-MISS git add -A in ANOTHER repo" "$G" 's#^hook_in_gooey || exit 0#HOOK_REPO=$PWD#'
mutate over  "grep: the ^ anchor dropped, so 'command grep' matches" \
  "NEAR-MISS command grep -rn" "$G" "s|'\\^\\[\\[:space:\\]\\]\\*grep|'[[:space:]]*grep|"
mutate over  "stash: drop/pop blocks instead of warning" \
  "git stash pop" "$G" 's#^  hook_warn \\$#  hook_block \\#'
mutate over  "gofmt: warns on every .go file" \
  "NEAR-MISS a gofmt-clean .go file" "$F" 's#^\[ -n "$out" \] .*#out=$f#'

echo
if [ "$fail" -eq 0 ]; then
  echo "MUTATIONS: $pass proved, 0 failed"
else
  echo "MUTATIONS: $pass proved, $fail FAILED"
fi
[ "$fail" -eq 0 ]

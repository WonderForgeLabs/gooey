#!/bin/sh
# Hook tests. Every case is a pair: the thing the hook targets, and the
# NEAR-MISS it must leave alone.
#
# A hook that misfires is worse than no hook -- it wedges a fleet, and the
# author is not around to debug it. So the near-miss half of each pair is the
# more important half, and it is why this file exists at all.
#
#   sh .claude/plugin/tests/run.sh
#
# Exit 0 iff every case behaved. Reads the summary line, not the scroll.

set -u
here=$(cd "$(dirname "$0")" && pwd)
plugin=$(dirname "$here")
repo=$(cd "$plugin/../.." && pwd)

pass=0; fail=0

# emit a PreToolUse Bash payload
bash_json() {
  jq -nc --arg c "$1" --arg d "${2:-$repo}" \
    '{hook_event_name:"PreToolUse",tool_name:"Bash",cwd:$d,session_id:"t",tool_input:{command:$c}}'
}
edit_json() {
  jq -nc --arg f "$1" \
    '{hook_event_name:"PostToolUse",tool_name:"Edit",cwd:"'"$repo"'",session_id:"t",tool_input:{file_path:$f}}'
}

# run <script> <json> -> sets RC and OUT (stdout) and ERR (stderr)
run() {
  OUT=$(printf '%s' "$2" | "$1" 2>"$here/.stderr"); RC=$?
  ERR=$(cat "$here/.stderr"); rm -f "$here/.stderr"
}

# expect BLOCK  <name> <script> <json>
# expect WARN   <name> <script> <json> <substring-of-additionalContext>
# expect SILENT <name> <script> <json>
expect() {
  want=$1; name=$2; scr=$3; payload=$4; needle=${5:-}
  run "$scr" "$payload"
  case "$want" in
    BLOCK)
      if [ "$RC" -eq 2 ] && [ -n "$ERR" ]; then ok=1; else ok=0; fi ;;
    WARN)
      if [ "$RC" -eq 0 ] && printf '%s' "$OUT" | jq -e \
           --arg n "$needle" '.hookSpecificOutput.additionalContext|test($n)' \
           >/dev/null 2>&1; then ok=1; else ok=0; fi ;;
    SILENT)
      if [ "$RC" -eq 0 ] && [ -z "$OUT" ]; then ok=1; else ok=0; fi ;;
  esac
  if [ "$ok" -eq 1 ]; then
    pass=$((pass+1)); printf '  ok   %-58s [%s]\n' "$name" "$want"
  else
    fail=$((fail+1)); printf '  FAIL %-58s [%s] rc=%s\n' "$name" "$want" "$RC"
    printf '       stdout: %s\n       stderr: %s\n' "$(printf '%s' "$OUT" | head -c 200)" "$(printf '%s' "$ERR" | head -c 200)"
  fi
}

# check <name> <actual> <want> -- for the cases that are about the scripts
# themselves rather than about a hook's verdict on a payload.
check() {
  if [ "$2" = "$3" ]; then
    pass=$((pass+1)); printf '  ok   %-58s [CHECK]\n' "$1"
  else
    fail=$((fail+1)); printf '  FAIL %-58s [CHECK] got %s want %s\n' "$1" "$2" "$3"
  fi
}

G="$plugin/hooks/bash-guard.sh"
F="$plugin/hooks/go-format.sh"

echo "== 1. go build ./... (advisory) =="
expect WARN   "go build ./..."                      "$G" "$(bash_json 'go build ./...')" 'writes one executable'
expect WARN   "cd x && go build ./... (segmented)"  "$G" "$(bash_json 'cd cmd && go build ./...')" 'writes one executable'
expect SILENT "NEAR-MISS go build -o /tmp/x ./..."  "$G" "$(bash_json 'go build -o /tmp/gooey-bin/ ./...')"
expect SILENT "NEAR-MISS go build ./cmd/foo"        "$G" "$(bash_json 'go build ./cmd/browser')"
expect SILENT "NEAR-MISS go vet ./..."              "$G" "$(bash_json 'go vet ./...')"
expect SILENT "NEAR-MISS go test ./..."             "$G" "$(bash_json 'go test ./...')"

echo "== 2. git add -A (BLOCKING) =="
expect BLOCK  "git add -A"                          "$G" "$(bash_json 'git add -A')"
expect BLOCK  "git add ."                           "$G" "$(bash_json 'git add .')"
expect BLOCK  "git add --all"                       "$G" "$(bash_json 'git add --all')"
expect BLOCK  "git commit && git add -A (segment)"  "$G" "$(bash_json 'git status && git add -A')"
expect SILENT "NEAR-MISS git add -A in ANOTHER repo" "$G" "$(bash_json 'git add -A' /tmp)"
expect SILENT "NEAR-MISS git add explicit paths"    "$G" "$(bash_json 'git add README.md docs/demos.md')"
expect SILENT "NEAR-MISS git add -- Apple.go"       "$G" "$(bash_json 'git add -- Apple.go')"
expect SILENT "NEAR-MISS git add -u"                "$G" "$(bash_json 'git add -u')"
expect SILENT "NEAR-MISS the string in a PR body"   "$G" "$(bash_json 'gh pr comment 1 -b "never use git add -A here"')"

echo "== 3. compiled executable in a pathspec (BLOCKING) =="
mkdir -p "$repo/.hooktest-bin"
printf '\177ELF\002\001\001\000junk' > "$repo/.hooktest-bin/fakebin"
chmod +x "$repo/.hooktest-bin/fakebin"
printf '#!/bin/sh\necho hi\n' > "$repo/.hooktest-bin/script.sh"
chmod +x "$repo/.hooktest-bin/script.sh"
expect BLOCK  "git add <dir> holding an ELF"        "$G" "$(bash_json 'git add .hooktest-bin/')"
# Naming the binary itself, and the one-liner it actually gets typed as. The
# compound is the important one: nothing is staged at PreToolUse time, so the
# `git commit` half sees an empty index and the `git add` half is the only
# thing between the agent and a committed ELF.
expect BLOCK  "git add <the ELF itself, not its dir>" "$G" "$(bash_json 'git add .hooktest-bin/fakebin')"
expect BLOCK  "git add <ELF> && git commit -- <ELF>"  "$G" "$(bash_json 'git add .hooktest-bin/fakebin && git commit -m wip -- .hooktest-bin/fakebin')"
expect SILENT "NEAR-MISS git add a 755 .sh by name" "$G" "$(bash_json 'git add .hooktest-bin/script.sh')"
rm -f "$repo/.hooktest-bin/fakebin"
expect SILENT "NEAR-MISS same dir, only a 755 .sh"  "$G" "$(bash_json 'git add .hooktest-bin/')"
expect SILENT "NEAR-MISS this plugin's own dir"     "$G" "$(bash_json 'git add .claude/plugin/')"
expect SILENT "NEAR-MISS git commit, nothing staged" "$G" "$(bash_json 'git commit -m wip')"
rm -rf "$repo/.hooktest-bin"

# The `git commit` half needs a real index with a real ELF in it. Without a
# positive case here the arm was unproved: `run.sh` carried only the near-miss
# (a commit with nothing staged), which passes just as well against a check
# that never looks at the index, and `mutate.sh` had nothing to turn red.
#
# It runs against a THROWAWAY repo rather than this checkout: staging into the
# live index would leave it dirty if this script is interrupted, and the gate's
# only notion of identity is the module path in go.mod, so a temp directory
# with one line in it is a gooey repo as far as the hook is concerned.
fake=$(mktemp -d "${TMPDIR:-/tmp}/gooey-hooktest.XXXXXX")
printf 'module github.com/WonderForgeLabs/gooey\n' > "$fake/go.mod"
( cd "$fake" && git init -q ) >/dev/null 2>&1
printf '\177ELF\002\001\001\000junk' > "$fake/stagedbin"
printf '#!/bin/sh\necho hi\n'        > "$fake/staged.sh"
chmod +x "$fake/stagedbin" "$fake/staged.sh"
( cd "$fake" && git add staged.sh ) >/dev/null 2>&1
expect SILENT "NEAR-MISS git commit, only a 755 .sh staged" "$G" "$(bash_json 'git commit -m wip' "$fake")"
( cd "$fake" && git add stagedbin ) >/dev/null 2>&1
expect BLOCK  "git commit with an ELF already staged" "$G" "$(bash_json 'git commit -m wip' "$fake")"
rm -rf "$fake"

echo "== 4. git stash =="
expect BLOCK  "git stash clear"                     "$G" "$(bash_json 'git stash clear')"
expect WARN   "git stash pop"                       "$G" "$(bash_json 'git stash pop')" 'REPO-GLOBAL'
expect WARN   "git stash drop"                      "$G" "$(bash_json "git stash drop 'stash@{0}'")" 'REPO-GLOBAL'
expect SILENT "NEAR-MISS git stash push (creating)" "$G" "$(bash_json 'git stash push -m wip')"
expect SILENT "NEAR-MISS git stash list"            "$G" "$(bash_json 'git stash list')"

echo "== 5. recursive grep (advisory, once per session) =="
rm -f "${TMPDIR:-/tmp}/gooey-grep-note.t"
expect WARN   "grep -rn PATTERN ."                  "$G" "$(bash_json 'grep -rn Composer .')" 'not /usr/bin/grep'
expect SILENT "same session, second time (dedup)"   "$G" "$(bash_json 'grep -rn Frame .')"
rm -f "${TMPDIR:-/tmp}/gooey-grep-note.t"
expect SILENT "NEAR-MISS command grep -rn"          "$G" "$(bash_json 'command grep -rn Composer .')"
expect SILENT "NEAR-MISS git grep"                  "$G" "$(bash_json 'git grep -n Composer')"
expect SILENT "NEAR-MISS non-recursive grep"        "$G" "$(bash_json 'grep -n Composer composer.go')"
expect SILENT "NEAR-MISS piped into grep"           "$G" "$(bash_json 'go test ./... | grep FAIL')"

echo "== 6. gofmt (advisory, PostToolUse) =="
printf 'package p\n\nfunc  F( ) {\n}\n' > "$repo/.hooktest-bad.go"
printf 'package p\n\nfunc F() {}\n'      > "$repo/.hooktest-ok.go"
expect WARN   "an unformatted .go file"             "$F" "$(edit_json "$repo/.hooktest-bad.go")" 'gofmt -w'
expect SILENT "NEAR-MISS a gofmt-clean .go file"    "$F" "$(edit_json "$repo/.hooktest-ok.go")"
expect SILENT "NEAR-MISS a .md file"                "$F" "$(edit_json "$repo/README.md")"
expect SILENT "NEAR-MISS a .go file outside gooey"  "$F" "$(edit_json "/tmp/elsewhere.go")"
rm -f "$repo/.hooktest-bad.go" "$repo/.hooktest-ok.go"

echo "== 7. the gate itself =="
expect SILENT "NEAR-MISS every rule, outside gooey" "$G" "$(bash_json 'git add -A; git stash clear; go build ./...' /tmp)"
expect SILENT "empty command"                       "$G" "$(bash_json '')"

echo "== 8. portability: the GNU-isms that fail SILENTLY on macOS =="
# `\n` in a sed REPLACEMENT is a GNU extension -- BSD sed substitutes a
# literal `n`. hook_segments used to be four sed substitutions, so on macOS it
# returned the whole command as one segment and BOTH blocking checks weakened
# without a word: `git status && git add -A` stopped being blocked, because
# the `git add` segment no longer existed. Compounding it, mutate.sh used GNU
# `sed -i EXPR FILE`, which BSD reads as "-i with suffix EXPR", so every
# mutation reported DID NOT APPLY and the suite could not even diagnose the
# first problem. Neither failure is visible from a Linux box, which is why
# these are checks and not a sentence in the README.
check "hook_segments splits on && || ; and |" \
  "$( . "$plugin/hooks/lib.sh"; hook_segments 'a && b || c ; d | e' | grep -c . )" 5
# The pattern below is written with a bracket expression so this line does not
# match itself, and comment lines are stripped first so that the paragraph
# above -- which has to name the thing it forbids -- does not count as three
# violations. There is no portable in-place spelling: GNU takes the next
# argument as the expression, BSD takes it as the backup suffix, and `-i ''`
# flips which one breaks. Write to a temp file and copy it back instead.
check "no GNU sed in-place edits anywhere in the plugin" \
  "$(cat "$plugin"/hooks/*.sh "$plugin"/scripts/*.sh "$here"/*.sh |
       grep -v '^[[:space:]]*#' | grep -c 'sed[ ]-i')" 0

echo
if [ "$fail" -eq 0 ]; then
  echo "HOOKS: $pass passed, 0 failed"
else
  echo "HOOKS: $pass passed, $fail FAILED"
fi
[ "$fail" -eq 0 ]

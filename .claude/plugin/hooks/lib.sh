# Shared helpers for gooey's hooks. Sourced, never executed.
#
# Contract with Claude Code (verified against code.claude.com/docs/en/hooks.md):
#   - the tool call arrives on STDIN as JSON. There is no $CLAUDE_TOOL_INPUT.
#   - exit 0 + stdout starting with `{` -> parsed as a hook decision.
#   - exit 2 + stderr                   -> BLOCKS the tool call, stderr is the
#                                          reason shown to the model.
#   - any other non-zero               -> non-blocking error; the tool runs.
#
# ADVISORY hooks here deliberately omit `permissionDecision`. Emitting
# "allow" would also AUTO-APPROVE a command the user would otherwise be
# prompted about -- a warning must not double as a permission grant.

# ---------------------------------------------------------------- input ----

HOOK_INPUT=""
hook_read_input() {
  HOOK_INPUT=$(cat)
}

# The Bash tool's command string, or empty.
hook_command() {
  printf '%s' "$HOOK_INPUT" | jq -r '.tool_input.command // empty' 2>/dev/null
}

hook_field() {
  printf '%s' "$HOOK_INPUT" | jq -r ".$1 // empty" 2>/dev/null
}

# ----------------------------------------------------------------- gate ----

# True only inside a checkout of github.com/WonderForgeLabs/gooey.
#
# This gate is what keeps the blocking hooks off every other repo on the
# machine -- `git add -A` is ordinary practice elsewhere, and a plugin that
# blocks it globally is a plugin people uninstall. Identity comes from the
# module path in go.mod, not from a directory name, so a worktree, a clone
# under another name, and a fork all resolve the same.
hook_in_gooey() {
  d=${1:-}
  [ -n "$d" ] || d=$(hook_field 'cwd')
  [ -n "$d" ] || d=${CLAUDE_PROJECT_DIR:-}
  [ -n "$d" ] || d=$PWD
  [ -d "$d" ] || return 1
  while [ "$d" != "/" ] && [ -n "$d" ]; do
    if [ -f "$d/go.mod" ] &&
       head -n 1 "$d/go.mod" 2>/dev/null |
         grep -q '^module github\.com/WonderForgeLabs/gooey$'; then
      HOOK_REPO=$d
      return 0
    fi
    d=$(dirname "$d")
  done
  return 1
}

# ------------------------------------------------------------ splitting ----

# Split a shell command string into rough simple-command segments, one per
# line, on `;` `&&` `||` `|` and newline.
#
# This is not a shell parser and does not pretend to be. It exists so that
# `cd x && go build ./...` is judged on the `go build` segment and
# `go build -o /tmp/bin ./...` is judged as one segment with a `-o` in it --
# the two cases a whole-string grep gets wrong in opposite directions.
# A separator inside quotes over-splits, which can only ever cost a spurious
# ADVISORY line; nothing here blocks on a pattern a quote could fabricate.
#
# awk, NOT sed. `\n` in a sed REPLACEMENT is a GNU extension: BSD/macOS sed
# substitutes a literal `n`, so this function used to degenerate into "return
# the whole command as one segment" on every Mac -- silently, and in the
# direction that WEAKENS both blocking checks. `git status && git add -A`
# went straight through, because the `git add` segment no longer existed.
# awk's gsub takes a real newline in its replacement string on every awk
# (checked against gawk, mawk and busybox awk); the `||` alternative is
# listed before `|` and POSIX leftmost-longest matching keeps it that way.
hook_segments() {
  printf '%s\n' "$1" | awk '{ gsub(/&&|\|\||;|\|/, "\n"); print }'
}

# Does this segment invoke $2 as its command (leading position)?
hook_seg_is() {
  printf '%s' "$1" | grep -Eq "^[[:space:]]*$2([[:space:]]|$)"
}

# Is $2 present as a standalone word in $1?
hook_has_word() {
  printf '%s' "$1" | grep -Eq "(^|[[:space:]])$2([[:space:]]|$)"
}

# ---------------------------------------------------------------- output ----

HOOK_NOTES=""

hook_warn() {
  if [ -n "$HOOK_NOTES" ]; then
    HOOK_NOTES="$HOOK_NOTES

$1"
  else
    HOOK_NOTES="$1"
  fi
}

# Emit accumulated advisories as PreToolUse additionalContext and exit 0.
hook_emit_advisories() {
  event=${1:-PreToolUse}
  [ -n "$HOOK_NOTES" ] || exit 0
  jq -nc --arg e "$event" --arg c "$HOOK_NOTES" \
    '{hookSpecificOutput: {hookEventName: $e, additionalContext: $c}}'
  exit 0
}

# Block the tool call. stderr is what the model reads.
hook_block() {
  printf '%s\n' "$1" >&2
  exit 2
}

# ------------------------------------------------------------- binaries ----

# Is $1 a compiled executable? Magic bytes, not the mode bit -- this repo is
# full of legitimately-755 shell scripts, and blocking those would make the
# hook worse than useless.
hook_is_binary() {
  [ -f "$1" ] || return 1
  magic=$(head -c 4 "$1" 2>/dev/null | od -An -tx1 | tr -d ' \n')
  case "$magic" in
    7f454c46*) return 0 ;;  # ELF
    cffaedfe*|feedfacf*|cefaedfe*|feedface*) return 0 ;;  # Mach-O
    cafebabe*) return 0 ;;  # Mach-O fat / Java class
    4d5a*)     return 0 ;;  # PE (MZ)
  esac
  return 1
}

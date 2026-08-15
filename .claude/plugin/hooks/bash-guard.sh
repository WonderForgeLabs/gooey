#!/bin/sh
# PreToolUse / Bash. One process per Bash call, six checks inside it.
#
# Split into six scripts this would spawn six processes on every Bash call in
# a repo that routinely has five to fifteen agents live at once. One
# dispatcher, one spawn.
#
# NOT A GATE. A PreToolUse hook that times out is canceled, its output
# discarded, and the call continues through the normal permission flow. Every
# block below catches the honest mistake; none of them is a guarantee, and
# nothing in this plugin should be read as claiming otherwise.

set -u
dir=$(dirname "$0")
. "$dir/lib.sh"

hook_read_input
cmd=$(hook_command)
[ -n "$cmd" ] || exit 0

# Every check below is scoped to a gooey checkout. `git add -A` is ordinary
# practice in other repos; a plugin that blocks it everywhere gets uninstalled.
hook_in_gooey || exit 0
repo=$HOOK_REPO

segs=$(hook_segments "$cmd")

# ------------------------------------------------------------------------
# 1. `go build ./...` at the repo root -- ADVISORY.
#
# It writes one executable per main package into the tree. .gitignore covers
# some of them and has never covered all of them, which is how five got
# committed. The message gives the derived list rather than a count, because
# a count in prose is what went stale in the first place.
# ------------------------------------------------------------------------
printf '%s\n' "$segs" | while IFS= read -r s; do
  hook_seg_is "$s" "go" || continue
  printf '%s' "$s" | grep -Eq '^[[:space:]]*go[[:space:]]+build([[:space:]]|$)' || continue
  hook_has_word "$s" '\./\.\.\.' || continue          # ./cmd/foo is not this
  printf '%s' "$s" | grep -Eq '(^|[[:space:]])-o([[:space:]]|=)' && continue  # -o is the fix
  echo HIT
done | grep -q HIT && hook_warn \
"\`go build ./...\` writes one executable per main package into the tree, and
.gitignore has never covered all of them -- five have been committed before.

  Whole-repo compile check:  go vet ./...
  When you actually want binaries:  go build -o /tmp/gooey-bin/ ./...
      (-o takes a DIRECTORY for a multi-package build and leaves the tree clean)

Which names would land unignored today is derived, not remembered:

  cd $repo && go list -f '{{if eq .Name \"main\"}}{{.Dir}}{{end}}' ./... |
    sed 's|.*/||' | sort -u |
    while read b; do git check-ignore -q \"\$b\" || echo \"UNIGNORED: \$b\"; done"

# ------------------------------------------------------------------------
# 2. `git add -A` / `--all` / `.` -- BLOCKING.
#
# The only mechanism by which a build artifact has ever entered this repo's
# history. Blocking is justified on all three counts: the action is
# unambiguously wrong here (an explicit pathspec is always available and is
# the house rule), the cost of it landing is a committed binary in a shared
# history, and the cost of a false block is one round trip.
# ------------------------------------------------------------------------
printf '%s\n' "$segs" | while IFS= read -r s; do
  printf '%s' "$s" | grep -Eq '^[[:space:]]*git[[:space:]]+add([[:space:]]|$)' || continue
  args=$(printf '%s' "$s" | sed 's/^[[:space:]]*git[[:space:]]*add//')
  if hook_has_word "$args" '\-A' || hook_has_word "$args" '\-\-all' ||
     hook_has_word "$args" '\-\-no-ignore-removal' || hook_has_word "$args" '\.'; then
    echo HIT
  fi
done | grep -q HIT && hook_block \
"BLOCKED: \`git add -A\` (or \`git add .\`) in the gooey repo.

This is how build artifacts entered this history. \`git status --porcelain\`
lists an untracked DIRECTORY once and never descends, so a binary inside one
is invisible to the check you would have run first.

Stage explicit pathspecs instead:

  git status --porcelain          # look first, every time
  git add path/one path/two
  git commit -m ... -- path/one path/two

If you genuinely want everything, enumerate it and look at the list:

  git status --porcelain --untracked-files=all"

# ------------------------------------------------------------------------
# 3. A pathspec that would sweep in a compiled executable -- BLOCKING.
#
# Three halves, not two. `git add <dir>/` where the directory holds an
# untracked binary; `git add <binary>` NAMING it, which is the most direct
# form of the mistake and which an earlier version of this check waved
# through because it only inspected pathspecs that resolved to a DIRECTORY;
# and `git commit` with an ELF already staged.
#
# The by-name case is the one that matters most in practice, because
# `git add toolkitdemo && git commit -m wip -- toolkitdemo` is a single Bash
# call: at PreToolUse time nothing is staged yet, so the `git commit` half
# has nothing to look at and the `git add` half is the only thing standing
# there. `toolkitdemo` is one of the names .gitignore does not cover.
#
# Detection is by magic bytes, not the mode bit -- this repo is full of
# legitimately-755 shell scripts and blocking those would make the hook worse
# than useless.
# ------------------------------------------------------------------------
found=""
add_paths=$(printf '%s\n' "$segs" | sed -n 's/^[[:space:]]*git[[:space:]]\{1,\}add[[:space:]]\{1,\}//p')
if [ -n "$add_paths" ]; then
  for a in $add_paths; do
    case "$a" in -*) continue ;; esac
    p="$repo/${a%/}"
    if [ -d "$p" ]; then
      for f in $(cd "$repo" && git ls-files -o --exclude-standard -- "${a%/}" 2>/dev/null); do
        hook_is_binary "$repo/$f" && found="$found $f"
      done
    elif [ -f "$p" ]; then
      hook_is_binary "$p" && found="$found ${a%/}"
    fi
  done
fi
if printf '%s\n' "$segs" | grep -Eq '^[[:space:]]*git[[:space:]]+commit([[:space:]]|$)'; then
  for f in $(cd "$repo" && git diff --cached --name-only 2>/dev/null); do
    hook_is_binary "$repo/$f" && found="$found $f"
  done
fi
if [ -n "$found" ]; then
  hook_block \
"BLOCKED: this would commit a compiled executable.

 $(printf '%s' "$found" | tr ' ' '\n' | sed '/^$/d' | sed 's/^/ /')

Those are ELF/Mach-O/PE files, not scripts -- the check is magic bytes, so a
755 shell script does not trip it. Build artifacts do not belong in this
history; five have been committed before, which is why .gitignore carries a
list of demo binaries at all.

  git restore --staged <file>     # unstage it
  rm <file>                       # or delete it -- it rebuilds
  go build -o /tmp/gooey-bin/ ./...   # build out of tree next time

If a binary genuinely belongs in the tree, add it to .gitignore's neighbours
deliberately and say so in the commit message."
fi

# ------------------------------------------------------------------------
# 4. git stash -- `clear` BLOCKS, everything else warns.
#
# The stash stack is REPO-GLOBAL: every worktree under this repo shares one
# stack, and this repo runs five to fifteen worktrees at once. `git stash
# pop` with no ref takes stash@{0}, which is whoever pushed last.
#
# Only `clear` blocks. It destroys every session's stash at once and there is
# no version of that which is right. `drop` and `pop` warn with the live
# listing attached, because an agent dropping its own stash is ordinary and a
# block there would be a false positive several times a day.
# ------------------------------------------------------------------------
if printf '%s\n' "$segs" | grep -Eq '^[[:space:]]*git[[:space:]]+stash[[:space:]]+clear'; then
  hook_block \
"BLOCKED: \`git stash clear\` destroys the stash of every worktree in this repo.

The stash stack is repo-global. This repo runs five to fifteen worktrees at
once, so \"my stashes\" is not a category git has.

  git stash list                  # see whose they are
  git stash drop 'stash@{N}'      # one, explicitly, after archiving it"
fi
if printf '%s\n' "$segs" | grep -Eq '^[[:space:]]*git[[:space:]]+stash[[:space:]]+(drop|pop|apply)'; then
  listing=$(cd "$repo" && git stash list 2>/dev/null | head -20)
  [ -n "$listing" ] || listing="(the stack is empty right now)"
  hook_warn \
"The stash stack is REPO-GLOBAL -- one stack shared by every worktree under
this repo, and this repo runs five to fifteen at once. A bare \`git stash pop\`
takes stash@{0}, which is whoever pushed last, not necessarily you.

Live stack:
$listing

Name the entry you mean, and pin anything destructive before dropping it:

  git update-ref refs/archive/<name> \"\$(git rev-parse 'stash@{N}')\"
  git stash drop 'stash@{N}'"
fi

# ------------------------------------------------------------------------
# 5. Recursive grep -- ADVISORY, once per session.
#
# `grep` in this shell is not /usr/bin/grep. Claude Code injects a shell
# function that redirects to its own ugrep with `--ignore-files`, so the
# search honours .gitignore and silently skips everything in it -- including
# `.claude/worktrees/`, which holds whole checkouts of this repo. `rg` is
# shimmed too and ripgrep honours .gitignore on its own account.
#
# Once per session, keyed on session_id, because recursive grep is frequent
# and a warning on every one of them is noise that gets tuned out.
# ------------------------------------------------------------------------
if printf '%s\n' "$segs" | grep -Eq '^[[:space:]]*grep([[:space:]].*)?[[:space:]]-[a-zA-Z]*[rR]'; then
  sid=$(hook_field 'session_id')
  mark="${TMPDIR:-/tmp}/gooey-grep-note.${sid:-nosession}"
  if [ ! -f "$mark" ]; then
    : > "$mark" 2>/dev/null
    hook_warn \
"\`grep\` here is not /usr/bin/grep. Claude Code injects a shell function that
runs its own ugrep with \`--ignore-files\`, so a recursive grep honours
.gitignore and silently skips what it covers -- including \`.claude/worktrees/\`,
which holds whole checkouts of this repo. Reproduce it in ten seconds: write a
file under a gitignored directory, then compare \`grep -rl\` with
\`command grep -rl\`.

  command grep -rn PATTERN .      # the real grep
  rg --no-ignore -n PATTERN .     # rg is shimmed too, and ripgrep skips
                                  # gitignored paths on its own account

This matters for any claim of the form \"there are no X in this repo\".

And the inverse trap, which has also been hit: filtering \`/\\.claude/\` out of
results to drop worktree noise excludes the ENTIRE TREE when the path you are
searching is itself under \`.claude/worktrees/\`. Filter on the worktree
directory, not on the string \`.claude\`."
  fi
fi

hook_emit_advisories PreToolUse

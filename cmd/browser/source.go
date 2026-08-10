package main

// A SOURCE is a checkout root the browser resolves demos against: the
// tree it was launched from, any other worktree of the same repository,
// or a local branch with no worktree of its own. The first two are
// directories that already exist; the third is materialized on demand as
// a THROWAWAY detached worktree under os.MkdirTemp and removed again when
// the browser switches away or exits. Detached is the load-bearing word:
// the branch itself is never checked out, so nothing the browser does can
// collide with a worktree that already has it, move a ref, or leave a
// branch "checked out elsewhere" for whoever owns the real tree.
//
// git runs as a child process, from the launch root. That is a deliberate
// asymmetry with the rest of the repository's tooling rules: the browser
// is a PROGRAM the user runs, and enumerating worktrees or adding a
// disposable one is exactly what they asked it to do — but every mutating
// command in this file targets only directories this process created.
// Real worktrees are read (list, status, log) and never written.
//
// Everything here that talks to git is serialized through one worker
// goroutine (sourceMgr): enumeration, materialization and removal happen
// in the order the UI asked for them, so a switch-away's remove can never
// race a switch-back's add on the same branch. Results marshal back to
// the UI goroutine via the dispatcher, per the house confinement rule.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// source is one selectable checkout root.
type source struct {
	Name      string // what the picker shows: branch name, or the worktree dir for a detached head
	Root      string // absolute checkout root; "" for a branch not yet materialized
	Branch    string // short branch name; "" for a detached worktree
	Head      string // subject of the tip commit, best effort
	Dirty     bool   // real worktrees: tracked files modified
	Launch    bool   // the tree the browser was started from
	Ephemeral bool   // materialized by this browser; removed on switch-away/exit
}

// id is the identity a selection compares by. A branch keeps the same id
// before and after materialization — the ephemeral worktree is an
// implementation detail of viewing the branch, not a different source.
func (s source) id() string {
	if s.Branch != "" && (s.Root == "" || s.Ephemeral) {
		return "branch:" + s.Branch
	}
	return s.Root
}

// describe is the status-line rendering of the source.
func (s source) describe() string {
	switch {
	case s.Launch:
		return s.Name + " (launch tree)"
	case s.Ephemeral:
		return s.Name + " (ephemeral worktree)"
	default:
		return s.Name + " — " + s.Root
	}
}

// git runs one git command with dir as the working directory and returns
// stdout. Stderr rides the error, because "fatal: not a git repository"
// is the answer the caller needs to show.
func git(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
			return "", fmt.Errorf("git %s: %s", args[0], strings.TrimSpace(string(ee.Stderr)))
		}
		return "", fmt.Errorf("git %s: %w", args[0], err)
	}
	return string(out), nil
}

// branchOf names the branch dir has checked out, "" when detached or not
// a repository. Best effort — the browser works fine without git.
func branchOf(dir string) string {
	out, err := git(dir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return ""
	}
	name := strings.TrimSpace(out)
	if name == "HEAD" { // detached
		return ""
	}
	return name
}

// wtEntry is one worktree from `git worktree list --porcelain`.
type wtEntry struct {
	path     string
	head     string // full sha; "" for a bare entry
	branch   string // short name; "" when detached or bare
	bare     bool
	detached bool
}

// parseWorktrees reads the --porcelain format: attribute lines per
// worktree, blank line between them. Unknown attributes (locked,
// prunable, …) are skipped rather than rejected — the format is
// documented to grow.
func parseWorktrees(out string) []wtEntry {
	var (
		entries []wtEntry
		cur     *wtEntry
	)
	flush := func() {
		if cur != nil {
			entries = append(entries, *cur)
			cur = nil
		}
	}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			flush()
			continue
		}
		switch {
		case strings.HasPrefix(line, "worktree "):
			flush()
			cur = &wtEntry{path: strings.TrimPrefix(line, "worktree ")}
		case cur == nil:
			// attribute before any worktree line: not ours to guess at
		case line == "bare":
			cur.bare = true
		case line == "detached":
			cur.detached = true
		case strings.HasPrefix(line, "HEAD "):
			cur.head = strings.TrimPrefix(line, "HEAD ")
		case strings.HasPrefix(line, "branch "):
			cur.branch = strings.TrimPrefix(strings.TrimPrefix(line, "branch "), "refs/heads/")
		}
	}
	flush()
	return entries
}

// branchInfo is one local branch from for-each-ref.
type branchInfo struct {
	name    string
	subject string
}

// parseBranches reads `for-each-ref --format=%(refname:short)%00%(contents:subject)`
// output: one NUL-separated record per line. NUL because a subject may
// contain anything printable, including the tabs a naive format would
// split on.
func parseBranches(out string) []branchInfo {
	var bs []branchInfo
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		name, subject, _ := strings.Cut(line, "\x00")
		if name == "" {
			continue
		}
		bs = append(bs, branchInfo{name: name, subject: subject})
	}
	return bs
}

// samePath compares two directories by resolved identity, so the launch
// root matches its own worktree entry even through a symlink.
func samePath(a, b string) bool {
	ra, err1 := filepath.EvalSymlinks(a)
	rb, err2 := filepath.EvalSymlinks(b)
	if err1 != nil || err2 != nil {
		return filepath.Clean(a) == filepath.Clean(b)
	}
	return ra == rb
}

// listSources enumerates everything selectable: the launch tree first,
// then the repository's other worktrees, then local branches that no
// worktree has checked out. eph maps branch → the throwaway worktree this
// browser already materialized for it; those directories are OURS and are
// hidden from the worktree group (the branch entry represents them).
//
// The launch tree is in the list even when git fails — a browser run
// from an exported tree still browses itself; it just has nothing to
// switch to.
func listSources(launchRoot string, eph map[string]string) []source {
	launch := source{Name: "this tree", Root: launchRoot, Launch: true}
	if b := branchOf(launchRoot); b != "" {
		launch.Name, launch.Branch = b, b
	} else {
		launch.Name = filepath.Base(launchRoot)
	}
	launch.Head = subjectOf(launchRoot, "HEAD")
	launch.Dirty = isDirty(launchRoot)
	out := []source{launch}

	wtOut, err := git(launchRoot, "worktree", "list", "--porcelain")
	if err != nil {
		return out // not a repository, or no git: the launch tree is it
	}
	brOut, _ := git(launchRoot, "for-each-ref",
		"--format=%(refname:short)%00%(contents:subject)", "refs/heads")
	branches := parseBranches(brOut)
	subject := make(map[string]string, len(branches))
	for _, b := range branches {
		subject[b.name] = b.subject
	}

	taken := map[string]bool{} // branches some real worktree has checked out
	if launch.Branch != "" {
		taken[launch.Branch] = true
	}
	ours := map[string]bool{}
	for _, dir := range eph {
		ours[filepath.Clean(dir)] = true
	}
	for _, wt := range parseWorktrees(wtOut) {
		if wt.bare || samePath(wt.path, launchRoot) || ours[filepath.Clean(wt.path)] {
			continue
		}
		s := source{Root: wt.path, Branch: wt.branch}
		if wt.branch != "" {
			s.Name = wt.branch
			s.Head = subject[wt.branch]
			taken[wt.branch] = true
		} else {
			s.Name = filepath.Base(wt.path) + " (detached)"
			s.Head = subjectOf(launchRoot, wt.head)
		}
		s.Dirty = isDirty(wt.path)
		out = append(out, s)
	}
	for _, b := range branches {
		if taken[b.name] {
			continue
		}
		out = append(out, source{Name: b.name, Branch: b.name, Head: b.subject})
	}
	return out
}

// subjectOf is the one-line summary of a commit, "" on any failure.
func subjectOf(repo, rev string) string {
	if rev == "" {
		return ""
	}
	out, err := git(repo, "log", "-1", "--format=%s", rev)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// isDirty reports tracked modifications in a real worktree. Untracked
// files are deliberately not counted: build output and recordings would
// mark every tree in this repository dirty forever.
func isDirty(dir string) bool {
	out, err := git(dir, "status", "--porcelain", "--untracked-files=no")
	return err == nil && strings.TrimSpace(out) != ""
}

// sourceMgr owns the ephemeral worktrees and the worker goroutine all
// git work runs on. The eph map is touched only from that goroutine
// after start — jobs are the synchronization.
type sourceMgr struct {
	repo string
	eph  map[string]string // branch → temp dir holding its detached worktree
	jobs chan func()
	done chan struct{}
}

func newSourceMgr(repo string) *sourceMgr {
	m := &sourceMgr{repo: repo, eph: map[string]string{},
		jobs: make(chan func(), 16), done: make(chan struct{})}
	go func() {
		defer close(m.done)
		for fn := range m.jobs {
			fn()
		}
	}()
	return m
}

// do queues fn onto the worker. Everything that enumerates, adds or
// removes worktrees goes through here, in UI order.
func (m *sourceMgr) do(fn func()) { m.jobs <- fn }

// Close removes every ephemeral worktree and joins the worker. Called
// once, after the UI loop has returned — and before gooey.Exit, which
// would skip it.
func (m *sourceMgr) Close() {
	m.do(func() { m.cleanup() })
	close(m.jobs)
	<-m.done
}

// materialize gives branch a throwaway detached worktree, reusing the
// one it already has. The checkout goes in a subdirectory of the temp
// dir so `git worktree add` always sees a path that does not exist yet.
func (m *sourceMgr) materialize(branch string) (string, error) {
	if dir, ok := m.eph[branch]; ok {
		return dir, nil
	}
	tmp, err := os.MkdirTemp("", "gooey-browser-src-")
	if err != nil {
		return "", err
	}
	dir := filepath.Join(tmp, "tree")
	if _, err := git(m.repo, "worktree", "add", "--detach", dir, branch); err != nil {
		os.RemoveAll(tmp)
		return "", err
	}
	m.eph[branch] = dir
	return dir, nil
}

// release removes branch's ephemeral worktree, if it has one. worktree
// remove unregisters AND deletes; RemoveAll mops up the temp parent (and
// the checkout itself if git failed), prune covers the inverse failure —
// a deleted directory whose registration survived.
func (m *sourceMgr) release(branch string) {
	dir, ok := m.eph[branch]
	if !ok {
		return
	}
	delete(m.eph, branch)
	git(m.repo, "worktree", "remove", "--force", dir)
	os.RemoveAll(filepath.Dir(dir))
	git(m.repo, "worktree", "prune")
}

// cleanup releases everything — the exit path.
func (m *sourceMgr) cleanup() {
	for branch := range m.eph {
		m.release(branch)
	}
}

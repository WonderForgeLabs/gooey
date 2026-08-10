package main

// The parsing tests run against fixture strings — the porcelain formats
// are documented and stable, and a fixture pins what we rely on. The
// lifecycle tests run against a real repository built in t.TempDir: a
// THROWAWAY repo is the only honest way to test `git worktree add`, and
// the only kind of repository these tests are allowed to mutate.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseWorktreesPorcelain(t *testing.T) {
	fixture := "worktree /home/u/repo\n" +
		"HEAD 1111111111111111111111111111111111111111\n" +
		"branch refs/heads/main\n" +
		"\n" +
		"worktree /home/u/repo/.claude/worktrees/feat\n" +
		"HEAD 2222222222222222222222222222222222222222\n" +
		"branch refs/heads/feat/thing\n" +
		"locked reason with spaces\n" +
		"\n" +
		"worktree /tmp/detached-tree\n" +
		"HEAD 3333333333333333333333333333333333333333\n" +
		"detached\n" +
		"\n" +
		"worktree /srv/bare.git\n" +
		"bare\n"

	got := parseWorktrees(fixture)
	if len(got) != 4 {
		t.Fatalf("parsed %d entries, want 4: %+v", len(got), got)
	}
	if got[0].path != "/home/u/repo" || got[0].branch != "main" || got[0].detached || got[0].bare {
		t.Fatalf("entry 0 = %+v", got[0])
	}
	if got[1].branch != "feat/thing" {
		t.Fatalf("entry 1 branch = %q, want feat/thing (locked line must not derail it)", got[1].branch)
	}
	if !got[2].detached || got[2].branch != "" || got[2].head == "" {
		t.Fatalf("entry 2 = %+v, want detached with a HEAD", got[2])
	}
	if !got[3].bare {
		t.Fatalf("entry 3 = %+v, want bare", got[3])
	}
}

func TestParseWorktreesTolerates(t *testing.T) {
	if got := parseWorktrees(""); len(got) != 0 {
		t.Fatalf("empty input parsed to %+v", got)
	}
	// No trailing blank line — the last entry still lands.
	got := parseWorktrees("worktree /a\nHEAD 1234\nbranch refs/heads/x")
	if len(got) != 1 || got[0].branch != "x" {
		t.Fatalf("unterminated entry = %+v", got)
	}
	// An attribute with no worktree line is ignored, not attached to a
	// phantom entry.
	if got := parseWorktrees("detached\n\nworktree /b\nHEAD 9\n"); len(got) != 1 || got[0].path != "/b" {
		t.Fatalf("leading stray attribute = %+v", got)
	}
}

func TestParseBranches(t *testing.T) {
	out := "main\x00Fix the flux capacitor\n" +
		"feat/x\x00\n" + // no subject: an unborn or empty-message tip
		"\n" +
		"topic\x00subject with \t tab\n"
	got := parseBranches(out)
	if len(got) != 3 {
		t.Fatalf("parsed %d branches, want 3: %+v", len(got), got)
	}
	if got[0].name != "main" || got[0].subject != "Fix the flux capacitor" {
		t.Fatalf("branch 0 = %+v", got[0])
	}
	if got[1].subject != "" {
		t.Fatalf("branch 1 subject = %q, want empty", got[1].subject)
	}
	if got[2].subject != "subject with \t tab" {
		t.Fatalf("branch 2 = %+v — the NUL separator exists so tabs survive", got[2])
	}
}

func TestSourceIdentity(t *testing.T) {
	branch := source{Name: "feat", Branch: "feat"}
	eph := source{Name: "feat", Branch: "feat", Root: "/tmp/x/tree", Ephemeral: true}
	if branch.id() != eph.id() {
		t.Fatal("a branch must keep its identity across materialization")
	}
	wt := source{Name: "feat", Branch: "feat", Root: "/home/u/wt"}
	if wt.id() == branch.id() {
		t.Fatal("a real worktree on the branch is a different source than the bare branch")
	}
}

// ---- lifecycle, against a throwaway repository ----

// testRepo builds a real repo: main has cmd/alpha, branch "extra" adds
// cmd/beta on top. The working tree ends back on main.
func testRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	repo := t.TempDir()
	mustGit := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	writeDemo := func(dir string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Join(repo, "cmd", dir), 0o755); err != nil {
			t.Fatal(err)
		}
		write(t, repo, filepath.Join("cmd", dir, "main.go"), "// "+dir+"\npackage main\n")
	}
	mustGit("init", "-b", "main")
	writeDemo("alpha")
	mustGit("add", "-A")
	mustGit("commit", "-m", "alpha demo")
	mustGit("switch", "-c", "extra")
	writeDemo("beta")
	mustGit("add", "-A")
	mustGit("commit", "-m", "beta demo on a branch")
	mustGit("switch", "main")
	return repo
}

func TestListSourcesFindsBranchesAndMarksDirty(t *testing.T) {
	repo := testRepo(t)
	srcs := listSources(repo, nil)
	if len(srcs) != 2 {
		t.Fatalf("got %d sources, want 2 (launch + branch): %+v", len(srcs), srcs)
	}
	launch := srcs[0]
	if !launch.Launch || launch.Branch != "main" || launch.Root != repo {
		t.Fatalf("launch source = %+v", launch)
	}
	if launch.Head != "alpha demo" {
		t.Fatalf("launch head subject = %q", launch.Head)
	}
	if launch.Dirty {
		t.Fatal("a fresh checkout reported dirty")
	}
	br := srcs[1]
	if br.Branch != "extra" || br.Root != "" || br.Head != "beta demo on a branch" {
		t.Fatalf("branch source = %+v", br)
	}

	// Dirty means TRACKED modifications; untracked build output must not
	// count, or every tree in a working repo reads dirty forever.
	write(t, repo, filepath.Join("cmd", "alpha", "untracked.txt"), "x")
	if listSources(repo, nil)[0].Dirty {
		t.Fatal("an untracked file marked the tree dirty")
	}
	write(t, repo, filepath.Join("cmd", "alpha", "main.go"), "// edited\npackage main\n")
	if !listSources(repo, nil)[0].Dirty {
		t.Fatal("a tracked edit did not mark the tree dirty")
	}
}

func TestListSourcesWithoutGitStillHasTheLaunchTree(t *testing.T) {
	dir := t.TempDir() // not a repository
	srcs := listSources(dir, nil)
	if len(srcs) != 1 || !srcs[0].Launch {
		t.Fatalf("sources for a non-repo = %+v, want just the launch tree", srcs)
	}
}

func TestEphemeralWorktreeLifecycle(t *testing.T) {
	repo := testRepo(t)
	m := newSourceMgr(repo)

	dir, err := m.materialize("extra")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "cmd", "beta", "main.go")); err != nil {
		t.Fatalf("materialized worktree lacks the branch's demo: %v", err)
	}
	// Detached: the branch itself is NOT checked out, so a real worktree
	// that has it (or wants it) is never blocked or disturbed.
	if out, _ := git(dir, "rev-parse", "--abbrev-ref", "HEAD"); strings.TrimSpace(out) != "HEAD" {
		t.Fatalf("ephemeral worktree is on %q, want a detached HEAD", strings.TrimSpace(out))
	}
	// Idempotent: asking again reuses the same checkout.
	if again, err := m.materialize("extra"); err != nil || again != dir {
		t.Fatalf("second materialize = %q, %v; want the original %q", again, err, dir)
	}

	// The demo list is per-source truth: beta exists on the branch,
	// not on main.
	if ds := scan(scanEnvFor(dir, repo), "browser"); len(ds) != 2 {
		t.Fatalf("branch scan found %d demos, want 2 (alpha+beta): %+v", len(ds), ds)
	}
	if ds := scan(scanEnvFor(repo, repo), "browser"); len(ds) != 1 || ds[0].name != "alpha" {
		t.Fatalf("main scan = %+v, want just alpha", ds)
	}

	// listSources must hide OUR temp worktree from the worktree group —
	// the branch entry represents it — while a real worktree would show.
	srcs := listSources(repo, m.eph)
	for _, s := range srcs {
		if s.Root == dir {
			t.Fatalf("ephemeral worktree leaked into the source list: %+v", s)
		}
	}

	m.release("extra")
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("released worktree still on disk: %v", err)
	}
	out, err := git(repo, "worktree", "list", "--porcelain")
	if err != nil {
		t.Fatal(err)
	}
	if got := len(parseWorktrees(out)); got != 1 {
		t.Fatalf("%d worktrees registered after release, want 1 (the repo itself):\n%s", got, out)
	}
	// Releasing a branch with no worktree is a no-op, not a crash.
	m.release("extra")
	m.Close()
}

func TestSourceMgrCloseRemovesEverything(t *testing.T) {
	repo := testRepo(t)
	m := newSourceMgr(repo)
	dir, err := m.materialize("extra")
	if err != nil {
		t.Fatal(err)
	}
	// Close is the exit path: it must reap ephemeral worktrees even when
	// nothing released them — and it runs the cleanup on the worker, so
	// queued jobs land first.
	ran := false
	m.do(func() { ran = true })
	m.Close()
	if !ran {
		t.Fatal("Close did not drain queued work first")
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("Close left the ephemeral worktree behind: %v", err)
	}
	out, err := git(repo, "worktree", "list", "--porcelain")
	if err != nil {
		t.Fatal(err)
	}
	if got := len(parseWorktrees(out)); got != 1 {
		t.Fatalf("%d worktrees registered after Close, want 1:\n%s", got, out)
	}
}

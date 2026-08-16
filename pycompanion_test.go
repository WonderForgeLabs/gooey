package gooey

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// These cover the pre-Run host-path surface — SourceDir and the Python
// worker launcher — that every app which ships a companion had been
// writing by hand. The App-level ones run a REAL companion over a pty
// like companion_test.go's, because what is being asserted is lifetime:
// the log file is opened when the child starts and released only once
// the child is gone, so no app needs a `defer logFile.Close()` whose
// correctness rests on remembering that Run waits for its companions.

func TestSourceDirPrefersTheWorkingDirectoryWhenTheMarkerIsThere(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "app.gooey"), []byte("<Gooey/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	if got := SourceDir("app.gooey"); got != "." {
		t.Errorf(`SourceDir with the marker present = %q, want "."`, got)
	}

	// No marker: the binary's own directory, which under `go test` is the
	// test binary's temporary home. The value matters less than the
	// switch — what must never happen is silently answering "." for a
	// program launched from somewhere else.
	exe, err := os.Executable()
	if err != nil {
		t.Skip("no os.Executable on this platform")
	}
	if got, want := SourceDir("nosuchpage.gooey"), filepath.Dir(exe); got != want {
		t.Errorf("SourceDir with the marker absent = %q, want %q", got, want)
	}

	// A directory is not a marker file. os.Stat alone would say yes here.
	if err := os.Mkdir(filepath.Join(dir, "dirmarker"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := SourceDir("dirmarker"); got == "." {
		t.Error("a DIRECTORY named like the marker was accepted as the marker")
	}
}

// The one piece of policy this launcher owns: an explicit interpreter
// always wins, and only in its absence does the virtualenv beside the
// script beat the bare name.
func TestPythonWorkerInterpreterPrefersTheVenv(t *testing.T) {
	dir := t.TempDir()
	venv := filepath.Join(dir, ".venv", "bin", "python")

	for _, tc := range []struct {
		name   string
		python string
		want   string
	}{
		{"no venv, empty means the default", "", DefaultPython},
		{"no venv, the default named explicitly", DefaultPython, DefaultPython},
		{"no venv, explicit", "/usr/bin/python3.13", "/usr/bin/python3.13"},
	} {
		if got := (PythonWorker{Dir: dir, Python: tc.python}).Interpreter(); got != tc.want {
			t.Errorf("%s: Interpreter() = %q, want %q", tc.name, got, tc.want)
		}
	}

	if err := os.MkdirAll(filepath.Dir(venv), 0o755); err != nil {
		t.Fatal(err)
	}
	// The venv's bin/ exists but the interpreter does not yet: a
	// half-built venv must not be preferred.
	if got := (PythonWorker{Dir: dir}).Interpreter(); got != DefaultPython {
		t.Errorf("a venv with no interpreter in it was preferred: %q", got)
	}
	if err := os.WriteFile(venv, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name   string
		python string
		want   string
	}{
		{"venv wins over the bare default", "", venv},
		{"venv wins over an explicit default", DefaultPython, venv},
		{"an explicit interpreter beats the venv", "/usr/bin/python3.13", "/usr/bin/python3.13"},
	} {
		if got := (PythonWorker{Dir: dir, Python: tc.python}).Interpreter(); got != tc.want {
			t.Errorf("%s: Interpreter() = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestPythonWorkerLogPathIsRelativeToTheWorker(t *testing.T) {
	w := PythonWorker{Dir: filepath.Join("apps", "kanban", "worker"), Log: "worker.log"}
	if got, want := w.LogPath(), filepath.Join("apps", "kanban", "worker", "worker.log"); got != want {
		t.Errorf("LogPath() = %q, want %q", got, want)
	}
	if got := (PythonWorker{Dir: "x"}).LogPath(); got != "" {
		t.Errorf(`LogPath() with no Log = %q, want "" (os.DevNull)`, got)
	}
}

// The teardown assertion, and the reason this constructor exists rather
// than forty lines in each app's main: after Run returns, the child is
// gone AND the log descriptor is released — in that order, because the
// release happens after CompanionCmd's Wait, which is fed from cmd.Wait.
// A close that merely signalled would show up here as a live process.
func TestPythonCompanionReleasesItsLogOnlyAfterTheChildIsGone(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh")
	}
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "grandchild.pid")
	script := "echo started\n" +
		"echo inherited=$GOOEY_TEST_INHERITED\n" +
		"echo declared=$GOOEY_TEST_DECLARED\n" +
		"sleep 300 & echo $! > " + pidFile + "\n" +
		"wait\n"
	if err := os.WriteFile(filepath.Join(dir, "worker.sh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	// Stale content from a previous run: the log is truncated, not
	// appended to, which is what both apps did by hand.
	if err := os.WriteFile(filepath.Join(dir, "worker.log"), []byte("stale\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOOEY_TEST_INHERITED", "yes")

	c := PythonCompanion(PythonWorker{
		Name: "worker",
		Dir:  dir,
		// sh stands in for python: an explicit interpreter is used
		// verbatim, which is the same path a -worker-python flag takes.
		Python:    "sh",
		Script:    "worker.sh",
		Log:       "worker.log",
		Env:       []string{"GOOEY_TEST_DECLARED=1"},
		KillDelay: 500 * time.Millisecond,
	})
	pc := c.(*pyCompanion)
	app, tty := companionApp(t, &eventLog{}, WithCompanions(c), WithCompanionGrace(0))

	done := start(t, app)
	tty.waitForFrame(t)
	grandchild := waitForPID(t, pidFile)

	tty.send("\x03")
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the run loop did not stop")
	}

	// Everything below reads state written on the supervisor goroutine.
	// It is ordered by Run's own teardown: stopCompanions receives on the
	// channel the supervisor sends to after Wait returned, so Run
	// returning is a happens-before edge for all of it.
	if pc.log != nil {
		t.Error("the worker log was still open after Run returned")
	}
	child := pc.inner.(*cmdCompanion).Process().Pid
	for _, p := range []struct {
		pid  int
		what string
	}{{child, "worker"}, {grandchild, "backgrounded grandchild"}} {
		if !waitForDeath(p.pid) {
			t.Errorf("the %s (pid %d) outlived the app", p.what, p.pid)
		}
	}
	if app.CompanionLeaked() {
		t.Error("the tripwire fired on a companion that stopped properly")
	}

	b, err := os.ReadFile(filepath.Join(dir, "worker.log"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	if strings.Contains(got, "stale") {
		t.Errorf("the log was appended to rather than truncated: %q", got)
	}
	for _, want := range []string{"started", "inherited=yes", "declared=1"} {
		if !strings.Contains(got, want) {
			t.Errorf("the log does not contain %q; it holds %q", want, got)
		}
	}
}

// The same contract without an App around it, stated as sharply as it
// can be: when Wait returns the process is ALREADY REAPED — a stop that
// merely signalled would return here with the child still alive — and
// the log file is ALREADY CLOSED, which the second Close reports by
// refusing. This is the assertion that has to survive any future rework
// of the launcher.
func TestPythonCompanionWaitJoinsTheChildThenClosesTheLog(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "worker.sh"), []byte("sleep 300\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	c := PythonCompanion(PythonWorker{
		Name:      "worker",
		Dir:       dir,
		Python:    "sh",
		Script:    "worker.sh",
		Log:       "worker.log",
		KillDelay: 500 * time.Millisecond,
	})
	pc := c.(*pyCompanion)

	ctx, cancel := context.WithCancel(context.Background())
	if err := c.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	pid := pc.inner.(*cmdCompanion).Process().Pid
	if !alive(pid) {
		t.Fatalf("the worker (pid %d) was never running", pid)
	}
	logFile := pc.log // the handle Wait is about to close

	cancel()
	if err := c.Wait(); err != nil {
		t.Logf("the worker exited with %v, which a signalled shell may", err)
	}

	if alive(pid) {
		t.Errorf("Wait returned while the worker (pid %d) was still alive", pid)
	}
	if err := logFile.Close(); !errors.Is(err, os.ErrClosed) {
		t.Errorf("closing the log a second time returned %v, want os.ErrClosed — Wait did not close it", err)
	}
}

// A log that cannot be opened is a start failure like any other: Run
// says so on a cooked terminal and never takes the screen.
func TestPythonCompanionUnopenableLogFailsBeforeRawMode(t *testing.T) {
	dir := t.TempDir()
	c := PythonCompanion(PythonWorker{
		Name:   "worker",
		Dir:    dir,
		Python: "sh",
		Script: "worker.sh",
		Log:    filepath.Join("nosuchdir", "worker.log"),
	})
	app, tty := companionApp(t, &eventLog{}, WithCompanions(c))

	err := <-start(t, app)

	var ce *CompanionError
	if !errors.As(err, &ce) || ce.Phase != PhaseStart || ce.Name != "worker" {
		t.Fatalf("Run returned %v, want the worker's start failure", err)
	}
	if !strings.Contains(ce.Error(), filepath.Join(dir, "nosuchdir", "worker.log")) {
		t.Errorf("the error does not name the log it could not open: %v", ce)
	}
	if tty.openCount() != 0 {
		t.Error("a worker that never started still cost a screen")
	}
}

// The other end of the log's lifetime: a start that fails AFTER the log
// was opened must not strand the descriptor. Nothing calls Wait on a
// companion whose Start returned an error, so Start closes it itself.
func TestPythonCompanionStartFailureReleasesTheLog(t *testing.T) {
	fds := func() int {
		ents, err := os.ReadDir("/proc/self/fd")
		if err != nil {
			t.Skip("no /proc/self/fd to count descriptors with")
		}
		return len(ents)
	}
	dir := t.TempDir()
	c := PythonCompanion(PythonWorker{
		Name:   "worker",
		Dir:    dir,
		Python: "gooey-no-such-interpreter",
		Script: "worker.py",
		Log:    "worker.log",
	})
	pc := c.(*pyCompanion)

	before := fds()
	if err := c.Start(context.Background()); err == nil {
		t.Fatal("Start succeeded with an interpreter that does not exist")
	}
	if after := fds(); after != before {
		t.Errorf("a failed start left %d descriptor(s) open", after-before)
	}
	if pc.log != nil {
		t.Error("the log file survived a failed start")
	}
	if _, err := os.Stat(filepath.Join(dir, "worker.log")); err != nil {
		t.Errorf("the log was never created, so the failure was not the one under test: %v", err)
	}
}

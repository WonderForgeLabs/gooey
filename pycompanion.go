package gooey

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// PythonWorker is the companion this repo actually keeps writing: a
// Python program that lives beside the app that needs it, run with the
// interpreter from the virtualenv sitting next to its script, its output
// captured to a log file rather than to the terminal the UI owns.
//
//	app.AddCompanion(gooey.PythonCompanion(gooey.PythonWorker{
//	    Name:   "temporal-worker",
//	    Dir:    filepath.Join(dir, "worker"),
//	    Script: "worker.py",
//	    Python: *workerPython,
//	    Log:    "kanban-worker.log",
//	    Env:    []string{"GOOEY_MCP_URL=" + mcpURL},
//	}))
//
// It is a description, not a process: nothing is opened, resolved or
// launched until the App starts the companion it produces. That is what
// makes it safe to build one on a path that may never Run — an app that
// exits before Run has not truncated anybody's log.
//
// Every field is optional except Name, Dir and Script, which is the
// point: the two demos that motivated this differed only in their
// directory, their log name and their environment, and neither should
// have to pass an empty argument for the rest.
type PythonWorker struct {
	// Name labels the companion in errors (*CompanionError). It is not a
	// key; nothing looks a companion up by it.
	Name string
	// Dir is the worker's working directory, as an OS path. It is also
	// what .venv and Log resolve against — a worker's interpreter and its
	// log belong beside its source, not beside whatever the shell's cwd
	// happened to be. Resolve it from SourceDir, not from ".".
	Dir string
	// Script is the Python file, relative to Dir: argv[1].
	Script string
	// Args are the arguments after Script.
	Args []string
	// Python is the interpreter. Empty or DefaultPython means "no
	// preference stated" and lets a virtualenv beside the script win; see
	// Interpreter for the whole rule.
	Python string
	// Log is a file, relative to Dir, that the child's stdout and stderr
	// are truncated into. It is opened when the child starts and closed
	// once the child is gone. Empty sends the output to os.DevNull, which
	// is CompanionCmd's own default and the only other safe destination:
	// a child writing to the inherited stdout paints over a raw-mode UI.
	Log string
	// Env are "NAME=VALUE" entries applied on top of the app's own
	// environment, later entries winning, which is how os/exec reads a
	// duplicated name. Inheriting is deliberate: a worker usually needs
	// the ANTHROPIC_API_KEY or TEMPORAL_ADDRESS already exported in the
	// shell that launched the app.
	Env []string
	// KillDelay is the grace between the polite stop signal and SIGKILL
	// (0 = CompanionCmd's default, five seconds).
	KillDelay time.Duration
}

// DefaultPython is the interpreter name a `-worker-python` flag defaults
// to, and the value PythonWorker.Python reads as "no preference stated".
const DefaultPython = "python3"

// Interpreter reports the interpreter this worker will actually run.
//
// The rule is one line of policy and it is the reason this helper exists
// at all: an explicit choice always wins, and only in its absence does
// `<Dir>/.venv/bin/python` beat the bare name. A worker's dependencies
// (temporalio, mcp, an SDK) are installed in that virtualenv and system
// python3 almost never has them — and a companion that cannot import its
// dependencies exits, which takes the whole app down with it. Preferring
// the venv turns the single commonest way to misconfigure one of these
// demos into a non-event.
//
// The venv layout it knows is the POSIX one, which is what these workers
// ship. A Windows virtualenv (`.venv\Scripts\python.exe`) is named
// explicitly through Python rather than guessed at here.
func (w PythonWorker) Interpreter() string {
	py := w.Python
	if py == "" {
		py = DefaultPython
	}
	if py != DefaultPython {
		return py // the caller stated one; that is the end of it
	}
	if venv := filepath.Join(w.Dir, ".venv", "bin", "python"); fileExists(venv) {
		return venv
	}
	return py
}

// LogPath reports where this worker's output will go, or "" for
// os.DevNull. It is the string an app puts on screen ("worker log at …"),
// which is why it is derived here rather than re-joined at every call
// site that mentions it.
func (w PythonWorker) LogPath() string {
	if w.Log == "" {
		return ""
	}
	return filepath.Join(w.Dir, w.Log)
}

// PythonCompanion turns a PythonWorker into a Companion: CompanionCmd
// with the interpreter chosen, the environment merged and the log file's
// lifetime tied to the child's.
//
// It adds no teardown machinery of its own, and that is the whole design.
// Stopping is still exactly CompanionCmd's — cancel the context, signal
// the process GROUP, escalate to SIGKILL after KillDelay, and report
// through Wait — so the doctrine that a stop closes AND joins is
// asserted in one place instead of once per app. What this constructor
// owns is the log file, and it owns it on the same terms: opened when the
// child starts, closed only after Wait has returned, which is only after
// the process is gone. An app therefore never needs a `defer
// logFile.Close()` whose correctness depends on remembering that Run
// waits for its companions.
//
// Errors arrive through Start, like every other companion's: a log that
// cannot be opened or an interpreter that is not there is a
// *CompanionError{Phase: PhaseStart}, which Run returns before it takes
// the screen.
func PythonCompanion(w PythonWorker) Companion {
	return &pyCompanion{w: w}
}

// pyCompanion is a thin owner of the log file wrapped around a
// cmdCompanion. It exists so that "the log outlives the child by exactly
// nothing" is a property of the companion rather than of an app's defer
// ordering.
type pyCompanion struct {
	w PythonWorker

	// inner and log are written by Start and read by Wait. Start returns
	// before the App launches the supervisor goroutine that calls Wait,
	// so that hand-off is ordered by the App's own sequencing and needs
	// no lock; on the failure path Start closes the log itself, before
	// any supervisor exists.
	inner Companion
	log   *os.File
}

func (c *pyCompanion) Name() string { return c.w.Name }

func (c *pyCompanion) Start(ctx context.Context) error {
	if c.w.Script == "" {
		return errors.New("no script")
	}
	opts := []CmdOption{}
	if path := c.w.LogPath(); path != "" {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			return fmt.Errorf("cannot open the worker log %s: %w", path, err)
		}
		c.log = f
		opts = append(opts, CompanionOutput(f))
	}
	if c.w.KillDelay > 0 {
		opts = append(opts, CompanionKillDelay(c.w.KillDelay))
	}

	cmd := exec.Command(c.w.Interpreter(), append([]string{c.w.Script}, c.w.Args...)...)
	cmd.Dir = c.w.Dir
	if len(c.w.Env) > 0 {
		cmd.Env = append(os.Environ(), c.w.Env...)
	}
	c.inner = CompanionCmd(c.w.Name, cmd, opts...)
	if err := c.inner.Start(ctx); err != nil {
		c.closeLog()
		return err
	}
	return nil
}

// Wait joins the child and only then releases the log. The order is the
// contract: CompanionCmd's Wait returns after the process has exited (it
// is fed from cmd.Wait, on both the polite and the SIGKILL path), so
// there is no writer left when the file closes.
func (c *pyCompanion) Wait() error {
	if c.inner == nil {
		return errors.New("companion was not started")
	}
	err := c.inner.Wait()
	c.closeLog()
	return err
}

func (c *pyCompanion) closeLog() {
	if c.log != nil {
		c.log.Close()
		c.log = nil
	}
}

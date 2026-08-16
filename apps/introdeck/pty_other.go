//go:build !linux

package main

// The non-Linux half of the pty, which exists so that <Terminal> is a
// component that FAILS on your platform rather than a package that does
// not compile on it.
//
// pty_linux.go is three ioctls against /dev/ptmx: TIOCSPTLCK, TIOCGPTN
// and TIOCSWINSZ, plus /dev/pts/N. None of those spellings exist on
// darwin or the BSDs (posix_openpt and grantpt/ptsname are the portable
// route) and none of it exists on Windows at all. Porting is a real
// piece of work and nobody has needed it; that is a fine answer.
//
// What is not a fine answer is `undefined: openPty`. main.go's package
// doc says `cd apps/introdeck && go run .`, and a contributor on a Mac
// following it got a compiler error naming a symbol, in a file they had
// not opened, with no statement anywhere that this is Linux-only. The
// build tag on pty_linux.go was doing its job perfectly and the job was
// the wrong one.
//
// So openPty compiles everywhere and returns an error off Linux. Start
// already has that path — `t.fail.Set("pty: " + err.Error())` — and it
// renders on the island, which means the deck runs, every non-Terminal
// slide works, and the ones that host a guest say what is missing.
//
// The other methods are here only to satisfy the compiler. Nothing
// reaches them: openPty is the sole constructor and it never returns a
// non-nil *pty on this build.

import (
	"errors"
	"os"
	"os/exec"
	"runtime"
)

type pty struct {
	master *os.File
	slave  *os.File
}

func openPty(cols, rows int) (*pty, error) {
	return nil, errors.New("no pty on " + runtime.GOOS +
		": <Terminal> is implemented with Linux's /dev/ptmx ioctls (pty_linux.go)")
}

func (p *pty) resize(cols, rows int) error     { return nil }
func (p *pty) start(string) (*exec.Cmd, error) { return nil, errors.New("no pty") }
func (p *pty) Close()                          {}

package main

// A real pty, in about forty lines of syscall.
//
// The first version of <Terminal> borrowed `script -qec` as its pty,
// which is the recipe docs/learn/howto/howto-testing.md uses and which
// works fine for a program you never resize. It cannot do the thing this
// deck actually needs: the master fd stays inside `script`, so there is
// nothing to call TIOCSWINSZ on, and a hosted vim in a window that just
// changed size has no way to find out.
//
// So the pty is opened here. Three ioctls and an open:
//
//	/dev/ptmx  →  TIOCSPTLCK(0)  →  TIOCGPTN  →  /dev/pts/N
//
// TIOCSWINSZ on the master then resizes the terminal and the kernel
// delivers SIGWINCH to the foreground process group for free, which is
// exactly what a hosted full-screen app needs and what `script` could
// not give us.
//
// Linux only, deliberately: this is an example, the ioctl numbers are
// architecture-specific, and a portable pty is a dependency rather than
// a file.

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"unsafe"
)

// The two ioctls the standard library does not name. TIOCSWINSZ it does.
const (
	tiocsptlck = 0x40045431 // unlock the slave
	tiocgptn   = 0x80045430 // read the slave's number
)

type winsize struct {
	rows, cols, x, y uint16
}

// pty is an opened pair. The slave is kept open in the parent on
// purpose: without a keeper, the master returns EIO the instant the
// child closes its own copy, and a guest that exits would take the
// reader down before its last frame had been read.
type pty struct {
	master *os.File
	slave  *os.File
}

func openPty(cols, rows int) (*pty, error) {
	// O_NONBLOCK is not a performance flag here, it is the difference
	// between a stop func that returns and one that wedges the UI
	// goroutine forever.
	//
	// os.OpenFile without it hands back a file in BLOCKING mode, outside
	// Go's netpoller. A goroutine parked in Read on such a file cannot be
	// interrupted: Close does not cancel it and SetReadDeadline fails
	// with ErrNoDeadline. So the reader never returns, the stop func
	// never gets past its join — and because stop is called from the UI
	// goroutine, the whole app stops drawing and stops answering MCP,
	// while still holding its port. It looks exactly like a hang with no
	// crash, which is what it is.
	//
	// With O_NONBLOCK, os routes the file through the poller (kindNonBlock),
	// Read parks in the runtime rather than in the kernel, and Close
	// unblocks it. This is the same lesson as the Fd() trap in
	// docs/specs/2026-08-10-tty-read-lifecycle.md, arrived at from the
	// other direction: there the file was pollable until Fd() took it out
	// of the poller; here it was never in it.
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, fmt.Errorf("open /dev/ptmx: %w", err)
	}

	var unlock int32
	if err := ioctl(master, tiocsptlck, uintptr(unsafe.Pointer(&unlock))); err != nil {
		master.Close()
		return nil, fmt.Errorf("unlock: %w", err)
	}
	var n uint32
	if err := ioctl(master, tiocgptn, uintptr(unsafe.Pointer(&n))); err != nil {
		master.Close()
		return nil, fmt.Errorf("ptsname: %w", err)
	}

	slave, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", n), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		master.Close()
		return nil, fmt.Errorf("open slave: %w", err)
	}

	p := &pty{master: master, slave: slave}
	if err := p.resize(cols, rows); err != nil {
		p.Close()
		return nil, err
	}
	return p, nil
}

// resize sets the window size and lets the kernel deliver SIGWINCH. The
// signal is the whole reason this exists: a full-screen guest redraws on
// it, so the island reflows without anything here knowing how.
func (p *pty) resize(cols, rows int) error {
	if cols <= 0 || rows <= 0 {
		return nil
	}
	ws := winsize{rows: uint16(rows), cols: uint16(cols)}
	return ioctl(p.master, syscall.TIOCSWINSZ, uintptr(unsafe.Pointer(&ws)))
}

// start runs cmdline under this pty with its own session, so the guest
// is a real foreground process group and job control works inside it.
func (p *pty) start(cmdline string) (*exec.Cmd, error) {
	cmd := exec.Command("sh", "-c", cmdline)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = p.slave, p.slave, p.slave
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid:  true,
		Setctty: true,
		Ctty:    0, // the slave, as fd 0 in the child
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return cmd, nil
}

func (p *pty) Close() {
	if p.slave != nil {
		p.slave.Close()
	}
	if p.master != nil {
		p.master.Close()
	}
}

// ioctl routes through SyscallConn rather than Fd(). Calling Fd() puts
// the file into blocking mode and drops it from the netpoller, after
// which a pending Read is an uninterruptible syscall that Close cannot
// cancel — the bug recorded in docs/specs/2026-08-10-tty-read-lifecycle.md
// and the reason term.Screen does the same thing.
func ioctl(f *os.File, req, arg uintptr) error {
	conn, err := f.SyscallConn()
	if err != nil {
		return err
	}
	var ioerr error
	err = conn.Control(func(fd uintptr) {
		if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, fd, req, arg); e != 0 {
			ioerr = e
		}
	})
	if err != nil {
		return err
	}
	return ioerr
}

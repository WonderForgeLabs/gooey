//go:build unix

package term

import (
	"syscall"
	"unsafe"
)

// InForeground reports whether this process's group owns the terminal —
// whether it is the group the tty driver sends ctrl+c and ctrl+z to.
//
// It exists to make suspending safe. The classic ctrl+z dance restores
// the terminal, puts SIGTSTP back to its default disposition and
// re-raises it, expecting the process to stop there. In an ORPHANED
// process group — nohup, a supervisor, a CI runner, a `go test` binary —
// POSIX says a stop signal is not honored, so the raise does not stop
// anything and the signal comes back to the handler the moment it
// re-registers. That is an infinite restore/re-acquire loop with the UI
// flickering through it, and it is not hypothetical: it is what this
// framework's own test suite did the first time the dance was written.
//
// A false answer means "do not try to stop" — either we are in the
// background already or there is no job control here to speak of.
func (s *Screen) InForeground() bool {
	var pgrp int32
	err := s.control(func(fd int) error {
		_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd),
			uintptr(syscall.TIOCGPGRP), uintptr(unsafe.Pointer(&pgrp)))
		if errno != 0 {
			return errno
		}
		return nil
	})
	if err != nil {
		return false
	}
	return int(pgrp) == syscall.Getpgrp()
}

// CanSuspend reports whether a SIGTSTP raised at this process would
// actually stop it. Both conditions are necessary, and each corresponds
// to a configuration that was observed failing:
//
//  1. We own the terminal (InForeground). A `go test` binary does not,
//     and its stop signals are discarded.
//  2. We are not the session leader. A session leader's process group is
//     orphaned BY DEFINITION — no member can have a parent in a
//     different group of the same session — so it can never be stopped.
//     This is not exotic: `script`, which is how this project records
//     every demo, runs its child exactly that way.
//
// Where it answers false, ctrl+z must be declined rather than attempted:
// an unhonored raise leaves the signal pending, and the handler that
// re-registers it gets it straight back, restoring and re-acquiring the
// terminal in a loop.
func (s *Screen) CanSuspend() bool {
	if !s.InForeground() {
		return false
	}
	// getsid(0) via the raw syscall: the syscall package wraps it on some
	// unixes and not on others.
	sid, _, errno := syscall.Syscall(syscall.SYS_GETSID, 0, 0, 0)
	return errno == 0 && int(sid) != syscall.Getpgrp()
}

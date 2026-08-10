//go:build unix

package exechandlers

import (
	"os"
	"os/exec"
	"syscall"
)

// Process-group care, the Unix half — the same discipline as the root
// module's companion_unix.go (a separate module cannot share the
// code, so it shares the record: docs/specs/2026-08-10-companions.md).
//
// The child goes into its own process group so that killing it kills
// what IT started: interpreters and wrappers fork, and signalling the
// direct child alone leaves grandchildren holding pipes — which would
// also keep our Wait from returning. The negative pid is the group.
// Being in its own group also keeps the child out of the terminal's
// foreground group: it can never read the tty gooey is running raw.

func setProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// terminateProcess asks the child's whole group to stop, falling back
// to the process alone if the group is already gone.
func terminateProcess(p *os.Process) {
	if p == nil {
		return
	}
	if err := syscall.Kill(-p.Pid, syscall.SIGTERM); err != nil {
		p.Signal(syscall.SIGTERM)
	}
}

// killProcess is the escalation: SIGKILL to the group.
func killProcess(p *os.Process) {
	if p == nil {
		return
	}
	if err := syscall.Kill(-p.Pid, syscall.SIGKILL); err != nil {
		p.Kill()
	}
}

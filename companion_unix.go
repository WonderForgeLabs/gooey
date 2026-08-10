//go:build unix

package gooey

import (
	"os"
	"os/exec"
	"syscall"
)

// Process-group care, the Unix half.
//
// A companion child is put in its own process group so that stopping it
// stops what IT started. Dev servers, workers and language runtimes
// routinely fork; signalling the direct child alone leaves the
// grandchildren running, still holding the port the next run needs. The
// negative pid in Kill is the group.
//
// The group is also why the app's own ctrl+c does not reach the child
// twice: a child in the foreground group would get the tty driver's
// SIGINT as well as ours. Here it gets exactly what teardown sends it.

func setProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// terminateProcess asks the child's whole group to stop. It falls back
// to the process alone if the group is gone — which happens when the
// child already exited and reaping is in flight.
func terminateProcess(p *os.Process) error {
	if p == nil {
		return nil
	}
	if err := syscall.Kill(-p.Pid, syscall.SIGTERM); err != nil {
		return p.Signal(syscall.SIGTERM)
	}
	return nil
}

// killProcess is the escalation, and it is aimed at the group for the
// same reason: a child that ignored SIGTERM has almost certainly left
// something else ignoring it too.
func killProcess(p *os.Process) error {
	if p == nil {
		return nil
	}
	if err := syscall.Kill(-p.Pid, syscall.SIGKILL); err != nil {
		return p.Kill()
	}
	return nil
}

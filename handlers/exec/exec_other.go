//go:build !unix

package exechandlers

import (
	"os"
	"os/exec"
)

// Process groups, where the platform has no equivalent worth
// pretending to (the root module's companion_other.go makes the same
// call). The child is signalled directly; its own children are its own
// business — a real difference from the Unix build, not a papered-over
// one.

func setProcessGroup(*exec.Cmd) {}

func terminateProcess(p *os.Process) {
	if p == nil {
		return
	}
	p.Signal(os.Interrupt)
}

func killProcess(p *os.Process) {
	if p == nil {
		return
	}
	p.Kill()
}

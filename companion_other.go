//go:build !unix

package gooey

import (
	"os"
	"os/exec"
)

// Process groups, where the platform has no equivalent worth pretending
// to. A companion child is signalled directly; its own children are its
// own business, which is a real difference from the Unix build and the
// reason CompanionCmd's doc says "process group" rather than promising
// it everywhere.

func setProcessGroup(*exec.Cmd) {}

func terminateProcess(p *os.Process) error {
	if p == nil {
		return nil
	}
	return p.Signal(os.Interrupt)
}

func killProcess(p *os.Process) error {
	if p == nil {
		return nil
	}
	return p.Kill()
}

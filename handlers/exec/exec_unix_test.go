//go:build unix

package exechandlers_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	exechandlers "github.com/WonderForgeLabs/gooey/handlers/exec"
)

// A child that ignores SIGTERM is SIGKILLed after the grace window —
// the escalation half of the companions discipline.
func TestStubbornChildIsEscalatedToSigkill(t *testing.T) {
	register(t, []exechandlers.Command{
		{Name: "stubborn", Path: helper, Args: []string{"stubborn"}, Timeout: 200 * time.Millisecond},
	}, exechandlers.WithKillDelay(300*time.Millisecond))
	h := build(t, page("{{sys:Run `stubborn` | into .Out}}"), nil)
	start := time.Now()
	h.clickAndSettle()
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("delivery took %s; SIGKILL never landed", elapsed)
	}
	if got := h.out.Get(); !strings.Contains(got, "timed out") {
		t.Fatalf("out=%q, want a timeout ERROR", got)
	}
}

// The timeout kills the whole process GROUP: a grandchild backgrounded
// by the direct child dies too, instead of surviving to hold pipes and
// ports. The host registering /bin/sh here is the host's prerogative —
// markup still only ever names the registered entry; nothing markup
// supplies is shell-interpreted.
func TestTimeoutKillsTheWholeGroup(t *testing.T) {
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("no /bin/sh on this host")
	}
	pidfile := filepath.Join(t.TempDir(), "pid")
	script := fmt.Sprintf("sleep 30 & echo $! > %s; wait", pidfile)
	register(t, []exechandlers.Command{
		{Name: "spawner", Path: "/bin/sh", Args: []string{"-c", script}, Timeout: 300 * time.Millisecond},
	}, exechandlers.WithKillDelay(300*time.Millisecond))

	h := build(t, page("{{sys:Run `spawner` | into .Out}}"), nil)
	h.clickAndSettle()
	if got := h.out.Get(); !strings.Contains(got, "timed out") {
		t.Fatalf("out=%q, want a timeout ERROR", got)
	}

	raw, err := os.ReadFile(pidfile)
	if err != nil {
		t.Fatalf("the spawner never wrote its child's pid: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatalf("pidfile holds %q: %v", raw, err)
	}
	// The kill is delivered by the time the ERROR lands, but give the
	// kernel a beat to reap before declaring the grandchild immortal.
	deadline := time.Now().Add(2 * time.Second)
	for {
		if err := syscall.Kill(pid, 0); err == syscall.ESRCH {
			return // the backgrounded sleep is gone — the group died
		}
		if time.Now().After(deadline) {
			syscall.Kill(pid, syscall.SIGKILL) // do not leak it from the test
			t.Fatalf("grandchild %d survived the group kill", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

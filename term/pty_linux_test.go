package term

import (
	"os"
	"syscall"
	"testing"
	"unsafe"
)

// openPTY allocates a pty pair without a third-party dependency: the
// three ioctls behind every pty library. Tests that need a REAL terminal
// (termios, window size, a device that blocks on read) use it; anything
// that only needs a blocking file descriptor uses os.Pipe instead.
func openPTY(t *testing.T) (master, slave *os.File) {
	t.Helper()
	m, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		t.Skipf("no /dev/ptmx: %v", err)
	}
	var unlock int32
	if err := ioctl(m, syscall.TIOCSPTLCK, uintptr(unsafe.Pointer(&unlock))); err != nil {
		m.Close()
		t.Skipf("TIOCSPTLCK: %v", err)
	}
	var n int32
	if err := ioctl(m, syscall.TIOCGPTN, uintptr(unsafe.Pointer(&n))); err != nil {
		m.Close()
		t.Skipf("TIOCGPTN: %v", err)
	}
	s, err := os.OpenFile("/dev/pts/"+itoa(int(n)), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		m.Close()
		t.Skipf("open pts: %v", err)
	}
	t.Cleanup(func() { s.Close(); m.Close() })
	return m, s
}

// ioctl goes through SyscallConn for the same reason the package does:
// os.File.Fd() would detach the file from the poller and quietly change
// the behavior the test is measuring.
func ioctl(f *os.File, req, arg uintptr) error {
	c, err := f.SyscallConn()
	if err != nil {
		return err
	}
	var errno syscall.Errno
	cerr := c.Control(func(fd uintptr) {
		_, _, errno = syscall.Syscall(syscall.SYS_IOCTL, fd, req, arg)
	})
	if cerr != nil {
		return cerr
	}
	if errno != 0 {
		return errno
	}
	return nil
}

type winsize struct{ rows, cols, xpix, ypix uint16 }

// setSize resizes the pty from the master side, which is what a terminal
// emulator does when its window changes.
func setSize(t *testing.T, master *os.File, cols, rows int) {
	t.Helper()
	ws := winsize{rows: uint16(rows), cols: uint16(cols)}
	if err := ioctl(master, syscall.TIOCSWINSZ, uintptr(unsafe.Pointer(&ws))); err != nil {
		t.Fatalf("TIOCSWINSZ: %v", err)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [8]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

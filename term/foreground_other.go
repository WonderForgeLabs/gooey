//go:build !unix

package term

// InForeground has no meaning without POSIX job control, and the App
// never tries to suspend on such a platform. Answering false keeps the
// one caller on its safe path.
func (s *Screen) InForeground() bool { return false }

// CanSuspend is false without POSIX job control: there is nothing to
// suspend to.
func (s *Screen) CanSuspend() bool { return false }

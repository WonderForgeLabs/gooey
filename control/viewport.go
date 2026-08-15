package control

import (
	"fmt"

	"github.com/WonderForgeLabs/gooey/internal/viewport"
)

// Resize re-targets the running app at a new viewport size, the act a
// host with no terminal needs: SIGWINCH never fires without a tty, so
// without this an app's size is fixed for its whole life at whatever
// WithSize or the opening ioctl said.
//
// On a tty-backed app the change is ADVISORY — it takes effect
// immediately, and the next SIGWINCH replaces it with the terminal's real
// size. That is deliberate: refusing the act on a tty would make the same
// call succeed or fail depending on how the host was started, and a
// caller watching the Resized lifecycle event sees the correction either
// way.
func (s *Service) Resize(cols, rows int) error {
	if cols <= 0 || rows <= 0 {
		return fmt.Errorf("control: resize to %dx%d: a viewport must be positive", cols, rows)
	}
	return viewport.Resize(s.host, cols, rows)
}

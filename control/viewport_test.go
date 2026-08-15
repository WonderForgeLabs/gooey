package control

import (
	"strings"
	"testing"
)

// A size an app cannot paint into is rejected at the act, before it
// reaches the framework — a zero or negative viewport would allocate an
// empty buffer and the app would go black with no error, which is the
// failure mode a forced graphics protocol already taught this repo once.
func TestResizeRejectsANonPositiveSize(t *testing.T) {
	svc, _ := testService(nil)

	for _, tc := range []struct{ cols, rows int }{{0, 20}, {60, 0}, {-1, 20}, {60, -1}} {
		if err := svc.Resize(tc.cols, tc.rows); err == nil {
			t.Errorf("Resize(%d, %d) succeeded; want an error", tc.cols, tc.rows)
		}
	}
}

// The host here owns a composer but no App, which control.NewService
// accepts by design. Refusing with a legible error is the contract — not
// a panic, and not a silent no-op that would leave a caller believing it
// had resized something.
func TestResizeRefusesAHostThatOwnsNoApp(t *testing.T) {
	svc, _ := testService(nil)

	err := svc.Resize(60, 20)
	if err == nil {
		t.Fatal("Resize on a host with no App succeeded; want an error")
	}
	if !strings.Contains(err.Error(), "resize") {
		t.Errorf("error %q does not mention resize", err)
	}
}

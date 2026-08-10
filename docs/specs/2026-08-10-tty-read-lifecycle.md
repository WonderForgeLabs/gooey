# tty read lifecycle: Fd() detaches the netpoller (bug record)

Found by the demo-browser agent 2026-08-10 while proving the terminal
handoff; recorded here by the coordinator because the finder's write
set didn't include docs/.

## The bug

`term.Screen.Restore()` closes the tty, but a goroutine blocked in
`Read` on that file does NOT unblock. Root cause: `os.File.Fd()`
switches the file to blocking mode and REMOVES it from Go's netpoller;
after that, a pending Read is an uninterruptible syscall that Close
cannot cancel. `term.Screen` calls `Fd()` unavoidably today — `Raw()`
uses `term.MakeRaw(int(fd))` and `Size()` uses `term.GetSize(int(fd))`
because golang.org/x/term takes an int fd.

A/B evidence (same /dev/tty under a pty, read genuinely blocked 700ms
before Close):

    without Fd(): read UNBLOCKED by Close (err=file already closed)
    with    Fd(): read STILL BLOCKED 2s after Close

## Impact

- Every `term.DecodeEvents` goroutine outlives its Screen: N terminal
  handoffs (cmd/browser) = N stale readers competing for the terminal
  with the child process and with every later decoder.
- `Screen.Detect()` has the same hazard by design: it documents
  abandoning its probe goroutine with a Read pending whenever nothing
  answers DA1 — routine under headless/recording ptys. A second
  permanent competing reader.

## Current workaround (cmd/browser)

The browser opens a second `/dev/tty` handle used only for reading and
never touched by `Fd()`, so it stays pollable and Close reliably
unblocks it; a `decDone` channel makes decoder death observable and
the teardown waits on it (with a status-line warning as the tripwire).

## Framework fix — DONE 2026-08-10

Implemented as the preferred option: `Screen.control` performs the
MakeRaw/GetSize/Restore ioctls through `SyscallConn().Control`, so the
fd is never detached from the poller. Close is now a reliable
cancellation for every reader.

On top of that:

- `Screen.Events` starts the decoder and the Screen OWNS it;
  `Screen.Restore` restores modes, closes the tty, then JOINS the
  decoder (draining its channel so a reader blocked on send cannot
  wedge teardown), bounded by `term.DecoderTimeout`.
  `Screen.DecoderLeaked` is the tripwire.
- The invariant now holds and is tested:
  **no Screen teardown leaves a goroutine reading the terminal**
  (`term/lifecycle_test.go`, including both halves of the A/B above on
  a real pty).
- `Detect`'s read deadline actually works now. It had been failing with
  `ErrNoDeadline` on every run — `Size()` called `Fd()` first — silently
  degrading the probe to one blocking read.
- cmd/browser's second-`/dev/tty`-handle workaround and its private copy
  of the decoder are DELETED (-185 lines); the hand-off is
  `gooey.App.Suspend`, and the status-line tripwire remains.

ORDERING NOTE for anyone re-running the A/B: `Fd()` must be called
BEFORE the read starts. A read submitted while the file is still
pollable is parked in the netpoller and Close wakes it even if `Fd()` is
called afterwards — which is why the bug looked intermittent.

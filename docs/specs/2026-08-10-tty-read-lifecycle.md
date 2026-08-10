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

## Framework fix (owner: whoever holds term/ next)

Preferred: perform the MakeRaw/GetSize ioctls through
`SyscallConn().Control` so the fd is never detached from the poller —
then Close is a reliable cancellation for every reader, and Detect's
probe goroutine can be joined instead of abandoned. Alternative:
give DecodeEvents an explicit stop mechanism and make Screen teardown
wait for reader exit. Either way: the invariant to establish is
**no Screen teardown may leave a goroutine reading the terminal**, and
cmd/browser's second-handle workaround becomes removable once it
holds.

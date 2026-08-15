package main

// Launching what you just bought.
//
// A store that charges you and then tells you to open another terminal is
// not a store, it is a screenshot with a wallet. Subscribe says "Chromatica
// may now modify this app"; this file is what makes "may" turn into "does".
//
// Why these are NOT gooey Companions, which is the obvious first idea:
//
//   - App.AddCompanion is a no-op once Run has started (companion.go:146),
//     and by construction a purchase happens mid-run. That is not an
//     oversight to work around — a Companion is a specific LIFETIME:
//     started before the tree exists and before raw mode, supervised, and
//     joined at teardown. A vendor that arrives at minute six cannot have
//     the first of those and must not have the second.
//
//   - The supervision is backwards for this case. A companion that dies
//     takes the app down with it, which is exactly right for a Temporal
//     worker the UI is meaningless without, and exactly wrong for a
//     third-party toolbar widget. Chromatica crashing must cost you a
//     colour picker, not Northwind Ops.
//
// So: an ordinary child process the store owns, started on Subscribe and
// stopped on Cancel. Nothing about the trust story changes — the vendor
// still speaks MCP over HTTP with no library from Northwind, still reaches
// only its own island, and is still refused if it names anything outside
// the grant. Launching a process is not linking a plugin. It is what an
// app store does.

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
)

// vendorProc is one running vendor.
type vendorProc struct {
	cmd *exec.Cmd
}

// vendors tracks what is running, by Integration.ID.
//
// Guarded by a mutex even though Subscribe and Cancel both run on the UI
// goroutine, because stopAll is called from main's defer on the way out —
// which is a different goroutine, on the teardown path, after Run has
// returned.
type vendors struct {
	mu    sync.Mutex
	addr  string // the VENDOR island. Never the owner's port.
	procs map[string]*vendorProc
}

func newVendors(addr string) *vendors {
	return &vendors{addr: addr, procs: map[string]*vendorProc{}}
}

// launch starts the vendor behind an Integration, if it has one.
//
// Returns a line for the receipt. A launch that failed has to say so on
// screen: the whole demo is about a third party changing your UI, and
// "nothing happened, silently" is the one outcome that teaches nobody
// anything.
func (v *vendors) launch(it Integration) string {
	if it.Cmd == "" {
		// Vestibule and Ledgerline are catalogue entries with no product
		// behind them. Saying so is better than implying the subscription
		// did something invisible.
		return "subscribed — " + it.Vendor + " has no local build in this demo"
	}
	if v.addr == "" {
		return "subscribed — but the vendor island is disabled (-vendor \"\")"
	}

	v.mu.Lock()
	defer v.mu.Unlock()
	if _, ok := v.procs[it.ID]; ok {
		return "subscribed — " + it.Vendor + " was already running"
	}

	// Build first, then run the binary — rather than `go run ./cmd/...`,
	// which puts a compiler between the store and the vendor and leaves a
	// GRANDCHILD that Process.Kill cannot reach. Cancel would then report
	// success while the vendor kept its grant. The build is cached, so
	// this is slow exactly once.
	bin := filepath.Join(os.TempDir(), "northwind-vendor-"+it.ID)
	if out, err := exec.Command("go", "build", "-o", bin, it.Cmd).CombinedOutput(); err != nil {
		return "subscribed — " + it.Vendor + " failed to build: " + firstLine(string(out))
	}

	// The vendor's output goes to a FILE, and this is not tidiness.
	//
	// chromatica narrates what it is doing on stdout -- "chromatica │
	// dialling…", "chromatica │ patched: {…}". Run from its own terminal
	// that is the point of it. Inherited from the store it is a disaster:
	// this process holds the terminal in raw mode on the alternate screen,
	// and a child writing to the same fd paints straight onto cells the
	// damage model believes are clean. They are not repainted, because
	// nothing that owns them changed -- so the garbage stays until
	// something else happens to dirty that row. The first version of this
	// inherited os.Stderr and scribbled JSON across the running UI.
	//
	// A file rather than io.Discard because what the vendor did is worth
	// being able to read afterwards, and the path goes in the receipt.
	logPath := filepath.Join(os.TempDir(), "northwind-vendor-"+it.ID+".log")
	logf, err := os.Create(logPath)
	if err != nil {
		return "subscribed — " + it.Vendor + " could not open a log: " + err.Error()
	}
	cmd := exec.Command(bin, "-addr", v.addr)
	cmd.Stdout = logf
	cmd.Stderr = logf
	if err := cmd.Start(); err != nil {
		logf.Close()
		return "subscribed — " + it.Vendor + " failed to start: " + err.Error()
	}
	// Reap it, and close the log with it. Without the Wait the process
	// stays a zombie after it exits, and `ps` during a demo is a fair
	// thing for someone to do.
	go func() { _ = cmd.Wait(); logf.Close() }()

	v.procs[it.ID] = &vendorProc{cmd: cmd}
	return "subscribed — " + it.Vendor + " is running · log " + logPath
}

// stop ends one vendor. Cancelling a subscription that leaves the vendor
// connected is not a cancellation.
func (v *vendors) stop(id string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if p, ok := v.procs[id]; ok {
		_ = p.cmd.Process.Kill()
		delete(v.procs, id)
	}
}

// stopAll is teardown. Called from main's defer, so a vendor never
// outlives the app that launched it — the one Companion guarantee that IS
// wanted here, kept by hand because it is the only one that applies.
func (v *vendors) stopAll() {
	v.mu.Lock()
	defer v.mu.Unlock()
	for id, p := range v.procs {
		_ = p.cmd.Process.Kill()
		delete(v.procs, id)
	}
}

// restoreToolbar puts Northwind's own toolbar back over whatever a vendor
// left there.
//
// Through the app owner's OWN service, which is unscoped — not through the
// island. The host patching its own tree is not a vendor operation and must
// not be expressible as one: an island that could write Toolbar back could
// also be used by one vendor to evict another.
//
// The source is a file rather than a Go string, because nothing in this
// program assembles markup by concatenation — there is no shape of it the
// loader has not already checked.
func (s *Store) restoreToolbar() error {
	src, err := fs.ReadFile(s.dir, "toolbar.gooey")
	if err != nil {
		return err
	}
	_, err = s.svc.PatchMarkup("Toolbar", string(src))
	return err
}

func firstLine(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			return s[:i]
		}
	}
	if s == "" {
		return "(no output)"
	}
	return s
}

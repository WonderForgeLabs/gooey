// Package fshandlers is the filesystem handler namespace: files from
// markup, with no app code.
//
//	<Gooey xmlns:fs="gooey.dev/handlers/fs">
//	  <Button Content="open" Click="{{fs:Read .Path | into .Content}}"/>
//
// The host app grants the capability by registering the provider, and
// the registration names a ROOT:
//
//	markup.RegisterHandlers(fshandlers.URI, fshandlers.New(os.DirFS("./docs")))
//
// The fs.FS handed to New is the security boundary. Every path a page
// names resolves inside that FS and nowhere else: escapes are rejected
// structurally (per fs.ValidPath — relative, slash-separated, no "..",
// no leading /), so the pack never decides what markup may touch; the
// grant already did. An embed.FS grants exactly its embedded files, a
// TarFS grants an archive, os.DirFS grants a directory subtree.
//
// Reading is the default posture. Writes require a separate, explicit
// grant — NewWritable(dir), backed by os.Root, which enforces the same
// no-escape guarantee at the OS level (symlinks included). A read-only
// registration makes Write and Append load-time errors, not runtime
// surprises.
//
// Functions and result shapes (v1 grammar — every call needs a
// `| into .Target` and the target holds a string):
//
//	Read .Path      → the file's contents (capped; see DefaultMaxRead)
//	List .Dir       → JSON array of entries (shape below)
//	Stat .Path      → JSON of one entry
//	Glob .Pattern   → JSON array of matching path strings
//	Write .Path .Content   (writable grant only)
//	Append .Path .Content  (writable grant only)
//
// A directory entry is a protojson-adjacent JSON object:
//
//	{"name":"spec.md","size":1204,"dir":false,"modTime":"2026-08-10T12:00:00Z"}
//
// modTime is RFC 3339 (protojson's Timestamp rendering); a zero mod
// time renders as "" the way protojson leaves unset fields out. size is
// a JSON number — these are display values headed for a terminal, not
// wire int64s.
//
// Failures deliver into the same target as an "ERROR: …" string, the
// house v1 convention. Write and Append use their target as a status
// slot: Set to "" on success, the ERROR string on failure — the same
// cleared-on-success semantics the pipeline grammar v2 record assigns
// to `err` tails.
//
// Watch (delivering on file change) is deliberately absent: a v1
// handler is one-shot and command-shaped, while watching is a
// subscription with a lifetime — that belongs to the v2 stage grammar
// or to a companion, not to a Command that would leak a goroutine per
// click.
package fshandlers

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"
	"time"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/markup"
)

// URI is the namespace URI markup declares to reach this provider.
const URI = "gooey.dev/handlers/fs"

// The pack's function names, per the pack-distribution doctrine: typed
// constants, enumerated by AllNames, with error text derived from the
// same lists so the inventory cannot drift from the messages.
const (
	NameRead   = "Read"
	NameList   = "List"
	NameStat   = "Stat"
	NameGlob   = "Glob"
	NameWrite  = "Write"  // writable grant only
	NameAppend = "Append" // writable grant only
)

var (
	readNames  = []string{NameRead, NameList, NameStat, NameGlob}
	writeNames = []string{NameWrite, NameAppend}
)

// AllNames lists every function the pack defines — its inventory. A
// read-only provider resolves only the first four; Write and Append
// exist on a writable grant (NewWritable).
func AllNames() []string {
	return append(append([]string{}, readNames...), writeNames...)
}

// DefaultMaxRead caps what fs:Read can put into a property. A terminal
// shows a screenful; a markup pipeline pointed at a 2GB log should get
// an error naming the cap, not become the application's memory profile.
const DefaultMaxRead = 1 << 20

// Provider implements markup.HandlerProvider for the fs namespace.
type Provider struct {
	fsys    fs.FS
	root    *os.Root
	maxRead int64
}

// Option configures a Provider.
type Option func(*Provider)

// WithMaxRead caps the bytes fs:Read will deliver. Larger files fail
// with an ERROR naming the cap rather than truncating: a silently cut
// file is corrupt data, and unlike a network stream we can know better.
func WithMaxRead(n int64) Option { return func(p *Provider) { p.maxRead = n } }

// New builds the read-only provider over fsys. Register it to grant the
// namespace; the fsys IS the grant's extent.
func New(fsys fs.FS, opts ...Option) *Provider {
	p := &Provider{fsys: fsys, maxRead: DefaultMaxRead}
	for _, o := range opts {
		o(p)
	}
	return p
}

// NewWritable builds the read-write provider rooted at dir. It is a
// separate constructor on purpose: a writable grant must be a visible,
// explicit decision at the registration site, never a flag that
// defaults on. The root is an os.Root, so the OS itself refuses any
// resolution (including through symlinks) that would leave dir.
func NewWritable(dir string, opts ...Option) (*Provider, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("fshandlers.NewWritable: %w", err)
	}
	p := &Provider{fsys: root.FS(), root: root, maxRead: DefaultMaxRead}
	for _, o := range opts {
		o(p)
	}
	return p, nil
}

// Close releases the writable root's handle. A read-only provider's
// Close is a no-op. Providers normally live for the process, so this
// exists for tests and short-lived tools.
func (p *Provider) Close() error {
	if p.root != nil {
		return p.root.Close()
	}
	return nil
}

// functions names what THIS provider resolves — the grant's view of
// the inventory, derived from the same lists AllNames uses.
func (p *Provider) functions() string {
	if p.root != nil {
		return strings.Join(AllNames(), ", ")
	}
	return strings.Join(readNames, ", ")
}

// NewCommand resolves one {{fs:…}} expression at load time.
func (p *Provider) NewCommand(c *markup.Call) (gooey.Command, error) {
	switch c.Fn {
	case NameRead:
		return p.query(c, "the path", p.readFile)
	case NameList:
		return p.query(c, "the directory", p.listDir)
	case NameStat:
		return p.query(c, "the path", p.statPath)
	case NameGlob:
		return p.globCmd(c)
	case NameWrite, NameAppend:
		if p.root == nil {
			return nil, fmt.Errorf("%s needs a writable grant — this namespace was registered read-only (fshandlers.New); the host must register fshandlers.NewWritable(dir) to allow writes", c.Fn)
		}
		return p.writeCmd(c)
	}
	return nil, fmt.Errorf("unknown function %q; fs provides: %s", c.Fn, p.functions())
}

// query builds the shared shape of the four read functions: one path
// argument, a required target, the work off the UI goroutine.
func (p *Provider) query(c *markup.Call, what string, run func(string) string) (gooey.Command, error) {
	if len(c.Args) != 1 {
		return nil, fmt.Errorf("%s takes 1 argument (%s), got %d", c.Fn, what, len(c.Args))
	}
	if !c.Target.Valid() {
		return nil, fmt.Errorf("%s needs a result target — add `| into .SomeProperty`", c.Fn)
	}
	if a := c.Args[0]; a.IsLiteral() && !fs.ValidPath(a.String()) {
		return nil, fmt.Errorf("%s: %q is not a valid path — fs paths are relative, slash-separated, without \"..\" (fs.ValidPath)", c.Fn, a.String())
	}
	src, target := c.Args[0], c.Target
	return func() {
		// Read the argument handle HERE: the command runs on the UI
		// goroutine, where touching properties is legal. The value, not
		// the handle, crosses to the worker goroutine.
		name := src.String()
		go func() { target.Deliver(run(name)) }()
	}, nil
}

func (p *Provider) globCmd(c *markup.Call) (gooey.Command, error) {
	if len(c.Args) != 1 {
		return nil, fmt.Errorf("Glob takes 1 argument (the pattern), got %d", len(c.Args))
	}
	if !c.Target.Valid() {
		return nil, fmt.Errorf("Glob needs a result target — add `| into .SomeProperty`")
	}
	if a := c.Args[0]; a.IsLiteral() {
		if _, err := path.Match(a.String(), ""); err != nil {
			return nil, fmt.Errorf("Glob: %q is not a valid pattern: %v", a.String(), err)
		}
	}
	src, target := c.Args[0], c.Target
	return func() {
		pattern := src.String()
		go func() { target.Deliver(p.globPaths(pattern)) }()
	}, nil
}

func (p *Provider) writeCmd(c *markup.Call) (gooey.Command, error) {
	if len(c.Args) != 2 {
		return nil, fmt.Errorf("%s takes 2 arguments (the path, then the content), got %d", c.Fn, len(c.Args))
	}
	if !c.Target.Valid() {
		return nil, fmt.Errorf("%s needs a status target — add `| into .SomeProperty` (Set to \"\" on success, an ERROR string on failure)", c.Fn)
	}
	if a := c.Args[0]; a.IsLiteral() && !fs.ValidPath(a.String()) {
		return nil, fmt.Errorf("%s: %q is not a valid path — fs paths are relative, slash-separated, without \"..\" (fs.ValidPath)", c.Fn, a.String())
	}
	src, content, target := c.Args[0], c.Args[1], c.Target
	appendMode := c.Fn == NameAppend
	fn := c.Fn
	return func() {
		name, data := src.String(), content.String()
		go func() { target.Deliver(p.writeFile(fn, name, data, appendMode)) }()
	}, nil
}

func badPath(name string) string {
	return fmt.Sprintf("ERROR: %q: not a valid path — fs paths are relative, slash-separated, without \"..\"", name)
}

// readFile delivers the file's contents, or an ERROR string. The cap is
// enforced by reading one byte past it: no Stat round-trip, no
// time-of-check gap, works on FSes that report no size.
func (p *Provider) readFile(name string) string {
	if !fs.ValidPath(name) {
		return badPath(name)
	}
	f, err := p.fsys.Open(name)
	if err != nil {
		return "ERROR: " + err.Error()
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, p.maxRead+1))
	if err != nil {
		return fmt.Sprintf("ERROR: fs:Read %s: %s", name, err)
	}
	if int64(len(data)) > p.maxRead {
		return fmt.Sprintf("ERROR: fs:Read %s: file exceeds the %d-byte cap (fshandlers.WithMaxRead raises it)", name, p.maxRead)
	}
	return string(data)
}

// entry is the JSON shape List and Stat deliver — documented in the
// package comment; change it there too or not at all.
type entry struct {
	Name    string `json:"name"`
	Size    int64  `json:"size"`
	Dir     bool   `json:"dir"`
	ModTime string `json:"modTime"`
}

func entryOf(info fs.FileInfo) entry {
	e := entry{Name: info.Name(), Size: info.Size(), Dir: info.IsDir()}
	if t := info.ModTime(); !t.IsZero() {
		e.ModTime = t.UTC().Format(time.RFC3339Nano)
	}
	return e
}

func (p *Provider) listDir(name string) string {
	if !fs.ValidPath(name) {
		return badPath(name)
	}
	des, err := fs.ReadDir(p.fsys, name)
	if err != nil {
		return "ERROR: " + err.Error()
	}
	entries := make([]entry, 0, len(des))
	for _, de := range des {
		info, err := de.Info()
		if err != nil {
			return fmt.Sprintf("ERROR: fs:List %s: %s", name, err)
		}
		entries = append(entries, entryOf(info))
	}
	return mustJSON(entries)
}

func (p *Provider) statPath(name string) string {
	if !fs.ValidPath(name) {
		return badPath(name)
	}
	info, err := fs.Stat(p.fsys, name)
	if err != nil {
		return "ERROR: " + err.Error()
	}
	return mustJSON(entryOf(info))
}

func (p *Provider) globPaths(pattern string) string {
	matches, err := fs.Glob(p.fsys, pattern)
	if err != nil {
		return fmt.Sprintf("ERROR: fs:Glob %s: %s", pattern, err)
	}
	if matches == nil {
		matches = []string{}
	}
	return mustJSON(matches)
}

func (p *Provider) writeFile(fn, name, data string, appendMode bool) string {
	if !fs.ValidPath(name) {
		return badPath(name)
	}
	if appendMode {
		f, err := p.root.OpenFile(name, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return fmt.Sprintf("ERROR: fs:%s %s: %s", fn, name, err)
		}
		_, werr := f.WriteString(data)
		cerr := f.Close()
		if werr != nil {
			return fmt.Sprintf("ERROR: fs:%s %s: %s", fn, name, werr)
		}
		if cerr != nil {
			return fmt.Sprintf("ERROR: fs:%s %s: %s", fn, name, cerr)
		}
		return ""
	}
	if err := p.root.WriteFile(name, []byte(data), 0o644); err != nil {
		return fmt.Sprintf("ERROR: fs:%s %s: %s", fn, name, err)
	}
	return ""
}

// mustJSON marshals values whose types cannot fail to marshal.
func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "ERROR: " + err.Error() // unreachable for entry/[]string
	}
	return string(b)
}

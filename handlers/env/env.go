// Package envhandlers is the environment namespace: ambient host
// values, readable from markup with no app code.
//
//	<Gooey xmlns:env="gooey.dev/handlers/env">
//	  <Text>logged in as {{env:Get `USER`}}</Text>
//
// It is the first namespace registered on the PULL side. An
// environment variable is not an event — it is the value of a binding —
// so it is reached through markup.RegisterValues rather than
// markup.RegisterHandlers, and {{env:Get …}} goes wherever {{.Path}}
// goes. See docs/specs/2026-08-12-value-namespaces.md.
//
// # The grant is an itemized allowlist
//
// The host grants named variables, one at a time:
//
//	markup.RegisterValues(envhandlers.URI, envhandlers.New("USER", "HOME", "TERM"))
//
// A name outside that list is a LOAD error, not an empty string. This
// is the exec pack's posture rather than the fs pack's, and the reason
// is that the environment is not a uniform space: it is where processes
// keep AWS_SECRET_ACCESS_KEY next to TERM. A page loaded from an
// untrusted fs.FS that could read the whole environment, in a process
// that also grants net:Get, is a credential exfiltration path with two
// markup elements and no app code. So there is deliberately no
// "grant everything" constructor; a host that truly wants one can
// enumerate os.Environ itself and see what it is doing.
//
// Because the grant is a list rather than a predicate, it is
// enumerable, and {{env:Names}} renders it — a namespace that can show
// a page what it is allowed to see.
//
// # Writing is a separate grant
//
// NewWritable additionally provides the PUSH half, registered on the
// handler side:
//
//	p := envhandlers.NewWritable("EDITOR")
//	markup.RegisterValues(envhandlers.URI, p)   // env:Get, env:Names
//	markup.RegisterHandlers(envhandlers.URI, p) // env:Set, env:Unset
//
//	<Button Content="use vim" Click="{{env:Set `EDITOR` `vim`}}"/>
//	<Text>EDITOR={{env:Get `EDITOR`}}</Text>
//
// The two registrations are what make that Text update when the Button
// is pressed: env:Get hands out a per-name SOURCE property, and env:Set
// writes the process environment and that same source, so the change
// travels the ordinary property graph and repaints exactly the
// components reading it. Registering only the value half leaves the
// namespace read-only and makes env:Set a load error.
package envhandlers

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/prop"
)

// URI is the namespace URI markup declares to reach this provider.
const URI = "gooey.dev/handlers/env"

// The namespace's function names. Get and Names are value functions
// (they appear where a binding appears); Set and Unset are handler
// functions (they appear on an event attribute) and exist only on a
// writable grant.
const (
	NameGet   = "Get"
	NameNames = "Names"
	NameSet   = "Set"   // writable grant only
	NameUnset = "Unset" // writable grant only
)

var (
	valueNames   = []string{NameGet, NameNames}
	handlerNames = []string{NameSet, NameUnset}
)

// AllNames lists every function the pack defines — its inventory, per
// docs/specs/2026-08-10-pack-distribution.md.
func AllNames() []string {
	return append(append([]string{}, valueNames...), handlerNames...)
}

// Provider implements markup.ValueProvider always, and
// markup.HandlerProvider when built with NewWritable.
type Provider struct {
	allow    []string // sorted; the grant, verbatim
	allowSet map[string]bool
	writable bool

	// mu guards sources only. The properties themselves are unlocked by
	// design and belong to the UI goroutine, exactly like every other
	// property in the framework; what needs guarding is the map, which a
	// concurrent Load could otherwise grow from two goroutines.
	mu      sync.Mutex
	sources map[string]*prop.Property[string]

	lookup func(string) (string, bool)
	store  func(string, string) error
	remove func(string) error
}

// Option configures a Provider.
type Option func(*Provider)

// WithEnviron replaces the process environment with an in-memory map.
// It is the test seam, and also the way to hand a page a synthetic
// environment without touching the real one.
func WithEnviron(m map[string]string) Option {
	return func(p *Provider) {
		env := make(map[string]string, len(m))
		for k, v := range m {
			env[k] = v
		}
		p.lookup = func(k string) (string, bool) { v, ok := env[k]; return v, ok }
		p.store = func(k, v string) error { env[k] = v; return nil }
		p.remove = func(k string) error { delete(env, k); return nil }
	}
}

// New builds a read-only provider granting exactly the named variables.
// Register it with markup.RegisterValues; the names ARE the grant's
// extent. Granting nothing is legal and makes every env:Get a load
// error, which is the right default for a host that has not thought
// about it yet.
func New(names ...string) *Provider { return build(false, names) }

// NewWritable builds a provider that also serves env:Set and env:Unset
// over the same allowlist. Register it with BOTH markup.RegisterValues
// and markup.RegisterHandlers; registering only the first leaves the
// namespace readable and makes a write a load error.
func NewWritable(names ...string) *Provider { return build(true, names) }

func build(writable bool, names []string) *Provider {
	p := &Provider{
		allow:    append([]string{}, names...),
		allowSet: make(map[string]bool, len(names)),
		writable: writable,
		sources:  map[string]*prop.Property[string]{},
		lookup:   os.LookupEnv,
		store:    os.Setenv,
		remove:   os.Unsetenv,
	}
	for _, n := range names {
		p.allowSet[n] = true
	}
	sort.Strings(p.allow)
	return p
}

// Configure applies options after construction. Kept separate from New
// so the allowlist stays the first thing a reader sees at every call
// site.
func (p *Provider) Configure(opts ...Option) *Provider {
	for _, o := range opts {
		o(p)
	}
	return p
}

// Granted lists the variables this provider was built with, sorted.
func (p *Provider) Granted() []string { return append([]string{}, p.allow...) }

// source returns the per-name property, seeded from the environment on
// first use. One handle per name per provider, so two bindings to the
// same variable share a source and a single env:Set updates both.
func (p *Provider) source(name string) *prop.Property[string] {
	p.mu.Lock()
	defer p.mu.Unlock()
	if s, ok := p.sources[name]; ok {
		return s
	}
	v, _ := p.lookup(name)
	s := prop.NewSource(v)
	p.sources[name] = s
	return s
}

// literalName validates the first argument of every function here: it
// must be a backtick literal, because the allowlist check is a LOAD
// check. A bound name would move the capability decision to paint time,
// where markup has no way to report it.
func (p *Provider) literalName(fn string, a markup.Arg) (string, error) {
	if !a.IsLiteral() {
		return "", fmt.Errorf("%s takes a backtick literal variable name, not .%s — the allowlist is checked when the page loads, and a bound name would move that check to paint time", fn, a.Path)
	}
	name := a.String()
	if !p.allowSet[name] {
		return "", fmt.Errorf("%q is not in this host's environment grant; granted: %s", name, p.grantText())
	}
	return name, nil
}

func (p *Provider) grantText() string {
	if len(p.allow) == 0 {
		return "(nothing — the host called envhandlers.New() with no names)"
	}
	return strings.Join(p.allow, ", ")
}

// NewValue resolves one {{env:…}} expression in a value position.
func (p *Provider) NewValue(c *markup.Call) (*prop.Property[string], error) {
	switch c.Fn {
	case NameGet:
		return p.get(c)
	case NameNames:
		if len(c.Args) != 0 {
			return nil, fmt.Errorf("Names takes no arguments, got %d", len(c.Args))
		}
		return prop.NewSource(strings.Join(p.allow, ", ")), nil
	case NameSet, NameUnset:
		return nil, fmt.Errorf("%s is an effect, not a value: invoke it from an event attribute, as Click=\"{{%s:%s …}}\"", c.Fn, c.Prefix, c.Fn)
	}
	return nil, fmt.Errorf("unknown function %q; env reads: %s", c.Fn, strings.Join(valueNames, ", "))
}

func (p *Provider) get(c *markup.Call) (*prop.Property[string], error) {
	if len(c.Args) != 1 && len(c.Args) != 2 {
		return nil, fmt.Errorf("Get takes the variable name and an optional fallback, got %d arguments", len(c.Args))
	}
	name, err := p.literalName(NameGet, c.Args[0])
	if err != nil {
		return nil, err
	}
	src := p.source(name)
	if len(c.Args) == 1 {
		return src, nil
	}
	fallback := c.Args[1]
	return prop.NewComputed(func() string {
		// Both reads are HOISTED above the branch on purpose. A
		// fallback may be a bound .Path, and a Get that runs only on
		// some frames drops out of the dependency set on the others —
		// the component would go deaf to the fallback exactly when it
		// is displaying the variable. See CLAUDE.md, "Dependencies are
		// recorded by the Get that actually runs".
		v := src.Get()
		fb := fallback.String()
		if v == "" {
			return fb
		}
		return v
	}), nil
}

// NewCommand resolves one {{env:…}} expression on an event attribute.
// Only a writable grant reaches this at all.
func (p *Provider) NewCommand(c *markup.Call) (gooey.Command, error) {
	switch c.Fn {
	case NameSet, NameUnset:
		if !p.writable {
			return nil, fmt.Errorf("%s needs a writable grant: the host registered envhandlers.New(…), which is read-only — use envhandlers.NewWritable(…) to grant writes", c.Fn)
		}
	case NameGet, NameNames:
		return nil, fmt.Errorf("%s is a value, not an effect: write it where a binding goes, as Text=\"{{%s:%s …}}\"", c.Fn, c.Prefix, c.Fn)
	default:
		return nil, fmt.Errorf("unknown function %q; env writes: %s", c.Fn, strings.Join(handlerNames, ", "))
	}

	if c.Fn == NameUnset {
		if len(c.Args) != 1 {
			return nil, fmt.Errorf("Unset takes 1 argument (the variable name), got %d", len(c.Args))
		}
		name, err := p.literalName(NameUnset, c.Args[0])
		if err != nil {
			return nil, err
		}
		src, target := p.source(name), c.Target
		return func() {
			if err := p.remove(name); err != nil {
				target.Deliver("ERROR: " + err.Error())
				return
			}
			src.Set("")
			target.Deliver("")
		}, nil
	}

	if len(c.Args) != 2 {
		return nil, fmt.Errorf("Set takes 2 arguments (the variable name and its value), got %d", len(c.Args))
	}
	name, err := p.literalName(NameSet, c.Args[0])
	if err != nil {
		return nil, err
	}
	val, src, target := c.Args[1], p.source(name), c.Target
	return func() {
		// The Command runs on the UI goroutine, so reading the argument
		// handle and Setting the source are both legal here, and the
		// repaint lands in this frame rather than the next.
		v := val.String()
		if err := p.store(name, v); err != nil {
			target.Deliver("ERROR: " + err.Error())
			return
		}
		src.Set(v)
		target.Deliver("")
	}, nil
}

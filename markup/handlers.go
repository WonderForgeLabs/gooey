package markup

import (
	"fmt"
	"sort"
	"sync"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/prop"
)

// Handler namespaces: behavior declared in markup, with no app code.
//
// An event attribute can name a *provider* function instead of a
// viewmodel delegate:
//
//	<Gooey xmlns:net="gooey.dev/handlers/net">
//	  <Button Content="fetch" Click="{{net:Get .Url | into .Body}}"/>
//
// The prefix resolves through the document's xmlns table to a URI, the
// URI resolves through a process-wide registry to a HandlerProvider,
// and the provider turns the call into a gooey.Command. Nothing about
// the mechanism is reflective: providers are typed factories, arguments
// are property handles resolved at build time, and the result target is
// a handle too.
//
// Registration is the capability grant. Markup can only invoke
// namespaces the *host app* registered, so a page loaded from an
// untrusted fs.FS reaches exactly the capabilities its host chose to
// hand it — the lens model. An undeclared prefix or an unregistered URI
// is a load-time error naming what was missing, never a silent no-op.

// HandlerProvider turns one {{ns:Func …}} expression into a Command.
//
// NewCommand runs at BUILD time, once per expression, so everything
// resolvable is resolved before the UI is live: bad arity, bad argument
// types, and unknown functions are load errors, not surprises on click.
// The returned Command is what runs on each event.
type HandlerProvider interface {
	NewCommand(c *Call) (gooey.Command, error)
}

// HandlerFunc adapts a plain function to HandlerProvider, for providers
// with no configuration of their own.
type HandlerFunc func(c *Call) (gooey.Command, error)

func (f HandlerFunc) NewCommand(c *Call) (gooey.Command, error) { return f(c) }

// Call is one parsed handler expression handed to a provider.
type Call struct {
	// Prefix and URI are the namespace it was invoked through.
	Prefix, URI string
	// Fn is the function name after the colon ("Get", "Activity").
	Fn string
	// Args are the call arguments, left to right.
	Args []Arg
	// Target is the `| into .Path` destination; check Valid before use.
	Target Target
	// Ctx is the binding context the expression was built in.
	Ctx *Context
	// Dispatcher marshals work onto the UI goroutine. Providers that
	// touch properties from a background goroutine MUST go through it
	// (Target.Deliver already does).
	Dispatcher *gooey.Dispatcher
}

// Arg is one argument of a handler expression: a backtick literal, or a
// `.Path` bound to a context value.
//
// A path argument holds the *handle*, not a snapshot — String() reads it
// at invoke time, which is the same lvalue semantics as every other
// binding. Read it on the UI goroutine (inside the Command), before
// handing anything to a background goroutine.
type Arg struct {
	// Path is the source path for a bound argument ("" for literals).
	Path string
	// Raw is the context value a path resolved to — a *prop.Property[T]
	// handle or a plain value. Providers needing a type other than
	// string type-switch on it; there is no reflection here.
	Raw any

	lit   string
	isLit bool
}

// IsLiteral reports whether the argument came from a backtick literal.
func (a Arg) IsLiteral() bool { return a.isLit }

// String returns the argument's current value. Literals are constant;
// bound arguments read their property, so call this on the UI goroutine.
//
// It is total: argument types are validated when the expression is
// built, so an Arg that exists can always produce a string.
func (a Arg) String() string {
	if a.isLit {
		return a.lit
	}
	switch v := a.Raw.(type) {
	case *prop.Property[string]:
		return v.Get()
	case string:
		return v
	}
	return ""
}

// Target is a handler's result destination: a property handle resolved
// at build time from `| into .Path`.
type Target struct {
	path string
	p    *prop.Property[string]
	d    *gooey.Dispatcher
}

// Valid reports whether an `into` target was declared.
func (t Target) Valid() bool { return t.p != nil }

// Path is the source path of the target, for error messages.
func (t Target) Path() string { return t.path }

// Deliver Sets the target property with the handler's result. It is
// safe to call from any goroutine: the Set is queued on the Dispatcher
// and runs on the UI loop, which is what keeps async handlers inside
// the UI-goroutine confinement rule. Delivering to an absent target is
// a no-op, so providers need not branch on Valid.
func (t Target) Deliver(s string) {
	if t.p == nil || t.d == nil {
		return
	}
	t.d.Post(func() { t.p.Set(s) })
}

var (
	providersMu sync.RWMutex
	providers   = map[string]HandlerProvider{}
)

// RegisterHandlers grants markup the capability to invoke a namespace:
// after this call, any document declaring xmlns:x="uri" can bind events
// to {{x:Func …}}. Registering the same URI again replaces the
// provider. Safe for concurrent use, though the normal place to call it
// is app startup.
func RegisterHandlers(uri string, p HandlerProvider) {
	providersMu.Lock()
	defer providersMu.Unlock()
	if p == nil {
		delete(providers, uri)
		return
	}
	providers[uri] = p
}

// RegisteredHandlers lists the granted namespace URIs, sorted. For
// diagnostics and for the error a document gets when it asks for one
// that was not granted.
func RegisteredHandlers() []string {
	providersMu.RLock()
	defer providersMu.RUnlock()
	uris := make([]string, 0, len(providers))
	for u := range providers {
		uris = append(uris, u)
	}
	sort.Strings(uris)
	return uris
}

func lookupProvider(uri string) (HandlerProvider, bool) {
	providersMu.RLock()
	defer providersMu.RUnlock()
	p, ok := providers[uri]
	return p, ok
}

// handlerCommand resolves a parsed expression against the document's
// namespace table and the provider registry, then asks the provider for
// its Command. Every failure here is a load-time failure.
func (ctx *Context) handlerCommand(x *handlerExpr) (gooey.Command, error) {
	uri, ok := ctx.ns[x.Prefix]
	if !ok {
		return nil, fmt.Errorf("markup: undeclared namespace prefix %q — add xmlns:%s=\"…\" to the root element", x.Prefix, x.Prefix)
	}
	p, ok := lookupProvider(uri)
	if !ok {
		return nil, fmt.Errorf("markup: namespace %q (prefix %q) has no registered handler provider; the host app must call markup.RegisterHandlers(%q, …). Granted: %v",
			uri, x.Prefix, uri, RegisteredHandlers())
	}
	if ctx.Dispatcher == nil {
		return nil, fmt.Errorf("markup: {{%s:%s …}} needs a Dispatcher: handler results are Set on the UI goroutine, so Context.Dispatcher must be set", x.Prefix, x.Fn)
	}

	args := make([]Arg, len(x.Args))
	for i, a := range x.Args {
		arg, err := ctx.resolveArg(a)
		if err != nil {
			return nil, fmt.Errorf("markup: {{%s:%s …}} argument %d: %w", x.Prefix, x.Fn, i+1, err)
		}
		args[i] = arg
	}

	var target Target
	if x.Into != "" {
		v, err := resolve(ctx.Values, x.Into)
		if err != nil {
			return nil, fmt.Errorf("markup: {{%s:%s … | into .%s}}: %w", x.Prefix, x.Fn, x.Into, err)
		}
		h, ok := v.(*prop.Property[string])
		if !ok {
			return nil, fmt.Errorf("markup: {{%s:%s … | into .%s}}: target is %T; need *prop.Property[string]", x.Prefix, x.Fn, x.Into, v)
		}
		target = Target{path: x.Into, p: h, d: ctx.Dispatcher}
	}

	cmd, err := p.NewCommand(&Call{
		Prefix: x.Prefix, URI: uri, Fn: x.Fn,
		Args: args, Target: target, Ctx: ctx, Dispatcher: ctx.Dispatcher,
	})
	if err != nil {
		return nil, fmt.Errorf("markup: {{%s:%s …}}: %w", x.Prefix, x.Fn, err)
	}
	if cmd == nil {
		return nil, fmt.Errorf("markup: provider for %q returned no command for %s", uri, x.Fn)
	}
	return cmd, nil
}

// resolveArg turns one argument token into an Arg, failing the load if a
// bound path is missing or is not a type Arg.String can render.
func (ctx *Context) resolveArg(t token) (Arg, error) {
	if t.kind == tokLiteral {
		return Arg{lit: t.text, isLit: true}, nil
	}
	v, err := resolve(ctx.Values, t.text)
	if err != nil {
		return Arg{}, err
	}
	switch v.(type) {
	case *prop.Property[string], string:
		return Arg{Path: t.text, Raw: v}, nil
	}
	return Arg{}, fmt.Errorf(".%s is %T; need *prop.Property[string] or string", t.text, v)
}

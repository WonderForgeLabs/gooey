package markup

import (
	"fmt"
	"sort"
	"sync"

	"github.com/WonderForgeLabs/gooey/prop"
)

// Value namespaces: the PULL half of the namespace mechanism.
//
// A handler namespace answers "what happens when the user does this" —
// it is reachable only from an event attribute, and its result is
// PUSHED into a property by `| into .Path`. That shape is right for an
// effect (fetch a URL, run a workflow, play a sound) and wrong for a
// value. An ambient reading like the environment, and a pure transform
// like uppercasing a name, are not events: they ARE the value of a
// binding, and the page wants to write them where a binding goes.
//
//	<Gooey xmlns:env="gooey.dev/handlers/env"
//	       xmlns:str="gooey.dev/handlers/str">
//	  <Text>{{str:Upper .User}} @ {{env:Get `HOSTNAME`}}</Text>
//
// A value expression resolves at BUILD time to a *prop.Property[string]
// handle, exactly as {{.Path}} does, and composes with literals and
// paths in the same interpolation. Nothing about it is reflective: the
// provider is a typed factory, arguments are property handles, and the
// result is a handle.
//
// The damage guarantee comes for free, and it is worth being explicit
// about why. A provider builds its handle with prop.NewComputed, so
// every Arg.String() it calls runs INSIDE an evaluation — which is what
// makes that Get a subscription rather than a read (prop/prop.go's
// recordRead). {{str:Upper .Name}} therefore repaints exactly the
// components that display it, when and only when .Name changes, with
// no participation from this file. The corollary is the usual trap: a
// provider that reads an argument behind an early return goes deaf to
// it on the frames where the read does not run.
//
// Registration is the capability grant, as with handlers, and the two
// registries are SEPARATE on purpose: "may read the environment" and
// "may write the environment" are different grants, so a namespace that
// offers both is registered twice and a host can grant either half.

// ValueProvider turns one {{ns:Func …}} expression in a value position
// into a property handle.
//
// NewValue runs at BUILD time, once per expression, so everything
// resolvable is resolved before the UI is live: bad arity, bad argument
// types, unknown functions and — for a provider like env — names
// outside the host's grant are load errors, not surprises on the first
// paint.
type ValueProvider interface {
	NewValue(c *Call) (*prop.Property[string], error)
}

// ValueFunc adapts a plain function to ValueProvider, for providers
// with no configuration of their own.
type ValueFunc func(c *Call) (*prop.Property[string], error)

func (f ValueFunc) NewValue(c *Call) (*prop.Property[string], error) { return f(c) }

var (
	valuesMu       sync.RWMutex
	valueProviders = map[string]ValueProvider{}
)

// RegisterValues grants markup the capability to READ a namespace:
// after this call, any document declaring xmlns:x="uri" can write
// {{x:Func …}} wherever a binding goes. Registering the same URI again
// replaces the provider; passing nil revokes the grant. Safe for
// concurrent use, though the normal place to call it is app startup.
func RegisterValues(uri string, p ValueProvider) {
	valuesMu.Lock()
	defer valuesMu.Unlock()
	if p == nil {
		delete(valueProviders, uri)
		return
	}
	valueProviders[uri] = p
}

// RegisteredValues lists the granted value-namespace URIs, sorted. For
// diagnostics and for the error a document gets when it asks for one
// that was not granted.
func RegisteredValues() []string {
	valuesMu.RLock()
	defer valuesMu.RUnlock()
	uris := make([]string, 0, len(valueProviders))
	for u := range valueProviders {
		uris = append(uris, u)
	}
	sort.Strings(uris)
	return uris
}

func lookupValueProvider(uri string) (ValueProvider, bool) {
	valuesMu.RLock()
	defer valuesMu.RUnlock()
	p, ok := valueProviders[uri]
	return p, ok
}

// valueHandle resolves a parsed expression in a VALUE position against
// the document's namespace table and the value registry, then asks the
// provider for its handle. Every failure here is a load-time failure.
func (ctx *Context) valueHandle(x *handlerExpr) (*prop.Property[string], error) {
	uri, ok := ctx.ns[x.Prefix]
	if !ok {
		return nil, fmt.Errorf("markup: undeclared namespace prefix %q — add xmlns:%s=\"…\" to the root element", x.Prefix, x.Prefix)
	}
	p, ok := lookupValueProvider(uri)
	if !ok {
		// The most common way to get here is reaching for a namespace
		// that exists but is event-only, so say which half is missing
		// rather than only which half was asked for.
		if _, isHandler := lookupProvider(uri); isHandler {
			return nil, fmt.Errorf("markup: {{%s:%s …}} is in a value position, but %q is registered as a HANDLER namespace (event-only): invoke it from an event attribute, as Click=\"{{%s:%s … | into .Target}}\"",
				x.Prefix, x.Fn, uri, x.Prefix, x.Fn)
		}
		return nil, fmt.Errorf("markup: namespace %q (prefix %q) has no registered value provider; the host app must call markup.RegisterValues(%q, …). Granted: %v",
			uri, x.Prefix, uri, RegisteredValues())
	}
	if x.Into != "" {
		return nil, fmt.Errorf("markup: {{%s:%s … | into .%s}}: a value expression delivers its result by BEING the binding — drop the `| into` stage, or move the call to an event attribute",
			x.Prefix, x.Fn, x.Into)
	}

	args := make([]Arg, len(x.Args))
	for i, a := range x.Args {
		arg, err := ctx.resolveArg(a)
		if err != nil {
			return nil, fmt.Errorf("markup: {{%s:%s …}} argument %d: %w", x.Prefix, x.Fn, i+1, err)
		}
		args[i] = arg
	}

	h, err := p.NewValue(&Call{
		Prefix: x.Prefix, URI: uri, Fn: x.Fn,
		Args: args, Ctx: ctx, Dispatcher: ctx.Dispatcher,
	})
	if err != nil {
		return nil, fmt.Errorf("markup: {{%s:%s …}}: %w", x.Prefix, x.Fn, err)
	}
	if h == nil {
		return nil, fmt.Errorf("markup: value provider for %q returned no handle for %s", uri, x.Fn)
	}
	return h, nil
}

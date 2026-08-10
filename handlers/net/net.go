// Package nethandlers is the first handler namespace: HTTP from markup,
// with no app code.
//
//	<Gooey xmlns:net="gooey.dev/handlers/net">
//	  <Button Content="fetch" Click="{{net:Get .Url | into .Body}}"/>
//
// The host app grants the capability by registering the provider:
//
//	markup.RegisterHandlers(nethandlers.URI, nethandlers.New())
//
// Without that line the same markup fails to load, naming the URI it
// wanted — which is the whole point of registration-as-capability.
//
// v1 shape: one function (Get), body delivered as a string, failures
// delivered to the same target as an "ERROR: …" string. A page can
// therefore show what went wrong without a second binding; a status/err
// split is a later revision of the pipeline grammar, not of this
// provider.
package nethandlers

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/markup"
)

// URI is the namespace URI markup declares to reach this provider.
const URI = "gooey.dev/handlers/net"

// NameGet is the one function the namespace provides in v1.
const NameGet = "Get"

// AllNames lists every function the provider resolves — the pack's
// invocable inventory as data, per
// docs/specs/2026-08-10-pack-distribution.md.
func AllNames() []string { return []string{NameGet} }

// DefaultMaxBody caps what a response can put into a property. A
// terminal shows a screenful; a runaway body should not become the
// application's memory profile.
const DefaultMaxBody = 1 << 20

// Provider implements markup.HandlerProvider for the net namespace.
type Provider struct {
	client  *http.Client
	maxBody int64
}

// Option configures a Provider.
type Option func(*Provider)

// WithClient supplies the http.Client used for requests — the seam for
// tests, proxies, and auth-carrying transports. The app decides what
// the capability actually reaches; markup only names a URL.
func WithClient(c *http.Client) Option { return func(p *Provider) { p.client = c } }

// WithMaxBody caps the bytes read from a response.
func WithMaxBody(n int64) Option { return func(p *Provider) { p.maxBody = n } }

// New builds the provider. Register it to grant the namespace.
func New(opts ...Option) *Provider {
	p := &Provider{
		client:  &http.Client{Timeout: 30 * time.Second},
		maxBody: DefaultMaxBody,
	}
	for _, o := range opts {
		o(p)
	}
	return p
}

// NewCommand resolves one {{net:…}} expression at load time.
func (p *Provider) NewCommand(c *markup.Call) (gooey.Command, error) {
	switch c.Fn {
	case NameGet:
		return p.get(c)
	}
	return nil, fmt.Errorf("unknown function %q; net provides: %s", c.Fn, strings.Join(AllNames(), ", "))
}

func (p *Provider) get(c *markup.Call) (gooey.Command, error) {
	if len(c.Args) != 1 {
		return nil, fmt.Errorf("Get takes 1 argument (the URL), got %d", len(c.Args))
	}
	if !c.Target.Valid() {
		return nil, fmt.Errorf("Get needs a result target — add `| into .SomeProperty`")
	}
	src, target := c.Args[0], c.Target
	return func() {
		// Read the argument handle HERE: the command runs on the UI
		// goroutine, where touching properties is legal. The value, not
		// the handle, is what crosses to the request goroutine.
		u := src.String()
		go func() { target.Deliver(p.fetch(u)) }()
	}, nil
}

// fetch performs the request off the UI goroutine and renders both
// outcomes as the string the target property will hold.
func (p *Provider) fetch(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "ERROR: " + raw + ": " + err.Error()
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Sprintf("ERROR: %s: net:Get speaks http and https, not %q", raw, parsed.Scheme)
	}
	resp, err := p.client.Get(raw)
	if err != nil {
		return "ERROR: " + err.Error()
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, p.maxBody))
	if err != nil {
		return fmt.Sprintf("ERROR: GET %s: %s", raw, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Sprintf("ERROR: GET %s: %s", raw, resp.Status)
	}
	return string(body)
}

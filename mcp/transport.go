package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/WonderForgeLabs/gooey/control"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// The transport is the official SDK's: modelcontextprotocol/go-sdk owns
// the JSON-RPC framing, the initialize handshake, protocol-version
// negotiation, tools/list paging and the streamable-HTTP rules, and this
// file is only the wiring plus the one thing the SDK deliberately does
// not do (see originGuard).
//
// The handler runs STATELESS. That is not a downgrade: there is exactly
// one app behind this server and it is the same app for every client, so
// a session has nothing to hold. Stateless means each POST is
// independent — the SDK synthesizes the initialized state for a request
// that arrives without a handshake, which keeps `tools/call` usable from
// a one-shot client such as curl — and it is the direction the spec
// itself is moving (SEP-2567). GET and DELETE are answered 405: nothing
// here is server-initiated, so there is no stream to hold open and no
// session to delete.
//
// Responses are application/json rather than SSE (JSONResponse), because
// every tool is a request/response round trip that finishes in a
// millisecond on the UI loop.

const endpointPath = "/mcp"

const instructions = "This server drives a running gooey terminal app. " +
	"tree_snapshot and screen_text show what is on screen; list_values shows the bindable " +
	"state and list_styles the registered style names; invoke_command, set_value, " +
	"send_keys, send_mouse and focus act on it; register_properties grows the bindable " +
	"state with new typed source properties; swap_markup replaces the whole page, " +
	"optionally registering new properties first so the new page can bind them; " +
	"patch_markup replaces one named element's subtree, and validate_markup checks markup " +
	"against the live context without touching the app. Every call runs on the app's UI " +
	"goroutine and returns after the next frame has been composed, so a read taken right " +
	"after a write sees the write."

// newSDKServer builds the protocol-side server. Tools are attached to it
// by register as they are registered on ours.
func newSDKServer(name, version string) *mcpsdk.Server {
	return mcpsdk.NewServer(
		&mcpsdk.Implementation{Name: name, Version: version},
		&mcpsdk.ServerOptions{Instructions: instructions},
	)
}

// Handler is the http.Handler serving this server's MCP endpoint. It
// accepts any path, so both /mcp and / reach it — the endpoint is on
// loopback and belongs to one app, so path routing would only be a way to
// get the URL wrong.
//
// Each call builds a fresh handler, which is safe because a stateless one
// holds nothing between requests; the tools and the app are behind s.sdk,
// which is shared.
func (s *Server) Handler() http.Handler {
	h := mcpsdk.NewStreamableHTTPHandler(
		func(*http.Request) *mcpsdk.Server { return s.sdk },
		&mcpsdk.StreamableHTTPOptions{Stateless: true, JSONResponse: true},
	)
	return s.originGuard(h)
}

// bindTool publishes one of our tools on the SDK server.
//
// It uses Server.AddTool — the explicit path — rather than the top-level
// generic AddTool, which would derive a schema from a Go struct through
// github.com/google/jsonschema-go by reflection. The schemas here are
// hand-written and already exist; going through the reflective path would
// mean rewriting nine tools' arguments as structs and inheriting the
// SDK's validation messages in place of this package's, for no gain. The
// SDK's own reflection is accepted at this boundary; there is no reason
// to invoke it where a literal already says the same thing.
//
// The handler is a thin envelope: decode the arguments object, hand the
// tool to the bridge (which is what puts it on the UI goroutine), and
// turn what comes back into content. A tool that RAN and FAILED is a
// normal result with isError set — that text is for the agent to read and
// act on, and a JSON-RPC error would be swallowed as a transport fault.
// An unknown tool, by contrast, never reaches here: the SDK answers it
// with a JSON-RPC InvalidParams, which is the right shape, because the
// client asked for something that does not exist.
func (s *Server) bindTool(t *Tool) {
	schema := t.Schema
	if schema == nil {
		schema = object(map[string]any{})
	}
	sdkTool := &mcpsdk.Tool{Name: t.Name, Description: t.Description, InputSchema: schema}
	if t.OutputSchema != nil {
		sdkTool.OutputSchema = t.OutputSchema
	}
	s.sdk.AddTool(
		sdkTool,
		func(_ context.Context, req *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
			var in args
			if raw := req.Params.Arguments; len(raw) > 0 && string(raw) != "null" {
				if err := json.Unmarshal(raw, &in); err != nil {
					return nil, &jsonrpc.Error{
						Code:    jsonrpc.CodeInvalidParams,
						Message: fmt.Sprintf("arguments for %s must be a JSON object: %v", t.Name, err),
					}
				}
			}
			out, err := s.call(t, in)
			if err != nil {
				return textResult(err.Error(), true), nil
			}
			res := textResult(renderResult(out), false)
			// A tool with a published output schema returns its result
			// twice, per the spec's guidance: structuredContent for
			// schema-checked consumption, and the same JSON as text for
			// clients that only read text. `out` is plain data built on the
			// UI goroutine; serializing it here is safe because the bridge's
			// channel established the happens-before edge.
			if t.OutputSchema != nil {
				res.StructuredContent = out
			}
			return res, nil
		},
	)
}

// call is the crossing. The tool body never runs here — it is handed to
// control.Bridge and executed on the UI goroutine — and the value it
// produces is picked up afterwards, which is safe because the channel
// inside the bridge establishes the happens-before edge between the two
// goroutines. Bridge.Do also waits for the settle barrier: the response
// goes out only after the frame the call's Sets asked for has been
// composed, which is what lets screen_text be called immediately after
// invoke_command and see the new pixels.
func (s *Server) call(t *Tool, in args) (any, error) {
	var out any
	err := s.ui.Do(func() error {
		v, err := t.Run(in)
		if err != nil {
			return err
		}
		out = v
		return nil
	})
	if err != nil {
		return nil, toolError(err)
	}
	return out, nil
}

// toolError maps a service failure to the string this tool surface has
// always reported. The control package names its own operations in its
// messages (ListValues, SnapshotTree, PatchMarkup, SendKeys); an MCP
// client can only call TOOLS, so the adapter — which owns the tool-name
// wording — rewords exactly those references and nothing else. Bridge
// failures (timeout, panic) and the adapter's own argument errors pass
// through untouched.
func toolError(err error) error {
	var ce *control.Error
	if !errors.As(err, &ce) {
		return err
	}
	return errors.New(toolWording.Replace(ce.Msg))
}

// toolWording rewrites full phrases rather than bare names, so a user
// value that happens to contain a service-method name (a property named
// ListValues, say) is never rewritten along with it.
var toolWording = strings.NewReplacer(
	"; ListValues shows", "; list_values shows",
	"; SnapshotTree lists the named elements", "; tree_snapshot lists the named elements",
	"; PatchMarkup replaces", "; patch_markup replaces",
	"which PatchMarkup cannot rewrite", "which patch_markup cannot rewrite",
	"SendKeys needs text or gestures", "send_keys needs text or keys",
)

func textResult(text string, isError bool) *mcpsdk.CallToolResult {
	return &mcpsdk.CallToolResult{
		Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: text}},
		IsError: isError,
	}
}

// renderResult turns a tool's return value into the text an agent reads.
// Strings go through unchanged (screen_text is a screenshot, not JSON);
// everything else is indented JSON.
func renderResult(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprintf("could not encode result: %v", err)
	}
	return string(b)
}

// ---- the origin guard ----

// originGuard wraps the SDK handler with this package's Origin check.
//
// The SDK does two things here and neither is this. It rejects a
// loopback-served request whose Host header is not loopback (its DNS
// rebinding guard, on by default, and kept), and it has an optional
// CrossOriginProtection hook that is nil by default in v1.7.0 — so out of
// the box NO Origin checking happens at all. Its own documentation says
// to wrap the handler, which is what this does.
//
// The stricter rule is deliberate. net/http's CrossOriginProtection is a
// Sec-Fetch-Site/Origin check aimed at CSRF against a site; the threat
// here is a page in the user's browser reaching a port that can type into
// their terminal, so the check also pins the port, which is what stops a
// DIFFERENT local service's page from driving this app.
func (s *Server) originGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.originAllowed(r) {
			http.Error(w, "forbidden origin", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// originAllowed is the DNS-rebinding guard, and it is default-DENY for
// anything that claims to be a browser.
//
// The threat is specific and realistic: a page the user visits resolves
// a hostname it controls to 127.0.0.1, then POSTs here. The only thing
// standing between that page and a tool surface that can type into the
// user's terminal is this function, so an Origin that cannot be proven
// same-app is refused rather than parsed generously.
//
// Absent entirely means "not a browser" — Go and Node HTTP clients do
// not set Origin — and is the one allowed case that skips the checks.
// Present-but-anything-else must pass all of them.
func (s *Server) originAllowed(r *http.Request) bool {
	vs := r.Header.Values("Origin")
	switch len(vs) {
	case 0:
		return true // no Origin at all: a non-browser client
	case 1:
	default:
		return false // more than one Origin is malformed; do not guess
	}
	return checkOrigin(vs[0], s.expectedPort(r))
}

// checkOrigin decides one Origin against the port a same-app page would
// have been served from. It is a free function so the table test can
// enumerate the cases without standing up a listener.
//
// Every reject here is a real bypass shape, not a hypothetical:
//
//   - "null" is what a sandboxed iframe, a file:// page and some
//     cross-origin redirects send. It must never be treated as absent.
//   - Unparseable junk ("garbage"), an empty value, and file:// URLs all
//     parse to an EMPTY hostname. An earlier version of this function
//     allowed the empty hostname, which made every one of them pass —
//     the whole guard, bypassed by three characters.
//   - "//localhost:7788" has no scheme but DOES parse to hostname
//     "localhost", so the scheme allowlist is load-bearing rather than
//     decorative.
//   - "http://localhost:7788@evil.com" parses to host evil.com — the
//     userinfo trick — and "http://127.0.0.1.evil.com" is a DNS name
//     that merely starts like a loopback address. Exact hostname
//     matching is what rejects both; prefix or suffix matching would
//     not.
func checkOrigin(origin, wantPort string) bool {
	if strings.TrimSpace(origin) == "" || strings.EqualFold(strings.TrimSpace(origin), "null") {
		return false
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
	default:
		return false
	}
	switch u.Hostname() {
	case "localhost", "127.0.0.1", "::1":
	default:
		return false
	}
	// The port rule is what stops a DIFFERENT local service's page —
	// a dev server on :3000, another app's console — from driving this
	// app just because it also happens to be on loopback. It applies
	// only when the port we are reachable at is knowable; a handler
	// mounted behind a proxy by an embedding host is that host's call.
	if wantPort != "" && u.Port() != wantPort {
		return false
	}
	return true
}

// expectedPort is the port a legitimate same-app page would have been
// served from: our own listener when we own one, and otherwise the port
// the client actually connected to, for a Handler someone mounted
// themselves. s.ln is written once in Serve before any request can
// arrive, so reading it from a handler goroutine needs no lock.
func (s *Server) expectedPort(r *http.Request) string {
	if s.ln != nil {
		if _, port, err := net.SplitHostPort(s.ln.Addr().String()); err == nil {
			return port
		}
	}
	if _, port, err := net.SplitHostPort(r.Host); err == nil {
		return port
	}
	return ""
}

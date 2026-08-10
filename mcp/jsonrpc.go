package mcp

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
)

// The transport: MCP streamable HTTP, cut down to what an app-control
// server needs.
//
// A client POSTs one JSON-RPC message (or a batch) to the endpoint and
// gets its response as application/json. The spec also allows the server
// to answer with an SSE stream, and allows a long-lived GET stream for
// server-initiated messages; neither is implemented, because nothing
// here is server-initiated — every tool is a request/response round trip
// that finishes in a millisecond on the UI loop. GET is answered 405,
// which is the documented way to say "this server has no stream".
//
// Sessions are minted at initialize and echoed in Mcp-Session-Id. They
// carry no state — there is exactly one app behind this server, and it is
// the same app for every client — so a request without one is served
// anyway. The id exists so a client that tracks sessions is not
// surprised, and so DELETE has something to delete.

const (
	endpointPath = "/mcp"

	// The protocol versions this server speaks. A client's requested
	// version is echoed when it is one of these; otherwise it is answered
	// with the default, which the spec says the client may then accept or
	// disconnect over.
	defaultProtocolVersion = "2025-06-18"
)

var supportedProtocolVersions = []string{
	"2025-11-25",
	"2025-06-18",
	"2025-03-26",
	"2024-11-05",
}

// JSON-RPC 2.0 error codes used here.
const (
	codeParseError     = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
	codeInternalError  = -32603
)

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// isNotification reports a message with no id: the client wants no
// answer, and per JSON-RPC we must not send one.
func (r rpcRequest) isNotification() bool { return len(r.ID) == 0 || string(r.ID) == "null" }

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func result(id json.RawMessage, v any) rpcResponse {
	return rpcResponse{JSONRPC: "2.0", ID: id, Result: v}
}

func failure(id json.RawMessage, code int, msg string) rpcResponse {
	return rpcResponse{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: msg}}
}

// Handler is the http.Handler serving this server's MCP endpoint. It
// accepts any path, so both /mcp and / reach it — the endpoint is on
// loopback and belongs to one app, so path routing would only be a way to
// get the URL wrong.
func (s *Server) Handler() http.Handler { return http.HandlerFunc(s.serveHTTP) }

func (s *Server) serveHTTP(w http.ResponseWriter, r *http.Request) {
	// Origin checking is the loopback trust boundary, and it matters more
	// here than on an ordinary local server: a page in the user's browser
	// that can reach this port can drive their terminal.
	if !s.originAllowed(r) {
		http.Error(w, "forbidden origin", http.StatusForbidden)
		return
	}
	switch r.Method {
	case http.MethodPost:
		s.servePost(w, r)
	case http.MethodGet:
		// No server-initiated stream. 405 with Allow is how the spec says
		// to decline it.
		w.Header().Set("Allow", "POST, DELETE")
		http.Error(w, "this server has no server-initiated stream", http.StatusMethodNotAllowed)
	case http.MethodDelete:
		if id := r.Header.Get("Mcp-Session-Id"); id != "" {
			s.mu.Lock()
			delete(s.sessions, id)
			s.mu.Unlock()
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		w.Header().Set("Allow", "POST, DELETE")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) servePost(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 8<<20))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, failure(nil, codeParseError, "cannot read body: "+err.Error()))
		return
	}
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		writeJSON(w, http.StatusBadRequest, failure(nil, codeInvalidRequest, "empty request"))
		return
	}

	batch := trimmed[0] == '['
	var reqs []rpcRequest
	if batch {
		if err := json.Unmarshal(body, &reqs); err != nil {
			writeJSON(w, http.StatusBadRequest, failure(nil, codeParseError, err.Error()))
			return
		}
	} else {
		var one rpcRequest
		if err := json.Unmarshal(body, &one); err != nil {
			writeJSON(w, http.StatusBadRequest, failure(nil, codeParseError, err.Error()))
			return
		}
		reqs = []rpcRequest{one}
	}

	var out []rpcResponse
	for _, req := range reqs {
		resp, answer := s.dispatch(w, req)
		if answer {
			out = append(out, resp)
		}
	}
	if len(out) == 0 {
		// Notifications only: acknowledged with no body, per the spec.
		w.WriteHeader(http.StatusAccepted)
		return
	}
	if batch {
		writeJSON(w, http.StatusOK, out)
		return
	}
	writeJSON(w, http.StatusOK, out[0])
}

// dispatch handles one JSON-RPC message. It reports whether a response
// should be sent — notifications get none.
func (s *Server) dispatch(w http.ResponseWriter, req rpcRequest) (rpcResponse, bool) {
	if req.JSONRPC != "" && req.JSONRPC != "2.0" {
		return failure(req.ID, codeInvalidRequest, "jsonrpc must be \"2.0\""), !req.isNotification()
	}
	switch req.Method {
	case "initialize":
		resp := s.initialize(req)
		// The session id has to go out on the response headers, so it is
		// minted here rather than in the result body.
		w.Header().Set("Mcp-Session-Id", s.newSession())
		return resp, true
	case "notifications/initialized", "notifications/cancelled":
		return rpcResponse{}, false
	case "ping":
		return result(req.ID, map[string]any{}), !req.isNotification()
	case "tools/list":
		return result(req.ID, map[string]any{"tools": s.toolDescriptors()}), !req.isNotification()
	case "tools/call":
		return s.toolsCall(req), !req.isNotification()
	}
	if req.isNotification() {
		return rpcResponse{}, false
	}
	return failure(req.ID, codeMethodNotFound, "unknown method "+req.Method), true
}

func (s *Server) initialize(req rpcRequest) rpcResponse {
	var p struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if len(req.Params) > 0 {
		_ = json.Unmarshal(req.Params, &p)
	}
	version := defaultProtocolVersion
	for _, v := range supportedProtocolVersions {
		if v == p.ProtocolVersion {
			version = v
			break
		}
	}
	return result(req.ID, map[string]any{
		"protocolVersion": version,
		"capabilities":    map[string]any{"tools": map[string]any{}},
		"serverInfo":      map[string]any{"name": s.name, "version": s.version},
		"instructions": "This server drives a running gooey terminal app. " +
			"tree_snapshot and screen_text show what is on screen; list_values shows the bindable " +
			"state; invoke_command, set_value, send_keys, send_mouse and focus act on it; " +
			"swap_markup replaces the whole page. Every call runs on the app's UI goroutine and " +
			"returns after the next frame has been composed, so a read taken right after a write " +
			"sees the write.",
	})
}

// toolsCall is the one method that reaches the app. Everything about the
// crossing lives in Server.call; this is just the protocol envelope.
//
// The two failure kinds are different on purpose. An unknown tool is a
// PROTOCOL error — the client asked for something that does not exist —
// and comes back as a JSON-RPC error. A tool that ran and failed (no such
// element, wrong type, markup that will not parse) comes back as a normal
// result with isError set, because that text is for the agent to read and
// act on, and a JSON-RPC error would be swallowed as a transport fault.
func (s *Server) toolsCall(req rpcRequest) rpcResponse {
	var p struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return failure(req.ID, codeInvalidParams, err.Error())
		}
	}
	if p.Name == "" {
		return failure(req.ID, codeInvalidParams, "tools/call needs a name")
	}
	t, ok := s.byName[p.Name]
	if !ok {
		return failure(req.ID, codeInvalidParams, "unknown tool "+p.Name)
	}
	out, err := s.call(t, p.Arguments)
	if err != nil {
		return result(req.ID, toolResult(err.Error(), true))
	}
	return result(req.ID, toolResult(renderResult(out), false))
}

// call is the crossing. The tool body never runs here — it is handed to
// the bridge and executed on the UI goroutine — and the value it produces
// is picked up afterwards, which is safe because the channel inside
// bridge.round establishes the happens-before edge between the two
// goroutines.
func (s *Server) call(t *Tool, in map[string]any) (any, error) {
	var out any
	err := s.ui.do(func() error {
		v, err := t.Run(args(in))
		if err != nil {
			return err
		}
		out = v
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func toolResult(text string, isError bool) map[string]any {
	return map[string]any{
		"content": []any{map[string]any{"type": "text", "text": text}},
		"isError": isError,
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

func (s *Server) toolDescriptors() []any {
	out := make([]any, 0, len(s.tools))
	for _, t := range s.tools {
		schema := t.Schema
		if schema == nil {
			schema = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		out = append(out, map[string]any{
			"name":        t.Name,
			"description": t.Description,
			"inputSchema": schema,
		})
	}
	return out
}

func (s *Server) newSession() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "gooey"
	}
	id := hex.EncodeToString(b[:])
	s.mu.Lock()
	s.sessions[id] = struct{}{}
	s.mu.Unlock()
	return id
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

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

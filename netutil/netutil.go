// Package netutil holds the bind-address checks that gooey's servers and
// the apps embedding them share. It is deliberately small and deliberately
// public: the one check in it was copied by hand into four places before
// this package existed, and it was unexported in three of them, which is
// precisely why the fourth copy got written.
//
// Nothing here touches the property graph. These checks run before
// App.Run, on whatever goroutine parses flags, so the UI-goroutine
// confinement rule has no bearing on them.
//
// It imports only the standard library — `net` and `fmt` — which is what
// lets the root module keep exactly two direct requirements while `mcp`,
// `grpc` and both demo apps all call into it.
package netutil

import (
	"fmt"
	"net"
)

// CheckLoopback refuses a bind that is not confined to the loopback
// interface. It is the whole of v1's security posture for the MCP and
// gRPC servers, and it is deliberately a hard error rather than a
// warning: there is no token auth yet, so a server reachable from the
// network is a remote-control handle on the user's terminal. Remote binds
// arrive with authentication or not at all.
//
// what is a short label naming the caller and, where there is one, the
// flag the address came from — "gooey/mcp", "kanban -mcp",
// "dynamic-activities -activity-mcp". It becomes the error's prefix, so
// the message stays specific to the site that rejected the address.
//
// addr must be in host:port form, as net.Listen takes it. The rules, in
// the order they are applied:
//
//   - anything net.SplitHostPort rejects — a bare "127.0.0.1" with no
//     port, a bare "localhost", a path, a "unix:" URL — is an error. A
//     missing port is not a loopback bind with a detail left out; it is a
//     different kind of address, and this package only knows one kind.
//   - an EMPTY host (":8080", ":0", "") is the dangerous case and gets its
//     own message: it binds every interface, which is the exact exposure
//     this function exists to prevent. It must never be waved through as
//     "no host specified".
//   - the literal "localhost" is accepted, and only that spelling. Any
//     other name is refused WITHOUT resolving it, including names that do
//     resolve to loopback ("localhost.", "localhost.localdomain",
//     "ip6-localhost"). Resolution is not a security boundary — it is
//     attacker-influenced through /etc/hosts, DNS and search domains —
//     and a name that merely starts like one ("127.0.0.1.evil.com") must
//     not get near an IsLoopback test.
//   - everything else must parse as an IP literal and satisfy
//     net.IP.IsLoopback: 127.0.0.0/8, ::1, and the IPv4-mapped forms of
//     both. A zone ("fe80::1%eth0") fails to parse and is refused.
//
// Port VALIDITY is not checked here, only presence: "127.0.0.1:abc" and
// "127.0.0.1:99999" pass, because they are unambiguously loopback and
// net.Listen is the authority on ports. Do not add a port check without
// re-reading the callers — several pass port 0 on purpose so the kernel
// picks one.
func CheckLoopback(what, addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("%s: bad address %q: %w", what, addr, err)
	}
	if host == "" {
		return fmt.Errorf("%s: %q binds every interface; loopback only (use 127.0.0.1:port)", what, addr)
	}
	if host == "localhost" {
		return nil
	}
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("%s: %q is not a loopback address; these servers have no authentication, so remote binds are refused", what, addr)
	}
	return nil
}

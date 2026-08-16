package netutil

import (
	"errors"
	"net"
	"strings"
	"testing"
)

// TestCheckLoopback is a table rather than a handful of spot checks for
// the same reason mcp's TestCheckOrigin is: the interesting cases are the
// ones that LOOK inert. ":8080" reads as "no host given" and is in fact
// "every interface"; "localhost.localdomain" resolves to 127.0.0.1 on
// most machines and is still refused; "127.0.0.1.evil.com" is a name that
// merely starts like an address. This function is the entire perimeter
// for two unauthenticated servers, so every row here is a claim about
// what an attacker cannot reach.
//
// Before this package existed the same rules lived in four hand-copied
// implementations (mcp, grpc, apps/kanban, apps/dynamic-activities). Each
// row below was checked against all four; they agreed on every one, so
// this table is their union and not a relaxation of any of them.
func TestCheckLoopback(t *testing.T) {
	cases := []struct {
		addr string
		ok   bool
		why  string
	}{
		// --- accepted: unambiguously loopback, with a port ---
		{"127.0.0.1:0", true, "the canonical bind; port 0 lets the kernel pick"},
		{"127.0.0.1:7788", true, "a fixed port a client is registered against"},
		{"127.0.0.2:80", true, "all of 127.0.0.0/8 is loopback, not just .1"},
		{"127.255.255.254:80", true, "the top of 127.0.0.0/8"},
		{"localhost:0", true, "the one name accepted, and only this spelling"},
		{"[::1]:0", true, "loopback by v6 literal"},
		{"[0:0:0:0:0:0:0:1]:0", true, "the same v6 address written out"},
		{"[::ffff:127.0.0.1]:0", true, "v4-mapped v6 is still 127.0.0.1"},
		{"[::ffff:7f00:1]:0", true, "and in hex, which is the spelling that hides it"},
		{"[127.0.0.1]:80", true, "brackets around a v4 literal still split cleanly"},

		// --- refused: every interface. The whole point of the check. ---
		{"", false, "empty is not 'unset', it is 'no host and no port'"},
		{":0", false, "empty host binds every interface even on an ephemeral port"},
		{":8080", false, "the shape a Go tutorial teaches, and the exposure this exists to stop"},
		{"0.0.0.0:0", false, "the wildcard spelled out"},
		{"[::]:0", false, "the v6 wildcard, which also accepts v4 on most stacks"},

		// --- refused: routable ---
		{"8.8.8.8:80", false, "a public address"},
		{"192.168.1.10:0", false, "an RFC1918 LAN address is NOT loopback"},
		{"10.0.0.1:0", false, "nor is any other private range"},
		{"169.254.1.1:0", false, "link-local is reachable from the segment"},
		{"[fe80::1]:80", false, "v6 link-local likewise"},

		// --- refused: names. Never resolved, however loopback they look. ---
		{"example.com:7788", false, "a plain foreign name"},
		{"localhost.:0", false, "the fully-qualified root form resolves to loopback and is still refused"},
		{"localhost.localdomain:0", false, "resolves to 127.0.0.1 on most hosts; resolution is not a boundary"},
		{"ip6-localhost:0", false, "same, via /etc/hosts"},
		{"LOCALHOST:0", false, "the accepted spelling is exact; case is not folded"},
		{"Localhost:0", false, "nor mixed case"},
		{"127.0.0.1.evil.com:80", false, "a name that merely starts like an address"},
		{"localhost.evil.com:80", false, "and one that merely starts like the name"},

		// --- refused: not host:port at all ---
		{"127.0.0.1", false, "a bare address with no port"},
		{"localhost", false, "a bare name with no port"},
		{"::1:0", false, "an unbracketed v6 literal is ambiguous and SplitHostPort says so"},
		{"::ffff:127.0.0.1", false, "likewise unbracketed"},
		{"/tmp/gooey.sock", false, "a unix socket path is a different kind of address"},
		{"unix:/tmp/gooey.sock", false, "and so is a scheme-qualified one"},
		{"127.0.0.1:80:81", false, "two ports"},
		{" 127.0.0.1:0", false, "a leading space is not trimmed into a valid host"},
		{"127.0.0.1 :0", false, "nor a trailing one"},

		// --- refused: obfuscated v4, which net.ParseIP rejects outright ---
		{"0x7f.0.0.1:80", false, "hex octets are not a Go IP literal"},
		{"0177.0.0.1:80", false, "octal octets likewise"},
		{"2130706433:80", false, "the 32-bit integer form of 127.0.0.1 parses as a hostname, not an IP"},

		// --- refused: a zone makes the literal unparseable, and a zoned
		// address is an interface-scoped bind rather than a loopback one ---
		{"[fe80::1%eth0]:80", false, "zoned link-local"},
		{"[::1%lo0]:0", false, "even a zoned loopback: ParseIP rejects the zone, so it is refused"},
	}

	for _, tc := range cases {
		err := CheckLoopback("test", tc.addr)
		if tc.ok && err != nil {
			t.Errorf("CheckLoopback(%q) = %v, want nil — %s", tc.addr, err, tc.why)
			continue
		}
		if !tc.ok && err == nil {
			t.Errorf("CheckLoopback(%q) = nil, want an error — %s", tc.addr, tc.why)
			continue
		}
		if err != nil && !strings.HasPrefix(err.Error(), "test: ") {
			t.Errorf("CheckLoopback(%q) error %q does not carry the caller's label", tc.addr, err)
		}
	}
}

// TestCheckLoopbackAcceptsWhatNetListenAccepts closes the loop the table
// cannot: an "accepted" row is only meaningful if the address is one a
// server can actually bind. A rule that accepted a string net.Listen
// rejects would be a check that passes and a server that never starts.
func TestCheckLoopbackAcceptsWhatNetListenAccepts(t *testing.T) {
	for _, addr := range []string{
		"127.0.0.1:0", "localhost:0", "[::1]:0", "[::ffff:127.0.0.1]:0",
	} {
		if err := CheckLoopback("test", addr); err != nil {
			t.Fatalf("CheckLoopback(%q) = %v; the table calls this accepted", addr, err)
		}
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			// A v6-less or v4-less machine is a fact about the host, not
			// a failure of the rule.
			t.Logf("net.Listen(%q): %v (skipping; host stack does not support it)", addr, err)
			continue
		}
		bound := ln.Addr().String()
		ln.Close()
		// The resolved address must itself satisfy the rule. This is the
		// property the servers rely on when they report srv.Addr() back
		// to a client.
		if err := CheckLoopback("test", bound); err != nil {
			t.Errorf("listening on %q bound %q, which the rule refuses: %v", addr, bound, err)
		}
	}
}

// TestCheckLoopbackMessagesNameTheSite pins the three distinct
// diagnostics. They are not interchangeable: "binds every interface" is
// the one that tells an operator they just published a control handle,
// and collapsing it into the generic "not a loopback address" is how a
// consolidation quietly loses information.
func TestCheckLoopbackMessagesNameTheSite(t *testing.T) {
	cases := []struct {
		what, addr, want string
	}{
		{"gooey/mcp", ":8080", `gooey/mcp: ":8080" binds every interface`},
		{"kanban -mcp", "8.8.8.8:80", `kanban -mcp: "8.8.8.8:80" is not a loopback address`},
		{"dynamic-activities -activity-mcp", "127.0.0.1", `dynamic-activities -activity-mcp: bad address "127.0.0.1"`},
	}
	for _, tc := range cases {
		err := CheckLoopback(tc.what, tc.addr)
		if err == nil {
			t.Fatalf("CheckLoopback(%q, %q) = nil", tc.what, tc.addr)
		}
		if !strings.HasPrefix(err.Error(), tc.want) {
			t.Errorf("CheckLoopback(%q, %q) = %q, want it to start %q", tc.what, tc.addr, err, tc.want)
		}
	}

	// The parse failure is wrapped, not flattened, so a caller can still
	// reach net.AddrError underneath.
	err := CheckLoopback("test", "127.0.0.1")
	var ae *net.AddrError
	if !errors.As(err, &ae) {
		t.Errorf("CheckLoopback(%q) = %v; the SplitHostPort error must stay wrapped", "127.0.0.1", err)
	}
}

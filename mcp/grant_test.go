package mcp

import (
	"strings"
	"testing"
	"time"

	"github.com/WonderForgeLabs/gooey/control"
	"github.com/WonderForgeLabs/gooey/prop"
)

// MCP inherits island enforcement rather than implementing it: the rule
// lives in control.Service, which every tool body already calls. So what
// needs pinning HERE is not the rule — it is the WIRING. A dropped
// Options.Grant would be silent, and every tool would go back to
// reaching the whole app with nothing failing anywhere.
//
// One tool per axis is enough for that, plus the narrowing, because a
// grant that reached the service at all reaches all of it.

const mcpIslandMarkup = `<Gooey>
  <VStack Gap="0">
    <Border Name="Mine" Title="mine">
      <Text Name="MineText">{{.Mine.Body}}</Text>
    </Border>
    <Text Name="Theirs">{{.Host.Secret}}</Text>
  </VStack>
</Gooey>`

func islandServer(t *testing.T) (*client, *client) {
	t.Helper()
	mine := prop.NewSource("m0")
	secret := prop.NewSource("hunter2")
	app := newTestApp(t, mcpIslandMarkup, map[string]any{
		"Mine": map[string]any{"Body": mine},
		"Host": map[string]any{"Secret": secret},
	})
	guest, err := New(app, Options{
		Context: app.ctx,
		Timeout: 5 * time.Second,
		Grant:   control.Island("Mine", "Mine"),
	})
	if err != nil {
		t.Fatalf("New (guest): %v", err)
	}
	host, err := New(app, Options{Context: app.ctx, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("New (host): %v", err)
	}
	return newClient(t, guest), newClient(t, host)
}

func TestMCPHonorsAnIslandGrant(t *testing.T) {
	guest, host := islandServer(t)

	// The value axis: in-grant writes land, out-of-grant ones are
	// refused with the grant's own wording.
	guest.ok("set_value", map[string]any{"name": "Mine.Body", "value": "ok"})
	guest.fails("set_value", map[string]any{"name": "Host.Secret", "value": "stolen"},
		"outside this session's granted values")

	// The element axis.
	guest.fails("patch_markup", map[string]any{
		"name":   "Theirs",
		"source": `<Gooey><Text Name="Theirs">stolen</Text></Gooey>`,
	}, `outside this session's island "Mine"`)

	// The verb with no scoped form.
	guest.fails("swap_markup", map[string]any{"source": mcpIslandMarkup}, "reassigns every Name=")

	// The host's endpoint on the SAME app still does all three, which is
	// what makes the refusals a property of the grant and not of the app
	// having got into a bad state.
	host.ok("set_value", map[string]any{"name": "Host.Secret", "value": "host wrote this"})
	host.ok("patch_markup", map[string]any{
		"name":   "Theirs",
		"source": `<Gooey><Text Name="Theirs">host wrote this</Text></Gooey>`,
	})
}

func TestMCPNarrowsListValuesAndTree(t *testing.T) {
	guest, host := islandServer(t)

	got := guest.ok("list_values", nil)
	if !strings.Contains(got, "Mine.Body") {
		t.Errorf("scoped list_values hid the granted value:\n%s", got)
	}
	if strings.Contains(got, "Host.Secret") {
		t.Errorf("scoped list_values exposed the host's value:\n%s", got)
	}

	tree := guest.ok("tree_snapshot", nil)
	if !strings.Contains(tree, "MineText") {
		t.Errorf("scoped tree_snapshot hid the island's own child:\n%s", tree)
	}
	if strings.Contains(tree, "Theirs") {
		t.Errorf("scoped tree_snapshot exposed an element outside the island:\n%s", tree)
	}

	// Control arm: the host endpoint sees both.
	full := host.ok("list_values", nil)
	if !strings.Contains(full, "Host.Secret") || !strings.Contains(full, "Mine.Body") {
		t.Errorf("the host endpoint lost values it owns:\n%s", full)
	}
}

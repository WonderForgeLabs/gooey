package grpc

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/control"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/prop"
	grpcgo "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	controlv1 "github.com/WonderForgeLabs/gooey/grpc/gen/gooey/control/v1"
)

// Island grants, enforced HOST-SIDE.
//
// Before control/grant.go, "the editor owns one named element and never
// writes outside it" was a rule the CLIENT followed: every test here
// would have passed trivially, because the server had no opinion at all.
// So each of these is written the way the catalog spec's own rule
// demands — remove the check and the test must go RED — and the A/B for
// every one of them is recorded in the report that accompanied this
// change.
//
// The page has two islands and a strip that belongs to neither, so
// "outside your island" has both a peer and a host flavour.

const islandMarkup = `<Gooey>
  <VStack Gap="0">
    <Border Name="Left" Title="left">
      <VStack>
        <Text Name="LeftText">{{.Left.Body}}</Text>
        <TextBox Name="LeftBox" Text="{{.Left.Body}}"/>
      </VStack>
    </Border>
    <Border Name="Right" Title="right">
      <VStack>
        <Text Name="RightText">{{.Right.Body}}</Text>
        <TextBox Name="RightBox" Text="{{.Right.Body}}"/>
      </VStack>
    </Border>
    <Text Name="HostBar">{{.Host.Secret}}</Text>
  </VStack>
</Gooey>`

type islandVM struct {
	left, right, secret *prop.Property[string]
	danger              int
}

// islandRig is one app with THREE endpoints on it: the host's own
// unscoped one, and one per island. That shape is the design under test —
// registration is the grant, one endpoint carries one grant, and the
// address a guest was handed IS its capability.
type islandRig struct {
	t    *testing.T
	app  *testApp
	vm   *islandVM
	host *clientPair // unscoped
	left *clientPair // Island("Left", "Left")
	rght *clientPair // Island("Right", "Right")
}

type clientPair struct {
	srv  *Server
	ctl  controlv1.ControlServiceClient
	sess controlv1.SessionServiceClient
}

func newIslandRig(t *testing.T) *islandRig {
	t.Helper()
	vm := &islandVM{
		left:   prop.NewSource("L0"),
		right:  prop.NewSource("R0"),
		secret: prop.NewSource("hunter2"),
	}
	values := map[string]any{
		"Left":  map[string]any{"Body": vm.left},
		"Right": map[string]any{"Body": vm.right},
		"Host": map[string]any{
			"Secret": vm.secret,
			"Danger": gooey.Command(func() { vm.danger++ }),
		},
	}
	app := newTestApp(t, islandMarkup, values, nil)
	r := &islandRig{t: t, app: app, vm: vm}
	r.host = endpoint(t, app, nil)
	r.left = endpoint(t, app, control.Island("Left", "Left"))
	r.rght = endpoint(t, app, control.Island("Right", "Right"))
	return r
}

func endpoint(t *testing.T, app *testApp, g *control.Grant) *clientPair {
	t.Helper()
	srv, err := Serve(app, Options{Context: app.ctx, Grant: g, Name: "island-test"})
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	t.Cleanup(srv.Close)
	conn, err := grpcgo.NewClient(srv.Addr(), grpcgo.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return &clientPair{
		srv:  srv,
		ctl:  controlv1.NewControlServiceClient(conn),
		sess: controlv1.NewSessionServiceClient(conn),
	}
}

func (r *islandRig) onUI(fn func()) {
	done := make(chan struct{})
	r.app.Post(func() { fn(); close(done) })
	<-done
}

func fragment(name, body string) string {
	return fmt.Sprintf("<Gooey><Text Name=%q>%s</Text></Gooey>", name, body)
}

func (c *clientPair) patch(name, source string) error {
	_, err := c.ctl.PatchMarkup(context.Background(), &controlv1.PatchMarkupRequest{
		Name: name, Source: source,
	})
	return err
}

func (c *clientPair) set(name, v string) error {
	_, err := c.ctl.SetProperty(context.Background(), &controlv1.SetPropertyRequest{
		Name: name, Value: strVal(v),
	})
	return err
}

// ---- the element axis ----

func TestGrantRefusesPatchingAnotherIsland(t *testing.T) {
	r := newIslandRig(t)

	// Its own: allowed.
	if err := r.left.patch("LeftText", fragment("LeftText", "mine")); err != nil {
		t.Fatalf("patching its own island: %v", err)
	}
	// The island element itself: allowed — the grant is the subtree
	// INCLUDING its root, which is the address the wysiwyg editor uses.
	if err := r.left.patch("Left", `<Gooey><Border Name="Left" Title="left"><Text Name="LeftText">rebuilt</Text></Border></Gooey>`); err != nil {
		t.Fatalf("patching the island root: %v", err)
	}

	// A peer's island, and an element that belongs to neither.
	wantCode(t, r.left.patch("RightText", fragment("RightText", "stolen")),
		codes.PermissionDenied, `outside this session's island "Left"`)
	wantCode(t, r.left.patch("HostBar", fragment("HostBar", "stolen")),
		codes.PermissionDenied, `outside this session's island "Left"`)

	// And the host still reaches everything, which is the control arm:
	// without it a broken grant that refused EVERYTHING would look like
	// a passing enforcement test.
	if err := r.host.patch("RightText", fragment("RightText", "host wrote this")); err != nil {
		t.Fatalf("host patching anything: %v", err)
	}
}

func TestGrantRefusesFocusOutsideTheIsland(t *testing.T) {
	r := newIslandRig(t)
	if _, err := r.left.ctl.SetFocus(context.Background(), &controlv1.SetFocusRequest{Name: "LeftBox"}); err != nil {
		t.Fatalf("focusing its own: %v", err)
	}
	_, err := r.left.ctl.SetFocus(context.Background(), &controlv1.SetFocusRequest{Name: "RightBox"})
	wantCode(t, err, codes.PermissionDenied, `outside this session's island "Left"`)

	// The refusal must not have moved focus as a side effect.
	r.onUI(func() {
		if got := r.app.comp.Focus().Focused(); got != r.app.ctx.Named["LeftBox"] {
			t.Errorf("focus moved on a refused SetFocus: %T", got)
		}
	})
}

// A name that does not exist stays NOT_FOUND even for a scoped session.
// Collapsing every miss into PermissionDenied would make a typo
// indistinguishable from a boundary, and a client cannot act on that.
func TestGrantKeepsNotFoundDistinctFromDenied(t *testing.T) {
	r := newIslandRig(t)
	wantCode(t, r.left.patch("NoSuchName", fragment("NoSuchName", "x")),
		codes.NotFound, "no element named")
}

// ---- the value axis ----

func TestGrantRefusesValuesOutsideTheGrant(t *testing.T) {
	r := newIslandRig(t)

	if err := r.left.set("Left.Body", "mine"); err != nil {
		t.Fatalf("writing its own value: %v", err)
	}
	wantCode(t, r.left.set("Right.Body", "stolen"), codes.PermissionDenied, "outside this session's granted values")
	wantCode(t, r.left.set("Host.Secret", "stolen"), codes.PermissionDenied, "outside this session's granted values")

	r.onUI(func() {
		if r.vm.left.Get() != "mine" {
			t.Errorf("own write did not land: %q", r.vm.left.Get())
		}
		if r.vm.right.Get() != "R0" || r.vm.secret.Get() != "hunter2" {
			t.Errorf("a refused write landed anyway: right=%q secret=%q", r.vm.right.Get(), r.vm.secret.Get())
		}
	})
}

// Commands are values, so a command is granted exactly as a property is.
// A guest whose island merely CONTAINS a button bound to host code has
// not been granted that code.
func TestGrantRefusesInvokingHostCommands(t *testing.T) {
	r := newIslandRig(t)
	_, err := r.left.ctl.InvokeCommand(context.Background(), &controlv1.InvokeCommandRequest{Name: "Host.Danger"})
	wantCode(t, err, codes.PermissionDenied, "outside this session's granted values")
	r.onUI(func() {
		if r.vm.danger != 0 {
			t.Errorf("a refused command ran anyway (%d times)", r.vm.danger)
		}
	})
	if _, err := r.host.ctl.InvokeCommand(context.Background(), &controlv1.InvokeCommandRequest{Name: "Host.Danger"}); err != nil {
		t.Fatalf("host invoking its own command: %v", err)
	}
	r.onUI(func() {
		if r.vm.danger != 1 {
			t.Errorf("host invoke did not run (%d)", r.vm.danger)
		}
	})
}

// TestGrantKeepsValuesNotFoundDistinctFromDenied is the value-axis twin
// of TestGrantKeepsNotFoundDistinctFromDenied, and it exists because the
// element axis had the ordering right while this one did not: the three
// value verbs asked the grant BEFORE they asked whether the name was even
// real, so every unreachable name came back PERMISSION_DENIED.
//
// That is an oracle. Ask for "Host.Secret" and "Host.Sekret" with the
// broken order and both say DENIED; with the right order the second says
// NOT_FOUND, and the difference is the whole point — a guest must not be
// able to sift the host's private surface out of the error codes.
//
// The two arms are what make this a discrimination rather than a
// tautology. A fix that returned NOT_FOUND for everything would pass the
// first arm alone, and it would have destroyed the grant.
func TestGrantKeepsValuesNotFoundDistinctFromDenied(t *testing.T) {
	r := newIslandRig(t)

	get := func(name string) error {
		_, err := r.left.ctl.GetProperty(context.Background(), &controlv1.GetPropertyRequest{Name: name})
		return err
	}
	invoke := func(name string) error {
		_, err := r.left.ctl.InvokeCommand(context.Background(), &controlv1.InvokeCommandRequest{Name: name})
		return err
	}

	// Absent AND ungranted: existence answers first.
	for _, tc := range []struct {
		verb string
		err  error
	}{
		{"GetProperty", get("Host.Sekret")},
		{"SetProperty", r.left.set("Host.Sekret", "x")},
		{"InvokeCommand", invoke("Host.Dangr")},
		{"GetProperty/top-level", get("NoSuchScope")},
	} {
		wantCode(t, tc.err, codes.NotFound, "no value named")
		_ = tc.verb
	}

	// Present but ungranted: the grant still refuses, and says so.
	for _, err := range []error{
		get("Host.Secret"),
		r.left.set("Host.Secret", "x"),
		invoke("Host.Danger"),
	} {
		wantCode(t, err, codes.PermissionDenied, "outside this session's granted values")
	}

	// And an UNSCOPED session sees no change: it was never subject to
	// either check, so the reordering must not have moved its errors.
	_, err := r.host.ctl.GetProperty(context.Background(), &controlv1.GetPropertyRequest{Name: "Host.Sekret"})
	wantCode(t, err, codes.NotFound, "no value named")
}

// Register is the one value verb whose precondition is ABSENCE, so it
// keeps permission-first ordering on purpose: checking existence first
// would refuse every legitimate registration. This pins that the
// resolveValue reordering did not sweep it up — an out-of-grant name that
// does not exist must still be DENIED here, not NOT_FOUND, because
// "does not exist" is exactly what the caller is asking to change.
func TestRegisterKeepsPermissionFirstOrdering(t *testing.T) {
	r := newIslandRig(t)
	_, err := r.left.ctl.RegisterProperties(context.Background(), &controlv1.RegisterPropertiesRequest{
		Properties: []*controlv1.PropertyRegistration{{
			Name: "Host.Minted", Kind: controlv1.ValueKind_VALUE_KIND_STRING,
		}},
	})
	wantCode(t, err, codes.PermissionDenied, "outside this session's granted values")
}

// The prefix rule is by dotted SEGMENT, so a grant of "Left" must not
// spill onto a sibling namespace whose name merely starts with it.
func TestGrantPrefixMatchesWholeSegments(t *testing.T) {
	vm := prop.NewSource("x")
	other := prop.NewSource("y")
	app := newTestApp(t, `<Gooey><Text Name="Only">{{.Left.Body}}</Text></Gooey>`, map[string]any{
		"Left":     map[string]any{"Body": vm},
		"Leftover": map[string]any{"Body": other},
	}, nil)
	ep := endpoint(t, app, control.Island("Only", "Left"))
	if err := ep.set("Left.Body", "ok"); err != nil {
		t.Fatalf("granted name: %v", err)
	}
	wantCode(t, ep.set("Leftover.Body", "nope"), codes.PermissionDenied, "outside this session's granted values")
}

// register_properties is a capability-minting verb: without a scope
// check a guest registers a fresh top-level name and then writes it, and
// the grant bounds only the names that existed at attach time.
func TestGrantRefusesRegisteringOutsideItsNamespace(t *testing.T) {
	r := newIslandRig(t)
	reg := func(c *clientPair, name string) error {
		_, err := c.ctl.RegisterProperties(context.Background(), &controlv1.RegisterPropertiesRequest{
			Properties: []*controlv1.PropertyRegistration{{
				Name: name, Kind: controlv1.ValueKind_VALUE_KIND_STRING,
			}},
		})
		return err
	}
	if err := reg(r.left, "Left.Scratch"); err != nil {
		t.Fatalf("registering inside its namespace: %v", err)
	}
	wantCode(t, reg(r.left, "Escape"), codes.PermissionDenied, "outside this session's granted values")
	r.onUI(func() {
		if _, leaked := r.app.ctx.Values["Escape"]; leaked {
			t.Error("a refused registration created the name anyway")
		}
	})
}

// ---- the escalation that makes value scoping worth anything ----
//
// Refusing SetProperty on .Host.Secret while leaving the BINDING surface
// open would enforce the spelling of an escalation and not the
// escalation: patch a Text bound to the secret into your own island and
// read it off the screen; patch a Button bound to a host command and
// press it.
func TestGrantRefusesMarkupThatBindsOutsideTheGrant(t *testing.T) {
	r := newIslandRig(t)

	err := r.left.patch("LeftText", fragment("LeftText", "{{.Host.Secret}}"))
	wantCode(t, err, codes.PermissionDenied, "outside this session's granted values")

	// The SHAPE matters twice over. It must not be the ordinary load
	// error ("no value named"), because that is the shape of a typo and
	// says nothing about which check fired. And it must not name the
	// value it refused, or a refusal becomes an enumeration of the
	// host's state.
	if strings.Contains(err.Error(), "Secret") {
		t.Errorf("the refusal names the value it hid: %v", err)
	}

	// The same document IS valid for the host, which is what makes the
	// refusal a grant decision rather than bad markup.
	if err := r.host.patch("LeftText", fragment("LeftText", "{{.Host.Secret}}")); err != nil {
		t.Fatalf("host patching the same source: %v", err)
	}

	// A genuinely broken document still fails as INVALID_ARGUMENT, so the
	// classification is a real discrimination and not "scoped sessions
	// always get PermissionDenied".
	wantCode(t, r.left.patch("LeftText", `<Gooey><Text Name="LeftText" NoSuchAttr="1"/></Gooey>`),
		codes.InvalidArgument, "")
}

// ValidateMarkup answers valid/invalid for a bad document, but a grant
// denial is not an answer about the document — it is an answer about the
// session. It stays an error, so a client never records "the target
// rejected my markup" for something no amount of editing will fix.
func TestValidateReportsAGrantDenialAsAnError(t *testing.T) {
	r := newIslandRig(t)

	res, err := r.left.ctl.ValidateMarkup(context.Background(), &controlv1.ValidateMarkupRequest{
		Source: fragment("LeftText", "{{.Left.Body}}"),
	})
	if err != nil || !res.GetValid() {
		t.Fatalf("in-grant markup: valid=%v err=%v (%s)", res.GetValid(), err, res.GetError())
	}

	_, err = r.left.ctl.ValidateMarkup(context.Background(), &controlv1.ValidateMarkupRequest{
		Source: fragment("LeftText", "{{.Host.Secret}}"),
	})
	wantCode(t, err, codes.PermissionDenied, "outside this session's granted values")

	// A merely invalid document is still valid=false with no status.
	res, err = r.left.ctl.ValidateMarkup(context.Background(), &controlv1.ValidateMarkupRequest{
		Source: `<Gooey><Text Name="LeftText" NoSuchAttr="1"/></Gooey>`,
	})
	if err != nil || res.GetValid() || res.GetError() == "" {
		t.Fatalf("invalid document: valid=%v err=%v loadErr=%q", res.GetValid(), err, res.GetError())
	}
}

// Pruning the build context must be restored on EVERY path, success
// included — a pruned Values left committed would silently narrow the
// host's own context, and the failure would show up somewhere else
// entirely.
func TestAScopedBuildLeavesTheHostContextIntact(t *testing.T) {
	r := newIslandRig(t)
	if err := r.left.patch("LeftText", fragment("LeftText", "{{.Left.Body}}")); err != nil {
		t.Fatalf("in-grant patch: %v", err)
	}
	wantCode(t, r.left.patch("LeftText", fragment("LeftText", "{{.Host.Secret}}")),
		codes.PermissionDenied, "")

	r.onUI(func() {
		for _, n := range []string{"Left", "Right", "Host"} {
			if _, ok := r.app.ctx.Values[n]; !ok {
				t.Errorf("scoped build pruned %q out of the host's context permanently", n)
			}
		}
	})
	// And the host can still bind everything afterwards.
	if err := r.host.patch("HostBar", fragment("HostBar", "{{.Host.Secret}}")); err != nil {
		t.Fatalf("host after a scoped build: %v", err)
	}
}

// ---- verbs with no scoped form at all ----

func TestGrantRefusesSwapMarkup(t *testing.T) {
	r := newIslandRig(t)
	_, err := r.left.ctl.SwapMarkup(context.Background(), &controlv1.SwapMarkupRequest{Source: islandMarkup})
	wantCode(t, err, codes.PermissionDenied, "reassigns every Name=")

	// The host may still swap; it owns the page.
	if _, err := r.host.ctl.SwapMarkup(context.Background(), &controlv1.SwapMarkupRequest{Source: islandMarkup}); err != nil {
		t.Fatalf("host swap: %v", err)
	}
}

// PatchMarkup on the composition root degrades to a whole swap, so a
// grant whose island IS the root would be SwapMarkup wearing a patch's
// name.
func TestGrantRefusesPatchingTheCompositionRoot(t *testing.T) {
	vm := prop.NewSource("x")
	app := newTestApp(t, `<Gooey><VStack Name="RootStack"><Text Name="Inner">{{.A}}</Text></VStack></Gooey>`,
		map[string]any{"A": vm}, nil)
	ep := endpoint(t, app, control.Island("RootStack", "A"))
	err := ep.patch("RootStack", `<Gooey><VStack Name="RootStack"><Text Name="Inner">y</Text></VStack></Gooey>`)
	wantCode(t, err, codes.PermissionDenied, "composition root")
}

func TestGrantRefusesTheRunningPagesSchema(t *testing.T) {
	r := newIslandRig(t)
	r.left.srv.svc.Doc = func() []byte { return []byte(islandMarkup) }
	_, err := r.left.ctl.GetDeclaredSchema(context.Background(), &controlv1.GetDeclaredSchemaRequest{})
	wantCode(t, err, codes.PermissionDenied, "cannot ask about the running page's own document")

	// Explicit source is still fine: that is the guest's own document.
	if _, err := r.left.ctl.GetDeclaredSchema(context.Background(), &controlv1.GetDeclaredSchemaRequest{
		Source: fragment("LeftText", "hi"),
	}); err != nil {
		t.Fatalf("explicit source: %v", err)
	}
}

// ---- input, which is not name-addressed ----

func TestGrantSendsKeysOnlyWhileFocusIsInsideTheIsland(t *testing.T) {
	r := newIslandRig(t)
	keys := func(c *clientPair) error {
		_, err := c.ctl.SendKeys(context.Background(), &controlv1.SendKeysRequest{Text: "ab"})
		return err
	}

	// Focus starts nowhere in particular; put it in the peer's island
	// through the HOST endpoint, which is the realistic case — the user
	// clicked over there.
	if _, err := r.host.ctl.SetFocus(context.Background(), &controlv1.SetFocusRequest{Name: "RightBox"}); err != nil {
		t.Fatalf("host focus: %v", err)
	}
	wantCode(t, keys(r.left), codes.PermissionDenied, "keys go where focus is")
	r.onUI(func() {
		if r.vm.right.Get() != "R0" {
			t.Errorf("refused keys reached the peer's TextBox: %q", r.vm.right.Get())
		}
	})

	// Move focus into its own island and the same call is allowed.
	if _, err := r.left.ctl.SetFocus(context.Background(), &controlv1.SetFocusRequest{Name: "LeftBox"}); err != nil {
		t.Fatalf("own focus: %v", err)
	}
	if err := keys(r.left); err != nil {
		t.Fatalf("keys with focus inside the island: %v", err)
	}
	r.onUI(func() {
		if !strings.Contains(r.vm.left.Get(), "ab") {
			t.Errorf("allowed keys did not reach its own TextBox: %q", r.vm.left.Get())
		}
	})
}

func TestGrantSendsPointerOnlyInsideTheIsland(t *testing.T) {
	r := newIslandRig(t)

	// Bounds come from the scoped tree read, which is itself the narrowed
	// view: a guest can locate its own island and nothing else.
	res, err := r.left.ctl.SnapshotTree(context.Background(), &controlv1.SnapshotTreeRequest{Depth: 1})
	if err != nil {
		t.Fatalf("SnapshotTree: %v", err)
	}
	b := res.GetRoot().GetBounds()
	if b == nil || b.GetWidth() == 0 {
		t.Fatalf("island has no bounds: %v", b)
	}
	inside := func() (int32, int32) { return b.GetX() + b.GetWidth()/2, b.GetY() + b.GetHeight()/2 }
	x, y := inside()
	point := func(c *clientPair, x, y int32) error {
		_, err := c.ctl.SendPointer(context.Background(), &controlv1.SendPointerRequest{
			Event: &controlv1.PointerEvent{
				Kind:   controlv1.PointerKind_POINTER_KIND_MOVE,
				X:      x,
				Y:      y,
				Button: controlv1.MouseButton_MOUSE_BUTTON_NONE,
			},
		})
		return err
	}
	if err := point(r.left, x, y); err != nil {
		t.Fatalf("pointing inside its own island: %v", err)
	}
	// Straight down, past the island's own rows, is the peer's.
	wantCode(t, point(r.left, x, b.GetY()+b.GetHeight()+1), codes.PermissionDenied, "would be delivered to")
}

// ---- narrowing rather than refusing ----

func TestGrantNarrowsTreeAndValues(t *testing.T) {
	r := newIslandRig(t)

	tree, err := r.left.ctl.SnapshotTree(context.Background(), &controlv1.SnapshotTreeRequest{})
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	var walk func(*controlv1.TreeNode)
	walk = func(n *controlv1.TreeNode) {
		if n == nil {
			return
		}
		if n.GetName() != "" {
			names[n.GetName()] = true
		}
		for _, c := range n.GetChildren() {
			walk(c)
		}
		for _, a := range n.GetAttached() {
			walk(a)
		}
	}
	walk(tree.GetRoot())
	if tree.GetRoot().GetName() != "Left" {
		t.Errorf("scoped tree root = %q, want the island", tree.GetRoot().GetName())
	}
	for _, hidden := range []string{"Right", "RightText", "RightBox", "HostBar"} {
		if names[hidden] {
			t.Errorf("scoped tree exposed %q", hidden)
		}
	}
	if !names["LeftText"] {
		t.Error("scoped tree hid the island's own child")
	}

	vals, err := r.left.ctl.ListValues(context.Background(), &controlv1.ListValuesRequest{})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, v := range vals.GetValues() {
		seen[v.GetName()] = true
	}
	if !seen["Left.Body"] {
		t.Error("scoped ListValues hid the granted value")
	}
	for _, hidden := range []string{"Right.Body", "Host.Secret", "Host.Danger"} {
		if seen[hidden] {
			t.Errorf("scoped ListValues exposed %q", hidden)
		}
	}
	for _, n := range vals.GetNamed() {
		if n == "Right" || n == "HostBar" || n == "RightBox" {
			t.Errorf("scoped ListValues named %q, outside the island", n)
		}
	}

	// The host still sees everything — the control arm again.
	hostVals, err := r.host.ctl.ListValues(context.Background(), &controlv1.ListValuesRequest{})
	if err != nil {
		t.Fatal(err)
	}
	hostSeen := map[string]bool{}
	for _, v := range hostVals.GetValues() {
		hostSeen[v.GetName()] = true
	}
	if !hostSeen["Host.Secret"] || !hostSeen["Right.Body"] {
		t.Errorf("the host endpoint lost values it owns: %v", hostSeen)
	}
}

func TestGrantCropsTheScreenInBothForms(t *testing.T) {
	r := newIslandRig(t)
	r.onUI(func() { r.vm.secret.Set("SECRETSTRING") })

	full, err := r.host.ctl.ScreenText(context.Background(), &controlv1.ScreenTextRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(full.GetText(), "SECRETSTRING") {
		t.Fatalf("the host read does not show the strip under test:\n%s", full.GetText())
	}

	scoped, err := r.left.ctl.ScreenText(context.Background(), &controlv1.ScreenTextRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(scoped.GetText(), "SECRETSTRING") {
		t.Errorf("a scoped screen read showed a row outside the island:\n%s", scoped.GetText())
	}
	if !strings.Contains(scoped.GetText(), "left") {
		t.Errorf("a scoped screen read lost its own island:\n%s", scoped.GetText())
	}

	// The STYLED form is cropped too, not refused: refusing one flag value
	// while narrowing the other would be an API shape driven by which
	// helper happened to exist.
	styled, err := r.left.ctl.ScreenText(context.Background(), &controlv1.ScreenTextRequest{Styled: true})
	if err != nil {
		t.Fatalf("scoped styled read: %v", err)
	}
	if strings.Contains(styled.GetText(), "SECRETSTRING") {
		t.Errorf("a scoped STYLED read showed a row outside the island")
	}
	if !strings.Contains(styled.GetText(), "left") {
		t.Errorf("a scoped styled read lost its own island")
	}
	// It must still be an escape stream — otherwise "cropped" was
	// achieved by quietly returning the plain form.
	if !strings.Contains(styled.GetText(), "\x1b[") {
		t.Errorf("the scoped styled read carries no escape sequences: %q", styled.GetText())
	}
	// And it must be HOMED rather than absolutely positioned: the guest's
	// screen is its island, so the stream must not encode where on the
	// host's page that island sits.
	hostStyled, err := r.host.ctl.ScreenText(context.Background(), &controlv1.ScreenTextRequest{Styled: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(styled.GetText()) >= len(hostStyled.GetText()) {
		t.Errorf("the scoped styled read (%d bytes) is not smaller than the host's (%d)",
			len(styled.GetText()), len(hostStyled.GetText()))
	}
}

// ---- the property the whole contract exists for ----
//
// Two guests, disjoint islands, one app, concurrently. What this
// measures, precisely:
//
//   - every LEGAL op on both sides succeeds while the other side is
//     hammering the same app (no false refusals under concurrency, and
//     no serialization deadlock across two endpoints and one UI
//     goroutine);
//   - each island's final state is its OWN writer's last write, so
//     neither guest's traffic altered the other's subtree or value;
//   - every ILLEGAL cross-op is refused, every time, with the shaped
//     code — the count is asserted, not merely "at least one".
//
// The last clause is what keeps it from passing vacuously: a build where
// enforcement did nothing would still satisfy the first two, because
// both clients are also behaving. The A/B is recorded in the report —
// with mayAddress neutered, crossDenied drops to 0 and this goes red.
func TestDisjointIslandsDriveOneAppConcurrently(t *testing.T) {
	r := newIslandRig(t)
	const rounds = 40

	type result struct {
		crossDenied int
		err         error
	}
	run := func(ep *clientPair, own, ownIsland, peerIsland, ownValue, peerValue string) result {
		var res result
		for i := 0; i < rounds; i++ {
			want := fmt.Sprintf("%s-%d", own, i)
			if err := ep.set(ownValue, want); err != nil {
				res.err = fmt.Errorf("round %d: own set: %w", i, err)
				return res
			}
			if err := ep.patch(ownIsland, fragment(ownIsland, want)); err != nil {
				res.err = fmt.Errorf("round %d: own patch: %w", i, err)
				return res
			}
			// Now reach for the peer's, every round.
			if err := ep.set(peerValue, "trespass"); err != nil {
				if s, _ := statusCode(err); s == codes.PermissionDenied {
					res.crossDenied++
				} else {
					res.err = fmt.Errorf("round %d: cross set: %w", i, err)
					return res
				}
			} else {
				res.err = fmt.Errorf("round %d: cross set SUCCEEDED", i)
				return res
			}
			if err := ep.patch(peerIsland, fragment(peerIsland, "trespass")); err != nil {
				if s, _ := statusCode(err); s == codes.PermissionDenied {
					res.crossDenied++
				} else {
					res.err = fmt.Errorf("round %d: cross patch: %w", i, err)
					return res
				}
			} else {
				res.err = fmt.Errorf("round %d: cross patch SUCCEEDED", i)
				return res
			}
		}
		return res
	}

	var wg sync.WaitGroup
	var lres, rres result
	wg.Add(2)
	go func() {
		defer wg.Done()
		lres = run(r.left, "L", "LeftText", "RightText", "Left.Body", "Right.Body")
	}()
	go func() {
		defer wg.Done()
		rres = run(r.rght, "R", "RightText", "LeftText", "Right.Body", "Left.Body")
	}()
	wg.Wait()

	if lres.err != nil {
		t.Fatalf("left session: %v", lres.err)
	}
	if rres.err != nil {
		t.Fatalf("right session: %v", rres.err)
	}
	if want := rounds * 2; lres.crossDenied != want || rres.crossDenied != want {
		t.Fatalf("cross-island refusals = %d/%d, want %d each", lres.crossDenied, rres.crossDenied, want)
	}

	r.onUI(func() {
		if got, want := r.vm.left.Get(), fmt.Sprintf("L-%d", rounds-1); got != want {
			t.Errorf("Left.Body = %q, want %q — the peer's traffic altered it", got, want)
		}
		if got, want := r.vm.right.Get(), fmt.Sprintf("R-%d", rounds-1); got != want {
			t.Errorf("Right.Body = %q, want %q — the peer's traffic altered it", got, want)
		}
	})

	// Each island holds its own writer's last patch, in the live tree.
	r.onUI(func() {
		for name, want := range map[string]string{
			"LeftText":  fmt.Sprintf("L-%d", rounds-1),
			"RightText": fmt.Sprintf("R-%d", rounds-1),
		} {
			w, ok := r.app.ctx.Named[name]
			if !ok {
				t.Fatalf("%s vanished", name)
			}
			if got := textOf(w); got != want {
				t.Errorf("%s = %q, want %q", name, got, want)
			}
		}
	})
}

func textOf(w gooey.Component) string {
	if t, ok := w.(*components.Text); ok && t.Content != nil {
		return t.Content.Get()
	}
	return fmt.Sprintf("<not a Text: %T>", w)
}

func statusCode(err error) (codes.Code, string) {
	st, ok := status.FromError(err)
	if !ok {
		return codes.Unknown, err.Error()
	}
	return st.Code(), st.Message()
}

// ---- the streaming path ----
//
// The unary surface above proves the RULE. These prove it also holds on
// the channel the wysiwyg editor actually drives — the Attach stream —
// and cover the three narrowings that exist only there: the input echo,
// the Swapped name table and the frame's damage rects. Those three were
// written with no test until this block; the A/B for each is in the
// report.

// islandSession is a scoped Attach stream with a bounded read, so an
// absence can be asserted without hanging the suite.
type islandSession struct {
	t      *testing.T
	stream grpcgo.BidiStreamingClient[controlv1.AttachRequest, controlv1.AttachResponse]
	in     chan *controlv1.AttachResponse
}

func (c *clientPair) attach(t *testing.T, sub *controlv1.Subscription) *islandSession {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	stream, err := c.sess.Attach(ctx)
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}
	if err := stream.Send(&controlv1.AttachRequest{
		Msg: &controlv1.AttachRequest_Subscribe{Subscribe: sub},
	}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	s := &islandSession{t: t, stream: stream, in: make(chan *controlv1.AttachResponse, 256)}
	go func() {
		for {
			m, err := stream.Recv()
			if err != nil {
				close(s.in)
				return
			}
			select {
			case s.in <- m:
			default:
				return
			}
		}
	}()
	if w := s.next(2 * time.Second); w == nil || w.GetWelcome() == nil {
		t.Fatalf("first message was not a welcome: %v", w)
	}
	return s
}

// next returns the next message, or nil if none arrived in time. A nil
// is a real answer here — "nothing was echoed" is the assertion.
func (s *islandSession) next(d time.Duration) *controlv1.AttachResponse {
	select {
	case m, ok := <-s.in:
		if !ok {
			return nil
		}
		return m
	case <-time.After(d):
		return nil
	}
}

func (s *islandSession) act(id uint64, a *controlv1.Act) {
	s.t.Helper()
	a.Id = id
	if err := s.stream.Send(&controlv1.AttachRequest{Msg: &controlv1.AttachRequest_Act{Act: a}}); err != nil {
		s.t.Fatalf("send act: %v", err)
	}
}

// drain empties whatever has already arrived, so a following assertion
// is about messages caused by the next action.
func (s *islandSession) drain() {
	for {
		select {
		case <-s.in:
		default:
			return
		}
	}
}

func TestGrantRefusesActsOnTheAttachStream(t *testing.T) {
	r := newIslandRig(t)
	s := r.left.attach(t, &controlv1.Subscription{Properties: true})

	// In-grant act: applied.
	s.act(1, &controlv1.Act{Act: &controlv1.Act_SetProperty{
		SetProperty: &controlv1.SetPropertyRequest{Name: "Left.Body", Value: strVal("via-stream")},
	}})
	// Out-of-grant act on the same stream.
	s.act(2, &controlv1.Act{Act: &controlv1.Act_SetProperty{
		SetProperty: &controlv1.SetPropertyRequest{Name: "Host.Secret", Value: strVal("stolen")},
	}})
	// And a swap, which no grant authorizes.
	s.act(3, &controlv1.Act{Act: &controlv1.Act_SwapMarkup{
		SwapMarkup: &controlv1.SwapMarkupRequest{Source: islandMarkup},
	}})

	got := map[uint64]uint32{}
	deadline := time.After(5 * time.Second)
	for len(got) < 3 {
		select {
		case m, ok := <-s.in:
			if !ok {
				t.Fatal("the session ended before every act was answered")
			}
			if res := m.GetResult(); res != nil {
				got[res.GetId()] = res.GetCode()
			}
		case <-deadline:
			t.Fatalf("only %d act results arrived", len(got))
		}
	}
	if got[1] != 0 {
		t.Errorf("in-grant act was refused: code %d", got[1])
	}
	if got[2] != uint32(codes.PermissionDenied) {
		t.Errorf("out-of-grant act code = %d, want %d", got[2], codes.PermissionDenied)
	}
	if got[3] != uint32(codes.PermissionDenied) {
		t.Errorf("swap act code = %d, want %d", got[3], codes.PermissionDenied)
	}
	r.onUI(func() {
		if r.vm.left.Get() != "via-stream" {
			t.Errorf("in-grant act did not land: %q", r.vm.left.Get())
		}
		if r.vm.secret.Get() != "hunter2" {
			t.Errorf("a refused act landed anyway: %q", r.vm.secret.Get())
		}
	})
}

// Terminal input is the USER typing, anywhere on the page. An island
// grant is not a grant to watch that.
func TestGrantDoesNotEchoTerminalInputToAGuest(t *testing.T) {
	r := newIslandRig(t)
	guest := r.left.attach(t, &controlv1.Subscription{Input: true})
	host := r.host.attach(t, &controlv1.Subscription{Input: true})
	guest.drain()
	host.drain()

	r.onUI(func() { r.app.fireEvent(input.KeyOf(input.Rune('z')), true) })

	// The host session hears it — without this arm, a broken echo path
	// that dropped everything would read as enforcement.
	if m := host.next(2 * time.Second); m == nil || m.GetInput() == nil {
		t.Fatalf("the host session did not hear terminal input: %v", m)
	}
	if m := guest.next(300 * time.Millisecond); m != nil && m.GetInput() != nil {
		t.Errorf("a scoped session was echoed the user's keystroke: %v", m.GetInput())
	}
}

// Swapped is total invalidation and carries the new name table. A guest
// gets the names inside ITS island, not the host's address book.
func TestGrantNarrowsTheSwappedNameTable(t *testing.T) {
	r := newIslandRig(t)
	guest := r.left.attach(t, &controlv1.Subscription{Lifecycle: true})
	guest.drain()

	if _, err := r.host.ctl.SwapMarkup(context.Background(), &controlv1.SwapMarkupRequest{Source: islandMarkup}); err != nil {
		t.Fatalf("host swap: %v", err)
	}
	var named []string
	deadline := time.After(5 * time.Second)
	for named == nil {
		select {
		case m, ok := <-guest.in:
			if !ok {
				t.Fatal("session ended")
			}
			if sw := m.GetLifecycle().GetSwapped(); sw != nil {
				named = sw.GetNamed()
			}
		case <-deadline:
			t.Fatal("no Swapped arrived")
		}
	}
	seen := map[string]bool{}
	for _, n := range named {
		seen[n] = true
	}
	if !seen["Left"] || !seen["LeftText"] {
		t.Errorf("Swapped hid the guest's own names: %v", named)
	}
	for _, hidden := range []string{"Right", "RightText", "HostBar"} {
		if seen[hidden] {
			t.Errorf("Swapped handed the guest %q, outside its island: %v", hidden, named)
		}
	}
}

// A frame's damage rects are the host's page geometry. A scoped session
// sees the rects touching its island and no others.
func TestGrantNarrowsFrameDamage(t *testing.T) {
	r := newIslandRig(t)

	tree, err := r.left.ctl.SnapshotTree(context.Background(), &controlv1.SnapshotTreeRequest{Depth: 1})
	if err != nil {
		t.Fatal(err)
	}
	isle := tree.GetRoot().GetBounds()
	if isle == nil || isle.GetHeight() == 0 {
		t.Fatalf("island has no bounds")
	}

	guest := r.left.attach(t, &controlv1.Subscription{Frames: true})
	guest.drain()

	// Repaint something OUTSIDE the island, repeatedly.
	for i := 0; i < 5; i++ {
		if err := r.host.set("Host.Secret", fmt.Sprintf("s%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	outside := 0
	for {
		m := guest.next(400 * time.Millisecond)
		if m == nil {
			break
		}
		for _, d := range m.GetFrame().GetDamage() {
			if d.GetY() >= isle.GetY()+isle.GetHeight() || d.GetY()+d.GetHeight() <= isle.GetY() {
				outside++
			}
		}
	}
	if outside != 0 {
		t.Errorf("a scoped session was sent %d damage rects outside its island", outside)
	}

	// Non-vacuity: its OWN repaint does reach it, so the filter is not
	// simply dropping every rect.
	guest.drain()
	if err := r.left.set("Left.Body", "mine-now"); err != nil {
		t.Fatal(err)
	}
	inside := 0
	for {
		m := guest.next(400 * time.Millisecond)
		if m == nil {
			break
		}
		inside += len(m.GetFrame().GetDamage())
	}
	if inside == 0 {
		t.Error("a scoped session saw no damage for a repaint inside its own island")
	}
}

// ---- pointer routing: the check must model dispatch, not paraphrase it ----
//
// Two framework behaviours move a pointer event off its hit, and a scope
// check written against HitTest alone gets BOTH wrong — one by letting an
// escalation through, one by refusing legitimate work.

// freezeHost is a Frozen container registered as <Freeze>, so a test page
// can put an island underneath one. Not hypothetical since f744ada:
// preview.Pane in the wysiwyg editor is exactly this shape.
type freezeHost struct {
	gooey.Base
	gooey.FocusState
	child  gooey.Component
	frozen bool
	got    int
}

func (h *freezeHost) Frozen() bool                          { return h.frozen }
func (h *freezeHost) ChildComponents() []gooey.Component    { return []gooey.Component{h.child} }
func (h *freezeHost) Measure(a gooey.Size) gooey.Size       { return gooey.MeasureChild(h.child, a) }
func (h *freezeHost) Arrange(b gooey.Rect)                  { h.Base.Arrange(b); gooey.ArrangeChild(h.child, b) }
func (h *freezeHost) Render(*gooey.Frame)                   {}
func (h *freezeHost) AcceptsFocus() bool                    { return true }
func (h *freezeHost) HandleMouse(input.MouseEvent) bool     { h.got++; return true }
func (h *freezeHost) HandleMouseMove(input.MouseEvent) bool { h.got++; return true }

// A guest whose island sits INSIDE a frozen host it does not own: the hit
// lands in the island, but dispatch retargets to the host, so the event
// would be delivered outside the grant. The raw-hit check cleared exactly
// this.
func TestGrantRefusesAPointerRetargetedOutOfTheIsland(t *testing.T) {
	var host *freezeHost
	body := prop.NewSource("x")
	src := `<Gooey><VStack><Freeze Name="Shell"><Border Name="Isle">` +
		`<Text Name="IsleText">{{.Isle.Body}}</Text></Border></Freeze></VStack></Gooey>`
	app := newTestApp(t, src, map[string]any{"Isle": map[string]any{"Body": body}},
		func(c *markup.Context) {
			c.Components = map[string]markup.Builder{
				"Freeze": func(e markup.Element, ctx *markup.Context) (gooey.Component, error) {
					kids, _, err := markup.BuildChildren(e, ctx)
					if err != nil {
						return nil, err
					}
					host = &freezeHost{child: kids[0]}
					return host, nil
				},
			}
		})
	ep := endpoint(t, app, control.Island("Isle", "Isle"))

	tree, err := ep.ctl.SnapshotTree(context.Background(), &controlv1.SnapshotTreeRequest{Depth: 1})
	if err != nil {
		t.Fatal(err)
	}
	b := tree.GetRoot().GetBounds()
	x, y := b.GetX()+b.GetWidth()/2, b.GetY()+b.GetHeight()/2

	point := func() error {
		_, err := ep.ctl.SendPointer(context.Background(), &controlv1.SendPointerRequest{
			Event: &controlv1.PointerEvent{
				Kind: controlv1.PointerKind_POINTER_KIND_MOVE, X: x, Y: y,
				Button: controlv1.MouseButton_MOUSE_BUTTON_NONE,
			},
		})
		return err
	}

	// THAWED: the event reaches the island. Allowed — and this arm is what
	// stops the frozen arm below passing for the wrong reason.
	if err := point(); err != nil {
		t.Fatalf("pointing inside its own island while thawed: %v", err)
	}

	// The shell has already been touched here, and that is CORRECT rather
	// than a leak: the island's Text handles no motion, so the event
	// BUBBLES to its ancestors exactly as a user's own pointer would. A
	// grant scopes the TARGET of an event, never its bubble — the bubble
	// is the framework's event model, shared with keys, and suppressing it
	// would change how the app behaves rather than what the guest may
	// reach. So the frozen arm below is measured as a DELTA.
	baseline := 0
	done := make(chan struct{})
	app.Post(func() { baseline = host.got; host.frozen = true; app.comp.Focus().Resync(); close(done) })
	<-done

	// FROZEN: same cell, same island, but dispatch would now hand the
	// event to the shell as its TARGET.
	wantCode(t, point(), codes.PermissionDenied, "would be delivered to")

	after := make(chan int, 1)
	app.Post(func() { after <- host.got })
	if got := <-after; got != baseline {
		t.Errorf("a refused pointer call still dispatched: the shell went from %d to %d events", baseline, got)
	}
}

// The mirror image: a guest's OWN drag leaves its island's bounds. The
// captor is inside the island, so dispatch delivers to the island — and
// refusing it would break every drag a guest could ever do.
func TestGrantAllowsAGuestsOwnDragOutsideItsBounds(t *testing.T) {
	r := newIslandRig(t)

	tree, err := r.left.ctl.SnapshotTree(context.Background(), &controlv1.SnapshotTreeRequest{Depth: 1})
	if err != nil {
		t.Fatal(err)
	}
	b := tree.GetRoot().GetBounds()
	inside := b.GetY() + b.GetHeight()/2
	below := b.GetY() + b.GetHeight() + 1

	capture := func(name string) {
		done := make(chan struct{})
		r.app.Post(func() {
			r.app.comp.Focus().ReleaseCapture()
			if !r.app.comp.Focus().CaptureMouse(r.app.ctx.Named[name]) {
				t.Errorf("CaptureMouse refused %q", name)
			}
			close(done)
		})
		<-done
	}
	move := func(y int32) error {
		_, err := r.left.ctl.SendPointer(context.Background(), &controlv1.SendPointerRequest{
			Event: &controlv1.PointerEvent{
				Kind: controlv1.PointerKind_POINTER_KIND_MOVE, X: b.GetX() + 1, Y: y,
				Button: controlv1.MouseButton_MOUSE_BUTTON_NONE,
			},
		})
		return err
	}

	// A HELD capture on something the guest owns, the way a scrollbar
	// thumb or a splitter holds one while tracking a drag.
	capture("LeftBox")
	if err := move(inside); err != nil {
		t.Fatalf("drag inside the island: %v", err)
	}
	// The CELL is the peer's, but the CAPTOR is the guest's, so this is
	// the guest's own drag and must be allowed.
	if err := move(below); err != nil {
		t.Fatalf("a guest's own drag outside its island's bounds was refused: %v", err)
	}

	// Discrimination: hand the capture to a component OUTSIDE the island
	// and the same call over the guest's own cells must be refused.
	capture("RightBox")
	wantCode(t, move(inside), codes.PermissionDenied, "would be delivered to")
}

// A load-time side effect in a guest's fragment must run ONCE, even when
// the fragment is refused.
//
// The first version of the grant classified a denial by EXPERIMENT:
// build against the pruned surface, and if that failed, build again
// against the host's full surface to see whether the grant was the only
// difference. It gave the right answer and it ran the document twice —
// so a fragment containing a <Companion> would have launched two
// processes on the error path alone. This is the pin for the fix
// (markup.UnresolvedError + one map lookup); it is RED against the
// double build, which is the only reason it is worth having.
type sideEffect struct {
	gooey.Base
	n *int
}

func (s *sideEffect) Measure(gooey.Size) gooey.Size { return gooey.Size{W: 1, H: 1} }
func (s *sideEffect) Render(*gooey.Frame)           {}

func TestARefusedFragmentRunsItsLoadTimeSideEffectsOnce(t *testing.T) {
	builds := 0
	body := prop.NewSource("b")
	secret := prop.NewSource("s")
	app := newTestApp(t, `<Gooey><VStack><Text Name="Isle">{{.Isle.Body}}</Text></VStack></Gooey>`,
		map[string]any{
			"Isle": map[string]any{"Body": body},
			"Host": map[string]any{"Secret": secret},
		},
		func(c *markup.Context) {
			c.Components = map[string]markup.Builder{
				"SideEffect": func(e markup.Element, ctx *markup.Context) (gooey.Component, error) {
					builds++
					return &sideEffect{n: &builds}, nil
				},
			}
		})
	ep := endpoint(t, app, control.Island("Isle", "Isle"))

	// The side effect is built BEFORE the offending binding, so the
	// counter records every attempt the server made at the document.
	frag := `<Gooey><VStack Name="Isle"><SideEffect/><Text>{{.Host.Secret}}</Text></VStack></Gooey>`
	wantCode(t, ep.patch("Isle", frag), codes.PermissionDenied, "outside this session's granted values")

	got := make(chan int, 1)
	app.Post(func() { got <- builds })
	if n := <-got; n != 1 {
		t.Errorf("a refused fragment built its side-effecting component %d times, want 1", n)
	}

	// Discrimination: the same fragment WITHOUT the out-of-grant binding
	// builds once and succeeds, so the count above is a property of the
	// refusal path and not of the component never being reached.
	before := make(chan int, 1)
	app.Post(func() { before <- builds })
	base := <-before
	if err := ep.patch("Isle", `<Gooey><VStack Name="Isle"><SideEffect/><Text>{{.Isle.Body}}</Text></VStack></Gooey>`); err != nil {
		t.Fatalf("in-grant fragment: %v", err)
	}
	app.Post(func() { got <- builds })
	if n := <-got; n != base+1 {
		t.Errorf("an accepted fragment built its component %d times, want %d", n-base, 1)
	}
}

// FrameDelta.repainted means something NARROWER on a scoped session, and
// this is the test that makes that undeniable rather than a footnote.
//
// Two sessions watch the SAME frame — one scoped to Left, one unscoped —
// and report different counts. That is correct: a guest's damage budget
// is its own subtree, and the app's total is a measurement of something
// it neither owns nor can act on. It is also exactly the kind of quiet
// contract change that a reader would otherwise discover by being
// confused, so the number is asserted from both sides at once.
func TestScopedAndUnscopedSessionsCountTheSameFrameDifferently(t *testing.T) {
	r := newIslandRig(t)
	guest := r.left.attach(t, &controlv1.Subscription{Frames: true})
	host := r.host.attach(t, &controlv1.Subscription{Frames: true})
	guest.drain()
	host.drain()

	// One write, well outside the island: the HostBar strip repaints.
	if err := r.host.set("Host.Secret", "a-visible-change"); err != nil {
		t.Fatal(err)
	}

	sum := func(s *islandSession) (frames, repainted int) {
		for {
			m := s.next(500 * time.Millisecond)
			if m == nil {
				return
			}
			if f := m.GetFrame(); f != nil {
				frames++
				repainted += int(f.GetRepainted())
			}
		}
	}
	gFrames, gRepainted := sum(guest)
	hFrames, hRepainted := sum(host)

	if hFrames == 0 || gFrames == 0 {
		t.Fatalf("both sessions must see the frame: guest=%d host=%d frames", gFrames, hFrames)
	}
	if hRepainted == 0 {
		t.Fatalf("the unscoped session counted no repaints for a visible change")
	}
	if gRepainted != 0 {
		t.Errorf("the scoped session counted %d repaints for a change outside its island, want 0", gRepainted)
	}
	if gRepainted == hRepainted {
		t.Errorf("scoped and unscoped counts agree (%d) — the narrowing is not in effect", gRepainted)
	}

	// The other direction, so "scoped always reports 0" is ruled out: a
	// repaint INSIDE the island is counted by both.
	guest.drain()
	host.drain()
	if err := r.left.set("Left.Body", "mine-now"); err != nil {
		t.Fatal(err)
	}
	_, gOwn := sum(guest)
	_, hOwn := sum(host)
	if gOwn == 0 {
		t.Error("the scoped session counted no repaints for a change inside its own island")
	}
	if hOwn == 0 {
		t.Error("the unscoped session counted no repaints for the same change")
	}
}

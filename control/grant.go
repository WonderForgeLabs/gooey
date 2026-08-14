package control

import (
	"sort"
	"strings"

	"github.com/WonderForgeLabs/gooey"
)

// Islands, enforced by the HOST.
//
// # What this replaces
//
// "The editor owns ONE named element in the target and never writes
// outside it" was, until this file existed, a rule the EDITOR followed.
// Nothing on the app's side checked it: every act and every unary RPC
// resolved a name against the whole binding context, so an attached
// session could patch any element, write any property, invoke any
// command and swap the entire page. Client politeness is not a security
// boundary — it is a comment.
//
// # Registration is the grant
//
// This is the framework's existing security model, not a new one.
// markup.Context.Components, .Handlers and .Rules all work the same way:
// the HOST registers, and what it registered is exactly what the guest
// can reach. A Grant extends that model to the control plane. The host
// says
//
//	control.Island("EditorPane", "Doc")
//
// and a service built with that grant reaches the subtree rooted at
// <... Name="EditorPane"> and the value namespace "Doc", and nothing
// else. There is no token, no negotiation and no way for a guest to ask
// for more: a guest cannot name a capability it was not handed, because
// the grant is a field on the SERVER the host started, not a parameter
// on the call.
//
// # The address IS the capability
//
// One control-plane endpoint carries one grant. Two guests with disjoint
// islands are two servers on two loopback ports, each holding its own
// grant; that is what lets them drive one app concurrently without
// interfering. This composes with v1's actual security posture
// (loopback-only, no authentication) rather than pretending to improve
// it: a port a guest was never told about is a port it cannot use, and
// this file makes a port a guest WAS told about narrower than the whole
// app.
//
// Stated plainly, because the distinction matters: a Grant is SCOPING,
// not AUTHENTICATION. It stops an attached guest from exceeding its
// brief. It does not stop something that can reach the host's own
// unscoped endpoint. Authentication is what a non-loopback bind would
// need, and v1 refuses those outright (checkLoopback).
//
// # Host and guest
//
// The host owns the thing; the guest acts on or with it. A grant is the
// host deciding what to EXPOSE and what to HIDE, and both halves are
// used here. Some verbs are REFUSED outside the island (a write is an
// error naming the island). Some are NARROWED (a tree snapshot is
// rooted at the island; a value listing shows only granted names; a
// screen read is cropped to the island's rectangle). Narrowing is the
// stronger form: a guest cannot refuse-probe its way to a map of what
// it cannot touch, because the world it is shown is already the world
// it was granted.

// Grant is the capability a host hands one control-plane endpoint.
//
// The zero Grant grants NOTHING — an empty island name resolves to no
// element and an empty value list to no values, so every scoped verb
// refuses. Fail-closed is deliberate: a grant that was half-built by a
// host that forgot a field must not read as "everything".
//
// A nil *Grant is the HOST's own service: unscoped, exactly as every
// endpoint behaved before grants existed.
type Grant struct {
	// Island is the Name= of the element this guest owns. The guest may
	// address that element and anything in its live subtree —
	// descendants and non-visual attachments both, the same reach
	// PatchMarkup's own subtree collection has.
	//
	// The name is resolved PER CALL against the live tree, never cached:
	// a swap or a hot reload reassigns every Name=, and a grant that
	// cached a component pointer would keep pointing at a detached
	// subtree while the guest believed it owned the visible one.
	Island string

	// Values are the dotted value names this guest may read, write,
	// invoke, register under, and bind from markup it patches. Matching
	// is by dotted-segment prefix: "Doc" grants "Doc" and "Doc.Body" but
	// not "Document"; "Doc.Body" grants only "Doc.Body".
	//
	// Empty means no values at all, which is a useful grant on its own:
	// a guest that may re-shape its island's markup but may not touch
	// the host's state.
	Values []string
}

// Island builds a grant scoping a guest to one named element's subtree
// and the value names it is handed.
func Island(name string, values ...string) *Grant {
	return &Grant{Island: name, Values: values}
}

// allowsValue reports whether a dotted value name is inside the grant.
func (g *Grant) allowsValue(name string) bool {
	for _, p := range g.Values {
		if name == p {
			return true
		}
		if strings.HasPrefix(name, p+".") {
			return true
		}
	}
	return false
}

// ---- service-side guards ----

// scoped reports whether this service speaks for a guest rather than
// the host.
func (s *Service) scoped() bool { return s.grant != nil }

// islandSet resolves the grant's island to the set of components the
// guest owns. Resolved per call — see Grant.Island on why caching is
// wrong.
//
// A grant whose island names nothing resolves to the EMPTY set rather
// than an error, so every element-addressed verb refuses with the same
// shaped message instead of one path leaking "your island is gone" and
// another leaking "that element exists". The caller distinguishes the
// two cases only when it needs to say something more useful.
func (s *Service) islandSet() map[gooey.Component]bool {
	out := map[gooey.Component]bool{}
	if s.bind == nil {
		return out
	}
	root, ok := s.bind.Named[s.grant.Island]
	if !ok || root == nil {
		return out
	}
	collectSubtree(root, out)
	return out
}

// islandRoot is the island's own component, nil when the grant names
// nothing in the live tree.
func (s *Service) islandRoot() gooey.Component {
	if s.bind == nil {
		return nil
	}
	return s.bind.Named[s.grant.Island]
}

// VisibleDamage filters a composed frame's damage rects down to what
// this service's scope may see — the rects that intersect the granted
// island. An unscoped service passes them through unchanged.
//
// It lives here rather than in a transport because it is a scoping rule,
// and scoping rules belong in the one place both transports call. The
// consequence a client should know: for a SCOPED session the repaint
// count that travels with a frame counts the repaints touching its
// island, not the app's total. That is the honest number for a guest —
// the app's total would be an unowned measurement of the host.
func (s *Service) VisibleDamage(rects []gooey.Rect) []gooey.Rect {
	if !s.scoped() {
		return rects
	}
	var clip gooey.Rect
	if root := s.islandRoot(); root != nil {
		if b, ok := root.(gooey.Bounded); ok {
			clip = b.Bounds()
		}
	}
	if clip.W <= 0 || clip.H <= 0 {
		return nil
	}
	out := make([]gooey.Rect, 0, len(rects))
	for _, r := range rects {
		if r.X < clip.X+clip.W && clip.X < r.X+r.W &&
			r.Y < clip.Y+clip.H && clip.Y < r.Y+r.H {
			out = append(out, r)
		}
	}
	return out
}

// mayAddress guards a verb that names an ELEMENT. The name must resolve
// to a component inside the granted island's live subtree.
func (s *Service) mayAddress(name string) error {
	if !s.scoped() {
		return nil
	}
	if s.bind == nil {
		return errNoContext
	}
	w, ok := s.bind.Named[strings.TrimSpace(name)]
	if !ok {
		// Not found is not found — a scoped guest still gets the ordinary
		// error for a name that does not exist, because the alternative
		// is a permission error for every typo.
		return notFoundf("no element named %q; SnapshotTree lists the named elements", name)
	}
	if s.islandRoot() == nil {
		return deniedf("this session is scoped to island %q, which names no element in the running tree; every address is refused until it exists again", s.grant.Island)
	}
	if !s.islandSet()[w] {
		return deniedf("element %q is outside this session's island %q; a session may only address its own subtree", name, s.grant.Island)
	}
	return nil
}

// mayTouchValue guards a verb that names a VALUE (a property or a
// command).
func (s *Service) mayTouchValue(name string) error {
	if !s.scoped() {
		return nil
	}
	if !s.grant.allowsValue(name) {
		return deniedf("%q is outside this session's granted values (%s); ListValues shows what this session may reach",
			name, s.grant.valueList())
	}
	return nil
}

func (g *Grant) valueList() string {
	if len(g.Values) == 0 {
		return "none"
	}
	vs := append([]string(nil), g.Values...)
	sort.Strings(vs)
	return strings.Join(vs, ", ")
}

// grantedValues is the binding context's Values pruned to the grant: the
// world a scoped guest is shown, and the world markup it patches is
// built against.
//
// The pruned map is FRESH at every level along a granted path, so
// pruning can never mutate the host's own maps; the leaves are the
// SAME handles, because a guest writing a granted property has to write
// the real one.
func (s *Service) grantedValues() map[string]any {
	out := map[string]any{}
	if s.bind == nil || s.bind.Values == nil {
		return out
	}
	for _, p := range s.grant.Values {
		graft(out, s.bind.Values, strings.Split(p, "."))
	}
	return out
}

// graft copies one dotted path from src into dst, creating fresh
// intermediate maps in dst. A path that does not resolve is skipped: a
// grant may name something the app has not registered yet.
func graft(dst, src map[string]any, segs []string) {
	head := segs[0]
	v, ok := src[head]
	if !ok {
		return
	}
	if len(segs) == 1 {
		if m, isMap := v.(map[string]any); isMap {
			// A whole namespace: copy it wholesale, still through fresh
			// maps so the guest's view cannot be used to mutate the
			// host's structure.
			dst[head] = cloneScope(m)
			return
		}
		dst[head] = v
		return
	}
	inner, isMap := v.(map[string]any)
	if !isMap {
		return
	}
	next, _ := dst[head].(map[string]any)
	if next == nil {
		next = map[string]any{}
		dst[head] = next
	}
	graft(next, inner, segs[1:])
}

func cloneScope(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		if inner, ok := v.(map[string]any); ok {
			out[k] = cloneScope(inner)
			continue
		}
		out[k] = v
	}
	return out
}

package markup

import (
	"fmt"
	"sort"
	"strings"
)

// Unknown-attribute rejection: the payoff the component catalog was
// built for.
//
// Before this, an attribute nobody recognized was DROPPED IN SILENCE.
// applyLayout switches on the attribute key with no default arm, and
// most element arms read the attributes they know by name and never look
// at the rest. The motivating case is an attached property spelled bare:
//
//	<Text Left="10" Top="3">BARE</Text>
//
// Canvas.Left and Canvas.Top are the only spellings that work. The bare
// forms were accepted, ignored, and left the element sitting at the
// origin with nothing on screen, and nothing in any error, to say why.
//
// This is a deliberate BEHAVIOR CHANGE and it breaks pages: markup that
// relied on an attribute being ignored now fails to load. That cost was
// accepted knowingly, because the alternative is a class of defect that
// cannot be debugged from what the user can see. Three elements already
// worked this way — <Companion>, its <Var>, and <Validate> — and their
// errors are the model followed here.
//
// The error must name the FIX, not just the fault. "unknown attribute
// Left" is nearly useless when the answer is four characters away, so a
// near-miss suggestion is offered whenever one is close enough to be
// worth printing. That is cheap: the vocabulary is already in hand.

// checkAttrs rejects attributes the element cannot accept. It runs
// beside checkProps, which does the same job for property elements.
func checkAttrs(e Element, ctx *Context) error {
	spec, ok := ctx.spec(e.Name)
	if !ok || !spec.AttrsKnown {
		// A registered Go builder interprets attributes however it
		// likes, and an opaque element's vocabulary was never
		// enumerable. Claiming to validate either would be inventing a
		// rule the catalog cannot support.
		return nil
	}
	if spec.Open {
		// An open element owns its own check and can say more than this
		// one can. <Validate> reports the live rule vocabulary,
		// built-ins plus Context.Rules, which is strictly better than a
		// generic near-miss — so it must run instead of this, not after
		// it.
		return nil
	}
	allowed, attached := ctx.vocabulary(spec, e.parent)

	names := make([]string, 0, len(e.Attrs))
	for name := range e.Attrs {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		if allowed[name] {
			continue
		}
		// An attached property that exists but belongs to a different
		// parent gets its own error: the attribute is spelled correctly
		// and is simply in the wrong place, which is a different
		// mistake from a typo and deserves a different sentence.
		if parent, ok := attached[name]; ok {
			return fmt.Errorf("markup: <%s %s=%q>: %s is contributed by a <%s> parent, but this element's parent is %s; it would be ignored here",
				e.Name, name, e.Attrs[name], name, parent, describeParent(e.parent))
		}
		return fmt.Errorf("markup: <%s %s=%q>: no such attribute%s",
			e.Name, name, e.Attrs[name], suggest(name, allowed, attached))
	}
	return nil
}

func describeParent(parent string) string {
	if parent == "" {
		return "the document root"
	}
	return "<" + parent + ">"
}

// vocabulary is everything settable on this element in this context: its
// own attributes, the universal set if it has a Layout, and the attached
// properties its actual parent contributes. It also returns every OTHER
// attached property, keyed by the parent that would contribute it, so a
// misplaced one can be reported as misplaced rather than as unknown.
func (ctx *Context) vocabulary(spec ElementSpec, parentName string) (allowed map[string]bool, attached map[string]string) {
	allowed = make(map[string]bool, len(spec.Attrs)+len(universalAttrs)+4)
	allowed["Name"] = true
	for _, a := range spec.Attrs {
		allowed[a.Name] = true
	}
	if spec.Open {
		for _, a := range ctx.openAttrs(spec) {
			allowed[a.Name] = true
		}
	}
	attached = map[string]string{}
	if !TakesLayout(spec) {
		return allowed, attached
	}
	for _, a := range universalAttrs {
		allowed[a.Name] = true
	}
	// The DOCUMENT ROOT has no layout parent to scope against: its
	// syntactic parent is the <Gooey> wrapper, which is not one.
	//
	// This is not a convenience. At the root the answer does not exist
	// AT THIS LAYER. A patch fragment's real parent lives in the
	// target's live tree, which Build has never seen and which
	// PatchMarkup only resolves after the fragment builds; a whole
	// page's root has no layout parent at all. Build cannot even tell
	// the two apart — they are the same syntax.
	//
	// So every attached property is permitted at the root rather than
	// rejected. patch_markup documents restating a layout attribute as a
	// FEATURE — "layout attributes the fragment does not restate are
	// preserved from the old element; restating one takes it over" — and
	// enforcing a parent rule at a position with no parent broke every
	// such patch, plus every swap of a page whose root carried one.
	//
	// Only the misplaced-attached rule is suspended, and only here:
	// unknown attributes are still rejected at the root like anywhere
	// else.
	if parentName == "Gooey" || parentName == "" {
		for _, d := range ctx.granting() {
			for _, a := range d.Grants.Attached {
				allowed[a.Name] = true
			}
		}
		return allowed, attached
	}
	for _, d := range ctx.granting() {
		for _, a := range d.Grants.Attached {
			if d.Name == parentName {
				allowed[a.Name] = true
				continue
			}
			attached[a.Name] = d.Name
		}
	}
	return allowed, attached
}

// granting is every element in scope that contributes attached
// attributes — the builtins, plus whatever the host registered.
//
// Host-registered elements are included, and that direction is safe:
// a container that declares a Grant can only ADD names to `allowed`,
// so this accepts markup that was previously rejected and can never
// reject markup that previously loaded. Leaving them out would mean a
// host could declare Table.Column in its catalog, watch a palette offer
// it, and then have the loader refuse the result — the catalog lying
// about the target, which is the defect the catalog exists to remove.
func (ctx *Context) granting() []*ElementDef {
	// Registered first, and the shadowing runs in that direction:
	// buildComponent consults Context.Elements BEFORE the builtins, so
	// for a name declared in both, the registered definition is the one
	// that actually builds — and therefore the one whose grant is real.
	// Taking the builtin's grant here would validate against a
	// vocabulary the build never uses.
	out := make([]*ElementDef, 0, len(elementDefs)+len(ctx.Elements))
	shadowed := map[string]bool{}
	for name, d := range ctx.Elements {
		if d == nil {
			continue
		}
		shadowed[name] = true
		if len(d.Grants.Attached) > 0 {
			out = append(out, d)
		}
	}
	for _, d := range definedElements() {
		if len(d.Grants.Attached) > 0 && !shadowed[d.Name] {
			out = append(out, d)
		}
	}
	return out
}

// spec finds the catalog entry for an element name in this context.
//
// The order mirrors buildComponent's, and it has to: a spec describing a
// different element from the one that will build is worse than no spec,
// because the attribute check would then reject valid markup.
func (ctx *Context) spec(name string) (ElementSpec, bool) {
	// A host element with a DECLARATION is checkable like any built-in.
	// This is the half of Context.Elements that matters most — an
	// unknown attribute on a registered component used to be ignored
	// forever, and the near-miss suggestion works here for free.
	if d, ok := ctx.Elements[name]; ok {
		return d.specAs(OriginRegistered), true
	}
	if _, custom := ctx.Components[name]; custom {
		return ElementSpec{}, false
	}
	if d, ok := elementDefs[name]; ok {
		return d.spec(), true
	}
	return ElementSpec{}, false
}

// suggest offers the closest spelling when one is close enough to be
// worth printing. The motivating case is Left vs Canvas.Left, where the
// answer differs by a prefix rather than by a letter, so a suffix match
// is checked before edit distance.
func suggest(name string, allowed map[string]bool, attached map[string]string) string {
	for a, parent := range attached {
		if strings.HasSuffix(a, "."+name) {
			return fmt.Sprintf("; did you mean %s? (it is contributed by a <%s> parent)", a, parent)
		}
	}
	var best string
	bestD := 3 // never suggest something more than two edits away
	for a := range allowed {
		if strings.HasSuffix(a, "."+name) {
			return fmt.Sprintf("; did you mean %s?", a)
		}
		if d := distance(name, a); d < bestD {
			best, bestD = a, d
		}
	}
	if best != "" {
		return fmt.Sprintf("; did you mean %s?", best)
	}
	names := make([]string, 0, len(allowed))
	for a := range allowed {
		names = append(names, a)
	}
	sort.Strings(names)
	return "; this element takes " + strings.Join(names, ", ")
}

// distance is Levenshtein, case-insensitive, capped by the caller's
// threshold rather than by an early exit — the strings are attribute
// names, so they are short.
func distance(a, b string) int {
	a, b = strings.ToLower(a), strings.ToLower(b)
	if a == b {
		return 0
	}
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, min(cur[j-1]+1, prev[j-1]+cost))
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}

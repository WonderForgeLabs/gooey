// Package catalogen cross-checks the declared element vocabulary
// against the code that reads it.
//
// It used to be a GENERATOR: it derived the vocabulary from
// buildComponent's switch with go/ast and emitted a committed table.
// The vocabulary is now declared directly, in ElementDef literals beside
// the code that consumes it, so there is nothing left to derive.
//
// What survives is the half that was always the point. A declared
// vocabulary can be wrong in two ways and THEY ARE NOT SYMMETRIC:
//
//   - UNDER-declared — the code reads an attribute the literal omits.
//     Loud: unknown attributes are rejected, so the first document using
//     it fails to load and the corpus test names the file.
//   - OVER-declared — the literal permits an attribute nothing reads.
//     SILENT. The attribute is accepted and ignored, which is exactly
//     the silent-drop defect the vocabulary exists to prevent,
//     reintroduced through the vocabulary.
//
// Nothing but this catches the second. Measured on the element
// vocabulary: 51 of 124 rows (41%) are text, string, style or identity
// kinds for which no "absurd value" exists, so the cheaper trick of
// setting an attribute to nonsense and demanding an error cannot reach
// them. Style is the worst — an unknown style renders unstyled with no
// error, so it is unguardable that way AND silent at runtime.
//
// So this walks each ElementDef's Build closure, collects the attributes
// it actually reads, and compares that against what the literal claims.
package catalogen

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Finding is one disagreement between a declaration and its code.
type Finding struct {
	Element string
	Attr    string
	// OverDeclared is true when the literal claims an attribute the code
	// never reads; false when the code reads one the literal omits.
	OverDeclared bool
	// Note carries a disagreement that is not about a single attribute
	// — a ParsedBy naming an element with no Build to check against. It
	// wins over the two attribute messages when set.
	Note string
}

func (f Finding) String() string {
	if f.Note != "" {
		return f.Note
	}
	if f.OverDeclared {
		return fmt.Sprintf("<%s> declares %q but its Build never reads it: markup setting it would be accepted and silently ignored",
			f.Element, f.Attr)
	}
	return fmt.Sprintf("<%s> reads %q but does not declare it: the loader will reject markup that sets it",
		f.Element, f.Attr)
}

// universal attributes are applied outside any element (named,
// applyTooltipShorthand, applyLayout), so an element that reads one is
// not under-declaring.
var universal = map[string]bool{
	"Name": true, "Tooltip": true, "Width": true, "Height": true,
	"Margin": true, "HAlign": true, "VAlign": true, "Visibility": true,
}

// Check parses the markup package and compares every ElementDef's
// declared Attrs against the attributes its Build closure reads.
func Check(dir string) ([]Finding, error) {
	fset := token.NewFileSet()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var files []*ast.File
	funcs := map[string]*ast.FuncDecl{}
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, n), nil, 0)
		if err != nil {
			return nil, err
		}
		files = append(files, f)
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv == nil {
				funcs[fd.Name.Name] = fd
			}
		}
	}

	var out []Finding
	var defs []defInfo
	buildOf := map[string]*ast.FuncLit{}
	for _, f := range files {
		for _, d := range f.Decls {
			gd, ok := d.(*ast.GenDecl)
			if !ok || gd.Tok != token.VAR {
				continue
			}
			for _, sp := range gd.Specs {
				vs, ok := sp.(*ast.ValueSpec)
				if !ok || len(vs.Values) != 1 {
					continue
				}
				name, declared, build, parsedBy, ok := elementDef(vs.Values[0])
				if !ok || (build == nil && parsedBy == "") {
					continue
				}
				defs = append(defs, defInfo{name, declared, build, parsedBy})
				if build != nil {
					buildOf[name] = build
				}
			}
		}
	}
	// Every pseudo-element sharing a host, so the under-declared
	// direction can be checked against their UNION. See checkPseudo.
	pool := map[string]map[string]bool{}
	for _, d := range defs {
		if d.parsedBy == "" {
			continue
		}
		if pool[d.parsedBy] == nil {
			pool[d.parsedBy] = map[string]bool{}
		}
		for a := range d.declared {
			pool[d.parsedBy][a] = true
		}
	}
	hostChecked := map[string]bool{}
	for _, d := range defs {
		if d.parsedBy != "" {
			out = append(out, checkPseudo(d, buildOf, funcs)...)
			if !hostChecked[d.parsedBy] {
				hostChecked[d.parsedBy] = true
				out = append(out, checkPseudoPool(d.parsedBy, pool[d.parsedBy], buildOf, funcs)...)
			}
			continue
		}
		read := map[string]bool{}
		scan(d.build, funcs, read, map[string]bool{}, 0)
		for a := range read {
			if !d.declared[a] && !universal[a] {
				out = append(out, Finding{Element: d.name, Attr: a})
			}
		}
		for a := range d.declared {
			if !read[a] {
				out = append(out, Finding{Element: d.name, Attr: a, OverDeclared: true})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Element != out[j].Element {
			return out[i].Element < out[j].Element
		}
		return out[i].Attr < out[j].Attr
	})
	return out, nil
}

// defInfo is one element literal, held until every file has been read.
// A ParsedBy element is checked against ANOTHER element's Build, which
// may be declared in a file the walk has not reached yet, so nothing can
// be decided in the first pass.
type defInfo struct {
	name     string
	declared map[string]bool
	build    *ast.FuncLit
	parsedBy string
}

// checkPseudo is the OVER-declared direction for a pseudo-element,
// checked against the Build that actually parses it. See
// ElementDef.ParsedBy for why the field exists at all.
//
// Per element, only this direction is decidable. One Build parses
// several pseudo-elements — buildMenuBar reads both <Menu>'s Title and
// <MenuItem>'s Text — and the attribute names in it carry no record of
// which element they came off, so a read cannot be attributed to one
// declaration.
//
// The under-declared direction is NOT given up; it moves up a level, to
// checkPseudoPool, which asks it of the host's whole family at once.
// That split is the honest decomposition: "nobody declares this" is
// answerable, "the wrong sibling declares this" is not. Only the second
// is lost, and it is at least visible in the property grid.
//
// An earlier version of this comment gave up the under-declared
// direction outright, on the grounds that "under-declaring stays loud
// the ordinary way because unknown attributes are rejected at load".
// That was FALSE for exactly the elements this function covers —
// checkAttrs runs inside build(), and a pseudo-element never reaches
// build() — which left <Menu> and <MenuItem> the only elements in the
// vocabulary with NEITHER direction guarded. buildMenuBar calls
// checkAttrs on its children now, so the sentence is true; the pool
// check is here anyway, because a guard that rests on a claim about
// somebody else's code is one refactor from being wrong again.
func checkPseudo(d defInfo, buildOf map[string]*ast.FuncLit, funcs map[string]*ast.FuncDecl) []Finding {
	host := buildOf[d.parsedBy]
	if host == nil {
		return []Finding{{Element: d.name, Note: fmt.Sprintf(
			"<%s> is ParsedBy <%s>, which declares no Build to check it against: the declaration is unverifiable",
			d.name, d.parsedBy)}}
	}
	// THE CHILD HALF ONLY. A read the host takes off its own element is
	// the host's attribute, not this one's — see scanChildAttrs.
	_, child := hostReads(d.parsedBy, buildOf, funcs)
	// AND THE ELEMENT'S OWN Build, which is not usually nothing. A
	// ParsedBy def whose Build reads an attribute is a legal shape —
	// today's two only return "only valid inside…" errors, but the
	// branch above skips the ordinary two-direction scan, so without
	// this such a read would be unattributed AND the declaration
	// serving it reported over-declared.
	if d.build != nil {
		scan(d.build, funcs, child, map[string]bool{}, 0)
	}
	var out []Finding
	for a := range d.declared {
		// NO universal SKIP HERE, and that is the difference between a
		// pseudo-element and an ordinary one. The skip is right for an
		// ordinary element because applyLayout consumes Margin, Width
		// and the rest outside its Build. A pseudo-element has no
		// applyLayout — a nil Proto makes TakesLayout false, so
		// vocabulary() never adds the universal set to it — but
		// checkAttrs allows anything in spec.Attrs regardless. So
		// declaring Margin on <MenuItem> made it settable, unread and
		// silently dropped, with the whole suite green: the exact
		// defect TestAnUnknownMenuAttributeIsALoadError closes for an
		// undeclared name, still reachable through the eight universal
		// ones.
		if !child[a] {
			out = append(out, Finding{Element: d.name, Attr: a, OverDeclared: true})
		}
	}
	return out
}

// hostReads is one walk of a host Build, split into what it reads off
// its own element and what it reads off its children.
func hostReads(host string, buildOf map[string]*ast.FuncLit, funcs map[string]*ast.FuncDecl) (own, child map[string]bool) {
	own, child = map[string]bool{}, map[string]bool{}
	fn := buildOf[host]
	if fn == nil {
		return own, child
	}
	scanChildAttrs(fn, funcs, elementParam(fn.Type), own, child, map[string]bool{})
	return own, child
}

// checkPseudoPool is the UNDER-declared direction, recovered at the only
// level ParsedBy actually models: the host reads a set of attribute
// names off its children, and every one of them must be declared by
// SOME pseudo-element naming that host.
//
// Per-element it is undecidable — the names in buildMenuBar carry no
// record of whether they came off a <Menu> or a <MenuItem> — so this
// pools the declarations and asks the weaker, still-useful question. An
// attribute the host reads and NOBODY declares is caught, which is the
// half that matters: it is a read the property grid cannot offer,
// #429's symptom returning through the machinery built to remove it.
// What stays unattributable is which of the siblings should have
// declared it, and that one is at least visible in the grid.
//
// The reads are attributed to the HOST in the finding rather than to a
// child, because that is the honest address: the fix is to add the
// attribute to whichever child element it belongs on, and this cannot
// say which.
func checkPseudoPool(host string, declared map[string]bool, buildOf map[string]*ast.FuncLit, funcs map[string]*ast.FuncDecl) []Finding {
	if buildOf[host] == nil {
		return nil // already reported per-element by checkPseudo
	}
	// ONE WALK, SPLIT — not two walks subtracted. The previous version
	// took the child set with the loose walk and the own set with
	// scan's strict one and subtracted them, which is not a subtraction
	// at all: the two disagree about the deny-list, about
	// passesElement and about depth, so anything reachable through the
	// generic builder machinery landed in the first set, could not
	// appear in the second, and was reported as an attribute nobody
	// wrote. scanChildAttrs answers both from the same walk, so they
	// are comparable by construction.
	_, child := hostReads(host, buildOf, funcs)

	names := make([]string, 0, len(child))
	for a := range child {
		names = append(names, a)
	}
	sort.Strings(names)
	var out []Finding
	for _, a := range names {
		// NO `universal[a]` HERE, and its absence is the point. The
		// over-declared direction dropped that skip when checkPseudo
		// learned that a pseudo-element has no applyLayout and so no
		// universal set; leaving it in the under-declared direction made
		// the two halves of one check assert opposite things about the
		// same eight names. A host reading ic.Attrs["Margin"] off a
		// pseudo child would be waved through even though vocabulary()
		// will never let markup set it — a dead read, which is exactly
		// what this package exists to surface. Found in review of #454.
		if declared[a] {
			continue
		}
		out = append(out, Finding{Element: host, Attr: a, Note: fmt.Sprintf(
			"<%s> reads %q off a child element that no <%s>-parsed element declares: "+
				"markup setting it would work and no catalog would know it exists",
			host, a, host)})
	}
	return out
}

// scanChildAttrs splits a host Build's attribute reads in two: the ones
// taken off its OWN element and the ones taken off some other element
// variable. For a ParsedBy host those other variables ARE its
// pseudo-children — c and ic in buildMenuBar — so the second set is the
// pseudo-elements' surface and the first is the host's own.
//
// THE SPLIT IS THE WHOLE POINT, and collapsing it is a silent hole.
// Before it existed, checkPseudo counted every read in the body, so a
// pseudo-element could declare an attribute only the HOST reads off
// itself and pass: <MenuItem> declaring Style — which buildMenuBar reads
// off e, the bar — was green, and `<MenuItem Style="…">` loaded clean
// and was dropped on the floor. That is the silent-drop defect this
// package exists to catch, reached through the check meant to catch it.
//
// The host's own element is identified by NAME, taken from the
// parameter declared `Element` in the body being walked, rather than
// assumed to be `e`. attrOf hardcodes `e` and is right to — it is what
// stops an ordinary element claiming a neighbour's reads — but a
// hardcode here would silently mis-split the first host that names its
// parameter anything else.
//
// generic and passesElement are applied for the same reason scan
// applies them: without the deny-list this follows build/buildChildren/
// checkAttrs into the general builder machinery and collects literals
// that belong to no element, and checkPseudoPool would then report an
// attribute nobody wrote. scan's depth cap is not needed — seen already
// bounds the walk, and it bounds it per walk rather than per call so a
// mutually recursive pair terminates.
func scanChildAttrs(n ast.Node, funcs map[string]*ast.FuncDecl, self string, own, child, seen map[string]bool) {
	ast.Inspect(n, func(nd ast.Node) bool {
		switch v := nd.(type) {
		case *ast.IndexExpr:
			sel, isSel := v.X.(*ast.SelectorExpr)
			if !isSel || sel.Sel.Name != "Attrs" {
				return true
			}
			recv, isIdent := sel.X.(*ast.Ident)
			if !isIdent {
				return true
			}
			lit, isLit := v.Index.(*ast.BasicLit)
			if !isLit || lit.Kind != token.STRING {
				return true
			}
			a, err := strconv.Unquote(lit.Value)
			if err != nil {
				return true
			}
			if recv.Name == self {
				own[a] = true
			} else {
				child[a] = true
			}
		case *ast.CallExpr:
			// THE HELPER IDIOM, which scan recognises and this used to
			// miss: Bound(e, ctx, "Text"), BoundColor(ic, ctx, "Fg"),
			// optDuration(c, "Tick") — the attribute name is a string
			// argument, not an index. Missing it is not a quiet gap in
			// one direction: it produces a FALSE over-declaration,
			// because the declaration is real and the read is invisible.
			// Which element it belongs to is the same question as
			// everywhere else here, answered the same way — by whether
			// the element argument is the host's own.
			if elem, ok := elementOf(v, funcs); ok {
				for _, arg := range v.Args {
					lit, isLit := arg.(*ast.BasicLit)
					if !isLit || lit.Kind != token.STRING {
						continue
					}
					a, err := strconv.Unquote(lit.Value)
					if err != nil || !isAttrName(a) {
						continue
					}
					if elem == self {
						own[a] = true
					} else {
						child[a] = true
					}
				}
			}
			name := calleeName(v.Fun)
			if name == "" || generic[name] || !passesAnElement(v, funcs) {
				return true
			}
			fd := funcs[name]
			if fd == nil || fd.Body == nil {
				return true
			}
			// The callee's OWN element parameter, not the caller's: a
			// helper is free to name it differently, and inheriting the
			// caller's name would file its self-reads as child reads.
			inner := elementParam(fd.Type)
			// UNLESS THE HELPER WAS HANDED A CHILD, which is the whole
			// reason the receiver split exists and the case that gets it
			// backwards. menuItemIcon(ic, ctx, &it) names its parameter
			// `ic` too, so the line above would call ic.Attrs["Icon"] a
			// read of the HOST's own attribute — filing a child read in
			// the one set that cannot see it, and reporting <MenuItem>
			// as over-declaring the attribute the code plainly reads.
			//
			// "" matches no identifier, so every read inside the helper
			// lands on the child side, which is what being handed a
			// child element means.
			if elem, ok := elementOf(v, funcs); ok && elem != self {
				inner = ""
			}
			// KEYED ON THE FUNCTION AND THE ROLE, not the function alone.
			// One builder may call the same helper twice — once with its
			// own element and once with a child — and those two calls
			// scan to different sets. A name-only key recorded whichever
			// came first and dropped the other's reads entirely, which is
			// a read going invisible: a false over-declaration, the exact
			// class this walk exists to prevent. The key space is still
			// finite (functions x their element parameter names plus ""),
			// so termination is unchanged.
			key := name + "\x00" + inner
			if seen[key] {
				return true
			}
			seen[key] = true
			scanChildAttrs(fd.Body, funcs, inner, own, child, seen)
		}
		return true
	})
}

// elementArg is the name of the first bare identifier argument, which is
// the element a helper is being asked about: Bound(e, ctx, "Text") and
// Bound(ic, ctx, "Text") differ in exactly that position.
//
// It is a HEURISTIC and deliberately a loose one, in the direction that
// cannot hide a defect: a wrong guess files a read under the wrong
// element, which produces a finding, where missing the read entirely
// produces a false over-declaration — a red test asserting the opposite
// of what the code does.
func elementArg(c *ast.CallExpr) (string, bool) {
	for _, a := range c.Args {
		if id, ok := a.(*ast.Ident); ok {
			return id.Name, true
		}
	}
	return "", false
}

// elementParam is the name of the parameter declared `Element`, or ""
// when there is none — in which case every read in that body is a child
// read, which is the conservative direction: it can produce a finding,
// never suppress one.
func elementParam(ft *ast.FuncType) string {
	if ft == nil || ft.Params == nil {
		return ""
	}
	for _, f := range ft.Params.List {
		id, ok := f.Type.(*ast.Ident)
		if !ok || id.Name != "Element" {
			continue
		}
		if len(f.Names) > 0 {
			return f.Names[0].Name
		}
	}
	return ""
}

// elementDef pulls Name, the declared attribute names, and the Build
// closure out of an &ElementDef{...} literal.
func elementDef(v ast.Expr) (name string, declared map[string]bool, build *ast.FuncLit, parsedBy string, ok bool) {
	var dynamic bool
	u, isUnary := v.(*ast.UnaryExpr)
	if !isUnary || u.Op != token.AND {
		return "", nil, nil, "", false
	}
	cl, isLit := u.X.(*ast.CompositeLit)
	if !isLit {
		return "", nil, nil, "", false
	}
	if id, isID := cl.Type.(*ast.Ident); !isID || id.Name != "ElementDef" {
		return "", nil, nil, "", false
	}
	declared = map[string]bool{}
	for _, el := range cl.Elts {
		kv, isKV := el.(*ast.KeyValueExpr)
		if !isKV {
			continue
		}
		key, _ := kv.Key.(*ast.Ident)
		if key == nil {
			continue
		}
		switch key.Name {
		case "Name":
			if lit, isLit := kv.Value.(*ast.BasicLit); isLit {
				name, _ = strconv.Unquote(lit.Value)
			}
		case "Attrs":
			ast.Inspect(kv.Value, func(n ast.Node) bool {
				inner, isKV := n.(*ast.KeyValueExpr)
				if !isKV {
					return true
				}
				if k, _ := inner.Key.(*ast.Ident); k != nil && k.Name == "Name" {
					if lit, isLit := inner.Value.(*ast.BasicLit); isLit {
						s, _ := strconv.Unquote(lit.Value)
						declared[s] = true
					}
				}
				return true
			})
		case "Build":
			build, _ = kv.Value.(*ast.FuncLit)
		case "ParsedBy":
			if lit, isLit := kv.Value.(*ast.BasicLit); isLit && lit.Kind == token.STRING {
				parsedBy, _ = strconv.Unquote(lit.Value)
			}
		case "DynamicAttrs":
			// The element consumes attributes by ranging rather than by
			// name, so this walk cannot follow it. It validates its own
			// vocabulary at load; skip it here rather than emit noise
			// that would train someone to ignore this test.
			dynamic = true
		}
	}
	if dynamic {
		return name, nil, nil, "", false
	}
	return name, declared, build, parsedBy, name != ""
}

// generic helpers that must not be followed: they belong to no element.
var generic = map[string]bool{
	"build": true, "buildComponent": true, "buildChildren": true,
	"named": true, "attachAll": true, "applyLayout": true,
	"slotChild": true, "checkProps": true, "checkAttrs": true,
}

// scan collects every attribute name a body reads, following helpers the
// body hands the Element to — passing e is what lets a helper read
// attributes, so any such call is a place they can hide.
func scan(n ast.Node, funcs map[string]*ast.FuncDecl, read, visiting map[string]bool, depth int) {
	vars := map[string]string{}
	ast.Inspect(n, func(nd ast.Node) bool {
		if a, ok := attrOf(nd, vars); ok {
			read[a] = true
		}
		switch v := nd.(type) {
		case *ast.AssignStmt:
			if len(v.Rhs) == 1 {
				if a, ok := attrOf(v.Rhs[0], vars); ok {
					if id, isID := v.Lhs[0].(*ast.Ident); isID && id.Name != "_" {
						vars[id.Name] = a
					}
				}
			}
		case *ast.CallExpr:
			callee := calleeName(v.Fun)
			// A literal naming an attribute: Bound(e, ctx, "Text"),
			// BoundColor(e, ctx, "Background"), optDuration(e, "Tick").
			for _, arg := range v.Args {
				if lit, isLit := arg.(*ast.BasicLit); isLit && lit.Kind == token.STRING {
					if s, err := strconv.Unquote(lit.Value); err == nil && isAttrName(s) && passesElement(v) {
						read[s] = true
					}
				}
			}
			if generic[callee] || depth >= 4 || visiting[callee] || !passesElement(v) {
				return true
			}
			fd := funcs[callee]
			if fd == nil {
				return true
			}
			visiting[callee] = true
			inner := map[string]string{}
			bindParams(fd, v, inner)
			scanWith(fd.Body, funcs, read, visiting, inner, depth+1)
			delete(visiting, callee)
		}
		return true
	})
}

func scanWith(n ast.Node, funcs map[string]*ast.FuncDecl, read, visiting map[string]bool, vars map[string]string, depth int) {
	ast.Inspect(n, func(nd ast.Node) bool {
		if a, ok := attrOf(nd, vars); ok {
			read[a] = true
		}
		switch v := nd.(type) {
		case *ast.AssignStmt:
			if len(v.Rhs) == 1 {
				if a, ok := attrOf(v.Rhs[0], vars); ok {
					if id, isID := v.Lhs[0].(*ast.Ident); isID && id.Name != "_" {
						vars[id.Name] = a
					}
				}
			}
		case *ast.CallExpr:
			callee := calleeName(v.Fun)
			for _, arg := range v.Args {
				if lit, isLit := arg.(*ast.BasicLit); isLit && lit.Kind == token.STRING {
					if s, err := strconv.Unquote(lit.Value); err == nil && isAttrName(s) && passesElement(v) {
						read[s] = true
					}
				}
			}
			if generic[callee] || depth >= 4 || visiting[callee] || !passesElement(v) {
				return true
			}
			if fd := funcs[callee]; fd != nil {
				visiting[callee] = true
				inner := map[string]string{}
				for k, val := range vars {
					inner[k] = val
				}
				bindParams(fd, v, inner)
				scanWith(fd.Body, funcs, read, visiting, inner, depth+1)
				delete(visiting, callee)
			}
		}
		return true
	})
}

// isAttrName filters the string literals that could plausibly be an
// attribute: markup attributes are capitalised identifiers, optionally
// dotted for an attached property.
func isAttrName(s string) bool {
	if s == "" || s[0] < 'A' || s[0] > 'Z' {
		return false
	}
	for _, r := range s {
		if !(r == '.' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')) {
			return false
		}
	}
	return true
}

func bindParams(fd *ast.FuncDecl, c *ast.CallExpr, vars map[string]string) {
	if fd.Type.Params == nil {
		return
	}
	var names []string
	for _, f := range fd.Type.Params.List {
		for _, n := range f.Names {
			names = append(names, n.Name)
		}
	}
	for i, arg := range c.Args {
		if i >= len(names) {
			break
		}
		if lit, ok := arg.(*ast.BasicLit); ok && lit.Kind == token.STRING {
			if s, err := strconv.Unquote(lit.Value); err == nil && s != "" {
				vars[names[i]] = s
			}
		}
	}
}

func passesElement(c *ast.CallExpr) bool {
	for _, a := range c.Args {
		if id, ok := a.(*ast.Ident); ok && id.Name == "e" {
			return true
		}
	}
	return false
}

// passesAnElement is passesElement widened to any element, and IT IS
// ONLY SAFE WHERE THE RECEIVER IS SPLIT — scanChildAttrs, never scan.
//
// scan collects one undifferentiated read set, so widening it there
// files a CHILD's attribute as the host's own read and reports the host
// as under-declaring it: <MenuBar> reads "Checked", <Tabs> reads
// "Header", <Companion> reads "Value". All three appeared the moment
// this was applied to both, and TestAHostsOwnReadIsNotAChildsAttribute
// said so about the fixture in the same run. Inside scanChildAttrs the
// split answers the question the widening opens, which is why the same
// change is correct in one and wrong in the other.
func passesAnElement(c *ast.CallExpr, funcs map[string]*ast.FuncDecl) bool {
	if _, ok := elementOf(c, funcs); ok {
		return true
	}
	// THE NAME "e" IS A CONVENTION, NOT THE QUESTION, and taking it for
	// the question is a hole this function shipped with: a helper handed
	// a CHILD element — menuItemIcon(ic, ctx, &it) — was never followed,
	// so every attribute it read became a FALSE OVER-DECLARATION. That
	// is the loud direction, a red test asserting the opposite of what
	// the code does, and #400 tripped it on its first day.
	//
	// So ask the callee instead: a function declaring an Element
	// parameter is being handed one, whatever the caller named its
	// variable. The "e" arm stays as well as rather than instead —
	// funcs holds only THIS package's decls, so a helper across a
	// package boundary has no signature here to ask, and dropping the
	// convention would reintroduce the same false positive by another
	// route. The union is the loose direction, which is the one this
	// file argues for at elementArg: a wrong guess produces a finding
	// somebody reads, a missed read produces a finding that is wrong.
	return false
}

// elementOf is which element a call is asking about, resolved from the
// CALLEE'S SIGNATURE when it is available and from argument order only
// when it is not.
//
// elementArg — the first bare identifier — is the wrong question and was
// wrong the moment markup exported its own helper. attr.go documents the
// public idiom as
//
//	value, err := markup.Attr[*prop.Property[int]](ctx, e, "Value")
//
// with ctx FIRST, so elementArg answers "ctx", which is nobody's element
// and in particular is not the host's `e`. Every read inside such a
// helper would then be filed as a CHILD read — the same false
// over-declaration this file exists to prevent, arriving by the other
// door, and reported against the host instead. Latent only because no
// ElementDef.Build uses Attr yet; it is the idiom attr.go tells authors
// to use.
//
// `nil`, `true` and `false` are also *ast.Ident in Go's AST, so the old
// first-bare-ident scan happily accepted helper(nil) as "an element was
// passed". Asking the signature removes that too.
func elementOf(c *ast.CallExpr, funcs map[string]*ast.FuncDecl) (string, bool) {
	if fd := funcs[calleeName(c.Fun)]; fd != nil {
		i, ok := elementParamIndex(fd.Type)
		if !ok {
			return "", false
		}
		if i < len(c.Args) {
			if id, isID := c.Args[i].(*ast.Ident); isID {
				return id.Name, true
			}
		}
		return "", false
	}
	// Not this package's function — no signature to ask. Fall back to
	// the heuristic, which is loose in the direction elementArg argues
	// for: a wrong guess produces a finding somebody reads, a missed
	// read produces a finding that is wrong.
	return elementArg(c)
}

// elementParamIndex is elementParam's position rather than its name.
// Counts one per NAME, not one per field: `func f(a, b Element)` is two
// parameters in one ast.Field.
func elementParamIndex(ft *ast.FuncType) (int, bool) {
	if ft == nil || ft.Params == nil {
		return 0, false
	}
	i := 0
	for _, f := range ft.Params.List {
		n := len(f.Names)
		if n == 0 {
			n = 1
		}
		if id, ok := f.Type.(*ast.Ident); ok && id.Name == "Element" && len(f.Names) > 0 {
			return i, true
		}
		i += n
	}
	return 0, false
}

// attrOf resolves an expression to the attribute it reads, seeing
// through strings.TrimSpace and through a local bound to one earlier.
func attrOf(n ast.Node, vars map[string]string) (string, bool) {
	switch v := n.(type) {
	case *ast.Ident:
		a, ok := vars[v.Name]
		return a, ok
	case *ast.CallExpr:
		if calleeName(v.Fun) == "TrimSpace" && len(v.Args) == 1 {
			return attrOf(v.Args[0], vars)
		}
	case *ast.IndexExpr:
		sel, ok := v.X.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Attrs" {
			return "", false
		}
		if id, ok := sel.X.(*ast.Ident); !ok || id.Name != "e" {
			return "", false
		}
		if lit, ok := v.Index.(*ast.BasicLit); ok && lit.Kind == token.STRING {
			s, _ := strconv.Unquote(lit.Value)
			return s, true
		}
		if id, ok := v.Index.(*ast.Ident); ok {
			a, ok := vars[id.Name]
			return a, ok
		}
	}
	return "", false
}

func calleeName(f ast.Expr) string {
	switch v := f.(type) {
	case *ast.Ident:
		return v.Name
	case *ast.SelectorExpr:
		return v.Sel.Name
	case *ast.IndexExpr:
		return calleeName(v.X)
	case *ast.IndexListExpr:
		return calleeName(v.X)
	}
	return ""
}

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
}

func (f Finding) String() string {
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
				name, declared, build, ok := elementDef(vs.Values[0])
				if !ok || build == nil {
					continue
				}
				read := map[string]bool{}
				scan(build, funcs, read, map[string]bool{}, 0)

				for a := range read {
					if !declared[a] && !universal[a] {
						out = append(out, Finding{name, a, false})
					}
				}
				for a := range declared {
					if !read[a] {
						out = append(out, Finding{name, a, true})
					}
				}
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

// elementDef pulls Name, the declared attribute names, and the Build
// closure out of an &ElementDef{...} literal.
func elementDef(v ast.Expr) (name string, declared map[string]bool, build *ast.FuncLit, ok bool) {
	var dynamic bool
	u, isUnary := v.(*ast.UnaryExpr)
	if !isUnary || u.Op != token.AND {
		return "", nil, nil, false
	}
	cl, isLit := u.X.(*ast.CompositeLit)
	if !isLit {
		return "", nil, nil, false
	}
	if id, isID := cl.Type.(*ast.Ident); !isID || id.Name != "ElementDef" {
		return "", nil, nil, false
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
		case "DynamicAttrs":
			// The element consumes attributes by ranging rather than by
			// name, so this walk cannot follow it. It validates its own
			// vocabulary at load; skip it here rather than emit noise
			// that would train someone to ignore this test.
			dynamic = true
		}
	}
	if dynamic {
		return name, nil, nil, false
	}
	return name, declared, build, name != ""
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
			// A literal naming an attribute: boundProp(e, ctx, "Text"),
			// bindColor(e, ctx, "Background"), optDuration(e, "Tick").
			for _, arg := range v.Args {
				if lit, isLit := arg.(*ast.BasicLit); isLit && lit.Kind == token.STRING {
					if s, err := strconv.Unquote(lit.Value); err == nil && isAttrName(s) && passesElement(v) {
						read[s] = true
					}
				}
			}
			if callee == "bindStyle" {
				read["Style"] = true
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
			if callee == "bindStyle" {
				read["Style"] = true
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

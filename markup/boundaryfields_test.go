package markup

import (
	"bytes"
	"go/ast"
	"go/parser"
	gotoken "go/token"
	"image"
	gopng "image/png"
	"io/fs"
	"maps"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
	"github.com/WonderForgeLabs/gooey/validate"
)

// The control boundary is a PARTITION of Context, and issue #314 is what
// happens when it is maintained by hand.
//
// Everything a page registers either crosses into a control or is
// deliberately withheld, and both halves are contracts. Components,
// Handlers, Styles, Includes, Dispatcher and Declared crossed; Rules,
// Elements, Dir and Variant did not, and every one of those four was a
// silent wrong answer rather than an error:
//
//   - Rules — `<Validate Email="true"/>` inside a control failed naming
//     only the built-ins, against a doc that calls the registration
//     "exactly like Components and Handlers";
//   - Elements — the DECLARED spelling of a host element was unknown
//     inside a control while the undeclared Components spelling worked,
//     which is the incentive backwards;
//   - Dir — a <Companion> in a control resolved its paths against the
//     process working directory;
//   - Variant — worked at depth 1 and stopped at depth 2, because the
//     resolveVariant call reads parent.Variant and one level down the
//     parent IS the child context.
//
// #314 FOUND TWO OF THE FOUR, and that is the argument for this test
// rather than for four more lines in the block. The issue lists Rules
// and Dir because those are what somebody hit; Elements and Variant sat
// beside them, in the same block, with the same shape. A list of fields
// somebody noticed is not the set.
//
// So the set is DERIVED — from the struct declaration, by parsing it —
// and the partition below has to account for every exported field. Add a
// field to Context and this test fails until you say which side of the
// boundary it is on.

// boundaryPartition is the CONTRACT, and the only hand-maintained thing
// here. Each field is inherit or isolate, with the reason, and the test
// checks the claim behaviourally rather than taking it.
var boundaryPartition = map[string]struct {
	inherit bool
	why     string
}{
	"Styles":     {true, "a theme is ambient: a control paints in the app's style table"},
	"Components": {true, "a host builder registration is app-wide"},
	"Elements":   {true, "the declared form of the same registration, and it must not be weaker"},
	"Handlers":   {true, "a code-behind name resolves the same everywhere"},
	"Rules":      {true, "documented as a registration exactly like Components and Handlers"},
	"Declared":   {true, "the declared-surface registry is page-wide by construction"},
	"Includes":   {true, "a control may instantiate another control"},
	"Dispatcher": {true, "there is one UI goroutine, so there is one dispatcher"},
	"Dir": {true, "the PAGE's directory <Companion> resolves Dir/Log against — " +
		"the reference paragraph is derived from this row, so the wording " +
		"corrected on Context.Dir had to reach here too"},
	"Variant": {true, "the pixel protocol is a property of the app, not of one file"},
	"catalogNoIncludes": {false, "RESET, and structurally rather than by " +
		"choice: a control is a DOCUMENT, so it goes through " +
		"document.build, which clears the memo on the way in and restores " +
		"it on the way out. The opposite answer from the row seam, which " +
		"is not a document and inherits it — and the difference is real " +
		"rather than an oversight, because a nested Load may carry a " +
		"different Context.Elements and must not hand its assembly back " +
		"to the page"},

	"Values": {false, "VALUES ISOLATE — the whole point of the boundary. They " +
		"cross only through the declared surface (<x:Property>), which is " +
		"what makes a control a contract rather than a macro"},
	"Named": {false, "Name is the page's ADDRESS BOOK. A control's internals " +
		"are not page-addressable, or PatchMarkup and markup.Find would " +
		"reach inside a control and two instances of it would collide"},

	// THE UNEXPORTED HALF, and leaving it out was not a scoping choice —
	// it was the filter in contextFields quietly deciding the contract.
	// Every one of these six is DECIDED at this boundary
	// (usercontrol.go), and the class has a track record: `arms` is
	// #459's fix, whose doc comment had PROMISED it crossed while the
	// code did not. That is the same sentence-versus-behaviour gap this
	// file exists to close, and the guard as first written would not
	// have caught it. Raised in review of #490.
	"arms": {true, "the <Frozen AllowError> scope is page-wide per build: a " +
		"control that arrived with its own must not opt out of the page's set"},
	"res": {true, "the resource chain is lexical, and a control's markup " +
		"resolves Style= against the document scope it was instantiated in"},
	"controls": {true, "the ancestry EXTENDS rather than resets — a control " +
		"appearing twice in it is the load-time cycle check"},
	"rowDepth": {true, "the row-seam counter, and it must survive a control " +
		"instantiation or a recursive template resets it every level and " +
		"the bound never fires — see MaxTemplateDepth"},

	"fsys": {false, "REPLACED, not inherited: a control's literal asset paths " +
		"resolve against the FS its OWN markup came from, the same isolation " +
		"its bindings get"},
	"ns": {false, "the xmlns table is per-DOCUMENT — an included file cannot " +
		"borrow a prefix the page happened to declare"},
	"declared": {false, "the dependency properties of the control being " +
		"instantiated, installed for the duration of one setup call"},
}

// rowPartition is the SAME question at the other seam, and it exists
// because that seam's answer was a six-entry literal in a test while
// this one was a table over every field.
//
// The asymmetry is not academic. It is what left `Elements` and
// `Variant` out of the row context in the first place — the defect the
// previous round of this PR fixed — and then, one round later, left
// `fsys` and `declared` scoped to the row with no reason written
// anywhere, while the test's own error message asserted that "the only
// fields itemsview.go may scope to a row are Named and arms". Sixteen of
// eighteen accounted for and two decided by omission is the shape this
// file exists to remove. Raised in review of #490.
//
// A ROW IS NOT A BOUNDARY, which is why most of this is `true`: the row
// is the same document, so the default is "inherits" and every `false`
// owes a reason. The control partition's defaults run the other way for
// the fields that make a control a contract.
var rowPartition = map[string]struct {
	inherit bool
	why     string
}{
	"Styles":     {true, "same document, same theme"},
	"Components": {true, "a builder registration is app-wide"},
	"Elements": {true, "the DECLARED vocabulary: without it <Meter> in a " +
		"template was unknown while the undeclared spelling worked"},
	"Handlers":   {true, "a code-behind name resolves the same in a row"},
	"Rules":      {true, "a validation rule is a registration like the rest"},
	"Includes":   {true, "a template may instantiate a control"},
	"Dispatcher": {true, "one UI goroutine, one dispatcher"},
	"Dir": {true, "Dir is the PAGE's host-side anchor, and a row does not " +
		"change which page it is in"},
	"Variant": {true, "the pixel protocol is a property of the app"},
	"catalogNoIncludes": {false, "RESET, because it is DERIVED STATE and not " +
		"a registration. Every other true in this table is something an " +
		"author registered and a row must still see; this is a memo of an " +
		"assembly over three of them, so inheriting it would be a claim " +
		"about a cache rather than about vocabulary — and a wrong one the " +
		"day a seam is handed different Elements. The control seam resets " +
		"it too, structurally, through document.build"},
	"controls": {false, "RESET, because a row is a legitimate re-entry and " +
		"identity cannot tell a terminating recursive template from a " +
		"self-supplying one. Inheriting it caught the #216 stack overflow " +
		"and also refused a finite tree view; rowDepth below is what " +
		"catches the first without the second — see MaxTemplateDepth"},
	"rowDepth": {false, "INCREMENTED, which is the one field this seam neither " +
		"inherits nor resets: crossing a template seam is exactly what it " +
		"counts"},
	"res": {true, "the resource chain is lexical and the row is lexically " +
		"inside the document"},
	"fsys": {true, "a row's markup CAME FROM the document's FS, so a literal " +
		"<Image Src> must resolve the same inside a template as outside one"},
	"ns": {true, "the xmlns table is per-DOCUMENT, and a template is part of " +
		"the document that declared the prefixes — the opposite answer from " +
		"the control seam, where an included file cannot borrow the page's. " +
		"Found by this table rather than written into it: the walk reported " +
		"ns unaccounted for on its first run"},

	"Values": {false, "the row's Values ARE the item — that is what a template is"},
	"Named": {false, "uniqueness is per DOCUMENT, and a scrolling list would " +
		"collide with itself"},
	"arms": {false, "CONSTRUCTED member by member rather than inherited: sinks " +
		"and allows are row-local, outer and nested are the page's, and " +
		"pending is the row's own. Four members, four reasons, in itemsview.go"},
	"declared": {false, "the dependency properties of the control being " +
		"instantiated, installed for the duration of one runSetup call. A row " +
		"is not that call, and the save/restore exists so a nested " +
		"instantiation cannot see the wrong declarations"},
	"Declared": {false, "TAKEN BACK after one round of inheriting it: nothing " +
		"retires a row, so sharing the page registry pinned one entry and one " +
		"dead row subtree per row ever shown. The reasoning is on " +
		"Context.Declared in markup.go and the gap is tracked as #512"},
}

// TestEveryPartitionTableIsRegistered derives the set of partition
// tables from the package source and compares it against
// partitionTables, so a table declared and not registered is RED rather
// than a nil *regexp.Regexp waiting in partitionWords.
//
// The check partitionTables' own doc asks for. Relocating the
// enumeration beside the tables made adding one a single edit at the
// declaration site — better than a literal in another file, and still a
// convention: a `var controlPartition = map[string]struct{inherit bool;
// why string}{…}` declared here and left out of the literal gets no
// pattern, partitionRunSide dereferences nil on it, and the panic
// aborts the binary before any guard prints. Raised in review of #543.
//
// BY SHAPE, NOT BY NAME SUFFIX. The thing that makes a map one of these
// tables is its TYPE — the same anonymous struct the `partition` alias
// names — so that is what the walk matches. A suffix rule would miss a
// table named something else and would claim one that merely ends in
// "Partition".
//
// go/ast rather than reflection, for the reason contextFields gives:
// CLAUDE.md's first invariant is that core carries none, and the
// declaration is right there in the source.
func TestEveryPartitionTableIsRegistered(t *testing.T) {
	source := partitionTablesInSource(t)
	declared := slices.Sorted(maps.Keys(source))
	if len(declared) < 2 {
		t.Fatalf("the source walk found %v, and this package declares at least "+
			"boundaryPartition and rowPartition — the walk is broken, and the "+
			"comparison below would pass over nothing", declared)
	}
	registered := map[string]bool{}
	for _, tb := range partitionTables() {
		registered[tb.name] = true
		// AND THE NAME NAMES THAT MAP, which the name sets above
		// cannot settle. partitionTables pairs a hand-written string
		// with a map variable, and a copy-paste row like
		// {"rowPartition", boundaryPartition} passes both arms of this
		// test (the name sets match) and the key-set guard (a table
		// compared against itself) while partitionWords builds no
		// pattern for the real table's keys — the exact hole this
		// change closes, reopened by a typo. The `why` strings are
		// what discriminate it, and the AST walk is already standing
		// on the literal. Raised in review of #543.
		want, ok := source[tb.name]
		if !ok {
			continue // reported by the arm below
		}
		// UNREADABLE IS ITS OWN ANSWER, and it has to be, because the
		// comparison below has exactly one verdict. A spelling this
		// walk does not parse — a keyed {inherit: true, why: "…"}
		// with the why built from something other than string
		// literals, or a const key — used to arrive as the empty why
		// and be reported as a pairing fault, sending the reader to
		// partitionTables, which had nothing wrong with it. That is
		// the defect class this file's own header is about, inside
		// the guard. Raised in review of #543.
		// AND THE DECLARATION ITSELF CAN BE UNREADABLE, which is not a
		// loud version of the arm below but a different verdict: zero
		// entries AND zero unread keys, so the key loop finds no source
		// entry for anything and passes over the whole table. Measured
		// in review of #543 — a `var lazy partition` populated in
		// init() with every why replaced passed this test and the
		// key-set guard. A guard certifying something it did not check
		// is this file's own subject.
		if !want.readable {
			t.Errorf("%s is declared in a shape partitionTablesInSource "+
				"cannot read — no composite literal to walk — so NONE of its "+
				"entries are compared and registering it here buys nothing. "+
				"Declare it as a literal, or teach the walk that shape",
				tb.name)
			continue
		}
		if want.unreadKeys > 0 {
			t.Errorf("%d of %s's entries are spelled in a way "+
				"partitionTablesInSource cannot read the KEY of, so they are "+
				"missing from the comparison below and this table is only "+
				"partly checked. Teach exprText that spelling, or write the "+
				"key as a plain string literal", want.unreadKeys, tb.name)
		}
		// SORTED, so a red tree says the same thing twice. The loop
		// breaks on the first mismatch and tb.part is a map, so map
		// iteration order made the key this message names change
		// between two runs of the same failure — in a file whose
		// declared purpose is landing the reader on the cause in one
		// step. Raised in review of #543.
		for _, key := range slices.Sorted(maps.Keys(tb.part)) {
			entry := tb.part[key]
			got, ok := want.entries[key]
			if !ok {
				continue // the unreadKeys arm above owns this
			}
			if !got.read {
				t.Errorf("the %q entry of %s is spelled in a way "+
					"partitionTablesInSource cannot read the why of, so it "+
					"cannot be compared. Teach partitionWhy that spelling, "+
					"or write the why as a string literal or a + chain of "+
					"them — this is NOT a pairing fault", key, tb.name)
				break
			}
			if got.why != entry.why {
				t.Errorf("partitionTables registers %s, but the map it hands "+
					"over answers %q for %q where the declaration of %s in "+
					"this package's source says %q. The name is paired with "+
					"the wrong map", tb.name, entry.why, key, tb.name, got.why)
				break
			}
		}
	}
	for _, name := range declared {
		if !registered[name] {
			t.Errorf("%s is declared in this package and not returned by "+
				"partitionTables, so partitionWords builds no pattern for its "+
				"keys and partitionRunSide nil-dereferences on the first one "+
				"it is handed — a panic in whichever test runs first, naming "+
				"nothing. Add it to partitionTables", name)
		}
	}
	// AND NOTHING REGISTERED HAS GONE AWAY, which is the direction a
	// deletion breaks: partitionTables would name a variable that no
	// longer compiles, so this arm can only fire while it does.
	for name := range registered {
		if !slices.Contains(declared, name) {
			t.Errorf("partitionTables names %s, which the source walk does not "+
				"find — the matcher below it has stopped recognising a table "+
				"it is meant to cover", name)
		}
	}
}

// partitionTablesInSource is every package-level map in this package
// whose value type is the partition struct, by name, with each one's
// declared key -> why text.
//
// THE why TEXT IS WHY THIS RETURNS MORE THAN NAMES. Key sets cannot
// discriminate a name paired with the wrong map — the key-set guard
// forces every table's keys equal — so the reasons are the only field
// that differs between two tables, and they are right there in the
// literal. Raised in review of #543.
//
// EVERY .go FILE, not just the tests. The glob was *_test.go, which
// made the doc above wider than the walk: a partition table newly
// declared in a non-test `package markup` file was invisible, so
// `registered` never had to contain it and nothing went red — the
// exact silent case the unregistered-table arm exists to catch.
// (A table MOVED out of a test file was already caught, by the third
// arm: partitionTables still names it and the walk stops finding it.)
// Reproduced in review of #543: the same probe table reddens the test
// as markup/zzprobe_test.go and is silent as markup/zzprobe.go.
//
// PACKAGE markup ONLY, and that filter is load-bearing rather than
// tidiness — it is also what makes the wider glob safe. The glob
// reaches markup/thirdparty_test.go, which is `package markup_test`,
// and a partition-shaped var declared there would be reported as
// unregistered and COULD NOT BE FIXED, because partitionTables() is in
// `package markup` and cannot name it. Nothing is shaped that way
// today, which is what makes it the silent kind. Raised in review of
// #543.
func partitionTablesInSource(t *testing.T) map[string]partitionSource {
	t.Helper()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("globbing this package: %v", err)
	}
	out := map[string]partitionSource{}
	for _, f := range files {
		file, err := parser.ParseFile(gotoken.NewFileSet(), f, nil, parser.ParseComments)
		if err != nil {
			t.Fatalf("%s does not parse: %v", f, err)
		}
		if file.Name.Name != "markup" {
			continue
		}
		maps.Copy(out, partitionTablesIn(file))
	}
	return out
}

// partitionTablesIn is the per-file half of the walk above, split out
// so a FIXTURE can drive it.
//
// Four of the branches below and in the helpers they call are
// unexercised by this package's two real tables — the keyed `why`
// spelling, the `partition` type alias, a var with a declared type, and
// every unreadable-key path — so loosening any of them changes nothing
// in the tree and the next edit is unguarded. Each was verified by hand
// mutation when it was written, which is the right measurement in the
// wrong place. `TestThePartitionSourceReaderSeesWhatItClaimsTo`
// supplies the corpus the tree does not, the way
// `TestTheZeroedTopMatcherSeesOnlyAReleasedSlot` does one module over.
// Raised in review of #543.
func partitionTablesIn(file *ast.File) map[string]partitionSource {
	out := map[string]partitionSource{}
	for _, d := range file.Decls {
		g, ok := d.(*ast.GenDecl)
		if !ok || g.Tok != gotoken.VAR {
			continue
		}
		for _, sp := range g.Specs {
			vs, ok := sp.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, n := range vs.Names {
				if !isPartitionLiteral(typeOfSpec(vs, i)) {
					continue
				}
				src := partitionSource{
					entries:  map[string]partitionEntrySource{},
					readable: true,
				}
				if i >= len(vs.Values) {
					src.readable = false
					out[n.Name] = src
					continue
				}
				cl, ok := vs.Values[i].(*ast.CompositeLit)
				if !ok {
					src.readable = false
					out[n.Name] = src
					continue
				}
				for _, el := range cl.Elts {
					kv, ok := el.(*ast.KeyValueExpr)
					if !ok {
						src.unreadKeys++
						continue
					}
					key, kok := exprText(kv.Key)
					if !kok {
						// A KEY THIS WALK CANNOT READ IS COUNTED,
						// not dropped. Dropped, it was simply
						// absent from the map below, and the
						// comparison read that absence as the
						// empty why — a PAIRING fault, which is a
						// different and false accusation. Raised
						// in review of #543.
						src.unreadKeys++
						continue
					}
					why, wok := partitionWhy(kv.Value)
					src.entries[key] = partitionEntrySource{why: why, read: wok}
				}
				out[n.Name] = src
			}
		}
	}
	return out
}

// partitionSource is one table as the source walk read it.
//
// THE BOOLS ARE THE POINT. "unreadable" and "empty" are different
// answers, and folding them into one string made every spelling this
// walk does not parse arrive at the comparison as an empty why — i.e.
// as a PAIRING fault, which is false and sends the reader to
// partitionTables. Raised in review of #543.
type partitionSource struct {
	entries map[string]partitionEntrySource
	// unreadKeys is entries whose KEY this walk could not read, which
	// cannot be recorded in entries because there is no key to record
	// them under.
	unreadKeys int
	// readable is whether the DECLARATION itself is one this walk can
	// read at all, which unreadKeys cannot say: a var with no
	// initializer, or one initialised from something that is not a
	// composite literal, yields zero entries and zero unread keys. That
	// pair used to register as a table checked against nothing —
	// measured, a table populated in init() with every why replaced
	// passed both guards — which is this file's own subject one
	// spelling further out. Raised in review of #543.
	readable bool
}

// partitionEntrySource is one entry's why text and whether the literal
// spelling it came from is one this walk understands.
type partitionEntrySource struct {
	why  string
	read bool
}

// partitionWhy is the `why` text of one partition entry, with the
// file's own line continuations joined back together, and whether the
// literal was a spelling this reader understands.
//
// BOTH SPELLINGS, keyed and positional, because gofmt accepts both and
// {inherit: true, why: "…"} is not a mistake — it read only the
// positional {true, "…"} until review of #543 wrote one existing entry
// the other way and watched a correctly paired table be reported as
// mis-paired.
func partitionWhy(e ast.Expr) (string, bool) {
	cl, ok := e.(*ast.CompositeLit)
	if !ok {
		return "", false
	}
	keyed := false
	for _, el := range cl.Elts {
		kv, ok := el.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		keyed = true
		if id, ok := kv.Key.(*ast.Ident); ok && id.Name == "why" {
			return concatText(kv.Value)
		}
	}
	// A KEYED LITERAL THAT OMITS why HAS ONE, and it is "". Falling
	// through to the positional arm reported {inherit: true} as
	// UNREADABLE — the same misdiagnosis the (string, bool) split was
	// made to remove, one spelling further in, and with a message
	// offering two remedies that do not apply: there is no spelling to
	// teach and no string to write. The argument for reading the keyed
	// form at all — gofmt accepts it and writing one is not a mistake —
	// covers omitting a zero-valued field verbatim. Raised in review of
	// #543.
	if keyed {
		return "", true
	}
	// AND {} IS THE SAME ANSWER, reached from the other side. An empty
	// literal sets no keys, so `keyed` stays false and the positional
	// arm's arity test fires — reporting the zero value of the struct,
	// which is legal gofmt-clean Go for `inherit: false, why: ""`, as a
	// spelling this walk cannot parse. The paragraph above applies to it
	// verbatim: there is nothing to teach and no string to write. Raised
	// in review of #543.
	if len(cl.Elts) == 0 {
		return "", true
	}
	if len(cl.Elts) != 2 {
		return "", false
	}
	return concatText(cl.Elts[1])
}

// concatText unquotes a string literal, or a `"a" + "b" + …` chain of
// them — the shape a 72-column comment width forces on every reason
// long enough to be worth reading — and reports whether every leaf was
// one it could read.
func concatText(e ast.Expr) (string, bool) {
	switch v := e.(type) {
	case *ast.BasicLit:
		s, err := strconv.Unquote(v.Value)
		if err != nil {
			return "", false
		}
		return s, true
	case *ast.BinaryExpr:
		if v.Op != gotoken.ADD {
			return "", false
		}
		x, xok := concatText(v.X)
		y, yok := concatText(v.Y)
		return x + y, xok && yok
	}
	return "", false
}

// exprText is the unquoted value of a BasicLit string key, and whether
// the expression was one: a const key, or any other expression, is not
// something this walk can resolve, and saying so is the caller's job.
func exprText(e ast.Expr) (string, bool) {
	lit, ok := e.(*ast.BasicLit)
	if !ok {
		return "", false
	}
	s, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", false
	}
	return s, true
}

// typeOfSpec is the type expression for the i'th name in a var spec:
// the declared type when there is one, otherwise the initializer's own
// composite-literal type.
func typeOfSpec(vs *ast.ValueSpec, i int) ast.Expr {
	if vs.Type != nil {
		return vs.Type
	}
	if i < len(vs.Values) {
		if cl, ok := vs.Values[i].(*ast.CompositeLit); ok {
			return cl.Type
		}
	}
	return nil
}

// isPartitionLiteral reports whether e is map[string]struct{inherit
// bool; why string} — spelled out or through the partition alias.
func isPartitionLiteral(e ast.Expr) bool {
	if id, ok := e.(*ast.Ident); ok && id.Name == "partition" {
		return true
	}
	m, ok := e.(*ast.MapType)
	if !ok {
		return false
	}
	if id, ok := m.Key.(*ast.Ident); !ok || id.Name != "string" {
		return false
	}
	st, ok := m.Value.(*ast.StructType)
	if !ok || st.Fields == nil || len(st.Fields.List) != 2 {
		return false
	}
	want := []struct{ name, typ string }{{"inherit", "bool"}, {"why", "string"}}
	for i, fl := range st.Fields.List {
		if len(fl.Names) != 1 || fl.Names[0].Name != want[i].name {
			return false
		}
		id, ok := fl.Type.(*ast.Ident)
		if !ok || id.Name != want[i].typ {
			return false
		}
	}
	return true
}

// namedPartition is a partition table with its NAME, because every
// consumer of the set either iterates them all or has to say which one
// is at fault, and a map has no name to print.
type namedPartition struct {
	name string
	part partition
}

// partitionTables is EVERY partition table, declared beside them and
// read by everything that has to cover all of them — partitionWords'
// union and `TestThePartitionTablesShareOneKeySet` today.
//
// It exists because the union was a two-element literal in
// referencedoc_test.go, which moved the coupling rather than removing
// it: a third table declared here and left out of that literal
// reintroduces the nil *regexp.Regexp partitionRunSide dereferences,
// with the same symptom (a panic in whichever test is declared first)
// and nothing red to name it. That is the enumerated-list shape
// CLAUDE.md refuses, at a two-element sample.
//
// IT IS STILL A LITERAL, and that is a convention rather than a check —
// so `TestEveryPartitionTableIsRegistered` derives the set from the
// package source, the way `TestTheControlBoundaryPartitionsEveryContextField`
// derives Context's fields, and goes red when a table is declared here
// and left out of this function. Raised in review of #543, twice: the
// first round relocated the enumeration and the doc above said why
// relocating is not enough.
func partitionTables() []namedPartition {
	return []namedPartition{
		{"boundaryPartition", boundaryPartition},
		{"rowPartition", rowPartition},
	}
}

// TestTheControlBoundaryPartitionsEveryContextField is the derived half:
// boundaryPartition must account for exactly the exported fields
// Context declares, no more and no fewer.
//
// NAMED, NOT "the partition above". It was the declaration above when
// this was written and is 294 lines up now, with rowPartition, a test,
// four AST helpers, a type and partitionTables in between — so the
// positional reference had come to point at partitionTables(). A name
// costs a word and cannot drift with the next insertion. Raised in
// review of #543.
//
// AST, not reflection — CLAUDE.md's first invariant is that core carries
// none, and a test that imported it to read a struct would be the first
// exception in the package. The declaration is right there in the source
// and parsing it costs nothing.
func TestTheControlBoundaryPartitionsEveryContextField(t *testing.T) {
	declared := contextFields(t)
	for _, name := range declared {
		if _, ok := boundaryPartition[name]; !ok {
			t.Errorf("Context.%s is not in boundaryPartition: every field a "+
				"page can set either crosses into a control or is "+
				"deliberately withheld, and which one it is has to be "+
				"decided rather than defaulted. Add it with the reason, "+
				"then let the behavioural test below check the claim", name)
		}
	}
	have := map[string]bool{}
	for _, n := range declared {
		have[n] = true
	}
	for name := range boundaryPartition {
		if !have[name] {
			t.Errorf("boundaryPartition names %s, which Context no longer "+
				"declares — a rule about a field that is gone", name)
		}
	}
}

// TestEveryInheritedRegistrationReachesAControl is the behavioural half,
// and it is the one that would have caught all four.
//
// It sets every field on the page's context, instantiates a control, and
// reads the context the control's own children are built with — which is
// the child context itself, reached through a registered builder rather
// than inferred from an error message. An error-message probe answers
// "did this particular markup load", which is a different and weaker
// question: Elements failed with `unknown element`, Rules with `unknown
// rule`, and Dir with nothing at all.
//
// THE SWITCH IS EXHAUSTIVE BY CONSTRUCTION. It has to name fields one at
// a time — without reflection there is no other way to read them — but
// the test above pins the partition against the struct, and the default
// arm fails on a field nobody wired up here. So forgetting one is a red
// test rather than a quiet gap, which is the property #314 shows the
// hand-maintained block did not have.
func TestEveryInheritedRegistrationReachesAControl(t *testing.T) {
	// TWO FILE SYSTEMS, because one cannot tell `fsys` apart from
	// Includes: the page is loaded from pageFS and the control resolves
	// out of ctlFS, so "the FS this control's own markup came from" is a
	// question with a different answer from "the page's".
	//
	// The page also declares a RESOURCE SCOPE and an XMLNS PREFIX. Both
	// exist for the unexported arms below: `res` is only non-nil in the
	// child because it crossed, and `ns` is only interesting if the page
	// has a prefix for the child to fail to inherit. A fixture that
	// leaves a field zero on the page reads "did not cross" for a
	// correct implementation and proves nothing — which is the argument
	// TestTheBoundaryProbeCanActuallySeeAFailure already makes.
	pageFS := fstest.MapFS{
		"page.gooey": {Data: []byte(`<Gooey xmlns:probe="urn:boundary-probe">
  <Gooey.Resources>
    <Style Key="pageRes" Fg="#ffaa3c"/>
  </Gooey.Resources>
  <Card/>
</Gooey>`)},
	}
	ctlFS := fstest.MapFS{
		"card.gooey": {Data: []byte(`<Gooey><Probe/></Gooey>`)},
	}
	// A SENTINEL ENTRY, not an empty map. Declared's own doc calls it
	// page-wide — "child contexts inherit the same map, so nested
	// control instances are visible from the context the page was built
	// against" — which is what lets a tree snapshot report a nested
	// control's surface. `child.Declared != nil` cannot see that
	// promise break: swap the assignment for a fresh empty map and the
	// arm still reads "crossed" while the registry silently becomes
	// per-control. Raised in review of #490.
	declSentinel := &components.Text{}
	var child *Context
	page := &Context{
		Includes: ctlFS,
		Dir:      "/tmp/anchor",
		Variant:  "sixel",
		// NON-ZERO, because the rowDepth arm is an equality and two
		// zeroes satisfy one. The page is not actually inside three
		// templates; the value is a sentinel, and the claim being
		// checked is that a control instantiation carries it across
		// unchanged rather than starting again at nothing.
		rowDepth:   3,
		Values:     map[string]any{"N": 1},
		Named:      map[string]gooey.Component{"PageOnly": &components.Text{}},
		Declared:   map[gooey.Component]DeclaredSurface{declSentinel: {Control: "PageOnly"}},
		Styles:     map[string]render.Style{"s": {}},
		Handlers:   map[string]gooey.Action{"H": gooey.Command(func() {})},
		Elements:   map[string]*ElementDef{"Meter": meterDef()},
		Dispatcher: gooey.NewDispatcher(),
		// THE LAST INERT ARM. declared was the one unexported field left
		// on `!= nil` with nothing behind it: the fixture never set it,
		// so the arm read false whatever control() did. Measured —
		// adding `child.declared = parent.declared` to control() and the
		// test still PASSED, which is the exact failure the rest of this
		// fixture exists to remove. The leak it could not see is real:
		// runSetup saves and restores declared so a setup that itself
		// instantiates a control cannot see the wrong declarations, and
		// a crossing declared breaks that. Raised in review of #490.
		declared: map[string]any{"PageDecl": nil},
		Rules: map[string]RuleFunc{
			"Zonk": func(string) (validate.Rule[string], error) { return nil, nil },
		},
		Components: map[string]Builder{
			"Probe": func(e Element, c *Context) (gooey.Component, error) {
				child = c
				return &components.Text{}, nil
			},
		},
	}
	if _, err := Load(pageFS, "page.gooey", page); err != nil {
		t.Fatalf("the page did not load, so nothing below was observed: %v", err)
	}
	if child == nil {
		t.Fatal("the probe builder never ran, so no child context was reached")
	}

	for _, name := range contextFields(t) {
		rule, ok := boundaryPartition[name]
		if !ok {
			continue // the test above reports this
		}
		// EVERY ARM ASKS FOR THE PAGE'S OWN VALUE, never merely whether
		// something is there. Eight of these read `!= nil` until review
		// of #490, which answers "is there a map here" rather than "is it
		// the page's map" — so replacing an inherited registry with a
		// fresh empty one of the same type read as "crossed" and the
		// contract broke silently. The PR had already found that
		// reasoning wrong for Named and stopped at the one field where
		// it visibly misfired. Every fixture key below is a sentinel the
		// page set and nothing else could have produced.
		var crossed bool
		switch name {
		case "Styles":
			_, crossed = child.Styles["s"]
		case "Components":
			_, crossed = child.Components["Probe"]
		case "Elements":
			_, crossed = child.Elements["Meter"]
		case "Handlers":
			_, crossed = child.Handlers["H"]
		case "Rules":
			_, crossed = child.Rules["Zonk"]
		case "Declared":
			_, crossed = child.Declared[declSentinel]
		case "Includes":
			// The FS itself, asked a question only the page's answers —
			// behind a nil check, because dropping the propagation
			// leaves a nil interface and fs.ReadFile PANICS on one. A
			// panic takes the rest of the package's run with it, which
			// is a worse answer than the report this arm was written to
			// give. Raised in review of #490.
			if child.Includes != nil {
				_, err := fs.ReadFile(child.Includes, "card.gooey")
				crossed = err == nil
			}
		case "Dispatcher":
			// A POINTER COMPARE, which is available here and is the
			// whole claim: there is one UI goroutine, so a control
			// posting to a different dispatcher is the defect. The
			// non-nil half is not redundant — this page happens to carry
			// a dispatcher and the row test's did not, which is how the
			// identical arm over there passed against a deleted
			// propagation. Stating it here keeps the two arms from
			// diverging again. Raised in review of #490.
			crossed = child.Dispatcher != nil && child.Dispatcher == page.Dispatcher
		case "arms":
			// Non-nil IS the sentinel here, and for a reason worth
			// stating: a child Context is constructed fresh at this
			// boundary, so its armScope is zero unless the assignment
			// ran. The page's set exists because Load made one.
			crossed = child.arms.sinks != nil
		case "res":
			// Same shape: the page declares <Gooey.Resources>, so a
			// non-nil scope in the child can only have come across.
			crossed = child.res.cur != nil
		case "rowDepth":
			// CARRIED UNCHANGED. A control instantiation is not a row
			// seam, so the counter neither resets nor advances here —
			// and if it reset, a recursive template would start again
			// at zero on every level and MaxTemplateDepth would never
			// fire. The page's sentinel is 3 so this is not two zeroes
			// agreeing.
			crossed = child.rowDepth == page.rowDepth
		case "catalogNoIncludes":
			// RESET, and structurally: a control is a DOCUMENT, so
			// document.build clears the memo on the way in and restores
			// it on the way out. Non-nil here would mean a nested load
			// answering from the page's assembly — which is exactly the
			// staleness the clear exists to prevent, since the child
			// may carry different Elements. See boundaryPartition.
			crossed = child.catalogNoIncludes != nil
		case "controls":
			// It EXTENDS rather than copies, so the claim is that the
			// ancestry names the control being built. The entries are
			// FILE names, not element names — <Card/> resolves through
			// the Includes convention to card.gooey, and the cycle check
			// is about which document is already on the stack.
			crossed = len(child.controls) > 0 &&
				child.controls[len(child.controls)-1] == "card.gooey"
		case "fsys":
			// Not "is it set" but "is it the CONTROL's": the page's own
			// file must not be readable through it.
			//
			// Behind a nil check, because dropping `child.fsys = fsys`
			// from usercontrol.go leaves a nil interface and fs.ReadFile
			// PANICS on one — taking the rest of the package's run with
			// it instead of giving the report this arm exists for. Its
			// two siblings (Includes above, the row's fsys below) already
			// guard it; this one did not. Raised in review of #490.
			//
			// AND THE NIL CASE IS ITS OWN FAULT, not "did not cross".
			// The guard alone would turn the panic into a SILENT PASS:
			// the partition says fsys must not inherit, so `crossed =
			// false` is the expected answer and a control handed no FS
			// at all satisfies it. Measured — with only the guard,
			// dropping `child.fsys = fsys` left this test green. A
			// control always gets an FS; which one is the question the
			// arm below asks.
			if child.fsys == nil {
				t.Errorf("the control context has NO file system at all, so its " +
					"markup could not resolve an <Image Src> of its own. This is " +
					"not the same as \"the page's did not cross\" — usercontrol.go " +
					"must REPLACE fsys, and dropping the assignment reads as " +
					"withheld to a partition that only asks whether the page's " +
					"value arrived")
				continue
			}
			_, viaCtl := fs.ReadFile(child.fsys, "card.gooey")
			_, viaPage := fs.ReadFile(child.fsys, "page.gooey")
			crossed = viaCtl != nil || viaPage == nil
		case "ns":
			// The page declares xmlns:probe; a control's document
			// declares its own namespaces or has none.
			_, crossed = child.ns["probe"]
		case "declared":
			_, crossed = child.declared["PageDecl"]
		case "Dir":
			crossed = child.Dir == page.Dir
		case "Variant":
			crossed = child.Variant == page.Variant
		case "Values":
			// The declared surface is the only road, and this control
			// declares nothing, so the page's N must not be visible.
			_, crossed = child.Values["N"]
		case "Named":
			// A SENTINEL, not a nil check. The child gets its own map —
			// named() makes one lazily — so `!= nil` reads as "crossed"
			// for a correctly isolated control. The contract is that the
			// PAGE's entries are not visible from inside, which is what
			// the sentinel asks. Measured: the nil check reported Named
			// crossing, against a boundary that was working.
			_, crossed = child.Named["PageOnly"]
		default:
			t.Errorf("Context.%s is partitioned but this switch does not read "+
				"it, so its half of the contract is unchecked", name)
			continue
		}
		if crossed != rule.inherit {
			verb := "did not cross into the control"
			if crossed {
				verb = "crossed into the control and must not have"
			}
			t.Errorf("Context.%s %s — %s", name, verb, rule.why)
		}
	}
}

// TestTheBoundaryProbeCanActuallySeeAFailure is the must-fire arm.
//
// The test above compares a computed bool against a declared one and
// reports nothing when they agree — a negative assertion, which passes
// for any reason, including a probe that never observed anything. It
// already fails loudly on a nil child, so what is left to prove is that
// the page's context really did carry every field into the comparison:
// a field left zero on the page would read as "did not cross" for a
// correct implementation, and a field the page set to the same zero the
// child has would read as "crossed" for a broken one.
func TestTheBoundaryProbeCanActuallySeeAFailure(t *testing.T) {
	fsys := fstest.MapFS{
		"page.gooey": {Data: []byte(`<Gooey><Card/></Gooey>`)},
		"card.gooey": {Data: []byte(`<Gooey><Probe/></Gooey>`)},
	}
	var child *Context
	page := &Context{
		Includes: fsys,
		Components: map[string]Builder{
			"Probe": func(e Element, c *Context) (gooey.Component, error) {
				child = c
				return &components.Text{}, nil
			},
		},
	}
	if _, err := Load(fsys, "page.gooey", page); err != nil {
		t.Fatalf("the page did not load: %v", err)
	}
	if child == nil {
		t.Fatal("the probe builder never ran")
	}
	// With Rules unset on the page, the inheriting arm must read FALSE.
	// If it reads true the probe is looking at something other than the
	// child — the page itself, most likely — and every "crossed" verdict
	// above is meaningless.
	if child.Rules != nil {
		t.Error("the child reports an inherited Rules map the page never " +
			"set, so the probe is not observing the child context")
	}
	if child.Components == nil {
		t.Error("the child reports no Components, but the probe builder that " +
			"set it came from exactly that map — the observation is not of a " +
			"real child context")
	}
}

// contextFields parses Context's field names out of the source — ALL of
// them, exported or not.
//
// It filtered on nm.IsExported() until review of #490, which put six
// fields outside the contract: fsys, arms, ns, declared, res and
// controls. Every one is decided at this boundary, and `arms` is the
// case with a record — it is #459's fix, and its own doc comment had
// PROMISED it crossed while the code did not, which is exactly the gap
// this file was written to close. A filter that drops the fields most
// likely to rot is the guard choosing not to look where the bug was.
//
// Reading them is legal because this test is IN package markup; the
// switch below names each one directly, no reflection.
// IT WALKS THE PACKAGE rather than naming markup.go. Pinning the
// filename meant that moving Context to another file in this package
// tripped the len(out) == 0 fatal below, whose message diagnoses the
// removed IsExported filter — a red test pointing at the wrong cause,
// which costs more than the walk does. Raised in review of #490.
func contextFields(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("globbing this package: %v", err)
	}
	var out []string
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(gotoken.NewFileSet(), f, nil, 0)
		if err != nil {
			t.Fatalf("%s does not parse: %v", f, err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			ts, ok := n.(*ast.TypeSpec)
			if !ok || ts.Name.Name != "Context" {
				return true
			}
			st, ok := ts.Type.(*ast.StructType)
			if !ok {
				return false
			}
			for _, fl := range st.Fields.List {
				out = append(out, fieldNames(fl)...)
			}
			return false
		})
		if len(out) > 0 {
			break
		}
	}
	if len(out) == 0 {
		t.Fatal("no field was found on Context, so every loop over this list " +
			"would pass over nothing. NOT \"no exported field\": this walk " +
			"stopped filtering on IsExported when the unexported half of the " +
			"boundary came under the partition, and a message naming the old " +
			"filter is the strongest comment in the file pointing at the wrong " +
			"rule")
	}
	sort.Strings(out)
	return out
}

// fieldNames is the names a struct field declares — a list rather than a
// name because `a, b int` is one ast.Field — and for an EMBEDDED field,
// the type's own name, which is the name Go gives it.
//
// This comment carried its own superseded first paragraph above this
// one for two rounds, saying embedded fields are SKIPPED and arguing
// that inventing a name from the type is wrong. embeddedName below does
// exactly that. Godoc renders both, stale one first, so the rule a
// reader took away was the one the code had stopped following — in the
// file whose whole thesis is that a sentence outliving its behaviour is
// the defect. Removed in review of #490.
//
// f.Names is empty for an embedded field, so returning it alone dropped
// one silently: a Context growing `armScope` or a `*Dispatcher` inline
// would be in neither partition table and neither guard would say so,
// which is the exact failure both of them exist to prevent one level up.
// Raised in review of #490.
func fieldNames(f *ast.Field) []string {
	if len(f.Names) == 0 {
		if n := embeddedName(f.Type); n != "" {
			return []string{n}
		}
		return nil
	}
	out := make([]string, 0, len(f.Names))
	for _, nm := range f.Names {
		out = append(out, nm.Name)
	}
	return out
}

// embeddedName is the field name Go gives an embedded type: the type's
// own name, with any pointer and package qualifier stripped.
func embeddedName(t ast.Expr) string {
	switch x := t.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.StarExpr:
		return embeddedName(x.X)
	case *ast.SelectorExpr:
		return x.Sel.Name
	case *ast.IndexExpr: // an embedded generic instantiation
		return embeddedName(x.X)
	}
	return ""
}

// TestVariantResolutionSurvivesNesting is #314's fourth defect on its
// own, because the partition test cannot see it.
//
// child.Variant being empty was HARMLESS AT DEPTH 1 — resolveVariant
// reads parent.Variant, and for a page-level control the parent is the
// page. One level down the parent is the child context, so the file
// choice silently fell back to the unspecialized name. A field-level
// check on the child would have flagged it, but only because the field
// happened to be readable; the thing that actually broke is which FILE
// was loaded, and that is what this asserts.
func TestVariantResolutionSurvivesNesting(t *testing.T) {
	base := map[string]string{
		"outer.gooey":       `<Gooey><Inner/></Gooey>`,
		"outer.sixel.gooey": `<Gooey><Inner/></Gooey>`,
		"inner.gooey":       `<Gooey><Text>PLAIN</Text></Gooey>`,
		"inner.sixel.gooey": `<Gooey><Text>SIXEL</Text></Gooey>`,
	}
	for _, tc := range []struct{ name, page string }{
		{"page to control", `<Gooey><Inner/></Gooey>`},
		{"page to control to control", `<Gooey><Outer/></Gooey>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fsys := fstest.MapFS{}
			for k, v := range base {
				fsys[k] = &fstest.MapFile{Data: []byte(v)}
			}
			fsys["page.gooey"] = &fstest.MapFile{Data: []byte(tc.page)}
			ctx := &Context{Includes: fsys, Variant: "sixel"}
			root, err := Load(fsys, "page.gooey", ctx)
			if err != nil {
				t.Fatalf("did not load: %v", err)
			}
			got := firstText(root)
			if got == "" {
				t.Fatal("no <Text> was built, so this test read nothing")
			}
			if got != "SIXEL" {
				t.Errorf("the control resolved to %q — the app asked for the "+
					"sixel variant and got the unspecialized file, with no "+
					"error anywhere. Variant is read off the parent, and one "+
					"level down the parent is the child context", got)
			}
		})
	}
}

func firstText(c gooey.Component) string {
	if tx, ok := c.(*components.Text); ok && tx.Content != nil {
		return tx.Content.Get()
	}
	ct, ok := c.(gooey.Container)
	if !ok {
		return ""
	}
	for _, k := range ct.ChildComponents() {
		if s := firstText(k); s != "" {
			return s
		}
	}
	return ""
}

// TestEveryInheritedRegistrationReachesATemplateRow is the same contract
// at the seam the partition forgot it had.
//
// boundaryPartition and docs/markup-reference.md both state the rule
// globally — "everything a page registers inherits". control() honours
// it. The ITEM-TEMPLATE row context did not: markup/itemsview.go built
// its own literal, copied ten fields and dropped six, so a row was a
// control boundary nobody had declared, with a different partition and
// no statement of it anywhere.
//
// Two of the six cost more than a missing convenience. Elements is the
// DECLARED vocabulary, so <Meter Level="{{.N}}"/> in a template failed
// with `unknown element <Meter>` while the undeclared Components
// spelling worked — the incentive backwards, which is the defect #314
// exists to remove, reproduced one seam over from the one it fixed. And
// controls is the cycle ancestry: dropping it RESET it, so a control
// whose template instantiates itself recursed to `fatal error: stack
// overflow` rather than the load error indexOf(parent.controls, name)
// exists to give — and a fatal skips Screen.Restore.
//
// NOT A COPY OF THE SWITCH ABOVE, because a row is not a control and
// three fields diverge for reasons of their own: Values IS the item,
// Named is row-scoped (names are unique per document, not per row), and
// arms is constructed member by member with four separate arguments
// recorded in itemsview.go. What this asserts is the six that had no
// reason at all. Raised in review of #490.
func TestEveryInheritedRegistrationReachesATemplateRow(t *testing.T) {
	// LOADED FROM AN FS, not built from bytes, because fsys is one of the
	// fields under test and Build leaves it nil — which would read as
	// "did not cross" for a correct implementation and prove nothing.
	// The page also declares a RESOURCE SCOPE and an XMLNS PREFIX for the
	// unexported arms, exactly as the control fixture next door does.
	pageFS := fstest.MapFS{
		"page.gooey": {Data: []byte(`<Gooey xmlns:probe="urn:boundary-probe">
  <Gooey.Resources>
    <Style Key="pageRes" Fg="#ffaa3c"/>
  </Gooey.Resources>
  <VStack>
    <PageProbe/>
    <ItemsView Name="List" Items="{{.Items}}">
      <ItemsView.ItemTemplate><Probe/></ItemsView.ItemTemplate>
    </ItemsView>
  </VStack>
</Gooey>`)},
	}
	ctlFS := fstest.MapFS{"card.gooey": {Data: []byte(`<Gooey><Text>x</Text></Gooey>`)}}

	declSentinel := &components.Text{}
	// THE ARMS SENTINEL, written into the page's sink map WHILE IT IS
	// LIVE by a page-level probe that builds before the <ItemsView> does.
	//
	// Reading page.arms after Load cannot answer this: document.build
	// restores the whole arm scope in its defer, so
	// page.arms.sinks is nil by the time the switch runs, and the arm's
	// old form — `len(page.arms.sinks) > 0 && sameSinks(...)` — was
	// therefore false whatever itemsview.go did. Measured in review of
	// #490: giving the row the page's own map, exactly what this arm
	// says must not happen, left the test PASSING.
	armsSentinel := prop.NewSource("")
	// items is held rather than inlined so the composer below can force
	// rows the load never realized.
	items := prop.NewSource([]string{"a"})
	var row, lateRow *Context
	// loaded flips when Load returns, which is what tells the Probe
	// builder which phase it is running in.
	var loaded bool
	lateRead := map[string]bool{}
	// The four fields a page build SAVES AND RESTORES — ns and arms in
	// document.build's own deferred closures, res through the pop
	// pushDocumentResources hands back, and fsys in Load's. A read of
	// any of them AFTER Load is a read of the value the build put back,
	// not the value the build had, and that gap is what the arms arm
	// below was burned by.
	// Both sides of it are measured rather than described: scopes taken
	// live here, compared against the same reads after Load.
	type scopes struct{ ns, arms, fsys, res bool }
	live := func(c *Context) scopes {
		return scopes{c.ns != nil, c.arms.sinks != nil, c.fsys != nil, c.res.cur != nil}
	}
	var pageLive, rowLive scopes
	// EVERY FIELD, READ WHILE THE ROW IS LIVE. The four scope fields
	// above are the ones a build is known to restore, but "known" is the
	// weak word in that sentence: the row's OTHER thirteen fields were
	// read after Load too, so a future field restored on the row would
	// be measured after the restore and this guard would report the
	// value the build put back. crossedIn is the whole switch, called
	// once inside the Probe builder and once afterwards; the LIVE answer
	// is the one the partition is checked against, and a disagreement
	// between the two is reported on its own. Raised in review of #490.
	var crossedIn func(row *Context, name string) (crossed, known bool)
	liveRead := map[string]bool{}
	page := &Context{
		Dir:      "/tmp/anchor",
		Variant:  "sixel",
		Includes: ctlFS,
		// A SENTINEL, for the reason the control fixture's copy gives:
		// the row arm asserts an INCREMENT, and 0 -> 1 is satisfied by
		// a counter that was reset and then advanced.
		rowDepth: 3,
		// SET, because nil == nil. The Dispatcher arm below is a pointer
		// compare, and a page that carries no dispatcher makes it read
		// true against a row that carries none either — so the arm passed
		// with the propagation deleted from itemsview.go. Raised in
		// review of #490, which measured exactly that.
		Dispatcher: gooey.NewDispatcher(),
		Styles:     map[string]render.Style{"s": {}},
		Handlers:   map[string]gooey.Action{"H": gooey.Command(func() {})},
		Named:      map[string]gooey.Component{"PageOnly": &components.Text{}},
		Declared:   map[gooey.Component]DeclaredSurface{declSentinel: {Control: "PageOnly"}},
		Elements:   map[string]*ElementDef{"Meter": meterDef()},
		Rules: map[string]RuleFunc{
			"Zonk": func(string) (validate.Rule[string], error) { return nil, nil },
		},
		Values: map[string]any{
			"PageOnly": prop.NewSource("page"),
			"Items": components.Items(items,
				func(s string) map[string]any { return map[string]any{"S": s, "N": 1} }),
		},
		Components: map[string]Builder{
			"Probe": func(e Element, c *Context) (gooey.Component, error) {
				into := liveRead
				if loaded {
					// A ROW NOBODY REALIZED AT LOAD TIME. Everything
					// above this line is ItemsView.Validate's throwaway
					// probe row, built while the page build is still
					// running; a row the user scrolls to is built by the
					// composer after Load's defers have put ns, arms,
					// fsys and res back. Raised in review of #490, which
					// measured the gap: reverting the docFS capture in
					// itemsview.go left this test green and only
					// TestAPageRelativeAssetPathWorksInsideARow red.
					into = lateRead
					lateRow = c
				} else {
					row, rowLive = c, live(c)
				}
				for _, name := range contextFields(t) {
					if crossed, known := crossedIn(c, name); known {
						into[name] = crossed
					}
				}
				return &components.Text{}, nil
			},
			// Builds BEFORE the <ItemsView> — document order — so the
			// sentinel is in the page's map before any row is realized.
			// The fixture declares no <Frozen>, so the map may not exist
			// yet; a row that reads the page's sinks would see this key.
			"PageProbe": func(e Element, c *Context) (gooey.Component, error) {
				if c.arms.sinks == nil {
					c.arms.sinks = map[*prop.Property[string]]string{}
				}
				c.arms.sinks[armsSentinel] = "page"
				pageLive = live(c)
				return &components.Text{}, nil
			},
		},
		declared: map[string]any{"PageDecl": nil},
	}
	// The ancestry is normally pushed by control(); there is no control
	// here, so it is set directly — the question is whether the ROW
	// keeps it, not how it got onto the page.
	page.controls = []string{"page.gooey"}

	// crossedIn is that switch, so it can be asked the same question
	// twice — once while the row is building and once after Load — and
	// the two answers compared. known is false for a field it does not
	// read, which the caller reports as the contract's other half being
	// unchecked.
	crossedIn = func(row *Context, name string) (crossed bool, known bool) {
		switch name {
		case "Styles":
			_, crossed = row.Styles["s"]
		case "Components":
			_, crossed = row.Components["Probe"]
		case "Elements":
			_, crossed = row.Elements["Meter"]
		case "Handlers":
			_, crossed = row.Handlers["H"]
		case "Rules":
			_, crossed = row.Rules["Zonk"]
		case "Declared":
			_, crossed = row.Declared[declSentinel]
		case "Includes":
			// THE FS ITSELF, asked a question only the page's answers.
			// `!= nil` says "there is an FS here", not "it is the
			// page's" — the weak form this file's control switch spent
			// eight arms replacing, kept in the row switch by oversight.
			// Raised in review of #490.
			if row.Includes != nil {
				_, err := fs.ReadFile(row.Includes, "card.gooey")
				crossed = err == nil
			}
		case "Dispatcher":
			// NON-NIL AND IDENTICAL. The identity is the claim — one UI
			// goroutine, one dispatcher — but identity alone is satisfied
			// by two nils, which is the state the page was in.
			crossed = row.Dispatcher != nil && row.Dispatcher == page.Dispatcher
		case "Dir":
			crossed = row.Dir == page.Dir
		case "Variant":
			crossed = row.Variant == page.Variant
		case "rowDepth":
			// INCREMENTED, which is neither inherited nor reset — so a
			// negative alone would be satisfied by a reset to zero, and
			// the increment is asserted on its own line rather than
			// left to it.
			if row.rowDepth != page.rowDepth+1 {
				t.Errorf("a template row's rowDepth is %d, want %d — crossing a "+
					"template seam is exactly what this counter counts, and a "+
					"row that does not advance it makes MaxTemplateDepth "+
					"unreachable", row.rowDepth, page.rowDepth+1)
			}
			crossed = row.rowDepth == page.rowDepth
		case "catalogNoIncludes":
			// RESET, and non-nil is the whole of what crossing would
			// mean here: the row answering vocabulary questions from an
			// assembly made before the seam. It is derived state rather
			// than a registration — see rowPartition's entry — so there
			// is no page-side sentinel to look for, only its absence.
			crossed = row.catalogNoIncludes != nil
		case "controls":
			// RESET, so the claim is that the row does NOT carry the
			// page's ancestry. See rowPartition's entry and
			// MaxTemplateDepth.
			crossed = len(row.controls) > 0 &&
				row.controls[len(row.controls)-1] == "page.gooey"
		case "res":
			crossed = row.res.cur != nil
		case "fsys":
			// The DOCUMENT's FS, asked a question only it answers —
			// behind a nil check, because dropping the propagation leaves
			// a nil interface and fs.ReadFile PANICS on one, taking the
			// package's run with it instead of reporting. Same trap the
			// Includes arm above carries.
			if row.fsys != nil {
				_, err := fs.ReadFile(row.fsys, "page.gooey")
				crossed = err == nil
			}
		case "Values":
			// The row's Values are the ITEM, so the page's own key must
			// not be visible through them.
			_, crossed = row.Values["PageOnly"]
		case "Named":
			_, crossed = row.Named["PageOnly"]
		case "arms":
			// Not "is it set" — the row builds its own — but whether the
			// page's map IS the row's. The sentinel is the whole answer:
			// it was written into the page's live sink map by PageProbe,
			// so a row sharing that map sees it and a row with its own
			// does not. Two empty maps cannot fake agreement here, which
			// is what the old membership comparison allowed.
			_, crossed = row.arms.sinks[armsSentinel]
		case "ns":
			_, crossed = row.ns["probe"]
		case "declared":
			_, crossed = row.declared["PageDecl"]
		default:
			return false, false
		}
		return crossed, true
	}

	root, err := Load(pageFS, "page.gooey", page)
	if err != nil {
		t.Fatalf("the page did not load, so nothing below was observed: %v", err)
	}
	loaded = true
	// THE SCROLL-TIME ROW. Without it every answer below is about a
	// context built while the page build was still installed, and the
	// two idioms the row literal mixes — fields CAPTURED when the
	// factory is built and fields READ inside it — are indistinguishable
	// here. A field added in the read-live idiom that Load later starts
	// restoring is green at load time and nil at scroll time, which is
	// exactly the defect this round fixed for fsys. Raised in review of
	// #490.
	list, _ := page.Named["List"].(*components.ItemsView)
	if list == nil {
		t.Fatal("the <ItemsView> is not in the page's Named map, so no row can " +
			"be realized after Load and the comparison below checks nothing")
	}
	c := gooey.NewComposer(root, 40, 10)
	t.Cleanup(c.Close)
	c.Frame()
	items.Set([]string{"a", "b", "c"})
	c.Frame()
	if err := list.Err(); err != nil {
		t.Fatalf("a row realized after Load returned did not build: %v", err)
	}
	if lateRow == nil {
		t.Fatal("the composer realized no template row after Load returned, so " +
			"every answer below is about the load-time probe row alone — the " +
			"one context whose fields are read while the page build is still " +
			"installed")
	}
	if row == nil {
		t.Fatal("the probe builder never ran, so no row context was reached. " +
			"ItemsView.Validate realizes one throwaway row during the build; " +
			"if that stopped happening this test sees nothing")
	}

	// THE HAZARD, ASSERTED. The arms arm below reads a sentinel rather
	// than comparing row.arms against page.arms, and the comment there
	// says why; this is that reason in a form that can go red. If a
	// restored field ever survives the build, an arm comparing it
	// against the page becomes legitimate and this test should be the
	// thing that says so — and if one stops surviving on the ROW, the
	// switch's read-after-Load answer for it is no longer what the row
	// had while it was live, and every arm reading it is lying.
	for _, f := range []struct {
		name        string
		live, after bool
	}{
		{"ns", pageLive.ns, page.ns != nil},
		{"arms", pageLive.arms, page.arms.sinks != nil},
		{"fsys", pageLive.fsys, page.fsys != nil},
		{"res", pageLive.res, page.res.cur != nil},
	} {
		if !f.live {
			t.Errorf("the page had no %s DURING its own build, so this fixture "+
				"cannot measure whether the build restores it — and the arm "+
				"below that avoids comparing against page.%s rests on that",
				f.name, f.name)
			continue
		}
		if f.after {
			t.Errorf("Context.%s survived the page's build. An arm may now compare "+
				"row.%s against page.%s; while it did not, such an arm compared "+
				"nil to nil and passed whatever itemsview.go did — which is how "+
				"the arms arm came to be written as a sentinel", f.name, f.name, f.name)
		}
	}
	for _, f := range []struct {
		name        string
		when, after bool
	}{
		{"ns", rowLive.ns, row.ns != nil},
		{"arms", rowLive.arms, row.arms.sinks != nil},
		{"fsys", rowLive.fsys, row.fsys != nil},
		{"res", rowLive.res, row.res.cur != nil},
	} {
		if f.when != f.after {
			t.Errorf("the row had Context.%s=%v while it was building and %v after "+
				"Load returned, so the switch below reads the restored value, not "+
				"the one the row was given. Capture it in the Probe builder instead",
				f.name, f.when, f.after)
		}
	}

	// EVERY FIELD, driven by contextFields — the same walk the control
	// seam uses, so Context growing a field is red at BOTH seams. The
	// six-entry literal this replaces could not have been red for fsys
	// or declared, which is how they came to be row-scoped by omission.
	for _, name := range contextFields(t) {
		rule, ok := rowPartition[name]
		if !ok {
			t.Errorf("Context.%s is not in rowPartition, so nothing says "+
				"whether an item-template row inherits it. That omission is "+
				"how Elements and Variant came to be missing from the row "+
				"context, and fsys and declared came to be scoped to it with "+
				"no reason written anywhere", name)
			continue
		}
		crossed, known := liveRead[name]
		if !known {
			t.Errorf("Context.%s is partitioned for a row but crossedIn does "+
				"not read it, so its half of the contract is unchecked", name)
			continue
		}
		// THE LIVE ANSWER IS THE ONE CHECKED, and a disagreement with
		// the after-Load read is reported rather than silently resolved:
		// it means this field is restored on the row the way ns, arms,
		// fsys and res are on the page, and every assertion taken
		// afterwards is about the restore rather than about the row.
		if after, _ := crossedIn(row, name); after != crossed {
			t.Errorf("Context.%s read %v while the row was building and %v after "+
				"Load returned. Something restores it on the row, so an assertion "+
				"taken afterwards is about the restore rather than about the row",
				name, crossed, after)
		}
		// THE TWO ROWS MUST AGREE. The load-time probe row and a row the
		// composer realized afterwards are built by the same factory and
		// are the same claim; a field that crosses for one and not the
		// other is the capture-versus-read-inside-the-factory defect,
		// which no assertion about a single row can see.
		if late, known := lateRead[name]; !known {
			t.Errorf("Context.%s was not read on a row realized after Load, so "+
				"the scroll-time half of the contract is unchecked", name)
		} else if late != crossed {
			t.Errorf("Context.%s reached the load-time probe row (%v) and the "+
				"scroll-time row (%v) differently. itemsview.go must CAPTURE the "+
				"page's value beside pagePending rather than read it inside the "+
				"row factory: Load restores ns, arms, fsys and res in its defers, "+
				"so a row built when the user scrolls sees whatever was put back",
				name, crossed, late)
		}
		if crossed != rule.inherit {
			verb := "did not reach an item-template row"
			if crossed {
				verb = "reached an item-template row and must not have"
			}
			t.Errorf("Context.%s %s — %s", name, verb, rule.why)
		}
	}
}

// TestADeclaredElementWorksInsideARow is the symptom, and it is the one
// a user reports.
//
// The test above reads the row's context, which is the strong form. This
// is the weak form kept deliberately: <Meter> is registered in Elements
// and nowhere else, so a row that cannot see Elements answers `unknown
// element <Meter>` — the same message #314 was filed for, from the seam
// that PR did not reach. An error-message probe alone would be a worse
// test; beside the context read it is what ties the contract to the
// complaint. Raised in review of #490.
func TestADeclaredElementWorksInsideARow(t *testing.T) {
	ctx := &Context{
		Elements: map[string]*ElementDef{"Meter": meterDef()},
		Values: map[string]any{
			"Items": components.Items(prop.NewSource([]string{"a"}),
				func(s string) map[string]any { return map[string]any{"N": 1} }),
		},
	}
	src := `<Gooey xmlns="wonderforge.io/gooey/2026">` +
		`<ItemsView Items="{{.Items}}">` +
		`<ItemsView.ItemTemplate><Meter Level="{{.N}}"/></ItemsView.ItemTemplate>` +
		`</ItemsView></Gooey>`
	if _, err := Build([]byte(src), ctx); err != nil {
		t.Fatalf("a DECLARED element was not usable inside an item template, "+
			"while the same component registered under the undeclared "+
			"Components spelling is: %v", err)
	}
}

// TestASelfSupplyingItemSourceIsALoadError is the sharp half of the row
// seam, and the difference between a load error and a fatal.
//
// A control whose item template instantiates it, fed a projection that
// hands down an item source at every depth, never stops instantiating.
// Nothing in the ancestry says so — a row is a legitimate re-entry — so
// what catches it is the row-seam counter, MaxTemplateDepth. Both
// directions measured on this fixture:
//
//	with the bound:    markup: control loop.gooey: <ItemsView.ItemTemplate>:
//	                   nested item templates too deep
//	without it:        fatal error: stack overflow
//
// The second is not merely worse, it is unreportable: a Go fatal is not
// a panic, so nothing recovers it and Screen.Restore never runs — the
// terminal is left in raw mode with the alternate screen up. That is the
// whole reason a load-time refusal exists here.
//
// THIS TEST USED TO REQUIRE "includes itself", and that was the round
// that inherited `controls` across the row seam. It caught this fixture
// and also refused the terminating tree-view shape, which
// TestATerminatingRecursiveTemplateLoads is the other half of. The
// message is what changed; the guarantee is the same one. Raised in
// review of #490, twice.
func TestASelfSupplyingItemSourceIsALoadError(t *testing.T) {
	ctlFS := fstest.MapFS{
		"loop.gooey": {Data: []byte(`<Gooey xmlns="wonderforge.io/gooey/2026">` +
			`<ItemsView Items="{{.Items}}">` +
			`<ItemsView.ItemTemplate><Loop Items="{{.Items}}"/></ItemsView.ItemTemplate>` +
			`</ItemsView></Gooey>`)},
	}
	// SELF-SUPPLYING, and it has to be. A projection that stops handing
	// down an item source ends the recursion for a reason that has
	// nothing to do with the ancestry — the build fails at depth two with
	// `"Items" not found in context` and the test agrees with the bug.
	var proj func(string) map[string]any
	proj = func(string) map[string]any {
		return map[string]any{"Items": components.Items(prop.NewSource([]string{"a"}), proj)}
	}
	ctx := &Context{
		Includes: ctlFS,
		Values:   map[string]any{"Items": components.Items(prop.NewSource([]string{"a"}), proj)},
	}

	_, err := Build([]byte(`<Gooey xmlns="wonderforge.io/gooey/2026">`+
		`<Loop Items="{{.Items}}"/></Gooey>`), ctx)
	if err == nil {
		t.Fatal("a control whose item template instantiates itself built cleanly")
	}
	if !strings.Contains(err.Error(), "nested item templates too deep") {
		t.Errorf("a control whose item template instantiates itself failed for "+
			"some other reason than the depth bound, so this test is not "+
			"reaching it: %v", err)
	}
	// THE MESSAGE NAMES THE SEAM ONCE. The refusal is raised at the
	// innermost of MaxTemplateDepth nested templates and passes back out
	// through every one of them, so before errTemplateTooDeep gave it an
	// identity the author read sixty-four copies of
	// "markup: <ItemsView.ItemTemplate>: " before the first useful word.
	if n := strings.Count(err.Error(), "<ItemsView.ItemTemplate>"); n != 1 {
		t.Errorf("the refusal names <ItemsView.ItemTemplate> %d times, want 1 — "+
			"every enclosing template re-wrapped it:\n%v", n, err)
	}
}

// The shape the round that inherited `controls` across the row seam
// refused, and the reason identity is the wrong instrument here.
//
// A tree view is node.gooey: a label, and an <ItemsView> over the node's
// children whose template instantiates <Node/>. It is recursive and it
// TERMINATES, because ItemsView.Validate realizes a probe row only for a
// non-empty collection — so the load descends exactly as far as the data
// goes. With the ancestry inherited it failed at depth two with
//
//	markup: control node.gooey includes itself: node.gooey → node.gooey
//	— a control cannot be its own ancestor, because instantiating it
//	never terminates
//
// whose final clause is false at this seam. Raised in review of #490.
func TestATerminatingRecursiveTemplateLoads(t *testing.T) {
	ctlFS := fstest.MapFS{
		"node.gooey": {Data: []byte(`<Gooey xmlns="wonderforge.io/gooey/2026">` +
			`<VStack><Text>{{.Label}}</Text>` +
			`<ItemsView Items="{{.Kids}}">` +
			`<ItemsView.ItemTemplate><Node Label="{{.Label}}" Kids="{{.Kids}}"/></ItemsView.ItemTemplate>` +
			`</ItemsView></VStack></Gooey>`)},
	}
	// TWO LEVELS AND A LEAF, so the recursion is real and finite: the
	// probe row for the root's one child is itself a <Node/>, whose own
	// <ItemsView> has an empty collection and realizes nothing.
	leaf := components.ItemsOf([]string{}, func(string) map[string]any { return nil })
	kids := components.ItemsOf([]string{"leaf"}, func(string) map[string]any {
		return map[string]any{
			"Label": prop.NewSource("leaf"),
			"Kids":  prop.NewSource(leaf),
		}
	})
	ctx := &Context{
		Includes: ctlFS,
		Values: map[string]any{
			"Label": prop.NewSource("root"),
			"Kids":  prop.NewSource(kids),
		},
	}
	if _, err := Build([]byte(`<Gooey xmlns="wonderforge.io/gooey/2026">`+
		`<Node Label="{{.Label}}" Kids="{{.Kids}}"/></Gooey>`), ctx); err != nil {
		t.Fatalf("a finite tree view failed to load: %v", err)
	}
}

// TestAPageRelativeAssetPathWorksInsideARow is the symptom, and it is
// the one an author reports.
//
// fsys is what Context.assets resolves a literal path against, falling
// back to Includes when it is nil. With the row context leaving it nil,
// <Image Src="logo.png"> inside an <ItemsView.ItemTemplate> failed with
// "no file system to load from — this tree was built from bytes; use
// markup.Load", which is advice the author had already taken, while the
// identical element one line outside the template loaded. <MenuItem
// Icon> and <FileWatcher Paths> read the same seam.
//
// BOTH ARMS, because the template arm alone would pass against a fixture
// whose asset is simply unreadable everywhere. The page arm is what says
// the FS and the file are fine and the SEAM is the difference. Raised in
// review of #490.
func TestAPageRelativeAssetPathWorksInsideARow(t *testing.T) {
	var png bytes.Buffer
	if err := gopng.Encode(&png, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatalf("building the fixture image: %v", err)
	}
	const el = `<Image Src="logo.png" Cols="2" Rows="1"/>`
	fsys := fstest.MapFS{
		"logo.png": {Data: png.Bytes()},
		"page.gooey": {Data: []byte(`<Gooey xmlns="wonderforge.io/gooey/2026">` +
			el + `</Gooey>`)},
		"row.gooey": {Data: []byte(`<Gooey xmlns="wonderforge.io/gooey/2026">` +
			`<ItemsView Name="List" Items="{{.Items}}"><ItemsView.ItemTemplate>` +
			`<VStack><Probe/>` + el + `</VStack>` +
			`</ItemsView.ItemTemplate></ItemsView></Gooey>`)},
	}
	src := prop.NewSource([]string{"a"})
	var realized int
	ctx := &Context{
		Named: map[string]gooey.Component{},
		Values: map[string]any{
			"Items": components.Items(src,
				func(string) map[string]any { return map[string]any{} }),
		},
		Components: map[string]Builder{
			// Beside the <Image> rather than instead of it: a row that
			// fails on the Image still builds this first, so it counts
			// the realizations that HAPPENED, which is what keeps the
			// Err() check below from passing over an empty list.
			"Probe": func(Element, *Context) (gooey.Component, error) {
				realized++
				return &components.Text{}, nil
			},
		},
	}
	if _, err := Load(fsys, "page.gooey", &Context{Values: ctx.Values}); err != nil {
		t.Fatalf("the same element failed at PAGE level, so this fixture cannot "+
			"tell the seam from a broken asset: %v", err)
	}
	root, err := Load(fsys, "row.gooey", ctx)
	if err != nil {
		t.Fatalf("a page-relative asset path did not resolve inside an item "+
			"template, while the identical element at page level did: %v\n"+
			"The row context is built in markup/itemsview.go and must carry "+
			"the document's fsys — a row's markup came from the same "+
			"document the <ItemsView> did", err)
	}

	// AND THEN A ROW NOBODY REALIZED AT LOAD TIME, which is the arm that
	// matters and the one this test did not have.
	//
	// Load succeeding proves only that ItemsView.Validate's throwaway
	// probe row built, and that row is realized DURING the page build,
	// while ctx.fsys is still installed. Every row a user scrolls to is
	// realized by the composer after Load returned and its defer put
	// ctx.fsys back — so with the FS read inside the factory rather than
	// captured, the arm above passed against the bug. Raised in review of
	// #490.
	list, _ := ctx.Named["List"].(*components.ItemsView)
	if list == nil {
		t.Fatal("the <ItemsView> is not in the page's Named map, so the rows " +
			"below cannot be asked whether they built")
	}
	c := gooey.NewComposer(root, 40, 10)
	t.Cleanup(c.Close)
	c.Frame()
	src.Set([]string{"a", "b", "c"})
	c.Frame()
	if realized < 2 {
		t.Fatalf("only %d template rows were realized, and one of those is the "+
			"load-time probe: the composer never built a row after Load "+
			"returned, so nothing here measures the seam", realized)
	}
	if err := list.Err(); err != nil {
		t.Errorf("a row realized AFTER Load returned could not resolve a "+
			"page-relative asset path: %v\n"+
			"itemsview.go must CAPTURE ctx.fsys beside pagePending rather than "+
			"read it inside the factory — Load restores it in a defer, so a "+
			"row built at scroll time sees nil", err)
	}
}

// TestAControlCannotShadowAPageDeclaredElement is the behaviour change
// that came free with inheriting Elements, stated rather than
// discovered.
//
// Before this branch a control's context could not see the page's
// Elements, so a setup registering Components["Meter"] privately, on a
// page that declares Elements["Meter"], simply won: the two names lived
// in different scopes. Now Elements crosses the boundary, and
// markup.buildComponent refuses a name present in BOTH maps
// because one of them would be unreachable and which one would depend on
// the order those ifs happen to be written in.
//
// That refusal is the intended answer — the alternative is a control
// silently shadowing a declared element, which is the vocabulary problem
// #314 exists to remove — but it is a document that used to load and now
// does not, and the error names a collision the control author did not
// create. So: pinned here, and the way out is written in the Elements
// arm's comment in usercontrol.go. Raised in review of #490.
func TestAControlCannotShadowAPageDeclaredElement(t *testing.T) {
	ctlFS := fstest.MapFS{
		"card.gooey": {Data: []byte(
			`<Gooey xmlns="wonderforge.io/gooey/2026"><Meter/></Gooey>`)},
	}
	page := &Context{
		Elements: map[string]*ElementDef{"Meter": meterDef()},
		Components: map[string]Builder{
			"Card": UserControl(ctlFS, "card.gooey",
				func(e Element, parent *Context) (*Context, error) {
					// The control's OWN idea of <Meter>, private to it.
					return &Context{Components: map[string]Builder{
						"Meter": func(Element, *Context) (gooey.Component, error) {
							return &components.Text{}, nil
						},
					}}, nil
				}),
		},
	}
	_, err := Build([]byte(
		`<Gooey xmlns="wonderforge.io/gooey/2026"><Card/></Gooey>`), page)
	if err == nil {
		t.Fatal("a control registered its own <Meter> builder on a page that " +
			"DECLARES <Meter>, and the document loaded. One of the two is " +
			"unreachable, and which one would depend on the order of the ifs " +
			"in markup.buildComponent — that is the silent shadowing the declared " +
			"vocabulary exists to prevent")
	}
	if !strings.Contains(err.Error(), "registered in both") {
		t.Errorf("the load failed for some other reason than the both-maps "+
			"collision, so this test is not reaching the seam it is about: %v", err)
	}
	// AND IT NAMES THE CONTROL. buildComponent's message is written for one
	// author holding both maps; here they are two, and neither wrote a
	// duplicate. Without the control's name the person who can act on it —
	// whoever wrote card.gooey's setup — is handed a sentence about a page
	// they may not own. Pinning only the refusal pins that the load fails,
	// not that the report is usable. Raised in review of #490.
	if !strings.Contains(err.Error(), "card.gooey") {
		t.Errorf("the collision is reported as %q — it names neither the control "+
			"nor its file, so it reads as a page-level duplicate that nobody wrote", err)
	}
}

// TestThePartitionSourceReaderSeesWhatItClaimsTo is the fixture corpus
// for partitionTablesIn and the helpers under it, and it exists because
// the live corpus cannot be one.
//
// This package declares two partition tables and both are spelled the
// same way: `var x = map[string]struct{…}{"K": {true, "why"}}`. So four
// branches never execute against the tree — the keyed `why` lookup, the
// `partition` type alias in isPartitionLiteral, typeOfSpec's declared-
// type arm, and every unreadable-key path including the unreadKeys
// report. Loosening any of them leaves the suite green, which is the
// state a fixture test is for.
// `TestTheZeroedTopMatcherSeesOnlyAReleasedSlot` makes the same
// argument for the clear-to-cap matcher one module over — backticked
// because it lives in the ROOT module and is cited from here, so a
// rename by somebody who never opens markup/ would otherwise orphan
// this justification with nothing red.
//
// It also pins the distinction the misdiagnosis rounds were about: an
// entry whose why is legitimately EMPTY reads as `{why: "", read:
// true}`, and one this reader cannot parse reads as `read: false`.
// Raised in review of #543.
func TestThePartitionSourceReaderSeesWhatItClaimsTo(t *testing.T) {
	const src = `package markup

type partition = map[string]struct {
	inherit bool
	why     string
}

var positional = map[string]struct {
	inherit bool
	why     string
}{
	"A": {true, "plain"},
	"B": {false, "a " + "joined " + "chain"},
}

var keyed partition = partition{
	"C": {inherit: true, why: "by key"},
	"D": {inherit: true},
	"G": {},
}

var unreadable = map[string]struct {
	inherit bool
	why     string
}{
	constKey: {true, "the key is a const"},
	"E":      {true, someConst},
}

// A DECLARED TYPE AND NO INITIALIZER, which is the only shape that
// reaches typeOfSpec's vs.Type arm alone: with an initializer the
// composite literal carries the type too, so the arm can be deleted
// and every other fixture still resolves.
var declaredNoInit partition

var notATable = map[string]int{"F": 1}
`
	file, err := parser.ParseFile(gotoken.NewFileSet(), "zzfixture.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("the fixture does not parse: %v", err)
	}
	got := partitionTablesIn(file)

	if _, ok := got["notATable"]; ok {
		t.Errorf("a map[string]int was read as a partition table — "+
			"isPartitionLiteral matches on the VALUE type, and a table this "+
			"walk invents is a name partitionTables can never satisfy: %v",
			slices.Sorted(maps.Keys(got)))
	}
	if src, ok := got["declaredNoInit"]; !ok || len(src.entries) != 0 || src.readable {
		t.Errorf("a var with a declared partition type and no initializer read "+
			"as %v (found=%v, readable=%v), want an empty table marked "+
			"UNREADABLE — this is the only shape that reaches typeOfSpec's "+
			"declared-type arm, because a composite literal carries the type "+
			"as well, and an empty table that does not say so registers as "+
			"checked against nothing", src.entries, ok, src.readable)
	}
	if src, ok := got["positional"]; !ok || !src.readable {
		t.Errorf("a table declared as a composite literal read as "+
			"readable=%v (found=%v), want true — without this arm the verdict "+
			"above passes for a reader that calls every table unreadable",
			src.readable, ok)
	}
	for _, name := range []string{"positional", "keyed", "unreadable"} {
		if _, ok := got[name]; !ok {
			t.Fatalf("%s was not found, so every assertion below it is "+
				"vacuous. Found: %v", name, slices.Sorted(maps.Keys(got)))
		}
	}
	// `keyed` is declared with the ALIAS and with a declared type, which
	// is two branches no live table reaches at once.
	for _, tc := range []struct {
		table, key string
		why        string
		read       bool
	}{
		{"positional", "A", "plain", true},
		{"positional", "B", "a joined chain", true},
		{"keyed", "C", "by key", true},
		// THE ONE THAT WAS CALLED UNREADABLE. A keyed literal omitting
		// the zero-valued field is legitimate and its why is "".
		{"keyed", "D", "", true},
		// AND THE EMPTY LITERAL, which reaches the same answer from the
		// other side: no keys at all, so the keyed arm above cannot see
		// it and the positional arity test used to call the struct's
		// own zero value unreadable.
		{"keyed", "G", "", true},
		{"unreadable", "E", "", false},
	} {
		e, ok := got[tc.table].entries[tc.key]
		if !ok {
			t.Errorf("%s has no entry for %q — the reader dropped it, which is "+
				"how an entry comes to be compared as an empty why",
				tc.table, tc.key)
			continue
		}
		if e.why != tc.why || e.read != tc.read {
			t.Errorf("%s[%q] read as (%q, %v), want (%q, %v). An entry that is "+
				"legitimately empty and one this reader cannot parse are "+
				"different answers, and the pairing comparison has only one "+
				"verdict for them", tc.table, tc.key, e.why, e.read, tc.why, tc.read)
		}
	}
	if n := got["unreadable"].unreadKeys; n != 1 {
		t.Errorf("unreadKeys is %d, want 1: the const-spelled key cannot be "+
			"recorded under a key, so counting it is the only way the table "+
			"says it is partly checked rather than clean", n)
	}
	if n := got["positional"].unreadKeys; n != 0 {
		t.Errorf("unreadKeys is %d for a table whose keys are all string "+
			"literals, want 0 — every table would report as partly checked", n)
	}
}

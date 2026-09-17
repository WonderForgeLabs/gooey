package mcp

// The tool INVENTORY guards: every place this repo writes down the list
// of MCP tools in prose, checked against the list the server actually
// publishes.
//
// They are here rather than beside the tool that occasioned them —
// screen_size is what made every one of these inventories stale at once
// — because which tool prompted a guard is a fact about its history and
// not about what it checks. None of these mentions screen_size; they read
// markdown and a workflow blob, not a session. Raised in review of #504.
//
// What they have in common is the failure mode, which is the reason to
// keep them together: a prose list of tools goes stale SILENTLY. Adding
// a tool breaks nothing a compiler or a schema can see, and the page goes
// on describing a server that no longer exists. So each of these guards
// derives the expected set from the server and compares, and none of them
// may skip itself on a condition it cannot distinguish from the thing it
// is looking for — see pageOrSkip.

import (
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestTheTutorialsToolInventoryIsComplete is the class, not the
// instance. The tutorial lists every tool by name in prose, and prose
// enumerating a set is the thing this repo keeps being wrong about — the
// inventory said "no tool reports terminal size" for as long as that was
// true and would have gone on saying it, because nothing reads the list
// but a human.
//
// DERIVED FROM v1Tools, so a tool added tomorrow fails here rather than
// leaving a tutorial that is quietly missing one. The reverse direction
// is deliberately NOT asserted: the page is free to mention a name that
// is not a tool.
//
// It SKIPS rather than fails when the page is out of reach, because
// `mcp` is its own module: the zip a proxy serves contains mcp/ and
// nothing above it, so `../docs` does not exist for anyone consuming the
// module standalone. A guard that cannot run there must say so rather
// than report the repo's docs as broken on someone else's machine.
//
// THE QUESTION IS ASKED ONCE, UP FRONT, and not derived from the read —
// see pageOrSkip. Skipping on fs.ErrNotExist covered "this module is
// being consumed standalone" and "somebody renamed the page and nothing
// follows it any more" with the same green, which is the defect
// TestTheAgentWorkflowsToolInventoriesAreComplete had already fixed for
// the workflow blobs. A guard whose whole premise is that a prose
// inventory goes stale silently cannot disable itself on a rename.
// Raised in review of #504.
func TestTheTutorialsToolInventoryIsComplete(t *testing.T) {
	const page = "../docs/learn/08-remote-control.md"
	body := pageOrSkip(t, page)
	// THE INVENTORY PARAGRAPH, not the page. This handed over the whole
	// tutorial until review of #504, which is the page-wide vacuous pass
	// TestTheGRPCContractTableNamesEveryTool declines by name below: the
	// paragraph after this one explains `screen_size`, so deleting the
	// name from the inventory left the guard green on a mention that is
	// not a list. An inventory is a list, and the assertion has to be
	// made against the list.
	para, ok := paragraphWith(string(body), inventoryLead)
	if !ok {
		t.Fatalf("%s no longer carries a %q paragraph, so either the tutorial "+
			"was restructured and this guard has to follow it, or the inventory "+
			"is gone", page, inventoryLead)
	}
	assertNamesEveryTool(t, para, page+"'s inventory paragraph")
}

// paragraphWith returns the blank-line-delimited paragraph containing
// marker. A prose inventory is a sentence, not a line — the tutorial's
// wraps over four — so a line-wise read of it would see one comma-cut
// fragment and call the rest undocumented.
func paragraphWith(body, marker string) (string, bool) {
	for _, para := range strings.Split(body, "\n\n") {
		if strings.Contains(para, marker) {
			return para, true
		}
	}
	return "", false
}

// declaresTool reports whether body carries an ENTRY for name — a list
// item or a bold lead-in that introduces the tool — rather than a
// sentence that happens to mention it. The lead is everything before the
// entry's em dash, which is the shape every inventory in this repo's
// records uses:
//
//   - `screen_size` — the visible surface in cells
//   - `send_keys` / `send_mouse` — inject input.Events
//     **`validate_markup(source)`** — swap_markup's parse-and-bind
//
// The name has to be backtick-opened and may not run into another
// identifier character, which is what keeps `register_properties` from
// matching the `unregister_properties` beside it and lets
// `validate_markup(source)` match all the same.
//
// THE LINE ALSO HAS TO LOOK LIKE AN ENTRY, and it did not until the
// counterfactual below said so: an em dash is ordinary punctuation in
// this repo's prose, so "Prose about `gamma` — which is a mention"
// scored as an entry for gamma. An entry OPENS with its subject — a
// list marker or a bold lead-in — and a sentence about something else
// does not. NOT "or the backticked name itself", which this said until
// review of #504: prose wraps, so a continuation line can open with one,
// and one on the guarded page did. See the prefix test for the
// measurement.
func declaresTool(body, name string) bool {
	for _, line := range strings.Split(body, "\n") {
		l := strings.TrimSpace(line)
		// A BARE BACKTICK IS NOT AN ENTRY SHAPE, and dropping it is not
		// tightening for its own sake — it closes a page-wide vacuous
		// pass that was live on the page this guards. Prose WRAPS, and a
		// continuation line can land a backticked tool name in column 0:
		// docs/specs/2026-08-10-mcp-server.md carries
		//
		//	`set_value` — the #112 ceiling-lift follow-up remains its own deliberate
		//
		// as the tail of the register_properties paragraph. That scored
		// as set_value's entry, so deleting set_value's REAL entry left
		// TestTheMCPSpecsToolInventoryIsComplete green — measured, by
		// deleting it. Any tool could acquire that at any reflow, with
		// nothing said, which is the class this whole file exists to
		// close. Every real entry on that page opens with a list marker
		// or a bold lead-in, and all of them still resolve without this
		// prefix. Raised in review of #504.
		if !strings.HasPrefix(l, "- ") && !strings.HasPrefix(l, "* ") &&
			!strings.HasPrefix(l, "**`") {
			continue
		}
		head, _, ok := strings.Cut(l, " — ")
		if !ok {
			continue
		}
		// EVERY OCCURRENCE IN THE LEAD, not the first. This took
		// strings.Index once and moved to the next LINE when the byte
		// after it was an identifier byte — so an entry leading with a
		// longer name that contains this one ("- `screen_size_v2` /
		// `screen_size` — …") reported screen_size as undocumented,
		// while the two-names-per-entry shape the record actually uses
		// ("- `send_keys` / `send_mouse` — …") is the very reason this
		// function reads the whole lead. One false match ended the line.
		// Raised in review of #504.
		for rest := head; ; {
			i := strings.Index(rest, "`"+name)
			if i < 0 {
				break
			}
			rest = rest[i+len(name)+1:]
			if rest == "" || !isIdentByte(rest[0]) {
				return true
			}
		}
	}
	return false
}

// inventoryLead is how the tutorial opens its enumeration. Written once
// because the guard and its own counterfactual both need it.
const inventoryLead = "The tool inventory:"

// toolTableHeader is the #112 table's header row. A markdown table is a
// blank-line-delimited block, so paragraphWith slices exactly this one.
const toolTableHeader = "| MCP tool | args | RPC | notes |"

// tableToolRe reads the tool name out of a table row's first column.
//
// THE SAME NAME SHAPE isIdentByte USES, which is the point rather than
// a coincidence: this PR widened that one to uppercase and digits on the
// ground that "a rule that is correct only for the names that happen to
// exist is the kind that stops being correct silently", and left this
// copy of the same rule at [a-z_]. A tool named screen_size2 would
// capture screen_size, hit a digit where the closing backtick was
// demanded, and be dropped from the column — so the guard would report a
// missing row for a table that has one. It fails red rather than green,
// which makes it a false alarm rather than a hole, and it is still the
// class the PR just closed one function over. Raised in review of #504.
var tableToolRe = regexp.MustCompile("^\\|\\s*`([A-Za-z0-9_]+)`\\s*\\|")

// isIdentByte is what separates `screen_size` from a longer name that
// merely starts with it. UPPERCASE INCLUDED: it was a-z, 0-9 and
// underscore, so `screen_sizeV2` would have read as an entry for
// screen_size. Tool names are lower_snake today, which is what made the
// gap invisible — and a rule that is correct only for the names that
// happen to exist is the kind that stops being correct silently. Raised
// in review of #504.
//
// ONE BLOCK, and the three declarations above are outside it. Moving
// them here for discoverability spliced them into the MIDDLE of this
// paragraph, so its first half documented inventoryLead, its sentence
// ran into inventoryLead's own ("…which is what made the inventoryLead
// is how the tutorial opens its enumeration"), and this function was
// left with a fragment opening "gap invisible —". Godoc attaches a
// comment group to the declaration that immediately follows it, which
// is the same defect islandGoneFmt's own comment (control/snapshot.go)
// records fixing on this very branch — cited by NAME rather than by
// line, because a line number in prose is the other thing that rots
// silently. Raised in review of #504.
func isIdentByte(b byte) bool {
	return b == '_' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') ||
		(b >= '0' && b <= '9')
}

// TestTheInventoryReadsAreNarrowerThanThePage is the counterfactual for
// the two narrowings above, and it is the assertion neither guard can
// make about itself: a slice that quietly returned the whole document,
// or an entry test that matched any mention, would leave both of them
// exactly as vacuous as they were — and green.
func TestTheInventoryReadsAreNarrowerThanThePage(t *testing.T) {
	const page = "# Doc\n\n" + inventoryLead + " `alpha`, `beta`.\n\n" +
		"`gamma` is explained down here, outside the list.\n"

	para, ok := paragraphWith(page, inventoryLead)
	if !ok {
		t.Fatal("paragraphWith did not find the inventory it was pointed at")
	}
	if !namesTool(para, "beta") {
		t.Error("the inventory paragraph lost a name that is in it")
	}
	if namesTool(para, "gamma") {
		t.Error("the inventory paragraph reaches a name from the NEXT paragraph, " +
			"so deleting a tool from the list would still read as documented")
	}

	// THE WRAPPED LINE IS THE SECOND MENTION SHAPE, and it is the one
	// that was live rather than hypothetical: a paragraph reflowed so a
	// backticked name lands in column 0 reads as an entry under any rule
	// that accepts a bare backtick. docs/specs/2026-08-10-mcp-server.md
	// had exactly that for set_value, and deleting set_value's real
	// entry left TestTheMCPSpecsToolInventoryIsComplete green. Mid-line
	// alone could not see it. Raised in review of #504.
	const record = "- `alpha` — the first one\n" +
		"- `send_keys` / `send_mouse` — two on one entry\n" +
		"**`delta(source)`** — a bold lead-in with an argument\n" +
		"Prose about `gamma` — which is a mention and not an entry.\n" +
		"`zeta` — the tail of a paragraph that happened to wrap\n" +
		"- `unregister_epsilon` — the inverse\n"
	for _, name := range []string{"alpha", "send_mouse", "delta"} {
		if !declaresTool(record, name) {
			t.Errorf("declaresTool missed the entry for %s, so a record that "+
				"documents a tool would read as omitting it", name)
		}
	}
	if declaresTool(record, "gamma") {
		t.Error("a sentence mentioning `gamma` counts as an entry, which is the " +
			"page-wide vacuous pass this shape exists to close")
	}
	if declaresTool(record, "zeta") {
		t.Error("a WRAPPED prose line opening with a backticked name counts as " +
			"an entry. That is not hypothetical: it was live for set_value on " +
			"docs/specs/2026-08-10-mcp-server.md, and it let that tool's real " +
			"entry be deleted with this guard still green")
	}
	if declaresTool(record, "epsilon") {
		t.Error("`unregister_epsilon` counts as an entry for epsilon — the same " +
			"substring pass namesTool's backticks close one surface over")
	}

	// A LONGER NAME FIRST IN THE SAME LEAD. This read as undocumented
	// while declaresTool took only the first backtick match on a line and
	// moved on, which is the shape the two-names-per-entry rows in the
	// real record would hit the moment a `screen_size_v2` joined them.
	const shadowed = "- `alpha_v2` / `alpha` — the longer name leads\n"
	if !declaresTool(shadowed, "alpha") {
		t.Error("an entry whose lead opens with a LONGER name containing this " +
			"one reads as no entry at all, so a tool listed second on its own " +
			"row would be reported as undocumented")
	}
	if declaresTool(shadowed, "alpha_v3") {
		t.Error("declaresTool matched a name the entry does not carry")
	}
	// The uppercase half of the same boundary.
	if declaresTool("- `alphaV2` — a camel sibling\n", "alpha") {
		t.Error("`alphaV2` counts as an entry for alpha: the identifier test " +
			"stops at lowercase, so a camel-cased sibling reads as the name it " +
			"merely starts with")
	}

	// AND THE TABLE READ HOLDS THE SAME NAME SHAPE. toolColumn is the
	// fourth reader in this file and the only one with its own character
	// class, which sat at [a-z_] while isIdentByte above was widened —
	// so a row for screen_size2 was invisible to the guard and reported
	// as a missing row. Raised in review of #504.
	rows := toolColumn("| `alpha` | one |\n| `screen_size2` | two |\n| `sendKeys` | three |\n")
	for _, name := range []string{"alpha", "screen_size2", "sendKeys"} {
		if !rows[name] {
			t.Errorf("toolColumn dropped the row for %s, so a table that names "+
				"every tool would be reported as missing one: %v", name, rows)
		}
	}

	// AND THE TABLE READ IS NARROWED TO ONE TABLE. The contract spec
	// carries two, and reading both folded the kinds column into the
	// coverage map — so a tool sharing a name with a kind would read as
	// documented with no row of its own.
	const twoTables = "# Doc\n\n| kind | proto |\n|---|---|\n| `image` | `bytes` |\n\n" +
		toolTableHeader + "\n|---|---|---|---|\n| `alpha` | — | none | |\n"
	narrow, ok := tableRows(twoTables)
	if !ok {
		t.Fatal("tableRows did not find the table it is pointed at")
	}
	if !narrow["alpha"] {
		t.Errorf("the narrowed read lost a row that is in the table: %v", narrow)
	}
	if narrow["image"] {
		t.Error("a name from ANOTHER table on the page counts as a documented " +
			"tool, so deleting a tool's own row would still read as covered")
	}
}

// TestTheMCPSpecsToolInventoryIsComplete is the fourth surface, and the
// one this change edited by hand while closing the other three.
//
// docs/specs/2026-08-10-mcp-server.md is the decision record — its
// "Tools (v1)" section is the same shape as the tutorial's, backticked
// names in prose read only by humans. Adding screen_size to it and not
// guarding it means the next tool lands with three surfaces reddening
// and the record quietly wrong, which is the exact outcome the tutorial
// guard exists to prevent.
//
// ADDITIVE, NOT A FIX: this passed as written before it was committed.
// Saying so matters because a guard added beside a repair reads as the
// repair's pin, and this one pins nothing that was broken. Raised in
// review of #504.
//
// Skips when the file is absent, for the module-boundary reason
// TestTheTutorialsToolInventoryIsComplete gives.
func TestTheMCPSpecsToolInventoryIsComplete(t *testing.T) {
	const page = "../docs/specs/2026-08-10-mcp-server.md"
	body := pageOrSkip(t, page)
	// AN ENTRY, not a mention — and a SECTION SLICE IS THE WRONG
	// NARROWING HERE, which is a measurement and not a preference.
	// "## Tools (v1)" is the ORIGINAL set and this record extends
	// itself in dated sections: list_styles, validate_markup,
	// register_properties and unregister_properties are each
	// introduced in one of them. Slicing to the v1 section reported
	// four tools as undocumented in a record that documents all four,
	// and the repair would have been to backdate them into a decision
	// that did not include them. So the page stays the subject and the
	// SHAPE does the narrowing: a tool has to be the subject of an
	// entry, which a sentence in "Extended 2026-08-10" mentioning it
	// is not. Raised in review of #504.
	s := &Server{}
	tools := s.v1Tools()
	if len(tools) == 0 {
		t.Fatal("v1Tools is empty, so this guard would pass vacuously")
	}
	for _, tl := range tools {
		if !declaresTool(string(body), tl.Name) {
			t.Errorf("%s mentions %s nowhere as an ENTRY of its own — no list item "+
				"and no bold lead-in introducing it. The record is where somebody "+
				"asks what the tool surface IS; a tool that only appears inside a "+
				"sentence about another one is not in the inventory", page, tl.Name)
		}
	}
}

// TestTheGRPCContractTableNamesEveryTool is the third surface, and the
// one that states its own completeness out loud.
//
// docs/specs/2026-08-10-grpc-contract.md's "#112 table" opens "Every v1
// MCP tool, argument-for-argument" and closes with the rule that any new
// tool must name the RPC it fronts. screen_size had no row — so the
// reader most in need of it, somebody implementing the rest of #112
// looking for which tools still lack an RPC, would find the ONE tool
// that lacks one missing from the list of tools. A doc that asserts
// completeness more loudly than the tutorial did went stale in exactly
// the way this change exists to stop. Raised in review of #504.
//
// It reads the tool column of THAT table, not of the whole page: this
// file names tools in prose elsewhere, so a page-wide Contains would be
// satisfied by a mention outside the table — and the page carries a
// second table, the TypedValue kinds one, whose own first column would
// otherwise be folded into the same coverage map. Skips when the file is
// absent, for the module-boundary reason
// TestTheTutorialsToolInventoryIsComplete gives.
func TestTheGRPCContractTableNamesEveryTool(t *testing.T) {
	const page = "../docs/specs/2026-08-10-grpc-contract.md"
	body := pageOrSkip(t, page)
	rows, ok := tableRows(string(body))
	if !ok {
		t.Fatalf("%s no longer carries a %q header, so either the #112 table was "+
			"restructured and this guard has to follow it, or the table is gone",
			page, toolTableHeader)
	}
	if len(rows) == 0 {
		t.Fatalf("found no tool rows in %s's #112 table, so this guard would pass "+
			"vacuously — the row shape changed and toolColumn no longer recognizes "+
			"it", page)
	}
	s := &Server{}
	for _, tl := range s.v1Tools() {
		if !rows[tl.Name] {
			t.Errorf("%s claims \"Every v1 MCP tool\" and has no row for %s. A tool "+
				"with no RPC behind it yet still needs the row — that is what somebody "+
				"implementing #112 reads the table to find", page, tl.Name)
		}
	}
}

// tableRows narrows body to the #112 table and reads its tool column.
// The narrowing and the read are one function because they are one
// claim — a tool is documented when THAT table carries a row for it —
// and because a call site that narrowed for itself could stop and no
// counterfactual would see it.
func tableRows(body string) (map[string]bool, bool) {
	table, ok := paragraphWith(body, toolTableHeader)
	if !ok {
		return nil, false
	}
	return toolColumn(table), true
}

// toolColumn returns the backticked name in the first cell of every
// markdown table row in body. Hand it ONE table: it cannot tell which
// table a row came from, and every name it finds counts as covered, so a
// whole-page read of the contract spec folds the TypedValue kinds table's
// `string`, `int`, `bool`, `image` … into the coverage map, where a tool
// that ever shares one of those names reads as documented with no row of
// its own. Adding names to a coverage map is how it passes over an
// absence, not why it cannot. Raised in review of #504.
func toolColumn(body string) map[string]bool {
	out := map[string]bool{}
	for _, line := range strings.Split(body, "\n") {
		m := tableToolRe.FindStringSubmatch(line)
		if m != nil {
			out[m[1]] = true
		}
	}
	return out
}

// TestTheServerInstructionsNameEveryTool is the same guard one surface
// closer to the client.
//
// `instructions` is a hand-written prose enumeration shipped to every MCP
// client, and an agent reads it BEFORE the tutorial. It omitted
// screen_size for as long as nothing checked it — the identical failure
// TestTheTutorialsToolInventoryIsComplete exists to end, in the string
// that reaches clients first. Unlike the tutorial this lives in the
// module, so it never skips.
func TestTheServerInstructionsNameEveryTool(t *testing.T) {
	assertNamesEveryTool(t, instructions, "the server instructions string")
}

// assertNamesEveryTool derives the expectation from v1Tools, which is
// what keeps both callers from becoming lists of their own.
//
// IT ASKS FOR THE NAME IN BACKTICKS, and that is the finding rather than
// a style preference. `strings.Contains` was the first spelling and it
// passed vacuously for two of the names then registered: `register_properties`
// is a substring of the `unregister_properties` that sits beside it in
// both surfaces, and `focus` is an ordinary English word that appears in
// prose about focus whether or not a tool has that name. So a guard
// written to end prose inventories going stale was itself checking all
// but two — the same shape of defect, one level up.
//
// NO DENOMINATOR, deliberately. The measurement is historical and worth
// keeping; the total it was taken against is a sample, in the file whose
// whole subject is that a count in prose goes stale silently, and
// CLAUDE.md's Verify section refuses one for the same reason. Every
// guard here derives its set from v1Tools(). Raised in review of #504.
//
// A non-identifier BOUNDARY closes the first half and not the second:
// "…, send_mouse and focus act on it" delimits the word exactly as a
// tool name would be delimited. The backtick is the mark that means "this
// is a name and not a word", the tutorial already used it on every one of
// them, and mcp/transport.go now does too. TestTheToolNameMatchIsDelimited
// pins both halves. Raised in review of #504.
func assertNamesEveryTool(t *testing.T, body, what string) {
	t.Helper()
	s := &Server{}
	tools := s.v1Tools()
	if len(tools) == 0 {
		t.Fatal("v1Tools is empty, so this guard would pass vacuously")
	}
	for _, tl := range tools {
		if !namesTool(body, tl.Name) {
			t.Errorf("%s never names %s in backticks. The inventory is what a reader "+
				"uses to find a tool, and a tool missing from it does not exist as far "+
				"as they are concerned. Backticks because an unmarked name cannot be "+
				"told from the prose around it — see assertNamesEveryTool", what, tl.Name)
		}
	}
}

func namesTool(body, name string) bool {
	return strings.Contains(body, "`"+name+"`")
}

// TestTheToolNameMatchIsDelimited measures the two vacuous passes rather
// than asserting they are gone, because both are still a `strings.Contains`
// away.
func TestTheToolNameMatchIsDelimited(t *testing.T) {
	const nested = "`unregister_properties` removes names again"
	if namesTool(nested, "register_properties") {
		t.Error("a body naming only unregister_properties reports register_properties " +
			"as documented, which is the substring pass this match exists to close")
	}
	if !namesTool("`register_properties` grows the bindable state", "register_properties") {
		t.Error("the marked form is not recognized, so the guard asks for something " +
			"nobody can write")
	}
	if namesTool("keys go to whatever has focus at the time", "focus") {
		t.Error("ordinary prose about focus satisfies the focus tool's entry, which is " +
			"the word-not-name pass this match exists to close")
	}
	if !namesTool("send_mouse and `focus` act on it", "focus") {
		t.Error("the marked form of a name that is also an English word is not " +
			"recognized")
	}
}

// TestTheAgentWorkflowsToolInventoriesAreComplete is the fifth and sixth
// surfaces, and they are the ones whose readers drive the app.
//
// .claude/workflows/gooey-new-component.js and gooey-new-demo.js each
// carry a "The tools:" line inside the prompt they hand an agent that
// then prototypes a component over MCP — the audience with the most
// direct use for the screen's size, and the one least able to discover a
// tool the list omits, because the list IS its documentation. Both
// omitted screen_size, and both had omitted unregister_properties since
// before this change.
//
// IT READS THE COMMITTED BLOB, not the working tree, and that is the
// whole reason this still lives in the mcp module. These two files are
// outside it and are rewritten by tooling: the review of #504 ran in a
// checkout where both had been restored to origin/main's content, and
// this test failed with thirty errors about a repo that was fine. The
// fs.ErrNotExist skip could not help — the file was present, just not
// ours. What the guard is actually about is whether the REPOSITORY's
// inventory is complete, so HEAD's copy is the honest subject and a
// dirty worktree is none of its business.
//
// Moving it to the root module, where .claude/ is in-tree, was the other
// option and is worse: v1Tools() lives here, and a root-module copy would
// have to enumerate the tools by hand — which is the defect all six of
// these surfaces exist to prevent.
//
// THE BACKTICKS ARE ESCAPED IN THE SOURCE, because the inventory sits
// inside a JavaScript template literal: an unescaped ` would end the
// string. \` is what the file holds and a backtick is what the agent
// reads, so this unescapes before asking assertNamesEveryTool — the
// alternative, a second matching rule, would let these two surfaces drift
// apart from the other four.
func TestTheAgentWorkflowsToolInventoriesAreComplete(t *testing.T) {
	pages := []string{
		".claude/workflows/gooey-new-component.js",
		".claude/workflows/gooey-new-demo.js",
	}
	// THE CHECKOUT QUESTION IS ASKED ONCE, AND OUT HERE. It is the only
	// reason a blob may be missing without anybody having done anything
	// wrong — the zip a proxy serves contains mcp/ and nothing above it
	// — and asking it per file made the two answers indistinguishable:
	// a bare continue covered "this module is being consumed
	// standalone" and "somebody renamed a workflow and nothing follows
	// it any more" with the same green. Raised in review of #504, which
	// is the second time this loop has lost a page quietly.
	if !inRepoCheckout(t) {
		t.Skipf("HEAD is not readable from here, so this guard only runs inside "+
			"the repo checkout (%v)", pages)
	}
	for _, page := range pages {
		src, ok := committedBlob(t, page)
		if !ok {
			t.Errorf("%s is not in HEAD, and git can read HEAD here — so it was "+
				"renamed, moved or deleted. The inventory it carries is what an "+
				"agent driving this server reads, and a surface the guard cannot "+
				"find is a surface nobody is checking: follow the file, or delete "+
				"the entry deliberately", page)
			continue
		}
		line, found := toolsLine(src)
		if !found {
			t.Errorf("%s no longer carries a \"The tools:\" line, so either the "+
				"prompt was restructured and this guard has to follow it, or the "+
				"inventory is gone", page)
			continue
		}
		// THE LINE, not the page. assertNamesEveryTool used to be handed
		// the whole file, which is the page-wide vacuous pass
		// TestTheGRPCContractTableNamesEveryTool declines by name two
		// tests up: these prompts mention tool names elsewhere, so a
		// name could be "documented" by a sentence outside the
		// inventory. Raised in review of #504.
		// NAMED AS WHAT WAS ACTUALLY READ. The label said
		// ".claude/workflows/…", which is the path in the worktree, and
		// this test deliberately does not read that file — so a
		// developer with the fix already applied was told their own
		// open editor was missing a tool. Raised in review of #504.
		//
		// AND WHEN HEAD IS BEHIND THE WORKTREE, SAY SO. Reading the
		// committed blob is right — see the head of this comment — but
		// it has a cost the failure message has to carry: a developer
		// who has ALREADY written the missing tool into the file and
		// not committed it gets a red suite describing a repository
		// they have fixed, with no way to tell that from a real gap.
		// Asking the worktree only in the failure path keeps HEAD the
		// subject and turns "you are missing a tool" into "commit what
		// you have". Raised in review of #504.
		if missing := toolsMissingFrom(t, line); len(missing) > 0 {
			what := "HEAD:" + page + "'s \"The tools:\" line"
			if wt, err := os.ReadFile(filepath.Join("..", page)); err == nil {
				// THE SAME UNESCAPE committedBlob does, and forgetting
				// it made this branch unreachable: the inventory lives
				// inside a JavaScript template literal, so the file
				// holds \` where the agent reads a backtick, and
				// namesTool matches on backticks. Measured — without
				// this the worktree copy reads as missing every tool and
				// the fallback never fires, which is the same kind of
				// silent pass the guard above declines by name.
				wtSrc := strings.ReplaceAll(string(wt), "\\`", "`")
				if wtLine, ok := toolsLine(wtSrc); ok &&
					len(toolsMissingFrom(t, wtLine)) == 0 {
					t.Errorf("%s never names %s in backticks — but your WORKING TREE "+
						"copy of %s names them all. Nothing is wrong with the file "+
						"in front of you; this guard reads the committed blob, "+
						"because a checkout where tooling has restored these pages "+
						"to origin/main's content is not evidence about the "+
						"repository. Commit the change", what,
						strings.Join(missing, ", "), page)
					continue
				}
			}
			assertNamesEveryTool(t, line, what)
		}
	}
}

// toolsMissingFrom is assertNamesEveryTool's question without the
// assertion: which registered tools this body does not name. The two
// share namesTool, so a change to what "names" means cannot make them
// disagree.
func toolsMissingFrom(t *testing.T, body string) []string {
	t.Helper()
	s := &Server{}
	tools := s.v1Tools()
	if len(tools) == 0 {
		t.Fatal("v1Tools is empty, so this guard would pass vacuously")
	}
	var missing []string
	for _, tl := range tools {
		if !namesTool(body, tl.Name) {
			missing = append(missing, tl.Name)
		}
	}
	return missing
}

// inRepoCheckout reports whether git can read HEAD from the parent
// directory. It separates "this module was consumed standalone" — the
// one blameless reason a committed blob is unreadable — from a path
// that is simply no longer there.
// THE ROOT, NOT MERELY A REPOSITORY. `git -C .. rev-parse HEAD` succeeds
// for ANY enclosing repository, so a gooey checkout nested inside a
// larger one answered yes — and committedBlob's `show HEAD:<path>`
// resolves paths from that outer repo's root, where `.claude/workflows/…`
// does not exist. The guard then reported the workflows as renamed or
// deleted in a tree where they are present and correct. Comparing the
// toplevel against `..` itself is what asks the question the caller
// means. Raised in review of #504.
func inRepoCheckout(t *testing.T) bool {
	t.Helper()
	out, err := exec.Command("git", "-C", "..", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return false
	}
	top, err := filepath.EvalSymlinks(strings.TrimSpace(string(out)))
	if err != nil {
		return false
	}
	// Abs BEFORE EvalSymlinks. EvalSymlinks does not absolutise: handed
	// ".." it returns ".." unchanged, which never equals a toplevel and
	// silently skipped this guard everywhere — the first version of this
	// fix did exactly that, and the test reported itself skipped in a
	// checkout where it should have run.
	abs, err := filepath.Abs("..")
	if err != nil {
		return false
	}
	parent, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return false
	}
	return top == parent
}

// committedBlob reads one repo-root-relative path out of HEAD. It returns
// false — rather than failing — when git cannot answer, which covers the
// module being consumed standalone (the zip a proxy serves contains mcp/
// and nothing above it) as well as a path not yet committed.
func committedBlob(t *testing.T, path string) (string, bool) {
	t.Helper()
	out, err := exec.Command("git", "-C", "..", "show", "HEAD:"+path).Output()
	if err != nil {
		return "", false
	}
	return strings.ReplaceAll(string(out), "\\`", "`"), true
}

// toolsLine is the inventory itself: the one line beginning the "The
// tools:" enumeration. Everything else in these prompts is prose that
// happens to mention tools.
func toolsLine(src string) (string, bool) {
	for _, line := range strings.Split(src, "\n") {
		if strings.Contains(line, "The tools:") {
			return line, true
		}
	}
	return "", false
}

// pageOrSkip reads a page that lives ABOVE this module, skipping when
// the module is being consumed standalone and failing when the page is
// simply not where the guard says it is.
//
// The two are different answers and os.ReadFile gives them the same
// error, so something OUTSIDE the page's own path has to separate them.
// `../go.work` is that anchor: consumed standalone the module is `mcp/`
// and whatever is above it is not this repo, so the workspace file is
// absent and there is nothing to compare against; with it present this
// is a checkout, and a missing page is a rename the guard has to follow.
//
// NOT THE PAGE'S OWN DIRECTORY, which is where this discriminator was
// and which self-disables one path segment later: `filepath.Dir` of
// `../docs/learn/08-remote-control.md` is `../docs/learn`, so renaming
// `docs/learn` made the stat fail, the guard skip, and the rename it
// exists to catch pass as "consumed standalone". A discriminator drawn
// from the thing being checked answers the question with its own
// premise. go.work is the repo's own anchor — CLAUDE.md's workspace rule
// is built on it and TestCLAUDEMDVerifyLoopReachesEveryNestedModule
// would fail loudly if it moved, which is the opposite of quiet.
// Raised in review of #504.
//
// NOT inRepoCheckout, which asks a git question these three guards do
// not have. It returns false when git is absent, when git refuses
// ("detected dubious ownership"), or when `..` is not a repo root — so a
// `git archive`, a Download ZIP, or a Docker build whose context omits
// `.git` carries all three pages, readable, and skipped all three guards
// green. That is the shape of the fs.ErrNotExist skip this branch
// already removed for the same tests: a guard whose premise is that a
// prose inventory goes stale silently cannot disable itself. The split
// is between reading a file that is right there and reading a COMMIT —
// TestTheAgentWorkflowsToolInventoriesAreComplete does the second and
// keeps inRepoCheckout. Raised in review of #504.
func pageOrSkip(t *testing.T, page string) []byte {
	t.Helper()
	const anchor = "../go.work"
	if _, err := os.Stat(anchor); errors.Is(err, fs.ErrNotExist) {
		t.Skipf("%s is outside this module and %s is not present, so the module "+
			"is being consumed standalone and this guard cannot run here",
			page, anchor)
	} else if err != nil {
		t.Fatalf("stat %s: %v", anchor, err)
	}
	body, err := os.ReadFile(page)
	if errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("%s is missing from the repo checkout — renamed, moved or "+
			"deleted. This guard is the only thing keeping that page's tool "+
			"inventory in step with the server, so it follows the page rather "+
			"than skipping: point it at the new path, or delete it with the page",
			page)
	}
	if err != nil {
		t.Fatalf("reading %s: %v", page, err)
	}
	return body
}

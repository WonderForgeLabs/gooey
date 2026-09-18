package gooey

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"testing"
)

// The version a nested module REQUIRES of core is the one line in this
// repo that nothing in this repo reads.
//
// `go.work` resolves every sibling to the checkout beside it, so the
// require is inert here — CLAUDE.md's "One workspace" section says so
// outright: "`go test` in a nested module is no longer testing that
// module against what it *requires*". CI inherits the same workspace,
// because its legs `cd` into each module inside this checkout. And the
// `replace … => ../..` that nine of these modules carry cannot close the
// gap, because a replace in a DEPENDENCY's go.mod is ignored by whoever
// depends on it; it applies only here, where the workspace has already
// made it redundant.
//
// So the require is read for the first time on a stranger's machine, by
// `go get`. Every nested module said `v0.0.0` — not a tag, not a
// pseudo-version, nothing a proxy can serve:
//
//	go: github.com/WonderForgeLabs/gooey/imagefmt/svg@latest requires
//	    github.com/WonderForgeLabs/gooey@v0.0.0: reading
//	    github.com/WonderForgeLabs/gooey/go.mod at revision v0.0.0:
//	    unknown revision v0.0.0
//
// That made every consumable module in the tree — every one outside
// apps/, which is the property rather than the list this named until
// review of #497 — impossible to `go get`, while every module in it
// stayed green in here. Nobody inside the workspace could reach the
// failure, which is why it survived: the tree is not the environment the
// line is for.
//
// THE apps/* MODULES ARE A DIFFERENT CASE and this comment used to
// claim them: "every apps/* one impossible to `go install`", as though
// fixing the require fixed that too. It does not. `go install pkg@ver`
// refuses any target module whose own go.mod carries `replace`
// directives, and it refuses BEFORE resolving a single require —
// measured against a published commit:
//
//	go install github.com/WonderForgeLabs/gooey/apps/introdeck@e5cdb56…
//	go: … contains one or more replace directives. It must not contain
//	    directives that would cause it to be interpreted differently
//	    than if it were the main module.
//
// Every apps/* go.mod carries them, so those modules stay
// un-installable whatever this guard says — a real pin resolving fine
// and the install failing anyway. What the pin buys is `go get` of the
// LIBRARY modules, where a replace in a dependency's go.mod is ignored
// rather than fatal. Raised in review of #497.
//
// This test reads the require rather than resolving around it, so it is
// the only mechanism in the repo that can see the defect at all. It
// deliberately does NOT check that a version resolves — that needs the
// network, and this suite is meant to pass against the vendor directory
// with the network off. It checks the sentinel that can never resolve
// under any conditions, which is the part that does not need a proxy to
// know.
//
// EVERY MODULE IN THIS TREE, not just core. The guard read `r.Path !=
// core` for its whole life, and the blind spot that left was not
// hypothetical: handlers/temporal required
// packs/temporal-visibility at `v0.0.0`, and nineteen more sibling
// requires across apps/* said the same — one `go get` away from the
// identical "unknown revision v0.0.0", and invisible here because the
// path was not core's. The argument in the paragraphs above never
// depended on WHICH module is required; it depends on the require being
// read outside this workspace, which is true of a sibling exactly as it
// is of core. Raised in review of #497.
func TestNestedModulesRequireResolvableGooeyVersions(t *testing.T) {
	// A module that requires core at all must name a version that could be
	// served. `packs/*` require nothing of core and are simply skipped —
	// and `seen` is counted below, so a walk that stops finding requires
	// is visible rather than passing as "nothing to check". It carried a
	// separate `checked` counter incremented on the line above the
	// append, which is two names for one quantity and one early
	// `continue` away from disagreeing. (The `continue` that argument
	// named is gone too — it had become the last statement of the loop
	// below and reached nothing. The hazard it described is real and the
	// single counter is what removes it; the statement was not the
	// mechanism.) Raised in review of #497.
	//
	// Every own-module require in the tree, so the skew check below can
	// compare them against each other rather than against a constant.
	all, mods := allOwnRequires(t)
	seen := pinsOf(all)
	for _, r := range all {
		if msg := shapeMsg(classifyRequire(r.version), r); msg != "" {
			t.Error(msg)
		}
	}

	if len(seen) == 0 {
		// NAMING THE SET THE NUMBER DESCRIBES, and naming which of the
		// two empties it is. The first half was a round-fifteen repair
		// — it said "nested modules" while len(mods) had come to
		// include the ROOT; the second is emptyPopulationMsg's, one
		// round later.
		t.Fatal(emptyPopulationMsg(len(mods), len(all)))
	}

	// ONE REVISION ACROSS THE TREE, which the shape checks above cannot
	// see and which is the failure they let through.
	//
	// Every one of these paths lives in THIS repository and is published
	// by the same push, so two of them naming different commits is not a
	// version choice, it is skew. And skew is not cosmetic here: core
	// sat three weeks behind its own siblings while every sibling
	// require was current, so `go install .../apps/introdeck@latest`
	// resolved core to a commit predating render.StringWidth and did not
	// build — outside the workspace, which is the only place these lines
	// are read. Inside it, all 25 modules were green. Measured in review
	// of #497, where the bump that introduced the skew passed the shape
	// checks above with nothing to say.
	// CONSISTENCY, NEVER CURRENCY, and the distinction is worth stating
	// because the failure this test narrates can survive it. All the
	// pins naming one commit is what it checks; whether that commit is
	// anywhere near origin/main it cannot ask, since a go.mod cannot
	// name the merge that will contain it. So the moment this lands they
	// name main's parent, and every push after widens the gap uniformly
	// with nothing red — and uniform lag reproduces the symptom exactly:
	// `go get …/mcp@latest` resolves core to a commit predating an API
	// mcp's HEAD compiles against. Round 6's skew was visible only
	// because the siblings happened to be current. Closing that needs a
	// scheduled bump or a distance-from-main check, neither of which is
	// this guard — so it is #515 rather than a caveat with no owner.
	// Raised in review of #497.
	_, newestRev, behind, tagged := skewFrom(seen)
	// REPORTED, NOT CHECKED. A require naming a plain tag is legitimate
	// and is also the one shape this guard cannot compare without
	// resolving it, so the count of what it skipped is the honest
	// output — a silent skip is how a check comes to cover less than its
	// name. Raised in review of #497.
	//
	// AND THE COUNT COMPARED IS PART OF THE REPORT, because "3 skipped"
	// reads very differently beside 36 pins and beside 4. Raised in
	// review of #497.
	if len(tagged) > 0 {
		t.Logf("%d of %d own-module require(s) name a plain tag, which this "+
			"check cannot compare against a commit without the network, so "+
			"they are outside the skew report:\n\t%s",
			len(tagged), len(seen), strings.Join(tagged, "\n\t"))
	}
	// AND A LOG IS NOT A SIGNAL IN CI, AND STDERR IS NOT EITHER — which
	// was worth measuring rather than assuming, because the obvious fix
	// is to write the report to os.Stderr on the grounds that `go test`
	// passes it through. It does not: in package-list mode `go test`
	// buffers a package's whole output and prints only `ok` when it
	// passes, so a bare Fprintf to stderr from a passing test is
	// discarded exactly like t.Logf. Measured on this tree with
	// -count=1, both spellings, both `./` and `.` forms: neither string
	// appears. So the log stays a log, and the middle band — SOME
	// requires tagged, at least one pseudo-version left, comparedNothing
	// false and nothing red — has no signal available to it short of
	// failing, which would be the two-distinct-commits rule that reds a
	// correct tree. That residue is #515's, which already owns "this
	// guard's claim is narrower than it reads". Raised in review of
	// #497 round 8.
	//
	// Combined with skewFrom's
	// early return on fewer than two revisions, a tree whose own-module
	// requires ALL name plain tags passes having compared nothing: the
	// silent skip the paragraph above calls out, arrived at by the
	// mechanism it chose. That state is not hypothetical — revisionOf's
	// comment is written for the day per-subdirectory tags are cut, and
	// one `go get -u` per module after that is enough to reach it.
	// Raised in review of #497.
	//
	// NOTHING RESOLVED, not "fewer than two revisions". This asked for two
	// DISTINCT commits until review of #497, which is the state a correct
	// tree is in: one revision across every pin is the invariant, so the
	// moment a single tag appeared beside it the guard reported a healthy
	// tree as having compared nothing. What the check is actually about is
	// whether any pin reached the comparison at all — a tree whose
	// requires ALL name tags, which skewFrom returns empty-handed for,
	// indistinguishably from a tree with no skew.
	if comparedNothing(seen, tagged) {
		t.Errorf("all %d own-module require(s) name a plain tag, so this check "+
			"compared nothing and passed. Tags cannot be ordered without "+
			"resolving them, which needs the network; pin the requires to "+
			"pseudo-versions, or give this guard a way to resolve a tag before "+
			"it can claim to cover them:\n\t%s",
			len(tagged), strings.Join(tagged, "\n\t"))
	}
	for _, g := range behind {
		// THE REVISION IS THE REMEDY, NOT A VERSION STRING. skewFrom keys
		// on the commit and keeps ONE representative spelling per
		// revision, and in a partly-tagged tree that spelling belongs to
		// whichever path won the tie-break: v0.1.1-0.<stamp>-<rev> sorts
		// above v0.0.0-<stamp>-<rev>, so the tagged path's string can be
		// printed at modules requiring an untagged one — a version that
		// path has never had. The commit is the fact every spelling of
		// it shares, and each require now carries its own string in the
		// list above so the reader can see which is which. Raised in
		// review of #497.
		// TWO REMEDIES, AND WHICH IS RIGHT DEPENDS ON WHO IS READING.
		// This offered one — move the laggards up — which is correct for
		// the defect the guard was written from (core three weeks behind
		// its siblings) and is the expensive answer to the shape it will
		// meet most often once it lands: somebody adds a module, pins it
		// to a current origin/main commit, and every existing require in
		// the tree is suddenly "behind", so the message tells them to
		// rewrite every one of them when pinning the newcomer back
		// restores the invariant in one line. This guard checks
		// CONSISTENCY, not currency — its own doc says so — so the
		// consistency-restoring direction is as valid as the other and
		// the message has to name both. The count in the report is what
		// tells them apart: many behind and one ahead is a newcomer.
		// Raised in review of #497.
		//
		// THE GROUP COUNT GOES WITH IT, because the second remedy is
		// true of exactly one shape. The caller renders ONE message PER
		// GROUP, so at three revisions the reader got two messages each
		// naming its own commit as "what the rest of the tree names" —
		// following either leaves two revisions and this test red — and
		// each said "two revisions" where there were three. Round 9 made
		// the remedy correct per group while the guard can emit several.
		// Raised in review of #497.
		t.Error(skewMsg(g, newestRev, len(behind)+1))
	}
}

// modulesIncludingTheRoot is discoverModules plus the ROOT module, and
// the addition is this guard's comment being made true.
//
// discoverModules filters `d.Name() == "go.mod" && p != "go.mod"` —
// deliberately, because every caller of it is about the NESTED modules
// the `./...` loop skips. This guard's own doc says "EVERY MODULE IN
// THIS TREE", and the root was the one module outside the walk.
//
// Nothing is missed today: the root requires only uniseg, x/image,
// x/term and yaml.v3, none of them own-module. But the root is the one
// module that could acquire a require of a `packs/*` module without
// creating a cycle — `packs/*` require nothing of core — and that
// require would be read outside this workspace exactly like every other
// one here. Raised in review of #497.
func modulesIncludingTheRoot(t *testing.T) []string {
	t.Helper()
	return append([]string{"."}, discoverModules(t)...)
}

// comparedNothing reports whether the skew check above reached no
// comparison at all: every own-module require names a plain tag, which
// skewFrom cannot order without the network.
//
// A PREDICATE RATHER THAN AN INLINE CONDITION, because the test below
// has to pin the caller's actual rule. The version this replaces asked
// for fewer than two DISTINCT COMMITS, which is what a correct tree has
// — so a single tag beside a healthy set of pins reported the tree as
// having compared nothing, and the arm that was meant to catch that
// restated the condition in its own words instead of calling it. Raised
// in review of #497.
func comparedNothing(seen []ownRequire, tagged []string) bool {
	return len(tagged) > 0 && len(seen)-len(tagged) == 0
}

// ownRequire is one require of a module in this repository, by the module
// that names it.
type ownRequire struct{ dir, path, version string }

// shapeMsg is what a require of this shape is wrong about, and "" for
// one that is fine.
//
// RENDER THEN DISPATCH, which is the idiom skewMsg, pinCoverage and
// emptyPopulationMsg already use here — and the reason is measured
// rather than stylistic. The three arms were three `if shape == …`
// blocks in the loop, and disabling any of them, or all three at once,
// left the whole root package GREEN: pinsOf has already taken a
// rejected require out of the skew and existence populations, so with
// its reporter gone nothing downstream notices and a v0.0.0 sentinel
// would sit in the tree in silence. One renderer cannot lose one arm
// without losing all of them, and each shape's own remedy becomes
// assertable from a table.
//
// NOT BECAUSE OF A `continue` IN THE CALLER, which this said and which
// was not true by the time it said it. The caller's loop had a trailing
// `continue` as its last statement, reaching nothing, inside a bare
// block left over from when `seen` was accumulated in it. What makes the
// ordering structural is that pinsOf FILTERS — a rejected require is out
// of the skew and existence populations before they are read — and that
// is what pinsOf's doc calls load-bearing. Both the `continue` and the
// block are gone; a reader deleting dead code should not have to wonder
// whether a doc means they broke something. Raised in review of #497.
// pseudoVersionRoutes is the three ways to get a real pseudo-version,
// shared by every arm whose answer is "go and get one".
//
// SHARED BECAUSE THE SENTINEL'S REMEDY WARNS YOU INTO shapeNotAVersion.
// It says `go mod edit -require` writes LITERALLY what you hand it, so a
// bare short hash stays a bare short hash — and when it does, the shape
// the reader lands on is shapeNotAVersion, whose whole message was one
// sentence with none of the three routes in it. That arm's own `why` in
// TestEveryRequireShapeReachesItsOwnArm used to argue a bare hash "is not
// a shape any remedy can repair in place", which is true and beside the
// point: it is repaired the way the sentinel is, by fetching a real one.
//
// The per-shape assertion still works, because each arm keeps its own
// leading sentence and is distinguished by that. Raised in review of
// #497.
const pseudoVersionRoutes = "Get a real pseudo-version with `GOWORK=off go list -m " +
	"-f '{{.Version}}' %s@$(git rev-parse origin/main)` (needs the " +
	"network; GOWORK=off is load-bearing — inside the workspace the " +
	"committed vendor/ forces -mod=vendor and the query is refused, " +
	"and -mod=mod is not allowed in workspace mode), or copy the " +
	"one the rest of the tree already names, or derive it with no " +
	"network at all: `TZ=UTC git -c core.abbrev=12 log -1 " +
	"--date=format-local:%%Y%%m%%d%%H%%M%%S --format='v0.0.0-%%cd-%%h' " +
	"origin/main` (TZ=UTC is load-bearing, and the form is exact " +
	"only while the module is untagged). " +
	"Keep any `replace` line, which is what makes local development " +
	"use the checkout."

func shapeMsg(shape requireShape, r ownRequire) string {
	dir := r.dir
	switch shape {
	// THE ZERO PSEUDO-VERSION TOO. `go mod edit -require=X@v0.0.0`
	// on a module the workspace replaces writes the canonical
	// spelling `v0.0.0-00010101000000-000000000000`, which is the
	// same unservable revision wearing a pseudo-version's shape —
	// and apps/wysiwyg carried three of them while the plain
	// sentinel elsewhere carried the rest.
	case shapeSentinel:
		return fmt.Sprintf("%s requires %s %s, which is neither a tag nor a "+
			"pseudo-version any proxy can serve: `go get` of this module "+
			"fails with \"unknown revision\" for everybody outside this "+
			"workspace. Point it at a published commit, and note that "+
			"`go mod edit -require` writes LITERALLY what you hand it: a "+
			"bare short hash stays a bare short hash and fails this test "+
			"again. "+pseudoVersionRoutes,
			dir, r.path, r.version, r.path)
	// Not a resolution check (no network here), just the shape: a
	// version the proxy could be asked for at all.
	case shapeNotAVersion:
		return fmt.Sprintf("%s requires %s %q, which is not a version — the "+
			"proxy has nothing to be asked for. This is the shape the "+
			"sentinel's remedy above warns you into, and the way out is the "+
			"same. "+pseudoVersionRoutes,
			dir, r.path, r.version, r.path)
	// AND A PSEUDO-VERSION THAT IS NOT ONE IS NOT A TAG. Without
	// this the failure had no reporter at all: revisionOf wants
	// exactly twelve hex characters, so a short or clipped tail
	// returns ok=false, skewFrom files it under `tagged` — "a
	// require naming a plain tag", legitimate and merely
	// unorderable — and that is reported through a t.Logf this
	// same file measures to be invisible for a passing package.
	// pinPopulations drops it too, so the existence check never
	// asks whether the commit is real. Green, silent, and `go
	// get` cannot resolve it: Go does not recognise a short tail
	// as a pseudo-version, so it asks for a TAG of that name.
	// Measured on this tree, revisionOf("v0.0.0-20260913132232-
	// e5cdb56") = ("", false) with stampOf = "20260913132232".
	// Raised in review of #497.
	case shapeMalformed:
		return fmt.Sprintf("%s requires %s %q, which carries a pseudo-version's "+
			"14-digit stamp and NOT its twelve-hex-character revision — so "+
			"it is a malformed pseudo-version, not a plain tag, and no "+
			"proxy can serve it: Go asks for a tag of that name instead and "+
			"gets \"unknown revision\". Twelve is the whole of it and git's "+
			"default abbreviation is seven, so the remedy this file prints "+
			"elsewhere produces exactly this string with its `-c "+
			"core.abbrev=12` dropped: `TZ=UTC git -c core.abbrev=12 log -1 "+
			"--date=format-local:%%Y%%m%%d%%H%%M%%S --format='v0.0.0-%%cd-%%h' "+
			"origin/main`. A `sed` across the tree that clips one character "+
			"lands here too", dir, r.path, r.version)
	}
	return ""
}

// requireShape is what the three shape gates decide about one require,
// and the reason it is a value rather than three `if`s in the loop.
//
// The gates were each a function with a table — revisionOf, stampOf,
// unservableSentinel, malformedPseudo — and the DISPATCH was not: the
// loop reads the real tree, the real tree holds only correct pins, so
// the whole malformedPseudo block could be deleted from the caller with
// the package still green. Measured in review of #497, round sixteen.
// The ordering was unpinned the same way, and its own comment called it
// load-bearing: a require that reached the skew population before being
// rejected produced two failures with two contradicting remedies.
//
// With the decision in one function the ordering is structural — pinsOf
// and the reporting loop read the same answer — and both are
// table-driven below.
type requireShape int

const (
	shapePin         requireShape = iota // a version a proxy could be asked for
	shapeSentinel                        // v0.0.0, or the canonical zero pseudo-version
	shapeNotAVersion                     // no leading "v"
	shapeMalformed                       // a pseudo-version's stamp without its revision
)

func (s requireShape) String() string {
	switch s {
	case shapePin:
		return "pin"
	case shapeSentinel:
		return "sentinel"
	case shapeNotAVersion:
		return "not-a-version"
	case shapeMalformed:
		return "malformed-pseudo"
	}
	return "unknown"
}

// classifyRequire is the three gates, in the order the loop applied
// them. The order is not arbitrary: unservableSentinel accepts
// `v0.0.0-00010101000000-000000000000`, which also carries a stamp, so
// asking malformedPseudo first would file the sentinel under the wrong
// remedy.
func classifyRequire(version string) requireShape {
	switch {
	case unservableSentinel(version):
		return shapeSentinel
	case !strings.HasPrefix(version, "v"):
		return shapeNotAVersion
	case malformedPseudo(version):
		return shapeMalformed
	}
	return shapePin
}

// pinsOf is the requires that classify as a pin — the population the
// skew and existence checks read, and nothing else.
//
// APPENDED AFTER THE SHAPE CHECKS, not before, which this expresses
// rather than remembers. A sentinel that went into the skew population
// and was then rejected produced two failures with two different
// remedies: the sentinel error, and a skew group naming the same line
// and telling the fixer to match a revision. Raised in review of #497.
func pinsOf(all []ownRequire) []ownRequire {
	var out []ownRequire
	for _, r := range all {
		if classifyRequire(r.version) == shapePin {
			out = append(out, r)
		}
	}
	return out
}

// emptyPopulationMsg says WHY the pin population is empty, and the two
// causes are different facts.
//
// It said "none of the N modules walked were found to require core or a
// module under it" whatever had happened. Once the population was
// gated, a tree whose own-module requires were all rewritten badly — a
// `sed` clipping one character off each revision, which is the hazard
// the malformed gate's own message names — produced 36 accurate errors
// and then that sentence, which states the requires were not found.
// They were found. They were rejected. Raised in review of #497.
func emptyPopulationMsg(mods, found int) string {
	if found == 0 {
		return fmt.Sprintf("none of the %d modules walked — the root and every "+
			"nested module — were found to require %s or a module under it; "+
			"either the requires moved or this test stopped reading them, and "+
			"an empty check is not a passing one", mods, coreModule)
	}
	return fmt.Sprintf("all %d own-module require(s) across the %d modules "+
		"walked were REJECTED by the shape checks above, so the skew and "+
		"existence checks below have nothing to read. This is not a second "+
		"defect: fix the errors already printed and this line goes with "+
		"them. It is here because an empty population and an absent one are "+
		"different facts, and the sentence that stood here reported the "+
		"absent one for both", found, mods)
}

// skewGroup is the requires naming one revision that is not the newest.
// skewMsg is one skew group's failure, rendered rather than formatted at
// the call — so a test can assert what the reader is handed.
//
// THE PARENTHETICAL IS GONE, and that was the last representative
// spelling in this message. It read "the newest in the tree is <rev>
// (<version>)", where the version is version[newestRev]: whichever
// spelling won laterThan's tie-break among the requires AT that commit.
// In a partly-tagged tree that is the tagged path's string —
// v0.1.1-0.<stamp>-<rev> outsorts v0.0.0-<stamp>-<rev> — so a reader
// copying the first version string the message shows them into
// `go mod edit -require` for an untagged module lands on a version core
// has never had, and fails this same test again. That is the failure
// mode the remedy text below was rewritten to prevent, left standing at
// the reference site while round 9 fixed it for the behind groups.
//
// The commit is the fact every spelling shares; the remedy already
// spells BOTH shapes off it. Raised in review of #497.
// revisions is how many distinct commits the tree names, this group's
// included. It decides the SECOND remedy, which is true only at two:
// "pin the newcomer back to what the rest of the tree names" presupposes
// that the rest of the tree names one thing. Raised in review of #497.
func skewMsg(g skewGroup, newestRev string, revisions int) string {
	// The pin-back remedy, and the clause that counts. At three or more
	// there is no single newcomer — every group below has to move up —
	// and saying so is the difference between a remedy a reader can
	// follow and two contradicting each other in the same run.
	//
	// SPELLED, NOT HASHED, on this branch too. It named a bare
	// twelve-character commit while the move-up direction spelled both
	// pseudo-version shapes, which is the exact "`go mod edit -require`
	// writes literally what it is handed: a bare short hash stays a bare
	// short hash and fails this test again" trap the sentinel message
	// above exists to prevent. The `Behind:` list is not a substitute —
	// it carries spellings of g.rev only for the paths in THIS group,
	// and the newcomer being pinned back need not require any of them.
	// Raised in review of #497.
	//
	// THE "EITHER DIRECTION" CLAUSE LIVES HERE, not in the base string.
	// It was unconditional, so at three or more revisions the message
	// withdrew the second direction in one sentence and offered it in
	// the next — a reader taking the last clause at its word goes
	// looking for the direction just retracted. That is the same defect
	// round 10 fixed one clause over: a remedy outliving its condition.
	// Raised in review of #497.
	remedy := fmt.Sprintf(" — or, if the newest is a single module you just "+
		"added or bumped, pin THAT one back to %s, which is what the rest of "+
		"the tree names, in its own spelling of it: v0.1.1-0.<stamp>-%s off a "+
		"tag, v0.0.0-<stamp>-%s off an untagged path. This guard is about one "+
		"revision across the tree, not about which revision, so either "+
		"direction closes it", g.rev, g.rev, g.rev)
	count := "two revisions is"
	if revisions > 2 {
		remedy = fmt.Sprintf(". With %d revisions in the tree there is no single "+
			"newcomer to pin back: this group and every other one below the "+
			"newest have to move up", revisions)
		count = fmt.Sprintf("%d revisions are", revisions)
	}
	return fmt.Sprintf("%d requires name commit %s while the newest in the tree "+
		"is commit %s — one repository, one push, so %s skew "+
		"rather than a choice. Behind:\n\t%s\nEither move them up to commit %s, "+
		"in each module's own spelling of it — a pseudo-version off a tag reads "+
		"v0.1.1-0.<stamp>-%s and one off an untagged path reads "+
		"v0.0.0-<stamp>-%s%s. A module requiring an OLDER core than its "+
		"siblings builds in this workspace and fails for anyone who "+
		"`go get`s it.",
		len(g.at), g.rev, newestRev, count, strings.Join(g.at, "\n\t"),
		newestRev, newestRev, newestRev, remedy)
}

type skewGroup struct {
	// rev is the 12-character COMMIT the group's requires name, and it is
	// what the remedy prints.
	//
	// THERE IS NO version FIELD, and the doc that claimed one named two
	// uses the code had stopped having: skewFrom orders off its own local
	// `version` map before any group is built, and the message prints
	// g.rev — round 13 changed that deliberately, so a reader could not
	// copy a tagged path's spelling into an untagged module's require.
	// The only remaining reader was a test arm, which made the field look
	// covered while nothing depended on it. Raised in review of #497.
	rev string
	at  []string
}

// skewFrom groups the tree's own-module requires by the COMMIT they name
// and returns the newest version seen plus every group behind it.
//
// BY COMMIT, NOT BY VERSION STRING. The invariant is one revision across
// the tree, and two paths can name the same commit with different
// strings the moment anything is tagged: Go monorepo tags are
// per-subdirectory, so `paint/v0.1.0` makes requires of paint read
// v0.1.1-0.20260913132232-e5cdb56ececd while everything else stays
// v0.0.0-20260913132232-e5cdb56ececd. Same commit, different string, and
// a string-keyed check reds on a tree with no skew in it. The repo has
// no tags today, which is the only reason keying on the whole string was
// exact. Raised in review of #497.
//
// THE NEWEST IS THE REFERENCE, NOT THE MOST COMMON. Picking the majority
// read correctly for the defect this guard was written from — 20 siblings
// current, 16 core stale — and is backwards for the shape it will meet
// next: a bump that correctly moves ONE module forward while 35 lag
// reports the correct module as the anomaly and points the fixer at the
// stale revision. It was also decided by ranging a map, so an 18/18 tie
// named a different culprit run to run. A pseudo-version embeds its
// timestamp, so the newest is both correct and deterministic. Raised in
// review of #497.
func skewFrom(seen []ownRequire) (newest, newestRev string, behind []skewGroup, tagged []string) {
	byRev := map[string][]string{}
	version := map[string]string{}
	for _, r := range seen {
		rev, ok := revisionOf(r.version)
		if !ok {
			tagged = append(tagged, r.dir+" → "+r.path+" "+r.version)
			continue
		}
		byRev[rev] = append(byRev[rev], r.dir+" → "+r.path+" "+r.version)
		// The representative string for a revision. Ties are broken the
		// same way the reference is, so the report is stable whichever
		// module the walk met first.
		if was, ok := version[rev]; !ok || laterThan(r.version, was) {
			version[rev] = r.version
		}
	}
	// SORTED BEFORE THE EARLY RETURN, not after it. The sort used to sit
	// at the bottom, below this return — which covers both states a
	// correct tree is in (one revision everywhere, and every require a
	// plain tag), so on exactly the two paths the caller PRINTS tagged
	// the list was unsorted. Raised in review of #497.
	sort.Strings(tagged)
	if len(byRev) < 2 {
		return "", "", nil, tagged
	}
	for rev := range byRev {
		if newestRev == "" || laterThan(version[rev], version[newestRev]) {
			newestRev = rev
		}
	}
	newest = version[newestRev]
	revs := make([]string, 0, len(byRev))
	for rev := range byRev {
		if rev != newestRev {
			revs = append(revs, rev)
		}
	}
	sort.Strings(revs) // a report that reorders itself run to run is hard to read
	for _, rev := range revs {
		behind = append(behind, skewGroup{rev: rev, at: byRev[rev]})
	}
	return newest, newestRev, behind, tagged
}

// revisionOf is the commit a version names — the trailing 12-character
// revision of a pseudo-version — and ok is false for a plain tag, which
// names one only to something that can resolve it.
//
// A TAG IS NOT A REVISION HERE, and treating it as one was wrong in both
// directions. `paint v0.1.0` keyed its own bucket beside the 12-char
// hash of the very commit it points at, so the day the first
// per-subdirectory tag is cut at the tree's head the guard reds on a
// tree nobody has broken and tells 39 requires to move to a version
// core has never had. The converse is the silent half: two modules
// tagged v0.1.0 at DIFFERENT commits collapse into one bucket and real
// skew goes unreported. Resolving a tag needs the network, which this
// suite deliberately does not have — so it says it cannot compare them
// rather than pretending. Raised in review of #497.
func revisionOf(v string) (string, bool) {
	// AND HEX, because length alone is not the shape. v1.2.3-abcdefghijkl
	// is a legitimate prerelease tag whose last dash-part is twelve
	// characters, and it keyed its own bucket as though it were a
	// revision. Raised in review of #497. The test is hasRevisionTail
	// above rather than inline, because THIS FUNCTION ANSWERS FALSE FOR
	// TWO REASONS and malformedPseudo has to tell them apart — re-deriving
	// one of them from stampOf made the other unreachable.
	if !hasRevisionTail(v) {
		return "", false
	}
	i := strings.LastIndex(v, "-")
	// AND HEX IS NOT ENOUGH EITHER, which is the same finding one round
	// on: v1.2.3-abcdef123456 is every bit as legitimate a prerelease tag
	// and IS hex, so shape alone cannot separate the two — measured, it
	// keyed its own bucket, stampOf returned "" for it, and laterThan
	// fell through to the string compare that stampOf's comment below
	// calls the round-6 defect and that laterThan's own comment claims
	// nothing here reaches. What separates them is not the tail: a
	// pseudo-version ALWAYS carries a 14-digit stamp and a tag never
	// does, so asking stampOf is the whole test — and it is what keeps a
	// TAG out of laterThan's backstop. It does not empty the backstop:
	// two pseudo-versions sharing a committer second reach it with both
	// stamps present, which laterThan's own comment now names. Raised in
	// review of #497.
	if stampOf(v) == "" {
		return "", false
	}
	return v[i+1:], true
}

// laterThan orders two versions of THIS repository by the timestamp a
// pseudo-version embeds.
//
// THE STRING FALLBACK IS A BACKSTOP, NOT A PATH. It read "when one of
// them is a plain tag and carries none", which told a reader that tags
// are ordered here — and they are not: skewFrom filters every tag into
// `tagged` before populating `version`, so both arguments always carry a
// stamp. Raised in review of #497.
//
// TWO ROUTES REACH IT, AND THE SECOND NEEDS NO UNPARSED VERSION. This
// said the fallback was reachable only by a pseudo-version this package
// failed to parse — a fourth spelling Go starts writing — and that
// missed the ordinary one: the short-circuit above requires the stamps
// to DIFFER, so two distinct commits sharing a committer second (two
// pushes in the same second, or a landing and the merge that contains
// it) fall through to the string compare with both stamps present and
// equal. What decides then is the trailing revision hash, so skewFrom
// picks whichever hash sorts higher and tells the other group to move
// to it.
//
// The tie is not breakable here: two commits one second apart carry no
// offline evidence of their order, which is the same reason a plain tag
// cannot be ordered. So the fallback stays, the ordering stays total
// and deterministic, and the cost is written down instead of implied —
// on a tie the "newest" is arbitrary but stable, and the skew it reports
// is real either way (the two groups do disagree). Raised in review of
// #497 round 8.
func laterThan(a, b string) bool {
	sa, sb := stampOf(a), stampOf(b)
	if sa != "" && sb != "" && sa != sb {
		return sa > sb
	}
	return a > b
}

// unservableSentinel reports whether a version is one of the two
// spellings of "no published commit at all".
//
// ONE PREDICATE, TWO READERS. The shape guard rejects these, and
// TestEveryOwnModulePinNamesACommitThisRepositoryPublished has to skip
// them — revisionOf accepts `v0.0.0-00010101000000-000000000000`
// (twelve hex characters, a fourteen-digit stamp), so reading the
// requires raw made one sentinel produce the shape error AND "pins
// commit 000000000000, which is not a commit in this repository": two
// failures with two different remedies for one line. That is the defect
// the `APPENDED AFTER THE SHAPE CHECKS` comment records fixing on the
// skew path, arriving again by the other door, and a predicate is what
// stops it arriving by a third. Raised in review of #497.
func unservableSentinel(v string) bool {
	return v == "v0.0.0" || strings.HasPrefix(v, "v0.0.0-00010101000000-")
}

// stampNames reports whether version carries `want` as its
// pseudo-version stamp.
//
// A FUNCTION SO IT CAN BE PINNED, and because the obvious spelling is
// wrong. This was strings.Contains(version, "-"+want+"-"+rev), which is
// true of `v0.0.0-<stamp>-<rev>` and false of both tagged forms — the
// stamp is preceded by a DOT in `v0.1.1-0.<stamp>-<rev>` and in
// `v0.2.0-rc.1.0.<stamp>-<rev>`. revisionOf and stampOf both accept all
// three, so the day the first per-subdirectory tag is cut a CORRECT pin
// would have failed, naming the very stamp it carries. It was also the
// only one of the three readers with no arm of its own, which is how
// the three-spelling table two functions down missed it. Raised in
// review of #497.
func stampNames(version, want string) bool { return stampOf(version) == want }

// stampOf is the 14-digit UTC timestamp inside a pseudo-version, or ""
// for a plain tag.
//
// THREE SPELLINGS, NOT ONE, and this read only the first — which is the
// form the tag-awareness above was written for. Go writes
// `vX.0.0-<stamp>-<rev>` off no tag, `vX.Y.Z-0.<stamp>-<rev>` off a
// release tag, and `vX.Y.Z-pre.0.<stamp>-<rev>` off a prerelease. The
// penultimate dash-part is `20260913132232` in the first and
// `0.20260913132232` in the second, so the length test rejected exactly
// the tagged form: measured, stampOf("v0.1.1-0.20260913132232-…") was
// "". laterThan then fell through to a string compare and named the
// OLDER commit as the reference, which is the round-6 defect back
// again. Taking the tail after the last dot covers all three. Raised in
// review of #497.
func stampOf(v string) string {
	parts := strings.Split(v, "-")
	if len(parts) < 2 {
		return ""
	}
	stamp := parts[len(parts)-2]
	if i := strings.LastIndex(stamp, "."); i >= 0 {
		stamp = stamp[i+1:]
	}
	if len(stamp) != 14 {
		return ""
	}
	for i := 0; i < len(stamp); i++ {
		if stamp[i] < '0' || stamp[i] > '9' {
			return ""
		}
	}
	return stamp
}

// TestTheSkewCheckComparesCommitsAndNamesTheNewest drives skewFrom over
// documents whose answer is known, because the tree it normally reads is
// consistent — a walk that grouped everything into one bucket and a walk
// that compared correctly are the same green there.
//
// Both arms are the review of #497's, and each is a real future rather
// than an invented one: the repo gets its first per-subdirectory tag, and
// somebody bumps one module ahead of the rest.
func TestTheSkewCheckComparesCommitsAndNamesTheNewest(t *testing.T) {
	const (
		old = "v0.0.0-20260822101500-aaaaaaaaaaaa"
		// newer, not `newer`: the builtin was shadowed for the whole of
		// this function. Raised in review of #497.
		newer = "v0.0.0-20260913132232-e5cdb56ececd"
		// The same commit as `newer`, spelled the way a require of a
		// TAGGED module reads it: Go monorepo tags are per-subdirectory,
		// so paint/v0.1.0 changes the base of paint's pseudo-versions
		// and nothing else's.
		tagged = "v0.1.1-0.20260913132232-e5cdb56ececd"
	)

	// ONE COMMIT, TWO STRINGS: no skew. A string-keyed check reported
	// two groups here, on a tree nobody had broken.
	if newest, _, behind, _ := skewFrom([]ownRequire{
		{"apps/introdeck", "github.com/WonderForgeLabs/gooey", newer},
		{"apps/introdeck", "github.com/WonderForgeLabs/gooey/paint", tagged},
	}); len(behind) != 0 {
		t.Errorf("a tagged module and an untagged one at the SAME commit report "+
			"skew (newest %s, behind %v) — the invariant is one revision, and a "+
			"per-subdirectory tag changes the string without changing the commit",
			newest, behind)
	}

	// ONE AHEAD, MANY BEHIND: the one that moved is the reference.
	newest, newestRev, behind, _ := skewFrom([]ownRequire{
		{"mcp", "github.com/WonderForgeLabs/gooey", newer},
		{"grpc", "github.com/WonderForgeLabs/gooey", old},
		{"paint", "github.com/WonderForgeLabs/gooey", old},
		{"apps/introdeck", "github.com/WonderForgeLabs/gooey", old},
	})
	if newest != newer {
		t.Errorf("with one module bumped ahead and three lagging, the reference is "+
			"%s; want the NEWEST (%s). Picking the majority reports the correctly "+
			"bumped module as the anomaly and sends the fixer to the stale "+
			"revision", newest, newer)
	}
	if len(behind) != 1 || len(behind[0].at) != 3 {
		t.Fatalf("behind = %v, want the three laggards in one group", behind)
	}
	if want := old[len(old)-12:]; behind[0].rev != want {
		t.Errorf("the group behind names commit %s, want %s — the COMMIT, which "+
			"is the fact every spelling of it shares and the only thing the "+
			"remedy prints", behind[0].rev, want)
	}
	// THE REVISION COMES BACK TOO, rather than the caller re-deriving it
	// from `newest` with the ok dropped. skewFrom already picked the
	// newest BY revision, so handing back the string and making the
	// caller parse it again was a fact reconstructed from a representative
	// spelling — the same shape as the tagged-spelling bug two arms down.
	// It was correct only because `newest` is always a string revisionOf
	// has already accepted; the day that stops holding, the dropped ok
	// leaves an empty revision and the remedy prints "move them up to
	// commit " naming nothing, in the failure path. Raised in review of
	// #497.
	if want := newer[len(newer)-12:]; newestRev != want {
		t.Errorf("skewFrom reports the newest revision as %q, want %q — the "+
			"remedy is printed from this, so an empty or wrong one is a failure "+
			"message that names no commit", newestRev, want)
	}

	// AND THE MESSAGE ITSELF, which is the half the assertions above
	// cannot reach: they read skewFrom's RETURN, and every representative
	// spelling this guard has printed wrongly was printed from a value
	// that was correct. Round 9 fixed the behind groups by carrying
	// g.rev; the reference site went on printing version[newestRev] for
	// one more round, because nothing rendered the message.
	//
	// The newest group here is MIXED — paint tagged, the rest not — so
	// version[newestRev] is the tagged spelling, a string no laggard's
	// module has ever held. It may not appear. Raised in review of #497.
	mixedNewest, mixedRev, mixedBehind, _ := skewFrom([]ownRequire{
		{"mcp", "github.com/WonderForgeLabs/gooey", newer},
		{"paint", "github.com/WonderForgeLabs/gooey/paint", tagged},
		{"grpc", "github.com/WonderForgeLabs/gooey", old},
		{"apps/introdeck", "github.com/WonderForgeLabs/gooey", old},
	})
	if mixedNewest != tagged {
		t.Fatalf("the newest group's representative spelling is %q, want the "+
			"TAGGED one (%q) — this arm is about a message printing a spelling "+
			"no laggard's module has held, so it needs the tie-break to have "+
			"picked one", mixedNewest, tagged)
	}
	if len(mixedBehind) != 1 {
		t.Fatalf("mixedBehind = %v, want one group", mixedBehind)
	}
	if msg := skewMsg(mixedBehind[0], mixedRev, len(mixedBehind)+1); strings.Contains(msg, tagged) {
		t.Errorf("the skew message names %s, a spelling of the newest commit "+
			"that belongs to whichever path won the tie-break — a reader who "+
			"copies the first version string they are shown into `go mod edit "+
			"-require` for an untagged module lands on a version core has never "+
			"had, and fails this test again:\n\t%s", tagged, msg)
	} else {
		// NON-VACUOUS: the message has to still carry the remedy the
		// assertion above is allowed to remove, or deleting the whole
		// sentence passes.
		for _, want := range []string{
			"commit " + mixedRev,
			"v0.1.1-0.<stamp>-" + mixedRev,
			"v0.0.0-<stamp>-" + mixedRev,
		} {
			if !strings.Contains(msg, want) {
				t.Errorf("the skew message does not contain %q, so the arm above "+
					"passes over a message that names no remedy at all:\n\t%s",
					want, msg)
			}
		}
	}

	// THREE REVISIONS, WHICH IS WHERE THE SECOND REMEDY STOPS BEING
	// TRUE. The caller renders one message PER GROUP, and round 9's
	// "pin THAT one back to <this group's commit>, which is what the
	// rest of the tree names" presupposes the rest of the tree names one
	// thing. At three revisions the reader gets two messages sending
	// them to two different commits, each claiming to be the consensus,
	// and following either leaves the tree at two revisions and this
	// test still red. The clause that counts was wrong in the same
	// breath: "so two revisions is skew" over a tree holding three.
	//
	// Reachable by the shape this guard's own comment says it will meet
	// most often — somebody bumps module A, somebody else later bumps
	// module B. No arm before this one ever RENDERED a message for a
	// multi-group tree: every one asserts len(behind) is 0 or 1. Raised
	// in review of #497.
	const older = "v0.0.0-20260801000000-aaaaaaaaaaaa"
	const middle = "v0.0.0-20260901000000-bbbbbbbbbbbb"
	_, threeRev, threeBehind, _ := skewFrom([]ownRequire{
		{"paint", "github.com/WonderForgeLabs/gooey/paint", older},
		{"grpc", "github.com/WonderForgeLabs/gooey", middle},
		{"mcp", "github.com/WonderForgeLabs/gooey", newer},
	})
	if len(threeBehind) != 2 {
		t.Fatalf("threeBehind = %v, want two groups — this arm is about what the "+
			"reader is handed when the guard emits more than one message",
			threeBehind)
	}
	for _, g := range threeBehind {
		msg := skewMsg(g, threeRev, len(threeBehind)+1)
		if strings.Contains(msg, "pin THAT one back") {
			t.Errorf("with three revisions in the tree the message still offers "+
				"the pin-the-newcomer-back remedy, which names THIS group's "+
				"commit as \"what the rest of the tree names\" while a second "+
				"message names another:\n\t%s", msg)
		}
		if strings.Contains(msg, "two revisions") {
			t.Errorf("the message counts two revisions in a tree holding three:"+
				"\n\t%s", msg)
		}
		// THE CLAUSE THAT OFFERS THE SECOND DIRECTION GOES WITH IT. It
		// sat in the base format string and so was unconditional, which
		// left the message withdrawing the direction in one sentence and
		// recommending it in the next. Neither arm above could see it:
		// they name the pin-back remedy and the count, and this is
		// neither. Raised in review of #497.
		if strings.Contains(msg, "either direction closes it") {
			t.Errorf("with three revisions the message says there is no single "+
				"newcomer to pin back and then says either direction closes it, "+
				"so a reader goes looking for the direction the sentence before "+
				"just withdrew:\n\t%s", msg)
		}
		if !strings.Contains(msg, "3 revisions") {
			t.Errorf("the message does not say how many revisions there are, so "+
				"the arms above pass over one that simply dropped the clause:"+
				"\n\t%s", msg)
		}
		// The FIRST remedy has to survive, or a message with no remedy
		// at all satisfies both arms above.
		if !strings.Contains(msg, "move them up to commit "+threeRev) {
			t.Errorf("the message names no way forward:\n\t%s", msg)
		}
	}
	// AND THE TWO-REVISION MESSAGE KEEPS IT, or this is a deletion
	// wearing a condition's hat.
	twoRev := skewMsg(threeBehind[0], threeRev, 2)
	if !strings.Contains(twoRev, "pin THAT one back") {
		t.Errorf("at two revisions the pin-the-newcomer-back remedy is correct "+
			"and is gone:\n\t%s", twoRev)
	}
	if !strings.Contains(twoRev, "either direction closes it") {
		t.Errorf("at two revisions BOTH directions are available and the clause "+
			"saying so is gone, which is how a conditional becomes a deletion:"+
			"\n\t%s", twoRev)
	}
	// AND IT SAYS HOW MANY, which only the three-revision loop's ABSENCE
	// check covered — so the string could have been changed to anything,
	// including something wrong, with every arm green. A count asserted
	// from one side is not asserted. Raised in review of #497.
	if !strings.Contains(twoRev, "two revisions is") {
		t.Errorf("the two-revision message does not say how many revisions it "+
			"found, so the clause the three-revision arms check for the ABSENCE "+
			"of is pinned from neither side:\n\t%s", twoRev)
	}
	// AND IT SPELLS THE VERSION, which the arm above cannot see because
	// a bare hash and a spelled one both contain "pin THAT one back".
	// Both shapes, because a tree can hold either and the reader has to
	// be able to tell which is theirs. The move-up direction had this
	// from round 9 and the pin-back direction did not, which is the same
	// representative-versus-fact defect one branch over: `go mod edit
	// -require` writes literally what it is handed, so a reader copying
	// the bare hash fails this test again. Raised in review of #497.
	backRev := threeBehind[0].rev
	for _, want := range []string{
		"v0.1.1-0.<stamp>-" + backRev,
		"v0.0.0-<stamp>-" + backRev,
	} {
		if !strings.Contains(twoRev, want) {
			t.Errorf("the pin-back remedy does not spell %q, so it names a bare "+
				"commit the reader cannot hand to `go mod edit -require`. The "+
				"Behind: list is not a substitute — it carries spellings only "+
				"for the paths in THIS group, and the module being pinned back "+
				"need not require any of them:\n\t%s", want, twoRev)
		}
	}

	// THE TAGGED SPELLING AT THE OLDER COMMIT, which is the arm that
	// discriminates. With no stamp laterThan falls back to a string
	// compare, and "v0.1.1-0.…" sorts ABOVE "v0.0.0-…" whatever the
	// dates say — so the tagged module would be named the reference and
	// the whole tree told to move backwards. Putting the tag on the
	// NEWER commit proves nothing: both rules agree there.
	const taggedOld = "v0.1.1-0.20260822101500-aaaaaaaaaaaa"
	if newest, _, behind, _ := skewFrom([]ownRequire{
		{"paint", "github.com/WonderForgeLabs/gooey/paint", taggedOld},
		{"mcp", "github.com/WonderForgeLabs/gooey", newer},
	}); newest != newer || len(behind) != 1 {
		t.Errorf("with a TAGGED module at the OLDER commit the reference is %s "+
			"(behind %v); want %s, the newer commit. A pseudo-version off a tag "+
			"spells its stamp as 0.<stamp>, and a reader that cannot see it "+
			"compares strings, where the tag's major-minor wins regardless of "+
			"date", newest, behind, newer)
	}

	// ONE GROUP, TWO SPELLINGS: the group carries the COMMIT and each
	// require's own string. The representative `version` is whichever
	// spelling sorted highest — taggedOld here, a string the untagged
	// module has never had — so a remedy built from it tells `paint` to
	// move to a version that does not exist for it. rev is the fact both
	// spellings share, and `at` is where the reader sees which module
	// holds which. Raised in review of #497.
	if _, _, behind, _ := skewFrom([]ownRequire{
		{"mcp", "github.com/WonderForgeLabs/gooey", newer},
		{"paint", "github.com/WonderForgeLabs/gooey", old},
		{"grpc", "github.com/WonderForgeLabs/gooey/paint", taggedOld},
	}); len(behind) != 1 || behind[0].rev != "aaaaaaaaaaaa" {
		t.Errorf("two spellings of the older commit report %v; want ONE group "+
			"keyed on aaaaaaaaaaaa. A remedy naming a version string names one "+
			"path's spelling for a group that holds several", behind)
	} else if at := strings.Join(behind[0].at, " | "); !strings.Contains(at, old) ||
		!strings.Contains(at, taggedOld) {
		t.Errorf("the group lists %q; want each require to carry its own "+
			"version string, so the reader can tell %s from %s", at, old, taggedOld)
	}

	// A PLAIN TAG IS REPORTED, NOT GROUPED. It names a commit only to
	// something that can resolve it, which this suite cannot.
	if newest, _, behind, skipped := skewFrom([]ownRequire{
		{"apps/introdeck", "github.com/WonderForgeLabs/gooey", newer},
		{"apps/introdeck", "github.com/WonderForgeLabs/gooey/paint", "v0.1.0"},
	}); len(behind) != 0 || len(skipped) != 1 {
		t.Errorf("a require naming the plain tag v0.1.0 alongside a "+
			"pseudo-version reports newest=%s behind=%v skipped=%v; want no "+
			"skew and one skipped. Keying a tag string as a revision reds on a "+
			"tree with no skew the day the first per-subdirectory tag is cut, "+
			"and hides real skew between two modules tagged alike at different "+
			"commits", newest, behind, skipped)
	}

	// A TREE OF NOTHING BUT TAGS COMPARES NOTHING, and the guard in the
	// caller is what says so out loud. skewFrom returns no skew here and
	// no newest — which is indistinguishable, from the return alone,
	// from a tree with one revision and no problem. byRevisions is the
	// discriminator, and the caller errors on it rather than logging,
	// because CI runs `go test` without -v and discards a passing test's
	// log. Raised in review of #497.
	allTags := []ownRequire{
		{"mcp", "github.com/WonderForgeLabs/gooey", "v0.1.0"},
		{"grpc", "github.com/WonderForgeLabs/gooey", "v0.1.0"},
	}
	if _, _, behind, skipped := skewFrom(allTags); len(behind) != 0 || len(skipped) != 2 {
		t.Errorf("a tree of plain tags reports behind=%v skipped=%v; want no "+
			"skew and both skipped", behind, skipped)
	}
	if _, _, _, skipped := skewFrom(allTags); !comparedNothing(allTags, skipped) {
		t.Error("the compared-nothing guard does not fire on a tree whose every " +
			"require names a plain tag, so that tree passes having checked nothing")
	}
	// AND SORTED ON THIS PATH, which is where it was not. allTags is
	// deliberately given "mcp" before "grpc" — sorted the other way —
	// because the sort used to sit BELOW skewFrom's early return, so the
	// two paths that actually print this list (the caller's log and the
	// compared-nothing error) were the two it never reached. Raised in
	// review of #497.
	if _, _, _, skipped := skewFrom(allTags); !sort.StringsAreSorted(skipped) {
		t.Errorf("the skipped-require report comes back unsorted (%v) for an "+
			"all-tags tree, so it reorders itself run to run in exactly the "+
			"case it exists for", skipped)
	}

	// AND IT MUST NOT FIRE ON A TREE THAT IS MERELY PARTLY TAGGED — the
	// half that went the wrong way. The guard asked for two DISTINCT
	// COMMITS, and one commit across every pin is precisely the invariant
	// this file checks for, so the first per-subdirectory tag would have
	// reddened a tree in exactly the state it is supposed to be in. This
	// arm existed before that round and asserted the same thing about
	// byRevisions rather than about the caller's rule, which is why it
	// was green while the caller was wrong: it restated the condition
	// instead of calling it. Raised in review of #497.
	partly := []ownRequire{allTags[0], {"paint", "github.com/WonderForgeLabs/gooey", newer}}
	if _, _, _, skipped := skewFrom(partly); comparedNothing(partly, skipped) {
		t.Errorf("the compared-nothing guard fires on a tree with one tag and one "+
			"pseudo-version (skipped=%v). One revision across every pin IS the "+
			"invariant here, so this reds a healthy tree the day the first "+
			"per-subdirectory tag is cut", skipped)
	}

	// A PRERELEASE TAG WHOSE TAIL IS TWELVE CHARACTERS IS NOT A
	// REVISION. v1.2.3-abcdefghijkl passed a length-only test, keyed its
	// own bucket, then had no stamp — which dropped laterThan into the
	// string compare the tagged arm above exists to close.
	//
	// AND HEX DOES NOT SETTLE IT, which is the arm that was missing.
	// v1.2.3-abcdef123456 is as legitimate a prerelease tag as its
	// all-letter sibling and satisfies the hex test exactly, so the round
	// that added hex closed the case it could see and left the case that
	// matters — measured, it still keyed its own bucket and still reached
	// laterThan's string compare, the one laterThan's own comment says
	// nothing here can reach. The tail cannot decide this. A
	// pseudo-version carries a 14-digit stamp and a tag does not, so the
	// last two arms are the discriminating pair: identical in shape,
	// opposite in answer, separated only by the stamp. Raised in review
	// of #497.
	for _, tc := range []struct {
		v    string
		want bool
	}{
		{"v0.0.0-20260913132232-e5cdb56ececd", true},
		{"v0.1.1-0.20260913132232-e5cdb56ececd", true},
		{"v0.1.1-pre.0.20260913132232-e5cdb56ececd", true},
		{"v1.2.3-abcdefghijkl", false},
		{"v1.2.3-abcdefABCDEF", false},
		{"v1.2.3-abcdef123456", false},
		{"v0.1.0", false},
	} {
		if _, got := revisionOf(tc.v); got != tc.want {
			t.Errorf("revisionOf(%q) reports %v, want %v — a twelve-character hex "+
				"tail is a revision only when the version also carries a stamp, "+
				"because a prerelease tag can have one too", tc.v, got, tc.want)
		}
	}

	// AND THE OTHER SIDE OF THE SAME FACT, which had no reporter at all.
	// revisionOf answers false for a plain tag and for a MALFORMED
	// pseudo-version alike, and the two want opposite treatment: one is
	// legitimate and merely unorderable, the other is a pin no proxy can
	// serve. Filed together, the second reached only a t.Logf describing
	// it as "a plain tag" and was dropped from the existence check.
	// The stamp separates them, and the first and fourth arms here are
	// the discriminating pair — both fail revisionOf, both are hex-
	// tailed, and they answer oppositely. Raised in review of #497.
	for _, tc := range []struct {
		v    string
		want bool
	}{
		// git's default abbreviation is seven, which is what this file's
		// own offline remedy prints with `-c core.abbrev=12` dropped.
		{"v0.0.0-20260913132232-e5cdb56", true},
		// And clipped by one, which is the `sed` across the tree hazard.
		{"v0.0.0-20260913132232-e5cdb56ecec", true},
		{"v0.0.0-20260913132232-e5cdb56ececd", false},
		{"v1.2.3-abcdef123456", false},
		{"v0.1.0", false},
		// The sentinels have their own reporter and their own remedy,
		// and both carry a well-formed tail, so neither lands here.
		{"v0.0.0", false},
		{"v0.0.0-00010101000000-000000000000", false},
	} {
		if got := malformedPseudo(tc.v); got != tc.want {
			t.Errorf("malformedPseudo(%q) reports %v, want %v — a version carrying "+
				"a 14-digit stamp and no twelve-hex revision is a pseudo-version "+
				"that failed, not a tag, and filing it as a tag is what let it "+
				"past every check in this file", tc.v, got, tc.want)
		}
	}

	// AND THE CONSEQUENCE, stated where it bites rather than only as a
	// property of revisionOf: laterThan's string fallback is documented
	// as unreachable by construction, and a hex-tailed tag reaching
	// revisionOf is exactly what made that false. skewFrom now files it
	// under `tagged`, so `version` holds nothing without a stamp.
	hexTag := []ownRequire{
		{"mcp", "github.com/WonderForgeLabs/gooey", "v1.2.3-abcdef123456"},
		{"paint", "github.com/WonderForgeLabs/gooey", newer},
	}
	// One revision and one unorderable tag, so there is no skew to report
	// and skewFrom's early return leaves `newest` empty — that empty
	// string is the "nothing to compare" answer, not a reference. Under
	// the defect the tag keys a SECOND bucket, which is two revisions,
	// which is skew: `behind` fills and the tag is not skipped. Both
	// observables move, and in opposite directions.
	if got, _, behind, skipped := skewFrom(hexTag); got != "" || len(behind) != 0 ||
		len(skipped) != 1 {
		t.Errorf("skewFrom with a hex-tailed prerelease tag beside a "+
			"pseudo-version reports newest=%q behind=%v skipped=%v; want no skew "+
			"and the tag skipped. Bucketed as a revision the tag is skew against "+
			"a tree nobody has broken, and it carries no stamp — so laterThan "+
			"compares the two as strings and can name the TAG as the reference "+
			"every other module is told to move to", got, behind, skipped)
	}

	// AND stampOf ITSELF, on the three spellings Go writes. The two
	// arms above exercise it through skewFrom, where an ordering can be
	// right for the wrong reason on two inputs.
	for _, tc := range []struct{ v, want string }{
		{"v0.0.0-20260913132232-e5cdb56ececd", "20260913132232"},
		{"v0.1.1-0.20260913132232-e5cdb56ececd", "20260913132232"},
		{"v0.2.0-rc.1.0.20260913132232-e5cdb56ececd", "20260913132232"},
		{"v0.1.0", ""},
	} {
		if got := stampOf(tc.v); got != tc.want {
			t.Errorf("stampOf(%q) = %q, want %q", tc.v, got, tc.want)
		}
	}

	// AND stampNames, WHICH IS A DIFFERENT QUESTION. stampOf reading a
	// stamp does not make the pin check's comparison right: that check
	// was strings.Contains(version, "-"+want+"-"+rev), and the two
	// tagged spellings put a DOT in front of the stamp, so a correct pin
	// off the first per-subdirectory tag would have been reported as
	// carrying the wrong committer date — naming the stamp it already
	// has. The third reader was the only one with no arm. Raised in
	// review of #497.
	for _, tc := range []struct {
		v, want string
		ok      bool
	}{
		{"v0.0.0-20260913132232-e5cdb56ececd", "20260913132232", true},
		{"v0.1.1-0.20260913132232-e5cdb56ececd", "20260913132232", true},
		{"v0.2.0-rc.1.0.20260913132232-e5cdb56ececd", "20260913132232", true},
		// One digit out, which is the hazard the check exists for.
		{"v0.1.1-0.20260913132232-e5cdb56ececd", "20260913132233", false},
		{"v0.1.0", "20260913132232", false},
	} {
		if got := stampNames(tc.v, tc.want); got != tc.ok {
			t.Errorf("stampNames(%q, %q) = %v, want %v — every spelling Go writes "+
				"has to compare the same way, or a correct pin fails on the day "+
				"the first tag is cut", tc.v, tc.want, got, tc.ok)
		}
	}

	// AND THE SENTINEL PREDICATE, because revisionOf CANNOT be asked
	// this. The canonical sentinel is twelve hex characters behind a
	// fourteen-digit stamp, so it wears a pseudo-version's shape
	// exactly — measured below — and any reader that filters on
	// revisionOf's ok alone lets it through to be reported a second
	// time, with a second remedy, for one line. Raised in review of
	// #497.
	if rev, ok := revisionOf("v0.0.0-00010101000000-000000000000"); !ok || rev != "000000000000" {
		t.Errorf("revisionOf(sentinel) = (%q, %v), want (\"000000000000\", true) — "+
			"this fixture exists to record that the sentinel PASSES the shape "+
			"gate, which is why unservableSentinel is asked separately", rev, ok)
	}
	for _, tc := range []struct {
		v  string
		is bool
	}{
		{"v0.0.0", true},
		{"v0.0.0-00010101000000-000000000000", true},
		{"v0.0.0-20260913132232-e5cdb56ececd", false},
		{"v0.1.0", false},
	} {
		if got := unservableSentinel(tc.v); got != tc.is {
			t.Errorf("unservableSentinel(%q) = %v, want %v", tc.v, got, tc.is)
		}
	}

	// AND THE COVERAGE RULE, which is the one distinction this file
	// exists to make: "checked nothing" is a failure and "checked some"
	// is a note. The tree cannot show either — every pin here is an
	// object — so it is a fixture or it is a hand-run clone in a review
	// comment, which is not a check. Raised in review of #497.
	for _, tc := range []struct {
		name     string
		pins     int
		absent   []string
		wantFail bool
		want     string // a phrase the message must carry, "" for no message
	}{
		{"everything present", 1, nil, false, ""},
		{"the only pin absent", 1, []string{"a"}, true, "NOTHING was checked here"},
		{"every pin absent", 3, []string{"a", "b", "c"}, true, "NOTHING was checked here"},
		{"some absent", 3, []string{"a"}, false, "1 of the 3 pinned revisions"},
	} {
		fail, msg := pinCoverage(tc.pins, tc.absent)
		if fail != tc.wantFail {
			t.Errorf("%s: pinCoverage fails=%v, want %v — the difference between "+
				"a run that verified nothing and one that verified most of it is "+
				"the difference between red and a note `go test` discards",
				tc.name, fail, tc.wantFail)
		}
		if tc.want == "" {
			if msg != "" {
				t.Errorf("%s: pinCoverage says %q with nothing absent", tc.name, msg)
			}
			continue
		}
		if !strings.Contains(msg, tc.want) {
			t.Errorf("%s: pinCoverage says %q, want it to carry %q",
				tc.name, msg, tc.want)
		}
		// BOTH REMEDIES, ON BOTH ARMS. The nothing-ran arm is a
		// t.Error, so `git clone --depth 1` of this repo reds the root
		// suite — and a reader at a terminal has no checkout step to
		// set fetch-depth on. A message that names only the CI remedy
		// spends the attention "a red suite is yours" exists to buy, on
		// a failure that is not theirs. Raised in review of #497.
		//
		// AND THE CI HALF IS `matrix.depth`, NOT `fetch-depth: 0`. The
		// literal this asked for stopped being in the workflow when the
		// depth became derived — it survives only in ci.yml's comments
		// — so this arm was pinning the one spelling a reader could not
		// find. Raised in review of #497, round after.
		for _, want := range []string{"--unshallow", "matrix.depth"} {
			if !strings.Contains(msg, want) {
				t.Errorf("%s: pinCoverage's message does not name %q. Both the "+
					"local and the CI remedy have to be there: the reader who "+
					"hits this could be at either:\n\t%s", tc.name, want, msg)
			}
		}
		// AND NOT AS actions/checkout's DOING, which is a cause the
		// local reader never had.
		if strings.Contains(msg, "actions/checkout") {
			t.Errorf("%s: pinCoverage attributes the shallow clone to "+
				"actions/checkout, which a developer who ran `git clone "+
				"--depth 1` never invoked:\n\t%s", tc.name, msg)
		}
	}

	// AND THE POPULATION THAT READS IT, on a fixture, because the tree
	// carries nothing to exclude and so cannot show an exclusion
	// happening. Three requires in, one revision out: the sentinel and
	// the plain tag belong to the shape guard, which reports each with
	// its own remedy, and a second report here would be a second remedy
	// for one line. Raised in review of #497.
	//
	// THE FOURTH ROW IS THE ONE THAT WAS MISSING, and it is the shape
	// that got past the old hand-written dispatch: a version with NO
	// LEADING v but a well-formed <stamp>-<rev> tail. classifyRequire
	// calls it not-a-version and pinsOf drops it; unservableSentinel says
	// false and revisionOf succeeds, so the old gate let it in. Measured
	// before the fix, on one require:
	//
	//	pinsOf         → 0
	//	pinPopulations → at[e5cdb56ececd] = "mcp → …"
	//
	// With a bogus tail that is one bad require producing two failures
	// with two different remedies, which is the exact defect pinsOf's own
	// doc records fixing on the skew path. Raised in review of #497.
	at, spellings := pinPopulations([]ownRequire{
		{"apps/x", coreModule, "v0.0.0-00010101000000-000000000000"},
		{"apps/y", coreModule, "v0.1.0"},
		{"apps/w", coreModule, "0.0.0-20260913132232-e5cdb56ececd"},
		{"apps/z", coreModule, "v0.0.0-20260913132232-e5cdb56ececd"},
		// TWO PINS AT ONE REVISION, which is what the tree looks like
		// after the `sed` the existence error's own text names — all of
		// them carrying the one bad commit. Keeping the FIRST require
		// per revision was right for asking git and wrong for telling
		// the reader: they fixed one module, re-ran, and were handed the
		// next. Raised in review of #497.
		{"apps/v", coreModule, "v0.0.0-20260913132232-e5cdb56ececd"},
	})
	if len(at) != 1 || len(spellings) != 1 {
		t.Errorf("pinPopulations over a sentinel, a tag, a v-less pseudo-version "+
			"and one pin gave at=%v spellings=%v; want just the pin. Anything "+
			"else reaches the commit checks and reports a line the shape guard "+
			"has already reported, with a different remedy", at, spellings)
	}
	if got := at["e5cdb56ececd"]; len(got) != 2 {
		t.Errorf("the revision's population is %v — two requires name that "+
			"commit and the existence and ancestry errors print this slice, so "+
			"keeping one of them makes a tree-wide typo a queue of runs rather "+
			"than one report", got)
	}
	if got := spellings["0.0.0-20260913132232-e5cdb56ececd"]; got != "" {
		t.Errorf("the v-less spelling is in the spellings population as %q. It "+
			"shares a revision with the pin beside it, so it would be reported "+
			"as a SECOND SPELLING of that commit — advice to unify two lines "+
			"where the real remedy is that one of them is not a version at all",
			got)
	}

	// AND THE ORDERING the reports are built with.
	if got := sortedKeys(map[string]string{"c": "", "a": "", "b": ""}); got[0] != "a" ||
		got[1] != "b" || got[2] != "c" {
		t.Errorf("sortedKeys gave %v — a report ranging a Go map reorders itself "+
			"run to run, which is what skewFrom sorts to avoid", got)
	}
}

// TestTwoCommitsInOneSecondReachTheStringFallback is the second route
// into laterThan's backstop — the one that needs no unparsed version and
// that the comment above it used to exclude. Raised in review of #497.
//
// The short-circuit requires the stamps to DIFFER, so two distinct
// commits carrying the same 14-digit second (two pushes in one second,
// or a landing and the merge that contains it) fall through to the
// string compare with both stamps present. What decides is then the
// trailing revision hash.
//
// THE ASSERTION IS THE PROPERTY, NOT THE WINNER. Which hash sorts higher
// is arbitrary and this test does not bless it; what it pins is that the
// order is TOTAL and STABLE — exactly one of the two is later, and the
// answer does not depend on argument order — because that is what
// skewFrom needs to name a reference at all. A tie-break on anything
// meaningful is not available offline: two commits one second apart
// carry no evidence of their order, which is the same reason a plain tag
// carries none.
func TestTwoCommitsInOneSecondReachTheStringFallback(t *testing.T) {
	const stamp = "20260913132232"
	a := "v0.0.0-" + stamp + "-aaaaaaaaaaaa"
	b := "v0.0.0-" + stamp + "-bbbbbbbbbbbb"

	// NON-VACUITY: if either stamp were missing the fallback would be
	// reached for the round-6 reason instead, and this would be a second
	// copy of the hex-tag arm.
	if stampOf(a) != stamp || stampOf(b) != stamp {
		t.Fatalf("stampOf reads %q and %q, want %q for both — these fixtures "+
			"reach the fallback because the stamps are EQUAL, not because "+
			"either is unparsed", stampOf(a), stampOf(b), stamp)
	}
	if laterThan(a, b) == laterThan(b, a) {
		t.Errorf("laterThan(a,b)=%v and laterThan(b,a)=%v for two revisions "+
			"sharing one stamp, so the order is not total: skewFrom picks a "+
			"reference by scanning a map, and an order that answers the same "+
			"way both ways round makes which module it met first decide",
			laterThan(a, b), laterThan(b, a))
	}
}

// hasRevisionTail reports whether v's last dash-part is twelve lowercase
// hex characters — the SHAPE of a pseudo-version's revision, said
// without reference to the stamp.
//
// It is revisionOf's first half, extracted, and the extraction is the
// point: revisionOf answers false for TWO reasons — no such tail, and no
// 14-digit stamp — and everything downstream had to re-derive which.
// See malformedPseudo.
func hasRevisionTail(v string) bool {
	i := strings.LastIndex(v, "-")
	if i < 0 || len(v)-i != 13 {
		return false
	}
	for _, c := range v[i+1:] {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// malformedPseudo is the one shape this file could not report: a
// version TRYING to be a pseudo-version and failing.
//
// TWO WAYS TO FAIL, AND THE FIRST VERSION SAW ONE. It was
// `!revisionOf(v).ok && stampOf(v) != ""`, which re-derived the reason
// from stampOf alone — so the case where the STAMP is what is broken was
// unreachable by construction: a 13- or 15-digit stamp makes stampOf
// answer "" and the whole thing collapses to "plain tag". Measured on
// this branch, every one of these was filed as a tag and reported by
// nothing:
//
//	v0.0.0-2026091313223-e5cdb56ececd     13-digit stamp
//	v0.0.0-202609131322321-e5cdb56ececd   15-digit
//	v0.0.0--e5cdb56ececd                  no stamp at all
//	v0.0.0-2026091x132232-e5cdb56ececd    a letter in the stamp
//
// Go's pseudo-version form requires EXACTLY 14 digits, so each of these
// is read as an ordinary prerelease and the proxy is asked for a tag of
// that name — the same unresolvable require as the clipped revision,
// with shapeMsg silent and skewFrom filing it under `tagged`, which is
// the mis-filing shapeMalformed's own comment calls "what let it past
// every check in this file".
//
// THE DISCRIMINATOR IS THE TAIL, asked directly rather than through
// revisionOf's conjunction. `>= 2` dashes is what keeps a legitimate
// prerelease tag out: v1.2.3-abcdef123456 has one dash and a hex tail,
// and the arm at TestEveryRequireShapeReachesItsOwnArm pins that it
// stays a tag. Raised in review of #497.
func malformedPseudo(v string) bool {
	if _, ok := revisionOf(v); ok {
		return false
	}
	return stampOf(v) != "" || (hasRevisionTail(v) && strings.Count(v, "-") >= 2)
}

// pinPopulations splits the tree's own-module requires into the two
// keyed sets the pin check needs: revision → a representative pin, and
// full version string → where it was found.
//
// A FUNCTION SO THE SKIPS CAN BE PINNED. The population is where a
// require is excluded, and on a healthy tree there is nothing to exclude
// — so a fixture handed straight to it is the only way an arm can see
// that a sentinel and a plain tag stay out. Raised in review of #497.
func pinPopulations(reqs []ownRequire) (at map[string][]string, spellings map[string]string) {
	at, spellings = map[string][]string{}, map[string]string{}
	for _, r := range reqs {
		// classifyRequire, NOT A SECOND COPY OF THE DISPATCH. This read
		// `unservableSentinel` and then `revisionOf` by hand, which is a
		// different predicate from the one pinsOf asks, and the two came
		// to disagree. Measured on this tree:
		//
		//	classifyRequire("0.0.0-20260913132232-e5cdb56ececd")
		//	  = not-a-version
		//	pinsOf                 → 0 requires
		//	pinPopulations         → at[e5cdb56ececd] = "mcp → …"
		//
		// A version missing its leading v but carrying a well-formed
		// <stamp>-<rev> tail was rejected by the shape gate and ACCEPTED
		// here. With a bogus tail that is one bad require producing two
		// failures with two different remedies — "which is not a version"
		// from the shape guard, and "pins commit …, which is not a commit
		// in this repository" from the existence guard — which is exactly
		// the defect pinsOf's own doc records fixing on the skew path,
		// and exactly the third door unservableSentinel's doc says a
		// predicate exists to close.
		//
		// The stamp loop below already carries the error text for this
		// state ("pinPopulations is meant to have filtered it out, so the
		// two walks have come to disagree about what a pin is") and could
		// not fire: the divergence was upstream of its own guard. Raised
		// in review of #497.
		if classifyRequire(r.version) != shapePin {
			continue // the shape guard reports every other shape, with its own remedy
		}
		rev, ok := revisionOf(r.version)
		if !ok {
			// A PLAIN TAG, and that is now the only shape reaching here.
			// The shape guard ACCEPTS one — it is a version — so nothing
			// reports it, and the only thing that mentions it is the
			// caller's tagged t.Logf, which this file measures to be
			// invisible for a passing package. The malformed-pseudo half
			// this comment used to describe is filtered above.
			continue
		}
		where := r.dir + " → " + r.path + " " + r.version
		// EVERY REQUIRE AT THAT REVISION, not the first. One
		// representative is the right answer for QUERYING git —
		// existence and ancestry are properties of the commit — and it
		// was being used for REPORTING too, which is a different
		// question. The scenario the existence error's own text names is
		// a `sed` across the tree, where all 36 pins carry the one bad
		// revision: the reader was handed one module, fixed it, re-ran,
		// and was handed the next. Thirty-six sequential runs to see
		// thirty-six lines. skewFrom already solved this the same way
		// (byRev accumulates and skewGroup.at prints all of them), and
		// `absent` inherited it, so pinCoverage's some-absent arm
		// claimed to say WHICH pins were not looked at and named one per
		// revision. The git commands below still run once per revision.
		// Raised in review of #497.
		at[rev] = append(at[rev], where)
		if _, seen := spellings[r.version]; !seen {
			spellings[r.version] = where
		}
	}
	return at, spellings
}

// TestEveryOwnModulePinNamesACommitThisRepositoryPublished is the half
// the shape and skew checks cannot reach: whether the commit a pin names
// is a commit at all.
//
// The whole argument of the guard above is that these lines are read
// OUTSIDE the workspace, by somebody's `go get`, where an unservable
// revision fails. `v0.0.0` and the canonical `v0.0.0-00010101000000-…`
// are caught by shape. A pin to a commit no proxy can serve is not: a
// hash transposed by hand, a commit that only lived on a branch that was
// force-pushed away, or one taken from a worktree's unmerged head. The
// skew check catches the ONE-module typo, because it disagrees with its
// siblings — but these 36 pins get rewritten by a `sed` across the tree,
// and a uniform edit is indistinguishable from a correct one to every
// check that compares pins against each other.
//
// NO NETWORK IS NEEDED, because the repository IS the module. git
// answers both halves: the object exists, and it is an ancestor of
// origin/main rather than something that only ever existed here.
//
// THE STAMP IS CHECKED TOO, and it comes free from the same command. A
// pseudo-version's 14 digits are the commit's committer date in UTC, and
// `go mod edit -require` writes literally what it is handed — so a pin
// whose stamp does not match its own commit is a real hazard the shape
// check cannot see, and `go get` rejects it.
//
// A SHALLOW CLONE COSTS ONE CONJUNCT, NOT THE TEST. This gave up the
// moment `git rev-parse --is-shallow-repository` said true — and
// actions/checkout defaults to fetch-depth 1, so it never ran in CI at
// all. Measured in the review that found it: in a shallow CI checkout
// `git cat-file -e e5cdb56ececd^{commit}` and `git merge-base
// --is-ancestor e5cdb56ececd origin/main` BOTH succeed, and the test
// skipped anyway — a check reporting green while never reaching the
// thing it is named for, which is the defect this whole file narrates.
//
// Shallowness is a property of the REPOSITORY; the question here is
// per-REVISION, and a shallow clone still holds the commits near its
// tip. So existence is asked per revision and an absent object is
// reported-and-skipped only where shallowness explains it. `merge-base
// --is-ancestor` is the one conjunct that needs the whole gate: a
// truncated history answers it WRONGLY rather than not at all, so it is
// the only thing shallowness costs. Raised in review of #497.
func TestEveryOwnModulePinNamesACommitThisRepositoryPublished(t *testing.T) {
	git := func(args ...string) (string, error) {
		out, err := exec.Command("git", args...).Output()
		return strings.TrimSpace(string(out)), err
	}

	shallow, err := git("rev-parse", "--is-shallow-repository")
	if err != nil {
		t.Skipf("git is not answering here (%v), so this check cannot run; the "+
			"shape and skew checks above still did", err)
	}
	// Reachability needs a published branch to compare against AND a
	// history deep enough for the answer to mean anything.
	canReach := shallow != "true"
	if canReach {
		if _, err := git("rev-parse", "--verify", "origin/main"); err != nil {
			canReach = false
		}
	}

	// TWO POPULATIONS OUT OF ONE WALK, because the two questions have
	// different keys and keying them alike is what left 35 of 36 pins
	// unchecked. Existence and ancestry are properties of the COMMIT, so
	// one representative per revision answers them. The stamp is a
	// property of the version STRING — two spellings of one commit are
	// two claims — so a transposed stamp in one module's pin sat in the
	// same bucket as the correct one and was never read. Measured: the
	// skew check cannot see it either (skewFrom keys on the trailing 12
	// characters, so both land in one bucket), and `go work vendor`
	// accepts it, so the whole root suite went green over a pin `go get`
	// refuses. Raised in review of #497.
	all, _ := allOwnRequires(t)
	at, spellings := pinPopulations(all)
	if len(at) == 0 {
		t.Skip("no own-module require names a pseudo-version, so there is no " +
			"commit to look for — the guard above reports that state")
	}

	// Revisions whose object is genuinely not here. Reported once at the
	// end rather than per pin, and only where shallowness explains it.
	var absent []string
	present := map[string]bool{}
	// SORTED, like skewFrom's report and for the same reason: a report
	// that reorders itself run to run is hard to read, and a reader
	// comparing two runs has to diff them. Raised in review of #497.
	for _, rev := range sortedKeys(at) {
		where := strings.Join(at[rev], "\n\t")
		if _, err := git("cat-file", "-e", rev+"^{commit}"); err != nil {
			if shallow == "true" {
				absent = append(absent, at[rev]...)
				continue
			}
			t.Errorf("%d require(s) pin commit %s, which is not a commit in this "+
				"repository. `go get` of those modules fails with \"unknown "+
				"revision\" for everybody outside this workspace, and every "+
				"check that compares pins against each other passes a "+
				"tree-wide typo — `sed` is how these get rewritten, which is "+
				"why they are all named here rather than one at a time:\n\t%s",
				len(at[rev]), rev, where)
			continue
		}
		present[rev] = true
		if !canReach {
			continue
		}
		if _, err := git("merge-base", "--is-ancestor", rev, "origin/main"); err != nil {
			t.Errorf("%d require(s) pin commit %s, which exists here but is NOT an "+
				"ancestor of origin/main — a branch commit, or one taken from "+
				"an unmerged worktree head. A proxy can only serve what the "+
				"published history contains:\n\t%s", len(at[rev]), rev, where)
		}
	}

	// EVERY SPELLING, not one per commit. The stamp is the 14 digits of
	// the version string, and two modules can name one commit with two
	// different ones.
	for _, version := range sortedKeys(spellings) {
		where := spellings[version]
		// THE ok IS READ, for the reason the newestRev arm gives about
		// dropping it: correct today only because pinPopulations
		// filtered first, and a silent "" would key present[] at the
		// empty string and skip every spelling the moment the two walks
		// come to disagree about what a pin is. Raised in review of
		// #497.
		rev, ok := revisionOf(version)
		if !ok {
			t.Errorf("the stamp check reached %s with version %q, which has no "+
				"revision — pinPopulations is meant to have filtered it out, so "+
				"the two walks have come to disagree about what a pin is",
				where, version)
			continue
		}
		if !present[rev] {
			continue // said below
		}
		// The stamp is the same commit's committer date, in UTC.
		// `format-local` means "in TZ", so TZ is set on THIS command
		// rather than for the process — the rest of this package's tests
		// share a runtime with it.
		utc := exec.Command("git", "show", "-s",
			"--date=format-local:%Y%m%d%H%M%S", "--format=%cd", rev)
		utc.Env = append(utc.Environ(), "TZ=UTC")
		stamp, err := utc.Output()
		if err != nil {
			t.Errorf("reading %s's committer date: %v", rev, err)
			continue
		}
		want := strings.TrimSpace(string(stamp))
		if !stampNames(version, want) {
			t.Errorf("%s names commit %s with a stamp that is not that commit's "+
				"committer date in UTC (%s). `go mod edit -require` writes "+
				"literally what it is handed, so a hand-built pseudo-version "+
				"can carry the wrong 14 digits and be refused by `go get` while "+
				"passing every shape check here — including the skew check, "+
				"which buckets by commit and cannot see two stamps of one",
				where, rev, want)
		}
	}

	sort.Strings(absent)
	nothingRan, msg := pinCoverage(len(at), absent)
	if msg != "" {
		if nothingRan {
			t.Error(msg)
		} else {
			t.Log(msg)
		}
	}
	// WHICH REASON, because the two are not the same news. On a shallow
	// clone the absent-revision report above has already said what was
	// skipped. On a FULL clone the ancestry conjunct is the only thing
	// dropped and nothing else says so, so it is an error: a checkout
	// that can see every commit and not the branch they are supposed to
	// be on is a misconfigured remote, and naming it is cheaper than a
	// reader discovering later that this test never asked. Raised in
	// review of #497.
	switch {
	case canReach:
	case nothingRan:
		// SUPPRESSED, because the line below would withdraw the one
		// above it. pinCoverage has just said no check ran at all, and
		// "existence and stamp ran for every revision present here" is
		// true only of an empty set — measured verbatim in a real `git
		// clone --depth 1 --no-local` of this branch, the two lines
		// landed two apart and contradicted each other. It is not a
		// corner: the tree holds ONE revision across every pin, so the
		// all-absent arm is the only shallow outcome available today,
		// and it is the CI failure the fetch-depth change exists to
		// prevent. Raised in review of #497.
	case shallow == "true":
		t.Logf("ancestry of origin/main was NOT checked for any pin: this clone " +
			"is SHALLOW, and a truncated history answers `merge-base " +
			"--is-ancestor` wrongly rather than not at all. Existence and stamp " +
			"ran for every revision present here")
	default:
		t.Error("ancestry of origin/main was NOT checked for any pin, and this " +
			"clone is NOT shallow — it simply has no origin/main ref. Every " +
			"other conjunct ran, so the one thing unverified is whether these " +
			"commits are on the published branch at all, which is the half a " +
			"proxy can serve. `git fetch origin main` fixes it; a remote under " +
			"another name needs this test taught the name")
	}
}

// pinCoverage decides what a run that could not look at every pinned
// revision should report about it, and whether that is a failure.
//
// A PREDICATE, FOR comparedNothing's REASON. The rule it replaces was an
// inline switch making the distinction this whole file is about —
// "checked nothing" against "checked some" — and nothing called it, so
// the only evidence for it was a hand-run `git clone --depth 1` in a
// review comment. A hand-run clone is not a check. Every other rule here
// is a function with an arm: revisionOf, stampOf, stampNames,
// unservableSentinel, pinPopulations, sortedKeys, skewFrom, skewMsg.
// Raised in review of #497.
//
// FAILING WHEN NOTHING RAN is the substance. t.Logf is invisible for a
// passing package — this file measures that itself, at the `go test`
// buffering note above — so a run that verified nothing reported `ok`
// and said so only under -v. It is not a corner: the tree holds ONE
// revision across all its pins today, so a single absent object removes
// everything there was to check.
//
// TWO REMEDIES, AND CI IS ONLY ONE OF THEM. Both messages used to
// explain the shallow clone as actions/checkout's doing and print
// `fetch-depth: 0`. Once the nothing-ran arm became a t.Error that made
// `git clone --depth 1` of this repo red the root suite with no remedy
// the reader could act on: they have no checkout step, and the sentence
// names an action they never ran. CLAUDE.md's "a red suite is yours" is
// what makes that expensive — a contributor spends the attention that
// rule is for on a failure which is not theirs. The local remedy is
// `git fetch --unshallow`, or `--deepen=<n>`: measured, a depth-40
// deepen brings the pinned revisions into reach while the clone stays
// shallow, which is the some-absent arm. CLAUDE.md's Verify section now
// carries the requirement too, because it lived only inside this string
// and a ci.yml comment. Raised in review of #497.
func pinCoverage(pins int, absent []string) (fail bool, msg string) {
	// THE CI REMEDY IS NO LONGER A KNOB TO SET, and this said it was:
	// after the matrix-depth change `fetch-depth: 0` appears in ci.yml
	// only inside comments — the checkout step reads
	// `fetch-depth: ${{ matrix.depth }}`. A reader who hit this in CI
	// and grepped for the string found prose rather than the line, and
	// the arm below hard-asserted that spelling, so the stale one was
	// the pinned one. A leg that reds here now means the DERIVATION
	// broke — the leg running this suite did not get the root module —
	// which is a different action from the local deepen. Raised in
	// review of #497.
	const remedy = "Deepen the clone: `git fetch --unshallow` locally (or " +
		"`--deepen=50`, which reaches these while the clone stays shallow). " +
		"In CI there is nothing to set by hand: the checkout step takes " +
		"`fetch-depth: ${{ matrix.depth }}`, and discover gives depth 0 to " +
		"the leg carrying the root module — so this failing there means that " +
		"derivation broke, not that a depth wants editing"
	switch {
	case len(absent) == 0:
		return false, ""
	case len(absent) >= pins:
		return true, fmt.Sprintf("NOTHING was checked here: this is a shallow "+
			"clone and not one of the %d pinned revisions is an object in it, "+
			"so no existence or stamp check ran at all: %s. %s",
			pins, strings.Join(absent, "; "), remedy)
	default:
		// SAID, NOT PASSED OVER, and named one by one: a reader has to
		// be able to tell "checked and clean" from "not looked at".
		return false, fmt.Sprintf("this is a SHALLOW clone and %d of the %d "+
			"pinned revisions are not objects here, so their existence and "+
			"stamp were NOT checked: %s. %s; existence and stamp ran for the "+
			"rest", len(absent), pins, strings.Join(absent, "; "), remedy)
	}
}

// allOwnRequires is every require of this repository's own modules,
// across the root module and every nested one, read TEXTUALLY.
//
// ONE POPULATION, TWO QUESTIONS. Two tests built this walk
// independently — same command, same anonymous struct, same two
// t.Fatalfs, same `own` predicate declared twice — and the divergence
// was not cosmetic: the second copy keyed its map by revision where the
// first keyed by module, which is how 35 of 36 pins came to be
// unchecked. Asking two questions of one population makes them agree by
// construction. Raised in review of #497.
//
// It returns the module directories too, because "the walk came back
// empty" is a failure both callers have to be able to describe.
func allOwnRequires(t *testing.T) (reqs []ownRequire, mods []string) {
	t.Helper()
	mods = modulesIncludingTheRoot(t)
	if len(mods) == 0 {
		t.Fatal("no modules found; the walk is wrong, not the tree")
	}
	for _, dir := range mods {
		// `go mod edit -json` is textual — it reports what the file says
		// rather than what the workspace would resolve, which is the whole
		// point here. It is also how ci.yml reads the `go` directive, so
		// this adds no dependency the repo does not already rely on.
		cmd := exec.Command("go", "mod", "edit", "-json")
		cmd.Dir = dir
		body, err := cmd.Output()
		if err != nil {
			t.Fatalf("%s: go mod edit -json: %v", dir, err)
		}
		var mf struct {
			Require []struct{ Path, Version string }
		}
		if err := json.Unmarshal(body, &mf); err != nil {
			t.Fatalf("%s: parsing go mod edit -json: %v", dir, err)
		}
		for _, r := range mf.Require {
			if !ownModule(r.Path) {
				continue
			}
			reqs = append(reqs, ownRequire{dir, r.Path, r.Version})
		}
	}
	return reqs, mods
}

// coreModule is this repository's own module path.
const coreModule = "github.com/WonderForgeLabs/gooey"

// ownModule reports whether path is core or a module under it. A sibling
// is core's path plus a DIRECTORY, and prefixing with the slash is what
// keeps a future `github.com/WonderForgeLabs/gooeyfoo` from matching.
func ownModule(path string) bool {
	return path == coreModule || strings.HasPrefix(path, coreModule+"/")
}

// TestEveryRequireShapeReachesItsOwnArm is the DISPATCH's fixture, and
// it is the half the tree cannot supply: the real tree holds 36 correct
// pins, so every gate could be deleted from the caller with the whole
// package green. Measured in review of #497, round sixteen, on
// malformedPseudo's block.
//
// The three halves are separate assertions because they are separate
// claims. classifyRequire is the rule; shapeMsg is what turns a
// classification into something a human reads; pinsOf is the wiring,
// and it is the one that says a rejected require never reaches the skew
// and existence checks — the ordering the comment in the caller calls
// load-bearing and nothing read.
//
// THE MIDDLE ONE WAS MISSING, and its absence was measured rather than
// argued: with the reporting arms three `if shape == …` blocks in the
// loop, disabling any one of them — or all three at once — left the
// whole root package green, because pinsOf had already taken the
// rejected require out of every downstream population. The rule was
// pinned and the DISPATCH was not, which is round sixteen's finding one
// refactor on. Each arm is asserted by its own distinguishing REMEDY
// rather than by being non-empty: two shapes sharing one message is the
// state that makes a lost arm invisible again. Raised in review of
// #497.
func TestEveryRequireShapeReachesItsOwnArm(t *testing.T) {
	const good = "v0.0.0-20260913132232-e5cdb56ececd"
	for _, tc := range []struct {
		v    string
		want requireShape
		why  string
	}{
		{good, shapePin, "a well-formed pseudo-version is the population everything below reads"},
		{"v0.1.0", shapePin, "and so is a plain tag: unorderable for skew, but a proxy can serve it"},
		{"v0.0.0", shapeSentinel, "the placeholder no proxy can serve, with its own remedy"},
		{"v0.0.0-00010101000000-000000000000", shapeSentinel,
			"the canonical zero pseudo-version is the same unservable revision " +
				"wearing a pseudo-version's shape — and it carries a stamp, so " +
				"asking malformedPseudo first would file it under the wrong remedy"},
		{"e5cdb56ececd", shapeNotAVersion, "`go mod edit -require` writes literally what it is handed"},
		{"v0.0.0-20260913132232-e5cdb56", shapeMalformed,
			"git's default abbreviation is seven, which is this file's own " +
				"offline remedy with `-c core.abbrev=12` dropped"},
		// THE OTHER WAY revisionOf SAYS NO, and the four rows below are
		// the population that was unreachable until this round: the
		// REVISION is well-formed and the STAMP is not, so re-deriving
		// the reason from stampOf answered "" and filed each of them as
		// a plain tag. Go's form wants exactly 14 digits, so every one
		// is read as an ordinary prerelease and the proxy is asked for a
		// tag of that name — the same unresolvable require as the
		// clipped revision above, with nothing saying so.
		{"v0.0.0-2026091313223-e5cdb56ececd", shapeMalformed,
			"one digit short: the `sed` across the tree this file's own hazard " +
				"note names, landing on the stamp instead of the revision"},
		{"v0.0.0-202609131322321-e5cdb56ececd", shapeMalformed,
			"one digit long, which no length check that only looks for `short` " +
				"would catch"},
		{"v0.0.0--e5cdb56ececd", shapeMalformed,
			"no stamp at all, which is the degenerate case of both rows above"},
		{"v0.0.0-2026091x132232-e5cdb56ececd", shapeMalformed,
			"fourteen characters and not fourteen DIGITS — the shape stampOf " +
				"rejects for a reason revisionOf cannot report"},
		// AND THE DISCRIMINATING PAIR, which is what keeps the new arm
		// from swallowing legitimate tags: same twelve-hex tail, opposite
		// answer, separated only by whether a stamp is being attempted.
		// The second dash is the whole test — a prerelease tag has one.
		{"v1.2.3-abcdef123456", shapePin,
			"a legitimate prerelease tag whose tail IS twelve hex characters; " +
				"the row above it differs only by carrying a second dash-part"},
	} {
		if got := classifyRequire(tc.v); got != tc.want {
			t.Errorf("classifyRequire(%q) = %s, want %s — %s", tc.v, got, tc.want, tc.why)
		}
	}

	// THE RENDERER. A remedy per shape, and "" for a pin — asserted by a
	// string only that shape's message carries, so two arms cannot
	// collapse into one without this noticing.
	for _, tc := range []struct {
		shape requireShape
		carry string
		why   string
	}{
		{shapePin, "", "a pin is not wrong about anything, so it has nothing to say"},
		{shapeSentinel, "GOWORK=off go list -m",
			"the sentinel's remedy is to go and get a real pseudo-version"},
		{shapeNotAVersion, "GOWORK=off go list -m",
			"a bare hash is repaired exactly as the sentinel is — by going and " +
				"getting a real pseudo-version — and it is the shape the " +
				"sentinel's own remedy warns you into, since `go mod edit " +
				"-require` writes literally what it is handed"},
		{shapeMalformed, "core.abbrev=12",
			"the malformed one is this file's own offline remedy with a flag " +
				"dropped, so naming the flag IS the fix"},
	} {
		msg := shapeMsg(tc.shape, ownRequire{"mcp", coreModule, "v0.0.0-20260913132232-e5cdb56"})
		if tc.carry == "" {
			if msg != "" {
				t.Errorf("shapeMsg(%s) = %q, want the empty string — %s", tc.shape, msg, tc.why)
			}
			continue
		}
		if !strings.Contains(msg, tc.carry) {
			t.Errorf("shapeMsg(%s) does not carry %q, so this shape cannot be told "+
				"from the others by what it tells the reader to do — %s. Got: %q",
				tc.shape, tc.carry, tc.why, msg)
		}
	}

	// THE WIRING. Every shape in one slice, and only the pins come back.
	all := []ownRequire{
		{"mcp", coreModule, "v0.0.0"},
		{"grpc", coreModule, good},
		{"editor", coreModule, "e5cdb56ececd"},
		{"paint", coreModule, "v0.0.0-20260913132232-e5cdb56"},
		{"apps/gitui", coreModule, "v0.1.0"},
	}
	got := pinsOf(all)
	if len(got) != 2 || got[0].dir != "grpc" || got[1].dir != "apps/gitui" {
		t.Errorf("pinsOf returned %v, want the grpc and apps/gitui requires and "+
			"nothing else. A sentinel, a non-version or a malformed pseudo-version "+
			"in the skew population is one bad require producing two failures with "+
			"two contradicting remedies — the shape error, and a skew group telling "+
			"the fixer to match a revision", got)
	}
}

// TestTheEmptyPopulationSaysWhichEmptyItIs pins the distinction round
// sixteen found: the message reported "not found" for a tree whose
// requires were all found and REJECTED.
//
// Neither arm is reachable from the tree — it holds 36 correct pins —
// so both live here.
func TestTheEmptyPopulationSaysWhichEmptyItIs(t *testing.T) {
	absent := emptyPopulationMsg(21, 0)
	if !strings.Contains(absent, "were found to require") {
		t.Errorf("with no own-module require found at all, the message does not "+
			"say the requires were not found:\n\t%s", absent)
	}
	rejected := emptyPopulationMsg(21, 36)
	if !strings.Contains(rejected, "REJECTED") || strings.Contains(rejected, "were found to require") {
		t.Errorf("with 36 requires found and every one rejected, the message must "+
			"say so rather than reporting them absent — a `sed` clipping one "+
			"character off every revision prints 36 accurate errors and then "+
			"this line:\n\t%s", rejected)
	}
	if !strings.Contains(rejected, "not a second defect") {
		t.Errorf("the rejected-population message does not tell the reader this "+
			"line goes away with the errors above it, so it reads as a %dth "+
			"failure to chase:\n\t%s", 37, rejected)
	}
}

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
	const core = "github.com/WonderForgeLabs/gooey"

	// A sibling is core's path plus a directory. Prefixing with the
	// slash is what keeps a future `github.com/WonderForgeLabs/gooeyfoo`
	// from matching.
	own := func(path string) bool {
		return path == core || strings.HasPrefix(path, core+"/")
	}

	mods := modulesIncludingTheRoot(t)
	if len(mods) == 0 {
		t.Fatal("no nested modules found; the walk is wrong, not the tree")
	}

	// A module that requires core at all must name a version that could be
	// served. `packs/*` require nothing of core and are simply skipped —
	// and `seen` is counted below, so a walk that stops finding requires
	// is visible rather than passing as "nothing to check". It carried a
	// separate `checked` counter incremented on the line above the
	// append, which is two names for one quantity and one early
	// `continue` away from disagreeing. Raised in review of #497.
	//
	// Every own-module require in the tree, so the skew check below can
	// compare them against each other rather than against a constant.
	var seen []ownRequire
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
			Require []struct {
				Path    string
				Version string
			}
		}
		if err := json.Unmarshal(body, &mf); err != nil {
			t.Fatalf("%s: parsing go mod edit -json: %v", dir, err)
		}

		for _, r := range mf.Require {
			if !own(r.Path) {
				continue
			}
			// THE ZERO PSEUDO-VERSION TOO. `go mod edit -require=X@v0.0.0`
			// on a module the workspace replaces writes the canonical
			// spelling `v0.0.0-00010101000000-000000000000`, which is the
			// same unservable revision wearing a pseudo-version's shape —
			// and apps/wysiwyg carried three of them while the plain
			// sentinel elsewhere carried the rest.
			if r.Version == "v0.0.0" || strings.HasPrefix(r.Version, "v0.0.0-00010101000000-") {
				t.Errorf("%s requires %s %s, which is neither a tag nor a "+
					"pseudo-version any proxy can serve: `go get` of this module "+
					"fails with \"unknown revision\" for everybody outside this "+
					"workspace. Point it at a published commit, and note that "+
					"`go mod edit -require` writes LITERALLY what you hand it: a "+
					"bare short hash stays a bare short hash and fails this test "+
					"again. Get a real pseudo-version with `GOWORK=off go list -m "+
					"-f '{{.Version}}' %s@$(git rev-parse origin/main)` (needs the "+
					"network; GOWORK=off is load-bearing — inside the workspace the "+
					"committed vendor/ forces -mod=vendor and the query is refused, "+
					"and -mod=mod is not allowed in workspace mode), or copy the "+
					"one the rest of the tree already names, or derive it with no "+
					"network at all: `TZ=UTC git -c core.abbrev=12 log -1 "+
					"--date=format-local:%%Y%%m%%d%%H%%M%%S --format='v0.0.0-%%cd-%%h' "+
					"origin/main` (TZ=UTC is load-bearing, and the form is exact "+
					"only while the module is untagged). "+
					"Keep any `replace` line, which is what makes local development "+
					"use the checkout.",
					dir, r.Path, r.Version, r.Path)
				continue
			}
			// Not a resolution check (no network here), just the shape: a
			// version the proxy could be asked for at all.
			if !strings.HasPrefix(r.Version, "v") {
				t.Errorf("%s requires %s %q, which is not a version", dir, r.Path, r.Version)
				continue
			}
			// APPENDED AFTER THE SHAPE CHECKS, not before. A sentinel
			// went into the skew population and was then rejected, so
			// one bad require produced two failures with two different
			// remedies — the sentinel error, and a skew group naming the
			// same line and telling the fixer to match a revision. The
			// len(seen) == 0 fatal below still fires correctly: a tree of
			// nothing but sentinels fails loudly on the sentinels.
			// Raised in review of #497.
			seen = append(seen, ownRequire{dir, r.Path, r.Version})
		}
	}

	if len(seen) == 0 {
		t.Fatalf("none of the %d nested modules were found to require %s or a "+
			"module under it; either the requires moved or this test stopped "+
			"reading them, and an empty check is not a passing one", len(mods), core)
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
	remedy := fmt.Sprintf(" — or, if the newest is a single module you just "+
		"added or bumped, pin THAT one back to %s, which is what the rest of "+
		"the tree names", g.rev)
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
		"v0.0.0-<stamp>-%s%s. This guard is about one revision across the tree, "+
		"not about which revision, so either direction closes it. A module "+
		"requiring an OLDER core than its siblings builds in this workspace "+
		"and fails for anyone who `go get`s it.",
		len(g.at), g.rev, newestRev, count, strings.Join(g.at, "\n\t"),
		newestRev, newestRev, newestRev, remedy)
}

type skewGroup struct {
	// rev is the 12-character COMMIT the group's requires name, and it
	// is what the remedy prints. version is one spelling of it, kept for
	// ordering and for the "names %s" half of the message.
	rev     string
	version string
	at      []string
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
		behind = append(behind, skewGroup{rev: rev, version: version[rev], at: byRev[rev]})
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
	i := strings.LastIndex(v, "-")
	if i < 0 || len(v)-i != 13 {
		return "", false
	}
	// AND HEX, because length alone is not the shape. v1.2.3-abcdefghijkl
	// is a legitimate prerelease tag whose last dash-part is twelve
	// characters, and it keyed its own bucket as though it were a
	// revision. Raised in review of #497.
	for _, c := range v[i+1:] {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return "", false
		}
	}
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
	if behind[0].version != old {
		t.Errorf("the group behind names %s, want %s", behind[0].version, old)
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
	if msg := skewMsg(threeBehind[0], threeRev, 2); !strings.Contains(msg, "pin THAT one back") {
		t.Errorf("at two revisions the pin-the-newcomer-back remedy is correct "+
			"and is gone:\n\t%s", msg)
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
// A SHALLOW CLONE IS SAID, NOT PASSED OVER. actions/checkout defaults to
// fetch-depth 1, so in CI the commit is usually absent and `cat-file -e`
// would report every pin as unpublished. `git rev-parse
// --is-shallow-repository` is the discriminator, and the honest
// behaviour is the one the tagged-require path above already takes:
// report what was skipped rather than pass silently. Raised in review of
// #497.
func TestEveryOwnModulePinNamesACommitThisRepositoryPublished(t *testing.T) {
	const core = "github.com/WonderForgeLabs/gooey"
	own := func(path string) bool {
		return path == core || strings.HasPrefix(path, core+"/")
	}

	git := func(args ...string) (string, error) {
		out, err := exec.Command("git", args...).Output()
		return strings.TrimSpace(string(out)), err
	}

	if shallow, err := git("rev-parse", "--is-shallow-repository"); err != nil {
		t.Skipf("git is not answering here (%v), so this check cannot run; the "+
			"shape and skew checks above still did", err)
	} else if shallow == "true" {
		t.Skipf("this is a SHALLOW clone (actions/checkout's default is " +
			"fetch-depth: 1), so the commits these pins name are not objects " +
			"here and their absence would say nothing. Run with a full clone " +
			"— fetch-depth: 0 — to check that every pin names a published " +
			"commit; the shape and skew checks above ran either way")
	}
	if _, err := git("rev-parse", "--verify", "origin/main"); err != nil {
		t.Skipf("no origin/main in this checkout (%v), so reachability cannot "+
			"be asked; the shape and skew checks above still ran", err)
	}

	// One entry per distinct revision, with a representative pin so the
	// failure names a file rather than a hash.
	at := map[string]string{}
	for _, dir := range modulesIncludingTheRoot(t) {
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
			if !own(r.Path) {
				continue
			}
			rev, ok := revisionOf(r.Version)
			if !ok {
				continue // a plain tag; the guard above reports those
			}
			if _, seen := at[rev]; !seen {
				at[rev] = dir + " → " + r.Path + " " + r.Version
			}
		}
	}
	if len(at) == 0 {
		t.Skip("no own-module require names a pseudo-version, so there is no " +
			"commit to look for — the guard above reports that state")
	}

	for rev, where := range at {
		if _, err := git("cat-file", "-e", rev+"^{commit}"); err != nil {
			t.Errorf("%s pins commit %s, which is not a commit in this "+
				"repository. `go get` of that module fails with \"unknown "+
				"revision\" for everybody outside this workspace, and every "+
				"check that compares pins against each other passes a "+
				"tree-wide typo — `sed` is how these get rewritten", where, rev)
			continue
		}
		if _, err := git("merge-base", "--is-ancestor", rev, "origin/main"); err != nil {
			t.Errorf("%s pins commit %s, which exists here but is NOT an "+
				"ancestor of origin/main — a branch commit, or one taken from "+
				"an unmerged worktree head. A proxy can only serve what the "+
				"published history contains", where, rev)
			continue
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
		if !strings.Contains(where, "-"+want+"-"+rev) {
			t.Errorf("%s names commit %s with a stamp that is not that commit's "+
				"committer date in UTC (%s). `go mod edit -require` writes "+
				"literally what it is handed, so a hand-built pseudo-version "+
				"can carry the wrong 14 digits and be refused by `go get` while "+
				"passing every shape check here", where, rev, want)
		}
	}
}

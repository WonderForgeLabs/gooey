package gooey

import (
	"encoding/json"
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
// That made every consumable module in the tree — imagefmt/svg, paint,
// mcp, grpc, handlers/* — impossible to `go get`, while all 25 modules
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

	mods := discoverModules(t)
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
	newest, behind, tagged := skewFrom(seen)
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
	newestRev, _ := revisionOf(newest)
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
		t.Errorf("%d requires name commit %s while the newest in the tree is %s "+
			"(%s) — one repository, one push, so two revisions is skew rather "+
			"than a choice. Behind:\n\t%s\nMove them up to commit %s, in each "+
			"module's own spelling of it — a pseudo-version off a tag reads "+
			"v0.1.1-0.<stamp>-%s and one off an untagged path reads "+
			"v0.0.0-<stamp>-%s. A module requiring an OLDER core than its "+
			"siblings builds in this workspace and fails for anyone who "+
			"`go get`s it.",
			len(g.at), g.rev, newestRev, newest, strings.Join(g.at, "\n\t"),
			newestRev, newestRev, newestRev)
	}
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
func skewFrom(seen []ownRequire) (newest string, behind []skewGroup, tagged []string) {
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
		return "", nil, tagged
	}
	var newestRev string
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
	return newest, behind, tagged
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
	if newest, behind, _ := skewFrom([]ownRequire{
		{"apps/introdeck", "github.com/WonderForgeLabs/gooey", newer},
		{"apps/introdeck", "github.com/WonderForgeLabs/gooey/paint", tagged},
	}); len(behind) != 0 {
		t.Errorf("a tagged module and an untagged one at the SAME commit report "+
			"skew (newest %s, behind %v) — the invariant is one revision, and a "+
			"per-subdirectory tag changes the string without changing the commit",
			newest, behind)
	}

	// ONE AHEAD, MANY BEHIND: the one that moved is the reference.
	newest, behind, _ := skewFrom([]ownRequire{
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

	// THE TAGGED SPELLING AT THE OLDER COMMIT, which is the arm that
	// discriminates. With no stamp laterThan falls back to a string
	// compare, and "v0.1.1-0.…" sorts ABOVE "v0.0.0-…" whatever the
	// dates say — so the tagged module would be named the reference and
	// the whole tree told to move backwards. Putting the tag on the
	// NEWER commit proves nothing: both rules agree there.
	const taggedOld = "v0.1.1-0.20260822101500-aaaaaaaaaaaa"
	if newest, behind, _ := skewFrom([]ownRequire{
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
	if _, behind, _ := skewFrom([]ownRequire{
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
	if newest, behind, skipped := skewFrom([]ownRequire{
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
	if _, behind, skipped := skewFrom(allTags); len(behind) != 0 || len(skipped) != 2 {
		t.Errorf("a tree of plain tags reports behind=%v skipped=%v; want no "+
			"skew and both skipped", behind, skipped)
	}
	if _, _, skipped := skewFrom(allTags); !comparedNothing(allTags, skipped) {
		t.Error("the compared-nothing guard does not fire on a tree whose every " +
			"require names a plain tag, so that tree passes having checked nothing")
	}
	// AND SORTED ON THIS PATH, which is where it was not. allTags is
	// deliberately given "mcp" before "grpc" — sorted the other way —
	// because the sort used to sit BELOW skewFrom's early return, so the
	// two paths that actually print this list (the caller's log and the
	// compared-nothing error) were the two it never reached. Raised in
	// review of #497.
	if _, _, skipped := skewFrom(allTags); !sort.StringsAreSorted(skipped) {
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
	if _, _, skipped := skewFrom(partly); comparedNothing(partly, skipped) {
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
	if got, behind, skipped := skewFrom(hexTag); got != "" || len(behind) != 0 ||
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

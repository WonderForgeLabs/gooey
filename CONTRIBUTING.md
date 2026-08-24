# Contributing to gooey

Welcome. If this is your first open-source contribution, it is a fine place to start — read the two sections below and ignore the rest until you need it.

## Claiming an issue

**Comment `/claim` on the issue. A bot assigns it to you within a minute.**

That is the whole convention. You do not need to ask permission, explain your plan, or wait for a maintainer.

Please do claim before you start writing code. Not to be formal about it — it is so nobody else spends their evening on the same fix. On 2026-08-23 two people independently fixed the same issue two hours apart, and neither had any way to know the other had started. That was our fault for having no mechanism, and this is the mechanism.

A few things worth knowing:

- **Claiming costs you nothing.** If life happens, say so and we will unassign it. There is no penalty, and it is much better than an issue sitting silently claimed. If a claim goes quiet for a couple of weeks we will ask before freeing it up.
- **If an issue is already claimed**, the bot will tell you immediately and point you at unclaimed ones — before you have written anything.
- **You do not have to finish what you claim.** A draft PR with a question in it is a real contribution. So is a comment saying "I tried this and got stuck here."

Issues labelled `good first issue` are ones we think are self-contained and have a clear finish line. `help wanted` means we would genuinely like someone to take it.

## Opening a pull request

- **Open it as a draft** if you want early eyes on it. Reviews run on drafts.
- **Say what you verified**, not just what you changed. "Ran `go test ./markup/...`" is worth more than "should be fine." If you found something by running it rather than reading it, say that too — it is the most useful sentence in most PR descriptions.
- **Tests that can fail.** A test that passes whether or not the fix is present is worse than no test, because it reads as coverage. If you add a guard, try deleting the fix and confirming the test goes red.
- **Small and focused beats complete.** One logical change per PR.

Do not worry about matching the house style perfectly on your first try. Review will tell you, kindly.

## What review looks like

An automated review runs on every PR and leaves a summary comment. It is thorough and it will find things — that is not a judgement of you or your code, and a review with findings is the normal case, not a bad outcome. Address them, or say why you disagree; disagreeing is allowed and sometimes right.

A human merges. Nothing merges on the bot's say-so alone.

## Comments and commit messages

Explain **why**, not what. The code says what it does; it cannot say what it was chosen over, or which failure it exists to prevent. A comment that says "keying on the file's presence, not the path shape, is what excludes X" is only useful if it is true — if you are not sure, run it and find out, then write down what you found.

## Local checks

```sh
go test ./...          # from the repo root, and from any nested module you touched
go vet ./...
gofmt -l .             # silence is success
```

Some modules are run with `-race` in CI; if you touched concurrency, run it locally too.

## Questions

Ask on the issue. "I do not understand what this is asking for" is a perfectly good comment and often means the issue is badly written, which is useful for us to know.

# Release

Consulted when branching, opening or merging a pull request, versioning, writing the changelog, and recording deploy evidence. Used during planning, quality gates, and closure.

## Defaults

Work lands on a branch and reaches the default branch through a reviewed pull request. A user-visible or contract change gets a changelog entry in the same change set. Closure evidence names the commit or release that carries the work.

## Team extensions

Branch model: feature branches merged to `main` by reviewed pull request (use `gh` outside the sandbox); hotfixes may publish from `release`. `make cut-release` is the guided release entrypoint and is never run as part of ordinary work; see [docs/RELEASING.md](../RELEASING.md). Every user-visible or contract change adds a line under `## [Unreleased]` in `CHANGELOG.md` in the same change set. Effort closure names the PR or merge commit that carries the work. Release notes collect evidence from completed efforts at the same Git revision; `scripts/release/CONTEXT.md` describes what the collector reads.

Effort closure ships with the work, not after the merge. Before opening a pull request for effort-driven work, check the effort against the branch. If everything in scope is delivered or covered by a recorded deviation, commit the closure pass on the branch, add the pull request number to the effort once it exists, and update the record before merging if review or CI changes what was delivered. If work remains, tell the user what is left before opening or merging, and leave the effort active. An effort planned across several pull requests closes in its last one; when the user signals they are wrapping up, for example by asking to merge or ship, say whether the effort still has open work. An effort whose last pull request merged without closure closes in the next related pull request, not in a closure-only one.

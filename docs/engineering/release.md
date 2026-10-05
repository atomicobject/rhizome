# Release

Consulted when branching, versioning, writing the changelog, and recording deploy evidence. Used during planning, quality gates, and closure.

## Defaults

Work lands on a branch and reaches the default branch through a reviewed pull request. A user-visible or contract change gets a changelog entry in the same change set. Closure evidence names the commit or release that carries the work.

## Team extensions

Branch model: feature branches merged to `main` by reviewed pull request (use `gh` outside the sandbox); hotfixes may publish from `release`. `make cut-release` is the guided release entrypoint and is never run as part of ordinary work; see [docs/RELEASING.md](../RELEASING.md). Every user-visible or contract change adds a line under `## [Unreleased]` in `CHANGELOG.md` in the same change set. Effort closure names the PR or merge commit that carries the work. Release notes collect evidence from completed efforts at the same Git revision; `scripts/release/CONTEXT.md` describes what the collector reads.

---
type: TechnicalSpec
summary: "Defines force-include semantics for gitignored subtrees (e.g. wrapper-repo submodules) so they index while still honoring their own nested ignore rules, plus candidate detection and diagnostics."
id: SPEC-0064
spec-status: active
last-updated: 2026-08-05
aliases:
  - SPEC-0064
  - ignored-subtree-inclusion
---

# Ignored-subtree inclusion

## Summary

Wrapper repositories often gitignore a vendored checkout or git submodule at the root (for example a `app/` submodule listed in the root `.gitignore`) while that subtree is the code the team actually works on. Today the unified ignore matcher makes such subtrees invisible to note discovery, init code detection, code indexing, and the watcher, and the only escape hatch — hand-authored negation patterns in `.rhizome/ignore` — is undocumented as a contract, unverified by tests, and invisible to `rzm init`.

This spec promotes ignored-subtree inclusion to a first-class, verified contract: a subtree can be re-included over root-level gitignore exclusions while the subtree's own `.gitignore` chain and built-in defaults still apply, every scanner honors the same decision, init can discover and offer candidate subtrees, and a diagnostic explains any path's inclusion decision.

## Goals

- One durable, committed representation of "index this subtree even though the root gitignores it".
- Re-included subtrees still honor their own nested `.gitignore` files, built-in default ignores, and later `.rhizome/ignore` rules. Config excludes continue to select ordinary notes/code; exact system `CONTEXT.md` is the documented exception.
- All scanners (markdown discovery, init detection, indexing pipeline, watcher) agree on inclusion decisions for the same path.
- Ignored nested repos that look like real code are discoverable as inclusion candidates without walking huge ignored trees.
- Inclusion decisions are explainable: a user or agent can ask why a path is in or out and get the deciding layer and pattern.

## Non-Goals

- Indexing nested repos as separate vaults or separate Rhizome configs; the wrapper repo's config remains the single root.
- Bypassing built-in default ignores (`.git/`, `node_modules/`, `vendor/`, caches) inside re-included subtrees.
- Changing the ignore-layer precedence model (defaults → gitignore → `.rhizome/ignore` → config excludes).
- Submodule lifecycle management (cloning, updating, or validating git submodules).

## Requirements

### Inclusion representation

- Negation patterns in `.rhizome/ignore` (for example `!app/`) MUST be the durable storage for subtree inclusion; the file is committed, so the decision is shared by every contributor and CI.
- Tooling that writes inclusions (init, future commands) MUST append them under a labeled comment block (for example `# rhizome: included subtrees`) so humans can find and prune them, but hand-authored negations anywhere in the file MUST behave identically.
- A subtree negation MUST re-include the directory and its descendants with respect to root-level gitignore rules that excluded the subtree, including patterns of the shapes `sub/`, `/sub`, and `sub/**`.

### Layer semantics

- Within a re-included subtree, built-in default ignore patterns MUST still apply.
- Within a re-included subtree, the subtree's own `.gitignore` (and deeper nested `.gitignore` files) MUST still apply: lazily loading a directory's `.gitignore` MUST happen whenever the directory itself is included after all layers are evaluated, not merely when no earlier layer ignored it.
- Later `.rhizome/ignore` rules MUST still be able to re-exclude every path inside a re-included subtree. Config excludes MUST still re-exclude ordinary candidates, but MUST NOT suppress exact `CONTEXT.md`; `.rhizome/ignore` is the explicit hard boundary for system context.
- The decision function MUST be identical across markdown discovery, init layout/code detection, the code indexing pipeline, and the file watcher; the watcher MUST begin watching a subtree when an inclusion is added and stop when it is removed (existing ignore-file reload behavior).

### Candidate detection

- Layout detection MUST be able to enumerate "ignored nested repo" candidates: directories excluded by gitignore layers (not by defaults or explicit `.rhizome/ignore` rules) whose root contains a `.git` entry or a recognized language marker (`go.mod`, `package.json`, `pyproject.toml`/`setup.py`, `*.csproj`, `*.sln`).
- Candidate probing MUST be bounded: inspect only the candidate directory's top level (plus one marker-probe level), never a full walk of the ignored tree.
- Candidate detection MUST NOT auto-include anything; inclusion is an explicit user decision surfaced by init ([[init-starter-workflow]]) or authored by hand.

### Diagnostics

- Rhizome MUST provide a diagnostic that reports, for a given path: whether it would be indexed, and which layer and pattern produced the decision (default, which `.gitignore` file and line, which `.rhizome/ignore` pattern, or config exclude).
- The diagnostic MUST ship as `rzm index --explain <path>` (CLI surface; no new subcommand and no new MCP tool in this slice).
- The diagnostic MUST be usable for both note paths and code paths.

### Test obligations

- An integration fixture MUST model the wrapper-repo shape: root `.gitignore` excluding a subtree, the subtree containing its own `.gitignore` that excludes build output, plus indexable notes and code.
- Tests MUST cover: subtree excluded by default; included via negation; nested `.gitignore` still excluding build output inside the included subtree; defaults still excluding `node_modules/` inside the included subtree; candidate detection enumerating the subtree; and scanner agreement (discovery, detection, indexing pipeline) on the same fixture.

## User Stories

### US1 - Index a gitignored submodule while its own ignore rules still apply
- id:: ^SPEC-0064-US1
- summary:: A maintainer of a wrapper repo includes a root-gitignored submodule in indexing and gets the subtree's notes and code indexed while the subtree's own .gitignore and built-in defaults keep excluding junk.
- status:: satisfied

#### Acceptance Criteria

- Adding a labeled negation for the subtree to `.rhizome/ignore` causes its markdown and code to appear in discovery, detection, and indexing without any other config change. ^SPEC-0064-US1-AC1
- The subtree's own `.gitignore` exclusions (for example `dist/`) remain excluded inside the included subtree. ^SPEC-0064-US1-AC2
- Built-in defaults (for example `node_modules/`) remain excluded inside the included subtree. ^SPEC-0064-US1-AC3
- Removing the negation returns the subtree to excluded with the watcher and caches resyncing. ^SPEC-0064-US1-AC4

### US2 - Explain why a path is or is not indexed
- id:: ^SPEC-0064-US2
- summary:: A user or agent debugging missing files asks Rhizome why a specific path is excluded and gets the deciding layer and pattern instead of guessing across four ignore sources.
- status:: satisfied

#### Acceptance Criteria

- The diagnostic names the deciding layer for an excluded path, including the source file and pattern when the layer is a `.gitignore` or `.rhizome/ignore` rule. ^SPEC-0064-US2-AC1
- The diagnostic confirms inclusion for a path re-included by a subtree negation, attributing it to the negation pattern. ^SPEC-0064-US2-AC2
- The diagnostic works for paths that do not exist yet (pattern evaluation only), so users can test rules before moving files. ^SPEC-0064-US2-AC3

## Open Questions

None currently; the diagnostic surface decision (`rzm index --explain <path>`) was resolved with Colthorp on 2026-06-10.

## Documentation plan

- Update `docs/reference/guides/Ignore behavior.md` and `Ignore + exclude rules.md` with the subtree-inclusion contract and the labeled-block convention.
- README section on wrapper repos / monorepos with gitignored submodules.
- `rzm init` help text and MCP agent guide pointer if the diagnostic lands in MCP later (not in this slice).

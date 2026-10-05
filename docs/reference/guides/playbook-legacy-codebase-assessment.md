---
type: ReferenceDoc
summary: "Skill-forward playbook for running a Rhizome-powered legacy codebase assessment — orient the engineer to the three-skill engagement chain (assess → flag → package), with CLI limited to `rzm init` and `rzm index`."
reference-kind: guide
status: active
last-verified: 2026-10-01
---

# Playbook: Legacy Codebase Assessment

This playbook orients an AO engineer to the Rhizome-powered engagement workflow. Most of the work runs through agent skills; you only type two CLI commands directly. Agents invoke the rest as part of broader tasks.

**Time to first results:** ~15 minutes for a medium-sized repo (once indexed).

---

## Engagement workflow at a glance

A typical legacy-codebase engagement runs through three skills in order:

```
legacy-codebase-assessor   →   assumption-tracker   →   client-harness-builder
       (assess)                  (flag + synthesize)         (package)
```

| Skill | When | What it does |
|---|---|---|
| `legacy-codebase-assessor` | After indexing, at engagement kickoff | Runs four assessment reports, diagnoses scope, writes `docs/assessment/findings-summary.md` with priority files + suggested next steps |
| `assumption-tracker` | During the documentation pass that follows assessment | Flags uncertain claims (`flag` mode) and synthesizes them into client kickoff questions (`synthesize` mode) |
| `client-harness-builder` | At engagement close | Packages validated documentation into a Rhizome-free Claude Code harness the client can use standalone |

The CLI commands those skills run live inside their SKILL.md files. You don't need to memorize them.

---

## Prerequisites

- Rhizome installed (`rzm --version` to confirm; [install guide](getting-started.md#install))
- Access to the codebase repo (local clone, read access)
- Optional but recommended: a Voyage AI key, or an OpenAI key, for semantic search (unlocks the `code_similarity` report; the other three reports run without embeddings)

---

## Step 1 — Initialize Rhizome

From the repo root, run `rzm init`. It shows what Rhizome found (docs, code languages, suggested skips, agents, and semantic search), then asks:

- **Workflow**: Agentic Engineering if you plan to produce a structured assessment vault (it includes the `action-items` layer that `assumption-tracker` uses), or Search and agent guidance only if you just want reports. To install only `action-items` for the `assumption-tracker` phase, run `rzm init --workflow action-items` instead.
- **Semantic search key**: asked only when no key is available. A Voyage AI key enables the `code_similarity` report; press Enter to set it up later.

Confirm with `Set up Rhizome? [Y/n/e to edit]`. Init writes the configuration and the `AGENTS.md` guidance block the assessment agent uses. When an agent runs init without a terminal, it uses the recommendations; pass `--workflow agentic-engineering` or `--workflow none` to choose.

Code indexing covers the whole repository, and each file's language comes from its extension, so you do not need to list source folders. Check the **Skip** row on the findings screen: legacy repositories often carry checked-in dependencies, generated code, or minified bundles that fill search results with code the team does not maintain. Init writes confirmed skips to `.rhizome/ignore`; add your own lines there for anything it missed. To limit code indexing to specific folders, see [Advanced configuration tuning](<Advanced configuration tuning.md>).

Supported languages: Go, TypeScript, JavaScript, Python, C#, PHP, Java, C/C++, Rust, Ruby.

---

## Step 2 — Build the Index

```bash
rzm index
```

This indexes notes, scans code for coderefs (wikilinks and `@NotePath` mentions in comments/docstrings), and builds the unified graph. Plain indexing uses configured, enabled embedding providers for semantic evidence, including the `code_similarity` report. Append `--rebuild` to force a clean rebuild after config changes.

**Expected output:** progress lines showing notes indexed, code files scanned, and graph written. If zero code files were indexed, run `rzm index --explain <path>` on a source file to see which rule or setting keeps it out.

---

## Step 3 — Run the assessor skill

Now tell your agent:

> *Invoke the `legacy-codebase-assessor` skill against this codebase.*

The skill handles everything from here:

1. Diagnoses scope (warns if any configured language has zero indexed files, or if a single language dominates a multi-language config, or if the assessment path excludes most of the indexed corpus — surfaced from the wp-tst engagement)
2. Runs `doc_coverage`, `hotspots`, `complexity`, and (when embeddings are available) `code_similarity` against a single session
3. Synthesizes the JSON outputs into `docs/assessment/findings-summary.md` — executive summary, top-5 documentation priority files, hotspot × complexity intersections, doc-coverage gaps, suggested starting subsystem
4. Names the next skill in the chain so the documentation pass starts without re-deriving the priority list

The skill's two modes:

- **`assess`** (default) — for the initial assessment of a freshly-indexed repo
- **`refresh`** — re-run on a vault that already has `findings-summary.md`; reuses the prior path/scope and flags drift since the last assessment

The findings note seeds candidate `assumption-tracker` types per priority file so the documentation pass that follows knows what kinds of uncertainty to expect — `business-logic`, `schema`, `integration`, or `process`.

### What "good" looks like in the findings note

| Signal | Healthy range | Action if low |
|---|---|---|
| `coveragePercent` | >30% of significant files documented | Identify top-hotspot files as first documentation targets |
| Hotspot → doc overlap | Top 10 hotspots have at least one coderef or anchor | Add wikilink references to top hotspot file comments |
| Complexity outliers | No single function >5x the median complexity | Flag for refactor; add CONTEXT.md explaining the invariants |
| Vault health orphans | <5% of notes are dead-ends or orphans | Link orphan notes into relevant areas or delete them |

These are the metric thresholds the skill checks against when writing the executive summary. If the numbers look off, the skill flags it — you don't need to interpret raw JSON.

---

## Step 4 — Flag assumptions during documentation

After assessment, you (or an agent) start documenting the priority files surfaced in `findings-summary.md`. Whenever you hit an uncertain claim — an ambiguous field name, an opaque business rule, a magic-number conditional — flag it immediately.

Tell your agent:

> *Use `assumption-tracker --mode flag` while documenting `<file or subsystem>`.*

The skill writes typed `#assumption/<type>` ActionItems near the relevant code (not in a catch-all file) using the four-type vocabulary the assessor pre-suggested:

- `#assumption/business-logic` — unknown business rule or process decision
- `#assumption/schema` — uncertain field meaning or implicit relationship
- `#assumption/integration` — unclear integration point between modules
- `#assumption/process` — uncertain operational workflow

The assessor's findings note already names likely types per priority file, so flag mode starts with concrete expectations rather than a blank slate.

---

## Step 5 — Synthesize meeting questions

Before a client kickoff or clarification session:

> *Use `assumption-tracker --mode synthesize` to draft kickoff questions.*

The skill queries all open assumptions, groups by subsystem and type, drafts specific questions, and outputs to `docs/assessment/kickoff-questions.md`. Order goes business-logic + integration first (highest business risk), then schema + process.

---

## Step 6 — Package for client handoff

When the documentation has matured and assumptions have been resolved:

> *Use `client-harness-builder` to package the docs into a client harness.*

The skill produces a `client-harness/` directory containing `.claude/commands/` files, `CLAUDE.md`/`AGENTS.md` entry points, and a maintenance runbook. No `rzm` references appear in the output — the client doesn't need Rhizome installed.

---

## Common situations

**Index returns zero code files:**
Run `rzm index --explain <path>` on a source file. It names the ignore rule or code setting that keeps the file out. If `.rhizome/config.yml` lists `roots:` folders that miss your source, remove them so code indexing covers the whole repository, or run `rzm init --check` to see the fix init proposes. Then re-run Step 2.

**Assessor flags scope problems:**
The `legacy-codebase-assessor` skill diagnoses scope before running reports. If it warns about a configured language having zero indexed files, that's the skill telling you the indexer didn't walk where you expected. Common causes: `roots:` folders in `.rhizome/config.yml` that miss the source, or ignore rules masking the directory. `rzm index --explain <path>` names the cause. Fix it and re-run Step 2 with `--rebuild`.

**Code anchors return zero matches:**
Code anchor notes need a `code-anchors:` frontmatter block pointing at real paths. Anchors are opt-in. Consider adding them to CONTEXT.md files as a first step.

**Embedding provider unavailable:**
`doc_coverage`, `hotspots`, and `complexity` don't require embeddings. `code_similarity` does — the assessor skips it with a one-line warning in the findings note's preamble and continues with the other three reports.

**Hotspot list looks wrong (too many test files):**
Add test file patterns to `.rhizome/ignore` to exclude them from authority scoring:

```
# .rhizome/ignore
**/*_test.go
**/*.test.ts
**/test/**
```

Then re-run Step 2 with `--rebuild` and ask the assessor for a fresh `mode=refresh`.

---

## Inheriting a Configured Assessment Repo

If you're picking up an assessment that was already set up by someone else:

1. Re-run Step 2 to ensure the index reflects the current codebase state.
2. Tell your agent: *"Invoke `legacy-codebase-assessor` in refresh mode."*

The skill reads the existing `docs/assessment/findings-summary.md`, recovers the original assessment scope from its frontmatter, and produces a refreshed assessment with drift annotations ("New since last assessment: …" / "Removed since last assessment: …").

If you also need to inspect the prior open assumptions: *"Run `assumption-tracker --mode synthesize` to see all open assumptions."*

---

## Next Steps

- [How Rhizome Works](how-rhizome-works.md) — understand why the retrieval and reporting work the way they do
- [Playbook: Agentic Harness Setup](playbook-agentic-harness-setup.md) — set up structured delivery on top of the assessed codebase

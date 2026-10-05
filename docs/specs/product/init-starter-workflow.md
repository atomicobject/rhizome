---
type: ProductSpec
summary: "Defines the user-facing rzm init workflow: a one-screen first run, maintenance-first reruns, a four-section settings menu, single-pass credential onboarding with Voyage AI as the default, one ownership rule for generated files, ignored-subtree inclusion, agentic-engineering starter selection and migration, and starter ejection."
id: SPEC-0038
spec-status: active
last-updated: 2026-10-01
aliases:
  - SPEC-0038
  - init-starter-workflow
---

# Init Starter Workflow

## Summary

`rzm init` should make a repository agent-ready without making the user think about configuration. On a fresh repository it shows what Rhizome found (docs, code, agents, search provider) in plain words, asks only what it cannot decide (the workflow, and a semantic search key when none is available), and writes everything after one confirmation. On a configured repository it is a maintenance command first: it summarizes the current setup, lists what changed since the last run (newly detected code, Rhizome updates, locally edited files), and applies safe changes with one confirmation. Settings are one keystroke away and contain four product-level sections. Runtime settings live in `.rhizome/config.yml`, starter adoption and management state in `.rhizome/workflows.yml`, and the record of the files Rhizome generated in `.rhizome/generated-files.yml`. The detailed agent-surface contract lives in [[agent-surface-integration-modes]].

Every generated file follows one ownership rule: Rhizome creates new files without asking, updates files that still match what it last wrote without asking, and asks only about files someone edited. Team-owned starter docs are create-only. Expert tuning knobs live in documented config fields, not prompts.

## Goals

- make first-run setup a single reviewable screen with at most three questions and quiet, grouped output
- recommend Voyage AI for semantic search by default, with OpenAI and Ollama as alternatives
- make the agentic-engineering starter available as Rhizome's repository harness for Agentic Engineering, offered as one of three plain workflow choices
- make reruns a maintenance workflow that names what changed, including code added after setup, and applies safe changes with one confirmation
- ask about a generated file only when someone edited it, and never ask twice about the same declined version
- keep the settings menu to four product decisions; relocate expert tuning to documented config fields
- ask for each credential at most once per run, and never re-ask for a key Rhizome already has
- finish in one run: one confirmation in a terminal, none without one; give scripts and agents a small flag set plus `--check` to preview without writing
- let maintainers eject a whole workflow starter from Rhizome management while keeping installed files in place

For guidance on choosing the right starter for a given use case, see [[choosing-your-starter]] (`docs/reference/guides/choosing-your-starter.md`).

## Non-Goals

- replacing each agent tool's full configuration system
- creating MCP config files during init
- overwriting unmanaged repo instructions, prompts, commands, or skills
- preserving backward compatibility with removed standalone `RHIZOME.md` guidance files or removed init flags; removed flags fail with a message naming their replacement
- making starter docs the only way to use Rhizome in a repo
- exposing every config field interactively; the menu is a curated subset, the YAML file is the full surface
- a TUI rewrite; the flow stays a plain prompt/print interaction
- hunk-level review or automatic three-way merging of edited files; an edited file is updated whole, kept whole, or compared through a shown diff

## User Stories

### US1 - Set up Rhizome from one screen that shows what it found and asks only what it cannot decide
- id:: ^SPEC-0038-US1
- summary:: Run init once, see what Rhizome found and will do in plain words, answer at most three questions, and leave with a working setup and a short summary of what was written.
- status:: ready

The first run should feel inspectable rather than magical, and it should not ask the user to understand Rhizome's configuration to get there. Detection and opinionated defaults decide everything except the workflow and a missing search key.

#### Acceptance Criteria

- The first screen reports findings before any question. ^SPEC-0038-US1-AC1
  - It names the note count and where the notes are, the detected code languages and file count, the agents Rhizome will set up, and the semantic search provider.
  - Notes cover all Markdown and code covers every supported language: first runs write no folder limits, so content added later is indexed, and `.rhizome/ignore` is the only way content stays out.
  - It uses plain words; it does not print config keys, glob patterns, or `auto|on|off` modes.
- The first run asks at most three questions before writing. ^SPEC-0038-US1-AC2
  - Workflow: Agentic Engineering (recommended, default), Agentic Engineering with domain modeling, or search and agent guidance only.
  - Semantic search key: asked only when no key for Voyage AI resolves from the environment, the global CLI config, or the Atomic Object team key. Enter defers setup; `other` offers OpenAI, Ollama, or turning semantic search off. The key is saved immediately.
  - When the binary bundles Atomic Object team keys, the prompt leads with the Atomic Object Rhizome key, which unlocks Voyage AI and every other bundled provider, and also accepts a Voyage API key, recognizing which was pasted. When no team keys are bundled, the prompt asks only for a Voyage API key and never mentions the Atomic Object key.
  - Confirmation: `Y` writes, `n` exits without writing, `e` opens the settings menu with the recommendations loaded.
  - A detected ignored nested repository adds one include question before confirmation, per [[ignored-subtree-inclusion]].
- Agents are inferred rather than asked. ^SPEC-0038-US1-AC3
  - Claude Code, Codex, and Cursor are enabled when the repository has their markers or the tool's command is on `PATH`.
  - Shared `AGENTS.md` and `.agents/skills` are enabled whenever any agent is enabled, per [[agent-surface-integration-modes]].
- Writing is quiet and complete. ^SPEC-0038-US1-AC4
  - New files are created without per-file prompts.
  - The summary groups writes by purpose (config, agent guidance, skills, workflow docs, saved key) with project-relative paths, then offers to build the search index (default yes) and names the next steps: commit, and `rzm index` when the index was not built.
  - A rerun in a terminal that applied changes offers to update the index the same way. Runs without a terminal never index; their Next block lists `rzm index`.
  - Missing-key and readiness notes appear once, in the summary.
- Non-interactive runs express the same choices through options. ^SPEC-0038-US1-AC5
  - Without a terminal, `rzm init` uses the recommendations, writes, and prints the same summary of what it did.
- Semantic search turns on only with a usable key. ^SPEC-0038-US1-AC7
  - When no key resolves and none is pasted, semantic search stays off (no semantic search section is written) and init prints one line saying how to turn it on, so `rzm index` keeps working without a key.
  - A later rerun whose change list finds a usable key for Voyage AI offers to turn semantic search on.
  - Turning semantic search off on purpose (`--search off` or the settings menu) is recorded, and reruns do not offer to turn it back on.
  - `--check` prints the findings, the recommended choices, and every file and setting init would write, writes nothing, and exits with status 1 when anything would change.
  - `--workflow`, `--agents`, and `--search` choose the workflow, agents, and provider without prompting.
- Generated or rewritten `.rhizome/config.yml` content uses deterministic YAML formatting with two-space indentation across the whole file so reruns do not produce invalid YAML or indentation-only diffs. ^SPEC-0038-US1-AC6

### US2 - Select the agentic-engineering starter and receive a coherent workflow bundle
- id:: ^SPEC-0038-US2
- summary:: Select the agentic-engineering starter and receive a coherent workflow bundle with concise team extension docs, managed guidance, ontology assets, and workflow skills.
- status:: ready

The starter is the Rhizome repository harness for Agentic Engineering. It should install as one understandable workflow surface without implying that every activity is specification work or that adopting the starter defines the team's entire engineering process.

#### Acceptance Criteria

- The selected starter writes its concise extension docs and ontology assets from the starter template tree. ^SPEC-0038-US2-AC1
  - Selecting `agentic-engineering` installs team-editable plain Markdown under `docs/engineering/` factored by recurring engineering concern rather than by workflow phase: an index plus testing policy, quality gates, documentation, review and approval, architecture, and release. Several phases read the same concern document.
  - The initial extension docs are short, readable process defaults rather than exhaustive manuals or structured-data formats; after installation, the repository owns their content.
  - The starter ontology schemas are installed from `pkg/app/cli/init/templates/starters/agentic-engineering/rhizome/ontology/*.graphql` while preserving domain-accurate filenames such as `spec-driven.graphql`.
  - Scaffold output excludes starter skill directories, managed agent-doc templates, and starter ontology schemas because those families have their own installation rules.
- The selected starter adds a template-specific managed block to `AGENTS.md` and `CLAUDE.md` when those surfaces are enabled. ^SPEC-0038-US2-AC2
  - Enabled agent docs receive one core Rhizome managed block and one starter managed block per active template.
  - The starter block is keyed by the normalized template name so reruns can replace the block instead of appending duplicates.
  - User-authored prose outside managed fences remains byte-preserved except for normal diff-updater acceptance behavior.
- The selected starter installs starter-only skills alongside the core Rhizome skills without name collisions. ^SPEC-0038-US2-AC3
  - Core skills install independently of workflow starter choice.
  - Starter skills are loaded only from the selected starter's `skills/` tree.
  - Any starter skill name that collides with a core or already selected starter skill fails the run before the colliding skill is installed.
  - A lean `agentic-engineering` router owns shared workflow resources and takes the phase as its argument; only distinct workflows (`foundation-review`, `ingest-transcript`) remain separate skills, and no skill imports sibling files.
  - Development, review, debugging, and closure guidance loads only for the current phase; the closure path does not preload every shared resource.


- A clear local task completes proportionally. ^SPEC-0038-US2-AC4
  - Implementation and the repository's applicable checks do not require a new spec, effort, effort-closure query, or documentation artifact when the actual change does not warrant them.
  - Escalation follows the repository's effort-driven risk criteria; starter defaults do not override local engineering policy.
- Approved work retains its authority across phases and sessions. ^SPEC-0038-US2-AC5
  - An approved plan authorizes covered steps, routine implementation choices, and in-scope review fixes. Phase transitions and fresh sessions do not require renewed approval.
  - Missing authority, conflicting scope, or a material decision outside existing authority holds only dependent work; independent authorized work continues.
- Effort retrieval exposes the evidence needed to resume and close truthfully. ^SPEC-0038-US2-AC6
  - Execution and closure recipes return approval, frozen scope, plan, execution notes, actual delivery, deviations, and the closure checklist. Blank approval remains a meaningful pending state.
  - A fresh agent verifies current revision and unfinished work before continuing and preserves complete or archived efforts as history.
- Phase guidance retrieves and verifies proportionally. ^SPEC-0038-US2-AC7
  - The starter composes with base Rhizome mechanics and loads relevant local policy and phase context; it reuses current results and refreshes evidence when affected inputs change.
  - Missing, partial, or stale results stay explicit evidence gaps rather than becoming empty results or successful checks.
- Durable maintenance and handoff preserve useful delivery knowledge. ^SPEC-0038-US2-AC8
  - Delivery updates the smallest durable home of changed reusable knowledge; effort-local rationale remains in execution notes. No durable knowledge change requires no additional document.
  - Handoff records revision, verified outcomes, remaining work, and unresolved decisions in the existing effort where one exists, without implying completion or merge authority.
- Distinct judgment and provenance workflows keep their boundaries. ^SPEC-0038-US2-AC9
  - Foundation review presents only unresolved formative decisions requested by the plan, and ingestion preserves provenance and uncertain meaning while honoring authorized writes.
  - Ordinary review feedback stays within implementation or alignment unless it changes intended behavior or scope.

### US3 - Rerun init to see what changed and apply safe maintenance with one confirmation
- id:: ^SPEC-0038-US3
- summary:: Rerun init to see the current setup and what changed since the last run, apply safe updates with one confirmation, and decide only about files someone edited.
- status:: ready

Reruns are a maintenance workflow, not a reset button. Rhizome-owned files that nobody edited refresh without questions; user-owned prose, team-owned starter docs, and locally edited Rhizome files are never overwritten without a decision.

#### Acceptance Criteria

- Managed Rhizome and starter blocks refresh in place while prose outside managed fences is preserved. ^SPEC-0038-US3-AC1
  - Reruns remove legacy Rhizome fences and legacy standalone redirects before rendering the current managed blocks.
  - The rendered agent doc contains at most one core Rhizome block and at most one block for each active workflow template.
  - Existing content outside managed fences remains byte-identical.
- Existing starter docs are team-owned and not rewritten unless a doc refresh is explicitly requested. ^SPEC-0038-US3-AC2
  - Starter scaffold files under `docs/` are create-only during ordinary reruns.
  - Init records each starter doc's hash when it creates the doc, so `--refresh-docs` can apply the ownership rule in AC4: a doc whose content matches neither the recorded hash nor the current template is treated as edited.
- A rerun starts with a status summary and a change list. ^SPEC-0038-US3-AC3
  - The summary names the current docs, code languages, agents, search provider, and workflow in plain words.
  - The change list names proposed skips, Rhizome-owned files with updates, Rhizome-owned files with updates that someone edited, and missing Rhizome-owned files.
  - When config limits notes or code to folders and Markdown or code sits outside them, or config leaves code indexing off, a rerun suggests removing the limits or turning code on. Limits may be deliberate, so these are suggestions.
  - With an empty change list, init says everything is up to date and offers the settings menu.
  - `Y` applies the listed changes, `n` exits without writing, `s` opens the settings menu.
  - Without a terminal, `rzm init` applies the change list and prints what it did; nobody has to run it twice.
  - `--check` prints the same summary and change list, writes nothing, and exits with status 1 when the list is not empty.
  - Files someone edited, and files Rhizome cannot tell were edited, need a person's decision. In a terminal they join the change list and are asked about after `Y`. Without a terminal, init lists them under "Needs a decision" and keeps them; `--check` lists them without counting them, so `rzm init` followed by `rzm init --check` passes in CI.
  - Changes that add a capability nobody chose are suggestions: turning on semantic search because a key is now available, and an addon a workflow now turns on by default. They are listed under "Suggestions" in every mode. In a terminal `Y` applies them with the changes. Without a terminal, init leaves them unless `--accept-suggestions` is given, which an agent uses after the person agrees; `--check` lists them and counts them only with `--accept-suggestions`.
- Every Rhizome-owned file follows one ownership rule. ^SPEC-0038-US3-AC4
  - Rhizome-owned files are managed agent-doc blocks, core and starter skills, harness command files, starter ontology schemas, query recipes, and views.
  - A missing file is created without asking.
  - A file whose content matches the hash Rhizome recorded when it last wrote it is updated without asking.
  - A file someone edited is shown with three choices: take the update, keep my version (the default), or show the diff. Keeping records the declined version; init asks again only when a newer version ships.
  - Copies of one skill in several agent folders (for example `.agents/skills` and `.claude/skills`) are decided together with one question.
  - Without a terminal, init updates unedited files, keeps edited files, and lists the kept files with a note that running `rzm init` in a terminal offers the update.
- Ownership records survive upgrades without nagging. ^SPEC-0038-US3-AC5
  - Rhizome records written hashes and declined versions in the tracked `.rhizome/generated-files.yml`. The file holds nothing else.
  - On the first run without records, files that already match the current version are recorded silently, and files that differ are grouped into one question (update all, keep all, or review each). Managed blocks inside `AGENTS.md` and `CLAUDE.md` are not part of that question; their fences already mark them as Rhizome's. Without a terminal, init keeps them and lists them.
  - `.rhizome/template-rejections.yml` is no longer read or written; the first run under this contract deletes it after recording what it can.

### US4 - Navigate a settings menu that only contains decisions I actually need to make
- id:: ^SPEC-0038-US4
- summary:: A maintainer changing setup sees four plainly named product decisions instead of configuration sections and maintenance actions.
- status:: ready

The menu is the curated surface; `.rhizome/config.yml` is the complete one. Anything a maintainer would only change while debugging retrieval quality belongs in documented config, not in the interactive flow.

#### Acceptance Criteria

- The settings menu contains exactly four sections: what gets indexed, semantic search, agents, and workflow. ^SPEC-0038-US4-AC1
  - What gets indexed offers: skip a folder or file, stop skipping one, and include a folder Git ignores; when config has folder limits, it also offers to remove them. Folder limits are otherwise a config field documented with the expert settings.
  - Semantic search offers Voyage AI (recommended), OpenAI, Ollama, or off, and asks for a key only when the chosen provider has none. Notes and code share one provider; code embeddings follow code indexing.
  - Agents is a checklist of Claude Code and Cursor, plus an option to stop managing agent files. Codex has no row: it reads `AGENTS.md` and `.agents/skills`, which are written for every agent; `--agents codex` still selects the shared files alone.
  - Workflow offers the three first-run choices.
- Expert fields are not prompted anywhere in the interactive flow. ^SPEC-0038-US4-AC2
  - Link style, note include/exclude globs, per-language code roots, coderef scan/ignore globs, separate note and code embedding switches, models, endpoints, index path, compression, file-context packing, graph tuning, starter family update policy, and starter ejection remain config fields or flags covered by a reference guide. The menu prints one pointer to that guide.
- Each menu item shows a one-line plain-language summary without config keys, globs, or `auto|on|off` modes. ^SPEC-0038-US4-AC3
- Leaving the menu returns to the confirmation that will write the changes; nothing is written from inside a section. ^SPEC-0038-US4-AC4

### US5 - Provide each credential at most once
- id:: ^SPEC-0038-US5
- summary:: A user setting up semantic features pastes the Atomic Object Rhizome key (or a provider key) exactly once per run, and Rhizome never asks again for a key it already has or that the user already declined.
- status:: ready

#### Acceptance Criteria

- Init collects credential needs across every enabled feature (note embeddings, code embeddings, compression) and runs one consolidated credential pass; enabling a feature later in the same run consults what the pass already collected instead of prompting fresh. ^SPEC-0038-US5-AC1
- A key pasted at any prompt is persisted to the global CLI config immediately, so no later prompt in the same run or any future command asks for it again. ^SPEC-0038-US5-AC2
- The Atomic Object Rhizome key satisfies all team-covered providers at once; after it is provided, no per-provider prompt for a covered provider appears. Prompts mention the Atomic Object key only when the running binary bundles team keys, and in that case suggest it first. ^SPEC-0038-US5-AC3
- Declining ("skip for now") is persisted per credential; later commands such as `rzm index` surface a one-line hint about the missing key rather than re-prompting, unless the user explicitly opts back in (for example via the init menu or providing the env var). ^SPEC-0038-US5-AC4
- In a binary that bundles team keys, one key prompt accepts either the Atomic Object Rhizome key or the selected provider's key and recognizes which was pasted. ^SPEC-0038-US5-AC5

### US6 - Include a gitignored submodule from inside init
- id:: ^SPEC-0038-US6
- summary:: A maintainer of a wrapper repo whose real code lives in a root-gitignored submodule gets that subtree detected, offered, and included during init without hand-editing ignore files.
- status:: ready

#### Acceptance Criteria

- First-run detection lists ignored nested repo candidates (per [[ignored-subtree-inclusion]]) in the recommended setup, each with an include/exclude choice; accepted candidates are written as labeled `.rhizome/ignore` negations before code detection finalizes. ^SPEC-0038-US6-AC1
- After accepting a candidate, code and note detection re-run over the included subtree so language roots and the indexing preview reflect it in the same init run. ^SPEC-0038-US6-AC2
- The "what gets indexed" menu section offers the same include action on reruns, plus manual entry of a folder detection missed entirely; manual entries are validated to exist before being written. ^SPEC-0038-US6-AC3
- After an include, the section shows the re-detected docs and code in plain words before returning to the menu. ^SPEC-0038-US6-AC4
- Non-interactive runs can express the same inclusion through a flag (for example `--include-ignored <path>`, repeatable). ^SPEC-0038-US6-AC5

### US7 - Eject a workflow starter from Rhizome management with an explicit option
- id:: ^SPEC-0038-US7
- summary:: A maintainer can keep a starter's installed files as local repo assets while telling Rhizome to stop managing updates for that starter.
- status:: ready

Ejection is a maintenance decision for an existing repository, not a first-run setup choice. It should be explicit, reversible, and dependency-aware: ejecting a parent starter may drop unneeded transitive dependencies from Rhizome management, but ejecting a required dependency is blocked unless the user chooses a cascade that also ejects every dependent starter.

#### Acceptance Criteria

- A workflow can be removed or ejected, and the two are distinct. ^SPEC-0038-US7-AC1
  - Removing a workflow (choosing a different one in settings, or `--workflow`) deletes its skills, managed agent-doc blocks, saved queries, and views when they are unedited, applies the edit check to edited ones, and keeps its team-owned docs and its ontology schema, which existing notes may still use.
  - Ejecting a workflow (`--eject <starter>`, or the eject choice when moving away from a workflow in settings) keeps every file exactly as it is and stops Rhizome from updating it; the team owns and edits it from then on. `--restore <starter>` resumes management.
  - The first run offers neither, because nothing is installed yet.
- Ejection keeps files and stops management. ^SPEC-0038-US7-AC2
  - Ejecting a starter does not delete installed docs, schemas, skills, query recipes, views, managed agent-doc text, or config files.
  - Future reruns do not refresh or notify about ejected starter assets unless management is restored.
  - Existing managed starter blocks in agent docs are left exactly as they are, including Rhizome fence comments, so ejection freezes rather than removes the last managed guidance.
  - The ejection state is visible in config or starter-management state so future maintainers can tell the difference between "never installed" and "installed but unmanaged."
- Dependency consequences are previewed before changes are saved. ^SPEC-0038-US7-AC3
  - The preview lists the requested starter, dependent starters that block ejection, transitive dependencies that will stop being managed because they are no longer required, and starters that remain managed because they are explicit or still required elsewhere.
  - A required dependency such as `core` cannot be ejected while `agentic-engineering`, `project-kb`, `action-items`, or another dependent starter remains managed.
  - A required parent such as `agentic-engineering` cannot be ejected while `complex-domain` remains managed unless the same `--eject` names `complex-domain` too; the refusal names the dependents to add.
  - Optional default addons such as `action-items` are handled separately from required dependencies; when they are only active because of the starter being ejected, the preview defaults them to ejected unless the user explicitly keeps or disables them.
- Management can be restored. ^SPEC-0038-US7-AC4
  - Restoring an ejected starter returns its files to the ownership rule in [[#^SPEC-0038-US3-AC4]]; files changed while the starter was ejected count as edited.

### US8 - Migrate an existing spec-driven installation without losing local process decisions
- id:: ^SPEC-0038-US8
- summary:: An existing repository moves to the canonical agentic-engineering starter identity while preserving team-owned process changes for explicit reconciliation.
- status:: ready

The old name is a migration input, not a permanent equal alias. Migration should leave the repository with one canonical starter identity and make locally modified legacy process documents visible rather than silently merging or deleting them.

#### Acceptance Criteria

- Existing starter state migrates to one canonical identity. ^SPEC-0038-US8-AC1
  - `spec-driven` is accepted in existing workflow state and explicit init input only long enough to migrate it to `agentic-engineering`.
  - Managed/ejected starter state, dependency edges, starter fences, embedded-source fingerprints, inference, and update ownership migrate together so no duplicate starter remains.
  - Managed `spec-driven` fences migrate to the canonical id; ejected `spec-driven` fences remain byte-identical and continue to be preserved as frozen legacy content.
  - A rerun after migration is idempotent and does not recreate `spec-driven` state or a second managed block.
- Legacy process documents retire according to ownership evidence. ^SPEC-0038-US8-AC2
  - A legacy process document whose bytes match a known shipped template may be deleted after the replacement extension docs are installed.
  - A modified legacy process document remains at its old path with a clear retirement notice until a maintainer or authorized agent reconciles its relevant policy into the new docs.
  - Migration records the classification, fingerprint evidence, and required follow-up in tracked Markdown at `.rhizome/migrations/agentic-engineering/README.md`; the managed `.rhizome/.gitignore` keeps that path trackable, and the team deletes it after reconciliation.
  - Missing or uncataloged fingerprint evidence is never treated as proof that a document is unchanged.
  - Migration never performs a silent semantic merge.
- Existing domain assets keep accurate names. ^SPEC-0038-US8-AC3
  - Ontology schemas and query recipes whose contents specifically model specification-driven delivery may remain named `spec-driven`.
  - The `complex-domain` starter depends on the canonical `agentic-engineering` starter without otherwise broadening this migration into a complex-domain redesign.

### US11 - Clean up skills Rhizome retired
- id:: ^SPEC-0038-US11
- summary:: Skills a newer Rhizome version replaced disappear on the next init without a question.
- status:: ready

#### Acceptance Criteria

- Init remembers the names of skills it retired and removes those folders once their replacement is in place, without asking. ^SPEC-0038-US11-AC1
- Retired names that start with `rhizome-` count as Rhizome's by name; other retired names (such as `plan` or `implement`) are removed only when the record or an older Rhizome marker shows Rhizome wrote them, because a team may have its own skill by that name. ^SPEC-0038-US11-AC2
- A retired skill file that the record shows was edited still gets the edit check. ^SPEC-0038-US11-AC3

### US9 - Index code added after setup without rerunning init
- id:: ^SPEC-0038-US9
- summary:: A repository set up before its code (or before a new language) existed indexes that code on the next `rzm index`, and every diagnostic names why a file is or is not indexed as code.
- status:: ready

Today init records the language folders it detected, so code added later, or code in a repository that had none at setup, is silently skipped. Code indexing should follow the repository instead: on by default, covering everything that is not ignored, with the language taken from each file's extension.

#### Acceptance Criteria

- When code indexing is on and config names no code folders, the indexer treats the whole project (minus ignored paths) as code, and each file's language comes from its extension. ^SPEC-0038-US9-AC1
  - Fresh init writes code indexing on without folder limits, including in a repository with no code yet.
  - The indexing scope hash stays stable while config is unchanged, so automatic scope does not trigger repeated rebuilds.
  - Python module names use the indexer's default source folders when none are configured.
- Explicit folder limits remain supported and are visible. ^SPEC-0038-US9-AC2
  - Config that names code folders keeps limiting code indexing to them.
  - A rerun and `--check` list code that detection finds outside those folders as a suggestion to remove the limits; a person's confirmation in a terminal applies it.
  - A configuration with no `code` section (an older docs-only setup) gets a suggestion to turn on code indexing.
- `rzm index --explain <path>` reports, for a code file, whether it is indexed as code and its language, or which setting keeps it out (code indexing off, its language listed in `code.disabledLanguages`, or outside the configured folders) and how to change that. ^SPEC-0038-US9-AC3

### US10 - Keep bulky, low-value content out of the index
- id:: ^SPEC-0038-US10
- summary:: Init notices tracked content that would bloat the index without helping search (checked-in third-party code under unfamiliar names, minified or generated files, very large files) and skips it by default, visibly and reversibly.
- status:: ready

Git-ignored files and the built-in dependency and build folders are already skipped. What remains is content that is checked in on purpose but is not useful to search: a `third_party/` copy of a library, a generated API client, a minified bundle, a 40 MB Markdown export. These are repository-specific, so detection proposes them and the user sees the choice in the same screen as everything else.

#### Acceptance Criteria

- Detection proposes skips only for tracked, not-yet-ignored, indexable content that matches a conservative rule: a directory named like vendored or generated code (`third_party`, `third-party`, `external`, `extern`, `Pods`, `generated`, `__generated__`, `.next`, `.nuxt`, `.svelte-kit`) or test fixtures (`testdata`, `fixtures`, `__fixtures__`, `__snapshots__`), a minified or bundled file (`*.min.js`, `*.min.css`, `*.bundle.js`, `*.chunk.js`), a generated-code file (`*.pb.go`, `*_pb2.py`, `*.g.dart`, `*.designer.cs`, or a first line containing `Code generated` and `DO NOT EDIT`), or an indexable file larger than 1 MB. ^SPEC-0038-US10-AC1
- The first-run findings show proposed skips on one line with counts, and confirming writes them to `.rhizome/ignore` under a `# rhizome: suggested skips` comment with one short reason per entry. ^SPEC-0038-US10-AC2
- Reruns and `--check` list newly proposed skips in the change list. A path the user wants indexed carries a `# rhizome: keep indexed <path>` line in `.rhizome/ignore` (the settings menu writes it), and init never proposes such a path again; a suggested line deleted by hand is proposed again on the next rerun. ^SPEC-0038-US10-AC3
- "What gets indexed" in settings lets the user skip or stop skipping a folder or file by path, and shows the result in plain words. ^SPEC-0038-US10-AC4
- Without a terminal, init applies proposed skips and lists them. ^SPEC-0038-US10-AC5

## Requirements

### Setup flow and credentials

- `rzm init` first run MUST report detected docs, code, agents, and search provider before asking anything, MUST ask at most the workflow, a missing semantic search key, and one confirmation (plus one include question per ignored nested repository), and MUST write nothing before confirmation.
- Voyage AI MUST be the default semantic search provider for interactive and non-terminal runs; OpenAI and Ollama MUST remain selectable, and auto-detection MUST NOT silently substitute another provider for the default. Init MUST NOT enable a provider whose key does not resolve.
- Credential prompts in init and other commands MUST mention the Atomic Object Rhizome key only when the running binary bundles team keys, and MUST suggest it first when it does and a team-covered credential is missing.
- `rzm init` MUST run at most one consolidated credential pass per run, MUST persist provided keys immediately, and MUST NOT re-prompt for a key that is present in the environment, the global CLI config, or the current run's collected answers.
- Credential skips MUST persist and MUST suppress interactive re-prompts in non-init commands, which instead print a one-line actionable hint.
- A rerun MUST open with a status summary and change list, MUST apply the change list with one confirmation in a terminal and without confirmation otherwise, and `--check` MUST report the same list without writing.
- The interactive settings menu MUST be limited to: what gets indexed, semantic search, agents, and workflow.
- Expert tuning fields MUST NOT be interactive prompts and MUST be documented in a reference guide the menu points to.
- Interactive output MUST use project-relative paths and plain language; it MUST NOT print config keys, glob patterns, or `auto|on|off` modes in summaries.
- Non-interactive runs MUST be able to express first-run acceptance, workflow, agents, search provider, credential provision (via environment), and ignored-subtree inclusion through flags or environment rather than prompts.
- Removed flags MUST fail with a one-line message naming their replacement.
- Notes MUST default to all Markdown and code MUST default to on without folder limits for fresh setups; the indexer MUST treat an enabled code configuration with no folders as the whole project minus ignored paths. Init MUST NOT remove existing folder limits without a person's confirmation.
- Proposed skips MUST be limited to tracked, not-yet-ignored, indexable content matching the US10 rules, MUST be written only to `.rhizome/ignore` with a reason, and MUST NOT be re-proposed after the user removes them.

### Agentic-engineering starter identity and migration

- `agentic-engineering` MUST be the canonical workflow starter id and MUST be described as Rhizome's repository harness for Agentic Engineering.
- `spec-driven` MUST be treated as a bounded migration input, not retained as a second canonical starter or permanent equal alias.
- Migration MUST preserve local process decisions for review, MUST auto-delete only content proven unchanged by a checked-in historical shipped-content fingerprint catalog, and MUST record modified or unproven document reconciliation work in tracked Markdown under `.rhizome/migrations/agentic-engineering/`.
- Specification-specific ontology and query assets MAY retain the `spec-driven` filename when that name accurately describes their modeled domain.

### Templates, surfaces, and config writing

- `rzm init` MUST treat `.rhizome/config.yml` as the project-local configuration target.
- Ordinary reads of `.rhizome/config.yml` and `.rhizome/workflows.yml` MUST treat blank/comment-only files as empty mappings, otherwise accept exactly one YAML document, and MUST NOT rewrite either file. Unknown keys MUST return path-aware warnings and remain preserved; malformed YAML, multiple documents, and invalid known-field types MUST remain distinct errors. `rzm init` MUST losslessly migrate the retired v0.49 `workflowTemplates`, `workflowTemplateAddons`, and `workflowTemplateManagement` fields into `.rhizome/workflows.yml`, remove only those keys from `config.yml`, update tracking state, and be idempotent.
- `rzm init` MUST preserve user-authored content outside Rhizome managed fences in `AGENTS.md` and `CLAUDE.md`.
- `rzm init` MUST inject the core Rhizome guidance block using `BEGIN/END RZM INIT RHIZOME BLOCK`.
- `rzm init` MUST inject workflow-starter agent guidance using `BEGIN/END RZM INIT TEMPLATE BLOCK: <template>`.
- `rzm init` MUST install core Rhizome skills independently of workflow starter selection.
- `rzm init` MUST install starter-only skills only when their starter is active.
- `rzm init` MUST remove stale managed starter skills that are no longer present in the active managed starters when their files are unedited.
- `rzm init` MUST reject unknown workflow template names.
- `rzm init` MUST reject target-path collisions between selected workflow templates.
- `rzm init` MUST reject skill-name collisions between core and starter skill bundles.
- `rzm init` MUST keep existing starter docs create-only by default and apply the ownership rule to them only when `--refresh-docs` is given.
- `rzm init` MUST create missing Rhizome-owned files without prompting, MUST update Rhizome-owned files whose content matches the recorded written hash without prompting, and MUST ask before replacing a Rhizome-owned file someone edited.
- `rzm init` MUST record written hashes and declined versions in the tracked `.rhizome/generated-files.yml` and MUST NOT ask again about a declined version.
- `rzm init` without a terminal MUST update unedited Rhizome-owned files, MUST keep edited ones, and MUST list the kept files.
- `rzm init` MUST support per-starter ejection from management, preserving files while suppressing future updates and notifications for that starter.
- `rzm init` MUST leave ejected starter managed blocks in agent docs untouched, including their fence comments, unless management is restored and an update is accepted.
- `rzm init` MUST compute ejection consequences from the starter dependency graph and block ejection of required dependencies unless every dependent is ejected in the same request.
- `rzm init` MUST distinguish required dependencies from optional default addons when presenting or applying ejection consequences, defaulting optional addons that are only active through an ejected starter to ejected.
- `rzm init` MUST leave legacy MCP config files untouched and MUST NOT scaffold new MCP config files.
- `rzm init` MUST resolve agent-surface participation according to [[agent-surface-integration-modes]] before rendering managed docs, commands, prompts, or skills.
- `rzm init` SHOULD make template updates observable through the rerun change list, a diff on request, and the list of kept files in batch runs.
- `rzm init` SHOULD remove old generated `RHIZOME.md` guidance files once their content is represented in managed agent-doc blocks.

## Open Questions

- Should future workflow starters remain mutually compatible, or should init continue to treat cross-template target collisions as an explicit design error?

## Documentation plan

- Reference guide covering the config-only fields the menu no longer prompts for (link style, include/exclude globs, code roots, embedding providers and endpoints, index path, compression, file-context packing, graph tuning), linked from the menu pointer line.
- README and getting-started init walkthrough refresh (first run, rerun change list, settings, flags).
- Init module context and hub update covering the ownership rule, `.rhizome/generated-files.yml`, and the flag set.
- `rhizome` skill configuration, installation, and indexing references rewritten around `rzm init`, `rzm init --check`, and `rzm index --explain`, plus a new index-scope reference covering ignore layers, how to find bulky content, when to skip it, and how to write and verify rules.
- Update `docs/reference/guides/Ignore behavior.md` cross-link for the init-driven inclusion flow (contract in [[ignored-subtree-inclusion]]).

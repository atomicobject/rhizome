---
type: EffortNote
id: EFF-2026-10-01-10-06
name: Init experience redesign
created-at: 2026-10-01T14:06:08Z
status: active
summary: Replace the rzm init interaction with an opinionated first run, a maintenance-first rerun, a four-section settings menu, and one file-ownership rule that asks only about locally edited files; align the rhizome skill's setup and troubleshooting guidance.
aliases:
  - EFF-2026-10-01-10-06
---

# Init experience redesign

## Scope

Redesign how people and agents set up and maintain Rhizome in a repository:

- `rzm init` on a fresh repository: one screen that shows what Rhizome found and what it will do, at most three questions, and quiet, grouped output.
- `rzm init` on a configured repository: a status summary that notices what changed (new code, available updates, locally edited files) and applies safe maintenance with one confirmation. Settings stay one keystroke away.
- A settings menu with four sections: what gets indexed, semantic search, agents, workflow.
- One rule for every file Rhizome generates: create new files without asking, update files nobody edited without asking, and ask only about files someone edited.
- Voyage AI as the default semantic search provider, with OpenAI and Ollama as alternatives.
- `rzm init` always finishes in one run: one confirmation in a terminal, none without one. A smaller flag set for scripts and agents, plus `--check` to preview without writing.
- Skip suggestions for bulky, low-value content that Git and the built-in defaults do not already exclude (checked-in third-party code under unfamiliar names, minified or generated files, very large files), shown in the same screen and written to `.rhizome/ignore`.
- The `rhizome` skill's configuration, installation, and indexing guidance rewritten around those commands, including the "my code is not indexed" case, plus a new index-scope reference so agents can judge and change what Rhizome indexes, not only run init.

Outside this slice: the project installer's download and pin logic, detecting per-language source folders at index time (automatic code scope uses the whole repository instead), the web UI, and ontology or starter content changes beyond the wording of starter descriptions.

## Review of the current experience

Evidence comes from running the current build (`bin/darwin/rzm`, built from `3e2a5d3`) in fresh scratch repositories through a pseudo-terminal, plus a code trace of `pkg/app/cli/init` and `pkg/app/cli/init/diff`.

### First run

1. **The first question is the hardest one.** Before showing anything it found, init asks the user to pick from five workflow bundles by number: `agentic-engineering`, `none`, `action-items`, `core`, `complex-domain`. The ids are internal. `core` is a dependency every other starter already pulls in, `none` sits at position 2 of 5, and the descriptions use internal vocabulary ("phase adapters", "shared Person identity ontology").
2. **Agent setup is a separate question about file mechanics.** "This can refresh managed Rhizome guidance blocks in AGENTS.md / CLAUDE.md, create shared .agents skills, and populate detected agent folders like .claude or .codex." The user is asked to consent to an implementation detail rather than told which agents will work.
3. **The recommended setup leaks configuration syntax.** `Code indexing: enabled; Go:.`, `Agent helpers: .agents:on; AGENTS.md:on`, `Compression: disabled`. The "Indexing preview" block repeats the notes line and adds `Areas Go: repo root`.
4. **Semantic search takes four prompts for one decision, and offers a key most users cannot have.** "Choose an embeddings provider now?", then a four-way provider menu, then a warning that the key is missing followed by "semantic search is set to enabled", then a three-way credential menu ("Atomic Object Rhizome Key / Voyage API Key / Skip"). That menu offers the Atomic Object key even in public and source builds, whose team-key bundle is empty, so pasting it there unlocks nothing. With an OpenAI key in the environment and no Voyage key, auto-detection silently picks OpenAI instead of the recommended provider.
5. **Fresh files ask for approval one by one.** Starter files are created silently, but every core skill file (`rhizome`, `custom-views`, `client-harness-builder`, `legacy-codebase-assessor`) shows a ten-option prompt: `[y]es [n]o [v]iew hunks [s]kip [a]ccept all [A]always accept all [r]eject all [w]always [x]never [q]uit`. AGENTS.md and CLAUDE.md print "created" and then immediately prompt "(1 change(s))", because the starter block is written first and the core Rhizome block is then reviewed as a change to the file init just created.
6. **Output is noisy and partly wrong.** About 60 absolute-path "created" lines print during setup. Prompt headers show `../../../private/tmp/...` because the updater's root is the unresolved working directory while targets use the symlink-resolved project root; the same wrong relative path becomes the key for saved rejections. Missing-key warnings print twice in the middle of file creation. A failing pinned-binary install prints the full flag help.

### Reruns

7. **The first question hides the useful path.** "Found existing config. Reconfigure it? (y/N)". Answering no silently refreshes managed files and may prompt per file; answering yes opens a menu. Nothing says what changed or what would be updated.
8. **The menu mixes decisions with maintenance actions and raw config.** Seven entries: notes and docs (shown as raw globs such as `docs/**/*.md, docs/efforts/**/*.html`), code indexing (`enabled; Go:.`), "Semantic + AI" (`disabled; compression disabled`), agent integration (`.agents:on; AGENTS.md:on`), workflow starters, "Re-run detection", and "Refresh starter docs". The heading and instructions print twice.
9. **Sections expose knobs no one wants to manage.** Notes ask for link style (wikilinks, Markdown, both) and include/exclude globs. Code asks for per-language comma-separated root lists. Semantic search asks separately whether to enable notes and code embeddings and for an index path. Agents ask `auto/on/off` for five surfaces, including `.agents skills mode` and `AGENTS.md mode`. Workflow starters open a six-item submenu: change, review updates, eject, restore, dependency graph, and per-family update policy (`docs`, `skills`, `managedDocs`, `ontology`, `queryRecipes`, `views` × `always/never/prompt`).
10. **Code added after init is silently missed.** In a repository initialized with only `docs/`, init writes no `code` section. After adding `src/main.go` and running `rzm index`, `rzm code symbols` fails with "code index not enabled", while `rzm index --explain src/main.go` reports the file as "indexed". Rerunning `rzm init --yes` does not turn code on; it writes a stray `noteEmbeddings: {provider: ollama}` instead. The same gap applies to any code outside the language folders init recorded (code under a recorded root of `.` is covered, because the indexer takes each file's language from its extension). The command help still claims init "will only prompt you about newly detected language support", which the code no longer does.

### Update and approval mechanics

11. **There is no usable record of what Rhizome last wrote, and the partial ones clutter skill folders.** Starter source fingerprints in `.rhizome/workflows.yml` hash embedded sources (not written bytes), cover only starter assets, and advance even when the user declines an update. Core skills, the core managed block, and harness command files have no record. Every installed skill folder also carries two hidden files, `.rhizome-managed` (a marker that grants cleanup authority) and `.rhizome-managed-files` (hashes of reference files, used only to prune dropped references), in each agent mirror; this repository tracks 28 of them. Init still cannot tell an untouched old file from a customized one, so it either asks about everything or applies everything.
12. **Approval state has grown many overlapping layers.** Hunk rejections (honored only with `--skip-rejected`), per-file always/never preferences, a global always-accept that silently overrides per-file never, session apply-all and reject-all, family update policies, starter adoption authority, and `--yes` that is deliberately not update authority. An insert-only hunk's rejection fingerprint is the hash of its removed lines, so every insert-only hunk in a file shares one fingerprint.

### Flags

13. **Twenty flags, most of them maintenance internals.** Five separate `auto|on|off` agent flags, three rejection flags, three ejection flags, `--no-config`, `--refresh-template-docs`, and a debug manifest flag. The README's recommended command is `rzm init --template agentic-engineering --agentsmd on --agent-skills on`, which spells out defaults the user should never have to know.

### Index scope

14. **Bulky content is indexed in full.** Git-ignored paths and a built-in list (`node_modules/`, `vendor/`, `dist/`, `build/`, `bin/`, and similar) are skipped, but nothing notices a checked-in `third_party/` folder, generated API clients, minified bundles, or a 40 MB Markdown export, and indexing has no file-size limit. The built-in list is copied into `.rhizome/ignore` on first run, so later improvements to it never reach existing repositories, and a team's own rules are buried among thirty generated lines.

### Rhizome skill

15. **Setup guidance tells agents to ask before running init and then use flags.** `references/configuration.md` lists the knobs (note globs, code roots, file-context packing, graph scope, coderef globs, embedding endpoints) without a path for the common questions: "why isn't my code indexed", "I added a language", "switch semantic search provider", "the key is missing". There is no read-only way for an agent to see what init would change.

## New experience

### Principles

- Show what Rhizome found before asking anything, in plain words.
- Ask only what Rhizome cannot decide: which workflow to install, and a semantic search key when none is available. Everything else has an opinionated default the user can change later.
- Create freely, update carefully: Rhizome-owned files that nobody edited update silently; files someone edited are the only ones that need a decision.
- A rerun is maintenance first. It says what changed and offers to apply it.
- Configuration syntax, file mechanics, and expert knobs stay in `.rhizome/config.yml` and the reference guide.

### First run

```text
$ rzm init

Set up Rhizome in demo
Rhizome indexes this repository's docs and code so agents can search them,
and installs guidance that teaches your agents to use it.

  Docs      docs/ (12 notes) and README.md
  Code      Go, TypeScript (148 files)
  Skip      third_party/ (1,204 files), 3 minified bundles, data/export.md (14 MB)
  Agents    Claude Code, Codex
  Search    Voyage AI

Workflow
  1  Agentic Engineering (recommended)
     Specs, efforts, and engineering policy docs your agents follow
  2  Agentic Engineering with domain modeling
     Adds sources, requirements, and traceability for regulated or complex domains
  3  Search and agent guidance only
Choose [1]:

Semantic search
  Rhizome uses Voyage AI to search notes and code by meaning.
  This Rhizome build includes Atomic Object team keys. Paste your
  Atomic Object Rhizome key to unlock them, or paste your own Voyage API key.
  Press Enter to set it up later, or type "other" for OpenAI or Ollama.
Key:

Set up Rhizome? [Y/n/e to edit]:

  ✓ .rhizome/config.yml
  ✓ Rhizome guidance in AGENTS.md and CLAUDE.md
  ✓ 9 skills for Claude Code and Codex
  ✓ Agentic Engineering docs in docs/engineering, docs/specs, docs/efforts
  ✓ Voyage key saved to ~/.config/rhizome/config.yml

Next
  rzm index        build the search index
  Commit .rhizome/, AGENTS.md, CLAUDE.md, .agents/, and .claude/ so your team shares this setup.
```

- The key question appears only when no Voyage key or team key resolves.
- The Atomic Object key appears only in builds that bundle team keys (internal releases); there it leads the prompt because one key unlocks Voyage and the other bundled providers. Public and source builds have an empty bundle, so their prompt reads "Paste a Voyage API key" with a link to create one and never mentions the Atomic Object key. Typing `other` shows OpenAI (with a found/missing key marker), Ollama (installed or not), and "Turn off semantic search".
- `e` opens the settings menu with these choices preloaded; Enter in the menu returns to the confirmation.
- Agents are inferred from the repository (`.claude`, `CLAUDE.md`, `.cursor`, `.codex`) and from the machine (a `claude`, `codex`, or `cursor` command on `PATH`; home directories such as `~/.cursor` outlive uninstalled tools, so they do not count). `AGENTS.md` and `.agents/skills` are always written because Codex, Cursor, and most other agents read them.
- Ignored nested repositories, when detected, add one line and one yes/no question before confirmation, as today.
- The Skip row appears only when detection proposes something. Proposals are conservative: tracked, not already ignored, indexable, and either named like vendored or generated code (`third_party`, `external`, `Pods`, `generated`, `.next`, ...), minified or bundled (`*.min.js`, `*.bundle.js`), generated by a code generator (`*.pb.go`, `*_pb2.py`, a `Code generated ... DO NOT EDIT` header), or larger than 1 MB. Confirming writes them to `.rhizome/ignore` under `# rhizome: suggested skips`, each with a short reason.
- Without a terminal (an agent or a script), `rzm init` uses these recommendations, writes, and prints the same summary of what it did.
- With no usable key, semantic search stays off and init prints one line saying how to turn it on; `rzm index` refuses to run with search on and no key, so enabling it early would break indexing. A later rerun offers to turn it on once a key is available.
- `rzm init --check` prints this screen plus every file and setting init would write, writes nothing, and exits with status 1 when anything would change.

### Rerun

```text
$ rzm init

Rhizome is set up in demo
  Docs      docs/ (14 notes) and README.md
  Code      Go
  Agents    Claude Code, Codex
  Search    Voyage AI
  Workflow  Agentic Engineering

Changes
  + Index code in tools/ (outside the folders this repository limits code indexing to)
  − Skip web/public/vendor.bundle.js (minified, 2.1 MB)
  ↻ Update 6 Rhizome files (guidance and skills from v0.52.0)
  ! 1 skill you edited has an update: .agents/skills/agentic-engineering/SKILL.md

Apply? [Y/n/s for settings]:

  ✓ Code indexing now covers the whole repository
  ✓ Updated 6 Rhizome files

.agents/skills/agentic-engineering/SKILL.md has local edits and a newer Rhizome version.
  y  take the update      n  keep my version      d  show the diff
Choose [n]:

Run rzm index to pick up the change.
```

- With nothing to change, the command prints the summary and "Everything is up to date. Press s for settings or Enter to exit."
- "Keep my version" records the declined version. Rhizome asks again only when a newer version of that file ships.
- Without a terminal, `rzm init` applies the change list, keeps every edited file, and lists the kept files with a note that running `rzm init` in a terminal offers the update. Nobody has to run init twice.
- `rzm init --check` prints the summary and change list, writes nothing, and exits with status 1 when the list is not empty. Agents use it to diagnose setup, and CI can use it to confirm generated files are current.
- Copies of one skill in `.agents/skills` and `.claude/skills` share one question.

### Settings

```text
Settings
  1  What gets indexed   docs/ and README.md · Go, TypeScript
  2  Semantic search     Voyage AI
  3  Agents              Claude Code, Codex
  4  Workflow            Agentic Engineering
Choose a number, or press Enter when done:
```

1. **What gets indexed** shows the current docs, code, and skips in plain words and offers: use what Rhizome detects now (default), index all Markdown in the repository, choose doc folders (a comma-separated folder list, never globs), skip a folder or file, stop skipping one, and include a folder Git ignores. Code indexing covers the whole repository minus ignored paths, so languages need no setup; explicit code folder limits, link style, and include/exclude globs move to the config reference.
2. **Semantic search** offers Voyage AI (recommended), OpenAI, Ollama, or off, and asks for a key only when the chosen provider has none. Notes and code always share one provider; code embeddings follow code indexing. Index path, models, endpoints, and compression stay in config.
3. **Agents** is a checklist of Claude Code, Codex, and Cursor with what each adds, plus "stop managing agent files". `AGENTS.md` and `.agents/skills` follow automatically.
4. **Workflow** shows the same three choices as the first run. Ejecting a starter and family update policies leave the menu; ejection remains a flag.

### File ownership

Every generated file falls into one of three kinds:

| Kind | Examples | Rule |
| --- | --- | --- |
| Rhizome-owned | managed blocks in `AGENTS.md`/`CLAUDE.md`, skills, harness commands, starter ontology, query recipes, views | Created without asking. Updated without asking when the file still matches what Rhizome last wrote. When someone edited it, interactive runs ask (take the update, keep mine, show diff); batch runs keep the local version and report it. |
| Team-owned after install | starter docs under `docs/` | Created when missing; never updated unless `--refresh-docs` is given, which applies the Rhizome-owned rule. |
| Configuration | `.rhizome/config.yml`, `.rhizome/workflows.yml`, `.rhizome/ignore` | Written from settings; unknown keys and user-authored ignore lines preserved. Init appends only to its own commented sections in `.rhizome/ignore`. |

Rhizome records the hash of every Rhizome-owned file and managed block it writes, and the declined version when someone keeps their own, in a tracked `.rhizome/generated-files.yml`. That is all the file holds, and it replaces the `.rhizome-managed` and `.rhizome-managed-files` files in every skill folder: a skill folder is Rhizome-owned when the record lists its files, and a reference a newer version dropped is deleted only when it still matches its record. The first run folds those per-folder files into the record and deletes them. The record exists because without it Rhizome cannot tell "you edited this skill" from "this is an older Rhizome version of this skill", so it must either ask about every changed file (today's behavior) or overwrite your edits. It is tracked so a teammate's rerun makes the same call; a fresh clone without it would treat every file as possibly edited. Hashes normalize line endings so a Windows checkout with `core.autocrlf` does not make every file look edited. On the first run after upgrading, files without a record that differ from the current version are grouped into one question ("Rhizome cannot tell whether these 12 files were edited. Update them? [Y/n/r to review each]"); batch runs keep them and report them.

This replaces hunk rejections, `.rhizome/template-rejections.yml`, per-file and global always/never preferences, session apply-all/reject-all, starter family update policies, and starter source fingerprints as update authority.

### Flags

| Flag | Purpose |
| --- | --- |
| `--check` | Show what init would change; write nothing; exit 1 when something would change |
| `--workflow agentic-engineering\|domain\|none` | Choose the workflow without prompting |
| `--agents claude,codex,cursor\|none` | Choose agent integrations without prompting |
| `--search voyage\|openai\|ollama\|off` | Choose the semantic search provider without prompting |
| `--include-ignored <path>` | Index a folder Git ignores (repeatable) |
| `--refresh-docs` | Offer updates to starter docs the team owns |
| `--binary-manager external` | Let an external tool such as mise own the Rhizome binary |
| `--path <dir>` | Initialize another directory |
| `--eject <starter>` / `--restore <starter>` | Keep a workflow's files for the team to own and stop updating them, or resume updates |

Removed: `--yes` (`rzm init` without a terminal already runs without asking), `--template` (replaced by `--workflow`), `--cursor`, `--claude`, `--codex`, `--agent-skills`, `--agentsmd` (replaced by `--agents`), `--no-config` (a rerun with no setting changes already leaves config alone), `--skip-rejected`, `--reject-all`, `--clear-rejections` (replaced by the ownership rule), `--refresh-template-docs` (renamed), `--eject-template-cascade` (eject reports dependents and `--eject` takes several names), `--restore-template-management` (renamed), `--skill-overlay-manifest` (moved to a test helper). Per the architecture policy there are no compatibility shims: a removed flag fails with a one-line message naming its replacement.

`--workflow action-items` and `--workflow core` remain accepted for repositories that want only those layers; the menu does not offer them.

### Rhizome skill

`references/configuration.md` and `references/installation-and-integration.md` get a short troubleshooting route built on `rzm init --check`, `rzm init`, `rzm index --status`, and `rzm index --explain`:

| Symptom | Route |
| --- | --- |
| Code is not indexed, or code was added after setup | `rzm index --explain <file>` names the setting that keeps it out; `rzm init --check` shows the fix; with consent run `rzm init` (which makes every change the check listed, so show the user that list first), then `rzm index` |
| A file is missing from search | `rzm index --explain <path>`, then fix ignore rules or "What gets indexed" |
| Semantic search is off or a key is missing | `rzm index --status`; set the key or run `rzm init --search <provider>` |
| Agent guidance or skills are missing | `rzm init --check`, then `rzm init`; `--agents` to change which agents |
| An edited skill keeps its old version | expected; an interactive `rzm init` offers the update |
| Search is slow, noisy, or full of third-party or generated results | `rzm init --check` for proposed skips; otherwise the index-scope reference |

The skill keeps the rule that init runs only with the user's consent, but it no longer needs flag recipes such as `init --agentsmd on --agent-skills on --yes`.

A new `references/index-scope.md` gives agents working knowledge of what Rhizome indexes, so they can change it directly when init's suggestions are not enough:

- **Layers, in order:** built-in infrastructure folders, `.gitignore` files (including nested ones), `.rhizome/ignore`, then config excludes; the last matching rule wins. Exact `CONTEXT.md` files are always notes unless a hard ignore excludes them. `notes.includes`/`excludes` decide which Markdown counts as notes; ignore rules remove a path from everything.
- **What to skip:** checked-in dependencies, generated code and API clients, minified or bundled assets, snapshots, large fixtures and data exports, exported HTML or Markdown from other tools, and copies of upstream docs. **What to keep:** source the team maintains, docs that describe this system, and tests that explain behavior.
- **How to find candidates:** `rzm init --check`; tracked-file counts per directory (`git ls-files | cut -d/ -f1-2 | sort | uniq -c | sort -rn | head`); large tracked files; `rzm index --status` for indexed counts.
- **How to write rules:** gitignore syntax in `.rhizome/ignore`, not `.gitignore`, for Rhizome-only concerns; a trailing `/` for folders; a leading `/` to anchor at the repository root; one comment line per group saying why; `# rhizome: keep indexed <path>` to stop init proposing a skip; `!/path/` to include a folder Git ignores (an include boundary; nested `.gitignore` files still apply inside it).
- **How to verify:** `rzm index --explain <path>` names the deciding layer, file, line, and pattern; ignore edits trigger a full resync on the next `rzm index`; rerun the search that exposed the problem.

`references/configuration.md` is reorganized around the settings that decide whether Rhizome works well, with a route for each, before it mentions expert fields:

| Setting | Why it matters | How an agent changes it |
| --- | --- | --- |
| What gets indexed | Missing content cannot be found; bulky content drowns real results | `index-scope.md`; `rzm init --check`; `.rhizome/ignore` |
| Semantic search provider and key | Without it, meaning-based search and code similarity are off | `rzm init --search <provider>`; key in the environment or via an interactive `rzm init` |
| Agents | Missing guidance or skills mean agents do not use Rhizome | `rzm init --agents ...` |
| Current user | Action items, approvals, and "my" queries need an identity | `rzm agent current-user set "<Person>"` |
| Binary ownership | Decides who updates Rhizome and how teammates get the same version | existing install guidance |
| Validation suites | Decides what the team's gate checks | `validation:` in config, verified with `rzm validate` |

Expert fields (file-context packing, graph tuning, compression, embedding models and endpoints, index path) stay in the Advanced configuration tuning guide, and the skill tells agents to leave them alone unless the user asks or a diagnosis points to one.

## Decisions for approval

1. **Generated-files record.** A tracked `.rhizome/generated-files.yml` that records the hash of each file Rhizome wrote and any version someone declined, replacing `.rhizome/template-rejections.yml`, starter source fingerprints, and the `.rhizome-managed` and `.rhizome-managed-files` files in every skill folder. Agreed by Drew on 2026-10-01. Tension: one more tracked file, against an update rule people can predict. Alternative considered: a hash comment inside each generated file, which travels with the file but adds noise to every skill and cannot carry a declined version without editing the user's copy.
2. **One-run init and `--check`.** `rzm init` asks once in a terminal and does not ask without one, so `--yes` goes away. Without a terminal it applies the recommendations and change list, keeps edited files, and lists them. `rzm init --check` previews without writing. This reverses SPEC-0038's rule that batch runs never apply updates, because the edit check now protects local work. Agreed by Drew on 2026-10-01.
3. **Starter choice.** Agreed by Drew on 2026-10-01. The menu offers three workflows; `action-items` and `core` alone remain flag-only.
4. **Removed menu knobs.** Agreed by Drew on 2026-10-01. Link style, include/exclude globs, per-language roots, separate note and code embeddings, index path, compression, per-surface `auto/on/off`, ejection, family policies, dependency graph, and refresh-starter-docs leave the interactive flow; the config reference documents what remains configurable.
5. **Flag migration.** Agreed by Drew on 2026-10-01. Remove replaced flags without aliases, with a one-line replacement message, per `docs/engineering/architecture.md`.
6. **Automatic code scope.** Code indexing is on by default and, when config names no code folders, covers the whole repository minus ignored paths; the indexer already takes each file's language from its extension. Fresh init writes code on without folder limits, even with no code yet, so code added later indexes on the next `rzm index`. Explicit folder limits keep working, and reruns offer to remove them when code exists outside them. `rzm index --explain` names the setting that keeps a code file out. Agreed by Drew on 2026-10-01. The indexer change is small (the few places that build the code-folder list from config fall back to the project root) but touches indexing, the live runtime, and validation applicability, so Batch 5 loads those subsystem notes.
7. **Skip suggestions.** Init proposes skips with conservative rules, applies them on confirmation or in a non-terminal run, and writes them to `.rhizome/ignore` with reasons. Keeping a path indexed is recorded visibly in the same file as `# rhizome: keep indexed <path>`, so no hidden state is needed. Agreed by Drew on 2026-10-01. Tension: a wrong suggestion hides content, against bulky content silently degrading search; visibility in the findings screen and a plain reason per line keep it reversible.
8. **Built-in ignore list.** Keep the current behavior (defaults copied into `.rhizome/ignore` on first run) in this effort and record "apply built-in defaults without copying them" as a follow-up, because it changes the ignore precedence contract owned by vault core. Agreed by Drew on 2026-10-01.
9. **Index everything by default.** Notes cover all Markdown and code covers every supported language; init writes no folder limits, and `.rhizome/ignore` is the only way content stays out, so content added later is never silently missed. For existing configs with folder limits (or code off), reruns suggest removing them and apply only after a person confirms, because a limit may be deliberate. Agreed by Drew on 2026-10-02.

## Spec Set (Frozen)

Proposed; frozen at plan approval, at the revision that carries these spec edits:

- [[init-starter-workflow|SPEC-0038]]: US1, US3, US4, US5-AC5, US6-AC3/AC4, US7-AC1/AC3/AC4, new US9, US10, and US11, and the setup, ownership, skip, and flag requirements.
- [[agent-surface-integration-modes|SPEC-0045]]: machine detection, `--agents`, and first-run confirmation in US1 and US3.
- [[init-template-architecture|SPEC-0039]]: the refresh and ownership workflow, management state, and US3/US4 criteria.

US2 and US8 of SPEC-0038 (starter content and `spec-driven` migration) are unchanged and out of scope except where their state keys are removed.

## Stories In Scope (Frozen)

Proposed; frozen at plan approval:

- [[init-starter-workflow#^SPEC-0038-US1]], [[init-starter-workflow#^SPEC-0038-US3]], [[init-starter-workflow#^SPEC-0038-US4]], [[init-starter-workflow#^SPEC-0038-US5]], [[init-starter-workflow#^SPEC-0038-US6]], [[init-starter-workflow#^SPEC-0038-US7]], [[init-starter-workflow#^SPEC-0038-US9]], [[init-starter-workflow#^SPEC-0038-US10]], [[init-starter-workflow#^SPEC-0038-US11]]
- [[agent-surface-integration-modes#^SPEC-0045-US1]], [[agent-surface-integration-modes#^SPEC-0045-US3]]
- [[init-template-architecture#^SPEC-0039-US3]], [[init-template-architecture#^SPEC-0039-US4]]

## Spec Coverage Checklist

- [x] First run: findings first, at most three questions, quiet grouped output (SPEC-0038 US1).
- [x] Voyage default and one key prompt that recognizes the team key (SPEC-0038 US1-AC2, US5-AC5).
- [x] Rerun summary, change list, one confirmation, one run without a terminal, `--check` (SPEC-0038 US3-AC3).
- [x] One ownership rule and `.rhizome/generated-files.yml`, including upgrade migration (SPEC-0038 US3-AC4/AC5, SPEC-0039).
- [x] Four-section settings menu with no expert knobs (SPEC-0038 US4).
- [x] Named agent selection, machine detection, `--agents` (SPEC-0045 US1, US3).
- [x] Ignored-folder inclusion from "What gets indexed" (SPEC-0038 US6).
- [x] `--eject` and `--restore` with dependency checks (SPEC-0038 US7, SPEC-0039 US3).
- [x] Code added later indexes without rerunning init; explicit folder limits are visible and removable; `index --explain` names why a file is out (SPEC-0038 US9).
- [x] Skip suggestions for bulky content (SPEC-0038 US10).
- [x] Retired skills cleaned up without asking; remove versus eject a workflow (SPEC-0038 US11, US7).
- [x] `rhizome` skill and docs describe the new flow, including the index-scope reference.

## Plan

Six batches. Batch 1 fixes a persisted format and the update rule that every later batch depends on, so it ends with a `foundation-review` pause.

### Batch 1: ownership record and update rule

| Field | Content |
| --- | --- |
| Outcome | Every Rhizome-owned write goes through one updater that creates missing files silently, updates unedited files silently, and asks only about edited ones (SPEC-0038 US3-AC4/AC5, SPEC-0039 refresh and ownership workflow). |
| Scope | `pkg/app/cli/init/diff` replaced by a planner/applier; `.rhizome-managed` and `.rhizome-managed-files` retired from skill folders; callers in `agent_surfaces.go`, `template.go`, `template_update_authority.go`, `starter_updates.go`, `run.go`. New `.rhizome/generated-files.yml` (tracked; added to the managed `.rhizome/.gitignore` allowlist). Removes rejections, per-file/global preferences, session apply/reject-all, family update policy, and starter source fingerprints. Fixes the symlinked project-root path bug. |
| Work | Define the record (`version`, per-path `written` and optional `declined` hashes, block-keyed entries for managed fences, starter docs recorded at creation, sorted keys, LF-normalized hashing, unreadable record treated as absent). Mirrored skill copies share one decision. Non-terminal runs apply everything except edited files; `--check` writes nothing. Classify each target as missing, current, updatable, or edited. Edited-file prompt with three choices and diff on request. First-run-without-record grouped decision. Migration deletes `.rhizome/template-rejections.yml` and strips `management.updatePolicy` and `management.sourceFingerprints`. Retired-skill, stale-starter-skill, and dropped-reference cleanup only for unedited files, driven by the record. Migration folds existing skill-folder markers and manifests into the record and deletes them. |
| Tests | Through `Run` with temporary repositories: fresh install creates everything with zero prompts; rerun after a template change updates unedited files with zero prompts; an edited skill prompts once, "keep" is remembered, and a newer version asks again; a non-terminal run keeps edited files and lists them; `--check` writes nothing and exits 1 with pending changes; upgrade from a config with rejections, fingerprints, and skill-folder markers migrates idempotently and leaves skill folders with only skill content; `/tmp`-style symlinked roots print project-relative paths. |
| Exit evidence | Focused `go test ./pkg/app/cli/init/...`; `make check-fast`. Then a `foundation-review` of the record format and update rule, inspecting a real `.rhizome/generated-files.yml` from a scratch repository, before Batch 2. |
| Escalation | Any need to keep hunk-level review or a second preference layer. |

### Batch 2: first run

| Field | Content |
| --- | --- |
| Outcome | The first-run transcript in this note (SPEC-0038 US1, US5-AC5; SPEC-0045 US1, US3). |
| Scope | `interactive_flow.go`, `onboarding_preview.go`, `configure_embed.go`, `credentials.go` and `pkg/app/credentials` prompt, `detect.go` (machine harness detection), `cmd/init.go` (flags, `SilenceUsage`). |
| Work | Findings screen with plain summaries (note count, language and file counts). Workflow question with three choices. Voyage-first key prompt; when `pkg/teamkeys` reports a bundled build (new `Bundled()`), it leads with the Atomic Object key and recognizes it by trial decryption, otherwise it never mentions it. The same rule applies to the shared `pkg/app/credentials` prompt used by `rzm index`, with `other` for OpenAI, Ollama, or off; remove the OpenAI/Ollama auto-substitution. Confirmation with `e` into settings. Grouped write summary and next steps. New flags `--workflow`, `--agents`, `--search`, `--check`; removed flags, including `--yes`, fail with their replacement. |
| Tests | Scripted first runs: defaults, workflow choice 3, key provided, key deferred, bundled and unbundled builds (synthetic bundle, per the credential policy), `other` → Ollama, `e` → settings → back, a non-terminal first run with no key, `--agents none`, a removed flag. |
| Exit evidence | Focused tests; an expect-driven first run in a scratch repository whose transcript matches the design. |

### Batch 3: rerun and settings

| Field | Content |
| --- | --- |
| Outcome | The rerun and settings transcripts (SPEC-0038 US3-AC3, US4, US6-AC3/AC4, US7, US9-AC1/AC2). |
| Scope | `run.go` branches collapse into detect → plan → summarize → confirm → apply; `settings_menu.go`, `configure_*.go`, `starter_management.go`, `include_subtrees.go`. |
| Work | Status summary and change list including detection drift (new languages and roots, new documentation folders). Four-section menu; moving away from a workflow in the Workflow section asks whether to remove it or eject it. `--refresh-docs`, `--eject`, `--restore`. Delete the per-language root prompts, link style, glob, index path, per-surface mode, family policy, and dependency-graph code paths. |
| Tests | Rerun with nothing pending; rerun after adding code outside configured folders; `--check` writes nothing; settings changes for each section; eject blocked by a dependent and allowed when both are named. |
| Exit evidence | Focused tests; expect-driven reruns in the scratch repository. |

### Batch 4: skip suggestions

| Field | Content |
| --- | --- |
| Outcome | Bulky, low-value content is proposed for skipping on first run, rerun, and `--check` (SPEC-0038 US10). |
| Scope | New detection alongside `detect.go`, using the unified ignore matcher and `git ls-files` when available; writes through the existing `.rhizome/ignore` append path in `include_subtrees.go`. Load the `vault-core-subsystem` skill for ignore semantics. |
| Work | Name, pattern, header, and size rules from US10; `# rhizome: suggested skips` section with reasons; `# rhizome: keep indexed <path>` lines for declined skips; skip and stop-skipping actions in "What gets indexed". |
| Tests | Fixture repository with `third_party/`, a minified bundle, a `*.pb.go` file, a 2 MB Markdown file, and an already-ignored `vendor/`: only the first four are proposed; a path with a keep-indexed line is not proposed; a non-terminal run writes them. |
| Exit evidence | Focused tests; `rzm index --explain` on a skipped path names the suggested-skips line. |

### Batch 5: automatic code scope

| Field | Content |
| --- | --- |
| Outcome | Code added after setup indexes without rerunning init, and diagnostics name why a code file is out (SPEC-0038 US9). |
| Scope | `pkg/vault/obsidian/code_config.go` (one place that yields the effective code folders, falling back to the project root when code is on and none are configured); its consumers in `pkg/app/indexing/unified_ownership.go`, `pkg/app/bootstrap/live_ownership_policy.go` and live watch-root registration, `pkg/app/indexing/commands.go`, `CodeAnchorRootsConfigured` and validation applicability, agent capability languages; `cmd/index_explain.go`; init's config writer and rerun change list. Load the `indexing-subsystem`, `vault-runtime-subsystem`, and `code-intel-subsystem` skills first. |
| Work | Effective-folder fallback; stable scope hash for automatic scope; explain reports code status and the deciding setting; init writes `code.enabled: true` without roots; change-list items for "turn on code indexing" and "remove code folder limits". |
| Tests | Docs-only repository, then code added, then `rzm index`: symbols present without rerunning init. Explicit-root repository keeps its limit and its rerun offers removal. Explain for a file in each state. The scope hash is unchanged across two indexes with unchanged config. |
| Exit evidence | Focused tests; `go test -tags=integration` for the affected indexing packages; real binary in a scratch repository reproducing finding 10 with the fix. |
| Escalation | If the live runtime or validation applicability cannot take the fallback without a contract change in their subsystem notes. |

### Batch 6: skill, docs, and integration

| Field | Content |
| --- | --- |
| Outcome | Agents and people find the new flow documented everywhere it is described. |
| Scope | `rhizome` skill references (`configuration.md`, `installation-and-integration.md`, `indexing-and-freshness.md`, new `index-scope.md` and its route in `SKILL.md`), `docs/rhizome-md-templates/RHIZOME.md`, README, getting-started, choosing-your-starter, adapting-rhizome, playbooks, Advanced configuration tuning, Init hub, `pkg/app/cli/init/CONTEXT.md`, `scripts/install/install-rzm.sh` (`initCandidate` becomes plain `init` for pins that support it and keeps today's flags for older pins) and its tests, `docs/engineering/quality-gates.md` (the generated-surfaces gate becomes `rzm init` followed by `rzm init --check`), agent-experience eval scripts, CHANGELOG. |
| Work | Troubleshooting table from this note; remove flag recipes; regenerate this repository's managed surfaces. |
| Exit evidence | `make check`; `make check-full` (indexing touched in Batch 5); `./scripts/rzm validate`; `./scripts/rzm validate frozen-scope-drift`; `make build` then `rzm init` followed by `rzm init --check` exiting 0; final expect-driven walkthrough of first run, rerun with new code, edited skill, and settings. |

## Follow-ups

- `code.disabledLanguages` leaves a language out only under automatic code scope; with named code folders it still only stops init prompts.
- C# coderef scanning is dropped in mixed-language repositories because `NormalizeCodeRefPatterns` has no C# branch while older init versions pruned `**/*.cs` from `code.scan`. New setups no longer write `code.scan`; existing configs with a pruned list are still affected.
- Code with code indexing turned on cannot be switched off through `code.enabled: false`, because the field is omitted when false and a rerun turns code indexing on again; listing every language in `code.disabledLanguages` is the current way out.
- `rzm index --explain` says "indexed" for any Markdown file no ignore rule matches, even when `notes.includes` leaves it out.
- The first-run Code row counts files that the Skip row proposes to leave out.
- `rzm index --status` prints an embeddings provider even when semantic search is off.
- Resolved in Batch 2: the code path that wrote `noteEmbeddings: {provider: ollama}` on docs-only reruns is gone; init now writes only `{enabled, provider}` or nothing.
- Apply built-in ignore defaults without copying them into `.rhizome/ignore`, so improvements reach existing repositories and the file holds only team rules (vault-core ignore precedence change).
- Consider an indexing size limit for single files, independent of skip suggestions.

## Plan Approval

Drew Colthorp approved all eight decisions in chat on 2026-10-01 ("2 y 7 y", after agreeing to 1, 3, 4, 5, 6, and 8), in reply to "Once you OK both, I'll start Batch 1." That approves this plan and the spec revisions in the same working tree on branch `t3code/80ccd4a0` (base `3e2a5d3`). No current user is configured in this vault, so the `plan-approved-by` metadata field stays absent rather than inferred.

## Original Intended Delivery

A first run that asks at most three questions and writes quietly, a rerun that names what changed and applies it with one confirmation, a four-section settings menu, an update rule that only asks about edited files, and `rhizome` skill guidance that lets an agent diagnose and fix setup without config knob recipes.

## Actual Delivered

- A first run with one findings screen (Docs, Code, Skip, Agents, Search), the three-choice workflow question, a Voyage-first key prompt only when no key is available (the Atomic Object key only in bundled builds), one `[Y/n/e]` confirmation, and a grouped summary with next steps.
- Reruns that show the setup and one change list (setting changes, code and doc folders detection finds, skips, generated files, support files), apply it after one `Apply? [Y/n/s]`, and leave suggestions and edited files for a person; `--check` reports without writing and exits 1 only for changes a run without a terminal would make.
- A four-section settings menu: what gets indexed (detect, all Markdown, doc folders, skip, stop skipping, include a Git-ignored folder), semantic search, an agents checklist, and workflow with remove or eject.
- One ownership rule for every generated file, recorded in tracked `.rhizome/generated-files.yml`, replacing rejections, update policies, source fingerprints, and per-skill marker files; retired skills cleaned up; an ejected starter's dependencies frozen with it.
- Automatic code scope: code on without folders covers the whole repository, so code added later indexes on the next `rzm index`; named folders stay supported and reruns offer to remove limits that miss code; `rzm index --explain` reports code status.
- Skip suggestions for vendored, generated, minified, and very large tracked content, written with reasons to `.rhizome/ignore`, with `# rhizome: keep indexed` to opt out.
- The rhizome skill's configuration, installation, and indexing references rewritten around `rzm init --check`, plus a new `index-scope.md`; README, guides, playbooks, the Advanced configuration tuning guide, subsystem notes, the Init hub, CONTEXT, quality gates, the installer's version-gated init candidate, eval scripts, and CHANGELOG updated. This repository's own managed files were regenerated.

## Deviations

- Suggestions are a new category: turning on semantic search once a key exists and a workflow's new default addon apply only after a person confirms. Edited and unknown files likewise count toward `--check` only in a terminal. Both are recorded in SPEC-0038 US3-AC3; without them a non-terminal `rzm init` followed by `rzm init --check` could never pass.
- Decision 9 (2026-10-02) replaced folder-based notes selection: notes default to all Markdown, "Choose doc folders" and "Use what Rhizome detects now" left the settings menu, and removing existing notes or code folder limits became a suggestion.
- `code.disabledLanguages` now leaves a language out under automatic scope (final review finding); with named folders it keeps its older prompt-only meaning.
- Batch 3 first added per-language code drift; Batch 5 replaced it with folder-limit removal, which matches how the indexer treats folders as one union.
- The agents checklist has no separate AGENTS.md switch, and Codex adds nothing beyond the shared files because no command templates ship today.
- Regenerating this repository needed one grouped answer for the core `SKILL.md` copies, which had no record yet; they matched the committed generated output, so the update was taken.

## Execution Notes

- 2026-10-02: Output polish after an independent UX review (Fable, grade B before the pass) of fresh transcripts. Applied: a run without a terminal prints the same findings plus the workflow chosen; reruns group Changes, Needs a decision, and Suggestions the same way in every mode, list up to five paths per line, and say "Proposed setup" after settings edits; results report outcomes in the past tense ("Semantic search on (Voyage AI)", "Kept your version of ...") including created support files; one Next block replaces the bare "Run rzm index" line and the stray "Recommended validation" line, and it names `rzm validate <selector>` when schema, saved queries, or views changed; the search hint moved from before the confirmation into Next; `--accept-suggestions` lets an agent apply suggestions after the person agrees; the settings row reads "34 notes · Go, TypeScript", the search options mark the current choice, the workflow option 3 has a description, moving off a workflow defaults to keeping its files, and ejected starters read "Agentic Engineering files kept for your team"; the diff explains its markers; `−` became `-`. Building the index at the end of `rzm init` was left for Drew and decided below. Regenerating this repository added `web/tests/e2e/fixtures/` as a suggested skip.

- 2026-10-02: PR #5 review fixes (code review on two axes, then Greptile). A config with only `tsParseTimeout` read as code off in init while the indexer treated it as on; `LocalCodeConfig.IndexesCode` and `LanguageBlocks` are now the one reading. The first run shows findings before asking about Git-ignored folders and counts an included folder's notes and code. Eject previews name the remaining workflow and every starter left unmanaged, including Core identity. `--restore` of a starter the chosen workflow no longer installs selects it again instead of removing its files. `--search ollama` keeps a configured remote server. `--check` no longer reports a removal that a kept, edited skill router blocks, so it can become clean. A missing, unreadable, or stale `generated-files.yml` and leftover marker files now count as pending, so a rerun repairs them (this repository's record was stale after merging main's custom-views skill update). Typed skip paths go through `pkg/paths` (`./cmd` wrote `/./cmd/`). `rzm index --explain` names `code.disabledLanguages` under automatic scope. The dead skill overlay manifest option is gone, and `rerun.go`, `file_plan.go`, and `first_run.go` were split under the 500-line guideline. Left as is: the v0.49 workflow-file migration that runs before the first prompt predates this effort.
- 2026-10-02: Drew chose to offer indexing with a default of yes. A terminal first run asks "Build the search index now? [Y/n]" after the summary and runs the same path as `rzm index` (it starts the vault runtime); a rerun asks "Update the search index now?" only when it applied changes. Next drops the `rzm index` step once indexing finishes. Runs without a terminal never index. A failed index keeps the setup, prints "Indexing stopped", and exits nonzero. Verified with the real binary in a fresh sample repository with no key: the prompt, streamed progress, and a Next block without `rzm index`.
- 2026-10-02: Decision 9 implemented. First runs write no notes folders and code without folders; `notesLimitDrift` and `codeDrift` are suggestions; "What gets indexed" offers skip, stop skipping, include a Git-ignored folder, and remove folder limits when a config has them; `testdata`, `fixtures`, `__fixtures__`, and `__snapshots__` join the skip suggestions with the reason "test fixtures"; the Docs row says where notes are ("all Markdown (34 notes in docs/, guides/, and 3 top-level files)"). Watcher measurement on two copies of this repository during `rzm index --rebuild` (about 46 seconds each, macOS FSEvents, the runtime's root registration): with old-style limits (notes `docs/**`, Go roots `cmd`, `pkg`) the hub received 54 backend events, dropped 51 under `.rhizome/`, delivered 1, and used 11 ms of CPU over 4 watch paths; with the new defaults it received 59, dropped 56, delivered 1, and used 11 ms over 2 watch paths. The live runtime already watches the whole repository root for notes, so whole-repository indexing adds indexing work for newly included files and no watcher load. The 3 `.rhizome/config.yml` rename events seen during a rebuild are FSEvents history flags; the file's inode and modified time do not change.

- 2026-10-02: Captured real walkthroughs for review (first run with and without a key, the other-provider branch, the edit path, a run without a terminal, up-to-date and drifted reruns, `--check`, the settings tour, an older-setup upgrade, `--explain`, removed flags). Fixes found while capturing: a typo at a fixed-choice prompt picked an option (at the unknown-files question it meant "update them all"), so every fixed-choice prompt now asks again; the Search row reads "off until a key is available" or "off (a key is available)"; `--check` lists a missing `.rhizome/ignore` as created and the retired `template-rejections.yml` as removed; a block added to an existing `AGENTS.md` counts as an update; a single skipped file no longer repeats its path.

- 2026-10-01: Batch 6 implemented. Helpers rewrote the rhizome skill references (new `index-scope.md`, reorganized `configuration.md`, a troubleshooting table in `installation-and-integration.md`, automatic scope in `indexing-and-freshness.md`) and the user docs (README, getting started, choosing a starter, adapting, how Rhizome works, three playbooks, Advanced configuration tuning, and two specs' flag mentions); a third rewrote the stale Init agent-surfaces guide table. The installer's init candidate is plain `init` for pins after v0.50.5 and keeps the old flags for older pins; its `.rhizome/.gitignore` allowlist now names `workflows.yml` and `generated-files.yml`. Eval scripts use `--workflow` and `--agents codex`. Quality gates now read `rzm init` then `rzm init --check`.
- 2026-10-01: Final independent review (Fable) found seven issues, all fixed with tests: automatic scope indexed languages listed in `code.disabledLanguages`; `--check` stayed red after a non-terminal run whenever a skill was edited; `--search` with the same provider dropped model and endpoint overrides; "Use what Rhizome detects now" dropped other code settings; folders written as `./pkg` counted as missing code; a TypeScript block without folders plus a JavaScript block with folders read as automatic scope; a cancelled first run said nothing was written after a pasted key was saved. Also: the grouped unknown-file question names mirrored copies, and a rerun where someone keeps every edited file says "Nothing changed." instead of suggesting `rzm index`.
- 2026-10-01: Final verification: `make check-full` passes every step except `TestAmbiguousLinkGitProvenanceUsesSourceBranchAncestryNotAuthorDates` (fails on the base commit too); `make web-test` and `make credential-check` pass; integration tests for indexing, bootstrap, cache, and anchors pass; installer tests (29) and eval script tests (42) pass; `./scripts/rzm validate` and `frozen-scope-drift` report 0 issues. `make build`, then `rzm init` in this repository, then `rzm init --check` exits 0. A final expect-driven walkthrough covered a first run with a Skip row, code added later (no change needed), an edited skill without and with a terminal, and keeping it.

- 2026-10-01: Batch 5 implemented. `applyAutomaticCodeScope` in `pkg/vault/obsidian/code_config.go` is the one place that yields code folders: with code on and no language block naming folders, every language root is `.` and `codeanchor.Config.AutomaticScope` is set; a block without folders also covers the project; named folders keep limiting code indexing to their union. Every consumer already read roots through `LoadCodeConfig`, so the indexer, live runtime, validation applicability, and capabilities follow without their own fallbacks. Two loops that walked once per language root (`RunCodeIndexCommand` and the live WatchHub registration) now use the deduplicated `Config.CodeRoots()`. Python and PHP module naming keep the indexers' default source folders under automatic scope, so FQNs keep their shape. `ScopeConfigHash` is computed from the authored config, so automatic scope cannot churn it. `rzm index --explain` reports code status and the deciding setting, and `rzm index --status` prints the folders plainly. Init writes `code: {enabled: true}`; reruns turn code on for configs without it and offer to remove folder limits when detected code files sit outside them. The `code_anchors` not-applicable message now points at `rzm init --check`, folder limits, and `code.disabledLanguages`. Subsystem notes updated: code-intel (new constraint), indexing, validate.
- 2026-10-01: Batch 5 verification: a new `pkg/app/indexing` test indexes a docs-only setup, adds Go and TypeScript in folders init never saw, and finds both files and the Go symbol after the next run; it fails without the change. Config-merge, `--explain`, init rerun, and drift tests cover each state. Real binary: a docs-only repository set up with `--search off`, then Go code added and `rzm index` run, returns `example.com/demo/tools/hello.Hello` from `rzm code symbols`, and `--explain` says the file is indexed as Go code because code indexing covers the whole repository (finding 10 fixed).

- 2026-10-01: Batch 4 implemented in `skips.go`. Detection takes the files code detection walked (already excluding ignored and hidden paths), keeps those Git tracks, and proposes the US10 rules; indexable means a source extension or a match for the notes includes, so init still never branches on note extensions. Hidden folders such as `.next/` are already pruned by every walker, so they are never proposed. Entries are written as anchored, escaped patterns under `# rhizome: suggested skips` with the reason on the line above; stopping a skip removes the entry with its reason line and records `# rhizome: keep indexed <path>`. Stopping a path that another rule skips explains that rule (Git ignore, built-in list, or a file and line) instead of overriding it. Verification: `go test ./pkg/app/cli/init/ ./pkg/noteformat/ ./cmd/` pass; `skips_test.go` covers the fixture from the plan (vendored folder, minified bundle, `*.pb.go`, a generated header, a 2 MB Markdown file, an already-ignored `vendor/`, an untracked large file, a large non-indexable JSON file), keep-indexed paths, first-run writes with `Explain` naming the line, rerun proposals and `--check`, and both settings actions. Real binary: the first-run Skip row, the summary line, and `rzm index --explain` reporting `.rhizome/ignore:36 '/third_party/'`.
- 2026-10-01: Full `make test-fast` after Batch 3 found a new `pkg/noteformat` architecture violation (the Docs summary branched on `.md`); fixed by matching include globs only. The only other failure is the pre-existing Git-provenance test.

- 2026-10-01: Batch 3 implemented. `rerun.go` replaces the "Found existing config. Reconfigure it?" path and the old menus: `Run` now branches into `firstRun` and `rerun`, which share `resolve` (final config plus classified generated files, no writes) and `apply` (generated files, then config). A rerun prints the setup summary and one change list built from setting changes (summary rows that differ), detection drift (code outside configured language folders, doc folders a limited notes setting misses), a missing embeddings provider, the version pin, generated files, and `.rhizome` support files; `Apply? [Y/n/s for settings]` covers all of it, and `--check` prints the same report and returns `ErrChangesPending` (exit 1 in `cmd/init.go`). `settings.go` holds the four sections; the Workflow section asks to remove or eject a workflow someone moves away from, and choosing a workflow again restores it. `interactive_flow.go`, `settings_menu.go`, `onboarding_preview.go`, `credentials.go`, the per-setting `configure*` prompts, and their tests are deleted. Decisions made during implementation, now in SPEC-0038 US3-AC3: turning on semantic search when a key appears and a workflow's new default addon are suggestions that only a terminal run applies (`--check` lists them without counting them), so a non-terminal `rzm init` followed by `rzm init --check` exits 0; choosing doc folders records the detected folders left out as note excludes so drift does not re-add them; `code.enabled` with no language blocks counts as covering every language, which matches Batch 5's automatic scope.
- 2026-10-01: Batch 3 fixes found by tests and the real binary: ejecting a workflow removed the core starter's AGENTS.md block, because only the ejected ids were frozen; `frozenStarters` now freezes their dependencies too. Choosing "Search and agent guidance only" did not stick, because an empty template list fell back to inferring the workflow from starter docs left on disk; a recorded `.rhizome/workflows.yml` now counts as the choice. The change list missed AGENTS.md rewrites that only reorder blocks; `pending()` now renders each shared doc the way apply will. The first run rendered guidance from an unpruned config (`code.scan`), so the first rerun always updated AGENTS.md; the first run now prunes before rendering. Summary labels lost alignment with colors on, and the Docs row said "all Markdown" whenever an effort HTML include was present.
- 2026-10-01: Batch 3 verification: `go test ./pkg/app/cli/init/... ./pkg/app/credentials/... ./cmd/ ./pkg/search/qualityeval/...` pass; `make lint vet` pass; `golangci-lint` reports nothing new in the changed packages. New tests in `rerun_test.go` cover an up-to-date rerun, `--check` on first runs and reruns, an edited skill without a terminal, code added after setup, a new doc folder, code drift rules, addon suggestions (`n` writes nothing, `Y` installs), remove versus eject in settings (and that the choice sticks), the agents checklist, turning search off, chosen doc folders staying chosen, and a skill collision that writes nothing until `--agents none`. Expect-driven runs of the real binary in scratch repositories: an up-to-date rerun, an edited skill mirrored in two folders (one question, "keep" remembered, `--check` then exits 0), Cursor turned on from settings, eject from the Workflow section, search turned off and then declined with `n`, and a first run through `e` into settings and back.
- 2026-10-01: Batch 1 implemented. New `generated_files.go` (record, LF-normalized 64-bit fingerprints, legacy marker folding) and `file_plan.go` (planner, classification, terminal question UI, applier). `agent_surfaces.go` and `planTemplateScaffold` now only plan; `diff/updater.go`, `diff/prompt.go`, `diff/rejection.go`, `diff/fingerprint.go`, and `starter_updates.go` are deleted; `--skip-rejected`, `--reject-all`, and `--clear-rejections` are removed. Decisions made during implementation, recorded in SPEC-0039 and SPEC-0038: a managed block with no record counts as Rhizome's (otherwise every upgrade would ask about AGENTS.md); an edited file with no newer Rhizome version is left alone silently; inside a Rhizome-owned stale skill folder only recorded files are removal candidates; an unreadable record skips the user-skill collision refusal. The search-quality development corpus negative judgment for the renamed `template_update_authority.go` now points at `ontology_identifier_contracts.go`.
- 2026-10-01: Batch 2 implemented. `first_run.go` adds the findings screen, the three-choice workflow question, the Voyage-first key prompt (Atomic Object key only in bundled builds, recognized by trial decryption through `teamkeys.Unlocks`), `other` for OpenAI/Ollama/off, the `[Y/n/e]` confirmation, installed-agent detection on `PATH` (stored as `on`), and a grouped summary printed before the pinned-binary download. Semantic search config is now only `noteEmbeddings: {enabled, provider}`; models and endpoints come from load-time defaults, and an absolute index path is no longer written. `RunOptions.Interactive` (stdin is a terminal) replaces `Yes`; new flags `--workflow`, `--agents`, `--search`, `--refresh-docs`, `--eject` (several names), `--restore`; removed flags fail with their replacement through `initFlagError`. `credentials.Session` gained `OffersTeamKey`, `ProvideKeyFor`, and `WithTeamKeyBundle`, and its prompt is now one line. Fixed along the way: `--binary-manager external` was dropped by the first run, a `Skill overlays: N fragments` debug line, and the old `Workflow templates: … (resolved …)` line. A non-terminal first run installs the recommended Agentic Engineering workflow. The managed AGENTS.md block's repair command now names `init --agents <agents>`. Detection counts notes by matching the configured include globs against walked files, which keeps init free of concrete note-provider selection (`pkg/noteformat` architecture rule); two dead exemptions for deleted functions were removed from that rule.
- 2026-10-01: Batch 2 verification: `go test ./pkg/app/cli/init/... ./pkg/app/credentials/... ./pkg/teamkeys/... ./pkg/noteformat/... ./cmd/` pass; `make lint vet` pass; full `make test-fast` passes except the pre-existing Git-provenance failure. Real binary in scratch repositories: interactive first run with and without a key, `--binary-manager external`, a non-terminal first run, and a rerun that changes nothing all behave as designed.
- 2026-10-01: Batch 2 finding: `rzm index` stops with an error when semantic search is on and the key is missing, so selecting Voyage without a key would break indexing. Semantic search now stays off until a key is available, and a rerun offers to turn it on (SPEC-0038 US1-AC7).
- 2026-10-01: Foundation review with Drew. (1) Record format: approved. (2) Unrecorded managed blocks count as Rhizome's: approved. (3) Retired skills: "remember and clean up the old skills that we don't want anymore", so retired skills are now removed without asking (retired `rhizome-*` names by name; generic retired names with a record or marker), added as SPEC-0038 US11. (4) Support both removing and ejecting a workflow: removing (choosing another workflow) deletes unedited skills, blocks, saved queries, and views and keeps docs and schema; ejecting keeps every file for the team to edit. Drew is fine keeping the word "eject", so `--eject`/`--restore` stay. Removal of saved queries and views is implemented (`planRemovedStarterFiles`); the remove-or-eject choice in the settings menu is Batch 3 work.
- 2026-10-01: Independent code review of Batch 1 found eight issues; all fixed with tests: old skill markers are deleted only after the record owns the folder (a run that kept every file previously left the next run refusing to proceed); unknown removals get their own question that defaults to keep, and a name alone no longer marks an old skill folder as Rhizome's; an edited block of a starter that was turned off goes through the edit check instead of being stripped; a clean clone without the record accepts a `rhizome` skill whose SKILL.md matches this version; a kept removal is remembered; the ontology fingerprint ignores only identifier contracts a refresh carries forward; record pruning keeps entries behind a broken folder symlink; and rhizome-* command cleanup moved from planning into the apply step so planning has no side effects. Retired-field clearing is skipped under `--no-config`, which Batch 2 removes.
- 2026-10-01: Batch 1 verification. `go test ./pkg/app/cli/init/... ./cmd/` pass; `make lint vet` pass; `make test-fast` passes except `TestAmbiguousLinkGitProvenanceUsesSourceBranchAncestryNotAuthorDates`, which also fails on a clean archive of `3e2a5d3` (pre-existing, as recorded in EFF-2026-09-29-14-46). Real-binary checks in scratch repositories: a fresh interactive first run created 135 files with no per-file prompts; reruns are idempotent; an older unedited reference updated silently; an edited skill mirrored in both folders was asked about once, the diff displayed, and "keep" was not asked again; an upgrade from the previous build folded 28 marker files and deleted `template-rejections.yml`. `make check-fast` cannot run its web steps in this worktree (`openapi-typescript` not installed), so its Go steps were run directly.
- 2026-10-01: Drew agreed to decisions 2 and 7, completing plan approval. Starting Batch 1.
- 2026-10-01: Drew agreed to decisions 1, 6, and 8 and asked what decision 7 means.
- 2026-10-01: Drew liked the generated-files record and asked whether it could remove the small files in each skill folder; it can, and Batch 1 now retires `.rhizome-managed` and `.rhizome-managed-files`. Drew also did not want people to deal with a plan-then-apply workflow or run init twice. `rzm init` now finishes in one run (one confirmation in a terminal, none without), and `--check` replaces `--plan`/`--apply` as a preview.
- 2026-10-01: Drew agreed to decisions 3, 4, and 5, found `--yes` unclear about what it accepts, and asked what `init-state.yml` is for. Replaced `--yes`/`--dry-run` with `--plan`/`--apply`, renamed the record to `.rhizome/generated-files.yml` with a stated purpose, and moved skip decisions into visible `.rhizome/ignore` comments.
- 2026-10-01: Drew asked that the Atomic Object key be offered only when team keys are bundled into the binary, and suggested first when they are. Source and public builds have an empty bundle (`pkg/teamkeys/keys_encrypted.go`), yet today's prompt always offers the key. Updated SPEC-0038 US1-AC2, US5-AC3/AC5, the requirements, the transcript, and Batch 2.
- 2026-10-01: Independent review (Fable advisor) agreed with the direction and found that a Go root of `.` already indexes TypeScript added later. A rerun of the experiment confirmed it; the original test file had no symbols. The real gap is a repository with no `code` section or narrower roots, reproduced in finding 10. The review also asked for LF-normalized hashes, sorted mergeable records, starter-doc creation records, one question per mirrored skill, and defined non-terminal behavior; all are now in the specs and Batch 1.
- 2026-10-01: Drew asked that agents be able to configure Rhizome more deeply than init exposes, especially ignore rules for bulky checked-in content, and was open to handling ignores in init. Added skip suggestions (US10) and the index-scope skill reference.
- 2026-10-01: Reviewed the current build in scratch repositories through an expect-driven pseudo-terminal and traced `pkg/app/cli/init`, `pkg/app/cli/init/diff`, and code-config consumption. Findings are recorded under "Review of the current experience". Drafted revisions to SPEC-0038, SPEC-0045, and SPEC-0039.

## Closure Checklist

- [x] Plan approved in chat.
- [x] Foundation review after Batch 1.
- [x] All batches' exit evidence recorded.
- [x] Gates run: `make check-full`, documentation validation, frozen-scope drift, `rzm init` then a clean `rzm init --check`.
- [x] Specs, hub, CONTEXT, skill references, and CHANGELOG updated.

## Status

Implementation complete and verified; uncommitted in the working tree, awaiting Drew's review before commit and closure.

## Compounding Follow-ups

None yet.

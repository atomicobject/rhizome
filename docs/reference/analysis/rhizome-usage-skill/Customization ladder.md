---
type: ReferenceDoc
summary: "Evidence for the usage skill's advanced half: every layer a user can customize in Rhizome, ordered from no-code to code, with each rung's purpose, files, commands, prerequisites, pitfalls, and the documentation gaps a newcomer would hit."
reference-kind: analysis
status: draft
last-verified: 2026-11-15
---

# Customization ladder

This note collects evidence for the part of the Rhizome usage skill that goes past starters. It orders what a user can customize from the least effort and risk to the most, says what each rung is for, and records where the shipped guidance is missing, stale, or wrong. Two companion notes go deeper: `docs/reference/analysis/rhizome-usage-skill/Schema evolution and views for custom workflows.md` works one realistic scenario end to end, and `docs/reference/analysis/rhizome-usage-skill/Code mode and automation.md` covers code mode, actions, scheduled tasks, and module lifecycle.

## How this was checked

Statements marked **verified** were checked against code, `rzm --help` output from a fresh `make build NO_WEB=1` of this checkout (v0.52.1), or a run against a scratch vault in the session scratchpad. That vault had one team module (`sales`) with an ontology, a view, a recipe, a task, and an action. Statements marked **inference** are reasoned from the specs and docs and were not exercised. The globally installed `rzm` on this machine is v0.50.5 and lags the checkout, so help text from it would mislead.

## The ladder at a glance

| Rung | What the user changes | Typical ask | Main files | Risk |
| --- | --- | --- | --- | --- |
| 0 | Personal view choices in the app | "Sort this by due date and remember it" | none (ignored `.rhizome/user-state.sqlite`) | none |
| 1 | Team policy and skills | "Our specs keep landing in different folders" | `docs/engineering/*.md`, `.agents/skills/**` | low |
| 2 | Index scope, search, and configuration | "Search is full of vendored code" | `.rhizome/ignore`, `.rhizome/config.yml` | low to medium |
| 3 | Typed Markdown inside the existing schema | "Add a story to this spec" | notes | low |
| 4 | Shared native views | "Show active efforts as a board by owner" | `.rhizome/views/*.yaml` or `<module>/views/*.yaml` | low |
| 5 | Query recipes | "Give agents one call for my open action items" | `.rhizome/query-recipes/*.yaml` or `<module>/recipes/*.yaml` | low |
| 6 | Schema evolution | "Track sales opportunities by stage" | `.rhizome/ontology/*.graphql` or `<module>/ontology/*.graphql` | high |
| 7 | Code-mode scripts and skill scripts | "Brief me on every stalled deal" | `scripts/*.js` in a skill or module | medium |
| 8 | Custom HTML/TSX views and experiences | "A pipeline home page with a deal panel" | `<views folder>/<folder>/*.tsx`, `experience.yml` | medium (trusted code) |
| 9 | Module actions and scheduled tasks | "Every week, flag deals with no call booked" | `<module>/actions/*.yaml`, `<module>/tasks/*.yml` | medium to high (runs commands and agents) |
| 10 | Packaging as a module, ejecting a starter | "Make this a reusable capability" | `.rhizome/modules/<name>/**` | medium |

The rungs are not a strict sequence. Rung 4 needs no schema change, and many requests stop at rung 3 or 4. Rungs 6 and above change contracts other people and agents depend on.

## Rung 0: personal view choices (no files)

- **For:** reshaping what one person sees without changing the team's defaults.
- **Verified:** native views remember filters, presets, sort, grouping, columns, density, widths, board fields, and expansion per definition and concrete subject in ignored `.rhizome/user-state.sqlite` (`pkg/app/cli/init/templates/skills/markdown/rhizome/references/views.md`, "Personal settings and shared configuration"). Reset returns to the repository defaults.
- **Bridge to rung 4:** the workspace's explicit save (`POST /api/v1/views/{id}/save`) promotes the reviewed layout to YAML. From a generated view it writes `.rhizome/views/<type>.yaml` with `replaceGenerated: true`; from an authored view it rewrites only `defaults` and `variants` (`pkg/app/views/CONTEXT.md`, "Boards and Save"). This is the no-code path to a shared view and the skill should mention it before offering to write YAML.
- **Pitfall:** an earlier personal choice can mask a newly authored default. The fix is to pick the view the switcher marks with a dot, which clears the remembered choice (`views.md`).

## Rung 1: team policy and skills

- **For:** changing how agents work rather than what Rhizome stores.
- **Verified:** Agentic Engineering installs editable policy documents under `docs/engineering/` and skills under `.agents/skills/` and `.claude/skills/` (`pkg/app/cli/init/templates/starters/agentic-engineering/repo/docs/engineering/`). `rzm init` keeps edited managed files and asks about them; `rzm init --eject <starter>` hands a starter to the team and `--restore` resumes updates (`docs/reference/guides/adapting-rhizome.md`, `rzm init --help`).
- **Pitfall:** generated copies under `.agents/skills` and `.claude/skills` are managed. Editing them is allowed in a downstream repository, but the team must then review each update through `rzm init`. Repository preferences belong outside the managed blocks in `AGENTS.md` and `CLAUDE.md`.

## Rung 2: index scope, search, and configuration

- **For:** making search find the right things and skip noise; turning on semantic search.
- **Files:** `.rhizome/config.yml` (notes includes/excludes, code roots and disabled languages, embeddings, validation suites, `tasks:` defaults), `.rhizome/ignore`. Init owns `.rhizome/workflows.yml` and `.rhizome/generated-files.yml`; users should not edit them (`references/configuration.md`).
- **Commands:** `rzm init --check` (writes nothing, exits 1 when it would change something), `rzm index --status`, `rzm index --explain`, `rzm init --search voyage|openai|ollama|off`.
- **Verified:** this repository's own config is a working example: `notes.includes` with `docs/efforts/**/*.html`, per-language code roots, `disabledLanguages`, Voyage embeddings for notes and code, and `validation.brokenLinks.placeholders: strict` (`.rhizome/config.yml`).
- **Pitfalls:** an ignore-file edit forces a full resync on the next index; `rzm index` fails when search is on and its key is missing; the skill must never read or print keys. Expert fields are documented only in `docs/reference/guides/Advanced configuration tuning.md`, which does not install downstream; `references/configuration.md` links to it on GitHub.

## Rung 3: typed Markdown in the existing schema

- **For:** writing notes that queries, views, and validation understand, without changing the schema.
- **Sequence (verified in `references/structured-markdown.md`):** `rzm agent ontology-inspect`, `rzm agent ontology-authoring-guide --type <T>`, a recipe or `ontology-query-schema` for related context, `rzm agent next-id --type <T>` when the type allocates identifiers, draft, then `rzm validate <check>`.
- **Pitfall:** heading order and subtree boundaries are schema. `key:: value` inline properties and `- key:: value` metadata bullets are different authoring styles, and the `@authoring` directive decides which one validation accepts.

## Rung 4: shared native views

- **For:** tables, cards, and boards over one type, interface, or recipe, with defaults the whole team sees.
- **Files:** `apiVersion: rhizome.view.v1` YAML with `source` (`ontology_type`, `ontology_interface`, `query_recipe`, or `custom`), `mount`, `defaults`, `filterPresets`, and `variants.table|card|kanban` (verified in `pkg/ontology/viewconfig/types.go`).
- **Mount kinds (verified):** `type`, `interface`, `group`, `workspace`, `node`, `standalone`, and `panel`. `views.md`'s mount table omits `panel`, which only experiences use; `custom-views/references/experiences.md` documents it.
- **Commands:** `rzm agent view list`, `rzm agent view show <id>`, `rzm agent view run --id <id> --omit-capabilities`, `rzm validate views`, `rzm view eject <bundled id>`.
- **Verified pitfall:** `rzm agent view run <id>` with a positional id fails with `view not found: ` and an empty name, because `run` takes `--id` while `show` takes a positional argument.
- **Inconsistency:** `views.md` says to mount a single-type view on that type rather than as `standalone`, but the shipped `action-items.open` view (`.rhizome/modules/action-items/views/action-items.yaml`) and this repository's `reference.subsystem-guidance` view are standalone views over one type. The skill should explain the rule and not treat the starter as the model.

## Rung 5: query recipes

- **For:** one named, validated, bounded question that agents, views (`source.kind: query_recipe`), and the command line can all run.
- **Format (verified in `pkg/ontology/queryrecipe/types.go`):** `apiVersion: rhizome.query-recipe.v1`, `id`, `name`, `problem`, `inputSpec` (`mode`: `none`, `optional_anchor`, `required_anchor`, `multi_anchor`; `inputs` with `name`, `kind`, `required`, `description`, `default`), `query.graphQL`, `outputContract` (`expectedPaths`, `rowPath`, `empty`, `partial`, `highVolume`), `adaptationGuidance` (`summary`, `rules`), optional `examples` and `tags`.
- **Verified:** a recipe inside a module becomes a command: `rzm <module> <recipe-id> --<input> <value>`; `rzm sales` listed `upcoming-calls` in the scratch vault.
- **Gap:** no shipped skill reference documents this file format. The rhizome skill's route table sends "Change ontology SDL, migrations, or recipes" to `references/ontology-authoring.md`, which only says to validate affected recipes. An agent asked to write a recipe must copy an existing file, such as `.rhizome/modules/action-items/recipes/action-items.yaml`.

## Rung 6: schema evolution

This rung has its own note. The short version:

- **Where SDL goes (verified):** top-level `*.graphql` files in `.rhizome/ontology/` and in each `.rhizome/modules/<module>/ontology/` (`docs/reference/subsystems/ontology.md`). A team can create its own module folder; init manages only the files it recorded in `.rhizome/generated-files.yml`, and the managed `.rhizome/.gitignore` allowlists `modules/**`.
- **Stale guidance:** `references/ontology-authoring.md` opens with "Changing `.rhizome/ontology/*.graphql`", and the unshipped `docs/rhizome-md-templates/Ontology schema authoring.md` says ontology files live there. Since SPEC-0130, every starter's schema lives in its module, and this repository has no flat `.rhizome/ontology` folder at all.
- **Missing reference:** no downstream skill reference lists the directive grammar (`@node`, `@field`, `@link`, `@contains`, `@source`, `@identifier`, `@requiresWhen`, and others). The complete list exists only in code (`pkg/ontology/schema/schema_sdl.go`) and in the in-repo template above, which nothing embeds downstream.
- **No UI editor (verified):** every `/api/v1/ontology/*` route is a GET (`pkg/app/web/server_http.go`). Schema change is agent or text-editor work, which makes it a natural job for the in-app chat.

## Rung 7: code-mode scripts and skill scripts

- **For:** answers that need several Rhizome reads, and repeated activities whose retrieval logic should not be rebuilt each time.
- **Verified:** `rzm agent code execute` runs a JavaScript body with a connected `rzm` client, `--input`/`--input-file`, `--read-write`, and `--timeout` up to 24h. The surface has 37 operations; the write-capable ones are `note_move`, `validate` fix apply, `current_user` set, `view` eject, and `action_run`. There is no general note-edit operation.
- **Details:** `docs/reference/analysis/rhizome-usage-skill/Code mode and automation.md`.

## Rung 8: custom views and experiences

- **For:** pages the native layouts cannot express, such as dashboards, record pages, triage tools, and a site with a sidebar panel.
- **Files:** a definition with `source: { kind: custom, entry: <file>.tsx|.jsx|.ts|.js|.html }` beside its entry. The kit is `@rhizome/kit`, and bare imports resolve only through the import map (`react`, `react-dom`, their JSX and client entries, `@tanstack/react-query`, `lucide-react`, `@rhizome/ui`, and `@rhizome/kit`); anything else needs a full URL and network access (`custom-views/SKILL.md`). No install or bundler step exists.
- **Experiences:** an `experience.yml` in a views folder with `title` (required), `group`, `types`, `panel`, and `tailwind` (verified in `pkg/ontology/viewconfig/experience.go`) makes the folder one site in one frame with a sidebar panel. The Delivery experience (`.rhizome/modules/agentic-engineering/views/delivery/`) and the AO KB Knowledge site (`pkg/app/cli/init/templates/starters/ao-kb/module/views/knowledge/`) are working examples.
- **Writes:** views edit records through `useSetField`/`setField` inside an `EditSession`, which goes through ontology edit replay, not raw file writes (`custom-views/references/kit-api.md`).
- **Bundled starting points:** `rzm view eject group.briefing` (or `group.overview`, `group.trace`, `group.sections`, `type.briefing`, `interface.briefing`, `workspace.briefing`, `workspace.overview`) copies the shared folder into `.rhizome/views/group/`.
- **Pitfalls:** custom views run with the user's full authority, like a build script. A query held in a constant is not checked by `rzm validate views`; only an inline literal passed to `graphql` or `useGraphQL` is. Pages in an experience must read their subject through hooks because hidden pages re-render. A `NO_WEB=1` build has no bundled views (verified: the scratch vault's catalog had no Briefing), so dogfooding view work needs a full build.

## Rung 9: actions and scheduled tasks

- **Actions (verified):** `.rhizome/modules/<module>/actions/<name>.yaml` with `description`, `inputs` (`string`, `int`, `bool`, `list`, optional `required` and `enum`), `run.command` or `run.script`, and `timeout` (default 60 s, at most 5 min). The same action is reachable from views (`useActionQuery`, `useActionCommand`), agents (`action_list`, `action_run`), and people (`rzm <module> <action>`). Every run requires a trusted repository. Actions exist only inside modules.
- **Scheduled tasks (verified):** `.rhizome/tasks/<id>.yml` or `.rhizome/modules/<module>/tasks/<name>.yml` with `name`, `every`, optional `description`, `scope` (`person` or `team`), `check` (`command` or `script`), `prompt`, and `timeout`. A task needs a check, a prompt, or both (`pkg/vault/taskdef/taskdef.go`). Scheduled runs read the primary branch only; `rzm tasks run --file` tests a draft.
- **Pitfall:** both rungs execute code and agents on teammates' machines. A task prompt spends the person's model budget; Rhizome does not meter it.

## Rung 10: modules and ejected starters

- **For:** shipping a capability as one folder: ontology, recipes, views, tasks, scripts, and actions (SPEC-0130, `docs/specs/technical/rhizome-modules.md`). A module has no manifest; its name is the folder name and must match `^[a-z0-9][a-z0-9-]{0,62}$` and not equal a built-in command.
- **Lifecycle (verified):** an optional `.rhizome/modules/<id>/lifecycle.yml` names `observe`, `setup`, `index`, and `verify` actions plus watched `inputs`, and the runtime drives readiness from them (`docs/reference/module-lifecycle.md`). AO KB is the only shipped user (`pkg/app/cli/init/templates/starters/ao-kb/module/lifecycle.yml`).
- **Limits:** skills, `AGENTS.md` blocks, installed documents, and hook declarations stay outside modules. A team module therefore cannot ship its own skill through the module folder; it must check the skill into the harness skill folders.

## Gaps a newcomer would hit

1. **Most advanced guidance never installs downstream (verified).** `adapting-rhizome.md`, `build-an-experience.md`, `scheduled-tasks-guide.md`, `module-lifecycle.md`, and `Advanced configuration tuning.md` live only in this repository's `docs/reference/`. Starters install only their own guides (`action-items.md`, `ao-kb.md`, `compounding.md`, `core-identity.md`, `complex-domain-workflow.md`, and the agentic-engineering guides). Downstream agents see only the `rhizome` and `custom-views` skills.
2. **The `rhizome` skill has no route for tasks, actions, modules, or recipe authoring (verified).** No reference under `pkg/app/cli/init/templates/skills/markdown/rhizome/references/` mentions `rzm tasks`, `.rhizome/tasks`, or module actions. Actions appear only in `custom-views` references, framed as something a view calls.
3. **No directive reference ships (verified).** See rung 6.
4. **Schema location guidance predates modules (verified).** See rung 6.
5. **Bulk property migration commands are undocumented for agents (verified).** `rzm note properties rename|set|delete` and `rzm note tags rename`, each with `--dry-run`, exist, but no skill reference or guide mentions them.
6. **`choosing-your-starter.md` omits AO KB (verified).** AO KB ships as an optional add-on (`optionalAddon: true` in `pkg/app/cli/init/templates/starters/ao-kb/template.yaml`), installed with `rzm init --add-addon ao-kb`, and the guide does not list it.

## Implications for the skill (inference)

- The skill should present the ladder as "start at the lowest rung that answers the question" and say what each rung costs. Most "I want a board" requests are rung 0 or 4, not rung 6.
- Because downstream repositories lack the guides, the usage skill must either carry the essentials of rungs 5, 9, and 10 itself or route to new references inside the `rhizome` skill. It should not link to `docs/reference/guides/...` paths that do not exist downstream.
- Schema evolution is the rung where the skill most needs a procedure and a stop point: propose the smallest extension, test it on representative notes, show the validation output, and get approval before migrating existing notes.

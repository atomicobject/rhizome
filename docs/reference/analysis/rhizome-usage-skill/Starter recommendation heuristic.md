---
type: ReferenceDoc
summary: "Draft heuristic for inspecting a repository and recommending Rhizome starters: which repository signals point to which starter, the order to weigh them, and what to ask the user when signals conflict."
reference-kind: analysis
status: draft
last-verified: 2026-11-15
---

# Starter recommendation heuristic

This is a draft for the council, not settled behavior. Rhizome today has no content-based recommender: init's recommended workflow is always Agentic Engineering, and its detection looks only at Markdown, code languages, agent folders, `.obsidian`, ignored nested repositories, and bulky files (`pkg/app/cli/init/detect*.go`, `pkg/app/cli/init/first_run_prompts.go`). Nothing in init detects an AO KB or domain material. A usage skill that recommends starters would therefore add new judgment, and the signal mapping below is **Inference** grounded in each starter's stated purpose (see `Starter catalog.md` in this folder) and in `docs/reference/guides/choosing-your-starter.md`'s rule: "Choose the smallest combination that matches the work you need to make recoverable."

## Read-only inspection first

1. **Is Rhizome already set up?** Look for `.rhizome/config.yml` and `.rhizome/workflows.yml`. If present, `rzm init --check --json` reports the current workflow, every add-on with its enabled state, agents, search provider, pin, and pending writes and decisions. The recommendation becomes a change ("add `compounding`", "Rhizome files are behind"), not a setup.
2. **What is the repository mostly?** Count code by build manifests and source trees versus Markdown notes, and look at the top-level layout.
3. **What structured material already exists?** Look for the specific signals below.
4. **What blocks setup?** User skills under `.claude/skills/` or `.agents/skills/` named like a Rhizome skill fail setup (probed). Lowercase local types such as `assertion`, `concept`, `source`, or `artifact` in `.rhizome/ontology/` collide with `ao-kb`.
5. **Preview.** Run `rzm init --check --json` with the candidate options and show the person the summary lines and files before anything is written.

## Signals and the starters they suggest

| Signal in the repository | Suggests | Strength | Why |
| --- | --- | --- | --- |
| `.kb/config/vault.md` at the root, with `assertions/`, `sources/`, `concepts/`, `artifacts/`, optionally `products/<slug>/` and `.kb/curation/` | `ao-kb` | Decisive | It is the starter's prerequisite; the check reads that file's schemaVersion. Nothing else uses it. |
| Same, but the AO KB sits in a subfolder rather than the root | `ao-kb` only if the person can make the KB root the vault root | Blocking question | The starter requires the vault root to be the AO KB root. |
| Build manifests (`go.mod`, `package.json`, `pyproject.toml`, `Cargo.toml`, `*.csproj`), tests, CI workflows, pull-request activity | `agentic-engineering` | Strong | Code with delivery work is the starter's target and the recommended default. |
| Existing `docs/specs/`, `docs/rfcs/`, `docs/design/`, PRD or user-story documents | `agentic-engineering` | Strong, with a layout question | Fits the spec and effort model. Scaffolds are create-only, so existing files survive, but the starter expects `docs/specs/{product,technical,experience,operations}/`. |
| ADRs (`docs/adr/`, `docs/decisions/`) | `agentic-engineering`, plus a schema extension later | Moderate | No starter has a decision-record type; `docs/reference/guides/adapting-rhizome.md` suggests asking the agent to extend the schema. |
| Regulations, contracts, standards, RFPs, compliance or audit folders, requirements spreadsheets, a domain glossary, workflow or process maps | `domain` (`complex-domain` on top of Agentic Engineering) | Strong when the product's behavior must trace to them | Source-backed requirements and traceability are its whole purpose. `adapting-rhizome.md` says to add it "when that need is concrete". |
| Meeting transcripts or recordings (`docs/meetings/`, `transcripts/`, `*.vtt`, `*.srt`, exported call notes) | Depends on what they are evidence for | Ambiguous | Decisions and context for delivery: `ingest-transcript` in Agentic Engineering. Stakeholder requirement sources: `requirements-ingest` in `complex-domain`. Sources of an AO KB: AO's `/ao-kb:process`, not Rhizome ingestion. |
| `.obsidian/`, mostly Markdown, little or no code, daily or project notes, no delivery process | `--workflow none`, or `action-items` alone when notes hold commitments | Moderate | Agentic Engineering's 30 scaffold docs and delivery views would be noise. `none` still gives search, the `rhizome` skill, and `custom-views`. |
| Checkbox commitments with owners or dates scattered in notes, meeting notes with follow-ups | `action-items` (default with Agentic Engineering; `--workflow action-items` alone otherwise) | Moderate | Its queries find marked checkboxes in any note. |
| A legacy codebase someone is assessing or documenting for a handoff | Agentic Engineering, or `--workflow action-items` for assessment only | Moderate | The assessment chain needs `assumption-tracker`, which ships only with `action-items`; the assessor and harness builder are core skills. |
| Heavy existing agent use (`.claude/`, `.codex/`, `AGENTS.md`, many local sessions) and a team that wants agents to learn the repository | `compounding`, offered, never assumed | Weak alone | Needs a TypeSafe credential, a spend budget, consent to send transcript text to the harness's provider and Jev, and approval of every proposal. Pair it with `action-items` so findings' action items are queryable. |
| Existing `AGENTS.md` or `CLAUDE.md` with team prose | No starter change | Neutral | Init adds managed blocks and preserves the prose. |
| A wrapper repository whose code lives in a Git-ignored nested repository | No starter change; index-scope question | Neutral | Init offers to include each ignored nested repository. |

## Decision order

1. **Configured already:** recommend only changes the evidence supports, and report pending Rhizome file updates first.
2. **AO KB at the root:** recommend `ao-kb`. Add a workflow only if the same repository also has code delivery or domain work; otherwise `--workflow none --addons ao-kb`. First run uses `--addons`; a configured repository uses `--add-addon ao-kb`. Hand off to the `ao-kb-setup` skill for trust, includes, binding refresh, and index.
3. **Code with delivery work:** recommend Agentic Engineering with its default `action-items`.
4. **Upgrade to `domain`** only when concrete source-backed obligations exist, not merely because the domain is complicated.
5. **Knowledge vault without delivery:** recommend `none`, or `action-items` alone when commitments are present.
6. **Offer, never preselect, `compounding`,** and only after naming its credential, cost, and privacy terms.
7. **Prefer the smallest set.** Adding a starter later is one `--add-addon` or Settings change; removing a workflow later deletes its unedited Rhizome files and leaves its docs and schema behind.

## When signals conflict, ask one question at a time with a recommendation

- **Code and an AO KB in one repository.** "Which should Rhizome be organized around? I recommend `ao-kb` with no workflow if the code supports the KB, because the KB root must be the vault root." Mixed repositories are untested in the evidence gathered.
- **An existing specs or design folder with its own structure.** "Should Agentic Engineering adopt your layout, or should new specs go in its folders? I recommend keeping your files and adopting the starter's layout for new specs, then reconciling." Init will not move or overwrite existing files, so both layouts would coexist.
- **Domain signals that may only be background.** "Must the product's behavior be traceable to these sources, with each obligation accepted by a person? If not, Agentic Engineering alone is enough."
- **Transcripts.** "Are these meetings a record of decisions for this team's work, requirements from stakeholders, or raw sources for the knowledge base?" Each answer routes to a different skill.
- **A notes vault with some code.** "Do you plan to ship changes from this repository with specs and efforts, or mostly read and write notes? I recommend `none` until delivery work appears."
- **Personal versus team repository.** Setup writes files the team must commit and pins a version for everyone. Ask whether teammates agree before recommending a workflow that writes 30 team-owned documents.
- **A colliding user skill.** "Your `.claude/skills/<name>` has the same name as a Rhizome skill. Rename yours, or should Rhizome stop managing agent files?" Do not delete it.

## Output the skill could give

**Inference.** A recommendation that the person can accept or redirect: the chosen workflow and add-ons; the evidence for each, citing paths; what it adds day to day; the exact command or Settings path; the plan's summary lines and file count from `rzm init --check --json`; and the steps only the person can take (trust, keys, the current `Person`, budgets, commits). After applying, report files installed, starter setup ready, and index ready as separate states.

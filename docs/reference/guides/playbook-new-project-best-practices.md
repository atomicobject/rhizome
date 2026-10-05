---
type: ReferenceDoc
summary: "Best practices for greenfield projects using Rhizome — starter selection, team identity, first spec-to-implementation cycle, quality gates, and inheriting-team guidance."
reference-kind: guide
status: active
last-verified: 2026-10-01
---

# Playbook: New Project Best Practices

This playbook covers how to start a greenfield project with Rhizome set up correctly from day one — structured delivery, team identity, and quality gates built in before the first feature ships. It also covers what the inheriting team needs to know when they take over a configured repo.

---

## Starter Selection

For most greenfield software projects: **`agentic-engineering`**.

It gives you delivery routing, effort tracking, a lean phase skill suite, and the ontology to answer structural questions about your project's intended versus actual state.

If your project depends on external requirements — regulations, client contracts, complex business domain rules — start with **`complex-domain`** instead. It builds on `agentic-engineering` and adds domain modeling.

Not sure? See [Choosing Your Starter](choosing-your-starter.md).

---

## Step 1 — Initialize the Repo

```bash
cd /path/to/new-project
rzm init
```

Init shows what it found, then asks which workflow to install. Choose **Agentic Engineering** (the default), or **Agentic Engineering with domain modeling** for `complex-domain`. If no semantic search key is available, it asks for a Voyage AI key; press Enter to set it up later. Then confirm `Set up Rhizome? [Y/n/e to edit]`.

Code indexing covers the whole repository, so code you add later is indexed on the next `rzm index` without rerunning init. Init detects your agents from the repository and from installed commands; press `e` at the confirmation, or pass `--agents claude,codex,cursor`, to choose them yourself.

Commit everything `rzm init` wrote:

```bash
git add .rhizome/ AGENTS.md CLAUDE.md .agents/ .claude/ .codex/ .cursor/
git commit -m "chore: initialize Rhizome agentic engineering harness"
```

Pin the Rhizome version for the team:

```bash
rzm update --pinned
git add .rhizome/config.yml
git commit -m "chore: pin Rhizome version"
```

---

## Step 2 — Establish Team Identity

Every regular contributor needs a Person node. These are typed notes that Rhizome uses to resolve accountability in effort tracking, action items, and current-user attribution.

Create one per team member in `docs/reference/people/`:

```markdown
---
type: Person
title: Jane Smith
email: jane.smith@example.com
aliases:
  - Jane Smith
---

# Jane Smith

Lead engineer, platform team.
```

Each team member runs on their machine:

```bash
rzm agent current-user set "Jane Smith"
rzm agent current-user validate
```

This is a local config — it's stored in `~/.config/rhizome/config.yml`, not committed. Each contributor sets their own.

---

## Step 3 — Create the First Spec

Before writing any code, create a spec. Specs are the delivery contract — they define what you're building, who it's for, and the acceptance criteria agents and reviewers verify against.

Start an agent session:

```bash
rzm agent start --profile code --ontology
```

Then invoke `agentic-engineering specify`. The phase will:
1. Ask for the spec title, type (ProductSpec, TechnicalSpec, ExperienceSpec, etc.), and user stories
2. Allocate the next `SPEC-XXXX` id
3. Write the spec note to `docs/specs/`

A healthy first spec has:
- At least one user story with concrete acceptance criteria
- A clear scope boundary (what's in, what's out)
- A `spec-status: proposed` that moves to `active` once reviewed

---

## Step 4 — Create the First Effort

Once the spec is written and reviewed, create an effort to execute it. Use `agentic-engineering effort`.

The skill will:
1. Ask for the effort name and which spec it delivers
2. Write the frozen spec set and stories in scope
3. Choose the local-stamped prospective path, then allocate its `EFF-YYYY-MM-DD-HH-MM[-N]` id through `rzm agent next-id --type EffortNote --path <path>`
4. Write the effort note to `docs/efforts/YYYY-MM-DD-HH-MM-<name>.md`

An effort is a bounded execution unit. Once frozen, the spec set and stories in scope don't change — if scope changes, it goes in `Deviations` or a follow-on effort.

---

## Step 5 — Plan Before You Implement

With a spec and effort created, run `agentic-engineering plan` before touching code. The plan:
- Maps the spec's acceptance criteria to implementation tasks
- Identifies critical architectural decisions (foundation review gate)
- Documents the test approach
- Gets explicit approval before execution begins

```bash
# In your agent session, invoke the 'plan' skill
# It reads the effort note and governing spec, then produces a phased implementation plan
```

Record plan approval in the effort note:

```yaml
plan-approved-by: Jane Smith
plan-approved-at: 2026-06-05T14:00:00Z
```

This approval record is required before `agentic-engineering implement` will execute.

---

## Step 6 — Implement with the Skill

Use `agentic-engineering implement` rather than ad hoc coding. The phase:
- Loads the effort's frozen scope and plan
- Executes phase by phase, checking off tasks
- Runs tests after each behavior change
- Updates docs alongside code
- Validates before handoff

```bash
# In your agent session, invoke the 'implement' skill with the effort path
# Example: implement EFF-0001
```

The skill will not flip the effort to `complete` by itself — use `agentic-engineering finish` to gather the relevant quality, alignment, reconciliation, and follow-up evidence.

---

## Step 7 — Quality Gates

Run quality gates before committing and before closing an effort.

**Pre-commit gate:**
```bash
make check    # lint + typecheck + unit tests; CI and `make check-full` add race and integration
```

**Pre-handoff gate:**
```bash
rzm agent validate all --max-issues 40
rzm agent validate audit --max-issues 40
```

**What clean looks like:**
- `outcomes` reports `completed` for `broken-links`, `ontology`, and `identifiers`
- the separate audit result reports `completed` for `frozen-scope-drift` (no active efforts with a spec changed after effort creation)
- `issueCount` and `errorCount` are zero and the process exits `0`

---

## Greenfield Patterns Worth Establishing Early

**One spec per bounded deliverable.** Don't make omnibus specs. Small specs close faster and drift less.

**Freeze scope before you implement.** The effort's "Spec Set (Frozen)" and "Stories In Scope (Frozen)" are immutable once execution starts. If scope changes, record a deviation — don't edit the frozen section.

**Author code anchors as you ship.** When a note explains a module, add a `code-anchors:` block pointing at the relevant code. This makes `rzm agent file-context` immediately useful instead of requiring a separate documentation pass later.

**Run `rzm agent validate` before every PR.** Integrate it into your PR template or CI. A passing validate before merge means the vault stays coherent as the project grows.

**Track compounding follow-ups.** At closure, record recurring friction that should become tests, scripts, skills, or a new effort. This is where process improvements become durable tooling instead of tribal knowledge.

---

## Inheriting a Configured Repo

You're joining a project that already has Rhizome set up. Here's what to do on your first day.

### Orientation (15 minutes)

```bash
cd /path/to/project
rzm agent start --profile code --ontology
```

Read the output:
- **Note counts by type**: what's in the vault (specs, efforts, requirements, etc.)
- **Active efforts**: what's in flight
- **Recent vault activity**: what changed recently

```bash
export RZM_SESSION=<sessionId>

# Read the local engineering policy
rzm agent file-context \
  --session-id "$RZM_SESSION" \
  --file docs/engineering/

# Find active efforts
rzm agent files \
  --session-id "$RZM_SESSION" \
  --inputs "status:active" --content false
```

### Set Your Identity

```bash
rzm agent current-user set "<Your Name>"
rzm agent current-user validate
```

If your Person note doesn't exist, create it in `docs/reference/people/` following the pattern of existing entries.

### Read Before Contributing

These documents describe how the project uses Rhizome:
- `docs/engineering/workflow.md` — how the project classifies, approves, and closes work
- `docs/engineering/efforts.md` — what an effort preserves for this team
- `docs/engineering/testing-policy.md` — required test approach
- `docs/engineering/quality-gates.md` — executable verification expectations

### First Contribution

1. Find an open story in an active effort (use `effort-execution-context` query recipe)
2. Load the full effort context: `rzm agent query-recipe run --id effort-execution-context --anchor <effort-path>`
3. Use `agentic-engineering implement`; it loads the frozen scope and plan for you
4. Run `make check` before committing
5. Run `rzm agent validate` before opening a PR

Don't create new specs or efforts until you've shipped at least one contribution through the existing process. The loop makes more sense after one full cycle.

---

## Next Steps

- [How Rhizome Works](how-rhizome-works.md) — the why behind the typed graph and retrieval model
- [Choosing Your Starter](choosing-your-starter.md) — if you want to add `complex-domain` on top later
- [Playbook: Agentic Harness Setup](playbook-agentic-harness-setup.md) — if you need the full harness setup walkthrough

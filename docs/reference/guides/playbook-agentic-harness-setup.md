---
type: ReferenceDoc
summary: "Playbook for setting up a Rhizome agentic harness on a new or existing repo — starter selection, rzm init, skill wiring, team identity, first effort, and team onboarding checklist."
reference-kind: guide
status: active
last-verified: 2026-10-01
---

# Playbook: Agentic Harness Setup

This playbook covers setting up Rhizome as a structured delivery harness — not just indexing, but wiring the agent skill suite, establishing team identity, and running the first effort. It covers both the person doing the setup and the team inheriting the configured repo.

---

## Before You Start

Answer these three questions to pick the right path:

1. **What kind of project is this?** Software delivery → `agentic-engineering`. Domain-heavy with source-backed requirements → `complex-domain`.
2. **Is there already a Rhizome config?** If `.rhizome/config.yml` exists, go to [Inheriting a Configured Repo](#inheriting-a-configured-repo). You do not need to run init to join it.
3. **Who will use this?** Establish Person nodes for every regular contributor.

---

## Setup: New Repo

### Step 1 — Install Rhizome

```bash
cd /path/to/repo
curl -fsSLo /tmp/install-rzm.sh \
  https://raw.githubusercontent.com/atomicobject/rhizome/main/scripts/install/install-rzm.sh
bash /tmp/install-rzm.sh \
  --project "$PWD" --user-binary skip --yes --json
```

Inspect `/tmp/install-rzm.sh` before running it when desired; inspection is optional rather than a required pager step. This installs only the project launcher by default. The current installer preserves an existing `rhizome.version` pin; it selects the latest released version only when the project has no pin. The JSON structural status/reason is not a capability check. For a missing or partial surface, `next` is empty and `agentSurfaceRepair.initCandidate` requires owner consent. A non-latest preserved pin may not support the consolidated skill, while matching the advertised latest still does not prove support. Change the pin with the explicit installer candidate only after the owner accepts it and the release is confirmed capable. Verify with `./bin/rzm --version`, then complete the post-init agent-session check before claiming runtime integration.

### Step 2 — Initialize with Your Starter, with Approval

The installer never runs init. Confirm that the project owner wants Rhizome configuration and managed agent surfaces before continuing. Afterward, verify that the selected release actually created the consolidated `rhizome` skill; if not, keep the integration incomplete and choose a known-supporting release explicitly rather than silently advancing the pin.

```bash
./bin/rzm init
```

Init shows what it found (docs, code languages, suggested skips, agents, and semantic search), then asks which workflow to install:

- **Agentic Engineering** (the default) for most software delivery projects.
- **Agentic Engineering with domain modeling** for projects with complex domain requirements (regulations, client contracts, external business rules).
- **Search and agent guidance only** when the team does not want a workflow starter.

If no semantic search key is available, init asks for a Voyage AI key; press Enter to set it up later. It then asks `Set up Rhizome? [Y/n/e to edit]` once and writes the configuration, the managed guidance block in `AGENTS.md` (and `CLAUDE.md` for Claude Code), and the skills in `.agents/skills/` and each detected agent's folder.

Code indexing covers the whole repository, so there are no source folders to enter. Agents are detected from the repository and from commands installed on the machine. To choose without prompts, for example when an agent runs init for the owner:

```bash
./bin/rzm init --workflow agentic-engineering --agents claude,codex
# or, with domain modeling:
./bin/rzm init --workflow domain --agents claude,codex
```

### Step 3 — Change the Repo Version Pin Only Intentionally

The installer already creates or preserves the team pin. Advance it only when the team wants to adopt a newer release:

```bash
./bin/rzm update --latest
```

Use `./bin/rzm update --pinned` to fetch or repair the version already selected by `.rhizome/config.yml`.

### Step 4 — Build the Index

```bash
./bin/rzm index
```

Plain `rzm index` uses configured, enabled embedding providers. Semantic search significantly improves `rzm agent semantic-query` quality.

### Step 5 — Establish Team Identity

Rhizome tracks accountability through Person nodes — typed notes that represent contributors. Create one per regular team member:

```bash
# Your agent will load the `rhizome` structured-markdown guidance or create directly:
./bin/rzm note create "docs/reference/people/jane-smith.md" --content "---
type: Person
title: Jane Smith
email: jane.smith@example.com
aliases:
  - Jane Smith
---

# Jane Smith

Software engineer on the delivery team.
"
```

Set the current user so Rhizome knows who's running the agent:

```bash
./bin/rzm agent current-user set "Jane Smith"
./bin/rzm agent current-user validate   # confirm it resolves correctly
```

Each team member runs `./bin/rzm agent current-user set "<Their Name>"` on their machine.

### Step 6 — Verify Skills Are Registered

```bash
ls .agents/skills/
```

Every enabled agent-skill surface should include the base `rhizome` skill. For `agentic-engineering` you should also see the `agentic-engineering` router plus `foundation-review` and `ingest-transcript`.

Rhizome installs skills once for Cursor, Codex, and other compatible harnesses
under the shared `.agents/skills/` surface. Claude also receives a mirrored
`.claude/skills/` tree. Check harness-specific adapters separately:

```bash
ls .claude/skills/     # Claude Code skill mirror
ls .codex/commands/    # Codex helpers, when command templates exist
ls .cursor/rules/      # Cursor adapter rules
```

**If a harness adapter is missing** (no CLAUDE.md / `.claude/skills/`, for
example), `rzm init` did not detect that agent. Name the agents you use with
`--agents`. `AGENTS.md` and the shared `.agents/skills/` surface follow
automatically:

```bash
./bin/rzm init --agents claude,codex,cursor
```

`claude` creates `CLAUDE.md` and `.claude/skills/`, `codex` creates the `.codex`
command and prompt adapters, and `cursor` creates the `.cursor` rule and command
adapters. On an already initialized repo, init lists this change along with any
other pending updates before it applies them.

### Step 7 — Run the First Agent Session

```bash
./bin/rzm agent start --intent "verify this new Rhizome harness"
```

This returns a `sessionId`, a vault context summary, and ontology type counts. If the vault context shows 0 notes, your `notes.includes` pattern in `.rhizome/config.yml` may not match your docs directory. Check:

```yaml
notes:
  includes: ["docs/**/*.md", "**/*.md"]  # adjust to your layout
```

### Step 8 — Create the First Effort

With an agent session running, use `agentic-engineering effort` to create the first structured effort. The phase will:
1. Prompt for the effort scope and linked spec
2. Allocate the id from the live EffortNote strategy (fresh spec-driven projects use the prospective local-stamped path; preserved projects may remain sequential)
3. Write the effort note to `docs/efforts/`

A healthy first effort tests the full route: `agentic-engineering specify` → `effort` → `plan` → `implement` → `finish`. Even a small, bounded scope validates that the harness is wired correctly end-to-end.

### Team Onboarding Checklist

Before handing the repo to the broader team:

- [ ] `./bin/rzm --version` reports the intended project pin
- [ ] `.rhizome/` committed, including `config.yml` (notes includes, version pin) and `generated-files.yml`
- [ ] `AGENTS.md` and `CLAUDE.md` committed with managed Rhizome guidance block
- [ ] Every enabled skill surface contains `rhizome/SKILL.md`; `.agents/skills/` contains all selected starter skills
- [ ] Shared `.agents/skills/`, the Claude skill mirror when enabled, and harness-specific adapters (`.claude/`, `.codex/`, `.cursor/` as applicable) committed
- [ ] Person nodes created for all regular contributors
- [ ] `rzm agent current-user set "<Name>"` documented in onboarding notes
- [ ] First effort created and visible in `docs/efforts/`
- [ ] `./bin/rzm agent validate all` passes clean (including broken links, ontology, and identifiers)
- [ ] `./bin/rzm agent start --intent "verify Rhizome setup"` returns a session and vault context

---

## Inheriting a Configured Repo

You've been handed a repo that already has Rhizome set up. You do not need to run `rzm init` to join it. Here's how to orient and start contributing.

### Step 1 — Orient

```bash
cd /path/to/repo
./bin/rzm agent start --intent "orient to this configured project"
```

Read the output:
- `vaultContext.summary`: what the vault contains (note counts by type, active efforts, recent activity)
- `vaultContext.ontologyTypes`: which type families exist (tells you which starters are installed)
- `sessionId`: keep this for all follow-up commands in this session

### Step 2 — Understand the Active Work

```bash
export RZM_SESSION=<sessionId>

# List active and planned efforts
./bin/rzm agent files --session-id "$RZM_SESSION" \
  --inputs "status:active" --content false

# Load full context on the current effort
./bin/rzm agent query-recipe run \
  --session-id "$RZM_SESSION" \
  --id effort-execution-context \
  --anchor docs/efforts/<current-effort-file>.md
```

### Step 3 — Set Your Identity

```bash
./bin/rzm agent current-user set "<Your Name>"
./bin/rzm agent current-user validate
```

If your Person note doesn't exist yet, ask a team lead or create one following the pattern in `docs/reference/people/`.

### Step 4 — Understand the Vault Conventions

```bash
# See what types are in use
./bin/rzm agent files --session-id "$RZM_SESSION" \
  --inputs "type:ProductSpec" --content false

# Read local engineering policy
./bin/rzm agent file-context --session-id "$RZM_SESSION" \
  --file docs/engineering/
```

Key files to read before making changes:
- `docs/engineering/workflow.md` — how this repo routes, approves, and closes work
- `docs/engineering/efforts.md` — what efforts preserve for this team
- `docs/engineering/testing-policy.md` — testing expectations

### Step 5 — Make Your First Contribution

For Agentic Engineering repos: pick an open story from an active effort, run `rzm agent query-recipe run --id effort-execution-context` to load the scope, then use `agentic-engineering implement` to execute within the frozen scope.

**If your agent surface is missing** (no CLAUDE.md, no `.claude/skills/`), check what init would change, then name the agents you use:
```bash
./bin/rzm init --check
./bin/rzm init --agents claude,codex
```

`rzm init --check` writes nothing and lists every change init would make. A rerun shows that same list and asks `Apply? [Y/n/s for settings]` before writing.

**Don't:**
- Edit managed blocks in `AGENTS.md` / `CLAUDE.md` directly (they're refreshed by `rzm init`)
- Create efforts without an approved spec (the spec → effort → plan sequence is required)

**Do:**
- Run `./bin/rzm agent validate` before and after any significant changes
- Use `./bin/rzm agent current-user validate` before any accountability-linked operations
- Add your Person node if it doesn't exist

---

## Maintenance: Keeping the Harness Fresh

As Rhizome updates ship:

```bash
# Update the repo pin
./bin/rzm update --pinned

# Show and apply Rhizome's updates to guidance, skills, and settings
./bin/rzm init

# Rebuild the index after significant codebase changes
./bin/rzm index --rebuild
```

Run `./bin/rzm agent validate all` after any init refresh to catch schema changes, broken links, and other comprehensive validation failures introduced by the update.

---

## Next Steps

- [How Rhizome Works](how-rhizome-works.md) — understand the typed graph and retrieval model
- [Choosing Your Starter](choosing-your-starter.md) — if you want to add a starter (e.g., `complex-domain` on top of `agentic-engineering`)
- [Playbook: New Project Best Practices](playbook-new-project-best-practices.md) — greenfield project patterns

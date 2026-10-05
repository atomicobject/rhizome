---
type: ProductSpec
id: SPEC-0077
summary: "Defines the developer onboarding experience for Rhizome: a persona-driven README, a starter decision guide, use-case playbooks, and conceptual documentation that explains why the tool works the way it does."
spec-status: active
last-updated: 2026-07-16
satisfied-by: EFF-0055
aliases:
  - developer-onboarding-and-documentation
  - SPEC-0077
---
# Developer Onboarding and Documentation

## Summary

Rhizome ships with five bundled starters, thirty-one agent skills, and a rich reference guide library — but developers arriving at the tool for the first time cannot determine which starter to use, the README reads as developer implementation notes rather than a user-facing onboarding guide, and demos consistently reveal confusion about which commands apply to which workflows. This spec defines the content and structure of a first-class onboarding surface: a persona-driven README, a starter decision guide, three workflow playbooks, and conceptual documentation explaining why Rhizome's core mechanisms matter.

The audience is any developer who works with a Rhizome-powered repo — whether they configured it (consulting teams setting up a new engagement) or inherited it (client dev teams adopting a delivered system). The same documentation serves both.

## Goals

- A developer new to Rhizome can identify the right starter for their use case and issue their first meaningful `rzm` command within one reading session
- A developer inheriting a Rhizome-powered repo can understand the system's structure and begin contributing within one session
- A developer executing a legacy codebase assessment has a complete step-by-step playbook to follow
- A developer setting up an agentic harness for a new or handoff repo has a playbook covering starter selection, skill wiring, and team onboarding
- A developer can articulate why ontologies matter for agent retrieval and why code anchors outperform ad hoc LLM guessing — i.e., the conceptual layer lands

## Non-Goals

- Replacing the CLI `--help` output or command reference (that is the `surface` contract)
- Documenting internal implementation detail or Go architecture
- Creating a public marketing site or external documentation portal
- Adding new Rhizome features to support the documentation (this spec is content + structure only)
- Automating playbook execution (playbooks are human-readable guides, not scripts)

## User Stories

### US1 - Read the README and know which starter and first command to use

- id:: ^SPEC-0077-US1
- summary:: A developer reads the README cold and leaves knowing which starter fits their situation and what command to run first.
- status:: satisfied

The README is the first document a developer reads. It must function as an onboarding guide, not a feature index or implementation reference. Every reader should exit with a clear next action.

#### Acceptance Criteria

- README leads with persona-to-use-case orientation, not feature inventory.
  - The README opening maps the reader to their situation (setting up a new repo, assessing a legacy codebase, inheriting a configured repo) and routes them to the relevant next step. Feature inventory and architecture notes move to later sections or linked references.
- README is navigable in under 5 minutes to a first command.
  - A developer who has never used Rhizome before can read the README and run a meaningful `rzm` command within five minutes. The path must not require reading reference guides or linked documents to take the first step.
- README references the starter decision guide for choosing a workflow template.
  - The README links to the "Choosing Your Starter" guide and gives a one-sentence summary of each starter inline so readers can make a fast choice without leaving the page.

---

### US2 - Choose the right starter using a decision guide

- id:: ^SPEC-0077-US2
- summary:: A developer can use a decision guide to select the right starter for their use case, understand what it installs, and know how to combine starters.
- status:: satisfied

The five starters (core, action-items, spec-driven, project-kb, complex-domain) install different ontologies, skills, and scaffold docs. Developers cannot effectively choose without a comparative table and decision criteria.

#### Acceptance Criteria

- Starter decision guide exists as a reference doc under docs/reference/guides/.
  - A markdown guide at `docs/reference/guides/choosing-your-starter.md` provides a decision table mapping: use case → starter → what installs → which skills activate → when to combine starters.
- Decision guide covers all five starters with concrete use-case examples.
  - Each starter entry describes at least two concrete scenarios where it is the right choice, one scenario where it is not, and what a developer will see installed after running `rzm init --workflow <name>`.
- Decision guide documents valid starter combinations and dependency order.
  - The guide documents which starters are composable (e.g. `complex-domain` implies `spec-driven`), what each combination produces, and the `rzm init` invocation for each combination.
- Decision guide is linked from README and from the init-starter-workflow spec (SPEC-0038).
  - The guide is discoverable from the primary onboarding surface and from the spec that governs init behavior.

---

### US3 - Execute a legacy codebase assessment end-to-end

- id:: ^SPEC-0077-US3
- summary:: A developer or consulting team assessing a client's legacy codebase has a complete playbook: from first `rzm init` through structured assessment output.
- status:: satisfied

Legacy codebase assessment is a recurring consulting workflow. Rhizome has the commands to support it (`rzm agent report`, `rzm code anchors validate`, `rzm agent vault-health`) but no documented end-to-end flow.

#### Acceptance Criteria

- Playbook A exists at docs/reference/guides/playbook-legacy-codebase-assessment.md.
  - The guide covers: prerequisites, `rzm init` with code indexing, indexing, running assessment reports (`doc_coverage`, `hotspots`, `complexity`), interpreting results, and producing a structured assessment vault.
- Playbook A includes "what good looks like" and "common mistakes" sections.
  - Good: what a healthy assessment result looks like. Common mistakes: what to do when the first index run is sparse, when code anchors return zero matches, when embedding provider is unavailable.
- Playbook A references the specific `rzm agent report --op` flags used in assessment.
  - The commands in the playbook are copy-paste ready with concrete flags, not abstract descriptions.

---

### US4 - Set up an agentic harness for a new or inherited repo

- id:: ^SPEC-0077-US4
- summary:: A developer setting up or inheriting a Rhizome-powered repo has a playbook for starter selection, skill wiring, AGENTS.md/CLAUDE.md setup, and team onboarding.
- status:: satisfied

This is the primary handoff scenario: AO sets up Rhizome for a client engagement, then the client's dev team inherits it. Both the setup agent and the inheriting team need clear guidance.

#### Acceptance Criteria

- Playbook B (Agentic Harness Setup) exists at docs/reference/guides/playbook-agentic-harness-setup.md.
  - Covers: starter selection for ongoing delivery, `rzm init` with agent surfaces, wiring AGENTS.md/CLAUDE.md, confirming skills are registered, running the first effort, and team onboarding checklist.
- Playbook C (New Project Best Practices) exists at docs/reference/guides/playbook-new-project-best-practices.md.
  - Covers: greenfield project starter selection, establishing Person nodes for identity, creating the first effort, running the first spec → plan → implement cycle, and quality gate expectations.
- Both playbooks include "inheriting team" onboarding steps.
  - Each playbook has a section specifically for a developer who inherits a configured repo rather than setting it up from scratch. The section covers: what to read first, how to orient using `rzm agent start`, and how to contribute without breaking vault conventions.

---

### US5 - Understand why Rhizome works the way it does

- id:: ^SPEC-0077-US5
- summary:: A developer can read conceptual documentation that explains why ontologies, code anchors, and starter choice matter — not just how to use them.
- status:: satisfied

Demo feedback consistently shows that developers who understand the "why" become advocates; those who only see the "how" see Rhizome as overhead. Conceptual documentation closes this gap.

#### Acceptance Criteria

- A "How Rhizome Works" conceptual doc exists at docs/reference/guides/how-rhizome-works.md.
  - Explains: the mental model (vault + code index + agent retrieval), why typed ontologies produce better retrieval than flat search, why code anchors create explicit doc-to-code binding, and why starter choice shapes which skills and workflows are available.
- The conceptual doc uses concrete examples, not abstract descriptions.
  - Each mechanism is illustrated with a before/after comparison: what an agent sees without Rhizome context vs. what it sees with a properly configured vault and code anchors.
- The conceptual doc is linked from the README and from relevant playbooks.
  - Readers who want the "why" can navigate to it from the onboarding entry points.

### US6 - Install Rhizome into this project before choosing advanced setup paths

- id:: ^SPEC-0077-US6
- summary:: A new user sees one primary project-local installation path, understands the pinned launcher model, and can verify agent integration before moving to advanced installation or starter choices.
- status:: satisfied

#### Acceptance Criteria

- The README's primary installation path downloads `install-rzm.sh` from the GitHub `main` branch to a local file that can be inspected when desired, runs that file explicitly rather than piping it to a shell, and targets the project with a repo launcher. ^SPEC-0077-US6-AC1
- The primary path explains that the latest installer creates a launcher whose runtime follows the project's preserved `rhizome.version` pin; user-global, development-checkout, explicit-version, and release-channel details are secondary. ^SPEC-0077-US6-AC2
- The README and installation reference ask before using the returned project-launcher init candidate, never silently change a preserved pin, and use the returned launcher for all verification commands. ^SPEC-0077-US6-AC3
- Verification confirms launcher/pin, config resolution, managed guidance, installed `rhizome` skill, and a minimal agent session; missing layers are described as incomplete integration with a repair route. ^SPEC-0077-US6-AC4

## Requirements

- README MUST orient the reader by use case within the opening screen — feature inventory and architecture notes MUST NOT dominate the opening
- README MUST reference the starter decision guide inline
- Starter decision guide MUST cover all five starters with concrete use-case examples and combination guidance
- Playbook A MUST include copy-paste ready `rzm` commands with specific flags for assessment workflows
- Playbooks B and C MUST include sections for developers inheriting a configured repo
- Conceptual documentation MUST explain why ontologies affect retrieval quality, not just that they do
- All guides MUST live under `docs/reference/guides/` following the existing `ReferenceDoc` note pattern
- All guides MUST include `type: ReferenceDoc` frontmatter consistent with existing guides
- README MUST be updated to reflect the rewrite; existing sections may be consolidated but MUST NOT be deleted until content is confirmed moved
- README MUST lead installation with an explicit project-local launcher path using the downloaded GitHub-main installer; global/user and contributor-development paths MUST be secondary.
- README MUST explain that the installer version and repository binary pin are separate: the current installer preserves the project pin unless the user explicitly changes it.

## Open Questions

Standalone "how Rhizome works" guide confirmed (not inline in README). Single-file playbooks confirmed for v1.

## Documentation Plan

- `README.md` — full rewrite as onboarding guide
- `docs/reference/guides/choosing-your-starter.md` — new guide (ReferenceDoc)
- `docs/reference/guides/how-rhizome-works.md` — new conceptual guide (ReferenceDoc)
- `docs/reference/guides/playbook-legacy-codebase-assessment.md` — new playbook (ReferenceDoc)
- `docs/reference/guides/playbook-agentic-harness-setup.md` — new playbook (ReferenceDoc)
- `docs/reference/guides/playbook-new-project-best-practices.md` — new playbook (ReferenceDoc)
- Existing `Spec-driven delivery starter - adoption guide.md` — review for consolidation or cross-linking with the new starter decision guide

# Rhizome-enabled skill authoring

Rhizome integration is opt-in. Use whatever general skill-authoring guidance the active harness provides (for example a `skill-creator` skill) as the primary skill-design discipline.

If the user asks to create or revise a skill and has not already chosen, ask whether Rhizome capabilities would materially help. Give concrete examples: semantic discovery, known-file context and documentation bindings, typed GraphQL/query recipes, ontology-aware Markdown, graph relationships, reports, validation, or safe note mutation. Do not force Rhizome into a self-contained skill that does not benefit from them.

## Design the information people and agents need

Start with the user's questions and the decisions or activities the skill supports. Read a few representative notes and follow their important relationships. Sketch the human reading experience and the agent's context together: what each needs to understand, which sources establish it, and what can stay one follow-up away. A useful brief explains the subject; counts and status fields alone rarely do.

Keep the notes readable as standalone documents. Use body sections for explanation, evidence, uncertainty, and rationale. Add structured fields or relations only when a concrete query, view, validation rule, or other requested consumer needs them. A view and a context script can read the same authored section rather than requiring a second summary in frontmatter.

Preserve the domain's distinctions. In an IP workspace, for example, opportunities describe problems, ideas describe possible approaches, and initiatives record accepted work. Linking them does not turn every problem or idea into a commitment. An absent optional field or relation is context, not a task to create or a reason to invent a workflow.

When the workspace records a person's current thinking separately, a targeted brief can include a linked Perspective with its author, date, and source. Use that convention only where it exists and matters to the question; it is not a required part of every brief.

## Choose Rhizome mechanics

When the user opts in:

1. Classify Rhizome as **required** or an **enhancement**.
2. If required, define the integration preflight and stop behavior when unavailable.
3. If an enhancement, define the degraded path and how the skill discloses lower confidence or missing validation.
4. Reuse a session; discover live agent commands from `agent surface` only when exact syntax is needed.
5. Reuse a saved recipe only when it answers the same workflow question, accepts compatible inputs, drives the first decision, bounds output, and has usable adaptation guidance.
6. If a new repeatable typed packet is justified, validate it against `ontology-query-schema`, bound its output, document adaptation guidance and empty/partial/high-volume semantics, and test representative inputs.
7. Keep repo-local types, fields, relations, report operations, and paths in live ontology/docs/recipes rather than freezing them into a generic skill.

The more-specific skill still owns its deliverable. Rhizome mechanics should compose with that workflow, not duplicate its steps. Task-specific skills need at least one realistic command or query example. Record whether the integration is required or optional so future agents do not imply a Rhizome-backed result when the capability was absent.

For repeated activities that need several Rhizome reads, ship a code-mode script when it saves agents from rebuilding the retrieval logic; see `references/skill-scripts.md`.

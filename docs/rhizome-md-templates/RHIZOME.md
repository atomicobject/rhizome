# Rhizome integration

- This repository uses Rhizome for project-aware retrieval, typed Markdown, graph-safe note mutations, and validation.
- `rzm init` manages this block in `AGENTS.md` and `CLAUDE.md`. Do not edit it in place; put repository-specific guidance outside its fences.

## Route through the Rhizome skill

- If an installed `rhizome` skill is available to the active harness, load it before acting when a change touches a boundary or contract that a subsystem note, spec, or decision record may document; when you need to know why something is built the way it is; when a Markdown note/attachment moves or a linked heading is renamed; when the work changes reusable intent, rationale, or an operating boundary that a durable note should record; or when the request names Rhizome, a session, search, validation, or typed notes. A trivial local edit whose effect is visible in the diff needs none of this.
- Markdown note/attachment moves and linked-heading renames go through the skill's mutation route, never raw filesystem or text edits.
- A more-specific workflow skill owns its deliverable and calls `rhizome` for mechanics. When one answer needs several Rhizome calls or a filtered result, use the skill's code-mode route (`rzm agent code execute`).
- For skill authoring, follow the harness's own skill-creation guidance first. If the user has not said, ask whether Rhizome capabilities such as semantic search, file context, typed queries, ontology-aware Markdown, validation, or safe note mutation would help. Rhizome enablement is opt-in.

## Integration failures

- If the `rhizome` skill, project launcher or pin, configuration, this block, or a minimal agent session is missing or broken, say the Rhizome-integrated experience is incomplete and name the failed layer. Stop explicit Rhizome operations and risky Markdown mutations until it works; for ordinary work, report the evidence gap to the owning skill. Never silently substitute generic shell behavior while implying Rhizome was used.
- If only the managed `rhizome` skill is missing and the launcher works, tell apart intentionally disabled guidance from broken files. With existing authorization, or after asking, run `init --agents <agents>` through the launcher, naming the agents this repository uses (`claude`, `codex`, `cursor`), to re-enable it, or repair the harness-specific skill path. If the launcher is also missing, follow the project's Rhizome installation guidance first.

### Repo configuration

{{REPO_CONFIG}}

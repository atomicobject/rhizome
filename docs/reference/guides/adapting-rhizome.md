---
type: ReferenceDoc
reference-kind: guide
summary: "Adapt team policy and shared skills, then extend Rhizome's project data and views as needed."
last-verified: 2026-10-03
status: active
---

# Adapt Rhizome to your team

[Back to the README](../../../README.md) · [Using the skills](using-skills.md)

The Agentic Engineering starter gives you a place to begin. Your team's working agreements, project constraints, and experience should shape what it becomes.

Start with the process documents. Change skills when the instructions need to change. Add data structure or a custom interface when you have a question the existing tools cannot answer well.

## Start with team policy

After initialization, the project's `docs/engineering/` files are yours to edit. The installed skills consult the relevant document as a concern comes up.

| File | Put your team's decisions here |
| --- | --- |
| `quality-gates.md` | Actual build and test commands, required checks, and completion evidence |
| `testing-policy.md` | What to test, at which layer, and with which tools |
| `review-and-approval.md` | What can proceed directly and what needs a human decision |
| `architecture.md` | Code organization, dependency boundaries, and design conventions |
| `documentation.md` | What knowledge to record and where it belongs |
| `release.md` | Branching, versioning, changelog, and deployment steps |

For example, replace placeholder quality gates with the commands your project actually uses. If a review is only useful for certain kinds of work, say which kinds. If every feature has to meet an accessibility check, put that requirement where agents will consult it.

Keep the rules short and testable. Review policy changes with teammates and commit them so every agent receives the same guidance.

## Improve the shared skills

Skills live in the project, where you can inspect them, change them, and review those changes alongside code. Read the [update ownership rules](#keep-updates-under-control) before customizing managed skills. Use that to fix recurring problems such as specs going into inconsistent folders or agents forgetting a project-specific check.

Start by asking your agent to diagnose the gap:

> Our specs keep ending up in different folders. Read our engineering policy and installed skills, find the conflicting guidance, and propose one consistent convention.

For a task the team performs repeatedly, a project skill can collect its instructions and use Rhizome to retrieve the relevant context. For a small policy change, the engineering document may be enough.

## Keep updates under control

Team-owned documents and managed assets have different update behavior:

- Normal `rzm init` reruns add missing starter documents and leave existing ones alone. `rzm init --refresh-docs` offers Rhizome's latest versions: documents nobody edited update, and init asks about documents your team edited.
- Managed skills and agent guidance update through `rzm init`. Files that still match what Rhizome last wrote update without asking. When someone edited one, a run in a terminal asks whether to take the update, keep your version, or show the diff. If you keep your version, Rhizome asks again only when a newer version of that file ships. Runs without a terminal keep edited files and list them. Keep repository preferences outside the managed blocks in `AGENTS.md` and `CLAUDE.md`.
- Rhizome records what it wrote in `.rhizome/generated-files.yml`. Commit that file so a teammate's rerun makes the same decisions about which files were edited.
- Keep new project-specific skills under team ownership. For changes to bundled skills, edit them directly and decide on each update when init asks, or eject the starter when your team wants to take over its management.

If you want to take over all management of the starter, you can eject it while retaining its files:

```bash
rzm init --eject agentic-engineering
```

If another managed starter depends on it, the command names that dependency. Eject them together by listing every starter, for example `rzm init --eject agentic-engineering,complex-domain`. To resume managed updates:

```bash
rzm init --restore agentic-engineering
```

You can stay with managed defaults and team policy indefinitely. Ejection is useful when the team wants to own the installed starter itself.

## Extend the project information

Rhizome can treat sections and properties in Markdown as structured information. The starter already uses that for specs, stories, efforts, and action items. Notes remain readable files, while agents and views can ask questions about their relationships.

For example, a team may want to see which stories belong to a spec, which efforts are active, or which action items are overdue. As the project grows, you can add the information needed to answer other recurring questions.

> Use the rhizome skill to help us track architecture decisions and the specs they affect. Inspect our existing schema and propose the smallest useful extension.

A schema defines the types, fields, and relationships available in your project. You do not need to design a new schema to start using the Agentic Engineering workflow. [How the underlying structure works](how-rhizome-works.md)

The [complex-domain starter](choosing-your-starter.md) adds source-backed requirements and domain modeling when a project needs those workflows. Add it when that need is concrete.

## Adapt the workspace

Run `rzm start` to open the local UI. Begin with the installed spec, effort, story, and action-item views. Use table, card, or Kanban presentations to make the work easier to scan. A view can make a large set of Markdown documents easier to follow without changing where the information lives.

When a team needs a different view, start with the question:

> Show active efforts with their status, owner, and linked spec. Let us filter to the work we're reviewing this week.

Your agent can inspect the available data and adjust the view definition under `.rhizome/views/`. Validate it with `rzm validate views` and check the result in the browser.

For a dashboard or interaction beyond the built-in views, use the `custom-views` skill:

> Use custom-views to build a dashboard for our open action items, grouped by assignee. Use the project's existing ActionItem data and verify that edits update the source notes.

Custom views can use HTML or React with Rhizome's supplied UI components and data access. They live in the repository, so the team can review and share them. Check both the displayed information and any edits in the real interface.

Display groups open Rhizome's bundled Briefing, Trace, and Sections views, and every type and interface offers a bundled Briefing; all are custom views too. To start from one, run `rzm view eject group.briefing` (or `group.trace`, `group.sections`, `type.briefing`, `interface.briefing`). It copies the views' shared folder into `.rhizome/views/group/`, where your copies replace the bundled ones and your team can change them.

HTML notes and custom views run as trusted project code. Review them as you would other executable files before using them.

[Return to the README](../../../README.md)

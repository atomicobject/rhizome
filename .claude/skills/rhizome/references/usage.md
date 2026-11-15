# Using Rhizome

Use this route to explain Rhizome, to answer from inside the Rhizome app's chat, or to pick the mechanism for something a person wants. It orients and routes; the reference each row names owns the mechanics.

## What Rhizome is

Rhizome indexes a repository's Markdown notes and code on the person's machine and puts one model over them: search by meaning, typed notes whose structure a schema (the ontology) defines, validation, link-safe note moves, and views that show typed notes as tables, cards, boards, and custom pages. People use it through the Rhizome app; agents use the same data through `rzm` and this skill. Everything a team shapes is a file in the repository: notes, and schema, views, saved queries, scripts, and settings under `.rhizome/`.

A **starter** packages note types, views, skills, and documents for one kind of work. A repository installs one **workflow** (a starter set chosen at setup) and any number of **add-ons**. What is installed differs per repository, so read it live (`references/setup-and-starters.md`) and describe what this repository has, by the names its own schema and views use.

## Where you are running

A message that begins with a `[Rhizome view]` block comes from the app's chat pane. Any other session is standalone: a terminal or editor harness in the repository.

```text
[Rhizome view] Note "Title" · docs/path/to/note.md
Route: /notes?note=docs/path/to/note.md
Detail: Section heading
```

In the app's chat:

- The block names what the person is looking at. It is the subject of "this", "here", and "these"; answer about it first.
- When the block is not enough, call the `page_text` tool of the `rhizome_page` server for the visible text of the person's view. For a note, read the file at the block's path.
- You cannot press anything in the app. Name the control or shortcut for the person to use, such as "open Settings, choose this repository's tab, turn the add-on on under Add-ons, then Apply changes".
- Ask a question in plain text and end the turn; the pane shows no choice prompts.
- Write short paragraphs and short lists; the pane is narrow.
- For a long setup or customization job, suggest **Open in Agent**, which continues the same conversation on the full Agent page.
- Run `rzm --version` once before relying on Rhizome commands. If it is missing, answer from the repository's files and `page_text`, and say which Rhizome-backed checks that leaves out.

Standalone, the same routes apply without a view block: ask what the person is trying to do, or inspect the repository.

## What each view kind offers

The block's first line carries the kind. Each kind names its subject; the live call tells you what this repository actually has.

| Kind | The block gives | Discover the rest with |
| --- | --- | --- |
| `Note` | Title and note path | Read the file; `rzm agent ontology-inspect --input <path>` resolves its type and authoring context |
| `View` | View id | `rzm agent view show <view-id>` for its definition; `rzm agent view run --id=<view-id> --omit-capabilities` for its rows |
| `Type` | Type name, as a slug | `rzm agent ontology-reference --type <Type>` for fields, lifecycle, and profile; `rzm agent view list` for the views offered on it |
| `Group` | Display group name | `rzm agent view list` for the group's entry in `targets` and its view choices; `page_text` for the member types shown |
| `Ontology` | The map of all types | `rzm agent ontology-reference` |
| `Notes` | All notes, Problems, or Changes | Problems: `rzm agent validate`; All notes and Changes: `page_text` |
| `Section` | Only the section's name, such as Explorer, Agent, Tasks, or Graphql | `page_text`; for Tasks, `rzm tasks status` |

`rzm agent` calls take the session described in `SKILL.md`. Offer what the discovered types and views support: a type with a lifecycle has a board, a group has overview and briefing pages, a note has typed fields to edit and links to follow.

## Goal to mechanism

Take the first row that satisfies the goal. Rows further down change files other people and agents depend on, so they need a reason the earlier rows cannot meet.

| The person wants | Mechanism | Route |
| --- | --- | --- |
| To find or understand what notes and code say | Search, file context, reading | `references/search-and-code-evidence.md`, `references/onboarding.md` |
| To see records sorted, filtered, or grouped their own way | Personal view settings in the app; nothing is written to the repository | `references/views.md` |
| To write or fix a note of an existing type | Typed Markdown | `references/structured-markdown.md` |
| To clear reported problems | Validation and repair | `references/validation-and-repair.md` |
| Search that finds the right things | Index scope and search settings | `references/index-scope.md`, `references/configuration.md` |
| A table, cards, or board the whole team opens | Shared native view | `references/views.md` |
| A ready-made process for a kind of work | Starter | `references/setup-and-starters.md` |
| To track a new kind of thing, field, or stage | Schema change | `references/customization.md` |
| A page the native layouts cannot express, or several pages as one site | Custom view or experience | The `custom-views` skill |
| One answer from many records or calls | Code mode | `references/code-mode.md` |
| Repeated, one-click, or scheduled work | Saved script, module action, or scheduled task | `references/automation.md` |
| Agents that work the team's way | The team's own policy documents and skills | `references/skill-authoring.md` |

## Name cost, egress, and trusted code before proposing them

Rhizome's index and runtime are local: commands reach the repository's own Rhizome process on 127.0.0.1. Say so when a permission prompt suggests otherwise. Before proposing any of the following, tell the person what it sends, spends, or runs:

- **Semantic search and reranking** send query and indexed text to the configured embedding provider and need its key. `rzm index --status` shows whether search is on and which provider serves it.
- **`evaluate` and `evaluateBatch`** send the supplied content to TypeSafe and bill usage. **`generate`** makes a model call through the person's own Claude Code or Codex login.
- **A scheduled task's prompt** runs an agent on each teammate's machine and spends that person's model budget. Rhizome does not meter it.
- **Custom views, module actions, scheduled tasks, and hooks** are repository code that runs with the person's full authority. Actions and tasks run only after the person trusts the repository (`rzm trust`, in their own terminal).

Each code-mode operation's contract lists its effects: `rzm agent code describe --operation <name>`.

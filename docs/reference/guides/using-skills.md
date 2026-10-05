---
type: ReferenceDoc
reference-kind: guide
summary: "Use Rhizome's Agentic Engineering skills for small tasks, specifications, delivery, and project context."
last-verified: 2026-09-30
status: active
---

# Working with the skills

[Back to the README](../../../README.md) · [Setup](getting-started.md) · [Adapt the workflow](adapting-rhizome.md)

A skill is a set of instructions your coding agent follows. Rhizome installs skills in the repository so teammates share the same workflow and can review changes to it in Git.

The Agentic Engineering starter helps agents use the specs, decisions, and code context already in the project. You describe the outcome; the skill supplies a way to work toward it using your team's policy.

## Start with the task

When the next step is unclear, name the main skill and explain what you want:

> Use agentic-engineering to help me add account invitations. Let's work through the behavior and open decisions before implementation.

For a small task with a clear outcome:

> Use agentic-engineering implement to fix the incorrect validation message. Run the relevant checks and prepare a pull request.

The skill distinguishes local tasks from work that needs a spec, scoped effort, and plan. A clear local fix can finish in one pass. Larger work gets the records and checks needed to resume it and assess whether it is complete.

In Codex, you can invoke `$agentic-engineering`. In Claude Code, use `/agentic-engineering`. You can also ask the agent to use the skill by name, as in the examples here. The installed skill's phase argument selects a particular activity.

## Shape larger work, then delegate it

Work with the agent while important choices are unsettled. Once the outcome and constraints are clear, delegate a bounded implementation. People still own product decisions, approvals, and the merge decision.

| What you need | Ask the agent |
| --- | --- |
| Define behavior | "Use agentic-engineering specify to turn these notes and designs into a spec. Flag decisions we haven't settled." |
| Select a delivery slice | "Use agentic-engineering effort to scope the approved part of this spec." |
| Plan implementation | "Use agentic-engineering plan for this effort. Identify the phases and how we'll verify them." |
| Build approved work | "Use agentic-engineering implement to execute this plan and run the project's checks." |
| Check delivery | "Use agentic-engineering gates and align to verify this effort against its spec." |
| Close and preserve learning | "Use agentic-engineering finish to reconcile the docs and record the delivery evidence." |

These are entry points, not a sequence to repeat for every task. Reuse the current spec and effort when continuing the same work. An approved plan remains authorization for its covered steps and routine fixes; pause when a material decision goes beyond it or when your team's policy requires review.

A **spec** describes the behavior and constraints, with links to the notes, designs, decisions, and open questions needed to implement it. An **effort** captures the selected scope, implementation plan, and verification evidence. They give another person or agent enough context to pick up the work.

## Bring source material into the project

Meeting notes, prototypes, and design files can provide the context for a spec. Use `ingest-transcript` when you want a transcript or interview turned into durable notes with its source retained:

> Use ingest-transcript to capture this meeting in the project. Distinguish agreed decisions from suggestions and open questions.

Then ask `agentic-engineering specify` to work from that material. A source note is evidence; it does not automatically authorize everything someone discussed.

## Supporting skills

| Skill | When it helps |
| --- | --- |
| `rhizome` | Find relevant project context, query structured notes, validate documents, or safely move linked notes |
| `foundation-review` | Review foundational choices when the implementation plan calls for that pause |
| `action-items` | Capture, assign, find, or complete commitments in project Markdown |
| `custom-views` | Build a project view or tool around the questions your team needs to answer |

For example:

> Use the rhizome skill to find the spec and design decisions that govern this part of the code.

> Use action-items to show my open commitments for this project.

For personal assignments, Rhizome needs a note-backed Person identity. Ask your agent to create your Person note, index it, configure it with `rzm agent current-user set "Your name"`, and run `rzm agent current-user validate`. This identity is local to the project; the agent should not guess it from your login name.

## Make the next task easier

When the same problem recurs, use `agentic-engineering compound` to examine it and propose a useful change: a better test, a clearer spec, a shared command, or improved guidance. Use the evidence from real work to decide what deserves a permanent rule.

> Use agentic-engineering compound to review the repeated problems from this effort. Recommend the smallest change that would prevent them next time.

[Next: adapt the process and workspace](adapting-rhizome.md)

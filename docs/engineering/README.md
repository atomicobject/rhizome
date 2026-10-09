# Engineering policy

These documents are the team's editable defaults for how engineering work happens in this repository. They are factored by recurring concern, not by workflow phase, so one document can shape several activities. Edit a sentence here and the installed `agentic-engineering` skill follows it.

They are created on first install and team-owned afterward. A normal `rzm init` rerun does not replace them, and a requested refresh skips any file the team has edited unless you confirm it.

## Precedence

The user's request comes first, then these documents, then the skill defaults. When a document and a skill disagree, the document wins.

## Concerns

| Concern | Document | Consulted when |
| --- | --- | --- |
| Testing | [testing-policy.md](testing-policy.md) | choosing what to test, at which layer, with what |
| Quality gates | [quality-gates.md](quality-gates.md) | running verification, deciding what is safe to run unprompted |
| Documentation | [documentation.md](documentation.md) | deciding which docs change with a change |
| Review and approval | [review-and-approval.md](review-and-approval.md) | classifying risk, finding an approver, deciding whether to pause |
| Architecture | [architecture.md](architecture.md) | placing code, crossing a boundary, reusing or adding a pattern |
| Release | [release.md](release.md) | branching, opening or merging a pull request, versioning, changelog, deploy evidence |

## How agents use these

Read the document for the concern at hand when that concern comes up, not the whole set before every task. Skills name which document applies at each step.

## Ownership boundary

Keep policy, commands, and conventions here. Keep schema-enforced note shapes, identifiers, lifecycle values, ontology definitions, and query mechanics in the managed Rhizome guidance. Keep each document short enough to read in a few seconds; a default sentence and a named exception beat a handbook.

Credential storage, release bundling, and scanning: [secrets.md](secrets.md).

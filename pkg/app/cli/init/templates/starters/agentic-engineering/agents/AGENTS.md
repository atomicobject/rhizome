## Agentic Engineering

This starter is Rhizome's repository harness for Agentic Engineering. It routes engineering work without claiming that every activity is specification work or that the bundled defaults replace this team's process.

## Route

| Situation | Route |
| --- | --- |
| Clear, local, low-risk task | `agentic-engineering implement` |
| Start, resume, or unclear delivery work | `agentic-engineering` |
| Define intended behavior | `agentic-engineering specify` |
| Open a bounded multi-phase effort, then design it | `agentic-engineering effort`, then `agentic-engineering plan` |
| Finish or hand off an effort | `agentic-engineering finish` |
| A plan requested a formative-phase review | `foundation-review` |
| Ingest source material | `ingest-transcript` |

The core `rhizome` skill owns sessions, retrieval, validation, ontology-aware authoring, documentation bindings, and safe mutations.

Phase names are entry points into one task, not mandatory stops. A clear local task ends after its applicable checks and concise report; it needs no effort or closure workflow unless the work crosses the local policy's effort boundary.

## Precedence and boundaries

- The user's request comes first, then the team's policy in [docs/engineering/](docs/engineering/README.md), then skill defaults. Local policy is factored by concern: testing, quality gates, documentation, review and approval, architecture, release.
- An approved plan is authorization: resolve routine implementation choices and complete every step it covers. Stop for a material decision outside that authority, a destructive action, or a genuine scope change. When a skill makes you stop, name the skill file.
- Approval continues across phase transitions, review fixes, handoffs, and fresh sessions while scope agrees. Reuse current context and evidence, refreshing only what later changes invalidate.
- Quality-gate commands come from the local document; a bracketed placeholder is a setup gap, never a command to invent.
- Closed efforts are historical record; new effort-driven work gets a new effort, while a clear local fix remains local.

## Agent Modes

- `Agent Partnership` fits ambiguous, judgment-heavy decisions: short turns, one tension at a time, a recommendation with each question.
- `Agent Delegation` fits bounded production after the brief is clear: produce the artifact directly for human review.

## Installed Skills

{{TEAM_SKILLS}}

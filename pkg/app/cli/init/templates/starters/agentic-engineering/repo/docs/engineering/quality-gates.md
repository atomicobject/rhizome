# Quality gates

Consulted when running verification and when deciding what is safe to run without asking. Used during implementation, quality gates, and closure.

## Commands

Replace each bracketed value with the command this repository actually uses. A bracketed value is a setup gap, never a command to invent, and another repository's command never substitutes.

| Gate | When | Command |
| --- | --- | --- |
| Fast checks | every changed behavior | `[lint / type / unit command]` |
| Focused tests | changed package or bug fix | `[focused test command]` |
| Full verification | before merge or effort closure | `[full build / lint / test command]` |
| Documentation validation | changed Markdown or agent guidance | `[documentation validation command]` |
| Domain or integration | domain contracts, integrations, migrations | `[domain / integration command]` |

## Safe to run without asking

The gates above are local, use disposable fixtures, and touch no production system. Run them, fix failures caused by the requested change, and rerun the affected gate without asking at each step. Anything that deploys, mutates shared state, or costs money is not on this list until the team adds it.

## Evidence

Record the exact command and result. A failed or unavailable gate is evidence, not a pass: record the failure, why it matters, and who or what resolves it. A green subset never proves an unrelated required gate.

## Team extensions

Replace the setup gaps. Add gates for security, migrations, performance, accessibility, or packaging, say which changes require each, and which role can accept a documented exception.

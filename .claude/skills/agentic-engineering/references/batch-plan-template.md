# Delivery batch template

A batch delivers a coherent, verifiable outcome or resolves an uncertainty that blocks delivery. Group related implementation, tests, and documentation together; files, layers, and workflow phases are subtasks rather than automatic handoff boundaries. Use only as many batches as the dependencies and risk justify. A clear local task may need one batch and no effort.

For each batch, include the following in a compact table or short prose. Detail should make the work executable without prescribing every edit.

| Field | Required content |
| --- | --- |
| Outcome | What becomes usable or demonstrably true, and which acceptance obligations it advances |
| Scope and ownership | Affected modules and contracts, exclusions, and responsibility where work is divided |
| Dependencies | Evidence or decisions that must be settled before this batch starts |
| Work | Concrete implementation, test, and documentation steps that belong together |
| Exit evidence | Observable behavior and named checks or measurements that demonstrate the outcome |
| Escalation boundary | Material decisions outside existing authority; distinguish these from routine corrections |

At implementation-time batch exit, record actual changes, verification results, remaining gaps, and the next action in execution notes. Completion records are not required planning content; do not invent results or add speculative delivery records.

Trace every in-scope acceptance obligation to batch evidence or final integrated verification. Reordering work must not lose requirements. Follow user and project guidance on delegation; otherwise inherit the harness's default behavior. A batch does not imply a subagent.

Plan focused tests during implementation, representative evaluation after substantive changes, and broad gates at integration as required by local policy. Reuse evidence until relevant changes or discovered coverage gaps invalidate it. A progress checkpoint records state; it does not itself require approval or a pause. Group routine review findings at a batch boundary, and request earlier review only when a consequential decision would make dependent work expensive to undo or local policy requires it.

For uncertain work, name the question, the smallest useful experiment, comparison, success criteria, and a stopping condition before starting. Set a bounded experiment allowance appropriate to the task. Repeated failure of the same approach triggers diagnosis or a design decision, not an indefinite series of small tuning changes. Build evaluation tooling only when existing checks cannot answer that question.

## Retry helper

The `src/retry.py` module implements the approved maximum-attempt rule. Read the
linked spec and effort before editing. The effort is approved by the synthetic
fixture approver recorded in `docs/people/Synthetic Approver.md`; do not infer
approval from the operator's identity.

- maximum attempts: three
- once an operation succeeds, no retry is allowed
- jitter belongs to a neighboring backlog item and is out of scope

## Two-phase retry work

`docs/efforts/2026-09-07-12-30-two-phase-retry.md` is the approved execution
contract. `src/retry.py` contains the behavior under review and
`docs/specs/two-phase-retry.md` is the product contract.

The handoff is represented by durable files in this worktree. A consumer must
discover phase state from those files rather than relying on a producer
transcript or an implicit resume id.

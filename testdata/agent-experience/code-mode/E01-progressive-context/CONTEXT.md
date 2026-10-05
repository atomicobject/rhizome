## Code-mode fixture context

The repository has two independent code paths:

- `src/expiry.py` parses persisted UTC timestamps and applies the current
  before-expiry boundary rule.
- `src/retention.py` exposes the accepted 30-day retention value.

Use the current `ReferenceDoc` policies for governing behavior. The analysis
notes under `docs/reference/analysis/` are historical support and may explain
why the current policy changed. Query the `Requirement` notes separately from
code context: `REQ-9010` is accepted but untraced, `REQ-9011` is accepted and
fully traced, and `REQ-9012` is candidate evidence only.

The separate `E02-invalid-source-trace/` case is a deliberate negative
fixture. Keep it out of a valid index run and report its invalid source and
broken trace targets when the negative case is selected.

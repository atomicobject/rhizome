---
type: ReferenceDoc
summary: "Current accepted retention policy for the code-mode fixture."
reference-kind: guide
status: active
last-verified: 2026-09-07
code-anchors:
  python:
    - label: retention-path
      glob: src/retention.py
---

# Retention policy

The current accepted contract retains records for **30 days**. The value is
implemented by `src/retention.py` and remains the governing value while the
later candidate proposal is reviewed.

The signed agreement source is linked from `REQ-9011`. The earlier policy note
under `docs/reference/analysis/retention-policy-v1.md` is historical support.

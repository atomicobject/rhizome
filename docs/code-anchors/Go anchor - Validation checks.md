---
summary: "Code anchors for the validation suite (check execution, name resolution, fix plan build/apply) so check authors and CLI surfaces pull the subsystem guidance in file_context."
tags: [type/reference, subsystem/codeanchor, subsystem/validate]
code-anchors:
  go:
    - label: validate-apply-repair-session
      symbol: github.com/atomicobject/rhizome/pkg/validate.ApplyRepairSession
    - label: validate-run-suite-once
      symbol: github.com/atomicobject/rhizome/pkg/validate.RunSuiteOnce
    - label: validate-run-check
      symbol: github.com/atomicobject/rhizome/pkg/validate.RunCheck
    - label: validate-resolve-checks
      symbol: github.com/atomicobject/rhizome/pkg/validate.ResolveChecks
    - label: validate-build-fix-plan
      symbol: github.com/atomicobject/rhizome/pkg/validate.BuildFixPlan
    - label: validate-apply-fix-plan
      symbol: github.com/atomicobject/rhizome/pkg/validate.ApplyFixPlan
---

# Go anchor - Validation checks

Code anchors for `pkg/validate`: concurrent read-only check fan-out via `RunSuiteOnce`, check-name normalization, and the safety-tiered fix suggestion/apply pipeline that `ApplyRepairSession` drives from a reviewed result.

## Read these first

![[validate]]

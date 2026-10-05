---
summary: "Code anchors for the noderead typed read seam (request-scoped Scope batching over indexed ontology rows) so consumers pull the subsystem guidance in file_context."
tags: [type/reference, subsystem/codeanchor, subsystem/noderead]
code-anchors:
  go:
    - label: noderead-new-service
      symbol: github.com/atomicobject/rhizome/pkg/ontology/noderead.NewService
    - label: noderead-scope-options
      symbol: github.com/atomicobject/rhizome/pkg/ontology/noderead.ScopeOptions
    - label: noderead-execute
      symbol: github.com/atomicobject/rhizome/pkg/ontology/noderead.Scope.Execute
    - label: noderead-expand
      symbol: github.com/atomicobject/rhizome/pkg/ontology/noderead.Scope.Expand
    - label: noderead-walk
      symbol: github.com/atomicobject/rhizome/pkg/ontology/noderead.Scope.Walk
---

# Go anchor - Noderead subsystem

Code anchors for the noderead read facade: one `Scope` per request, batched and memoized reads over indexed ontology rows, read-only except the `Resolve` ensure-link seam.

## Read these first

![[noderead]]

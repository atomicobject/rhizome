---
summary: "Code anchors for the typed GraphQL query layer (query prepare/variables, SQLite pushdown planning, saved query recipes) so callers pull the subsystem guidance in file_context."
tags: [type/reference, subsystem/codeanchor, subsystem/graphql-query]
code-anchors:
  go:
    - label: query-prepare-with-variables
      symbol: github.com/atomicobject/rhizome/pkg/ontology/query.PrepareWithVariables
    - label: pushdown-planner
      symbol: github.com/atomicobject/rhizome/pkg/ontology/pushdown.Planner
    - label: queryrecipe-load-default-sources
      symbol: github.com/atomicobject/rhizome/pkg/ontology/queryrecipe.LoadDefaultSources
    - label: queryrecipe-validate
      symbol: github.com/atomicobject/rhizome/pkg/ontology/queryrecipe.Validate
    - label: queryrecipe-compile
      symbol: github.com/atomicobject/rhizome/pkg/ontology/queryrecipe.Compile
    - label: queryrecipe-bind-variables
      symbol: github.com/atomicobject/rhizome/pkg/ontology/queryrecipe.BindVariables
---

# Go anchor - Typed query layer

Code anchors for the read-only typed query layer: SDL-driven execution in `pkg/ontology/query`, indexed filter/sort pushdown in `pkg/ontology/pushdown`, and deterministic saved recipes in `pkg/ontology/queryrecipe`.

## Read these first

![[graphql-query]]

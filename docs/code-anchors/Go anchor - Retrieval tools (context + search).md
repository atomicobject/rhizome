---
summary: "Code anchors for CLI retrieval surfaces (`rzm agent vault-context`, `file-context`, `semantic-query`); embeds the tool guide for agents editing these implementations."
tags: [subsystem/codeanchor]
code-anchors:
  go:
    - label: semantic-query-impl
      ref: github.com/atomicobject/rhizome/pkg/app/mcp.semanticQueryUnified
    - label: file-context-impl
      ref: github.com/atomicobject/rhizome/pkg/app/cli.BuildFileContextText
    - label: vault-context-impl
      ref: github.com/atomicobject/rhizome/pkg/app/cli.BuildVaultContextText
    - label: file-context-build
      ref: github.com/atomicobject/rhizome/pkg/app/cli.BuildFileContext
---

![[Rhizome documentation - Tool guide (agent CLI tools + tradeoffs)]]
![[agent-surface]]

---
summary: "Code anchors for the rzm init managed agent-doc pipeline: RHIZOME guidance templates, managed fences, starter blocks, helper artifact families, and refresh behavior."
tags: [type/reference, subsystem/codeanchor, subsystem/init]
code-anchors:
  go:
    - label: init.rhizome_md_pipeline
      glob: pkg/app/cli/init/rhizome_md.go
    - label: init.rhizome_md_helpers
      glob: pkg/app/cli/init/helper_templates.go
    - label: init.rhizome_md_agents
      glob: pkg/app/cli/init/agents_templates.go
    - label: init.agent_doc_render
      symbol: github.com/atomicobject/rhizome/pkg/app/cli/init.renderAgentHarnessDoc
    - label: init.agent_doc_upsert
      symbol: github.com/atomicobject/rhizome/pkg/app/cli/init.upsertAgentHarnessDocWithUpdater
    - label: init.run
      symbol: github.com/atomicobject/rhizome/pkg/app/cli/init.Run
---

# Go anchor - Init (RHIZOME.md templates)

Use this note as the code-anchor contract for `rzm init`'s managed agent-doc generation and embedded helper template families.

## Contract

- `AGENTS.md` is the canonical managed agent-doc surface; `CLAUDE.md` is an adapter when Claude is enabled.
- Core Rhizome guidance comes from `docs/rhizome-md-templates/*` and renders inside the `RZM INIT RHIZOME BLOCK` fence.
- Starter managed docs render as one `RZM INIT TEMPLATE BLOCK: <template>` per active workflow template.
- User prose outside managed fences must survive reruns; legacy standalone `RHIZOME.md` guidance is migrated away only when it is recognized as generated Rhizome content.
- Command/prompt helper artifacts currently use the command template family; do not add prompt-only source ownership without the decision criteria below.

## Read these first

- ![[cli]]
- [[Init (Hub)]]
- [[RHIZOME.md templates + rzm init]]
- [[init-starter-workflow]]
- [[agent-surface-integration-modes]]
- [[init-template-architecture]]
- [[Keep prompt targets command-backed until prompt semantics diverge]]

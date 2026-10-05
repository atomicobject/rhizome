---
summary: "Code anchors for rzm init skill/template scaffolding: core skills, starter skills, managed agent blocks, collisions, and refresh."
tags: [type/reference, subsystem/codeanchor, subsystem/init]
code-anchors:
  go:
    - label: init.skills_templates
      glob: pkg/app/cli/init/agent_surfaces.go
    - label: init.template_scaffold
      symbol: github.com/atomicobject/rhizome/pkg/app/cli/init.applyTemplateScaffold
    - label: init.load_all_skills
      symbol: github.com/atomicobject/rhizome/pkg/app/cli/init.loadAllSkillTemplates
    - label: init.write_skill_artifacts
      symbol: github.com/atomicobject/rhizome/pkg/app/cli/init.writeSkillArtifactsWithUpdater
    - label: init.load_starter_templates
      symbol: github.com/atomicobject/rhizome/pkg/app/cli/init.loadStarterTemplates
---

# Go anchor - Init (skills templates)

Use this note as the code-anchor contract for init skill/template scaffolding. Keep it compact: this note is automatically retrieved from `agent_surfaces.go`.

## Contract

- Core skills load from `pkg/app/cli/init/templates/skills/markdown/*` and install independently of workflow starter choice.
- Starter skills load only from `pkg/app/cli/init/templates/starters/<template>/agents/skills/*`.
- Core and starter skill names must not collide; `loadAllSkillTemplates` rejects collisions before writes.
- Markdown skill files flow through the diff updater; non-markdown support files copy directly, and `scripts/` stay executable.
- Starter scaffold files are planned separately from skills and managed docs; target-path collisions across selected starters fail unless bytes match.

## Read these first

- ![[cli]]
- [[Init (Hub)]]
- [[Init - Agent surfaces (prompts, commands, skills)]]
- [[init-starter-workflow]]
- [[agent-surface-integration-modes]]
- [[init-template-architecture]]
- [[Agent Skills - Authoring best practices]]
- [[Agent Skills (Hub)]]

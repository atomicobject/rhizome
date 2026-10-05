# Workflow starters

Each directory is one `rzm init` starter. Its assets are grouped by where they install:

| Source under `<id>/` | Installs to |
| --- | --- |
| `template.yaml` | Nothing. Resolver metadata: `requires`, `activatesByDefault`. |
| `repo/**` | The same relative path in the target repository, e.g. `repo/docs/specs/README.md` → `docs/specs/README.md`. |
| `rhizome/ontology/*.graphql` | `.rhizome/ontology/` |
| `rhizome/query-recipes/*` | `.rhizome/query-recipes/` |
| `rhizome/views/*` | `.rhizome/views/` |
| `agents/skills/<name>/**` | `.agents/skills/<name>/` and `.claude/skills/<name>/` |
| `agents/AGENTS.md` | The starter's managed block in `AGENTS.md` and `CLAUDE.md` |
| `agents/skill-overlays/*.yaml` | No file. Patches slots in other starters' skills before they install. |

Core skills that install regardless of starter live in `../skills/markdown/`.

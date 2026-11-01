# Workflow starters

Each directory is one `rzm init` starter. Its assets are grouped by where they install:

| Source under `<id>/` | Installs to |
| --- | --- |
| `template.yaml` | Nothing. Resolver metadata: `requires`, `activatesByDefault`, and `optionalAddon: true` for an add-on no starter activates by default, installed only when chosen with `--addons` or `--add-addon`. |
| `repo/**` | The same relative path in the target repository, e.g. `repo/docs/specs/README.md` → `docs/specs/README.md`. |
| `module/**` | The starter's module, `.rhizome/modules/<id>/`, e.g. `module/views/specs.yaml` → `.rhizome/modules/<id>/views/specs.yaml`. Asset kinds go in `ontology/` (`*.graphql`), `recipes/`, `views/`, `tasks/`, and `scripts/` (written once, executable). Files a script reads by a relative path, such as compounding's `models.json` and `references/`, sit beside them. |
| `agents/skills/<name>/**` | `.agents/skills/<name>/` and `.claude/skills/<name>/` |
| `agents/AGENTS.md` | The starter's managed block in `AGENTS.md` and `CLAUDE.md` |
| `agents/skill-overlays/*.yaml` | No file. Patches slots in other starters' skills before they install. |

Core skills that install regardless of starter live in `../skills/markdown/`.

Init moves a starter file it installed in a flat `.rhizome/` folder before modules into the module when the copy is unedited, and keeps an edited copy instead of installing its module copy.

Script tests and fixtures for a starter live under `../../testdata/<id>/`, outside the embedded tree, so they never install downstream.

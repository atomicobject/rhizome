# Setup and starters

Use this route to recommend, install, change, update, or repair the workflow and add-on starters in a repository. `rzm init` is the one engine: the app's setup sheet, its Settings, and an agent all plan with `rzm init --check --json` and apply with `rzm init --json`.

## Let the request set the authority

- **"Which starter fits?", "What would this add?"** Stay read-only through step 3 and end with a recommendation.
- **"Set it up", "Add `<id>`".** The request authorizes previewing and applying the plan it describes. Show the preview, apply it, and report.
- **Switching workflows.** Init deletes the old workflow's unedited skills, guidance blocks, saved queries, and views; it keeps the team's documents and schema. Show the preview and apply only after an explicit yes to that removal.
- **The person's own steps.** Trusting the repository, supplying provider keys, naming which `Person` they are, setting a spend budget, and committing stay with the person. List them; do not perform or guess them.

## 1. Inspect

Every command here writes nothing. Always pass `--check` when inspecting: a bare `rzm init` without a terminal applies its changes.

1. `rzm --version` names the executable that runs and its version. An old global `rzm` beside a newer project pin explains missing commands; use the project's invocation from `SKILL.md`.
2. `rzm init --check --json` reports `configured`, the current `workflow`, every choice in `workflows` and `addons` (each add-on's `enabled`), `agents`, `search`, `pin`, and pending `writes`. In an unconfigured folder it reports what setup would write for the given choices. Until it reports `configured: true`, run no `rzm agent` command: in a folder without `.rhizome/config.yml`, `rzm` reads the registered default vault, which is another repository.

Done when you can state the executable and version, the pin, whether the repository is configured, its workflow and enabled add-ons, and whether writes are pending.

## 2. Recommend

Read the catalog from that JSON, never from memory: starters change between versions, and a repository can carry its own. Each `workflows` and `addons` entry has an `id`, `label`, and `description`. When an entry has `fit` (the repository signals that suggest it) and `cautions` (credentials, spend, data egress, prerequisites), weigh those; otherwise work from the description. `types` lists every schema type name the entry installs, including those of the starters it requires. `defaultFor` lists the workflows that turn an add-on on by default, and `optional: true` marks an add-on no workflow turns on.

Match the catalog against evidence, citing paths:

- What the repository mostly is: build manifests, tests, and CI, or mostly Markdown notes.
- Structured material that already exists: requirement, decision, or meeting folders, checkbox commitments, knowledge-base layouts an entry's `fit` names.
- The process the team already follows, in its own guidance files, policy documents, and skills. A team with a working process of its own gets the choice that adds search and agent guidance without a second process.

Recommend the smallest set the evidence supports. Adding a starter later is one flag; removing a workflow later deletes files. State each recommended entry's `cautions` with the recommendation. Recommend an `optional` add-on only when the evidence asks for it, and leave it off unless the person chooses it.

Check two blockers before recommending:

- **Colliding type names.** Compare, ignoring case, the repository's own type names with the `types` of the recommended workflow and of each add-on that will be on. A type declared twice is a schema error (`Cannot redeclare type`). Renaming or retiring a local type is the person's decision.
  - **Configured** (`configured: true`): list the vault's types with `rzm agent ontology-reference`. Set aside the `types` of the step 1 `workflow` and of the add-ons it reported `enabled`; those starters are already installed and their types are not collisions.
  - **Unconfigured**: there is no Rhizome schema yet, so skip the check. The exception is `.graphql` files already under `.rhizome/ontology/` or `.rhizome/modules/*/ontology/`, which setup compiles with the starters' schema: read their top-level `type`, `interface`, `enum`, `union`, `input`, and `scalar` names from the files.
- **Same-named skills.** A person's own skill folder with a managed skill's name makes the plan fail with a skill-collision error that names the folder. The person renames it or stops Rhizome managing agent files; never delete it.

When signals conflict, ask one question with your recommendation attached. A recommendation states the workflow and add-ons, the evidence for each, what each adds day to day, its cautions, the command or Settings control that installs it, and the steps only the person can take.

## 3. Preview

Run the check with the candidate choices and show `writes.summary`, the count of `writes.files`, and any `writes.decisions` (files someone edited, with diffs).

| Repository | Flags | Behavior |
| --- | --- | --- |
| Not yet configured | `--workflow <id>` with `--addons <id>,<id>` or `--addons none` | `--addons` is the exact set: a default add-on left out is recorded as disabled. To add one, list the defaults too (`defaultFor` names them). Without `--addons`, defaults apply |
| Configured | `--add-addon <id>`, `--remove-addon <id>`, `--workflow <id>` | Each add-on flag changes only the named add-on. `--addons` is refused here |

```bash
rzm init --check --json --add-addon <id>
```

On a configured repository the plan carries `writes.decisionSnapshot`. Keep it for the apply.

## 4. Apply

Within the authority above, rerun the same choices without `--check`:

```bash
rzm init --json --add-addon <id> --decision-snapshot <token>
```

`--decision-snapshot` binds the apply to the file versions the preview showed; if it is rejected, files changed since the review, so preview again. Edited files are kept and stay pending unless the person chose `--take <path>` or `--keep <path>` for them. A first-time setup also downloads the pinned Rhizome binary. Pass `--accept-suggestions` only for suggestions the person agreed to.

In the app, the person can make the same change themselves: Settings, this repository's tab, Workflow or Add-ons, then **Apply changes**. Offer that path when they would rather review there.

## 5. Report three states

Init writes files; it does not index or run a starter's own setup. Report each state separately, with its evidence:

1. **Files installed.** A repeated `rzm init --check --json` shows no pending writes for the choice. List the written files for the person to commit.
2. **Starter setup ready.** Find the skill the starter installed among the written files and load it; it owns the starter's own setup, readiness checks, and daily use. A starter with no setup steps is ready once its files are in.
3. **Index ready.** `rzm index --status`, then one typed query or search that returns the starter's records (`references/indexing-and-freshness.md`).

Close with the person's remaining steps: commit the files; run `rzm trust` in their own terminal if the starter has actions or scheduled tasks (the app asks for trust when it opens the repository); supply keys through the app or an interactive `rzm init`, never through you; and name their `Person` so you can run `rzm agent current-user set "<Person title>"`.

## Update and repair

Generated skills and guidance fall behind when the Rhizome version advances. `rzm init --check` exits 1, and the JSON plan lists `writes`, when they are behind the executable that runs.

- Compare `rzm --version` with the plan's `pin` first; a mismatch means the wrong executable is running, which init cannot repair.
- `rzm update` changes the version a repository runs and then reruns that version's init. With `--yes` it keeps edited files.
- Apply a refresh through steps 3 and 4. Edited files appear in `writes.decisions`; each is the person's choice.
- In the app the same repairs are toolbar offers: **Update to** a version, and **Update Rhizome files**.
- Fix a wrong generated skill or guidance block through init, or through the team's own files outside the managed blocks. A hand-edited generated copy becomes an edited file every later update asks about.

A missing launcher, a failed session, or a broken capability goes to `references/installation-and-integration.md`.

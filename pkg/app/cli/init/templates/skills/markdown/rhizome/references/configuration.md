# Configuration

Rhizome reads repository settings from `.rhizome/config.yml`. `rzm init` also maintains `.rhizome/workflows.yml` (installed workflows) and `.rhizome/generated-files.yml` (which generated files Rhizome wrote and any version someone declined). Leave those two files to init, and do not move their fields into `config.yml`.

## Diagnose first

- Use the project launcher so Rhizome resolves the intended project and pin.
- `rzm init --check` prints the setup summary and every change init would make, writes nothing, and exits 1 when something would change. Suggestions, such as turning on semantic search once a key exists, are listed separately and do not count as changes.
- `rzm index --status` shows what indexing uses now: whether semantic search is on, the code folders, the runtime, and the index lock. Read the `Enabled` line for semantic search; a provider line alone does not mean search is on.
- For a symptom such as code that is not indexed, a missing file, search that is off, or missing skills, use the troubleshooting table in `references/installation-and-integration.md`.
- Read the current file before proposing a field. Unknown keys produce warnings; malformed YAML and wrong value types are errors. Loading never rewrites the file.

## Run init with consent

`rzm init` changes settings and generated files, so run it only with the user's consent, after showing them the change list from `rzm init --check`. Without a terminal, `rzm init` applies every listed change, keeps files someone edited and lists them, and leaves suggestions alone. Flags choose settings without prompting: `--search`, `--agents`, `--workflow`, and `--include-ignored`. `rzm init --help` lists the flags the installed version accepts.

## Important settings

These settings decide whether Rhizome works well. Change each one through its route.

| Setting | Why it matters | How to change it |
| --- | --- | --- |
| What gets indexed | Missing content cannot be found, and bulky content drowns real results | `references/index-scope.md`; `rzm init --check` for proposed skips; rules in `.rhizome/ignore` |
| Semantic search provider and key | Without it, search by meaning and code similarity are off | `rzm init --search <provider>` with `voyage`, `openai`, `ollama`, or `off`. Search turns on only when the provider's key resolves, from `VOYAGE_API_KEY` or `OPENAI_API_KEY` in the environment or from a key the user pasted into an interactive `rzm init`. Ollama needs no key. Ask the user for the key; never read or print it. |
| Agents | Missing guidance or skills mean agents do not use Rhizome | `rzm init --agents <agents>`, naming every agent the repository uses (`claude`, `codex`, `cursor`), or `none` to stop managing agent files |
| Current user | Action items, approvals, and "my" queries need an identity | `rzm agent current-user set "<Person title>"`, then `rzm agent current-user validate`. Never infer the person. |
| Binary ownership | Decides who updates Rhizome and how teammates get the same version | [Binary ownership](#binary-ownership) below and `references/installation-and-integration.md` |
| Validation suites | Decides what the team's gate checks | `validation:` in `.rhizome/config.yml`, with check names from `rzm validate list`; confirm with `rzm validate` |

`rzm index` stops with an error when semantic search is on and its key is missing, so init keeps search off until a key resolves. Turning search off on purpose is recorded, and later runs do not offer to turn it back on.

## Binary ownership

By default Rhizome owns its binary: `rhizome.version` pins the release, the project launcher runs it, and `rzm update` changes it.

To let an external tool such as mise own the version and executable, run `rzm init --binary-manager external` with consent. Its change list shows the switch. It writes the field below, clears `version`, `devBinaryDir`, `binaryDir`, and `binaryPath`, and leaves any `.rhizome/bin` contents in place.

```yaml
rhizome:
  binaryManager: external
```

`external` does not name the tool; Rhizome runs whichever executable the tool selected. Any other `binaryManager` value, or `external` combined with `version` or a binary path field, is a configuration error. Returning to Rhizome management is an explicit config edit; init has no flag for it.

In an externally managed repository, do not run `rzm update` or the project installer. Change the version through the owning tool. For mise, edit `mise.toml`, run `mise lock`, commit the refreshed `mise.lock`, install with `mise install --locked`, and verify with `mise exec -- rzm --version`.

## Expert fields

Everything else in `.rhizome/config.yml` is an expert field: note include and exclude globs, code folder limits (`code.<language>.roots`), coderef scan globs, link style, file-context packing, graph tuning, compression, embedding models and endpoints, and the index path. Rhizome's [Advanced configuration tuning guide](https://github.com/atomicobject/rhizome/blob/main/docs/reference/guides/Advanced%20configuration%20tuning.md) documents them.

Leave expert fields alone unless the user asks or a diagnosis points to one, for example `rzm index --explain` naming code folder limits. Never infer a key's shape from memory; read the installed version's docs and the existing config.

## After a change

Run the narrowest check that covers it. Indexing settings take effect on the next `rzm index`; an ignore-file edit forces a full resync on that run. Validation-suite changes need a focused `rzm validate` run and no reindexing. Preserve unrelated configuration.

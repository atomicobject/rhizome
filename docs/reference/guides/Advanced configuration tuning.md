---
type: ReferenceDoc
summary: "Reference for the expert tuning fields in .rhizome/config.yml that the rzm init menu intentionally does not prompt for."
reference-kind: guide
last-verified: 2026-10-01
status: active
---

# Advanced configuration tuning

## Summary

The `rzm init` settings menu covers four product-level decisions: what gets indexed, semantic search, agents, and workflow ([[init-starter-workflow|SPEC-0038]]). The fields below are deliberate expert knobs: edit them directly in `.rhizome/config.yml` (the file is the full configuration surface). Indexing knobs take effect on the next `rzm index`; validation-suite composition takes effect on the next validation command and does not trigger indexing.

Rhizome reads only `.rhizome/config.yml` for repo-local runtime settings. Unknown keys produce warnings and remain untouched; malformed YAML and invalid known-field types remain errors. Loading and discovery never rewrite the file. Workflow starter adoption and management state belong in `.rhizome/workflows.yml`; `rzm init` losslessly moves the retired v0.49 workflow fields there and removes only those migrated keys from `config.yml`.

`notes.includes` and `notes.excludes` select ordinary Markdown notes. `rzm init` writes no notes folders, so all Markdown is a note unless `.rhizome/ignore` keeps it out. Listing folders in `notes.includes` limits notes to them; when Markdown sits outside those folders, an `rzm init` rerun suggests removing the limit and applies it only after a person confirms. Exact `CONTEXT.md` is a Rhizome system convention and remains indexed outside those patterns. To create an explicit hard boundary for sensitive or irrelevant context files, add the path to `.rhizome/ignore`; repository containment, infrastructure skips, and Git/Rhizome ignore rules still apply.

## Validation suite composition

Compose the configured `default` and `all` suites independently. `audit` is fixed maintenance scope and is intentionally not configurable.

```yaml
validation:
  default:
    add: [link-hygiene]
    skip: [broken-links]
  all:
    add: [frozen-scope-drift]
    skip: [code-anchors]
```

Check names are public kebab-case names from `rzm validate list`. Unknown names, overlap between one suite's `add` and `skip`, skipping a check absent from that built-in suite plus its `add` list, or composing an empty suite are configuration errors. A directly selected check such as `rzm validate broken-links` bypasses configured suite composition so it remains usable while diagnosing invalid config.

## Broken links and placeholders

`broken-links` resolves links the way Obsidian does: names compare case-insensitively, only `.md` is implied, attachments and files under `.rhizome/ignore` paths are real targets, several files with one name still resolve, and links inside code are not links. Ignore rules exclude files from indexing and search, never from link resolution.

An unresolved link is either a link that broke or a placeholder. By default, git history decides: a target whose file name was deleted or renamed away in the vault's history, or is still tracked but missing from the working tree, broke, and fails the check; any other unresolved target is a placeholder that never existed, which `rzm validate placeholder-links` lists (an `audit` check, never part of `default` or `all`). Without usable history (not a git repository, or a shallow clone) every unresolved link counts as broken. To make every unresolved link fail the gate, as documentation repositories usually want:

```yaml
validation:
  brokenLinks:
    placeholders: strict   # default: history
```

## File-context packing

Controls what `rzm agent file-context` packs around a seed file:

```yaml
fileContext:
  docPatterns: [CONTEXT.md, README.md] # priority-ordered doc filenames pulled from ancestor dirs
  contextBudget: 12000                 # max characters of ancestor docs (0 = default)
  maxEmptyLevels: 2                    # stop climbing after this many doc-less ancestors (0 = default)
  includeDocsInGraph: true             # treat docPatterns files as graph notes
```

## Graph tuning

Controls graph analysis scoring and scope:

```yaml
graph:
  ignore: ["archive/**"]            # notes excluded from graph analysis
  keyNotePatterns: ["docs/hubs/**"] # patterns treated as key/hub notes
  authorityFactors:                 # authority score multipliers
    - pattern: "docs/specs/**"
      factor: 1.5
```

## Code folders

Code folders are optional. When code indexing is on and no language block lists `roots`, code indexing covers the whole repository minus ignored paths, and each file's language comes from its extension. Code added after setup is indexed on the next `rzm index` without rerunning init. This is what `rzm init` writes:

```yaml
code:
  enabled: true
```

Listing `roots` in a language block limits code indexing to the named folders:

```yaml
code:
  enabled: true
  go:
    roots: [cmd, pkg]
  typescript:
    roots: [web/src]
```

Once any block lists `roots`, languages without a block are left out, and a block without `roots` still covers the whole repository. When code exists outside the named folders, an `rzm init` rerun lists "Index code in ... (remove the code folder limits)" as a suggestion, because a limit may be deliberate; it applies only after a person confirms in a terminal. To keep a folder out of the index for good, add it to `.rhizome/ignore` instead. `rzm index --explain <path>` reports whether a code file is inside the configured folders.

## Coderef scan globs

Per-language or global override globs for comment/doc-reference scanning:

```yaml
code:
  scan: ["src/**"]      # coderefs-only include globs
  ignore: ["**/gen/**"] # coderefs-only exclusion globs
  go:
    scan: ["cmd/**", "pkg/**"]
    ignore: ["**/zz_generated*"]
```

## Embedding endpoints and models

The init menu chooses one provider for notes and code. Models, endpoints, and a separate code provider are config-only. Choosing a provider through `rzm init` (in settings or with `--search`) rewrites these blocks with only the provider, so reapply overrides afterward:

```yaml
noteEmbeddings:
  provider: voyage
  model: voyage-4-lite
  endpoint: https://api.voyageai.com/v1/embeddings
codeEmbeddings:
  provider: ollama
  model: nomic-embed-text:latest
  endpoint: http://localhost:11434
```

## SQLite placement in VMs and containers

SQLite WAL databases require coherent file locks and shared memory. Keep every Rhizome process that opens an index in the same host or VM, and place the index on that environment's local filesystem. Rhizome rejects known shared and network filesystem types, including `virtiofs`, FUSE, NFS, CIFS/SMB, and 9p.

For a checkout mounted into a Lima guest over `virtiofs`, set the top-level `indexPath` to an absolute path on the guest's local disk:

```yaml
indexPath: /var/lib/rhizome/my-project/db.sqlite
```

Create the parent directory in the guest and ensure the guest user can write it. The absolute path is VM-local: do not reuse the host's absolute path, and do not point it back into the mounted checkout. A VM-local checkout is the preferred topology when practical because it avoids shared-mount latency for both source traversal and database traffic.

`RHIZOME_SQLITE_ALLOW_UNSAFE_FS=1` is an expert escape hatch that only bypasses Rhizome's filesystem refusal. It does not add locking or shared-memory guarantees, change SQLite durability settings, improve performance, or make a rejected filesystem supported. A database-on-`virtiofs` benchmark cell is useful for diagnosis but is not a supported deployment topology.

## Agent-start benchmark matrix

Maintainers can measure the pinned startup command with `scripts/perf/agent_start_benchmark.sh`. Each configured cell names a prepared checkout and the absolute `indexPath` already configured for that checkout:

```bash
./scripts/perf/agent_start_benchmark.sh \
  --rzm /absolute/path/to/rzm \
  --cell source-local_database-local=/absolute/local/checkout,/absolute/local/db.sqlite \
  --cell source-virtiofs_database-local=/mnt/host/checkout,/absolute/guest-local/db.sqlite \
  --environment-file /tmp/pinned-lima-environment.json \
  --output /tmp/agent-start-matrix.json
```

The other accepted cells are `source-local_database-virtiofs` and `source-virtiofs_database-virtiofs`. Omit cells that are not available in the current environment; the report marks them `unavailable` rather than manufacturing measurements. Use `--environment-file` to attach a JSON record of the pinned host and guest OS, Lima version/configuration, CPU and memory allocation, mount type/options, and storage placement. The harness also probes the source and database filesystem types for each cell.

Before sampling, the harness asks the selected executable's `agent surface` to resolve the effective vault and unified database paths. It fails if either identity differs from the cell declaration and records the verified identity in the result. The harness then runs the exact representative command with additive `--timings`, discards one warm-up, collects seven measured samples by default, reports median and p95 latency plus diagnostics and an environment manifest, and compares output after removing volatile session/timing fields.

## Related

- [[Ignore behavior]] — what gets indexed at all
- [[Ignore + exclude rules]] — matcher internals
- [[getting-started|Getting started]]: the `rzm init` first run and reruns

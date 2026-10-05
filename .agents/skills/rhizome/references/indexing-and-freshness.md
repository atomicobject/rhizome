# Indexing and freshness

Use the project launcher and inspect status before assuming search is current:

```bash
rzm index --status
```

For slow or failed indexing, read `rzm diagnostics index --last --json` before rerunning it. `references/diagnostics.md` explains the retained timings, fallback reasons, history, and evidence gaps. This offline read works even when the index or runtime is unavailable.

Normal `rzm index` refreshes configured notes, code, and enabled embeddings based on content freshness. Reserve `--rebuild` for index-format/config changes, corrupt state, or persistently stale matches after a normal sync; it is more expensive than an incremental update.

## The vault runtime

Each vault root has at most one runtime process (`rzm serve`, started headless on demand). It watches files, keeps the index fresh, and runs every indexing job on one lane, so `rzm index` never contends with it: with a runtime present, `rzm index` submits the job to the runtime and streams its progress. Commands such as `rzm agent start` and code mode start the runtime when none exists; you do not need to start it yourself.

- `rzm index --status` shows the runtime (mode, PID, whether a job is running) and the index lock holder, or says neither exists.
- If `rzm index` reports it is waiting on a lock holder, the message names the holder's role and PID; wait or stop the holder. Do not delete lock files.
- `rzm stop` shuts the vault's runtime down; `rzm stop --all` stops every runtime on the machine. A headless runtime also exits on its own after an hour without activity.
- `rzm index --rebuild` stops a headless runtime, rebuilds, and restarts it. Against an attached runtime (a terminal running `rzm serve` or `rzm start`) it refuses; stop that first.
- `rzm index --in-process` and `RZM_RUNTIME_AUTOSTART=0` bypass the runtime for one invocation; `runtime.autostart: false` in `.rhizome/config.yml` disables auto-start for the repository.

When a file is missing, diagnose scope before rebuilding:

```bash
rzm index --explain <path>
```

It names the deciding ignore layer, file, line, and pattern. For a code file it also says whether the file is indexed as code, and otherwise which setting keeps it out: code indexing is off, or the file sits outside the code folders named in `.rhizome/config.yml`. `references/index-scope.md` explains the layers and how to change them. An ignore-file edit forces a full resync on the next `rzm index`.

Code indexing covers the whole repository minus ignored paths unless config names code folders, and each file's language comes from its extension. Code added after setup is indexed on the next `rzm index` without rerunning init. `rzm index --status` prints the code folders. When folder limits or a missing `code` section keep code out, `rzm init --check` lists the fix; run `rzm init` only with the user's consent.

Distinguish these cases:

- **unindexed**: the relevant lane has never been built
- **stale**: source/config changed after the index snapshot
- **excluded**: an ignore rule or a code folder limit keeps the path out
- **capability unavailable**: for example, semantic search is off or its key is missing (`rzm index` stops when search is on and the key is missing)
- **retrieval warning**: the lane exists but a query timed out or degraded

After indexing, rerun the same focused query or exact lookup that exposed the problem. Do not claim freshness only because the index command exited successfully.

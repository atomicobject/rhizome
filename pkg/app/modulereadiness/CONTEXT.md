# Installed module readiness

`Coordinator` owns one repository's installed lifecycle descriptors and public readiness snapshots. `serve` starts and drains its one worker independently of startup readiness and the indexing lane.

- `pkg/vault/modulelifecycle` reads optional `.rhizome/modules/<id>/lifecycle.yml` declarations. Each names module-local observe, setup, index, and verify actions and bounded input paths. Invalid declarations remain visible as blocked setup.
- Only the worker executes actions. GET reads copy the snapshot. Recovery requests validate a currently offered retry or choice, coalesce by module, and queue work on the runtime lifetime rather than the HTTP request lifetime.
- Relevant input fingerprints and repository trust govern automatic observation. A changed input triggers a new assessment; unchanged ready and failed inputs do not repeat setup. Ready state is reconstructed and queried on reopen. Other terminal states have a local receipt under `.rhizome/state/module-readiness.json`, already ignored by init's default deny rule.
- Assessments choose `ready`, `prepare`, `index`, `needs-input`, or `blocked`. Modules own their domain interpretation, selection persistence, and query/index evidence. The coordinator bounds convergence, checks authored and executable schemas through its composition callback, and rejects an assessment if its inputs changed while observing.
- Setup and index actions run through the shared action runner outside the writer lane. The normal `rzm index` command retains runtime submission, leases, and writes. The coordinator never acquires an index lease. Cancellation drains subprocesses before runtime shutdown releases its stores.
- Source admission uses an init-owned exact-path include helper, preserving existing rules and unrelated configuration. No readiness action deletes authored schema or knowledge.

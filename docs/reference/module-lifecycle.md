---
summary: Installed module lifecycle descriptors, assessment results, and runtime recovery endpoints.
reference-kind: guide
last-verified: 2026-11-10
code-paths:
  - pkg/app/modulereadiness
  - pkg/vault/modulelifecycle
  - pkg/app/web/starter_readiness.go
---

# Module lifecycle readiness

An installed module can declare `.rhizome/modules/<id>/lifecycle.yml` with a title, four module-local action names, and one to 32 repository-relative input paths. The fields are `title`, `observe`, `setup`, `index`, `verify`, and `inputs`. Input paths are literal files or directories. Optional `excludes` holds up to 32 relative glob patterns for irrelevant subtrees. AO KB excludes raw evidence archives from readiness fingerprints. Observation includes missing files, rejects symlinks, and is bounded to 20000 files and 64 MiB. Narrow an oversized descriptor before retrying.

Input fingerprints include directory names even when directories are empty. Adding, removing, or renaming an artifact phase therefore triggers another assessment without requiring a note-file change.

Descriptors are regular files of at most 64 KiB. Descriptor, fingerprint, and receipt reads use repository-confined handles, reject symlinks and special files, and bound bytes before parsing. These reads run before trust is granted. Receipt publication uses an exclusive temporary file in a repository-confined state directory, so an authored symlink cannot redirect a startup write outside the repository.

The elected runtime discovers declarations on opening and every two seconds, including modules installed through a separate CLI process. It invokes existing module actions, with their normal repository trust, process limits, and cancellation. Starter readiness is independent of the initial-index startup gate. Commands receive the runtime's absolute selected executable in `RZM`, with repository delegation suppressed.

Observe and verify actions return one JSON assessment. `next` is one of `ready`, `prepare`, `index`, `needs-input`, or `blocked`. `reason` explains the actual problem or work. `input` supplies setup action input for unambiguous automatic preparation. `includes` lists exact declared source files that the init-owned helper must admit before indexing, including files not yet created. `recovery` lists typed actions. A choice declares its setup `input` name and allowed `choices`; retry carries no input. Review actions may name `paths`. Trust and install recovery use the existing repository Settings flow.

The runtime serializes execution and coalesces pending recovery for each module. Init's format-aware helper reconciles an assessment's declared source includes before accepting ready or starting an index; changed admission requires indexing and another verification. Modules do not duplicate include-pattern interpretation. Setup and index actions run outside the index lane. An index action must submit through normal runtime indexing and verify the current input generation. AO KB reuses its compiler and index receipt wrapper, checks query coverage even for an empty KB, and never runs ingestion, curation, or site publishing. Existing SDL and executable-query conflicts block automatic work and preserve all source bytes.

Source-include updates prepare and classify all existing managed guidance through init's normal file plan before changing configuration. A malformed guidance fence therefore leaves configuration, documents, and ownership records unchanged. The same plan preserves locally edited blocks and unrelated template blocks.

Include reconciliation loads and edits the current configuration and publishes guidance under the same cross-process lock used by ordinary configuration saves and init publication. Init rechecks its captured configuration under that lock and refuses a stale plan before writing. Guidance documents are rechecked after preparation and before each write; a concurrent edit stops publication and remains intact. An unchanged configuration update preserves the authored file bytes.

`GET /api/v1/starter-readiness` returns the shared snapshot without running actions or requiring GraphQL. Each entry names `module`, `title`, `state`, `reason`, and `recovery`. States are `checking`, `working`, `required_input`, `blocked`, `failed`, and `ready`.

`POST /api/v1/starter-readiness/<module>` accepts `{"kind":"retry"}` or `{"kind":"choice","value":"<offered value>"}`. It returns 202 when queued and 409 if the current snapshot does not offer that action. The normal mutation origin guard applies. `starter-readiness.changed` on the existing global event stream tells clients to refresh the HTTP snapshot. Clients also refresh after stream reconnect.

Desktop trust recovery opens repository Settings, where an untrusted configured worktree offers an explicit Trust this worktree action. It uses the existing scoped trust command and refreshes Settings; the readiness worker observes changed trust without requiring a runtime restart.

Unchanged ready inputs do not repeat setup or indexing. On reopen, ready state is observed again against current query/index evidence. Other terminal outcomes retain a fingerprinted local receipt in ignored `.rhizome/state/module-readiness.json`, preventing identical failed work from restarting. A relevant file change, changed trust, or explicit retry admits another assessment. Fixing dependencies outside the repository requires Retry.

AO KB observes `.rhizome/ignore`, `.obsidianignore`, and `.gitignore`, so changes to note admission invalidate ready and failed states. An action failure after inputs change returns to checking rather than caching the obsolete error against the new inputs. Lock and staging files are excluded from fingerprints.

AO binding operations atomically publish a lock file containing the process ID and a unique staging token. Only the elected runtime's serial lifecycle runner sets `RZM_MODULE_LIFECYCLE=1`. It may reclaim a lock after the recorded process has stopped; ordinary action runners remove this flag and never reclaim locks. Unknown legacy locks and live owners remain blocked. Staging and lock files stay ignored by Git.

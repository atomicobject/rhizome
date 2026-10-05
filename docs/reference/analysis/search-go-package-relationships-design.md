---
summary: "Bounded package-level Go relationship analysis for exact calls and implicit implementations."
reference-kind: analysis
status: proposed
tags: [subsystem/code-intel, search-quality]
---

# Go package relationship analysis

## Decision boundary

Use `go/types` over an immutable snapshot of each affected local Go package. Do not build a second, partial Go type system from parser facts. The first delivery resolves same-package calls and implicit interface implementations across sibling files. External-package relationships remain explicitly unavailable when local type checking cannot prove them.

## Package identity and source snapshot

- Group by module import path, source directory, package name, and build variant. Keep `foo` and external `foo_test` separate. Files excluded by the repository's active build policy cannot contribute facts.
- Capture every indexed sibling's vault-relative path, content hash, parse status, and bytes before analysis. Read paths through `pkg/paths` under the configured vault root; a symlink or path escape fails the package analysis.
- Bind the snapshot to a directory-wide package-membership digest over every persisted Go sibling path, hash, parse status, package identity, and build variant. Verify the digest before analysis and again in the atomic relationship replacement. This detects a concurrently added, deleted, moved, or changed sibling rather than validating only the caller's listed hashes. A mismatch leaves the previous trusted package relationships intact and reports a stale-snapshot diagnostic.
- A missing, deleted, or parse-failed sibling makes the affected type graph incomplete. The analyzer may retain independently proven calls, but it must not claim a complete method set or an implicit implementation from that package snapshot.

## Offline type checking

- Parse package files together and type-check them with `go/types`, recording definitions, uses, selections, scopes, and package objects.
- The first delivery uses a rejecting offline importer for every import except `unsafe`. Standard-library imports such as `testing` and `fmt` are recorded as unavailable; local relationships may still be emitted only when `go/types` produced complete local objects and the proof's type graph does not depend on the unavailable import. This is conservative and works on Go installations without `GOROOT/pkg` export archives. A later GOROOT source importer may improve coverage, but the analyzer never invokes `go list`, another subprocess, the network, or the module cache.
- Unavailable external or workspace imports return a typed unavailable-dependency diagnostic. A relationship is emitted only when every object and type in its proof is valid and complete despite other reported type errors.
- `types.Info.Selections` supplies the exact receiver, selected field chain, and method object for calls such as `q.Drain()` and `w.Queue.Drain()`. Preserve pointer receiver semantics and canonical declaration FQNs.
- `types.Implements` and Go method sets determine local implicit implementations. Emit the canonical named-type identity when only its pointer method set implements an interface, because the index has no separate pointer declaration anchor.

## Derived relationship contract

Package analysis emits typed records rather than mutating parser summaries:

```go
type GoPackageKey struct {
    ImportPath  string
    Directory   string
    PackageName string
    BuildVariant string
}

type GoDerivedRelationship struct {
    Kind       GoRelationshipKind // calls or implements
    SourcePath string
    SourceFQN  string
    TargetFQN  string
    PointerOnly bool
}

type GoPackageRelationshipReplacement struct {
    Package       GoPackageKey
    MembershipDigest string
    SourceHashes  map[string]string
    Complete      bool
    Diagnostics   []GoRelationshipDiagnostic
    Relationships []GoDerivedRelationship
}
```

The store owns origin-separated package state and derived relationship tables. The numbered Intel migration uses this exact schema:

```sql
CREATE TABLE intel_go_package_relationship_state (
    package_key TEXT PRIMARY KEY CHECK (package_key != ''),
    import_path TEXT NOT NULL,
    directory TEXT NOT NULL,
    package_name TEXT NOT NULL,
    build_variant TEXT NOT NULL,
    membership_digest TEXT NOT NULL CHECK (membership_digest != ''),
    analyzer_version TEXT NOT NULL CHECK (analyzer_version != ''),
    complete INTEGER NOT NULL CHECK (complete IN (0, 1)),
    valid INTEGER NOT NULL CHECK (valid IN (0, 1)),
    diagnostics_json TEXT NOT NULL DEFAULT '[]',
    updated_at INTEGER NOT NULL CHECK (updated_at >= 0)
) WITHOUT ROWID, STRICT;

CREATE TABLE intel_go_package_files (
    source_path TEXT PRIMARY KEY,
    package_key TEXT NOT NULL CHECK (package_key != ''),
    import_path TEXT NOT NULL,
    directory TEXT NOT NULL,
    package_name TEXT NOT NULL,
    build_variant TEXT NOT NULL,
    FOREIGN KEY (source_path) REFERENCES files(path) ON DELETE CASCADE
) WITHOUT ROWID, STRICT;

CREATE INDEX idx_intel_go_package_files_package
    ON intel_go_package_files(package_key, source_path);
CREATE INDEX idx_intel_go_package_files_directory
    ON intel_go_package_files(directory, source_path);

CREATE TABLE intel_go_derived_relationships (
    package_key TEXT NOT NULL,
    kind TEXT NOT NULL CHECK (kind IN ('calls', 'implements')),
    source_path TEXT NOT NULL,
    source_fqn TEXT NOT NULL,
    target_fqn TEXT NOT NULL,
    pointer_only INTEGER NOT NULL DEFAULT 0 CHECK (pointer_only IN (0, 1)),
    PRIMARY KEY (package_key, kind, source_path, source_fqn, target_fqn),
    FOREIGN KEY (package_key)
        REFERENCES intel_go_package_relationship_state(package_key)
        ON DELETE CASCADE
) WITHOUT ROWID, STRICT;

CREATE INDEX idx_intel_go_derived_relationship_target
    ON intel_go_derived_relationships(kind, target_fqn, source_fqn, source_path);
CREATE INDEX idx_intel_go_derived_relationship_source
    ON intel_go_derived_relationships(source_path, kind, source_fqn, target_fqn);
```

Parser-produced symbol refs and explicit super edges remain unchanged. Schema validation checks the exact columns, foreign key, checks, and indexes; drift at the current schema version follows the existing Intel-domain recovery policy.

The normal code-persistence transaction replaces `intel_go_package_files` for successfully parsed Go files; a parse failure preserves the prior trusted identity while the existing `files.parse_status` becomes non-OK. Atomic relationship replacement scans every persisted immediate Go sibling in the directory, left-joins package identity, and recomputes the package membership digest inside the transaction. Thus a new parse-failed file with no identity and an old file that newly fails both invalidate completeness. It compares the digest with `MembershipDigest`, replaces all prior derived rows for the exact package key, and advances `intel_go_relationship_generation` in `index_metadata` in the same commit. `SearchCodeCorpusFingerprint` includes that generation, so committed relationship changes invalidate continuations even when source hashes are unchanged. Cancellation, validation failure, or insertion failure rolls back the whole replacement. Replacing an old and new package identity after a move happens in one transaction.

The normal file-persistence transaction marks every old and new affected package state `valid=0` before it commits. Every derived relationship read joins `valid=1`, so a source change immediately hides the prior proof. The first implementation reads derived calls directly through the shared caller-query seam rather than copying them into `intel_edges`; no second materialized call edge can outlive invalidation. If materialization is added later, its rows require the same package origin and transaction-owned invalidation.

An incomplete replacement deletes every prior derived row for that package, writes `complete=0` plus diagnostics, and inserts only independently proven exact calls. It emits no implicit implementation. This prevents an old complete method-set proof from surviving missing/deleted/parse-failed siblings while leaving parser refs and explicit super edges untouched.

The shared call-edge rebuild consumes exact derived call records alongside parser refs. `Children`/`ChildrenBatch` union derived `implements` records with explicit super edges. Derived rows never become definitions or search sources by themselves.

## Scheduling and invalidation

- The normal parallel file parser and `ApplyCodePersistenceBatch` remain the hot ingest path.
- The reducer records old and new Go package keys for changed paths. After the ordinary writer flushes, analyze each affected package once from the current local source snapshot, then atomically replace its derived relationships.
- Full indexing analyzes all indexed Go packages after the last file batch. Incremental indexing analyzes the union of old and new package keys. Delete, purge, move, and package rename capture the old key before removal and recompute surviving siblings.
- A method-only file deletion must remove calls and implementations that depended on it. A missing or failed sibling cannot leave the prior package proof labeled current.
- Exact derived calls feed the existing deferred call-edge rebuild before scope recomputation. Relationship replacement invalidates continuation generation just like other committed retrieval-population changes.

## Required verification

- Split-file direct calls, nested selectors, pointer methods, same-name receiver distractors, and external test packages.
- Split-file interfaces and concrete methods, value and pointer method sets, wrong signatures, embedded/promoted methods, aliases, and incomplete type graphs.
- Missing imports remain unavailable without subprocesses or network access; unrelated errors cannot authorize a partial relationship.
- Stale snapshot rejection, cancellation, injected rollback, reopen, method-only deletion, interface deletion, purge, move, and old/new package replacement.
- Adaptive writer batches prove analysis happens after all siblings are durable. A real rebuilt fixture proves definition, callers, tests, and implementers through the public retrieval path.

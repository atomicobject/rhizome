# Legacy process fingerprint catalog provenance

`legacy_process_fingerprints.json` is the runtime catalog for exact legacy
process-document retirement. `legacy_process_fingerprints_provenance.json` is
its checked-in, immutable evidence: every `[sha256, commit, blob]` tuple names
the historical source path and Git objects that produced one catalog digest.
Neither file requires Git or network access at runtime.

The evidence file is the authority for deterministic regeneration. From the
repository root, regenerate a candidate catalog from its pinned records:

```sh
jq -S '.paths | with_entries(.value |= (map(.[0]) | sort))' \
  pkg/app/cli/init/legacy_process_fingerprints_provenance.json \
  > /tmp/legacy_process_fingerprints.json
cmp /tmp/legacy_process_fingerprints.json \
  pkg/app/cli/init/legacy_process_fingerprints.json
```

Before accepting a regenerated catalog, run:

```sh
go test -tags fts5 ./pkg/app/cli/init -run '^TestLegacyProcessFingerprintCatalogIsCompleteAndStructured$'
```

This check requires exact catalog/provenance path-and-digest coverage and
valid commit and blob object identifiers. It runs in ordinary CI without
requiring historical Git objects, including in a repository with one initial
commit.

Before changing historical records, also run the opt-in audit in the private
archive with the full original Git history:

```sh
RZM_AUDIT_LEGACY_PROCESS_PROVENANCE=1 go test -tags fts5 ./pkg/app/cli/init -run '^TestLegacyProcessFingerprintCatalogMatchesGitHistory$' -count=1
```

The audit resolves every blob at its recorded commit and hashes the blob bytes.
It intentionally fails when those objects are unavailable. To extend history
coverage, add explicit rows from local repository history to the provenance
file first, then regenerate the catalog
from those pinned rows. Do not use mutable ref traversal as the source of
authority.

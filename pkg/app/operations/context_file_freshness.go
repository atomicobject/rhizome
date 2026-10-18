package operations

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/paths"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/notediscovery"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
)

func indexedFileContextFreshness(vault repoconfig.VaultManager, params FileContextTextParams) (IndexedContextFreshness, int, *IndexedContextWarning, error) {
	if params.IndexedUnavailable != nil {
		freshness := *params.IndexedUnavailable
		warning := indexedContextWarning(freshness, "indexed enrichment is unavailable because the unified index could not be opened")
		return freshness, 0, &warning, nil
	}
	if params.SessionStore == nil {
		freshness := IndexedContextFreshness{
			State:       IndexedContextMissing,
			WarningCode: "indexed-context-missing",
			Remediation: "rzm index",
		}
		warning := indexedContextWarning(freshness, "indexed enrichment is unavailable because the unified index could not be opened")
		return freshness, 0, &warning, nil
	}

	ctx := params.Context
	if ctx == nil {
		ctx = context.Background()
	}
	inputs, err := resolveContextInputs(vault, params.Profile)
	if err != nil {
		return IndexedContextFreshness{}, 0, nil, err
	}
	expectedScopeHash := ""
	if inputs.LocalCfg != nil && inputs.LocalCfg.cfg != nil {
		expectedScopeHash = inputs.LocalCfg.cfg.ScopeConfigHash()
	}
	snapshot, snapshotErr := params.SessionStore.CodeIndexPrerequisiteSnapshot(ctx)
	if snapshotErr != nil {
		if ctx.Err() != nil {
			return IndexedContextFreshness{}, 1, nil, ctx.Err()
		}
		freshness := IndexedContextFreshness{
			State:       IndexedContextMissing,
			WarningCode: "indexed-context-missing",
			Remediation: "rzm index",
		}
		warning := indexedContextWarning(freshness, fmt.Sprintf("indexed enrichment metadata is unavailable: %v", snapshotErr))
		return freshness, 1, &warning, nil
	}
	freshness := EvaluateIndexedContextFreshness(IndexedContextFreshnessEvidence{
		SchemaCompatible:  true,
		HasIndexerVersion: snapshot.IndexerVersionPresent,
		IndexerVersion:    snapshot.IndexerVersion,
		ExpectedVersion:   codeanchor.IndexerVersion,
		HasScopeHash:      snapshot.ScopeConfigHashPresent,
		ScopeHash:         snapshot.ScopeConfigHash,
		ExpectedScopeHash: expectedScopeHash,
	})
	if freshness.State != IndexedContextAvailable {
		warning := indexedContextWarning(freshness, "indexed enrichment is unavailable or does not match the current index contract")
		return freshness, 1, &warning, nil
	}

	fileReads, warning, err := indexedRequestedFileFreshness(ctx, params.SessionStore, inputs, params.NoteMetadata, params.Files)
	if err != nil {
		return IndexedContextFreshness{}, 1 + fileReads, nil, err
	}
	if warning != nil {
		return IndexedContextFreshness{
			State:       IndexedContextStale,
			WarningCode: warning.Code,
			Remediation: warning.Remediation,
		}, 1 + fileReads, warning, nil
	}
	return freshness, 1 + fileReads, nil, nil
}

func indexedRequestedFileFreshness(
	ctx context.Context,
	store *semdb.Store,
	inputs resolvedContextInputs,
	noteMetadata notemeta.Indexer,
	requested []string,
) (int, *IndexedContextWarning, error) {
	vaultPaths, err := paths.NewVaultPaths(inputs.VaultDef.BasePath())
	if err != nil {
		return 0, nil, err
	}
	seen := make(map[string]struct{}, len(requested))
	reads := 0
	for _, target := range requested {
		if err := ctx.Err(); err != nil {
			return reads, nil, err
		}
		abs := target
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(inputs.ProjectRoot, abs)
		}
		if resolved := paths.ResolveSymlinks(abs).String(); resolved != "" {
			abs = resolved
		}
		info, statErr := os.Stat(abs)
		if statErr != nil {
			return reads, staleRequestedFileWarning(target, "the requested file cannot be read"), nil
		}
		if !info.Mode().IsRegular() {
			continue
		}
		rel, relErr := vaultPaths.RelStrict(abs)
		if relErr != nil || rel.String() == "" {
			return reads, staleRequestedFileWarning(target, "the requested file is outside the indexed vault"), nil
		}
		relPath := rel.String()
		if _, ok := seen[relPath]; ok {
			continue
		}
		seen[relPath] = struct{}{}

		content, readErr := os.ReadFile(abs)
		if readErr != nil {
			return reads, staleRequestedFileWarning(relPath, "the requested file cannot be hashed"), nil
		}
		sum := sha256.Sum256(content)
		currentHash := hex.EncodeToString(sum[:])

		classification, classifyErr := classifyConfiguredPath(inputs.VaultDef, abs, noteMetadata)
		if classifyErr != nil {
			return reads, staleRequestedFileWarning(relPath, "the requested file cannot be classified"), nil
		}
		if classification.Owner == notediscovery.Note {
			indexedHash, indexerVersion, _, ok, metaErr := store.NoteIndexMeta(ctx, string(paths.EnsureNoteExtension(relPath, ".md")))
			reads++
			if metaErr != nil {
				if ctx.Err() != nil {
					return reads, nil, ctx.Err()
				}
				return reads, staleRequestedFileWarning(relPath, "indexed note metadata could not be read"), nil
			}
			if !ok || indexedHash == "" || indexerVersion != codeanchor.NoteIndexerVersion {
				return reads, staleRequestedFileWarning(relPath, "indexed note metadata is missing or outdated"), nil
			}
			if indexedHash != currentHash {
				return reads, staleRequestedFileWarning(relPath, "the requested note changed since indexing"), nil
			}
			continue
		}

		indexedHash, indexerVersion, parseStatus, ok, metaErr := store.FileHash(ctx, string(paths.NormalizeCode(relPath)))
		reads++
		if metaErr != nil {
			if ctx.Err() != nil {
				return reads, nil, ctx.Err()
			}
			return reads, staleRequestedFileWarning(relPath, "indexed code metadata could not be read"), nil
		}
		if !ok || indexedHash == "" || indexerVersion != codeanchor.IndexerVersion || !parseStatus.TrustedForIndexing() {
			return reads, staleRequestedFileWarning(relPath, "indexed code metadata is missing or outdated"), nil
		}
		if indexedHash != currentHash {
			return reads, staleRequestedFileWarning(relPath, "the requested code file changed since indexing"), nil
		}
	}
	return reads, nil, nil
}

func staleRequestedFileWarning(path, reason string) *IndexedContextWarning {
	return &IndexedContextWarning{
		Code:        "indexed-context-stale",
		Message:     fmt.Sprintf("indexed enrichment is unavailable for %q because %s", path, reason),
		Remediation: "rzm index",
	}
}

package validationrun

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/ontology"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/validate"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"strings"
	"time"
)

// IndexValidationSnapshot runs the configured default suite and returns the
// complete sanitized cache contract. Private repair authority and executable
// operation payloads remain only in the in-memory Result.
func IndexValidationSnapshot(ctx context.Context, noteMetadata notemeta.Indexer, vaultDef obsidian.VaultDefinition, runtime *ontology.Runtime, runtimeErr error, maxIssues int) (semdb.ValidationSnapshot, int64, error) {
	started := time.Now()
	if maxIssues <= 0 {
		maxIssues = 500
	}
	prepared, err := Prepare(vaultDef, noteMetadata, nil, maxIssues)
	if err != nil {
		return semdb.ValidationSnapshot{}, time.Since(started).Milliseconds(), err
	}
	result, err := RunPrepared(ctx, prepared, validate.ScanOptions{MaxIssues: maxIssues}, runtime, runtimeErr)
	durationMs := time.Since(started).Milliseconds()
	if err != nil {
		return semdb.ValidationSnapshot{}, durationMs, err
	}
	vaultIdentity := strings.TrimSpace(vaultDef.Name)
	if vaultIdentity == "" {
		vaultIdentity = strings.TrimSpace(vaultDef.BasePath())
	}
	schemaIdentity := ""
	inputRevision := ""
	if runtime != nil {
		if runtime.Schema != nil {
			schemaIdentity = runtime.Schema.Hash
		}
		if runtime.Store != nil {
			if state, stateErr := runtime.Store.GetNoteMetadataState(ctx); stateErr == nil {
				inputRevision = state.NotesHash
			}
		}
	}
	configBytes, _ := json.Marshal(result.SelectedChecks)
	configDigest := sha256.Sum256(configBytes)
	var notePaths []string
	if runtime != nil && runtime.Store != nil {
		notePaths, err = runtime.Store.NotePaths(ctx)
		if err != nil {
			return semdb.ValidationSnapshot{}, durationMs, fmt.Errorf("read indexed note inventory: %w", err)
		}
	}
	snapshot, err := BuildDiagnosticSnapshot(result, validate.DiagnosticSnapshotContext{
		VaultIdentity: vaultIdentity, Scope: "default", SchemaIdentity: schemaIdentity,
		ConfigIdentity: fmt.Sprintf("sha256:%x", configDigest[:]), InputRevision: inputRevision,
		StartedAt: started.Unix(), FinishedAt: time.Now().Unix(), DurationMs: durationMs,
		NotePaths: notePaths,
	})
	return snapshot, durationMs, err
}

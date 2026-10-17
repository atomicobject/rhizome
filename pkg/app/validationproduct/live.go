package validationproduct

import (
	"context"
	"fmt"
	"slices"

	"github.com/atomicobject/rhizome/pkg/app/bootstrap/lane"
	"github.com/atomicobject/rhizome/pkg/app/validationrun"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/validate"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// AuthoritativeRun retains the exact in-process inputs needed to create a
// repair review. It must never be reconstructed from the public snapshot.
type AuthoritativeRun struct {
	Result     validationrun.ValidationResult
	RunContext validate.RunContext
	NotePaths  []string
}

// RunLive executes the configured default validation product against the live
// projection and returns its un-serialized authority.
func RunLive(
	ctx context.Context,
	noteMetadata notemeta.Indexer,
	vaultDef obsidian.VaultDefinition,
	maxIssues int,
) (AuthoritativeRun, error) {
	return RunLiveOnLane(ctx, noteMetadata, vaultDef, maxIssues, nil)
}

// RunLiveOnLane serializes live projection writes with runtime indexing jobs.
// Checks run after the projection has released the lane's index lock.
func RunLiveOnLane(ctx context.Context, noteMetadata notemeta.Indexer, vaultDef obsidian.VaultDefinition, maxIssues int, indexLane lane.Lane) (AuthoritativeRun, error) {
	if maxIssues <= 0 {
		maxIssues = 500
	}
	prepared, err := validationrun.Prepare(vaultDef, noteMetadata, nil, maxIssues)
	if err != nil {
		return AuthoritativeRun{}, err
	}
	runContext := prepared.RunContext
	reader := runContext.NoteReader
	request := prepared.Request
	request.Surface = validate.SurfaceLocal
	result, err := RunWithProjection(ctx, request, ValidationProjectionRequest{
		VaultPath: runContext.VaultPath, VaultDef: runContext.VaultDef, NoteMetadata: noteMetadata,
		NoteReader: reader, Target: ValidationProjectionLive, Lane: indexLane,
	}, validate.ScanOptions{MaxIssues: maxIssues, RunContext: &runContext})
	if err != nil {
		return AuthoritativeRun{}, err
	}
	notePaths, err := obsidian.NotesListContext(ctx, runContext.VaultDef, reader)
	if err != nil {
		return AuthoritativeRun{}, fmt.Errorf("read validation note inventory: %w", err)
	}
	slices.Sort(notePaths)
	return AuthoritativeRun{Result: result, RunContext: runContext, NotePaths: notePaths}, nil
}

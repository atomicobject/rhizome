package indexing

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// NodeLinkApplyRequest supplies the authority required to make embedded
// ontology link targets durable and converge their exact source paths.
type NodeLinkApplyRequest struct {
	VaultDef     obsidian.VaultDefinition
	NoteMetadata notemeta.Indexer
	NoteReader   obsidian.NoteReader
	SchemaHash   string
	LinkTarget   ontology.LinkTargetRequest
}

type nodeLinkProjectionFunc func(
	context.Context,
	ValidationProjectionRequest,
	*semdb.Store,
	string,
	func() error,
) (*ValidationProjectionResult, error)

// ApplyNodeLinkTargets commits requested block identifiers, then publishes
// metadata and ontology rows for the exact source notes through the live
// writer's queued convergence lane.
func ApplyNodeLinkTargets(ctx context.Context, request NodeLinkApplyRequest) (ontology.LinkTargetResult, error) {
	return applyNodeLinkTargets(ctx, request, refreshValidationProjectionWithHeldStore)
}

// applyNodeLinkTargets accepts its projection dependency only for package-local
// failure tests. Public callers always use the retained-store core.
func applyNodeLinkTargets(ctx context.Context, request NodeLinkApplyRequest, project nodeLinkProjectionFunc) (ontology.LinkTargetResult, error) {
	projectionRequest, exactPaths, err := normalizeNodeLinkApplyRequest(ctx, request)
	if err != nil {
		return ontology.LinkTargetResult{}, err
	}

	release, err := TryAcquireIndexLock(ctx, obsidian.IndexLockPath(projectionRequest.VaultPath), true, false, &bytes.Buffer{})
	if err != nil {
		return ontology.LinkTargetResult{}, err
	}
	result, runErr := applyNodeLinkTargetsWithHeldIndexLock(ctx, request, projectionRequest, exactPaths, project)
	releaseErr := release()
	if releaseErr != nil && runErr == nil {
		if result.Applied {
			releaseErr = fmt.Errorf("source applied and index convergence completed, but index lock release failed: %w", releaseErr)
		} else {
			releaseErr = fmt.Errorf("index convergence completed, but index lock release failed: %w", releaseErr)
		}
	}
	if runErr != nil || releaseErr != nil {
		return result, errors.Join(runErr, releaseErr)
	}
	return result, nil
}

func normalizeNodeLinkApplyRequest(ctx context.Context, request NodeLinkApplyRequest) (ValidationProjectionRequest, ValidationProjectionPaths, error) {
	if err := ctx.Err(); err != nil {
		return ValidationProjectionRequest{}, ValidationProjectionPaths{}, err
	}
	if err := request.NoteMetadata.Validate(); err != nil {
		return ValidationProjectionRequest{}, ValidationProjectionPaths{}, fmt.Errorf("note metadata indexer: %w", err)
	}
	if request.NoteReader == nil {
		return ValidationProjectionRequest{}, ValidationProjectionPaths{}, fmt.Errorf("note reader is required")
	}
	if strings.TrimSpace(request.SchemaHash) == "" {
		return ValidationProjectionRequest{}, ValidationProjectionPaths{}, fmt.Errorf("expected schema hash is required")
	}
	if request.LinkTarget.Ensure != ontology.EnsureLinkTargetApply {
		return ValidationProjectionRequest{}, ValidationProjectionPaths{}, fmt.Errorf("ensure link target apply is required")
	}

	projectionRequest := normalizeValidationProjectionRequest(ValidationProjectionRequest{
		VaultPath:    request.VaultDef.BasePath(),
		VaultDef:     request.VaultDef,
		NoteMetadata: request.NoteMetadata,
		NoteReader:   request.NoteReader,
		Target:       ValidationProjectionLive,
	})
	if err := validateValidationProjectionRequest(projectionRequest); err != nil {
		return ValidationProjectionRequest{}, ValidationProjectionPaths{}, err
	}

	vaultPaths, err := paths.NewVaultPaths(projectionRequest.VaultPath)
	if err != nil {
		return ValidationProjectionRequest{}, ValidationProjectionPaths{}, fmt.Errorf("resolve vault paths: %w", err)
	}
	if len(request.LinkTarget.Refs) == 0 {
		return ValidationProjectionRequest{}, ValidationProjectionPaths{}, fmt.Errorf("link target refs are required")
	}
	changed := make([]paths.NotePath, 0, len(request.LinkTarget.Refs))
	for _, ref := range request.LinkTarget.Refs {
		if ref.Kind != ontology.NodeKindEmbedded {
			return ValidationProjectionRequest{}, ValidationProjectionPaths{}, fmt.Errorf("link target ref %q must be an embedded node", ref.String())
		}
		notePath, pathErr := vaultPaths.RelNotePathStrict(ref.NotePath)
		if pathErr != nil {
			return ValidationProjectionRequest{}, ValidationProjectionPaths{}, fmt.Errorf("normalize link target path %q: %w", ref.NotePath, pathErr)
		}
		if notePath == "" {
			return ValidationProjectionRequest{}, ValidationProjectionPaths{}, fmt.Errorf("link target note path is required")
		}
		changed = append(changed, notePath)
	}
	normalizedChanged, err := normalizedNotePathStrings(vaultPaths, changed)
	if err != nil {
		return ValidationProjectionRequest{}, ValidationProjectionPaths{}, fmt.Errorf("normalize link target paths: %w", err)
	}
	return projectionRequest, ValidationProjectionPaths{Changed: stringPathsToNotePaths(normalizedChanged)}, nil
}

func applyNodeLinkTargetsWithHeldIndexLock(
	ctx context.Context,
	request NodeLinkApplyRequest,
	projectionRequest ValidationProjectionRequest,
	exactPaths ValidationProjectionPaths,
	project nodeLinkProjectionFunc,
) (ontology.LinkTargetResult, error) {
	store, indexPath, cleanup, err := openValidationProjectionStore(projectionRequest)
	if err != nil {
		return ontology.LinkTargetResult{}, err
	}
	// Both failure paths may receive the owner; close the live store once.
	cleanup = sync.OnceValue(cleanup)
	storeOwned := true
	defer func() {
		if storeOwned {
			_ = cleanup()
		}
	}()

	schema, err := ontology.LoadSchema(projectionRequest.VaultPath)
	if err != nil {
		return ontology.LinkTargetResult{}, fmt.Errorf("load ontology schema: %w", err)
	}
	if schema.Hash != request.SchemaHash {
		return ontology.LinkTargetResult{}, fmt.Errorf("expected schema hash %q does not match current schema hash %q", request.SchemaHash, schema.Hash)
	}
	if err := ctx.Err(); err != nil {
		return ontology.LinkTargetResult{}, err
	}

	linkRequest := request.LinkTarget
	result, sourceErr := (&ontology.NodeLinkService{
		VaultDef:            projectionRequest.VaultDef,
		NoteReader:          projectionRequest.NoteReader,
		Schema:              schema,
		VaultWriteLeaseHeld: true,
	}).LinkTargets(ctx, linkRequest)
	if nodeLinkApplyFailed(result) {
		return result, fmt.Errorf("source apply failed")
	}
	if sourceErr != nil && !result.Applied {
		return result, fmt.Errorf("apply link targets: %w", sourceErr)
	}

	projectionRequest.ExactPaths = &exactPaths
	storeOwned = false
	projection, projectionErr := project(ctx, projectionRequest, store, indexPath, cleanup)
	if projectionErr != nil {
		if projection == nil {
			_ = cleanup()
		} else {
			_ = projection.Close()
		}
		return result, nodeLinkApplyConvergenceError(result, projectionErr)
	}
	if projection == nil {
		_ = cleanup()
		return result, nodeLinkApplyConvergenceError(result, fmt.Errorf("validation projection result is absent"))
	}
	if closeErr := projection.Close(); closeErr != nil {
		return result, nodeLinkApplyConvergenceError(result, closeErr)
	}
	if sourceErr != nil {
		return result, fmt.Errorf("source applied and index convergence completed, but refreshed link targets could not be read: %w", sourceErr)
	}
	return result, nil
}

func nodeLinkApplyFailed(result ontology.LinkTargetResult) bool {
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code == "apply_failed" {
			return true
		}
	}
	return false
}

func nodeLinkApplyConvergenceError(result ontology.LinkTargetResult, cause error) error {
	if result.Applied {
		return fmt.Errorf("source applied; index convergence incomplete: %w", cause)
	}
	return fmt.Errorf("index convergence incomplete: %w", cause)
}

package validationrun

import (
	"context"
	"fmt"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/validate"
)

// RunPrepared uses the indexing caller's completed projection without opening
// stores, refreshing data, or acquiring the index lock again.
func RunPrepared(ctx context.Context, prepared Prepared, options validate.ScanOptions, runtime *ontology.Runtime, runtimeErr error) (ValidationResult, error) {
	probe := preparedProbes{runtime: runtime, runtimeErr: runtimeErr, codeConfigured: prepared.Request.Features.CodeConfigured}
	request := prepared.Request
	request.Probes = validate.PrerequisiteProbes{Projection: probe, CodeIndex: probe}
	options.RunContext = &prepared.RunContext
	return RunValidation(ctx, request, func(ctx context.Context, checks []string) (validate.Result, error) {
		options.Checks = checks
		result, _, err := validate.RunSuiteOncePrepared(ctx, options, runtime, runtimeErr)
		return result, err
	})
}

type preparedProbes struct {
	runtime        *ontology.Runtime
	runtimeErr     error
	codeConfigured bool
}

func (p preparedProbes) AutoManagedProjection(ctx context.Context, _ validate.CheckDescriptor, domain validate.ProjectionDomain) (validate.AutoManagedProjectionSnapshot, error) {
	snapshot := validate.AutoManagedProjectionSnapshot{Domain: domain}
	if p.runtime == nil || p.runtime.Store == nil {
		if p.runtimeErr != nil {
			return snapshot, p.runtimeErr
		}
		return snapshot, nil
	}
	switch domain {
	case validate.ProjectionValidation:
		state, err := p.runtime.Store.GetNoteMetadataState(ctx)
		snapshot.Available = state.Ready
		return snapshot, err
	case validate.ProjectionOntology:
		if p.runtimeErr != nil {
			return snapshot, p.runtimeErr
		}
		state, err := p.runtime.Store.GetOntologySchemaState(ctx)
		snapshot.Available = state.Ready && state.MaterializationVersion == ontology.OntologyMaterializationVersion
		return snapshot, err
	default:
		return snapshot, fmt.Errorf("unsupported prepared projection domain %q", domain)
	}
}

func (p preparedProbes) CodeIndexSnapshot(ctx context.Context) (validate.CodeIndexSnapshot, error) {
	return ReadCodeIndexSnapshot(ctx, p.runtime, p.codeConfigured)
}

// ReadCodeIndexSnapshot reads persisted code facts without discovering files or
// building indexes. Scratch callers must pass no live runtime.
func ReadCodeIndexSnapshot(ctx context.Context, runtime *ontology.Runtime, configured bool) (validate.CodeIndexSnapshot, error) {
	snapshot := validate.CodeIndexSnapshot{ConfiguredCapability: configured}
	if runtime == nil || runtime.Store == nil {
		return snapshot, nil
	}
	snapshot.StorePresent = true
	stored, err := runtime.Store.CodeIndexPrerequisiteSnapshot(ctx)
	if err != nil {
		return snapshot, err
	}
	snapshot.RowsPresent = stored.CodeRowsPresent
	snapshot.IndexedFileCount = stored.IndexedFileCount
	if stored.IndexerVersionPresent {
		snapshot.IndexerVersion = stored.IndexerVersion
	}
	if stored.ScopeConfigHashPresent {
		snapshot.ScopeHash = stored.ScopeConfigHash
	}
	if stored.IndexedAtPresent {
		snapshot.IndexedAt = stored.IndexedAt
	}
	return snapshot, nil
}

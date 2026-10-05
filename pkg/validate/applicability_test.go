package validate

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestResolveCheckApplicability_FeatureAbsenceIsAnExplicitResult(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		applicability CheckApplicability
		facts         VaultFeatureFacts
	}{
		{name: "ontology", applicability: ApplicabilityOntology},
		{name: "code", applicability: ApplicabilityCode},
		{name: "query recipes", applicability: ApplicabilityQueryRecipes},
		{name: "views", applicability: ApplicabilityViews},
		{name: "efforts", applicability: ApplicabilityEfforts},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			probe := &prerequisiteProbeSpy{}
			descriptor := autoManagedDescriptor(tt.applicability)

			result := ResolveCheckApplicability(context.Background(), descriptor, SurfaceLocal, tt.facts, PrerequisiteProbes{
				Projection: probe,
				CodeIndex:  probe,
			})

			require.Equal(t, descriptor.Name, result.Check)
			require.Equal(t, CheckOutcomeNotApplicable, result.Outcome)
			require.Equal(t, []ApplicabilityEvidence{{
				Code:    EvidenceFeatureAbsent,
				Message: "vault feature is not configured or present",
			}}, result.Evidence)
			require.Empty(t, probe.projectionCalls)
			require.Zero(t, probe.codeSnapshotCalls)
		})
	}
}

func TestResolveCheckApplicability_AutoManagedProjection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		snapshot AutoManagedProjectionSnapshot
		err      error
		outcome  CheckOutcome
		code     string
	}{
		{
			name:     "available after refresh",
			snapshot: AutoManagedProjectionSnapshot{Available: true, Refreshed: true},
			outcome:  CheckOutcomeCompleted,
			code:     EvidenceProjectionAvailable,
		},
		{
			name:     "unavailable",
			snapshot: AutoManagedProjectionSnapshot{},
			outcome:  CheckOutcomeBlocked,
			code:     EvidenceProjectionUnavailable,
		},
		{
			name:    "refresh failure",
			err:     errors.New("refresh lock unavailable"),
			outcome: CheckOutcomeBlocked,
			code:    EvidenceProjectionProbeFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			probe := projectionProbe(tt.snapshot, tt.err)
			descriptor := autoManagedDescriptor(ApplicabilityAlways)

			result := ResolveCheckApplicability(context.Background(), descriptor, SurfaceLocal, VaultFeatureFacts{}, PrerequisiteProbes{Projection: probe})

			require.Equal(t, tt.outcome, result.Outcome)
			require.Len(t, result.Evidence, 1)
			require.Equal(t, tt.code, result.Evidence[0].Code)
			require.Equal(t, ProjectionValidation, result.Evidence[0].Projection.Domain)
			require.Equal(t, tt.snapshot.Available, result.Evidence[0].Projection.Available)
			require.Equal(t, tt.snapshot.Refreshed, result.Evidence[0].Projection.Refreshed)
			require.Equal(t, []ProjectionDomain{ProjectionValidation}, probe.projectionCalls)
		})
	}
}

func TestResolveCheckApplicability_ProjectionEvidenceIsPerEstablishedDomain(t *testing.T) {
	t.Parallel()

	descriptor := autoManagedDescriptor(ApplicabilityAlways)
	descriptor.ProjectionDomains = []ProjectionDomain{ProjectionValidation, ProjectionOntology}
	probe := &prerequisiteProbeSpy{projectionSnapshots: map[ProjectionDomain]AutoManagedProjectionSnapshot{
		ProjectionValidation: {Domain: ProjectionValidation, Available: true, Refreshed: true},
		ProjectionOntology:   {Domain: ProjectionOntology, Available: true},
	}}

	result := ResolveCheckApplicability(context.Background(), descriptor, SurfaceLocal, VaultFeatureFacts{}, PrerequisiteProbes{Projection: probe})

	require.Equal(t, CheckOutcomeCompleted, result.Outcome)
	require.Equal(t, []ProjectionDomain{ProjectionValidation, ProjectionOntology}, probe.projectionCalls)
	require.Len(t, result.Evidence, 2)
	require.Equal(t, ProjectionValidation, result.Evidence[0].Projection.Domain)
	require.True(t, result.Evidence[0].Projection.Refreshed)
	require.Equal(t, ProjectionOntology, result.Evidence[1].Projection.Domain)
	require.False(t, result.Evidence[1].Projection.Refreshed)
	for _, evidence := range result.Evidence {
		require.Equal(t, EvidenceProjectionAvailable, evidence.Code)
		require.NotEmpty(t, evidence.Projection.Domain)
	}
}

func TestResolveCheckApplicability_BlocksMismatchedPrerequisiteMode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		descriptor CheckDescriptor
	}{
		{
			name: "code domain declared auto managed",
			descriptor: func() CheckDescriptor {
				descriptor := codeDescriptor(SurfaceLocal)
				descriptor.PrerequisiteMode = PrerequisiteAutoManaged
				return descriptor
			}(),
		},
		{
			name: "validation domain declared external",
			descriptor: func() CheckDescriptor {
				descriptor := autoManagedDescriptor(ApplicabilityAlways)
				descriptor.PrerequisiteMode = PrerequisiteExternal
				return descriptor
			}(),
		},
		{
			name: "unknown mode",
			descriptor: func() CheckDescriptor {
				descriptor := autoManagedDescriptor(ApplicabilityAlways)
				descriptor.PrerequisiteMode = PrerequisiteMode("mystery")
				return descriptor
			}(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			probe := projectionProbe(AutoManagedProjectionSnapshot{Available: true}, nil)

			result := ResolveCheckApplicability(context.Background(), tt.descriptor, SurfaceLocal, VaultFeatureFacts{CodeConfigured: true}, PrerequisiteProbes{
				Projection: probe,
				CodeIndex:  probe,
			})

			require.Equal(t, CheckOutcomeBlocked, result.Outcome)
			require.Equal(t, "check prerequisite metadata is invalid", result.Summary)
			require.Empty(t, result.PreparationCommand)
			require.Len(t, result.Evidence, 1)
			require.Equal(t, EvidencePrerequisiteMetadataInvalid, result.Evidence[0].Code)
			require.Empty(t, probe.projectionCalls)
			require.Zero(t, probe.codeSnapshotCalls)
		})
	}
}

func TestResolveCheckApplicability_RejectsProjectionEvidenceForAnotherDomain(t *testing.T) {
	t.Parallel()

	descriptor := autoManagedDescriptor(ApplicabilityAlways)
	probe := &prerequisiteProbeSpy{projectionSnapshots: map[ProjectionDomain]AutoManagedProjectionSnapshot{
		ProjectionValidation: {Domain: ProjectionOntology, Available: true},
	}}

	result := ResolveCheckApplicability(context.Background(), descriptor, SurfaceLocal, VaultFeatureFacts{}, PrerequisiteProbes{Projection: probe})

	require.Equal(t, CheckOutcomeBlocked, result.Outcome)
	require.Len(t, result.Evidence, 1)
	require.Equal(t, EvidenceProjectionProbeFailed, result.Evidence[0].Code)
	require.Equal(t, ProjectionValidation, result.Evidence[0].Projection.Domain)
	require.Contains(t, result.Evidence[0].Message, `returned evidence for "ontology"`)
}

func TestResolveCheckApplicability_UsesDeclaredMissingPrerequisiteOutcome(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		err     error
		summary string
		code    string
	}{
		{
			name:    "projection unavailable",
			summary: "required validation projection is unavailable",
			code:    EvidenceProjectionUnavailable,
		},
		{
			name:    "projection probe failed",
			err:     errors.New("refresh failed"),
			summary: "required validation projection could not be prepared",
			code:    EvidenceProjectionProbeFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			descriptor := autoManagedDescriptor(ApplicabilityAlways)
			descriptor.MissingPrerequisiteOutcome = PrerequisiteNotApplicable
			probe := projectionProbe(AutoManagedProjectionSnapshot{}, tt.err)

			result := ResolveCheckApplicability(context.Background(), descriptor, SurfaceLocal, VaultFeatureFacts{}, PrerequisiteProbes{Projection: probe})

			require.Equal(t, CheckOutcomeNotApplicable, result.Outcome)
			require.Equal(t, tt.summary, result.Summary)
			require.Empty(t, result.PreparationCommand)
			require.Len(t, result.Evidence, 1)
			require.Equal(t, tt.code, result.Evidence[0].Code)
		})
	}
}

func TestResolveCheckApplicability_LocalCodeIndexSnapshot(t *testing.T) {
	t.Parallel()

	indexedAt := time.Date(2026, time.July, 15, 20, 0, 0, 0, time.UTC)
	usable := CodeIndexSnapshot{
		ConfiguredCapability: true,
		StorePresent:         true,
		RowsPresent:          true,
		IndexerVersion:       "v1.8.0",
		ScopeHash:            "scope-v2",
		IndexedFileCount:     42,
		IndexedAt:            indexedAt,
	}
	facts := VaultFeatureFacts{
		CodeConfigured:             true,
		RequiredCodeIndexerVersion: "v1.8.0",
		RequiredCodeScopeHash:      "scope-v2",
	}

	tests := []struct {
		name     string
		mutate   func(*CodeIndexSnapshot)
		probeErr error
		outcome  CheckOutcome
	}{
		{name: "usable", outcome: CheckOutcomeCompleted},
		{name: "configured capability absent", mutate: func(snapshot *CodeIndexSnapshot) { snapshot.ConfiguredCapability = false }, outcome: CheckOutcomeBlocked},
		{name: "store absent", mutate: func(snapshot *CodeIndexSnapshot) { snapshot.StorePresent = false }, outcome: CheckOutcomeBlocked},
		{name: "rows absent", mutate: func(snapshot *CodeIndexSnapshot) { snapshot.RowsPresent = false }, outcome: CheckOutcomeBlocked},
		{name: "indexer version missing", mutate: func(snapshot *CodeIndexSnapshot) { snapshot.IndexerVersion = "" }, outcome: CheckOutcomeBlocked},
		{name: "indexer version mismatch", mutate: func(snapshot *CodeIndexSnapshot) { snapshot.IndexerVersion = "v1.7.0" }, outcome: CheckOutcomeBlocked},
		{name: "scope hash missing", mutate: func(snapshot *CodeIndexSnapshot) { snapshot.ScopeHash = "" }, outcome: CheckOutcomeBlocked},
		{name: "scope hash mismatch", mutate: func(snapshot *CodeIndexSnapshot) { snapshot.ScopeHash = "scope-v1" }, outcome: CheckOutcomeBlocked},
		{name: "file count absent", mutate: func(snapshot *CodeIndexSnapshot) { snapshot.IndexedFileCount = 0 }, outcome: CheckOutcomeBlocked},
		{name: "indexed timestamp absent", mutate: func(snapshot *CodeIndexSnapshot) { snapshot.IndexedAt = time.Time{} }, outcome: CheckOutcomeBlocked},
		{name: "snapshot probe failed", probeErr: errors.New("database unavailable"), outcome: CheckOutcomeBlocked},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			snapshot := usable
			if tt.mutate != nil {
				tt.mutate(&snapshot)
			}
			probe := projectionProbe(AutoManagedProjectionSnapshot{Available: true, Refreshed: true}, nil)
			probe.codeSnapshot = snapshot
			probe.codeSnapshotErr = tt.probeErr

			result := ResolveCheckApplicability(context.Background(), codeDescriptor(SurfaceLocal, SurfaceAgent), SurfaceLocal, facts, PrerequisiteProbes{Projection: probe, CodeIndex: probe})

			require.Equal(t, tt.outcome, result.Outcome)
			require.Len(t, result.Evidence, 2)
			require.Equal(t, EvidenceProjectionAvailable, result.Evidence[0].Code)
			require.Equal(t, ProjectionValidation, result.Evidence[0].Projection.Domain)
			require.Equal(t, []ProjectionDomain{ProjectionValidation}, probe.projectionCalls)
			require.Equal(t, 1, probe.codeSnapshotCalls)
			if tt.outcome == CheckOutcomeCompleted {
				require.Equal(t, EvidenceCodeIndexUsable, result.Evidence[1].Code)
				require.Equal(t, ProjectionCode, result.Evidence[1].Projection.Domain)
				require.Contains(t, result.Evidence[1].Message, "does not prove live-worktree freshness")
				require.False(t, result.Evidence[1].CodeIndex.LiveWorktreeFreshnessProved)
				require.Equal(t, usable, result.Evidence[1].CodeIndex.Snapshot)
				require.Empty(t, result.PreparationCommand)
			} else {
				require.Contains(t, []string{EvidenceCodeIndexUnusable, EvidenceCodeIndexProbeFailed}, result.Evidence[1].Code)
				require.Equal(t, ProjectionCode, result.Evidence[1].Projection.Domain)
				require.Equal(t, "rzm index", result.PreparationCommand)
			}
		})
	}
}

func TestResolveCheckApplicability_CodeProbeDoesNotRunWhenValidationProjectionFails(t *testing.T) {
	t.Parallel()

	probe := projectionProbe(AutoManagedProjectionSnapshot{}, errors.New("validation refresh failed"))
	result := ResolveCheckApplicability(context.Background(), codeDescriptor(SurfaceLocal), SurfaceLocal, VaultFeatureFacts{CodeConfigured: true}, PrerequisiteProbes{
		Projection: probe,
		CodeIndex:  probe,
	})

	require.Equal(t, CheckOutcomeBlocked, result.Outcome)
	require.Len(t, result.Evidence, 1)
	require.Equal(t, EvidenceProjectionProbeFailed, result.Evidence[0].Code)
	require.Equal(t, ProjectionValidation, result.Evidence[0].Projection.Domain)
	require.Zero(t, probe.codeSnapshotCalls)
}

func TestResolveCheckApplicability_CodeMissPreservesPreparedDomainEvidence(t *testing.T) {
	t.Parallel()

	descriptor := codeDescriptor(SurfaceLocal)
	descriptor.MissingPrerequisiteOutcome = PrerequisiteNotApplicable
	probe := projectionProbe(AutoManagedProjectionSnapshot{Available: true, Refreshed: true}, nil)
	probe.codeSnapshot = CodeIndexSnapshot{ConfiguredCapability: true}

	result := ResolveCheckApplicability(context.Background(), descriptor, SurfaceLocal, VaultFeatureFacts{
		CodeConfigured:             true,
		RequiredCodeIndexerVersion: "v1.8.0",
		RequiredCodeScopeHash:      "scope-v2",
	}, PrerequisiteProbes{Projection: probe, CodeIndex: probe})

	require.Equal(t, CheckOutcomeNotApplicable, result.Outcome)
	require.Empty(t, result.PreparationCommand)
	require.Len(t, result.Evidence, 2)
	require.Equal(t, ProjectionValidation, result.Evidence[0].Projection.Domain)
	require.Equal(t, EvidenceProjectionAvailable, result.Evidence[0].Code)
	require.Equal(t, ProjectionCode, result.Evidence[1].Projection.Domain)
	require.Equal(t, EvidenceCodeIndexUnusable, result.Evidence[1].Code)
}

func TestResolveCheckApplicability_ScratchCIBlocksUnsupportedCodeCheckWithoutProbing(t *testing.T) {
	t.Parallel()

	probe := &prerequisiteProbeSpy{
		codeSnapshot: CodeIndexSnapshot{
			ConfiguredCapability: true,
			StorePresent:         true,
			RowsPresent:          true,
			IndexerVersion:       "v1.8.0",
			ScopeHash:            "scope-v2",
			IndexedFileCount:     42,
			IndexedAt:            time.Date(2026, time.July, 15, 20, 0, 0, 0, time.UTC),
		},
	}

	result := ResolveCheckApplicability(context.Background(), codeDescriptor(SurfaceLocal, SurfaceAgent), SurfaceCI, VaultFeatureFacts{
		CodeConfigured:             true,
		RequiredCodeIndexerVersion: "v1.8.0",
		RequiredCodeScopeHash:      "scope-v2",
	}, PrerequisiteProbes{Projection: probe, CodeIndex: probe})

	require.Equal(t, CheckOutcomeBlocked, result.Outcome)
	require.Equal(t, "rzm validate code-anchors", result.PreparationCommand)
	require.Equal(t, []ApplicabilityEvidence{{
		Code:    EvidenceExecutionSurfaceUnsupported,
		Message: "check requires the live persisted code index and is unavailable in isolated scratch CI; run it locally or remove it from the configured CI suite",
	}}, result.Evidence)
	require.Zero(t, probe.codeSnapshotCalls)
	require.Empty(t, probe.projectionCalls)
}

func autoManagedDescriptor(applicability CheckApplicability) CheckDescriptor {
	return CheckDescriptor{
		Name:                       "test_check",
		Applicability:              applicability,
		ProjectionDomains:          []ProjectionDomain{ProjectionValidation},
		ExecutionSurfaces:          []ExecutionSurface{SurfaceLocal, SurfaceCI, SurfaceAgent},
		PrerequisiteMode:           PrerequisiteAutoManaged,
		MissingPrerequisiteOutcome: PrerequisiteBlocked,
	}
}

func codeDescriptor(surfaces ...ExecutionSurface) CheckDescriptor {
	return CheckDescriptor{
		Name:                       CheckCodeAnchors,
		Applicability:              ApplicabilityCode,
		ProjectionDomains:          []ProjectionDomain{ProjectionValidation, ProjectionCode},
		ExecutionSurfaces:          surfaces,
		PrerequisiteMode:           PrerequisiteExternal,
		MissingPrerequisiteOutcome: PrerequisiteBlocked,
		PreparationCommand:         "rzm index",
	}
}

type prerequisiteProbeSpy struct {
	projectionSnapshots map[ProjectionDomain]AutoManagedProjectionSnapshot
	projectionErrors    map[ProjectionDomain]error
	codeSnapshot        CodeIndexSnapshot
	codeSnapshotErr     error

	projectionCalls   []ProjectionDomain
	codeSnapshotCalls int
}

func (s *prerequisiteProbeSpy) AutoManagedProjection(_ context.Context, _ CheckDescriptor, domain ProjectionDomain) (AutoManagedProjectionSnapshot, error) {
	s.projectionCalls = append(s.projectionCalls, domain)
	return s.projectionSnapshots[domain], s.projectionErrors[domain]
}

func (s *prerequisiteProbeSpy) CodeIndexSnapshot(context.Context) (CodeIndexSnapshot, error) {
	s.codeSnapshotCalls++
	return s.codeSnapshot, s.codeSnapshotErr
}

func projectionProbe(snapshot AutoManagedProjectionSnapshot, err error) *prerequisiteProbeSpy {
	if snapshot.Domain == "" {
		snapshot.Domain = ProjectionValidation
	}
	return &prerequisiteProbeSpy{
		projectionSnapshots: map[ProjectionDomain]AutoManagedProjectionSnapshot{ProjectionValidation: snapshot},
		projectionErrors:    map[ProjectionDomain]error{ProjectionValidation: err},
	}
}

func TestResolveCheckApplicability_CodeAnchorsRequireLanguageRoots(t *testing.T) {
	t.Parallel()

	registered, ok := lookupCheck(CheckCodeAnchors)
	require.True(t, ok)
	descriptor := registered.CheckDescriptor
	require.Equal(t, ApplicabilityCodeAnchors, descriptor.Applicability)
	for _, name := range []string{CheckCodeFrontmatter, CheckCompanionDocs} {
		other, ok := lookupCheck(name)
		require.True(t, ok)
		require.Equal(t, ApplicabilityCode, other.Applicability, name)
	}

	tests := []struct {
		name  string
		facts VaultFeatureFacts
	}{
		{name: "code enabled without roots", facts: VaultFeatureFacts{CodeConfigured: true}},
		{name: "code not configured", facts: VaultFeatureFacts{CodeAnchorRootsConfigured: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			probe := &prerequisiteProbeSpy{}
			result := ResolveCheckApplicability(context.Background(), descriptor, SurfaceLocal, tt.facts, PrerequisiteProbes{Projection: probe, CodeIndex: probe})

			require.Equal(t, CheckOutcomeNotApplicable, result.Outcome)
			require.Equal(t, "no code folders for an enabled language; run rzm init --check to see the fix, or remove code folder limits or code.disabledLanguages in .rhizome/config.yml", result.Summary)
			require.Empty(t, result.PreparationCommand)
			require.Equal(t, EvidenceFeatureAbsent, result.Evidence[0].Code)
			require.Empty(t, probe.projectionCalls)
			require.Zero(t, probe.codeSnapshotCalls)
		})
	}

	t.Run("roots configured proceeds to the code-index probe", func(t *testing.T) {
		t.Parallel()
		probe := projectionProbe(AutoManagedProjectionSnapshot{Available: true, Refreshed: true}, nil)
		result := ResolveCheckApplicability(context.Background(), descriptor, SurfaceLocal,
			VaultFeatureFacts{CodeConfigured: true, CodeAnchorRootsConfigured: true}, PrerequisiteProbes{Projection: probe, CodeIndex: probe})

		require.Equal(t, CheckOutcomeBlocked, result.Outcome)
		require.Equal(t, "rzm index", result.PreparationCommand)
		require.Equal(t, 1, probe.codeSnapshotCalls)
	})
}

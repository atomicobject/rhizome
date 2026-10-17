package validationproduct

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/validationrun"
	"github.com/atomicobject/rhizome/pkg/noteformat/builtin"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/ontology"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/validate"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/stretchr/testify/require"
)

func TestProjectionAdapterRefreshesOnceAndMapsDomains(t *testing.T) {
	t.Parallel()

	refreshCalls := 0
	adapter := newProjectionAdapter(ValidationProjectionRequest{}, true, func(context.Context, ValidationProjectionRequest) (*ValidationProjectionResult, error) {
		refreshCalls++
		return &ValidationProjectionResult{
			Runtime: &ontology.Runtime{},
			Freshness: map[ProjectionDomain]ProjectionFreshness{
				ProjectionDomainMetadata: {State: ProjectionFresh},
				ProjectionDomainOntology: {State: ProjectionFresh},
			},
		}, nil
	})

	validationSnapshot, err := adapter.AutoManagedProjection(context.Background(), validate.CheckDescriptor{}, validate.ProjectionValidation)
	require.NoError(t, err)
	ontologySnapshot, err := adapter.AutoManagedProjection(context.Background(), validate.CheckDescriptor{}, validate.ProjectionOntology)
	require.NoError(t, err)
	require.Equal(t, 1, refreshCalls)
	require.Equal(t, validate.AutoManagedProjectionSnapshot{
		Domain: validate.ProjectionValidation, Available: true, Refreshed: true,
	}, validationSnapshot)
	require.Equal(t, validate.AutoManagedProjectionSnapshot{
		Domain: validate.ProjectionOntology, Available: true, Refreshed: true,
	}, ontologySnapshot)
}

func TestProjectionAdapterRequiresSourceMetadata(t *testing.T) {
	t.Parallel()

	adapter := newProjectionAdapter(ValidationProjectionRequest{}, false, func(context.Context, ValidationProjectionRequest) (*ValidationProjectionResult, error) {
		return &ValidationProjectionResult{Freshness: map[ProjectionDomain]ProjectionFreshness{
			ProjectionDomainMetadata: {State: ProjectionUnavailable},
		}}, nil
	})

	snapshot, err := adapter.AutoManagedProjection(context.Background(), validate.CheckDescriptor{}, validate.ProjectionValidation)
	require.NoError(t, err)
	require.False(t, snapshot.Available)
	require.True(t, snapshot.Refreshed)
}

func TestProjectionAdapterScratchDoesNotClaimPersistedCode(t *testing.T) {
	t.Parallel()

	adapter := newProjectionAdapter(ValidationProjectionRequest{
		Target: ValidationProjectionScratch,
	}, true, func(context.Context, ValidationProjectionRequest) (*ValidationProjectionResult, error) {
		return &ValidationProjectionResult{Runtime: &ontology.Runtime{}}, nil
	})

	snapshot, err := adapter.CodeIndexSnapshot(context.Background())
	require.NoError(t, err)
	require.Equal(t, validate.CodeIndexSnapshot{ConfiguredCapability: true}, snapshot)
}

func TestProjectionAdapterReadsPersistedCodeFactsFromLiveProjection(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := writeProjectionVault(t)
	request := ValidationProjectionRequest{
		VaultPath:    root,
		VaultDef:     obsidian.VaultDefinition{Path: root},
		NoteMetadata: testProjectionNoteMetadata(t),
		Target:       ValidationProjectionLive,
	}
	seed, err := RefreshValidationProjection(ctx, request)
	require.NoError(t, err)
	seedProjectionCodeFacts(t, seed.Runtime.Store)
	require.NoError(t, seed.Close())

	adapter := newProjectionAdapter(request, true, RefreshValidationProjection)
	snapshot, err := adapter.CodeIndexSnapshot(ctx)
	require.NoError(t, err)
	require.Equal(t, validate.CodeIndexSnapshot{
		ConfiguredCapability: true,
		StorePresent:         true,
		RowsPresent:          true,
		IndexerVersion:       "v9",
		ScopeHash:            "scope-9",
		IndexedFileCount:     1,
		IndexedAt:            time.Unix(1700000000, 0).UTC(),
	}, snapshot)
	require.NoError(t, adapter.Close())
}

func TestRunWithProjectionRefreshesOnceAcrossLocalCodeProbesAndSuite(t *testing.T) {
	t.Parallel()

	root := writeProjectionVault(t)
	require.NoError(t, repoconfig.SaveLocalConfig(root, repoconfig.LocalConfig{Code: repoconfig.LocalCodeConfig{Enabled: true}}))
	store, err := semdb.Open(filepath.Join(t.TempDir(), "prepared.db"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	seedProjectionCodeFacts(t, store)

	refreshCalls := 0
	result, err := runWithProjection(context.Background(), validationrun.ValidationRunRequest{
		Selectors: []string{"code-anchors"},
		Surface:   validate.SurfaceLocal,
		Features: validate.VaultFeatureFacts{
			CodeConfigured:             true,
			CodeAnchorRootsConfigured:  true,
			RequiredCodeIndexerVersion: "v9",
			RequiredCodeScopeHash:      "scope-9",
		},
	}, ValidationProjectionRequest{
		VaultPath:    root,
		VaultDef:     obsidian.VaultDefinition{Path: root},
		NoteMetadata: testProjectionNoteMetadata(t),
		Target:       ValidationProjectionLive,
	}, validate.ScanOptions{MaxIssues: 20}, func(context.Context, ValidationProjectionRequest) (*ValidationProjectionResult, error) {
		refreshCalls++
		return &ValidationProjectionResult{
			Runtime: &ontology.Runtime{Store: store},
			Freshness: map[ProjectionDomain]ProjectionFreshness{
				ProjectionDomainMetadata: {State: ProjectionFresh},
			},
		}, nil
	})

	require.NoError(t, err)
	require.Equal(t, 1, refreshCalls)
	require.Equal(t, validate.CheckOutcomeCompleted, result.Outcomes[0].Outcome)
	require.Len(t, result.Checks, 1)
}

func TestRunWithProjectionRejectsMissingProjectionIndexerBeforeRefresh(t *testing.T) {
	root := writeProjectionVault(t)
	refreshCalls := 0

	_, err := runWithProjection(context.Background(), validationrun.ValidationRunRequest{
		Selectors: []string{"ontology"},
		Surface:   validate.SurfaceLocal,
		Features:  validate.VaultFeatureFacts{OntologyConfigured: true},
	}, ValidationProjectionRequest{
		VaultPath: root,
		VaultDef:  obsidian.VaultDefinition{Path: root},
		Target:    ValidationProjectionLive,
	}, validate.ScanOptions{}, func(context.Context, ValidationProjectionRequest) (*ValidationProjectionResult, error) {
		refreshCalls++
		return nil, nil
	})

	require.ErrorContains(t, err, "validation projection note metadata indexer")
	require.Zero(t, refreshCalls)
	require.NoFileExists(t, filepath.Join(root, ".rhizome", "db.sqlite"))
}

func TestRunWithProjectionRejectsMissingRunContextIndexerBeforeRefresh(t *testing.T) {
	root := writeProjectionVault(t)
	refreshCalls := 0
	runContext := &validate.RunContext{
		VaultDef:   obsidian.VaultDefinition{Path: root},
		VaultPath:  root,
		NoteReader: &obsidian.Note{},
	}

	_, err := runWithProjection(context.Background(), validationrun.ValidationRunRequest{
		Selectors: []string{"ontology"},
		Surface:   validate.SurfaceLocal,
		Features:  validate.VaultFeatureFacts{OntologyConfigured: true},
	}, ValidationProjectionRequest{
		VaultPath:    root,
		VaultDef:     obsidian.VaultDefinition{Path: root},
		NoteMetadata: testProjectionNoteMetadata(t),
		Target:       ValidationProjectionLive,
	}, validate.ScanOptions{RunContext: runContext}, func(context.Context, ValidationProjectionRequest) (*ValidationProjectionResult, error) {
		refreshCalls++
		return nil, nil
	})

	require.ErrorContains(t, err, "validation RunContext note metadata indexer")
	require.Zero(t, refreshCalls)
	require.NoFileExists(t, filepath.Join(root, ".rhizome", "db.sqlite"))
}

func TestRunWithProjectionPendingJournalBlocksBeforeProjectionMutation(t *testing.T) {
	t.Parallel()

	root := writeProjectionVault(t)
	writePendingProjectionRepairJournal(t, root, "interrupted-repair", []string{validate.CheckOntology})

	refreshCalls := 0
	result, err := runWithProjection(context.Background(), validationrun.ValidationRunRequest{
		Selectors:    []string{"ontology"},
		ApplyCommand: "rzm validate fix ontology --apply --vault test --scope-note 'Notes/A B.md'",
		Surface:      validate.SurfaceLocal,
		Features:     validate.VaultFeatureFacts{OntologyConfigured: true},
	}, ValidationProjectionRequest{
		VaultPath:    root,
		VaultDef:     obsidian.VaultDefinition{Name: "test", Path: root},
		NoteMetadata: testProjectionNoteMetadata(t),
		Target:       ValidationProjectionLive,
	}, validate.ScanOptions{}, func(context.Context, ValidationProjectionRequest) (*ValidationProjectionResult, error) {
		refreshCalls++
		return nil, nil
	})

	require.NoError(t, err)
	require.Zero(t, refreshCalls, "journal recovery barrier must precede every projection probe or refresh")
	require.Empty(t, result.Checks)
	require.Nil(t, result.FixPlan)
	require.Equal(t, validationrun.ValidationExitFailure, result.ExitCode())
	require.Len(t, result.Outcomes, 1)
	require.Equal(t, validate.CheckOutcomeBlocked, result.Outcomes[0].Outcome)
	require.Equal(t, "rzm validate fix ontology --apply --vault test --scope-note 'Notes/A B.md'", result.Outcomes[0].PreparationCommand)
	require.Len(t, result.RepairJournals, 1)
	require.Equal(t, "interrupted-repair", result.RepairJournals[0].TransactionID)
	require.NotNil(t, result.NextActions)
	require.Len(t, result.NextActions.Actions, 1)
	require.Equal(t, "repair_recovery", result.NextActions.Actions[0].Category)
	require.Equal(t, result.Outcomes[0].PreparationCommand, result.NextActions.Actions[0].Command)
}

func TestRunWithProjectionPendingJournalPreservesCILocalApplyCommand(t *testing.T) {
	t.Parallel()

	root := writeProjectionVault(t)
	writePendingProjectionRepairJournal(t, root, "ci-interrupted-repair", []string{validate.CheckBrokenLinks})

	refreshCalls := 0
	result, err := runWithProjection(context.Background(), validationrun.ValidationRunRequest{
		Selectors:    []string{"all"},
		ApplyCommand: "rzm validate fix all --apply --vault docs --scope-target Target.md",
		Surface:      validate.SurfaceCI,
	}, ValidationProjectionRequest{
		VaultPath:    root,
		VaultDef:     obsidian.VaultDefinition{Name: "docs", Path: root},
		NoteMetadata: testProjectionNoteMetadata(t),
		Target:       ValidationProjectionScratch,
	}, validate.ScanOptions{}, func(context.Context, ValidationProjectionRequest) (*ValidationProjectionResult, error) {
		refreshCalls++
		return nil, nil
	})

	require.NoError(t, err)
	require.Zero(t, refreshCalls)
	require.Equal(t, validationrun.ValidationExitFailure, result.ExitCode())
	require.NotEmpty(t, result.Outcomes)
	for _, outcome := range result.Outcomes {
		require.Equal(t, validate.CheckOutcomeBlocked, outcome.Outcome)
		require.Equal(t, "rzm validate fix all --apply --vault docs --scope-target Target.md", outcome.PreparationCommand)
	}
	require.Equal(t, "rzm validate fix all --apply --vault docs --scope-target Target.md", result.NextActions.Actions[0].Command)
}

// This fake refresher invokes BeforeMutation itself, so the test proves only
// that the product installs the journal barrier and maps its blocked result.
// Lock-before-store ordering belongs to the indexing projection tests
// TestRefreshValidationProjectionRunsPreMutationCheckBeforeOpeningStore and
// TestValidationProjectionLaneRetriesPreemptionAndReleasesLock.
func TestRunWithProjectionInstallsPreMutationJournalBarrier(t *testing.T) {
	t.Parallel()

	root := writeProjectionVault(t)
	refreshCalls := 0
	result, err := runWithProjection(context.Background(), validationrun.ValidationRunRequest{
		Selectors: []string{"ontology"},
		Surface:   validate.SurfaceLocal,
		Features:  validate.VaultFeatureFacts{OntologyConfigured: true},
	}, ValidationProjectionRequest{
		VaultPath:    root,
		VaultDef:     obsidian.VaultDefinition{Name: "test", Path: root},
		NoteMetadata: testProjectionNoteMetadata(t),
		Target:       ValidationProjectionLive,
	}, validate.ScanOptions{}, func(ctx context.Context, request ValidationProjectionRequest) (*ValidationProjectionResult, error) {
		writePendingProjectionRepairJournal(t, root, "concurrent-repair", []string{validate.CheckOntology})
		require.NotNil(t, request.BeforeMutation)
		if err := request.BeforeMutation(ctx); err != nil {
			return nil, err
		}
		refreshCalls++
		return nil, nil
	})

	require.NoError(t, err)
	require.Zero(t, refreshCalls, "the installed barrier must stop the refresher before it mutates")
	require.Equal(t, validationrun.ValidationExitFailure, result.ExitCode())
	require.Len(t, result.Outcomes, 1)
	require.Equal(t, validate.CheckOutcomeBlocked, result.Outcomes[0].Outcome)
	require.Equal(t, "rzm agent validate fix ontology --apply --vault test", result.Outcomes[0].PreparationCommand)
	require.Len(t, result.RepairJournals, 1)
	require.Equal(t, "concurrent-repair", result.RepairJournals[0].TransactionID)
}

func TestRunWithProjectionJournalPublishedAfterRefreshBlocksBeforeSuite(t *testing.T) {
	t.Parallel()

	root := writeProjectionVault(t)
	exactCommand := "rzm validate fix ontology --apply --vault test --scope-note 'Notes/A B.md'"
	refreshCalls := 0
	result, err := runWithProjection(context.Background(), validationrun.ValidationRunRequest{
		Selectors:    []string{"ontology"},
		ApplyCommand: exactCommand,
		Surface:      validate.SurfaceLocal,
		Features:     validate.VaultFeatureFacts{OntologyConfigured: true},
	}, ValidationProjectionRequest{
		VaultPath:    root,
		VaultDef:     obsidian.VaultDefinition{Name: "test", Path: root},
		NoteMetadata: testProjectionNoteMetadata(t),
		Target:       ValidationProjectionLive,
	}, validate.ScanOptions{}, func(ctx context.Context, request ValidationProjectionRequest) (*ValidationProjectionResult, error) {
		require.NotNil(t, request.BeforeMutation)
		require.NoError(t, request.BeforeMutation(ctx))
		refreshCalls++
		writePendingProjectionRepairJournal(t, root, "post-refresh-repair", []string{validate.CheckOntology})
		return &ValidationProjectionResult{
			Runtime: &ontology.Runtime{},
			Freshness: map[ProjectionDomain]ProjectionFreshness{
				ProjectionDomainMetadata: {State: ProjectionFresh},
				ProjectionDomainOntology: {State: ProjectionFresh},
			},
		}, nil
	})

	require.NoError(t, err)
	require.Equal(t, 1, refreshCalls)
	require.Empty(t, result.Checks)
	require.Equal(t, validationrun.ValidationExitFailure, result.ExitCode())
	require.Equal(t, validate.CheckOutcomeBlocked, result.Outcomes[0].Outcome)
	require.Equal(t, exactCommand, result.Outcomes[0].PreparationCommand)
	require.Equal(t, exactCommand, result.NextActions.Actions[0].Command)
	require.Equal(t, "post-refresh-repair", result.RepairJournals[0].TransactionID)
}

func TestRunWithProjectionLocalCodeUsableButScratchCITruthfullyBlocked(t *testing.T) {
	t.Parallel()

	refreshCalls := 0
	result, err := runWithProjection(context.Background(), validationrun.ValidationRunRequest{
		Selectors: []string{"code-anchors"},
		Surface:   validate.SurfaceCI,
		Features:  validate.VaultFeatureFacts{CodeConfigured: true, CodeAnchorRootsConfigured: true},
	}, ValidationProjectionRequest{NoteMetadata: testProjectionNoteMetadata(t), Target: ValidationProjectionScratch}, validate.ScanOptions{}, func(context.Context, ValidationProjectionRequest) (*ValidationProjectionResult, error) {
		refreshCalls++
		return nil, nil
	})

	require.NoError(t, err)
	require.Zero(t, refreshCalls, "surface blocking must precede projection work")
	require.Equal(t, validate.CheckOutcomeBlocked, result.Outcomes[0].Outcome)
	require.Equal(t, "rzm validate code-anchors", result.Outcomes[0].PreparationCommand)
	require.Contains(t, result.Outcomes[0].Summary, "unavailable in isolated scratch CI")
	require.Empty(t, result.Checks)
}

func TestProjectionAdapterClosesScratchWithoutCodeOrProviderWork(t *testing.T) {
	t.Parallel()

	root := writeProjectionVault(t)
	adapter := newProjectionAdapter(ValidationProjectionRequest{
		VaultPath:    root,
		VaultDef:     obsidian.VaultDefinition{Path: root},
		NoteMetadata: testProjectionNoteMetadata(t),
		Target:       ValidationProjectionScratch,
	}, false, RefreshValidationProjection)

	snapshot, err := adapter.AutoManagedProjection(context.Background(), validate.CheckDescriptor{}, validate.ProjectionValidation)
	require.NoError(t, err)
	require.True(t, snapshot.Available)
	require.NotNil(t, adapter.result)
	require.Zero(t, adapter.result.Counters.ProjectCodeEnumerations)
	require.Zero(t, adapter.result.Counters.ProviderCalls)
	scratchDir := filepath.Dir(adapter.result.IndexPath)

	require.NoError(t, adapter.Close())
	require.NoError(t, adapter.Close())
	_, err = os.Stat(scratchDir)
	require.True(t, os.IsNotExist(err))
}

func TestRunWithProjectionUsesScratchPreparedRuntime(t *testing.T) {
	t.Parallel()

	root := writeProjectionVault(t)
	var scratchDir string
	result, err := runWithProjection(context.Background(), validationrun.ValidationRunRequest{
		Selectors: []string{"ontology"},
		Surface:   validate.SurfaceCI,
		Features:  validate.VaultFeatureFacts{OntologyConfigured: true},
	}, ValidationProjectionRequest{
		VaultPath:    root,
		VaultDef:     obsidian.VaultDefinition{Name: "test", Path: root, Links: obsidian.LinkTypeBoth},
		NoteMetadata: testProjectionNoteMetadata(t),
	}, validate.ScanOptions{MaxIssues: 20}, func(ctx context.Context, request ValidationProjectionRequest) (*ValidationProjectionResult, error) {
		require.Equal(t, ValidationProjectionScratch, request.Target)
		projection, err := RefreshValidationProjection(ctx, request)
		if projection != nil {
			scratchDir = filepath.Dir(projection.IndexPath)
		}
		return projection, err
	})

	require.NoError(t, err)
	require.Equal(t, validate.CheckOutcomeCompleted, result.Outcomes[0].Outcome)
	require.Len(t, result.Checks, 1)
	_, err = os.Stat(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.True(t, os.IsNotExist(err), "prepared scratch validation must not open the live index")
	require.NotEmpty(t, scratchDir)
	_, err = os.Stat(scratchDir)
	require.True(t, os.IsNotExist(err), "RunWithProjection must close and remove its scratch projection")
}

func TestRunWithProjectionScratchLeavesPreexistingLiveDBUnchanged(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := writeProjectionVault(t)
	live, err := RefreshValidationProjection(ctx, ValidationProjectionRequest{
		VaultPath:    root,
		VaultDef:     obsidian.VaultDefinition{Path: root},
		NoteMetadata: testProjectionNoteMetadata(t),
		Target:       ValidationProjectionLive,
	})
	require.NoError(t, err)
	seedProjectionCodeFacts(t, live.Runtime.Store)
	livePath := live.IndexPath
	require.NoError(t, live.Close())
	before, err := os.ReadFile(livePath)
	require.NoError(t, err)
	beforeHash := sha256.Sum256(before)

	result, err := RunWithProjection(ctx, validationrun.ValidationRunRequest{
		Selectors: []string{"ontology"},
		Surface:   validate.SurfaceCI,
		Features:  validate.VaultFeatureFacts{OntologyConfigured: true},
	}, ValidationProjectionRequest{
		VaultPath:    root,
		VaultDef:     obsidian.VaultDefinition{Path: root},
		NoteMetadata: testProjectionNoteMetadata(t),
	}, validate.ScanOptions{})
	require.NoError(t, err)
	require.Equal(t, validate.CheckOutcomeCompleted, result.Outcomes[0].Outcome)

	after, err := os.ReadFile(livePath)
	require.NoError(t, err)
	require.Equal(t, beforeHash, sha256.Sum256(after))
}

func TestRunWithProjectionRejectsUnsafeTarget(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		surface validate.ExecutionSurface
		target  ValidationProjectionTarget
		options validate.ScanOptions
		wantErr string
	}{
		{name: "CI cannot use live", surface: validate.SurfaceCI, target: ValidationProjectionLive, wantErr: "CI validation requires a scratch projection"},
		{name: "local cannot use scratch", surface: validate.SurfaceLocal, target: ValidationProjectionScratch, wantErr: "local validation requires the live projection"},
		{name: "agent cannot use scratch", surface: validate.SurfaceAgent, target: ValidationProjectionScratch, wantErr: "agent validation requires the live projection"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := RunWithProjection(context.Background(), validationrun.ValidationRunRequest{Surface: tt.surface}, ValidationProjectionRequest{Target: tt.target}, tt.options)
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func writeProjectionVault(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome", "ontology"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "notes"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "ontology", "schema.graphql"), []byte(`
type ExampleNote @node(paths: ["notes/*.md"]) {
  name: String!
}
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "one.md"), []byte("---\ntype: ExampleNote\nname: One\n---\n# One\n"), 0o644))
	return root
}

func testProjectionNoteMetadata(t *testing.T) notemeta.Indexer {
	t.Helper()
	runtime, err := builtin.NewRuntime()
	require.NoError(t, err)
	indexer, err := notemeta.NewIndexer(runtime)
	require.NoError(t, err)
	return indexer
}

func writePendingProjectionRepairJournal(t *testing.T, root, transactionID string, checks []string) {
	t.Helper()
	digest := sha256.Sum256([]byte(transactionID))
	dir := filepath.Join(root, ".rhizome", "repair-journal", "v1", hex.EncodeToString(digest[:]))
	require.NoError(t, os.MkdirAll(dir, 0o755))
	manifest, err := json.Marshal(map[string]any{
		"version":         1,
		"transactionId":   transactionID,
		"planFingerprint": "sha256:pending",
		"checks":          checks,
		"entries":         []any{},
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "manifest.json"), append(manifest, '\n'), 0o600))
}

func seedProjectionCodeFacts(t *testing.T, store *semdb.Store) {
	t.Helper()
	ctx := context.Background()
	db := store.DB()
	_, err := db.ExecContext(ctx, `INSERT INTO files(path, lang, hash, indexer_version, parse_status, call_edges_stale, mtime) VALUES ('code/example.go', 'go', 'hash', 'v9', 'ok', 0, 1700000000)`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO intel_code_anchors(id, anchor_id, lang, kind, path, symbol, fqn, start_byte, end_byte, start_line, end_line, fingerprint, updated_at) VALUES (41, 'anchor-41', 'go', 'function', 'code/example.go', 'Example', 'example.Example', 0, 7, 1, 1, 'fp', 1)`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT OR REPLACE INTO index_metadata(key, value) VALUES (?, 'v9'), (?, 'scope-9')`, semdb.MetaKeyIndexerVersion, semdb.MetaKeyScopeConfigHash)
	require.NoError(t, err)
}

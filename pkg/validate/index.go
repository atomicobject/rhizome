package validate

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// IndexValidationSnapshot runs the configured default suite and returns the
// complete sanitized cache contract. Private repair authority and executable
// operation payloads remain only in the in-memory Result.
func IndexValidationSnapshot(ctx context.Context, noteMetadata notemeta.Indexer, vaultDef obsidian.VaultDefinition, runtime *ontology.Runtime, runtimeErr error, maxIssues int) (semdb.ValidationSnapshot, int64, error) {
	started := time.Now()
	result, durationMs, err := runIndexedValidation(ctx, noteMetadata, vaultDef, runtime, runtimeErr, maxIssues)
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
	completion := semdb.ValidationCompletionComplete
	staleReason := ""
	result, checkOutcomes, complete := indexedDiagnosticOutcomes(result)
	if !complete {
		completion = semdb.ValidationCompletionIncomplete
		staleReason = "selected checks did not produce complete outcomes"
	}
	var notePaths []string
	if runtime != nil && runtime.Store != nil {
		notePaths, err = runtime.Store.NotePaths(ctx)
		if err != nil {
			return semdb.ValidationSnapshot{}, durationMs, fmt.Errorf("read indexed note inventory: %w", err)
		}
	}
	snapshot, err := BuildDiagnosticSnapshot(result, DiagnosticSnapshotContext{
		VaultIdentity: vaultIdentity, Scope: "default", SchemaIdentity: schemaIdentity,
		ConfigIdentity: fmt.Sprintf("sha256:%x", configDigest[:]), InputRevision: inputRevision,
		StartedAt: started.Unix(), FinishedAt: time.Now().Unix(), DurationMs: durationMs,
		Completion: completion, StaleReason: staleReason,
		CheckOutcomes: checkOutcomes,
		NotePaths:     notePaths,
	})
	return snapshot, durationMs, err
}

func indexedDiagnosticOutcomes(result Result) (Result, []DiagnosticCheckOutcome, bool) {
	byCheck := make(map[string]CheckResult, len(result.Checks))
	for _, check := range result.Checks {
		canonical := diagnosticCanonicalCheck(check.Name)
		check.Name = canonical
		byCheck[canonical] = check
	}
	blockedByJournal := len(result.RepairJournals) > 0
	outcomes := make([]DiagnosticCheckOutcome, 0, len(result.SelectedChecks))
	executed := make([]CheckResult, 0, len(result.Checks))
	complete := true
	for _, selected := range result.SelectedChecks {
		canonical := diagnosticCanonicalCheck(selected)
		check, found := byCheck[canonical]
		outcome := DiagnosticCheckOutcome{Check: canonical}
		switch {
		case blockedByJournal:
			outcome.Outcome = CheckOutcomeBlocked
			outcome.Summary = "pending repair journal blocks validation"
			complete = false
		case !found:
			outcome.Outcome = CheckOutcomeBlocked
			outcome.Summary = "selected check did not produce an outcome"
			complete = false
		case check.Skipped:
			outcome.Outcome = CheckOutcomeNotApplicable
			outcome.Summary = check.Summary
		case found:
			outcome.Outcome = CheckOutcomeCompleted
			executed = append(executed, check)
		}
		outcomes = append(outcomes, outcome)
	}
	result.Checks = executed
	result.SelectedChecks = diagnosticCanonicalChecks(result.SelectedChecks)
	return result, outcomes, complete
}

func runIndexedValidation(ctx context.Context, noteMetadata notemeta.Indexer, vaultDef obsidian.VaultDefinition, runtime *ontology.Runtime, runtimeErr error, maxIssues int) (Result, int64, error) {
	if err := noteMetadata.Validate(); err != nil {
		return Result{}, 0, fmt.Errorf("note metadata indexer: %w", err)
	}
	if maxIssues <= 0 {
		maxIssues = 500
	}
	start := time.Now()
	checks, err := configuredDefaultValidationChecks(vaultDef)
	if err != nil {
		return Result{}, time.Since(start).Milliseconds(), err
	}
	runCtx := RunContext{
		VaultDef:     vaultDef,
		VaultPath:    vaultDef.BasePath(),
		VaultMgr:     &fixedVaultMgr{def: vaultDef},
		NoteReader:   &obsidian.Note{},
		NoteMetadata: noteMetadata,
		MaxIssues:    maxIssues,
	}
	result, _, err := runSuiteOnce(ctx, Options{
		Checks:     checks,
		MaxIssues:  maxIssues,
		RunContext: &runCtx,
	}, runtime, runtimeErr, runtime != nil || runtimeErr != nil)
	if err != nil {
		return Result{}, time.Since(start).Milliseconds(), err
	}
	return result, time.Since(start).Milliseconds(), nil
}

func configuredDefaultValidationChecks(vaultDef obsidian.VaultDefinition) ([]string, error) {
	vaultPath := strings.TrimSpace(vaultDef.BasePath())
	if vaultPath == "" {
		return nil, fmt.Errorf("validation vault root is required")
	}
	local, err := obsidian.LoadLocalConfig(vaultPath)
	if errors.Is(err, obsidian.ErrNoLocalConfig) {
		local = &obsidian.LocalConfig{}
	} else if err != nil {
		return nil, fmt.Errorf("load validation config from %s: %w", vaultPath, err)
	}
	selection, err := ResolveSelection(nil, validationSuiteConfigFromLocal(local.Validation))
	if err != nil {
		return nil, fmt.Errorf("resolve configured default validation suite: %w", err)
	}
	return slices.Clone(selection.Checks), nil
}

func validationSuiteConfigFromLocal(local obsidian.LocalValidationConfig) SuiteConfig {
	return SuiteConfig{
		Default: SuiteOverlay{
			Add:  slices.Clone(local.Default.Add),
			Skip: slices.Clone(local.Default.Skip),
		},
		All: SuiteOverlay{
			Add:  slices.Clone(local.All.Add),
			Skip: slices.Clone(local.All.Skip),
		},
	}
}

// fixedVaultMgr wraps a VaultDefinition to satisfy obsidian.VaultManager.
type fixedVaultMgr struct {
	def obsidian.VaultDefinition
}

func (v *fixedVaultMgr) DefaultName() (string, error)                  { return v.def.Name, nil }
func (v *fixedVaultMgr) SetDefaultName(name string) error              { v.def.Name = name; return nil }
func (v *fixedVaultMgr) Path() (string, error)                         { return v.def.BasePath(), nil }
func (v *fixedVaultMgr) Definition() (obsidian.VaultDefinition, error) { return v.def, nil }

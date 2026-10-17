package validate

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/atomicobject/rhizome/pkg/ontology"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"github.com/atomicobject/rhizome/pkg/paths"
)

// repairPreparation retains hashes rather than the scan's whole source bytes.
type repairPreparation struct {
	root               string
	sources            map[string]string
	completeInventory  bool
	schemaHash         string
	fingerprint        string
	summaryFingerprint string
}

func captureRepairPreparation(ctx context.Context, runCtx RunContext, runtime *ontology.Runtime, checks []CheckResult) (*repairPreparation, error) {
	vault, err := paths.NewVaultPaths(runCtx.VaultPath)
	if err != nil {
		return nil, err
	}
	preparation := &repairPreparation{root: vault.Root(), sources: make(map[string]string)}
	if runtime != nil && runtime.Schema != nil {
		preparation.schemaHash = runtime.Schema.Hash
	}
	if snapshot := runCtx.sourceSnapshot; snapshot != nil {
		preparation.completeInventory = snapshot.completeInventory
		for _, source := range snapshot.sources {
			preparation.sources[source.Path.String()] = "sha256:" + source.ContentHash
		}
	} else {
		// Store-only checks need source authority only for their proposed edits.
		for _, check := range checks {
			for _, action := range check.Fixes {
				for _, edit := range action.Edits {
					path := edit.NotePath
					if path == "" {
						path = edit.SourcePath
					}
					if path == "" || preparation.sources[path] != "" {
						continue
					}
					if _, err := repairAbsPath(runCtx, path); err != nil {
						return nil, err
					}
					if err := ctx.Err(); err != nil {
						return nil, err
					}
					content, err := runCtx.NoteReader.GetContents(runCtx.VaultDef, path)
					if err != nil {
						return nil, err
					}
					preparation.sources[path] = SourceHash([]byte(content))
				}
			}
		}
	}
	witness, err := json.Marshal(struct {
		Root              string
		Sources           map[string]string
		Schema            string
		CompleteInventory bool
	}{preparation.root, preparation.sources, preparation.schemaHash, preparation.completeInventory})
	if err != nil {
		return nil, err
	}
	preparation.fingerprint = SourceHash(witness)
	return preparation, nil
}

// PrepareRepair turns an in-process read result into a source-backed preview
// or apply plan. Changed scan inputs require a fresh validation run.
func PrepareRepair(ctx context.Context, scanned Result, runCtx RunContext) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if scanned.FixPlan == nil || !scanned.FixPlan.PreparationRequired {
		return scanned, nil
	}
	preparation := scanned.repairPreparation
	if preparation == nil {
		return Result{}, fmt.Errorf("repair preparation requires in-process validation authority; rerun validation")
	}
	fingerprint, err := RepairPlanFingerprint(*scanned.FixPlan)
	if err != nil {
		return Result{}, err
	}
	if fingerprint != preparation.summaryFingerprint || scanned.FixPlan.Fingerprint != fingerprint {
		return Result{}, fmt.Errorf("validation action summary changed; rerun validation")
	}
	if err := validateRepairSessionRoots(runCtx); err != nil {
		return Result{}, err
	}
	vault, err := paths.NewVaultPaths(runCtx.VaultPath)
	if err != nil {
		return Result{}, fmt.Errorf("resolve repair preparation vault: %w", err)
	}
	if !paths.CaseEqual(vault.Root(), preparation.root) {
		return Result{}, fmt.Errorf("repair preparation vault differs from captured validation authority")
	}
	if err := preparation.validateSchema(); err != nil {
		return Result{}, err
	}
	snapshot, err := captureValidationRunSnapshot(ctx, runCtx)
	if err != nil {
		return Result{}, err
	}
	if err := preparation.validateSnapshot(snapshot); err != nil {
		return Result{}, err
	}
	planningCtx := runCtx
	planningCtx.sourceSnapshot = snapshot
	planningCtx.NoteReader = snapshot.reader(runCtx.NoteReader)
	checks, err := prepareIdentifierRepairChecks(ctx, planningCtx, scanned.Checks)
	if err != nil {
		return Result{}, err
	}
	plan, err := BuildRepairPlan(ctx, planningCtx, checks)
	if err != nil {
		return Result{}, fmt.Errorf("build repair plan: %w", err)
	}
	if plan != nil {
		for _, operation := range plan.Operations {
			if hash, captured := preparation.sources[operation.Path]; !captured || hash != operation.SourceHash {
				return Result{}, fmt.Errorf("repair source %q differs from captured validation authority; rerun validation", operation.Path)
			}
		}
	}
	// Planning adapters may read the live filesystem; reject changes during preparation.
	if err := preparation.validateCurrentSources(ctx, runCtx); err != nil {
		return Result{}, err
	}
	if err := preparation.validateSchema(); err != nil {
		return Result{}, err
	}
	scanned.Checks = checks
	scanned.FixPlan = plan
	scanned.repairPreparation = nil
	for _, check := range checks {
		if check.identifierReconciliation != nil {
			scanned.IdentifierReconciliation = check.identifierReconciliation
		}
	}
	scanned.NextActions = BuildNextActions(scanned)
	return scanned, nil
}

func (p *repairPreparation) validateSchema() error {
	if p.schemaHash == "" {
		return nil
	}
	schema, err := ontoschema.LoadSchema(p.root)
	if err != nil {
		return err
	}
	if schema == nil || schema.Hash != p.schemaHash {
		return fmt.Errorf("ontology schema changed after validation; rerun validation")
	}
	return nil
}

func (p *repairPreparation) validateSnapshot(snapshot *validationRunSnapshot) error {
	if p.completeInventory && len(snapshot.sources) != len(p.sources) {
		return fmt.Errorf("source inventory changed after validation; rerun validation")
	}
	for path, hash := range p.sources {
		source, exists := snapshot.byPath[path]
		if !exists || "sha256:"+source.ContentHash != hash {
			return fmt.Errorf("source %q changed after validation; rerun validation", path)
		}
	}
	return nil
}

func (p *repairPreparation) validateCurrentSources(ctx context.Context, runCtx RunContext) error {
	snapshot, err := captureValidationRunSnapshot(ctx, runCtx)
	if err != nil {
		return err
	}
	return p.validateSnapshot(snapshot)
}

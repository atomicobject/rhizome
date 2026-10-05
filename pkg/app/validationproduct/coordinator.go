package validationproduct

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/app/validationrun"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/validate"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

var ErrRefreshSuperseded = errors.New("validation refresh superseded")

type RefreshRun func(context.Context) (AuthoritativeRun, error)

type RefreshCoordinatorOptions struct {
	Context      context.Context
	Store        *semdb.Store
	VaultDef     obsidian.VaultDefinition
	NoteMetadata notemeta.Indexer
	RepairStore  *validate.RepairReviewStore
	Run          RefreshRun
	MaxIssues    int
	ApplyOptions validate.Options
}

type validationAuthority struct {
	generation int64
	snapshot   semdb.ValidationSnapshot
	run        AuthoritativeRun
}

// RefreshCoordinator serializes publication identity while allowing a newer
// request to cancel and supersede an older validation run.
type RefreshCoordinator struct {
	ctx          context.Context
	store        *semdb.Store
	vaultDef     obsidian.VaultDefinition
	repairStore  *validate.RepairReviewStore
	run          RefreshRun
	applyOptions validate.Options

	startMu sync.Mutex
	cancel  context.CancelFunc
	// generationMu prevents a repair apply from passing its generation check
	// while a refresh advances the durable current generation.
	generationMu sync.Mutex

	authorityMu  sync.RWMutex
	authority    *validationAuthority
	afterPublish func(int64)
}

func NewRefreshCoordinator(opts RefreshCoordinatorOptions) *RefreshCoordinator {
	ctx := opts.Context
	if ctx == nil {
		ctx = context.Background()
	}
	repairStore := opts.RepairStore
	if repairStore == nil {
		repairStore = validate.NewRepairReviewStore(validate.RepairReviewStoreOptions{})
	}
	run := opts.Run
	if run == nil {
		run = func(ctx context.Context) (AuthoritativeRun, error) {
			return RunLive(ctx, opts.NoteMetadata, opts.VaultDef, opts.MaxIssues)
		}
	}
	return &RefreshCoordinator{
		ctx: ctx, store: opts.Store, vaultDef: opts.VaultDef, repairStore: repairStore, run: run,
		applyOptions: opts.ApplyOptions,
	}
}

// Refresh publishes one generation. Only the generation accepted by the
// durable store becomes repair authority.
func (c *RefreshCoordinator) Refresh(ctx context.Context) error {
	if c == nil || c.store == nil || c.run == nil {
		return fmt.Errorf("validation refresh coordinator is unavailable")
	}
	if ctx == nil {
		ctx = c.ctx
	}
	c.generationMu.Lock()
	c.startMu.Lock()
	runCtx, cancel := context.WithCancel(ctx)
	generation, err := c.store.SetValidationRunning(ctx)
	if err == nil {
		// Advance the durable generation before cancellation lets the old run
		// finish. Failed admission must leave that run in control.
		if c.cancel != nil {
			c.cancel()
		}
		c.cancel = cancel
	}
	c.startMu.Unlock()
	c.generationMu.Unlock()
	if err != nil {
		cancel()
		return err
	}
	defer cancel()

	started := time.Now()
	run, runErr := c.run(runCtx)
	duration := time.Since(started).Milliseconds()
	if runErr == nil {
		runErr = runCtx.Err()
	}
	if runErr != nil {
		return c.finishError(ctx, generation, duration, runErr)
	}

	snapshot, err := c.buildSnapshot(context.WithoutCancel(ctx), generation, started, duration, run)
	if err != nil {
		return c.finishError(ctx, generation, duration, err)
	}
	published, err := c.store.PublishValidationSnapshot(context.WithoutCancel(ctx), snapshot)
	if err != nil {
		return c.finishError(ctx, generation, duration, err)
	}
	if !published {
		return ErrRefreshSuperseded
	}
	if c.afterPublish != nil {
		c.afterPublish(generation)
	}

	// Publication and authority installation are distinct stores. Recheck the
	// durable winner while holding the same admission lock used by refresh
	// starts and repair applies so a delayed older publisher cannot replace a
	// newer in-memory authority.
	c.generationMu.Lock()
	defer c.generationMu.Unlock()
	state, err := c.store.GetValidationState(context.WithoutCancel(ctx))
	if err != nil {
		return err
	}
	if state.Status != semdb.ValidationStatusOK || state.Generation != generation || state.PublishedGeneration != generation {
		return ErrRefreshSuperseded
	}
	c.authorityMu.Lock()
	c.authority = &validationAuthority{generation: generation, snapshot: snapshot, run: run}
	c.authorityMu.Unlock()
	c.repairStore.MarkVaultStale(snapshot.VaultIdentity, generation, "a newer validation generation was published")
	return nil
}

func (c *RefreshCoordinator) finishError(ctx context.Context, generation, duration int64, runErr error) error {
	updated, stateErr := c.store.SetValidationError(context.WithoutCancel(ctx), generation, runErr.Error(), duration)
	if stateErr != nil {
		return errors.Join(runErr, stateErr)
	}
	if !updated {
		return ErrRefreshSuperseded
	}
	return runErr
}

func (c *RefreshCoordinator) buildSnapshot(ctx context.Context, generation int64, started time.Time, duration int64, run AuthoritativeRun) (semdb.ValidationSnapshot, error) {
	completion := semdb.ValidationCompletionComplete
	staleReason := ""
	for _, outcome := range run.Result.Outcomes {
		if outcome.Outcome == validate.CheckOutcomeBlocked {
			completion = semdb.ValidationCompletionIncomplete
			staleReason = "selected checks were blocked"
			break
		}
	}
	configBytes, _ := json.Marshal(run.Result.EffectiveChecks)
	configHash := sha256.Sum256(configBytes)
	vaultIdentity := strings.TrimSpace(c.vaultDef.Name)
	if vaultIdentity == "" {
		vaultIdentity = strings.TrimSpace(c.vaultDef.BasePath())
	}
	schemaIdentity, inputRevision := "", ""
	if state, err := c.store.GetOntologySchemaState(ctx); err == nil {
		schemaIdentity = state.SchemaHash
	}
	if state, err := c.store.GetNoteMetadataState(ctx); err == nil {
		inputRevision = state.NotesHash
	}
	return validationrun.BuildDiagnosticSnapshot(run.Result, validate.DiagnosticSnapshotContext{
		VaultIdentity: vaultIdentity, Scope: "default", Generation: generation,
		SchemaIdentity: schemaIdentity, ConfigIdentity: fmt.Sprintf("sha256:%x", configHash[:]),
		InputRevision: inputRevision, StartedAt: started.Unix(), FinishedAt: time.Now().Unix(),
		DurationMs: duration, Completion: completion, StaleReason: staleReason, NotePaths: run.NotePaths,
	})
}

// CreateRepairReview stages actions only from the latest successfully
// published in-process result.
func (c *RefreshCoordinator) CreateRepairReview(ctx context.Context, generation int64, planFingerprint string, actionIDs []string) (validate.RepairReview, error) {
	if c == nil || c.store == nil || c.repairStore == nil {
		return validate.RepairReview{}, &validate.RepairReviewError{Code: validate.RepairReviewErrorAuthorityUnavailable, Message: "repair authority is unavailable"}
	}
	c.generationMu.Lock()
	defer c.generationMu.Unlock()
	state, err := c.store.GetValidationState(ctx)
	if err != nil {
		return validate.RepairReview{}, err
	}
	c.authorityMu.RLock()
	authority := c.authority
	if authority == nil || authority.generation != generation || state.Status != semdb.ValidationStatusOK ||
		state.Generation != generation || state.PublishedGeneration != generation {
		c.authorityMu.RUnlock()
		return validate.RepairReview{}, &validate.RepairReviewError{Code: validate.RepairReviewErrorGenerationMismatch, Message: "validation generation no longer has live repair authority"}
	}
	request := validate.RepairReviewCreateRequest{
		VaultIdentity: authority.snapshot.VaultIdentity, Generation: generation,
		PlanFingerprint: planFingerprint, ActionIDs: append([]string(nil), actionIDs...),
		Result: authority.run.Result.Result, RunContext: authority.run.RunContext,
	}
	c.authorityMu.RUnlock()
	return c.repairStore.Create(ctx, request)
}

func (c *RefreshCoordinator) GetRepairReview(id string) (validate.RepairReview, error) {
	if c == nil || c.repairStore == nil {
		return validate.RepairReview{}, &validate.RepairReviewError{Code: validate.RepairReviewErrorAuthorityUnavailable, Message: "repair authority is unavailable"}
	}
	return c.repairStore.Get(id)
}

// ApplyRepairReview holds the generation admission boundary through apply so
// a refresh cannot invalidate the review between its current-generation check
// and canonical repair execution.
func (c *RefreshCoordinator) ApplyRepairReview(ctx context.Context, id string, request validate.RepairReviewApplyRequest) (validate.Result, *validate.FixExecution, error) {
	if c == nil || c.store == nil || c.repairStore == nil {
		return validate.Result{}, nil, &validate.RepairReviewError{Code: validate.RepairReviewErrorAuthorityUnavailable, Message: "repair authority is unavailable"}
	}
	if c.applyOptions.PostApplyRefresher != nil {
		request.Options.PostApplyRefresher = c.applyOptions.PostApplyRefresher
	}
	request.Options.AllowHistorical = c.applyOptions.AllowHistorical
	if c.applyOptions.ReplanCommand != "" {
		request.Options.ReplanCommand = c.applyOptions.ReplanCommand
	}
	review, err := c.repairStore.Get(id)
	if err != nil {
		return validate.Result{}, nil, err
	}
	// Terminal retries are retrieval of a recorded outcome, not new mutation
	// authority. Preserve their idempotent result after later refreshes.
	if review.State == validate.RepairReviewApplied || review.State == validate.RepairReviewFailed {
		return c.repairStore.Apply(ctx, id, request)
	}
	if review.State != validate.RepairReviewPending && review.State != validate.RepairReviewApplying {
		return c.repairStore.Apply(ctx, id, request)
	}
	c.generationMu.Lock()
	defer c.generationMu.Unlock()
	review, err = c.repairStore.Get(id)
	if err != nil {
		return validate.Result{}, nil, err
	}
	if review.State != validate.RepairReviewPending && review.State != validate.RepairReviewApplying {
		return c.repairStore.Apply(ctx, id, request)
	}
	state, err := c.store.GetValidationState(ctx)
	if err != nil {
		return validate.Result{}, nil, err
	}
	if state.Status != semdb.ValidationStatusOK || state.Generation != request.Generation || state.PublishedGeneration != request.Generation {
		return validate.Result{}, nil, &validate.RepairReviewError{Code: validate.RepairReviewErrorGenerationMismatch, Message: "validation generation no longer has live repair authority"}
	}
	return c.repairStore.Apply(ctx, id, request)
}

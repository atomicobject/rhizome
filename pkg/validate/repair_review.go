package validate

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"
	"sync"
	"time"
)

const (
	RepairReviewPending  = "pending"
	RepairReviewApplying = "applying"
	RepairReviewApplied  = "applied"
	RepairReviewFailed   = "failed"
	RepairReviewStale    = "stale"
	RepairReviewExpired  = "expired"
)

type RepairReviewErrorCode string

const (
	RepairReviewErrorNotFound                     RepairReviewErrorCode = "repair_review_not_found"
	RepairReviewErrorActionNotFound               RepairReviewErrorCode = "repair_action_not_found"
	RepairReviewErrorActionUnavailable            RepairReviewErrorCode = "repair_action_unavailable"
	RepairReviewErrorConfirmationRequired         RepairReviewErrorCode = "repair_confirmation_required"
	RepairReviewErrorTransactionIncomplete        RepairReviewErrorCode = "repair_transaction_incomplete"
	RepairReviewErrorPlanFingerprintMismatch      RepairReviewErrorCode = "repair_plan_fingerprint_mismatch"
	RepairReviewErrorSelectionFingerprintMismatch RepairReviewErrorCode = "repair_selection_fingerprint_mismatch"
	RepairReviewErrorGenerationMismatch           RepairReviewErrorCode = "repair_generation_mismatch"
	RepairReviewErrorManualOverlap                RepairReviewErrorCode = "repair_manual_overlap"
	RepairReviewErrorExpired                      RepairReviewErrorCode = "repair_review_expired"
	RepairReviewErrorCapacityFull                 RepairReviewErrorCode = "repair_review_capacity_full"
	RepairReviewErrorApplying                     RepairReviewErrorCode = "repair_review_applying"
	RepairReviewErrorRevalidationRequired         RepairReviewErrorCode = "revalidation_required"
	RepairReviewErrorAuthorityUnavailable         RepairReviewErrorCode = "repair_authority_unavailable"
)

type RepairReviewError struct {
	Code              RepairReviewErrorCode `json:"code"`
	Message           string                `json:"message"`
	RequiredActionIDs []string              `json:"requiredActionIds,omitempty"`
	OverlappingPaths  []string              `json:"overlappingPaths,omitempty"`
}

func (e *RepairReviewError) Error() string {
	if e == nil {
		return "repair review failed"
	}
	return e.Message
}

func newRepairReviewError(code RepairReviewErrorCode, message string) *RepairReviewError {
	return &RepairReviewError{Code: code, Message: message}
}

// RepairPathReservation atomically checks server-known manual drafts and
// reserves the selected paths until release. Manual-session admission must use
// the same coordinator, so a draft cannot enter the overlap gap before apply.
type RepairPathReservation interface {
	Release()
}

type RepairPathReservationSource interface {
	ReserveRepairPaths(context.Context, string, []string) (RepairPathReservation, error)
}

type RepairReviewStoreOptions struct {
	TTL              time.Duration
	MaxPerVault      int
	Now              func() time.Time
	PathReservations RepairPathReservationSource
}

type RepairReviewCreateRequest struct {
	VaultIdentity   string
	Generation      int64
	PlanFingerprint string
	ActionIDs       []string
	Result          Result
	RunContext      RunContext
}

type RepairReviewApplyRequest struct {
	Generation           int64
	PlanFingerprint      string
	SelectionFingerprint string
	Confirmations        []RepairReviewConfirmation
	Options              Options
}

// RepairReviewConfirmation is the exact user acknowledgement required before
// a needs_confirmation action can be applied. CandidatePath is populated when
// the reviewed action chose a concrete candidate.
type RepairReviewConfirmation struct {
	ActionID      string   `json:"actionId"`
	Question      string   `json:"question,omitempty"`
	CandidatePath string   `json:"candidatePath,omitempty"`
	AffectedPaths []string `json:"affectedPaths,omitempty"`
}

type RepairReview struct {
	ID                    string                     `json:"id"`
	VaultIdentity         string                     `json:"vaultIdentity"`
	Generation            int64                      `json:"generation"`
	PlanFingerprint       string                     `json:"planFingerprint"`
	SelectionFingerprint  string                     `json:"selectionFingerprint"`
	ActionIDs             []string                   `json:"actionIds"`
	TransactionIDs        []string                   `json:"transactionIds"`
	AffectedPaths         []string                   `json:"affectedPaths"`
	Preview               []RepairPreviewFile        `json:"preview"`
	RequiredConfirmations []RepairReviewConfirmation `json:"requiredConfirmations,omitempty"`
	State                 string                     `json:"state"`
	CreatedAt             time.Time                  `json:"createdAt"`
	LastAccessedAt        time.Time                  `json:"lastAccessedAt"`
	ExpiresAt             time.Time                  `json:"expiresAt"`
	StaleReason           string                     `json:"staleReason,omitempty"`
}

type repairReviewEntry struct {
	review    RepairReview
	result    Result
	runCtx    RunContext
	terminal  Result
	execution *FixExecution
	applyErr  error
	done      chan struct{}
}

type RepairReviewStore struct {
	mu           sync.Mutex
	reviews      map[string]*repairReviewEntry
	ttl          time.Duration
	maxPerVault  int
	now          func() time.Time
	reservations RepairPathReservationSource
}

func NewRepairReviewStore(opts RepairReviewStoreOptions) *RepairReviewStore {
	if opts.TTL <= 0 {
		opts.TTL = 15 * time.Minute
	}
	if opts.MaxPerVault <= 0 {
		opts.MaxPerVault = 32
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &RepairReviewStore{
		reviews: make(map[string]*repairReviewEntry), ttl: opts.TTL,
		maxPerVault: opts.MaxPerVault, now: opts.Now, reservations: opts.PathReservations,
	}
}

func (s *RepairReviewStore) Create(ctx context.Context, request RepairReviewCreateRequest) (RepairReview, error) {
	if s == nil {
		return RepairReview{}, newRepairReviewError(RepairReviewErrorAuthorityUnavailable, "repair review store is unavailable")
	}
	vault := strings.TrimSpace(request.VaultIdentity)
	if vault == "" || request.Generation <= 0 {
		return RepairReview{}, newRepairReviewError(RepairReviewErrorGenerationMismatch, "repair review requires a vault identity and positive validation generation")
	}
	selection, err := buildRepairReviewSelection(
		request.Result, request.RunContext, request.Generation, request.PlanFingerprint, request.ActionIDs,
	)
	if err != nil {
		return RepairReview{}, err
	}
	reservation, err := s.reserveRepairPaths(ctx, vault, selection.affectedPaths)
	if err != nil {
		return RepairReview{}, err
	}
	reservation.Release()
	id, err := newRepairReviewID()
	if err != nil {
		return RepairReview{}, fmt.Errorf("create repair review id: %w", err)
	}
	now := s.now().UTC()
	review := RepairReview{
		ID: id, VaultIdentity: vault, Generation: request.Generation,
		PlanFingerprint:      strings.TrimSpace(request.PlanFingerprint),
		SelectionFingerprint: selection.selectionFingerprint,
		ActionIDs:            selection.actionIDs, TransactionIDs: selection.transactionIDs,
		AffectedPaths: selection.affectedPaths, Preview: selection.preview,
		RequiredConfirmations: selection.confirmations,
		State:                 RepairReviewPending, CreatedAt: now, LastAccessedAt: now, ExpiresAt: now.Add(s.ttl),
	}
	entry := &repairReviewEntry{review: review, result: selection.result, runCtx: request.RunContext, done: make(chan struct{})}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.evictForVaultLocked(vault, now)
	if s.countVaultLocked(vault) >= s.maxPerVault {
		return RepairReview{}, newRepairReviewError(RepairReviewErrorCapacityFull, "repair review capacity is full; retry after retained reviews expire")
	}
	s.reviews[id] = entry
	return cloneRepairReview(review), nil
}

func (s *RepairReviewStore) Get(id string) (RepairReview, error) {
	if s == nil {
		return RepairReview{}, newRepairReviewError(RepairReviewErrorNotFound, "repair review was not found")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.reviews[strings.TrimSpace(id)]
	if !ok {
		return RepairReview{}, newRepairReviewError(RepairReviewErrorNotFound, "repair review was not found")
	}
	now := s.now().UTC()
	s.expireLocked(entry, now)
	entry.review.LastAccessedAt = now
	if entry.review.State != RepairReviewExpired {
		entry.review.ExpiresAt = now.Add(s.ttl)
	}
	return cloneRepairReview(entry.review), nil
}

func (s *RepairReviewStore) Apply(
	ctx context.Context,
	id string,
	request RepairReviewApplyRequest,
) (Result, *FixExecution, error) {
	if s == nil {
		return Result{}, nil, newRepairReviewError(RepairReviewErrorNotFound, "repair review was not found")
	}
	id = strings.TrimSpace(id)
	var entry *repairReviewEntry
	for {
		s.mu.Lock()
		var err error
		entry, err = s.requireApplicableLocked(id, request, s.now().UTC())
		if err != nil {
			s.mu.Unlock()
			return Result{}, nil, err
		}
		switch entry.review.State {
		case RepairReviewApplied, RepairReviewFailed:
			result, execution, applyErr := cloneRepairReviewResult(entry.terminal), cloneFixExecution(entry.execution), entry.applyErr
			s.mu.Unlock()
			return result, execution, applyErr
		case RepairReviewApplying:
			done := entry.done
			s.mu.Unlock()
			select {
			case <-ctx.Done():
				return Result{}, nil, ctx.Err()
			case <-done:
			}
			continue
		default:
			entry.review.State = RepairReviewApplying
			entry.review.LastAccessedAt = s.now().UTC()
		}
		s.mu.Unlock()
		break
	}

	reservation, err := s.reserveRepairPaths(ctx, entry.review.VaultIdentity, entry.review.AffectedPaths)
	if err != nil {
		s.finishApply(id, Result{}, nil, err)
		return Result{}, nil, err
	}
	defer reservation.Release()
	planned := entry.result
	runCtx := entry.runCtx

	opts := request.Options
	opts.Fix = true
	opts.NonInteractive = false
	opts.Confirm = func(string) (bool, error) { return true, nil }
	opts.RunContext = nil
	result, execution, applyErr := ApplyRepairSession(ctx, planned, runCtx, opts)

	s.finishApply(id, result, execution, applyErr)
	return result, execution, applyErr
}

// MarkVaultStale preserves readable review evidence while removing apply authority.
func (s *RepairReviewStore) MarkVaultStale(vault string, currentGeneration int64, reason string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, entry := range s.reviews {
		if entry.review.VaultIdentity != vault || entry.review.Generation == currentGeneration || entry.review.State != RepairReviewPending {
			continue
		}
		entry.review.State = RepairReviewStale
		entry.review.StaleReason = strings.TrimSpace(reason)
	}
}

func (s *RepairReviewStore) requireApplicableLocked(id string, request RepairReviewApplyRequest, now time.Time) (*repairReviewEntry, error) {
	entry, ok := s.reviews[id]
	if !ok {
		return nil, newRepairReviewError(RepairReviewErrorNotFound, "repair review was not found")
	}
	s.expireLocked(entry, now)
	switch entry.review.State {
	case RepairReviewExpired:
		return nil, newRepairReviewError(RepairReviewErrorExpired, "repair review expired; revalidate and stage the fixes again")
	case RepairReviewStale:
		return nil, newRepairReviewError(RepairReviewErrorRevalidationRequired, "repair review is stale; revalidate and stage the fixes again")
	}
	if request.Generation != entry.review.Generation {
		return nil, newRepairReviewError(RepairReviewErrorGenerationMismatch, "validation generation does not match the repair review")
	}
	if strings.TrimSpace(request.PlanFingerprint) != entry.review.PlanFingerprint {
		return nil, newRepairReviewError(RepairReviewErrorPlanFingerprintMismatch, "repair plan fingerprint does not match the repair review")
	}
	if strings.TrimSpace(request.SelectionFingerprint) != entry.review.SelectionFingerprint {
		return nil, newRepairReviewError(RepairReviewErrorSelectionFingerprintMismatch, "repair selection fingerprint does not match the repair review")
	}
	if !repairReviewConfirmationsEqual(request.Confirmations, entry.review.RequiredConfirmations) {
		return nil, newRepairReviewError(RepairReviewErrorConfirmationRequired, "confirm each needs_confirmation action and its exact candidate before applying")
	}
	return entry, nil
}

func (s *RepairReviewStore) reserveRepairPaths(ctx context.Context, vault string, paths []string) (RepairPathReservation, error) {
	if s.reservations == nil {
		return noopRepairPathReservation{}, nil
	}
	reservation, err := s.reservations.ReserveRepairPaths(ctx, vault, paths)
	if err != nil {
		return nil, err
	}
	if reservation == nil {
		return nil, fmt.Errorf("repair path reservation provider returned no reservation")
	}
	return reservation, nil
}

func (s *RepairReviewStore) finishApply(id string, result Result, execution *FixExecution, applyErr error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry := s.reviews[id]
	if entry == nil || entry.review.State != RepairReviewApplying {
		return
	}
	entry.terminal = cloneRepairReviewResult(result)
	entry.execution = cloneFixExecution(execution)
	entry.applyErr = applyErr
	entry.review.LastAccessedAt = s.now().UTC()
	entry.review.ExpiresAt = entry.review.LastAccessedAt.Add(s.ttl)
	if applyErr == nil {
		entry.review.State = RepairReviewApplied
	} else {
		entry.review.State = RepairReviewFailed
	}
	close(entry.done)
}

func (s *RepairReviewStore) expireLocked(entry *repairReviewEntry, now time.Time) {
	if now.After(entry.review.ExpiresAt) && entry.review.State != RepairReviewApplying {
		entry.review.State = RepairReviewExpired
	}
}

func (s *RepairReviewStore) evictForVaultLocked(vault string, now time.Time) {
	for _, entry := range s.reviews {
		s.expireLocked(entry, now)
	}
	for s.countVaultLocked(vault) >= s.maxPerVault {
		var candidate *repairReviewEntry
		for _, entry := range s.reviews {
			if entry.review.VaultIdentity != vault || (entry.review.State == RepairReviewApplying || entry.review.State == RepairReviewApplied || entry.review.State == RepairReviewFailed) {
				continue
			}
			if candidate == nil || entry.review.LastAccessedAt.Before(candidate.review.LastAccessedAt) ||
				(entry.review.LastAccessedAt.Equal(candidate.review.LastAccessedAt) && entry.review.ID < candidate.review.ID) {
				candidate = entry
			}
		}
		if candidate == nil {
			return
		}
		delete(s.reviews, candidate.review.ID)
	}
}

func (s *RepairReviewStore) countVaultLocked(vault string) int {
	count := 0
	for _, entry := range s.reviews {
		if entry.review.VaultIdentity == vault {
			count++
		}
	}
	return count
}

func newRepairReviewID() (string, error) {
	bytes := make([]byte, 18)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return "repair-review:" + base64.RawURLEncoding.EncodeToString(bytes), nil
}

func cloneRepairReview(review RepairReview) RepairReview {
	review.ActionIDs = append([]string(nil), review.ActionIDs...)
	review.TransactionIDs = append([]string(nil), review.TransactionIDs...)
	review.AffectedPaths = append([]string(nil), review.AffectedPaths...)
	review.Preview = append([]RepairPreviewFile(nil), review.Preview...)
	review.RequiredConfirmations = cloneRepairReviewConfirmations(review.RequiredConfirmations)
	return review
}

func cloneRepairReviewConfirmations(input []RepairReviewConfirmation) []RepairReviewConfirmation {
	cloned := append([]RepairReviewConfirmation(nil), input...)
	for i := range cloned {
		cloned[i].AffectedPaths = append([]string(nil), cloned[i].AffectedPaths...)
	}
	return cloned
}

func cloneFixExecution(execution *FixExecution) *FixExecution {
	if execution == nil {
		return nil
	}
	copy := *execution
	copy.Applied = append([]string(nil), execution.Applied...)
	copy.Skipped = append([]string(nil), execution.Skipped...)
	copy.Failed = append([]string(nil), execution.Failed...)
	copy.RemainingIssueKeys = append([]string(nil), execution.RemainingIssueKeys...)
	copy.Transactions = append([]RepairTransactionExecution(nil), execution.Transactions...)
	copy.FollowUps = append([]RepairFollowUp(nil), execution.FollowUps...)
	return &copy
}

type noopRepairPathReservation struct{}

func (noopRepairPathReservation) Release() {}

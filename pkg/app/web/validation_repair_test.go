package web

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/validate"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestValidationRepairRoutesCreateGetAndApplyCanonicalReview(t *testing.T) {
	now := time.Now().UTC()
	review := validate.RepairReview{
		ID: "repair-review:test", VaultIdentity: "vault", Generation: 7,
		PlanFingerprint: "plan", SelectionFingerprint: "selection",
		ActionIDs: []string{"safe", "confirm", "identifier"}, TransactionIDs: []string{"write", "rename", "delete", "identifier"},
		AffectedPaths: []string{"a.md", "b.md", "c.md", "identifier.md"},
		Preview: []validate.RepairPreviewFile{
			{Path: "a.md", Kind: validate.RepairOperationWrite, Diff: "write diff"},
			{Path: "b.md", OriginalPath: "old.md", Kind: validate.RepairOperationRename, Diff: "rename diff"},
			{Path: "c.md", Kind: validate.RepairOperationDelete, Diff: "delete diff"},
			{Path: "identifier.md", Kind: validate.RepairOperationWrite, Diff: "identifier diff"},
		},
		RequiredConfirmations: []validate.RepairReviewConfirmation{{
			ActionID: "confirm", Question: "Use this target?", CandidatePath: "target.md", AffectedPaths: []string{"a.md"},
		}},
		State: validate.RepairReviewPending, CreatedAt: now, LastAccessedAt: now, ExpiresAt: now.Add(time.Minute),
	}
	authority := &repairHandlerAuthority{review: review}
	srv := repairHandlerServer(authority, "vault")

	create := performRepairRequest(t, srv, http.MethodPost, "/api/v1/validation/repair-reviews", ValidationRepairReviewCreateRequest{
		Generation: 7, PlanFingerprint: "plan", ActionIDs: review.ActionIDs,
	})
	require.Equal(t, http.StatusCreated, create.Code)
	var created validate.RepairReview
	require.NoError(t, json.NewDecoder(create.Body).Decode(&created))
	require.Equal(t, review.Preview, created.Preview)
	require.Equal(t, review.RequiredConfirmations, created.RequiredConfirmations)
	require.Equal(t, review.ActionIDs, authority.createdActions)

	get := performRepairRequest(t, srv, http.MethodGet, "/api/v1/validation/repair-reviews/repair-review%3Atest", nil)
	require.Equal(t, http.StatusOK, get.Code)

	confirmation := created.RequiredConfirmations
	authority.applyResult = validate.Result{OK: true}
	authority.applyExecution = &validate.FixExecution{
		Requested: true, AppliedTransactions: 4,
		Transactions: []validate.RepairTransactionExecution{{TransactionID: "write", Status: "applied"}, {TransactionID: "identifier", Status: "applied"}},
	}
	authority.review.State = validate.RepairReviewApplied
	apply := performRepairRequest(t, srv, http.MethodPost, "/api/v1/validation/repair-reviews/repair-review%3Atest/apply", ValidationRepairReviewApplyRequest{
		Generation: 7, PlanFingerprint: "plan", SelectionFingerprint: "selection", Confirmations: confirmation,
	})
	require.Equal(t, http.StatusOK, apply.Code)
	var applied ValidationRepairApplyResponse
	require.NoError(t, json.NewDecoder(apply.Body).Decode(&applied))
	require.Equal(t, validate.RepairReviewApplied, applied.Review.State)
	require.Equal(t, confirmation, authority.appliedRequest.Confirmations)
	require.Equal(t, 4, applied.Execution.AppliedTransactions)
}

func TestValidationRepairApplyReturnsMixedTerminalEvidenceAndStableSerialization(t *testing.T) {
	mixedExecution := func() *validate.FixExecution {
		return &validate.FixExecution{
			Requested: true, Applied: []string{"action:a"}, Skipped: []string{"action:b"}, Failed: []string{"action:c"},
			RemainingIssueKeys: []string{"issue:b", "issue:c"}, RemainingFindings: 2,
			Transactions: []validate.RepairTransactionExecution{
				{TransactionID: "tx:a", Status: "applied", AffectedPaths: []string{"a.md"}},
				{TransactionID: "tx:b", Status: "skipped", Reason: "source changed", AffectedPaths: []string{"b.md"}},
				{TransactionID: "tx:c", Status: "failed", Reason: "lifecycle protected", AffectedPaths: []string{"c.md"}},
			},
		}
	}
	review := repairHandlerReview("vault")
	authority := &repairHandlerAuthority{
		review:         review,
		applyResult:    validate.Result{OK: false, IssueCount: 2},
		applyExecution: mixedExecution(),
	}
	authority.review.State = validate.RepairReviewApplied
	srv := repairHandlerServer(authority, "vault")
	request := ValidationRepairReviewApplyRequest{Generation: 7, PlanFingerprint: "plan", SelectionFingerprint: "selection"}

	first := performRepairRequest(t, srv, http.MethodPost, "/api/v1/validation/repair-reviews/id/apply", request)
	require.Equal(t, http.StatusOK, first.Code)
	var applied ValidationRepairApplyResponse
	require.NoError(t, json.Unmarshal(first.Body.Bytes(), &applied))
	require.Equal(t, validate.RepairReviewApplied, applied.Review.State)
	require.False(t, applied.Result.OK)
	require.Equal(t, 2, applied.Result.IssueCount)
	require.Equal(t, mixedExecution(), applied.Execution)

	// The fake authority returns the same evidence twice, so this proves stable
	// serialization only; canonical apply idempotence belongs to pkg/validate.
	second := performRepairRequest(t, srv, http.MethodPost, "/api/v1/validation/repair-reviews/id/apply", request)
	require.Equal(t, first.Body.String(), second.Body.String())
	require.Equal(t, 2, authority.applyCalls)
}

func TestValidationRepairApplyFailurePreservesStaleSourceAndIndependentOutcomes(t *testing.T) {
	authority := &repairHandlerAuthority{
		review: repairHandlerReview("vault"), applyResult: validate.Result{OK: false, IssueCount: 1},
		applyExecution: &validate.FixExecution{Requested: true, Applied: []string{"action:a"}, Failed: []string{"action:b"}, Transactions: []validate.RepairTransactionExecution{
			{TransactionID: "tx:a", Status: "applied", AffectedPaths: []string{"a.md"}},
			{TransactionID: "tx:b", Status: "failed", Reason: "source hash changed", AffectedPaths: []string{"stale.md"}},
		}},
		applyErr: errors.New("repair source stale.md changed after review"),
	}
	authority.review.State = validate.RepairReviewFailed
	rec := performRepairRequest(t, repairHandlerServer(authority, "vault"), http.MethodPost, "/api/v1/validation/repair-reviews/id/apply", ValidationRepairReviewApplyRequest{
		Generation: 7, PlanFingerprint: "plan", SelectionFingerprint: "selection",
	})
	require.Equal(t, http.StatusConflict, rec.Code)
	var response struct {
		Error   string                       `json:"error"`
		Code    string                       `json:"code"`
		Details validationRepairApplyFailure `json:"details"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	require.Equal(t, PublicErrorRepairApplyFailed, response.Code)
	require.Equal(t, "repair source stale.md changed after review", response.Error)
	details := response.Details
	require.NotNil(t, details.Review)
	require.Equal(t, validate.RepairReviewFailed, details.Review.State)
	require.NotNil(t, details.Result)
	require.Equal(t, 1, details.Result.IssueCount)
	require.NotNil(t, details.Execution)
	require.Equal(t, []string{"action:a"}, details.Execution.Applied)
	require.Equal(t, []string{"action:b"}, details.Execution.Failed)
	require.Equal(t, []validate.RepairTransactionExecution{
		{TransactionID: "tx:a", Status: "applied", AffectedPaths: []string{"a.md"}},
		{TransactionID: "tx:b", Status: "failed", Reason: "source hash changed", AffectedPaths: []string{"stale.md"}},
	}, details.Execution.Transactions)
}

func TestValidationRepairRoutesReturnTypedLifecycleErrors(t *testing.T) {
	tests := []struct {
		name, route string
		method      string
		reviewErr   *validate.RepairReviewError
		status      int
	}{
		{"expired", "/api/v1/validation/repair-reviews/id", http.MethodGet, repairReviewError(validate.RepairReviewErrorExpired), http.StatusGone},
		{"generation invalidated", "/api/v1/validation/repair-reviews/id/apply", http.MethodPost, repairReviewError(validate.RepairReviewErrorGenerationMismatch), http.StatusConflict},
		{"lifecycle blocked", "/api/v1/validation/repair-reviews", http.MethodPost, repairReviewError(validate.RepairReviewErrorRevalidationRequired), http.StatusConflict},
		{"restart lost authority", "/api/v1/validation/repair-reviews/id", http.MethodGet, repairReviewError(validate.RepairReviewErrorNotFound), http.StatusNotFound},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			authority := &repairHandlerAuthority{review: repairHandlerReview("vault")}
			if test.method == http.MethodGet {
				authority.getErr = test.reviewErr
			} else if test.route == "/api/v1/validation/repair-reviews" {
				authority.createErr = test.reviewErr
			} else {
				authority.applyErr = test.reviewErr
			}
			body := any(ValidationRepairReviewApplyRequest{Generation: 7, PlanFingerprint: "plan", SelectionFingerprint: "selection"})
			if test.route == "/api/v1/validation/repair-reviews" {
				body = ValidationRepairReviewCreateRequest{Generation: 7, PlanFingerprint: "plan", ActionIDs: []string{"action"}}
			}
			rec := performRepairRequest(t, repairHandlerServer(authority, "vault"), test.method, test.route, body)
			require.Equal(t, test.status, rec.Code)
			var response ErrorResponse
			require.NoError(t, json.NewDecoder(rec.Body).Decode(&response))
			require.Equal(t, strings.ToUpper(string(test.reviewErr.Code)), response.Code)
		})
	}
}

func TestValidationRepairGetReturnsExpiredStoredReviewAsGone(t *testing.T) {
	review := repairHandlerReview("vault")
	review.State = validate.RepairReviewExpired
	review.ExpiresAt = time.Now().Add(-time.Minute)
	rec := performRepairRequest(t, repairHandlerServer(&repairHandlerAuthority{review: review}, "vault"), http.MethodGet, "/api/v1/validation/repair-reviews/id", nil)
	require.Equal(t, http.StatusGone, rec.Code)
	var response ErrorResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&response))
	require.Equal(t, "REPAIR_REVIEW_EXPIRED", response.Code)
}

func TestValidationRepairRoutesRejectWrongVaultAndMissingRuntimeAuthority(t *testing.T) {
	wrong := performRepairRequest(t, repairHandlerServer(&repairHandlerAuthority{review: repairHandlerReview("other")}, "vault"), http.MethodGet, "/api/v1/validation/repair-reviews/id", nil)
	require.Equal(t, http.StatusNotFound, wrong.Code)
	var wrongBody ErrorResponse
	require.NoError(t, json.NewDecoder(wrong.Body).Decode(&wrongBody))
	require.Equal(t, PublicErrorRepairWrongVault, wrongBody.Code)

	missing := repairHandlerServer(nil, "vault")
	unavailable := performRepairRequest(t, missing, http.MethodPost, "/api/v1/validation/repair-reviews", ValidationRepairReviewCreateRequest{Generation: 1, PlanFingerprint: "plan", ActionIDs: []string{"action"}})
	require.Equal(t, http.StatusServiceUnavailable, unavailable.Code)
}

func TestPublicCapabilitiesAdvertiseLiveRepairContract(t *testing.T) {
	fixture := prepareOntologyFixtureVault(t)
	srv := newFixtureServer(t, fixture, nil)
	srv.runtime.SetValidationRepairAuthority(&repairHandlerAuthority{review: repairHandlerReview(fixture.vaultDef.Name)})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/capabilities", nil)
	rec := httptest.NewRecorder()
	srv.handlePublicCapabilities(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	var capabilities PublicCapabilitiesResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&capabilities))
	require.True(t, capabilities.Validation.RepairAvailable)
	require.Contains(t, capabilities.Validation.RepairEndpoints, "/api/v1/validation/repair-reviews/{id}/apply")
	require.Contains(t, capabilities.REST.Resources, "validation-repair-reviews")
	require.Contains(t, capabilities.Errors.Codes, "REPAIR_REVIEW_EXPIRED")
	require.Contains(t, capabilities.Validation.Endpoints, "/api/v1/validation/groups")

	groups := newApplicationRequest(http.MethodPost, "/api/v1/validation/groups", strings.NewReader(`{"generation":99999}`))
	groupsRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(groupsRec, groups)
	require.Equal(t, http.StatusGone, groupsRec.Code, "the mounted groups route reaches the store")
}

type repairHandlerAuthority struct {
	mu             sync.Mutex
	review         validate.RepairReview
	createErr      error
	getErr         error
	applyErr       error
	applyResult    validate.Result
	applyExecution *validate.FixExecution
	createdActions []string
	appliedRequest validate.RepairReviewApplyRequest
	applyCalls     int
}

func (a *repairHandlerAuthority) CreateRepairReview(_ context.Context, _ int64, _ string, actions []string) (validate.RepairReview, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.createdActions = append([]string(nil), actions...)
	return a.review, a.createErr
}

func (a *repairHandlerAuthority) GetRepairReview(string) (validate.RepairReview, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.review, a.getErr
}

func (a *repairHandlerAuthority) ApplyRepairReview(_ context.Context, _ string, request validate.RepairReviewApplyRequest) (validate.Result, *validate.FixExecution, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.applyCalls++
	a.appliedRequest = request
	return a.applyResult, a.applyExecution, a.applyErr
}

func repairHandlerReview(vault string) validate.RepairReview {
	now := time.Now().UTC()
	return validate.RepairReview{
		ID: "id", VaultIdentity: vault, Generation: 7, PlanFingerprint: "plan", SelectionFingerprint: "selection",
		ActionIDs: []string{"action"}, TransactionIDs: []string{"tx"}, AffectedPaths: []string{"note.md"},
		Preview: []validate.RepairPreviewFile{{Path: "note.md", Kind: validate.RepairOperationWrite, Diff: "diff"}},
		State:   validate.RepairReviewPending, CreatedAt: now, LastAccessedAt: now, ExpiresAt: now.Add(time.Minute),
	}
}

func repairReviewError(code validate.RepairReviewErrorCode) *validate.RepairReviewError {
	return &validate.RepairReviewError{Code: code, Message: string(code)}
}

func repairHandlerServer(authority ValidationRepairAuthority, vault string) *Server {
	runtime := &Runtime{}
	if authority != nil {
		runtime.SetValidationRepairAuthority(authority)
	}
	srv := &Server{runtime: runtime, cfg: Config{VaultDef: obsidian.VaultDefinition{Name: vault}}, mux: http.NewServeMux()}
	srv.registerRoutes()
	return srv
}

func performRepairRequest(t *testing.T, srv *Server, method, route string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var encoded []byte
	if body != nil {
		var err error
		encoded, err = json.Marshal(body)
		require.NoError(t, err)
	}
	req := newApplicationRequest(method, route, bytes.NewReader(encoded))
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

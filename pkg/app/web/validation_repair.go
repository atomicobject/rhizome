package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/atomicobject/rhizome/pkg/validate"
)

const (
	PublicErrorRepairApplyFailed = "REPAIR_APPLY_FAILED"
	PublicErrorRepairWrongVault  = "REPAIR_WRONG_VAULT"
)

type ValidationRepairReviewCreateRequest struct {
	Generation      int64    `json:"generation"`
	PlanFingerprint string   `json:"planFingerprint"`
	ActionIDs       []string `json:"actionIds"`
}

type ValidationRepairReviewApplyRequest struct {
	Generation           int64                               `json:"generation"`
	PlanFingerprint      string                              `json:"planFingerprint"`
	SelectionFingerprint string                              `json:"selectionFingerprint"`
	Confirmations        []validate.RepairReviewConfirmation `json:"confirmations,omitempty"`
}

type ValidationRepairApplyResponse struct {
	Review    validate.RepairReview  `json:"review"`
	Result    validate.Result        `json:"result"`
	Execution *validate.FixExecution `json:"execution,omitempty"`
}

type validationRepairApplyFailure struct {
	Review      *validate.RepairReview      `json:"review,omitempty"`
	Result      *validate.Result            `json:"result,omitempty"`
	Execution   *validate.FixExecution      `json:"execution,omitempty"`
	RepairError *validate.RepairReviewError `json:"repairError,omitempty"`
}

func (s *Server) handlePublicValidationRepairReviews(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writePublicError(w, http.StatusMethodNotAllowed, PublicErrorMethodNotAllowed, errors.New("method not allowed"))
		return
	}
	authority := s.validationRepairAuthority()
	if authority == nil {
		writeRepairReviewError(w, &validate.RepairReviewError{Code: validate.RepairReviewErrorAuthorityUnavailable, Message: "repair authority is unavailable until validation completes"}, nil)
		return
	}
	var request ValidationRepairReviewCreateRequest
	if err := decodeValidationRepairBody(w, r, &request); err != nil {
		writePublicError(w, http.StatusBadRequest, PublicErrorBadRequest, err)
		return
	}
	review, err := authority.CreateRepairReview(r.Context(), request.Generation, request.PlanFingerprint, request.ActionIDs)
	if err != nil {
		writeRepairReviewError(w, err, nil)
		return
	}
	if !s.repairReviewMatchesVault(review) {
		writeRepairReviewWrongVault(w)
		return
	}
	writeJSON(w, http.StatusCreated, review)
}

func (s *Server) handlePublicValidationRepairReviewByID(w http.ResponseWriter, r *http.Request) {
	id, action, err := parseValidationRepairReviewPath(r.URL.Path)
	if err != nil {
		writePublicError(w, http.StatusBadRequest, PublicErrorBadRequest, err)
		return
	}
	authority := s.validationRepairAuthority()
	if authority == nil {
		writeRepairReviewError(w, &validate.RepairReviewError{Code: validate.RepairReviewErrorAuthorityUnavailable, Message: "repair authority is unavailable until validation completes"}, nil)
		return
	}
	if action == "" {
		if r.Method != http.MethodGet {
			writePublicError(w, http.StatusMethodNotAllowed, PublicErrorMethodNotAllowed, errors.New("method not allowed"))
			return
		}
		review, err := authority.GetRepairReview(id)
		if err != nil {
			writeRepairReviewError(w, err, nil)
			return
		}
		if !s.repairReviewMatchesVault(review) {
			writeRepairReviewWrongVault(w)
			return
		}
		if review.State == validate.RepairReviewExpired {
			writeRepairReviewError(w, &validate.RepairReviewError{Code: validate.RepairReviewErrorExpired, Message: "repair review expired; revalidate and stage the fixes again"}, review)
			return
		}
		writeJSON(w, http.StatusOK, review)
		return
	}
	if action != "apply" {
		writePublicError(w, http.StatusNotFound, PublicErrorNotFound, errors.New("repair review action not found"))
		return
	}
	if r.Method != http.MethodPost {
		writePublicError(w, http.StatusMethodNotAllowed, PublicErrorMethodNotAllowed, errors.New("method not allowed"))
		return
	}
	s.applyPublicValidationRepairReview(w, r, authority, id)
}

func (s *Server) applyPublicValidationRepairReview(w http.ResponseWriter, r *http.Request, authority ValidationRepairAuthority, id string) {
	review, err := authority.GetRepairReview(id)
	if err != nil {
		writeRepairReviewError(w, err, nil)
		return
	}
	if !s.repairReviewMatchesVault(review) {
		writeRepairReviewWrongVault(w)
		return
	}
	var request ValidationRepairReviewApplyRequest
	if err := decodeValidationRepairBody(w, r, &request); err != nil {
		writePublicError(w, http.StatusBadRequest, PublicErrorBadRequest, err)
		return
	}
	result, execution, applyErr := authority.ApplyRepairReview(r.Context(), id, validate.RepairReviewApplyRequest{
		Generation: request.Generation, PlanFingerprint: request.PlanFingerprint,
		SelectionFingerprint: request.SelectionFingerprint, Confirmations: request.Confirmations,
	})
	terminal, getErr := authority.GetRepairReview(id)
	if getErr != nil || !s.repairReviewMatchesVault(terminal) {
		terminal = review
	}
	if applyErr != nil {
		failure := validationRepairApplyFailure{Review: &terminal, Result: &result, Execution: execution}
		var repairErr *validate.RepairReviewError
		if errors.As(applyErr, &repairErr) {
			failure.RepairError = repairErr
		}
		writeRepairReviewError(w, applyErr, failure)
		return
	}
	writeJSON(w, http.StatusOK, ValidationRepairApplyResponse{Review: terminal, Result: result, Execution: execution})
}

func (s *Server) validationRepairAuthority() ValidationRepairAuthority {
	if s == nil || s.runtime == nil {
		return nil
	}
	return s.runtime.ValidationRepairAuthority()
}

func (s *Server) repairReviewMatchesVault(review validate.RepairReview) bool {
	return strings.TrimSpace(review.VaultIdentity) != "" && review.VaultIdentity == s.validationVaultIdentity()
}

func decodeValidationRepairBody(w http.ResponseWriter, r *http.Request, target any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode repair review request: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("decode repair review request: multiple JSON values")
		}
		return fmt.Errorf("decode repair review request: %w", err)
	}
	return nil
}

func parseValidationRepairReviewPath(raw string) (string, string, error) {
	clean := path.Clean(raw)
	const prefix = "/api/v1/validation/repair-reviews/"
	if !strings.HasPrefix(clean, prefix) {
		return "", "", errors.New("invalid repair review route")
	}
	parts := strings.Split(strings.TrimPrefix(clean, prefix), "/")
	if len(parts) < 1 || len(parts) > 2 || strings.TrimSpace(parts[0]) == "" {
		return "", "", errors.New("invalid repair review route")
	}
	id, err := url.PathUnescape(parts[0])
	if err != nil || strings.TrimSpace(id) == "" {
		return "", "", errors.New("invalid repair review id")
	}
	action := ""
	if len(parts) == 2 {
		action = strings.TrimSpace(parts[1])
	}
	return strings.TrimSpace(id), action, nil
}

func writeRepairReviewWrongVault(w http.ResponseWriter) {
	writePublicError(w, http.StatusNotFound, PublicErrorRepairWrongVault, errors.New("repair review does not belong to this vault"))
}

func writeRepairReviewError(w http.ResponseWriter, err error, details any) {
	var reviewErr *validate.RepairReviewError
	if !errors.As(err, &reviewErr) {
		writePublicErrorDetails(w, http.StatusConflict, PublicErrorRepairApplyFailed, err, details)
		return
	}
	status := http.StatusConflict
	switch reviewErr.Code {
	case validate.RepairReviewErrorNotFound:
		status = http.StatusNotFound
	case validate.RepairReviewErrorExpired:
		status = http.StatusGone
	case validate.RepairReviewErrorAuthorityUnavailable:
		status = http.StatusServiceUnavailable
	case validate.RepairReviewErrorActionNotFound,
		validate.RepairReviewErrorActionUnavailable,
		validate.RepairReviewErrorConfirmationRequired,
		validate.RepairReviewErrorTransactionIncomplete,
		validate.RepairReviewErrorPlanFingerprintMismatch,
		validate.RepairReviewErrorSelectionFingerprintMismatch,
		validate.RepairReviewErrorGenerationMismatch,
		validate.RepairReviewErrorManualOverlap,
		validate.RepairReviewErrorApplying,
		validate.RepairReviewErrorCapacityFull,
		validate.RepairReviewErrorRevalidationRequired:
		status = http.StatusConflict
	}
	if details == nil {
		details = reviewErr
	}
	writePublicErrorDetails(w, status, strings.ToUpper(string(reviewErr.Code)), reviewErr, details)
}

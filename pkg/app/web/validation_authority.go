package web

import (
	"context"
	"strings"

	"github.com/atomicobject/rhizome/pkg/validate"
)

// ValidationRepairAuthority is the handler-neutral boundary to the latest
// server-owned validation result. HTTP routes consume it without recreating
// repair operations from cached JSON.
type ValidationRepairAuthority interface {
	CreateRepairReview(context.Context, int64, string, []string) (validate.RepairReview, error)
	GetRepairReview(string) (validate.RepairReview, error)
	ApplyRepairReview(context.Context, string, validate.RepairReviewApplyRequest) (validate.Result, *validate.FixExecution, error)
}

// ValidationRefreshRequester schedules a server-owned validation generation
// after a successful source mutation has refreshed its exact derived state.
type ValidationRefreshRequester interface {
	RequestValidationRefresh()
	ValidationRefreshPending() bool
}

func (s *Server) validationVaultIdentity() string {
	if s == nil {
		return ""
	}
	if name := strings.TrimSpace(s.cfg.VaultDef.Name); name != "" {
		return name
	}
	return strings.TrimSpace(s.cfg.VaultDef.BasePath())
}

func (r *Runtime) SetValidationRepairAuthority(authority ValidationRepairAuthority) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.validationRepairAuthority = authority
}

func (r *Runtime) SetRepairPathCoordinator(paths *validate.RepairPathCoordinator) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.repairPathCoordinator = paths
}

func (r *Runtime) SetValidationRefreshRequester(requester ValidationRefreshRequester) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.validationRefreshRequester = requester
}

func (r *Runtime) RequestValidationRefresh() bool {
	if r == nil {
		return false
	}
	r.mu.RLock()
	requester := r.validationRefreshRequester
	r.mu.RUnlock()
	if requester != nil {
		requester.RequestValidationRefresh()
		return true
	}
	return false
}

// ValidationRefreshPending includes debounced and startup-blocked requests.
func (r *Runtime) ValidationRefreshPending() bool {
	if r == nil {
		return false
	}
	r.mu.RLock()
	requester := r.validationRefreshRequester
	r.mu.RUnlock()
	return requester != nil && requester.ValidationRefreshPending()
}

func (r *Runtime) ValidationRepairAuthority() ValidationRepairAuthority {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.validationRepairAuthority
}

func (r *Runtime) RepairPathCoordinator() *validate.RepairPathCoordinator {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.repairPathCoordinator
}

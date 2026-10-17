package desktop

import (
	"context"
	"fmt"

	"github.com/atomicobject/rhizome/pkg/app/appbundle"
	"github.com/atomicobject/rhizome/pkg/app/repoexec"
	appupdate "github.com/atomicobject/rhizome/pkg/app/update"
	"github.com/atomicobject/rhizome/pkg/vault/version"
)

type PinOffer struct {
	State     string `json:"state"`
	Pinned    string `json:"pinned"`
	Available string `json:"available"`
}

type PinAdvanceResult struct {
	PinnedVersion string `json:"pinnedVersion"`
}

func (s *Service) pinOffer(plan repoexec.Plan) *PinOffer {
	if plan.Authority != "managed" || plan.SourceRoot != "" {
		return nil
	}
	available := ""
	if s.available != nil {
		available = s.available()
	} else {
		available = s.availableVersion()
	}
	order, comparable := appupdate.CompareVersions(plan.PinnedVersion, available)
	if !comparable || order == 0 {
		return nil
	}
	state := "advance"
	if order > 0 {
		state = "app-older"
	}
	return &PinOffer{State: state, Pinned: plan.PinnedVersion, Available: available}
}

func (s *Service) availableVersion() string {
	if executable, err := desktopExecutable(); err == nil {
		if bundle, err := appbundle.Resolve(executable); err == nil && bundle != nil {
			return version.Version
		}
	}
	executable, err := s.globalExecutable("")
	if err != nil || executable == "" {
		return ""
	}
	neutral, cleanup, err := s.neutralDir()
	if err != nil {
		return ""
	}
	defer cleanup()
	available, _ := repoexec.Version(context.Background(), executable, neutral)
	return available
}

// PinAdvance uses this bridge's updater and existing checkout trust. Runtime
// replacement remains the normal Open operation after a successful advance.
func (s *Service) PinAdvance(ctx context.Context, path string) (PinAdvanceResult, error) {
	info, plan, err := s.inspect(path)
	result := PinAdvanceResult{PinnedVersion: info.PinnedVersion}
	if err != nil {
		return result, err
	}
	if plan.Authority != "managed" || plan.SourceRoot != "" {
		return result, problem("pin_unsupported", "Only Rhizome-managed repository pins can be advanced by the app.")
	}
	if !info.Trusted {
		return result, problem("trust_required", "Confirm trust for this folder before advancing its Rhizome pin.")
	}
	executable, err := desktopExecutable()
	if err != nil {
		return result, problem("pin_advance_failed", fmt.Sprintf("Pin advance executable stage failed: %v", err))
	}
	_, updateErr := s.install(ctx, appupdate.Options{ExecutablePath: executable, WorkDir: info.Path, SetVersion: version.Version, Yes: true})
	if updateErr != nil {
		return result, problem("pin_advance_failed", fmt.Sprintf("Pin advance failed: %v", updateErr))
	}
	updated, err := s.Inspect(info.Path)
	if err != nil {
		return result, problem("pin_advance_failed", fmt.Sprintf("Pin advance inspection stage failed: %v", err))
	}
	return PinAdvanceResult{PinnedVersion: updated.PinnedVersion}, nil
}

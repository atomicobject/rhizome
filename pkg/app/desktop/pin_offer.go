package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/repoexec"
	appupdate "github.com/atomicobject/rhizome/pkg/app/update"
	"github.com/atomicobject/rhizome/pkg/sqliteutil/migration"
	"github.com/atomicobject/rhizome/pkg/store"
	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
	"github.com/atomicobject/rhizome/pkg/vault/version"
)

// PinOffer compares a worktree's pin with the app's Rhizome. State is
// "advance" (the pin can move to Available), "app-older", or "incompatible"
// (Available is newer but cannot read the worktree's index; Reason says why).
// Caution qualifies an advance the app could not check.
type PinOffer struct {
	State     string `json:"state"`
	Pinned    string `json:"pinned"`
	Available string `json:"available"`
	Reason    string `json:"reason,omitempty"`
	Caution   string `json:"caution,omitempty"`
}

type PinAdvanceResult struct {
	PinnedVersion string `json:"pinnedVersion"`
}

func (s *Service) pinOffer(plan repoexec.Plan) *PinOffer {
	if plan.Authority != "managed" || plan.SourceRoot != "" {
		return nil
	}
	available, readsIndex := "", s.readsIndex
	if s.available != nil {
		available = s.available()
	} else {
		s.availableOnce.Do(func() { s.availableCached, s.availableOwn = s.availableVersion() })
		available = s.availableCached
		if s.availableOwn {
			readsIndex = func(path string) error { return store.CheckReadable(context.Background(), path) }
		}
	}
	order, comparable := appupdate.CompareVersions(plan.PinnedVersion, available)
	if !comparable || order == 0 {
		return nil
	}
	state := "advance"
	if order > 0 {
		state = "app-older"
	}
	// Pins and release tags carry a leading v; release builds report their
	// version without one.
	offer := &PinOffer{State: state, Pinned: plan.PinnedVersion, Available: appupdate.NormalizeVersion(available)}
	if state == "advance" {
		offer.State, offer.Reason, offer.Caution = advanceCompatibility(plan, offer.Available, readsIndex)
	}
	return offer
}

// advanceCompatibility keeps the app from offering a Rhizome that loses the
// worktree's index. The app can vouch only for its own build (readsIndex is
// nil otherwise): that build reads the index as its runtime would, and ships
// a credential store on every desktop platform, so sign-in carries over.
func advanceCompatibility(plan repoexec.Plan, available string, readsIndex func(string) error) (state, reason, caution string) {
	if readsIndex == nil {
		return "advance", "", fmt.Sprintf("The app could not check that Rhizome %s can read this worktree's index and sign in.", available)
	}
	path, ok := databasePath(plan.ConfigDir)
	if !ok {
		return "advance", "", ""
	}
	err := readsIndex(path)
	var future *migration.ErrFutureSchema
	switch {
	case errors.As(err, &future):
		return "incompatible", fmt.Sprintf("Rhizome %s can't read this worktree's index, which a newer Rhizome wrote.", available), ""
	case err != nil:
		return "advance", "", fmt.Sprintf("The app could not check that Rhizome %s can read this worktree's index: %v", available, err)
	}
	return "advance", "", ""
}

// availableVersion reports the app's Rhizome version, and whether this
// bridge's own build is that version's published release, so its index
// support is what a pin advance installs. A development build reports the
// last release's version while reading newer indexes, so it is unchecked.
func (s *Service) availableVersion() (string, bool) {
	if executable, err := desktopExecutable(); err == nil {
		if bundled, err := bundledExecutable(executable); err == nil && bundled != "" {
			return version.Version, !version.Development()
		}
	}
	executable, err := s.globalExecutable("")
	if err != nil || executable == "" {
		return "", false
	}
	neutral, cleanup, err := s.neutralDir()
	if err != nil {
		return "", false
	}
	defer cleanup()
	available, _ := repoexec.Version(context.Background(), executable, neutral)
	return available, false
}

func (s *Service) pinAdvanceFolder(path string) (FolderInfo, error) {
	info, plan, err := s.inspect(path)
	if err != nil {
		return info, err
	}
	if plan.Authority != "managed" || plan.SourceRoot != "" {
		return info, problem("pin_unsupported", "Only Rhizome-managed repository pins can be advanced by the app.")
	}
	if !info.Trusted {
		return info, problem("trust_required", "Confirm trust for this folder before advancing its Rhizome pin.")
	}
	// The offer may be stale, for example after a pull moved the pin past the
	// app; advancing then would be a downgrade.
	if info.PinOffer == nil || info.PinOffer.State != "advance" {
		return info, problem("pin_unsupported", "This repository's pin is not older than the app's Rhizome.")
	}
	return info, nil
}

func (s *Service) UpgradePlan(ctx context.Context, path string) (json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(vaultconfig.WithEnv(ctx, s.env), 2*time.Minute)
	defer cancel()
	info, err := s.pinAdvanceFolder(path)
	if err != nil {
		return nil, err
	}
	// The executable that reported the offered version, as availableVersion
	// resolves it, so the preview shows that release's files.
	executable, err := s.globalExecutable("")
	if err != nil || executable == "" {
		return nil, problem("upgrade_plan_failed", fmt.Sprintf("Upgrade plan executable stage failed: no Rhizome executable found (%v)", err))
	}
	return runSetupJSON(ctx, executable, info.Path, []string{"init", "--path", info.Path, "--check", "--json"}, "", "upgrade_plan_failed", false)
}

// PinAdvance uses this bridge's updater and existing checkout trust. Runtime
// replacement remains the normal Open operation after a successful advance.
func (s *Service) PinAdvance(ctx context.Context, path string) (PinAdvanceResult, error) {
	info, err := s.pinAdvanceFolder(path)
	result := PinAdvanceResult{PinnedVersion: info.PinnedVersion}
	if err != nil {
		return result, err
	}
	executable, err := desktopExecutable()
	if err != nil {
		return result, problem("pin_advance_failed", fmt.Sprintf("Pin advance executable stage failed: %v", err))
	}
	manifestURL, _ := s.env.Lookup("RZM_UPDATE_MANIFEST_URL")
	_, updateErr := s.install(vaultconfig.WithEnv(ctx, s.env), appupdate.Options{ExecutablePath: executable, WorkDir: info.Path, SetVersion: info.PinOffer.Available, Yes: true, ManifestURL: manifestURL})
	if updateErr != nil {
		return result, problem("pin_advance_failed", fmt.Sprintf("Pin advance failed: %v", updateErr))
	}
	updated, err := s.Inspect(info.Path)
	if err != nil {
		return result, problem("pin_advance_failed", fmt.Sprintf("Pin advance inspection stage failed: %v", err))
	}
	return PinAdvanceResult{PinnedVersion: updated.PinnedVersion}, nil
}

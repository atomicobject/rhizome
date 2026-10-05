package desktop

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/user"
	"path/filepath"

	"github.com/atomicobject/rhizome/pkg/app/repoexec"
	appruntime "github.com/atomicobject/rhizome/pkg/app/runtime"
	appupdate "github.com/atomicobject/rhizome/pkg/app/update"
	"github.com/atomicobject/rhizome/pkg/repositorytrust"
)

type FolderInfo struct {
	ID            string `json:"id"`
	Path          string `json:"path"`
	Name          string `json:"name"`
	Configured    bool   `json:"configured"`
	Trusted       bool   `json:"trusted"`
	TrustRequired bool   `json:"trustRequired"`
	Authority     string `json:"authority"`
	PinnedVersion string `json:"pinnedVersion"`
	// Executable is the development build a developer override selects.
	Executable string `json:"executable,omitempty"`
}

type Service struct {
	stateDir     string
	home         string
	trust        repositorytrust.Store
	ensure       func(context.Context, appruntime.EnsureOptions) (appruntime.EnsureResult, error)
	install      func(context.Context, appupdate.Options) (appupdate.Result, error)
	ensurePinned func(context.Context, appupdate.EnsureOptions) error
}

func New(stateDir string) (*Service, error) {
	if !filepath.IsAbs(stateDir) {
		return nil, problem("invalid_request", "The desktop state directory must be absolute.")
	}
	account, err := user.Current()
	if err != nil {
		return nil, err
	}
	store, err := repositorytrust.DefaultStore()
	if err != nil {
		return nil, err
	}
	return &Service{stateDir: stateDir, home: account.HomeDir, trust: store, ensure: appruntime.Ensure, install: appupdate.Run, ensurePinned: appupdate.EnsurePinnedBinary}, nil
}

func (s *Service) inspect(path string) (FolderInfo, repoexec.Plan, error) {
	var info FolderInfo
	if !filepath.IsAbs(path) {
		return info, repoexec.Plan{}, problem("invalid_request", "Choose an absolute folder path.")
	}
	canonical, err := repositorytrust.CanonicalCheckout(path)
	if err != nil {
		return info, repoexec.Plan{}, problem("folder_missing", "This folder is unavailable. Restore it or choose another folder.")
	}
	stat, err := os.Stat(canonical)
	if err != nil || !stat.IsDir() {
		return info, repoexec.Plan{}, problem("folder_missing", "The selected path is not a folder.")
	}
	plan, err := repoexec.Select(canonical, filepath.Join(s.stateDir, "unselected-rzm"))
	if err != nil {
		return info, plan, err
	}
	root := plan.ConfigDir
	if root == "" {
		root = plan.TrustRoot()
	}
	if root != "" {
		canonical, err = repositorytrust.CanonicalCheckout(root)
		if err != nil {
			return info, plan, err
		}
	}
	digest := sha256.Sum256([]byte(canonical))
	info = FolderInfo{ID: hex.EncodeToString(digest[:]), Path: canonical, Name: filepath.Base(canonical), Configured: plan.Config != nil, Authority: plan.Authority, PinnedVersion: plan.PinnedVersion}
	if plan.Authority == "development" {
		info.Executable = plan.Target
	}
	root = plan.TrustRoot()
	if root != "" {
		info.Trusted, err = s.trust.Trusted(root)
		if err != nil {
			return info, plan, err
		}
		info.TrustRequired = !info.Trusted
	}
	return info, plan, nil
}

func (s *Service) Inspect(path string) (FolderInfo, error) {
	info, _, err := s.inspect(path)
	return info, err
}

func (s *Service) Trust(path string) (FolderInfo, error) {
	info, plan, err := s.inspect(path)
	if err != nil {
		return info, err
	}
	if filepath.Clean(path) != info.Path {
		return info, problem("invalid_request", fmt.Sprintf("Confirm the canonical folder %s before trusting it.", info.Path))
	}
	root := plan.TrustRoot()
	if root != "" {
		if _, err = s.trust.Trust(root); err != nil {
			return info, err
		}
	}
	return s.Inspect(info.Path)
}

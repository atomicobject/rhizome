package repoexec

import (
	"errors"
	"path/filepath"

	appupdate "github.com/atomicobject/rhizome/pkg/app/update"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// Plan is an inert selection of the executable authority for a checkout.
// Resolving it never probes executables, installs files, or grants trust.
type Plan struct {
	Authority             string
	Target                string
	ConfigDir             string
	Config                *obsidian.LocalConfig
	PinnedVersion         string
	SourceRoot            string
	PreferDevelopment     bool
	ConfiguredDevelopment bool
}

// Select applies the same external, configured-development, source-development,
// and managed-pin precedence used by command dispatch. fallback is an absolute
// global executable, or a non-executable placeholder when only inspecting.
func Select(cwd, fallback string) (Plan, error) {
	p := Plan{Authority: "global", Target: fallback}
	cfgDir, cfg, err := obsidian.FindLocalConfigForDelegation(cwd)
	if err != nil && !errors.Is(err, obsidian.ErrNoLocalConfig) {
		return p, err
	}
	if err == nil {
		p.ConfigDir, p.Config = cfgDir, cfg
		external, err := cfg.Rhizome.UsesExternalBinaryManager()
		if err != nil {
			return p, err
		}
		if external {
			p.Authority = "external"
			p.Target = ""
			return p, nil
		}
	}
	root, source := FindSourceRoot(cwd)
	p.SourceRoot = root
	if source && isGoRunExecutable(fallback) {
		return p, nil
	}
	if target, ok := configuredDevBinaryTarget(cfgDir, p.Config); ok {
		p.Authority, p.Target, p.ConfiguredDevelopment = "development", target, true
		return p, nil
	}
	p.PreferDevelopment = source && (p.Config == nil || SamePath(root, cfgDir))
	if p.PreferDevelopment {
		target, ok, err := rhizomeDevBinaryAt(root)
		if err != nil {
			return p, err
		}
		if ok {
			p.Authority, p.Target = "development", target
			return p, nil
		}
	}
	if p.Config != nil {
		invocation, err := appupdate.ResolveInvocation(fallback, cfgDir, p.Config)
		if err != nil {
			return p, err
		}
		if invocation.RepoPinned {
			p.Authority, p.Target, p.PinnedVersion = "managed", invocation.RepoTargetPath, invocation.RepoPinVersion
		}
	}
	return p, nil
}

// TrustRoot identifies the checkout authorizing repository-selected code.
func (p Plan) TrustRoot() string {
	if p.Authority != "managed" && p.Authority != "development" {
		return ""
	}
	if p.ConfigDir != "" {
		return p.ConfigDir
	}
	return p.SourceRoot
}

func absoluteExecutable(path string) (string, error) { return filepath.Abs(path) }

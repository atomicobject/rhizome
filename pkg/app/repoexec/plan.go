package repoexec

import (
	"errors"
	"os"
	"path/filepath"

	appupdate "github.com/atomicobject/rhizome/pkg/app/update"
	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
)

// Plan is an inert selection of the executable authority for a checkout.
// Resolving it never probes executables, installs files, or grants trust.
type Plan struct {
	Authority             string
	Target                string
	ConfigDir             string
	Config                *repoconfig.LocalConfig
	PinnedVersion         string
	SourceRoot            string
	PreferDevelopment     bool
	ConfiguredDevelopment bool
	// DevApp marks a development Target that is an installed Rhizome Dev
	// app's rzm. The repository did not choose that binary, so running it
	// needs no checkout trust. CLI dispatch still asks for trust in a pinned
	// checkout (see TrustRequirement), because commands such as init and
	// update act on the pin whichever binary runs them.
	DevApp bool
}

// Select applies the same external, configured-development, source-development
// (the checkout's bin/<goos>/rzm, else an installed Rhizome Dev app's rzm),
// and managed-pin precedence used by command dispatch. fallback is an absolute
// global executable, or a non-executable placeholder when only inspecting.
// It reads the process environment; see SelectWithEnv.
func Select(cwd, fallback string) (Plan, error) {
	home, _ := os.UserHomeDir()
	return SelectWithEnv(vaultconfig.Env{HomeDir: home, LookupEnv: os.LookupEnv}, cwd, fallback)
}

// SelectWithEnv is Select for a caller with an explicit invocation
// environment, whose home directory and variables locate the Dev app.
func SelectWithEnv(env vaultconfig.Env, cwd, fallback string) (Plan, error) {
	p := Plan{Authority: "global", Target: fallback}
	cfgDir, cfg, err := repoconfig.FindLocalConfigForDelegation(cwd)
	if err != nil && !errors.Is(err, repoconfig.ErrNoLocalConfig) {
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
		// WHY: without its own build, a source checkout runs the Dev app's
		// build rather than a release, so the app and terminal agree.
		if target, ok := DevAppExecutable(env); ok {
			p.Authority, p.Target, p.DevApp = "development", target, true
		}
	}
	if p.Config != nil {
		invocation, err := appupdate.ResolveInvocation(fallback, cfgDir, p.Config)
		if err != nil {
			return p, err
		}
		if invocation.RepoPinned {
			p.PinnedVersion = invocation.RepoPinVersion
			if !p.DevApp {
				p.Authority, p.Target = "managed", invocation.RepoTargetPath
			}
		}
	}
	return p, nil
}

// TrustRoot identifies the checkout authorizing repository-selected code. The
// Dev app's rzm is app-owned, so a plan that runs it has none.
func (p Plan) TrustRoot() string {
	if p.Authority != "managed" && p.Authority != "development" || p.DevApp {
		return ""
	}
	if p.ConfigDir != "" {
		return p.ConfigDir
	}
	return p.SourceRoot
}

func absoluteExecutable(path string) (string, error) { return filepath.Abs(path) }

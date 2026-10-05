package repoexec

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	appupdate "github.com/atomicobject/rhizome/pkg/app/update"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

const (
	repoDelegatedEnv    = "RZM_REPO_DELEGATED"
	repoSkipDelegateEnv = "RZM_SKIP_REPO_DELEGATE"
)

const (
	BootstrapReasonMissing  = "binary missing"
	BootstrapReasonMismatch = "version mismatch"
)

type Decision struct {
	Delegate bool
	Warning  string
	// Bootstrap means the repo pins a version but the repo-local binary is
	// missing or mismatched: install the pin, then delegate to Target.
	Bootstrap     bool
	Target        string
	PinnedVersion string
	Reason        string
	Err           error

	ConfigDir string
	Config    *obsidian.LocalConfig
}

func DecisionFor(args []string, cwd, exe string) Decision {
	plan, err := Select(cwd, exe)
	if err != nil {
		return Decision{Err: err}
	}
	cfgDir, cfg := plan.ConfigDir, plan.Config
	hasConfig := cfg != nil
	warning := ""
	if hasConfig {
		loaded, validationErr := obsidian.LoadLocalConfig(cfgDir)
		if validationErr != nil {
			warning = fmt.Sprintf("global Rhizome could not validate repo config while selecting the repo binary: %v; the selected binary will validate the config", validationErr)
		} else if len(loaded.Warnings) > 0 {
			keys := make([]string, 0)
			for _, configWarning := range loaded.Warnings {
				keys = append(keys, configWarning.OffendingKeys...)
			}
			sort.Strings(keys)
			warning = fmt.Sprintf("global Rhizome does not recognize config key(s) %s; the selected repo binary will validate them", strings.Join(keys, ", "))
			if containsLegacyWorkflowConfigKey(keys) {
				warning += "; run `rzm init` to migrate recognized v0.49 workflow settings"
			}
		}
	}
	if hasConfig {
		externallyManaged, err := cfg.Rhizome.UsesExternalBinaryManager()
		if err != nil {
			return Decision{Err: err}
		}
		if externallyManaged {
			// WHY: the external manager selected the executable that is already
			// running; probing source builds or repo caches would add a second owner.
			return Decision{Warning: warning}
		}
	}
	if isExplicitExternalBinaryManagerMigration(args) {
		// WHY: the current executable owns this explicit transition. Delegating or
		// bootstrapping first can select a binary that does not support the flag.
		return Decision{Warning: warning}
	}
	if isRepositoryTrustControlInvocation(args) {
		return Decision{Warning: warning}
	}

	if plan.Authority == "development" {
		targetAbs, err := filepath.Abs(plan.Target)
		if err != nil {
			return Decision{Err: err}
		}
		exeAbs, err := filepath.Abs(exe)
		if err != nil {
			return Decision{Err: err}
		}
		if SameExecutablePath(exeAbs, targetAbs) {
			return Decision{Warning: warning}
		}
		if plan.ConfiguredDevelopment {
			if err := requireConfiguredDevBinary(targetAbs); err != nil {
				return Decision{Err: err}
			}
		}
		return Decision{Delegate: true, Target: targetAbs, Warning: warning}
	}
	if plan.PreferDevelopment && isRepoDelegateBypassInvocation(args) {
		return Decision{Warning: warning}
	}
	if isUpdateControlPlaneInvocation(args) {
		return Decision{Warning: warning}
	}
	if plan.Authority != "managed" {
		return Decision{Warning: warning}
	}
	invocation, err := appupdate.ResolveInvocation(exe, cfgDir, cfg)
	if err != nil {
		return Decision{Err: err}
	}
	if !invocation.RepoPinned {
		return Decision{Warning: warning}
	}
	if invocation.RepoScoped {
		return Decision{Warning: warning}
	}
	targetAbs := invocation.RepoTargetPath
	pin := invocation.RepoPinVersion
	bootstrap := func(reason string) Decision {
		return Decision{
			Bootstrap:     true,
			Warning:       warning,
			Target:        targetAbs,
			PinnedVersion: pin,
			Reason:        reason,
			ConfigDir:     cfgDir,
			Config:        cfg,
		}
	}
	if _, err := os.Stat(targetAbs); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return bootstrap(BootstrapReasonMissing)
		}
		return Decision{Err: err}
	}
	installed, ok := appupdate.ReadVersionMarker(targetAbs)
	if !ok {
		// Legacy installs predate the marker: probe once, then record it.
		probed, err := ProbeVersion(targetAbs)
		if err != nil {
			return bootstrap(BootstrapReasonMismatch)
		}
		// Best effort: a failed marker write just means another probe later.
		_ = appupdate.WriteVersionMarker(targetAbs, probed)
		installed = probed
	}
	if installed != pin {
		return bootstrap(BootstrapReasonMismatch)
	}
	return Decision{Delegate: true, Target: targetAbs, Warning: warning}
}

func containsLegacyWorkflowConfigKey(keys []string) bool {
	for _, key := range keys {
		switch key {
		case "workflowTemplates", "workflowTemplateAddons", "workflowTemplateManagement":
			return true
		}
	}
	return false
}

// ProbeVersion runs the repo-local binary once with --version (guarded
// against re-delegation) and parses the reported version.
func ProbeVersion(target string) (string, error) {
	out, err := Probe(context.Background(), target, "", "--version")
	if err != nil {
		return "", err
	}
	version := ParseVersionOutput(string(out))
	if version == "" {
		return "", fmt.Errorf("selected executable did not report a version")
	}
	return version, nil
}

func ParseVersionOutput(out string) string {
	for _, field := range strings.Fields(out) {
		candidate := strings.TrimPrefix(field, "v")
		if candidate == "" || candidate[0] < '0' || candidate[0] > '9' {
			continue
		}
		if strings.Contains(candidate, ".") {
			return appupdate.NormalizeVersion(field)
		}
	}
	return ""
}

// Bootstrap installs the repo-pinned binary for a Bootstrap decision.
func Bootstrap(ctx context.Context, decision Decision, progress io.Writer) error {
	return appupdate.EnsurePinnedBinary(ctx, appupdate.EnsureOptions{
		CfgDir:   decision.ConfigDir,
		Config:   decision.Config,
		Progress: progress,
	})
}

func rhizomeDevBinaryAt(root string) (string, bool, error) {
	target := filepath.Join(root, "bin", runtime.GOOS, ExecutableName())
	info, err := os.Stat(target)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", false, nil
		}
		return "", false, err
	}
	if info.IsDir() {
		return "", false, nil
	}
	if runtime.GOOS != "windows" && info.Mode()&0o111 == 0 {
		return "", false, nil
	}
	return target, true, nil
}

func configuredDevBinaryTarget(cfgDir string, cfg *obsidian.LocalConfig) (string, bool) {
	if cfg == nil {
		return "", false
	}
	devDir := strings.TrimSpace(cfg.Rhizome.DevBinaryDir)
	if devDir == "" {
		return "", false
	}
	if filepath.IsAbs(devDir) {
		return filepath.Join(devDir, runtime.GOOS, ExecutableName()), true
	}
	return filepath.Join(cfgDir, devDir, runtime.GOOS, ExecutableName()), true
}

func requireConfiguredDevBinary(target string) error {
	info, err := os.Stat(target)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("configured rhizome.devBinaryDir target missing: %s; run make build", target)
		}
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("configured rhizome.devBinaryDir target is a directory: %s", target)
	}
	if runtime.GOOS != "windows" && info.Mode()&0o111 == 0 {
		return fmt.Errorf("configured rhizome.devBinaryDir target is not executable: %s", target)
	}
	return nil
}

func FindSourceRoot(cwd string) (string, bool) {
	if root, ok := findRhizomeSourceRootFrom(filepath.Clean(cwd)); ok {
		return root, true
	}
	abs, err := filepath.Abs(cwd)
	if err == nil {
		return findRhizomeSourceRootFrom(abs)
	}
	return "", false
}

func findRhizomeSourceRootFrom(dir string) (string, bool) {
	for {
		goMod := filepath.Join(dir, "go.mod")
		if data, err := os.ReadFile(goMod); err == nil && hasRhizomeModuleLine(string(data)) {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

func hasRhizomeModuleLine(goMod string) bool {
	for _, line := range strings.Split(goMod, "\n") {
		if strings.TrimSpace(line) == "module github.com/atomicobject/rhizome" {
			return true
		}
	}
	return false
}

func SamePath(a, b string) bool {
	aAbs, aErr := filepath.Abs(a)
	bAbs, bErr := filepath.Abs(b)
	if aErr == nil {
		a = aAbs
	}
	if bErr == nil {
		b = bAbs
	}
	if aInfo, aStatErr := os.Stat(a); aStatErr == nil {
		if bInfo, bStatErr := os.Stat(b); bStatErr == nil && os.SameFile(aInfo, bInfo) {
			return true
		}
	}
	if runtime.GOOS == "windows" {
		return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

func PlatformDir() string {
	return runtime.GOOS + "-" + runtime.GOARCH
}

func ExecutableName() string {
	if runtime.GOOS == "windows" {
		return "rzm.exe"
	}
	return "rzm"
}

func isGoRunExecutable(exe string) bool {
	exe = filepath.Clean(exe)
	hasExeDir := filepath.Base(filepath.Dir(exe)) == "exe"
	for dir := filepath.Dir(exe); ; dir = filepath.Dir(dir) {
		base := filepath.Base(dir)
		if hasExeDir && strings.HasPrefix(base, "go-build") {
			return true
		}
		if strings.Contains(base, "go-build") {
			return true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return false
		}
	}
}

func isUpdateControlPlaneInvocation(args []string) bool {
	return FirstCommandArg(args) == "update"
}

func isExplicitExternalBinaryManagerMigration(args []string) bool {
	if FirstCommandArg(args) != "init" {
		return false
	}
	for i, arg := range args {
		if arg == "--binary-manager" && i+1 < len(args) {
			return strings.TrimSpace(args[i+1]) == obsidian.BinaryManagerExternal
		}
		if value, ok := strings.CutPrefix(arg, "--binary-manager="); ok {
			return strings.TrimSpace(value) == obsidian.BinaryManagerExternal
		}
	}
	return false
}

func isRepoDelegateBypassInvocation(args []string) bool {
	return FirstCommandArg(args) == "new-worktree"
}

// FirstCommandArg identifies the command before Cobra parses argv. Dispatch must
// make this small amount of persistent/pre-command flag grammar explicit so an
// update entered through the global executable stays on its control plane.
// Boolean flags need no registry: they are skipped as individual dash-prefixed
// arguments. Value-taking flags must be listed by flagConsumesValue, otherwise
// their value would be mistaken for the command.
func FirstCommandArg(args []string) string {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			return ""
		}
		if !strings.HasPrefix(arg, "-") {
			return arg
		}
		if flagConsumesValue(arg) && !strings.Contains(arg, "=") {
			i++
		}
	}
	return ""
}

func flagConsumesValue(arg string) bool {
	switch arg {
	case "--vault", "-v": // Persistent vault selection used by command families.
		return true
	case "--manifest-url": // Update manifest override; tolerate pre-command ordering.
		return true
	case "--set-version": // Exact update version; tolerate pre-command ordering.
		return true
	default:
		return false
	}
}

func SameExecutablePath(a, b string) bool {
	a = filepath.Clean(a)
	b = filepath.Clean(b)
	if aInfo, aErr := os.Stat(a); aErr == nil {
		if bInfo, bErr := os.Stat(b); bErr == nil && os.SameFile(aInfo, bInfo) {
			return true
		}
	}
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	aEval, aErr := filepath.EvalSymlinks(a)
	bEval, bErr := filepath.EvalSymlinks(b)
	if aErr == nil && bErr == nil {
		return aEval == bEval
	}
	return a == b
}

package update

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// Options controls a binary update run.
type Options struct {
	ManifestURL    string
	Version        string
	SetVersion     string
	Pinned         bool
	Latest         bool
	Yes            bool
	ExecutablePath string
	WorkDir        string
	Stdin          io.Reader
	Stdout         io.Writer
	HTTPClient     *http.Client
	deps           *runDependencies
}

// TargetResult reports which durable steps completed for one logical target.
// A result may be partially complete when a later metadata step fails.
type TargetResult struct {
	Role          UpdateTargetRole
	Path          string
	Version       string
	BinaryUpdated bool
	MarkerWritten bool
	PinWritten    bool
}

// Result contains truthful per-target outcomes, including work completed
// before a later target or metadata step failed.
type Result struct {
	Targets []TargetResult
}

// Summaries renders completed binary replacements for human CLI output.
func (r Result) Summaries() []string {
	lines := make([]string, 0, len(r.Targets))
	for _, target := range r.Targets {
		if target.BinaryUpdated {
			lines = append(lines, fmt.Sprintf("Updated %s %s to Rhizome %s", target.Role, target.Path, target.Version))
		}
	}
	return lines
}

type preparedArtifact struct {
	path    string
	cleanup func()
}

type runDependencies struct {
	prepare     func(context.Context, *http.Client, string, Manifest, string) (preparedArtifact, error)
	replace     func(string, string) error
	writeMarker func(string, string) error
	saveConfig  func(string, obsidian.LocalConfig) error
}

func defaultRunDependencies() runDependencies {
	return runDependencies{
		prepare:     prepareVersionArtifact,
		replace:     replaceFile,
		writeMarker: WriteVersionMarker,
		saveConfig:  obsidian.SaveLocalConfig,
	}
}

// Run updates the selected Rhizome installations according to an explicit,
// ordered plan. All required artifacts are prepared before the first mutation.
func Run(ctx context.Context, opts Options) (Result, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if opts.Stdout == nil {
		opts.Stdout = io.Discard
	}
	mode, setVersion, err := updateMode(opts)
	if err != nil {
		return Result{}, err
	}
	cfgDir, localCfg, err := findRepoConfig(opts.WorkDir)
	if err != nil {
		return Result{}, err
	}
	if localCfg != nil {
		externallyManaged, err := localCfg.Rhizome.UsesExternalBinaryManager()
		if err != nil {
			return Result{}, err
		}
		if externallyManaged {
			// WHY: self-update would compete with the external tool that owns the
			// repository's Rhizome version and executable selection.
			return Result{}, fmt.Errorf("Rhizome is externally managed for this repository; change the configured version through the external binary manager")
		}
	}
	invocation, err := ResolveInvocation(opts.ExecutablePath, cfgDir, localCfg)
	if err != nil {
		return Result{}, err
	}
	// Validate repo-only modes before any network or filesystem mutation.
	if (mode == UpdateModePinned || mode == UpdateModeSetVersion) && !invocation.RepoPinned {
		if mode == UpdateModePinned {
			return Result{}, fmt.Errorf("--pinned requires rhizome.version in .rhizome/config.yml")
		}
		return Result{}, fmt.Errorf("--set-version requires rhizome.version in .rhizome/config.yml")
	}

	var latest Manifest
	if mode == UpdateModeDefault || mode == UpdateModeLatest {
		latest, err = FetchManifest(opts.HTTPClient, opts.ManifestURL)
		if err != nil {
			return Result{}, stageErr(StageManifest, err)
		}
	}
	advancePin := false
	if mode == UpdateModeDefault && invocation.RepoPinned && NormalizeVersion(invocation.RepoPinVersion) != NormalizeVersion(latest.Version) {
		if opts.Yes {
			advancePin = true
		} else {
			fmt.Fprintf(opts.Stdout, "Repo target %s. ", invocation.RepoTargetPath)
			advancePin = promptAdvancePin(opts.Stdin, opts.Stdout, invocation.RepoPinVersion, latest.Version)
		}
	}
	plan, err := BuildUpdatePlan(invocation, UpdatePlanOptions{
		LatestVersion: latest.Version,
		Mode:          mode,
		SetVersion:    setVersion,
		AdvancePin:    advancePin,
	})
	if err != nil {
		return Result{}, err
	}

	deps := defaultRunDependencies()
	if opts.deps != nil {
		deps = *opts.deps
	}
	prepared := make(map[string]preparedArtifact)
	defer func() {
		for _, artifact := range prepared {
			if artifact.cleanup != nil {
				artifact.cleanup()
			}
		}
	}()
	for _, target := range plan.Targets {
		if _, ok := prepared[target.Version]; ok {
			continue
		}
		artifact, prepErr := deps.prepare(ctx, opts.HTTPClient, opts.ManifestURL, latest, target.Version)
		if prepErr != nil {
			return Result{}, targetError(target, "prepare", prepErr)
		}
		prepared[target.Version] = artifact
	}

	result := Result{Targets: make([]TargetResult, 0, len(plan.Targets))}
	for _, target := range plan.Targets {
		outcome := TargetResult{Role: target.Role, Path: target.LogicalPath, Version: target.Version}
		if err := deps.replace(prepared[target.Version].path, target.LogicalPath); err != nil {
			result.Targets = append(result.Targets, outcome)
			return result, targetError(target, StageReplace, err)
		}
		outcome.BinaryUpdated = true
		result.Targets = append(result.Targets, outcome)
		current := &result.Targets[len(result.Targets)-1]
		if target.WriteMarker {
			if err := deps.writeMarker(target.LogicalPath, target.Version); err != nil {
				return result, targetError(target, "marker", err)
			}
			current.MarkerWritten = true
		}
		if target.WritePin {
			if cfgDir == "" || localCfg == nil {
				return result, targetError(target, "pin", fmt.Errorf("repo config unavailable"))
			}
			updated := *localCfg
			updated.Rhizome.Version = target.Version
			if err := deps.saveConfig(cfgDir, updated); err != nil {
				if repoPinMatches(cfgDir, target.Version) {
					localCfg.Rhizome.Version = target.Version
					current.PinWritten = true
				}
				return result, targetError(target, "pin", err)
			}
			localCfg.Rhizome.Version = target.Version
			current.PinWritten = true
		}
	}
	return result, nil
}

func updateMode(opts Options) (UpdateMode, string, error) {
	setVersion := strings.TrimSpace(firstNonEmpty(opts.SetVersion, opts.Version))
	selected := 0
	if opts.Pinned {
		selected++
	}
	if opts.Latest {
		selected++
	}
	if setVersion != "" {
		selected++
	}
	if selected > 1 {
		return "", "", fmt.Errorf("--pinned, --latest, and --set-version are mutually exclusive")
	}
	if opts.Pinned {
		return UpdateModePinned, "", nil
	}
	if opts.Latest {
		return UpdateModeLatest, "", nil
	}
	if setVersion != "" {
		return UpdateModeSetVersion, setVersion, nil
	}
	return UpdateModeDefault, "", nil
}

func targetError(target UpdateTarget, stage string, err error) error {
	return fmt.Errorf("%s target %s version %s %s stage: %w", target.Role, target.LogicalPath, target.Version, stage, err)
}

func repoPinMatches(cfgDir, version string) bool {
	foundDir, cfg, err := obsidian.FindLocalConfigForDelegation(cfgDir)
	if err != nil || foundDir != cfgDir || cfg == nil {
		return false
	}
	return NormalizeVersion(cfg.Rhizome.Version) == NormalizeVersion(version)
}

func findRepoConfig(workDir string) (string, *obsidian.LocalConfig, error) {
	if strings.TrimSpace(workDir) == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", nil, err
		}
		workDir = cwd
	}
	cfgDir, cfg, err := obsidian.FindLocalConfig(workDir)
	if err != nil {
		if errors.Is(err, obsidian.ErrNoLocalConfig) {
			return "", nil, nil
		}
		return "", nil, err
	}
	return cfgDir, cfg, nil
}

// targetPath resolves the install destination. repoTarget reports whether the
// destination is a repo-local binary (which carries a version marker) rather
// than the global executable.
func targetPath(executablePath, cfgDir string, cfg *obsidian.LocalConfig) (target string, repoTarget bool, err error) {
	invocation, err := ResolveInvocation(executablePath, cfgDir, cfg)
	if err != nil {
		return "", false, err
	}
	if invocation.RepoPinned {
		return invocation.RepoTargetPath, true, nil
	}
	return invocation.ExecutablePath, false, nil
}

func CurrentPlatformDir() string {
	return runtime.GOOS + "-" + runtime.GOARCH
}

func executableName() string {
	if runtime.GOOS == "windows" {
		return "rzm.exe"
	}
	return "rzm"
}

func installArtifact(ctx context.Context, client *http.Client, artifact Artifact, target string) error {
	prepared, err := prepareArtifact(ctx, client, artifact)
	if err != nil {
		return err
	}
	defer prepared.cleanup()
	return stageErr(StageReplace, replaceFile(prepared.path, target))
}

func prepareVersionArtifact(ctx context.Context, client *http.Client, manifestURL string, latest Manifest, version string) (preparedArtifact, error) {
	manifest := latest
	if NormalizeVersion(version) != NormalizeVersion(latest.Version) {
		var err error
		manifest, err = FetchVersionManifest(client, manifestURL, version)
		if err != nil {
			return preparedArtifact{}, stageErr(StageManifest, err)
		}
	}
	artifact, err := manifest.ArtifactFor(CurrentPlatformKey())
	if err != nil {
		return preparedArtifact{}, stageErr(StageManifest, err)
	}
	return prepareArtifact(ctx, client, artifact)
}

func prepareArtifact(ctx context.Context, client *http.Client, artifact Artifact) (preparedArtifact, error) {
	if client == nil {
		client = defaultHTTPClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, artifact.URL, nil)
	if err != nil {
		return preparedArtifact{}, stageErr(StageDownload, err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return preparedArtifact{}, stageErr(StageDownload, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return preparedArtifact{}, stageErr(StageDownload, fmt.Errorf("download artifact: %s", resp.Status))
	}

	tmpDir, err := os.MkdirTemp("", "rzm-update-*")
	if err != nil {
		return preparedArtifact{}, stageErr(StageDownload, err)
	}
	cleanup := func() { _ = os.RemoveAll(tmpDir) }
	archivePath := filepath.Join(tmpDir, "rzm.tar.gz")
	if err := writeAndVerify(resp.Body, archivePath, artifact.SHA256); err != nil {
		cleanup()
		return preparedArtifact{}, stageErr(StageDownload, err)
	}
	extracted, err := extractRZM(archivePath, tmpDir)
	if err != nil {
		cleanup()
		return preparedArtifact{}, stageErr(StageExtract, err)
	}
	if err := smokeTest(ctx, extracted); err != nil {
		cleanup()
		return preparedArtifact{}, stageErr(StageSmokeTest, err)
	}
	return preparedArtifact{path: extracted, cleanup: cleanup}, nil
}

func writeAndVerify(r io.Reader, path, expected string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	hash := sha256.New()
	_, copyErr := io.Copy(io.MultiWriter(f, hash), r)
	closeErr := f.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	actual := hex.EncodeToString(hash.Sum(nil))
	if !strings.EqualFold(actual, strings.TrimSpace(expected)) {
		return stageErr(StageChecksum, fmt.Errorf("sha256 mismatch: expected %s, got %s", expected, actual))
	}
	return nil
}

func extractRZM(archivePath, destDir string) (string, error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return "", err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return "", err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	want := "rzm"
	if runtime.GOOS == "windows" {
		want = "rzm.exe"
	}
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", err
		}
		if filepath.Base(header.Name) != want {
			continue
		}
		out := filepath.Join(destDir, want)
		if err := writeExecutable(out, tr); err != nil {
			return "", err
		}
		return out, nil
	}
	return "", fmt.Errorf("archive missing %s", want)
}

func writeExecutable(path string, r io.Reader) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(f, r)
	closeErr := f.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	return nil
}

func smokeTest(ctx context.Context, path string) error {
	cmd := exec.CommandContext(ctx, path, "--version")
	cmd.Dir = filepath.Dir(path)
	cmd.Env = envWithOverride(os.Environ(), "RZM_SKIP_REPO_DELEGATE", "1")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func envWithOverride(environ []string, key, value string) []string {
	prefix := key + "="
	result := make([]string, 0, len(environ)+1)
	for _, entry := range environ {
		if !strings.HasPrefix(entry, prefix) {
			result = append(result, entry)
		}
	}
	return append(result, prefix+value)
}

func replaceFile(src, target string) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(target), ".rzm-update-*")
	if err != nil {
		return err
	}
	staged := tmp.Name()
	if err := tmp.Close(); err != nil {
		_ = os.Remove(staged)
		return err
	}
	defer func() { _ = os.Remove(staged) }()
	if err := copyFile(src, staged, 0o755); err != nil {
		return err
	}
	return os.Rename(staged, target)
}

func copyFile(src, dst string, perm os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, perm)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Chmod(dst, perm)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

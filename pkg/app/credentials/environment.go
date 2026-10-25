package credentials

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
)

const ImportTimeout = 90 * time.Second
const maxCaptureBytes = 64 << 10

var environmentID = regexp.MustCompile(`^[a-z0-9]{26}$`)
var stableVersion = regexp.MustCompile(`^(\d+)\.(\d+)\.(\d+)$`)

// Capture is an early executable route. It reads only explicitly supported keys,
// before normal dotenv, configuration, logging, or repository dispatch runs.
func Capture(args []string, lookup func(string) (string, bool), output io.Writer) (bool, int) {
	if len(args) == 0 || args[0] != "credentials-capture" {
		return false, 0
	}
	keys, err := validateKeys(args[1:])
	if err != nil {
		return true, 1
	}
	values := make(map[string]string, len(keys))
	for _, key := range keys {
		value, _ := lookup(key)
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if len(value) > 8192 {
			return true, 1
		}
		values[key] = value
	}
	data, err := json.Marshal(values)
	if err != nil || len(data) > maxCaptureBytes {
		return true, 1
	}
	if _, err := output.Write(data); err != nil {
		return true, 1
	}
	return true, 0
}

func validateKeys(keys []string) ([]string, error) {
	if len(keys) == 0 {
		return nil, fmt.Errorf("select at least one supported provider key")
	}
	seen := map[string]bool{}
	for _, key := range keys {
		if !IsSupportedKey(key) {
			return nil, fmt.Errorf("select only supported provider keys")
		}
		if seen[key] {
			return nil, fmt.Errorf("select each provider key only once")
		}
		seen[key] = true
	}
	return keys, nil
}

func ValidateSource(source repoconfig.CredentialSource) error {
	if strings.TrimSpace(source.Account) == "" || strings.HasPrefix(source.Account, "-") || strings.ContainsAny(source.Account, "\x00\r\n") {
		return fmt.Errorf("enter your 1Password account sign-in address or account ID")
	}
	if !environmentID.MatchString(source.Environment) {
		return fmt.Errorf("enter the 26-character Environment ID from 1Password's Manage environment menu")
	}
	_, err := validateKeys(source.Keys)
	return err
}

// FindOnePassword includes native GUI installation paths, without running a shell.
func FindOnePassword(env vaultconfig.Env) string {
	path, _ := env.Lookup("PATH")
	dirs := filepath.SplitList(path)
	if env.HomeDir != "" {
		dirs = append(dirs, filepath.Join(env.HomeDir, ".local", "bin"))
	}
	dirs = append(dirs, "/opt/homebrew/bin", "/usr/local/bin")
	name := "op"
	if filepath.Separator == '\\' {
		name = "op.exe"
	}
	for _, dir := range dirs {
		if !filepath.IsAbs(dir) {
			continue
		}
		candidate := filepath.Join(dir, name)
		info, err := os.Stat(candidate)
		if err == nil && info.Mode().IsRegular() && (filepath.Separator == '\\' || info.Mode()&0111 != 0) {
			return candidate
		}
	}
	return ""
}

type boundedCapture struct{ data []byte }

func (b *boundedCapture) Write(p []byte) (int, error) {
	if len(b.data)+len(p) > maxCaptureBytes {
		return 0, fmt.Errorf("capture limit exceeded")
	}
	b.data = append(b.data, p...)
	return len(p), nil
}

// run keeps subprocess output private and bounded. Ambient provider values and
// secret references cannot become fallbacks when an Environment omits a key.
func runOnePassword(ctx context.Context, binary string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, binary, args...)
	configureImportProcess(command)
	command.Env = []string{}
	for _, entry := range os.Environ() {
		key, value, _ := strings.Cut(entry, "=")
		if IsSupportedKey(key) || key == AtomicRhizomeKey || key == "OP_DEBUG" || strings.HasPrefix(value, "op://") {
			continue
		}
		command.Env = append(command.Env, entry)
	}
	command.WaitDelay = time.Second
	var output boundedCapture
	command.Stdout = &output
	command.Stderr = io.Discard
	if err := command.Run(); err != nil {
		return nil, err
	}
	return output.data, nil
}

func (c OnePasswordCLI) run(ctx context.Context, args ...string) ([]byte, error) {
	binary := c.Binary
	if binary == "" {
		binary = FindOnePassword(vaultconfig.FromContext(ctx))
	}
	if binary == "" {
		return nil, fmt.Errorf("install 1Password CLI 2.41 or newer, then reopen Rhizome")
	}
	run := c.Run
	if run == nil {
		run = runOnePassword
	}
	result, err := run(ctx, binary, args...)
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("1Password import was canceled or timed out; unlock 1Password and try again")
		}
		return nil, fmt.Errorf("1Password could not read the requested credentials; unlock the app, sign in to the selected account, enable CLI integration, and check read access")
	}
	return result, nil
}

// ReadEnvironment uses stable CLI 2.41+, transporting only requested keys through
// a private pipe from this executable. No credential enters an argument or error.
func (c OnePasswordCLI) ReadEnvironment(ctx context.Context, source repoconfig.CredentialSource) (map[string]string, error) {
	ctx, cancel := context.WithTimeout(ctx, ImportTimeout)
	defer cancel()
	if err := ValidateSource(source); err != nil {
		return nil, err
	}
	version, err := c.run(ctx, "--version")
	if err != nil {
		return nil, err
	}
	parts := stableVersion.FindStringSubmatch(strings.TrimSpace(string(version)))
	var major, minor, patch int
	if len(parts) != 4 {
		return nil, fmt.Errorf("install stable 1Password CLI 2.41 or newer; beta builds are unsupported")
	}
	_, _ = fmt.Sscanf(string(version), "%d.%d.%d", &major, &minor, &patch)
	if major < 2 || (major == 2 && minor < 41) {
		return nil, fmt.Errorf("update to stable 1Password CLI 2.41 or newer for Environments")
	}
	executable, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("cannot locate Rhizome for 1Password import")
	}
	args := []string{"run", "--account", source.Account, "--environment", source.Environment, "--no-masking", "--", executable, "credentials-capture"}
	args = append(args, source.Keys...)
	output, err := c.run(ctx, args...)
	if err != nil {
		return nil, err
	}
	var values map[string]string
	if len(output) > maxCaptureBytes || json.Unmarshal(output, &values) != nil {
		return nil, fmt.Errorf("1Password returned invalid credentials; check the Environment and try again")
	}
	requested := make(map[string]string, len(source.Keys))
	for _, key := range source.Keys {
		value := strings.TrimSpace(values[key])
		if value == "" || len(value) > 8192 {
			return nil, fmt.Errorf("1Password Environment is missing %s; add it or select a different Environment", key)
		}
		requested[key] = value
	}
	return requested, nil
}

func ImportEnvironment(ctx context.Context, source repoconfig.CredentialSource) (int, error) {
	return importEnvironment(ctx, OnePasswordCLI{}, source)
}

func importEnvironment(ctx context.Context, reader OnePasswordCLI, source repoconfig.CredentialSource) (int, error) {
	values, err := reader.ReadEnvironment(ctx, source)
	if err != nil {
		return 0, err
	}
	cfg, err := repoconfig.LoadCliConfig(vaultconfig.FromContext(ctx), true)
	if err != nil {
		return 0, fmt.Errorf("cannot load user configuration; repair it before importing")
	}
	if cfg.Env == nil {
		cfg.Env = map[string]string{}
	}
	for key, value := range values {
		cfg.Env[key] = value
		delete(cfg.CredentialSkips, key)
	}
	cfg.CredentialSource = &source
	if err := ctx.Err(); err != nil {
		return 0, fmt.Errorf("1Password import was canceled or timed out; try again")
	}
	if err := repoconfig.SaveCliConfig(vaultconfig.FromContext(ctx), cfg); err != nil {
		return 0, fmt.Errorf("cannot save private user configuration; check file permissions and try again")
	}
	return len(values), nil
}

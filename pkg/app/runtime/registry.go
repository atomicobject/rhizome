package runtime

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/fileio"
	"github.com/atomicobject/rhizome/pkg/paths"
	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
)

type InstanceManifest struct {
	InstanceID string `json:"instanceId"`
	VaultName  string `json:"vaultName"`
	VaultPath  string `json:"vaultPath"`
	PID        int    `json:"pid"`
	Version    string `json:"version"`
	// Mode, BuildID, Executable, and ControlToken are runtime-coordination
	// fields (SPEC-0104). The token is why the vault manifest is mode 0600.
	// RunID is random per process start. Probe matches it and the PID so a
	// reused PID or reused port can never pass for the manifest's runtime.
	RunID        string    `json:"runId,omitempty"`
	Mode         Mode      `json:"mode,omitempty"`
	BuildID      string    `json:"buildId,omitempty"`
	Executable   string    `json:"executable,omitempty"`
	ControlToken string    `json:"controlToken,omitempty"`
	HTTPHost     string    `json:"httpHost"`
	HTTPPort     int       `json:"httpPort"`
	HTTPURL      string    `json:"httpURL"`
	StartedAt    time.Time `json:"startedAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
	Ready        bool      `json:"ready"`
}

type Registry struct {
	dir string
}

func NewRegistry() (*Registry, error) {
	cliDir, _, err := vaultconfig.CliPath()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(cliDir, "instances")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &Registry{dir: dir}, nil
}

func InstanceID(vaultPath string) string {
	normalized := paths.ResolveSymlinks(vaultPath).String()
	if normalized == "" {
		normalized = filepath.Clean(vaultPath)
	}
	sum := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(sum[:8])
}

func (r *Registry) Path(instanceID string) string {
	return filepath.Join(r.dir, instanceID+".json")
}

func (r *Registry) Write(manifest InstanceManifest) error {
	if r == nil {
		return nil
	}
	manifest.UpdatedAt = time.Now().UTC()
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	target := r.Path(manifest.InstanceID)
	return writeFileReplace(target, data)
}

func (r *Registry) Remove(instanceID string) error {
	if r == nil {
		return nil
	}
	target := r.Path(instanceID)
	var errs []error
	if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
		errs = append(errs, err)
	}
	for _, pattern := range []string{target + ".tmp", target + ".*.tmp"} {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		for _, match := range matches {
			if err := os.Remove(match); err != nil && !os.IsNotExist(err) {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

func (r *Registry) PruneStale() error {
	if r == nil {
		return nil
	}
	entries, err := os.ReadDir(r.dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" || strings.HasSuffix(entry.Name(), ".pending.json") {
			continue
		}
		fullPath := filepath.Join(r.dir, entry.Name())
		var manifest InstanceManifest
		data, err := fileio.ReadFile(fullPath)
		if err != nil {
			continue
		}
		if err := json.Unmarshal(data, &manifest); err != nil {
			_ = os.Remove(fullPath)
			continue
		}
		if !pidExists(manifest.PID) {
			_ = os.Remove(fullPath)
		}
	}
	return nil
}

func (r *Registry) List() ([]InstanceManifest, error) {
	if r == nil {
		return nil, nil
	}
	if err := r.PruneStale(); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(r.dir)
	if err != nil {
		return nil, err
	}
	out := make([]InstanceManifest, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" || strings.HasSuffix(entry.Name(), ".pending.json") {
			continue
		}
		data, err := fileio.ReadFile(filepath.Join(r.dir, entry.Name()))
		if err != nil {
			continue
		}
		var manifest InstanceManifest
		if err := json.Unmarshal(data, &manifest); err != nil {
			continue
		}
		out = append(out, manifest)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].VaultName == out[j].VaultName {
			return out[i].VaultPath < out[j].VaultPath
		}
		return out[i].VaultName < out[j].VaultName
	})
	return out, nil
}

func FormatStartup(manifest InstanceManifest, warm bool) string {
	state := "ready"
	if warm {
		state = "warming"
	}
	return fmt.Sprintf("Rhizome serve\n  vault: %s\n  path:  %s\n  pid:   %d\n  url:   %s\n  state: %s\n",
		manifest.VaultName,
		manifest.VaultPath,
		manifest.PID,
		manifest.HTTPURL,
		state,
	)
}

// pidExists uses the lock package's platform-aware liveness check; a
// Windows PID needs a process handle, not FindProcess, to be trusted.
func pidExists(pid int) bool { return indexlock.PIDExists(pid) }

func writeFileReplace(target string, data []byte) error {
	file, err := os.CreateTemp(filepath.Dir(target), filepath.Base(target)+".*.tmp")
	if err != nil {
		return err
	}
	tmpPath := file.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	// A failed replacement must leave discovery intact. Removing the target
	// first creates a gap and can lose it entirely if the second rename fails.
	// fileio.Replace also succeeds on Windows while readers hold the target open.
	if err := fileio.Replace(tmpPath, target); err != nil {
		return err
	}
	cleanup = false
	return nil
}

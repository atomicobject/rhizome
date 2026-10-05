package runtime

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/atomicobject/rhizome/pkg/fileio"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// Mode says who owns the runtime process's lifetime.
type Mode string

const (
	// ModeAttached is a human-started `rzm serve`/`rzm start`; it never idle-exits.
	ModeAttached Mode = "attached"
	// ModeHeadless is a client-spawned `rzm serve --headless`; it idle-exits.
	ModeHeadless Mode = "headless"
)

// Vault-relative runtime files. All live under .rhizome and are ignored by the
// generated .rhizome/.gitignore.
const (
	ManifestFileName  = "runtime.json"
	LockFileName      = "runtime.lock"
	SpawnLockFileName = "runtime-spawn.lock"
	LogFileName       = "runtime.log"
	PortFileName      = "runtime-port"
)

func rhizomeFile(vaultPath, name string) string {
	return filepath.Join(vaultPath, obsidian.RhizomeDirName, name)
}

// ManifestPath is the discovery file a live runtime publishes for its vault.
func ManifestPath(vaultPath string) string { return rhizomeFile(vaultPath, ManifestFileName) }

// LockPath is the election lock: exactly one runtime per vault root holds it.
func LockPath(vaultPath string) string { return rhizomeFile(vaultPath, LockFileName) }

// SpawnLockPath is the short-lived lease a client takes while spawning.
func SpawnLockPath(vaultPath string) string { return rhizomeFile(vaultPath, SpawnLockFileName) }

// PortPath records the last port this vault's runtime served on. Unlike the
// manifest it survives shutdown, so a restarted runtime can keep its URL.
func PortPath(vaultPath string) string { return rhizomeFile(vaultPath, PortFileName) }

// ReadLastPort returns the recorded port, or 0 when none is usable.
func ReadLastPort(vaultPath string) int {
	data, err := fileio.ReadFile(PortPath(vaultPath))
	if err != nil {
		return 0
	}
	port, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || port <= 0 || port > 65535 {
		return 0
	}
	return port
}

// WriteLastPort records port for ReadLastPort.
func WriteLastPort(vaultPath string, port int) error {
	return writeFileReplace(PortPath(vaultPath), []byte(strconv.Itoa(port)+"\n"))
}

// LogPath is where a headless runtime writes its stderr/stdout.
func LogPath(vaultPath string) string {
	return rhizomeFile(vaultPath, filepath.Join("diagnostics", "runtime-output.log"))
}

// WriteManifest atomically publishes the manifest with owner-only permissions
// because it carries the control token.
func WriteManifest(vaultPath string, manifest InstanceManifest) error {
	target := ManifestPath(vaultPath)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	if err := writeFileReplace(target, data); err != nil {
		return err
	}
	return os.Chmod(target, 0o600)
}

// ReadManifest reads the published manifest without checking liveness. Use
// LiveManifest when you need a runtime you can talk to.
func ReadManifest(vaultPath string) (InstanceManifest, error) {
	data, err := fileio.ReadFile(ManifestPath(vaultPath))
	if err != nil {
		return InstanceManifest{}, err
	}
	var manifest InstanceManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return InstanceManifest{}, fmt.Errorf("parse %s: %w", ManifestPath(vaultPath), err)
	}
	return manifest, nil
}

// RemoveManifestIfOwner deletes the manifest only when it still names pid. An
// election loser or a slow shutdown must never remove the winner's manifest.
func RemoveManifestIfOwner(vaultPath string, pid int) error {
	manifest, err := ReadManifest(vaultPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if manifest.PID != pid {
		return nil
	}
	err = os.Remove(ManifestPath(vaultPath))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// NewControlToken returns a random bearer token for the control endpoints.
func NewControlToken() (string, error) { return randomHex(24) }

// NewRunID returns the per-process identity published in the manifest.
func NewRunID() (string, error) { return randomHex(8) }

func randomHex(n int) (string, error) {
	raw := make([]byte, n)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

// BuildID identifies the code a runtime runs. Released builds differ by
// version; development builds share a version, so the executable's size and
// modification time are folded in. Clients replace a headless runtime whose
// BuildID differs from their own.
func BuildID(version, executable string) string {
	h := sha256.New()
	h.Write([]byte(strings.TrimSpace(version)))
	if info, err := os.Stat(executable); err == nil {
		fmt.Fprintf(h, "|%d|%d", info.Size(), info.ModTime().UnixNano())
	}
	return hex.EncodeToString(h.Sum(nil)[:8])
}

// ErrManifestMismatch reports a manifest whose published identity does not
// match the process answering at its address.
var ErrManifestMismatch = errors.New("runtime manifest does not match the responding process")

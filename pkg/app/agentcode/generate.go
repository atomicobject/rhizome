package agentcode

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const manifestFormat = "rhizome-agent-code-manifest-v1"

type Manifest struct {
	Format           string   `json:"format"`
	GeneratorVersion string   `json:"generatorVersion"`
	ContractHash     string   `json:"contractHash"`
	ArtifactHash     string   `json:"artifactHash"`
	Selected         []string `json:"selected"`
	Module           string   `json:"module"`
	Declarations     string   `json:"declarations"`
	ModulePath       string   `json:"modulePath,omitempty"`
	DeclarationsPath string   `json:"declarationsPath,omitempty"`
	ExecutablePath   string   `json:"executablePath,omitempty"`
	VaultPath        string   `json:"vaultPath,omitempty"`
	Reused           bool     `json:"reused"`
}

type persistedManifest struct {
	Format           string   `json:"format"`
	GeneratorVersion string   `json:"generatorVersion"`
	ContractHash     string   `json:"contractHash"`
	ArtifactHash     string   `json:"artifactHash"`
	Selected         []string `json:"selected"`
	Module           string   `json:"module"`
	Declarations     string   `json:"declarations"`
}

// Generate publishes an immutable content-addressed ESM client and atomically
// updates only the Rhizome-owned manifest in output.
func Generate(output string, selected []string) (Manifest, error) {
	if output == "" {
		return Manifest{}, fmt.Errorf("output directory is required")
	}
	absOutput, err := filepath.Abs(output)
	if err != nil {
		return Manifest{}, fmt.Errorf("resolve output directory: %w", err)
	}
	description, err := Describe(selected)
	if err != nil {
		return Manifest{}, err
	}
	module := generateModule(description)
	declarations, err := generateDeclarations(description)
	if err != nil {
		return Manifest{}, err
	}
	artifactHash := contentHash(description.ContractHash, module, declarations)
	persisted := persistedManifest{
		Format: manifestFormat, GeneratorVersion: GeneratorVersion, ContractHash: description.ContractHash,
		ArtifactHash: artifactHash, Selected: append([]string(nil), description.Selected...),
		Module:       filepath.ToSlash(filepath.Join(artifactHash, "index.mjs")),
		Declarations: filepath.ToSlash(filepath.Join(artifactHash, "index.d.mts")),
	}
	persistedJSON, err := json.MarshalIndent(persisted, "", "  ")
	if err != nil {
		return Manifest{}, err
	}
	persistedJSON = append(persistedJSON, '\n')
	files := map[string][]byte{
		"index.mjs": []byte(module), "index.d.mts": []byte(declarations), "manifest.json": persistedJSON,
	}
	reused, err := publish(absOutput, artifactHash, files, persistedJSON)
	if err != nil {
		return Manifest{}, err
	}
	return Manifest{
		Format: persisted.Format, GeneratorVersion: persisted.GeneratorVersion, ContractHash: persisted.ContractHash,
		ArtifactHash: persisted.ArtifactHash, Selected: persisted.Selected, Module: persisted.Module,
		Declarations: persisted.Declarations, ModulePath: filepath.Join(absOutput, artifactHash, "index.mjs"),
		DeclarationsPath: filepath.Join(absOutput, artifactHash, "index.d.mts"), Reused: reused,
	}, nil
}

func contentHash(contractHash, module, declarations string) string {
	sum := sha256.Sum256([]byte(contractHash + "\x00" + module + "\x00" + declarations))
	return hex.EncodeToString(sum[:])
}

func publish(output, hash string, files map[string][]byte, pointer []byte) (bool, error) {
	if err := os.MkdirAll(output, 0o755); err != nil {
		return false, fmt.Errorf("create code output: %w", err)
	}
	if err := preflightPointer(output); err != nil {
		return false, err
	}
	target := filepath.Join(output, hash)
	if info, err := os.Lstat(target); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return false, fmt.Errorf("refusing symlink code artifact %s", target)
	} else if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	if completeArtifact(target, files) {
		if err := writePointer(output, pointer); err != nil {
			return false, err
		}
		return true, nil
	}
	if _, err := os.Stat(target); err == nil {
		return false, fmt.Errorf("immutable code artifact %s exists with different contents", hash)
	} else if !os.IsNotExist(err) {
		return false, err
	}
	staging, err := os.MkdirTemp(output, ".agent-code-staging-")
	if err != nil {
		return false, err
	}
	defer os.RemoveAll(staging)
	for _, name := range []string{"index.mjs", "index.d.mts", "manifest.json"} {
		if err := os.WriteFile(filepath.Join(staging, name), files[name], 0o644); err != nil {
			return false, err
		}
	}
	if err := os.Rename(staging, target); err != nil {
		return false, fmt.Errorf("publish code artifact: %w", err)
	}
	if err := writePointer(output, pointer); err != nil {
		return false, err
	}
	return false, nil
}

func completeArtifact(target string, files map[string][]byte) bool {
	info, err := os.Lstat(target)
	if err != nil || !info.IsDir() {
		return false
	}
	for name, want := range files {
		path := filepath.Join(target, name)
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() {
			return false
		}
		got, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(got, want) {
			return false
		}
	}
	return true
}

func preflightPointer(output string) error {
	path := filepath.Join(output, "manifest.json")
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("refusing to overwrite non-regular %s", path)
	}
	existing, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var owned persistedManifest
	if json.Unmarshal(existing, &owned) != nil || owned.Format != manifestFormat {
		return fmt.Errorf("refusing to overwrite unrelated %s", path)
	}
	return nil
}

func writePointer(output string, payload []byte) error {
	path := filepath.Join(output, "manifest.json")
	if err := preflightPointer(output); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(output, ".agent-code-manifest-")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err = tmp.Write(payload); err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("publish code manifest: %w", err)
	}
	return nil
}

func generateModule(description DescribeResponse) string {
	selected := make([]string, 0, len(description.Operations))
	for _, operation := range description.Operations {
		selected = append(selected, operation.Name)
	}
	selectedJSON, _ := json.Marshal(selected)
	module := strings.ReplaceAll(clientModuleTemplate, "__CONTRACT_HASH__", description.ContractHash)
	module = strings.ReplaceAll(module, "__SELECTED__", string(selectedJSON))
	module = strings.ReplaceAll(module, "__DEFAULT_CALL_TIMEOUT_MS__", strconv.Itoa(defaultCallTimeoutMS))
	return module
}

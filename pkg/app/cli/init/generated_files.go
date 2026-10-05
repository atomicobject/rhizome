// The generated-files record lets init tell an untouched Rhizome file from one
// someone edited. Without it, a file that differs from the current template is
// ambiguous: it could be an older Rhizome version or a local customization.
//
// Docs:
// - [[init-starter-workflow#^SPEC-0038-US3-AC4]]
// - [[init-starter-workflow#^SPEC-0038-US3-AC5]]
// - [[init-template-architecture]]
package init

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"gopkg.in/yaml.v3"
)

const generatedFilesName = "generated-files.yml"

const generatedFilesHeader = `# Written by rzm init. Fingerprints of the files Rhizome generated, so later
# runs update files nobody edited and ask about files someone did. Commit it.
`

// generatedFiles maps record keys to content fingerprints. A key is a
// project-relative slash path, or "path#block" for a managed block inside a
// shared file such as AGENTS.md.
type generatedFiles struct {
	Version int `yaml:"version"`
	// Written is the fingerprint of what Rhizome last wrote.
	Written map[string]string `yaml:"written,omitempty"`
	// Declined is the fingerprint of a version someone chose not to take.
	Declined map[string]string `yaml:"declined,omitempty"`
	// unreadable marks a record that exists but could not be parsed, for
	// example after a merge conflict.
	unreadable bool
}

func generatedFilesPath(projectRoot string) string {
	return filepath.Join(projectRoot, obsidian.RhizomeDirName, generatedFilesName)
}

// loadGeneratedFiles reads the record. A missing, unreadable, or conflicted
// record is treated as empty so the run falls back to asking about files it
// cannot classify instead of failing.
func loadGeneratedFiles(projectRoot string) (*generatedFiles, bool) {
	record := &generatedFiles{Version: 1, Written: map[string]string{}, Declined: map[string]string{}}
	data, err := os.ReadFile(generatedFilesPath(projectRoot))
	if err != nil {
		return record, errors.Is(err, os.ErrNotExist)
	}
	var parsed generatedFiles
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		record.unreadable = true
		return record, false
	}
	for key, value := range parsed.Written {
		record.Written[key] = value
	}
	for key, value := range parsed.Declined {
		record.Declined[key] = value
	}
	return record, true
}

// save writes the record with sorted keys, one entry per line, so concurrent
// branches merge cleanly. Entries for files that no longer exist are dropped.
func (g *generatedFiles) save(projectRoot string) error {
	for key := range g.Written {
		if !recordTargetExists(projectRoot, key) {
			delete(g.Written, key)
		}
	}
	for key := range g.Declined {
		if !recordTargetExists(projectRoot, key) {
			delete(g.Declined, key)
		}
	}
	path := generatedFilesPath(projectRoot)
	if len(g.Written) == 0 && len(g.Declined) == 0 {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	g.Version = 1
	var body bytes.Buffer
	body.WriteString(generatedFilesHeader)
	enc := yaml.NewEncoder(&body)
	enc.SetIndent(obsidian.LocalConfigYAMLIndent)
	if err := enc.Encode(g); err != nil {
		return err
	}
	if err := enc.Close(); err != nil {
		return err
	}
	content := body.Bytes()
	if existing, err := os.ReadFile(path); err == nil && bytes.Equal(existing, content) {
		return nil
	}
	if err := ensureDirExists(filepath.Dir(path)); err != nil {
		return err
	}
	return os.WriteFile(path, content, 0o644)
}

// recordTargetExists reports false only when the file is gone from a folder
// that still exists. A folder behind a broken symlink proves nothing, so its
// entries are kept.
func recordTargetExists(projectRoot, key string) bool {
	path, _, _ := strings.Cut(key, "#")
	full := filepath.Join(projectRoot, filepath.FromSlash(path))
	if _, err := os.Stat(full); err == nil {
		return true
	}
	info, err := os.Stat(filepath.Dir(full))
	return err != nil || !info.IsDir()
}

func (g *generatedFiles) recordWritten(key, fingerprint string) {
	g.Written[key] = fingerprint
	delete(g.Declined, key)
}

func (g *generatedFiles) recordDeclined(key, fingerprint string) {
	g.Declined[key] = fingerprint
}

func (g *generatedFiles) forget(key string) {
	delete(g.Written, key)
	delete(g.Declined, key)
}

// ownsUnder reports whether the record holds any file under dir (a
// project-relative slash path), which is how a skill folder is known to be
// Rhizome's.
func (g *generatedFiles) ownsUnder(dir string) bool {
	prefix := strings.TrimSuffix(dir, "/") + "/"
	for key := range g.Written {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return false
}

// contentFingerprint hashes content after normalizing line endings and the
// trailing newline, so a Windows checkout with core.autocrlf does not make
// every generated file look edited.
func contentFingerprint(content []byte) string {
	normalized := bytes.ReplaceAll(content, []byte("\r\n"), []byte("\n"))
	normalized = append(bytes.TrimRight(normalized, "\n"), '\n')
	sum := sha256.Sum256(normalized)
	return hex.EncodeToString(sum[:])[:16]
}

// legacyManagedSkillFiles describes the per-skill-folder ownership files that
// the record replaces.
var legacyManagedSkillFiles = []string{".rhizome-managed", ".rhizome-managed-files"}

// readLegacySkillManifest returns the raw sha256 hashes that older versions
// recorded for reference files inside one skill folder.
func readLegacySkillManifest(skillDir string) map[string]string {
	data, err := os.ReadFile(filepath.Join(skillDir, ".rhizome-managed-files"))
	if err != nil {
		return nil
	}
	recorded := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		path, hash, ok := strings.Cut(strings.TrimSpace(line), " ")
		if ok && path != "" && hash != "" {
			recorded[path] = hash
		}
	}
	return recorded
}

func hasLegacySkillMarker(skillDir string) bool {
	_, err := os.Stat(filepath.Join(skillDir, ".rhizome-managed"))
	return err == nil
}

// foldLegacySkillFolder moves the evidence in an older skill folder's marker
// files into the record: reference files whose bytes still match the legacy
// manifest are recorded as written. Other files are classified normally.
func (g *generatedFiles) foldLegacySkillFolder(projectRoot, relDir string) {
	skillDir := filepath.Join(projectRoot, filepath.FromSlash(relDir))
	for path, hash := range readLegacySkillManifest(skillDir) {
		data, err := os.ReadFile(filepath.Join(skillDir, filepath.FromSlash(path)))
		if err != nil {
			continue
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != hash {
			continue
		}
		key := relDir + "/" + path
		if _, recorded := g.Written[key]; !recorded {
			g.Written[key] = contentFingerprint(data)
		}
	}
}

func removeLegacySkillFiles(skillDir string) error {
	for _, name := range legacyManagedSkillFiles {
		if err := os.Remove(filepath.Join(skillDir, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove %s: %w", filepath.Join(skillDir, name), err)
		}
	}
	return nil
}

// legacyRejectionsName is the retired per-hunk rejection store. The ownership
// record replaces it.
const legacyRejectionsName = "template-rejections.yml"

func removeLegacyRejections(projectRoot string) error {
	path := filepath.Join(projectRoot, obsidian.RhizomeDirName, legacyRejectionsName)
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

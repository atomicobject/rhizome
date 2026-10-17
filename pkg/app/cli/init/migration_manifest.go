package init

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/fileio"
	"gopkg.in/yaml.v3"
)

type legacyProcessDocRecord struct {
	Path           string                         `yaml:"path"`
	Classification legacyProcessDocClassification `yaml:"classification"`
	Fingerprint    string                         `yaml:"fingerprint"`
	OriginalStatus string                         `yaml:"originalStatus,omitempty"`
	FollowUp       string                         `yaml:"followUp"`
}

type legacyProcessMigrationManifest struct {
	Version int                      `yaml:"version"`
	Records []legacyProcessDocRecord `yaml:"records"`
}

func sortedLegacyProcessDocRecords(recordsByPath map[string]legacyProcessDocRecord) []legacyProcessDocRecord {
	records := make([]legacyProcessDocRecord, 0, len(recordsByPath))
	for _, record := range recordsByPath {
		records = append(records, record)
	}
	sort.Slice(records, func(i, j int) bool { return records[i].Path < records[j].Path })
	return records
}

func writeLegacyProcessMigrationManifest(projectRoot string, records []legacyProcessDocRecord) error {
	dir := legacyProcessMigrationDir(projectRoot)
	if err := ensureDirExists(dir); err != nil {
		return err
	}
	manifest, err := yaml.Marshal(legacyProcessMigrationManifest{Version: 1, Records: records})
	if err != nil {
		return err
	}
	if err := fileio.WriteFileAtomicPreservingMode(legacyProcessMigrationManifestPath(projectRoot), manifest, 0o644); err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString("# Agentic Engineering migration\n\n")
	b.WriteString("This manifest records legacy `docs/specs/process/` evidence retained for team reconciliation. Delete it after the team has completed the listed follow-up.\n\n")
	b.WriteString("| Path | Classification | SHA-256 | Original active status | Follow-up |\n| --- | --- | --- | --- | --- |\n")
	for _, record := range records {
		fmt.Fprintf(&b, "| `%s` | %s | `%s` | %s | %s |\n", record.Path, record.Classification, record.Fingerprint, firstNonEmpty(record.OriginalStatus, "-"), record.FollowUp)
	}
	return fileio.WriteFileAtomicPreservingMode(filepath.Join(dir, "README.md"), []byte(b.String()), 0o644)
}

func legacyProcessMigrationDir(projectRoot string) string {
	return filepath.Join(projectRoot, ".rhizome", "migrations", templateAgenticEngineering)
}

func legacyProcessMigrationManifestPath(projectRoot string) string {
	return filepath.Join(legacyProcessMigrationDir(projectRoot), legacyProcessMigrationManifestName)
}

func legacyProcessMigrationRecords(projectRoot string) (map[string]legacyProcessDocRecord, error) {
	data, err := os.ReadFile(legacyProcessMigrationManifestPath(projectRoot))
	if os.IsNotExist(err) {
		return map[string]legacyProcessDocRecord{}, nil
	}
	if err != nil {
		return nil, err
	}
	var manifest legacyProcessMigrationManifest
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("decode legacy process migration manifest: %w", err)
	}
	records := make(map[string]legacyProcessDocRecord, len(manifest.Records))
	for _, record := range manifest.Records {
		records[record.Path] = record
	}
	return records, nil
}

func clearLegacyProcessMigrationManifest(projectRoot string) error {
	for _, path := range []string{
		legacyProcessMigrationManifestPath(projectRoot),
		filepath.Join(legacyProcessMigrationDir(projectRoot), "README.md"),
	} {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

// fingerprintStarterAsset is the raw sha256 used by the legacy process-doc
// fingerprint catalog.
func fingerprintStarterAsset(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

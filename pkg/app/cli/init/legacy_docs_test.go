package init

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

const legacyProcessFingerprintProvenancePath = "legacy_process_fingerprints_provenance.json"

type legacyProcessFingerprintProvenance struct {
	Version    int                   `json:"version"`
	SourceRoot string                `json:"source_root"`
	Paths      map[string][][]string `json:"paths"`
}

func TestClassifyLegacyProcessDocRequiresExactCatalogMatch(t *testing.T) {
	path := "docs/specs/process/example.md"
	content := []byte("shipped bytes")
	catalog := legacyProcessFingerprintCatalog{path: {fingerprintStarterAsset(content)}}

	require.Equal(t, legacyProcessDocExact, classifyLegacyProcessDoc(path, content, catalog).Classification)
	require.Equal(t, legacyProcessDocModified, classifyLegacyProcessDoc(path, []byte("local edit"), catalog).Classification)
	require.Equal(t, legacyProcessDocUncataloged, classifyLegacyProcessDoc("docs/specs/process/missing.md", content, catalog).Classification)
}

func TestLegacyProcessFingerprintCatalogIsCompleteAndStructured(t *testing.T) {
	catalog, err := loadLegacyProcessFingerprintCatalog()
	require.NoError(t, err)

	paths := make([]string, 0, len(catalog))
	for path, fingerprints := range catalog {
		paths = append(paths, path)
		require.NotEmpty(t, fingerprints, path)
		for _, fingerprint := range fingerprints {
			decoded, err := hex.DecodeString(fingerprint)
			require.NoError(t, err, path)
			require.Len(t, decoded, sha256.Size, path)
		}
	}
	sort.Strings(paths)
	expected := append([]string(nil), legacyProcessDocPaths...)
	sort.Strings(expected)
	require.Equal(t, expected, paths)
	require.Len(t, paths, 18)

	provenance := loadLegacyProcessFingerprintProvenance(t)
	require.Equal(t, 1, provenance.Version)
	require.Equal(t, "pkg/app/cli/init/templates/starters/spec-driven/", provenance.SourceRoot)

	provenancePaths := make([]string, 0, len(provenance.Paths))
	for path := range provenance.Paths {
		provenancePaths = append(provenancePaths, path)
	}
	sort.Strings(provenancePaths)
	require.Equal(t, expected, provenancePaths)

	for _, rel := range expected {
		records := provenance.Paths[rel]
		require.Len(t, records, len(catalog[rel]), rel)

		seen := make(map[string]struct{}, len(records))
		for _, record := range records {
			require.Len(t, record, 3, rel)
			digest, commit, blob := record[0], record[1], record[2]
			require.Contains(t, catalog[rel], digest, rel)
			_, duplicate := seen[digest]
			require.False(t, duplicate, "%s has duplicate provenance for %s", rel, digest)
			seen[digest] = struct{}{}
			requireGitObjectID(t, commit, "commit", rel)
			requireGitObjectID(t, blob, "blob", rel)
		}
		require.Len(t, seen, len(catalog[rel]), rel)
	}
}

func TestLegacyProcessFingerprintCatalogMatchesGitHistory(t *testing.T) {
	if os.Getenv("RZM_AUDIT_LEGACY_PROCESS_PROVENANCE") != "1" {
		t.Skip("set RZM_AUDIT_LEGACY_PROCESS_PROVENANCE=1 to audit historical Git objects")
	}
	catalog, err := loadLegacyProcessFingerprintCatalog()
	require.NoError(t, err)
	provenance := loadLegacyProcessFingerprintProvenance(t)
	for rel, records := range provenance.Paths {
		for _, record := range records {
			digest, commit, blob := record[0], record[1], record[2]
			require.Contains(t, catalog[rel], digest, rel)
			resolvedCommit := runGit(t, "rev-parse", "--verify", commit+"^{commit}")
			require.Equal(t, commit, resolvedCommit, rel)
			resolvedBlob := runGit(t, "rev-parse", commit+":"+provenance.SourceRoot+rel)
			require.Equal(t, blob, resolvedBlob, rel)
			blobBytes := runGitBytes(t, "cat-file", "blob", blob)
			require.Equal(t, digest, fingerprintStarterAsset(blobBytes), rel)
		}
	}
}

func loadLegacyProcessFingerprintProvenance(t *testing.T) legacyProcessFingerprintProvenance {
	t.Helper()
	provenanceBytes, err := os.ReadFile(legacyProcessFingerprintProvenancePath)
	require.NoError(t, err)
	var provenance legacyProcessFingerprintProvenance
	require.NoError(t, json.Unmarshal(provenanceBytes, &provenance))
	return provenance
}

func requireGitObjectID(t *testing.T, objectID, objectType, path string) {
	t.Helper()
	require.True(t, len(objectID) == 40 || len(objectID) == 64, "%s %s has unexpected length", path, objectType)
	decoded, err := hex.DecodeString(objectID)
	require.NoError(t, err, "%s %s is not hexadecimal", path, objectType)
	require.NotEmpty(t, decoded, "%s %s is empty", path, objectType)
	require.Equal(t, strings.ToLower(objectID), objectID, "%s %s must be lowercase", path, objectType)
}

func runGit(t *testing.T, args ...string) string {
	t.Helper()
	return strings.TrimSpace(string(runGitBytes(t, args...)))
}

func runGitBytes(t *testing.T, args ...string) []byte {
	t.Helper()
	output, err := exec.Command("git", args...).Output()
	require.NoErrorf(t, err, "git %s", strings.Join(args, " "))
	return output
}

func TestRetireLegacyProcessDocsKeepsUnprovenContentAndWritesOneNotice(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "docs", "specs", "process", "local.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte("---\ntype: ProcessSpec\nspec-status: active\n---\n# Team policy\n"), 0o600))

	records, err := retireLegacyProcessDocs(root)
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.Equal(t, "active", records[0].OriginalStatus)
	first, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(first), legacyProcessRetirementSentinel)
	frontmatter, err := obsidian.ExtractFrontmatter(string(first))
	require.NoError(t, err)
	require.Equal(t, "archived", frontmatter["spec-status"])
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		require.NoError(t, err)
		require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	}
	require.FileExists(t, filepath.Join(root, ".rhizome", "migrations", templateAgenticEngineering, "README.md"))
	require.FileExists(t, legacyProcessMigrationManifestPath(root))
	firstManifest, err := os.ReadFile(legacyProcessMigrationManifestPath(root))
	require.NoError(t, err)

	records, err = retireLegacyProcessDocs(root)
	require.NoError(t, err)
	require.Equal(t, "active", records[0].OriginalStatus)
	second, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, string(first), string(second))
	secondManifest, err := os.ReadFile(legacyProcessMigrationManifestPath(root))
	require.NoError(t, err)
	require.Equal(t, firstManifest, secondManifest)
}

func TestRetireLegacyProcessDocsDeletesExactShippedContentAfterReplacement(t *testing.T) {
	root := t.TempDir()
	legacyPath := filepath.Join(root, "docs", "specs", "process", "README.md")
	replacement := []byte("shipped process bytes\n")
	require.NoError(t, os.MkdirAll(filepath.Dir(legacyPath), 0o755))
	require.NoError(t, os.WriteFile(legacyPath, replacement, 0o644))

	engineering := filepath.Join(root, "docs", "engineering")
	require.NoError(t, os.MkdirAll(engineering, 0o755))
	for _, name := range []string{"README.md", "testing-policy.md", "quality-gates.md", "documentation.md", "review-and-approval.md", "architecture.md", "release.md"} {
		require.NoError(t, os.WriteFile(filepath.Join(engineering, name), []byte("replacement\n"), 0o644))
	}

	records, err := retireLegacyProcessDocsWithCatalog(root, legacyProcessFingerprintCatalog{
		"docs/specs/process/README.md": {fingerprintStarterAsset(replacement)},
	})
	require.NoError(t, err)
	require.NoFileExists(t, legacyPath)
	require.Len(t, records, 1)
	require.Equal(t, legacyProcessDocExact, records[0].Classification)
	require.Equal(t, "Deleted after replacement engineering docs were present.", records[0].FollowUp)
	firstManifest, err := os.ReadFile(legacyProcessMigrationManifestPath(root))
	require.NoError(t, err)

	replayed, err := retireLegacyProcessDocsWithCatalog(root, legacyProcessFingerprintCatalog{
		"docs/specs/process/README.md": {fingerprintStarterAsset(replacement)},
	})
	require.NoError(t, err)
	require.Equal(t, records, replayed)
	secondManifest, err := os.ReadFile(legacyProcessMigrationManifestPath(root))
	require.NoError(t, err)
	require.Equal(t, firstManifest, secondManifest)
}

func TestRetireLegacyProcessDocsKeepsExactShippedContentWithAuthoredReferences(t *testing.T) {
	root := t.TempDir()
	legacyDir := filepath.Join(root, "docs", "specs", "process")
	replacement := []byte("---\ntype: ProcessSpec\nspec-status: active\n---\n# Shipped process\n")
	require.NoError(t, os.MkdirAll(legacyDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(legacyDir, "README.md"), replacement, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(legacyDir, "agent-workflow.md"), replacement, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(legacyDir, "review-handling.md"), replacement, 0o644))

	engineering := filepath.Join(root, "docs", "engineering")
	require.NoError(t, os.MkdirAll(engineering, 0o755))
	for _, name := range []string{"README.md", "testing-policy.md", "quality-gates.md", "documentation.md", "review-and-approval.md", "architecture.md", "release.md"} {
		require.NoError(t, os.WriteFile(filepath.Join(engineering, name), []byte("replacement\n"), 0o644))
	}
	require.NoError(t, os.MkdirAll(filepath.Join(root, "docs", "efforts"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "docs", "efforts", "active.md"), []byte("[[../specs/process/README|process]]\n\n[review policy][review]\n\n[review]:\n  ../specs/process/review-handling.md\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "docs", "reference.md"), []byte("[workflow](specs/process/agent-workflow.md)\n"), 0o644))

	catalog := legacyProcessFingerprintCatalog{
		"docs/specs/process/README.md":          {fingerprintStarterAsset(replacement)},
		"docs/specs/process/agent-workflow.md":  {fingerprintStarterAsset(replacement)},
		"docs/specs/process/review-handling.md": {fingerprintStarterAsset(replacement)},
	}
	first, err := retireLegacyProcessDocsWithCatalog(root, catalog)
	require.NoError(t, err)
	require.Len(t, first, 3)
	for _, record := range first {
		require.Equal(t, legacyProcessDocExactReferenced, record.Classification)
		require.Equal(t, "active", record.OriginalStatus)
		body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(record.Path)))
		require.NoError(t, err)
		require.Contains(t, string(body), legacyProcessRetirementSentinel)
	}
	firstManifest, err := os.ReadFile(legacyProcessMigrationManifestPath(root))
	require.NoError(t, err)

	second, err := retireLegacyProcessDocsWithCatalog(root, catalog)
	require.NoError(t, err)
	require.Equal(t, first, second)
	secondManifest, err := os.ReadFile(legacyProcessMigrationManifestPath(root))
	require.NoError(t, err)
	require.Equal(t, firstManifest, secondManifest)
}

func TestRetireLegacyProcessDocsKeepsExactDependencyOfModifiedLegacyDoc(t *testing.T) {
	root := t.TempDir()
	legacyDir := filepath.Join(root, "docs", "specs", "process")
	exact := []byte("# Shipped process\n\n[Downstream](downstream.md)\n")
	downstream := []byte("# Downstream process\n")
	require.NoError(t, os.MkdirAll(legacyDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(legacyDir, "README.md"), exact, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(legacyDir, "downstream.md"), downstream, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(legacyDir, "local-policy.md"), []byte("# Team policy\n\n[Shipped dependency](README.md)\n"), 0o644))

	engineering := filepath.Join(root, "docs", "engineering")
	require.NoError(t, os.MkdirAll(engineering, 0o755))
	for _, name := range []string{"README.md", "testing-policy.md", "quality-gates.md", "documentation.md", "review-and-approval.md", "architecture.md", "release.md"} {
		require.NoError(t, os.WriteFile(filepath.Join(engineering, name), []byte("replacement\n"), 0o644))
	}

	records, err := retireLegacyProcessDocsWithCatalog(root, legacyProcessFingerprintCatalog{
		"docs/specs/process/README.md":     {fingerprintStarterAsset(exact)},
		"docs/specs/process/downstream.md": {fingerprintStarterAsset(downstream)},
	})
	require.NoError(t, err)
	require.FileExists(t, filepath.Join(legacyDir, "README.md"))
	require.FileExists(t, filepath.Join(legacyDir, "downstream.md"))
	byPath := make(map[string]legacyProcessDocRecord, len(records))
	for _, record := range records {
		byPath[record.Path] = record
	}
	require.Equal(t, legacyProcessDocExactReferenced, byPath["docs/specs/process/README.md"].Classification)
	require.Equal(t, legacyProcessDocExactReferenced, byPath["docs/specs/process/downstream.md"].Classification)
	require.Equal(t, legacyProcessDocUncataloged, byPath["docs/specs/process/local-policy.md"].Classification)
}

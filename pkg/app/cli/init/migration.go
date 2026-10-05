package init

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"gopkg.in/yaml.v3"
)

const legacyProcessDocsRoot = "docs/specs/process"
const legacyProcessRetirementSentinel = "<!-- RZM AGENTIC-ENGINEERING RETIREMENT NOTICE -->"
const legacyProcessMigrationManifestName = "manifest.yml"

var legacyProcessDocPaths = []string{
	"docs/specs/process/README.md",
	"docs/specs/process/agent-skills.md",
	"docs/specs/process/agent-workflow.md",
	"docs/specs/process/audits.md",
	"docs/specs/process/compounding-work.md",
	"docs/specs/process/debugging-workflow.md",
	"docs/specs/process/development-loop.md",
	"docs/specs/process/effort-lifecycle.md",
	"docs/specs/process/id-allocation.md",
	"docs/specs/process/lifecycle-immutability.md",
	"docs/specs/process/ontology-design-principles.md",
	"docs/specs/process/quality-gates.md",
	"docs/specs/process/repo-layout.md",
	"docs/specs/process/review-handling.md",
	"docs/specs/process/specs-organization.md",
	"docs/specs/process/story-lifecycle.md",
	"docs/specs/process/testing-policy.md",
	"docs/specs/process/transcript-ingestion.md",
}

var retiredStarterSkillNames = []string{
	"development-loop",
	"alignment-audit",
	"quality-gates-check",
	"backport",
	"compound",
	"debugging",
	"rhizome-review-feedback",
	"code-docs",
	"refactor-planning",
	"specify",
	"effort-new",
	"plan",
	"implement",
	"effort-finish",
}

//go:embed legacy_process_fingerprints.json
var legacyProcessFingerprintCatalogJSON []byte

type legacyProcessFingerprintCatalog map[string][]string

var legacyProcessFingerprintCatalogLoader = loadLegacyProcessFingerprintCatalog

func loadLegacyProcessFingerprintCatalog() (legacyProcessFingerprintCatalog, error) {
	var catalog legacyProcessFingerprintCatalog
	if err := json.Unmarshal(legacyProcessFingerprintCatalogJSON, &catalog); err != nil {
		return nil, fmt.Errorf("decode legacy process fingerprint catalog: %w", err)
	}
	return catalog, nil
}

func hasLegacyAgenticEngineeringState(cfg obsidian.LocalConfig) bool {
	for _, ids := range [][]string{
		cfg.WorkflowTemplates,
		cfg.WorkflowTemplateAddons.Enabled,
		cfg.WorkflowTemplateAddons.Disabled,
		cfg.WorkflowTemplateManagement.Ejected,
	} {
		for _, candidate := range ids {
			if strings.EqualFold(strings.TrimSpace(candidate), legacyTemplateSpecDriven) {
				return true
			}
		}
	}
	for key := range cfg.WorkflowTemplateManagement.SourceFingerprints {
		starter, _, _, ok := parseStarterFingerprintKey(key)
		if ok && strings.EqualFold(strings.TrimSpace(starter), legacyTemplateSpecDriven) {
			return true
		}
	}
	return false
}

// normalizeLegacyAgenticEngineeringState performs only in-memory state work.
// Its caller owns filesystem actions and commits workflows.yml after those
// actions complete, so a failure remains safely replayable from legacy state.
func normalizeLegacyAgenticEngineeringState(cfg *obsidian.LocalConfig) (bool, error) {
	if cfg == nil {
		return false, nil
	}
	changed := false
	normalizeWithoutMigration := func(values []string) []string {
		seen := make(map[string]struct{}, len(values))
		var normalized []string
		for _, raw := range values {
			id := strings.ToLower(strings.TrimSpace(raw))
			if id == "" {
				continue
			}
			if _, exists := seen[id]; exists {
				continue
			}
			seen[id] = struct{}{}
			normalized = append(normalized, id)
		}
		sort.Strings(normalized)
		return normalized
	}
	normalize := func(values []string) []string {
		before := normalizeWithoutMigration(values)
		after := normalizeMetadataIDsWithCanonicalIDs(values)
		if strings.Join(before, "\x00") != strings.Join(after, "\x00") {
			changed = true
		}
		return after
	}
	cfg.WorkflowTemplates = normalize(cfg.WorkflowTemplates)
	cfg.WorkflowTemplateAddons.Enabled = normalize(cfg.WorkflowTemplateAddons.Enabled)
	cfg.WorkflowTemplateAddons.Disabled = normalize(cfg.WorkflowTemplateAddons.Disabled)
	cfg.WorkflowTemplateManagement.Ejected = normalize(cfg.WorkflowTemplateManagement.Ejected)

	return changed, nil
}

func normalizeMetadataIDsWithCanonicalIDs(ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(ids))
	var out []string
	for _, raw := range ids {
		id := canonicalTemplateID(raw)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func parseStarterFingerprintKey(key string) (starter, family, assetPath string, ok bool) {
	parts := strings.SplitN(key, ":", 3)
	if len(parts) != 3 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" || strings.TrimSpace(parts[2]) == "" {
		return "", "", "", false
	}
	return parts[0], parts[1], filepath.ToSlash(parts[2]), true
}

type legacyProcessDocClassification string

const (
	legacyProcessDocExact           legacyProcessDocClassification = "exact-shipped-match"
	legacyProcessDocExactReferenced legacyProcessDocClassification = "exact-shipped-referenced"
	legacyProcessDocModified        legacyProcessDocClassification = "modified"
	legacyProcessDocUncataloged     legacyProcessDocClassification = "uncataloged"
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

func classifyLegacyProcessDoc(rel string, content []byte, catalog legacyProcessFingerprintCatalog) legacyProcessDocRecord {
	record := legacyProcessDocRecord{Path: filepath.ToSlash(rel), Fingerprint: fingerprintStarterAsset(content)}
	known, cataloged := catalog[record.Path]
	if !cataloged {
		record.Classification = legacyProcessDocUncataloged
		record.FollowUp = "Keep and reconcile manually; no shipped fingerprint catalog entry proves ownership."
		return record
	}
	for _, fingerprint := range known {
		if record.Fingerprint == fingerprint {
			record.Classification = legacyProcessDocExact
			record.FollowUp = "Delete after the replacement engineering docs are installed."
			return record
		}
	}
	record.Classification = legacyProcessDocModified
	record.FollowUp = "Keep and reconcile the local policy into docs/engineering before deleting."
	return record
}

// retireLegacyProcessDocs is deliberately conservative: a catalog match only
// proves a file was shipped, never that a replacement is available. The phase
// that supplies docs/engineering enables deletion on a later idempotent run.
func retireLegacyProcessDocs(projectRoot string) ([]legacyProcessDocRecord, error) {
	catalog, err := legacyProcessFingerprintCatalogLoader()
	if err != nil {
		return nil, err
	}
	return retireLegacyProcessDocsWithCatalog(projectRoot, catalog)
}

func retireLegacyProcessDocsWithCatalog(projectRoot string, catalog legacyProcessFingerprintCatalog) ([]legacyProcessDocRecord, error) {
	priorRecords, err := legacyProcessMigrationRecords(projectRoot)
	if err != nil {
		return nil, err
	}
	legacyRoot := filepath.Join(projectRoot, filepath.FromSlash(legacyProcessDocsRoot))
	entries, err := os.ReadDir(legacyRoot)
	if os.IsNotExist(err) {
		if len(priorRecords) > 0 {
			return sortedLegacyProcessDocRecords(priorRecords), nil
		}
		return nil, clearLegacyProcessMigrationManifest(projectRoot)
	}
	if err != nil {
		return nil, err
	}
	referenced, err := referencedLegacyProcessDocs(projectRoot, catalog)
	if err != nil {
		return nil, err
	}
	replacementReady := hasAgenticEngineeringReplacementDocs(projectRoot)
	var records []legacyProcessDocRecord
	seen := make(map[string]bool)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		rel := filepath.ToSlash(filepath.Join(legacyProcessDocsRoot, entry.Name()))
		path := filepath.Join(legacyRoot, entry.Name())
		content, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		record := classifyLegacyProcessDoc(rel, content, catalog)
		seen[record.Path] = true
		if prior, ok := priorRecords[record.Path]; ok && strings.Contains(string(content), legacyProcessRetirementSentinel) {
			record.Classification = prior.Classification
			record.FollowUp = prior.FollowUp
		}
		if record.Classification == legacyProcessDocExact && referenced[record.Path] {
			record.Classification = legacyProcessDocExactReferenced
			record.FollowUp = "Retained because authored links resolve to this document; reconcile those references before deleting."
		}
		if record.Classification == legacyProcessDocExact && replacementReady {
			if err := os.Remove(path); err != nil {
				return nil, fmt.Errorf("retire unchanged legacy process doc %s: %w", rel, err)
			}
			record.FollowUp = "Deleted after replacement engineering docs were present."
		} else if record.Classification != legacyProcessDocExact {
			updated, originalStatus, changed, err := prepareRetainedLegacyProcessDoc(content)
			if err != nil {
				return nil, err
			}
			if originalStatus == "" {
				originalStatus = priorRecords[record.Path].OriginalStatus
			}
			record.OriginalStatus = originalStatus
			if changed {
				if err := obsidian.WriteFileAtomicPreservingMode(path, updated, 0o644); err != nil {
					return nil, fmt.Errorf("retain legacy process doc %s: %w", rel, err)
				}
			}
			// The manifest fingerprints the retained artifact, not the pre-migration
			// input, so replay remains byte-stable after adding the notice/status.
			record.Fingerprint = fingerprintStarterAsset(updated)
		}
		records = append(records, record)
	}
	for path, record := range priorRecords {
		if !seen[path] {
			records = append(records, record)
		}
	}
	if len(records) == 0 {
		return nil, clearLegacyProcessMigrationManifest(projectRoot)
	}
	sort.Slice(records, func(i, j int) bool { return records[i].Path < records[j].Path })
	if err := writeLegacyProcessMigrationManifest(projectRoot, records); err != nil {
		return nil, err
	}
	return records, nil
}

func sortedLegacyProcessDocRecords(recordsByPath map[string]legacyProcessDocRecord) []legacyProcessDocRecord {
	records := make([]legacyProcessDocRecord, 0, len(recordsByPath))
	for _, record := range recordsByPath {
		records = append(records, record)
	}
	sort.Slice(records, func(i, j int) bool { return records[i].Path < records[j].Path })
	return records
}

// referencedLegacyProcessDocs returns legacy process documents targeted by
// authored links outside the legacy process directory. Such links are adoption
// evidence: deleting the target would break the repository's graph even when
// the target bytes still match a shipped template.
func referencedLegacyProcessDocs(projectRoot string, catalog legacyProcessFingerprintCatalog) (map[string]bool, error) {
	var notes []string
	err := filepath.WalkDir(projectRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", ".rhizome", "node_modules", "vendor":
				if path != projectRoot {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if !strings.EqualFold(filepath.Ext(entry.Name()), ".md") {
			return nil
		}
		rel, err := filepath.Rel(projectRoot, path)
		if err != nil {
			return err
		}
		notes = append(notes, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan legacy process references: %w", err)
	}

	cache := obsidian.BuildNotePathCache(notes)
	legacyPrefix := legacyProcessDocsRoot + "/"
	legacyContent := make(map[string][]byte)
	linksBySource := make(map[string][]string)
	for _, source := range notes {
		content, err := os.ReadFile(filepath.Join(projectRoot, filepath.FromSlash(source)))
		if err != nil {
			return nil, fmt.Errorf("read link source %s: %w", source, err)
		}
		if strings.HasPrefix(source, legacyPrefix) {
			legacyContent[source] = content
		}
		for _, link := range obsidian.ScanStructuredLinks(string(content)) {
			var target string
			var ok bool
			switch link.Kind {
			case obsidian.StructuredLinkWikilink:
				if strings.HasPrefix(link.Path, "./") || strings.HasPrefix(link.Path, "../") {
					target, ok = cache.ResolveMdLink(link.Target, source)
				} else {
					target, ok = cache.ResolveNote(link.Target)
				}
			case obsidian.StructuredLinkMarkdown:
				target, ok = cache.ResolveMdLink(link.Target, source)
			}
			if ok {
				target = canonicalCachedNotePath(cache, target)
			}
			if ok && strings.HasPrefix(target, legacyPrefix) {
				linksBySource[source] = append(linksBySource[source], target)
			}
		}
		for _, definition := range obsidian.ExtractMarkdownReferenceDefinitions(string(content)) {
			target, ok := cache.ResolveMdLink(definition, source)
			if ok {
				target = canonicalCachedNotePath(cache, target)
			}
			if ok && strings.HasPrefix(target, legacyPrefix) {
				linksBySource[source] = append(linksBySource[source], target)
			}
		}
	}

	// Seed reachability with every external reference and every legacy document
	// that will be retained for local reconciliation. Following legacy-to-legacy
	// edges transitively prevents a retained document from gaining broken links
	// when its exact shipped dependencies would otherwise be deleted.
	referenced := make(map[string]bool)
	queued := make(map[string]bool)
	queue := make([]string, 0)
	enqueue := func(path string) {
		if queued[path] {
			return
		}
		queued[path] = true
		queue = append(queue, path)
	}
	for source, targets := range linksBySource {
		if strings.HasPrefix(source, legacyPrefix) {
			continue
		}
		for _, target := range targets {
			referenced[target] = true
			enqueue(target)
		}
	}
	for path, content := range legacyContent {
		record := classifyLegacyProcessDoc(path, content, catalog)
		if record.Classification != legacyProcessDocExact || strings.Contains(string(content), legacyProcessRetirementSentinel) {
			enqueue(path)
		}
	}
	for len(queue) > 0 {
		source := queue[0]
		queue = queue[1:]
		for _, target := range linksBySource[source] {
			referenced[target] = true
			enqueue(target)
		}
	}
	return referenced, nil
}

func canonicalCachedNotePath(cache *obsidian.NotePathCache, target string) string {
	target = filepath.ToSlash(target)
	if _, exists := cache.NotePaths[target]; !exists && !strings.HasSuffix(strings.ToLower(target), ".md") {
		if _, exists := cache.NotePaths[target+".md"]; exists {
			target += ".md"
		}
	}
	return target
}

func hasLegacyProcessMigrationPending(projectRoot string) bool {
	for _, path := range []string{
		legacyProcessMigrationManifestPath(projectRoot),
		filepath.Join(legacyProcessMigrationDir(projectRoot), "README.md"),
	} {
		if _, err := os.Stat(path); err == nil {
			return true
		}
	}
	return false
}

func hasLegacyProcessDocEvidence(projectRoot string) bool {
	for _, rel := range legacyProcessDocPaths {
		info, err := os.Stat(filepath.Join(projectRoot, filepath.FromSlash(rel)))
		if err == nil && !info.IsDir() {
			return true
		}
	}
	return false
}

func needsComplexDomainLegacyProcessMigration(projectRoot string, effectiveTemplates []string) bool {
	active := stringSet(effectiveTemplates)
	return active[templateAgenticEngineering] && active[templateComplexDomain] && hasLegacyProcessDocEvidence(projectRoot)
}

func hasAgenticEngineeringReplacementDocs(projectRoot string) bool {
	for _, name := range []string{
		"README.md",
		"testing-policy.md",
		"quality-gates.md",
		"documentation.md",
		"review-and-approval.md",
		"architecture.md",
		"release.md",
	} {
		info, err := os.Stat(filepath.Join(projectRoot, "docs", "engineering", name))
		if err != nil || info.IsDir() {
			return false
		}
	}
	return true
}

func prepareRetainedLegacyProcessDoc(content []byte) ([]byte, string, bool, error) {
	updated := insertLegacyProcessRetirementNotice(content)
	archived, originalStatus, changed, err := archiveActiveLegacyProcessSpec(updated)
	if err != nil {
		return content, "", false, err
	}
	return archived, originalStatus, changed || string(updated) != string(content), nil
}

func archiveActiveLegacyProcessSpec(content []byte) ([]byte, string, bool, error) {
	frontmatter, err := obsidian.ExtractFrontmatter(string(content))
	if err != nil || frontmatter == nil || !frontmatterStringEquals(frontmatter, "type", "ProcessSpec") {
		return content, "", false, err
	}
	status, ok := frontmatterString(frontmatter, "spec-status")
	if !ok || !strings.EqualFold(status, "active") {
		return content, "", false, nil
	}
	updated, changed, err := obsidian.SetFrontmatterProperty(string(content), "spec-status", "archived", true)
	return []byte(updated), status, changed, err
}

func frontmatterString(frontmatter map[string]interface{}, key string) (string, bool) {
	for candidate, value := range frontmatter {
		if !strings.EqualFold(candidate, key) {
			continue
		}
		text, ok := value.(string)
		return strings.TrimSpace(text), ok
	}
	return "", false
}

func frontmatterStringEquals(frontmatter map[string]interface{}, key, expected string) bool {
	value, ok := frontmatterString(frontmatter, key)
	return ok && strings.EqualFold(value, expected)
}

func insertLegacyProcessRetirementNotice(content []byte) []byte {
	text := string(content)
	if strings.Contains(text, legacyProcessRetirementSentinel) {
		return content
	}
	notice := legacyProcessRetirementSentinel + "\n" +
		"This is a historical record from the retired spec-driven starter, not current guidance. Current team policy lives in `docs/engineering/`. Reconcile any local policy still unique to this document there, then delete it.\n\n"
	if at := yamlFrontMatterEnd(text); at > 0 {
		return []byte(insertLegacyProcessNoticeAt(text, at, notice))
	}
	for offset := 0; offset < len(text); {
		line, end := markdownLine(text, offset)
		if strings.HasPrefix(strings.TrimSuffix(line, "\r"), "# ") {
			return []byte(insertLegacyProcessNoticeAt(text, end, notice))
		}
		offset = end
	}
	return []byte(notice + text)
}

func yamlFrontMatterEnd(text string) int {
	first, end := markdownLine(text, 0)
	if strings.TrimSuffix(first, "\r") != "---" || end == len(text) {
		return 0
	}
	for offset := end; offset < len(text); {
		line, next := markdownLine(text, offset)
		if strings.TrimSuffix(line, "\r") == "---" {
			return next
		}
		offset = next
	}
	return 0
}

func markdownLine(text string, offset int) (string, int) {
	if newline := strings.IndexByte(text[offset:], '\n'); newline >= 0 {
		end := offset + newline + 1
		return text[offset : end-1], end
	}
	return text[offset:], len(text)
}

func insertLegacyProcessNoticeAt(text string, at int, notice string) string {
	separator := ""
	if at > 0 && text[at-1] != '\n' {
		separator = "\n"
	}
	return text[:at] + separator + notice + text[at:]
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
	if err := obsidian.WriteFileAtomicPreservingMode(legacyProcessMigrationManifestPath(projectRoot), manifest, 0o644); err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString("# Agentic Engineering migration\n\n")
	b.WriteString("This manifest records legacy `docs/specs/process/` evidence retained for team reconciliation. Delete it after the team has completed the listed follow-up.\n\n")
	b.WriteString("| Path | Classification | SHA-256 | Original active status | Follow-up |\n| --- | --- | --- | --- | --- |\n")
	for _, record := range records {
		fmt.Fprintf(&b, "| `%s` | %s | `%s` | %s | %s |\n", record.Path, record.Classification, record.Fingerprint, firstNonEmpty(record.OriginalStatus, "-"), record.FollowUp)
	}
	return obsidian.WriteFileAtomicPreservingMode(filepath.Join(dir, "README.md"), []byte(b.String()), 0o644)
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

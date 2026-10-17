package init

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/fileio"
	"github.com/atomicobject/rhizome/pkg/markdownsyntax"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
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

func hasLegacyAgenticEngineeringState(cfg repoconfig.LocalConfig) bool {
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
func normalizeLegacyAgenticEngineeringState(cfg *repoconfig.LocalConfig) (bool, error) {
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
				if err := fileio.WriteFileAtomicPreservingMode(path, updated, 0o644); err != nil {
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
	frontmatter, err := markdownsyntax.ExtractFrontmatter(string(content))
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

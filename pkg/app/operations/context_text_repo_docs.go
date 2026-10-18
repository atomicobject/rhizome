package operations

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/app/contextpack"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/codepatterns"
	"github.com/atomicobject/rhizome/pkg/vault/ignore"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/bmatcuk/doublestar/v4"
)

// repoDocMaxTiersDefault controls how many tiers of CONTEXT.md files are included in vault_context
// when compression is disabled. Tier 0 = root CONTEXT.md, Tier 1 = first level where non-root
// CONTEXT.md appears, etc. Default 1 means: root + first code tier only.
const repoDocMaxTiersDefault = 1

// repoDocMaxTiersWithCompression is used when compression is enabled. Walk deeper into the tree
// to collect more module docs, which compression will synthesize into a dense overview.
const repoDocMaxTiersWithCompression = 3

// vaultContextIntent is the hardcoded intent for vault_context compression.
// This helps the LLM produce an excellent, token-dense repository overview.
const vaultContextIntent = "Produce a token-dense repository overview for an AI agent. " +
	"Prioritize: (1) key subsystems and their responsibilities, (2) filesystem organization and module boundaries, " +
	"(3) important patterns/conventions, (4) entry points and extension patterns. " +
	"Omit boilerplate, installation instructions, and content that doesn't help navigate or understand the codebase."

// isTestOrFixturePath returns true if the path looks like test/mock/fixture data.
func isTestOrFixturePath(rel string) bool {
	lower := strings.ToLower(rel)
	for _, marker := range []string{"testdata/", "test_data/", "mocks/", "mock/", "fixtures/", "fixture/", "__tests__/", "__mocks__/"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// pathDepth returns the number of path segments (slashes) in a relative path.
func pathDepth(rel string) int {
	if rel == "" || rel == "." {
		return 0
	}
	return strings.Count(filepath.ToSlash(rel), "/")
}

func buildRepoDocBlocks(ctx context.Context, projectRoot string, docPatterns []string, localCfg *localCfgInfo, vaultDef obsidian.VaultDefinition, tracker DedupeTracker, maxTiers int) ([]contextpack.Piece, map[string][]DedupeItem, error) {
	phaseCtx := indexingperf.WithPhase(ctx, indexingperf.AgentStartPhaseRepoDocInventory)
	done := indexingperf.StartSpan(phaseCtx, indexingperf.AgentStartPhaseRepoDocInventory)
	defer done(nil)
	if projectRoot == "" {
		return nil, nil, nil
	}
	docPatterns = obsidian.NormalizeDocPatterns(docPatterns)
	vaultPaths, _ := paths.NewVaultPaths(projectRoot)

	var allDocs []string
	var codeFileDirs []string // directories containing code files (for scoring)

	codeScan, codeIgnore := codeGlobs(localCfg)
	if len(codeScan) == 0 {
		// Reasonable fallback for repo docs ranking.
		codeScan = codepatterns.DefaultScanGlobs()
	}
	userExcludes := append([]string{}, codeIgnore...)
	if len(vaultDef.Excludes) > 0 {
		userExcludes = append(userExcludes, vaultDef.Excludes...)
	}
	matcher := ignore.LoadUnifiedMatcher(projectRoot, userExcludes)

	// Single walk collects both docs and code file directories.
	err := filepath.WalkDir(projectRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		relPath, relErr := vaultPaths.RelStrict(path)
		if relErr != nil {
			return nil
		}
		rel := relPath.String()
		if d.IsDir() {
			if matcher.IsIgnoredShallow(rel, true) {
				return filepath.SkipDir
			}
			return nil
		}
		if matcher.IsIgnoredShallow(rel, false) {
			return nil
		}
		if obsidian.MatchDocPatternNormalized(rel, docPatterns) {
			allDocs = append(allDocs, path)
		}
		if matchesAny(rel, codeScan) {
			codeFileDirs = append(codeFileDirs, filepath.Clean(filepath.Dir(path)))
		}
		return nil
	})
	indexingperf.AddCount(phaseCtx, indexingperf.AgentStartOpRepoWalks, 1)
	if err != nil {
		return nil, nil, err
	}
	if len(allDocs) == 0 {
		return nil, nil, nil
	}

	// Filter docs by depth tier: include root (depth 0) and up to repoDocMaxTiers
	// levels beyond the first non-root tier where docs appear.
	// Also filter out testdata/mocks paths.
	var docs []string
	minNonRootDepth := -1
	for _, d := range allDocs {
		rel := ""
		if relPath, err := vaultPaths.RelStrict(d); err == nil {
			rel = relPath.String()
		}
		if isTestOrFixturePath(rel) {
			continue
		}
		depth := pathDepth(filepath.Dir(rel)) // depth of the directory containing the doc
		if depth > 0 && (minNonRootDepth < 0 || depth < minNonRootDepth) {
			minNonRootDepth = depth
		}
	}
	maxDepth := maxTiers
	if minNonRootDepth > 0 {
		maxDepth = minNonRootDepth + maxTiers - 1
	}
	for _, d := range allDocs {
		rel := ""
		if relPath, err := vaultPaths.RelStrict(d); err == nil {
			rel = relPath.String()
		}
		if isTestOrFixturePath(rel) {
			continue
		}
		depth := pathDepth(filepath.Dir(rel))
		if depth <= maxDepth {
			docs = append(docs, d)
		}
	}
	if len(docs) == 0 {
		return nil, nil, nil
	}

	docDirSet := make(map[string]struct{}, len(docs))
	for _, d := range docs {
		docDirSet[filepath.Clean(filepath.Dir(d))] = struct{}{}
	}

	// Compute code counts by iterating collected code file directories.
	codeCounts := make(map[string]int, len(docDirSet))
	for _, codeDir := range codeFileDirs {
		// Attribute this code file to any ancestor doc dir(s).
		dir := codeDir
		for {
			if _, ok := docDirSet[dir]; ok {
				codeCounts[dir]++
			}
			if dir == projectRoot || dir == filepath.Dir(dir) {
				break
			}
			dir = filepath.Dir(dir)
		}
	}

	type scored struct {
		path  string
		score float64
	}
	scoredDocs := make([]scored, 0, len(docs))
	for _, d := range docs {
		dir := filepath.Clean(filepath.Dir(d))
		score := math.Log1p(float64(codeCounts[dir]))
		// Prefer root docs slightly.
		if filepath.Clean(dir) == filepath.Clean(projectRoot) {
			score += 5
		}
		scoredDocs = append(scoredDocs, scored{path: d, score: score})
	}
	sort.Slice(scoredDocs, func(i, j int) bool {
		if scoredDocs[i].score == scoredDocs[j].score {
			return scoredDocs[i].path < scoredDocs[j].path
		}
		return scoredDocs[i].score > scoredDocs[j].score
	})

	var pieces []contextpack.Piece
	nestedDedupe := make(map[string][]DedupeItem)
	pieces = append(pieces, contextpack.Piece{
		Key:      "repo-docs-header",
		Priority: 91,
		Score:    math.MaxFloat64,
		Text:     "## Repo docs",
	})
	for _, s := range scoredDocs {
		body, err := os.ReadFile(s.path)
		if err != nil {
			continue
		}
		content, _ := contextpack.TrimMarkdown(string(body), 16000)
		rel := filepath.ToSlash(s.path)
		if relPath, err := vaultPaths.RelStrict(s.path); err == nil && relPath.String() != "" {
			rel = relPath.String()
		}
		content = strings.TrimSpace(content)
		dedupeKey := noteDedupeKey(rel, vaultPaths)
		fp := pieceFingerprint(content)
		if tracker != nil && dedupeKey != "" && fp != "" && tracker.Seen(dedupeKey, fp) {
			continue
		}
		text := renderEmbeddedFileXML("repo-doc", rel, content, false)
		pieceKey := "doc:" + rel
		pieces = append(pieces, contextpack.Piece{
			Key:      pieceKey,
			Priority: 90,
			Score:    s.score,
			Text:     text,
		})
		if dedupeKey != "" && fp != "" {
			nestedDedupe[pieceKey] = append(nestedDedupe[pieceKey], DedupeItem{Key: dedupeKey, Fingerprint: fp})
		}
	}
	return pieces, nestedDedupe, nil
}

func codeGlobs(localCfg *localCfgInfo) (scan []string, ignore []string) {
	if localCfg == nil || localCfg.cfg == nil {
		return nil, repoconfig.DefaultCodeConfig.Ignore
	}
	scan, ignore = repoconfig.NormalizeCodeRefPatterns(*localCfg.cfg)
	if len(ignore) == 0 {
		ignore = append(ignore, repoconfig.DefaultCodeConfig.Ignore...)
	}
	return scan, ignore
}

func matchesAny(rel string, globs []string) bool {
	for _, pat := range globs {
		ok, _ := doublestar.Match(pat, rel)
		if ok {
			return true
		}
	}
	return false
}

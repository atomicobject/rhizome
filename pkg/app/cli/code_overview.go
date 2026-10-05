package actions

import (
	"container/heap"
	"context"
	"fmt"
	"math"
	"path/filepath"
	"sort"
	"strings"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semstore "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/app/contextpack"
	"github.com/atomicobject/rhizome/pkg/codefile"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/frontmatter"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// CodeOverviewIntel captures the intel + doc link capabilities needed for the overview.
type CodeOverviewIntel interface {
	IntelAnchors(ctx context.Context) ([]codeanchor.IntelAnchor, error)
	DocStatsForFQNs(ctx context.Context, lang string, fqns []string) (map[string]codeanchor.DocStats, error)
	DocLinksFromCodePath(ctx context.Context, srcPath string, limit int) ([]codeanchor.DocLink, error)
	// Graph scores for ranking (optional; methods may return empty maps if not available)
	GraphDocScoresByPaths(ctx context.Context, paths []string) (map[string]semstore.GraphDocScore, error)
	AnchorScoresByIDs(ctx context.Context, anchorIDs []string) (map[string]float64, error)
}

type CodeOverviewOptions struct {
	Vault          obsidian.VaultManager
	Notes          obsidian.NoteReader
	VaultDef       obsidian.VaultDefinition
	Intel          CodeOverviewIntel
	VaultPath      string
	Roots          []string
	DocPatterns    []string
	MaxEmptyLevels int
	BudgetChars    int
	LimitModules   int
	IncludeTests   bool
	// Depth is deprecated and ignored - progressive expansion is now used
	Depth int
}

type CodeOverviewResult struct {
	Modules   []CodeOverviewModule
	Truncated bool
	Warning   string
}

// OverviewNode represents a node in the hierarchical module tree.
// Nodes can have children, and we progressively expand high-signal nodes.
type OverviewNode struct {
	Path        string
	Depth       int
	Score       float64
	Stats       ModuleStats
	Docs        []ModuleDoc
	Anchors     []ModuleAnchor
	ContextBody string
	Children    []*OverviewNode
	FileCount   int
	Expanded    bool // Whether we've included this node's children
}

// CodeOverviewModule is the flattened output for compatibility
type CodeOverviewModule struct {
	Path              string // Directory path (e.g., "pkg/search/")
	Depth             int    // Depth level for hierarchical rendering
	Score             float64
	Stats             ModuleStats
	Docs              []ModuleDoc
	Anchors           []ModuleAnchor // Top anchors by PageRank/importance
	Tests             []string
	DocLinkCount      int
	FileCount         int      // Number of files in this directory
	Submodules        []string // Notable subdirectories
	ContextSummary    string   // Summary from the primary module doc (if present)
	PrimaryDocPattern string   // Primary doc filename (first doc pattern)
}

type ModuleStats struct {
	DocMentions int
	DocLinks    int
	Anchors     int
}

type ModuleDoc struct {
	Path    string
	Title   string
	Summary string
	Body    string // Truncated body content (first ~300 chars after frontmatter)
}

type ModuleAnchor struct {
	Name      string
	Kind      string
	FQN       string
	Lang      string
	Signature string
	Mentions  int
	Links     int
	Path      string
	Doc       string
}

// nodeHeap is a max-heap of OverviewNode pointers ordered by Score
type nodeHeap []*OverviewNode

func (h nodeHeap) Len() int           { return len(h) }
func (h nodeHeap) Less(i, j int) bool { return h[i].Score > h[j].Score } // Max heap
func (h nodeHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }

func (h *nodeHeap) Push(x any) {
	*h = append(*h, x.(*OverviewNode))
}

func (h *nodeHeap) Pop() any {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[0 : n-1]
	return x
}

// CodeOverview builds a structured overview using progressive expansion with a heap.
// It starts from root directories and expands high-signal modules to show their children.
// Output is hierarchical with header levels based on depth.
func CodeOverview(ctx context.Context, opts CodeOverviewOptions) (CodeOverviewResult, string, error) {
	opts.DocPatterns = obsidian.NormalizeDocPatterns(opts.DocPatterns)
	if opts.MaxEmptyLevels <= 0 {
		opts.MaxEmptyLevels = obsidian.FileContextConfigDefaults.MaxEmptyLevels
	}

	budget := opts.BudgetChars
	if budget <= 0 {
		budget = contextpack.DefaultBudgetChars
	}
	limit := opts.LimitModules
	if limit <= 0 {
		limit = 25
	}

	if opts.VaultDef.BasePath() == "" {
		if opts.Vault != nil {
			def, err := opts.Vault.Definition()
			if err != nil {
				return CodeOverviewResult{}, "", err
			}
			opts.VaultDef = def
		}
	}
	vaultPath := opts.VaultDef.BasePath()
	if opts.VaultPath != "" {
		vaultPath = opts.VaultPath
	}
	vaultPaths, _ := paths.NewVaultPaths(vaultPath)

	if opts.Intel == nil {
		msg := "Code index unavailable; check that code indexing is enabled (`rzm code enable`), then run `rzm index`."
		return CodeOverviewResult{Warning: msg}, msg, nil
	}

	anchors, err := opts.Intel.IntelAnchors(ctx)
	if err != nil {
		return CodeOverviewResult{}, "", err
	}

	rootChecks := buildRootChecks(vaultPaths, opts.Roots)

	// Group anchors by their immediate parent directory (at every level)
	byDir := map[string][]codeanchor.IntelAnchor{}
	fileSetByDir := map[string]map[string]struct{}{}
	var langFQNs = map[string][]string{}
	var allAnchorIDs []string

	for _, a := range anchors {
		filePath := filepath.ToSlash(strings.TrimPrefix(filepath.Clean(a.Path), "./"))
		if !rootChecks.match(filePath) {
			continue
		}
		if !opts.IncludeTests && isTestPath(filePath) {
			continue
		}

		// Add to every ancestor directory (for hierarchical tree building)
		dirPath := filepath.ToSlash(filepath.Dir(filePath))
		if dirPath == "." {
			dirPath = ""
		}

		// Store at immediate parent directory
		normalizedAnchor := a
		normalizedAnchor.Path = filePath
		byDir[dirPath] = append(byDir[dirPath], normalizedAnchor)
		if fileSetByDir[dirPath] == nil {
			fileSetByDir[dirPath] = make(map[string]struct{})
		}
		fileSetByDir[dirPath][filePath] = struct{}{}

		if a.FQN != "" {
			langFQNs[string(a.Lang)] = append(langFQNs[string(a.Lang)], a.FQN)
		}
		if a.AnchorID != "" {
			allAnchorIDs = append(allAnchorIDs, a.AnchorID)
		}
	}

	// Fetch doc stats
	docStatsByFQN := map[string]codeanchor.DocStats{}
	for lang, fqns := range langFQNs {
		stats, err := opts.Intel.DocStatsForFQNs(ctx, lang, fqns)
		if err != nil {
			return CodeOverviewResult{Warning: err.Error()}, err.Error(), nil
		}
		for fqn, ds := range stats {
			docStatsByFQN[fqn] = ds
		}
	}

	// Fetch anchor scores
	anchorScores := map[string]float64{}
	if len(allAnchorIDs) > 0 {
		if scores, err := opts.Intel.AnchorScoresByIDs(ctx, allAnchorIDs); err == nil {
			anchorScores = scores
		}
	}

	// Build tree structure: identify root directories and their children
	dirTree := buildDirectoryTree(byDir)

	// Compute scores for all directories
	dirScores := computeAllDirScores(ctx, opts, byDir, fileSetByDir, docStatsByFQN, anchorScores, vaultPath)

	// Use a heap to progressively expand high-signal modules
	modules := progressiveExpand(ctx, opts, dirTree, dirScores, byDir, fileSetByDir, docStatsByFQN, anchorScores, vaultPath, vaultPaths, limit)

	// Filter test directories
	if !opts.IncludeTests {
		var filtered []CodeOverviewModule
		for _, m := range modules {
			if isTestPath(m.Path) || isTestOnlyDirectory(byDir[m.Path]) {
				continue
			}
			filtered = append(filtered, m)
		}
		modules = filtered
	}

	res := CodeOverviewResult{Modules: modules}
	text, truncated := renderCodeOverviewMarkdownHierarchical(modules, budget)
	res.Truncated = truncated
	return res, text, nil
}

// buildDirectoryTree constructs a map of parent -> children directories
func buildDirectoryTree(byDir map[string][]codeanchor.IntelAnchor) map[string][]string {
	tree := make(map[string][]string)
	allDirs := make(map[string]struct{})

	for dir := range byDir {
		allDirs[dir] = struct{}{}
	}

	for dir := range allDirs {
		if dir == "" {
			continue
		}
		parent := filepath.ToSlash(filepath.Dir(dir))
		if parent == "." {
			parent = ""
		}
		tree[parent] = append(tree[parent], dir)
	}

	// Sort children for determinism
	for parent := range tree {
		sort.Strings(tree[parent])
	}

	return tree
}

// computeAllDirScores computes scores for all directories
func computeAllDirScores(ctx context.Context, opts CodeOverviewOptions, byDir map[string][]codeanchor.IntelAnchor, fileSetByDir map[string]map[string]struct{}, docStatsByFQN map[string]codeanchor.DocStats, anchorScores map[string]float64, vaultPath string) map[string]float64 {
	scores := make(map[string]float64)
	vaultPaths, _ := paths.NewVaultPaths(vaultPath)
	primaryPattern := primaryDocPattern(opts.DocPatterns)

	// Fetch graph doc scores for all files
	allFilePaths := make([]string, 0)
	for _, fileSet := range fileSetByDir {
		for filePath := range fileSet {
			allFilePaths = append(allFilePaths, filePath)
		}
	}
	fileDocScores := map[string]semstore.GraphDocScore{}
	if opts.Intel != nil && len(allFilePaths) > 0 {
		if s, err := opts.Intel.GraphDocScoresByPaths(ctx, allFilePaths); err == nil {
			fileDocScores = s
		}
	}

	for dirPath, dirAnchors := range byDir {
		stats := ModuleStats{}
		var maxPageRank float64

		for _, a := range dirAnchors {
			ds := docStatsByFQN[a.FQN]
			stats.DocMentions += ds.Mentions
			stats.DocLinks += ds.Links
			stats.Anchors++

			if pr := anchorScores[a.AnchorID]; pr > maxPageRank {
				maxPageRank = pr
			}
		}

		// Aggregate file-level graph scores
		var maxAuth float64
		var totalInbound int
		for filePath := range fileSetByDir[dirPath] {
			if sc, ok := fileDocScores[filePath]; ok {
				if sc.Authority > maxAuth {
					maxAuth = sc.Authority
				}
				totalInbound += sc.Inbound
			}
		}

		// Check for a primary module doc (first doc pattern).
		hasPrimaryDoc := false
		if vaultPath != "" && primaryPattern != "" {
			absDir := filepath.Join(vaultPath, filepath.FromSlash(dirPath))
			if _, ok := findDocMatchInDir(absDir, []string{primaryPattern}, vaultPaths); ok {
				hasPrimaryDoc = true
			}
		}

		entryPoints := countEntryPoints(dirAnchors, anchorScores)
		score := scoreDirectory(stats, maxPageRank, maxAuth, totalInbound, entryPoints)

		// Bonus for primary module doc presence
		if hasPrimaryDoc {
			score += 3.0
		}

		scores[dirPath] = score
	}

	return scores
}

// progressiveExpand uses a heap to select which modules to include and expand
func progressiveExpand(ctx context.Context, opts CodeOverviewOptions, dirTree map[string][]string, dirScores map[string]float64, byDir map[string][]codeanchor.IntelAnchor, fileSetByDir map[string]map[string]struct{}, docStatsByFQN map[string]codeanchor.DocStats, anchorScores map[string]float64, vaultPath string, vaultPaths paths.VaultPaths, limit int) []CodeOverviewModule {
	var modules []CodeOverviewModule
	included := make(map[string]bool)

	// Initialize heap with root-level directories
	h := &nodeHeap{}
	heap.Init(h)

	// Find roots: directories with no parent in byDir
	for dir := range byDir {
		parent := filepath.ToSlash(filepath.Dir(dir))
		if parent == "." {
			parent = ""
		}
		if _, hasParent := byDir[parent]; !hasParent || parent == "" {
			// This is a root
			if !included[dir] {
				heap.Push(h, &OverviewNode{
					Path:  dir,
					Depth: depthOf(dir),
					Score: dirScores[dir],
				})
				included[dir] = true
			}
		}
	}

	// Also add the root ("") if it has direct anchors
	if _, hasRoot := byDir[""]; hasRoot && !included[""] {
		heap.Push(h, &OverviewNode{
			Path:  "",
			Depth: 0,
			Score: dirScores[""],
		})
		included[""] = true
	}

	// Progressively expand
	for h.Len() > 0 && len(modules) < limit {
		node := heap.Pop(h).(*OverviewNode)

		// Build module for this node
		module := buildModuleFromDir(ctx, opts, node.Path, node.Depth, byDir, fileSetByDir, docStatsByFQN, anchorScores, vaultPath, dirScores[node.Path], vaultPaths)
		modules = append(modules, module)

		// Add children to heap if this is a high-signal node
		// Only expand if score is above threshold OR we haven't reached limit/2
		shouldExpand := node.Score > 2.0 || len(modules) < limit/2
		if shouldExpand {
			for _, child := range dirTree[node.Path] {
				if !included[child] {
					heap.Push(h, &OverviewNode{
						Path:  child,
						Depth: depthOf(child),
						Score: dirScores[child],
					})
					included[child] = true
				}
			}
		}
	}

	// Sort by depth first (for hierarchical rendering), then by path within same depth
	sort.SliceStable(modules, func(i, j int) bool {
		if modules[i].Depth != modules[j].Depth {
			return modules[i].Depth < modules[j].Depth
		}
		return modules[i].Path < modules[j].Path
	})

	return modules
}

func depthOf(path string) int {
	if path == "" || path == "." {
		return 0
	}
	return strings.Count(filepath.ToSlash(path), "/") + 1
}

// buildModuleFromDir constructs a CodeOverviewModule from a directory's anchors
func buildModuleFromDir(ctx context.Context, opts CodeOverviewOptions, dirPath string, depth int, byDir map[string][]codeanchor.IntelAnchor, fileSetByDir map[string]map[string]struct{}, docStatsByFQN map[string]codeanchor.DocStats, anchorScores map[string]float64, vaultPath string, score float64, vaultPaths paths.VaultPaths) CodeOverviewModule {
	dirAnchors := byDir[dirPath]
	stats := ModuleStats{}
	var anchorSummaries []ModuleAnchor

	for _, a := range dirAnchors {
		ds := docStatsByFQN[a.FQN]
		stats.DocMentions += ds.Mentions
		stats.DocLinks += ds.Links
		stats.Anchors++

		anchorSummaries = append(anchorSummaries, ModuleAnchor{
			Name:      anchorName(a),
			Kind:      a.Kind,
			FQN:       a.FQN,
			Lang:      string(a.Lang),
			Signature: a.Signature,
			Mentions:  ds.Mentions,
			Links:     ds.Links,
			Path:      a.Path,
			Doc:       strings.TrimSpace(a.DocComment),
		})
	}

	// Sort anchors by PageRank
	sort.Slice(anchorSummaries, func(i, j int) bool {
		prI := anchorScores[findAnchorIDFromAnchors(dirAnchors, anchorSummaries[i])]
		prJ := anchorScores[findAnchorIDFromAnchors(dirAnchors, anchorSummaries[j])]
		if prI != prJ {
			return prI > prJ
		}
		return anchorSummaries[i].Name < anchorSummaries[j].Name
	})
	if len(anchorSummaries) > 5 {
		anchorSummaries = anchorSummaries[:5]
	}

	// Collect doc links
	var allDocLinks []codeanchor.DocLink
	for filePath := range fileSetByDir[dirPath] {
		if opts.Intel != nil {
			links, _ := opts.Intel.DocLinksFromCodePath(ctx, filePath, 10)
			allDocLinks = append(allDocLinks, links...)
		}
	}
	docMap := dedupeDocs(allDocLinks)

	docPatterns := opts.DocPatterns
	primaryPattern := primaryDocPattern(docPatterns)
	primaryDocPath := ""
	if vaultPath != "" && primaryPattern != "" {
		absDir := filepath.Join(vaultPath, filepath.FromSlash(dirPath))
		if match, ok := findDocMatchInDir(absDir, []string{primaryPattern}, vaultPaths); ok {
			primaryDocPath = match.Path
		}
	}

	moduleDocs := buildModuleDocs(opts.Notes, dirPath, docMap, vaultPaths, docPatterns, primaryPattern, opts.MaxEmptyLevels)
	if primaryDocPath != "" {
		found := false
		for _, d := range moduleDocs {
			if d.Path == primaryDocPath {
				found = true
				break
			}
		}
		if !found {
			title := filepath.Base(primaryDocPath)
			summary := ""
			body := ""
			if fact, ok := NoteFactsFromReader(opts.Notes).LookupFact(primaryDocPath); ok {
				blessed := frontmatter.FilterBlessed(fact.Frontmatter)
				summary = firstSummary(blessed)
				if primaryPattern != "" && obsidian.MatchDocPatternNormalized(primaryDocPath, []string{primaryPattern}) {
					body = moduleDocContextBody(fact, 300)
				}
			}
			moduleDocs = append(moduleDocs, ModuleDoc{
				Path:    primaryDocPath,
				Title:   strings.TrimSuffix(title, filepath.Ext(title)),
				Summary: summary,
				Body:    body,
			})
		}
	}

	if len(moduleDocs) > 5 {
		moduleDocs = moduleDocs[:5]
	}

	contextSummary := ""
	moduleDirPrimary := ""
	if primaryPattern != "" {
		moduleDirPrimary = filepath.ToSlash(filepath.Join(dirPath, primaryPattern))
	}
	for _, d := range moduleDocs {
		if primaryPattern != "" && obsidian.MatchDocPatternNormalized(d.Path, []string{primaryPattern}) && d.Summary != "" {
			// Only use summary if the doc is directly in this module's directory
			// (not an ancestor doc). This avoids repeating root CONTEXT.md summary in every module.
			if d.Path == moduleDirPrimary || filepath.Dir(d.Path) == dirPath {
				contextSummary = d.Summary
				break
			}
		}
	}

	tests := collectTests(dirAnchors)
	submodules := findSubmodules(dirPath, byDir)

	return CodeOverviewModule{
		Path:              dirPath,
		Depth:             depth,
		Score:             score,
		Stats:             stats,
		Docs:              moduleDocs,
		Anchors:           anchorSummaries,
		Tests:             tests,
		DocLinkCount:      len(allDocLinks),
		FileCount:         len(fileSetByDir[dirPath]),
		Submodules:        submodules,
		ContextSummary:    contextSummary,
		PrimaryDocPattern: primaryPattern,
	}
}

func findAnchorIDFromAnchors(anchors []codeanchor.IntelAnchor, target ModuleAnchor) string {
	for _, a := range anchors {
		if a.FQN == target.FQN && a.Path == target.Path {
			return a.AnchorID
		}
	}
	return ""
}

func anchorName(a codeanchor.IntelAnchor) string {
	if a.Symbol != "" {
		return a.Symbol
	}
	if a.FQN != "" {
		return a.FQN
	}
	return filepath.Base(a.Path)
}

func findSubmodules(dirPath string, byDir map[string][]codeanchor.IntelAnchor) []string {
	dirPath = filepath.ToSlash(filepath.Clean(dirPath))
	if dirPath == "." {
		dirPath = ""
	}
	prefix := dirPath
	if prefix != "" {
		prefix += "/"
	}

	seen := make(map[string]struct{})
	var submodules []string
	for otherDir := range byDir {
		otherDir = filepath.ToSlash(filepath.Clean(otherDir))
		if strings.HasPrefix(otherDir, prefix) && otherDir != dirPath {
			// Extract the immediate subdirectory
			remainder := strings.TrimPrefix(otherDir, prefix)
			parts := strings.Split(remainder, "/")
			if len(parts) > 0 {
				subDir := prefix + parts[0]
				if _, exists := seen[subDir]; !exists {
					seen[subDir] = struct{}{}
					submodules = append(submodules, subDir)
				}
			}
		}
	}
	sort.Strings(submodules)
	if len(submodules) > 5 {
		submodules = submodules[:5]
	}
	return submodules
}

func scoreDirectory(stats ModuleStats, maxPageRank, authority float64, inboundEdges int, entryPoints int) float64 {
	score := 0.0
	// Graph signals (weighted heavily)
	score += 5 * maxPageRank                       // Max anchor PageRank in directory
	score += 4 * authority                         // HITS authority score
	score += 2 * math.Log1p(float64(inboundEdges)) // Inbound references
	score += 3 * math.Log1p(float64(entryPoints))  // Entry point density (high-PageRank anchors)

	// Traditional signals (still useful)
	score += 3 * math.Log1p(float64(stats.DocMentions))
	score += 2 * math.Log1p(float64(stats.DocLinks))
	score += 1 * math.Log1p(float64(stats.Anchors))

	// Require meaningful signal: avoid including modules with only 1-2 anchors and no docs
	if stats.DocMentions == 0 && stats.DocLinks == 0 && stats.Anchors < 3 {
		score *= 0.1 // Heavily penalize low-signal modules
	}

	return score
}

type docNote struct {
	Path string
}

func dedupeDocs(links []codeanchor.DocLink) map[string]docNote {
	out := map[string]docNote{}
	for _, l := range links {
		if l.DstPath == "" {
			continue
		}
		if _, exists := out[l.DstPath]; exists {
			continue
		}
		out[l.DstPath] = docNote{Path: l.DstPath}
	}
	return out
}

// moduleDocContextBody renders projected note evidence without inferring
// syntax from content. Markdown retains its legacy formatting through the
// exact FormatID-gated adapter; every other provider supplies search regions.
func moduleDocContextBody(fact NoteFact, maxChars int) string {
	if fact.Format == noteformat.FormatID("markdown") {
		return markdownContextBodyCompat(fact.Content, maxChars)
	}
	return searchRegionContextBody(fact.SearchRegions, maxChars)
}

// markdownContextBodyCompat is the named Markdown-only compatibility adapter
// for legacy module-doc rendering. Do not call it for another FormatID.
func markdownContextBodyCompat(content string, maxChars int) string {
	if maxChars <= 0 {
		return ""
	}
	if strings.TrimSpace(content) == "" {
		return ""
	}

	// Strip frontmatter (find the second --- marker)
	if strings.HasPrefix(content, "---") {
		// Find the closing --- of frontmatter
		lines := strings.Split(content, "\n")
		bodyStart := 0
		for i := 1; i < len(lines); i++ {
			if strings.TrimSpace(lines[i]) == "---" {
				// Found closing marker
				bodyStart = i + 1
				break
			}
		}
		if bodyStart < len(lines) {
			content = strings.TrimSpace(strings.Join(lines[bodyStart:], "\n"))
		}
	}

	// Strip leading markdown title
	if strings.HasPrefix(content, "#") {
		lines := strings.Split(content, "\n")
		if len(lines) > 1 {
			content = strings.TrimSpace(strings.Join(lines[1:], "\n"))
		}
	}

	// Trim to maxChars, preferring paragraph break
	if len(content) > maxChars {
		trimmed := content[:maxChars]
		// Try to break at paragraph boundary (double newline)
		if idx := strings.LastIndex(trimmed, "\n\n"); idx > 0 {
			trimmed = trimmed[:idx]
		} else if idx := strings.LastIndex(trimmed, "\n"); idx > maxChars/2 {
			// Fallback to last line break within reasonable distance
			trimmed = trimmed[:idx]
		}
		trimmed = strings.TrimSpace(trimmed)
		// Add ellipsis if we actually trimmed
		if len(trimmed) < len(content) && trimmed != "" {
			trimmed += "…"
		}
		return trimmed
	}
	return strings.TrimSpace(content)
}

func searchRegionContextBody(regions []noteformat.SearchRegionFact, maxChars int) string {
	if maxChars <= 0 {
		return ""
	}
	visible := make([]string, 0, len(regions))
	for _, region := range regions {
		if region.Kind != noteformat.SearchRegionVisible || strings.TrimSpace(region.Text) == "" {
			continue
		}
		visible = append(visible, strings.TrimSpace(region.Text))
	}
	return truncateContextBody(strings.Join(visible, "\n\n"), maxChars)
}

func truncateContextBody(content string, maxChars int) string {
	content = strings.TrimSpace(content)
	if len(content) <= maxChars {
		return content
	}
	trimmed := content[:maxChars]
	if idx := strings.LastIndex(trimmed, "\n\n"); idx > 0 {
		trimmed = trimmed[:idx]
	} else if idx := strings.LastIndex(trimmed, "\n"); idx > maxChars/2 {
		trimmed = trimmed[:idx]
	}
	trimmed = strings.TrimSpace(trimmed)
	if trimmed != "" {
		return trimmed + "…"
	}
	return ""
}

func primaryDocPattern(patterns []string) string {
	patterns = obsidian.NormalizeDocPatterns(patterns)
	if len(patterns) == 0 {
		return ""
	}
	return patterns[0]
}

func buildModuleDocs(notes obsidian.NoteReader, modulePath string, docs map[string]docNote, vaultPaths paths.VaultPaths, docPatterns []string, primaryPattern string, maxEmptyLevels int) []ModuleDoc {
	if len(docs) == 0 {
		docs = map[string]docNote{}
	}
	docPatterns = obsidian.NormalizeDocPatterns(docPatterns)
	if vaultPaths.Root() != "" {
		absDir := filepath.Join(vaultPaths.Root(), filepath.FromSlash(modulePath))
		for _, match := range collectAncestorDocMatches(absDir, vaultPaths, docPatterns, maxEmptyLevels) {
			if _, exists := docs[match.Path]; !exists {
				docs[match.Path] = docNote{Path: match.Path}
			}
		}
	}
	var out []ModuleDoc
	for path := range docs {
		// Normalize path separators for consistent handling across platforms
		path = filepath.ToSlash(path)
		title := filepath.Base(path)
		summary := ""
		body := ""
		if fact, ok := NoteFactsFromReader(notes).LookupFact(path); ok {
			blessed := frontmatter.FilterBlessed(fact.Frontmatter)
			summary = firstSummary(blessed)
			// Extract body content for primary module docs.
			if primaryPattern != "" && obsidian.MatchDocPatternNormalized(path, []string{primaryPattern}) {
				body = moduleDocContextBody(fact, 300)
			}
		}
		out = append(out, ModuleDoc{
			Path:    path,
			Title:   strings.TrimSuffix(title, filepath.Ext(title)),
			Summary: summary,
			Body:    body,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Summary != out[j].Summary {
			return out[i].Summary > out[j].Summary
		}
		return out[i].Path < out[j].Path
	})
	return out
}

func firstSummary(fm map[string]interface{}) string {
	if len(fm) == 0 {
		return ""
	}
	for _, key := range frontmatter.BlessedKeys {
		v, ok := fm[strings.ToLower(key)]
		if !ok {
			continue
		}
		if s, ok := v.(string); ok {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

// isTestPath returns true if the path represents a test file or test directory.
// Detects:
// - Directories: tests/, test/, __tests__/, spec/, _test/, fixtures/, testdata/
// - Go: *_test.go, *_bench.go
// - Python: test_*.py, *_test.py, conftest.py
// - TS/JS: *.test.ts, *.test.js, *.spec.ts, *.spec.js
// - General: paths containing /tests/ or /test/ segments
func isTestPath(path string) bool {
	path = filepath.ToSlash(path)
	base := filepath.Base(path)
	dir := filepath.Dir(path)

	// Check for test directories
	testDirs := []string{"tests", "test", "__tests__", "spec", "_test", "fixtures", "testdata", ".test", "testing"}
	for _, testDir := range testDirs {
		if base == testDir || strings.Contains(dir, "/"+testDir+"/") || strings.HasPrefix(dir, testDir+"/") || strings.HasSuffix(dir, "/"+testDir) {
			return true
		}
	}

	// Check for test file patterns
	ext := strings.ToLower(filepath.Ext(path))
	baseLower := strings.ToLower(base)

	// Go: *_test.go, *_bench.go
	if ext == ".go" && (strings.HasSuffix(baseLower, "_test.go") || strings.HasSuffix(baseLower, "_bench.go")) {
		return true
	}

	// Python: test_*.py, *_test.py, conftest.py
	if ext == ".py" {
		if strings.HasPrefix(baseLower, "test_") || strings.HasSuffix(baseLower, "_test.py") || baseLower == "conftest.py" {
			return true
		}
	}

	// TS/JS: *.test.ts, *.test.js, *.spec.ts, *.spec.js
	if codefile.IsTypeScriptJavaScriptExtension(ext) {
		if strings.Contains(baseLower, ".test.") || strings.Contains(baseLower, ".spec.") {
			return true
		}
	}

	return false
}

func collectTests(anchors []codeanchor.IntelAnchor) []string {
	seen := map[string]struct{}{}
	var tests []string
	for _, a := range anchors {
		if isTestPath(a.Path) {
			if _, ok := seen[a.Path]; ok {
				continue
			}
			seen[a.Path] = struct{}{}
			tests = append(tests, a.Path)
		}
	}
	sort.Strings(tests)
	return tests
}

// isTestOnlyDirectory returns true if all anchors in a directory are test files.
func isTestOnlyDirectory(anchors []codeanchor.IntelAnchor) bool {
	if len(anchors) == 0 {
		return false
	}
	for _, a := range anchors {
		if !isTestPath(a.Path) {
			return false
		}
	}
	return true
}

// countEntryPoints counts the number of anchors with PageRank above the threshold.
func countEntryPoints(anchors []codeanchor.IntelAnchor, anchorScores map[string]float64) int {
	const threshold = 0.1
	count := 0
	for _, a := range anchors {
		if score, ok := anchorScores[a.AnchorID]; ok && score > threshold {
			count++
		}
	}
	return count
}

type rootMatcher struct {
	roots []string
}

func buildRootChecks(vaultPaths paths.VaultPaths, roots []string) rootMatcher {
	if len(roots) == 0 {
		return rootMatcher{}
	}
	var relRoots []string
	for _, r := range roots {
		if r == "" {
			continue
		}
		cleaned := filepath.ToSlash(filepath.Clean(r))
		if filepath.IsAbs(cleaned) {
			if rel, err := vaultPaths.RelStrict(cleaned); err == nil {
				cleaned = rel.String()
			} else {
				continue
			}
		} else {
			cleaned = string(paths.Normalize(cleaned))
		}
		cleaned = strings.TrimSuffix(cleaned, "/")
		relRoots = append(relRoots, cleaned)
	}
	return rootMatcher{roots: relRoots}
}

func (m rootMatcher) match(relPath string) bool {
	if len(m.roots) == 0 {
		return true
	}
	relPath = strings.TrimSuffix(filepath.ToSlash(relPath), "/")
	for _, r := range m.roots {
		if r == "" {
			return true
		}
		if relPath == r || strings.HasPrefix(relPath, r+"/") {
			return true
		}
	}
	return false
}

// renderCodeOverviewMarkdownHierarchical renders modules with header levels based on depth.
// Depth 1 = ##, Depth 2 = ###, Depth 3+ = ####
func renderCodeOverviewMarkdownHierarchical(modules []CodeOverviewModule, budget int) (string, bool) {
	var b strings.Builder
	b.WriteString("# Code Overview\n")
	truncated := false

	for i, m := range modules {
		section := renderModuleHierarchical(m)
		if budget > 0 && b.Len()+len(section) > budget {
			truncated = true
			break
		}
		if i == 0 {
			b.WriteString(section)
		} else {
			b.WriteString("\n")
			b.WriteString(section)
		}
	}
	if truncated {
		b.WriteString("\n\n(truncated; increase --budget-chars or --limit-modules to see more)\n")
	}
	return b.String(), truncated
}

// renderModuleHierarchical renders a module with header level based on its depth.
func renderModuleHierarchical(m CodeOverviewModule) string {
	var b strings.Builder

	// Determine header level: depth 1 = ##, depth 2 = ###, depth 3+ = ####
	headerLevel := m.Depth + 1
	if headerLevel < 2 {
		headerLevel = 2
	}
	if headerLevel > 4 {
		headerLevel = 4
	}
	header := strings.Repeat("#", headerLevel)

	b.WriteString(header)
	b.WriteString(" ")
	if m.Path == "" || m.Path == "." {
		b.WriteString("(root)")
	} else {
		b.WriteString(m.Path)
		if !strings.HasSuffix(m.Path, "/") {
			b.WriteString("/")
		}
	}
	b.WriteString("\n")

	// Show primary module doc body if available - prefer the one in this module's directory
	contextBody := ""
	primaryPattern := m.PrimaryDocPattern
	if primaryPattern == "" {
		primaryPattern = primaryDocPattern(nil)
	}
	moduleDirPrimary := ""
	if primaryPattern != "" {
		moduleDirPrimary = filepath.ToSlash(filepath.Join(m.Path, primaryPattern))
	}
	for _, d := range m.Docs {
		if primaryPattern != "" && obsidian.MatchDocPatternNormalized(d.Path, []string{primaryPattern}) && d.Body != "" {
			// Only show body if the primary doc is directly in this module's directory
			// (not an ancestor doc). This avoids repeating root CONTEXT.md in every module.
			if d.Path == moduleDirPrimary || filepath.Dir(d.Path) == m.Path {
				contextBody = d.Body
				break
			}
			// No fallback to ancestor docs - that causes repetition of root intro text
		}
	}
	if contextBody != "" {
		b.WriteString(contextBody)
		b.WriteString("\n\n")
	} else if m.ContextSummary != "" {
		// Fall back to summary if no body available
		b.WriteString(m.ContextSummary)
		b.WriteString("\n\n")
	}

	// Stats line: files, docs, anchors
	stats := fmt.Sprintf("_files %d | docs %d | anchors %d_", m.FileCount, m.Stats.DocMentions+m.Stats.DocLinks, m.Stats.Anchors)
	if len(m.Tests) > 0 {
		stats = fmt.Sprintf("%s | tests %d", stats, len(m.Tests))
	}
	b.WriteString(stats)
	b.WriteString("\n")

	// Key exports (top anchors)
	if len(m.Anchors) > 0 {
		b.WriteString("- Key exports: ")
		for i, a := range m.Anchors {
			if i > 0 {
				b.WriteString("; ")
			}
			b.WriteString(a.Name)
			if a.Signature != "" {
				sig := a.Signature
				if len(sig) > 60 {
					sig = sig[:57] + "…"
				}
				b.WriteString(sig)
			}
			if a.Kind != "" && a.Kind != "module" {
				b.WriteString(" (")
				b.WriteString(a.Kind)
				b.WriteString(")")
			}
		}
		b.WriteString("\n")
	}

	// Submodules
	if len(m.Submodules) > 0 {
		b.WriteString("- Submodules: ")
		for i, sub := range m.Submodules {
			if i > 0 {
				b.WriteString("; ")
			}
			base := filepath.Base(sub)
			if base == "" {
				base = sub
			}
			b.WriteString(base)
		}
		b.WriteString("\n")
	}

	// Docs (excluding the primary module doc which is already shown)
	if len(m.Docs) > 0 {
		nonContextDocs := make([]ModuleDoc, 0)
		for _, d := range m.Docs {
			if primaryPattern == "" || !obsidian.MatchDocPatternNormalized(d.Path, []string{primaryPattern}) {
				nonContextDocs = append(nonContextDocs, d)
			}
		}
		if len(nonContextDocs) > 0 {
			b.WriteString("- Docs: ")
			for i, d := range nonContextDocs {
				if i > 0 {
					b.WriteString("; ")
				}
				b.WriteString(d.Title)
				if d.Summary != "" {
					b.WriteString(" — ")
					summary := d.Summary
					if len(summary) > 100 {
						summary = summary[:97] + "…"
					}
					b.WriteString(summary)
				}
			}
			b.WriteString("\n")
		}
	}

	return b.String()
}

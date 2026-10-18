package operations

import (
	"context"
	"path/filepath"
	"strings"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/paths"
	semstore "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
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
	Vault          repoconfig.VaultManager
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

// CodeOverview builds a structured overview using progressive expansion with a heap.
// It starts from root directories and expands high-signal modules to show their children.
// Output is hierarchical with header levels based on depth.
func CodeOverview(ctx context.Context, opts CodeOverviewOptions) (CodeOverviewResult, string, error) {
	opts.DocPatterns = obsidian.NormalizeDocPatterns(opts.DocPatterns)
	if opts.MaxEmptyLevels <= 0 {
		opts.MaxEmptyLevels = repoconfig.FileContextConfigDefaults.MaxEmptyLevels
	}

	budget := opts.BudgetChars
	if budget <= 0 {
		budget = repoconfig.DefaultBudgetChars
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

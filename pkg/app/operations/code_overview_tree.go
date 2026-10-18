package operations

import (
	"container/heap"
	"context"
	"math"
	"path/filepath"
	"sort"
	"strings"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/paths"
	semstore "github.com/atomicobject/rhizome/pkg/store"
)

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

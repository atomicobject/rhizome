package retrieval

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

type LocalDocsRetriever struct {
	VaultPath      string
	DocPatterns    []string
	MaxEmptyLevels int
	SubmoduleDepth int
	Limit          int
}

func (r *LocalDocsRetriever) Name() string { return "local_docs" }

func (r *LocalDocsRetriever) Retrieve(ctx context.Context, spec search.QuerySpec) ([]search.Candidate, error) {
	if len(spec.ExplicitSeedPaths) == 0 {
		return nil, nil
	}
	patterns := obsidian.NormalizeDocPatterns(r.DocPatterns)
	if len(patterns) == 0 {
		return nil, nil
	}
	maxEmpty := r.MaxEmptyLevels
	if maxEmpty <= 0 {
		maxEmpty = obsidian.FileContextConfigDefaults.MaxEmptyLevels
	}
	submoduleDepth := r.SubmoduleDepth
	if submoduleDepth <= 0 {
		submoduleDepth = 2
	}
	limit := r.Limit
	if limit <= 0 {
		limit = max(8, min(24, spec.Limits.Total))
	}
	vaultPaths, _ := paths.NewVaultPaths(r.VaultPath)

	seen := map[string]struct{}{}
	out := make([]search.Candidate, 0, limit)
	addDoc := func(match localDocMatch, relType string, depth int, seedPath string) {
		if len(out) >= limit {
			return
		}
		h := knowledge.NoteHandle(match.Path)
		key := h.String()
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		score := 1.0 - 0.08*float64(depth)
		if relType == "submodule_doc" {
			score -= 0.08
		}
		if score < 0.45 {
			score = 0.45
		}
		out = append(out, search.Candidate{
			Handle: h,
			Owner:  h,
			Evidence: []search.Evidence{{
				Type:     relType,
				RawScore: score,
				Source:   "local_docs",
				Details: map[string]string{
					"seed":  seedPath,
					"depth": strconvItoa(depth),
				},
			}},
			Type:       "note",
			Path:       match.Path,
			Title:      filepath.Base(match.Path),
			ChunkIndex: -1,
			NoteID:     match.Path,
			DocClass:   search.DocClassModule,
			PrimaryDoc: strings.EqualFold(match.Pattern, patterns[0]),
		})
	}

	for _, seedPath := range spec.ExplicitSeedPaths {
		if ctx.Err() != nil || len(out) >= limit {
			break
		}
		absPath, ok := safeJoinVaultPath(r.VaultPath, seedPath)
		if !ok {
			continue
		}
		info, err := os.Stat(absPath)
		if err != nil {
			continue
		}
		startDir := absPath
		isDir := info.IsDir()
		if !isDir {
			startDir = filepath.Dir(absPath)
		}
		for depth, match := range collectAncestorLocalDocMatches(startDir, vaultPaths, patterns, maxEmpty) {
			addDoc(match, "module_doc", depth, seedPath)
		}
		if isDir {
			for _, match := range collectSubmoduleLocalDocMatches(absPath, vaultPaths, patterns, submoduleDepth, limit-len(out)) {
				addDoc(match, "submodule_doc", match.Depth, seedPath)
				if len(out) >= limit {
					break
				}
			}
		}
	}
	return out, nil
}

type localDocMatch struct {
	Path    string
	Dir     string
	Pattern string
	Depth   int
}

func collectAncestorLocalDocMatches(startDir string, rootPaths paths.VaultPaths, patterns []string, maxEmpty int) []localDocMatch {
	if startDir == "" {
		return nil
	}
	root := rootPaths.Root()
	if root == "" {
		root = startDir
	}
	var docs []localDocMatch
	emptyStreak := 0
	depth := 0
	for dir := startDir; ; {
		match, ok := findLocalDocMatchInDir(dir, patterns, rootPaths, depth)
		if ok {
			emptyStreak = 0
			docs = append(docs, match)
		} else {
			emptyStreak++
			if maxEmpty > 0 && emptyStreak >= maxEmpty {
				break
			}
		}
		if dir == root {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir || !isSubdir(parent, rootPaths) {
			break
		}
		dir = parent
		depth++
	}
	return docs
}

func collectSubmoduleLocalDocMatches(dirPath string, rootPaths paths.VaultPaths, patterns []string, maxDepth, limit int) []localDocMatch {
	if maxDepth <= 0 || limit <= 0 {
		return nil
	}
	var out []localDocMatch
	errStop := context.Canceled
	_ = filepath.WalkDir(dirPath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		if path == dirPath {
			return nil
		}
		rel, err := filepath.Rel(dirPath, path)
		if err != nil {
			return nil
		}
		depth := 1
		if rel != "." {
			depth = len(strings.Split(filepath.ToSlash(rel), "/"))
		}
		if depth > maxDepth {
			return filepath.SkipDir
		}
		if match, ok := findLocalDocMatchInDir(path, patterns, rootPaths, depth); ok {
			out = append(out, match)
			if len(out) >= limit {
				return errStop
			}
		}
		return nil
	})
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Depth != out[j].Depth {
			return out[i].Depth < out[j].Depth
		}
		return out[i].Path < out[j].Path
	})
	return out
}

func findLocalDocMatchInDir(dir string, patterns []string, rootPaths paths.VaultPaths, depth int) (localDocMatch, bool) {
	for _, pattern := range patterns {
		candidate := filepath.Join(dir, pattern)
		info, err := os.Stat(candidate)
		if err != nil || info.IsDir() {
			continue
		}
		relPath := filepath.ToSlash(candidate)
		relDir := filepath.ToSlash(dir)
		if rel, err := rootPaths.RelStrict(candidate); err == nil && rel.String() != "" {
			relPath = rel.String()
		}
		if rel, err := rootPaths.RelStrict(dir); err == nil && rel.String() != "" {
			relDir = rel.String()
		}
		return localDocMatch{
			Path:    relPath,
			Dir:     relDir,
			Pattern: pattern,
			Depth:   depth,
		}, true
	}
	return localDocMatch{}, false
}

func safeJoinVaultPath(vaultPath, rel string) (string, bool) {
	vaultPath = strings.TrimSpace(vaultPath)
	rel = strings.TrimSpace(rel)
	if vaultPath == "" || rel == "" {
		return "", false
	}
	vaultPaths, err := paths.NewVaultPaths(vaultPath)
	if err != nil || vaultPaths.Root() == "" {
		return "", false
	}
	relPath, err := vaultPaths.RelStrict(rel)
	if err != nil || relPath == "" {
		return "", false
	}
	abs, err := vaultPaths.Abs(relPath)
	if err != nil || abs == "" {
		return "", false
	}
	return abs.String(), true
}

func isSubdir(path string, rootPaths paths.VaultPaths) bool {
	root := rootPaths.Root()
	if root == "" {
		return true
	}
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func strconvItoa(v int) string { return strconv.Itoa(v) }

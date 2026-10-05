package retrieval

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
)

type ExplicitSeedRetriever struct {
	VaultPath           string
	Limit               int
	IncludeSiblingCode  bool
	IncludeSiblingTests bool
}

func (r *ExplicitSeedRetriever) Name() string { return "explicit_seeds" }

func (r *ExplicitSeedRetriever) Retrieve(ctx context.Context, spec search.QuerySpec) ([]search.Candidate, error) {
	if !spec.HasExplicitSeeds || len(spec.ExplicitSeedPaths) == 0 {
		return nil, nil
	}
	limit := r.Limit
	if limit <= 0 {
		limit = max(4, min(12, spec.Limits.Total))
	}

	seen := map[string]struct{}{}
	out := make([]search.Candidate, 0, limit)
	addCode := func(relPath string, score float64, exact bool) {
		if len(out) >= limit {
			return
		}
		h := knowledge.FileHandle(relPath)
		key := h.String()
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		evidence := []search.Evidence{{
			Type:     "explicit_seed",
			RawScore: score,
			Source:   "explicit_seeds",
		}}
		if exact {
			evidence = append(evidence, search.Evidence{Type: "path_exact", RawScore: 1, Source: "explicit_seeds"})
		}
		out = append(out, search.Candidate{
			Handle:   h,
			Owner:    h,
			Evidence: evidence,
			Type:     "code",
			Path:     relPath,
			Title:    filepath.Base(relPath),
		})
	}
	addNote := func(relPath string, score float64) {
		if len(out) >= limit {
			return
		}
		h := knowledge.NoteHandle(relPath)
		key := h.String()
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, search.Candidate{
			Handle: h,
			Owner:  h,
			Evidence: []search.Evidence{
				{Type: "explicit_seed", RawScore: score, Source: "explicit_seeds"},
				{Type: "path_exact", RawScore: 1, Source: "explicit_seeds"},
			},
			Type:       "note",
			Path:       relPath,
			NoteID:     relPath,
			Title:      filepath.Base(relPath),
			ChunkIndex: -1,
		})
	}

	for _, relPath := range spec.ExplicitSeedPaths {
		if ctx.Err() != nil || len(out) >= limit {
			break
		}
		absPath, ok := safeJoinVaultPath(r.VaultPath, relPath)
		if !ok {
			continue
		}
		info, err := os.Stat(absPath)
		if err != nil {
			continue
		}
		if info.IsDir() {
			for _, file := range representativeCodeFiles(absPath, relPath, spec.PathKinds, max(1, min(3, limit-len(out)))) {
				addCode(file, 0.95, false)
			}
			continue
		}
		switch search.ExplicitSeedPathKind(spec, relPath) {
		case search.PathKindNote:
			addNote(relPath, 0.9)
		case search.PathKindCode:
			addCode(relPath, 1.0, true)
			if r.IncludeSiblingCode {
				for _, file := range siblingCodeFiles(absPath, relPath, spec.PathKinds, limit-len(out), false) {
					addCode(file, 0.86, false)
				}
			}
			if r.IncludeSiblingTests {
				for _, file := range siblingCodeFiles(absPath, relPath, spec.PathKinds, limit-len(out), true) {
					addCode(file, 0.72, false)
				}
			}
		}
	}
	return out, nil
}

func siblingCodeFiles(absFile, relFile string, pathKinds map[string]search.PathKind, limit int, includeTests bool) []string {
	if limit <= 0 {
		return nil
	}
	dir := filepath.Dir(absFile)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	relDir := filepath.ToSlash(filepath.Dir(relFile))
	base := filepath.Base(relFile)
	hits := make([]string, 0, limit)
	for _, entry := range entries {
		if entry.IsDir() || entry.Name() == base {
			continue
		}
		rel := filepath.ToSlash(filepath.Join(relDir, entry.Name()))
		if pathKinds[rel] != search.PathKindCode || (search.IsTestPath(rel) != includeTests) {
			continue
		}
		hits = append(hits, rel)
	}
	sort.SliceStable(hits, func(i, j int) bool {
		pi := seedFilePriority(hits[i], relDir)
		pj := seedFilePriority(hits[j], relDir)
		if pi != pj {
			return pi < pj
		}
		return hits[i] < hits[j]
	})
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits
}

func representativeCodeFiles(absDir, relDir string, pathKinds map[string]search.PathKind, limit int) []string {
	if limit <= 0 {
		return nil
	}
	var hits []string
	_ = filepath.WalkDir(absDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if path != absDir {
				rel, relErr := filepath.Rel(absDir, path)
				if relErr != nil {
					return nil
				}
				if depth := len(strings.Split(filepath.ToSlash(rel), "/")); depth > 2 {
					return filepath.SkipDir
				}
			}
			return nil
		}
		rel, relErr := filepath.Rel(absDir, path)
		if relErr != nil {
			return nil
		}
		candidate := filepath.ToSlash(filepath.Join(relDir, rel))
		if pathKinds[candidate] != search.PathKindCode || search.IsTestPath(candidate) {
			return nil
		}
		hits = append(hits, candidate)
		if len(hits) >= limit*3 {
			return context.Canceled
		}
		return nil
	})
	sort.SliceStable(hits, func(i, j int) bool {
		pi := seedFilePriority(hits[i], relDir)
		pj := seedFilePriority(hits[j], relDir)
		if pi != pj {
			return pi < pj
		}
		return hits[i] < hits[j]
	})
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits
}

func seedFilePriority(path, relDir string) int {
	base := strings.ToLower(filepath.Base(path))
	dirBase := strings.ToLower(filepath.Base(relDir))
	switch {
	case search.IsEntryPointFile(path):
		return 0
	case dirBase != "" && base == dirBase+".go":
		return 1
	case strings.Contains(base, "index.") || strings.Contains(base, "service.") || strings.Contains(base, "semantic_query"):
		return 2
	default:
		return 3
	}
}

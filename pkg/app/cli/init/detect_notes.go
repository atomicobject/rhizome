package init

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/paths"
)

type markdownDirCandidate struct {
	name            string
	include         string
	reason          string
	score           int
	markdownCount   int
	wikilinkCount   int
	frontmatterHits int
}

func detectMarkdownDirCandidates(root string) []markdownDirCandidate {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}

	candidates := make([]markdownDirCandidate, 0)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}

		dir := filepath.Join(root, name)
		stats := scanMarkdownDirCandidate(root, dir, name)
		if stats.markdownCount == 0 {
			continue
		}
		if !shouldIncludeMarkdownCandidate(stats) {
			continue
		}
		candidates = append(candidates, stats)
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].score != candidates[j].score {
			return candidates[i].score > candidates[j].score
		}
		if candidates[i].markdownCount != candidates[j].markdownCount {
			return candidates[i].markdownCount > candidates[j].markdownCount
		}
		return candidates[i].name < candidates[j].name
	})
	return candidates
}

func scanMarkdownDirCandidate(root, dir, name string) markdownDirCandidate {
	stats := markdownDirCandidate{
		name:    name,
		include: filepath.ToSlash(filepath.Join(name, "**/*.md")),
		reason:  "Found markdown in " + name,
		score:   markdownDirNameScore(name),
	}

	rootPaths, _ := paths.NewVaultPaths(root)
	matcher := initIgnoreMatcher(root)

	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if rel, relErr := rootPaths.RelStrict(path); relErr == nil && shouldSkipDir(rel.String(), d.Name(), matcher) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(d.Name()), ".md") {
			return nil
		}

		stats.markdownCount++
		if stats.markdownCount <= 12 {
			stats.score += 2
		}

		if stats.markdownCount <= 20 {
			if body, readErr := os.ReadFile(path); readErr == nil {
				snippet := string(body)
				if len(snippet) > 4096 {
					snippet = snippet[:4096]
				}
				if strings.Contains(snippet, "[[") {
					stats.wikilinkCount++
					stats.score += 3
				}
				if strings.Contains(snippet, "---") || strings.Contains(snippet, "tags:") || strings.Contains(snippet, "summary:") {
					stats.frontmatterHits++
					stats.score += 2
				}
			}
		}
		return nil
	})

	return stats
}

func shouldIncludeMarkdownCandidate(candidate markdownDirCandidate) bool {
	if candidate.markdownCount == 0 {
		return false
	}
	if markdownDirNameScore(candidate.name) > 0 {
		return true
	}
	if candidate.markdownCount >= 3 {
		return true
	}
	return candidate.wikilinkCount > 0 || candidate.frontmatterHits > 0
}

func markdownDirNameScore(name string) int {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "notes", "vault", "wiki":
		return 12
	case "docs", "documentation", "knowledge", "handbook", "playbook":
		return 10
	case "specs", "design":
		return 7
	default:
		return 0
	}
}

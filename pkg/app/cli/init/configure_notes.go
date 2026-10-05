package init

import (
	"path/filepath"
	"strings"

	"github.com/atomicobject/rhizome/pkg/paths"
)

func markdownDirFromInclude(include string) (string, bool) {
	include = filepath.ToSlash(strings.TrimSpace(include))
	switch {
	case include == "**/*.md":
		return "the repo root", true
	case strings.HasSuffix(include, "/**/*.md"):
		return strings.TrimSuffix(include, "/**/*.md") + "/", true
	case strings.HasSuffix(include, "/*.md"):
		return strings.TrimSuffix(include, "/*.md") + "/", true
	default:
		return "", false
	}
}

func joinHumanPaths(paths []string) string {
	seen := map[string]struct{}{}
	ordered := make([]string, 0, len(paths))
	for _, path := range paths {
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		ordered = append(ordered, path)
	}
	switch len(ordered) {
	case 0:
		return ""
	case 1:
		return ordered[0]
	case 2:
		return ordered[0] + " and " + ordered[1]
	default:
		return strings.Join(ordered[:len(ordered)-1], ", ") + ", and " + ordered[len(ordered)-1]
	}
}

func relativizeToProject(path, projectRoot string) string {
	if path == "" {
		return ""
	}
	abs := path
	if !filepath.IsAbs(abs) {
		return filepath.ToSlash(path)
	}
	projectPaths, err := paths.NewVaultPaths(projectRoot)
	if err != nil || projectPaths.Root() == "" {
		return filepath.ToSlash(path)
	}
	rel, err := projectPaths.RelStrict(abs)
	if err != nil {
		return filepath.ToSlash(path)
	}
	if rel.String() == "" {
		return "."
	}
	return rel.String()
}

package init

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/notediscovery"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/bmatcuk/doublestar/v4"
)

// Include existing workspace starters because a resumed install may write no assets.
func ensureInstalledTemplateIncludes(projectRoot string, cfg *obsidian.LocalConfig, writtenPaths []string) ([]string, bool, error) {
	const templates = "docs/efforts/templates"
	vaultPaths, err := paths.NewVaultPaths(projectRoot)
	if err != nil {
		return nil, false, fmt.Errorf("resolve template vault root: %w", err)
	}
	templateRoot := filepath.Join(vaultPaths.Root(), templates)
	resolvedRoot, err := filepath.EvalSymlinks(templateRoot)
	if err != nil && !os.IsNotExist(err) {
		return nil, false, fmt.Errorf("resolve installed effort templates: %w", err)
	}
	if err == nil {
		if _, err := vaultPaths.RelStrict(resolvedRoot); err != nil {
			return nil, false, fmt.Errorf("resolve installed effort templates: %w", err)
		}
		// Note discovery does not descend through directory symlinks. A lexical
		// include would claim coverage for generated notes it cannot discover.
		if resolvedRoot != templateRoot {
			return nil, false, fmt.Errorf("installed effort templates must not use directory symlinks: %s", templateRoot)
		}
	}
	templatePaths := append([]string(nil), writtenPaths...)
	err = filepath.WalkDir(templateRoot, func(path string, entry fs.DirEntry, err error) error {
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if entry.Type().IsRegular() && (strings.HasSuffix(entry.Name(), ".md.template") || strings.HasSuffix(entry.Name(), ".html.template")) {
			// Resolve first so RelStrict's lexical-root fallback cannot admit
			// an installed asset reached through an escaping directory symlink.
			resolved, err := filepath.EvalSymlinks(path)
			if err != nil {
				return err
			}
			if _, err := vaultPaths.RelStrict(resolved); err != nil {
				return err
			}
			// Include coverage follows the installed docs path.
			rel, err := paths.ToRel(paths.AbsPath(path), vaultPaths.Root())
			if err != nil {
				return err
			}
			templatePaths = append(templatePaths, rel.String())
		}
		return nil
	})
	if err != nil {
		return nil, false, fmt.Errorf("read installed effort templates: %w", err)
	}
	added, changed := ensureIncludesCoverTemplatePaths(cfg, templatePaths)
	return added, changed, nil
}

// ensureIncludesCoverTemplatePaths inspects template-written paths and appends
// any glob to cfg.Notes.Includes needed so the Markdown and effort HTML notes among them
// actually enter the vault. Returns the list of globs that were appended (in
// order) and a bool indicating whether cfg was modified.
//
// Paths under hidden top-level directories (e.g. .rhizome, .agents, .claude)
// are skipped — those are agent-skill / config artifacts, not vault notes.
func ensureIncludesCoverTemplatePaths(cfg *obsidian.LocalConfig, writtenPaths []string) ([]string, bool) {
	if cfg == nil || len(writtenPaths) == 0 {
		return nil, false
	}

	includes := cfg.Notes.Includes
	if len(includes) == 0 {
		// Empty Includes resolves to "**/*.md" at discovery time, which covers
		// every markdown file. Preserve that default if HTML needs an include.
		includes = []string{"**/*.md"}
	}

	added := map[string]struct{}{}
	var addedOrder []string

	for _, raw := range writtenPaths {
		rel := filepath.ToSlash(strings.TrimSpace(raw))
		if rel == "" {
			continue
		}
		// Workspace starters are inert .html.template assets. Authorize the
		// HTML notes in any generated effort workspace, not the template directory.
		if strings.HasPrefix(rel, "docs/efforts/templates/workspace/") && strings.HasSuffix(rel, ".html.template") {
			rel = "docs/efforts/**/*.html"
		}
		if strings.HasPrefix(rel, "docs/efforts/templates/") && strings.HasSuffix(rel, ".md.template") {
			rel = strings.TrimSuffix(rel, ".template")
		}
		ext := strings.ToLower(filepath.Ext(rel))
		if ext != ".md" && !(ext == ".html" && strings.HasPrefix(rel, "docs/efforts/")) {
			continue
		}
		topDir := topLevelDir(rel)
		if topDir == "" || strings.HasPrefix(topDir, ".") {
			continue
		}
		glob := topDir + "/**/*.md"
		matches := pathMatchesAnyGlob
		if ext == ".html" {
			glob = "docs/efforts/**/*.html"
			matches = pathMatchesExplicitHTMLGlob
		}
		if matches(rel, includes) {
			continue
		}
		if _, seen := added[glob]; seen {
			continue
		}
		// Re-check against newly-added globs in this pass.
		if matches(rel, addedOrder) {
			continue
		}
		added[glob] = struct{}{}
		addedOrder = append(addedOrder, glob)
	}

	if len(addedOrder) == 0 {
		return nil, false
	}
	cfg.Notes.Includes = append(includes, addedOrder...)
	return addedOrder, true
}

// HTML ownership requires an explicit extension claim, even when broad includes match.
func pathMatchesExplicitHTMLGlob(rel string, globs []string) bool {
	for _, glob := range globs {
		if notediscovery.MatchesExplicitInclude(glob, paths.RelPath(rel), []string{".html", ".htm"}) {
			return true
		}
	}
	return false
}

func pathMatchesAnyGlob(rel string, globs []string) bool {
	for _, g := range globs {
		if ok, _ := doublestar.Match(filepath.ToSlash(g), rel); ok {
			return true
		}
	}
	return false
}

func topLevelDir(rel string) string {
	rel = filepath.ToSlash(rel)
	if i := strings.Index(rel, "/"); i > 0 {
		return rel[:i]
	}
	return ""
}

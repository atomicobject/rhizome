package cmd

// rzm index --explain <path>: ignore-model diagnostic (SPEC-0064 US2).
// Evaluates a path against the unified ignore layers and reports the deciding
// rule instead of indexing.

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/ignore"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// runIndexExplain resolves input against the vault root, evaluates it with the
// same matcher indexing uses, and writes a short report. Pure pattern
// evaluation: the path does not need to exist.
func runIndexExplain(w io.Writer, vaultRoot string, configExcludes []string, input string) error {
	relPath, isDir, err := resolveExplainPath(vaultRoot, input)
	if err != nil {
		return err
	}
	if !isDir && ignore.IsSystemContextPath(relPath) {
		if ignore.IsDefaultInfrastructurePath(relPath) {
			fmt.Fprintf(w, "%s: ignored\n", relPath)
			fmt.Fprintln(w, "  ignored by built-in infrastructure boundary")
			return nil
		}
		matcher := obsidian.LoadVaultHardIgnoreMatcher(vaultRoot)
		decision := matcher.Explain(relPath, false)
		if decision.Ignored {
			renderIgnoreExplain(w, matcher, relPath, false)
			return nil
		}
		fmt.Fprintf(w, "%s: indexed\n", relPath)
		fmt.Fprintln(w, "  system CONTEXT.md bypasses notes includes/excludes")
		return nil
	}
	matcher := obsidian.LoadVaultIgnoreMatcher(vaultRoot, configExcludes)
	renderIgnoreExplain(w, matcher, relPath, isDir)
	if !isDir && !matcher.Explain(relPath, false).Ignored {
		renderCodeExplain(w, vaultRoot, relPath)
	}
	return nil
}

// renderCodeExplain reports whether a code file is indexed as code, and
// otherwise which setting keeps it out and how to change it.
//
// Docs: [[init-starter-workflow#^SPEC-0038-US9-AC3]]
func renderCodeExplain(w io.Writer, vaultRoot, relPath string) {
	lang, ok := codeanchor.LangForPath(relPath)
	if !ok {
		return
	}
	cfg, err := obsidian.LoadCodeConfig(vaultRoot)
	if err != nil {
		return
	}
	name := codeLanguageName(lang)
	roots := cfg.CodeRoots()
	switch {
	case !cfg.Enabled:
		fmt.Fprintf(w, "  not indexed as %s code: code indexing is off; run rzm init to turn it on\n", name)
	case cfg.AutomaticScope && cfg.LanguageDisabled(lang):
		fmt.Fprintf(w, "  not indexed as %s code: code.disabledLanguages in .rhizome/config.yml turns %s off; remove it there to index this file\n", name, name)
	case !pathUnderRoots(relPath, roots):
		fmt.Fprintf(w, "  not indexed as %s code: outside the code folders in .rhizome/config.yml (%s); run rzm init to remove the folder limits\n", name, strings.Join(roots, ", "))
	case cfg.AutomaticScope:
		fmt.Fprintf(w, "  indexed as %s code (code indexing covers the whole repository)\n", name)
	default:
		fmt.Fprintf(w, "  indexed as %s code (inside the code folders in .rhizome/config.yml)\n", name)
	}
}

func pathUnderRoots(relPath string, roots []string) bool {
	for _, root := range roots {
		root = strings.Trim(filepath.ToSlash(filepath.Clean(root)), "/")
		if root == "." || root == "" || relPath == root || strings.HasPrefix(relPath, root+"/") {
			return true
		}
	}
	return false
}

func codeLanguageName(lang codeanchor.Lang) string {
	switch lang {
	case codeanchor.LangPy:
		return "Python"
	case codeanchor.LangGo:
		return "Go"
	case codeanchor.LangTS:
		return "TypeScript/JavaScript"
	case codeanchor.LangCs:
		return "C#"
	case codeanchor.LangPhp:
		return "PHP"
	}
	return string(lang)
}

// resolveExplainPath converts user input (absolute or vault-root-relative) to
// a vault-root-relative path and decides whether to evaluate it as a directory.
func resolveExplainPath(vaultRoot, input string) (relPath string, isDir bool, err error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", false, fmt.Errorf("--explain requires a path")
	}
	trailingSlash := strings.HasSuffix(input, "/")
	vaultPaths, err := paths.NewVaultPaths(vaultRoot)
	if err != nil {
		return "", false, err
	}
	rel, err := vaultPaths.RelStrict(input)
	if err != nil {
		return "", false, fmt.Errorf("path %q is outside the vault root %s: %w", input, vaultRoot, err)
	}
	if rel.String() == "" {
		return "", false, fmt.Errorf("path %q resolves to the vault root; pass a path inside the vault", input)
	}
	relPath = rel.String()

	isDir = trailingSlash
	if !isDir {
		abs := paths.AbsFromInputWithVaultPaths(vaultPaths, vaultRoot, relPath)
		if info, statErr := os.Stat(abs.String()); statErr == nil && info.IsDir() {
			isDir = true
		}
	}
	return relPath, isDir, nil
}

// renderIgnoreExplain writes the human-readable verdict for relPath.
func renderIgnoreExplain(w io.Writer, matcher *ignore.Matcher, relPath string, isDir bool) {
	d := matcher.Explain(relPath, isDir)
	if d.Ignored {
		fmt.Fprintf(w, "%s: ignored\n", relPath)
		line := fmt.Sprintf("ignored by %s", formatIgnoreRule(d.Rule))
		if d.IgnoredAncestor != "" && d.IgnoredAncestor != relPath {
			line += fmt.Sprintf(" via ancestor '%s'", d.IgnoredAncestor)
		}
		fmt.Fprintf(w, "  %s\n", line)
		return
	}
	fmt.Fprintf(w, "%s: indexed\n", relPath)
	if d.Boundary != nil {
		fmt.Fprintf(w, "  re-included by %s\n", formatIgnoreRule(d.Boundary))
		return
	}
	fmt.Fprintln(w, "  included (no ignore rule matches)")
}

// formatIgnoreRule renders a RuleRef as e.g. ".gitignore:3 'app/' (gitignore layer)".
func formatIgnoreRule(r *ignore.RuleRef) string {
	if r == nil {
		return "unknown rule"
	}
	switch r.Layer {
	case ignore.LayerDefault:
		return fmt.Sprintf("built-in defaults '%s'", r.Pattern)
	case ignore.LayerConfig:
		return fmt.Sprintf("config excludes '%s'", r.Pattern)
	}
	source := r.Source
	if source == "" {
		if r.Layer == ignore.LayerRhizome {
			source = ".rhizome/ignore"
		} else {
			source = ".gitignore"
		}
	}
	return fmt.Sprintf("%s:%d '%s' (%s layer)", source, r.Line, r.Pattern, r.Layer)
}

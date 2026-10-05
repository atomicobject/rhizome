package codeanchor

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/bmatcuk/doublestar/v4"
)

// ErrUnsupportedLanguage is returned when no indexer is registered.
var ErrUnsupportedLanguage = errors.New("unsupported language")

func normalizeBasePath(basePath string) string {
	basePath = strings.TrimSpace(basePath)
	if basePath == "" {
		return ""
	}
	return paths.ResolveSymlinks(basePath).String()
}

func (s *Service) codePathRef(path string) (paths.CodePathRef, error) {
	if strings.TrimSpace(path) == "" {
		return paths.CodePathRef{}, nil
	}
	if s.vault.Root() == "" {
		// Test and in-memory services may not know the vault root. Preserve the old
		// fallback, but production callers should configure VaultPaths so persisted
		// code paths cannot escape the vault or mix absolute and relative keys.
		rel := paths.NormalizeCode(path)
		abs := paths.ResolveSymlinks(path)
		return paths.CodePathRef{Rel: rel, Abs: abs}, nil
	}
	return paths.ResolveCodeRefWithVaultPaths(s.vault, path)
}

func (s *Service) notePathRef(path string) (paths.NotePathRef, error) {
	if strings.TrimSpace(path) == "" {
		return paths.NotePathRef{}, nil
	}
	if s.vault.Root() == "" {
		// Same rootless fallback as codePathRef; persisted note keys in real vaults
		// should come from ResolveNotePathRefWithVaultPaths for strict scoping.
		// This path is already an authored note identity, so do not infer a
		// Markdown suffix while preserving its extension casing.
		rel := paths.NormalizeNotePath(path)
		abs := paths.ResolveSymlinks(path)
		return paths.NotePathRef{Rel: rel, Abs: abs}, nil
	}
	return paths.ResolveNotePathRefWithVaultPaths(s.vault, path)
}

func (s *Service) relCodePath(path string) (string, error) {
	ref, err := s.codePathRef(path)
	if err != nil {
		return "", err
	}
	return ref.Rel.String(), nil
}

func (s *Service) absCodePath(path string) (string, error) {
	ref, err := s.codePathRef(path)
	if err != nil {
		return "", err
	}
	return ref.Abs.String(), nil
}

func resolveGlobPatterns(globs []string, vaultPaths paths.VaultPaths) ([]string, error) {
	if len(globs) == 0 {
		return nil, fmt.Errorf("missing glob pattern")
	}
	out := make([]string, 0, len(globs))
	seen := make(map[string]bool)
	for _, raw := range globs {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		pats, err := resolveOneGlob(raw, vaultPaths)
		if err != nil {
			return nil, err
		}
		for _, p := range pats {
			if p == "" || seen[p] {
				continue
			}
			seen[p] = true
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("empty glob")
	}
	return out, nil
}

func resolveOneGlob(raw string, vaultPaths paths.VaultPaths) ([]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("empty glob")
	}
	if vaultPaths.Root() == "" {
		return nil, fmt.Errorf("missing vault root for glob anchor")
	}

	// Treat trailing slashes as a directory intent.
	dirIntent := strings.HasSuffix(raw, "/") || strings.HasSuffix(raw, string(filepath.Separator))
	raw = strings.TrimRight(raw, "/"+string(filepath.Separator))
	if raw == "" {
		return nil, fmt.Errorf("empty glob")
	}

	// If there are no glob metacharacters, resolve as a path and expand directories.
	if !hasGlobMeta(raw) {
		resolved, err := resolvePathPrefix(raw, vaultPaths)
		if err != nil {
			return nil, err
		}
		// Expand directory globs: match the directory itself and anything beneath it.
		isDir := dirIntent
		if !isDir {
			absPath, err := vaultPaths.AbsCode(paths.NormalizeCode(resolved))
			statErr := err
			if err == nil {
				if info, err := os.Stat(absPath.String()); err == nil && info.IsDir() {
					isDir = true
					statErr = nil
				} else {
					statErr = err
				}
			}
			if statErr != nil && filepath.Ext(resolved) == "" {
				// Best-effort heuristic: paths without an extension are often directories.
				isDir = true
			}
		}
		if isDir {
			return []string{
				filepath.ToSlash(resolved),
				filepath.ToSlash(filepath.Join(resolved, "**")),
			}, nil
		}
		return []string{filepath.ToSlash(resolved)}, nil
	}

	// For true glob patterns, normalize to vault-relative paths without touching wildcards.
	p := filepath.ToSlash(raw)
	normalizedRoot := filepath.ToSlash(vaultPaths.Root())
	if strings.HasPrefix(p, normalizedRoot+"/") {
		p = strings.TrimPrefix(p, normalizedRoot+"/")
	} else if strings.HasPrefix(p, "/") {
		return nil, fmt.Errorf("glob %q is outside vault root", raw)
	}
	if len(p) >= 2 && p[1] == ':' {
		return nil, fmt.Errorf("glob %q is outside vault root", raw)
	}
	p = filepath.ToSlash(filepath.Clean(p))
	if p == ".." || strings.HasPrefix(p, "../") {
		return nil, fmt.Errorf("glob %q is outside vault root", raw)
	}
	// Validate pattern early to fail fast on invalid glob syntax.
	if !doublestar.ValidatePattern(p) {
		return nil, fmt.Errorf("invalid glob pattern %q", raw)
	}
	return []string{p}, nil
}

func hasGlobMeta(s string) bool {
	// doublestar supports **, *, ?, character classes, and {alt} groups.
	// We treat any of these characters as meta for "this is not a plain path".
	return strings.ContainsAny(s, "*?[]{}")
}

// HasIndexer reports whether a language indexer is configured.
func (s *Service) HasIndexer(lang Lang) bool {
	_, ok := s.indexers[lang]
	return ok
}

// HasAnyIndexer reports whether any language indexers are configured.
func (s *Service) HasAnyIndexer() bool {
	return len(s.indexers) > 0
}

// SupportedLangs returns configured language identifiers (sorted).
func (s *Service) SupportedLangs() []Lang {
	langs := make([]Lang, 0, len(s.indexers))
	for lang := range s.indexers {
		langs = append(langs, lang)
	}
	sort.Slice(langs, func(i, j int) bool { return langs[i] < langs[j] })
	return langs
}

// TailIndex returns the configured path tail index (if any).
func (s *Service) TailIndex() *PathTailIndex { return s.tailIdx }

func normalizeSymbol(ref SymbolRef) string {
	if ref.Pkg == "" {
		return ref.Name
	}
	if ref.Lang == LangPhp {
		// PHP joins class/static members with `::` and namespace-only symbols
		// (functions, classes, interfaces) with `\`. The PHP indexer emits both
		// forms; lookups must match the form stored at indexing time.
		if ref.Member {
			return ref.Pkg + "::" + ref.Name
		}
		return ref.Pkg + `\` + ref.Name
	}
	return fmt.Sprintf("%s.%s", ref.Pkg, ref.Name)
}

func normalizeTypeSymbol(ref SymbolRef) string {
	if ref.Pkg == "" {
		return ref.Name
	}
	if ref.Lang == LangPhp {
		return ref.Pkg + `\` + ref.Name
	}
	return normalizeSymbol(ref)
}

func symbolRefKey(ref SymbolRef) string {
	key := fmt.Sprintf("%s|%s|%s", strings.ToLower(string(ref.Lang)), ref.Pkg, ref.Name)
	if ref.Lang == LangPhp {
		return fmt.Sprintf("%s|%t", key, ref.Member)
	}
	return key
}

// NormalizeSymbolRef returns the language-aware fully qualified identity for a
// symbol reference. PHP members use Class::member; namespace symbols use `\`.
func NormalizeSymbolRef(ref SymbolRef) string {
	return normalizeSymbol(ref)
}

func hashBytes(b []byte) string {
	return fmt.Sprintf("%x", sha256.Sum256(b))
}

func matchArgs(filters map[string]string, args map[string]string) bool {
	if len(filters) == 0 {
		return true
	}
	if args == nil {
		return false
	}
	for k, v := range filters {
		if args[k] != v {
			return false
		}
	}
	return true
}

func splitFQN(fqn string) (pkg string, name string) {
	fqn = strings.TrimSpace(fqn)
	if fqn == "" {
		return "", ""
	}
	if dot := strings.LastIndex(fqn, "."); dot >= 0 {
		return fqn[:dot], fqn[dot+1:]
	}
	return "", fqn
}

// normalizePath resolves symlinks and cleans the path for consistent storage.
// If symlink resolution fails, returns the cleaned absolute path.
// Always returns forward slashes for consistent cross-platform database lookups.
// Deprecated: Use paths.ResolveSymlinks directly for new code.
// ReverseString reverses a string character-by-character (rune-safe).
// Used for suffix matching via reversed FQN index.
func ReverseString(s string) string {
	runes := []rune(s)
	for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
		runes[i], runes[j] = runes[j], runes[i]
	}
	return string(runes)
}
